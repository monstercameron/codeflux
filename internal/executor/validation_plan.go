package executor

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const ValidationWorkingDirectoryScope = "task-worktree"

type canonicalValidationPlanCommand struct {
	SchemaVersion         int                               `json:"schema_version"`
	Tool                  ToolName                          `json:"tool"`
	Arguments             []canonicalValidationPlanArgument `json:"arguments"`
	WorkingDirectoryScope string                            `json:"working_directory_scope"`
	TimeoutNanoseconds    int64                             `json:"timeout_nanoseconds"`
	Authority             AuthorityClass                    `json:"authority"`
	Effects               []SideEffect                      `json:"effects"`
}

type canonicalValidationPlanArgument struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// RenderValidationPlanCommand projects execution semantics while excluding
// run-specific identities and the physical task-worktree path.
func RenderValidationPlanCommand(request ToolRequest) (string, error) {
	if request.SchemaVersion != ToolSchemaVersion ||
		!validToolName(request.Name) ||
		request.Timeout < time.Second ||
		request.Timeout > 30*time.Minute ||
		!validAuthority(request.ClaimedAuthority) ||
		len(request.ExpectedSideEffects) == 0 ||
		len(request.ExpectedSideEffects) > 16 {
		return "", errors.New("validation plan command semantics are invalid")
	}
	projection := canonicalValidationPlanCommand{
		SchemaVersion:         request.SchemaVersion,
		Tool:                  request.Name,
		WorkingDirectoryScope: ValidationWorkingDirectoryScope,
		TimeoutNanoseconds:    int64(request.Timeout),
		Authority:             RequiredAuthority(request),
		Effects: append(
			[]SideEffect(nil), request.ExpectedSideEffects...,
		),
		Arguments: make(
			[]canonicalValidationPlanArgument, 0, len(request.Arguments),
		),
	}
	for _, argument := range request.Arguments {
		if argument.Name == "" || len(argument.Name) > 255 ||
			len(argument.Value) > 32<<10 || argument.Sensitive {
			return "", errors.New(
				"validation plan command argument is invalid or sensitive",
			)
		}
		projection.Arguments = append(
			projection.Arguments,
			canonicalValidationPlanArgument{
				Name: argument.Name, Value: argument.Value,
			},
		)
	}
	for _, effect := range request.ExpectedSideEffects {
		if !validSideEffect(effect) {
			return "", errors.New("validation plan command effect is invalid")
		}
	}
	encoded, err := json.Marshal(projection)
	if err != nil {
		return "", fmt.Errorf("encode validation plan command: %w", err)
	}
	if len(encoded) > 4096 {
		return "", errors.New("validation plan command exceeds its bound")
	}
	return string(encoded), nil
}

// ValidateValidationPlanCommand rejects non-canonical or malformed persisted
// projections.
func ValidateValidationPlanCommand(value string) error {
	if value == "" || strings.TrimSpace(value) != value ||
		len(value) > 4096 || !json.Valid([]byte(value)) {
		return errors.New("validation plan command is invalid")
	}
	var projection canonicalValidationPlanCommand
	if err := json.Unmarshal([]byte(value), &projection); err != nil {
		return errors.New("validation plan command is invalid")
	}
	if projection.WorkingDirectoryScope !=
		ValidationWorkingDirectoryScope {
		return errors.New("validation plan command worktree scope is invalid")
	}
	request := ToolRequest{
		SchemaVersion:    projection.SchemaVersion,
		Name:             projection.Tool,
		Timeout:          time.Duration(projection.TimeoutNanoseconds),
		ClaimedAuthority: projection.Authority,
		ExpectedSideEffects: append(
			[]SideEffect(nil), projection.Effects...,
		),
		Arguments: make([]ToolArgument, 0, len(projection.Arguments)),
	}
	for _, argument := range projection.Arguments {
		request.Arguments = append(request.Arguments, ToolArgument{
			Name: argument.Name, Value: argument.Value,
		})
	}
	rendered, err := RenderValidationPlanCommand(request)
	if err != nil || rendered != value {
		return errors.New("validation plan command is not canonical")
	}
	return nil
}
