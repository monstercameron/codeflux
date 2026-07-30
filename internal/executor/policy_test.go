package executor

import (
	"path/filepath"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
)

func TestPermissionPolicyClassifiesAutomaticTaskAndApprovalActions(t *testing.T) {
	t.Parallel()

	worktree := filepath.Join(t.TempDir(), "task")
	taskID := fixtureExecutorTaskID(t, 10)
	policy := PermissionPolicy{
		TaskID: taskID, WorktreePath: worktree, TaskActive: true,
	}

	read := fixtureToolRequest(t, ToolReadFile)
	read.TaskID = taskID
	read.WorkingDirectory = worktree
	assertPolicyOutcome(t, read, policy, OutcomeAutomatic, AuthorityAutomaticRead)

	edit := read
	edit.Name = ToolApplyEdit
	edit.ClaimedAuthority = AuthorityTaskWrite
	edit.ExpectedSideEffects = []SideEffect{EffectTaskWorktreeWrite}
	assertPolicyOutcome(t, edit, policy, OutcomeTaskScoped, AuthorityTaskWrite)

	outside := edit
	outside.WorkingDirectory = filepath.Dir(worktree)
	assertPolicyOutcome(t, outside, policy, OutcomeApprovalRequired, AuthorityTaskWrite)

	network := commandRequest(t, taskID, worktree, "curl", "https://example.invalid")
	assertPolicyOutcome(t, network, policy, OutcomeApprovalRequired, AuthorityNetwork)

	install := commandRequest(t, taskID, worktree, "go", "get", "example.invalid/module")
	assertPolicyOutcome(t, install, policy, OutcomeApprovalRequired, AuthorityDependencyInstall)

	destructive := commandRequest(t, taskID, worktree, "git", "reset", "--hard", "HEAD")
	assertPolicyOutcome(t, destructive, policy, OutcomeApprovalRequired, AuthorityDestructive)
}

func TestPermissionPolicyRequiresExactGrantAndExpiresWithTask(t *testing.T) {
	t.Parallel()

	worktree := filepath.Join(t.TempDir(), "task")
	taskID := fixtureExecutorTaskID(t, 20)
	request := commandRequest(t, taskID, worktree, "curl", "https://example.invalid")
	pattern := actionPattern(request)
	policy := PermissionPolicy{
		TaskID: taskID, WorktreePath: worktree, TaskActive: true,
		Grants: []PermissionGrant{{
			ID: "grant-one", Pattern: pattern, Mode: GrantAllowForTask,
			GrantedBy: "user", Reason: "fetch fixture",
		}},
	}
	classification, err := ClassifyAuthority(request, policy)
	if err != nil {
		t.Fatal(err)
	}
	if classification.Outcome != OutcomeTaskScoped ||
		classification.MatchedGrantID != "grant-one" {
		t.Fatalf("exact grant classification = %#v", classification)
	}
	changed := request
	changed.Arguments = append([]ToolArgument(nil), request.Arguments...)
	changed.Arguments[1].Value = "https://other.invalid"
	assertPolicyOutcome(t, changed, policy, OutcomeApprovalRequired, AuthorityNetwork)

	policy.Grants[0].Mode = GrantAllowOnce
	policy.Grants[0].Used = true
	assertPolicyOutcome(t, request, policy, OutcomeApprovalRequired, AuthorityNetwork)

	policy.Grants[0].Mode = GrantAllowForTask
	policy.Grants[0].Used = false
	policy.TaskActive = false
	assertPolicyOutcome(t, request, policy, OutcomeDenied, AuthorityNetwork)

	otherTask := request
	otherTask.TaskID = fixtureExecutorTaskID(t, 21)
	if _, err := ClassifyAuthority(otherTask, policy); err == nil {
		t.Fatal("grant leaked to an unrelated task")
	}
}

func TestPermissionDenialCannotBeRegainedThroughToolSubstitution(t *testing.T) {
	t.Parallel()

	worktree := filepath.Join(t.TempDir(), "task")
	taskID := fixtureExecutorTaskID(t, 30)
	network := commandRequest(t, taskID, worktree, "curl", "https://example.invalid")
	policy := PermissionPolicy{
		TaskID: taskID, WorktreePath: worktree, TaskActive: true,
		Denials: []PermissionDenial{{
			TaskID: taskID, Capability: AuthorityNetwork,
			ScopeHash: capabilityScopeHash(network, AuthorityNetwork),
			DeniedBy:  "user", Reason: "offline task",
		}},
	}
	assertPolicyOutcome(t, network, policy, OutcomeDenied, AuthorityNetwork)

	substitution := fixtureToolRequest(t, ToolPluginRPC)
	substitution.TaskID = taskID
	substitution.WorkingDirectory = worktree
	substitution.ClaimedAuthority = AuthorityPrivileged
	substitution.ExpectedSideEffects = []SideEffect{EffectSubprocess, EffectNetwork}
	substitution.Arguments = []ToolArgument{{Name: "plugin", Value: "network-helper"}}
	assertPolicyOutcome(t, substitution, policy, OutcomeDenied, AuthorityNetwork)
}

func TestApprovedProjectCommandRequiresExactArray(t *testing.T) {
	t.Parallel()

	worktree := filepath.Join(t.TempDir(), "task")
	taskID := fixtureExecutorTaskID(t, 40)
	request := fixtureToolRequest(t, ToolTest)
	request.TaskID = taskID
	request.WorkingDirectory = worktree
	request.ClaimedAuthority = AuthorityAutomaticRead
	request.ExpectedSideEffects = []SideEffect{EffectSubprocess, EffectRepositoryRead}
	request.Arguments = []ToolArgument{
		{Name: "executable", Value: "go"},
		{Name: "argument", Value: "test"},
		{Name: "argument", Value: "./..."},
	}
	policy := PermissionPolicy{
		TaskID: taskID, WorktreePath: worktree, TaskActive: true,
		ApprovedCommands: []ActionPattern{actionPattern(request)},
	}
	assertPolicyOutcome(t, request, policy, OutcomeAutomatic, AuthorityPrivileged)
	changed := request
	changed.Arguments = append([]ToolArgument(nil), request.Arguments...)
	changed.Arguments = append(changed.Arguments, ToolArgument{Name: "argument", Value: "-run=Malicious"})
	assertPolicyOutcome(t, changed, policy, OutcomeApprovalRequired, AuthorityPrivileged)
}

func commandRequest(
	t *testing.T,
	taskID interface{ String() string },
	worktree string,
	arguments ...string,
) ToolRequest {
	t.Helper()
	parsedTask, err := domain.ParseTaskID(taskID.String())
	if err != nil {
		t.Fatal(err)
	}
	request := fixtureToolRequest(t, ToolRunCommand)
	request.TaskID = parsedTask
	request.WorkingDirectory = worktree
	request.ClaimedAuthority = AuthorityPrivileged
	request.ExpectedSideEffects = []SideEffect{EffectSubprocess}
	request.Arguments = make([]ToolArgument, len(arguments))
	for index, argument := range arguments {
		request.Arguments[index] = ToolArgument{Name: "argument", Value: argument}
	}
	return request
}

func assertPolicyOutcome(
	t *testing.T,
	request ToolRequest,
	policy PermissionPolicy,
	outcome PolicyOutcome,
	authority AuthorityClass,
) {
	t.Helper()
	classification, err := ClassifyAuthority(request, policy)
	if err != nil {
		t.Fatal(err)
	}
	if classification.Outcome != outcome || classification.Required != authority ||
		classification.Description == "" || classification.ScopeHash == "" {
		t.Fatalf("classification = %#v, want %s/%s", classification, outcome, authority)
	}
}
