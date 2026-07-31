package timeline

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"google.golang.org/grpc"
)

const DefaultPageLimit uint32 = 50

var (
	ErrInvalidThreadPage = errors.New("invalid thread page")
	ErrPageScope         = errors.New("thread page belongs to another thread")
)

// Cursor is an opaque server-issued pagination identity.
type Cursor string

// RedactedBody preserves the server's explicit redaction and truncation facts.
type RedactedBody struct {
	Text          string
	Truncated     bool
	OriginalBytes uint64
}

// DurableMessage is the transport-neutral projection returned by
// ThreadService.GetThreadPage.
type DurableMessage struct {
	ID          domain.MessageID
	ThreadID    domain.ThreadID
	Role        string
	Body        RedactedBody
	Attachments []string
	Revision    uint64
	Sequence    uint64
	CreatedAt   time.Time
}

// MessagePage is one bounded response. RequestCursor is retained so retries
// and diagnostics never infer meaning from the opaque cursor.
type MessagePage struct {
	ThreadID      domain.ThreadID
	RequestCursor Cursor
	Messages      []DurableMessage
	NextCursor    Cursor
	HasOlder      bool
}

// MessageFeed is the immutable newest-first pagination aggregate presented by
// the selected thread. Messages remain in readable oldest-to-newest order.
type MessageFeed struct {
	ThreadID     domain.ThreadID
	Messages     []DurableMessage
	OlderCursor  Cursor
	HasOlder     bool
	ReachedStart bool
	LoadingOlder bool
	Retryable    bool
	SafeError    string
}

// ThreadPageRPC is the smallest generated-client boundary used by the
// timeline. A generated ThreadServiceClient satisfies it without leaking the
// rest of the service into tests.
type ThreadPageRPC interface {
	GetThreadPage(
		context.Context,
		*codefluxv1.GetThreadPageRequest,
		...grpc.CallOption,
	) (*codefluxv1.GetThreadPageResponse, error)
}

// PageClient fetches bounded message history for exactly one selected thread.
type PageClient struct {
	threadID domain.ThreadID
	rpc      ThreadPageRPC
	limit    uint32
}

func NewPageClient(threadID domain.ThreadID, rpc ThreadPageRPC, limit uint32) (PageClient, error) {
	if threadID.IsZero() || rpc == nil || limit == 0 || limit > 1000 {
		return PageClient{}, fmt.Errorf("%w: client scope", ErrInvalidThreadPage)
	}
	return PageClient{threadID: threadID, rpc: rpc, limit: limit}, nil
}

func (client PageClient) FetchNewest(ctx context.Context) (MessagePage, error) {
	return client.fetch(ctx, "")
}

func (client PageClient) FetchOlder(ctx context.Context, cursor Cursor) (MessagePage, error) {
	if strings.TrimSpace(string(cursor)) == "" {
		return MessagePage{}, fmt.Errorf("%w: older cursor is empty", ErrInvalidThreadPage)
	}
	return client.fetch(ctx, cursor)
}

func (client PageClient) fetch(ctx context.Context, cursor Cursor) (MessagePage, error) {
	response, err := client.rpc.GetThreadPage(ctx, &codefluxv1.GetThreadPageRequest{
		ThreadId: &codefluxv1.StableIdentity{
			Kind:  codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_THREAD,
			Value: client.threadID.String(),
		},
		Page: &codefluxv1.PageRequest{Cursor: string(cursor), Limit: client.limit},
	})
	if err != nil {
		return MessagePage{}, err
	}
	if response == nil || response.GetPage() == nil || response.GetThread() == nil {
		return MessagePage{}, fmt.Errorf("%w: response envelope", ErrInvalidThreadPage)
	}
	responseThreadID, err := decodeStableThreadID(response.GetThread().GetThreadId())
	if err != nil || responseThreadID != client.threadID {
		return MessagePage{}, fmt.Errorf("%w: response thread identity", ErrPageScope)
	}
	messages := make([]DurableMessage, 0, len(response.GetMessages()))
	for _, message := range response.GetMessages() {
		decoded, decodeErr := decodeMessage(client.threadID, message)
		if decodeErr != nil {
			return MessagePage{}, decodeErr
		}
		messages = append(messages, decoded)
	}
	page := MessagePage{
		ThreadID: client.threadID, RequestCursor: cursor, Messages: messages,
		NextCursor: Cursor(response.GetPage().GetNextCursor()),
		HasOlder:   response.GetPage().GetHasMore(),
	}
	if page.HasOlder && strings.TrimSpace(string(page.NextCursor)) == "" {
		return MessagePage{}, fmt.Errorf("%w: has_more without cursor", ErrInvalidThreadPage)
	}
	return page, nil
}

func decodeStableThreadID(identity *codefluxv1.StableIdentity) (domain.ThreadID, error) {
	if identity == nil || identity.GetKind() != codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_THREAD {
		return domain.ThreadID{}, ErrInvalidThreadPage
	}
	return domain.ParseThreadID(identity.GetValue())
}

func decodeMessage(threadID domain.ThreadID, message *codefluxv1.MessageView) (DurableMessage, error) {
	if message == nil || message.GetMessageId() == nil ||
		message.GetMessageId().GetKind() != codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_MESSAGE ||
		message.GetThreadId() == nil ||
		message.GetThreadId().GetKind() != codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_THREAD ||
		message.GetThreadId().GetValue() != threadID.String() ||
		message.GetBody() == nil || message.GetCreatedAt() == nil ||
		!message.GetCreatedAt().IsValid() || strings.TrimSpace(message.GetRole()) == "" {
		return DurableMessage{}, fmt.Errorf("%w: message envelope", ErrInvalidThreadPage)
	}
	messageID, err := domain.ParseMessageID(message.GetMessageId().GetValue())
	if err != nil {
		return DurableMessage{}, fmt.Errorf("%w: message identity", ErrInvalidThreadPage)
	}
	attachments := make([]string, 0, len(message.GetAttachments()))
	for _, attachment := range message.GetAttachments() {
		if attachment == nil || strings.TrimSpace(attachment.GetWorkspaceRelativeSlashPath()) == "" {
			return DurableMessage{}, fmt.Errorf("%w: attachment identity", ErrInvalidThreadPage)
		}
		attachments = append(attachments, attachment.GetWorkspaceRelativeSlashPath())
	}
	return DurableMessage{
		ID: messageID, ThreadID: threadID, Role: message.GetRole(),
		Body: RedactedBody{
			Text:          message.GetBody().GetValue(),
			Truncated:     message.GetBody().GetTruncated(),
			OriginalBytes: message.GetBody().GetOriginalBytes(),
		},
		Attachments: attachments, Revision: message.GetRevision(),
		Sequence:  message.GetSequence(),
		CreatedAt: message.GetCreatedAt().AsTime().UTC(),
	}, nil
}

// ApplyNewestMessagePage replaces the selected thread feed with the newest
// authoritative page.
func ApplyNewestMessagePage(page MessagePage) (MessageFeed, error) {
	messages, err := validateMessagePage(page)
	if err != nil {
		return MessageFeed{}, err
	}
	return MessageFeed{
		ThreadID: page.ThreadID, Messages: messages,
		OlderCursor: page.NextCursor, HasOlder: page.HasOlder,
		ReachedStart: !page.HasOlder,
	}, nil
}

// BeginOlderMessagePage marks exactly one bounded older-page request active.
// It rejects stale or exhausted feeds so scroll observers cannot issue
// duplicate pagination commands while a request is already in flight.
func BeginOlderMessagePage(feed MessageFeed) (MessageFeed, error) {
	if feed.ThreadID.IsZero() || !feed.HasOlder || feed.ReachedStart ||
		strings.TrimSpace(string(feed.OlderCursor)) == "" || feed.LoadingOlder {
		return feed, ErrInvalidThreadPage
	}
	next := cloneMessageFeed(feed)
	next.LoadingOlder = true
	next.Retryable = false
	next.SafeError = ""
	return next, nil
}

// PrependOlderMessagePage joins an older page without duplicating overlap at a
// pagination boundary. Existing newer projections win when the same durable
// message is replayed with an older revision.
func PrependOlderMessagePage(feed MessageFeed, page MessagePage) (MessageFeed, error) {
	older, err := validateMessagePage(page)
	if err != nil {
		return feed, err
	}
	if feed.ThreadID.IsZero() || feed.ThreadID != page.ThreadID ||
		feed.OlderCursor == "" || page.RequestCursor != feed.OlderCursor {
		return feed, ErrPageScope
	}
	byID := make(map[string]DurableMessage, len(feed.Messages)+len(older))
	order := make([]string, 0, len(feed.Messages)+len(older))
	appendMessage := func(message DurableMessage, existingWins bool) {
		key := message.ID.String()
		if existing, ok := byID[key]; ok {
			if existingWins || existing.Revision >= message.Revision {
				return
			}
			byID[key] = cloneDurableMessage(message)
			return
		}
		order = append(order, key)
		byID[key] = cloneDurableMessage(message)
	}
	for _, message := range older {
		appendMessage(message, false)
	}
	for _, message := range feed.Messages {
		appendMessage(message, true)
	}
	merged := make([]DurableMessage, 0, len(order))
	for _, key := range order {
		merged = append(merged, cloneDurableMessage(byID[key]))
	}
	return MessageFeed{
		ThreadID: page.ThreadID, Messages: merged,
		OlderCursor: page.NextCursor, HasOlder: page.HasOlder,
		ReachedStart: !page.HasOlder,
	}, nil
}

func FailOlderMessagePage(feed MessageFeed) MessageFeed {
	next := cloneMessageFeed(feed)
	next.LoadingOlder = false
	next.Retryable = true
	next.SafeError = "Older messages could not be loaded."
	return next
}

func validateMessagePage(page MessagePage) ([]DurableMessage, error) {
	if page.ThreadID.IsZero() || (page.HasOlder && page.NextCursor == "") {
		return nil, ErrInvalidThreadPage
	}
	seen := make(map[string]struct{}, len(page.Messages))
	messages := make([]DurableMessage, 0, len(page.Messages))
	for _, message := range page.Messages {
		if message.ID.IsZero() || message.ThreadID != page.ThreadID ||
			strings.TrimSpace(message.Role) == "" || message.CreatedAt.IsZero() {
			return nil, ErrInvalidThreadPage
		}
		key := message.ID.String()
		if _, duplicate := seen[key]; duplicate {
			return nil, fmt.Errorf("%w: duplicate message %s", ErrInvalidThreadPage, key)
		}
		seen[key] = struct{}{}
		messages = append(messages, cloneDurableMessage(message))
	}
	return messages, nil
}

func cloneDurableMessage(message DurableMessage) DurableMessage {
	message.Attachments = slices.Clone(message.Attachments)
	return message
}

func cloneMessageFeed(feed MessageFeed) MessageFeed {
	feed.Messages = make([]DurableMessage, len(feed.Messages))
	for index, message := range feed.Messages {
		feed.Messages[index] = cloneDurableMessage(message)
	}
	return feed
}
