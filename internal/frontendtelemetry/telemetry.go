// Package frontendtelemetry defines the content-free local UX measurements
// emitted by the Go frontend. Its schema deliberately has no free-form payload,
// keystroke, prompt, source, tool-output, or hidden-content field.
package frontendtelemetry

import (
	"context"
	"errors"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

const (
	MaxStoredEvents = 10_000
	MaxQueryLimit   = 200
	DefaultLimit    = 50
)

type Kind string

const (
	KindFirstRunStep      Kind = "first-run-step"
	KindTimeToThread      Kind = "time-to-thread"
	KindTimeToMessage     Kind = "time-to-message"
	KindTimeToPlan        Kind = "time-to-plan"
	KindTimeToDiff        Kind = "time-to-diff"
	KindPlanDecision      Kind = "plan-decision"
	KindApprovalDecision  Kind = "approval-decision"
	KindTaskControl       Kind = "task-control"
	KindReviewDecision    Kind = "review-decision"
	KindGraphInteraction  Kind = "graph-interaction"
	KindMemoryInteraction Kind = "memory-interaction"
	KindReconnect         Kind = "reconnect"
	KindRecoveryAction    Kind = "recovery-action"
	KindFrontendError     Kind = "frontend-error"
	KindLongTask          Kind = "long-task"
	KindSlowRender        Kind = "slow-render"
)

var allKinds = []Kind{
	KindFirstRunStep, KindTimeToThread, KindTimeToMessage, KindTimeToPlan,
	KindTimeToDiff, KindPlanDecision, KindApprovalDecision, KindTaskControl,
	KindReviewDecision, KindGraphInteraction, KindMemoryInteraction,
	KindReconnect, KindRecoveryAction, KindFrontendError, KindLongTask,
	KindSlowRender,
}

func AllKinds() []Kind { return append([]Kind(nil), allKinds...) }

func (kind Kind) IsValid() bool {
	for _, candidate := range allKinds {
		if kind == candidate {
			return true
		}
	}
	return false
}

type Outcome string

const (
	OutcomeSucceeded         Outcome = "succeeded"
	OutcomeFailed            Outcome = "failed"
	OutcomeCancelled         Outcome = "cancelled"
	OutcomeApproved          Outcome = "approved"
	OutcomeDenied            Outcome = "denied"
	OutcomeExpired           Outcome = "expired"
	OutcomeRevisionRequested Outcome = "revision-requested"
	OutcomePaused            Outcome = "paused"
	OutcomeStopped           Outcome = "stopped"
	OutcomeOpened            Outcome = "opened"
	OutcomeNavigated         Outcome = "navigated"
	OutcomeInspected         Outcome = "inspected"
	OutcomeCorrected         Outcome = "corrected"
	OutcomeAccepted          Outcome = "accepted"
	OutcomeRepairRequested   Outcome = "repair-requested"
	OutcomeRejected          Outcome = "rejected"
	OutcomeRolledBack        Outcome = "rolled-back"
	OutcomeReconnected       Outcome = "reconnected"
	OutcomeSafeResumed       Outcome = "safe-resumed"
	OutcomeReconciled        Outcome = "reconciled"
	OutcomePatchPreserved    Outcome = "patch-preserved"
	OutcomeAbandoned         Outcome = "abandoned"
)

func (outcome Outcome) IsValid() bool {
	switch outcome {
	case OutcomeSucceeded, OutcomeFailed, OutcomeCancelled, OutcomeApproved,
		OutcomeDenied, OutcomeExpired, OutcomeRevisionRequested, OutcomePaused,
		OutcomeStopped, OutcomeOpened, OutcomeNavigated, OutcomeInspected,
		OutcomeCorrected, OutcomeAccepted, OutcomeRepairRequested,
		OutcomeRejected, OutcomeRolledBack, OutcomeReconnected,
		OutcomeSafeResumed, OutcomeReconciled, OutcomePatchPreserved,
		OutcomeAbandoned:
		return true
	default:
		return false
	}
}

type Component string

const (
	ComponentFirstRun Component = "first-run"
	ComponentThread   Component = "thread"
	ComponentComposer Component = "composer"
	ComponentPlan     Component = "plan"
	ComponentDiff     Component = "diff"
	ComponentApproval Component = "approval"
	ComponentTopBar   Component = "top-bar"
	ComponentReview   Component = "review"
	ComponentGraph    Component = "graph"
	ComponentMemory   Component = "memory"
	ComponentSession  Component = "session"
	ComponentRecovery Component = "recovery"
	ComponentTimeline Component = "timeline"
)

func (component Component) IsValid() bool {
	switch component {
	case ComponentFirstRun, ComponentThread, ComponentComposer, ComponentPlan,
		ComponentDiff, ComponentApproval, ComponentTopBar, ComponentReview,
		ComponentGraph, ComponentMemory, ComponentSession, ComponentRecovery,
		ComponentTimeline:
		return true
	default:
		return false
	}
}

type GraphMode string

const (
	GraphModeProgram   GraphMode = "program"
	GraphModeExecution GraphMode = "execution"
	GraphModeEvidence  GraphMode = "evidence"
)

func (mode GraphMode) IsValid() bool {
	return mode == GraphModeProgram || mode == GraphModeExecution || mode == GraphModeEvidence
}

type FailureClass string

const (
	FailureNone          FailureClass = "none"
	FailureInput         FailureClass = "input"
	FailureConfiguration FailureClass = "configuration"
	FailureDatabase      FailureClass = "database"
	FailureNetwork       FailureClass = "network"
	FailureAuthorization FailureClass = "authorization"
	FailureIncompatible  FailureClass = "incompatible"
	FailureProjection    FailureClass = "projection"
	FailureRender        FailureClass = "render"
	FailureTimeout       FailureClass = "timeout"
	FailureUnknown       FailureClass = "unknown"
)

func (failure FailureClass) IsValid() bool {
	switch failure {
	case FailureNone, FailureInput, FailureConfiguration, FailureDatabase,
		FailureNetwork, FailureAuthorization, FailureIncompatible,
		FailureProjection, FailureRender, FailureTimeout, FailureUnknown:
		return true
	default:
		return false
	}
}

type Event struct {
	ID           uint64
	Kind         Kind
	OccurredAt   time.Time
	Duration     time.Duration
	Outcome      Outcome
	Component    Component
	GraphMode    GraphMode
	FailureClass FailureClass
	TaskID       domain.TaskID
	ThreadID     domain.ThreadID
	SessionID    domain.SessionID
	Sequence     uint64
	Revision     uint64
}

func (event Event) ValidateForRecord() error {
	if !event.Kind.IsValid() || !event.Outcome.IsValid() || !event.Component.IsValid() {
		return errors.New("frontend telemetry kind, outcome, or component is invalid")
	}
	if event.ID != 0 || !event.OccurredAt.IsZero() || event.Duration < 0 {
		return errors.New("frontend telemetry record contains store-owned or negative values")
	}
	if event.FailureClass == "" {
		event.FailureClass = FailureNone
	}
	if !event.FailureClass.IsValid() {
		return errors.New("frontend telemetry failure class is invalid")
	}
	if event.GraphMode != "" && !event.GraphMode.IsValid() {
		return errors.New("frontend telemetry graph mode is invalid")
	}
	if event.Outcome == OutcomeFailed && event.FailureClass == FailureNone {
		return errors.New("failed frontend telemetry requires a failure class")
	}
	return validateShape(event)
}

func validateShape(event Event) error {
	requireTask := func() error {
		if event.TaskID.IsZero() {
			return errors.New("frontend telemetry event requires a task identity")
		}
		return nil
	}
	requireDuration := func() error {
		if event.Duration <= 0 {
			return errors.New("frontend telemetry timing requires a positive duration")
		}
		return nil
	}
	switch event.Kind {
	case KindFirstRunStep:
		if event.Component != ComponentFirstRun || !oneOf(event.Outcome, OutcomeSucceeded, OutcomeFailed, OutcomeCancelled) {
			return errors.New("first-run telemetry shape is invalid")
		}
	case KindTimeToThread, KindTimeToMessage:
		if err := requireDuration(); err != nil || event.Outcome != OutcomeSucceeded {
			return errors.New("first-use timing telemetry shape is invalid")
		}
	case KindTimeToPlan, KindTimeToDiff:
		if err := requireTask(); err != nil {
			return err
		}
		if err := requireDuration(); err != nil || event.Outcome != OutcomeSucceeded {
			return errors.New("task timing telemetry shape is invalid")
		}
	case KindPlanDecision:
		if err := requireTask(); err != nil {
			return err
		}
		if !oneOf(event.Outcome, OutcomeApproved, OutcomeRevisionRequested) {
			return errors.New("plan decision telemetry shape is invalid")
		}
	case KindApprovalDecision:
		if err := requireTask(); err != nil {
			return err
		}
		if !oneOf(event.Outcome, OutcomeApproved, OutcomeDenied, OutcomeExpired, OutcomeCancelled) {
			return errors.New("approval decision telemetry shape is invalid")
		}
	case KindTaskControl:
		if err := requireTask(); err != nil {
			return err
		}
		if !oneOf(event.Outcome, OutcomePaused, OutcomeStopped) {
			return errors.New("task control telemetry shape is invalid")
		}
	case KindReviewDecision:
		if err := requireTask(); err != nil {
			return err
		}
		if !oneOf(event.Outcome, OutcomeOpened, OutcomeAccepted, OutcomeRepairRequested, OutcomeRejected, OutcomeRolledBack) {
			return errors.New("review telemetry shape is invalid")
		}
	case KindGraphInteraction:
		if err := requireTask(); err != nil {
			return err
		}
		if !event.GraphMode.IsValid() || !oneOf(event.Outcome, OutcomeOpened, OutcomeNavigated) {
			return errors.New("graph telemetry shape is invalid")
		}
	case KindMemoryInteraction:
		if !oneOf(event.Outcome, OutcomeInspected, OutcomeCorrected) {
			return errors.New("memory telemetry shape is invalid")
		}
	case KindReconnect:
		if event.SessionID.IsZero() {
			return errors.New("reconnect telemetry requires a session identity")
		}
		if err := requireDuration(); err != nil || !oneOf(event.Outcome, OutcomeReconnected, OutcomeFailed) {
			return errors.New("reconnect telemetry shape is invalid")
		}
	case KindRecoveryAction:
		if err := requireTask(); err != nil {
			return err
		}
		if !oneOf(event.Outcome, OutcomeSafeResumed, OutcomeReconciled, OutcomePatchPreserved, OutcomeAbandoned) {
			return errors.New("recovery telemetry shape is invalid")
		}
	case KindFrontendError:
		if event.Outcome != OutcomeFailed || event.FailureClass == FailureNone {
			return errors.New("frontend error telemetry shape is invalid")
		}
	case KindLongTask:
		if err := requireTask(); err != nil {
			return err
		}
		if err := requireDuration(); err != nil {
			return err
		}
	case KindSlowRender:
		if event.Duration < 50*time.Millisecond || event.Outcome != OutcomeSucceeded {
			return errors.New("slow-render telemetry must be a render of at least 50 milliseconds")
		}
	}
	return nil
}

func oneOf(value Outcome, candidates ...Outcome) bool {
	for _, candidate := range candidates {
		if value == candidate {
			return true
		}
	}
	return false
}

// Diagnostic is the identity-free form suitable for logs and support output.
// The local inspection API may return Event when the user explicitly opens it.
type Diagnostic struct {
	Kind         Kind
	OccurredAt   time.Time
	Duration     time.Duration
	Outcome      Outcome
	Component    Component
	GraphMode    GraphMode
	FailureClass FailureClass
	HasTask      bool
	HasThread    bool
	HasSession   bool
	HasSequence  bool
	HasRevision  bool
}

func (event Event) RedactedDiagnostic() Diagnostic {
	return Diagnostic{
		Kind: event.Kind, OccurredAt: event.OccurredAt, Duration: event.Duration,
		Outcome: event.Outcome, Component: event.Component, GraphMode: event.GraphMode,
		FailureClass: event.FailureClass, HasTask: !event.TaskID.IsZero(),
		HasThread: !event.ThreadID.IsZero(), HasSession: !event.SessionID.IsZero(),
		HasSequence: event.Sequence != 0, HasRevision: event.Revision != 0,
	}
}

type Query struct {
	BeforeID uint64
	Since    time.Time
	Until    time.Time
	Kinds    []Kind
	Limit    int
}

func (query Query) Validate() error {
	if query.Limit < 0 || query.Limit > MaxQueryLimit || len(query.Kinds) > len(allKinds) {
		return errors.New("frontend telemetry query exceeds bounded limits")
	}
	if !query.Since.IsZero() && !query.Until.IsZero() && query.Since.After(query.Until) {
		return errors.New("frontend telemetry query time range is inverted")
	}
	seen := make(map[Kind]struct{}, len(query.Kinds))
	for _, kind := range query.Kinds {
		if !kind.IsValid() {
			return errors.New("frontend telemetry query kind is invalid")
		}
		if _, duplicate := seen[kind]; duplicate {
			return errors.New("frontend telemetry query kind is duplicated")
		}
		seen[kind] = struct{}{}
	}
	return nil
}

type Page struct {
	Events       []Event
	NextBeforeID uint64
}

type DeleteScope string

const (
	DeleteAll    DeleteScope = "all"
	DeleteBefore DeleteScope = "before"
)

type DeleteConfirmation string

const ConfirmTelemetryDeletion DeleteConfirmation = "confirm-telemetry-deletion"

type DeleteRequest struct {
	Scope        DeleteScope
	Before       time.Time
	Confirmation DeleteConfirmation
}

func (request DeleteRequest) Validate() error {
	if request.Confirmation != ConfirmTelemetryDeletion {
		return errors.New("frontend telemetry deletion requires explicit confirmation")
	}
	switch request.Scope {
	case DeleteAll:
		if !request.Before.IsZero() {
			return errors.New("delete-all telemetry request must not include a cutoff")
		}
	case DeleteBefore:
		if request.Before.IsZero() {
			return errors.New("delete-before telemetry request requires a cutoff")
		}
	default:
		return errors.New("frontend telemetry deletion scope is invalid")
	}
	return nil
}

type DeleteResult struct {
	Deleted   uint64
	Remaining uint64
}

// Store is the bounded local persistence contract used by the frontend-facing
// application service. Implementations must preserve the content-free schema.
type Store interface {
	RecordFrontendTelemetry(context.Context, Event) (Event, error)
	ListFrontendTelemetry(context.Context, Query) (Page, error)
	DeleteFrontendTelemetry(context.Context, DeleteRequest) (DeleteResult, error)
}
