package composer_test

import (
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/composer"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func renderComposer(t *testing.T, props composer.Props) string {
	t.Helper()
	markup, err := ui.RenderToString(ui.CreateElement(composer.Composer, props))
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func TestComposerRendersMultilineOverridesAttachmentsAndReachableStop(t *testing.T) {
	ids := fixtureIdentities(t)
	model := newFixtureModel(t, ids)
	file, err := composer.NewFileAttachment(ids.repositoryOne, ids.artifact, "internal/server.go")
	if err != nil {
		t.Fatal(err)
	}
	modelOverride, err := composer.NewModelOverride(ids.provider, "gpt-fixture", "2026-07-31")
	if err != nil {
		t.Fatal(err)
	}
	usd, _ := domain.ParseCurrencyCode("USD")
	budget, _ := domain.NewMoney(usd, 2500)
	model = reduce(t, model, composer.DraftTextChanged{
		ThreadID: ids.threadOne, Text: "line one\nline two",
	})
	model = reduce(t, model, composer.AttachmentAdded{ThreadID: ids.threadOne, Attachment: file})
	model = reduce(t, model, composer.PolicyOverrideChanged{
		ThreadID: ids.threadOne, Value: domain.PolicyPresetCorrectness,
	})
	model = reduce(t, model, composer.BudgetOverrideChanged{ThreadID: ids.threadOne, Value: budget})
	model = reduce(t, model, composer.ModelOverrideChanged{ThreadID: ids.threadOne, Value: modelOverride})
	model = reduce(t, model, composer.EffortOverrideChanged{
		ThreadID: ids.threadOne, Value: domain.ReasoningEffortMaximum,
	})
	markup := renderComposer(t, composer.Props{
		View:           composer.View(model, ids.threadOne, domain.TaskStateRunning),
		BudgetCurrency: usd,
		ModelOptions:   []composer.ModelOption{{Value: modelOverride, Label: "Fixture model"}},
		OnTextChange:   func(string) {}, OnSubmitRequested: func() {},
		OnRetryRequested:         func(composer.IdempotencyKey) {},
		OnPolicyChange:           func(domain.PolicyPreset, bool) {},
		OnBudgetMinorUnitsChange: func(string) {},
		OnModelChange:            func(composer.ModelOverride, bool) {},
		OnEffortChange:           func(domain.ReasoningEffort, bool) {},
		OnOpenAttachmentPicker:   func() {}, OnRemoveAttachment: func(string) {},
		OnTaskAction: func(composer.TaskAction) {},
	})
	for _, want := range []string{
		`data-component="composer"`,
		`data-send-state="idle"`,
		`data-stop-reachable="true"`,
		`<textarea`,
		`rows="2"`,
		`aria-multiline="true"`,
		`data-keyboard="enter-submit-shift-enter-newline"`,
		"line one\nline two",
		"Enter sends. Shift+Enter inserts a newline.",
		`data-component="composer-advanced-options"`,
		`aria-label="Show policy, budget, model, and effort options"`,
		// The overrides open in a modal now, so the trigger is a control and the
		// fields live behind it rather than unfolding inside the composer.
		`id="composer-options"`,
		`data-modal="composer-options-modal"`,
		`aria-label="Cost speed correctness policy"`,
		`aria-label="Hard budget in exact USD minor units"`,
		`Hard budget`,
		`Whole cents. 5000 is $50.00.`,
		`value="2500"`,
		`aria-label="Optional model override"`,
		`aria-label="Optional reasoning effort override"`,
		`aria-label="Attach file or symbol"`,
		`>Use default policy</option>`,
		`>Correctness</option>`,
		`>Use default model</option>`,
		`>Fixture model</option>`,
		`>Use default effort</option>`,
		`>Maximum</option>`,
		`data-component="attachment-chip"`,
		`data-kind="file"`,
		`data-server-identity="` + ids.artifact.String() + `"`,
		`aria-label="Remove attachment internal/server.go"`,
		`data-task-action="stop"`,
		`data-immediately-reachable="true"`,
		`aria-label="Stop"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("composer markup missing %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, `type="file"`) {
		t.Fatalf("composer exposed a browser file-path picker: %s", markup)
	}
}

func TestComposerAttachmentPickerUsesOnlyServerResolvedChoices(t *testing.T) {
	ids := fixtureIdentities(t)
	model := newFixtureModel(t, ids)
	file, err := composer.NewFileAttachment(ids.repositoryOne, ids.artifact, "internal/server.go")
	if err != nil {
		t.Fatal(err)
	}
	symbol, err := composer.NewSymbolAttachment(ids.repositoryOne, ids.atom, "server.Run")
	if err != nil {
		t.Fatal(err)
	}
	markup := renderComposer(t, composer.Props{
		View:                      composer.View(model, ids.threadOne, domain.TaskStateDraft),
		AttachmentPickerOpen:      true,
		AttachmentOptions:         []composer.RepositoryAttachment{file, symbol},
		OnOpenAttachmentPicker:    func() {},
		OnAttachmentSelected:      func(composer.RepositoryAttachment) {},
		OnAttachmentPickerDismiss: func() {},
		OnTextChange:              func(string) {},
	})
	for _, want := range []string{
		`id="composer-attach"`,
		`data-component="repository-attachment-picker"`,
		`data-authority="server-identities-only"`,
		`data-focus-return="composer-attach"`,
		`aria-label="Attach internal/server.go"`,
		`aria-label="Attach server.Run"`,
		"Browser file paths are never accepted.",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("attachment picker missing %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, `type="file"`) {
		t.Fatalf("attachment picker exposed browser path authority: %s", markup)
	}
}

func TestComposerRendersStateAppropriateTaskActions(t *testing.T) {
	ids := fixtureIdentities(t)
	model := newFixtureModel(t, ids)
	tests := []struct {
		name  string
		state domain.TaskState
		want  []composer.TaskAction
		not   []composer.TaskAction
	}{
		{
			name: "running", state: domain.TaskStateRunning,
			want: []composer.TaskAction{composer.ActionPause, composer.ActionStop, composer.ActionInspectGraph},
			not:  []composer.TaskAction{composer.ActionResume, composer.ActionInspectEvidence},
		},
		{
			name: "paused", state: domain.TaskStatePaused,
			want: []composer.TaskAction{composer.ActionResume, composer.ActionReview, composer.ActionStop},
			not:  []composer.TaskAction{composer.ActionPause, composer.ActionInspectEvidence},
		},
		{
			name: "awaiting plan approval", state: domain.TaskStateAwaitingPlanApproval,
			want: []composer.TaskAction{composer.ActionApprovePlan, composer.ActionRequestChange, composer.ActionStop},
			not:  []composer.TaskAction{composer.ActionPause, composer.ActionResume},
		},
		{
			name: "awaiting authority", state: domain.TaskStateAwaitingAuthority,
			want: []composer.TaskAction{
				composer.ActionAllowOnce, composer.ActionAllowForTask, composer.ActionDeny, composer.ActionStop,
			},
			not: []composer.TaskAction{composer.ActionPause, composer.ActionResume},
		},
		{
			name: "completed", state: domain.TaskStateCompleted,
			want: []composer.TaskAction{composer.ActionInspectEvidence, composer.ActionStartRelatedTask},
			not:  []composer.TaskAction{composer.ActionPause, composer.ActionResume, composer.ActionStop},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			markup := renderComposer(t, composer.Props{
				View:         composer.View(model, ids.threadOne, test.state),
				OnTaskAction: func(composer.TaskAction) {},
			})
			for _, action := range test.want {
				marker := `data-task-action="` + string(action) + `"`
				if !strings.Contains(markup, marker) {
					t.Errorf("composer for %s missing %s: %s", test.state, action, markup)
				}
			}
			for _, action := range test.not {
				marker := `data-task-action="` + string(action) + `"`
				if strings.Contains(markup, marker) {
					t.Errorf("composer for %s unexpectedly rendered %s: %s", test.state, action, markup)
				}
			}
		})
	}
}

func TestMutationDisabledPreservesLocalDraftEditingButBlocksCommands(t *testing.T) {
	ids := fixtureIdentities(t)
	model := newFixtureModel(t, ids)
	model = reduce(t, model, composer.DraftTextChanged{
		ThreadID: ids.threadOne, Text: "offline draft",
	})
	markup := renderComposer(t, composer.Props{
		View:             composer.View(model, ids.threadOne, domain.TaskStateRunning),
		MutationDisabled: true, MutationDisabledReason: "Connection is recovering",
		OnTextChange: func(string) {}, OnSubmitRequested: func() {},
		OnTaskAction: func(composer.TaskAction) {},
	})
	for _, want := range []string{
		`data-disabled="false"`,
		`data-mutation-disabled="true"`,
		`data-mutation-disabled-reason="Connection is recovering"`,
		`id="composer-mutation-disabled-reason"`,
		`role="status"`,
		`aria-describedby="composer-mutation-disabled-reason"`,
		"offline draft",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("mutation-disabled composer missing %q: %s", want, markup)
		}
	}
	textareaAt := strings.Index(markup, `id="thread-composer"`)
	if textareaAt < 0 {
		t.Fatalf("mutation-disabled composer lost textarea: %s", markup)
	}
	textareaStart := strings.LastIndex(markup[:textareaAt], "<textarea")
	if textareaStart < 0 || strings.Contains(markup[textareaStart:textareaAt], " disabled") {
		t.Fatalf("mutation-disabled composer prevented local draft editing: %s", markup)
	}
	submitAt := strings.Index(markup, `id="composer-submit"`)
	if submitAt < 0 {
		t.Fatalf("mutation-disabled composer lost submit: %s", markup)
	}
	submitStart := strings.LastIndex(markup[:submitAt], "<button")
	if submitStart < 0 || !strings.Contains(markup[submitStart:submitAt], "disabled") {
		t.Fatalf("mutation-disabled composer left submit actionable: %s", markup)
	}
	stopAt := strings.Index(markup, `data-task-action="stop"`)
	if stopAt < 0 {
		t.Fatalf("mutation-disabled composer lost Stop: %s", markup)
	}
	stopButtonOffset := strings.Index(markup[stopAt:], "<button")
	if stopButtonOffset < 0 {
		t.Fatalf("mutation-disabled Stop lost button: %s", markup)
	}
	stopButtonAt := stopAt + stopButtonOffset
	stopButtonEndOffset := strings.Index(markup[stopButtonAt:], ">")
	if stopButtonEndOffset < 0 ||
		!strings.Contains(markup[stopButtonAt:stopButtonAt+stopButtonEndOffset], "disabled") {
		t.Fatalf("mutation-disabled composer left Stop actionable: %s", markup)
	}
}

func TestComposerDisablesWhitespaceAndBusySendButNotStop(t *testing.T) {
	ids := fixtureIdentities(t)
	model := newFixtureModel(t, ids)
	model = reduce(t, model, composer.DraftTextChanged{ThreadID: ids.threadOne, Text: "  \n"})
	markup := renderComposer(t, composer.Props{
		View: composer.View(model, ids.threadOne, domain.TaskStateRunning),
	})
	submitAt := strings.Index(markup, `id="composer-submit"`)
	if submitAt < 0 {
		t.Fatalf("whitespace composer lost submit control: %s", markup)
	}
	submitStart := strings.LastIndex(markup[:submitAt], "<button")
	submitEnd := strings.Index(markup[submitAt:], "</button>")
	if submitStart < 0 || submitEnd < 0 {
		t.Fatalf("could not isolate whitespace submit control: %s", markup)
	}
	submitMarkup := markup[submitStart : submitAt+submitEnd]
	if !strings.Contains(submitMarkup, `aria-busy="false"`) ||
		!strings.Contains(submitMarkup, `disabled`) {
		t.Fatalf("whitespace submit is not disabled: %s", markup)
	}

	model = reduce(t, model, composer.DraftTextChanged{ThreadID: ids.threadOne, Text: "send"})
	model = reduce(t, model, composer.SendStarted{ThreadID: ids.threadOne, Key: sendKey(t, "6")})
	markup = renderComposer(t, composer.Props{
		View:         composer.View(model, ids.threadOne, domain.TaskStateRunning),
		OnTaskAction: func(composer.TaskAction) {},
	})
	if !strings.Contains(markup, `data-send-state="pending"`) ||
		!strings.Contains(markup, `aria-busy="true"`) ||
		!strings.Contains(markup, `data-state="busy" disabled id="composer-submit"`) {
		t.Fatalf("pending send is not visibly busy: %s", markup)
	}
	stopAt := strings.Index(markup, `data-task-action="stop"`)
	if stopAt < 0 {
		t.Fatalf("pending composer lost Stop: %s", markup)
	}
	stopMarkup := markup[stopAt:]
	if end := strings.Index(stopMarkup, "</span>"); end >= 0 {
		stopMarkup = stopMarkup[:end]
	}
	if strings.Contains(stopMarkup, "disabled") {
		t.Fatalf("pending send disabled immediately reachable Stop: %s", stopMarkup)
	}
}

func TestComposerShowsExplicitRetryWithRetainedIdentity(t *testing.T) {
	ids := fixtureIdentities(t)
	model := newFixtureModel(t, ids)
	model = reduce(t, model, composer.DraftTextChanged{ThreadID: ids.threadOne, Text: "retry"})
	key := sendKey(t, "7")
	model = reduce(t, model, composer.SendStarted{ThreadID: ids.threadOne, Key: key})
	model = reduce(t, model, composer.SendFailureReceived{
		ThreadID: ids.threadOne, Key: key, Retryable: true,
		SafeMessage: "The committed message was not confirmed.",
	})
	markup := renderComposer(t, composer.Props{
		View: composer.View(model, ids.threadOne, domain.TaskStateDraft),
	})
	for _, want := range []string{
		`data-send-state="failed"`,
		`id="composer-send-error"`,
		"The committed message was not confirmed.",
		`id="composer-retry"`,
		`aria-label="Retry send"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("failed composer missing %q: %s", want, markup)
		}
	}
	view := composer.View(model, ids.threadOne, domain.TaskStateDraft)
	if view.IdempotencyKey != key {
		t.Fatalf("retry view key = %q, want %q", view.IdempotencyKey, key)
	}
}

func TestComposerShowsSafeAwaitingTimelineStateWithoutClearingDraft(t *testing.T) {
	ids := fixtureIdentities(t)
	model := newFixtureModel(t, ids)
	model = reduce(t, model, composer.DraftTextChanged{
		ThreadID: ids.threadOne, Text: "keep visible until confirmed",
	})
	key := sendKey(t, "d")
	model = reduce(t, model, composer.SendStarted{ThreadID: ids.threadOne, Key: key})
	model = reduce(t, model, composer.SendAccepted{
		ThreadID: ids.threadOne, Key: key, MessageID: ids.message,
	})

	markup := renderComposer(t, composer.Props{
		View:         composer.View(model, ids.threadOne, domain.TaskStateRunning),
		OnTaskAction: func(composer.TaskAction) {},
	})
	for _, want := range []string{
		`data-send-state="awaiting-confirmation"`,
		`id="composer-send-confirmation"`,
		`role="status"`,
		"Your draft is preserved until it appears in the authoritative timeline.",
		"keep visible until confirmed",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("awaiting-confirmation composer missing %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, `id="composer-retry"`) {
		t.Fatalf("unresolved accepted send exposed a duplicate-send retry: %s", markup)
	}
	stopAt := strings.Index(markup, `data-task-action="stop"`)
	if stopAt < 0 {
		t.Fatalf("awaiting-confirmation composer lost Stop: %s", markup)
	}
	stopMarkup := markup[stopAt:]
	if end := strings.Index(stopMarkup, "</button>"); end >= 0 {
		stopMarkup = stopMarkup[:end]
	}
	if strings.Contains(stopMarkup, "disabled") {
		t.Fatalf("awaiting-confirmation composer disabled Stop: %s", stopMarkup)
	}
}
