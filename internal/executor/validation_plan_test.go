package executor

import (
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

func TestValidationPlanCommandExcludesRunIdentityAndPhysicalWorktree(t *testing.T) {
	taskID, _ := domain.NewTaskID()
	runID, _ := domain.NewRunID()
	request := ToolRequest{
		SchemaVersion: ToolSchemaVersion,
		ID:            "validation-one",
		TaskID:        taskID,
		RunID:         runID,
		Name:          ToolTest,
		Arguments: []ToolArgument{
			{Name: "executable", Value: "go"},
			{Name: "argument", Value: "test"},
			{Name: "argument", Value: "./internal/widget"},
		},
		WorkingDirectory: "C:/first/worktree",
		Timeout:          time.Minute,
		ClaimedAuthority: AuthorityAutomaticRead,
		ExpectedSideEffects: []SideEffect{
			EffectSubprocess, EffectRepositoryRead,
		},
		IdempotencyKey: "validation-one",
		Requester:      "test",
	}
	first, err := RenderValidationPlanCommand(request)
	if err != nil {
		t.Fatal(err)
	}
	request.ID = "validation-two"
	request.WorkingDirectory = "D:/second/worktree"
	request.IdempotencyKey = "validation-two"
	second, err := RenderValidationPlanCommand(request)
	if err != nil {
		t.Fatal(err)
	}
	if first != second ||
		!strings.Contains(
			first, `"working_directory_scope":"task-worktree"`,
		) ||
		strings.Contains(first, "first/worktree") {
		t.Fatalf("first = %s, second = %s", first, second)
	}
	if !strings.Contains(first, `"authority":"privileged"`) {
		t.Fatalf("test plan understated required authority: %s", first)
	}
	if err := ValidateValidationPlanCommand(first); err != nil {
		t.Fatalf("canonical validation command = %v", err)
	}
	request.Arguments[2].Value = "./..."
	substituted, err := RenderValidationPlanCommand(request)
	if err != nil {
		t.Fatal(err)
	}
	if substituted == first {
		t.Fatal("argument substitution did not change plan command")
	}
}

func TestValidationPlanCommandBindsArgumentDerivedAuthority(t *testing.T) {
	request := ToolRequest{
		SchemaVersion: ToolSchemaVersion,
		Name:          ToolRunCommand,
		Arguments: []ToolArgument{
			{Name: "executable", Value: "curl"},
			{Name: "argument", Value: "https://example.invalid"},
		},
		Timeout:          time.Minute,
		ClaimedAuthority: AuthorityAutomaticRead,
		ExpectedSideEffects: []SideEffect{
			EffectSubprocess,
		},
	}
	network, err := RenderValidationPlanCommand(request)
	if err != nil {
		t.Fatal(err)
	}
	request.Arguments = []ToolArgument{
		{Name: "executable", Value: "rm"},
		{Name: "argument", Value: "artifact"},
	}
	destructive, err := RenderValidationPlanCommand(request)
	if err != nil {
		t.Fatal(err)
	}
	if network == destructive ||
		!strings.Contains(network, `"authority":"network"`) ||
		!strings.Contains(destructive, `"authority":"destructive"`) {
		t.Fatalf("network=%s destructive=%s", network, destructive)
	}
}

func TestValidationPlanCommandRejectsSensitiveArgument(t *testing.T) {
	request := ToolRequest{
		SchemaVersion: ToolSchemaVersion,
		Name:          ToolTest,
		Arguments: []ToolArgument{{
			Name: "token", Value: "redacted", Sensitive: true,
		}},
		Timeout:          time.Minute,
		ClaimedAuthority: AuthorityAutomaticRead,
		ExpectedSideEffects: []SideEffect{
			EffectSubprocess, EffectRepositoryRead,
		},
	}
	if _, err := RenderValidationPlanCommand(request); err == nil {
		t.Fatal("sensitive validation argument unexpectedly rendered")
	}
}
