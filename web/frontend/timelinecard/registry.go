package timelinecard

import (
	"fmt"
	"slices"

	"codeflux.dev/codeflux/internal/events"
)

type Announcement string

const (
	AnnouncementNone   Announcement = "none"
	AnnouncementPolite Announcement = "polite"
)

// ProjectionDisposition tells the timeline whether an event appends a new
// card, updates a stable existing card, or remains available only through its
// owning summary projection. It prevents routine usage and known-cost ticks
// from becoming timeline noise without dropping them from the event registry.
type ProjectionDisposition string

const (
	ProjectionAppend      ProjectionDisposition = "append"
	ProjectionUpsert      ProjectionDisposition = "upsert"
	ProjectionSummaryOnly ProjectionDisposition = "summary-only"
)

type ProjectionDecision struct {
	Disposition ProjectionDisposition
	Reason      string
}

// DecideProjection applies the deterministic progressive-disclosure policy
// for one validated durable event.
func DecideProjection(event events.SessionEvent) (ProjectionDecision, error) {
	if err := event.Validate(); err != nil {
		return ProjectionDecision{}, fmt.Errorf("decide timeline projection: %w", err)
	}
	switch event.Kind {
	case events.KindMessageDelta, events.KindMessageFinal,
		events.KindForecastUpdated,
		events.KindToolProgress, events.KindToolCompleted,
		events.KindApprovalResolved,
		events.KindBudgetUpdated,
		events.KindValidationUpdated,
		events.KindGraphSnapshot, events.KindGraphPatch:
		return ProjectionDecision{Disposition: ProjectionUpsert, Reason: "update stable timeline projection"}, nil
	case events.KindUsageUpdated:
		return ProjectionDecision{Disposition: ProjectionSummaryOnly, Reason: "routine usage belongs in measured summary"}, nil
	case events.KindCostUpdated:
		if event.Payload.Cost.Known {
			return ProjectionDecision{Disposition: ProjectionSummaryOnly, Reason: "routine known-cost tick"}, nil
		}
		return ProjectionDecision{Disposition: ProjectionUpsert, Reason: "unknown price requires visible budget context"}, nil
	default:
		return ProjectionDecision{Disposition: ProjectionAppend, Reason: "material durable event"}, nil
	}
}

// Descriptor documents the card or grouping rule for one durable event kind.
type Descriptor struct {
	EventKind          events.Kind
	RegistryName       string
	CardKind           Kind
	GroupingRule       string
	CorrectnessBearing bool
	Routine            bool
	Announcement       Announcement
}

var descriptors = []Descriptor{
	{events.KindMessageDelta, "session.message.delta", KindMessage, "group by message identity; append provisional delta", false, true, AnnouncementNone},
	{events.KindMessageFinal, "session.message.final", KindMessage, "group by message identity; replace deltas with durable final", true, false, AnnouncementPolite},
	{events.KindThreadCreated, "session.thread.created", KindThreadState, "one item for the authoritative initial thread projection", true, false, AnnouncementPolite},
	{events.KindThreadRenamed, "session.thread.renamed", KindThreadState, "one item per durable title revision", true, false, AnnouncementPolite},
	{events.KindThreadArchived, "session.thread.archived", KindThreadState, "one item per durable archive revision", true, false, AnnouncementPolite},
	{events.KindPlanCreated, "session.plan.created", KindPlan, "one item per durable sequence", false, false, AnnouncementPolite},
	{events.KindPlanChanged, "session.plan.changed", KindPlanRevision, "one revision item per durable sequence; retain plan history", false, false, AnnouncementPolite},
	{events.KindToolStarted, "session.tool.started", KindTool, "group by execution identity", false, false, AnnouncementPolite},
	{events.KindToolProgress, "session.tool.progress", KindTool, "group by execution identity; update in place", false, true, AnnouncementNone},
	{events.KindToolCompleted, "session.tool.completed", KindTool, "group by execution identity; finalize existing item", false, false, AnnouncementPolite},
	{events.KindApprovalRequested, "session.approval.requested", KindApproval, "group by approval identity", true, false, AnnouncementPolite},
	{events.KindApprovalResolved, "session.approval.resolved", KindApproval, "group by approval identity; retain attributable decision", true, false, AnnouncementPolite},
	{events.KindTaskStateChanged, "session.task.state.changed", KindTaskState, "one item per durable transition; terminal transitions may use completion detail", true, false, AnnouncementPolite},
	{events.KindForecastUpdated, "session.forecast.updated", KindForecast, "replace forecast projection at the latest sequence", false, false, AnnouncementPolite},
	{events.KindUsageUpdated, "session.usage.updated", KindUsage, "update measured usage without a routine card or announcement", false, true, AnnouncementNone},
	{events.KindCostUpdated, "session.cost.updated", KindCostBudget, "group routine ticks; emit a card only for a meaningful threshold", false, true, AnnouncementNone},
	{events.KindBudgetUpdated, "session.budget.updated", KindCostBudget, "one card for warning, cap, unknown price, material change, or approved increase", true, false, AnnouncementPolite},
	{events.KindValidationUpdated, "session.validation.updated", KindValidation, "group by validation identity and revision", true, false, AnnouncementPolite},
	{events.KindGraphSnapshot, "session.graph.snapshot", KindGraphChange, "replace graph projection; retain sequence in timeline registry", true, true, AnnouncementNone},
	{events.KindGraphPatch, "session.graph.patch", KindGraphChange, "apply once by sequence and revision identity", true, true, AnnouncementNone},
	{events.KindCheckpointCreated, "session.checkpoint.created", KindCheckpoint, "one item per checkpoint identity", true, false, AnnouncementPolite},
	{events.KindRecoveryRequired, "session.recovery.required", KindRecovery, "one blocking recovery item per durable sequence", true, false, AnnouncementPolite},
	{events.KindChangeAcceptanceUpdated, "session.change.acceptance.updated", KindUnknown, "retain the durable acceptance revision while task projection owns its bound decision state", true, false, AnnouncementPolite},
	{events.KindTaskProjectionInvalidated, "session.task.projection.invalidated", KindUnknown, "one content-free refresh notice per invalidated authoritative entity revision", true, false, AnnouncementPolite},
	{events.KindError, "session.error", KindError, "one safe error item per durable sequence", true, false, AnnouncementPolite},
}

func Registry() []Descriptor { return slices.Clone(descriptors) }

func Lookup(kind events.Kind) (Descriptor, bool) {
	for _, descriptor := range descriptors {
		if descriptor.EventKind == kind {
			return descriptor, true
		}
	}
	return Descriptor{}, false
}

// ValidateRegistry checks uniqueness and exact coverage of the generated
// durable event registry.
func ValidateRegistry() error {
	byName := make(map[string]Descriptor, len(descriptors))
	byKind := make(map[events.Kind]struct{}, len(descriptors))
	for _, descriptor := range descriptors {
		if descriptor.EventKind == "" || descriptor.RegistryName == "" ||
			descriptor.CardKind == "" || descriptor.GroupingRule == "" {
			return fmt.Errorf("timeline registry contains an incomplete descriptor")
		}
		if _, duplicate := byName[descriptor.RegistryName]; duplicate {
			return fmt.Errorf("timeline registry duplicates %q", descriptor.RegistryName)
		}
		if _, duplicate := byKind[descriptor.EventKind]; duplicate {
			return fmt.Errorf("timeline registry duplicates event kind %q", descriptor.EventKind)
		}
		byName[descriptor.RegistryName] = descriptor
		byKind[descriptor.EventKind] = struct{}{}
	}
	for _, generated := range events.Registry {
		if _, ok := byName[generated.Name]; !ok {
			return fmt.Errorf("durable event %q has no timeline presentation", generated.Name)
		}
	}
	if len(byName) != len(events.Registry) {
		return fmt.Errorf("timeline registry has %d entries for %d durable events", len(byName), len(events.Registry))
	}
	return nil
}
