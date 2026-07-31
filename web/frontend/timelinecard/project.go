package timelinecard

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"codeflux.dev/codeflux/internal/events"
)

// Project converts one validated durable event into a typed card model.
func Project(event events.SessionEvent) (Card, error) {
	if err := event.Validate(); err != nil {
		return Card{}, fmt.Errorf("project timeline event: %w", err)
	}
	card := Card{
		Sequence: event.Sequence, OccurredAt: event.Timestamp,
		StableKey: "sequence:" + strconv.FormatUint(event.Sequence, 10),
	}
	switch event.Kind {
	case events.KindMessageDelta:
		value := event.Payload.MessageDelta
		card.Kind = KindMessage
		card.StableKey = "message:" + value.MessageID.String()
		card.Message = &Message{ID: value.MessageID.String(), Body: value.RedactedDelta, Status: MessageProvisional, OccurredAt: event.Timestamp}
	case events.KindMessageFinal:
		value := event.Payload.MessageFinal
		card.Kind = KindMessage
		card.StableKey = "message:" + value.MessageID.String()
		card.Message = &Message{ID: value.MessageID.String(), Role: value.Role, Body: value.RedactedBody, Status: MessageComplete, OccurredAt: event.Timestamp}
	case events.KindThreadCreated:
		value := event.Payload.ThreadCreated
		workspaceID := ""
		if value.WorkspaceID != nil {
			workspaceID = value.WorkspaceID.String()
		}
		card.Kind = KindThreadState
		card.ThreadState = &ThreadState{Action: "created", WorkspaceID: workspaceID, Title: value.Title, Archived: value.Archived}
	case events.KindThreadRenamed:
		value := event.Payload.ThreadRenamed
		card.Kind = KindThreadState
		card.ThreadState = &ThreadState{Action: "renamed", PreviousTitle: value.PreviousTitle, Title: value.Title}
	case events.KindThreadArchived:
		card.Kind = KindThreadState
		card.ThreadState = &ThreadState{Action: "archived", Archived: event.Payload.ThreadArchived.Archived}
	case events.KindPlanCreated:
		value := event.Payload.Plan
		card.Kind = KindPlan
		card.Plan = &Plan{Revision: value.Revision, Summary: value.RedactedSummary, ApprovalPending: true}
	case events.KindPlanChanged:
		value := event.Payload.Plan
		card.Kind = KindPlanRevision
		card.PlanRevision = &PlanRevision{CurrentRevision: value.Revision, Summary: value.RedactedSummary, ApprovalReset: true}
	case events.KindToolStarted, events.KindToolProgress, events.KindToolCompleted:
		value := event.Payload.Tool
		card.Kind = KindTool
		card.StableKey = "tool:" + value.ExecutionID
		card.Tool = &ToolActivity{ExecutionID: value.ExecutionID, Tool: value.CommandName, State: value.State, Summary: value.RedactedSummary}
	case events.KindApprovalRequested, events.KindApprovalResolved:
		value := event.Payload.Approval
		card.Kind = KindApproval
		card.StableKey = "approval:" + value.ApprovalID.String()
		card.Approval = &Approval{ID: value.ApprovalID.String(), Scope: value.Scope, Reason: value.RedactedReason, State: string(value.State)}
	case events.KindTaskStateChanged:
		value := event.Payload.TaskStateChanged
		card.Kind = KindTaskState
		card.TaskState = &TaskState{From: string(value.From), To: string(value.To), Approval: string(value.Approval)}
	case events.KindForecastUpdated:
		card.Kind = KindForecast
		card.StableKey = "forecast:current"
		card.Forecast = &Forecast{Range: event.Payload.Forecast.Range}
	case events.KindUsageUpdated:
		card.Kind = KindUsage
		card.StableKey = "usage:current"
		card.Usage = &Usage{Tokens: event.Payload.Usage.Tokens}
	case events.KindCostUpdated:
		value := event.Payload.Cost
		card.Kind = KindCostBudget
		card.StableKey = "cost-budget:current"
		card.CostBudget = &CostBudget{Known: value.Known, Actual: value.Value, Reason: "cost update"}
	case events.KindBudgetUpdated:
		value := event.Payload.Budget
		card.Kind = KindCostBudget
		card.StableKey = "cost-budget:current"
		card.CostBudget = &CostBudget{Known: true, Actual: value.Actual, HardLimit: value.HardLimit, Reserved: value.Reserved, Reason: "budget update"}
	case events.KindValidationUpdated:
		value := event.Payload.Validation
		card.Kind = KindValidation
		card.StableKey = "validation:" + value.ValidationID.String()
		card.Validation = &Validation{ID: value.ValidationID.String(), Status: validationStatus(string(value.State)), Summary: value.RedactedSummary}
	case events.KindGraphSnapshot, events.KindGraphPatch:
		value := event.Payload.Graph
		card.Kind = KindGraphChange
		card.StableKey = "graph:" + value.RevisionID.String()
		card.GraphChange = &GraphChange{RevisionID: value.RevisionID.String(), Patch: event.Kind == events.KindGraphPatch, ByteCount: len(value.EncodedChange)}
	case events.KindCheckpointCreated:
		value := event.Payload.Checkpoint
		card.Kind = KindCheckpoint
		card.StableKey = "checkpoint:" + value.CheckpointID.String()
		card.Checkpoint = &Checkpoint{ID: value.CheckpointID.String(), TaskRevision: value.TaskRevision}
	case events.KindRecoveryRequired:
		value := event.Payload.RecoveryRequired
		checkpointID := ""
		if value.CheckpointID != nil {
			checkpointID = value.CheckpointID.String()
		}
		card.Kind = KindRecovery
		card.Recovery = &Recovery{CheckpointID: checkpointID, Reason: value.RedactedReason}
	case events.KindError:
		value := event.Payload.Error
		card.Kind = KindError
		card.Error = &Error{Code: string(value.Code), Message: value.RedactedMessage, Retryable: value.Retryable}
	default:
		return UnknownFallback(string(event.Kind), event.Sequence, event.Timestamp, "unsupported durable event"), nil
	}
	if err := card.Validate(); err != nil {
		return Card{}, err
	}
	return card, nil
}

func validationStatus(value string) ValidationStatus {
	switch ValidationStatus(value) {
	case ValidationPending, ValidationRunning, ValidationPassed, ValidationFailed,
		ValidationWaived, ValidationSkipped, ValidationCancelled:
		return ValidationStatus(value)
	case "invalidated":
		return ValidationStale
	default:
		return ValidationUnavailable
	}
}

// UnknownFallback preserves identity and bounded safe details for forward
// compatibility. Its diagnostics target is a fixed internal route.
func UnknownFallback(kind string, sequence uint64, occurredAt time.Time, safeDetails string) Card {
	details := strings.Map(func(character rune) rune {
		if unicode.IsControl(character) && character != '\n' && character != '\t' {
			return -1
		}
		return character
	}, safeDetails)
	const maximumDetails = 4096
	if len(details) > maximumDetails {
		details = validPrefix(details, maximumDetails) + "…"
	}
	return Card{
		Kind: KindUnknown, Sequence: sequence, OccurredAt: occurredAt,
		StableKey: "sequence:" + strconv.FormatUint(sequence, 10),
		Unknown: &Unknown{
			EventKind: kind, OccurredAt: occurredAt, Sequence: sequence,
			SafeDetails: details, DiagnosticsPath: "/diagnostics",
		},
	}
}
