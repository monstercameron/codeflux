package shell

import (
	"fmt"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/routes"
	"codeflux.dev/codeflux/web/frontend/state"
	"codeflux.dev/codeflux/web/frontend/threadrail"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestPreviewThreadRailRestoresSelectionWhenLaterPageArrives(t *testing.T) {
	views := make([]state.ThreadView, 5)
	for index := range views {
		views[index] = state.ThreadView{ID: fmt.Sprintf("thread-%d", index+1), Title: fmt.Sprintf("Thread %d", index+1)}
	}
	snapshot := state.NewSnapshot(views, nil, nil)
	repositoryID, err := domain.ParseRepositoryID("repo_" + threadRailPreviewUUID)
	if err != nil {
		t.Fatal(err)
	}
	workspaceID, err := domain.ParseWorkspaceID("wsp_" + threadRailPreviewUUID)
	if err != nil {
		t.Fatal(err)
	}
	targetID, err := domain.ParseThreadID("thr_01890f3c-4a00-7abc-8def-0123456789ae")
	if err != nil {
		t.Fatal(err)
	}
	route := routes.Route{Name: routes.ThreadWorkspace, RepositoryID: repositoryID, ThreadID: targetID}
	railState, err := previewThreadRailState(snapshot, route)
	if err != nil {
		t.Fatal(err)
	}
	if !railState.SelectedThreadID().IsZero() {
		t.Fatalf("later-page selection resolved too early: %s", railState.SelectedThreadID())
	}
	loading, query, ok := threadrail.BeginNextPage(railState)
	if !ok || query.Cursor != previewThreadRailNextCursor {
		t.Fatalf("next page query = %+v, ok=%v", query, ok)
	}
	threads, err := previewThreadRailThreads(snapshot, repositoryID, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	railState, err = threadrail.ApplyPage(loading, threadrail.Page{
		RequestCursor: query.Cursor,
		Threads:       threads[previewThreadRailPageSize:],
	})
	if err != nil {
		t.Fatal(err)
	}
	if railState.SelectedThreadID() != targetID {
		t.Fatalf("restored selection = %s, want %s", railState.SelectedThreadID(), targetID)
	}
}

func TestProductSidebarUsesMountedThreadRailSeam(t *testing.T) {
	snapshot := state.NewSnapshot(nil, nil, nil)
	snapshot.Layout = state.DefaultLayoutPreferences()
	markup, err := ui.RenderToString(ui.CreateElement(ProductSidebar, ProductSidebarProps{
		Snapshot: snapshot,
		ThreadRail: html.Div(html.Props{
			Data: map[string]string{"component": "authoritative-thread-rail-test"},
		}),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `data-component="authoritative-thread-rail-test"`) {
		t.Fatalf("product sidebar did not render mounted rail seam: %s", markup)
	}
	if strings.Contains(markup, `data-transport-mode="local-preview-fallback"`) {
		t.Fatalf("product sidebar rendered preview fallback beside mounted rail: %s", markup)
	}
}

func TestCompactProductSidebarUsesDismissibleModalDrawerContract(t *testing.T) {
	snapshot := state.NewSnapshot(nil, nil, nil)
	snapshot.Layout = state.LayoutPreferences{
		Viewport: state.ViewportNarrow, ActivePane: state.PaneConversation,
		RailWidth: 280, SplitPercent: 60,
	}
	markup, err := ui.RenderToString(ui.CreateElement(ProductSidebar, ProductSidebarProps{
		Snapshot: snapshot, CompactOpen: true,
		ThreadRail:     html.Div(html.Props{Data: map[string]string{"component": "mounted-thread-rail"}}),
		OnNavigatePath: func(string) {}, OnCollapse: func() {},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, contract := range []string{
		`data-component="drawer"`, `data-focus-policy="trap-restore"`,
		`data-dismiss-policy="escape-outside"`, `id="product-sidebar-close"`,
		`id="product-sidebar-navigation"`,
		`aria-label="Primary navigation"`, `data-component="mounted-thread-rail"`,
	} {
		if !strings.Contains(markup, contract) {
			t.Fatalf("compact sidebar missing overlay contract %q: %s", contract, markup)
		}
	}
}
