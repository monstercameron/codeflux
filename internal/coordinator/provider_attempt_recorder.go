package coordinator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"codeflux.dev/codeflux/internal/providers"
	"codeflux.dev/codeflux/internal/redact"
	"codeflux.dev/codeflux/internal/storage"
)

type providerAttemptStore interface {
	CreateProviderRequestAttempt(
		context.Context,
		storage.CreateProviderRequestAttempt,
	) (storage.ProviderRequestAttempt, error)
	TransitionProviderRequestAttempt(
		context.Context,
		storage.TransitionProviderRequestAttempt,
	) (storage.ProviderRequestAttempt, error)
	AbortPreparedProviderRequestAttemptBeforeIO(
		context.Context,
		storage.AbortPreparedProviderRequestAttemptBeforeIO,
	) (storage.ProviderRequestAttempt, error)
	EnsureProviderAttemptAccounting(
		context.Context,
		storage.EnsureProviderAttemptAccounting,
	) error
}

type providerAttemptRedactor interface {
	Redact(redact.Boundary, string) (redact.Result, error)
}

// ProviderAttemptRecorder maps the shared retry lifecycle to durable SQLite
// intent before external I/O and a terminal physical-attempt outcome after it.
type ProviderAttemptRecorder struct {
	store    providerAttemptStore
	redactor providerAttemptRedactor
	mu       sync.Mutex
	live     map[string]storage.ProviderRequestAttempt
}

func NewProviderAttemptRecorder(
	store providerAttemptStore,
	redactors ...providerAttemptRedactor,
) (*ProviderAttemptRecorder, error) {
	if store == nil {
		return nil, errors.New("provider attempt store is required")
	}
	if len(redactors) > 1 {
		return nil, errors.New("only one provider attempt redactor is supported")
	}
	var redactorImpl providerAttemptRedactor
	if len(redactors) == 1 {
		redactorImpl = redactors[0]
		if redactorImpl == nil {
			return nil, errors.New("provider attempt redactor is required")
		}
	} else {
		pipeline, err := redact.NewPipeline(nil, redact.Limits{
			MaximumInputBytes:  16 << 10,
			MaximumOutputBytes: 8 << 10,
		})
		if err != nil {
			return nil, err
		}
		redactorImpl = pipeline
	}
	return &ProviderAttemptRecorder{
		store: store, redactor: redactorImpl,
		live: make(map[string]storage.ProviderRequestAttempt),
	}, nil
}

func (recorder *ProviderAttemptRecorder) PrepareProviderAttempt(
	ctx context.Context,
	attempt providers.PhysicalAttempt,
) error {
	if recorder == nil || recorder.store == nil {
		return errors.New("provider attempt recorder is unavailable")
	}
	id := providerAttemptID(attempt)
	var idempotencyKey *string
	if attempt.Identity.Idempotency.Key != "" {
		key := attempt.Identity.Idempotency.Key
		idempotencyKey = &key
	}
	prepared, err := recorder.store.CreateProviderRequestAttempt(
		ctx,
		storage.CreateProviderRequestAttempt{
			ID: id, LogicalRequestID: attempt.Identity.ModelRequestID,
			AttemptNumber:         uint64(attempt.Number),
			RequestIdempotencyKey: idempotencyKey,
		},
	)
	if err != nil {
		return err
	}
	started, err := recorder.store.TransitionProviderRequestAttempt(
		ctx,
		storage.TransitionProviderRequestAttempt{
			ID: id, ExpectedRevision: prepared.Revision,
			From: prepared.State, To: storage.ProviderRequestAttemptStarted,
			EffectStatus: storage.ProviderRequestEffectPossible,
			ObservedAt:   attempt.StartedAt,
		},
	)
	if err != nil {
		abortContext, cancel := context.WithTimeout(
			context.WithoutCancel(ctx),
			5*time.Second,
		)
		defer cancel()
		_, abortErr := recorder.store.AbortPreparedProviderRequestAttemptBeforeIO(
			abortContext,
			storage.AbortPreparedProviderRequestAttemptBeforeIO{
				ID: prepared.ID, ExpectedRevision: prepared.Revision,
				ObservedAt: attempt.StartedAt.UTC(),
			},
		)
		return errors.Join(err, abortErr)
	}
	recorder.mu.Lock()
	recorder.live[id] = started
	recorder.mu.Unlock()
	return nil
}

func (recorder *ProviderAttemptRecorder) CompleteProviderAttempt(
	ctx context.Context,
	record providers.AttemptRecord,
) error {
	if recorder == nil || recorder.store == nil {
		return errors.New("provider attempt recorder is unavailable")
	}
	id := providerAttemptID(record.PhysicalAttempt)
	recorder.mu.Lock()
	current, found := recorder.live[id]
	recorder.mu.Unlock()
	if !found {
		return errors.New("provider attempt was not durably prepared")
	}
	target, effect := providerAttemptOutcome(record)
	errorClass := providerAttemptErrorClass(record.Err)
	var retryAfterMillis *int64
	if record.ProviderRetryAfter > 0 {
		value := record.ProviderRetryAfter.Milliseconds()
		retryAfterMillis = &value
	}
	providerRequestID, metadataJSON, evidenceErr :=
		recorder.sanitizeAttemptEvidence(record)
	partial := record.PartialEvidence != nil &&
		(record.PartialEvidence.StreamedOutput || record.PartialEvidence.ToolCall)
	accountingErr := recorder.store.EnsureProviderAttemptAccounting(
		ctx,
		storage.EnsureProviderAttemptAccounting{
			AttemptID:  id,
			ObservedAt: record.FinishedAt.UTC(),
		},
	)
	if accountingErr != nil {
		retryContext, cancel := context.WithTimeout(
			context.WithoutCancel(ctx),
			5*time.Second,
		)
		retryErr := recorder.store.EnsureProviderAttemptAccounting(
			retryContext,
			storage.EnsureProviderAttemptAccounting{
				AttemptID:  id,
				ObservedAt: record.FinishedAt.UTC(),
			},
		)
		cancel()
		accountingErr = errors.Join(accountingErr, retryErr)
		if retryErr == nil {
			accountingErr = nil
		}
	}
	transition := storage.TransitionProviderRequestAttempt{
		ID: id, ExpectedRevision: current.Revision,
		From: current.State, To: target,
		ProviderRequestIDRedacted: providerRequestID,
		EffectStatus:              effect, PartialStreamObserved: partial,
		ErrorClass: errorClass,
		Retryable: record.Disposition == providers.AttemptRetryScheduled ||
			record.Disposition == providers.AttemptRetryExhausted,
		RetryAfterMillis: retryAfterMillis,
		SafeMetadataJSON: metadataJSON,
		FirstResponseAt:  record.FirstResponseAt,
		ObservedAt:       record.FinishedAt,
	}
	_, err := recorder.store.TransitionProviderRequestAttempt(ctx, transition)
	if err != nil {
		retryContext, cancel := context.WithTimeout(
			context.WithoutCancel(ctx),
			5*time.Second,
		)
		_, retryErr := recorder.store.TransitionProviderRequestAttempt(
			retryContext,
			transition,
		)
		cancel()
		err = errors.Join(err, retryErr)
		if retryErr == nil {
			err = nil
		}
	}
	if err != nil {
		return errors.Join(evidenceErr, accountingErr, err)
	}
	recorder.mu.Lock()
	delete(recorder.live, id)
	recorder.mu.Unlock()
	return errors.Join(evidenceErr, accountingErr)
}

func (recorder *ProviderAttemptRecorder) sanitizeAttemptEvidence(
	record providers.AttemptRecord,
) (*string, *string, error) {
	redactValue := func(value string) (string, error) {
		result, err := recorder.redactor.Redact(
			redact.BoundaryLogPersistence,
			value,
		)
		if err != nil {
			return "", err
		}
		return result.Text, nil
	}
	sanitizeValue := func(value string) (string, error) {
		var redactionErr error
		sanitized, err := providers.SanitizeProviderIdentifier(
			value,
			func(input string) string {
				var output string
				output, redactionErr = redactValue(input)
				return output
			},
		)
		if redactionErr != nil {
			return "", redactionErr
		}
		return sanitized, err
	}

	var (
		metadata     *providers.RedactedProviderMetadata
		metadataJSON *string
	)
	if record.SafeMetadata != nil {
		var redactionErr error
		sanitized, err := providers.SanitizeProviderMetadata(
			*record.SafeMetadata,
			func(input string) string {
				var output string
				output, redactionErr = redactValue(input)
				return output
			},
		)
		if redactionErr != nil {
			return nil, nil, redactionErr
		}
		if err != nil {
			return nil, nil, err
		}
		metadata = &sanitized
		encoded, err := json.Marshal(sanitized)
		if err != nil {
			return nil, nil, fmt.Errorf("serialize safe provider metadata: %w", err)
		}
		value := string(encoded)
		metadataJSON = &value
	}

	providerID := ""
	if metadata != nil {
		for _, key := range []string{
			"openai_request_id",
			"anthropic_request_id",
			"provider_request_id",
		} {
			if value := metadata.Fields[key]; value != "" {
				providerID = value
				break
			}
		}
	}
	if providerID == "" {
		providerID = record.ProviderRequestID
	}
	if providerID == "" && metadata != nil {
		providerID = metadata.ResponseID
	}
	var providerRequestID *string
	if providerID != "" {
		value, err := sanitizeValue(providerID)
		if err != nil {
			return nil, nil, err
		}
		providerRequestID = &value
	}
	return providerRequestID, metadataJSON, nil
}

func providerAttemptID(attempt providers.PhysicalAttempt) string {
	return attempt.Identity.ModelRequestID.String() +
		"-attempt-" + strconv.Itoa(attempt.Number)
}

func providerAttemptOutcome(
	record providers.AttemptRecord,
) (storage.ProviderRequestAttemptState, storage.ProviderRequestEffectStatus) {
	effect := storage.ProviderRequestEffectPossible
	if record.PartialEvidence != nil && record.PartialEvidence.ProviderAck {
		effect = storage.ProviderRequestEffectConfirmed
	}
	switch record.Disposition {
	case providers.AttemptSucceeded:
		return storage.ProviderRequestAttemptSucceeded, effect
	case providers.AttemptCanceled:
		return storage.ProviderRequestAttemptCancelled, effect
	case providers.AttemptRetryUnsafePartial:
		return storage.ProviderRequestAttemptOutcomeUnknown, effect
	case providers.AttemptFailed, providers.AttemptRetryScheduled,
		providers.AttemptRetryExhausted:
		return storage.ProviderRequestAttemptFailed, effect
	default:
		return storage.ProviderRequestAttemptOutcomeUnknown, effect
	}
}

func providerAttemptErrorClass(err error) *string {
	if err == nil {
		return nil
	}
	value := "unknown"
	var failure *providers.Failure
	if errors.As(err, &failure) {
		switch failure.Kind {
		case providers.FailureTimeout:
			value = "timeout"
		case providers.FailureTransport:
			value = "retryable"
		case providers.FailureRateLimited:
			value = "rate-limit"
		case providers.FailureAuthentication:
			value = "authentication"
		case providers.FailureInvalidRequest:
			value = "invalid-request"
		case providers.FailureSafety:
			value = "safety"
		case providers.FailureUnavailable:
			value = "unavailable"
		case providers.FailureCanceled:
			value = "cancelled"
		default:
			value = "unknown"
		}
	}
	return &value
}

func (recorder *ProviderAttemptRecorder) String() string {
	if recorder == nil {
		return "provider-attempt-recorder(unavailable)"
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return fmt.Sprintf("provider-attempt-recorder(active=%d)", len(recorder.live))
}

var _ providers.AttemptRecorder = (*ProviderAttemptRecorder)(nil)
