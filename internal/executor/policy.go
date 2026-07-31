package executor

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

// PolicyOutcome is the complete authority decision.
type PolicyOutcome string

const (
	OutcomeAutomatic        PolicyOutcome = "automatic"
	OutcomeTaskScoped       PolicyOutcome = "task-scoped"
	OutcomeApprovalRequired PolicyOutcome = "approval-required"
	OutcomeDenied           PolicyOutcome = "denied"
)

// GrantMode distinguishes one execution from the remaining active task.
type GrantMode string

const (
	GrantAllowOnce    GrantMode = "allow-once"
	GrantAllowForTask GrantMode = "allow-for-task"
)

// ActionPattern is an exact executable action and scope identity.
type ActionPattern struct {
	TaskID           domain.TaskID
	Tool             ToolName
	Arguments        []string
	WorkingDirectory string
	Effects          []SideEffect
}

// PermissionGrant is one attributable exact grant.
type PermissionGrant struct {
	ID        string
	Pattern   ActionPattern
	Mode      GrantMode
	GrantedBy string
	Reason    string
	GrantedAt time.Time
	Used      bool
}

// PermissionDenial prevents tool substitution for the same capability/scope.
type PermissionDenial struct {
	TaskID     domain.TaskID
	Capability AuthorityClass
	ScopeHash  string
	DeniedBy   string
	Reason     string
	DeniedAt   time.Time
}

// PermissionPolicy is task-local and expires with task activity.
type PermissionPolicy struct {
	TaskID           domain.TaskID
	WorktreePath     string
	TaskActive       bool
	ApprovedCommands []ActionPattern
	Grants           []PermissionGrant
	Denials          []PermissionDenial
}

// AuthorityClassification is safe to persist and display.
type AuthorityClassification struct {
	Outcome        PolicyOutcome
	Required       AuthorityClass
	Capability     AuthorityClass
	ScopeHash      string
	MatchedGrantID string
	Description    string
	Reason         string
}

// ClassifyAuthority derives authority from tool/effects/arguments rather than
// trusting the request's purpose or claimed class.
func ClassifyAuthority(
	request ToolRequest,
	policy PermissionPolicy,
) (AuthorityClassification, error) {
	if err := ValidateToolRequest(request); err != nil {
		return AuthorityClassification{}, err
	}
	if policy.TaskID.IsZero() || request.TaskID != policy.TaskID {
		return AuthorityClassification{}, errors.New("permission policy belongs to a different task")
	}
	required := requiredAuthority(request)
	if !validAuthority(required) {
		return AuthorityClassification{}, errors.New("derived authority class is unknown")
	}
	pattern := actionPattern(request)
	scopeHash := hashActionPattern(pattern)
	classification := AuthorityClassification{
		Required: required, Capability: required, ScopeHash: scopeHash,
		Description: UserReadableToolSummary(request),
	}
	for _, denial := range policy.Denials {
		if denial.TaskID == request.TaskID &&
			denial.Capability == required &&
			(denial.ScopeHash == scopeHash || denial.ScopeHash == capabilityScopeHash(request, required)) {
			classification.Outcome = OutcomeDenied
			classification.Reason = "matching task authority was denied"
			return classification, nil
		}
	}
	if required == AuthorityAutomaticRead {
		classification.Outcome = OutcomeAutomatic
		classification.Reason = "bounded read-only repository action"
		return classification, nil
	}
	if required == AuthorityTaskWrite && withinWorktree(request.WorkingDirectory, policy.WorktreePath) {
		classification.Outcome = OutcomeTaskScoped
		classification.MatchedGrantID = "policy:task-worktree-write"
		classification.Reason = "bounded write inside the active task worktree"
		return classification, nil
	}
	if !policy.TaskActive {
		classification.Outcome = OutcomeDenied
		classification.Reason = "task-scoped authority expired with the task"
		return classification, nil
	}
	for _, approved := range policy.ApprovedCommands {
		if equalActionPattern(approved, pattern) {
			classification.Outcome = OutcomeAutomatic
			classification.Reason = "exact approved project command"
			return classification, nil
		}
	}
	for _, grant := range policy.Grants {
		if grant.Pattern.TaskID != request.TaskID || !equalActionPattern(grant.Pattern, pattern) {
			continue
		}
		if grant.Mode == GrantAllowOnce && grant.Used {
			continue
		}
		if grant.Mode != GrantAllowOnce && grant.Mode != GrantAllowForTask {
			continue
		}
		classification.Outcome = OutcomeTaskScoped
		classification.MatchedGrantID = grant.ID
		classification.Reason = "exact attributable permission grant"
		return classification, nil
	}
	classification.Outcome = OutcomeApprovalRequired
	classification.Reason = approvalReason(required)
	return classification, nil
}

// ActionSHA256 returns the exact action identity recorded with an authority
// decision. Changing any argument, scope, effect, tool, or task changes it.
func ActionSHA256(request ToolRequest) string {
	return hashActionPattern(actionPattern(request))
}

func requiredAuthority(request ToolRequest) AuthorityClass {
	effects := append([]SideEffect(nil), request.ExpectedSideEffects...)
	for _, candidate := range []struct {
		effect    SideEffect
		authority AuthorityClass
	}{
		{EffectExternalCommunication, AuthorityExternalCommunication},
		{EffectCredentialAccess, AuthorityCredential},
		{EffectPrivileged, AuthorityPrivileged},
		{EffectDestructive, AuthorityDestructive},
		{EffectExternalWrite, AuthorityExternalWrite},
		{EffectDependencyInstall, AuthorityDependencyInstall},
		{EffectNetwork, AuthorityNetwork},
	} {
		if slices.Contains(effects, candidate.effect) {
			return candidate.authority
		}
	}
	if request.Name == ToolRunCommand || request.Name == ToolPluginRPC {
		return classifyCommandArguments(argumentValues(request.Arguments))
	}
	switch request.Name {
	case ToolTest, ToolBuild, ToolFormat, ToolStaticAnalysis:
		return AuthorityPrivileged
	}
	if slices.Contains(effects, EffectTaskWorktreeWrite) {
		return AuthorityTaskWrite
	}
	return descriptorAuthority(request.Name)
}

// RequiredAuthority derives authority from the executable tool, ordered
// arguments, and declared effects. Caller claims never lower this value.
func RequiredAuthority(request ToolRequest) AuthorityClass {
	return requiredAuthority(request)
}

func classifyCommandArguments(arguments []string) AuthorityClass {
	if len(arguments) == 0 {
		return AuthorityPrivileged
	}
	executable := strings.ToLower(filepath.Base(arguments[0]))
	joined := strings.ToLower(strings.Join(arguments[1:], "\x00"))
	switch {
	case executable == "curl" || executable == "wget" ||
		executable == "ssh" || executable == "scp":
		return AuthorityNetwork
	case (executable == "go" && strings.HasPrefix(joined, "get\x00")) ||
		(executable == "npm" && strings.Contains(joined, "install")) ||
		(executable == "pip" && strings.Contains(joined, "install")):
		return AuthorityDependencyInstall
	case executable == "rm" || executable == "rmdir" ||
		(executable == "git" &&
			(strings.HasPrefix(joined, "reset\x00--hard") ||
				strings.HasPrefix(joined, "clean\x00"))):
		return AuthorityDestructive
	case executable == "sudo" || executable == "doas" ||
		executable == "runas":
		return AuthorityPrivileged
	case executable == "gh" && (strings.Contains(joined, "issue\x00create") ||
		strings.Contains(joined, "pr\x00create") ||
		strings.Contains(joined, "release\x00create")):
		return AuthorityExternalCommunication
	default:
		return AuthorityPrivileged
	}
}

func descriptorAuthority(name ToolName) AuthorityClass {
	for _, descriptor := range ToolCatalog() {
		if descriptor.Name == name {
			return descriptor.DefaultAuthority
		}
	}
	return ""
}

func actionPattern(request ToolRequest) ActionPattern {
	effects := append([]SideEffect(nil), request.ExpectedSideEffects...)
	slices.Sort(effects)
	return ActionPattern{
		TaskID: request.TaskID, Tool: request.Name,
		Arguments:        argumentValues(request.Arguments),
		WorkingDirectory: filepath.Clean(request.WorkingDirectory),
		Effects:          effects,
	}
}

func hashActionPattern(pattern ActionPattern) string {
	hash := sha256.New()
	hash.Write([]byte(pattern.TaskID.String()))
	hash.Write([]byte{0})
	hash.Write([]byte(pattern.Tool))
	hash.Write([]byte{0})
	hash.Write([]byte(pattern.WorkingDirectory))
	for _, argument := range pattern.Arguments {
		hash.Write([]byte{0})
		hash.Write([]byte(argument))
	}
	for _, effect := range pattern.Effects {
		hash.Write([]byte{0})
		hash.Write([]byte(effect))
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func capabilityScopeHash(request ToolRequest, authority AuthorityClass) string {
	hash := sha256.Sum256([]byte(
		request.TaskID.String() + "\x00" + string(authority) + "\x00" +
			filepath.Clean(request.WorkingDirectory),
	))
	return hex.EncodeToString(hash[:])
}

func equalActionPattern(left, right ActionPattern) bool {
	return left.TaskID == right.TaskID &&
		left.Tool == right.Tool &&
		filepath.Clean(left.WorkingDirectory) == filepath.Clean(right.WorkingDirectory) &&
		slices.Equal(left.Arguments, right.Arguments) &&
		slices.Equal(left.Effects, right.Effects)
}

func argumentValues(arguments []ToolArgument) []string {
	values := make([]string, len(arguments))
	for index, argument := range arguments {
		values[index] = argument.Value
	}
	return values
}

func withinWorktree(directory, worktree string) bool {
	if directory == "" || worktree == "" {
		return false
	}
	relative, err := filepath.Rel(filepath.Clean(worktree), filepath.Clean(directory))
	return err == nil && relative != ".." &&
		!strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func approvalReason(authority AuthorityClass) string {
	return fmt.Sprintf("%s authority requires an attributable user decision", authority)
}
