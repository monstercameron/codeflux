package transport

import (
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
)

func TestSessionEventWireProjectionPreservesM18Facts(t *testing.T) {
	sessionID, _ := domain.ParseSessionID("ses_01890f3c-4a00-7abc-8def-0123456789ab")
	threadID, _ := domain.ParseThreadID("thr_01890f3c-4a00-7abc-8def-0123456789ab")
	taskID, _ := domain.ParseTaskID("tsk_01890f3c-4a00-7abc-8def-0123456789ab")
	checkpointID, _ := domain.ParseCheckpointID("ckp_01890f3c-4a00-7abc-8def-0123456789ab")
	validationID, _ := domain.ParseValidationID("val_01890f3c-4a00-7abc-8def-0123456789ab")
	eventID, _ := domain.ParseEventID("evt_01890f3c-4a00-7abc-8def-0123456789ab")
	bindings := events.RevisionBindings{Diff: 1, Plan: 2, Validation: 3, Evidence: 4, Graph: 5}
	event := events.SessionEvent{
		Sequence: 1, SessionID: sessionID, ThreadID: threadID, TaskID: &taskID,
		Timestamp: time.UnixMicro(1).UTC(), Kind: events.KindRecoveryRequired,
		Revision: 1, PayloadVersion: 1,
		Payload: events.Payload{RecoveryRequired: &events.RecoveryRequired{
			CheckpointID: &checkpointID, RedactedReason: "settlement uncertain",
			Classification: events.RecoveryAmbiguousOutcome, DivergenceSummary: "worktree differs",
			ExternalOutcomeAmbiguous: true, PreservePatchAvailable: true, Bindings: bindings,
			RelatedEventIDs: []domain.EventID{eventID}, RelatedFiles: []string{"internal/task.go"},
		}},
	}
	wire, err := sessionEventToProto(event)
	if err != nil {
		t.Fatal(err)
	}
	recovery := wire.GetRecoveryRequired()
	if recovery.GetClassification() != string(events.RecoveryAmbiguousOutcome) ||
		!recovery.GetExternalOutcomeAmbiguous() || !recovery.GetPreservePatchAvailable() ||
		recovery.GetValidationRevision() != bindings.Validation || recovery.GetGraphRevision() != bindings.Graph ||
		len(recovery.GetRelatedEventIds()) != 1 || recovery.GetRelatedEventIds()[0].GetValue() != eventID.String() ||
		len(recovery.GetRelatedFiles()) != 1 || recovery.GetRelatedFiles()[0] != "internal/task.go" {
		t.Fatalf("wire recovery = %#v", recovery)
	}
	validationWire, err := sessionEventToProto(events.SessionEvent{
		Sequence: 2, SessionID: sessionID, ThreadID: threadID, TaskID: &taskID,
		Timestamp: time.UnixMicro(2).UTC(), Kind: events.KindValidationUpdated,
		Revision: 1, PayloadVersion: 1,
		Payload: events.Payload{Validation: &events.Validation{
			ValidationID: validationID, State: domain.ValidationStateRunning,
			Required: true, Acknowledged: false, DiffRevision: 7,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !validationWire.GetValidation().GetRequired() || validationWire.GetValidation().GetDiffRevision() != 7 {
		t.Fatalf("wire validation = %#v", validationWire.GetValidation())
	}
	checkpointWire, err := sessionEventToProto(events.SessionEvent{
		Sequence: 3, SessionID: sessionID, ThreadID: threadID, TaskID: &taskID,
		Timestamp: time.UnixMicro(3).UTC(), Kind: events.KindCheckpointCreated,
		Revision: 1, PayloadVersion: 1,
		Payload: events.Payload{Checkpoint: &events.Checkpoint{
			CheckpointID: checkpointID, TaskRevision: 1, PlanStep: "validate",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if checkpointWire.GetCheckpoint().GetPlanStep() != "validate" {
		t.Fatalf("wire checkpoint = %#v", checkpointWire.GetCheckpoint())
	}
	acceptanceWire, err := sessionEventToProto(events.SessionEvent{
		Sequence: 4, SessionID: sessionID, ThreadID: threadID, TaskID: &taskID,
		Timestamp: time.UnixMicro(4).UTC(), Kind: events.KindChangeAcceptanceUpdated,
		Revision: 1, PayloadVersion: 1,
		Payload: events.Payload{ChangeAcceptance: &events.ChangeAcceptance{
			State: domain.ChangeAcceptanceStatePending, Bindings: bindings,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if acceptanceWire.GetChangeAcceptance().GetEvidenceRevision() != bindings.Evidence ||
		acceptanceWire.GetChangeAcceptance().GetState() != string(domain.ChangeAcceptanceStatePending) {
		t.Fatalf("wire acceptance = %#v", acceptanceWire.GetChangeAcceptance())
	}
}

func TestSessionSnapshotWireProjectionPreservesReviewAndPolicyFacts(t *testing.T) {
	sessionID, _ := domain.ParseSessionID("ses_01890f3c-4a00-7abc-8def-0123456789ab")
	threadID, _ := domain.ParseThreadID("thr_01890f3c-4a00-7abc-8def-0123456789ab")
	taskID, _ := domain.ParseTaskID("tsk_01890f3c-4a00-7abc-8def-0123456789ab")
	bindings := events.RevisionBindings{Diff: 1, Plan: 2, Validation: 3, Evidence: 4, Graph: 5}
	wire, err := sessionProjectionSnapshotToProto(SessionProjectionSnapshotView{
		SessionID: sessionID, ThreadID: threadID, TaskID: &taskID,
		ThroughSequence: 12, ObservedAt: time.UnixMicro(12).UTC(),
		TaskState: domain.TaskStateRunning, TaskRevision: 4,
		ReviewBindings: &bindings, ReviewRevision: 2, GraphRevision: 5,
		DeniedTaskActions:      []string{"stop"},
		TaskActionPolicyReason: "Stop is denied while a protected tool settles.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if wire.GetReviewRevision() != 2 || wire.GetReviewBindings().GetEvidenceRevision() != 4 ||
		wire.GetGraphRevision() != 5 || len(wire.GetDeniedTaskActions()) != 1 ||
		wire.GetTaskActionPolicyReason() == "" {
		t.Fatalf("snapshot review/policy wire facts = %#v", wire)
	}
}
