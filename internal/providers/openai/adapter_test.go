package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/providers"
)

func TestAdapterListsConfiguredCapabilitiesWithoutInferringOtherModels(t *testing.T) {
	adapter, server, _ := newTestAdapter(t, func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/models" {
			http.NotFound(writer, request)
			return
		}
		assertAuthorization(t, request)
		writeJSON(t, writer, map[string]any{
			"data": []map[string]any{
				{"id": "gpt-test"},
				{"id": "gpt-unknown"},
			},
		})
	})
	defer server.Close()

	models, err := adapter.ListModels(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 {
		t.Fatalf("model count = %d, want 2", len(models))
	}
	if !models[0].Capabilities.Tools ||
		models[0].Identity.Revision != "gpt-test-2026-07-30" {
		t.Fatalf("configured model = %#v", models[0])
	}
	if models[1].Capabilities.Tools ||
		models[1].Capabilities.ContextTokens != 0 {
		t.Fatalf("unproven capabilities were inferred: %#v", models[1].Capabilities)
	}
	if _, err := adapter.Capabilities(t.Context(), models[1].Identity); err == nil {
		t.Fatal("capabilities accepted an unconfigured model")
	} else {
		var failure *providers.Failure
		if !errors.As(err, &failure) ||
			failure.Kind != providers.FailureInvalidRequest {
			t.Fatalf("capability error = %v", err)
		}
	}
}

func TestAdapterStreamsResponsesTextToolsUsageAndSafeMetadata(t *testing.T) {
	var recorded map[string]any
	adapter, server, providerID := newTestAdapter(
		t,
		func(writer http.ResponseWriter, request *http.Request) {
			if request.URL.Path != "/v1/responses" {
				http.NotFound(writer, request)
				return
			}
			assertAuthorization(t, request)
			if request.Header.Get("Idempotency-Key") != "idem-1" {
				t.Errorf("idempotency header = %q", request.Header.Get("Idempotency-Key"))
			}
			if err := json.NewDecoder(request.Body).Decode(&recorded); err != nil {
				t.Errorf("decode request: %v", err)
				http.Error(writer, "bad request", http.StatusBadRequest)
				return
			}
			writer.Header().Set("Content-Type", "text/event-stream")
			writer.Header().Set("X-Request-ID", "req_header_123")
			events := []map[string]any{
				{
					"type": "response.created",
					"response": map[string]any{
						"id": "resp_123", "model": "gpt-test",
						"status": "in_progress", "service_tier": "default",
						"system_fingerprint": "fp_safe",
					},
				},
				{"type": "response.output_text.delta", "delta": "hello"},
				{
					"type": "response.output_item.added", "output_index": 0,
					"item": map[string]any{
						"type": "function_call", "id": "item_1",
						"call_id": "call_1", "name": "lookup",
					},
				},
				{
					"type":         "response.function_call_arguments.delta",
					"output_index": 0, "item_id": "item_1", "delta": `{"city":`,
				},
				{
					"type":         "response.function_call_arguments.done",
					"output_index": 0, "item_id": "item_1", "name": "lookup",
					"arguments": `{"city":"Paris"}`,
				},
				{
					"type": "response.completed",
					"response": map[string]any{
						"id": "resp_123", "model": "gpt-test", "status": "completed",
						"service_tier": "default", "system_fingerprint": "fp_safe",
						"output": []map[string]any{
							{"type": "function_call", "call_id": "call_1", "name": "lookup"},
						},
						"usage": map[string]any{
							"input_tokens": 20, "output_tokens": 7, "total_tokens": 27,
							"input_tokens_details":  map[string]any{"cached_tokens": 3},
							"output_tokens_details": map[string]any{"reasoning_tokens": 2},
						},
					},
				},
			}
			for _, event := range events {
				writeSSE(t, writer, event)
			}
		},
	)
	defer server.Close()

	requestID, err := domain.NewModelRequestID()
	if err != nil {
		t.Fatal(err)
	}
	identity := providers.RequestIdentity{
		ModelRequestID: requestID,
		Provider:       adapter.ProviderIdentity(),
		Model: providers.ModelIdentity{
			Provider: adapter.ProviderIdentity(),
			Model:    "gpt-test", Revision: "gpt-test-2026-07-30",
		},
		Idempotency: providers.RequestIdempotency{
			ProviderSupported: true, Key: "idem-1", ProviderScope: "project",
		},
		IdempotencyKey: "idem-1",
		RequestHash:    "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	stream, err := adapter.Stream(t.Context(), providers.ModelRequest{
		Identity: identity,
		Messages: []providers.Message{
			{
				Role: providers.MessageRoleUser,
				Content: []providers.ContentPart{
					{Kind: providers.ContentKindText, Text: "hello"},
				},
			},
			{
				Role: providers.MessageRoleAssistant,
				Content: []providers.ContentPart{
					{
						Kind: providers.ContentKindToolCall,
						ToolCall: &providers.ToolCall{
							ID: "prior_call", Name: "lookup",
							Arguments: json.RawMessage(`{"city":"Rome"}`),
						},
					},
				},
			},
			{
				Role: providers.MessageRoleTool,
				Content: []providers.ContentPart{
					{
						Kind: providers.ContentKindToolResult,
						ToolResult: &providers.ToolResult{
							CallID: "prior_call",
							Content: []providers.ContentPart{
								{Kind: providers.ContentKindText, Text: "sunny"},
							},
						},
					},
				},
			},
		},
		Tools: []providers.ToolDeclaration{
			{
				Name: "lookup", Description: "look up weather",
				InputSchema: json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}}}`),
			},
		},
		StructuredOutput: &providers.StructuredOutputRequirement{
			Name: "answer", Strict: true,
			Schema: json.RawMessage(`{"type":"object","properties":{"answer":{"type":"string"}}}`),
		},
		MaximumTokens: 128,
		Idempotency: providers.RequestIdempotency{
			ProviderSupported: true, Key: "idem-1", ProviderScope: "project",
		},
		Deadline: time.Now().Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	var events []providers.StreamEvent
	for {
		event, receiveErr := stream.Recv(t.Context())
		if errors.Is(receiveErr, io.EOF) {
			break
		}
		if receiveErr != nil {
			t.Fatal(receiveErr)
		}
		events = append(events, event)
	}
	if len(events) != 6 {
		t.Fatalf("event count = %d, want 6: %#v", len(events), events)
	}
	for index, event := range events {
		if event.Sequence != int64(index+1) {
			t.Fatalf("event %d sequence = %d", index, event.Sequence)
		}
	}
	if events[1].Kind != providers.StreamEventTextDelta ||
		events[1].Text != "hello" {
		t.Fatalf("text event = %#v", events[1])
	}
	if events[3].ToolCall == nil ||
		events[3].ToolCall.ID != "call_1" ||
		string(events[3].ToolCall.Arguments) != `{"city":"Paris"}` {
		t.Fatalf("tool event = %#v", events[3])
	}
	final := events[len(events)-1].Final
	if final == nil || final.StopReason != providers.StopReasonToolCalls ||
		!final.Usage.Known || final.Usage.InputTokens != 20 ||
		final.Usage.CachedInputTokens != 3 ||
		final.Usage.ReasoningTokens != 2 ||
		final.Metadata.ResponseID != "resp_123" ||
		final.Metadata.Fingerprint != "fp_safe" ||
		!final.PartialEffect.ToolCall || !final.PartialEffect.StreamedOutput {
		t.Fatalf("final response = %#v", final)
	}
	if final.Metadata.RequestID != requestID.String() ||
		final.Metadata.Fields["service_tier"] != "default" ||
		final.Metadata.Fields["openai_request_id"] != "req_header_123" {
		t.Fatalf("safe metadata = %#v", final.Metadata)
	}
	if recorded["store"] != false || recorded["stream"] != true ||
		recorded["model"] != "gpt-test" {
		t.Fatalf("request policy = %#v", recorded)
	}
	if _, ok := recorded["text"].(map[string]any); !ok {
		t.Fatalf("structured output missing: %#v", recorded["text"])
	}
	input, ok := recorded["input"].([]any)
	if !ok || !containsItemType(input, "function_call_output") {
		t.Fatalf("tool result was not encoded: %#v", recorded["input"])
	}
	if providerID.IsZero() {
		t.Fatal("test provider ID is zero")
	}
}

func TestAdapterCancellationStopsDeliveryAndIsIdempotent(t *testing.T) {
	started := make(chan struct{})
	canceled := make(chan struct{})
	var once sync.Once
	adapter, server, _ := newTestAdapter(
		t,
		func(writer http.ResponseWriter, request *http.Request) {
			if request.URL.Path != "/v1/responses" {
				http.NotFound(writer, request)
				return
			}
			writer.Header().Set("Content-Type", "text/event-stream")
			writeSSE(t, writer, map[string]any{
				"type":     "response.created",
				"response": map[string]any{"id": "resp_cancel", "status": "in_progress"},
			})
			if flusher, ok := writer.(http.Flusher); ok {
				flusher.Flush()
			}
			close(started)
			<-request.Context().Done()
			once.Do(func() { close(canceled) })
		},
	)
	defer server.Close()

	request := validRequest(t, adapter)
	stream, err := adapter.Stream(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not start")
	}
	if err := adapter.Cancel(t.Context(), request.Identity.ModelRequestID); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Cancel(t.Context(), request.Identity.ModelRequestID); err != nil {
		t.Fatalf("idempotent cancel: %v", err)
	}
	if _, err := stream.Recv(t.Context()); err == nil {
		t.Fatal("canceled stream delivered an event")
	} else {
		var failure *providers.Failure
		if !errors.As(err, &failure) ||
			failure.Kind != providers.FailureCanceled {
			t.Fatalf("cancel error = %v", err)
		}
	}
	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("HTTP request was not canceled")
	}
}

func TestResponseStreamCancellationSuppressesBufferedLateEvent(t *testing.T) {
	owner, cancel := context.WithCancel(context.Background())
	stream := newResponseStream(owner, cancel, time.Now)
	stream.output <- streamResult{event: providers.StreamEvent{
		Sequence: 1,
		Kind:     providers.StreamEventTextDelta,
		Text:     "must not be delivered",
	}}
	cancel()
	if _, err := stream.Recv(t.Context()); err == nil {
		t.Fatal("canceled stream delivered a buffered event")
	} else {
		var failure *providers.Failure
		if !errors.As(err, &failure) ||
			failure.Kind != providers.FailureCanceled {
			t.Fatalf("buffered cancellation error = %v", err)
		}
	}
}

func TestAdapterDeadlineIsTimeoutAndStopsDelivery(t *testing.T) {
	canceled := make(chan struct{})
	adapter, server, _ := newTestAdapter(
		t,
		func(writer http.ResponseWriter, request *http.Request) {
			writer.Header().Set("Content-Type", "text/event-stream")
			writer.WriteHeader(http.StatusOK)
			if flusher, ok := writer.(http.Flusher); ok {
				flusher.Flush()
			}
			<-request.Context().Done()
			close(canceled)
		},
	)
	defer server.Close()
	request := validRequest(t, adapter)
	request.Deadline = time.Now().Add(50 * time.Millisecond)
	stream, err := adapter.Stream(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Recv(t.Context()); err == nil {
		t.Fatal("expired request deadline delivered an event")
	} else {
		var failure *providers.Failure
		if !errors.As(err, &failure) ||
			failure.Kind != providers.FailureTimeout ||
			!errors.Is(err, providers.ErrTimeout) {
			t.Fatalf("deadline error = %v", err)
		}
	}
	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("deadline did not cancel the HTTP request")
	}
}

func TestAdapterRejectsUnapprovedRemoteImageBeforeCredentialOrNetwork(t *testing.T) {
	var requests int
	adapter, server, _ := newTestAdapter(
		t,
		func(writer http.ResponseWriter, request *http.Request) {
			requests++
			http.Error(writer, "unexpected", http.StatusInternalServerError)
		},
	)
	defer server.Close()
	request := validRequest(t, adapter)
	request.Messages[0].Content = []providers.ContentPart{
		{
			Kind: providers.ContentKindImage,
			Image: &providers.ImageInput{
				URL: "https://private.example.test/image.png",
			},
		},
	}
	if _, err := adapter.Stream(t.Context(), request); err == nil {
		t.Fatal("unapproved remote image URL was accepted")
	} else {
		var failure *providers.Failure
		if !errors.As(err, &failure) ||
			failure.Kind != providers.FailureInvalidRequest {
			t.Fatalf("remote image error = %v", err)
		}
	}
	if requests != 0 {
		t.Fatalf("unapproved image caused %d provider requests", requests)
	}
}

func TestAdapterRejectsInvalidSharedRequestIdentityBeforeNetwork(t *testing.T) {
	var requests int
	adapter, server, _ := newTestAdapter(
		t,
		func(writer http.ResponseWriter, request *http.Request) {
			requests++
			http.Error(writer, "unexpected", http.StatusInternalServerError)
		},
	)
	defer server.Close()
	tests := []struct {
		name   string
		change func(*providers.ModelRequest)
	}{
		{
			name: "request hash",
			change: func(request *providers.ModelRequest) {
				request.Identity.RequestHash = "not-a-sha256"
			},
		},
		{
			name: "idempotency mismatch",
			change: func(request *providers.ModelRequest) {
				request.Idempotency = providers.RequestIdempotency{
					ProviderSupported: true,
					Key:               "different-key",
					ProviderScope:     "project",
				}
			},
		},
		{
			name: "missing deadline",
			change: func(request *providers.ModelRequest) {
				request.Deadline = time.Time{}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := validRequest(t, adapter)
			test.change(&request)
			if _, err := adapter.Stream(t.Context(), request); err == nil {
				t.Fatal("invalid normalized request was accepted")
			} else {
				var failure *providers.Failure
				if !errors.As(err, &failure) ||
					failure.Kind != providers.FailureInvalidRequest {
					t.Fatalf("invalid request error = %v", err)
				}
			}
		})
	}
	if requests != 0 {
		t.Fatalf("invalid requests caused %d provider requests", requests)
	}
}

func TestAdapterClassifiesHTTPFailuresWithoutExposingBodies(t *testing.T) {
	tests := []struct {
		status    int
		body      string
		kind      providers.FailureKind
		retryable bool
	}{
		{http.StatusBadRequest, `{"error":{"code":"bad","message":"secret body"}}`, providers.FailureInvalidRequest, false},
		{http.StatusUnauthorized, `{"error":{"code":"bad_key"}}`, providers.FailureAuthentication, false},
		{http.StatusTooManyRequests, `{"error":{"code":"rate_limit"}}`, providers.FailureRateLimited, true},
		{http.StatusInternalServerError, `{"error":{"code":"server_error"}}`, providers.FailureUnavailable, true},
		{http.StatusBadRequest, `{"error":{"code":"content_policy_violation"}}`, providers.FailureSafety, false},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("status-%d-%s", test.status, test.kind), func(t *testing.T) {
			adapter, server, _ := newTestAdapter(t, func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Retry-After", "2")
				writer.WriteHeader(test.status)
				_, _ = writer.Write([]byte(test.body))
			})
			defer server.Close()
			_, err := adapter.ListModels(t.Context())
			var failure *providers.Failure
			if !errors.As(err, &failure) || failure.Kind != test.kind ||
				failure.Retryable != test.retryable {
				t.Fatalf("failure = %#v, err = %v", failure, err)
			}
			if !errors.Is(err, failureSentinel(test.kind)) {
				t.Fatalf("failure %v does not expose its stable sentinel", err)
			}
			if test.kind == providers.FailureRateLimited &&
				failure.RetryAfter != 2*time.Second {
				t.Fatalf("retry after = %s", failure.RetryAfter)
			}
			if err != nil && contains(err.Error(), "secret body") {
				t.Fatalf("provider body leaked through error: %v", err)
			}
		})
	}
}

func newTestAdapter(
	t *testing.T,
	handler http.HandlerFunc,
) (*Adapter, *httptest.Server, domain.ProviderID) {
	t.Helper()
	server := httptest.NewServer(handler)
	providerID, err := domain.NewProviderID()
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	transport, err := providers.NewHTTPTransport(providers.TransportOptions{
		HTTPClient: server.Client(), RequestTimeout: 5 * time.Second,
	})
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	adapter, err := New(Config{
		ProviderID: providerID, Endpoint: server.URL + "/v1",
		Model: "gpt-test", ModelRevision: "gpt-test-2026-07-30",
		Capabilities: providers.ModelCapabilities{
			Tools: true, StructuredOutput: true, ContextTokens: 128_000,
			MaximumOutputTokens: 16_384, ImageInput: true, Streaming: true,
			PromptCaching: true, ReasoningControls: []string{"low", "high"},
		},
		Credentials: staticCredential{
			providerID: providerID, value: []byte("test-only-token"),
		},
		Transport: transport,
	})
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	return adapter, server, providerID
}

type staticCredential struct {
	providerID domain.ProviderID
	value      []byte
}

func (credential staticCredential) Use(
	ctx context.Context,
	providerID domain.ProviderID,
	operation func([]byte) error,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if providerID != credential.providerID {
		return providers.ErrAuthentication
	}
	copy := append([]byte(nil), credential.value...)
	defer func() {
		for index := range copy {
			copy[index] = 0
		}
	}()
	return operation(copy)
}

func validRequest(t *testing.T, adapter *Adapter) providers.ModelRequest {
	t.Helper()
	requestID, err := domain.NewModelRequestID()
	if err != nil {
		t.Fatal(err)
	}
	return providers.ModelRequest{
		Identity: providers.RequestIdentity{
			ModelRequestID: requestID,
			Provider:       adapter.ProviderIdentity(),
			Model: providers.ModelIdentity{
				Provider: adapter.ProviderIdentity(),
				Model:    "gpt-test", Revision: "gpt-test-2026-07-30",
			},
			IdempotencyKey: "logical-only-1",
			RequestHash:    "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		},
		Messages: []providers.Message{
			{
				Role: providers.MessageRoleUser,
				Content: []providers.ContentPart{
					{Kind: providers.ContentKindText, Text: "hello"},
				},
			},
		},
		MaximumTokens: 32,
		Deadline:      time.Now().Add(time.Minute),
	}
}

func assertAuthorization(t *testing.T, request *http.Request) {
	t.Helper()
	if request.Header.Get("Authorization") != "Bearer test-only-token" {
		t.Errorf("authorization header missing or incorrect")
	}
}

func writeJSON(t *testing.T, writer http.ResponseWriter, value any) {
	t.Helper()
	writer.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(writer).Encode(value); err != nil {
		t.Errorf("write JSON: %v", err)
	}
}

func writeSSE(t *testing.T, writer http.ResponseWriter, value any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Errorf("marshal SSE: %v", err)
		return
	}
	if _, err := fmt.Fprintf(writer, "data: %s\n\n", encoded); err != nil {
		t.Errorf("write SSE: %v", err)
	}
}

func containsItemType(items []any, target string) bool {
	for _, item := range items {
		object, ok := item.(map[string]any)
		if ok && object["type"] == target {
			return true
		}
	}
	return false
}

func contains(value, fragment string) bool {
	for index := 0; index+len(fragment) <= len(value); index++ {
		if value[index:index+len(fragment)] == fragment {
			return true
		}
	}
	return false
}
