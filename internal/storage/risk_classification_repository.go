package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/review"
)

type TaskRiskClassification struct {
	TaskID         domain.TaskID
	Revision       uint64
	Classification review.RiskClassification
	CreatedAt      time.Time
}

// RecordInitialTaskRiskClassification persists the first policy decision and
// its complete structured explanation. A task can have only one revision 1.
func (repositories *Repositories) RecordInitialTaskRiskClassification(
	ctx context.Context,
	taskID domain.TaskID,
	signals []review.RiskSignal,
	userOverride domain.RiskLevel,
) (TaskRiskClassification, error) {
	classification, err := review.ClassifyChangeRisk(signals, userOverride)
	if err != nil {
		return TaskRiskClassification{}, err
	}
	return repositories.persistTaskRiskClassification(ctx, taskID, 1, classification, true)
}

// EscalateTaskRiskClassification retains every prior signal and applies new
// evidence through the monotonic policy before appending an immutable revision.
func (repositories *Repositories) EscalateTaskRiskClassification(
	ctx context.Context,
	taskID domain.TaskID,
	newSignals []review.RiskSignal,
	userOverride domain.RiskLevel,
) (TaskRiskClassification, error) {
	current, err := repositories.GetLatestTaskRiskClassification(ctx, taskID)
	if err != nil {
		return TaskRiskClassification{}, err
	}
	next, err := review.EscalateChangeRisk(current.Classification, newSignals, userOverride)
	if err != nil {
		return TaskRiskClassification{}, err
	}
	return repositories.persistTaskRiskClassification(ctx, taskID, current.Revision+1, next, false)
}

func (repositories *Repositories) persistTaskRiskClassification(
	ctx context.Context,
	taskID domain.TaskID,
	revision uint64,
	classification review.RiskClassification,
	initial bool,
) (TaskRiskClassification, error) {
	if repositories == nil || repositories.database == nil || taskID.IsZero() || revision == 0 {
		return TaskRiskClassification{}, errors.New("task risk classification repository input is invalid")
	}
	if err := classification.Validate(); err != nil {
		return TaskRiskClassification{}, err
	}
	createdAt, micros := repositories.timestamp()
	var override any
	if value, ok := classification.UserOverride(); ok {
		override = value
	}
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		var latest uint64
		err := transaction.sql.QueryRowContext(ctx,
			`SELECT COALESCE(MAX(revision), 0) FROM task_risk_classifications WHERE task_id = ?`, taskID,
		).Scan(&latest)
		if err != nil {
			return classify("read task risk classification revision", err)
		}
		if initial && latest != 0 || !initial && latest+1 != revision {
			return ErrConflict
		}
		if _, err := transaction.sql.ExecContext(ctx, `
			INSERT INTO task_risk_classifications (
				task_id, revision, policy_version, selected_risk, user_override,
				explanation, created_at_unix_micros
			) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			taskID, revision, classification.PolicyVersion(), classification.SelectedRisk(), override,
			classification.Explanation(), micros,
		); err != nil {
			return classify("record task risk classification", err)
		}
		for ordinal, signal := range classification.Signals() {
			if _, err := transaction.sql.ExecContext(ctx, `
				INSERT INTO task_risk_classification_signals (
					task_id, classification_revision, ordinal, signal, floor
				) VALUES (?, ?, ?, ?, ?)`,
				taskID, revision, ordinal, signal, signal.Floor(),
			); err != nil {
				return classify("record task risk classification signal", err)
			}
		}
		for ordinal, reason := range classification.Reasons() {
			var signal any
			if reason.Signal.IsValid() {
				signal = reason.Signal
			}
			if _, err := transaction.sql.ExecContext(ctx, `
				INSERT INTO task_risk_classification_reasons (
					task_id, classification_revision, ordinal, reason_code, signal, floor
				) VALUES (?, ?, ?, ?, ?, ?)`,
				taskID, revision, ordinal, reason.Code, signal, reason.Floor,
			); err != nil {
				return classify("record task risk classification reason", err)
			}
		}
		return nil
	})
	if err != nil {
		return TaskRiskClassification{}, err
	}
	return TaskRiskClassification{TaskID: taskID, Revision: revision, Classification: classification, CreatedAt: createdAt}, nil
}

func (repositories *Repositories) GetLatestTaskRiskClassification(
	ctx context.Context,
	taskID domain.TaskID,
) (TaskRiskClassification, error) {
	if repositories == nil || repositories.database == nil || taskID.IsZero() {
		return TaskRiskClassification{}, errors.New("task risk classification repository input is invalid")
	}
	var revision uint64
	var policy string
	var selected domain.RiskLevel
	var override sql.NullString
	var explanation string
	var micros int64
	err := repositories.database.sql.QueryRowContext(ctx, `
		SELECT revision, policy_version, selected_risk, user_override, explanation, created_at_unix_micros
		FROM task_risk_classifications
		WHERE task_id = ?
		ORDER BY revision DESC
		LIMIT 1`, taskID,
	).Scan(&revision, &policy, &selected, &override, &explanation, &micros)
	if errors.Is(err, sql.ErrNoRows) {
		return TaskRiskClassification{}, ErrNotFound
	}
	if err != nil {
		return TaskRiskClassification{}, classify("get latest task risk classification", err)
	}
	rows, err := repositories.database.sql.QueryContext(ctx, `
		SELECT signal
		FROM task_risk_classification_signals
		WHERE task_id = ? AND classification_revision = ?
		ORDER BY ordinal`, taskID, revision)
	if err != nil {
		return TaskRiskClassification{}, classify("get task risk classification signals", err)
	}
	defer rows.Close()
	signals := make([]review.RiskSignal, 0, review.MaximumRiskSignals)
	for rows.Next() {
		var signal review.RiskSignal
		if err := rows.Scan(&signal); err != nil {
			return TaskRiskClassification{}, classify("scan task risk classification signal", err)
		}
		signals = append(signals, signal)
	}
	if err := rows.Err(); err != nil {
		return TaskRiskClassification{}, classify("iterate task risk classification signals", err)
	}
	var userOverride domain.RiskLevel
	if override.Valid {
		userOverride = domain.RiskLevel(override.String)
	}
	classification, err := review.ClassifyChangeRisk(signals, userOverride)
	if err != nil {
		return TaskRiskClassification{}, err
	}
	if string(classification.PolicyVersion()) != policy || classification.SelectedRisk() != selected || classification.Explanation() != explanation {
		return TaskRiskClassification{}, fmt.Errorf("persisted task risk classification does not match policy inputs")
	}
	return TaskRiskClassification{
		TaskID: taskID, Revision: revision, Classification: classification,
		CreatedAt: time.UnixMicro(micros).UTC(),
	}, nil
}
