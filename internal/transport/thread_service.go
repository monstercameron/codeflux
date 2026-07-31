package transport

import (
	"context"
	"errors"
	"strings"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	ErrThreadNotFound      = errors.New("thread target not found")
	ErrThreadStaleRevision = errors.New("thread revision is stale")
	ErrThreadConflict      = errors.New("thread command conflict")
	ErrThreadDenied        = errors.New("thread workspace authority denied")
	ErrThreadInvalidCursor = errors.New("thread page cursor is invalid")
)

type ThreadView struct {
	ThreadID    domain.ThreadID
	ProjectID   domain.ProjectID
	WorkspaceID domain.WorkspaceID
	SessionID   domain.SessionID
	TaskID      domain.TaskID
	TaskState   domain.TaskState
	Attention   string
	Unread      uint32
	Title       string
	Archived    bool
	Revision    uint64
	UpdatedAt   time.Time
}

type MessageView struct {
	MessageID     domain.MessageID
	ThreadID      domain.ThreadID
	Role          string
	BodyRedacted  string
	AttachmentIDs []domain.ArtifactID
	Revision      uint64
	Sequence      uint64
	CreatedAt     time.Time
}

type DraftTaskView struct {
	TaskID    domain.TaskID
	ThreadID  domain.ThreadID
	State     domain.TaskState
	Revision  uint64
	UpdatedAt time.Time
}

type ThreadPage struct {
	Threads    []ThreadView
	NextCursor string
	HasMore    bool
}

type MessagePage struct {
	Thread     ThreadView
	Messages   []MessageView
	NextCursor string
	HasMore    bool
}

type CreateThreadCommand struct {
	WorkspaceID    domain.WorkspaceID
	IdempotencyKey string
	Title          string
}

type ListThreadsQuery struct {
	WorkspaceID     domain.WorkspaceID
	Cursor          string
	Limit           uint32
	IncludeArchived bool
}

type ThreadPageQuery struct {
	ThreadID domain.ThreadID
	Cursor   string
	Limit    uint32
}

type SendMessageCommand struct {
	ThreadID         domain.ThreadID
	IdempotencyKey   string
	ExpectedRevision uint64
	Body             string
	AttachmentIDs    []domain.ArtifactID
	CreateDraftTask  bool
}

type SendMessageResult struct {
	Message   MessageView
	DraftTask *DraftTaskView
}

type RenameThreadCommand struct {
	ThreadID         domain.ThreadID
	IdempotencyKey   string
	ExpectedRevision uint64
	Title            string
}

type ArchiveThreadCommand struct {
	ThreadID         domain.ThreadID
	IdempotencyKey   string
	ExpectedRevision uint64
	Archived         bool
}

type ThreadApplication interface {
	CreateThread(context.Context, CreateThreadCommand) (ThreadView, error)
	ListThreads(context.Context, ListThreadsQuery) (ThreadPage, error)
	GetThreadPage(context.Context, ThreadPageQuery) (MessagePage, error)
	SendMessage(context.Context, SendMessageCommand) (SendMessageResult, error)
	RenameThread(context.Context, RenameThreadCommand) (ThreadView, error)
	ArchiveThread(context.Context, ArchiveThreadCommand) (ThreadView, error)
}

type ThreadService struct {
	codefluxv1.UnimplementedThreadServiceServer
	application ThreadApplication
}

func NewThreadService(application ThreadApplication) (*ThreadService, error) {
	if application == nil {
		return nil, errors.New("thread application is required")
	}
	return &ThreadService{application: application}, nil
}

func (service *ThreadService) CreateThread(ctx context.Context, request *codefluxv1.CreateThreadRequest) (*codefluxv1.CreateThreadResponse, error) {
	workspaceID, err := WorkspaceIDFromProto(request.GetWorkspaceId())
	if err != nil {
		return nil, requestIdentityError("workspace_id", err)
	}
	view, err := service.application.CreateThread(ctx, CreateThreadCommand{
		WorkspaceID: workspaceID, IdempotencyKey: request.GetControl().GetIdempotencyKey(),
		Title: strings.TrimSpace(request.GetTitle()),
	})
	if err != nil {
		return nil, mapThreadError(err, nil)
	}
	message, err := threadViewToProto(view)
	return &codefluxv1.CreateThreadResponse{Thread: message}, err
}

func (service *ThreadService) ListThreads(ctx context.Context, request *codefluxv1.ListThreadsRequest) (*codefluxv1.ListThreadsResponse, error) {
	workspaceID, err := WorkspaceIDFromProto(request.GetWorkspaceId())
	if err != nil {
		return nil, requestIdentityError("workspace_id", err)
	}
	page := request.GetPage()
	result, err := service.application.ListThreads(ctx, ListThreadsQuery{
		WorkspaceID: workspaceID, Cursor: page.GetCursor(), Limit: page.GetLimit(),
		IncludeArchived: request.GetIncludeArchived(),
	})
	if err != nil {
		return nil, mapThreadError(err, nil)
	}
	views := make([]*codefluxv1.ThreadView, 0, len(result.Threads))
	for _, view := range result.Threads {
		converted, err := threadViewToProto(view)
		if err != nil {
			return nil, err
		}
		views = append(views, converted)
	}
	return &codefluxv1.ListThreadsResponse{Threads: views, Page: &codefluxv1.PageInfo{
		NextCursor: result.NextCursor, HasMore: result.HasMore,
	}}, nil
}

func (service *ThreadService) GetThreadPage(ctx context.Context, request *codefluxv1.GetThreadPageRequest) (*codefluxv1.GetThreadPageResponse, error) {
	threadID, err := ThreadIDFromProto(request.GetThreadId())
	if err != nil {
		return nil, requestIdentityError("thread_id", err)
	}
	page := request.GetPage()
	result, err := service.application.GetThreadPage(ctx, ThreadPageQuery{
		ThreadID: threadID, Cursor: page.GetCursor(), Limit: page.GetLimit(),
	})
	if err != nil {
		return nil, mapThreadError(err, request.GetThreadId())
	}
	thread, err := threadViewToProto(result.Thread)
	if err != nil {
		return nil, err
	}
	messages := make([]*codefluxv1.MessageView, 0, len(result.Messages))
	for _, view := range result.Messages {
		converted, err := messageViewToProto(view)
		if err != nil {
			return nil, err
		}
		messages = append(messages, converted)
	}
	return &codefluxv1.GetThreadPageResponse{Thread: thread, Messages: messages, Page: &codefluxv1.PageInfo{
		NextCursor: result.NextCursor, HasMore: result.HasMore,
	}}, nil
}

func (service *ThreadService) SendMessage(ctx context.Context, request *codefluxv1.SendMessageRequest) (*codefluxv1.SendMessageResponse, error) {
	if len(request.GetAttachmentPaths()) != 0 {
		return nil, &RequestValidationError{Field: "attachment_paths", Reason: "is deprecated; use server attachment identities"}
	}
	threadID, revision, err := threadMutationControl(request.GetControl(), request.GetThreadId())
	if err != nil {
		return nil, err
	}
	attachmentIDs := make([]domain.ArtifactID, 0, len(request.GetAttachmentIds()))
	for _, identity := range request.GetAttachmentIds() {
		artifactID, err := ArtifactIDFromProto(identity)
		if err != nil {
			return nil, requestIdentityError("attachment_ids", err)
		}
		for _, existing := range attachmentIDs {
			if existing == artifactID {
				return nil, &RequestValidationError{Field: "attachment_ids", Reason: "must not contain duplicates"}
			}
		}
		attachmentIDs = append(attachmentIDs, artifactID)
	}
	result, err := service.application.SendMessage(ctx, SendMessageCommand{
		ThreadID: threadID, IdempotencyKey: request.GetControl().GetIdempotencyKey(),
		ExpectedRevision: revision,
		Body:             request.GetBody(), AttachmentIDs: attachmentIDs,
		CreateDraftTask: request.GetCreateDraftTask(),
	})
	if err != nil {
		return nil, mapThreadError(err, request.GetThreadId())
	}
	message, err := messageViewToProto(result.Message)
	if err != nil {
		return nil, err
	}
	response := &codefluxv1.SendMessageResponse{Message: message}
	if result.DraftTask != nil {
		response.DraftTask, err = draftTaskViewToProto(*result.DraftTask)
	}
	return response, err
}

func (service *ThreadService) RenameThread(ctx context.Context, request *codefluxv1.RenameThreadRequest) (*codefluxv1.RenameThreadResponse, error) {
	threadID, revision, err := threadMutationControl(request.GetControl(), request.GetThreadId())
	if err != nil {
		return nil, err
	}
	view, err := service.application.RenameThread(ctx, RenameThreadCommand{
		ThreadID: threadID, IdempotencyKey: request.GetControl().GetIdempotencyKey(),
		ExpectedRevision: revision, Title: strings.TrimSpace(request.GetTitle()),
	})
	if err != nil {
		return nil, mapThreadError(err, request.GetThreadId())
	}
	converted, err := threadViewToProto(view)
	return &codefluxv1.RenameThreadResponse{Thread: converted}, err
}

func (service *ThreadService) ArchiveThread(ctx context.Context, request *codefluxv1.ArchiveThreadRequest) (*codefluxv1.ArchiveThreadResponse, error) {
	threadID, revision, err := threadMutationControl(request.GetControl(), request.GetThreadId())
	if err != nil {
		return nil, err
	}
	view, err := service.application.ArchiveThread(ctx, ArchiveThreadCommand{
		ThreadID: threadID, IdempotencyKey: request.GetControl().GetIdempotencyKey(),
		ExpectedRevision: revision, Archived: request.GetArchived(),
	})
	if err != nil {
		return nil, mapThreadError(err, request.GetThreadId())
	}
	converted, err := threadViewToProto(view)
	return &codefluxv1.ArchiveThreadResponse{Thread: converted}, err
}

func threadMutationControl(control *codefluxv1.MutationControl, identity *codefluxv1.StableIdentity) (domain.ThreadID, uint64, error) {
	if control == nil || control.ExpectedRevision == nil {
		return domain.ThreadID{}, 0, &RequestValidationError{Field: "control.expected_revision", Reason: "is required"}
	}
	threadID, err := ThreadIDFromProto(identity)
	if err != nil {
		return domain.ThreadID{}, 0, requestIdentityError("thread_id", err)
	}
	return threadID, control.GetExpectedRevision(), nil
}

func requestIdentityError(field string, err error) error {
	return &RequestValidationError{Field: field, Reason: "has an invalid typed identity: " + err.Error()}
}

func threadViewToProto(view ThreadView) (*codefluxv1.ThreadView, error) {
	threadID, err := ThreadIDToProto(view.ThreadID)
	if err != nil {
		return nil, err
	}
	workspaceID, err := WorkspaceIDToProto(view.WorkspaceID)
	if err != nil {
		return nil, err
	}
	sessionID, err := SessionIDToProto(view.SessionID)
	if err != nil {
		return nil, err
	}
	updated := timestamppb.New(view.UpdatedAt.UTC())
	if err := updated.CheckValid(); err != nil {
		return nil, err
	}
	var taskID *codefluxv1.StableIdentity
	if !view.TaskID.IsZero() {
		taskID, err = TaskIDToProto(view.TaskID)
		if err != nil {
			return nil, err
		}
	}
	var projectID *codefluxv1.StableIdentity
	if !view.ProjectID.IsZero() {
		projectID, err = ProjectIDToProto(view.ProjectID)
		if err != nil {
			return nil, err
		}
	}
	return &codefluxv1.ThreadView{ThreadId: threadID, WorkspaceId: workspaceID, SessionId: sessionID,
		Title: redactedText(view.Title), Archived: view.Archived,
		Revision: view.Revision, UpdatedAt: updated, TaskState: string(view.TaskState),
		Attention: view.Attention, UnreadCount: view.Unread, TaskId: taskID, ProjectId: projectID}, nil
}

func messageViewToProto(view MessageView) (*codefluxv1.MessageView, error) {
	messageID, err := MessageIDToProto(view.MessageID)
	if err != nil {
		return nil, err
	}
	threadID, err := ThreadIDToProto(view.ThreadID)
	if err != nil {
		return nil, err
	}
	created := timestamppb.New(view.CreatedAt.UTC())
	if err := created.CheckValid(); err != nil {
		return nil, err
	}
	attachments := make([]*codefluxv1.StableIdentity, 0, len(view.AttachmentIDs))
	for _, id := range view.AttachmentIDs {
		identity, err := ArtifactIDToProto(id)
		if err != nil {
			return nil, err
		}
		attachments = append(attachments, identity)
	}
	return &codefluxv1.MessageView{MessageId: messageID, ThreadId: threadID,
		Role: view.Role, Body: redactedText(view.BodyRedacted), AttachmentIds: attachments,
		Revision: view.Revision, Sequence: view.Sequence, CreatedAt: created}, nil
}

func draftTaskViewToProto(view DraftTaskView) (*codefluxv1.TaskView, error) {
	taskID, err := TaskIDToProto(view.TaskID)
	if err != nil {
		return nil, err
	}
	threadID, err := ThreadIDToProto(view.ThreadID)
	if err != nil {
		return nil, err
	}
	updated := timestamppb.New(view.UpdatedAt.UTC())
	if err := updated.CheckValid(); err != nil {
		return nil, err
	}
	return &codefluxv1.TaskView{TaskId: taskID, ThreadId: threadID,
		State: string(view.State), Revision: view.Revision, UpdatedAt: updated}, nil
}

func redactedText(value string) *codefluxv1.RedactedText {
	return &codefluxv1.RedactedText{Value: value, OriginalBytes: uint64(len(value))}
}

func mapThreadError(err error, entity *codefluxv1.StableIdentity) error {
	switch {
	case errors.Is(err, ErrThreadNotFound):
		return &ApplicationError{Code: codefluxv1.ErrorCode_ERROR_CODE_NOT_FOUND, SafeMessage: "The thread could not be found.", EntityID: entity}
	case errors.Is(err, ErrThreadStaleRevision):
		return &ApplicationError{Code: codefluxv1.ErrorCode_ERROR_CODE_STALE_REVISION, SafeMessage: "The thread changed before this request.", EntityID: entity}
	case errors.Is(err, ErrThreadConflict):
		return &ApplicationError{Code: codefluxv1.ErrorCode_ERROR_CODE_DUPLICATE, SafeMessage: "The idempotency key belongs to a different thread request.", EntityID: entity}
	case errors.Is(err, ErrThreadDenied):
		return &ApplicationError{Code: codefluxv1.ErrorCode_ERROR_CODE_DENIED, SafeMessage: "The workspace does not authorize this thread.", EntityID: entity}
	case errors.Is(err, ErrThreadInvalidCursor):
		return &ApplicationError{Code: codefluxv1.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, SafeMessage: "The page cursor is invalid or no longer applies.", EntityID: entity}
	default:
		return err
	}
}

var _ codefluxv1.ThreadServiceServer = (*ThreadService)(nil)
