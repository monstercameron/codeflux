package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
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
	WorkspaceID  domain.WorkspaceID
	SessionID    domain.SessionID
	TaskID       domain.TaskID
	TaskState    domain.TaskState
	Title        string
	Archived     bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
	Revision     uint64
}

// MaximumMessageBodyBytes bounds one durable thread message body.
//
// Assistant and tool text is already redaction-bounded before it reaches this
// layer, but the store is the last place an oversized body can be stopped, and
// a bound enforced only upstream is a bound a hostile or defective client can
// skip. It is sized above the task requirement bound so a legitimate long turn
// is never truncated by it.
const MaximumMessageBodyBytes = 64 << 10

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
	AttachmentIDs  []domain.ArtifactID
	IdempotencyKey string
	CreatedAt      time.Time
}

// Repositories implements all SQLite domain repository contracts.
type Repositories struct {
	database               *Database
	now                    func() time.Time
	acceptanceObservations AcceptanceObservationSource
}

// ContextManifest is one immutable, revision-bound provider-context selection.
type ContextManifest struct {
	ID                  string
	RepositoryID        domain.RepositoryID
	RepositoryRevision  string
	MapRevision         string
	RequirementSHA256   string
	SelectionPolicy     int
	MaxFiles            int
	MaxBytes            int
	MaxEstimatedTokens  int
	UsedFiles           int
	UsedBytes           int
	UsedEstimatedTokens int
	Items               []ContextManifestItem
	Exclusions          []ContextManifestExclusion
	CreatedAt           time.Time
}

// ContextManifestItem is one redacted, explainable excerpt admitted to context.
type ContextManifestItem struct {
	Path            string
	Kind            string
	StartLine       int
	EndLine         int
	ContentRedacted string
	ContentSHA256   string
	Reasons         []string
	Trust           string
	Generated       bool
	Binary          bool
	Minified        bool
	Vendor          bool
	Dependency      bool
	EstimatedTokens int
}

// ContextManifestExclusion explains why one candidate did not enter context.
type ContextManifestExclusion struct {
	Path   string
	Reason string
}

// RecordContextManifest declares one complete immutable context selection.
type RecordContextManifest struct {
	ID                  string
	RepositoryID        domain.RepositoryID
	RepositoryRevision  string
	MapRevision         string
	RequirementSHA256   string
	SelectionPolicy     int
	MaxFiles            int
	MaxBytes            int
	MaxEstimatedTokens  int
	UsedFiles           int
	UsedBytes           int
	UsedEstimatedTokens int
	Items               []ContextManifestItem
	Exclusions          []ContextManifestExclusion
}

// ContextManifestOperations groups immutable context evidence operations.
type ContextManifestOperations interface {
	RecordContextManifest(context.Context, RecordContextManifest) (ContextManifest, error)
	GetContextManifest(context.Context, string) (ContextManifest, error)
}

// WorktreeBindingState is the durable task-worktree lifecycle.
type WorktreeBindingState string

const (
	WorktreeBindingActive           WorktreeBindingState = "active"
	WorktreeBindingReleased         WorktreeBindingState = "released"
	WorktreeBindingRecoveryRequired WorktreeBindingState = "recovery-required"
)

// WorktreeBinding binds one task to one isolated Git branch and path.
type WorktreeBinding struct {
	WorkspaceID  domain.WorkspaceID
	TaskID       domain.TaskID
	RepositoryID domain.RepositoryID
	BaseRevision string
	HeadRevision string
	BranchName   string
	WorktreePath string
	State        WorktreeBindingState
	CreatedAt    time.Time
	UpdatedAt    time.Time
	Revision     uint64
}

// CreateWorktreeBinding atomically records one complete active binding.
type CreateWorktreeBinding struct {
	WorkspaceID  domain.WorkspaceID
	TaskID       domain.TaskID
	RepositoryID domain.RepositoryID
	BaseRevision string
	HeadRevision string
	BranchName   string
	WorktreePath string
}

// AdvanceWorktreeBinding declares an optimistic Codeflux-owned HEAD update.
type AdvanceWorktreeBinding struct {
	TaskID           domain.TaskID
	ExpectedRevision uint64
	ExpectedHead     string
	HeadRevision     string
}

// TransitionWorktreeBinding declares an optimistic lifecycle transition.
type TransitionWorktreeBinding struct {
	TaskID           domain.TaskID
	ExpectedRevision uint64
	From             WorktreeBindingState
	To               WorktreeBindingState
}

// WorktreeBindingOperations groups durable task-worktree binding operations.
type WorktreeBindingOperations interface {
	CreateWorktreeBinding(context.Context, CreateWorktreeBinding) (WorktreeBinding, error)
	GetWorktreeBinding(context.Context, domain.TaskID) (WorktreeBinding, error)
	ListActiveWorktreeBindings(context.Context, int) ([]WorktreeBinding, error)
	AdvanceWorktreeBinding(context.Context, AdvanceWorktreeBinding) (WorktreeBinding, error)
	TransitionWorktreeBinding(context.Context, TransitionWorktreeBinding) (WorktreeBinding, error)
	MarkWorktreeRecoveryRequired(context.Context, domain.TaskID, uint64, string) (WorktreeBinding, error)
}

// SettingsScope identifies one persisted non-secret configuration layer.
type SettingsScope string

const (
	SettingsScopeUser       SettingsScope = "user"
	SettingsScopeRepository SettingsScope = "repository"
)

// SettingsRevision is one immutable, approved non-secret configuration layer.
type SettingsRevision struct {
	Revision          uint64
	Scope             SettingsScope
	RepositoryID      *domain.RepositoryID
	ConfigurationJSON string
	ContentSHA256     string
	ApprovalReference *string
	IdempotencyKey    string
	CreatedAt         time.Time
}

// CreateSettingsRevision declares one idempotent settings snapshot.
type CreateSettingsRevision struct {
	Scope             SettingsScope
	RepositoryID      *domain.RepositoryID
	ConfigurationJSON string
	ApprovalReference *string
	IdempotencyKey    string
}

// RunConfiguration is the effective non-secret configuration bound to one run.
type RunConfiguration struct {
	RunID            domain.RunID
	SettingsRevision uint64
	EffectiveJSON    string
	SourcesJSON      string
	ContentSHA256    string
	CreatedAt        time.Time
}

// RecordRunConfiguration declares one immutable effective run snapshot.
type RecordRunConfiguration struct {
	RunID            domain.RunID
	SettingsRevision uint64
	EffectiveJSON    string
	SourcesJSON      string
}

// ConfigurationOperations groups versioned settings and effective run facts.
type ConfigurationOperations interface {
	CreateSettingsRevision(context.Context, CreateSettingsRevision) (SettingsRevision, error)
	GetSettingsRevision(context.Context, uint64) (SettingsRevision, error)
	RecordRunConfiguration(context.Context, RecordRunConfiguration) (RunConfiguration, error)
	GetRunConfiguration(context.Context, domain.RunID) (RunConfiguration, error)
}

// ProviderCredentialReference is the non-secret OS-store identity bound to a
// provider. It never contains credential material.
type ProviderCredentialReference struct {
	ProviderID      domain.ProviderID
	OpaqueReference string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	Revision        uint64
}

// BindProviderCredentialReference declares an idempotent opaque binding.
type BindProviderCredentialReference struct {
	ProviderID      domain.ProviderID
	OpaqueReference string
}

// ProviderCredentialOperations groups non-secret provider reference storage.
type ProviderCredentialOperations interface {
	BindProviderCredentialReference(context.Context, BindProviderCredentialReference) (ProviderCredentialReference, error)
	GetProviderCredentialReference(context.Context, domain.ProviderID) (ProviderCredentialReference, error)
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
	UpdateRepositoryGitIdentity(context.Context, domain.RepositoryID, string) error
}

// ConversationOperations groups thread and immutable message operations.
type ConversationOperations interface {
	CreateThread(context.Context, CreateThread) (Thread, error)
	CreateThreadCommit(context.Context, CreateThread) (ThreadCommit, error)
	ListThreads(context.Context, ListThreads) (ThreadPage, error)
	GetThread(context.Context, domain.ThreadID) (Thread, error)
	ListMessages(context.Context, ListMessages) (MessagePage, error)
	AppendMessage(context.Context, AppendMessage) (Message, error)
	AppendMessageAndDraftTask(context.Context, AppendMessageAndDraftTask) (AppendedMessageAndDraftTask, error)
	RenameThread(context.Context, RenameThread) (Thread, error)
	RenameThreadCommit(context.Context, RenameThread) (ThreadCommit, error)
	ArchiveThread(context.Context, ArchiveThread) (Thread, error)
	ArchiveThreadCommit(context.Context, ArchiveThread) (ThreadCommit, error)
}

// Session is one durable, thread-scoped ordered event stream.
type Session struct {
	ID                       domain.SessionID
	ThreadID                 domain.ThreadID
	CurrentSequence          uint64
	CompactedThroughSequence uint64
	CreatedAt                time.Time
	UpdatedAt                time.Time
}

// CreateSession declares the sole reconnectable stream for one thread.
type CreateSession struct {
	ID       domain.SessionID
	ThreadID domain.ThreadID
}

// ReplaySessionEvents declares a bounded exclusive replay query.
type ReplaySessionEvents struct {
	SessionID     domain.SessionID
	AfterSequence uint64
	Limit         int
}

// SessionReplay is the reconnect response for one requested sequence.
type SessionReplay struct {
	Snapshot *events.SessionSnapshot
	Events   []events.SessionEvent
	Boundary uint64
}

// SessionCommand identifies one idempotent UI mutation.
type SessionCommand struct {
	SessionID      domain.SessionID
	IdempotencyKey string
	RequestSHA256  string
}

// SessionCommandResult is the immutable result returned to every retry.
type SessionCommandResult struct {
	SessionID      domain.SessionID
	IdempotencyKey string
	RequestSHA256  string
	ResultJSON     string
	FinalSequence  uint64
	CreatedAt      time.Time
	Replayed       bool
}

// SessionCommandOperation performs the command effect in the command ledger's
// transaction and returns stable JSON plus its final event sequence.
type SessionCommandOperation func(*Transaction) (resultJSON string, finalSequence uint64, err error)

// SessionEventOperations groups durable append and replay operations.
type SessionEventOperations interface {
	CreateSession(context.Context, CreateSession) (Session, error)
	GetThreadSession(context.Context, domain.ThreadID) (Session, error)
	AppendSessionEvent(context.Context, *Transaction, events.NewSessionEvent) (events.SessionEvent, error)
	PersistSessionEvent(context.Context, events.NewSessionEvent) (events.SessionEvent, error)
	PersistAndPublishSessionEvent(context.Context, events.NewSessionEvent, CommittedEventPublisher) (events.SessionEvent, error)
	ReplaySessionEvents(context.Context, ReplaySessionEvents) ([]events.SessionEvent, error)
	StoreSessionSnapshot(context.Context, events.SessionSnapshot) error
	ReplaySession(context.Context, ReplaySessionEvents) (SessionReplay, error)
	ExecuteSessionCommand(context.Context, SessionCommand, SessionCommandOperation) (SessionCommandResult, error)
	CurrentSessionSequence(context.Context, domain.SessionID) (uint64, error)
}

// CommittedEventPublisher accepts only events returned after commit.
type CommittedEventPublisher interface {
	PublishCommitted(events.SessionEvent) error
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
	ID             domain.ThreadID
	SessionID      domain.SessionID
	ProjectID      domain.ProjectID
	RepositoryID   domain.RepositoryID
	WorkspaceID    domain.WorkspaceID
	Title          string
	IdempotencyKey string
}

// ThreadCommit carries correctness-bearing events committed atomically with a
// thread command. Events is empty for an idempotent replay.
type ThreadCommit struct {
	Thread Thread
	Events []events.SessionEvent
}

// ThreadCursor is an exclusive stable cursor over descending update order.
type ThreadCursor struct {
	UpdatedAt time.Time
	ID        domain.ThreadID
}

// ListThreads declares bounded repository thread pagination.
type ListThreads struct {
	RepositoryID    domain.RepositoryID
	WorkspaceID     domain.WorkspaceID
	Before          *ThreadCursor
	Limit           int
	IncludeArchived bool
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
	AttachmentIDs  []domain.ArtifactID
	IdempotencyKey string
}

// AppendMessageAndDraftTask declares one message and optional draft task that
// must commit or roll back as a single durable command.
type AppendMessageAndDraftTask struct {
	Message          AppendMessage
	ExpectedRevision uint64
	DraftTask        *CreateTask
}

// AppendedMessageAndDraftTask is the exact idempotent command result.
type AppendedMessageAndDraftTask struct {
	Message   Message
	DraftTask *Task
	Events    []events.SessionEvent
}

// MessageCursor is an exclusive cursor over descending durable sequence.
type MessageCursor struct{ BeforeSequence uint64 }

// ListMessages declares bounded newest-first thread pagination.
type ListMessages struct {
	ThreadID domain.ThreadID
	Before   *MessageCursor
	Limit    int
}

// MessagePage carries one newest-first page and its exclusive continuation.
type MessagePage struct {
	Messages []Message
	Next     *MessageCursor
}

// RenameThread declares one optimistic, idempotent title mutation.
type RenameThread struct {
	ThreadID         domain.ThreadID
	ExpectedRevision uint64
	Title            string
	IdempotencyKey   string
}

// ArchiveThread declares one optimistic, idempotent archive-state mutation.
type ArchiveThread struct {
	ThreadID         domain.ThreadID
	ExpectedRevision uint64
	Archived         bool
	IdempotencyKey   string
}

// WorkspaceScope is the durable repository authority of one workspace.
type WorkspaceScope struct {
	WorkspaceID   domain.WorkspaceID
	RepositoryID  domain.RepositoryID
	ProjectID     domain.ProjectID
	CanonicalPath string
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
	SettingsRevision  uint64
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
	GetCheckpoint(context.Context, domain.CheckpointID) (Checkpoint, error)
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
	_ SessionEventOperations     = (*Repositories)(nil)
	_ TaskOperations             = (*Repositories)(nil)
	_ ApprovalBudgetOperations   = (*Repositories)(nil)
	_ RecoveryEvidenceOperations = (*Repositories)(nil)
	_ ContextManifestOperations  = (*Repositories)(nil)
	_ WorktreeBindingOperations  = (*Repositories)(nil)
)

// CountRowsForTest reports how many rows one table holds.
//
// It exists so a test can assert on what was actually persisted rather than on
// what a service said it persisted. The table name is checked against the
// schema rather than interpolated blindly, because a name reaching a query
// unchecked is an injection whether or not the caller is a test.
func (repositories *Repositories) CountRowsForTest(table string) (int, error) {
	if repositories == nil || repositories.database == nil {
		return 0, errors.New("repositories are unavailable")
	}
	var known string
	if err := repositories.database.sql.QueryRow(
		"SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?", table,
	).Scan(&known); err != nil {
		return 0, fmt.Errorf("no such table: %s", table)
	}
	var count int
	if err := repositories.database.sql.QueryRow(
		"SELECT COUNT(*) FROM " + known,
	).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}
