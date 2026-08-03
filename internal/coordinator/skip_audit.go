package coordinator

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/pipeline"
	"codeflux.dev/codeflux/internal/storage"
)

// SkipAuditClassification names why one non-established stage did not
// establish anything, distinguishing a decision this run made on purpose
// from a gap nobody has closed yet (PIPE-042).
//
// The three values are the vocabulary internal/pipeline already carries
// through pipeline.StageCheck and pipeline.Section33Resolved rather than a
// second one invented here: MaySatisfy and Section33Resolved together say,
// for every stage, whether it can ever be satisfied and, when it cannot,
// whether that is because a real check declined it or because nothing was
// ever meant to check it at all.
type SkipAuditClassification string

const (
	// SkipPrincipledDecline means a real check exists for this stage, it may
	// satisfy in general, and it examined this run's own facts and
	// legitimately declined to claim satisfied. atom-optimization declining
	// because no rewrite is performed (PIPE-010) and atom-complexity
	// declining because nothing measures growth across input sizes
	// (PIPE-011) are both this, and so is any other stage this run itself
	// recorded skipped while its check remains one that could, on a
	// different run, have held.
	SkipPrincipledDecline SkipAuditClassification = "principled-decline"
	// SkipNoCheckByDesign means nothing performs this stage because nothing
	// is meant to: human-acceptance and deliver are decisions a run cannot
	// make for itself (PIPE-004), so the only honest outcomes for them are
	// blocked, skipped, or not-implemented, never satisfied.
	SkipNoCheckByDesign SkipAuditClassification = "no-check-by-design"
	// SkipUnimplemented means this run's own ledger recorded not-implemented,
	// or skipped with no governing check able to satisfy it — for this run,
	// nothing established anything for the stage, whether because the build
	// genuinely has no code for it yet or because this attempt never reached
	// it. It is the default: a stage falls here unless the static vocabulary
	// in pipeline.Checks names a reason it could not have done otherwise.
	SkipUnimplemented SkipAuditClassification = "unimplemented"
)

// SkipAuditEntry is one stage this run did not establish anything for.
type SkipAuditEntry struct {
	Stage          pipeline.Number
	Name           string
	State          pipeline.State // pipeline.StateSkipped or pipeline.StateNotImplemented
	Classification SkipAuditClassification
	// Reason is the governing PIPE ticket, prefixed to this run's own
	// recorded detail, when pipeline.Checks or pipeline.Section33Resolved
	// names one; otherwise it is this run's own recorded detail alone, so a
	// decline always carries why.
	Reason string
}

// SkipAuditResult is what PIPE-042's audit found in one run's own ledger.
//
// It is the shape PIPE-044 reads to show the headline figure and its
// breakdown without recomputing anything: every count is already derived
// from the classification this type carries, and Entries already states a
// classification and reason per stage, so a presentation layer needs no
// access to pipeline.Checks or pipeline.Section33Resolved itself.
type SkipAuditResult struct {
	// TotalStages is how many stages this run recorded any state for,
	// excluding the audit's own stage — it cannot yet know its own outcome
	// while computing it (requirement 4).
	TotalStages int
	// Satisfied, Failed, Blocked, Skipped, and NotImplemented partition
	// TotalStages by pipeline.State. Skipped and NotImplemented are the
	// states this audit challenges; Satisfied is what established
	// something; Failed and Blocked are neither a decline nor a gap — a
	// failed check ran and produced a real negative result, and a blocked
	// stage was never given a fair chance by something upstream — so
	// neither is folded into the skip ratio or classified in Entries.
	Satisfied      int
	Failed         int
	Blocked        int
	Skipped        int
	NotImplemented int
	// SkipRatio is the headline figure: the fraction of TotalStages that
	// recorded skipped or not-implemented, i.e. established nothing.
	SkipRatio float64
	// Entries classifies every stage recorded skipped or not-implemented.
	// Its length equals Skipped + NotImplemented, ordered by stage number.
	Entries []SkipAuditEntry
}

// errSkipAuditNothingRecorded is returned when a ledger carries no stage
// besides the audit's own, so no ratio can honestly be computed.
var errSkipAuditNothingRecorded = errors.New(
	"this run's ledger carries no stage besides the audit itself, so no " +
		"skip ratio can be computed")

// ComputeSkipAudit classifies every stage in records other than self and
// reports the fraction that established nothing (PIPE-042).
//
// It is pure over its arguments — a slice of already-fetched records and the
// audit's own stage number — rather than reading storage itself, so a test
// can drive every classification without a database and so the wire shape
// PIPE-044 needs exists independently of how the records were fetched.
// self is excluded from every count because the audit is deciding its own
// outcome as it runs and by definition has not recorded one yet; a caller
// that fetches records after the audit's own row exists would otherwise
// count the audit auditing itself.
//
// It returns an error, rather than a zero-valued result that looks like a
// perfect run, when records carries nothing else to audit — the vacuity
// guard requirement 4 asks for. checkSkipAudit turns that error into a
// skipped stage outcome rather than a satisfied one.
func ComputeSkipAudit(
	records []storage.PipelineStageRecord,
	self pipeline.Number,
) (SkipAuditResult, error) {
	var result SkipAuditResult
	for _, record := range records {
		if record.Stage == self {
			continue
		}
		result.TotalStages++
		switch record.State {
		case pipeline.StateSatisfied:
			result.Satisfied++
		case pipeline.StateFailed:
			result.Failed++
		case pipeline.StateBlocked:
			result.Blocked++
		case pipeline.StateSkipped:
			result.Skipped++
			result.Entries = append(result.Entries, classifySkipAuditEntry(record))
		case pipeline.StateNotImplemented:
			result.NotImplemented++
			result.Entries = append(result.Entries, classifySkipAuditEntry(record))
		}
	}
	if result.TotalStages == 0 {
		return SkipAuditResult{}, errSkipAuditNothingRecorded
	}
	result.SkipRatio = float64(result.Skipped+result.NotImplemented) /
		float64(result.TotalStages)
	sort.Slice(result.Entries, func(first, second int) bool {
		return result.Entries[first].Stage < result.Entries[second].Stage
	})
	return result, nil
}

// classifySkipAuditEntry decides why one skipped or not-implemented stage
// established nothing, using only pipeline.Checks and
// pipeline.Section33Resolved — the same static vocabulary PIPE-004,
// PIPE-010, and PIPE-011 already carry — rather than inventing a second
// classification or guessing at this run's intent from prose (PIPE-042).
//
// The rule is about what CAN ever be true of the stage, combined with what
// this run's own ledger says happened to it:
//
//   - a stage nothing may ever satisfy (MaySatisfy false) either has a real
//     check that legitimately cannot claim — named in Section33Resolved,
//     which makes it a principled decline — or has no check at all, which is
//     no-check-by-design (human-acceptance, deliver);
//   - a stage a check CAN satisfy that this run recorded skipped is a
//     principled decline: the check ran, examined this run's own facts, and
//     said so, exactly PIPE-010's and PIPE-011's shape and every other
//     legitimate runtime skip (atom-fuzz with no boundary, non-functional on
//     a first run with nothing to compare against);
//   - the same stage recorded not-implemented is unimplemented: for this
//     run, nothing established anything for it, whether because the build
//     genuinely has no code for it or because this attempt never reached it.
func classifySkipAuditEntry(record storage.PipelineStageRecord) SkipAuditEntry {
	entry := SkipAuditEntry{
		Stage: record.Stage, Name: record.Name, State: record.State,
		Classification: SkipUnimplemented,
		Reason:         record.DetailRedacted,
	}
	check, bound := pipeline.CheckFor(record.Stage)
	if !bound {
		return entry
	}
	if !check.MaySatisfy {
		if ticket, resolved := pipeline.Section33Resolved[record.Stage]; resolved {
			entry.Classification = SkipPrincipledDecline
			entry.Reason = ticket + ": " + record.DetailRedacted
		} else {
			entry.Classification = SkipNoCheckByDesign
			entry.Reason = "no check performs this stage by design: " + record.DetailRedacted
		}
		return entry
	}
	if record.State == pipeline.StateSkipped {
		entry.Classification = SkipPrincipledDecline
		if check.Unestablished != "" {
			entry.Reason = check.Unestablished + ": " + record.DetailRedacted
		}
	}
	return entry
}

// checkSkipAudit is StageSkipAudit's performer (PIPE-042).
//
// It reads this run's own ledger, through the same PipelineStageLedgerStore
// seam PIPE-006b opened for the wire, rather than reasoning from the flow
// declaration — the question it answers is what this run did, not what the
// product could in principle do.
//
// It must not satisfy vacuously (requirement 4): a nil store, a read that
// fails, or a ledger that carries no stage besides the audit itself all
// record skipped rather than a ratio computed from nothing, the same
// fallback checkSimplification (PIPE-010) and checkComplexity (PIPE-011) use
// when their own gate cannot be established.
func checkSkipAudit(
	ctx context.Context,
	store PipelineStageLedgerStore,
	taskID domain.TaskID,
	attempt uint64,
) stageOutcome {
	if store == nil {
		return skipped("no pipeline stage ledger is attached, so no skip " +
			"ratio can be computed")
	}
	records, err := store.ListPipelineStages(ctx, taskID, attempt)
	if err != nil {
		return skipped(fmt.Sprintf(
			"this run's ledger could not be read, so no skip ratio can be "+
				"computed: %s", err.Error()))
	}
	result, computeErr := ComputeSkipAudit(records, pipeline.StageSkipAudit)
	if computeErr != nil {
		return skipped(computeErr.Error())
	}
	return held(skipAuditDetail(result), skipAuditEvidence(result))
}

// skipAuditDetail renders the headline figure and its breakdown for a
// person reading the ledger row directly.
func skipAuditDetail(result SkipAuditResult) string {
	return fmt.Sprintf(
		"%d of %d stage(s) established nothing (%.0f%% skip ratio): "+
			"%d principled decline(s), %d stage(s) with no check by design, "+
			"%d unimplemented",
		result.Skipped+result.NotImplemented, result.TotalStages,
		result.SkipRatio*100,
		countSkipAuditClassification(result.Entries, SkipPrincipledDecline),
		countSkipAuditClassification(result.Entries, SkipNoCheckByDesign),
		countSkipAuditClassification(result.Entries, SkipUnimplemented))
}

// countSkipAuditClassification counts entries carrying one classification.
func countSkipAuditClassification(
	entries []SkipAuditEntry,
	classification SkipAuditClassification,
) int {
	count := 0
	for _, entry := range entries {
		if entry.Classification == classification {
			count++
		}
	}
	return count
}

// skipAuditEvidence renders a SkipAuditResult into the ledger's evidence
// vocabulary (PIPE-042).
//
// This is the shape PIPE-044 reads: the headline (skip_ratio, and the counts
// behind it) alongside a fully classified, per-stage breakdown, so a
// presentation layer can show both without recomputing anything from
// pipeline.Checks or pipeline.Section33Resolved itself. Keys are named to
// match the ledger's own snake_case evidence convention (see
// nonFunctionalTolerance's evidence in agent_stage_checks.go) rather than
// the Go field names on SkipAuditResult.
func skipAuditEvidence(result SkipAuditResult) map[string]any {
	entries := make([]map[string]any, 0, len(result.Entries))
	for _, entry := range result.Entries {
		entries = append(entries, map[string]any{
			"stage":          int(entry.Stage),
			"name":           entry.Name,
			"state":          string(entry.State),
			"classification": string(entry.Classification),
			"reason":         entry.Reason,
		})
	}
	return map[string]any{
		"total_stages":       result.TotalStages,
		"satisfied":          result.Satisfied,
		"failed":             result.Failed,
		"blocked":            result.Blocked,
		"skipped":            result.Skipped,
		"not_implemented":    result.NotImplemented,
		"skip_ratio":         result.SkipRatio,
		"principled_decline": countSkipAuditClassification(result.Entries, SkipPrincipledDecline),
		"no_check_by_design": countSkipAuditClassification(result.Entries, SkipNoCheckByDesign),
		"unimplemented":      countSkipAuditClassification(result.Entries, SkipUnimplemented),
		"entries":            entries,
	}
}
