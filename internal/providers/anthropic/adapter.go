package anthropic

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/providers"
)

const apiVersion = "2023-06-01"

type CredentialUse func(context.Context, func([]byte) error) error

type Config struct {
	BaseURL        string
	Model          providers.ModelIdentity
	Capabilities   providers.ModelCapabilities
	Transport      *providers.HTTPTransport
	UseCredential  CredentialUse
	RemoteApproved bool
}

type Adapter struct {
	config Config
	mu     sync.Mutex
	active map[domain.ModelRequestID]context.CancelFunc
}

func New(config Config) (*Adapter, error) {
	if config.BaseURL == "" || config.Transport == nil ||
		config.UseCredential == nil || config.Model.Model == "" {
		return nil, errors.New("anthropic adapter configuration is incomplete")
	}
	base := strings.TrimRight(config.BaseURL, "/")
	if _, err := providers.ValidateProviderEndpoint(
		base, providers.EndpointPolicy{AllowRemote: config.RemoteApproved},
	); err != nil {
		return nil, err
	}
	if !validConfiguredModel(config.Model, "anthropic-messages", "anthropic") {
		return nil, errors.New("anthropic model identity and revision are incomplete")
	}
	config.BaseURL = base
	return &Adapter{config: config, active: make(map[domain.ModelRequestID]context.CancelFunc)}, nil
}

func (adapter *Adapter) ProviderIdentity() providers.ProviderIdentity {
	return adapter.config.Model.Provider
}

func (adapter *Adapter) ListModels(
	ctx context.Context,
) ([]providers.ModelDescriptor, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	capabilities := adapter.config.Capabilities
	capabilities.ReasoningControls = append(
		[]string(nil), capabilities.ReasoningControls...,
	)
	return []providers.ModelDescriptor{{
		Identity: adapter.config.Model, DisplayName: adapter.config.Model.Model,
		Capabilities: capabilities,
	}}, nil
}

func (adapter *Adapter) Capabilities(
	ctx context.Context,
	model providers.ModelIdentity,
) (providers.ModelCapabilities, error) {
	if err := ctx.Err(); err != nil {
		return providers.ModelCapabilities{}, err
	}
	if model != adapter.config.Model {
		return providers.ModelCapabilities{}, errors.New("anthropic model is not configured")
	}
	capabilities := adapter.config.Capabilities
	capabilities.ReasoningControls = append(
		[]string(nil), capabilities.ReasoningControls...,
	)
	return capabilities, nil
}

func (adapter *Adapter) Stream(
	ctx context.Context,
	request providers.ModelRequest,
) (providers.ModelStream, error) {
	if err := providers.ValidateModelRequest(
		request,
		adapter.config.Model,
		adapter.config.Capabilities,
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
	err := adapter.config.UseCredential(ctx, func(secret []byte) error {
		header := make(http.Header)
		header.Set("Content-Type", "application/json")
		header.Set("Accept", "text/event-stream")
		header.Set("anthropic-version", apiVersion)
		header.Set("x-api-key", string(secret))
		defer header.Del("x-api-key")
		var streamErr error
		transportResult, streamErr = adapter.config.Transport.StreamSSE(
			ctx,
			providers.HTTPRequest{
				Method:         http.MethodPost,
				Endpoint:       adapter.config.BaseURL + "/v1/messages",
				EndpointPolicy: providers.EndpointPolicy{AllowRemote: adapter.config.RemoteApproved},
				Header:         header, Body: body,
			},
			func(event providers.SSEEvent) error {
				return state.consume(ctx, event, stream)
			},
		)
		return streamErr
	})
	if err != nil {
		state.partial.LastSequence = stream.sequence
		stream.fail(classify("stream", err, state.partial))
		return
	}
	if state.pendingFinal == nil {
		state.partial.LastSequence = stream.sequence
		stream.fail(classify(
			"stream",
			errors.New("anthropic stream ended without message_stop"),
			state.partial,
		))
		return
	}
	if requestID := safeResponseHeader(
		transportResult.ResponseHeader("request-id"),
	); requestID != "" {
		state.pendingFinal.Metadata.Fields["anthropic_request_id"] = requestID
	}
	state.pendingFinal.PartialEffect.LastSequence = stream.sequence
	if err := stream.send(ctx, providers.StreamEvent{
		Kind: providers.StreamEventFinal, Final: state.pendingFinal,
	}); err != nil {
		stream.fail(classify("complete stream", err, state.partial))
	}
}

type requestBody struct {
	Model        string        `json:"model"`
	MaxTokens    int64         `json:"max_tokens"`
	Messages     []wireMessage `json:"messages"`
	System       string        `json:"system,omitempty"`
	Tools        []wireTool    `json:"tools,omitempty"`
	OutputConfig *outputConfig `json:"output_config,omitempty"`
	Stream       bool          `json:"stream"`
}

type wireMessage struct {
	Role    string        `json:"role"`
	Content []wireContent `json:"content"`
}

type wireContent struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   any             `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
	Source    *imageSource    `json:"source,omitempty"`
}

type imageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type wireTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type outputConfig struct {
	Format outputFormat `json:"format"`
}

type outputFormat struct {
	Type   string          `json:"type"`
	Schema json.RawMessage `json:"schema"`
}

func encodeRequest(request providers.ModelRequest, model string) ([]byte, error) {
	body := requestBody{Model: model, MaxTokens: request.MaximumTokens, Stream: true}
	if body.MaxTokens < 1 {
		return nil, errors.New("anthropic maximum tokens must be positive")
	}
	for _, message := range request.Messages {
		if message.Role == providers.MessageRoleSystem ||
			message.Role == providers.MessageRoleDeveloper {
			for _, part := range message.Content {
				if part.Kind != providers.ContentKindText {
					return nil, errors.New("anthropic system content must be text")
				}
				if body.System != "" {
					body.System += "\n\n"
				}
				body.System += part.Text
			}
			continue
		}
		role := string(message.Role)
		if role == string(providers.MessageRoleTool) {
			role = string(providers.MessageRoleUser)
		}
		if role != "user" && role != "assistant" {
			return nil, errors.New("anthropic message role is unsupported")
		}
		wire := wireMessage{Role: role}
		for _, part := range message.Content {
			content, err := encodeContent(part)
			if err != nil {
				return nil, err
			}
			wire.Content = append(wire.Content, content)
		}
		body.Messages = append(body.Messages, wire)
	}
	for _, tool := range request.Tools {
		if tool.Name == "" || len(tool.InputSchema) == 0 {
			return nil, errors.New("anthropic tool declaration is incomplete")
		}
		body.Tools = append(body.Tools, wireTool{
			Name: tool.Name, Description: tool.Description,
			InputSchema: append(json.RawMessage(nil), tool.InputSchema...),
		})
	}
	if requirement := request.StructuredOutput; requirement != nil {
		if len(requirement.Schema) == 0 {
			return nil, errors.New("anthropic structured output schema is required")
		}
		body.OutputConfig = &outputConfig{Format: outputFormat{
			Type: "json_schema", Schema: append(json.RawMessage(nil), requirement.Schema...),
		}}
	}
	return json.Marshal(body)
}

func encodeContent(part providers.ContentPart) (wireContent, error) {
	switch part.Kind {
	case providers.ContentKindText:
		return wireContent{Type: "text", Text: part.Text}, nil
	case providers.ContentKindImage:
		if part.Image == nil || len(part.Image.Data) == 0 || part.Image.MediaType == "" {
			return wireContent{}, errors.New("anthropic image input must be embedded with a media type")
		}
		return wireContent{Type: "image", Source: &imageSource{
			Type: "base64", MediaType: part.Image.MediaType,
			Data: base64.StdEncoding.EncodeToString(part.Image.Data),
		}}, nil
	case providers.ContentKindToolCall:
		if part.ToolCall == nil {
			return wireContent{}, errors.New("anthropic tool call is missing")
		}
		return wireContent{
			Type: "tool_use", ID: part.ToolCall.ID, Name: part.ToolCall.Name,
			Input: append(json.RawMessage(nil), part.ToolCall.Arguments...),
		}, nil
	case providers.ContentKindToolResult:
		if part.ToolResult == nil {
			return wireContent{}, errors.New("anthropic tool result is missing")
		}
		var text strings.Builder
		for _, resultPart := range part.ToolResult.Content {
			if resultPart.Kind != providers.ContentKindText {
				return wireContent{}, errors.New("anthropic tool result supports text content only")
			}
			text.WriteString(resultPart.Text)
		}
		return wireContent{
			Type: "tool_result", ToolUseID: part.ToolResult.CallID,
			Content: text.String(), IsError: part.ToolResult.IsError,
		}, nil
	default:
		return wireContent{}, errors.New("anthropic content kind is unsupported")
	}
}

type streamState struct {
	model        providers.ModelIdentity
	usage        providers.Usage
	stop         providers.StopReason
	metadata     providers.RedactedProviderMetadata
	partial      providers.PartialEffectEvidence
	tools        map[int]*toolState
	started      bool
	pendingFinal *providers.FinalResponse
}

type toolState struct {
	id        string
	name      string
	arguments strings.Builder
}

type eventEnvelope struct {
	Type    string `json:"type"`
	Index   int    `json:"index"`
	Message struct {
		ID    string     `json:"id"`
		Model string     `json:"model"`
		Usage *wireUsage `json:"usage"`
	} `json:"message"`
	ContentBlock struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"content_block"`
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		PartialJSON string `json:"partial_json"`
		StopReason  string `json:"stop_reason"`
	} `json:"delta"`
	Usage *wireUsage `json:"usage"`
	Error struct {
		Type string `json:"type"`
	} `json:"error"`
}

type wireUsage struct {
	InputTokens              *int64 `json:"input_tokens"`
	OutputTokens             *int64 `json:"output_tokens"`
	CacheCreationInputTokens *int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     *int64 `json:"cache_read_input_tokens"`
}

func (state *streamState) consume(
	ctx context.Context,
	event providers.SSEEvent,
	stream *modelStream,
) error {
	var envelope eventEnvelope
	if err := json.Unmarshal([]byte(event.Data), &envelope); err != nil {
		return fmt.Errorf("decode anthropic stream event: %w", err)
	}
	if envelope.Type == "" {
		return errors.New("anthropic stream event type is required")
	}
	if event.Event != "" && event.Event != envelope.Type {
		return errors.New("anthropic SSE event name does not match its data type")
	}
	if state.pendingFinal != nil {
		return errors.New("anthropic emitted an event after message_stop")
	}
	switch envelope.Type {
	case "message_start":
		if state.started || envelope.Message.ID == "" || envelope.Message.Model == "" {
			return errors.New("anthropic message_start is invalid or duplicated")
		}
		state.started = true
		state.partial.ProviderAck = true
		state.metadata.ResponseID = envelope.Message.ID
		if envelope.Message.Model != "" {
			state.model.Revision = envelope.Message.Model
		}
		if err := state.applyUsage(envelope.Message.Usage); err != nil {
			return err
		}
		metadata := cloneMetadata(state.metadata)
		return stream.send(ctx, providers.StreamEvent{
			Kind:     providers.StreamEventMetadata,
			Metadata: &metadata,
		})
	case "content_block_start":
		if !state.started {
			return errors.New("anthropic content block started before message_start")
		}
		if envelope.ContentBlock.Type == "tool_use" {
			state.tools[envelope.Index] = &toolState{
				id: envelope.ContentBlock.ID, name: envelope.ContentBlock.Name,
			}
			state.partial.ProviderAck = true
		}
	case "content_block_delta":
		if !state.started {
			return errors.New("anthropic content delta arrived before message_start")
		}
		switch envelope.Delta.Type {
		case "text_delta":
			state.partial.StreamedOutput = true
			return stream.send(ctx, providers.StreamEvent{
				Kind: providers.StreamEventTextDelta, Text: envelope.Delta.Text,
			})
		case "input_json_delta":
			tool := state.tools[envelope.Index]
			if tool == nil {
				return errors.New("anthropic tool delta has no active tool block")
			}
			tool.arguments.WriteString(envelope.Delta.PartialJSON)
			return stream.send(ctx, providers.StreamEvent{
				Kind: providers.StreamEventToolCallDelta,
				ToolCallDelta: &providers.ToolCallDelta{
					Index: envelope.Index, ID: tool.id, Name: tool.name,
					ArgumentsFragment: envelope.Delta.PartialJSON,
				},
			})
		}
	case "content_block_stop":
		if !state.started {
			return errors.New("anthropic content block stopped before message_start")
		}
		if tool := state.tools[envelope.Index]; tool != nil {
			arguments := json.RawMessage(tool.arguments.String())
			if !json.Valid(arguments) {
				return errors.New("anthropic tool arguments are not complete JSON")
			}
			state.partial.ToolCall = true
			delete(state.tools, envelope.Index)
			return stream.send(ctx, providers.StreamEvent{
				Kind: providers.StreamEventToolCall,
				ToolCall: &providers.ToolCall{
					ID: tool.id, Name: tool.name, Arguments: arguments,
				},
			})
		}
	case "message_delta":
		if !state.started {
			return errors.New("anthropic message_delta arrived before message_start")
		}
		state.stop = normalizeStop(envelope.Delta.StopReason)
		if err := state.applyUsage(envelope.Usage); err != nil {
			return err
		}
		if state.usage.Known {
			usage := state.usage
			return stream.send(ctx, providers.StreamEvent{
				Kind: providers.StreamEventUsage, Usage: &usage,
			})
		}
		return nil
	case "message_stop":
		if !state.started {
			return errors.New("anthropic message_stop arrived before message_start")
		}
		if len(state.tools) != 0 {
			return errors.New("anthropic message_stop arrived with an incomplete tool call")
		}
		metadata := cloneMetadata(state.metadata)
		state.pendingFinal = &providers.FinalResponse{
			Identity: state.model, StopReason: state.stop, Usage: state.usage,
			Metadata: metadata, PartialEffect: state.partial,
		}
		return nil
	case "error":
		return classifyAnthropicStreamError(envelope.Error.Type, state.partial)
	case "ping":
		return nil
	default:
		// Anthropic's versioning policy permits new event types.
		return nil
	}
	return nil
}

func (state *streamState) applyUsage(usage *wireUsage) error {
	if usage == nil {
		return nil
	}
	for _, count := range []*int64{
		usage.InputTokens, usage.OutputTokens,
		usage.CacheCreationInputTokens, usage.CacheReadInputTokens,
	} {
		if count != nil && *count < 0 {
			return errors.New("anthropic usage must not be negative")
		}
	}
	state.usage.Known = true
	state.usage.Source = providers.UsageSourceProvider
	if usage.InputTokens != nil {
		state.usage.InputTokens = *usage.InputTokens
	}
	if usage.OutputTokens != nil {
		state.usage.OutputTokens = *usage.OutputTokens
	}
	if usage.CacheCreationInputTokens != nil {
		state.usage.CacheWriteTokens = *usage.CacheCreationInputTokens
	}
	if usage.CacheReadInputTokens != nil {
		state.usage.CachedInputTokens = *usage.CacheReadInputTokens
	}
	return nil
}

func normalizeStop(value string) providers.StopReason {
	switch value {
	case "end_turn", "stop_sequence":
		return providers.StopReasonCompleted
	case "max_tokens", "model_context_window_exceeded":
		return providers.StopReasonMaximumTokens
	case "tool_use", "pause_turn":
		return providers.StopReasonToolCalls
	case "refusal":
		return providers.StopReasonSafety
	case "":
		return providers.StopReasonUnknown
	default:
		return providers.StopReasonUnknown
	}
}

func classifyAnthropicStreamError(
	errorType string,
	partial providers.PartialEffectEvidence,
) error {
	failure := &providers.Failure{
		Operation: "stream", PartialEffect: partial,
	}
	switch errorType {
	case "invalid_request_error":
		failure.Kind, failure.Cause =
			providers.FailureInvalidRequest, providers.ErrInvalidRequest
	case "authentication_error", "permission_error":
		failure.Kind, failure.Cause =
			providers.FailureAuthentication, providers.ErrAuthentication
	case "rate_limit_error":
		failure.Kind, failure.Cause =
			providers.FailureRateLimited, providers.ErrRateLimited
		failure.Retryable = !hasPartialEffect(partial)
	case "overloaded_error", "api_error", "":
		failure.Kind, failure.Cause =
			providers.FailureUnavailable, providers.ErrUnavailable
		failure.Retryable = !hasPartialEffect(partial)
	default:
		failure.Kind, failure.Cause =
			providers.FailureUnavailable, providers.ErrUnavailable
		failure.Retryable = false
	}
	return failure
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
		failure.Kind = providers.FailureCanceled
		failure.Cause = providers.ErrCanceled
		return failure
	}
	if errors.Is(err, context.DeadlineExceeded) {
		failure.Kind = providers.FailureTimeout
		failure.Retryable = !hasPartialEffect(partial)
		failure.Cause = providers.ErrTimeout
		return failure
	}
	var status *providers.HTTPStatusError
	if !errors.As(err, &status) {
		failure.Kind = providers.FailureTransport
		failure.Retryable = !hasPartialEffect(partial)
		failure.Cause = providers.ErrTransport
		return failure
	}
	failure.RetryAfter = status.RetryAfter
	switch status.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		failure.Kind, failure.Cause = providers.FailureAuthentication, providers.ErrAuthentication
	case http.StatusTooManyRequests:
		failure.Kind, failure.Cause = providers.FailureRateLimited, providers.ErrRateLimited
		failure.Retryable = !hasPartialEffect(partial)
	case http.StatusRequestTimeout:
		failure.Kind, failure.Cause = providers.FailureTimeout, providers.ErrTimeout
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

func validConfiguredModel(
	model providers.ModelIdentity,
	adapterName string,
	providerName string,
) bool {
	if model.Provider.Adapter != adapterName ||
		model.Provider.Provider != providerName {
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
