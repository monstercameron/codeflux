package executor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/redact"
)

const redactedProgressChunkBytes = 4 << 10

// AuthorizedToolRequest is the worker-owned executable command boundary.
type AuthorizedToolRequest struct {
	Request                  ToolRequest
	Classification           AuthorityClassification
	WorktreePath             string
	ExpectedExecutable       string
	ExpectedExecutableSHA256 string
	Environment              map[string]string
	AllowedEnvironment       []string
	Redactor                 *redact.Pipeline
	Progress                 ToolProgressSink
	// Faults, when set, is consulted at the named injection points before an
	// irreversible step. It is an interface declared here rather than a test
	// package's type so production imports nothing test-only, and it is nil in
	// production.
	//
	// AUDIT-027: the fault vocabulary existed and no production boundary
	// consulted it, so every crash and recovery claim rested on a ledger that
	// never entered the code it described.
	Faults FaultInjector
}

// FaultInjector reports an injected fault at one named boundary.
//
// A nil injector never faults, which is what makes wiring this into a
// production path safe: the check is a nil comparison unless a test armed one.
type FaultInjector interface {
	// Check returns a non-nil error when a fault is armed at the point.
	Check(point string) error
}

// Named injection points inside mediated command execution.
//
// The strings match internal/testfixtures.FaultPoint values exactly, so the
// declared vocabulary and the wired boundaries cannot drift apart silently.
const (
	// FaultPointBeforeCommandStart fires after authorization and before the
	// process exists, which is where a crash leaves an approved-but-unstarted
	// command.
	FaultPointBeforeCommandStart = "worker-during-command-execution"
	// FaultPointAfterCommandStart fires with the process running, which is the
	// case that leaves a real orphan to reconcile.
	FaultPointAfterCommandStart = "worker-after-command-start"
)

// checkFault consults the injector when one is present.
func checkFault(faults FaultInjector, point string) error {
	if faults == nil {
		return nil
	}
	return faults.Check(point)
}

// ToolProgress is a bounded redacted lifecycle or output update.
type ToolProgress struct {
	RequestID string
	State     string
	Stream    string
	Content   string
}

// ToolProgressSink receives only lifecycle metadata or already-redacted text.
type ToolProgressSink interface {
	PublishToolProgress(context.Context, ToolProgress) error
}

// ExecuteAuthorizedTool runs one argument array in the task worktree. Callers
// must pass the current policy classification; this function never upgrades it.
func ExecuteAuthorizedTool(
	ctx context.Context,
	authorized AuthorizedToolRequest,
) (ToolResult, error) {
	request := authorized.Request
	if err := ValidateToolRequest(request); err != nil {
		return ToolResult{}, err
	}
	if authorized.Classification.Outcome != OutcomeAutomatic &&
		authorized.Classification.Outcome != OutcomeTaskScoped {
		return ToolResult{}, errors.New("tool request lacks executable authority")
	}
	if authorized.Classification.Outcome == OutcomeTaskScoped &&
		strings.TrimSpace(authorized.Classification.MatchedGrantID) == "" {
		return ToolResult{}, errors.New("task-scoped tool authority lacks attribution")
	}
	if authorized.Classification.Required != requiredAuthority(request) ||
		authorized.Classification.ScopeHash != hashActionPattern(actionPattern(request)) {
		return ToolResult{}, errors.New("tool authority classification does not match request")
	}
	if request.Name != ToolRunCommand && request.Name != ToolTest &&
		request.Name != ToolBuild && request.Name != ToolFormat &&
		request.Name != ToolStaticAnalysis && request.Name != ToolPluginRPC {
		return ToolResult{}, errors.New("tool is not a subprocess tool")
	}
	if authorized.Redactor == nil {
		return ToolResult{}, errors.New("tool redactor is required")
	}
	values := argumentValues(request.Arguments)
	if len(values) == 0 || strings.TrimSpace(values[0]) == "" {
		return ToolResult{}, errors.New("command executable is required")
	}
	worktree, directory, err := validateCommandDirectory(
		authorized.WorktreePath,
		request.WorkingDirectory,
	)
	if err != nil {
		return ToolResult{}, err
	}
	_ = worktree
	resolved, _, err := ResolveExecutableIdentity(values[0])
	if err != nil {
		return ToolResult{}, err
	}
	if authorized.ExpectedExecutable != "" || authorized.ExpectedExecutableSHA256 != "" {
		if authorized.ExpectedExecutable == "" || authorized.ExpectedExecutableSHA256 == "" ||
			!sameExecutablePath(resolved, authorized.ExpectedExecutable) {
			return ToolResult{}, errors.New("resolved executable differs from validation intent")
		}
		digest, digestErr := executableSHA256(resolved)
		if digestErr != nil {
			return ToolResult{}, digestErr
		}
		if digest != authorized.ExpectedExecutableSHA256 {
			return ToolResult{}, errors.New("executable content differs from validation intent")
		}
	}
	environment, err := minimalEnvironment(
		os.Environ(),
		authorized.Environment,
		authorized.AllowedEnvironment,
	)
	if err != nil {
		return ToolResult{}, err
	}
	stdout, err := authorized.Redactor.NewStream(redact.BoundaryLogPersistence)
	if err != nil {
		return ToolResult{}, err
	}
	stderr, err := authorized.Redactor.NewStream(redact.BoundaryLogPersistence)
	if err != nil {
		return ToolResult{}, err
	}
	// AUDIT-014: output is streamed while the process runs rather than only
	// after it exits. The durable redaction streams still receive every byte;
	// these writers tee into them.
	stdoutLive := newLiveProgressWriter(
		ctx, stdout, authorized, "stdout", redact.BoundaryLogPersistence)
	stderrLive := newLiveProgressWriter(
		ctx, stderr, authorized, "stderr", redact.BoundaryLogPersistence)

	command := exec.Command(resolved, values[1:]...)
	command.Dir = directory
	command.Env = environment
	command.Stdout = stdoutLive
	command.Stderr = stderrLive
	prepareProcessTree(command)

	// The last point before the command becomes real. A fault here must leave
	// no process behind, which is what distinguishes it from the point below.
	if err := checkFault(authorized.Faults, FaultPointBeforeCommandStart); err != nil {
		return ToolResult{}, err
	}

	started := time.Now()
	if err := command.Start(); err != nil {
		return ToolResult{}, fmt.Errorf("start command: %w", err)
	}
	publishProgress(ctx, authorized.Progress, ToolProgress{
		RequestID: request.ID, State: "running",
	})

	// A fault with the process already running must not strand it. The tree is
	// terminated before returning, so an injected crash here reproduces a
	// worker dying mid-command rather than leaking one into the test host.
	if err := checkFault(authorized.Faults, FaultPointAfterCommandStart); err != nil {
		_ = terminateProcessTree(command)
		_ = command.Wait()
		return ToolResult{}, err
	}

	waited := make(chan error, 1)
	go func() {
		waited <- command.Wait()
	}()
	timer := time.NewTimer(request.Timeout)
	defer timer.Stop()
	var (
		waitErr   error
		timedOut  bool
		cancelled bool
	)
	select {
	case waitErr = <-waited:
	case <-ctx.Done():
		cancelled = true
		_ = terminateProcessTree(command)
		waitErr = <-waited
	case <-timer.C:
		timedOut = true
		_ = terminateProcessTree(command)
		waitErr = <-waited
	}
	stdoutLive.flush()
	stderrLive.flush()
	stdoutResult, stdoutErr := stdout.Finalize()
	stderrResult, stderrErr := stderr.Finalize()
	if stdoutErr != nil || stderrErr != nil {
		return ToolResult{}, errors.Join(stdoutErr, stderrErr)
	}
	exitCode := -1
	if command.ProcessState != nil {
		exitCode = command.ProcessState.ExitCode()
	}
	state := "succeeded"
	if timedOut || cancelled {
		state = "cancelled"
	} else if waitErr != nil || exitCode != 0 {
		state = "failed"
	}
	result := ToolResult{
		RequestID: request.ID, SchemaVersion: ToolSchemaVersion, State: state,
		Executable: values[0], ResolvedExecutable: resolved, ExitCode: exitCode,
		Duration: time.Since(started), TimedOut: timedOut, Cancelled: cancelled,
		StdoutRedacted: stdoutResult.Text, StderrRedacted: stderrResult.Text,
		StdoutTruncated: stdoutResult.Report.InputTruncated ||
			stdoutResult.Report.OutputTruncated,
		StderrTruncated: stderrResult.Report.InputTruncated ||
			stderrResult.Report.OutputTruncated,
	}
	result.Summary = fmt.Sprintf(
		"%s exited %d after %s (timeout=%t cancellation=%t stdout-truncated=%t stderr-truncated=%t)",
		filepath.Base(resolved),
		exitCode,
		result.Duration.Round(time.Millisecond),
		timedOut,
		cancelled,
		result.StdoutTruncated,
		result.StderrTruncated,
	)
	// Output already streamed while the process ran, so republishing the whole
	// finalized text would duplicate it in the timeline. What is published
	// here is only what streaming could not carry: the part the budget
	// withheld, named as withheld rather than silently missing.
	reportWithheldOutput(ctx, authorized.Progress, request.ID, "stdout", stdoutLive)
	reportWithheldOutput(ctx, authorized.Progress, request.ID, "stderr", stderrLive)
	publishProgress(ctx, authorized.Progress, ToolProgress{
		RequestID: request.ID, State: state,
	})
	return result, nil
}

// ResolveExecutableIdentity returns the exact canonical executable path and
// content digest that a mediated command will verify again before process
// start. The digest is safe metadata; executable contents are never retained.
func ResolveExecutableIdentity(executable string) (string, string, error) {
	if strings.TrimSpace(executable) == "" {
		return "", "", errors.New("command executable is required")
	}
	resolved, err := exec.LookPath(executable)
	if err != nil {
		return "", "", fmt.Errorf("resolve command executable: %w", err)
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return "", "", fmt.Errorf("canonicalize command executable: %w", err)
	}
	resolved, err = filepath.EvalSymlinks(resolved)
	if err != nil {
		return "", "", fmt.Errorf("resolve command executable links: %w", err)
	}
	digest, err := executableSHA256(resolved)
	if err != nil {
		return "", "", err
	}
	return resolved, digest, nil
}

func executableSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("hash command executable: %w", err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("hash command executable: %w", err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func sameExecutablePath(left, right string) bool {
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func validateCommandDirectory(worktreePath, directory string) (string, string, error) {
	if !filepath.IsAbs(worktreePath) || !filepath.IsAbs(directory) {
		return "", "", errors.New("worktree and command directory must be absolute")
	}
	worktree, err := filepath.EvalSymlinks(filepath.Clean(worktreePath))
	if err != nil {
		return "", "", fmt.Errorf("resolve command worktree: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(filepath.Clean(directory))
	if err != nil {
		return "", "", fmt.Errorf("resolve command directory: %w", err)
	}
	relative, err := filepath.Rel(worktree, resolved)
	if err != nil || relative == ".." ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", "", errors.New("command directory escaped the task worktree")
	}
	return worktree, resolved, nil
}

func minimalEnvironment(
	parent []string,
	requested map[string]string,
	allowed []string,
) ([]string, error) {
	allow := map[string]struct{}{
		"PATH": {}, "PATHEXT": {}, "SYSTEMROOT": {}, "WINDIR": {},
		"COMSPEC": {}, "TMP": {}, "TEMP": {}, "TMPDIR": {},
		"HOME": {}, "USERPROFILE": {}, "LOCALAPPDATA": {},
	}
	for _, name := range allowed {
		normalized := strings.ToUpper(strings.TrimSpace(name))
		if normalized == "" || sensitiveEnvironmentName(normalized) {
			return nil, errors.New("environment allowlist contains a sensitive name")
		}
		allow[normalized] = struct{}{}
	}
	values := make(map[string]string)
	for _, entry := range parent {
		name, value, found := strings.Cut(entry, "=")
		normalized := strings.ToUpper(name)
		if !found || sensitiveEnvironmentName(normalized) {
			continue
		}
		if _, ok := allow[normalized]; ok {
			values[normalized] = value
		}
	}
	for name, value := range requested {
		normalized := strings.ToUpper(strings.TrimSpace(name))
		if sensitiveEnvironmentName(normalized) {
			return nil, errors.New("command environment contains a sensitive name")
		}
		if _, ok := allow[normalized]; !ok {
			return nil, errors.New("command environment name is not allowlisted")
		}
		if strings.ContainsRune(value, 0) {
			return nil, errors.New("command environment value is invalid")
		}
		values[normalized] = value
	}
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]string, 0, len(names))
	for _, name := range names {
		result = append(result, name+"="+values[name])
	}
	return result, nil
}

func sensitiveEnvironmentName(name string) bool {
	switch name {
	case "OPENAI_API_KEY", "ANTHROPIC_API_KEY", "AZURE_OPENAI_API_KEY",
		"GITHUB_TOKEN", "GITLAB_TOKEN", "BITBUCKET_TOKEN",
		"AWS_SECRET_ACCESS_KEY", "GOOGLE_API_KEY":
		return true
	}
	for _, suffix := range []string{
		"_API_KEY", "_ACCESS_TOKEN", "_AUTH_TOKEN", "_BEARER_TOKEN",
		"_SECRET", "_PASSWORD",
	} {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return strings.HasPrefix(name, "CODEFLUX_CREDENTIAL_")
}

func publishProgress(ctx context.Context, sink ToolProgressSink, progress ToolProgress) {
	if sink != nil {
		_ = sink.PublishToolProgress(ctx, progress)
	}
}
