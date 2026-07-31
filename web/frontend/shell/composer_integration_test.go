package shell_test

import (
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/composer"
	"codeflux.dev/codeflux/web/frontend/routes"
	"codeflux.dev/codeflux/web/frontend/shell"
	"codeflux.dev/codeflux/web/frontend/state"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

const composerFixtureUUID = "01890f3c-4a00-7abc-8def-0123456789ab"

func mountedComposerProps(t *testing.T) composer.Props {
	t.Helper()
	repositoryID, err := domain.ParseRepositoryID("repo_" + composerFixtureUUID)
	if err != nil {
		t.Fatal(err)
	}
	threadID, err := domain.ParseThreadID("thr_" + composerFixtureUUID)
	if err != nil {
		t.Fatal(err)
	}
	artifactID, err := domain.ParseArtifactID("art_" + composerFixtureUUID)
	if err != nil {
		t.Fatal(err)
	}
	attachment, err := composer.NewFileAttachment(
		repositoryID, artifactID, "internal/application/send_message.go",
	)
	if err != nil {
		t.Fatal(err)
	}
	model, err := composer.NewModel(composer.ThreadBinding{
		ThreadID: threadID, RepositoryID: repositoryID,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []composer.Action{
		composer.DraftTextChanged{ThreadID: threadID, Text: "Implement the mounted composer\nwith retained draft state."},
		composer.AttachmentAdded{ThreadID: threadID, Attachment: attachment},
		composer.PolicyOverrideChanged{ThreadID: threadID, Value: domain.PolicyPresetCorrectness},
		composer.EffortOverrideChanged{ThreadID: threadID, Value: domain.ReasoningEffortExtended},
	} {
		model, err = composer.Reduce(model, action)
		if err != nil {
			t.Fatal(err)
		}
	}
	usd, err := domain.ParseCurrencyCode("USD")
	if err != nil {
		t.Fatal(err)
	}
	return composer.Props{
		View:           composer.View(model, threadID, domain.TaskStateRunning),
		BudgetCurrency: usd,
		OnTextChange:   func(string) {}, OnSubmitRequested: func() {},
		OnRetryRequested:         func(composer.IdempotencyKey) {},
		OnPolicyChange:           func(domain.PolicyPreset, bool) {},
		OnBudgetMinorUnitsChange: func(string) {},
		OnModelChange:            func(composer.ModelOverride, bool) {},
		OnEffortChange:           func(domain.ReasoningEffort, bool) {},
		OnOpenAttachmentPicker:   func() {}, OnRemoveAttachment: func(string) {},
		OnTaskAction: func(composer.TaskAction) {},
	}
}

func TestAppRootMountsTypedComposerInConversationWorkspace(t *testing.T) {
	markup := render(t, ui.CreateElement(shell.AppRoot, shell.RootProps{
		Snapshot: readySnapshot(), Route: routes.Route{Name: routes.ThreadWorkspace},
		Tokens: tokens(t), Composer: mountedComposerProps(t),
	}))
	for _, want := range []string{
		`data-component="task-workspace-shell"`,
		`data-component="conversation-pane"`,
		`data-component="composer"`,
		`data-disabled="false"`,
		`id="thread-composer"`,
		`aria-multiline="true"`,
		"Implement the mounted composer\nwith retained draft state.",
		`data-component="attachment-chip"`,
		`data-server-identity="art_` + composerFixtureUUID + `"`,
		`selected value="correctness"`,
		`selected value="extended"`,
		`aria-label="Hard budget in exact USD minor units"`,
		`data-task-action="pause"`,
		`data-task-action="stop"`,
		`data-immediately-reachable="true"`,
		`id="composer-submit"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("mounted workspace composer missing %q: %s", want, markup)
		}
	}
	if strings.Count(markup, `data-component="composer"`) != 1 {
		t.Fatalf("composer mount count = %d", strings.Count(markup, `data-component="composer"`))
	}
}

func TestConversationDisablesMountedComposerWhenRemoteStateIsUncertain(t *testing.T) {
	markup := render(t, ui.CreateElement(shell.ConversationPane, shell.ConversationPaneProps{
		State: state.DataDisconnected, Composer: mountedComposerProps(t),
	}))
	for _, want := range []string{
		`data-component="composer"`,
		`data-disabled="false"`,
		`data-mutation-disabled="true"`,
		`data-mutation-disabled-reason="Conversation state is not current"`,
		`id="thread-composer"`,
		`data-task-action="stop"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("uncertain conversation composer missing %q: %s", want, markup)
		}
	}
	sendAt := strings.Index(markup, `id="composer-submit"`)
	if sendAt < 0 {
		t.Fatalf("disconnected composer lost send control: %s", markup)
	}
	sendStart := strings.LastIndex(markup[:sendAt], "<button")
	if sendStart < 0 || !strings.Contains(markup[sendStart:sendAt], "disabled") {
		t.Fatalf("disconnected composer send remained actionable: %s", markup)
	}
	textareaAt := strings.Index(markup, `id="thread-composer"`)
	if textareaAt < 0 {
		t.Fatalf("disconnected composer lost textarea: %s", markup)
	}
	textareaStart := strings.LastIndex(markup[:textareaAt], "<textarea")
	if textareaStart < 0 || strings.Contains(markup[textareaStart:textareaAt], " disabled") {
		t.Fatalf("disconnected composer draft is not locally editable: %s", markup)
	}
}

func TestWorkspaceDisablesMountedComposerWhenSessionIsNotLive(t *testing.T) {
	store := state.NewStore(readySnapshot()).ReduceRemote(state.SessionChanged{
		Session: state.SessionView{
			Bootstrap: state.BootstrapReady, Connection: state.ConnectionDegraded,
		},
	})
	markup := render(t, ui.CreateElement(shell.TaskWorkspaceShell, shell.TaskWorkspaceProps{
		Snapshot: store.Snapshot(), Tokens: tokens(t), Composer: mountedComposerProps(t),
	}))
	for _, want := range []string{
		`data-component="composer"`,
		`data-disabled="false"`,
		`data-mutation-disabled="true"`,
		`data-mutation-disabled-reason="Connection certainty is degraded; this draft is preserved"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("recovering workspace composer missing %q: %s", want, markup)
		}
	}
}

func TestWorkspaceGatesLocalPreviewTransportWhenSessionIsOffline(t *testing.T) {
	store := state.NewStore(readySnapshot()).ReduceRemote(state.SessionChanged{
		Session: state.SessionView{
			Bootstrap: state.BootstrapReady, Connection: state.ConnectionDisconnected,
		},
	})
	composerProps := mountedComposerProps(t)
	composerProps.TransportMode = "authoritative-bridge-with-local-preview-fallback"
	markup := render(t, ui.CreateElement(shell.TaskWorkspaceShell, shell.TaskWorkspaceProps{
		Snapshot: store.Snapshot(), Tokens: tokens(t), Composer: composerProps,
	}))
	for _, want := range []string{
		`data-component="composer"`,
		`data-disabled="false"`,
		`data-mutation-disabled="true"`,
		`data-mutation-disabled-reason="Local Disconnected: reconnect to send this draft"`,
		`id="composer-mutation-disabled-reason"`,
		`role="status"`,
		`aria-describedby="composer-mutation-disabled-reason"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("offline local-preview composer missing %q: %s", want, markup)
		}
	}
	sendAt := strings.Index(markup, `id="composer-submit"`)
	if sendAt < 0 {
		t.Fatalf("offline composer lost Send: %s", markup)
	}
	sendStart := strings.LastIndex(markup[:sendAt], "<button")
	if sendStart < 0 || !strings.Contains(markup[sendStart:sendAt], "disabled") {
		t.Fatalf("offline composer left Send actionable: %s", markup)
	}
	textareaAt := strings.Index(markup, `id="thread-composer"`)
	if textareaAt < 0 {
		t.Fatalf("offline composer lost draft editor: %s", markup)
	}
	textareaStart := strings.LastIndex(markup[:textareaAt], "<textarea")
	if textareaStart < 0 || strings.Contains(markup[textareaStart:textareaAt], " disabled") {
		t.Fatalf("offline composer disabled local draft editing: %s", markup)
	}
}
