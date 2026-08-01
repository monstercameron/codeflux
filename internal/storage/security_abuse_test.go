package storage

import (
	"errors"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
)

func abuseThreadFixture(t *testing.T, base int) (*Repositories, domain.ThreadID) {
	t.Helper()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, base)
	repositoryID := testRepositoryID(t, base+1)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)
	threadID := testThreadID(t, base+2)
	if _, err := repositories.CreateThread(t.Context(), CreateThread{
		ID:           threadID,
		ProjectID:    projectID,
		RepositoryID: repositoryID,
		Title:        "Abuse fixture",
	}); err != nil {
		t.Fatalf("create thread: %v", err)
	}
	return repositories, threadID
}

// TestM22_055_RepeatedIdempotencyKeyDoesNotMutate is M22-055.
//
// The dangerous shape is not a repeated identical call, which must be a no-op,
// but a repeated call under the SAME key carrying DIFFERENT content: if the
// store accepted it, a replayed request could silently rewrite durable
// history. It must be refused as a conflict, and the original must survive
// unchanged.
func TestM22_055_RepeatedIdempotencyKeyDoesNotMutate(t *testing.T) {
	repositories, threadID := abuseThreadFixture(t, 9100)
	const key = "abuse-idempotency-key"

	original, err := repositories.AppendMessage(t.Context(), AppendMessage{
		ID:             testMessageID(t, 9110),
		ThreadID:       threadID,
		Role:           MessageRoleUser,
		BodyRedacted:   "Add pagination to the reservation list.",
		IdempotencyKey: key,
	})
	if err != nil {
		t.Fatalf("append original message: %v", err)
	}

	// An identical retry is a no-op returning the same durable row.
	replayed, err := repositories.AppendMessage(t.Context(), AppendMessage{
		ID:             original.ID,
		ThreadID:       threadID,
		Role:           MessageRoleUser,
		BodyRedacted:   "Add pagination to the reservation list.",
		IdempotencyKey: key,
	})
	if err != nil {
		t.Fatalf("identical retry was refused: %v", err)
	}
	if replayed.ID != original.ID || replayed.BodyRedacted != original.BodyRedacted {
		t.Fatalf("identical retry produced a different row: %+v vs %+v", replayed, original)
	}

	mutations := []struct {
		name  string
		input AppendMessage
	}{
		{"different body", AppendMessage{
			ID: original.ID, ThreadID: threadID, Role: MessageRoleUser,
			BodyRedacted: "Delete every reservation.", IdempotencyKey: key,
		}},
		{"different role", AppendMessage{
			ID: original.ID, ThreadID: threadID, Role: MessageRoleSystem,
			BodyRedacted: original.BodyRedacted, IdempotencyKey: key,
		}},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			if _, err := repositories.AppendMessage(t.Context(), mutation.input); err == nil {
				t.Fatal("a reused idempotency key carrying different content was accepted")
			} else if !errors.Is(err, ErrConflict) {
				t.Fatalf("reuse was refused with %v, want ErrConflict", err)
			}
		})
	}

	// A retry that mints a fresh message ID is the ordinary client shape, since
	// IDs are per-attempt and the key is the deduplication identity. It must
	// return the ORIGINAL row rather than storing a second one, and the caller
	// must be able to see which ID actually persisted.
	reminted, err := repositories.AppendMessage(t.Context(), AppendMessage{
		ID:             testMessageID(t, 9111),
		ThreadID:       threadID,
		Role:           MessageRoleUser,
		BodyRedacted:   original.BodyRedacted,
		IdempotencyKey: key,
	})
	if err != nil {
		t.Fatalf("retry under a fresh message ID was refused: %v", err)
	}
	if reminted.ID != original.ID {
		t.Fatalf("retry returned ID %v, want the stored %v", reminted.ID, original.ID)
	}

	// The original row must be exactly as it was, and there must be no second
	// row: a refused mutation that still wrote something would be worse than
	// one that succeeded, because nothing would report it.
	page, err := repositories.ListMessages(t.Context(), ListMessages{ThreadID: threadID})
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	listed := page.Messages
	if len(listed) != 1 {
		t.Fatalf("thread holds %d messages after refused mutations, want 1", len(listed))
	}
	if listed[0].BodyRedacted != original.BodyRedacted || listed[0].Role != original.Role {
		t.Fatalf("a refused mutation altered the stored message: %+v", listed[0])
	}
}

// TestM22_062_OversizedMessagePayloadIsRefused is the message half of M22-062.
func TestM22_062_OversizedMessagePayloadIsRefused(t *testing.T) {
	repositories, threadID := abuseThreadFixture(t, 9200)

	// At the bound the message is accepted; one byte past it, refused. Testing
	// only a wildly oversized payload would pass against an off-by-a-megabyte
	// bound, which is not a bound anyone reasoned about.
	atLimit := strings.Repeat("a", MaximumMessageBodyBytes)
	if _, err := repositories.AppendMessage(t.Context(), AppendMessage{
		ID:             testMessageID(t, 9210),
		ThreadID:       threadID,
		Role:           MessageRoleUser,
		BodyRedacted:   atLimit,
		IdempotencyKey: "abuse-at-limit",
	}); err != nil {
		t.Fatalf("a message exactly at the bound was refused: %v", err)
	}

	oversized := []struct {
		name string
		body string
	}{
		{"one byte over", strings.Repeat("a", MaximumMessageBodyBytes+1)},
		{"ten times over", strings.Repeat("a", MaximumMessageBodyBytes*10)},
	}
	for _, testCase := range oversized {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := repositories.AppendMessage(t.Context(), AppendMessage{
				ID:             testMessageID(t, 9211),
				ThreadID:       threadID,
				Role:           MessageRoleUser,
				BodyRedacted:   testCase.body,
				IdempotencyKey: "abuse-oversized-" + testCase.name,
			}); err == nil {
				t.Fatalf("a %d byte message body was accepted", len(testCase.body))
			}
		})
	}

	page, err := repositories.ListMessages(t.Context(), ListMessages{ThreadID: threadID})
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	listed := page.Messages
	if len(listed) != 1 {
		t.Fatalf("thread holds %d messages, want only the at-limit one", len(listed))
	}
}
