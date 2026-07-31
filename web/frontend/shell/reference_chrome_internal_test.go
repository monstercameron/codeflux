package shell

import (
	"strings"
	"testing"

	"codeflux.dev/codeflux/web/frontend/design"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"codeflux.dev/codeflux/web/frontend/state"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestGraphNodeEmphasisKeepsIdleNodesNeutral(t *testing.T) {
	tokens, err := design.TokensFor(design.Options{Theme: design.ThemeDark, Density: design.DensityComfortable})
	if err != nil {
		t.Fatal(err)
	}
	fixtures := []struct {
		name     string
		node     state.GraphNodeView
		selected bool
		want     string
	}{
		{name: "idle complete", node: state.GraphNodeView{Title: "Repository", Status: "complete"}, want: "idle"},
		{name: "active", node: state.GraphNodeView{Title: "Tests", Status: "running"}, want: "active"},
		{name: "evidence", node: state.GraphNodeView{Title: "Past evidence", Status: "complete"}, want: "evidence"},
		{name: "blocked", node: state.GraphNodeView{Title: "Validation", Status: "failed"}, want: "blocked"},
		{name: "selected", node: state.GraphNodeView{Title: "Repository", Status: "complete"}, selected: true, want: "selected"},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			_, got := graphNodeEmphasis(fixture.node, fixture.selected, tokens)
			if got != fixture.want {
				t.Fatalf("emphasis = %q, want %q", got, fixture.want)
			}
		})
	}
}

func TestApplicationBarShortcutHelpButtonInvokesShellHandler(t *testing.T) {
	opened := false
	root := ApplicationBar(ApplicationBarProps{
		Mode:           primitives.Mode{},
		OnShortcutHelp: func() { opened = true },
	})
	handler, found := findButtonHandler(root, "Shortcut help")
	if !found {
		t.Fatal("application bar did not render the shortcut-help control")
	}
	if handler == nil {
		t.Fatal("shortcut-help control did not retain the shell open handler")
	}
	handler()
	if !opened {
		t.Fatal("shortcut-help control did not invoke the shell open handler")
	}
}

func TestApplicationBarSearchButtonInvokesOpenHandler(t *testing.T) {
	opened := false
	root := ApplicationBar(ApplicationBarProps{
		Mode: primitives.Mode{}, OnSearchOpen: func() { opened = true },
	})
	handler, found := findButtonHandler(root, "Search")
	if !found || handler == nil {
		t.Fatal("application bar search control did not retain its open handler")
	}
	handler()
	if !opened {
		t.Fatal("application bar search control did not invoke its open handler")
	}
}

func TestApplicationBarExposesManualReconnectOnlyWhenDisconnected(t *testing.T) {
	requested := false
	root := ApplicationBar(ApplicationBarProps{
		Session: state.SessionView{Connection: state.ConnectionDisconnected},
		Mode:    primitives.Mode{}, OnReconnectRequested: func() { requested = true },
	})
	handler, found := findButtonHandler(root, "Reconnect live session")
	if !found || handler == nil {
		t.Fatal("disconnected application bar did not expose manual reconnect")
	}
	handler()
	if !requested {
		t.Fatal("manual reconnect did not invoke the session restart handler")
	}
	live := ApplicationBar(ApplicationBarProps{
		Session: state.SessionView{Connection: state.ConnectionLive},
		Mode:    primitives.Mode{}, OnReconnectRequested: func() {},
	})
	if _, found := findButtonHandler(live, "Reconnect live session"); found {
		t.Fatal("live application bar exposed an unnecessary reconnect action")
	}
}

func TestApplicationBarLabelsPausedTaskControlAsResumeAndShowsAuthoritativeFacts(t *testing.T) {
	root := ApplicationBar(ApplicationBarProps{
		Session: state.SessionView{Connection: state.ConnectionLive},
		View: state.TopBarView{
			TaskState: "paused", Provider: "openai", Model: "gpt-5.6-sol", Effort: "high",
			ForecastCost: "USD 25-40 minor units", ActualTokens: "150 tokens",
			ActualCost: "USD 18 minor units", PricingSnapshot: "price-17",
			HardBudget: "USD 100 minor units", RemainingBudget: "USD 82 minor units",
			BudgetWarning: "below threshold", CanPause: true,
		},
		Mode:             primitives.Mode{Theme: design.ThemeDark, Density: design.DensityComfortable},
		OnPauseRequested: func() {},
	})
	markup, err := ui.RenderToString(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"openai · gpt-5.6-sol · high", "150 tokens", "price-17",
		"USD 82 minor units", "below threshold",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("authoritative application bar missing %q: %s", want, markup)
		}
	}
	if label, accessible := taskPausePresentation("paused", false); label != "▶  Resume" || accessible != "Resume task" {
		t.Fatalf("paused task presentation = %q/%q", label, accessible)
	}
}

func TestSearchDialogRoutesAndDismissesThroughClientNavigation(t *testing.T) {
	dismissed := false
	navigated := ""
	root := SearchDialog(SearchDialogProps{
		Open: true, Mode: primitives.Mode{},
		OnDismiss:      func() { dismissed = true },
		OnNavigatePath: func(path string) { navigated = path },
	})
	handler, found := findButtonHandler(root, "Search task graph")
	if !found || handler == nil {
		t.Fatal("search dialog did not render its task-graph destination")
	}
	handler()
	if !dismissed || navigated != "/graphs" {
		t.Fatalf("search graph action = dismissed %t, path %q", dismissed, navigated)
	}
}

func TestSearchDialogFiltersDestinationsWithoutInventingResults(t *testing.T) {
	root := SearchDialog(SearchDialogProps{
		Open: true, Query: "graph", Mode: primitives.Mode{},
		OnNavigatePath: func(string) {},
	})
	if _, found := findButtonHandler(root, "Search task graph"); !found {
		t.Fatal("graph query removed the matching graph destination")
	}
	if _, found := findButtonHandler(root, "Search tasks"); found {
		t.Fatal("graph query retained a non-matching task destination")
	}
}

func TestTaskActionsPopoverInvokesStateAwareActions(t *testing.T) {
	paused := false
	dismissed := false
	store := state.NewStore(state.NewSnapshot(nil, nil, nil))
	store = store.ReduceRemote(state.TopBarChanged{TopBar: state.TopBarView{
		TaskState: "paused", CanPause: true, CanStop: true,
	}})
	snapshot := store.Snapshot()
	root := TaskActionsPopover(TaskActionsPopoverProps{
		Open: true, Snapshot: snapshot, Mode: primitives.Mode{},
		OnDismiss:        func() { dismissed = true },
		OnPauseRequested: func() { paused = true },
	})
	handler, found := findButtonHandler(root, "Resume task from task actions")
	if !found || handler == nil {
		t.Fatal("paused task actions did not render a working Resume action")
	}
	handler()
	if !paused || !dismissed {
		t.Fatalf("resume task action = invoked %t, dismissed %t", paused, dismissed)
	}
}

func TestTaskWorkspaceHeaderMoreActionsInvokesOpenHandler(t *testing.T) {
	opened := false
	root := TaskWorkspaceHeader(TaskWorkspaceHeaderProps{
		Mode: primitives.Mode{}, OnTaskActionsOpen: func() { opened = true },
	})
	handler, found := findButtonHandler(root, "More task actions")
	if !found || handler == nil {
		t.Fatal("task header did not render a working More task actions control")
	}
	handler()
	if !opened {
		t.Fatal("task header More task actions control did not invoke its open handler")
	}
}

func TestShortcutHelpCloseButtonInvokesDismissHandler(t *testing.T) {
	dismissed := false
	root := ShortcutHelpDialog(ShortcutHelpDialogProps{
		Open: true, Mode: primitives.Mode{}, OnDismiss: func() { dismissed = true },
	})
	handler, found := findButtonHandler(root, "Close keyboard shortcut help")
	if !found || handler == nil {
		t.Fatal("shortcut-help dialog did not retain its close handler")
	}
	handler()
	if !dismissed {
		t.Fatal("shortcut-help close control did not invoke the dismiss handler")
	}
}

func TestRailWidthStopsAreReversibleAndBounded(t *testing.T) {
	for _, width := range railWidthStops {
		if width != railWidthStops[0] {
			narrower := nextRailWidth(width, -1)
			if restored := nextRailWidth(narrower, 1); restored != width {
				t.Fatalf("narrow/widen from %d restored %d via %d", width, restored, narrower)
			}
		}
		if width != railWidthStops[len(railWidthStops)-1] {
			wider := nextRailWidth(width, 1)
			if restored := nextRailWidth(wider, -1); restored != width {
				t.Fatalf("widen/narrow from %d restored %d via %d", width, restored, wider)
			}
		}
	}
	if got := nextRailWidth(224, -1); got != 224 {
		t.Fatalf("minimum width narrowed to %d", got)
	}
	if got := nextRailWidth(480, 1); got != 480 {
		t.Fatalf("maximum width widened to %d", got)
	}
	if got := nextRailWidth(240, -1); got != 224 || nextRailWidth(got, 1) != 240 {
		t.Fatalf("default width pair is not reversible: narrow=%d restore=%d", got, nextRailWidth(got, 1))
	}
}

func findButtonHandler(node ui.Node, accessibleLabel string) (func(), bool) {
	if node == nil {
		return nil, false
	}
	if raw, ok := node.Props["__ui_props"]; ok {
		if overlay, ok := raw.(ui.AccessibleOverlayProps); ok {
			if handler, found := findButtonHandler(overlay.Child, accessibleLabel); found {
				return handler, true
			}
		}
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
