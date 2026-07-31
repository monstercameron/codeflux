package coordinator

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
)

func TestRecoveryDecisionServicePreservesPatchWithImmutableFactsAndReplay(
	t *testing.T,
) {
	t.Parallel()

	assessment := recoveryDecisionAssessmentFixture(t)
	store := newRecoveryDecisionStoreStub(assessment)
	exporter := &recoveryPatchExporterStub{
		path:      `C:\codeflux\patches\checkpoint.patch`,
		available: true,
	}
	service, err := NewRecoveryDecisionService(store, exporter)
	if err != nil {
		t.Fatal(err)
	}
	input := PreserveRecoveryPatchInput{
		AssessmentID:   assessment.ID,
		ReasonRedacted: "preserve the exact checkpoint patch for review",
		IdempotencyKey: "preserve-recovery-patch",
	}
	first, err := service.PreservePatch(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := service.PreservePatch(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first.Decision, replayed.Decision) ||
		!reflect.DeepEqual(first.Started, replayed.Started) ||
		!reflect.DeepEqual(first.Terminal, replayed.Terminal) ||
		first.PatchPath != replayed.PatchPath {
		t.Fatalf("replayed preservation = %#v, want %#v", replayed, first)
	}
	if first.Decision.Action != storage.RecoveryActionPreservePatch ||
		first.Started.Outcome != storage.RecoveryAttemptStarted ||
		first.Terminal.Outcome != storage.RecoveryAttemptSucceeded ||
		first.PatchPath != exporter.path {
		t.Fatalf("preservation result = %#v", first)
	}
	if len(store.decisions) != 1 || len(store.attempts) != 2 {
		t.Fatalf(
			"durable recovery facts = %d decisions, %d attempts",
			len(store.decisions),
			len(store.attempts),
		)
	}
	if exporter.calls != 2 {
		t.Fatalf("idempotent exporter calls = %d, want 2", exporter.calls)
	}
}

func TestRecoveryDecisionServiceRecordsFailedPatchAttempt(t *testing.T) {
	t.Parallel()

	assessment := recoveryDecisionAssessmentFixture(t)
	store := newRecoveryDecisionStoreStub(assessment)
	exportErr := errors.New("git diff failed")
	service, err := NewRecoveryDecisionService(
		store,
		&recoveryPatchExporterStub{err: exportErr},
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.PreservePatch(
		t.Context(),
		PreserveRecoveryPatchInput{
			AssessmentID:   assessment.ID,
			ReasonRedacted: "preserve the patch before abandoning recovery",
			IdempotencyKey: "failed-preserve-recovery-patch",
		},
	)
	if !errors.Is(err, exportErr) {
		t.Fatalf("preserve error = %v", err)
	}
	if result.Started.Outcome != storage.RecoveryAttemptStarted ||
		result.Terminal.Outcome != storage.RecoveryAttemptFailed ||
		len(store.decisions) != 1 ||
		len(store.attempts) != 2 {
		t.Fatalf("failed preservation facts = %#v", result)
	}
}

func TestRecoveryDecisionServiceRejectsAssessmentWithoutPatchBeforeDecision(
	t *testing.T,
) {
	t.Parallel()

	assessment := recoveryDecisionAssessmentFixture(t)
	assessment.PatchAvailable = false
	assessment.PatchLocator = ""
	store := newRecoveryDecisionStoreStub(assessment)
	exporter := &recoveryPatchExporterStub{}
	service, err := NewRecoveryDecisionService(store, exporter)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.PreservePatch(
		t.Context(),
		PreserveRecoveryPatchInput{
			AssessmentID:   assessment.ID,
			ReasonRedacted: "preserve a missing patch",
			IdempotencyKey: "missing-preserve-recovery-patch",
		},
	)
	if err == nil {
		t.Fatal("assessment without patch was accepted")
	}
	if len(store.decisions) != 0 || len(store.attempts) != 0 ||
		exporter.calls != 0 {
		t.Fatal("invalid patch choice created recovery side effects")
	}
}

func recoveryDecisionAssessmentFixture(
	t *testing.T,
) storage.RecoveryAssessmentRecord {
	t.Helper()
	fixture := recoveryClassificationFixture(t)
	checkpointID := fixture.CheckpointID
	return storage.RecoveryAssessmentRecord{
		ID:             "recovery-assessment-for-patch",
		TaskID:         fixture.Checkpoint.TaskID,
		RunID:          fixture.Checkpoint.RunID,
		CheckpointID:   &checkpointID,
		Classification: storage.RecoveryClassificationPatchOnly,
		FindingsJSON:   "[]", DivergencesJSON: "[]",
		ObservationSHA256: recoverySHA('f'),
		PatchAvailable:    true,
		PatchLocator: "refs/codeflux/checkpoints/" +
			checkpointID.String(),
		IdempotencyKey: "recovery-assessment-for-patch",
		CreatedAt:      time.Unix(1, 0),
	}
}

type recoveryDecisionStoreStub struct {
	assessment storage.RecoveryAssessmentRecord
	decisions  map[string]storage.RecoveryDecisionRecord
	attempts   map[string]storage.RecoveryAttemptRecord
}

func newRecoveryDecisionStoreStub(
	assessment storage.RecoveryAssessmentRecord,
) *recoveryDecisionStoreStub {
	return &recoveryDecisionStoreStub{
		assessment: assessment,
		decisions:  make(map[string]storage.RecoveryDecisionRecord),
		attempts:   make(map[string]storage.RecoveryAttemptRecord),
	}
}

func (stub *recoveryDecisionStoreStub) GetRecoveryAssessment(
	_ context.Context,
	id string,
) (storage.RecoveryAssessmentRecord, error) {
	if id != stub.assessment.ID {
		return storage.RecoveryAssessmentRecord{}, storage.ErrNotFound
	}
	return stub.assessment, nil
}

func (stub *recoveryDecisionStoreStub) RecordRecoveryDecision(
	_ context.Context,
	input storage.RecordRecoveryDecision,
) (storage.RecoveryDecisionRecord, error) {
	if existing, ok := stub.decisions[input.IdempotencyKey]; ok {
		return existing, nil
	}
	record := storage.RecoveryDecisionRecord{
		ID: input.ID, AssessmentID: input.AssessmentID,
		TaskID: input.TaskID, RunID: input.RunID,
		CheckpointID: input.CheckpointID, Actor: input.Actor,
		Action: input.Action, ReasonRedacted: input.ReasonRedacted,
		IdempotencyKey: input.IdempotencyKey, CreatedAt: time.Unix(2, 0),
	}
	stub.decisions[input.IdempotencyKey] = record
	return record, nil
}

func (stub *recoveryDecisionStoreStub) RecordRecoveryAttempt(
	_ context.Context,
	input storage.RecordRecoveryAttempt,
) (storage.RecoveryAttemptRecord, error) {
	if existing, ok := stub.attempts[input.IdempotencyKey]; ok {
		return existing, nil
	}
	record := storage.RecoveryAttemptRecord{
		ID: input.ID, AssessmentID: input.AssessmentID,
		TaskID: input.TaskID, RunID: input.RunID,
		CheckpointID: input.CheckpointID, Action: input.Action,
		Outcome: input.Outcome, ReasonRedacted: input.ReasonRedacted,
		IdempotencyKey: input.IdempotencyKey, CreatedAt: time.Unix(3, 0),
	}
	stub.attempts[input.IdempotencyKey] = record
	return record, nil
}

type recoveryPatchExporterStub struct {
	path       string
	available  bool
	err        error
	calls      int
	checkpoint domain.CheckpointID
}

func (stub *recoveryPatchExporterStub) PreserveCheckpointPatch(
	_ context.Context,
	checkpointID domain.CheckpointID,
) (string, bool, error) {
	stub.calls++
	stub.checkpoint = checkpointID
	return stub.path, stub.available, stub.err
}
