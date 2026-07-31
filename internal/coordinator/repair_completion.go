package coordinator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	agentloop "codeflux.dev/codeflux/internal/agent"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/executor"
	"codeflux.dev/codeflux/internal/redact"
	"codeflux.dev/codeflux/internal/storage"
)

var (
	// ErrRepairBudgetExhausted distinguishes a repair stop from cancellation
	// or a validation failure.
	ErrRepairBudgetExhausted = errors.New("repair budget exhausted")
	// ErrRequiredValidationIncomplete prevents completion without the original
	// required validation floor.
	ErrRequiredValidationIncomplete = errors.New("required validation incomplete")
)

// ValidationCommandRun is one already-persisted validation and its mediated
// command outcome.
type ValidationCommandRun struct {
	ValidationID       domain.ValidationID
	CommandExecutionID string
	Result             executor.ToolResult
}

// ValidationCommandRunner executes and persists one selected validation. A
// non-zero command exit is returned in Result; error is reserved for an
// orchestration or persistence failure.
type ValidationCommandRunner interface {
	RunValidationCommand(
		context.Context,
		agentloop.ValidationCommand,
		uint32,
	) (ValidationCommandRun, error)
}

// PreRepairCheckpointCreator preserves the exact pre-repair worktree state.
type PreRepairCheckpointCreator interface {
	CreatePreRepairCheckpoint(
		context.Context,
		domain.TaskID,
		domain.RunID,
		uint64,
		uint32,
		string,
	) (domain.CheckpointID, error)
}

// SuccessfulValidationCheckpointCreator is the optional M15 checkpoint
// trigger implemented by checkpoint services that can bind a passed required
// validation to the current plan/worktree state.
type SuccessfulValidationCheckpointCreator interface {
	CreateSuccessfulValidationCheckpoint(
		context.Context,
		domain.TaskID,
		domain.RunID,
		uint64,
		domain.ValidationID,
		string,
	) (domain.CheckpointID, error)
}

// RepairRequest contains only bounded redacted failures from the immutable
// validation profile.
type RepairRequest struct {
	TaskID         domain.TaskID
	RunID          domain.RunID
	PlanRevision   uint64
	Ordinal        uint32
	CheckpointID   domain.CheckpointID
	ReasonRedacted string
	Failures       []agentloop.ValidationFailure
}

// RepairResult records the material scope changed by one bounded repair.
type RepairResult struct {
	ChangedFiles []string
}

// RepairExecutor performs one repair under the existing fixed-policy agent
// loop. It does not receive authority to alter the validation profile.
type RepairExecutor interface {
	ExecuteRepair(context.Context, RepairRequest) (RepairResult, error)
}

type validationAttributionRecord struct {
	TaskID                domain.TaskID
	RunID                 domain.RunID
	PlanRevision          uint64
	PlanStepIDs           []string
	ValidationID          domain.ValidationID
	CommandExecutionID    string
	RepairAttemptRevision *uint64
	ValidationPassed      bool
	ProfileDigest         string
	Round                 uint32
	CommandOrdinal        uint64
	CommandID             string
	CommandFingerprint    string
	Required              bool
	AcceptanceTest        bool
	State                 domain.ValidationState
	Failure               *agentloop.ValidationFailure
	IdempotencyKey        string
}

type selectedValidationProfileRecord struct {
	TaskID         domain.TaskID
	RunID          domain.RunID
	PlanRevision   uint64
	Profile        agentloop.ValidationProfile
	ProfileDigest  string
	IdempotencyKey string
}

type completionPlanStepRecord struct {
	PlanStepID string
	State      agentloop.StepState
}

type completionValidationAttributionRecord struct {
	TaskID       domain.TaskID
	RunID        domain.RunID
	PlanRevision uint64
	PlanStepID   string
	ValidationID domain.ValidationID
}

type completionEvidenceRecord struct {
	PlanSteps              []completionPlanStepRecord
	ValidationAttributions []completionValidationAttributionRecord
}

type beginRepairRecord struct {
	TaskID                domain.TaskID
	RunID                 domain.RunID
	PlanRevision          uint64
	Ordinal               uint32
	FailedValidationID    domain.ValidationID
	PreRepairCheckpointID domain.CheckpointID
	ReasonRedacted        string
	IdempotencyKey        string
}

type repairOutcomeKind string

const (
	repairValidationPassed repairOutcomeKind = "validation-passed"
	repairValidationFailed repairOutcomeKind = "validation-failed"
	repairBudgetExhausted  repairOutcomeKind = "budget-exhausted"
	repairStopped          repairOutcomeKind = "stopped"
)

type repairOutcomeRecord struct {
	TaskID                    domain.TaskID
	RunID                     domain.RunID
	PlanRevision              uint64
	RepairAttemptRevision     uint64
	Outcome                   repairOutcomeKind
	PostRepairValidationID    *domain.ValidationID
	UnresolvedSummaryRedacted string
	IdempotencyKey            string
}

type completionCandidateRecord struct {
	TaskID                 domain.TaskID
	RunID                  domain.RunID
	PlanRevision           uint64
	ExpectedTaskRevision   uint64
	ExpectedRunRevision    uint64
	EventID                domain.EventID
	EventIdempotencyKey    string
	RepositoryStatusJSON   string
	DiffSummaryJSON        string
	DiffSHA256             string
	ValidationSummaryJSON  string
	BudgetSummaryJSON      string
	AssumptionsJSON        string
	LimitationsJSON        string
	ImplementationComplete bool
	ValidationComplete     bool
	IdempotencyKey         string
}

type reviewDecisionRecord struct {
	TaskID               domain.TaskID
	RunID                domain.RunID
	PlanRevision         uint64
	CompletionRevision   uint64
	ExpectedTaskRevision uint64
	ExpectedRunRevision  uint64
	EventID              domain.EventID
	EventIdempotencyKey  string
	Decision             agentloop.ReviewDecision
	Actor                string
	AuthorityReference   string
	ReasonRedacted       string
	IdempotencyKey       string
}

// repairCompletionStore is an adapter boundary over the append-only M14
// storage API. The storage adapter maps these values without granting the
// coordinator direct SQL authority.
type repairCompletionStore interface {
	BindValidationProfile(
		context.Context,
		selectedValidationProfileRecord,
	) error
	RecordValidationAttribution(context.Context, validationAttributionRecord) error
	ReadFinalValidationReport(
		context.Context,
		domain.TaskID,
		domain.RunID,
		uint64,
	) (agentloop.ValidationReport, error)
	ReadCompletionEvidence(
		context.Context,
		domain.TaskID,
		domain.RunID,
		uint64,
	) (completionEvidenceRecord, error)
	BeginRepair(context.Context, beginRepairRecord) (uint64, error)
	RecordRepairOutcome(context.Context, repairOutcomeRecord) error
	RecordCompletion(
		context.Context,
		completionCandidateRecord,
	) (uint64, domain.TaskState, error)
	RecordReviewDecision(
		context.Context,
		reviewDecisionRecord,
	) (domain.TaskState, error)
}

// FinalRepositoryInspector captures final status and diff from the isolated
// task worktree.
type FinalRepositoryInspector interface {
	InspectFinalRepository(
		context.Context,
		domain.TaskID,
		domain.RunID,
	) (agentloop.RepositoryCompletionSummary, error)
}

// FinalBudgetInspector returns exact or explicitly unknown completion
// accounting.
type FinalBudgetInspector interface {
	InspectFinalBudget(
		context.Context,
		domain.TaskID,
		domain.RunID,
	) (agentloop.BudgetCompletionSummary, error)
}

// RepairCompletionDependencies are the explicit effect and persistence ports.
type RepairCompletionDependencies struct {
	Validations ValidationCommandRunner
	Checkpoints PreRepairCheckpointCreator
	Repairs     RepairExecutor
	Control     agentloop.ControlReader
	Store       repairCompletionStore
	Repository  FinalRepositoryInspector
	Budget      FinalBudgetInspector
	Redactor    *redact.Pipeline
}

// RepairCompletionService owns selected validation, bounded repair, completion
// evidence, and the explicit user review boundary.
type RepairCompletionService struct {
	dependencies RepairCompletionDependencies
}

// NewRepairCompletionService rejects a partially wired orchestration path.
func NewRepairCompletionService(
	dependencies RepairCompletionDependencies,
) (*RepairCompletionService, error) {
	if dependencies.Validations == nil ||
		dependencies.Checkpoints == nil ||
		dependencies.Repairs == nil ||
		dependencies.Control == nil ||
		dependencies.Store == nil ||
		dependencies.Repository == nil ||
		dependencies.Budget == nil ||
		dependencies.Redactor == nil {
		return nil, errors.New("repair completion dependencies are required")
	}
	return &RepairCompletionService{dependencies: dependencies}, nil
}

// NewStoredRepairCompletionService wires the coordinator to the authoritative
// SQLite repository without exposing storage records to the agent package.
func NewStoredRepairCompletionService(
	repositories *storage.Repositories,
	validations ValidationCommandRunner,
	checkpoints PreRepairCheckpointCreator,
	repairs RepairExecutor,
	control agentloop.ControlReader,
	repository FinalRepositoryInspector,
	budget FinalBudgetInspector,
	pipeline *redact.Pipeline,
) (*RepairCompletionService, error) {
	if repositories == nil {
		return nil, errors.New("repair completion repository is required")
	}
	return newStoredRepairCompletionService(
		repositories,
		validations,
		checkpoints,
		repairs,
		control,
		repository,
		budget,
		pipeline,
	)
}

func newStoredRepairCompletionService(
	repositories repairCompletionRepositories,
	validations ValidationCommandRunner,
	checkpoints PreRepairCheckpointCreator,
	repairs RepairExecutor,
	control agentloop.ControlReader,
	repository FinalRepositoryInspector,
	budget FinalBudgetInspector,
	pipeline *redact.Pipeline,
) (*RepairCompletionService, error) {
	if repositories == nil {
		return nil, errors.New("repair completion repository is required")
	}
	return NewRepairCompletionService(RepairCompletionDependencies{
		Validations: validations,
		Checkpoints: checkpoints,
		Repairs:     repairs,
		Control:     control,
		Store: &storageRepairCompletionStore{
			repositories: repositories,
		},
		Repository: repository,
		Budget:     budget,
		Redactor:   pipeline,
	})
}

type repairCompletionRepositories interface {
	BindRunValidationProfile(
		context.Context,
		storage.BindRunValidationProfile,
	) (storage.SelectedValidationProfileEvidence, error)
	GetRunValidationProfile(
		context.Context,
		domain.RunID,
	) (storage.SelectedValidationProfileEvidence, error)
	ListDurableValidationExecutions(
		context.Context,
		domain.RunID,
	) ([]storage.DurableValidationExecution, error)
	RecordPlanValidationAttributions(
		context.Context,
		storage.RecordPlanValidationAttributions,
	) ([]storage.PlanValidationAttribution, error)
	GetPlanRevision(
		context.Context,
		domain.TaskID,
		uint64,
	) (storage.PlanRevision, error)
	ListPlanStepStates(
		context.Context,
		domain.RunID,
	) ([]storage.PlanStepStatus, error)
	ListPlanValidationAttributions(
		context.Context,
		domain.RunID,
	) ([]storage.PlanValidationAttribution, error)
	BeginRepairAttempt(
		context.Context,
		storage.BeginRepairAttempt,
	) (storage.RepairAttempt, error)
	RecordRepairAttemptOutcome(
		context.Context,
		storage.RecordRepairAttemptOutcome,
	) (storage.RepairAttemptOutcome, error)
	RecordCompletionCandidate(
		context.Context,
		storage.RecordCompletionCandidate,
	) (storage.CompletionCandidate, error)
	RecordTaskReviewDecision(
		context.Context,
		storage.RecordTaskReviewDecision,
	) (storage.TaskReviewDecision, error)
}

type storageRepairCompletionStore struct {
	repositories repairCompletionRepositories
}

func (store *storageRepairCompletionStore) BindValidationProfile(
	ctx context.Context,
	record selectedValidationProfileRecord,
) error {
	commands := make(
		[]storage.SelectedValidationCommandEvidence,
		0,
		len(record.Profile.Commands),
	)
	for index, command := range record.Profile.Commands {
		fingerprint, err := command.Digest()
		if err != nil {
			return err
		}
		planCommand, err := agentloop.RenderValidationPlanCommand(
			command.Request,
		)
		if err != nil {
			return err
		}
		commands = append(commands, storage.SelectedValidationCommandEvidence{
			Ordinal: uint64(index + 1), CommandID: command.ID,
			CommandFingerprint: fingerprint, Required: command.Required,
			AcceptanceTest: command.AcceptanceTest, PlanCommand: planCommand,
			RelevantChangedFiles: append(
				[]string{},
				command.RelevantChangedFiles...,
			),
			PlanStepIDs: append([]string{}, command.PlanStepIDs...),
		})
	}
	_, err := store.repositories.BindRunValidationProfile(
		ctx,
		storage.BindRunValidationProfile{
			TaskID: record.TaskID, RunID: record.RunID,
			PlanRevision:   record.PlanRevision,
			ProfileName:    record.Profile.Name,
			ProfileVersion: record.Profile.Version,
			ProfileDigest:  record.ProfileDigest,
			Commands:       commands, IdempotencyKey: record.IdempotencyKey,
		},
	)
	return err
}

func (store *storageRepairCompletionStore) RecordValidationAttribution(
	ctx context.Context,
	record validationAttributionRecord,
) error {
	var commandExecutionID *string
	if record.CommandExecutionID != "" {
		value := record.CommandExecutionID
		commandExecutionID = &value
	}
	presentation := agentloop.ValidationExecution{
		ValidationID:       record.ValidationID,
		CommandID:          record.CommandID,
		CommandFingerprint: record.CommandFingerprint,
		Required:           record.Required,
		AcceptanceTest:     record.AcceptanceTest,
		PlanStepIDs:        append([]string{}, record.PlanStepIDs...),
		State:              record.State,
		CommandExecutionID: record.CommandExecutionID,
		Failure:            record.Failure,
	}
	presentationJSON, presentationSHA256, err :=
		validationExecutionPresentation(presentation)
	if err != nil {
		return err
	}
	_, err = store.repositories.RecordPlanValidationAttributions(
		ctx,
		storage.RecordPlanValidationAttributions{
			TaskID: record.TaskID, RunID: record.RunID,
			PlanRevision:             record.PlanRevision,
			PlanStepIDs:              append([]string{}, record.PlanStepIDs...),
			ValidationID:             record.ValidationID,
			CommandExecutionID:       commandExecutionID,
			RepairAttemptRevision:    record.RepairAttemptRevision,
			ValidationPassed:         record.ValidationPassed,
			ProfileDigest:            record.ProfileDigest,
			Round:                    uint64(record.Round),
			CommandOrdinal:           record.CommandOrdinal,
			CommandID:                record.CommandID,
			CommandFingerprint:       record.CommandFingerprint,
			PresentationRedactedJSON: presentationJSON,
			PresentationSHA256:       presentationSHA256,
			IdempotencyKey:           record.IdempotencyKey,
		},
	)
	return err
}

func (store *storageRepairCompletionStore) ReadFinalValidationReport(
	ctx context.Context,
	taskID domain.TaskID,
	runID domain.RunID,
	planRevision uint64,
) (agentloop.ValidationReport, error) {
	profile, err := store.repositories.GetRunValidationProfile(ctx, runID)
	if err != nil {
		return agentloop.ValidationReport{}, err
	}
	if profile.TaskID != taskID || profile.RunID != runID ||
		profile.PlanRevision != planRevision {
		return agentloop.ValidationReport{},
			errors.New("selected validation profile belongs to another plan")
	}
	executions, err := store.repositories.ListDurableValidationExecutions(
		ctx,
		runID,
	)
	if err != nil {
		return agentloop.ValidationReport{}, err
	}
	if len(executions) == 0 {
		return agentloop.ValidationReport{},
			ErrRequiredValidationIncomplete
	}
	var finalRound uint64
	for _, execution := range executions {
		if execution.Round > finalRound {
			finalRound = execution.Round
		}
	}
	if finalRound > math.MaxUint32 {
		return agentloop.ValidationReport{},
			errors.New("durable validation round is unbounded")
	}
	final := make([]storage.DurableValidationExecution, 0, len(profile.Commands))
	for _, execution := range executions {
		if execution.Round == finalRound {
			final = append(final, execution)
		}
	}
	if len(final) != len(profile.Commands) {
		return agentloop.ValidationReport{},
			ErrRequiredValidationIncomplete
	}
	report := agentloop.ValidationReport{
		ProfileName:    profile.ProfileName,
		ProfileVersion: profile.ProfileVersion,
		ProfileDigest:  profile.ProfileDigest,
		Round:          uint32(finalRound),
	}
	for index, command := range profile.Commands {
		execution := final[index]
		if command.Ordinal != uint64(index+1) ||
			strings.TrimSpace(command.PlanCommand) != command.PlanCommand ||
			command.PlanCommand == "" ||
			!canonicalRepositoryRelativePaths(
				command.RelevantChangedFiles,
				128,
			) ||
			execution.TaskID != taskID || execution.RunID != runID ||
			execution.PlanRevision != planRevision ||
			execution.ProfileDigest != profile.ProfileDigest ||
			execution.Ordinal != command.Ordinal ||
			execution.CommandID != command.CommandID ||
			execution.CommandFingerprint != command.CommandFingerprint ||
			!slices.Equal(execution.PlanStepIDs, command.PlanStepIDs) ||
			execution.ValidationPassed !=
				(execution.State == domain.ValidationStatePassed) {
			return agentloop.ValidationReport{},
				errors.New("durable final validation pass differs from selection")
		}
		presentationHash := sha256.Sum256(
			[]byte(execution.PresentationRedactedJSON),
		)
		if hex.EncodeToString(presentationHash[:]) !=
			execution.PresentationSHA256 {
			return agentloop.ValidationReport{},
				errors.New("durable validation presentation hash differs")
		}
		var value agentloop.ValidationExecution
		if err := json.Unmarshal(
			[]byte(execution.PresentationRedactedJSON),
			&value,
		); err != nil {
			return agentloop.ValidationReport{},
				errors.New("durable validation presentation is malformed")
		}
		commandExecutionID := ""
		if execution.CommandExecutionID != nil {
			commandExecutionID = *execution.CommandExecutionID
		}
		if value.ValidationID != execution.ValidationID ||
			value.CommandID != command.CommandID ||
			value.CommandFingerprint != command.CommandFingerprint ||
			value.Required != command.Required ||
			value.AcceptanceTest != command.AcceptanceTest ||
			!slices.Equal(value.PlanStepIDs, command.PlanStepIDs) ||
			value.State != execution.State ||
			value.CommandExecutionID != commandExecutionID ||
			!validDurableFailureLinkage(value, execution, command) {
			return agentloop.ValidationReport{},
				errors.New("durable validation presentation differs from facts")
		}
		report.Executions = append(report.Executions, value)
	}
	if err := report.Validate(); err != nil {
		return agentloop.ValidationReport{},
			errors.Join(ErrRequiredValidationIncomplete, err)
	}
	return report, nil
}

func validDurableFailureLinkage(
	value agentloop.ValidationExecution,
	execution storage.DurableValidationExecution,
	command storage.SelectedValidationCommandEvidence,
) bool {
	switch execution.State {
	case domain.ValidationStatePassed:
		return value.Failure == nil &&
			!execution.FailurePresent &&
			execution.FailureSummaryRedacted == "" &&
			len(execution.FailureChangedFiles) == 0 &&
			len(execution.FailurePlanStepIDs) == 0 &&
			!execution.OutputTruncated
	case domain.ValidationStateFailed, domain.ValidationStateCancelled:
		if value.Failure == nil ||
			!execution.FailurePresent ||
			value.Failure.SummaryRedacted !=
				execution.FailureSummaryRedacted ||
			!slices.Equal(
				value.Failure.ChangedFiles,
				execution.FailureChangedFiles,
			) ||
			!slices.Equal(
				value.Failure.PlanStepIDs,
				execution.FailurePlanStepIDs,
			) ||
			value.Failure.OutputTruncated != execution.OutputTruncated ||
			!slices.Equal(value.Failure.PlanStepIDs, command.PlanStepIDs) ||
			!canonicalRepositoryRelativePaths(
				value.Failure.ChangedFiles,
				128,
			) {
			return false
		}
		for _, changedFile := range value.Failure.ChangedFiles {
			if !pathWithinCanonicalScopes(
				changedFile,
				command.RelevantChangedFiles,
			) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func pathWithinCanonicalScopes(path string, scopes []string) bool {
	for _, scope := range scopes {
		if path == scope || strings.HasPrefix(path, scope+"/") {
			return true
		}
	}
	return false
}

func canonicalRepositoryRelativePaths(paths []string, maximum int) bool {
	if len(paths) > maximum {
		return false
	}
	seen := make(map[string]struct{}, len(paths))
	previous := ""
	for index, path := range paths {
		if path == "" ||
			strings.TrimSpace(path) != path ||
			len(path) > 4096 ||
			strings.Contains(path, "\\") ||
			filepath.IsAbs(path) ||
			filepath.VolumeName(path) != "" {
			return false
		}
		normalized := filepath.ToSlash(filepath.Clean(path))
		if normalized != path ||
			normalized == "." ||
			normalized == ".." ||
			strings.HasPrefix(normalized, "../") ||
			(index > 0 && path <= previous) {
			return false
		}
		if _, duplicate := seen[path]; duplicate {
			return false
		}
		seen[path] = struct{}{}
		previous = path
	}
	return true
}

func (store *storageRepairCompletionStore) ReadCompletionEvidence(
	ctx context.Context,
	taskID domain.TaskID,
	runID domain.RunID,
	planRevision uint64,
) (completionEvidenceRecord, error) {
	plan, err := store.repositories.GetPlanRevision(ctx, taskID, planRevision)
	if err != nil {
		return completionEvidenceRecord{}, err
	}
	statuses, err := store.repositories.ListPlanStepStates(ctx, runID)
	if err != nil {
		return completionEvidenceRecord{}, err
	}
	links, err := store.repositories.ListPlanValidationAttributions(ctx, runID)
	if err != nil {
		return completionEvidenceRecord{}, err
	}
	statusByID := make(map[string]storage.PlanStepStatus, len(statuses))
	for _, status := range statuses {
		if status.TaskID != taskID || status.RunID != runID ||
			status.PlanRevision != planRevision {
			return completionEvidenceRecord{},
				errors.New("durable plan-step state belongs to another plan")
		}
		if _, duplicate := statusByID[status.PlanStepID]; duplicate {
			return completionEvidenceRecord{},
				errors.New("durable plan-step state is duplicated")
		}
		statusByID[status.PlanStepID] = status
	}
	evidence := completionEvidenceRecord{
		PlanSteps: make(
			[]completionPlanStepRecord,
			0,
			len(plan.Plan.Steps),
		),
	}
	for _, step := range plan.Plan.Steps {
		status, exists := statusByID[step.ID]
		if !exists {
			return completionEvidenceRecord{},
				errors.New("durable plan-step state is missing")
		}
		state, err := agentPlanStepState(status.State)
		if err != nil {
			return completionEvidenceRecord{}, err
		}
		evidence.PlanSteps = append(
			evidence.PlanSteps,
			completionPlanStepRecord{PlanStepID: step.ID, State: state},
		)
		delete(statusByID, step.ID)
	}
	if len(statusByID) != 0 {
		return completionEvidenceRecord{},
			errors.New("durable plan-step state set differs from the plan")
	}
	for _, link := range links {
		evidence.ValidationAttributions = append(
			evidence.ValidationAttributions,
			completionValidationAttributionRecord{
				TaskID: link.TaskID, RunID: link.RunID,
				PlanRevision: link.PlanRevision,
				PlanStepID:   link.PlanStepID,
				ValidationID: link.ValidationID,
			},
		)
	}
	return evidence, nil
}

func (store *storageRepairCompletionStore) BeginRepair(
	ctx context.Context,
	record beginRepairRecord,
) (uint64, error) {
	value, err := store.repositories.BeginRepairAttempt(
		ctx,
		storage.BeginRepairAttempt{
			TaskID: record.TaskID, RunID: record.RunID,
			PlanRevision: record.PlanRevision, Ordinal: uint64(record.Ordinal),
			FailedValidationID:    record.FailedValidationID,
			PreRepairCheckpointID: record.PreRepairCheckpointID,
			ReasonRedacted:        record.ReasonRedacted,
			IdempotencyKey:        record.IdempotencyKey,
		},
	)
	return value.Revision, err
}

func (store *storageRepairCompletionStore) RecordRepairOutcome(
	ctx context.Context,
	record repairOutcomeRecord,
) error {
	var outcome storage.RepairAttemptOutcomeKind
	switch record.Outcome {
	case repairValidationPassed:
		outcome = storage.RepairOutcomeValidationPassed
	case repairValidationFailed:
		outcome = storage.RepairOutcomeValidationFailed
	case repairBudgetExhausted:
		outcome = storage.RepairOutcomeBudgetExhausted
	case repairStopped:
		outcome = storage.RepairOutcomeStopped
	default:
		return errors.New("repair outcome is unsupported")
	}
	_, err := store.repositories.RecordRepairAttemptOutcome(
		ctx,
		storage.RecordRepairAttemptOutcome{
			TaskID: record.TaskID, RunID: record.RunID,
			PlanRevision:              record.PlanRevision,
			RepairAttemptRevision:     record.RepairAttemptRevision,
			Outcome:                   outcome,
			PostRepairValidationID:    record.PostRepairValidationID,
			UnresolvedSummaryRedacted: record.UnresolvedSummaryRedacted,
			IdempotencyKey:            record.IdempotencyKey,
		},
	)
	return err
}

func (store *storageRepairCompletionStore) RecordCompletion(
	ctx context.Context,
	record completionCandidateRecord,
) (uint64, domain.TaskState, error) {
	value, err := store.repositories.RecordCompletionCandidate(
		ctx,
		storage.RecordCompletionCandidate{
			TaskID: record.TaskID, RunID: record.RunID,
			PlanRevision:           record.PlanRevision,
			ExpectedTaskRevision:   record.ExpectedTaskRevision,
			ExpectedRunRevision:    record.ExpectedRunRevision,
			EventID:                record.EventID,
			EventIdempotencyKey:    record.EventIdempotencyKey,
			RepositoryStatusJSON:   record.RepositoryStatusJSON,
			DiffSummaryJSON:        record.DiffSummaryJSON,
			DiffSHA256:             record.DiffSHA256,
			ValidationSummaryJSON:  record.ValidationSummaryJSON,
			BudgetSummaryJSON:      record.BudgetSummaryJSON,
			AssumptionsJSON:        record.AssumptionsJSON,
			LimitationsJSON:        record.LimitationsJSON,
			ImplementationComplete: record.ImplementationComplete,
			ValidationComplete:     record.ValidationComplete,
			IdempotencyKey:         record.IdempotencyKey,
		},
	)
	if err != nil {
		return 0, "", err
	}
	return value.Revision, domain.TaskStateAwaitingReview, nil
}

func (store *storageRepairCompletionStore) RecordReviewDecision(
	ctx context.Context,
	record reviewDecisionRecord,
) (domain.TaskState, error) {
	var decision storage.TaskReviewDecisionKind
	switch record.Decision {
	case agentloop.ReviewDecisionAccept:
		decision = storage.TaskReviewAccept
	case agentloop.ReviewDecisionRequestRepair:
		decision = storage.TaskReviewRequestRepair
	case agentloop.ReviewDecisionRollback:
		decision = storage.TaskReviewRollback
	case agentloop.ReviewDecisionAbandon:
		decision = storage.TaskReviewAbandon
	default:
		return "", agentloop.ErrInvalidReviewDecision
	}
	_, err := store.repositories.RecordTaskReviewDecision(
		ctx,
		storage.RecordTaskReviewDecision{
			TaskID: record.TaskID, RunID: record.RunID,
			PlanRevision:         record.PlanRevision,
			CompletionRevision:   record.CompletionRevision,
			ExpectedTaskRevision: record.ExpectedTaskRevision,
			ExpectedRunRevision:  record.ExpectedRunRevision,
			Decision:             decision, ActorReference: record.Actor,
			AuthorityReference:  record.AuthorityReference,
			ReasonRedacted:      record.ReasonRedacted,
			EventID:             record.EventID,
			EventIdempotencyKey: record.EventIdempotencyKey,
			IdempotencyKey:      record.IdempotencyKey,
		},
	)
	if err != nil {
		return "", err
	}
	return record.Decision.ResultingTaskState()
}

// ValidationRepairInput binds repair to one immutable plan/profile and fixed
// maximum. The caller cannot raise MaximumRepairRounds during this invocation.
type ValidationRepairInput struct {
	TaskID              domain.TaskID
	RunID               domain.RunID
	PlanRevision        uint64
	Profile             agentloop.ValidationProfile
	ChangedFiles        []string
	MaximumRepairRounds uint32
	IdempotencyPrefix   string
}

// ValidationRepairOutcome retains every validation pass and honest stop
// reason. ReadyForCompletion never implies user acceptance.
type ValidationRepairOutcome struct {
	Reports            []agentloop.ValidationReport
	RepairRounds       uint32
	ReadyForCompletion bool
	BudgetExhausted    bool
	UnresolvedReason   string
}

// ValidateAndRepair runs the complete selected profile, preserves a checkpoint
// before each bounded repair, and reruns the exact complete profile after every
// repair so no acceptance command can silently disappear.
func (service *RepairCompletionService) ValidateAndRepair(
	ctx context.Context,
	input ValidationRepairInput,
) (ValidationRepairOutcome, error) {
	if service == nil {
		return ValidationRepairOutcome{}, errors.New(
			"repair completion service is unavailable",
		)
	}
	if input.PlanRevision == 0 ||
		!boundedCoordinatorText(input.IdempotencyPrefix, 180) {
		return ValidationRepairOutcome{}, errors.New(
			"plan revision and idempotency prefix are required",
		)
	}
	profile := cloneValidationProfile(input.Profile)
	if err := profile.Validate(input.TaskID, input.RunID); err != nil {
		return ValidationRepairOutcome{}, err
	}
	profileDigest, err := profile.Digest()
	if err != nil {
		return ValidationRepairOutcome{}, err
	}
	if err := service.dependencies.Store.BindValidationProfile(
		ctx,
		selectedValidationProfileRecord{
			TaskID: input.TaskID, RunID: input.RunID,
			PlanRevision: input.PlanRevision,
			Profile:      profile, ProfileDigest: profileDigest,
			IdempotencyKey: input.IdempotencyPrefix + "/validation-profile",
		},
	); err != nil {
		return ValidationRepairOutcome{}, err
	}
	report, err := service.runValidationPass(
		ctx,
		input,
		profile,
		profileDigest,
		0,
		nil,
		input.ChangedFiles,
	)
	if err != nil {
		return ValidationRepairOutcome{}, err
	}
	outcome := ValidationRepairOutcome{
		Reports: []agentloop.ValidationReport{report},
	}
	if report.RequiredPassed() {
		if err := service.checkpointSuccessfulValidation(
			ctx,
			input,
			report,
			input.IdempotencyPrefix+"/validation/0/checkpoint",
		); err != nil {
			return outcome, err
		}
		outcome.ReadyForCompletion = true
		return outcome, nil
	}
	for ordinal := uint32(1); ordinal <= input.MaximumRepairRounds; ordinal++ {
		control, controlErr := service.dependencies.Control.ReadControl(
			ctx,
			input.TaskID,
			input.RunID,
		)
		if controlErr != nil {
			return outcome, controlErr
		}
		if control.Disposition != agentloop.ControlActive {
			outcome.UnresolvedReason = "repair stopped by current task control state"
			return outcome, nil
		}
		if !control.PolicyCurrent {
			outcome.UnresolvedReason = "repair stopped because policy is stale"
			return outcome, nil
		}
		if !control.BudgetAvailable {
			outcome.BudgetExhausted = true
			outcome.UnresolvedReason = ErrRepairBudgetExhausted.Error()
			return outcome, nil
		}
		reason := unresolvedValidationSummary(report)
		checkpointID, checkpointErr :=
			service.dependencies.Checkpoints.CreatePreRepairCheckpoint(
				ctx,
				input.TaskID,
				input.RunID,
				input.PlanRevision,
				ordinal,
				reason,
			)
		if checkpointErr != nil {
			return outcome, checkpointErr
		}
		failedValidationID, found := firstFailedRequiredValidation(report)
		if !found {
			return outcome, errors.New(
				"required validation failed without attributable validation ID",
			)
		}
		repairRevision, beginErr := service.dependencies.Store.BeginRepair(
			ctx,
			beginRepairRecord{
				TaskID: input.TaskID, RunID: input.RunID,
				PlanRevision: input.PlanRevision, Ordinal: ordinal,
				FailedValidationID:    failedValidationID,
				PreRepairCheckpointID: checkpointID,
				ReasonRedacted:        reason,
				IdempotencyKey: fmt.Sprintf(
					"%s/repair/%d",
					input.IdempotencyPrefix,
					ordinal,
				),
			},
		)
		if beginErr != nil {
			return outcome, beginErr
		}
		repairResult, repairErr := service.dependencies.Repairs.ExecuteRepair(
			ctx,
			RepairRequest{
				TaskID: input.TaskID, RunID: input.RunID,
				PlanRevision: input.PlanRevision, Ordinal: ordinal,
				CheckpointID: checkpointID, ReasonRedacted: reason,
				Failures: validationFailures(report),
			},
		)
		outcome.RepairRounds = ordinal
		if repairErr != nil {
			recordErr := service.dependencies.Store.RecordRepairOutcome(
				context.WithoutCancel(ctx),
				repairOutcomeRecord{
					TaskID: input.TaskID, RunID: input.RunID,
					PlanRevision:          input.PlanRevision,
					RepairAttemptRevision: repairRevision,
					Outcome:               repairStopped,
					UnresolvedSummaryRedacted: boundedCoordinatorSummary(
						"repair failed: " + repairErr.Error(),
					),
					IdempotencyKey: fmt.Sprintf(
						"%s/repair/%d/outcome",
						input.IdempotencyPrefix,
						ordinal,
					),
				},
			)
			return outcome, errors.Join(repairErr, recordErr)
		}
		currentDigest, digestErr := profile.Digest()
		if digestErr != nil || currentDigest != profileDigest {
			return outcome, errors.Join(
				ErrRequiredValidationIncomplete,
				digestErr,
				errors.New("selected validation profile changed before rerun"),
			)
		}
		changedFiles := mergeChangedFiles(
			input.ChangedFiles,
			repairResult.ChangedFiles,
		)
		report, err = service.runValidationPass(
			ctx,
			input,
			profile,
			profileDigest,
			ordinal,
			&repairRevision,
			changedFiles,
		)
		if err != nil {
			return outcome, err
		}
		outcome.Reports = append(outcome.Reports, report)
		postValidationID, passedIDFound := firstPassedRequiredValidation(report)
		record := repairOutcomeRecord{
			TaskID: input.TaskID, RunID: input.RunID,
			PlanRevision:              input.PlanRevision,
			RepairAttemptRevision:     repairRevision,
			Outcome:                   repairValidationFailed,
			UnresolvedSummaryRedacted: unresolvedValidationSummary(report),
			IdempotencyKey: fmt.Sprintf(
				"%s/repair/%d/outcome",
				input.IdempotencyPrefix,
				ordinal,
			),
		}
		if report.RequiredPassed() {
			if !passedIDFound {
				return outcome, errors.New(
					"required validation passed without attributable validation ID",
				)
			}
			record.Outcome = repairValidationPassed
			record.PostRepairValidationID = &postValidationID
			record.UnresolvedSummaryRedacted =
				"all required validation passed after bounded repair"
		} else if ordinal < input.MaximumRepairRounds {
			nextControl, controlErr := service.dependencies.Control.ReadControl(
				ctx,
				input.TaskID,
				input.RunID,
			)
			if controlErr != nil {
				record.Outcome = repairStopped
				record.UnresolvedSummaryRedacted =
					"repair stopped because current control state is unavailable"
				recordErr := service.dependencies.Store.RecordRepairOutcome(
					context.WithoutCancel(ctx),
					record,
				)
				return outcome, errors.Join(controlErr, recordErr)
			}
			switch {
			case !nextControl.BudgetAvailable:
				record.Outcome = repairBudgetExhausted
				record.UnresolvedSummaryRedacted =
					ErrRepairBudgetExhausted.Error()
				if err := service.dependencies.Store.RecordRepairOutcome(
					context.WithoutCancel(ctx),
					record,
				); err != nil {
					return outcome, err
				}
				outcome.BudgetExhausted = true
				outcome.UnresolvedReason = ErrRepairBudgetExhausted.Error()
				return outcome, nil
			case nextControl.Disposition != agentloop.ControlActive:
				record.Outcome = repairStopped
				record.UnresolvedSummaryRedacted =
					"repair stopped by current task control state"
			case !nextControl.PolicyCurrent:
				record.Outcome = repairStopped
				record.UnresolvedSummaryRedacted =
					"repair stopped because policy is stale"
			}
		}
		if err := service.dependencies.Store.RecordRepairOutcome(
			context.WithoutCancel(ctx),
			record,
		); err != nil {
			return outcome, err
		}
		if record.Outcome == repairStopped {
			outcome.UnresolvedReason = record.UnresolvedSummaryRedacted
			return outcome, nil
		}
		if report.RequiredPassed() {
			if err := service.checkpointSuccessfulValidation(
				ctx,
				input,
				report,
				fmt.Sprintf(
					"%s/validation/%d/checkpoint",
					input.IdempotencyPrefix,
					ordinal,
				),
			); err != nil {
				return outcome, err
			}
			outcome.ReadyForCompletion = true
			outcome.UnresolvedReason = ""
			return outcome, nil
		}
	}
	outcome.UnresolvedReason = boundedCoordinatorSummary(
		"repair limit reached; " +
			unresolvedValidationSummary(outcome.Reports[len(outcome.Reports)-1]),
	)
	return outcome, nil
}

func (service *RepairCompletionService) checkpointSuccessfulValidation(
	ctx context.Context,
	input ValidationRepairInput,
	report agentloop.ValidationReport,
	idempotencyKey string,
) error {
	checkpoints, ok := service.dependencies.Checkpoints.(SuccessfulValidationCheckpointCreator)
	if !ok {
		return nil
	}
	validationID, found := firstPassedRequiredValidation(report)
	if !found {
		return errors.New(
			"successful required validation lacks an attributable identity",
		)
	}
	_, err := checkpoints.CreateSuccessfulValidationCheckpoint(
		context.WithoutCancel(ctx),
		input.TaskID,
		input.RunID,
		input.PlanRevision,
		validationID,
		idempotencyKey,
	)
	return err
}

// CompletionInput contains the non-model claims that must be checked before
// transitioning to awaiting-review.
type CompletionInput struct {
	TaskID               domain.TaskID
	RunID                domain.RunID
	PlanRevision         uint64
	ExpectedTaskRevision uint64
	ExpectedRunRevision  uint64
	EventID              domain.EventID
	EventIdempotencyKey  string
	Assumptions          []string
	Limitations          []string
	IdempotencyKey       string
}

// CompletionResult exposes the exact candidate revision and awaiting-review
// state; it cannot represent automatic acceptance.
type CompletionResult struct {
	Revision uint64
	State    domain.TaskState
	Summary  agentloop.CompletionSummary
}

// PrepareCompletion captures final Git, validation, budget, assumption, and
// limitation evidence and atomically transitions to awaiting-review.
func (service *RepairCompletionService) PrepareCompletion(
	ctx context.Context,
	input CompletionInput,
) (CompletionResult, error) {
	if service == nil {
		return CompletionResult{}, errors.New(
			"repair completion service is unavailable",
		)
	}
	report, err := service.dependencies.Store.ReadFinalValidationReport(
		ctx,
		input.TaskID,
		input.RunID,
		input.PlanRevision,
	)
	if err != nil {
		return CompletionResult{}, errors.Join(
			ErrRequiredValidationIncomplete,
			err,
		)
	}
	if !report.RequiredPassed() {
		return CompletionResult{}, ErrRequiredValidationIncomplete
	}
	evidence, err := service.dependencies.Store.ReadCompletionEvidence(
		ctx,
		input.TaskID,
		input.RunID,
		input.PlanRevision,
	)
	if err != nil {
		return CompletionResult{}, err
	}
	if err := validateCompletionEvidence(
		input.TaskID,
		input.RunID,
		input.PlanRevision,
		report,
		evidence,
	); err != nil {
		return CompletionResult{}, err
	}
	repository, err := service.dependencies.Repository.InspectFinalRepository(
		ctx,
		input.TaskID,
		input.RunID,
	)
	if err != nil {
		return CompletionResult{}, err
	}
	repository, err = service.redactRepositorySummary(repository)
	if err != nil {
		return CompletionResult{}, err
	}
	budget, err := service.dependencies.Budget.InspectFinalBudget(
		ctx,
		input.TaskID,
		input.RunID,
	)
	if err != nil {
		return CompletionResult{}, err
	}
	summary := agentloop.CompletionSummary{
		TaskID: input.TaskID, RunID: input.RunID,
		PlanRevision: input.PlanRevision,
		Repository:   repository, Validation: report, Budget: budget,
		Assumptions:            append([]string{}, input.Assumptions...),
		Limitations:            append([]string{}, input.Limitations...),
		ImplementationComplete: true,
		ValidationComplete:     true,
		State:                  domain.TaskStateAwaitingReview,
	}
	if err := summary.Validate(); err != nil {
		return CompletionResult{}, err
	}
	repositoryJSON, diffJSON, validationJSON, budgetJSON,
		assumptionsJSON, limitationsJSON, err := marshalCompletionEvidence(summary)
	if err != nil {
		return CompletionResult{}, err
	}
	revision, state, err := service.dependencies.Store.RecordCompletion(
		ctx,
		completionCandidateRecord{
			TaskID: input.TaskID, RunID: input.RunID,
			PlanRevision:         input.PlanRevision,
			ExpectedTaskRevision: input.ExpectedTaskRevision,
			ExpectedRunRevision:  input.ExpectedRunRevision,
			EventID:              input.EventID,
			EventIdempotencyKey:  input.EventIdempotencyKey,
			RepositoryStatusJSON: repositoryJSON,
			DiffSummaryJSON:      diffJSON, DiffSHA256: repository.DiffSHA256,
			ValidationSummaryJSON:  validationJSON,
			BudgetSummaryJSON:      budgetJSON,
			AssumptionsJSON:        assumptionsJSON,
			LimitationsJSON:        limitationsJSON,
			ImplementationComplete: summary.ImplementationComplete,
			ValidationComplete:     summary.ValidationComplete,
			IdempotencyKey:         input.IdempotencyKey,
		},
	)
	if err != nil {
		return CompletionResult{}, err
	}
	if state != domain.TaskStateAwaitingReview {
		return CompletionResult{}, errors.New(
			"completion persistence did not enter awaiting-review",
		)
	}
	return CompletionResult{Revision: revision, State: state, Summary: summary}, nil
}

// DecideReview records and atomically applies one explicit user choice.
func (service *RepairCompletionService) DecideReview(
	ctx context.Context,
	request agentloop.ReviewDecisionRequest,
	eventID domain.EventID,
	eventIdempotencyKey string,
) (domain.TaskState, error) {
	if service == nil {
		return "", errors.New("repair completion service is unavailable")
	}
	if err := request.Validate(); err != nil {
		return "", err
	}
	if eventID.IsZero() ||
		!boundedCoordinatorText(eventIdempotencyKey, 255) {
		return "", errors.New("review decision event attribution is required")
	}
	expected, err := request.Decision.ResultingTaskState()
	if err != nil {
		return "", err
	}
	state, err := service.dependencies.Store.RecordReviewDecision(
		ctx,
		reviewDecisionRecord{
			TaskID: request.TaskID, RunID: request.RunID,
			PlanRevision:         request.PlanRevision,
			CompletionRevision:   request.CompletionRevision,
			ExpectedTaskRevision: request.ExpectedTaskRevision,
			ExpectedRunRevision:  request.ExpectedRunRevision,
			EventID:              eventID, EventIdempotencyKey: eventIdempotencyKey,
			Decision: request.Decision, Actor: request.Actor,
			AuthorityReference: request.AuthorityReference,
			ReasonRedacted:     request.ReasonRedacted,
			IdempotencyKey:     request.IdempotencyKey,
		},
	)
	if err != nil {
		return "", err
	}
	if state != expected {
		return "", errors.New("recorded review decision returned an invalid task state")
	}
	return state, nil
}

func (service *RepairCompletionService) runValidationPass(
	ctx context.Context,
	input ValidationRepairInput,
	profile agentloop.ValidationProfile,
	expectedDigest string,
	round uint32,
	repairRevision *uint64,
	changedFiles []string,
) (agentloop.ValidationReport, error) {
	currentDigest, err := profile.Digest()
	if err != nil || currentDigest != expectedDigest {
		return agentloop.ValidationReport{}, errors.Join(
			ErrRequiredValidationIncomplete,
			err,
			errors.New("selected validation profile changed"),
		)
	}
	report := agentloop.ValidationReport{
		ProfileName: profile.Name, ProfileVersion: profile.Version,
		ProfileDigest: expectedDigest, Round: round,
	}
	for commandIndex, selected := range profile.Commands {
		command := cloneValidationCommand(selected)
		fingerprint, err := command.Digest()
		if err != nil {
			return report, err
		}
		run, runErr := service.dependencies.Validations.RunValidationCommand(
			ctx,
			command,
			round,
		)
		if run.ValidationID.IsZero() {
			return report, errors.Join(
				runErr,
				errors.New("validation runner returned no durable validation ID"),
			)
		}
		execution := agentloop.ValidationExecution{
			ValidationID: run.ValidationID, CommandID: command.ID,
			CommandFingerprint: fingerprint, Required: command.Required,
			AcceptanceTest:     command.AcceptanceTest,
			PlanStepIDs:        append([]string{}, command.PlanStepIDs...),
			CommandExecutionID: run.CommandExecutionID,
			State:              domain.ValidationStateFailed,
		}
		switch {
		case run.Result.Cancelled || errors.Is(runErr, context.Canceled):
			execution.State = domain.ValidationStateCancelled
		case runErr == nil &&
			run.Result.State == "succeeded" &&
			run.Result.ExitCode == 0:
			execution.State = domain.ValidationStatePassed
		}
		if execution.State != domain.ValidationStatePassed {
			failure, parseErr := agentloop.ParseValidationFailure(
				command,
				run.Result,
				runErr,
				changedFiles,
				service.dependencies.Redactor,
			)
			if parseErr != nil {
				return report, parseErr
			}
			execution.Failure = &failure
		}
		if err := service.dependencies.Store.RecordValidationAttribution(
			ctx,
			validationAttributionRecord{
				TaskID: input.TaskID, RunID: input.RunID,
				PlanRevision:          input.PlanRevision,
				PlanStepIDs:           append([]string{}, command.PlanStepIDs...),
				ValidationID:          run.ValidationID,
				CommandExecutionID:    run.CommandExecutionID,
				RepairAttemptRevision: repairRevision,
				ValidationPassed: execution.State ==
					domain.ValidationStatePassed,
				ProfileDigest:      expectedDigest,
				Round:              round,
				CommandOrdinal:     uint64(commandIndex + 1),
				CommandID:          command.ID,
				CommandFingerprint: fingerprint,
				Required:           command.Required,
				AcceptanceTest:     command.AcceptanceTest,
				State:              execution.State,
				Failure:            execution.Failure,
				IdempotencyKey: fmt.Sprintf(
					"%s/validation/%d/%s",
					input.IdempotencyPrefix,
					round,
					command.ID,
				),
			},
		); err != nil {
			return report, err
		}
		report.Executions = append(report.Executions, execution)
		if runErr != nil {
			return report, runErr
		}
	}
	finalDigest, err := profile.Digest()
	if err != nil || finalDigest != expectedDigest {
		return report, errors.Join(
			ErrRequiredValidationIncomplete,
			err,
			errors.New("selected validation profile changed during execution"),
		)
	}
	return report, nil
}

func (service *RepairCompletionService) redactRepositorySummary(
	summary agentloop.RepositoryCompletionSummary,
) (agentloop.RepositoryCompletionSummary, error) {
	status, err := service.dependencies.Redactor.Redact(
		redact.BoundaryUIDelivery,
		summary.StatusRedacted,
	)
	if err != nil {
		return agentloop.RepositoryCompletionSummary{}, err
	}
	diff, err := service.dependencies.Redactor.Redact(
		redact.BoundaryUIDelivery,
		summary.DiffRedacted,
	)
	if err != nil {
		return agentloop.RepositoryCompletionSummary{}, err
	}
	summary.StatusRedacted = status.Text
	summary.DiffRedacted = diff.Text
	return summary, nil
}

func marshalCompletionEvidence(
	summary agentloop.CompletionSummary,
) (string, string, string, string, string, string, error) {
	repository, err := json.Marshal(struct {
		Status       string   `json:"status_redacted"`
		ChangedFiles []string `json:"changed_files"`
	}{
		Status:       summary.Repository.StatusRedacted,
		ChangedFiles: summary.Repository.ChangedFiles,
	})
	if err != nil {
		return "", "", "", "", "", "", err
	}
	diff, err := json.Marshal(struct {
		Summary string `json:"summary_redacted"`
		SHA256  string `json:"sha256"`
	}{
		Summary: summary.Repository.DiffRedacted,
		SHA256:  summary.Repository.DiffSHA256,
	})
	if err != nil {
		return "", "", "", "", "", "", err
	}
	validation, err := json.Marshal(summary.Validation)
	if err != nil {
		return "", "", "", "", "", "", err
	}
	budget, err := json.Marshal(summary.Budget)
	if err != nil {
		return "", "", "", "", "", "", err
	}
	assumptions, err := json.Marshal(summary.Assumptions)
	if err != nil {
		return "", "", "", "", "", "", err
	}
	limitations, err := json.Marshal(summary.Limitations)
	if err != nil {
		return "", "", "", "", "", "", err
	}
	return string(repository), string(diff), string(validation), string(budget),
		string(assumptions), string(limitations), nil
}

func cloneValidationProfile(
	profile agentloop.ValidationProfile,
) agentloop.ValidationProfile {
	clone := agentloop.ValidationProfile{
		Name: profile.Name, Version: profile.Version,
		Commands: make([]agentloop.ValidationCommand, len(profile.Commands)),
	}
	for index, command := range profile.Commands {
		clone.Commands[index] = cloneValidationCommand(command)
	}
	return clone
}

func cloneValidationCommand(
	command agentloop.ValidationCommand,
) agentloop.ValidationCommand {
	clone := command
	clone.Request.Arguments = append(
		[]executor.ToolArgument(nil),
		command.Request.Arguments...,
	)
	clone.Request.ExpectedSideEffects = append(
		[]executor.SideEffect(nil),
		command.Request.ExpectedSideEffects...,
	)
	clone.RelevantChangedFiles = append(
		[]string(nil),
		command.RelevantChangedFiles...,
	)
	clone.PlanStepIDs = append([]string(nil), command.PlanStepIDs...)
	return clone
}

func validationFailures(
	report agentloop.ValidationReport,
) []agentloop.ValidationFailure {
	var failures []agentloop.ValidationFailure
	for _, execution := range report.Executions {
		if execution.Failure != nil {
			failures = append(failures, *execution.Failure)
		}
	}
	return failures
}

func validateCompletionEvidence(
	taskID domain.TaskID,
	runID domain.RunID,
	planRevision uint64,
	report agentloop.ValidationReport,
	evidence completionEvidenceRecord,
) error {
	if err := report.Validate(); err != nil {
		return errors.Join(ErrRequiredValidationIncomplete, err)
	}
	if len(evidence.PlanSteps) == 0 {
		return errors.Join(
			ErrRequiredValidationIncomplete,
			errors.New("durable plan-step evidence is empty"),
		)
	}
	requiredPassed := make(map[domain.ValidationID]map[string]struct{})
	for _, execution := range report.Executions {
		if !execution.Required ||
			execution.State != domain.ValidationStatePassed {
			continue
		}
		steps := make(map[string]struct{}, len(execution.PlanStepIDs))
		for _, stepID := range execution.PlanStepIDs {
			steps[stepID] = struct{}{}
		}
		requiredPassed[execution.ValidationID] = steps
	}
	durableLinks := make(map[domain.ValidationID]map[string]struct{})
	for _, link := range evidence.ValidationAttributions {
		if link.TaskID != taskID || link.RunID != runID ||
			link.PlanRevision != planRevision ||
			link.ValidationID.IsZero() ||
			strings.TrimSpace(link.PlanStepID) != link.PlanStepID ||
			link.PlanStepID == "" {
			return errors.Join(
				ErrRequiredValidationIncomplete,
				errors.New("durable validation attribution is malformed"),
			)
		}
		steps := durableLinks[link.ValidationID]
		if steps == nil {
			steps = make(map[string]struct{})
			durableLinks[link.ValidationID] = steps
		}
		steps[link.PlanStepID] = struct{}{}
	}
	covered := make(map[string]struct{})
	for validationID, expectedSteps := range requiredPassed {
		actualSteps := durableLinks[validationID]
		for stepID := range expectedSteps {
			if _, exists := actualSteps[stepID]; !exists {
				return errors.Join(
					ErrRequiredValidationIncomplete,
					fmt.Errorf(
						"required validation lacks durable attribution to %q",
						stepID,
					),
				)
			}
			covered[stepID] = struct{}{}
		}
	}
	seenSteps := make(map[string]struct{}, len(evidence.PlanSteps))
	for _, step := range evidence.PlanSteps {
		if strings.TrimSpace(step.PlanStepID) != step.PlanStepID ||
			step.PlanStepID == "" {
			return errors.Join(
				ErrRequiredValidationIncomplete,
				errors.New("durable plan-step identity is malformed"),
			)
		}
		if _, duplicate := seenSteps[step.PlanStepID]; duplicate {
			return errors.Join(
				ErrRequiredValidationIncomplete,
				errors.New("durable plan-step evidence is duplicated"),
			)
		}
		seenSteps[step.PlanStepID] = struct{}{}
		if step.State != agentloop.StepValidated {
			return errors.Join(
				ErrRequiredValidationIncomplete,
				fmt.Errorf(
					"required plan step %q is not durably validated",
					step.PlanStepID,
				),
			)
		}
		if _, exists := covered[step.PlanStepID]; !exists {
			return errors.Join(
				ErrRequiredValidationIncomplete,
				fmt.Errorf(
					"required plan step %q lacks current validation evidence",
					step.PlanStepID,
				),
			)
		}
	}
	for _, expectedSteps := range requiredPassed {
		for stepID := range expectedSteps {
			if _, exists := seenSteps[stepID]; !exists {
				return errors.Join(
					ErrRequiredValidationIncomplete,
					fmt.Errorf(
						"required validation references unknown plan step %q",
						stepID,
					),
				)
			}
		}
	}
	return nil
}

func firstFailedRequiredValidation(
	report agentloop.ValidationReport,
) (domain.ValidationID, bool) {
	for _, execution := range report.Executions {
		if execution.Required &&
			execution.State != domain.ValidationStatePassed &&
			!execution.ValidationID.IsZero() {
			return execution.ValidationID, true
		}
	}
	return domain.ValidationID{}, false
}

func firstPassedRequiredValidation(
	report agentloop.ValidationReport,
) (domain.ValidationID, bool) {
	for _, execution := range report.Executions {
		if execution.Required &&
			execution.State == domain.ValidationStatePassed &&
			!execution.ValidationID.IsZero() {
			return execution.ValidationID, true
		}
	}
	return domain.ValidationID{}, false
}

func unresolvedValidationSummary(report agentloop.ValidationReport) string {
	var summaries []string
	for _, execution := range report.Executions {
		if execution.State == domain.ValidationStatePassed {
			continue
		}
		if execution.Failure != nil {
			summaries = append(
				summaries,
				execution.CommandID+": "+execution.Failure.SummaryRedacted,
			)
		} else {
			summaries = append(
				summaries,
				execution.CommandID+": "+string(execution.State),
			)
		}
	}
	if len(summaries) == 0 {
		return "validation has no unresolved failures"
	}
	return boundedCoordinatorSummary(strings.Join(summaries, "; "))
}

func mergeChangedFiles(left, right []string) []string {
	result := append([]string(nil), left...)
	seen := make(map[string]struct{}, len(result))
	for _, path := range result {
		seen[path] = struct{}{}
	}
	for _, path := range right {
		if _, found := seen[path]; found {
			continue
		}
		seen[path] = struct{}{}
		result = append(result, path)
	}
	return result
}

func boundedCoordinatorText(value string, maximum int) bool {
	return value != "" &&
		strings.TrimSpace(value) == value &&
		len(value) <= maximum
}

func boundedCoordinatorSummary(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 2048 {
		return value
	}
	end := 2037
	for end > 0 && !utf8.ValidString(value[:end]) {
		end--
	}
	return value[:end] + "[TRUNCATED]"
}

func agentPlanStepState(value storage.PlanStepState) (agentloop.StepState, error) {
	switch value {
	case storage.PlanStepPending:
		return agentloop.StepPending, nil
	case storage.PlanStepInProgress:
		return agentloop.StepInProgress, nil
	case storage.PlanStepImplemented:
		return agentloop.StepImplemented, nil
	case storage.PlanStepValidated:
		return agentloop.StepValidated, nil
	case storage.PlanStepFailed:
		return agentloop.StepFailed, nil
	case storage.PlanStepSkipped:
		return agentloop.StepSkipped, nil
	default:
		return "", errors.New("durable plan-step state is invalid")
	}
}

func validationExecutionPresentation(
	execution agentloop.ValidationExecution,
) (string, string, error) {
	encoded, err := json.Marshal(execution)
	if err != nil {
		return "", "", err
	}
	digest := sha256.Sum256(encoded)
	return string(encoded), hex.EncodeToString(digest[:]), nil
}
