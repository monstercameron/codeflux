package pipeline

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// RestrictToStages narrows a requirements table to exactly the given stages,
// dropping any Requires entry that names a stage outside the set (PIPE-058a).
//
// A caller scheduling only part of the flow — examineStructure decides a
// fixed subset of Flow, never the whole thing in one pass — needs a graph
// that is self-contained over exactly what it is about to run. Requirements
// itself is written against the whole flow: an entry like
// StageAtomDocumentation's names StageAtomFuzz and StageAtomMutation, which
// belong to a later pass the caller has not reached yet. Left in, those
// edges would never be satisfiable inside this call and the stage would
// never become ready; the caller does not want that stricter answer, it
// wants the graph restricted to what it actually controls, on the
// understanding that anything pruned away was already decided (or will be,
// by a later call) outside this one.
//
// A dependency that survives the restriction is kept exactly as
// Requirements declared it, in whatever order the caller's stage list
// named it, and the returned table is not itself validated: the caller is
// expected to run it straight through RunConcurrently, which validates the
// shape it actually needs (every requirement present, no forward edge)
// rather than the acyclic/reachable properties ValidateRequirementsTable
// checks for the unrestricted, whole-flow table.
func RestrictToStages(requirements []Requirement, stages []Number) []Requirement {
	included := make(map[Number]bool, len(stages))
	for _, stage := range stages {
		included[stage] = true
	}
	byStage := make(map[Number][]Number, len(requirements))
	for _, requirement := range requirements {
		byStage[requirement.Stage] = requirement.Requires
	}
	restricted := make([]Requirement, 0, len(stages))
	for _, stage := range stages {
		var kept []Number
		for _, dependency := range byStage[stage] {
			if included[dependency] {
				kept = append(kept, dependency)
			}
		}
		restricted = append(restricted, Requirement{Stage: stage, Requires: kept})
	}
	return restricted
}

// Runner performs one stage's check and reports its outcome in the ledger's
// own vocabulary — a State and the detail a person reads — so
// RunConcurrently can decide whether the stage's dependents may proceed
// (PIPE-063) without knowing anything about what the check actually
// examined. Evidence is returned separately so a Runner that has none is not
// forced to allocate an empty map.
type Runner func(ctx context.Context, stage Number) (state State, detail string, evidence map[string]any)

// Decision is one stage's outcome from a concurrent run, carrying its own
// measured span so a caller's ledger can stamp a real per-check start
// rather than inferring one from when a neighbouring stage happened to
// finish. That inference is what PIPE-006 originally did and is exactly
// what stops being correct the moment stages run concurrently: "the
// previous stage's finish" is not well defined for a stage that started
// before its sibling in the same wave recorded anything.
type Decision struct {
	Stage      Number
	State      State
	Detail     string
	Evidence   map[string]any
	StartedAt  time.Time
	FinishedAt time.Time
	// Measured is true for every stage RunConcurrently actually invoked: its
	// Runner call is individually timed, immediately around the call, by
	// this function rather than inferred from a neighbour. It is false only
	// for a stage this call marked Blocked without ever calling Runner
	// (PIPE-063) — there is no span to report for work that never ran, and
	// reporting StartedAt==FinishedAt for it would be exactly the fabricated
	// instant PIPE-006 exists to refuse.
	Measured bool
}

// ScheduleOptions configures one RunConcurrently call.
type ScheduleOptions struct {
	// MaxWorkers bounds how many goroutines run at once inside one wave;
	// zero or negative means unbounded, limited only by the wave's own
	// width, which is itself bounded by the number of stages the caller
	// restricted the graph to. It exists so a test can pin it to one and
	// compare the result against an unbounded run (PIPE-064): the same
	// requirements, the same classes, and a Runner with no hidden shared
	// state must produce identical Decisions either way, because a stage's
	// verdict must not depend on how many stages happened to be in flight
	// beside it.
	MaxWorkers int
	// OnWave, when set, is called once per wave with the stages about to run
	// concurrently in it, before any of their Runner calls start. It exists
	// for observability — a caller or a test measuring how wide the
	// scheduler's frontier actually gets against the real tables — and it is
	// never required for correctness: RunConcurrently's own result does not
	// depend on it being set.
	OnWave func(wave []Number)
}

// RunConcurrently executes every stage named in requirements, honoring each
// stage's declared dependency edges and resource class, and reports one
// Decision per stage (PIPE-058a/PIPE-059).
//
// Stages are scheduled in waves: within a wave, every ready stage's Runner
// is invoked in its own goroutine, and the call blocks until the whole wave
// finishes before computing the next one. This is not the maximum
// concurrency the graph allows — a stage waits for its whole wave even when
// only its own declared requirements have actually finished — but it is
// simple enough to schedule correctly and to prove correct (PIPE-064), and a
// wave's width is exactly what a reader means by "how much of this ran at
// once."
//
// A stage becomes ready once every stage it Requires has a Decision. A
// stage whose Requires names one whose Decision.State is StateFailed or
// StateBlocked is never given to Runner at all: it is recorded StateBlocked,
// with a detail naming the unmet requirement, and that Blocked state
// propagates to its own dependents the same way in a later round — a chain
// of unmet preconditions blocks all the way down rather than only its
// immediate child (PIPE-063). Skipped and not-implemented dependencies do
// not block: a stage this run had no need of, or cannot perform, is a
// recorded fact its dependents may still build on, the same distinction
// pipelineLedger.blocked already draws from pipelineLedger.record for every
// other state.
//
// Within one wave, at most one exclusive-mutating stage ever runs, and it
// never shares a wave with any other stage of any class: when a wave's
// ready set would otherwise mix an exclusive-mutating stage with others,
// the exclusive-mutating stage (the lowest-numbered, for determinism) is
// run alone and the rest wait for the wave after (PIPE-059).
//
// requirements must not point at a stage requirements itself does not
// declare — RestrictToStages guarantees this for its own output, and a
// caller building a table by hand must too, because a Requires entry naming
// an unknown stage can never be decided and would stall the schedule
// forever; RunConcurrently reports that case as every remaining stage
// permanently pending rather than looping, by returning once no further
// stage can become ready or blocked.
func RunConcurrently(
	ctx context.Context,
	requirements []Requirement,
	classes map[Number]ResourceClass,
	run Runner,
	options ScheduleOptions,
) []Decision {
	requiresOf := make(map[Number][]Number, len(requirements))
	pending := make(map[Number]bool, len(requirements))
	for _, requirement := range requirements {
		requiresOf[requirement.Stage] = requirement.Requires
		pending[requirement.Stage] = true
	}

	decisions := make(map[Number]Decision, len(requirements))
	for len(pending) > 0 {
		ready, blocked := nextRoundStages(pending, requiresOf, decisions)
		if len(ready) == 0 && len(blocked) == 0 {
			// Nothing left can become ready or blocked: either every stage
			// requirements declared has already run, or the table names a
			// dependency it never declares itself, which this function
			// cannot repair — ValidateRequirementsTable exists to catch that
			// before a table ever reaches here.
			break
		}

		for _, stage := range blocked {
			detail := blockedDetail(stage, requiresOf[stage], decisions)
			decisions[stage] = Decision{
				Stage: stage, State: StateBlocked, Detail: detail,
				Evidence: map[string]any{}, Measured: false,
			}
			delete(pending, stage)
		}

		if len(ready) == 0 {
			continue
		}
		wave := ready
		if exclusive := lowestExclusive(wave, classes); exclusive != 0 {
			wave = []Number{exclusive}
		}
		if options.OnWave != nil {
			options.OnWave(append([]Number(nil), wave...))
		}
		for stage, decision := range runWave(ctx, wave, run, options.MaxWorkers) {
			decisions[stage] = decision
			delete(pending, stage)
		}
	}

	results := make([]Decision, 0, len(decisions))
	for _, decision := range decisions {
		results = append(results, decision)
	}
	sort.Slice(results, func(first, second int) bool {
		return results[first].Stage < results[second].Stage
	})
	return results
}

// nextRoundStages splits every still-pending stage into the ones ready to
// run now and the ones a failed or blocked requirement rules out, leaving
// every other pending stage — one still waiting on a requirement that has
// not been decided yet — in neither list, to be reconsidered on a later
// round.
func nextRoundStages(
	pending map[Number]bool,
	requiresOf map[Number][]Number,
	decisions map[Number]Decision,
) (ready, blocked []Number) {
	for stage := range pending {
		satisfied := true
		unmet := false
		for _, dependency := range requiresOf[stage] {
			decision, decided := decisions[dependency]
			if !decided {
				satisfied = false
				break
			}
			if decision.State == StateFailed || decision.State == StateBlocked {
				unmet = true
				break
			}
		}
		switch {
		case unmet:
			blocked = append(blocked, stage)
		case satisfied:
			ready = append(ready, stage)
		}
	}
	sort.Slice(ready, func(first, second int) bool { return ready[first] < ready[second] })
	sort.Slice(blocked, func(first, second int) bool { return blocked[first] < blocked[second] })
	return ready, blocked
}

// blockedDetail names the first unmet requirement a stage was blocked on, in
// requirement-declaration order, so the message points at one concrete
// cause rather than listing every requirement whether or not it was the
// problem.
func blockedDetail(
	stage Number,
	requires []Number,
	decisions map[Number]Decision,
) string {
	for _, dependency := range requires {
		decision, decided := decisions[dependency]
		if !decided {
			continue
		}
		if decision.State != StateFailed && decision.State != StateBlocked {
			continue
		}
		name := fmt.Sprintf("stage %d", dependency)
		if described, found := StageByNumber(dependency); found {
			name = fmt.Sprintf("%d (%s)", dependency, described.Name)
		}
		verb := "failed"
		if decision.State == StateBlocked {
			verb = "was itself blocked"
		}
		return fmt.Sprintf(
			"blocked: this stage requires %s, which %s, so running this "+
				"stage's check would be wasted work computed over a known-"+
				"broken precondition", name, verb)
	}
	return fmt.Sprintf(
		"blocked: stage %d requires a stage that did not hold, but the "+
			"specific one could not be identified", stage)
}

// lowestExclusive returns the lowest-numbered exclusive-mutating stage in a
// ready set, or zero when none of them is exclusive. wave is expected
// sorted, which nextRoundStages already guarantees, so "lowest" and "first"
// agree without a second sort here.
func lowestExclusive(wave []Number, classes map[Number]ResourceClass) Number {
	for _, stage := range wave {
		if classes[stage].Exclusive() {
			return stage
		}
	}
	return 0
}

// runWave invokes run for every stage in wave concurrently, bounded by
// maxWorkers, and returns one Decision per stage. Every goroutine it starts
// is joined before runWave returns — there is no path out of this function
// that leaves one running — and each is individually timed immediately
// around its own run call, which is what makes every returned Decision's
// span real rather than inferred from a neighbour (PIPE-006).
func runWave(
	ctx context.Context,
	wave []Number,
	run Runner,
	maxWorkers int,
) map[Number]Decision {
	results := make(map[Number]Decision, len(wave))
	if len(wave) == 0 {
		return results
	}

	var mutex sync.Mutex
	var group sync.WaitGroup
	var semaphore chan struct{}
	if maxWorkers > 0 {
		semaphore = make(chan struct{}, maxWorkers)
	}

	for _, stage := range wave {
		group.Add(1)
		go func(stage Number) {
			defer group.Done()
			if semaphore != nil {
				semaphore <- struct{}{}
				defer func() { <-semaphore }()
			}
			started := time.Now()
			state, detail, evidence := run(ctx, stage)
			finished := time.Now()
			if evidence == nil {
				evidence = map[string]any{}
			}
			decision := Decision{
				Stage: stage, State: state, Detail: detail, Evidence: evidence,
				StartedAt: started, FinishedAt: finished, Measured: true,
			}
			mutex.Lock()
			results[stage] = decision
			mutex.Unlock()
		}(stage)
	}
	group.Wait()
	return results
}

// PlanWaves computes the wave partition RunConcurrently would produce if
// every stage held, without running anything.
//
// It exists so a caller — or a test measuring this table's actual
// concurrency, such as the widest frontier PIPE-058 reports — can ask "how
// wide does this get" without a Runner or a worktree, and so RunConcurrently
// itself never needs to expose its internal round loop for that purpose.
// The answer can differ from a real run's actual waves whenever a stage
// fails: a failure blocks its dependents immediately, in the same round it
// is decided, which removes them from every later wave PlanWaves would
// otherwise have placed them in. PlanWaves is therefore the schedule's best
// case, not a prediction of every run.
func PlanWaves(
	requirements []Requirement,
	classes map[Number]ResourceClass,
) [][]Number {
	var waves [][]Number
	options := ScheduleOptions{
		OnWave: func(wave []Number) {
			waves = append(waves, wave)
		},
	}
	holdEverything := func(_ context.Context, _ Number) (State, string, map[string]any) {
		return StateSatisfied, "", nil
	}
	RunConcurrently(context.Background(), requirements, classes, holdEverything, options)
	return waves
}
