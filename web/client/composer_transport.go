package main

import (
	"context"
	"errors"
	"fmt"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/composer"
	"google.golang.org/grpc"
)

type composerSendCommand struct {
	ThreadID         domain.ThreadID
	ExpectedRevision uint64
	Key              composer.IdempotencyKey
	Draft            composer.Draft
	// TaskClass is what kind of work the person declared. Empty means they
	// declared none, and then the request is recorded without starting
	// anything: intake refuses to guess a class, because the guess would land
	// inside the fingerprint that gates memory retrieval and routing.
	TaskClass string
}

type generatedMessageClient interface {
	SendMessage(context.Context, *codefluxv1.SendMessageRequest, ...grpc.CallOption) (*codefluxv1.SendMessageResponse, error)
}

type generatedComposerTransport struct{ client generatedMessageClient }

func (transport generatedComposerTransport) Send(
	ctx context.Context,
	command composerSendCommand,
) (domain.MessageID, error) {
	if transport.client == nil || command.ThreadID.IsZero() || command.Key == "" ||
		!command.Draft.CanSubmit() {
		return domain.MessageID{}, errors.New("composer send command is invalid")
	}
	attachmentIDs, err := composerAttachmentIdentities(command.Draft.Attachments())
	if err != nil {
		return domain.MessageID{}, err
	}
	response, err := transport.client.SendMessage(ctx, &codefluxv1.SendMessageRequest{
		Control: &codefluxv1.MutationControl{
			IdempotencyKey:   string(command.Key),
			ExpectedRevision: &command.ExpectedRevision,
		},
		ThreadId: &codefluxv1.StableIdentity{
			Kind:  codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_THREAD,
			Value: command.ThreadID.String(),
		},
		// The task is created by CreateTask rather than by this flag. A task
		// made here is a bare draft with no policy, forecast, or budget, so
		// nothing could ever start it — which is exactly what happened: a
		// request became a draft task and stopped there.
		Body: command.Draft.Text(), CreateDraftTask: false,
		AttachmentIds: attachmentIDs,
	})
	if err != nil {
		return domain.MessageID{}, err
	}
	if response == nil || response.GetMessage() == nil || response.GetMessage().GetMessageId() == nil ||
		response.GetMessage().GetMessageId().GetKind() != codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_MESSAGE {
		return domain.MessageID{}, errors.New("send message response lacks a typed message identity")
	}
	messageID, err := domain.ParseMessageID(response.GetMessage().GetMessageId().GetValue())
	if err != nil {
		return domain.MessageID{}, fmt.Errorf("parse committed message identity: %w", err)
	}
	return messageID, nil
}

func composerAttachmentIdentities(
	attachments []composer.RepositoryAttachment,
) ([]*codefluxv1.StableIdentity, error) {
	identities := make([]*codefluxv1.StableIdentity, 0, len(attachments))
	for _, attachment := range attachments {
		if err := attachment.Validate(); err != nil {
			return nil, fmt.Errorf("validate composer attachment: %w", err)
		}
		kind := codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_UNSPECIFIED
		switch attachment.Kind() {
		case composer.AttachmentFile:
			kind = codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_ARTIFACT
		case composer.AttachmentSymbol:
			kind = codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_ATOM
		default:
			return nil, errors.New("composer attachment kind is unsupported")
		}
		identities = append(identities, &codefluxv1.StableIdentity{
			Kind: kind, Value: attachment.Identity(),
		})
	}
	return identities, nil
}
