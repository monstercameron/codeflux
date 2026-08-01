package testfixtures

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RepositoryState names one repository condition the coordinator must handle
// (M22-106). Each is a state a user can genuinely arrive in, not a synthetic
// one: an agent that only ever sees a clean checkout is untested against the
// repositories people actually have.
type RepositoryState string

const (
	// StateClean is a committed working tree with nothing outstanding.
	StateClean RepositoryState = "clean"
	// StateDirty has uncommitted local changes the user has not saved.
	StateDirty RepositoryState = "dirty"
	// StateDetached has HEAD pointing at a commit rather than a branch, so a
	// commit made here would be reachable from nothing.
	StateDetached RepositoryState = "detached"
	// StateConflicted is mid-merge with unresolved conflict markers on disk.
	StateConflicted RepositoryState = "conflicted"
	// StateNested contains a second repository inside the first, where naive
	// path handling will happily cross the inner boundary.
	StateNested RepositoryState = "nested"
	// StateMalicious contains prompt injection and credential-shaped strings.
	StateMalicious RepositoryState = "malicious"
)

// AllRepositoryStates returns every declared state.
func AllRepositoryStates() []RepositoryState {
	return []RepositoryState{
		StateClean, StateDirty, StateDetached,
		StateConflicted, StateNested, StateMalicious,
	}
}

// Valid reports whether a state is one of the declared set.
func (state RepositoryState) Valid() bool {
	for _, candidate := range AllRepositoryStates() {
		if candidate == state {
			return true
		}
	}
	return false
}

// SafeToOperateOn reports whether the coordinator may begin work here without
// first asking the user to resolve something.
//
// This is a fixture's claim about what the state means, and tests assert the
// real policy agrees. Encoding it here rather than in each test keeps the
// meaning of "conflicted" from drifting between suites.
func (state RepositoryState) SafeToOperateOn() bool {
	switch state {
	case StateClean:
		return true
	case StateDirty, StateDetached, StateConflicted, StateNested, StateMalicious:
		return false
	default:
		return false
	}
}

// StatefulRepository is a fixture repository in a named state.
type StatefulRepository struct {
	RepositoryFixture
	State RepositoryState
	// InnerRoot is set for StateNested: the path of the repository living
	// inside the outer one.
	InnerRoot string
	// ConflictedPaths is set for StateConflicted.
	ConflictedPaths []string
}

// NewStatefulRepository builds a real repository in the requested state
// (M22-106). Every state is produced with real git operations, because a
// fixture that faked the state would not exercise the code that reads git.
func NewStatefulRepository(
	ctx context.Context,
	root string,
	state RepositoryState,
) (StatefulRepository, error) {
	if !state.Valid() {
		return StatefulRepository{}, fmt.Errorf("unknown repository state %q", state)
	}
	files := CleanGoRepositoryFiles()
	if state == StateMalicious {
		files = MaliciousRepositoryFiles()
	}
	fixture, err := NewRepositoryFixture(ctx, root, files)
	if err != nil {
		return StatefulRepository{}, err
	}
	result := StatefulRepository{RepositoryFixture: fixture, State: state}

	switch state {
	case StateClean, StateMalicious:
		return result, nil

	case StateDirty:
		if err := MakeWorktreeDirty(root); err != nil {
			return StatefulRepository{}, fmt.Errorf("make worktree dirty: %w", err)
		}
		return result, nil

	case StateDetached:
		// Checking out the commit by hash detaches HEAD. A commit made from
		// here belongs to no branch, which is exactly the hazard the
		// coordinator must refuse to walk into.
		if _, err := runGit(ctx, root, "checkout", "--detach", fixture.Revision); err != nil {
			return StatefulRepository{}, fmt.Errorf("detach HEAD: %w", err)
		}
		return result, nil

	case StateConflicted:
		conflicted, err := buildConflictedRepository(ctx, root)
		if err != nil {
			return StatefulRepository{}, err
		}
		result.ConflictedPaths = conflicted
		revision, err := runGit(ctx, root, "rev-parse", "HEAD")
		if err != nil {
			return StatefulRepository{}, err
		}
		result.Revision = revision
		return result, nil

	case StateNested:
		inner := filepath.Join(root, "vendor", "inner")
		if _, err := NewRepositoryFixture(ctx, inner, map[string]string{
			"go.mod":   "module inner.invalid\n\ngo 1.26\n",
			"inner.go": "package inner\n\n// Value is inner-repository content.\nfunc Value() int { return 7 }\n",
		}); err != nil {
			return StatefulRepository{}, fmt.Errorf("build nested repository: %w", err)
		}
		result.InnerRoot = inner
		return result, nil
	}
	return StatefulRepository{}, fmt.Errorf("state %q has no builder", state)
}

// buildConflictedRepository leaves the working tree mid-merge with real
// conflict markers, and returns the conflicted paths.
func buildConflictedRepository(ctx context.Context, root string) ([]string, error) {
	// The conflicted file must be one the initial commit already tracks, or
	// `git commit -a` would skip it as untracked and no conflict would form.
	const path = "internal/server/server.go"
	if _, err := runGit(ctx, root, "checkout", "-b", "fixture-side"); err != nil {
		return nil, fmt.Errorf("create side branch: %w", err)
	}
	if err := WriteFiles(root, map[string]string{
		path: "package server\n\n// Health reports the side-branch answer.\nfunc Health() string { return \"side\" }\n",
	}); err != nil {
		return nil, err
	}
	if _, err := runGit(ctx, root, "commit", "-am", "fixture: side change"); err != nil {
		return nil, fmt.Errorf("commit side change: %w", err)
	}
	if _, err := runGit(ctx, root, "checkout", "main"); err != nil {
		return nil, fmt.Errorf("return to main: %w", err)
	}
	if err := WriteFiles(root, map[string]string{
		path: "package server\n\n// Health reports the main-branch answer.\nfunc Health() string { return \"main\" }\n",
	}); err != nil {
		return nil, err
	}
	if _, err := runGit(ctx, root, "commit", "-am", "fixture: main change"); err != nil {
		return nil, fmt.Errorf("commit main change: %w", err)
	}
	// The merge is expected to fail: that failure IS the fixture.
	if _, err := runGit(ctx, root, "merge", "fixture-side"); err == nil {
		return nil, errors.New("fixture merge succeeded, so no conflict was produced")
	}
	contents, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		return nil, fmt.Errorf("read conflicted file: %w", err)
	}
	if !strings.Contains(string(contents), "<<<<<<<") {
		return nil, errors.New("conflicted fixture has no conflict markers on disk")
	}
	return []string{path}, nil
}

// IsDirty reports whether the working tree has uncommitted changes.
func IsDirty(ctx context.Context, root string) (bool, error) {
	output, err := runGit(ctx, root, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(output) != "", nil
}

// IsDetached reports whether HEAD points at a commit rather than a branch.
func IsDetached(ctx context.Context, root string) (bool, error) {
	output, err := runGit(ctx, root, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(output) == "HEAD", nil
}

// HasUnresolvedConflicts reports whether a merge is in progress with unmerged
// paths remaining.
func HasUnresolvedConflicts(ctx context.Context, root string) (bool, error) {
	output, err := runGit(ctx, root, "ls-files", "--unmerged")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(output) != "", nil
}
