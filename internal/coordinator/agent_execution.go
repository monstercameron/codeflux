package coordinator

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	agentloop "codeflux.dev/codeflux/internal/agent"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
	"codeflux.dev/codeflux/internal/executor"
	"codeflux.dev/codeflux/internal/openaimodel"
	"codeflux.dev/codeflux/internal/pipeline"
	"codeflux.dev/codeflux/internal/providers"
	"codeflux.dev/codeflux/internal/redact"
	"codeflux.dev/codeflux/internal/storage"
)

// AgentExecution runs one started task through the real agent loop.
//
// The loop, the tools, the journal, and the checkpoints all existed; what did
// not exist was anything that assembled them when a task started. So a request
// reached a worktree and stopped, and the interface truthfully showed a running
// task doing nothing. This is the composition that was missing.
type AgentExecution struct {
	graphs       *GraphProjectionService
	repositories *storage.Repositories
	events       *events.Hub
	worktrees    worktreeBindingReader
	model        agentloop.FixedModel
	// escalate builds a model by name, so a run that has stopped making
	// progress can move up its ladder. It is nil when the coordinator was
	// handed a model directly rather than a key to build one from — a test
	// fixture, most often — and a run with no way to build another model stays
	// on the one it has instead of failing for want of an escalation it was
	// never going to need.
	escalate func(name string) (agentloop.FixedModel, error)
	redactor *redact.Pipeline
	// settings are every choice the flow leaves to the person running it.
	// They were constants scattered through this package, each a reasonable
	// default and none of them visible; gathering them into one declared value
	// is what lets an interface show them and a person change them.
	settings pipeline.Settings
	// completion is what moves a finished run out of "running" into
	// "awaiting-review" (AUDIT-020). It is nil when the coordinator could not
	// construct it — no repositories or no redaction pipeline, most often a
	// test fixture — and Run leaves a task running rather than panicking over
	// a surface it was never given.
	completion *RepairCompletionService
	// completionValidations and completionGate are completion's own
	// dependencies that Run must reach into directly: the validation runner is
	// told this run's own validation already passed immediately before
	// ValidateAndRepair is called, and the gate is marked ready only once that
	// call has actually produced passing evidence.
	completionValidations *agentExecutionValidationRunner
	completionGate        *agentExecutionCompletionGate
}

// WithSettings replaces every choice the flow leaves open.
//
// An invalid value is refused rather than clamped: silently correcting one
// setting produces a run that does not do what its configuration says, which
// is worse than refusing to start.
func (execution *AgentExecution) WithSettings(
	settings pipeline.Settings,
) (*AgentExecution, error) {
	if err := settings.Validate(); err != nil {
		return nil, err
	}
	execution.settings = settings
	return execution, nil
}

// Settings reports the choices this coordinator is running with.
func (execution *AgentExecution) Settings() pipeline.Settings {
	return execution.settings
}

// ambiguityPolicy renders the configured posture in the domain's vocabulary.
func (execution *AgentExecution) ambiguityPolicy() domain.AmbiguityPolicy {
	if execution.settings.Ambiguity == pipeline.AmbiguityAssume {
		return domain.AmbiguityAssume
	}
	return domain.AmbiguityAsk
}

// platformTargets renders the configured platforms as build targets.
func (execution *AgentExecution) platformTargets() []platformTarget {
	if execution.settings.Platforms == pipeline.PlatformsPortable {
		return PortablePlatforms()
	}
	return hostPlatform()
}

// worktreeBindingReader looks up where a task's isolated copy lives.
type worktreeBindingReader interface {
	GetWorktreeBinding(context.Context, domain.TaskID) (storage.WorktreeBinding, error)
}

// NewAgentExecution builds the runner.
func NewAgentExecution(
	repositories *storage.Repositories,
	hub *events.Hub,
	worktrees worktreeBindingReader,
	model agentloop.FixedModel,
	redactor *redact.Pipeline,
	graphs *GraphProjectionService,
) (*AgentExecution, error) {
	switch {
	case repositories == nil || hub == nil || worktrees == nil:
		return nil, errors.New("agent execution needs repositories, events, and worktrees")
	case model == nil:
		return nil, errors.New("agent execution needs a model")
	case redactor == nil:
		return nil, errors.New("agent execution needs a redaction pipeline")
	}
	return &AgentExecution{
		graphs:       graphs,
		repositories: repositories, events: hub, worktrees: worktrees,
		model: model, redactor: redactor,
		settings: pipeline.DefaultSettings(),
	}, nil
}

// Run executes one task and narrates it into the session.
//
// Every step is published as it happens rather than at the end, because a
// supervision console whose timeline fills in only once the work is over is not
// supervising anything.
func (execution *AgentExecution) Run(
	ctx context.Context,
	taskID domain.TaskID,
	runID domain.RunID,
) error {
	scope, err := execution.resolveScope(ctx, taskID)
	if err != nil {
		return err
	}
	// The invariant, enforced rather than intended.
	//
	// When this returns, the task is terminal or explicitly recoverable — never
	// running, never validating. Every path below tries to arrange that, and
	// "every path below" is exactly the kind of claim that is true until
	// somebody adds a path. A run left running is invisible: nothing retries
	// it, nothing reports it, and whatever is waiting on it waits forever.
	//
	// So the guarantee is made where it cannot be forgotten, and it is the
	// generous ending rather than the harsh one: a run that reached here
	// without recording an outcome has produced work nobody has judged, and
	// recovery-required says that while failed does not.
	defer execution.ensureTerminal(ctx, scope, taskID)

	execution.say(ctx, scope, events.KindMessageFinal,
		"Starting work in an isolated worktree.")

	// Every stage of the flow is written down as the run goes, and the ones
	// this build cannot perform are written down too. Without that, a run that
	// performed six of the flow's stages left the same record as one that
	// performed all of them.
	ledger := newPipelineLedger(ctx, execution.repositories, taskID, runID)
	defer ledger.close(ctx)

	// The append-only chronological record §31 assumes exists (MEM-001):
	// opened here, at the start of the run, bound to this task's real
	// pipeline attempt number (MEM-003) so a task started twice gets two
	// episodes rather than one clobbering the other's evidence. Closed by
	// the deferred call below at every return path out of this function,
	// not only the one at its end.
	episode := execution.openRunEpisode(ctx, scope, taskID, runID, ledger.currentAttempt())
	defer execution.closeRunEpisode(ctx, episode, scope)
	// The gate requires at least one executable acceptance example (PIPE-019).
	// It used to also accept an explicit declaration that none was supplied,
	// which let a request with nothing external to check it ride through this
	// stage as satisfied — the same shape of defect the ledger exists to
	// catch, reproduced in the ledger's own first stage. That escape is gone:
	// zero examples now fails the gate rather than satisfying a weaker one.
	//
	// It was blocked on PIPE-117/PIPE-118: without a second form, requiring an
	// example would have refused every task class with no command-line
	// surface. The instructions stage counts both forms a requirement can use
	// today: a command's expected output, and a named test that must pass.
	examples := parseAcceptanceExamples(scope.requirement)
	namedExamples := parseNamedTestExamples(scope.requirement)
	totalExamples := len(examples) + len(namedExamples)
	ledger.require(ctx, pipeline.StageInstructions, totalExamples > 0,
		acceptanceDetail(totalExamples),
		map[string]any{
			"requirement_bytes":   len(scope.requirement),
			"acceptance_examples": len(examples),
			"named_test_examples": len(namedExamples),
			"total_examples":      totalExamples,
		})

	// The acceptance-oracle control (PIPE-020): requiring an example is not
	// the same as the example being any good. This runs before anything below
	// touches scope.worktree, which is exactly the property the check needs —
	// it proves the example(s) fail against the repository this run actually
	// received, not against some other build of it.
	ledger.decide(ctx, pipeline.StageAcceptanceOracle,
		execution.checkAcceptanceOracle(ctx, scope.worktree, scope.requirement, examples))

	// A request that reads two ways is settled before anything is written. The
	// store refuses to record a plan for one that still needs an answer, so a
	// run that pressed on reached a constraint failure instead of a question,
	// and the person was shown a database error where one sentence would have
	// unblocked the work.
	if analysis, analysisErr := storage.AnalyzeTaskRequirement(
		scope.requirement,
	); analysisErr == nil {
		decision := resolveAmbiguity(analysis, execution.ambiguityPolicy())
		if decision.Blocked {
			execution.askForClarity(ctx, scope, decision.Question)
			ledger.record(ctx, pipeline.StageClarification,
				pipeline.StateBlocked,
				"the request reads two ways and the person was asked", nil)
			return nil
		}
		execution.noteAssumptions(ctx, scope, decision.Assumptions)
		ledger.satisfied(ctx, pipeline.StageClarification,
			"no material ambiguity was left unresolved",
			map[string]any{
				"policy":      string(execution.ambiguityPolicy()),
				"assumptions": len(decision.Assumptions),
			})
	} else {
		// This used to be swallowed: the stage was simply never recorded, so
		// the ledger's own close() reported it not-implemented with no way to
		// tell that from a build that genuinely performs no clarification
		// check. Recording the failure here, with the analysis error in the
		// detail, says why in the one place a reader is already looking
		// (PIPE-019a).
		ledger.failed(ctx, pipeline.StageClarification,
			fmt.Sprintf(
				"the requirement could not be analysed for ambiguity: %v",
				analysisErr,
			))
	}

	// How the work is broken up is decided before anything is written, on the
	// rung the settings name for it and no other. It does not climb: there is
	// nothing to climb from, and a decomposition that misses a behaviour is
	// paid for on every rung, on every attempt, by work that cannot recover it.
	steps, planningNote := execution.planFromRequirement(ctx, scope.worktree, scope.requirement)
	ledger.satisfied(ctx, pipeline.StageAtomicInstructions, planningNote,
		map[string]any{
			"planning_rung": execution.settings.PlanningRung,
			"steps":         len(steps),
		})

	// The diagram is built from the same facts the run produces, as it produces
	// them, so the panel fills in while the work happens rather than after. The
	// recorder is attached to the scope before anything copies the scope: it was
	// attached afterwards, so every tool call saw a scope with no recorder and
	// drew nothing.
	recorder := newAgentGraphRecorder(ctx, execution.graphs, execution.repositories,
		scope.projectID, taskID, scope.requirement)
	scope.graph = recorder

	// The plan is recorded before anything is drawn from it: every operation the
	// diagram shows binds to a plan revision, so a run with no durable plan can
	// draw nothing it did.
	plan, planErr := execution.recordDurablePlan(ctx, scope, steps, 1)
	if planErr != nil {
		execution.say(ctx, scope, events.KindMessageFinal,
			"The plan could not be recorded, so this run will not be diagrammed: "+
				planErr.Error())
	}
	// The run then adopts the recorded plan's own step identities. Two
	// vocabularies for one plan meant every tool call, journal entry, and drawn
	// operation named a step the stored plan had never heard of, and the store
	// refused each of them.
	steps = adoptDurablePlanSteps(steps, plan)
	if planErr == nil {
		ledger.satisfied(ctx, pipeline.StageAtomicInstructions,
			"the request was split into the plan steps the run carried out",
			map[string]any{
				"steps":         len(steps),
				"plan_revision": plan.Revision,
			})
	} else {
		ledger.failed(ctx, pipeline.StageAtomicInstructions, planErr.Error())
	}
	// Every file the request named must be covered by a step, and no step may
	// name a file the request did not. The first half catches work that was
	// asked for and silently dropped; the second catches a step invented for a
	// file nobody wants, which is how a run came to write a stray file at the
	// repository root and then refuse its own writes as out of scope.
	uncovered, unasked := decompositionGaps(scope.requirement, steps)
	ledger.require(ctx, pipeline.StageDecompositionCover,
		len(uncovered) == 0 && len(unasked) == 0,
		decompositionDetail(uncovered, unasked),
		map[string]any{
			"steps": len(steps), "uncovered": uncovered, "unasked": unasked,
		})

	execution.publishPlan(ctx, scope, steps)
	recorder.declarePlan(ctx, plan, graphPlanStepsOf(steps))
	execution.reportGraphFailure(ctx, scope, recorder)

	// The run is bound to the plan it is carrying out before it does anything.
	// That binding is the execution root everything else attributes to: a tool
	// record naming an unbound run is refused, and correctly, because nothing
	// could then say which plan the tool was run for.
	if _, bindErr := execution.repositories.BindRunPlan(
		ctx, storage.BindRunPlan{
			RunID: runID, TaskID: taskID, PlanRevision: plan.Revision,
			IdempotencyKey: agentExecutionKey("agent-run-plan-", runID.String()),
		},
	); bindErr != nil {
		execution.say(ctx, scope, events.KindMessageFinal,
			"This run could not be bound to its plan, so its durable tool "+
				"record will be incomplete: "+bindErr.Error())
	}

	tools, err := NewWorktreeToolExecutor(scope.worktree, execution.redactor, nil)
	if err != nil {
		return err
	}
	router, err := NewPolicyAuthorityRouter(
		agentPermissionPolicy(taskID, scope.worktree), 1,
		strings.Repeat("0", 64),
	)
	if err != nil {
		return err
	}
	// The provider is registered before the first request is made against it.
	// Every model request, and so every tool record, names the provider it was
	// sent to and the configuration and rates in force at the time.
	registration, registrationErr := execution.registerProvider(ctx)
	if registrationErr != nil {
		execution.say(ctx, scope, events.KindMessageFinal,
			"This run's provider could not be registered, so its durable tool "+
				"record will be incomplete: "+registrationErr.Error())
	}
	journal := newAgentToolJournal(execution.repositories, taskID, runID,
		registration, execution.model.Identity())
	narrator := &narratingExecutor{
		inner: tools, execution: execution, scope: scope, ctx: ctx,
		journal: journal, worktree: scope.worktree,
	}
	// The loop is rebuilt rather than mutated when the run escalates: the model
	// is a dependency it is constructed with, and reaching in to swap it would
	// leave the journal naming the model that produced the earlier turns as
	// though it had produced the later ones too.
	//
	// AUDIT-020: PlanSteps is durable, not the no-op journal the loop's other
	// optional ports still use. Every step transition the loop makes is what
	// RepairCompletionService.PrepareCompletion's evidence chain later reads
	// back through ListPlanStepStates, so this is the one port here that has
	// to be real for a run to ever become reviewable.
	//
	// AgentExecutionPersistence already existed, fully implemented and tested,
	// with no production caller of its own — the same defect class as
	// PrepareCompletion itself. It also implements ToolJournal, but that half
	// is not reused here: journal (agentToolJournal, below) additionally plans
	// each tool's model request before recording the tool against it, which
	// this type does not do, so swapping the tool journal too would break
	// attribution rather than fix it. Only PersistPlanStepTransition is used,
	// through the narrower PlanStepStore port.
	var planSteps agentloop.PlanStepStore = agentNoDurableJournal{}
	if persistence, persistenceErr := NewAgentExecutionPersistence(
		execution.repositories,
	); persistenceErr == nil {
		planSteps = persistence
	}
	buildLoop := func(model agentloop.FixedModel) (*agentloop.ExecutionLoop, error) {
		return agentloop.NewExecutionLoop(agentloop.LoopDependencies{
			Model: model, Authority: router, Tools: narrator,
			Journal: journal, PlanSteps: planSteps,
			Checkpoints:             agentNoDurableJournal{},
			PlanApprovalCheckpoints: agentNoDurableJournal{},
			Control:                 agentActiveControl{},
			Interrupts:              agentActiveControl{},
		})
	}
	loop, err := buildLoop(execution.model)
	if err != nil {
		return err
	}

	maximumCost, err := providers.NewExactAmount("USD", 500, 100)
	if err != nil {
		return err
	}

	// A run gets more than one attempt. One edit step allows one write, so a
	// first attempt that writes a file and then watches its own tests fail has
	// nowhere left to fix it: it can see the failure and cannot act on it. Each
	// further attempt gets a fresh plan for the same files and is told exactly
	// what the last one broke, which is what a person would do.
	var outcome agentloop.LoopOutcome
	var assembled error
	// reviewed keeps the adversarial pass to one round. It asks a question no
	// gate asks, and asking it every attempt would turn a bounded loop into an
	// argument about taste.
	reviewed := false
	// casesAsked keeps the synthesised-case request to one round, for the same
	// reason the review is kept to one: it is a bounded ask, not a standard
	// nothing can ever finally meet.
	//
	// The variable was removed when three refinement passes were consolidated
	// into one review, and this comment outlived it. The stage kept reporting
	// untried cases exactly as before, so nothing looked wrong — but no run
	// could act on it, and rung 5 recorded twenty-one untried cases it was
	// never once asked about. A stage that can only accuse is worse than no
	// stage: a reader cannot tell an unfixed defect from an unaskable one.
	// The last state of the worktree that was known good, kept beside it so
	// nothing the run does can see or edit its own safety net.
	checkpoint := newVerifiedCheckpoint(scope.worktree)

	caseRounds := 0
	documentationRounds := 0
	coverageRounds := 0
	propertyRounds := 0
	// The circuit breaker's state: how many identical infrastructure failures
	// have happened in a row, against what, and whether it has opened.
	// attemptedFindings remembers which criticisms this run has already been
	// sent back for, and carriedAdvisories the ones it will finish with rather
	// than keep spending attempts on.
	attemptedFindings := map[string]bool{}
	var carriedAdvisories []adversarialFinding
	reviewRounds := 0
	unrecognisedLoopErrors := 0
	// The machinery's own allowance, separate from the work's and never
	// refunded, and the ruling that ends the run when it is spent.
	infrastructure := newInfrastructureBudget(time.Now())
	var providerCircuit circuitDecision
	// unfinished is what the run still owed when it ran out of attempts, so
	// the completion message can say so instead of claiming to be done.
	unfinished := ""
	sentBackBecause := "the tests did not pass"
	failure := ""
	attempts := 0
	// The tracker watches for a run repeating itself rather than converging,
	// and moves it up the model ladder when it is. sendBack routes every way a
	// run is returned through it, so there is one place that knows a run is
	// stuck and one place that decides what to do about it.
	progress := newConvergence(execution.settings)
	// awaitingApproval stops the loop when the ladder's next step is one this
	// machine will not take on its own.
	awaitingApproval := ""
	// sendBackInfrastructure records an attempt lost to the machinery rather
	// than to the work.
	//
	// Kept apart from sendBack on purpose, and not merely renamed. A provider
	// that would not answer used to arrive at the assembly gate — the gate that
	// means "the code does not compile" — because that was the closest name to
	// hand, and three consequences followed from the mislabelling. It counted
	// toward the stall that drives escalation, so a closed socket bought a more
	// expensive model, which is money spent on the wrong remedy. It was written
	// down as a lesson, so the project learned something about its own code
	// from an outage. And it charged an attempt, so a run two gates from
	// finished could die of a transport blip.
	//
	// None of those follow here. The worktree did not change, the model formed
	// no judgement, and there is nothing to learn.
	// Whether the review that is sending work back found only blind spots,
	// which decides whether the next attempt is a tests-only round.
	blindSpotsOnlyThisRound := false

	// The acceptance examples every instruction carries, so a run correcting
	// one thing cannot quietly break the one thing that defines done.
	acceptanceGuard := acceptanceInvariant(
		parseAcceptanceExamples(scope.requirement))

	// One ruling per provider failure, taken before anything is said or done.
	//
	// The old shape scattered the decision: a counter here, a checkpoint test
	// there, a message printed before either had been consulted. It could say
	// "Trying again" and finalise on the next line, and it tied opening the
	// breaker to holding a checkpoint — so the case with no verified work, the
	// one where stopping matters most because there is nothing to fall back
	// on, was the case that kept going.
	sendBackInfrastructure := func(refusal error, instruction string) {
		decision := decideCircuit(
			refusal, infrastructure, checkpoint.taken, time.Now())
		tracef("infra", "gate=provider-availability outcome=%s "+
			"disposition=%s worktree_changed=false budget=%d",
			providerOutcomeOf(refusal), decision.Disposition,
			infrastructure.AttemptsRemaining)
		failure = instruction
		sentBackBecause = "the provider did not answer"
		// The work's budget is refunded because the run learned nothing. The
		// infrastructure allowance is not, and it is the one that terminates.
		progress.refund()
		if decision.Open {
			providerCircuit = decision
			// What this run learned about the provider is worth more than one
			// run. Twenty tasks discovering the same dead provider one full
			// timeout at a time is twenty times the wait for one fact.
			if decision.Disposition != circuitFailConfiguration {
				sharedProviderHealth.recordExhausted(
					progress.currentModel(), decision.Reason, time.Now())
			}
		}
		if decision.RetryAfter > 0 {
			select {
			case <-ctx.Done():
			case <-time.After(decision.RetryAfter):
			}
		}
	}

	sendBack := func(gate string, instruction string, because string) {
		tracef("sendback", "gate=%s because=%s", gate, because)
		// What the next attempt may touch, decided from the gate rather than
		// from the prose. The prose is what the model reads; the gate is what
		// the coordinator decided, and the two can drift.
		narrator.permitted = scopeOfNextAttempt(gate, blindSpotsOnlyThisRound)
		if narrator.permitted != editAnything {
			tracef("sendback", "  next attempt may edit: %s",
				narrator.permitted)
		}
		// Appended rather than woven in, so it reads as a constraint on the
		// instruction rather than as another item in it.
		if acceptanceGuard != "" && !strings.Contains(instruction, "must remain true") {
			instruction += acceptanceGuard
		}
		// Every refusal is something this project has now learned. Written
		// down here rather than at the end, because a run that dies later
		// still learned it, and a lesson only a surviving run records is a
		// lesson the worst runs never contribute.
		execution.recordRunLesson(ctx, scope, gate, because, instruction)
		traceBlock("instruct", "what the next attempt is told:", instruction)
		failure = instruction
		sentBackBecause = because
		// The rung THIS attempt actually ran on, captured before anything
		// below might move progress to a new one for the NEXT attempt
		// (MEM-002: the transition records what happened to the attempt
		// that just ran, not what the next one will run on).
		rung := progress.currentModel()
		// The stall is judged on why the work came back, not on the prose
		// telling the model about it.
		//
		// This recorded the whole combined instruction, which opens with
		// "There are N things still outstanding" and then lists whichever
		// gates are outstanding now. Its first four hundred characters — the
		// part the fingerprint keeps — therefore changed whenever the set of
		// gates changed, which is exactly what happens while a run fixes some
		// and not others. A failure that repeated every other attempt never
		// looked repeated, so the stall never reached its threshold and the
		// ladder never escalated. Ladder rung 2 asked for the same doc comment
		// six times on the lowest rung with three better ones available.
		//
		// The reason is short, stable, and is what a person would call the
		// failure: "it did not compile", "the work was not finished". Two
		// attempts that came back for the same reason are a repeat even when
		// the surrounding list has moved on.
		decision := progress.record(gate, because)
		// A gate a project has pinned to a particular rung selects it for the
		// attempt it is sending back, whatever the ladder has reached. It only
		// ever moves upward, so a pin cannot undo an escalation a run earned.
		if pinned := execution.settings.RungForStage(
			gate, progress.currentModel(),
		); pinned != progress.currentModel() && decision.Escalated == "" {
			decision.Escalated = pinned
			decision.Why = "this project pins " + gate + " to " + pinned +
				", so the next attempt runs there"
		}
		outcome := storage.EpisodeAttemptOutcomeSentBack
		switch {
		case decision.Escalated != "" &&
			execution.settings.NeedsApproval(decision.Escalated):
			// Stopped rather than climbed. There is no resume: the run ends
			// here and is started again once the rung is allowed, which is
			// stated plainly because a person told "waiting for approval"
			// would reasonably expect it to carry on by itself.
			awaitingApproval = decision.Escalated
			outcome = storage.EpisodeAttemptOutcomeAwaitingApproval
			execution.say(ctx, scope, events.KindMessageFinal,
				approvalRequest(decision.Escalated, gate, instruction, progress))
		case decision.Escalated != "" && execution.escalate != nil:
			outcome = storage.EpisodeAttemptOutcomeEscalated
			stronger, buildErr := execution.escalate(decision.Escalated)
			if buildErr != nil {
				execution.say(ctx, scope, events.KindMessageFinal,
					"This run is stuck and the stronger model could not be "+
						"built, so it continues on "+progress.currentModel()+
						": "+buildErr.Error())
				execution.recordAttemptTransition(ctx, episode, attempts, gate, instruction, rung, outcome)
				return
			}
			rebuilt, loopErr := buildLoop(stronger)
			if loopErr != nil {
				execution.say(ctx, scope, events.KindMessageFinal,
					"This run is stuck and could not be moved to a stronger "+
						"model: "+loopErr.Error())
				execution.recordAttemptTransition(ctx, episode, attempts, gate, instruction, rung, outcome)
				return
			}
			loop = rebuilt
			execution.say(ctx, scope, events.KindMessageFinal, decision.Why)
		case decision.Decompose:
			// The same request, cut finer. What the next attempt is shown is
			// the instruction to break the work down, ahead of the failure
			// that prompted it, because the failure alone has already been
			// tried against twice and produced this.
			outcome = storage.EpisodeAttemptOutcomeDecomposed
			failure = decompositionInstruction(scope.requirement) +
				"\n\nThe check that keeps failing:\n" + instruction
			sentBackBecause = "the request is being broken into smaller pieces"
			execution.say(ctx, scope, events.KindMessageFinal, decision.Why)
		}
		execution.recordAttemptTransition(ctx, episode, attempts, gate, instruction, rung, outcome)
	}
	// The bound is the tracker's, not a flat count. A run that escalates or
	// decomposes is granted a fresh allowance for the new approach, because
	// the attempts already spent were spent establishing that the previous
	// one does not work; charging them to the new one gave the strong model
	// the tail of an exhausted budget and put decomposition out of reach
	// entirely on the shipped defaults.
	// Intake, clarification and planning are decided once, above this loop.
	// Sealing them here lets each later attempt carry them forward instead of
	// recording them as never performed.
	// What this project already knows, gathered before the run plans anything.
	//
	// Retrieval and registration both lived in recallKnownAtoms, which runs
	// after the attempt loop has finished. A run was therefore told what it
	// could have reused at the moment it could no longer use it, and the
	// stage's report that the project held no earlier work was filed after the
	// work had been rebuilt. Registration was implemented and worked; nothing
	// read it in time for it to matter.
	//
	// Computed once rather than per attempt. What the project knows does not
	// change while this run is working, and re-reading it every attempt would
	// spend a query to be told the same thing.
	preflight := execution.runMemoryPreflight(ctx, scope)
	execution.say(ctx, scope, events.KindMessageFinal,
		"Memory preflight: "+preflight.summary()+".")

	ledger.sealPreAttemptStages()
	for attempt := 1; progress.moreAttempts() && awaitingApproval == "" &&
		!providerCircuit.Open; attempt++ {
		// Time enough to finish, or do not start.
		//
		// A model call takes as long as it takes, and finishing takes a known
		// amount: restore the checkpoint, rebuild, re-run the suite, check the
		// acceptance examples, write the terminal state. Starting a call that
		// cannot leave room for that trades a result the run already has for a
		// chance at a better one, and loses both when the deadline arrives
		// mid-call — the work is unfinalised and the record says nothing.
		//
		// Only when the caller set a deadline. A run with no deadline has
		// nothing to reserve against, and inventing one would end runs that
		// were entitled to continue.
		if short, remaining := tooLateToStartAnAttempt(ctx); short {
			providerCircuit = circuitDecision{Open: true,
				Disposition: circuitRestoreAndFinish,
				Reason:      "the time left is reserved for finishing properly"}
			tracef("deadline", "%s left, less than the %s reserved to finish; "+
				"not starting attempt %d",
				remaining.Round(time.Second), finalisationReserve, attempt)
			break
		}
		progress.beginAttempt()
		// Each attempt gets one wholesale write per file. The count is per
		// attempt rather than per run, because a run that legitimately starts
		// over on a later attempt should not be refused for what an earlier one
		// did.
		narrator.wholeFileWrites = map[string]int{}
		tracef("attempt", "%d begins on rung %s", attempt, progress.currentRung())
		// What another run has already found out about this provider, said
		// before the wait rather than after it. Advisory: this run still tries,
		// because a task that never starts because an unrelated one failed is
		// the worse mistake.
		if why, recent := sharedProviderHealth.recentlyExhausted(
			progress.currentModel(), time.Now(),
		); recent {
			tracef("infra", "another run found %s unavailable moments ago (%s); "+
				"trying anyway", progress.currentModel(), why)
		}
		if attempt > 1 {
			// The flow ledger follows the attempt loop rather than the run, so
			// each attempt's stages are recorded under their own number. See
			// pipelineLedger.beginAttempt: without this every attempt wrote
			// into attempt one, storage kept the first row, and a run that
			// converged still read as its first draft.
			ledger.beginAttempt(ctx)
		}
		attempts = attempt
		if attempt > 1 {
			// The reason is carried rather than assumed. There are four ways a
			// run is sent back now — it did not compile, its tests failed, its
			// work was incomplete, or a review found it weak — and announcing
			// the wrong one makes every later message in the timeline suspect.
			execution.say(ctx, scope, events.KindMessageFinal, fmt.Sprintf(
				"Attempt %d: %s, so I am revising the files.",
				attempt, sentBackBecause))
			// The plan itself is unchanged — the same files, for the same
			// reason — so it is not recorded again. What changes is what the
			// agent knows: it is told what its last attempt broke. Recording a
			// new plan revision per attempt would claim the plan was revised
			// when only the attempt was, and would move the revision every
			// other record attributes itself to.
			steps = adoptDurablePlanSteps(
				agentPlanSteps(scope.worktree, scope.requirement), plan)
			execution.publishPlan(ctx, scope, steps)
		}
		approvalID, approvalErr := domain.NewApprovalID()
		if approvalErr != nil {
			return approvalErr
		}
		// AUDIT-010: the first round now plans against deterministic context
		// selection with a persisted manifest, not against a directory
		// listing. The listing remains the fallback, and a run that falls back
		// says so rather than quietly planning against less.
		selection := selectAgentContext(
			ctx, execution.repositories, scope.repositoryID, scope.worktree,
			scope.revision, scope.requirement, execution.redactor,
			execution.repositories.InstructionApprovalResolverFor(
				scope.projectID, scope.repositoryID, "agent-first-round"),
		)
		if selection.Degraded {
			execution.say(ctx, scope, events.KindMessageFinal,
				"Planning against the worktree listing rather than selected "+
					"context: "+selection.Reason)
		}
		context := selection.Items
		// What this project has already built and already got wrong, put in
		// front of the run before it plans rather than reported to it after it
		// has finished. Gathered once above; shaped here into the decision the
		// run has to make about each item.
		context = append(context, preflight.contextItems()...)
		// The files this attempt is going to change, in full, with the revision
		// a patch is written against — and the tests beside them, which are
		// what must still pass afterwards.
		//
		// Context selection reads the repository at its base revision, and on a
		// generated project that is empty: the files being refined are ones
		// this run wrote, and they were in no revision when the selection ran.
		// A patch cannot be written from memory, so offering the patch tool
		// without the file was offering a tool that could not be used.
		context = append(context, patchContextItems(scope.worktree, steps)...)
		context = append(context, producedTestFilesFor(scope.worktree, steps)...)
		if failure != "" {
			context = append(context, agentContextItem(
				"last-test-run-output", failure))
		}
		attemptOutcome, runErr := loop.Run(ctx, agentloop.LoopInput{
			TaskID: taskID, RunID: runID, PlanApprovalID: approvalID,
			WorktreePath: scope.worktree, PolicyRevision: 1,
			PolicySHA256: strings.Repeat("0", 64),
			Plan: agentloop.PlanProjection{
				Revision: plan.Revision, RepositoryRevision: scope.revision,
				Steps: steps,
			},
			RepositoryContext: context,
			ApprovedTools:     agentApprovedTools(),
			Limits:            agentLoopLimits(maximumCost),
		})
		// A malformed turn is a mistake the model made, not a failure of the
		// machinery, so it costs an attempt rather than the run.
		//
		// Every loop error used to end the run. That put a model slip — two
		// writes to one file in a turn, a call attributed to a step that
		// cannot accept it — in the same category as a database that will not
		// open, and a run with five attempts left died on the first one. The
		// attempt loop exists to absorb exactly this: the next attempt is told
		// what the loop refused and why, which is information it can act on.
		if errors.Is(runErr, agentloop.ErrMalformedModelTurn) {
			sendBack("assembly", malformedTurnInstruction(runErr),
				"the loop refused a malformed turn")
			execution.say(ctx, scope, events.KindMessageFinal, fmt.Sprintf(
				"Attempt %d was refused by the loop: %s. Trying again.",
				attempt, runErr.Error()))
			continue
		}
		// A provider that would not answer is the machinery failing, not the
		// work, and it is recoverable in a way "the database will not open"
		// is not: the next attempt may run on a different rung, and the
		// transport may simply be having a moment. Ending the run here threw
		// away everything the earlier attempts had already got past their
		// gates — observed on ladder rung 2, where six attempts of accepted
		// work were discarded because the seventh could not reach the
		// provider immediately after escalating.
		//
		// The retry executor has already exhausted its own budget by this
		// point, so this is not a second retry layer: it is the attempt loop
		// absorbing a failed attempt, which is what it is for. If the
		// provider stays unreachable the attempt budget ends the run anyway,
		// and it ends with the record of what did succeed intact.
		if errors.Is(runErr, providers.ErrRetryBudgetExhausted) ||
			errors.Is(runErr, providers.ErrTransport) ||
			errors.Is(runErr, providers.ErrRateLimited) {
			sendBackInfrastructure(runErr, providerFailureInstruction(runErr))
			// Said after the ruling, never before it.
			execution.say(ctx, scope, events.KindMessageFinal,
				providerCircuit.narrate(attempt))
			continue
		}
		// A plan this loop will never accept is not a model failure.
		//
		// A step whose completion tool does not match its kind fails
		// identically however good the model is, so retrying it spends
		// attempts and an escalation on coordinator metadata. A run did
		// exactly that: three attempts, an escalation to the most expensive
		// rung, zero files written, zero tests run, and a full pipeline dump
		// describing stages nothing had reached.
		if errors.Is(runErr, agentloop.ErrPlanContract) {
			execution.say(ctx, scope, events.KindMessageFinal,
				"This run cannot proceed: its plan does not satisfy the "+
					"agent loop's own contract ("+runErr.Error()+"). That is "+
					"a defect in this build rather than in the work or the "+
					"model, and no number of attempts will change it.")
			return runErr
		}
		if runErr != nil {
			// A loop error this does not recognise costs an attempt, not the
			// run — up to a point.
			//
			// Two named errors were absorbed here and everything else ended the
			// run outright, which put "the loop refused a tool result whose
			// identity it could not match" in the same category as "the
			// database will not open". Ladder rung 3 died on exactly that,
			// having already written a working program and needing one more
			// attempt to add a missing import; thirty-seven stages were
			// recorded as never performed because of one refused turn.
			//
			// Bounded, because an error that repeats is not recoverable by
			// repetition. Past the bound the run ends with the error, which is
			// the correct outcome for a machine that is actually broken.
			unrecognisedLoopErrors++
			if unrecognisedLoopErrors <= maximumUnrecognisedLoopErrors {
				sendBack("assembly", loopRefusalInstruction(runErr),
					"the loop refused the attempt")
				execution.say(ctx, scope, events.KindMessageFinal, fmt.Sprintf(
					"Attempt %d was refused by the loop: %s. Trying again.",
					attempt, runErr.Error()))
				continue
			}
			execution.say(ctx, scope, events.KindMessageFinal,
				"The run stopped: "+runErr.Error())
			return runErr
		}
		outcome = attemptOutcome

		// Whatever the loop thinks, the code has to compile. Nothing checked
		// that: a run whose model wrote Go that does not build reported
		// implementation-complete, and the first person to find out was
		// whoever tried to build it. The compiler's own output is what the
		// next attempt is shown, because it names the file, the line, and the
		// mistake better than any summary of it could.
		assembled = execution.assemble(ctx, scope.worktree)
		if assembled != nil {
			sendBack("assembly",
				"the code does not compile:\n"+assembled.Error(),
				"it did not compile")
			execution.say(ctx, scope, events.KindMessageFinal, fmt.Sprintf(
				"Attempt %d produced code that does not compile. Fixing it.",
				attempt))
			continue
		}

		// Everything still owed, asked for at once.
		//
		// These were separate gates that each returned on the first thing they
		// found, and the gates fought each other: a run told only to add the
		// missing cases rewrote the test file and dropped a doc comment, was
		// told only to add the doc comment, and lost the cases again. Rung 5
		// spent six attempts oscillating between exactly those two, fixing each
		// in turn and regressing the other, and neither failure repeated often
		// enough to look like a stall.
		//
		// One instruction listing every outstanding gap is the same lesson the
		// adversarial review already learned: a run shown a third of the
		// picture fixes a third of it, and the part it is not shown is the part
		// it is free to break.
		// A concrete test failure outranks every research gate.
		//
		// go build does not compile test files, so a test calling a function
		// with the wrong number of arguments passes the assembly gate and
		// arrives here. Ladder rung 3 got exactly that — "too many arguments in
		// call to run" — listed underneath a block asking for three synthesised
		// integer cases, and spent the attempt on the cases. The compiler had
		// named the file, the line and the mistake; nothing else the run could
		// have been told was worth as much.
		//
		// Sent alone, deliberately. The argument for naming everything at once
		// holds between gaps of comparable weight, where a run fixing one is
		// free to break another. It does not hold between a broken build and a
		// missing doc comment: there is nothing to trade off, because until
		// this passes none of the other checks are measuring the program that
		// will exist.
		testsHeld, testFailure := revalidateAfterWrite(ctx, scope.worktree)
		if !testsHeld {
			sendBack("integration-tests",
				"the tests do not pass. Fix this before anything else; it is "+
					"the only thing being asked for in this attempt:\n\n"+
					testFailure+
					discardRefinement(checkpoint, scope.worktree, narrator),
				"its tests do not pass")
			execution.say(ctx, scope, events.KindMessageFinal, fmt.Sprintf(
				"Attempt %d: the tests do not pass (%s). Fixing that first.",
				attempt, firstMeaningfulLine(testFailure)))
			continue
		}

		// Acceptance is next, and alone.
		//
		// A program whose tests pass and which does not do what was asked is
		// wrong in a way no amount of coverage or documentation compensates
		// for, and the failing example names the difference exactly. Listing it
		// beside three other asks invites a run to treat it as one item of
		// four, which is how a run comes to spend its attempt on doc comments
		// while the program prints the wrong thing.
		acceptanceExamples := parseAcceptanceExamples(scope.requirement)
		if _, acceptanceFailures := execution.checkAcceptance(
			ctx, scope.worktree, scope.requirement, acceptanceExamples,
		); len(acceptanceFailures) > 0 {
			sendBack("acceptance",
				"it does not do what was asked. Fix this before anything else; "+
					"it is the only thing being asked for in this attempt:\n\n"+
					acceptanceInstruction(acceptanceExamples, acceptanceFailures)+
					discardRefinement(checkpoint, scope.worktree, narrator),
				"it did not do what was asked")
			execution.say(ctx, scope, events.KindMessageFinal, fmt.Sprintf(
				"Attempt %d: %s Fixing that first.",
				attempt, acceptanceFailures[0]))
			continue
		}

		// Compiles, tests pass, does what was asked. That is worth keeping
		// before anything is allowed to refine it.
		//
		// Every attempt edits the same worktree in place, so a run that reached
		// a good state and was then sent back for coverage could rewrite its
		// way out of it and report, six attempts later, a failure it had
		// already solved once. The copy is what makes "refine" mean refine
		// rather than gamble.
		if err := checkpoint.capture(scope.worktree,
			"compiled, tests passed, acceptance matched"); err != nil {
			tracef("checkpoint", "could not capture: %v", err)
		}

		// Blind spots are asked for once; defects are asked for until they are
		// gone. The single round was right about taste and wrong about facts: a
		// surviving mutant is not an opinion about how the work could be
		// better, it is a demonstration that the tests cannot detect a defect
		// in a named line. Rung 5 had one found at attempt three, sent back
		// once, not fixed, and never raised again — and the run then reported
		// implementation-complete with a suite that caught nothing.
		//
		// PIPE-102: no longer gated behind execution.settings.AdversarialReview.
		// Plan.md §22 forbids trading away a required reviewer for a lower
		// cost, so a setting may scale what the critic checks -- PIPE-095's
		// risk-selected checks already do exactly that -- but it may not
		// delete the critic outright the way this flag used to.
		if !progress.lastAttempt() && !reviewed {
			findings, reviewErr := execution.reviewAdversariallyForRisk(
				ctx, scope.worktree, scope.riskLevel)
			if reviewErr != nil {
				findings = nil
			}
			// A criticism already made and already attempted is not made again.
			// Fingerprinted with the identifiers removed, so renaming the
			// function a finding is about cannot present the same objection as
			// a new one — rung 3 was sent back three times for what a reader
			// would call one criticism.
			var fresh []adversarialFinding
			for _, finding := range findings {
				print := findingFingerprint(finding)
				if attemptedFindings[print] {
					carriedAdvisories = append(carriedAdvisories, finding)
					continue
				}
				attemptedFindings[print] = true
				fresh = append(fresh, finding)
			}
			// What is left after the repeats are removed may be only opinions.
			// A run that builds, passes its tests and matches its acceptance
			// examples has crossed the completion floor, and spending its
			// remaining budget on a rule's opinion it has already tried once to
			// satisfy is what left rung 3 stalling past 447 seconds with a
			// verified result in hand.
			//
			// A measured defect is never advisory: the tests were actually run
			// against it and did not catch it, which is a fact about the suite
			// rather than an opinion about the code.
			blindSpotsOnlyThisRound = len(blockingFindings(fresh)) == 0
			if reviewRounds > 0 && len(blockingFindings(fresh)) == 0 {
				carriedAdvisories = append(
					carriedAdvisories, advisoryFindings(fresh)...)
				fresh = nil
			}
			findings = fresh
			if len(findings) > 0 {
				reviewRounds++
				defects := 0
				for _, finding := range findings {
					if finding.Kind == findingDefect {
						defects++
					}
				}
				// Only a review that found nothing objective closes the door.
				// Blind spots alone are the bounded ask the round limit was
				// written for.
				//
				// PIPE-100: reviewed is set on this path only, which is
				// reached only when the review actually produced findings
				// that are about to be sent back below. A review that errored
				// or found nothing never reaches this assignment, so it does
				// not spend the run's one review round -- a later attempt is
				// still reviewed for real rather than the round being consumed
				// by a pass that never actually saw the code that ships.
				reviewed = defects == 0
				execution.say(ctx, scope, events.KindMessageFinal, fmt.Sprintf(
					"Attempt %d passes every gate. A review found %d defect(s) and "+
						"%d blind spot(s) in it anyway; going back for them.",
					attempt, defects, len(findings)-defects))
				// PIPE-099: routed through sendBack exactly like every other
				// gate, so the convergence tracker sees a run repeating the
				// same review finding and can escalate or decompose on it.
				// Before this, a review finding set failure and continued
				// directly, and progress.record was never called for it, so a
				// run stuck on the same finding attempt after attempt neither
				// escalated up the model ladder nor decomposed -- the one
				// stall the rest of the loop is built to catch was invisible
				// to it from exactly this gate.
				// True, not inferred. The suite was re-run on this exact tree a
				// few lines above and passed; reading the narrator's flags here
				// instead produced an instruction telling the model its tests
				// "have not been shown to pass" in the same run whose trace
				// says a checkpoint was captured because they did.
				sendBack("adversarial-review",
					adversarialInstruction(findings, true),
					"a review found it weaker than it looks")
				continue
			}
		}

		// Reaching here means the suite ran and passed on the worktree as it
		// stands now, which is a stronger fact than the narrator's flags: those
		// record what the agent's own tool calls did, and the agent's last act
		// is almost always a write.
		outstanding := execution.outstandingWork(
			ctx, scope, caseRounds, documentationRounds, coverageRounds,
			propertyRounds, true)
		if outstanding.any() && progress.lastAttempt() {
			// Out of attempts with work still owed. Saying so is the whole
			// point: the run used to fall through here and report
			// implementation-complete while its own ledger recorded the gate
			// it had just named as failed, which is the run and the record
			// disagreeing about the same fact.
			unfinished = outstanding.summary
		}
		if outstanding.any() && !progress.lastAttempt() {
			if outstanding.askedForCases {
				caseRounds++
			}
			if outstanding.askedForDocumentation {
				documentationRounds++
			}
			if outstanding.askedForCoverage {
				coverageRounds++
			}
			if outstanding.askedForProperty {
				propertyRounds++
			}
			sendBack(outstanding.gate, outstanding.instruction, outstanding.because)
			execution.say(ctx, scope, events.KindMessageFinal, fmt.Sprintf(
				"Attempt %d: %s Going back for all of it.",
				attempt, outstanding.summary))
			continue
		}

		if outcome.Kind == agentloop.OutcomeImplementationComplete ||
			outcome.Kind == agentloop.OutcomeValidationComplete {
			// MEM-002: the attempt that ends the loop by satisfying it,
			// rather than being sent back for another one.
			execution.recordAttemptTransition(ctx, episode, attempt, "convergence", "",
				progress.currentModel(), storage.EpisodeAttemptOutcomeConverged)
			break
		}
		failure = narrator.lastFailure
		sentBackBecause = "the tests did not pass"
		if failure == "" {
			// Nothing failed and nothing completed: another attempt would ask
			// the same question and get the same answer.
			break
		}
	}

	// The loop is over. If what it left behind is worse than something it
	// reached earlier, put the earlier state back.
	//
	// This is the whole point of the copy. A run that compiled, passed its
	// tests and matched its acceptance examples, was sent back for coverage,
	// and rewrote its way out of all three, currently reports the last state it
	// happened to stop on — which is a worse answer than one it had already
	// found and paid for. Restoring costs nothing and cannot lose work, because
	// the state being replaced is by definition one that does not pass.
	//
	// Only when the current state fails. A run that ended better than its
	// checkpoint keeps what it ended with, and one that never reached a
	// checkpoint has nothing to go back to.
	restoredFromCheckpoint := false
	if providerCircuit.Open {
		execution.say(ctx, scope, events.KindMessageFinal,
			"Refinement stopped: "+providerCircuit.Reason+".")
	}
	if checkpoint.taken {
		if held, _ := revalidateAfterWrite(ctx, scope.worktree); !held {
			restoredFromCheckpoint = checkpoint.restore(scope.worktree)
			if restoredFromCheckpoint {
				// Everything the run believed about the previous tree is now
				// about a tree that no longer exists. Clearing the flags rather
				// than reasoning around them: a stale fact that survives a
				// restore is indistinguishable from a current one, and the
				// whole point of the restore is that the worktree changed.
				narrator.ranValidation = false
				narrator.validationFailed = false
				narrator.filesChangedSinceValidation = false
				narrator.lastTestFingerprint = ""
				narrator.lastFailure = ""
				assembled = execution.assemble(ctx, scope.worktree)
				execution.say(ctx, scope, events.KindMessageFinal, fmt.Sprintf(
					"The last attempts left the work failing, so it was put back "+
						"to revision %s, which %s.",
					checkpoint.digest, checkpoint.reason))
			}
		}
	}
	tracef("checkpoint", "restored=%t verified_revision=%s",
		restoredFromCheckpoint, checkpoint.digest)

	// Enrichment, after the work is verified and never before it, as a
	// transaction.
	//
	// The atom schema used to be asked of the model, which cost attempts,
	// competed with the delivery gates, and produced a worse answer than the
	// analysis already had. Deriving it is right; doing it in place was not.
	// Enrichment edited the verified worktree, revalidated, and stopped —
	// leaving the tree different from every recorded artifact and from the
	// checkpoint digest the run then reported as its verified revision. The
	// ladder read the stored bytes, found the version before enrichment, and
	// failed a run whose program was correct on disk.
	//
	// So the whole sequence commits or it does not happen: enrich, build, test,
	// re-record the artifacts, re-capture the checkpoint. Any step failing puts
	// the previous revision back.
	if checkpoint.taken && !restoredFromCheckpoint {
		if enriched := execution.enrichVerifiedWork(
			ctx, scope, checkpoint,
		); enriched > 0 {
			assembled = execution.assemble(ctx, scope.worktree)
		}
	}

	// The assembly gate. A run that cannot produce something that builds has
	// not produced a program, whatever else it did, and everything downstream
	// is a claim about a thing that does not exist.
	compiles := ledger.require(ctx, pipeline.StageAssembly, assembled == nil,
		assemblyDetail(assembled),
		map[string]any{"command": "go build ./...", "attempts": attempts})
	// What the run produced, and what it managed to check about it. These are
	// deliberately separate: a program that exists is one claim, and a program
	// whose tests were run is another, and a build that conflates them is how
	// "implementation-complete" ends up on a program that prints the wrong
	// answer.
	ledger.record(ctx, pipeline.StageProgram, programState(compiles),
		programDetail(compiles),
		map[string]any{
			"outcome":    string(outcome.Kind),
			"rounds":     outcome.Rounds,
			"tool_calls": outcome.ToolCalls,
			"attempts":   attempts,
			// What the run cost in models, not only in attempts. A run that
			// quietly climbed to the expensive model is worse than one that
			// says so: the cost lands on a bill with nothing in the record
			// explaining which runs earned it.
			"model":        progress.currentModel(),
			"convergence":  progress.summary(),
			"model_ladder": execution.settings.ModelLadder,
		})
	// The verification gate. A run may not claim to have finished work it never
	// checked, and it may not claim to have finished work whose checks failed.
	// Both used to end the same way — "implementation-complete" — which is how
	// a program that printed the wrong answer came to be reported as done.
	//
	// When files were written after the last test run, the suite is run here
	// rather than trusting a verdict about older code. Asking the model to run
	// its tests again instead was tried and was worse: the model's last act in
	// almost every attempt is a write, so the ask fired every time and the run
	// never reached the stages past this one — twelve attempts, five stages
	// recorded, no progress. The coordinator already runs this exact command
	// in three other stages; running it once more is cheaper than a round trip
	// and cannot be forgotten.
	validationHeld := narrator.ranValidation && !narrator.validationFailed
	validationReason := validationDetail(narrator)
	// A restore is a write, and the verdict has to describe the tree that
	// exists.
	//
	// filesChangedSinceValidation tracks what the agent wrote, so a restore was
	// invisible to it: the run put back a revision it had verified, and then
	// read the failed verdict of the attempt that had broken it. Ladder rung 5
	// ended with "restored=true verified_revision=28c3375abe72" and "Final
	// status: failed — the work was never verified" in the same record, about
	// the same worktree.
	if narrator.filesChangedSinceValidation || restoredFromCheckpoint {
		validationHeld, validationReason = revalidateAfterWrite(ctx, scope.worktree)
	}
	verified := compiles && ledger.require(ctx, pipeline.StageIntegrationTests,
		validationHeld, validationReason,
		map[string]any{"command": "go test ./...", "attempts": attempts})
	if !verified {
		// Only the two stages that genuinely cannot proceed are blocked here
		// (PIPE-001).
		//
		// This swept five stages, including three the run goes on to compute
		// below: the adversarial probe, the acceptance check, and the evidence
		// bundle. Stage storage is first-write-wins, so the blocked row landed
		// first and the three real verdicts were computed and then discarded.
		// A reader of a failing run's ledger saw "blocked" where the run had an
		// answer, and the evidence bundle — the thing most worth reading when
		// the news is bad — was the one most reliably lost.
		//
		// Acceptance and delivery are different: they are decisions about
		// whether to hand the work over, and an unverified run cannot reach
		// either. Those stay blocked.
		for _, stage := range []pipeline.Number{
			pipeline.StageHumanAcceptance, pipeline.StageDeliver,
		} {
			ledger.blocked(ctx, stage,
				"the run's own verification did not pass, so nothing after it "+
					"can be claimed")
		}
		outcome.Kind = agentloop.OutcomeAwaitingDirection
	}

	// The program is run, not merely built. Every command the run produced is
	// executed against input nobody intended, because a program is not correct
	// because it handles the example — it is correct when it also refuses,
	// visibly, everything it cannot handle. This was checked outside the
	// product until now, which meant the product could not tell you whether
	// what it built was robust; it could only tell you that it compiled.
	if compiles {
		survived, probed, adversarialErr := execution.probeProducedCommands(
			ctx, scope.worktree)
		ledger.require(ctx, pipeline.StageAdversarial,
			adversarialErr == nil && len(survived) == 0,
			adversarialDetail(survived, probed, adversarialErr),
			map[string]any{"commands_probed": probed, "findings": survived})
	} else {
		ledger.blocked(ctx, pipeline.StageAdversarial,
			"nothing that builds exists to probe")
	}

	acceptance, _ := execution.checkAcceptance(
		ctx, scope.worktree, scope.requirement, examples)
	ledger.decide(ctx, pipeline.StageEndToEndTests, acceptance)

	tracef("flow", "compiles=%t verified=%t — structure stages %s",
		compiles, verified,
		map[bool]string{true: "run their checks", false: "record blocked"}[compiles])
	execution.examineStructure(ctx, ledger, scope, compiles, verified)
	if compiles {
		recalled, registration := execution.recallKnownAtoms(
			ctx, scope, scope.worktree)
		ledger.decide(ctx, pipeline.StageRecall, recalled)
		// Recorded through the ledger like every other stage. These two used to
		// write their own rows directly and the ledger separately declared them
		// not-implemented, so one run's records said both that registration had
		// run and that no part of this build performs it.
		ledger.decide(ctx, pipeline.StageAtomRegistration, registration.atom)
		ledger.decide(ctx, pipeline.StageMoleculeRegistration, registration.molecule)
	} else {
		ledger.blocked(ctx, pipeline.StageRecall,
			"nothing was produced, so nothing could have been reused instead")
	}

	// The evidence is assembled last and always, because a reader deciding
	// whether to trust this work needs it most when the news is bad. Whether
	// the work may then be handed over is a separate question, and the answer
	// is no while anything is still failing.
	bundle, clean := execution.assembleEvidence(ctx, taskID, ledger.currentAttempt())
	ledger.decide(ctx, pipeline.StageEvidenceBundle, bundle)
	// The completion floor and the refinement ceiling, kept apart.
	//
	// The floor is: it builds, its tests pass, and it does what was asked. A run
	// that has crossed it has produced something a person can review, and
	// telling them "the work is not ready to be looked at" because branch
	// coverage came to 84% rather than the threshold, or because an atom
	// carries no registry documentation, is untrue in the way that matters: it
	// sends a reviewable program back into the queue.
	//
	// The ceiling is everything above that — coverage thresholds, registry
	// metadata, a critic's remaining opinions. Missing it is worth saying and
	// is not worth withholding the work for.
	//
	// Delivery is unchanged either way. Nothing is handed over before a person
	// accepts it, and that is not a judgement this run is entitled to make.
	switch {
	case clean && verified:
		ledger.record(ctx, pipeline.StageHumanAcceptance, pipeline.StateBlocked,
			"the work is ready and nobody has looked at it yet: acceptance is "+
				"a person's decision and this run cannot make it", nil)
		ledger.record(ctx, pipeline.StageDeliver, pipeline.StateBlocked,
			"nothing is delivered before a person accepts it", nil)
	case checkpoint.taken && verified:
		// "Ready with advisories" is reserved for a run whose hard gates all
		// held. Rung 6 said it while a hard property-test gate had failed and
		// the task was moving to a stopped state, which is the ledger being
		// cheerful about something it had just refused.
		ledger.record(ctx, pipeline.StageHumanAcceptance, pipeline.StateBlocked,
			"the completion floor passes on revision "+checkpoint.digest+
				" — it "+checkpoint.reason+" — but this is not ready for "+
				"review: a required gate did not hold, and what it is is "+
				"recorded above", nil)
		ledger.record(ctx, pipeline.StageDeliver, pipeline.StateBlocked,
			"nothing is delivered before a person accepts it", nil)
	default:
		ledger.record(ctx, pipeline.StageHumanAcceptance, pipeline.StateBlocked,
			"the work is not ready to be looked at: something it was checked "+
				"against did not hold", nil)
		ledger.record(ctx, pipeline.StageDeliver, pipeline.StateBlocked,
			"nothing that failed its own checks is delivered", nil)
	}

	// The skip audit reads every other stage's row, so it runs after they are
	// all written and before close() sweeps the flow. Left to the sweep it would
	// record not-implemented for itself, which is the one verdict it exists to
	// make impossible to reach by accident (PIPE-042).
	ledger.decide(ctx, pipeline.StageSkipAudit,
		checkSkipAudit(ctx, execution.repositories, scope.taskID, ledger.currentAttempt()))

	execution.reportGraphFailure(ctx, scope, recorder)
	if journalErr := journal.Failure(); journalErr != nil {
		execution.say(ctx, scope, events.KindMessageFinal,
			"The work is done, but its durable tool record is incomplete: "+
				journalErr.Error())
	}
	// The ledger is completed before the run says it is finished. Left to the
	// deferred sweep it closed *after* the final message, so anything reading
	// the record the moment the run announced completion could see a partial
	// flow and conclude stages were missing when they were merely late.
	ledger.close(ctx)
	execution.publishValidation(ctx, scope, outcome)
	finishedNote := completionCaveat(verified)
	if unfinished != "" {
		finishedNote = "It ran out of attempts with work still owed: " +
			unfinished + ". " + finishedNote
	}
	execution.say(ctx, scope, events.KindMessageFinal, fmt.Sprintf(
		"Finished: %s after %d round(s) and %d tool call(s). %s",
		outcome.Kind, outcome.Rounds, outcome.ToolCalls, finishedNote))
	// AUDIT-020: the completion call site. Everything above this line already
	// existed; what did not was anything that moved a finished run out of
	// "running". This records the evidence PrepareCompletion requires from the
	// validation the run's own loop already ran — it does not run anything
	// itself — and, only if every declared plan step reached durable validated
	// state, moves the task into awaiting-review. A run whose own validation
	// did not pass, or whose code does not compile, is left running with the
	// reason said above rather than completed on a weaker check.
	finished := execution.completeRunIfPossible(
		ctx, scope, taskID, runID, plan, steps, compiles, verified, clean)
	// A run may not return with its task still running.
	//
	// Completion had fifteen bare returns in it, every one a real decision —
	// the evidence did not resolve, a required stage did not hold, the revision
	// could not be read — and every one left the task in `running` with nothing
	// recorded. The ladder then waited for a terminal state nothing was going
	// to write: rung 3 verified its work at 60 seconds, finished its checks at
	// 71, and sat there until the enclosing timeout.
	//
	// A message saying a run has ended is not an ending. The durable state is.
	if !finished.Terminal {
		// A run stopped by the machinery leaves a draft worth resuming, which
		// is a different ending from work that was checked and found wanting.
		recoverable := providerCircuit.Open &&
			providerCircuit.Disposition == circuitRecoveryRequired
		finished = execution.finaliseNonTerminalRun(
			ctx, scope, taskID, compiles && verified, recoverable,
			finished.Reason)
	}

	// The terminal record, last, once the state it describes is durable.
	//
	// It used to be written before this, which made it a prediction: it named a
	// verified revision and a status while the task was still running, and the
	// two could disagree with nobody the wiser. Now it reports what the store
	// will answer if someone asks.
	report := terminalReport(terminalFacts{
		status:           string(finished.TaskState),
		reason:           finished.Reason,
		floorHeld:        compiles && verified,
		gatesHeld:        clean,
		verifiedRevision: checkpoint.digest,
		verifiedBecause:  checkpoint.reason,
		currentIsVerified: checkpoint.taken && (restoredFromCheckpoint ||
			producedTreeDigest(scope.worktree) == checkpoint.digest),
		advisories:            carriedAdvisories,
		attempts:              attempts,
		infrastructureRetries: infrastructure.Consecutive,
		unresolved:            unfinished,
	})
	tracePhaseTotals()
	traceBlock("final", "how this run ended:", report)
	execution.say(ctx, scope, events.KindMessageFinal, report)
	return nil
}

// entityRevisions counts each projected entity's own revision.
//
// The client rebuilds tool, plan, and validation state by applying updates in
// order, and it requires each entity's revision to increment by exactly one. A
// run publishing everything at revision zero produced a projection the client
// refused to build, so it never connected at all and the interface reported
// "Disconnected" while the coordinator was running an agent perfectly well.
type entityRevisions struct {
	tool       uint64
	plan       uint64
	validation uint64
}

// next returns the revision one entity's next update carries.
func (revisions *entityRevisions) next(counter *uint64) uint64 {
	*counter++
	return *counter
}

// agentScope is everything one run needs about its task.
type agentScope struct {
	revisions      *entityRevisions
	requestMessage *domain.MessageID
	repositoryID   domain.RepositoryID
	graph          *agentGraphRecorder
	projectID      domain.ProjectID
	sessionID      domain.SessionID
	threadID       domain.ThreadID
	taskID         domain.TaskID
	worktree       string
	revision       string
	requirement    string
	// riskLevel is the risk the task already carries.
	//
	// The requirement is analysed again here to plan the work, and that
	// analysis has its own opinion about risk. The store checks the two against
	// each other, so the task's is taken as the authority: intake decided it,
	// the person was shown it, and a run must not quietly reclassify the work
	// it was approved to do.
	riskLevel domain.RiskLevel
}

// resolveScope reads the task, its conversation, and its worktree.
func (execution *AgentExecution) resolveScope(
	ctx context.Context,
	taskID domain.TaskID,
) (agentScope, error) {
	task, err := execution.repositories.GetTask(ctx, taskID)
	if err != nil {
		return agentScope{}, fmt.Errorf("read the task: %w", err)
	}
	thread, err := execution.repositories.GetThread(ctx, task.ThreadID)
	if err != nil {
		return agentScope{}, fmt.Errorf("read the thread: %w", err)
	}
	session, err := execution.repositories.GetThreadSession(ctx, thread.ID)
	if err != nil {
		return agentScope{}, fmt.Errorf("read the session: %w", err)
	}
	binding, err := execution.worktrees.GetWorktreeBinding(ctx, taskID)
	if err != nil {
		return agentScope{}, fmt.Errorf("read the task worktree: %w", err)
	}
	repository, err := execution.repositories.GetRepository(ctx, task.RepositoryID)
	if err != nil {
		return agentScope{}, fmt.Errorf("read the repository: %w", err)
	}
	return agentScope{
		revisions:      &entityRevisions{},
		requestMessage: task.RequestMessageID,
		repositoryID:   task.RepositoryID,
		projectID:      repository.ProjectID,
		sessionID:      session.ID, threadID: thread.ID, taskID: taskID,
		riskLevel: task.RiskLevel,
		worktree:  binding.WorktreePath, revision: repository.GitIdentity,
		requirement: execution.requirementOf(ctx, task),
	}, nil
}

// requirementOf reads the request the task was created from.
func (execution *AgentExecution) requirementOf(
	ctx context.Context,
	task storage.Task,
) string {
	if task.RequestMessageID == nil {
		return ""
	}
	message, err := execution.repositories.GetMessage(ctx, *task.RequestMessageID)
	if err != nil {
		return ""
	}
	// Deliberately not trimmed here (PIPE-019a). storage.RecordTaskRequirement
	// independently re-reads this same message row and re-derives its own
	// analysis from those exact bytes, so trimming on this side alone would
	// only make this copy agree with a hand-trimmed one while the store's
	// side still read the untrimmed row. The fix is that
	// storage.AnalyzeTaskRequirement now normalizes internally before it
	// derives anything, so every caller — this one, recordDurablePlan, and
	// RecordTaskRequirement's own re-derivation — computes the same analysis
	// from the same untrimmed bytes without needing to agree on trimming
	// beforehand. See PIPE-019a.
	return message.BodyRedacted
}

// agentPermissionPolicy is what a run is allowed to do.
//
// Writes inside the task's own worktree are automatic; a subprocess is not, and
// only the project's own test command is named. Everything else reaches the
// approval path rather than running.
func agentPermissionPolicy(
	taskID domain.TaskID,
	worktree string,
) executor.PermissionPolicy {
	return executor.PermissionPolicy{
		TaskID: taskID, WorktreePath: worktree, TaskActive: true,
		ApprovedCommands: []executor.ActionPattern{{
			TaskID: taskID, Tool: executor.ToolTest,
			Arguments:        []string{"go", "test", "./..."},
			WorkingDirectory: filepath.Clean(worktree),
			Effects: []executor.SideEffect{
				executor.EffectRepositoryRead, executor.EffectSubprocess,
			},
		}},
	}
}

// agentLoopLimits are the ceilings one run may not exceed.
func agentLoopLimits(maximumCost providers.ExactAmount) agentloop.LoopLimits {
	return agentloop.LoopLimits{
		MaximumRounds: 16, MaximumToolCalls: 60, MaximumToolCallsPerRound: 6,
		MaximumTokens: 600000, MaximumTokensPerRound: 150000,
		MaximumWallClock: 12 * time.Minute, MaximumCost: maximumCost,
		MaximumIdenticalFailures: 3, MaximumContextItems: 20,
		MaximumFactualEvents: 40, MaximumContextBytes: 400000,
		MaximumResultBytes: 60000,
	}
}

// worktreeContextItems seeds the first round with what the worktree holds.
func worktreeContextItems(worktree string) []agentloop.RepositoryContextItem {
	entries, err := os.ReadDir(worktree)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Name() == ".git" {
			continue
		}
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		names = append(names, name)
	}
	items := []agentloop.RepositoryContextItem{
		agentContextItem("worktree-root-listing", strings.Join(names, "\n")),
	}
	for _, candidate := range []string{"go.mod", "README.md"} {
		content, readErr := os.ReadFile(filepath.Join(worktree, candidate))
		if readErr == nil && len(content) < 8000 {
			items = append(items, agentContextItem(candidate, string(content)))
		}
	}
	return items
}

// buildAgentExecution assembles the agent when a model is available.
//
// A coordinator with no provider key returns nothing rather than failing to
// start: the rest of the product — opening a repository, recording a request,
// reviewing a diff — works without one, and refusing to boot would take all of
// that away over a key the person may be about to set.
func buildAgentExecution(
	options ApplicationOptions,
	repositories *storage.Repositories,
	hub *events.Hub,
	checkpointing applicationCheckpointing,
	graphs *GraphProjectionService,
) (*AgentExecution, error) {
	settings := settingsOrDefaults(options.Settings)
	model := options.AgentModel
	buildNamed := options.AgentModelFactory
	if buildNamed != nil && model == nil {
		built, err := buildNamed(settings.FirstRung())
		if err != nil {
			return nil, err
		}
		model = built
	}
	if model == nil {
		directory := options.AgentWorkingDirectory
		if directory == "" {
			working, err := os.Getwd()
			if err != nil {
				return nil, nil
			}
			directory = working
		}
		key := ReadProviderKey(directory)
		if key == "" {
			return nil, nil
		}
		style := settings.CodeStyle
		buildNamed = func(name string) (agentloop.FixedModel, error) {
			return newDefaultAgentModel(key, name, style)
		}
		built, err := buildNamed(settings.FirstRung())
		if err != nil {
			return nil, err
		}
		model = built
	}
	execution, err := NewAgentExecution(
		repositories, hub, repositories, model, checkpointing.redactor, graphs,
	)
	if err != nil {
		return nil, err
	}
	execution.escalate = buildNamed
	built, err := execution.WithSettings(settings)
	if err != nil {
		return nil, err
	}
	// AUDIT-020: constructed here, at the same site that already refuses to
	// leave the coordinator half-wired over a missing provider key above.
	// Completion is genuinely optional in the same sense a model is: a
	// coordinator built without repositories or a redaction pipeline (a bare
	// test fixture, most often) runs exactly as it did before this existed,
	// and Run reports that plainly rather than failing to start over a
	// surface most callers never touch.
	if completion, validations, gate, completionErr :=
		buildAgentExecutionCompletion(repositories, checkpointing); completionErr == nil {
		built.completion = completion
		built.completionValidations = validations
		built.completionGate = gate
	}
	return built, nil
}

// settingsOrDefaults reads the zero value as "the defaults", not "empty".
//
// A caller that says nothing about settings wants a working run, not one
// refused for values it did not know it had to supply.
func settingsOrDefaults(settings pipeline.Settings) pipeline.Settings {
	if settings.Ambiguity == "" {
		return pipeline.DefaultSettings()
	}
	return settings
}

// newDefaultAgentModel builds the model a coordinator uses when none was given.
//
// The rates are declared here rather than discovered, and they are the reason
// this is a named function: a price is an operator's fact, and burying a guess
// inside the constructor would put an invented number into every budget the
// product enforces.
func newDefaultAgentModel(
	key string,
	named string,
	style string,
) (agentloop.FixedModel, error) {
	rung, err := pipeline.ParseRung(named)
	if err != nil {
		return nil, err
	}
	perMillion := func(dollars int64) providers.ExactAmount {
		amount, err := providers.NewExactAmount("USD", dollars, 1_000_000)
		if err != nil {
			return providers.UnknownAmount("USD")
		}
		return amount
	}
	// Rates per model, because escalation exists to spend more and a ladder
	// that priced every rung at the cheap model's rates would report the same
	// cost whichever model ran. The budget the product enforces would then be
	// enforcing a number that stopped being true the moment a run escalated.
	//
	// Effort does not appear here, and that is the point of ordering the ladder
	// the way it is: raising effort bills more tokens at the rate already in
	// force, so the second rung costs more than the first without costing more
	// per token. Changing model is what moves these numbers.
	input, output := int64(2), int64(8)
	if rung.Model == pipeline.ModelSol {
		input, output = 10, 40
	}
	return openaimodel.New(openaimodel.Options{
		APIKey:         key,
		Model:          rung.Model,
		Effort:         rung.Effort,
		StyleDirective: styleDirectiveFor(style),
		Price: providers.TokenPrice{
			Input: perMillion(input), CachedInput: perMillion(input / 2),
			CacheWrite: perMillion(input), Output: perMillion(output),
			Reasoning: perMillion(output),
		},
	})
}

// graphPlanStepsOf renders plan steps for the diagram.
func graphPlanStepsOf(steps []agentloop.PlanStep) []graphPlanStep {
	result := make([]graphPlanStep, 0, len(steps))
	for _, step := range steps {
		result = append(result, graphPlanStep{ID: step.ID, Summary: step.SummaryRedacted})
	}
	return result
}

// reportGraphFailure says once why the diagram stopped being drawn.
//
// A panel that is simply empty is indistinguishable from a run that did
// nothing, so the reason is put where a person is already looking.
func (execution *AgentExecution) reportGraphFailure(
	ctx context.Context,
	scope agentScope,
	recorder *agentGraphRecorder,
) {
	failure := recorder.Failure()
	if failure == nil || recorder.reported {
		return
	}
	recorder.reported = true
	execution.say(ctx, scope, events.KindMessageFinal,
		"The task diagram stopped updating: "+failure.Error())
}

// adoptDurablePlanSteps renames the loop's steps to the recorded plan's own.
//
// The loop and the store each had their own step identities for the same plan,
// paired only by position. Everything downstream — the tool journal, the plan
// projection, the diagram — names a step, and each of them named one the stored
// plan did not have. One vocabulary removes the whole class of mismatch rather
// than translating between them at every boundary.
func adoptDurablePlanSteps(
	steps []agentloop.PlanStep,
	plan durablePlan,
) []agentloop.PlanStep {
	if len(plan.Steps) == 0 {
		return steps
	}
	adopted := make([]agentloop.PlanStep, 0, len(steps))
	for _, step := range steps {
		if durable, present := plan.Steps[step.ID]; present {
			step.ID = durable
		}
		adopted = append(adopted, step)
	}
	return adopted
}

// pricedModel is a model that declares what it charges.
//
// The rates are an operator's fact rather than something a client discovers,
// so a model that does not declare them registers as pricing that is not
// known. That is a true statement; a zero rate would not be.
type pricedModel interface {
	Price() providers.TokenPrice
}

// registerProvider records the provider this run's model belongs to.
func (execution *AgentExecution) registerProvider(
	ctx context.Context,
) (storage.RegisteredProvider, error) {
	identity := execution.model.Identity()
	registration := storage.EnsureProviderRegistration{
		DisplayName:     identity.Provider.Provider,
		ProviderType:    identity.Provider.Adapter,
		AdapterName:     identity.Provider.Adapter,
		AdapterVersion:  identity.Provider.AdapterVersion,
		ProviderVersion: identity.Provider.ProviderVersion,
		// The endpoint is the adapter's own, and it is recorded redacted
		// because a configured endpoint can carry a key in its query.
		EndpointRedacted: identity.Provider.Provider + "/" + identity.Provider.Adapter,
		CapabilitiesJSON: `{"tools":true}`,
		ModelIdentifier:  identity.Model,
		ModelVersion:     identity.Revision,
	}
	if priced, declares := execution.model.(pricedModel); declares {
		registration.Currency, registration.Prices = providerPrices(priced.Price())
	}
	return execution.repositories.EnsureProviderRegistration(ctx, registration)
}

// providerPrices renders declared rates in the store's own vocabulary.
//
// A rate is per million tokens here and per token denominator there, and both
// are exact integers rather than a converted float, so the conversion carries
// the denominator through instead of computing a rate.
func providerPrices(
	price providers.TokenPrice,
) (domain.CurrencyCode, []storage.ProviderPriceComponent) {
	var currency domain.CurrencyCode
	var components []storage.ProviderPriceComponent
	for kind, amount := range map[string]providers.ExactAmount{
		"input": price.Input, "cached-input": price.CachedInput,
		"cache-write": price.CacheWrite, "output": price.Output,
		"reasoning": price.Reasoning,
	} {
		if !amount.Known || amount.Denominator < 1 {
			continue
		}
		parsed, err := domain.ParseCurrencyCode(amount.Currency)
		if err != nil {
			continue
		}
		currency = parsed
		components = append(components, storage.ProviderPriceComponent{
			UsageKind:      kind,
			MinorNumerator: amount.Numerator, TokenDenominator: amount.Denominator,
		})
	}
	sort.Slice(components, func(first, second int) bool {
		return components[first].UsageKind < components[second].UsageKind
	})
	return currency, components
}

// styleDirectiveFor renders the house style a project asked for.
//
// The neutral directive states only what is true of any working program in any
// style. Everything beyond it is a preference, and a preference the engine
// applies without being asked is a bias wearing the clothes of a standard: it
// would make the pipeline produce one kind of code well and quietly penalise
// every other kind.
func styleDirectiveFor(style string) string {
	if style == pipeline.StyleFunctional {
		return neutralStyleDirective + "\n\n" + FunctionalStyleDirective
	}
	return neutralStyleDirective
}

// neutralStyleDirective is what any working program owes, whatever its shape.
//
// Each line here is a property an imperative program, an object-oriented one
// and a functional one all need. Nothing in it prefers one over another.
const neutralStyleDirective = `Write complete, working code.
- No placeholders, no TODOs, no elided bodies, no functions that return a fixed value standing in for real work.
- Report failures rather than discarding them. A caller must be able to tell a failure from a result.
- Match the conventions already in the repository: its naming, its error handling, its comment density.
- Prefer the clearest expression of the task over the shortest or the cleverest.

Every function you write is finished only when all three of these are true:
- It has a test that calls it directly, not merely through something else that uses it. Cover the cases where it refuses or returns nothing, not only the case where it works.
- It has a doc comment on the declaration, starting with its name, saying what it is for, what its arguments mean, what it returns, and how it works.
- It does one thing.

A function that compiles and passes a test of its caller is not finished. Write the test and the comment as you write the function, not afterwards.`

// FunctionalStyleDirective is one house style a project may ask for.
//
// It is a house style, not a rule the tools enforce, and it is stated once here
// so every run is shaped the same way rather than depending on how a particular
// request happened to be worded. The evidence for stating it: asked for a
// program with no style named, the model produced one imperative main with the
// whole computation inlined and no function boundary anywhere in it — code that
// worked and that nothing could test, because there was nothing to call. Asked
// for a pure core, the same model produced one immediately. The difference was
// the specification, not the capability.
//
// It asks for the shape rather than the vocabulary. Go has no sum types and no
// do-notation, and a directive demanding Haskell in Go would produce something
// worse than either. What transfers is: small total functions, values instead
// of mutation, effects pushed to the edge, and errors carried as data.
const FunctionalStyleDirective = `Write in a functional style.
- Build the program out of small, total functions that each do one thing and are named for what they return, not for what they do to something.
- Keep the computation pure: a function's result must depend only on its arguments. Do not read the clock, the environment, the filesystem, or global state inside it.
- Push every effect to the edge. Input, output, and failure reporting belong in main or in a thin shell around the core; the core neither prints nor reads.
- Prefer returning new values to mutating existing ones. Build and return a result rather than filling in a variable declared above.
- Carry failure as a value that flows through the computation and is handled at the edge. Never discard an error, and never let a failure exit silently.
- Compose the pipeline out of those functions rather than inlining the steps into one body.
- Give the program a testable seam and test that, not main. Put the work in a function taking an io.Reader and an io.Writer and returning an error, and let main do nothing but wire os.Stdin, os.Stdout and os.Args to it and report failure. Tests then pass a strings.Reader and a bytes.Buffer, which cannot deadlock.
- Never reassign os.Stdin or os.Stdout in a test. Swapping them for an os.Pipe and then reading to EOF blocks forever unless the write end is closed first, and a deferred close runs too late — that is a hang, not a failure, and it stops the run rather than reporting anything. The built program is already run against the acceptance examples, so a test never needs to fake standard input.
- Assert that a failure happened, not what it was called. Unless the request states the wording of an error, a test must check that an error was returned, that it is the sentinel or type the code declares, and that no plausible-looking value came back with it. Never assert the exact text of a message the request did not specify: the wording is the implementation's to choose, a test that pins it fails the next time anyone rephrases it, and it turns a correct program into a failing one for a reason nobody asked about.
- Change an existing file with apply-patch, not by rewriting it. A whole-file write to move ten lines replaces the other two thousand, and every one of them is a chance to drop a function, drift a signature, or reword an output literal by accident. Write the patch as "*** Update File: <path>", then "@@", then the lines to remove prefixed "-", the lines to add prefixed "+", and enough unprefixed lines around them to say where the change goes. Use the whole-file tool to create a file, and to replace one only when most of it is genuinely changing.
- This is a house style, not a licence to invent machinery: use plain Go, and do not add abstractions the task does not need.`

// validationDetail says what the run's own verification actually did.
func validationDetail(narrator *narratingExecutor) string {
	switch {
	case !narrator.ranValidation:
		return "the run never reached its validation step, so nothing it " +
			"produced has been checked"
	case narrator.validationFailed:
		return "the project's own test command ran and did not pass"
	case narrator.filesChangedSinceValidation:
		return "files were written after the last test run, so what the run " +
			"leaves behind has not been checked as it stands"
	default:
		return "the project's own test command ran and passed in the worktree"
	}
}

// completionCaveat is what a person is told about how much the result is worth.
//
// A run that verified its work and one that did not both used to end with the
// same sentence. The difference is the only thing that decides whether the
// change is ready to look at, so it is said in the message rather than left in
// a table somebody would have to go and read.
func completionCaveat(verified bool) string {
	if verified {
		return "The change is in this task's worktree and its tests pass."
	}
	return "The change is in this task's worktree but it is NOT verified: " +
		"treat it as a draft, not as finished work."
}

// assemble reports whether what the run wrote actually builds.
//
// It compiles the whole module rather than only the package the plan named. A
// change that builds in isolation and breaks its caller is still a broken
// repository, and the person who would find that out is the next one to run
// the build.
func (execution *AgentExecution) assemble(
	ctx context.Context,
	worktree string,
) error {
	build := exec.CommandContext(ctx, "go", "build", "./...")
	build.Dir = worktree
	output, err := build.CombinedOutput()
	if err == nil {
		return nil
	}
	// The compiler's own words are kept, bounded. A summary would drop the
	// file and line, which are the only parts that make the next attempt
	// cheaper than the last one.
	message := strings.TrimSpace(string(output))
	const bound = 4000
	if len(message) > bound {
		message = message[:bound] + "\n… (truncated)"
	}
	if message == "" {
		message = err.Error()
	}
	return errors.New(message)
}

// assemblyDetail says what the compiler found, for the record.
func assemblyDetail(assembled error) string {
	if assembled == nil {
		return "the module compiles"
	}
	return "the module does not compile: " + assembled.Error()
}

// programState reports whether a program exists, rather than whether files do.
//
// Files that do not compile are not a program. Recording this as satisfied
// because something was written is how a run comes to report that it produced
// something it did not.
func programState(compiles bool) pipeline.State {
	if compiles {
		return pipeline.StateSatisfied
	}
	return pipeline.StateFailed
}

// programDetail says what the run actually left behind.
func programDetail(compiles bool) string {
	if compiles {
		return "the run left work in the task worktree and it builds"
	}
	return "the run left files in the task worktree but they do not build, " +
		"so no program was produced"
}

// decompositionGaps compares what the request named against what the plan
// covers.
//
// It returns the files the request asked for that no step will write, and the
// files steps will write that the request never mentioned. Both are defects
// and they are different ones: the first drops work, the second invents it.
//
// A request that names no file at all is the exception, and it is the common
// case rather than a corner: "write a program that prints the sum of its
// arguments" delegates the layout to the run, which is the product's whole
// premise. Judging planned files against an empty asked-set marked every one
// of them as invented, so the gate could not be passed by any plan whatsoever
// — the model wrote correct code and the flow refused it at stage four,
// reproduced on two unrelated ladder rungs. Delegation is not invention: with
// nothing named there is no stated intent for a planned file to contradict,
// so the second arm has nothing to say and stays silent. The first arm is
// unaffected either way, because a request that named nothing cannot have
// named something a step then dropped.
func decompositionGaps(
	requirement string,
	steps []agentloop.PlanStep,
) (uncovered []string, unasked []string) {
	analysis, err := storage.AnalyzeTaskRequirement(requirement)
	if err != nil {
		return nil, nil
	}
	planned := map[string]bool{}
	for _, step := range steps {
		for _, file := range step.ExpectedFiles {
			planned[file] = true
		}
	}
	asked := map[string]bool{}
	for _, file := range analysis.ExplicitFiles {
		asked[file] = true
		if !planned[file] {
			uncovered = append(uncovered, file)
		}
	}
	// Nothing named means the layout was delegated, so there is no stated
	// intent for a planned file to contradict. See this function's own
	// comment: without this the gate is unpassable for every request that
	// describes behaviour rather than files.
	if len(asked) > 0 {
		for file := range planned {
			// A test file the run adds for a source file the request named is
			// wanted work, not invented work: the request asked for the
			// behaviour and the product requires it be tested.
			if asked[file] || asked[strings.TrimSuffix(file, "_test.go")+".go"] {
				continue
			}
			unasked = append(unasked, file)
		}
	}
	sort.Strings(uncovered)
	sort.Strings(unasked)
	return uncovered, unasked
}

// decompositionDetail says which way the decomposition failed.
func decompositionDetail(uncovered, unasked []string) string {
	switch {
	case len(uncovered) == 0 && len(unasked) == 0:
		return "every file the request named is covered by a step, and no " +
			"step writes a file the request did not ask for"
	case len(unasked) == 0:
		return "the request named files no step will write: " +
			strings.Join(uncovered, ", ")
	case len(uncovered) == 0:
		return "steps will write files the request never named: " +
			strings.Join(unasked, ", ")
	default:
		return "the request named " + strings.Join(uncovered, ", ") +
			" with no step, and steps will write " +
			strings.Join(unasked, ", ") + " which nobody asked for"
	}
}

// acceptanceDetail states what the request is judged against (PIPE-019).
func acceptanceDetail(count int) string {
	if count == 0 {
		return "the request carries no executable acceptance example, so " +
			"nothing external would check whether the finished work does " +
			"what was asked"
	}
	return fmt.Sprintf("the request was recorded as a message and carries %d "+
		"executable acceptance example(s) the finished work must satisfy",
		count)
}

// revalidateAfterWrite runs the project's suite because the worktree moved
// since the last time the run ran it.
//
// The verdict this replaces was about code that no longer exists: a run that
// tested, then edited, then stopped reported "the project's own test command
// ran and passed" for a worktree that did not build. Rather than refuse such a
// run outright — which fails it for something a single command would settle —
// the command is run here and the real answer recorded.
func revalidateAfterWrite(ctx context.Context, worktree string) (bool, string) {
	command := exec.CommandContext(ctx, "go", "test", suiteTimeout, "-count=1", "./...")
	command.Dir = worktree
	output, err := command.CombinedOutput()
	if err == nil {
		return true, "files were written after the run's own last test, so " +
			"the suite was run again here and passed"
	}
	return false, "files were written after the run's own last test, so the " +
		"suite was run again here and did not pass: " +
		firstMeaningfulLine(string(output))
}

// firstMeaningfulLine is the first line of command output worth quoting.
//
// Go prints package status lines before it prints what broke, so the first
// line of a failing run is routinely "? pkg [no test files]" — which reads as
// success and is why a failure once went unnoticed in a stage's own detail.
func firstMeaningfulLine(output string) string {
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "?") ||
			strings.HasPrefix(trimmed, "ok ") {
			continue
		}
		if len(trimmed) > 200 {
			return trimmed[:200]
		}
		return trimmed
	}
	return "the suite reported no readable failure"
}

// providerOutcomeOf names what the transport did, as a typed outcome rather
// than a sentence about it.
//
// The distinction is recorded because the three are not the same problem: a
// rate limit clears on its own, an exhausted retry budget means the provider
// answered and kept failing, and a transport error may be this machine's
// network. Reading any of them off console text would collapse them into one.
func providerOutcomeOf(err error) string {
	switch {
	case errors.Is(err, providers.ErrRateLimited):
		return "rate-limited"
	case errors.Is(err, providers.ErrRetryBudgetExhausted):
		return "retry-budget-exhausted"
	case errors.Is(err, providers.ErrTransport):
		return "transport-failed"
	default:
		return "unavailable"
	}
}

// terminalFacts is everything a reader needs to know how a run ended.
type terminalFacts struct {
	status string
	reason string
	// floorHeld, gatesHeld and currentIsVerified are three separate questions
	// that one boolean used to answer badly.
	//
	// Does the tree that exists now build, pass its tests and do what was
	// asked; did every required gate hold; and is this tree the one the
	// checkpoint verified. Rung 5 restored a verified revision and then
	// reported "the work was never verified" — true of the gates, false of the
	// tree, and stated as though it were both.
	floorHeld             bool
	gatesHeld             bool
	verifiedRevision      string
	verifiedBecause       string
	currentIsVerified     bool
	advisories            []adversarialFinding
	attempts              int
	infrastructureRetries int
	unresolved            string
}

// terminalStatus names the ending in the vocabulary a reader can act on.
//
// The three endings are genuinely different decisions. A run that finished with
// a verified revision and some opinions left over is work someone can review; a
// run that lost its provider after verifying something is work someone can
// review and a machine to look at; a run with nothing verified is neither.
func terminalStatus(
	circuitOpen string, checkpoint *verifiedCheckpoint, unfinished string,
) string {
	switch {
	case circuitOpen == "deadline-reserved-for-finalisation" && checkpoint.taken:
		return "completed-with-advisories"
	case circuitOpen != "" && checkpoint.taken:
		return "provider-unavailable-after-verified-result"
	case circuitOpen != "":
		return "provider-unavailable-with-nothing-verified"
	case checkpoint.taken && unfinished != "":
		return "completed-with-advisories"
	case checkpoint.taken:
		return "completed"
	default:
		return "stopped-with-nothing-verified"
	}
}

// terminalReport renders the ending as something a person reads once and knows
// where they stand.
func terminalReport(facts terminalFacts) string {
	var report strings.Builder
	fmt.Fprintf(&report, "Final status: %s", facts.status)
	if facts.reason != "" {
		fmt.Fprintf(&report, " — %s", facts.reason)
	}
	report.WriteString(".\n")
	// Three facts, stated separately, because one boolean answered them badly.
	//
	// Whether the tree that exists now passes the completion floor, whether
	// every required gate held, and whether this tree is the one the checkpoint
	// verified are three different questions. Rung 5 restored a verified
	// revision and reported "the work was never verified" — true of the gates,
	// false of the tree, and said as though it were both.
	switch {
	case facts.floorHeld:
		report.WriteString(
			"The worktree builds, passes its tests, and reproduces the " +
				"acceptance examples.\n")
	case facts.verifiedRevision != "":
		report.WriteString(
			"The worktree does not currently pass the completion floor, " +
				"though a revision of this work did.\n")
	}
	if !facts.gatesHeld {
		report.WriteString(
			"Not every required gate held; what is outstanding is listed " +
				"below.\n")
	}
	if facts.verifiedRevision == "" {
		report.WriteString(
			"No revision of this work was ever verified, so there is nothing " +
				"here that is known to build, pass its tests and do what was " +
				"asked.\n")
	} else {
		fmt.Fprintf(&report,
			"Verified revision %s, which %s.\n",
			facts.verifiedRevision, facts.verifiedBecause)
		if facts.currentIsVerified {
			report.WriteString("The worktree is that revision.\n")
		} else {
			report.WriteString(
				"The worktree is NOT that revision: later work changed it and " +
					"was not verified.\n")
		}
	}
	fmt.Fprintf(&report, "Attempts: %d", facts.attempts)
	if facts.infrastructureRetries > 0 {
		fmt.Fprintf(&report, ", of which %d were lost to the provider rather "+
			"than spent on the work", facts.infrastructureRetries)
	}
	report.WriteString(".\n")
	if facts.unresolved != "" {
		fmt.Fprintf(&report, "Unresolved: %s\n", facts.unresolved)
	}
	if len(facts.advisories) > 0 {
		fmt.Fprintf(&report,
			"%d advisory finding(s), carried rather than fixed — none of them "+
				"is a demonstrated defect:\n", len(facts.advisories))
		for _, advisory := range facts.advisories {
			fmt.Fprintf(&report, "  - %s: %s\n", advisory.Where, advisory.What)
		}
	}
	return strings.TrimRight(report.String(), "\n")
}

// maximumUnrecognisedLoopErrors bounds how many times a run absorbs a loop
// error it has no name for.
//
// Two is enough to survive a transient bookkeeping mismatch and few enough that
// a genuinely broken machine still ends the run rather than spending its whole
// budget rediscovering that it is broken.
const maximumUnrecognisedLoopErrors = 2

// loopRefusalInstruction is what the next attempt is told when the loop refused
// the last one for a reason of its own.
//
// It names the refusal and then says the only thing that reliably helps: do the
// same work in a plainer way. The refusals in this class are about the shape of
// a turn -- two writes to one file, a result whose identity does not match a
// call -- and the model cannot inspect that shape, so telling it the mechanism
// would be telling it something it cannot act on.
func loopRefusalInstruction(refusal error) string {
	return "The last attempt was refused before any of its work was recorded: " +
		refusal.Error() + "\n\nNothing was rolled back; every file the earlier " +
		"attempts wrote is still in the worktree exactly as they left it. " +
		"Continue from there. Make one change at a time, write each file once, " +
		"and run the tests after the write rather than alongside it."
}
