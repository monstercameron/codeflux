//go:build !js || !wasm

package threadrail

import (
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func renderRail(t *testing.T, props ThreadRailProps) string {
	t.Helper()
	output, err := ui.RenderToString(ui.CreateElement(ThreadRail, props))
	if err != nil {
		t.Fatal(err)
	}
	return output
}

func requireRailMarkup(t *testing.T, output string, values ...string) {
	t.Helper()
	for _, value := range values {
		if !strings.Contains(output, value) {
			t.Fatalf("thread rail lacks %q:\n%s", value, output)
		}
	}
}

func TestMountedThreadRailRendersMetadataAttentionUnreadSelectionAndActions(t *testing.T) {
	fixture := newRailFixture(t)
	thread := fixture.thread(
		t, "Review authorization", time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC),
		TaskStatePaused, AttentionPendingApproval, 1, false,
	)
	state := loadThreads(t, fixture.state(t), thread)
	var err error
	state, _, err = SelectThread(state, state.Rows()[0].Key())
	if err != nil {
		t.Fatal(err)
	}
	output := renderRail(t, ThreadRailProps{
		State: state, Height: 280, Mode: primitives.Mode{HighContrast: true, ReducedMotion: true},
		OnNewThread: func() {}, OnFilterChange: func(Filter) {}, OnActiveChange: func(RowKey) {},
		OnSelect: func(RowKey) {}, OnRetry: func() {}, OnRename: func(domain.ThreadID) {},
		OnArchiveRequest: func(domain.ThreadID) {}, OnArchive: func(domain.ThreadID, bool) {},
	})
	requireRailMarkup(t, output,
		`data-component="thread-rail"`, `aria-label="Thread navigation"`,
		`data-filter="active"`, `role="group"`, `aria-label="Thread filters"`,
		`aria-label="Show current threads"`, `aria-pressed="true"`,
		`role="listbox"`, `aria-label="Repository threads"`, `role="option"`,
		`aria-setsize="1"`, `data-component="thread-row"`, `data-selected="true"`,
		`title="Review authorization"`, `data-field="task-state"`, "Paused",
		`data-field="last-activity"`, "Jul 31",
		`data-field="attention"`, "Needs approval",
		`data-field="unread"`, `aria-label="2 unread"`,
		`aria-label="Selected thread actions"`, `aria-label="Rename thread Review authorization"`,
		`aria-label="Archive thread Review authorization"`,
	)
	for _, unwanted := range []string{`data-field="repository-association"`, `data-field="task-association"`, "Repository linked", "Task linked"} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("compact thread rail unexpectedly renders %q:\n%s", unwanted, output)
		}
	}
}

func TestMountedThreadRailRequiresExplicitArchiveConfirmation(t *testing.T) {
	fixture := newRailFixture(t)
	thread := fixture.thread(t, "Keep this work", time.Now().UTC(), TaskStatePaused, AttentionNone, 1, false)
	state := loadThreads(t, fixture.state(t), thread)
	state, _, err := SelectThread(state, state.Rows()[0].Key())
	if err != nil {
		t.Fatal(err)
	}

	initial := renderRail(t, ThreadRailProps{
		State: state, Height: 280, OnActiveChange: func(RowKey) {}, OnSelect: func(RowKey) {},
		OnArchiveRequest: func(domain.ThreadID) {}, OnArchive: func(domain.ThreadID, bool) {},
	})
	requireRailMarkup(t, initial, `aria-label="Archive thread Keep this work"`)
	if strings.Contains(initial, `data-component="archive-confirmation"`) {
		t.Fatalf("archive confirmation opened before an explicit request: %s", initial)
	}

	confirmed := renderRail(t, ThreadRailProps{
		State: state, Height: 280, OnActiveChange: func(RowKey) {}, OnSelect: func(RowKey) {},
		ArchiveConfirmation: thread.ID(), OnArchiveCancel: func() {}, OnArchive: func(domain.ThreadID, bool) {},
	})
	requireRailMarkup(t, confirmed,
		`data-component="archive-confirmation"`, `data-component="dialog"`,
		`data-focus-policy="trap-restore"`, `data-dismiss-policy="escape-outside"`,
		`id="thread-rail-archive"`, `aria-expanded="true"`, `aria-controls="thread-rail-archive-dialog"`,
		"Archive this thread?",
		`aria-label="Confirm archive thread Keep this work"`,
		`aria-label="Cancel archiving thread Keep this work"`,
	)
}

func TestMountedThreadRailRequestsNextPageOnlyNearLoadedEnd(t *testing.T) {
	fixture := newRailFixture(t)
	thread := fixture.thread(t, "First page", time.Now().UTC(), TaskStateRunning, AttentionNone, 1, false)
	loading, _ := BeginFirstPage(fixture.state(t))
	state, err := ApplyPage(loading, Page{Threads: []Thread{thread}, NextCursor: "next", HasMore: true})
	if err != nil {
		t.Fatal(err)
	}
	contract, err := NewVirtualListContract(state, 280)
	if err != nil {
		t.Fatal(err)
	}
	requests := 0
	if requestMountedNextPage(contract, -1, func() { requests++ }) || requests != 0 {
		t.Fatal("invalid range requested a page")
	}
	if !requestMountedNextPage(contract, 1, func() { requests++ }) || requests != 1 {
		t.Fatalf("near-end requests = %d", requests)
	}
	loadingState, _, ok := BeginNextPage(state)
	if !ok {
		t.Fatal("expected next page request")
	}
	loadingContract, err := NewVirtualListContract(loadingState, 280)
	if err != nil {
		t.Fatal(err)
	}
	if requestMountedNextPage(loadingContract, 1, func() { requests++ }) || requests != 1 {
		t.Fatal("loading contract duplicated a page request")
	}
}

func TestMountedThreadRailWiresViewportAndArchiveRequestHandlers(t *testing.T) {
	fixture := newRailFixture(t)
	thread := fixture.thread(t, "Mounted action", time.Now().UTC(), TaskStateRunning, AttentionNone, 1, false)
	loading, _ := BeginFirstPage(fixture.state(t))
	state, err := ApplyPage(loading, Page{Threads: []Thread{thread}, NextCursor: "next", HasMore: true})
	if err != nil {
		t.Fatal(err)
	}
	state, _, err = SelectThread(state, state.Rows()[0].Key())
	if err != nil {
		t.Fatal(err)
	}
	pageRequests := 0
	archiveRequests := 0
	archiveCommits := 0
	node := ThreadRail(ThreadRailProps{
		State: state, Height: 280,
		OnActiveChange: func(RowKey) {}, OnSelect: func(RowKey) {},
		OnLoadNext:       func() { pageRequests++ },
		OnArchiveRequest: func(domain.ThreadID) { archiveRequests++ },
		OnArchive:        func(domain.ThreadID, bool) { archiveCommits++ },
	})
	listProps, ok := findMountedVirtualList(node)
	if !ok || listProps.OnVisibleRangeChange == nil {
		t.Fatal("mounted rail did not retain the viewport-range callback")
	}
	listProps.OnVisibleRangeChange(1)
	if pageRequests != 1 {
		t.Fatalf("mounted next-page requests = %d", pageRequests)
	}
	archive, ok := findMountedRailButton(node, "Archive thread Mounted action")
	if !ok || archive == nil {
		t.Fatal("mounted rail did not retain the archive request handler")
	}
	archive()
	if archiveRequests != 1 || archiveCommits != 0 {
		t.Fatalf("archive request=%d commit=%d; archive bypassed confirmation", archiveRequests, archiveCommits)
	}
}

func findMountedVirtualList(node ui.Node) (primitives.VirtualListProps[Row], bool) {
	if raw, ok := node.Props["__ui_props"]; ok {
		if props, ok := raw.(primitives.VirtualListProps[Row]); ok {
			return props, true
		}
	}
	for _, child := range node.Children {
		childNode, ok := child.(ui.Node)
		if !ok {
			continue
		}
		if props, found := findMountedVirtualList(childNode); found {
			return props, true
		}
	}
	return primitives.VirtualListProps[Row]{}, false
}

func findMountedRailButton(node ui.Node, accessibleLabel string) (func(), bool) {
	if raw, ok := node.Props["__ui_props"]; ok {
		if props, ok := raw.(primitives.ButtonProps); ok && props.AccessibleLabel == accessibleLabel {
			return props.OnClick, true
		}
	}
	for _, child := range node.Children {
		childNode, ok := child.(ui.Node)
		if !ok {
			continue
		}
		if handler, found := findMountedRailButton(childNode, accessibleLabel); found {
			return handler, true
		}
	}
	return nil, false
}

func TestMountedThreadRailArchivedFilterShowsRestoreAffordance(t *testing.T) {
	fixture := newRailFixture(t)
	thread := fixture.thread(t, "Old work", time.Now().UTC(), TaskStateComplete, AttentionNone, 1, true)
	state := loadThreads(t, fixture.state(t), thread)
	var err error
	state, err = SetFilter(state, FilterArchived)
	if err != nil {
		t.Fatal(err)
	}
	state, _, err = SelectThread(state, state.Rows()[0].Key())
	if err != nil {
		t.Fatal(err)
	}
	output := renderRail(t, ThreadRailProps{
		State: state, Height: 280, OnNewThread: func() {}, OnFilterChange: func(Filter) {},
		OnActiveChange: func(RowKey) {}, OnSelect: func(RowKey) {}, OnRetry: func() {},
		OnRename: func(domain.ThreadID) {}, OnArchive: func(domain.ThreadID, bool) {},
	})
	requireRailMarkup(t, output,
		`data-filter="archived"`, `data-presentation="archived-view"`,
		`data-archived="true"`, "Archived", `aria-label="Restore archived thread Old work"`,
	)
}

func TestMountedThreadRailOwnsLoadingErrorEmptyAndPaginationStates(t *testing.T) {
	fixture := newRailFixture(t)
	loading, _ := BeginFirstPage(fixture.state(t))
	output := renderRail(t, ThreadRailProps{State: loading, Height: 280, OnRetry: func() {}})
	requireRailMarkup(t, output, `data-presentation="loading-skeleton"`, `data-state="loading"`, "Loading repository threads")

	failed := FailPage(loading, PageError{Code: "temporary", Message: "Retry the thread query.", Retryable: true})
	output = renderRail(t, ThreadRailProps{State: failed, Height: 280, OnRetry: func() {}})
	requireRailMarkup(t, output, `data-presentation="pagination-error"`, `data-state="error"`, "Threads unavailable", "Retry thread list")

	empty := loadThreads(t, fixture.state(t))
	output = renderRail(t, ThreadRailProps{State: empty, Height: 280})
	requireRailMarkup(t, output, `data-presentation="empty-repository"`, `data-state="empty"`, "No threads")

	thread := fixture.thread(t, "Cached", time.Now().UTC(), TaskStateRunning, AttentionNone, 1, false)
	state, _ := BeginFirstPage(fixture.state(t))
	state, err := ApplyPage(state, Page{Threads: []Thread{thread}, NextCursor: "next", HasMore: true})
	if err != nil {
		t.Fatal(err)
	}
	state, _, _ = BeginNextPage(state)
	state = FailPage(state, PageError{Code: "next", Message: "The next page is temporarily unavailable.", Retryable: true})
	output = renderRail(t, ThreadRailProps{
		State: state, Height: 280, OnActiveChange: func(RowKey) {}, OnSelect: func(RowKey) {}, OnRetry: func() {},
	})
	requireRailMarkup(t, output,
		`role="listbox"`, "Cached", `data-component="inline-alert"`,
		"More threads unavailable", "The next page is temporarily unavailable.",
		`aria-label="Retry loading more threads"`,
	)
}

func TestMountedThreadRailRejectsUnscopedState(t *testing.T) {
	output := renderRail(t, ThreadRailProps{State: State{}, Height: 280})
	requireRailMarkup(t, output, `data-state="invalid-contract"`, `role="alert"`, "repository and workspace state are required")
}
