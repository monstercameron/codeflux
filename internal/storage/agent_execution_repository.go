package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

// AgentToolResultState is the immutable outcome classification for one tool
// request.
type AgentToolResultState string

const (
	AgentToolResultSucceeded      AgentToolResultState = "succeeded"
	AgentToolResultFailed         AgentToolResultState = "failed"
	AgentToolResultCancelled      AgentToolResultState = "cancelled"
	AgentToolResultOutcomeUnknown AgentToolResultState = "outcome-unknown"
)

func (value AgentToolResultState) valid() bool {
	switch value {
	case AgentToolResultSucceeded, AgentToolResultFailed,
		AgentToolResultCancelled, AgentToolResultOutcomeUnknown:
		return true
	default:
		return false
	}
}

// PlanStepState is the attributable execution state of one immutable step.
type PlanStepState string

const (
	PlanStepPending     PlanStepState = "pending"
	PlanStepInProgress  PlanStepState = "in-progress"
	PlanStepImplemented PlanStepState = "implemented"
	PlanStepValidated   PlanStepState = "validated"
	PlanStepFailed      PlanStepState = "failed"
	PlanStepSkipped     PlanStepState = "skipped"
)

func (value PlanStepState) valid() bool {
	switch value {
	case PlanStepPending, PlanStepInProgress, PlanStepImplemented,
		PlanStepValidated, PlanStepFailed, PlanStepSkipped:
		return true
	default:
		return false
	}
}

// AgentToolRequest is one model-originated, schema-bound tool intent.
type AgentToolRequest struct {
	ID                    string
	TaskID                domain.TaskID
	RunID                 domain.RunID
	PlanRevision          uint64
	PlanStepID            string
	ModelRequestID        domain.ModelRequestID
	ToolName              string
	ToolSchemaVersion     uint64
	ArgumentsRedactedJSON string
	ArgumentsSHA256       string
	PermissionDecisionID  *string
	IdempotencyKey        string
	CreatedAt             time.Time
}

// RecordAgentToolRequest declares a tool intent before any external effect.
type RecordAgentToolRequest struct {
	ID                    string
	TaskID                domain.TaskID
	RunID                 domain.RunID
	PlanRevision          uint64
	PlanStepID            string
	ModelRequestID        domain.ModelRequestID
	ToolName              string
	ToolSchemaVersion     uint64
	ArgumentsRedactedJSON string
	ArgumentsSHA256       string
	PermissionDecisionID  *string
	IdempotencyKey        string
}

// AgentToolResult is one bounded already-redacted outcome.
type AgentToolResult struct {
	ID                 string
	ToolRequestID      string
	State              AgentToolResultState
	ResultRedactedJSON string
	ResultSHA256       string
	CommandExecutionID *string
	IdempotencyKey     string
	CreatedAt          time.Time
}

// RecordAgentToolResult declares the result after mediated execution.
type RecordAgentToolResult struct {
	ID                 string
	ToolRequestID      string
	State              AgentToolResultState
	ResultRedactedJSON string
	ResultSHA256       string
	CommandExecutionID *string
	IdempotencyKey     string
}

// PlanStepTransition is one immutable attributable state fact.
type PlanStepTransition struct {
	ID             string
	TaskID         domain.TaskID
	RunID          domain.RunID
	PlanRevision   uint64
	PlanStepID     string
	Sequence       uint64
	From           PlanStepState
	To             PlanStepState
	ReasonRedacted string
	ModelRequestID domain.ModelRequestID
	ValidationID   *domain.ValidationID
	ToolRequestID  *string
	IdempotencyKey string
	CreatedAt      time.Time
}

// RecordPlanStepTransition applies one optimistic step transition.
type RecordPlanStepTransition struct {
	ID             string
	TaskID         domain.TaskID
	RunID          domain.RunID
	PlanRevision   uint64
	PlanStepID     string
	From           PlanStepState
	To             PlanStepState
	ReasonRedacted string
	ModelRequestID domain.ModelRequestID
	ValidationID   *domain.ValidationID
	ToolRequestID  *string
	IdempotencyKey string
}

// PlanStepStatus is the current projection for one immutable plan step.
type PlanStepStatus struct {
	TaskID       domain.TaskID
	RunID        domain.RunID
	PlanRevision uint64
	PlanStepID   string
	State        PlanStepState
	Sequence     uint64
}

// PlanValidationAttribution binds a durable validation to its exact plan step.
type PlanValidationAttribution struct {
	TaskID                domain.TaskID
	RunID                 domain.RunID
	PlanRevision          uint64
	PlanStepID            string
	ValidationID          domain.ValidationID
	CommandExecutionID    *string
	RepairAttemptRevision *uint64
	IdempotencyKey        string
	CreatedAt             time.Time
}

// RecordPlanValidationAttribution declares one validation binding.
type RecordPlanValidationAttribution struct {
	TaskID                domain.TaskID
	RunID                 domain.RunID
	PlanRevision          uint64
	PlanStepID            string
	ValidationID          domain.ValidationID
	CommandExecutionID    *string
	RepairAttemptRevision *uint64
	IdempotencyKey        string
}

// RecordPlanValidationAttributions binds one durable validation to every
// declared plan step atomically. IdempotencyKey is the base identity; storage
// derives one stable link identity per PlanStepID.
type RecordPlanValidationAttributions struct {
	TaskID                   domain.TaskID
	RunID                    domain.RunID
	PlanRevision             uint64
	PlanStepIDs              []string
	ValidationID             domain.ValidationID
	CommandExecutionID       *string
	RepairAttemptRevision    *uint64
	IdempotencyKey           string
	ValidationPassed         bool
	ProfileDigest            string
	Round                    uint64
	CommandOrdinal           uint64
	CommandID                string
	CommandFingerprint       string
	PresentationRedactedJSON string
	PresentationSHA256       string
}

// RepairAttempt preserves the failed validation and ready pre-repair
// checkpoint that authorized one bounded repair round.
type RepairAttempt struct {
	TaskID                domain.TaskID
	RunID                 domain.RunID
	PlanRevision          uint64
	Revision              uint64
	Ordinal               uint64
	FailedValidationID    domain.ValidationID
	PreRepairCheckpointID domain.CheckpointID
	ReasonRedacted        string
	IdempotencyKey        string
	CreatedAt             time.Time
}

// BeginRepairAttempt declares a repair before any repair edit.
type BeginRepairAttempt struct {
	TaskID                domain.TaskID
	RunID                 domain.RunID
	PlanRevision          uint64
	Ordinal               uint64
	FailedValidationID    domain.ValidationID
	PreRepairCheckpointID domain.CheckpointID
	ReasonRedacted        string
	IdempotencyKey        string
}

// RepairAttemptOutcomeKind is the honest bounded repair result.
type RepairAttemptOutcomeKind string

const (
	RepairOutcomeValidationPassed RepairAttemptOutcomeKind = "validation-passed"
	RepairOutcomeValidationFailed RepairAttemptOutcomeKind = "validation-failed"
	RepairOutcomeBudgetExhausted  RepairAttemptOutcomeKind = "budget-exhausted"
	RepairOutcomeStopped          RepairAttemptOutcomeKind = "stopped"
)

func (value RepairAttemptOutcomeKind) valid() bool {
	switch value {
	case RepairOutcomeValidationPassed, RepairOutcomeValidationFailed,
		RepairOutcomeBudgetExhausted, RepairOutcomeStopped:
		return true
	default:
		return false
	}
}

// RepairAttemptOutcome is one immutable terminal repair fact.
type RepairAttemptOutcome struct {
	TaskID                    domain.TaskID
	RunID                     domain.RunID
	PlanRevision              uint64
	RepairAttemptRevision     uint64
	Outcome                   RepairAttemptOutcomeKind
	PostRepairValidationID    *domain.ValidationID
	UnresolvedSummaryRedacted string
	IdempotencyKey            string
	CreatedAt                 time.Time
}

// RecordRepairAttemptOutcome declares the repair result.
type RecordRepairAttemptOutcome struct {
	TaskID                    domain.TaskID
	RunID                     domain.RunID
	PlanRevision              uint64
	RepairAttemptRevision     uint64
	Outcome                   RepairAttemptOutcomeKind
	PostRepairValidationID    *domain.ValidationID
	UnresolvedSummaryRedacted string
	IdempotencyKey            string
}

// CompletionCandidate is an immutable final-state proposal, never acceptance.
type CompletionCandidate struct {
	TaskID                 domain.TaskID
	RunID                  domain.RunID
	PlanRevision           uint64
	Revision               uint64
	ExpectedTaskRevision   uint64
	ExpectedRunRevision    uint64
	EventID                domain.EventID
	EventIdempotencyKey    string
	RepositoryStatusJSON   string
	DiffSummaryJSON        string
	DiffSHA256             string
	ValidationSummaryJSON  string
	BudgetSummaryJSON      string
	AssumptionsJSON        string
	LimitationsJSON        string
	ImplementationComplete bool
	ValidationComplete     bool
	IdempotencyKey         string
	CreatedAt              time.Time
}

func (repositories *Repositories) GetLatestCompletionCandidateForTask(ctx context.Context, taskID domain.TaskID) (CompletionCandidate, error) {
	var value CompletionCandidate
	var implemented, validated int
	var micros int64
	err := repositories.database.sql.QueryRowContext(ctx, `SELECT task_id, run_id, plan_revision, revision,
		expected_task_revision, expected_run_revision, event_id, event_idempotency_key,
		repository_status_json, diff_summary_json, diff_sha256, validation_summary_json,
		budget_summary_json, assumptions_json, limitations_json, implementation_complete,
		validation_complete, idempotency_key, created_at_unix_micros
		FROM completion_candidates WHERE task_id = ?
		ORDER BY created_at_unix_micros DESC, revision DESC LIMIT 1`, taskID).Scan(
		&value.TaskID, &value.RunID, &value.PlanRevision, &value.Revision,
		&value.ExpectedTaskRevision, &value.ExpectedRunRevision, &value.EventID,
		&value.EventIdempotencyKey, &value.RepositoryStatusJSON, &value.DiffSummaryJSON,
		&value.DiffSHA256, &value.ValidationSummaryJSON, &value.BudgetSummaryJSON,
		&value.AssumptionsJSON, &value.LimitationsJSON, &implemented, &validated,
		&value.IdempotencyKey, &micros)
	if err != nil {
		return CompletionCandidate{}, repositoryReadConstraint("get latest completion candidate", err)
	}
	value.ImplementationComplete, value.ValidationComplete = implemented != 0, validated != 0
	value.CreatedAt = repositoryTime(micros)
	return value, nil
}

func (repositories *Repositories) GetRunRevision(ctx context.Context, taskID domain.TaskID, runID domain.RunID) (uint64, error) {
	var revision uint64
	if err := repositories.database.sql.QueryRowContext(ctx, `SELECT revision FROM runs WHERE id = ? AND task_id = ?`, runID, taskID).Scan(&revision); err != nil {
		return 0, repositoryReadConstraint("get run revision", err)
	}
	return revision, nil
}

// RecordCompletionCandidate atomically records the candidate and moves the
// task from validating to awaiting review.
type RecordCompletionCandidate struct {
	TaskID                 domain.TaskID
	RunID                  domain.RunID
	PlanRevision           uint64
	ExpectedTaskRevision   uint64
	ExpectedRunRevision    uint64
	EventID                domain.EventID
	EventIdempotencyKey    string
	RepositoryStatusJSON   string
	DiffSummaryJSON        string
	DiffSHA256             string
	ValidationSummaryJSON  string
	BudgetSummaryJSON      string
	AssumptionsJSON        string
	LimitationsJSON        string
	ImplementationComplete bool
	ValidationComplete     bool
	IdempotencyKey         string
}

// TaskReviewDecisionKind is the user's final authority choice.
type TaskReviewDecisionKind string

const (
	TaskReviewAccept        TaskReviewDecisionKind = "accept"
	TaskReviewRequestRepair TaskReviewDecisionKind = "request-repair"
	TaskReviewRollback      TaskReviewDecisionKind = "rollback"
	TaskReviewAbandon       TaskReviewDecisionKind = "abandon"
)

func (value TaskReviewDecisionKind) valid() bool {
	switch value {
	case TaskReviewAccept, TaskReviewRequestRepair,
		TaskReviewRollback, TaskReviewAbandon:
		return true
	default:
		return false
	}
}

// TaskReviewDecision is one immutable final human decision.
type TaskReviewDecision struct {
	TaskID               domain.TaskID
	RunID                domain.RunID
	PlanRevision         uint64
	CompletionRevision   uint64
	Revision             uint64
	ExpectedTaskRevision uint64
	ExpectedRunRevision  uint64
	EventID              domain.EventID
	EventIdempotencyKey  string
	Decision             TaskReviewDecisionKind
	ActorReference       string
	AuthorityReference   string
	ReasonRedacted       string
	MessageID            *domain.MessageID
	IdempotencyKey       string
	CreatedAt            time.Time
}

// RecordTaskReviewDecision atomically records authority and applies both task
// and run lifecycle states.
type RecordTaskReviewDecision struct {
	TaskID               domain.TaskID
	RunID                domain.RunID
	PlanRevision         uint64
	CompletionRevision   uint64
	ExpectedTaskRevision uint64
	ExpectedRunRevision  uint64
	Decision             TaskReviewDecisionKind
	ActorReference       string
	AuthorityReference   string
	ReasonRedacted       string
	MessageID            *domain.MessageID
	EventID              domain.EventID
	EventIdempotencyKey  string
	IdempotencyKey       string
}

func (repositories *Repositories) RecordAgentToolRequest(
	ctx context.Context,
	input RecordAgentToolRequest,
) (AgentToolRequest, error) {
	if err := validateAgentToolRequest(input); err != nil {
		return AgentToolRequest{}, err
	}
	var value AgentToolRequest
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		existing, found, err := findAgentToolRequest(
			ctx, transaction.sql, input.RunID, input.IdempotencyKey,
		)
		if err != nil {
			return err
		}
		if found {
			if existing.ID != input.ID ||
				existing.TaskID != input.TaskID ||
				existing.RunID != input.RunID ||
				existing.PlanRevision != input.PlanRevision ||
				existing.PlanStepID != input.PlanStepID ||
				existing.ModelRequestID != input.ModelRequestID ||
				existing.ToolName != input.ToolName ||
				existing.ToolSchemaVersion != input.ToolSchemaVersion ||
				existing.ArgumentsRedactedJSON != input.ArgumentsRedactedJSON ||
				existing.ArgumentsSHA256 != input.ArgumentsSHA256 ||
				!sameStringPointer(
					existing.PermissionDecisionID,
					input.PermissionDecisionID,
				) ||
				existing.IdempotencyKey != input.IdempotencyKey {
				return typedError(
					ErrConflict, "record agent tool request",
					errors.New("idempotency key belongs to another tool request"),
				)
			}
			value = existing
			return nil
		}
		_, micros := repositories.timestamp()
		if _, err := transaction.sql.ExecContext(ctx,
			`INSERT INTO agent_tool_requests (
				id, task_id, run_id, plan_revision, plan_step_id,
				model_request_id, tool_name, tool_schema_version,
				arguments_redacted_json, arguments_sha256,
				permission_decision_id, idempotency_key,
				created_at_unix_micros
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			input.ID, input.TaskID, input.RunID, input.PlanRevision,
			input.PlanStepID, input.ModelRequestID, input.ToolName,
			input.ToolSchemaVersion, input.ArgumentsRedactedJSON,
			input.ArgumentsSHA256, nullableString(input.PermissionDecisionID),
			input.IdempotencyKey, micros,
		); err != nil {
			return repositoryWriteError("record agent tool request", err)
		}
		value, _, err = findAgentToolRequest(
			ctx, transaction.sql, input.RunID, input.IdempotencyKey,
		)
		return err
	})
	return value, err
}

func (repositories *Repositories) RecordAgentToolResult(
	ctx context.Context,
	input RecordAgentToolResult,
) (AgentToolResult, error) {
	if err := validateAgentToolResult(input); err != nil {
		return AgentToolResult{}, err
	}
	var value AgentToolResult
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		existing, found, err := findAgentToolResult(
			ctx, transaction.sql, input.ToolRequestID,
		)
		if err != nil {
			return err
		}
		if found {
			if existing.ID != input.ID || existing.State != input.State ||
				existing.ResultRedactedJSON != input.ResultRedactedJSON ||
				existing.ResultSHA256 != input.ResultSHA256 ||
				existing.IdempotencyKey != input.IdempotencyKey ||
				!sameStringPointer(
					existing.CommandExecutionID, input.CommandExecutionID,
				) {
				return typedError(
					ErrConflict, "record agent tool result",
					errors.New("tool request already has another result"),
				)
			}
			value = existing
			return nil
		}
		_, micros := repositories.timestamp()
		if _, err := transaction.sql.ExecContext(ctx,
			`INSERT INTO agent_tool_results (
				id, tool_request_id, state, result_redacted_json,
				result_sha256, command_execution_id, idempotency_key,
				created_at_unix_micros
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			input.ID, input.ToolRequestID, input.State,
			input.ResultRedactedJSON, input.ResultSHA256,
			nullableString(input.CommandExecutionID), input.IdempotencyKey,
			micros,
		); err != nil {
			return repositoryWriteError("record agent tool result", err)
		}
		value, _, err = findAgentToolResult(
			ctx, transaction.sql, input.ToolRequestID,
		)
		return err
	})
	return value, err
}

func (repositories *Repositories) RecordPlanStepTransition(
	ctx context.Context,
	input RecordPlanStepTransition,
) (PlanStepTransition, error) {
	if err := validatePlanStepTransition(input); err != nil {
		return PlanStepTransition{}, err
	}
	var value PlanStepTransition
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		existing, found, err := findPlanStepTransition(
			ctx, transaction.sql, input.RunID, input.IdempotencyKey,
		)
		if err != nil {
			return err
		}
		if found {
			if existing.ID != input.ID ||
				existing.TaskID != input.TaskID ||
				existing.RunID != input.RunID ||
				existing.PlanRevision != input.PlanRevision ||
				existing.PlanStepID != input.PlanStepID ||
				existing.From != input.From ||
				existing.To != input.To ||
				existing.ReasonRedacted != input.ReasonRedacted ||
				existing.ModelRequestID != input.ModelRequestID ||
				!sameValidationIDPointer(
					existing.ValidationID,
					input.ValidationID,
				) ||
				!sameStringPointer(existing.ToolRequestID, input.ToolRequestID) ||
				existing.IdempotencyKey != input.IdempotencyKey {
				return typedError(
					ErrConflict, "record plan step transition",
					errors.New("idempotency key belongs to another transition"),
				)
			}
			value = existing
			return nil
		}
		current, err := currentPlanStepState(
			ctx, transaction.sql, input.RunID, input.PlanStepID,
		)
		if err != nil {
			return err
		}
		if current != input.From {
			return typedError(
				ErrStaleRevision, "record plan step transition",
				errors.New("plan step state changed"),
			)
		}
		if !validPlanStepTransition(input.From, input.To) {
			return typedError(
				ErrConstraint, "record plan step transition",
				errors.New("plan step transition is not permitted"),
			)
		}
		var sequence uint64
		if err := transaction.sql.QueryRowContext(
			ctx,
			`SELECT COALESCE(MAX(sequence), 0) + 1
			 FROM agent_plan_step_transitions
			 WHERE run_id = ? AND plan_step_id = ?`,
			input.RunID,
			input.PlanStepID,
		).Scan(&sequence); err != nil {
			return classify("allocate plan step transition sequence", err)
		}
		_, micros := repositories.timestamp()
		if _, err := transaction.sql.ExecContext(ctx,
			`INSERT INTO agent_plan_step_transitions (
				id, task_id, run_id, plan_revision, plan_step_id,
				sequence, from_state, to_state, reason_redacted, model_request_id,
				validation_id, tool_request_id, idempotency_key,
				created_at_unix_micros
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			input.ID, input.TaskID, input.RunID, input.PlanRevision,
			input.PlanStepID, sequence, input.From, input.To, input.ReasonRedacted,
			nullableModelRequestID(input.ModelRequestID),
			nullableValidationID(input.ValidationID),
			nullableString(input.ToolRequestID),
			input.IdempotencyKey, micros,
		); err != nil {
			return repositoryWriteError("record plan step transition", err)
		}
		value, _, err = findPlanStepTransition(
			ctx, transaction.sql, input.RunID, input.IdempotencyKey,
		)
		return err
	})
	return value, err
}

func (repositories *Repositories) ListPlanStepStates(
	ctx context.Context,
	runID domain.RunID,
) ([]PlanStepStatus, error) {
	if runID.IsZero() {
		return nil, errors.New("run is required")
	}
	rows, err := repositories.database.sql.QueryContext(ctx,
		`SELECT binding.task_id, binding.run_id, binding.plan_revision,
		        step.step_id,
		        COALESCE((
		          SELECT transition.to_state
		          FROM agent_plan_step_transitions AS transition
		          WHERE transition.run_id = ?
		            AND transition.plan_step_id = step.step_id
		          ORDER BY transition.sequence DESC
		          LIMIT 1
		        ), 'pending'),
		        COALESCE((
		          SELECT transition.sequence
		          FROM agent_plan_step_transitions AS transition
		          WHERE transition.run_id = ?
		            AND transition.plan_step_id = step.step_id
		          ORDER BY transition.sequence DESC
		          LIMIT 1
		        ), 0)
		 FROM run_plan_bindings AS binding
		 JOIN agent_plan_steps AS step
		   ON step.task_id = binding.task_id
		  AND step.plan_revision = binding.plan_revision
		 WHERE binding.run_id = ?
		 ORDER BY step.ordinal`,
		runID, runID, runID,
	)
	if err != nil {
		return nil, classify("list plan step states", err)
	}
	defer rows.Close()
	var values []PlanStepStatus
	for rows.Next() {
		var value PlanStepStatus
		if err := rows.Scan(
			&value.TaskID,
			&value.RunID,
			&value.PlanRevision,
			&value.PlanStepID,
			&value.State,
			&value.Sequence,
		); err != nil {
			return nil, classify("scan plan step state", err)
		}
		if !value.State.valid() {
			return nil, typedError(
				ErrCorrupt, "list plan step states",
				errors.New("stored plan step state is invalid"),
			)
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (repositories *Repositories) RecordPlanValidationAttribution(
	ctx context.Context,
	input RecordPlanValidationAttribution,
) (PlanValidationAttribution, error) {
	if input.TaskID.IsZero() || input.RunID.IsZero() ||
		input.PlanRevision == 0 || input.ValidationID.IsZero() {
		return PlanValidationAttribution{},
			errors.New("validation plan attribution is incomplete")
	}
	if err := validateBounded("plan step ID", input.PlanStepID, 64); err != nil {
		return PlanValidationAttribution{}, err
	}
	if err := validateBounded(
		"validation attribution idempotency key", input.IdempotencyKey, 255,
	); err != nil {
		return PlanValidationAttribution{}, err
	}
	var value PlanValidationAttribution
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		existing, found, err := findPlanValidationAttribution(
			ctx, transaction.sql, input.RunID, input.IdempotencyKey,
		)
		if err != nil {
			return err
		}
		if found {
			if existing.TaskID != input.TaskID ||
				existing.RunID != input.RunID ||
				existing.PlanRevision != input.PlanRevision ||
				existing.ValidationID != input.ValidationID ||
				existing.PlanStepID != input.PlanStepID ||
				!sameStringPointer(
					existing.CommandExecutionID,
					input.CommandExecutionID,
				) ||
				!sameUint64Pointer(
					existing.RepairAttemptRevision,
					input.RepairAttemptRevision,
				) ||
				existing.IdempotencyKey != input.IdempotencyKey {
				return typedError(
					ErrConflict, "record plan validation attribution",
					errors.New("idempotency key belongs to another validation"),
				)
			}
			value = existing
			return nil
		}
		_, micros := repositories.timestamp()
		if _, err := transaction.sql.ExecContext(ctx,
			`INSERT INTO plan_validation_attributions (
				task_id, run_id, plan_revision, plan_step_id, validation_id,
				command_execution_id, repair_attempt_revision,
				idempotency_key, created_at_unix_micros
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			input.TaskID, input.RunID, input.PlanRevision, input.PlanStepID,
			input.ValidationID, nullableString(input.CommandExecutionID),
			nullableUint64(input.RepairAttemptRevision),
			input.IdempotencyKey, micros,
		); err != nil {
			return repositoryWriteError("record plan validation attribution", err)
		}
		value, _, err = findPlanValidationAttribution(
			ctx, transaction.sql, input.RunID, input.IdempotencyKey,
		)
		return err
	})
	return value, err
}

// RecordPlanValidationAttributions records every declared validation-to-step
// link in one transaction. A passing validation also advances each linked
// implemented step to validated with validation (rather than model)
// attribution. Any invalid link or state rolls the whole operation back.
func (repositories *Repositories) RecordPlanValidationAttributions(
	ctx context.Context,
	input RecordPlanValidationAttributions,
) ([]PlanValidationAttribution, error) {
	if input.TaskID.IsZero() || input.RunID.IsZero() ||
		input.PlanRevision == 0 || input.ValidationID.IsZero() {
		return nil, errors.New("validation plan attribution is incomplete")
	}
	if err := validateBounded(
		"validation attribution idempotency key",
		input.IdempotencyKey,
		255,
	); err != nil {
		return nil, err
	}
	if len(input.PlanStepIDs) == 0 || len(input.PlanStepIDs) > 64 {
		return nil, errors.New("validation plan-step links are empty or unbounded")
	}
	seen := make(map[string]struct{}, len(input.PlanStepIDs))
	for _, stepID := range input.PlanStepIDs {
		if err := validateBounded("plan step ID", stepID, 64); err != nil {
			return nil, err
		}
		if _, duplicate := seen[stepID]; duplicate {
			return nil, errors.New("validation plan-step link is duplicated")
		}
		seen[stepID] = struct{}{}
	}
	var values []PlanValidationAttribution
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		if err := recordValidationOperationTransaction(
			ctx, repositories, transaction, input,
		); err != nil {
			return err
		}
		if input.ValidationPassed {
			var state domain.ValidationState
			if err := transaction.sql.QueryRowContext(
				ctx,
				`SELECT state FROM validations
				 WHERE id = ? AND task_id = ? AND run_id = ?`,
				input.ValidationID,
				input.TaskID,
				input.RunID,
			).Scan(&state); err != nil {
				return classify("verify passed plan validation", err)
			}
			if state != domain.ValidationStatePassed {
				return typedError(
					ErrConstraint,
					"verify passed plan validation",
					errors.New("validation is not durably passed"),
				)
			}
		}
		values = make([]PlanValidationAttribution, 0, len(input.PlanStepIDs))
		for _, stepID := range input.PlanStepIDs {
			key := validationStepIdentity(input.IdempotencyKey, stepID)
			record := RecordPlanValidationAttribution{
				TaskID: input.TaskID, RunID: input.RunID,
				PlanRevision: input.PlanRevision, PlanStepID: stepID,
				ValidationID:          input.ValidationID,
				CommandExecutionID:    input.CommandExecutionID,
				RepairAttemptRevision: input.RepairAttemptRevision,
				IdempotencyKey:        key,
			}
			value, err := recordPlanValidationAttributionTransaction(
				ctx,
				repositories,
				transaction,
				record,
			)
			if err != nil {
				return err
			}
			values = append(values, value)
			if !input.ValidationPassed {
				continue
			}
			current, err := currentPlanStepState(
				ctx,
				transaction.sql,
				input.RunID,
				stepID,
			)
			if err != nil {
				return err
			}
			if current == PlanStepValidated {
				continue
			}
			if current != PlanStepImplemented {
				return typedError(
					ErrConstraint,
					"validate attributed plan step",
					fmt.Errorf(
						"plan step %q is %s, not implemented",
						stepID,
						current,
					),
				)
			}
			if err := recordValidationStepTransitionTransaction(
				ctx,
				repositories,
				transaction,
				input,
				stepID,
			); err != nil {
				return err
			}
		}
		return nil
	})
	return values, err
}

// ListPlanValidationAttributions returns every validation link for the exact
// immutable run plan in deterministic validation/step order.
func (repositories *Repositories) ListPlanValidationAttributions(
	ctx context.Context,
	runID domain.RunID,
) ([]PlanValidationAttribution, error) {
	if runID.IsZero() {
		return nil, errors.New("run is required")
	}
	rows, err := repositories.database.sql.QueryContext(
		ctx,
		`SELECT task_id, run_id, plan_revision, plan_step_id,
		        validation_id, command_execution_id,
		        repair_attempt_revision, idempotency_key,
		        created_at_unix_micros
		 FROM plan_validation_attributions
		 WHERE run_id = ?
		 ORDER BY validation_id, plan_step_id`,
		runID,
	)
	if err != nil {
		return nil, classify("list plan validation attributions", err)
	}
	defer rows.Close()
	var values []PlanValidationAttribution
	for rows.Next() {
		value, err := scanPlanValidationAttribution(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, classify("iterate plan validation attributions", err)
	}
	return values, nil
}

func recordPlanValidationAttributionTransaction(
	ctx context.Context,
	repositories *Repositories,
	transaction *Transaction,
	input RecordPlanValidationAttribution,
) (PlanValidationAttribution, error) {
	existing, found, err := findPlanValidationAttribution(
		ctx,
		transaction.sql,
		input.RunID,
		input.IdempotencyKey,
	)
	if err != nil {
		return PlanValidationAttribution{}, err
	}
	if found {
		if existing.TaskID != input.TaskID ||
			existing.RunID != input.RunID ||
			existing.PlanRevision != input.PlanRevision ||
			existing.ValidationID != input.ValidationID ||
			existing.PlanStepID != input.PlanStepID ||
			!sameStringPointer(
				existing.CommandExecutionID,
				input.CommandExecutionID,
			) ||
			!sameUint64Pointer(
				existing.RepairAttemptRevision,
				input.RepairAttemptRevision,
			) ||
			existing.IdempotencyKey != input.IdempotencyKey {
			return PlanValidationAttribution{}, typedError(
				ErrConflict,
				"record plan validation attribution",
				errors.New("idempotency key belongs to another validation"),
			)
		}
		return existing, nil
	}
	_, micros := repositories.timestamp()
	if _, err := transaction.sql.ExecContext(
		ctx,
		`INSERT INTO plan_validation_attributions (
			task_id, run_id, plan_revision, plan_step_id, validation_id,
			command_execution_id, repair_attempt_revision,
			idempotency_key, created_at_unix_micros
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		input.TaskID,
		input.RunID,
		input.PlanRevision,
		input.PlanStepID,
		input.ValidationID,
		nullableString(input.CommandExecutionID),
		nullableUint64(input.RepairAttemptRevision),
		input.IdempotencyKey,
		micros,
	); err != nil {
		return PlanValidationAttribution{},
			repositoryWriteError("record plan validation attribution", err)
	}
	value, _, err := findPlanValidationAttribution(
		ctx,
		transaction.sql,
		input.RunID,
		input.IdempotencyKey,
	)
	return value, err
}

func recordValidationStepTransitionTransaction(
	ctx context.Context,
	repositories *Repositories,
	transaction *Transaction,
	input RecordPlanValidationAttributions,
	stepID string,
) error {
	key := validationStepIdentity(input.IdempotencyKey+"/transition", stepID)
	existing, found, err := findPlanStepTransition(
		ctx,
		transaction.sql,
		input.RunID,
		key,
	)
	if err != nil {
		return err
	}
	validationID := input.ValidationID
	if found {
		if existing.TaskID != input.TaskID ||
			existing.RunID != input.RunID ||
			existing.PlanRevision != input.PlanRevision ||
			existing.PlanStepID != stepID ||
			existing.From != PlanStepImplemented ||
			existing.To != PlanStepValidated ||
			existing.ReasonRedacted != "required validation passed" ||
			!existing.ModelRequestID.IsZero() ||
			!sameValidationIDPointer(existing.ValidationID, &validationID) ||
			existing.ToolRequestID != nil ||
			existing.IdempotencyKey != key {
			return typedError(
				ErrConflict,
				"record validation plan step transition",
				errors.New("idempotency key belongs to another transition"),
			)
		}
		return nil
	}
	var sequence uint64
	if err := transaction.sql.QueryRowContext(
		ctx,
		`SELECT COALESCE(MAX(sequence), 0) + 1
		 FROM agent_plan_step_transitions
		 WHERE run_id = ? AND plan_step_id = ?`,
		input.RunID,
		stepID,
	).Scan(&sequence); err != nil {
		return classify("allocate validation step transition sequence", err)
	}
	_, micros := repositories.timestamp()
	id := validationStepIdentity(
		"agent-validation-step/"+input.ValidationID.String(),
		stepID,
	)
	if _, err := transaction.sql.ExecContext(
		ctx,
		`INSERT INTO agent_plan_step_transitions (
			id, task_id, run_id, plan_revision, plan_step_id, sequence,
			from_state, to_state, reason_redacted, model_request_id,
			validation_id, tool_request_id, idempotency_key,
			created_at_unix_micros
		) VALUES (?, ?, ?, ?, ?, ?, 'implemented', 'validated', ?, NULL, ?,
		          NULL, ?, ?)`,
		id,
		input.TaskID,
		input.RunID,
		input.PlanRevision,
		stepID,
		sequence,
		"required validation passed",
		input.ValidationID,
		key,
		micros,
	); err != nil {
		return repositoryWriteError("record validation plan step transition", err)
	}
	return nil
}

func validationStepIdentity(prefix, stepID string) string {
	digest := sha256.Sum256([]byte(prefix + "\x00" + stepID))
	return "agent-validation-" + hex.EncodeToString(digest[:])
}

func (repositories *Repositories) BeginRepairAttempt(
	ctx context.Context,
	input BeginRepairAttempt,
) (RepairAttempt, error) {
	if input.TaskID.IsZero() || input.RunID.IsZero() ||
		input.PlanRevision == 0 || input.Ordinal == 0 ||
		input.FailedValidationID.IsZero() ||
		input.PreRepairCheckpointID.IsZero() {
		return RepairAttempt{}, errors.New("repair attempt attribution is incomplete")
	}
	if err := validateBounded("repair reason", input.ReasonRedacted, 2048); err != nil {
		return RepairAttempt{}, err
	}
	if err := validateBounded(
		"repair idempotency key", input.IdempotencyKey, 255,
	); err != nil {
		return RepairAttempt{}, err
	}
	var value RepairAttempt
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		existing, found, err := findRepairAttempt(
			ctx, transaction.sql, input.RunID, input.IdempotencyKey,
		)
		if err != nil {
			return err
		}
		if found {
			if existing.TaskID != input.TaskID ||
				existing.RunID != input.RunID ||
				existing.PlanRevision != input.PlanRevision ||
				existing.Ordinal != input.Ordinal ||
				existing.FailedValidationID != input.FailedValidationID ||
				existing.PreRepairCheckpointID != input.PreRepairCheckpointID ||
				existing.ReasonRedacted != input.ReasonRedacted ||
				existing.IdempotencyKey != input.IdempotencyKey {
				return typedError(
					ErrConflict, "begin repair attempt",
					errors.New("idempotency key belongs to another repair"),
				)
			}
			value = existing
			return nil
		}
		var revision uint64
		if err := transaction.sql.QueryRowContext(ctx,
			`SELECT COALESCE(MAX(revision), 0) + 1
			 FROM repair_attempts WHERE run_id = ?`,
			input.RunID,
		).Scan(&revision); err != nil {
			return classify("allocate repair attempt revision", err)
		}
		_, micros := repositories.timestamp()
		if _, err := transaction.sql.ExecContext(ctx,
			`INSERT INTO repair_attempts (
				task_id, run_id, plan_revision, revision, ordinal,
				failed_validation_id, pre_repair_checkpoint_id,
				reason_redacted, idempotency_key, created_at_unix_micros
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			input.TaskID, input.RunID, input.PlanRevision, revision,
			input.Ordinal, input.FailedValidationID,
			input.PreRepairCheckpointID, input.ReasonRedacted,
			input.IdempotencyKey, micros,
		); err != nil {
			return repositoryWriteError("begin repair attempt", err)
		}
		value, _, err = findRepairAttempt(
			ctx, transaction.sql, input.RunID, input.IdempotencyKey,
		)
		return err
	})
	return value, err
}

func (repositories *Repositories) RecordRepairAttemptOutcome(
	ctx context.Context,
	input RecordRepairAttemptOutcome,
) (RepairAttemptOutcome, error) {
	if input.TaskID.IsZero() || input.RunID.IsZero() ||
		input.PlanRevision == 0 || input.RepairAttemptRevision == 0 ||
		!input.Outcome.valid() {
		return RepairAttemptOutcome{}, errors.New("repair outcome is incomplete")
	}
	if input.Outcome == RepairOutcomeValidationPassed &&
		input.PostRepairValidationID == nil {
		return RepairAttemptOutcome{},
			errors.New("passed repair requires post-repair validation")
	}
	if err := validateBounded(
		"unresolved repair summary", input.UnresolvedSummaryRedacted, 4096,
	); err != nil {
		return RepairAttemptOutcome{}, err
	}
	if err := validateBounded(
		"repair outcome idempotency key", input.IdempotencyKey, 255,
	); err != nil {
		return RepairAttemptOutcome{}, err
	}
	var value RepairAttemptOutcome
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		existing, found, err := findRepairAttemptOutcome(
			ctx, transaction.sql, input.RunID, input.RepairAttemptRevision,
		)
		if err != nil {
			return err
		}
		if found {
			if existing.TaskID != input.TaskID ||
				existing.RunID != input.RunID ||
				existing.PlanRevision != input.PlanRevision ||
				existing.RepairAttemptRevision != input.RepairAttemptRevision ||
				existing.Outcome != input.Outcome ||
				!sameValidationIDPointer(
					existing.PostRepairValidationID,
					input.PostRepairValidationID,
				) ||
				existing.UnresolvedSummaryRedacted !=
					input.UnresolvedSummaryRedacted ||
				existing.IdempotencyKey != input.IdempotencyKey {
				return typedError(
					ErrConflict, "record repair attempt outcome",
					errors.New("repair already has another outcome"),
				)
			}
			value = existing
			return nil
		}
		_, micros := repositories.timestamp()
		if _, err := transaction.sql.ExecContext(ctx,
			`INSERT INTO repair_attempt_outcomes (
				task_id, run_id, plan_revision, repair_attempt_revision,
				outcome, post_repair_validation_id,
				unresolved_summary_redacted, idempotency_key,
				created_at_unix_micros
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			input.TaskID, input.RunID, input.PlanRevision,
			input.RepairAttemptRevision, input.Outcome,
			nullableValidationID(input.PostRepairValidationID),
			input.UnresolvedSummaryRedacted, input.IdempotencyKey, micros,
		); err != nil {
			return repositoryWriteError("record repair attempt outcome", err)
		}
		value, _, err = findRepairAttemptOutcome(
			ctx, transaction.sql, input.RunID, input.RepairAttemptRevision,
		)
		return err
	})
	return value, err
}

// TransitionRunToValidation is one durable input.
type TransitionRunToValidation struct {
	EventID              domain.EventID
	TaskID               domain.TaskID
	RunID                domain.RunID
	ExpectedTaskRevision uint64
	ExpectedRunRevision  uint64
	IdempotencyKey       string
}

// TransitionedRunToValidation reports the exact revisions after the move.
type TransitionedRunToValidation struct {
	TaskRevision uint64
	RunRevision  uint64
}

// TransitionRunToValidation atomically moves a running task and its run into
// validation, the durable precondition RecordCompletionCandidate requires.
//
// Nothing else produces this state (AUDIT-020): the agent execution loop runs
// to completion entirely inside "running", and RecordCompletionCandidate
// refuses every candidate until both the task and its run have left it.
// Without an explicit caller for this, "running" was a state nothing could
// ever be recorded as leaving on the completion path, which is the same shape
// of gap RepairCompletionService.PrepareCompletion's own missing caller was.
//
// A second, narrower gap sits directly upstream and this function bridges it
// rather than assuming it away: StartPreparedTask inserts a run at state
// "starting" (execution_preflight_repository.go), and confirmed by symbol
// search, no production code anywhere transitions a run's own durable state
// to "running" afterward — domain.RunStateRunning is referenced only by the
// domain package's own declaration and by tests. The *task* reliably reaches
// "running" (StartPreparedTask sets it directly); the *run* does not. Rather
// than widen this ticket into fixing the general worker-acknowledgment gap —
// a separate, larger concern belonging to whatever should mark a run started,
// not to a completion call site — this function reads the run's actual
// current state and, if it is still "starting", applies the one further
// transition domain.RunState already declares valid (starting -> running)
// before applying running -> validating. Both steps go through
// domain.ValidateRunTransition; neither bypasses the declared state machine,
// and a run in any other state is refused exactly as before.
func (repositories *Repositories) TransitionRunToValidation(
	ctx context.Context,
	input TransitionRunToValidation,
) (TransitionedRunToValidation, error) {
	switch {
	case input.EventID.IsZero():
		return TransitionedRunToValidation{}, errors.New("event ID must not be empty")
	case input.TaskID.IsZero():
		return TransitionedRunToValidation{}, errors.New("task ID must not be empty")
	case input.RunID.IsZero():
		return TransitionedRunToValidation{}, errors.New("run ID must not be empty")
	}
	if err := validateBounded(
		"run validation transition idempotency key",
		input.IdempotencyKey, 255,
	); err != nil {
		return TransitionedRunToValidation{}, err
	}
	if err := domain.ValidateTaskTransition(domain.TaskTransition{
		From: domain.TaskStateRunning, To: domain.TaskStateValidating,
	}); err != nil {
		return TransitionedRunToValidation{}, err
	}
	if err := domain.ValidateRunTransition(
		domain.RunStateRunning, domain.RunStateValidating,
	); err != nil {
		return TransitionedRunToValidation{}, err
	}
	if err := domain.ValidateRunTransition(
		domain.RunStateStarting, domain.RunStateRunning,
	); err != nil {
		return TransitionedRunToValidation{}, err
	}
	var result TransitionedRunToValidation
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		existing, found, err := findTaskEventByIdempotency(
			ctx, transaction, input.TaskID, input.IdempotencyKey,
		)
		if err != nil {
			return err
		}
		if found {
			if existing.ID != input.EventID ||
				existing.EventType != "task.state-transition" ||
				!sameRunID(existing.RunID, &input.RunID) {
				return typedError(
					ErrConflict, "transition run to validation",
					errors.New("idempotency key belongs to a different transition"),
				)
			}
			var taskRevision, runRevision uint64
			if err := transaction.sql.QueryRowContext(ctx,
				`SELECT revision FROM tasks WHERE id = ?`, input.TaskID,
			).Scan(&taskRevision); err != nil {
				return classify("read idempotent task revision", err)
			}
			if err := transaction.sql.QueryRowContext(ctx,
				`SELECT revision FROM runs WHERE id = ?`, input.RunID,
			).Scan(&runRevision); err != nil {
				return classify("read idempotent run revision", err)
			}
			result = TransitionedRunToValidation{
				TaskRevision: taskRevision, RunRevision: runRevision,
			}
			return nil
		}
		now, micros := repositories.timestamp()
		payload, err := json.Marshal(struct {
			From domain.TaskState `json:"from"`
			To   domain.TaskState `json:"to"`
		}{From: domain.TaskStateRunning, To: domain.TaskStateValidating})
		if err != nil {
			return err
		}
		if err := transitionTaskWithinAgentRecord(
			ctx, transaction, input.TaskID, input.RunID,
			input.ExpectedTaskRevision,
			domain.TaskStateRunning, domain.TaskStateValidating,
			input.EventID, input.IdempotencyKey, string(payload), now, micros,
		); err != nil {
			return err
		}
		// The run's actual current state and revision, read fresh inside this
		// transaction rather than trusted from the caller: input.ExpectedRunRevision
		// is still honoured below as the optimistic-concurrency floor, but which
		// literal state string that revision is currently paired with is a fact
		// only this read establishes.
		var currentRunState domain.RunState
		var currentRunRevision uint64
		if err := transaction.sql.QueryRowContext(ctx,
			`SELECT state, revision FROM runs WHERE id = ? AND task_id = ?`,
			input.RunID, input.TaskID,
		).Scan(&currentRunState, &currentRunRevision); err != nil {
			return classify("read run for validation transition", err)
		}
		if currentRunRevision != input.ExpectedRunRevision {
			return typedError(
				ErrStaleRevision, "transition run to validation",
				errors.New("run revision changed"),
			)
		}
		if currentRunState == domain.RunStateStarting {
			bridgeResult, bridgeErr := transaction.sql.ExecContext(ctx,
				`UPDATE runs
				 SET state = ?, revision = revision + 1, updated_at_unix_micros = ?
				 WHERE id = ? AND task_id = ? AND state = ? AND revision = ?`,
				domain.RunStateRunning, micros, input.RunID, input.TaskID,
				domain.RunStateStarting, currentRunRevision,
			)
			if bridgeErr != nil {
				return repositoryWriteError(
					"bridge run to running before validation", bridgeErr,
				)
			}
			if affected, _ := bridgeResult.RowsAffected(); affected != 1 {
				return typedError(
					ErrStaleRevision, "bridge run to running before validation",
					errors.New("run state or revision changed"),
				)
			}
			currentRunState = domain.RunStateRunning
			currentRunRevision++
		}
		if currentRunState != domain.RunStateRunning {
			return typedError(
				ErrConstraint, "transition run to validation",
				fmt.Errorf(
					"run is %q, not running or startable into it", currentRunState,
				),
			)
		}
		runResult, err := transaction.sql.ExecContext(ctx,
			`UPDATE runs
			 SET state = ?, revision = revision + 1, updated_at_unix_micros = ?
			 WHERE id = ? AND task_id = ? AND state = ? AND revision = ?`,
			domain.RunStateValidating, micros, input.RunID, input.TaskID,
			domain.RunStateRunning, currentRunRevision,
		)
		if err != nil {
			return repositoryWriteError("transition run to validation", err)
		}
		if affected, _ := runResult.RowsAffected(); affected != 1 {
			return typedError(
				ErrStaleRevision, "transition run to validation",
				errors.New("run state or revision changed"),
			)
		}
		result = TransitionedRunToValidation{
			TaskRevision: input.ExpectedTaskRevision + 1,
			RunRevision:  currentRunRevision + 1,
		}
		return nil
	})
	return result, err
}

// RecordCompletionCandidate atomically creates the candidate and its task
// transition event. The run remains validating until the user's decision.
func (repositories *Repositories) RecordCompletionCandidate(
	ctx context.Context,
	input RecordCompletionCandidate,
) (CompletionCandidate, error) {
	if err := validateCompletionCandidateInput(input); err != nil {
		return CompletionCandidate{}, err
	}
	var value CompletionCandidate
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		existing, found, err := findCompletionCandidate(
			ctx, transaction.sql, input.RunID, input.IdempotencyKey,
		)
		if err != nil {
			return err
		}
		if found {
			if existing.TaskID != input.TaskID ||
				existing.RunID != input.RunID ||
				existing.PlanRevision != input.PlanRevision ||
				existing.ExpectedTaskRevision != input.ExpectedTaskRevision ||
				existing.ExpectedRunRevision != input.ExpectedRunRevision ||
				existing.EventID != input.EventID ||
				existing.EventIdempotencyKey != input.EventIdempotencyKey ||
				existing.RepositoryStatusJSON != input.RepositoryStatusJSON ||
				existing.DiffSummaryJSON != input.DiffSummaryJSON ||
				existing.DiffSHA256 != input.DiffSHA256 ||
				existing.ValidationSummaryJSON != input.ValidationSummaryJSON ||
				existing.BudgetSummaryJSON != input.BudgetSummaryJSON ||
				existing.AssumptionsJSON != input.AssumptionsJSON ||
				existing.LimitationsJSON != input.LimitationsJSON ||
				existing.ImplementationComplete != input.ImplementationComplete ||
				existing.ValidationComplete != input.ValidationComplete ||
				existing.IdempotencyKey != input.IdempotencyKey {
				return typedError(
					ErrConflict, "record completion candidate",
					errors.New("idempotency key belongs to another completion"),
				)
			}
			value = existing
			return nil
		}
		if err := requireTaskAndRunStates(
			ctx, transaction.sql, input.TaskID, input.RunID,
			input.ExpectedTaskRevision, domain.TaskStateValidating,
			input.ExpectedRunRevision, domain.RunStateValidating,
		); err != nil {
			return err
		}
		var revision uint64
		if err := transaction.sql.QueryRowContext(ctx,
			`SELECT COALESCE(MAX(revision), 0) + 1
			 FROM completion_candidates WHERE run_id = ?`,
			input.RunID,
		).Scan(&revision); err != nil {
			return classify("allocate completion revision", err)
		}
		now, micros := repositories.timestamp()
		if _, err := transaction.sql.ExecContext(ctx,
			`INSERT INTO completion_candidates (
				task_id, run_id, plan_revision, revision,
				expected_task_revision, expected_run_revision,
				event_id, event_idempotency_key,
				repository_status_json, diff_summary_json, diff_sha256,
				validation_summary_json, budget_summary_json,
				assumptions_json, limitations_json,
				implementation_complete, validation_complete,
				idempotency_key, created_at_unix_micros
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			input.TaskID, input.RunID, input.PlanRevision, revision,
			input.ExpectedTaskRevision, input.ExpectedRunRevision,
			input.EventID, input.EventIdempotencyKey,
			input.RepositoryStatusJSON, input.DiffSummaryJSON,
			input.DiffSHA256, input.ValidationSummaryJSON,
			input.BudgetSummaryJSON, input.AssumptionsJSON,
			input.LimitationsJSON, boolInteger(input.ImplementationComplete),
			boolInteger(input.ValidationComplete), input.IdempotencyKey,
			micros,
		); err != nil {
			return repositoryWriteError("record completion candidate", err)
		}
		payload, _ := json.Marshal(map[string]any{
			"from":                domain.TaskStateValidating,
			"to":                  domain.TaskStateAwaitingReview,
			"plan_revision":       input.PlanRevision,
			"completion_revision": revision,
		})
		if err := transitionTaskWithinAgentRecord(
			ctx, transaction, input.TaskID, input.RunID,
			input.ExpectedTaskRevision, domain.TaskStateValidating,
			domain.TaskStateAwaitingReview, input.EventID,
			input.EventIdempotencyKey, string(payload), now, micros,
		); err != nil {
			return err
		}
		value, _, err = findCompletionCandidate(
			ctx, transaction.sql, input.RunID, input.IdempotencyKey,
		)
		return err
	})
	return value, err
}

func (repositories *Repositories) RecordTaskReviewDecision(
	ctx context.Context,
	input RecordTaskReviewDecision,
) (TaskReviewDecision, error) {
	if err := validateTaskReviewDecisionInput(input); err != nil {
		return TaskReviewDecision{}, err
	}
	var value TaskReviewDecision
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		existing, found, err := findTaskReviewDecision(
			ctx, transaction.sql, input.TaskID, input.IdempotencyKey,
		)
		if err != nil {
			return err
		}
		if found {
			if existing.TaskID != input.TaskID ||
				existing.RunID != input.RunID ||
				existing.PlanRevision != input.PlanRevision ||
				existing.ExpectedTaskRevision != input.ExpectedTaskRevision ||
				existing.ExpectedRunRevision != input.ExpectedRunRevision ||
				existing.EventID != input.EventID ||
				existing.EventIdempotencyKey != input.EventIdempotencyKey ||
				existing.Decision != input.Decision ||
				existing.CompletionRevision != input.CompletionRevision ||
				existing.ActorReference != input.ActorReference ||
				existing.AuthorityReference != input.AuthorityReference ||
				existing.ReasonRedacted != input.ReasonRedacted ||
				!sameMessageIDPointer(existing.MessageID, input.MessageID) ||
				existing.IdempotencyKey != input.IdempotencyKey {
				return typedError(
					ErrConflict, "record task review decision",
					errors.New("idempotency key belongs to another decision"),
				)
			}
			value = existing
			return nil
		}
		if err := requireTaskAndRunStates(
			ctx, transaction.sql, input.TaskID, input.RunID,
			input.ExpectedTaskRevision, domain.TaskStateAwaitingReview,
			input.ExpectedRunRevision, domain.RunStateValidating,
		); err != nil {
			return err
		}
		taskTo, runTo := reviewDecisionStates(input.Decision)
		if err := domain.ValidateTaskTransition(domain.TaskTransition{
			From: domain.TaskStateAwaitingReview, To: taskTo,
		}); err != nil {
			return err
		}
		if err := domain.ValidateRunTransition(
			domain.RunStateValidating,
			runTo,
		); err != nil {
			return err
		}
		var revision uint64
		if err := transaction.sql.QueryRowContext(ctx,
			`SELECT COALESCE(MAX(revision), 0) + 1
			 FROM task_review_decisions WHERE task_id = ?`,
			input.TaskID,
		).Scan(&revision); err != nil {
			return classify("allocate review decision revision", err)
		}
		now, micros := repositories.timestamp()
		if _, err := transaction.sql.ExecContext(ctx,
			`INSERT INTO task_review_decisions (
				task_id, run_id, plan_revision, completion_revision,
				revision, expected_task_revision, expected_run_revision,
				event_id, event_idempotency_key,
				decision, actor_reference, authority_reference,
				reason_redacted, message_id, idempotency_key,
				created_at_unix_micros
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			input.TaskID, input.RunID, input.PlanRevision,
			input.CompletionRevision, revision,
			input.ExpectedTaskRevision, input.ExpectedRunRevision,
			input.EventID, input.EventIdempotencyKey, input.Decision,
			input.ActorReference, input.AuthorityReference,
			input.ReasonRedacted, nullableMessageID(input.MessageID),
			input.IdempotencyKey, micros,
		); err != nil {
			return repositoryWriteError("record task review decision", err)
		}
		result, err := transaction.sql.ExecContext(ctx,
			`UPDATE runs
			 SET state = ?, revision = revision + 1,
			     updated_at_unix_micros = ?
			 WHERE id = ? AND task_id = ? AND state = 'validating'
			   AND revision = ?`,
			runTo, micros, input.RunID, input.TaskID,
			input.ExpectedRunRevision,
		)
		if err != nil {
			return repositoryWriteError("apply review run decision", err)
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return typedError(
				ErrStaleRevision, "apply review run decision",
				errors.New("run state or revision changed"),
			)
		}
		payload, _ := json.Marshal(map[string]any{
			"from":                domain.TaskStateAwaitingReview,
			"to":                  taskTo,
			"plan_revision":       input.PlanRevision,
			"completion_revision": input.CompletionRevision,
			"decision":            input.Decision,
		})
		if err := transitionTaskWithinAgentRecord(
			ctx, transaction, input.TaskID, input.RunID,
			input.ExpectedTaskRevision, domain.TaskStateAwaitingReview,
			taskTo, input.EventID, input.EventIdempotencyKey,
			string(payload), now, micros,
		); err != nil {
			return err
		}
		value, _, err = findTaskReviewDecision(
			ctx, transaction.sql, input.TaskID, input.IdempotencyKey,
		)
		return err
	})
	return value, err
}

func validateAgentToolRequest(input RecordAgentToolRequest) error {
	switch {
	case input.TaskID.IsZero(), input.RunID.IsZero(),
		input.PlanRevision == 0, input.ModelRequestID.IsZero():
		return errors.New("agent tool request attribution is incomplete")
	case input.ToolSchemaVersion == 0:
		return errors.New("tool schema version is required")
	case len(input.ArgumentsSHA256) != 64 || !lowerHex(input.ArgumentsSHA256):
		return errors.New("tool arguments digest is invalid")
	case !json.Valid([]byte(input.ArgumentsRedactedJSON)):
		return errors.New("tool arguments must be valid redacted JSON")
	}
	for label, value := range map[string]string{
		"tool request ID": input.ID, "plan step ID": input.PlanStepID,
		"tool name": input.ToolName, "tool request idempotency key": input.IdempotencyKey,
	} {
		if err := validateBounded(label, value, 255); err != nil {
			return err
		}
	}
	return nil
}

func validateAgentToolResult(input RecordAgentToolResult) error {
	switch {
	case !input.State.valid():
		return errors.New("tool result state is invalid")
	case len(input.ResultSHA256) != 64 || !lowerHex(input.ResultSHA256):
		return errors.New("tool result digest is invalid")
	case !json.Valid([]byte(input.ResultRedactedJSON)):
		return errors.New("tool result must be valid redacted JSON")
	case hashJSON(input.ResultRedactedJSON) != input.ResultSHA256:
		return errors.New("tool result digest is inconsistent")
	}
	for label, value := range map[string]string{
		"tool result ID": input.ID, "tool request ID": input.ToolRequestID,
		"tool result idempotency key": input.IdempotencyKey,
	} {
		if err := validateBounded(label, value, 255); err != nil {
			return err
		}
	}
	return nil
}

func validatePlanStepTransition(input RecordPlanStepTransition) error {
	switch {
	case input.TaskID.IsZero(), input.RunID.IsZero(), input.PlanRevision == 0:
		return errors.New("plan step transition attribution is incomplete")
	case input.ModelRequestID.IsZero() == (input.ValidationID == nil):
		return errors.New(
			"plan step transition requires exactly one model or validation attribution",
		)
	case input.ValidationID != nil &&
		(input.From != PlanStepImplemented || input.To != PlanStepValidated):
		return errors.New(
			"validation attribution may only validate an implemented plan step",
		)
	case input.ModelRequestID.IsZero() && input.ToolRequestID != nil:
		return errors.New("validation-attributed transition cannot cite a model tool")
	case !input.From.valid(), !input.To.valid():
		return errors.New("plan step state is invalid")
	}
	for label, value := range map[string]string{
		"plan step transition ID": input.ID, "plan step ID": input.PlanStepID,
		"plan step transition reason":          input.ReasonRedacted,
		"plan step transition idempotency key": input.IdempotencyKey,
	} {
		if err := validateBounded(label, value, 2048); err != nil {
			return err
		}
	}
	return nil
}

func validPlanStepTransition(from, to PlanStepState) bool {
	switch from {
	case PlanStepPending:
		return to == PlanStepInProgress || to == PlanStepSkipped
	case PlanStepInProgress:
		return to == PlanStepImplemented || to == PlanStepFailed
	case PlanStepImplemented:
		return to == PlanStepValidated || to == PlanStepFailed ||
			to == PlanStepInProgress
	case PlanStepFailed:
		return to == PlanStepInProgress || to == PlanStepSkipped
	default:
		return false
	}
}

func validateCompletionCandidateInput(input RecordCompletionCandidate) error {
	switch {
	case input.TaskID.IsZero(), input.RunID.IsZero(),
		input.PlanRevision == 0, input.EventID.IsZero():
		return errors.New("completion candidate attribution is incomplete")
	case len(input.DiffSHA256) != 64 || !lowerHex(input.DiffSHA256):
		return errors.New("completion diff digest is invalid")
	case !input.ImplementationComplete:
		return errors.New("completion candidate requires implementation completion")
	}
	for label, value := range map[string]string{
		"repository status":  input.RepositoryStatusJSON,
		"diff summary":       input.DiffSummaryJSON,
		"validation summary": input.ValidationSummaryJSON,
		"budget summary":     input.BudgetSummaryJSON,
		"assumptions":        input.AssumptionsJSON,
		"limitations":        input.LimitationsJSON,
	} {
		if !json.Valid([]byte(value)) {
			return fmt.Errorf("%s must be valid JSON", label)
		}
	}
	for label, value := range map[string]string{
		"completion idempotency key":       input.IdempotencyKey,
		"completion event idempotency key": input.EventIdempotencyKey,
	} {
		if err := validateBounded(label, value, 255); err != nil {
			return err
		}
	}
	return nil
}

func validateTaskReviewDecisionInput(input RecordTaskReviewDecision) error {
	switch {
	case input.TaskID.IsZero(), input.RunID.IsZero(),
		input.PlanRevision == 0, input.CompletionRevision == 0,
		input.EventID.IsZero():
		return errors.New("review decision attribution is incomplete")
	case !input.Decision.valid():
		return errors.New("review decision is invalid")
	}
	for label, value := range map[string]string{
		"review actor":                 input.ActorReference,
		"review authority":             input.AuthorityReference,
		"review reason":                input.ReasonRedacted,
		"review idempotency key":       input.IdempotencyKey,
		"review event idempotency key": input.EventIdempotencyKey,
	} {
		if strings.TrimSpace(value) != value {
			return fmt.Errorf("%s must be trimmed", label)
		}
		if err := validateBounded(label, value, 2048); err != nil {
			return err
		}
	}
	return nil
}

func reviewDecisionStates(
	decision TaskReviewDecisionKind,
) (domain.TaskState, domain.RunState) {
	switch decision {
	case TaskReviewAccept:
		return domain.TaskStateCompleted, domain.RunStateCompleted
	case TaskReviewRequestRepair:
		return domain.TaskStateRunning, domain.RunStateRunning
	case TaskReviewRollback:
		return domain.TaskStateRolledBack, domain.RunStateCompleted
	default:
		return domain.TaskStateCancelled, domain.RunStateCancelled
	}
}

func requireTaskAndRunStates(
	ctx context.Context,
	queries queryRower,
	taskID domain.TaskID,
	runID domain.RunID,
	taskRevision uint64,
	taskState domain.TaskState,
	runRevision uint64,
	runState domain.RunState,
) error {
	var actualTaskRevision, actualRunRevision uint64
	var actualTaskState domain.TaskState
	var actualRunState domain.RunState
	if err := queries.QueryRowContext(ctx,
		`SELECT task.state, task.revision, run.state, run.revision
		 FROM tasks AS task
		 JOIN runs AS run ON run.task_id = task.id
		 WHERE task.id = ? AND run.id = ?`,
		taskID, runID,
	).Scan(
		&actualTaskState, &actualTaskRevision,
		&actualRunState, &actualRunRevision,
	); err != nil {
		return classify("load task and run lifecycle", err)
	}
	if actualTaskState != taskState || actualTaskRevision != taskRevision ||
		actualRunState != runState || actualRunRevision != runRevision {
		return typedError(
			ErrStaleRevision, "verify task and run lifecycle",
			errors.New("task or run state/revision changed"),
		)
	}
	return nil
}

func transitionTaskWithinAgentRecord(
	ctx context.Context,
	transaction *Transaction,
	taskID domain.TaskID,
	runID domain.RunID,
	expectedRevision uint64,
	from, to domain.TaskState,
	eventID domain.EventID,
	eventKey string,
	payload string,
	now time.Time,
	micros int64,
) error {
	if err := domain.ValidateTaskTransition(domain.TaskTransition{
		From: from, To: to,
	}); err != nil {
		return err
	}
	result, err := transaction.sql.ExecContext(ctx,
		`UPDATE tasks
		 SET state = ?, revision = revision + 1,
		     updated_at_unix_micros = ?
		 WHERE id = ? AND state = ? AND revision = ?`,
		to, micros, taskID, from, expectedRevision,
	)
	if err != nil {
		return repositoryWriteError("apply agent task transition", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return typedError(
			ErrStaleRevision, "apply agent task transition",
			errors.New("task state or revision changed"),
		)
	}
	_, err = appendTaskEventTransaction(ctx, transaction, AppendTaskEvent{
		ID: eventID, TaskID: taskID, RunID: &runID,
		EventType: "task.state-transition", PayloadJSON: payload,
		IdempotencyKey: eventKey,
	}, now, micros)
	return err
}

func currentPlanStepState(
	ctx context.Context,
	queries queryRower,
	runID domain.RunID,
	stepID string,
) (PlanStepState, error) {
	var exists int
	if err := queries.QueryRowContext(ctx,
		`SELECT count(*)
		 FROM run_plan_bindings AS binding
		 JOIN agent_plan_steps AS step
		   ON step.task_id = binding.task_id
		  AND step.plan_revision = binding.plan_revision
		 WHERE binding.run_id = ? AND step.step_id = ?`,
		runID, stepID,
	).Scan(&exists); err != nil {
		return "", classify("verify run plan step", err)
	}
	if exists != 1 {
		return "", typedError(
			ErrConstraint, "load plan step state",
			errors.New("step does not belong to run plan"),
		)
	}
	var state PlanStepState
	err := queries.QueryRowContext(ctx,
		`SELECT to_state
		 FROM agent_plan_step_transitions
		 WHERE run_id = ? AND plan_step_id = ?
		 ORDER BY sequence DESC LIMIT 1`,
		runID, stepID,
	).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return PlanStepPending, nil
	}
	if err != nil {
		return "", classify("load plan step state", err)
	}
	return state, nil
}

func findAgentToolRequest(
	ctx context.Context, queries queryRower,
	runID domain.RunID, key string,
) (AgentToolRequest, bool, error) {
	var value AgentToolRequest
	var permission sql.NullString
	var micros int64
	err := queries.QueryRowContext(ctx,
		`SELECT id, task_id, run_id, plan_revision, plan_step_id,
		        model_request_id, tool_name, tool_schema_version,
		        arguments_redacted_json, arguments_sha256,
		        permission_decision_id, idempotency_key,
		        created_at_unix_micros
		 FROM agent_tool_requests
		 WHERE run_id = ? AND idempotency_key = ?`,
		runID, key,
	).Scan(
		&value.ID, &value.TaskID, &value.RunID, &value.PlanRevision,
		&value.PlanStepID, &value.ModelRequestID, &value.ToolName,
		&value.ToolSchemaVersion, &value.ArgumentsRedactedJSON,
		&value.ArgumentsSHA256, &permission, &value.IdempotencyKey, &micros,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return AgentToolRequest{}, false, nil
	}
	if err != nil {
		return AgentToolRequest{}, false, classify("read agent tool request", err)
	}
	if permission.Valid {
		value.PermissionDecisionID = &permission.String
	}
	value.CreatedAt = repositoryTime(micros)
	return value, true, nil
}

func findAgentToolResult(
	ctx context.Context, queries queryRower, requestID string,
) (AgentToolResult, bool, error) {
	var value AgentToolResult
	var command sql.NullString
	var micros int64
	err := queries.QueryRowContext(ctx,
		`SELECT id, tool_request_id, state, result_redacted_json,
		        result_sha256, command_execution_id, idempotency_key,
		        created_at_unix_micros
		 FROM agent_tool_results WHERE tool_request_id = ?`,
		requestID,
	).Scan(
		&value.ID, &value.ToolRequestID, &value.State,
		&value.ResultRedactedJSON, &value.ResultSHA256, &command,
		&value.IdempotencyKey, &micros,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return AgentToolResult{}, false, nil
	}
	if err != nil {
		return AgentToolResult{}, false, classify("read agent tool result", err)
	}
	if command.Valid {
		value.CommandExecutionID = &command.String
	}
	value.CreatedAt = repositoryTime(micros)
	return value, true, nil
}

func findPlanStepTransition(
	ctx context.Context, queries queryRower,
	runID domain.RunID, key string,
) (PlanStepTransition, bool, error) {
	var value PlanStepTransition
	var model sql.NullString
	var validation sql.NullString
	var tool sql.NullString
	var micros int64
	err := queries.QueryRowContext(ctx,
		`SELECT id, task_id, run_id, plan_revision, plan_step_id,
		        sequence, from_state, to_state, reason_redacted,
		        model_request_id, validation_id, tool_request_id,
		        idempotency_key, created_at_unix_micros
		 FROM agent_plan_step_transitions
		 WHERE run_id = ? AND idempotency_key = ?`,
		runID, key,
	).Scan(
		&value.ID, &value.TaskID, &value.RunID, &value.PlanRevision,
		&value.PlanStepID, &value.Sequence, &value.From, &value.To,
		&value.ReasonRedacted,
		&model, &validation, &tool, &value.IdempotencyKey, &micros,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return PlanStepTransition{}, false, nil
	}
	if err != nil {
		return PlanStepTransition{}, false, classify("read plan step transition", err)
	}
	if tool.Valid {
		value.ToolRequestID = &tool.String
	}
	if model.Valid {
		parsed, err := domain.ParseModelRequestID(model.String)
		if err != nil {
			return PlanStepTransition{}, false,
				typedError(ErrCorrupt, "parse step model attribution", err)
		}
		value.ModelRequestID = parsed
	}
	if validation.Valid {
		parsed, err := domain.ParseValidationID(validation.String)
		if err != nil {
			return PlanStepTransition{}, false,
				typedError(ErrCorrupt, "parse step validation attribution", err)
		}
		value.ValidationID = &parsed
	}
	value.CreatedAt = repositoryTime(micros)
	return value, true, nil
}

func findPlanValidationAttribution(
	ctx context.Context, queries queryRower,
	runID domain.RunID, key string,
) (PlanValidationAttribution, bool, error) {
	value, err := scanPlanValidationAttribution(queries.QueryRowContext(ctx,
		`SELECT task_id, run_id, plan_revision, plan_step_id,
		        validation_id, command_execution_id,
		        repair_attempt_revision, idempotency_key,
		        created_at_unix_micros
		 FROM plan_validation_attributions
		 WHERE run_id = ? AND idempotency_key = ?`,
		runID, key,
	))
	if errors.Is(err, ErrNotFound) {
		return PlanValidationAttribution{}, false, nil
	}
	return value, err == nil, err
}

func scanPlanValidationAttribution(
	row rowScanner,
) (PlanValidationAttribution, error) {
	var value PlanValidationAttribution
	var command sql.NullString
	var repair sql.NullInt64
	var micros int64
	if err := row.Scan(
		&value.TaskID, &value.RunID, &value.PlanRevision,
		&value.PlanStepID, &value.ValidationID, &command, &repair,
		&value.IdempotencyKey, &micros,
	); err != nil {
		return PlanValidationAttribution{},
			typedError(ErrNotFound, "read plan validation attribution", err)
	}
	if command.Valid {
		value.CommandExecutionID = &command.String
	}
	if repair.Valid {
		revision := uint64(repair.Int64)
		value.RepairAttemptRevision = &revision
	}
	value.CreatedAt = repositoryTime(micros)
	return value, nil
}

func findRepairAttempt(
	ctx context.Context, queries queryRower,
	runID domain.RunID, key string,
) (RepairAttempt, bool, error) {
	var value RepairAttempt
	var micros int64
	err := queries.QueryRowContext(ctx,
		`SELECT task_id, run_id, plan_revision, revision, ordinal,
		        failed_validation_id, pre_repair_checkpoint_id,
		        reason_redacted, idempotency_key, created_at_unix_micros
		 FROM repair_attempts
		 WHERE run_id = ? AND idempotency_key = ?`,
		runID, key,
	).Scan(
		&value.TaskID, &value.RunID, &value.PlanRevision, &value.Revision,
		&value.Ordinal, &value.FailedValidationID,
		&value.PreRepairCheckpointID, &value.ReasonRedacted,
		&value.IdempotencyKey, &micros,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return RepairAttempt{}, false, nil
	}
	if err != nil {
		return RepairAttempt{}, false, classify("read repair attempt", err)
	}
	value.CreatedAt = repositoryTime(micros)
	return value, true, nil
}

func findRepairAttemptOutcome(
	ctx context.Context, queries queryRower,
	runID domain.RunID, revision uint64,
) (RepairAttemptOutcome, bool, error) {
	var value RepairAttemptOutcome
	var validation sql.NullString
	var micros int64
	err := queries.QueryRowContext(ctx,
		`SELECT task_id, run_id, plan_revision, repair_attempt_revision,
		        outcome, post_repair_validation_id,
		        unresolved_summary_redacted, idempotency_key,
		        created_at_unix_micros
		 FROM repair_attempt_outcomes
		 WHERE run_id = ? AND repair_attempt_revision = ?`,
		runID, revision,
	).Scan(
		&value.TaskID, &value.RunID, &value.PlanRevision,
		&value.RepairAttemptRevision, &value.Outcome, &validation,
		&value.UnresolvedSummaryRedacted, &value.IdempotencyKey, &micros,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return RepairAttemptOutcome{}, false, nil
	}
	if err != nil {
		return RepairAttemptOutcome{}, false,
			classify("read repair attempt outcome", err)
	}
	if validation.Valid {
		parsed, err := domain.ParseValidationID(validation.String)
		if err != nil {
			return RepairAttemptOutcome{}, false,
				typedError(ErrCorrupt, "parse repair validation", err)
		}
		value.PostRepairValidationID = &parsed
	}
	value.CreatedAt = repositoryTime(micros)
	return value, true, nil
}

func findCompletionCandidate(
	ctx context.Context, queries queryRower,
	runID domain.RunID, key string,
) (CompletionCandidate, bool, error) {
	var value CompletionCandidate
	var implemented, validated int
	var micros int64
	err := queries.QueryRowContext(ctx,
		`SELECT task_id, run_id, plan_revision, revision,
		        expected_task_revision, expected_run_revision,
		        event_id, event_idempotency_key,
		        repository_status_json, diff_summary_json, diff_sha256,
		        validation_summary_json, budget_summary_json,
		        assumptions_json, limitations_json,
		        implementation_complete, validation_complete,
		        idempotency_key, created_at_unix_micros
		 FROM completion_candidates
		 WHERE run_id = ? AND idempotency_key = ?`,
		runID, key,
	).Scan(
		&value.TaskID, &value.RunID, &value.PlanRevision, &value.Revision,
		&value.ExpectedTaskRevision, &value.ExpectedRunRevision,
		&value.EventID, &value.EventIdempotencyKey,
		&value.RepositoryStatusJSON, &value.DiffSummaryJSON, &value.DiffSHA256,
		&value.ValidationSummaryJSON, &value.BudgetSummaryJSON,
		&value.AssumptionsJSON, &value.LimitationsJSON,
		&implemented, &validated, &value.IdempotencyKey, &micros,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return CompletionCandidate{}, false, nil
	}
	if err != nil {
		return CompletionCandidate{}, false, classify("read completion candidate", err)
	}
	value.ImplementationComplete = implemented != 0
	value.ValidationComplete = validated != 0
	value.CreatedAt = repositoryTime(micros)
	return value, true, nil
}

func findTaskReviewDecision(
	ctx context.Context, queries queryRower,
	taskID domain.TaskID, key string,
) (TaskReviewDecision, bool, error) {
	var value TaskReviewDecision
	var message sql.NullString
	var micros int64
	err := queries.QueryRowContext(ctx,
		`SELECT task_id, run_id, plan_revision, completion_revision,
		        revision, expected_task_revision, expected_run_revision,
		        event_id, event_idempotency_key,
		        decision, actor_reference, authority_reference,
		        reason_redacted, message_id, idempotency_key,
		        created_at_unix_micros
		 FROM task_review_decisions
		 WHERE task_id = ? AND idempotency_key = ?`,
		taskID, key,
	).Scan(
		&value.TaskID, &value.RunID, &value.PlanRevision,
		&value.CompletionRevision, &value.Revision,
		&value.ExpectedTaskRevision, &value.ExpectedRunRevision,
		&value.EventID, &value.EventIdempotencyKey, &value.Decision,
		&value.ActorReference, &value.AuthorityReference,
		&value.ReasonRedacted, &message, &value.IdempotencyKey, &micros,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return TaskReviewDecision{}, false, nil
	}
	if err != nil {
		return TaskReviewDecision{}, false, classify("read task review decision", err)
	}
	if message.Valid {
		parsed, err := domain.ParseMessageID(message.String)
		if err != nil {
			return TaskReviewDecision{}, false,
				typedError(ErrCorrupt, "parse review message", err)
		}
		value.MessageID = &parsed
	}
	value.CreatedAt = repositoryTime(micros)
	return value, true, nil
}

func nullableValidationID(value *domain.ValidationID) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableModelRequestID(value domain.ModelRequestID) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func sameStringPointer(left, right *string) bool {
	return left == nil && right == nil ||
		left != nil && right != nil && *left == *right
}

func sameValidationIDPointer(
	left, right *domain.ValidationID,
) bool {
	return left == nil && right == nil ||
		left != nil && right != nil && *left == *right
}
