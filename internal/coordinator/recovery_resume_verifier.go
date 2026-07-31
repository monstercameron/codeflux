package coordinator

import (
	"context"
	"errors"
	"slices"

	"codeflux.dev/codeflux/internal/checkpoint"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/workspace"
)

type recoveryLatestCheckpointSource interface {
	LatestCheckpoint(
		context.Context,
		domain.TaskID,
		domain.RunID,
	) (checkpoint.PersistedCheckpoint, error)
}

// RecoveryResumeVerifier applies the same exact checkpoint classifier at the
// pause/resume boundary and translates it into the TaskControl closed result.
type RecoveryResumeVerifier struct {
	checkpoints  recoveryLatestCheckpointSource
	observations RecoveryObservationSource
	patches      RecoveryPatchLocator
}

func NewRecoveryResumeVerifier(
	checkpoints recoveryLatestCheckpointSource,
	observations RecoveryObservationSource,
	patches recoveryPatchMetadataSource,
) (*RecoveryResumeVerifier, error) {
	if checkpoints == nil || observations == nil || patches == nil {
		return nil, errors.New(
			"resume checkpoint, observation, and patch metadata are required",
		)
	}
	locator, err := NewDurableRecoveryPatchLocator(
		patches,
		workspace.ExecRunner{},
	)
	if err != nil {
		return nil, err
	}
	return &RecoveryResumeVerifier{
		checkpoints: checkpoints, observations: observations, patches: locator,
	}, nil
}

func (verifier *RecoveryResumeVerifier) VerifyResume(
	ctx context.Context,
	control storage.TaskControlSnapshot,
) (ResumeAssessment, error) {
	if verifier == nil {
		return ResumeAssessment{}, errors.New(
			"recovery resume verifier is unavailable",
		)
	}
	if control.TaskID.IsZero() || control.RunID.IsZero() {
		return ResumeAssessment{}, errors.New(
			"resume task and run identities are required",
		)
	}
	persisted, err := verifier.checkpoints.LatestCheckpoint(
		ctx,
		control.TaskID,
		control.RunID,
	)
	if err != nil {
		return ResumeAssessment{}, err
	}
	checkpointID := persisted.ID
	candidate := storage.RecoveryCheckpointCandidate{
		CheckpointID:            &checkpointID,
		TaskID:                  persisted.TaskID,
		RunID:                   persisted.RunID,
		SchemaVersion:           persisted.SchemaVersion,
		StateJSON:               persisted.StateJSON,
		StateSHA256:             persisted.StateSHA256,
		CheckpointEventSequence: persisted.CheckpointEventSequence,
	}
	snapshot, err := decodeRecoveryCheckpoint(candidate)
	if err != nil {
		return ResumeAssessment{}, err
	}
	patch, err := verifier.patches.LocateCheckpointPatch(
		ctx,
		control.TaskID,
		checkpointID,
	)
	if err != nil {
		return ResumeAssessment{}, err
	}
	observation, err := verifier.observations.ObserveCheckpointRecovery(
		ctx,
		candidate,
		snapshot,
	)
	if err != nil {
		return ResumeAssessment{}, err
	}
	observation.PatchAvailable = patch.Available
	observation.PatchLocator = patch.Locator
	observation.PatchExportPath = patch.Path
	assessment, err := ClassifyCheckpointRecovery(
		RecoveryClassificationInput{
			CheckpointID:            checkpointID,
			CheckpointEventSequence: persisted.CheckpointEventSequence,
			Checkpoint:              snapshot,
			Observation:             observation,
		},
	)
	if err != nil {
		return ResumeAssessment{}, err
	}
	return resumeAssessmentFromRecovery(observation, assessment), nil
}

func resumeAssessmentFromRecovery(
	observation RecoveryObservation,
	assessment RecoveryAssessment,
) ResumeAssessment {
	result := ResumeAssessment{
		RepositoryCurrent: observation.RepositoryPathMatches &&
			observation.RepositoryIdentityMatches &&
			observation.BaseRevisionAvailable,
		WorktreeCurrent: observation.WorktreeExists &&
			observation.WorktreeOwned,
		PolicyCurrent:   observation.PolicyCompatible,
		ProviderCurrent: observation.ProviderCompatible,
		ToolsCurrent:    observation.ToolsCompatible,
		AmbiguousExternalOutcome: len(
			assessment.ActionsThatMustNotBeRepeated,
		) != 0,
		PausedEdits: PausedEditsUnchanged,
		ReasonRedacted: "checkpoint recovery classification: " +
			string(assessment.Classification),
	}
	for _, finding := range assessment.Findings {
		switch finding.Code {
		case RecoveryFindingWorktreeBindingChanged,
			RecoveryFindingWorktreeHeadChanged,
			RecoveryFindingDiffIdentityChanged,
			RecoveryFindingGitOperationUnresolved:
			result.WorktreeCurrent = false
		case RecoveryFindingDirtyFileConflict:
			result.WorktreeCurrent = false
			result.PausedEdits = PausedEditsConflicting
			result.ConflictFiles = append(
				result.ConflictFiles,
				finding.Paths...,
			)
		case RecoveryFindingAmbiguousExternalAction,
			RecoveryFindingEventJournalBehind,
			RecoveryFindingCheckpointCorrupt,
			RecoveryFindingCheckpointMissing:
			result.AmbiguousExternalOutcome = true
		}
	}
	result.ChangedFiles = append(
		result.ChangedFiles,
		assessment.NonOverlappingUserEditPaths...,
	)
	if len(result.ConflictFiles) != 0 {
		result.ChangedFiles = append(
			result.ChangedFiles,
			result.ConflictFiles...,
		)
	} else if len(result.ChangedFiles) != 0 {
		result.PausedEdits = PausedEditsNonOverlapping
	}
	slices.Sort(result.ChangedFiles)
	result.ChangedFiles = slices.Compact(result.ChangedFiles)
	slices.Sort(result.ConflictFiles)
	result.ConflictFiles = slices.Compact(result.ConflictFiles)
	return result
}

var _ ResumeVerifier = (*RecoveryResumeVerifier)(nil)
