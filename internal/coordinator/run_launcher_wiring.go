package coordinator

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/gitwork"
)

// buildRunLauncher wires the launcher that turns a started run into a running
// worker.
//
// A coordinator that owns its own workers gets a real launcher. One driven by
// an external worker controller gets a launcher that refuses by name, because
// the alternative — no launcher, and a start path that silently records a run —
// is the failure this whole wiring exists to remove.
func buildRunLauncher(
	options ApplicationOptions,
	repositories runLauncherRepositories,
	application *Application,
) (RunLauncher, error) {
	if application.runtime == nil {
		return unavailableRunLauncher{
			reason: "this coordinator does not own worker processes, so it cannot start a " +
				"run; the configured worker controller owns execution",
		}, nil
	}

	root := strings.TrimSpace(options.WorktreeRoot)
	if root == "" {
		// Beside the database, because the worktrees and the rows describing
		// them are one unit: moving a database without its worktrees leaves
		// every binding pointing at nothing.
		root = filepath.Join(filepath.Dir(options.DatabasePath), "worktrees")
	}
	worktrees, err := gitwork.NewService(root, repositories, gitwork.ExecRunner{}, nil)
	if err != nil {
		return nil, fmt.Errorf("prepare the task worktree root: %w", err)
	}
	// A launched run is the path that actually edits a repository, so an
	// unbound recorder here means the edits with the most to answer for are
	// the ones with no durable record.
	launcherEditEvents, err := gitwork.NewStorageEditEventRecorder(repositories)
	if err != nil {
		return nil, fmt.Errorf("prepare the edit event recorder: %w", err)
	}
	worktrees.SetEditEventRecorder(launcherEditEvents)

	executable := strings.TrimSpace(options.WorkerExecutable)
	if executable == "" {
		executable, err = DefaultWorkerExecutable()
		if err != nil {
			return nil, err
		}
	}
	if err := validateContainerCommand(options.ContainerCommand); err != nil {
		return nil, fmt.Errorf("container command: %w", err)
	}
	return NewTaskRunLauncher(
		repositories,
		worktrees,
		application.runtime,
		application.scheduler,
		executable,
		application.Address,
		options.Random,
		append([]string(nil), options.ContainerCommand...),
	)
}

// validateContainerCommand checks a user-authorized container command before
// it can ever reach a launched worker (M11-033, AUDIT-017).
//
// Codeflux atom documentation (schema v1):
//
//	Purpose:
//	  Refuse a malformed container command at configuration time, before any
//	  task can launch a worker under it.
//	Use when:
//	  Validating ApplicationOptions.ContainerCommand during application
//	  startup, once, before it is bound into the run launcher.
//	Do not use when:
//	  Validating a worker's own StartupParameters.ContainerCommand after
//	  launch — internal/worker.StartupParameters.Validate covers that
//	  narrower, per-launch check.
//	Semantics:
//	  An empty or nil command is always valid: it is the native path, and
//	  stronger isolation stays opt-in. A non-empty command must have no more
//	  entries than a worker startup message accepts, and no entry may be
//	  blank or carry a NUL byte, which exec.Command cannot represent.
//	Inputs:
//	  - command: the configured container command argv, or nil for native
//	    execution. Not yet split into an executable and its arguments; that
//	    split happens once, in the worker, at the moment of exec.
//	Outputs:
//	  - nil when the command is usable as configured.
//	  - an error naming the defect when it is not.
//	Preconditions:
//	  None.
//	Postconditions:
//	  None: pure validation, no state changes.
//	Effects:
//	  None: pure atom.
//	Failure semantics:
//	  Every failure is a caller configuration defect; there is nothing to
//	  retry without changing the configured command.
//	Determinism:
//	  Deterministic; no time, randomness, or external state.
//	Idempotency and retry:
//	  Idempotent; safe to call repeatedly with the same input.
//	Reconciliation and compensation:
//	  None: the caller corrects the configuration and restarts.
//	Security and privacy:
//	  The command is operator-supplied local configuration, not
//	  repository-derived or model-derived text, so it carries no injection
//	  risk this check needs to defend against; the check exists to fail
//	  loudly rather than silently launch every task natively.
//	Dependencies and bindings:
//	  Mirrors the bound worker/protocol.go argument-count limit so a command
//	  this accepts is never later refused by worker startup.
//	Complexity and limits:
//	  O(len(command)); the same 64-entry bound worker startup enforces.
//	Examples:
//	  - []string{"podman", "run", "--rm", "-i"} is accepted.
//	  - []string{"podman", ""} is refused: a blank argument.
//	Verification:
//	  internal/coordinator/container_command_wiring_test.go.
//	Retrieval concepts:
//	  container command validation, worker isolation, M11-033, AUDIT-017.
//
//codeflux:atom
func validateContainerCommand(command []string) error {
	if len(command) == 0 {
		return nil
	}
	if len(command) > 64 {
		return errors.New("container command must not exceed 64 arguments")
	}
	for _, token := range command {
		if strings.TrimSpace(token) == "" {
			return errors.New("container command must not contain a blank argument")
		}
		if strings.ContainsRune(token, 0) {
			return errors.New("container command must not contain a NUL byte")
		}
	}
	return nil
}

// runLauncherRepositories is the storage surface the launcher and its worktree
// service need together.
type runLauncherRepositories interface {
	taskRunLauncherStore
	gitwork.BindingRepository
	// A launched run's mediated edits are recorded in the ordered task
	// journal, so the launcher's store must be able to append to it.
	gitwork.TaskEventAppender
}

// unavailableRunLauncher refuses to start a run, by name.
//
// It exists so an application configured without its own worker processes
// still fails loudly at the moment somebody presses start, rather than
// reporting success and doing nothing.
type unavailableRunLauncher struct{ reason string }

func (launcher unavailableRunLauncher) Launch(
	context.Context, domain.TaskID, domain.RunID, uint64, string,
) error {
	return errors.New(launcher.reason)
}
