package coordinator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"codeflux.dev/codeflux/internal/checkpoint"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
)

const maximumRecoveryCandidates = 1000

// RecoveryCheckpointSource lists latest durable checkpoints for incomplete
// runs and persists their assessments.
type RecoveryCheckpointSource interface {
	ListRecoveryCheckpointCandidates(
		context.Context,
		int,
	) ([]storage.RecoveryCheckpointCandidate, error)
	RecordRecoveryAssessment(
		context.Context,
		storage.RecordRecoveryAssessment,
	) (storage.RecoveryAssessmentRecord, error)
}

// RecoveryObservationSource reads current recovery facts without changing
// task state, Git state, worker ownership, or external systems.
type RecoveryObservationSource interface {
	ObserveCheckpointRecovery(
		context.Context,
		storage.RecoveryCheckpointCandidate,
		checkpoint.Snapshot,
	) (RecoveryObservation, error)
}

// RecoveryPatchLocation identifies an already-preserved, exact checkpoint
// patch. Locating it must not create or mutate Git state.
type RecoveryPatchLocation struct {
	Available bool
	Locator   string
	Path      string
}

// RecoveryPatchLocator is the Lane A patch-preservation read port.
type RecoveryPatchLocator interface {
	LocateCheckpointPatch(
		context.Context,
		domain.TaskID,
		domain.CheckpointID,
	) (RecoveryPatchLocation, error)
}

// RecoveryAssessmentService discovers and classifies incomplete work without
// automatically resuming or repeating any action.
type RecoveryAssessmentService struct {
	checkpoints  RecoveryCheckpointSource
	observations RecoveryObservationSource
	patches      RecoveryPatchLocator
}

// NewRecoveryAssessmentService validates recovery assessment dependencies.
func NewRecoveryAssessmentService(
	checkpoints RecoveryCheckpointSource,
	observations RecoveryObservationSource,
	patches RecoveryPatchLocator,
) (*RecoveryAssessmentService, error) {
	if checkpoints == nil || observations == nil || patches == nil {
		return nil, errors.New(
			"recovery checkpoint, observation, and patch ports are required",
		)
	}
	return &RecoveryAssessmentService{
		checkpoints: checkpoints, observations: observations, patches: patches,
	}, nil
}

// AssessIncompleteTaskRuns records one structured assessment per latest
// checkpoint and waits for a separate user decision.
func (service *RecoveryAssessmentService) AssessIncompleteTaskRuns(
	ctx context.Context,
	limit int,
) ([]storage.RecoveryAssessmentRecord, error) {
	if service == nil {
		return nil, errors.New("recovery assessment service is unavailable")
	}
	if limit < 1 || limit > maximumRecoveryCandidates {
		return nil, errors.New("recovery assessment limit is outside supported bounds")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	candidates, err := service.checkpoints.ListRecoveryCheckpointCandidates(
		ctx,
		limit,
	)
	if err != nil {
		return nil, err
	}
	if len(candidates) > limit {
		return nil, errors.New("recovery checkpoint source exceeded requested bound")
	}
	records := make([]storage.RecoveryAssessmentRecord, 0, len(candidates))
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return records, err
		}
		assessment, observationSHA, err := service.assessCandidate(ctx, candidate)
		if err != nil {
			return records, err
		}
		findingsJSON, err := json.Marshal(assessment.Findings)
		if err != nil {
			return records, fmt.Errorf("encode recovery findings: %w", err)
		}
		divergencesJSON, err := json.Marshal([]struct {
			NonOverlappingUserEditPaths  []string `json:"non_overlapping_user_edit_paths,omitempty"`
			ActionsThatMustNotBeRepeated []string `json:"actions_that_must_not_be_repeated,omitempty"`
		}{{
			NonOverlappingUserEditPaths: append(
				[]string(nil),
				assessment.NonOverlappingUserEditPaths...,
			),
			ActionsThatMustNotBeRepeated: append(
				[]string(nil),
				assessment.ActionsThatMustNotBeRepeated...,
			),
		}})
		if err != nil {
			return records, fmt.Errorf("encode recovery divergences: %w", err)
		}
		idempotencyKey := recoveryAssessmentIdempotencyKey(
			candidate,
			observationSHA,
		)
		record, err := service.checkpoints.RecordRecoveryAssessment(
			ctx,
			storage.RecordRecoveryAssessment{
				ID:                idempotencyKey,
				TaskID:            assessment.TaskID,
				RunID:             assessment.RunID,
				CheckpointID:      assessment.CheckpointID,
				Classification:    storageRecoveryClassification(assessment.Classification),
				FindingsJSON:      string(findingsJSON),
				DivergencesJSON:   string(divergencesJSON),
				ObservationSHA256: observationSHA,
				PatchAvailable:    assessment.PatchAvailable,
				PatchLocator:      assessment.PatchLocator,
				PatchPath:         assessment.PatchExportPath,
				IdempotencyKey:    idempotencyKey,
			},
		)
		if err != nil {
			return records, err
		}
		records = append(records, record)
	}
	return records, nil
}

func (service *RecoveryAssessmentService) assessCandidate(
	ctx context.Context,
	candidate storage.RecoveryCheckpointCandidate,
) (RecoveryAssessment, string, error) {
	if candidate.TaskID.IsZero() || candidate.RunID.IsZero() {
		return RecoveryAssessment{}, "", errors.New(
			"recovery candidate task and run identities are required",
		)
	}
	if candidate.CheckpointID == nil {
		observationSHA, hashErr := hashRecoveryObservation(struct {
			TaskID            domain.TaskID
			RunID             domain.RunID
			CheckpointMissing bool
		}{
			TaskID: candidate.TaskID, RunID: candidate.RunID,
			CheckpointMissing: true,
		})
		return RecoveryAssessment{
			TaskID:         candidate.TaskID,
			RunID:          candidate.RunID,
			Classification: RecoveryUnrecoverable,
			Findings: []RecoveryFinding{{
				Code: RecoveryFindingCheckpointMissing,
				Detail: "the incomplete run has no durable checkpoint; " +
					"automatic resume is unavailable",
			}},
		}, observationSHA, hashErr
	}
	patch, err := service.patches.LocateCheckpointPatch(
		ctx,
		candidate.TaskID,
		*candidate.CheckpointID,
	)
	if err != nil {
		return RecoveryAssessment{}, "", err
	}
	if patch.Available != (patch.Locator != "" || patch.Path != "") {
		return RecoveryAssessment{}, "", errors.New(
			"recovery patch locator returned inconsistent availability",
		)
	}

	snapshot, decodeErr := decodeRecoveryCheckpoint(candidate)
	if decodeErr != nil {
		assessment := RecoveryAssessment{
			CheckpointID:    candidate.CheckpointID,
			TaskID:          candidate.TaskID,
			RunID:           candidate.RunID,
			Classification:  RecoveryUnrecoverable,
			PatchAvailable:  patch.Available,
			PatchLocator:    patch.Locator,
			PatchExportPath: patch.Path,
			Findings: []RecoveryFinding{{
				Code:   RecoveryFindingCheckpointCorrupt,
				Detail: "the latest checkpoint cannot be validated against its durable identity",
			}},
		}
		observationSHA, hashErr := hashRecoveryObservation(struct {
			CheckpointStateSHA256 string
			DecodeError           bool
			Patch                 RecoveryPatchLocation
		}{
			CheckpointStateSHA256: candidate.StateSHA256,
			DecodeError:           true,
			Patch:                 patch,
		})
		return assessment, observationSHA, hashErr
	}
	observation, err := service.observations.ObserveCheckpointRecovery(
		ctx,
		candidate,
		snapshot,
	)
	if err != nil {
		return RecoveryAssessment{}, "", err
	}
	observation.PatchAvailable = patch.Available
	observation.PatchLocator = patch.Locator
	observation.PatchExportPath = patch.Path
	observationSHA, err := hashRecoveryObservation(struct {
		CheckpointStateSHA256 string
		Observation           RecoveryObservation
	}{
		CheckpointStateSHA256: candidate.StateSHA256,
		Observation:           observation,
	})
	if err != nil {
		return RecoveryAssessment{}, "", err
	}
	assessment, err := ClassifyCheckpointRecovery(
		RecoveryClassificationInput{
			CheckpointID:            *candidate.CheckpointID,
			CheckpointEventSequence: candidate.CheckpointEventSequence,
			Checkpoint:              snapshot,
			Observation:             observation,
		},
	)
	return assessment, observationSHA, err
}

func decodeRecoveryCheckpoint(
	candidate storage.RecoveryCheckpointCandidate,
) (checkpoint.Snapshot, error) {
	if candidate.CheckpointID == nil ||
		candidate.SchemaVersion != checkpoint.SchemaVersion ||
		candidate.StateJSON == "" ||
		len(candidate.StateSHA256) != sha256.Size*2 {
		return checkpoint.Snapshot{}, errors.New("checkpoint encoding metadata is invalid")
	}
	digest := sha256.Sum256([]byte(candidate.StateJSON))
	if hex.EncodeToString(digest[:]) != candidate.StateSHA256 {
		return checkpoint.Snapshot{}, errors.New("checkpoint state hash differs from content")
	}
	var snapshot checkpoint.Snapshot
	if err := json.Unmarshal([]byte(candidate.StateJSON), &snapshot); err != nil {
		return checkpoint.Snapshot{}, err
	}
	canonical, err := checkpoint.Canonicalize(snapshot)
	if err != nil ||
		canonical.JSON != candidate.StateJSON ||
		canonical.StateSHA256 != candidate.StateSHA256 ||
		canonical.Snapshot.TaskID != candidate.TaskID ||
		canonical.Snapshot.RunID != candidate.RunID ||
		canonical.Snapshot.LastDurableEventSequence+1 !=
			candidate.CheckpointEventSequence {
		return checkpoint.Snapshot{}, errors.New(
			"checkpoint state is not canonical or does not match its record",
		)
	}
	return canonical.Snapshot, nil
}

func recoveryAssessmentIdempotencyKey(
	candidate storage.RecoveryCheckpointCandidate,
	observationSHA string,
) string {
	identity := candidate.TaskID.String() + ":" + candidate.RunID.String()
	if candidate.CheckpointID != nil {
		identity = candidate.CheckpointID.String()
	}
	return "recovery-assessment:" + identity + ":" + observationSHA
}

func hashRecoveryObservation(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode recovery observation: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func storageRecoveryClassification(
	value RecoveryClassification,
) storage.RecoveryClassification {
	switch value {
	case RecoverySafeResume:
		return storage.RecoveryClassificationSafeResume
	case RecoveryReconcileRequired:
		return storage.RecoveryClassificationReconcile
	case RecoveryPatchPreservationOnly:
		return storage.RecoveryClassificationPatchOnly
	default:
		return storage.RecoveryClassificationImpossible
	}
}
