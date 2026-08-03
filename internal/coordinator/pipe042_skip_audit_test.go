package coordinator

import (
	"context"
	"errors"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/pipeline"
	"codeflux.dev/codeflux/internal/storage"
)

// TestPIPE042_ComputeSkipAuditClassifiesEveryNonEstablishingStage covers the
// classification requirement 3 asks for: every stage this run recorded
// skipped or not-implemented is either a principled decline, a decision with
// no check by design, or an unimplemented gap, and the headline ratio only
// counts those two states.
//
// Proven to discriminate: changing classifySkipAuditEntry's MaySatisfy-false
// branch to always report SkipNoCheckByDesign (dropping the
// Section33Resolved lookup) makes the atom-optimization and atom-complexity
// assertions below fail, because both would then read no-check-by-design
// instead of principled-decline. Reverting restores this test to green. The
// change was made, the failure observed, and the fix restored before this
// report; the test as committed exercises the real code path.
func TestPIPE042_ComputeSkipAuditClassifiesEveryNonEstablishingStage(t *testing.T) {
	records := []storage.PipelineStageRecord{
		// Satisfied, failed, and blocked: counted toward TotalStages but never
		// classified, and never counted toward the skip ratio.
		{Stage: pipeline.StageInstructions, Name: "instructions",
			State: pipeline.StateSatisfied, DetailRedacted: "held"},
		{Stage: pipeline.StageAssembly, Name: "assembly",
			State: pipeline.StateFailed, DetailRedacted: "did not compile"},
		{Stage: pipeline.StageAdversarial, Name: "adversarial",
			State: pipeline.StateBlocked, DetailRedacted: "nothing built to probe"},

		// A §33 stage resolved by being made unable to claim (PIPE-010):
		// skipped, with a real check, MaySatisfy false, named in
		// Section33Resolved — a principled decline.
		{Stage: pipeline.StageAtomOptimization, Name: "atom-optimization",
			State:          pipeline.StateSkipped,
			DetailRedacted: "no rewrite is performed by this build"},
		// The other §33 stage resolved the same way (PIPE-011).
		{Stage: pipeline.StageAtomComplexity, Name: "atom-complexity",
			State:          pipeline.StateSkipped,
			DetailRedacted: "nothing measures growth across input sizes"},

		// A stage with no check at all, recorded not-implemented rather than
		// blocked (the shape a truncated run leaves via ledger.close): no
		// check by design, not a decline of a check that could have run.
		{Stage: pipeline.StageHumanAcceptance, Name: "human-acceptance",
			State:          pipeline.StateNotImplemented,
			DetailRedacted: "no part of this build performs this stage"},
		{Stage: pipeline.StageDeliver, Name: "deliver",
			State:          pipeline.StateNotImplemented,
			DetailRedacted: "no part of this build performs this stage"},

		// A stage whose check may satisfy in general (MaySatisfy true, not a
		// §33 entry) but this run's own facts made a legitimate skip: a
		// principled decline all the same, per PIPE-014's zero-boundary case.
		{Stage: pipeline.StageAtomFuzz, Name: "atom-fuzz",
			State:          pipeline.StateSkipped,
			DetailRedacted: "the run produced no decoding boundary"},

		// A stage whose check may satisfy and is even flagged Unestablished
		// (PIPE-137), but this run recorded not-implemented, not skipped: for
		// this run nothing established anything, so it reads unimplemented
		// regardless of the stage's general potential.
		{Stage: pipeline.StageContracts, Name: "contracts",
			State:          pipeline.StateNotImplemented,
			DetailRedacted: "no part of this build performs this stage"},

		// The audit's own stage, present in the fetched rows as a caller
		// that reads after the audit's row exists would see it. It must be
		// excluded from every count.
		{Stage: pipeline.StageSkipAudit, Name: "skip-audit",
			State: pipeline.StateSatisfied, DetailRedacted: "a stale prior read"},
	}

	result, err := ComputeSkipAudit(records, pipeline.StageSkipAudit, pipeline.ProfileDefault)
	if err != nil {
		t.Fatalf("ComputeSkipAudit refused a real ledger: %v", err)
	}

	if result.TotalStages != 9 {
		t.Fatalf("TotalStages = %d, want 9 (self excluded)", result.TotalStages)
	}
	if result.Satisfied != 1 || result.Failed != 1 || result.Blocked != 1 {
		t.Fatalf("satisfied/failed/blocked = %d/%d/%d, want 1/1/1",
			result.Satisfied, result.Failed, result.Blocked)
	}
	if result.Skipped != 3 {
		t.Fatalf("Skipped = %d, want 3", result.Skipped)
	}
	if result.NotImplemented != 3 {
		t.Fatalf("NotImplemented = %d, want 3", result.NotImplemented)
	}
	const wantRatio = 6.0 / 9.0
	if diff := result.SkipRatio - wantRatio; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("SkipRatio = %v, want %v", result.SkipRatio, wantRatio)
	}
	if len(result.Entries) != 6 {
		t.Fatalf("Entries has %d rows, want 6 (skipped + not-implemented)",
			len(result.Entries))
	}

	classifications := make(map[pipeline.Number]SkipAuditClassification, len(result.Entries))
	for _, entry := range result.Entries {
		classifications[entry.Stage] = entry.Classification
	}
	want := map[pipeline.Number]SkipAuditClassification{
		pipeline.StageAtomOptimization: SkipPrincipledDecline,
		pipeline.StageAtomComplexity:   SkipPrincipledDecline,
		pipeline.StageAtomFuzz:         SkipPrincipledDecline,
		pipeline.StageHumanAcceptance:  SkipNoCheckByDesign,
		pipeline.StageDeliver:          SkipNoCheckByDesign,
		pipeline.StageContracts:        SkipUnimplemented,
	}
	for stage, expected := range want {
		if got := classifications[stage]; got != expected {
			t.Errorf("stage %d classified %q, want %q", stage, got, expected)
		}
	}

	// The reason for a principled decline names its governing ticket.
	for _, entry := range result.Entries {
		if entry.Stage == pipeline.StageAtomOptimization &&
			!containsSubstring(entry.Reason, "PIPE-010") {
			t.Errorf("atom-optimization's reason does not name PIPE-010: %q", entry.Reason)
		}
	}

	// Entries are ordered by stage number, so a presentation layer can
	// render them without sorting itself.
	for index := 1; index < len(result.Entries); index++ {
		if result.Entries[index-1].Stage >= result.Entries[index].Stage {
			t.Fatalf("entries are not ordered by stage: %d then %d",
				result.Entries[index-1].Stage, result.Entries[index].Stage)
		}
	}
}

// containsSubstring is a tiny helper so this file needs no extra import for
// one substring check.
func containsSubstring(text, substring string) bool {
	for index := 0; index+len(substring) <= len(text); index++ {
		if text[index:index+len(substring)] == substring {
			return true
		}
	}
	return false
}

// TestPIPE042_ComputeSkipAuditRefusesToClaimARatioFromNothing covers
// requirement 4: the audit must not satisfy vacuously.
//
// Proven to discriminate: removing the `result.TotalStages == 0` guard in
// ComputeSkipAudit makes this test fail — it would return a zero-valued
// SkipAuditResult with SkipRatio 0 and a nil error instead of the error this
// test requires, which is indistinguishable from "every stage established
// something" to a caller that does not check the error. That change was
// made, this test was rerun and failed on the `err == nil` branch exactly as
// expected, and the guard was restored before this report.
func TestPIPE042_ComputeSkipAuditRefusesToClaimARatioFromNothing(t *testing.T) {
	for name, records := range map[string][]storage.PipelineStageRecord{
		"empty ledger": nil,
		"only the audit's own stale row": {
			{Stage: pipeline.StageSkipAudit, Name: "skip-audit",
				State: pipeline.StateSatisfied},
		},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := ComputeSkipAudit(records, pipeline.StageSkipAudit, pipeline.ProfileDefault)
			if err == nil {
				t.Fatalf("ComputeSkipAudit computed a ratio from nothing: %+v", result)
			}
			if result.TotalStages != 0 || len(result.Entries) != 0 {
				t.Errorf("a refused computation still carries data: %+v", result)
			}
		})
	}
}

// fakeSkipAuditStore is a PipelineStageLedgerStore a test can script without
// a database, matching the seam PIPE-006b defined for exactly this purpose.
type fakeSkipAuditStore struct {
	records []storage.PipelineStageRecord
	err     error
}

func (fake fakeSkipAuditStore) ListPipelineStages(
	context.Context, domain.TaskID, uint64,
) ([]storage.PipelineStageRecord, error) {
	return fake.records, fake.err
}

// TestPIPE042_CheckSkipAuditRecordsSkippedWhenItCannotComputeARatio proves
// checkSkipAudit — the bound performer in pipeline.Checks — honours the same
// vacuity guard at the stageOutcome boundary: a nil store, a failing read,
// and a ledger with nothing to audit all come back Skipped rather than
// Held, so ledger.decide can never record this stage satisfied on nothing.
//
// Proven to discriminate: changing the nil-store branch to `return
// held("", nil)` makes the first subtest fail on `outcome.Held`, exactly the
// defect this guards against — a run with no repositories attached would
// otherwise report a suspiciously perfect skip ratio of zero. Verified by
// making that change, observing the failure, and reverting it.
func TestPIPE042_CheckSkipAuditRecordsSkippedWhenItCannotComputeARatio(t *testing.T) {
	taskID, err := domain.NewTaskID()
	if err != nil {
		t.Fatalf("mint a task id: %v", err)
	}

	t.Run("nil store", func(t *testing.T) {
		outcome := checkSkipAudit(context.Background(), nil, taskID, 1)
		if outcome.Held || !outcome.Skipped {
			t.Fatalf("nil store outcome = %+v, want Skipped", outcome)
		}
	})

	t.Run("read error", func(t *testing.T) {
		store := fakeSkipAuditStore{err: errors.New("database is unavailable")}
		outcome := checkSkipAudit(context.Background(), store, taskID, 1)
		if outcome.Held || !outcome.Skipped {
			t.Fatalf("failed-read outcome = %+v, want Skipped", outcome)
		}
	})

	t.Run("nothing recorded besides the audit itself", func(t *testing.T) {
		store := fakeSkipAuditStore{records: []storage.PipelineStageRecord{
			{Stage: pipeline.StageSkipAudit, Name: "skip-audit",
				State: pipeline.StateSatisfied},
		}}
		outcome := checkSkipAudit(context.Background(), store, taskID, 1)
		if outcome.Held || !outcome.Skipped {
			t.Fatalf("empty-ledger outcome = %+v, want Skipped", outcome)
		}
	})
}

// TestPIPE042_CheckSkipAuditRecordsHeldWithTheFullBreakdownWhenItCan proves
// the shape a presentation layer (PIPE-044) reads: a successful computation
// is Held, its Detail states the headline figure, and its Evidence carries
// every count skipAuditEvidence documents, so PIPE-044 can render the
// headline and drill into the breakdown without recomputing anything.
func TestPIPE042_CheckSkipAuditRecordsHeldWithTheFullBreakdownWhenItCan(t *testing.T) {
	taskID, err := domain.NewTaskID()
	if err != nil {
		t.Fatalf("mint a task id: %v", err)
	}
	store := fakeSkipAuditStore{records: []storage.PipelineStageRecord{
		{Stage: pipeline.StageInstructions, Name: "instructions",
			State: pipeline.StateSatisfied, DetailRedacted: "held"},
		{Stage: pipeline.StageAtomOptimization, Name: "atom-optimization",
			State: pipeline.StateSkipped, DetailRedacted: "no rewrite performed"},
		{Stage: pipeline.StageHumanAcceptance, Name: "human-acceptance",
			State: pipeline.StateNotImplemented, DetailRedacted: "no check performs this"},
	}}

	outcome := checkSkipAudit(context.Background(), store, taskID, 1)
	if !outcome.Held || outcome.Skipped {
		t.Fatalf("outcome = %+v, want Held", outcome)
	}
	if outcome.Detail == "" {
		t.Fatal("a held audit reports no detail, so a reader sees no headline figure")
	}

	for _, key := range []string{
		"total_stages", "satisfied", "failed", "blocked", "skipped",
		"not_implemented", "skip_ratio", "principled_decline",
		"no_check_by_design", "unimplemented", "entries",
	} {
		if _, present := outcome.Evidence[key]; !present {
			t.Errorf("evidence carries no %q, which PIPE-044 needs to read "+
				"the breakdown without recomputing it", key)
		}
	}

	entries, ok := outcome.Evidence["entries"].([]map[string]any)
	if !ok {
		t.Fatalf("entries evidence is %T, want []map[string]any", outcome.Evidence["entries"])
	}
	if len(entries) != 2 {
		t.Fatalf("entries carries %d rows, want 2 (skipped + not-implemented)", len(entries))
	}
	if total, ok := outcome.Evidence["total_stages"].(int); !ok || total != 3 {
		t.Errorf("total_stages = %v, want 3", outcome.Evidence["total_stages"])
	}
}

// TestPIPE046a_SkipAuditClassifiesAProfileDeclineDifferentlyFromTheDefault
// proves classifySkipAuditEntry consults the run's declared profile
// (PIPE-046a) rather than only pipeline.Checks/pipeline.Section33Resolved.
//
// The same skipped row -- platform-matrix, which pipeline.Checks marks
// MaySatisfy true with no Section33Resolved entry -- classifies as
// SkipPrincipledDecline under checkSkipAudit's default profile (the same
// answer ClassifyDecline alone would give: a check that may satisfy,
// recorded skipped, is a principled decline) and as SkipDeclinedByProfile
// under checkSkipAuditWithProfile(..., pipeline.ProfileLibrary), which is
// the one profiles.go actually declines it under.
//
// Discrimination: replacing classifySkipAuditEntry's
// pipeline.ClassifyDeclineForProfile call with the unprofiled
// pipeline.ClassifyDecline (dropping the profile argument entirely) makes
// the library subtest below fail, because the row would then classify
// SkipPrincipledDecline the same as the default run -- the two subtests
// would stop disagreeing.
func TestPIPE046a_SkipAuditClassifiesAProfileDeclineDifferentlyFromTheDefault(t *testing.T) {
	taskID, err := domain.NewTaskID()
	if err != nil {
		t.Fatalf("mint a task id: %v", err)
	}
	store := fakeSkipAuditStore{records: []storage.PipelineStageRecord{
		{Stage: pipeline.StageInstructions, Name: "instructions",
			State: pipeline.StateSatisfied, DetailRedacted: "held"},
		{Stage: pipeline.StagePlatformMatrix, Name: "platform-matrix",
			State: pipeline.StateSkipped, DetailRedacted: "no cross-compilation target declared"},
	}}

	findEntry := func(entries []map[string]any, stage pipeline.Number) (map[string]any, bool) {
		for _, entry := range entries {
			if stageValue, ok := entry["stage"].(int); ok && pipeline.Number(stageValue) == stage {
				return entry, true
			}
		}
		return nil, false
	}

	t.Run("default profile classifies it a principled decline", func(t *testing.T) {
		outcome := checkSkipAudit(context.Background(), store, taskID, 1)
		entries, ok := outcome.Evidence["entries"].([]map[string]any)
		if !ok {
			t.Fatalf("entries evidence is %T", outcome.Evidence["entries"])
		}
		entry, found := findEntry(entries, pipeline.StagePlatformMatrix)
		if !found {
			t.Fatalf("platform-matrix is not in the entries: %+v", entries)
		}
		if entry["classification"] != string(SkipPrincipledDecline) {
			t.Errorf("default classification = %v, want %q",
				entry["classification"], SkipPrincipledDecline)
		}
	})

	t.Run("library profile classifies the same row declined by profile", func(t *testing.T) {
		outcome := checkSkipAuditWithProfile(context.Background(), store, taskID, 1, pipeline.ProfileLibrary)
		entries, ok := outcome.Evidence["entries"].([]map[string]any)
		if !ok {
			t.Fatalf("entries evidence is %T", outcome.Evidence["entries"])
		}
		entry, found := findEntry(entries, pipeline.StagePlatformMatrix)
		if !found {
			t.Fatalf("platform-matrix is not in the entries: %+v", entries)
		}
		if entry["classification"] != string(SkipDeclinedByProfile) {
			t.Errorf("library classification = %v, want %q",
				entry["classification"], SkipDeclinedByProfile)
		}
		reason, _ := entry["reason"].(string)
		if !strings.Contains(reason, `run profile "library"`) {
			t.Errorf("library reason does not name the declaring profile: %q", reason)
		}
		if declined, ok := outcome.Evidence["declined_by_profile"].(int); !ok || declined != 1 {
			t.Errorf("declined_by_profile evidence = %v, want 1", outcome.Evidence["declined_by_profile"])
		}
	})
}
