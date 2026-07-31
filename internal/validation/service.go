package validation

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/executor"
	"codeflux.dev/codeflux/internal/redact"
)

type RunRepository interface {
	CreateValidationRunIntent(context.Context, RunIntent) (RunIntent, error)
	CommitValidationRunResult(context.Context, RunResult) (RunResult, error)
	InvalidateValidationRuns(context.Context, domain.RunID, string) ([]Invalidation, error)
	ListRequiredValidationReruns(context.Context, domain.RunID, string) ([]RequiredRerun, error)
}

type BindingSource interface {
	CurrentValidationBinding(context.Context, domain.TaskID) (WorktreeDiffBinding, error)
}

type EventSink interface {
	ValidationStarted(context.Context, RunIntent) error
	ValidationResultCommitted(context.Context, RunIntent, RunResult) error
	ValidationInvalidated(context.Context, Invalidation) error
}

type Runner interface {
	ResolveExecutable([]string) (ExecutableIdentity, error)
	Execute(context.Context, RunExecution) (ExecutionResult, error)
}

type RunExecution struct {
	Intent             RunIntent
	Arguments          []string
	WorktreePath       string
	PermissionPolicy   executor.PermissionPolicy
	Environment        map[string]string
	AllowedEnvironment []string
}

type ExecutionResult struct {
	ExitCode        int
	Duration        time.Duration
	TimedOut        bool
	Cancelled       bool
	StdoutRedacted  string
	StderrRedacted  string
	StdoutTruncated bool
	StderrTruncated bool
	Summary         string
}

type RunRequest struct {
	ValidationID       domain.ValidationID
	TaskID             domain.TaskID
	RunID              domain.RunID
	Profile            Profile
	Check              Check
	WorktreePath       string
	PermissionPolicy   executor.PermissionPolicy
	Environment        map[string]string
	AllowedEnvironment []string
	IdempotencyKey     string
}

type RunCompletion struct {
	Intent         RunIntent
	Result         RunResult
	Invalidations  []Invalidation
	RequiredReruns []RequiredRerun
}

type Service struct {
	repository RunRepository
	bindings   BindingSource
	runner     Runner
	events     EventSink
}

func NewService(repository RunRepository, bindings BindingSource, runner Runner, events EventSink) (*Service, error) {
	if repository == nil || bindings == nil || runner == nil || events == nil {
		return nil, errors.New("validation repository, binding source, runner, and events are required")
	}
	return &Service{repository: repository, bindings: bindings, runner: runner, events: events}, nil
}

// Run creates and commits the immutable intent before invoking the mediated
// runner. Result and invalidation events are published only after their
// corresponding repository transaction has committed.
func (service *Service) Run(ctx context.Context, request RunRequest) (RunCompletion, error) {
	if request.ValidationID.IsZero() || request.TaskID.IsZero() || request.RunID.IsZero() ||
		strings.TrimSpace(request.WorktreePath) == "" ||
		strings.TrimSpace(request.IdempotencyKey) != request.IdempotencyKey || request.IdempotencyKey == "" {
		return RunCompletion{}, ErrInvalidValidationRun
	}
	if err := validateProfileCheck(request.Profile, request.Check); err != nil {
		return RunCompletion{}, err
	}
	profileDigest, err := request.Profile.Digest()
	if err != nil {
		return RunCompletion{}, err
	}
	binding, err := service.bindings.CurrentValidationBinding(ctx, request.TaskID)
	if err != nil {
		return RunCompletion{}, err
	}
	if !validBinding(binding) {
		return RunCompletion{}, ErrInvalidValidationRun
	}
	executableIdentity, err := service.runner.ResolveExecutable(request.Check.Arguments)
	if err != nil {
		return RunCompletion{}, err
	}
	definition, err := commandDefinition(request.Check)
	if err != nil {
		return RunCompletion{}, err
	}
	intent, err := SealRunIntent(RunIntent{
		ID: request.ValidationID, TaskID: request.TaskID, RunID: request.RunID,
		ProfileName: request.Profile.Name, ProfileVersion: request.Profile.Version,
		ProfileDigest: profileDigest, CheckID: request.Check.ID, CheckClass: request.Check.Class,
		Required:         request.Check.Strength == RequirementRequired,
		WorktreeRevision: binding.WorktreeRevision, DiffIdentity: binding.DiffIdentity,
		CommandDefinitionJSON: definition, CommandFingerprint: request.Check.CommandFingerprint,
		Executable: executableIdentity, Timeout: request.Check.Timeout,
		IdempotencyKey: request.IdempotencyKey,
	})
	if err != nil {
		return RunCompletion{}, err
	}
	intent, err = service.repository.CreateValidationRunIntent(ctx, intent)
	if err != nil {
		return RunCompletion{}, err
	}
	if err := service.events.ValidationStarted(ctx, intent); err != nil {
		return RunCompletion{}, err
	}

	execution, executionErr := service.runner.Execute(ctx, RunExecution{
		Intent: intent, Arguments: slices.Clone(request.Check.Arguments),
		WorktreePath: request.WorktreePath, PermissionPolicy: request.PermissionPolicy,
		Environment: request.Environment, AllowedEnvironment: slices.Clone(request.AllowedEnvironment),
	})
	after, bindingErr := service.bindings.CurrentValidationBinding(ctx, request.TaskID)
	if bindingErr != nil {
		return RunCompletion{}, bindingErr
	}
	if !validBinding(after) {
		return RunCompletion{}, ErrInvalidValidationRun
	}
	result, err := buildRunResult(intent, request.Check, execution, executionErr, after.DiffIdentity)
	if err != nil {
		return RunCompletion{}, err
	}
	result, err = service.repository.CommitValidationRunResult(ctx, result)
	if err != nil {
		return RunCompletion{}, err
	}
	if err := service.events.ValidationResultCommitted(ctx, intent, result); err != nil {
		return RunCompletion{}, err
	}
	completion := RunCompletion{Intent: intent, Result: result}
	if after.DiffIdentity != intent.DiffIdentity {
		completion.Invalidations, err = service.repository.InvalidateValidationRuns(ctx, request.RunID, after.DiffIdentity)
		if err != nil {
			return RunCompletion{}, err
		}
		for _, invalidation := range completion.Invalidations {
			if err := service.events.ValidationInvalidated(ctx, invalidation); err != nil {
				return RunCompletion{}, err
			}
		}
	}
	completion.RequiredReruns, err = service.repository.ListRequiredValidationReruns(ctx, request.RunID, after.DiffIdentity)
	return completion, err
}

func buildRunResult(intent RunIntent, check Check, execution ExecutionResult, executionErr error, observedDiff string) (RunResult, error) {
	state := domain.ValidationStatePassed
	if execution.Cancelled || execution.TimedOut {
		state = domain.ValidationStateCancelled
	} else if executionErr != nil || execution.ExitCode != 0 {
		state = domain.ValidationStateFailed
	}
	stdout, stdoutTruncated := boundCaptured(execution.StdoutRedacted)
	stderr, stderrTruncated := boundCaptured(execution.StderrRedacted)
	parsed := ParseCheckOutput(check, stdout, stderr, execution.Summary)
	if executionErr != nil && parsed.RawSummary == "validation command produced no output" {
		parsed.RawSummary = "validation runner failed before producing redacted output"
	}
	return SealRunResult(RunResult{
		ValidationRunID: intent.ID, State: state, ExitCode: execution.ExitCode,
		Duration: execution.Duration, TimedOut: execution.TimedOut, Cancelled: execution.Cancelled,
		StdoutRedacted: stdout, StderrRedacted: stderr,
		StdoutTruncated: execution.StdoutTruncated || stdoutTruncated,
		StderrTruncated: execution.StderrTruncated || stderrTruncated,
		ParserName:      parsed.ParserName, ParseSucceeded: parsed.ParseSucceeded,
		ParsedResultJSON: parsed.JSON, RawRedactedSummary: parsed.RawSummary,
		ObservedDiffIdentity: observedDiff,
	})
}

func validBinding(binding WorktreeDiffBinding) bool {
	return validGitRevision(binding.WorktreeRevision) && lowerHexDigest(binding.DiffIdentity)
}

func boundCaptured(value string) (string, bool) {
	value = strings.ToValidUTF8(value, "\uFFFD")
	if len(value) <= MaximumCapturedOutputBytes {
		return value, false
	}
	end := MaximumCapturedOutputBytes
	for end > 0 && !utf8.ValidString(value[:end]) {
		end--
	}
	return value[:end], true
}

type MediatedRunner struct {
	redactor *redact.Pipeline
	progress executor.ToolProgressSink
}

func NewMediatedRunner(redactor *redact.Pipeline, progress executor.ToolProgressSink) (*MediatedRunner, error) {
	if redactor == nil {
		return nil, errors.New("validation runner redactor is required")
	}
	return &MediatedRunner{redactor: redactor, progress: progress}, nil
}

func (runner *MediatedRunner) ResolveExecutable(arguments []string) (ExecutableIdentity, error) {
	if len(arguments) == 0 {
		return ExecutableIdentity{}, ErrInvalidValidationRun
	}
	path, digest, err := executor.ResolveExecutableIdentity(arguments[0])
	return ExecutableIdentity{Path: path, SHA256: digest}, err
}

func (runner *MediatedRunner) Execute(ctx context.Context, request RunExecution) (ExecutionResult, error) {
	toolRequest, err := validationToolRequest(request)
	if err != nil {
		return ExecutionResult{}, err
	}
	classification, err := executor.ClassifyAuthority(toolRequest, request.PermissionPolicy)
	if err != nil {
		return ExecutionResult{}, err
	}
	result, err := executor.ExecuteAuthorizedTool(ctx, executor.AuthorizedToolRequest{
		Request: toolRequest, Classification: classification, WorktreePath: request.WorktreePath,
		ExpectedExecutable:       request.Intent.Executable.Path,
		ExpectedExecutableSHA256: request.Intent.Executable.SHA256,
		Environment:              request.Environment, AllowedEnvironment: request.AllowedEnvironment,
		Redactor: runner.redactor, Progress: runner.progress,
	})
	return ExecutionResult{
		ExitCode: result.ExitCode, Duration: result.Duration, TimedOut: result.TimedOut,
		Cancelled: result.Cancelled, StdoutRedacted: result.StdoutRedacted,
		StderrRedacted: result.StderrRedacted, StdoutTruncated: result.StdoutTruncated,
		StderrTruncated: result.StderrTruncated, Summary: result.Summary,
	}, err
}

func validationToolRequest(request RunExecution) (executor.ToolRequest, error) {
	if err := request.Intent.Validate(); err != nil || len(request.Arguments) == 0 {
		return executor.ToolRequest{}, ErrInvalidValidationRun
	}
	arguments := make([]executor.ToolArgument, 0, len(request.Arguments))
	for index, value := range request.Arguments {
		arguments = append(arguments, executor.ToolArgument{Name: fmt.Sprintf("arg-%03d", index), Value: value})
	}
	name, effects := validationToolPolicy(request.Intent.CheckClass)
	toolRequest := executor.ToolRequest{
		SchemaVersion: executor.ToolSchemaVersion, ID: request.Intent.ID.String(),
		TaskID: request.Intent.TaskID, RunID: request.Intent.RunID, Name: name,
		Arguments: arguments, WorkingDirectory: request.WorktreePath,
		Timeout: request.Intent.Timeout, ClaimedAuthority: executor.AuthorityPrivileged,
		ExpectedSideEffects: effects, IdempotencyKey: request.Intent.IdempotencyKey,
		Requester: "validation-service", PurposeUntrusted: "execute immutable validation run intent",
	}
	return toolRequest, executor.ValidateToolRequest(toolRequest)
}

func validationToolPolicy(class CheckClass) (executor.ToolName, []executor.SideEffect) {
	switch class {
	case CheckFormatter:
		return executor.ToolFormat, []executor.SideEffect{executor.EffectSubprocess, executor.EffectTaskWorktreeWrite}
	case CheckTargetedTest, CheckBroadTest:
		return executor.ToolTest, []executor.SideEffect{executor.EffectSubprocess, executor.EffectRepositoryRead}
	case CheckBuild:
		return executor.ToolBuild, []executor.SideEffect{executor.EffectSubprocess, executor.EffectRepositoryRead}
	case CheckStaticAnalysis:
		return executor.ToolStaticAnalysis, []executor.SideEffect{executor.EffectSubprocess, executor.EffectRepositoryRead}
	default:
		return "", nil
	}
}
