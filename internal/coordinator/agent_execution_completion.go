package coordinator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	agentloop "codeflux.dev/codeflux/internal/agent"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
	"codeflux.dev/codeflux/internal/executor"
	"codeflux.dev/codeflux/internal/storage"
)

// buildAgentExecutionCompletion constructs the production RepairCompletionService
// for one coordinator instance, plus the narrow "this run's own validation
// already passed" runner it needs.
//
// AUDIT-020: RepairCompletionService.PrepareCompletion is what moves a task out
// of "running" into "awaiting-review", and until this function's caller existed
// nothing constructed the service outside its own tests — every run did real
// work and then sat in "running" forever. A previous attempt (2026-08-02) wired
// this and was backed out: it re-ran the project's own "go test ./..." through
// the mediated tool boundary a second time at the end of every run, and that
// unbounded extra work — not merely the durable bookkeeping around it — hung
// TestTheEngineCarriesARequirementThroughToRealWorkOnDisk from 3.8s to a
// 480-second-plus timeout with nothing else in the tree changed. This
// reconstruction does not re-run anything: completeRunIfPossible only ever
// asks for completion once the run's own loop has already run the project's
// tests and they already passed, and agentExecutionValidationRunner reports
// that already-established fact rather than spawning a second subprocess. See
// TODOS.md AUDIT-020 for the measured before/after.
//
// One dependency the interface requires — RepairExecutor — is not reachable
// from this call site: AgentExecution.Run's own attempt loop is the repair
// mechanism (it re-plans and re-attempts on failure before completion is ever
// considered), so the completion call site always runs ValidateAndRepair with
// MaximumRepairRounds set to zero. At zero, ValidateAndRepair's own
// bounded-repair loop cannot execute, so RepairExecutor.ExecuteRepair is
// provably never called; agentExecutionRepairUnavailable exists only so
// construction can be honest about not having wired the general bounded-repair
// path, which is a materially larger, separate piece of work than closing this
// ticket requires.
func buildAgentExecutionCompletion(
	repositories *storage.Repositories,
	checkpointing applicationCheckpointing,
) (*RepairCompletionService, *agentExecutionValidationRunner, *agentExecutionCompletionGate, error) {
	if repositories == nil {
		return nil, nil, nil, errors.New("agent execution completion needs repositories")
	}
	if checkpointing.redactor == nil {
		return nil, nil, nil, errors.New("agent execution completion needs a redaction pipeline")
	}
	// Deliberately the stub, not checkpointing.checkpoints, even though the
	// real adapter is available and now implements PreRepairCheckpointCreator
	// (task_control_checkpoint_adapter.go). Two different reasons land on the
	// same answer:
	//
	//   - CreatePreRepairCheckpoint is provably unreachable here regardless of
	//     which value is passed: MaximumRepairRounds is always zero (see the
	//     doc comment above), so ValidateAndRepair's bounded-repair loop never
	//     runs and never calls it.
	//   - The real adapter also implements the *optional*
	//     SuccessfulValidationCheckpointCreator interface
	//     (repair_completion.go), and ValidateAndRepair attempts that capture
	//     on every passed validation. The capture service currently requires
	//     a durable `run_configurations` row (storage.GetRunConfiguration),
	//     and nothing in production writes one — confirmed by symbol search:
	//     only this package's own tests and configuration_repository.go
	//     itself reference the table. That gap predates this ticket and is
	//     not AUDIT-020's to fix; passing the real adapter here would make
	//     every completion fail on it. The stub does not implement the
	//     optional interface, so ValidateAndRepair's own type assertion finds
	//     it absent and skips the capture rather than failing on it — the
	//     same "optional, declared absent rather than pretended present"
	//     shape every other honestly-unwired port in this file uses.
	checkpoints := PreRepairCheckpointCreator(agentExecutionCheckpointsUnavailable{})
	validations := newAgentExecutionValidationRunner(repositories)
	gate := newAgentExecutionCompletionGate()
	service, err := NewStoredRepairCompletionService(
		repositories,
		validations,
		gate,
		checkpoints,
		agentExecutionRepairUnavailable{},
		agentActiveControl{},
		&agentExecutionFinalRepositoryInspector{repositories: repositories},
		&agentExecutionFinalBudgetInspector{repositories: repositories},
		checkpointing.redactor,
	)
	if err != nil {
		return nil, nil, nil, err
	}
	return service, validations, gate, nil
}

// completeRunIfPossible records the evidence a completion candidate needs and
// moves the task into awaiting-review. It never claims completion by
// weakening what it checks: unless the run's own required validation already
// passed in this exact worktree state (verified), or the code does not
// compile, the task is left exactly where it was, with the reason said in the
// conversation rather than swallowed.
//
// It does not run anything. narrator.ranValidation and
// narrator.validationFailed already say whether the project's own tests ran
// and passed as part of the ordinary loop above; this reuses that fact rather
// than re-running "go test ./..." a second time (AUDIT-020 — see
// buildAgentExecutionCompletion for why re-running it was the defect in the
// previous attempt).
func (execution *AgentExecution) completeRunIfPossible(
	ctx context.Context,
	scope agentScope,
	taskID domain.TaskID,
	runID domain.RunID,
	plan durablePlan,
	steps []agentloop.PlanStep,
	compiles bool,
	verified bool,
	// clean is whether every stage the flow requires actually held. It is the
	// third of three, and it was the missing one.
	//
	// Readiness was reconstructed here from compiles and verified alone, while
	// the caller had already computed whether the ledger was clean and used it
	// to decide the delivery stages — and then did not pass it. A run with a
	// failed or blocked required stage could therefore be moved to
	// awaiting-review by this function while its own ledger recorded the
	// failure, which is the run and the record disagreeing about whether the
	// work is ready.
	//
	// A model outcome is a proposal. Only the reconciled ledger says a run is
	// ready, so this takes that answer rather than deriving its own.
	clean bool,
) finalization {
	switch {
	case execution.completion == nil || execution.completionValidations == nil ||
		execution.completionGate == nil:
		return finalization{Reason: "this build performs no completion"}
	case !compiles:
		return finalization{Reason: "the work does not compile"}
	case !verified:
		return finalization{Reason: "the work was never verified"}
	case !clean:
		return finalization{
			Reason: "a stage the flow requires did not hold"}
	}
	stepIDs := make([]string, 0, len(steps))
	for _, step := range steps {
		stepIDs = append(stepIDs, step.ID)
	}
	if len(stepIDs) == 0 {
		return finalization{Reason: "the run declared no plan steps"}
	}
	command, err := finalValidationCommand(taskID, runID, scope.worktree, stepIDs)
	if err != nil {
		execution.say(ctx, scope, events.KindMessageFinal,
			"The run's final validation could not be prepared: "+err.Error())
		return finalization{Reason: "completion could not be recorded"}
	}
	// BindValidationProfile refuses a name it cannot rank against the plan's
	// own recorded floor (storage.BindRunValidationProfile): the profile a
	// completion selects has to be one of the plan-contract's own named
	// floors — risk-derived, not a call-site invention — and at least as
	// strong as the one the plan itself carries. Using anything else here
	// (an earlier attempt used a free-form "agent-execution-final" name) is
	// refused before a single row is written.
	profile := agentloop.ValidationProfile{
		Name:     string(storage.ValidationProfileForRisk(scope.riskLevel)),
		Version:  "v1",
		Commands: []agentloop.ValidationCommand{command},
	}
	execution.completionValidations.markVerified(runID, profile.Name)
	defer execution.completionValidations.unmark(runID)
	idempotencyPrefix := agentExecutionKey(
		"agent-completion-validation-", runID.String())
	outcome, err := execution.completion.ValidateAndRepair(
		ctx, ValidationRepairInput{
			TaskID: taskID, RunID: runID, PlanRevision: plan.Revision,
			Profile: profile, MaximumRepairRounds: 0,
			IdempotencyPrefix: idempotencyPrefix,
		},
	)
	if err != nil {
		execution.say(ctx, scope, events.KindMessageFinal,
			"The run's completion evidence could not be recorded: "+err.Error())
		return finalization{Reason: "completion could not be recorded"}
	}
	if !outcome.ReadyForCompletion || len(outcome.Reports) == 0 {
		execution.say(ctx, scope, events.KindMessageFinal,
			"The run's completion evidence did not resolve, so it stays running: "+
				outcome.UnresolvedReason)
		return finalization{Reason: "completion could not be recorded"}
	}
	report := outcome.Reports[len(outcome.Reports)-1]
	if len(report.Executions) == 0 || report.Executions[0].ValidationID.IsZero() {
		execution.say(ctx, scope, events.KindMessageFinal,
			"The run's validation passed but recorded no attributable "+
				"evidence, so it stays running.")
		return finalization{Reason: "completion could not be recorded"}
	}
	// Every declared step is already durably Validated at this point.
	// ValidateAndRepair's own validation pass (runValidationPass, in this
	// package) attributes the passed command to every plan step named in
	// its PlanStepIDs — every step this call declared, via
	// finalValidationCommand — and storage.RecordPlanValidationAttributions
	// promotes each one from Implemented to Validated itself as part of
	// recording that attribution (agent_execution_repository.go, the
	// ValidationPassed branch). An earlier version of this function
	// promoted the same steps a second time here, which failed the moment
	// it ran: the store's own optimistic-revision check refused a second
	// implemented-to-validated transition against a step already validated
	// by the pass above. There is nothing left to do here but proceed.
	execution.completionGate.markReady(taskID, runID)
	taskRevision, runRevision, err := execution.currentTaskAndRunRevision(ctx, taskID, runID)
	if err != nil {
		execution.say(ctx, scope, events.KindMessageFinal,
			"The run's current revision could not be read, so completion was "+
				"not recorded: "+err.Error())
		return finalization{Reason: "completion could not be recorded"}
	}
	validationEventID, err := domain.NewEventID()
	if err != nil {
		execution.say(ctx, scope, events.KindMessageFinal,
			"The run's validation transition could not be attributed to an "+
				"event, so it stays running: "+err.Error())
		return finalization{Reason: "completion could not be recorded"}
	}
	transitioned, err := execution.repositories.TransitionRunToValidation(
		ctx, storage.TransitionRunToValidation{
			EventID: validationEventID, TaskID: taskID, RunID: runID,
			ExpectedTaskRevision: taskRevision, ExpectedRunRevision: runRevision,
			IdempotencyKey: agentExecutionKey(
				"agent-run-validating-", runID.String()),
		},
	)
	if err != nil {
		execution.say(ctx, scope, events.KindMessageFinal,
			"The task could not be moved into validation, so it stays "+
				"running: "+err.Error())
		return finalization{Reason: "completion could not be recorded"}
	}
	completionEventID, err := domain.NewEventID()
	if err != nil {
		execution.say(ctx, scope, events.KindMessageFinal,
			"The run's completion could not be attributed to an event, so it "+
				"stays validating rather than awaiting review: "+err.Error())
		return finalization{Reason: "completion could not be recorded"}
	}
	result, err := execution.completion.PrepareCompletion(ctx, CompletionInput{
		TaskID: taskID, RunID: runID, PlanRevision: plan.Revision,
		ExpectedTaskRevision: transitioned.TaskRevision,
		ExpectedRunRevision:  transitioned.RunRevision,
		EventID:              completionEventID,
		EventIdempotencyKey: agentExecutionKey(
			"agent-completion-event-", runID.String()),
		IdempotencyKey: agentExecutionKey("agent-completion-", runID.String()),
	})
	if err != nil {
		execution.say(ctx, scope, events.KindMessageFinal,
			"The run's completion evidence could not be recorded, so it stays "+
				"validating rather than awaiting review: "+err.Error())
		return finalization{Reason: "completion could not be recorded"}
	}
	execution.say(ctx, scope, events.KindMessageFinal, fmt.Sprintf(
		"This run's work is now awaiting review (completion revision %d).",
		result.Revision))
	return finalization{
		Terminal:  true,
		Reason:    "every required gate held and the work is awaiting review",
		TaskState: domain.TaskStateAwaitingReview,
	}
}

// finalValidationCommand describes the one required validation completion's
// evidence chain attributes to every declared plan step: the project's own
// test command, over every step this run declared, so a passing result
// durably covers every step PrepareCompletion's evidence check requires.
//
// It is never executed (AUDIT-020). It exists to give BindValidationProfile
// and the durable attribution chain a real, verifiable identity for "the
// command this run's own validation already ran" — the same executable, the
// same arguments, the same working directory the run itself already ran
// through narratingExecutor — without running it again.
func finalValidationCommand(
	taskID domain.TaskID,
	runID domain.RunID,
	worktree string,
	stepIDs []string,
) (agentloop.ValidationCommand, error) {
	request := executor.ToolRequest{
		SchemaVersion: executor.ToolSchemaVersion,
		ID:            "agent-completion-validation",
		TaskID:        taskID, RunID: runID,
		Name: executor.ToolTest,
		Arguments: []executor.ToolArgument{
			{Name: "executable", Value: "go"},
			{Name: "arg1", Value: "test"},
			{Name: "arg2", Value: "./..."},
		},
		WorkingDirectory: filepath.Clean(worktree),
		Timeout:          4 * time.Minute,
		ClaimedAuthority: executor.AuthorityAutomaticRead,
		ExpectedSideEffects: []executor.SideEffect{
			executor.EffectRepositoryRead, executor.EffectSubprocess,
		},
		IdempotencyKey: "agent-completion-validation-" + runID.String(),
		Requester:      "agent-execution-completion",
	}
	planCommand, err := agentloop.RenderValidationPlanCommand(request)
	if err != nil {
		return agentloop.ValidationCommand{}, err
	}
	return agentloop.ValidationCommand{
		ID: "final-verification", Required: true, AcceptanceTest: true,
		PlanCommand: planCommand, Request: request,
		PlanStepIDs: append([]string{}, stepIDs...),
	}, nil
}

// currentTaskAndRunRevision reads the exact durable revisions
// TransitionRunToValidation's optimistic expectation binds to.
func (execution *AgentExecution) currentTaskAndRunRevision(
	ctx context.Context,
	taskID domain.TaskID,
	runID domain.RunID,
) (uint64, uint64, error) {
	task, err := execution.repositories.GetTask(ctx, taskID)
	if err != nil {
		return 0, 0, err
	}
	runRevision, err := execution.repositories.GetRunRevision(ctx, taskID, runID)
	if err != nil {
		return 0, 0, err
	}
	return task.Revision, runRevision, nil
}

// agentExecutionValidationRunner reports that a run's own required validation
// already passed, without executing anything.
//
// AUDIT-020: a previous attempt implemented this by re-running the final
// validation command through the mediated tool boundary — the same
// "go test ./..." the run's own loop had already run — and that second,
// unbounded subprocess is what hung real runs (see
// buildAgentExecutionCompletion). completeRunIfPossible only ever calls
// ValidateAndRepair once narrator.ranValidation and !narrator.validationFailed
// are already true, so by the time this type is asked to run a command, the
// fact it needs to report is already established; it reports it rather than
// re-establishing it.
//
// The run-keyed map exists because RepairCompletionService is constructed
// once, at startup, before any particular run exists: completeRunIfPossible
// marks its own run verified immediately before calling ValidateAndRepair and
// clears it immediately after, so a validation command for any other run (or
// one bound to nothing) is refused rather than reported as passing.
//
// It still writes one durable row (storage.CreateValidation) rather than
// inventing a validation identity purely in memory: ReadFinalValidationReport
// and the plan-step consistency trigger both dereference the validation ID a
// transition or attribution names, through a real `validations` row this
// package is the only writer of. What it does not do is run a command to
// produce that row's verdict — the verdict (State: passed) is the fact
// completeRunIfPossible already established before calling this at all.
type agentExecutionValidationRunner struct {
	repositories *storage.Repositories
	mutex        sync.Mutex
	// verified maps a run to the plan's own validation profile name, present
	// only once completeRunIfPossible has confirmed that run's required
	// validation already passed.
	verified map[domain.RunID]string
}

func newAgentExecutionValidationRunner(
	repositories *storage.Repositories,
) *agentExecutionValidationRunner {
	return &agentExecutionValidationRunner{
		repositories: repositories, verified: map[domain.RunID]string{},
	}
}

func (runner *agentExecutionValidationRunner) markVerified(
	runID domain.RunID,
	profileName string,
) {
	runner.mutex.Lock()
	defer runner.mutex.Unlock()
	runner.verified[runID] = profileName
}

func (runner *agentExecutionValidationRunner) unmark(runID domain.RunID) {
	runner.mutex.Lock()
	defer runner.mutex.Unlock()
	delete(runner.verified, runID)
}

// RunValidationCommand implements ValidationCommandRunner. It never spawns a
// process; see the type's doc comment for why.
func (runner *agentExecutionValidationRunner) RunValidationCommand(
	ctx context.Context,
	command agentloop.ValidationCommand,
	_ uint32,
) (ValidationCommandRun, error) {
	runner.mutex.Lock()
	profileName, verified := runner.verified[command.Request.RunID]
	runner.mutex.Unlock()
	if !verified {
		return ValidationCommandRun{}, errors.New(
			"final validation is only reported for a run whose own required " +
				"validation has already passed")
	}
	validationID, err := domain.NewValidationID()
	if err != nil {
		return ValidationCommandRun{}, err
	}
	summary := "reused from this run's own required validation, which " +
		"already ran and passed"
	runID := command.Request.RunID
	if _, err := runner.repositories.CreateValidation(ctx, storage.CreateValidation{
		ID: validationID, TaskID: command.Request.TaskID, RunID: &runID,
		State: domain.ValidationStatePassed,
		// Blocking, not advisory: this is the run's required validation floor,
		// the one PrepareCompletion refuses to proceed without.
		Severity:    domain.ValidationSeverityBlocking,
		ProfileName: profileName,
		// SummaryRedacted is deliberately nil, not &summary. It round-trips as
		// validations.summary_redacted, and ListDurableValidationExecutions
		// reads that same column back as FailureSummaryRedacted regardless of
		// state (agent_validation_profile_repository.go) — there is no
		// separate "passed" narrative column. ReadFinalValidationReport's
		// validDurableFailureLinkage requires FailureSummaryRedacted == "" for
		// a passed validation, so setting any summary here, on a passed
		// validation, makes every completion refuse itself with "durable
		// validation presentation differs from facts". The narration this
		// validation reused an existing result is said in the tool result's
		// own Summary field below instead, which carries no such constraint.
	}); err != nil {
		return ValidationCommandRun{}, fmt.Errorf(
			"record the reused validation: %w", err)
	}
	return ValidationCommandRun{
		ValidationID: validationID,
		// CommandExecutionID is deliberately left empty: it would name a
		// command_executions row this call never created, since nothing here
		// executes a command (AUDIT-020). RecordValidationAttribution treats
		// an empty ID as "not attributed to one," which the consistency
		// trigger on plan_validation_operations accepts (NULL, not a
		// dangling reference).
		Result: executor.ToolResult{
			RequestID: command.Request.ID, SchemaVersion: executor.ToolSchemaVersion,
			State: "succeeded", Executable: "go", ResolvedExecutable: "go",
			Summary: summary,
		},
	}, nil
}

// agentExecutionFinalRepositoryInspector reads the exact worktree state a
// completion candidate reports: git's own status and diff, not a description
// of what the run believes it did.
type agentExecutionFinalRepositoryInspector struct {
	repositories *storage.Repositories
}

func (inspector *agentExecutionFinalRepositoryInspector) InspectFinalRepository(
	ctx context.Context,
	taskID domain.TaskID,
	runID domain.RunID,
) (agentloop.RepositoryCompletionSummary, error) {
	binding, err := inspector.repositories.GetWorktreeBinding(ctx, taskID)
	if err != nil {
		return agentloop.RepositoryCompletionSummary{}, err
	}
	status := exec.CommandContext(ctx, "git", "status", "--porcelain")
	status.Dir = binding.WorktreePath
	statusOutput, err := status.CombinedOutput()
	if err != nil {
		return agentloop.RepositoryCompletionSummary{}, fmt.Errorf(
			"read the worktree's git status: %w", err)
	}
	diff := exec.CommandContext(ctx, "git", "diff", "HEAD")
	diff.Dir = binding.WorktreePath
	diffOutput, err := diff.CombinedOutput()
	if err != nil {
		return agentloop.RepositoryCompletionSummary{}, fmt.Errorf(
			"read the worktree's git diff: %w", err)
	}
	digest := sha256.Sum256(diffOutput)
	return agentloop.RepositoryCompletionSummary{
		StatusRedacted: string(statusOutput),
		DiffRedacted:   string(diffOutput),
		DiffSHA256:     hex.EncodeToString(digest[:]),
		ChangedFiles:   changedFilesFromPorcelainStatus(string(statusOutput)),
	}, nil
}

// changedFilesFromPorcelainStatus reads `git status --porcelain` output into
// the plain repository-relative paths it named.
func changedFilesFromPorcelainStatus(output string) []string {
	var files []string
	for _, line := range strings.Split(output, "\n") {
		if len(line) < 4 {
			continue
		}
		path := strings.TrimSpace(line[3:])
		if arrow := strings.Index(path, " -> "); arrow >= 0 {
			path = path[arrow+4:]
		}
		path = strings.Trim(path, `"`)
		path = filepath.ToSlash(path)
		if path != "" {
			files = append(files, path)
		}
	}
	sort.Strings(files)
	return files
}

// agentExecutionFinalBudgetInspector reports the task's real durable budget
// accounting rather than an invented figure.
type agentExecutionFinalBudgetInspector struct {
	repositories *storage.Repositories
}

func (inspector *agentExecutionFinalBudgetInspector) InspectFinalBudget(
	ctx context.Context,
	taskID domain.TaskID,
	runID domain.RunID,
) (agentloop.BudgetCompletionSummary, error) {
	snapshot, err := inspector.repositories.GetBudgetSnapshot(ctx, taskID)
	if err != nil {
		return agentloop.BudgetCompletionSummary{}, err
	}
	return agentloop.BudgetCompletionSummary{
		// The task's warning threshold is the closest durable fact to a
		// forecast this call site has: nothing here holds the original
		// pre-execution estimate separately from the enforced ceiling.
		Forecast: exactCostSummaryFrom(snapshot.WarningCost),
		Reserved: exactCostSummaryFrom(snapshot.ReservedCost),
		Actual: agentloop.ExactCostSummary{
			Known:       !snapshot.CostAccountingUnknown,
			Numerator:   snapshot.ActualKnownCost.Numerator,
			Denominator: snapshot.ActualKnownCost.Denominator,
			Currency:    snapshot.ActualKnownCost.Currency,
		},
		Remaining: exactCostSummaryFromPointer(snapshot.RemainingCost),
	}, nil
}

func exactCostSummaryFrom(cost storage.ExactMinorCost) agentloop.ExactCostSummary {
	return agentloop.ExactCostSummary{
		Known:     cost.Denominator > 0,
		Numerator: cost.Numerator, Denominator: cost.Denominator,
		Currency: cost.Currency,
	}
}

func exactCostSummaryFromPointer(
	cost *storage.ExactMinorCost,
) agentloop.ExactCostSummary {
	if cost == nil {
		return agentloop.ExactCostSummary{Known: false}
	}
	return exactCostSummaryFrom(*cost)
}

// agentExecutionCompletionGate tracks that this run's completion evidence was
// prepared through completeRunIfPossible, so PrepareCompletion cannot be
// reached for a run that never established it.
type agentExecutionCompletionGate struct {
	mutex sync.Mutex
	ready map[domain.RunID]domain.TaskID
}

func newAgentExecutionCompletionGate() *agentExecutionCompletionGate {
	return &agentExecutionCompletionGate{ready: map[domain.RunID]domain.TaskID{}}
}

func (gate *agentExecutionCompletionGate) markReady(
	taskID domain.TaskID,
	runID domain.RunID,
) {
	gate.mutex.Lock()
	defer gate.mutex.Unlock()
	gate.ready[runID] = taskID
}

// EnsureCompletionReady implements CompletionValidationGate.
func (gate *agentExecutionCompletionGate) EnsureCompletionReady(
	_ context.Context,
	taskID domain.TaskID,
	runID domain.RunID,
) error {
	if gate == nil {
		return ErrRequiredValidationIncomplete
	}
	gate.mutex.Lock()
	owner, ready := gate.ready[runID]
	gate.mutex.Unlock()
	if !ready || owner != taskID {
		return ErrRequiredValidationIncomplete
	}
	return nil
}

// agentExecutionCheckpointsUnavailable stands in for PreRepairCheckpointCreator
// when no checkpoint capture service was constructed. It refuses rather than
// silently succeeding, so a coordinator built without checkpointing cannot be
// mistaken for one that captured a checkpoint it never took. It is never
// actually called in this call site's own flow: MaximumRepairRounds is always
// zero, so ValidateAndRepair's bounded-repair loop — the only caller of
// CreatePreRepairCheckpoint — never executes.
type agentExecutionCheckpointsUnavailable struct{}

func (agentExecutionCheckpointsUnavailable) CreatePreRepairCheckpoint(
	context.Context, domain.TaskID, domain.RunID, uint64, uint32, string,
) (domain.CheckpointID, error) {
	return domain.CheckpointID{}, errors.New(
		"no checkpoint capture service is available")
}

// agentExecutionRepairUnavailable stands in for RepairExecutor.
//
// AgentExecution.Run's own attempt loop is this call site's repair mechanism:
// it re-plans, tells the next attempt what broke, and re-runs the loop before
// completion is ever considered. RepairCompletionService.ValidateAndRepair is
// therefore always invoked here with MaximumRepairRounds set to zero, and at
// zero its bounded-repair loop cannot execute — ExecuteRepair is provably
// unreachable. A general bounded-repair executor is real, separate work this
// ticket does not require.
type agentExecutionRepairUnavailable struct{}

func (agentExecutionRepairUnavailable) ExecuteRepair(
	context.Context, RepairRequest,
) (RepairResult, error) {
	return RepairResult{}, errors.New(
		"bounded repair is not wired at the agent-execution completion call site")
}
