package validation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/executor"
	"codeflux.dev/codeflux/internal/redact"
	"codeflux.dev/codeflux/internal/workspace"
)

func TestServiceCommitsIntentBeforeExecutionAndEventsAfterCommits(t *testing.T) {
	t.Parallel()
	taskID, _ := domain.NewTaskID()
	runID, _ := domain.NewRunID()
	validationID, _ := domain.NewValidationID()
	before := WorktreeDiffBinding{WorktreeRevision: strings.Repeat("a", 40), DiffIdentity: strings.Repeat("b", 64)}
	after := WorktreeDiffBinding{WorktreeRevision: before.WorktreeRevision, DiffIdentity: strings.Repeat("c", 64)}
	bindings := &sequenceBindings{values: []WorktreeDiffBinding{before, after}}
	repository := &memoryRunRepository{}
	runner := &recordingRunner{repository: repository, result: ExecutionResult{
		ExitCode: 0, Duration: time.Second,
		StdoutRedacted: `{"Action":"pass","Package":"codeflux.dev/fixture","Test":"TestOneLine"}`,
		Summary:        "go exited 0",
	}}
	events := &commitCheckingEvents{repository: repository}
	service, err := NewService(repository, bindings, runner, events)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := SelectProfile(domain.RiskLevelProtected, []workspace.SuggestedCommand{{
		Kind: "test", Arguments: []string{"go", "test", "-json", "./internal/validation"}, Source: "go-toolchain",
	}})
	if err != nil {
		t.Fatal(err)
	}

	completion, err := service.Run(context.Background(), RunRequest{
		ValidationID: validationID, TaskID: taskID, RunID: runID,
		Profile: profile, Check: profile.Checks[0], WorktreePath: t.TempDir(),
		IdempotencyKey: "validation-service-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !runner.sawCommittedIntent || !events.startedAfterCommit || !events.resultAfterCommit {
		t.Fatalf("ordering runner=%t events=%#v", runner.sawCommittedIntent, events)
	}
	if completion.Result.State != domain.ValidationStatePassed || !completion.Result.ParseSucceeded ||
		len(completion.Invalidations) != 1 || len(completion.RequiredReruns) != 1 {
		t.Fatalf("completion = %#v", completion)
	}
}

func TestParseCheckOutputRecognizesGoValidationAndPreservesFallback(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		class     CheckClass
		stdout    string
		stderr    string
		parser    string
		succeeded bool
		contains  string
	}{
		{name: "go test json", class: CheckTargetedTest, stdout: `{"Action":"pass","Package":"codeflux.dev/x","Test":"TestX"}`, parser: "go-test-v1", succeeded: true, contains: "TestX"},
		{name: "formatter", class: CheckFormatter, stdout: "internal/x.go\ninternal/y.go", parser: "go-formatter-v1", succeeded: true, contains: "internal/x.go"},
		{name: "vet", class: CheckStaticAnalysis, stderr: "internal/x.go:12:4: unreachable code", parser: "go-diagnostics-v1", succeeded: true, contains: "unreachable code"},
		{name: "fallback", class: CheckBuild, stderr: "tool emitted an unfamiliar diagnostic", parser: "go-diagnostics-v1", succeeded: false, contains: "tool emitted an unfamiliar diagnostic"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parsed := ParseCheckOutput(Check{Class: test.class}, test.stdout, test.stderr, "fallback")
			if parsed.ParserName != test.parser || parsed.ParseSucceeded != test.succeeded {
				t.Fatalf("parsed = %#v", parsed)
			}
			combined := parsed.JSON + parsed.RawSummary
			if !strings.Contains(combined, test.contains) {
				t.Fatalf("parse output %q does not contain %q", combined, test.contains)
			}
		})
	}
}

func TestBuildRunResultCapturesCancellationDurationAndTruncation(t *testing.T) {
	t.Parallel()
	id, _ := domain.NewValidationID()
	intent := RunIntent{ID: id}
	result, err := buildRunResult(intent, Check{Class: CheckBuild}, ExecutionResult{
		ExitCode: -1, Duration: 3 * time.Second, TimedOut: true,
		StdoutRedacted: strings.Repeat("x", MaximumCapturedOutputBytes+1024),
		StderrRedacted: "bounded failure", Summary: "build timed out",
	}, nil, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	if result.State != domain.ValidationStateCancelled || !result.TimedOut || result.Cancelled ||
		result.Duration != 3*time.Second || !result.StdoutTruncated ||
		len(result.StdoutRedacted) != MaximumCapturedOutputBytes {
		t.Fatalf("captured result = %#v", result)
	}
}

func TestMediatedRunnerRejectsExecutableIdentityChange(t *testing.T) {
	t.Parallel()
	pipeline, err := redact.NewPipeline(nil, redact.Limits{MaximumInputBytes: 64 << 10, MaximumOutputBytes: 32 << 10})
	if err != nil {
		t.Fatal(err)
	}
	defer pipeline.Close()
	runner, err := NewMediatedRunner(pipeline, nil)
	if err != nil {
		t.Fatal(err)
	}
	taskID, _ := domain.NewTaskID()
	runID, _ := domain.NewRunID()
	validationID, _ := domain.NewValidationID()
	identity, err := runner.ResolveExecutable([]string{"go", "version"})
	if err != nil {
		t.Skipf("Go executable unavailable: %v", err)
	}
	intent, err := SealRunIntent(RunIntent{
		ID: validationID, TaskID: taskID, RunID: runID,
		ProfileName: ProfileProtected, ProfileVersion: ProfileVersionV1,
		ProfileDigest: strings.Repeat("a", 64), CheckID: "targeted-test",
		CheckClass: CheckTargetedTest, Required: true,
		WorktreeRevision: strings.Repeat("b", 40), DiffIdentity: strings.Repeat("c", 64),
		CommandDefinitionJSON: `{"arguments":["go","version"]}`,
		CommandFingerprint:    strings.Repeat("d", 64), Executable: identity,
		Timeout: 5 * time.Second, IdempotencyKey: "identity-change",
	})
	if err != nil {
		t.Fatal(err)
	}
	intent.Executable.SHA256 = strings.Repeat("f", 64)
	intent, err = SealRunIntent(intent)
	if err != nil {
		t.Fatal(err)
	}
	worktree := t.TempDir()
	arguments := []string{"go", "version"}
	policy := executor.PermissionPolicy{
		TaskID: taskID, WorktreePath: worktree, TaskActive: true,
		ApprovedCommands: []executor.ActionPattern{{
			TaskID: taskID, Tool: executor.ToolTest, Arguments: arguments,
			WorkingDirectory: worktree,
			Effects:          []executor.SideEffect{executor.EffectRepositoryRead, executor.EffectSubprocess},
		}},
	}
	_, err = runner.Execute(context.Background(), RunExecution{
		Intent: intent, Arguments: arguments, WorktreePath: worktree, PermissionPolicy: policy,
	})
	if err == nil || !strings.Contains(err.Error(), "executable content differs") {
		t.Fatalf("identity mismatch error = %v", err)
	}
}

type sequenceBindings struct {
	values []WorktreeDiffBinding
	index  int
}

func (source *sequenceBindings) CurrentValidationBinding(context.Context, domain.TaskID) (WorktreeDiffBinding, error) {
	if source.index >= len(source.values) {
		return WorktreeDiffBinding{}, errors.New("binding sequence exhausted")
	}
	value := source.values[source.index]
	source.index++
	return value, nil
}

type memoryRunRepository struct {
	intent *RunIntent
	result *RunResult
}

func (repository *memoryRunRepository) CreateValidationRunIntent(_ context.Context, intent RunIntent) (RunIntent, error) {
	copy := intent
	repository.intent = &copy
	return intent, nil
}

func (repository *memoryRunRepository) CommitValidationRunResult(_ context.Context, result RunResult) (RunResult, error) {
	copy := result
	repository.result = &copy
	return result, nil
}

func (repository *memoryRunRepository) InvalidateValidationRuns(_ context.Context, _ domain.RunID, current string) ([]Invalidation, error) {
	return []Invalidation{{ValidationRunID: repository.intent.ID, PreviousDiffIdentity: repository.intent.DiffIdentity, CurrentDiffIdentity: current, Reason: "underlying-diff-changed"}}, nil
}

func (repository *memoryRunRepository) ListRequiredValidationReruns(_ context.Context, _ domain.RunID, current string) ([]RequiredRerun, error) {
	return []RequiredRerun{{CheckID: repository.intent.CheckID, PreviousRunID: repository.intent.ID, PreviousDiffIdentity: repository.intent.DiffIdentity, CurrentDiffIdentity: current}}, nil
}

type recordingRunner struct {
	repository         *memoryRunRepository
	result             ExecutionResult
	sawCommittedIntent bool
}

func (runner *recordingRunner) ResolveExecutable([]string) (ExecutableIdentity, error) {
	return ExecutableIdentity{Path: "C:/tool/go.exe", SHA256: strings.Repeat("e", 64)}, nil
}

func (runner *recordingRunner) Execute(context.Context, RunExecution) (ExecutionResult, error) {
	runner.sawCommittedIntent = runner.repository.intent != nil
	return runner.result, nil
}

type commitCheckingEvents struct {
	repository         *memoryRunRepository
	startedAfterCommit bool
	resultAfterCommit  bool
}

func (events *commitCheckingEvents) ValidationStarted(_ context.Context, intent RunIntent) error {
	events.startedAfterCommit = events.repository.intent != nil && events.repository.intent.ID == intent.ID
	return nil
}

func (events *commitCheckingEvents) ValidationResultCommitted(_ context.Context, _ RunIntent, result RunResult) error {
	events.resultAfterCommit = events.repository.result != nil && events.repository.result.ResultDigest == result.ResultDigest
	return nil
}

func (*commitCheckingEvents) ValidationInvalidated(context.Context, Invalidation) error { return nil }
