// Package coordinator: turning a requirement into a startable task.
//
// This closes the gap between a user submitting a requirement and the agent
// loop being able to begin. Before this, TaskPreflightService had a complete
// lifecycle (ForecastTask, BindExecution, Prepare, Start, RecordOutcome) with
// no production caller, and TaskForecastInput was constructed only in tests,
// so nothing could actually start work.
//
// DESIGN DECISION worth knowing: the caller DECLARES the task's shape
// (class, risk, required assurance, affected hints, toolchain bindings,
// requested authority) rather than this service inferring it. No requirement
// classifier exists in the repository, and inventing one here would mean
// either a speculative model call or keyword guessing — both of which would
// put an unmeasured guess inside the fingerprint that gates memory retrieval
// and routing. docs/plan.md §5 also treats scope as something the user
// approves, not something the system decides silently. The cost is that a
// caller must supply these; the benefit is that nothing here fabricates a
// fact the system cannot observe.
package coordinator

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/fingerprint"
	"codeflux.dev/codeflux/internal/forecast"
	"codeflux.dev/codeflux/internal/storage"
)

// MaximumRequirementBytes bounds a submitted requirement.
const MaximumRequirementBytes = 32 * 1024

// TaskIntakeRequest is everything needed to turn one requirement into a
// prepared, startable task.
//
// Fields with no observable source are declared, never guessed. Where a
// field has a safe conventional default it may be left zero; those defaults
// are documented on each field and applied in applyIntakeDefaults.
type TaskIntakeRequest struct {
	ThreadID         domain.ThreadID
	RequestMessageID *domain.MessageID
	Requirement      string

	// TaskClass, RiskLevel, and RequiredAssurance describe the task's shape
	// and correctness floor. RiskLevel and RequiredAssurance default
	// conservatively (see applyIntakeDefaults); TaskClass is required
	// because no default is honest across documentation, migration, and
	// security work.
	TaskClass         fingerprint.TaskClass
	RiskLevel         domain.RiskLevel
	RequiredAssurance domain.AssuranceLevel

	// PolicyPreset and ReasoningEffort select the fixed routing policy.
	// Both default to the balanced/standard pair.
	PolicyPreset    domain.PolicyPreset
	ReasoningEffort domain.ReasoningEffort

	// RepositoryRevision is the exact revision the task starts from. It is
	// required: a fingerprint bound to an unknown base cannot be compared
	// against prior work.
	RepositoryRevision string

	// Structured scope hints. Optional, but they are what lets project
	// memory find anything: retrieval matches on these, not on the
	// requirement prose.
	AffectedPaths      []string
	AffectedPackages   []string
	AffectedSymbols    []string
	ToolchainBindings  []fingerprint.ToolchainBinding
	RequestedAuthority []fingerprint.AuthorityClass

	RepositorySize forecast.RepositorySize

	// ToolConfigurationVersion and ValidationProfileVersion pin the
	// environment the forecast is made against. Both are required: a
	// forecast compared across different tool or validation configurations
	// is not comparable at all.
	ToolConfigurationVersion string
	ValidationProfileVersion string
	ValidationCommands       []string

	BaselineModelRevision string
	SettingsRevision      uint64
	IdempotencyKey        string
}

// TaskIntakeResult is the created task and its prepared preflight, ready to
// present for approval and then start.
type TaskIntakeResult struct {
	Task storage.Task
	// Budget is the hard budget the selected fixed policy materialized for
	// this task, not one the caller chose.
	Budget storage.BudgetSnapshot
	// Forecasted is the immutable policy, effort forecast, and budget to
	// PRESENT for approval. It is not an execution binding: binding a
	// preflight requires the task to be Ready, and reaching Ready requires
	// a granted approval, so BindExecution is deliberately a separate step
	// the user's decision gates.
	Forecasted ForecastedTask
}

// IntakeTask creates a task from a requirement and prepares it for
// execution: it resolves the thread's project and repository, records the
// task and its hard budget, builds the real structured fingerprint, and runs
// the fixed-policy preflight — which consults project memory before planning.
//
// It deliberately stops before binding an execution preflight. Binding
// requires the task to be Ready, and a task only reaches Ready through a
// granted approval, so scope, forecast, and budget are presented first and
// the user's decision gates everything after — docs/plan.md §5's approval
// step, enforced by the task state machine rather than by convention.
func (service *TaskPreflightService) IntakeTask(
	ctx context.Context,
	request TaskIntakeRequest,
) (TaskIntakeResult, error) {
	if service == nil || service.store == nil {
		return TaskIntakeResult{}, errors.New("task preflight service is unavailable")
	}
	request = applyIntakeDefaults(request)
	if err := validateTaskIntakeRequest(request); err != nil {
		return TaskIntakeResult{}, err
	}

	thread, err := service.store.GetThread(ctx, request.ThreadID)
	if err != nil {
		return TaskIntakeResult{}, err
	}

	taskID, err := domain.NewTaskID()
	if err != nil {
		return TaskIntakeResult{}, err
	}
	task, err := service.store.CreateTask(ctx, storage.CreateTask{
		ID:                taskID,
		ThreadID:          thread.ID,
		RepositoryID:      thread.RepositoryID,
		RequestMessageID:  request.RequestMessageID,
		PolicyPreset:      request.PolicyPreset,
		ReasoningEffort:   request.ReasoningEffort,
		RiskLevel:         request.RiskLevel,
		RequiredAssurance: request.RequiredAssurance,
		SettingsRevision:  request.SettingsRevision,
		IdempotencyKey:    request.IdempotencyKey,
	})
	if err != nil {
		return TaskIntakeResult{}, err
	}

	// The hard budget is NOT supplied by the caller: ForecastTask
	// materializes it from the selected fixed policy's own BudgetDefaults,
	// so the correctness/cost floor comes from policy rather than from
	// whoever submitted the requirement. Intake only mints the identity that
	// budget account will carry.
	budgetID, err := domain.NewBudgetID()
	if err != nil {
		return TaskIntakeResult{}, err
	}

	// Counterfactual eligibility is advisory-only shadow telemetry for a
	// later routing study (docs/plan.md §29 Phases 3-5). Adaptive routing is
	// off through prototype exit, so intake records the honest state --
	// ineligible, with the reason -- rather than silently claiming a task is
	// enrolled in a study that is not running.
	eligibility, err := forecast.NewCounterfactualEligibility(false, []string{
		"adaptive routing is disabled until the fixed-policy evidence gate passes",
	})
	if err != nil {
		return TaskIntakeResult{}, err
	}

	forecastInput := TaskForecastInput{
		TaskID:                task.ID,
		BudgetID:              budgetID,
		BaselineModelRevision: request.BaselineModelRevision,
		RepositoryRevision:    request.RepositoryRevision,
		Fingerprint: fingerprint.ExactFingerprintInput{
			Project:            thread.ProjectID,
			Repository:         thread.RepositoryID,
			BaseRevision:       domain.RevisionBinding{Known: true, ExactRevision: request.RepositoryRevision},
			TaskClass:          request.TaskClass,
			AffectedPaths:      request.AffectedPaths,
			AffectedPackages:   request.AffectedPackages,
			AffectedSymbols:    request.AffectedSymbols,
			Bindings:           request.ToolchainBindings,
			Risk:               request.RiskLevel,
			RequiredAssurance:  request.RequiredAssurance,
			RequestedAuthority: request.RequestedAuthority,
		},
		TaskClass:                forecast.TaskClass(request.TaskClass),
		RepositorySize:           request.RepositorySize,
		ValidationCommands:       request.ValidationCommands,
		ToolConfigurationVersion: request.ToolConfigurationVersion,
		ValidationProfileVersion: request.ValidationProfileVersion,
		Eligibility:              eligibility,
		PolicyIdempotencyKey:     "intake-policy:" + request.IdempotencyKey,
		ForecastIdempotencyKey:   "intake-forecast:" + request.IdempotencyKey,
	}
	forecasted, err := service.ForecastTask(ctx, forecastInput)
	if err != nil {
		return TaskIntakeResult{}, err
	}

	return TaskIntakeResult{Task: task, Budget: forecasted.Budget, Forecasted: forecasted}, nil
}

// applyIntakeDefaults fills the fields with a safe conventional default.
// Every default here is conservative: it can only narrow what the task is
// permitted to do, never widen it.
func applyIntakeDefaults(request TaskIntakeRequest) TaskIntakeRequest {
	if request.PolicyPreset == "" {
		request.PolicyPreset = domain.PolicyPresetBalanced
	}
	if request.ReasoningEffort == "" {
		request.ReasoningEffort = domain.ReasoningEffortStandard
	}
	if request.RiskLevel == "" {
		// Routine is the floor, not a claim the work is safe: risk is
		// re-classified from the real diff at review time (§22), and the
		// router may only strengthen a floor, never weaken it.
		//
		// The floor is read from the requirement by the product's own
		// deterministic analysis, which is the same function the store applies
		// to the stored message when a run records what it is doing. Defaulting
		// to routine regardless of the text made the two disagree for any
		// request the analysis reads as heavier — one naming four files, say —
		// and the store then refused the requirement, the plan, and with them
		// every record the run would have left behind.
		request.RiskLevel = domain.RiskLevelRoutine
		if analysis, err := storage.AnalyzeTaskRequirement(
			strings.TrimSpace(request.Requirement),
		); err == nil {
			request.RiskLevel = analysis.RiskLevel
		}
	}
	if request.RequiredAssurance == "" {
		request.RequiredAssurance = domain.AssuranceLevelRuntimeOnly
	}
	if request.IdempotencyKey == "" {
		request.IdempotencyKey = "intake:" + request.ThreadID.String()
	}
	return request
}

// validateTaskIntakeRequest rejects an intake that would put an unobservable
// or unbounded value into the fingerprint or the task record.
func validateTaskIntakeRequest(request TaskIntakeRequest) error {
	if request.ThreadID.IsZero() {
		return errors.New("thread ID must not be empty")
	}
	requirement := strings.TrimSpace(request.Requirement)
	if requirement == "" {
		return errors.New("requirement must not be empty")
	}
	if len(request.Requirement) > MaximumRequirementBytes {
		return fmt.Errorf("requirement exceeds %d bytes", MaximumRequirementBytes)
	}
	if request.TaskClass == "" {
		return errors.New("task class must be declared: no classifier exists, and guessing it would put an unmeasured value inside the retrieval fingerprint")
	}
	if request.RepositoryRevision == "" {
		return errors.New("repository revision must be declared: a fingerprint bound to an unknown base cannot be compared against prior work")
	}
	if strings.TrimSpace(request.ToolConfigurationVersion) == "" {
		return errors.New("tool configuration version must be declared: forecasts made against different tool configurations are not comparable")
	}
	if strings.TrimSpace(request.ValidationProfileVersion) == "" {
		return errors.New("validation profile version must be declared: it pins the correctness floor the forecast assumes")
	}
	if strings.TrimSpace(request.BaselineModelRevision) == "" {
		return errors.New("baseline model revision must be declared: a fixed routing policy cannot be bound to an unknown model, and cost comparisons against it would be meaningless")
	}
	return nil
}
