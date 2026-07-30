package providers_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/providers"
	"codeflux.dev/codeflux/internal/providers/anthropic"
	"codeflux.dev/codeflux/internal/providers/openai"
	"codeflux.dev/codeflux/internal/providers/opencompat"
)

func TestNormalizedConversationPassesAllAdapters(t *testing.T) {
	for _, fixture := range []struct {
		name string
		new  func(*testing.T, *httptest.Server) providers.ModelProvider
	}{
		{name: "openai", new: newOpenAIConformanceAdapter},
		{name: "anthropic", new: newAnthropicConformanceAdapter},
		{name: "openai-compatible", new: newLocalConformanceAdapter},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(
				func(writer http.ResponseWriter, request *http.Request) {
					if request.URL.Path == "/models" {
						_, _ = writer.Write([]byte(
							`{"data":[{"id":"model-fixture"}]}`,
						))
						return
					}
					var body map[string]any
					if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					writer.Header().Set("Content-Type", "text/event-stream")
					switch request.URL.Path {
					case "/responses":
						_, _ = fmt.Fprint(writer,
							"data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n",
							"data: {\"type\":\"response.output_item.added\",\"output_index\":1,\"item\":{\"type\":\"function_call\",\"call_id\":\"call_1\",\"name\":\"lookup\"}}\n\n",
							"data: {\"type\":\"response.function_call_arguments.delta\",\"output_index\":1,\"item_id\":\"call_1\",\"delta\":\"{\\\"id\\\":\"}\n\n",
							"data: {\"type\":\"response.function_call_arguments.delta\",\"output_index\":1,\"item_id\":\"call_1\",\"delta\":\"\\\"42\\\"}\"}\n\n",
							"data: {\"type\":\"response.function_call_arguments.done\",\"output_index\":1,\"item_id\":\"call_1\",\"name\":\"lookup\",\"arguments\":\"{\\\"id\\\":\\\"42\\\"}\"}\n\n",
							"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"model\":\"model-fixture\",\"status\":\"completed\",\"output\":[{\"type\":\"function_call\",\"call_id\":\"call_1\",\"name\":\"lookup\"}],\"usage\":{\"input_tokens\":10,\"output_tokens\":2,\"total_tokens\":12}}}\n\n",
						)
					case "/v1/messages":
						_, _ = fmt.Fprint(writer,
							"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"model-fixture\",\"usage\":{\"input_tokens\":10,\"output_tokens\":1}}}\n\n",
							"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\n",
							"data: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"call_1\",\"name\":\"lookup\"}}\n\n",
							"data: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"id\\\":\"}}\n\n",
							"data: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"\\\"42\\\"}\"}}\n\n",
							"data: {\"type\":\"content_block_stop\",\"index\":1}\n\n",
							"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"output_tokens\":2}}\n\n",
							"data: {\"type\":\"message_stop\"}\n\n",
						)
					case "/v1/chat/completions":
						_, _ = fmt.Fprint(writer,
							"data: {\"id\":\"chat_1\",\"model\":\"model-fixture\",\"choices\":[{\"delta\":{\"content\":\"hello\",\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"function\":{\"name\":\"lookup\",\"arguments\":\"{\\\"id\\\":\"}}]},\"finish_reason\":\"\"}],\"usage\":null}\n\n",
							"data: {\"id\":\"chat_1\",\"model\":\"model-fixture\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"\\\"42\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}],\"usage\":null}\n\n",
							"data: {\"id\":\"chat_1\",\"model\":\"model-fixture\",\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2,\"total_tokens\":12}}\n\n",
							"data: [DONE]\n\n",
						)
					default:
						http.NotFound(writer, request)
					}
				},
			))
			defer server.Close()
			adapter := fixture.new(t, server)
			models, err := adapter.ListModels(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if len(models) != 1 {
				t.Fatalf("models = %#v", models)
			}
			request := conformanceRequest(t, models[0].Identity)
			stream, err := adapter.Stream(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			var text string
			var final *providers.FinalResponse
			var tool *providers.ToolCall
			var fragments string
			for {
				event, recvErr := stream.Recv(t.Context())
				if errors.Is(recvErr, io.EOF) {
					break
				}
				if recvErr != nil {
					t.Fatal(recvErr)
				}
				text += event.Text
				if event.ToolCallDelta != nil {
					fragments += event.ToolCallDelta.ArgumentsFragment
				}
				if event.ToolCall != nil {
					copy := *event.ToolCall
					tool = &copy
				}
				if event.Final != nil {
					final = event.Final
				}
			}
			if text != "hello" || final == nil ||
				final.StopReason != providers.StopReasonToolCalls ||
				!final.Usage.Known ||
				final.Usage.InputTokens != 10 ||
				final.Usage.OutputTokens != 2 ||
				tool == nil || tool.ID != "call_1" ||
				tool.Name != "lookup" ||
				string(tool.Arguments) != `{"id":"42"}` ||
				fragments != `{"id":"42"}` {
				t.Fatalf(
					"text=%q fragments=%q tool=%#v final=%#v",
					text,
					fragments,
					tool,
					final,
				)
			}
		})
	}
}

type credentialFixture struct {
	providerID domain.ProviderID
}

func (fixture credentialFixture) Use(
	_ context.Context,
	providerID domain.ProviderID,
	operation func([]byte) error,
) error {
	if providerID != fixture.providerID {
		return errors.New("provider credential identity changed")
	}
	return operation([]byte("conformance-credential"))
}

func newOpenAIConformanceAdapter(
	t *testing.T,
	server *httptest.Server,
) providers.ModelProvider {
	t.Helper()
	providerID, err := domain.NewProviderID()
	if err != nil {
		t.Fatal(err)
	}
	transport, err := providers.NewHTTPTransport(
		providers.TransportOptions{HTTPClient: server.Client()},
	)
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := openai.New(openai.Config{
		ProviderID: providerID, Endpoint: server.URL,
		Model: "model-fixture", ModelRevision: "revision-fixture",
		Capabilities: conformanceCapabilities(),
		Credentials:  credentialFixture{providerID: providerID},
		Transport:    transport,
	})
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}

func newAnthropicConformanceAdapter(
	t *testing.T,
	server *httptest.Server,
) providers.ModelProvider {
	t.Helper()
	transport, err := providers.NewHTTPTransport(
		providers.TransportOptions{HTTPClient: server.Client()},
	)
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := anthropic.New(anthropic.Config{
		BaseURL: server.URL,
		Model: providers.ModelIdentity{
			Provider: providers.ProviderIdentity{
				Adapter: "anthropic-messages", AdapterVersion: "1",
				Provider: "anthropic", ProviderVersion: "messages-v1",
			},
			Model: "model-fixture", Revision: "revision-fixture",
		},
		Capabilities: conformanceCapabilities(),
		Transport:    transport,
		UseCredential: func(
			_ context.Context,
			operation func([]byte) error,
		) error {
			return operation([]byte("conformance-credential"))
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}

func newLocalConformanceAdapter(
	t *testing.T,
	server *httptest.Server,
) providers.ModelProvider {
	t.Helper()
	transport, err := providers.NewHTTPTransport(
		providers.TransportOptions{HTTPClient: server.Client()},
	)
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := opencompat.New(opencompat.Config{
		BaseURL: server.URL,
		Model: providers.ModelIdentity{
			Provider: providers.ProviderIdentity{
				Adapter: "openai-compatible", AdapterVersion: "1",
				Provider: "local", ProviderVersion: "configured",
			},
			Model: "model-fixture", Revision: "revision-fixture",
		},
		Capabilities: conformanceCapabilities(),
		Transport:    transport,
		UseCredential: func(
			_ context.Context,
			operation func([]byte) error,
		) error {
			return operation([]byte("conformance-credential"))
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}

func conformanceCapabilities() providers.ModelCapabilities {
	return providers.ModelCapabilities{
		Tools: true, StructuredOutput: true, Streaming: true,
		ContextTokens: 16_384, MaximumOutputTokens: 4_096,
	}
}

func conformanceRequest(
	t *testing.T,
	model providers.ModelIdentity,
) providers.ModelRequest {
	t.Helper()
	requestID, err := domain.NewModelRequestID()
	if err != nil {
		t.Fatal(err)
	}
	return providers.ModelRequest{
		Identity: providers.RequestIdentity{
			ModelRequestID: requestID, Provider: model.Provider, Model: model,
			RequestHash:    "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			IdempotencyKey: "conformance-logical-request",
		},
		Messages: []providers.Message{{
			Role: providers.MessageRoleUser,
			Content: []providers.ContentPart{{
				Kind: providers.ContentKindText, Text: "say hello",
			}},
		}},
		Tools: []providers.ToolDeclaration{{
			Name: "lookup", Description: "lookup fixture",
			InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
		}},
		StructuredOutput: &providers.StructuredOutputRequirement{
			Name: "answer", Strict: true,
			Schema: json.RawMessage(
				`{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"],"additionalProperties":false}`,
			),
		},
		MaximumTokens: 64,
		Deadline:      time.Now().Add(time.Minute),
	}
}
