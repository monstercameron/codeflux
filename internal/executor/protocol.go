package executor

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

const ToolSchemaVersion = 1

// ToolName is one versioned internal tool contract.
type ToolName string

const (
	ToolReadFile      ToolName = "read-file"
	ToolListDirectory ToolName = "list-directory"
	ToolSearchText    ToolName = "search-text"
	ToolSearchSymbol  ToolName = "search-symbol"
	// ToolApplyEdit writes a whole file. Named apply-edit for compatibility
	// with every record already written; what it does is described by its
	// summary, which is what a model actually reads.
	ToolApplyEdit ToolName = "apply-edit"
	// ToolApplyPatch changes part of an existing file.
	//
	// The default for refinement. A whole-file write to change one error
	// message replaces two or three thousand bytes to move ten, and every byte
	// it replaces is a chance to drop a function, drift a signature, or reword
	// an acceptance-sensitive literal by accident — which is what ladder rungs
	// 5 and 6 spent most of their attempts undoing.
	ToolApplyPatch     ToolName = "apply-patch"
	ToolInspectDiff    ToolName = "inspect-diff"
	ToolRunCommand     ToolName = "run-command"
	ToolGitStatus      ToolName = "git-status"
	ToolGitHistory     ToolName = "git-history"
	ToolTest           ToolName = "test"
	ToolBuild          ToolName = "build"
	ToolFormat         ToolName = "format"
	ToolStaticAnalysis ToolName = "static-analysis"
	ToolPluginRPC      ToolName = "plugin-rpc"
)

// AuthorityClass is the non-inferable authority required by an effect.
type AuthorityClass string

const (
	AuthorityAutomaticRead         AuthorityClass = "automatic-read"
	AuthorityTaskWrite             AuthorityClass = "task-write"
	AuthorityNetwork               AuthorityClass = "network"
	AuthorityDependencyInstall     AuthorityClass = "dependency-install"
	AuthorityExternalWrite         AuthorityClass = "external-write"
	AuthorityCredential            AuthorityClass = "credential"
	AuthorityDestructive           AuthorityClass = "destructive"
	AuthorityPrivileged            AuthorityClass = "privileged"
	AuthorityExternalCommunication AuthorityClass = "external-communication"
)

// SideEffect is one declared and policy-classified effect.
type SideEffect string

const (
	EffectRepositoryRead        SideEffect = "repository-read"
	EffectTaskWorktreeWrite     SideEffect = "task-worktree-write"
	EffectNetwork               SideEffect = "network"
	EffectDependencyInstall     SideEffect = "dependency-install"
	EffectExternalWrite         SideEffect = "external-write"
	EffectCredentialAccess      SideEffect = "credential-access"
	EffectDestructive           SideEffect = "destructive"
	EffectPrivileged            SideEffect = "privileged"
	EffectExternalCommunication SideEffect = "external-communication"
	EffectSubprocess            SideEffect = "subprocess"
)

// ToolArgument preserves array ordering and per-value redaction metadata.
type ToolArgument struct {
	Name      string
	Value     string
	Sensitive bool
}

// ToolRequest is the complete typed execution request.
type ToolRequest struct {
	SchemaVersion       int
	ID                  string
	TaskID              domain.TaskID
	RunID               domain.RunID
	Name                ToolName
	Arguments           []ToolArgument
	WorkingDirectory    string
	Timeout             time.Duration
	ClaimedAuthority    AuthorityClass
	ExpectedSideEffects []SideEffect
	IdempotencyKey      string
	Requester           string
	PurposeUntrusted    string
}

// ToolResult preserves execution facts independently from output content.
type ToolResult struct {
	RequestID          string
	SchemaVersion      int
	State              string
	Executable         string
	ResolvedExecutable string
	ExitCode           int
	Duration           time.Duration
	TimedOut           bool
	Cancelled          bool
	StdoutRedacted     string
	StderrRedacted     string
	StdoutTruncated    bool
	StderrTruncated    bool
	Summary            string
}

// ToolDescriptor is one user-visible versioned tool schema.
type ToolDescriptor struct {
	Name             ToolName
	SchemaVersion    int
	Summary          string
	DefaultAuthority AuthorityClass
	Effects          []SideEffect
}

// ToolCatalog returns every supported tool in stable name order.
func ToolCatalog() []ToolDescriptor {
	result := []ToolDescriptor{
		{Name: ToolReadFile, SchemaVersion: ToolSchemaVersion, Summary: "Read one bounded task file", DefaultAuthority: AuthorityAutomaticRead, Effects: []SideEffect{EffectRepositoryRead}},
		{Name: ToolListDirectory, SchemaVersion: ToolSchemaVersion, Summary: "List one bounded task directory", DefaultAuthority: AuthorityAutomaticRead, Effects: []SideEffect{EffectRepositoryRead}},
		{Name: ToolSearchText, SchemaVersion: ToolSchemaVersion, Summary: "Search bounded repository text", DefaultAuthority: AuthorityAutomaticRead, Effects: []SideEffect{EffectRepositoryRead}},
		{Name: ToolSearchSymbol, SchemaVersion: ToolSchemaVersion, Summary: "Search the revision-bound symbol map", DefaultAuthority: AuthorityAutomaticRead, Effects: []SideEffect{EffectRepositoryRead}},
		// The summary is what a model reads before deciding what to send, and
		// "apply an expected-hash edit batch" reads like a patch API. It got
		// what it asked for: ladder rung 6 sent "*** Begin Patch" three times
		// and "@@" once across four turns, each refused, each a wasted turn.
		// The tool takes a whole file, so the summary says a whole file.
		{Name: ToolApplyEdit, SchemaVersion: ToolSchemaVersion, Summary: "Create a file, or replace one wholesale: takes the complete new contents, not a patch. Prefer apply-patch to change an existing file — a second wholesale rewrite of the same file in one attempt is refused, because re-sending every line to move a few risks every line it re-sends", DefaultAuthority: AuthorityTaskWrite, Effects: []SideEffect{EffectTaskWorktreeWrite}},
		{Name: ToolApplyPatch, SchemaVersion: ToolSchemaVersion, Summary: "Change part of an existing file. Takes a diff: \"*** Update File: <path>\", then \"@@\", then lines to remove prefixed \"-\", lines to add prefixed \"+\", and unprefixed context lines around them. Every hunk needs at least two unprefixed lines above and below the change, copied exactly from the file: a hunk that is only the line you are replacing cannot say which line it means, and is the single commonest way a patch fails. One file per call: a patch naming a second file is refused, so send a separate call for each file you change", DefaultAuthority: AuthorityTaskWrite, Effects: []SideEffect{EffectTaskWorktreeWrite}},
		{Name: ToolInspectDiff, SchemaVersion: ToolSchemaVersion, Summary: "Inspect the exact task diff", DefaultAuthority: AuthorityAutomaticRead, Effects: []SideEffect{EffectRepositoryRead}},
		{Name: ToolRunCommand, SchemaVersion: ToolSchemaVersion, Summary: "Run one mediated argument-array command", DefaultAuthority: AuthorityPrivileged, Effects: []SideEffect{EffectSubprocess}},
		{Name: ToolGitStatus, SchemaVersion: ToolSchemaVersion, Summary: "Inspect task Git status", DefaultAuthority: AuthorityAutomaticRead, Effects: []SideEffect{EffectRepositoryRead}},
		{Name: ToolGitHistory, SchemaVersion: ToolSchemaVersion, Summary: "Inspect bounded task Git history", DefaultAuthority: AuthorityAutomaticRead, Effects: []SideEffect{EffectRepositoryRead}},
		{Name: ToolTest, SchemaVersion: ToolSchemaVersion, Summary: "Run the project's test suite — for Go, executable \"go\" with arg1 \"test\" and arg2 \"./...\". Running it against files that have not changed since the last run is answered from that run rather than executed, and asking a third time in a row is refused: write the change first, then run it", DefaultAuthority: AuthorityAutomaticRead, Effects: []SideEffect{EffectSubprocess, EffectRepositoryRead}},
		{Name: ToolBuild, SchemaVersion: ToolSchemaVersion, Summary: "Run an approved build recipe", DefaultAuthority: AuthorityAutomaticRead, Effects: []SideEffect{EffectSubprocess, EffectRepositoryRead}},
		{Name: ToolFormat, SchemaVersion: ToolSchemaVersion, Summary: "Run an approved formatter recipe", DefaultAuthority: AuthorityTaskWrite, Effects: []SideEffect{EffectSubprocess, EffectTaskWorktreeWrite}},
		{Name: ToolStaticAnalysis, SchemaVersion: ToolSchemaVersion, Summary: "Run an approved static-analysis recipe", DefaultAuthority: AuthorityAutomaticRead, Effects: []SideEffect{EffectSubprocess, EffectRepositoryRead}},
		{Name: ToolPluginRPC, SchemaVersion: ToolSchemaVersion, Summary: "Invoke a separately launched plugin protocol", DefaultAuthority: AuthorityPrivileged, Effects: []SideEffect{EffectSubprocess}},
	}
	slices.SortFunc(result, func(left, right ToolDescriptor) int {
		return strings.Compare(string(left.Name), string(right.Name))
	})
	return result
}

func ValidateToolRequest(request ToolRequest) error {
	switch {
	case request.SchemaVersion != ToolSchemaVersion:
		return errors.New("tool schema version is unsupported")
	case request.ID == "" || len(request.ID) > 255:
		return errors.New("tool request ID is invalid")
	case request.TaskID.IsZero():
		return errors.New("tool task ID is required")
	case request.RunID.IsZero():
		return errors.New("tool run ID is required")
	case !validToolName(request.Name):
		return errors.New("tool name is unsupported")
	case request.WorkingDirectory == "" || len(request.WorkingDirectory) > 4096:
		return errors.New("tool working directory is invalid")
	case request.Timeout < time.Second || request.Timeout > 30*time.Minute:
		return errors.New("tool timeout is outside supported bounds")
	case request.IdempotencyKey == "" || len(request.IdempotencyKey) > 255:
		return errors.New("tool idempotency key is invalid")
	case request.Requester == "" || len(request.Requester) > 255:
		return errors.New("tool requester is invalid")
	case !validAuthority(request.ClaimedAuthority):
		return errors.New("tool authority class is unknown")
	case len(request.ExpectedSideEffects) == 0 || len(request.ExpectedSideEffects) > 16:
		return errors.New("tool side effects are invalid")
	}
	for _, argument := range request.Arguments {
		if argument.Name == "" || len(argument.Name) > 255 ||
			len(argument.Value) > 32<<10 {
			return errors.New("tool argument is invalid")
		}
	}
	for _, effect := range request.ExpectedSideEffects {
		if !validSideEffect(effect) {
			return errors.New("tool side effect is unknown")
		}
	}
	return nil
}

// UserReadableToolSummary is display-only and cannot change execution fields.
func UserReadableToolSummary(request ToolRequest) string {
	arguments := make([]string, 0, len(request.Arguments))
	for _, argument := range request.Arguments {
		value := argument.Value
		if argument.Sensitive {
			value = "[REDACTED]"
		}
		arguments = append(arguments,
			argument.Name+"="+summarizeArgumentValue(value))
	}
	purpose := strings.ReplaceAll(strings.ReplaceAll(request.PurposeUntrusted, "\r", " "), "\n", " ")
	if len(purpose) > 256 {
		purpose = purpose[:256]
	}
	return fmt.Sprintf(
		"%s in %s with [%s]; stated purpose (untrusted): %q",
		request.Name,
		path.Clean(strings.ReplaceAll(request.WorkingDirectory, "\\", "/")),
		strings.Join(arguments, ", "),
		purpose,
	)
}

func validToolName(name ToolName) bool {
	for _, descriptor := range ToolCatalog() {
		if descriptor.Name == name {
			return true
		}
	}
	return false
}

func validAuthority(authority AuthorityClass) bool {
	switch authority {
	case AuthorityAutomaticRead, AuthorityTaskWrite, AuthorityNetwork,
		AuthorityDependencyInstall, AuthorityExternalWrite, AuthorityCredential,
		AuthorityDestructive, AuthorityPrivileged, AuthorityExternalCommunication:
		return true
	default:
		return false
	}
}

func validSideEffect(effect SideEffect) bool {
	switch effect {
	case EffectRepositoryRead, EffectTaskWorktreeWrite, EffectNetwork,
		EffectDependencyInstall, EffectExternalWrite, EffectCredentialAccess,
		EffectDestructive, EffectPrivileged, EffectExternalCommunication,
		EffectSubprocess:
		return true
	default:
		return false
	}
}

// maximumSummarizedArgumentBytes is how much of one argument value appears in a
// tool summary.
//
// This summary is not only for a person. It travels back into the next prompt
// as the record of what the last round did, so a written file's entire contents
// were being echoed into every subsequent request — two or three thousand bytes
// per edit, repeated for the rest of the run, paid for by the token and
// crowding out the instruction.
//
// What the next round needs is that the write happened, to what, and how big it
// was. If it needs the contents it can read the file, which is what the read
// tool is for and costs the same tokens once instead of every round.
const maximumSummarizedArgumentBytes = 160

// summarizeArgumentValue renders one argument, shortened when it is large.
//
// The digest is included so two rounds can be compared: a run that wrote the
// same bytes twice shows the same digest, which is a fact worth having and one
// the truncated text alone would hide.
func summarizeArgumentValue(value string) string {
	if len(value) <= maximumSummarizedArgumentBytes {
		return fmt.Sprintf("%q", value)
	}
	digest := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%q… (%d bytes, sha256 %s)",
		value[:maximumSummarizedArgumentBytes], len(value),
		hex.EncodeToString(digest[:])[:12])
}
