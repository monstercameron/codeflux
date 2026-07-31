package shell

import (
	"fmt"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"codeflux.dev/codeflux/web/frontend/routes"
	"codeflux.dev/codeflux/web/frontend/state"
	"codeflux.dev/codeflux/web/frontend/threadrail"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

const threadRailPreviewUUID = "01890f3c-4a00-7abc-8def-0123456789ab"

type TypedThreadRailPreviewProps struct {
	Snapshot   state.Snapshot
	Route      routes.Route
	Mode       primitives.Mode
	OnNavigate func(routes.Route)
}

// TypedThreadRailPreview mounts the immutable M17 rail in the canonical
// sidebar while the server-backed page client remains responsible for remote
// projections in production.
func TypedThreadRailPreview(props TypedThreadRailPreviewProps) ui.Node {
	initial, err := previewThreadRailState(props.Snapshot, props.Route)
	railState := ui.UseState(initial)
	archiveConfirmation := ui.UseState(domain.ThreadID{})
	commandSequence := ui.UseRef(uint64(0))
	if err != nil {
		return ui.CreateElement(threadrail.ThreadRail, threadrail.ThreadRailProps{
			State: initial, Mode: props.Mode, Embedded: true,
		})
	}
	applyState := func(next threadrail.State, reduceErr error) bool {
		if reduceErr != nil {
			return false
		}
		railState.Set(next)
		return true
	}
	routeDependency := props.Route.RepositoryID.String() + "|" + props.Route.ThreadID.String()
	ui.UseEffectOf(func() func() {
		current := railState.Get()
		if props.Route.Name != routes.ThreadWorkspace || props.Route.RepositoryID != current.RepositoryID() || props.Route.ThreadID.IsZero() {
			return nil
		}
		next, restoreErr := threadrail.RestoreSelection(current, props.Route)
		if restoreErr == nil {
			railState.Set(next)
		}
		return nil
	}, routeDependency)
	nextKey := func(operation string) threadrail.CommandKey {
		next := commandSequence.Get() + 1
		commandSequence.Set(next)
		return threadrail.CommandKey(fmt.Sprintf("preview-%s-%d", operation, next))
	}
	return ui.CreateElement(threadrail.ThreadRail, threadrail.ThreadRailProps{
		State: railState.Get(), Mode: props.Mode, Embedded: true, Height: 270,
		OnNewThread: func() {
			key := nextKey("create")
			startedAt := time.Now().UTC()
			pending, reduceErr := threadrail.BeginCreate(railState.Get(), threadrail.CreateCommand{
				Key: key, Title: "Untitled thread", StartedAt: startedAt,
			})
			if !applyState(pending, reduceErr) {
				return
			}
			ui.SafeGo("commit preview thread", func() {
				time.Sleep(300 * time.Millisecond)
				threadID, identityErr := domain.NewThreadID()
				if identityErr != nil {
					return
				}
				thread, projectionErr := threadrail.NewThread(threadrail.ThreadInput{
					ID: threadID, RepositoryID: pending.RepositoryID(),
					WorkspaceID: pending.WorkspaceID(), Title: "Untitled thread",
					TaskState: threadrail.TaskStateDraft, Attention: threadrail.AttentionNone,
					Revision: 1, UpdatedAt: startedAt.Add(time.Second),
				})
				if projectionErr != nil {
					return
				}
				committed, commitErr := threadrail.CommitCreate(railState.Get(), key, thread)
				if commitErr == nil {
					railState.Set(committed)
				}
			})
		},
		OnFilterChange: func(filter threadrail.Filter) {
			applyState(threadrail.SetFilter(railState.Get(), filter))
		},
		OnActiveChange: func(key threadrail.RowKey) {
			applyState(threadrail.SetActiveRow(railState.Get(), key))
		},
		OnSelect: func(key threadrail.RowKey) {
			next, route, reduceErr := threadrail.SelectThread(railState.Get(), key)
			if applyState(next, reduceErr) && props.OnNavigate != nil {
				props.OnNavigate(route)
			}
		},
		OnLoadNext: func() {
			current := railState.Get()
			loading, query, ok := threadrail.BeginNextPage(current)
			if !ok {
				return
			}
			railState.Set(loading)
			threads, pageErr := previewThreadRailThreads(props.Snapshot, current.RepositoryID(), current.WorkspaceID())
			if pageErr != nil || query.Cursor != previewThreadRailNextCursor {
				railState.Set(threadrail.FailPage(loading, threadrail.PageError{
					Code: "preview-pagination", Message: "More threads could not be loaded.", Retryable: true,
				}))
				return
			}
			start := min(previewThreadRailPageSize, len(threads))
			next, applyErr := threadrail.ApplyPage(loading, threadrail.Page{
				RequestCursor: query.Cursor, Threads: threads[start:],
			})
			applyState(next, applyErr)
		},
		OnRetry: func() {},
		OnRename: func(threadID domain.ThreadID) {
			current, row, ok := previewRailRow(railState.Get(), threadID)
			if !ok {
				return
			}
			key := nextKey("rename")
			pending, reduceErr := threadrail.BeginRename(current, threadrail.RenameCommand{
				Key: key, ThreadID: threadID, Title: row.Title() + " · refined",
				ExpectedRevision: row.Thread().Revision(),
			})
			if reduceErr != nil {
				return
			}
			updated, projectionErr := previewUpdatedThread(
				row.Thread(), row.Title()+" · refined", row.Archived(),
			)
			if projectionErr != nil {
				return
			}
			applyState(threadrail.CommitRename(pending, key, updated))
		},
		OnArchive: func(threadID domain.ThreadID, archived bool) {
			current, row, ok := previewRailRow(railState.Get(), threadID)
			if !ok {
				return
			}
			key := nextKey("archive")
			pending, reduceErr := threadrail.BeginArchive(current, threadrail.ArchiveCommand{
				Key: key, ThreadID: threadID, Archived: archived, Confirmed: true,
				ExpectedRevision: row.Thread().Revision(),
			})
			if reduceErr != nil {
				return
			}
			updated, projectionErr := previewUpdatedThread(row.Thread(), row.Title(), archived)
			if projectionErr != nil {
				return
			}
			if applyState(threadrail.CommitArchive(pending, key, updated)) {
				archiveConfirmation.Set(domain.ThreadID{})
			}
		},
		ArchiveConfirmation: archiveConfirmation.Get(),
		OnArchiveRequest:    archiveConfirmation.Set,
		OnArchiveCancel:     func() { archiveConfirmation.Set(domain.ThreadID{}) },
	})
}

const (
	previewThreadRailPageSize   = 3
	previewThreadRailNextCursor = threadrail.Cursor("preview-page-2")
)

func previewThreadRailState(snapshot state.Snapshot, route routes.Route) (threadrail.State, error) {
	repositoryID, err := domain.ParseRepositoryID("repo_" + threadRailPreviewUUID)
	if err != nil {
		return threadrail.State{}, err
	}
	workspaceID, err := domain.ParseWorkspaceID("wsp_" + threadRailPreviewUUID)
	if err != nil {
		return threadrail.State{}, err
	}
	railState, err := threadrail.NewState(repositoryID, workspaceID)
	if err != nil {
		return threadrail.State{}, err
	}
	hasRestoration := route.Name == routes.ThreadWorkspace && route.RepositoryID == repositoryID && !route.ThreadID.IsZero()
	if hasRestoration {
		railState, err = threadrail.RestoreSelection(railState, route)
		if err != nil {
			return threadrail.State{}, err
		}
	}
	loading, _ := threadrail.BeginFirstPage(railState)
	threads, err := previewThreadRailThreads(snapshot, repositoryID, workspaceID)
	if err != nil {
		return threadrail.State{}, err
	}
	pageEnd := min(previewThreadRailPageSize, len(threads))
	hasMore := pageEnd < len(threads)
	nextCursor := threadrail.Cursor("")
	if hasMore {
		nextCursor = previewThreadRailNextCursor
	}
	ready, err := threadrail.ApplyPage(loading, threadrail.Page{
		Threads: threads[:pageEnd], NextCursor: nextCursor, HasMore: hasMore,
	})
	if err != nil {
		return threadrail.State{}, err
	}
	if !hasRestoration && ready.SelectedThreadID().IsZero() {
		if rows := ready.Rows(); len(rows) > 0 {
			ready, _, err = threadrail.SelectThread(ready, rows[0].Key())
		}
	}
	return ready, err
}

func previewThreadRailThreads(snapshot state.Snapshot, repositoryID domain.RepositoryID, workspaceID domain.WorkspaceID) ([]threadrail.Thread, error) {
	threads := make([]threadrail.Thread, 0, len(snapshot.Threads()))
	for index, view := range snapshot.Threads() {
		rawUUID := fmt.Sprintf("01890f3c-4a00-7abc-8def-%012x", 0x123456789ab+index)
		threadID, parseErr := domain.ParseThreadID("thr_" + rawUUID)
		if parseErr != nil {
			return nil, parseErr
		}
		taskState, attention := previewRailStatus(view.Status)
		thread, projectionErr := threadrail.NewThread(threadrail.ThreadInput{
			ID: threadID, RepositoryID: repositoryID, WorkspaceID: workspaceID,
			Title: view.Title, TaskState: taskState, Attention: attention,
			Unread: previewUnread(view.Unread), Revision: 1,
			UpdatedAt: timelineFixtureEpoch.Add(-time.Duration(index) * time.Hour),
		})
		if projectionErr != nil {
			return nil, projectionErr
		}
		threads = append(threads, thread)
	}
	return threads, nil
}

func previewUnread(value int) uint32 {
	if value <= 0 {
		return 0
	}
	return uint32(value)
}

func previewRailStatus(raw string) (threadrail.TaskState, threadrail.Attention) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "active", "running", "in progress":
		return threadrail.TaskStateRunning, threadrail.AttentionNone
	case "complete", "completed":
		return threadrail.TaskStateComplete, threadrail.AttentionNone
	case "blocked", "recovery":
		return threadrail.TaskStateFailed, threadrail.AttentionRecovery
	case "waiting", "paused":
		return threadrail.TaskStatePaused, threadrail.AttentionUserInput
	default:
		return threadrail.TaskStateNone, threadrail.AttentionNone
	}
}

func previewRailRow(
	current threadrail.State,
	threadID domain.ThreadID,
) (threadrail.State, threadrail.Row, bool) {
	for _, row := range current.AllRows() {
		if !row.Pending() && row.ThreadID() == threadID {
			return current, row, true
		}
	}
	return current, threadrail.Row{}, false
}

func previewUpdatedThread(
	current threadrail.Thread,
	title string,
	archived bool,
) (threadrail.Thread, error) {
	return threadrail.NewThread(threadrail.ThreadInput{
		ID: current.ID(), RepositoryID: current.RepositoryID(), WorkspaceID: current.WorkspaceID(),
		TaskID: current.TaskID(), Title: title, TaskState: current.TaskState(),
		Attention: current.Attention(), Unread: current.Unread(), Archived: archived,
		Revision: current.Revision() + 1, UpdatedAt: time.Now().UTC(),
	})
}
