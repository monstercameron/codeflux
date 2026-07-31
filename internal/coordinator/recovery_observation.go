package coordinator

import (
	"context"
	"errors"
	"path/filepath"

	"codeflux.dev/codeflux/internal/checkpoint"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/workspace"
)

// RecoveryMetadataSource reads durable repository, worktree, and event
// bindings used by recovery observation.
type RecoveryMetadataSource interface {
	GetRepository(context.Context, domain.RepositoryID) (storage.Repository, error)
	GetWorktreeBinding(context.Context, domain.TaskID) (storage.WorktreeBinding, error)
	ReplayTask(context.Context, domain.TaskID) (storage.TaskReplay, error)
}

// RecoveryWorktreeFacts is the shared divergence-tolerant Lane A observation.
type RecoveryWorktreeFacts = checkpoint.RecoveryWorktreeFacts

// RecoveryWorktreeFactSource observes the task worktree without mutation.
type RecoveryWorktreeFactSource interface {
	ObserveRecoveryWorktree(
		context.Context,
		storage.WorktreeBinding,
		checkpoint.Snapshot,
	) (RecoveryWorktreeFacts, error)
}

// RecoveryCompatibilityFacts are authoritative current non-secret bindings.
// The coordinator compares every exact binding to the checkpoint rather than
// accepting a caller-computed compatibility verdict.
type RecoveryCompatibilityFacts struct {
	Policy   checkpoint.PolicyBinding
	Provider checkpoint.ProviderBinding
	Tools    checkpoint.ToolBinding
}

// RecoveryCompatibilitySource reads current policy, provider/model, and tool
// bindings without switching or migrating them.
type RecoveryCompatibilitySource interface {
	ObserveRecoveryCompatibility(
		context.Context,
		domain.TaskID,
		domain.RunID,
	) (RecoveryCompatibilityFacts, error)
}

// RecoveryActionFacts prevent replay of already completed or outcome-unknown
// operations, including facts committed after the checkpoint event.
type RecoveryActionFacts struct {
	CompletedActionIDs       []string
	AmbiguousExternalActions []checkpoint.AmbiguousExternalAction
}

// RecoveryActionSource reads attributable durable model, tool, command, and
// external-effect outcomes.
type RecoveryActionSource interface {
	ObserveRecoveryActions(
		context.Context,
		domain.TaskID,
		domain.RunID,
		uint64,
	) (RecoveryActionFacts, error)
}

// DurableRecoveryObservationSource verifies repository identity and base
// availability directly, then composes the Lane A worktree and durable
// compatibility/action ports.
type DurableRecoveryObservationSource struct {
	metadata      RecoveryMetadataSource
	worktrees     RecoveryWorktreeFactSource
	compatibility RecoveryCompatibilitySource
	actions       RecoveryActionSource
	repositories  workspace.CommandRunner
}

// NewDurableRecoveryObservationSource validates recovery observation ports.
func NewDurableRecoveryObservationSource(
	metadata RecoveryMetadataSource,
	worktrees RecoveryWorktreeFactSource,
	compatibility RecoveryCompatibilitySource,
	actions RecoveryActionSource,
	repositories workspace.CommandRunner,
) (*DurableRecoveryObservationSource, error) {
	if metadata == nil || worktrees == nil || compatibility == nil ||
		actions == nil || repositories == nil {
		return nil, errors.New(
			"recovery metadata, worktree, compatibility, action, and repository ports are required",
		)
	}
	return &DurableRecoveryObservationSource{
		metadata: metadata, worktrees: worktrees,
		compatibility: compatibility, actions: actions,
		repositories: repositories,
	}, nil
}

// ObserveCheckpointRecovery performs only bounded reads and never starts,
// resumes, repeats, fetches, resets, or cleans up work.
func (source *DurableRecoveryObservationSource) ObserveCheckpointRecovery(
	ctx context.Context,
	candidate storage.RecoveryCheckpointCandidate,
	snapshot checkpoint.Snapshot,
) (RecoveryObservation, error) {
	if source == nil {
		return RecoveryObservation{}, errors.New(
			"durable recovery observation source is unavailable",
		)
	}
	repository, err := source.metadata.GetRepository(ctx, snapshot.RepositoryID)
	if err != nil {
		return RecoveryObservation{}, err
	}
	binding, err := source.metadata.GetWorktreeBinding(ctx, snapshot.TaskID)
	if err != nil {
		return RecoveryObservation{}, err
	}
	replay, err := source.metadata.ReplayTask(ctx, snapshot.TaskID)
	if err != nil {
		return RecoveryObservation{}, err
	}
	worktree, err := source.worktrees.ObserveRecoveryWorktree(
		ctx,
		binding,
		snapshot,
	)
	if err != nil {
		return RecoveryObservation{}, err
	}
	compatibility, err := source.compatibility.ObserveRecoveryCompatibility(
		ctx,
		snapshot.TaskID,
		snapshot.RunID,
	)
	if err != nil {
		return RecoveryObservation{}, err
	}
	actions, err := source.actions.ObserveRecoveryActions(
		ctx,
		snapshot.TaskID,
		snapshot.RunID,
		candidate.CheckpointEventSequence,
	)
	if err != nil {
		return RecoveryObservation{}, err
	}

	observation := RecoveryObservation{
		WorktreeExists: worktree.Exists,
		WorktreeOwned: worktree.Owned &&
			binding.TaskID == snapshot.TaskID &&
			binding.RepositoryID == snapshot.RepositoryID &&
			worktree.RepositoryID == snapshot.RepositoryID &&
			recoveryWorktreeBranchMatches(
				ctx,
				source.repositories,
				binding,
			),
		WorktreeBindingRevision: worktree.BindingRevision,
		WorktreeHead:            worktree.HeadRevision,
		DirtyFiles:              append([]checkpoint.DirtyFileHash(nil), worktree.DirtyFiles...),
		DiffSHA256:              worktree.DiffSHA256,
		GitOperationStates:      append([]string(nil), worktree.UnresolvedGitOperations...),
		PolicyCompatible:        compatibility.Policy == snapshot.Policy,
		ProviderCompatible:      compatibility.Provider == snapshot.Provider,
		ToolsCompatible:         compatibility.Tools == snapshot.Tools,
		DurableEventSequence:    replay.EventCount,
		CompletedActionIDs:      append([]string(nil), actions.CompletedActionIDs...),
		AmbiguousExternalActions: append(
			[]checkpoint.AmbiguousExternalAction(nil),
			actions.AmbiguousExternalActions...,
		),
	}
	discovered, discoverErr := workspace.DiscoverRepository(
		ctx,
		repository.CanonicalPath,
		source.repositories,
	)
	if discoverErr == nil {
		observation.RepositoryPathMatches = sameRecoveryPath(
			discovered.CanonicalRoot,
			repository.CanonicalPath,
		)
		observation.RepositoryIdentityMatches =
			discovered.GitIdentity == repository.GitIdentity
		observation.BaseRevisionAvailable = recoveryBaseRevisionAvailable(
			ctx,
			source.repositories,
			discovered.CanonicalRoot,
			snapshot.BaseRevision,
		)
	} else if ctxErr := ctx.Err(); ctxErr != nil {
		return RecoveryObservation{}, ctxErr
	}
	return observation, nil
}

func recoveryWorktreeBranchMatches(
	ctx context.Context,
	runner workspace.CommandRunner,
	binding storage.WorktreeBinding,
) bool {
	if binding.WorktreePath == "" || binding.BranchName == "" {
		return false
	}
	result, err := runner.Run(
		ctx,
		binding.WorktreePath,
		"git",
		"symbolic-ref",
		"--short",
		"HEAD",
	)
	return err == nil &&
		string(result.Stdout) == binding.BranchName+"\n"
}

func recoveryBaseRevisionAvailable(
	ctx context.Context,
	runner workspace.CommandRunner,
	repositoryPath string,
	baseRevision string,
) bool {
	_, err := runner.Run(
		ctx,
		repositoryPath,
		"git",
		"cat-file",
		"-e",
		baseRevision+"^{commit}",
	)
	return err == nil
}

func sameRecoveryPath(left, right string) bool {
	relative, err := filepath.Rel(filepath.Clean(left), filepath.Clean(right))
	return err == nil && relative == "."
}

var _ RecoveryObservationSource = (*DurableRecoveryObservationSource)(nil)
