package storage

import (
	"context"
	"errors"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

// Project is one durable project aggregate.
type Project struct {
	ID        domain.ProjectID
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
	Revision  uint64
}

// Repository is one stable repository identity and current canonical location.
type Repository struct {
	ID            domain.RepositoryID
	ProjectID     domain.ProjectID
	CanonicalPath string
	GitIdentity   string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Revision      uint64
}

// Thread is one repository-scoped conversation aggregate.
type Thread struct {
	ID           domain.ThreadID
	ProjectID    domain.ProjectID
	RepositoryID domain.RepositoryID
	Title        string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	Revision     uint64
}

// MessageRole is the durable bounded message author class.
type MessageRole string

const (
	MessageRoleUser      MessageRole = "user"
	MessageRoleAssistant MessageRole = "assistant"
	MessageRoleSystem    MessageRole = "system"
	MessageRoleTool      MessageRole = "tool"
)

func (role MessageRole) IsValid() bool {
	switch role {
	case MessageRoleUser, MessageRoleAssistant, MessageRoleSystem, MessageRoleTool:
		return true
	default:
		return false
	}
}

// Message is one immutable thread entry.
type Message struct {
	ID             domain.MessageID
	ThreadID       domain.ThreadID
	Sequence       uint64
	Role           MessageRole
	BodyRedacted   string
	IdempotencyKey string
	CreatedAt      time.Time
}

// Repositories implements all SQLite domain repository contracts.
type Repositories struct {
	database *Database
	now      func() time.Time
}

// NewRepositories creates domain repositories without opening external state.
func NewRepositories(database *Database, now func() time.Time) (*Repositories, error) {
	if database == nil {
		return nil, errors.New("database must not be nil")
	}
	if now == nil {
		now = time.Now
	}
	return &Repositories{database: database, now: now}, nil
}

// ProjectOperations groups project and repository identity operations.
type ProjectOperations interface {
	CreateProject(context.Context, CreateProject) (Project, error)
	GetProject(context.Context, domain.ProjectID) (Project, error)
	CreateRepository(context.Context, CreateRepository) (Repository, error)
	GetRepository(context.Context, domain.RepositoryID) (Repository, error)
}

// ConversationOperations groups thread and immutable message operations.
type ConversationOperations interface {
	CreateThread(context.Context, CreateThread) (Thread, error)
	ListThreads(context.Context, ListThreads) (ThreadPage, error)
	AppendMessage(context.Context, AppendMessage) (Message, error)
}

// CreateProject declares one idempotency-independent project creation.
type CreateProject struct {
	ID   domain.ProjectID
	Name string
}

// CreateRepository binds one stable repository to a project and Git identity.
type CreateRepository struct {
	ID            domain.RepositoryID
	ProjectID     domain.ProjectID
	CanonicalPath string
	GitIdentity   string
}

// CreateThread declares one repository-scoped conversation.
type CreateThread struct {
	ID           domain.ThreadID
	ProjectID    domain.ProjectID
	RepositoryID domain.RepositoryID
	Title        string
}

// ThreadCursor is an exclusive stable cursor over descending update order.
type ThreadCursor struct {
	UpdatedAt time.Time
	ID        domain.ThreadID
}

// ListThreads declares bounded repository thread pagination.
type ListThreads struct {
	RepositoryID domain.RepositoryID
	Before       *ThreadCursor
	Limit        int
}

// ThreadPage carries one stable page and an optional exclusive next cursor.
type ThreadPage struct {
	Threads []Thread
	Next    *ThreadCursor
}

// AppendMessage declares one immutable idempotent message append.
type AppendMessage struct {
	ID             domain.MessageID
	ThreadID       domain.ThreadID
	Role           MessageRole
	BodyRedacted   string
	IdempotencyKey string
}

func validateBounded(label, value string, maximum int) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New(label + " must not be empty")
	}
	if len(value) > maximum {
		return errors.New(label + " exceeds maximum length")
	}
	return nil
}

func repositoryTime(microseconds int64) time.Time {
	return time.UnixMicro(microseconds).UTC()
}

func (repositories *Repositories) timestamp() (time.Time, int64) {
	now := repositories.now().UTC()
	return now, now.UnixMicro()
}

var (
	_ ProjectOperations      = (*Repositories)(nil)
	_ ConversationOperations = (*Repositories)(nil)
)
