package coordinator

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/executor"
	"codeflux.dev/codeflux/internal/review"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/testselection"
	"codeflux.dev/codeflux/internal/validation"
	"codeflux.dev/codeflux/internal/validationbaseline"
	"codeflux.dev/codeflux/internal/workspace"
)

const maximumValidationDiffRerunRounds = 4

// ValidationWorkflowRepository is the durable boundary shared by risk
// classification and exact-diff validation execution.
type ValidationWorkflowRepository interface {
	validation.RunRepository
	GetLatestTaskRiskClassification(context.Context, domain.TaskID) (storage.TaskRiskClassification, error)
	RecordInitialTaskRiskClassification(context.Context, domain.TaskID, []review.RiskSignal, domain.RiskLevel) (storage.TaskRiskClassification, error)
	EscalateTaskRiskClassification(context.Context, domain.TaskID, []review.RiskSignal, domain.RiskLevel) (storage.TaskRiskClassification, error)
}

type BaselineEvidencePair struct {
	Baseline  validationbaseline.Evidence
	Candidate validationbaseline.Evidence
}

type ValidationPlanningInput struct {
	TaskID             domain.TaskID
	RunID              domain.RunID
	RiskSignals        []review.RiskSignal
	UserRiskOverride   domain.RiskLevel
	TestSelection      testselection.Request
	DiscoveredCommands []workspace.SuggestedCommand
	BaselineEvidence   []BaselineEvidencePair
}

// ValidationReviewPlan preserves the selection explanations and baseline
// limitations alongside the executable immutable profile.
type ValidationReviewPlan struct {
	TaskID           domain.TaskID
	RunID            domain.RunID
	Risk             review.RiskClassification
	Tests            testselection.Result
	Profile          validation.Profile
	BaselineRecords  []validationbaseline.ReportRecord
	workingDirectory map[string]string
}

type ValidationExecutionInput struct {
	Plan               ValidationReviewPlan
	WorktreePath       string
	PermissionPolicy   executor.PermissionPolicy
	Environment        map[string]string
	AllowedEnvironment []string
	FirstRunAuthority  map[string]validation.FirstRunAuthority
	IdempotencyPrefix  string
}

type ValidationExecutionReport struct {
	Profile         validation.Profile
	Completions     []validation.RunCompletion
	BaselineRecords []validationbaseline.ReportRecord
	DiffIdentity    string
}

type validationReadyState struct {
	taskID        domain.TaskID
	profileDigest string
	diffIdentity  string
}

// CompletionValidationGate is required by completion orchestration. A
// changed diff cannot silently inherit the preceding successful run.
type CompletionValidationGate interface {
	EnsureCompletionReady(context.Context, domain.TaskID, domain.RunID) error
}

// ValidationReviewWorkflow is the production application seam joining risk,
// test selection, profile policy, execution, stale reruns, and baseline facts.
type ValidationReviewWorkflow struct {
	repository ValidationWorkflowRepository
	bindings   validation.BindingSource
	events     validation.EventSink
	service    *validation.Service

	mu    sync.RWMutex
	ready map[domain.RunID]validationReadyState
}

func NewValidationReviewWorkflow(
	repository ValidationWorkflowRepository,
	bindings validation.BindingSource,
	runner validation.Runner,
	events validation.EventSink,
) (*ValidationReviewWorkflow, error) {
	service, err := validation.NewService(repository, bindings, runner, events)
	if err != nil {
		return nil, err
	}
	return &ValidationReviewWorkflow{
		repository: repository, bindings: bindings, events: events,
		service: service, ready: make(map[domain.RunID]validationReadyState),
	}, nil
}

func (workflow *ValidationReviewWorkflow) Plan(
	ctx context.Context,
	input ValidationPlanningInput,
) (ValidationReviewPlan, error) {
	if workflow == nil || input.TaskID.IsZero() || input.RunID.IsZero() {
		return ValidationReviewPlan{}, errors.New("validation planning input is invalid")
	}
	classification, err := workflow.persistRiskClassification(
		ctx, input.TaskID, input.RiskSignals, input.UserRiskOverride,
	)
	if err != nil {
		return ValidationReviewPlan{}, err
	}
	selectionRequest := input.TestSelection
	selectionRequest.ProtectedChange = selectionRequest.ProtectedChange ||
		classification.SelectedRisk() == domain.RiskLevelProtected
	selected, err := testselection.Select(selectionRequest)
	if err != nil {
		return ValidationReviewPlan{}, err
	}
	commands, directories := validationProfileCommands(input.DiscoveredCommands, selected)
	profile, err := validation.SelectProfile(classification.SelectedRisk(), commands)
	if err != nil {
		return ValidationReviewPlan{}, err
	}
	workingDirectories := make(map[string]string, len(profile.Checks))
	for _, check := range profile.Checks {
		workingDirectories[check.ID] = directories[validationArgumentsKey(check.Arguments)]
	}
	baselines := make([]validationbaseline.ReportRecord, 0, len(input.BaselineEvidence))
	for _, pair := range input.BaselineEvidence {
		comparison, compareErr := validationbaseline.Compare(pair.Baseline, pair.Candidate)
		if compareErr != nil {
			return ValidationReviewPlan{}, compareErr
		}
		record, recordErr := validationbaseline.FinalReportRecord(comparison)
		if recordErr != nil {
			return ValidationReviewPlan{}, recordErr
		}
		baselines = append(baselines, record)
	}
	return ValidationReviewPlan{
		TaskID: input.TaskID, RunID: input.RunID, Risk: classification,
		Tests: selected, Profile: profile, BaselineRecords: baselines,
		workingDirectory: workingDirectories,
	}, nil
}

func (workflow *ValidationReviewWorkflow) persistRiskClassification(
	ctx context.Context,
	taskID domain.TaskID,
	signals []review.RiskSignal,
	override domain.RiskLevel,
) (review.RiskClassification, error) {
	_, err := workflow.repository.GetLatestTaskRiskClassification(ctx, taskID)
	if errors.Is(err, storage.ErrNotFound) {
		created, createErr := workflow.repository.RecordInitialTaskRiskClassification(ctx, taskID, signals, override)
		return created.Classification, createErr
	}
	if err != nil {
		return review.RiskClassification{}, err
	}
	escalated, err := workflow.repository.EscalateTaskRiskClassification(ctx, taskID, signals, override)
	if err != nil {
		return review.RiskClassification{}, err
	}
	return escalated.Classification, nil
}

func (workflow *ValidationReviewWorkflow) Execute(
	ctx context.Context,
	input ValidationExecutionInput,
) (ValidationExecutionReport, error) {
	if workflow == nil || input.Plan.TaskID.IsZero() || input.Plan.RunID.IsZero() ||
		strings.TrimSpace(input.WorktreePath) == "" ||
		strings.TrimSpace(input.IdempotencyPrefix) != input.IdempotencyPrefix || input.IdempotencyPrefix == "" {
		return ValidationExecutionReport{}, errors.New("validation execution input is invalid")
	}
	if err := input.Plan.Profile.Validate(); err != nil {
		return ValidationExecutionReport{}, err
	}
	profileDigest, err := input.Plan.Profile.Digest()
	if err != nil {
		return ValidationExecutionReport{}, err
	}
	workflow.mu.Lock()
	delete(workflow.ready, input.Plan.RunID)
	workflow.mu.Unlock()

	checks := slices.Clone(input.Plan.Profile.Checks)
	completions := make([]validation.RunCompletion, 0, len(checks))
	latest := make(map[string]validation.RunCompletion, len(checks))
	for round := 0; ; round++ {
		for _, check := range checks {
			authority := input.FirstRunAuthority[check.ID]
			var authorityPointer *validation.FirstRunAuthority
			if authority.CheckID != "" {
				authorityPointer = &authority
			}
			if err := validation.ValidateFirstRunAuthority(input.Plan.Profile, check, authorityPointer); err != nil {
				return ValidationExecutionReport{}, err
			}
			validationID, err := domain.NewValidationID()
			if err != nil {
				return ValidationExecutionReport{}, err
			}
			worktree := input.WorktreePath
			if relative := input.Plan.workingDirectory[check.ID]; relative != "" {
				worktree = filepath.Join(worktree, filepath.FromSlash(relative))
			}
			completion, err := workflow.service.Run(ctx, validation.RunRequest{
				ValidationID: validationID, TaskID: input.Plan.TaskID, RunID: input.Plan.RunID,
				Profile: input.Plan.Profile, Check: check, WorktreePath: worktree,
				PermissionPolicy: input.PermissionPolicy, Environment: input.Environment,
				AllowedEnvironment: slices.Clone(input.AllowedEnvironment),
				IdempotencyKey:     fmt.Sprintf("%s-r%d-%s", input.IdempotencyPrefix, round, check.ID),
			})
			if err != nil {
				return ValidationExecutionReport{}, err
			}
			completions = append(completions, completion)
			latest[check.ID] = completion
		}
		binding, err := workflow.bindings.CurrentValidationBinding(ctx, input.Plan.TaskID)
		if err != nil {
			return ValidationExecutionReport{}, err
		}
		invalidations, err := workflow.repository.InvalidateValidationRuns(ctx, input.Plan.RunID, binding.DiffIdentity)
		if err != nil {
			return ValidationExecutionReport{}, err
		}
		for _, invalidation := range invalidations {
			if err := workflow.events.ValidationInvalidated(ctx, invalidation); err != nil {
				return ValidationExecutionReport{}, err
			}
		}
		reruns, err := workflow.repository.ListRequiredValidationReruns(ctx, input.Plan.RunID, binding.DiffIdentity)
		if err != nil {
			return ValidationExecutionReport{}, err
		}
		if len(reruns) == 0 {
			for _, check := range input.Plan.Profile.Checks {
				if check.Strength != validation.RequirementRequired {
					continue
				}
				completion, exists := latest[check.ID]
				if !exists || completion.Result.State != domain.ValidationStatePassed ||
					completion.Intent.DiffIdentity != binding.DiffIdentity ||
					completion.Result.ObservedDiffIdentity != binding.DiffIdentity {
					return ValidationExecutionReport{}, ErrRequiredValidationIncomplete
				}
			}
			workflow.mu.Lock()
			workflow.ready[input.Plan.RunID] = validationReadyState{
				taskID: input.Plan.TaskID, profileDigest: profileDigest, diffIdentity: binding.DiffIdentity,
			}
			workflow.mu.Unlock()
			return ValidationExecutionReport{
				Profile: input.Plan.Profile, Completions: completions,
				BaselineRecords: slices.Clone(input.Plan.BaselineRecords), DiffIdentity: binding.DiffIdentity,
			}, nil
		}
		if round+1 >= maximumValidationDiffRerunRounds {
			return ValidationExecutionReport{}, ErrRequiredValidationIncomplete
		}
		checks = checksForRequiredReruns(input.Plan.Profile, reruns)
		if len(checks) != len(reruns) {
			return ValidationExecutionReport{}, ErrRequiredValidationIncomplete
		}
	}
}

func (workflow *ValidationReviewWorkflow) EnsureCompletionReady(
	ctx context.Context,
	taskID domain.TaskID,
	runID domain.RunID,
) error {
	if workflow == nil || taskID.IsZero() || runID.IsZero() {
		return ErrRequiredValidationIncomplete
	}
	workflow.mu.RLock()
	ready, exists := workflow.ready[runID]
	workflow.mu.RUnlock()
	if !exists || ready.taskID != taskID || ready.profileDigest == "" {
		return ErrRequiredValidationIncomplete
	}
	binding, err := workflow.bindings.CurrentValidationBinding(ctx, taskID)
	if err != nil {
		return errors.Join(ErrRequiredValidationIncomplete, err)
	}
	invalidations, err := workflow.repository.InvalidateValidationRuns(ctx, runID, binding.DiffIdentity)
	if err != nil {
		return errors.Join(ErrRequiredValidationIncomplete, err)
	}
	for _, invalidation := range invalidations {
		if err := workflow.events.ValidationInvalidated(ctx, invalidation); err != nil {
			return errors.Join(ErrRequiredValidationIncomplete, err)
		}
	}
	reruns, err := workflow.repository.ListRequiredValidationReruns(ctx, runID, binding.DiffIdentity)
	if err != nil {
		return errors.Join(ErrRequiredValidationIncomplete, err)
	}
	if ready.diffIdentity != binding.DiffIdentity || len(reruns) != 0 {
		workflow.mu.Lock()
		delete(workflow.ready, runID)
		workflow.mu.Unlock()
		return ErrRequiredValidationIncomplete
	}
	return nil
}

func validationProfileCommands(
	discovered []workspace.SuggestedCommand,
	selected testselection.Result,
) ([]workspace.SuggestedCommand, map[string]string) {
	commands := make([]workspace.SuggestedCommand, 0, len(discovered)+len(selected.Checks))
	directories := make(map[string]string, len(commands))
	seen := make(map[string]struct{}, len(commands))
	add := func(command workspace.SuggestedCommand, directory string) {
		key := validationArgumentsKey(command.Arguments)
		if _, duplicate := seen[key]; duplicate {
			return
		}
		seen[key] = struct{}{}
		commands = append(commands, command)
		directories[key] = directory
	}
	for _, command := range discovered {
		add(command, "")
	}
	for _, check := range selected.Checks {
		arguments := append([]string{check.Command.Executable}, check.Command.Arguments...)
		requiresApproval := slices.Contains(check.Reasons, testselection.ReasonUserAcceptance)
		add(workspace.SuggestedCommand{
			Kind: "test", Arguments: arguments,
			Source: "test-selection-policy-v1", RequiresApproval: requiresApproval,
		}, check.Command.WorkingDirectory)
	}
	return commands, directories
}

func validationArgumentsKey(arguments []string) string {
	var builder strings.Builder
	for _, argument := range arguments {
		fmt.Fprintf(&builder, "%d:%s", len(argument), argument)
	}
	return builder.String()
}

func checksForRequiredReruns(profile validation.Profile, reruns []validation.RequiredRerun) []validation.Check {
	result := make([]validation.Check, 0, len(reruns))
	for _, check := range profile.Checks {
		if slices.ContainsFunc(reruns, func(rerun validation.RequiredRerun) bool { return rerun.CheckID == check.ID }) {
			result = append(result, check)
		}
	}
	return result
}

var _ CompletionValidationGate = (*ValidationReviewWorkflow)(nil)
