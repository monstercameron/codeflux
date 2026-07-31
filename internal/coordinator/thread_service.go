package coordinator

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
)

type threadRepository interface {
	GetWorkspaceScope(context.Context, domain.WorkspaceID) (storage.WorkspaceScope, error)
	CreateThreadCommit(context.Context, storage.CreateThread) (storage.ThreadCommit, error)
	ListThreads(context.Context, storage.ListThreads) (storage.ThreadPage, error)
	GetThread(context.Context, domain.ThreadID) (storage.Thread, error)
	ListMessages(context.Context, storage.ListMessages) (storage.MessagePage, error)
	AppendMessageAndDraftTask(context.Context, storage.AppendMessageAndDraftTask) (storage.AppendedMessageAndDraftTask, error)
	RenameThreadCommit(context.Context, storage.RenameThread) (storage.ThreadCommit, error)
	ArchiveThreadCommit(context.Context, storage.ArchiveThread) (storage.ThreadCommit, error)
}

type ThreadApplication struct {
	repositories threadRepository
	cursors      cipher.AEAD
	random       io.Reader
	publisher    storage.CommittedEventPublisher
}

func NewThreadApplication(repositories threadRepository, cursorSecret string, publishers ...storage.CommittedEventPublisher) (*ThreadApplication, error) {
	if repositories == nil || len(cursorSecret) < 32 {
		return nil, errors.New("thread repositories and cursor secret are required")
	}
	key := sha256.Sum256([]byte("codeflux-thread-cursor-v1\x00" + cursorSecret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	application := &ThreadApplication{repositories: repositories, cursors: aead, random: rand.Reader}
	if len(publishers) > 0 {
		application.publisher = publishers[0]
	}
	return application, nil
}

func (application *ThreadApplication) CreateThread(ctx context.Context, command transport.CreateThreadCommand) (transport.ThreadView, error) {
	scope, err := application.repositories.GetWorkspaceScope(ctx, command.WorkspaceID)
	if err != nil {
		return transport.ThreadView{}, mapThreadRepositoryError(err)
	}
	threadID, err := domain.NewThreadID()
	if err != nil {
		return transport.ThreadView{}, err
	}
	sessionID, err := domain.NewSessionID()
	if err != nil {
		return transport.ThreadView{}, err
	}
	commit, err := application.repositories.CreateThreadCommit(ctx, storage.CreateThread{
		ID: threadID, SessionID: sessionID, ProjectID: scope.ProjectID, RepositoryID: scope.RepositoryID,
		WorkspaceID: scope.WorkspaceID, Title: command.Title, IdempotencyKey: command.IdempotencyKey,
	})
	if err != nil {
		return transport.ThreadView{}, mapThreadRepositoryError(err)
	}
	if err := application.publishCommitted(commit.Events); err != nil {
		return transport.ThreadView{}, err
	}
	return storedThreadView(commit.Thread), nil
}

func (application *ThreadApplication) ListThreads(ctx context.Context, query transport.ListThreadsQuery) (transport.ThreadPage, error) {
	scope, err := application.repositories.GetWorkspaceScope(ctx, query.WorkspaceID)
	if err != nil {
		return transport.ThreadPage{}, mapThreadRepositoryError(err)
	}
	limit, err := boundedThreadPageLimit(query.Limit)
	if err != nil {
		return transport.ThreadPage{}, err
	}
	var before *storage.ThreadCursor
	if query.Cursor != "" {
		payload, err := application.decodeCursor(query.Cursor)
		if err != nil || payload.Kind != "threads" || payload.Scope != query.WorkspaceID.String() || payload.IncludeArchived != query.IncludeArchived {
			return transport.ThreadPage{}, transport.ErrThreadInvalidCursor
		}
		id, err := domain.ParseThreadID(payload.Identity)
		if err != nil || payload.Position < 0 {
			return transport.ThreadPage{}, transport.ErrThreadInvalidCursor
		}
		before = &storage.ThreadCursor{UpdatedAt: time.UnixMicro(payload.Position).UTC(), ID: id}
	}
	page, err := application.repositories.ListThreads(ctx, storage.ListThreads{
		RepositoryID: scope.RepositoryID, WorkspaceID: scope.WorkspaceID,
		Before: before, Limit: limit, IncludeArchived: query.IncludeArchived,
	})
	if err != nil {
		return transport.ThreadPage{}, mapThreadRepositoryError(err)
	}
	result := transport.ThreadPage{Threads: make([]transport.ThreadView, 0, len(page.Threads)), HasMore: page.Next != nil}
	for _, thread := range page.Threads {
		result.Threads = append(result.Threads, storedThreadView(thread))
	}
	if page.Next != nil {
		result.NextCursor, err = application.encodeCursor(cursorPayload{
			Kind: "threads", Scope: query.WorkspaceID.String(), IncludeArchived: query.IncludeArchived,
			Position: page.Next.UpdatedAt.UnixMicro(), Identity: page.Next.ID.String(),
		})
	}
	return result, err
}

func (application *ThreadApplication) GetThreadPage(ctx context.Context, query transport.ThreadPageQuery) (transport.MessagePage, error) {
	thread, err := application.repositories.GetThread(ctx, query.ThreadID)
	if err != nil || thread.WorkspaceID.IsZero() {
		return transport.MessagePage{}, mapThreadRepositoryError(coalesceThreadAuthorityError(err, thread.WorkspaceID.IsZero()))
	}
	limit, err := boundedThreadPageLimit(query.Limit)
	if err != nil {
		return transport.MessagePage{}, err
	}
	var before *storage.MessageCursor
	if query.Cursor != "" {
		payload, err := application.decodeCursor(query.Cursor)
		if err != nil || payload.Kind != "messages" || payload.Scope != query.ThreadID.String() || payload.Position < 1 {
			return transport.MessagePage{}, transport.ErrThreadInvalidCursor
		}
		before = &storage.MessageCursor{BeforeSequence: uint64(payload.Position)}
	}
	page, err := application.repositories.ListMessages(ctx, storage.ListMessages{ThreadID: query.ThreadID, Before: before, Limit: limit})
	if err != nil {
		return transport.MessagePage{}, mapThreadRepositoryError(err)
	}
	result := transport.MessagePage{Thread: storedThreadView(thread), Messages: make([]transport.MessageView, 0, len(page.Messages)), HasMore: page.Next != nil}
	for _, message := range page.Messages {
		result.Messages = append(result.Messages, storedMessageView(message))
	}
	if page.Next != nil {
		result.NextCursor, err = application.encodeCursor(cursorPayload{
			Kind: "messages", Scope: query.ThreadID.String(), Position: int64(page.Next.BeforeSequence),
		})
	}
	return result, err
}

func (application *ThreadApplication) SendMessage(ctx context.Context, command transport.SendMessageCommand) (transport.SendMessageResult, error) {
	thread, err := application.repositories.GetThread(ctx, command.ThreadID)
	if err != nil || thread.WorkspaceID.IsZero() || thread.Archived {
		return transport.SendMessageResult{}, mapThreadRepositoryError(coalesceThreadAuthorityError(err, thread.WorkspaceID.IsZero() || thread.Archived))
	}
	messageID, err := domain.NewMessageID()
	if err != nil {
		return transport.SendMessageResult{}, err
	}
	input := storage.AppendMessageAndDraftTask{Message: storage.AppendMessage{
		ID: messageID, ThreadID: command.ThreadID, Role: storage.MessageRoleUser,
		BodyRedacted: command.Body, AttachmentIDs: append([]domain.ArtifactID(nil), command.AttachmentIDs...),
		IdempotencyKey: command.IdempotencyKey,
	}, ExpectedRevision: command.ExpectedRevision}
	if command.CreateDraftTask {
		taskID, err := domain.NewTaskID()
		if err != nil {
			return transport.SendMessageResult{}, err
		}
		input.DraftTask = &storage.CreateTask{
			ID: taskID, ThreadID: thread.ID, RepositoryID: thread.RepositoryID,
			PolicyPreset: domain.PolicyPresetCorrectness, ReasoningEffort: domain.ReasoningEffortMaximum,
			RiskLevel:         domain.RiskLevelRoutine,
			RequiredAssurance: domain.AssuranceLevelContractChecked, SettingsRevision: 0,
			IdempotencyKey: command.IdempotencyKey,
		}
	}
	stored, err := application.repositories.AppendMessageAndDraftTask(ctx, input)
	if err != nil {
		return transport.SendMessageResult{}, mapThreadRepositoryError(err)
	}
	if err := application.publishCommitted(stored.Events); err != nil {
		return transport.SendMessageResult{}, err
	}
	result := transport.SendMessageResult{Message: storedMessageView(stored.Message)}
	if stored.DraftTask != nil {
		result.DraftTask = &transport.DraftTaskView{TaskID: stored.DraftTask.ID, ThreadID: stored.DraftTask.ThreadID,
			State: stored.DraftTask.State, Revision: stored.DraftTask.Revision, UpdatedAt: stored.DraftTask.UpdatedAt}
	}
	return result, nil
}

func (application *ThreadApplication) RenameThread(ctx context.Context, command transport.RenameThreadCommand) (transport.ThreadView, error) {
	commit, err := application.repositories.RenameThreadCommit(ctx, storage.RenameThread{
		ThreadID: command.ThreadID, ExpectedRevision: command.ExpectedRevision,
		Title: command.Title, IdempotencyKey: command.IdempotencyKey,
	})
	if err != nil {
		return transport.ThreadView{}, mapThreadRepositoryError(err)
	}
	if err := application.publishCommitted(commit.Events); err != nil {
		return transport.ThreadView{}, err
	}
	return storedThreadView(commit.Thread), nil
}

func (application *ThreadApplication) ArchiveThread(ctx context.Context, command transport.ArchiveThreadCommand) (transport.ThreadView, error) {
	commit, err := application.repositories.ArchiveThreadCommit(ctx, storage.ArchiveThread{
		ThreadID: command.ThreadID, ExpectedRevision: command.ExpectedRevision,
		Archived: command.Archived, IdempotencyKey: command.IdempotencyKey,
	})
	if err != nil {
		return transport.ThreadView{}, mapThreadRepositoryError(err)
	}
	if err := application.publishCommitted(commit.Events); err != nil {
		return transport.ThreadView{}, err
	}
	return storedThreadView(commit.Thread), nil
}

func (application *ThreadApplication) publishCommitted(committed []events.SessionEvent) error {
	if application.publisher == nil {
		return nil
	}
	for _, event := range committed {
		if err := application.publisher.PublishCommitted(event); err != nil {
			return err
		}
	}
	return nil
}

func storedThreadView(thread storage.Thread) transport.ThreadView {
	return transport.ThreadView{ThreadID: thread.ID, WorkspaceID: thread.WorkspaceID,
		SessionID: thread.SessionID, TaskID: thread.TaskID, TaskState: thread.TaskState,
		Attention: threadAttention(thread.TaskState),
		Title:     thread.Title, Archived: thread.Archived, Revision: thread.Revision, UpdatedAt: thread.UpdatedAt}
}

func storedMessageView(message storage.Message) transport.MessageView {
	return transport.MessageView{MessageID: message.ID, ThreadID: message.ThreadID,
		Role: string(message.Role), BodyRedacted: message.BodyRedacted,
		AttachmentIDs: append([]domain.ArtifactID(nil), message.AttachmentIDs...),
		Revision:      message.Sequence, Sequence: message.Sequence, CreatedAt: message.CreatedAt}
}

func threadAttention(state domain.TaskState) string {
	switch state {
	case domain.TaskStateAwaitingAuthority:
		return "pending-approval"
	case domain.TaskStateRecoveryRequired:
		return "recovery"
	case domain.TaskStateFailed:
		return "validation-failure"
	case domain.TaskStateAwaitingPlanApproval, domain.TaskStateAwaitingReview:
		return "user-input"
	default:
		return "none"
	}
}

func boundedThreadPageLimit(limit uint32) (int, error) {
	if limit == 0 {
		return 50, nil
	}
	if limit > 100 {
		return 0, transport.ErrThreadInvalidCursor
	}
	return int(limit), nil
}

func mapThreadRepositoryError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, storage.ErrNotFound):
		return transport.ErrThreadNotFound
	case errors.Is(err, storage.ErrStaleRevision):
		return transport.ErrThreadStaleRevision
	case errors.Is(err, storage.ErrConflict):
		return transport.ErrThreadConflict
	case errors.Is(err, storage.ErrConstraint):
		return transport.ErrThreadDenied
	default:
		return err
	}
}

func coalesceThreadAuthorityError(err error, denied bool) error {
	if err != nil {
		return err
	}
	if denied {
		return storage.ErrConstraint
	}
	return nil
}

type cursorPayload struct {
	Version         int    `json:"v"`
	Kind            string `json:"k"`
	Scope           string `json:"s"`
	IncludeArchived bool   `json:"a,omitempty"`
	Position        int64  `json:"p"`
	Identity        string `json:"i,omitempty"`
}

func (application *ThreadApplication) encodeCursor(payload cursorPayload) (string, error) {
	payload.Version = 1
	plain, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, application.cursors.NonceSize())
	if _, err := io.ReadFull(application.random, nonce); err != nil {
		return "", err
	}
	sealed := application.cursors.Seal(nil, nonce, plain, []byte("thread-page-v1"))
	return base64.RawURLEncoding.EncodeToString(append(nonce, sealed...)), nil
}

func (application *ThreadApplication) decodeCursor(raw string) (cursorPayload, error) {
	if len(raw) == 0 || len(raw) > 1024 {
		return cursorPayload{}, transport.ErrThreadInvalidCursor
	}
	encoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(encoded) <= application.cursors.NonceSize() {
		return cursorPayload{}, transport.ErrThreadInvalidCursor
	}
	nonce, sealed := encoded[:application.cursors.NonceSize()], encoded[application.cursors.NonceSize():]
	plain, err := application.cursors.Open(nil, nonce, sealed, []byte("thread-page-v1"))
	if err != nil {
		return cursorPayload{}, transport.ErrThreadInvalidCursor
	}
	var payload cursorPayload
	if err := json.Unmarshal(plain, &payload); err != nil || payload.Version != 1 || payload.Scope == "" {
		return cursorPayload{}, transport.ErrThreadInvalidCursor
	}
	return payload, nil
}

var _ transport.ThreadApplication = (*ThreadApplication)(nil)
