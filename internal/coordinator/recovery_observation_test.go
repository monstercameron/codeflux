package coordinator

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/checkpoint"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/workspace"
)

func TestDurableRecoveryObservationSourceVerifiesRepositoryAndBindings(
	t *testing.T,
) {
	t.Parallel()

	repositoryPath := createRecoveryObservationRepository(t)
	discovered, err := workspace.DiscoverRepository(
		t.Context(),
		repositoryPath,
		workspace.ExecRunner{},
	)
	if err != nil {
		t.Fatal(err)
	}
	branchResult, err := workspace.ExecRunner{}.Run(
		t.Context(),
		repositoryPath,
		"git",
		"symbolic-ref",
		"--short",
		"HEAD",
	)
	if err != nil {
		t.Fatal(err)
	}
	input := recoveryClassificationFixture(t)
	input.Checkpoint.BaseRevision = discovered.HeadRevision
	input.Checkpoint.WorktreeHead = discovered.HeadRevision
	input.Observation.WorktreeHead = discovered.HeadRevision
	canonical, err := checkpoint.Canonicalize(input.Checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	candidate := storage.RecoveryCheckpointCandidate{
		CheckpointID:            &input.CheckpointID,
		TaskID:                  input.Checkpoint.TaskID,
		RunID:                   input.Checkpoint.RunID,
		SchemaVersion:           checkpoint.SchemaVersion,
		StateJSON:               canonical.JSON,
		StateSHA256:             canonical.StateSHA256,
		CheckpointEventSequence: input.Checkpoint.LastDurableEventSequence + 1,
	}
	binding := storage.WorktreeBinding{
		TaskID:       input.Checkpoint.TaskID,
		RepositoryID: input.Checkpoint.RepositoryID,
		BaseRevision: input.Checkpoint.BaseRevision,
		HeadRevision: input.Checkpoint.WorktreeHead,
		WorktreePath: repositoryPath,
		BranchName:   strings.TrimSpace(string(branchResult.Stdout)),
		Revision:     input.Checkpoint.WorktreeBindingRevision,
	}
	metadata := &recoveryMetadataStub{
		repository: storage.Repository{
			ID:            input.Checkpoint.RepositoryID,
			CanonicalPath: discovered.CanonicalRoot,
			GitIdentity:   discovered.GitIdentity,
		},
		binding: binding,
		replay: storage.TaskReplay{
			EventCount: input.Checkpoint.LastDurableEventSequence + 1,
		},
	}
	worktrees := &recoveryWorktreeFactsStub{facts: RecoveryWorktreeFacts{
		Exists:          true,
		Owned:           true,
		RepositoryID:    input.Checkpoint.RepositoryID,
		BindingRevision: input.Checkpoint.WorktreeBindingRevision,
		HeadRevision:    input.Checkpoint.WorktreeHead,
		DirtyFiles:      input.Checkpoint.DirtyFiles,
		DiffSHA256:      input.Checkpoint.DiffSHA256,
	}}
	compatibility := &recoveryCompatibilityStub{
		facts: RecoveryCompatibilityFacts{
			Policy:   input.Checkpoint.Policy,
			Provider: input.Checkpoint.Provider,
			Tools:    input.Checkpoint.Tools,
		},
	}
	actions := &recoveryActionFactsStub{facts: RecoveryActionFacts{
		CompletedActionIDs: []string{"completed-tool-request"},
	}}
	source, err := NewDurableRecoveryObservationSource(
		metadata,
		worktrees,
		compatibility,
		actions,
		workspace.ExecRunner{},
	)
	if err != nil {
		t.Fatal(err)
	}
	got, err := source.ObserveCheckpointRecovery(
		t.Context(),
		candidate,
		input.Checkpoint,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !got.RepositoryPathMatches ||
		!got.RepositoryIdentityMatches ||
		!got.BaseRevisionAvailable ||
		!got.WorktreeExists ||
		!got.WorktreeOwned ||
		!got.PolicyCompatible ||
		!got.ProviderCompatible ||
		!got.ToolsCompatible ||
		len(got.CompletedActionIDs) != 1 {
		t.Fatalf("recovery observation = %#v", got)
	}
	metadata.binding.BranchName = "codeflux/task/switched-at-same-head"
	got, err = source.ObserveCheckpointRecovery(
		t.Context(),
		candidate,
		input.Checkpoint,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got.WorktreeOwned {
		t.Fatalf("switched worktree branch was accepted: %#v", got)
	}
}

func TestDurableRecoveryObservationSourceDetectsRepositoryIdentityChange(
	t *testing.T,
) {
	t.Parallel()

	repositoryPath := createRecoveryObservationRepository(t)
	discovered, err := workspace.DiscoverRepository(
		t.Context(),
		repositoryPath,
		workspace.ExecRunner{},
	)
	if err != nil {
		t.Fatal(err)
	}
	input := recoveryClassificationFixture(t)
	metadata := &recoveryMetadataStub{
		repository: storage.Repository{
			ID:            input.Checkpoint.RepositoryID,
			CanonicalPath: discovered.CanonicalRoot,
			GitIdentity:   "git-root-sha256:" + recoverySHA('f'),
		},
		binding: storage.WorktreeBinding{
			TaskID:       input.Checkpoint.TaskID,
			RepositoryID: input.Checkpoint.RepositoryID,
			Revision:     input.Checkpoint.WorktreeBindingRevision,
		},
		replay: storage.TaskReplay{
			EventCount: input.Checkpoint.LastDurableEventSequence + 1,
		},
	}
	source, err := NewDurableRecoveryObservationSource(
		metadata,
		&recoveryWorktreeFactsStub{facts: RecoveryWorktreeFacts{
			Exists: true, Owned: true,
			RepositoryID:    input.Checkpoint.RepositoryID,
			BindingRevision: input.Checkpoint.WorktreeBindingRevision,
			HeadRevision:    input.Checkpoint.WorktreeHead,
			DirtyFiles:      input.Checkpoint.DirtyFiles,
			DiffSHA256:      input.Checkpoint.DiffSHA256,
		}},
		&recoveryCompatibilityStub{facts: RecoveryCompatibilityFacts{
			Policy: input.Checkpoint.Policy, Provider: input.Checkpoint.Provider,
			Tools: input.Checkpoint.Tools,
		}},
		&recoveryActionFactsStub{},
		workspace.ExecRunner{},
	)
	if err != nil {
		t.Fatal(err)
	}
	got, err := source.ObserveCheckpointRecovery(
		t.Context(),
		storage.RecoveryCheckpointCandidate{
			CheckpointID: &input.CheckpointID,
			TaskID:       input.Checkpoint.TaskID,
			RunID:        input.Checkpoint.RunID,
		},
		input.Checkpoint,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got.RepositoryIdentityMatches {
		t.Fatalf("changed repository identity was accepted: %#v", got)
	}
}

type recoveryMetadataStub struct {
	repository storage.Repository
	binding    storage.WorktreeBinding
	replay     storage.TaskReplay
}

func (stub *recoveryMetadataStub) GetRepository(
	context.Context,
	domain.RepositoryID,
) (storage.Repository, error) {
	return stub.repository, nil
}

func (stub *recoveryMetadataStub) GetWorktreeBinding(
	context.Context,
	domain.TaskID,
) (storage.WorktreeBinding, error) {
	return stub.binding, nil
}

func (stub *recoveryMetadataStub) ReplayTask(
	context.Context,
	domain.TaskID,
) (storage.TaskReplay, error) {
	return stub.replay, nil
}

type recoveryWorktreeFactsStub struct {
	facts RecoveryWorktreeFacts
}

func (stub *recoveryWorktreeFactsStub) ObserveRecoveryWorktree(
	context.Context,
	storage.WorktreeBinding,
	checkpoint.Snapshot,
) (RecoveryWorktreeFacts, error) {
	return stub.facts, nil
}

type recoveryCompatibilityStub struct {
	facts RecoveryCompatibilityFacts
}

func (stub *recoveryCompatibilityStub) ObserveRecoveryCompatibility(
	context.Context,
	domain.TaskID,
	domain.RunID,
) (RecoveryCompatibilityFacts, error) {
	return stub.facts, nil
}

type recoveryActionFactsStub struct {
	facts RecoveryActionFacts
}

func (stub *recoveryActionFactsStub) ObserveRecoveryActions(
	context.Context,
	domain.TaskID,
	domain.RunID,
	uint64,
) (RecoveryActionFacts, error) {
	return stub.facts, nil
}

func createRecoveryObservationRepository(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "repository")
	runRecoveryObservationGit(t, "", "init", path)
	runRecoveryObservationGit(t, path, "config", "user.name", "Codeflux Test")
	runRecoveryObservationGit(
		t,
		path,
		"config",
		"user.email",
		"codeflux@example.invalid",
	)
	runRecoveryObservationGit(t, path, "commit", "--allow-empty", "-m", "initial")
	absolute, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(absolute)
}

func runRecoveryObservationGit(
	t *testing.T,
	directory string,
	arguments ...string,
) {
	t.Helper()
	command := exec.Command("git", arguments...)
	if directory != "" {
		command.Dir = directory
	}
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", arguments, err, output)
	}
}
