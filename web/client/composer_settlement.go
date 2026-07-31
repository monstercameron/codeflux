package main

import (
	"fmt"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
	"codeflux.dev/codeflux/web/frontend/composer"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const composerUnconfirmedSendMessage = "The local coordinator did not confirm the send. Your draft is preserved. Reconnect, then retry with the retained request identity."

const composerStaleSendMessage = "The thread changed before this send could commit. Your draft is preserved; refresh the authoritative thread state before sending again."
const composerDeniedSendMessage = "The coordinator denied this send in the current authoritative thread state. Your draft is preserved for review or editing."

const (
	composerTransportAuthoritative = "authoritative-bridge-awaiting-timeline"
	composerTransportRecovery      = "local-preview-recovery"
)

func composerSettlementDependency(
	selectedThreadID domain.ThreadID,
	latest events.SessionEvent,
	attempt composer.SendAttempt,
	attemptPresent bool,
) string {
	return fmt.Sprintf(
		"%s|%s|%d|%s|%t|%s|%s",
		selectedThreadID.String(), latest.ThreadID.String(), latest.Sequence,
		latest.Kind, attemptPresent, attempt.Status(), attempt.MessageID().String(),
	)
}

// settleComposerCommand records only what the command transport can prove.
// A successful response records the server-issued message identity but leaves
// the draft intact until the ordered session timeline confirms that identity.
// A failed bridge never fabricates a local confirmation.
func settleComposerCommand(
	model composer.Model,
	command composerSendCommand,
	messageID domain.MessageID,
	sendErr error,
) (composer.Model, string, error) {
	if sendErr != nil || messageID.IsZero() {
		retryable := true
		message := composerUnconfirmedSendMessage
		switch status.Code(sendErr) {
		case codes.Aborted:
			retryable = false
			message = composerStaleSendMessage
		case codes.PermissionDenied, codes.FailedPrecondition,
			codes.InvalidArgument, codes.NotFound, codes.Unauthenticated:
			retryable = false
			message = composerDeniedSendMessage
		}
		next, err := composer.Reduce(model, composer.SendFailureReceived{
			ThreadID:    command.ThreadID,
			Key:         command.Key,
			Retryable:   retryable,
			SafeMessage: message,
		})
		return next, composerTransportRecovery, err
	}
	next, err := composer.Reduce(model, composer.SendAccepted{
		ThreadID:  command.ThreadID,
		Key:       command.Key,
		MessageID: messageID,
	})
	return next, composerTransportAuthoritative, err
}
