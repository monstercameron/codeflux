package composer_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
	"codeflux.dev/codeflux/web/frontend/composer"
)

const fixtureUUID = "01890f3c-4a00-7abc-8def-0123456789ab"

type identities struct {
	repositoryOne domain.RepositoryID
	repositoryTwo domain.RepositoryID
	threadOne     domain.ThreadID
	threadTwo     domain.ThreadID
	artifact      domain.ArtifactID
	atom          domain.AtomID
	provider      domain.ProviderID
	message       domain.MessageID
}

func fixtureIdentities(t *testing.T) identities {
	t.Helper()
	repositoryOne, err := domain.ParseRepositoryID("repo_" + fixtureUUID)
	if err != nil {
		t.Fatal(err)
	}
	repositoryTwo, err := domain.ParseRepositoryID("repo_01890f3c-4a00-7abc-8def-0123456789ac")
	if err != nil {
		t.Fatal(err)
	}
	threadOne, err := domain.ParseThreadID("thr_" + fixtureUUID)
	if err != nil {
		t.Fatal(err)
	}
	threadTwo, err := domain.ParseThreadID("thr_01890f3c-4a00-7abc-8def-0123456789ac")
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := domain.ParseArtifactID("art_" + fixtureUUID)
	if err != nil {
		t.Fatal(err)
	}
	atom, err := domain.ParseAtomID("atm_" + fixtureUUID)
	if err != nil {
		t.Fatal(err)
	}
	provider, err := domain.ParseProviderID("prv_" + fixtureUUID)
	if err != nil {
		t.Fatal(err)
	}
	message, err := domain.ParseMessageID("msg_" + fixtureUUID)
	if err != nil {
		t.Fatal(err)
	}
	return identities{
		repositoryOne: repositoryOne, repositoryTwo: repositoryTwo,
		threadOne: threadOne, threadTwo: threadTwo,
		artifact: artifact, atom: atom, provider: provider, message: message,
	}
}

func newFixtureModel(t *testing.T, ids identities) composer.Model {
	t.Helper()
	model, err := composer.NewModel(
		composer.ThreadBinding{ThreadID: ids.threadOne, RepositoryID: ids.repositoryOne},
		composer.ThreadBinding{ThreadID: ids.threadTwo, RepositoryID: ids.repositoryTwo},
	)
	if err != nil {
		t.Fatal(err)
	}
	return model
}

func reduce(t *testing.T, model composer.Model, action composer.Action) composer.Model {
	t.Helper()
	next, err := composer.Reduce(model, action)
	if err != nil {
		t.Fatal(err)
	}
	return next
}

func sendKey(t *testing.T, suffix string) composer.IdempotencyKey {
	t.Helper()
	if len(suffix) != 1 {
		t.Fatal("test key suffix must be one hexadecimal character")
	}
	key, err := composer.ParseIdempotencyKey("send-" + strings.Repeat("0", 31) + suffix)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func timelineConfirmation(
	t *testing.T,
	threadID domain.ThreadID,
	messageID domain.MessageID,
	sequence uint64,
) composer.TimelineCommitConfirmation {
	t.Helper()
	sessionID, err := domain.ParseSessionID("ses_" + fixtureUUID)
	if err != nil {
		t.Fatal(err)
	}
	event, err := (events.NewSessionEvent{
		SessionID:      sessionID,
		ThreadID:       threadID,
		Kind:           events.KindMessageFinal,
		Revision:       1,
		PayloadVersion: 1,
		Payload: events.Payload{MessageFinal: &events.MessageFinal{
			MessageID:    messageID,
			Role:         "user",
			RedactedBody: "committed user message",
		}},
	}).Build(sequence, time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	confirmation, err := composer.NewTimelineCommitConfirmation(event)
	if err != nil {
		t.Fatal(err)
	}
	return confirmation
}

func TestDraftsAreImmutableRestoredAndIsolatedPerThread(t *testing.T) {
	ids := fixtureIdentities(t)
	original := newFixtureModel(t, ids)
	file, err := composer.NewFileAttachment(ids.repositoryOne, ids.artifact, "internal/server.go")
	if err != nil {
		t.Fatal(err)
	}
	updated := reduce(t, original, composer.DraftTextChanged{
		ThreadID: ids.threadOne, Text: "first line\nsecond line",
	})
	updated = reduce(t, updated, composer.AttachmentAdded{
		ThreadID: ids.threadOne, Attachment: file,
	})
	updated = reduce(t, updated, composer.DraftTextChanged{
		ThreadID: ids.threadTwo, Text: "independent thread",
	})

	if !original.Draft(ids.threadOne).IsZero() {
		t.Fatal("immutable reducer mutated the original model")
	}
	if got := updated.Draft(ids.threadOne).Text(); got != "first line\nsecond line" {
		t.Fatalf("restored thread-one draft = %q", got)
	}
	if got := updated.Draft(ids.threadTwo).Text(); got != "independent thread" {
		t.Fatalf("thread-two draft = %q", got)
	}
	attachments := updated.Draft(ids.threadOne).Attachments()
	attachments[0] = composer.RepositoryAttachment{}
	if got := updated.Draft(ids.threadOne).Attachments()[0].Identity(); got != ids.artifact.String() {
		t.Fatalf("caller mutated retained attachment identity: %q", got)
	}
}

func TestWhitespaceAndPendingSendValidation(t *testing.T) {
	ids := fixtureIdentities(t)
	model := newFixtureModel(t, ids)
	model = reduce(t, model, composer.DraftTextChanged{ThreadID: ids.threadOne, Text: " \n\t "})
	if model.CanSubmit(ids.threadOne) {
		t.Fatal("whitespace-only draft became submittable")
	}
	if _, err := composer.Reduce(model, composer.SendStarted{
		ThreadID: ids.threadOne, Key: sendKey(t, "1"),
	}); !composer.IsComposerValueError(err) {
		t.Fatalf("whitespace send error = %v", err)
	}

	model = reduce(t, model, composer.DraftTextChanged{ThreadID: ids.threadOne, Text: "send me"})
	key := sendKey(t, "2")
	pending := reduce(t, model, composer.SendStarted{ThreadID: ids.threadOne, Key: key})
	if pending.CanSubmit(ids.threadOne) || pending.Draft(ids.threadOne).Text() != "send me" {
		t.Fatal("pending send cleared the draft or remained submittable")
	}
	attempt, ok := pending.Attempt(ids.threadOne)
	if !ok || attempt.Status() != composer.SendPending || attempt.Key() != key {
		t.Fatalf("pending attempt = %#v, exists=%t", attempt, ok)
	}
	if _, err := composer.Reduce(pending, composer.DraftTextChanged{
		ThreadID: ids.threadOne, Text: "edit while pending",
	}); !errors.Is(err, composer.ErrComposerBusy) {
		t.Fatalf("pending edit error = %v", err)
	}
}

func TestRetryRetainsKeyAndCommitAloneClearsDraft(t *testing.T) {
	ids := fixtureIdentities(t)
	model := newFixtureModel(t, ids)
	model = reduce(t, model, composer.DraftTextChanged{ThreadID: ids.threadOne, Text: "retry this"})
	key := sendKey(t, "3")
	model = reduce(t, model, composer.SendStarted{ThreadID: ids.threadOne, Key: key})
	failed := reduce(t, model, composer.SendFailureReceived{
		ThreadID: ids.threadOne, Key: key, Retryable: true,
		SafeMessage: "The message was not confirmed. Retry with the same request identity.",
	})
	if failed.Draft(ids.threadOne).Text() != "retry this" {
		t.Fatal("send failure cleared the uncommitted draft")
	}
	attempt, _ := failed.Attempt(ids.threadOne)
	if attempt.Key() != key || !attempt.Retryable() || attempt.Status() != composer.SendFailed {
		t.Fatalf("failed attempt = %#v", attempt)
	}

	retried := reduce(t, failed, composer.SendRetryRequested{ThreadID: ids.threadOne, Key: key})
	retryAttempt, _ := retried.Attempt(ids.threadOne)
	if retryAttempt.Key() != key || retryAttempt.Status() != composer.SendPending ||
		retryAttempt.Request().Text() != "retry this" {
		t.Fatalf("retry changed request identity or payload: %#v", retryAttempt)
	}
	accepted := reduce(t, retried, composer.SendAccepted{
		ThreadID: ids.threadOne, Key: key, MessageID: ids.message,
	})
	if accepted.Draft(ids.threadOne).Text() != "retry this" {
		t.Fatal("transport acceptance cleared the draft before timeline confirmation")
	}
	if _, err := composer.Reduce(accepted, composer.SendCommitConfirmed{
		ThreadID: ids.threadOne, Key: sendKey(t, "4"),
		Confirmation: timelineConfirmation(t, ids.threadOne, ids.message, 42),
	}); !errors.Is(err, composer.ErrSendAttemptMismatch) {
		t.Fatalf("mismatched commit error = %v", err)
	}
	if accepted.Draft(ids.threadOne).IsZero() {
		t.Fatal("mismatched commit cleared the draft")
	}
	committed := reduce(t, accepted, composer.SendCommitConfirmed{
		ThreadID: ids.threadOne, Key: key,
		Confirmation: timelineConfirmation(t, ids.threadOne, ids.message, 42),
	})
	if !committed.Draft(ids.threadOne).IsZero() {
		t.Fatal("confirmed committed message did not clear draft")
	}
	if _, exists := committed.Attempt(ids.threadOne); exists {
		t.Fatal("confirmed committed message retained transient attempt")
	}
}

func TestFailedDraftEditExplicitlyAbandonsRetryIdentity(t *testing.T) {
	ids := fixtureIdentities(t)
	model := newFixtureModel(t, ids)
	model = reduce(t, model, composer.DraftTextChanged{ThreadID: ids.threadOne, Text: "original"})
	key := sendKey(t, "5")
	model = reduce(t, model, composer.SendStarted{ThreadID: ids.threadOne, Key: key})
	model = reduce(t, model, composer.SendFailureReceived{
		ThreadID: ids.threadOne, Key: key, Retryable: true, SafeMessage: "Retry available.",
	})
	changed := reduce(t, model, composer.DraftTextChanged{ThreadID: ids.threadOne, Text: "changed"})
	if _, exists := changed.Attempt(ids.threadOne); exists {
		t.Fatal("editing a failed request retained its old idempotency key")
	}
	if changed.Draft(ids.threadOne).Text() != "changed" {
		t.Fatal("failed request edit was not retained")
	}
}

func TestOverridesUseTypedExactValues(t *testing.T) {
	ids := fixtureIdentities(t)
	model := newFixtureModel(t, ids)
	usd, err := domain.ParseCurrencyCode("USD")
	if err != nil {
		t.Fatal(err)
	}
	budget, err := domain.NewMoney(usd, 2500)
	if err != nil {
		t.Fatal(err)
	}
	modelOverride, err := composer.NewModelOverride(ids.provider, "gpt-fixture", "2026-07-31")
	if err != nil {
		t.Fatal(err)
	}
	model = reduce(t, model, composer.PolicyOverrideChanged{
		ThreadID: ids.threadOne, Value: domain.PolicyPresetCorrectness,
	})
	model = reduce(t, model, composer.BudgetOverrideChanged{ThreadID: ids.threadOne, Value: budget})
	model = reduce(t, model, composer.ModelOverrideChanged{ThreadID: ids.threadOne, Value: modelOverride})
	model = reduce(t, model, composer.EffortOverrideChanged{
		ThreadID: ids.threadOne, Value: domain.ReasoningEffortMaximum,
	})
	draft := model.Draft(ids.threadOne)
	if policy, ok := draft.PolicyOverride(); !ok || policy != domain.PolicyPresetCorrectness {
		t.Fatalf("policy override = %q, %t", policy, ok)
	}
	if got, ok := draft.BudgetOverride(); !ok || got != budget {
		t.Fatalf("budget override = %#v, %t", got, ok)
	}
	if got, ok := draft.ModelOverride(); !ok || got.Key() != modelOverride.Key() {
		t.Fatalf("model override = %#v, %t", got, ok)
	}
	if effort, ok := draft.EffortOverride(); !ok || effort != domain.ReasoningEffortMaximum {
		t.Fatalf("effort override = %q, %t", effort, ok)
	}
	if _, err := composer.Reduce(model, composer.BudgetOverrideChanged{
		ThreadID: ids.threadOne, Value: domain.Money{Currency: usd, MinorUnits: 0},
	}); !composer.IsComposerValueError(err) {
		t.Fatalf("zero hard budget error = %v", err)
	}
}

func TestAttachmentsRequireServerIdentitiesAndThreadRepository(t *testing.T) {
	ids := fixtureIdentities(t)
	model := newFixtureModel(t, ids)
	file, err := composer.NewFileAttachment(ids.repositoryOne, ids.artifact, "internal/server.go")
	if err != nil {
		t.Fatal(err)
	}
	symbol, err := composer.NewSymbolAttachment(ids.repositoryOne, ids.atom, "ServeLoopback")
	if err != nil {
		t.Fatal(err)
	}
	model = reduce(t, model, composer.AttachmentAdded{ThreadID: ids.threadOne, Attachment: file})
	model = reduce(t, model, composer.AttachmentAdded{ThreadID: ids.threadOne, Attachment: symbol})
	model = reduce(t, model, composer.AttachmentAdded{ThreadID: ids.threadOne, Attachment: file})
	if got := len(model.Draft(ids.threadOne).Attachments()); got != 2 {
		t.Fatalf("deduplicated attachment count = %d", got)
	}
	if _, err := composer.Reduce(model, composer.AttachmentAdded{
		ThreadID: ids.threadTwo, Attachment: file,
	}); !composer.IsComposerValueError(err) {
		t.Fatalf("cross-repository attachment error = %v", err)
	}
	model = reduce(t, model, composer.AttachmentRemoved{
		ThreadID: ids.threadOne, AttachmentKey: file.Key(),
	})
	remaining := model.Draft(ids.threadOne).Attachments()
	if len(remaining) != 1 || remaining[0].Key() != symbol.Key() {
		t.Fatalf("remaining attachments = %#v", remaining)
	}

	typeOfAttachment := reflect.TypeFor[composer.RepositoryAttachment]()
	for index := range typeOfAttachment.NumField() {
		if strings.Contains(strings.ToLower(typeOfAttachment.Field(index).Name), "path") {
			t.Fatalf("attachment authority exposes browser path field %q", typeOfAttachment.Field(index).Name)
		}
	}
}

func TestGeneratedIdempotencyKeysUseCanonicalOpaqueFormat(t *testing.T) {
	first, err := composer.NewIdempotencyKey()
	if err != nil {
		t.Fatal(err)
	}
	second, err := composer.NewIdempotencyKey()
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("independent sends received the same idempotency key")
	}
	if _, err := composer.ParseIdempotencyKey(string(first)); err != nil {
		t.Fatalf("generated key is not canonical: %v", err)
	}
}
