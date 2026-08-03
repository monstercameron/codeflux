package coordinator

import (
	"context"
	"errors"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/fingerprint"
	"codeflux.dev/codeflux/internal/forecast"
	"codeflux.dev/codeflux/internal/policy"
	"codeflux.dev/codeflux/internal/providers"
	"codeflux.dev/codeflux/internal/retrieval"
	"codeflux.dev/codeflux/internal/retrievalgate"
	"codeflux.dev/codeflux/internal/storage"
)

type taskPreflightStore interface {
	// GetThread, CreateTask, and CreateBudget back IntakeTask
	// (task_intake.go): resolving a thread's project and repository, then
	// recording the task and its hard budget before any preflight runs.
	GetThread(context.Context, domain.ThreadID) (storage.Thread, error)
	CreateTask(context.Context, storage.CreateTask) (storage.Task, error)
	GetTask(context.Context, domain.TaskID) (storage.Task, error)

	RecordExecutionPolicy(
		context.Context,
		storage.RecordExecutionPolicy,
	) (storage.ExecutionPolicyRevision, error)
	RecordEffortForecast(
		context.Context,
		storage.RecordEffortForecast,
	) (storage.EffortForecastRevision, error)
	CreateBudget(
		context.Context,
		storage.CreateBudget,
	) (storage.BudgetAccount, error)
	GetBudgetSnapshot(
		context.Context,
		domain.TaskID,
	) (storage.BudgetSnapshot, error)
	AdjustBudgetBeforeApproval(
		context.Context,
		storage.AdjustPreApprovalBudget,
	) (storage.PreApprovalBudgetAdjustment, storage.BudgetSnapshot, error)
	PrepareTaskExecution(
		context.Context,
		storage.PrepareTaskExecution,
	) (storage.ExecutionPreflight, error)
	GetTaskExecutionPresentation(
		context.Context,
		domain.TaskID,
		uint64,
	) (storage.TaskExecutionPresentation, error)
	StartPreparedTaskRun(
		context.Context,
		storage.StartPreparedTaskRun,
	) (storage.StartedTaskRun, error)
	RecordForecastOutcome(
		context.Context,
		storage.RecordForecastOutcome,
	) (storage.ForecastOutcome, error)
	// RecordTaskExactFingerprintBinding carries ForecastTask's exact
	// fingerprint (schema version + hash) durably past the gap between
	// forecast time and episode-open time (MEM-001): OpenEpisode runs at
	// the start of a run, long after ForecastTask returns, and has no
	// other way to learn what fingerprint retrieval already indexed this
	// task under.
	RecordTaskExactFingerprintBinding(
		context.Context,
		storage.RecordTaskExactFingerprintBinding,
	) (storage.TaskExactFingerprintBinding, error)
}

// TaskForecastInput contains the fixed-policy features and stable identities
// needed before plan approval.
type TaskForecastInput struct {
	TaskID                domain.TaskID
	BudgetID              domain.BudgetID
	BaselineModelRevision string
	Override              *policy.ManualOverride
	RepositoryRevision    string
	// Fingerprint is the real, structured task fingerprint (docs/plan.md §5
	// "Task Fingerprint and Retrieval"; internal/fingerprint.
	// ExactFingerprintInput's project, repository, base revision, task
	// class, affected hints, toolchain bindings, risk, required assurance,
	// and requested authority) that ForecastTask both hashes for
	// forecast.Generate's own TaskFingerprint binding AND -- the reason this
	// replaces what used to be a bare hash string -- passes to
	// retrieval.Service.RunPreWorkGate BEFORE planning from scratch, so
	// project memory has a real chance to influence the task
	// (docs/plan.md §31 "Retrieval and Pre-Work Gate").
	Fingerprint              fingerprint.ExactFingerprintInput
	TaskClass                forecast.TaskClass
	RepositorySize           forecast.RepositorySize
	LikelyFiles              []string
	ValidationCommands       []string
	ToolConfigurationVersion string
	ValidationProfileVersion string
	PriceSnapshot            *providers.PriceSnapshot
	Eligibility              forecast.CounterfactualEligibility
	PolicyIdempotencyKey     string
	ForecastIdempotencyKey   string
}

// ForecastedTask is the durable pre-approval policy, forecast, and budget.
type ForecastedTask struct {
	Policy   storage.ExecutionPolicyRevision
	Forecast storage.EffortForecastRevision
	Budget   storage.BudgetSnapshot
	// Retrieval is the pre-work retrieval gate's own typed result for this
	// task's fingerprint (M21-073): the bounded set of eligible memory
	// items, or Retrieval.FellBack == true when nothing was eligible -- a
	// normal outcome, already durably recorded, never an error. A caller
	// that finds Retrieval.Eligible non-empty and goes on to use, adapt, or
	// reject one of those items must call
	// TaskPreflightService.RecordMemoryInfluence afterward: ForecastTask
	// itself never does, because retrieval and influence are different
	// facts.
	Retrieval retrieval.PreWorkGateResult
}

// PrepareTaskPreflight adds a ready-task revision and presentation identity to
// the fixed forecast inputs.
type PrepareTaskPreflight struct {
	Forecast                TaskForecastInput
	ExpectedTaskRevision    uint64
	PreflightIdempotencyKey string
}

// PreparedTaskPreflight is returned to presentation code before Start.
type PreparedTaskPreflight struct {
	Forecasted ForecastedTask
	Preflight  storage.ExecutionPreflight
}

// TaskPreflightService is the coordinator-owned production path for fixed
// policy selection, forecasting, exact budgets, start, and outcome telemetry.
type TaskPreflightService struct {
	store     taskPreflightStore
	retrieval *retrieval.Service
}

// NewTaskPreflightService builds a TaskPreflightService. retrievalService is
// the pre-work retrieval gate (internal/retrieval.Service) ForecastTask
// consults before planning from scratch; both store and retrievalService
// are required, so a caller can never silently construct a preflight service
// that plans without ever giving project memory a chance to influence the
// task.
func NewTaskPreflightService(
	store taskPreflightStore,
	retrievalService *retrieval.Service,
) (*TaskPreflightService, error) {
	if store == nil {
		return nil, errors.New("task preflight repository is required")
	}
	if retrievalService == nil {
		return nil, errors.New("task preflight retrieval service is required")
	}
	return &TaskPreflightService{store: store, retrieval: retrievalService}, nil
}

// ForecastTask records the exact selected policy, advisory forecast, and
// initial task budget while adjustments are still allowed.
func (service *TaskPreflightService) ForecastTask(
	ctx context.Context,
	input TaskForecastInput,
) (ForecastedTask, error) {
	if service == nil || service.store == nil || service.retrieval == nil {
		return ForecastedTask{}, errors.New("task preflight service is unavailable")
	}
	exact, err := fingerprint.BuildExactFingerprint(input.Fingerprint)
	if err != nil {
		return ForecastedTask{}, err
	}
	fingerprintHash, err := exact.Hash()
	if err != nil {
		return ForecastedTask{}, err
	}
	// MEM-001: bind the exact fingerprint to the task now, while it is
	// still in hand, so a run opening this task's episode much later can
	// read back the same fingerprint retrieval already indexed the task
	// under, rather than recomputing (and likely mismatching) one from
	// whatever a run happens to still have access to.
	if _, err := service.store.RecordTaskExactFingerprintBinding(ctx, storage.RecordTaskExactFingerprintBinding{
		TaskID: input.TaskID, FingerprintSchemaVersion: exact.SchemaVersion, FingerprintHash: fingerprintHash,
	}); err != nil {
		return ForecastedTask{}, err
	}

	// Pre-work retrieval gate (docs/plan.md §31 "Before generating a
	// solution, the agent: 1. constructs the task fingerprint... 2. loads
	// version-compatible workspace facts and execution recipes... 9.
	// records every retrieval, routing, reuse, rejection, escalation, and
	// outcome"): consult project memory BEFORE selecting a policy or
	// generating an effort forecast, i.e. before planning from scratch.
	// RunPreWorkGate itself durably records a fallback when nothing is
	// eligible (M21-076) -- that is a normal outcome, never an error, and
	// ordinary planning below always proceeds regardless of FellBack.
	retrievalResult, err := service.retrieval.RunPreWorkGate(ctx, retrieval.PreWorkGateInput{
		QueryID:   "forecast-retrieval:" + input.ForecastIdempotencyKey,
		ProjectID: exact.Project,
		TaskID:    input.TaskID,
		Boundary:  domain.MemoryQueryProjectBoundary{Project: exact.Project},
		Task:      exact,
	})
	if err != nil {
		return ForecastedTask{}, err
	}

	selected, err := policy.Select(policy.SelectionInput{
		BaselineModelRevision: input.BaselineModelRevision,
		Override:              input.Override,
	})
	if err != nil {
		return ForecastedTask{}, err
	}
	policyRevision, err := service.store.RecordExecutionPolicy(
		ctx,
		storage.RecordExecutionPolicy{
			TaskID: input.TaskID, Policy: selected,
			IdempotencyKey: input.PolicyIdempotencyKey,
		},
	)
	if err != nil {
		return ForecastedTask{}, err
	}
	value, err := forecast.Generate(forecast.Input{
		RepositoryRevision:       input.RepositoryRevision,
		TaskFingerprint:          fingerprintHash,
		TaskClass:                input.TaskClass,
		RepositorySize:           input.RepositorySize,
		LikelyFiles:              input.LikelyFiles,
		ValidationCommands:       input.ValidationCommands,
		Policy:                   selected,
		ToolConfigurationVersion: input.ToolConfigurationVersion,
		ValidationProfileVersion: input.ValidationProfileVersion,
		PriceSnapshot:            input.PriceSnapshot,
	})
	if err != nil {
		return ForecastedTask{}, err
	}
	forecastRevision, err := service.store.RecordEffortForecast(
		ctx,
		storage.RecordEffortForecast{
			TaskID: input.TaskID, PolicyRevision: policyRevision.Revision,
			Forecast: value, Eligibility: input.Eligibility,
			IdempotencyKey: input.ForecastIdempotencyKey,
		},
	)
	if err != nil {
		return ForecastedTask{}, err
	}
	budget, err := selected.BudgetDefaults.Materialize(input.BudgetID)
	if err != nil {
		return ForecastedTask{}, err
	}
	if _, err := service.store.CreateBudget(ctx, storage.CreateBudget{
		TaskID: input.TaskID,
		Budget: budget,
	}); err != nil {
		existing, getErr := service.store.GetBudgetSnapshot(ctx, input.TaskID)
		if getErr != nil || existing.BudgetID != input.BudgetID {
			return ForecastedTask{}, err
		}
	}
	snapshot, err := service.store.GetBudgetSnapshot(ctx, input.TaskID)
	if err != nil {
		return ForecastedTask{}, err
	}
	return ForecastedTask{
		Policy: policyRevision, Forecast: forecastRevision, Budget: snapshot,
		Retrieval: retrievalResult,
	}, nil
}

// RecordMemoryInfluence persists what the agent actually did with one
// eligible memory item ForecastTask's retrieval gate surfaced -- used it as
// -is, adapted it, or rejected it (M21-074, M21-075). Calling this is the
// separate, later fact that turns "eligible" into "influenced (or was
// deliberately not used for) the outcome": ForecastTask itself never calls
// this, so a caller that never calls it for an eligible item has retrieved
// memory without that retrieval ever counting as influential.
func (service *TaskPreflightService) RecordMemoryInfluence(
	ctx context.Context,
	item retrieval.InfluentialMemoryItem,
	action retrievalgate.AgentInfluenceAction,
	justification string,
) error {
	if service == nil || service.retrieval == nil {
		return errors.New("task preflight retrieval service is unavailable")
	}
	return service.retrieval.RecordInfluence(ctx, item, action, justification)
}

// AdjustBudgetBeforeApproval applies an attributable durable user choice.
func (service *TaskPreflightService) AdjustBudgetBeforeApproval(
	ctx context.Context,
	input storage.AdjustPreApprovalBudget,
) (storage.PreApprovalBudgetAdjustment, storage.BudgetSnapshot, error) {
	if service == nil || service.store == nil {
		return storage.PreApprovalBudgetAdjustment{}, storage.BudgetSnapshot{},
			errors.New("task preflight service is unavailable")
	}
	return service.store.AdjustBudgetBeforeApproval(ctx, input)
}

// BindExecution creates and returns the combined inspectable presentation.
func (service *TaskPreflightService) BindExecution(
	ctx context.Context,
	taskID domain.TaskID,
	expectedTaskRevision uint64,
	forecasted ForecastedTask,
	idempotencyKey string,
) (storage.ExecutionPreflight, error) {
	if service == nil || service.store == nil {
		return storage.ExecutionPreflight{},
			errors.New("task preflight service is unavailable")
	}
	currentBudget, err := service.store.GetBudgetSnapshot(ctx, taskID)
	if err != nil {
		return storage.ExecutionPreflight{}, err
	}
	return service.store.PrepareTaskExecution(
		ctx,
		storage.PrepareTaskExecution{
			TaskID: taskID, ExpectedTaskRevision: expectedTaskRevision,
			PolicyRevision:      forecasted.Policy.Revision,
			ForecastRevision:    forecasted.Forecast.Revision,
			BudgetID:            currentBudget.BudgetID,
			BudgetLimitRevision: currentBudget.LimitRevision,
			IdempotencyKey:      idempotencyKey,
		},
	)
}

// Prepare records all fixed inputs and returns presentation before execution.
func (service *TaskPreflightService) Prepare(
	ctx context.Context,
	input PrepareTaskPreflight,
) (PreparedTaskPreflight, error) {
	forecasted, err := service.ForecastTask(ctx, input.Forecast)
	if err != nil {
		return PreparedTaskPreflight{}, err
	}
	preflight, err := service.BindExecution(
		ctx,
		input.Forecast.TaskID,
		input.ExpectedTaskRevision,
		forecasted,
		input.PreflightIdempotencyKey,
	)
	if err != nil {
		return PreparedTaskPreflight{}, err
	}
	return PreparedTaskPreflight{
		Forecasted: forecasted,
		Preflight:  preflight,
	}, nil
}

// Presentation returns the immutable policy and forecast with a freshly
// computed exact budget view for UI and audit consumers.
func (service *TaskPreflightService) Presentation(
	ctx context.Context,
	taskID domain.TaskID,
	preflightRevision uint64,
) (storage.TaskExecutionPresentation, error) {
	if service == nil || service.store == nil {
		return storage.TaskExecutionPresentation{},
			errors.New("task preflight service is unavailable")
	}
	return service.store.GetTaskExecutionPresentation(
		ctx,
		taskID,
		preflightRevision,
	)
}

// Start begins only the exact preflight returned for presentation.
func (service *TaskPreflightService) Start(
	ctx context.Context,
	input storage.StartPreparedTaskRun,
) (storage.StartedTaskRun, error) {
	if service == nil || service.store == nil {
		return storage.StartedTaskRun{},
			errors.New("task preflight service is unavailable")
	}
	return service.store.StartPreparedTaskRun(ctx, input)
}

// RecordOutcome persists terminal actuals and forecast comparison evidence.
func (service *TaskPreflightService) RecordOutcome(
	ctx context.Context,
	input storage.RecordForecastOutcome,
) (storage.ForecastOutcome, error) {
	if service == nil || service.store == nil {
		return storage.ForecastOutcome{},
			errors.New("task preflight service is unavailable")
	}
	return service.store.RecordForecastOutcome(ctx, input)
}
