//go:build !js || !wasm

package threadrail

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/web/frontend/primitives"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func renderThreadList(t *testing.T, contract VirtualListContract) string {
	t.Helper()
	props := contract.Props(
		primitives.Mode{ReducedMotion: true},
		func(item primitives.VirtualListItemProps[Row]) ui.Node { return html.Text(item.Item.Title()) },
		func(RowKey) {}, func(RowKey) {}, func() {},
	)
	node := ui.CreateElement(renderVirtualThreadList, props)
	output, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	return output
}

func renderVirtualThreadList(props primitives.VirtualListProps[Row]) ui.Node {
	return primitives.VirtualList(props)
}

func TestVirtualListContractPresentsLoadingEmptyAndRetryableError(t *testing.T) {
	fixture := newRailFixture(t)
	loading, _ := BeginFirstPage(fixture.state(t))
	contract, err := NewVirtualListContract(loading, 560)
	if err != nil {
		t.Fatal(err)
	}
	output := renderThreadList(t, contract)
	for _, want := range []string{`data-state="loading"`, `aria-label="Loading repository threads"`, `aria-busy="true"`} {
		if !strings.Contains(output, want) {
			t.Fatalf("loading output lacks %q: %s", want, output)
		}
	}
	failed := FailPage(loading, PageError{Code: "temporary", Message: "Try the local database again.", Retryable: true})
	contract, err = NewVirtualListContract(failed, 560)
	if err != nil {
		t.Fatal(err)
	}
	output = renderThreadList(t, contract)
	for _, want := range []string{`data-state="error"`, "Threads unavailable", "Retry thread list"} {
		if !strings.Contains(output, want) {
			t.Fatalf("error output lacks %q: %s", want, output)
		}
	}
	empty := loadThreads(t, fixture.state(t))
	contract, err = NewVirtualListContract(empty, 560)
	if err != nil {
		t.Fatal(err)
	}
	output = renderThreadList(t, contract)
	if !strings.Contains(output, "No threads") || !strings.Contains(output, `data-state="empty"`) {
		t.Fatalf("empty output = %s", output)
	}
}

func TestRowViewCarriesEveryThreadRailField(t *testing.T) {
	fixture := newRailFixture(t)
	thread := fixture.thread(
		t, "Needs a decision", time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC),
		TaskStatePaused, AttentionPendingApproval, 1, false,
	)
	row := loadThreads(t, fixture.state(t), thread).Rows()[0]
	view := row.View()
	if view.Title != "Needs a decision" || view.TaskState != TaskStatePaused ||
		view.Attention != AttentionPendingApproval || view.LastActivity != "2026-07-31 12:00:00Z" ||
		view.RepositoryID != fixture.repository.String() || view.TaskID == "" || view.Unread != 2 ||
		!strings.Contains(view.AccessibleName, "attention pending-approval") ||
		!strings.Contains(view.AccessibleName, "2 unread") {
		t.Fatalf("row view = %#v", view)
	}
}

func TestOneThousandThreadRowsUseBoundedVirtualizedOptions(t *testing.T) {
	fixture := newRailFixture(t)
	threads := make([]Thread, 1000)
	base := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	for index := range threads {
		threads[index] = fixture.thread(
			t, fmt.Sprintf("Thread %04d", index+1), base.Add(-time.Duration(index)*time.Second),
			TaskStateNone, AttentionNone, 1, false,
		)
	}
	state, _ := BeginFirstPage(fixture.state(t))
	for pageIndex := 0; pageIndex < 20; pageIndex++ {
		start := pageIndex * 50
		hasMore := pageIndex < 19
		nextCursor := Cursor("")
		if hasMore {
			nextCursor = Cursor(fmt.Sprintf("page-%02d", pageIndex+1))
		}
		requestCursor := Cursor("")
		if pageIndex > 0 {
			requestCursor = Cursor(fmt.Sprintf("page-%02d", pageIndex))
		}
		var err error
		state, err = ApplyPage(state, Page{
			RequestCursor: requestCursor, Threads: threads[start : start+50],
			NextCursor: nextCursor, HasMore: hasMore,
		})
		if err != nil {
			t.Fatalf("apply page %d: %v", pageIndex, err)
		}
		if hasMore {
			var ok bool
			state, _, ok = BeginNextPage(state)
			if !ok {
				t.Fatalf("page %d did not request next", pageIndex)
			}
		}
	}
	contract, err := NewVirtualListContract(state, 560)
	if err != nil {
		t.Fatal(err)
	}
	if len(contract.Rows) != 1000 || contract.ShouldRequestNextPage(999) {
		t.Fatalf("contract rows=%d hasMore=%v", len(contract.Rows), contract.HasMore)
	}
	output := renderThreadList(t, contract)
	optionCount := strings.Count(output, `role="option"`)
	if optionCount <= 0 || optionCount >= 1000 {
		t.Fatalf("virtual list rendered %d of 1000 options", optionCount)
	}
	for _, want := range []string{`role="listbox"`, `aria-setsize="1000"`, `data-component="virtual-list"`} {
		if !strings.Contains(output, want) {
			t.Fatalf("1000-row output lacks %q", want)
		}
	}
}

func TestPendingCommitKeepsVirtualOptionIdentity(t *testing.T) {
	fixture := newRailFixture(t)
	command := CreateCommand{Key: "stable-option", Title: "Pending", StartedAt: time.Now().UTC()}
	state, err := BeginCreate(fixture.state(t), command)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := NewVirtualListContract(state, 560)
	if err != nil {
		t.Fatal(err)
	}
	thread := fixture.thread(t, "Committed", command.StartedAt.Add(time.Second), TaskStateDraft, AttentionNone, 1, false)
	state, err = CommitCreate(state, command.Key, thread)
	if err != nil {
		t.Fatal(err)
	}
	committed, err := NewVirtualListContract(state, 560)
	if err != nil {
		t.Fatal(err)
	}
	if pending.Rows[0].Key() != committed.Rows[0].Key() || committed.Rows[0].Pending() {
		t.Fatalf("option identity changed from %q to %q", pending.Rows[0].Key(), committed.Rows[0].Key())
	}
}

func TestEmptyFilteredPageAdvancesPaginationBeforeClaimingNoMatches(t *testing.T) {
	fixture := newRailFixture(t)
	active := fixture.thread(t, "Active work", time.Now().UTC(), TaskStateRunning, AttentionNone, 1, false)
	loading, _ := BeginFirstPage(fixture.state(t))
	state, err := ApplyPage(loading, Page{
		Threads: []Thread{active}, NextCursor: "archived-page", HasMore: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	state, err = SetFilter(state, FilterArchived)
	if err != nil {
		t.Fatal(err)
	}
	contract, err := NewVirtualListContract(state, 560)
	if err != nil {
		t.Fatal(err)
	}
	if len(contract.Rows) != 0 || !contract.ShouldRequestNextPage(-1) {
		t.Fatalf("filtered page did not advance: rows=%d hasMore=%t loading=%t", len(contract.Rows), contract.HasMore, contract.LoadingNext)
	}
	if contract.State != primitives.VirtualListLoading {
		t.Fatalf("filtered page state = %s, want loading until pagination is exhausted", contract.State)
	}
}
