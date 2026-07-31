package gitwork

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAcceptTaskPatchAppliesExactReviewedDiffAndPreservesUnrelatedPrimaryChanges(t *testing.T) {
	t.Parallel()

	service, _, repository, taskID, binding := createWorktreeFixture(t, 140)
	service.SetEditEventRecorder(&memoryEditRecorder{})
	before, err := ReadFileAtRevision(t.Context(), binding, "main.go")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyEditBatch(t.Context(), ApplyEditBatchInput{
		TaskID: taskID,
		Mutations: []FileMutation{
			{
				Operation: MutationUpdate, Path: "main.go",
				Content:        []byte("package main\n\nconst Accepted = true\n"),
				ExpectedSHA256: before.SHA256,
			},
			{
				Operation: MutationCreate, Path: "accepted.go",
				Content:      []byte("package main\n\nconst Added = true\n"),
				ExpectAbsent: true,
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(repository, "unrelated.txt"), "user change\n")
	diff, err := service.GetTaskDiff(t.Context(), TaskDiffQuery{TaskID: taskID})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.AcceptTaskChange(t.Context(), AcceptTaskChangeInput{
		TaskID: taskID, RepositoryPath: repository,
		ExpectedDiffIdentity: diff.Identity, Mode: AcceptancePatch,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.PatchApplied || result.BaseRevision != binding.BaseRevision ||
		result.DiffIdentity != diff.Identity {
		t.Fatalf("acceptance result = %#v", result)
	}
	accepted, err := os.ReadFile(filepath.Join(repository, "main.go"))
	if err != nil || !bytes.Contains(accepted, []byte("Accepted")) {
		t.Fatalf("primary accepted content = %q, %v", accepted, err)
	}
	if _, err := os.Stat(filepath.Join(repository, "accepted.go")); err != nil {
		t.Fatalf("accepted created file missing: %v", err)
	}
	unrelated, err := os.ReadFile(filepath.Join(repository, "unrelated.txt"))
	if err != nil || string(unrelated) != "user change\n" {
		t.Fatalf("unrelated primary change = %q, %v", unrelated, err)
	}
	report, err := service.VerifyTaskWorktree(t.Context(), taskID)
	if err != nil || !report.Dirty {
		t.Fatalf("patch acceptance unexpectedly changed task branch: %#v, %v", report, err)
	}
}

func TestAcceptTaskCommitUsesExplicitAuthorAndRejectsStaleReview(t *testing.T) {
	t.Parallel()

	service, _, repository, taskID, binding := createWorktreeFixture(t, 150)
	service.SetEditEventRecorder(&memoryEditRecorder{})
	before, err := ReadFileAtRevision(t.Context(), binding, "main.go")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyEditBatch(t.Context(), ApplyEditBatchInput{
		TaskID: taskID,
		Mutations: []FileMutation{{
			Operation: MutationUpdate, Path: "main.go",
			Content:        []byte("package main\n\nconst ReviewOne = true\n"),
			ExpectedSHA256: before.SHA256,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	stale, err := service.GetTaskDiff(t.Context(), TaskDiffQuery{TaskID: taskID})
	if err != nil {
		t.Fatal(err)
	}
	current, err := ReadFileAtRevision(t.Context(), binding, "main.go")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyEditBatch(t.Context(), ApplyEditBatchInput{
		TaskID: taskID,
		Mutations: []FileMutation{{
			Operation: MutationUpdate, Path: "main.go",
			Content:        []byte("package main\n\nconst ReviewTwo = true\n"),
			ExpectedSHA256: current.SHA256,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AcceptTaskChange(t.Context(), AcceptTaskChangeInput{
		TaskID: taskID, RepositoryPath: repository,
		ExpectedDiffIdentity: stale.Identity, Mode: AcceptanceCommit,
		AuthorName: "Codeflux User", AuthorEmail: "user@example.invalid",
		CommitMessage: "Accept task change",
	}); !errors.Is(err, ErrEditConflict) {
		t.Fatalf("stale review error = %v", err)
	}

	reviewed, err := service.GetTaskDiff(t.Context(), TaskDiffQuery{TaskID: taskID})
	if err != nil {
		t.Fatal(err)
	}
	acceptedInput := AcceptTaskChangeInput{
		TaskID: taskID, RepositoryPath: repository,
		ExpectedDiffIdentity: reviewed.Identity, Mode: AcceptanceCommit,
		AcceptanceReference: strings.Repeat("a", 64),
		AuthorName:          "Codeflux User", AuthorEmail: "user@example.invalid",
		CommitMessage: "Accept task change",
	}
	result, err := service.AcceptTaskChange(t.Context(), acceptedInput)
	if err != nil {
		t.Fatal(err)
	}
	if result.CommitRevision == binding.BaseRevision || result.PatchApplied {
		t.Fatalf("commit acceptance result = %#v", result)
	}
	replayed, err := service.AcceptTaskChange(t.Context(), acceptedInput)
	if err != nil || replayed.CommitRevision != result.CommitRevision || replayed.DiffIdentity != reviewed.Identity {
		t.Fatalf("idempotent accepted commit replay = %#v, %v", replayed, err)
	}
	author := runGit(
		t,
		binding.WorktreePath,
		"show",
		"-s",
		"--format=%an <%ae>",
		result.CommitRevision,
	)
	if author != "Codeflux User <user@example.invalid>" {
		t.Fatalf("accepted commit author = %q", author)
	}
	primary, err := os.ReadFile(filepath.Join(repository, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(primary), "ReviewTwo") {
		t.Fatal("commit-only acceptance modified the primary worktree")
	}
	report, err := service.VerifyTaskWorktree(t.Context(), taskID)
	if err != nil || report.Dirty || !report.HeadMatches {
		t.Fatalf("accepted task branch verification = %#v, %v", report, err)
	}
}

func TestAcceptTaskBothCommitsAndApplies(t *testing.T) {
	t.Parallel()

	service, _, repository, taskID, binding := createWorktreeFixture(t, 160)
	service.SetEditEventRecorder(&memoryEditRecorder{})
	before, err := ReadFileAtRevision(t.Context(), binding, "main.go")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyEditBatch(t.Context(), ApplyEditBatchInput{
		TaskID: taskID,
		Mutations: []FileMutation{{
			Operation: MutationUpdate, Path: "main.go",
			Content:        []byte("package main\n\nconst Both = true\n"),
			ExpectedSHA256: before.SHA256,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	diff, err := service.GetTaskDiff(t.Context(), TaskDiffQuery{TaskID: taskID})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.AcceptTaskChange(t.Context(), AcceptTaskChangeInput{
		TaskID: taskID, RepositoryPath: repository,
		ExpectedDiffIdentity: diff.Identity, Mode: AcceptanceBoth,
		AuthorName: "Codeflux User", AuthorEmail: "user@example.invalid",
		CommitMessage: "Accept and apply task",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.PatchApplied || result.CommitRevision == binding.BaseRevision {
		t.Fatalf("combined acceptance = %#v", result)
	}
	content, err := os.ReadFile(filepath.Join(repository, "main.go"))
	if err != nil || !bytes.Contains(content, []byte("Both")) {
		t.Fatalf("combined accepted content = %q, %v", content, err)
	}
}

func TestWorktreeAndAcceptanceDoNotRunRepositoryHooks(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX hook fixture")
	}

	base := t.TempDir()
	repository := filepath.Join(base, "repository")
	head := initializeGitRepository(t, repository)
	marker := filepath.Join(base, "hook-ran")
	hooks := filepath.Join(repository, ".git", "hooks")
	for _, name := range []string{"post-checkout", "post-commit"} {
		hook := filepath.Join(hooks, name)
		if err := os.WriteFile(
			hook,
			[]byte("#!/bin/sh\nprintf ran > "+marker+"\n"),
			0o700,
		); err != nil {
			t.Fatal(err)
		}
	}
	store := newMemoryBindingStore()
	service := newTestService(
		t,
		filepath.Join(base, "worktrees"),
		store,
		bytes.Repeat([]byte{170}, 64),
	)
	taskID := fixtureTaskID(t, 170)
	created, err := service.CreateTaskWorktree(t.Context(), CreateWorktreeInput{
		TaskID: taskID, RepositoryID: fixtureRepositoryID(t, 171),
		RepositoryPath: repository, BaseRevision: head,
	})
	if err != nil {
		t.Fatal(err)
	}
	service.SetEditEventRecorder(&memoryEditRecorder{})
	before, err := ReadFileAtRevision(t.Context(), created.Binding, "main.go")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyEditBatch(t.Context(), ApplyEditBatchInput{
		TaskID: taskID,
		Mutations: []FileMutation{{
			Operation: MutationUpdate, Path: "main.go",
			Content:        []byte("package main\n\nconst NoHooks = true\n"),
			ExpectedSHA256: before.SHA256,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	diff, err := service.GetTaskDiff(t.Context(), TaskDiffQuery{TaskID: taskID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.AcceptTaskChange(t.Context(), AcceptTaskChangeInput{
		TaskID: taskID, RepositoryPath: repository,
		ExpectedDiffIdentity: diff.Identity, Mode: AcceptanceCommit,
		AuthorName: "Codeflux User", AuthorEmail: "user@example.invalid",
		CommitMessage: "Verify disabled hooks",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("repository hook executed: %v", err)
	}
}
