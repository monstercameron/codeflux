package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/providers"
)

func TestAdapterStreamsNormalizedTextToolsUsageAndMetadata(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		requests.Add(1)
		if request.URL.Path != "/v1/messages" ||
			request.Header.Get("x-api-key") != "anthropic-fixture" ||
			request.Header.Get("anthropic-version") != apiVersion {
			t.Errorf("unexpected request: %s headers=%v", request.URL.Path, request.Header)
		}
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["model"] != "claude-fixture" || body["stream"] != true ||
			body["output_config"] == nil || body["tools"] == nil {
			t.Errorf("request body = %#v", body)
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		writer.Header().Set("Request-ID", "req_anthropic_fixture")
		events := []string{
			`{"type":"message_start","message":{"id":"msg_fixture","model":"claude-fixture-20260730","usage":{"input_tokens":11,"output_tokens":1}}}`,
			`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"tool_1","name":"lookup"}}`,
			`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"id\":"}}`,
			`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"7}"}}`,
			`{"type":"content_block_stop","index":1}`,
			`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":9}}`,
			`{"type":"message_stop"}`,
		}
		for _, event := range events {
			_, _ = fmt.Fprintf(writer, "data: %s\n\n", event)
		}
	}))
	defer server.Close()
	adapter := newTestAdapter(t, server)
	request := testRequest(t)
	stream, err := adapter.Stream(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	var (
		text       string
		tool       *providers.ToolCall
		toolDelta  *providers.ToolCallDelta
		final      *providers.FinalResponse
		eventKinds []providers.StreamEventKind
	)
	for {
		event, recvErr := stream.Recv(t.Context())
		if errors.Is(recvErr, io.EOF) {
			break
		}
		if recvErr != nil {
			t.Fatal(recvErr)
		}
		eventKinds = append(eventKinds, event.Kind)
		text += event.Text
		if event.ToolCall != nil {
			tool = event.ToolCall
		}
		if event.ToolCallDelta != nil {
			toolDelta = event.ToolCallDelta
			if _, err := json.Marshal(event); err != nil {
				t.Fatalf("tool delta event is not serializable: %v", err)
			}
		}
		if event.Final != nil {
			final = event.Final
		}
	}
	if requests.Load() != 1 || text != "hello" || tool == nil ||
		tool.ID != "tool_1" || tool.Name != "lookup" ||
		string(tool.Arguments) != `{"id":7}` {
		t.Fatalf("text=%q tool=%#v kinds=%v", text, tool, eventKinds)
	}
	if final == nil || final.StopReason != providers.StopReasonToolCalls ||
		final.Usage.InputTokens != 11 || final.Usage.OutputTokens != 9 ||
		final.Metadata.ResponseID != "msg_fixture" ||
		final.Metadata.RequestID != request.Identity.ModelRequestID.String() ||
		final.Metadata.Fields["anthropic_request_id"] != "req_anthropic_fixture" ||
		final.Identity.Revision != "claude-fixture-20260730" ||
		!final.PartialEffect.StreamedOutput || !final.PartialEffect.ToolCall {
		t.Fatalf("final = %#v", final)
	}
	if toolDelta == nil {
		t.Fatal("missing normalized tool delta")
	}
	if toolDelta.ArgumentsFragment != "7}" &&
		toolDelta.ArgumentsFragment != `{"id":` {
		t.Fatalf("tool delta fragment = %q", toolDelta.ArgumentsFragment)
	}
}

func TestAdapterCancellationStopsDelivery(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		writer.Header().Set("Content-Type", "text/event-stream")
		flusher := writer.(http.Flusher)
		_, _ = fmt.Fprint(writer,
			"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_cancel\",\"model\":\"claude-fixture\",\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n",
			"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"first\"}}\n\n",
			"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"late-buffered\"}}\n\n",
		)
		flusher.Flush()
		close(started)
		<-request.Context().Done()
	}))
	defer server.Close()
	adapter := newTestAdapter(t, server)
	request := testRequest(t)
	stream, err := adapter.Stream(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("anthropic stream did not start")
	}
	first, err := stream.Recv(t.Context())
	if err != nil || first.Kind != providers.StreamEventMetadata {
		t.Fatalf("metadata event = %#v, %v", first, err)
	}
	first, err = stream.Recv(t.Context())
	if err != nil || first.Text != "first" {
		t.Fatalf("text event = %#v, %v", first, err)
	}
	if err := adapter.Cancel(t.Context(), request.Identity.ModelRequestID); err != nil {
		t.Fatal(err)
	}
	_, err = stream.Recv(t.Context())
	var failure *providers.Failure
	if !errors.As(err, &failure) || failure.Kind != providers.FailureCanceled {
		t.Fatalf("cancellation error = %T %v", err, err)
	}
}

func TestAdapterRejectsIdentityMismatchAndMissingTerminal(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		_ *http.Request,
	) {
		requests.Add(1)
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(writer,
			"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_incomplete\",\"model\":\"claude-fixture\"}}\n\n",
		)
	}))
	defer server.Close()
	adapter := newTestAdapter(t, server)
	mismatch := testRequest(t)
	mismatch.Identity.Model.Revision = "different-revision"
	if _, err := adapter.Stream(t.Context(), mismatch); err == nil {
		t.Fatal("mismatched model revision was accepted")
	}
	if requests.Load() != 0 {
		t.Fatalf("identity mismatch caused %d network requests", requests.Load())
	}
	stream, err := adapter.Stream(t.Context(), testRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	for {
		_, recvErr := stream.Recv(t.Context())
		if recvErr == nil {
			continue
		}
		if errors.Is(recvErr, io.EOF) {
			t.Fatal("missing message_stop was reported as clean EOF")
		}
		var failure *providers.Failure
		if !errors.As(recvErr, &failure) ||
			failure.Kind != providers.FailureTransport ||
			failure.Retryable {
			t.Fatalf("missing-terminal failure = %T %v", recvErr, recvErr)
		}
		break
	}
}

func TestAdapterLeavesUsageUnknownWhenProviderOmitsIt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		_ *http.Request,
	) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(writer,
			"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_unknown\",\"model\":\"claude-fixture\"}}\n\n",
			"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n",
			"data: {\"type\":\"message_stop\"}\n\n",
		)
	}))
	defer server.Close()
	adapter := newTestAdapter(t, server)
	stream, err := adapter.Stream(t.Context(), testRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	var final *providers.FinalResponse
	for {
		event, recvErr := stream.Recv(t.Context())
		if errors.Is(recvErr, io.EOF) {
			break
		}
		if recvErr != nil {
			t.Fatal(recvErr)
		}
		if event.Final != nil {
			final = event.Final
		}
	}
	if final == nil || final.Usage.Known ||
		final.Usage.Source != providers.UsageSourceUnknown {
		t.Fatalf("unknown usage final = %#v", final)
	}
}

func TestAdapterHonorsRequestDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		<-request.Context().Done()
	}))
	defer server.Close()
	adapter := newTestAdapter(t, server)
	request := testRequest(t)
	request.Deadline = time.Now().Add(-time.Second)
	stream, err := adapter.Stream(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	_, err = stream.Recv(t.Context())
	var failure *providers.Failure
	if !errors.As(err, &failure) ||
		failure.Kind != providers.FailureTimeout ||
		failure.Retryable {
		t.Fatalf("deadline failure = %T %v", err, err)
	}
}

func TestModelStreamDoesNotDropTerminalFailureWhenBufferIsFull(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	stream := newModelStream(ctx, cancel)
	for index := 0; index < cap(stream.events); index++ {
		stream.events <- eventResult{event: providers.StreamEvent{
			Kind: providers.StreamEventTextDelta,
		}}
	}
	entered := make(chan struct{})
	delivered := make(chan struct{})
	terminal := errors.New("terminal")
	go func() {
		close(entered)
		stream.fail(terminal)
		close(delivered)
	}()
	<-entered
	select {
	case <-delivered:
		t.Fatal("terminal failure was reported delivered into a full buffer")
	case <-time.After(20 * time.Millisecond):
	}
	<-stream.events
	select {
	case <-delivered:
	case <-time.After(time.Second):
		t.Fatal("terminal failure was not delivered after buffer capacity became available")
	}
	found := false
	for index := 0; index < cap(stream.events); index++ {
		result := <-stream.events
		found = found || errors.Is(result.err, terminal)
	}
	if !found {
		t.Fatal("terminal failure was dropped")
	}
}

func TestEncodeRequestMapsToolResultsAndRejectsUnsupportedImages(t *testing.T) {
	request := testRequest(t)
	request.Messages = append(request.Messages, providers.Message{
		Role: providers.MessageRoleTool,
		Content: []providers.ContentPart{{
			Kind: providers.ContentKindToolResult,
			ToolResult: &providers.ToolResult{
				CallID: "call_1",
				Content: []providers.ContentPart{{
					Kind: providers.ContentKindText, Text: "result",
				}},
			},
		}},
	})
	body, err := encodeRequest(request, "claude-fixture")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"tool_result"`) ||
		!strings.Contains(string(body), `"tool_use_id":"call_1"`) {
		t.Fatalf("tool result body = %s", body)
	}
	request.Messages[0].Content = []providers.ContentPart{{
		Kind:  providers.ContentKindImage,
		Image: &providers.ImageInput{URL: "https://example.test/private.png"},
	}}
	if _, err := encodeRequest(request, "claude-fixture"); err == nil {
		t.Fatal("remote image URL was accepted without an explicit fetch boundary")
	}
}

func TestAdapterRequiresExplicitRemoteEndpointApproval(t *testing.T) {
	transport, err := providers.NewHTTPTransport(providers.TransportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = New(Config{
		BaseURL:   "https://api.anthropic.com",
		Model:     providers.ModelIdentity{Model: "claude-fixture"},
		Transport: transport,
		UseCredential: func(
			context.Context,
			func([]byte) error,
		) error {
			return nil
		},
	})
	if !errors.Is(err, providers.ErrEndpointApprovalRequired) {
		t.Fatalf("remote endpoint error = %v", err)
	}
}

func newTestAdapter(t *testing.T, server *httptest.Server) *Adapter {
	t.Helper()
	transport, err := providers.NewHTTPTransport(
		providers.TransportOptions{HTTPClient: server.Client()},
	)
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := New(Config{
		BaseURL: server.URL,
		Model: providers.ModelIdentity{
			Provider: providers.ProviderIdentity{
				Adapter: "anthropic-messages", AdapterVersion: "1",
				Provider: "anthropic", ProviderVersion: apiVersion,
			},
			Model: "claude-fixture", Revision: "configured",
		},
		Capabilities: providers.ModelCapabilities{
			Tools: true, StructuredOutput: true, Streaming: true,
		},
		Transport: transport,
		UseCredential: func(
			_ context.Context,
			operation func([]byte) error,
		) error {
			return operation([]byte("anthropic-fixture"))
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}

func testRequest(t *testing.T) providers.ModelRequest {
	t.Helper()
	requestID, err := domain.NewModelRequestID()
	if err != nil {
		t.Fatal(err)
	}
	model := providers.ModelIdentity{
		Provider: providers.ProviderIdentity{
			Adapter: "anthropic-messages", AdapterVersion: "1",
			Provider: "anthropic", ProviderVersion: apiVersion,
		},
		Model: "claude-fixture", Revision: "configured",
	}
	return providers.ModelRequest{
		Identity: providers.RequestIdentity{
			ModelRequestID: requestID, Provider: model.Provider, Model: model,
			RequestHash:    "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			IdempotencyKey: "anthropic-test-logical-request",
			Idempotency:    providers.RequestIdempotency{ProviderSupported: false},
		},
		Messages: []providers.Message{{
			Role: providers.MessageRoleUser,
			Content: []providers.ContentPart{{
				Kind: providers.ContentKindText, Text: "hello",
			}},
		}},
		Tools: []providers.ToolDeclaration{{
			Name: "lookup", Description: "look up a value",
			InputSchema: json.RawMessage(`{"type":"object"}`),
		}},
		StructuredOutput: &providers.StructuredOutputRequirement{
			Name: "answer", Strict: true,
			Schema: json.RawMessage(`{"type":"object"}`),
		},
		MaximumTokens: 256,
		Deadline:      time.Now().Add(time.Minute),
	}
}
