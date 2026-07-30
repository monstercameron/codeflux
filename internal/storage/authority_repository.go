package storage

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

func (repositories *Repositories) CreateApproval(
	ctx context.Context,
	input CreateApproval,
) (Approval, error) {
	switch {
	case input.ID.IsZero():
		return Approval{}, errors.New("approval ID must not be empty")
	case input.TaskID.IsZero():
		return Approval{}, errors.New("task ID must not be empty")
	}
	if err := validateBounded("approval scope", input.Scope, 1024); err != nil {
		return Approval{}, err
	}
	if err := validateBounded("approval request reason", input.RequestReason, 2048); err != nil {
		return Approval{}, err
	}
	if err := validateBounded("approval idempotency key", input.IdempotencyKey, 255); err != nil {
		return Approval{}, err
	}
	now, micros := repositories.timestamp()
	if input.ExpiresAt != nil && input.ExpiresAt.UTC().Before(now) {
		return Approval{}, errors.New("approval expiration must not precede request time")
	}
	approval := Approval{
		ID:             input.ID,
		TaskID:         input.TaskID,
		RunID:          input.RunID,
		State:          domain.ApprovalRequestStatePending,
		Scope:          input.Scope,
		RequestReason:  input.RequestReason,
		IdempotencyKey: input.IdempotencyKey,
		RequestedAt:    now,
		ExpiresAt:      input.ExpiresAt,
	}
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		existing, found, err := findApprovalByIdempotency(
			ctx,
			transaction,
			input.TaskID,
			input.IdempotencyKey,
		)
		if err != nil {
			return err
		}
		if found {
			if existing.ID != input.ID ||
				!sameRunID(existing.RunID, input.RunID) ||
				existing.Scope != input.Scope ||
				existing.RequestReason != input.RequestReason ||
				!sameTimePointer(existing.ExpiresAt, input.ExpiresAt) {
				return typedError(
					ErrConflict,
					"create idempotent approval",
					errors.New("idempotency key belongs to different approval"),
				)
			}
			approval = existing
			return nil
		}
		if input.RunID != nil {
			if err := verifyRunBelongsToTask(
				ctx,
				transaction,
				*input.RunID,
				input.TaskID,
				"create approval",
			); err != nil {
				return err
			}
		}
		_, err = transaction.sql.ExecContext(
			ctx,
			`INSERT INTO approvals (
				id, task_id, run_id, state, scope, request_reason,
				idempotency_key, requested_at_unix_micros,
				expires_at_unix_micros, revision
			) VALUES (?, ?, ?, 'pending', ?, ?, ?, ?, ?, 0)`,
			input.ID,
			input.TaskID,
			nullableRunID(input.RunID),
			input.Scope,
			input.RequestReason,
			input.IdempotencyKey,
			micros,
			nullableTimeMicros(input.ExpiresAt),
		)
		return repositoryWriteError("create approval", err)
	})
	return approval, err
}

func (repositories *Repositories) ResolveApproval(
	ctx context.Context,
	input ResolveApproval,
) (Approval, error) {
	if input.ID.IsZero() {
		return Approval{}, errors.New("approval ID must not be empty")
	}
	if err := domain.ValidateApprovalRequestTransition(
		domain.ApprovalRequestStatePending,
		input.To,
	); err != nil {
		return Approval{}, err
	}
	if err := validateBounded("approval resolution reason", input.ResolutionReason, 2048); err != nil {
		return Approval{}, err
	}
	now, micros := repositories.timestamp()
	var approval Approval
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		current, err := scanApproval(transaction.sql.QueryRowContext(
			ctx,
			`SELECT id, task_id, run_id, state, scope, request_reason,
			        resolution_reason, idempotency_key, requested_at_unix_micros,
			        decided_at_unix_micros, expires_at_unix_micros, revision
			 FROM approvals WHERE id = ?`,
			input.ID,
		), "read approval for resolution")
		if err != nil {
			return err
		}
		if current.Revision != input.ExpectedRevision ||
			current.State != domain.ApprovalRequestStatePending {
			return typedError(
				ErrStaleRevision,
				"resolve approval",
				errors.New("approval state or revision changed"),
			)
		}
		result, err := transaction.sql.ExecContext(
			ctx,
			`UPDATE approvals
			 SET state = ?, resolution_reason = ?, decided_at_unix_micros = ?,
			     revision = revision + 1
			 WHERE id = ? AND state = 'pending' AND revision = ?`,
			input.To,
			input.ResolutionReason,
			micros,
			input.ID,
			input.ExpectedRevision,
		)
		if err != nil {
			return repositoryWriteError("resolve approval", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected != 1 {
			return typedError(
				ErrStaleRevision,
				"resolve approval",
				errors.New("approval revision changed during resolution"),
			)
		}
		current.State = input.To
		current.ResolutionReason = &input.ResolutionReason
		current.DecidedAt = &now
		current.Revision++
		approval = current
		return nil
	})
	return approval, err
}

func (repositories *Repositories) CreateBudget(
	ctx context.Context,
	input CreateBudget,
) (BudgetAccount, error) {
	if input.TaskID.IsZero() {
		return BudgetAccount{}, errors.New("task ID must not be empty")
	}
	if err := input.Budget.Validate(); err != nil {
		return BudgetAccount{}, err
	}
	if input.Budget.WarningTokens > math.MaxInt64 ||
		input.Budget.HardStopTokens > math.MaxInt64 {
		return BudgetAccount{}, errors.New("budget token threshold exceeds SQLite integer range")
	}
	now, micros := repositories.timestamp()
	account := BudgetAccount{
		Budget:       input.Budget,
		TaskID:       input.TaskID,
		ReservedCost: domain.Money{Currency: input.Budget.WarningCost.Currency},
		ActualCost:   domain.Money{Currency: input.Budget.WarningCost.Currency},
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		_, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO budgets (
				id, task_id, currency, warning_cost_minor, hard_stop_cost_minor,
				warning_tokens, hard_stop_tokens, warning_wall_clock_millis,
				hard_stop_wall_clock_millis, maximum_provider_calls,
				maximum_repair_rounds, maximum_tool_executions,
				reserved_cost_minor, actual_cost_minor, actual_tokens,
				created_at_unix_micros, updated_at_unix_micros, revision
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, 0, 0, ?, ?, 0)`,
			input.Budget.ID,
			input.TaskID,
			input.Budget.WarningCost.Currency,
			input.Budget.WarningCost.MinorUnits,
			input.Budget.HardStopCost.MinorUnits,
			input.Budget.WarningTokens,
			input.Budget.HardStopTokens,
			input.Budget.WarningWallClock,
			input.Budget.HardStopWallClock,
			input.Budget.MaximumProviderCalls,
			input.Budget.MaximumRepairRounds,
			input.Budget.MaximumToolExecutions,
			micros,
			micros,
		)
		return repositoryWriteError("create budget", err)
	})
	return account, err
}

func (repositories *Repositories) ReserveBudget(
	ctx context.Context,
	input ReserveBudget,
) (BudgetAccount, error) {
	if input.ID.IsZero() {
		return BudgetAccount{}, errors.New("budget ID must not be empty")
	}
	if err := input.Amount.Validate(); err != nil {
		return BudgetAccount{}, err
	}
	if input.Amount.MinorUnits < 0 {
		return BudgetAccount{}, errors.New("reservation amount must not be negative")
	}
	_, micros := repositories.timestamp()
	var account BudgetAccount
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		current, err := scanBudget(transaction.sql.QueryRowContext(
			ctx,
			budgetSelect+` WHERE id = ?`,
			input.ID,
		), "read budget for reservation")
		if err != nil {
			return err
		}
		if current.Revision != input.ExpectedRevision {
			return typedError(ErrStaleRevision, "reserve budget", errors.New("budget revision changed"))
		}
		if current.Budget.WarningCost.Currency != input.Amount.Currency {
			return typedError(ErrConstraint, "reserve budget", errors.New("currency mismatch"))
		}
		exactSnapshot, err := computeBudgetSnapshot(
			ctx, transaction.sql, input.ID,
		)
		if err != nil {
			return err
		}
		if exactSnapshot.CostAccountingUnknown ||
			exactSnapshot.TokenAccountingUnknown {
			return typedError(
				ErrBudgetAccountingUnknown, "reserve budget",
				errors.New("prior settled usage is unknown"),
			)
		}
		if exactSnapshot.HardCapReached {
			return typedError(
				ErrBudgetExhausted, "reserve budget",
				errors.New("task hard cap is already reached"),
			)
		}
		legacyReservation := ExactMinorCost{
			Numerator: input.Amount.MinorUnits, Denominator: 1,
			Currency: input.Amount.Currency,
		}
		exposure, err := addExactCosts(
			exactSnapshot.ReservedCost, exactSnapshot.ChargedCost,
		)
		if err != nil {
			return err
		}
		exposure, err = addExactCosts(exposure, legacyReservation)
		if err != nil {
			return err
		}
		if compareExactCosts(exposure, exactSnapshot.HardCost) > 0 {
			return typedError(
				ErrConstraint, "reserve budget",
				errors.New("hard cost cap exceeded"),
			)
		}
		if input.Amount.MinorUnits >
			current.Budget.HardStopCost.MinorUnits-
				current.ReservedCost.MinorUnits-
				current.ActualCost.MinorUnits {
			return typedError(ErrConstraint, "reserve budget", errors.New("hard cost cap exceeded"))
		}
		result, err := transaction.sql.ExecContext(
			ctx,
			`UPDATE budgets
			 SET reserved_cost_minor = reserved_cost_minor + ?,
			     updated_at_unix_micros = ?, revision = revision + 1
			 WHERE id = ? AND revision = ?`,
			input.Amount.MinorUnits,
			micros,
			input.ID,
			input.ExpectedRevision,
		)
		if err != nil {
			return repositoryWriteError("reserve budget", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected != 1 {
			return typedError(ErrStaleRevision, "reserve budget", errors.New("budget changed"))
		}
		current.ReservedCost.MinorUnits += input.Amount.MinorUnits
		current.UpdatedAt = repositoryTime(micros)
		current.Revision++
		account = current
		return nil
	})
	return account, err
}

func (repositories *Repositories) PostActualCost(
	ctx context.Context,
	input PostActualCost,
) (BudgetAccount, error) {
	if input.ID.IsZero() {
		return BudgetAccount{}, errors.New("budget ID must not be empty")
	}
	if err := input.Actual.Validate(); err != nil {
		return BudgetAccount{}, err
	}
	if input.Actual.MinorUnits < 0 || input.ReleaseReservedMinor < 0 {
		return BudgetAccount{}, errors.New("actual cost and reservation release must not be negative")
	}
	if input.Tokens > math.MaxInt64 {
		return BudgetAccount{}, errors.New("actual token count exceeds SQLite integer range")
	}
	_, micros := repositories.timestamp()
	var account BudgetAccount
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		current, err := scanBudget(transaction.sql.QueryRowContext(
			ctx,
			budgetSelect+` WHERE id = ?`,
			input.ID,
		), "read budget for actual cost")
		if err != nil {
			return err
		}
		switch {
		case current.Revision != input.ExpectedRevision:
			return typedError(ErrStaleRevision, "post actual cost", errors.New("budget revision changed"))
		case current.Budget.WarningCost.Currency != input.Actual.Currency:
			return typedError(ErrConstraint, "post actual cost", errors.New("currency mismatch"))
		case input.ReleaseReservedMinor > current.ReservedCost.MinorUnits:
			return typedError(ErrConstraint, "post actual cost", errors.New("release exceeds reservation"))
		case input.Actual.MinorUnits > math.MaxInt64-current.ActualCost.MinorUnits:
			return typedError(ErrConstraint, "post actual cost", errors.New("actual cost overflow"))
		case uint64(input.Tokens) > uint64(math.MaxInt64)-uint64(current.ActualTokens):
			return typedError(ErrConstraint, "post actual cost", errors.New("actual token overflow"))
		}
		newReserved := current.ReservedCost.MinorUnits - input.ReleaseReservedMinor
		newActual := current.ActualCost.MinorUnits + input.Actual.MinorUnits
		if newReserved > current.Budget.HardStopCost.MinorUnits ||
			newActual > current.Budget.HardStopCost.MinorUnits-newReserved {
			return typedError(ErrConstraint, "post actual cost", errors.New("hard cost cap exceeded"))
		}
		result, err := transaction.sql.ExecContext(
			ctx,
			`UPDATE budgets
			 SET reserved_cost_minor = ?, actual_cost_minor = ?,
			     actual_tokens = actual_tokens + ?,
			     updated_at_unix_micros = ?, revision = revision + 1
			 WHERE id = ? AND revision = ?`,
			newReserved,
			newActual,
			input.Tokens,
			micros,
			input.ID,
			input.ExpectedRevision,
		)
		if err != nil {
			return repositoryWriteError("post actual cost", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected != 1 {
			return typedError(ErrStaleRevision, "post actual cost", errors.New("budget changed"))
		}
		current.ReservedCost.MinorUnits = newReserved
		current.ActualCost.MinorUnits = newActual
		current.ActualTokens += input.Tokens
		current.UpdatedAt = repositoryTime(micros)
		current.Revision++
		account = current
		return nil
	})
	return account, err
}

const budgetSelect = `SELECT
	id, task_id, currency, warning_cost_minor, hard_stop_cost_minor,
	warning_tokens, hard_stop_tokens, warning_wall_clock_millis,
	hard_stop_wall_clock_millis, maximum_provider_calls,
	maximum_repair_rounds, maximum_tool_executions,
	reserved_cost_minor, actual_cost_minor, actual_tokens,
	created_at_unix_micros, updated_at_unix_micros, revision
	FROM budgets`

func scanBudget(row rowScanner, operation string) (BudgetAccount, error) {
	var (
		account        BudgetAccount
		currencyRaw    string
		warningCost    int64
		hardCost       int64
		warningTokens  int64
		hardTokens     int64
		warningWall    int64
		hardWall       int64
		maximumCalls   uint32
		maximumRepairs uint32
		maximumTools   uint32
		reservedCost   int64
		actualCost     int64
		actualTokens   int64
		createdMicros  int64
		updatedMicros  int64
	)
	err := row.Scan(
		&account.Budget.ID,
		&account.TaskID,
		&currencyRaw,
		&warningCost,
		&hardCost,
		&warningTokens,
		&hardTokens,
		&warningWall,
		&hardWall,
		&maximumCalls,
		&maximumRepairs,
		&maximumTools,
		&reservedCost,
		&actualCost,
		&actualTokens,
		&createdMicros,
		&updatedMicros,
		&account.Revision,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return BudgetAccount{}, typedError(ErrNotFound, operation, err)
	}
	if err != nil {
		return BudgetAccount{}, classify(operation, err)
	}
	currency, err := domain.ParseCurrencyCode(currencyRaw)
	if err != nil {
		return BudgetAccount{}, typedError(ErrCorrupt, operation, err)
	}
	account.Budget.WarningCost = domain.Money{Currency: currency, MinorUnits: warningCost}
	account.Budget.HardStopCost = domain.Money{Currency: currency, MinorUnits: hardCost}
	account.Budget.WarningTokens = domain.TokenCount(warningTokens)
	account.Budget.HardStopTokens = domain.TokenCount(hardTokens)
	account.Budget.WarningWallClock = domain.Milliseconds(warningWall)
	account.Budget.HardStopWallClock = domain.Milliseconds(hardWall)
	account.Budget.MaximumProviderCalls = maximumCalls
	account.Budget.MaximumRepairRounds = maximumRepairs
	account.Budget.MaximumToolExecutions = maximumTools
	account.ReservedCost = domain.Money{Currency: currency, MinorUnits: reservedCost}
	account.ActualCost = domain.Money{Currency: currency, MinorUnits: actualCost}
	account.ActualTokens = domain.TokenCount(actualTokens)
	account.CreatedAt = repositoryTime(createdMicros)
	account.UpdatedAt = repositoryTime(updatedMicros)
	return account, nil
}

func findApprovalByIdempotency(
	ctx context.Context,
	transaction *Transaction,
	taskID domain.TaskID,
	key string,
) (Approval, bool, error) {
	approval, err := scanApproval(transaction.sql.QueryRowContext(
		ctx,
		`SELECT id, task_id, run_id, state, scope, request_reason,
		        resolution_reason, idempotency_key, requested_at_unix_micros,
		        decided_at_unix_micros, expires_at_unix_micros, revision
		 FROM approvals WHERE task_id = ? AND idempotency_key = ?`,
		taskID,
		key,
	), "find idempotent approval")
	if errors.Is(err, ErrNotFound) {
		return Approval{}, false, nil
	}
	return approval, err == nil, err
}

func scanApproval(row rowScanner, operation string) (Approval, error) {
	var (
		approval      Approval
		runRaw        sql.NullString
		resolutionRaw sql.NullString
		requested     int64
		decided       sql.NullInt64
		expires       sql.NullInt64
	)
	err := row.Scan(
		&approval.ID,
		&approval.TaskID,
		&runRaw,
		&approval.State,
		&approval.Scope,
		&approval.RequestReason,
		&resolutionRaw,
		&approval.IdempotencyKey,
		&requested,
		&decided,
		&expires,
		&approval.Revision,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Approval{}, typedError(ErrNotFound, operation, err)
	}
	if err != nil {
		return Approval{}, classify(operation, err)
	}
	if runRaw.Valid {
		id, err := domain.ParseRunID(runRaw.String)
		if err != nil {
			return Approval{}, typedError(ErrCorrupt, operation, err)
		}
		approval.RunID = &id
	}
	if resolutionRaw.Valid {
		approval.ResolutionReason = &resolutionRaw.String
	}
	approval.RequestedAt = repositoryTime(requested)
	if decided.Valid {
		value := repositoryTime(decided.Int64)
		approval.DecidedAt = &value
	}
	if expires.Valid {
		value := repositoryTime(expires.Int64)
		approval.ExpiresAt = &value
	}
	return approval, nil
}

func nullableTimeMicros(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().UnixMicro()
}

func sameTimePointer(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.UTC().Equal(right.UTC())
}
