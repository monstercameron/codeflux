package gitwork

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestResolveTaskPathRejectsTraversalAbsoluteAndSymlinkEscapes(t *testing.T) {
	t.Parallel()

	service, _, _, _, binding := createWorktreeFixture(t, 60)
	_ = service
	for _, unsafe := range []string{
		"../outside.go",
		"nested/../../outside.go",
		"/absolute.go",
		`C:\outside.go`,
		`nested\outside.go`,
		".",
		" main.go",
	} {
		if _, err := ResolveTaskPath(binding, unsafe); !errors.Is(err, ErrUnsafeTaskPath) {
			t.Errorf("ResolveTaskPath(%q) error = %v", unsafe, err)
		}
	}
	outside := filepath.Join(t.TempDir(), "outside.go")
	writeFile(t, outside, "outside")
	link := filepath.Join(binding.WorktreePath, "escape.go")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	if _, err := ResolveTaskPath(binding, "escape.go"); !errors.Is(err, ErrUnsafeTaskPath) {
		t.Fatalf("symlink escape error = %v", err)
	}
	nestedOutside := t.TempDir()
	nestedLink := filepath.Join(binding.WorktreePath, "nested")
	if err := os.Symlink(nestedOutside, nestedLink); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveTaskPath(binding, "nested/file.go"); !errors.Is(err, ErrUnsafeTaskPath) {
		t.Fatalf("parent symlink escape error = %v", err)
	}
}

func TestApplyEditBatchSupportsCreateUpdateRenameDeleteAndPreservesMetadata(t *testing.T) {
	t.Parallel()

	service, _, _, taskID, binding := createWorktreeFixture(t, 70)
	recorder := &memoryEditRecorder{}
	service.SetEditEventRecorder(recorder)

	mainPath := filepath.Join(binding.WorktreePath, "main.go")
	writeFile(t, mainPath, "package main\r\n\r\nfunc main() {}\r\n")
	if err := os.Chmod(mainPath, 0o750); err != nil {
		t.Fatal(err)
	}
	before, err := ReadFileAtRevision(t.Context(), binding, "main.go")
	if err != nil {
		t.Fatal(err)
	}
	updateContent := []byte("package main\n\nfunc main() { println(\"updated\") }\n")
	result, err := service.ApplyEditBatch(t.Context(), ApplyEditBatchInput{
		TaskID: taskID,
		Mutations: []FileMutation{
			{
				Operation: MutationUpdate, Path: "main.go",
				Content: updateContent, ExpectedSHA256: before.SHA256,
			},
			{
				Operation: MutationCreate, Path: "created.go",
				Content:      []byte("package main\n\nconst Created = true\n"),
				ExpectAbsent: true,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary.Updated != 1 || result.Summary.Created != 1 ||
		result.Summary.FileCount != 2 || result.Summary.BatchSHA256 == "" {
		t.Fatalf("edit summary = %#v", result.Summary)
	}
	updated, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(updated, []byte("\r\n")) ||
		bytes.Contains(bytes.ReplaceAll(updated, []byte("\r\n"), nil), []byte("\n")) {
		t.Fatalf("CRLF style was not preserved: %q", updated)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(mainPath)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o750 {
			t.Fatalf("mode = %o, want 750", info.Mode().Perm())
		}
	}

	created, err := ReadFileAtRevision(t.Context(), binding, "created.go")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyEditBatch(t.Context(), ApplyEditBatchInput{
		TaskID: taskID,
		Mutations: []FileMutation{{
			Operation: MutationRename, Path: "created.go", NewPath: "renamed.go",
			ExpectedSHA256: created.SHA256,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	renamed, err := ReadFileAtRevision(t.Context(), binding, "renamed.go")
	if err != nil {
		t.Fatal(err)
	}
	if !renamed.Exists {
		t.Fatal("renamed file is missing")
	}
	if _, err := service.ApplyEditBatch(t.Context(), ApplyEditBatchInput{
		TaskID: taskID,
		Mutations: []FileMutation{{
			Operation: MutationDelete, Path: "renamed.go",
			ExpectedSHA256: renamed.SHA256,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(binding.WorktreePath, "renamed.go")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted file remains: %v", err)
	}
	if recorder.count() != 3 {
		t.Fatalf("recorded edit event count = %d, want 3", recorder.count())
	}
}

func TestApplyEditBatchRejectsConcurrentChangeBinaryAndBroadDelete(t *testing.T) {
	t.Parallel()

	service, _, _, taskID, binding := createWorktreeFixture(t, 80)
	service.SetEditEventRecorder(&memoryEditRecorder{})
	before, err := ReadFileAtRevision(t.Context(), binding, "main.go")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(binding.WorktreePath, "main.go"), "package main\n\nconst UserEdit = true\n")
	if _, err := service.ApplyEditBatch(t.Context(), ApplyEditBatchInput{
		TaskID: taskID,
		Mutations: []FileMutation{{
			Operation: MutationUpdate, Path: "main.go",
			Content: []byte("package main\n"), ExpectedSHA256: before.SHA256,
		}},
	}); !errors.Is(err, ErrEditConflict) {
		t.Fatalf("concurrent edit error = %v", err)
	}
	content, err := os.ReadFile(filepath.Join(binding.WorktreePath, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(content, []byte("UserEdit")) {
		t.Fatal("concurrent user edit was overwritten")
	}

	if _, err := service.ApplyEditBatch(t.Context(), ApplyEditBatchInput{
		TaskID: taskID,
		Mutations: []FileMutation{{
			Operation: MutationCreate, Path: "binary.dat",
			Content: []byte{0, 1, 2}, ExpectAbsent: true,
		}},
	}); !errors.Is(err, ErrUnsupportedBinary) {
		t.Fatalf("binary create error = %v", err)
	}
	writeFile(t, filepath.Join(binding.WorktreePath, "large.txt"), strings.Repeat("x", largeDeleteBytes))
	large, err := ReadFileAtRevision(t.Context(), binding, "large.txt")
	if err != nil {
		t.Fatal(err)
	}
	deleteInput := ApplyEditBatchInput{
		TaskID: taskID,
		Mutations: []FileMutation{{
			Operation: MutationDelete, Path: "large.txt",
			ExpectedSHA256: large.SHA256,
		}},
	}
	if _, err := service.ApplyEditBatch(t.Context(), deleteInput); !errors.Is(err, ErrApprovalRequired) {
		t.Fatalf("large delete error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(binding.WorktreePath, "large.txt")); err != nil {
		t.Fatalf("unapproved delete changed file: %v", err)
	}
	deleteInput.LargeDeleteApproved = true
	if _, err := service.ApplyEditBatch(t.Context(), deleteInput); err != nil {
		t.Fatal(err)
	}
}

func TestApplyEditBatchRollsBackWhenEventPersistenceFails(t *testing.T) {
	t.Parallel()

	service, _, _, taskID, binding := createWorktreeFixture(t, 90)
	recorder := &memoryEditRecorder{err: errors.New("fixture event failure")}
	service.SetEditEventRecorder(recorder)
	before, err := ReadFileAtRevision(t.Context(), binding, "main.go")
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.ApplyEditBatch(t.Context(), ApplyEditBatchInput{
		TaskID: taskID,
		Mutations: []FileMutation{
			{
				Operation: MutationUpdate, Path: "main.go",
				Content:        []byte("package main\n\nconst Changed = true\n"),
				ExpectedSHA256: before.SHA256,
			},
			{
				Operation: MutationCreate, Path: "created.go",
				Content: []byte("package main\n"), ExpectAbsent: true,
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "record edit summary") {
		t.Fatalf("event failure error = %v", err)
	}
	restored, err := ReadFileAtRevision(t.Context(), binding, "main.go")
	if err != nil {
		t.Fatal(err)
	}
	if restored.SHA256 != before.SHA256 {
		t.Fatalf("updated file was not rolled back: %s != %s", restored.SHA256, before.SHA256)
	}
	if _, err := os.Stat(filepath.Join(binding.WorktreePath, "created.go")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("created file was not rolled back: %v", err)
	}
}

type memoryEditRecorder struct {
	mu      sync.Mutex
	entries []RedactedEditSummary
	err     error
}

func (recorder *memoryEditRecorder) RecordEditSummary(
	_ context.Context,
	summary RedactedEditSummary,
) error {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if recorder.err != nil {
		return recorder.err
	}
	recorder.entries = append(recorder.entries, summary)
	return nil
}

func (recorder *memoryEditRecorder) count() int {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return len(recorder.entries)
}
