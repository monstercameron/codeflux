package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

// TaskRequirementRevision preserves structured intake over the original
// immutable user message.
type TaskRequirementRevision struct {
	TaskID                domain.TaskID
	Revision              uint64
	MessageID             domain.MessageID
	OriginalBodySHA256    string
	Analysis              RequirementAnalysis
	ContentSHA256         string
	RequiresClarification bool
	ClarificationQuestion *string
	IdempotencyKey        string
	CreatedAt             time.Time
}

// RecordTaskRequirement binds analysis to a user message that was persisted
// before this operation.
type RecordTaskRequirement struct {
	TaskID         domain.TaskID
	MessageID      domain.MessageID
	Analysis       RequirementAnalysis
	IdempotencyKey string
}

// PlanRevision is one immutable fully bound structured plan.
type PlanRevision struct {
	TaskID                 domain.TaskID
	Revision               uint64
	RequirementRevision    uint64
	RepositoryID           domain.RepositoryID
	RepositoryRevision     string
	ContextManifestID      string
	PolicyRevision         uint64
	ForecastRevision       uint64
	BudgetID               domain.BudgetID
	BudgetLimitRevision    uint64
	BudgetSnapshotRevision uint64
	RiskLevel              domain.RiskLevel
	ValidationProfile      ValidationProfileName
	ApprovalRequired       bool
	SupersedesRevision     *uint64
	RedirectMessageID      *domain.MessageID
	Plan                   AgentPlan
	ContentSHA256          string
	UserSummary            string
	PresentationJSON       string
	PresentationSHA256     string
	ApprovalID             *domain.ApprovalID
	IdempotencyKey         string
	CreatedAt              time.Time
}

// RecordPlanRevision declares one initial or redirected immutable plan.
type RecordPlanRevision struct {
	TaskID                 domain.TaskID
	RequirementRevision    uint64
	RepositoryRevision     string
	ContextManifestID      string
	PolicyRevision         uint64
	ForecastRevision       uint64
	BudgetID               domain.BudgetID
	BudgetLimitRevision    uint64
	BudgetSnapshotRevision uint64
	Plan                   AgentPlan
	SupersedesRevision     *uint64
	RedirectMessageID      *domain.MessageID
	IdempotencyKey         string
}

// ApprovePlanRevision binds a granted approval to the exact plan digest.
type ApprovePlanRevision struct {
	TaskID         domain.TaskID
	PlanRevision   uint64
	ApprovalID     domain.ApprovalID
	IdempotencyKey string
}

// PlanApprovalBinding is immutable exact plan authority.
type PlanApprovalBinding struct {
	TaskID         domain.TaskID
	PlanRevision   uint64
	ApprovalID     domain.ApprovalID
	PlanSHA256     string
	IdempotencyKey string
	CreatedAt      time.Time
}

// BindRunPlan attributes a started exact execution preflight to the current
// approved plan before any agent action can be recorded.
type BindRunPlan struct {
	RunID          domain.RunID
	TaskID         domain.TaskID
	PlanRevision   uint64
	IdempotencyKey string
}

// RunPlanBinding is the immutable plan/policy/forecast/budget execution root.
type RunPlanBinding struct {
	RunID                  domain.RunID
	TaskID                 domain.TaskID
	PlanRevision           uint64
	PlanSHA256             string
	PolicyRevision         uint64
	ForecastRevision       uint64
	BudgetID               domain.BudgetID
	BudgetLimitRevision    uint64
	BudgetSnapshotRevision uint64
	IdempotencyKey         string
	CreatedAt              time.Time
}

// RecordTaskRequirement persists deterministic intake only after the original
// user message is already durable and bound to the task.
func (repositories *Repositories) RecordTaskRequirement(
	ctx context.Context,
	input RecordTaskRequirement,
) (TaskRequirementRevision, error) {
	if input.TaskID.IsZero() || input.MessageID.IsZero() {
		return TaskRequirementRevision{},
			errors.New("task and original message are required")
	}
	if err := input.Analysis.Validate(); err != nil {
		return TaskRequirementRevision{}, err
	}
	if err := validateBounded(
		"requirement idempotency key",
		input.IdempotencyKey,
		255,
	); err != nil {
		return TaskRequirementRevision{}, err
	}
	var recorded TaskRequirementRevision
	err := repositories.database.RunInTransaction(
		ctx,
		func(transaction *Transaction) error {
			var body string
			if err := transaction.sql.QueryRowContext(
				ctx,
				`SELECT message.body_redacted
				 FROM tasks AS task
				 JOIN messages AS message
				   ON message.id = task.request_message_id
				  AND message.thread_id = task.thread_id
				  AND message.role = 'user'
				 WHERE task.id = ? AND message.id = ?`,
				input.TaskID,
				input.MessageID,
			).Scan(&body); err != nil {
				return typedError(
					ErrConstraint,
					"record task requirement",
					err,
				)
			}
			deterministic, err := AnalyzeTaskRequirement(body)
			if err != nil {
				return typedError(
					ErrConstraint,
					"analyze stored task requirement",
					err,
				)
			}
			analysisJSON, err := json.Marshal(deterministic)
			if err != nil {
				return err
			}
			suppliedJSON, err := json.Marshal(input.Analysis)
			if err != nil {
				return err
			}
			if string(suppliedJSON) != string(analysisJSON) {
				return typedError(
					ErrConflict,
					"record task requirement",
					errors.New(
						"supplied analysis differs from deterministic stored-message analysis",
					),
				)
			}
			existing, found, err := findTaskRequirement(
				ctx,
				transaction.sql,
				input.TaskID,
				input.IdempotencyKey,
			)
			if err != nil {
				return err
			}
			if found {
				if existing.MessageID != input.MessageID ||
					existing.ContentSHA256 != hashJSON(string(analysisJSON)) {
					return typedError(
						ErrConflict,
						"record task requirement",
						errors.New("idempotency key belongs to another requirement"),
					)
				}
				recorded = existing
				return nil
			}
			var revision uint64
			if err := transaction.sql.QueryRowContext(
				ctx,
				`SELECT COALESCE(MAX(revision), 0) + 1
				 FROM task_requirement_revisions WHERE task_id = ?`,
				input.TaskID,
			).Scan(&revision); err != nil {
				return classify("allocate requirement revision", err)
			}
			question := deterministic.ClarificationQuestion()
			var nullableQuestion any
			if question != "" {
				nullableQuestion = question
			}
			_, micros := repositories.timestamp()
			contentSHA := hashJSON(string(analysisJSON))
			if _, err := transaction.sql.ExecContext(
				ctx,
				`INSERT INTO task_requirement_revisions (
					task_id, revision, message_id, original_body_sha256,
					task_type, risk_level, validation_profile,
					requires_clarification, clarification_question,
					analysis_json, content_sha256, idempotency_key,
					created_at_unix_micros
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				input.TaskID,
				revision,
				input.MessageID,
				hashJSON(body),
				deterministic.TaskType,
				deterministic.RiskLevel,
				deterministic.ValidationProfile,
				boolInteger(deterministic.RequiresClarification()),
				nullableQuestion,
				string(analysisJSON),
				contentSHA,
				input.IdempotencyKey,
				micros,
			); err != nil {
				return repositoryWriteError("record task requirement", err)
			}
			recorded, _, err = findTaskRequirement(
				ctx,
				transaction.sql,
				input.TaskID,
				input.IdempotencyKey,
			)
			return err
		},
	)
	return recorded, err
}

// GetTaskRequirement reads one immutable intake revision.
func (repositories *Repositories) GetTaskRequirement(
	ctx context.Context,
	taskID domain.TaskID,
	revision uint64,
) (TaskRequirementRevision, error) {
	if taskID.IsZero() || revision == 0 {
		return TaskRequirementRevision{},
			errors.New("task and requirement revision are required")
	}
	return scanTaskRequirement(
		repositories.database.sql.QueryRowContext(
			ctx,
			taskRequirementSelect+` WHERE task_id = ? AND revision = ?`,
			taskID,
			revision,
		),
	)
}

// RecordPlanRevision writes a complete plan, its normalized steps and graph
// links, and an exact forecast/budget presentation in one transaction.
func (repositories *Repositories) RecordPlanRevision(
	ctx context.Context,
	input RecordPlanRevision,
) (PlanRevision, error) {
	if err := validateRecordPlanRevision(input); err != nil {
		return PlanRevision{}, err
	}
	canonical, err := json.Marshal(input.Plan)
	if err != nil {
		return PlanRevision{}, err
	}
	contentSHA := hashJSON(string(canonical))
	var recorded PlanRevision
	err = repositories.database.RunInTransaction(
		ctx,
		func(transaction *Transaction) error {
			existing, found, err := findPlanRevisionByIdempotency(
				ctx,
				transaction.sql,
				input.TaskID,
				input.IdempotencyKey,
			)
			if err != nil {
				return err
			}
			if found {
				if existing.ContentSHA256 != contentSHA ||
					existing.RequirementRevision != input.RequirementRevision ||
					existing.RepositoryRevision != input.RepositoryRevision ||
					existing.ContextManifestID != input.ContextManifestID ||
					existing.PolicyRevision != input.PolicyRevision ||
					existing.ForecastRevision != input.ForecastRevision ||
					existing.BudgetID != input.BudgetID ||
					existing.BudgetLimitRevision != input.BudgetLimitRevision ||
					existing.BudgetSnapshotRevision != input.BudgetSnapshotRevision ||
					!sameUint64Pointer(
						existing.SupersedesRevision,
						input.SupersedesRevision,
					) ||
					!sameMessageIDPointer(
						existing.RedirectMessageID,
						input.RedirectMessageID,
					) {
					return typedError(
						ErrConflict,
						"record plan revision",
						errors.New("idempotency key belongs to another plan"),
					)
				}
				recorded = existing
				return nil
			}
			requirement, err := scanTaskRequirement(
				transaction.sql.QueryRowContext(
					ctx,
					taskRequirementSelect+
						` WHERE task_id = ? AND revision = ?`,
					input.TaskID,
					input.RequirementRevision,
				),
			)
			if err != nil {
				return err
			}
			if requirement.RequiresClarification {
				return typedError(
					ErrConstraint,
					"record plan revision task type",
					errors.New("requirement still needs targeted clarification"),
				)
			}
			if input.Plan.TaskType != requirement.Analysis.TaskType {
				return typedError(
					ErrConstraint,
					"record plan revision",
					errors.New("plan task type differs from durable requirement"),
				)
			}
			if !slices.Equal(
				input.Plan.ExplicitValidationCommands,
				requirement.Analysis.ExplicitCommands,
			) {
				return typedError(
					ErrConstraint,
					"record plan revision explicit validation commands",
					errors.New(
						"plan explicit validation commands differ from durable requirement",
					),
				)
			}
			canonicalExplicit, err :=
				canonicalRequirementValidationCommands(
					requirement.Analysis.ExplicitCommands,
				)
			if err != nil {
				return typedError(
					ErrConstraint,
					"record plan revision validation command",
					err,
				)
			}
			for _, command := range canonicalExplicit {
				if !slices.Contains(input.Plan.ValidationCommands, command) {
					return typedError(
						ErrConstraint,
						"record plan revision validation command",
						errors.New(
							"plan omitted canonical explicit validation command",
						),
					)
				}
			}
			for _, explicitFile := range requirement.Analysis.ExplicitFiles {
				if !slices.Contains(input.Plan.ExpectedFiles, explicitFile) {
					return typedError(
						ErrConstraint,
						"record plan revision explicit files",
						fmt.Errorf(
							"plan omitted explicit requirement file %q",
							explicitFile,
						),
					)
				}
			}
			var repositoryID domain.RepositoryID
			var contextRequirementSHA string
			if err := transaction.sql.QueryRowContext(
				ctx,
				`SELECT task.repository_id, context.requirement_sha256
				 FROM tasks AS task
				 JOIN context_manifests AS context
				   ON context.repository_id = task.repository_id
				  AND context.id = ?
				  AND context.repository_revision = ?
				 WHERE task.id = ?`,
				input.ContextManifestID,
				input.RepositoryRevision,
				input.TaskID,
			).Scan(&repositoryID, &contextRequirementSHA); err != nil {
				return typedError(
					ErrConstraint,
					"record plan revision context",
					err,
				)
			}
			if contextRequirementSHA != requirement.OriginalBodySHA256 {
				return typedError(
					ErrConstraint,
					"record plan revision",
					errors.New("context was selected for another requirement"),
				)
			}
			policyRevision, err := scanExecutionPolicy(
				transaction.sql.QueryRowContext(
					ctx,
					`SELECT task_id, revision, policy_version,
					        selection_source, canonical_json, content_sha256,
					        idempotency_key, created_at_unix_micros
					 FROM execution_policy_revisions
					 WHERE task_id = ? AND revision = ?`,
					input.TaskID,
					input.PolicyRevision,
				),
				"load plan policy",
			)
			if err != nil {
				return err
			}
			forecastRevision, err := scanForecast(
				transaction.sql.QueryRowContext(
					ctx,
					`SELECT task_id, revision, policy_revision,
					        algorithm_version, canonical_json, content_sha256,
					        features_json, features_sha256,
					        counterfactual_eligible, eligibility_json,
					        idempotency_key, created_at_unix_micros
					 FROM effort_forecast_revisions
					 WHERE task_id = ? AND revision = ?`,
					input.TaskID,
					input.ForecastRevision,
				),
				"load plan forecast",
			)
			if err != nil {
				return err
			}
			if forecastRevision.PolicyRevision != input.PolicyRevision {
				return typedError(
					ErrConstraint,
					"record plan revision",
					errors.New("forecast belongs to another policy"),
				)
			}
			budget, err := computeBudgetSnapshot(
				ctx,
				transaction.sql,
				input.BudgetID,
			)
			if err != nil {
				return err
			}
			if budget.TaskID != input.TaskID ||
				budget.LimitRevision != input.BudgetLimitRevision ||
				budget.Revision != input.BudgetSnapshotRevision {
				return typedError(
					ErrStaleRevision,
					"record plan revision",
					errors.New("budget snapshot binding changed"),
				)
			}
			forecastBudgetJSON, err := marshalExecutionPresentation(
				policyRevision.CanonicalJSON,
				forecastRevision.CanonicalJSON,
				budget,
			)
			if err != nil {
				return err
			}
			presentation, err := json.Marshal(struct {
				SchemaVersion  uint64          `json:"schema_version"`
				Plan           json.RawMessage `json:"plan"`
				UserSummary    string          `json:"user_summary"`
				ForecastBudget json.RawMessage `json:"forecast_budget"`
			}{
				SchemaVersion:  1,
				Plan:           canonical,
				UserSummary:    input.Plan.UserSummary(),
				ForecastBudget: json.RawMessage(forecastBudgetJSON),
			})
			if err != nil {
				return err
			}
			var revision uint64
			if err := transaction.sql.QueryRowContext(
				ctx,
				`SELECT COALESCE(MAX(revision), 0) + 1
				 FROM agent_plan_revisions WHERE task_id = ?`,
				input.TaskID,
			).Scan(&revision); err != nil {
				return classify("allocate plan revision", err)
			}
			if revision == 1 {
				if input.SupersedesRevision != nil ||
					input.RedirectMessageID != nil {
					return errors.New("initial plan cannot be a redirection")
				}
			} else if input.SupersedesRevision == nil ||
				*input.SupersedesRevision != revision-1 ||
				input.RedirectMessageID == nil {
				return errors.New(
					"redirected plan must supersede the current revision and bind a user message",
				)
			}
			_, micros := repositories.timestamp()
			if _, err := transaction.sql.ExecContext(
				ctx,
				`INSERT INTO agent_plan_revisions (
					task_id, revision, requirement_revision, repository_id,
					repository_revision, context_manifest_id, policy_revision,
					forecast_revision, budget_id, budget_limit_revision,
					budget_snapshot_revision, risk_level, validation_profile,
					approval_required, supersedes_revision,
					redirect_message_id, canonical_json, content_sha256,
					user_summary, presentation_json, presentation_sha256,
					idempotency_key, created_at_unix_micros
				) VALUES (
					?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
					?, ?, ?, ?, ?
				)`,
				input.TaskID,
				revision,
				input.RequirementRevision,
				repositoryID,
				input.RepositoryRevision,
				input.ContextManifestID,
				input.PolicyRevision,
				input.ForecastRevision,
				input.BudgetID,
				input.BudgetLimitRevision,
				input.BudgetSnapshotRevision,
				requirement.Analysis.RiskLevel,
				requirement.Analysis.ValidationProfile,
				boolInteger(
					requirement.Analysis.RiskLevel != domain.RiskLevelRoutine,
				),
				nullableUint64(input.SupersedesRevision),
				nullableMessageID(input.RedirectMessageID),
				string(canonical),
				contentSHA,
				input.Plan.UserSummary(),
				string(presentation),
				hashJSON(string(presentation)),
				input.IdempotencyKey,
				micros,
			); err != nil {
				return repositoryWriteError("record plan revision", err)
			}
			for index, step := range input.Plan.Steps {
				filesJSON, _ := json.Marshal(step.ExpectedFiles)
				validationJSON, _ := json.Marshal(step.ValidationCommands)
				completionToolsJSON, _ := json.Marshal(step.CompletionTools)
				authorityJSON, _ := json.Marshal(step.AuthorityNeeds)
				if _, err := transaction.sql.ExecContext(
					ctx,
					`INSERT INTO agent_plan_steps (
						task_id, plan_revision, step_id, ordinal, step_kind, title,
						detail_redacted, expected_files_json,
						validation_commands_json, completion_tools_json,
						authority_needs_json
					) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
					input.TaskID,
					revision,
					step.ID,
					index+1,
					step.Kind,
					step.Title,
					step.DetailRedacted,
					string(filesJSON),
					string(validationJSON),
					string(completionToolsJSON),
					string(authorityJSON),
				); err != nil {
					return repositoryWriteError("record plan step", err)
				}
				for nodeIndex, nodeID := range step.GraphNodeIDs {
					if _, err := transaction.sql.ExecContext(
						ctx,
						`INSERT INTO agent_plan_step_graph_nodes (
							task_id, plan_revision, step_id, node_id, ordinal
						) VALUES (?, ?, ?, ?, ?)`,
						input.TaskID,
						revision,
						step.ID,
						nodeID,
						nodeIndex,
					); err != nil {
						return repositoryWriteError(
							"record plan step graph node",
							err,
						)
					}
				}
			}
			recorded, err = scanPlanRevision(
				transaction.sql.QueryRowContext(
					ctx,
					planRevisionSelect+
						` WHERE plan.task_id = ? AND plan.revision = ?`,
					input.TaskID,
					revision,
				),
			)
			return err
		},
	)
	return recorded, err
}

// GetPlanRevision reads one immutable plan.
func (repositories *Repositories) GetPlanRevision(
	ctx context.Context,
	taskID domain.TaskID,
	revision uint64,
) (PlanRevision, error) {
	if taskID.IsZero() || revision == 0 {
		return PlanRevision{}, errors.New("task and plan revision are required")
	}
	return scanPlanRevision(
		repositories.database.sql.QueryRowContext(
			ctx,
			planRevisionSelect+` WHERE plan.task_id = ? AND plan.revision = ?`,
			taskID,
			revision,
		),
	)
}

// GetCurrentPlanRevision returns the only revision eligible for new execution.
func (repositories *Repositories) GetCurrentPlanRevision(
	ctx context.Context,
	taskID domain.TaskID,
) (PlanRevision, error) {
	if taskID.IsZero() {
		return PlanRevision{}, errors.New("task is required")
	}
	return scanPlanRevision(
		repositories.database.sql.QueryRowContext(
			ctx,
			planRevisionSelect+
				` WHERE plan.task_id = ?
				  ORDER BY plan.revision DESC LIMIT 1`,
			taskID,
		),
	)
}

// ApprovePlanRevision records exact granted plan authority.
func (repositories *Repositories) ApprovePlanRevision(
	ctx context.Context,
	input ApprovePlanRevision,
) (PlanApprovalBinding, error) {
	if input.TaskID.IsZero() || input.PlanRevision == 0 ||
		input.ApprovalID.IsZero() {
		return PlanApprovalBinding{},
			errors.New("task, plan revision, and approval are required")
	}
	if err := validateBounded(
		"plan approval idempotency key",
		input.IdempotencyKey,
		255,
	); err != nil {
		return PlanApprovalBinding{}, err
	}
	var binding PlanApprovalBinding
	err := repositories.database.RunInTransaction(
		ctx,
		func(transaction *Transaction) error {
			plan, err := scanPlanRevision(
				transaction.sql.QueryRowContext(
					ctx,
					planRevisionSelect+
						` WHERE plan.task_id = ? AND plan.revision = ?`,
					input.TaskID,
					input.PlanRevision,
				),
			)
			if err != nil {
				return err
			}
			var existingApproval domain.ApprovalID
			var existingSHA, existingKey string
			var createdMicros int64
			err = transaction.sql.QueryRowContext(
				ctx,
				`SELECT approval_id, plan_sha256, idempotency_key,
				        created_at_unix_micros
				 FROM agent_plan_approval_bindings
				 WHERE task_id = ? AND (
				    plan_revision = ? OR idempotency_key = ?
				 )`,
				input.TaskID,
				input.PlanRevision,
				input.IdempotencyKey,
			).Scan(
				&existingApproval,
				&existingSHA,
				&existingKey,
				&createdMicros,
			)
			if err == nil {
				if existingApproval != input.ApprovalID ||
					existingSHA != plan.ContentSHA256 ||
					existingKey != input.IdempotencyKey {
					return typedError(
						ErrConflict,
						"approve plan revision",
						errors.New("plan approval identity was reused"),
					)
				}
				binding = PlanApprovalBinding{
					TaskID: input.TaskID, PlanRevision: input.PlanRevision,
					ApprovalID: input.ApprovalID, PlanSHA256: existingSHA,
					IdempotencyKey: existingKey,
					CreatedAt:      repositoryTime(createdMicros),
				}
				return nil
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return classify("find plan approval", err)
			}
			var state domain.ApprovalRequestState
			var scope string
			if err := transaction.sql.QueryRowContext(
				ctx,
				`SELECT state, scope FROM approvals WHERE id = ? AND task_id = ?`,
				input.ApprovalID,
				input.TaskID,
			).Scan(&state, &scope); err != nil {
				return typedError(ErrConstraint, "approve plan revision", err)
			}
			if state != domain.ApprovalRequestStateGranted {
				return typedError(
					ErrConstraint,
					"approve plan revision",
					errors.New("approval is not granted"),
				)
			}
			if scope != PlanApprovalScope(plan) {
				return typedError(
					ErrConstraint,
					"approve plan revision",
					errors.New("approval scope differs from exact plan"),
				)
			}
			_, micros := repositories.timestamp()
			if _, err := transaction.sql.ExecContext(
				ctx,
				`INSERT INTO agent_plan_approval_bindings (
					task_id, plan_revision, approval_id, plan_sha256,
					idempotency_key, created_at_unix_micros
				) VALUES (?, ?, ?, ?, ?, ?)`,
				input.TaskID,
				input.PlanRevision,
				input.ApprovalID,
				plan.ContentSHA256,
				input.IdempotencyKey,
				micros,
			); err != nil {
				return repositoryWriteError("approve plan revision", err)
			}
			binding = PlanApprovalBinding{
				TaskID: input.TaskID, PlanRevision: input.PlanRevision,
				ApprovalID: input.ApprovalID, PlanSHA256: plan.ContentSHA256,
				IdempotencyKey: input.IdempotencyKey,
				CreatedAt:      repositoryTime(micros),
			}
			return nil
		},
	)
	return binding, err
}

// BindRunPlan rejects a superseded, unapproved, or execution-inconsistent
// plan. It is also, since AUDIT-020a, the primary place a run's durable
// state moves from "starting" to "running": the agent execution loop calls
// this once, early, before any tool call or file change, so binding the run
// to the plan it executes is the first durable fact that real work has
// begun. See transitionRunStartingToRunning for the transition itself and
// why it runs unconditionally here, ahead of this function's own
// idempotency check.
func (repositories *Repositories) BindRunPlan(
	ctx context.Context,
	input BindRunPlan,
) (RunPlanBinding, error) {
	if input.RunID.IsZero() || input.TaskID.IsZero() || input.PlanRevision == 0 {
		return RunPlanBinding{}, errors.New("run, task, and plan revision are required")
	}
	if err := validateBounded(
		"run plan idempotency key",
		input.IdempotencyKey,
		255,
	); err != nil {
		return RunPlanBinding{}, err
	}
	var binding RunPlanBinding
	err := repositories.database.RunInTransaction(
		ctx,
		func(transaction *Transaction) error {
			_, micros := repositories.timestamp()
			// The run reaches "running" here, regardless of whether this bind
			// is the first for the run or an idempotent/resumed replay
			// (AUDIT-020a): binding a run to the plan it executes is the
			// first durable evidence real work is beginning, and it recurs
			// identically after a pause/resume cycle puts the run row back at
			// "starting" (ResumeControlledTask). A run already at "running",
			// or in any other state, is left untouched.
			if err := transitionRunStartingToRunning(
				ctx, transaction, input.RunID, input.TaskID, micros,
			); err != nil {
				return err
			}
			existing, found, err := findRunPlanBinding(
				ctx,
				transaction.sql,
				input.RunID,
			)
			if err != nil {
				return err
			}
			if found {
				if existing.TaskID != input.TaskID ||
					existing.PlanRevision != input.PlanRevision ||
					existing.IdempotencyKey != input.IdempotencyKey {
					return typedError(
						ErrConflict,
						"bind run plan",
						errors.New("run is already bound to another plan"),
					)
				}
				binding = existing
				return nil
			}
			plan, err := scanPlanRevision(
				transaction.sql.QueryRowContext(
					ctx,
					planRevisionSelect+
						` WHERE plan.task_id = ? AND plan.revision = ?`,
					input.TaskID,
					input.PlanRevision,
				),
			)
			if err != nil {
				return err
			}
			var latest uint64
			if err := transaction.sql.QueryRowContext(
				ctx,
				`SELECT MAX(revision) FROM agent_plan_revisions
				 WHERE task_id = ?`,
				input.TaskID,
			).Scan(&latest); err != nil {
				return classify("load current plan revision", err)
			}
			if latest != input.PlanRevision {
				return typedError(
					ErrStaleRevision,
					"bind run plan",
					errors.New("plan was superseded"),
				)
			}
			if plan.ApprovalRequired && plan.ApprovalID == nil {
				return typedError(
					ErrConstraint,
					"bind run plan",
					errors.New("plan approval is required"),
				)
			}
			if _, err := transaction.sql.ExecContext(
				ctx,
				`INSERT INTO run_plan_bindings (
					run_id, task_id, plan_revision, plan_sha256,
					policy_revision, forecast_revision, budget_id,
					budget_limit_revision, budget_snapshot_revision,
					idempotency_key, created_at_unix_micros
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				input.RunID,
				input.TaskID,
				input.PlanRevision,
				plan.ContentSHA256,
				plan.PolicyRevision,
				plan.ForecastRevision,
				plan.BudgetID,
				plan.BudgetLimitRevision,
				plan.BudgetSnapshotRevision,
				input.IdempotencyKey,
				micros,
			); err != nil {
				return repositoryWriteError("bind run plan", err)
			}
			binding, _, err = findRunPlanBinding(
				ctx,
				transaction.sql,
				input.RunID,
			)
			return err
		},
	)
	return binding, err
}

// GetRunPlanBinding reads one immutable run attribution root.
func (repositories *Repositories) GetRunPlanBinding(
	ctx context.Context,
	runID domain.RunID,
) (RunPlanBinding, error) {
	if runID.IsZero() {
		return RunPlanBinding{}, errors.New("run is required")
	}
	value, found, err := findRunPlanBinding(
		ctx,
		repositories.database.sql,
		runID,
	)
	if err != nil {
		return RunPlanBinding{}, err
	}
	if !found {
		return RunPlanBinding{},
			typedError(ErrNotFound, "get run plan binding", sql.ErrNoRows)
	}
	return value, nil
}

const taskRequirementSelect = `SELECT
	task_id, revision, message_id, original_body_sha256, analysis_json,
	content_sha256, requires_clarification, clarification_question,
	idempotency_key, created_at_unix_micros
 FROM task_requirement_revisions`

func findTaskRequirement(
	ctx context.Context,
	queries queryRower,
	taskID domain.TaskID,
	key string,
) (TaskRequirementRevision, bool, error) {
	value, err := scanTaskRequirement(
		queries.QueryRowContext(
			ctx,
			taskRequirementSelect+
				` WHERE task_id = ? AND idempotency_key = ?`,
			taskID,
			key,
		),
	)
	if errors.Is(err, ErrNotFound) {
		return TaskRequirementRevision{}, false, nil
	}
	return value, err == nil, err
}

func scanTaskRequirement(row rowScanner) (TaskRequirementRevision, error) {
	var (
		value         TaskRequirementRevision
		analysisJSON  string
		requires      int
		question      sql.NullString
		createdMicros int64
	)
	if err := row.Scan(
		&value.TaskID,
		&value.Revision,
		&value.MessageID,
		&value.OriginalBodySHA256,
		&analysisJSON,
		&value.ContentSHA256,
		&requires,
		&question,
		&value.IdempotencyKey,
		&createdMicros,
	); err != nil {
		return TaskRequirementRevision{},
			typedError(ErrNotFound, "read task requirement", err)
	}
	if err := json.Unmarshal([]byte(analysisJSON), &value.Analysis); err != nil {
		return TaskRequirementRevision{},
			typedError(ErrCorrupt, "decode task requirement", err)
	}
	if err := value.Analysis.Validate(); err != nil ||
		hashJSON(analysisJSON) != value.ContentSHA256 {
		return TaskRequirementRevision{},
			typedError(ErrCorrupt, "validate task requirement", err)
	}
	value.RequiresClarification = requires != 0
	if question.Valid {
		value.ClarificationQuestion = &question.String
	}
	if value.RequiresClarification != value.Analysis.RequiresClarification() ||
		(value.RequiresClarification &&
			(value.ClarificationQuestion == nil ||
				*value.ClarificationQuestion !=
					value.Analysis.ClarificationQuestion())) {
		return TaskRequirementRevision{},
			typedError(
				ErrCorrupt,
				"validate task requirement clarification",
				errors.New("clarification projection differs"),
			)
	}
	value.CreatedAt = repositoryTime(createdMicros)
	return value, nil
}

const planRevisionSelect = `SELECT
	plan.task_id, plan.revision, plan.requirement_revision,
	plan.repository_id, plan.repository_revision, plan.context_manifest_id,
	plan.policy_revision, plan.forecast_revision, plan.budget_id,
	plan.budget_limit_revision, plan.budget_snapshot_revision,
	plan.risk_level, plan.validation_profile, plan.approval_required,
	plan.supersedes_revision, plan.redirect_message_id, plan.canonical_json,
	plan.content_sha256, plan.user_summary, plan.presentation_json,
	plan.presentation_sha256, approved.approval_id,
	plan.idempotency_key, plan.created_at_unix_micros
 FROM agent_plan_revisions AS plan
 LEFT JOIN agent_plan_approval_bindings AS approved
   ON approved.task_id = plan.task_id
  AND approved.plan_revision = plan.revision`

func findPlanRevisionByIdempotency(
	ctx context.Context,
	queries queryRower,
	taskID domain.TaskID,
	key string,
) (PlanRevision, bool, error) {
	value, err := scanPlanRevision(
		queries.QueryRowContext(
			ctx,
			planRevisionSelect+
				` WHERE plan.task_id = ? AND plan.idempotency_key = ?`,
			taskID,
			key,
		),
	)
	if errors.Is(err, ErrNotFound) {
		return PlanRevision{}, false, nil
	}
	return value, err == nil, err
}

func scanPlanRevision(row rowScanner) (PlanRevision, error) {
	var (
		value            PlanRevision
		supersedes       sql.NullInt64
		redirectMessage  sql.NullString
		canonical        string
		approvalRequired int
		approvalID       sql.NullString
		createdMicros    int64
	)
	if err := row.Scan(
		&value.TaskID,
		&value.Revision,
		&value.RequirementRevision,
		&value.RepositoryID,
		&value.RepositoryRevision,
		&value.ContextManifestID,
		&value.PolicyRevision,
		&value.ForecastRevision,
		&value.BudgetID,
		&value.BudgetLimitRevision,
		&value.BudgetSnapshotRevision,
		&value.RiskLevel,
		&value.ValidationProfile,
		&approvalRequired,
		&supersedes,
		&redirectMessage,
		&canonical,
		&value.ContentSHA256,
		&value.UserSummary,
		&value.PresentationJSON,
		&value.PresentationSHA256,
		&approvalID,
		&value.IdempotencyKey,
		&createdMicros,
	); err != nil {
		return PlanRevision{}, typedError(ErrNotFound, "read plan revision", err)
	}
	if err := json.Unmarshal([]byte(canonical), &value.Plan); err != nil {
		return PlanRevision{}, typedError(ErrCorrupt, "decode plan revision", err)
	}
	if err := value.Plan.Validate(); err != nil ||
		hashJSON(canonical) != value.ContentSHA256 ||
		hashJSON(value.PresentationJSON) != value.PresentationSHA256 ||
		value.UserSummary != value.Plan.UserSummary() ||
		value.ValidationProfile != validationProfileForRisk(value.RiskLevel) {
		return PlanRevision{},
			typedError(ErrCorrupt, "validate plan revision", err)
	}
	value.ApprovalRequired = approvalRequired != 0
	if supersedes.Valid {
		revision := uint64(supersedes.Int64)
		value.SupersedesRevision = &revision
	}
	if redirectMessage.Valid {
		parsed, err := domain.ParseMessageID(redirectMessage.String)
		if err != nil {
			return PlanRevision{},
				typedError(ErrCorrupt, "decode plan redirect message", err)
		}
		value.RedirectMessageID = &parsed
	}
	if approvalID.Valid {
		parsed, err := domain.ParseApprovalID(approvalID.String)
		if err != nil {
			return PlanRevision{},
				typedError(ErrCorrupt, "decode plan approval", err)
		}
		value.ApprovalID = &parsed
	}
	value.CreatedAt = repositoryTime(createdMicros)
	return value, nil
}

func findRunPlanBinding(
	ctx context.Context,
	queries queryRower,
	runID domain.RunID,
) (RunPlanBinding, bool, error) {
	var value RunPlanBinding
	var createdMicros int64
	err := queries.QueryRowContext(
		ctx,
		`SELECT run_id, task_id, plan_revision, plan_sha256,
		        policy_revision, forecast_revision, budget_id,
		        budget_limit_revision, budget_snapshot_revision,
		        idempotency_key, created_at_unix_micros
		 FROM run_plan_bindings WHERE run_id = ?`,
		runID,
	).Scan(
		&value.RunID,
		&value.TaskID,
		&value.PlanRevision,
		&value.PlanSHA256,
		&value.PolicyRevision,
		&value.ForecastRevision,
		&value.BudgetID,
		&value.BudgetLimitRevision,
		&value.BudgetSnapshotRevision,
		&value.IdempotencyKey,
		&createdMicros,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return RunPlanBinding{}, false, nil
	}
	if err != nil {
		return RunPlanBinding{}, false, classify("read run plan binding", err)
	}
	value.CreatedAt = repositoryTime(createdMicros)
	return value, true, nil
}

func validateRecordPlanRevision(input RecordPlanRevision) error {
	switch {
	case input.TaskID.IsZero(), input.RequirementRevision == 0:
		return errors.New("task and requirement revision are required")
	case len(input.RepositoryRevision) != 40 &&
		len(input.RepositoryRevision) != 64:
		return errors.New("repository revision must be a Git object hash")
	case !lowerHex(input.RepositoryRevision):
		return errors.New("repository revision must be lowercase hexadecimal")
	case strings.TrimSpace(input.ContextManifestID) != input.ContextManifestID,
		len(input.ContextManifestID) != 64,
		!lowerHex(input.ContextManifestID):
		return errors.New("context manifest identity is invalid")
	case input.PolicyRevision == 0, input.ForecastRevision == 0:
		return errors.New("policy and forecast revisions are required")
	case input.BudgetID.IsZero():
		return errors.New("budget is required")
	}
	if err := input.Plan.Validate(); err != nil {
		return err
	}
	return validateBounded("plan idempotency key", input.IdempotencyKey, 255)
}

func lowerHex(value string) bool {
	for _, character := range value {
		if character < '0' || character > '9' {
			if character < 'a' || character > 'f' {
				return false
			}
		}
	}
	return true
}

func nullableUint64(value *uint64) any {
	if value == nil {
		return nil
	}
	return *value
}

func sameUint64Pointer(left, right *uint64) bool {
	return left == nil && right == nil ||
		left != nil && right != nil && *left == *right
}

func sameMessageIDPointer(
	left, right *domain.MessageID,
) bool {
	return left == nil && right == nil ||
		left != nil && right != nil && *left == *right
}

// PlanApprovalScope returns the exact human-authority scope string.
func PlanApprovalScope(plan PlanRevision) string {
	return fmt.Sprintf(
		"plan:%s:%d:%s",
		plan.TaskID,
		plan.Revision,
		plan.ContentSHA256,
	)
}
