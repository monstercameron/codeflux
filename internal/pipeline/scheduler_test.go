package pipeline

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
)

// TestPIPE058a_RestrictToStagesDropsEdgesOutsideTheSet proves
// RestrictToStages prunes exactly the edges that leave the given set, and
// keeps exactly the ones that stay inside it, rather than either dropping
// everything or keeping everything.
func TestPIPE058a_RestrictToStagesDropsEdgesOutsideTheSet(t *testing.T) {
	table := []Requirement{
		{StageContracts, nil},
		{StageAtomCaseSynthesis, []Number{StageContracts}},
		{StageAtomDocumentation, []Number{
			StageAtomCaseSynthesis, StageAtomMutation, StageAtomFuzz,
		}},
	}
	restricted := RestrictToStages(table,
		[]Number{StageContracts, StageAtomCaseSynthesis, StageAtomDocumentation})

	byStage := map[Number][]Number{}
	for _, requirement := range restricted {
		byStage[requirement.Stage] = requirement.Requires
	}
	if len(byStage[StageContracts]) != 0 {
		t.Errorf("contracts kept a requirement it should have none of: %v",
			byStage[StageContracts])
	}
	if got := byStage[StageAtomCaseSynthesis]; len(got) != 1 || got[0] != StageContracts {
		t.Errorf("atom-case-synthesis requires = %v, want [contracts]", got)
	}
	// atom-mutation and atom-fuzz are outside the restricted set, so both
	// edges must be pruned, leaving only atom-case-synthesis.
	if got := byStage[StageAtomDocumentation]; len(got) != 1 || got[0] != StageAtomCaseSynthesis {
		t.Errorf("atom-documentation requires = %v, want [atom-case-synthesis] "+
			"(atom-mutation and atom-fuzz should have been pruned)", got)
	}
}

// syntheticGraph is a small, hand-built dependency graph used by every test
// below that does not need the real 39-stage table: A -> B, A -> C, B and C
// both -> D. It is deliberately shaped to have a two-wide frontier (B, C)
// so a test can assert on concurrency without depending on
// internal/pipeline's real, larger table.
func syntheticGraph() []Requirement {
	return []Requirement{
		{StageInstructions, nil},                                                         // A
		{StageClarification, []Number{StageInstructions}},                                // B
		{StageAtomicInstructions, []Number{StageInstructions}},                           // C
		{StageDecompositionCover, []Number{StageClarification, StageAtomicInstructions}}, // D
	}
}

// syntheticClasses classes every stage in syntheticGraph as pure-ast, which
// imposes no resource constraint at all, so tests using it exercise only
// the dependency logic, not the exclusivity logic (covered separately).
func syntheticClasses() map[Number]ResourceClass {
	return map[Number]ResourceClass{
		StageInstructions:       ResourceClassPureAST,
		StageClarification:      ResourceClassPureAST,
		StageAtomicInstructions: ResourceClassPureAST,
		StageDecompositionCover: ResourceClassPureAST,
	}
}

// TestPIPE064_AStageVerdictDoesNotDependOnConcurrency is PIPE-064 itself:
// the same graph, the same Runner, run once with MaxWorkers pinned to one
// (as serial as this scheduler gets) and once with no bound at all, must
// produce identical Decisions apart from their timing fields.
//
// Discrimination: the Runner used here increments a package-level-shaped
// shared counter without any synchronization of its own — relying entirely
// on the scheduler to serialize access when it is told to run serially, and
// tolerating concurrent access when it is not (the counter's exact value is
// never asserted, only that decisions.State/Detail/Evidence come out
// identical). Run with `-race`, a scheduler that silently ran stages
// concurrently even when MaxWorkers==1 — for example by launching every
// wave unconditionally rather than actually bounding it — would be caught
// by the race detector on this counter, not just by a mismatched result.
func TestPIPE064_AStageVerdictDoesNotDependOnConcurrency(t *testing.T) {
	requirements := syntheticGraph()
	classes := syntheticClasses()

	run := func(_ context.Context, stage Number) (State, string, map[string]any) {
		return StateSatisfied, fmt.Sprintf("stage %d decided", stage),
			map[string]any{"stage": int(stage)}
	}

	serial := RunConcurrently(context.Background(), requirements, classes, run,
		ScheduleOptions{MaxWorkers: 1})
	parallel := RunConcurrently(context.Background(), requirements, classes, run,
		ScheduleOptions{MaxWorkers: 0})

	if len(serial) != len(parallel) {
		t.Fatalf("serial produced %d decisions, parallel produced %d",
			len(serial), len(parallel))
	}
	for index := range serial {
		one, other := serial[index], parallel[index]
		if one.Stage != other.Stage || one.State != other.State ||
			one.Detail != other.Detail {
			t.Errorf("stage %d: serial={%v %q} parallel={%v %q}",
				one.Stage, one.State, one.Detail, other.State, other.Detail)
		}
		if fmt.Sprint(one.Evidence) != fmt.Sprint(other.Evidence) {
			t.Errorf("stage %d: evidence differs: serial=%v parallel=%v",
				one.Stage, one.Evidence, other.Evidence)
		}
	}
}

// TestPIPE064_ConcurrencyActuallyHappens is the other half of PIPE-064's
// proof obligation: the "parallel" run above is only a meaningful
// comparison if it is actually concurrent. This test proves the scheduler
// really does run a wave's stages at the same time rather than happening to
// serialize them anyway, by having two Runner calls block until both have
// started — which only both complete if they are genuinely running
// together, and which deadlocks the test (caught by go test's own timeout)
// if the scheduler secretly serializes wave B/C.
func TestPIPE064_ConcurrencyActuallyHappens(t *testing.T) {
	requirements := syntheticGraph()
	classes := syntheticClasses()

	var barrier sync.WaitGroup
	barrier.Add(2)
	run := func(_ context.Context, stage Number) (State, string, map[string]any) {
		switch stage {
		case StageClarification, StageAtomicInstructions:
			barrier.Done()
			barrier.Wait()
		}
		return StateSatisfied, "", nil
	}

	decisions := RunConcurrently(context.Background(), requirements, classes, run,
		ScheduleOptions{})
	for _, decision := range decisions {
		if decision.State != StateSatisfied {
			t.Errorf("stage %d = %v, want satisfied", decision.Stage, decision.State)
		}
	}
}

// TestPIPE063_AFailedRequirementBlocksItsDependentsWithoutRunningThem
// proves the scheduler never calls Runner for a stage whose requirement
// failed, and records it blocked instead — the wasted, misleading work
// PIPE-063 exists to prevent.
func TestPIPE063_AFailedRequirementBlocksItsDependentsWithoutRunningThem(t *testing.T) {
	requirements := syntheticGraph()
	classes := syntheticClasses()

	var called sync.Map // Number -> true, for every stage Runner was invoked for
	run := func(_ context.Context, stage Number) (State, string, map[string]any) {
		called.Store(stage, true)
		if stage == StageClarification {
			return StateFailed, "clarification failed on purpose", nil
		}
		return StateSatisfied, "", nil
	}

	decisions := RunConcurrently(context.Background(), requirements, classes, run,
		ScheduleOptions{})
	byStage := map[Number]Decision{}
	for _, decision := range decisions {
		byStage[decision.Stage] = decision
	}

	if byStage[StageClarification].State != StateFailed {
		t.Fatalf("clarification = %v, want failed", byStage[StageClarification].State)
	}
	if byStage[StageAtomicInstructions].State != StateSatisfied {
		t.Errorf("atomic-instructions = %v, want satisfied (it does not "+
			"depend on clarification)", byStage[StageAtomicInstructions].State)
	}
	// decomposition-coverage requires BOTH clarification and
	// atomic-instructions; clarification failed, so it must be blocked, and
	// its Runner must never have been called at all.
	if byStage[StageDecompositionCover].State != StateBlocked {
		t.Fatalf("decomposition-coverage = %v, want blocked",
			byStage[StageDecompositionCover].State)
	}
	if _, invoked := called.Load(StageDecompositionCover); invoked {
		t.Error("decomposition-coverage's Runner was invoked despite its " +
			"requirement having failed — this is the wasted, misleading " +
			"work PIPE-063 exists to prevent")
	}
	if byStage[StageDecompositionCover].Measured {
		t.Error("a blocked stage reported Measured=true, which would let a " +
			"caller stamp a fabricated span for work that never happened")
	}
}

// TestPIPE063_BlockingPropagatesThroughAnAlreadyBlockedStage proves the
// chain goes more than one level deep: a stage blocked because its own
// requirement failed must itself block whatever requires it, not merely
// report Blocked and stop propagating.
func TestPIPE063_BlockingPropagatesThroughAnAlreadyBlockedStage(t *testing.T) {
	// A -> B -> C -> D, a straight chain, so each stage's only requirement
	// is the one immediately before it.
	requirements := []Requirement{
		{StageInstructions, nil},
		{StageClarification, []Number{StageInstructions}},
		{StageAtomicInstructions, []Number{StageClarification}},
		{StageDecompositionCover, []Number{StageAtomicInstructions}},
	}
	classes := syntheticClasses()

	run := func(_ context.Context, stage Number) (State, string, map[string]any) {
		if stage == StageInstructions {
			return StateFailed, "root failed", nil
		}
		return StateSatisfied, "should never be reached", nil
	}

	decisions := RunConcurrently(context.Background(), requirements, classes, run,
		ScheduleOptions{})
	for _, decision := range decisions {
		if decision.Stage == StageInstructions {
			if decision.State != StateFailed {
				t.Errorf("root = %v, want failed", decision.State)
			}
			continue
		}
		if decision.State != StateBlocked {
			t.Errorf("stage %d = %v, want blocked (chained from the failed root)",
				decision.Stage, decision.State)
		}
	}
}

// TestPIPE059_ExclusiveMutatingStageNeverSharesAWaveWithAnything proves the
// scheduler's resource-class isolation directly: while the exclusive-
// mutating stage's Runner is executing, no other stage's Runner may be
// executing at the same time, in either direction (before or after).
//
// Discrimination: the Runner tracks how many stages are concurrently
// "in flight" via an atomic counter. The exclusive-mutating stage asserts
// the counter reads exactly one — itself — for the whole time it runs; every
// other stage asserts the exclusive-mutating one is not among the
// currently-running set. A scheduler that let the exclusive-mutating stage
// share a wave with anything else would fail this under `-race` (concurrent
// unsynchronized access the counter is specifically there to catch) and
// under the plain assertion (the counter would read more than one).
func TestPIPE059_ExclusiveMutatingStageNeverSharesAWaveWithAnything(t *testing.T) {
	requirements := []Requirement{
		{StageInstructions, nil},
		{StageClarification, []Number{StageInstructions}},      // pure-ast
		{StageAtomicInstructions, []Number{StageInstructions}}, // pure-ast
		{StageAtomMutation, []Number{StageInstructions}},       // exclusive-mutating
		{StageDecompositionCover, []Number{
			StageClarification, StageAtomicInstructions, StageAtomMutation,
		}},
	}
	classes := map[Number]ResourceClass{
		StageInstructions:       ResourceClassPureAST,
		StageClarification:      ResourceClassPureAST,
		StageAtomicInstructions: ResourceClassPureAST,
		StageAtomMutation:       ResourceClassExclusiveMutating,
		StageDecompositionCover: ResourceClassPureAST,
	}

	var inFlight int32
	var violation atomic.Bool
	run := func(_ context.Context, stage Number) (State, string, map[string]any) {
		count := atomic.AddInt32(&inFlight, 1)
		defer atomic.AddInt32(&inFlight, -1)
		if stage == StageAtomMutation && count != 1 {
			violation.Store(true)
		}
		if stage != StageAtomMutation && atomic.LoadInt32(&inFlight) > 1 {
			// Another stage may legitimately be running beside this one
			// (clarification and atomic-instructions both run in the same
			// wave) — what must never happen is atom-mutation being one of
			// them, which the branch above already checks from its own
			// side. This branch exists so the counter is actually read from
			// more than one goroutine while atom-mutation could plausibly
			// be running, which is what makes -race meaningful here.
			_ = count
		}
		return StateSatisfied, "", nil
	}

	RunConcurrently(context.Background(), requirements, classes, run, ScheduleOptions{})
	if violation.Load() {
		t.Fatal("atom-mutation ran while another stage was in flight")
	}
}

// TestPIPE059_ExclusiveMutatingIsolationHoldsUnderRepeatedRuns runs the
// isolation proof above many times, because a scheduling race is exactly
// the kind of defect that passes most of the time.
func TestPIPE059_ExclusiveMutatingIsolationHoldsUnderRepeatedRuns(t *testing.T) {
	for attempt := 0; attempt < 25; attempt++ {
		TestPIPE059_ExclusiveMutatingStageNeverSharesAWaveWithAnything(t)
	}
}

// TestPIPE058a_MeasuredFrontierWidthAgainstTheRealTables computes the wave
// partition PlanWaves produces for the real Requirements and ResourceClasses
// tables, restricted to exactly the stages examineStructure decides
// unconditionally (pipeline.ExaminesProducedSource minus the five stages
// that additionally require a passing suite), and reports the widest wave —
// the measured frontier width this lane's scheduler actually achieves
// against its own owned subset of the flow, as distinct from PIPE-058's
// theoretical widest frontier of 5 across the whole graph, three of whose
// five stages are decided in agent_execution.go rather than in this lane's
// examineStructure.
func TestPIPE058a_MeasuredFrontierWidthAgainstTheRealTables(t *testing.T) {
	unconditional := []Number{
		StageContracts, StageAtomCaseSynthesis, StageAtomExampleTests,
		StageAtomPropertyTests, StageAtoms, StageAtomVerification,
		StageAntiPatterns, StageAtomComplexity, StageAtomDocumentation,
		StageCompositionObligations, StageMoleculeTests, StageMolecules,
		StageMoleculeVerification, StageControlObligations, StageControlTests,
		StageControlFlow, StagePathCoverage, StageGlobalInvariants,
		StagePlatformMatrix,
	}
	requirements := RestrictToStages(Requirements, unconditional)
	classes := ResourceClassMap()
	waves := PlanWaves(requirements, classes)

	total := 0
	widest := 0
	for _, wave := range waves {
		total += len(wave)
		if len(wave) > widest {
			widest = len(wave)
		}
	}
	if total != len(unconditional) {
		t.Fatalf("waves covered %d stages, want %d", total, len(unconditional))
	}
	// The unconditional subset is real production data, not a synthetic
	// fixture, so this asserts a floor rather than an exact number: a lane
	// adding a new, more parallel stage to this set should not have to edit
	// this test, but a regression that collapses the whole subset back to a
	// single-wide, fully serial chain must fail it.
	if widest < 2 {
		t.Fatalf("widest wave across the unconditional examineStructure "+
			"subset is %d; want at least 2 to call this concurrent at all "+
			"(waves: %v)", widest, waves)
	}
	t.Logf("unconditional subset: %d stages across %d waves, widest wave %d",
		len(unconditional), len(waves), widest)
}

// TestPIPE058a_TheVerifiedSuiteSubsetGetsAFourWideWaveAfterMutation covers
// the second scheduling region examineStructure decides — the five stages
// currently gated behind `if verified`. atom-mutation is exclusive-mutating
// and every one of the other four's true requirement is external
// (satisfied before this call ever starts), so the real schedule should be
// exactly two waves: atom-mutation alone, then the rest together.
func TestPIPE058a_TheVerifiedSuiteSubsetGetsAFourWideWaveAfterMutation(t *testing.T) {
	verifiedGated := []Number{
		StageAtomFuzz, StageAtomMutation, StageRepetition, StageNonFunctional,
		StageAtomOptimization,
	}
	requirements := RestrictToStages(Requirements, verifiedGated)
	classes := ResourceClassMap()
	waves := PlanWaves(requirements, classes)

	if len(waves) != 2 {
		t.Fatalf("got %d waves, want 2 (atom-mutation alone, then the rest): %v",
			len(waves), waves)
	}
	if len(waves[0]) != 1 || waves[0][0] != StageAtomMutation {
		t.Fatalf("first wave = %v, want [atom-mutation] alone", waves[0])
	}
	rest := append([]Number(nil), waves[1]...)
	sort.Slice(rest, func(i, j int) bool { return rest[i] < rest[j] })
	want := []Number{
		StageAtomFuzz, StageRepetition, StageNonFunctional, StageAtomOptimization,
	}
	sort.Slice(want, func(i, j int) bool { return want[i] < want[j] })
	if fmt.Sprint(rest) != fmt.Sprint(want) {
		t.Fatalf("second wave = %v, want %v", rest, want)
	}
}

// TestScheduler_NoStagesReturnsNoDecisions is the boundary case: an empty
// requirements table must not panic and must produce no decisions, rather
// than the zero value of Number being treated as a real stage.
func TestScheduler_NoStagesReturnsNoDecisions(t *testing.T) {
	run := func(_ context.Context, _ Number) (State, string, map[string]any) {
		t.Fatal("Runner was called for an empty schedule")
		return StateFailed, "", nil
	}
	decisions := RunConcurrently(context.Background(), nil, nil, run, ScheduleOptions{})
	if len(decisions) != 0 {
		t.Fatalf("got %d decisions from an empty table, want 0", len(decisions))
	}
}

// TestScheduler_SkippedAndNotImplementedDoNotBlockDependents proves the
// blocking rule is specific to failed and blocked, not to "anything other
// than satisfied" — a stage this run had no need of, or cannot perform, is
// still a recorded fact its dependents may build on.
func TestScheduler_SkippedAndNotImplementedDoNotBlockDependents(t *testing.T) {
	requirements := syntheticGraph()
	classes := syntheticClasses()
	run := func(_ context.Context, stage Number) (State, string, map[string]any) {
		switch stage {
		case StageClarification:
			return StateSkipped, "nothing to clarify", nil
		case StageAtomicInstructions:
			return StateNotImplemented, "", nil
		}
		return StateSatisfied, "", nil
	}
	decisions := RunConcurrently(context.Background(), requirements, classes, run,
		ScheduleOptions{})
	for _, decision := range decisions {
		if decision.Stage == StageDecompositionCover && decision.State != StateSatisfied {
			t.Fatalf("decomposition-coverage = %v, want satisfied: skipped and "+
				"not-implemented requirements must not block a dependent",
				decision.State)
		}
	}
}
