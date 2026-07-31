package coordinator

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

func TestApplicationHostsDurableAuthenticatedThreadService(t *testing.T) {
	root := t.TempDir()
	application, err := StartApplication(t.Context(), ApplicationOptions{
		DatabasePath: filepath.Join(root, "codeflux.sqlite3"), BackupDirectory: filepath.Join(root, "backups"),
		ListenAddress: "127.0.0.1:0", TaskListenAddress: "127.0.0.1:0",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := application.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	})

	projectID, _ := domain.NewProjectID()
	repositoryID, _ := domain.NewRepositoryID()
	seedThreadID, _ := domain.NewThreadID()
	seedTaskID, _ := domain.NewTaskID()
	workspaceID, _ := domain.NewWorkspaceID()
	if _, err := application.repos.CreateProject(t.Context(), storage.CreateProject{ID: projectID, Name: "Thread RPC fixture"}); err != nil {
		t.Fatal(err)
	}
	if _, err := application.repos.CreateRepository(t.Context(), storage.CreateRepository{
		ID: repositoryID, ProjectID: projectID, CanonicalPath: filepath.Join(root, "repository"), GitIdentity: "thread-rpc-fixture",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := application.repos.CreateThread(t.Context(), storage.CreateThread{
		ID: seedThreadID, ProjectID: projectID, RepositoryID: repositoryID, Title: "Workspace seed",
	}); err != nil {
		t.Fatal(err)
	}
	seedTask, err := application.repos.CreateTask(t.Context(), storage.CreateTask{
		ID: seedTaskID, ThreadID: seedThreadID, RepositoryID: repositoryID,
		PolicyPreset: domain.PolicyPresetBalanced, ReasoningEffort: domain.ReasoningEffortStandard,
		RiskLevel: domain.RiskLevelRoutine, RequiredAssurance: domain.AssuranceLevelRuntimeOnly,
		SettingsRevision: 0, IdempotencyKey: "thread-rpc-seed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.repos.CreateWorktreeBinding(t.Context(), storage.CreateWorktreeBinding{
		WorkspaceID: workspaceID, TaskID: seedTask.ID, RepositoryID: repositoryID,
		BaseRevision: strings.Repeat("1", 40), HeadRevision: strings.Repeat("1", 40),
		BranchName: "codeflux/thread-rpc", WorktreePath: filepath.Join(root, "worktree"),
	}); err != nil {
		t.Fatal(err)
	}

	connection, err := grpc.NewClient(application.TaskControlAddress(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := connection.Close(); err != nil {
			t.Error(err)
		}
	})
	client := codefluxv1.NewThreadServiceClient(connection)
	ctx := metadata.AppendToOutgoingContext(t.Context(), transport.SessionMetadataKey, application.BrowserSessionSecret())
	workspaceIdentity, _ := transport.WorkspaceIDToProto(workspaceID)
	created, err := client.CreateThread(ctx, &codefluxv1.CreateThreadRequest{
		Control: &codefluxv1.MutationControl{IdempotencyKey: "create-rpc"}, WorkspaceId: workspaceIdentity, Title: "RPC thread",
	})
	if err != nil {
		t.Fatal(err)
	}
	retriedCreate, err := client.CreateThread(ctx, &codefluxv1.CreateThreadRequest{
		Control: &codefluxv1.MutationControl{IdempotencyKey: "create-rpc"}, WorkspaceId: workspaceIdentity, Title: "RPC thread",
	})
	if err != nil {
		t.Fatal(err)
	}
	if retriedCreate.GetThread().GetThreadId().GetValue() != created.GetThread().GetThreadId().GetValue() {
		t.Fatalf("create retry = %#v, want %#v", retriedCreate, created)
	}
	sessionID, err := transport.SessionIDFromProto(created.GetThread().GetSessionId())
	if err != nil {
		t.Fatal(err)
	}
	threadID, err := transport.ThreadIDFromProto(created.GetThread().GetThreadId())
	if err != nil {
		t.Fatal(err)
	}
	subscription, err := application.EventHub().Subscribe(ctx, events.SubscriptionQuery{
		SessionID: sessionID, ThreadID: &threadID, AfterSequence: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = subscription.Close() })
	listed, err := client.ListThreads(ctx, &codefluxv1.ListThreadsRequest{WorkspaceId: workspaceIdentity, Page: &codefluxv1.PageRequest{Limit: 10}})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.GetThreads()) != 1 || listed.GetThreads()[0].GetThreadId().GetValue() != created.GetThread().GetThreadId().GetValue() {
		t.Fatalf("listed threads = %#v", listed)
	}

	threadIdentity := created.GetThread().GetThreadId()
	sendRevision := created.GetThread().GetRevision()
	sent, err := client.SendMessage(ctx, &codefluxv1.SendMessageRequest{
		Control: &codefluxv1.MutationControl{IdempotencyKey: "send-rpc", ExpectedRevision: &sendRevision}, ThreadId: threadIdentity,
		Body: "Create a draft task", CreateDraftTask: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	retriedSend, err := client.SendMessage(ctx, &codefluxv1.SendMessageRequest{
		Control: &codefluxv1.MutationControl{IdempotencyKey: "send-rpc", ExpectedRevision: &sendRevision}, ThreadId: threadIdentity,
		Body: "Create a draft task", CreateDraftTask: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if sent.GetMessage().GetMessageId().GetValue() != retriedSend.GetMessage().GetMessageId().GetValue() ||
		sent.GetDraftTask().GetTaskId().GetValue() != retriedSend.GetDraftTask().GetTaskId().GetValue() {
		t.Fatalf("send retry = %#v, want %#v", retriedSend, sent)
	}
	page, err := client.GetThreadPage(ctx, &codefluxv1.GetThreadPageRequest{ThreadId: threadIdentity, Page: &codefluxv1.PageRequest{Limit: 10}})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.GetMessages()) != 1 || page.GetMessages()[0].GetBody().GetValue() != "Create a draft task" {
		t.Fatalf("thread page = %#v", page)
	}

	revision := page.GetThread().GetRevision()
	renamed, err := client.RenameThread(ctx, &codefluxv1.RenameThreadRequest{
		Control:  &codefluxv1.MutationControl{IdempotencyKey: "rename-rpc", ExpectedRevision: &revision},
		ThreadId: threadIdentity, Title: "Renamed over RPC",
	})
	if err != nil {
		t.Fatal(err)
	}
	revision = renamed.GetThread().GetRevision()
	archived, err := client.ArchiveThread(ctx, &codefluxv1.ArchiveThreadRequest{
		Control:  &codefluxv1.MutationControl{IdempotencyKey: "archive-rpc", ExpectedRevision: &revision},
		ThreadId: threadIdentity, Archived: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !archived.GetThread().GetArchived() || archived.GetThread().GetRevision() != revision+1 {
		t.Fatalf("archived thread = %#v", archived)
	}
	for index, kind := range []events.Kind{events.KindMessageFinal, events.KindGraphSnapshot, events.KindThreadRenamed, events.KindThreadArchived} {
		event, err := subscription.Next(ctx)
		if err != nil || event.Sequence != uint64(index+2) || event.Kind != kind {
			t.Fatalf("live thread event %d = %#v, %v", index, event, err)
		}
	}
	session, err := application.repos.GetThreadSession(ctx, threadID)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := application.repos.ReplaySessionEvents(ctx, storage.ReplaySessionEvents{SessionID: session.ID, Limit: 10})
	if err != nil || len(replayed) != 5 || replayed[0].Kind != events.KindThreadCreated {
		t.Fatalf("thread event replay = %#v, %v", replayed, err)
	}
}

func TestThreadApplicationUsesAuthenticatedScopeBoundCursors(t *testing.T) {
	t.Parallel()

	workspaceID, _ := domain.NewWorkspaceID()
	otherWorkspaceID, _ := domain.NewWorkspaceID()
	projectID, _ := domain.NewProjectID()
	repositoryID, _ := domain.NewRepositoryID()
	threadID, _ := domain.NewThreadID()
	updated := time.Unix(1_700_000_000, 123_000_000).UTC()
	repository := &threadRepositoryStub{
		scopes: map[domain.WorkspaceID]storage.WorkspaceScope{
			workspaceID:      {WorkspaceID: workspaceID, ProjectID: projectID, RepositoryID: repositoryID},
			otherWorkspaceID: {WorkspaceID: otherWorkspaceID, ProjectID: projectID, RepositoryID: repositoryID},
		},
		threadPages: []storage.ThreadPage{{
			Threads: []storage.Thread{{ID: threadID, ProjectID: projectID, RepositoryID: repositoryID, WorkspaceID: workspaceID, Title: "First", UpdatedAt: updated}},
			Next:    &storage.ThreadCursor{UpdatedAt: updated, ID: threadID},
		}, {Threads: []storage.Thread{}}},
	}
	application, err := NewThreadApplication(repository, strings.Repeat("s", 32))
	if err != nil {
		t.Fatal(err)
	}
	application.random = strings.NewReader(strings.Repeat("n", 128))

	first, err := application.ListThreads(t.Context(), transport.ListThreadsQuery{WorkspaceID: workspaceID, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !first.HasMore || first.NextCursor == "" || strings.Contains(first.NextCursor, threadID.String()) {
		t.Fatalf("first page cursor = %q, has-more=%t", first.NextCursor, first.HasMore)
	}
	second, err := application.ListThreads(t.Context(), transport.ListThreadsQuery{WorkspaceID: workspaceID, Cursor: first.NextCursor, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if second.HasMore || repository.lastList.Before == nil || repository.lastList.Before.ID != threadID || !repository.lastList.Before.UpdatedAt.Equal(updated) {
		t.Fatalf("decoded continuation = %#v, second page = %#v", repository.lastList.Before, second)
	}

	tampered := "A" + first.NextCursor[1:]
	if tampered == first.NextCursor {
		tampered = "B" + first.NextCursor[1:]
	}
	for name, query := range map[string]transport.ListThreadsQuery{
		"workspace": {WorkspaceID: otherWorkspaceID, Cursor: first.NextCursor, Limit: 1},
		"archive":   {WorkspaceID: workspaceID, Cursor: first.NextCursor, Limit: 1, IncludeArchived: true},
		"tamper":    {WorkspaceID: workspaceID, Cursor: tampered, Limit: 1},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := application.ListThreads(t.Context(), query); !errors.Is(err, transport.ErrThreadInvalidCursor) {
				t.Fatalf("cursor error = %v", err)
			}
		})
	}
	if _, err := application.ListThreads(t.Context(), transport.ListThreadsQuery{WorkspaceID: workspaceID, Limit: 101}); !errors.Is(err, transport.ErrThreadInvalidCursor) {
		t.Fatalf("oversized page error = %v", err)
	}
}

func TestThreadApplicationMapsRepositoryErrors(t *testing.T) {
	t.Parallel()

	for name, fixture := range map[string]struct {
		storage error
		want    error
	}{
		"not-found": {storage: storage.ErrNotFound, want: transport.ErrThreadNotFound},
		"stale":     {storage: storage.ErrStaleRevision, want: transport.ErrThreadStaleRevision},
		"conflict":  {storage: storage.ErrConflict, want: transport.ErrThreadConflict},
		"denied":    {storage: storage.ErrConstraint, want: transport.ErrThreadDenied},
	} {
		t.Run(name, func(t *testing.T) {
			if mapped := mapThreadRepositoryError(fixture.storage); !errors.Is(mapped, fixture.want) {
				t.Fatalf("mapped error = %v, want %v", mapped, fixture.want)
			}
		})
	}
}

type threadRepositoryStub struct {
	scopes      map[domain.WorkspaceID]storage.WorkspaceScope
	threadPages []storage.ThreadPage
	lastList    storage.ListThreads
}

func (stub *threadRepositoryStub) GetWorkspaceScope(_ context.Context, id domain.WorkspaceID) (storage.WorkspaceScope, error) {
	scope, ok := stub.scopes[id]
	if !ok {
		return storage.WorkspaceScope{}, storage.ErrNotFound
	}
	return scope, nil
}

func (stub *threadRepositoryStub) CreateThread(_ context.Context, input storage.CreateThread) (storage.Thread, error) {
	return storage.Thread{ID: input.ID, ProjectID: input.ProjectID, RepositoryID: input.RepositoryID, WorkspaceID: input.WorkspaceID, Title: input.Title}, nil
}

func (stub *threadRepositoryStub) CreateThreadCommit(_ context.Context, input storage.CreateThread) (storage.ThreadCommit, error) {
	return storage.ThreadCommit{Thread: storage.Thread{ID: input.ID, ProjectID: input.ProjectID,
		RepositoryID: input.RepositoryID, WorkspaceID: input.WorkspaceID, SessionID: input.SessionID, Title: input.Title}}, nil
}

func (stub *threadRepositoryStub) ListThreads(_ context.Context, input storage.ListThreads) (storage.ThreadPage, error) {
	stub.lastList = input
	if len(stub.threadPages) == 0 {
		return storage.ThreadPage{}, nil
	}
	page := stub.threadPages[0]
	stub.threadPages = stub.threadPages[1:]
	return page, nil
}

func (stub *threadRepositoryStub) GetThread(_ context.Context, id domain.ThreadID) (storage.Thread, error) {
	return storage.Thread{ID: id}, nil
}

func (stub *threadRepositoryStub) ListMessages(_ context.Context, _ storage.ListMessages) (storage.MessagePage, error) {
	return storage.MessagePage{}, nil
}

func (stub *threadRepositoryStub) AppendMessage(_ context.Context, input storage.AppendMessage) (storage.Message, error) {
	return storage.Message{ID: input.ID, ThreadID: input.ThreadID, Role: input.Role, BodyRedacted: input.BodyRedacted, AttachmentIDs: input.AttachmentIDs}, nil
}

func (stub *threadRepositoryStub) CreateTask(_ context.Context, input storage.CreateTask) (storage.Task, error) {
	return storage.Task{ID: input.ID, ThreadID: input.ThreadID, RepositoryID: input.RepositoryID, State: domain.TaskStateDraft}, nil
}

func (stub *threadRepositoryStub) AppendMessageAndDraftTask(_ context.Context, input storage.AppendMessageAndDraftTask) (storage.AppendedMessageAndDraftTask, error) {
	result := storage.AppendedMessageAndDraftTask{Message: storage.Message{
		ID: input.Message.ID, ThreadID: input.Message.ThreadID, Role: input.Message.Role,
		BodyRedacted: input.Message.BodyRedacted, AttachmentIDs: input.Message.AttachmentIDs,
	}}
	if input.DraftTask != nil {
		result.DraftTask = &storage.Task{ID: input.DraftTask.ID, ThreadID: input.DraftTask.ThreadID,
			RepositoryID: input.DraftTask.RepositoryID, State: domain.TaskStateDraft}
	}
	return result, nil
}

func (stub *threadRepositoryStub) RenameThread(_ context.Context, input storage.RenameThread) (storage.Thread, error) {
	return storage.Thread{ID: input.ThreadID, Title: input.Title, Revision: input.ExpectedRevision + 1}, nil
}

func (stub *threadRepositoryStub) RenameThreadCommit(_ context.Context, input storage.RenameThread) (storage.ThreadCommit, error) {
	return storage.ThreadCommit{Thread: storage.Thread{ID: input.ThreadID, Title: input.Title, Revision: input.ExpectedRevision + 1}}, nil
}

func (stub *threadRepositoryStub) ArchiveThread(_ context.Context, input storage.ArchiveThread) (storage.Thread, error) {
	return storage.Thread{ID: input.ThreadID, Archived: input.Archived, Revision: input.ExpectedRevision + 1}, nil
}

func (stub *threadRepositoryStub) ArchiveThreadCommit(_ context.Context, input storage.ArchiveThread) (storage.ThreadCommit, error) {
	return storage.ThreadCommit{Thread: storage.Thread{ID: input.ThreadID, Archived: input.Archived, Revision: input.ExpectedRevision + 1}}, nil
}
