package threadrail

import (
	"errors"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/routes"
)

type railFixture struct {
	repository domain.RepositoryID
	workspace  domain.WorkspaceID
}

func newRailFixture(t *testing.T) railFixture {
	t.Helper()
	repository, err := domain.NewRepositoryID()
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := domain.NewWorkspaceID()
	if err != nil {
		t.Fatal(err)
	}
	return railFixture{repository: repository, workspace: workspace}
}

func (f railFixture) state(t *testing.T) State {
	t.Helper()
	state, err := NewState(f.repository, f.workspace)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func (f railFixture) thread(t *testing.T, title string, updated time.Time, taskState RailTaskState, attention Attention, revision uint64, archived bool) Thread {
	t.Helper()
	id, err := domain.NewThreadID()
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	sessionID, err := domain.NewSessionID()
	if err != nil {
		t.Fatal(err)
	}
	thread, err := NewThread(ThreadInput{
		ID: id, SessionID: sessionID, RepositoryID: f.repository, WorkspaceID: f.workspace, TaskID: taskID,
		Title: title, TaskState: taskState, Attention: attention, Unread: 2,
		Archived: archived, Revision: revision, UpdatedAt: updated,
	})
	if err != nil {
		t.Fatal(err)
	}
	return thread
}

func updateThread(t *testing.T, thread Thread, title string, archived bool) Thread {
	t.Helper()
	updated, err := NewThread(ThreadInput{
		ID: thread.ID(), SessionID: thread.SessionID(), RepositoryID: thread.RepositoryID(), WorkspaceID: thread.WorkspaceID(), TaskID: thread.TaskID(),
		Title: title, TaskState: thread.TaskState(), Attention: thread.Attention(), Unread: thread.Unread(),
		Archived: archived, Revision: thread.Revision() + 1, UpdatedAt: thread.UpdatedAt().Add(time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	return updated
}

func loadThreads(t *testing.T, state State, threads ...Thread) State {
	t.Helper()
	loading, _ := BeginFirstPage(state)
	loaded, err := ApplyPage(loading, Page{Threads: threads})
	if err != nil {
		t.Fatal(err)
	}
	return loaded
}

func TestThreadModelAndStateAreImmutableAndValidated(t *testing.T) {
	fixture := newRailFixture(t)
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.FixedZone("source", 3600))
	thread := fixture.thread(t, "  Important work  ", now, TaskStateRunning, AttentionUserInput, 1, false)
	if thread.Title() != "Important work" || thread.UpdatedAt().Location() != time.UTC {
		t.Fatalf("normalized thread = title %q time %v", thread.Title(), thread.UpdatedAt())
	}
	state := loadThreads(t, fixture.state(t), thread)
	rows := state.Rows()
	rows[0] = Row{}
	if got := state.Rows()[0].Title(); got != "Important work" {
		t.Fatalf("state mutated through Rows copy: %q", got)
	}
	invalid := ThreadInput{RepositoryID: fixture.repository, WorkspaceID: fixture.workspace, Title: "missing identity"}
	if _, err := NewThread(invalid); !errors.Is(err, ErrInvalidModel) {
		t.Fatalf("invalid thread error = %v", err)
	}
	if _, err := ParseCommandKey(" spaced "); !errors.Is(err, ErrInvalidModel) {
		t.Fatalf("invalid command key error = %v", err)
	}
}

func TestPagesSortAttentionBeforeRunningAndPreserveEqualTimestampOrder(t *testing.T) {
	fixture := newRailFixture(t)
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	inactiveA := fixture.thread(t, "inactive-a", now, TaskStateComplete, AttentionNone, 1, false)
	inactiveB := fixture.thread(t, "inactive-b", now, TaskStateFailed, AttentionNone, 1, false)
	running := fixture.thread(t, "running", now.Add(-time.Hour), TaskStateRunning, AttentionNone, 1, false)
	input := fixture.thread(t, "input", now.Add(-2*time.Hour), TaskStatePaused, AttentionUserInput, 1, false)
	validation := fixture.thread(t, "validation", now.Add(-3*time.Hour), TaskStatePaused, AttentionValidationFailure, 1, false)
	recovery := fixture.thread(t, "recovery", now.Add(-4*time.Hour), TaskStatePaused, AttentionRecovery, 1, false)
	approval := fixture.thread(t, "approval", now.Add(-5*time.Hour), TaskStatePaused, AttentionPendingApproval, 1, false)
	state := loadThreads(t, fixture.state(t), inactiveA, inactiveB, running, input, validation, recovery, approval)
	want := []string{"approval", "recovery", "validation", "input", "running", "inactive-a", "inactive-b"}
	for index, row := range state.Rows() {
		if row.Title() != want[index] {
			t.Fatalf("row %d = %q, want %q", index, row.Title(), want[index])
		}
	}
}

func TestPaginationIsBoundedDeduplicatedRetryableAndCursorChecked(t *testing.T) {
	fixture := newRailFixture(t)
	now := time.Now().UTC()
	first := fixture.thread(t, "first", now, TaskStateRunning, AttentionNone, 1, false)
	duplicateNewer := updateThread(t, first, "first renamed remotely", false)
	second := fixture.thread(t, "second", now.Add(-time.Minute), TaskStateNone, AttentionNone, 1, false)
	state, query := BeginFirstPage(fixture.state(t))
	if query.Cursor != "" || query.Limit != DefaultPageLimit || !query.IncludeArchived {
		t.Fatalf("first query = %+v", query)
	}
	state, err := ApplyPage(state, Page{Threads: []Thread{first}, NextCursor: "page-2", HasMore: true})
	if err != nil {
		t.Fatal(err)
	}
	if !ShouldLoadNextPage(0, 1) || ShouldLoadNextPage(-1, 1) {
		t.Fatal("near-end pagination trigger is incorrect")
	}
	state, nextQuery, ok := BeginNextPage(state)
	if !ok || nextQuery.Cursor != "page-2" {
		t.Fatalf("next query = %+v ok=%v", nextQuery, ok)
	}
	if _, _, duplicateRequest := BeginNextPage(state); duplicateRequest {
		t.Fatal("duplicate in-flight page request was issued")
	}
	state, err = ApplyPage(state, Page{RequestCursor: "page-2", Threads: []Thread{duplicateNewer, second}})
	if err != nil {
		t.Fatal(err)
	}
	state, err = ApplyPage(state, Page{RequestCursor: "page-2", Threads: []Thread{duplicateNewer, second}})
	if err != nil {
		t.Fatalf("duplicate response was not idempotent: %v", err)
	}
	if rows := state.AllRows(); len(rows) != 2 || rows[0].Thread().Revision() != 2 {
		t.Fatalf("deduplicated rows = %#v", rows)
	}
	badState, _ := BeginFirstPage(fixture.state(t))
	if _, err := ApplyPage(badState, Page{RequestCursor: "wrong"}); !errors.Is(err, ErrUnexpectedPage) {
		t.Fatalf("cursor mismatch error = %v", err)
	}
	failed := FailPage(badState, PageError{Code: "offline", Message: "Try again", Retryable: true})
	if failed.Presentation() != PresentationPaginationError {
		t.Fatalf("failure presentation = %s", failed.Presentation())
	}
	retrying, retryQuery, ok := BeginPageRetry(failed)
	if !ok || retryQuery.Cursor != "" || retrying.LoadState() != LoadLoading {
		t.Fatalf("retry = %+v %v %s", retryQuery, ok, retrying.LoadState())
	}
}

func TestExplicitEmptyArchivedAndNoMatchPresentations(t *testing.T) {
	fixture := newRailFixture(t)
	empty := loadThreads(t, fixture.state(t))
	if empty.Presentation() != PresentationEmptyRepository {
		t.Fatalf("empty = %s", empty.Presentation())
	}
	active := fixture.thread(t, "active", time.Now().UTC(), TaskStateRunning, AttentionNone, 1, false)
	state := loadThreads(t, fixture.state(t), active)
	archived, err := SetFilter(state, FilterArchived)
	if err != nil {
		t.Fatal(err)
	}
	if archived.Presentation() != PresentationNoMatches {
		t.Fatalf("filtered empty = %s", archived.Presentation())
	}
	archivedThread := fixture.thread(t, "archived", time.Now().UTC(), TaskStateComplete, AttentionNone, 1, true)
	state = loadThreads(t, fixture.state(t), active, archivedThread)
	state, err = SetFilter(state, FilterArchived)
	if err != nil {
		t.Fatal(err)
	}
	if state.Presentation() != PresentationArchivedView || len(state.Rows()) != 1 || state.Rows()[0].Title() != "archived" {
		t.Fatalf("archived view = %s %#v", state.Presentation(), state.Rows())
	}
}

func TestSelectionProducesRouteAndRestoresAcrossLaterPage(t *testing.T) {
	fixture := newRailFixture(t)
	thread := fixture.thread(t, "selected", time.Now().UTC(), TaskStateRunning, AttentionNone, 1, false)
	state := fixture.state(t)
	restoreRoute := routes.Route{Name: routes.ThreadWorkspace, RepositoryID: fixture.repository, ThreadID: thread.ID()}
	state, err := RestoreSelection(state, restoreRoute)
	if err != nil {
		t.Fatal(err)
	}
	loading, _ := BeginFirstPage(state)
	state, err = ApplyPage(loading, Page{Threads: []Thread{thread}})
	if err != nil {
		t.Fatal(err)
	}
	if state.SelectedThreadID() != thread.ID() || state.ActiveRowKey() == "" {
		t.Fatalf("selection not restored: %#v", state)
	}
	state, route, err := SelectThread(state, state.Rows()[0].Key())
	if err != nil {
		t.Fatal(err)
	}
	path, err := routes.Path(route)
	if err != nil {
		t.Fatal(err)
	}
	if path != "/workspace/"+fixture.repository.String()+"/thread/"+thread.ID().String() || state.SelectedThreadID() != thread.ID() {
		t.Fatalf("selection route = %s", path)
	}
}

func TestCreateReplacementRetainsRowKeySelectionAndFocus(t *testing.T) {
	fixture := newRailFixture(t)
	current := fixture.thread(t, "current", time.Now().UTC(), TaskStateRunning, AttentionNone, 1, false)
	state := loadThreads(t, fixture.state(t), current)
	state, _, err := SelectThread(state, state.Rows()[0].Key())
	if err != nil {
		t.Fatal(err)
	}
	activeBefore, selectedBefore := state.ActiveRowKey(), state.SelectedThreadID()
	command := CreateCommand{Key: "create-1", Title: "New work", StartedAt: time.Now().UTC().Add(time.Second)}
	state, err = BeginCreate(state, command)
	if err != nil {
		t.Fatal(err)
	}
	if repeated, err := BeginCreate(state, command); err != nil || len(repeated.AllRows()) != 2 {
		t.Fatalf("idempotent begin = rows %d err %v", len(repeated.AllRows()), err)
	}
	changed := command
	changed.Title = "Different work"
	if _, err := BeginCreate(state, changed); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("key conflict = %v", err)
	}
	var pending Row
	for _, row := range state.AllRows() {
		if row.Pending() {
			pending = row
		}
	}
	committed := fixture.thread(t, "New work", command.StartedAt.Add(time.Second), TaskStateDraft, AttentionNone, 1, false)
	state, err = CommitCreate(state, command.Key, committed)
	if err != nil {
		t.Fatal(err)
	}
	var replacement Row
	for _, row := range state.AllRows() {
		if row.ThreadID() == committed.ID() {
			replacement = row
		}
	}
	if replacement.Key() != pending.Key() || replacement.Pending() || state.ActiveRowKey() != activeBefore || state.SelectedThreadID() != selectedBefore {
		t.Fatalf("replacement jumped state: pending=%q committed=%q active=%q selected=%s", pending.Key(), replacement.Key(), state.ActiveRowKey(), state.SelectedThreadID())
	}
	if status, ok := state.OwnedCommand(command.Key); !ok || status != CommandCommitted {
		t.Fatalf("command status = %s %v", status, ok)
	}
}

func TestCreateCommitCoalescesEarlierAuthoritativeProjectionWithoutFocusJump(t *testing.T) {
	fixture := newRailFixture(t)
	command := CreateCommand{
		Key: "create-replay-race", Title: "New work",
		StartedAt: time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC),
	}
	state, err := BeginCreate(fixture.state(t), command)
	if err != nil {
		t.Fatal(err)
	}
	pendingKey := state.Rows()[0].Key()
	committed := fixture.thread(t, command.Title, command.StartedAt.Add(time.Second), TaskStateDraft, AttentionNone, 1, false)
	newerProjection := updateThread(t, committed, "New work renamed", false)
	loading, _ := BeginFirstPage(state)
	state, err = ApplyPage(loading, Page{Threads: []Thread{newerProjection}})
	if err != nil {
		t.Fatal(err)
	}
	var authoritativeKey RowKey
	for _, row := range state.Rows() {
		if !row.Pending() && row.ThreadID() == committed.ID() {
			authoritativeKey = row.Key()
		}
	}
	state, _, err = SelectThread(state, authoritativeKey)
	if err != nil {
		t.Fatal(err)
	}
	state, err = CommitCreate(state, command.Key, committed)
	if err != nil {
		t.Fatal(err)
	}
	rows := state.AllRows()
	if len(rows) != 1 {
		t.Fatalf("create replay race left %d rows: %#v", len(rows), rows)
	}
	replacement := rows[0]
	if replacement.Pending() || replacement.Key() != pendingKey ||
		replacement.Thread().Revision() != newerProjection.Revision() ||
		replacement.Title() != newerProjection.Title() {
		t.Fatalf("replacement = %#v, pending key %q, newer %#v", replacement, pendingKey, newerProjection)
	}
	if state.ActiveRowKey() != pendingKey || state.SelectedThreadID() != committed.ID() {
		t.Fatalf("focus or selection jumped: active=%q selected=%s", state.ActiveRowKey(), state.SelectedThreadID())
	}
}

func TestRenameAndArchiveWaitForCommittedAuthority(t *testing.T) {
	fixture := newRailFixture(t)
	thread := fixture.thread(t, "before", time.Now().UTC(), TaskStateComplete, AttentionNone, 4, false)
	state := loadThreads(t, fixture.state(t), thread)
	rename := RenameCommand{Key: "rename-1", ThreadID: thread.ID(), Title: "after", ExpectedRevision: 4}
	pending, err := BeginRename(state, rename)
	if err != nil {
		t.Fatal(err)
	}
	if pending.Rows()[0].Title() != "before" {
		t.Fatal("rename was optimistically presented as committed")
	}
	renamed := updateThread(t, thread, "after", false)
	state, err = CommitRename(pending, rename.Key, renamed)
	if err != nil || state.Rows()[0].Title() != "after" {
		t.Fatalf("rename commit = %v %#v", err, state.Rows())
	}
	archive := ArchiveCommand{Key: "archive-1", ThreadID: thread.ID(), Archived: true, ExpectedRevision: renamed.Revision()}
	if _, err := BeginArchive(state, archive); !errors.Is(err, ErrConfirmationNeeded) {
		t.Fatalf("missing confirmation = %v", err)
	}
	archive.Confirmed = true
	pending, err = BeginArchive(state, archive)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending.Rows()) != 1 {
		t.Fatal("archive hid the row before commit")
	}
	archivedThread := updateThread(t, renamed, "after", true)
	state, err = CommitArchive(pending, archive.Key, archivedThread)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Rows()) != 0 {
		t.Fatal("committed archive remained in default view")
	}
	state, err = SetFilter(state, FilterArchived)
	if err != nil || len(state.Rows()) != 1 {
		t.Fatalf("archived filter rows = %d err=%v", len(state.Rows()), err)
	}
}

func TestAbandonPendingCreateReleasesItsRowAndKey(t *testing.T) {
	fixture := newRailFixture(t)
	state, err := BeginCreate(fixture.state(t), CreateCommand{Key: "abandon-1", Title: "Temporary", StartedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	state, err = AbandonCommand(state, "abandon-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(state.AllRows()) != 0 {
		t.Fatal("abandoned pending row remains")
	}
	if _, ok := state.OwnedCommand("abandon-1"); ok {
		t.Fatal("abandoned key remains owned")
	}
}

func TestReducerRejectsInvalidPagesFiltersTargetsAndMutationOutcomes(t *testing.T) {
	fixture := newRailFixture(t)
	thread := fixture.thread(t, "valid", time.Now().UTC(), TaskStateRunning, AttentionNone, 1, false)
	loading, _ := BeginFirstPage(fixture.state(t))
	if _, err := ApplyPage(loading, Page{HasMore: true}); !errors.Is(err, ErrInvalidPage) {
		t.Fatalf("missing next cursor error = %v", err)
	}
	if _, err := ApplyPage(loading, Page{NextCursor: "impossible"}); !errors.Is(err, ErrInvalidPage) {
		t.Fatalf("terminal cursor error = %v", err)
	}
	other := newRailFixture(t)
	crossScope := other.thread(t, "other", time.Now().UTC(), TaskStateNone, AttentionNone, 1, false)
	if _, err := ApplyPage(loading, Page{Threads: []Thread{crossScope}}); !errors.Is(err, ErrInvalidPage) {
		t.Fatalf("cross-scope page error = %v", err)
	}
	state := loadThreads(t, fixture.state(t), thread)
	if _, err := SetFilter(state, "unknown"); !errors.Is(err, ErrInvalidModel) {
		t.Fatalf("invalid filter = %v", err)
	}
	if _, err := SetActiveRow(state, "missing"); !errors.Is(err, ErrThreadNotFound) {
		t.Fatalf("invalid active row = %v", err)
	}
	state, err := SetActiveRow(state, state.Rows()[0].Key())
	if err != nil || state.ActiveRowKey() != state.Rows()[0].Key() {
		t.Fatalf("active row = %q err %v", state.ActiveRowKey(), err)
	}
	pending, err := BeginCreate(state, CreateCommand{Key: "pending-select", Title: "Pending", StartedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	var pendingKey RowKey
	for _, row := range pending.Rows() {
		if row.Pending() {
			pendingKey = row.Key()
		}
	}
	if _, _, err := SelectThread(pending, pendingKey); !errors.Is(err, ErrPendingThread) {
		t.Fatalf("pending route error = %v", err)
	}
	wrongRoute := routes.Route{Name: routes.ThreadWorkspace, RepositoryID: other.repository, ThreadID: thread.ID()}
	if _, err := RestoreSelection(state, wrongRoute); !errors.Is(err, ErrInvalidModel) {
		t.Fatalf("wrong restore route = %v", err)
	}
	createPending, err := BeginCreate(fixture.state(t), CreateCommand{Key: "commit-scope", Title: "Pending", StartedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CommitCreate(createPending, "commit-scope", crossScope); !errors.Is(err, ErrInvalidModel) {
		t.Fatalf("cross-scope create commit = %v", err)
	}
}

func TestUnavailableDisconnectedAndNextPageFailurePreserveExplicitState(t *testing.T) {
	fixture := newRailFixture(t)
	thread := fixture.thread(t, "cached", time.Now().UTC(), TaskStateRunning, AttentionNone, 1, false)
	loading, _ := BeginFirstPage(fixture.state(t))
	state, err := ApplyPage(loading, Page{Threads: []Thread{thread}, NextCursor: "next", HasMore: true})
	if err != nil {
		t.Fatal(err)
	}
	next, _, ok := BeginNextPage(state)
	if !ok {
		t.Fatal("next page was not started")
	}
	failed := FailPage(next, PageError{Code: "temporary", Message: "Retry", Retryable: true})
	if failed.LoadState() != LoadReady || failed.Presentation() != PresentationPaginationError || len(failed.Rows()) != 1 {
		t.Fatalf("next-page failure = load %s presentation %s rows %d", failed.LoadState(), failed.Presentation(), len(failed.Rows()))
	}
	if got := RepositoryUnavailable(state).Presentation(); got != PresentationRepositoryUnavailable {
		t.Fatalf("repository presentation = %s", got)
	}
	disconnected := Disconnected(state)
	if disconnected.Presentation() != PresentationDisconnected || len(disconnected.Rows()) != 1 {
		t.Fatalf("disconnected = %s rows %d", disconnected.Presentation(), len(disconnected.Rows()))
	}
}
