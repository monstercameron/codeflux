package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

var (
	// ErrBudgetExhausted means a new billable operation cannot fit inside the
	// currently approved hard task budget.
	ErrBudgetExhausted = errors.New("task budget exhausted")
	// ErrBudgetAccountingUnknown means a completed operation has unknown cost
	// or token accounting, so no later billable operation may begin safely.
	ErrBudgetAccountingUnknown = errors.New("task budget accounting unknown")
	// ErrBudgetApprovalRequired means a limit mutation lacks explicit granted
	// authority bound to the same task.
	ErrBudgetApprovalRequired = errors.New("task budget approval required")
	// ErrBudgetReconciliationPending means billed usage is durably retained but
	// has not yet reached the exact committed usage ledger.
	ErrBudgetReconciliationPending = errors.New(
		"task budget reconciliation pending",
	)
)

// BudgetCostCategory identifies the independently visible economic class.
type BudgetCostCategory string

const (
	BudgetCostModel          BudgetCostCategory = "model"
	BudgetCostTool           BudgetCostCategory = "tool"
	BudgetCostInfrastructure BudgetCostCategory = "infrastructure"
)

// BudgetReservationState is the durable pre-I/O cost-bound lifecycle.
type BudgetReservationState string

const (
	BudgetReservationActive    BudgetReservationState = "active"
	BudgetReservationCommitted BudgetReservationState = "committed"
	BudgetReservationReleased  BudgetReservationState = "released"
)

// BudgetReservation binds one approved bound to one physical retry attempt.
type BudgetReservation struct {
	ID                       string
	BudgetID                 domain.BudgetID
	TaskID                   domain.TaskID
	OperationID              string
	AttemptID                *string
	RetryOrdinal             uint64
	Category                 BudgetCostCategory
	ProviderCallSlots        uint32
	State                    BudgetReservationState
	CostBound                ExactMinorCost
	TokenBound               *domain.TokenCount
	IdempotencyKey           string
	ProvenanceJSON           string
	Settlement               *string
	SettlementProvenanceJSON *string
	CreatedAt                time.Time
	SettledAt                *time.Time
	Revision                 uint64
}

// BudgetCategorySnapshot separates model, tool, and infrastructure exposure.
type BudgetCategorySnapshot struct {
	Category              BudgetCostCategory
	ReservationCount      uint64
	ProviderCallSlots     uint64
	ReservedCost          ExactMinorCost
	ChargedCost           ExactMinorCost
	ActualKnownCost       ExactMinorCost
	CostUnknown           bool
	ReservedTokens        domain.TokenCount
	ChargedTokens         domain.TokenCount
	ActualTokens          domain.TokenCount
	TokensUnknown         bool
	ReconciliationPending bool
}

// BudgetSnapshot is one immutable, revision-bound view of task spending.
type BudgetSnapshot struct {
	BudgetID               domain.BudgetID
	TaskID                 domain.TaskID
	Revision               uint64
	LimitRevision          uint64
	WarningCost            ExactMinorCost
	HardCost               ExactMinorCost
	WarningTokens          domain.TokenCount
	HardTokens             domain.TokenCount
	ReservedCost           ExactMinorCost
	ChargedCost            ExactMinorCost
	ActualKnownCost        ExactMinorCost
	CostAccountingUnknown  bool
	RemainingCost          *ExactMinorCost
	ReservedTokens         domain.TokenCount
	ChargedTokens          domain.TokenCount
	ActualTokens           domain.TokenCount
	TokenAccountingUnknown bool
	RemainingTokens        *domain.TokenCount
	WarningReached         bool
	HardCapReached         bool
	ProviderCallSlots      uint64
	ReconciliationPending  bool
	Categories             []BudgetCategorySnapshot
	CreatedAt              time.Time
}

// ReserveProviderBudget atomically claims one bounded physical attempt.
type ReserveProviderBudget struct {
	ID                string
	BudgetID          domain.BudgetID
	ExpectedRevision  uint64
	OperationID       string
	AttemptID         *string
	RetryOrdinal      uint64
	Category          BudgetCostCategory
	ProviderCallSlots uint32
	CostBound         ExactMinorCost
	TokenBound        *domain.TokenCount
	IdempotencyKey    string
	ProvenanceJSON    string
}

// SettleProviderBudget commits actual usage and releases the reservation in
// one transaction. Nil actuals remain explicitly unknown.
type SettleProviderBudget struct {
	ID                      string
	ReservationID           string
	ActualCost              *ExactMinorCost
	ActualTokens            *domain.TokenCount
	ActualProviderCallSlots uint32
	IdempotencyKey          string
	ProvenanceJSON          string
}

// ReleaseProviderBudget releases a reservation only when external I/O did not
// consume the approved bound.
type ReleaseProviderBudget struct {
	ReservationID    string
	ExpectedRevision uint64
	ReasonRedacted   string
	ProvenanceJSON   string
}

// RecordBudgetReconciliationIntent durably preserves billed usage when the
// ordinary settlement call cannot complete.
type RecordBudgetReconciliationIntent struct {
	ID                      string
	ReservationID           string
	ActualCost              *ExactMinorCost
	ActualTokens            *domain.TokenCount
	ActualProviderCallSlots uint32
	ReasonRedacted          string
	IdempotencyKey          string
	ProvenanceJSON          string
}

// RaiseProviderBudget declares one explicitly approved monotonic hard-limit
// increase with attributable authority.
type RaiseProviderBudget struct {
	BudgetID         domain.BudgetID
	ExpectedRevision uint64
	WarningCost      ExactMinorCost
	HardCost         ExactMinorCost
	WarningTokens    domain.TokenCount
	HardTokens       domain.TokenCount
	ApprovalID       domain.ApprovalID
	ActorKind        string
	ActorReference   string
	ReasonRedacted   string
	IdempotencyKey   string
	ProvenanceJSON   string
}

// BudgetLedgerOperations groups exact reservation and settlement operations.
type BudgetLedgerOperations interface {
	ReserveProviderBudget(
		context.Context,
		ReserveProviderBudget,
	) (BudgetReservation, BudgetSnapshot, error)
	SettleProviderBudget(context.Context, SettleProviderBudget) (BudgetSnapshot, error)
	ReleaseProviderBudget(context.Context, ReleaseProviderBudget) (BudgetSnapshot, error)
	RecordBudgetReconciliationIntent(
		context.Context,
		RecordBudgetReconciliationIntent,
	) (BudgetSnapshot, error)
	RaiseProviderBudget(context.Context, RaiseProviderBudget) (BudgetSnapshot, error)
	GetBudgetSnapshot(context.Context, domain.TaskID) (BudgetSnapshot, error)
}

var _ BudgetLedgerOperations = (*Repositories)(nil)

// ReserveProviderBudget prevents a physical attempt from beginning unless its
// exact approved bound fits both current cost and token hard caps.
func (repositories *Repositories) ReserveProviderBudget(
	ctx context.Context,
	input ReserveProviderBudget,
) (BudgetReservation, BudgetSnapshot, error) {
	normalizedCost, err := validateReserveProviderBudget(input)
	if err != nil {
		return BudgetReservation{}, BudgetSnapshot{}, err
	}
	var reservation BudgetReservation
	var snapshot BudgetSnapshot
	err = repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		existing, found, err := findBudgetReservationByIdentity(
			ctx, transaction.sql, input.BudgetID, input.ID, input.IdempotencyKey,
		)
		if err != nil {
			return err
		}
		if found {
			if !budgetReservationMatches(existing, input, normalizedCost) {
				return typedError(
					ErrConflict, "reserve provider budget",
					errors.New("reservation identity was reused with different bounds"),
				)
			}
			reservation = existing
			snapshot, err = computeBudgetSnapshot(ctx, transaction.sql, input.BudgetID)
			if err == nil && snapshot.ReconciliationPending {
				return typedError(
					ErrBudgetReconciliationPending, "reserve provider budget",
					errors.New("billed reservation still requires reconciliation"),
				)
			}
			return err
		}
		current, err := computeBudgetSnapshot(ctx, transaction.sql, input.BudgetID)
		if err != nil {
			return err
		}
		if current.Revision != input.ExpectedRevision {
			return typedError(
				ErrStaleRevision, "reserve provider budget",
				errors.New("budget revision changed"),
			)
		}
		if current.ReconciliationPending {
			return typedError(
				ErrBudgetReconciliationPending, "reserve provider budget",
				errors.New("prior billed usage still requires reconciliation"),
			)
		}
		if current.CostAccountingUnknown || current.TokenAccountingUnknown {
			return typedError(
				ErrBudgetAccountingUnknown, "reserve provider budget",
				errors.New("prior settled usage is unknown"),
			)
		}
		if normalizedCost.Currency != current.HardCost.Currency {
			return typedError(
				ErrConstraint, "reserve provider budget",
				errors.New("reservation currency differs from budget"),
			)
		}
		if current.HardCapReached {
			return typedError(
				ErrBudgetExhausted, "reserve provider budget",
				errors.New("task hard cap is already reached"),
			)
		}
		costExposure, err := addExactCosts(current.ReservedCost, current.ChargedCost)
		if err != nil {
			return err
		}
		costExposure, err = addExactCosts(costExposure, normalizedCost)
		if err != nil {
			return err
		}
		if compareExactCosts(costExposure, current.HardCost) > 0 {
			return typedError(
				ErrBudgetExhausted, "reserve provider budget",
				errors.New("reservation exceeds approved hard cost cap"),
			)
		}
		if input.TokenBound != nil {
			if uint64(*input.TokenBound) >
				uint64(current.HardTokens-current.ReservedTokens-current.ChargedTokens) {
				return typedError(
					ErrBudgetExhausted, "reserve provider budget",
					errors.New("reservation exceeds approved hard token cap"),
				)
			}
		} else if input.Category == BudgetCostModel {
			return errors.New("model reservation requires an exact token bound")
		}
		providerCallSlots := normalizedProviderCallSlots(input)
		if err := verifyBudgetOperationLimit(
			ctx, transaction.sql, input.BudgetID, input.Category,
			providerCallSlots,
		); err != nil {
			return err
		}
		now, micros := repositories.timestamp()
		_, err = transaction.sql.ExecContext(
			ctx,
			`INSERT INTO budget_reservations (
				id, budget_id, task_id, operation_id, attempt_id,
				retry_ordinal, category, provider_call_slots, state,
				cost_bound_minor_numerator,
				cost_bound_minor_denominator, currency,
				token_bound_known, token_bound, idempotency_key,
				provenance_json, created_at_unix_micros, revision
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'active', ?, ?, ?, ?, ?, ?, ?, ?, 0)`,
			input.ID, input.BudgetID, current.TaskID, input.OperationID,
			nullableString(input.AttemptID), input.RetryOrdinal, input.Category,
			providerCallSlots,
			normalizedCost.Numerator, normalizedCost.Denominator,
			normalizedCost.Currency, boolInteger(input.TokenBound != nil),
			nullableTokenCount(input.TokenBound), input.IdempotencyKey,
			input.ProvenanceJSON, micros,
		)
		if err != nil {
			return repositoryWriteError("reserve provider budget", err)
		}
		newRevision, err := advanceBudgetRevision(
			ctx, transaction.sql, input.BudgetID, input.ExpectedRevision, micros,
		)
		if err != nil {
			return err
		}
		snapshot, err = computeBudgetSnapshot(ctx, transaction.sql, input.BudgetID)
		if err != nil {
			return err
		}
		snapshot.Revision = newRevision
		snapshot.CreatedAt = now
		if err := persistBudgetSnapshot(ctx, transaction.sql, snapshot); err != nil {
			return err
		}
		if err := persistBudgetBoundaryEvents(ctx, transaction.sql, snapshot, micros); err != nil {
			return err
		}
		reservation = BudgetReservation{
			ID: input.ID, BudgetID: input.BudgetID, TaskID: current.TaskID,
			OperationID: input.OperationID, AttemptID: cloneString(input.AttemptID),
			RetryOrdinal: input.RetryOrdinal, Category: input.Category,
			ProviderCallSlots: providerCallSlots,
			State:             BudgetReservationActive, CostBound: normalizedCost,
			TokenBound:     cloneTokenCount(input.TokenBound),
			IdempotencyKey: input.IdempotencyKey,
			ProvenanceJSON: input.ProvenanceJSON, CreatedAt: now,
		}
		return nil
	})
	return reservation, snapshot, err
}

// SettleProviderBudget always permits an already approved in-flight attempt to
// settle. Exposure beyond the cap is retained and blocks subsequent reserves.
func (repositories *Repositories) SettleProviderBudget(
	ctx context.Context,
	input SettleProviderBudget,
) (BudgetSnapshot, error) {
	if err := validateSettleProviderBudget(input); err != nil {
		return BudgetSnapshot{}, err
	}
	var actualCost *ExactMinorCost
	if input.ActualCost != nil {
		value, err := normalizeExactCost(*input.ActualCost)
		if err != nil {
			return BudgetSnapshot{}, err
		}
		actualCost = &value
	}
	var snapshot BudgetSnapshot
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		reservation, err := getBudgetReservation(
			ctx, transaction.sql, input.ReservationID,
		)
		if err != nil {
			return err
		}
		existing, found, err := findBudgetPostingByIdentity(
			ctx, transaction.sql, reservation.BudgetID,
			input.ID, input.IdempotencyKey,
		)
		if err != nil {
			return err
		}
		if found {
			actualProviderCallSlots, normalizeErr :=
				normalizedActualProviderCallSlots(
					reservation, input.ActualProviderCallSlots,
				)
			if normalizeErr != nil {
				return normalizeErr
			}
			if existing.ReservationID != input.ReservationID ||
				!exactCostEqual(existing.ActualCost, actualCost) ||
				!sameTokenCount(existing.ActualTokens, input.ActualTokens) ||
				existing.ActualProviderCallSlots != actualProviderCallSlots {
				return typedError(
					ErrConflict, "settle provider budget",
					errors.New("posting identity was reused with different actual usage"),
				)
			}
			return scanLatestBudgetSnapshot(
				ctx, transaction.sql, existing.BudgetID, &snapshot,
			)
		}
		if reservation.State != BudgetReservationActive {
			return typedError(
				ErrConflict, "settle provider budget",
				errors.New("reservation is not active"),
			)
		}
		actualProviderCallSlots, err := normalizedActualProviderCallSlots(
			reservation, input.ActualProviderCallSlots,
		)
		if err != nil {
			return err
		}
		current, err := computeBudgetSnapshot(ctx, transaction.sql, reservation.BudgetID)
		if err != nil {
			return err
		}
		if actualCost != nil && actualCost.Currency != reservation.CostBound.Currency {
			return typedError(
				ErrConstraint, "settle provider budget",
				errors.New("actual cost currency differs from reservation"),
			)
		}
		chargedCost := reservation.CostBound
		if actualCost != nil {
			chargedCost = *actualCost
		}
		var chargedTokens *domain.TokenCount
		if input.ActualTokens != nil {
			chargedTokens = cloneTokenCount(input.ActualTokens)
		} else {
			chargedTokens = cloneTokenCount(reservation.TokenBound)
		}
		_, micros := repositories.timestamp()
		result, err := transaction.sql.ExecContext(
			ctx,
			`UPDATE budget_reservations
			 SET state = 'committed', settlement_reason_redacted = ?,
			     settlement_provenance_json = ?,
			     settled_at_unix_micros = ?, revision = revision + 1
			 WHERE id = ? AND state = 'active' AND revision = ?`,
			"usage committed", input.ProvenanceJSON, micros,
			reservation.ID, reservation.Revision,
		)
		if err != nil {
			return repositoryWriteError("settle provider budget reservation", err)
		}
		changed, _ := result.RowsAffected()
		if changed != 1 {
			return typedError(
				ErrConflict, "settle provider budget",
				errors.New("reservation changed during settlement"),
			)
		}
		_, err = transaction.sql.ExecContext(
			ctx,
			`INSERT INTO budget_usage_postings (
				id, reservation_id, budget_id, task_id, category,
				actual_provider_call_slots,
				cost_known, cost_minor_numerator, cost_minor_denominator,
				charged_cost_minor_numerator,
				charged_cost_minor_denominator, currency,
				tokens_known, actual_tokens, charged_tokens,
				idempotency_key, provenance_json, created_at_unix_micros
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			input.ID, reservation.ID, reservation.BudgetID, reservation.TaskID,
			reservation.Category, actualProviderCallSlots,
			boolInteger(actualCost != nil),
			nullableExactNumerator(actualCost), nullableExactDenominator(actualCost),
			chargedCost.Numerator, chargedCost.Denominator, chargedCost.Currency,
			boolInteger(input.ActualTokens != nil),
			nullableTokenCount(input.ActualTokens), nullableTokenCount(chargedTokens),
			input.IdempotencyKey, input.ProvenanceJSON, micros,
		)
		if err != nil {
			return repositoryWriteError("settle provider budget", err)
		}
		newRevision, err := advanceBudgetRevision(
			ctx, transaction.sql, reservation.BudgetID,
			current.Revision, micros,
		)
		if err != nil {
			return err
		}
		snapshot, err = computeBudgetSnapshot(
			ctx, transaction.sql, reservation.BudgetID,
		)
		if err != nil {
			return err
		}
		snapshot.Revision = newRevision
		snapshot.CreatedAt = repositoryTime(micros)
		if err := persistBudgetSnapshot(ctx, transaction.sql, snapshot); err != nil {
			return err
		}
		return persistBudgetBoundaryEvents(
			ctx, transaction.sql, snapshot, micros,
		)
	})
	return snapshot, err
}

// ReleaseProviderBudget releases a bound without posting usage.
func (repositories *Repositories) ReleaseProviderBudget(
	ctx context.Context,
	input ReleaseProviderBudget,
) (BudgetSnapshot, error) {
	if err := validateReleaseProviderBudget(input); err != nil {
		return BudgetSnapshot{}, err
	}
	var snapshot BudgetSnapshot
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		reservation, err := getBudgetReservation(
			ctx, transaction.sql, input.ReservationID,
		)
		if err != nil {
			return err
		}
		if reservation.State == BudgetReservationReleased {
			if reservation.Settlement == nil ||
				*reservation.Settlement != input.ReasonRedacted ||
				reservation.SettlementProvenanceJSON == nil ||
				*reservation.SettlementProvenanceJSON != input.ProvenanceJSON {
				return typedError(
					ErrConflict, "release provider budget",
					errors.New("released reservation reason changed"),
				)
			}
			return scanLatestBudgetSnapshot(
				ctx, transaction.sql, reservation.BudgetID, &snapshot,
			)
		}
		if reservation.State != BudgetReservationActive {
			return typedError(
				ErrConflict, "release provider budget",
				errors.New("committed reservation cannot be released"),
			)
		}
		current, err := computeBudgetSnapshot(ctx, transaction.sql, reservation.BudgetID)
		if err != nil {
			return err
		}
		if current.Revision != input.ExpectedRevision {
			return typedError(
				ErrStaleRevision, "release provider budget",
				errors.New("budget revision changed"),
			)
		}
		_, micros := repositories.timestamp()
		result, err := transaction.sql.ExecContext(
			ctx,
			`UPDATE budget_reservations
			 SET state = 'released', settlement_reason_redacted = ?,
			     settlement_provenance_json = ?,
			     settled_at_unix_micros = ?,
			     revision = revision + 1
			 WHERE id = ? AND state = 'active' AND revision = ?`,
			input.ReasonRedacted, input.ProvenanceJSON, micros,
			reservation.ID, reservation.Revision,
		)
		if err != nil {
			return repositoryWriteError("release provider budget", err)
		}
		changed, _ := result.RowsAffected()
		if changed != 1 {
			return typedError(
				ErrConflict, "release provider budget",
				errors.New("reservation changed during release"),
			)
		}
		newRevision, err := advanceBudgetRevision(
			ctx, transaction.sql, reservation.BudgetID,
			input.ExpectedRevision, micros,
		)
		if err != nil {
			return err
		}
		snapshot, err = computeBudgetSnapshot(
			ctx, transaction.sql, reservation.BudgetID,
		)
		if err != nil {
			return err
		}
		snapshot.Revision = newRevision
		snapshot.CreatedAt = repositoryTime(micros)
		return persistBudgetSnapshot(ctx, transaction.sql, snapshot)
	})
	return snapshot, err
}

// RecordBudgetReconciliationIntent is the bounded fallback for billed work
// whose exact settlement could not be confirmed. It is append-only and does
// not rely on an optimistic revision supplied by the caller.
func (repositories *Repositories) RecordBudgetReconciliationIntent(
	ctx context.Context,
	input RecordBudgetReconciliationIntent,
) (BudgetSnapshot, error) {
	if err := validateBudgetReconciliationIntent(input); err != nil {
		return BudgetSnapshot{}, err
	}
	var actualCost *ExactMinorCost
	if input.ActualCost != nil {
		value, err := normalizeExactCost(*input.ActualCost)
		if err != nil {
			return BudgetSnapshot{}, err
		}
		actualCost = &value
	}
	var snapshot BudgetSnapshot
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		reservation, err := getBudgetReservation(
			ctx, transaction.sql, input.ReservationID,
		)
		if err != nil {
			return err
		}
		var (
			existingReservationID, existingReason, existingProvenance string
			existingCostKnown, existingTokensKnown                    int
			existingCostNumerator, existingCostDenominator            sql.NullInt64
			existingCurrencyRaw                                       string
			existingTokens                                            sql.NullInt64
			existingProviderCallSlots                                 uint32
		)
		err = transaction.sql.QueryRowContext(
			ctx,
			`SELECT reservation_id, actual_provider_call_slots,
			        cost_known, cost_minor_numerator,
			        cost_minor_denominator, currency, tokens_known,
			        actual_tokens, reason_redacted, provenance_json
			 FROM budget_reconciliation_intents
			 WHERE budget_id = ? AND (id = ? OR idempotency_key = ?)
			 ORDER BY CASE WHEN id = ? THEN 0 ELSE 1 END LIMIT 1`,
			reservation.BudgetID, input.ID, input.IdempotencyKey, input.ID,
		).Scan(
			&existingReservationID, &existingProviderCallSlots,
			&existingCostKnown,
			&existingCostNumerator, &existingCostDenominator,
			&existingCurrencyRaw, &existingTokensKnown, &existingTokens,
			&existingReason, &existingProvenance,
		)
		if err == nil {
			actualProviderCallSlots, normalizeErr :=
				normalizedActualProviderCallSlots(
					reservation, input.ActualProviderCallSlots,
				)
			if normalizeErr != nil {
				return normalizeErr
			}
			var existingCost *ExactMinorCost
			if existingCostKnown != 0 {
				currency, parseErr := domain.ParseCurrencyCode(existingCurrencyRaw)
				if parseErr != nil {
					return typedError(
						ErrCorrupt, "read budget reconciliation intent", parseErr,
					)
				}
				existingCost = &ExactMinorCost{
					Numerator:   existingCostNumerator.Int64,
					Denominator: existingCostDenominator.Int64,
					Currency:    currency,
				}
			}
			var existingActualTokens *domain.TokenCount
			if existingTokensKnown != 0 {
				value := domain.TokenCount(existingTokens.Int64)
				existingActualTokens = &value
			}
			if existingReservationID != input.ReservationID ||
				existingProviderCallSlots != actualProviderCallSlots ||
				!exactCostEqual(existingCost, actualCost) ||
				!sameTokenCount(existingActualTokens, input.ActualTokens) ||
				existingReason != input.ReasonRedacted ||
				existingProvenance != input.ProvenanceJSON {
				return typedError(
					ErrConflict, "record budget reconciliation intent",
					errors.New(
						"reconciliation intent identity was reused with different billed usage",
					),
				)
			}
			return scanLatestBudgetSnapshot(
				ctx, transaction.sql, reservation.BudgetID, &snapshot,
			)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return classify("find budget reconciliation intent", err)
		}
		var postingCount int
		if err := transaction.sql.QueryRowContext(
			ctx,
			`SELECT count(*) FROM budget_usage_postings
			 WHERE reservation_id = ?`,
			reservation.ID,
		).Scan(&postingCount); err != nil {
			return classify("check reconciled budget reservation", err)
		}
		if postingCount != 0 {
			return scanLatestBudgetSnapshot(
				ctx, transaction.sql, reservation.BudgetID, &snapshot,
			)
		}
		if reservation.State != BudgetReservationActive {
			return typedError(
				ErrConflict, "record budget reconciliation intent",
				errors.New("only an unsettled active reservation needs reconciliation"),
			)
		}
		actualProviderCallSlots, err := normalizedActualProviderCallSlots(
			reservation, input.ActualProviderCallSlots,
		)
		if err != nil {
			return err
		}
		if actualCost != nil &&
			actualCost.Currency != reservation.CostBound.Currency {
			return typedError(
				ErrConstraint, "record budget reconciliation intent",
				errors.New("billed cost currency differs from reservation"),
			)
		}
		_, micros := repositories.timestamp()
		_, err = transaction.sql.ExecContext(
			ctx,
			`INSERT INTO budget_reconciliation_intents (
				id, reservation_id, budget_id, task_id,
				actual_provider_call_slots,
				cost_known, cost_minor_numerator, cost_minor_denominator,
				currency, tokens_known, actual_tokens, reason_redacted,
				idempotency_key, provenance_json, created_at_unix_micros
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			input.ID, reservation.ID, reservation.BudgetID, reservation.TaskID,
			actualProviderCallSlots, boolInteger(actualCost != nil),
			nullableExactNumerator(actualCost),
			nullableExactDenominator(actualCost), reservation.CostBound.Currency,
			boolInteger(input.ActualTokens != nil),
			nullableTokenCount(input.ActualTokens), input.ReasonRedacted,
			input.IdempotencyKey, input.ProvenanceJSON, micros,
		)
		if err != nil {
			return repositoryWriteError(
				"record budget reconciliation intent", err,
			)
		}
		current, err := computeBudgetSnapshot(
			ctx, transaction.sql, reservation.BudgetID,
		)
		if err != nil {
			return err
		}
		newRevision, err := advanceBudgetRevision(
			ctx, transaction.sql, reservation.BudgetID,
			current.Revision, micros,
		)
		if err != nil {
			return err
		}
		snapshot = current
		snapshot.Revision = newRevision
		snapshot.CreatedAt = repositoryTime(micros)
		if err := persistBudgetSnapshot(ctx, transaction.sql, snapshot); err != nil {
			return err
		}
		return persistBudgetBoundaryEvents(
			ctx, transaction.sql, snapshot, micros,
		)
	})
	return snapshot, err
}

// RaiseProviderBudget records a monotonic limit change only after verifying a
// granted same-task approval and attributable actor.
func (repositories *Repositories) RaiseProviderBudget(
	ctx context.Context,
	input RaiseProviderBudget,
) (BudgetSnapshot, error) {
	warning, hard, err := validateRaiseProviderBudget(input)
	if err != nil {
		return BudgetSnapshot{}, err
	}
	var snapshot BudgetSnapshot
	err = repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		var (
			existingRevision              uint64
			existingWarning, existingHard ExactMinorCost
			existingCurrencyRaw           string
			existingWarningTokens         domain.TokenCount
			existingHardTokens            domain.TokenCount
			existingApprovalRaw           string
			existingActorKind             string
			existingActorReference        string
			existingReason                string
			existingProvenance            string
		)
		err := transaction.sql.QueryRowContext(
			ctx,
			`SELECT revision,
			        warning_cost_minor_numerator,
			        warning_cost_minor_denominator,
			        hard_cost_minor_numerator,
			        hard_cost_minor_denominator, currency,
			        warning_tokens, hard_tokens, approval_id,
			        actor_kind, actor_reference, reason_redacted,
			        provenance_json
			 FROM budget_limit_revisions
			 WHERE budget_id = ? AND idempotency_key = ?`,
			input.BudgetID, input.IdempotencyKey,
		).Scan(
			&existingRevision,
			&existingWarning.Numerator, &existingWarning.Denominator,
			&existingHard.Numerator, &existingHard.Denominator,
			&existingCurrencyRaw, &existingWarningTokens,
			&existingHardTokens, &existingApprovalRaw,
			&existingActorKind, &existingActorReference, &existingReason,
			&existingProvenance,
		)
		if err == nil {
			existingCurrency, parseErr := domain.ParseCurrencyCode(
				existingCurrencyRaw,
			)
			if parseErr != nil {
				return typedError(
					ErrCorrupt, "read idempotent budget limit raise", parseErr,
				)
			}
			existingWarning.Currency = existingCurrency
			existingHard.Currency = existingCurrency
			if !exactCostEqual(&existingWarning, &warning) ||
				!exactCostEqual(&existingHard, &hard) ||
				existingWarningTokens != input.WarningTokens ||
				existingHardTokens != input.HardTokens ||
				existingApprovalRaw != input.ApprovalID.String() ||
				existingActorKind != input.ActorKind ||
				existingActorReference != input.ActorReference ||
				existingReason != input.ReasonRedacted ||
				existingProvenance != input.ProvenanceJSON {
				return typedError(
					ErrConflict, "raise provider budget",
					errors.New(
						"budget raise idempotency key was reused with different authority or limits",
					),
				)
			}
			return scanLatestBudgetSnapshot(
				ctx, transaction.sql, input.BudgetID, &snapshot,
			)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return classify("find idempotent budget limit raise", err)
		}
		current, err := computeBudgetSnapshot(ctx, transaction.sql, input.BudgetID)
		if err != nil {
			return err
		}
		if current.Revision != input.ExpectedRevision {
			return typedError(
				ErrStaleRevision, "raise provider budget",
				errors.New("budget revision changed"),
			)
		}
		if warning.Currency != current.HardCost.Currency ||
			hard.Currency != current.HardCost.Currency {
			return typedError(
				ErrConstraint, "raise provider budget",
				errors.New("budget currency cannot change"),
			)
		}
		costComparison := compareExactCosts(hard, current.HardCost)
		if costComparison < 0 ||
			input.HardTokens < current.HardTokens ||
			(compareExactCosts(warning, current.WarningCost) < 0) ||
			input.WarningTokens < current.WarningTokens ||
			costComparison == 0 && input.HardTokens == current.HardTokens {
			return typedError(
				ErrConstraint, "raise provider budget",
				errors.New(
					"approved budget raise must monotonically raise at least one hard cap",
				),
			)
		}
		if err := verifyGrantedBudgetApproval(
			ctx, transaction.sql, input.ApprovalID, current.TaskID,
			input.BudgetID, warning, hard, input.WarningTokens, input.HardTokens,
		); err != nil {
			return err
		}
		_, micros := repositories.timestamp()
		limitRevision := current.LimitRevision + 1
		_, err = transaction.sql.ExecContext(
			ctx,
			`INSERT INTO budget_limit_revisions (
				budget_id, revision,
				warning_cost_minor_numerator,
				warning_cost_minor_denominator,
				hard_cost_minor_numerator, hard_cost_minor_denominator,
				currency, warning_tokens, hard_tokens, approval_id,
				authority_kind, actor_kind, actor_reference,
				reason_redacted, idempotency_key, provenance_json,
				created_at_unix_micros
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'user-approval',
			          ?, ?, ?, ?, ?, ?)`,
			input.BudgetID, limitRevision,
			warning.Numerator, warning.Denominator,
			hard.Numerator, hard.Denominator, hard.Currency,
			input.WarningTokens, input.HardTokens, input.ApprovalID,
			input.ActorKind, input.ActorReference, input.ReasonRedacted,
			input.IdempotencyKey, input.ProvenanceJSON, micros,
		)
		if err != nil {
			return repositoryWriteError("raise provider budget", err)
		}
		newRevision, err := advanceBudgetRevision(
			ctx, transaction.sql, input.BudgetID,
			input.ExpectedRevision, micros,
		)
		if err != nil {
			return err
		}
		snapshot, err = computeBudgetSnapshot(
			ctx, transaction.sql, input.BudgetID,
		)
		if err != nil {
			return err
		}
		snapshot.Revision = newRevision
		snapshot.CreatedAt = repositoryTime(micros)
		if err := persistBudgetSnapshot(ctx, transaction.sql, snapshot); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{
			"schema_version": 1,
			"limit_revision": limitRevision,
			"approval_id":    input.ApprovalID.String(),
		})
		return insertBudgetEvent(
			ctx, transaction.sql, snapshot, "limit-raised",
			string(payload), input.IdempotencyKey, micros,
		)
	})
	return snapshot, err
}

// GetBudgetSnapshot returns the latest immutable task budget view.
func (repositories *Repositories) GetBudgetSnapshot(
	ctx context.Context,
	taskID domain.TaskID,
) (BudgetSnapshot, error) {
	if taskID.IsZero() {
		return BudgetSnapshot{}, errors.New("task ID must not be empty")
	}
	var budgetIDRaw string
	if err := repositories.database.sql.QueryRowContext(
		ctx, `SELECT id FROM budgets WHERE task_id = ?`, taskID,
	).Scan(&budgetIDRaw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return BudgetSnapshot{}, typedError(
				ErrNotFound, "get budget snapshot", err,
			)
		}
		return BudgetSnapshot{}, classify("get budget snapshot identity", err)
	}
	budgetID, err := domain.ParseBudgetID(budgetIDRaw)
	if err != nil {
		return BudgetSnapshot{}, typedError(
			ErrCorrupt, "get budget snapshot identity", err,
		)
	}
	var snapshot BudgetSnapshot
	if err := scanLatestBudgetSnapshot(
		ctx, repositories.database.sql, budgetID, &snapshot,
	); err != nil {
		return BudgetSnapshot{}, err
	}
	var currentRevision uint64
	if err := repositories.database.sql.QueryRowContext(
		ctx, `SELECT revision FROM budgets WHERE id = ?`, budgetID,
	).Scan(&currentRevision); err != nil {
		return BudgetSnapshot{}, classify("read current budget revision", err)
	}
	if currentRevision != snapshot.Revision {
		snapshot, err = computeBudgetSnapshot(
			ctx, repositories.database.sql, budgetID,
		)
		if err != nil {
			return BudgetSnapshot{}, err
		}
	}
	categories, err := loadBudgetCategorySnapshots(
		ctx, repositories.database.sql, budgetID, snapshot.WarningCost.Currency,
	)
	if err != nil {
		return BudgetSnapshot{}, err
	}
	snapshot.Categories = categories
	return snapshot, nil
}

type budgetQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type budgetPostingIdentity struct {
	ID                      string
	ReservationID           string
	BudgetID                domain.BudgetID
	ActualCost              *ExactMinorCost
	ActualTokens            *domain.TokenCount
	ActualProviderCallSlots uint32
}

func validateReserveProviderBudget(
	input ReserveProviderBudget,
) (ExactMinorCost, error) {
	switch {
	case input.BudgetID.IsZero():
		return ExactMinorCost{}, errors.New("budget ID must not be empty")
	case !budgetCategoryValid(input.Category):
		return ExactMinorCost{}, errors.New("budget category is invalid")
	case input.Category != BudgetCostModel && input.ProviderCallSlots != 0:
		return ExactMinorCost{}, errors.New(
			"only model reservations may consume provider-call slots",
		)
	case input.RetryOrdinal > math.MaxInt64:
		return ExactMinorCost{}, errors.New("budget retry ordinal exceeds SQLite range")
	}
	for label, value := range map[string]string{
		"budget reservation ID":              input.ID,
		"budget operation ID":                input.OperationID,
		"budget reservation idempotency key": input.IdempotencyKey,
	} {
		if err := validateBounded(label, value, 255); err != nil {
			return ExactMinorCost{}, err
		}
	}
	if input.AttemptID != nil {
		if err := validateBounded(
			"budget physical attempt ID", *input.AttemptID, 255,
		); err != nil {
			return ExactMinorCost{}, err
		}
	}
	if input.TokenBound != nil && uint64(*input.TokenBound) > math.MaxInt64 {
		return ExactMinorCost{}, errors.New(
			"budget token bound exceeds SQLite integer range",
		)
	}
	if err := validateJSONBounded(input.ProvenanceJSON, 65536); err != nil {
		return ExactMinorCost{}, fmt.Errorf("budget reservation provenance: %w", err)
	}
	normalized, err := normalizeExactCost(input.CostBound)
	if err != nil {
		return ExactMinorCost{}, err
	}
	return normalized, nil
}

func normalizedProviderCallSlots(input ReserveProviderBudget) uint32 {
	if input.Category != BudgetCostModel {
		return 0
	}
	if input.ProviderCallSlots == 0 {
		return 1
	}
	return input.ProviderCallSlots
}

func normalizedActualProviderCallSlots(
	reservation BudgetReservation,
	requested uint32,
) (uint32, error) {
	if reservation.Category != BudgetCostModel {
		if requested != 0 {
			return 0, errors.New(
				"only model settlements may record provider-call slots",
			)
		}
		return 0, nil
	}
	if requested == 0 {
		// Compatibility for already-issued callers: absent physical-attempt
		// evidence conservatively retains the complete reserved retry bound.
		return reservation.ProviderCallSlots, nil
	}
	return requested, nil
}

func validateSettleProviderBudget(input SettleProviderBudget) error {
	for label, value := range map[string]string{
		"budget posting ID":              input.ID,
		"budget reservation ID":          input.ReservationID,
		"budget posting idempotency key": input.IdempotencyKey,
	} {
		if err := validateBounded(label, value, 255); err != nil {
			return err
		}
	}
	if input.ActualTokens != nil && uint64(*input.ActualTokens) > math.MaxInt64 {
		return errors.New("actual token usage exceeds SQLite integer range")
	}
	if err := validateJSONBounded(input.ProvenanceJSON, 65536); err != nil {
		return fmt.Errorf("budget posting provenance: %w", err)
	}
	return nil
}

func validateReleaseProviderBudget(input ReleaseProviderBudget) error {
	if err := validateBounded(
		"budget reservation ID", input.ReservationID, 255,
	); err != nil {
		return err
	}
	if err := validateBounded(
		"budget release reason", input.ReasonRedacted, 2048,
	); err != nil {
		return err
	}
	if err := validateJSONBounded(input.ProvenanceJSON, 65536); err != nil {
		return fmt.Errorf("budget release provenance: %w", err)
	}
	return nil
}

func validateBudgetReconciliationIntent(
	input RecordBudgetReconciliationIntent,
) error {
	for label, value := range map[string]string{
		"budget reconciliation intent ID":       input.ID,
		"budget reservation ID":                 input.ReservationID,
		"budget reconciliation reason":          input.ReasonRedacted,
		"budget reconciliation idempotency key": input.IdempotencyKey,
	} {
		maximum := 255
		if label == "budget reconciliation reason" {
			maximum = 2048
		}
		if err := validateBounded(label, value, maximum); err != nil {
			return err
		}
	}
	if input.ActualTokens != nil && uint64(*input.ActualTokens) > math.MaxInt64 {
		return errors.New(
			"budget reconciliation tokens exceed SQLite integer range",
		)
	}
	if err := validateJSONBounded(input.ProvenanceJSON, 65536); err != nil {
		return fmt.Errorf("budget reconciliation provenance: %w", err)
	}
	return nil
}

func validateRaiseProviderBudget(
	input RaiseProviderBudget,
) (ExactMinorCost, ExactMinorCost, error) {
	switch {
	case input.BudgetID.IsZero():
		return ExactMinorCost{}, ExactMinorCost{}, errors.New(
			"budget ID must not be empty",
		)
	case input.ApprovalID.IsZero():
		return ExactMinorCost{}, ExactMinorCost{}, typedError(
			ErrBudgetApprovalRequired, "raise provider budget",
			errors.New("approval ID must not be empty"),
		)
	case input.WarningTokens > input.HardTokens:
		return ExactMinorCost{}, ExactMinorCost{}, errors.New(
			"budget token warning exceeds hard cap",
		)
	case uint64(input.HardTokens) > math.MaxInt64:
		return ExactMinorCost{}, ExactMinorCost{}, errors.New(
			"budget hard token cap exceeds SQLite range",
		)
	case input.ActorKind != "user" && input.ActorKind != "coordinator":
		return ExactMinorCost{}, ExactMinorCost{}, errors.New(
			"budget raise actor kind is invalid",
		)
	}
	for label, value := range map[string]string{
		"budget raise actor":           input.ActorReference,
		"budget raise reason":          input.ReasonRedacted,
		"budget raise idempotency key": input.IdempotencyKey,
	} {
		maximum := 255
		if label == "budget raise reason" {
			maximum = 2048
		}
		if err := validateBounded(label, value, maximum); err != nil {
			return ExactMinorCost{}, ExactMinorCost{}, err
		}
	}
	if err := validateJSONBounded(input.ProvenanceJSON, 65536); err != nil {
		return ExactMinorCost{}, ExactMinorCost{}, fmt.Errorf(
			"budget raise provenance: %w", err,
		)
	}
	warning, err := normalizeExactCost(input.WarningCost)
	if err != nil {
		return ExactMinorCost{}, ExactMinorCost{}, err
	}
	hard, err := normalizeExactCost(input.HardCost)
	if err != nil {
		return ExactMinorCost{}, ExactMinorCost{}, err
	}
	if warning.Currency != hard.Currency ||
		compareExactCosts(warning, hard) > 0 {
		return ExactMinorCost{}, ExactMinorCost{}, errors.New(
			"budget cost warning must not exceed same-currency hard cap",
		)
	}
	return warning, hard, nil
}

func budgetCategoryValid(category BudgetCostCategory) bool {
	switch category {
	case BudgetCostModel, BudgetCostTool, BudgetCostInfrastructure:
		return true
	default:
		return false
	}
}

func findBudgetReservationByIdentity(
	ctx context.Context,
	queryer budgetQueryer,
	budgetID domain.BudgetID,
	id string,
	idempotencyKey string,
) (BudgetReservation, bool, error) {
	reservation, err := scanBudgetReservation(queryer.QueryRowContext(
		ctx,
		budgetReservationSelect+
			` WHERE budget_id = ? AND (id = ? OR idempotency_key = ?)
			  ORDER BY CASE WHEN id = ? THEN 0 ELSE 1 END LIMIT 1`,
		budgetID, id, idempotencyKey, id,
	), "find budget reservation")
	if errors.Is(err, ErrNotFound) {
		return BudgetReservation{}, false, nil
	}
	return reservation, err == nil, err
}

func getBudgetReservation(
	ctx context.Context,
	queryer budgetQueryer,
	id string,
) (BudgetReservation, error) {
	return scanBudgetReservation(queryer.QueryRowContext(
		ctx, budgetReservationSelect+` WHERE id = ?`, id,
	), "get budget reservation")
}

const budgetReservationSelect = `SELECT
	id, budget_id, task_id, operation_id, attempt_id, retry_ordinal,
	category, provider_call_slots, state, cost_bound_minor_numerator,
	cost_bound_minor_denominator, currency, token_bound_known,
	token_bound, idempotency_key, provenance_json,
	settlement_reason_redacted, settlement_provenance_json,
	created_at_unix_micros,
	settled_at_unix_micros, revision
	FROM budget_reservations`

func scanBudgetReservation(
	row rowScanner,
	operation string,
) (BudgetReservation, error) {
	var (
		reservation                               BudgetReservation
		budgetIDRaw, taskIDRaw, currencyRaw       string
		attempt, settlement, settlementProvenance sql.NullString
		tokenKnown                                int
		tokenBound                                sql.NullInt64
		createdMicros                             int64
		settledMicros                             sql.NullInt64
	)
	err := row.Scan(
		&reservation.ID, &budgetIDRaw, &taskIDRaw, &reservation.OperationID,
		&attempt, &reservation.RetryOrdinal, &reservation.Category,
		&reservation.ProviderCallSlots, &reservation.State,
		&reservation.CostBound.Numerator,
		&reservation.CostBound.Denominator, &currencyRaw, &tokenKnown,
		&tokenBound, &reservation.IdempotencyKey,
		&reservation.ProvenanceJSON, &settlement, &settlementProvenance,
		&createdMicros,
		&settledMicros, &reservation.Revision,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return BudgetReservation{}, typedError(ErrNotFound, operation, err)
	}
	if err != nil {
		return BudgetReservation{}, classify(operation, err)
	}
	budgetID, err := domain.ParseBudgetID(budgetIDRaw)
	if err != nil {
		return BudgetReservation{}, typedError(ErrCorrupt, operation, err)
	}
	taskID, err := domain.ParseTaskID(taskIDRaw)
	if err != nil {
		return BudgetReservation{}, typedError(ErrCorrupt, operation, err)
	}
	currency, err := domain.ParseCurrencyCode(currencyRaw)
	if err != nil {
		return BudgetReservation{}, typedError(ErrCorrupt, operation, err)
	}
	reservation.BudgetID = budgetID
	reservation.TaskID = taskID
	reservation.CostBound.Currency = currency
	reservation.AttemptID = nullStringPointer(attempt)
	reservation.Settlement = nullStringPointer(settlement)
	reservation.SettlementProvenanceJSON =
		nullStringPointer(settlementProvenance)
	if tokenKnown != 0 {
		value := domain.TokenCount(tokenBound.Int64)
		reservation.TokenBound = &value
	}
	reservation.CreatedAt = repositoryTime(createdMicros)
	if settledMicros.Valid {
		value := repositoryTime(settledMicros.Int64)
		reservation.SettledAt = &value
	}
	return reservation, nil
}

func budgetReservationMatches(
	existing BudgetReservation,
	input ReserveProviderBudget,
	normalized ExactMinorCost,
) bool {
	return existing.BudgetID == input.BudgetID &&
		existing.OperationID == input.OperationID &&
		equalOptionalString(existing.AttemptID, input.AttemptID) &&
		existing.RetryOrdinal == input.RetryOrdinal &&
		existing.Category == input.Category &&
		existing.ProviderCallSlots == normalizedProviderCallSlots(input) &&
		exactCostEqual(&existing.CostBound, &normalized) &&
		sameTokenCount(existing.TokenBound, input.TokenBound) &&
		existing.IdempotencyKey == input.IdempotencyKey &&
		existing.ProvenanceJSON == input.ProvenanceJSON
}

func findBudgetPostingByIdentity(
	ctx context.Context,
	queryer budgetQueryer,
	budgetID domain.BudgetID,
	id string,
	idempotencyKey string,
) (budgetPostingIdentity, bool, error) {
	var (
		posting                        budgetPostingIdentity
		budgetIDRaw, currencyRaw       string
		costKnown, tokensKnown         int
		costNumerator, costDenominator sql.NullInt64
		actualTokens                   sql.NullInt64
	)
	err := queryer.QueryRowContext(
		ctx,
		`SELECT id, reservation_id, budget_id, actual_provider_call_slots,
		        cost_known,
		        cost_minor_numerator, cost_minor_denominator, currency,
		        tokens_known, actual_tokens
		 FROM budget_usage_postings
		 WHERE budget_id = ? AND (id = ? OR idempotency_key = ?)
		 ORDER BY CASE WHEN id = ? THEN 0 ELSE 1 END LIMIT 1`,
		budgetID, id, idempotencyKey, id,
	).Scan(
		&posting.ID, &posting.ReservationID, &budgetIDRaw,
		&posting.ActualProviderCallSlots, &costKnown,
		&costNumerator, &costDenominator, &currencyRaw,
		&tokensKnown, &actualTokens,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return budgetPostingIdentity{}, false, nil
	}
	if err != nil {
		return budgetPostingIdentity{}, false, classify(
			"find budget posting", err,
		)
	}
	parsedBudgetID, err := domain.ParseBudgetID(budgetIDRaw)
	if err != nil {
		return budgetPostingIdentity{}, false, typedError(
			ErrCorrupt, "find budget posting", err,
		)
	}
	posting.BudgetID = parsedBudgetID
	if costKnown != 0 {
		currency, err := domain.ParseCurrencyCode(currencyRaw)
		if err != nil {
			return budgetPostingIdentity{}, false, typedError(
				ErrCorrupt, "find budget posting", err,
			)
		}
		posting.ActualCost = &ExactMinorCost{
			Numerator: costNumerator.Int64, Denominator: costDenominator.Int64,
			Currency: currency,
		}
	}
	if tokensKnown != 0 {
		value := domain.TokenCount(actualTokens.Int64)
		posting.ActualTokens = &value
	}
	return posting, true, nil
}

func computeBudgetSnapshot(
	ctx context.Context,
	queryer budgetQueryer,
	budgetID domain.BudgetID,
) (BudgetSnapshot, error) {
	var (
		snapshot           BudgetSnapshot
		taskIDRaw          string
		limitCurrencyRaw   string
		warningNumerator   int64
		warningDenominator int64
		hardNumerator      int64
		hardDenominator    int64
		updatedMicros      int64
	)
	err := queryer.QueryRowContext(
		ctx,
		`SELECT budget.task_id, budget.revision, limits.revision,
		        limits.warning_cost_minor_numerator,
		        limits.warning_cost_minor_denominator,
		        limits.hard_cost_minor_numerator,
		        limits.hard_cost_minor_denominator,
		        limits.currency, limits.warning_tokens, limits.hard_tokens,
		        budget.updated_at_unix_micros
		 FROM budgets AS budget
		 JOIN budget_limit_revisions AS limits
		   ON limits.budget_id = budget.id
		  AND limits.revision = (
		      SELECT max(latest.revision)
		      FROM budget_limit_revisions AS latest
		      WHERE latest.budget_id = budget.id
		  )
		 WHERE budget.id = ?`,
		budgetID,
	).Scan(
		&taskIDRaw, &snapshot.Revision, &snapshot.LimitRevision,
		&warningNumerator, &warningDenominator,
		&hardNumerator, &hardDenominator, &limitCurrencyRaw,
		&snapshot.WarningTokens, &snapshot.HardTokens, &updatedMicros,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return BudgetSnapshot{}, typedError(
			ErrNotFound, "compute budget snapshot", err,
		)
	}
	if err != nil {
		return BudgetSnapshot{}, classify("compute budget snapshot", err)
	}
	taskID, err := domain.ParseTaskID(taskIDRaw)
	if err != nil {
		return BudgetSnapshot{}, typedError(
			ErrCorrupt, "compute budget snapshot", err,
		)
	}
	currency, err := domain.ParseCurrencyCode(limitCurrencyRaw)
	if err != nil {
		return BudgetSnapshot{}, typedError(
			ErrCorrupt, "compute budget snapshot", err,
		)
	}
	snapshot.BudgetID = budgetID
	snapshot.TaskID = taskID
	snapshot.WarningCost = ExactMinorCost{
		Numerator: warningNumerator, Denominator: warningDenominator,
		Currency: currency,
	}
	snapshot.HardCost = ExactMinorCost{
		Numerator: hardNumerator, Denominator: hardDenominator,
		Currency: currency,
	}
	snapshot.CreatedAt = repositoryTime(updatedMicros)
	categories, err := loadBudgetCategorySnapshots(
		ctx, queryer, budgetID, currency,
	)
	if err != nil {
		return BudgetSnapshot{}, err
	}
	snapshot.Categories = categories
	snapshot.ReservedCost = zeroExactCost(currency)
	snapshot.ChargedCost = zeroExactCost(currency)
	snapshot.ActualKnownCost = zeroExactCost(currency)
	for _, category := range categories {
		if math.MaxUint64-snapshot.ProviderCallSlots <
			category.ProviderCallSlots {
			return BudgetSnapshot{}, errors.New(
				"budget provider-call aggregate overflow",
			)
		}
		snapshot.ProviderCallSlots += category.ProviderCallSlots
		snapshot.ReservedCost, err = addExactCosts(
			snapshot.ReservedCost, category.ReservedCost,
		)
		if err != nil {
			return BudgetSnapshot{}, err
		}
		snapshot.ChargedCost, err = addExactCosts(
			snapshot.ChargedCost, category.ChargedCost,
		)
		if err != nil {
			return BudgetSnapshot{}, err
		}
		snapshot.ActualKnownCost, err = addExactCosts(
			snapshot.ActualKnownCost, category.ActualKnownCost,
		)
		if err != nil {
			return BudgetSnapshot{}, err
		}
		snapshot.CostAccountingUnknown =
			snapshot.CostAccountingUnknown || category.CostUnknown
		snapshot.TokenAccountingUnknown =
			snapshot.TokenAccountingUnknown || category.TokensUnknown
		snapshot.ReconciliationPending =
			snapshot.ReconciliationPending || category.ReconciliationPending
		if uint64(snapshot.ReservedTokens) >
			math.MaxUint64-uint64(category.ReservedTokens) ||
			uint64(snapshot.ChargedTokens) >
				math.MaxUint64-uint64(category.ChargedTokens) ||
			uint64(snapshot.ActualTokens) >
				math.MaxUint64-uint64(category.ActualTokens) {
			return BudgetSnapshot{}, errors.New(
				"budget token aggregate overflow",
			)
		}
		snapshot.ReservedTokens += category.ReservedTokens
		snapshot.ChargedTokens += category.ChargedTokens
		snapshot.ActualTokens += category.ActualTokens
	}
	exposure, err := addExactCosts(snapshot.ReservedCost, snapshot.ChargedCost)
	if err != nil {
		return BudgetSnapshot{}, err
	}
	snapshot.WarningReached =
		compareExactCosts(exposure, snapshot.WarningCost) >= 0 ||
			snapshot.ReservedTokens+snapshot.ChargedTokens >= snapshot.WarningTokens
	snapshot.HardCapReached =
		snapshot.ReconciliationPending ||
			snapshot.CostAccountingUnknown ||
			snapshot.TokenAccountingUnknown ||
			compareExactCosts(exposure, snapshot.HardCost) >= 0 ||
			snapshot.ReservedTokens+snapshot.ChargedTokens >= snapshot.HardTokens
	if !snapshot.CostAccountingUnknown {
		remaining := subtractExactCostsFloorZero(snapshot.HardCost, exposure)
		snapshot.RemainingCost = &remaining
	}
	if !snapshot.TokenAccountingUnknown {
		remaining := domain.TokenCount(0)
		exposed := snapshot.ReservedTokens + snapshot.ChargedTokens
		if exposed < snapshot.HardTokens {
			remaining = snapshot.HardTokens - exposed
		}
		snapshot.RemainingTokens = &remaining
	}
	return snapshot, nil
}

func loadBudgetCategorySnapshots(
	ctx context.Context,
	queryer budgetQueryer,
	budgetID domain.BudgetID,
	currency domain.CurrencyCode,
) ([]BudgetCategorySnapshot, error) {
	categories := []BudgetCategorySnapshot{
		{Category: BudgetCostModel},
		{Category: BudgetCostTool},
		{Category: BudgetCostInfrastructure},
	}
	index := map[BudgetCostCategory]int{
		BudgetCostModel: 0, BudgetCostTool: 1, BudgetCostInfrastructure: 2,
	}
	for categoryIndex := range categories {
		categories[categoryIndex].ReservedCost = zeroExactCost(currency)
		categories[categoryIndex].ChargedCost = zeroExactCost(currency)
		categories[categoryIndex].ActualKnownCost = zeroExactCost(currency)
	}
	var legacyReserved, legacyActual, legacyTokens int64
	if err := queryer.QueryRowContext(
		ctx,
		`SELECT reserved_cost_minor, actual_cost_minor, actual_tokens
		 FROM budgets WHERE id = ?`,
		budgetID,
	).Scan(&legacyReserved, &legacyActual, &legacyTokens); err != nil {
		return nil, classify("read legacy budget aggregate", err)
	}
	legacy := &categories[index[BudgetCostInfrastructure]]
	legacy.ReservedCost = ExactMinorCost{
		Numerator: legacyReserved, Denominator: 1, Currency: currency,
	}
	legacy.ChargedCost = ExactMinorCost{
		Numerator: legacyActual, Denominator: 1, Currency: currency,
	}
	legacy.ActualKnownCost = legacy.ChargedCost
	legacy.ChargedTokens = domain.TokenCount(legacyTokens)
	legacy.ActualTokens = domain.TokenCount(legacyTokens)
	rows, err := queryer.QueryContext(
		ctx,
		`SELECT category, provider_call_slots, cost_bound_minor_numerator,
		        cost_bound_minor_denominator, token_bound_known, token_bound
		 FROM budget_reservations
		 WHERE budget_id = ? AND state = 'active'
		   AND NOT EXISTS (
		       SELECT 1 FROM budget_reconciliation_intents AS intent
		       WHERE intent.reservation_id = budget_reservations.id
		         AND NOT EXISTS (
		             SELECT 1 FROM budget_usage_postings AS posting
		             WHERE posting.reservation_id = intent.reservation_id
		         )
		   )
		 ORDER BY created_at_unix_micros, id`,
		budgetID,
	)
	if err != nil {
		return nil, classify("read active budget reservations", err)
	}
	for rows.Next() {
		var (
			category          BudgetCostCategory
			providerCallSlots uint64
			cost              ExactMinorCost
			tokenKnown        int
			tokens            sql.NullInt64
		)
		if err := rows.Scan(
			&category, &providerCallSlots,
			&cost.Numerator, &cost.Denominator,
			&tokenKnown, &tokens,
		); err != nil {
			rows.Close()
			return nil, classify("scan active budget reservation", err)
		}
		cost.Currency = currency
		position, found := index[category]
		if !found {
			rows.Close()
			return nil, typedError(
				ErrCorrupt, "scan active budget reservation",
				errors.New("unknown budget category"),
			)
		}
		item := &categories[position]
		item.ReservationCount++
		item.ProviderCallSlots += providerCallSlots
		item.ReservedCost, err = addExactCosts(item.ReservedCost, cost)
		if err != nil {
			rows.Close()
			return nil, err
		}
		if tokenKnown != 0 {
			item.ReservedTokens += domain.TokenCount(tokens.Int64)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, classify("iterate active budget reservations", err)
	}
	rows.Close()
	pendingRows, err := queryer.QueryContext(
		ctx,
		`SELECT reservation.category,
		        coalesce(nullif(intent.actual_provider_call_slots, 0),
		                 reservation.provider_call_slots),
		        reservation.cost_bound_minor_numerator,
		        reservation.cost_bound_minor_denominator,
		        reservation.token_bound_known, reservation.token_bound,
		        intent.cost_known, intent.cost_minor_numerator,
		        intent.cost_minor_denominator, intent.tokens_known,
		        intent.actual_tokens
		 FROM budget_reconciliation_intents AS intent
		 JOIN budget_reservations AS reservation
		   ON reservation.id = intent.reservation_id
		 WHERE intent.budget_id = ?
		   AND NOT EXISTS (
		       SELECT 1 FROM budget_usage_postings AS posting
		       WHERE posting.reservation_id = intent.reservation_id
		   )
		 ORDER BY intent.created_at_unix_micros, intent.id`,
		budgetID,
	)
	if err != nil {
		return nil, classify("read pending budget reconciliation", err)
	}
	for pendingRows.Next() {
		var (
			category                   BudgetCostCategory
			providerCallSlots          uint64
			bound                      ExactMinorCost
			tokenBoundKnown, costKnown int
			tokenBound                 sql.NullInt64
			actualNumerator            sql.NullInt64
			actualDenominator          sql.NullInt64
			tokensKnown                int
			actualTokens               sql.NullInt64
		)
		if err := pendingRows.Scan(
			&category, &providerCallSlots,
			&bound.Numerator, &bound.Denominator,
			&tokenBoundKnown, &tokenBound, &costKnown,
			&actualNumerator, &actualDenominator,
			&tokensKnown, &actualTokens,
		); err != nil {
			pendingRows.Close()
			return nil, classify("scan pending budget reconciliation", err)
		}
		position, found := index[category]
		if !found {
			pendingRows.Close()
			return nil, typedError(
				ErrCorrupt, "scan pending budget reconciliation",
				errors.New("unknown budget category"),
			)
		}
		item := &categories[position]
		item.ReservationCount++
		item.ProviderCallSlots += providerCallSlots
		item.ReconciliationPending = true
		charged := bound
		charged.Currency = currency
		if costKnown != 0 {
			charged = ExactMinorCost{
				Numerator:   actualNumerator.Int64,
				Denominator: actualDenominator.Int64,
				Currency:    currency,
			}
			item.ActualKnownCost, err = addExactCosts(
				item.ActualKnownCost, charged,
			)
			if err != nil {
				pendingRows.Close()
				return nil, err
			}
		} else {
			item.CostUnknown = true
		}
		item.ChargedCost, err = addExactCosts(item.ChargedCost, charged)
		if err != nil {
			pendingRows.Close()
			return nil, err
		}
		chargedTokenCount := domain.TokenCount(0)
		if tokenBoundKnown != 0 {
			chargedTokenCount = domain.TokenCount(tokenBound.Int64)
		}
		if tokensKnown != 0 {
			chargedTokenCount = domain.TokenCount(actualTokens.Int64)
			item.ActualTokens += chargedTokenCount
		} else {
			item.TokensUnknown = true
		}
		item.ChargedTokens += chargedTokenCount
	}
	if err := pendingRows.Err(); err != nil {
		pendingRows.Close()
		return nil, classify("iterate pending budget reconciliation", err)
	}
	pendingRows.Close()
	postings, err := queryer.QueryContext(
		ctx,
		`SELECT posting.category,
		        coalesce(nullif(posting.actual_provider_call_slots, 0),
		                 reservation.provider_call_slots),
		        posting.cost_known, posting.cost_minor_numerator,
		        cost_minor_denominator, charged_cost_minor_numerator,
		        charged_cost_minor_denominator, tokens_known,
		        actual_tokens, charged_tokens
		 FROM budget_usage_postings AS posting
		 JOIN budget_reservations AS reservation
		   ON reservation.id = posting.reservation_id
		 WHERE posting.budget_id = ?
		 ORDER BY posting.created_at_unix_micros, posting.id`,
		budgetID,
	)
	if err != nil {
		return nil, classify("read budget usage postings", err)
	}
	defer postings.Close()
	for postings.Next() {
		var (
			category                           BudgetCostCategory
			providerCallSlots                  uint64
			costKnown, tokensKnown             int
			actualNumerator, actualDenominator sql.NullInt64
			charged                            ExactMinorCost
			actualTokens, chargedTokens        sql.NullInt64
		)
		if err := postings.Scan(
			&category, &providerCallSlots, &costKnown,
			&actualNumerator, &actualDenominator,
			&charged.Numerator, &charged.Denominator,
			&tokensKnown, &actualTokens, &chargedTokens,
		); err != nil {
			return nil, classify("scan budget usage posting", err)
		}
		position, found := index[category]
		if !found {
			return nil, typedError(
				ErrCorrupt, "scan budget usage posting",
				errors.New("unknown budget category"),
			)
		}
		item := &categories[position]
		item.ReservationCount++
		item.ProviderCallSlots += providerCallSlots
		charged.Currency = currency
		item.ChargedCost, err = addExactCosts(item.ChargedCost, charged)
		if err != nil {
			return nil, err
		}
		if costKnown != 0 {
			actual := ExactMinorCost{
				Numerator:   actualNumerator.Int64,
				Denominator: actualDenominator.Int64,
				Currency:    currency,
			}
			item.ActualKnownCost, err = addExactCosts(
				item.ActualKnownCost, actual,
			)
			if err != nil {
				return nil, err
			}
		} else {
			item.CostUnknown = true
		}
		if chargedTokens.Valid {
			item.ChargedTokens += domain.TokenCount(chargedTokens.Int64)
		}
		if tokensKnown != 0 {
			item.ActualTokens += domain.TokenCount(actualTokens.Int64)
		} else {
			item.TokensUnknown = true
		}
	}
	if err := postings.Err(); err != nil {
		return nil, classify("iterate budget usage postings", err)
	}
	return categories, nil
}

func verifyBudgetOperationLimit(
	ctx context.Context,
	queryer budgetQueryer,
	budgetID domain.BudgetID,
	category BudgetCostCategory,
	providerCallSlots uint32,
) error {
	var maximum int64
	column := ""
	switch category {
	case BudgetCostModel:
		column = "maximum_provider_calls"
	case BudgetCostTool:
		column = "maximum_tool_executions"
	default:
		return nil
	}
	if err := queryer.QueryRowContext(
		ctx, `SELECT `+column+` FROM budgets WHERE id = ?`, budgetID,
	).Scan(&maximum); err != nil {
		return classify("read budget operation limit", err)
	}
	var used int64
	aggregate := "count(*)"
	increment := int64(1)
	from := `budget_reservations`
	if category == BudgetCostModel {
		aggregate = `coalesce(sum(
			CASE
			WHEN budget_reservations.state = 'active'
				THEN budget_reservations.provider_call_slots
			ELSE coalesce(
				nullif(budget_usage_postings.actual_provider_call_slots, 0),
				nullif(budget_reconciliation_intents.actual_provider_call_slots, 0),
				budget_reservations.provider_call_slots
			)
			END
		), 0)`
		from = `budget_reservations
			LEFT JOIN budget_usage_postings
			  ON budget_usage_postings.reservation_id = budget_reservations.id
			LEFT JOIN budget_reconciliation_intents
			  ON budget_reconciliation_intents.reservation_id = budget_reservations.id`
		increment = int64(providerCallSlots)
	}
	if err := queryer.QueryRowContext(
		ctx,
		`SELECT `+aggregate+` FROM `+from+`
		 WHERE budget_reservations.budget_id = ?
		   AND budget_reservations.category = ?
		   AND budget_reservations.state != 'released'`,
		budgetID, category,
	).Scan(&used); err != nil {
		return classify("read budget operation consumption", err)
	}
	if used > maximum-increment {
		return typedError(
			ErrBudgetExhausted, "reserve provider budget",
			errors.New("budget operation-count hard cap reached"),
		)
	}
	return nil
}

func verifyGrantedBudgetApproval(
	ctx context.Context,
	queryer budgetQueryer,
	approvalID domain.ApprovalID,
	taskID domain.TaskID,
	budgetID domain.BudgetID,
	warningCost ExactMinorCost,
	hardCost ExactMinorCost,
	warningTokens domain.TokenCount,
	hardTokens domain.TokenCount,
) error {
	var state, scope string
	var approvalTaskID string
	if err := queryer.QueryRowContext(
		ctx,
		`SELECT task_id, state, scope FROM approvals WHERE id = ?`,
		approvalID,
	).Scan(&approvalTaskID, &state, &scope); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return typedError(
				ErrBudgetApprovalRequired, "raise provider budget", err,
			)
		}
		return classify("verify budget raise approval", err)
	}
	requiredScope := BudgetRaiseApprovalScope(
		budgetID, warningCost, hardCost, warningTokens, hardTokens,
	)
	if approvalTaskID != taskID.String() || state != "granted" ||
		scope != requiredScope {
		return typedError(
			ErrBudgetApprovalRequired, "raise provider budget",
			errors.New(
				"approval is not granted for this task, budget, and exact limit revision",
			),
		)
	}
	return nil
}

// BudgetRaiseApprovalScope is the canonical immutable approval scope for one
// exact proposed limit revision.
func BudgetRaiseApprovalScope(
	budgetID domain.BudgetID,
	warningCost ExactMinorCost,
	hardCost ExactMinorCost,
	warningTokens domain.TokenCount,
	hardTokens domain.TokenCount,
) string {
	return fmt.Sprintf(
		"budget.raise:%s:%s:%d/%d:%d/%d:%d:%d",
		budgetID.String(), hardCost.Currency,
		warningCost.Numerator, warningCost.Denominator,
		hardCost.Numerator, hardCost.Denominator,
		warningTokens, hardTokens,
	)
}

func advanceBudgetRevision(
	ctx context.Context,
	transaction *sql.Tx,
	budgetID domain.BudgetID,
	expected uint64,
	micros int64,
) (uint64, error) {
	result, err := transaction.ExecContext(
		ctx,
		`UPDATE budgets SET revision = revision + 1,
		        updated_at_unix_micros = ?
		 WHERE id = ? AND revision = ?`,
		micros, budgetID, expected,
	)
	if err != nil {
		return 0, repositoryWriteError("advance budget revision", err)
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return 0, typedError(
			ErrStaleRevision, "advance budget revision",
			errors.New("budget revision changed"),
		)
	}
	return expected + 1, nil
}

func persistBudgetSnapshot(
	ctx context.Context,
	transaction *sql.Tx,
	snapshot BudgetSnapshot,
) error {
	_, err := transaction.ExecContext(
		ctx,
		`INSERT INTO budget_snapshots (
			budget_id, revision, task_id, limit_revision, currency,
			reserved_cost_minor_numerator,
			reserved_cost_minor_denominator,
			charged_cost_minor_numerator,
			charged_cost_minor_denominator, actual_cost_known,
			actual_cost_minor_numerator, actual_cost_minor_denominator,
			cost_accounting_unknown, reserved_tokens, charged_tokens,
			actual_tokens_known, actual_tokens, token_accounting_unknown,
			model_reservations, tool_reservations,
			infrastructure_reservations, provider_call_slots, warning_reached,
			reconciliation_pending, hard_cap_reached, created_at_unix_micros
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
		          ?, ?, ?, ?, ?, ?, ?)`,
		snapshot.BudgetID, snapshot.Revision, snapshot.TaskID,
		snapshot.LimitRevision, snapshot.HardCost.Currency,
		snapshot.ReservedCost.Numerator, snapshot.ReservedCost.Denominator,
		snapshot.ChargedCost.Numerator, snapshot.ChargedCost.Denominator,
		boolInteger(!snapshot.CostAccountingUnknown),
		snapshot.ActualKnownCost.Numerator,
		snapshot.ActualKnownCost.Denominator,
		boolInteger(snapshot.CostAccountingUnknown),
		snapshot.ReservedTokens, snapshot.ChargedTokens,
		boolInteger(!snapshot.TokenAccountingUnknown),
		snapshot.ActualTokens,
		boolInteger(snapshot.TokenAccountingUnknown),
		categoryReservationCount(snapshot.Categories, BudgetCostModel),
		categoryReservationCount(snapshot.Categories, BudgetCostTool),
		categoryReservationCount(snapshot.Categories, BudgetCostInfrastructure),
		snapshot.ProviderCallSlots,
		boolInteger(snapshot.WarningReached),
		boolInteger(snapshot.ReconciliationPending),
		boolInteger(snapshot.HardCapReached),
		snapshot.CreatedAt.UnixMicro(),
	)
	if err != nil {
		return repositoryWriteError("persist budget snapshot", err)
	}
	return nil
}

func scanLatestBudgetSnapshot(
	ctx context.Context,
	queryer budgetQueryer,
	budgetID domain.BudgetID,
	output *BudgetSnapshot,
) error {
	var (
		taskIDRaw, currencyRaw string
		actualKnown            int
		costUnknown            int
		tokensKnown            int
		tokensUnknown          int
		warningReached         int
		reconciliationPending  int
		hardReached            int
		createdMicros          int64
	)
	err := queryer.QueryRowContext(
		ctx,
		`SELECT snapshot.task_id, snapshot.revision,
		        snapshot.limit_revision, snapshot.currency,
		        limits.warning_cost_minor_numerator,
		        limits.warning_cost_minor_denominator,
		        limits.hard_cost_minor_numerator,
		        limits.hard_cost_minor_denominator,
		        limits.warning_tokens, limits.hard_tokens,
		        snapshot.reserved_cost_minor_numerator,
		        snapshot.reserved_cost_minor_denominator,
		        snapshot.charged_cost_minor_numerator,
		        snapshot.charged_cost_minor_denominator,
		        snapshot.actual_cost_known,
		        snapshot.actual_cost_minor_numerator,
		        snapshot.actual_cost_minor_denominator,
		        snapshot.cost_accounting_unknown,
		        snapshot.reserved_tokens, snapshot.charged_tokens,
		        snapshot.actual_tokens_known, snapshot.actual_tokens,
		        snapshot.token_accounting_unknown,
		        snapshot.warning_reached, snapshot.reconciliation_pending,
		        snapshot.hard_cap_reached,
		        snapshot.created_at_unix_micros
		 FROM budget_snapshots AS snapshot
		 JOIN budget_limit_revisions AS limits
		   ON limits.budget_id = snapshot.budget_id
		  AND limits.revision = snapshot.limit_revision
		 WHERE snapshot.budget_id = ?
		 ORDER BY snapshot.revision DESC LIMIT 1`,
		budgetID,
	).Scan(
		&taskIDRaw, &output.Revision, &output.LimitRevision, &currencyRaw,
		&output.WarningCost.Numerator, &output.WarningCost.Denominator,
		&output.HardCost.Numerator, &output.HardCost.Denominator,
		&output.WarningTokens, &output.HardTokens,
		&output.ReservedCost.Numerator, &output.ReservedCost.Denominator,
		&output.ChargedCost.Numerator, &output.ChargedCost.Denominator,
		&actualKnown, &output.ActualKnownCost.Numerator,
		&output.ActualKnownCost.Denominator, &costUnknown,
		&output.ReservedTokens, &output.ChargedTokens,
		&tokensKnown, &output.ActualTokens, &tokensUnknown,
		&warningReached, &reconciliationPending, &hardReached, &createdMicros,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return typedError(ErrNotFound, "read budget snapshot", err)
	}
	if err != nil {
		return classify("read budget snapshot", err)
	}
	taskID, err := domain.ParseTaskID(taskIDRaw)
	if err != nil {
		return typedError(ErrCorrupt, "read budget snapshot", err)
	}
	currency, err := domain.ParseCurrencyCode(currencyRaw)
	if err != nil {
		return typedError(ErrCorrupt, "read budget snapshot", err)
	}
	output.BudgetID = budgetID
	output.TaskID = taskID
	output.WarningCost.Currency = currency
	output.HardCost.Currency = currency
	output.ReservedCost.Currency = currency
	output.ChargedCost.Currency = currency
	output.ActualKnownCost.Currency = currency
	output.CostAccountingUnknown = costUnknown != 0 || actualKnown == 0
	output.TokenAccountingUnknown = tokensUnknown != 0 || tokensKnown == 0
	output.WarningReached = warningReached != 0
	output.ReconciliationPending = reconciliationPending != 0
	output.HardCapReached = hardReached != 0
	output.CreatedAt = repositoryTime(createdMicros)
	exposure, err := addExactCosts(output.ReservedCost, output.ChargedCost)
	if err != nil {
		return err
	}
	if !output.CostAccountingUnknown {
		remaining := subtractExactCostsFloorZero(output.HardCost, exposure)
		output.RemainingCost = &remaining
	}
	if !output.TokenAccountingUnknown {
		remaining := domain.TokenCount(0)
		exposed := output.ReservedTokens + output.ChargedTokens
		if exposed < output.HardTokens {
			remaining = output.HardTokens - exposed
		}
		output.RemainingTokens = &remaining
	}
	categories, err := loadBudgetCategorySnapshots(
		ctx, queryer, budgetID, currency,
	)
	if err != nil {
		return err
	}
	output.Categories = categories
	for _, category := range categories {
		output.ProviderCallSlots += category.ProviderCallSlots
	}
	return nil
}

func persistBudgetBoundaryEvents(
	ctx context.Context,
	transaction *sql.Tx,
	snapshot BudgetSnapshot,
	micros int64,
) error {
	type boundary struct {
		reached bool
		kind    string
	}
	exposure, err := addExactCosts(snapshot.ReservedCost, snapshot.ChargedCost)
	if err != nil {
		return err
	}
	tokenExposure := snapshot.ReservedTokens + snapshot.ChargedTokens
	boundaries := []boundary{
		{compareExactCosts(exposure, snapshot.WarningCost) >= 0, "warning-cost"},
		{tokenExposure >= snapshot.WarningTokens, "warning-tokens"},
		{compareExactCosts(exposure, snapshot.HardCost) >= 0, "hard-cap-cost"},
		{tokenExposure >= snapshot.HardTokens, "hard-cap-tokens"},
		{snapshot.CostAccountingUnknown || snapshot.TokenAccountingUnknown,
			"accounting-unknown"},
		{snapshot.ReconciliationPending, "reconciliation-pending"},
	}
	for _, item := range boundaries {
		if !item.reached {
			continue
		}
		payload, _ := json.Marshal(map[string]any{
			"schema_version":  1,
			"budget_id":       snapshot.BudgetID.String(),
			"budget_revision": snapshot.Revision,
			"limit_revision":  snapshot.LimitRevision,
			"boundary":        item.kind,
		})
		key := fmt.Sprintf(
			"budget-boundary-%d-%s", snapshot.LimitRevision, item.kind,
		)
		if err := insertBudgetEvent(
			ctx, transaction, snapshot, item.kind, string(payload), key, micros,
		); err != nil {
			return err
		}
	}
	return nil
}

func insertBudgetEvent(
	ctx context.Context,
	transaction *sql.Tx,
	snapshot BudgetSnapshot,
	eventType string,
	payload string,
	idempotencyKey string,
	micros int64,
) error {
	eventID, err := domain.NewEventID()
	if err != nil {
		return err
	}
	result, err := transaction.ExecContext(
		ctx,
		`INSERT INTO budget_events (
			id, budget_id, task_id, limit_revision, event_type,
			payload_json, idempotency_key, created_at_unix_micros
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(budget_id, limit_revision, event_type) DO NOTHING`,
		eventID, snapshot.BudgetID, snapshot.TaskID, snapshot.LimitRevision,
		eventType, payload, idempotencyKey, micros,
	)
	if err != nil {
		return repositoryWriteError("insert budget event", err)
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		return nil
	}
	var sequence uint64
	if err := transaction.QueryRowContext(
		ctx,
		`SELECT coalesce(max(sequence), 0) + 1
		 FROM task_events WHERE task_id = ?`,
		snapshot.TaskID,
	).Scan(&sequence); err != nil {
		return classify("allocate budget task event sequence", err)
	}
	_, err = transaction.ExecContext(
		ctx,
		`INSERT INTO task_events (
			id, task_id, run_id, sequence, event_type, payload_json,
			idempotency_key, created_at_unix_micros
		) VALUES (?, ?, NULL, ?, ?, ?, ?, ?)`,
		eventID, snapshot.TaskID, sequence, "budget."+eventType,
		payload, "budget-event:"+eventID.String(),
		micros,
	)
	if err != nil {
		return repositoryWriteError("insert budget task event", err)
	}
	return nil
}

func addExactCosts(
	left ExactMinorCost,
	right ExactMinorCost,
) (ExactMinorCost, error) {
	if left.Currency != right.Currency {
		return ExactMinorCost{}, errors.New("exact budget currencies differ")
	}
	total := new(big.Rat).Add(exactCostRat(left), exactCostRat(right))
	return exactCostFromRat(left.Currency, total)
}

func compareExactCosts(left, right ExactMinorCost) int {
	if left.Currency != right.Currency {
		return strings.Compare(string(left.Currency), string(right.Currency))
	}
	return exactCostRat(left).Cmp(exactCostRat(right))
}

func subtractExactCostsFloorZero(
	left ExactMinorCost,
	right ExactMinorCost,
) ExactMinorCost {
	if compareExactCosts(left, right) <= 0 {
		return zeroExactCost(left.Currency)
	}
	value, err := exactCostFromRat(
		left.Currency, new(big.Rat).Sub(exactCostRat(left), exactCostRat(right)),
	)
	if err != nil {
		return zeroExactCost(left.Currency)
	}
	return value
}

func exactCostRat(value ExactMinorCost) *big.Rat {
	return new(big.Rat).SetFrac(
		big.NewInt(value.Numerator), big.NewInt(value.Denominator),
	)
}

func exactCostFromRat(
	currency domain.CurrencyCode,
	value *big.Rat,
) (ExactMinorCost, error) {
	if value.Sign() < 0 || !value.Num().IsInt64() || !value.Denom().IsInt64() {
		return ExactMinorCost{}, errors.New(
			"exact budget aggregate exceeds storage range",
		)
	}
	return ExactMinorCost{
		Numerator: value.Num().Int64(), Denominator: value.Denom().Int64(),
		Currency: currency,
	}, nil
}

func zeroExactCost(currency domain.CurrencyCode) ExactMinorCost {
	return ExactMinorCost{Denominator: 1, Currency: currency}
}

func categoryReservationCount(
	categories []BudgetCategorySnapshot,
	category BudgetCostCategory,
) uint64 {
	for _, item := range categories {
		if item.Category == category {
			return item.ReservationCount
		}
	}
	return 0
}

func nullableTokenCount(value *domain.TokenCount) any {
	if value == nil {
		return nil
	}
	return int64(*value)
}

func cloneTokenCount(value *domain.TokenCount) *domain.TokenCount {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func sameTokenCount(left, right *domain.TokenCount) bool {
	return left == nil && right == nil ||
		left != nil && right != nil && *left == *right
}

func nullableExactNumerator(value *ExactMinorCost) any {
	if value == nil {
		return nil
	}
	return value.Numerator
}

func nullableExactDenominator(value *ExactMinorCost) any {
	if value == nil {
		return nil
	}
	return value.Denominator
}
