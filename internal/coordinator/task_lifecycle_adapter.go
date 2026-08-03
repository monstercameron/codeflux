package coordinator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/fingerprint"
	"codeflux.dev/codeflux/internal/policy"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
)

// TaskLifecycleAdapter implements transport.TaskLifecycleApplication over
// TaskPreflightService, closing the last link between a submitted
// requirement and the agent loop.
//
// It is deliberately thin. Everything it does is translate a transport
// command into a TaskIntakeRequest and translate the result back; no
// lifecycle judgement lives here.
type TaskLifecycleAdapter struct {
	preflight *TaskPreflightService
	store     taskLifecycleStore
	launcher  RunLauncher
	// environment observes the facts intake requires and a caller cannot know:
	// the base revision, the toolchain, the validation profile, the baseline
	// model. Without it every client had to supply four values it would have
	// been guessing, which is precisely what intake refuses.
	environment *TaskEnvironmentObserver
	// agent carries out the work a started task describes. It is nil when no
	// provider key is configured, and then a task still starts and still gets a
	// worktree — it simply has nobody to act in it, which is the honest state.
	agent TaskAgent
	// notifyDispatch asks the dispatch pump to look for startable work once a
	// plan approval is durably granted (AUDIT-018a). It is nil until
	// SetDispatchNotifier installs it, and every call this adapter makes to
	// it must stay non-blocking, matching the pump's buffer-of-one signal
	// channel, so an approval is never delayed by it.
	notifyDispatch func()
}

// TaskAgent performs the work one started task asks for.
type TaskAgent interface {
	Run(ctx context.Context, taskID domain.TaskID, runID domain.RunID) error
}

// abandonRunThatCouldNotProceed moves a task off running when the agent
// returned an error instead of finishing.
//
// The error used to be discarded outright. The consequence was not a lost log
// line: a run whose agent failed before doing anything left the task reading
// running forever, with a worker subprocess idle on the other end of a
// loopback connection waiting for instructions nobody would send, and no
// record anywhere of why. Every observer — the browser, the ladder, a person
// — saw a task that had started and was working. Diagnosing one such stall
// took a goroutine dump of the coordinator and a process listing to establish
// that the worker had burned zero seconds of CPU.
//
// It is deliberately conditional on the task still being in a running state.
// The ordinary reasons Run returns an error include paths where the run has
// already recorded its own terminal state, and overwriting that would replace
// a specific outcome with this generic one.
func (adapter *TaskLifecycleAdapter) abandonRunThatCouldNotProceed(
	taskID domain.TaskID, cause error,
) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	task, err := adapter.store.GetTask(ctx, taskID)
	if err != nil {
		return
	}
	if task.State != domain.TaskStateRunning {
		// The run reached a terminal state of its own. Its account is better
		// than this one.
		return
	}
	eventID, err := domain.NewEventID()
	if err != nil {
		return
	}
	_, _ = adapter.store.TransitionTask(ctx, storage.TransitionTask{
		EventID:          eventID,
		TaskID:           taskID,
		ExpectedRevision: task.Revision,
		From:             task.State,
		To:               domain.TaskStateFailed,
		IdempotencyKey: fmt.Sprintf(
			"agent-run-failed:%s:%d", taskID, task.Revision),
	})
	// The transition result is deliberately not inspected. It is best effort
	// by construction: something else may have moved the task between the read
	// above and this write, and that something else knew more than this does.
	_ = cause
}

// SetAgentExecution installs the agent a started task hands off to.
func (adapter *TaskLifecycleAdapter) SetAgentExecution(agent TaskAgent) {
	adapter.agent = agent
}

// SetEnvironmentObserver installs the observer that fills what a caller left
// unstated.
func (adapter *TaskLifecycleAdapter) SetEnvironmentObserver(
	observer *TaskEnvironmentObserver,
) {
	adapter.environment = observer
}

// SetDispatchNotifier installs the dispatch pump signal this adapter raises
// after a plan approval is durably granted (AUDIT-018a). It is optional: an
// adapter without one behaves exactly as before, relying on the pump's
// fallback tick.
func (adapter *TaskLifecycleAdapter) SetDispatchNotifier(notify func()) {
	adapter.notifyDispatch = notify
}

// RunLauncher starts the subprocess that executes one prepared run.
//
// It is an interface so a caller that only exercises durable state can supply
// a recording double, and so the adapter cannot reach past it into process
// management.
type RunLauncher interface {
	Launch(
		ctx context.Context,
		taskID domain.TaskID,
		runID domain.RunID,
		policyRevision uint64,
		providerKey string,
	) error
}

// taskLifecycleStore is the narrow read/write surface starting a task needs
// beyond the preflight service itself.
type taskLifecycleStore interface {
	GetTask(context.Context, domain.TaskID) (storage.Task, error)
	GetTaskExecutionPreflight(context.Context, domain.TaskID, uint64) (storage.ExecutionPreflight, error)
	GetTaskExecutionPolicy(context.Context, domain.TaskID, uint64) (storage.ExecutionPolicyRevision, error)
	// Approving a plan walks the task to Ready and binds the exact forecast
	// that was reviewed. Both are declared here rather than reached through a
	// type assertion, so a store that cannot do it fails to compile instead of
	// failing at the moment somebody presses the button.
	GetCurrentTaskForecast(context.Context, domain.TaskID) (storage.TaskForecastRevisions, error)
	TransitionTask(context.Context, storage.TransitionTask) (storage.TransitionedTask, error)
}

// NewTaskLifecycleAdapter builds the adapter.
//
// launcher is required. An adapter without one records that a run started and
// then starts nothing, which is exactly the failure this argument exists to
// make impossible to reintroduce: the task read as running, no subprocess ever
// existed, and no provider request was ever made.
func NewTaskLifecycleAdapter(
	preflight *TaskPreflightService,
	store taskLifecycleStore,
	launcher RunLauncher,
) (*TaskLifecycleAdapter, error) {
	if preflight == nil {
		return nil, errors.New("task preflight service must not be nil")
	}
	if store == nil {
		return nil, errors.New("task lifecycle store must not be nil")
	}
	if launcher == nil {
		return nil, errors.New(
			"a run launcher is required; without one, starting a task would record a run " +
				"and execute nothing")
	}
	return &TaskLifecycleAdapter{preflight: preflight, store: store, launcher: launcher}, nil
}

// CreateTaskFromRequirement turns a submitted requirement into a forecasted
// task and returns the immutable policy, forecast, and budget to present for
// approval.
func (adapter *TaskLifecycleAdapter) CreateTaskFromRequirement(
	ctx context.Context,
	command transport.CreateTaskCommand,
) (transport.CreatedTaskView, error) {
	request := TaskIntakeRequest{
		ThreadID:                 command.ThreadID,
		RequestMessageID:         command.RequestMessageID,
		Requirement:              command.Requirement,
		TaskClass:                fingerprint.TaskClass(command.TaskClass),
		RepositoryRevision:       command.RepositoryRevision,
		BaselineModelRevision:    command.BaselineModelRevision,
		ToolConfigurationVersion: command.ToolConfigurationVersion,
		ValidationProfileVersion: command.ValidationProfileVersion,
		AffectedPaths:            command.AffectedPaths,
		AffectedPackages:         command.AffectedPackages,
		AffectedSymbols:          command.AffectedSymbols,
		IdempotencyKey:           command.IdempotencyKey,
	}
	if adapter.environment != nil {
		observed, observeErr := adapter.environment.Observe(ctx, command.ThreadID)
		if observeErr != nil {
			return transport.CreatedTaskView{}, observeErr
		}
		request = observed.applyTo(request)
	}
	result, err := adapter.preflight.IntakeTask(ctx, request)
	if err != nil {
		return transport.CreatedTaskView{}, err
	}
	return transport.CreatedTaskView{
		TaskControlView: transport.TaskControlView{
			TaskID:    result.Task.ID,
			State:     result.Task.State,
			Revision:  result.Task.Revision,
			UpdatedAt: result.Task.UpdatedAt,
		},
		PolicyRevision:   result.Forecasted.Policy.Revision,
		ForecastRevision: result.Forecasted.Forecast.Revision,
		BudgetID:         result.Budget.BudgetID,
	}, nil
}

// StartPreparedTask begins the exact preflight that was prepared and
// approved for this task.
//
// The caller names the exact preflight revision it approved, and
// StartPreparedTaskRun validates that binding against the task's current
// state. A stale or superseded binding is rejected rather than silently
// upgraded, so what starts is exactly what was reviewed.
func (adapter *TaskLifecycleAdapter) StartPreparedTask(
	ctx context.Context,
	command transport.StartTaskCommand,
) (transport.TaskControlView, error) {
	preflight, err := adapter.store.GetTaskExecutionPreflight(ctx, command.TaskID, command.PreflightRevision)
	if err != nil {
		return transport.TaskControlView{}, err
	}
	runID, err := domain.NewRunID()
	if err != nil {
		return transport.TaskControlView{}, err
	}
	eventID, err := domain.NewEventID()
	if err != nil {
		return transport.TaskControlView{}, err
	}
	if _, err := adapter.preflight.Start(ctx, storage.StartPreparedTaskRun{
		RunID:                runID,
		EventID:              eventID,
		TaskID:               command.TaskID,
		PreflightRevision:    preflight.Revision,
		ExpectedTaskRevision: command.ExpectedRevision,
		Attempt:              1,
		IdempotencyKey:       "start:" + command.IdempotencyKey,
		EventIdempotencyKey:  "start-event:" + command.IdempotencyKey,
	}); err != nil {
		return transport.TaskControlView{}, err
	}
	// The run is durable before the subprocess exists. A crash between the two
	// leaves a row RecoverUnownedTaskRuns can see and reconcile, which is a
	// state somebody can act on; the reverse order would leave a running
	// process nobody recorded.
	providerKey, err := adapter.providerKeyForPolicy(ctx, command.TaskID, preflight.PolicyRevision)
	if err != nil {
		return transport.TaskControlView{}, err
	}
	if err := adapter.launcher.Launch(
		ctx, command.TaskID, runID, preflight.PolicyRevision, providerKey,
	); err != nil {
		// The failure is reported rather than swallowed. The run row stays, and
		// the recovery path owns it: a task that reads as started while nothing
		// runs is the condition this whole path exists to prevent.
		return transport.TaskControlView{}, fmt.Errorf(
			"the run was recorded but its worker did not start: %w", err)
	}

	if adapter.agent != nil {
		// The work runs in the background so starting a task returns at once,
		// which is what the caller and the interface both expect. Its context
		// is deliberately not the request's: a run must not be cut short
		// because the browser call that started it returned.
		agent, taskID, agentRun := adapter.agent, command.TaskID, runID
		go func() {
			runContext, cancel := context.WithTimeout(
				context.Background(), agentRunTimeout)
			defer cancel()
			if err := agent.Run(runContext, taskID, agentRun); err != nil {
				adapter.abandonRunThatCouldNotProceed(taskID, err)
			}
		}()
	}

	started, err := adapter.store.GetTask(ctx, command.TaskID)
	if err != nil {
		return transport.TaskControlView{}, err
	}
	return transport.TaskControlView{
		TaskID:    started.ID,
		State:     started.State,
		Revision:  started.Revision,
		UpdatedAt: started.UpdatedAt,
	}, nil
}

// providerKeyForPolicy reads the provider the approved policy named.
//
// The provider comes from the policy revision that was reviewed, not from a
// current default. A run that was approved against one provider must not be
// queued against another because a preference changed in between.
func (adapter *TaskLifecycleAdapter) providerKeyForPolicy(
	ctx context.Context,
	taskID domain.TaskID,
	policyRevision uint64,
) (string, error) {
	record, err := adapter.store.GetTaskExecutionPolicy(ctx, taskID, policyRevision)
	if err != nil {
		return "", fmt.Errorf("read the approved execution policy: %w", err)
	}
	var snapshot policy.Snapshot
	if err := json.Unmarshal([]byte(record.CanonicalJSON), &snapshot); err != nil {
		return "", fmt.Errorf("decode the approved execution policy: %w", err)
	}
	key := strings.TrimSpace(snapshot.Model.Provider.Provider)
	if key == "" {
		return "", errors.New("the approved execution policy names no provider")
	}
	return key, nil
}

// agentRunTimeout bounds one run.
//
// It is generous because real work takes minutes, and bounded because a
// goroutine with no deadline outlives the thing it was started for.
const agentRunTimeout = 20 * time.Minute
