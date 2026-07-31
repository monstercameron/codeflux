// Package threadrail owns the immutable client-side projection and pure
// interaction contracts for the repository thread rail.
package threadrail

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

const (
	DefaultPageLimit       uint32 = 50
	PaginationThreshold    int    = 8
	maximumTitleBytes      int    = 512
	maximumCommandKeyBytes int    = 255
)

var (
	ErrInvalidModel        = errors.New("thread rail: invalid model")
	ErrInvalidPage         = errors.New("thread rail: invalid page")
	ErrUnexpectedPage      = errors.New("thread rail: unexpected page")
	ErrThreadNotFound      = errors.New("thread rail: thread not found")
	ErrPendingThread       = errors.New("thread rail: pending thread has no route")
	ErrConfirmationNeeded  = errors.New("thread rail: archive confirmation is required")
	ErrIdempotencyConflict = errors.New("thread rail: idempotency key conflict")
	ErrCommandNotPending   = errors.New("thread rail: command is not pending")
)

// Cursor is an opaque server-issued pagination cursor.
type Cursor string

// CommandKey is the retained identity of one UI mutation until that mutation
// commits or is explicitly abandoned.
type CommandKey string

// ParseCommandKey validates an idempotency key without interpreting its value.
func ParseCommandKey(raw string) (CommandKey, error) {
	if raw == "" || strings.TrimSpace(raw) != raw || len(raw) > maximumCommandKeyBytes {
		return "", fmt.Errorf("%w: command key", ErrInvalidModel)
	}
	return CommandKey(raw), nil
}

// Attention identifies why a thread presently requires the user's attention.
type Attention string

const (
	AttentionNone              Attention = "none"
	AttentionPendingApproval   Attention = "pending-approval"
	AttentionRecovery          Attention = "recovery"
	AttentionValidationFailure Attention = "validation-failure"
	AttentionUserInput         Attention = "user-input"
)

func validAttention(value Attention) bool {
	switch value {
	case AttentionNone, AttentionPendingApproval, AttentionRecovery,
		AttentionValidationFailure, AttentionUserInput:
		return true
	default:
		return false
	}
}

// RailTaskState is the compact presentation state shown in one thread row.
// It is deliberately not an alternative definition of the authoritative
// domain task state.
type RailTaskState string

const (
	TaskStateNone     RailTaskState = "none"
	TaskStateDraft    RailTaskState = "draft"
	TaskStateRunning  RailTaskState = "running"
	TaskStatePaused   RailTaskState = "paused"
	TaskStateComplete RailTaskState = "complete"
	TaskStateFailed   RailTaskState = "failed"
	TaskStateStopped  RailTaskState = "cancelled"
)

func validTaskState(value RailTaskState) bool {
	switch value {
	case TaskStateNone, TaskStateDraft, TaskStateRunning, TaskStatePaused,
		TaskStateComplete, TaskStateFailed, TaskStateStopped:
		return true
	default:
		return false
	}
}

// ThreadInput contains one authoritative thread projection. NewThread copies
// and validates it before it can enter State.
type ThreadInput struct {
	ID           domain.ThreadID
	ProjectID    domain.ProjectID
	SessionID    domain.SessionID
	RepositoryID domain.RepositoryID
	WorkspaceID  domain.WorkspaceID
	TaskID       domain.TaskID
	Title        string
	TaskState    RailTaskState
	Attention    Attention
	Unread       uint32
	Archived     bool
	Revision     uint64
	UpdatedAt    time.Time
}

// Thread is an immutable row projection. Its fields remain private so callers
// cannot mutate rail state through a value returned by State.Rows.
type Thread struct {
	id           domain.ThreadID
	projectID    domain.ProjectID
	sessionID    domain.SessionID
	repositoryID domain.RepositoryID
	workspaceID  domain.WorkspaceID
	taskID       domain.TaskID
	title        string
	taskState    RailTaskState
	attention    Attention
	unread       uint32
	archived     bool
	revision     uint64
	updatedAt    time.Time
}

// NewThread validates and copies an authoritative thread projection.
func NewThread(input ThreadInput) (Thread, error) {
	title := strings.TrimSpace(input.Title)
	if input.ID.IsZero() || input.RepositoryID.IsZero() || input.WorkspaceID.IsZero() ||
		title == "" || len(title) > maximumTitleBytes ||
		input.UpdatedAt.IsZero() || !validTaskState(input.TaskState) ||
		!validAttention(input.Attention) {
		return Thread{}, fmt.Errorf("%w: thread", ErrInvalidModel)
	}
	return Thread{
		id: input.ID, projectID: input.ProjectID, sessionID: input.SessionID, repositoryID: input.RepositoryID, workspaceID: input.WorkspaceID,
		taskID: input.TaskID, title: title, taskState: input.TaskState,
		attention: input.Attention, unread: input.Unread, archived: input.Archived,
		revision: input.Revision, updatedAt: input.UpdatedAt.UTC(),
	}, nil
}

func (t Thread) ID() domain.ThreadID               { return t.id }
func (t Thread) ProjectID() domain.ProjectID       { return t.projectID }
func (t Thread) SessionID() domain.SessionID       { return t.sessionID }
func (t Thread) RepositoryID() domain.RepositoryID { return t.repositoryID }
func (t Thread) WorkspaceID() domain.WorkspaceID   { return t.workspaceID }
func (t Thread) TaskID() domain.TaskID             { return t.taskID }
func (t Thread) Title() string                     { return t.title }
func (t Thread) TaskState() RailTaskState          { return t.taskState }
func (t Thread) Attention() Attention              { return t.attention }
func (t Thread) Unread() uint32                    { return t.unread }
func (t Thread) Archived() bool                    { return t.archived }
func (t Thread) Revision() uint64                  { return t.revision }
func (t Thread) UpdatedAt() time.Time              { return t.updatedAt }

// RowKey is a stable virtual-list identity. A pending create row retains this
// key when its committed Thread replaces the local placeholder.
type RowKey string

// Row is an immutable virtual-list item.
type Row struct {
	key       RowKey
	thread    Thread
	pending   bool
	command   CommandKey
	title     string
	startedAt time.Time
	order     uint64
}

func (r Row) Key() RowKey            { return r.key }
func (r Row) Thread() Thread         { return r.thread }
func (r Row) Pending() bool          { return r.pending }
func (r Row) CommandKey() CommandKey { return r.command }
func (r Row) Title() string {
	if r.pending {
		return r.title
	}
	return r.thread.Title()
}
func (r Row) UpdatedAt() time.Time {
	if r.pending {
		return r.startedAt
	}
	return r.thread.UpdatedAt()
}
func (r Row) ThreadID() domain.ThreadID { return r.thread.ID() }
func (r Row) Archived() bool            { return !r.pending && r.thread.Archived() }
func (r Row) Attention() Attention {
	if r.pending {
		return AttentionNone
	}
	return r.thread.Attention()
}
func (r Row) TaskState() RailTaskState {
	if r.pending {
		return TaskStateDraft
	}
	return r.thread.TaskState()
}
func (r Row) Unread() uint32 {
	if r.pending {
		return 0
	}
	return r.thread.Unread()
}

// Filter selects the archived membership shown by Rows.
type Filter string

const (
	FilterActive   Filter = "active"
	FilterArchived Filter = "archived"
	FilterAll      Filter = "all"
)

func validFilter(filter Filter) bool {
	return filter == FilterActive || filter == FilterArchived || filter == FilterAll
}

// LoadState identifies the first-page data lifecycle.
type LoadState string

const (
	LoadNotRequested          LoadState = "not-requested"
	LoadLoading               LoadState = "loading"
	LoadReady                 LoadState = "ready"
	LoadRecoverableError      LoadState = "recoverable-error"
	LoadRepositoryUnavailable LoadState = "repository-unavailable"
	LoadDisconnected          LoadState = "disconnected"
)

// Presentation identifies the explicit rail presentation derived from state.
type Presentation string

const (
	PresentationNotRequested          Presentation = "not-requested"
	PresentationLoadingSkeleton       Presentation = "loading-skeleton"
	PresentationReady                 Presentation = "ready"
	PresentationEmptyRepository       Presentation = "empty-repository"
	PresentationNoMatches             Presentation = "no-matching-filter-results"
	PresentationPaginationError       Presentation = "pagination-error"
	PresentationRepositoryUnavailable Presentation = "repository-unavailable"
	PresentationArchivedView          Presentation = "archived-view"
	PresentationDisconnected          Presentation = "disconnected"
)

// PageError is safe, retryable list-fetch failure information.
type PageError struct {
	Code      string
	Message   string
	Retryable bool
}

// Page is one bounded page returned by PageClient.
type Page struct {
	RequestCursor Cursor
	Threads       []Thread
	NextCursor    Cursor
	HasMore       bool
}

// PageQuery is a bounded repository-workspace thread query.
type PageQuery struct {
	RepositoryID    domain.RepositoryID
	WorkspaceID     domain.WorkspaceID
	Cursor          Cursor
	Limit           uint32
	IncludeArchived bool
}

// State is an immutable thread-rail projection. Reducers clone all slices and
// maps before changing them, and accessors return copies.
type State struct {
	repositoryID domain.RepositoryID
	workspaceID  domain.WorkspaceID
	rows         []Row
	filter       Filter
	load         LoadState
	nextCursor   Cursor
	hasMore      bool
	loadingNext  bool
	requested    Cursor
	requestedSet bool
	applied      map[Cursor]struct{}
	pageError    PageError
	hasPageError bool
	selected     domain.ThreadID
	restore      domain.ThreadID
	activeKey    RowKey
	commands     map[CommandKey]commandRecord
	nextOrder    uint64
}

// NewState creates an empty repository-scoped rail.
func NewState(repositoryID domain.RepositoryID, workspaceID domain.WorkspaceID) (State, error) {
	if repositoryID.IsZero() || workspaceID.IsZero() {
		return State{}, fmt.Errorf("%w: repository and workspace are required", ErrInvalidModel)
	}
	return State{
		repositoryID: repositoryID, workspaceID: workspaceID, filter: FilterActive,
		load: LoadNotRequested, applied: make(map[Cursor]struct{}),
		commands: make(map[CommandKey]commandRecord),
	}, nil
}

func (s State) RepositoryID() domain.RepositoryID { return s.repositoryID }
func (s State) WorkspaceID() domain.WorkspaceID   { return s.workspaceID }
func (s State) Filter() Filter                    { return s.filter }
func (s State) LoadState() LoadState              { return s.load }
func (s State) NextCursor() Cursor                { return s.nextCursor }
func (s State) HasMore() bool                     { return s.hasMore }
func (s State) LoadingNextPage() bool             { return s.loadingNext }
func (s State) SelectedThreadID() domain.ThreadID { return s.selected }
func (s State) ActiveRowKey() RowKey              { return s.activeKey }
func (s State) PageError() (PageError, bool)      { return s.pageError, s.hasPageError }

// Rows returns a copy of the filtered, deterministically ordered list.
func (s State) Rows() []Row {
	rows := make([]Row, 0, len(s.rows))
	for _, row := range s.rows {
		if rowVisible(row, s.filter) {
			rows = append(rows, row)
		}
	}
	return rows
}

// AllRows returns a copy including rows hidden by the current filter.
func (s State) AllRows() []Row { return append([]Row(nil), s.rows...) }

// Presentation derives the explicit loading, empty, filtered, error, archived,
// or ready surface required by the rail contract.
func (s State) Presentation() Presentation {
	switch s.load {
	case LoadNotRequested:
		return PresentationNotRequested
	case LoadLoading:
		return PresentationLoadingSkeleton
	case LoadRecoverableError:
		return PresentationPaginationError
	case LoadRepositoryUnavailable:
		return PresentationRepositoryUnavailable
	case LoadDisconnected:
		return PresentationDisconnected
	}
	if s.hasPageError {
		return PresentationPaginationError
	}
	if len(s.rows) == 0 {
		return PresentationEmptyRepository
	}
	if len(s.Rows()) == 0 {
		return PresentationNoMatches
	}
	if s.filter == FilterArchived {
		return PresentationArchivedView
	}
	return PresentationReady
}

func (s State) clone() State {
	s.rows = append([]Row(nil), s.rows...)
	s.applied = cloneSet(s.applied)
	s.commands = cloneCommands(s.commands)
	return s
}

func cloneSet(input map[Cursor]struct{}) map[Cursor]struct{} {
	result := make(map[Cursor]struct{}, len(input))
	for key := range input {
		result[key] = struct{}{}
	}
	return result
}

func cloneCommands(input map[CommandKey]commandRecord) map[CommandKey]commandRecord {
	result := make(map[CommandKey]commandRecord, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func rowVisible(row Row, filter Filter) bool {
	if row.pending {
		return filter != FilterArchived
	}
	switch filter {
	case FilterActive:
		return !row.thread.Archived()
	case FilterArchived:
		return row.thread.Archived()
	default:
		return true
	}
}
