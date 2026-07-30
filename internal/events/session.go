package events

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

// Kind identifies one versioned session payload.
type Kind string

//codeflux:event session.message.delta
const KindMessageDelta Kind = "message-delta"

//codeflux:event session.message.final
const KindMessageFinal Kind = "message-final"

//codeflux:event session.plan.created
const KindPlanCreated Kind = "plan-created"

//codeflux:event session.plan.changed
const KindPlanChanged Kind = "plan-changed"

//codeflux:event session.tool.started
const KindToolStarted Kind = "tool-started"

//codeflux:event session.tool.progress
const KindToolProgress Kind = "tool-progress"

//codeflux:event session.tool.completed
const KindToolCompleted Kind = "tool-completed"

//codeflux:event session.approval.requested
const KindApprovalRequested Kind = "approval-requested"

//codeflux:event session.approval.resolved
const KindApprovalResolved Kind = "approval-resolved"

//codeflux:event session.task.state.changed
const KindTaskStateChanged Kind = "task-state-changed"

//codeflux:event session.forecast.updated
const KindForecastUpdated Kind = "forecast-updated"

//codeflux:event session.usage.updated
const KindUsageUpdated Kind = "usage-updated"

//codeflux:event session.cost.updated
const KindCostUpdated Kind = "cost-updated"

//codeflux:event session.budget.updated
const KindBudgetUpdated Kind = "budget-updated"

//codeflux:event session.validation.updated
const KindValidationUpdated Kind = "validation-updated"

//codeflux:event session.graph.snapshot
const KindGraphSnapshot Kind = "graph-snapshot"

//codeflux:event session.graph.patch
const KindGraphPatch Kind = "graph-patch"

//codeflux:event session.checkpoint.created
const KindCheckpointCreated Kind = "checkpoint-created"

//codeflux:event session.recovery.required
const KindRecoveryRequired Kind = "recovery-required"

//codeflux:event session.error
const KindError Kind = "error"

// DeliveryClass fixes backpressure behavior before subscriptions exist.
type DeliveryClass string

const (
	DeliveryEphemeralCoalescible DeliveryClass = "ephemeral-coalescible"
	DeliveryMaterial             DeliveryClass = "material"
)

// SessionEvent is one ordered, versioned, UI-safe session fact.
type SessionEvent struct {
	Sequence       uint64
	SessionID      domain.SessionID
	ThreadID       domain.ThreadID
	TaskID         *domain.TaskID
	Timestamp      time.Time
	Kind           Kind
	Revision       uint64
	CausationID    *domain.EventID
	CorrelationID  *domain.EventID
	PayloadVersion uint32
	Payload        Payload
}

// Payload is a typed one-of. Exactly the field selected by Kind must be set.
type Payload struct {
	MessageDelta     *MessageDelta
	MessageFinal     *MessageFinal
	Plan             *Plan
	Tool             *Tool
	Approval         *Approval
	TaskStateChanged *TaskStateChanged
	Forecast         *Forecast
	Usage            *Usage
	Cost             *Cost
	Budget           *Budget
	Validation       *Validation
	Graph            *Graph
	Checkpoint       *Checkpoint
	RecoveryRequired *RecoveryRequired
	Error            *UserError
}

type MessageDelta struct {
	MessageID     domain.MessageID
	RedactedDelta string
}

type MessageFinal struct {
	MessageID    domain.MessageID
	Role         string
	RedactedBody string
}

type Plan struct {
	Revision        uint64
	RedactedSummary string
}

type Tool struct {
	ExecutionID     string
	CommandName     string
	State           string
	RedactedSummary string
}

type Approval struct {
	ApprovalID     domain.ApprovalID
	State          domain.ApprovalRequestState
	Scope          string
	RedactedReason string
}

type TaskStateChanged struct {
	From     domain.TaskState
	To       domain.TaskState
	Approval domain.ApprovalRequestState
}

type Forecast struct {
	Range domain.ForecastRange
}

type Usage struct {
	Tokens domain.TokenUsage
}

type Cost struct {
	Known bool
	Value domain.Money
}

type Budget struct {
	HardLimit domain.Money
	Reserved  domain.Money
	Actual    domain.Money
}

type Validation struct {
	ValidationID    domain.ValidationID
	State           domain.ValidationState
	RedactedSummary string
}

type Graph struct {
	RevisionID    domain.GraphRevisionID
	EncodedChange []byte
}

type Checkpoint struct {
	CheckpointID domain.CheckpointID
	TaskRevision uint64
}

type RecoveryRequired struct {
	CheckpointID   *domain.CheckpointID
	RedactedReason string
}

// ErrorCode is a stable user-presentable failure classification.
type ErrorCode string

const (
	ErrorCodeInvalidRequest   ErrorCode = "invalid-request"
	ErrorCodeConflict         ErrorCode = "conflict"
	ErrorCodePermissionDenied ErrorCode = "permission-denied"
	ErrorCodeBudgetExhausted  ErrorCode = "budget-exhausted"
	ErrorCodeProvider         ErrorCode = "provider-failure"
	ErrorCodeValidation       ErrorCode = "validation-failure"
	ErrorCodeRecoveryRequired ErrorCode = "recovery-required"
	ErrorCodeInternal         ErrorCode = "internal"
)

type UserError struct {
	Code            ErrorCode
	RedactedMessage string
	Retryable       bool
}

// Validate rejects incomplete, mismatched, private, or unversioned envelopes.
func (event SessionEvent) Validate() error {
	switch {
	case event.Sequence == 0:
		return errors.New("session event sequence must be positive")
	case event.SessionID.IsZero():
		return errors.New("session event session ID must not be empty")
	case event.ThreadID.IsZero():
		return errors.New("session event thread ID must not be empty")
	case event.TaskID != nil && event.TaskID.IsZero():
		return errors.New("session event task ID must not be empty")
	case event.Timestamp.IsZero():
		return errors.New("session event timestamp must not be zero")
	case event.Timestamp.Location() != time.UTC:
		return errors.New("session event timestamp must be UTC")
	case event.PayloadVersion != 1:
		return errors.New("session event payload version is unsupported")
	case event.CausationID != nil && event.CausationID.IsZero():
		return errors.New("session event causation ID must not be empty")
	case event.CorrelationID != nil && event.CorrelationID.IsZero():
		return errors.New("session event correlation ID must not be empty")
	}
	if event.Payload.count() != 1 {
		return errors.New("session event must contain exactly one typed payload")
	}
	if err := event.validatePayload(); err != nil {
		return fmt.Errorf("validate %s payload: %w", event.Kind, err)
	}
	return nil
}

// DeliveryClass reports whether backpressure may coalesce this event.
func (event SessionEvent) DeliveryClass() DeliveryClass {
	switch event.Kind {
	case KindMessageDelta, KindToolProgress:
		return DeliveryEphemeralCoalescible
	default:
		return DeliveryMaterial
	}
}

// CorrectnessBearing reports whether this immutable event must never be
// dropped from durable history or delivery.
func (event SessionEvent) CorrectnessBearing() bool {
	switch event.Kind {
	case KindTaskStateChanged,
		KindApprovalRequested,
		KindApprovalResolved,
		KindBudgetUpdated,
		KindValidationUpdated,
		KindGraphSnapshot,
		KindGraphPatch,
		KindCheckpointCreated,
		KindRecoveryRequired,
		KindError:
		return true
	default:
		return false
	}
}

// SafeForUI is true only after full envelope validation. Payload fields are
// deliberately redacted or structural by contract.
func (event SessionEvent) SafeForUI() bool {
	return event.Validate() == nil
}

func (payload Payload) count() int {
	count := 0
	for _, present := range []bool{
		payload.MessageDelta != nil,
		payload.MessageFinal != nil,
		payload.Plan != nil,
		payload.Tool != nil,
		payload.Approval != nil,
		payload.TaskStateChanged != nil,
		payload.Forecast != nil,
		payload.Usage != nil,
		payload.Cost != nil,
		payload.Budget != nil,
		payload.Validation != nil,
		payload.Graph != nil,
		payload.Checkpoint != nil,
		payload.RecoveryRequired != nil,
		payload.Error != nil,
	} {
		if present {
			count++
		}
	}
	return count
}

func (event SessionEvent) validatePayload() error {
	switch event.Kind {
	case KindMessageDelta:
		return validateMessageDelta(event.Payload.MessageDelta)
	case KindMessageFinal:
		return validateMessageFinal(event.Payload.MessageFinal)
	case KindPlanCreated, KindPlanChanged:
		return validatePlan(event.Payload.Plan)
	case KindToolStarted, KindToolProgress, KindToolCompleted:
		return validateTool(event.Payload.Tool)
	case KindApprovalRequested, KindApprovalResolved:
		return validateApproval(event.Payload.Approval)
	case KindTaskStateChanged:
		return validateTaskState(event.Payload.TaskStateChanged)
	case KindForecastUpdated:
		if event.Payload.Forecast == nil {
			return errors.New("forecast payload is missing")
		}
		return event.Payload.Forecast.Range.Validate()
	case KindUsageUpdated:
		if event.Payload.Usage == nil {
			return errors.New("usage payload is missing")
		}
		return event.Payload.Usage.Tokens.Validate()
	case KindCostUpdated:
		return validateCost(event.Payload.Cost)
	case KindBudgetUpdated:
		return validateBudget(event.Payload.Budget)
	case KindValidationUpdated:
		return validateValidation(event.Payload.Validation)
	case KindGraphSnapshot, KindGraphPatch:
		return validateGraph(event.Payload.Graph)
	case KindCheckpointCreated:
		return validateCheckpoint(event.Payload.Checkpoint)
	case KindRecoveryRequired:
		return validateRecovery(event.Payload.RecoveryRequired)
	case KindError:
		return validateUserError(event.Payload.Error)
	default:
		return errors.New("event kind is invalid")
	}
}

func validateMessageDelta(value *MessageDelta) error {
	if value == nil || value.MessageID.IsZero() || value.RedactedDelta == "" {
		return errors.New("message delta is incomplete")
	}
	return nil
}

func validateMessageFinal(value *MessageFinal) error {
	if value == nil || value.MessageID.IsZero() ||
		strings.TrimSpace(value.Role) == "" {
		return errors.New("message final is incomplete")
	}
	return nil
}

func validatePlan(value *Plan) error {
	if value == nil || value.Revision == 0 || strings.TrimSpace(value.RedactedSummary) == "" {
		return errors.New("plan is incomplete")
	}
	return nil
}

func validateTool(value *Tool) error {
	if value == nil || strings.TrimSpace(value.ExecutionID) == "" ||
		strings.TrimSpace(value.CommandName) == "" ||
		strings.TrimSpace(value.State) == "" {
		return errors.New("tool event is incomplete")
	}
	return nil
}

func validateApproval(value *Approval) error {
	if value == nil || value.ApprovalID.IsZero() ||
		!value.State.IsValid() || strings.TrimSpace(value.Scope) == "" {
		return errors.New("approval event is incomplete")
	}
	return nil
}

func validateTaskState(value *TaskStateChanged) error {
	if value == nil {
		return errors.New("task state payload is missing")
	}
	return domain.ValidateTaskTransition(domain.TaskTransition{
		From:     value.From,
		To:       value.To,
		Approval: value.Approval,
	})
}

func validateCost(value *Cost) error {
	if value == nil {
		return errors.New("cost payload is missing")
	}
	if !value.Known {
		if value.Value != (domain.Money{}) {
			return errors.New("unknown cost must not contain a value")
		}
		return nil
	}
	if err := value.Value.Validate(); err != nil || value.Value.MinorUnits < 0 {
		return errors.New("known cost is invalid")
	}
	return nil
}

func validateBudget(value *Budget) error {
	if value == nil ||
		value.HardLimit.Validate() != nil ||
		value.Reserved.Validate() != nil ||
		value.Actual.Validate() != nil ||
		value.HardLimit.Currency != value.Reserved.Currency ||
		value.HardLimit.Currency != value.Actual.Currency ||
		value.HardLimit.MinorUnits < 0 ||
		value.Reserved.MinorUnits < 0 ||
		value.Actual.MinorUnits < 0 ||
		value.Actual.MinorUnits > value.HardLimit.MinorUnits ||
		value.Reserved.MinorUnits >
			value.HardLimit.MinorUnits-value.Actual.MinorUnits {
		return errors.New("budget payload is invalid")
	}
	return nil
}

func validateValidation(value *Validation) error {
	if value == nil || value.ValidationID.IsZero() || !value.State.IsValid() {
		return errors.New("validation payload is incomplete")
	}
	return nil
}

func validateGraph(value *Graph) error {
	if value == nil || value.RevisionID.IsZero() || len(value.EncodedChange) == 0 {
		return errors.New("graph payload is incomplete")
	}
	return nil
}

func validateCheckpoint(value *Checkpoint) error {
	if value == nil || value.CheckpointID.IsZero() {
		return errors.New("checkpoint payload is incomplete")
	}
	return nil
}

func validateRecovery(value *RecoveryRequired) error {
	if value == nil || strings.TrimSpace(value.RedactedReason) == "" ||
		value.CheckpointID != nil && value.CheckpointID.IsZero() {
		return errors.New("recovery payload is incomplete")
	}
	return nil
}

func validateUserError(value *UserError) error {
	if value == nil || !value.Code.valid() ||
		strings.TrimSpace(value.RedactedMessage) == "" {
		return errors.New("user error payload is incomplete")
	}
	return nil
}

func (code ErrorCode) valid() bool {
	switch code {
	case ErrorCodeInvalidRequest,
		ErrorCodeConflict,
		ErrorCodePermissionDenied,
		ErrorCodeBudgetExhausted,
		ErrorCodeProvider,
		ErrorCodeValidation,
		ErrorCodeRecoveryRequired,
		ErrorCodeInternal:
		return true
	default:
		return false
	}
}
