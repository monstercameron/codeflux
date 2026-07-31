package timelineview

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/design"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"codeflux.dev/codeflux/web/frontend/timelinecard"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

var viewFixtureTime = time.Date(2026, 7, 31, 16, 0, 0, 0, time.UTC)

func TestRendererExhaustivelyRendersEveryTypedCard(t *testing.T) {
	fixture := fixedCards(t)
	if len(fixture) != len(timelinecard.KnownKinds()) {
		t.Fatalf("fixture cards=%d known=%d", len(fixture), len(timelinecard.KnownKinds()))
	}
	seen := map[timelinecard.Kind]bool{}
	for _, card := range fixture {
		card := card
		t.Run(string(card.Kind), func(t *testing.T) {
			markup := renderCard(t, card)
			for _, want := range []string{
				`data-component="timeline-card"`,
				`data-card-kind="` + string(card.Kind) + `"`,
				`data-stable-key="` + card.StableKey + `"`,
				`data-component="badge"`,
				`data-shape=`,
				`<h2`,
				`Event details`,
			} {
				if !strings.Contains(markup, want) {
					t.Errorf("%s markup missing %q:\n%s", card.Kind, want, markup)
				}
			}
			if strings.Contains(markup, `card-kind="invalid"`) {
				t.Fatalf("%s rendered invalid fallback: %s", card.Kind, markup)
			}
			seen[card.Kind] = true
		})
	}
	for _, kind := range timelinecard.KnownKinds() {
		if !seen[kind] {
			t.Fatalf("renderer fixture missing %s", kind)
		}
	}
}

func TestRendererKeepsUnsafeMarkdownInertAndSafeLinksHardened(t *testing.T) {
	card := fixedCards(t)[0]
	card.Message.Body = `<script>alert(1)</script> [unsafe](javascript:alert(1)) [safe](https://example.com/docs)`
	markup := renderCard(t, card)
	for _, forbidden := range []string{`<script`, `href="javascript:`, `data:text/html`, `file:///`} {
		if strings.Contains(strings.ToLower(markup), forbidden) {
			t.Fatalf("unsafe markdown survived as executable markup %q:\n%s", forbidden, markup)
		}
	}
	for _, want := range []string{
		`&lt;script&gt;alert(1)&lt;/script&gt;`,
		`href="https://example.com/docs"`,
		`target="_blank"`,
		`rel="noopener noreferrer"`,
		`data-blocked-links="1"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("safe markdown markup missing %q:\n%s", want, markup)
		}
	}
}

func TestCodeBlocksSupportWrapScrollAndCopyWithoutRawHTML(t *testing.T) {
	copied := ""
	document := timelinecard.Markdown{Blocks: []timelinecard.Block{
		{Kind: timelinecard.BlockCode, Code: "wrapped code", CopyText: "wrapped code", Overflow: timelinecard.CodeWrap},
		{Kind: timelinecard.BlockCode, Code: "scrolling code", CopyText: "scrolling code", Overflow: timelinecard.CodeHorizontalScroll},
	}}
	root := MarkdownView(MarkdownProps{
		Document: document, Mode: testMode(), OnCopy: func(value string) { copied = value },
	})
	markup := renderNode(t, root)
	for _, want := range []string{`data-overflow="wrap"`, `data-overflow="horizontal-scroll"`, "Copy code"} {
		if !strings.Contains(markup, want) {
			t.Errorf("code presentation missing %q:\n%s", want, markup)
		}
	}
	handler, found := findButtonHandler(root, "Copy code")
	if !found || handler == nil {
		t.Fatal("code copy action is not invokable")
	}
	handler()
	if copied != "wrapped code" {
		t.Fatalf("copied code = %q", copied)
	}
}

func TestRedactedOutputIsAbsentUntilExplicitDisclosure(t *testing.T) {
	output, err := timelinecard.NewRedactedOutput("41 tests; 2 failed", "redacted page one\nredacted page two", 18, 128)
	if err != nil {
		t.Fatal(err)
	}
	card := fixedCards(t)[6]
	card.Tool.Output = output
	markup := renderCard(t, card)
	if strings.Contains(markup, "redacted page one") || strings.Contains(markup, "redacted page two") {
		t.Fatalf("collapsed output leaked into DOM:\n%s", markup)
	}
	for _, want := range []string{
		`41 tests; 2 failed`, `Show redacted output`, `aria-expanded="false"`,
		`data-component="redacted-output"`, `data-state="collapsed"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("collapsed disclosure missing %q:\n%s", want, markup)
		}
	}
	if strings.Contains(markup, `data-component="redacted-output-pages"`) {
		t.Fatalf("collapsed disclosure mounted page region:\n%s", markup)
	}
}

func TestRequirementKeepsSummaryVisibleAndSecondaryDetailCollapsed(t *testing.T) {
	card := fixedCards(t)[1]
	markup := renderCard(t, card)
	goalAt := strings.Index(markup, "Build typed cards")
	detailsAt := strings.Index(markup, `data-component="card-secondary-details"`)
	constraintAt := strings.Index(markup, "No raw HTML")
	if goalAt < 0 || detailsAt < 0 || constraintAt < 0 {
		t.Fatalf("requirement hierarchy is incomplete:\n%s", markup)
	}
	if !(goalAt < detailsAt && detailsAt < constraintAt) {
		t.Fatalf("goal must precede collapsed secondary detail: goal=%d details=%d constraint=%d\n%s", goalAt, detailsAt, constraintAt, markup)
	}
	for _, want := range []string{
		`data-component="card-summary"`, `data-default-state="collapsed"`,
		`Constraints and interpretation details`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("compact requirement missing %q:\n%s", want, markup)
		}
	}
	if strings.Contains(markup, `<details open`) {
		t.Fatalf("secondary detail must be collapsed initially:\n%s", markup)
	}
}

func TestRendererDeclaresCompactCardsWithoutNestedVerticalScrolling(t *testing.T) {
	markup := renderCard(t, fixedCards(t)[1])
	for _, want := range []string{`data-density="compact"`, `data-overflow-y="visible"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("compact card contract missing %q:\n%s", want, markup)
		}
	}
}

func TestTimelineCardRailIsReservedForAttentionStates(t *testing.T) {
	fixtures := fixedCards(t)
	for _, index := range []int{0, 2, 3, 5, 6, 8, 9, 10, 14, 15, 16, 17} {
		markup := renderCard(t, fixtures[index])
		if !strings.Contains(markup, `data-attention="false"`) {
			t.Errorf("%s should keep a neutral border:\n%s", fixtures[index].Kind, markup)
		}
	}
	for _, index := range []int{1, 4, 7, 11, 12, 13, 18} {
		markup := renderCard(t, fixtures[index])
		if !strings.Contains(markup, `data-attention="true"`) {
			t.Errorf("%s should retain an attention rail:\n%s", fixtures[index].Kind, markup)
		}
	}
	resolvedApproval := fixtures[7]
	resolvedApproval.Approval = &timelinecard.Approval{
		ID: "approval-1", State: "granted", ResolvedBy: "local user", ResolvedAt: viewFixtureTime,
	}
	if markup := renderCard(t, resolvedApproval); !strings.Contains(markup, `data-attention="false"`) {
		t.Errorf("resolved approval should return to a neutral border:\n%s", markup)
	}
}

func TestForecastUsesResponsiveMetricsAndCollapsesUncertainty(t *testing.T) {
	card := fixedCards(t)[2]
	card.Forecast.Range.LatencyKnown = true
	card.Forecast.Range.LatencyP50Millis = 1200
	card.Forecast.Range.LatencyP90Millis = 3400
	card.Forecast.Range.TokensKnown = true
	card.Forecast.Range.TokensP50 = 800
	card.Forecast.Range.TokensP90 = 1400
	markup := renderCard(t, card)
	metricsAt := strings.Index(markup, `data-component="forecast-metrics"`)
	contextAt := strings.Index(markup, `data-component="forecast-context"`)
	detailsAt := strings.Index(markup, `Uncertainty and estimate notes`)
	uncertaintyAt := strings.Index(markup, `new surface`)
	if metricsAt < 0 || contextAt < 0 || detailsAt < 0 || uncertaintyAt < 0 {
		t.Fatalf("forecast hierarchy is incomplete:\n%s", markup)
	}
	if !(metricsAt < contextAt && contextAt < detailsAt && detailsAt < uncertaintyAt) {
		t.Fatalf("forecast must lead with metrics and collapse uncertainty: metrics=%d context=%d details=%d uncertainty=%d\n%s", metricsAt, contextAt, detailsAt, uncertaintyAt, markup)
	}
	for _, want := range []string{
		`data-layout="responsive-grid"`, `data-metric="latency"`, `data-metric="tokens"`,
		`data-metric="cost"`, `aria-label="Forecast execution context"`, `data-default-state="collapsed"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("compact forecast missing %q:\n%s", want, markup)
		}
	}
	if strings.Contains(markup, `<details open`) {
		t.Fatalf("forecast uncertainty must be collapsed initially:\n%s", markup)
	}
}

func TestValidationAndCompletionStatusesHaveTextShapeAndState(t *testing.T) {
	validationStatuses := []timelinecard.ValidationStatus{
		timelinecard.ValidationPending, timelinecard.ValidationRunning,
		timelinecard.ValidationPassed, timelinecard.ValidationFailed,
		timelinecard.ValidationWaived, timelinecard.ValidationSkipped,
		timelinecard.ValidationUnavailable, timelinecard.ValidationCancelled,
		timelinecard.ValidationStale,
	}
	for _, status := range validationStatuses {
		card := fixtureEnvelope(10, timelinecard.KindValidation)
		card.Validation = &timelinecard.Validation{ID: "validation-1", Status: status}
		markup := renderCard(t, card)
		statusLabel := humanize(string(status))
		if !strings.Contains(markup, `data-status="`+statusLabel+`"`) ||
			!strings.Contains(markup, `data-shape=`) || !strings.Contains(markup, statusLabel) {
			t.Errorf("validation %s lacks redundant semantics:\n%s", status, markup)
		}
	}
	completionStatuses := []timelinecard.CompletionStatus{
		timelinecard.CompletionImplemented, timelinecard.CompletionValidated,
		timelinecard.CompletionReviewed, timelinecard.CompletionAccepted,
		timelinecard.CompletionRejected, timelinecard.CompletionRolledBack,
	}
	for _, status := range completionStatuses {
		card := fixtureEnvelope(15, timelinecard.KindCompletion)
		card.Completion = &timelinecard.Completion{Status: status}
		markup := renderCard(t, card)
		statusLabel := humanize(string(status))
		if !strings.Contains(markup, `data-status="`+statusLabel+`"`) ||
			!strings.Contains(markup, statusLabel) {
			t.Errorf("completion %s lacks status text:\n%s", status, markup)
		}
	}
}

func TestApprovalActionsDisappearButAttributionRemainsAfterResolution(t *testing.T) {
	pending := fixedCards(t)[7]
	pendingMarkup := renderCard(t, pending)
	for _, action := range []string{"Allow once", "Allow for this task", "Deny"} {
		if !strings.Contains(pendingMarkup, action) {
			t.Errorf("pending approval missing %q", action)
		}
	}
	resolved := pending
	resolved.Approval = &timelinecard.Approval{
		ID: "approval-1", State: "granted", Scope: "repository",
		ResolvedBy: "local user", ResolvedAt: viewFixtureTime,
	}
	resolvedMarkup := renderCard(t, resolved)
	for _, action := range []string{"Allow once", "Allow for this task", ">Deny<"} {
		if strings.Contains(resolvedMarkup, action) {
			t.Errorf("resolved approval retained action %q:\n%s", action, resolvedMarkup)
		}
	}
	for _, want := range []string{"Resolved by", "local user", "Resolved at"} {
		if !strings.Contains(resolvedMarkup, want) {
			t.Errorf("resolved approval missing %q", want)
		}
	}
}

func TestApprovalCommandStateIsInspectableAndDisablesDuplicateResolution(t *testing.T) {
	pending := fixedCards(t)[7]
	markup := renderNode(t, ui.CreateElement(Renderer, Props{
		Card: pending,
		Mode: testMode(),
		Actions: Actions{
			OnApproval: func(string, timelinecard.ApprovalAction) {},
			ApprovalCommand: func(string) ApprovalCommandState {
				return ApprovalCommandState{
					Busy: true, IdempotencyKey: "approval-command-1",
					TransportMode: "authoritative-bridge",
				}
			},
		},
	}))
	for _, want := range []string{
		`id="timeline-approval-approval-1"`,
		`tabIndex="-1"`,
		`aria-busy="true"`,
		`data-component="approval-card-interaction"`,
		`data-command-state="busy"`,
		`data-command-key="approval-command-1"`,
		`data-transport-mode="authoritative-bridge"`,
		`data-focus-retained="card-after-resolution"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("busy approval contract missing %q:\n%s", want, markup)
		}
	}
	if strings.Count(markup, "disabled") < 3 {
		t.Fatalf("all approval choices must be disabled while one command is busy:\n%s", markup)
	}
}

func TestInvalidCardRendersSafeInspectableFallback(t *testing.T) {
	markup := renderNode(t, ui.CreateElement(Renderer, Props{
		Card: timelinecard.Card{Kind: timelinecard.KindMessage, StableKey: "broken"},
		Mode: testMode(),
	}))
	for _, want := range []string{`card-kind="invalid"`, `Timeline item unavailable`, `Diagnostic detail`} {
		if !strings.Contains(markup, want) {
			t.Errorf("invalid fallback missing %q:\n%s", want, markup)
		}
	}
}

func TestMessageExplainsStaleGraphRevisionWithoutDroppingNodeActions(t *testing.T) {
	card := timelinecard.Card{
		Kind:       timelinecard.KindMessage,
		StableKey:  "message:stale-graph",
		Sequence:   44,
		OccurredAt: viewFixtureTime,
		Message: &timelinecard.Message{
			ID: "message-stale", Role: "agent", Body: "Historical graph evidence.",
			Status: timelinecard.MessageComplete, NodeIDs: []string{"node:implementation"},
			GraphRevision: "grv_01890f3c-4a00-7abc-8def-0123456789ab",
		},
	}
	markup := renderCard(t, card)
	for _, want := range []string{
		`data-component="stale-graph-revision"`,
		`data-graph-revision="grv_01890f3c-4a00-7abc-8def-0123456789ab"`,
		`Graph node node:implementation`,
		`which is no longer current`,
		`preserves the historical revision context`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("stale graph message missing %q: %s", want, markup)
		}
	}
}

func TestGraphNodeChipInvokesStableIdentitySelection(t *testing.T) {
	selected := ""
	card := timelinecard.Card{
		Kind: timelinecard.KindMessage, StableKey: "message:node", Sequence: 43,
		Message: &timelinecard.Message{ID: "message", Role: "agent", Body: "See node", Status: timelinecard.MessageComplete, NodeIDs: []string{"node:validation"}},
	}
	root := renderMessage(*card.Message, Props{
		Card: card, Mode: testMode(), Actions: Actions{OnSelectNode: func(id string) { selected = id }},
	})
	handler, found := findButtonHandler(root, "Graph node node:validation")
	if !found || handler == nil {
		t.Fatal("graph identity chip is not invokable")
	}
	handler()
	if selected != "node:validation" {
		t.Fatalf("selected node = %q", selected)
	}
}

func TestUserAgentAndInterruptedMessagesHaveDistinctAccessiblePresentation(t *testing.T) {
	fixtures := []struct {
		role   string
		status timelinecard.MessageStatus
		want   []string
	}{
		{role: "user", status: timelinecard.MessageComplete, want: []string{`data-message-role="user"`, `aria-label="User message content"`}},
		{role: "assistant", status: timelinecard.MessageProvisional, want: []string{`data-message-role="agent"`, `aria-label="Agent message content"`, `Response is still in progress and is not yet durable.`}},
		{role: "agent", status: timelinecard.MessageInterrupted, want: []string{`data-message-status="interrupted"`, `Response was interrupted before a durable final message arrived.`}},
	}
	for _, fixture := range fixtures {
		card := fixtureEnvelope(50, timelinecard.KindMessage)
		card.Message = &timelinecard.Message{ID: "message", Role: fixture.role, Body: "Safe body", Status: fixture.status}
		markup := renderCard(t, card)
		for _, want := range fixture.want {
			if !strings.Contains(markup, want) {
				t.Errorf("role=%s status=%s missing %q:\n%s", fixture.role, fixture.status, want, markup)
			}
		}
	}
}

func TestPlanHistoryAndDiffReviewActionsRemainInspectibleAndInvokable(t *testing.T) {
	approvedRevision := uint64(0)
	changedRevision := uint64(0)
	comparedRevision := uint64(0)
	plan := timelinecard.Plan{
		Revision: 3, Summary: "Current plan", ApprovalPending: true,
		PriorRevisions: []uint64{1, 2},
	}
	props := Props{Mode: testMode(), Actions: Actions{
		OnApprovePlan:         func(revision uint64) { approvedRevision = revision },
		OnRequestPlanChange:   func(revision uint64) { changedRevision = revision },
		OnComparePlanRevision: func(revision uint64) { comparedRevision = revision },
	}}
	root := renderPlan(plan, props)
	markup := renderNode(t, root)
	for _, want := range []string{"Prior revisions", ">1<", ">2<", "Approve plan", "Request plan change", "Compare plan revision"} {
		if !strings.Contains(markup, want) {
			t.Errorf("plan presentation missing %q:\n%s", want, markup)
		}
	}
	for label, want := range map[string]*uint64{
		"Approve plan":          &approvedRevision,
		"Request plan change":   &changedRevision,
		"Compare plan revision": &comparedRevision,
	} {
		handler, found := findButtonHandler(root, label)
		if !found || handler == nil {
			t.Fatalf("plan action %q is not invokable", label)
		}
		handler()
		if *want != 3 {
			t.Fatalf("plan action %q revision = %d", label, *want)
		}
	}

	reviewID := "review-7"
	opened := ""
	diffRoot := renderDiff(timelinecard.DiffSummary{ReviewID: reviewID}, Props{
		Mode: testMode(), Actions: Actions{OnOpenReview: func(id string) { opened = id }},
	})
	handler, found := findButtonHandler(diffRoot, "Open review")
	if !found || handler == nil {
		t.Fatal("diff summary did not expose its review action")
	}
	handler()
	if opened != reviewID {
		t.Fatalf("opened review = %q", opened)
	}
}

func TestPresentationStatusMatrixIsTextuallyDistinct(t *testing.T) {
	fixtures := []struct {
		card timelinecard.Card
		want string
	}{
		{card: timelinecard.Card{Kind: timelinecard.KindMessage, Message: &timelinecard.Message{Status: timelinecard.MessageComplete}}, want: "Complete"},
		{card: timelinecard.Card{Kind: timelinecard.KindMessage, Message: &timelinecard.Message{Status: timelinecard.MessageProvisional}}, want: "In progress"},
		{card: timelinecard.Card{Kind: timelinecard.KindMessage, Message: &timelinecard.Message{Status: timelinecard.MessageInterrupted}}, want: "Interrupted"},
		{card: timelinecard.Card{Kind: timelinecard.KindTool, Tool: &timelinecard.ToolActivity{State: "running"}}, want: "Running"},
		{card: timelinecard.Card{Kind: timelinecard.KindTool, Tool: &timelinecard.ToolActivity{State: "failed"}}, want: "Failed"},
		{card: timelinecard.Card{Kind: timelinecard.KindApproval, Approval: &timelinecard.Approval{State: "pending"}}, want: "Pending"},
		{card: timelinecard.Card{Kind: timelinecard.KindApproval, Approval: &timelinecard.Approval{State: "denied"}}, want: "Denied"},
		{card: timelinecard.Card{Kind: timelinecard.KindPlan, Plan: &timelinecard.Plan{Superseded: true}}, want: "Superseded"},
		{card: timelinecard.Card{Kind: timelinecard.KindTaskState, TaskState: &timelinecard.TaskState{To: "paused"}}, want: "Paused"},
	}
	seen := map[string]bool{}
	for _, fixture := range fixtures {
		presentation := PresentationFor(fixture.card)
		if presentation.StatusLabel != fixture.want {
			t.Errorf("%s status = %q, want %q", fixture.card.Kind, presentation.StatusLabel, fixture.want)
		}
		seen[string(fixture.card.Kind)+":"+presentation.StatusLabel] = true
	}
	if len(seen) != len(fixtures) {
		t.Fatalf("status matrix collapsed distinct variants: %#v", seen)
	}
	for _, taskState := range domain.AllTaskStates() {
		card := timelinecard.Card{Kind: timelinecard.KindTaskState, TaskState: &timelinecard.TaskState{To: string(taskState)}}
		if got, want := PresentationFor(card).StatusLabel, humanize(string(taskState)); got != want {
			t.Errorf("task state %s status = %q, want %q", taskState, got, want)
		}
	}
	for _, approvalState := range []domain.ApprovalRequestState{
		domain.ApprovalRequestStatePending, domain.ApprovalRequestStateGranted,
		domain.ApprovalRequestStateDenied, domain.ApprovalRequestStateExpired,
		domain.ApprovalRequestStateCancelled,
	} {
		card := timelinecard.Card{Kind: timelinecard.KindApproval, Approval: &timelinecard.Approval{State: string(approvalState)}}
		if got, want := PresentationFor(card).StatusLabel, humanize(string(approvalState)); got != want {
			t.Errorf("approval state %s status = %q, want %q", approvalState, got, want)
		}
	}
}

func fixedCards(t *testing.T) []timelinecard.Card {
	t.Helper()
	currency, err := domain.ParseCurrencyCode("USD")
	if err != nil {
		t.Fatal(err)
	}
	money, err := domain.NewMoney(currency, 420)
	if err != nil {
		t.Fatal(err)
	}
	output, err := timelinecard.NewRedactedOutput("bounded summary", "bounded redacted output", 8, 64)
	if err != nil {
		t.Fatal(err)
	}
	cards := []timelinecard.Card{
		{Kind: timelinecard.KindMessage, Message: &timelinecard.Message{ID: "message-1", Role: "assistant", Body: "Safe **message**", Status: timelinecard.MessageComplete, NodeIDs: []string{"node-1"}}},
		{Kind: timelinecard.KindRequirement, Requirement: &timelinecard.Requirement{Goal: "Build typed cards", Constraints: []string{"No raw HTML"}, Assumptions: []string{"GWC v5"}, UnresolvedAmbiguities: []string{"None"}}},
		{Kind: timelinecard.KindForecast, Forecast: &timelinecard.Forecast{Model: "gpt-5", Effort: "high", UncertaintyReasons: []string{"new surface"}}},
		{Kind: timelinecard.KindPlan, Plan: &timelinecard.Plan{Revision: 1, Summary: "Implement and test", ApprovalPending: true, Steps: []timelinecard.PlanStep{{Ordinal: 1, Title: "Render", Status: timelinecard.PlanStepActive}}, CompletionCriteria: []string{"Tests pass"}}},
		{Kind: timelinecard.KindPlanRevision, PlanRevision: &timelinecard.PlanRevision{PreviousRevision: 1, CurrentRevision: 2, Summary: "Add disclosure", ApprovalReset: true, Added: []string{"Disclosure test"}}},
		{Kind: timelinecard.KindContext, Context: &timelinecard.ContextSelection{RepositoryRevision: "abc123", Included: []timelinecard.ContextIdentity{{Identity: "renderer.go", Reason: "implementation", Revision: "abc123"}}, Exclusions: []string{"full source dump"}, BudgetUsed: 10, BudgetLimit: 20}},
		{Kind: timelinecard.KindTool, Tool: &timelinecard.ToolActivity{ExecutionID: "exec-1", Tool: "go-test", Purpose: "verify renderer", Scope: "timelineview", State: "passed", Duration: time.Second, ExitStatus: "0", Summary: "all passed", Output: output}},
		{Kind: timelinecard.KindApproval, Approval: &timelinecard.Approval{ID: "approval-1", Action: "write files", SafeArguments: "timelineview", Scope: "repository", Reason: "implement card renderer", Consequences: "source changes", State: "pending"}},
		{Kind: timelinecard.KindCheckpoint, Checkpoint: &timelinecard.Checkpoint{ID: "checkpoint-1", TaskRevision: 2, PlanStep: "render", DiffSummary: "three files", Reason: "before integration", RestoreSafe: true}},
		{Kind: timelinecard.KindValidation, Validation: &timelinecard.Validation{ID: "validation-1", Status: timelinecard.ValidationPassed, Required: true, Summary: "tests pass", RevisionBinding: "abc123", Output: output}},
		{Kind: timelinecard.KindDiff, Diff: &timelinecard.DiffSummary{Files: []timelinecard.ChangedFile{{Path: "renderer.go", Category: "source", Added: 20}}, Additions: 20, ReviewID: "review-1"}},
		{Kind: timelinecard.KindCostBudget, CostBudget: &timelinecard.CostBudget{Reason: "warning threshold", Known: true, Actual: money, HardLimit: money, Threshold: "80 percent"}},
		{Kind: timelinecard.KindRecovery, Recovery: &timelinecard.Recovery{CheckpointID: "checkpoint-1", Reason: "worktree divergence", Known: []string{"checkpoint exists"}, Ambiguous: []string{"external edit"}, Choices: []timelinecard.RecoveryChoice{{Kind: "safe-resume", Label: "Safe resume", Explanation: "Resume from verified state"}, {Kind: "abandon", Label: "Abandon", Explanation: "Discard task state", Destructive: true}}}},
		{Kind: timelinecard.KindError, Error: &timelinecard.Error{Code: "conflict", Message: "state changed", AffectedAction: "save", Retryable: true, NextSteps: []string{"Refresh"}}},
		{Kind: timelinecard.KindCompletion, Completion: &timelinecard.Completion{Status: timelinecard.CompletionValidated, Files: []string{"renderer.go"}, Validation: []timelinecard.Validation{{ID: "validation-1", Status: timelinecard.ValidationPassed}}, Evidence: []string{"unit tests"}, Cost: &money}},
		{Kind: timelinecard.KindTaskState, TaskState: &timelinecard.TaskState{From: "running", To: "validating", Approval: "granted"}},
		{Kind: timelinecard.KindUsage, Usage: &timelinecard.Usage{Tokens: domain.TokenUsage{Known: true, Input: 100, Output: 20}}},
		{Kind: timelinecard.KindGraphChange, GraphChange: &timelinecard.GraphChange{RevisionID: "graph-1", Patch: true, ByteCount: 128}},
		{Kind: timelinecard.KindUnknown, Unknown: &timelinecard.Unknown{EventKind: "future-event", OccurredAt: viewFixtureTime, Sequence: 19, SafeDetails: "unsupported but safe", DiagnosticsPath: "/diagnostics"}},
		{Kind: timelinecard.KindThreadState, ThreadState: &timelinecard.ThreadState{Action: "renamed", PreviousTitle: "Thread", Title: "Renamed"}},
	}
	for index := range cards {
		cards[index].Sequence = uint64(index + 1)
		cards[index].StableKey = fmt.Sprintf("fixture:%d", index+1)
		cards[index].OccurredAt = viewFixtureTime.Add(time.Duration(index) * time.Second)
		if err := cards[index].Validate(); err != nil {
			t.Fatalf("fixture %s: %v", cards[index].Kind, err)
		}
	}
	return cards
}

func fixtureEnvelope(sequence uint64, kind timelinecard.Kind) timelinecard.Card {
	return timelinecard.Card{
		Kind: kind, Sequence: sequence, StableKey: fmt.Sprintf("fixture:%d", sequence), OccurredAt: viewFixtureTime,
	}
}

func renderCard(t *testing.T, card timelinecard.Card) string {
	t.Helper()
	return renderNode(t, ui.CreateElement(Renderer, Props{Card: card, Mode: testMode()}))
}

func renderNode(t *testing.T, node ui.Node) string {
	t.Helper()
	markup, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func testMode() primitives.Mode {
	return primitives.Mode{Theme: design.ThemeDark, Density: design.DensityComfortable, ReducedMotion: true}
}

func findButtonHandler(node ui.Node, accessibleLabel string) (func(), bool) {
	if node == nil {
		return nil, false
	}
	if label, ok := node.Props["aria-label"].(string); ok && label == accessibleLabel {
		handler, _ := node.Props["onclick"].(func())
		return handler, true
	}
	for _, child := range node.Children {
		childNode, ok := child.(ui.Node)
		if !ok {
			continue
		}
		if handler, found := findButtonHandler(childNode, accessibleLabel); found {
			return handler, true
		}
	}
	return nil, false
}
