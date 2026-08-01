package coordinator

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"codeflux.dev/codeflux/internal/buildinfo"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/gitwork"
	"codeflux.dev/codeflux/internal/policy"
	"codeflux.dev/codeflux/internal/storage"
)

// TaskEnvironment pins what a forecast is made against.
//
// Every field here is OBSERVED. Intake refuses each one when it is missing,
// and correctly so — a fingerprint bound to an unknown base cannot be compared
// against prior work, and forecasts made under different tool configurations
// are not comparable at all. But those are facts about this machine and this
// build, not decisions, so a browser must not be the one supplying them: a
// client that invented a toolchain version would put an unmeasured guess inside
// the fingerprint that gates memory retrieval and routing, which is exactly
// what the refusals exist to prevent. The coordinator reads its own.
type TaskEnvironment struct {
	RepositoryRevision       string
	BaselineModelRevision    string
	ToolConfigurationVersion string
	ValidationProfileVersion string
}

// taskEnvironmentStore is the narrow read surface observing an environment
// needs.
type taskEnvironmentStore interface {
	GetThread(context.Context, domain.ThreadID) (storage.Thread, error)
	GetRepository(context.Context, domain.RepositoryID) (storage.Repository, error)
}

// TaskEnvironmentObserver reads the environment a task will run in.
type TaskEnvironmentObserver struct {
	store     taskEnvironmentStore
	worktrees *gitwork.Service
	info      buildinfo.Info
}

// NewTaskEnvironmentObserver builds the observer.
func NewTaskEnvironmentObserver(
	store taskEnvironmentStore,
	worktrees *gitwork.Service,
	info buildinfo.Info,
) (*TaskEnvironmentObserver, error) {
	if store == nil {
		return nil, errors.New("a store is required to observe a task environment")
	}
	return &TaskEnvironmentObserver{store: store, worktrees: worktrees, info: info}, nil
}

// Observe reports the environment for one thread's repository.
func (observer *TaskEnvironmentObserver) Observe(
	ctx context.Context,
	threadID domain.ThreadID,
) (TaskEnvironment, error) {
	if observer == nil {
		return TaskEnvironment{}, errors.New("no task environment observer is configured")
	}
	thread, err := observer.store.GetThread(ctx, threadID)
	if err != nil {
		return TaskEnvironment{}, fmt.Errorf("resolve the thread's repository: %w", err)
	}
	repository, err := observer.store.GetRepository(ctx, thread.RepositoryID)
	if err != nil {
		return TaskEnvironment{}, fmt.Errorf("read the repository: %w", err)
	}

	environment := TaskEnvironment{
		// The baseline is the model the fixed routing policy will actually
		// select, named by the same constants that select it, so a comparison
		// against this baseline is a comparison against what ran.
		BaselineModelRevision: policy.FixedBaselineProvider + "/" +
			policy.FixedBaselineModel + "@" + policy.FixedBaselineProviderVersion,
		// The tools and the validation profile are this build. There is no
		// separate configuration to read: the commands a task runs and the
		// checks it must pass are compiled into this binary, so its version is
		// the honest answer and it changes when they do.
		ToolConfigurationVersion: "coordinator@" + observer.info.Version,
		ValidationProfileVersion: "schema-" +
			strconv.FormatUint(uint64(observer.info.SchemaVersion), 10) +
			"@" + observer.info.Version,
	}

	// The revision comes from Git when Git can answer, because the stored
	// identity records where the repository was when it was opened and a task
	// starting now starts from where it is.
	if observer.worktrees != nil {
		if live, liveErr := observer.worktrees.ReadRepositoryState(
			ctx, repository.CanonicalPath,
		); liveErr == nil && live.HeadRevision != "" {
			environment.RepositoryRevision = live.HeadRevision
		}
	}
	if environment.RepositoryRevision == "" {
		environment.RepositoryRevision = strings.TrimSpace(repository.GitIdentity)
	}
	if environment.RepositoryRevision == "" {
		// Refused rather than defaulted. A task bound to an empty base would
		// pass every check and be comparable to nothing.
		return TaskEnvironment{}, errors.New(
			"the repository has no readable revision, so a task cannot be bound to a base")
	}
	return environment, nil
}

// applyTo fills only what a caller left empty.
//
// A caller that names a value keeps it, because a benchmark comparing against a
// pinned baseline has a legitimate reason to say so; a caller that names none
// gets this machine's answer instead of a refusal.
func (environment TaskEnvironment) applyTo(request TaskIntakeRequest) TaskIntakeRequest {
	if strings.TrimSpace(request.RepositoryRevision) == "" {
		request.RepositoryRevision = environment.RepositoryRevision
	}
	if strings.TrimSpace(request.BaselineModelRevision) == "" {
		request.BaselineModelRevision = environment.BaselineModelRevision
	}
	if strings.TrimSpace(request.ToolConfigurationVersion) == "" {
		request.ToolConfigurationVersion = environment.ToolConfigurationVersion
	}
	if strings.TrimSpace(request.ValidationProfileVersion) == "" {
		request.ValidationProfileVersion = environment.ValidationProfileVersion
	}
	return request
}
