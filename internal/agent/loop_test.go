package agent

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/executor"
	"codeflux.dev/codeflux/internal/providers"
)

func TestExecutionLoopSuccessfulEditUsesBoundedApprovedContextAndCheckpoint(
	t *testing.T,
) {
	harness := newLoopHarness(t)
	requestOne := newModelRequestID(t)
	requestTwo := newModelRequestID(t)
	var observed []ModelInput
	harness.model.steps = []modelStep{
		func(_ context.Context, input ModelInput) (ModelTurn, error) {
			observed = append(observed, input)
			if len(input.ApprovedTools) != 1 ||
				input.ApprovedTools[0].Name != string(executor.ToolApplyEdit) {
				t.Fatalf("approved tools = %#v", input.ApprovedTools)
			}
			var schema struct {
				AdditionalProperties bool `json:"additionalProperties"`
			}
			if err := json.Unmarshal(
				input.ApprovedTools[0].InputSchema,
				&schema,
			); err != nil {
				t.Fatal(err)
			}
			if schema.AdditionalProperties {
				t.Fatal("tool schema unexpectedly permits additional properties")
			}
			if len(input.RepositoryContext) != 1 ||
				len(input.FactualEvents) != 1 ||
				len(input.PreviousResults) != 0 {
				t.Fatalf("first model observation = %#v", input)
			}
			return successfulToolTurn(
				harness.model.identity,
				requestOne,
				"edit-1",
				executor.ToolApplyEdit,
				`{"path":"main.go","content":"sensitive replacement"}`,
				"implementation",
				false,
			), nil
		},
		func(_ context.Context, input ModelInput) (ModelTurn, error) {
			observed = append(observed, input)
			if len(input.PreviousResults) != 1 ||
				input.PreviousResults[0].CallID != "edit-1" ||
				len(input.PreviousResults[0].StdoutRedacted) >
					harness.input.Limits.MaximumResultBytes ||
				input.Plan.Steps[0].State != StepImplemented {
				t.Fatalf("second model observation = %#v", input)
			}
			return completionTurn(
				harness.model.identity,
				requestTwo,
				CompletionImplementationComplete,
			), nil
		},
	}
	harness.tools.result = executor.ToolResult{
		RequestID: "edit-1", SchemaVersion: executor.ToolSchemaVersion,
		State: "succeeded", ExitCode: 0, Summary: "edit applied",
		StdoutRedacted: strings.Repeat("x", 2048),
	}

	outcome, err := harness.loop.Run(context.Background(), harness.input)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Kind != OutcomeImplementationComplete ||
		!outcome.ValidationRequired ||
		outcome.Rounds != 2 || outcome.ToolCalls != 1 ||
		outcome.Plan.Steps[0].State != StepImplemented {
		t.Fatalf("outcome = %#v", outcome)
	}
	if len(observed) != 2 ||
		len(harness.journal.starts) != 1 ||
		len(harness.journal.results) != 1 ||
		len(harness.checkpoints.requests) != 1 {
		t.Fatalf(
			"observed=%d starts=%d results=%d checkpoints=%d",
			len(observed),
			len(harness.journal.starts),
			len(harness.journal.results),
			len(harness.checkpoints.requests),
		)
	}
	if harness.checkpoints.requests[0].Trigger !=
		CheckpointMaterialEdit {
		t.Fatalf(
			"material checkpoint = %#v",
			harness.checkpoints.requests[0],
		)
	}
	start := harness.journal.starts[0]
	if start.ModelRequestID != requestOne ||
		start.Authorization.DecisionID == "" ||
		start.Authorization.PolicyRevision != harness.input.PolicyRevision ||
		start.Authorization.PolicySHA256 != harness.input.PolicySHA256 ||
		strings.Contains(start.ArgumentsRedactedJSON, "sensitive replacement") ||
		!strings.Contains(start.ArgumentsRedactedJSON, "[REDACTED]") {
		t.Fatalf("tool start = %#v", start)
	}
	wantOrder := []string{
		"plan-approved-checkpoint",
		"authority", "start", "step:in-progress", "execute", "result",
		"checkpoint", "step:implemented",
	}
	if strings.Join(harness.order.values, ",") != strings.Join(wantOrder, ",") {
		t.Fatalf("execution order = %#v, want %#v", harness.order.values, wantOrder)
	}
}

func TestExecutionLoopCheckpointsBeforeRiskyApprovedAction(t *testing.T) {
	harness := newLoopHarness(t)
	harness.input.Plan.Steps = []PlanStep{{
		ID:                 "test",
		Kind:               StepKindTest,
		SummaryRedacted:    "Run the approved test recipe",
		State:              StepPending,
		ValidationRequired: true,
		CompletionTools:    []executor.ToolName{executor.ToolTest},
	}}
	harness.input.ApprovedTools = []ApprovedTool{approvedTestTool(t)}
	harness.model.steps = []modelStep{
		func(context.Context, ModelInput) (ModelTurn, error) {
			return successfulToolTurn(
				harness.model.identity,
				newModelRequestID(t),
				"test-risky",
				executor.ToolTest,
				`{"executable":"go","arg1":"test","arg2":"./..."}`,
				"test",
				false,
			), nil
		},
		func(context.Context, ModelInput) (ModelTurn, error) {
			return completionTurn(
				harness.model.identity,
				newModelRequestID(t),
				CompletionImplementationComplete,
			), nil
		},
	}
	harness.tools.result = executor.ToolResult{
		RequestID:     "test-risky",
		SchemaVersion: executor.ToolSchemaVersion,
		State:         "succeeded",
		Summary:       "tests passed",
	}

	if _, err := harness.loop.Run(t.Context(), harness.input); err != nil {
		t.Fatal(err)
	}
	if len(harness.checkpoints.requests) != 1 {
		t.Fatalf("risky checkpoints = %#v", harness.checkpoints.requests)
	}
	request := harness.checkpoints.requests[0]
	if request.Trigger != CheckpointBeforeRisky ||
		request.PermissionID != "permission-decision-1" ||
		!validSHA256(request.ActionSHA256) {
		t.Fatalf("risky checkpoint = %#v", request)
	}
	checkpointIndex := -1
	executeIndex := -1
	for index, event := range harness.order.values {
		switch event {
		case "checkpoint":
			checkpointIndex = index
		case "execute":
			executeIndex = index
		}
	}
	if checkpointIndex < 0 || executeIndex < 0 ||
		checkpointIndex >= executeIndex {
		t.Fatalf("risky action order = %v", harness.order.values)
	}
}

func TestExecutionLoopRejectsMalformedAndUnknownToolCallsBeforeAuthority(
	t *testing.T,
) {
	tests := []struct {
		name    string
		call    providers.ToolCall
		wantErr error
	}{
		{
			name: "duplicate argument",
			call: providers.ToolCall{
				ID: "call-1", Name: string(executor.ToolApplyEdit),
				Arguments: json.RawMessage(
					`{"path":"a.go","path":"b.go","content":"x"}`,
				),
			},
			wantErr: ErrMalformedModelTurn,
		},
		{
			name: "unknown tool",
			call: providers.ToolCall{
				ID: "call-1", Name: "unregistered-tool",
				Arguments: json.RawMessage(`{}`),
			},
			wantErr: ErrUnknownTool,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newLoopHarness(t)
			harness.model.steps = []modelStep{func(
				context.Context,
				ModelInput,
			) (ModelTurn, error) {
				return ModelTurn{
					ModelRequestID: newModelRequestID(t),
					Model:          harness.model.identity,
					ToolCalls: []ModelToolCall{{
						Call: test.call, PlanStepID: "implementation",
					}},
					Usage: knownUsage(), Cost: knownCost(t, 1, 100),
				}, nil
			}}

			_, err := harness.loop.Run(context.Background(), harness.input)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want %v", err, test.wantErr)
			}
			if harness.authority.calls != 0 ||
				len(harness.journal.starts) != 0 ||
				harness.tools.calls != 0 {
				t.Fatalf(
					"authority=%d starts=%d tools=%d, want zero",
					harness.authority.calls,
					len(harness.journal.starts),
					harness.tools.calls,
				)
			}
		})
	}
}

func TestClonePlanDoesNotAliasExpectedFileScope(t *testing.T) {
	original := PlanProjection{Steps: []PlanStep{{
		ExpectedFiles:   []string{"internal/agent"},
		CompletionTools: []executor.ToolName{executor.ToolReadFile},
	}}}
	cloned := clonePlan(original)
	cloned.Steps[0].ExpectedFiles[0] = "other"
	cloned.Steps[0].CompletionTools[0] = executor.ToolGitStatus
	if original.Steps[0].ExpectedFiles[0] != "internal/agent" ||
		original.Steps[0].CompletionTools[0] != executor.ToolReadFile {
		t.Fatalf("clone mutated original plan: %#v", original)
	}
}

func TestExecutionLoopRejectsModelOwnedCompletionAndIncompatibleStepTools(
	t *testing.T,
) {
	tests := []struct {
		name      string
		stepID    string
		tool      ApprovedTool
		toolName  executor.ToolName
		arguments string
	}{
		{
			name:   "material edit requires checkpoint-capable material tool",
			stepID: "implementation",
			tool: func() ApprovedTool {
				value := approvedApplyEditTool(t)
				value.CreatesCheckpoint = false
				return value
			}(),
			toolName:  executor.ToolApplyEdit,
			arguments: `{"path":"main.go","content":"untrusted"}`,
		},
		{
			name:   "read tool cannot complete a material step",
			stepID: "implementation", tool: approvedInspectDiffTool(t),
			toolName: executor.ToolInspectDiff, arguments: `{}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newLoopHarness(t)
			harness.input.ApprovedTools = []ApprovedTool{test.tool}
			harness.model.steps = []modelStep{func(
				context.Context,
				ModelInput,
			) (ModelTurn, error) {
				return successfulToolTurn(
					harness.model.identity,
					newModelRequestID(t),
					"untrusted-completion",
					test.toolName,
					test.arguments,
					test.stepID,
					true,
				), nil
			}}
			harness.rebuildLoop(t)

			_, err := harness.loop.Run(t.Context(), harness.input)
			if err == nil ||
				harness.authority.calls != 0 ||
				harness.tools.calls != 0 ||
				len(harness.plan.transitions) != 0 {
				t.Fatalf(
					"error=%v authority=%d tools=%d transitions=%#v",
					err,
					harness.authority.calls,
					harness.tools.calls,
					harness.plan.transitions,
				)
			}
		})
	}
}

func TestExecutionLoopRejectsUnrelatedReadToolBeforeAuthority(t *testing.T) {
	harness := newLoopHarness(t)
	harness.input.Plan.Steps[0].Kind = StepKindGitStatus
	harness.input.Plan.Steps[0].MaterialEdit = false
	harness.input.Plan.Steps[0].ValidationRequired = false
	harness.input.Plan.Steps[0].CompletionTools = []executor.ToolName{
		executor.ToolGitStatus,
	}
	harness.input.ApprovedTools = []ApprovedTool{approvedInspectDiffTool(t)}
	harness.model.steps = []modelStep{func(
		context.Context,
		ModelInput,
	) (ModelTurn, error) {
		return successfulToolTurn(
			harness.model.identity,
			newModelRequestID(t),
			"unrelated-read",
			executor.ToolInspectDiff,
			`{}`,
			"implementation",
			true,
		), nil
	}}
	harness.rebuildLoop(t)

	_, err := harness.loop.Run(t.Context(), harness.input)
	if !errors.Is(err, ErrMalformedModelTurn) ||
		harness.authority.calls != 0 ||
		harness.tools.calls != 0 ||
		len(harness.plan.transitions) != 0 {
		t.Fatalf(
			"error=%v authority=%d tools=%d transitions=%#v",
			err,
			harness.authority.calls,
			harness.tools.calls,
			harness.plan.transitions,
		)
	}
}

func TestExecutionLoopRejectsEditOutsideExpectedFilesBeforeAuthority(
	t *testing.T,
) {
	harness := newLoopHarness(t)
	outside := func(context.Context, ModelInput) (ModelTurn, error) {
		return successfulToolTurn(
			harness.model.identity,
			newModelRequestID(t),
			"cross-step-edit",
			executor.ToolApplyEdit,
			`{"path":"other.go","content":"package other"}`,
			"implementation",
			true,
		), nil
	}
	// Reaching outside the plan is answered, then answered again, and only then
	// does it end the attempt. The write never happens either way — what the
	// retries buy is the chance to aim the same change at a file the plan owns,
	// which on a generated workspace is nearly always what was meant: the stub
	// main.go in the opening context, mistaken for cmd/generated/main.go.
	harness.model.steps = []modelStep{outside, outside, outside}

	_, err := harness.loop.Run(t.Context(), harness.input)
	if !errors.Is(err, ErrMalformedModelTurn) ||
		harness.authority.calls != 0 ||
		harness.tools.calls != 0 ||
		len(harness.journal.starts) != 0 ||
		len(harness.plan.transitions) != 0 ||
		len(harness.checkpoints.requests) != 0 {
		t.Fatalf(
			"error=%v authority=%d tools=%d starts=%d transitions=%d checkpoints=%d",
			err,
			harness.authority.calls,
			harness.tools.calls,
			len(harness.journal.starts),
			len(harness.plan.transitions),
			len(harness.checkpoints.requests),
		)
	}
}

func TestPathScopedCompletionRequiresExactCanonicalExpectedFile(t *testing.T) {
	tests := []struct {
		name string
		kind StepKind
		tool executor.ToolName
		path string
	}{
		{
			name: "read file from another step",
			kind: StepKindReadFile, tool: executor.ToolReadFile,
			path: "other.go",
		},
		{
			name: "directory outside scope",
			kind: StepKindListDirectory, tool: executor.ToolListDirectory,
			path: "internal/other",
		},
		{
			name: "search parent traversal",
			kind: StepKindSearchText, tool: executor.ToolSearchText,
			path: "../internal",
		},
		{
			name: "symbol search noncanonical",
			kind: StepKindSearchSymbol, tool: executor.ToolSearchSymbol,
			path: "internal/./agent",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			step := PlanStep{
				ID: "scoped", Kind: test.kind,
				ExpectedFiles: []string{"internal/agent"},
			}
			request := executor.ToolRequest{
				Name: test.tool,
				Arguments: []executor.ToolArgument{{
					Name: "path", Value: test.path,
				}},
			}
			if err := validateToolStepPathScope(step, request); !errors.Is(
				err,
				ErrMalformedModelTurn,
			) {
				t.Fatalf("error = %v, want malformed model turn", err)
			}
		})
	}
	step := PlanStep{
		ID: "scoped", Kind: StepKindReadFile,
		ExpectedFiles: []string{"internal/agent"},
	}
	request := executor.ToolRequest{
		Name: executor.ToolReadFile,
		Arguments: []executor.ToolArgument{{
			Name: "path", Value: "internal/agent",
		}},
	}
	if err := validateToolStepPathScope(step, request); err != nil {
		t.Fatalf("exact expected path rejected: %v", err)
	}
	step.Kind = StepKindListDirectory
	request.Name = executor.ToolListDirectory
	request.Arguments[0].Value = "internal/agent/loop.go"
	if err := validateToolStepPathScope(step, request); err != nil {
		t.Fatalf("expected directory descendant rejected: %v", err)
	}
	request.Arguments[0].Value = "internal/agent-old/loop.go"
	if err := validateToolStepPathScope(step, request); !errors.Is(
		err,
		ErrMalformedModelTurn,
	) {
		t.Fatalf("sibling prefix error = %v, want malformed model turn", err)
	}
}

func TestExecutionLoopRejectsInvalidCompletionToolContracts(t *testing.T) {
	tests := []struct {
		name  string
		tools []executor.ToolName
	}{
		{name: "empty"},
		{
			name: "duplicate",
			tools: []executor.ToolName{
				executor.ToolApplyEdit,
				executor.ToolApplyEdit,
			},
		},
		{name: "unknown", tools: []executor.ToolName{"unknown-tool"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newLoopHarness(t)
			harness.input.Plan.Steps[0].CompletionTools = test.tools
			if _, err := harness.loop.Run(
				t.Context(),
				harness.input,
			); err == nil || harness.model.calls != 0 {
				t.Fatalf("error=%v model calls=%d", err, harness.model.calls)
			}
		})
	}
}

func TestExecutionLoopRejectsStepKindSemanticDrift(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*PlanStep)
	}{
		{
			name: "unknown kind",
			mutate: func(step *PlanStep) {
				step.Kind = "unknown"
			},
		},
		{
			name: "kind completion tool drift",
			mutate: func(step *PlanStep) {
				step.Kind = StepKindGitStatus
			},
		},
		{
			name: "materiality drift",
			mutate: func(step *PlanStep) {
				step.MaterialEdit = false
			},
		},
		{
			name: "validation requirement drift",
			mutate: func(step *PlanStep) {
				step.ValidationRequired = false
			},
		},
		{
			name: "missing expected files",
			mutate: func(step *PlanStep) {
				step.ExpectedFiles = nil
			},
		},
		{
			name: "absolute expected file",
			mutate: func(step *PlanStep) {
				step.ExpectedFiles = []string{`C:/other/main.go`}
			},
		},
		{
			name: "parent traversal expected file",
			mutate: func(step *PlanStep) {
				step.ExpectedFiles = []string{"../main.go"}
			},
		},
		{
			name: "noncanonical expected file",
			mutate: func(step *PlanStep) {
				step.ExpectedFiles = []string{"internal/../main.go"}
			},
		},
		{
			name: "unsorted expected files",
			mutate: func(step *PlanStep) {
				step.ExpectedFiles = []string{"z.go", "a.go"}
			},
		},
		{
			name: "duplicate expected file",
			mutate: func(step *PlanStep) {
				step.ExpectedFiles = []string{"main.go", "main.go"}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newLoopHarness(t)
			test.mutate(&harness.input.Plan.Steps[0])
			if _, err := harness.loop.Run(
				t.Context(),
				harness.input,
			); err == nil || harness.model.calls != 0 {
				t.Fatalf("error=%v model calls=%d", err, harness.model.calls)
			}
		})
	}
}

func TestExecutionLoopExecutionErrorOverridesContradictoryResult(t *testing.T) {
	tests := []struct {
		name          string
		returnedState string
		executionErr  error
		wantState     string
		wantCancelled bool
		wantTimedOut  bool
		wantSummary   string
		wantOutcome   OutcomeKind
	}{
		{
			name:          "success plus error is unknown",
			returnedState: "succeeded",
			executionErr:  errors.New("transport disappeared"),
			wantState:     "outcome-unknown",
			wantOutcome:   OutcomeAwaitingDirection,
		},
		{
			name:          "failure plus error is unknown",
			returnedState: "failed",
			executionErr:  errors.New("boundary failed"),
			wantState:     "outcome-unknown",
			wantOutcome:   OutcomeAwaitingDirection,
		},
		{
			name:          "success plus cancellation is cancelled",
			returnedState: "succeeded",
			executionErr:  context.Canceled,
			wantState:     "cancelled", wantCancelled: true,
			wantOutcome: OutcomeCancelled,
		},
		{
			name:          "failure plus deadline is cancelled",
			returnedState: "failed",
			executionErr:  context.DeadlineExceeded,
			wantState:     "cancelled", wantTimedOut: true,
			wantOutcome: OutcomeCancelled,
		},
		{
			name:          "explicit trustworthy unknown is preserved",
			returnedState: "outcome-unknown",
			executionErr:  errors.New("ambiguous boundary"),
			wantState:     "outcome-unknown",
			wantSummary:   "executor retained explicit uncertainty",
			wantOutcome:   OutcomeAwaitingDirection,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newLoopHarness(t)
			harness.model.steps = []modelStep{
				func(context.Context, ModelInput) (ModelTurn, error) {
					return successfulToolTurn(
						harness.model.identity,
						newModelRequestID(t),
						"contradictory-edit",
						executor.ToolApplyEdit,
						`{"path":"main.go","content":"partial"}`,
						"implementation",
						true,
					), nil
				},
				func(context.Context, ModelInput) (ModelTurn, error) {
					return completionTurn(
						harness.model.identity,
						newModelRequestID(t),
						CompletionNeedsDirection,
					), nil
				},
			}
			harness.tools.execute = func(
				_ context.Context,
				request executor.ToolRequest,
			) (executor.ToolResult, error) {
				return executor.ToolResult{
					RequestID:     request.ID,
					SchemaVersion: executor.ToolSchemaVersion,
					State:         test.returnedState, ExitCode: 0,
					Summary: test.wantSummary,
				}, test.executionErr
			}
			harness.rebuildLoop(t)

			outcome, err := harness.loop.Run(t.Context(), harness.input)
			if err != nil {
				t.Fatal(err)
			}
			result := harness.journal.results[0].Result
			if outcome.Kind != test.wantOutcome ||
				result.State != test.wantState ||
				result.Cancelled != test.wantCancelled ||
				result.TimedOut != test.wantTimedOut ||
				test.wantSummary != "" &&
					result.Summary != test.wantSummary ||
				len(harness.checkpoints.requests) != 0 ||
				outcome.Plan.Steps[0].State != StepInProgress {
				t.Fatalf(
					"outcome=%#v result=%#v checkpoints=%#v",
					outcome,
					result,
					harness.checkpoints.requests,
				)
			}
		})
	}
}

func TestExecutionLoopCannotMarkValidationRequiredStepValidated(
	t *testing.T,
) {
	harness := newLoopHarness(t)
	harness.model.steps = []modelStep{
		func(context.Context, ModelInput) (ModelTurn, error) {
			return successfulToolTurn(
				harness.model.identity,
				newModelRequestID(t),
				"implementation-only",
				executor.ToolApplyEdit,
				`{"path":"main.go","content":"implemented"}`,
				"implementation",
				true,
			), nil
		},
		func(context.Context, ModelInput) (ModelTurn, error) {
			return completionTurn(
				harness.model.identity,
				newModelRequestID(t),
				CompletionValidationComplete,
			), nil
		},
	}

	outcome, err := harness.loop.Run(t.Context(), harness.input)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Kind != OutcomeImplementationComplete ||
		!outcome.ValidationRequired ||
		outcome.Plan.Steps[0].State != StepImplemented {
		t.Fatalf("outcome = %#v", outcome)
	}
	for _, transition := range harness.plan.transitions {
		if transition.To == StepValidated {
			t.Fatalf("model-owned validation transition = %#v", transition)
		}
	}
}

func TestExecutionLoopRejectsLimitsAboveAbsoluteCeilings(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*LoopLimits)
	}{
		{
			name: "rounds",
			mutate: func(value *LoopLimits) {
				value.MaximumRounds = absoluteMaximumRounds + 1
			},
		},
		{
			name: "tool calls",
			mutate: func(value *LoopLimits) {
				value.MaximumToolCalls = absoluteMaximumToolCalls + 1
			},
		},
		{
			name: "tokens",
			mutate: func(value *LoopLimits) {
				value.MaximumTokens = absoluteMaximumTokens + 1
			},
		},
		{
			name: "context bytes",
			mutate: func(value *LoopLimits) {
				value.MaximumContextBytes = absoluteMaximumContextBytes + 1
			},
		},
		{
			name: "cost",
			mutate: func(value *LoopLimits) {
				value.MaximumCost = knownCost(
					t,
					absoluteMaximumCostUnits+1,
					1,
				)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newLoopHarness(t)
			test.mutate(&harness.input.Limits)
			if _, err := harness.loop.Run(
				t.Context(),
				harness.input,
			); err == nil || harness.model.calls != 0 {
				t.Fatalf("error=%v model calls=%d", err, harness.model.calls)
			}
		})
	}
}

func TestExecutionLoopRoutesApprovalAndDenialWithoutExecuting(t *testing.T) {
	tests := []struct {
		name    string
		policy  executor.PolicyOutcome
		outcome OutcomeKind
	}{
		{
			name: "approval", policy: executor.OutcomeApprovalRequired,
			outcome: OutcomeAwaitingApproval,
		},
		{
			name: "denial", policy: executor.OutcomeDenied,
			outcome: OutcomePermissionDenied,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newLoopHarness(t)
			harness.authority.outcome = test.policy
			harness.model.steps = []modelStep{func(
				context.Context,
				ModelInput,
			) (ModelTurn, error) {
				return successfulToolTurn(
					harness.model.identity,
					newModelRequestID(t),
					"edit-approval",
					executor.ToolApplyEdit,
					`{"path":"main.go","content":"x"}`,
					"implementation",
					false,
				), nil
			}}

			outcome, err := harness.loop.Run(
				context.Background(),
				harness.input,
			)
			if err != nil {
				t.Fatal(err)
			}
			if outcome.Kind != test.outcome ||
				len(harness.journal.starts) != 0 ||
				harness.tools.calls != 0 {
				t.Fatalf(
					"outcome=%#v starts=%d tools=%d",
					outcome,
					len(harness.journal.starts),
					harness.tools.calls,
				)
			}
		})
	}
}

func TestExecutionLoopStopsAfterRepeatedIdenticalFailedAction(t *testing.T) {
	harness := newLoopHarness(t)
	for index := 0; index < 2; index++ {
		callID := "failed-" + string(rune('1'+index))
		requestID := newModelRequestID(t)
		harness.model.steps = append(
			harness.model.steps,
			func(_ context.Context, input ModelInput) (ModelTurn, error) {
				if input.Round == 2 &&
					(len(input.PreviousResults) != 1 ||
						!input.PreviousResults[0].IsError) {
					t.Fatalf("failure feedback = %#v", input.PreviousResults)
				}
				return successfulToolTurn(
					harness.model.identity,
					requestID,
					callID,
					executor.ToolApplyEdit,
					`{"path":"main.go","content":"same edit"}`,
					"implementation",
					false,
				), nil
			},
		)
	}
	harness.tools.resultFor = func(request executor.ToolRequest) executor.ToolResult {
		return executor.ToolResult{
			RequestID:     request.ID,
			SchemaVersion: executor.ToolSchemaVersion,
			State:         "failed", ExitCode: 1, Summary: "edit conflict",
		}
	}

	outcome, err := harness.loop.Run(context.Background(), harness.input)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Kind != OutcomeAwaitingDirection ||
		outcome.ToolCalls != 2 ||
		len(harness.journal.results) != 2 {
		t.Fatalf("outcome = %#v", outcome)
	}
	// The step is not written off for this.
	//
	// Stopping is right — a call that has failed twice will not work a third
	// time — and the outcome above says so. Recording the step as failed is a
	// different thing, and it outlives the attempt: the coordinator rebuilds
	// the plan for every attempt but the durable step record persists, and
	// nothing can move a step out of failed, so completion evidence can never
	// attribute to it again. A run could then fix the work, satisfy every gate,
	// and still be unable to record that it had.
	//
	// Ladder rung 4 on 2026-08-03 ended precisely there: "the program is
	// correct and the pipeline did not converge", with the reason four lines
	// below it — "validate attributed plan step: plan step \"step-001\" is
	// failed, not implemented". Three failed patches in one early attempt had
	// closed the door on the other five.
	if outcome.Plan.Steps[0].State == StepFailed {
		t.Error("the step was recorded as failed, which no later attempt can " +
			"undo and which blocks completion for the rest of the run")
	}
}

func TestExecutionLoopDurablePauseInterruptsToolAction(t *testing.T) {
	harness := newLoopHarness(t)
	harness.model.steps = []modelStep{func(
		context.Context,
		ModelInput,
	) (ModelTurn, error) {
		return successfulToolTurn(
			harness.model.identity,
			newModelRequestID(t),
			"pause-edit",
			executor.ToolApplyEdit,
			`{"path":"main.go","content":"partial"}`,
			"implementation",
			false,
		), nil
	}}
	started := make(chan struct{})
	harness.tools.execute = func(
		ctx context.Context,
		request executor.ToolRequest,
	) (executor.ToolResult, error) {
		close(started)
		<-ctx.Done()
		return executor.ToolResult{}, ctx.Err()
	}
	harness.interrupts = &interruptBridgeStub{
		triggerBinding: 2,
		onTrigger: func(cancel context.CancelFunc) {
			<-started
			harness.control.set(ControlState{
				Disposition:     ControlPaused,
				BudgetAvailable: true, PolicyCurrent: true,
			})
			cancel()
		},
	}
	harness.rebuildLoop(t)

	outcome, err := harness.loop.Run(context.Background(), harness.input)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Kind != OutcomePaused ||
		len(harness.journal.results) != 1 ||
		harness.journal.results[0].Result.State != "cancelled" ||
		!harness.journal.results[0].Result.Cancelled {
		t.Fatalf(
			"outcome=%#v results=%#v",
			outcome,
			harness.journal.results,
		)
	}
}

func TestExecutionLoopDurableCancelInterruptsModelStreaming(t *testing.T) {
	harness := newLoopHarness(t)
	started := make(chan struct{})
	harness.model.steps = []modelStep{func(
		ctx context.Context,
		_ ModelInput,
	) (ModelTurn, error) {
		close(started)
		<-ctx.Done()
		return ModelTurn{}, ctx.Err()
	}}
	harness.interrupts = &interruptBridgeStub{
		triggerBinding: 1,
		onTrigger: func(cancel context.CancelFunc) {
			<-started
			harness.control.set(ControlState{
				Disposition:     ControlCancelled,
				BudgetAvailable: true, PolicyCurrent: true,
			})
			cancel()
		},
	}
	harness.rebuildLoop(t)

	outcome, err := harness.loop.Run(context.Background(), harness.input)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Kind != OutcomeCancelled ||
		len(harness.journal.starts) != 0 {
		t.Fatalf("outcome=%#v starts=%d", outcome, len(harness.journal.starts))
	}
}

func TestExecutionLoopRechecksControlBeforeCompletion(t *testing.T) {
	harness := newLoopHarness(t)
	harness.model.steps = []modelStep{func(
		context.Context,
		ModelInput,
	) (ModelTurn, error) {
		harness.control.set(ControlState{
			Disposition:     ControlPaused,
			BudgetAvailable: true, PolicyCurrent: true,
		})
		return completionTurn(
			harness.model.identity,
			newModelRequestID(t),
			CompletionImplementationComplete,
		), nil
	}}

	outcome, err := harness.loop.Run(context.Background(), harness.input)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Kind != OutcomePaused {
		t.Fatalf("outcome = %#v, want paused", outcome)
	}
}

func TestExecutionLoopDistinguishesValidationCompletion(t *testing.T) {
	harness := newLoopHarness(t)
	harness.input.Plan.Steps[0].State = StepValidated
	harness.control.set(ControlState{
		Disposition:     ControlActive,
		BudgetAvailable: true, PolicyCurrent: true,
		ValidationComplete: true,
	})
	harness.model.steps = []modelStep{func(
		context.Context,
		ModelInput,
	) (ModelTurn, error) {
		return completionTurn(
			harness.model.identity,
			newModelRequestID(t),
			CompletionValidationComplete,
		), nil
	}}

	outcome, err := harness.loop.Run(context.Background(), harness.input)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Kind != OutcomeValidationComplete ||
		outcome.ValidationRequired {
		t.Fatalf("outcome = %#v", outcome)
	}
}

func TestExecutionLoopStopsForPrematureCompletionClaim(t *testing.T) {
	harness := newLoopHarness(t)
	harness.model.steps = []modelStep{func(
		context.Context,
		ModelInput,
	) (ModelTurn, error) {
		return completionTurn(
			harness.model.identity,
			newModelRequestID(t),
			CompletionImplementationComplete,
		), nil
	}}

	outcome, err := harness.loop.Run(context.Background(), harness.input)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Kind != OutcomeAwaitingDirection ||
		!outcome.ValidationRequired {
		t.Fatalf("outcome = %#v", outcome)
	}
}

func TestExecutionLoopEnforcesRoundTokenTimeAndCostLimits(t *testing.T) {
	t.Run("round", func(t *testing.T) {
		harness := newLoopHarness(t)
		harness.input.Limits.MaximumRounds = 1
		harness.model.steps = []modelStep{func(
			context.Context,
			ModelInput,
		) (ModelTurn, error) {
			return successfulToolTurn(
				harness.model.identity,
				newModelRequestID(t),
				"round-read",
				executor.ToolApplyEdit,
				`{"path":"main.go","content":"x"}`,
				"implementation",
				false,
			), nil
		}}
		outcome, err := harness.loop.Run(context.Background(), harness.input)
		if err != nil || outcome.Kind != OutcomeLimitReached {
			t.Fatalf("outcome=%#v error=%v", outcome, err)
		}
	})
	t.Run("tokens", func(t *testing.T) {
		harness := newLoopHarness(t)
		harness.input.Limits.MaximumTokensPerRound = 1
		harness.model.steps = []modelStep{func(
			context.Context,
			ModelInput,
		) (ModelTurn, error) {
			turn := completionTurn(
				harness.model.identity,
				newModelRequestID(t),
				CompletionImplementationComplete,
			)
			turn.Usage.InputTokens = 2
			return turn, nil
		}}
		outcome, err := harness.loop.Run(context.Background(), harness.input)
		if err != nil || outcome.Kind != OutcomeLimitReached {
			t.Fatalf("outcome=%#v error=%v", outcome, err)
		}
	})
	t.Run("per-round tool calls", func(t *testing.T) {
		harness := newLoopHarness(t)
		harness.input.Limits.MaximumToolCallsPerRound = 1
		harness.model.steps = []modelStep{func(
			context.Context,
			ModelInput,
		) (ModelTurn, error) {
			turn := successfulToolTurn(
				harness.model.identity,
				newModelRequestID(t),
				"first-call",
				executor.ToolApplyEdit,
				`{"path":"main.go","content":"x"}`,
				"implementation",
				false,
			)
			second := turn.ToolCalls[0]
			second.Call.ID = "second-call"
			turn.ToolCalls = append(turn.ToolCalls, second)
			return turn, nil
		}}
		_, err := harness.loop.Run(context.Background(), harness.input)
		if !errors.Is(err, ErrMalformedModelTurn) ||
			harness.authority.calls != 0 ||
			harness.tools.calls != 0 {
			t.Fatalf(
				"error=%v authority=%d tools=%d",
				err,
				harness.authority.calls,
				harness.tools.calls,
			)
		}
	})
	t.Run("cumulative tool calls", func(t *testing.T) {
		harness := newLoopHarness(t)
		harness.input.Limits.MaximumToolCalls = 1
		harness.input.Limits.MaximumToolCallsPerRound = 1
		harness.model.steps = []modelStep{
			func(context.Context, ModelInput) (ModelTurn, error) {
				return successfulToolTurn(
					harness.model.identity,
					newModelRequestID(t),
					"first-call",
					executor.ToolApplyEdit,
					`{"path":"main.go","content":"x"}`,
					"implementation",
					false,
				), nil
			},
			func(context.Context, ModelInput) (ModelTurn, error) {
				return successfulToolTurn(
					harness.model.identity,
					newModelRequestID(t),
					"second-call",
					executor.ToolApplyEdit,
					`{"path":"main.go","content":"y"}`,
					"implementation",
					false,
				), nil
			},
		}
		outcome, err := harness.loop.Run(context.Background(), harness.input)
		if err != nil ||
			outcome.Kind != OutcomeLimitReached ||
			harness.tools.calls != 1 {
			t.Fatalf(
				"outcome=%#v error=%v tools=%d",
				outcome,
				err,
				harness.tools.calls,
			)
		}
	})
	t.Run("cost", func(t *testing.T) {
		harness := newLoopHarness(t)
		harness.input.Limits.MaximumCost = knownCost(t, 1, 100)
		harness.model.steps = []modelStep{func(
			context.Context,
			ModelInput,
		) (ModelTurn, error) {
			turn := completionTurn(
				harness.model.identity,
				newModelRequestID(t),
				CompletionImplementationComplete,
			)
			turn.Cost = knownCost(t, 2, 100)
			return turn, nil
		}}
		outcome, err := harness.loop.Run(context.Background(), harness.input)
		if err != nil || outcome.Kind != OutcomeLimitReached {
			t.Fatalf("outcome=%#v error=%v", outcome, err)
		}
	})
	t.Run("time", func(t *testing.T) {
		harness := newLoopHarness(t)
		harness.input.Limits.MaximumWallClock = time.Second
		base := time.Now().UTC()
		calls := 0
		harness.now = func() time.Time {
			calls++
			if calls == 1 {
				return base
			}
			return base.Add(2 * time.Second)
		}
		harness.rebuildLoop(t)
		outcome, err := harness.loop.Run(context.Background(), harness.input)
		if err != nil || outcome.Kind != OutcomeLimitReached ||
			harness.model.calls != 0 {
			t.Fatalf(
				"outcome=%#v error=%v model-calls=%d",
				outcome,
				err,
				harness.model.calls,
			)
		}
	})
}

func TestExecutionLoopStopsOnBudgetAndPolicyControl(t *testing.T) {
	tests := []struct {
		name    string
		control ControlState
		outcome OutcomeKind
	}{
		{
			name: "budget",
			control: ControlState{
				Disposition:     ControlActive,
				BudgetAvailable: false, PolicyCurrent: true,
			},
			outcome: OutcomeBudgetExhausted,
		},
		{
			name: "policy",
			control: ControlState{
				Disposition:     ControlActive,
				BudgetAvailable: true, PolicyCurrent: false,
			},
			outcome: OutcomePolicyBlocked,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newLoopHarness(t)
			harness.control.set(test.control)
			outcome, err := harness.loop.Run(
				context.Background(),
				harness.input,
			)
			if err != nil || outcome.Kind != test.outcome ||
				harness.model.calls != 0 {
				t.Fatalf(
					"outcome=%#v error=%v model-calls=%d",
					outcome,
					err,
					harness.model.calls,
				)
			}
		})
	}
}

type loopHarness struct {
	loop        *ExecutionLoop
	input       LoopInput
	model       *scriptedModel
	authority   *authorityStub
	tools       *toolExecutorStub
	journal     *journalStub
	plan        *planStepStoreStub
	checkpoints *checkpointStoreStub
	control     *controlReaderStub
	interrupts  ControlInterruptBridge
	order       *orderedEvents
	now         func() time.Time
}

func newLoopHarness(t *testing.T) *loopHarness {
	t.Helper()
	taskID, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	runID, err := domain.NewRunID()
	if err != nil {
		t.Fatal(err)
	}
	approvalID, err := domain.NewApprovalID()
	if err != nil {
		t.Fatal(err)
	}
	model := providers.ModelIdentity{
		Provider: providers.ProviderIdentity{
			Adapter: "fake", AdapterVersion: "1",
			Provider: "fake", ProviderVersion: "1",
		},
		Model: "fixed-model", Revision: "fixed-revision",
	}
	order := &orderedEvents{}
	control := &controlReaderStub{}
	control.set(ControlState{
		Disposition:     ControlActive,
		BudgetAvailable: true, PolicyCurrent: true,
	})
	harness := &loopHarness{
		model: &scriptedModel{identity: model},
		authority: &authorityStub{
			outcome:        executor.OutcomeTaskScoped,
			policyRevision: 3,
			policySHA256:   strings.Repeat("b", 64),
			order:          order,
		},
		tools:       &toolExecutorStub{order: order},
		journal:     &journalStub{order: order},
		plan:        &planStepStoreStub{order: order},
		checkpoints: &checkpointStoreStub{order: order},
		control:     control,
		interrupts:  passthroughInterruptBridge{},
		order:       order,
		now:         time.Now,
		input: LoopInput{
			TaskID: taskID, RunID: runID,
			PlanApprovalID: approvalID,
			WorktreePath:   filepath.Join(t.TempDir(), "worktree"),
			PolicyRevision: 3,
			PolicySHA256:   strings.Repeat("b", 64),
			Plan: PlanProjection{
				Revision: 5, RepositoryRevision: "git-revision",
				Steps: []PlanStep{
					{
						ID:              "implementation",
						Kind:            StepKindEdit,
						SummaryRedacted: "Apply the scoped implementation",
						State:           StepPending, MaterialEdit: true,
						ValidationRequired: true,
						ExpectedFiles:      []string{"main.go"},
						CompletionTools: []executor.ToolName{
							executor.ToolApplyEdit,
						},
					},
				},
			},
			RepositoryContext: []RepositoryContextItem{{
				Path: "main.go", ContentSHA256: strings.Repeat("a", 64),
				ContentRedacted: "package main",
			}},
			FactualEvents: []FactualEvent{{
				Sequence: 1, Type: "task.started",
				SummaryRedacted: "task execution started",
			}},
			ApprovedTools: []ApprovedTool{approvedApplyEditTool(t)},
			Limits: LoopLimits{
				MaximumRounds:            5,
				MaximumToolCalls:         5,
				MaximumToolCallsPerRound: 2,
				MaximumTokens:            1000,
				MaximumTokensPerRound:    500,
				MaximumWallClock:         time.Minute,
				MaximumCost:              knownCost(t, 10, 1),
				MaximumIdenticalFailures: 2,
				MaximumContextItems:      5,
				MaximumFactualEvents:     5,
				MaximumContextBytes:      4096,
				MaximumResultBytes:       256,
			},
		},
	}
	harness.rebuildLoop(t)
	return harness
}

func (harness *loopHarness) rebuildLoop(t *testing.T) {
	t.Helper()
	loop, err := NewExecutionLoop(LoopDependencies{
		Model: harness.model, Authority: harness.authority,
		Tools: harness.tools, Journal: harness.journal,
		PlanSteps: harness.plan, Checkpoints: harness.checkpoints,
		PlanApprovalCheckpoints: harness.checkpoints,
		Control:                 harness.control, Interrupts: harness.interrupts,
		Now: harness.now,
	})
	if err != nil {
		t.Fatal(err)
	}
	harness.loop = loop
}

func approvedApplyEditTool(t *testing.T) ApprovedTool {
	t.Helper()
	var descriptor executor.ToolDescriptor
	for _, candidate := range executor.ToolCatalog() {
		if candidate.Name == executor.ToolApplyEdit {
			descriptor = candidate
			break
		}
	}
	if descriptor.Name == "" {
		t.Fatal("apply-edit descriptor is unavailable")
	}
	return ApprovedTool{
		Descriptor: descriptor,
		Arguments: []ToolArgumentDefinition{
			{Name: "path", Required: true, MaxBytes: 4096},
			{
				Name: "content", Required: true,
				Sensitive: true, MaxBytes: 32 << 10,
			},
		},
		DefaultTimeout: time.Minute,
		MaterialEdit:   true, CreatesCheckpoint: true,
	}
}

func approvedInspectDiffTool(t *testing.T) ApprovedTool {
	t.Helper()
	var descriptor executor.ToolDescriptor
	for _, candidate := range executor.ToolCatalog() {
		if candidate.Name == executor.ToolInspectDiff {
			descriptor = candidate
			break
		}
	}
	if descriptor.Name == "" {
		t.Fatal("inspect-diff descriptor is unavailable")
	}
	return ApprovedTool{
		Descriptor: descriptor, DefaultTimeout: time.Minute,
	}
}

func approvedTestTool(t *testing.T) ApprovedTool {
	t.Helper()
	var descriptor executor.ToolDescriptor
	for _, candidate := range executor.ToolCatalog() {
		if candidate.Name == executor.ToolTest {
			descriptor = candidate
			break
		}
	}
	if descriptor.Name == "" {
		t.Fatal("test descriptor is unavailable")
	}
	return ApprovedTool{
		Descriptor: descriptor,
		Arguments: []ToolArgumentDefinition{
			{Name: "executable", Required: true},
			{Name: "arg1", Required: true},
			{Name: "arg2", Required: true},
		},
		DefaultTimeout: time.Minute,
	}
}

type modelStep func(context.Context, ModelInput) (ModelTurn, error)

type scriptedModel struct {
	identity providers.ModelIdentity
	steps    []modelStep
	calls    int
}

func (model *scriptedModel) Identity() providers.ModelIdentity {
	return model.identity
}

func (model *scriptedModel) ObserveThink(
	ctx context.Context,
	input ModelInput,
) (ModelTurn, error) {
	if model.calls >= len(model.steps) {
		return ModelTurn{}, errors.New("unexpected model turn")
	}
	step := model.steps[model.calls]
	model.calls++
	return step(ctx, input)
}

type authorityStub struct {
	outcome        executor.PolicyOutcome
	policyRevision uint64
	policySHA256   string
	calls          int
	order          *orderedEvents
}

func (stub *authorityStub) RouteTool(
	_ context.Context,
	request executor.ToolRequest,
) (ToolAuthorization, error) {
	stub.calls++
	stub.order.add("authority")
	classification := executor.AuthorityClassification{
		Outcome:     stub.outcome,
		Required:    request.ClaimedAuthority,
		Capability:  request.ClaimedAuthority,
		ScopeHash:   executor.ActionSHA256(request),
		Description: executor.UserReadableToolSummary(request),
	}
	if stub.outcome == executor.OutcomeTaskScoped {
		classification.MatchedGrantID = "grant-1"
	}
	return ToolAuthorization{
		Classification: classification,
		DecisionID:     "permission-decision-1",
		PolicyRevision: stub.policyRevision,
		PolicySHA256:   stub.policySHA256,
	}, nil
}

type toolExecutorStub struct {
	result    executor.ToolResult
	resultFor func(executor.ToolRequest) executor.ToolResult
	execute   func(context.Context, executor.ToolRequest) (executor.ToolResult, error)
	calls     int
	order     *orderedEvents
}

func (stub *toolExecutorStub) ExecuteTool(
	ctx context.Context,
	authorized executor.AuthorizedToolRequest,
) (executor.ToolResult, error) {
	stub.calls++
	stub.order.add("execute")
	if stub.execute != nil {
		return stub.execute(ctx, authorized.Request)
	}
	if stub.resultFor != nil {
		return stub.resultFor(authorized.Request), nil
	}
	result := stub.result
	if result.RequestID == "" {
		result = executor.ToolResult{
			RequestID:     authorized.Request.ID,
			SchemaVersion: executor.ToolSchemaVersion,
			State:         "succeeded", ExitCode: 0, Summary: "tool succeeded",
		}
	}
	return result, nil
}

type journalStub struct {
	starts  []ToolStartRecord
	results []ToolResultRecord
	order   *orderedEvents
}

func (stub *journalStub) PersistToolStart(
	_ context.Context,
	record ToolStartRecord,
) error {
	stub.starts = append(stub.starts, record)
	stub.order.add("start")
	return nil
}

func (stub *journalStub) PersistToolResult(
	_ context.Context,
	record ToolResultRecord,
) error {
	stub.results = append(stub.results, record)
	stub.order.add("result")
	return nil
}

type planStepStoreStub struct {
	transitions []PlanStepTransition
	order       *orderedEvents
}

func (stub *planStepStoreStub) PersistPlanStepTransition(
	_ context.Context,
	transition PlanStepTransition,
) error {
	stub.transitions = append(stub.transitions, transition)
	stub.order.add("step:" + string(transition.To))
	return nil
}

type checkpointStoreStub struct {
	requests []CheckpointRequest
	order    *orderedEvents
}

func (stub *checkpointStoreStub) CreateCheckpoint(
	_ context.Context,
	request CheckpointRequest,
) error {
	stub.requests = append(stub.requests, request)
	stub.order.add("checkpoint")
	return nil
}

func (stub *checkpointStoreStub) CreatePlanApprovedCheckpoint(
	_ context.Context,
	_ PlanApprovedCheckpointRequest,
) error {
	stub.order.add("plan-approved-checkpoint")
	return nil
}

type controlReaderStub struct {
	mu    sync.Mutex
	state ControlState
}

func (stub *controlReaderStub) set(state ControlState) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	stub.state = state
}

func (stub *controlReaderStub) ReadControl(
	context.Context,
	domain.TaskID,
	domain.RunID,
) (ControlState, error) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	return stub.state, nil
}

type passthroughInterruptBridge struct{}

func (passthroughInterruptBridge) BindActionContext(
	parent context.Context,
	_ domain.TaskID,
	_ domain.RunID,
	_ ActionDescriptor,
) (context.Context, context.CancelFunc, error) {
	ctx, cancel := context.WithCancel(parent)
	return ctx, cancel, nil
}

type interruptBridgeStub struct {
	mu             sync.Mutex
	bindings       int
	triggerBinding int
	onTrigger      func(context.CancelFunc)
}

func (stub *interruptBridgeStub) BindActionContext(
	parent context.Context,
	_ domain.TaskID,
	_ domain.RunID,
	_ ActionDescriptor,
) (context.Context, context.CancelFunc, error) {
	ctx, cancel := context.WithCancel(parent)
	stub.mu.Lock()
	stub.bindings++
	binding := stub.bindings
	stub.mu.Unlock()
	if binding == stub.triggerBinding {
		go stub.onTrigger(cancel)
	}
	return ctx, cancel, nil
}

type orderedEvents struct {
	values []string
}

func (events *orderedEvents) add(value string) {
	events.values = append(events.values, value)
}

func successfulToolTurn(
	model providers.ModelIdentity,
	requestID domain.ModelRequestID,
	callID string,
	tool executor.ToolName,
	arguments string,
	stepID string,
	completes bool,
) ModelTurn {
	return ModelTurn{
		ModelRequestID: requestID,
		Model:          model,
		ToolCalls: []ModelToolCall{{
			Call: providers.ToolCall{
				ID: callID, Name: string(tool),
				Arguments: json.RawMessage(arguments),
			},
			PlanStepID: stepID, CompletesStep: completes,
		}},
		Usage: knownUsage(),
		Cost: providers.ExactAmount{
			Currency: "USD", Numerator: 1, Denominator: 100, Known: true,
		},
	}
}

func completionTurn(
	model providers.ModelIdentity,
	requestID domain.ModelRequestID,
	completion CompletionSignal,
) ModelTurn {
	return ModelTurn{
		ModelRequestID: requestID,
		Model:          model, Completion: completion,
		Usage: knownUsage(),
		Cost: providers.ExactAmount{
			Currency: "USD", Numerator: 1, Denominator: 100, Known: true,
		},
	}
}

func knownUsage() providers.Usage {
	return providers.Usage{
		Known: true, Source: providers.UsageSourceProvider,
		InputTokens: 1, OutputTokens: 1,
	}
}

func knownCost(t *testing.T, numerator, denominator int64) providers.ExactAmount {
	t.Helper()
	value, err := providers.NewExactAmount("USD", numerator, denominator)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func newModelRequestID(t *testing.T) domain.ModelRequestID {
	t.Helper()
	value, err := domain.NewModelRequestID()
	if err != nil {
		t.Fatal(err)
	}
	return value
}
