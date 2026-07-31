package coordinator

import (
	"context"
	"errors"
	"strings"

	"codeflux.dev/codeflux/internal/checkpoint"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
)

type recoverySafeResumeStore interface {
	recoveryDecisionStore
	ReadTaskControl(context.Context, domain.TaskID) (storage.TaskControlSnapshot, error)
	ReadTaskControlReplay(context.Context, storage.TaskControlReplayRequest) (storage.TaskControlReplay, error)
	LoadCheckpoint(context.Context, domain.CheckpointID) (checkpoint.PersistedCheckpoint, error)
	AuthorizeSafeResumeRecovery(context.Context, storage.AuthorizeSafeResumeRecovery) (storage.TaskControlSnapshot, error)
}

type recoverySafeResumeExecutor interface {
	ResumeVerifiedRecovery(context.Context, storage.TaskControlSnapshot, string, string) (storage.TaskControlSnapshot, error)
}

// SafeResumeRecoveryInput is a separate user recovery choice, not the normal
// paused-task ResumeTask command.
type SafeResumeRecoveryInput struct {
	TaskID           domain.TaskID
	ExpectedRevision uint64
	ReasonRedacted   string
	IdempotencyKey   string
}

type SafeResumeRecoveryResult struct {
	AssessmentID string
	CheckpointID domain.CheckpointID
	Control      storage.TaskControlSnapshot
}

// RecoverySafeResumeService re-observes a safe checkpoint, commits explicit
// recovery authority, then enters the coordinator's verified resume path.
type RecoverySafeResumeService struct {
	store        recoverySafeResumeStore
	observations RecoveryObservationSource
	executor     recoverySafeResumeExecutor
}

func NewRecoverySafeResumeService(
	store recoverySafeResumeStore,
	observations RecoveryObservationSource,
	executor recoverySafeResumeExecutor,
) (*RecoverySafeResumeService, error) {
	if store == nil || observations == nil || executor == nil {
		return nil, errors.New("safe recovery resume ports are required")
	}
	return &RecoverySafeResumeService{store: store, observations: observations, executor: executor}, nil
}

func (service *RecoverySafeResumeService) SafeResume(
	ctx context.Context,
	input SafeResumeRecoveryInput,
) (SafeResumeRecoveryResult, error) {
	if service == nil {
		return SafeResumeRecoveryResult{}, errors.New("safe recovery resume service is unavailable")
	}
	if input.TaskID.IsZero() || input.ExpectedRevision == 0 ||
		strings.TrimSpace(input.ReasonRedacted) != input.ReasonRedacted || input.ReasonRedacted == "" ||
		strings.TrimSpace(input.IdempotencyKey) != input.IdempotencyKey || input.IdempotencyKey == "" ||
		len(input.ReasonRedacted) > 2048 || len(input.IdempotencyKey) > 150 {
		return SafeResumeRecoveryResult{}, errors.New("safe recovery resume input is invalid")
	}
	replay, err := service.store.ReadTaskControlReplay(ctx, storage.TaskControlReplayRequest{
		TaskID: input.TaskID, Operation: storage.TaskControlReplaySafeResumeRecovery,
		ExpectedTaskRevision: input.ExpectedRevision,
		ReasonRedacted:       input.ReasonRedacted, IdempotencyKey: input.IdempotencyKey,
	})
	if err != nil {
		return SafeResumeRecoveryResult{}, err
	}
	if replay.Found {
		if replay.CheckpointID == nil || replay.AssessmentID == "" {
			return SafeResumeRecoveryResult{}, errors.New("safe recovery resume replay is incomplete")
		}
		return SafeResumeRecoveryResult{
			AssessmentID: replay.AssessmentID, CheckpointID: *replay.CheckpointID,
			Control: replay.Control,
		}, nil
	}
	control, err := service.store.ReadTaskControl(ctx, input.TaskID)
	if err != nil {
		return SafeResumeRecoveryResult{}, err
	}
	if control.TaskRevision != input.ExpectedRevision ||
		control.TaskState != domain.TaskStateRecoveryRequired ||
		control.RunState != domain.RunStateRecoveryRequired {
		return SafeResumeRecoveryResult{}, storageStaleRevision("safe recovery resume state changed")
	}
	assessment, err := service.store.GetCurrentRecoveryAssessment(ctx, input.TaskID, input.ExpectedRevision)
	if err != nil {
		return SafeResumeRecoveryResult{}, err
	}
	if assessment.Classification != storage.RecoveryClassificationSafeResume || assessment.CheckpointID == nil {
		return SafeResumeRecoveryResult{}, refuseRecoveryReconcile(RecoveryReconcileNotRequired)
	}
	persisted, err := service.store.LoadCheckpoint(ctx, *assessment.CheckpointID)
	if err != nil {
		return SafeResumeRecoveryResult{}, err
	}
	candidate := recoveryCandidateFromCheckpoint(persisted)
	snapshot, err := decodeRecoveryCheckpoint(candidate)
	if err != nil {
		return SafeResumeRecoveryResult{}, refuseRecoveryReconcile(RecoveryReconcileUnsafeFinding)
	}
	observation, err := service.observations.ObserveCheckpointRecovery(ctx, candidate, snapshot)
	if err != nil {
		return SafeResumeRecoveryResult{}, err
	}
	verified, err := ClassifyCheckpointRecovery(RecoveryClassificationInput{
		CheckpointID:            persisted.ID,
		CheckpointEventSequence: persisted.CheckpointEventSequence,
		Checkpoint:              snapshot,
		Observation:             observation,
	})
	if err != nil {
		return SafeResumeRecoveryResult{}, err
	}
	if err := validateStrictSafeResume(verified); err != nil {
		return SafeResumeRecoveryResult{}, err
	}

	identity := recoveryDecisionIdentity(input.IdempotencyKey)
	checkpointID := persisted.ID
	eventID, err := domain.NewEventID()
	if err != nil {
		return SafeResumeRecoveryResult{}, err
	}
	paused, err := service.store.AuthorizeSafeResumeRecovery(ctx, storage.AuthorizeSafeResumeRecovery{
		EventID: eventID, TaskID: input.TaskID, RunID: control.RunID,
		ExpectedTaskRevision: input.ExpectedRevision,
		ExpectedRunRevision:  control.RunRevision,
		AssessmentID:         assessment.ID, CheckpointID: checkpointID,
		ReasonRedacted: input.ReasonRedacted, IdempotencyKey: input.IdempotencyKey,
		Decision: storage.RecordRecoveryDecision{
			ID: "recovery-decision-" + identity, AssessmentID: assessment.ID,
			TaskID: input.TaskID, RunID: control.RunID, CheckpointID: &checkpointID,
			Actor: storage.RecoveryDecisionActorUser, Action: storage.RecoveryActionResume,
			ReasonRedacted: input.ReasonRedacted,
			IdempotencyKey: input.IdempotencyKey + "/decision",
		},
		Started: storage.RecordRecoveryAttempt{
			ID: "recovery-attempt-started-" + identity, AssessmentID: assessment.ID,
			TaskID: input.TaskID, RunID: control.RunID, CheckpointID: &checkpointID,
			Action: storage.RecoveryActionResume, Outcome: storage.RecoveryAttemptStarted,
			ReasonRedacted: input.ReasonRedacted,
			IdempotencyKey: input.IdempotencyKey + "/started",
		},
	})
	if err != nil {
		return SafeResumeRecoveryResult{}, err
	}
	resumed, resumeErr := service.executor.ResumeVerifiedRecovery(
		ctx, paused, input.ReasonRedacted, input.IdempotencyKey+"/execution",
	)
	outcome := storage.RecoveryAttemptSucceeded
	terminalReason := input.ReasonRedacted
	if resumeErr != nil {
		outcome = storage.RecoveryAttemptFailed
		terminalReason = "safe recovery resume failed after authority was committed"
		if errors.Is(resumeErr, context.Canceled) || errors.Is(resumeErr, context.DeadlineExceeded) {
			outcome = storage.RecoveryAttemptCancelled
			terminalReason = "safe recovery resume was cancelled"
		}
	}
	_, terminalErr := service.store.RecordRecoveryAttempt(
		context.WithoutCancel(ctx),
		storage.RecordRecoveryAttempt{
			ID:           "recovery-attempt-terminal-" + identity,
			AssessmentID: assessment.ID, TaskID: assessment.TaskID,
			RunID: assessment.RunID, CheckpointID: assessment.CheckpointID,
			Action: storage.RecoveryActionResume, Outcome: outcome,
			ReasonRedacted: terminalReason,
			IdempotencyKey: input.IdempotencyKey + "/terminal",
		},
	)
	if resumeErr != nil || terminalErr != nil {
		return SafeResumeRecoveryResult{AssessmentID: assessment.ID, CheckpointID: checkpointID, Control: resumed}, errors.Join(resumeErr, terminalErr)
	}
	return SafeResumeRecoveryResult{AssessmentID: assessment.ID, CheckpointID: checkpointID, Control: resumed}, nil
}

func validateStrictSafeResume(assessment RecoveryAssessment) error {
	if hasRecoveryFinding(assessment.Findings, RecoveryFindingAmbiguousExternalAction) {
		return refuseRecoveryReconcile(RecoveryReconcileAmbiguousEffects)
	}
	if assessment.Classification != RecoverySafeResume {
		return refuseRecoveryReconcile(RecoveryReconcileNotRequired)
	}
	for _, finding := range assessment.Findings {
		if finding.Code != RecoveryFindingEventsAfterCheckpoint {
			return refuseRecoveryReconcile(RecoveryReconcileUnsafeFinding)
		}
	}
	return nil
}

// SafeResumeTaskRecovery adapts the distinct recovery command to transport.
func (service *RecoveryDecisionService) SafeResumeTaskRecovery(
	ctx context.Context,
	command transport.RecoverySafeResumeCommand,
) (transport.RecoverySafeResumeView, error) {
	if service == nil || service.safeResume == nil {
		return transport.RecoverySafeResumeView{}, transport.ErrTaskControlConflict
	}
	result, err := service.safeResume.SafeResume(ctx, SafeResumeRecoveryInput{
		TaskID: command.TaskID, ExpectedRevision: command.ExpectedRevision,
		ReasonRedacted: command.ReasonRedacted, IdempotencyKey: command.IdempotencyKey,
	})
	if err != nil {
		return transport.RecoverySafeResumeView{}, taskControlPortError(err)
	}
	return transport.RecoverySafeResumeView{
		TaskID: result.Control.TaskID, AssessmentID: result.AssessmentID,
		CheckpointID: result.CheckpointID, State: result.Control.TaskState,
		Revision: result.Control.TaskRevision,
	}, nil
}
