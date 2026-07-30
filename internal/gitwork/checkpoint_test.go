package gitwork

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
)

func TestCheckpointRollbackAfterSeveralEditBatchesPreservesLineage(t *testing.T) {
	t.Parallel()

	service, _, _, taskID, binding := createWorktreeFixture(t, 120)
	service.SetEditEventRecorder(&memoryEditRecorder{})
	checkpoints := newMemoryCheckpointStore()
	service.SetCheckpointRepository(checkpoints)

	main, err := ReadFileAtRevision(t.Context(), binding, "main.go")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyEditBatch(t.Context(), ApplyEditBatchInput{
		TaskID: taskID,
		Mutations: []FileMutation{{
			Operation: MutationUpdate, Path: "main.go",
			Content:        []byte("package main\n\nconst First = true\n"),
			ExpectedSHA256: main.SHA256,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	firstID := fixtureCheckpointID(t, 121)
	first, err := service.CreateCheckpoint(t.Context(), CreateCheckpointInput{
		ID: firstID, TaskID: taskID, EventSequence: 10,
		IdempotencyKey: "checkpoint-first",
	})
	if err != nil {
		t.Fatal(err)
	}
	retry, err := service.CreateCheckpoint(t.Context(), CreateCheckpointInput{
		ID: firstID, TaskID: taskID, EventSequence: 10,
		IdempotencyKey: "checkpoint-first",
	})
	if err != nil || retry != first {
		t.Fatalf("checkpoint retry = %#v, %v; want %#v", retry, err, first)
	}

	if _, err := service.ApplyEditBatch(t.Context(), ApplyEditBatchInput{
		TaskID: taskID,
		Mutations: []FileMutation{{
			Operation: MutationCreate, Path: "second.go",
			Content:      []byte("package main\n\nconst Second = true\n"),
			ExpectAbsent: true,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	secondID := fixtureCheckpointID(t, 122)
	second, err := service.CreateCheckpoint(t.Context(), CreateCheckpointInput{
		ID: secondID, TaskID: taskID, EventSequence: 20,
		IdempotencyKey: "checkpoint-second",
	})
	if err != nil {
		t.Fatal(err)
	}
	current, err := service.bindings.GetWorktreeBinding(t.Context(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	main, err = ReadFileAtRevision(t.Context(), current, "main.go")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyEditBatch(t.Context(), ApplyEditBatchInput{
		TaskID: taskID,
		Mutations: []FileMutation{{
			Operation: MutationUpdate, Path: "main.go",
			Content:        []byte("package main\n\nconst Third = true\n"),
			ExpectedSHA256: main.SHA256,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RestoreCheckpoint(t.Context(), RestoreCheckpointInput{
		TaskID: taskID, CheckpointID: firstID,
	}); !errors.Is(err, ErrApprovalRequired) {
		t.Fatalf("unapproved rollback error = %v", err)
	}
	restoredBinding, err := service.RestoreCheckpoint(
		t.Context(),
		RestoreCheckpointInput{
			TaskID: taskID, CheckpointID: firstID,
			DiscardCurrentApproved: true,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if restoredBinding.HeadRevision != first.RepositoryRevision {
		t.Fatalf("restored HEAD = %q, want %q", restoredBinding.HeadRevision, first.RepositoryRevision)
	}
	content, err := os.ReadFile(filepath.Join(binding.WorktreePath, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "First") ||
		strings.Contains(string(content), "Third") {
		t.Fatalf("restored main.go = %s", content)
	}
	if _, err := os.Stat(filepath.Join(binding.WorktreePath, "second.go")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("post-checkpoint file remains: %v", err)
	}
	if pinned := runGit(
		t,
		binding.WorktreePath,
		"rev-parse",
		checkpointReferencePrefix+secondID.String(),
	); pinned != second.RepositoryRevision {
		t.Fatalf("later checkpoint lineage was lost: %q", pinned)
	}
}

func TestCheckpointPersistenceFailureRestoresPendingEdits(t *testing.T) {
	t.Parallel()

	service, _, _, taskID, binding := createWorktreeFixture(t, 130)
	service.SetEditEventRecorder(&memoryEditRecorder{})
	checkpoints := newMemoryCheckpointStore()
	checkpoints.createErr = errors.New("fixture checkpoint persistence failure")
	service.SetCheckpointRepository(checkpoints)
	main, err := ReadFileAtRevision(t.Context(), binding, "main.go")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyEditBatch(t.Context(), ApplyEditBatchInput{
		TaskID: taskID,
		Mutations: []FileMutation{{
			Operation: MutationUpdate, Path: "main.go",
			Content:        []byte("package main\n\nconst Pending = true\n"),
			ExpectedSHA256: main.SHA256,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	_, err = service.CreateCheckpoint(t.Context(), CreateCheckpointInput{
		ID: fixtureCheckpointID(t, 131), TaskID: taskID,
		IdempotencyKey: "checkpoint-fails",
	})
	if err == nil {
		t.Fatal("checkpoint persistence failure was ignored")
	}
	loaded, err := service.bindings.GetWorktreeBinding(t.Context(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.HeadRevision != binding.HeadRevision {
		t.Fatalf("failed checkpoint advanced durable HEAD: %#v", loaded)
	}
	report, err := service.VerifyTaskWorktree(t.Context(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Dirty {
		t.Fatal("failed checkpoint did not restore pending edits")
	}
	content, err := os.ReadFile(filepath.Join(binding.WorktreePath, "main.go"))
	if err != nil || !strings.Contains(string(content), "Pending") {
		t.Fatalf("pending edit = %q, %v", content, err)
	}
}

func fixtureCheckpointID(t *testing.T, number int) domain.CheckpointID {
	t.Helper()
	id, err := domain.ParseCheckpointID("ckp_" + fixtureUUID(number))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

type memoryCheckpointStore struct {
	mu          sync.Mutex
	checkpoints map[domain.CheckpointID]storage.Checkpoint
	createErr   error
}

func newMemoryCheckpointStore() *memoryCheckpointStore {
	return &memoryCheckpointStore{
		checkpoints: make(map[domain.CheckpointID]storage.Checkpoint),
	}
}

func (store *memoryCheckpointStore) CreateCheckpoint(
	_ context.Context,
	input storage.CreateCheckpoint,
) (storage.Checkpoint, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.createErr != nil {
		return storage.Checkpoint{}, store.createErr
	}
	if existing, exists := store.checkpoints[input.ID]; exists {
		return existing, nil
	}
	checkpoint := storage.Checkpoint{
		ID: input.ID, TaskID: input.TaskID, RunID: input.RunID,
		State: input.State, RepositoryRevision: input.RepositoryRevision,
		WorktreeDiffHash: input.WorktreeDiffHash,
		EventSequence:    input.EventSequence, IdempotencyKey: input.IdempotencyKey,
	}
	store.checkpoints[input.ID] = checkpoint
	return checkpoint, nil
}

func (store *memoryCheckpointStore) GetCheckpoint(
	_ context.Context,
	id domain.CheckpointID,
) (storage.Checkpoint, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	checkpoint, exists := store.checkpoints[id]
	if !exists {
		return storage.Checkpoint{}, storage.ErrNotFound
	}
	return checkpoint, nil
}
