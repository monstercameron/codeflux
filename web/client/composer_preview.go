//go:build js && wasm

package main

import (
	"context"
	"strconv"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
	"codeflux.dev/codeflux/web/frontend/composer"
	"codeflux.dev/codeflux/web/frontend/threadrail"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

const previewComposerUUID = "01890f3c-4a00-7abc-8def-0123456789ab"

// livePreviewComposer owns browser-local drafts while every send is bound to
// the selected server-issued thread identity and exact authoritative revision.
func livePreviewComposer(thread threadrail.Thread, latest events.SessionEvent, _ ui.State[string]) composer.Props {
	threadID := thread.ID()
	repositoryID := thread.RepositoryID()
	initial, modelErr := composer.NewModel()
	modelState := ui.UseState(initial)
	latestEvent := ui.UseRef(latest)
	latestEvent.Set(latest)
	transportMode := ui.UseState(composerTransportAuthoritative)
	// The kind of change is the one thing about a task nothing can observe, so
	// it is held here and sent with the request rather than defaulted anywhere.
	taskClass := ui.UseState("")
	usd, currencyErr := domain.ParseCurrencyCode("USD")
	providerID, providerErr := domain.ParseProviderID("prv_" + previewComposerUUID)
	modelOptions, modelOptionsErr := previewModelOptions(providerID)
	initErr := firstComposerError(
		modelErr,
		currencyErr,
		providerErr,
		modelOptionsErr,
	)
	apply := func(action composer.Action) bool {
		applied := false
		modelState.Update(func(current composer.Model) composer.Model {
			next, err := composer.Reduce(current, action)
			if err != nil {
				return current
			}
			applied = true
			return next
		})
		return applied
	}
	bindingDependency := threadID.String() + "|" + repositoryID.String()
	settlementAttempt, settlementAttemptPresent := modelState.Get().Attempt(threadID)
	settlementDependency := composerSettlementDependency(
		threadID, latest, settlementAttempt, settlementAttemptPresent,
	)
	ui.UseEffectOf(func() func() {
		if threadID.IsZero() || repositoryID.IsZero() {
			return nil
		}
		apply(composer.ThreadBound{ThreadID: threadID, RepositoryID: repositoryID})
		return nil
	}, bindingDependency)
	ui.UseEffectOf(func() func() {
		if latest.Sequence == 0 || latest.ThreadID != threadID {
			return nil
		}
		confirmation, err := composer.NewTimelineCommitConfirmation(latest)
		if err != nil {
			return nil
		}
		attempt, ok := modelState.Get().Attempt(threadID)
		if !ok {
			return nil
		}
		apply(composer.SendCommitConfirmed{
			ThreadID: threadID, Key: attempt.Key(), Confirmation: confirmation,
		})
		return nil
	}, settlementDependency)
	complete := func(command composerSendCommand) {
		ui.SafeGo("send mounted composer message", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			messageID, sendErr := sendComposerCommand(ctx, command)
			cancel()
			next, mode, err := settleComposerCommand(modelState.Get(), command, messageID, sendErr)
			if err == nil {
				if committed := latestEvent.Get(); sendErr == nil && committed.ThreadID == command.ThreadID {
					if confirmation, confirmationErr := composer.NewTimelineCommitConfirmation(committed); confirmationErr == nil {
						next, _ = composer.Reduce(next, composer.SendCommitConfirmed{
							ThreadID: command.ThreadID, Key: command.Key, Confirmation: confirmation,
						})
					}
				}
				ui.PostAsync(func() {
					modelState.Set(next)
					transportMode.Set(mode)
				})
			}
		})
	}
	props := composer.Props{
		View:           composer.View(modelState.Get(), threadID, ""),
		BudgetCurrency: usd,
		TransportMode:  transportMode.Get(),
		ModelOptions:   modelOptions,
		TaskClass:      taskClass.Get(),
		OnTaskClassChange: func(value string) {
			taskClass.Set(value)
		},
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
				ThreadID: threadID, ExpectedRevision: thread.Revision(),
				Key: key, Draft: draft, TaskClass: taskClass.Get(),
			})
		},
		OnRetryRequested: func(key composer.IdempotencyKey) {
			if apply(composer.SendRetryRequested{ThreadID: threadID, Key: key}) {
				attempt, ok := modelState.Get().Attempt(threadID)
				if ok {
					complete(composerSendCommand{
						ThreadID: threadID, ExpectedRevision: thread.Revision(),
						Key: key, Draft: attempt.Request(), TaskClass: taskClass.Get(),
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
	}
	if initErr != nil || threadID.IsZero() || repositoryID.IsZero() {
		props.Disabled = true
		props.DisabledReason = "Select an authoritative thread to compose a message"
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
