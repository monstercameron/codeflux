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

func TestDecodeEventRetainsM18ValidationAndAcceptanceFacts(t *testing.T) {
	identity := func(kind codefluxv1.StableIdentityKind, value string) *codefluxv1.StableIdentity {
		return &codefluxv1.StableIdentity{Kind: kind, Value: value}
	}
	base := func(sequence uint64, kind codefluxv1.SessionEventKind) *codefluxv1.SessionEvent {
		return &codefluxv1.SessionEvent{
			Sequence: sequence, PayloadVersion: 1, TimestampUnixMicros: int64(sequence), Kind: kind,
			SessionId: identity(codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_SESSION, "ses_01890f3c-4a00-7abc-8def-0123456789ab"),
			ThreadId:  identity(codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_THREAD, "thr_01890f3c-4a00-7abc-8def-0123456789ab"),
			TaskId:    identity(codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_TASK, "tsk_01890f3c-4a00-7abc-8def-0123456789ab"),
		}
	}
	validation := base(1, codefluxv1.SessionEventKind_SESSION_EVENT_KIND_VALIDATION_UPDATED)
	validation.Revision = 1
	validation.Payload = &codefluxv1.SessionEvent_Validation{Validation: &codefluxv1.ValidationEvent{
		ValidationId: identity(codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_VALIDATION, "val_01890f3c-4a00-7abc-8def-0123456789ab"),
		State:        "running", Required: true, DiffRevision: 7,
	}}
	decoded, err := DecodeEvent(validation)
	if err != nil {
		t.Fatal(err)
	}
	if !decoded.Payload.Validation.Required || decoded.Payload.Validation.DiffRevision != 7 {
		t.Fatalf("decoded validation = %#v", decoded.Payload.Validation)
	}
	acceptance := base(2, codefluxv1.SessionEventKind_SESSION_EVENT_KIND_CHANGE_ACCEPTANCE_UPDATED)
	acceptance.Revision = 1
	acceptance.Payload = &codefluxv1.SessionEvent_ChangeAcceptance{ChangeAcceptance: &codefluxv1.ChangeAcceptanceEvent{
		State: "pending", DiffRevision: 1, PlanRevision: 2, ValidationRevision: 3,
		EvidenceRevision: 4, GraphRevision: 5,
	}}
	decoded, err = DecodeEvent(acceptance)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Kind != events.KindChangeAcceptanceUpdated ||
		decoded.Payload.ChangeAcceptance.Bindings.Evidence != 4 ||
		decoded.Payload.ChangeAcceptance.Bindings.Graph != 5 {
		t.Fatalf("decoded acceptance = %#v", decoded.Payload.ChangeAcceptance)
	}
}

func TestDecodeEventMapsTaskProjectionInvalidationToSnapshotRepairSignal(t *testing.T) {
	value := &codefluxv1.SessionEvent{
		Sequence: 9, Revision: 4, PayloadVersion: 1, TimestampUnixMicros: 9,
		SessionId: &codefluxv1.StableIdentity{Kind: codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_SESSION, Value: "ses_01890f3c-4a00-7abc-8def-0123456789ab"},
		ThreadId:  &codefluxv1.StableIdentity{Kind: codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_THREAD, Value: "thr_01890f3c-4a00-7abc-8def-0123456789ab"},
		TaskId:    &codefluxv1.StableIdentity{Kind: codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_TASK, Value: "tsk_01890f3c-4a00-7abc-8def-0123456789ab"},
		Kind:      codefluxv1.SessionEventKind_SESSION_EVENT_KIND_TASK_PROJECTION_INVALIDATED,
		Payload: &codefluxv1.SessionEvent_TaskProjectionInvalidated{
			TaskProjectionInvalidated: &codefluxv1.TaskProjectionInvalidatedEvent{Entity: "budget", EntityRevision: 4},
		},
	}

	decoded, err := DecodeEvent(value)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Kind != events.KindTaskProjectionInvalidated ||
		decoded.Payload.TaskProjectionInvalidated == nil ||
		decoded.Payload.TaskProjectionInvalidated.Entity != "budget" ||
		decoded.Payload.TaskProjectionInvalidated.Revision != 4 {
		t.Fatalf("decoded invalidation = %#v", decoded)
	}
}
