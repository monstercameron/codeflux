package primitives

import (
	"fmt"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type virtualListFixture struct {
	ID    string
	Label string
}

func TestResolveVirtualListKeyClampsNavigationAndActivates(t *testing.T) {
	keys := []string{"a", "b", "c", "d", "e"}
	cases := []struct {
		name, active, key string
		page              int
		want              VirtualListKeyDecision
	}{
		{name: "down", active: "b", key: "ArrowDown", page: 2, want: VirtualListKeyDecision{ActiveKey: "c", TargetIndex: 2, Handled: true}},
		{name: "up clamps", active: "a", key: "ArrowUp", page: 2, want: VirtualListKeyDecision{ActiveKey: "a", TargetIndex: 0, Handled: true}},
		{name: "page down", active: "b", key: "PageDown", page: 2, want: VirtualListKeyDecision{ActiveKey: "d", TargetIndex: 3, Handled: true}},
		{name: "page up clamps", active: "b", key: "PageUp", page: 4, want: VirtualListKeyDecision{ActiveKey: "a", TargetIndex: 0, Handled: true}},
		{name: "end", active: "a", key: "End", page: 2, want: VirtualListKeyDecision{ActiveKey: "e", TargetIndex: 4, Handled: true}},
		{name: "enter activates", active: "c", key: "Enter", page: 2, want: VirtualListKeyDecision{ActiveKey: "c", TargetIndex: 2, Handled: true, Activate: true}},
		{name: "unhandled", active: "c", key: "Tab", page: 2, want: VirtualListKeyDecision{ActiveKey: "c", TargetIndex: 2}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := ResolveVirtualListKey(keys, test.active, test.key, test.page); got != test.want {
				t.Fatalf("ResolveVirtualListKey() = %+v, want %+v", got, test.want)
			}
		})
	}
	if got := ResolveVirtualListKey(nil, "", "End", 1); got.TargetIndex != -1 || got.Handled {
		t.Fatalf("empty list decision = %+v", got)
	}
}

func TestVirtualListScrollTopRevealsOnlyHiddenRows(t *testing.T) {
	for _, test := range []struct {
		name                   string
		current, viewport, row float64
		index                  int
		want                   float64
	}{
		{name: "already visible", current: 96, viewport: 144, row: 48, index: 3, want: 96},
		{name: "above", current: 96, viewport: 144, row: 48, index: 1, want: 48},
		{name: "below", current: 0, viewport: 144, row: 48, index: 4, want: 96},
		{name: "invalid retains current", current: 24, viewport: 0, row: 48, index: 4, want: 24},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := VirtualListScrollTop(test.current, test.viewport, test.row, test.index); got != test.want {
				t.Fatalf("VirtualListScrollTop() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestVirtualListRendersBoundedOptionsWithStableFullLabels(t *testing.T) {
	items := make([]virtualListFixture, 20)
	for index := range items {
		items[index] = virtualListFixture{
			ID:    fmt.Sprintf("atom-%02d", index),
			Label: fmt.Sprintf("ReconcileAmbiguousProviderOutcomeForRepositoryTask%02d", index),
		}
	}
	output := render(t, VirtualList(VirtualListProps[virtualListFixture]{
		ID: "atom-list", Label: "Repository atoms", Items: items,
		Height: 96, RowHeight: 48, Overscan: 1, ActiveKey: "atom-01",
		Mode:           Mode{HighContrast: true, ReducedMotion: true},
		ItemKey:        func(item virtualListFixture) string { return item.ID },
		ItemLabel:      func(item virtualListFixture) string { return item.Label },
		OnActiveChange: func(string) {},
		RenderItem: func(item VirtualListItemProps[virtualListFixture]) ui.Node {
			return html.Text("Reconcile…" + item.Item.ID)
		},
	}))
	requireContains(t, output,
		`role="listbox"`,
		`aria-label="Repository atoms"`,
		`tabIndex="0"`,
		`data-component="virtual-list"`,
		`data-keyboard="arrows-page-home-end-enter-space"`,
		`data-focus-policy="active-descendant"`,
		`data-high-contrast="true"`,
		`data-reduced-motion="true"`,
		`aria-label="ReconcileAmbiguousProviderOutcomeForRepositoryTask01"`,
		`title="ReconcileAmbiguousProviderOutcomeForRepositoryTask01"`,
		`data-full-label="ReconcileAmbiguousProviderOutcomeForRepositoryTask01"`,
		`aria-selected="true"`,
		`aria-setsize="20"`,
	)
	if count := strings.Count(output, `role="option"`); count >= len(items) || count < 2 {
		t.Fatalf("virtualization rendered %d of %d options: %s", count, len(items), output)
	}
}

func TestVirtualListPresentsEveryOwnedDataState(t *testing.T) {
	cases := []struct {
		name  string
		props VirtualListProps[virtualListFixture]
		want  []string
	}{
		{name: "loading", props: VirtualListProps[virtualListFixture]{ID: "loading", Label: "Tasks", State: VirtualListLoading, LoadingLabel: "Loading tasks", Mode: Mode{ReducedMotion: true}}, want: []string{`data-state="loading"`, `aria-label="Loading tasks"`, `aria-busy="true"`}},
		{name: "empty", props: VirtualListProps[virtualListFixture]{ID: "empty", Label: "Tasks", EmptyTitle: "No tasks", EmptyBody: "Start a task to see it here."}, want: []string{`data-state="empty"`, `data-component="empty-state"`, "No tasks"}},
		{name: "error", props: VirtualListProps[virtualListFixture]{ID: "error", Label: "Tasks", State: VirtualListError, ErrorTitle: "Tasks unavailable", ErrorBody: "Try again.", RetryLabel: "Retry", OnRetry: func() {}}, want: []string{`data-state="error"`, `data-component="error-state"`, `role="alert"`, "Retry"}},
		{name: "disconnected", props: VirtualListProps[virtualListFixture]{ID: "offline", Label: "Tasks", State: VirtualListDisconnected, DisconnectedTitle: "Connection lost", DisconnectedBody: "Showing no task data.", RetryLabel: "Reconnect", OnRetry: func() {}}, want: []string{`data-state="disconnected"`, `data-component="disconnected-state"`, `role="status"`, "Reconnect"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			output := render(t, VirtualList(test.props))
			requireContains(t, output, append([]string{`data-component="virtual-list"`, `role="region"`, `aria-label="Tasks"`}, test.want...)...)
		})
	}
}

func TestVirtualListRejectsAmbiguousOrUnsafeContracts(t *testing.T) {
	base := VirtualListProps[virtualListFixture]{
		ID: "atoms", Label: "Atoms", Height: 96, RowHeight: 48,
		Items:          []virtualListFixture{{ID: "same", Label: "First"}, {ID: "same", Label: "Second"}},
		ItemKey:        func(item virtualListFixture) string { return item.ID },
		ItemLabel:      func(item virtualListFixture) string { return item.Label },
		OnActiveChange: func(string) {},
		RenderItem:     func(item VirtualListItemProps[virtualListFixture]) ui.Node { return html.Text(item.FullLabel) },
	}
	duplicate := render(t, VirtualList(base))
	requireContains(t, duplicate, `data-state="invalid-contract"`, "item keys must be unique")

	base.Items = base.Items[:1]
	base.RowHeight = 20
	undersized := render(t, VirtualList(base))
	requireContains(t, undersized, `data-state="invalid-contract"`, "minimum pointer target")

	missingLoadingLabel := render(t, VirtualList(VirtualListProps[virtualListFixture]{ID: "loading", Label: "Atoms", State: VirtualListLoading}))
	requireContains(t, missingLoadingLabel, `data-state="invalid-contract"`, "loading label is required")

	base.RowHeight = 48
	base.OnActiveChange = nil
	missingKeyboardOwner := render(t, VirtualList(base))
	requireContains(t, missingKeyboardOwner, `data-state="invalid-contract"`, "active change handler is required")

	missingRetryHandler := render(t, VirtualList(VirtualListProps[virtualListFixture]{
		ID: "error", Label: "Atoms", State: VirtualListError, ErrorTitle: "Unavailable", RetryLabel: "Retry",
	}))
	requireContains(t, missingRetryHandler, `data-state="invalid-contract"`, "retry action requires a handler")
}
