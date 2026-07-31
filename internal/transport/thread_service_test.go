package transport

import (
	"context"
	"errors"
	"testing"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
)

func TestThreadServiceMapsIdentityBackedLifecycle(t *testing.T) {
	t.Parallel()

	workspaceID, _ := domain.NewWorkspaceID()
	sessionID, _ := domain.NewSessionID()
	threadID, _ := domain.NewThreadID()
	messageID, _ := domain.NewMessageID()
	artifactID, _ := domain.NewArtifactID()
	taskID, _ := domain.NewTaskID()
	now := time.Unix(1_700_000_000, 123_000_000).UTC()
	application := &threadApplicationStub{
		createResult: ThreadView{ThreadID: threadID, WorkspaceID: workspaceID, SessionID: sessionID, Title: "Created", Revision: 1, UpdatedAt: now},
		listResult:   ThreadPage{Threads: []ThreadView{{ThreadID: threadID, WorkspaceID: workspaceID, SessionID: sessionID, Title: "Listed", Revision: 2, UpdatedAt: now}}, NextCursor: "opaque", HasMore: true},
		pageResult: MessagePage{
			Thread:   ThreadView{ThreadID: threadID, WorkspaceID: workspaceID, SessionID: sessionID, Title: "Page", Revision: 3, UpdatedAt: now},
			Messages: []MessageView{{MessageID: messageID, ThreadID: threadID, Role: "user", BodyRedacted: "hello", AttachmentIDs: []domain.ArtifactID{artifactID}, Revision: 1, CreatedAt: now}},
		},
		sendResult: SendMessageResult{
			Message:   MessageView{MessageID: messageID, ThreadID: threadID, Role: "user", BodyRedacted: "sent", AttachmentIDs: []domain.ArtifactID{artifactID}, Revision: 1, CreatedAt: now},
			DraftTask: &DraftTaskView{TaskID: taskID, ThreadID: threadID, State: domain.TaskStateDraft, UpdatedAt: now},
		},
		renameResult:  ThreadView{ThreadID: threadID, WorkspaceID: workspaceID, SessionID: sessionID, Title: "Renamed", Revision: 4, UpdatedAt: now},
		archiveResult: ThreadView{ThreadID: threadID, WorkspaceID: workspaceID, SessionID: sessionID, Title: "Renamed", Archived: true, Revision: 5, UpdatedAt: now},
	}
	service, err := NewThreadService(application)
	if err != nil {
		t.Fatal(err)
	}
	workspaceIdentity, _ := WorkspaceIDToProto(workspaceID)
	threadIdentity, _ := ThreadIDToProto(threadID)
	artifactIdentity, _ := ArtifactIDToProto(artifactID)

	created, err := service.CreateThread(t.Context(), &codefluxv1.CreateThreadRequest{
		Control:     &codefluxv1.MutationControl{IdempotencyKey: "create-one"},
		WorkspaceId: workspaceIdentity, Title: "  Created  ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if application.create.WorkspaceID != workspaceID || application.create.Title != "Created" || created.GetThread().GetThreadId().GetValue() != threadID.String() || created.GetThread().GetSessionId().GetValue() != sessionID.String() {
		t.Fatalf("create command/response = %#v / %#v", application.create, created)
	}

	listed, err := service.ListThreads(t.Context(), &codefluxv1.ListThreadsRequest{
		WorkspaceId: workspaceIdentity, Page: &codefluxv1.PageRequest{Cursor: "cursor", Limit: 7}, IncludeArchived: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if application.list.Cursor != "cursor" || application.list.Limit != 7 || !application.list.IncludeArchived || listed.GetPage().GetNextCursor() != "opaque" || !listed.GetPage().GetHasMore() {
		t.Fatalf("list command/response = %#v / %#v", application.list, listed)
	}

	page, err := service.GetThreadPage(t.Context(), &codefluxv1.GetThreadPageRequest{
		ThreadId: threadIdentity, Page: &codefluxv1.PageRequest{Cursor: "messages", Limit: 9},
	})
	if err != nil {
		t.Fatal(err)
	}
	if application.page.ThreadID != threadID || len(page.GetMessages()) != 1 || page.GetMessages()[0].GetAttachmentIds()[0].GetValue() != artifactID.String() {
		t.Fatalf("page command/response = %#v / %#v", application.page, page)
	}

	sendRevision := uint64(3)
	sent, err := service.SendMessage(t.Context(), &codefluxv1.SendMessageRequest{
		Control:  &codefluxv1.MutationControl{IdempotencyKey: "send-one", ExpectedRevision: &sendRevision},
		ThreadId: threadIdentity, Body: "body", AttachmentIds: []*codefluxv1.StableIdentity{artifactIdentity}, CreateDraftTask: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(application.send.AttachmentIDs) != 1 || application.send.AttachmentIDs[0] != artifactID || sent.GetDraftTask().GetTaskId().GetValue() != taskID.String() {
		t.Fatalf("send command/response = %#v / %#v", application.send, sent)
	}

	revision := uint64(3)
	renamed, err := service.RenameThread(t.Context(), &codefluxv1.RenameThreadRequest{
		Control:  &codefluxv1.MutationControl{IdempotencyKey: "rename-one", ExpectedRevision: &revision},
		ThreadId: threadIdentity, Title: "  Renamed  ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if application.rename.ExpectedRevision != revision || application.rename.Title != "Renamed" || renamed.GetThread().GetRevision() != 4 {
		t.Fatalf("rename command/response = %#v / %#v", application.rename, renamed)
	}

	archived, err := service.ArchiveThread(t.Context(), &codefluxv1.ArchiveThreadRequest{
		Control:  &codefluxv1.MutationControl{IdempotencyKey: "archive-one", ExpectedRevision: &revision},
		ThreadId: threadIdentity, Archived: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !application.archive.Archived || !archived.GetThread().GetArchived() {
		t.Fatalf("archive command/response = %#v / %#v", application.archive, archived)
	}
}

func TestThreadServiceRejectsLegacyAndInvalidAttachments(t *testing.T) {
	t.Parallel()

	threadID, _ := domain.NewThreadID()
	threadIdentity, _ := ThreadIDToProto(threadID)
	service, err := NewThreadService(&threadApplicationStub{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.SendMessage(t.Context(), &codefluxv1.SendMessageRequest{
		ThreadId: threadIdentity, AttachmentPaths: []string{"C:/secret.txt"},
	})
	var validation *RequestValidationError
	if !errors.As(err, &validation) || validation.Field != "attachment_paths" {
		t.Fatalf("legacy attachment error = %#v", err)
	}
	_, err = service.SendMessage(t.Context(), &codefluxv1.SendMessageRequest{
		Control:       &codefluxv1.MutationControl{ExpectedRevision: new(uint64)},
		ThreadId:      threadIdentity,
		AttachmentIds: []*codefluxv1.StableIdentity{{Kind: codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_THREAD, Value: threadID.String()}},
	})
	if !errors.As(err, &validation) || validation.Field != "attachment_ids" {
		t.Fatalf("wrong-kind attachment error = %#v", err)
	}
}

func TestThreadServiceRequiresRevisionAndMapsStaleError(t *testing.T) {
	t.Parallel()

	threadID, _ := domain.NewThreadID()
	threadIdentity, _ := ThreadIDToProto(threadID)
	application := &threadApplicationStub{renameErr: ErrThreadStaleRevision}
	service, err := NewThreadService(application)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.RenameThread(t.Context(), &codefluxv1.RenameThreadRequest{
		Control: &codefluxv1.MutationControl{IdempotencyKey: "missing-revision"}, ThreadId: threadIdentity, Title: "Title",
	})
	var validation *RequestValidationError
	if !errors.As(err, &validation) || validation.Field != "control.expected_revision" {
		t.Fatalf("missing revision error = %#v", err)
	}
	revision := uint64(4)
	_, err = service.RenameThread(t.Context(), &codefluxv1.RenameThreadRequest{
		Control: &codefluxv1.MutationControl{IdempotencyKey: "stale", ExpectedRevision: &revision}, ThreadId: threadIdentity, Title: "Title",
	})
	var applicationErr *ApplicationError
	if !errors.As(err, &applicationErr) || applicationErr.Code != codefluxv1.ErrorCode_ERROR_CODE_STALE_REVISION || applicationErr.EntityID.GetValue() != threadID.String() {
		t.Fatalf("stale error = %#v", err)
	}
}

type threadApplicationStub struct {
	create  CreateThreadCommand
	list    ListThreadsQuery
	page    ThreadPageQuery
	send    SendMessageCommand
	rename  RenameThreadCommand
	archive ArchiveThreadCommand

	createResult  ThreadView
	listResult    ThreadPage
	pageResult    MessagePage
	sendResult    SendMessageResult
	renameResult  ThreadView
	archiveResult ThreadView

	createErr  error
	listErr    error
	pageErr    error
	sendErr    error
	renameErr  error
	archiveErr error
}

func (stub *threadApplicationStub) CreateThread(_ context.Context, command CreateThreadCommand) (ThreadView, error) {
	stub.create = command
	return stub.createResult, stub.createErr
}

func (stub *threadApplicationStub) ListThreads(_ context.Context, query ListThreadsQuery) (ThreadPage, error) {
	stub.list = query
	return stub.listResult, stub.listErr
}

func (stub *threadApplicationStub) GetThreadPage(_ context.Context, query ThreadPageQuery) (MessagePage, error) {
	stub.page = query
	return stub.pageResult, stub.pageErr
}

func (stub *threadApplicationStub) SendMessage(_ context.Context, command SendMessageCommand) (SendMessageResult, error) {
	stub.send = command
	return stub.sendResult, stub.sendErr
}

func (stub *threadApplicationStub) RenameThread(_ context.Context, command RenameThreadCommand) (ThreadView, error) {
	stub.rename = command
	return stub.renameResult, stub.renameErr
}

func (stub *threadApplicationStub) ArchiveThread(_ context.Context, command ArchiveThreadCommand) (ThreadView, error) {
	stub.archive = command
	return stub.archiveResult, stub.archiveErr
}
