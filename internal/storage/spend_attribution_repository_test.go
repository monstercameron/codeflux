package storage

import (
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/pipeline"
)

// TestAttributeSpendSplitsCostAcrossAtomsMoleculesAndProgram is the
// load-bearing check for per-phase spend.
//
// The clock is driven rather than observed. Attribution reads the order of two
// independently written timestamps — the provider call and the stage record —
// so a test that let both take wall-clock values would pass or fail on how
// long SQLite happened to take between two writes.
//
// The fixture also pins the property the whole feature rests on: a call that no
// stage claimed is still in Total. A summary whose parts silently omit spend is
// worse than one that reports the gap, because the parts are what a person
// reads.
func TestAttributeSpendSplitsCostAcrossAtomsMoleculesAndProgram(t *testing.T) {
	repositories, task := createTaskFixture(t, 9600)
	base := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	clock := base
	repositories.now = func() time.Time { return clock }

	providerID, configuration, pricing := seedProviderRequestDependencies(
		t, repositories, 9601,
	)
	request := planAndStartProviderRequest(
		t, repositories, task.ID, providerID, configuration, pricing, 5,
	)
	usd := mustCurrencyCode(t, "USD")

	// Priced at one minor unit per three input tokens, so each call's cost is
	// checkable by hand rather than merely non-zero.
	call := func(name string, number uint64, at time.Time, input int64) {
		t.Helper()
		clock = at
		attempt := settleProviderAttemptForMetrics(
			t, repositories, request.ID, name, number, at,
		)
		clock = at
		appendMetricsAccounting(t, repositories, AppendProviderAttemptAccounting{
			ID: name + "-accounting", AttemptID: attempt.ID,
			Sequence: 1, Source: "provider-final",
			Usage: domain.TokenUsage{
				Known: true, Input: domain.TokenCount(input), Output: 1,
			},
			PricingRevisionID: &pricing.ID,
			Cost: &ExactMinorCost{
				Numerator: input, Denominator: 3, Currency: usd,
			},
			ProvenanceJSON: `{"source":"provider-final"}`,
		})
	}
	stage := func(at time.Time, number pipeline.Number) {
		t.Helper()
		clock = at
		if _, err := repositories.RecordPipelineStageResult(
			t.Context(),
			RecordPipelineStage{
				TaskID: task.ID, Attempt: 1, Stage: number,
				State:          pipeline.StateSatisfied,
				DetailRedacted: "fixture",
				Evidence:       map[string]any{"fixture": true},
			},
		); err != nil {
			t.Fatalf("record stage %d: %v", number, err)
		}
	}

	// Each call is followed by the stage that was running when it was made,
	// which is the ordering the approximation reads.
	call("spend-atom-call", 1, base.Add(1*time.Minute), 3)
	stage(base.Add(2*time.Minute), pipeline.StageAtoms)
	call("spend-program-call", 2, base.Add(3*time.Minute), 6)
	stage(base.Add(4*time.Minute), pipeline.StageProgram)
	// Made after the last stage was recorded, so no stage claims it.
	call("spend-orphan-call", 3, base.Add(5*time.Minute), 9)

	attribution, err := repositories.AttributeSpend(
		t.Context(), metricsWindowFixture(),
	)
	if err != nil {
		t.Fatalf("attribute spend: %v", err)
	}

	// Total covers every call, including the one no stage claimed.
	if attribution.Total.Calls != knownCount(3) {
		t.Errorf("total calls = %+v, want 3", attribution.Total.Calls)
	}
	if attribution.Total.InputTokens != knownCount(18) {
		t.Errorf("total input tokens = %+v, want 18", attribution.Total.InputTokens)
	}
	wantTotal := ExactMinorCost{Numerator: 6, Denominator: 1, Currency: usd}
	if attribution.Total.KnownCost != wantTotal || !attribution.Total.CostKnown {
		t.Errorf("total cost = %+v known=%v, want %+v known",
			attribution.Total.KnownCost, attribution.Total.CostKnown, wantTotal)
	}

	byPhase := map[pipeline.Phase]SpendSlice{}
	for _, phase := range attribution.ByPhase {
		byPhase[phase.Phase] = phase.Spend
	}
	atoms, found := byPhase[pipeline.PhaseAtoms]
	if !found {
		t.Fatalf("no atom-phase spend in %+v", attribution.ByPhase)
	}
	wantAtoms := ExactMinorCost{Numerator: 1, Denominator: 1, Currency: usd}
	if atoms.KnownCost != wantAtoms {
		t.Errorf("atom phase cost = %+v, want %+v", atoms.KnownCost, wantAtoms)
	}
	program, found := byPhase[pipeline.PhaseProgram]
	if !found {
		t.Fatalf("no program-phase spend in %+v", attribution.ByPhase)
	}
	wantProgram := ExactMinorCost{Numerator: 2, Denominator: 1, Currency: usd}
	if program.KnownCost != wantProgram {
		t.Errorf("program phase cost = %+v, want %+v",
			program.KnownCost, wantProgram)
	}
	if _, found := byPhase[pipeline.PhaseMolecules]; found {
		t.Error("a molecule phase was reported for a run with no molecule stage")
	}

	// The orphan call is reported, not dropped: the phases sum to four of the
	// six minor units and the missing two are named.
	wantOrphan := ExactMinorCost{Numerator: 3, Denominator: 1, Currency: usd}
	if attribution.Unattributed.KnownCost != wantOrphan {
		t.Errorf("unattributed cost = %+v, want %+v",
			attribution.Unattributed.KnownCost, wantOrphan)
	}
	if attribution.Unattributed.Calls != knownCount(1) {
		t.Errorf("unattributed calls = %+v, want 1", attribution.Unattributed.Calls)
	}

	// Stage detail carries the phase, so a reader can expand a phase without a
	// second lookup.
	stages := map[pipeline.Number]StageSpend{}
	for _, recorded := range attribution.ByStage {
		stages[recorded.Stage] = recorded
	}
	atomStage, found := stages[pipeline.StageAtoms]
	if !found {
		t.Fatalf("no stage-level atom spend in %+v", attribution.ByStage)
	}
	if atomStage.Phase != pipeline.PhaseAtoms || atomStage.Name != "atoms" {
		t.Errorf("atom stage = %+v, want the atoms stage of the atom phase",
			atomStage)
	}

	// The model is recorded on the request, so its attribution is exact rather
	// than approximated, and every call counts toward it.
	if len(attribution.ByModel) != 1 {
		t.Fatalf("models = %+v, want one", attribution.ByModel)
	}
	if attribution.ByModel[0].Model != pricing.ModelIdentifier {
		t.Errorf("model = %q, want %q",
			attribution.ByModel[0].Model, pricing.ModelIdentifier)
	}
	if attribution.ByModel[0].Spend.KnownCost != wantTotal {
		t.Errorf("model cost = %+v, want the full %+v",
			attribution.ByModel[0].Spend.KnownCost, wantTotal)
	}
}

// TestAttributeSpendReportsAnEmptyWindowAsZeroRatherThanUnknown proves a window
// with no provider call is an exact zero.
//
// No call means nothing was bought, which is a known figure. Reporting it as
// unknown would make an idle period indistinguishable from one whose costs
// could not be determined.
func TestAttributeSpendReportsAnEmptyWindowAsZeroRatherThanUnknown(t *testing.T) {
	repositories := openTestRepositories(t)
	attribution, err := repositories.AttributeSpend(
		t.Context(), metricsWindowFixture(),
	)
	if err != nil {
		t.Fatalf("attribute spend: %v", err)
	}
	if !attribution.Total.CostKnown {
		t.Error("an empty window reported unknown cost")
	}
	if attribution.Total.Calls != knownCount(0) {
		t.Errorf("calls = %+v, want a known 0", attribution.Total.Calls)
	}
	if len(attribution.ByPhase) != 0 || len(attribution.ByStage) != 0 ||
		len(attribution.ByModel) != 0 {
		t.Errorf("an empty window produced slices: %+v", attribution)
	}
	if _, err := repositories.AttributeSpend(
		t.Context(), MetricsWindow{},
	); err == nil {
		t.Error("an unbounded window was accepted")
	}
}
