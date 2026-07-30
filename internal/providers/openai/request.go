package openai

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"codeflux.dev/codeflux/internal/providers"
)

type responseRequest struct {
	Model           string             `json:"model"`
	Input           []any              `json:"input"`
	Tools           []responseTool     `json:"tools,omitempty"`
	Text            *responseText      `json:"text,omitempty"`
	MaxOutputTokens int64              `json:"max_output_tokens,omitempty"`
	Temperature     *float64           `json:"temperature,omitempty"`
	Reasoning       *responseReasoning `json:"reasoning,omitempty"`
	Stream          bool               `json:"stream"`
	Store           bool               `json:"store"`
}

type responseTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
	Strict      bool            `json:"strict"`
}

type responseText struct {
	Format responseFormat `json:"format"`
}

type responseFormat struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Schema      json.RawMessage `json:"schema"`
	Strict      bool            `json:"strict"`
}

type responseReasoning struct {
	Effort string `json:"effort"`
}

func (adapter *Adapter) buildRequest(request providers.ModelRequest) ([]byte, error) {
	if err := providers.ValidateModelRequest(
		request,
		adapter.model,
		adapter.capabilities,
		true,
	); err != nil {
		return nil, err
	}
	input, err := encodeMessages(request.Messages)
	if err != nil {
		return nil, err
	}
	wire := responseRequest{
		Model: adapter.model.Model, Input: input, MaxOutputTokens: request.MaximumTokens,
		Temperature: request.Temperature, Stream: true, Store: false,
	}
	for _, tool := range request.Tools {
		if strings.TrimSpace(tool.Name) == "" || !validJSONObject(tool.InputSchema) {
			return nil, errors.New("tool name and object input schema are required")
		}
		wire.Tools = append(wire.Tools, responseTool{
			Type: "function", Name: tool.Name, Description: tool.Description,
			Parameters: append(json.RawMessage(nil), tool.InputSchema...), Strict: true,
		})
	}
	if request.StructuredOutput != nil {
		output := request.StructuredOutput
		if strings.TrimSpace(output.Name) == "" || !validJSONObject(output.Schema) {
			return nil, errors.New("structured output name and object schema are required")
		}
		wire.Text = &responseText{Format: responseFormat{
			Type: "json_schema", Name: output.Name, Description: output.Description,
			Schema: append(json.RawMessage(nil), output.Schema...), Strict: output.Strict,
		}}
	}
	if request.ReasoningEffort != "" {
		wire.Reasoning = &responseReasoning{Effort: request.ReasoningEffort}
	}
	return json.Marshal(wire)
}

func encodeMessages(messages []providers.Message) ([]any, error) {
	if len(messages) == 0 {
		return nil, errors.New("at least one message is required")
	}
	input := make([]any, 0, len(messages))
	for _, message := range messages {
		if !validRole(message.Role) || len(message.Content) == 0 {
			return nil, errors.New("message role and content are required")
		}
		content := make([]any, 0, len(message.Content))
		flushContent := func() {
			if len(content) == 0 {
				return
			}
			input = append(input, map[string]any{
				"role": string(message.Role), "content": content,
			})
			content = nil
		}
		for _, part := range message.Content {
			switch part.Kind {
			case providers.ContentKindText:
				if message.Role == providers.MessageRoleTool {
					return nil, errors.New("tool messages require normalized tool-result content")
				}
				if part.Text == "" {
					return nil, errors.New("text content must not be empty")
				}
				content = append(content, map[string]any{
					"type": "input_text", "text": part.Text,
				})
			case providers.ContentKindImage:
				if message.Role == providers.MessageRoleTool {
					return nil, errors.New("tool messages do not accept image content")
				}
				if !adapterSupportsImagesInMessage(message) {
					return nil, errors.New("image input requires a user message")
				}
				image, err := encodeImage(part.Image)
				if err != nil {
					return nil, err
				}
				content = append(content, image)
			case providers.ContentKindToolCall:
				if message.Role != providers.MessageRoleAssistant {
					return nil, errors.New("tool calls require an assistant message")
				}
				if part.ToolCall == nil || part.ToolCall.ID == "" ||
					part.ToolCall.Name == "" || !json.Valid(part.ToolCall.Arguments) {
					return nil, errors.New("tool call identity, name, and JSON arguments are required")
				}
				flushContent()
				input = append(input, map[string]any{
					"type": "function_call", "call_id": part.ToolCall.ID,
					"name": part.ToolCall.Name, "arguments": string(part.ToolCall.Arguments),
				})
			case providers.ContentKindToolResult:
				if message.Role != providers.MessageRoleTool {
					return nil, errors.New("tool results require a tool message")
				}
				if part.ToolResult == nil || part.ToolResult.CallID == "" {
					return nil, errors.New("tool result call identity is required")
				}
				flushContent()
				output, err := encodeToolResult(*part.ToolResult)
				if err != nil {
					return nil, err
				}
				input = append(input, map[string]any{
					"type": "function_call_output", "call_id": part.ToolResult.CallID,
					"output": output,
				})
			default:
				return nil, fmt.Errorf("unsupported message content kind %q", part.Kind)
			}
		}
		flushContent()
	}
	if len(input) == 0 {
		return nil, errors.New("messages produced no OpenAI input items")
	}
	return input, nil
}

func encodeImage(image *providers.ImageInput) (map[string]any, error) {
	if image == nil {
		return nil, errors.New("image content is required")
	}
	if image.URL != "" {
		return nil, errors.New("remote image URLs require separate fetch authority and are not accepted")
	}
	if len(image.Data) == 0 || image.MediaType == "" {
		return nil, errors.New("inline image data and media type are required")
	}
	url := "data:" + image.MediaType + ";base64," +
		base64.StdEncoding.EncodeToString(image.Data)
	value := map[string]any{"type": "input_image", "image_url": url}
	if image.Detail != "" {
		value["detail"] = image.Detail
	}
	return value, nil
}

func encodeToolResult(result providers.ToolResult) (string, error) {
	var text strings.Builder
	for _, part := range result.Content {
		if part.Kind != providers.ContentKindText || part.Text == "" {
			return "", errors.New("OpenAI tool results currently require non-empty text content")
		}
		text.WriteString(part.Text)
	}
	if text.Len() == 0 {
		return "", errors.New("tool result content is required")
	}
	if result.IsError {
		encoded, err := json.Marshal(map[string]any{
			"is_error": true,
			"content":  text.String(),
		})
		if err != nil {
			return "", err
		}
		return string(encoded), nil
	}
	return text.String(), nil
}

func validRole(role providers.MessageRole) bool {
	switch role {
	case providers.MessageRoleSystem, providers.MessageRoleDeveloper,
		providers.MessageRoleUser, providers.MessageRoleAssistant,
		providers.MessageRoleTool:
		return true
	default:
		return false
	}
}

func validJSONObject(value json.RawMessage) bool {
	if !json.Valid(value) {
		return false
	}
	var object map[string]json.RawMessage
	return json.Unmarshal(value, &object) == nil && object != nil
}

func adapterSupportsImagesInMessage(message providers.Message) bool {
	return message.Role == providers.MessageRoleUser
}
