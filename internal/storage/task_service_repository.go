package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"codeflux.dev/codeflux/internal/checkpoint"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/forecast"
	"codeflux.dev/codeflux/internal/policy"
)

const maxTaskServiceSummaryCharacters = 2048

// TaskServiceSnapshot is one transactionally consistent SQLite projection for
// the authoritative TaskService.GetTask query. SummaryRedacted comes only from
// the already-redacted immutable user message.
type TaskServiceSnapshot struct {
	Task                     Task
	SessionID                domain.SessionID
	PlanRevision             uint64
	SummaryRedacted          string
	SummaryOriginalBytes     uint64
	SummaryTruncated         bool
	Budget                   *BudgetSnapshot
	Policy                   *policy.Snapshot
	PolicyRevision           uint64
	Forecast                 *forecast.Forecast
	ForecastRevision         uint64
	ActualPricingSnapshotIDs []string
	SettlingProviderRequest  *bool
	LatestCheckpoint         *TaskServiceCheckpoint
	ObservedAt               time.Time
}

// TaskServiceCheckpoint is the latest durable checkpoint context included in
// the same read transaction as budget and provider-settlement facts.
type TaskServiceCheckpoint struct {
	ID       domain.CheckpointID
	State    domain.CheckpointState
	PlanStep string
}

// ReadTaskServiceSnapshot reads task, session, plan, and exact budget state
// from one read transaction so the response cannot combine different SQLite
// revisions.
func (repositories *Repositories) ReadTaskServiceSnapshot(
	ctx context.Context,
	taskID domain.TaskID,
) (TaskServiceSnapshot, error) {
	if repositories == nil || repositories.database == nil {
		return TaskServiceSnapshot{}, errors.New("repositories are unavailable")
	}
	if taskID.IsZero() {
		return TaskServiceSnapshot{}, errors.New("task ID must not be empty")
	}
	transaction, err := repositories.database.sql.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return TaskServiceSnapshot{}, classify("begin task service snapshot", err)
	}
	defer transaction.Rollback()

	snapshot := TaskServiceSnapshot{ObservedAt: repositories.now().UTC()}
	snapshot.Task, err = scanTask(transaction.QueryRowContext(
		ctx,
		`SELECT id, thread_id, repository_id, request_message_id, state,
		        policy_preset, reasoning_effort, risk_level, required_assurance,
		        created_at_unix_micros, updated_at_unix_micros, revision
		 FROM tasks WHERE id = ?`,
		taskID,
	), "read task service task")
	if err != nil {
		return TaskServiceSnapshot{}, err
	}

	var sessionIDRaw string
	err = transaction.QueryRowContext(ctx,
		"SELECT id FROM sessions WHERE thread_id = ?",
		snapshot.Task.ThreadID,
	).Scan(&sessionIDRaw)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return TaskServiceSnapshot{}, classify("read task service session", err)
	}
	if err == nil {
		snapshot.SessionID, err = domain.ParseSessionID(sessionIDRaw)
		if err != nil {
			return TaskServiceSnapshot{}, typedError(ErrCorrupt, "read task service session", err)
		}
	}

	if err := transaction.QueryRowContext(ctx,
		"SELECT coalesce(max(revision), 0) FROM agent_plan_revisions WHERE task_id = ?",
		taskID,
	).Scan(&snapshot.PlanRevision); err != nil {
		return TaskServiceSnapshot{}, classify("read task service plan revision", err)
	}
	if snapshot.Task.RequestMessageID != nil {
		var originalBytes int64
		if err := transaction.QueryRowContext(ctx, `
			SELECT substr(body_redacted, 1, ?),
			       length(CAST(body_redacted AS BLOB))
			FROM messages
			WHERE id = ? AND thread_id = ? AND role = 'user'`,
			maxTaskServiceSummaryCharacters,
			*snapshot.Task.RequestMessageID,
			snapshot.Task.ThreadID,
		).Scan(&snapshot.SummaryRedacted, &originalBytes); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return TaskServiceSnapshot{}, typedError(
					ErrCorrupt,
					"read task service redacted summary",
					err,
				)
			}
			return TaskServiceSnapshot{}, classify("read task service redacted summary", err)
		}
		snapshot.SummaryOriginalBytes = uint64(originalBytes)
		snapshot.SummaryTruncated = int64(len(snapshot.SummaryRedacted)) < originalBytes
	}

	var policyJSON string
	err = transaction.QueryRowContext(ctx, `
		SELECT revision, canonical_json
		FROM execution_policy_revisions
		WHERE task_id = ? ORDER BY revision DESC LIMIT 1`, taskID,
	).Scan(&snapshot.PolicyRevision, &policyJSON)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return TaskServiceSnapshot{}, classify("read task service policy", err)
	}
	if err == nil {
		var selected policy.Snapshot
		if decodeErr := json.Unmarshal([]byte(policyJSON), &selected); decodeErr != nil {
			return TaskServiceSnapshot{}, typedError(ErrCorrupt, "read task service policy", decodeErr)
		}
		if validateErr := selected.Validate(); validateErr != nil {
			return TaskServiceSnapshot{}, typedError(ErrCorrupt, "read task service policy", validateErr)
		}
		snapshot.Policy = &selected
	}

	var forecastJSON, forecastPolicyJSON string
	err = transaction.QueryRowContext(ctx, `
		SELECT forecast.revision, forecast.canonical_json, policy.canonical_json
		FROM effort_forecast_revisions AS forecast
		JOIN execution_policy_revisions AS policy
		  ON policy.task_id = forecast.task_id
		 AND policy.revision = forecast.policy_revision
		WHERE forecast.task_id = ?
		ORDER BY forecast.revision DESC LIMIT 1`, taskID,
	).Scan(&snapshot.ForecastRevision, &forecastJSON, &forecastPolicyJSON)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return TaskServiceSnapshot{}, classify("read task service forecast", err)
	}
	if err == nil {
		var value forecast.Forecast
		var selected policy.Snapshot
		if decodeErr := json.Unmarshal([]byte(forecastJSON), &value); decodeErr != nil {
			return TaskServiceSnapshot{}, typedError(ErrCorrupt, "read task service forecast", decodeErr)
		}
		if decodeErr := json.Unmarshal([]byte(forecastPolicyJSON), &selected); decodeErr != nil {
			return TaskServiceSnapshot{}, typedError(ErrCorrupt, "read task service forecast policy", decodeErr)
		}
		if validateErr := value.Validate(selected); validateErr != nil {
			return TaskServiceSnapshot{}, typedError(ErrCorrupt, "read task service forecast", validateErr)
		}
		snapshot.Forecast = &value
	}

	var budgetIDRaw string
	err = transaction.QueryRowContext(ctx,
		"SELECT id FROM budgets WHERE task_id = ?",
		taskID,
	).Scan(&budgetIDRaw)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return TaskServiceSnapshot{}, classify("read task service budget identity", err)
	}
	if err == nil {
		budgetID, parseErr := domain.ParseBudgetID(budgetIDRaw)
		if parseErr != nil {
			return TaskServiceSnapshot{}, typedError(
				ErrCorrupt,
				"read task service budget identity",
				parseErr,
			)
		}
		budget, budgetErr := computeBudgetSnapshot(ctx, transaction, budgetID)
		if budgetErr != nil {
			return TaskServiceSnapshot{}, budgetErr
		}
		snapshot.Budget = &budget
		settling := false
		if err := transaction.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM provider_logical_requests AS request
				LEFT JOIN provider_request_attempts AS attempt
				  ON attempt.logical_request_id = request.id
				WHERE request.task_id = ?
				  AND (
				    request.state = 'in-flight'
				    OR attempt.state IN ('started', 'streaming')
				  )
			)`, taskID,
		).Scan(&settling); err != nil {
			return TaskServiceSnapshot{}, classify("read task service provider settlement", err)
		}
		snapshot.SettlingProviderRequest = &settling
	}

	var checkpointIDRaw, checkpointStateRaw string
	var checkpointStateJSON sql.NullString
	err = transaction.QueryRowContext(ctx, `
		SELECT id, state, canonical_state_json
		FROM checkpoints
		WHERE task_id = ?
		ORDER BY created_at_unix_micros DESC, id DESC
		LIMIT 1`, taskID,
	).Scan(&checkpointIDRaw, &checkpointStateRaw, &checkpointStateJSON)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return TaskServiceSnapshot{}, classify("read task service latest checkpoint", err)
	}
	if err == nil {
		checkpointID, parseErr := domain.ParseCheckpointID(checkpointIDRaw)
		if parseErr != nil {
			return TaskServiceSnapshot{}, typedError(ErrCorrupt, "read task service latest checkpoint", parseErr)
		}
		checkpointState := domain.CheckpointState(checkpointStateRaw)
		if !checkpointState.IsValid() {
			return TaskServiceSnapshot{}, typedError(ErrCorrupt, "read task service latest checkpoint", errors.New("checkpoint state is invalid"))
		}
		value := TaskServiceCheckpoint{ID: checkpointID, State: checkpointState}
		if checkpointStateJSON.Valid {
			var state checkpoint.Snapshot
			if decodeErr := json.Unmarshal([]byte(checkpointStateJSON.String), &state); decodeErr != nil {
				return TaskServiceSnapshot{}, typedError(ErrCorrupt, "read task service checkpoint state", decodeErr)
			}
			canonical, canonicalErr := checkpoint.Canonicalize(state)
			if canonicalErr != nil || canonical.JSON != checkpointStateJSON.String {
				return TaskServiceSnapshot{}, typedError(ErrCorrupt, "read task service checkpoint state", errors.New("checkpoint state is not canonical"))
			}
			value.PlanStep = taskServiceCheckpointPlanStep(canonical.Snapshot)
		}
		snapshot.LatestCheckpoint = &value
	}

	rows, err := transaction.QueryContext(ctx, `
		SELECT DISTINCT request.pricing_revision_id
		FROM provider_logical_requests AS request
		JOIN provider_request_attempts AS attempt
		  ON attempt.logical_request_id = request.id
		JOIN budget_reservations AS reservation
		  ON reservation.attempt_id = attempt.id
		JOIN budget_usage_postings AS posting
		  ON posting.reservation_id = reservation.id
		WHERE request.task_id = ? AND posting.cost_known = 1
		ORDER BY request.pricing_revision_id`, taskID)
	if err != nil {
		return TaskServiceSnapshot{}, classify("read task service pricing identities", err)
	}
	for rows.Next() {
		var identity string
		if scanErr := rows.Scan(&identity); scanErr != nil {
			rows.Close()
			return TaskServiceSnapshot{}, classify("read task service pricing identity", scanErr)
		}
		snapshot.ActualPricingSnapshotIDs = append(snapshot.ActualPricingSnapshotIDs, identity)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		rows.Close()
		return TaskServiceSnapshot{}, classify("read task service pricing identities", rowsErr)
	}
	if closeErr := rows.Close(); closeErr != nil {
		return TaskServiceSnapshot{}, classify("close task service pricing identities", closeErr)
	}

	if err := transaction.Commit(); err != nil {
		return TaskServiceSnapshot{}, classify("commit task service snapshot", err)
	}
	return snapshot, nil
}

func taskServiceCheckpointPlanStep(snapshot checkpoint.Snapshot) string {
	for _, step := range snapshot.PendingPlanSteps {
		if step.State == checkpoint.PlanStepInProgress {
			return step.ID
		}
	}
	for _, step := range snapshot.PendingPlanSteps {
		if step.State == checkpoint.PlanStepFailed {
			return step.ID
		}
	}
	if len(snapshot.PendingPlanSteps) != 0 {
		return snapshot.PendingPlanSteps[0].ID
	}
	if len(snapshot.CompletedPlanSteps) != 0 {
		return snapshot.CompletedPlanSteps[len(snapshot.CompletedPlanSteps)-1].ID
	}
	return ""
}
