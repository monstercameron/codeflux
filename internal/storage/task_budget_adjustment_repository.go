package storage

import (
	"context"
	"database/sql"
	"errors"

	"codeflux.dev/codeflux/internal/domain"
)

// TaskBudgetAdjustmentState is the authoritative state needed to choose the
// pre-approval adjustment path or the explicitly approved raise path.
type TaskBudgetAdjustmentState struct {
	TaskState domain.TaskState
	Account   BudgetAccount
	Snapshot  BudgetSnapshot
}

// ReadTaskBudgetAdjustmentState reads the task lifecycle, complete legacy
// budget policy, and exact accounting snapshot from one SQLite transaction.
func (repositories *Repositories) ReadTaskBudgetAdjustmentState(
	ctx context.Context,
	taskID domain.TaskID,
) (TaskBudgetAdjustmentState, error) {
	if taskID.IsZero() {
		return TaskBudgetAdjustmentState{}, errors.New("task ID is required")
	}
	var state TaskBudgetAdjustmentState
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		if err := transaction.sql.QueryRowContext(
			ctx, `SELECT state FROM tasks WHERE id = ?`, taskID,
		).Scan(&state.TaskState); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return typedError(ErrNotFound, "read task budget adjustment state", err)
			}
			return classify("read task budget adjustment state", err)
		}
		account, err := scanBudget(
			transaction.sql.QueryRowContext(
				ctx, budgetSelect+` WHERE task_id = ?`, taskID,
			),
			"read task budget adjustment account",
		)
		if err != nil {
			return err
		}
		snapshot, err := computeBudgetSnapshot(ctx, transaction.sql, account.Budget.ID)
		if err != nil {
			return err
		}
		state.Account = account
		state.Snapshot = snapshot
		return nil
	})
	return state, err
}

// ResolveBudgetRaiseApproval first preserves the authority attached to an
// idempotent prior raise, then resolves only a granted approval whose task and
// canonical scope exactly match a new proposed limit revision.
func (repositories *Repositories) ResolveBudgetRaiseApproval(
	ctx context.Context,
	budgetID domain.BudgetID,
	taskID domain.TaskID,
	idempotencyKey string,
	scope string,
) (domain.ApprovalID, error) {
	if budgetID.IsZero() || taskID.IsZero() || idempotencyKey == "" || scope == "" {
		return domain.ApprovalID{}, errors.New("budget, task, command, and approval scope are required")
	}
	var priorApproval sql.NullString
	var authorityKind string
	err := repositories.database.sql.QueryRowContext(
		ctx,
		`SELECT approval_id, authority_kind
		 FROM budget_limit_revisions
		 WHERE budget_id = ? AND idempotency_key = ?`,
		budgetID,
		idempotencyKey,
	).Scan(&priorApproval, &authorityKind)
	if err == nil {
		if authorityKind != "user-approval" || !priorApproval.Valid {
			return domain.ApprovalID{}, typedError(
				ErrConflict, "resolve budget raise approval",
				errors.New("idempotency key belongs to a different budget adjustment"),
			)
		}
		approvalID, parseErr := domain.ParseApprovalID(priorApproval.String)
		if parseErr != nil {
			return domain.ApprovalID{}, typedError(
				ErrCorrupt, "resolve budget raise approval", parseErr,
			)
		}
		return approvalID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return domain.ApprovalID{}, classify("resolve idempotent budget raise approval", err)
	}
	var raw string
	err = repositories.database.sql.QueryRowContext(
		ctx,
		`SELECT id
		 FROM approvals
		 WHERE task_id = ? AND state = 'granted' AND scope = ?
		 ORDER BY decided_at_unix_micros DESC, id ASC
		 LIMIT 1`,
		taskID,
		scope,
	).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ApprovalID{}, typedError(
			ErrBudgetApprovalRequired,
			"find granted budget raise approval",
			err,
		)
	}
	if err != nil {
		return domain.ApprovalID{}, classify("resolve granted budget raise approval", err)
	}
	approvalID, err := domain.ParseApprovalID(raw)
	if err != nil {
		return domain.ApprovalID{}, typedError(
			ErrCorrupt, "find granted budget raise approval", err,
		)
	}
	return approvalID, nil
}
