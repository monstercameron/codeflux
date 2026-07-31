package main

import (
	"context"
	"testing"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/composer"
	"google.golang.org/grpc"
)

type composerGeneratedClientFake struct {
	request *codefluxv1.SendMessageRequest
}

func (fake *composerGeneratedClientFake) SendMessage(
	_ context.Context,
	request *codefluxv1.SendMessageRequest,
	_ ...grpc.CallOption,
) (*codefluxv1.SendMessageResponse, error) {
	fake.request = request
	return &codefluxv1.SendMessageResponse{Message: &codefluxv1.MessageView{
		MessageId: &codefluxv1.StableIdentity{
			Kind:  codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_MESSAGE,
			Value: "msg_01890f3c-4a00-7abc-8def-0123456789ab",
		},
	}}, nil
}

func TestGeneratedComposerTransportSendsRetainedIdentityAndReturnsCommittedMessage(t *testing.T) {
	threadID, _ := domain.ParseThreadID("thr_01890f3c-4a00-7abc-8def-0123456789ab")
	repositoryID, _ := domain.ParseRepositoryID("repo_01890f3c-4a00-7abc-8def-0123456789ab")
	model, err := composer.NewModel(composer.ThreadBinding{ThreadID: threadID, RepositoryID: repositoryID})
	if err != nil {
		t.Fatal(err)
	}
	model, err = composer.Reduce(model, composer.DraftTextChanged{ThreadID: threadID, Text: "authoritative send"})
	if err != nil {
		t.Fatal(err)
	}
	key, _ := composer.ParseIdempotencyKey("send-00000000000000000000000000000001")
	fake := &composerGeneratedClientFake{}
	messageID, err := (generatedComposerTransport{client: fake}).Send(t.Context(), composerSendCommand{
		ThreadID: threadID, Key: key, Draft: model.Draft(threadID),
	})
	if err != nil {
		t.Fatal(err)
	}
	if messageID.String() != "msg_01890f3c-4a00-7abc-8def-0123456789ab" ||
		fake.request.GetControl().GetIdempotencyKey() != string(key) ||
		fake.request.GetControl().ExpectedRevision == nil ||
		fake.request.GetControl().GetExpectedRevision() != 0 ||
		fake.request.GetThreadId().GetValue() != threadID.String() ||
		fake.request.GetBody() != "authoritative send" || !fake.request.GetCreateDraftTask() {
		t.Fatalf("generated send mapping = message %s request %#v", messageID, fake.request)
	}
}

func TestGeneratedComposerTransportSendsOnlyRepositoryAttachmentIdentities(t *testing.T) {
	threadID, _ := domain.ParseThreadID("thr_01890f3c-4a00-7abc-8def-0123456789ab")
	repositoryID, _ := domain.ParseRepositoryID("repo_01890f3c-4a00-7abc-8def-0123456789ab")
	artifactID, _ := domain.ParseArtifactID("art_01890f3c-4a00-7abc-8def-0123456789ab")
	atomID, _ := domain.ParseAtomID("atm_01890f3c-4a00-7abc-8def-0123456789ab")
	fileAttachment, err := composer.NewFileAttachment(repositoryID, artifactID, "internal/server.go")
	if err != nil {
		t.Fatal(err)
	}
	symbolAttachment, err := composer.NewSymbolAttachment(repositoryID, atomID, "server.Run")
	if err != nil {
		t.Fatal(err)
	}
	model, err := composer.NewModel(composer.ThreadBinding{ThreadID: threadID, RepositoryID: repositoryID})
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []composer.Action{
		composer.DraftTextChanged{ThreadID: threadID, Text: "inspect these identities"},
		composer.AttachmentAdded{ThreadID: threadID, Attachment: fileAttachment},
		composer.AttachmentAdded{ThreadID: threadID, Attachment: symbolAttachment},
	} {
		model, err = composer.Reduce(model, action)
		if err != nil {
			t.Fatal(err)
		}
	}
	key, _ := composer.ParseIdempotencyKey("send-00000000000000000000000000000002")
	fake := &composerGeneratedClientFake{}
	if _, err := (generatedComposerTransport{client: fake}).Send(t.Context(), composerSendCommand{
		ThreadID: threadID, Key: key, Draft: model.Draft(threadID),
	}); err != nil {
		t.Fatal(err)
	}
	if len(fake.request.GetAttachmentPaths()) != 0 {
		t.Fatalf("legacy browser attachment paths leaked into request: %v", fake.request.GetAttachmentPaths())
	}
	identities := fake.request.GetAttachmentIds()
	if len(identities) != 2 ||
		identities[0].GetKind() != codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_ARTIFACT ||
		identities[0].GetValue() != artifactID.String() ||
		identities[1].GetKind() != codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_ATOM ||
		identities[1].GetValue() != atomID.String() {
		t.Fatalf("attachment identity mapping = %#v", identities)
	}
}
