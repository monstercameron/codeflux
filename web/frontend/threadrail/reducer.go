package threadrail

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/routes"
)

type commandOperation string

const (
	commandCreate  commandOperation = "create"
	commandRename  commandOperation = "rename"
	commandArchive commandOperation = "archive"
)

// CommandStatus is the observable client ownership state of one mutation.
type CommandStatus string

const (
	CommandPending   CommandStatus = "pending"
	CommandCommitted CommandStatus = "committed"
)

type commandRecord struct {
	operation   commandOperation
	fingerprint string
	status      CommandStatus
	threadID    domain.ThreadID
}

// OwnedCommand reports whether a key is retained and its current status.
func (s State) OwnedCommand(key CommandKey) (CommandStatus, bool) {
	record, ok := s.commands[key]
	return record.status, ok
}

// BeginFirstPage transitions the rail to its stable loading-skeleton state and
// returns the bounded query for the open repository workspace.
func BeginFirstPage(state State) (State, PageQuery) {
	next := state.clone()
	next.load = LoadLoading
	next.requested = ""
	next.requestedSet = true
	next.loadingNext = false
	next.hasPageError = false
	next.pageError = PageError{}
	next.applied = make(map[Cursor]struct{})
	return next, pageQuery(next, "")
}

// BeginNextPage returns one request only when more data exists and no page is
// already in flight. Repeated approach-to-end signals are therefore harmless.
func BeginNextPage(state State) (State, PageQuery, bool) {
	if state.load != LoadReady || !state.hasMore || state.loadingNext || state.nextCursor == "" {
		return state, PageQuery{}, false
	}
	next := state.clone()
	next.loadingNext = true
	next.requested = state.nextCursor
	next.requestedSet = true
	next.hasPageError = false
	next.pageError = PageError{}
	return next, pageQuery(next, next.requested), true
}

// BeginPageRetry reuses the original cursor and does not create a mutation or
// a second logical list request.
func BeginPageRetry(state State) (State, PageQuery, bool) {
	if !state.hasPageError || !state.pageError.Retryable || !state.requestedSet {
		return state, PageQuery{}, false
	}
	next := state.clone()
	next.hasPageError = false
	next.pageError = PageError{}
	if next.requested == "" {
		next.load = LoadLoading
	} else {
		next.loadingNext = true
	}
	return next, pageQuery(next, next.requested), true
}

func pageQuery(state State, cursor Cursor) PageQuery {
	return PageQuery{
		RepositoryID: state.repositoryID, WorkspaceID: state.workspaceID,
		Cursor: cursor, Limit: DefaultPageLimit, IncludeArchived: true,
	}
}

// ShouldLoadNextPage reports when a virtualized rail is within the fixed
// prefetch threshold of its last loaded row.
func ShouldLoadNextPage(visibleEnd, total int) bool {
	if total <= 0 || visibleEnd < 0 {
		return false
	}
	return visibleEnd >= total-1-PaginationThreshold
}

// ApplyPage validates, deduplicates, merges, and deterministically sorts one
// requested page. Reapplying the same cursor is idempotent.
func ApplyPage(state State, page Page) (State, error) {
	if _, duplicate := state.applied[page.RequestCursor]; duplicate {
		return state, nil
	}
	if !state.requestedSet || page.RequestCursor != state.requested {
		return state, ErrUnexpectedPage
	}
	if page.HasMore && page.NextCursor == "" {
		return state, fmt.Errorf("%w: has_more requires next cursor", ErrInvalidPage)
	}
	if !page.HasMore && page.NextCursor != "" {
		return state, fmt.Errorf("%w: terminal page has next cursor", ErrInvalidPage)
	}
	for _, thread := range page.Threads {
		if thread.RepositoryID() != state.repositoryID || thread.WorkspaceID() != state.workspaceID {
			return state, fmt.Errorf("%w: page crosses repository or workspace", ErrInvalidPage)
		}
	}
	next := state.clone()
	if page.RequestCursor == "" {
		pending := next.rows[:0]
		for _, row := range next.rows {
			if row.pending {
				pending = append(pending, row)
			}
		}
		next.rows = pending
	}
	for _, thread := range page.Threads {
		next.mergeThread(thread)
	}
	next.applied[page.RequestCursor] = struct{}{}
	next.nextCursor = page.NextCursor
	next.hasMore = page.HasMore
	next.load = LoadReady
	next.loadingNext = false
	next.requestedSet = false
	next.hasPageError = false
	next.pageError = PageError{}
	next.sortRows()
	next.restoreSelectionIfPresent()
	next.repairActiveKey()
	return next, nil
}

// FailPage records attributable retry state without discarding already loaded
// rows after a next-page failure.
func FailPage(state State, failure PageError) State {
	next := state.clone()
	next.loadingNext = false
	next.pageError = failure
	next.hasPageError = true
	if next.requested == "" {
		next.load = LoadRecoverableError
	} else {
		next.load = LoadReady
	}
	return next
}

// RepositoryUnavailable enters the explicit unavailable presentation.
func RepositoryUnavailable(state State) State {
	next := state.clone()
	next.load = LoadRepositoryUnavailable
	next.loadingNext = false
	return next
}

// Disconnected preserves rows but explicitly marks their authority as stale.
func Disconnected(state State) State {
	next := state.clone()
	next.load = LoadDisconnected
	next.loadingNext = false
	return next
}

// SetFilter changes archived membership without mutating authoritative rows.
func SetFilter(state State, filter Filter) (State, error) {
	if !validFilter(filter) {
		return state, fmt.Errorf("%w: filter", ErrInvalidModel)
	}
	next := state.clone()
	next.filter = filter
	next.repairActiveKey()
	return next, nil
}

// SetActiveRow changes only the virtual-list active descendant.
func SetActiveRow(state State, key RowKey) (State, error) {
	if _, ok := state.visibleRowByKey(key); !ok {
		return state, ErrThreadNotFound
	}
	next := state.clone()
	next.activeKey = key
	return next, nil
}

// SelectThread returns the canonical typed route for a committed row.
func SelectThread(state State, key RowKey) (State, routes.Route, error) {
	row, ok := state.visibleRowByKey(key)
	if !ok {
		return state, routes.Route{}, ErrThreadNotFound
	}
	if row.pending {
		return state, routes.Route{}, ErrPendingThread
	}
	next := state.clone()
	next.selected = row.thread.ID()
	next.restore = row.thread.ID()
	next.activeKey = row.key
	return next, routes.Route{
		Name: routes.ThreadWorkspace, RepositoryID: state.repositoryID,
		ThreadID: row.thread.ID(),
	}, nil
}

// RestoreSelection retains an authorized route selection even when the row is
// on a later page, then resolves it as soon as ApplyPage encounters the thread.
func RestoreSelection(state State, route routes.Route) (State, error) {
	if route.Name != routes.ThreadWorkspace || route.RepositoryID != state.repositoryID || route.ThreadID.IsZero() {
		return state, fmt.Errorf("%w: selection route", ErrInvalidModel)
	}
	next := state.clone()
	next.restore = route.ThreadID
	next.restoreSelectionIfPresent()
	return next, nil
}

// CreateCommand owns one new-thread request and its local pending row.
type CreateCommand struct {
	Key       CommandKey
	Title     string
	StartedAt time.Time
}

// BeginCreate adds one pending row without changing current selection or focus.
func BeginCreate(state State, command CreateCommand) (State, error) {
	title := strings.TrimSpace(command.Title)
	if _, err := ParseCommandKey(string(command.Key)); err != nil || title == "" ||
		len(title) > maximumTitleBytes || command.StartedAt.IsZero() {
		return state, fmt.Errorf("%w: create command", ErrInvalidModel)
	}
	fingerprint := commandFingerprint(commandCreate, state.workspaceID.String(), title)
	if existing, ok := state.commands[command.Key]; ok {
		if existing.operation != commandCreate || existing.fingerprint != fingerprint {
			return state, ErrIdempotencyConflict
		}
		return state, nil
	}
	next := state.clone()
	next.nextOrder++
	next.rows = append(next.rows, Row{
		key: RowKey("command:" + string(command.Key)), pending: true,
		command: command.Key, title: title, startedAt: command.StartedAt.UTC(), order: next.nextOrder,
	})
	next.commands[command.Key] = commandRecord{
		operation: commandCreate, fingerprint: fingerprint, status: CommandPending,
	}
	next.sortRows()
	return next, nil
}

// CommitCreate replaces its pending row in place while retaining RowKey,
// active descendant, and current thread selection.
func CommitCreate(state State, key CommandKey, thread Thread) (State, error) {
	record, ok := state.commands[key]
	if !ok || record.operation != commandCreate {
		return state, ErrCommandNotPending
	}
	if record.status == CommandCommitted {
		if record.threadID == thread.ID() {
			return state, nil
		}
		return state, ErrIdempotencyConflict
	}
	if thread.RepositoryID() != state.repositoryID || thread.WorkspaceID() != state.workspaceID {
		return state, fmt.Errorf("%w: committed thread scope", ErrInvalidModel)
	}
	next := state.clone()
	pendingIndex := -1
	authoritative := thread
	for index, row := range next.rows {
		if row.pending && row.command == key {
			pendingIndex = index
			continue
		}
		if !row.pending && row.thread.ID() == thread.ID() && row.thread.Revision() > authoritative.Revision() {
			authoritative = row.thread
		}
	}
	if pendingIndex < 0 {
		return state, ErrCommandNotPending
	}
	pendingKey := next.rows[pendingIndex].key
	rows := make([]Row, 0, len(next.rows))
	for index, row := range next.rows {
		switch {
		case index == pendingIndex:
			row.pending = false
			row.thread = authoritative
			row.title = ""
			rows = append(rows, row)
		case !row.pending && row.thread.ID() == authoritative.ID():
			if next.activeKey == row.key {
				next.activeKey = pendingKey
			}
		default:
			rows = append(rows, row)
		}
	}
	next.rows = rows
	record.status = CommandCommitted
	record.threadID = authoritative.ID()
	next.commands[key] = record
	next.sortRows()
	next.restoreSelectionIfPresent()
	next.repairActiveKey()
	return next, nil
}

// RenameCommand owns a rename until the authoritative response commits.
type RenameCommand struct {
	Key              CommandKey
	ThreadID         domain.ThreadID
	Title            string
	ExpectedRevision uint64
}

// BeginRename marks a command busy but does not optimistically claim success.
func BeginRename(state State, command RenameCommand) (State, error) {
	title := strings.TrimSpace(command.Title)
	row, ok := state.rowByThreadID(command.ThreadID)
	if _, err := ParseCommandKey(string(command.Key)); err != nil || !ok || row.pending ||
		title == "" || len(title) > maximumTitleBytes || command.ExpectedRevision != row.thread.Revision() {
		return state, fmt.Errorf("%w: rename command", ErrInvalidModel)
	}
	fingerprint := commandFingerprint(commandRename, command.ThreadID.String(), title, fmt.Sprint(command.ExpectedRevision))
	return beginOwnedCommand(state, command.Key, commandRename, fingerprint)
}

// CommitRename replaces only the matching authoritative thread revision.
func CommitRename(state State, key CommandKey, thread Thread) (State, error) {
	return commitThreadMutation(state, key, commandRename, thread)
}

// ArchiveCommand owns an archive or restore request. User confirmation is
// mandatory before the command may enter the retained set.
type ArchiveCommand struct {
	Key              CommandKey
	ThreadID         domain.ThreadID
	Archived         bool
	Confirmed        bool
	ExpectedRevision uint64
}

// BeginArchive validates confirmation and retains the command without hiding
// the row before the server commits it.
func BeginArchive(state State, command ArchiveCommand) (State, error) {
	if !command.Confirmed {
		return state, ErrConfirmationNeeded
	}
	row, ok := state.rowByThreadID(command.ThreadID)
	if _, err := ParseCommandKey(string(command.Key)); err != nil || !ok || row.pending ||
		command.ExpectedRevision != row.thread.Revision() {
		return state, fmt.Errorf("%w: archive command", ErrInvalidModel)
	}
	fingerprint := commandFingerprint(commandArchive, command.ThreadID.String(), fmt.Sprint(command.Archived), fmt.Sprint(command.ExpectedRevision))
	return beginOwnedCommand(state, command.Key, commandArchive, fingerprint)
}

// CommitArchive applies the authoritative archive state. The default filter
// immediately excludes archived rows and clears an invalid selected route.
func CommitArchive(state State, key CommandKey, thread Thread) (State, error) {
	next, err := commitThreadMutation(state, key, commandArchive, thread)
	if err != nil {
		return state, err
	}
	if thread.Archived() && next.filter == FilterActive && next.selected == thread.ID() {
		next.selected = domain.ThreadID{}
	}
	next.repairActiveKey()
	return next, nil
}

// AbandonCommand releases a pending idempotency key and removes an uncommitted
// create placeholder. Committed keys remain retained.
func AbandonCommand(state State, key CommandKey) (State, error) {
	record, ok := state.commands[key]
	if !ok || record.status != CommandPending {
		return state, ErrCommandNotPending
	}
	next := state.clone()
	delete(next.commands, key)
	if record.operation == commandCreate {
		rows := next.rows[:0]
		for _, row := range next.rows {
			if !(row.pending && row.command == key) {
				rows = append(rows, row)
			}
		}
		next.rows = rows
		next.repairActiveKey()
	}
	return next, nil
}

func beginOwnedCommand(state State, key CommandKey, operation commandOperation, fingerprint string) (State, error) {
	if existing, ok := state.commands[key]; ok {
		if existing.operation != operation || existing.fingerprint != fingerprint {
			return state, ErrIdempotencyConflict
		}
		return state, nil
	}
	next := state.clone()
	next.commands[key] = commandRecord{operation: operation, fingerprint: fingerprint, status: CommandPending}
	return next, nil
}

func commitThreadMutation(state State, key CommandKey, operation commandOperation, thread Thread) (State, error) {
	record, ok := state.commands[key]
	if !ok || record.operation != operation {
		return state, ErrCommandNotPending
	}
	if record.status == CommandCommitted {
		if record.threadID == thread.ID() {
			return state, nil
		}
		return state, ErrIdempotencyConflict
	}
	row, ok := state.rowByThreadID(thread.ID())
	if !ok || row.pending || thread.RepositoryID() != state.repositoryID ||
		thread.WorkspaceID() != state.workspaceID || thread.Revision() < row.thread.Revision() {
		return state, fmt.Errorf("%w: committed mutation thread", ErrInvalidModel)
	}
	next := state.clone()
	next.mergeThread(thread)
	record.status = CommandCommitted
	record.threadID = thread.ID()
	next.commands[key] = record
	next.sortRows()
	return next, nil
}

func commandFingerprint(operation commandOperation, fields ...string) string {
	return string(operation) + "\x00" + strings.Join(fields, "\x00")
}

func (s *State) mergeThread(thread Thread) {
	for index, row := range s.rows {
		if !row.pending && row.thread.ID() == thread.ID() {
			if thread.Revision() >= row.thread.Revision() {
				s.rows[index].thread = thread
			}
			return
		}
	}
	s.nextOrder++
	s.rows = append(s.rows, Row{
		key: RowKey("thread:" + thread.ID().String()), thread: thread, order: s.nextOrder,
	})
}

func (s *State) sortRows() {
	sort.SliceStable(s.rows, func(left, right int) bool {
		a, b := s.rows[left], s.rows[right]
		if tierA, tierB := rowTier(a), rowTier(b); tierA != tierB {
			return tierA < tierB
		}
		if !a.UpdatedAt().Equal(b.UpdatedAt()) {
			return a.UpdatedAt().After(b.UpdatedAt())
		}
		return a.order < b.order
	})
}

func rowTier(row Row) int {
	if row.pending {
		return 0
	}
	switch row.thread.Attention() {
	case AttentionPendingApproval:
		return 1
	case AttentionRecovery:
		return 2
	case AttentionValidationFailure:
		return 3
	case AttentionUserInput:
		return 4
	}
	if row.thread.TaskState() == TaskStateRunning {
		return 5
	}
	return 6
}

func (s State) visibleRowByKey(key RowKey) (Row, bool) {
	for _, row := range s.rows {
		if row.key == key && rowVisible(row, s.filter) {
			return row, true
		}
	}
	return Row{}, false
}

func (s State) rowByThreadID(id domain.ThreadID) (Row, bool) {
	for _, row := range s.rows {
		if !row.pending && row.thread.ID() == id {
			return row, true
		}
	}
	return Row{}, false
}

func (s *State) restoreSelectionIfPresent() {
	if s.restore.IsZero() {
		return
	}
	for _, row := range s.rows {
		if !row.pending && row.thread.ID() == s.restore && !row.thread.Archived() {
			s.selected = s.restore
			s.activeKey = row.key
			return
		}
	}
}

func (s *State) repairActiveKey() {
	if s.activeKey != "" {
		if _, ok := s.visibleRowByKey(s.activeKey); ok {
			return
		}
	}
	rows := s.Rows()
	if len(rows) == 0 {
		s.activeKey = ""
		return
	}
	s.activeKey = rows[0].key
}
