package coordinator

import (
	"context"
	"errors"
	"fmt"

	agentloop "codeflux.dev/codeflux/internal/agent"
	"codeflux.dev/codeflux/internal/executor"
	"codeflux.dev/codeflux/internal/redact"
)

// PolicyAuthorityRouter decides what one proposed tool call is allowed to do.
//
// It derives the authority from the tool, its effects, and its arguments rather
// than from anything the model said about itself: a request's claimed class and
// stated purpose are both untrusted input, and ClassifyAuthority already
// ignores them. This type exists because the agent loop declared the port and
// nothing implemented it, so no tool call could ever be authorized.
type PolicyAuthorityRouter struct {
	policy executor.PermissionPolicy
	// revision and digest identify the execution policy this router decides
	// under. They are recorded on every decision so a run's authorizations can
	// be read back against the exact policy that granted them, rather than
	// against whatever the policy says today.
	revision uint64
	digest   string
}

// NewPolicyAuthorityRouter binds a router to one task's policy.
func NewPolicyAuthorityRouter(
	policy executor.PermissionPolicy,
	revision uint64,
	digest string,
) (*PolicyAuthorityRouter, error) {
	if policy.TaskID.IsZero() {
		return nil, errors.New("an authority router needs a policy bound to a task")
	}
	if revision == 0 {
		return nil, errors.New("an authority router needs the execution policy revision")
	}
	return &PolicyAuthorityRouter{policy: policy, revision: revision, digest: digest}, nil
}

// RouteTool classifies one request.
func (router *PolicyAuthorityRouter) RouteTool(
	_ context.Context,
	request executor.ToolRequest,
) (agentloop.ToolAuthorization, error) {
	classification, err := executor.ClassifyAuthority(request, router.policy)
	if err != nil {
		return agentloop.ToolAuthorization{}, err
	}
	return agentloop.ToolAuthorization{
		Classification: classification,
		DecisionID:     classification.ScopeHash,
		PolicyRevision: router.revision,
		PolicySHA256:   router.digest,
	}, nil
}

// WorktreeToolExecutor runs an authorized tool inside one task's worktree.
//
// The worktree path is held here rather than taken from the request, because a
// request that could name its own working directory could name any directory.
// This is the boundary that makes "the agent works in an isolated copy" a fact
// rather than an instruction.
type WorktreeToolExecutor struct {
	worktreePath string
	redactor     *redact.Pipeline
	progress     executor.ToolProgressSink
}

// NewWorktreeToolExecutor builds the executor.
func NewWorktreeToolExecutor(
	worktreePath string,
	redactor *redact.Pipeline,
	progress executor.ToolProgressSink,
) (*WorktreeToolExecutor, error) {
	if worktreePath == "" {
		return nil, errors.New("a tool executor needs the task's worktree path")
	}
	if redactor == nil {
		// Tool output reaches the event journal and the interface. Without a
		// redactor a command that echoed a token would publish it.
		return nil, errors.New("a tool executor needs a redaction pipeline")
	}
	return &WorktreeToolExecutor{
		worktreePath: worktreePath, redactor: redactor, progress: progress,
	}, nil
}

// ExecuteTool runs one authorized request.
func (toolExecutor *WorktreeToolExecutor) ExecuteTool(
	ctx context.Context,
	request executor.AuthorizedToolRequest,
) (executor.ToolResult, error) {
	if request.WorktreePath != "" && request.WorktreePath != toolExecutor.worktreePath {
		return executor.ToolResult{}, fmt.Errorf(
			"a tool request named a worktree that is not this task's")
	}
	request.WorktreePath = toolExecutor.worktreePath
	request.Redactor = toolExecutor.redactor
	if request.Progress == nil {
		request.Progress = toolExecutor.progress
	}
	// Reading, listing, and writing act on files directly; everything else is a
	// mediated subprocess. They are split because ExecuteAuthorizedTool runs an
	// argument array with no shell and no standard input, which can run a test
	// but cannot write a file — so an agent could validate work it had no way
	// to produce.
	if executor.IsFileTool(request.Request.Name) {
		return executor.ExecuteFileTool(ctx, request)
	}
	return executor.ExecuteAuthorizedTool(ctx, request)
}

var (
	_ agentloop.AuthorityRouter = (*PolicyAuthorityRouter)(nil)
	_ agentloop.ToolExecutor    = (*WorktreeToolExecutor)(nil)
)
