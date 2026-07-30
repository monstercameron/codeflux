// Package openai adapts the OpenAI Responses API to the normalized provider
// contract.
package openai

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/providers"
)

const (
	defaultEndpoint        = "https://api.openai.com/v1"
	defaultAdapterVersion  = "1"
	defaultProviderVersion = "responses-v1"
)

// CredentialSource exposes a provider credential only for the duration of one
// bounded operation.
type CredentialSource interface {
	Use(context.Context, domain.ProviderID, func([]byte) error) error
}

// Config binds one OpenAI configuration to exact model and capability
// evidence. Capabilities are configured evidence; model listing alone never
// proves them.
type Config struct {
	ProviderID       domain.ProviderID
	Endpoint         string
	RemoteApproved   bool
	Model            string
	ModelRevision    string
	AdapterVersion   string
	ProviderVersion  string
	Capabilities     providers.ModelCapabilities
	Credentials      CredentialSource
	Transport        *providers.HTTPTransport
	TransportOptions providers.TransportOptions
	Now              func() time.Time
}

// Adapter implements the normalized provider boundary with the OpenAI
// Responses API.
type Adapter struct {
	providerID   domain.ProviderID
	endpoint     string
	policy       providers.EndpointPolicy
	model        providers.ModelIdentity
	capabilities providers.ModelCapabilities
	credentials  CredentialSource
	transport    *providers.HTTPTransport
	now          func() time.Time

	mu     sync.Mutex
	active map[domain.ModelRequestID]context.CancelFunc
}

// New validates one exact OpenAI model configuration.
func New(config Config) (*Adapter, error) {
	if config.ProviderID.IsZero() {
		return nil, errors.New("OpenAI provider ID is required")
	}
	if config.Endpoint == "" {
		config.Endpoint = defaultEndpoint
	}
	endpoint, err := providers.ValidateProviderEndpoint(
		config.Endpoint,
		providers.EndpointPolicy{AllowRemote: config.RemoteApproved},
	)
	if err != nil {
		return nil, fmt.Errorf("validate OpenAI endpoint: %w", err)
	}
	if strings.TrimSpace(config.Model) == "" ||
		strings.TrimSpace(config.ModelRevision) == "" {
		return nil, errors.New("OpenAI model and exact revision are required")
	}
	if config.Credentials == nil {
		return nil, errors.New("OpenAI credential source is required")
	}
	if config.AdapterVersion == "" {
		config.AdapterVersion = defaultAdapterVersion
	}
	if config.ProviderVersion == "" {
		config.ProviderVersion = defaultProviderVersion
	}
	if config.Transport == nil {
		config.Transport, err = providers.NewHTTPTransport(config.TransportOptions)
		if err != nil {
			return nil, fmt.Errorf("create OpenAI transport: %w", err)
		}
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	identity := providers.ProviderIdentity{
		Adapter:         "openai-responses",
		AdapterVersion:  config.AdapterVersion,
		Provider:        "openai",
		ProviderVersion: config.ProviderVersion,
	}
	return &Adapter{
		providerID:   config.ProviderID,
		endpoint:     strings.TrimRight(endpoint.String(), "/"),
		policy:       providers.EndpointPolicy{AllowRemote: config.RemoteApproved},
		model:        providers.ModelIdentity{Provider: identity, Model: config.Model, Revision: config.ModelRevision},
		capabilities: cloneCapabilities(config.Capabilities),
		credentials:  config.Credentials,
		transport:    config.Transport,
		now:          config.Now,
		active:       make(map[domain.ModelRequestID]context.CancelFunc),
	}, nil
}

func (adapter *Adapter) ProviderIdentity() providers.ProviderIdentity {
	return adapter.model.Provider
}

// ListModels discovers provider-returned identifiers without inventing
// capabilities for models other than the configured, version-bound model.
func (adapter *Adapter) ListModels(ctx context.Context) ([]providers.ModelDescriptor, error) {
	var response listModelsResponse
	err := adapter.withCredential(ctx, func(header http.Header) error {
		_, requestErr := adapter.transport.DoJSON(ctx, providers.HTTPRequest{
			Method:         http.MethodGet,
			Endpoint:       adapter.endpoint + "/models",
			Header:         header,
			EndpointPolicy: adapter.policy,
		}, &response)
		return requestErr
	})
	if err != nil {
		return nil, classifyFailure("list models", err, providers.PartialEffectEvidence{})
	}
	models := make([]providers.ModelDescriptor, 0, len(response.Data))
	for _, candidate := range response.Data {
		if strings.TrimSpace(candidate.ID) == "" {
			continue
		}
		identity := providers.ModelIdentity{
			Provider: adapter.model.Provider,
			Model:    candidate.ID,
			Revision: candidate.ID,
		}
		var capabilities providers.ModelCapabilities
		if candidate.ID == adapter.model.Model {
			identity.Revision = adapter.model.Revision
			capabilities = cloneCapabilities(adapter.capabilities)
		}
		models = append(models, providers.ModelDescriptor{
			Identity: identity, DisplayName: candidate.ID, Capabilities: capabilities,
		})
	}
	return models, nil
}

func (adapter *Adapter) Capabilities(
	ctx context.Context,
	model providers.ModelIdentity,
) (providers.ModelCapabilities, error) {
	if err := ctx.Err(); err != nil {
		return providers.ModelCapabilities{}, classifyFailure(
			"get capabilities", err, providers.PartialEffectEvidence{},
		)
	}
	if model.Provider != adapter.model.Provider ||
		model.Model != adapter.model.Model ||
		model.Revision != adapter.model.Revision {
		return providers.ModelCapabilities{}, &providers.Failure{
			Kind: providers.FailureInvalidRequest, Operation: "get capabilities",
			Cause: providers.ErrInvalidRequest,
		}
	}
	return cloneCapabilities(adapter.capabilities), nil
}

func (adapter *Adapter) Stream(
	ctx context.Context,
	request providers.ModelRequest,
) (providers.ModelStream, error) {
	body, err := adapter.buildRequest(request)
	if err != nil {
		return nil, &providers.Failure{
			Kind: providers.FailureInvalidRequest, Operation: "build response request",
			Cause: fmt.Errorf("%w: %v", providers.ErrInvalidRequest, err),
		}
	}
	var streamContext context.Context
	var cancel context.CancelFunc
	if request.Deadline.IsZero() {
		streamContext, cancel = context.WithCancel(ctx)
	} else {
		streamContext, cancel = context.WithDeadline(ctx, request.Deadline)
	}
	adapter.mu.Lock()
	if _, exists := adapter.active[request.Identity.ModelRequestID]; exists {
		adapter.mu.Unlock()
		cancel()
		return nil, &providers.Failure{
			Kind: providers.FailureInvalidRequest, Operation: "start response stream",
			Cause: fmt.Errorf("%w: model request is already active", providers.ErrInvalidRequest),
		}
	}
	adapter.active[request.Identity.ModelRequestID] = cancel
	adapter.mu.Unlock()

	stream := newResponseStream(streamContext, cancel, adapter.now)
	go adapter.runStream(stream, request, body)
	return stream, nil
}

func (adapter *Adapter) Cancel(
	ctx context.Context,
	requestID domain.ModelRequestID,
) error {
	if err := ctx.Err(); err != nil {
		return classifyFailure("cancel response", err, providers.PartialEffectEvidence{})
	}
	if requestID.IsZero() {
		return &providers.Failure{
			Kind: providers.FailureInvalidRequest, Operation: "cancel response",
			Cause: providers.ErrInvalidRequest,
		}
	}
	adapter.mu.Lock()
	cancel := adapter.active[requestID]
	adapter.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	// Cancellation is idempotent: an absent request is already final or canceled.
	return nil
}

func (adapter *Adapter) runStream(
	stream *responseStream,
	request providers.ModelRequest,
	body []byte,
) {
	defer func() {
		adapter.mu.Lock()
		delete(adapter.active, request.Identity.ModelRequestID)
		adapter.mu.Unlock()
		stream.finish()
	}()
	header := make(http.Header)
	header.Set("Accept", "text/event-stream")
	header.Set("Content-Type", "application/json")
	if request.Idempotency.ProviderSupported && request.Idempotency.Key != "" {
		header.Set("Idempotency-Key", request.Idempotency.Key)
	}
	var transportResult providers.HTTPResult
	err := adapter.withCredential(stream.ctx, func(authorized http.Header) error {
		for key, values := range header {
			authorized[key] = append([]string(nil), values...)
		}
		var streamErr error
		transportResult, streamErr = adapter.transport.StreamSSE(
			stream.ctx,
			providers.HTTPRequest{
				Method:         http.MethodPost,
				Endpoint:       adapter.endpoint + "/responses",
				Header:         authorized,
				Body:           body,
				EndpointPolicy: adapter.policy,
			},
			func(event providers.SSEEvent) error {
				return stream.consume(event, request.Identity)
			},
		)
		return streamErr
	})
	if err != nil {
		stream.fail(classifyFailure(
			"stream response",
			err,
			stream.snapshotPartial(),
		))
		return
	}
	if stream.pendingFinal == nil {
		stream.fail(classifyFailure(
			"stream response",
			errors.New("OpenAI response stream ended without a final event"),
			stream.snapshotPartial(),
		))
		return
	}
	if requestID := safeResponseHeader(
		transportResult.ResponseHeader("x-request-id"),
	); requestID != "" {
		stream.pendingFinal.Metadata.Fields["openai_request_id"] = requestID
	}
	if err := stream.complete(); err != nil {
		stream.fail(classifyFailure(
			"complete response",
			err,
			stream.snapshotPartial(),
		))
	}
}

func (adapter *Adapter) withCredential(
	ctx context.Context,
	operation func(http.Header) error,
) error {
	return adapter.credentials.Use(ctx, adapter.providerID, func(secret []byte) error {
		if len(secret) == 0 {
			return providers.ErrAuthentication
		}
		header := make(http.Header)
		header.Set("Authorization", "Bearer "+string(secret))
		return operation(header)
	})
}

func cloneCapabilities(value providers.ModelCapabilities) providers.ModelCapabilities {
	value.ReasoningControls = append([]string(nil), value.ReasoningControls...)
	return value
}

func safeResponseHeader(value string) string {
	if value == "" || len(value) > 128 {
		return ""
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' ||
			character == '_' || character == '-' || character == '.' {
			continue
		}
		return ""
	}
	return value
}

type listModelsResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

var _ providers.ModelProvider = (*Adapter)(nil)
