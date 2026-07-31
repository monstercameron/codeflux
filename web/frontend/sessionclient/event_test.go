package sessionclient

import (
	"testing"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/events"
)

func TestDecodeEventRetainsTypedThreadSessionAuthority(t *testing.T) {
	value := &codefluxv1.SessionEvent{
		Sequence: 1, PayloadVersion: 1, TimestampUnixMicros: 1,
		SessionId: &codefluxv1.StableIdentity{Kind: codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_SESSION, Value: "ses_01890f3c-4a00-7abc-8def-0123456789ab"},
		ThreadId:  &codefluxv1.StableIdentity{Kind: codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_THREAD, Value: "thr_01890f3c-4a00-7abc-8def-0123456789ab"},
		Kind:      codefluxv1.SessionEventKind_SESSION_EVENT_KIND_THREAD_CREATED,
		Payload: &codefluxv1.SessionEvent_ThreadCreated{ThreadCreated: &codefluxv1.ThreadCreatedEvent{
			WorkspaceId: &codefluxv1.StableIdentity{Kind: codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_WORKSPACE, Value: "wsp_01890f3c-4a00-7abc-8def-0123456789ab"},
			Title:       "Authoritative",
		}},
	}
	decoded, err := DecodeEvent(value)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Kind != events.KindThreadCreated || decoded.Payload.ThreadCreated.Title != "Authoritative" || decoded.Payload.ThreadCreated.WorkspaceID == nil {
		t.Fatalf("decoded event = %#v", decoded)
	}
}
