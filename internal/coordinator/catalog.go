package coordinator

import (
	"fmt"
	"strings"
)

// ContractCatalogSchemaVersion versions the machine-reviewable M07 catalog.
const ContractCatalogSchemaVersion = 1

// OperationKind separates read, mutation, pure, stream, and external-effect
// contracts without leaking persistence implementation into protobuf.
type OperationKind string

const (
	OperationPure     OperationKind = "pure"
	OperationQuery    OperationKind = "query"
	OperationMutation OperationKind = "mutation"
	OperationStream   OperationKind = "stream"
	OperationEffect   OperationKind = "external-effect"
)

// FunctionContract records every behavior required by plan section 27B.
type FunctionContract struct {
	Category          string
	Name              string
	Kind              OperationKind
	Authority         string
	IdempotencyScope  string
	DuplicateBehavior string
	ExpectedRevision  string
	Transaction       string
	Repositories      []string
	DurableEvents     []string
	ExternalEffect    string
	EffectIntent      string
	EffectOutcome     string
	Ambiguity         string
	Retry             string
	Cancellation      string
	Errors            []string
}

// RPCContract binds one full RPC method to one application function and the
// only responsibilities permitted in its transport handler.
type RPCContract struct {
	FullMethod          string
	ApplicationFunction string
	CommandOrQueryType  string
	ResultType          string
	HandlerSteps        []string
	Function            FunctionContract
}

// Validate rejects an incomplete catalog row.
func (contract FunctionContract) Validate() error {
	required := map[string]string{
		"category":           contract.Category,
		"name":               contract.Name,
		"kind":               string(contract.Kind),
		"authority":          contract.Authority,
		"idempotency_scope":  contract.IdempotencyScope,
		"duplicate_behavior": contract.DuplicateBehavior,
		"expected_revision":  contract.ExpectedRevision,
		"transaction":        contract.Transaction,
		"external_effect":    contract.ExternalEffect,
		"effect_intent":      contract.EffectIntent,
		"effect_outcome":     contract.EffectOutcome,
		"ambiguity":          contract.Ambiguity,
		"retry":              contract.Retry,
		"cancellation":       contract.Cancellation,
	}
	for field, value := range required {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s.%s is empty", contract.Name, field)
		}
	}
	if len(contract.Errors) == 0 {
		return fmt.Errorf("%s.errors is empty", contract.Name)
	}
	if contract.Kind == OperationMutation &&
		strings.Contains(contract.IdempotencyScope, "not-required") {
		return fmt.Errorf("%s mutation lacks idempotency", contract.Name)
	}
	if contract.Kind == OperationEffect && contract.ExternalEffect == "none" {
		return fmt.Errorf("%s effect lacks external effect identity", contract.Name)
	}
	return nil
}

// Validate rejects an incomplete RPC mapping.
func (contract RPCContract) Validate() error {
	if !strings.HasPrefix(contract.FullMethod, "/codeflux.v1.") {
		return fmt.Errorf("%s is not a v1 full method", contract.FullMethod)
	}
	if strings.TrimSpace(contract.ApplicationFunction) == "" {
		return fmt.Errorf("%s lacks an application function", contract.FullMethod)
	}
	if strings.TrimSpace(contract.CommandOrQueryType) == "" {
		return fmt.Errorf("%s lacks a command or query type", contract.FullMethod)
	}
	if strings.TrimSpace(contract.ResultType) == "" {
		return fmt.Errorf("%s lacks a result type", contract.FullMethod)
	}
	if len(contract.HandlerSteps) != len(thinHandlerSteps) {
		return fmt.Errorf("%s has an invalid handler step count", contract.FullMethod)
	}
	for index := range thinHandlerSteps {
		if contract.HandlerSteps[index] != thinHandlerSteps[index] {
			return fmt.Errorf("%s handler step %d is %q", contract.FullMethod, index, contract.HandlerSteps[index])
		}
	}
	return contract.Function.Validate()
}

var stableErrors = []string{
	"not-found",
	"invalid-argument",
	"invalid-transition",
	"stale-revision",
	"duplicate",
	"denied",
	"budget-exhausted",
	"cancelled",
	"provider-retryable",
	"corruption",
	"recovery-required",
}

var thinHandlerSteps = []string{
	"validate-input",
	"convert-transport-domain",
	"delegate-once",
	"map-safe-error",
}

// RPCContracts returns a defensive copy of every prototype product RPC.
func RPCContracts() []RPCContract {
	return cloneRPCContracts(rpcContracts)
}

// BackendFunctionContracts returns a defensive copy of the complete §27B
// application and supporting function catalog.
func BackendFunctionContracts() []FunctionContract {
	return cloneFunctionContracts(backendFunctionContracts)
}

var rpcContracts = []RPCContract{
	rpcMutation("WorkspaceService/OpenWorkspace", "WorkspaceService.OpenRepository",
		"local-session", "workspace-open/session/key", "none",
		"workspace snapshot transaction", []string{"projects", "repositories", "workspaces", "sessions"},
		[]string{"workspace_opened"}, "filesystem-and-git-read"),
	rpcQuery("WorkspaceService/GetWorkspaceState", "WorkspaceService.GetWorkspaceState", "workspaces"),
	rpcQuery("WorkspaceService/ListRepositories", "WorkspaceService.ListRepositories", "repositories"),
	rpcQuery("WorkspaceService/InspectRepository", "WorkspaceService.InspectRepository", "repositories"),

	rpcMutation("ThreadService/CreateThread", "ThreadService.CreateThread",
		"local-session", "workspace/thread-create/key", "none",
		"thread transaction", []string{"threads", "sessions"}, []string{"thread_created"}, "none"),
	rpcQuery("ThreadService/ListThreads", "ThreadService.ListThreads", "threads"),
	rpcQuery("ThreadService/GetThreadPage", "ThreadService.GetThreadPage", "threads", "messages"),
	rpcMutation("ThreadService/SendMessage", "ThreadService.AppendUserMessage",
		"local-session", "thread/message/key", "required-current-thread",
		"message and optional draft-task transaction", []string{"threads", "messages", "tasks", "session-events"},
		[]string{"message_final", "task_state_changed"}, "none"),
	rpcMutation("ThreadService/RenameThread", "ThreadService.RenameThread",
		"local-session", "thread/rename/key", "required-current-thread",
		"thread transaction", []string{"threads", "session-events"}, []string{"thread_renamed"}, "none"),
	rpcMutation("ThreadService/ArchiveThread", "ThreadService.ArchiveThread",
		"local-session", "thread/archive/key", "required-current-thread",
		"thread transaction", []string{"threads", "session-events"}, []string{"thread_archived"}, "none"),

	rpcMutation("TaskService/CreateTask", "TaskService.CreateTask",
		"local-session", "thread/task-create/key", "required-current-thread",
		"task transaction", []string{"tasks", "session-events"}, []string{"task_state_changed"}, "none"),
	rpcQuery("TaskService/GetTask", "TaskService.GetTask", "tasks", "budgets"),
	rpcEffect("TaskService/StartTask", "TaskService.StartTask",
		"local-session", "task/start/key", "required-current-task",
		[]string{"tasks", "plans", "runs", "workers", "session-events"},
		[]string{"task_state_changed", "graph_patch"}, "worker-spawn"),
	rpcEffect("TaskService/PauseTask", "TaskService.PauseTask",
		"local-session", "task/pause/key", "required-current-task",
		[]string{"tasks", "runs", "checkpoints", "session-events"},
		[]string{"task_state_changed", "checkpoint_created"}, "worker-control"),
	rpcEffect("TaskService/ResumeTask", "TaskService.ResumeTask",
		"local-session", "task/resume/key", "required-current-task",
		[]string{"tasks", "runs", "workers", "session-events"},
		[]string{"task_state_changed"}, "worker-spawn"),
	rpcEffect("TaskService/CancelTask", "TaskService.CancelTask",
		"local-session", "task/cancel/key", "required-current-task",
		[]string{"tasks", "runs", "checkpoints", "session-events"},
		[]string{"task_state_changed", "checkpoint_created"}, "worker-control"),
	rpcEffect("TaskService/ApproveAction", "ToolService.ResolveApproval",
		"explicit-user-authority", "approval/resolve/key", "required-current-approval",
		[]string{"approvals", "permission-grants", "tool-executions", "session-events"},
		[]string{"approval_resolved", "tool_started"}, "mediated-tool"),
	rpcMutation("TaskService/SetBudget", "BudgetService.RaiseLimit",
		"explicit-user-authority", "task/budget/key", "required-current-budget",
		"budget transaction", []string{"budgets", "session-events"}, []string{"budget_updated"}, "none"),
	rpcMutation("TaskService/RequestRepair", "TaskService.RequestRepair",
		"local-session", "task/repair/key", "required-current-task",
		"plan and task transaction", []string{"tasks", "plans", "checkpoints", "session-events"},
		[]string{"plan_changed", "task_state_changed"}, "none"),
	rpcEffect("TaskService/RollbackTask", "TaskService.RollbackTask",
		"explicit-user-authority", "task/rollback/key", "required-current-task",
		[]string{"tasks", "checkpoints", "worktrees", "session-events"},
		[]string{"task_state_changed", "graph_patch"}, "git-and-filesystem"),
	rpcEffect("TaskService/PreserveRecoveryPatch", "TaskService.PreserveRecoveryPatch",
		"explicit-user-authority", "task/recovery-patch/key", "required-current-task",
		[]string{"checkpoint-recovery-assessments", "checkpoint-recovery-decisions", "checkpoint-recovery-attempts"},
		nil, "git-and-filesystem"),
	rpcEffect("TaskService/ReconcileRecovery", "TaskService.ReconcileRecovery",
		"explicit-user-authority", "task/recovery-reconcile/key", "required-current-task",
		[]string{"tasks", "runs", "checkpoints", "checkpoint-recovery-assessments", "checkpoint-recovery-decisions", "checkpoint-recovery-attempts"},
		[]string{"task_state_changed", "checkpoint_created"}, "git-and-filesystem"),
	rpcEffect("TaskService/SafeResumeRecovery", "TaskService.SafeResumeRecovery",
		"explicit-user-authority", "task/recovery-safe-resume/key", "required-current-task",
		[]string{"tasks", "runs", "workers", "checkpoint-recovery-assessments", "checkpoint-recovery-decisions", "checkpoint-recovery-attempts"},
		[]string{"task_state_changed"}, "worker-spawn"),

	rpcQuery("GraphService/GetGraphSlice", "GraphService.GetGraphSlice", "graphs", "graph-revisions"),
	rpcQuery("GraphService/ExpandGraph", "GraphService.ExpandGraph", "graphs", "graph-revisions"),
	rpcQuery("GraphService/GetNode", "GraphService.GetNode", "graphs", "graph-revisions"),
	rpcQuery("GraphService/SearchGraph", "GraphService.SearchGraph", "graphs", "graph-revisions"),
	rpcQuery("GraphService/ExplainNode", "GraphService.ExplainNode", "graphs", "evidence"),
	rpcQuery("GraphService/CompareGraphRevisions", "GraphService.CompareGraphRevisions", "graph-revisions"),

	rpcQuery("ReviewService/GetDiffSummary", "WorktreeService.GetTaskDiff", "tasks", "worktrees"),
	rpcQuery("ReviewService/GetValidationReport", "ValidationService.GetValidationReport", "validations", "evidence"),
	rpcEffect("ReviewService/AcceptChange", "ReviewService.AcceptChange",
		"explicit-user-authority", "task/accept/key", "required-task-diff-validation-evidence",
		[]string{"tasks", "acceptance-decisions", "episodes", "session-events"},
		[]string{"task_state_changed", "graph_patch"}, "git-acceptance"),
	rpcMutation("ReviewService/RejectChange", "ReviewService.RejectChange",
		"explicit-user-authority", "task/reject/key", "required-current-task-and-diff",
		"rejection transaction", []string{"tasks", "review-decisions", "session-events"},
		[]string{"task_state_changed"}, "none"),
	rpcEffect("ReviewService/OpenInEditor", "ReviewService.OpenInEditor",
		"explicit-user-action", "workspace/editor-open/key", "required-current-workspace",
		[]string{"workspaces", "effect-intents", "effect-outcomes"},
		[]string{"tool_started", "tool_completed"}, "editor-launch"),

	rpcQuery("SettingsService/GetModels", "ProviderService.GetModelCatalog", "provider-configurations"),
	rpcQuery("SettingsService/GetPolicy", "SettingsService.GetEffectiveSettings", "settings"),
	rpcMutation("SettingsService/SetPolicy", "SettingsService.UpdateUserSettings",
		"explicit-user-authority", "workspace/policy/key", "required-current-settings",
		"settings transaction", []string{"settings", "session-events"}, []string{"policy_updated"}, "none"),
	rpcMutation("SettingsService/SetBudgetDefaults", "SettingsService.SetBudgetDefaults",
		"explicit-user-authority", "workspace/budget-default/key", "required-current-settings",
		"settings transaction", []string{"settings", "session-events"}, []string{"budget_updated"}, "none"),
	rpcEffect("SettingsService/ConfigureProvider", "ProviderService.ConfigureProvider",
		"credential-admin", "workspace/provider/key", "required-current-provider-config",
		[]string{"provider-configurations", "credential-references", "effect-intents", "effect-outcomes"},
		[]string{"provider_configured"}, "credential-store"),
	rpcProbe("SettingsService/TestProvider", "ProviderService.TestProvider", "provider-network-probe"),
	rpcMutation("SettingsService/RecordFrontendTelemetry", "FrontendTelemetryService.RecordFrontendTelemetry",
		"authenticated-local-browser", "session/telemetry-record/key", "none",
		"frontend telemetry transaction", []string{"frontend-telemetry-events"}, nil, "none"),
	rpcQuery("SettingsService/ListFrontendTelemetry", "FrontendTelemetryService.ListFrontendTelemetry", "frontend-telemetry-events"),
	rpcMutation("SettingsService/DeleteFrontendTelemetry", "FrontendTelemetryService.DeleteFrontendTelemetry",
		"explicit-user-authority", "session/telemetry-delete/key", "none",
		"frontend telemetry transaction", []string{"frontend-telemetry-events"}, nil, "none"),

	rpcQuery("SessionService/GetSessionSnapshot", "SessionProjectionSnapshotService.GetSessionProjectionSnapshot", "sessions", "session-events", "tasks", "budgets", "approvals", "validations", "checkpoints"),
	rpcStream("SessionService/SubscribeSession", "EventHub.Subscribe", "session-events"),
}

var backendFunctionContracts = buildBackendFunctionContracts()

func buildBackendFunctionContracts() []FunctionContract {
	var contracts []FunctionContract
	add := func(values ...FunctionContract) { contracts = append(contracts, values...) }

	add(
		effectFunction("lifecycle", "NewApplication", "system-startup", "none", nil, nil, "dependency-validation"),
		effectFunction("lifecycle", "Application.Start", "system-startup", "none",
			[]string{"all-repositories"}, []string{"application_started"}, "database-lock-server"),
		queryFunction("lifecycle", "Application.Health", "all-health-projections"),
		effectFunction("lifecycle", "Application.Shutdown", "system-shutdown", "none",
			[]string{"workers", "session-events"}, []string{"application_stopped"}, "worker-server-database"),
		pureFunction("lifecycle", "ResolveApplicationPaths"),
		effectFunction("lifecycle", "AcquireInstanceLock", "system-startup", "none", nil, nil, "filesystem-lock"),
		effectFunction("lifecycle", "OpenDatabase", "system-startup", "none", nil, nil, "sqlite-open"),
		effectFunction("lifecycle", "ApplyMigrations", "system-startup", "none",
			[]string{"schema-migrations"}, nil, "sqlite-migration"),
		queryFunction("lifecycle", "DiscoverRecoveryCandidates", "tasks", "runs", "workers", "checkpoints"),
		effectFunction("lifecycle", "StartLoopbackServer", "system-startup", "none", nil, nil, "loopback-bind"),
		queryFunction("lifecycle", "BuildHealthReport", "all-health-projections"),
	)

	add(
		mutationFunction("events", "EventJournal.Append", "internal-application", "transaction/event/key",
			"required-current-session-sequence", "caller-owned transaction",
			[]string{"sessions", "session-events"}, []string{"typed-session-event"}),
		queryFunction("events", "EventJournal.Replay", "sessions", "session-events"),
		queryFunction("events", "EventJournal.CurrentSequence", "sessions"),
		pureFunction("events", "EventHub.PublishCommitted"),
		streamFunction("events", "EventHub.Subscribe", "sessions", "session-events"),
		queryFunction("events", "BuildSessionSnapshot", "sessions", "session-events", "session-snapshots"),
		pureFunction("events", "ReduceTaskEvents"),
		mutationFunction("events", "CompactEphemeralEvents", "system-maintenance", "compaction/run/key",
			"required-current-session-sequence", "compaction transaction",
			[]string{"session-events", "session-snapshots"}, []string{"snapshot_created"}),
	)

	add(
		effectFunction("workspace", "WorkspaceService.OpenRepository", "local-session", "workspace-open/session/key",
			[]string{"projects", "repositories", "workspaces", "session-events"},
			[]string{"workspace_opened"}, "filesystem-and-git-read"),
		queryFunction("workspace", "WorkspaceService.GetWorkspaceState", "workspaces"),
		queryFunction("workspace", "WorkspaceService.ListRepositories", "repositories"),
		queryFunction("workspace", "WorkspaceService.InspectRepository", "repositories"),
		effectFunction("workspace", "WorkspaceService.RefreshRepositoryMap", "local-session", "repository-map/key",
			[]string{"repository-maps", "session-events"}, []string{"repository_map_updated"}, "go-tool-read"),
		queryFunction("workspace", "WorkspaceService.SelectContext", "repository-maps", "context-manifests"),
		queryFunction("workspace", "WorkspaceService.ExplainContextSelection", "context-manifests"),
		pureFunction("workspace", "ResolveRepositoryRoot"),
		effectFunction("workspace", "InspectGitState", "internal-application", "none", nil, nil, "git-read"),
		effectFunction("workspace", "DiscoverGoWorkspace", "internal-application", "none", nil, nil, "filesystem-read"),
		effectFunction("workspace", "BuildGoRepositoryMap", "internal-application", "repository-map/key",
			[]string{"repository-maps"}, []string{"repository_map_updated"}, "go-tool-read"),
		pureFunction("workspace", "RankContextCandidates"),
		pureFunction("workspace", "EnforceContextBudget"),
		pureFunction("workspace", "ScrubContext"),
	)

	add(
		mutationFunction("thread", "ThreadService.CreateThread", "local-session", "workspace/thread-create/key",
			"none", "thread transaction", []string{"threads", "sessions"}, []string{"thread_created"}),
		queryFunction("thread", "ThreadService.ListThreads", "threads"),
		queryFunction("thread", "ThreadService.GetThreadPage", "threads", "messages"),
		mutationFunction("thread", "ThreadService.AppendUserMessage", "local-session", "thread/message/key",
			"required-current-thread", "message transaction",
			[]string{"threads", "messages", "tasks", "session-events"},
			[]string{"message_final", "task_state_changed"}),
		mutationFunction("thread", "ThreadService.RenameThread", "local-session", "thread/rename/key",
			"required-current-thread", "thread transaction",
			[]string{"threads", "session-events"}, []string{"thread_renamed"}),
		mutationFunction("thread", "ThreadService.ArchiveThread", "local-session", "thread/archive/key",
			"required-current-thread", "thread transaction",
			[]string{"threads", "session-events"}, []string{"thread_archived"}),
	)

	add(taskContracts()...)
	add(worktreeContracts()...)
	add(providerContracts()...)
	add(budgetContracts()...)
	add(toolContracts()...)
	add(workerContracts()...)
	add(reviewContracts()...)
	add(graphContracts()...)
	add(memoryContracts()...)
	add(settingsDiagnosticContracts()...)
	return contracts
}

func taskContracts() []FunctionContract {
	return []FunctionContract{
		mutationFunction("task", "TaskService.CreateTask", "local-session", "thread/task-create/key", "required-current-thread", "task transaction", []string{"tasks", "session-events"}, []string{"task_state_changed"}),
		effectFunction("task", "TaskService.ForecastTask", "internal-application", "task/forecast/key", []string{"tasks", "forecasts", "budgets", "session-events"}, []string{"forecast_updated", "budget_updated"}, "provider-model"),
		effectFunction("task", "TaskService.BuildPlan", "internal-application", "task/plan/key", []string{"tasks", "plans", "session-events"}, []string{"plan_created", "task_state_changed"}, "provider-model"),
		effectFunction("task", "TaskService.RevisePlan", "local-session", "task/plan-revision/key", []string{"tasks", "plans", "session-events"}, []string{"plan_changed"}, "provider-model"),
		mutationFunction("task", "TaskService.ApprovePlan", "explicit-user-authority", "task/plan-approval/key", "required-current-plan", "approval transaction", []string{"tasks", "plans", "approvals", "session-events"}, []string{"approval_resolved", "task_state_changed"}),
		effectFunction("task", "TaskService.StartTask", "local-session", "task/start/key", []string{"tasks", "runs", "workers", "session-events"}, []string{"task_state_changed"}, "worker-spawn"),
		effectFunction("task", "TaskService.PauseTask", "local-session", "task/pause/key", []string{"tasks", "runs", "checkpoints", "session-events"}, []string{"task_state_changed", "checkpoint_created"}, "worker-control"),
		effectFunction("task", "TaskService.ResumeTask", "local-session", "task/resume/key", []string{"tasks", "runs", "workers", "session-events"}, []string{"task_state_changed"}, "worker-spawn"),
		effectFunction("task", "TaskService.CancelTask", "local-session", "task/cancel/key", []string{"tasks", "runs", "checkpoints", "session-events"}, []string{"task_state_changed", "checkpoint_created"}, "worker-control"),
		mutationFunction("task", "TaskService.RequestRepair", "local-session", "task/repair/key", "required-current-task", "plan transaction", []string{"tasks", "plans", "checkpoints", "session-events"}, []string{"plan_changed", "task_state_changed"}),
		effectFunction("task", "TaskService.RollbackTask", "explicit-user-authority", "task/rollback/key", []string{"tasks", "checkpoints", "worktrees", "session-events"}, []string{"task_state_changed", "graph_patch"}, "git-and-filesystem"),
		effectFunction("task", "TaskService.PreserveRecoveryPatch", "explicit-user-authority", "task/recovery-patch/key", []string{"checkpoint-recovery-assessments", "checkpoint-recovery-decisions", "checkpoint-recovery-attempts"}, nil, "git-and-filesystem"),
		effectFunction("task", "TaskService.ReconcileRecovery", "explicit-user-authority", "task/recovery-reconcile/key", []string{"tasks", "runs", "checkpoints", "checkpoint-recovery-assessments", "checkpoint-recovery-decisions", "checkpoint-recovery-attempts"}, []string{"task_state_changed", "checkpoint_created"}, "git-and-filesystem"),
		effectFunction("task", "TaskService.SafeResumeRecovery", "explicit-user-authority", "task/recovery-safe-resume/key", []string{"tasks", "runs", "workers", "checkpoint-recovery-assessments", "checkpoint-recovery-decisions", "checkpoint-recovery-attempts"}, []string{"task_state_changed"}, "worker-spawn"),
		pureFunction("task", "InterpretRequirement"),
		pureFunction("task", "ClassifyTaskRisk"),
		pureFunction("task", "BuildTaskFingerprint"),
		queryFunction("task", "RetrievePreWorkEvidence", "facts", "atoms", "episodes", "embeddings"),
		pureFunction("task", "EstimateTask"),
		effectFunction("task", "ConstructPlan", "internal-application", "task/plan-model/key", []string{"model-requests", "budgets"}, []string{"plan_created"}, "provider-model"),
		pureFunction("task", "ValidatePlan"),
		mutationFunction("task", "StartRun", "internal-application", "task/run/key", "required-current-task", "run transaction", []string{"runs", "workers", "session-events"}, []string{"task_state_changed"}),
		effectFunction("task", "ExecuteNextStep", "worker-lease", "run/step/key", []string{"runs", "plans", "checkpoints", "session-events"}, []string{"tool_started", "tool_completed", "graph_patch"}, "model-tool-process"),
		queryFunction("task", "ObserveProgress", "runs", "plans", "session-events"),
		pureFunction("task", "DecideNextAction"),
		mutationFunction("task", "CompleteRun", "worker-lease", "run/complete/key", "required-current-run", "run transaction", []string{"runs", "tasks", "session-events"}, []string{"task_state_changed"}),
	}
}

func worktreeContracts() []FunctionContract {
	return []FunctionContract{
		effectFunction("worktree", "WorktreeService.CreateTaskWorktree", "internal-application", "task/worktree/key", []string{"worktrees", "effect-intents", "effect-outcomes"}, []string{"checkpoint_created"}, "git-worktree"),
		effectFunction("worktree", "WorktreeService.VerifyTaskWorktree", "internal-application", "none", []string{"worktrees"}, nil, "git-and-filesystem-read"),
		effectFunction("worktree", "WorktreeService.ApplyEditBatch", "worker-lease", "task/edit-batch/key", []string{"worktrees", "edit-batches", "effect-intents", "effect-outcomes"}, []string{"graph_patch"}, "filesystem-write"),
		effectFunction("worktree", "WorktreeService.GetTaskDiff", "local-session", "none", []string{"worktrees"}, nil, "git-read"),
		effectFunction("worktree", "WorktreeService.CreateCheckpoint", "worker-lease", "task/checkpoint/key", []string{"checkpoints", "worktrees", "session-events"}, []string{"checkpoint_created"}, "git-read"),
		effectFunction("worktree", "WorktreeService.RestoreCheckpoint", "explicit-user-authority", "task/restore/key", []string{"checkpoints", "worktrees", "session-events"}, []string{"task_state_changed", "graph_patch"}, "git-and-filesystem"),
		effectFunction("worktree", "WorktreeService.AcceptTaskChange", "explicit-user-authority", "task/accept/key", []string{"acceptance-decisions", "worktrees", "session-events"}, []string{"task_state_changed"}, "git-acceptance"),
		effectFunction("worktree", "WorktreeService.AbandonTaskWorktree", "explicit-user-authority", "task/abandon/key", []string{"worktrees", "session-events"}, []string{"task_state_changed"}, "git-worktree"),
		effectFunction("worktree", "WorktreeService.CleanupTaskWorktree", "explicit-user-authority", "task/cleanup/key", []string{"worktrees", "effect-intents", "effect-outcomes"}, nil, "git-worktree"),
		pureFunction("worktree", "ResolveTaskPath"),
		effectFunction("worktree", "ReadFileAtRevision", "worker-lease", "none", nil, nil, "filesystem-read"),
		effectFunction("worktree", "ApplyFileMutation", "worker-lease", "file-mutation/key", []string{"edit-batches", "effect-intents", "effect-outcomes"}, []string{"graph_patch"}, "filesystem-write"),
		effectFunction("worktree", "ComputeDiffIdentity", "internal-application", "none", nil, nil, "git-read"),
		pureFunction("worktree", "DetectConcurrentUserChanges"),
	}
}

func providerContracts() []FunctionContract {
	return []FunctionContract{
		queryFunction("provider", "ModelProvider.ProviderIdentity"),
		effectFunction("provider", "ModelProvider.ListModels", "internal-application", "none", nil, nil, "provider-network"),
		effectFunction("provider", "ModelProvider.Stream", "worker-lease", "model-request/id", []string{"model-requests", "budget-reservations", "effect-intents", "effect-outcomes"}, []string{"message_delta", "message_final", "usage_updated", "cost_updated"}, "provider-network"),
		effectFunction("provider", "ModelProvider.Cancel", "worker-lease", "model-request/id", []string{"model-requests", "effect-outcomes"}, []string{"error"}, "provider-network"),
		effectFunction("provider", "ProviderService.ConfigureProvider", "credential-admin", "workspace/provider/key", []string{"provider-configurations", "credential-references"}, []string{"provider_configured"}, "credential-store"),
		effectFunction("provider", "ProviderService.TestProvider", "local-session", "none", nil, nil, "provider-network-probe"),
		queryFunction("provider", "ProviderService.GetModelCatalog", "provider-configurations"),
		effectFunction("provider", "ProviderService.ExecuteModelRequest", "worker-lease", "model-request/id", []string{"model-requests", "budgets", "session-events"}, []string{"message_delta", "message_final", "usage_updated", "cost_updated"}, "provider-network"),
		pureFunction("provider", "BuildModelRequest"),
		mutationFunction("provider", "ReserveRequestBudget", "worker-lease", "request/reservation/key", "required-current-budget", "budget transaction", []string{"budgets", "budget-reservations", "session-events"}, []string{"budget_updated"}),
		mutationFunction("provider", "PersistRequestIntent", "worker-lease", "model-request/id", "required-current-request", "request-intent transaction", []string{"model-requests", "effect-intents"}, []string{"tool_started"}),
		effectFunction("provider", "ConsumeModelStream", "worker-lease", "model-request/id", []string{"model-requests", "session-events"}, []string{"message_delta", "message_final"}, "provider-network"),
		mutationFunction("provider", "ReconcileUsage", "internal-application", "model-request/reconcile/key", "required-current-budget", "usage transaction", []string{"model-requests", "budgets", "usage", "session-events"}, []string{"usage_updated", "cost_updated", "budget_updated"}),
		pureFunction("provider", "ResolveModelPrice"),
		pureFunction("provider", "ClassifyProviderFailure"),
	}
}

func budgetContracts() []FunctionContract {
	return []FunctionContract{
		mutationFunction("budget", "BudgetService.CreateBudget", "internal-application", "task/budget-create/key", "none", "budget transaction", []string{"budgets", "session-events"}, []string{"budget_updated"}),
		mutationFunction("budget", "BudgetService.Reserve", "worker-lease", "budget/reserve/key", "required-current-budget", "budget transaction", []string{"budgets", "budget-reservations", "session-events"}, []string{"budget_updated"}),
		mutationFunction("budget", "BudgetService.CommitUsage", "internal-application", "budget/usage/key", "required-current-budget", "budget transaction", []string{"budgets", "usage", "session-events"}, []string{"usage_updated", "cost_updated", "budget_updated"}),
		mutationFunction("budget", "BudgetService.ReleaseReservation", "internal-application", "budget/release/key", "required-current-budget", "budget transaction", []string{"budgets", "budget-reservations", "session-events"}, []string{"budget_updated"}),
		mutationFunction("budget", "BudgetService.RaiseLimit", "explicit-user-authority", "budget/raise/key", "required-current-budget", "budget transaction", []string{"budgets", "session-events"}, []string{"budget_updated"}),
		queryFunction("budget", "BudgetService.GetBudget", "budgets", "budget-reservations", "usage"),
	}
}

func toolContracts() []FunctionContract {
	return []FunctionContract{
		queryFunction("tool", "ToolService.ListTools", "tool-registry"),
		effectFunction("tool", "ToolService.RequestToolExecution", "worker-lease", "tool-execution/id", []string{"tool-executions", "approvals", "effect-intents", "effect-outcomes", "session-events"}, []string{"tool_started", "approval_requested", "tool_completed"}, "mediated-tool"),
		effectFunction("tool", "ToolService.ResolveApproval", "explicit-user-authority", "approval/resolve/key", []string{"approvals", "permission-grants", "tool-executions", "session-events"}, []string{"approval_resolved", "tool_started"}, "mediated-tool"),
		effectFunction("tool", "ToolService.CancelToolExecution", "local-session", "tool-execution/id", []string{"tool-executions", "effect-outcomes", "session-events"}, []string{"tool_completed"}, "process-control"),
		pureFunction("tool", "ClassifyAuthority"),
		pureFunction("tool", "MatchExistingGrant"),
		pureFunction("tool", "BuildApprovalRequest"),
		effectFunction("tool", "ExecuteAuthorizedTool", "scoped-grant", "tool-execution/id", []string{"tool-executions", "effect-intents", "effect-outcomes"}, []string{"tool_started", "tool_completed"}, "mediated-tool"),
		pureFunction("tool", "BoundToolOutput"),
		pureFunction("tool", "RedactToolResult"),
	}
}

func workerContracts() []FunctionContract {
	return []FunctionContract{
		effectFunction("worker", "WorkerManager.Spawn", "internal-application", "run/worker-spawn/key", []string{"workers", "runs", "effect-intents", "effect-outcomes"}, []string{"task_state_changed"}, "process-spawn"),
		effectFunction("worker", "WorkerManager.Pause", "local-session", "worker/pause/key", []string{"workers", "runs"}, []string{"task_state_changed"}, "process-control"),
		effectFunction("worker", "WorkerManager.Resume", "local-session", "worker/resume/key", []string{"workers", "runs"}, []string{"task_state_changed"}, "process-control"),
		effectFunction("worker", "WorkerManager.Cancel", "local-session", "worker/cancel/key", []string{"workers", "runs"}, []string{"task_state_changed"}, "process-control"),
		effectFunction("worker", "WorkerManager.Checkpoint", "internal-application", "worker/checkpoint/key", []string{"workers", "checkpoints"}, []string{"checkpoint_created"}, "process-control"),
		effectFunction("worker", "WorkerManager.Status", "internal-application", "none", nil, nil, "process-observation"),
		mutationFunction("worker", "AcquireRunOwnership", "worker-process", "run/lease/key", "required-current-run", "lease transaction", []string{"runs", "worker-leases"}, []string{"task_state_changed"}),
		mutationFunction("worker", "RenewRunOwnership", "worker-process", "run/lease-renew/key", "required-current-lease", "lease transaction", []string{"worker-leases"}, nil),
		mutationFunction("worker", "ReleaseRunOwnership", "worker-process", "run/lease-release/key", "required-current-lease", "lease transaction", []string{"worker-leases"}, nil),
		mutationFunction("worker", "HandleWorkerHeartbeat", "worker-process", "worker/heartbeat/id", "required-current-lease", "heartbeat transaction", []string{"workers", "worker-leases"}, nil),
		queryFunction("worker", "ClassifyLostWorker", "runs", "workers", "worker-leases", "checkpoints", "effect-intents"),
	}
}

func reviewContracts() []FunctionContract {
	return []FunctionContract{
		queryFunction("review", "ValidationService.SelectValidationPlan", "tasks", "policies"),
		effectFunction("review", "ValidationService.RunValidation", "worker-lease", "validation/id", []string{"validations", "effect-intents", "effect-outcomes", "session-events"}, []string{"validation_updated"}, "process-execution"),
		queryFunction("review", "ValidationService.GetValidationReport", "validations", "evidence"),
		mutationFunction("review", "ValidationService.InvalidateStaleValidation", "internal-application", "diff/invalidation/key", "required-current-diff", "validation transaction", []string{"validations", "evidence", "session-events"}, []string{"validation_updated"}),
		queryFunction("review", "ReviewService.BuildReviewBundle", "tasks", "plans", "validations", "evidence", "worktrees"),
		effectFunction("review", "ReviewService.AcceptChange", "explicit-user-authority", "task/accept/key", []string{"tasks", "acceptance-decisions", "episodes", "session-events"}, []string{"task_state_changed"}, "git-acceptance"),
		mutationFunction("review", "ReviewService.RequestRepair", "local-session", "task/review-repair/key", "required-task-diff-review", "repair transaction", []string{"tasks", "plans", "checkpoints", "session-events"}, []string{"plan_changed", "task_state_changed"}),
		mutationFunction("review", "ReviewService.RejectChange", "explicit-user-authority", "task/reject/key", "required-task-diff-review", "rejection transaction", []string{"tasks", "review-decisions", "session-events"}, []string{"task_state_changed"}),
		effectFunction("review", "ReviewService.OpenInEditor", "explicit-user-action", "workspace/editor-open/key", []string{"effect-intents", "effect-outcomes"}, []string{"tool_started", "tool_completed"}, "editor-launch"),
		pureFunction("review", "SelectTests"),
		pureFunction("review", "CompareWithBaseline"),
		pureFunction("review", "BuildEvidenceReport"),
		pureFunction("review", "AssignClaimGuarantees"),
		pureFunction("review", "VerifyAcceptanceRevision"),
	}
}

func graphContracts() []FunctionContract {
	return []FunctionContract{
		queryFunction("graph", "GraphService.GetGraphSlice", "graphs", "graph-revisions"),
		queryFunction("graph", "GraphService.ExpandGraph", "graphs", "graph-revisions"),
		queryFunction("graph", "GraphService.GetNode", "graphs", "graph-revisions"),
		queryFunction("graph", "GraphService.SearchGraph", "graphs", "graph-revisions"),
		queryFunction("graph", "GraphService.ExplainNode", "graphs", "evidence"),
		queryFunction("graph", "GraphService.CompareGraphRevisions", "graph-revisions"),
		pureFunction("graph", "ProjectSessionEvent"),
		mutationFunction("graph", "CommitGraphRevision", "internal-application", "graph/revision/key", "required-current-graph", "graph transaction", []string{"graphs", "graph-revisions", "session-events"}, []string{"graph_snapshot", "graph_patch"}),
		pureFunction("graph", "BuildGraphPatch"),
		pureFunction("graph", "ComputeStableLayout"),
		pureFunction("graph", "IsolateDependencyCone"),
		pureFunction("graph", "IsolateEvidenceCone"),
	}
}

func memoryContracts() []FunctionContract {
	return []FunctionContract{
		mutationFunction("memory", "MemoryService.FinalizeEpisode", "internal-application", "task/episode/key", "required-accepted-task", "episode transaction", []string{"episodes", "facts", "session-events"}, []string{"task_state_changed"}),
		mutationFunction("memory", "MemoryService.ExtractDeterministicFacts", "internal-application", "episode/facts/key", "required-current-episode", "fact transaction", []string{"episodes", "facts"}, nil),
		queryFunction("memory", "MemoryService.RetrieveBeforeWork", "episodes", "facts", "atoms", "embeddings"),
		mutationFunction("memory", "MemoryService.RecordInfluence", "internal-application", "retrieval/influence/key", "required-current-artifact", "influence transaction", []string{"retrieval-influences"}, nil),
		mutationFunction("memory", "MemoryService.InvalidateArtifact", "explicit-user-or-defect-authority", "artifact/invalidate/key", "required-current-artifact", "invalidation transaction", []string{"facts", "atoms", "embeddings", "invalidations"}, []string{"graph_patch"}),
		pureFunction("memory", "ValidateAtomName"),
		pureFunction("memory", "ParseAtomDocumentation"),
		pureFunction("memory", "ValidateAtomDocumentation"),
		mutationFunction("memory", "AdmitAtomRevision", "internal-application", "atom/revision/key", "required-current-atom", "atom transaction", []string{"atoms", "atom-revisions"}, nil),
		pureFunction("memory", "BuildEmbeddingInput"),
		effectFunction("memory", "EmbedArtifact", "internal-application", "artifact/embedding/key", []string{"embeddings", "effect-intents", "effect-outcomes"}, nil, "embedding-provider"),
		queryFunction("memory", "RetrieveExact", "facts", "atoms", "episodes"),
		queryFunction("memory", "RetrieveVectorCandidates", "embeddings", "facts", "atoms"),
		pureFunction("memory", "CheckApplicability"),
		pureFunction("memory", "CheckAssurance"),
	}
}

func settingsDiagnosticContracts() []FunctionContract {
	return []FunctionContract{
		effectFunction("credentials", "CredentialStore.Put", "credential-admin", "credential/ref/key", nil, nil, "credential-store"),
		effectFunction("credentials", "CredentialStore.Get", "provider-adapter", "none", nil, nil, "credential-store"),
		effectFunction("credentials", "CredentialStore.Delete", "credential-admin", "credential/ref/delete/key", nil, nil, "credential-store"),
		effectFunction("credentials", "CredentialStore.TestAvailability", "local-session", "none", nil, nil, "credential-store"),
		queryFunction("settings", "SettingsService.GetEffectiveSettings", "settings"),
		mutationFunction("settings", "SettingsService.UpdateUserSettings", "explicit-user-authority", "settings/user/key", "required-current-settings", "settings transaction", []string{"settings", "session-events"}, []string{"policy_updated"}),
		mutationFunction("settings", "SettingsService.ApproveRepositorySettings", "explicit-user-authority", "settings/repository/key", "required-current-settings", "settings transaction", []string{"settings", "approvals", "session-events"}, []string{"approval_resolved", "policy_updated"}),
		mutationFunction("settings", "SettingsService.SetBudgetDefaults", "explicit-user-authority", "settings/budget-default/key", "required-current-settings", "settings transaction", []string{"settings", "session-events"}, []string{"budget_updated"}),
		queryFunction("diagnostics", "DiagnosticsService.RunDoctor", "all-health-projections"),
		effectFunction("diagnostics", "DiagnosticsService.BackupDatabase", "explicit-user-action", "database/backup/key", []string{"backup-manifests"}, nil, "filesystem-write"),
		effectFunction("diagnostics", "DiagnosticsService.CheckIntegrity", "local-session", "none", nil, nil, "sqlite-integrity-read"),
		effectFunction("diagnostics", "DiagnosticsService.BuildDiagnosticExport", "explicit-user-action", "diagnostic/export/key", []string{"diagnostic-manifests"}, nil, "filesystem-write"),
	}
}

func rpcQuery(method, function string, repositories ...string) RPCContract {
	return newRPC(method, function, queryFunction("rpc", function, repositories...))
}

func rpcStream(method, function string, repositories ...string) RPCContract {
	return newRPC(method, function, streamFunction("rpc", function, repositories...))
}

func rpcProbe(method, function, effect string) RPCContract {
	return newRPC(method, function, FunctionContract{
		Category:          "rpc",
		Name:              function,
		Kind:              OperationEffect,
		Authority:         "local-session",
		IdempotencyScope:  "not-required-read-only-probe",
		DuplicateBehavior: "perform a new bounded probe",
		ExpectedRevision:  "none",
		Transaction:       "none",
		Repositories:      []string{"none"},
		DurableEvents:     []string{"none"},
		ExternalEffect:    effect,
		EffectIntent:      "not-required-read-only-probe",
		EffectOutcome:     "bounded probe result is returned without durable state",
		Ambiguity:         "a cancelled probe has no durable success",
		Retry:             "caller may start a new bounded probe",
		Cancellation:      "cooperative; no durable success to rewrite",
		Errors:            cloneStrings(stableErrors),
	})
}

func rpcMutation(
	method, function, authority, idempotency, revision, transaction string,
	repositories, events []string,
	effect string,
) RPCContract {
	contract := mutationFunction("rpc", function, authority, idempotency, revision, transaction, repositories, events)
	contract.ExternalEffect = effect
	if effect != "none" {
		contract.Kind = OperationEffect
		contract.EffectIntent = "persist identity and authorized parameters before execution"
		contract.EffectOutcome = "persist attributable success, failure, cancellation, or unknown outcome"
		contract.Ambiguity = "reconcile by effect identity; never re-execute until attribution is known"
		contract.Retry = "reuse intent and outcome; retry only a classified safe failure"
	}
	return newRPC(method, function, contract)
}

func rpcEffect(
	method, function, authority, idempotency, revision string,
	repositories, events []string,
	effect string,
) RPCContract {
	contract := effectFunction("rpc", function, authority, idempotency, repositories, events, effect)
	contract.ExpectedRevision = revision
	return newRPC(method, function, contract)
}

func newRPC(method, function string, contract FunctionContract) RPCContract {
	_, methodName, _ := strings.Cut(method, "/")
	return RPCContract{
		FullMethod:          "/codeflux.v1." + method,
		ApplicationFunction: function,
		CommandOrQueryType:  "codeflux.v1." + methodName + "Request",
		ResultType:          "codeflux.v1." + methodName + "Response",
		HandlerSteps:        cloneStrings(thinHandlerSteps),
		Function:            contract,
	}
}

func pureFunction(category, name string) FunctionContract {
	return FunctionContract{
		Category:          category,
		Name:              name,
		Kind:              OperationPure,
		Authority:         "internal-caller",
		IdempotencyScope:  "not-required-pure",
		DuplicateBehavior: "same inputs produce the same decision",
		ExpectedRevision:  "immutable-input",
		Transaction:       "none",
		Repositories:      []string{"none"},
		DurableEvents:     []string{"none"},
		ExternalEffect:    "none",
		EffectIntent:      "none",
		EffectOutcome:     "none",
		Ambiguity:         "none",
		Retry:             "safe pure reevaluation",
		Cancellation:      "check before expensive bounded work",
		Errors:            cloneStrings(stableErrors),
	}
}

func queryFunction(category, name string, repositories ...string) FunctionContract {
	return FunctionContract{
		Category:          category,
		Name:              name,
		Kind:              OperationQuery,
		Authority:         "authenticated-scoped-reader",
		IdempotencyScope:  "not-required-read-only",
		DuplicateBehavior: "repeat the same revision-bound read",
		ExpectedRevision:  "optional-read-binding",
		Transaction:       "bounded read transaction",
		Repositories:      explicitList(repositories),
		DurableEvents:     []string{"none"},
		ExternalEffect:    "none",
		EffectIntent:      "none",
		EffectOutcome:     "none",
		Ambiguity:         "none",
		Retry:             "safe revision-bound reread",
		Cancellation:      "cooperative before response",
		Errors:            cloneStrings(stableErrors),
	}
}

func streamFunction(category, name string, repositories ...string) FunctionContract {
	contract := queryFunction(category, name, repositories...)
	contract.Kind = OperationStream
	contract.DuplicateBehavior = "resume strictly after the supplied sequence"
	contract.ExpectedRevision = "after-sequence"
	contract.Cancellation = "unsubscribe immediately; committed events remain durable"
	return contract
}

func mutationFunction(
	category, name, authority, idempotency, revision, transaction string,
	repositories, events []string,
) FunctionContract {
	return FunctionContract{
		Category:          category,
		Name:              name,
		Kind:              OperationMutation,
		Authority:         authority,
		IdempotencyScope:  idempotency,
		DuplicateBehavior: "return the original committed result without repeating effects",
		ExpectedRevision:  revision,
		Transaction:       transaction,
		Repositories:      explicitList(repositories),
		DurableEvents:     explicitList(events),
		ExternalEffect:    "none",
		EffectIntent:      "none",
		EffectOutcome:     "committed result stored with idempotency record",
		Ambiguity:         "none after transaction resolution",
		Retry:             "same key returns original result; stale revision conflicts",
		Cancellation:      "honor before commit; committed success remains success",
		Errors:            cloneStrings(stableErrors),
	}
}

func effectFunction(
	category, name, authority, idempotency string,
	repositories, events []string,
	effect string,
) FunctionContract {
	return FunctionContract{
		Category:          category,
		Name:              name,
		Kind:              OperationEffect,
		Authority:         authority,
		IdempotencyScope:  idempotency,
		DuplicateBehavior: "reuse durable intent and return the attributable outcome; never repeat an ambiguous effect",
		ExpectedRevision:  "required-current-owner",
		Transaction:       "short intent transaction; external effect; short outcome transaction",
		Repositories:      explicitList(repositories),
		DurableEvents:     explicitList(events),
		ExternalEffect:    effect,
		EffectIntent:      "persist identity and authorized parameters before execution",
		EffectOutcome:     "persist attributable success, failure, cancellation, or unknown outcome",
		Ambiguity:         "reconcile by effect identity; never re-execute until attribution is known",
		Retry:             "reuse intent and outcome; retry only a classified safe failure",
		Cancellation:      "cooperative; persist known or ambiguous outcome; never rewrite committed success",
		Errors:            cloneStrings(stableErrors),
	}
}

func cloneRPCContracts(values []RPCContract) []RPCContract {
	result := make([]RPCContract, len(values))
	for index, value := range values {
		value.HandlerSteps = cloneStrings(value.HandlerSteps)
		value.Function = cloneFunctionContract(value.Function)
		result[index] = value
	}
	return result
}

func cloneFunctionContracts(values []FunctionContract) []FunctionContract {
	result := make([]FunctionContract, len(values))
	for index, value := range values {
		result[index] = cloneFunctionContract(value)
	}
	return result
}

func cloneFunctionContract(value FunctionContract) FunctionContract {
	value.Repositories = cloneStrings(value.Repositories)
	value.DurableEvents = cloneStrings(value.DurableEvents)
	value.Errors = cloneStrings(value.Errors)
	return value
}

func cloneStrings(values []string) []string {
	return append([]string(nil), values...)
}

func explicitList(values []string) []string {
	if len(values) == 0 {
		return []string{"none"}
	}
	return cloneStrings(values)
}
