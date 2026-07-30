package gitwork

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
)

func TestTaskWorktreeCreationPreservesDirtyPrimaryAndSurvivesRestart(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	repository := filepath.Join(base, "repository")
	worktreeRoot := filepath.Join(base, "worktrees")
	head := initializeGitRepository(t, repository)
	writeFile(t, filepath.Join(repository, "main.go"), "package main\n\nconst Dirty = true\n")

	store := newMemoryBindingStore()
	service := newTestService(t, worktreeRoot, store, bytes.Repeat([]byte{1}, 64))
	taskID := fixtureTaskID(t, 1)
	repositoryID := fixtureRepositoryID(t, 2)
	result, err := service.CreateTaskWorktree(t.Context(), CreateWorktreeInput{
		TaskID:         taskID,
		RepositoryID:   repositoryID,
		RepositoryPath: repository,
		BaseRevision:   head,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.PrimaryDirty {
		t.Fatal("dirty primary worktree was not reported")
	}
	expectedPath, err := service.WorktreePath(repositoryID, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Binding.WorktreePath != expectedPath {
		t.Fatalf("worktree path = %q, want %q", result.Binding.WorktreePath, expectedPath)
	}
	content, err := os.ReadFile(filepath.Join(expectedPath, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(content, []byte("Dirty")) ||
		!bytes.Contains(content, []byte("func main()")) {
		t.Fatalf("task worktree inherited dirty primary content: %s", content)
	}
	report, err := service.VerifyTaskWorktree(t.Context(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	if !report.PathPresent || !report.RootMatches || !report.BranchMatches ||
		!report.HeadMatches || report.Dirty {
		t.Fatalf("initial verification = %#v", report)
	}

	writeFile(t, filepath.Join(expectedPath, "main.go"), "package main\n\nfunc main() { println(\"task\") }\n")
	restarted := newTestService(t, worktreeRoot, store, bytes.Repeat([]byte{2}, 64))
	report, err = restarted.VerifyTaskWorktree(t.Context(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Dirty || len(report.ChangedPaths) != 1 ||
		report.ChangedPaths[0] != "main.go" {
		t.Fatalf("restart verification = %#v", report)
	}
	primary, err := os.ReadFile(filepath.Join(repository, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(primary) != "package main\n\nconst Dirty = true\n" {
		t.Fatalf("primary worktree changed: %s", primary)
	}
}

func TestTaskWorktreeRetriesBranchCollisionAndCleansFailedRecord(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	repository := filepath.Join(base, "repository")
	worktreeRoot := filepath.Join(base, "worktrees")
	head := initializeGitRepository(t, repository)
	taskID := fixtureTaskID(t, 10)
	repositoryID := fixtureRepositoryID(t, 11)
	firstEntropy := bytes.Repeat([]byte{0}, 8)
	secondEntropy := bytes.Repeat([]byte{1}, 8)
	collisionBranch := expectedBranchName(taskID, firstEntropy)
	runGit(t, repository, "branch", collisionBranch, head)

	store := newMemoryBindingStore()
	service := newTestService(
		t,
		worktreeRoot,
		store,
		append(firstEntropy, secondEntropy...),
	)
	result, err := service.CreateTaskWorktree(t.Context(), CreateWorktreeInput{
		TaskID: taskID, RepositoryID: repositoryID,
		RepositoryPath: repository, BaseRevision: head,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Binding.BranchName == collisionBranch {
		t.Fatalf("colliding branch was reused: %s", collisionBranch)
	}

	failingStore := newMemoryBindingStore()
	failingStore.createErr = errors.New("fixture persistence failure")
	failingTaskID := fixtureTaskID(t, 12)
	failing := newTestService(
		t,
		filepath.Join(base, "failed-worktrees"),
		failingStore,
		bytes.Repeat([]byte{3}, 64),
	)
	_, err = failing.CreateTaskWorktree(t.Context(), CreateWorktreeInput{
		TaskID: failingTaskID, RepositoryID: repositoryID,
		RepositoryPath: repository, BaseRevision: head,
	})
	if err == nil {
		t.Fatal("persistence failure was ignored")
	}
	failedPath, pathErr := failing.WorktreePath(repositoryID, failingTaskID)
	if pathErr != nil {
		t.Fatal(pathErr)
	}
	if _, statErr := os.Stat(failedPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("failed creation left worktree path: %v", statErr)
	}
}

func TestTaskWorktreeDetectsExternalCommitDeletionAndMovement(t *testing.T) {
	t.Parallel()

	t.Run("external commit", func(t *testing.T) {
		service, store, repository, taskID, binding := createWorktreeFixture(t, 20)
		_ = store
		writeFile(t, filepath.Join(binding.WorktreePath, "main.go"), "package main\n\nconst External = true\n")
		runGit(t, binding.WorktreePath, "add", "main.go")
		runGit(t, binding.WorktreePath, "-c", "user.name=External User",
			"-c", "user.email=external@example.invalid", "commit", "-m", "external")
		report, err := service.VerifyTaskWorktree(t.Context(), taskID)
		if !errors.Is(err, ErrExternalCommit) || !report.RecoveryNeeded {
			t.Fatalf("external commit verification = %#v, %v", report, err)
		}
		_ = repository
	})

	t.Run("manual deletion", func(t *testing.T) {
		service, _, repository, taskID, binding := createWorktreeFixture(t, 30)
		runGit(t, repository, "worktree", "remove", "--force", binding.WorktreePath)
		report, err := service.VerifyTaskWorktree(t.Context(), taskID)
		if !errors.Is(err, ErrWorktreeMissing) || !report.RecoveryNeeded {
			t.Fatalf("deleted verification = %#v, %v", report, err)
		}
	})

	t.Run("manual movement", func(t *testing.T) {
		service, _, _, taskID, binding := createWorktreeFixture(t, 40)
		moved := binding.WorktreePath + "-moved"
		if err := os.Rename(binding.WorktreePath, moved); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(binding.WorktreePath, 0o700); err != nil {
			t.Fatal(err)
		}
		report, err := service.VerifyTaskWorktree(t.Context(), taskID)
		if !errors.Is(err, ErrWorktreeMoved) || !report.RecoveryNeeded {
			t.Fatalf("moved verification = %#v, %v", report, err)
		}
	})
}

func TestAbandonPreservesTaskWorktreeUntilExplicitCleanup(t *testing.T) {
	t.Parallel()

	service, store, repository, taskID, binding := createWorktreeFixture(t, 50)
	released, err := service.AbandonTaskWorktree(t.Context(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	if released.State != storage.WorktreeBindingReleased {
		t.Fatalf("released binding = %#v", released)
	}
	if _, err := os.Stat(binding.WorktreePath); err != nil {
		t.Fatalf("abandon removed worktree: %v", err)
	}
	if err := service.CleanupTaskWorktree(t.Context(), taskID, repository); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(binding.WorktreePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cleanup retained worktree: %v", err)
	}
	if output := runGit(t, repository, "show-ref", "--verify", "refs/heads/"+binding.BranchName); output == "" {
		t.Fatal("cleanup removed recovery branch")
	}
	loaded, err := store.GetWorktreeBinding(t.Context(), taskID)
	if err != nil || loaded.State != storage.WorktreeBindingReleased {
		t.Fatalf("durable released binding = %#v, %v", loaded, err)
	}
}

func createWorktreeFixture(
	t *testing.T,
	baseNumber int,
) (*Service, *memoryBindingStore, string, domain.TaskID, storage.WorktreeBinding) {
	t.Helper()
	base := t.TempDir()
	repository := filepath.Join(base, "repository")
	head := initializeGitRepository(t, repository)
	store := newMemoryBindingStore()
	service := newTestService(
		t,
		filepath.Join(base, "worktrees"),
		store,
		bytes.Repeat([]byte{byte(baseNumber)}, 64),
	)
	taskID := fixtureTaskID(t, baseNumber)
	result, err := service.CreateTaskWorktree(t.Context(), CreateWorktreeInput{
		TaskID: taskID, RepositoryID: fixtureRepositoryID(t, baseNumber+1),
		RepositoryPath: repository, BaseRevision: head,
	})
	if err != nil {
		t.Fatal(err)
	}
	return service, store, repository, taskID, result.Binding
}

func newTestService(
	t *testing.T,
	root string,
	store BindingRepository,
	random []byte,
) *Service {
	t.Helper()
	absolute, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(absolute, store, ExecRunner{}, bytes.NewReader(random))
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func initializeGitRepository(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	runGit(t, path, "init", "--initial-branch=main")
	writeFile(t, filepath.Join(path, "main.go"), "package main\n\nfunc main() {}\n")
	runGit(t, path, "add", "main.go")
	runGit(t, path, "-c", "user.name=Codeflux Test",
		"-c", "user.email=codeflux@example.invalid", "commit", "-m", "base")
	return runGit(t, path, "rev-parse", "HEAD")
}

func runGit(t *testing.T, directory string, arguments ...string) string {
	t.Helper()
	result, err := (ExecRunner{}).Run(t.Context(), directory, "git", arguments...)
	if err != nil {
		t.Fatalf("git %v: %v\n%s", arguments, err, result.Stderr)
	}
	return string(bytes.TrimSpace(result.Stdout))
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func expectedBranchName(taskID domain.TaskID, entropy []byte) string {
	taskPart := taskID.String()[4:]
	taskPart = strings.ReplaceAll(taskPart, "-", "")
	if len(taskPart) > 16 {
		taskPart = taskPart[len(taskPart)-16:]
	}
	return "codeflux/task/" + taskPart + "-" + fmt.Sprintf("%x", entropy)
}

func fixtureTaskID(t *testing.T, number int) domain.TaskID {
	t.Helper()
	id, err := domain.ParseTaskID("tsk_" + fixtureUUID(number))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func fixtureRepositoryID(t *testing.T, number int) domain.RepositoryID {
	t.Helper()
	id, err := domain.ParseRepositoryID("repo_" + fixtureUUID(number))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func fixtureUUID(number int) string {
	return fmt.Sprintf("01890f3c-4a00-7abc-8def-%012x", number)
}

type memoryBindingStore struct {
	mu        sync.Mutex
	bindings  map[domain.TaskID]storage.WorktreeBinding
	createErr error
}

func newMemoryBindingStore() *memoryBindingStore {
	return &memoryBindingStore{
		bindings: make(map[domain.TaskID]storage.WorktreeBinding),
	}
}

func (store *memoryBindingStore) CreateWorktreeBinding(
	_ context.Context,
	input storage.CreateWorktreeBinding,
) (storage.WorktreeBinding, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.createErr != nil {
		return storage.WorktreeBinding{}, store.createErr
	}
	if _, exists := store.bindings[input.TaskID]; exists {
		return storage.WorktreeBinding{}, storage.ErrConflict
	}
	for _, binding := range store.bindings {
		if binding.RepositoryID == input.RepositoryID &&
			binding.WorktreePath == input.WorktreePath {
			return storage.WorktreeBinding{}, storage.ErrConflict
		}
	}
	binding := storage.WorktreeBinding{
		WorkspaceID: input.WorkspaceID, TaskID: input.TaskID,
		RepositoryID: input.RepositoryID, BaseRevision: input.BaseRevision,
		HeadRevision: input.HeadRevision, BranchName: input.BranchName,
		WorktreePath: input.WorktreePath, State: storage.WorktreeBindingActive,
	}
	store.bindings[input.TaskID] = binding
	return binding, nil
}

func (store *memoryBindingStore) GetWorktreeBinding(
	_ context.Context,
	taskID domain.TaskID,
) (storage.WorktreeBinding, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	binding, exists := store.bindings[taskID]
	if !exists {
		return storage.WorktreeBinding{}, storage.ErrNotFound
	}
	return binding, nil
}

func (store *memoryBindingStore) AdvanceWorktreeBinding(
	_ context.Context,
	input storage.AdvanceWorktreeBinding,
) (storage.WorktreeBinding, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	binding, exists := store.bindings[input.TaskID]
	if !exists {
		return storage.WorktreeBinding{}, storage.ErrNotFound
	}
	if binding.Revision != input.ExpectedRevision ||
		binding.HeadRevision != input.ExpectedHead {
		return storage.WorktreeBinding{}, storage.ErrStaleRevision
	}
	binding.HeadRevision = input.HeadRevision
	binding.Revision++
	store.bindings[input.TaskID] = binding
	return binding, nil
}

func (store *memoryBindingStore) TransitionWorktreeBinding(
	_ context.Context,
	input storage.TransitionWorktreeBinding,
) (storage.WorktreeBinding, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	binding, exists := store.bindings[input.TaskID]
	if !exists {
		return storage.WorktreeBinding{}, storage.ErrNotFound
	}
	if binding.Revision != input.ExpectedRevision || binding.State != input.From {
		return storage.WorktreeBinding{}, storage.ErrStaleRevision
	}
	binding.State = input.To
	binding.Revision++
	store.bindings[input.TaskID] = binding
	return binding, nil
}
