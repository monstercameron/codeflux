package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/routes"
	frontendstate "codeflux.dev/codeflux/web/frontend/state"
	"codeflux.dev/codeflux/web/frontend/threadrail"
)

const threadRailTransportFixtureUUID = "01890f3c-4a00-7abc-8def-0123456789ab"

type threadRailTransportFixture struct {
	repositoryID domain.RepositoryID
	workspaceID  domain.WorkspaceID
	threadID     domain.ThreadID
	sessionID    domain.SessionID
	thread       threadrail.Thread
}

func newThreadRailTransportFixture(t *testing.T) threadRailTransportFixture {
	t.Helper()
	repositoryID, err := domain.ParseRepositoryID("repo_" + threadRailTransportFixtureUUID)
	if err != nil {
		t.Fatal(err)
	}
	workspaceID, err := domain.ParseWorkspaceID("wsp_" + threadRailTransportFixtureUUID)
	if err != nil {
		t.Fatal(err)
	}
	threadID, err := domain.ParseThreadID("thr_" + threadRailTransportFixtureUUID)
	if err != nil {
		t.Fatal(err)
	}
	sessionID, err := domain.ParseSessionID("ses_" + threadRailTransportFixtureUUID)
	if err != nil {
		t.Fatal(err)
	}
	thread, err := threadrail.NewThread(threadrail.ThreadInput{
		ID: threadID, SessionID: sessionID, RepositoryID: repositoryID, WorkspaceID: workspaceID,
		Title: "Authoritative thread", TaskState: threadrail.TaskStateRunning,
		Attention: threadrail.AttentionNone, Revision: 1,
		UpdatedAt: time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	return threadRailTransportFixture{
		repositoryID: repositoryID, workspaceID: workspaceID,
		threadID: threadID, sessionID: sessionID, thread: thread,
	}
}

func TestAuthorizedThreadRailScopeUsesOnlyBootstrapIdentities(t *testing.T) {
	fixture := newThreadRailTransportFixture(t)
	envelope := bootstrapEnvelope{
		SelectedWorkspaceID: &codefluxv1.StableIdentity{
			Kind: codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_WORKSPACE, Value: fixture.workspaceID.String(),
		},
		RouteAccess: routeAccessEnvelope{AccessibleRepositories: []*codefluxv1.StableIdentity{{
			Kind: codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_REPOSITORY, Value: fixture.repositoryID.String(),
		}}},
	}
	snapshot := frontendstate.NewSnapshot(nil, nil, nil)
	snapshot.Workspace.RepositoryID = "repo_01890f3c-4a00-7abc-8def-ffffffffffff"
	scope, err := authorizedThreadRailScope(envelope, snapshot, routes.Route{})
	if err != nil {
		t.Fatal(err)
	}
	if scope.repositoryID != fixture.repositoryID || scope.workspaceID != fixture.workspaceID {
		t.Fatalf("authorized scope = %#v", scope)
	}

	route := routes.Route{Name: routes.ThreadWorkspace, RepositoryID: fixture.repositoryID, ThreadID: fixture.threadID}
	scope, err = authorizedThreadRailScope(envelope, snapshot, route)
	if err != nil || scope.repositoryID != fixture.repositoryID {
		t.Fatalf("authorized route scope = %#v, %v", scope, err)
	}
}

func TestAuthorizedThreadRailScopeRejectsMissingOrUntrustedIdentities(t *testing.T) {
	fixture := newThreadRailTransportFixture(t)
	authorizedRepository := &codefluxv1.StableIdentity{
		Kind:  codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_REPOSITORY,
		Value: fixture.repositoryID.String(),
	}
	tests := []struct {
		name     string
		envelope bootstrapEnvelope
		want     error
	}{
		{
			name: "missing workspace",
			envelope: bootstrapEnvelope{RouteAccess: routeAccessEnvelope{
				AccessibleRepositories: []*codefluxv1.StableIdentity{authorizedRepository},
			}},
			want: errThreadRailAuthorizedWorkspaceUnavailable,
		},
		{
			name: "wrong workspace kind",
			envelope: bootstrapEnvelope{
				SelectedWorkspaceID: authorizedRepository,
				RouteAccess: routeAccessEnvelope{AccessibleRepositories: []*codefluxv1.StableIdentity{
					authorizedRepository,
				}},
			},
			want: errThreadRailAuthorizedWorkspaceUnavailable,
		},
		{
			name: "missing repository",
			envelope: bootstrapEnvelope{SelectedWorkspaceID: &codefluxv1.StableIdentity{
				Kind:  codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_WORKSPACE,
				Value: fixture.workspaceID.String(),
			}},
			want: errThreadRailAuthorizedRepositoryUnavailable,
		},
		{
			name: "wrong repository kind",
			envelope: bootstrapEnvelope{
				SelectedWorkspaceID: &codefluxv1.StableIdentity{
					Kind:  codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_WORKSPACE,
					Value: fixture.workspaceID.String(),
				},
				RouteAccess: routeAccessEnvelope{AccessibleRepositories: []*codefluxv1.StableIdentity{{
					Kind:  codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_THREAD,
					Value: fixture.threadID.String(),
				}}},
			},
			want: errThreadRailAuthorizedRepositoryUnavailable,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			scope, err := authorizedThreadRailScope(
				test.envelope, frontendstate.NewSnapshot(nil, nil, nil), routes.Route{},
			)
			if !errors.Is(err, test.want) {
				t.Fatalf("scope error = %v, want %v", err, test.want)
			}
			if scope.valid() {
				t.Fatalf("rejected scope became valid: %#v", scope)
			}
		})
	}

	otherRepository, err := domain.ParseRepositoryID("repo_01890f3c-4a00-7abc-8def-0123456789ac")
	if err != nil {
		t.Fatal(err)
	}
	envelope := bootstrapEnvelope{
		SelectedWorkspaceID: &codefluxv1.StableIdentity{
			Kind:  codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_WORKSPACE,
			Value: fixture.workspaceID.String(),
		},
		RouteAccess: routeAccessEnvelope{AccessibleRepositories: []*codefluxv1.StableIdentity{
			authorizedRepository,
		}},
	}
	if scope, err := authorizedThreadRailScope(
		envelope,
		frontendstate.NewSnapshot(nil, nil, nil),
		routes.Route{Name: routes.ThreadWorkspace, RepositoryID: otherRepository, ThreadID: fixture.threadID},
	); !errors.Is(err, errThreadRailAuthorizedRepositoryUnavailable) || scope.valid() {
		t.Fatalf("unauthorized routed repository scope = %#v, %v", scope, err)
	}
}

type fakeThreadRailPageClient struct {
	page          threadrail.Page
	createResult  threadrail.Thread
	renameResult  threadrail.Thread
	archiveResult threadrail.Thread
	listQuery     threadrail.PageQuery
	listCalls     int
	createCall    threadrail.CreateCommand
	renameCall    threadrail.RenameCommand
	archiveCall   threadrail.ArchiveCommand
}

func (client *fakeThreadRailPageClient) ListThreads(_ context.Context, query threadrail.PageQuery) (threadrail.Page, error) {
	client.listCalls++
	client.listQuery = query
	page := client.page
	page.RequestCursor = query.Cursor
	return page, nil
}

func (client *fakeThreadRailPageClient) CreateThread(_ context.Context, command threadrail.CreateCommand) (threadrail.Thread, error) {
	client.createCall = command
	return client.createResult, nil
}

func (client *fakeThreadRailPageClient) RenameThread(_ context.Context, command threadrail.RenameCommand) (threadrail.Thread, error) {
	client.renameCall = command
	return client.renameResult, nil
}

func (client *fakeThreadRailPageClient) ArchiveThread(_ context.Context, command threadrail.ArchiveCommand) (threadrail.Thread, error) {
	client.archiveCall = command
	return client.archiveResult, nil
}

func TestThreadRailBridgeRunsAuthoritativeCreateRenameAndArchive(t *testing.T) {
	fixture := newThreadRailTransportFixture(t)
	createdID, err := domain.ParseThreadID("thr_01890f3c-4a00-7abc-8def-0123456789ac")
	if err != nil {
		t.Fatal(err)
	}
	createdSessionID, err := domain.ParseSessionID("ses_01890f3c-4a00-7abc-8def-0123456789ac")
	if err != nil {
		t.Fatal(err)
	}
	created, err := threadrail.NewThread(threadrail.ThreadInput{
		ID: createdID, SessionID: createdSessionID, RepositoryID: fixture.repositoryID, WorkspaceID: fixture.workspaceID,
		Title: "Untitled thread", TaskState: threadrail.TaskStateDraft,
		Attention: threadrail.AttentionNone, Revision: 1,
		UpdatedAt: time.Date(2026, 7, 31, 12, 1, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	renamed, err := threadrail.NewThread(threadrail.ThreadInput{
		ID: createdID, SessionID: createdSessionID, RepositoryID: fixture.repositoryID, WorkspaceID: fixture.workspaceID,
		Title: "Renamed thread", TaskState: threadrail.TaskStateDraft,
		Attention: threadrail.AttentionNone, Revision: 2,
		UpdatedAt: time.Date(2026, 7, 31, 12, 2, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	archived, err := threadrail.NewThread(threadrail.ThreadInput{
		ID: createdID, SessionID: createdSessionID, RepositoryID: fixture.repositoryID, WorkspaceID: fixture.workspaceID,
		Title: "Renamed thread", TaskState: threadrail.TaskStateDraft,
		Attention: threadrail.AttentionNone, Archived: true, Revision: 3,
		UpdatedAt: time.Date(2026, 7, 31, 12, 3, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeThreadRailPageClient{
		createResult: created, renameResult: renamed, archiveResult: archived,
	}
	state, err := threadrail.NewState(fixture.repositoryID, fixture.workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	createKey, _ := threadrail.ParseCommandKey("create-key")
	state, err = threadrail.CreateThread(context.Background(), state, client, threadrail.CreateCommand{
		Key: createKey, Title: "Untitled thread",
		StartedAt: time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.createCall.Key != createKey || len(state.Rows()) != 1 || state.Rows()[0].ThreadID() != createdID {
		t.Fatalf("authoritative create call/state = %#v/%#v", client.createCall, state.Rows())
	}
	if target, targetErr := threadRailSessionTarget(state, createdID); targetErr != nil || target != createdSessionID {
		t.Fatalf("create session target = %s, %v", target, targetErr)
	}

	renameKey, _ := threadrail.ParseCommandKey("rename-key")
	state, err = threadrail.RenameThread(context.Background(), state, client, threadrail.RenameCommand{
		Key: renameKey, ThreadID: createdID, Title: "Renamed thread", ExpectedRevision: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.renameCall.Key != renameKey || state.Rows()[0].Title() != "Renamed thread" {
		t.Fatalf("authoritative rename call/state = %#v/%#v", client.renameCall, state.Rows())
	}
	if target, targetErr := threadRailSessionTarget(state, createdID); targetErr != nil || target != createdSessionID {
		t.Fatalf("rename session target = %s, %v", target, targetErr)
	}

	archiveKey, _ := threadrail.ParseCommandKey("archive-key")
	state, err = threadrail.ArchiveThread(context.Background(), state, client, threadrail.ArchiveCommand{
		Key: archiveKey, ThreadID: createdID, Archived: true, Confirmed: true, ExpectedRevision: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.archiveCall.Key != archiveKey || len(state.Rows()) != 0 {
		t.Fatalf("authoritative archive call/state = %#v/%#v", client.archiveCall, state.Rows())
	}
	if all := state.AllRows(); len(all) != 1 || !all[0].Archived() {
		t.Fatalf("authoritative archived projection = %#v", all)
	}
	if target, targetErr := threadRailSessionTarget(state, createdID); targetErr != nil || target != createdSessionID {
		t.Fatalf("archive session target = %s, %v", target, targetErr)
	}
}

func TestThreadRailSessionTargetRejectsPendingMissingAndUntrustedTargets(t *testing.T) {
	fixture := newThreadRailTransportFixture(t)
	state, err := threadrail.NewState(fixture.repositoryID, fixture.workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if _, targetErr := threadRailSessionTarget(state, fixture.threadID); !errors.Is(targetErr, errThreadRailSessionUnavailable) {
		t.Fatalf("missing target error = %v", targetErr)
	}
	pendingKey, _ := threadrail.ParseCommandKey("pending-session-target")
	state, err = threadrail.BeginCreate(state, threadrail.CreateCommand{
		Key: pendingKey, Title: "Pending", StartedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, targetErr := threadRailSessionTarget(state, fixture.threadID); !errors.Is(targetErr, errThreadRailSessionUnavailable) {
		t.Fatalf("pending target error = %v", targetErr)
	}

	zeroSessionThread, err := threadrail.NewThread(threadrail.ThreadInput{
		ID: fixture.threadID, RepositoryID: fixture.repositoryID, WorkspaceID: fixture.workspaceID,
		Title: "Legacy projection", TaskState: threadrail.TaskStateNone,
		Attention: threadrail.AttentionNone, Revision: 1, UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	loaded, _ := threadrail.BeginFirstPage(state)
	state, err = threadrail.ApplyPage(loaded, threadrail.Page{Threads: []threadrail.Thread{zeroSessionThread}})
	if err != nil {
		t.Fatal(err)
	}
	if _, targetErr := threadRailSessionTarget(state, fixture.threadID); !errors.Is(targetErr, errThreadRailSessionUnavailable) {
		t.Fatalf("zero session target error = %v", targetErr)
	}
}

func TestThreadRailBridgeLeaseScopesLoadsAndAlwaysCloses(t *testing.T) {
	fixture := newThreadRailTransportFixture(t)
	client := &fakeThreadRailPageClient{page: threadrail.Page{Threads: []threadrail.Thread{fixture.thread}}}
	state, err := threadrail.NewState(fixture.repositoryID, fixture.workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	closed := false
	opener := func(
		_ context.Context,
		repositoryID domain.RepositoryID,
		workspaceID domain.WorkspaceID,
	) (threadRailClientLease, error) {
		if repositoryID != fixture.repositoryID || workspaceID != fixture.workspaceID {
			t.Fatalf("bridge opened with scope %s/%s", repositoryID, workspaceID)
		}
		return threadRailClientLease{
			client: client,
			close:  func() error { closed = true; return nil },
		}, nil
	}
	next, err := withThreadRailClient(
		context.Background(), opener,
		threadRailScope{repositoryID: fixture.repositoryID, workspaceID: fixture.workspaceID},
		func(pageClient threadrail.PageClient) (threadrail.State, error) {
			return threadrail.LoadFirstPage(context.Background(), state, pageClient)
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !closed {
		t.Fatal("thread rail bridge lease was not closed")
	}
	if client.listCalls != 1 || client.listQuery.RepositoryID != fixture.repositoryID ||
		client.listQuery.WorkspaceID != fixture.workspaceID {
		t.Fatalf("authoritative list query = %#v", client.listQuery)
	}
	rows := next.Rows()
	if len(rows) != 1 || rows[0].ThreadID() != fixture.threadID {
		t.Fatalf("authoritative rows = %#v", rows)
	}
}

func TestThreadRailBridgeRejectsInvalidLeaseAndOperation(t *testing.T) {
	fixture := newThreadRailTransportFixture(t)
	scope := threadRailScope{repositoryID: fixture.repositoryID, workspaceID: fixture.workspaceID}
	invalidLease := func(context.Context, domain.RepositoryID, domain.WorkspaceID) (threadRailClientLease, error) {
		return threadRailClientLease{}, nil
	}
	if _, err := withThreadRailClient(context.Background(), invalidLease, scope, func(threadrail.PageClient) (threadrail.State, error) {
		return threadrail.State{}, nil
	}); !errors.Is(err, errThreadRailBridgeUnavailable) {
		t.Fatalf("invalid lease error = %v", err)
	}
	if _, err := withThreadRailClient(context.Background(), invalidLease, threadRailScope{}, nil); !errors.Is(err, errThreadRailBridgeUnavailable) {
		t.Fatalf("invalid operation error = %v", err)
	}
}

func TestThreadRailCommandKeyUsesOpaqueOperationScopedEntropy(t *testing.T) {
	key, err := generateThreadRailCommandKey(
		bytes.NewReader(bytes.Repeat([]byte{0xab}, 16)), "rename-thread",
	)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(key), "rename-thread-"+strings.Repeat("ab", 16); got != want {
		t.Fatalf("command key = %q, want %q", got, want)
	}
	if _, err := generateThreadRailCommandKey(io.LimitReader(strings.NewReader("short"), 5), "create-thread"); err == nil {
		t.Fatal("short entropy source unexpectedly produced a command key")
	}
}

func TestRetryThreadRailPageReusesFailedCursor(t *testing.T) {
	fixture := newThreadRailTransportFixture(t)
	state, err := threadrail.NewState(fixture.repositoryID, fixture.workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	loading, _ := threadrail.BeginFirstPage(state)
	failed := threadrail.FailPage(loading, threadrail.PageError{
		Code: "list-failed", Message: "Threads could not be loaded.", Retryable: true,
	})
	client := &fakeThreadRailPageClient{page: threadrail.Page{Threads: []threadrail.Thread{fixture.thread}}}
	next, err := retryThreadRailPage(context.Background(), failed, client)
	if err != nil {
		t.Fatal(err)
	}
	if client.listCalls != 1 || client.listQuery.Cursor != "" {
		t.Fatalf("retry query = %#v", client.listQuery)
	}
	if rows := next.Rows(); len(rows) != 1 || rows[0].ThreadID() != fixture.threadID {
		t.Fatalf("retry rows = %#v", rows)
	}
}
