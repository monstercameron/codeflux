package checkpoint

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

// Trigger identifies one required M15 checkpoint boundary.
type Trigger string

const (
	TriggerPlanApproved        Trigger = "plan-approved"
	TriggerMaterialEditApplied Trigger = "material-edit-applied"
	TriggerBeforeRiskyAction   Trigger = "before-risky-action"
	TriggerValidationSucceeded Trigger = "validation-succeeded"
	TriggerUserPaused          Trigger = "user-paused"
	TriggerGracefulShutdown    Trigger = "graceful-shutdown"
)

// TriggerAttribution binds the boundary to the exact durable cause.
type TriggerAttribution struct {
	ApprovalID           *domain.ApprovalID   `json:"approval_id,omitempty"`
	ToolRequestID        string               `json:"tool_request_id,omitempty"`
	PermissionDecisionID string               `json:"permission_decision_id,omitempty"`
	ActionSHA256         string               `json:"action_sha256,omitempty"`
	ValidationID         *domain.ValidationID `json:"validation_id,omitempty"`
}

// CaptureCommand declares one idempotent checkpoint attempt. It contains only
// non-secret immutable bindings; workspace and ledger state are read from
// authoritative ports.
type CaptureCommand struct {
	CheckpointID         domain.CheckpointID
	TaskID               domain.TaskID
	RunID                domain.RunID
	ExpectedPlanRevision uint64
	Trigger              Trigger
	Attribution          TriggerAttribution
	IdempotencyKey       string
}

// WorktreeState is one stable Git/worktree observation.
type WorktreeState struct {
	RepositoryID            domain.RepositoryID
	WorktreeBindingRevision uint64
	BaseRevision            string
	HeadRevision            string
	PreservedRevision       string
	PreservedRef            string
	DirtyFiles              []DirtyFileHash
	DiffSHA256              string
}

// RecoveryWorktreeFacts is a divergence-tolerant read of the current task
// worktree. Unlike checkpoint capture, incomplete or mismatched facts are
// returned for recovery classification rather than rejected as a safe state.
type RecoveryWorktreeFacts struct {
	Exists                  bool
	Owned                   bool
	RepositoryID            domain.RepositoryID
	BindingRevision         uint64
	HeadRevision            string
	DirtyFiles              []DirtyFileHash
	DiffSHA256              string
	UnresolvedGitOperations []string
}

// RuntimeState is one storage-backed execution observation.
type RuntimeState struct {
	PlanRevision             uint64
	PolicyRevision           uint64
	PlanSteps                []PlanStepSnapshot
	Budget                   BudgetPosition
	Policy                   PolicyBinding
	Provider                 ProviderBinding
	Tools                    ToolBinding
	AmbiguousExternalActions []AmbiguousExternalAction
	LastDurableEventSequence uint64
}

// WorktreeStateSource captures a self-consistent worktree state without
// returning file contents or a live Git handle.
type WorktreeStateSource interface {
	CaptureCheckpointWorktreeState(
		context.Context,
		domain.TaskID,
		domain.CheckpointID,
	) (WorktreeState, error)
	RemoveCheckpointWorktreePreservation(
		context.Context,
		domain.TaskID,
		domain.CheckpointID,
		string,
		string,
	) error
}

// RuntimeStateSource reads current immutable/revisioned execution facts.
type RuntimeStateSource interface {
	ReadCheckpointRuntimeState(
		context.Context,
		domain.TaskID,
		domain.RunID,
	) (RuntimeState, error)
}

// PersistedCheckpoint is the durable result of the atomic checkpoint/event
// transaction.
type PersistedCheckpoint struct {
	ID                      domain.CheckpointID
	TaskID                  domain.TaskID
	RunID                   domain.RunID
	SchemaVersion           uint64
	StateJSON               string
	StateSHA256             string
	CaptureRequestSHA256    string
	CheckpointEventSequence uint64
	PreservedRevision       string
	PreservedRef            string
	IdempotencyKey          string
}

// AtomicCommit declares the exact transaction preconditions and payload.
type AtomicCommit struct {
	CheckpointID             domain.CheckpointID
	TaskID                   domain.TaskID
	RunID                    domain.RunID
	Trigger                  Trigger
	Attribution              TriggerAttribution
	SchemaVersion            uint64
	StateJSON                string
	StateSHA256              string
	CaptureRequestSHA256     string
	PreservedRevision        string
	PreservedRef             string
	ExpectedEventSequence    uint64
	ExpectedBudgetRevision   uint64
	ExpectedWorktreeRevision uint64
	EventPayloadJSON         string
	IdempotencyKey           string
}

// AtomicStore owns deduplication and the single checkpoint-plus-event
// transaction. CommitCheckpointAndEvent must either commit both facts or
// neither. It must deduplicate same-task/run StateSHA256 values and must check
// the expected event, budget, and worktree revisions before creating a row.
type AtomicStore interface {
	FindCheckpointByIdempotency(
		context.Context,
		domain.TaskID,
		string,
	) (PersistedCheckpoint, bool, error)
	CommitCheckpointAndEvent(
		context.Context,
		AtomicCommit,
	) (PersistedCheckpoint, bool, error)
}

// SecretGuard rejects canonical state containing credential material.
type SecretGuard interface {
	EnsureCheckpointSecretFree(string) error
}

// CaptureResult distinguishes a new atomic commit from an idempotent or
// state-deduplicated replay.
type CaptureResult struct {
	Checkpoint PersistedCheckpoint
	State      CanonicalState
	Created    bool
	Replayed   bool
}

// Service captures versioned checkpoint state through bounded ports.
type Service struct {
	worktrees WorktreeStateSource
	runtime   RuntimeStateSource
	store     AtomicStore
	secrets   SecretGuard
}

// NewService validates checkpoint capture dependencies.
func NewService(
	worktrees WorktreeStateSource,
	runtime RuntimeStateSource,
	store AtomicStore,
	secrets SecretGuard,
) (*Service, error) {
	if worktrees == nil || runtime == nil || store == nil || secrets == nil {
		return nil, errors.New(
			"checkpoint worktree, runtime, atomic store, and secret guard ports are required",
		)
	}
	return &Service{
		worktrees: worktrees,
		runtime:   runtime,
		store:     store,
		secrets:   secrets,
	}, nil
}

// Capture reads bounded state and commits the checkpoint and its event through
// one atomic persistence operation.
func (service *Service) Capture(
	ctx context.Context,
	command CaptureCommand,
) (CaptureResult, error) {
	if service == nil {
		return CaptureResult{}, errors.New("checkpoint capture service is unavailable")
	}
	requestSHA, err := validateCaptureCommand(command)
	if err != nil {
		return CaptureResult{}, err
	}
	existing, found, err := service.store.FindCheckpointByIdempotency(
		ctx,
		command.TaskID,
		command.IdempotencyKey,
	)
	if err != nil {
		return CaptureResult{}, err
	}
	if found {
		return replayedCapture(
			existing,
			command,
			requestSHA,
			service.secrets,
		)
	}
	worktree, err := service.worktrees.CaptureCheckpointWorktreeState(
		ctx,
		command.TaskID,
		command.CheckpointID,
	)
	if err != nil {
		return CaptureResult{}, fmt.Errorf("capture checkpoint worktree: %w", err)
	}
	preservationRetained := false
	defer func() {
		if !preservationRetained {
			cleanupContext, cancel := context.WithTimeout(
				context.Background(),
				10*time.Second,
			)
			defer cancel()
			_ = service.worktrees.RemoveCheckpointWorktreePreservation(
				cleanupContext,
				command.TaskID,
				command.CheckpointID,
				worktree.PreservedRef,
				worktree.PreservedRevision,
			)
		}
	}()
	runtimeState, err := service.runtime.ReadCheckpointRuntimeState(
		ctx,
		command.TaskID,
		command.RunID,
	)
	if err != nil {
		return CaptureResult{}, fmt.Errorf("capture checkpoint runtime: %w", err)
	}
	if runtimeState.PlanRevision != command.ExpectedPlanRevision {
		return CaptureResult{}, errors.New(
			"checkpoint runtime plan revision changed before capture",
		)
	}
	if runtimeState.PolicyRevision != runtimeState.Policy.Revision {
		return CaptureResult{}, errors.New(
			"checkpoint runtime policy revision changed before capture",
		)
	}
	completed, pending, err := splitPlanSteps(runtimeState.PlanSteps)
	if err != nil {
		return CaptureResult{}, err
	}
	ambiguous := append(
		[]AmbiguousExternalAction(nil),
		runtimeState.AmbiguousExternalActions...,
	)
	state, err := Canonicalize(Snapshot{
		SchemaVersion:            SchemaVersion,
		TaskID:                   command.TaskID,
		RunID:                    command.RunID,
		RepositoryID:             worktree.RepositoryID,
		WorktreeBindingRevision:  worktree.WorktreeBindingRevision,
		PlanRevision:             runtimeState.PlanRevision,
		BaseRevision:             worktree.BaseRevision,
		WorktreeHead:             worktree.HeadRevision,
		PreservedRevision:        worktree.PreservedRevision,
		DirtyFiles:               worktree.DirtyFiles,
		DiffSHA256:               worktree.DiffSHA256,
		CompletedPlanSteps:       completed,
		PendingPlanSteps:         pending,
		Budget:                   runtimeState.Budget,
		Policy:                   runtimeState.Policy,
		Provider:                 runtimeState.Provider,
		Tools:                    runtimeState.Tools,
		LastDurableEventSequence: runtimeState.LastDurableEventSequence,
		ExternalOutcomeAmbiguous: len(ambiguous) != 0,
		AmbiguousExternalActions: ambiguous,
	})
	if err != nil {
		return CaptureResult{}, err
	}
	if err := service.secrets.EnsureCheckpointSecretFree(state.JSON); err != nil {
		return CaptureResult{}, err
	}
	eventPayload, err := checkpointEventPayload(
		command,
		state.StateSHA256,
		runtimeState.LastDurableEventSequence,
	)
	if err != nil {
		return CaptureResult{}, err
	}
	persisted, created, err := service.store.CommitCheckpointAndEvent(
		ctx,
		AtomicCommit{
			CheckpointID:             command.CheckpointID,
			TaskID:                   command.TaskID,
			RunID:                    command.RunID,
			Trigger:                  command.Trigger,
			Attribution:              command.Attribution,
			SchemaVersion:            SchemaVersion,
			StateJSON:                state.JSON,
			StateSHA256:              state.StateSHA256,
			CaptureRequestSHA256:     requestSHA,
			PreservedRevision:        worktree.PreservedRevision,
			PreservedRef:             worktree.PreservedRef,
			ExpectedEventSequence:    runtimeState.LastDurableEventSequence,
			ExpectedBudgetRevision:   runtimeState.Budget.SnapshotRevision,
			ExpectedWorktreeRevision: worktree.WorktreeBindingRevision,
			EventPayloadJSON:         eventPayload,
			IdempotencyKey:           command.IdempotencyKey,
		},
	)
	if err != nil {
		replayed, found, findErr := service.store.FindCheckpointByIdempotency(
			ctx,
			command.TaskID,
			command.IdempotencyKey,
		)
		if findErr != nil {
			// The database outcome cannot be reconciled, so retain the private
			// ref. An orphan is safe; deleting a possibly committed
			// checkpoint's only patch preservation is not.
			preservationRetained = true
			return CaptureResult{}, errors.Join(err, findErr)
		}
		if !found {
			return CaptureResult{}, err
		}
		persisted = replayed
		created = false
	}
	if err := validatePersistedCheckpoint(
		persisted,
		command,
		requestSHA,
		state.StateSHA256,
		created,
	); err != nil {
		return CaptureResult{}, err
	}
	retainsCapturedPreservation := created ||
		persisted.ID == command.CheckpointID &&
			persisted.PreservedRef == worktree.PreservedRef &&
			persisted.PreservedRevision == worktree.PreservedRevision
	if retainsCapturedPreservation {
		if persisted.PreservedRef != worktree.PreservedRef ||
			persisted.PreservedRevision != worktree.PreservedRevision {
			return CaptureResult{}, errors.New(
				"atomic checkpoint store changed Git preservation identity",
			)
		}
		preservationRetained = true
	} else {
		if err := service.worktrees.RemoveCheckpointWorktreePreservation(
			ctx,
			command.TaskID,
			command.CheckpointID,
			worktree.PreservedRef,
			worktree.PreservedRevision,
		); err != nil {
			return CaptureResult{}, fmt.Errorf(
				"remove deduplicated checkpoint preservation: %w",
				err,
			)
		}
		preservationRetained = true
	}
	return CaptureResult{
		Checkpoint: persisted,
		State:      state,
		Created:    created,
		Replayed:   !created,
	}, nil
}

// CaptureGracefulShutdown attempts one shutdown checkpoint within the caller's
// explicit bound.
func (service *Service) CaptureGracefulShutdown(
	ctx context.Context,
	command CaptureCommand,
	timeout time.Duration,
) (CaptureResult, error) {
	if timeout <= 0 || timeout > time.Minute {
		return CaptureResult{}, errors.New(
			"graceful checkpoint timeout must be within one minute",
		)
	}
	command.Trigger = TriggerGracefulShutdown
	command.Attribution = TriggerAttribution{}
	bounded, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return service.Capture(bounded, command)
}

func replayedCapture(
	existing PersistedCheckpoint,
	command CaptureCommand,
	requestSHA string,
	secrets SecretGuard,
) (CaptureResult, error) {
	if err := validatePersistedCheckpoint(
		existing,
		command,
		requestSHA,
		existing.StateSHA256,
		false,
	); err != nil {
		return CaptureResult{}, err
	}
	var snapshot Snapshot
	if err := json.Unmarshal([]byte(existing.StateJSON), &snapshot); err != nil {
		return CaptureResult{}, errors.New(
			"persisted checkpoint state is not valid JSON",
		)
	}
	state, err := Canonicalize(snapshot)
	if err != nil || state.JSON != existing.StateJSON ||
		state.StateSHA256 != existing.StateSHA256 {
		return CaptureResult{}, errors.New(
			"persisted checkpoint state is not canonical",
		)
	}
	if err := secrets.EnsureCheckpointSecretFree(state.JSON); err != nil {
		return CaptureResult{}, err
	}
	return CaptureResult{
		Checkpoint: existing,
		State:      state,
		Created:    false,
		Replayed:   true,
	}, nil
}

func validateCaptureCommand(command CaptureCommand) (string, error) {
	switch {
	case command.CheckpointID.IsZero() ||
		command.TaskID.IsZero() ||
		command.RunID.IsZero():
		return "", errors.New("checkpoint capture identities are required")
	case command.ExpectedPlanRevision == 0:
		return "", errors.New("checkpoint capture plan revision is required")
	case !boundedIdentifier(command.IdempotencyKey, 255):
		return "", errors.New("checkpoint capture idempotency key is invalid")
	}
	if err := validateTrigger(command.Trigger, command.Attribution); err != nil {
		return "", err
	}
	type requestIdentity struct {
		CheckpointID         domain.CheckpointID `json:"checkpoint_id"`
		TaskID               domain.TaskID       `json:"task_id"`
		RunID                domain.RunID        `json:"run_id"`
		ExpectedPlanRevision uint64              `json:"expected_plan_revision"`
		Trigger              Trigger             `json:"trigger"`
		Attribution          TriggerAttribution  `json:"attribution"`
		IdempotencyKey       string              `json:"idempotency_key"`
	}
	encoded, err := json.Marshal(requestIdentity(command))
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func validateTrigger(trigger Trigger, value TriggerAttribution) error {
	hasApproval := value.ApprovalID != nil && !value.ApprovalID.IsZero()
	hasTool := value.ToolRequestID != ""
	hasPermission := value.PermissionDecisionID != ""
	hasAction := value.ActionSHA256 != ""
	hasValidation := value.ValidationID != nil && !value.ValidationID.IsZero()
	switch trigger {
	case TriggerPlanApproved:
		if !hasApproval || hasTool || hasPermission || hasAction || hasValidation {
			return errors.New("plan-approved checkpoint attribution is invalid")
		}
	case TriggerMaterialEditApplied:
		if hasApproval || !hasTool || hasPermission || hasAction || hasValidation ||
			!boundedIdentifier(value.ToolRequestID, 255) {
			return errors.New("material-edit checkpoint attribution is invalid")
		}
	case TriggerBeforeRiskyAction:
		if hasApproval || hasTool || !hasPermission || !hasAction ||
			hasValidation ||
			!boundedIdentifier(value.PermissionDecisionID, 255) ||
			!validSHA256(value.ActionSHA256) {
			return errors.New("risky-action checkpoint attribution is invalid")
		}
	case TriggerValidationSucceeded:
		if hasApproval || hasTool || hasPermission || hasAction || !hasValidation {
			return errors.New("validation checkpoint attribution is invalid")
		}
	case TriggerUserPaused, TriggerGracefulShutdown:
		if hasApproval || hasTool || hasPermission || hasAction || hasValidation {
			return errors.New("pause or shutdown checkpoint attribution is invalid")
		}
	default:
		return errors.New("checkpoint trigger is invalid")
	}
	return nil
}

func splitPlanSteps(
	values []PlanStepSnapshot,
) ([]PlanStepSnapshot, []PlanStepSnapshot, error) {
	completed := make([]PlanStepSnapshot, 0, len(values))
	pending := make([]PlanStepSnapshot, 0, len(values))
	for _, value := range values {
		switch value.State {
		case PlanStepImplemented, PlanStepValidated, PlanStepSkipped:
			completed = append(completed, value)
		case PlanStepPending, PlanStepInProgress, PlanStepFailed:
			pending = append(pending, value)
		default:
			return nil, nil, errors.New(
				"checkpoint runtime returned an invalid plan-step state",
			)
		}
	}
	return completed, pending, nil
}

func checkpointEventPayload(
	command CaptureCommand,
	stateSHA string,
	lastSequence uint64,
) (string, error) {
	payload := struct {
		CheckpointID             domain.CheckpointID `json:"checkpoint_id"`
		RunID                    domain.RunID        `json:"run_id"`
		SchemaVersion            uint64              `json:"schema_version"`
		StateSHA256              string              `json:"state_sha256"`
		Trigger                  Trigger             `json:"trigger"`
		Attribution              TriggerAttribution  `json:"attribution"`
		LastDurableEventSequence uint64              `json:"last_durable_event_sequence"`
	}{
		CheckpointID:             command.CheckpointID,
		RunID:                    command.RunID,
		SchemaVersion:            SchemaVersion,
		StateSHA256:              stateSHA,
		Trigger:                  command.Trigger,
		Attribution:              command.Attribution,
		LastDurableEventSequence: lastSequence,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func validatePersistedCheckpoint(
	value PersistedCheckpoint,
	command CaptureCommand,
	requestSHA string,
	stateSHA string,
	requireCommandID bool,
) error {
	if value.ID.IsZero() ||
		requireCommandID && value.ID != command.CheckpointID ||
		value.TaskID != command.TaskID ||
		value.RunID != command.RunID ||
		value.SchemaVersion != SchemaVersion ||
		value.StateSHA256 != stateSHA ||
		value.CaptureRequestSHA256 != requestSHA ||
		value.CheckpointEventSequence == 0 ||
		!validGitObjectID(value.PreservedRevision) ||
		!validCheckpointReference(value.PreservedRef, value.ID) ||
		value.IdempotencyKey != command.IdempotencyKey ||
		!json.Valid([]byte(value.StateJSON)) {
		return errors.New(
			"atomic checkpoint store returned an inconsistent record",
		)
	}
	return nil
}

func validCheckpointReference(
	value string,
	id domain.CheckpointID,
) bool {
	return value == "refs/codeflux/checkpoints/"+id.String()
}
