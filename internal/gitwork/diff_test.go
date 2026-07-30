package gitwork

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
)

func TestTaskDiffIsDeterministicRelativeClassifiedAndAttributed(t *testing.T) {
	t.Parallel()

	service, _, _, taskID, binding := createWorktreeFixture(t, 110)
	service.SetEditEventRecorder(&memoryEditRecorder{})
	main, err := ReadFileAtRevision(t.Context(), binding, "main.go")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyEditBatch(t.Context(), ApplyEditBatchInput{
		TaskID: taskID,
		Mutations: []FileMutation{
			{
				Operation: MutationUpdate, Path: "main.go",
				Content:        []byte("package main\n\nfunc main() { println(\"changed\") }\n"),
				ExpectedSHA256: main.SHA256,
			},
			{
				Operation: MutationCreate, Path: "main_test.go",
				Content:      []byte("package main\n\nfunc TestMain() {}\n"),
				ExpectAbsent: true,
			},
			{
				Operation: MutationCreate, Path: "config.yaml",
				Content:      []byte("enabled: true\n"),
				ExpectAbsent: true,
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	eventID, err := domain.NewEventID()
	if err != nil {
		t.Fatal(err)
	}
	validationID, err := domain.NewValidationID()
	if err != nil {
		t.Fatal(err)
	}
	query := TaskDiffQuery{
		TaskID:        taskID,
		ApprovedPaths: []string{"main.go", "main_test.go"},
		AttributionByPath: map[string]DiffAttribution{
			"main.go": {
				EventIDs:      []domain.EventID{eventID},
				ValidationIDs: []domain.ValidationID{validationID},
			},
		},
	}
	first, err := service.GetTaskDiff(t.Context(), query)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.GetTaskDiff(t.Context(), query)
	if err != nil {
		t.Fatal(err)
	}
	if first.Identity == "" || first.Identity != second.Identity ||
		first.UnifiedDiff != second.UnifiedDiff {
		t.Fatalf("diff identity changed: %q/%q", first.Identity, second.Identity)
	}
	if first.FilesChanged != 3 || first.Insertions == 0 || first.Deletions == 0 {
		t.Fatalf("diff counts = %#v", first)
	}
	if strings.Contains(first.UnifiedDiff, binding.WorktreePath) ||
		!strings.Contains(first.UnifiedDiff, "a/main.go") ||
		!strings.Contains(first.UnifiedDiff, "b/main_test.go") {
		t.Fatalf("diff paths are not repository-relative:\n%s", first.UnifiedDiff)
	}
	mainFile := findDiffFile(t, first.Files, "main.go")
	if mainFile.Category != "source" ||
		len(mainFile.Attribution.EventIDs) != 1 ||
		len(mainFile.Attribution.ValidationIDs) != 1 {
		t.Fatalf("main diff = %#v", mainFile)
	}
	if testFile := findDiffFile(t, first.Files, "main_test.go"); testFile.Category != "test" {
		t.Fatalf("test diff = %#v", testFile)
	}
	config := findDiffFile(t, first.Files, "config.yaml")
	if config.Category != "configuration" || !config.OutsideApprovedScope {
		t.Fatalf("configuration diff = %#v", config)
	}
	if !strings.Contains(first.Summary, "Review the unified diff.") {
		t.Fatalf("summary substitutes for source review: %s", first.Summary)
	}
	if _, err := (ExecRunner{}).Run(
		t.Context(),
		binding.WorktreePath,
		"git",
		"diff",
		"--cached",
		"--quiet",
	); err != nil {
		t.Fatalf("diff inspection modified the real Git index: %v", err)
	}
}

func TestTaskDiffDetectsRenameDeleteBinaryAndFormattingChurn(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	repository := filepath.Join(base, "repository")
	_ = initializeGitRepository(t, repository)
	writeFile(t, filepath.Join(repository, "old.go"), "package main\n\nconst Old = true\n")
	writeFile(t, filepath.Join(repository, "delete.txt"), "delete me\n")
	runGit(t, repository, "add", "old.go", "delete.txt")
	runGit(t, repository, "-c", "user.name=Codeflux Test",
		"-c", "user.email=codeflux@example.invalid", "commit", "-m", "add tracked files")
	head := runGit(t, repository, "rev-parse", "HEAD")

	store := newMemoryBindingStore()
	service := newTestService(
		t,
		filepath.Join(base, "worktrees"),
		store,
		bytes.Repeat([]byte{111}, 64),
	)
	taskID := fixtureTaskID(t, 111)
	result, err := service.CreateTaskWorktree(t.Context(), CreateWorktreeInput{
		TaskID: taskID, RepositoryID: fixtureRepositoryID(t, 112),
		RepositoryPath: repository, BaseRevision: head,
	})
	if err != nil {
		t.Fatal(err)
	}
	binding := result.Binding
	if err := os.Rename(
		filepath.Join(binding.WorktreePath, "old.go"),
		filepath.Join(binding.WorktreePath, "new.go"),
	); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(binding.WorktreePath, "delete.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(binding.WorktreePath, "binary.dat"),
		[]byte{0, 1, 2, 3},
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	writeFile(
		t,
		filepath.Join(binding.WorktreePath, "churn.go"),
		"package main\n"+strings.Repeat("const Value = 1\n", 220),
	)
	diff, err := service.GetTaskDiff(t.Context(), TaskDiffQuery{TaskID: taskID})
	if err != nil {
		t.Fatal(err)
	}
	renamed := findDiffFile(t, diff.Files, "new.go")
	if renamed.PreviousPath != "old.go" {
		t.Fatalf("rename diff = %#v", renamed)
	}
	deleted := findDiffFile(t, diff.Files, "delete.txt")
	if deleted.DeletedLines == 0 {
		t.Fatalf("delete diff = %#v", deleted)
	}
	binary := findDiffFile(t, diff.Files, "binary.dat")
	if !binary.Binary {
		t.Fatalf("binary diff = %#v", binary)
	}
	churn := findDiffFile(t, diff.Files, "churn.go")
	if !churn.FormattingChurn {
		t.Fatalf("formatting churn diff = %#v", churn)
	}
	if !strings.Contains(diff.UnifiedDiff, "rename from old.go") ||
		!strings.Contains(diff.UnifiedDiff, "deleted file mode") ||
		(!strings.Contains(diff.UnifiedDiff, "Binary files") &&
			!strings.Contains(diff.UnifiedDiff, "GIT binary patch")) {
		t.Fatalf("unified rename/delete/binary diff is incomplete:\n%s", diff.UnifiedDiff)
	}
}

func findDiffFile(t *testing.T, files []DiffFile, path string) DiffFile {
	t.Helper()
	for _, file := range files {
		if file.Path == path {
			return file
		}
	}
	t.Fatalf("diff file %q not found in %#v", path, files)
	return DiffFile{}
}
