package agent

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/executor"
	"codeflux.dev/codeflux/internal/redact"
)

func TestValidationProfileBindsRequiredAcceptanceCommands(t *testing.T) {
	taskID := repairTaskID(t, 1)
	runID := repairRunID(t, 2)
	profile := repairValidationProfile(taskID, runID)
	if err := profile.Validate(taskID, runID); err != nil {
		t.Fatal(err)
	}
	first, err := profile.Digest()
	if err != nil {
		t.Fatal(err)
	}
	changed := profile
	changed.Commands = append([]ValidationCommand(nil), profile.Commands...)
	changed.Commands[0].Request.Arguments = append(
		[]executor.ToolArgument(nil),
		profile.Commands[0].Request.Arguments...,
	)
	changed.Commands[0].Request.Arguments[1].Value = "./internal/agent"
	changed.Commands[0].PlanCommand = mustRenderValidationPlanCommand(
		changed.Commands[0].Request,
	)
	second, err := changed.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("changed executable action retained validation profile digest")
	}
	for name, mutate := range map[string]func(*ValidationCommand){
		"relevant changed files": func(command *ValidationCommand) {
			command.RelevantChangedFiles = []string{
				"internal/agent/loop.go",
			}
		},
	} {
		t.Run(name+" changes digest", func(t *testing.T) {
			changed := profile
			changed.Commands = append(
				[]ValidationCommand(nil),
				profile.Commands...,
			)
			mutate(&changed.Commands[0])
			digest, err := changed.Digest()
			if err != nil {
				t.Fatal(err)
			}
			if digest == first {
				t.Fatalf("%s retained validation profile digest", name)
			}
		})
	}
	for name, mutate := range map[string]func(*ValidationCommand){
		"strong label with weaker arguments": func(command *ValidationCommand) {
			command.Request.Arguments[2].Value = "./internal/agent"
		},
		"strong label with different tool": func(command *ValidationCommand) {
			command.Request.Name = executor.ToolBuild
		},
		"sensitive argument": func(command *ValidationCommand) {
			command.Request.Arguments[2].Sensitive = true
		},
	} {
		t.Run(name+" is rejected", func(t *testing.T) {
			invalid := profile
			invalid.Commands = append(
				[]ValidationCommand(nil),
				profile.Commands...,
			)
			invalid.Commands[0].Request.Arguments = append(
				[]executor.ToolArgument(nil),
				profile.Commands[0].Request.Arguments...,
			)
			mutate(&invalid.Commands[0])
			if _, err := invalid.Digest(); !errors.Is(
				err,
				ErrInvalidValidationProfile,
			) {
				t.Fatalf("substituted command digest error = %v", err)
			}
		})
	}

	weakened := profile
	weakened.Commands = append([]ValidationCommand(nil), profile.Commands...)
	weakened.Commands[0].Required = false
	if err := weakened.Validate(taskID, runID); !errors.Is(
		err,
		ErrInvalidValidationProfile,
	) {
		t.Fatalf("weakened acceptance profile error = %v", err)
	}
	for _, paths := range [][]string{
		{"../repair_completion.go"},
		{"internal/agent/../repair_completion.go"},
		{`internal\agent\repair_completion.go`},
		{
			"internal/agent/repair_completion.go",
			"internal/agent/repair_completion.go",
		},
	} {
		invalid := profile
		invalid.Commands = append(
			[]ValidationCommand(nil),
			profile.Commands...,
		)
		invalid.Commands[0].RelevantChangedFiles = paths
		if err := invalid.Validate(taskID, runID); !errors.Is(
			err,
			ErrInvalidValidationProfile,
		) {
			t.Fatalf("invalid relevant paths %q error = %v", paths, err)
		}
	}
}

func TestRenderValidationPlanCommandBindsLogicalExecutionContract(
	t *testing.T,
) {
	request := repairValidationProfile(
		repairTaskID(t, 31),
		repairRunID(t, 31),
	).Commands[0].Request
	baseline, err := RenderValidationPlanCommand(request)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*executor.ToolRequest){
		"tool": func(value *executor.ToolRequest) {
			value.Name = executor.ToolBuild
		},
		"ordered arguments": func(value *executor.ToolRequest) {
			value.Arguments[2].Value = "./internal/agent"
		},
		"timeout": func(value *executor.ToolRequest) {
			value.Timeout += time.Second
		},
		"ordered effects": func(value *executor.ToolRequest) {
			value.ExpectedSideEffects[0], value.ExpectedSideEffects[1] =
				value.ExpectedSideEffects[1], value.ExpectedSideEffects[0]
		},
	} {
		t.Run(name+" changes projection", func(t *testing.T) {
			changed := cloneValidationToolRequest(request)
			mutate(&changed)
			rendered, err := RenderValidationPlanCommand(changed)
			if err != nil {
				t.Fatal(err)
			}
			if rendered == baseline {
				t.Fatalf("%s retained canonical plan command", name)
			}
		})
	}
	for name, mutate := range map[string]func(*executor.ToolRequest){
		"caller authority claim": func(value *executor.ToolRequest) {
			value.ClaimedAuthority = executor.AuthorityPrivileged
		},
		"request ID": func(value *executor.ToolRequest) {
			value.ID = "another-request"
		},
		"task ID": func(value *executor.ToolRequest) {
			value.TaskID = repairTaskID(t, 32)
		},
		"run ID": func(value *executor.ToolRequest) {
			value.RunID = repairRunID(t, 32)
		},
		"absolute worktree": func(value *executor.ToolRequest) {
			value.WorkingDirectory = `D:\another\task-worktree`
		},
	} {
		t.Run(name+" does not change projection", func(t *testing.T) {
			changed := cloneValidationToolRequest(request)
			mutate(&changed)
			rendered, err := RenderValidationPlanCommand(changed)
			if err != nil {
				t.Fatal(err)
			}
			if rendered != baseline {
				t.Fatalf("%s changed logical plan command", name)
			}
		})
	}
}

func cloneValidationToolRequest(
	request executor.ToolRequest,
) executor.ToolRequest {
	request.Arguments = append(
		[]executor.ToolArgument(nil),
		request.Arguments...,
	)
	request.ExpectedSideEffects = append(
		[]executor.SideEffect(nil),
		request.ExpectedSideEffects...,
	)
	return request
}

func TestParseValidationFailureRedactsBoundsAndLinksKnownScope(t *testing.T) {
	pipeline, err := redact.NewPipeline(nil, redact.Limits{
		MaximumInputBytes:  16 << 10,
		MaximumOutputBytes: 8 << 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pipeline.Close)
	command := repairValidationProfile(
		repairTaskID(t, 3),
		repairRunID(t, 4),
	).Commands[0]
	command.RelevantChangedFiles = []string{
		"internal/agent",
	}
	command.PlanStepIDs = []string{"step-validation"}
	failure, err := ParseValidationFailure(
		command,
		executor.ToolResult{
			Summary: "go test failed",
			StderrRedacted: strings.Repeat("x", 5<<10) +
				`\ninternal/agent/repair_completion.go:42: failed ` +
				`OPENAI_API_KEY="sk-proj-ABCDEFGHIJKLMNOPQRSTUVWX"`,
			ExitCode: 1, State: "failed",
		},
		errors.New("exit status 1"),
		[]string{
			"internal/agent/sub/repair_completion.go",
			"internal/agent-old/other.go",
		},
		pipeline,
	)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(failure.SummaryRedacted, "ABCDEFGHIJKLMNOP") ||
		len(failure.SummaryRedacted) > maximumFailureSummaryBytes ||
		!failure.OutputTruncated ||
		len(failure.ChangedFiles) != 1 ||
		failure.ChangedFiles[0] != "internal/agent/sub/repair_completion.go" ||
		len(failure.PlanStepIDs) != 1 ||
		failure.PlanStepIDs[0] != "step-validation" {
		t.Fatalf("failure summary = %#v", failure)
	}
}

func TestCompletionRequiresValidationAndAwaitsReview(t *testing.T) {
	report := ValidationReport{
		ProfileName: "fixed", ProfileVersion: "v1",
		ProfileDigest: strings.Repeat("a", 64),
		Executions: []ValidationExecution{
			{
				ValidationID:       repairValidationID(t, 5),
				CommandID:          "go-test",
				CommandFingerprint: strings.Repeat("b", 64),
				Required:           true,
				AcceptanceTest:     true,
				PlanStepIDs:        []string{"step-validation"},
				State:              domain.ValidationStatePassed,
			},
		},
	}
	summary := CompletionSummary{
		TaskID:       repairTaskID(t, 6),
		RunID:        repairRunID(t, 7),
		PlanRevision: 2,
		Repository: RepositoryCompletionSummary{
			StatusRedacted: "M internal/agent/repair_completion.go",
			DiffRedacted:   "diff --git synthetic",
			DiffSHA256:     strings.Repeat("c", 64),
			ChangedFiles:   []string{"internal/agent/repair_completion.go"},
		},
		Validation:             report,
		Budget:                 repairBudgetSummary(),
		Assumptions:            []string{"validation profile is fixed"},
		Limitations:            []string{"live provider not exercised"},
		ImplementationComplete: true,
		ValidationComplete:     true,
		State:                  domain.TaskStateAwaitingReview,
	}
	if err := summary.Validate(); err != nil {
		t.Fatal(err)
	}
	autoAccepted := summary
	autoAccepted.State = domain.TaskStateCompleted
	if err := autoAccepted.Validate(); err == nil {
		t.Fatal("completion auto-accepted without a user decision")
	}
	invalidDigest := summary
	invalidDigest.Repository.DiffSHA256 = strings.Repeat("Z", 64)
	if err := invalidDigest.Validate(); err == nil {
		t.Fatal("non-hex diff digest was accepted")
	}
	incomplete := summary
	incomplete.Validation.Executions[0].State = domain.ValidationStateFailed
	if err := incomplete.Validate(); err == nil {
		t.Fatal("failed required validation was presented as complete")
	}
}

func TestReviewDecisionRequiresAttributionAndMapsEveryUserChoice(t *testing.T) {
	request := ReviewDecisionRequest{
		TaskID:               repairTaskID(t, 8),
		RunID:                repairRunID(t, 9),
		PlanRevision:         2,
		CompletionRevision:   1,
		ExpectedTaskRevision: 7,
		Actor:                "user:fixture",
		AuthorityReference:   "review:fixture",
		ReasonRedacted:       "reviewed exact completion evidence",
		IdempotencyKey:       "review-decision-fixture",
	}
	for decision, state := range map[ReviewDecision]domain.TaskState{
		ReviewDecisionAccept:        domain.TaskStateCompleted,
		ReviewDecisionRequestRepair: domain.TaskStateRunning,
		ReviewDecisionRollback:      domain.TaskStateRolledBack,
		ReviewDecisionAbandon:       domain.TaskStateCancelled,
	} {
		request.Decision = decision
		if err := request.Validate(); err != nil {
			t.Fatalf("%s validation: %v", decision, err)
		}
		got, err := decision.ResultingTaskState()
		if err != nil || got != state {
			t.Fatalf("%s state = %s, %v; want %s", decision, got, err, state)
		}
	}
	request.AuthorityReference = ""
	if err := request.Validate(); !errors.Is(err, ErrInvalidReviewDecision) {
		t.Fatalf("unattributed review error = %v", err)
	}
}

func repairValidationProfile(
	taskID domain.TaskID,
	runID domain.RunID,
) ValidationProfile {
	command := ValidationCommand{
		ID: "go-test", Required: true, AcceptanceTest: true,
		Request: executor.ToolRequest{
			SchemaVersion: executor.ToolSchemaVersion,
			ID:            "validation-go-test",
			TaskID:        taskID,
			RunID:         runID,
			Name:          executor.ToolTest,
			Arguments: []executor.ToolArgument{
				{Name: "executable", Value: "go"},
				{Name: "argument", Value: "test"},
				{Name: "argument", Value: "./..."},
			},
			WorkingDirectory: `C:\fixture\worktree`,
			Timeout:          time.Minute,
			ClaimedAuthority: executor.AuthorityAutomaticRead,
			ExpectedSideEffects: []executor.SideEffect{
				executor.EffectSubprocess,
				executor.EffectRepositoryRead,
			},
			IdempotencyKey: "validation-go-test",
			Requester:      "fixed-agent",
		},
		RelevantChangedFiles: []string{"internal/agent/repair_completion.go"},
		PlanStepIDs:          []string{"step-validation"},
	}
	command.PlanCommand = mustRenderValidationPlanCommand(command.Request)
	return ValidationProfile{
		Name: "fixed-go", Version: "v1",
		Commands: []ValidationCommand{
			command,
		},
	}
}

func mustRenderValidationPlanCommand(
	request executor.ToolRequest,
) string {
	value, err := RenderValidationPlanCommand(request)
	if err != nil {
		panic(err)
	}
	return value
}

func repairBudgetSummary() BudgetCompletionSummary {
	currency := domain.CurrencyCode("USD")
	return BudgetCompletionSummary{
		Forecast: ExactCostSummary{
			Known: true, Numerator: 2, Denominator: 1, Currency: currency,
		},
		Reserved: ExactCostSummary{
			Known: true, Numerator: 0, Denominator: 1, Currency: currency,
		},
		Actual: ExactCostSummary{
			Known: true, Numerator: 1, Denominator: 1, Currency: currency,
		},
		Remaining: ExactCostSummary{
			Known: true, Numerator: 4, Denominator: 1, Currency: currency,
		},
	}
}

func repairTaskID(t *testing.T, number int) domain.TaskID {
	t.Helper()
	id, err := domain.ParseTaskID(
		"tsk_01890f3c-4a00-7abc-8def-" +
			leftPadHex(number),
	)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func repairRunID(t *testing.T, number int) domain.RunID {
	t.Helper()
	id, err := domain.ParseRunID(
		"run_01890f3c-4a00-7abc-8def-" +
			leftPadHex(number),
	)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func repairValidationID(t *testing.T, number int) domain.ValidationID {
	t.Helper()
	id, err := domain.ParseValidationID(
		"val_01890f3c-4a00-7abc-8def-" +
			leftPadHex(number),
	)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func leftPadHex(number int) string {
	const digits = "000000000000"
	value := fmt.Sprintf("%x", number)
	return digits[:len(digits)-len(value)] + value
}
