package pipelineledger

import (
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/pipeline"
)

// rowsFromFlow builds one StageRow per stage in pipeline.Flow, all satisfied
// with a measured, ordered elapsed span, so tests that only care about a
// handful of exceptional rows do not have to hand-write all thirty-nine.
func rowsFromFlow(t *testing.T) []StageRow {
	t.Helper()
	base := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	rows := make([]StageRow, 0, len(pipeline.Flow))
	for index, stage := range pipeline.Flow {
		rows = append(rows, StageRow{
			Number: stage.Number, Name: stage.Name, State: pipeline.StateSatisfied,
			FinishedAt:      base.Add(time.Duration(index+1) * time.Second),
			ElapsedMeasured: true, Elapsed: time.Second,
		})
	}
	return rows
}

func setState(rows []StageRow, stage pipeline.Number, state pipeline.State) []StageRow {
	out := append([]StageRow(nil), rows...)
	for index := range out {
		if out[index].Number == stage {
			out[index].State = state
		}
	}
	return out
}

// TestComputeSkipSummaryMatchesServerRatioArithmetic pins the ratio formula
// to ComputeSkipAudit's own: (skipped+not-implemented)/total, excluding the
// skip-audit stage's own row from the denominator.
func TestComputeSkipSummaryMatchesServerRatioArithmetic(t *testing.T) {
	rows := rowsFromFlow(t)
	rows = setState(rows, pipeline.StageAtomOptimization, pipeline.StateSkipped)
	rows = setState(rows, pipeline.StagePlatformMatrix, pipeline.StateNotImplemented)

	summary := ComputeSkipSummary(rows)
	if !summary.Computed {
		t.Fatal("summary should be computed when rows carry real stages")
	}
	total := len(pipeline.Flow) - 1 // the skip-audit row itself is excluded
	if summary.TotalStages != total {
		t.Errorf("TotalStages = %d, want %d", summary.TotalStages, total)
	}
	if summary.Skipped != 1 || summary.NotImplemented != 1 {
		t.Errorf("Skipped=%d NotImplemented=%d, want 1 and 1", summary.Skipped, summary.NotImplemented)
	}
	wantRatio := float64(2) / float64(total)
	if summary.SkipRatio != wantRatio {
		t.Errorf("SkipRatio = %v, want %v", summary.SkipRatio, wantRatio)
	}
}

// TestComputeSkipSummaryRefusesVacuousInput keeps a ledger with nothing but
// the skip-audit's own row from reading as a perfect run: Computed must stay
// false rather than reporting a fabricated zero-percent ratio.
func TestComputeSkipSummaryRefusesVacuousInput(t *testing.T) {
	onlyAudit := []StageRow{{Number: pipeline.StageSkipAudit, Name: "skip-audit", State: pipeline.StateSatisfied}}
	summary := ComputeSkipSummary(onlyAudit)
	if summary.Computed {
		t.Fatal("a ledger with only the audit's own row must not compute a ratio")
	}
	if summary.SkipRatio != 0 || summary.TotalStages != 0 {
		t.Errorf("vacuous summary should be the zero value, got %+v", summary)
	}

	empty := ComputeSkipSummary(nil)
	if empty.Computed {
		t.Fatal("an empty ledger must not compute a ratio")
	}
}

// TestClassifyDeclineDistinguishesPrincipledDeclineFromUnimplemented pins the
// three-way split PIPE-044 requires stay visible: a stage that resolved
// section 33 as a principled decline must not read the same as a stage that
// simply has not been built.
func TestClassifyDeclineDistinguishesPrincipledDeclineFromUnimplemented(t *testing.T) {
	rows := rowsFromFlow(t)
	// atom-optimization is named in pipeline.Section33Resolved (PIPE-010): a
	// real check exists, examined this run, and honestly declined.
	skipped := setState(rows, pipeline.StageAtomOptimization, pipeline.StateSkipped)
	summary := ComputeSkipSummary(skipped)
	decline := findDecline(t, summary, pipeline.StageAtomOptimization)
	if decline.Classification != ClassificationPrincipledDecline {
		t.Errorf("atom-optimization skipped = %v, want principled-decline", decline.Classification)
	}
	if decline.Ticket != "PIPE-010" {
		t.Errorf("atom-optimization ticket = %q, want PIPE-010", decline.Ticket)
	}

	// A stage whose check may satisfy and is not in Section33Resolved, but
	// this run recorded not-implemented: nothing established anything for it.
	// atom-fuzz (StageAtomFuzz) is not part of Section33Resolved.
	notBuilt := setState(rows, pipeline.StageAtomFuzz, pipeline.StateNotImplemented)
	summary = ComputeSkipSummary(notBuilt)
	decline = findDecline(t, summary, pipeline.StageAtomFuzz)
	if decline.Classification != ClassificationUnimplemented {
		t.Errorf("atom-fuzz not-implemented = %v, want unimplemented", decline.Classification)
	}
	if decline.Ticket != "" {
		t.Errorf("unimplemented stage should carry no ticket, got %q", decline.Ticket)
	}
}

// TestClassifyDeclineNamesNoCheckByDesignStages covers a stage nothing is
// meant to check at all -- human-acceptance and deliver are decisions a run
// cannot make for itself -- recorded skipped or not-implemented rather than
// its usual blocked, which is the fallback classifyDecline documents.
func TestClassifyDeclineNamesNoCheckByDesignStages(t *testing.T) {
	rows := rowsFromFlow(t)
	rows = setState(rows, pipeline.StageHumanAcceptance, pipeline.StateSkipped)
	summary := ComputeSkipSummary(rows)
	decline := findDecline(t, summary, pipeline.StageHumanAcceptance)
	if decline.Classification != ClassificationNoCheckByDesign {
		t.Errorf("human-acceptance skipped = %v, want no-check-by-design", decline.Classification)
	}
}

func findDecline(t *testing.T, summary SkipSummary, stage pipeline.Number) ClassifiedStage {
	t.Helper()
	for _, decline := range summary.Declines {
		if decline.Stage.Number == stage {
			return decline
		}
	}
	t.Fatalf("no decline recorded for stage %d", stage)
	return ClassifiedStage{}
}

// TestSkipSummaryCountsStayDistinct is the arithmetic form of the UI
// requirement: the three classification counts must sum to Declines and must
// never be read as a single collapsed number.
func TestSkipSummaryCountsStayDistinct(t *testing.T) {
	rows := rowsFromFlow(t)
	rows = setState(rows, pipeline.StageAtomOptimization, pipeline.StateSkipped) // principled decline
	rows = setState(rows, pipeline.StageHumanAcceptance, pipeline.StateSkipped)  // no-check-by-design
	rows = setState(rows, pipeline.StageAtomFuzz, pipeline.StateNotImplemented)  // unimplemented
	summary := ComputeSkipSummary(rows)
	if got := summary.PrincipledDeclineCount(); got != 1 {
		t.Errorf("PrincipledDeclineCount = %d, want 1", got)
	}
	if got := summary.NoCheckByDesignCount(); got != 1 {
		t.Errorf("NoCheckByDesignCount = %d, want 1", got)
	}
	if got := summary.UnimplementedCount(); got != 1 {
		t.Errorf("UnimplementedCount = %d, want 1", got)
	}
	if len(summary.Declines) != 3 {
		t.Errorf("len(Declines) = %d, want 3", len(summary.Declines))
	}
}
