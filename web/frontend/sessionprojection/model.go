// Package sessionprojection owns the pure frontend projection of one ordered
// authoritative session stream. Transport connection and retry execution stay
// in sessionclient; this package only derives safe UI state from that lifecycle.
package sessionprojection

import (
	"errors"
	"slices"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
	"codeflux.dev/codeflux/web/frontend/sessionclient"
	"codeflux.dev/codeflux/web/frontend/taskprojection"
)

var (
	ErrSnapshotRepairRequired     = errors.New("session projection requires snapshot repair")
	ErrManualReconnectUnavailable = errors.New("manual reconnect is unavailable")
)

// ConnectionState is the complete user-visible connection certainty model.
type ConnectionState string

const (
	ConnectionConnecting   ConnectionState = "connecting"
	ConnectionLive         ConnectionState = "live"
	ConnectionReplaying    ConnectionState = "replaying"
	ConnectionDegraded     ConnectionState = "degraded"
	ConnectionDisconnected ConnectionState = "disconnected"
	ConnectionIncompatible ConnectionState = "incompatible"
	ConnectionUnauthorized ConnectionState = "unauthorized"
)

// RetryDisposition explains retry authority without executing transport work.
type RetryDisposition string

const (
	RetryNone      RetryDisposition = "none"
	RetryAutomatic RetryDisposition = "automatic"
	RetryExhausted RetryDisposition = "exhausted"
	RetryBlocked   RetryDisposition = "blocked"
)

// RetryProjection is the bounded reconnect decision currently visible to UI.
type RetryProjection struct {
	Disposition RetryDisposition
	Attempt     int
	Maximum     int
	Delay       time.Duration
}

// ConnectionProjection separates stream certainty from backend task state.
type ConnectionProjection struct {
	State                    ConnectionState
	MutationsAllowed         bool
	ManualReconnectAvailable bool
	Retry                    RetryProjection
}

// RepairReason is a safe diagnostic category with no task or payload content.
type RepairReason string

const (
	RepairMissingSnapshot            RepairReason = "missing-snapshot"
	RepairInvalidSnapshot            RepairReason = "invalid-snapshot"
	RepairInvalidEvent               RepairReason = "invalid-event"
	RepairIdentityMismatch           RepairReason = "identity-mismatch"
	RepairSequenceGap                RepairReason = "sequence-gap"
	RepairTaskIdentityMismatch       RepairReason = "task-identity-mismatch"
	RepairTaskRevisionMismatch       RepairReason = "task-revision-mismatch"
	RepairTaskTransitionMismatch     RepairReason = "task-transition-mismatch"
	RepairGraphRevisionMismatch      RepairReason = "graph-revision-mismatch"
	RepairTaskProjectionUnsupported  RepairReason = "task-projection-unsupported"
	RepairTaskProjectionInconsistent RepairReason = "task-projection-inconsistent"
)

// SnapshotRepairRequest identifies the last trusted cursor and the rejected
// envelope. It deliberately excludes payloads and raw failure text.
type SnapshotRepairRequest struct {
	Reason           RepairReason
	AfterSequence    uint64
	ExpectedSequence uint64
	ReceivedSequence uint64
	EventKind        events.Kind
}

// Diagnostics is the bounded, content-free session projection diagnostic view.
type Diagnostics struct {
	LastAppliedSequence uint64
	AppliedEvents       uint64
	DuplicateEvents     uint64
	SequenceGaps        uint64
	LastEventKind       events.Kind
	Repair              *SnapshotRepairRequest
}

// SessionSnapshot is the authoritative reconnect base. GraphRevision is the
// numeric graph entity revision that graph patches must immediately follow.
type SessionSnapshot struct {
	Session       events.SessionSnapshot
	GraphRevision uint64
	// Task is the complete task reducer base at Session.ThroughSequence. It is
	// optional for sessions without a selected task; callers must not synthesize
	// missing plan, budget, validation, or review facts for a nonzero cursor.
	Task *taskprojection.Snapshot
}

// Projection is an immutable value. Its remote fields are private so only
// ApplySessionSnapshot and ApplySessionEvent can change authoritative state.
type Projection struct {
	snapshot    SessionSnapshot
	connection  ConnectionProjection
	diagnostics Diagnostics
	drafts      map[domain.ThreadID]string
}

// New returns an empty connecting projection with no authoritative snapshot.
func New() Projection {
	return Projection{
		connection: ConnectionProjection{State: ConnectionConnecting},
		drafts:     make(map[domain.ThreadID]string),
	}
}

func (projection Projection) Snapshot() SessionSnapshot {
	return cloneSessionSnapshot(projection.snapshot)
}

// TaskProjection returns a detached copy of the current authoritative task
// projection when the accepted session snapshot supplied one.
func (projection Projection) TaskProjection() (taskprojection.TaskProjection, bool) {
	if projection.snapshot.Task == nil {
		return taskprojection.TaskProjection{}, false
	}
	result, err := taskprojection.ApplySnapshot(*projection.snapshot.Task)
	if err != nil {
		return taskprojection.TaskProjection{}, false
	}
	return result, true
}
func (projection Projection) Connection() ConnectionProjection { return projection.connection }
func (projection Projection) Diagnostics() Diagnostics {
	result := projection.diagnostics
	result.Repair = cloneRepair(projection.diagnostics.Repair)
	return result
}
func (projection Projection) LastAppliedSequence() uint64 {
	return projection.diagnostics.LastAppliedSequence
}
func (projection Projection) SubscriptionAfterSequence() uint64 {
	return projection.diagnostics.LastAppliedSequence
}
func (projection Projection) Draft(threadID domain.ThreadID) string {
	return projection.drafts[threadID]
}

// WithDraft changes only ephemeral browser state and preserves all remote data.
func (projection Projection) WithDraft(threadID domain.ThreadID, draft string) Projection {
	next := cloneProjection(projection)
	if draft == "" {
		delete(next.drafts, threadID)
	} else {
		next.drafts[threadID] = draft
	}
	return next
}

// ReconnectRequest is a pure transport intent starting at the last trusted
// durable event; it never changes backend task state.
type ReconnectRequest struct{ AfterSequence uint64 }

// RequestManualReconnect moves disconnected presentation to connecting and
// returns the exact durable cursor for a new sessionclient subscription.
func RequestManualReconnect(projection Projection) (Projection, ReconnectRequest, error) {
	if !projection.connection.ManualReconnectAvailable {
		return projection, ReconnectRequest{}, ErrManualReconnectUnavailable
	}
	next := cloneProjection(projection)
	next.connection = ConnectionProjection{State: ConnectionConnecting}
	return next, ReconnectRequest{AfterSequence: next.LastAppliedSequence()}, nil
}

func cloneProjection(source Projection) Projection {
	result := source
	result.snapshot = cloneSessionSnapshot(source.snapshot)
	result.drafts = make(map[domain.ThreadID]string, len(source.drafts))
	for threadID, draft := range source.drafts {
		result.drafts[threadID] = draft
	}
	result.diagnostics.Repair = cloneRepair(source.diagnostics.Repair)
	return result
}

func cloneSessionSnapshot(source SessionSnapshot) SessionSnapshot {
	result := source
	if source.Session.TaskID != nil {
		taskID := *source.Session.TaskID
		result.Session.TaskID = &taskID
	}
	if source.Task != nil {
		projection, err := taskprojection.ApplySnapshot(*source.Task)
		if err == nil {
			result.Task = &taskprojection.Snapshot{Projection: projection}
		}
	}
	return result
}

func cloneRepair(source *SnapshotRepairRequest) *SnapshotRepairRequest {
	if source == nil {
		return nil
	}
	result := *source
	return &result
}

// EventKinds returns the generated durable event registry as reducer kinds.
// An unknown generated descriptor is retained as its canonical name so chaos
// coverage fails loudly instead of silently omitting a new category.
func EventKinds() []events.Kind {
	result := make([]events.Kind, 0, len(events.Registry))
	for _, descriptor := range slices.Clone(events.Registry) {
		if kind, ok := eventKindForRegistryName(descriptor.Name); ok {
			result = append(result, kind)
			continue
		}
		result = append(result, events.Kind(descriptor.Name))
	}
	return result
}

func eventKindForRegistryName(name string) (events.Kind, bool) {
	kinds := map[string]events.Kind{
		"session.message.delta":               events.KindMessageDelta,
		"session.message.final":               events.KindMessageFinal,
		"session.thread.created":              events.KindThreadCreated,
		"session.thread.renamed":              events.KindThreadRenamed,
		"session.thread.archived":             events.KindThreadArchived,
		"session.plan.created":                events.KindPlanCreated,
		"session.plan.changed":                events.KindPlanChanged,
		"session.tool.started":                events.KindToolStarted,
		"session.tool.progress":               events.KindToolProgress,
		"session.tool.completed":              events.KindToolCompleted,
		"session.approval.requested":          events.KindApprovalRequested,
		"session.approval.resolved":           events.KindApprovalResolved,
		"session.task.state.changed":          events.KindTaskStateChanged,
		"session.forecast.updated":            events.KindForecastUpdated,
		"session.usage.updated":               events.KindUsageUpdated,
		"session.cost.updated":                events.KindCostUpdated,
		"session.budget.updated":              events.KindBudgetUpdated,
		"session.validation.updated":          events.KindValidationUpdated,
		"session.graph.snapshot":              events.KindGraphSnapshot,
		"session.graph.patch":                 events.KindGraphPatch,
		"session.checkpoint.created":          events.KindCheckpointCreated,
		"session.recovery.required":           events.KindRecoveryRequired,
		"session.change.acceptance.updated":   events.KindChangeAcceptanceUpdated,
		"session.task.projection.invalidated": events.KindTaskProjectionInvalidated,
		"session.error":                       events.KindError,
	}
	kind, ok := kinds[name]
	return kind, ok
}

// ProjectConnection adapts the existing sessionclient lifecycle to the seven
// UI states and never starts, retries, or closes a transport.
func ProjectConnection(
	projection Projection,
	status sessionclient.Status,
	policy sessionclient.RetryPolicy,
) Projection {
	next := cloneProjection(projection)
	next.connection = connectionFromStatus(
		status,
		policy,
		next.diagnostics.LastAppliedSequence,
		next.diagnostics.Repair != nil,
	)
	return next
}

func connectionFromStatus(
	status sessionclient.Status,
	policy sessionclient.RetryPolicy,
	lastAppliedSequence uint64,
	repairPending bool,
) ConnectionProjection {
	normalized := sessionclient.NormalizedRetryPolicy(policy)
	result := ConnectionProjection{}
	switch status.State {
	case sessionclient.StateIdle, sessionclient.StateConnecting:
		result.State = ConnectionConnecting
	case sessionclient.StateReplaying:
		result.State = ConnectionReplaying
	case sessionclient.StateLive:
		result.State = ConnectionLive
	case sessionclient.StateReconnecting, sessionclient.StateGap:
		result.State = ConnectionDegraded
	case sessionclient.StateStopped:
		result.State = ConnectionDisconnected
	case sessionclient.StateFailed:
		switch status.Failure {
		case sessionclient.FailureAuthentication:
			result.State = ConnectionUnauthorized
		case sessionclient.FailureIncompatible:
			result.State = ConnectionIncompatible
		case sessionclient.FailureProtocol, sessionclient.FailureApplication:
			result.State = ConnectionDegraded
		default:
			result.State = ConnectionDisconnected
		}
	default:
		result.State = ConnectionDegraded
	}
	if repairPending {
		result.State = ConnectionDegraded
	}
	if result.State == ConnectionLive && status.LastSequence != lastAppliedSequence {
		result.State = ConnectionDegraded
	}
	result.MutationsAllowed = result.State == ConnectionLive && status.ControlsAllowed && !repairPending
	result.ManualReconnectAvailable = result.State == ConnectionDisconnected
	result.Retry = retryFromStatus(status, normalized)
	return result
}

func retryFromStatus(status sessionclient.Status, policy sessionclient.RetryPolicy) RetryProjection {
	result := RetryProjection{Disposition: RetryNone, Maximum: policy.MaxAttempts}
	switch {
	case status.State == sessionclient.StateReconnecting && status.ReconnectCount > 0 &&
		status.ReconnectCount < policy.MaxAttempts:
		result.Disposition = RetryAutomatic
		result.Attempt = status.ReconnectCount
		result.Delay = sessionclient.RetryBackoff(policy, status.ReconnectCount)
	case status.State == sessionclient.StateFailed &&
		(status.Failure == sessionclient.FailureAuthentication ||
			status.Failure == sessionclient.FailureIncompatible):
		result.Disposition = RetryBlocked
		result.Attempt = status.ReconnectCount
	case status.State == sessionclient.StateFailed && status.Failure == sessionclient.FailureUnavailable:
		result.Disposition = RetryExhausted
		result.Attempt = status.ReconnectCount
	}
	return result
}
