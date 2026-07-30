package coordinator

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/providers"
	"codeflux.dev/codeflux/internal/storage"
)

func TestProviderExecutionPersistsTerminalAttributionAndUnknownPrice(t *testing.T) {
	ctx := context.Background()
	repositories, smoke, identity := providerExecutionFixture(t)
	usage := providers.Usage{
		Known: true, Source: providers.UsageSourceProvider,
		InputTokens: 8, OutputTokens: 1,
		ProviderSpecific: json.RawMessage(`{"audio":2}`),
	}
	provider := &executionProvider{
		identity: identity.Provider,
		stream: &executionStream{events: []providers.StreamEvent{
			{
				Sequence: 1, Kind: providers.StreamEventMetadata,
				Metadata: &providers.RedactedProviderMetadata{
					RequestID: smoke.Request.ID.String(),
				},
			},
			{Sequence: 2, Kind: providers.StreamEventTextDelta, Text: "OK"},
			{Sequence: 3, Kind: providers.StreamEventUsage, Usage: &usage},
			{
				Sequence: 4, Kind: providers.StreamEventFinal,
				Final: &providers.FinalResponse{
					Identity: identity, StopReason: providers.StopReasonCompleted,
					Usage: usage,
					PartialEffect: providers.PartialEffectEvidence{
						ProviderAck: true, StreamedOutput: true, LastSequence: 3,
					},
				},
			},
		}},
	}
	service, err := NewProviderExecutionService(
		provider,
		repositories,
		providers.RetryPolicy{MaximumAttempts: 1},
	)
	if err != nil {
		t.Fatal(err)
	}
	request := providerExecutionRequest(smoke, identity)
	result, err := service.Execute(
		ctx,
		ExecuteProviderRequest{
			Request: request,
			PriceSnapshot: &providers.PriceSnapshot{
				ID: smoke.Pricing.ID, Model: identity,
				EffectiveAt: smoke.Pricing.EffectiveAt,
				CapturedAt:  smoke.Pricing.CreatedAt,
				Source:      "unknown",
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Final == nil ||
		result.Final.StopReason != providers.StopReasonCompleted ||
		!result.Accounting.Usage.Known ||
		result.Accounting.Usage.Input != 8 ||
		result.Accounting.Usage.Output != 1 ||
		result.Accounting.Usage.ProviderSpecific["audio"] != 2 ||
		result.Accounting.Cost != nil ||
		result.Accounting.AccountingComplete {
		t.Fatalf("execution result = %#v", result)
	}
	report, err := repositories.GetProviderRequestAttribution(
		ctx,
		smoke.Request.ID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if report.Request.State != storage.ProviderLogicalRequestSucceeded ||
		report.Request.AccountingStatus != storage.ProviderAccountingProviderReported ||
		report.Request.CompletedAt == nil ||
		report.Pricing == nil || report.Pricing.PricingKnown ||
		len(report.Attempts) != 1 ||
		report.Attempts[0].Attempt.State != storage.ProviderRequestAttemptSucceeded ||
		report.Attempts[0].Accounting == nil ||
		!report.Attempts[0].Accounting.Usage.Known {
		t.Fatalf("provider attribution = %#v", report)
	}
}

func TestProviderExecutionCancellationPreventsAdditionalToolCalls(t *testing.T) {
	repositories, smoke, identity := providerExecutionFixture(t)
	provider := &executionProvider{
		identity: identity.Provider,
		stream: &executionStream{events: []providers.StreamEvent{
			{
				Sequence: 1, Kind: providers.StreamEventTextDelta,
				Text: "cancel now",
			},
			{
				Sequence: 2, Kind: providers.StreamEventToolCall,
				ToolCall: &providers.ToolCall{
					ID: "call-1", Name: "must-not-run", Arguments: []byte(`{}`),
				},
			},
		}},
	}
	service, err := NewProviderExecutionService(
		provider,
		repositories,
		providers.RetryPolicy{MaximumAttempts: 1},
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var calls int
	var delivered int
	_, err = service.Execute(
		ctx,
		ExecuteProviderRequest{
			Request: providerExecutionRequest(smoke, identity),
			PriceSnapshot: &providers.PriceSnapshot{
				ID: smoke.Pricing.ID, Model: identity,
				EffectiveAt: smoke.Pricing.EffectiveAt,
				CapturedAt:  smoke.Pricing.CreatedAt,
				Source:      "unknown",
			},
			OnStreamEvent: func(context.Context, providers.StreamEvent) error {
				delivered++
				cancel()
				return nil
			},
			OnToolCall: func(context.Context, providers.ToolCall) error {
				calls++
				return nil
			},
		},
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
	if delivered != 1 || calls != 0 {
		t.Fatalf(
			"delivery/tool calls after cancellation = %d/%d, want 1/0",
			delivered,
			calls,
		)
	}
	provider.mu.Lock()
	cancelCalls := provider.cancelCalls
	provider.mu.Unlock()
	if cancelCalls != 1 {
		t.Fatalf("provider cancel calls = %d, want 1", cancelCalls)
	}
	report, reportErr := repositories.GetProviderRequestAttribution(
		context.Background(),
		smoke.Request.ID,
	)
	if reportErr != nil {
		t.Fatal(reportErr)
	}
	if report.Request.State != storage.ProviderLogicalRequestCancelled ||
		len(report.Attempts) != 1 ||
		report.Attempts[0].Attempt.State != storage.ProviderRequestAttemptCancelled {
		t.Fatalf("canceled provider attribution = %#v", report)
	}
}

func TestProviderExecutionRequiresUnknownPriceSnapshotBeforeIO(t *testing.T) {
	repositories, smoke, identity := providerExecutionFixture(t)
	provider := &executionProvider{
		identity: identity.Provider,
		stream:   &executionStream{},
	}
	service, err := NewProviderExecutionService(
		provider,
		repositories,
		providers.RetryPolicy{MaximumAttempts: 1},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Execute(
		context.Background(),
		ExecuteProviderRequest{
			Request: providerExecutionRequest(smoke, identity),
		},
	)
	if err == nil || !strings.Contains(err.Error(), "price snapshot") {
		t.Fatalf("missing price snapshot error = %v", err)
	}
	provider.mu.Lock()
	streamCalls := provider.streamCalls
	provider.mu.Unlock()
	if streamCalls != 0 {
		t.Fatalf("provider stream calls without pricing = %d", streamCalls)
	}
	report, err := repositories.GetProviderRequestAttribution(
		context.Background(),
		smoke.Request.ID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if report.Request.State != storage.ProviderLogicalRequestInFlight ||
		report.Accounting.AttemptCount != 0 {
		t.Fatalf("pre-I/O pricing rejection attribution = %#v", report)
	}
}

func TestProviderExecutionCancelInterruptsProviderThatNeedsExplicitCancel(
	t *testing.T,
) {
	repositories, smoke, identity := providerExecutionFixture(t)
	blocked := &cancelBlockingExecutionStream{
		started:  make(chan struct{}),
		released: make(chan struct{}),
	}
	provider := &executionProvider{
		identity: identity.Provider,
		stream:   blocked,
		cancel:   blocked.release,
	}
	service, err := NewProviderExecutionService(
		provider,
		repositories,
		providers.RetryPolicy{MaximumAttempts: 1},
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, executeErr := service.Execute(
			ctx,
			ExecuteProviderRequest{
				Request: providerExecutionRequest(smoke, identity),
				PriceSnapshot: &providers.PriceSnapshot{
					ID: smoke.Pricing.ID, Model: identity,
					EffectiveAt: smoke.Pricing.EffectiveAt,
					CapturedAt:  smoke.Pricing.CreatedAt,
					Source:      "unknown",
				},
			},
		)
		done <- executeErr
	}()
	select {
	case <-blocked.started:
	case <-time.After(2 * time.Second):
		t.Fatal("provider Recv did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("explicit provider cancellation error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("provider execution did not stop after explicit Cancel")
	}
	provider.mu.Lock()
	cancelCalls := provider.cancelCalls
	provider.mu.Unlock()
	if cancelCalls != 1 {
		t.Fatalf("explicit provider cancel calls = %d, want 1", cancelCalls)
	}
}

func TestProviderExecutionRejectsMalformedFinalBeforePublicationAndAccountsAttempt(
	t *testing.T,
) {
	repositories, smoke, identity := providerExecutionFixture(t)
	wrongIdentity := identity
	wrongIdentity.Revision = "wrong-revision"
	usage := providers.Usage{
		Known: true, Source: providers.UsageSourceProvider,
		InputTokens: 3, OutputTokens: 2,
	}
	provider := &executionProvider{
		identity: identity.Provider,
		stream: &executionStream{events: []providers.StreamEvent{{
			Sequence: 1, Kind: providers.StreamEventFinal,
			Final: &providers.FinalResponse{
				Identity: wrongIdentity, StopReason: providers.StopReasonCompleted,
				Usage: usage,
			},
		}}},
	}
	service, err := NewProviderExecutionService(
		provider,
		repositories,
		providers.RetryPolicy{MaximumAttempts: 1},
	)
	if err != nil {
		t.Fatal(err)
	}
	var published int
	_, err = service.Execute(
		context.Background(),
		ExecuteProviderRequest{
			Request: providerExecutionRequest(smoke, identity),
			PriceSnapshot: &providers.PriceSnapshot{
				ID: smoke.Pricing.ID, Model: identity,
				EffectiveAt: smoke.Pricing.EffectiveAt,
				CapturedAt:  smoke.Pricing.CreatedAt,
				Source:      "unknown",
			},
			OnStreamEvent: func(context.Context, providers.StreamEvent) error {
				published++
				return nil
			},
		},
	)
	if err == nil {
		t.Fatal("malformed final unexpectedly succeeded")
	}
	if published != 0 {
		t.Fatalf("malformed events published = %d", published)
	}
	report, err := repositories.GetProviderRequestAttribution(
		context.Background(),
		smoke.Request.ID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if report.Request.State != storage.ProviderLogicalRequestFailed ||
		len(report.Attempts) != 1 ||
		report.Attempts[0].Accounting == nil ||
		!report.Attempts[0].Accounting.Usage.Known {
		t.Fatalf("malformed final attribution = %#v", report)
	}
}

func TestProviderExecutionRedactsMetadataBeforeUIDeliveryAndResult(t *testing.T) {
	repositories, smoke, identity := providerExecutionFixture(t)
	const rawSecret = "sk-proj-AAAAAAAAAAAAAAAAAAAA"
	usage := providers.Usage{
		Known: true, Source: providers.UsageSourceProvider,
		InputTokens: 1, OutputTokens: 1,
	}
	unsafeMetadata := providers.RedactedProviderMetadata{
		RequestID:  smoke.Request.ID.String(),
		ResponseID: "response-redaction",
		Fields:     map[string]string{"request_status": rawSecret},
	}
	provider := &executionProvider{
		identity: identity.Provider,
		stream: &executionStream{events: []providers.StreamEvent{
			{
				Sequence: 1, Kind: providers.StreamEventMetadata,
				Metadata: &unsafeMetadata,
			},
			{
				Sequence: 2, Kind: providers.StreamEventFinal,
				Final: &providers.FinalResponse{
					Identity: identity, StopReason: providers.StopReasonCompleted,
					Usage: usage, Metadata: unsafeMetadata,
				},
			},
		}},
	}
	service, err := NewProviderExecutionService(
		provider,
		repositories,
		providers.RetryPolicy{MaximumAttempts: 1},
	)
	if err != nil {
		t.Fatal(err)
	}
	var delivered []string
	result, err := service.Execute(
		context.Background(),
		ExecuteProviderRequest{
			Request: providerExecutionRequest(smoke, identity),
			PriceSnapshot: &providers.PriceSnapshot{
				ID: smoke.Pricing.ID, Model: identity,
				EffectiveAt: smoke.Pricing.EffectiveAt,
				CapturedAt:  smoke.Pricing.CreatedAt,
				Source:      "unknown",
			},
			OnStreamEvent: func(
				_ context.Context,
				event providers.StreamEvent,
			) error {
				if event.Metadata != nil {
					delivered = append(
						delivered,
						event.Metadata.Fields["request_status"],
					)
				}
				if event.Final != nil {
					delivered = append(
						delivered,
						event.Final.Metadata.Fields["request_status"],
					)
				}
				return nil
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(delivered) != 2 {
		t.Fatalf("delivered metadata = %#v", delivered)
	}
	for _, value := range delivered {
		if strings.Contains(value, rawSecret) ||
			!strings.Contains(value, "[REDACTED]") {
			t.Fatalf("unsafe metadata reached UI: %q", value)
		}
	}
	if result.Final == nil ||
		strings.Contains(
			result.Final.Metadata.Fields["request_status"],
			rawSecret,
		) ||
		!strings.Contains(
			result.Final.Metadata.Fields["request_status"],
			"[REDACTED]",
		) {
		t.Fatalf("unsafe metadata reached result: %#v", result.Final)
	}
	report, err := repositories.GetProviderRequestAttribution(
		context.Background(),
		smoke.Request.ID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Attempts) != 1 ||
		report.Attempts[0].Attempt.SafeMetadataJSON == nil ||
		strings.Contains(*report.Attempts[0].Attempt.SafeMetadataJSON, rawSecret) ||
		!strings.Contains(
			*report.Attempts[0].Attempt.SafeMetadataJSON,
			"[REDACTED]",
		) {
		t.Fatalf("unsafe metadata reached storage: %#v", report.Attempts)
	}
}

func TestProviderExecutionFallsBackToUnknownAccountingAfterOneShotWriteFailure(
	t *testing.T,
) {
	repositories, smoke, identity := providerExecutionFixture(t)
	usage := providers.Usage{
		Known: true, Source: providers.UsageSourceProvider,
		InputTokens: 2, OutputTokens: 1,
	}
	provider := &executionProvider{
		identity: identity.Provider,
		stream: &executionStream{events: []providers.StreamEvent{{
			Sequence: 1,
			Kind:     providers.StreamEventFinal,
			Final: &providers.FinalResponse{
				Identity:   identity,
				StopReason: providers.StopReasonCompleted,
				Usage:      usage,
			},
		}}},
	}
	store := &oneShotAccountingFailureStore{
		Repositories: repositories,
		failures:     1,
	}
	service, err := NewProviderExecutionService(
		provider,
		store,
		providers.RetryPolicy{MaximumAttempts: 1},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Execute(
		context.Background(),
		ExecuteProviderRequest{
			Request: providerExecutionRequest(smoke, identity),
			PriceSnapshot: &providers.PriceSnapshot{
				ID: smoke.Pricing.ID, Model: identity,
				EffectiveAt: smoke.Pricing.EffectiveAt,
				CapturedAt:  smoke.Pricing.CreatedAt,
				Source:      "unknown",
			},
		},
	)
	if err == nil {
		t.Fatal("one-shot accounting failure was not surfaced")
	}
	report, reportErr := repositories.GetProviderRequestAttribution(
		context.Background(),
		smoke.Request.ID,
	)
	if reportErr != nil {
		t.Fatal(reportErr)
	}
	if report.Request.State != storage.ProviderLogicalRequestFailed ||
		len(report.Attempts) != 1 ||
		report.Attempts[0].Attempt.State !=
			storage.ProviderRequestAttemptFailed ||
		report.Attempts[0].Accounting == nil ||
		report.Attempts[0].Accounting.Usage.Known ||
		report.Attempts[0].Accounting.Cost != nil {
		t.Fatalf("accounting fallback attribution = %#v", report)
	}
}

func TestProviderStreamEventRejectsMixedKindPayloadBeforePublication(t *testing.T) {
	for _, event := range []providers.StreamEvent{
		{
			Kind: providers.StreamEventTextDelta,
			Text: "visible",
			ToolCall: &providers.ToolCall{
				ID: "smuggled", Name: "must-not-publish", Arguments: []byte(`{}`),
			},
		},
		{
			Kind: providers.StreamEventFinal,
			Text: "smuggled",
			Final: &providers.FinalResponse{
				StopReason: providers.StopReasonCompleted,
			},
		},
	} {
		if err := validateProviderStreamEvent(
			providers.RequestIdentity{},
			event,
			false,
		); err == nil {
			t.Fatalf("mixed normalized event accepted: %#v", event)
		}
	}
}

func TestProviderAccountingStatusRecordsSuccessfulReconciliation(t *testing.T) {
	status := providerAccountingStatus(
		storage.ProviderRequestAccountingSummary{
			AccountingComplete: true,
			Usage: domain.TokenUsage{
				Known:  true,
				Input:  2,
				Output: 1,
				ProviderSpecific: map[string]domain.TokenCount{
					"audio": 4,
				},
			},
		},
		providers.Usage{
			Known:            true,
			Source:           providers.UsageSourceEstimated,
			InputTokens:      2,
			OutputTokens:     1,
			ProviderSpecific: json.RawMessage(`{"audio":4}`),
		},
	)
	if status != storage.ProviderAccountingReconciled {
		t.Fatalf("accounting status = %q, want reconciled", status)
	}
	status = providerAccountingStatus(
		storage.ProviderRequestAccountingSummary{
			AccountingComplete: true,
			Usage: domain.TokenUsage{
				Known:  true,
				Input:  3,
				Output: 1,
			},
		},
		providers.Usage{
			Known:        true,
			Source:       providers.UsageSourceEstimated,
			InputTokens:  2,
			OutputTokens: 1,
		},
	)
	if status != storage.ProviderAccountingDiscrepant {
		t.Fatalf("mismatched accounting status = %q, want discrepant", status)
	}
}

func providerExecutionFixture(
	t *testing.T,
) (*storage.Repositories, storage.LiveProviderSmokeRequest, providers.ModelIdentity) {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "provider-execution.sqlite")
	database, err := storage.Open(ctx, storage.OpenOptions{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := database.Close(closeContext); err != nil {
			t.Errorf("close database: %v", err)
		}
	})
	if _, err := database.Migrate(
		ctx,
		storage.MigrationOptions{
			ApplicationVersion: "provider-execution-test",
			BackupDirectory:    filepath.Join(t.TempDir(), "backups"),
		},
	); err != nil {
		t.Fatal(err)
	}
	repositories, err := storage.NewRepositories(database, nil)
	if err != nil {
		t.Fatal(err)
	}
	const (
		adapter         = "fixture-adapter"
		adapterVersion  = "adapter-v1"
		providerName    = "openai"
		providerVersion = "provider-v1"
		model           = "fixture-model"
		revision        = "fixture-revision"
		requestHash     = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	)
	smoke, err := repositories.PrepareLiveProviderSmokeRequest(
		ctx,
		storage.PrepareLiveProviderSmokeRequest{
			IdempotencyKey:            "provider-execution-fixture",
			RepositoryPath:            filepath.Join(t.TempDir(), "repository"),
			RepositoryGitIdentity:     "provider-execution-git",
			ProviderType:              providerName,
			ProviderDisplayName:       "Provider execution fixture",
			AdapterName:               adapter,
			AdapterVersion:            adapterVersion,
			ProviderVersion:           providerVersion,
			EndpointRedacted:          "https://provider.invalid/v1",
			CapabilitiesJSON:          `{"streaming":true,"tools":true}`,
			OpaqueCredentialReference: "os://provider/execution-test",
			ModelIdentifier:           model,
			ModelVersion:              revision,
			RequestSHA256:             requestHash,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	identity := providers.ModelIdentity{
		Provider: providers.ProviderIdentity{
			Adapter: adapter, AdapterVersion: adapterVersion,
			Provider: providerName, ProviderVersion: providerVersion,
		},
		Model: model, Revision: revision,
	}
	return repositories, smoke, identity
}

func providerExecutionRequest(
	smoke storage.LiveProviderSmokeRequest,
	model providers.ModelIdentity,
) providers.ModelRequest {
	return providers.ModelRequest{
		Identity: providers.RequestIdentity{
			ModelRequestID: smoke.Request.ID,
			Provider:       model.Provider,
			Model:          model,
			IdempotencyKey: smoke.Request.IdempotencyKey,
			RequestHash:    strings.Repeat("a", 64),
		},
		Messages: []providers.Message{{
			Role: providers.MessageRoleUser,
			Content: []providers.ContentPart{{
				Kind: providers.ContentKindText, Text: "test",
			}},
		}},
		MaximumTokens: 16,
	}
}

type executionProvider struct {
	identity    providers.ProviderIdentity
	stream      providers.ModelStream
	mu          sync.Mutex
	cancelCalls int
	streamCalls int
	cancel      func()
}

type oneShotAccountingFailureStore struct {
	*storage.Repositories
	failures int
}

func (store *oneShotAccountingFailureStore) AppendProviderAttemptAccounting(
	ctx context.Context,
	input storage.AppendProviderAttemptAccounting,
) (storage.ProviderAttemptAccounting, error) {
	if store.failures > 0 {
		store.failures--
		return storage.ProviderAttemptAccounting{},
			errors.New("one-shot accounting write failure")
	}
	return store.Repositories.AppendProviderAttemptAccounting(ctx, input)
}

func (provider *executionProvider) ProviderIdentity() providers.ProviderIdentity {
	return provider.identity
}

func (*executionProvider) ListModels(context.Context) ([]providers.ModelDescriptor, error) {
	return nil, nil
}

func (*executionProvider) Capabilities(
	context.Context,
	providers.ModelIdentity,
) (providers.ModelCapabilities, error) {
	return providers.ModelCapabilities{}, nil
}

func (provider *executionProvider) Stream(
	context.Context,
	providers.ModelRequest,
) (providers.ModelStream, error) {
	provider.mu.Lock()
	provider.streamCalls++
	provider.mu.Unlock()
	return provider.stream, nil
}

func (provider *executionProvider) Cancel(
	context.Context,
	domain.ModelRequestID,
) error {
	provider.mu.Lock()
	provider.cancelCalls++
	cancel := provider.cancel
	provider.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

type executionStream struct {
	mu     sync.Mutex
	events []providers.StreamEvent
	index  int
	closed bool
}

func (stream *executionStream) Recv(
	ctx context.Context,
) (providers.StreamEvent, error) {
	if err := ctx.Err(); err != nil {
		return providers.StreamEvent{}, err
	}
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if stream.closed || stream.index >= len(stream.events) {
		return providers.StreamEvent{}, io.EOF
	}
	event := stream.events[stream.index]
	stream.index++
	return event, nil
}

func (stream *executionStream) Close() error {
	stream.mu.Lock()
	stream.closed = true
	stream.mu.Unlock()
	return nil
}

type cancelBlockingExecutionStream struct {
	started     chan struct{}
	released    chan struct{}
	startOnce   sync.Once
	releaseOnce sync.Once
}

func (stream *cancelBlockingExecutionStream) Recv(
	ctx context.Context,
) (providers.StreamEvent, error) {
	stream.startOnce.Do(func() { close(stream.started) })
	<-stream.released
	if err := ctx.Err(); err != nil {
		return providers.StreamEvent{}, err
	}
	return providers.StreamEvent{}, io.EOF
}

func (stream *cancelBlockingExecutionStream) Close() error {
	stream.release()
	return nil
}

func (stream *cancelBlockingExecutionStream) release() {
	stream.releaseOnce.Do(func() { close(stream.released) })
}
