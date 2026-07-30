package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/policy"
)

// PreApprovalBudgetAdjustment is one immutable exact before-approval revision.
type PreApprovalBudgetAdjustment struct {
	BudgetID              domain.BudgetID
	TaskID                domain.TaskID
	Revision              uint64
	PreviousLimitRevision uint64
	AdjustedLimitRevision uint64
	Previous              domain.TaskBudget
	Adjusted              domain.TaskBudget
	Actor                 string
	AuthorityReference    string
	Reason                string
	IdempotencyKey        string
	CreatedAt             time.Time
}

// AdjustPreApprovalBudget declares a complete exact replacement before plan
// approval. It cannot be used as a post-approval hard-limit raise.
type AdjustPreApprovalBudget struct {
	BudgetID               domain.BudgetID
	ExpectedBudgetRevision uint64
	ExpectedLimitRevision  uint64
	Requested              domain.TaskBudget
	Actor                  string
	AuthorityReference     string
	Reason                 string
	IdempotencyKey         string
}

// AdjustBudgetBeforeApproval durably records and applies an exact budget
// replacement while the task remains before its approval boundary.
func (repositories *Repositories) AdjustBudgetBeforeApproval(
	ctx context.Context,
	input AdjustPreApprovalBudget,
) (PreApprovalBudgetAdjustment, BudgetSnapshot, error) {
	if input.BudgetID.IsZero() || input.Requested.ID.IsZero() {
		return PreApprovalBudgetAdjustment{}, BudgetSnapshot{},
			errors.New("budget identity is required")
	}
	if input.BudgetID != input.Requested.ID {
		return PreApprovalBudgetAdjustment{}, BudgetSnapshot{},
			errors.New("requested budget identity differs")
	}
	if err := validateBounded("budget adjustment idempotency key", input.IdempotencyKey, 255); err != nil {
		return PreApprovalBudgetAdjustment{}, BudgetSnapshot{}, err
	}
	if input.Requested.WarningTokens > math.MaxInt64 ||
		input.Requested.HardStopTokens > math.MaxInt64 {
		return PreApprovalBudgetAdjustment{}, BudgetSnapshot{},
			errors.New("budget token threshold exceeds SQLite integer range")
	}
	var recorded PreApprovalBudgetAdjustment
	var snapshot BudgetSnapshot
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		existing, found, err := findPreApprovalBudgetAdjustment(
			ctx,
			transaction.sql,
			input.BudgetID,
			input.IdempotencyKey,
		)
		if err != nil {
			return err
		}
		if found {
			if existing.Adjusted != input.Requested ||
				existing.Actor != input.Actor ||
				existing.AuthorityReference != input.AuthorityReference ||
				existing.Reason != input.Reason {
				return typedError(
					ErrConflict,
					"adjust budget before approval",
					errors.New("idempotency key belongs to another adjustment"),
				)
			}
			recorded = existing
			return scanLatestBudgetSnapshot(
				ctx,
				transaction.sql,
				input.BudgetID,
				&snapshot,
			)
		}
		current, err := scanBudget(
			transaction.sql.QueryRowContext(
				ctx,
				budgetSelect+` WHERE id = ?`,
				input.BudgetID,
			),
			"load pre-approval budget",
		)
		if err != nil {
			return err
		}
		if current.Revision != input.ExpectedBudgetRevision {
			return typedError(
				ErrStaleRevision,
				"adjust budget before approval",
				errors.New("budget revision changed"),
			)
		}
		var state domain.TaskState
		if err := transaction.sql.QueryRowContext(
			ctx,
			`SELECT state FROM tasks WHERE id = ?`,
			current.TaskID,
		).Scan(&state); err != nil {
			return classify("load budget task state", err)
		}
		attributed, err := policy.AdjustBudgetBeforeApproval(
			state,
			current.Budget,
			input.Requested,
			input.Actor,
			input.AuthorityReference,
			input.Reason,
		)
		if err != nil {
			return err
		}
		if attributed.Adjusted.WarningCost.Currency !=
			attributed.Previous.WarningCost.Currency {
			return typedError(
				ErrConstraint,
				"adjust budget before approval",
				errors.New("budget currency cannot change"),
			)
		}
		if err := scanLatestBudgetSnapshot(
			ctx,
			transaction.sql,
			input.BudgetID,
			&snapshot,
		); err != nil {
			return err
		}
		if snapshot.LimitRevision != input.ExpectedLimitRevision {
			return typedError(
				ErrStaleRevision,
				"adjust budget before approval",
				errors.New("budget limit revision changed"),
			)
		}
		if snapshot.ReservedCost.Numerator != 0 ||
			snapshot.ChargedCost.Numerator != 0 ||
			snapshot.ActualKnownCost.Numerator != 0 ||
			snapshot.ReservedTokens != 0 ||
			snapshot.ChargedTokens != 0 ||
			snapshot.ActualTokens != 0 ||
			snapshot.CostAccountingUnknown ||
			snapshot.TokenAccountingUnknown ||
			snapshot.ReconciliationPending ||
			snapshot.ProviderCallSlots != 0 {
			return typedError(
				ErrConstraint,
				"adjust budget before approval",
				errors.New("budget already has execution exposure"),
			)
		}
		previousJSON, err := json.Marshal(attributed.Previous)
		if err != nil {
			return err
		}
		adjustedJSON, err := json.Marshal(attributed.Adjusted)
		if err != nil {
			return err
		}
		_, micros := repositories.timestamp()
		adjustedLimitRevision := snapshot.LimitRevision + 1
		provenance, _ := json.Marshal(map[string]any{
			"schema_version":      1,
			"source":              "preapproval-budget-adjustment",
			"authority_reference": input.AuthorityReference,
		})
		if _, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO budget_limit_revisions (
				budget_id, revision,
				warning_cost_minor_numerator,
				warning_cost_minor_denominator,
				hard_cost_minor_numerator,
				hard_cost_minor_denominator,
				currency, warning_tokens, hard_tokens, approval_id,
				authority_kind, actor_kind, actor_reference,
				reason_redacted, idempotency_key, provenance_json,
				created_at_unix_micros
			) VALUES (?, ?, ?, 1, ?, 1, ?, ?, ?, NULL,
			          'preapproval-user-adjustment', 'user', ?, ?, ?, ?, ?)`,
			input.BudgetID,
			adjustedLimitRevision,
			input.Requested.WarningCost.MinorUnits,
			input.Requested.HardStopCost.MinorUnits,
			input.Requested.WarningCost.Currency,
			input.Requested.WarningTokens,
			input.Requested.HardStopTokens,
			input.Actor,
			input.Reason,
			input.IdempotencyKey,
			string(provenance),
			micros,
		); err != nil {
			return repositoryWriteError("record adjusted budget limit", err)
		}
		result, err := transaction.sql.ExecContext(
			ctx,
			`UPDATE budgets
			 SET warning_cost_minor = ?, hard_stop_cost_minor = ?,
			     warning_tokens = ?, hard_stop_tokens = ?,
			     warning_wall_clock_millis = ?,
			     hard_stop_wall_clock_millis = ?,
			     maximum_provider_calls = ?,
			     maximum_repair_rounds = ?,
			     maximum_tool_executions = ?,
			     updated_at_unix_micros = ?, revision = revision + 1
			 WHERE id = ? AND revision = ?`,
			input.Requested.WarningCost.MinorUnits,
			input.Requested.HardStopCost.MinorUnits,
			input.Requested.WarningTokens,
			input.Requested.HardStopTokens,
			input.Requested.WarningWallClock,
			input.Requested.HardStopWallClock,
			input.Requested.MaximumProviderCalls,
			input.Requested.MaximumRepairRounds,
			input.Requested.MaximumToolExecutions,
			micros,
			input.BudgetID,
			input.ExpectedBudgetRevision,
		)
		if err != nil {
			return repositoryWriteError("apply adjusted budget", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected != 1 {
			return typedError(
				ErrStaleRevision,
				"adjust budget before approval",
				errors.New("budget revision changed during adjustment"),
			)
		}
		var revision uint64
		if err := transaction.sql.QueryRowContext(
			ctx,
			`SELECT COALESCE(MAX(revision), 0) + 1
			 FROM preapproval_budget_adjustments WHERE budget_id = ?`,
			input.BudgetID,
		).Scan(&revision); err != nil {
			return classify("allocate budget adjustment revision", err)
		}
		if _, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO preapproval_budget_adjustments (
				budget_id, revision, task_id, previous_limit_revision,
				adjusted_limit_revision, previous_budget_json,
				adjusted_budget_json, actor_reference,
				authority_reference, reason_redacted, idempotency_key,
				created_at_unix_micros
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			input.BudgetID,
			revision,
			current.TaskID,
			snapshot.LimitRevision,
			adjustedLimitRevision,
			string(previousJSON),
			string(adjustedJSON),
			input.Actor,
			input.AuthorityReference,
			input.Reason,
			input.IdempotencyKey,
			micros,
		); err != nil {
			return repositoryWriteError("record budget adjustment", err)
		}
		snapshot, err = computeBudgetSnapshot(
			ctx,
			transaction.sql,
			input.BudgetID,
		)
		if err != nil {
			return err
		}
		snapshot.CreatedAt = repositoryTime(micros)
		if err := persistBudgetSnapshot(
			ctx,
			transaction.sql,
			snapshot,
		); err != nil {
			return err
		}
		recorded = PreApprovalBudgetAdjustment{
			BudgetID:              input.BudgetID,
			TaskID:                current.TaskID,
			Revision:              revision,
			PreviousLimitRevision: snapshot.LimitRevision - 1,
			AdjustedLimitRevision: snapshot.LimitRevision,
			Previous:              attributed.Previous,
			Adjusted:              attributed.Adjusted,
			Actor:                 input.Actor,
			AuthorityReference:    input.AuthorityReference,
			Reason:                input.Reason,
			IdempotencyKey:        input.IdempotencyKey,
			CreatedAt:             repositoryTime(micros),
		}
		return nil
	})
	return recorded, snapshot, err
}

func findPreApprovalBudgetAdjustment(
	ctx context.Context,
	queries queryRower,
	budgetID domain.BudgetID,
	key string,
) (PreApprovalBudgetAdjustment, bool, error) {
	var (
		value         PreApprovalBudgetAdjustment
		previousJSON  string
		adjustedJSON  string
		createdMicros int64
	)
	err := queries.QueryRowContext(
		ctx,
		`SELECT budget_id, task_id, revision, previous_limit_revision,
		        adjusted_limit_revision, previous_budget_json,
		        adjusted_budget_json, actor_reference,
		        authority_reference, reason_redacted, idempotency_key,
		        created_at_unix_micros
		 FROM preapproval_budget_adjustments
		 WHERE budget_id = ? AND idempotency_key = ?`,
		budgetID,
		key,
	).Scan(
		&value.BudgetID,
		&value.TaskID,
		&value.Revision,
		&value.PreviousLimitRevision,
		&value.AdjustedLimitRevision,
		&previousJSON,
		&adjustedJSON,
		&value.Actor,
		&value.AuthorityReference,
		&value.Reason,
		&value.IdempotencyKey,
		&createdMicros,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return PreApprovalBudgetAdjustment{}, false, nil
	}
	if err != nil {
		return PreApprovalBudgetAdjustment{}, false,
			classify("find pre-approval budget adjustment", err)
	}
	if err := json.Unmarshal([]byte(previousJSON), &value.Previous); err != nil {
		return PreApprovalBudgetAdjustment{}, false,
			typedError(ErrCorrupt, "decode previous budget", err)
	}
	if err := json.Unmarshal([]byte(adjustedJSON), &value.Adjusted); err != nil {
		return PreApprovalBudgetAdjustment{}, false,
			typedError(ErrCorrupt, "decode adjusted budget", err)
	}
	if strings.TrimSpace(value.Actor) == "" ||
		strings.TrimSpace(value.AuthorityReference) == "" ||
		strings.TrimSpace(value.Reason) == "" {
		return PreApprovalBudgetAdjustment{}, false,
			typedError(
				ErrCorrupt,
				"read pre-approval budget adjustment",
				fmt.Errorf("attribution is incomplete"),
			)
	}
	value.CreatedAt = repositoryTime(createdMicros)
	return value, true, nil
}
