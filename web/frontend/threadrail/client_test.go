package threadrail

import (
	"context"
	"errors"
	"testing"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type fakePageClient struct {
	queries    []PageQuery
	page       Page
	listErr    error
	created    Thread
	renamed    Thread
	archived   Thread
	createErr  error
	renameErr  error
	archiveErr error
}

func (f *fakePageClient) ListThreads(_ context.Context, query PageQuery) (Page, error) {
	f.queries = append(f.queries, query)
	page := f.page
	page.RequestCursor = query.Cursor
	return page, f.listErr
}
func (f *fakePageClient) CreateThread(context.Context, CreateCommand) (Thread, error) {
	return f.created, f.createErr
}
func (f *fakePageClient) RenameThread(context.Context, RenameCommand) (Thread, error) {
	return f.renamed, f.renameErr
}
func (f *fakePageClient) ArchiveThread(context.Context, ArchiveCommand) (Thread, error) {
	return f.archived, f.archiveErr
}

func TestPageClientPortLoadsFirstAndNextPagesAndRetainsRetryState(t *testing.T) {
	fixture := newRailFixture(t)
	first := fixture.thread(t, "first", time.Now().UTC(), TaskStateRunning, AttentionNone, 1, false)
	port := &fakePageClient{page: Page{Threads: []Thread{first}, NextCursor: "next", HasMore: true}}
	state, err := LoadFirstPage(context.Background(), fixture.state(t), port)
	if err != nil {
		t.Fatal(err)
	}
	if len(port.queries) != 1 || port.queries[0].RepositoryID != fixture.repository || len(state.Rows()) != 1 {
		t.Fatalf("first load = queries %#v rows %d", port.queries, len(state.Rows()))
	}
	second := fixture.thread(t, "second", time.Now().UTC().Add(-time.Minute), TaskStateNone, AttentionNone, 1, false)
	port.page = Page{Threads: []Thread{second}}
	state, err = LoadNextPage(context.Background(), state, port)
	if err != nil {
		t.Fatal(err)
	}
	if len(port.queries) != 2 || port.queries[1].Cursor != "next" || len(state.Rows()) != 2 {
		t.Fatalf("next load = queries %#v rows %d", port.queries, len(state.Rows()))
	}
	port.listErr = errors.New("synthetic unavailable")
	failed, err := LoadFirstPage(context.Background(), fixture.state(t), port)
	if err == nil || failed.Presentation() != PresentationPaginationError {
		t.Fatalf("failed load = presentation %s err %v", failed.Presentation(), err)
	}
}

func TestMutationClientPortRetainsKeysAcrossFailureAndCommitsAuthoritativeRows(t *testing.T) {
	fixture := newRailFixture(t)
	port := &fakePageClient{}
	create := CreateCommand{Key: "create-port", Title: "Created", StartedAt: time.Now().UTC()}
	port.createErr = errors.New("ambiguous transport failure")
	state, err := CreateThread(context.Background(), fixture.state(t), port, create)
	if err == nil || len(state.Rows()) != 1 || !state.Rows()[0].Pending() {
		t.Fatalf("ambiguous create = rows %#v err %v", state.Rows(), err)
	}
	port.createErr = nil
	port.created = fixture.thread(t, "Created", create.StartedAt.Add(time.Second), TaskStateDraft, AttentionNone, 1, false)
	state, err = CreateThread(context.Background(), state, port, create)
	if err != nil || state.Rows()[0].Pending() {
		t.Fatalf("create retry commit = %#v err %v", state.Rows(), err)
	}
	thread := port.created
	rename := RenameCommand{Key: "rename-port", ThreadID: thread.ID(), Title: "Renamed", ExpectedRevision: thread.Revision()}
	port.renamed = updateThread(t, thread, "Renamed", false)
	state, err = RenameThread(context.Background(), state, port, rename)
	if err != nil || state.Rows()[0].Title() != "Renamed" {
		t.Fatalf("rename port = %#v err %v", state.Rows(), err)
	}
	archive := ArchiveCommand{Key: "archive-port", ThreadID: thread.ID(), Archived: true, Confirmed: true, ExpectedRevision: port.renamed.Revision()}
	port.archived = updateThread(t, port.renamed, "Renamed", true)
	state, err = ArchiveThread(context.Background(), state, port, archive)
	if err != nil || len(state.Rows()) != 0 {
		t.Fatalf("archive port = %#v err %v", state.Rows(), err)
	}
}

type fakeGeneratedThreadClient struct {
	listRequest    *codefluxv1.ListThreadsRequest
	createRequest  *codefluxv1.CreateThreadRequest
	renameRequest  *codefluxv1.RenameThreadRequest
	archiveRequest *codefluxv1.ArchiveThreadRequest
	view           *codefluxv1.ThreadView
}

func (f *fakeGeneratedThreadClient) CreateThread(_ context.Context, in *codefluxv1.CreateThreadRequest, _ ...grpc.CallOption) (*codefluxv1.CreateThreadResponse, error) {
	f.createRequest = in
	return &codefluxv1.CreateThreadResponse{Thread: f.view}, nil
}
func (f *fakeGeneratedThreadClient) ListThreads(_ context.Context, in *codefluxv1.ListThreadsRequest, _ ...grpc.CallOption) (*codefluxv1.ListThreadsResponse, error) {
	f.listRequest = in
	return &codefluxv1.ListThreadsResponse{Threads: []*codefluxv1.ThreadView{f.view}, Page: &codefluxv1.PageInfo{NextCursor: "opaque", HasMore: true}}, nil
}
func (f *fakeGeneratedThreadClient) GetThreadPage(context.Context, *codefluxv1.GetThreadPageRequest, ...grpc.CallOption) (*codefluxv1.GetThreadPageResponse, error) {
	return nil, errors.New("not used")
}
func (f *fakeGeneratedThreadClient) SendMessage(context.Context, *codefluxv1.SendMessageRequest, ...grpc.CallOption) (*codefluxv1.SendMessageResponse, error) {
	return nil, errors.New("not used")
}
func (f *fakeGeneratedThreadClient) RenameThread(_ context.Context, in *codefluxv1.RenameThreadRequest, _ ...grpc.CallOption) (*codefluxv1.RenameThreadResponse, error) {
	f.renameRequest = in
	return &codefluxv1.RenameThreadResponse{Thread: f.view}, nil
}
func (f *fakeGeneratedThreadClient) ArchiveThread(_ context.Context, in *codefluxv1.ArchiveThreadRequest, _ ...grpc.CallOption) (*codefluxv1.ArchiveThreadResponse, error) {
	f.archiveRequest = in
	return &codefluxv1.ArchiveThreadResponse{Thread: f.view}, nil
}

func TestGRPCClientMapsBoundedQueriesMutationsAndTypedIdentities(t *testing.T) {
	fixture := newRailFixture(t)
	threadID, err := fixture.thread(t, "source", time.Now().UTC(), TaskStateNone, AttentionNone, 1, false).ID(), error(nil)
	if err != nil {
		t.Fatal(err)
	}
	sessionID, err := domain.NewSessionID()
	if err != nil {
		t.Fatal(err)
	}
	view := &codefluxv1.ThreadView{
		ThreadId:    stableIdentity(codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_THREAD, threadID.String()),
		SessionId:   stableIdentity(codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_SESSION, sessionID.String()),
		WorkspaceId: stableIdentity(codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_WORKSPACE, fixture.workspace.String()),
		Title:       &codefluxv1.RedactedText{Value: "Transport thread"}, Revision: 7,
		UpdatedAt: timestamppb.New(time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)),
	}
	generated := &fakeGeneratedThreadClient{view: view}
	client, err := NewGRPCClient(fixture.repository, fixture.workspace, generated)
	if err != nil {
		t.Fatal(err)
	}
	page, err := client.ListThreads(context.Background(), PageQuery{
		RepositoryID: fixture.repository, WorkspaceID: fixture.workspace,
		Cursor: "cursor", Limit: 25, IncludeArchived: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Threads) != 1 || page.Threads[0].ID() != threadID || page.Threads[0].SessionID() != sessionID || page.NextCursor != "opaque" ||
		generated.listRequest.GetPage().GetCursor() != "cursor" || !generated.listRequest.GetIncludeArchived() {
		t.Fatalf("mapped page = %#v request=%#v", page, generated.listRequest)
	}
	create := CreateCommand{Key: "create-grpc", Title: "Transport thread", StartedAt: time.Now().UTC()}
	if _, err := client.CreateThread(context.Background(), create); err != nil {
		t.Fatal(err)
	}
	if generated.createRequest.GetControl().GetIdempotencyKey() != "create-grpc" || generated.createRequest.GetWorkspaceId().GetValue() != fixture.workspace.String() {
		t.Fatalf("create request = %#v", generated.createRequest)
	}
	rename := RenameCommand{Key: "rename-grpc", ThreadID: threadID, Title: "Renamed", ExpectedRevision: 7}
	if _, err := client.RenameThread(context.Background(), rename); err != nil {
		t.Fatal(err)
	}
	if generated.renameRequest.GetControl().GetExpectedRevision() != 7 || generated.renameRequest.GetThreadId().GetKind() != codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_THREAD {
		t.Fatalf("rename request = %#v", generated.renameRequest)
	}
	archive := ArchiveCommand{Key: "archive-grpc", ThreadID: threadID, Archived: true, Confirmed: true, ExpectedRevision: 7}
	if _, err := client.ArchiveThread(context.Background(), archive); err != nil {
		t.Fatal(err)
	}
	if !generated.archiveRequest.GetArchived() || generated.archiveRequest.GetControl().GetIdempotencyKey() != "archive-grpc" {
		t.Fatalf("archive request = %#v", generated.archiveRequest)
	}
}

func TestGRPCClientRejectsMalformedOrCrossScopeResponses(t *testing.T) {
	fixture := newRailFixture(t)
	generated := &fakeGeneratedThreadClient{view: &codefluxv1.ThreadView{}}
	client, err := NewGRPCClient(fixture.repository, fixture.workspace, generated)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ListThreads(context.Background(), PageQuery{RepositoryID: fixture.repository, WorkspaceID: fixture.workspace, Limit: 50})
	if !errors.Is(err, ErrInvalidPage) {
		t.Fatalf("malformed response error = %v", err)
	}
	other := newRailFixture(t)
	_, err = client.ListThreads(context.Background(), PageQuery{RepositoryID: other.repository, WorkspaceID: fixture.workspace, Limit: 50})
	if !errors.Is(err, ErrInvalidModel) {
		t.Fatalf("cross-scope query error = %v", err)
	}
}

func TestGRPCClientRejectsMissingMalformedOrMistypedSessionIdentity(t *testing.T) {
	fixture := newRailFixture(t)
	threadID, err := domain.NewThreadID()
	if err != nil {
		t.Fatal(err)
	}
	valid := func() *codefluxv1.ThreadView {
		return &codefluxv1.ThreadView{
			ThreadId:    stableIdentity(codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_THREAD, threadID.String()),
			SessionId:   stableIdentity(codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_SESSION, "ses_01890f3c-4a00-7abc-8def-0123456789ab"),
			WorkspaceId: stableIdentity(codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_WORKSPACE, fixture.workspace.String()),
			Title:       &codefluxv1.RedactedText{Value: "Transport thread"},
			Revision:    1,
			UpdatedAt:   timestamppb.Now(),
		}
	}
	tests := []struct {
		name   string
		mutate func(*codefluxv1.ThreadView)
	}{
		{name: "missing", mutate: func(view *codefluxv1.ThreadView) { view.SessionId = nil }},
		{name: "malformed", mutate: func(view *codefluxv1.ThreadView) { view.SessionId.Value = "session-by-name" }},
		{name: "mistyped", mutate: func(view *codefluxv1.ThreadView) {
			view.SessionId.Kind = codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_THREAD
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			view := valid()
			test.mutate(view)
			client, newErr := NewGRPCClient(fixture.repository, fixture.workspace, &fakeGeneratedThreadClient{view: view})
			if newErr != nil {
				t.Fatal(newErr)
			}
			_, listErr := client.ListThreads(context.Background(), PageQuery{
				RepositoryID: fixture.repository, WorkspaceID: fixture.workspace, Limit: 50,
			})
			if !errors.Is(listErr, ErrInvalidPage) {
				t.Fatalf("session identity error = %v", listErr)
			}
		})
	}
}
