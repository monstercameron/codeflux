package graphinspector

import (
	"errors"
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	taskgraph "codeflux.dev/codeflux/internal/graph"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestContextInspectorRendersCompleteBoundedNodeExplanation(t *testing.T) {
	props := inspectorFixture(t)
	markup := renderInspector(t, props)

	wants := []string{
		`data-component="graph-node-inspector"`,
		`data-node-id="` + props.Node.ID().String() + `"`,
		`data-graph-revision-id="` + props.RevisionID.String() + `"`,
		`data-node-class="atom-operation"`, `data-node-status="active"`,
		`aria-label="Node inspector: Validate task graph projection"`,
		"Validate task graph projection", "Stable node identity", "Graph revision",
		"Atom operation", "Active", "Validate the selected graph projection.",
		"bounded task graph", "validation evidence", "repository read",
		"Contract checked — typed contract and required validation are attached",
		`data-evidence-id="` + props.Evidence.SupportingItems[0].ID.String() + `"`,
		"Graph projection tests", "Passed", "All selected projection tests passed.",
		"1m30s attributable elapsed time",
		"175 total — input 100, cached input 25, cache write 5, output 30, reasoning 15",
		"exact provider-reported tokens", "USD 25 minor units — pricing snapshot prices-2026-07-31",
		props.RelatedMessages[0].String(), props.Node.Sources().EventIDs()[0].String(),
		"Plan revision 3 · step validate-graph", "internal/graph/projector.go:42:1–78:2",
		`type="button"`, `aria-label="Explain in chat for Validate task graph projection"`,
		`aria-label="Expand neighbors for Validate task graph projection"`,
		`aria-label="Isolate dependency cone for Validate task graph projection"`,
		`aria-label="Isolate evidence cone for Validate task graph projection"`,
		`aria-label="Compare revision for Validate task graph projection"`,
		`aria-label="Open in editor for Validate task graph projection"`,
		`data-keyboard="native-buttons"`, `data-read-only="true"`, `data-bounded="true"`,
	}
	for _, want := range wants {
		if !strings.Contains(markup, want) {
			t.Errorf("inspector markup missing %q\n%s", want, markup)
		}
	}
	if strings.Count(markup, `data-inspector-list=`) == 0 {
		t.Fatal("bounded inspector lists were not identified")
	}
}

func TestContextInspectorUsesExplicitUnknownTextWithoutInventingZero(t *testing.T) {
	props := unknownInspectorFixture(t)
	markup := renderInspector(t, props)

	for _, want := range []string{
		"Unknown — contract purpose is not modeled.",
		"Unknown — not modeled.",
		"Unknown — evidence query has not completed.",
		"Unknown — execution timing is unavailable.",
		"Unknown — provider usage is unavailable.",
		"Unknown — provider pricing is unavailable.",
		"Unknown — no related messages are attached.",
		"Unknown — no related events are attached.",
		"Unknown — no validated repository source location is attached.",
		"Select a node with a repository source", `aria-describedby="graph-inspector-action-open-in-editor-reason"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("unknown-state inspector missing %q\n%s", want, markup)
		}
	}
	for _, forbidden := range []string{
		`data-inspector-field="tokens">0`,
		`data-inspector-field="cost">USD 0`,
		`data-inspector-field="time">0s`,
	} {
		if strings.Contains(markup, forbidden) {
			t.Fatalf("unknown attribution was rendered as zero via %q\n%s", forbidden, markup)
		}
	}
}

func TestContextInspectorExposesDisabledReasonsAndNonColorStatus(t *testing.T) {
	props := inspectorFixture(t)
	props.Actions.EvidenceCone = ActionAvailability{DisabledReason: "Evidence cone is unavailable for this revision"}
	props.OnIsolateEvidenceCone = nil
	props.Actions.CompareRevision = ActionAvailability{Enabled: true, Busy: true}
	markup := renderInspector(t, props)

	for _, want := range []string{
		`data-action="evidence-cone"`,
		`data-enabled="false"`,
		`id="graph-inspector-action-evidence-cone-reason"`,
		`aria-describedby="graph-inspector-action-evidence-cone-reason"`,
		"Evidence cone is unavailable for this revision",
		`data-action="compare-revision"`, `data-busy="true"`, "Working… Compare revision",
		`data-component="badge"`, `data-status="active"`, `data-shape=`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("disabled/accessibility contract missing %q\n%s", want, markup)
		}
	}
	if !buttonOpeningTagContains(markup, "graph-inspector-action-evidence-cone", "disabled") ||
		!buttonOpeningTagContains(markup, "graph-inspector-action-compare-revision", "disabled") {
		t.Fatalf("unavailable or busy inspector command remained enabled\n%s", markup)
	}
}

func TestInspectorRejectsUnsafeSourcesUnboundedListsAndContradictoryKnownState(t *testing.T) {
	props := inspectorFixture(t)

	if _, err := NewSourceLocation(
		mustRepositoryID(t), strings.Repeat("a", 40), "../outside.go", 1, 1, 1, 2,
	); !errors.Is(err, ErrInvalidInspectorProps) {
		t.Fatalf("unsafe path error = %v", err)
	}
	if _, err := NewSourceLocation(
		mustRepositoryID(t), strings.Repeat("A", 40), "internal/graph/model.go", 1, 1, 1, 2,
	); !errors.Is(err, ErrInvalidInspectorProps) {
		t.Fatalf("noncanonical revision error = %v", err)
	}
	if _, err := NewSourceLocation(
		mustRepositoryID(t), strings.Repeat("a", 40), "C:/outside.go", 1, 1, 1, 2,
	); !errors.Is(err, ErrInvalidInspectorProps) {
		t.Fatalf("drive-style path error = %v", err)
	}
	if _, err := NewSourceLocation(
		mustRepositoryID(t), strings.Repeat("a", 40), "internal/graph/model.go",
		MaximumSourceCoordinate+1, 1, MaximumSourceCoordinate+1, 2,
	); !errors.Is(err, ErrInvalidInspectorProps) {
		t.Fatalf("overflowing editor coordinate error = %v", err)
	}

	props.RelatedMessages = make([]domain.MessageID, MaximumRelatedMessages+1)
	for index := range props.RelatedMessages {
		props.RelatedMessages[index] = mustMessageID(t)
	}
	if err := props.Validate(); !errors.Is(err, ErrInvalidInspectorProps) {
		t.Fatalf("unbounded message list error = %v", err)
	}

	props = inspectorFixture(t)
	props.Cost.UnknownReason = "contradicts known cost"
	if err := props.Validate(); !errors.Is(err, ErrInvalidInspectorProps) {
		t.Fatalf("contradictory cost error = %v", err)
	}

	props = inspectorFixture(t)
	props.Actions.ExpandNeighbors = ActionAvailability{}
	props.OnExpandNeighbors = nil
	if err := props.Validate(); !errors.Is(err, ErrInvalidInspectorProps) {
		t.Fatalf("missing disabled reason error = %v", err)
	}

	props = inspectorFixture(t)
	props.Tokens.Usage.ProviderSpecific = make(map[string]domain.TokenCount, MaximumProviderTokenCategories+1)
	for index := 0; index <= MaximumProviderTokenCategories; index++ {
		props.Tokens.Usage.ProviderSpecific[string(rune('a'+index))] = 1
	}
	if err := props.Validate(); !errors.Is(err, ErrInvalidInspectorProps) {
		t.Fatalf("unbounded provider token categories error = %v", err)
	}
}

func renderInspector(t *testing.T, props Props) string {
	t.Helper()
	markup, err := ui.RenderToString(ui.CreateElement(ContextInspector, props))
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func inspectorFixture(t *testing.T) Props {
	t.Helper()
	eventID, err := domain.NewEventID()
	if err != nil {
		t.Fatal(err)
	}
	sources, err := taskgraph.NewSourceLinks(
		[]domain.EventID{eventID}, []taskgraph.PlanStepLink{{PlanRevision: 3, StepID: "validate-graph"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	contract, err := taskgraph.NewContractSummary(
		"Validate the selected graph projection.",
		[]string{"bounded task graph"}, []string{"validation evidence"}, []string{"repository read"},
	)
	if err != nil {
		t.Fatal(err)
	}
	nodeID, err := domain.NewNodeID()
	if err != nil {
		t.Fatal(err)
	}
	node, err := taskgraph.NewNode(
		nodeID, taskgraph.NodeClassAtomOperation, taskgraph.NodeStatusActive,
		"Validate task graph projection", contract, sources,
	)
	if err != nil {
		t.Fatal(err)
	}
	revisionID, err := domain.NewGraphRevisionID()
	if err != nil {
		t.Fatal(err)
	}
	evidenceID, err := domain.NewEvidenceID()
	if err != nil {
		t.Fatal(err)
	}
	location, err := NewSourceLocation(
		mustRepositoryID(t), strings.Repeat("a", 40), "internal/graph/projector.go", 42, 1, 78, 2,
	)
	if err != nil {
		t.Fatal(err)
	}
	currency, err := domain.ParseCurrencyCode("USD")
	if err != nil {
		t.Fatal(err)
	}
	money, err := domain.NewMoney(currency, 25)
	if err != nil {
		t.Fatal(err)
	}
	available := ActionAvailability{Enabled: true}
	return Props{
		RevisionID: revisionID, Node: node,
		Evidence: EvidenceView{
			Known: true, GuaranteeKnown: true, Guarantee: domain.AssuranceLevelContractChecked,
			GuaranteeReason: "typed contract and required validation are attached",
			SupportingItems: []EvidenceItem{{
				ID: evidenceID, Label: "Graph projection tests", State: domain.ValidationStatePassed,
				Summary: "All selected projection tests passed.",
			}},
		},
		Duration: DurationAttribution{Known: true, Value: 90 * time.Second},
		Tokens: TokenAttribution{Usage: domain.TokenUsage{
			Known: true, Input: 100, CachedInput: 25, CacheWrite: 5, Output: 30, Reasoning: 15,
		}},
		Cost:            CostAttribution{Known: true, Value: money, PricingSnapshot: "prices-2026-07-31"},
		RelatedMessages: []domain.MessageID{mustMessageID(t)}, SourceLocations: []SourceLocation{location},
		Actions: Actions{
			ExplainInChat: available, ExpandNeighbors: available, DependencyCone: available,
			EvidenceCone: available, CompareRevision: available, OpenInEditor: available,
		},
		OnExplainInChat: func(domain.NodeID) {}, OnExpandNeighbors: func(domain.NodeID) {},
		OnIsolateDependencyCone: func(domain.NodeID) {}, OnIsolateEvidenceCone: func(domain.NodeID) {},
		OnCompareRevision: func(domain.GraphRevisionID) {}, OnOpenInEditor: func(string, uint32) {},
	}
}

func unknownInspectorFixture(t *testing.T) Props {
	t.Helper()
	nodeID, err := domain.NewNodeID()
	if err != nil {
		t.Fatal(err)
	}
	node, err := taskgraph.NewNode(
		nodeID, taskgraph.NodeClassRequirement, taskgraph.NodeStatusPending,
		"Accepted requirement", taskgraph.ContractSummary{}, taskgraph.SourceLinks{},
	)
	if err != nil {
		t.Fatal(err)
	}
	revisionID, err := domain.NewGraphRevisionID()
	if err != nil {
		t.Fatal(err)
	}
	return Props{
		RevisionID: revisionID, Node: node,
		Evidence: EvidenceView{UnknownReason: "evidence query has not completed"},
		Duration: DurationAttribution{UnknownReason: "execution timing is unavailable"},
		Tokens:   TokenAttribution{UnknownReason: "provider usage is unavailable"},
		Cost:     CostAttribution{UnknownReason: "provider pricing is unavailable"},
		Actions: Actions{
			ExplainInChat:   ActionAvailability{DisabledReason: "Chat is disconnected"},
			ExpandNeighbors: ActionAvailability{DisabledReason: "Neighbor query is unavailable"},
			DependencyCone:  ActionAvailability{DisabledReason: "Dependency cone is unavailable"},
			EvidenceCone:    ActionAvailability{DisabledReason: "Evidence cone is unavailable"},
			CompareRevision: ActionAvailability{DisabledReason: "No comparison revision is selected"},
			OpenInEditor:    ActionAvailability{DisabledReason: "Select a node with a repository source"},
		},
	}
}

func mustRepositoryID(t *testing.T) domain.RepositoryID {
	t.Helper()
	value, err := domain.NewRepositoryID()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func mustMessageID(t *testing.T) domain.MessageID {
	t.Helper()
	value, err := domain.NewMessageID()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func buttonOpeningTagContains(markup, id, fragment string) bool {
	start := strings.Index(markup, `id="`+id+`"`)
	if start < 0 {
		return false
	}
	openingStart := strings.LastIndex(markup[:start], "<button")
	openingEnd := strings.Index(markup[start:], ">")
	if openingStart < 0 || openingEnd < 0 {
		return false
	}
	return strings.Contains(markup[openingStart:start+openingEnd], fragment)
}
