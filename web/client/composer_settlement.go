package main

import (
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/composer"
)

const composerUnconfirmedSendMessage = "The local coordinator did not confirm the send. Your draft is preserved. Reconnect, then retry with the retained request identity."

const (
	composerTransportAuthoritative = "authoritative-bridge-awaiting-timeline"
	composerTransportRecovery      = "local-preview-recovery"
)

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
		next, err := composer.Reduce(model, composer.SendFailureReceived{
			ThreadID:    command.ThreadID,
			Key:         command.Key,
			Retryable:   true,
			SafeMessage: composerUnconfirmedSendMessage,
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
