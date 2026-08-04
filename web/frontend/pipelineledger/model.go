// Package pipelineledger renders the 38-stage pipeline ledger PIPE-006b
// exposed over PipelineStageService.ListPipelineStages: what a task attempt's
// delivery flow actually established, stage by stage, how long each stage
// took to reach, and what fraction of the flow established nothing at all
// (PIPE-006a, PIPE-044).
//
// It owns presentation only. The ledger rows themselves remain authoritative
// in SQLite and are read here exactly as the wire reports them; this package
// classifies and formats, it does not decide anything about a run.
package pipelineledger

import (
	"time"

	"codeflux.dev/codeflux/internal/pipeline"
	"codeflux.dev/codeflux/web/frontend/primitives"
)

// LoadState is what this surface currently knows about the ledger it was
// asked to show.
//
// "Unavailable" is distinct from "empty": a task with no attempt started yet
// and a ledger read that failed both end up with zero rows, and only one of
// those is a problem a reader can act on. Kept local to this package rather
// than imported from web/frontend/shell, which owns an equivalent vocabulary
// for its own surfaces, so this package has no build-time dependency on a
// shell package another lane is actively editing.
type LoadState string

const (
	// Unavailable means there is no task attempt to read: nothing is selected,
	// so there is nothing to succeed or fail at.
	Unavailable LoadState = "unavailable"
	Loading     LoadState = "loading"
	Ready       LoadState = "ready"
	Failed      LoadState = "failed"
)

// StageRow is one recorded stage exactly as PipelineStageView reports it.
//
// ElapsedMeasured must be read alongside Elapsed: an untracked stage's
// StartedAt defaults to its FinishedAt, which the wire deliberately reports
// as an absent Elapsed rather than an explicit zero, so a zero Elapsed here
// with ElapsedMeasured false means "nobody timed this," not "this took no
// time" (PIPE-006b).
type StageRow struct {
	Number          pipeline.Number
	Name            string
	State           pipeline.State
	FinishedAt      time.Time
	ElapsedMeasured bool
	Elapsed         time.Duration
}

// Classification names why one stage that established nothing did not,
// reusing the exact vocabulary internal/coordinator's skip audit (PIPE-042)
// already defines rather than inventing a second one for the browser.
//
// It is derived here from internal/pipeline's own static tables
// (pipeline.CheckFor, pipeline.Section33Resolved) plus the wire's own state
// string, which is the same input classifySkipAuditEntry uses for every case
// except the free-text run detail that PipelineStageView deliberately does
// not carry (see that message's own doc comment: it excludes "the stage's
// gate text or evidence payload" by design). Ticket names the governing
// PIPE-* ticket where PrincipledDecline; it is empty otherwise.
type Classification string

const (
	// ClassificationEstablished means the stage recorded satisfied.
	ClassificationEstablished Classification = "established"
	// ClassificationFailed means a real check ran and did not hold. Failing is
	// neither a decline nor a gap, so it is counted but never folded into the
	// skip ratio.
	ClassificationFailed Classification = "failed"
	// ClassificationBlocked means the stage never got a fair chance because
	// something upstream did not happen. Also excluded from the skip ratio.
	ClassificationBlocked Classification = "blocked"
	// ClassificationPrincipledDecline means a real check exists, may in
	// general satisfy, and this run's own facts led it to decline honestly.
	ClassificationPrincipledDecline Classification = "principled-decline"
	// ClassificationNoCheckByDesign means nothing performs this stage because
	// nothing is meant to (human-acceptance, deliver): the only honest
	// outcomes for it are blocked, skipped, or not-implemented, never
	// satisfied.
	ClassificationNoCheckByDesign Classification = "no-check-by-design"
	// ClassificationUnimplemented means, for this run, nothing established
	// anything for the stage: no governing check declines it on principle, so
	// either the build has no code for it yet or this attempt never reached
	// it.
	ClassificationUnimplemented Classification = "unimplemented"
	// ClassificationDeclinedByProfile means the run's declared
	// pipeline.RunProfile marked this stage inapplicable before the run
	// started (PIPE-046a), which takes priority over the other three
	// classifications the same way it does server-side in
	// internal/coordinator's classifySkipAuditEntry.
	ClassificationDeclinedByProfile Classification = "declined-by-profile"
)

// Label returns the short human word for a classification, used everywhere
// this package shows one so the three PIPE-042 categories are never
// paraphrased two different ways on two different screens.
func (classification Classification) Label() string {
	switch classification {
	case ClassificationEstablished:
		return "Established"
	case ClassificationFailed:
		return "Failed"
	case ClassificationBlocked:
		return "Blocked"
	case ClassificationPrincipledDecline:
		return "Declined on principle"
	case ClassificationNoCheckByDesign:
		return "No check by design"
	case ClassificationUnimplemented:
		return "Not implemented"
	case ClassificationDeclinedByProfile:
		return "Declined by profile"
	default:
		return "Unknown"
	}
}

// ClassifiedStage is one stage that established nothing, with why.
type ClassifiedStage struct {
	Stage          StageRow
	Classification Classification
	// Ticket names the PIPE-* ticket that resolved this stage as a principled
	// decline, when pipeline.Section33Resolved names one. When Classification
	// is ClassificationDeclinedByProfile, it instead carries the profile's own
	// one-sentence decline reason (PIPE-046a) -- there is no governing ticket
	// for a decision the run's profile made, only the profile's stated reason.
	// Empty otherwise.
	Ticket string
}

// SkipSummary is the headline PIPE-044 asks for, computed from a task
// attempt's own recorded rows exactly as internal/coordinator's ComputeSkipAudit
// computes it server-side: TotalStages and the five state counts come only
// from each row's own State, so this arithmetic cannot disagree with the
// server's own ratio for the same rows. It cannot recover the free-text
// detail a skip carried at run time -- that lives only in the ledger's own
// evidence payload, which PipelineStageView does not expose -- so Declines
// carries a classification and a governing ticket where one applies, not a
// narrative reason.
type SkipSummary struct {
	// Computed is false when there were no rows to audit (no attempt has
	// recorded anything yet, or the skip-audit stage's own row is the only
	// one present). A summary with Computed false must not be read as a
	// perfect run: it is the same vacuity this surface refuses to paper over
	// with a fabricated zero-percent ratio.
	Computed bool
	// TotalStages excludes the skip-audit stage's own row, matching
	// ComputeSkipAudit: it is deciding its own outcome while it runs.
	TotalStages    int
	Satisfied      int
	Failed         int
	Blocked        int
	Skipped        int
	NotImplemented int
	// SkipRatio is the fraction of TotalStages that established nothing.
	SkipRatio float64
	// Declines classifies every skipped or not-implemented row, ordered by
	// stage number, so a reader can move from the headline ratio straight to
	// which stages it is made of without a second read.
	Declines []ClassifiedStage
}

// PrincipledDeclineCount, NoCheckByDesignCount, UnimplementedCount, and
// DeclinedByProfileCount are the four counts PIPE-044/PIPE-046a require stay
// visibly distinct rather than collapsing into the single skip ratio.
func (summary SkipSummary) PrincipledDeclineCount() int {
	return summary.countClassification(ClassificationPrincipledDecline)
}

func (summary SkipSummary) NoCheckByDesignCount() int {
	return summary.countClassification(ClassificationNoCheckByDesign)
}

func (summary SkipSummary) UnimplementedCount() int {
	return summary.countClassification(ClassificationUnimplemented)
}

func (summary SkipSummary) DeclinedByProfileCount() int {
	return summary.countClassification(ClassificationDeclinedByProfile)
}

func (summary SkipSummary) countClassification(classification Classification) int {
	count := 0
	for _, decline := range summary.Declines {
		if decline.Classification == classification {
			count++
		}
	}
	return count
}

// ComputeSkipSummary derives the headline and its per-stage breakdown from
// one task attempt's rows, in the stage order the wire already returns them,
// classifying under the flow's own default profile.
//
// PipelineStageView does not carry the run's declared profile on the wire
// today (see docs/plan.md's "Integration status" note under "Declared Run
// Profiles"), so every caller reachable from a real mounted page still goes
// through this default-profile entry point; ComputeSkipSummaryWithProfile is
// the one real implementation (PIPE-046a), kept reachable so a caller that
// does carry a profile -- and a test proving the classification actually
// changes under one -- can use it without waiting on that wire change.
func ComputeSkipSummary(rows []StageRow) SkipSummary {
	return ComputeSkipSummaryWithProfile(rows, pipeline.ProfileDefault)
}

// ComputeSkipSummaryWithProfile is ComputeSkipSummary, extended with the
// run's declared profile (PIPE-046a): a row the profile declined classifies
// as ClassificationDeclinedByProfile, the same distinction
// classifySkipAuditEntry draws server-side, computed here from the row's
// wire-carried Number and State plus the caller-supplied profile rather than
// a profile field the wire does not yet carry.
//
// It excludes the skip-audit stage's own row from every count, mirroring
// ComputeSkipAudit's self-exclusion, and it returns Computed false rather
// than a zero-valued summary that reads as a perfect run when rows carries
// nothing else to audit.
func ComputeSkipSummaryWithProfile(rows []StageRow, profile pipeline.RunProfile) SkipSummary {
	var summary SkipSummary
	for _, row := range rows {
		if row.Number == pipeline.StageSkipAudit {
			continue
		}
		summary.TotalStages++
		switch row.State {
		case pipeline.StateSatisfied:
			summary.Satisfied++
		case pipeline.StateFailed:
			summary.Failed++
		case pipeline.StateBlocked:
			summary.Blocked++
		case pipeline.StateSkipped:
			summary.Skipped++
			summary.Declines = append(summary.Declines, classifyDecline(row, profile))
		case pipeline.StateNotImplemented:
			summary.NotImplemented++
			summary.Declines = append(summary.Declines, classifyDecline(row, profile))
		}
	}
	if summary.TotalStages == 0 {
		return SkipSummary{}
	}
	summary.Computed = true
	summary.SkipRatio = float64(summary.Skipped+summary.NotImplemented) / float64(summary.TotalStages)
	return summary
}

// classifyDecline decides why one skipped or not-implemented row established
// nothing, using internal/pipeline's static vocabulary and the run's declared
// profile -- the same tables and the same profile classifySkipAuditEntry
// reads server-side -- combined with this row's own wire-reported state. See
// Classification's doc comment for what it cannot recover: the run's own
// free-text detail, which stays server-side.
func classifyDecline(row StageRow, profile pipeline.RunProfile) ClassifiedStage {
	class, ticket := pipeline.ClassifyDeclineForProfile(row.Number, row.State, profile)
	return ClassifiedStage{
		Stage:          row,
		Classification: Classification(class),
		Ticket:         ticket,
	}
}

// Props drives LedgerCard.
type Props struct {
	Mode         primitives.Mode
	State        LoadState
	ErrorMessage string
	Attempt      uint64
	Rows         []StageRow
	OnRetry      func()
}

// stateOrUnavailable normalizes an unset zero-valued LoadState to Unavailable
// so every switch in this package has one closed set of four cases to handle
// rather than a fifth implicit "empty string" case.
func stateOrUnavailable(value LoadState) LoadState {
	if value == "" {
		return Unavailable
	}
	return value
}
