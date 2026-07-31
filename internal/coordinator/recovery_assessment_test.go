package coordinator

import (
	"context"
	"errors"
	"testing"

	"codeflux.dev/codeflux/internal/checkpoint"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
)

func TestRecoveryAssessmentServicePersistsSafeAssessmentWithoutResuming(
	t *testing.T,
) {
	t.Parallel()

	input := recoveryClassificationFixture(t)
	candidate := recoveryCheckpointCandidate(t, input)
	store := &recoveryAssessmentStoreStub{candidates: []storage.RecoveryCheckpointCandidate{
		candidate,
	}}
	observer := &recoveryObservationStub{observation: input.Observation}
	patches := &recoveryPatchStub{location: RecoveryPatchLocation{
		Available: true, Locator: input.Observation.PatchLocator,
	}}
	service, err := NewRecoveryAssessmentService(store, observer, patches)
	if err != nil {
		t.Fatal(err)
	}
	records, err := service.AssessIncompleteTaskRuns(t.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 ||
		records[0].Classification != storage.RecoveryClassificationSafeResume ||
		len(store.recorded) != 1 ||
		observer.calls != 1 ||
		patches.calls != 1 {
		t.Fatalf(
			"records=%#v recorded=%#v observer=%d patches=%d",
			records,
			store.recorded,
			observer.calls,
			patches.calls,
		)
	}
}

func TestRecoveryAssessmentServicePersistsCorruptCheckpointAsUnrecoverable(
	t *testing.T,
) {
	t.Parallel()

	input := recoveryClassificationFixture(t)
	candidate := recoveryCheckpointCandidate(t, input)
	candidate.StateJSON += " "
	store := &recoveryAssessmentStoreStub{candidates: []storage.RecoveryCheckpointCandidate{
		candidate,
	}}
	observer := &recoveryObservationStub{observation: input.Observation}
	patches := &recoveryPatchStub{location: RecoveryPatchLocation{
		Available: true, Locator: input.Observation.PatchLocator,
	}}
	service, err := NewRecoveryAssessmentService(store, observer, patches)
	if err != nil {
		t.Fatal(err)
	}
	records, err := service.AssessIncompleteTaskRuns(t.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 ||
		records[0].Classification != storage.RecoveryClassificationImpossible ||
		observer.calls != 0 {
		t.Fatalf("corrupt checkpoint records=%#v observer=%d", records, observer.calls)
	}
}

func TestRecoveryAssessmentServicePersistsMissingCheckpointWithoutPatchLookup(
	t *testing.T,
) {
	t.Parallel()

	input := recoveryClassificationFixture(t)
	store := &recoveryAssessmentStoreStub{
		candidates: []storage.RecoveryCheckpointCandidate{{
			TaskID: input.Checkpoint.TaskID,
			RunID:  input.Checkpoint.RunID,
		}},
	}
	observer := &recoveryObservationStub{}
	patches := &recoveryPatchStub{}
	service, err := NewRecoveryAssessmentService(store, observer, patches)
	if err != nil {
		t.Fatal(err)
	}
	records, err := service.AssessIncompleteTaskRuns(t.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 ||
		records[0].CheckpointID != nil ||
		records[0].Classification != storage.RecoveryClassificationImpossible ||
		records[0].PatchAvailable ||
		observer.calls != 0 ||
		patches.calls != 0 {
		t.Fatalf(
			"missing checkpoint records=%#v observer=%d patches=%d",
			records,
			observer.calls,
			patches.calls,
		)
	}
}

func TestRecoveryAssessmentServiceDoesNotRecordAfterObservationFailure(
	t *testing.T,
) {
	t.Parallel()

	input := recoveryClassificationFixture(t)
	store := &recoveryAssessmentStoreStub{candidates: []storage.RecoveryCheckpointCandidate{
		recoveryCheckpointCandidate(t, input),
	}}
	observer := &recoveryObservationStub{err: errors.New("observation failed")}
	patches := &recoveryPatchStub{location: RecoveryPatchLocation{
		Available: true, Locator: input.Observation.PatchLocator,
	}}
	service, err := NewRecoveryAssessmentService(store, observer, patches)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.AssessIncompleteTaskRuns(t.Context(), 10); err == nil {
		t.Fatal("observation failure was ignored")
	}
	if len(store.recorded) != 0 {
		t.Fatalf("failed observation recorded assessments: %#v", store.recorded)
	}
}

func TestRecoveryAssessmentServiceRejectsInconsistentPatchAvailability(
	t *testing.T,
) {
	t.Parallel()

	input := recoveryClassificationFixture(t)
	store := &recoveryAssessmentStoreStub{candidates: []storage.RecoveryCheckpointCandidate{
		recoveryCheckpointCandidate(t, input),
	}}
	service, err := NewRecoveryAssessmentService(
		store,
		&recoveryObservationStub{observation: input.Observation},
		&recoveryPatchStub{location: RecoveryPatchLocation{Available: true}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.AssessIncompleteTaskRuns(t.Context(), 10); err == nil {
		t.Fatal("inconsistent patch locator was accepted")
	}
	if len(store.recorded) != 0 {
		t.Fatalf("invalid patch location recorded assessments: %#v", store.recorded)
	}
}

func TestRecoveryAssessmentServiceHonorsCancellationBeforeDiscovery(
	t *testing.T,
) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	store := &recoveryAssessmentStoreStub{}
	service, err := NewRecoveryAssessmentService(
		store,
		&recoveryObservationStub{},
		&recoveryPatchStub{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.AssessIncompleteTaskRuns(ctx, 10); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("cancelled assessment error = %v", err)
	}
	if store.listCalls != 0 {
		t.Fatalf("cancelled assessment listed checkpoints %d times", store.listCalls)
	}
}

type recoveryAssessmentStoreStub struct {
	candidates []storage.RecoveryCheckpointCandidate
	recorded   []storage.RecordRecoveryAssessment
	listCalls  int
}

func (store *recoveryAssessmentStoreStub) ListRecoveryCheckpointCandidates(
	context.Context,
	int,
) ([]storage.RecoveryCheckpointCandidate, error) {
	store.listCalls++
	return append([]storage.RecoveryCheckpointCandidate(nil), store.candidates...), nil
}

func (store *recoveryAssessmentStoreStub) RecordRecoveryAssessment(
	_ context.Context,
	input storage.RecordRecoveryAssessment,
) (storage.RecoveryAssessmentRecord, error) {
	store.recorded = append(store.recorded, input)
	return storage.RecoveryAssessmentRecord{
		ID:                input.ID,
		TaskID:            input.TaskID,
		RunID:             input.RunID,
		CheckpointID:      input.CheckpointID,
		Classification:    input.Classification,
		FindingsJSON:      input.FindingsJSON,
		DivergencesJSON:   input.DivergencesJSON,
		ObservationSHA256: input.ObservationSHA256,
		PatchAvailable:    input.PatchAvailable,
		PatchLocator:      input.PatchLocator,
		PatchPath:         input.PatchPath,
		IdempotencyKey:    input.IdempotencyKey,
	}, nil
}

type recoveryObservationStub struct {
	observation RecoveryObservation
	err         error
	calls       int
}

func (stub *recoveryObservationStub) ObserveCheckpointRecovery(
	context.Context,
	storage.RecoveryCheckpointCandidate,
	checkpoint.Snapshot,
) (RecoveryObservation, error) {
	stub.calls++
	return stub.observation, stub.err
}

type recoveryPatchStub struct {
	location RecoveryPatchLocation
	err      error
	calls    int
}

func (stub *recoveryPatchStub) LocateCheckpointPatch(
	context.Context,
	domain.TaskID,
	domain.CheckpointID,
) (RecoveryPatchLocation, error) {
	stub.calls++
	return stub.location, stub.err
}

func recoveryCheckpointCandidate(
	t *testing.T,
	input RecoveryClassificationInput,
) storage.RecoveryCheckpointCandidate {
	t.Helper()
	canonical, err := checkpoint.Canonicalize(input.Checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	return storage.RecoveryCheckpointCandidate{
		CheckpointID:            &input.CheckpointID,
		TaskID:                  input.Checkpoint.TaskID,
		RunID:                   input.Checkpoint.RunID,
		SchemaVersion:           input.Checkpoint.SchemaVersion,
		StateJSON:               canonical.JSON,
		StateSHA256:             canonical.StateSHA256,
		CheckpointEventSequence: input.Checkpoint.LastDurableEventSequence + 1,
	}
}
