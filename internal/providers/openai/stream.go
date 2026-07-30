package openai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"time"

	"codeflux.dev/codeflux/internal/providers"
)

type streamResult struct {
	event providers.StreamEvent
	err   error
}

type responseStream struct {
	ctx    context.Context
	cancel context.CancelFunc
	now    func() time.Time
	output chan streamResult
	done   chan struct{}

	closeOnce    sync.Once
	partialMu    sync.RWMutex
	sequence     int64
	final        bool
	failed       bool
	partial      providers.PartialEffectEvidence
	metadata     providers.RedactedProviderMetadata
	tools        map[int]*toolState
	emitted      map[string]bool
	pendingFinal *providers.FinalResponse
}

type toolState struct {
	ID        string
	Name      string
	Arguments string
}

type responseEvent struct {
	Type           string          `json:"type"`
	SequenceNumber int64           `json:"sequence_number"`
	Delta          string          `json:"delta"`
	ItemID         string          `json:"item_id"`
	OutputIndex    int             `json:"output_index"`
	Name           string          `json:"name"`
	Arguments      string          `json:"arguments"`
	Item           responseItem    `json:"item"`
	Response       responsePayload `json:"response"`
	Error          *responseError  `json:"error"`
}

type responseItem struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type responsePayload struct {
	ID                string             `json:"id"`
	Model             string             `json:"model"`
	Status            string             `json:"status"`
	ServiceTier       string             `json:"service_tier"`
	SystemFingerprint string             `json:"system_fingerprint"`
	Output            []responseOutput   `json:"output"`
	Usage             responseUsage      `json:"usage"`
	IncompleteDetails *incompleteDetails `json:"incomplete_details"`
	Error             *responseError     `json:"error"`
}

type responseOutput struct {
	Type    string `json:"type"`
	CallID  string `json:"call_id"`
	Name    string `json:"name"`
	Content []struct {
		Type string `json:"type"`
	} `json:"content"`
}

type responseUsage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	TotalTokens  int64 `json:"total_tokens"`
	InputDetails struct {
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"input_tokens_details"`
	OutputDetails struct {
		ReasoningTokens int64 `json:"reasoning_tokens"`
	} `json:"output_tokens_details"`
}

type incompleteDetails struct {
	Reason string `json:"reason"`
}

type responseError struct {
	Code string `json:"code"`
	Type string `json:"type"`
}

func newResponseStream(
	ctx context.Context,
	cancel context.CancelFunc,
	now func() time.Time,
) *responseStream {
	return &responseStream{
		ctx: ctx, cancel: cancel, now: now,
		output: make(chan streamResult, 16), done: make(chan struct{}),
		tools: make(map[int]*toolState), emitted: make(map[string]bool),
	}
}

func (stream *responseStream) Recv(ctx context.Context) (providers.StreamEvent, error) {
	if err := ctx.Err(); err != nil {
		return providers.StreamEvent{}, err
	}
	if err := stream.ctx.Err(); err != nil {
		return providers.StreamEvent{}, classifyFailure(
			"receive OpenAI response",
			err,
			stream.snapshotPartial(),
		)
	}
	select {
	case <-ctx.Done():
		return providers.StreamEvent{}, ctx.Err()
	case <-stream.ctx.Done():
		return providers.StreamEvent{}, classifyFailure(
			"receive OpenAI response",
			stream.ctx.Err(),
			stream.snapshotPartial(),
		)
	case result, ok := <-stream.output:
		if err := ctx.Err(); err != nil {
			return providers.StreamEvent{}, err
		}
		if err := stream.ctx.Err(); err != nil {
			return providers.StreamEvent{}, classifyFailure(
				"receive OpenAI response",
				err,
				stream.snapshotPartial(),
			)
		}
		if !ok {
			return providers.StreamEvent{}, io.EOF
		}
		return result.event, result.err
	}
}

func (stream *responseStream) Close() error {
	stream.closeOnce.Do(stream.cancel)
	return nil
}

func (stream *responseStream) finish() {
	close(stream.output)
	close(stream.done)
}

func (stream *responseStream) fail(err error) {
	if err == nil || stream.failed || stream.final {
		return
	}
	stream.failed = true
	select {
	case stream.output <- streamResult{err: err}:
	case <-stream.ctx.Done():
	}
}

func (stream *responseStream) emit(event providers.StreamEvent) error {
	if stream.final || stream.failed {
		return errors.New("OpenAI response emitted after terminal event")
	}
	stream.sequence++
	event.Sequence = stream.sequence
	event.ObservedAt = stream.now().UTC()
	stream.updatePartial(func(partial *providers.PartialEffectEvidence) {
		partial.LastSequence = stream.sequence
	})
	select {
	case stream.output <- streamResult{event: event}:
		return nil
	case <-stream.ctx.Done():
		return stream.ctx.Err()
	}
}

func (stream *responseStream) complete() error {
	if stream.pendingFinal == nil {
		return errors.New("OpenAI response has no pending final event")
	}
	final := *stream.pendingFinal
	final.Metadata = *cloneMetadata(stream.pendingFinal.Metadata)
	if err := stream.emit(providers.StreamEvent{
		Kind: providers.StreamEventFinal, Final: &final,
	}); err != nil {
		return err
	}
	stream.final = true
	return nil
}

func (stream *responseStream) consume(
	sse providers.SSEEvent,
	request providers.RequestIdentity,
) error {
	if sse.Data == "[DONE]" {
		return nil
	}
	var event responseEvent
	if err := json.Unmarshal([]byte(sse.Data), &event); err != nil {
		return errors.New("decode OpenAI response event")
	}
	if event.Type == "" {
		event.Type = sse.Event
	}
	switch event.Type {
	case "response.created", "response.in_progress":
		stream.updatePartial(func(partial *providers.PartialEffectEvidence) {
			partial.ProviderAck = true
		})
		stream.updateMetadata(request, event.Response)
		return stream.emit(providers.StreamEvent{
			Kind: providers.StreamEventMetadata, Metadata: cloneMetadata(stream.metadata),
		})
	case "response.output_text.delta":
		if event.Delta == "" {
			return nil
		}
		stream.updatePartial(func(partial *providers.PartialEffectEvidence) {
			partial.StreamedOutput = true
		})
		return stream.emit(providers.StreamEvent{
			Kind: providers.StreamEventTextDelta, Text: event.Delta,
		})
	case "response.output_item.added":
		if event.Item.Type != "function_call" {
			return nil
		}
		stream.updatePartial(func(partial *providers.PartialEffectEvidence) {
			partial.ToolCall = true
		})
		state := &toolState{
			ID: event.Item.CallID, Name: event.Item.Name,
			Arguments: event.Item.Arguments,
		}
		if state.ID == "" {
			state.ID = event.Item.ID
		}
		stream.tools[event.OutputIndex] = state
		return nil
	case "response.function_call_arguments.delta":
		stream.updatePartial(func(partial *providers.PartialEffectEvidence) {
			partial.ToolCall = true
		})
		state := stream.tool(event.OutputIndex)
		if event.ItemID != "" && state.ID == "" {
			state.ID = event.ItemID
		}
		state.Arguments += event.Delta
		return stream.emit(providers.StreamEvent{
			Kind: providers.StreamEventToolCallDelta,
			ToolCallDelta: &providers.ToolCallDelta{
				Index: event.OutputIndex, ID: state.ID, Name: state.Name,
				ArgumentsFragment: event.Delta,
			},
		})
	case "response.function_call_arguments.done":
		stream.updatePartial(func(partial *providers.PartialEffectEvidence) {
			partial.ToolCall = true
		})
		state := stream.tool(event.OutputIndex)
		if event.ItemID != "" && state.ID == "" {
			state.ID = event.ItemID
		}
		if event.Name != "" {
			state.Name = event.Name
		}
		if event.Arguments != "" {
			state.Arguments = event.Arguments
		}
		return stream.emitTool(state)
	case "response.output_item.done":
		if event.Item.Type != "function_call" {
			return nil
		}
		state := stream.tool(event.OutputIndex)
		if event.Item.CallID != "" {
			state.ID = event.Item.CallID
		} else if state.ID == "" {
			state.ID = event.Item.ID
		}
		if event.Item.Name != "" {
			state.Name = event.Item.Name
		}
		if event.Item.Arguments != "" {
			state.Arguments = event.Item.Arguments
		}
		return stream.emitTool(state)
	case "response.completed", "response.incomplete":
		stream.updatePartial(func(partial *providers.PartialEffectEvidence) {
			partial.ProviderAck = true
		})
		stream.updateMetadata(request, event.Response)
		usage := normalizeUsage(event.Response.Usage)
		if err := stream.emit(providers.StreamEvent{
			Kind: providers.StreamEventUsage, Usage: &usage,
		}); err != nil {
			return err
		}
		final := &providers.FinalResponse{
			Identity: providers.ModelIdentity{
				Provider: request.Provider, Model: event.Response.Model,
				Revision: request.Model.Revision,
			},
			StopReason:    stopReason(event.Response),
			Usage:         usage,
			Metadata:      *cloneMetadata(stream.metadata),
			PartialEffect: stream.snapshotPartial(),
		}
		if final.Identity.Model == "" {
			final.Identity = request.Model
		}
		if stream.pendingFinal != nil {
			return errors.New("OpenAI returned more than one final response")
		}
		stream.pendingFinal = final
		return nil
	case "response.failed", "error":
		kind := providers.FailureUnavailable
		code := ""
		if event.Error != nil {
			code = event.Error.Code
		}
		if event.Response.Error != nil && code == "" {
			code = event.Response.Error.Code
		}
		if code == "content_policy_violation" {
			kind = providers.FailureSafety
		}
		cause := providers.ErrUnavailable
		if kind == providers.FailureSafety {
			cause = providers.ErrSafety
		}
		return &providers.Failure{
			Kind: kind, Operation: "OpenAI response",
			Retryable: kind == providers.FailureUnavailable &&
				!hasPartialEffect(stream.snapshotPartial()),
			PartialEffect: stream.snapshotPartial(),
			Cause:         cause,
		}
	default:
		// Responses adds event variants over time. Unknown descriptive events
		// are ignored; terminal status is still required before EOF.
		return nil
	}
}

func (stream *responseStream) snapshotPartial() providers.PartialEffectEvidence {
	stream.partialMu.RLock()
	defer stream.partialMu.RUnlock()
	return stream.partial
}

func (stream *responseStream) updatePartial(
	update func(*providers.PartialEffectEvidence),
) {
	stream.partialMu.Lock()
	update(&stream.partial)
	stream.partialMu.Unlock()
}

func (stream *responseStream) tool(index int) *toolState {
	state := stream.tools[index]
	if state == nil {
		state = &toolState{}
		stream.tools[index] = state
	}
	return state
}

func (stream *responseStream) emitTool(state *toolState) error {
	if state == nil || state.ID == "" || state.Name == "" ||
		!json.Valid([]byte(state.Arguments)) {
		return errors.New("OpenAI returned an invalid function call")
	}
	if stream.emitted[state.ID] {
		return nil
	}
	stream.emitted[state.ID] = true
	arguments := append(json.RawMessage(nil), []byte(state.Arguments)...)
	return stream.emit(providers.StreamEvent{
		Kind: providers.StreamEventToolCall,
		ToolCall: &providers.ToolCall{
			ID: state.ID, Name: state.Name, Arguments: arguments,
		},
	})
}

func (stream *responseStream) updateMetadata(
	request providers.RequestIdentity,
	response responsePayload,
) {
	stream.metadata = providers.RedactedProviderMetadata{
		RequestID:   request.ModelRequestID.String(),
		ResponseID:  response.ID,
		Fingerprint: response.SystemFingerprint,
		Fields:      make(map[string]string),
	}
	if response.Status != "" {
		stream.metadata.Fields["status"] = response.Status
	}
	if response.ServiceTier != "" {
		stream.metadata.Fields["service_tier"] = response.ServiceTier
	}
}

func normalizeUsage(usage responseUsage) providers.Usage {
	known := usage.InputTokens != 0 || usage.OutputTokens != 0 ||
		usage.TotalTokens != 0 || usage.InputDetails.CachedTokens != 0 ||
		usage.OutputDetails.ReasoningTokens != 0
	source := providers.UsageSourceUnknown
	if known {
		source = providers.UsageSourceProvider
	}
	return providers.Usage{
		Known: known, Source: source,
		InputTokens:       usage.InputTokens,
		CachedInputTokens: usage.InputDetails.CachedTokens,
		OutputTokens:      usage.OutputTokens,
		ReasoningTokens:   usage.OutputDetails.ReasoningTokens,
	}
}

func stopReason(response responsePayload) providers.StopReason {
	if response.Status == "incomplete" && response.IncompleteDetails != nil {
		switch response.IncompleteDetails.Reason {
		case "max_output_tokens":
			return providers.StopReasonMaximumTokens
		case "content_filter":
			return providers.StopReasonSafety
		default:
			return providers.StopReasonUnknown
		}
	}
	for _, output := range response.Output {
		if output.Type == "function_call" {
			return providers.StopReasonToolCalls
		}
		for _, content := range output.Content {
			if content.Type == "refusal" {
				return providers.StopReasonSafety
			}
		}
	}
	if response.Status == "completed" {
		return providers.StopReasonCompleted
	}
	if response.Status == "failed" {
		return providers.StopReasonError
	}
	return providers.StopReasonUnknown
}

func cloneMetadata(
	value providers.RedactedProviderMetadata,
) *providers.RedactedProviderMetadata {
	copy := value
	copy.Fields = make(map[string]string, len(value.Fields))
	for key, field := range value.Fields {
		copy.Fields[key] = field
	}
	return &copy
}

var _ providers.ModelStream = (*responseStream)(nil)
