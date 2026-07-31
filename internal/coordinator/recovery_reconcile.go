package coordinator

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"codeflux.dev/codeflux/internal/checkpoint"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/workspace"
)

// RecoveryReconcileRefusalReason is a closed, machine-testable explanation
// for refusing to adopt observed state. Refusal never mutates task/run state.
type RecoveryReconcileRefusalReason string

const (
	RecoveryReconcileNotRequired          RecoveryReconcileRefusalReason = "reconcile-not-required"
	RecoveryReconcileUnsafeFinding        RecoveryReconcileRefusalReason = "unsafe-recovery-finding"
	RecoveryReconcileAmbiguousEffects     RecoveryReconcileRefusalReason = "ambiguous-external-effects"
	RecoveryReconcileNonDescendantHead    RecoveryReconcileRefusalReason = "non-descendant-worktree-head"
	RecoveryReconcileCapturedStateChanged RecoveryReconcileRefusalReason = "captured-authoritative-state-changed"
)

// RecoveryReconcileRefusal is safe to surface to transport callers.
type RecoveryReconcileRefusal struct {
	Reason RecoveryReconcileRefusalReason
}

func (value *RecoveryReconcileRefusal) Error() string {
	if value == nil {
		return "recovery reconciliation was refused"
	}
	return "recovery reconciliation was refused: " + string(value.Reason)
}

type recoveryReconcileStore interface {
	recoveryDecisionStore
	ReadTaskControl(context.Context, domain.TaskID) (storage.TaskControlSnapshot, error)
	LoadCheckpoint(context.Context, domain.CheckpointID) (checkpoint.PersistedCheckpoint, error)
	GetWorktreeBinding(context.Context, domain.TaskID) (storage.WorktreeBinding, error)
	BeginRecoveryReconciliation(context.Context, storage.BeginRecoveryReconciliation) (storage.RecoveryReconciliationStart, error)
	CommitRecoveryReconciliation(context.Context, storage.CommitRecoveryReconciliation) (storage.TaskControlSnapshot, error)
}

type recoveryReconcileCheckpointer interface {
	CaptureReconciliationCheckpoint(context.Context, storage.TaskControlSnapshot, string) (domain.CheckpointID, error)
}

// RecoveryReconcileInput is one exact-revision user mutation.
type RecoveryReconcileInput struct {
	TaskID           domain.TaskID
	ExpectedRevision uint64
	ReasonRedacted   string
	IdempotencyKey   string
}

// RecoveryReconcileResult identifies the authoritative assessment and the
// newly captured boundary. Control is paused and safe for explicit resume.
type RecoveryReconcileResult struct {
	AssessmentID string
	CheckpointID domain.CheckpointID
	Control      storage.TaskControlSnapshot
}

// RecoveryReconciliationService re-verifies and safely rebaselines only a
// descendant user-worktree change. It never replays an external action.
type RecoveryReconciliationService struct {
	store        recoveryReconcileStore
	observations RecoveryObservationSource
	checkpoints  recoveryReconcileCheckpointer
	runner       workspace.CommandRunner
}

func NewRecoveryReconciliationService(
	store recoveryReconcileStore,
	observations RecoveryObservationSource,
	checkpoints recoveryReconcileCheckpointer,
	runner workspace.CommandRunner,
) (*RecoveryReconciliationService, error) {
	if store == nil || observations == nil || checkpoints == nil || runner == nil {
		return nil, errors.New("recovery reconciliation ports are required")
	}
	return &RecoveryReconciliationService{
		store: store, observations: observations, checkpoints: checkpoints, runner: runner,
	}, nil
}

func (service *RecoveryReconciliationService) Reconcile(
	ctx context.Context,
	input RecoveryReconcileInput,
) (RecoveryReconcileResult, error) {
	if service == nil {
		return RecoveryReconcileResult{}, errors.New("recovery reconciliation service is unavailable")
	}
	if input.TaskID.IsZero() || input.ExpectedRevision == 0 ||
		strings.TrimSpace(input.ReasonRedacted) != input.ReasonRedacted || input.ReasonRedacted == "" ||
		strings.TrimSpace(input.IdempotencyKey) != input.IdempotencyKey || input.IdempotencyKey == "" ||
		len(input.ReasonRedacted) > 4096 || len(input.IdempotencyKey) > maximumRecoveryDecisionIdempotencyBytes {
		return RecoveryReconcileResult{}, errors.New("recovery reconciliation input is invalid")
	}
	control, err := service.store.ReadTaskControl(ctx, input.TaskID)
	if err != nil {
		return RecoveryReconcileResult{}, err
	}
	if control.TaskRevision != input.ExpectedRevision {
		return RecoveryReconcileResult{}, storageStaleRevision("recovery reconciliation task revision changed")
	}
	assessment, err := service.store.GetCurrentRecoveryAssessment(ctx, input.TaskID, input.ExpectedRevision)
	if err != nil {
		return RecoveryReconcileResult{}, err
	}
	if assessment.Classification != storage.RecoveryClassificationReconcile || assessment.CheckpointID == nil {
		return RecoveryReconcileResult{}, refuseRecoveryReconcile(RecoveryReconcileNotRequired)
	}
	oldPersisted, err := service.store.LoadCheckpoint(ctx, *assessment.CheckpointID)
	if err != nil {
		return RecoveryReconcileResult{}, err
	}
	oldCandidate := recoveryCandidateFromCheckpoint(oldPersisted)
	oldSnapshot, err := decodeRecoveryCheckpoint(oldCandidate)
	if err != nil {
		return RecoveryReconcileResult{}, refuseRecoveryReconcile(RecoveryReconcileUnsafeFinding)
	}
	observation, err := service.observations.ObserveCheckpointRecovery(ctx, oldCandidate, oldSnapshot)
	if err != nil {
		return RecoveryReconcileResult{}, err
	}
	current, err := ClassifyCheckpointRecovery(RecoveryClassificationInput{
		CheckpointID:            oldPersisted.ID,
		CheckpointEventSequence: oldPersisted.CheckpointEventSequence,
		Checkpoint:              oldSnapshot,
		Observation:             observation,
	})
	if err != nil {
		return RecoveryReconcileResult{}, err
	}
	if err := validateUserWorktreeReconciliation(current); err != nil {
		return RecoveryReconcileResult{}, err
	}
	binding, err := service.store.GetWorktreeBinding(ctx, input.TaskID)
	if err != nil {
		return RecoveryReconcileResult{}, err
	}
	if err := verifyDescendantRecoveryHead(
		ctx, service.runner, binding.WorktreePath,
		oldSnapshot.WorktreeHead, observation.WorktreeHead,
	); err != nil {
		return RecoveryReconcileResult{}, err
	}

	identity := recoveryDecisionIdentity(input.IdempotencyKey)
	previous := oldPersisted.ID
	_, err = service.store.BeginRecoveryReconciliation(ctx, storage.BeginRecoveryReconciliation{
		ExpectedTaskRevision: input.ExpectedRevision,
		ExpectedRunRevision:  control.RunRevision,
		Decision: storage.RecordRecoveryDecision{
			ID: "recovery-decision-" + identity, AssessmentID: assessment.ID,
			TaskID: input.TaskID, RunID: control.RunID, CheckpointID: &previous,
			Actor: storage.RecoveryDecisionActorUser, Action: storage.RecoveryActionReconcile,
			ReasonRedacted: input.ReasonRedacted,
			IdempotencyKey: input.IdempotencyKey + "/decision",
		},
		Started: storage.RecordRecoveryAttempt{
			ID: "recovery-attempt-started-" + identity, AssessmentID: assessment.ID,
			TaskID: input.TaskID, RunID: control.RunID, CheckpointID: &previous,
			Action: storage.RecoveryActionReconcile, Outcome: storage.RecoveryAttemptStarted,
			ReasonRedacted: input.ReasonRedacted,
			IdempotencyKey: input.IdempotencyKey + "/started",
		},
	})
	if err != nil {
		return RecoveryReconcileResult{}, err
	}

	checkpointID, reconcileErr := service.checkpoints.CaptureReconciliationCheckpoint(
		ctx, control, input.IdempotencyKey+"/checkpoint",
	)
	if reconcileErr == nil {
		reconcileErr = service.verifyCapturedBoundary(ctx, oldSnapshot, checkpointID)
	}
	if reconcileErr != nil {
		service.recordFailedAttempt(ctx, assessment, input, identity, reconcileErr)
		return RecoveryReconcileResult{}, reconcileErr
	}
	eventID, err := domain.NewEventID()
	if err != nil {
		service.recordFailedAttempt(ctx, assessment, input, identity, err)
		return RecoveryReconcileResult{}, err
	}
	committed, err := service.store.CommitRecoveryReconciliation(
		ctx,
		storage.CommitRecoveryReconciliation{
			EventID: eventID, TaskID: input.TaskID, RunID: control.RunID,
			ExpectedTaskRevision:   input.ExpectedRevision,
			ExpectedRunRevision:    control.RunRevision,
			AssessmentID:           assessment.ID,
			PreviousCheckpointID:   previous,
			ReconciledCheckpointID: checkpointID,
			ReasonRedacted:         input.ReasonRedacted,
			IdempotencyKey:         input.IdempotencyKey + "/committed",
			TerminalAttemptID:      "recovery-attempt-terminal-" + identity,
			TerminalIdempotencyKey: input.IdempotencyKey + "/terminal",
		},
	)
	if err != nil {
		service.recordFailedAttempt(ctx, assessment, input, identity, err)
		return RecoveryReconcileResult{}, err
	}
	return RecoveryReconcileResult{
		AssessmentID: assessment.ID, CheckpointID: checkpointID, Control: committed,
	}, nil
}

func (service *RecoveryReconciliationService) verifyCapturedBoundary(
	ctx context.Context,
	old checkpoint.Snapshot,
	checkpointID domain.CheckpointID,
) error {
	persisted, err := service.store.LoadCheckpoint(ctx, checkpointID)
	if err != nil {
		return err
	}
	candidate := recoveryCandidateFromCheckpoint(persisted)
	captured, err := decodeRecoveryCheckpoint(candidate)
	if err != nil {
		return refuseRecoveryReconcile(RecoveryReconcileCapturedStateChanged)
	}
	if captured.TaskID != old.TaskID || captured.RunID != old.RunID ||
		captured.RepositoryID != old.RepositoryID ||
		captured.WorktreeBindingRevision != old.WorktreeBindingRevision ||
		captured.PlanRevision != old.PlanRevision || captured.BaseRevision != old.BaseRevision ||
		captured.Policy != old.Policy || captured.Provider != old.Provider || captured.Tools != old.Tools ||
		!slices.Equal(captured.CompletedPlanSteps, old.CompletedPlanSteps) ||
		!slices.Equal(captured.PendingPlanSteps, old.PendingPlanSteps) ||
		captured.ExternalOutcomeAmbiguous || len(captured.AmbiguousExternalActions) != 0 {
		return refuseRecoveryReconcile(RecoveryReconcileCapturedStateChanged)
	}
	conflicts, _ := compareCheckpointDirtyFiles(old.DirtyFiles, captured.DirtyFiles)
	if len(conflicts) != 0 {
		return refuseRecoveryReconcile(RecoveryReconcileUnsafeFinding)
	}
	if err := verifyDescendantRecoveryHead(
		ctx, service.runner, mustRecoveryWorktreePath(ctx, service.store, old.TaskID),
		old.WorktreeHead, captured.WorktreeHead,
	); err != nil {
		return err
	}
	observation, err := service.observations.ObserveCheckpointRecovery(ctx, candidate, captured)
	if err != nil {
		return err
	}
	assessment, err := ClassifyCheckpointRecovery(RecoveryClassificationInput{
		CheckpointID:            checkpointID,
		CheckpointEventSequence: persisted.CheckpointEventSequence,
		Checkpoint:              captured,
		Observation:             observation,
	})
	if err != nil {
		return err
	}
	if assessment.Classification != RecoverySafeResume ||
		len(assessment.ActionsThatMustNotBeRepeated) != 0 {
		return refuseRecoveryReconcile(RecoveryReconcileCapturedStateChanged)
	}
	return nil
}

func validateUserWorktreeReconciliation(assessment RecoveryAssessment) error {
	if assessment.Classification != RecoveryReconcileRequired {
		return refuseRecoveryReconcile(RecoveryReconcileNotRequired)
	}
	if len(assessment.ActionsThatMustNotBeRepeated) != 0 {
		return refuseRecoveryReconcile(RecoveryReconcileAmbiguousEffects)
	}
	headChanged := false
	for _, finding := range assessment.Findings {
		switch finding.Code {
		case RecoveryFindingWorktreeHeadChanged:
			headChanged = true
		case RecoveryFindingNonOverlappingUserEdit, RecoveryFindingEventsAfterCheckpoint:
		default:
			return refuseRecoveryReconcile(RecoveryReconcileUnsafeFinding)
		}
	}
	if !headChanged {
		return refuseRecoveryReconcile(RecoveryReconcileNotRequired)
	}
	return nil
}

func verifyDescendantRecoveryHead(
	ctx context.Context,
	runner workspace.CommandRunner,
	worktreePath, oldHead, newHead string,
) error {
	if worktreePath == "" || oldHead == "" || newHead == "" || oldHead == newHead {
		return refuseRecoveryReconcile(RecoveryReconcileNonDescendantHead)
	}
	_, err := runner.Run(ctx, worktreePath, "git", "merge-base", "--is-ancestor", oldHead, newHead)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return refuseRecoveryReconcile(RecoveryReconcileNonDescendantHead)
	}
	return nil
}

func recoveryCandidateFromCheckpoint(value checkpoint.PersistedCheckpoint) storage.RecoveryCheckpointCandidate {
	id := value.ID
	return storage.RecoveryCheckpointCandidate{
		CheckpointID: &id, TaskID: value.TaskID, RunID: value.RunID,
		SchemaVersion: value.SchemaVersion, StateJSON: value.StateJSON,
		StateSHA256:             value.StateSHA256,
		CheckpointEventSequence: value.CheckpointEventSequence,
	}
}

func mustRecoveryWorktreePath(
	ctx context.Context,
	store recoveryReconcileStore,
	taskID domain.TaskID,
) string {
	binding, err := store.GetWorktreeBinding(ctx, taskID)
	if err != nil {
		return ""
	}
	return binding.WorktreePath
}

func refuseRecoveryReconcile(reason RecoveryReconcileRefusalReason) error {
	return &RecoveryReconcileRefusal{Reason: reason}
}

func storageStaleRevision(detail string) error {
	return fmt.Errorf("%s: %w", detail, storage.ErrStaleRevision)
}

func (service *RecoveryReconciliationService) recordFailedAttempt(
	ctx context.Context,
	assessment storage.RecoveryAssessmentRecord,
	input RecoveryReconcileInput,
	identity string,
	cause error,
) {
	outcome := storage.RecoveryAttemptFailed
	reason := "recovery reconciliation failed"
	if errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) {
		outcome = storage.RecoveryAttemptCancelled
		reason = "recovery reconciliation was cancelled"
	}
	_, _ = service.store.RecordRecoveryAttempt(
		context.WithoutCancel(ctx),
		storage.RecordRecoveryAttempt{
			ID:           "recovery-attempt-terminal-" + identity,
			AssessmentID: assessment.ID, TaskID: assessment.TaskID,
			RunID: assessment.RunID, CheckpointID: assessment.CheckpointID,
			Action: storage.RecoveryActionReconcile, Outcome: outcome,
			ReasonRedacted: reason,
			IdempotencyKey: input.IdempotencyKey + "/terminal",
		},
	)
}
