package timelinecard

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"codeflux.dev/codeflux/internal/events"
)

func TestRegistryExhaustivelyCoversDurableEvents(t *testing.T) {
	if err := ValidateRegistry(); err != nil {
		t.Fatal(err)
	}
	if len(Registry()) != len(events.Registry) {
		t.Fatalf("registry length=%d durable=%d", len(Registry()), len(events.Registry))
	}
}

func TestEveryDurableEventProjectsFromFixedFixture(t *testing.T) {
	fixture := fixedEventFixture(t)
	seen := make(map[events.Kind]bool, len(fixture))
	for _, event := range fixture {
		card, err := Project(event)
		if err != nil {
			t.Fatalf("project %s: %v", event.Kind, err)
		}
		if err := card.Validate(); err != nil {
			t.Fatalf("validate %s card: %v", event.Kind, err)
		}
		descriptor, ok := Lookup(event.Kind)
		if !ok || descriptor.CardKind != card.Kind {
			t.Fatalf("%s descriptor=%#v card=%s", event.Kind, descriptor, card.Kind)
		}
		if descriptor.CorrectnessBearing != event.CorrectnessBearing() {
			t.Fatalf("%s correctness-bearing registry=%v event=%v", event.Kind, descriptor.CorrectnessBearing, event.CorrectnessBearing())
		}
		seen[event.Kind] = true
	}
	if len(seen) != len(events.Registry) {
		t.Fatalf("fixture covered %d of %d event kinds", len(seen), len(events.Registry))
	}
}

func TestReplaceableEventCardsUseStableProjectionKeys(t *testing.T) {
	expected := map[events.Kind]string{
		events.KindForecastUpdated:   "forecast:current",
		events.KindUsageUpdated:      "usage:current",
		events.KindCostUpdated:       "cost-budget:current",
		events.KindBudgetUpdated:     "cost-budget:current",
		events.KindValidationUpdated: "validation:",
		events.KindGraphSnapshot:     "graph:",
		events.KindGraphPatch:        "graph:",
	}
	for _, event := range fixedEventFixture(t) {
		prefix, ok := expected[event.Kind]
		if !ok {
			continue
		}
		card, err := Project(event)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(card.StableKey, prefix) {
			t.Errorf("%s stable key = %q, want prefix %q", event.Kind, card.StableKey, prefix)
		}
	}
}

func TestProjectionDecisionSuppressesRoutineTicksButSurfacesUnknownPrice(t *testing.T) {
	fixture := fixedEventFixture(t)
	for _, event := range fixture {
		decision, err := DecideProjection(event)
		if err != nil {
			t.Fatal(err)
		}
		switch event.Kind {
		case events.KindUsageUpdated, events.KindCostUpdated:
			if decision.Disposition != ProjectionSummaryOnly {
				t.Errorf("routine %s disposition = %s", event.Kind, decision.Disposition)
			}
		case events.KindToolProgress, events.KindApprovalResolved, events.KindValidationUpdated:
			if decision.Disposition != ProjectionUpsert {
				t.Errorf("replaceable %s disposition = %s", event.Kind, decision.Disposition)
			}
		}
	}
	var unknownPrice events.SessionEvent
	for _, event := range fixture {
		if event.Kind == events.KindCostUpdated {
			unknownPrice = event
			break
		}
	}
	unknownPrice.Payload.Cost = &events.Cost{Known: false}
	decision, err := DecideProjection(unknownPrice)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Disposition != ProjectionUpsert {
		t.Fatalf("unknown-price cost disposition = %s", decision.Disposition)
	}
}

func TestEveryTypedCardModelHasAFixedFixture(t *testing.T) {
	fixture := []Card{
		{Kind: KindMessage, StableKey: "message:1", Message: &Message{ID: "message-1", Status: MessageComplete}},
		{Kind: KindThreadState, StableKey: "sequence:2", ThreadState: &ThreadState{Action: "created", Title: "Thread"}},
		{Kind: KindRequirement, StableKey: "sequence:2", Requirement: &Requirement{Goal: "Build timeline"}},
		{Kind: KindForecast, StableKey: "sequence:3", Forecast: &Forecast{}},
		{Kind: KindPlan, StableKey: "sequence:4", Plan: &Plan{Revision: 1, Summary: "Plan"}},
		{Kind: KindPlanRevision, StableKey: "sequence:5", PlanRevision: &PlanRevision{CurrentRevision: 2, ApprovalReset: true}},
		{Kind: KindContext, StableKey: "sequence:6", Context: &ContextSelection{RepositoryRevision: "abc123"}},
		{Kind: KindTool, StableKey: "tool:1", Tool: &ToolActivity{ExecutionID: "tool-1", Tool: "go-test"}},
		{Kind: KindApproval, StableKey: "approval:1", Approval: &Approval{ID: "approval-1", State: "pending"}},
		{Kind: KindCheckpoint, StableKey: "checkpoint:1", Checkpoint: &Checkpoint{ID: "checkpoint-1"}},
		{Kind: KindValidation, StableKey: "validation:1", Validation: &Validation{ID: "validation-1", Status: ValidationPassed}},
		{Kind: KindDiff, StableKey: "sequence:11", Diff: &DiffSummary{ReviewID: "review-1"}},
		{Kind: KindCostBudget, StableKey: "sequence:12", CostBudget: &CostBudget{Reason: "warning"}},
		{Kind: KindRecovery, StableKey: "sequence:13", Recovery: &Recovery{Reason: "ambiguous state"}},
		{Kind: KindError, StableKey: "sequence:14", Error: &Error{Code: "conflict", Message: "state changed"}},
		{Kind: KindCompletion, StableKey: "sequence:15", Completion: &Completion{Status: CompletionValidated}},
		{Kind: KindTaskState, StableKey: "sequence:16", TaskState: &TaskState{From: "running", To: "validating"}},
		{Kind: KindUsage, StableKey: "sequence:17", Usage: &Usage{}},
		{Kind: KindGraphChange, StableKey: "sequence:18", GraphChange: &GraphChange{RevisionID: "graph-1"}},
		{Kind: KindUnknown, StableKey: "sequence:19", Unknown: &Unknown{EventKind: "future", Sequence: 19}},
	}
	if len(fixture) != len(KnownKinds()) {
		t.Fatalf("card fixtures=%d known kinds=%d", len(fixture), len(KnownKinds()))
	}
	seen := map[Kind]bool{}
	for _, card := range fixture {
		if err := card.Validate(); err != nil {
			t.Fatalf("%s fixture: %v", card.Kind, err)
		}
		if seen[card.Kind] {
			t.Fatalf("duplicate card fixture %s", card.Kind)
		}
		seen[card.Kind] = true
	}
	for _, kind := range KnownKinds() {
		if !seen[kind] {
			t.Fatalf("missing fixed fixture for %s", kind)
		}
	}
}

func TestUnknownFallbackPreservesSafeIdentityAndBoundsDetails(t *testing.T) {
	card := UnknownFallback("future-event", 44, fixedCardTime, strings.Repeat("é", 3000)+"\x00secret")
	if err := card.Validate(); err != nil {
		t.Fatal(err)
	}
	if card.Unknown.EventKind != "future-event" || card.Unknown.Sequence != 44 ||
		card.Unknown.DiagnosticsPath != "/diagnostics" || len(card.Unknown.SafeDetails) > 4100 ||
		!utf8.ValidString(card.Unknown.SafeDetails) || strings.ContainsRune(card.Unknown.SafeDetails, '\x00') {
		t.Fatalf("unknown fallback = %#v", card.Unknown)
	}
}

func TestUnsafeMarkdownAndURLPayloadsRemainInert(t *testing.T) {
	source := `<script>alert(1)</script>
[js](javascript:alert(1)) [data](data:text/html,bad) [file](file:///etc/passwd)
[credentials](https://user:pass@example.com/) [safe](https://example.com/docs)

` + "```go\nfmt.Println(\"safe\")\n```"
	document, err := ParseMarkdown(source)
	if err != nil {
		t.Fatal(err)
	}
	if document.BlockedLinks != 4 {
		t.Fatalf("blocked links=%d", document.BlockedLinks)
	}
	links := 0
	for _, block := range document.Blocks {
		for _, inline := range block.Inlines {
			if inline.Kind == InlineLink {
				links++
				if inline.Link.URL != "https://example.com/docs" {
					t.Fatalf("unsafe link survived: %#v", inline.Link)
				}
			}
		}
		if block.Kind == BlockCode && (block.CopyText == "" || block.Overflow != CodeHorizontalScroll) {
			t.Fatalf("code block disclosure = %#v", block)
		}
	}
	if links != 1 {
		t.Fatalf("safe links=%d", links)
	}
	if len(document.Blocks) == 0 || document.Blocks[0].Kind != BlockParagraph {
		t.Fatalf("raw HTML gained an executable block: %#v", document.Blocks)
	}
}

func TestRawRedactedOutputPaginationTruncationAndCopyDisclosure(t *testing.T) {
	output, err := NewRedactedOutput("tests failed", "αβγδεζηθικλμνξοπρστυφχψω", 9, 31)
	if err != nil {
		t.Fatal(err)
	}
	if !output.Collapsed || len(output.VisiblePages()) != 0 || output.CopyText() != "" || !output.Truncated {
		t.Fatalf("collapsed output = %#v", output)
	}
	output = output.Expand()
	first := output.CopyText()
	if first == "" || !utf8.ValidString(first) || output.LoadedPages != 1 {
		t.Fatalf("first page = %q output=%#v", first, output)
	}
	for output.LoadedPages < output.PageCount {
		output = output.LoadNext()
	}
	if !utf8.ValidString(output.CopyText()) || len(output.CopyText()) > 31 {
		t.Fatalf("copied output bytes=%d valid=%v", len(output.CopyText()), utf8.ValidString(output.CopyText()))
	}
}

func TestApprovalResolutionIsIdempotentAndAttributable(t *testing.T) {
	approval := Approval{ID: "approval-1", State: "pending", Scope: "repository"}
	actions := approval.AvailableActions()
	if len(actions) != 3 || actions[0] != ApprovalAllowOnce || actions[1] != ApprovalAllowForTask || actions[2] != ApprovalDeny {
		t.Fatalf("approval actions = %#v", actions)
	}
	resolution := ApprovalResolution{State: "granted", ResolvedBy: "user", ResolvedAt: fixedCardTime}
	resolved, changed, err := ResolveApproval(approval, resolution)
	if err != nil || !changed || resolved.ActionsAvailable() || resolved.ResolvedBy != "user" {
		t.Fatalf("first resolution=%#v changed=%v err=%v", resolved, changed, err)
	}
	if len(resolved.AvailableActions()) != 0 {
		t.Fatal("resolved approval retained mutation actions")
	}
	replayed, changed, err := ResolveApproval(resolved, resolution)
	if err != nil || changed || replayed != resolved {
		t.Fatalf("replayed resolution=%#v changed=%v err=%v", replayed, changed, err)
	}
	conflict := resolution
	conflict.State = "denied"
	if _, _, err := ResolveApproval(resolved, conflict); err == nil {
		t.Fatal("conflicting double resolution succeeded")
	}
}

func TestPlanRevisionResetsApprovalAndPreservesHistory(t *testing.T) {
	history, err := ApplyPlanRevision(nil, Plan{Revision: 1, Summary: "first"})
	if err != nil {
		t.Fatal(err)
	}
	history[0].ApprovalPending = false
	history, err = ApplyPlanRevision(history, Plan{Revision: 2, Summary: "second"})
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || !history[0].Superseded || history[0].ApprovalPending ||
		!history[1].ApprovalPending || len(history[1].PriorRevisions) != 1 || history[1].PriorRevisions[0] != 1 {
		t.Fatalf("plan history = %#v", history)
	}
}

func TestEveryValidationAndCompletionStatusIsStructurallyDistinct(t *testing.T) {
	validations := []ValidationStatus{
		ValidationPending, ValidationRunning, ValidationPassed, ValidationFailed,
		ValidationWaived, ValidationSkipped, ValidationUnavailable,
		ValidationCancelled, ValidationStale,
	}
	seenValidation := map[ValidationStatus]bool{}
	for _, status := range validations {
		if status == "" || seenValidation[status] {
			t.Fatalf("invalid validation status %q", status)
		}
		seenValidation[status] = true
	}
	completions := []CompletionStatus{
		CompletionImplemented, CompletionValidated, CompletionReviewed,
		CompletionAccepted, CompletionRejected, CompletionRolledBack,
	}
	seenCompletion := map[CompletionStatus]bool{}
	for _, status := range completions {
		if status == "" || seenCompletion[status] {
			t.Fatalf("invalid completion status %q", status)
		}
		seenCompletion[status] = true
	}
}

func TestRoutineProgressNeverRequestsAssertiveOrBlockingPresentation(t *testing.T) {
	for _, descriptor := range Registry() {
		if !descriptor.Routine {
			continue
		}
		if descriptor.Announcement != AnnouncementNone {
			t.Fatalf("routine %s announcement=%s", descriptor.EventKind, descriptor.Announcement)
		}
		if descriptor.CardKind == KindApproval || descriptor.CardKind == KindRecovery || descriptor.CardKind == KindError {
			t.Fatalf("routine %s mapped to blocking card %s", descriptor.EventKind, descriptor.CardKind)
		}
	}
}

func TestFirstMessageLatencyShowsPhaseAndStopAtThreshold(t *testing.T) {
	start := fixedCardTime
	before := FirstMessageLatency(start, start.Add(4*time.Second), 5*time.Second, "Forecasting")
	after := FirstMessageLatency(start, start.Add(5*time.Second), 5*time.Second, "Forecasting")
	if !before.Waiting || before.ShowStop || after.Waiting || !after.ShowStop || after.Phase != "Forecasting" {
		t.Fatalf("before=%#v after=%#v", before, after)
	}
}
