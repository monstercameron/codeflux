package composer

import (
	"errors"
	"slices"
	"strings"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
)

// Action is a closed set of deterministic, browser-local composer changes.
type Action interface{ composerAction() }

type ThreadBound struct {
	ThreadID     domain.ThreadID
	RepositoryID domain.RepositoryID
}

func (ThreadBound) composerAction() {}

type DraftTextChanged struct {
	ThreadID domain.ThreadID
	Text     string
}

func (DraftTextChanged) composerAction() {}

type AttachmentAdded struct {
	ThreadID   domain.ThreadID
	Attachment RepositoryAttachment
}

func (AttachmentAdded) composerAction() {}

type AttachmentRemoved struct {
	ThreadID      domain.ThreadID
	AttachmentKey string
}

func (AttachmentRemoved) composerAction() {}

type PolicyOverrideChanged struct {
	ThreadID domain.ThreadID
	Value    domain.PolicyPreset
	Clear    bool
}

func (PolicyOverrideChanged) composerAction() {}

type BudgetOverrideChanged struct {
	ThreadID domain.ThreadID
	Value    domain.Money
	Clear    bool
}

func (BudgetOverrideChanged) composerAction() {}

type ModelOverrideChanged struct {
	ThreadID domain.ThreadID
	Value    ModelOverride
	Clear    bool
}

func (ModelOverrideChanged) composerAction() {}

type EffortOverrideChanged struct {
	ThreadID domain.ThreadID
	Value    domain.ReasoningEffort
	Clear    bool
}

func (EffortOverrideChanged) composerAction() {}

type SendStarted struct {
	ThreadID domain.ThreadID
	Key      IdempotencyKey
}

func (SendStarted) composerAction() {}

type SendFailureReceived struct {
	ThreadID    domain.ThreadID
	Key         IdempotencyKey
	Retryable   bool
	SafeMessage string
}

func (SendFailureReceived) composerAction() {}

// SendAccepted records the message identity returned by the command transport.
// Acceptance alone is not authoritative timeline confirmation and never clears
// the retained draft.
type SendAccepted struct {
	ThreadID  domain.ThreadID
	Key       IdempotencyKey
	MessageID domain.MessageID
}

func (SendAccepted) composerAction() {}

type SendRetryRequested struct {
	ThreadID domain.ThreadID
	Key      IdempotencyKey
}

func (SendRetryRequested) composerAction() {}

// TimelineCommitConfirmation can only be constructed from a validated,
// ordered user-message final event. Keeping its fields private prevents an RPC
// response or local preview from masquerading as durable timeline evidence.
type TimelineCommitConfirmation struct {
	threadID  domain.ThreadID
	messageID domain.MessageID
	sequence  uint64
}

func NewTimelineCommitConfirmation(event events.SessionEvent) (TimelineCommitConfirmation, error) {
	if err := event.Validate(); err != nil {
		return TimelineCommitConfirmation{}, valueError("timeline_event", "must be a valid ordered session event")
	}
	if event.Kind != events.KindMessageFinal || event.Payload.MessageFinal == nil ||
		!strings.EqualFold(strings.TrimSpace(event.Payload.MessageFinal.Role), "user") {
		return TimelineCommitConfirmation{}, valueError("timeline_event", "must be a committed user message final event")
	}
	return TimelineCommitConfirmation{
		threadID: event.ThreadID, messageID: event.Payload.MessageFinal.MessageID,
		sequence: event.Sequence,
	}, nil
}

type SendCommitConfirmed struct {
	ThreadID     domain.ThreadID
	Key          IdempotencyKey
	Confirmation TimelineCommitConfirmation
}

func (SendCommitConfirmed) composerAction() {}

type SendAbandoned struct {
	ThreadID domain.ThreadID
	Key      IdempotencyKey
}

func (SendAbandoned) composerAction() {}

func Reduce(model Model, action Action) (Model, error) {
	switch value := action.(type) {
	case ThreadBound:
		return reduceThreadBound(model, value)
	case DraftTextChanged:
		return reduceDraft(model, value.ThreadID, func(draft Draft) (Draft, error) {
			if len(value.Text) > maxDraftBytes {
				return Draft{}, valueError("draft.text", "must be at most 64 KiB")
			}
			draft.text = value.Text
			return draft, nil
		})
	case AttachmentAdded:
		return reduceAttachmentAdded(model, value)
	case AttachmentRemoved:
		return reduceAttachmentRemoved(model, value)
	case PolicyOverrideChanged:
		return reduceDraft(model, value.ThreadID, func(draft Draft) (Draft, error) {
			if value.Clear {
				draft.policy, draft.hasPolicy = "", false
				return draft, nil
			}
			if !value.Value.IsValid() {
				return Draft{}, valueError("draft.policy_override", "must be a declared policy preset")
			}
			draft.policy, draft.hasPolicy = value.Value, true
			return draft, nil
		})
	case BudgetOverrideChanged:
		return reduceDraft(model, value.ThreadID, func(draft Draft) (Draft, error) {
			if value.Clear {
				draft.budget, draft.hasBudget = domain.Money{}, false
				return draft, nil
			}
			if err := value.Value.Validate(); err != nil || value.Value.MinorUnits <= 0 {
				return Draft{}, valueError("draft.budget_override", "must be a positive exact monetary amount")
			}
			draft.budget, draft.hasBudget = value.Value, true
			return draft, nil
		})
	case ModelOverrideChanged:
		return reduceDraft(model, value.ThreadID, func(draft Draft) (Draft, error) {
			if value.Clear {
				draft.model, draft.hasModel = ModelOverride{}, false
				return draft, nil
			}
			if err := value.Value.Validate(); err != nil {
				return Draft{}, err
			}
			draft.model, draft.hasModel = value.Value, true
			return draft, nil
		})
	case EffortOverrideChanged:
		return reduceDraft(model, value.ThreadID, func(draft Draft) (Draft, error) {
			if value.Clear {
				draft.effort, draft.hasEffort = "", false
				return draft, nil
			}
			if !value.Value.IsValid() {
				return Draft{}, valueError("draft.effort_override", "must be a declared reasoning effort")
			}
			draft.effort, draft.hasEffort = value.Value, true
			return draft, nil
		})
	case SendStarted:
		return reduceSendStarted(model, value)
	case SendFailureReceived:
		return reduceSendFailure(model, value)
	case SendAccepted:
		return reduceSendAccepted(model, value)
	case SendRetryRequested:
		return reduceSendRetry(model, value)
	case SendCommitConfirmed:
		return reduceSendCommit(model, value)
	case SendAbandoned:
		return reduceSendAbandoned(model, value)
	case nil:
		return model, valueError("action", "must not be nil")
	default:
		return model, valueError("action", "is not a supported composer action")
	}
}

func reduceThreadBound(model Model, action ThreadBound) (Model, error) {
	if action.ThreadID.IsZero() || action.RepositoryID.IsZero() {
		return model, valueError("thread_binding", "requires server-issued thread and repository identities")
	}
	key := action.ThreadID.String()
	if current, exists := model.repositories[key]; exists && current != action.RepositoryID {
		return model, valueError("thread_binding.repository_id", "cannot replace a thread repository binding")
	}
	next := cloneModel(model)
	next.repositories[key] = action.RepositoryID
	return next, nil
}

func reduceDraft(
	model Model,
	threadID domain.ThreadID,
	update func(Draft) (Draft, error),
) (Model, error) {
	threadKey, err := boundThreadKey(model, threadID)
	if err != nil {
		return model, err
	}
	if attempt, exists := model.attempts[threadKey]; exists && attempt.blocksEditing() {
		return model, ErrComposerBusy
	}
	draft, err := update(cloneDraft(model.drafts[threadKey]))
	if err != nil {
		return model, err
	}
	next := cloneModel(model)
	if draft.IsZero() {
		delete(next.drafts, threadKey)
	} else {
		next.drafts[threadKey] = cloneDraft(draft)
	}
	// Editing a failed request is an explicit abandonment of its retained key;
	// the changed payload must receive a new key on the next send.
	if attempt, exists := next.attempts[threadKey]; exists && attempt.status == SendFailed {
		delete(next.attempts, threadKey)
	}
	return next, nil
}

func reduceAttachmentAdded(model Model, action AttachmentAdded) (Model, error) {
	if err := action.Attachment.Validate(); err != nil {
		return model, err
	}
	threadKey, err := boundThreadKey(model, action.ThreadID)
	if err != nil {
		return model, err
	}
	if model.repositories[threadKey] != action.Attachment.RepositoryID() {
		return model, valueError("attachment.repository_id", "must match the thread repository")
	}
	return reduceDraft(model, action.ThreadID, func(draft Draft) (Draft, error) {
		for _, existing := range draft.attachments {
			if existing.Key() == action.Attachment.Key() {
				return draft, nil
			}
		}
		if len(draft.attachments) >= maxAttachments {
			return Draft{}, valueError("draft.attachments", "must contain at most 32 server identities")
		}
		draft.attachments = append(slices.Clone(draft.attachments), action.Attachment)
		return draft, nil
	})
}

func reduceAttachmentRemoved(model Model, action AttachmentRemoved) (Model, error) {
	attachmentKey := strings.TrimSpace(action.AttachmentKey)
	if attachmentKey == "" {
		return model, valueError("attachment_key", "must not be empty")
	}
	return reduceDraft(model, action.ThreadID, func(draft Draft) (Draft, error) {
		attachments := make([]RepositoryAttachment, 0, len(draft.attachments))
		for _, attachment := range draft.attachments {
			if attachment.Key() != attachmentKey {
				attachments = append(attachments, attachment)
			}
		}
		draft.attachments = attachments
		return draft, nil
	})
}

func reduceSendStarted(model Model, action SendStarted) (Model, error) {
	threadKey, err := boundThreadKey(model, action.ThreadID)
	if err != nil {
		return model, err
	}
	if _, err := ParseIdempotencyKey(string(action.Key)); err != nil {
		return model, err
	}
	if attempt, exists := model.attempts[threadKey]; exists {
		if attempt.blocksEditing() {
			return model, ErrComposerBusy
		}
		return model, ErrSendAttemptMismatch
	}
	draft := model.drafts[threadKey]
	if !draft.CanSubmit() {
		return model, valueError("draft.text", "must contain non-whitespace text before send")
	}
	next := cloneModel(model)
	next.attempts[threadKey] = SendAttempt{
		key: action.Key, request: cloneDraft(draft), status: SendPending,
	}
	return next, nil
}

func reduceSendFailure(model Model, action SendFailureReceived) (Model, error) {
	threadKey, attempt, err := matchingAttempt(model, action.ThreadID, action.Key)
	if err != nil {
		return model, err
	}
	if attempt.status != SendPending {
		return model, ErrSendAttemptMismatch
	}
	message := strings.TrimSpace(action.SafeMessage)
	if message == "" || len(message) > maxSafeMessageBytes {
		return model, valueError("send_failure.safe_message", "must be non-empty, redacted, and bounded")
	}
	next := cloneModel(model)
	attempt.status = SendFailed
	attempt.retryable = action.Retryable
	attempt.safeMessage = message
	next.attempts[threadKey] = attempt
	return next, nil
}

func reduceSendAccepted(model Model, action SendAccepted) (Model, error) {
	threadKey, attempt, err := matchingAttempt(model, action.ThreadID, action.Key)
	if err != nil {
		return model, err
	}
	if attempt.status != SendPending {
		return model, ErrSendAttemptMismatch
	}
	if action.MessageID.IsZero() {
		return model, valueError("message_id", "must be a committed server message identity")
	}
	next := cloneModel(model)
	attempt.status = SendAwaitingConfirmation
	attempt.messageID = action.MessageID
	attempt.retryable = false
	attempt.safeMessage = ""
	next.attempts[threadKey] = attempt
	return next, nil
}

func reduceSendRetry(model Model, action SendRetryRequested) (Model, error) {
	threadKey, attempt, err := matchingAttempt(model, action.ThreadID, action.Key)
	if err != nil {
		return model, err
	}
	if attempt.status != SendFailed {
		return model, ErrSendAttemptMismatch
	}
	if !attempt.retryable {
		return model, ErrSendNotRetryable
	}
	next := cloneModel(model)
	attempt.status = SendPending
	attempt.safeMessage = ""
	next.attempts[threadKey] = attempt
	return next, nil
}

func reduceSendCommit(model Model, action SendCommitConfirmed) (Model, error) {
	confirmation := action.Confirmation
	if confirmation.sequence == 0 || confirmation.threadID.IsZero() || confirmation.messageID.IsZero() {
		return model, valueError("timeline_confirmation", "must come from an authoritative ordered timeline event")
	}
	threadKey, attempt, err := matchingAttempt(model, action.ThreadID, action.Key)
	if err != nil {
		return model, err
	}
	if attempt.status != SendAwaitingConfirmation || confirmation.threadID != action.ThreadID ||
		confirmation.messageID != attempt.messageID {
		return model, ErrSendAttemptMismatch
	}
	next := cloneModel(model)
	delete(next.drafts, threadKey)
	delete(next.attempts, threadKey)
	return next, nil
}

func reduceSendAbandoned(model Model, action SendAbandoned) (Model, error) {
	threadKey, attempt, err := matchingAttempt(model, action.ThreadID, action.Key)
	if err != nil {
		return model, err
	}
	if attempt.status != SendFailed {
		return model, ErrComposerBusy
	}
	next := cloneModel(model)
	delete(next.attempts, threadKey)
	return next, nil
}

func boundThreadKey(model Model, threadID domain.ThreadID) (string, error) {
	if threadID.IsZero() {
		return "", valueError("thread_id", "must be a server-issued thread identity")
	}
	key := threadID.String()
	if repositoryID, exists := model.repositories[key]; !exists || repositoryID.IsZero() {
		return "", valueError("thread_id", "must be bound to an authorized repository")
	}
	return key, nil
}

func matchingAttempt(
	model Model,
	threadID domain.ThreadID,
	key IdempotencyKey,
) (string, SendAttempt, error) {
	threadKey, err := boundThreadKey(model, threadID)
	if err != nil {
		return "", SendAttempt{}, err
	}
	attempt, exists := model.attempts[threadKey]
	if !exists || attempt.key != key {
		return "", SendAttempt{}, ErrSendAttemptMismatch
	}
	return threadKey, attempt, nil
}

func IsComposerValueError(err error) bool {
	return errors.Is(err, ErrInvalidComposerValue)
}
