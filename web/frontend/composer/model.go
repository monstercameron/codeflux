// Package composer owns immutable per-thread composer state and presentation policy.
package composer

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"codeflux.dev/codeflux/internal/domain"
)

const (
	maxDraftBytes        = 64 * 1024
	maxAttachments       = 32
	maxDisplayLabelBytes = 512
	maxModelNameBytes    = 256
	maxRevisionBytes     = 256
	maxSafeMessageBytes  = 512
)

var (
	ErrInvalidComposerValue = errors.New("invalid composer value")
	ErrComposerBusy         = errors.New("composer send is pending")
	ErrSendNotRetryable     = errors.New("composer send is not retryable")
	ErrSendAttemptMismatch  = errors.New("composer send attempt does not match")
)

type ValueError struct {
	Field  string
	Reason string
}

func (e *ValueError) Error() string {
	return fmt.Sprintf("%s is invalid: %s", e.Field, e.Reason)
}

func (e *ValueError) Unwrap() error { return ErrInvalidComposerValue }

type AttachmentKind string

const (
	AttachmentFile   AttachmentKind = "file"
	AttachmentSymbol AttachmentKind = "symbol"
)

// RepositoryAttachment is a server-resolved repository resource. File
// attachments carry an ArtifactID and symbol attachments carry an AtomID;
// browser paths are deliberately not represented.
type RepositoryAttachment struct {
	repositoryID domain.RepositoryID
	kind         AttachmentKind
	artifactID   domain.ArtifactID
	atomID       domain.AtomID
	displayLabel string
}

func NewFileAttachment(
	repositoryID domain.RepositoryID,
	artifactID domain.ArtifactID,
	displayLabel string,
) (RepositoryAttachment, error) {
	attachment := RepositoryAttachment{
		repositoryID: repositoryID,
		kind:         AttachmentFile,
		artifactID:   artifactID,
		displayLabel: strings.TrimSpace(displayLabel),
	}
	return attachment, attachment.Validate()
}

func NewSymbolAttachment(
	repositoryID domain.RepositoryID,
	atomID domain.AtomID,
	displayLabel string,
) (RepositoryAttachment, error) {
	attachment := RepositoryAttachment{
		repositoryID: repositoryID,
		kind:         AttachmentSymbol,
		atomID:       atomID,
		displayLabel: strings.TrimSpace(displayLabel),
	}
	return attachment, attachment.Validate()
}

func (a RepositoryAttachment) Validate() error {
	switch {
	case a.repositoryID.IsZero():
		return valueError("attachment.repository_id", "must be a server-issued repository identity")
	case a.kind != AttachmentFile && a.kind != AttachmentSymbol:
		return valueError("attachment.kind", "must be file or symbol")
	case a.displayLabel == "" || len(a.displayLabel) > maxDisplayLabelBytes:
		return valueError("attachment.display_label", "must be non-empty and bounded")
	case a.kind == AttachmentFile && (a.artifactID.IsZero() || !a.atomID.IsZero()):
		return valueError("attachment.identity", "file requires only a server-issued artifact identity")
	case a.kind == AttachmentSymbol && (a.atomID.IsZero() || !a.artifactID.IsZero()):
		return valueError("attachment.identity", "symbol requires only a server-issued atom identity")
	default:
		return nil
	}
}

func (a RepositoryAttachment) RepositoryID() domain.RepositoryID { return a.repositoryID }
func (a RepositoryAttachment) Kind() AttachmentKind              { return a.kind }
func (a RepositoryAttachment) DisplayLabel() string              { return a.displayLabel }

func (a RepositoryAttachment) Identity() string {
	if a.kind == AttachmentFile {
		return a.artifactID.String()
	}
	return a.atomID.String()
}

func (a RepositoryAttachment) Key() string {
	return string(a.kind) + ":" + a.Identity()
}

type ModelOverride struct {
	providerID domain.ProviderID
	model      string
	revision   string
}

func NewModelOverride(
	providerID domain.ProviderID,
	model string,
	revision string,
) (ModelOverride, error) {
	value := ModelOverride{
		providerID: providerID,
		model:      strings.TrimSpace(model),
		revision:   strings.TrimSpace(revision),
	}
	return value, value.Validate()
}

func (m ModelOverride) Validate() error {
	switch {
	case m.providerID.IsZero():
		return valueError("model_override.provider_id", "must be a server-issued provider identity")
	case m.model == "" || len(m.model) > maxModelNameBytes:
		return valueError("model_override.model", "must be non-empty and bounded")
	case m.revision == "" || len(m.revision) > maxRevisionBytes:
		return valueError("model_override.revision", "must be non-empty and bounded")
	default:
		return nil
	}
}

func (m ModelOverride) ProviderID() domain.ProviderID { return m.providerID }
func (m ModelOverride) Model() string                 { return m.model }
func (m ModelOverride) Revision() string              { return m.revision }
func (m ModelOverride) Key() string {
	return m.providerID.String() + "/" + m.model + "/" + m.revision
}

// Draft is an immutable per-thread local draft. Accessors clone slice-backed
// values so a caller cannot mutate retained composer state.
type Draft struct {
	text        string
	attachments []RepositoryAttachment
	policy      domain.PolicyPreset
	hasPolicy   bool
	budget      domain.Money
	hasBudget   bool
	model       ModelOverride
	hasModel    bool
	effort      domain.ReasoningEffort
	hasEffort   bool
}

func (d Draft) Text() string                                   { return d.text }
func (d Draft) Attachments() []RepositoryAttachment            { return slices.Clone(d.attachments) }
func (d Draft) PolicyOverride() (domain.PolicyPreset, bool)    { return d.policy, d.hasPolicy }
func (d Draft) BudgetOverride() (domain.Money, bool)           { return d.budget, d.hasBudget }
func (d Draft) ModelOverride() (ModelOverride, bool)           { return d.model, d.hasModel }
func (d Draft) EffortOverride() (domain.ReasoningEffort, bool) { return d.effort, d.hasEffort }

func (d Draft) IsZero() bool {
	return d.text == "" && len(d.attachments) == 0 && !d.hasPolicy &&
		!d.hasBudget && !d.hasModel && !d.hasEffort
}

func (d Draft) CanSubmit() bool { return strings.TrimSpace(d.text) != "" }

type IdempotencyKey string

func NewIdempotencyKey() (IdempotencyKey, error) {
	return generateIdempotencyKey(rand.Reader)
}

func generateIdempotencyKey(reader io.Reader) (IdempotencyKey, error) {
	var entropy [16]byte
	if _, err := io.ReadFull(reader, entropy[:]); err != nil {
		return "", fmt.Errorf("generate composer idempotency key: %w", err)
	}
	return IdempotencyKey("send-" + hex.EncodeToString(entropy[:])), nil
}

func ParseIdempotencyKey(raw string) (IdempotencyKey, error) {
	if len(raw) != len("send-")+32 || !strings.HasPrefix(raw, "send-") {
		return "", valueError("idempotency_key", "must use the canonical send identity format")
	}
	if _, err := hex.DecodeString(strings.TrimPrefix(raw, "send-")); err != nil {
		return "", valueError("idempotency_key", "must contain lowercase hexadecimal entropy")
	}
	if raw != strings.ToLower(raw) {
		return "", valueError("idempotency_key", "must be lowercase")
	}
	return IdempotencyKey(raw), nil
}

type SendStatus string

const (
	SendIdle                 SendStatus = "idle"
	SendPending              SendStatus = "pending"
	SendAwaitingConfirmation SendStatus = "awaiting-confirmation"
	SendFailed               SendStatus = "failed"
)

type SendAttempt struct {
	key         IdempotencyKey
	request     Draft
	status      SendStatus
	retryable   bool
	safeMessage string
	messageID   domain.MessageID
}

func (a SendAttempt) Key() IdempotencyKey         { return a.key }
func (a SendAttempt) Request() Draft              { return cloneDraft(a.request) }
func (a SendAttempt) Status() SendStatus          { return a.status }
func (a SendAttempt) Retryable() bool             { return a.retryable }
func (a SendAttempt) SafeMessage() string         { return a.safeMessage }
func (a SendAttempt) MessageID() domain.MessageID { return a.messageID }

type ThreadBinding struct {
	ThreadID     domain.ThreadID
	RepositoryID domain.RepositoryID
}

// Model is an immutable aggregate of local drafts and transient send attempts.
// It contains no browser persistence or durable task state.
type Model struct {
	repositories map[string]domain.RepositoryID
	drafts       map[string]Draft
	attempts     map[string]SendAttempt
}

func NewModel(bindings ...ThreadBinding) (Model, error) {
	model := Model{
		repositories: make(map[string]domain.RepositoryID, len(bindings)),
		drafts:       map[string]Draft{},
		attempts:     map[string]SendAttempt{},
	}
	for _, binding := range bindings {
		if binding.ThreadID.IsZero() || binding.RepositoryID.IsZero() {
			return Model{}, valueError("thread_binding", "requires server-issued thread and repository identities")
		}
		threadKey := binding.ThreadID.String()
		if current, exists := model.repositories[threadKey]; exists && current != binding.RepositoryID {
			return Model{}, valueError(
				"thread_binding.repository_id",
				"cannot bind one thread identity to multiple repositories",
			)
		}
		model.repositories[threadKey] = binding.RepositoryID
	}
	return model, nil
}

func (m Model) Draft(threadID domain.ThreadID) Draft {
	return cloneDraft(m.drafts[threadID.String()])
}

func (m Model) Attempt(threadID domain.ThreadID) (SendAttempt, bool) {
	attempt, ok := m.attempts[threadID.String()]
	attempt.request = cloneDraft(attempt.request)
	return attempt, ok
}

func (m Model) CanSubmit(threadID domain.ThreadID) bool {
	draft := m.drafts[threadID.String()]
	attempt, exists := m.attempts[threadID.String()]
	return draft.CanSubmit() && (!exists || !attempt.blocksEditing())
}

func (a SendAttempt) blocksEditing() bool {
	return a.status == SendPending || a.status == SendAwaitingConfirmation
}

func cloneDraft(draft Draft) Draft {
	draft.attachments = slices.Clone(draft.attachments)
	return draft
}

func cloneModel(model Model) Model {
	next := Model{
		repositories: make(map[string]domain.RepositoryID, len(model.repositories)),
		drafts:       make(map[string]Draft, len(model.drafts)),
		attempts:     make(map[string]SendAttempt, len(model.attempts)),
	}
	for key, value := range model.repositories {
		next.repositories[key] = value
	}
	for key, value := range model.drafts {
		next.drafts[key] = cloneDraft(value)
	}
	for key, value := range model.attempts {
		value.request = cloneDraft(value.request)
		next.attempts[key] = value
	}
	return next
}

func valueError(field, reason string) error {
	return &ValueError{Field: field, Reason: reason}
}
