//go:build js && wasm

package main

import (
	"context"
	"strconv"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/composer"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

const previewComposerUUID = "01890f3c-4a00-7abc-8def-0123456789ab"
const previewThreadRevision uint64 = 0

// livePreviewComposer owns the local-first controlled composer state used by
// the generated browser shell. Durable transport remains behind the typed
// callbacks; the preview completes sends locally so every state is testable
// without inventing a remote session.
func livePreviewComposer(selectedGraph ui.State[string]) composer.Props {
	repositoryID, repositoryErr := domain.ParseRepositoryID("repo_" + previewComposerUUID)
	threadID, threadErr := domain.ParseThreadID("thr_" + previewComposerUUID)
	initial, modelErr := composer.NewModel(composer.ThreadBinding{
		ThreadID: threadID, RepositoryID: repositoryID,
	})
	modelState := ui.UseState(initial)
	taskState := ui.UseState(domain.TaskStateRunning)
	transportMode := ui.UseState("authoritative-bridge-with-local-preview-fallback")
	attachmentPickerOpen := ui.UseState(false)
	usd, currencyErr := domain.ParseCurrencyCode("USD")
	providerID, providerErr := domain.ParseProviderID("prv_" + previewComposerUUID)
	modelOptions, modelOptionsErr := previewModelOptions(providerID)
	artifactID, artifactErr := domain.ParseArtifactID("art_" + previewComposerUUID)
	fileAttachment, attachmentErr := composer.NewFileAttachment(
		repositoryID,
		artifactID,
		"web/frontend/shell/shell.go",
	)
	atomID, atomErr := domain.ParseAtomID("atm_" + previewComposerUUID)
	symbolAttachment, symbolAttachmentErr := composer.NewSymbolAttachment(
		repositoryID,
		atomID,
		"shell.AppRouter",
	)
	initErr := firstComposerError(
		repositoryErr,
		threadErr,
		modelErr,
		currencyErr,
		providerErr,
		modelOptionsErr,
		artifactErr,
		attachmentErr,
		atomErr,
		symbolAttachmentErr,
	)
	apply := func(action composer.Action) bool {
		next, err := composer.Reduce(modelState.Get(), action)
		if err != nil {
			return false
		}
		modelState.Set(next)
		return true
	}
	complete := func(command composerSendCommand) {
		ui.SafeGo("send mounted composer message", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			messageID, sendErr := sendComposerCommand(ctx, command)
			cancel()
			next, mode, err := settleComposerCommand(modelState.Get(), command, messageID, sendErr)
			if err == nil {
				modelState.Set(next)
				transportMode.Set(mode)
			}
		})
	}
	props := composer.Props{
		View:           composer.View(modelState.Get(), threadID, taskState.Get()),
		BudgetCurrency: usd,
		TransportMode:  transportMode.Get(),
		ModelOptions:   modelOptions,
		OnTextChange: func(value string) {
			apply(composer.DraftTextChanged{ThreadID: threadID, Text: value})
		},
		OnSubmitRequested: func() {
			key, err := composer.NewIdempotencyKey()
			if err != nil {
				return
			}
			draft := modelState.Get().Draft(threadID)
			if !apply(composer.SendStarted{ThreadID: threadID, Key: key}) {
				return
			}
			complete(composerSendCommand{
				ThreadID: threadID, ExpectedRevision: previewThreadRevision,
				Key: key, Draft: draft,
			})
		},
		OnRetryRequested: func(key composer.IdempotencyKey) {
			if apply(composer.SendRetryRequested{ThreadID: threadID, Key: key}) {
				attempt, ok := modelState.Get().Attempt(threadID)
				if ok {
					complete(composerSendCommand{
						ThreadID: threadID, ExpectedRevision: previewThreadRevision,
						Key: key, Draft: attempt.Request(),
					})
				}
			}
		},
		OnPolicyChange: func(value domain.PolicyPreset, clear bool) {
			apply(composer.PolicyOverrideChanged{
				ThreadID: threadID, Value: value, Clear: clear,
			})
		},
		OnBudgetMinorUnitsChange: func(raw string) {
			if strings.TrimSpace(raw) == "" {
				apply(composer.BudgetOverrideChanged{ThreadID: threadID, Clear: true})
				return
			}
			minorUnits, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				return
			}
			value, err := domain.NewMoney(usd, minorUnits)
			if err == nil {
				apply(composer.BudgetOverrideChanged{
					ThreadID: threadID, Value: value,
				})
			}
		},
		OnModelChange: func(value composer.ModelOverride, clear bool) {
			apply(composer.ModelOverrideChanged{
				ThreadID: threadID, Value: value, Clear: clear,
			})
		},
		OnEffortChange: func(value domain.ReasoningEffort, clear bool) {
			apply(composer.EffortOverrideChanged{
				ThreadID: threadID, Value: value, Clear: clear,
			})
		},
		AttachmentPickerOpen: attachmentPickerOpen.Get(),
		AttachmentOptions: []composer.RepositoryAttachment{
			fileAttachment,
			symbolAttachment,
		},
		OnOpenAttachmentPicker: func() {
			attachmentPickerOpen.Set(true)
		},
		OnAttachmentSelected: func(attachment composer.RepositoryAttachment) {
			apply(composer.AttachmentAdded{
				ThreadID: threadID, Attachment: attachment,
			})
			attachmentPickerOpen.Set(false)
		},
		OnAttachmentPickerDismiss: func() { attachmentPickerOpen.Set(false) },
		OnRemoveAttachment: func(key string) {
			apply(composer.AttachmentRemoved{
				ThreadID: threadID, AttachmentKey: key,
			})
		},
		OnTaskAction: func(action composer.TaskAction) {
			switch action {
			case composer.ActionStop:
				taskState.Set(domain.TaskStateCancelled)
			case composer.ActionPause:
				taskState.Set(domain.TaskStatePaused)
			case composer.ActionResume:
				taskState.Set(domain.TaskStateRunning)
			case composer.ActionInspectGraph:
				selectedGraph.Set("implementation")
			}
		},
	}
	if initErr != nil {
		props.Disabled = true
		props.DisabledReason = "Composer identities are unavailable"
	}
	return props
}

func firstComposerError(errors ...error) error {
	for _, err := range errors {
		if err != nil {
			return err
		}
	}
	return nil
}
