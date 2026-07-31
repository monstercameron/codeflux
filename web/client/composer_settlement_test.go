package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
	"codeflux.dev/codeflux/web/frontend/composer"
)

func TestOfflineComposerSettlementPreservesDraftAndExposesRecovery(t *testing.T) {
	model, command, _ := pendingComposerCommand(t, "keep this offline draft")

	next, mode, err := settleComposerCommand(
		model,
		command,
		domain.MessageID{},
		errors.New("bridge unavailable"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if mode != composerTransportRecovery {
		t.Fatalf("transport mode = %q", mode)
	}
	if got := next.Draft(command.ThreadID).Text(); got != "keep this offline draft" {
		t.Fatalf("offline settlement changed draft: %q", got)
	}
	attempt, ok := next.Attempt(command.ThreadID)
	if !ok || attempt.Status() != composer.SendFailed || !attempt.Retryable() ||
		!strings.Contains(attempt.SafeMessage(), "draft is preserved") {
		t.Fatalf("offline recovery attempt = %#v, exists=%t", attempt, ok)
	}
}

func TestTransportAcceptanceWaitsForAuthoritativeTimelineConfirmation(t *testing.T) {
	model, command, messageID := pendingComposerCommand(t, "retain until timeline")

	accepted, mode, err := settleComposerCommand(model, command, messageID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if mode != composerTransportAuthoritative {
		t.Fatalf("transport mode = %q", mode)
	}
	if got := accepted.Draft(command.ThreadID).Text(); got != "retain until timeline" {
		t.Fatalf("transport acceptance cleared draft: %q", got)
	}
	attempt, ok := accepted.Attempt(command.ThreadID)
	if !ok || attempt.Status() != composer.SendAwaitingConfirmation ||
		attempt.MessageID() != messageID || accepted.CanSubmit(command.ThreadID) {
		t.Fatalf("accepted attempt = %#v, exists=%t", attempt, ok)
	}

	unconfirmed, err := composer.Reduce(accepted, composer.SendCommitConfirmed{
		ThreadID: command.ThreadID,
		Key:      command.Key,
	})
	if !composer.IsComposerValueError(err) {
		t.Fatalf("zero-sequence confirmation error = %v", err)
	}
	if got := unconfirmed.Draft(command.ThreadID).Text(); got != "retain until timeline" {
		t.Fatalf("zero-sequence confirmation cleared draft: %q", got)
	}

	confirmed, err := composer.Reduce(accepted, composer.SendCommitConfirmed{
		ThreadID:     command.ThreadID,
		Key:          command.Key,
		Confirmation: clientTimelineConfirmation(t, command.ThreadID, messageID, 73),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !confirmed.Draft(command.ThreadID).IsZero() {
		t.Fatal("authoritative timeline confirmation did not clear draft")
	}
}

func clientTimelineConfirmation(
	t *testing.T,
	threadID domain.ThreadID,
	messageID domain.MessageID,
	sequence uint64,
) composer.TimelineCommitConfirmation {
	t.Helper()
	sessionID, _ := domain.ParseSessionID("ses_01890f3c-4a00-7abc-8def-0123456789ab")
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

func pendingComposerCommand(
	t *testing.T,
	text string,
) (composer.Model, composerSendCommand, domain.MessageID) {
	t.Helper()
	threadID, _ := domain.ParseThreadID("thr_01890f3c-4a00-7abc-8def-0123456789ab")
	repositoryID, _ := domain.ParseRepositoryID("repo_01890f3c-4a00-7abc-8def-0123456789ab")
	messageID, _ := domain.ParseMessageID("msg_01890f3c-4a00-7abc-8def-0123456789ab")
	model, err := composer.NewModel(composer.ThreadBinding{
		ThreadID:     threadID,
		RepositoryID: repositoryID,
	})
	if err != nil {
		t.Fatal(err)
	}
	model, err = composer.Reduce(model, composer.DraftTextChanged{ThreadID: threadID, Text: text})
	if err != nil {
		t.Fatal(err)
	}
	key, _ := composer.ParseIdempotencyKey("send-00000000000000000000000000000001")
	command := composerSendCommand{ThreadID: threadID, Key: key, Draft: model.Draft(threadID)}
	model, err = composer.Reduce(model, composer.SendStarted{ThreadID: threadID, Key: key})
	if err != nil {
		t.Fatal(err)
	}
	return model, command, messageID
}
