package coordinator

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"codeflux.dev/codeflux/internal/checkpoint"
	"codeflux.dev/codeflux/internal/domain"
)

// RecoveryClassification is the user-visible safety class assigned after
// comparing one durable checkpoint with current repository and runtime facts.
type RecoveryClassification string

const (
	RecoverySafeResume            RecoveryClassification = "safe-resume"
	RecoveryReconcileRequired     RecoveryClassification = "reconcile-required"
	RecoveryPatchPreservationOnly RecoveryClassification = "patch-preservation-only"
	RecoveryUnrecoverable         RecoveryClassification = "unrecoverable"
)

// RecoveryFindingCode identifies one bounded, structured divergence.
type RecoveryFindingCode string

const (
	RecoveryFindingCheckpointCorrupt         RecoveryFindingCode = "checkpoint-corrupt"
	RecoveryFindingCheckpointMissing         RecoveryFindingCode = "checkpoint-missing"
	RecoveryFindingRepositoryPathChanged     RecoveryFindingCode = "repository-path-changed"
	RecoveryFindingRepositoryIdentityChanged RecoveryFindingCode = "repository-identity-changed"
	RecoveryFindingBaseRevisionUnavailable   RecoveryFindingCode = "base-revision-unavailable"
	RecoveryFindingWorktreeMissing           RecoveryFindingCode = "worktree-missing"
	RecoveryFindingWorktreeOwnershipChanged  RecoveryFindingCode = "worktree-ownership-changed"
	RecoveryFindingWorktreeBindingChanged    RecoveryFindingCode = "worktree-binding-changed"
	RecoveryFindingWorktreeHeadChanged       RecoveryFindingCode = "worktree-head-changed"
	RecoveryFindingDirtyFileConflict         RecoveryFindingCode = "dirty-file-conflict"
	RecoveryFindingNonOverlappingUserEdit    RecoveryFindingCode = "non-overlapping-user-edit"
	RecoveryFindingDiffIdentityChanged       RecoveryFindingCode = "diff-identity-changed"
	RecoveryFindingGitOperationUnresolved    RecoveryFindingCode = "git-operation-unresolved"
	RecoveryFindingPolicyChanged             RecoveryFindingCode = "policy-changed"
	RecoveryFindingProviderChanged           RecoveryFindingCode = "provider-changed"
	RecoveryFindingToolConfigurationChanged  RecoveryFindingCode = "tool-configuration-changed"
	RecoveryFindingAmbiguousExternalAction   RecoveryFindingCode = "ambiguous-external-action"
	RecoveryFindingEventJournalBehind        RecoveryFindingCode = "event-journal-behind-checkpoint"
	RecoveryFindingEventsAfterCheckpoint     RecoveryFindingCode = "events-after-checkpoint"
)

// RecoveryFinding describes one exact reason that affects resume safety.
type RecoveryFinding struct {
	Code   RecoveryFindingCode `json:"code"`
	Detail string              `json:"detail"`
	Paths  []string            `json:"paths,omitempty"`
}

// RecoveryObservation is a read-only, self-consistent view of the current
// repository, worktree, configuration, and event journal.
type RecoveryObservation struct {
	RepositoryPathMatches     bool
	RepositoryIdentityMatches bool
	BaseRevisionAvailable     bool
	WorktreeExists            bool
	WorktreeOwned             bool
	WorktreeBindingRevision   uint64
	WorktreeHead              string
	DirtyFiles                []checkpoint.DirtyFileHash
	DiffSHA256                string
	GitOperationStates        []string
	PolicyCompatible          bool
	ProviderCompatible        bool
	ToolsCompatible           bool
	DurableEventSequence      uint64
	CompletedActionIDs        []string
	AmbiguousExternalActions  []checkpoint.AmbiguousExternalAction
	PatchAvailable            bool
	PatchLocator              string
	PatchExportPath           string
}

// RecoveryClassificationInput binds an observation to its exact durable
// checkpoint identity and state.
type RecoveryClassificationInput struct {
	CheckpointID            domain.CheckpointID
	CheckpointEventSequence uint64
	Checkpoint              checkpoint.Snapshot
	Observation             RecoveryObservation
}

// RecoveryAssessment is the complete presentation-safe result of recovery
// classification. It never grants authority or starts a worker.
type RecoveryAssessment struct {
	CheckpointID                 *domain.CheckpointID
	TaskID                       domain.TaskID
	RunID                        domain.RunID
	Classification               RecoveryClassification
	Findings                     []RecoveryFinding
	NonOverlappingUserEditPaths  []string
	ActionsThatMustNotBeRepeated []string
	PatchAvailable               bool
	PatchLocator                 string
	PatchExportPath              string
}

// ClassifyCheckpointRecovery compares durable and observed facts without
// mutating storage, Git, worker ownership, or task state.
func ClassifyCheckpointRecovery(
	input RecoveryClassificationInput,
) (RecoveryAssessment, error) {
	if input.CheckpointID.IsZero() {
		return RecoveryAssessment{}, errors.New("recovery checkpoint ID is required")
	}
	canonical, err := checkpoint.Canonicalize(input.Checkpoint)
	if err != nil {
		return RecoveryAssessment{}, fmt.Errorf(
			"validate recovery checkpoint: %w",
			err,
		)
	}
	if input.CheckpointEventSequence != 0 &&
		input.CheckpointEventSequence !=
			canonical.Snapshot.LastDurableEventSequence+1 {
		return RecoveryAssessment{}, errors.New(
			"recovery checkpoint event sequence is inconsistent",
		)
	}
	observation := cloneRecoveryObservation(input.Observation)
	if err := validateRecoveryObservation(canonical.Snapshot, observation); err != nil {
		return RecoveryAssessment{}, err
	}
	checkpointID := input.CheckpointID
	assessment := RecoveryAssessment{
		CheckpointID:    &checkpointID,
		TaskID:          canonical.Snapshot.TaskID,
		RunID:           canonical.Snapshot.RunID,
		PatchAvailable:  observation.PatchAvailable,
		PatchLocator:    observation.PatchLocator,
		PatchExportPath: observation.PatchExportPath,
	}
	appendCompletedRecoveryActionReplayGate(
		&assessment,
		observation.CompletedActionIDs,
	)
	ambiguousActions := mergeAmbiguousRecoveryActions(
		canonical.Snapshot.AmbiguousExternalActions,
		observation.AmbiguousExternalActions,
	)

	fundamentalLoss := appendFundamentalRecoveryFindings(
		&assessment,
		observation,
	)
	if fundamentalLoss {
		if observation.PatchAvailable {
			assessment.Classification = RecoveryPatchPreservationOnly
		} else {
			assessment.Classification = RecoveryUnrecoverable
		}
		appendAmbiguousRecoveryFindings(
			&assessment,
			ambiguousActions,
		)
		return assessment, nil
	}

	reconcile := appendWorktreeRecoveryFindings(
		&assessment,
		canonical.Snapshot,
		observation,
	)
	reconcile = appendCompatibilityRecoveryFindings(
		&assessment,
		observation,
	) || reconcile
	reconcile = appendEventRecoveryFindings(
		&assessment,
		recoveryCheckpointEventSequence(
			input.CheckpointEventSequence,
			canonical.Snapshot.LastDurableEventSequence,
		),
		observation.DurableEventSequence,
	) || reconcile
	if appendAmbiguousRecoveryFindings(
		&assessment,
		ambiguousActions,
	) {
		reconcile = true
	}
	if hasRecoveryFinding(assessment.Findings, RecoveryFindingEventJournalBehind) {
		assessment.Classification = RecoveryUnrecoverable
	} else if reconcile {
		assessment.Classification = RecoveryReconcileRequired
	} else {
		assessment.Classification = RecoverySafeResume
	}
	return assessment, nil
}

func recoveryCheckpointEventSequence(
	persisted uint64,
	snapshot uint64,
) uint64 {
	if persisted == 0 {
		return snapshot
	}
	return persisted
}

func appendFundamentalRecoveryFindings(
	assessment *RecoveryAssessment,
	observation RecoveryObservation,
) bool {
	fundamentalLoss := false
	if !observation.RepositoryPathMatches {
		fundamentalLoss = true
		assessment.Findings = append(assessment.Findings, RecoveryFinding{
			Code: RecoveryFindingRepositoryPathChanged,
			Detail: "the durable repository path no longer resolves to the " +
				"observed repository",
		})
	}
	if !observation.RepositoryIdentityMatches {
		fundamentalLoss = true
		assessment.Findings = append(assessment.Findings, RecoveryFinding{
			Code: RecoveryFindingRepositoryIdentityChanged,
			Detail: "the repository Git identity differs from the durable " +
				"repository identity",
		})
	}
	if !observation.BaseRevisionAvailable {
		fundamentalLoss = true
		assessment.Findings = append(assessment.Findings, RecoveryFinding{
			Code: RecoveryFindingBaseRevisionUnavailable,
			Detail: "the checkpoint base revision is not available in the " +
				"observed repository",
		})
	}
	if !observation.WorktreeExists {
		fundamentalLoss = true
		assessment.Findings = append(assessment.Findings, RecoveryFinding{
			Code:   RecoveryFindingWorktreeMissing,
			Detail: "the task worktree is missing or inaccessible",
		})
	} else if !observation.WorktreeOwned {
		fundamentalLoss = true
		assessment.Findings = append(assessment.Findings, RecoveryFinding{
			Code: RecoveryFindingWorktreeOwnershipChanged,
			Detail: "the task worktree is not owned by the checkpoint task " +
				"binding",
		})
	}
	return fundamentalLoss
}

func appendWorktreeRecoveryFindings(
	assessment *RecoveryAssessment,
	snapshot checkpoint.Snapshot,
	observation RecoveryObservation,
) bool {
	reconcile := false
	if observation.WorktreeBindingRevision != snapshot.WorktreeBindingRevision {
		reconcile = true
		assessment.Findings = append(assessment.Findings, RecoveryFinding{
			Code: RecoveryFindingWorktreeBindingChanged,
			Detail: fmt.Sprintf(
				"worktree binding revision is %d; checkpoint recorded %d",
				observation.WorktreeBindingRevision,
				snapshot.WorktreeBindingRevision,
			),
		})
	}
	if observation.WorktreeHead != snapshot.WorktreeHead {
		reconcile = true
		assessment.Findings = append(assessment.Findings, RecoveryFinding{
			Code:   RecoveryFindingWorktreeHeadChanged,
			Detail: "worktree HEAD differs from the checkpoint worktree HEAD",
		})
	}

	conflicts, additions := compareCheckpointDirtyFiles(
		snapshot.DirtyFiles,
		observation.DirtyFiles,
	)
	if len(conflicts) != 0 {
		reconcile = true
		assessment.Findings = append(assessment.Findings, RecoveryFinding{
			Code:   RecoveryFindingDirtyFileConflict,
			Detail: "checkpointed file content was changed, removed, or replaced",
			Paths:  conflicts,
		})
	}
	if len(additions) != 0 {
		assessment.NonOverlappingUserEditPaths = additions
		assessment.Findings = append(assessment.Findings, RecoveryFinding{
			Code: RecoveryFindingNonOverlappingUserEdit,
			Detail: "user edits outside the checkpointed path set are present " +
				"and must remain visible during resume",
			Paths: additions,
		})
	}
	if observation.DiffSHA256 != snapshot.DiffSHA256 &&
		len(conflicts) == 0 && len(additions) == 0 {
		reconcile = true
		assessment.Findings = append(assessment.Findings, RecoveryFinding{
			Code: RecoveryFindingDiffIdentityChanged,
			Detail: "the worktree diff identity changed without a file-content " +
				"explanation",
		})
	}
	if len(observation.GitOperationStates) != 0 {
		reconcile = true
		assessment.Findings = append(assessment.Findings, RecoveryFinding{
			Code:   RecoveryFindingGitOperationUnresolved,
			Detail: "an unresolved Git operation prevents automatic resume",
			Paths:  append([]string(nil), observation.GitOperationStates...),
		})
	}
	return reconcile
}

func appendCompatibilityRecoveryFindings(
	assessment *RecoveryAssessment,
	observation RecoveryObservation,
) bool {
	reconcile := false
	if !observation.PolicyCompatible {
		reconcile = true
		assessment.Findings = append(assessment.Findings, RecoveryFinding{
			Code:   RecoveryFindingPolicyChanged,
			Detail: "the effective execution policy is not checkpoint-compatible",
		})
	}
	if !observation.ProviderCompatible {
		reconcile = true
		assessment.Findings = append(assessment.Findings, RecoveryFinding{
			Code: RecoveryFindingProviderChanged,
			Detail: "the provider or model configuration is not compatible " +
				"with the checkpointed run",
		})
	}
	if !observation.ToolsCompatible {
		reconcile = true
		assessment.Findings = append(assessment.Findings, RecoveryFinding{
			Code:   RecoveryFindingToolConfigurationChanged,
			Detail: "the tool schema or catalog is not checkpoint-compatible",
		})
	}
	return reconcile
}

func appendEventRecoveryFindings(
	assessment *RecoveryAssessment,
	checkpointSequence uint64,
	currentSequence uint64,
) bool {
	switch {
	case currentSequence < checkpointSequence:
		assessment.Findings = append(assessment.Findings, RecoveryFinding{
			Code: RecoveryFindingEventJournalBehind,
			Detail: fmt.Sprintf(
				"durable event sequence is %d; checkpoint recorded %d",
				currentSequence,
				checkpointSequence,
			),
		})
		return true
	case currentSequence > checkpointSequence:
		assessment.Findings = append(assessment.Findings, RecoveryFinding{
			Code: RecoveryFindingEventsAfterCheckpoint,
			Detail: fmt.Sprintf(
				"%d durable event(s) occurred after the checkpoint and must be replayed before resume",
				currentSequence-checkpointSequence,
			),
		})
	}
	return false
}

func appendAmbiguousRecoveryFindings(
	assessment *RecoveryAssessment,
	actions []checkpoint.AmbiguousExternalAction,
) bool {
	if len(actions) == 0 {
		return false
	}
	ids := make([]string, 0, len(actions))
	for _, action := range actions {
		ids = append(ids, action.ActionID)
	}
	slices.Sort(ids)
	assessment.ActionsThatMustNotBeRepeated = append(
		assessment.ActionsThatMustNotBeRepeated,
		ids...,
	)
	slices.Sort(assessment.ActionsThatMustNotBeRepeated)
	assessment.ActionsThatMustNotBeRepeated = slices.Compact(
		assessment.ActionsThatMustNotBeRepeated,
	)
	assessment.Findings = append(assessment.Findings, RecoveryFinding{
		Code: RecoveryFindingAmbiguousExternalAction,
		Detail: "one or more external actions have an ambiguous outcome and " +
			"must be reconciled rather than repeated",
		Paths: append([]string(nil), ids...),
	})
	return true
}

func appendCompletedRecoveryActionReplayGate(
	assessment *RecoveryAssessment,
	actionIDs []string,
) {
	assessment.ActionsThatMustNotBeRepeated = append(
		assessment.ActionsThatMustNotBeRepeated,
		actionIDs...,
	)
	slices.Sort(assessment.ActionsThatMustNotBeRepeated)
	assessment.ActionsThatMustNotBeRepeated = slices.Compact(
		assessment.ActionsThatMustNotBeRepeated,
	)
}

func mergeAmbiguousRecoveryActions(
	checkpointActions []checkpoint.AmbiguousExternalAction,
	currentActions []checkpoint.AmbiguousExternalAction,
) []checkpoint.AmbiguousExternalAction {
	byID := make(
		map[string]checkpoint.AmbiguousExternalAction,
		len(checkpointActions)+len(currentActions),
	)
	for _, action := range checkpointActions {
		byID[action.ActionID] = action
	}
	for _, action := range currentActions {
		byID[action.ActionID] = action
	}
	result := make([]checkpoint.AmbiguousExternalAction, 0, len(byID))
	for _, action := range byID {
		result = append(result, action)
	}
	slices.SortFunc(
		result,
		func(left, right checkpoint.AmbiguousExternalAction) int {
			return strings.Compare(left.ActionID, right.ActionID)
		},
	)
	return result
}

func compareCheckpointDirtyFiles(
	recorded []checkpoint.DirtyFileHash,
	observed []checkpoint.DirtyFileHash,
) ([]string, []string) {
	recordedByPath := make(map[string]checkpoint.DirtyFileHash, len(recorded))
	for _, file := range recorded {
		recordedByPath[file.Path] = file
	}
	observedByPath := make(map[string]checkpoint.DirtyFileHash, len(observed))
	for _, file := range observed {
		observedByPath[file.Path] = file
	}
	var conflicts []string
	for path, want := range recordedByPath {
		got, found := observedByPath[path]
		if !found || got.Exists != want.Exists || got.SHA256 != want.SHA256 {
			conflicts = append(conflicts, path)
		}
	}
	var additions []string
	for path := range observedByPath {
		if _, found := recordedByPath[path]; !found {
			additions = append(additions, path)
		}
	}
	slices.Sort(conflicts)
	slices.Sort(additions)
	return conflicts, additions
}

func validateRecoveryObservation(
	snapshot checkpoint.Snapshot,
	value RecoveryObservation,
) error {
	switch {
	case value.PatchAvailable &&
		strings.TrimSpace(value.PatchLocator) == "" &&
		strings.TrimSpace(value.PatchExportPath) == "":
		return errors.New("available recovery patch requires a locator or export path")
	case !value.PatchAvailable &&
		(value.PatchLocator != "" || value.PatchExportPath != ""):
		return errors.New("unavailable recovery patch cannot have a locator or export path")
	case value.WorktreeExists && value.WorktreeBindingRevision == 0:
		return errors.New("existing recovery worktree requires a binding revision")
	case value.WorktreeExists && value.WorktreeHead == "":
		return errors.New("existing recovery worktree requires a HEAD revision")
	case value.WorktreeExists && value.DiffSHA256 == "":
		return errors.New("existing recovery worktree requires a diff identity")
	}
	if !value.WorktreeExists {
		return validateRecoveryActionObservations(snapshot, value)
	}
	probe := snapshot
	probe.WorktreeBindingRevision = value.WorktreeBindingRevision
	probe.WorktreeHead = value.WorktreeHead
	probe.DirtyFiles = value.DirtyFiles
	probe.DiffSHA256 = value.DiffSHA256
	if _, err := checkpoint.Canonicalize(probe); err != nil {
		return fmt.Errorf("validate observed recovery worktree: %w", err)
	}
	return validateRecoveryActionObservations(snapshot, value)
}

func validateRecoveryActionObservations(
	snapshot checkpoint.Snapshot,
	value RecoveryObservation,
) error {
	previous := ""
	completed := append([]string(nil), value.CompletedActionIDs...)
	slices.Sort(completed)
	for _, actionID := range completed {
		if strings.TrimSpace(actionID) == "" ||
			len(actionID) > 255 ||
			actionID == previous {
			return errors.New("completed recovery action IDs are invalid")
		}
		previous = actionID
	}
	probe := snapshot
	probe.AmbiguousExternalActions = value.AmbiguousExternalActions
	probe.ExternalOutcomeAmbiguous =
		len(value.AmbiguousExternalActions) != 0
	if _, err := checkpoint.Canonicalize(probe); err != nil {
		return fmt.Errorf("validate ambiguous recovery actions: %w", err)
	}
	return nil
}

func cloneRecoveryObservation(value RecoveryObservation) RecoveryObservation {
	value.DirtyFiles = append([]checkpoint.DirtyFileHash(nil), value.DirtyFiles...)
	value.GitOperationStates = append(
		[]string(nil),
		value.GitOperationStates...,
	)
	slices.Sort(value.GitOperationStates)
	value.GitOperationStates = slices.Compact(value.GitOperationStates)
	value.CompletedActionIDs = append(
		[]string(nil),
		value.CompletedActionIDs...,
	)
	slices.Sort(value.CompletedActionIDs)
	value.AmbiguousExternalActions = append(
		[]checkpoint.AmbiguousExternalAction(nil),
		value.AmbiguousExternalActions...,
	)
	return value
}

func hasRecoveryFinding(
	findings []RecoveryFinding,
	code RecoveryFindingCode,
) bool {
	for _, finding := range findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}
