package threadrail

import (
	"context"
	"fmt"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
)

// PageClient is the typed port owned by the thread rail. The generated gRPC
// adapter below is one implementation; reducer tests use deterministic fakes.
type PageClient interface {
	ListThreads(context.Context, PageQuery) (Page, error)
	CreateThread(context.Context, CreateCommand) (Thread, error)
	RenameThread(context.Context, RenameCommand) (Thread, error)
	ArchiveThread(context.Context, ArchiveCommand) (Thread, error)
}

// LoadFirstPage executes the first bounded query and applies the response.
func LoadFirstPage(ctx context.Context, state State, client PageClient) (State, error) {
	loading, query := BeginFirstPage(state)
	page, err := client.ListThreads(ctx, query)
	if err != nil {
		return FailPage(loading, PageError{Code: "list-failed", Message: "Threads could not be loaded.", Retryable: true}), err
	}
	return ApplyPage(loading, page)
}

// LoadNextPage executes at most one cursor request. If no request is due, the
// original state and a nil error are returned.
func LoadNextPage(ctx context.Context, state State, client PageClient) (State, error) {
	loading, query, ok := BeginNextPage(state)
	if !ok {
		return state, nil
	}
	page, err := client.ListThreads(ctx, query)
	if err != nil {
		return FailPage(loading, PageError{Code: "pagination-failed", Message: "More threads could not be loaded.", Retryable: true}), err
	}
	return ApplyPage(loading, page)
}

// CreateThread retains the command identity across ambiguous client failures.
func CreateThread(ctx context.Context, state State, client PageClient, command CreateCommand) (State, error) {
	pending, err := BeginCreate(state, command)
	if err != nil {
		return state, err
	}
	thread, err := client.CreateThread(ctx, command)
	if err != nil {
		return pending, err
	}
	return CommitCreate(pending, command.Key, thread)
}

// RenameThread retains the command and waits for an authoritative response
// before changing the visible title.
func RenameThread(ctx context.Context, state State, client PageClient, command RenameCommand) (State, error) {
	pending, err := BeginRename(state, command)
	if err != nil {
		return state, err
	}
	thread, err := client.RenameThread(ctx, command)
	if err != nil {
		return pending, err
	}
	return CommitRename(pending, command.Key, thread)
}

// ArchiveThread requires BeginArchive confirmation and waits for the server's
// committed projection before filter membership changes.
func ArchiveThread(ctx context.Context, state State, client PageClient, command ArchiveCommand) (State, error) {
	pending, err := BeginArchive(state, command)
	if err != nil {
		return state, err
	}
	thread, err := client.ArchiveThread(ctx, command)
	if err != nil {
		return pending, err
	}
	return CommitArchive(pending, command.Key, thread)
}

// GRPCClient adapts the generated ThreadService client without leaking protobuf
// values into immutable reducer state.
type GRPCClient struct {
	repositoryID domain.RepositoryID
	workspaceID  domain.WorkspaceID
	client       codefluxv1.ThreadServiceClient
}

// NewGRPCClient binds one generated client to an authorized repository and
// workspace scope.
func NewGRPCClient(repositoryID domain.RepositoryID, workspaceID domain.WorkspaceID, client codefluxv1.ThreadServiceClient) (GRPCClient, error) {
	if repositoryID.IsZero() || workspaceID.IsZero() || client == nil {
		return GRPCClient{}, fmt.Errorf("%w: grpc client scope", ErrInvalidModel)
	}
	return GRPCClient{repositoryID: repositoryID, workspaceID: workspaceID, client: client}, nil
}

func (c GRPCClient) ListThreads(ctx context.Context, query PageQuery) (Page, error) {
	if query.RepositoryID != c.repositoryID || query.WorkspaceID != c.workspaceID || query.Limit == 0 || query.Limit > 1000 {
		return Page{}, fmt.Errorf("%w: page query", ErrInvalidModel)
	}
	response, err := c.client.ListThreads(ctx, &codefluxv1.ListThreadsRequest{
		WorkspaceId:     stableIdentity(codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_WORKSPACE, c.workspaceID.String()),
		Page:            &codefluxv1.PageRequest{Cursor: string(query.Cursor), Limit: query.Limit},
		IncludeArchived: query.IncludeArchived,
	})
	if err != nil {
		return Page{}, err
	}
	if response == nil || response.GetPage() == nil {
		return Page{}, fmt.Errorf("%w: missing page response", ErrInvalidPage)
	}
	threads := make([]Thread, 0, len(response.GetThreads()))
	for _, view := range response.GetThreads() {
		thread, decodeErr := c.decodeThread(view)
		if decodeErr != nil {
			return Page{}, decodeErr
		}
		threads = append(threads, thread)
	}
	return Page{
		RequestCursor: query.Cursor, Threads: threads,
		NextCursor: Cursor(response.GetPage().GetNextCursor()), HasMore: response.GetPage().GetHasMore(),
	}, nil
}

func (c GRPCClient) CreateThread(ctx context.Context, command CreateCommand) (Thread, error) {
	response, err := c.client.CreateThread(ctx, &codefluxv1.CreateThreadRequest{
		Control:     &codefluxv1.MutationControl{IdempotencyKey: string(command.Key)},
		WorkspaceId: stableIdentity(codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_WORKSPACE, c.workspaceID.String()),
		Title:       command.Title,
	})
	if err != nil {
		return Thread{}, err
	}
	return c.decodeThread(response.GetThread())
}

func (c GRPCClient) RenameThread(ctx context.Context, command RenameCommand) (Thread, error) {
	revision := command.ExpectedRevision
	response, err := c.client.RenameThread(ctx, &codefluxv1.RenameThreadRequest{
		Control:  &codefluxv1.MutationControl{IdempotencyKey: string(command.Key), ExpectedRevision: &revision},
		ThreadId: stableIdentity(codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_THREAD, command.ThreadID.String()),
		Title:    command.Title,
	})
	if err != nil {
		return Thread{}, err
	}
	return c.decodeThread(response.GetThread())
}

func (c GRPCClient) ArchiveThread(ctx context.Context, command ArchiveCommand) (Thread, error) {
	revision := command.ExpectedRevision
	response, err := c.client.ArchiveThread(ctx, &codefluxv1.ArchiveThreadRequest{
		Control:  &codefluxv1.MutationControl{IdempotencyKey: string(command.Key), ExpectedRevision: &revision},
		ThreadId: stableIdentity(codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_THREAD, command.ThreadID.String()),
		Archived: command.Archived,
	})
	if err != nil {
		return Thread{}, err
	}
	return c.decodeThread(response.GetThread())
}

func (c GRPCClient) decodeThread(view *codefluxv1.ThreadView) (Thread, error) {
	if view == nil || view.GetThreadId() == nil ||
		view.GetThreadId().GetKind() != codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_THREAD ||
		view.GetSessionId() == nil ||
		view.GetSessionId().GetKind() != codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_SESSION ||
		view.GetWorkspaceId() == nil ||
		view.GetWorkspaceId().GetKind() != codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_WORKSPACE ||
		view.GetWorkspaceId().GetValue() != c.workspaceID.String() || view.GetTitle() == nil ||
		view.GetUpdatedAt() == nil || !view.GetUpdatedAt().IsValid() {
		return Thread{}, fmt.Errorf("%w: thread response", ErrInvalidPage)
	}
	threadID, err := domain.ParseThreadID(view.GetThreadId().GetValue())
	if err != nil {
		return Thread{}, fmt.Errorf("%w: thread identity", ErrInvalidPage)
	}
	sessionID, err := domain.ParseSessionID(view.GetSessionId().GetValue())
	if err != nil {
		return Thread{}, fmt.Errorf("%w: session identity", ErrInvalidPage)
	}
	var taskID domain.TaskID
	if view.GetTaskId() != nil {
		if view.GetTaskId().GetKind() != codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_TASK {
			return Thread{}, fmt.Errorf("%w: task identity", ErrInvalidPage)
		}
		taskID, err = domain.ParseTaskID(view.GetTaskId().GetValue())
		if err != nil {
			return Thread{}, fmt.Errorf("%w: task identity", ErrInvalidPage)
		}
	}
	taskState, taskStateOK := threadRailTaskState(domain.TaskState(view.GetTaskState()))
	if !taskStateOK {
		return Thread{}, fmt.Errorf("%w: task state", ErrInvalidPage)
	}
	attention := Attention(view.GetAttention())
	if attention == "" {
		attention = AttentionNone
	}
	return NewThread(ThreadInput{
		ID: threadID, SessionID: sessionID, RepositoryID: c.repositoryID, WorkspaceID: c.workspaceID, TaskID: taskID,
		Title: view.GetTitle().GetValue(), TaskState: taskState, Attention: attention, Unread: view.GetUnreadCount(),
		Archived: view.GetArchived(), Revision: view.GetRevision(), UpdatedAt: view.GetUpdatedAt().AsTime(),
	})
}

func threadRailTaskState(state domain.TaskState) (TaskState, bool) {
	switch state {
	case "":
		return TaskStateNone, true
	case domain.TaskStateDraft, domain.TaskStateForecasting,
		domain.TaskStateAwaitingPlanApproval, domain.TaskStateReady:
		return TaskStateDraft, true
	case domain.TaskStateRunning, domain.TaskStateAwaitingAuthority,
		domain.TaskStateValidating, domain.TaskStateAwaitingReview:
		return TaskStateRunning, true
	case domain.TaskStatePaused, domain.TaskStateRecoveryRequired:
		return TaskStatePaused, true
	case domain.TaskStateCompleted:
		return TaskStateComplete, true
	case domain.TaskStateFailed:
		return TaskStateFailed, true
	case domain.TaskStateCancelled, domain.TaskStateRolledBack:
		return TaskStateStopped, true
	default:
		return TaskStateNone, false
	}
}

func stableIdentity(kind codefluxv1.StableIdentityKind, value string) *codefluxv1.StableIdentity {
	return &codefluxv1.StableIdentity{Kind: kind, Value: value}
}

// Verify at compile time that GRPCClient remains the complete page/mutation
// adapter and that the generated interface retains ordinary gRPC call options.
var _ PageClient = GRPCClient{}
