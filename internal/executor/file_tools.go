package executor

import (
	"context"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/redact"
)

// maximumFileToolBytes bounds what a file tool reads or writes.
//
// Both directions are bounded. A read is bounded because the content is put in
// front of a model and paid for by the token; a write is bounded because the
// content arrives from one.
const maximumFileToolBytes = 512 << 10

// IsFileTool reports whether a tool acts on files directly.
//
// These tools were declared in the catalog with real authority classes and had
// no implementation at all: ExecuteAuthorizedTool refuses anything that is not
// a subprocess, so an agent could run a test but could not read a file and
// could not write one. Nothing could be built.
func IsFileTool(name ToolName) bool {
	switch name {
	case ToolReadFile, ToolListDirectory, ToolApplyEdit:
		return true
	default:
		return false
	}
}

// ExecuteFileTool performs one authorized file action inside the worktree.
//
// It shares ExecuteAuthorizedTool's authority checks exactly: the same
// classification must be present, must match what the request actually asks
// for, and must carry attribution when it is task-scoped. A weaker check here
// would make the file tools the way around the policy.
func ExecuteFileTool(
	_ context.Context,
	authorized AuthorizedToolRequest,
) (ToolResult, error) {
	request := authorized.Request
	if err := ValidateToolRequest(request); err != nil {
		return ToolResult{}, err
	}
	if !IsFileTool(request.Name) {
		return ToolResult{}, errors.New("tool is not a file tool")
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
	if authorized.Redactor == nil {
		return ToolResult{}, errors.New("tool redactor is required")
	}

	started := time.Now()
	arguments := namedArguments(request.Arguments)
	target, err := resolveWorktreeFile(authorized.WorktreePath, arguments["path"])
	if err != nil {
		return fileToolFailure(request, started, err), nil
	}

	var output string
	switch request.Name {
	case ToolReadFile:
		output, err = readWorktreeFile(target)
	case ToolListDirectory:
		output, err = listWorktreeDirectory(target)
	case ToolApplyEdit:
		output, err = writeWorktreeFile(target, arguments["content"])
	}
	if err != nil {
		return fileToolFailure(request, started, err), nil
	}

	redacted, redactErr := redactFileToolText(authorized, output)
	if redactErr != nil {
		return ToolResult{}, redactErr
	}
	return ToolResult{
		RequestID: request.ID, SchemaVersion: ToolSchemaVersion,
		State: "succeeded", ExitCode: 0, Duration: time.Since(started),
		StdoutRedacted: redacted.text, StdoutTruncated: redacted.truncated,
		Summary: UserReadableToolSummary(request),
	}, nil
}

// fileToolFailure reports a refusal as a result rather than an error.
//
// A file the model asked for and did not get is feedback it can act on — it
// will try another path. Returning an error instead would end the round and
// tell it nothing.
func fileToolFailure(request ToolRequest, started time.Time, err error) ToolResult {
	return ToolResult{
		RequestID: request.ID, SchemaVersion: ToolSchemaVersion,
		State: "failed", ExitCode: 1, Duration: time.Since(started),
		StderrRedacted: err.Error(),
		Summary:        UserReadableToolSummary(request),
	}
}

// resolveWorktreeFile turns a caller-supplied relative path into a real one.
//
// The path comes from a model, so it is treated as hostile: absolute paths and
// parent traversal are refused outright, and the result is checked to still be
// inside the worktree after cleaning. This is the boundary that keeps a task's
// writes inside the copy it was given.
func resolveWorktreeFile(worktreePath, relative string) (string, error) {
	if !filepath.IsAbs(worktreePath) {
		return "", errors.New("the task worktree path is not absolute")
	}
	cleaned := strings.TrimSpace(relative)
	if cleaned == "" {
		return "", errors.New("a path is required")
	}
	if filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, "/") ||
		strings.Contains(cleaned, ":") {
		return "", fmt.Errorf("path %q must be relative to the worktree", relative)
	}
	target := filepath.Clean(filepath.Join(worktreePath, filepath.FromSlash(cleaned)))
	inside, err := filepath.Rel(filepath.Clean(worktreePath), target)
	if err != nil || inside == ".." ||
		strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the task worktree", relative)
	}
	return target, nil
}

// readWorktreeFile reads one bounded file.
func readWorktreeFile(target string) (string, error) {
	info, err := os.Stat(target)
	if err != nil {
		return "", fmt.Errorf("no file at that path")
	}
	if info.IsDir() {
		return "", errors.New("that path is a directory; use list-directory")
	}
	if info.Size() > maximumFileToolBytes {
		return "", fmt.Errorf(
			"the file is %d bytes, past the %d-byte read limit",
			info.Size(), maximumFileToolBytes)
	}
	content, err := os.ReadFile(target)
	if err != nil {
		return "", errors.New("the file could not be read")
	}
	return string(content), nil
}

// listWorktreeDirectory lists one directory.
//
// Directories are marked and the order is stable, because an agent comparing
// two listings across rounds must see a difference only when something actually
// changed.
func listWorktreeDirectory(target string) (string, error) {
	entries, err := os.ReadDir(target)
	if err != nil {
		return "", fmt.Errorf("no directory at that path")
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return "(empty directory)", nil
	}
	return strings.Join(names, "\n"), nil
}

// goSourceIsWellFormed reports whether content parses as a Go source file,
// and if not, why.
//
// Only the parse is checked, not whether the code is correct or compiles:
// undefined names, type errors and unused imports are the compiler's business
// and the flow's, and refusing them here would make this tool an opinion about
// code rather than a guard on the file.
func goSourceIsWellFormed(path, content string) error {
	// testdata is excluded by the same convention the Go toolchain uses: a
	// file under it is deliberately outside the build, and a fixture that does
	// not parse is sometimes exactly the point.
	slashed := filepath.ToSlash(path)
	if !strings.HasSuffix(slashed, ".go") ||
		slashed == "testdata" ||
		strings.HasPrefix(slashed, "testdata/") ||
		strings.Contains(slashed, "/testdata/") {
		return nil
	}
	if _, err := parser.ParseFile(
		token.NewFileSet(), path, content, parser.SkipObjectResolution,
	); err != nil {
		return fmt.Errorf(
			"this is a .go file and the content does not parse as Go: %s. "+
				"Write the file's source exactly, with no surrounding prose, "+
				"markdown fence, diff marker or heading", err)
	}
	return nil
}

// writeWorktreeFile writes one file, creating the directories it needs.
func writeWorktreeFile(target, content string) (string, error) {
	if len(content) > maximumFileToolBytes {
		return "", fmt.Errorf(
			"the content is %d bytes, past the %d-byte write limit",
			len(content), maximumFileToolBytes)
	}
	// A .go file that is not Go is refused here rather than written and
	// discovered by the build.
	//
	// Observed on ladder rung 2 twice in one run: the model wrote a file
	// beginning with an asterisk and the build failed with "expected
	// 'package', found '*'". Each occurrence cost a whole attempt — a write, a
	// build, a send-back and a full rewrite — and one of them was the last
	// attempt the run had, so the flow recorded every structural stage as
	// blocked because the module did not build.
	//
	// The content is refused, never repaired. Stripping a fence or a heading
	// here would hide the defect from the run that caused it and leave the
	// model no reason to stop producing it; the error says exactly what is
	// wrong and what to write instead, in the same round.
	if err := goSourceIsWellFormed(target, content); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", errors.New("the containing directory could not be created")
	}
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		return "", errors.New("the file could not be written")
	}
	return fmt.Sprintf("wrote %d bytes", len(content)), nil
}

// namedArguments indexes a request's arguments by name.
func namedArguments(arguments []ToolArgument) map[string]string {
	values := make(map[string]string, len(arguments))
	for _, argument := range arguments {
		values[argument.Name] = argument.Value
	}
	return values
}

// redactedText is bounded, redacted tool output.
type redactedText struct {
	text      string
	truncated bool
}

// redactFileToolText runs output through the same pipeline command output uses.
//
// File contents reach the event journal and the interface exactly as command
// output does, so a file holding a token must be redacted for the same reason.
func redactFileToolText(
	authorized AuthorizedToolRequest,
	text string,
) (redactedText, error) {
	result := redactedText{text: text}
	if len(result.text) > maximumFileToolBytes {
		result.text = result.text[:maximumFileToolBytes]
		result.truncated = true
	}
	// The same boundary command output uses, because file contents reach the
	// same journal and the same screen.
	redacted, err := authorized.Redactor.Redact(redact.BoundaryLogPersistence, result.text)
	if err != nil {
		return redactedText{}, fmt.Errorf("redact tool output: %w", err)
	}
	result.text = redacted.Text
	return result, nil
}
