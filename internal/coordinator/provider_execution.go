package coordinator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/providers"
	"codeflux.dev/codeflux/internal/redact"
	"codeflux.dev/codeflux/internal/storage"
)

const maximumProviderEvidencePerAttempt = 4096

type providerExecutionStore interface {
	providerAttemptStore
	GetProviderLogicalRequest(
		context.Context,
		domain.ModelRequestID,
	) (storage.ProviderLogicalRequest, error)
	TransitionProviderLogicalRequest(
		context.Context,
		storage.TransitionProviderLogicalRequest,
	) (storage.ProviderLogicalRequest, error)
	AppendProviderAttemptAccounting(
		context.Context,
		storage.AppendProviderAttemptAccounting,
	) (storage.ProviderAttemptAccounting, error)
	AppendProviderAttemptEvidence(
		context.Context,
		storage.AppendProviderAttemptEvidence,
	) (storage.ProviderAttemptEvidence, error)
	SummarizeProviderRequestAccounting(
		context.Context,
		domain.ModelRequestID,
	) (storage.ProviderRequestAccountingSummary, error)
}

// ProviderToolCallHandler is invoked only while the request remains live. The
// next milestone supplies the policy-checked tool implementation.
type ProviderToolCallHandler func(context.Context, providers.ToolCall) error

// ProviderStreamEventHandler receives ordered normalized events for live UI
// publication. Cancellation is checked again before any tool handler runs.
type ProviderStreamEventHandler func(context.Context, providers.StreamEvent) error

// ExecuteProviderRequest binds one normalized request to its immutable price
// evidence and optional policy-checked tool-call consumer.
type ExecuteProviderRequest struct {
	Request        providers.ModelRequest
	EstimatedUsage providers.Usage
	PriceSnapshot  *providers.PriceSnapshot
	OnStreamEvent  ProviderStreamEventHandler
	OnToolCall     ProviderToolCallHandler
}

// ProviderExecutionResult is the attributable terminal result for one logical
// request, including all physical retry accounting.
type ProviderExecutionResult struct {
	Final      *providers.FinalResponse
	Accounting storage.ProviderRequestAccountingSummary
}

// ProviderExecutionService owns the complete provider request lifecycle. It
// never changes provider or model identity; recovery choices remain explicit.
type ProviderExecutionService struct {
	provider providers.ModelProvider
	store    providerExecutionStore
	retries  *providers.RetryExecutor
	redactor providerAttemptRedactor
}

func NewProviderExecutionService(
	provider providers.ModelProvider,
	store providerExecutionStore,
	policy providers.RetryPolicy,
	redactors ...providerAttemptRedactor,
) (*ProviderExecutionService, error) {
	if provider == nil || store == nil {
		return nil, errors.New("provider execution dependencies are required")
	}
	if len(redactors) > 1 {
		return nil, errors.New("only one provider execution redactor is supported")
	}
	var redactorImpl providerAttemptRedactor
	if len(redactors) == 1 {
		redactorImpl = redactors[0]
		if redactorImpl == nil {
			return nil, errors.New("provider execution redactor is required")
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
	recorder, err := NewProviderAttemptRecorder(store, redactorImpl)
	if err != nil {
		return nil, err
	}
	retries, err := providers.NewRetryExecutor(policy, recorder)
	if err != nil {
		return nil, err
	}
	return &ProviderExecutionService{
		provider: provider,
		store:    store,
		retries:  retries,
		redactor: redactorImpl,
	}, nil
}

func (service *ProviderExecutionService) Execute(
	ctx context.Context,
	input ExecuteProviderRequest,
) (ProviderExecutionResult, error) {
	var result ProviderExecutionResult
	if service == nil || service.provider == nil ||
		service.store == nil || service.retries == nil {
		return result, errors.New("provider execution service is unavailable")
	}
	if err := providers.ValidateRequestIdentity(input.Request.Identity); err != nil {
		return result, err
	}
	if err := validateExecutionInput(input); err != nil {
		return result, err
	}
	logical, err := service.store.GetProviderLogicalRequest(
		ctx,
		input.Request.Identity.ModelRequestID,
	)
	if err != nil {
		return result, err
	}
	if err := validateLogicalRequestBinding(logical, input); err != nil {
		return result, err
	}
	if logical.State != storage.ProviderLogicalRequestInFlight {
		return result, errors.New("provider logical request is not in flight")
	}

	requestContext := ctx
	cancel := func() {}
	if !input.Request.Deadline.IsZero() {
		requestContext, cancel = context.WithDeadline(ctx, input.Request.Deadline)
	}
	defer cancel()

	var final *providers.FinalResponse
	_, executeErr := service.retries.Execute(
		requestContext,
		input.Request.Identity,
		func(
			attemptContext context.Context,
			attempt providers.PhysicalAttempt,
		) providers.AttemptOutcome {
			attemptFinal, outcome := service.executeAttempt(
				attemptContext,
				attempt,
				input,
			)
			if attemptFinal != nil {
				copy := *attemptFinal
				final = &copy
			}
			return outcome
		},
	)

	summary, summaryErr := service.store.SummarizeProviderRequestAccounting(
		context.WithoutCancel(ctx),
		input.Request.Identity.ModelRequestID,
	)
	result.Final = final
	result.Accounting = summary
	accountingStatus := providerAccountingStatus(summary, input.EstimatedUsage)
	target := providerLogicalTerminalState(executeErr)
	if transitionErr := service.transitionLogicalRequest(
		context.WithoutCancel(ctx),
		logical,
		target,
		accountingStatus,
	); transitionErr != nil {
		return result, errors.Join(executeErr, summaryErr, transitionErr)
	}
	if executeErr != nil || summaryErr != nil {
		return result, errors.Join(executeErr, summaryErr)
	}
	if final == nil {
		return result, errors.New("provider request completed without a final response")
	}
	return result, nil
}

func (service *ProviderExecutionService) executeAttempt(
	ctx context.Context,
	attempt providers.PhysicalAttempt,
	input ExecuteProviderRequest,
) (*providers.FinalResponse, providers.AttemptOutcome) {
	stream, err := service.provider.Stream(ctx, input.Request)
	if err != nil {
		accountingErr := service.appendAttemptAccounting(
			context.WithoutCancel(ctx), attempt, input, providers.Usage{}, false,
		)
		return nil, providers.AttemptOutcome{
			Err: errors.Join(nonRetryableAccountingError(accountingErr), err),
		}
	}
	stopCancellationWatch := make(chan struct{})
	cancellationWatchDone := make(chan struct{})
	go func() {
		defer close(cancellationWatchDone)
		select {
		case <-ctx.Done():
			service.cancelProviderRequest(input.Request.Identity.ModelRequestID)
		case <-stopCancellationWatch:
		}
	}()
	defer func() {
		close(stopCancellationWatch)
		<-cancellationWatchDone
		_ = stream.Close()
	}()

	var (
		final          *providers.FinalResponse
		usage          providers.Usage
		partial        providers.PartialEffectEvidence
		evidenceNumber uint64
		lastSequence   int64
		firstResponse  time.Time
		metadata       *providers.RedactedProviderMetadata
	)
	outcome := func(outcomeErr error) providers.AttemptOutcome {
		providerRequestID := ""
		if metadata != nil {
			providerRequestID = metadata.ResponseID
		}
		return providers.AttemptOutcome{
			Err:               outcomeErr,
			PartialEvidence:   &partial,
			ProviderRequestID: providerRequestID,
			SafeMetadata:      metadata,
			FirstResponseAt:   firstResponse,
		}
	}
	for {
		event, receiveErr := stream.Recv(ctx)
		if receiveErr != nil {
			if errors.Is(receiveErr, io.EOF) {
				receiveErr = &providers.Failure{
					Kind: providers.FailureUnavailable, Operation: "receive provider stream",
					Retryable:     !hasProviderPartialEffect(partial),
					PartialEffect: partial, Cause: providers.ErrUnavailable,
				}
			}
			accountingErr := service.appendAttemptAccounting(
				context.WithoutCancel(ctx), attempt, input, usage,
				hasProviderPartialEffect(partial),
			)
			return nil, outcome(errors.Join(
				nonRetryableAccountingError(accountingErr),
				receiveErr,
			))
		}
		if event.Sequence <= lastSequence {
			accountingErr := service.appendAttemptAccounting(
				context.WithoutCancel(ctx), attempt, input, usage,
				hasProviderPartialEffect(partial),
			)
			return nil, outcome(errors.Join(
				errors.New("provider stream sequence is not strictly increasing"),
				accountingErr,
			))
		}
		if firstResponse.IsZero() && !event.ObservedAt.IsZero() {
			firstResponse = event.ObservedAt.UTC()
		}
		lastSequence = event.Sequence
		partial.LastSequence = lastSequence
		if err = service.sanitizeProviderEvent(&event); err != nil {
			accountingErr := service.appendAttemptAccounting(
				context.WithoutCancel(ctx), attempt, input, usage,
				hasProviderPartialEffect(partial),
			)
			return nil, outcome(errors.Join(err, accountingErr))
		}
		if err = validateProviderStreamEvent(
			input.Request.Identity,
			event,
			final != nil,
		); err != nil {
			accountingUsage := usage
			if event.Final != nil &&
				validateProviderUsage(event.Final.Usage) == nil {
				accountingUsage = event.Final.Usage
			}
			accountingErr := service.appendAttemptAccounting(
				context.WithoutCancel(ctx), attempt, input, accountingUsage,
				hasProviderPartialEffect(partial),
			)
			return nil, outcome(errors.Join(err, accountingErr))
		}
		if input.OnStreamEvent != nil {
			if contextErr := ctx.Err(); contextErr != nil {
				err = contextErr
			} else {
				err = nonRetryableLocalExecutionError(
					input.OnStreamEvent(ctx, event),
				)
			}
			if err != nil {
				_ = stream.Close()
				accountingErr := service.appendAttemptAccounting(
					context.WithoutCancel(ctx), attempt, input, usage,
					hasProviderPartialEffect(partial),
				)
				return nil, outcome(errors.Join(err, accountingErr))
			}
		}
		switch event.Kind {
		case providers.StreamEventTextDelta:
			partial.StreamedOutput = true
			evidenceNumber++
			err = service.appendProviderEvidence(
				ctx, attempt, evidenceNumber, "partial-output",
				[]byte(event.Text),
			)
		case providers.StreamEventToolCallDelta:
			partial.ToolCall = true
			evidenceNumber++
			var content []byte
			if event.ToolCallDelta != nil {
				content = []byte(event.ToolCallDelta.ArgumentsFragment)
			}
			err = service.appendProviderEvidence(
				ctx, attempt, evidenceNumber, "partial-tool-call", content,
			)
		case providers.StreamEventToolCall:
			partial.ToolCall = true
			if input.OnToolCall != nil {
				if contextErr := ctx.Err(); contextErr != nil {
					err = contextErr
					break
				}
				err = nonRetryableLocalExecutionError(
					input.OnToolCall(ctx, *event.ToolCall),
				)
			}
		case providers.StreamEventUsage:
			usage = *event.Usage
		case providers.StreamEventMetadata:
			partial.ProviderAck = true
			copy := *event.Metadata
			copy.Fields = cloneProviderMetadataFields(event.Metadata.Fields)
			metadata = &copy
		case providers.StreamEventFinal:
			copy := *event.Final
			final = &copy
			usage = copy.Usage
			partial = mergeProviderPartialEffect(partial, copy.PartialEffect)
			metadataCopy := copy.Metadata
			metadataCopy.Fields = cloneProviderMetadataFields(copy.Metadata.Fields)
			metadata = &metadataCopy
		}
		if err != nil {
			_ = stream.Close()
			accountingErr := service.appendAttemptAccounting(
				context.WithoutCancel(ctx), attempt, input, usage,
				hasProviderPartialEffect(partial),
			)
			return nil, outcome(errors.Join(err, accountingErr))
		}
		if final != nil {
			break
		}
	}
	if err := service.appendAttemptAccounting(
		context.WithoutCancel(ctx), attempt, input, usage, false,
	); err != nil {
		return nil, outcome(err)
	}
	return final, outcome(nil)
}

func (service *ProviderExecutionService) cancelProviderRequest(
	requestID domain.ModelRequestID,
) {
	cancelContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = service.provider.Cancel(cancelContext, requestID)
}

func (service *ProviderExecutionService) sanitizeProviderEvent(
	event *providers.StreamEvent,
) error {
	if event == nil || service.redactor == nil {
		return errors.New("provider event redactor is unavailable")
	}
	redactValue := func(value string) (string, error) {
		result, err := service.redactor.Redact(
			redact.BoundaryUIDelivery,
			value,
		)
		if err != nil {
			return "", err
		}
		return result.Text, nil
	}
	sanitize := func(
		metadata providers.RedactedProviderMetadata,
	) (providers.RedactedProviderMetadata, error) {
		var redactionErr error
		sanitized, err := providers.SanitizeProviderMetadata(
			metadata,
			func(input string) string {
				var output string
				output, redactionErr = redactValue(input)
				return output
			},
		)
		if redactionErr != nil {
			return providers.RedactedProviderMetadata{}, redactionErr
		}
		return sanitized, err
	}
	if event.Metadata != nil {
		metadata, err := sanitize(*event.Metadata)
		if err != nil {
			return err
		}
		event.Metadata = &metadata
	}
	if event.Final != nil {
		metadata, err := sanitize(event.Final.Metadata)
		if err != nil {
			return err
		}
		copy := *event.Final
		copy.Metadata = metadata
		event.Final = &copy
	}
	return nil
}

func (service *ProviderExecutionService) appendProviderEvidence(
	ctx context.Context,
	attempt providers.PhysicalAttempt,
	sequence uint64,
	kind string,
	content []byte,
) error {
	if sequence > maximumProviderEvidencePerAttempt {
		return errors.New("provider stream exceeded the bounded evidence limit")
	}
	sum := sha256.Sum256(content)
	_, err := service.store.AppendProviderAttemptEvidence(
		ctx,
		storage.AppendProviderAttemptEvidence{
			ID: providerAttemptID(attempt) + "-evidence-" +
				strconv.FormatUint(sequence, 10),
			AttemptID: providerAttemptID(attempt), Sequence: sequence,
			Kind: kind, Final: false, ContentSHA256: hex.EncodeToString(sum[:]),
			SummaryRedacted: "provider " + kind + " observed",
			ByteCount:       int64(len(content)),
		},
	)
	return err
}

func (service *ProviderExecutionService) appendAttemptAccounting(
	ctx context.Context,
	attempt providers.PhysicalAttempt,
	input ExecuteProviderRequest,
	reported providers.Usage,
	partial bool,
) error {
	reconciled, err := providers.ReconcileUsage(input.EstimatedUsage, reported)
	if err != nil {
		return err
	}
	effective := reconciled.Effective
	providerSpecific, err := providers.NormalizeProviderSpecificUsage(
		effective.ProviderSpecific,
	)
	if err != nil {
		return err
	}
	durableProviderSpecific := make(map[string]domain.TokenCount, len(providerSpecific))
	for category, count := range providerSpecific {
		durableProviderSpecific[category] = domain.TokenCount(count)
	}
	source := "provider-final"
	if partial {
		source = "provider-partial"
	} else if input.EstimatedUsage.Known {
		source = "reconciled"
	}
	var discrepancy *string
	if len(reconciled.Discrepancies) != 0 {
		value := "estimated and provider-reported usage differ or remain unknown"
		discrepancy = &value
	}
	var (
		pricingRevisionID *string
		cost              *storage.ExactMinorCost
	)
	if input.PriceSnapshot != nil {
		id := input.PriceSnapshot.ID
		pricingRevisionID = &id
		calculated := input.PriceSnapshot.Cost(effective)
		if calculated.Known {
			currency, parseErr := domain.ParseCurrencyCode(calculated.Currency)
			if parseErr != nil {
				return parseErr
			}
			cost = &storage.ExactMinorCost{
				Numerator: calculated.Numerator, Denominator: calculated.Denominator,
				Currency: currency,
			}
		}
	}
	provenance, err := json.Marshal(map[string]any{
		"schema_version": 1,
		"source":         source,
	})
	if err != nil {
		return err
	}
	_, err = service.store.AppendProviderAttemptAccounting(
		ctx,
		storage.AppendProviderAttemptAccounting{
			ID:        providerAttemptID(attempt) + "-accounting-1",
			AttemptID: providerAttemptID(attempt), Sequence: 1, Source: source,
			Usage: domain.TokenUsage{
				Known:            effective.Known,
				Input:            domain.TokenCount(effective.InputTokens),
				CachedInput:      domain.TokenCount(effective.CachedInputTokens),
				CacheWrite:       domain.TokenCount(effective.CacheWriteTokens),
				Output:           domain.TokenCount(effective.OutputTokens),
				Reasoning:        domain.TokenCount(effective.ReasoningTokens),
				ProviderSpecific: durableProviderSpecific,
			},
			PricingRevisionID:   pricingRevisionID,
			Cost:                cost,
			DiscrepancyRedacted: discrepancy,
			Partial:             partial,
			ProvenanceJSON:      string(provenance),
		},
	)
	return err
}

func (service *ProviderExecutionService) transitionLogicalRequest(
	ctx context.Context,
	logical storage.ProviderLogicalRequest,
	target storage.ProviderLogicalRequestState,
	accounting storage.ProviderAccountingStatus,
) error {
	_, err := service.store.TransitionProviderLogicalRequest(
		ctx,
		storage.TransitionProviderLogicalRequest{
			ID: logical.ID, ExpectedRevision: logical.Revision,
			From: logical.State, To: target, AccountingStatus: accounting,
		},
	)
	return err
}

func validateExecutionInput(input ExecuteProviderRequest) error {
	if input.Request.Identity.Idempotency != input.Request.Idempotency {
		return errors.New("provider request idempotency declarations disagree")
	}
	if err := validateProviderUsage(input.EstimatedUsage); err != nil {
		return fmt.Errorf("estimated provider usage: %w", err)
	}
	if input.PriceSnapshot == nil ||
		input.PriceSnapshot.ID == "" ||
		input.PriceSnapshot.Model != input.Request.Identity.Model {
		return errors.New(
			"provider price snapshot, including explicit unknown pricing, is required",
		)
	}
	return nil
}

func validateLogicalRequestBinding(
	logical storage.ProviderLogicalRequest,
	input ExecuteProviderRequest,
) error {
	identity := input.Request.Identity
	if logical.ID != identity.ModelRequestID ||
		logical.AdapterName != identity.Provider.Adapter ||
		logical.AdapterVersion != identity.Provider.AdapterVersion ||
		logical.ProviderVersion != identity.Provider.ProviderVersion ||
		logical.ModelIdentifier != identity.Model.Model ||
		logical.ModelVersion != identity.Model.Revision ||
		logical.RequestSHA256 != identity.RequestHash {
		return errors.New("durable provider request identity does not match execution")
	}
	if logical.PricingRevisionID == nil ||
		*logical.PricingRevisionID != input.PriceSnapshot.ID {
		return errors.New("durable provider pricing does not match execution")
	}
	return nil
}

func validateProviderFinal(
	request providers.RequestIdentity,
	final providers.FinalResponse,
) error {
	if final.Identity != request.Model {
		return errors.New("provider final identity changed from the durable request")
	}
	if final.StopReason == "" {
		return errors.New("provider final stop reason is missing")
	}
	return validateProviderUsage(final.Usage)
}

func validateProviderStreamEvent(
	request providers.RequestIdentity,
	event providers.StreamEvent,
	finalSeen bool,
) error {
	switch event.Kind {
	case providers.StreamEventTextDelta:
		if event.Text == "" ||
			event.ToolCallDelta != nil || event.ToolCall != nil ||
			event.Usage != nil || event.Metadata != nil || event.Final != nil {
			return errors.New("provider emitted an empty text delta")
		}
	case providers.StreamEventToolCallDelta:
		if event.ToolCallDelta == nil ||
			event.ToolCallDelta.Index < 0 ||
			event.ToolCallDelta.ID == "" ||
			event.ToolCallDelta.Name == "" ||
			event.Text != "" || event.ToolCall != nil ||
			event.Usage != nil || event.Metadata != nil || event.Final != nil {
			return errors.New("provider emitted an invalid tool-call delta")
		}
	case providers.StreamEventToolCall:
		if event.ToolCall == nil ||
			event.ToolCall.ID == "" ||
			event.ToolCall.Name == "" ||
			!json.Valid(event.ToolCall.Arguments) ||
			event.Text != "" || event.ToolCallDelta != nil ||
			event.Usage != nil || event.Metadata != nil || event.Final != nil {
			return errors.New("provider emitted an invalid tool call")
		}
	case providers.StreamEventUsage:
		if event.Usage == nil ||
			event.Text != "" || event.ToolCallDelta != nil ||
			event.ToolCall != nil || event.Metadata != nil || event.Final != nil {
			return errors.New("provider emitted empty usage")
		}
		return validateProviderUsage(*event.Usage)
	case providers.StreamEventMetadata:
		if event.Metadata == nil ||
			event.Text != "" || event.ToolCallDelta != nil ||
			event.ToolCall != nil || event.Usage != nil || event.Final != nil {
			return errors.New("provider emitted empty metadata")
		}
	case providers.StreamEventFinal:
		if event.Final == nil || finalSeen ||
			event.Text != "" || event.ToolCallDelta != nil ||
			event.ToolCall != nil || event.Usage != nil || event.Metadata != nil {
			return errors.New("provider emitted an invalid final event")
		}
		return validateProviderFinal(request, *event.Final)
	default:
		return errors.New("provider emitted an unsupported normalized event")
	}
	return nil
}

func validateProviderUsage(usage providers.Usage) error {
	_, err := providers.ReconcileUsage(providers.Usage{}, usage)
	return err
}

func providerLogicalTerminalState(err error) storage.ProviderLogicalRequestState {
	switch {
	case err == nil:
		return storage.ProviderLogicalRequestSucceeded
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded),
		errors.Is(err, providers.ErrCanceled):
		return storage.ProviderLogicalRequestCancelled
	case errors.Is(err, providers.ErrRetryBudgetExhausted):
		return storage.ProviderLogicalRequestRetryExhausted
	case errors.Is(err, providers.ErrRetryUnsafeAfterPartial):
		return storage.ProviderLogicalRequestOutcomeUnknown
	default:
		return storage.ProviderLogicalRequestFailed
	}
}

func providerAccountingStatus(
	summary storage.ProviderRequestAccountingSummary,
	estimated providers.Usage,
) storage.ProviderAccountingStatus {
	if summary.Discrepancy {
		return storage.ProviderAccountingDiscrepant
	}
	if estimated.Known && summary.Usage.Known {
		if providerUsageEqualsSummary(estimated, summary.Usage) {
			return storage.ProviderAccountingReconciled
		}
		return storage.ProviderAccountingDiscrepant
	}
	if summary.AccountingComplete || summary.Usage.Known {
		return storage.ProviderAccountingProviderReported
	}
	if estimated.Known {
		return storage.ProviderAccountingEstimated
	}
	return storage.ProviderAccountingUnknown
}

func providerUsageEqualsSummary(
	estimated providers.Usage,
	actual domain.TokenUsage,
) bool {
	if !estimated.Known || !actual.Known ||
		estimated.InputTokens != int64(actual.Input) ||
		estimated.CachedInputTokens != int64(actual.CachedInput) ||
		estimated.CacheWriteTokens != int64(actual.CacheWrite) ||
		estimated.OutputTokens != int64(actual.Output) ||
		estimated.ReasoningTokens != int64(actual.Reasoning) {
		return false
	}
	estimatedSpecific, err := providers.NormalizeProviderSpecificUsage(
		estimated.ProviderSpecific,
	)
	if err != nil {
		return false
	}
	if len(estimatedSpecific) != len(actual.ProviderSpecific) {
		return false
	}
	for category, count := range estimatedSpecific {
		if actual.ProviderSpecific[category] != domain.TokenCount(count) {
			return false
		}
	}
	return true
}

func mergeProviderPartialEffect(
	left providers.PartialEffectEvidence,
	right providers.PartialEffectEvidence,
) providers.PartialEffectEvidence {
	left.StreamedOutput = left.StreamedOutput || right.StreamedOutput
	left.ToolCall = left.ToolCall || right.ToolCall
	left.ProviderAck = left.ProviderAck || right.ProviderAck
	if right.LastSequence > left.LastSequence {
		left.LastSequence = right.LastSequence
	}
	return left
}

func hasProviderPartialEffect(value providers.PartialEffectEvidence) bool {
	return value.StreamedOutput || value.ToolCall ||
		value.ProviderAck || value.LastSequence != 0
}

func cloneProviderMetadataFields(fields map[string]string) map[string]string {
	copy := make(map[string]string, len(fields))
	for key, value := range fields {
		copy[key] = value
	}
	return copy
}

func nonRetryableAccountingError(err error) error {
	if err == nil {
		return nil
	}
	return &providers.Failure{
		Kind: providers.FailureInvalidRequest, Operation: "persist provider accounting",
		Retryable: false, Cause: err,
	}
}

func nonRetryableLocalExecutionError(err error) error {
	if err == nil {
		return nil
	}
	return &providers.Failure{
		Kind: providers.FailureInvalidRequest, Operation: "consume provider event",
		Retryable: false, Cause: err,
	}
}
