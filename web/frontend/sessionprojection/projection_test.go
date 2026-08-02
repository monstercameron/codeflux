package sessionprojection

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
	"codeflux.dev/codeflux/web/frontend/sessionclient"
	"codeflux.dev/codeflux/web/frontend/taskprojection"
)

func TestMountedTaskProjectionAdvancesFromSnapshotThroughOrderedSessionEvents(t *testing.T) {
	ids := newProjectionTestIDs(t)
	projection := applySnapshot(t, New(), taskProjectionSnapshotFor(ids))
	projection = applyEvent(t, projection, validEventForKind(
		t, ids, events.KindThreadCreated, 1, 0,
	))
	projection = applyEvent(t, projection, validEventForKind(
		t, ids, events.KindTaskStateChanged, 2, 1,
	))
	task, ok := projection.TaskProjection()
	if !ok || task.TaskID != ids.task || task.State != domain.TaskStateForecasting ||
		task.Revision != 1 || task.LastSequence != 2 || projection.LastAppliedSequence() != 2 {
		t.Fatalf("task/session projection = %#v / %#v", task, projection.Diagnostics())
	}
}

func TestMountedTaskProjectionAppliesSchemaBackedRecoveryEvent(t *testing.T) {
	ids := newProjectionTestIDs(t)
	projection := applySnapshot(t, New(), taskProjectionSnapshotFor(ids))
	event := validEventForKind(t, ids, events.KindRecoveryRequired, 1, 1)
	next, err := ApplySessionEvent(projection, event)
	if err != nil {
		t.Fatal(err)
	}
	task, ok := next.TaskProjection()
	if !ok || task.Recovery != taskprojection.RecoveryAmbiguousOutcome ||
		!task.RecoveryDetail.Present || !task.RecoveryDetail.ExternalOutcomeAmbiguous ||
		!task.RecoveryDetail.PreservePatchAvailable || task.LastSequence != 1 ||
		next.Diagnostics().Repair != nil {
		t.Fatalf("recovery projection = %#v diagnostics=%#v", task, next.Diagnostics())
	}
}

func TestMountedTaskProjectionAppliesCheckpointValidationAndAcceptance(t *testing.T) {
	ids := newProjectionTestIDs(t)
	projection := applySnapshot(t, New(), taskProjectionSnapshotFor(ids))
	projection = applyEvent(t, projection, validEventForKind(t, ids, events.KindTaskStateChanged, 1, 1))
	projection = applyEvent(t, projection, validEventForKind(t, ids, events.KindCheckpointCreated, 2, 1))
	projection = applyEvent(t, projection, validEventForKind(t, ids, events.KindValidationUpdated, 3, 1))
	projection = applyEvent(t, projection, validEventForKind(t, ids, events.KindChangeAcceptanceUpdated, 4, 1))
	task, ok := projection.TaskProjection()
	if !ok || !task.Checkpoint.Present || task.Checkpoint.PlanStep != "test" ||
		!task.Validation.Present || !task.Validation.Required || task.Validation.DiffRevision != 1 ||
		!task.Acceptance.Present || task.Acceptance.State != domain.ChangeAcceptanceStatePending ||
		task.Acceptance.Bindings.Graph != 1 || task.LastSequence != 4 ||
		projection.LastAppliedSequence() != 4 || projection.Diagnostics().Repair != nil {
		t.Fatalf("mounted task projection = %#v diagnostics=%#v", task, projection.Diagnostics())
	}
}

func TestMountedTaskProjectionRequestsRepairWithoutPartialApplyOnInconsistency(t *testing.T) {
	ids := newProjectionTestIDs(t)
	projection := applySnapshot(t, New(), taskProjectionSnapshotFor(ids))
	projection = applyEvent(t, projection, validEventForKind(
		t, ids, events.KindBudgetUpdated, 1, 1,
	))
	inconsistent := validEventForKind(t, ids, events.KindBudgetUpdated, 2, 3)
	broken, err := ApplySessionEvent(projection, inconsistent)
	if !errors.Is(err, ErrSnapshotRepairRequired) {
		t.Fatalf("inconsistent projection error = %v", err)
	}
	repair := broken.Diagnostics().Repair
	task, ok := broken.TaskProjection()
	if repair == nil || repair.Reason != RepairTaskProjectionInconsistent ||
		repair.EventKind != events.KindBudgetUpdated || repair.AfterSequence != 1 ||
		broken.LastAppliedSequence() != 1 || !ok || task.LastSequence != 1 ||
		task.Budget.Revision != 1 {
		t.Fatalf("inconsistent repair = %#v task=%#v", repair, task)
	}
}

func TestReconnectAndRefreshRestoreIdenticalActiveTaskAuthorityState(t *testing.T) {
	ids := newProjectionTestIDs(t)
	initial := taskProjectionSnapshotFor(ids)
	reconnecting := applySnapshot(t, New().WithDraft(ids.thread, "keep this draft"), initial)

	eventStream := []events.SessionEvent{
		validEventForKind(t, ids, events.KindPlanCreated, 1, 1),
		validEventForKind(t, ids, events.KindTaskStateChanged, 2, 1),
		validEventForKind(t, ids, events.KindTaskStateChanged, 3, 2),
		validEventForKind(t, ids, events.KindBudgetUpdated, 4, 1),
		validEventForKind(t, ids, events.KindApprovalRequested, 5, 1),
		validEventForKind(t, ids, events.KindApprovalResolved, 6, 2),
		validEventForKind(t, ids, events.KindTaskStateChanged, 7, 3),
		validEventForKind(t, ids, events.KindTaskStateChanged, 8, 4),
		validEventForKind(t, ids, events.KindValidationUpdated, 9, 1),
	}
	eventStream[2].Payload.TaskStateChanged = &events.TaskStateChanged{
		From: domain.TaskStateForecasting, To: domain.TaskStateAwaitingPlanApproval,
	}
	eventStream[6].Payload.TaskStateChanged = &events.TaskStateChanged{
		From: domain.TaskStateAwaitingPlanApproval, To: domain.TaskStateReady,
		Approval: domain.ApprovalRequestStateGranted,
	}
	eventStream[7].Payload.TaskStateChanged = &events.TaskStateChanged{
		From: domain.TaskStateReady, To: domain.TaskStateRunning,
	}
	for index, event := range eventStream {
		if err := event.Validate(); err != nil {
			t.Fatalf("adjusted event %d: %v", event.Sequence, err)
		}
		if index == len(eventStream)-1 {
			reconnecting = disconnectedProjection(
				reconnecting, reconnecting.LastAppliedSequence(), sessionclient.RetryPolicy{},
			)
			reconnecting = reconnectingProjection(
				reconnecting, reconnecting.LastAppliedSequence(), sessionclient.RetryPolicy{},
			)
		}
		reconnecting = applyEvent(t, reconnecting, event)
	}

	expected := finalActiveTaskSnapshot(t, ids)
	reconnected := reconnecting
	refreshed := applySnapshot(t, New().WithDraft(ids.thread, "keep this draft"), expected)
	reconnected = ProjectConnection(reconnected, sessionclient.Status{
		State: sessionclient.StateLive, LastSequence: 9, ControlsAllowed: true,
	}, sessionclient.RetryPolicy{})
	refreshed = ProjectConnection(refreshed, sessionclient.Status{
		State: sessionclient.StateLive, LastSequence: 9, ControlsAllowed: true,
	}, sessionclient.RetryPolicy{})
	reconnectedTask, reconnectOK := reconnected.TaskProjection()
	refreshedTask, refreshOK := refreshed.TaskProjection()
	if !reconnectOK || !refreshOK || !reflect.DeepEqual(reconnectedTask, refreshedTask) ||
		reconnectedTask.State != domain.TaskStateRunning ||
		!reconnectedTask.Budget.Present || reconnectedTask.Budget.Revision != 1 ||
		!reconnectedTask.Approval.Present ||
		reconnectedTask.Approval.State != domain.ApprovalRequestStateGranted ||
		!reconnectedTask.Validation.Present ||
		reconnectedTask.Validation.State != domain.ValidationStateRunning ||
		reconnected.LastAppliedSequence() != 9 || refreshed.LastAppliedSequence() != 9 ||
		!reconnected.Connection().MutationsAllowed || !refreshed.Connection().MutationsAllowed ||
		reconnected.Draft(ids.thread) != "keep this draft" {
		t.Fatalf("reconnect=%#v refresh=%#v reconnect-connection=%#v", reconnectedTask, refreshedTask, reconnected.Connection())
	}
}

func TestConnectionProjectionExposesEveryStateAndGatesMutations(t *testing.T) {
	ids := newProjectionTestIDs(t)
	projection := applySnapshot(t, New(), snapshotFor(ids, 7, 0))
	policy := sessionclient.RetryPolicy{MaxAttempts: 4}
	tests := []struct {
		name      string
		status    sessionclient.Status
		want      ConnectionState
		mutations bool
		manual    bool
		retry     RetryDisposition
	}{
		{"connecting", sessionclient.Status{State: sessionclient.StateConnecting, LastSequence: 7}, ConnectionConnecting, false, false, RetryNone},
		{"live", sessionclient.Status{State: sessionclient.StateLive, LastSequence: 7, ControlsAllowed: true}, ConnectionLive, true, false, RetryNone},
		{"replaying", sessionclient.Status{State: sessionclient.StateReplaying, LastSequence: 7}, ConnectionReplaying, false, false, RetryNone},
		{"degraded-reconnect", sessionclient.Status{State: sessionclient.StateReconnecting, LastSequence: 7, ReconnectCount: 1, Failure: sessionclient.FailureUnavailable}, ConnectionDegraded, false, false, RetryAutomatic},
		{"degraded-gap", sessionclient.Status{State: sessionclient.StateGap, LastSequence: 7, Failure: sessionclient.FailureProtocol}, ConnectionDegraded, false, false, RetryNone},
		{"disconnected", sessionclient.Status{State: sessionclient.StateStopped, LastSequence: 7}, ConnectionDisconnected, false, true, RetryNone},
		{"incompatible", sessionclient.Status{State: sessionclient.StateFailed, LastSequence: 7, Failure: sessionclient.FailureIncompatible}, ConnectionIncompatible, false, false, RetryBlocked},
		{"unauthorized", sessionclient.Status{State: sessionclient.StateFailed, LastSequence: 7, Failure: sessionclient.FailureAuthentication}, ConnectionUnauthorized, false, false, RetryBlocked},
	}
	seen := make(map[ConnectionState]bool)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			next := ProjectConnection(projection, test.status, policy)
			got := next.Connection()
			if got.State != test.want || got.MutationsAllowed != test.mutations ||
				got.ManualReconnectAvailable != test.manual || got.Retry.Disposition != test.retry {
				t.Fatalf("connection = %+v", got)
			}
			seen[got.State] = true
		})
	}
	if len(seen) != 7 {
		t.Fatalf("connection states covered = %v, want all seven", seen)
	}
	lagged := ProjectConnection(projection, sessionclient.Status{
		State: sessionclient.StateLive, LastSequence: 6, ControlsAllowed: true,
	}, policy)
	if lagged.Connection().State != ConnectionDegraded || lagged.Connection().MutationsAllowed {
		t.Fatalf("cursor-mismatched live projection = %+v", lagged.Connection())
	}
}

func TestRetryProjectionUsesSessionClientBoundedBackoffAndTerminalClassification(t *testing.T) {
	ids := newProjectionTestIDs(t)
	projection := applySnapshot(t, New(), snapshotFor(ids, 0, 0))
	policy := sessionclient.RetryPolicy{
		MaxAttempts: 4, InitialDelay: 10 * time.Millisecond,
		MaxDelay: 25 * time.Millisecond, Multiplier: 2,
	}
	for attempt, wantDelay := range []time.Duration{
		10 * time.Millisecond, 20 * time.Millisecond, 25 * time.Millisecond,
	} {
		got := ProjectConnection(projection, sessionclient.Status{
			State: sessionclient.StateReconnecting, ReconnectCount: attempt + 1,
			Failure: sessionclient.FailureUnavailable,
		}, policy).Connection().Retry
		if got.Disposition != RetryAutomatic || got.Attempt != attempt+1 ||
			got.Maximum != 4 || got.Delay != wantDelay {
			t.Fatalf("attempt %d retry = %+v, want delay %s", attempt+1, got, wantDelay)
		}
	}
	exhausted := ProjectConnection(projection, sessionclient.Status{
		State: sessionclient.StateFailed, ReconnectCount: 4,
		Failure: sessionclient.FailureUnavailable,
	}, policy).Connection()
	if exhausted.Retry.Disposition != RetryExhausted ||
		!exhausted.ManualReconnectAvailable || exhausted.Retry.Delay != 0 {
		t.Fatalf("exhausted retry = %+v", exhausted)
	}
}

func TestSnapshotReplayDuplicateGapRepairAndDraftPreservation(t *testing.T) {
	ids := newProjectionTestIDs(t)
	projection := New().WithDraft(ids.thread, "unsent requirement")
	projection = applySnapshot(t, projection, snapshotFor(ids, 10, 5))
	if projection.Connection().State != ConnectionReplaying ||
		projection.SubscriptionAfterSequence() != 10 {
		t.Fatalf("snapshot projection = %+v", projection)
	}
	event := validEventForKind(t, ids, events.KindMessageFinal, 11, 1)
	projection = applyEvent(t, projection, event)
	if projection.LastAppliedSequence() != 11 || projection.Diagnostics().AppliedEvents != 1 {
		t.Fatalf("applied diagnostics = %+v", projection.Diagnostics())
	}
	projection = applyEvent(t, projection, event)
	if projection.Diagnostics().DuplicateEvents != 1 || projection.Diagnostics().AppliedEvents != 1 {
		t.Fatalf("duplicate diagnostics = %+v", projection.Diagnostics())
	}
	gapped := validEventForKind(t, ids, events.KindError, 13, 1)
	broken, err := ApplySessionEvent(projection, gapped)
	if !errors.Is(err, ErrSnapshotRepairRequired) {
		t.Fatalf("gap error = %v", err)
	}
	repair := broken.Diagnostics().Repair
	if broken.LastAppliedSequence() != 11 || broken.Connection().State != ConnectionDegraded ||
		broken.Connection().MutationsAllowed || repair == nil ||
		repair.Reason != RepairSequenceGap || repair.AfterSequence != 11 ||
		repair.ExpectedSequence != 12 || repair.ReceivedSequence != 13 {
		t.Fatalf("gap projection = %+v diagnostics=%+v", broken.Connection(), broken.Diagnostics())
	}
	blockedEvent := validEventForKind(t, ids, events.KindError, 12, 1)
	blocked, err := ApplySessionEvent(broken, blockedEvent)
	if !errors.Is(err, ErrSnapshotRepairRequired) || blocked.LastAppliedSequence() != 11 {
		t.Fatalf("event applied while repair pending: state=%+v error=%v", blocked, err)
	}
	repaired := applySnapshot(t, broken, snapshotFor(ids, 13, 7))
	if repaired.Diagnostics().Repair != nil || repaired.LastAppliedSequence() != 13 ||
		repaired.Draft(ids.thread) != "unsent requirement" {
		t.Fatalf("repaired projection = %+v diagnostics=%+v", repaired, repaired.Diagnostics())
	}
	live := ProjectConnection(repaired, sessionclient.Status{
		State: sessionclient.StateLive, LastSequence: 13, ControlsAllowed: true,
	}, sessionclient.RetryPolicy{})
	if !live.Connection().MutationsAllowed {
		t.Fatalf("mutations remained disabled after replay: %+v", live.Connection())
	}
}

func TestTaskAndGraphInconsistencyRequestRepairWithoutPartialApply(t *testing.T) {
	ids := newProjectionTestIDs(t)
	base := applySnapshot(t, New(), snapshotFor(ids, 0, 5))
	validTransition := validEventForKind(t, ids, events.KindTaskStateChanged, 1, 1)
	advanced := applyEvent(t, base, validTransition)
	if advanced.Snapshot().Session.TaskState != domain.TaskStateForecasting ||
		advanced.Snapshot().Session.TaskRevision != 1 {
		t.Fatalf("task projection = %+v", advanced.Snapshot().Session)
	}
	impossible := validEventForKind(t, ids, events.KindTaskStateChanged, 2, 2)
	impossible.Payload.TaskStateChanged.From = domain.TaskStateForecasting
	impossible.Payload.TaskStateChanged.To = domain.TaskStateCompleted
	brokenTask, err := ApplySessionEvent(advanced, impossible)
	if !errors.Is(err, ErrSnapshotRepairRequired) ||
		brokenTask.Diagnostics().Repair.Reason != RepairTaskTransitionMismatch ||
		brokenTask.LastAppliedSequence() != 1 ||
		brokenTask.Snapshot().Session.TaskState != domain.TaskStateForecasting ||
		brokenTask.Snapshot().Session.TaskRevision != 1 {
		t.Fatalf("impossible transition partially applied: snapshot=%+v diagnostics=%+v error=%v", brokenTask.Snapshot(), brokenTask.Diagnostics(), err)
	}
	patch := validEventForKind(t, ids, events.KindGraphPatch, 1, 7)
	brokenGraph, err := ApplySessionEvent(base, patch)
	if !errors.Is(err, ErrSnapshotRepairRequired) ||
		brokenGraph.Diagnostics().Repair.Reason != RepairGraphRevisionMismatch ||
		brokenGraph.Snapshot().GraphRevision != 5 || brokenGraph.LastAppliedSequence() != 0 {
		t.Fatalf("stale graph patch partially applied: snapshot=%+v diagnostics=%+v error=%v", brokenGraph.Snapshot(), brokenGraph.Diagnostics(), err)
	}
	validPatch := validEventForKind(t, ids, events.KindGraphPatch, 1, 6)
	patched := applyEvent(t, base, validPatch)
	if patched.Snapshot().GraphRevision != 6 || patched.LastAppliedSequence() != 1 {
		t.Fatalf("valid graph patch = %+v", patched.Snapshot())
	}
}

func TestManualReconnectUsesLastTrustedCursorAndPreservesDraft(t *testing.T) {
	ids := newProjectionTestIDs(t)
	projection := applySnapshot(t, New().WithDraft(ids.thread, "retained"), snapshotFor(ids, 9, 0))
	projection = ProjectConnection(projection, sessionclient.Status{
		State: sessionclient.StateStopped, LastSequence: 9,
	}, sessionclient.RetryPolicy{})
	next, request, err := RequestManualReconnect(projection)
	if err != nil {
		t.Fatal(err)
	}
	if request.AfterSequence != 9 || next.Connection().State != ConnectionConnecting ||
		next.Draft(ids.thread) != "retained" {
		t.Fatalf("manual reconnect = request=%+v projection=%+v", request, next)
	}
	if _, _, err := RequestManualReconnect(next); !errors.Is(err, ErrManualReconnectUnavailable) {
		t.Fatalf("second reconnect error = %v", err)
	}
}

func TestSnapshotReplacementRejectsRegressionAndProjectionValuesAreImmutable(t *testing.T) {
	ids := newProjectionTestIDs(t)
	input := snapshotFor(ids, 3, 4)
	projection := applySnapshot(t, New(), input)
	otherTask := mustIdentity(t, domain.NewTaskID)
	*input.Session.TaskID = otherTask
	if *projection.Snapshot().Session.TaskID != ids.task {
		t.Fatal("projection retained caller-owned snapshot pointer")
	}
	view := projection.Snapshot()
	*view.Session.TaskID = otherTask
	if *projection.Snapshot().Session.TaskID != ids.task {
		t.Fatal("Snapshot returned a mutable pointer into projection state")
	}
	regressed := snapshotFor(ids, 4, 3)
	broken, err := ApplySessionSnapshot(projection, regressed)
	if !errors.Is(err, ErrSnapshotRepairRequired) ||
		broken.Diagnostics().Repair == nil ||
		broken.Diagnostics().Repair.Reason != RepairInvalidSnapshot ||
		broken.Snapshot().GraphRevision != 4 || broken.LastAppliedSequence() != 3 {
		t.Fatalf("regressed snapshot = snapshot=%+v diagnostics=%+v error=%v", broken.Snapshot(), broken.Diagnostics(), err)
	}

	missingTask := snapshotFor(ids, 4, 4)
	missingTask.Session.TaskID = nil
	missingTask.Session.TaskState = ""
	missingTask.Session.TaskRevision = 0
	broken, err = ApplySessionSnapshot(projection, missingTask)
	if !errors.Is(err, ErrSnapshotRepairRequired) ||
		broken.Diagnostics().Repair == nil ||
		broken.Diagnostics().Repair.Reason != RepairTaskIdentityMismatch ||
		broken.Snapshot().Session.TaskID == nil ||
		*broken.Snapshot().Session.TaskID != ids.task || broken.LastAppliedSequence() != 3 {
		t.Fatalf("task-erasing snapshot = snapshot=%+v diagnostics=%+v error=%v", broken.Snapshot(), broken.Diagnostics(), err)
	}
}

func TestDisconnectReplayFaultInjectionCoversEveryEventCategory(t *testing.T) {
	ids := newProjectionTestIDs(t)
	kinds := EventKinds()
	if len(kinds) != len(events.Registry) {
		t.Fatalf("event categories = %d, generated registry = %d", len(kinds), len(events.Registry))
	}
	seen := make(map[events.Kind]bool, len(kinds))
	policy := sessionclient.RetryPolicy{
		MaxAttempts: 3, InitialDelay: time.Millisecond, MaxDelay: 2 * time.Millisecond,
	}
	for _, kind := range kinds {
		t.Run(string(kind), func(t *testing.T) {
			graphRevision := uint64(0)
			eventRevision := uint64(1)
			if kind == events.KindGraphPatch {
				graphRevision = 1
				eventRevision = 2
			}
			baseSnapshot := snapshotFor(ids, 0, graphRevision)
			if kind == events.KindTaskProjectionInvalidated {
				baseSnapshot = taskProjectionSnapshotFor(ids)
			}
			base := applySnapshot(
				t,
				New().WithDraft(ids.thread, "preserved"),
				baseSnapshot,
			)
			if kind == events.KindTaskProjectionInvalidated {
				base = ProjectConnection(base, sessionclient.Status{
					State: sessionclient.StateLive, ControlsAllowed: true,
				}, policy)
				if !base.Connection().MutationsAllowed {
					t.Fatal("live invalidation fixture did not begin with mutations enabled")
				}
			}
			event := validEventForKind(t, ids, kind, 1, eventRevision)
			uninterrupted := applyFaultInjectedEvent(t, base, event, ids)
			phases := []string{"before", "during", "after"}
			for _, phase := range phases {
				t.Run(phase, func(t *testing.T) {
					projection := base
					if phase == "before" {
						projection = disconnectedProjection(projection, 0, policy)
						projection = reconnectingProjection(projection, 0, policy)
					}
					projection = applyFaultInjectedEvent(t, projection, event, ids)
					if phase == "during" || phase == "after" {
						projection = disconnectedProjection(projection, 1, policy)
						projection = reconnectingProjection(projection, 1, policy)
					}
					projection = ProjectConnection(projection, sessionclient.Status{
						State: sessionclient.StateLive, LastSequence: 1, ControlsAllowed: true,
					}, policy)
					expectedApplied := uint64(1)
					expectedLastKind := kind
					if kind == events.KindTaskProjectionInvalidated {
						expectedApplied = 0
						expectedLastKind = ""
					}
					if !reflect.DeepEqual(projection.Snapshot(), uninterrupted.Snapshot()) ||
						!projection.Connection().MutationsAllowed || projection.LastAppliedSequence() != 1 ||
						projection.SubscriptionAfterSequence() != 1 ||
						projection.Diagnostics().AppliedEvents != expectedApplied ||
						projection.Diagnostics().LastEventKind != expectedLastKind ||
						projection.Diagnostics().Repair != nil ||
						projection.Draft(ids.thread) != "preserved" {
						t.Fatalf("%s chaos projection = snapshot=%+v connection=%+v diagnostics=%+v", phase, projection.Snapshot(), projection.Connection(), projection.Diagnostics())
					}
				})
			}
			seen[kind] = true
		})
	}
	if !reflect.DeepEqual(kinds, EventKinds()) || len(seen) != len(kinds) {
		t.Fatalf("event category coverage = %v", seen)
	}
}

func applyFaultInjectedEvent(
	t *testing.T,
	projection Projection,
	event events.SessionEvent,
	ids projectionTestIDs,
) Projection {
	t.Helper()
	next, err := ApplySessionEvent(projection, event)
	if event.Kind != events.KindTaskProjectionInvalidated {
		if err != nil {
			t.Fatal(err)
		}
		return next
	}
	if !errors.Is(err, ErrSnapshotRepairRequired) || next.Diagnostics().Repair == nil ||
		next.Diagnostics().Repair.Reason != RepairTaskProjectionUnsupported ||
		next.Connection().MutationsAllowed || next.Connection().State != ConnectionDegraded {
		t.Fatalf("projection invalidation = diagnostics=%+v error=%v", next.Diagnostics(), err)
	}
	replacement := taskProjectionSnapshotFor(ids)
	replacement.Session.ThroughSequence = event.Sequence
	replacement.Task.Projection.LastSequence = event.Sequence
	return applySnapshot(t, next, replacement)
}

func disconnectedProjection(
	projection Projection,
	sequence uint64,
	policy sessionclient.RetryPolicy,
) Projection {
	return ProjectConnection(projection, sessionclient.Status{
		State: sessionclient.StateStopped, LastSequence: sequence,
	}, policy)
}

func reconnectingProjection(
	projection Projection,
	sequence uint64,
	policy sessionclient.RetryPolicy,
) Projection {
	projection = ProjectConnection(projection, sessionclient.Status{
		State: sessionclient.StateReconnecting, LastSequence: sequence,
		ReconnectCount: 1, Failure: sessionclient.FailureUnavailable,
	}, policy)
	return ProjectConnection(projection, sessionclient.Status{
		State: sessionclient.StateReplaying, LastSequence: sequence,
	}, policy)
}

type projectionTestIDs struct {
	session       domain.SessionID
	thread        domain.ThreadID
	task          domain.TaskID
	workspace     domain.WorkspaceID
	message       domain.MessageID
	approval      domain.ApprovalID
	validation    domain.ValidationID
	graphRevision domain.GraphRevisionID
	checkpoint    domain.CheckpointID
}

func newProjectionTestIDs(t *testing.T) projectionTestIDs {
	t.Helper()
	return projectionTestIDs{
		session:       mustIdentity(t, domain.NewSessionID),
		thread:        mustIdentity(t, domain.NewThreadID),
		task:          mustIdentity(t, domain.NewTaskID),
		workspace:     mustIdentity(t, domain.NewWorkspaceID),
		message:       mustIdentity(t, domain.NewMessageID),
		approval:      mustIdentity(t, domain.NewApprovalID),
		validation:    mustIdentity(t, domain.NewValidationID),
		graphRevision: mustIdentity(t, domain.NewGraphRevisionID),
		checkpoint:    mustIdentity(t, domain.NewCheckpointID),
	}
}

func mustIdentity[T any](t *testing.T, build func() (T, error)) T {
	t.Helper()
	value, err := build()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func snapshotFor(ids projectionTestIDs, sequence, graphRevision uint64) SessionSnapshot {
	taskID := ids.task
	return SessionSnapshot{
		Session: events.SessionSnapshot{
			SessionID: ids.session, ThreadID: ids.thread, ThroughSequence: sequence,
			TaskID: &taskID, TaskState: domain.TaskStateDraft, SnapshotVersion: 1,
			CreatedAt: time.UnixMicro(1).UTC(),
		},
		GraphRevision: graphRevision,
	}
}

func taskProjectionSnapshotFor(ids projectionTestIDs) SessionSnapshot {
	snapshot := snapshotFor(ids, 0, 0)
	snapshot.Task = &taskprojection.Snapshot{Projection: taskprojection.TaskProjection{
		TaskID: ids.task, State: domain.TaskStateDraft,
	}}
	return snapshot
}

func finalActiveTaskSnapshot(t *testing.T, ids projectionTestIDs) SessionSnapshot {
	t.Helper()
	usd, err := domain.ParseCurrencyCode("USD")
	if err != nil {
		t.Fatal(err)
	}
	money := domain.Money{Currency: usd, MinorUnits: 100}
	taskID := ids.task
	return SessionSnapshot{
		Session: events.SessionSnapshot{
			SessionID: ids.session, ThreadID: ids.thread, ThroughSequence: 9,
			TaskID: &taskID, TaskState: domain.TaskStateRunning, TaskRevision: 4,
			SnapshotVersion: 1, CreatedAt: time.UnixMicro(1).UTC(),
		},
		Task: &taskprojection.Snapshot{Projection: taskprojection.TaskProjection{
			TaskID: ids.task, State: domain.TaskStateRunning, Revision: 4, LastSequence: 9,
			Plan: taskprojection.PlanProjection{
				Present: true, Revision: 1, RedactedSummary: "plan",
				Approval: domain.ApprovalRequestStateGranted,
			},
			Approval: taskprojection.ApprovalProjection{
				Present: true, ID: ids.approval, State: domain.ApprovalRequestStateGranted,
				Scope: "network", Revision: 2,
			},
			Budget: taskprojection.BudgetProjection{
				Present: true, Revision: 1, HardLimit: money,
				Reserved: domain.Money{Currency: usd}, Actual: domain.Money{Currency: usd},
			},
			Validation: taskprojection.ValidationProjection{
				Present: true, ID: ids.validation, State: domain.ValidationStateRunning,
				Required: true, Revision: 1, DiffRevision: 1,
			},
		}},
	}
}

func applySnapshot(t *testing.T, projection Projection, snapshot SessionSnapshot) Projection {
	t.Helper()
	next, err := ApplySessionSnapshot(projection, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return next
}

func applyEvent(t *testing.T, projection Projection, event events.SessionEvent) Projection {
	t.Helper()
	next, err := ApplySessionEvent(projection, event)
	if err != nil {
		t.Fatal(err)
	}
	return next
}

func validEventForKind(
	t *testing.T,
	ids projectionTestIDs,
	kind events.Kind,
	sequence uint64,
	revision uint64,
) events.SessionEvent {
	t.Helper()
	usd, err := domain.ParseCurrencyCode("USD")
	if err != nil {
		t.Fatal(err)
	}
	money, err := domain.NewMoney(usd, 100)
	if err != nil {
		t.Fatal(err)
	}
	payloads := map[events.Kind]events.Payload{
		events.KindMessageDelta:      {MessageDelta: &events.MessageDelta{MessageID: ids.message, RedactedDelta: "delta"}},
		events.KindMessageFinal:      {MessageFinal: &events.MessageFinal{MessageID: ids.message, Role: "assistant", RedactedBody: "final"}},
		events.KindThreadCreated:     {ThreadCreated: &events.ThreadCreated{WorkspaceID: &ids.workspace, Title: "Thread"}},
		events.KindThreadRenamed:     {ThreadRenamed: &events.ThreadRenamed{PreviousTitle: "Thread", Title: "Renamed"}},
		events.KindThreadArchived:    {ThreadArchived: &events.ThreadArchived{Archived: true}},
		events.KindPlanCreated:       {Plan: &events.Plan{Revision: 1, RedactedSummary: "plan"}},
		events.KindPlanChanged:       {Plan: &events.Plan{Revision: 2, RedactedSummary: "changed"}},
		events.KindToolStarted:       {Tool: &events.Tool{ExecutionID: "execution", CommandName: "test", State: "running"}},
		events.KindToolProgress:      {Tool: &events.Tool{ExecutionID: "execution", CommandName: "test", State: "running", RedactedSummary: "progress"}},
		events.KindToolCompleted:     {Tool: &events.Tool{ExecutionID: "execution", CommandName: "test", State: "succeeded"}},
		events.KindApprovalRequested: {Approval: &events.Approval{ApprovalID: ids.approval, State: domain.ApprovalRequestStatePending, Scope: "network"}},
		events.KindApprovalResolved:  {Approval: &events.Approval{ApprovalID: ids.approval, State: domain.ApprovalRequestStateGranted, Scope: "network"}},
		events.KindTaskStateChanged:  {TaskStateChanged: &events.TaskStateChanged{From: domain.TaskStateDraft, To: domain.TaskStateForecasting}},
		events.KindForecastUpdated:   {Forecast: &events.Forecast{Range: domain.ForecastRange{}}},
		events.KindUsageUpdated:      {Usage: &events.Usage{Tokens: domain.TokenUsage{}}},
		events.KindCostUpdated:       {Cost: &events.Cost{}},
		events.KindBudgetUpdated:     {Budget: &events.Budget{HardLimit: money, Reserved: domain.Money{Currency: usd}, Actual: domain.Money{Currency: usd}}},
		events.KindValidationUpdated: {Validation: &events.Validation{ValidationID: ids.validation, State: domain.ValidationStateRunning, Required: true, DiffRevision: 1}},
		events.KindGraphSnapshot:     {Graph: &events.Graph{RevisionID: ids.graphRevision, EncodedChange: []byte{1}}},
		events.KindGraphPatch:        {Graph: &events.Graph{RevisionID: ids.graphRevision, EncodedChange: []byte{2}}},
		events.KindCheckpointCreated: {Checkpoint: &events.Checkpoint{CheckpointID: ids.checkpoint, TaskRevision: 1, PlanStep: "test"}},
		events.KindRecoveryRequired: {RecoveryRequired: &events.RecoveryRequired{
			CheckpointID: &ids.checkpoint, RedactedReason: "recovery required",
			Classification:    events.RecoveryAmbiguousOutcome,
			DivergenceSummary: "bounded divergence", ExternalOutcomeAmbiguous: true,
			PreservePatchAvailable: true,
			Bindings:               events.RevisionBindings{Diff: 1, Plan: 1, Validation: 1, Evidence: 1, Graph: 1},
		}},
		events.KindChangeAcceptanceUpdated: {ChangeAcceptance: &events.ChangeAcceptance{
			State:    domain.ChangeAcceptanceStatePending,
			Bindings: events.RevisionBindings{Diff: 1, Plan: 1, Validation: 1, Evidence: 1, Graph: 1},
		}},
		events.KindTaskProjectionInvalidated: {TaskProjectionInvalidated: &events.TaskProjectionInvalidated{
			Entity: "budget", Revision: revision,
		}},
		events.KindError: {Error: &events.UserError{Code: events.ErrorCodeProvider, RedactedMessage: "provider failed", Retryable: true}},
	}
	payload, ok := payloads[kind]
	if !ok {
		t.Fatalf("missing fixture for %q", kind)
	}
	taskID := ids.task
	event := events.SessionEvent{
		Sequence: sequence, SessionID: ids.session, ThreadID: ids.thread,
		TaskID: &taskID, Timestamp: time.UnixMicro(int64(sequence + 1)).UTC(),
		Kind: kind, Revision: revision, PayloadVersion: 1, Payload: payload,
	}
	if err := event.Validate(); err != nil {
		t.Fatalf("invalid %s fixture: %v", kind, err)
	}
	return event
}

// TestASecondTaskInAThreadReplacesTheProjectionInsteadOfFailingIt covers the
// ordinary case that used to disconnect the console: a person sends a second
// request in a thread, the coordinator creates another task, and its snapshot
// names a task the projection has never seen.
func TestASecondTaskInAThreadReplacesTheProjectionInsteadOfFailingIt(t *testing.T) {
	ids := newProjectionTestIDs(t)
	projection, err := ApplySessionSnapshot(New(), snapshotFor(ids, 3, 3))
	if err != nil {
		t.Fatal(err)
	}

	secondTask, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	next := snapshotFor(ids, 6, 6)
	next.Session.TaskID = &secondTask
	adopted, err := ApplySessionSnapshot(projection, next)
	if err != nil {
		t.Fatalf("a second task was refused: %v", err)
	}
	if adopted.Snapshot().Session.TaskID == nil || *adopted.Snapshot().Session.TaskID != secondTask {
		t.Fatalf("the projection kept the previous task: %+v", adopted.Snapshot().Session)
	}
	if adopted.Diagnostics().Repair != nil || adopted.LastAppliedSequence() != 6 {
		t.Fatalf("adoption left repair state: %+v", adopted.Diagnostics())
	}

	// A snapshot naming another task while sitting behind the applied cursor is
	// still stale, and still refused.
	stale := snapshotFor(ids, 2, 2)
	stale.Session.TaskID = &secondTask
	refused, err := ApplySessionSnapshot(adopted, stale)
	if !errors.Is(err, ErrSnapshotRepairRequired) || refused.LastAppliedSequence() != 6 {
		t.Fatalf("a stale second-task snapshot was adopted: %v", err)
	}
}
