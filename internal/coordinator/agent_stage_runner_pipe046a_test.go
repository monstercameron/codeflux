package coordinator

import (
	"context"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/pipeline"
	"codeflux.dev/codeflux/internal/storage"
)

// TestPIPE046a_ALibraryProfileDeclinesPlatformMatrixThroughTheRealScheduler
// proves examineStructureWithProfile — the one real implementation
// examineStructure itself now delegates to (PIPE-046a) — actually consults
// the run's declared profile, using the exact production stage lists
// (examineStructureUnconditionalStages), the exact production scheduler
// (pipeline.RestrictToStages/RunConcurrently), and a real pipelineLedger
// backed by real SQLite, not a hand-rolled model of any of them.
//
// This is the trap PIPE-046a's own ticket names directly: a test that only
// drives ProfileDefault would show identical ledgers before and after this
// change, because ProfileDefault is provably inert
// (TestPIPE046_DefaultProfileNeverOverridesAnything in internal/pipeline) —
// that would prove the wiring compiles, not that it does anything. So this
// test runs the *same* worktree, through the *same* method, twice: once
// under pipeline.ProfileDefault and once under pipeline.ProfileLibrary, and
// compares the two runs' own recorded StagePlatformMatrix row.
//
// Discrimination: under ProfileDefault, checkPlatformMatrix actually shells
// out to `go test ./...` against the fixture module and records a real
// State (Satisfied, since the module builds and has no failing test) with a
// detail describing what ran. Under ProfileLibrary, StagePlatformMatrix is
// one of the two stages profiles.go's own ProfileLibrary declines — "no
// cross-compilation target is declared to answer for" — so this test
// requires the row instead be State Skipped with a detail naming the
// profile and its reason, proving the check was never invoked at all rather
// than invoked and happening to decline on its own. Reverting
// examineStructureWithProfile's declineProfiledStages call (passing the
// unconditional stage list straight to RestrictToStages, unfiltered) makes
// this test fail: both runs would then report the same real
// go-test-derived State for StagePlatformMatrix, and the ProfileLibrary
// assertions below would not hold.
func TestPIPE046a_ALibraryProfileDeclinesPlatformMatrixThroughTheRealScheduler(t *testing.T) {
	worktree := writeWorktree(t, map[string]string{
		"cmd/thing/main.go": "package main\n\nfunc main() {}\n",
	})

	runUnder := func(t *testing.T, profile pipeline.RunProfile) []storage.PipelineStageRecord {
		t.Helper()
		repositories, taskID := createPipelineStageGRPCFixture(t)
		execution := &AgentExecution{
			repositories: repositories,
			settings:     pipeline.DefaultSettings(),
		}
		ctx := context.Background()
		ledger := newPipelineLedger(ctx, repositories, taskID, domain.RunID{})
		scope := agentScope{taskID: taskID, worktree: worktree}

		execution.examineStructureWithProfile(ctx, ledger, scope, true, false, profile)

		records, err := repositories.ListPipelineStages(ctx, taskID, ledger.currentAttempt())
		if err != nil {
			t.Fatalf("read back the recorded ledger: %v", err)
		}
		return records
	}

	byStage := func(records []storage.PipelineStageRecord, stage pipeline.Number) (storage.PipelineStageRecord, bool) {
		for _, record := range records {
			if record.Stage == stage {
				return record, true
			}
		}
		return storage.PipelineStageRecord{}, false
	}

	defaultRecords := runUnder(t, pipeline.ProfileDefault)
	libraryRecords := runUnder(t, pipeline.ProfileLibrary)

	defaultPlatform, ok := byStage(defaultRecords, pipeline.StagePlatformMatrix)
	if !ok {
		t.Fatalf("ProfileDefault run recorded no platform-matrix row at all: %+v", defaultRecords)
	}
	if defaultPlatform.State == pipeline.StateSkipped &&
		strings.Contains(defaultPlatform.DetailRedacted, "declined in advance by run profile") {
		t.Fatalf("ProfileDefault (which ValidateProfiles requires to decline nothing) "+
			"recorded platform-matrix as profile-declined: %+v", defaultPlatform)
	}

	libraryPlatform, ok := byStage(libraryRecords, pipeline.StagePlatformMatrix)
	if !ok {
		t.Fatalf("ProfileLibrary run recorded no platform-matrix row at all: %+v", libraryRecords)
	}
	if libraryPlatform.State != pipeline.StateSkipped {
		t.Fatalf("ProfileLibrary platform-matrix state = %q, want %q (declined before the "+
			"scheduler could invoke its check): detail=%q",
			libraryPlatform.State, pipeline.StateSkipped, libraryPlatform.DetailRedacted)
	}
	if !strings.Contains(libraryPlatform.DetailRedacted, `run profile "library"`) {
		t.Errorf("library platform-matrix detail does not name the declaring profile: %q",
			libraryPlatform.DetailRedacted)
	}
	if !strings.Contains(libraryPlatform.DetailRedacted, "no cross-compilation target is declared") {
		t.Errorf("library platform-matrix detail does not carry the profile's own decline "+
			"reason: %q", libraryPlatform.DetailRedacted)
	}

	// The two runs must disagree specifically about platform-matrix, not
	// generally: a stage the library profile does not touch (contracts) is
	// expected to reach the same kind of real, non-profile-declined outcome
	// under both, proving the profile narrowed exactly what profiles.go says
	// it narrows and nothing else.
	defaultContracts, ok := byStage(defaultRecords, pipeline.StageContracts)
	if !ok {
		t.Fatalf("ProfileDefault run recorded no contracts row: %+v", defaultRecords)
	}
	libraryContracts, ok := byStage(libraryRecords, pipeline.StageContracts)
	if !ok {
		t.Fatalf("ProfileLibrary run recorded no contracts row: %+v", libraryRecords)
	}
	if strings.Contains(libraryContracts.DetailRedacted, "declined in advance by run profile") {
		t.Errorf("library profile declined contracts, which profiles.go does not list: %+v",
			libraryContracts)
	}
	if defaultContracts.State != libraryContracts.State {
		t.Errorf("contracts state differs between profiles (%q vs %q) though neither "+
			"profile declines it: %+v / %+v",
			defaultContracts.State, libraryContracts.State, defaultContracts, libraryContracts)
	}
}
