package composer_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/composer"
)

func TestThreadRepositoryBindingCannotBeReplaced(t *testing.T) {
	ids := fixtureIdentities(t)

	if _, err := composer.NewModel(
		composer.ThreadBinding{ThreadID: ids.threadOne, RepositoryID: ids.repositoryOne},
		composer.ThreadBinding{ThreadID: ids.threadOne, RepositoryID: ids.repositoryTwo},
	); !composer.IsComposerValueError(err) {
		t.Fatalf("conflicting constructor binding error = %v", err)
	}

	model := newFixtureModel(t, ids)
	next, err := composer.Reduce(model, composer.ThreadBound{
		ThreadID: ids.threadOne, RepositoryID: ids.repositoryTwo,
	})
	if !composer.IsComposerValueError(err) {
		t.Fatalf("conflicting reducer binding error = %v", err)
	}
	if !next.Draft(ids.threadOne).IsZero() || !model.Draft(ids.threadOne).IsZero() {
		t.Fatal("rejected repository replacement mutated composer state")
	}
}

func TestSendAttemptRetainsExactDraftAndIsolationAcrossThreads(t *testing.T) {
	ids := fixtureIdentities(t)
	model := newFixtureModel(t, ids)
	file, err := composer.NewFileAttachment(ids.repositoryOne, ids.artifact, "internal/server.go")
	if err != nil {
		t.Fatal(err)
	}
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

	model = reduce(t, model, composer.DraftTextChanged{ThreadID: ids.threadOne, Text: "first\nsecond"})
	model = reduce(t, model, composer.AttachmentAdded{ThreadID: ids.threadOne, Attachment: file})
	model = reduce(t, model, composer.PolicyOverrideChanged{
		ThreadID: ids.threadOne, Value: domain.PolicyPresetCorrectness,
	})
	model = reduce(t, model, composer.BudgetOverrideChanged{ThreadID: ids.threadOne, Value: budget})
	model = reduce(t, model, composer.ModelOverrideChanged{ThreadID: ids.threadOne, Value: modelOverride})
	model = reduce(t, model, composer.EffortOverrideChanged{
		ThreadID: ids.threadOne, Value: domain.ReasoningEffortExtended,
	})
	model = reduce(t, model, composer.DraftTextChanged{ThreadID: ids.threadTwo, Text: "thread two"})

	firstKey := sendKey(t, "8")
	model = reduce(t, model, composer.SendStarted{ThreadID: ids.threadOne, Key: firstKey})
	attempt, exists := model.Attempt(ids.threadOne)
	if !exists {
		t.Fatal("thread-one send attempt was not retained")
	}
	request := attempt.Request()
	if request.Text() != "first\nsecond" || len(request.Attachments()) != 1 {
		t.Fatalf("retained request lost text or attachments: %#v", request)
	}
	if got, ok := request.PolicyOverride(); !ok || got != domain.PolicyPresetCorrectness {
		t.Fatalf("retained policy = %q, %t", got, ok)
	}
	if got, ok := request.BudgetOverride(); !ok || got != budget {
		t.Fatalf("retained budget = %#v, %t", got, ok)
	}
	if got, ok := request.ModelOverride(); !ok || got.Key() != modelOverride.Key() {
		t.Fatalf("retained model = %#v, %t", got, ok)
	}
	if got, ok := request.EffortOverride(); !ok || got != domain.ReasoningEffortExtended {
		t.Fatalf("retained effort = %q, %t", got, ok)
	}

	attachments := request.Attachments()
	attachments[0] = composer.RepositoryAttachment{}
	if got := attempt.Request().Attachments()[0].Identity(); got != ids.artifact.String() {
		t.Fatalf("caller mutated retained send payload identity: %q", got)
	}

	// A pending command in one thread must not freeze or overwrite another
	// thread's browser-local draft and command lifecycle.
	model = reduce(t, model, composer.DraftTextChanged{
		ThreadID: ids.threadTwo, Text: "thread two edited independently",
	})
	secondKey := sendKey(t, "9")
	model = reduce(t, model, composer.SendStarted{ThreadID: ids.threadTwo, Key: secondKey})
	model = reduce(t, model, composer.SendFailureReceived{
		ThreadID: ids.threadOne, Key: firstKey, Retryable: true,
		SafeMessage: "The committed message was not confirmed.",
	})
	model = reduce(t, model, composer.SendRetryRequested{ThreadID: ids.threadOne, Key: firstKey})

	firstRetry, _ := model.Attempt(ids.threadOne)
	secondPending, _ := model.Attempt(ids.threadTwo)
	if firstRetry.Key() != firstKey || firstRetry.Status() != composer.SendPending {
		t.Fatalf("thread-one retry identity/status = %q/%q", firstRetry.Key(), firstRetry.Status())
	}
	if secondPending.Key() != secondKey || secondPending.Status() != composer.SendPending {
		t.Fatalf("thread-two attempt was changed by thread-one retry: %#v", secondPending)
	}

	model = reduce(t, model, composer.SendAccepted{
		ThreadID: ids.threadOne, Key: firstKey, MessageID: ids.message,
	})
	model = reduce(t, model, composer.SendCommitConfirmed{
		ThreadID: ids.threadOne, Key: firstKey,
		Confirmation: timelineConfirmation(t, ids.threadOne, ids.message, 19),
	})
	if !model.Draft(ids.threadOne).IsZero() {
		t.Fatal("committed thread-one draft was not cleared")
	}
	if got := model.Draft(ids.threadTwo).Text(); got != "thread two edited independently" {
		t.Fatalf("thread-one commit changed thread-two draft: %q", got)
	}
	if attempt, ok := model.Attempt(ids.threadTwo); !ok || attempt.Key() != secondKey {
		t.Fatalf("thread-one commit changed thread-two attempt: %#v, %t", attempt, ok)
	}
}

func TestFailedSendRequiresRetryabilityOrExplicitAbandonment(t *testing.T) {
	ids := fixtureIdentities(t)
	model := newFixtureModel(t, ids)
	model = reduce(t, model, composer.DraftTextChanged{ThreadID: ids.threadOne, Text: "retain me"})
	key := sendKey(t, "a")
	model = reduce(t, model, composer.SendStarted{ThreadID: ids.threadOne, Key: key})
	failed := reduce(t, model, composer.SendFailureReceived{
		ThreadID: ids.threadOne, Key: key, Retryable: false,
		SafeMessage: "The request cannot be retried safely.",
	})

	next, err := composer.Reduce(failed, composer.SendRetryRequested{ThreadID: ids.threadOne, Key: key})
	if !errors.Is(err, composer.ErrSendNotRetryable) {
		t.Fatalf("nonretryable retry error = %v", err)
	}
	if attempt, ok := next.Attempt(ids.threadOne); !ok || attempt.Status() != composer.SendFailed {
		t.Fatalf("rejected retry mutated failed attempt: %#v, %t", attempt, ok)
	}
	if got := next.Draft(ids.threadOne).Text(); got != "retain me" {
		t.Fatalf("rejected retry changed draft: %q", got)
	}

	abandoned := reduce(t, failed, composer.SendAbandoned{ThreadID: ids.threadOne, Key: key})
	if _, ok := abandoned.Attempt(ids.threadOne); ok {
		t.Fatal("explicit abandonment retained the failed attempt identity")
	}
	if got := abandoned.Draft(ids.threadOne).Text(); got != "retain me" {
		t.Fatalf("explicit abandonment discarded unsent draft: %q", got)
	}
	newKey := sendKey(t, "b")
	started := reduce(t, abandoned, composer.SendStarted{ThreadID: ids.threadOne, Key: newKey})
	if attempt, ok := started.Attempt(ids.threadOne); !ok || attempt.Key() != newKey {
		t.Fatalf("new send did not receive a new retained identity: %#v, %t", attempt, ok)
	}
}

func TestInvalidCommitAndControlChangesLeaveDraftIntact(t *testing.T) {
	ids := fixtureIdentities(t)
	model := newFixtureModel(t, ids)
	model = reduce(t, model, composer.DraftTextChanged{ThreadID: ids.threadOne, Text: "do not clear"})
	key := sendKey(t, "c")
	pending := reduce(t, model, composer.SendStarted{ThreadID: ids.threadOne, Key: key})

	next, err := composer.Reduce(pending, composer.SendAccepted{
		ThreadID: ids.threadOne, Key: key,
	})
	if !composer.IsComposerValueError(err) {
		t.Fatalf("zero-message commit error = %v", err)
	}
	if next.Draft(ids.threadOne).Text() != "do not clear" {
		t.Fatal("unconfirmed commit cleared the draft")
	}
	if attempt, ok := next.Attempt(ids.threadOne); !ok || attempt.Key() != key {
		t.Fatal("unconfirmed commit cleared the retained send identity")
	}

	// Editing is intentionally blocked while pending; this also proves invalid
	// policy input cannot bypass the retained command boundary.
	if changed, err := composer.Reduce(pending, composer.PolicyOverrideChanged{
		ThreadID: ids.threadOne, Value: domain.PolicyPreset("unsupported"),
	}); !errors.Is(err, composer.ErrComposerBusy) || changed.Draft(ids.threadOne).Text() != "do not clear" {
		t.Fatalf("pending invalid control change = %#v, %v", changed.Draft(ids.threadOne), err)
	}
}

func TestInvalidOverridesAreRejectedWithoutPartialDraftMutation(t *testing.T) {
	ids := fixtureIdentities(t)
	model := newFixtureModel(t, ids)
	model = reduce(t, model, composer.DraftTextChanged{ThreadID: ids.threadOne, Text: "stable"})
	usd, err := domain.ParseCurrencyCode("USD")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		action composer.Action
	}{
		{
			name: "policy",
			action: composer.PolicyOverrideChanged{
				ThreadID: ids.threadOne, Value: domain.PolicyPreset("unsupported"),
			},
		},
		{
			name: "budget",
			action: composer.BudgetOverrideChanged{
				ThreadID: ids.threadOne, Value: domain.Money{Currency: usd},
			},
		},
		{
			name:   "model",
			action: composer.ModelOverrideChanged{ThreadID: ids.threadOne},
		},
		{
			name: "effort",
			action: composer.EffortOverrideChanged{
				ThreadID: ids.threadOne, Value: domain.ReasoningEffort("unsupported"),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			next, err := composer.Reduce(model, test.action)
			if !composer.IsComposerValueError(err) {
				t.Fatalf("invalid %s override error = %v", test.name, err)
			}
			draft := next.Draft(ids.threadOne)
			if draft.Text() != "stable" {
				t.Fatalf("invalid %s override changed text: %q", test.name, draft.Text())
			}
			if _, ok := draft.PolicyOverride(); ok {
				t.Fatalf("invalid %s override set policy", test.name)
			}
			if _, ok := draft.BudgetOverride(); ok {
				t.Fatalf("invalid %s override set budget", test.name)
			}
			if _, ok := draft.ModelOverride(); ok {
				t.Fatalf("invalid %s override set model", test.name)
			}
			if _, ok := draft.EffortOverride(); ok {
				t.Fatalf("invalid %s override set effort", test.name)
			}
		})
	}
}

func TestAttachmentSelectionIsBoundedAndIdentityOnly(t *testing.T) {
	ids := fixtureIdentities(t)
	model := newFixtureModel(t, ids)

	for index := 0; index < 32; index++ {
		artifactID, err := domain.ParseArtifactID(fmt.Sprintf(
			"art_01890f3c-4a00-7abc-8def-%012x", index+1,
		))
		if err != nil {
			t.Fatal(err)
		}
		attachment, err := composer.NewFileAttachment(
			ids.repositoryOne, artifactID, fmt.Sprintf("internal/file-%02d.go", index+1),
		)
		if err != nil {
			t.Fatal(err)
		}
		model = reduce(t, model, composer.AttachmentAdded{
			ThreadID: ids.threadOne, Attachment: attachment,
		})
	}
	if got := len(model.Draft(ids.threadOne).Attachments()); got != 32 {
		t.Fatalf("attachment count = %d, want 32", got)
	}

	extraID, err := domain.ParseArtifactID("art_01890f3c-4a00-7abc-8def-000000000021")
	if err != nil {
		t.Fatal(err)
	}
	extra, err := composer.NewFileAttachment(ids.repositoryOne, extraID, "internal/extra.go")
	if err != nil {
		t.Fatal(err)
	}
	next, err := composer.Reduce(model, composer.AttachmentAdded{
		ThreadID: ids.threadOne, Attachment: extra,
	})
	if !composer.IsComposerValueError(err) {
		t.Fatalf("attachment overflow error = %v", err)
	}
	if got := len(next.Draft(ids.threadOne).Attachments()); got != 32 {
		t.Fatalf("attachment overflow mutated selection: %d", got)
	}

	if _, err := composer.NewFileAttachment(domain.RepositoryID{}, ids.artifact, "browser/path.go"); !composer.IsComposerValueError(err) {
		t.Fatalf("attachment without server repository identity error = %v", err)
	}
	if _, err := composer.NewFileAttachment(ids.repositoryOne, domain.ArtifactID{}, "browser/path.go"); !composer.IsComposerValueError(err) {
		t.Fatalf("attachment without server artifact identity error = %v", err)
	}
}

func TestDraftBoundsAndOverrideClearsAreDeterministic(t *testing.T) {
	ids := fixtureIdentities(t)
	model := newFixtureModel(t, ids)
	exactLimit := strings.Repeat("x", 64*1024)
	model = reduce(t, model, composer.DraftTextChanged{ThreadID: ids.threadOne, Text: exactLimit})
	next, err := composer.Reduce(model, composer.DraftTextChanged{
		ThreadID: ids.threadOne, Text: exactLimit + "x",
	})
	if !composer.IsComposerValueError(err) {
		t.Fatalf("oversized draft error = %v", err)
	}
	if got := len(next.Draft(ids.threadOne).Text()); got != len(exactLimit) {
		t.Fatalf("oversized draft mutated retained text length: %d", got)
	}

	usd, err := domain.ParseCurrencyCode("USD")
	if err != nil {
		t.Fatal(err)
	}
	budget, err := domain.NewMoney(usd, 1)
	if err != nil {
		t.Fatal(err)
	}
	modelOverride, err := composer.NewModelOverride(ids.provider, "gpt-fixture", "stable")
	if err != nil {
		t.Fatal(err)
	}
	model = reduce(t, model, composer.PolicyOverrideChanged{
		ThreadID: ids.threadOne, Value: domain.PolicyPresetBalanced,
	})
	model = reduce(t, model, composer.BudgetOverrideChanged{ThreadID: ids.threadOne, Value: budget})
	model = reduce(t, model, composer.ModelOverrideChanged{ThreadID: ids.threadOne, Value: modelOverride})
	model = reduce(t, model, composer.EffortOverrideChanged{
		ThreadID: ids.threadOne, Value: domain.ReasoningEffortMinimal,
	})
	model = reduce(t, model, composer.PolicyOverrideChanged{ThreadID: ids.threadOne, Clear: true})
	model = reduce(t, model, composer.BudgetOverrideChanged{ThreadID: ids.threadOne, Clear: true})
	model = reduce(t, model, composer.ModelOverrideChanged{ThreadID: ids.threadOne, Clear: true})
	model = reduce(t, model, composer.EffortOverrideChanged{ThreadID: ids.threadOne, Clear: true})
	draft := model.Draft(ids.threadOne)
	if _, ok := draft.PolicyOverride(); ok {
		t.Fatal("cleared policy override remains present")
	}
	if _, ok := draft.BudgetOverride(); ok {
		t.Fatal("cleared budget override remains present")
	}
	if _, ok := draft.ModelOverride(); ok {
		t.Fatal("cleared model override remains present")
	}
	if _, ok := draft.EffortOverride(); ok {
		t.Fatal("cleared effort override remains present")
	}
	if draft.Text() != exactLimit {
		t.Fatal("clearing overrides changed draft text")
	}
}
