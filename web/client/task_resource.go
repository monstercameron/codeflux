package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/routes"
	frontendstate "codeflux.dev/codeflux/web/frontend/state"
	"codeflux.dev/codeflux/web/frontend/taskcontrols"
	"codeflux.dev/codeflux/web/frontend/taskprojection"
	"codeflux.dev/codeflux/web/frontend/threadrail"
	"google.golang.org/grpc"
)

var (
	errTaskResourceSelectionUnavailable = errors.New("authoritative task selection is unavailable")
	errTaskResourceBridgeUnavailable    = errors.New("authoritative task bridge is unavailable")
	errTaskResourceMalformed            = errors.New("authoritative task resource is malformed")
)

type taskResourceScope struct {
	taskID   domain.TaskID
	threadID domain.ThreadID
}

// decorateTaskControlsFromProjection mounts recovery and review detail accepted
// by the durable session projection. Identity, lifecycle, revisions, budget,
// and action availability are already derived from this same projection by
// decodeTaskControlProps; TaskView is query metadata only.
func decorateTaskControlsFromProjection(
	props *taskcontrols.Props,
	projection taskprojection.TaskProjection,
) {
	if props == nil || props.TaskID.IsZero() || projection.TaskID != props.TaskID {
		return
	}
	decorateProjectedReviewStaleness(props, projection)
	detail := projection.RecoveryDetail
	if !detail.Present {
		return
	}

	recovery := props.Recovery
	recovery.Required = true
	recovery.KnownState = fmt.Sprintf(
		"The coordinator reports %s recovery at recovery revision %d; the loaded task snapshot is revision %d.",
		detail.Classification, detail.Revision, props.TaskRevision,
	)
	recovery.DivergenceSummary = detail.DivergenceSummary
	recovery.ExternalOutcomeAmbiguous = detail.ExternalOutcomeAmbiguous
	recovery.SafeResumeVerified = detail.SafeResumeVerified
	recovery.ReconcileRequired = detail.ReconcileAvailable
	recovery.PatchPreservable = detail.PreservePatchAvailable
	recovery.Ambiguity = projectedRecoveryAmbiguity(projection)
	recovery.SafestRecommendation = projectedRecoveryRecommendation(detail)

	if checkpointMatchesRecovery(projection.Checkpoint, detail) {
		recovery.LastCheckpointAt = projection.Checkpoint.CreatedAt
		recovery.LastCheckpointPlanStep = projection.Checkpoint.PlanStep
	}
	if detail.SafeResumeVerified {
		recovery.SafeResume = props.Controls.Resume
		if props.OnResume != nil {
			props.OnSafeResume = props.OnResume
		}
		if props.OnSafeResume == nil && strings.TrimSpace(recovery.SafeResume.DisabledReason) == "" {
			recovery.SafeResume.DisabledReason = "Verified resume dispatch is unavailable until the live task command bridge is ready."
		}
	}
	if detail.ReconcileAvailable && props.OnReconcile == nil {
		recovery.Reconcile.DisabledReason = "The coordinator API does not yet expose a typed reconcile command."
	}
	if detail.PreservePatchAvailable && props.OnPreservePatch == nil {
		recovery.PreservePatch.DisabledReason = "The coordinator API does not yet expose a typed preserve-patch command."
	}
	recovery.Details = projectedRecoveryDetails(detail)
	props.Recovery = recovery
}

func checkpointMatchesRecovery(
	checkpoint taskprojection.CheckpointProjection,
	recovery taskprojection.RecoveryProjection,
) bool {
	return checkpoint.Present && recovery.CheckpointID != nil && checkpoint.ID == *recovery.CheckpointID
}

func projectedRecoveryAmbiguity(projection taskprojection.TaskProjection) string {
	detail := projection.RecoveryDetail
	parts := make([]string, 0, 2)
	if detail.ExternalOutcomeAmbiguous {
		parts = append(parts, "The outcome of at least one external action remains ambiguous.")
	} else {
		parts = append(parts, "No ambiguous external action outcome is reported by the authoritative recovery event.")
	}
	if detail.CheckpointID == nil {
		parts = append(parts, "No recovery checkpoint identity was supplied.")
	} else if !checkpointMatchesRecovery(projection.Checkpoint, detail) {
		parts = append(parts, "The checkpoint identity is known, but its time and plan step are not present in the accepted projection.")
	}
	return strings.Join(parts, " ")
}

func projectedRecoveryRecommendation(detail taskprojection.RecoveryProjection) string {
	prefix := strings.TrimSpace(detail.SafeReason)
	if prefix != "" {
		prefix += " "
	}
	switch {
	case detail.Classification == taskprojection.RecoverySafeResume && detail.SafeResumeVerified:
		return prefix + "Resume only from the verified checkpoint."
	case detail.Classification == taskprojection.RecoveryNeedsReconcile && detail.ReconcileAvailable:
		return prefix + "Reconcile the authoritative worktree state before continuing."
	case detail.PreservePatchAvailable:
		return prefix + "Preserve the current patch before any destructive recovery step."
	default:
		return prefix + "Inspect authoritative recovery evidence before choosing an action."
	}
}

func projectedRecoveryDetails(detail taskprojection.RecoveryProjection) []taskcontrols.RecoveryDetail {
	result := make([]taskcontrols.RecoveryDetail, 0, len(detail.RelatedEventIDs)+len(detail.RelatedFiles))
	for _, eventID := range detail.RelatedEventIDs {
		result = append(result, taskcontrols.RecoveryDetail{
			Kind: taskcontrols.RecoveryDetailEvent, Identity: eventID.String(),
			Label: "Recovery event " + eventID.String(),
		})
	}
	for _, path := range detail.RelatedFiles {
		item := taskcontrols.RecoveryDetail{
			Kind: taskcontrols.RecoveryDetailFile, Identity: path, Label: path,
		}
		if _, err := routes.ValidateWorkspaceRelativePath(path); err != nil {
			item.DisabledReason = "This file identity is not a safe workspace-relative path."
		}
		result = append(result, item)
	}
	return result
}

func decorateProjectedReviewStaleness(props *taskcontrols.Props, projection taskprojection.TaskProjection) {
	if !projection.Acceptance.Present {
		return
	}
	props.Review = taskcontrols.ReviewStaleness{}
	reasons := make([]string, 0, 5)
	accepted := projection.Acceptance.Bindings
	if projection.Review.Present {
		reasons = appendRevisionBindingMismatches(reasons, "review", projection.Review.Bindings, accepted)
	}
	if projection.Plan.Present && accepted.Plan != projection.Plan.Revision {
		reasons = append(reasons, fmt.Sprintf("plan revision changed from %d to %d", accepted.Plan, projection.Plan.Revision))
	}
	if projection.Validation.Present {
		if accepted.Validation != projection.Validation.Revision {
			reasons = append(reasons, fmt.Sprintf("validation revision changed from %d to %d", accepted.Validation, projection.Validation.Revision))
		}
		if accepted.Diff != projection.Validation.DiffRevision {
			reasons = append(reasons, fmt.Sprintf("validated diff revision changed from %d to %d", accepted.Diff, projection.Validation.DiffRevision))
		}
	}
	if projection.Graph.Present && accepted.Graph != projection.Graph.Revision {
		reasons = append(reasons, fmt.Sprintf("graph revision changed from %d to %d", accepted.Graph, projection.Graph.Revision))
	}
	if len(reasons) > 0 {
		props.Review = taskcontrols.ReviewStaleness{Stale: true, Reasons: reasons}
	}
}

func appendRevisionBindingMismatches(
	reasons []string,
	source string,
	current taskprojection.RevisionBindings,
	accepted taskprojection.RevisionBindings,
) []string {
	values := []struct {
		name       string
		current    uint64
		acceptedAt uint64
	}{
		{"diff", current.Diff, accepted.Diff},
		{"plan", current.Plan, accepted.Plan},
		{"validation", current.Validation, accepted.Validation},
		{"evidence", current.Evidence, accepted.Evidence},
		{"graph", current.Graph, accepted.Graph},
	}
	for _, value := range values {
		if value.current != value.acceptedAt {
			reasons = append(reasons, fmt.Sprintf(
				"%s %s revision changed from %d to %d",
				source, value.name, value.acceptedAt, value.current,
			))
		}
	}
	return reasons
}

func selectedTaskResourceScope(thread threadrail.Thread) (taskResourceScope, error) {
	if thread.ID().IsZero() || thread.TaskID().IsZero() {
		return taskResourceScope{}, errTaskResourceSelectionUnavailable
	}
	return taskResourceScope{taskID: thread.TaskID(), threadID: thread.ID()}, nil
}

type taskViewClient interface {
	GetTask(context.Context, *codefluxv1.GetTaskRequest, ...grpc.CallOption) (*codefluxv1.GetTaskResponse, error)
}

type taskViewLease struct {
	client taskViewClient
	close  func() error
}

type taskViewClientOpener func(context.Context) (taskViewLease, error)

func loadTaskControlProps(
	ctx context.Context,
	opener taskViewClientOpener,
	scope taskResourceScope,
	session frontendstate.SessionView,
	projection taskprojection.TaskProjection,
) (taskcontrols.Props, error) {
	if scope.taskID.IsZero() || scope.threadID.IsZero() {
		return taskcontrols.Props{}, errTaskResourceSelectionUnavailable
	}
	if opener == nil {
		return taskcontrols.Props{}, errTaskResourceBridgeUnavailable
	}
	lease, err := opener(ctx)
	if err != nil {
		return taskcontrols.Props{}, err
	}
	if lease.client == nil || lease.close == nil {
		return taskcontrols.Props{}, errTaskResourceBridgeUnavailable
	}
	defer lease.close()
	response, err := lease.client.GetTask(ctx, &codefluxv1.GetTaskRequest{
		TaskId: taskIdentity(scope.taskID),
	})
	if err != nil {
		return taskcontrols.Props{}, err
	}
	if response == nil {
		return taskcontrols.Props{}, fmt.Errorf("%w: missing GetTask response", errTaskResourceMalformed)
	}
	return decodeTaskControlProps(response.GetTask(), scope, session, projection)
}

func decodeTaskControlProps(
	view *codefluxv1.TaskView,
	scope taskResourceScope,
	session frontendstate.SessionView,
	projection taskprojection.TaskProjection,
) (taskcontrols.Props, error) {
	if view == nil {
		return taskcontrols.Props{}, fmt.Errorf("%w: missing task", errTaskResourceMalformed)
	}
	taskID, err := decodeTaskIdentity(view.GetTaskId())
	if err != nil || taskID != scope.taskID {
		return taskcontrols.Props{}, fmt.Errorf("%w: task identity mismatch", errTaskResourceMalformed)
	}
	threadID, err := decodeThreadIdentity(view.GetThreadId())
	if err != nil || threadID != scope.threadID {
		return taskcontrols.Props{}, fmt.Errorf("%w: thread identity mismatch", errTaskResourceMalformed)
	}
	if projection.TaskID.IsZero() || projection.TaskID != scope.taskID || !projection.State.IsValid() {
		return taskcontrols.Props{}, fmt.Errorf("%w: authoritative task projection mismatch", errTaskResourceMalformed)
	}
	usage := domain.TokenUsage{}
	if view.GetActualTokens() != nil {
		usage = domain.TokenUsage{
			Known: true,
			ProviderSpecific: map[string]domain.TokenCount{
				"authoritative task total": domain.TokenCount(view.GetActualTokens().GetTokens()),
			},
		}
	}
	pricing, pricingErr := normalizedPricingSnapshotIDs(view.GetActualPricingSnapshotIds())
	if pricingErr != nil {
		return taskcontrols.Props{}, fmt.Errorf("%w: pricing snapshot metadata", errTaskResourceMalformed)
	}
	cost := projectedActualCostView(projection, pricing)
	forecastView, err := decodeTaskForecast(view.GetForecast())
	if err != nil {
		return taskcontrols.Props{}, fmt.Errorf("%w: forecast: %v", errTaskResourceMalformed, err)
	}
	delivery := taskDeliveryView(session)
	props := taskcontrols.Props{
		TaskID:              projection.TaskID,
		TaskRevision:        projection.Revision,
		BudgetRevision:      projection.Budget.Revision,
		BudgetRevisionKnown: projection.Budget.Present,
		TaskSummary:         strings.TrimSpace(view.GetSummary().GetValue()),
		TaskState:           projection.State,
		Phase:               taskPhase(projection.State),
		Delivery:            delivery,
		Selection: taskcontrols.SelectionView{
			Provider: strings.TrimSpace(view.GetSelectedProvider()),
			Model:    strings.TrimSpace(view.GetSelectedModel()),
			Effort:   strings.TrimSpace(view.GetSelectedEffort()),
		},
		Forecast: forecastView,
		Usage:    taskcontrols.UsageView{Tokens: usage, Cost: cost},
		Budget:   projectedBudgetView(projection),
		Controls: projectedControlState(projection, taskprojection.ConnectionProjection(session.Connection)),
	}
	if projection.State == domain.TaskStateRecoveryRequired || projection.Recovery != taskprojection.RecoveryNone {
		knownState := fmt.Sprintf("The coordinator reports %s recovery at task revision %d.", projection.Recovery, projection.Revision)
		if projection.Plan.Present {
			knownState = fmt.Sprintf("The coordinator reports %s recovery at task revision %d and plan revision %d.",
				projection.Recovery, projection.Revision, projection.Plan.Revision)
		}
		props.Recovery = taskcontrols.RecoveryView{
			Required:             true,
			KnownState:           knownState,
			Ambiguity:            "The authoritative projection has not yet supplied complete recovery detail.",
			SafestRecommendation: "Inspect authoritative recovery events before choosing a recovery action.",
		}
	}
	decorateTaskControlsFromProjection(&props, projection)
	if err := props.Validate(); err != nil {
		return taskcontrols.Props{}, fmt.Errorf("%w: %v", errTaskResourceMalformed, err)
	}
	return props, nil
}

func taskStopRequiresConfirmation(state domain.TaskState) bool {
	switch state {
	case domain.TaskStateForecasting, domain.TaskStateRunning,
		domain.TaskStateAwaitingAuthority, domain.TaskStateValidating:
		return true
	default:
		return false
	}
}

func projectedActualCostView(
	projection taskprojection.TaskProjection,
	pricing []string,
) taskcontrols.ActualCostView {
	if !projection.Budget.Present {
		return taskcontrols.ActualCostView{UnknownReason: "authoritative priced usage has not arrived"}
	}
	pricingSnapshot := strings.Join(pricing, ", ")
	if pricingSnapshot == "" {
		pricingSnapshot = "authoritative session budget projection"
	}
	return taskcontrols.ActualCostView{
		Known: true, Value: projection.Budget.Actual, PricingSnapshot: pricingSnapshot,
	}
}

func projectedBudgetView(projection taskprojection.TaskProjection) taskcontrols.BudgetView {
	if !projection.Budget.Present {
		return taskcontrols.BudgetView{}
	}
	budget := taskcontrols.BudgetView{
		HardLimitKnown: true,
		HardLimit:      projection.Budget.HardLimit,
		RemainingKnown: true,
		Remaining: domain.Money{
			Currency: projection.Budget.HardLimit.Currency,
		},
	}
	spent, err := projection.Budget.Reserved.Add(projection.Budget.Actual)
	if err != nil || spent.MinorUnits >= projection.Budget.HardLimit.MinorUnits {
		budget.HardCapReached = true
	} else {
		budget.Remaining.MinorUnits = projection.Budget.HardLimit.MinorUnits - spent.MinorUnits
	}
	if projection.Checkpoint.Present {
		budget.CheckpointKnown = true
		budget.CheckpointedState = "checkpoint " + projection.Checkpoint.ID.String() +
			" at plan step " + projection.Checkpoint.PlanStep
	}
	return budget
}

func projectedControlState(
	projection taskprojection.TaskProjection,
	connection taskprojection.ConnectionProjection,
) taskcontrols.ControlState {
	const unavailable = "This action is unavailable in the current authoritative task projection."
	controls := taskcontrols.ControlState{
		Pause:        taskcontrols.CommandState{DisabledReason: unavailable},
		Resume:       taskcontrols.CommandState{DisabledReason: unavailable},
		Stop:         taskcontrols.CommandState{DisabledReason: unavailable},
		AdjustBudget: taskcontrols.CommandState{DisabledReason: unavailable},
	}
	for _, decision := range taskprojection.AvailableTaskActions(projection, connection) {
		reason := decision.Reason
		if decision.Enabled {
			reason = ""
		}
		switch decision.Kind {
		case taskprojection.ActionPause:
			controls.Pause.DisabledReason = reason
		case taskprojection.ActionResume:
			controls.Resume.DisabledReason = reason
		case taskprojection.ActionStop:
			controls.Stop.DisabledReason = reason
		case taskprojection.ActionChangeBudget:
			controls.AdjustBudget.DisabledReason = reason
		}
	}
	return controls
}

func normalizedPricingSnapshotIDs(values []string) ([]string, error) {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" || value != raw {
			return nil, errors.New("pricing snapshot identity is invalid")
		}
		if _, exists := seen[value]; exists {
			return nil, errors.New("pricing snapshot identity is duplicated")
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result, nil
}

func decodeTaskForecast(value *codefluxv1.TaskForecastView) (taskcontrols.ForecastView, error) {
	if value == nil {
		return taskcontrols.ForecastView{Range: domain.ForecastRange{}}, nil
	}
	rangeValue := domain.ForecastRange{
		LatencyKnown:     value.GetLatencyKnown(),
		LatencyP50Millis: domain.Milliseconds(value.GetLatencyP50Ms()),
		LatencyP90Millis: domain.Milliseconds(value.GetLatencyP90Ms()),
		TokensKnown:      value.GetTokensKnown(),
		TokensP50:        domain.TokenCount(value.GetTokensP50()),
		TokensP90:        domain.TokenCount(value.GetTokensP90()),
	}
	if (value.GetCostP50() == nil) != (value.GetCostP90() == nil) {
		return taskcontrols.ForecastView{}, errors.New("cost percentile presence differs")
	}
	if value.GetCostP50() != nil {
		p50, known, err := decodeTaskMoney(value.GetCostP50())
		if err != nil || !known {
			return taskcontrols.ForecastView{}, errors.New("cost P50 is invalid")
		}
		p90, known, err := decodeTaskMoney(value.GetCostP90())
		if err != nil || !known || p50.Currency != p90.Currency {
			return taskcontrols.ForecastView{}, errors.New("cost P90 is invalid")
		}
		rangeValue.CostKnown = true
		rangeValue.CostP50 = p50
		rangeValue.CostP90 = p90
	}
	if err := rangeValue.Validate(); err != nil {
		return taskcontrols.ForecastView{}, err
	}
	parts := make([]string, 0, 5)
	for _, fact := range []string{value.GetEstimateNotice(), value.GetAlgorithmVersion()} {
		if fact = strings.TrimSpace(fact); fact != "" {
			parts = append(parts, fact)
		}
	}
	if identity := strings.TrimSpace(value.GetPriceSnapshotId()); identity != "" {
		parts = append(parts, "price snapshot "+identity)
	}
	if source := strings.TrimSpace(value.GetPriceSource()); source != "" {
		parts = append(parts, "price source "+source)
	}
	if captured := value.GetPriceCapturedAt(); captured != nil {
		if err := captured.CheckValid(); err != nil {
			return taskcontrols.ForecastView{}, errors.New("price capture time is invalid")
		}
		parts = append(parts, "price captured "+captured.AsTime().UTC().Format(time.RFC3339Nano))
	}
	if len(value.GetUncertaintyReasons()) > 0 {
		reasons := value.GetUncertaintyReasons()
		for _, reason := range reasons {
			if strings.TrimSpace(reason) == "" || strings.TrimSpace(reason) != reason {
				return taskcontrols.ForecastView{}, errors.New("uncertainty reason is invalid")
			}
		}
		parts = append(parts, "uncertainty "+strings.Join(reasons, ", "))
	}
	return taskcontrols.ForecastView{Range: rangeValue, Assumptions: strings.Join(parts, " · ")}, nil
}

func taskDeliveryView(session frontendstate.SessionView) taskcontrols.DeliveryView {
	view := taskcontrols.DeliveryView{
		State: taskcontrols.DeliveryDegraded, TimelineReadable: true,
		Explanation: strings.TrimSpace(session.Message),
	}
	switch session.Connection {
	case frontendstate.ConnectionLive:
		view.State = taskcontrols.DeliveryLive
		view.SequenceCertain = true
	case frontendstate.ConnectionDisconnected, frontendstate.ConnectionUnauthorized,
		frontendstate.ConnectionIncompatible:
		view.State = taskcontrols.DeliveryDisconnected
	default:
		view.State = taskcontrols.DeliveryDegraded
	}
	return view
}

func taskPhase(state domain.TaskState) taskcontrols.Phase {
	switch state {
	case domain.TaskStateRunning, domain.TaskStateAwaitingAuthority, domain.TaskStatePaused:
		return taskcontrols.PhaseEditing
	case domain.TaskStateValidating:
		return taskcontrols.PhaseValidating
	case domain.TaskStateFailed, domain.TaskStateRecoveryRequired:
		return taskcontrols.PhaseRepairing
	case domain.TaskStateAwaitingReview, domain.TaskStateCompleted,
		domain.TaskStateCancelled, domain.TaskStateRolledBack:
		return taskcontrols.PhaseReviewing
	default:
		return taskcontrols.PhasePlanning
	}
}

func decodeTaskMoney(value *codefluxv1.Money) (domain.Money, bool, error) {
	if value == nil {
		return domain.Money{}, false, nil
	}
	currency, err := domain.ParseCurrencyCode(value.GetCurrencyCode())
	if err != nil {
		return domain.Money{}, false, err
	}
	money, err := domain.NewMoney(currency, value.GetMinorUnits())
	if err != nil {
		return domain.Money{}, false, err
	}
	return money, true, nil
}

func decodeTaskIdentity(value *codefluxv1.StableIdentity) (domain.TaskID, error) {
	if value == nil || value.GetKind() != codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_TASK {
		return domain.TaskID{}, errTaskResourceMalformed
	}
	return domain.ParseTaskID(value.GetValue())
}

func decodeThreadIdentity(value *codefluxv1.StableIdentity) (domain.ThreadID, error) {
	if value == nil || value.GetKind() != codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_THREAD {
		return domain.ThreadID{}, errTaskResourceMalformed
	}
	return domain.ParseThreadID(value.GetValue())
}

func decodeCheckpointIdentity(value *codefluxv1.StableIdentity) (domain.CheckpointID, error) {
	if value == nil || value.GetKind() != codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_CHECKPOINT {
		return domain.CheckpointID{}, domain.ErrInvalidID
	}
	return domain.ParseCheckpointID(value.GetValue())
}

func taskIdentity(taskID domain.TaskID) *codefluxv1.StableIdentity {
	return &codefluxv1.StableIdentity{
		Kind: codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_TASK, Value: taskID.String(),
	}
}
