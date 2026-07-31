package timeline

import (
	"context"
	"errors"
	"testing"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type fakeThreadPageRPC struct {
	request  *codefluxv1.GetThreadPageRequest
	response *codefluxv1.GetThreadPageResponse
	err      error
}

func (fake *fakeThreadPageRPC) GetThreadPage(
	_ context.Context,
	request *codefluxv1.GetThreadPageRequest,
	_ ...grpc.CallOption,
) (*codefluxv1.GetThreadPageResponse, error) {
	fake.request = request
	return fake.response, fake.err
}

func TestPageClientFetchesNewestAndOlderWithTypedIdentities(t *testing.T) {
	threadID := parseThreadIDFixture(t, "thr_018f0123-4567-789a-8bcd-ef0123456789")
	messageID := parseMessageIDFixture(t, "msg_018f0123-4567-789a-8bcd-ef0123456789")
	rpc := &fakeThreadPageRPC{}
	rpc.response = pageResponse(threadID, messageID, "opaque-next", true)
	client, err := NewPageClient(threadID, rpc, 25)
	if err != nil {
		t.Fatal(err)
	}
	newest, err := client.FetchNewest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rpc.request.GetPage().GetCursor() != "" || rpc.request.GetPage().GetLimit() != 25 ||
		rpc.request.GetThreadId().GetKind() != codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_THREAD ||
		len(newest.Messages) != 1 || newest.Messages[0].ID != messageID ||
		newest.Messages[0].Attachments[0] != "internal/domain/identity.go" {
		t.Fatalf("newest page = %#v request=%#v", newest, rpc.request)
	}
	rpc.response = pageResponse(threadID, messageID, "", false)
	older, err := client.FetchOlder(context.Background(), "opaque-next")
	if err != nil {
		t.Fatal(err)
	}
	if rpc.request.GetPage().GetCursor() != "opaque-next" ||
		older.RequestCursor != "opaque-next" || older.HasOlder {
		t.Fatalf("older page = %#v request=%#v", older, rpc.request)
	}
}

func TestPageClientRejectsMalformedCrossThreadAndCursorResponses(t *testing.T) {
	threadID := parseThreadIDFixture(t, "thr_018f0123-4567-789a-8bcd-ef0123456789")
	otherThreadID := parseThreadIDFixture(t, "thr_028f0123-4567-789a-8bcd-ef0123456789")
	messageID := parseMessageIDFixture(t, "msg_018f0123-4567-789a-8bcd-ef0123456789")
	rpc := &fakeThreadPageRPC{response: pageResponse(otherThreadID, messageID, "", false)}
	client, err := NewPageClient(threadID, rpc, DefaultPageLimit)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.FetchNewest(context.Background()); !errors.Is(err, ErrPageScope) {
		t.Fatalf("cross-thread error = %v", err)
	}
	rpc.response = pageResponse(threadID, messageID, "", true)
	if _, err := client.FetchNewest(context.Background()); !errors.Is(err, ErrInvalidThreadPage) {
		t.Fatalf("missing cursor error = %v", err)
	}
	if _, err := client.FetchOlder(context.Background(), " "); !errors.Is(err, ErrInvalidThreadPage) {
		t.Fatalf("empty older cursor error = %v", err)
	}
}

func TestMessagePagesJoinEveryOverlapWithoutDuplicates(t *testing.T) {
	threadID := parseThreadIDFixture(t, "thr_018f0123-4567-789a-8bcd-ef0123456789")
	base := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	messages := []DurableMessage{
		testDurableMessage(t, threadID, "msg_018f0123-4567-789a-8bcd-ef0123456781", base),
		testDurableMessage(t, threadID, "msg_018f0123-4567-789a-8bcd-ef0123456782", base.Add(time.Minute)),
		testDurableMessage(t, threadID, "msg_018f0123-4567-789a-8bcd-ef0123456783", base.Add(2*time.Minute)),
		testDurableMessage(t, threadID, "msg_018f0123-4567-789a-8bcd-ef0123456784", base.Add(3*time.Minute)),
	}
	for boundary := 0; boundary <= len(messages); boundary++ {
		newerStart := boundary
		if newerStart > 0 {
			newerStart--
		}
		feed, err := ApplyNewestMessagePage(MessagePage{
			ThreadID: threadID, Messages: messages[newerStart:],
			NextCursor: "older", HasOlder: true,
		})
		if err != nil {
			t.Fatalf("boundary %d newest: %v", boundary, err)
		}
		olderEnd := boundary
		if olderEnd < len(messages) {
			olderEnd++
		}
		feed, err = PrependOlderMessagePage(feed, MessagePage{
			ThreadID: threadID, RequestCursor: "older",
			Messages: messages[:olderEnd],
		})
		if err != nil {
			t.Fatalf("boundary %d older: %v", boundary, err)
		}
		if len(feed.Messages) != len(messages) || !feed.ReachedStart || feed.HasOlder {
			t.Fatalf("boundary %d merged = %#v", boundary, feed)
		}
		for index, message := range feed.Messages {
			if message.ID != messages[index].ID {
				t.Fatalf("boundary %d index %d = %s, want %s", boundary, index, message.ID, messages[index].ID)
			}
		}
	}
}

func TestOlderPageFailureIsSafeAndRetryable(t *testing.T) {
	feed := MessageFeed{SafeError: "secret detail", LoadingOlder: true}
	failed := FailOlderMessagePage(feed)
	if failed.LoadingOlder || !failed.Retryable ||
		failed.SafeError != "Older messages could not be loaded." {
		t.Fatalf("failed feed = %#v", failed)
	}
}

func TestBeginOlderMessagePagePreventsDuplicateAndExhaustedLoads(t *testing.T) {
	threadID := parseThreadIDFixture(t, "thr_018f0123-4567-789a-8bcd-ef0123456789")
	feed := MessageFeed{
		ThreadID: threadID, OlderCursor: "older-page", HasOlder: true,
		Retryable: true, SafeError: "previous safe error",
	}
	loading, err := BeginOlderMessagePage(feed)
	if err != nil {
		t.Fatal(err)
	}
	if !loading.LoadingOlder || loading.Retryable || loading.SafeError != "" {
		t.Fatalf("loading feed = %#v", loading)
	}
	if _, err := BeginOlderMessagePage(loading); !errors.Is(err, ErrInvalidThreadPage) {
		t.Fatalf("duplicate load error = %v", err)
	}
	exhausted := feed
	exhausted.HasOlder = false
	exhausted.ReachedStart = true
	if _, err := BeginOlderMessagePage(exhausted); !errors.Is(err, ErrInvalidThreadPage) {
		t.Fatalf("exhausted load error = %v", err)
	}
}

func pageResponse(
	threadID domain.ThreadID,
	messageID domain.MessageID,
	next string,
	hasMore bool,
) *codefluxv1.GetThreadPageResponse {
	return &codefluxv1.GetThreadPageResponse{
		Thread: &codefluxv1.ThreadView{
			ThreadId: &codefluxv1.StableIdentity{
				Kind:  codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_THREAD,
				Value: threadID.String(),
			},
		},
		Messages: []*codefluxv1.MessageView{{
			MessageId: &codefluxv1.StableIdentity{
				Kind:  codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_MESSAGE,
				Value: messageID.String(),
			},
			ThreadId: &codefluxv1.StableIdentity{
				Kind:  codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_THREAD,
				Value: threadID.String(),
			},
			Role: "agent",
			Body: &codefluxv1.RedactedText{
				Value: "Safe body", Truncated: true, OriginalBytes: 128,
			},
			Attachments: []*codefluxv1.SafePath{{
				WorkspaceRelativeSlashPath: "internal/domain/identity.go",
				Exists:                     true,
			}},
			Revision:  3,
			CreatedAt: timestamppb.New(time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)),
		}},
		Page: &codefluxv1.PageInfo{NextCursor: next, HasMore: hasMore},
	}
}

func testDurableMessage(
	t *testing.T,
	threadID domain.ThreadID,
	rawID string,
	created time.Time,
) DurableMessage {
	t.Helper()
	return DurableMessage{
		ID: parseMessageIDFixture(t, rawID), ThreadID: threadID, Role: "agent",
		Body: RedactedBody{Text: rawID}, Revision: 1, CreatedAt: created,
	}
}

func parseThreadIDFixture(t *testing.T, raw string) domain.ThreadID {
	t.Helper()
	value, err := domain.ParseThreadID(raw)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func parseMessageIDFixture(t *testing.T, raw string) domain.MessageID {
	t.Helper()
	value, err := domain.ParseMessageID(raw)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
