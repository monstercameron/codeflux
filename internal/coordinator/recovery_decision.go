package coordinator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
)

const maximumRecoveryDecisionIdempotencyBytes = 220

type recoveryDecisionStore interface {
	GetRecoveryAssessment(
		context.Context,
		string,
	) (storage.RecoveryAssessmentRecord, error)
	GetCurrentRecoveryAssessment(
		context.Context,
		domain.TaskID,
		uint64,
	) (storage.RecoveryAssessmentRecord, error)
	RecordRecoveryDecision(
		context.Context,
		storage.RecordRecoveryDecision,
	) (storage.RecoveryDecisionRecord, error)
	RecordRecoveryAttempt(
		context.Context,
		storage.RecordRecoveryAttempt,
	) (storage.RecoveryAttemptRecord, error)
}

// RecoveryPatchExporter materializes an exact binary-safe patch from the
// preserved checkpoint ref. Implementations must be idempotent.
type RecoveryPatchExporter interface {
	PreserveCheckpointPatch(
		context.Context,
		domain.CheckpointID,
	) (path string, available bool, err error)
}

// PreserveRecoveryPatchInput is one explicit user recovery choice.
type PreserveRecoveryPatchInput struct {
	AssessmentID   string
	ReasonRedacted string
	IdempotencyKey string
}

// PreserveTaskRecoveryPatchInput resolves the current assessment through the
// exact task revision carried by the transport mutation control.
type PreserveTaskRecoveryPatchInput struct {
	TaskID           domain.TaskID
	ExpectedRevision uint64
	ReasonRedacted   string
	IdempotencyKey   string
}

// PreserveRecoveryPatchResult returns every immutable fact plus the exported
// path. No worker or task state is changed.
type PreserveRecoveryPatchResult struct {
	Assessment storage.RecoveryAssessmentRecord
	Decision   storage.RecoveryDecisionRecord
	Started    storage.RecoveryAttemptRecord
	Terminal   storage.RecoveryAttemptRecord
	PatchPath  string
}

// RecoveryDecisionService owns user-authorized recovery actions.
type RecoveryDecisionService struct {
	store      recoveryDecisionStore
	exporter   RecoveryPatchExporter
	reconciler *RecoveryReconciliationService
	safeResume *RecoverySafeResumeService
}

func (service *RecoveryDecisionService) ConfigureSafeResume(
	safeResume *RecoverySafeResumeService,
) error {
	if service == nil || safeResume == nil {
		return errors.New("safe recovery resume service is required")
	}
	service.safeResume = safeResume
	return nil
}

// ConfigureReconciliation attaches the separately constructed verifier and
// reconciler before the transport server starts accepting requests.
func (service *RecoveryDecisionService) ConfigureReconciliation(
	reconciler *RecoveryReconciliationService,
) error {
	if service == nil || reconciler == nil {
		return errors.New("recovery reconciliation service is required")
	}
	service.reconciler = reconciler
	return nil
}

func NewRecoveryDecisionService(
	store recoveryDecisionStore,
	exporter RecoveryPatchExporter,
) (*RecoveryDecisionService, error) {
	if store == nil || exporter == nil {
		return nil, errors.New(
			"recovery decision store and patch exporter are required",
		)
	}
	return &RecoveryDecisionService{store: store, exporter: exporter}, nil
}

// PreservePatch records authority before export and records the terminal
// result through a cancellation-independent context.
func (service *RecoveryDecisionService) PreservePatch(
	ctx context.Context,
	input PreserveRecoveryPatchInput,
) (PreserveRecoveryPatchResult, error) {
	if service == nil {
		return PreserveRecoveryPatchResult{}, errors.New(
			"recovery decision service is unavailable",
		)
	}
	if err := validatePreserveRecoveryPatchInput(input); err != nil {
		return PreserveRecoveryPatchResult{}, err
	}
	assessment, err := service.store.GetRecoveryAssessment(
		ctx,
		input.AssessmentID,
	)
	if err != nil {
		return PreserveRecoveryPatchResult{}, err
	}
	if !assessment.PatchAvailable || assessment.CheckpointID == nil {
		return PreserveRecoveryPatchResult{}, errors.New(
			"recovery assessment has no preserved checkpoint patch",
		)
	}
	result := PreserveRecoveryPatchResult{Assessment: assessment}
	identity := recoveryDecisionIdentity(input.IdempotencyKey)
	result.Decision, err = service.store.RecordRecoveryDecision(
		ctx,
		storage.RecordRecoveryDecision{
			ID:             "recovery-decision-" + identity,
			AssessmentID:   assessment.ID,
			TaskID:         assessment.TaskID,
			RunID:          assessment.RunID,
			CheckpointID:   assessment.CheckpointID,
			Actor:          storage.RecoveryDecisionActorUser,
			Action:         storage.RecoveryActionPreservePatch,
			ReasonRedacted: input.ReasonRedacted,
			IdempotencyKey: input.IdempotencyKey + "/decision",
		},
	)
	if err != nil {
		return result, err
	}
	result.Started, err = service.store.RecordRecoveryAttempt(
		ctx,
		storage.RecordRecoveryAttempt{
			ID:             "recovery-attempt-started-" + identity,
			AssessmentID:   assessment.ID,
			TaskID:         assessment.TaskID,
			RunID:          assessment.RunID,
			CheckpointID:   assessment.CheckpointID,
			Action:         storage.RecoveryActionPreservePatch,
			Outcome:        storage.RecoveryAttemptStarted,
			ReasonRedacted: input.ReasonRedacted,
			IdempotencyKey: input.IdempotencyKey + "/started",
		},
	)
	if err != nil {
		return result, err
	}

	path, available, exportErr := service.exporter.PreserveCheckpointPatch(
		ctx,
		*assessment.CheckpointID,
	)
	outcome := storage.RecoveryAttemptSucceeded
	terminalReason := input.ReasonRedacted
	switch {
	case errors.Is(exportErr, context.Canceled) ||
		errors.Is(exportErr, context.DeadlineExceeded):
		outcome = storage.RecoveryAttemptCancelled
		terminalReason = "checkpoint patch preservation was cancelled"
	case exportErr != nil:
		outcome = storage.RecoveryAttemptFailed
		terminalReason = "checkpoint patch preservation failed"
	case !available || strings.TrimSpace(path) != path || path == "":
		exportErr = errors.New("preserved checkpoint patch is unavailable")
		outcome = storage.RecoveryAttemptFailed
		terminalReason = "checkpoint patch preservation was unavailable"
	}
	result.PatchPath = path
	result.Terminal, err = service.store.RecordRecoveryAttempt(
		context.WithoutCancel(ctx),
		storage.RecordRecoveryAttempt{
			ID:             "recovery-attempt-terminal-" + identity,
			AssessmentID:   assessment.ID,
			TaskID:         assessment.TaskID,
			RunID:          assessment.RunID,
			CheckpointID:   assessment.CheckpointID,
			Action:         storage.RecoveryActionPreservePatch,
			Outcome:        outcome,
			ReasonRedacted: terminalReason,
			IdempotencyKey: input.IdempotencyKey + "/terminal",
		},
	)
	if err != nil {
		return result, errors.Join(exportErr, err)
	}
	return result, exportErr
}

// PreserveTaskPatch binds a user request to the newest assessment that is
// still recovery-required at the exact caller-observed task revision.
func (service *RecoveryDecisionService) PreserveTaskPatch(
	ctx context.Context,
	input PreserveTaskRecoveryPatchInput,
) (PreserveRecoveryPatchResult, error) {
	if service == nil {
		return PreserveRecoveryPatchResult{}, errors.New("recovery decision service is unavailable")
	}
	if input.TaskID.IsZero() || input.ExpectedRevision == 0 {
		return PreserveRecoveryPatchResult{}, errors.New("recovery task and expected revision are required")
	}
	assessment, err := service.store.GetCurrentRecoveryAssessment(
		ctx,
		input.TaskID,
		input.ExpectedRevision,
	)
	if err != nil {
		return PreserveRecoveryPatchResult{}, err
	}
	return service.PreservePatch(ctx, PreserveRecoveryPatchInput{
		AssessmentID:   assessment.ID,
		ReasonRedacted: input.ReasonRedacted,
		IdempotencyKey: input.IdempotencyKey,
	})
}

// PreserveTaskRecoveryPatch adapts the recovery decision service to the
// transport application port without exposing storage identities to gRPC.
func (service *RecoveryDecisionService) PreserveTaskRecoveryPatch(
	ctx context.Context,
	command transport.RecoveryPatchCommand,
) (transport.RecoveryPatchView, error) {
	result, err := service.PreserveTaskPatch(ctx, PreserveTaskRecoveryPatchInput{
		TaskID: command.TaskID, ExpectedRevision: command.ExpectedRevision,
		ReasonRedacted: command.ReasonRedacted, IdempotencyKey: command.IdempotencyKey,
	})
	if err != nil {
		return transport.RecoveryPatchView{}, taskControlPortError(err)
	}
	return transport.RecoveryPatchView{
		TaskID:       result.Assessment.TaskID,
		AssessmentID: result.Assessment.ID,
		PatchPath:    result.PatchPath,
	}, nil
}

// ReconcileTaskRecovery adapts the safe reconciliation use case to the
// product API without exposing storage records.
func (service *RecoveryDecisionService) ReconcileTaskRecovery(
	ctx context.Context,
	command transport.RecoveryReconcileCommand,
) (transport.RecoveryReconcileView, error) {
	if service == nil || service.reconciler == nil {
		return transport.RecoveryReconcileView{}, transport.ErrTaskControlConflict
	}
	result, err := service.reconciler.Reconcile(ctx, RecoveryReconcileInput{
		TaskID: command.TaskID, ExpectedRevision: command.ExpectedRevision,
		ReasonRedacted: command.ReasonRedacted, IdempotencyKey: command.IdempotencyKey,
	})
	if err != nil {
		var refusal *RecoveryReconcileRefusal
		if errors.As(err, &refusal) {
			return transport.RecoveryReconcileView{}, fmt.Errorf(
				"%s: %w", refusal.Reason, transport.ErrTaskControlConflict,
			)
		}
		return transport.RecoveryReconcileView{}, taskControlPortError(err)
	}
	return transport.RecoveryReconcileView{
		TaskID: result.Control.TaskID, AssessmentID: result.AssessmentID,
		CheckpointID: result.CheckpointID, State: result.Control.TaskState,
		Revision: result.Control.TaskRevision,
	}, nil
}

func validatePreserveRecoveryPatchInput(
	input PreserveRecoveryPatchInput,
) error {
	for _, value := range []string{
		input.AssessmentID,
		input.ReasonRedacted,
		input.IdempotencyKey,
	} {
		if strings.TrimSpace(value) != value || value == "" {
			return errors.New(
				"recovery assessment, reason, and idempotency identities are required",
			)
		}
	}
	if len(input.AssessmentID) > 255 ||
		len(input.ReasonRedacted) > 4096 ||
		len(input.IdempotencyKey) > maximumRecoveryDecisionIdempotencyBytes {
		return errors.New("recovery patch decision input exceeds its bound")
	}
	return nil
}

func recoveryDecisionIdentity(idempotencyKey string) string {
	digest := sha256.Sum256([]byte(idempotencyKey))
	return hex.EncodeToString(digest[:])
}
