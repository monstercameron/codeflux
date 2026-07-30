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

// Task is one durable task aggregate.
type Task struct {
	ID                domain.TaskID
	ThreadID          domain.ThreadID
	RepositoryID      domain.RepositoryID
	RequestMessageID  *domain.MessageID
	State             domain.TaskState
	PolicyPreset      domain.PolicyPreset
	ReasoningEffort   domain.ReasoningEffort
	RiskLevel         domain.RiskLevel
	RequiredAssurance domain.AssuranceLevel
	CreatedAt         time.Time
	UpdatedAt         time.Time
	Revision          uint64
}

// TaskEvent is one immutable ordered task fact.
type TaskEvent struct {
	ID             domain.EventID
	TaskID         domain.TaskID
	RunID          *domain.RunID
	Sequence       uint64
	EventType      string
	PayloadJSON    string
	IdempotencyKey string
	CreatedAt      time.Time
}

// TaskReplay is the deterministic task state reconstructed from ordered events.
type TaskReplay struct {
	State      domain.TaskState
	Revision   uint64
	EventCount uint64
}

// CreateTask declares one initial draft task.
type CreateTask struct {
	ID                domain.TaskID
	ThreadID          domain.ThreadID
	RepositoryID      domain.RepositoryID
	RequestMessageID  *domain.MessageID
	PolicyPreset      domain.PolicyPreset
	ReasoningEffort   domain.ReasoningEffort
	RiskLevel         domain.RiskLevel
	RequiredAssurance domain.AssuranceLevel
	IdempotencyKey    string
}

// TransitionTask declares one optimistic state change and its atomic event.
type TransitionTask struct {
	EventID          domain.EventID
	TaskID           domain.TaskID
	RunID            *domain.RunID
	ExpectedRevision uint64
	From             domain.TaskState
	To               domain.TaskState
	Approval         domain.ApprovalRequestState
	IdempotencyKey   string
}

// TransitionedTask returns the aggregate and correctness-bearing event.
type TransitionedTask struct {
	Task  Task
	Event TaskEvent
}

// AppendTaskEvent declares one immutable event independent of a state change.
type AppendTaskEvent struct {
	ID             domain.EventID
	TaskID         domain.TaskID
	RunID          *domain.RunID
	EventType      string
	PayloadJSON    string
	IdempotencyKey string
}

// TaskOperations groups task aggregate and event-journal operations.
type TaskOperations interface {
	CreateTask(context.Context, CreateTask) (Task, error)
	GetTask(context.Context, domain.TaskID) (Task, error)
	TransitionTask(context.Context, TransitionTask) (TransitionedTask, error)
	AppendTaskEvent(context.Context, AppendTaskEvent) (TaskEvent, error)
	ReplayTask(context.Context, domain.TaskID) (TaskReplay, error)
}

// Approval is one durable authority request.
type Approval struct {
	ID               domain.ApprovalID
	TaskID           domain.TaskID
	RunID            *domain.RunID
	State            domain.ApprovalRequestState
	Scope            string
	RequestReason    string
	ResolutionReason *string
	IdempotencyKey   string
	RequestedAt      time.Time
	DecidedAt        *time.Time
	ExpiresAt        *time.Time
	Revision         uint64
}

// CreateApproval declares one idempotent authority request.
type CreateApproval struct {
	ID             domain.ApprovalID
	TaskID         domain.TaskID
	RunID          *domain.RunID
	Scope          string
	RequestReason  string
	IdempotencyKey string
	ExpiresAt      *time.Time
}

// ResolveApproval declares one optimistic authority decision.
type ResolveApproval struct {
	ID               domain.ApprovalID
	ExpectedRevision uint64
	To               domain.ApprovalRequestState
	ResolutionReason string
}

// BudgetAccount is the durable exact accounting state for one task budget.
type BudgetAccount struct {
	Budget       domain.TaskBudget
	TaskID       domain.TaskID
	ReservedCost domain.Money
	ActualCost   domain.Money
	ActualTokens domain.TokenCount
	CreatedAt    time.Time
	UpdatedAt    time.Time
	Revision     uint64
}

// CreateBudget declares one exact task budget.
type CreateBudget struct {
	TaskID domain.TaskID
	Budget domain.TaskBudget
}

// ReserveBudget declares one optimistic exact-cost reservation.
type ReserveBudget struct {
	ID               domain.BudgetID
	ExpectedRevision uint64
	Amount           domain.Money
}

// PostActualCost declares one exact posting and reservation release.
type PostActualCost struct {
	ID                   domain.BudgetID
	ExpectedRevision     uint64
	Actual               domain.Money
	ReleaseReservedMinor int64
	Tokens               domain.TokenCount
}

// ApprovalBudgetOperations groups authority and exact budget operations.
type ApprovalBudgetOperations interface {
	CreateApproval(context.Context, CreateApproval) (Approval, error)
	ResolveApproval(context.Context, ResolveApproval) (Approval, error)
	CreateBudget(context.Context, CreateBudget) (BudgetAccount, error)
	ReserveBudget(context.Context, ReserveBudget) (BudgetAccount, error)
	PostActualCost(context.Context, PostActualCost) (BudgetAccount, error)
}

// Checkpoint is one repository-bound recovery point.
type Checkpoint struct {
	ID                 domain.CheckpointID
	TaskID             domain.TaskID
	RunID              *domain.RunID
	State              domain.CheckpointState
	RepositoryRevision string
	WorktreeDiffHash   string
	EventSequence      uint64
	IdempotencyKey     string
	CreatedAt          time.Time
	UpdatedAt          time.Time
	Revision           uint64
}

// CreateCheckpoint declares one idempotent recovery point.
type CreateCheckpoint struct {
	ID                 domain.CheckpointID
	TaskID             domain.TaskID
	RunID              *domain.RunID
	State              domain.CheckpointState
	RepositoryRevision string
	WorktreeDiffHash   string
	EventSequence      uint64
	IdempotencyKey     string
}

// Validation is one durable validation aggregate.
type Validation struct {
	ID              domain.ValidationID
	TaskID          domain.TaskID
	RunID           *domain.RunID
	ArtifactID      *domain.ArtifactID
	State           domain.ValidationState
	Severity        domain.ValidationSeverity
	ProfileName     string
	SummaryRedacted *string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	Revision        uint64
}

// CreateValidation declares one validation record.
type CreateValidation struct {
	ID              domain.ValidationID
	TaskID          domain.TaskID
	RunID           *domain.RunID
	ArtifactID      *domain.ArtifactID
	State           domain.ValidationState
	Severity        domain.ValidationSeverity
	ProfileName     string
	SummaryRedacted *string
}

// Evidence is one revision-bound correctness artifact.
type Evidence struct {
	ID              domain.EvidenceID
	ValidationID    domain.ValidationID
	TaskID          domain.TaskID
	ArtifactID      *domain.ArtifactID
	AssuranceLevel  domain.AssuranceLevel
	EvidenceType    string
	ContentHash     string
	SummaryRedacted string
	CreatedAt       time.Time
	Revision        uint64
}

// CreateEvidence declares one immutable evidence row.
type CreateEvidence struct {
	ID              domain.EvidenceID
	ValidationID    domain.ValidationID
	TaskID          domain.TaskID
	ArtifactID      *domain.ArtifactID
	AssuranceLevel  domain.AssuranceLevel
	EvidenceType    string
	ContentHash     string
	SummaryRedacted string
}

// RecoveryEvidenceOperations groups checkpoint, validation, and evidence writes.
type RecoveryEvidenceOperations interface {
	CreateCheckpoint(context.Context, CreateCheckpoint) (Checkpoint, error)
	CreateValidation(context.Context, CreateValidation) (Validation, error)
	CreateEvidence(context.Context, CreateEvidence) (Evidence, error)
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
	_ ProjectOperations          = (*Repositories)(nil)
	_ ConversationOperations     = (*Repositories)(nil)
	_ TaskOperations             = (*Repositories)(nil)
	_ ApprovalBudgetOperations   = (*Repositories)(nil)
	_ RecoveryEvidenceOperations = (*Repositories)(nil)
)
