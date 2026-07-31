package threadrail

import (
	"fmt"
	"strings"

	"codeflux.dev/codeflux/web/frontend/primitives"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

const (
	VirtualThreadListID = "thread-rail-list"
	ThreadRowHeight     = 56
	ThreadListOverscan  = 4
)

// RowView is the complete typed presentation data required to render title,
// task state, last activity, attention, association, and unread status.
type RowView struct {
	Title          string
	TaskState      RailTaskState
	LastActivity   string
	Attention      Attention
	RepositoryID   string
	TaskID         string
	Unread         uint32
	Pending        bool
	Archived       bool
	AccessibleName string
}

// View returns the stable presentation fields for one row.
func (r Row) View() RowView {
	view := RowView{
		Title: r.Title(), TaskState: r.TaskState(),
		LastActivity: r.UpdatedAt().UTC().Format("2006-01-02 15:04:05Z"),
		Attention:    r.Attention(), Unread: r.Unread(), Pending: r.Pending(), Archived: r.Archived(),
	}
	if !r.Pending() {
		view.RepositoryID = r.Thread().RepositoryID().String()
		if !r.Thread().TaskID().IsZero() {
			view.TaskID = r.Thread().TaskID().String()
		}
	}
	parts := []string{view.Title, "state " + string(view.TaskState), "last activity " + view.LastActivity}
	if view.Attention != AttentionNone {
		parts = append(parts, "attention "+string(view.Attention))
	}
	if view.Unread > 0 {
		parts = append(parts, fmt.Sprintf("%d unread", view.Unread))
	}
	if view.Archived {
		parts = append(parts, "archived")
	}
	view.AccessibleName = strings.Join(parts, ", ")
	return view
}

// VirtualListContract binds immutable rail rows to the shared VirtualList
// primitive while keeping thread-specific fetch and pagination state explicit.
type VirtualListContract struct {
	Rows            []Row
	ActiveKey       RowKey
	Height          float64
	State           primitives.VirtualListState
	EmptyTitle      string
	EmptyBody       string
	ErrorTitle      string
	ErrorBody       string
	RetryLabel      string
	PaginationError PageError
	HasPageError    bool
	HasMore         bool
	LoadingNext     bool
}

// NewVirtualListContract derives the list integration from immutable state.
func NewVirtualListContract(state State, height float64) (VirtualListContract, error) {
	if height < ThreadRowHeight {
		return VirtualListContract{}, fmt.Errorf("%w: virtual-list height", ErrInvalidModel)
	}
	contract := VirtualListContract{
		Rows: state.Rows(), ActiveKey: state.ActiveRowKey(), Height: height,
		State: primitives.VirtualListReady, EmptyTitle: "No threads",
		EmptyBody: "Create a thread to begin work in this repository.",
		HasMore:   state.HasMore(), LoadingNext: state.LoadingNextPage(),
	}
	switch state.Presentation() {
	case PresentationLoadingSkeleton:
		contract.State = primitives.VirtualListLoading
	case PresentationPaginationError:
		failure, ok := state.PageError()
		if len(contract.Rows) == 0 {
			contract.State = primitives.VirtualListError
			contract.ErrorTitle = "Threads unavailable"
			contract.ErrorBody = failure.Message
			contract.RetryLabel = "Retry thread list"
		} else {
			contract.PaginationError, contract.HasPageError = failure, ok
		}
	case PresentationDisconnected:
		contract.State = primitives.VirtualListDisconnected
		contract.ErrorTitle = "Thread list disconnected"
		contract.ErrorBody = "Showing the last locally received thread data."
		contract.RetryLabel = "Reconnect thread list"
	case PresentationRepositoryUnavailable:
		contract.State = primitives.VirtualListError
		contract.ErrorTitle = "Repository unavailable"
		contract.ErrorBody = "The open repository can no longer be queried."
	case PresentationNoMatches:
		contract.EmptyTitle = "No matching threads"
		contract.EmptyBody = "Change the thread filter to see other results."
	case PresentationArchivedView:
		if len(contract.Rows) == 0 {
			contract.EmptyTitle = "No archived threads"
			contract.EmptyBody = "Archived threads will appear here."
		}
	}
	if len(contract.Rows) == 0 && contract.HasMore && contract.State == primitives.VirtualListReady {
		contract.State = primitives.VirtualListLoading
	}
	return contract, nil
}

// Props supplies the shared fixed-height virtualization primitive. The caller
// owns only row visuals and event dispatch; keys, labels, state, and sizing are
// fixed by this contract.
func (c VirtualListContract) Props(
	mode primitives.Mode,
	render func(primitives.VirtualListItemProps[Row]) ui.Node,
	onActive func(RowKey),
	onActivate func(RowKey),
	onRetry func(),
) primitives.VirtualListProps[Row] {
	return primitives.VirtualListProps[Row]{
		ID: VirtualThreadListID, Label: "Repository threads", Items: append([]Row(nil), c.Rows...),
		State: c.State, Height: c.Height, RowHeight: ThreadRowHeight, Overscan: ThreadListOverscan,
		Mode: mode, ActiveKey: string(c.ActiveKey),
		ItemKey:    func(row Row) string { return string(row.Key()) },
		ItemLabel:  func(row Row) string { return row.View().AccessibleName },
		RenderItem: render,
		OnActiveChange: func(key string) {
			if onActive != nil {
				onActive(RowKey(key))
			}
		},
		OnActivate: func(key string) {
			if onActivate != nil {
				onActivate(RowKey(key))
			}
		},
		EmptyTitle: c.EmptyTitle, EmptyBody: c.EmptyBody,
		LoadingLabel: "Loading repository threads",
		ErrorTitle:   c.ErrorTitle, ErrorBody: c.ErrorBody,
		DisconnectedTitle: c.ErrorTitle, DisconnectedBody: c.ErrorBody,
		RetryLabel: c.RetryLabel, OnRetry: onRetry,
	}
}

// ShouldRequestNextPage connects visible virtual-list range changes to the
// deterministic near-end pagination threshold.
func (c VirtualListContract) ShouldRequestNextPage(visibleEnd int) bool {
	if !c.HasMore || c.LoadingNext {
		return false
	}
	return len(c.Rows) == 0 || ShouldLoadNextPage(visibleEnd, len(c.Rows))
}
