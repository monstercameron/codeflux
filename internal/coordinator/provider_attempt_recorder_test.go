package coordinator

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/providers"
	"codeflux.dev/codeflux/internal/storage"
)

func TestProviderAttemptRecorderPersistsIntentBeforeTerminalOutcome(t *testing.T) {
	store := &providerAttemptRecorderStore{}
	recorder, err := NewProviderAttemptRecorder(store)
	if err != nil {
		t.Fatal(err)
	}
	physical := providerPhysicalAttemptFixture(t)
	if err := recorder.PrepareProviderAttempt(t.Context(), physical); err != nil {
		t.Fatal(err)
	}
	if len(store.operations) != 2 ||
		store.operations[0] != "create" ||
		store.operations[1] != "transition:prepared:started" {
		t.Fatalf("prepare operations = %v", store.operations)
	}
	failure := &providers.Failure{
		Kind: providers.FailureUnavailable, Retryable: true,
		Cause: providers.ErrUnavailable,
	}
	if err := recorder.CompleteProviderAttempt(
		t.Context(),
		providers.AttemptRecord{
			PhysicalAttempt:    physical,
			FinishedAt:         physical.StartedAt.Add(time.Second),
			Disposition:        providers.AttemptRetryUnsafePartial,
			Err:                failure,
			ProviderRetryAfter: 2 * time.Second,
			PartialEvidence: &providers.PartialEffectEvidence{
				StreamedOutput: true, ProviderAck: true,
			},
			ProviderRequestID: "response-fallback",
			FirstResponseAt:   physical.StartedAt.Add(250 * time.Millisecond),
			SafeMetadata: &providers.RedactedProviderMetadata{
				RequestID:  physical.Identity.ModelRequestID.String(),
				ResponseID: "response-1",
				Fields: map[string]string{
					"openai_request_id": "provider-wire-request-1",
					"service_tier":      "sk-proj-AAAAAAAAAAAAAAAAAAAA",
				},
			},
		},
	); err != nil {
		t.Fatal(err)
	}
	if len(store.transitions) != 2 {
		t.Fatalf("transitions = %#v", store.transitions)
	}
	completed := store.transitions[1]
	if completed.To != storage.ProviderRequestAttemptOutcomeUnknown ||
		!completed.PartialStreamObserved ||
		completed.EffectStatus != storage.ProviderRequestEffectConfirmed ||
		completed.ErrorClass == nil || *completed.ErrorClass != "unavailable" {
		t.Fatalf("completed transition = %#v", completed)
	}
	if completed.RetryAfterMillis == nil || *completed.RetryAfterMillis != 2000 {
		t.Fatalf("retry-after = %#v", completed.RetryAfterMillis)
	}
	if completed.ProviderRequestIDRedacted == nil ||
		*completed.ProviderRequestIDRedacted != "provider-wire-request-1" {
		t.Fatalf(
			"provider request ID = %#v",
			completed.ProviderRequestIDRedacted,
		)
	}
	if completed.SafeMetadataJSON == nil ||
		strings.Contains(*completed.SafeMetadataJSON, "sk-proj-") ||
		!strings.Contains(*completed.SafeMetadataJSON, "[REDACTED]") {
		t.Fatalf("safe metadata = %#v", completed.SafeMetadataJSON)
	}
	if completed.FirstResponseAt.IsZero() ||
		!completed.FirstResponseAt.Equal(
			physical.StartedAt.Add(250*time.Millisecond),
		) {
		t.Fatalf("first response = %v", completed.FirstResponseAt)
	}
}

func TestProviderAttemptRecorderRejectsCompletionWithoutPreparation(t *testing.T) {
	recorder, err := NewProviderAttemptRecorder(&providerAttemptRecorderStore{})
	if err != nil {
		t.Fatal(err)
	}
	err = recorder.CompleteProviderAttempt(
		t.Context(),
		providers.AttemptRecord{
			PhysicalAttempt: providerPhysicalAttemptFixture(t),
			Disposition:     providers.AttemptFailed,
		},
	)
	if err == nil {
		t.Fatal("unprepared provider attempt was completed")
	}
}

func TestProviderAttemptRecorderCompensatesFailedStartBeforeIO(t *testing.T) {
	startErr := errors.New("start transition failed")
	store := &providerAttemptRecorderStore{transitionErr: startErr}
	recorder, err := NewProviderAttemptRecorder(store)
	if err != nil {
		t.Fatal(err)
	}
	err = recorder.PrepareProviderAttempt(
		t.Context(),
		providerPhysicalAttemptFixture(t),
	)
	if !errors.Is(err, startErr) {
		t.Fatalf("prepare error = %v, want %v", err, startErr)
	}
	if len(store.operations) != 3 ||
		store.operations[0] != "create" ||
		store.operations[1] != "transition:prepared:started" ||
		store.operations[2] != "abort-prepared" {
		t.Fatalf("compensation operations = %v", store.operations)
	}
}

func TestProviderAttemptRecorderRetriesTerminalWriteOnce(t *testing.T) {
	store := &providerAttemptRecorderStore{}
	recorder, err := NewProviderAttemptRecorder(store)
	if err != nil {
		t.Fatal(err)
	}
	physical := providerPhysicalAttemptFixture(t)
	if err := recorder.PrepareProviderAttempt(t.Context(), physical); err != nil {
		t.Fatal(err)
	}
	store.terminalTransitionFailures = 1
	if err := recorder.CompleteProviderAttempt(
		t.Context(),
		providers.AttemptRecord{
			PhysicalAttempt: physical,
			FinishedAt:      physical.StartedAt.Add(time.Second),
			Disposition:     providers.AttemptSucceeded,
		},
	); err != nil {
		t.Fatal(err)
	}
	var terminalWrites int
	for _, transition := range store.transitions {
		if transition.To == storage.ProviderRequestAttemptSucceeded {
			terminalWrites++
		}
	}
	if terminalWrites != 2 {
		t.Fatalf("terminal transition attempts = %d, want 2", terminalWrites)
	}
}

type providerAttemptRecorderStore struct {
	operations                 []string
	transitions                []storage.TransitionProviderRequestAttempt
	transitionErr              error
	terminalTransitionFailures int
}

func (store *providerAttemptRecorderStore) EnsureProviderAttemptAccounting(
	_ context.Context,
	_ storage.EnsureProviderAttemptAccounting,
) error {
	return nil
}

func (store *providerAttemptRecorderStore) AbortPreparedProviderRequestAttemptBeforeIO(
	_ context.Context,
	input storage.AbortPreparedProviderRequestAttemptBeforeIO,
) (storage.ProviderRequestAttempt, error) {
	store.operations = append(store.operations, "abort-prepared")
	return storage.ProviderRequestAttempt{
		ID: input.ID, State: storage.ProviderRequestAttemptCancelled,
		Revision:     input.ExpectedRevision + 1,
		EffectStatus: storage.ProviderRequestEffectNone,
	}, nil
}

func (store *providerAttemptRecorderStore) CreateProviderRequestAttempt(
	_ context.Context,
	input storage.CreateProviderRequestAttempt,
) (storage.ProviderRequestAttempt, error) {
	store.operations = append(store.operations, "create")
	if input.ID == "" || input.LogicalRequestID.IsZero() || input.AttemptNumber == 0 {
		return storage.ProviderRequestAttempt{}, errors.New("invalid create")
	}
	return storage.ProviderRequestAttempt{
		ID: input.ID, LogicalRequestID: input.LogicalRequestID,
		AttemptNumber: input.AttemptNumber,
		State:         storage.ProviderRequestAttemptPrepared,
		EffectStatus:  storage.ProviderRequestEffectNone,
	}, nil
}

func (store *providerAttemptRecorderStore) TransitionProviderRequestAttempt(
	_ context.Context,
	input storage.TransitionProviderRequestAttempt,
) (storage.ProviderRequestAttempt, error) {
	store.operations = append(
		store.operations,
		"transition:"+string(input.From)+":"+string(input.To),
	)
	store.transitions = append(store.transitions, input)
	if store.transitionErr != nil &&
		input.From == storage.ProviderRequestAttemptPrepared &&
		input.To == storage.ProviderRequestAttemptStarted {
		return storage.ProviderRequestAttempt{}, store.transitionErr
	}
	if input.To == storage.ProviderRequestAttemptSucceeded &&
		store.terminalTransitionFailures > 0 {
		store.terminalTransitionFailures--
		return storage.ProviderRequestAttempt{},
			errors.New("one-shot terminal transition failure")
	}
	return storage.ProviderRequestAttempt{
		ID: input.ID, State: input.To, Revision: input.ExpectedRevision + 1,
		EffectStatus: input.EffectStatus,
	}, nil
}

func providerPhysicalAttemptFixture(t *testing.T) providers.PhysicalAttempt {
	t.Helper()
	requestID, err := domain.NewModelRequestID()
	if err != nil {
		t.Fatal(err)
	}
	provider := providers.ProviderIdentity{
		Adapter: "fixture", AdapterVersion: "1",
		Provider: "fixture", ProviderVersion: "1",
	}
	return providers.PhysicalAttempt{
		Identity: providers.RequestIdentity{
			ModelRequestID: requestID,
			Provider:       provider,
			Model: providers.ModelIdentity{
				Provider: provider, Model: "fixture", Revision: "1",
			},
			RequestHash: "fixture-request-hash",
		},
		Number:    1,
		StartedAt: time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC),
	}
}
