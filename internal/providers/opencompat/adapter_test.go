package opencompat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/providers"
)

func TestAdapterDiscoversModelsAndStreamsUnknownUsageSafely(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		if request.Header.Get("Authorization") != "Bearer local-fixture" {
			t.Errorf("authorization header = %q", request.Header.Get("Authorization"))
		}
		switch request.URL.Path {
		case "/v1/models":
			_, _ = writer.Write([]byte(
				`{"object":"list","data":[{"id":"local-fixture","object":"model","owned_by":"local"},{"id":"unconfigured","object":"model","owned_by":"local"}]}`,
			))
		case "/v1/chat/completions":
			var body map[string]any
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["response_format"] == nil || body["tools"] == nil {
				t.Errorf("request body = %#v", body)
			}
			writer.Header().Set("Content-Type", "text/event-stream")
			writer.Header().Set("X-Request-ID", "req_local_fixture")
			chunks := []string{
				`{"id":"chat_1","model":"local-fixture-r1","system_fingerprint":"fp_safe","choices":[{"delta":{"content":"hi","tool_calls":[]},"finish_reason":""}],"usage":null}`,
				`{"id":"chat_1","model":"local-fixture-r1","system_fingerprint":"fp_safe","choices":[{"delta":{"content":"","tool_calls":[{"index":0,"id":"call_1","function":{"name":"lookup","arguments":"{\"id\":"}}]},"finish_reason":""}],"usage":null}`,
				`{"id":"chat_1","model":"local-fixture-r1","system_fingerprint":"fp_safe","choices":[{"delta":{"content":"","tool_calls":[{"index":0,"id":"","function":{"name":"","arguments":"9}"}}]},"finish_reason":"tool_calls"}],"usage":null}`,
			}
			for _, chunk := range chunks {
				_, _ = fmt.Fprintf(writer, "data: %s\n\n", chunk)
			}
			_, _ = fmt.Fprint(writer, "data: [DONE]\n\n")
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	adapter := newLocalAdapter(t, server, true)
	models, err := adapter.ListModels(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].Identity.Model != "local-fixture" {
		t.Fatalf("models = %#v", models)
	}
	if models[1].Capabilities.Tools ||
		models[1].Capabilities.ContextTokens != 0 {
		t.Fatalf("unconfigured model inherited capabilities: %#v", models[1])
	}
	request := localRequest(t)
	stream, err := adapter.Stream(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	var text string
	var tool *providers.ToolCall
	var toolDelta *providers.ToolCallDelta
	var final *providers.FinalResponse
	for {
		event, recvErr := stream.Recv(t.Context())
		if errors.Is(recvErr, io.EOF) {
			break
		}
		if recvErr != nil {
			t.Fatal(recvErr)
		}
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
	if text != "hi" || tool == nil || string(tool.Arguments) != `{"id":9}` {
		t.Fatalf("text=%q tool=%#v", text, tool)
	}
	if final == nil || final.Usage.Known ||
		final.Usage.Source != providers.UsageSourceUnknown ||
		final.StopReason != providers.StopReasonToolCalls ||
		final.Metadata.ResponseID != "chat_1" ||
		final.Metadata.RequestID != request.Identity.ModelRequestID.String() ||
		final.Metadata.Fingerprint != "fp_safe" ||
		final.Metadata.Fields["provider_request_id"] != "req_local_fixture" {
		t.Fatalf("final = %#v", final)
	}
	if toolDelta == nil {
		t.Fatal("missing normalized tool delta")
	}
	if toolDelta.ArgumentsFragment != `9}` {
		t.Fatalf("tool delta fragment = %q", toolDelta.ArgumentsFragment)
	}
	capabilities, err := adapter.Capabilities(t.Context(), models[0].Identity)
	if err != nil || !capabilities.Tools {
		t.Fatalf("capabilities=%#v err=%v", capabilities, err)
	}
	if _, err := adapter.Capabilities(t.Context(), models[1].Identity); err == nil {
		t.Fatal("unconfigured model capabilities were accepted")
	}
}

func TestAdapterCancellationRejectsBufferedLateEvents(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		writer.Header().Set("Content-Type", "text/event-stream")
		flusher := writer.(http.Flusher)
		_, _ = fmt.Fprint(writer,
			"data: {\"id\":\"chat_cancel\",\"model\":\"local-fixture-r1\",\"choices\":[{\"delta\":{\"content\":\"first\"},\"finish_reason\":\"\"}]}\n\n",
			"data: {\"id\":\"chat_cancel\",\"model\":\"local-fixture-r1\",\"choices\":[{\"delta\":{\"content\":\"late-buffered\"},\"finish_reason\":\"\"}]}\n\n",
		)
		flusher.Flush()
		close(started)
		<-request.Context().Done()
	}))
	defer server.Close()
	adapter := newLocalAdapter(t, server, false)
	request := localRequest(t)
	stream, err := adapter.Stream(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("OpenAI-compatible stream did not start")
	}
	if _, err := stream.Recv(t.Context()); err != nil {
		t.Fatal(err)
	}
	first, err := stream.Recv(t.Context())
	if err != nil || first.Text != "first" {
		t.Fatalf("first text = %#v, %v", first, err)
	}
	if err := adapter.Cancel(t.Context(), request.Identity.ModelRequestID); err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Recv(t.Context()); err == nil {
		t.Fatal("buffered event was delivered after cancellation")
	} else {
		var failure *providers.Failure
		if !errors.As(err, &failure) ||
			failure.Kind != providers.FailureCanceled {
			t.Fatalf("cancellation error = %T %v", err, err)
		}
	}
}

func TestAdapterRejectsIdentityMismatchAndMalformedTerminalStream(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		requests.Add(1)
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(writer, "data: {}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	adapter := newLocalAdapter(t, server, false)
	mismatch := localRequest(t)
	mismatch.Identity.Provider.ProviderVersion = "different"
	if _, err := adapter.Stream(t.Context(), mismatch); err == nil {
		t.Fatal("mismatched provider identity was accepted")
	}
	if requests.Load() != 0 {
		t.Fatalf("identity mismatch caused %d provider requests", requests.Load())
	}
	stream, err := adapter.Stream(t.Context(), localRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	_, err = stream.Recv(t.Context())
	var failure *providers.Failure
	if !errors.As(err, &failure) ||
		failure.Kind != providers.FailureTransport ||
		!failure.Retryable ||
		failure.PartialEffect != (providers.PartialEffectEvidence{}) {
		t.Fatalf("malformed-stream failure = %T %v", err, err)
	}
}

func TestAdapterRejectsMissingDoneWithoutUnsafeRetry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		_ *http.Request,
	) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(writer,
			"data: {\"id\":\"chat_incomplete\",\"model\":\"local-fixture-r1\",\"choices\":[{\"delta\":{\"content\":\"partial\"},\"finish_reason\":\"\"}]}\n\n",
		)
	}))
	defer server.Close()
	adapter := newLocalAdapter(t, server, false)
	stream, err := adapter.Stream(t.Context(), localRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	for {
		_, recvErr := stream.Recv(t.Context())
		if recvErr == nil {
			continue
		}
		var failure *providers.Failure
		if !errors.As(recvErr, &failure) ||
			failure.Kind != providers.FailureTransport ||
			failure.Retryable ||
			!failure.PartialEffect.ProviderAck ||
			!failure.PartialEffect.StreamedOutput {
			t.Fatalf("missing-DONE failure = %T %v", recvErr, recvErr)
		}
		break
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
	adapter := newLocalAdapter(t, server, false)
	request := localRequest(t)
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

func TestAdapterRequiresApprovalForRemoteAndClassifiesNonstandardError(t *testing.T) {
	transport, err := providers.NewHTTPTransport(providers.TransportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = New(Config{
		BaseURL:   "https://models.example.test",
		Model:     providers.ModelIdentity{Model: "local"},
		Transport: transport,
	})
	if !errors.Is(err, providers.ErrEndpointApprovalRequired) {
		t.Fatalf("remote endpoint error = %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		_ *http.Request,
	) {
		writer.WriteHeader(http.StatusServiceUnavailable)
		_, _ = writer.Write([]byte("<html>synthetic private error</html>"))
	}))
	defer server.Close()
	adapter := newLocalAdapter(t, server, false)
	stream, err := adapter.Stream(t.Context(), localRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	_, err = stream.Recv(t.Context())
	var failure *providers.Failure
	if !errors.As(err, &failure) || failure.Kind != providers.FailureUnavailable ||
		!failure.Retryable {
		t.Fatalf("failure = %T %v", err, err)
	}
	if err != nil && containsPrivateText(err.Error()) {
		t.Fatal("nonstandard provider error body leaked through Error")
	}
}

func containsPrivateText(value string) bool {
	return value == "synthetic private error" ||
		len(value) > 0 && (value[0] == '<' || value[len(value)-1] == '>')
}

func newLocalAdapter(
	t *testing.T,
	server *httptest.Server,
	discover bool,
) *Adapter {
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
				Adapter: "openai-compatible", AdapterVersion: "1",
				Provider: "local", ProviderVersion: "configured",
			},
			Model: "local-fixture", Revision: "configured",
		},
		Capabilities: providers.ModelCapabilities{
			Tools: true, StructuredOutput: true, Streaming: true,
		},
		Transport: transport, DiscoverModels: discover,
		UseCredential: func(
			_ context.Context,
			operation func([]byte) error,
		) error {
			return operation([]byte("local-fixture"))
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}

func localRequest(t *testing.T) providers.ModelRequest {
	t.Helper()
	requestID, err := domain.NewModelRequestID()
	if err != nil {
		t.Fatal(err)
	}
	model := providers.ModelIdentity{
		Provider: providers.ProviderIdentity{
			Adapter: "openai-compatible", AdapterVersion: "1",
			Provider: "local", ProviderVersion: "configured",
		},
		Model: "local-fixture", Revision: "configured",
	}
	return providers.ModelRequest{
		Identity: providers.RequestIdentity{
			ModelRequestID: requestID, Provider: model.Provider, Model: model,
			RequestHash:    "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			IdempotencyKey: "opencompat-test-logical-request",
		},
		Messages: []providers.Message{{
			Role: providers.MessageRoleUser,
			Content: []providers.ContentPart{{
				Kind: providers.ContentKindText, Text: "hello",
			}},
		}},
		Tools: []providers.ToolDeclaration{{
			Name: "lookup", InputSchema: json.RawMessage(`{"type":"object"}`),
		}},
		StructuredOutput: &providers.StructuredOutputRequirement{
			Name: "answer", Strict: true,
			Schema: json.RawMessage(`{"type":"object"}`),
		},
		MaximumTokens: 128,
		Deadline:      time.Now().Add(time.Minute),
	}
}
