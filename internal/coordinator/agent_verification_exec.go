package coordinator

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/executor"
	"codeflux.dev/codeflux/internal/redact"
)

// PIPE-131: acceptance runs, adversarial probes, and mutation suite runs used
// to shell out with os/exec directly -- a TestMain, a package init, or a
// produced program's own main ran with the coordinator's full environment,
// including whatever provider credentials it happened to be holding, and
// with none of the argument-array discipline, timeout enforcement, or
// output redaction every model-issued command already goes through. This
// file is what those three stages now run their commands through instead:
// the same executor.ClassifyAuthority and executor.ExecuteAuthorizedTool the
// rest of the product uses.
//
// What this file does NOT claim: routing a command through
// ExecuteAuthorizedTool bounds the environment the command starts with, the
// directory it is launched from, and how long it may run. It does not
// sandbox what a running process does afterward -- a produced program or a
// go test binary is an ordinary OS process once started, and nothing here
// stops it from opening a socket or writing to a path outside the worktree
// by name. AGENTS.md is explicit that workspace confinement must never be
// described as a perfect sandbox, and PIPE-133's abuse case exists precisely
// to make that boundary's real edge visible rather than assumed.

// goToolchainEnvironmentNames lets `go build`/`go test`/`go list` see the
// toolchain configuration a real development environment sets, without
// widening the allowlist to anything that could carry a secret.
//
// minimalEnvironment's default allowlist -- PATH, TEMP, HOME/USERPROFILE,
// and a handful of others -- is enough for the Go toolchain to infer GOROOT,
// GOPATH, and GOCACHE from well-known defaults, but a repository whose own
// environment sets GOFLAGS, GOPROXY, or CGO_ENABLED explicitly would build
// successfully unmediated and fail once mediation stripped those values.
// None of these names match minimalEnvironment's sensitive-name patterns
// (no _API_KEY, _TOKEN, _SECRET, or _PASSWORD suffix, and none of the
// explicit blocked names), so allowing them here does not reopen the
// credential-scrubbing this routing exists to add.
var goToolchainEnvironmentNames = []string{
	"GOPATH", "GOCACHE", "GOMODCACHE", "GOROOT", "GOFLAGS", "GOPROXY",
	"GOSUMDB", "GO111MODULE", "GOTOOLCHAIN", "GOOS", "GOARCH",
	"CGO_ENABLED", "GOPRIVATE", "GOINSECURE", "GOVCS", "GOWORK",
}

// verificationCommandResult is the mediated outcome of one verification-stage
// command -- the parts agent_acceptance.go, agent_adversarial.go, and
// agent_stage_mutation.go compared against os/exec's CombinedOutput before
// PIPE-131, plus the timeout fact the unmediated call reported inconsistently
// (a killed process and a genuine nonzero exit both surfaced as the same
// Go error).
type verificationCommandResult struct {
	// Combined is stdout followed by stderr, both already passed through the
	// redaction pipeline. It is an approximation of exec.Cmd.CombinedOutput,
	// which interleaves the two streams in write order; that exact
	// interleaving is not preserved, because the mediated boundary keeps the
	// streams separate for redaction and truncation accounting. Every caller
	// in this package only ever scanned combined output for a line of text
	// (a build error, a differing line, a panic marker), never for the
	// relative order stdout and stderr were written in, so this holds their
	// existing behaviour.
	Combined string
	// Succeeded is true only for a real, un-cancelled, zero-exit completion.
	Succeeded bool
	TimedOut  bool
}

// runMediatedVerificationCommand executes one argument-array command for a
// verification stage -- acceptance, the adversarial probe, or mutation's
// build/suite checks -- through executor.ClassifyAuthority and
// executor.ExecuteAuthorizedTool rather than through a direct exec.Command
// call that never passed through the permission policy at all (PIPE-131).
//
// What reaches this function is never chosen by a model. Every argument
// array is built by this package's own stage logic: a fixed "go
// build"/"go test"/"go list" invocation, or the exact binary this run itself
// just built, run from a directory this call itself computed. What is NOT
// trustworthy is what runs once that process starts -- a TestMain, a package
// init, a produced program's main, or an acceptance example's own arguments
// and standard input, all sourced from requirement text nobody has reviewed
// as safe. Routing through the mediated boundary is what gives that
// untrusted code the same minimal, credential-scrubbed environment, the
// same worktree-confined launch directory, and the same enforced timeout
// every model-issued command gets -- controls a raw exec.Command call
// carried none of.
//
// A fresh single-use grant is minted for the exact command about to run,
// scoped to a synthetic identity rather than the run's own task or policy.
// This call is the coordinator's own verification decision, never a
// model's: there is no user approval to route it through, and it must not
// consume or leave behind a grant that belongs to the run's real task
// policy. What is reused for real is the authorization code path itself --
// ClassifyAuthority's derivation and ExecuteAuthorizedTool's execution --
// not the run's own per-task grant bookkeeping.
func (execution *AgentExecution) runMediatedVerificationCommand(
	ctx context.Context,
	worktreeRoot string,
	workingDirectory string,
	stage string,
	arguments []string,
	stdin string,
	timeout time.Duration,
	allowedEnvironment []string,
) (verificationCommandResult, error) {
	if len(arguments) == 0 {
		return verificationCommandResult{}, fmt.Errorf(
			"verification stage %q named no command to run", stage)
	}
	redactor, err := execution.verificationRedactor()
	if err != nil {
		return verificationCommandResult{}, fmt.Errorf(
			"verification stage %q has no redaction pipeline to route output "+
				"through: %w", stage, err)
	}
	taskID, err := domain.NewTaskID()
	if err != nil {
		return verificationCommandResult{}, err
	}
	runID, err := domain.NewRunID()
	if err != nil {
		return verificationCommandResult{}, err
	}
	cleanWorktree := filepath.Clean(worktreeRoot)
	cleanDirectory := filepath.Clean(workingDirectory)
	typedArguments := make([]executor.ToolArgument, len(arguments))
	for index, value := range arguments {
		typedArguments[index] = executor.ToolArgument{
			Name: fmt.Sprintf("arg%d", index), Value: value,
		}
	}
	effects := []executor.SideEffect{executor.EffectSubprocess}
	label := fmt.Sprintf("verification-%s-%s", stage, taskID.String())
	request := executor.ToolRequest{
		SchemaVersion:       executor.ToolSchemaVersion,
		ID:                  label,
		TaskID:              taskID,
		RunID:               runID,
		Name:                executor.ToolRunCommand,
		Arguments:           typedArguments,
		WorkingDirectory:    cleanDirectory,
		Timeout:             timeout,
		ClaimedAuthority:    executor.AuthorityPrivileged,
		ExpectedSideEffects: effects,
		IdempotencyKey:      label,
		Requester:           "coordinator-verification:" + stage,
	}
	if err := executor.ValidateToolRequest(request); err != nil {
		return verificationCommandResult{}, err
	}
	policy := executor.PermissionPolicy{
		TaskID: taskID, WorktreePath: cleanWorktree, TaskActive: true,
		Grants: []executor.PermissionGrant{{
			ID: label,
			Pattern: executor.ActionPattern{
				TaskID: taskID, Tool: executor.ToolRunCommand,
				Arguments: arguments, WorkingDirectory: cleanDirectory,
				Effects: effects,
			},
			Mode:      executor.GrantAllowOnce,
			GrantedBy: "coordinator:verification",
			Reason:    "system verification stage: " + stage,
			GrantedAt: time.Now().UTC(),
		}},
	}
	classification, err := executor.ClassifyAuthority(request, policy)
	if err != nil {
		return verificationCommandResult{}, err
	}
	if classification.Outcome != executor.OutcomeAutomatic &&
		classification.Outcome != executor.OutcomeTaskScoped {
		// This is the refusal PIPE-133's abuse case exercises directly: a
		// working directory outside the worktree changes the action pattern
		// (WorkingDirectory is part of it) so it no longer matches the grant
		// minted above, and the command never starts.
		return verificationCommandResult{}, fmt.Errorf(
			"verification stage %q command was not authorized: %s",
			stage, classification.Reason)
	}
	result, err := executor.ExecuteAuthorizedTool(ctx, executor.AuthorizedToolRequest{
		Request: request, Classification: classification,
		WorktreePath:       cleanWorktree,
		Redactor:           redactor,
		AllowedEnvironment: allowedEnvironment,
		Stdin:              stdin,
	})
	if err != nil {
		return verificationCommandResult{}, err
	}
	var combined strings.Builder
	combined.WriteString(result.StdoutRedacted)
	combined.WriteString(result.StderrRedacted)
	return verificationCommandResult{
		Combined:  combined.String(),
		Succeeded: !result.TimedOut && !result.Cancelled && result.ExitCode == 0,
		TimedOut:  result.TimedOut,
	}, nil
}

// verificationRedactor returns the coordinator's own redaction pipeline when
// one was supplied, and otherwise builds a bounded default.
//
// NewAgentExecution refuses to build a coordinator with no redactor, so
// production code always has one. A test fixture built directly as
// &AgentExecution{} -- the pattern every existing test in this package
// already uses for checkMutations and checkAcceptance -- does not, and
// without this fallback every one of those tests would need to be rewritten
// to attach one before PIPE-131 could route their commands through the
// mediated boundary at all. The fallback still redacts; it just has no
// exact-match provider secrets loaded, which a bare test fixture never
// carries at rest anyway.
func (execution *AgentExecution) verificationRedactor() (*redact.Pipeline, error) {
	if execution.redactor != nil {
		return execution.redactor, nil
	}
	return redact.NewPipeline(nil, redact.Limits{
		MaximumInputBytes: 1 << 20, MaximumOutputBytes: 512 << 10,
	})
}
