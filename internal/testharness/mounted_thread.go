package testharness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	agentloop "codeflux.dev/codeflux/internal/agent"
	"codeflux.dev/codeflux/internal/coordinator"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/executor"
	"codeflux.dev/codeflux/internal/fingerprint"
	"codeflux.dev/codeflux/internal/providers"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/testfixtures"
	"codeflux.dev/codeflux/internal/transport"
)

// MountedThreadOptions describes the one real requirement StartMountedThread
// drives end to end (REPO-041).
//
// It exists because the mounted browser matrix needs a durably-open thread
// carrying a real task graph and real timeline cards, and nothing on a
// freshly migrated database creates one. Everything here is driven through
// the same exported application surface TestAUDIT020_ARequirementReachesAwaitingReviewThroughTheRealApplication
// in internal/coordinator exercises — requirement, forecast, approval,
// preflight bind, start, real worktree, real agent loop, real tool executor —
// so what ends up in the database is a state the product can really produce,
// not rows shaped to satisfy an assertion.
type MountedThreadOptions struct {
	// Root is the directory the coordinator and its worktrees are built under.
	// Unlike CoordinatorHarness it is never deleted by this package: the
	// caller owns its lifetime, because the whole point of calling this is to
	// keep the database and the running Application alive for a browser to
	// point at.
	Root string
	// ListenAddress is the browser-facing loopback address. Empty means an
	// OS-assigned ephemeral port, which is what every isolated test uses; a
	// caller serving a real browser names a stable one instead.
	ListenAddress string
	// TaskListenAddress is the gRPC task-control loopback address. Empty means
	// an OS-assigned ephemeral port.
	TaskListenAddress string
	// WorkerExecutable is the compiled codeflux-worker binary the started run
	// spawns. It is required: unlike a production `codeflux start`, nothing
	// here can assume a worker binary sits beside the running executable,
	// because this package is imported by test and development-tool binaries,
	// never released as one itself.
	WorkerExecutable string
	// FrontendAssetsDirectory is a built browser asset set (index.html,
	// wasm_exec.js, bin/main.wasm), the same shape `codeflux-dev
	// build-frontend` produces. It is required for a caller that means to
	// point a real browser at the result: coordinator.StartApplication only
	// mounts the browser server, its security headers, and its same-origin
	// CSP when either this or ApplicationOptions.FrontendAssets is set, so
	// leaving it empty produces a coordinator that answers 404 to everything
	// and never renders app-root. A caller that only wants the database and
	// task-control surface (no browser) may leave it empty.
	FrontendAssetsDirectory string
	// Requirement is the plain-language ask a person would have typed.
	Requirement string
	// AffectedPackages seeds the forecast's declared scope, matching what a
	// real composer submission would have recorded.
	AffectedPackages []string
	// EditPath and EditContent are the file the scripted model writes.
	EditPath    string
	EditContent string
	// TestPath and TestContent are a companion test file the scripted model
	// writes before running the project's own tests. Both are required: a
	// plan step with no completion tool the model actually calls never
	// reaches implemented, and the run never leaves running.
	TestPath    string
	TestContent string
	// AwaitDeadline bounds how long StartMountedThread waits for the task to
	// leave running/validating. It defaults to 90s, the same bound
	// requirement_to_review_test.go uses for the same wait.
	AwaitDeadline time.Duration
}

// MountedThreadResult is what a driven requirement produced.
type MountedThreadResult struct {
	Application *coordinator.Application
	Repository  testfixtures.RepositoryFixture
	ThreadID    domain.ThreadID
	SessionID   domain.SessionID
	TaskID      domain.TaskID
	FinalState  domain.TaskState
}

// scriptedTurn is one scripted fixed-model decision, expressed the same way
// internal/coordinator's own end-to-end fixtures express one: as a function
// from the bounded observation to the turn it produces, so a turn can name
// the exact open plan step its tool call is attributed to.
type scriptedTurn func(agentloop.ModelInput) agentloop.ModelTurn

// scriptedMountedModel answers with a fixed sequence of turns and then claims
// implementation complete, mirroring internal/coordinator's own
// scriptedEngineModel. It is redeclared here rather than imported because the
// coordinator package's copy lives in a _test.go file and is not part of its
// exported surface.
type scriptedMountedModel struct {
	turns []scriptedTurn
	round int
}

func (model *scriptedMountedModel) Identity() providers.ModelIdentity {
	return providers.ModelIdentity{
		Provider: providers.ProviderIdentity{
			Adapter: "scripted", AdapterVersion: "1",
			Provider: "scripted", ProviderVersion: "1",
		},
		Model: "scripted", Revision: "scripted",
	}
}

func (model *scriptedMountedModel) ObserveThink(
	_ context.Context, input agentloop.ModelInput,
) (agentloop.ModelTurn, error) {
	if model.round >= len(model.turns) {
		return model.stamp(agentloop.ModelTurn{
			Completion: agentloop.CompletionImplementationComplete,
		}), nil
	}
	turn := model.turns[model.round](input)
	model.round++
	return model.stamp(turn), nil
}

func (model *scriptedMountedModel) stamp(turn agentloop.ModelTurn) agentloop.ModelTurn {
	requestID, _ := domain.NewModelRequestID()
	turn.ModelRequestID = requestID
	turn.Model = model.Identity()
	turn.Usage = providers.Usage{
		Known: true, Source: providers.UsageSourceProvider,
		InputTokens: 100, OutputTokens: 20,
	}
	amount, _ := providers.NewExactAmount("USD", 1, 1000)
	turn.Cost = amount
	return turn
}

// writeFileTurn is one scripted turn that edits a file.
func writeFileTurn(path, content string) scriptedTurn {
	return func(input agentloop.ModelInput) agentloop.ModelTurn {
		arguments, _ := json.Marshal(map[string]string{
			"path": path, "content": content,
		})
		callID, _ := domain.NewEventID()
		return agentloop.ModelTurn{ToolCalls: []agentloop.ModelToolCall{{
			Call: providers.ToolCall{
				ID: callID.String(), Name: string(executor.ToolApplyEdit),
				Arguments: arguments,
			},
			PlanStepID: stepAcceptingTool(input, executor.ToolApplyEdit),
		}}}
	}
}

// runProjectTestsTurn is one scripted turn that runs the project's own tests.
func runProjectTestsTurn() scriptedTurn {
	return func(input agentloop.ModelInput) agentloop.ModelTurn {
		arguments, _ := json.Marshal(map[string]string{
			"executable": "go", "arg1": "test", "arg2": "./...",
		})
		callID, _ := domain.NewEventID()
		return agentloop.ModelTurn{ToolCalls: []agentloop.ModelToolCall{{
			Call: providers.ToolCall{
				ID: callID.String(), Name: string(executor.ToolTest),
				Arguments: arguments,
			},
			PlanStepID: stepAcceptingTool(input, executor.ToolTest),
		}}}
	}
}

// stepAcceptingTool names an open plan step that can accept one tool, the
// same rule a real model has to satisfy: a call attributed to a step that
// cannot complete with that tool is refused by the loop.
func stepAcceptingTool(input agentloop.ModelInput, tool executor.ToolName) string {
	for _, step := range input.Plan.Steps {
		if step.State == agentloop.StepImplemented ||
			step.State == agentloop.StepValidated ||
			step.State == agentloop.StepSkipped {
			continue
		}
		for _, completion := range step.CompletionTools {
			if completion == tool {
				return step.ID
			}
		}
	}
	return ""
}

// StartMountedThread drives one real requirement through a real booted
// Application to awaiting-review or completed (REPO-041, following AUDIT-020
// and AUDIT-029).
//
// It never closes or deletes anything: the returned Application keeps
// running and Root keeps existing so a caller can point a browser at the
// database this produced. The caller owns Shutdown and cleanup.
func StartMountedThread(
	ctx context.Context,
	options MountedThreadOptions,
) (*MountedThreadResult, error) {
	if strings.TrimSpace(options.Root) == "" {
		return nil, errors.New("a mounted thread requires a root directory")
	}
	if strings.TrimSpace(options.WorkerExecutable) == "" {
		return nil, errors.New("a mounted thread requires a compiled worker executable")
	}
	if strings.TrimSpace(options.Requirement) == "" {
		return nil, errors.New("a mounted thread requires a requirement")
	}
	if strings.TrimSpace(options.EditPath) == "" || strings.TrimSpace(options.TestPath) == "" {
		return nil, errors.New("a mounted thread requires both an edit path and a companion test path")
	}
	deadline := options.AwaitDeadline
	if deadline <= 0 {
		deadline = 90 * time.Second
	}

	repositoryPath := filepath.Join(options.Root, "repository")
	fixture, err := testfixtures.NewRepositoryFixture(ctx, repositoryPath, testfixtures.CleanGoRepositoryFiles())
	if err != nil {
		return nil, fmt.Errorf("build mounted repository: %w", err)
	}

	model := &scriptedMountedModel{turns: []scriptedTurn{
		writeFileTurn(options.EditPath, options.EditContent),
		writeFileTurn(options.TestPath, options.TestContent),
		runProjectTestsTurn(),
	}}

	application, err := coordinator.StartApplication(ctx, coordinator.ApplicationOptions{
		DatabasePath:            filepath.Join(options.Root, "coordinator.sqlite3"),
		BackupDirectory:         filepath.Join(options.Root, "backups"),
		ListenAddress:           options.ListenAddress,
		TaskListenAddress:       options.TaskListenAddress,
		WorktreeRoot:            filepath.Join(options.Root, "worktrees"),
		WorkerExecutable:        options.WorkerExecutable,
		AgentModel:              model,
		FrontendAssetsDirectory: options.FrontendAssetsDirectory,
	})
	if err != nil {
		return nil, fmt.Errorf("start mounted application: %w", err)
	}

	result, err := driveMountedRequirement(ctx, application, fixture, options)
	if err != nil {
		_ = application.Shutdown(ctx)
		return nil, err
	}
	return result, nil
}

func driveMountedRequirement(
	ctx context.Context,
	application *coordinator.Application,
	fixture testfixtures.RepositoryFixture,
	options MountedThreadOptions,
) (*MountedThreadResult, error) {
	repositories := application.Repositories()
	lifecycle := application.TaskLifecycleApplication()
	if lifecycle == nil {
		return nil, errors.New("the started application exposes no task lifecycle")
	}

	// Opened the way `codeflux start --repository` opens it, not by inserting
	// rows by hand: a hand-built thread has no session, and a real run
	// resolves its scope through one.
	scope, err := repositories.EnsureLocalBootstrap(
		ctx, fixture.Root, fixture.Revision, "Seeded mounted thread")
	if err != nil {
		return nil, fmt.Errorf("open the mounted repository: %w", err)
	}

	messageID, err := domain.NewMessageID()
	if err != nil {
		return nil, err
	}
	if _, err := repositories.AppendMessage(ctx, storage.AppendMessage{
		ID: messageID, ThreadID: scope.ThreadID, Role: storage.MessageRoleUser,
		BodyRedacted:   options.Requirement,
		IdempotencyKey: "mounted-thread-request",
	}); err != nil {
		return nil, fmt.Errorf("record the request: %w", err)
	}

	created, err := lifecycle.CreateTaskFromRequirement(ctx, transport.CreateTaskCommand{
		ThreadID:                 scope.ThreadID,
		RequestMessageID:         &messageID,
		Requirement:              options.Requirement,
		TaskClass:                string(fingerprint.TaskClassFeature),
		RepositoryRevision:       fixture.Revision,
		BaselineModelRevision:    "scripted-mounted-fixture",
		ToolConfigurationVersion: "tools-v1",
		ValidationProfileVersion: "profile-v1",
		AffectedPackages:         options.AffectedPackages,
		IdempotencyKey:           "mounted-thread-requirement",
	})
	if err != nil {
		return nil, fmt.Errorf("intake the requirement: %w", err)
	}

	readyRevision, err := driveTaskToReady(ctx, repositories, created.TaskID, created.Revision)
	if err != nil {
		return nil, err
	}

	preflight, err := application.TaskPreflightService().BindExecution(
		ctx, created.TaskID, readyRevision,
		coordinator.ForecastedTask{
			Policy:   storage.ExecutionPolicyRevision{Revision: created.PolicyRevision},
			Forecast: storage.EffortForecastRevision{Revision: created.ForecastRevision},
		},
		"mounted-thread-bind",
	)
	if err != nil {
		return nil, fmt.Errorf("bind the approved preflight: %w", err)
	}

	if _, err := lifecycle.StartPreparedTask(ctx, transport.StartTaskCommand{
		TaskID:            created.TaskID,
		ExpectedRevision:  readyRevision,
		PreflightRevision: preflight.Revision,
		IdempotencyKey:    "mounted-thread-start",
	}); err != nil {
		return nil, fmt.Errorf("start the approved task: %w", err)
	}

	final, err := awaitReviewableState(ctx, repositories, created.TaskID, options.AwaitDeadline)
	if err != nil {
		return nil, err
	}

	return &MountedThreadResult{
		Application: application,
		Repository:  fixture,
		ThreadID:    scope.ThreadID,
		SessionID:   scope.SessionID,
		TaskID:      created.TaskID,
		FinalState:  final,
	}, nil
}

// driveTaskToReady performs the approval transitions a user's decision would,
// mirroring internal/coordinator's own test helper of the same name (which
// lives in a _test.go file and is not exported), and returns the task
// revision at Ready.
func driveTaskToReady(
	ctx context.Context,
	repositories *storage.Repositories,
	taskID domain.TaskID,
	fromRevision uint64,
) (uint64, error) {
	steps := []struct {
		from domain.TaskState
		to   domain.TaskState
		key  string
	}{
		{domain.TaskStateDraft, domain.TaskStateForecasting, "mounted-thread-forecasting"},
		{domain.TaskStateForecasting, domain.TaskStateAwaitingPlanApproval, "mounted-thread-awaiting"},
		{domain.TaskStateAwaitingPlanApproval, domain.TaskStateReady, "mounted-thread-ready"},
	}
	revision := fromRevision
	for _, step := range steps {
		eventID, err := domain.NewEventID()
		if err != nil {
			return 0, err
		}
		approval := domain.ApprovalRequestState("")
		if step.to == domain.TaskStateReady {
			approval = domain.ApprovalRequestStateGranted
		}
		transitioned, err := repositories.TransitionTask(ctx, storage.TransitionTask{
			EventID: eventID, TaskID: taskID, ExpectedRevision: revision,
			From: step.from, To: step.to, Approval: approval,
			IdempotencyKey: step.key,
		})
		if err != nil {
			return 0, fmt.Errorf("transition %s -> %s: %w", step.from, step.to, err)
		}
		revision = transitioned.Task.Revision
	}
	return revision, nil
}

// awaitReviewableState polls the task's durable state until it leaves
// running/validating, the same bounded wait
// TestAUDIT020_ARequirementReachesAwaitingReviewThroughTheRealApplication
// uses and for the same reason: completion is two durable steps, so a task
// legitimately passes through validating on its way to awaiting-review.
func awaitReviewableState(
	ctx context.Context,
	repositories *storage.Repositories,
	taskID domain.TaskID,
	deadline time.Duration,
) (domain.TaskState, error) {
	if deadline <= 0 {
		deadline = 90 * time.Second
	}
	final := domain.TaskState("")
	until := time.Now().Add(deadline)
	for time.Now().Before(until) {
		replay, err := repositories.ReplayTask(ctx, taskID)
		if err != nil {
			return "", fmt.Errorf("replay the driven task: %w", err)
		}
		final = replay.State
		switch final {
		case domain.TaskStateRunning, domain.TaskStateValidating:
			select {
			case <-ctx.Done():
				return final, ctx.Err()
			case <-time.After(250 * time.Millisecond):
			}
			continue
		}
		break
	}
	switch final {
	case domain.TaskStateAwaitingReview, domain.TaskStateCompleted:
		return final, nil
	default:
		return final, fmt.Errorf(
			"the driven task finished in %q, not a reviewable state", final)
	}
}
