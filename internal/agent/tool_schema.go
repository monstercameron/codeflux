package agent

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/executor"
	"codeflux.dev/codeflux/internal/providers"
)

const (
	maximumToolArguments    = 64
	defaultArgumentMaxBytes = 4 << 10
	maximumToolCallBytes    = 64 << 10
)

type toolSchemaProperty struct {
	Type      string `json:"type"`
	MaxLength int    `json:"maxLength"`
}

type toolInputSchema struct {
	Type                 string                        `json:"type"`
	Properties           map[string]toolSchemaProperty `json:"properties"`
	Required             []string                      `json:"required"`
	AdditionalProperties bool                          `json:"additionalProperties"`
}

func approvedToolSchema(tool ApprovedTool) (json.RawMessage, error) {
	if err := validateApprovedTool(tool); err != nil {
		return nil, err
	}
	schema := toolInputSchema{
		Type:                 "object",
		Properties:           make(map[string]toolSchemaProperty, len(tool.Arguments)),
		AdditionalProperties: false,
	}
	for _, argument := range tool.Arguments {
		maximum := argument.MaxBytes
		if maximum == 0 {
			maximum = defaultArgumentMaxBytes
		}
		schema.Properties[argument.Name] = toolSchemaProperty{
			Type: "string", MaxLength: maximum,
		}
		if argument.Required {
			schema.Required = append(schema.Required, argument.Name)
		}
	}
	slices.Sort(schema.Required)
	encoded, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

func buildToolDeclarations(
	tools []ApprovedTool,
) ([]providers.ToolDeclaration, map[string]ApprovedTool, error) {
	declarations := make([]providers.ToolDeclaration, 0, len(tools))
	approved := make(map[string]ApprovedTool, len(tools))
	for _, tool := range tools {
		if err := validateApprovedTool(tool); err != nil {
			return nil, nil, err
		}
		name := string(tool.Descriptor.Name)
		if _, exists := approved[name]; exists {
			return nil, nil, fmt.Errorf("approved tool %q is duplicated", name)
		}
		schema, err := approvedToolSchema(tool)
		if err != nil {
			return nil, nil, err
		}
		approved[name] = tool
		declarations = append(declarations, providers.ToolDeclaration{
			Name: name, Description: tool.Descriptor.Summary,
			InputSchema: schema,
		})
	}
	slices.SortFunc(declarations, func(left, right providers.ToolDeclaration) int {
		return strings.Compare(left.Name, right.Name)
	})
	return declarations, approved, nil
}

func validateApprovedTool(tool ApprovedTool) error {
	if tool.Descriptor.Name == "" ||
		tool.Descriptor.SchemaVersion != executor.ToolSchemaVersion {
		return errors.New("approved tool descriptor is invalid")
	}
	var catalog *executor.ToolDescriptor
	for _, descriptor := range executor.ToolCatalog() {
		if descriptor.Name == tool.Descriptor.Name {
			copy := descriptor
			catalog = &copy
			break
		}
	}
	if catalog == nil ||
		catalog.SchemaVersion != tool.Descriptor.SchemaVersion ||
		catalog.DefaultAuthority != tool.Descriptor.DefaultAuthority ||
		!slices.Equal(catalog.Effects, tool.Descriptor.Effects) {
		return fmt.Errorf(
			"approved tool %q differs from the registered tool contract",
			tool.Descriptor.Name,
		)
	}
	if tool.DefaultTimeout < time.Second ||
		tool.DefaultTimeout > 30*time.Minute {
		return fmt.Errorf(
			"approved tool %q timeout is outside supported bounds",
			tool.Descriptor.Name,
		)
	}
	if len(tool.Arguments) > maximumToolArguments {
		return fmt.Errorf(
			"approved tool %q has too many arguments",
			tool.Descriptor.Name,
		)
	}
	seen := make(map[string]struct{}, len(tool.Arguments))
	for _, argument := range tool.Arguments {
		if strings.TrimSpace(argument.Name) != argument.Name ||
			argument.Name == "" || len(argument.Name) > 255 {
			return fmt.Errorf(
				"approved tool %q argument name is invalid",
				tool.Descriptor.Name,
			)
		}
		if _, exists := seen[argument.Name]; exists {
			return fmt.Errorf(
				"approved tool %q argument %q is duplicated",
				tool.Descriptor.Name,
				argument.Name,
			)
		}
		seen[argument.Name] = struct{}{}
		if argument.MaxBytes < 0 ||
			argument.MaxBytes > maximumToolCallBytes {
			return fmt.Errorf(
				"approved tool %q argument %q bound is invalid",
				tool.Descriptor.Name,
				argument.Name,
			)
		}
	}
	if tool.CreatesCheckpoint != tool.MaterialEdit {
		return errors.New(
			"material edit tools must create checkpoints and read-only tools must not",
		)
	}
	return nil
}

type decodedToolCall struct {
	request           executor.ToolRequest
	redactedArguments string
	argumentsSHA256   string
}

func decodeModelToolCall(
	taskID domain.TaskID,
	runID domain.RunID,
	worktree string,
	model providers.ModelIdentity,
	tool ApprovedTool,
	call providers.ToolCall,
	stepID string,
) (decodedToolCall, error) {
	if strings.TrimSpace(call.ID) != call.ID ||
		call.ID == "" || len(call.ID) > 255 {
		return decodedToolCall{}, errors.New("model tool-call ID is invalid")
	}
	if call.Name != string(tool.Descriptor.Name) {
		return decodedToolCall{}, errors.New("model tool-call name changed during validation")
	}
	values, err := decodeStrictStringObject(call.Arguments)
	if err != nil {
		return decodedToolCall{}, fmt.Errorf("decode model tool call: %w", err)
	}
	arguments := make([]executor.ToolArgument, 0, len(tool.Arguments))
	redacted := make(map[string]string, len(tool.Arguments))
	canonical := make(map[string]string, len(tool.Arguments))
	for _, definition := range tool.Arguments {
		value, found := values[definition.Name]
		if definition.Required && !found {
			return decodedToolCall{}, fmt.Errorf(
				"model tool call lacks required argument %q",
				definition.Name,
			)
		}
		if !found {
			continue
		}
		maximum := definition.MaxBytes
		if maximum == 0 {
			maximum = defaultArgumentMaxBytes
		}
		if len(value) > maximum {
			return decodedToolCall{}, fmt.Errorf(
				"model tool argument %q exceeds its bound",
				definition.Name,
			)
		}
		arguments = append(arguments, executor.ToolArgument{
			Name: definition.Name, Value: value,
			Sensitive: definition.Sensitive,
		})
		canonical[definition.Name] = value
		if definition.Sensitive {
			redacted[definition.Name] = "[REDACTED]"
		} else {
			redacted[definition.Name] = value
		}
		delete(values, definition.Name)
	}
	if len(values) != 0 {
		return decodedToolCall{}, errors.New(
			"model tool call contains an unknown argument",
		)
	}
	redactedJSON, err := json.Marshal(redacted)
	if err != nil {
		return decodedToolCall{}, err
	}
	canonicalJSON, err := json.Marshal(canonical)
	if err != nil {
		return decodedToolCall{}, err
	}
	digest := sha256.Sum256(canonicalJSON)
	requester := model.Provider.Provider + "/" + model.Model + "@" + model.Revision
	if len(requester) > 255 {
		return decodedToolCall{}, errors.New("fixed model identity is too long")
	}
	request := executor.ToolRequest{
		SchemaVersion: executor.ToolSchemaVersion,
		ID:            call.ID, TaskID: taskID, RunID: runID,
		Name: tool.Descriptor.Name, Arguments: arguments,
		WorkingDirectory: worktree, Timeout: tool.DefaultTimeout,
		ClaimedAuthority:    tool.Descriptor.DefaultAuthority,
		ExpectedSideEffects: append([]executor.SideEffect(nil), tool.Descriptor.Effects...),
		IdempotencyKey:      "agent-tool/" + call.ID,
		Requester:           requester,
		PurposeUntrusted:    "execute approved plan step " + stepID,
	}
	if err := executor.ValidateToolRequest(request); err != nil {
		return decodedToolCall{}, err
	}
	return decodedToolCall{
		request: request, redactedArguments: string(redactedJSON),
		argumentsSHA256: hex.EncodeToString(digest[:]),
	}, nil
}

func decodeStrictStringObject(raw json.RawMessage) (map[string]string, error) {
	if len(raw) == 0 || len(raw) > maximumToolCallBytes {
		return nil, errors.New("tool arguments are missing or exceed the bound")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil {
		return nil, errors.New("tool arguments are not valid JSON")
	}
	delimiter, ok := token.(json.Delim)
	if !ok || delimiter != '{' {
		return nil, errors.New("tool arguments must be one JSON object")
	}
	values := make(map[string]string)
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, errors.New("tool argument name is invalid")
		}
		key, ok := keyToken.(string)
		if !ok || strings.TrimSpace(key) != key || key == "" {
			return nil, errors.New("tool argument name is invalid")
		}
		if _, exists := values[key]; exists {
			return nil, fmt.Errorf("tool argument %q is duplicated", key)
		}
		var value string
		if err := decoder.Decode(&value); err != nil {
			return nil, fmt.Errorf("tool argument %q must be a string", key)
		}
		values[key] = value
	}
	token, err = decoder.Token()
	if err != nil || token != json.Delim('}') {
		return nil, errors.New("tool arguments object is incomplete")
	}
	if token, err = decoder.Token(); !errors.Is(err, io.EOF) || token != nil {
		return nil, errors.New("tool arguments contain trailing JSON")
	}
	return values, nil
}
