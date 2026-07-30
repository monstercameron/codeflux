package coordinator

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/gitwork"
	"codeflux.dev/codeflux/internal/storage"
)

const recoveryHead = "1111111111111111111111111111111111111111"

func TestRecoverInvalidWorktreesMarksOnlyUncertainBindings(t *testing.T) {
	t.Parallel()

	healthyPath := canonicalRecoveryTestPath(t, t.TempDir())
	missingPath := filepath.Join(t.TempDir(), "missing")
	healthy := recoveryTestBinding(t, healthyPath)
	missing := recoveryTestBinding(t, missingPath)
	store := &recoveryTestStore{bindings: []storage.WorktreeBinding{healthy, missing}}
	runner := healthyRecoveryRunner(healthyPath, healthy.BranchName, healthy.HeadRevision)

	candidates, err := RecoverInvalidWorktrees(t.Context(), store, runner)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 {
		t.Fatalf("recovery candidates = %#v, want one", candidates)
	}
	candidate := candidates[0]
	if candidate.Binding.TaskID != missing.TaskID ||
		candidate.Binding.State != storage.WorktreeBindingRecoveryRequired ||
		candidate.Binding.Revision != missing.Revision+1 ||
		candidate.Reason != "task worktree is missing or inaccessible" {
		t.Fatalf("recovery candidate = %#v", candidate)
	}
	if len(store.marked) != 1 || store.marked[0] != missing.TaskID {
		t.Fatalf("marked tasks = %#v", store.marked)
	}
}

func TestRecoverInvalidWorktreesDetectsGitMetadataDivergence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		configure  func(string, storage.WorktreeBinding) *recoveryTestRunner
		wantReason string
	}{
		{
			name: "git root",
			configure: func(path string, binding storage.WorktreeBinding) *recoveryTestRunner {
				runner := healthyRecoveryRunner(path, binding.BranchName, binding.HeadRevision)
				runner.responses["rev-parse\x00--show-toplevel"] = gitwork.CommandResult{
					Stdout: []byte(filepath.Dir(path) + "\n"),
				}
				return runner
			},
			wantReason: "task worktree moved or its Git root diverged",
		},
		{
			name: "branch",
			configure: func(path string, binding storage.WorktreeBinding) *recoveryTestRunner {
				return healthyRecoveryRunner(path, "codeflux/task/replaced", binding.HeadRevision)
			},
			wantReason: "task worktree branch diverged from durable metadata",
		},
		{
			name: "head",
			configure: func(path string, binding storage.WorktreeBinding) *recoveryTestRunner {
				return healthyRecoveryRunner(
					path,
					binding.BranchName,
					"2222222222222222222222222222222222222222",
				)
			},
			wantReason: "task worktree HEAD diverged from durable metadata",
		},
		{
			name: "not git",
			configure: func(path string, binding storage.WorktreeBinding) *recoveryTestRunner {
				runner := healthyRecoveryRunner(path, binding.BranchName, binding.HeadRevision)
				runner.errors["rev-parse\x00--show-toplevel"] = errors.New("not a repository")
				return runner
			},
			wantReason: "task worktree is no longer a valid Git worktree",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			path := canonicalRecoveryTestPath(t, t.TempDir())
			binding := recoveryTestBinding(t, path)
			store := &recoveryTestStore{bindings: []storage.WorktreeBinding{binding}}
			candidates, err := RecoverInvalidWorktrees(
				t.Context(),
				store,
				test.configure(path, binding),
			)
			if err != nil {
				t.Fatal(err)
			}
			if len(candidates) != 1 || candidates[0].Reason != test.wantReason {
				t.Fatalf("recovery candidates = %#v, want reason %q", candidates, test.wantReason)
			}
		})
	}
}

func TestRecoverInvalidWorktreesRejectsUnsafePathsWithoutRunningGit(t *testing.T) {
	t.Parallel()

	binding := recoveryTestBinding(t, t.TempDir())
	binding.WorktreePath = "relative/worktree"
	store := &recoveryTestStore{bindings: []storage.WorktreeBinding{binding}}
	runner := &recoveryTestRunner{}

	candidates, err := RecoverInvalidWorktrees(t.Context(), store, runner)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 ||
		candidates[0].Reason != "task worktree path is no longer a safe absolute non-root path" {
		t.Fatalf("recovery candidates = %#v", candidates)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("unsafe path invoked Git: %#v", runner.calls)
	}
}

func TestRecoverInvalidWorktreesDetectsCanonicalPathSubstitution(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	target := filepath.Join(parent, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(parent, "bound-worktree")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("directory symlinks are unavailable: %v", err)
	}
	binding := recoveryTestBinding(t, link)
	store := &recoveryTestStore{bindings: []storage.WorktreeBinding{binding}}
	runner := &recoveryTestRunner{}

	candidates, err := RecoverInvalidWorktrees(t.Context(), store, runner)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 ||
		candidates[0].Reason != "task worktree moved or its canonical path diverged" {
		t.Fatalf("recovery candidates = %#v", candidates)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("substituted path invoked Git: %#v", runner.calls)
	}
}

func TestRecoverInvalidWorktreesDoesNotConvertCancellationIntoRecovery(t *testing.T) {
	t.Parallel()

	path := canonicalRecoveryTestPath(t, t.TempDir())
	binding := recoveryTestBinding(t, path)
	store := &recoveryTestStore{bindings: []storage.WorktreeBinding{binding}}
	ctx, cancel := context.WithCancel(t.Context())
	runner := healthyRecoveryRunner(path, binding.BranchName, binding.HeadRevision)
	runner.beforeRun = cancel
	runner.errors["rev-parse\x00--show-toplevel"] = context.Canceled

	candidates, err := RecoverInvalidWorktrees(ctx, store, runner)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
	if len(candidates) != 0 || len(store.marked) != 0 {
		t.Fatalf("cancelled recovery mutated state: %#v, %#v", candidates, store.marked)
	}
}

func TestRecoverInvalidWorktreesRequiresDependencies(t *testing.T) {
	t.Parallel()

	runner := &recoveryTestRunner{}
	if _, err := RecoverInvalidWorktrees(t.Context(), nil, runner); err == nil {
		t.Fatal("nil store was accepted")
	}
	store := &recoveryTestStore{}
	if _, err := RecoverInvalidWorktrees(t.Context(), store, nil); err == nil {
		t.Fatal("nil runner was accepted")
	}
}

type recoveryTestStore struct {
	bindings []storage.WorktreeBinding
	marked   []domain.TaskID
}

func (store *recoveryTestStore) ListActiveWorktreeBindings(
	context.Context,
	int,
) ([]storage.WorktreeBinding, error) {
	return append([]storage.WorktreeBinding(nil), store.bindings...), nil
}

func (store *recoveryTestStore) MarkWorktreeRecoveryRequired(
	_ context.Context,
	taskID domain.TaskID,
	expectedRevision uint64,
	_ string,
) (storage.WorktreeBinding, error) {
	for index := range store.bindings {
		binding := &store.bindings[index]
		if binding.TaskID != taskID {
			continue
		}
		if binding.State != storage.WorktreeBindingActive ||
			binding.Revision != expectedRevision {
			return storage.WorktreeBinding{}, errors.New("stale binding")
		}
		binding.State = storage.WorktreeBindingRecoveryRequired
		binding.Revision++
		store.marked = append(store.marked, taskID)
		return *binding, nil
	}
	return storage.WorktreeBinding{}, errors.New("binding not found")
}

type recoveryTestRunner struct {
	responses map[string]gitwork.CommandResult
	errors    map[string]error
	calls     []string
	beforeRun func()
}

func (runner *recoveryTestRunner) Run(
	_ context.Context,
	directory string,
	executable string,
	arguments ...string,
) (gitwork.CommandResult, error) {
	if runner.beforeRun != nil {
		beforeRun := runner.beforeRun
		runner.beforeRun = nil
		beforeRun()
	}
	key := strings.Join(arguments, "\x00")
	runner.calls = append(runner.calls, executable+"\x00"+directory+"\x00"+key)
	if err := runner.errors[key]; err != nil {
		return gitwork.CommandResult{}, err
	}
	result, ok := runner.responses[key]
	if !ok {
		return gitwork.CommandResult{}, errors.New("unexpected Git command")
	}
	return result, nil
}

func healthyRecoveryRunner(
	path string,
	branch string,
	head string,
) *recoveryTestRunner {
	return &recoveryTestRunner{
		responses: map[string]gitwork.CommandResult{
			"rev-parse\x00--show-toplevel": {
				Stdout: []byte(path + "\n"),
			},
			"symbolic-ref\x00--short\x00HEAD": {
				Stdout: []byte(branch + "\n"),
			},
			"rev-parse\x00--verify\x00HEAD": {
				Stdout: []byte(head + "\n"),
			},
		},
		errors: make(map[string]error),
	}
}

func recoveryTestBinding(t *testing.T, path string) storage.WorktreeBinding {
	t.Helper()
	taskID, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	return storage.WorktreeBinding{
		TaskID:       taskID,
		HeadRevision: recoveryHead,
		BranchName:   "codeflux/task/recovery",
		WorktreePath: path,
		State:        storage.WorktreeBindingActive,
		Revision:     3,
	}
}

func canonicalRecoveryTestPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	absolute, err := filepath.Abs(resolved)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.IsDir() {
		t.Fatalf("test path is not a directory: %v", err)
	}
	return filepath.Clean(absolute)
}
