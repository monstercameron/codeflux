package providers

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
)

const (
	maximumProviderMessages         = 1024
	maximumProviderContentParts     = 4096
	maximumProviderTools            = 128
	maximumProviderSchemaBytes      = 1 << 20
	maximumProviderInlineImageBytes = 6 << 20
	maximumProviderTextBytes        = 8 << 20
)

var (
	ErrInvalidModelRequest          = errors.New("provider model request is invalid")
	ErrRemoteImageAuthorityRequired = errors.New("remote image input requires a separate approved fetch boundary")
)

// ValidateModelRequest validates one adapter-bound normalized request before
// credentials are loaded or wire payloads are allocated.
func ValidateModelRequest(
	request ModelRequest,
	expected ModelIdentity,
	capabilities ModelCapabilities,
	providerSupportsIdempotency bool,
) error {
	if err := ValidateRequestIdentity(request.Identity); err != nil {
		return err
	}
	if !reflect.DeepEqual(request.Identity.Model, expected) ||
		!reflect.DeepEqual(request.Identity.Provider, expected.Provider) {
		return fmt.Errorf("%w: configured provider/model identity differs", ErrInvalidModelRequest)
	}
	if request.Idempotency != request.Identity.Idempotency {
		return fmt.Errorf("%w: request idempotency differs from durable identity", ErrInvalidModelRequest)
	}
	if request.Idempotency.ProviderSupported && !providerSupportsIdempotency {
		return fmt.Errorf("%w: adapter does not support provider idempotency", ErrInvalidModelRequest)
	}
	if request.Deadline.IsZero() {
		return fmt.Errorf("%w: request deadline is required", ErrInvalidModelRequest)
	}
	if request.MaximumTokens < 1 {
		return fmt.Errorf("%w: maximum output tokens must be positive", ErrInvalidModelRequest)
	}
	if capabilities.MaximumOutputTokens > 0 &&
		request.MaximumTokens > capabilities.MaximumOutputTokens {
		return fmt.Errorf("%w: maximum output tokens exceed configured capability", ErrInvalidModelRequest)
	}
	if request.Temperature != nil &&
		(math.IsNaN(*request.Temperature) ||
			math.IsInf(*request.Temperature, 0) ||
			*request.Temperature < 0 ||
			*request.Temperature > 2) {
		return fmt.Errorf("%w: temperature must be finite and between zero and two", ErrInvalidModelRequest)
	}
	if len(request.Messages) == 0 ||
		len(request.Messages) > maximumProviderMessages {
		return fmt.Errorf("%w: message count is outside supported bounds", ErrInvalidModelRequest)
	}
	if len(request.Tools) > maximumProviderTools {
		return fmt.Errorf("%w: tool count exceeds supported bounds", ErrInvalidModelRequest)
	}
	var contentParts int
	var textBytes int
	for _, message := range request.Messages {
		if !validMessageRole(message.Role) || len(message.Content) == 0 {
			return fmt.Errorf("%w: message role and content are required", ErrInvalidModelRequest)
		}
		contentParts += len(message.Content)
		if contentParts > maximumProviderContentParts {
			return fmt.Errorf("%w: content part count exceeds supported bounds", ErrInvalidModelRequest)
		}
		for _, part := range message.Content {
			count, err := validateContentPart(message.Role, part, capabilities)
			if err != nil {
				return err
			}
			if count > maximumProviderTextBytes-textBytes {
				return fmt.Errorf("%w: request text exceeds supported bounds", ErrInvalidModelRequest)
			}
			textBytes += count
		}
	}
	if len(request.Tools) != 0 && !capabilities.Tools {
		return fmt.Errorf("%w: configured model does not support tools", ErrInvalidModelRequest)
	}
	for _, tool := range request.Tools {
		if strings.TrimSpace(tool.Name) != tool.Name ||
			tool.Name == "" ||
			len(tool.Name) > 255 ||
			len(tool.Description) > 16<<10 ||
			!validBoundedJSONObject(tool.InputSchema) {
			return fmt.Errorf("%w: tool declaration is invalid", ErrInvalidModelRequest)
		}
	}
	if output := request.StructuredOutput; output != nil {
		if !capabilities.StructuredOutput ||
			strings.TrimSpace(output.Name) != output.Name ||
			output.Name == "" ||
			len(output.Name) > 255 ||
			len(output.Description) > 16<<10 ||
			!validBoundedJSONObject(output.Schema) {
			return fmt.Errorf("%w: structured output requirement is invalid", ErrInvalidModelRequest)
		}
	}
	if request.ReasoningEffort != "" &&
		!containsCapabilityValue(capabilities.ReasoningControls, request.ReasoningEffort) {
		return fmt.Errorf("%w: reasoning control is not configured", ErrInvalidModelRequest)
	}
	return nil
}

func validateContentPart(
	role MessageRole,
	part ContentPart,
	capabilities ModelCapabilities,
) (int, error) {
	switch part.Kind {
	case ContentKindText:
		if part.Text == "" ||
			part.Image != nil ||
			part.ToolCall != nil ||
			part.ToolResult != nil {
			return 0, fmt.Errorf("%w: text content is invalid", ErrInvalidModelRequest)
		}
		return len(part.Text), nil
	case ContentKindImage:
		if !capabilities.ImageInput ||
			part.Image == nil ||
			part.Text != "" ||
			part.ToolCall != nil ||
			part.ToolResult != nil {
			return 0, fmt.Errorf("%w: image content is invalid", ErrInvalidModelRequest)
		}
		if part.Image.URL != "" {
			return 0, ErrRemoteImageAuthorityRequired
		}
		if len(part.Image.Data) == 0 ||
			len(part.Image.Data) > maximumProviderInlineImageBytes ||
			!validImageMediaType(part.Image.MediaType) {
			return 0, fmt.Errorf("%w: inline image is invalid or too large", ErrInvalidModelRequest)
		}
		return 0, nil
	case ContentKindToolCall:
		if role != MessageRoleAssistant ||
			part.ToolCall == nil ||
			part.Text != "" ||
			part.Image != nil ||
			part.ToolResult != nil ||
			strings.TrimSpace(part.ToolCall.ID) == "" ||
			strings.TrimSpace(part.ToolCall.Name) == "" ||
			len(part.ToolCall.ID) > 255 ||
			len(part.ToolCall.Name) > 255 ||
			len(part.ToolCall.Arguments) > maximumProviderSchemaBytes ||
			!json.Valid(part.ToolCall.Arguments) {
			return 0, fmt.Errorf("%w: tool call content is invalid", ErrInvalidModelRequest)
		}
		return 0, nil
	case ContentKindToolResult:
		if role != MessageRoleTool ||
			part.ToolResult == nil ||
			part.Text != "" ||
			part.Image != nil ||
			part.ToolCall != nil ||
			strings.TrimSpace(part.ToolResult.CallID) == "" ||
			len(part.ToolResult.CallID) > 255 ||
			len(part.ToolResult.Content) == 0 {
			return 0, fmt.Errorf("%w: tool result content is invalid", ErrInvalidModelRequest)
		}
		total := 0
		for _, nested := range part.ToolResult.Content {
			if nested.Kind != ContentKindText || nested.Text == "" {
				return 0, fmt.Errorf("%w: tool result supports bounded text content", ErrInvalidModelRequest)
			}
			if len(nested.Text) > maximumProviderTextBytes-total {
				return 0, fmt.Errorf("%w: tool result text exceeds supported bounds", ErrInvalidModelRequest)
			}
			total += len(nested.Text)
		}
		return total, nil
	default:
		return 0, fmt.Errorf("%w: unsupported content kind %q", ErrInvalidModelRequest, part.Kind)
	}
}

func validMessageRole(role MessageRole) bool {
	switch role {
	case MessageRoleSystem, MessageRoleDeveloper, MessageRoleUser,
		MessageRoleAssistant, MessageRoleTool:
		return true
	default:
		return false
	}
}

func validBoundedJSONObject(value json.RawMessage) bool {
	if len(value) == 0 || len(value) > maximumProviderSchemaBytes ||
		!json.Valid(value) {
		return false
	}
	var object map[string]json.RawMessage
	return json.Unmarshal(value, &object) == nil && object != nil
}

func validImageMediaType(value string) bool {
	switch value {
	case "image/gif", "image/jpeg", "image/png", "image/webp":
		return true
	default:
		return false
	}
}

func containsCapabilityValue(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
