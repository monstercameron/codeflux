package opencompat

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/providers"
)

type CredentialUse func(context.Context, func([]byte) error) error

type Config struct {
	BaseURL        string
	Model          providers.ModelIdentity
	Capabilities   providers.ModelCapabilities
	Transport      *providers.HTTPTransport
	UseCredential  CredentialUse
	AllowRemote    bool
	DiscoverModels bool
}

type Adapter struct {
	config        Config
	mu            sync.Mutex
	active        map[domain.ModelRequestID]context.CancelFunc
	observedTools map[providers.ModelIdentity]bool
}

func New(config Config) (*Adapter, error) {
	if config.BaseURL == "" || config.Transport == nil || config.Model.Model == "" {
		return nil, errors.New("OpenAI-compatible adapter configuration is incomplete")
	}
	config.BaseURL = strings.TrimRight(config.BaseURL, "/")
	if _, err := providers.ValidateProviderEndpoint(
		config.BaseURL,
		providers.EndpointPolicy{AllowRemote: config.AllowRemote},
	); err != nil {
		return nil, err
	}
	if !validConfiguredModel(config.Model) {
		return nil, errors.New("OpenAI-compatible model identity and revision are incomplete")
	}
	return &Adapter{
		config:        config,
		active:        make(map[domain.ModelRequestID]context.CancelFunc),
		observedTools: make(map[providers.ModelIdentity]bool),
	}, nil
}

func (adapter *Adapter) ProviderIdentity() providers.ProviderIdentity {
	return adapter.config.Model.Provider
}

func (adapter *Adapter) ListModels(ctx context.Context) ([]providers.ModelDescriptor, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !adapter.config.DiscoverModels {
		return []providers.ModelDescriptor{{
			Identity: adapter.config.Model, DisplayName: adapter.config.Model.Model,
			Capabilities: adapter.capabilitiesFor(adapter.config.Model),
		}}, nil
	}
	var response struct {
		Object string `json:"object"`
		Data   []struct {
			ID      string `json:"id"`
			Object  string `json:"object"`
			OwnedBy string `json:"owned_by,omitempty"`
		} `json:"data"`
	}
	err := adapter.withCredential(ctx, func(header http.Header) error {
		_, requestErr := adapter.config.Transport.DoJSON(
			ctx,
			providers.HTTPRequest{
				Method:         http.MethodGet,
				Endpoint:       adapter.config.BaseURL + "/v1/models",
				EndpointPolicy: providers.EndpointPolicy{AllowRemote: adapter.config.AllowRemote},
				Header:         header,
			},
			&response,
		)
		return requestErr
	})
	if err != nil {
		return nil, classify("list models", err, providers.PartialEffectEvidence{})
	}
	models := make([]providers.ModelDescriptor, 0, len(response.Data))
	for _, discovered := range response.Data {
		if strings.TrimSpace(discovered.ID) == "" {
			continue
		}
		identity := adapter.config.Model
		identity.Model = discovered.ID
		identity.Revision = discovered.ID
		var capabilities providers.ModelCapabilities
		if discovered.ID == adapter.config.Model.Model {
			identity.Revision = adapter.config.Model.Revision
			capabilities = adapter.capabilitiesFor(identity)
		}
		models = append(models, providers.ModelDescriptor{
			Identity: identity, DisplayName: discovered.ID,
			Capabilities: capabilities,
		})
	}
	return models, nil
}

func (adapter *Adapter) Capabilities(
	ctx context.Context,
	model providers.ModelIdentity,
) (providers.ModelCapabilities, error) {
	if err := ctx.Err(); err != nil {
		return providers.ModelCapabilities{}, err
	}
	if model != adapter.config.Model {
		return providers.ModelCapabilities{}, errors.New("OpenAI-compatible model identity is not configured")
	}
	return adapter.capabilitiesFor(model), nil
}

func (adapter *Adapter) capabilities() providers.ModelCapabilities {
	capabilities := adapter.config.Capabilities
	capabilities.ReasoningControls = append(
		[]string(nil), capabilities.ReasoningControls...,
	)
	return capabilities
}

func (adapter *Adapter) capabilitiesFor(
	model providers.ModelIdentity,
) providers.ModelCapabilities {
	capabilities := adapter.capabilities()
	adapter.mu.Lock()
	if adapter.observedTools[model] {
		capabilities.Tools = true
	}
	adapter.mu.Unlock()
	return capabilities
}

func (adapter *Adapter) recordToolSupport(model providers.ModelIdentity) {
	adapter.mu.Lock()
	adapter.observedTools[model] = true
	adapter.mu.Unlock()
}

func (adapter *Adapter) Stream(
	ctx context.Context,
	request providers.ModelRequest,
) (providers.ModelStream, error) {
	if err := providers.ValidateModelRequest(
		request,
		adapter.config.Model,
		adapter.capabilitiesFor(adapter.config.Model),
		false,
	); err != nil {
		return nil, err
	}
	body, err := encodeRequest(request, adapter.config.Model.Model)
	if err != nil {
		return nil, err
	}
	var streamContext context.Context
	var cancel context.CancelFunc
	if request.Deadline.IsZero() {
		streamContext, cancel = context.WithCancel(ctx)
	} else {
		streamContext, cancel = context.WithDeadline(ctx, request.Deadline)
	}
	stream := newModelStream(streamContext, cancel)
	adapter.mu.Lock()
	if _, exists := adapter.active[request.Identity.ModelRequestID]; exists {
		adapter.mu.Unlock()
		cancel()
		return nil, errors.New("model request is already active")
	}
	adapter.active[request.Identity.ModelRequestID] = cancel
	adapter.mu.Unlock()
	go adapter.run(streamContext, request, body, stream)
	return stream, nil
}

func (adapter *Adapter) Cancel(
	ctx context.Context,
	requestID domain.ModelRequestID,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	adapter.mu.Lock()
	cancel := adapter.active[requestID]
	adapter.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

func (adapter *Adapter) run(
	ctx context.Context,
	request providers.ModelRequest,
	body []byte,
	stream *modelStream,
) {
	defer func() {
		adapter.mu.Lock()
		delete(adapter.active, request.Identity.ModelRequestID)
		adapter.mu.Unlock()
		stream.finish()
	}()
	state := streamState{
		model: adapter.config.Model,
		usage: providers.Usage{Source: providers.UsageSourceUnknown},
		metadata: providers.RedactedProviderMetadata{
			RequestID: request.Identity.ModelRequestID.String(),
			Fields:    make(map[string]string),
		},
		tools: make(map[int]*toolState),
	}
	var transportResult providers.HTTPResult
	err := adapter.withCredential(ctx, func(header http.Header) error {
		header.Set("Content-Type", "application/json")
		header.Set("Accept", "text/event-stream")
		var streamErr error
		transportResult, streamErr = adapter.config.Transport.StreamSSE(
			ctx,
			providers.HTTPRequest{
				Method:         http.MethodPost,
				Endpoint:       adapter.config.BaseURL + "/v1/chat/completions",
				EndpointPolicy: providers.EndpointPolicy{AllowRemote: adapter.config.AllowRemote},
				Header:         header, Body: body,
			},
			func(event providers.SSEEvent) error {
				if event.Data == "[DONE]" {
					return state.markDone()
				}
				return state.consume(ctx, event, stream, func() {
					adapter.recordToolSupport(adapter.config.Model)
				})
			},
		)
		return streamErr
	})
	if err != nil {
		state.partial.LastSequence = stream.sequence
		stream.fail(classify("stream", err, state.partial))
		return
	}
	if !state.done {
		state.partial.LastSequence = stream.sequence
		stream.fail(classify(
			"stream",
			errors.New("OpenAI-compatible stream ended without [DONE]"),
			state.partial,
		))
		return
	}
	if requestID := safeResponseHeader(
		transportResult.ResponseHeader("x-request-id"),
	); requestID != "" {
		state.metadata.Fields["provider_request_id"] = requestID
	}
	metadata := cloneMetadata(state.metadata)
	state.partial.LastSequence = stream.sequence
	final := &providers.FinalResponse{
		Identity: state.model, StopReason: state.stop, Usage: state.usage,
		Metadata: metadata, PartialEffect: state.partial,
	}
	if err := stream.send(ctx, providers.StreamEvent{
		Kind: providers.StreamEventFinal, Final: final,
	}); err != nil {
		stream.fail(classify("complete stream", err, state.partial))
	}
}

func (adapter *Adapter) withCredential(
	ctx context.Context,
	operation func(http.Header) error,
) error {
	header := make(http.Header)
	if adapter.config.UseCredential == nil {
		return operation(header)
	}
	return adapter.config.UseCredential(ctx, func(secret []byte) error {
		header.Set("Authorization", "Bearer "+string(secret))
		defer header.Del("Authorization")
		return operation(header)
	})
}

type requestBody struct {
	Model          string          `json:"model"`
	Messages       []wireMessage   `json:"messages"`
	Tools          []wireTool      `json:"tools,omitempty"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
	MaxTokens      int64           `json:"max_tokens,omitempty"`
	Temperature    *float64        `json:"temperature,omitempty"`
	Stream         bool            `json:"stream"`
	StreamOptions  streamOptions   `json:"stream_options"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type wireMessage struct {
	Role       string         `json:"role"`
	Content    any            `json:"content,omitempty"`
	ToolCalls  []wireToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

type contentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL *struct {
		URL    string `json:"url"`
		Detail string `json:"detail,omitempty"`
	} `json:"image_url,omitempty"`
}

type wireTool struct {
	Type     string       `json:"type"`
	Function wireFunction `json:"function"`
}

type wireFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Arguments   json.RawMessage `json:"arguments,omitempty"`
}

type wireToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function wireFunction `json:"function"`
}

type responseFormat struct {
	Type       string     `json:"type"`
	JSONSchema jsonSchema `json:"json_schema"`
}

type jsonSchema struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Schema      json.RawMessage `json:"schema"`
	Strict      bool            `json:"strict"`
}

func encodeRequest(request providers.ModelRequest, model string) ([]byte, error) {
	body := requestBody{
		Model: model, MaxTokens: request.MaximumTokens,
		Temperature: request.Temperature, Stream: true,
		StreamOptions: streamOptions{IncludeUsage: true},
	}
	for _, message := range request.Messages {
		wire := wireMessage{Role: string(message.Role)}
		var blocks []contentBlock
		for _, part := range message.Content {
			switch part.Kind {
			case providers.ContentKindText:
				blocks = append(blocks, contentBlock{Type: "text", Text: part.Text})
			case providers.ContentKindImage:
				if part.Image == nil || len(part.Image.Data) == 0 ||
					part.Image.MediaType == "" {
					return nil, errors.New("OpenAI-compatible image input must be embedded with a media type")
				}
				block := contentBlock{Type: "image_url"}
				block.ImageURL = &struct {
					URL    string `json:"url"`
					Detail string `json:"detail,omitempty"`
				}{
					URL: "data:" + part.Image.MediaType + ";base64," +
						base64.StdEncoding.EncodeToString(part.Image.Data),
					Detail: part.Image.Detail,
				}
				blocks = append(blocks, block)
			case providers.ContentKindToolCall:
				if part.ToolCall == nil {
					return nil, errors.New("OpenAI-compatible tool call is missing")
				}
				wire.ToolCalls = append(wire.ToolCalls, wireToolCall{
					ID: part.ToolCall.ID, Type: "function",
					Function: wireFunction{
						Name: part.ToolCall.Name, Arguments: part.ToolCall.Arguments,
					},
				})
			case providers.ContentKindToolResult:
				if part.ToolResult == nil {
					return nil, errors.New("OpenAI-compatible tool result is missing")
				}
				wire.Role = "tool"
				wire.ToolCallID = part.ToolResult.CallID
				var text strings.Builder
				for _, content := range part.ToolResult.Content {
					if content.Kind != providers.ContentKindText {
						return nil, errors.New("OpenAI-compatible tool result supports text only")
					}
					text.WriteString(content.Text)
				}
				wire.Content = text.String()
			default:
				return nil, errors.New("OpenAI-compatible content kind is unsupported")
			}
		}
		if wire.Content == nil && len(blocks) == 1 && blocks[0].Type == "text" {
			wire.Content = blocks[0].Text
		} else if wire.Content == nil && len(blocks) > 0 {
			wire.Content = blocks
		}
		body.Messages = append(body.Messages, wire)
	}
	for _, tool := range request.Tools {
		if tool.Name == "" || len(tool.InputSchema) == 0 {
			return nil, errors.New("OpenAI-compatible tool declaration is incomplete")
		}
		body.Tools = append(body.Tools, wireTool{
			Type: "function",
			Function: wireFunction{
				Name: tool.Name, Description: tool.Description,
				Parameters: tool.InputSchema,
			},
		})
	}
	if requirement := request.StructuredOutput; requirement != nil {
		if requirement.Name == "" || len(requirement.Schema) == 0 {
			return nil, errors.New("OpenAI-compatible structured output is incomplete")
		}
		body.ResponseFormat = &responseFormat{
			Type: "json_schema",
			JSONSchema: jsonSchema{
				Name: requirement.Name, Description: requirement.Description,
				Schema: requirement.Schema, Strict: requirement.Strict,
			},
		}
	}
	return json.Marshal(body)
}

type streamState struct {
	model        providers.ModelIdentity
	usage        providers.Usage
	stop         providers.StopReason
	metadata     providers.RedactedProviderMetadata
	partial      providers.PartialEffectEvidence
	tools        map[int]*toolState
	started      bool
	done         bool
	metadataSent bool
}

type toolState struct {
	id        string
	name      string
	arguments strings.Builder
}

type chunk struct {
	ID                string `json:"id"`
	Model             string `json:"model"`
	SystemFingerprint string `json:"system_fingerprint"`
	Choices           []struct {
		Delta struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens        int64 `json:"prompt_tokens"`
		CompletionTokens    int64 `json:"completion_tokens"`
		TotalTokens         int64 `json:"total_tokens"`
		PromptTokensDetails *struct {
			CachedTokens int64 `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	} `json:"usage"`
}

func (state *streamState) consume(
	ctx context.Context,
	event providers.SSEEvent,
	stream *modelStream,
	observeTools func(),
) error {
	if state.done {
		return errors.New("OpenAI-compatible endpoint emitted data after [DONE]")
	}
	var value chunk
	if err := json.Unmarshal([]byte(event.Data), &value); err != nil {
		return err
	}
	if value.ID == "" && value.Model == "" &&
		len(value.Choices) == 0 && value.Usage == nil {
		return errors.New("OpenAI-compatible stream chunk has no recognized fields")
	}
	state.started = true
	state.partial.ProviderAck = true
	if value.ID != "" {
		state.metadata.ResponseID = value.ID
	}
	if value.Model != "" {
		state.model.Revision = value.Model
	}
	if value.SystemFingerprint != "" {
		state.metadata.Fingerprint = value.SystemFingerprint
	}
	if !state.metadataSent &&
		(state.metadata.ResponseID != "" || value.Model != "") {
		state.metadataSent = true
		metadata := cloneMetadata(state.metadata)
		if err := stream.send(ctx, providers.StreamEvent{
			Kind: providers.StreamEventMetadata, Metadata: &metadata,
		}); err != nil {
			return err
		}
	}
	if value.Usage != nil {
		if value.Usage.PromptTokens < 0 ||
			value.Usage.CompletionTokens < 0 ||
			value.Usage.TotalTokens < 0 ||
			value.Usage.PromptTokensDetails != nil &&
				value.Usage.PromptTokensDetails.CachedTokens < 0 {
			return errors.New("OpenAI-compatible usage must not be negative")
		}
		state.usage.Known = true
		state.usage.Source = providers.UsageSourceProvider
		state.usage.InputTokens = value.Usage.PromptTokens
		state.usage.OutputTokens = value.Usage.CompletionTokens
		if value.Usage.PromptTokensDetails != nil {
			state.usage.CachedInputTokens = value.Usage.PromptTokensDetails.CachedTokens
		}
		usage := state.usage
		if err := stream.send(ctx, providers.StreamEvent{
			Kind: providers.StreamEventUsage, Usage: &usage,
		}); err != nil {
			return err
		}
	}
	for _, choice := range value.Choices {
		if choice.Delta.Content != "" {
			state.partial.StreamedOutput = true
			if err := stream.send(ctx, providers.StreamEvent{
				Kind: providers.StreamEventTextDelta, Text: choice.Delta.Content,
			}); err != nil {
				return err
			}
		}
		for _, delta := range choice.Delta.ToolCalls {
			tool := state.tools[delta.Index]
			if tool == nil {
				tool = &toolState{}
				state.tools[delta.Index] = tool
			}
			if delta.ID != "" {
				tool.id = delta.ID
			}
			if delta.Function.Name != "" {
				tool.name = delta.Function.Name
			}
			tool.arguments.WriteString(delta.Function.Arguments)
			state.partial.ProviderAck = true
			observeTools()
			if err := stream.send(ctx, providers.StreamEvent{
				Kind: providers.StreamEventToolCallDelta,
				ToolCallDelta: &providers.ToolCallDelta{
					Index: delta.Index, ID: tool.id, Name: tool.name,
					ArgumentsFragment: delta.Function.Arguments,
				},
			}); err != nil {
				return err
			}
		}
		if choice.FinishReason != "" {
			state.stop = normalizeStop(choice.FinishReason)
			for index, tool := range state.tools {
				arguments := json.RawMessage(tool.arguments.String())
				if !json.Valid(arguments) {
					return errors.New("OpenAI-compatible tool arguments are not complete JSON")
				}
				state.partial.ToolCall = true
				if err := stream.send(ctx, providers.StreamEvent{
					Kind: providers.StreamEventToolCall,
					ToolCall: &providers.ToolCall{
						ID: tool.id, Name: tool.name, Arguments: arguments,
					},
				}); err != nil {
					return err
				}
				delete(state.tools, index)
			}
		}
	}
	return nil
}

func (state *streamState) markDone() error {
	if state.done {
		return errors.New("OpenAI-compatible endpoint emitted duplicate [DONE]")
	}
	if !state.started {
		return errors.New("OpenAI-compatible endpoint ended before any response chunk")
	}
	if len(state.tools) != 0 {
		return errors.New("OpenAI-compatible endpoint ended with an incomplete tool call")
	}
	state.done = true
	return nil
}

func normalizeStop(value string) providers.StopReason {
	switch value {
	case "stop":
		return providers.StopReasonCompleted
	case "length":
		return providers.StopReasonMaximumTokens
	case "tool_calls", "function_call":
		return providers.StopReasonToolCalls
	case "content_filter":
		return providers.StopReasonSafety
	default:
		return providers.StopReasonUnknown
	}
}

func classify(
	operation string,
	err error,
	partial providers.PartialEffectEvidence,
) error {
	if err == nil {
		return nil
	}
	failure := &providers.Failure{
		Operation: operation, Cause: err, PartialEffect: partial,
	}
	if errors.Is(err, context.Canceled) {
		failure.Kind, failure.Cause = providers.FailureCanceled, providers.ErrCanceled
		return failure
	}
	if errors.Is(err, context.DeadlineExceeded) {
		failure.Kind, failure.Cause = providers.FailureTimeout, providers.ErrTimeout
		failure.Retryable = !hasPartialEffect(partial)
		return failure
	}
	var status *providers.HTTPStatusError
	if !errors.As(err, &status) {
		failure.Kind, failure.Cause = providers.FailureTransport, providers.ErrTransport
		failure.Retryable = !hasPartialEffect(partial)
		return failure
	}
	failure.RetryAfter = status.RetryAfter
	switch status.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		failure.Kind, failure.Cause = providers.FailureAuthentication, providers.ErrAuthentication
	case http.StatusTooManyRequests:
		failure.Kind, failure.Cause = providers.FailureRateLimited, providers.ErrRateLimited
		failure.Retryable = !hasPartialEffect(partial)
	default:
		if status.StatusCode >= 500 {
			failure.Kind, failure.Cause = providers.FailureUnavailable, providers.ErrUnavailable
			failure.Retryable = !hasPartialEffect(partial)
		} else {
			failure.Kind, failure.Cause = providers.FailureInvalidRequest, providers.ErrInvalidRequest
		}
	}
	return failure
}

func hasPartialEffect(partial providers.PartialEffectEvidence) bool {
	return partial.StreamedOutput || partial.ToolCall || partial.ProviderAck
}

func canceledFailure(operation string, err error) error {
	kind := providers.FailureCanceled
	cause := providers.ErrCanceled
	if errors.Is(err, context.DeadlineExceeded) {
		kind = providers.FailureTimeout
		cause = providers.ErrTimeout
	}
	return &providers.Failure{
		Kind: kind, Operation: operation, Cause: cause,
	}
}

func cloneMetadata(
	value providers.RedactedProviderMetadata,
) providers.RedactedProviderMetadata {
	copy := value
	copy.Fields = make(map[string]string, len(value.Fields))
	for key, field := range value.Fields {
		copy.Fields[key] = field
	}
	return copy
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

func validConfiguredModel(model providers.ModelIdentity) bool {
	if model.Provider.Adapter != "openai-compatible" ||
		strings.TrimSpace(model.Provider.Provider) == "" {
		return false
	}
	for _, value := range []string{
		model.Provider.AdapterVersion,
		model.Provider.ProviderVersion,
		model.Model,
		model.Revision,
	} {
		if value == "" || len(value) > 255 ||
			strings.TrimSpace(value) != value {
			return false
		}
	}
	return true
}

type eventResult struct {
	event providers.StreamEvent
	err   error
}

type modelStream struct {
	ctx      context.Context
	cancel   context.CancelFunc
	events   chan eventResult
	once     sync.Once
	sequence int64
}

func newModelStream(ctx context.Context, cancel context.CancelFunc) *modelStream {
	return &modelStream{
		ctx: ctx, cancel: cancel, events: make(chan eventResult, 16),
	}
}

func (stream *modelStream) Recv(ctx context.Context) (providers.StreamEvent, error) {
	if err := ctx.Err(); err != nil {
		return providers.StreamEvent{}, err
	}
	if err := stream.ctx.Err(); err != nil {
		return providers.StreamEvent{}, canceledFailure("receive stream", err)
	}
	select {
	case result, ok := <-stream.events:
		if err := ctx.Err(); err != nil {
			return providers.StreamEvent{}, err
		}
		if err := stream.ctx.Err(); err != nil {
			return providers.StreamEvent{}, canceledFailure("receive stream", err)
		}
		if !ok {
			return providers.StreamEvent{}, io.EOF
		}
		return result.event, result.err
	case <-ctx.Done():
		return providers.StreamEvent{}, ctx.Err()
	case <-stream.ctx.Done():
		return providers.StreamEvent{}, canceledFailure(
			"receive stream", stream.ctx.Err(),
		)
	}
}

func (stream *modelStream) Close() error {
	stream.cancel()
	return nil
}

func (stream *modelStream) send(
	ctx context.Context,
	event providers.StreamEvent,
) error {
	stream.sequence++
	event.Sequence = stream.sequence
	event.ObservedAt = time.Now().UTC()
	select {
	case stream.events <- eventResult{event: event}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (stream *modelStream) fail(err error) {
	select {
	case stream.events <- eventResult{err: err}:
	case <-stream.ctx.Done():
	}
}

func (stream *modelStream) finish() {
	stream.once.Do(func() { close(stream.events) })
}

var _ providers.ModelProvider = (*Adapter)(nil)
