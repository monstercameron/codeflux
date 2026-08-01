package domain

import (
	"crypto/rand"
	"database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// ErrInvalidID classifies malformed, empty, or wrong-kind stable identities.
var ErrInvalidID = errors.New("invalid stable identity")

// IDParseError describes why a stable identity was rejected.
type IDParseError struct {
	Kind   string
	Value  string
	Reason string
}

func (e *IDParseError) Error() string {
	return fmt.Sprintf("%s ID %q is invalid: %s", e.Kind, e.Value, e.Reason)
}

// Unwrap allows errors.Is(err, ErrInvalidID).
func (e *IDParseError) Unwrap() error {
	return ErrInvalidID
}

type idKind interface {
	prefix() string
	label() string
}

type typedID[K idKind] struct {
	value string
}

func (id typedID[K]) String() string {
	return id.value
}

// IsZero reports whether no validated identity has been assigned.
func (id typedID[K]) IsZero() bool {
	return id.value == ""
}

func (id typedID[K]) MarshalText() ([]byte, error) {
	if _, err := parseTypedID[K](id.value); err != nil {
		return nil, err
	}
	return []byte(id.value), nil
}

func (id *typedID[K]) UnmarshalText(text []byte) error {
	parsed, err := parseTypedID[K](string(text))
	if err != nil {
		*id = typedID[K]{}
		return err
	}
	*id = parsed
	return nil
}

func (id typedID[K]) MarshalJSON() ([]byte, error) {
	if _, err := parseTypedID[K](id.value); err != nil {
		return nil, err
	}
	return json.Marshal(id.value)
}

func (id *typedID[K]) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		var kind K
		*id = typedID[K]{}
		return &IDParseError{
			Kind:   kind.label(),
			Value:  string(data),
			Reason: "JSON value must be a non-null string",
		}
	}
	return id.UnmarshalText([]byte(raw))
}

func (id typedID[K]) Value() (driver.Value, error) {
	if _, err := parseTypedID[K](id.value); err != nil {
		return nil, err
	}
	return id.value, nil
}

func (id *typedID[K]) Scan(source any) error {
	var raw string
	switch value := source.(type) {
	case string:
		raw = value
	case []byte:
		raw = string(value)
	case nil:
		var kind K
		*id = typedID[K]{}
		return &IDParseError{
			Kind:   kind.label(),
			Reason: "SQL value must not be null",
		}
	default:
		var kind K
		*id = typedID[K]{}
		return &IDParseError{
			Kind:   kind.label(),
			Value:  fmt.Sprintf("%T", source),
			Reason: "SQL value must be text",
		}
	}
	return id.UnmarshalText([]byte(raw))
}

func parseTypedID[K idKind](raw string) (typedID[K], error) {
	var kind K
	if raw == "" {
		return typedID[K]{}, &IDParseError{
			Kind:   kind.label(),
			Reason: "value must not be empty",
		}
	}
	prefix := kind.prefix() + "_"
	if !strings.HasPrefix(raw, prefix) {
		return typedID[K]{}, &IDParseError{
			Kind:   kind.label(),
			Value:  raw,
			Reason: "wrong type prefix",
		}
	}
	if err := validateUUIDv7(raw[len(prefix):]); err != nil {
		return typedID[K]{}, &IDParseError{
			Kind:   kind.label(),
			Value:  raw,
			Reason: err.Error(),
		}
	}
	return typedID[K]{value: raw}, nil
}

func newTypedID[K idKind]() (typedID[K], error) {
	value, err := newUUIDv7(time.Now(), rand.Reader)
	if err != nil {
		return typedID[K]{}, err
	}
	var kind K
	return parseTypedID[K](kind.prefix() + "_" + value)
}

func newUUIDv7(now time.Time, entropy io.Reader) (string, error) {
	milliseconds := now.UnixMilli()
	if milliseconds < 0 || milliseconds > 1<<48-1 {
		return "", fmt.Errorf("UUIDv7 timestamp is outside the 48-bit range")
	}

	var value [16]byte
	value[0] = byte(milliseconds >> 40)
	value[1] = byte(milliseconds >> 32)
	value[2] = byte(milliseconds >> 24)
	value[3] = byte(milliseconds >> 16)
	value[4] = byte(milliseconds >> 8)
	value[5] = byte(milliseconds)
	if _, err := io.ReadFull(entropy, value[6:]); err != nil {
		return "", fmt.Errorf("read UUIDv7 entropy: %w", err)
	}
	value[6] = value[6]&0x0f | 0x70
	value[8] = value[8]&0x3f | 0x80

	var encoded [32]byte
	hex.Encode(encoded[:], value[:])
	return string(encoded[0:8]) + "-" +
		string(encoded[8:12]) + "-" +
		string(encoded[12:16]) + "-" +
		string(encoded[16:20]) + "-" +
		string(encoded[20:32]), nil
}

func validateUUIDv7(value string) error {
	if len(value) != 36 {
		return errors.New("UUIDv7 payload must contain 36 characters")
	}
	for index, character := range value {
		switch index {
		case 8, 13, 18, 23:
			if character != '-' {
				return errors.New("UUIDv7 payload has misplaced separators")
			}
		default:
			if !isLowerHex(character) {
				return errors.New("UUIDv7 payload must use canonical lowercase hexadecimal")
			}
		}
	}
	if value[14] != '7' {
		return errors.New("UUID payload is not version 7")
	}
	switch value[19] {
	case '8', '9', 'a', 'b':
	default:
		return errors.New("UUIDv7 payload has an invalid RFC 9562 variant")
	}
	return nil
}

func isLowerHex(character rune) bool {
	return character >= '0' && character <= '9' ||
		character >= 'a' && character <= 'f'
}

type projectIDKind struct{}

func (projectIDKind) prefix() string { return "prj" }
func (projectIDKind) label() string  { return "project" }

// ProjectID identifies one Codeflux project.
type ProjectID struct{ typedID[projectIDKind] }

// ParseProjectID parses a canonical project identity.
func ParseProjectID(raw string) (ProjectID, error) {
	value, err := parseTypedID[projectIDKind](raw)
	return ProjectID{value}, err
}

// NewProjectID generates a locally unique, time-sortable project identity.
func NewProjectID() (ProjectID, error) {
	value, err := newTypedID[projectIDKind]()
	return ProjectID{value}, err
}

type repositoryIDKind struct{}

func (repositoryIDKind) prefix() string { return "repo" }
func (repositoryIDKind) label() string  { return "repository" }

// RepositoryID identifies one source repository.
type RepositoryID struct{ typedID[repositoryIDKind] }

// ParseRepositoryID parses a canonical repository identity.
func ParseRepositoryID(raw string) (RepositoryID, error) {
	value, err := parseTypedID[repositoryIDKind](raw)
	return RepositoryID{value}, err
}

// NewRepositoryID generates a locally unique, time-sortable repository identity.
func NewRepositoryID() (RepositoryID, error) {
	value, err := newTypedID[repositoryIDKind]()
	return RepositoryID{value}, err
}

type workspaceIDKind struct{}

func (workspaceIDKind) prefix() string { return "wsp" }
func (workspaceIDKind) label() string  { return "workspace" }

// WorkspaceID identifies one repository workspace.
type WorkspaceID struct{ typedID[workspaceIDKind] }

// ParseWorkspaceID parses a canonical workspace identity.
func ParseWorkspaceID(raw string) (WorkspaceID, error) {
	value, err := parseTypedID[workspaceIDKind](raw)
	return WorkspaceID{value}, err
}

// NewWorkspaceID generates a locally unique, time-sortable workspace identity.
func NewWorkspaceID() (WorkspaceID, error) {
	value, err := newTypedID[workspaceIDKind]()
	return WorkspaceID{value}, err
}

type sessionIDKind struct{}

func (sessionIDKind) prefix() string { return "ses" }
func (sessionIDKind) label() string  { return "session" }

// SessionID identifies one ordered reconnectable event stream.
type SessionID struct{ typedID[sessionIDKind] }

// ParseSessionID parses a canonical session identity.
func ParseSessionID(raw string) (SessionID, error) {
	value, err := parseTypedID[sessionIDKind](raw)
	return SessionID{value}, err
}

// NewSessionID generates a locally unique, time-sortable session identity.
func NewSessionID() (SessionID, error) {
	value, err := newTypedID[sessionIDKind]()
	return SessionID{value}, err
}

type threadIDKind struct{}

func (threadIDKind) prefix() string { return "thr" }
func (threadIDKind) label() string  { return "thread" }

// ThreadID identifies one conversation thread.
type ThreadID struct{ typedID[threadIDKind] }

// ParseThreadID parses a canonical thread identity.
func ParseThreadID(raw string) (ThreadID, error) {
	value, err := parseTypedID[threadIDKind](raw)
	return ThreadID{value}, err
}

// NewThreadID generates a locally unique, time-sortable thread identity.
func NewThreadID() (ThreadID, error) {
	value, err := newTypedID[threadIDKind]()
	return ThreadID{value}, err
}

type messageIDKind struct{}

func (messageIDKind) prefix() string { return "msg" }
func (messageIDKind) label() string  { return "message" }

// MessageID identifies one thread message.
type MessageID struct{ typedID[messageIDKind] }

// ParseMessageID parses a canonical message identity.
func ParseMessageID(raw string) (MessageID, error) {
	value, err := parseTypedID[messageIDKind](raw)
	return MessageID{value}, err
}

// NewMessageID generates a locally unique, time-sortable message identity.
func NewMessageID() (MessageID, error) {
	value, err := newTypedID[messageIDKind]()
	return MessageID{value}, err
}

type taskIDKind struct{}

func (taskIDKind) prefix() string { return "tsk" }
func (taskIDKind) label() string  { return "task" }

// TaskID identifies one durable task.
type TaskID struct{ typedID[taskIDKind] }

// ParseTaskID parses a canonical task identity.
func ParseTaskID(raw string) (TaskID, error) {
	value, err := parseTypedID[taskIDKind](raw)
	return TaskID{value}, err
}

// NewTaskID generates a locally unique, time-sortable task identity.
func NewTaskID() (TaskID, error) {
	value, err := newTypedID[taskIDKind]()
	return TaskID{value}, err
}

type runIDKind struct{}

func (runIDKind) prefix() string { return "run" }
func (runIDKind) label() string  { return "run" }

// RunID identifies one task execution attempt.
type RunID struct{ typedID[runIDKind] }

// ParseRunID parses a canonical run identity.
func ParseRunID(raw string) (RunID, error) {
	value, err := parseTypedID[runIDKind](raw)
	return RunID{value}, err
}

// NewRunID generates a locally unique, time-sortable run identity.
func NewRunID() (RunID, error) {
	value, err := newTypedID[runIDKind]()
	return RunID{value}, err
}

type eventIDKind struct{}

func (eventIDKind) prefix() string { return "evt" }
func (eventIDKind) label() string  { return "event" }

// EventID identifies one durable event.
type EventID struct{ typedID[eventIDKind] }

// ParseEventID parses a canonical event identity.
func ParseEventID(raw string) (EventID, error) {
	value, err := parseTypedID[eventIDKind](raw)
	return EventID{value}, err
}

// NewEventID generates a locally unique, time-sortable event identity.
func NewEventID() (EventID, error) {
	value, err := newTypedID[eventIDKind]()
	return EventID{value}, err
}

type checkpointIDKind struct{}

func (checkpointIDKind) prefix() string { return "ckp" }
func (checkpointIDKind) label() string  { return "checkpoint" }

// CheckpointID identifies one durable recovery checkpoint.
type CheckpointID struct{ typedID[checkpointIDKind] }

// ParseCheckpointID parses a canonical checkpoint identity.
func ParseCheckpointID(raw string) (CheckpointID, error) {
	value, err := parseTypedID[checkpointIDKind](raw)
	return CheckpointID{value}, err
}

// NewCheckpointID generates a locally unique, time-sortable checkpoint identity.
func NewCheckpointID() (CheckpointID, error) {
	value, err := newTypedID[checkpointIDKind]()
	return CheckpointID{value}, err
}

type approvalIDKind struct{}

func (approvalIDKind) prefix() string { return "apr" }
func (approvalIDKind) label() string  { return "approval" }

// ApprovalID identifies one authority request.
type ApprovalID struct{ typedID[approvalIDKind] }

// ParseApprovalID parses a canonical approval identity.
func ParseApprovalID(raw string) (ApprovalID, error) {
	value, err := parseTypedID[approvalIDKind](raw)
	return ApprovalID{value}, err
}

// NewApprovalID generates a locally unique, time-sortable approval identity.
func NewApprovalID() (ApprovalID, error) {
	value, err := newTypedID[approvalIDKind]()
	return ApprovalID{value}, err
}

type graphIDKind struct{}

func (graphIDKind) prefix() string { return "grf" }
func (graphIDKind) label() string  { return "graph" }

// GraphID identifies one task-scoped graph.
type GraphID struct{ typedID[graphIDKind] }

// ParseGraphID parses a canonical graph identity.
func ParseGraphID(raw string) (GraphID, error) {
	value, err := parseTypedID[graphIDKind](raw)
	return GraphID{value}, err
}

// NewGraphID generates a locally unique, time-sortable graph identity.
func NewGraphID() (GraphID, error) {
	value, err := newTypedID[graphIDKind]()
	return GraphID{value}, err
}

type graphRevisionIDKind struct{}

func (graphRevisionIDKind) prefix() string { return "grv" }
func (graphRevisionIDKind) label() string  { return "graph revision" }

// GraphRevisionID identifies one immutable graph revision.
type GraphRevisionID struct{ typedID[graphRevisionIDKind] }

// ParseGraphRevisionID parses a canonical graph-revision identity.
func ParseGraphRevisionID(raw string) (GraphRevisionID, error) {
	value, err := parseTypedID[graphRevisionIDKind](raw)
	return GraphRevisionID{value}, err
}

// NewGraphRevisionID generates a locally unique, time-sortable graph-revision identity.
func NewGraphRevisionID() (GraphRevisionID, error) {
	value, err := newTypedID[graphRevisionIDKind]()
	return GraphRevisionID{value}, err
}

type nodeIDKind struct{}

func (nodeIDKind) prefix() string { return "nod" }
func (nodeIDKind) label() string  { return "node" }

// NodeID identifies one stable graph node.
type NodeID struct{ typedID[nodeIDKind] }

// ParseNodeID parses a canonical node identity.
func ParseNodeID(raw string) (NodeID, error) {
	value, err := parseTypedID[nodeIDKind](raw)
	return NodeID{value}, err
}

// NewNodeID generates a locally unique, time-sortable node identity.
func NewNodeID() (NodeID, error) {
	value, err := newTypedID[nodeIDKind]()
	return NodeID{value}, err
}

type edgeIDKind struct{}

func (edgeIDKind) prefix() string { return "edg" }
func (edgeIDKind) label() string  { return "edge" }

// EdgeID identifies one stable graph edge.
type EdgeID struct{ typedID[edgeIDKind] }

// ParseEdgeID parses a canonical edge identity.
func ParseEdgeID(raw string) (EdgeID, error) {
	value, err := parseTypedID[edgeIDKind](raw)
	return EdgeID{value}, err
}

// NewEdgeID generates a locally unique, time-sortable edge identity.
func NewEdgeID() (EdgeID, error) {
	value, err := newTypedID[edgeIDKind]()
	return EdgeID{value}, err
}

type validationIDKind struct{}

func (validationIDKind) prefix() string { return "val" }
func (validationIDKind) label() string  { return "validation" }

// ValidationID identifies one validation execution.
type ValidationID struct{ typedID[validationIDKind] }

// ParseValidationID parses a canonical validation identity.
func ParseValidationID(raw string) (ValidationID, error) {
	value, err := parseTypedID[validationIDKind](raw)
	return ValidationID{value}, err
}

// NewValidationID generates a locally unique, time-sortable validation identity.
func NewValidationID() (ValidationID, error) {
	value, err := newTypedID[validationIDKind]()
	return ValidationID{value}, err
}

type evidenceIDKind struct{}

func (evidenceIDKind) prefix() string { return "evd" }
func (evidenceIDKind) label() string  { return "evidence" }

// EvidenceID identifies one evidence item.
type EvidenceID struct{ typedID[evidenceIDKind] }

// ParseEvidenceID parses a canonical evidence identity.
func ParseEvidenceID(raw string) (EvidenceID, error) {
	value, err := parseTypedID[evidenceIDKind](raw)
	return EvidenceID{value}, err
}

// NewEvidenceID generates a locally unique, time-sortable evidence identity.
func NewEvidenceID() (EvidenceID, error) {
	value, err := newTypedID[evidenceIDKind]()
	return EvidenceID{value}, err
}

type artifactIDKind struct{}

func (artifactIDKind) prefix() string { return "art" }
func (artifactIDKind) label() string  { return "artifact" }

// ArtifactID identifies one reusable or diagnostic artifact.
type ArtifactID struct{ typedID[artifactIDKind] }

// ParseArtifactID parses a canonical artifact identity.
func ParseArtifactID(raw string) (ArtifactID, error) {
	value, err := parseTypedID[artifactIDKind](raw)
	return ArtifactID{value}, err
}

// NewArtifactID generates a locally unique, time-sortable artifact identity.
func NewArtifactID() (ArtifactID, error) {
	value, err := newTypedID[artifactIDKind]()
	return ArtifactID{value}, err
}

type atomIDKind struct{}

func (atomIDKind) prefix() string { return "atm" }
func (atomIDKind) label() string  { return "atom" }

// AtomID identifies one reusable atom across immutable versions.
type AtomID struct{ typedID[atomIDKind] }

// ParseAtomID parses a canonical atom identity.
func ParseAtomID(raw string) (AtomID, error) {
	value, err := parseTypedID[atomIDKind](raw)
	return AtomID{value}, err
}

// NewAtomID generates a locally unique, time-sortable atom identity.
func NewAtomID() (AtomID, error) {
	value, err := newTypedID[atomIDKind]()
	return AtomID{value}, err
}

type modelRequestIDKind struct{}

func (modelRequestIDKind) prefix() string { return "mrq" }
func (modelRequestIDKind) label() string  { return "model request" }

// ModelRequestID identifies one logical provider request.
type ModelRequestID struct{ typedID[modelRequestIDKind] }

// ParseModelRequestID parses a canonical model-request identity.
func ParseModelRequestID(raw string) (ModelRequestID, error) {
	value, err := parseTypedID[modelRequestIDKind](raw)
	return ModelRequestID{value}, err
}

// NewModelRequestID generates a locally unique, time-sortable model-request identity.
func NewModelRequestID() (ModelRequestID, error) {
	value, err := newTypedID[modelRequestIDKind]()
	return ModelRequestID{value}, err
}

type providerIDKind struct{}

func (providerIDKind) prefix() string { return "prv" }
func (providerIDKind) label() string  { return "provider" }

// ProviderID identifies one provider configuration without containing credentials.
type ProviderID struct{ typedID[providerIDKind] }

// ParseProviderID parses a canonical provider identity.
func ParseProviderID(raw string) (ProviderID, error) {
	value, err := parseTypedID[providerIDKind](raw)
	return ProviderID{value}, err
}

// NewProviderID generates a locally unique, time-sortable provider identity.
func NewProviderID() (ProviderID, error) {
	value, err := newTypedID[providerIDKind]()
	return ProviderID{value}, err
}

type budgetIDKind struct{}

func (budgetIDKind) prefix() string { return "bdg" }
func (budgetIDKind) label() string  { return "budget" }

// BudgetID identifies one task or request budget.
type BudgetID struct{ typedID[budgetIDKind] }

// ParseBudgetID parses a canonical budget identity.
func ParseBudgetID(raw string) (BudgetID, error) {
	value, err := parseTypedID[budgetIDKind](raw)
	return BudgetID{value}, err
}

// NewBudgetID generates a locally unique, time-sortable budget identity.
func NewBudgetID() (BudgetID, error) {
	value, err := newTypedID[budgetIDKind]()
	return BudgetID{value}, err
}

type episodeIDKind struct{}

func (episodeIDKind) prefix() string { return "epi" }
func (episodeIDKind) label() string  { return "episode" }

// EpisodeID identifies one immutable chronological task-history record. An
// episode is raw fact: it exists independently of whether any project-memory
// artifact ever earns authority from it. See MemoryArtifactID and
// MaturityState for the distinct, revocable authority concept built on top
// of episodes.
type EpisodeID struct{ typedID[episodeIDKind] }

// ParseEpisodeID parses a canonical episode identity.
func ParseEpisodeID(raw string) (EpisodeID, error) {
	value, err := parseTypedID[episodeIDKind](raw)
	return EpisodeID{value}, err
}

// NewEpisodeID generates a locally unique, time-sortable episode identity.
func NewEpisodeID() (EpisodeID, error) {
	value, err := newTypedID[episodeIDKind]()
	return EpisodeID{value}, err
}

type memoryArtifactIDKind struct{}

func (memoryArtifactIDKind) prefix() string { return "mem" }
func (memoryArtifactIDKind) label() string  { return "memory artifact" }

// MemoryArtifactID identifies one project-memory artifact across every
// immutable revision and maturity change it ever undergoes. The identity is
// stable; authority is not. See MaturityState.
type MemoryArtifactID struct{ typedID[memoryArtifactIDKind] }

// ParseMemoryArtifactID parses a canonical memory-artifact identity.
func ParseMemoryArtifactID(raw string) (MemoryArtifactID, error) {
	value, err := parseTypedID[memoryArtifactIDKind](raw)
	return MemoryArtifactID{value}, err
}

// NewMemoryArtifactID generates a locally unique, time-sortable memory-artifact identity.
func NewMemoryArtifactID() (MemoryArtifactID, error) {
	value, err := newTypedID[memoryArtifactIDKind]()
	return MemoryArtifactID{value}, err
}

type memoryArtifactRevisionIDKind struct{}

func (memoryArtifactRevisionIDKind) prefix() string { return "mrv" }
func (memoryArtifactRevisionIDKind) label() string  { return "memory artifact revision" }

// MemoryArtifactRevisionID identifies one immutable revision of a memory
// artifact. A user correction always creates a new revision identity; it
// never mutates an existing one.
type MemoryArtifactRevisionID struct {
	typedID[memoryArtifactRevisionIDKind]
}

// ParseMemoryArtifactRevisionID parses a canonical memory-artifact-revision identity.
func ParseMemoryArtifactRevisionID(raw string) (MemoryArtifactRevisionID, error) {
	value, err := parseTypedID[memoryArtifactRevisionIDKind](raw)
	return MemoryArtifactRevisionID{value}, err
}

// NewMemoryArtifactRevisionID generates a locally unique, time-sortable memory-artifact-revision identity.
func NewMemoryArtifactRevisionID() (MemoryArtifactRevisionID, error) {
	value, err := newTypedID[memoryArtifactRevisionIDKind]()
	return MemoryArtifactRevisionID{value}, err
}
