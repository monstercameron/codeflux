package coordinator

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	agentloop "codeflux.dev/codeflux/internal/agent"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
)

func TestActiveActionRegistryPauseFinishesOnlySafeReadAndBlocksNewWork(
	t *testing.T,
) {
	registry := NewActiveActionRegistry()
	runID := newTaskControlRunID(t)
	safeContext, finishSafe, err := registry.BindActionContext(
		t.Context(),
		newTaskControlTaskID(t),
		runID,
		agentloop.ActionDescriptor{
			Kind:      agentloop.ActionTool,
			RequestID: "safe-read",
			SafeRead:  true,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	longContext, finishLong, err := registry.BindActionContext(
		t.Context(),
		newTaskControlTaskID(t),
		runID,
		agentloop.ActionDescriptor{
			Kind:        agentloop.ActionValidation,
			RequestID:   "long-validation",
			LongRunning: true,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.RequestPause(runID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-longContext.Done():
	case <-time.After(time.Second):
		t.Fatal("long-running action was not cancelled")
	}
	select {
	case <-safeContext.Done():
		t.Fatal("safe read was cancelled by pause")
	default:
	}
	_, _, err = registry.BindActionContext(
		t.Context(),
		newTaskControlTaskID(t),
		runID,
		agentloop.ActionDescriptor{
			Kind:      agentloop.ActionModel,
			RequestID: "new-model",
		},
	)
	if !errors.Is(err, ErrTaskControlBlocked) {
		t.Fatalf("new action error = %v", err)
	}
	waited := make(chan error, 1)
	go func() {
		waited <- registry.WaitForQuiescence(t.Context(), runID)
	}()
	select {
	case err := <-waited:
		t.Fatalf("safe-read wait ended early: %v", err)
	default:
	}
	finishSafe()
	finishLong()
	select {
	case err := <-waited:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("safe-read wait did not finish")
	}
}

func TestTaskControlPauseOrdersCheckpointBeforePausedAndCancelsLongAction(
	t *testing.T,
) {
	taskID := newTaskControlTaskID(t)
	runID := newTaskControlRunID(t)
	order := &taskControlOrder{}
	store := &taskControlStoreStub{
		snapshot: taskControlSnapshot(
			taskID,
			runID,
			storage.TaskControlActive,
		),
		order: order,
	}
	actions := NewActiveActionRegistry()
	safeContext, finishSafe, err := actions.BindActionContext(
		t.Context(),
		taskID,
		runID,
		agentloop.ActionDescriptor{
			Kind:      agentloop.ActionTool,
			RequestID: "safe-read",
			SafeRead:  true,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	longContext, finishLong, err := actions.BindActionContext(
		t.Context(),
		taskID,
		runID,
		agentloop.ActionDescriptor{
			Kind:        agentloop.ActionModel,
			RequestID:   "long-model",
			LongRunning: true,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	longFinished := make(chan struct{})
	go func() {
		<-longContext.Done()
		finishLong()
		close(longFinished)
	}()
	workerPaused := make(chan struct{})
	workers := &taskControlWorkersStub{
		order: order,
		onPause: func() {
			close(workerPaused)
		},
	}
	safeFinished := make(chan struct{})
	checkpoints := &taskControlCheckpointStub{
		order: order,
		beforeCapture: func() {
			select {
			case <-safeFinished:
			default:
				t.Error("checkpoint captured before safe read finished")
			}
			select {
			case <-longFinished:
			default:
				t.Error("checkpoint captured before cancelled action persisted")
			}
		},
	}
	service := newTaskControlServiceForTest(
		t,
		store,
		actions,
		workers,
		checkpoints,
		&resumeVerifierStub{},
	)
	go func() {
		<-workerPaused
		if safeContext.Err() != nil {
			t.Error("safe read was cancelled")
		}
		finishSafe()
		close(safeFinished)
	}()
	result, err := service.PauseTask(
		t.Context(),
		PauseTaskInput{
			RequestEventID:       newTaskControlEventID(t),
			PausedEventID:        newTaskControlEventID(t),
			TaskID:               taskID,
			RunID:                runID,
			ExpectedTaskRevision: 7,
			ExpectedRunRevision:  11,
			ReasonRedacted:       "user requested pause",
			IdempotencyKey:       "pause-order",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Disposition != storage.TaskControlPaused {
		t.Fatalf("pause result = %#v", result)
	}
	select {
	case <-longContext.Done():
	default:
		t.Fatal("long model was not cancelled")
	}
	want := []string{
		"pause-requested",
		"worker-pause",
		"checkpoint",
		"paused",
	}
	if got := order.snapshot(); !equalTaskControlStrings(got, want) {
		t.Fatalf("pause order = %v, want %v", got, want)
	}
}

func TestTaskControlResumeSurfacesNonOverlapAndBlocksConflict(
	t *testing.T,
) {
	taskID := newTaskControlTaskID(t)
	runID := newTaskControlRunID(t)
	t.Run("non-overlap", func(t *testing.T) {
		store := &taskControlStoreStub{
			snapshot: taskControlSnapshot(
				taskID,
				runID,
				storage.TaskControlPaused,
			),
		}
		workers := &taskControlWorkersStub{}
		service := newTaskControlServiceForTest(
			t,
			store,
			NewActiveActionRegistry(),
			workers,
			&taskControlCheckpointStub{},
			&resumeVerifierStub{assessment: compatibleResumeAssessment(
				PausedEditsNonOverlapping,
				[]string{"docs/user-notes.md"},
			)},
		)
		result, err := service.ResumeTask(
			t.Context(),
			ResumeTaskInput{
				ResumedEventID:       newTaskControlEventID(t),
				BlockedEventID:       newTaskControlEventID(t),
				TaskID:               taskID,
				RunID:                runID,
				ExpectedTaskRevision: 7,
				ExpectedRunRevision:  11,
				IdempotencyKey:       "resume-non-overlap",
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		if result.Control.Disposition != storage.TaskControlActive ||
			!equalTaskControlStrings(
				store.resumedFiles,
				[]string{"docs/user-notes.md"},
			) ||
			workers.resumeCalls != 1 {
			t.Fatalf(
				"resume result=%#v files=%v calls=%d",
				result,
				store.resumedFiles,
				workers.resumeCalls,
			)
		}
	})
	t.Run("conflict", func(t *testing.T) {
		store := &taskControlStoreStub{
			snapshot: taskControlSnapshot(
				taskID,
				runID,
				storage.TaskControlPaused,
			),
		}
		workers := &taskControlWorkersStub{}
		service := newTaskControlServiceForTest(
			t,
			store,
			NewActiveActionRegistry(),
			workers,
			&taskControlCheckpointStub{},
			&resumeVerifierStub{assessment: ResumeAssessment{
				RepositoryCurrent: true,
				WorktreeCurrent:   true,
				PolicyCurrent:     true,
				ProviderCurrent:   true,
				ToolsCurrent:      true,
				PausedEdits:       PausedEditsConflicting,
				ChangedFiles:      []string{"internal/control.go"},
				ConflictFiles:     []string{"internal/control.go"},
				ReasonRedacted:    "paused edit overlaps planned work",
			}},
		)
		result, err := service.ResumeTask(
			t.Context(),
			ResumeTaskInput{
				ResumedEventID:       newTaskControlEventID(t),
				BlockedEventID:       newTaskControlEventID(t),
				TaskID:               taskID,
				RunID:                runID,
				ExpectedTaskRevision: 7,
				ExpectedRunRevision:  11,
				IdempotencyKey:       "resume-conflict",
			},
		)
		if !errors.Is(err, ErrResumeReconciliationRequired) ||
			result.Control.Disposition != storage.TaskControlPaused ||
			store.blockedEvents != 1 ||
			workers.resumeCalls != 0 {
			t.Fatalf(
				"result=%#v err=%v blocked=%d resumes=%d",
				result,
				err,
				store.blockedEvents,
				workers.resumeCalls,
			)
		}
	})
	t.Run("activation-failure-enters-recovery", func(t *testing.T) {
		store := &taskControlStoreStub{
			snapshot: taskControlSnapshot(
				taskID,
				runID,
				storage.TaskControlPaused,
			),
		}
		actions := NewActiveActionRegistry()
		_, finish, err := actions.BindActionContext(
			t.Context(),
			taskID,
			runID,
			agentloop.ActionDescriptor{
				Kind:      agentloop.ActionTool,
				RequestID: "stale-safe-read",
				SafeRead:  true,
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		defer finish()
		service := newTaskControlServiceForTest(
			t,
			store,
			actions,
			&taskControlWorkersStub{},
			&taskControlCheckpointStub{},
			&resumeVerifierStub{assessment: compatibleResumeAssessment(
				PausedEditsUnchanged,
				nil,
			)},
		)
		result, err := service.ResumeTask(
			t.Context(),
			ResumeTaskInput{
				ResumedEventID:       newTaskControlEventID(t),
				BlockedEventID:       newTaskControlEventID(t),
				TaskID:               taskID,
				RunID:                runID,
				ExpectedTaskRevision: 7,
				ExpectedRunRevision:  11,
				IdempotencyKey:       "resume-activation-failure",
			},
		)
		if err == nil ||
			result.Control.Disposition != storage.TaskControlRecovery ||
			result.Control.TaskState != domain.TaskStateRecoveryRequired {
			t.Fatalf("recovery result=%#v err=%v", result, err)
		}
	})
}

func TestTaskControlCancelIsTerminalEvenWhenWorkerDeliveryFails(
	t *testing.T,
) {
	taskID := newTaskControlTaskID(t)
	runID := newTaskControlRunID(t)
	store := &taskControlStoreStub{
		snapshot: taskControlSnapshot(
			taskID,
			runID,
			storage.TaskControlPauseRequested,
		),
	}
	actions := NewActiveActionRegistry()
	actionContext, finishAction, err := actions.BindActionContext(
		t.Context(),
		taskID,
		runID,
		agentloop.ActionDescriptor{
			Kind:      agentloop.ActionTool,
			RequestID: "active-tool",
			SafeRead:  true,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer finishAction()
	workers := &taskControlWorkersStub{
		cancelErr: errors.New("worker disconnected"),
	}
	service := newTaskControlServiceForTest(
		t,
		store,
		actions,
		workers,
		&taskControlCheckpointStub{},
		&resumeVerifierStub{},
	)
	result, err := service.CancelTask(
		t.Context(),
		CancelTaskInput{
			EventID:              newTaskControlEventID(t),
			TaskID:               taskID,
			RunID:                runID,
			ExpectedTaskRevision: 7,
			ExpectedRunRevision:  11,
			ReasonRedacted:       "user cancelled",
			IdempotencyKey:       "cancel-terminal",
		},
	)
	if err == nil ||
		result.Disposition != storage.TaskControlCancelled ||
		result.TaskState != domain.TaskStateCancelled ||
		result.RunState != domain.RunStateCancelled {
		t.Fatalf("cancel result=%#v err=%v", result, err)
	}
	select {
	case <-actionContext.Done():
	default:
		t.Fatal("active action was not cancelled")
	}
	if store.failedEvents != 0 {
		t.Fatalf("cancellation became failure: %d", store.failedEvents)
	}
}

func TestTaskControlTransportRetriesReplayBeforeStaleRevision(
	t *testing.T,
) {
	taskID := newTaskControlTaskID(t)
	runID := newTaskControlRunID(t)
	store := &taskControlStoreStub{
		snapshot: taskControlSnapshot(
			taskID,
			runID,
			storage.TaskControlCancelled,
		),
		replay: storage.TaskControlReplay{
			Found: true,
			Control: storage.TaskControlSnapshot{
				TaskID: taskID, RunID: runID,
				TaskState:    domain.TaskStateCancelled,
				RunState:     domain.RunStateCancelled,
				TaskRevision: 8, RunRevision: 12,
				Disposition: storage.TaskControlCancelled,
				UpdatedAt:   time.Unix(1_700_000_010, 0).UTC(),
			},
		},
	}
	service := newTaskControlServiceForTest(
		t,
		store,
		NewActiveActionRegistry(),
		&taskControlWorkersStub{},
		&taskControlCheckpointStub{},
		&resumeVerifierStub{
			assessment: compatibleResumeAssessment(
				PausedEditsUnchanged,
				nil,
			),
		},
	)
	command := transport.TaskControlCommand{
		TaskID: taskID, ExpectedRevision: 7,
		IdempotencyKey: "transport-retry",
		ReasonRedacted: "same durable request",
	}
	for name, invoke := range map[string]func() (
		transport.TaskControlView,
		error,
	){
		"pause": func() (transport.TaskControlView, error) {
			return service.PauseTaskControl(t.Context(), command)
		},
		"resume": func() (transport.TaskControlView, error) {
			return service.ResumeTaskControl(t.Context(), command)
		},
		"cancel": func() (transport.TaskControlView, error) {
			return service.CancelTaskControl(t.Context(), command)
		},
	} {
		t.Run(name, func(t *testing.T) {
			view, err := invoke()
			if err != nil || view.Revision != 8 ||
				view.State != domain.TaskStateCancelled {
				t.Fatalf("replay view = %#v, %v", view, err)
			}
		})
	}
	if store.readCalls != 0 {
		t.Fatalf(
			"current revision was read before replay: %d",
			store.readCalls,
		)
	}
}

type taskControlStoreStub struct {
	mu            sync.Mutex
	snapshot      storage.TaskControlSnapshot
	order         *taskControlOrder
	resumedFiles  []string
	blockedEvents int
	failedEvents  int
	replay        storage.TaskControlReplay
	readCalls     int
}

func (store *taskControlStoreStub) ReadTaskControl(
	context.Context,
	domain.TaskID,
) (storage.TaskControlSnapshot, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.readCalls++
	return store.snapshot, nil
}

func (store *taskControlStoreStub) ReadTaskControlReplay(
	context.Context,
	storage.TaskControlReplayRequest,
) (storage.TaskControlReplay, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.replay, nil
}

func (store *taskControlStoreStub) RequestTaskPause(
	_ context.Context,
	_ storage.RequestTaskPause,
) (storage.TaskControlSnapshot, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.add("pause-requested")
	store.snapshot.RunState = domain.RunStatePausing
	store.snapshot.RunRevision++
	store.snapshot.Disposition = storage.TaskControlPauseRequested
	return store.snapshot, nil
}

func (store *taskControlStoreStub) CompleteTaskPause(
	_ context.Context,
	_ storage.CompleteTaskPause,
) (storage.TaskControlSnapshot, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.add("paused")
	store.snapshot.TaskState = domain.TaskStatePaused
	store.snapshot.RunState = domain.RunStatePaused
	store.snapshot.TaskRevision++
	store.snapshot.RunRevision++
	store.snapshot.Disposition = storage.TaskControlPaused
	return store.snapshot, nil
}

func (store *taskControlStoreStub) CancelControlledTask(
	_ context.Context,
	_ storage.CancelControlledTask,
) (storage.TaskControlSnapshot, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.snapshot.TaskState = domain.TaskStateCancelled
	store.snapshot.RunState = domain.RunStateCancelled
	store.snapshot.TaskRevision++
	store.snapshot.RunRevision++
	store.snapshot.Disposition = storage.TaskControlCancelled
	return store.snapshot, nil
}

func (store *taskControlStoreStub) ResumeControlledTask(
	_ context.Context,
	input storage.ResumeControlledTask,
) (storage.TaskControlSnapshot, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.resumedFiles = append(
		[]string(nil),
		input.NonOverlappingFiles...,
	)
	store.snapshot.TaskState = domain.TaskStateRunning
	store.snapshot.RunState = domain.RunStateStarting
	store.snapshot.TaskRevision++
	store.snapshot.RunRevision++
	store.snapshot.Disposition = storage.TaskControlActive
	return store.snapshot, nil
}

func (store *taskControlStoreStub) RecordTaskResumeBlocked(
	_ context.Context,
	input storage.RecordTaskResumeBlocked,
) (storage.TaskEvent, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.blockedEvents++
	return storage.TaskEvent{
		ID:        input.EventID,
		TaskID:    input.TaskID,
		EventType: "task.resume-blocked",
	}, nil
}

func (store *taskControlStoreStub) MarkTaskControlRecoveryRequired(
	_ context.Context,
	_ storage.MarkTaskControlRecoveryRequired,
) (storage.TaskControlSnapshot, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.snapshot.TaskState = domain.TaskStateRecoveryRequired
	store.snapshot.RunState = domain.RunStateRecoveryRequired
	store.snapshot.Disposition = storage.TaskControlRecovery
	return store.snapshot, nil
}

func (store *taskControlStoreStub) add(value string) {
	if store.order != nil {
		store.order.add(value)
	}
}

type taskControlWorkersStub struct {
	order       *taskControlOrder
	onPause     func()
	cancelErr   error
	resumeCalls int
}

func (stub *taskControlWorkersStub) PauseRun(
	domain.RunID,
	string,
) error {
	if stub.order != nil {
		stub.order.add("worker-pause")
	}
	if stub.onPause != nil {
		stub.onPause()
	}
	return nil
}

func (stub *taskControlWorkersStub) ResumeRun(
	domain.RunID,
	string,
) error {
	stub.resumeCalls++
	return nil
}

func (stub *taskControlWorkersStub) CancelRun(
	domain.RunID,
	string,
) error {
	return stub.cancelErr
}

type taskControlCheckpointStub struct {
	order         *taskControlOrder
	beforeCapture func()
}

func (stub *taskControlCheckpointStub) CapturePauseCheckpoint(
	_ context.Context,
	_ storage.TaskControlSnapshot,
	_ string,
) (domain.CheckpointID, error) {
	if stub.beforeCapture != nil {
		stub.beforeCapture()
	}
	if stub.order != nil {
		stub.order.add("checkpoint")
	}
	return newTaskControlCheckpointID(), nil
}

type resumeVerifierStub struct {
	assessment ResumeAssessment
	err        error
}

func (stub *resumeVerifierStub) VerifyResume(
	context.Context,
	storage.TaskControlSnapshot,
) (ResumeAssessment, error) {
	return stub.assessment, stub.err
}

type taskControlFactsStub struct{}

func (taskControlFactsStub) ReadTaskControlFacts(
	context.Context,
	domain.TaskID,
	domain.RunID,
) (bool, bool, bool, error) {
	return true, true, false, nil
}

type taskControlOrder struct {
	mu     sync.Mutex
	values []string
}

func (order *taskControlOrder) add(value string) {
	order.mu.Lock()
	defer order.mu.Unlock()
	order.values = append(order.values, value)
}

func (order *taskControlOrder) snapshot() []string {
	order.mu.Lock()
	defer order.mu.Unlock()
	return append([]string(nil), order.values...)
}

func newTaskControlServiceForTest(
	t *testing.T,
	store TaskControlStore,
	actions *ActiveActionRegistry,
	workers TaskControlWorkers,
	checkpoints TaskControlCheckpointer,
	resume ResumeVerifier,
) *TaskControlService {
	t.Helper()
	service, err := NewTaskControlService(
		TaskControlDependencies{
			Store:       store,
			Actions:     actions,
			Workers:     workers,
			Checkpoints: checkpoints,
			Facts:       taskControlFactsStub{},
			Resume:      resume,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func taskControlSnapshot(
	taskID domain.TaskID,
	runID domain.RunID,
	disposition storage.TaskControlDisposition,
) storage.TaskControlSnapshot {
	taskState := domain.TaskStateRunning
	runState := domain.RunStateRunning
	switch disposition {
	case storage.TaskControlPauseRequested:
		runState = domain.RunStatePausing
	case storage.TaskControlPaused:
		taskState = domain.TaskStatePaused
		runState = domain.RunStatePaused
	}
	return storage.TaskControlSnapshot{
		TaskID:       taskID,
		RunID:        runID,
		TaskState:    taskState,
		RunState:     runState,
		TaskRevision: 7,
		RunRevision:  11,
		RunAttempt:   1,
		Disposition:  disposition,
		UpdatedAt:    time.Unix(1_700_000_000, 0).UTC(),
	}
}

func compatibleResumeAssessment(
	edits PausedEditClassification,
	files []string,
) ResumeAssessment {
	return ResumeAssessment{
		RepositoryCurrent: true,
		WorktreeCurrent:   true,
		PolicyCurrent:     true,
		ProviderCurrent:   true,
		ToolsCurrent:      true,
		PausedEdits:       edits,
		ChangedFiles:      append([]string(nil), files...),
		ReasonRedacted:    "all immutable bindings remain compatible",
	}
}

func newTaskControlTaskID(t *testing.T) domain.TaskID {
	t.Helper()
	value, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func newTaskControlRunID(t *testing.T) domain.RunID {
	t.Helper()
	value, err := domain.NewRunID()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func newTaskControlEventID(t *testing.T) domain.EventID {
	t.Helper()
	value, err := domain.NewEventID()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func newTaskControlCheckpointID() domain.CheckpointID {
	value, err := domain.NewCheckpointID()
	if err != nil {
		panic(err)
	}
	return value
}

func equalTaskControlStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
