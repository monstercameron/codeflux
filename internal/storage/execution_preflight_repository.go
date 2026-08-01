package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/forecast"
	"codeflux.dev/codeflux/internal/policy"
)

var (
	// ErrExecutionPreflightIncomplete means execution was requested without
	// exact, mutually consistent policy, forecast, and budget bindings.
	ErrExecutionPreflightIncomplete = errors.New("execution preflight incomplete")
	// ErrExecutionPreflightStale means an inspectable preflight no longer binds
	// the latest approved immutable inputs.
	ErrExecutionPreflightStale = errors.New("execution preflight stale")
	// ErrForecastOutcomeNotFinal prevents provisional run or task state from
	// becoming immutable calibration evidence.
	ErrForecastOutcomeNotFinal = errors.New("forecast outcome is not final")
)

// ExecutionPolicyRevision is one exact task-bound policy snapshot.
type ExecutionPolicyRevision struct {
	TaskID          domain.TaskID
	Revision        uint64
	PolicyVersion   string
	SelectionSource policy.SelectionSource
	CanonicalJSON   string
	ContentSHA256   string
	IdempotencyKey  string
	CreatedAt       time.Time
}

// RecordExecutionPolicy declares one immutable task policy revision.
type RecordExecutionPolicy struct {
	TaskID         domain.TaskID
	Policy         policy.Snapshot
	IdempotencyKey string
}

// EffortForecastRevision retains exact forecast inputs, output, and shadow
// eligibility without granting routing authority.
type EffortForecastRevision struct {
	TaskID                 domain.TaskID
	Revision               uint64
	PolicyRevision         uint64
	AlgorithmVersion       string
	CanonicalJSON          string
	ContentSHA256          string
	FeaturesJSON           string
	FeaturesSHA256         string
	CounterfactualEligible bool
	EligibilityJSON        string
	IdempotencyKey         string
	CreatedAt              time.Time
}

// RecordEffortForecast declares one immutable policy-bound forecast revision.
type RecordEffortForecast struct {
	TaskID         domain.TaskID
	PolicyRevision uint64
	Forecast       forecast.Forecast
	Eligibility    forecast.CounterfactualEligibility
	IdempotencyKey string
}

// ExecutionPreflight is the inspectable object shown before execution. Its
// canonical presentation contains the exact policy, forecast, and approved
// budget limit rather than only mutable labels.
type ExecutionPreflight struct {
	TaskID                 domain.TaskID
	Revision               uint64
	ExpectedTaskRevision   uint64
	PolicyRevision         uint64
	ForecastRevision       uint64
	BudgetID               domain.BudgetID
	BudgetLimitRevision    uint64
	BudgetSnapshotRevision uint64
	PresentationJSON       string
	ContentSHA256          string
	IdempotencyKey         string
	CreatedAt              time.Time
}

// TaskExecutionPresentation combines immutable policy/forecast evidence with
// the latest exact budget exposure for a prepared task.
type TaskExecutionPresentation struct {
	TaskID                 domain.TaskID
	PreflightRevision      uint64
	BudgetSnapshotRevision uint64
	PresentationJSON       string
	ContentSHA256          string
}

// PrepareTaskExecution declares the immutable pre-start binding.
type PrepareTaskExecution struct {
	TaskID               domain.TaskID
	ExpectedTaskRevision uint64
	PolicyRevision       uint64
	ForecastRevision     uint64
	BudgetID             domain.BudgetID
	BudgetLimitRevision  uint64
	IdempotencyKey       string
}

// StartPreparedTaskRun atomically starts a task and run from one current
// preflight revision.
type StartPreparedTaskRun struct {
	RunID                domain.RunID
	EventID              domain.EventID
	TaskID               domain.TaskID
	PreflightRevision    uint64
	ExpectedTaskRevision uint64
	Attempt              uint64
	IdempotencyKey       string
	EventIdempotencyKey  string
}

// StartedTaskRun is the durable task/run start result.
type StartedTaskRun struct {
	RunID        domain.RunID
	TaskID       domain.TaskID
	State        domain.RunState
	TaskRevision uint64
	Attempt      uint64
	Preflight    ExecutionPreflight
	TaskEvent    TaskEvent
	CreatedAt    time.Time
}

// ForecastOutcome is immutable forecast-versus-actual telemetry.
type ForecastOutcome struct {
	RunID            domain.RunID
	TaskID           domain.TaskID
	ForecastRevision uint64
	OutcomeJSON      string
	OutcomeSHA256    string
	ComparisonJSON   string
	ComparisonSHA256 string
	IdempotencyKey   string
	CreatedAt        time.Time
}

// RecordForecastOutcome declares completed-run telemetry.
type RecordForecastOutcome struct {
	RunID          domain.RunID
	TaskID         domain.TaskID
	Actual         forecast.ActualResult
	IdempotencyKey string
}

// RecordExecutionPolicy persists the exact canonical policy selected for a
// task. Identical retries return the original immutable revision.
func (repositories *Repositories) RecordExecutionPolicy(
	ctx context.Context,
	input RecordExecutionPolicy,
) (ExecutionPolicyRevision, error) {
	if input.TaskID.IsZero() {
		return ExecutionPolicyRevision{}, errors.New("task ID must not be empty")
	}
	if err := validateBounded("policy idempotency key", input.IdempotencyKey, 255); err != nil {
		return ExecutionPolicyRevision{}, err
	}
	canonical, err := input.Policy.CanonicalJSON()
	if err != nil {
		return ExecutionPolicyRevision{}, err
	}
	digest, err := input.Policy.Digest()
	if err != nil {
		return ExecutionPolicyRevision{}, err
	}
	now, micros := repositories.timestamp()
	var recorded ExecutionPolicyRevision
	err = repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		existing, found, err := findExecutionPolicyByIdempotency(
			ctx, transaction.sql, input.TaskID, input.IdempotencyKey,
		)
		if err != nil {
			return err
		}
		if found {
			if existing.CanonicalJSON != string(canonical) ||
				existing.ContentSHA256 != digest {
				return typedError(ErrConflict, "record execution policy",
					errors.New("idempotency key belongs to another policy"))
			}
			recorded = existing
			return nil
		}
		var revision uint64
		if err := transaction.sql.QueryRowContext(ctx,
			`SELECT COALESCE(MAX(revision), 0) + 1
			 FROM execution_policy_revisions WHERE task_id = ?`,
			input.TaskID).Scan(&revision); err != nil {
			return classify("allocate execution policy revision", err)
		}
		result, err := transaction.sql.ExecContext(ctx,
			`INSERT INTO execution_policy_revisions (
				task_id, revision, policy_version, selection_source,
				canonical_json, content_sha256, idempotency_key,
				created_at_unix_micros
			)
			SELECT ?, ?, ?, ?, ?, ?, ?, ?
			WHERE EXISTS (SELECT 1 FROM tasks WHERE id = ?)`,
			input.TaskID, revision, input.Policy.Version, input.Policy.Source,
			string(canonical), digest, input.IdempotencyKey, micros,
			input.TaskID)
		if err != nil {
			return repositoryWriteError("record execution policy", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected != 1 {
			return typedError(ErrConstraint, "record execution policy",
				errors.New("task does not exist"))
		}
		recorded = ExecutionPolicyRevision{
			TaskID: input.TaskID, Revision: revision,
			PolicyVersion:   input.Policy.Version,
			SelectionSource: input.Policy.Source,
			CanonicalJSON:   string(canonical), ContentSHA256: digest,
			IdempotencyKey: input.IdempotencyKey, CreatedAt: now,
		}
		return nil
	})
	return recorded, err
}

// RecordEffortForecast persists an exact forecast and its normalized feature
// and shadow-eligibility records.
func (repositories *Repositories) RecordEffortForecast(
	ctx context.Context,
	input RecordEffortForecast,
) (EffortForecastRevision, error) {
	if input.TaskID.IsZero() || input.PolicyRevision == 0 {
		return EffortForecastRevision{}, errors.New("task and policy revision are required")
	}
	if err := validateBounded("forecast idempotency key", input.IdempotencyKey, 255); err != nil {
		return EffortForecastRevision{}, err
	}
	if input.Forecast.AlgorithmVersion != forecast.AlgorithmVersion ||
		input.Forecast.EstimateNotice != forecast.EstimateNotice ||
		!input.Forecast.RequiredBeforeExecution || !input.Forecast.AdvisoryOnly {
		return EffortForecastRevision{}, errors.New("forecast is not an inspectable advisory preflight")
	}
	if !input.Eligibility.AdvisoryOnly ||
		(!input.Eligibility.Eligible && len(input.Eligibility.Reasons) == 0) {
		return EffortForecastRevision{}, errors.New("counterfactual eligibility is invalid")
	}
	canonical, err := json.Marshal(input.Forecast)
	if err != nil {
		return EffortForecastRevision{}, err
	}
	features, err := json.Marshal(input.Forecast.Features)
	if err != nil {
		return EffortForecastRevision{}, err
	}
	eligibility, err := json.Marshal(input.Eligibility)
	if err != nil {
		return EffortForecastRevision{}, err
	}
	digest := hashJSON(string(canonical))
	featureDigest := hashJSON(string(features))
	now, micros := repositories.timestamp()
	var recorded EffortForecastRevision
	err = repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		policyRevision, err := scanExecutionPolicy(transaction.sql.QueryRowContext(ctx,
			`SELECT task_id, revision, policy_version, selection_source,
			        canonical_json, content_sha256, idempotency_key,
			        created_at_unix_micros
			 FROM execution_policy_revisions
			 WHERE task_id = ? AND revision = ?`,
			input.TaskID, input.PolicyRevision), "load forecast policy")
		if err != nil {
			return typedError(ErrExecutionPreflightIncomplete,
				"record effort forecast", err)
		}
		if input.Forecast.Bindings.PolicyDigest != policyRevision.ContentSHA256 {
			return typedError(ErrExecutionPreflightIncomplete,
				"record effort forecast",
				errors.New("forecast policy digest does not match task policy"))
		}
		var selectedPolicy policy.Snapshot
		if err := json.Unmarshal(
			[]byte(policyRevision.CanonicalJSON),
			&selectedPolicy,
		); err != nil {
			return typedError(
				ErrCorrupt,
				"decode forecast policy",
				err,
			)
		}
		if err := input.Forecast.Validate(selectedPolicy); err != nil {
			return typedError(
				ErrExecutionPreflightIncomplete,
				"validate effort forecast",
				err,
			)
		}
		existing, found, err := findForecastByIdempotency(
			ctx, transaction.sql, input.TaskID, input.IdempotencyKey,
		)
		if err != nil {
			return err
		}
		if found {
			if existing.PolicyRevision != input.PolicyRevision ||
				existing.CanonicalJSON != string(canonical) ||
				existing.EligibilityJSON != string(eligibility) {
				return typedError(ErrConflict, "record effort forecast",
					errors.New("idempotency key belongs to another forecast"))
			}
			recorded = existing
			return nil
		}
		var revision uint64
		if err := transaction.sql.QueryRowContext(ctx,
			`SELECT COALESCE(MAX(revision), 0) + 1
			 FROM effort_forecast_revisions WHERE task_id = ?`,
			input.TaskID).Scan(&revision); err != nil {
			return classify("allocate effort forecast revision", err)
		}
		if _, err := transaction.sql.ExecContext(ctx,
			`INSERT INTO effort_forecast_revisions (
				task_id, revision, policy_revision, algorithm_version,
				canonical_json, content_sha256, features_json,
				features_sha256, counterfactual_eligible, eligibility_json,
				idempotency_key, created_at_unix_micros
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			input.TaskID, revision, input.PolicyRevision,
			input.Forecast.AlgorithmVersion, string(canonical), digest,
			string(features), featureDigest, input.Eligibility.Eligible,
			string(eligibility), input.IdempotencyKey, micros); err != nil {
			return repositoryWriteError("record effort forecast", err)
		}
		recorded = EffortForecastRevision{
			TaskID: input.TaskID, Revision: revision,
			PolicyRevision:   input.PolicyRevision,
			AlgorithmVersion: input.Forecast.AlgorithmVersion,
			CanonicalJSON:    string(canonical), ContentSHA256: digest,
			FeaturesJSON: string(features), FeaturesSHA256: featureDigest,
			CounterfactualEligible: input.Eligibility.Eligible,
			EligibilityJSON:        string(eligibility),
			IdempotencyKey:         input.IdempotencyKey, CreatedAt: now,
		}
		return nil
	})
	return recorded, err
}

// PrepareTaskExecution creates the exact user-presentable preflight after all
// three inputs are durable and current.
func (repositories *Repositories) PrepareTaskExecution(
	ctx context.Context,
	input PrepareTaskExecution,
) (ExecutionPreflight, error) {
	if input.TaskID.IsZero() || input.BudgetID.IsZero() ||
		input.PolicyRevision == 0 || input.ForecastRevision == 0 {
		return ExecutionPreflight{}, errors.New("task, policy, forecast, and budget are required")
	}
	if err := validateBounded("preflight idempotency key", input.IdempotencyKey, 255); err != nil {
		return ExecutionPreflight{}, err
	}
	now, micros := repositories.timestamp()
	var prepared ExecutionPreflight
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		existing, found, err := findPreflightByIdempotency(
			ctx, transaction.sql, input.TaskID, input.IdempotencyKey,
		)
		if err != nil {
			return err
		}
		if found {
			if !samePreflightInput(existing, input) {
				return typedError(ErrConflict, "prepare task execution",
					errors.New("idempotency key belongs to another preflight"))
			}
			prepared = existing
			return nil
		}
		presentation, budgetSnapshotRevision, err := buildCurrentPreflightPresentation(
			ctx, transaction.sql, input,
		)
		if err != nil {
			return err
		}
		var revision uint64
		if err := transaction.sql.QueryRowContext(ctx,
			`SELECT COALESCE(MAX(revision), 0) + 1
			 FROM task_execution_preflights WHERE task_id = ?`,
			input.TaskID).Scan(&revision); err != nil {
			return classify("allocate task execution preflight revision", err)
		}
		digest := hashJSON(presentation)
		if _, err := transaction.sql.ExecContext(ctx,
			`INSERT INTO task_execution_preflights (
				task_id, revision, expected_task_revision, policy_revision,
				forecast_revision, budget_id, budget_limit_revision,
				budget_snapshot_revision, presentation_json, content_sha256,
				idempotency_key, created_at_unix_micros
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			input.TaskID, revision, input.ExpectedTaskRevision,
			input.PolicyRevision, input.ForecastRevision, input.BudgetID,
			input.BudgetLimitRevision, budgetSnapshotRevision,
			presentation, digest,
			input.IdempotencyKey, micros); err != nil {
			return repositoryWriteError("prepare task execution", err)
		}
		prepared = ExecutionPreflight{
			TaskID: input.TaskID, Revision: revision,
			ExpectedTaskRevision:   input.ExpectedTaskRevision,
			PolicyRevision:         input.PolicyRevision,
			ForecastRevision:       input.ForecastRevision,
			BudgetID:               input.BudgetID,
			BudgetLimitRevision:    input.BudgetLimitRevision,
			BudgetSnapshotRevision: budgetSnapshotRevision,
			PresentationJSON:       presentation, ContentSHA256: digest,
			IdempotencyKey: input.IdempotencyKey, CreatedAt: now,
		}
		return nil
	})
	return prepared, err
}

// GetTaskExecutionPreflight reads one immutable preflight after restart.
func (repositories *Repositories) GetTaskExecutionPreflight(
	ctx context.Context,
	taskID domain.TaskID,
	revision uint64,
) (ExecutionPreflight, error) {
	if taskID.IsZero() || revision == 0 {
		return ExecutionPreflight{}, errors.New("task and preflight revision are required")
	}
	return scanPreflight(repositories.database.sql.QueryRowContext(ctx,
		`SELECT task_id, revision, expected_task_revision, policy_revision,
		        forecast_revision, budget_id, budget_limit_revision,
		        budget_snapshot_revision, presentation_json,
		        content_sha256, idempotency_key,
		        created_at_unix_micros
		 FROM task_execution_preflights
		 WHERE task_id = ? AND revision = ?`,
		taskID, revision), "get task execution preflight")
}

// GetTaskExecutionPolicy reads one immutable approved policy revision.
//
// Starting a run needs the provider the reviewed policy named, not a current
// default: a run approved against one provider must not be queued against
// another because a preference changed in between.
func (repositories *Repositories) GetTaskExecutionPolicy(
	ctx context.Context,
	taskID domain.TaskID,
	revision uint64,
) (ExecutionPolicyRevision, error) {
	if taskID.IsZero() || revision == 0 {
		return ExecutionPolicyRevision{}, errors.New("task and policy revision are required")
	}
	return scanExecutionPolicy(repositories.database.sql.QueryRowContext(ctx,
		`SELECT task_id, revision, policy_version, selection_source,
		        canonical_json, content_sha256, idempotency_key,
		        created_at_unix_micros
		 FROM execution_policy_revisions
		 WHERE task_id = ? AND revision = ?`,
		taskID, revision), "get task execution policy")
}

// GetTaskExecutionPresentation preserves the immutable prepared policy and
// forecast while refreshing exact reserved, actual, remaining, and unknown
// budget state.
func (repositories *Repositories) GetTaskExecutionPresentation(
	ctx context.Context,
	taskID domain.TaskID,
	preflightRevision uint64,
) (TaskExecutionPresentation, error) {
	preflight, err := repositories.GetTaskExecutionPreflight(
		ctx,
		taskID,
		preflightRevision,
	)
	if err != nil {
		return TaskExecutionPresentation{}, err
	}
	policyRevision, err := scanExecutionPolicy(
		repositories.database.sql.QueryRowContext(
			ctx,
			`SELECT task_id, revision, policy_version, selection_source,
			        canonical_json, content_sha256, idempotency_key,
			        created_at_unix_micros
			 FROM execution_policy_revisions
			 WHERE task_id = ? AND revision = ?`,
			taskID,
			preflight.PolicyRevision,
		),
		"load presentation policy",
	)
	if err != nil {
		return TaskExecutionPresentation{}, err
	}
	forecastRevision, err := scanForecast(
		repositories.database.sql.QueryRowContext(
			ctx,
			`SELECT task_id, revision, policy_revision, algorithm_version,
			        canonical_json, content_sha256, features_json,
			        features_sha256, counterfactual_eligible,
			        eligibility_json, idempotency_key,
			        created_at_unix_micros
			 FROM effort_forecast_revisions
			 WHERE task_id = ? AND revision = ?`,
			taskID,
			preflight.ForecastRevision,
		),
		"load presentation forecast",
	)
	if err != nil {
		return TaskExecutionPresentation{}, err
	}
	if forecastRevision.PolicyRevision != policyRevision.Revision {
		return TaskExecutionPresentation{}, typedError(
			ErrCorrupt,
			"load task execution presentation",
			errors.New("forecast and policy revisions differ"),
		)
	}
	budget, err := computeBudgetSnapshot(
		ctx,
		repositories.database.sql,
		preflight.BudgetID,
	)
	if err != nil {
		return TaskExecutionPresentation{}, err
	}
	presentation, err := marshalExecutionPresentation(
		policyRevision.CanonicalJSON,
		forecastRevision.CanonicalJSON,
		budget,
	)
	if err != nil {
		return TaskExecutionPresentation{}, err
	}
	return TaskExecutionPresentation{
		TaskID:                 taskID,
		PreflightRevision:      preflightRevision,
		BudgetSnapshotRevision: budget.Revision,
		PresentationJSON:       presentation,
		ContentSHA256:          hashJSON(presentation),
	}, nil
}

// StartPreparedTaskRun is the only repository operation that makes a prepared
// task and its new run active. It rejects every missing or stale binding before
// any visible state is changed.
func (repositories *Repositories) StartPreparedTaskRun(
	ctx context.Context,
	input StartPreparedTaskRun,
) (StartedTaskRun, error) {
	switch {
	case input.RunID.IsZero(), input.EventID.IsZero(), input.TaskID.IsZero():
		return StartedTaskRun{}, errors.New("run, event, and task IDs are required")
	case input.PreflightRevision == 0, input.Attempt == 0:
		return StartedTaskRun{}, errors.New("preflight revision and attempt are required")
	}
	if err := validateBounded("run idempotency key", input.IdempotencyKey, 255); err != nil {
		return StartedTaskRun{}, err
	}
	if err := validateBounded("run event idempotency key", input.EventIdempotencyKey, 255); err != nil {
		return StartedTaskRun{}, err
	}
	now, micros := repositories.timestamp()
	var started StartedTaskRun
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		existing, found, err := findStartedTaskRun(
			ctx, transaction.sql, input.TaskID, input.IdempotencyKey,
		)
		if err != nil {
			return err
		}
		if found {
			if existing.RunID != input.RunID ||
				existing.Attempt != input.Attempt ||
				existing.Preflight.Revision != input.PreflightRevision ||
				existing.Preflight.ExpectedTaskRevision != input.ExpectedTaskRevision ||
				existing.TaskEvent.ID != input.EventID ||
				existing.TaskEvent.IdempotencyKey != input.EventIdempotencyKey {
				return typedError(ErrConflict, "start prepared task run",
					errors.New("idempotency key belongs to another run"))
			}
			started = existing
			return nil
		}
		preflight, err := scanPreflight(transaction.sql.QueryRowContext(ctx,
			`SELECT task_id, revision, expected_task_revision, policy_revision,
			        forecast_revision, budget_id, budget_limit_revision,
			        budget_snapshot_revision, presentation_json,
			        content_sha256, idempotency_key,
			        created_at_unix_micros
			 FROM task_execution_preflights
			 WHERE task_id = ? AND revision = ?`,
			input.TaskID, input.PreflightRevision), "load start preflight")
		if err != nil {
			return typedError(ErrExecutionPreflightIncomplete,
				"start prepared task run", err)
		}
		if preflight.ExpectedTaskRevision != input.ExpectedTaskRevision {
			return typedError(ErrExecutionPreflightStale,
				"start prepared task run",
				errors.New("preflight task revision differs"))
		}
		if err := verifyPreflightStillCurrent(
			ctx, transaction.sql, preflight,
		); err != nil {
			return err
		}
		var taskState domain.TaskState
		var taskRevision uint64
		if err := transaction.sql.QueryRowContext(ctx,
			`SELECT state, revision FROM tasks WHERE id = ?`,
			input.TaskID).Scan(&taskState, &taskRevision); err != nil {
			return classify("load preflight task", err)
		}
		if taskState != domain.TaskStateReady ||
			taskRevision != input.ExpectedTaskRevision {
			return typedError(ErrExecutionPreflightStale,
				"start prepared task run",
				errors.New("task is not at the prepared ready revision"))
		}
		if _, err := transaction.sql.ExecContext(ctx,
			`INSERT INTO runs (
				id, task_id, state, attempt, task_revision, idempotency_key,
				created_at_unix_micros, updated_at_unix_micros, revision
			) VALUES (?, ?, 'starting', ?, ?, ?, ?, ?, 0)`,
			input.RunID, input.TaskID, input.Attempt, taskRevision,
			input.IdempotencyKey, micros, micros); err != nil {
			return repositoryWriteError("create prepared task run", err)
		}
		if _, err := transaction.sql.ExecContext(ctx,
			`INSERT INTO run_execution_bindings (
				run_id, task_id, preflight_revision, policy_revision,
				forecast_revision, budget_id, budget_limit_revision,
				budget_snapshot_revision, created_at_unix_micros
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			input.RunID, input.TaskID, preflight.Revision,
			preflight.PolicyRevision, preflight.ForecastRevision,
			preflight.BudgetID, preflight.BudgetLimitRevision,
			preflight.BudgetSnapshotRevision, micros); err != nil {
			return repositoryWriteError("bind prepared task run", err)
		}
		result, err := transaction.sql.ExecContext(ctx,
			`UPDATE tasks
			 SET state = 'running', revision = revision + 1,
			     updated_at_unix_micros = ?
			 WHERE id = ? AND state = 'ready' AND revision = ?`,
			micros, input.TaskID, input.ExpectedTaskRevision)
		if err != nil {
			return repositoryWriteError("start prepared task", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected != 1 {
			return typedError(ErrStaleRevision, "start prepared task run",
				errors.New("task revision changed during start"))
		}
		runID := input.RunID
		event, err := appendTaskEventTransaction(ctx, transaction, AppendTaskEvent{
			ID: input.EventID, TaskID: input.TaskID, RunID: &runID,
			EventType:      "task.state-transition",
			PayloadJSON:    `{"from":"ready","to":"running"}`,
			IdempotencyKey: input.EventIdempotencyKey,
		}, now, micros)
		if err != nil {
			return err
		}
		started = StartedTaskRun{
			RunID: input.RunID, TaskID: input.TaskID,
			State:        domain.RunStateStarting,
			TaskRevision: input.ExpectedTaskRevision + 1,
			Attempt:      input.Attempt, Preflight: preflight,
			TaskEvent: event, CreatedAt: now,
		}
		return nil
	})
	return started, err
}

// RecordForecastOutcome stores exact actuals and the deterministic comparison
// against the run-bound forecast.
func (repositories *Repositories) RecordForecastOutcome(
	ctx context.Context,
	input RecordForecastOutcome,
) (ForecastOutcome, error) {
	if input.RunID.IsZero() || input.TaskID.IsZero() {
		return ForecastOutcome{}, errors.New("run and task IDs are required")
	}
	if err := validateBounded("forecast outcome idempotency key", input.IdempotencyKey, 255); err != nil {
		return ForecastOutcome{}, err
	}
	now, micros := repositories.timestamp()
	var recorded ForecastOutcome
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		existing, found, err := findForecastOutcome(ctx, transaction.sql, input.RunID)
		if err != nil {
			return err
		}
		if found {
			actualJSON, _ := json.Marshal(input.Actual)
			if existing.TaskID != input.TaskID ||
				existing.OutcomeJSON != string(actualJSON) ||
				existing.IdempotencyKey != input.IdempotencyKey {
				return typedError(ErrConflict, "record forecast outcome",
					errors.New("run already has different forecast outcome"))
			}
			recorded = existing
			return nil
		}
		var forecastRevision uint64
		var forecastJSON string
		var runState domain.RunState
		var taskState domain.TaskState
		if err := transaction.sql.QueryRowContext(ctx,
			`SELECT binding.forecast_revision, value.canonical_json,
			        run.state, task.state
			 FROM run_execution_bindings AS binding
			 JOIN effort_forecast_revisions AS value
			   ON value.task_id = binding.task_id
			  AND value.revision = binding.forecast_revision
			 JOIN runs AS run ON run.id = binding.run_id
			 JOIN tasks AS task ON task.id = binding.task_id
			 WHERE binding.run_id = ? AND binding.task_id = ?`,
			input.RunID, input.TaskID).Scan(
			&forecastRevision, &forecastJSON, &runState, &taskState,
		); err != nil {
			return typedError(ErrExecutionPreflightIncomplete,
				"record forecast outcome", err)
		}
		if err := validateFinalForecastOutcome(
			runState,
			taskState,
			input.Actual.Accepted,
		); err != nil {
			return err
		}
		var value forecast.Forecast
		if err := json.Unmarshal([]byte(forecastJSON), &value); err != nil {
			return typedError(ErrCorrupt, "decode run forecast", err)
		}
		comparison, err := forecast.Compare(value, input.Actual)
		if err != nil {
			return err
		}
		outcomeJSON, err := json.Marshal(input.Actual)
		if err != nil {
			return err
		}
		comparisonJSON, err := json.Marshal(comparison)
		if err != nil {
			return err
		}
		outcomeDigest := hashJSON(string(outcomeJSON))
		comparisonDigest := hashJSON(string(comparisonJSON))
		if _, err := transaction.sql.ExecContext(ctx,
			`INSERT INTO forecast_outcomes (
				run_id, task_id, forecast_revision, outcome_json,
				outcome_sha256, comparison_json, comparison_sha256,
				idempotency_key, created_at_unix_micros
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			input.RunID, input.TaskID, forecastRevision,
			string(outcomeJSON), outcomeDigest, string(comparisonJSON),
			comparisonDigest, input.IdempotencyKey, micros); err != nil {
			return repositoryWriteError("record forecast outcome", err)
		}
		recorded = ForecastOutcome{
			RunID: input.RunID, TaskID: input.TaskID,
			ForecastRevision: forecastRevision,
			OutcomeJSON:      string(outcomeJSON), OutcomeSHA256: outcomeDigest,
			ComparisonJSON:   string(comparisonJSON),
			ComparisonSHA256: comparisonDigest,
			IdempotencyKey:   input.IdempotencyKey, CreatedAt: now,
		}
		return nil
	})
	return recorded, err
}

func validateFinalForecastOutcome(
	runState domain.RunState,
	taskState domain.TaskState,
	accepted bool,
) error {
	valid := false
	switch taskState {
	case domain.TaskStateCompleted:
		valid = runState == domain.RunStateCompleted && accepted
	case domain.TaskStateFailed:
		valid = runState == domain.RunStateFailed && !accepted
	case domain.TaskStateCancelled:
		valid = runState == domain.RunStateCancelled && !accepted
	case domain.TaskStateRolledBack:
		valid = runState == domain.RunStateCompleted && !accepted
	}
	if !valid {
		return typedError(
			ErrForecastOutcomeNotFinal,
			"record forecast outcome",
			errors.New("run and task do not have a compatible final outcome"),
		)
	}
	return nil
}

func buildCurrentPreflightPresentation(
	ctx context.Context,
	queries *sql.Tx,
	input PrepareTaskExecution,
) (string, uint64, error) {
	var taskState domain.TaskState
	var taskRevision uint64
	if err := queries.QueryRowContext(ctx,
		`SELECT state, revision FROM tasks WHERE id = ?`,
		input.TaskID).Scan(&taskState, &taskRevision); err != nil {
		return "", 0, classify("load task for preflight", err)
	}
	if taskState != domain.TaskStateReady ||
		taskRevision != input.ExpectedTaskRevision {
		return "", 0, typedError(ErrExecutionPreflightStale,
			"prepare task execution",
			errors.New("task is not at requested ready revision"))
	}
	policyRevision, err := scanExecutionPolicy(queries.QueryRowContext(ctx,
		`SELECT task_id, revision, policy_version, selection_source,
		        canonical_json, content_sha256, idempotency_key,
		        created_at_unix_micros
		 FROM execution_policy_revisions
		 WHERE task_id = ? AND revision = ?`,
		input.TaskID, input.PolicyRevision), "load preflight policy")
	if err != nil {
		return "", 0, typedError(ErrExecutionPreflightIncomplete,
			"prepare task execution policy", err)
	}
	forecastRevision, err := scanForecast(queries.QueryRowContext(ctx,
		`SELECT task_id, revision, policy_revision, algorithm_version,
		        canonical_json, content_sha256, features_json,
		        features_sha256, counterfactual_eligible, eligibility_json,
		        idempotency_key, created_at_unix_micros
		 FROM effort_forecast_revisions
		 WHERE task_id = ? AND revision = ?`,
		input.TaskID, input.ForecastRevision), "load preflight forecast")
	if err != nil {
		return "", 0, typedError(ErrExecutionPreflightIncomplete,
			"prepare task execution forecast", err)
	}
	if forecastRevision.PolicyRevision != policyRevision.Revision {
		return "", 0, typedError(ErrExecutionPreflightIncomplete,
			"prepare task execution",
			errors.New("forecast is bound to another policy revision"))
	}
	var latestPolicy, latestForecast uint64
	if err := queries.QueryRowContext(ctx,
		`SELECT
			(SELECT MAX(revision) FROM execution_policy_revisions WHERE task_id = ?),
			(SELECT MAX(revision) FROM effort_forecast_revisions WHERE task_id = ?)`,
		input.TaskID, input.TaskID).Scan(
		&latestPolicy, &latestForecast,
	); err != nil {
		return "", 0, classify("load latest preflight revisions", err)
	}
	if latestPolicy != input.PolicyRevision ||
		latestForecast != input.ForecastRevision {
		return "", 0, typedError(ErrExecutionPreflightStale,
			"prepare task execution",
			errors.New("policy or forecast revision is not latest"))
	}
	budgetSnapshot, err := computeBudgetSnapshot(
		ctx,
		queries,
		input.BudgetID,
	)
	if err != nil {
		return "", 0, typedError(ErrExecutionPreflightIncomplete,
			"prepare task execution budget", err)
	}
	if budgetSnapshot.TaskID != input.TaskID {
		return "", 0, typedError(ErrExecutionPreflightIncomplete,
			"prepare task execution",
			errors.New("budget belongs to another task"))
	}
	if budgetSnapshot.LimitRevision != input.BudgetLimitRevision {
		return "", 0, typedError(ErrExecutionPreflightStale,
			"prepare task execution",
			errors.New("budget limit revision is not latest"))
	}
	if budgetSnapshot.HardCapReached ||
		budgetSnapshot.CostAccountingUnknown ||
		budgetSnapshot.TokenAccountingUnknown ||
		budgetSnapshot.ReconciliationPending {
		return "", 0, typedError(
			ErrExecutionPreflightIncomplete,
			"prepare task execution",
			errors.New("budget snapshot is not safe to start"),
		)
	}
	var snapshotPersisted int
	if err := queries.QueryRowContext(
		ctx,
		`SELECT count(*) FROM budget_snapshots
		 WHERE budget_id = ? AND revision = ?`,
		input.BudgetID,
		budgetSnapshot.Revision,
	).Scan(&snapshotPersisted); err != nil {
		return "", 0, classify("check budget snapshot persistence", err)
	}
	if snapshotPersisted == 0 {
		if err := persistBudgetSnapshot(ctx, queries, budgetSnapshot); err != nil {
			return "", 0, err
		}
	}
	presentation, err := marshalExecutionPresentation(
		policyRevision.CanonicalJSON,
		forecastRevision.CanonicalJSON,
		budgetSnapshot,
	)
	if err != nil {
		return "", 0, err
	}
	return presentation, budgetSnapshot.Revision, nil
}

type presentationExactCost struct {
	Known       bool                `json:"known"`
	Numerator   int64               `json:"numerator,omitempty"`
	Denominator int64               `json:"denominator,omitempty"`
	Currency    domain.CurrencyCode `json:"currency,omitempty"`
}

type presentationExactTokens struct {
	Known bool              `json:"known"`
	Value domain.TokenCount `json:"value,omitempty"`
}

func marshalExecutionPresentation(
	policyJSON string,
	forecastJSON string,
	budget BudgetSnapshot,
) (string, error) {
	knownCost := func(value ExactMinorCost) presentationExactCost {
		return presentationExactCost{
			Known: true, Numerator: value.Numerator,
			Denominator: value.Denominator, Currency: value.Currency,
		}
	}
	optionalCost := func(value *ExactMinorCost) presentationExactCost {
		if value == nil {
			return presentationExactCost{}
		}
		return knownCost(*value)
	}
	presentation, err := json.Marshal(struct {
		SchemaVersion uint64          `json:"schema_version"`
		Notice        string          `json:"notice"`
		Policy        json.RawMessage `json:"policy"`
		Forecast      json.RawMessage `json:"forecast"`
		Budget        any             `json:"budget"`
	}{
		SchemaVersion: 1,
		Notice:        forecast.EstimateNotice,
		Policy:        json.RawMessage(policyJSON),
		Forecast:      json.RawMessage(forecastJSON),
		Budget: struct {
			ID                     domain.BudgetID         `json:"id"`
			SnapshotRevision       uint64                  `json:"snapshot_revision"`
			LimitRevision          uint64                  `json:"limit_revision"`
			WarningCost            presentationExactCost   `json:"warning_cost"`
			HardCost               presentationExactCost   `json:"hard_cost"`
			ReservedCost           presentationExactCost   `json:"reserved_cost"`
			ActualCost             presentationExactCost   `json:"actual_cost"`
			RemainingCost          presentationExactCost   `json:"remaining_cost"`
			WarningTokens          domain.TokenCount       `json:"warning_tokens"`
			HardTokens             domain.TokenCount       `json:"hard_tokens"`
			ReservedTokens         presentationExactTokens `json:"reserved_tokens"`
			ActualTokens           presentationExactTokens `json:"actual_tokens"`
			RemainingTokens        presentationExactTokens `json:"remaining_tokens"`
			CostAccountingUnknown  bool                    `json:"cost_accounting_unknown"`
			TokenAccountingUnknown bool                    `json:"token_accounting_unknown"`
			ReconciliationPending  bool                    `json:"reconciliation_pending"`
			HardCapReached         bool                    `json:"hard_cap_reached"`
		}{
			ID:               budget.BudgetID,
			SnapshotRevision: budget.Revision,
			LimitRevision:    budget.LimitRevision,
			WarningCost:      knownCost(budget.WarningCost),
			HardCost:         knownCost(budget.HardCost),
			ReservedCost:     knownCost(budget.ReservedCost),
			ActualCost: func() presentationExactCost {
				if budget.CostAccountingUnknown {
					return presentationExactCost{}
				}
				return knownCost(budget.ActualKnownCost)
			}(),
			RemainingCost: optionalCost(budget.RemainingCost),
			WarningTokens: budget.WarningTokens,
			HardTokens:    budget.HardTokens,
			ReservedTokens: presentationExactTokens{
				Known: true, Value: budget.ReservedTokens,
			},
			ActualTokens: presentationExactTokens{
				Known: !budget.TokenAccountingUnknown,
				Value: budget.ActualTokens,
			},
			RemainingTokens: func() presentationExactTokens {
				if budget.RemainingTokens == nil {
					return presentationExactTokens{}
				}
				return presentationExactTokens{
					Known: true, Value: *budget.RemainingTokens,
				}
			}(),
			CostAccountingUnknown:  budget.CostAccountingUnknown,
			TokenAccountingUnknown: budget.TokenAccountingUnknown,
			ReconciliationPending:  budget.ReconciliationPending,
			HardCapReached:         budget.HardCapReached,
		},
	})
	if err != nil {
		return "", err
	}
	return string(presentation), nil
}

func verifyPreflightStillCurrent(
	ctx context.Context,
	queries queryRower,
	preflight ExecutionPreflight,
) error {
	var (
		policyRevision, forecastRevision, preflightRevision uint64
		limitRevision, snapshotRevision                     uint64
	)
	if err := queries.QueryRowContext(ctx,
		`SELECT
			(SELECT MAX(revision) FROM execution_policy_revisions WHERE task_id = ?),
			(SELECT MAX(revision) FROM effort_forecast_revisions WHERE task_id = ?),
			(SELECT MAX(revision) FROM task_execution_preflights WHERE task_id = ?),
			(SELECT MAX(revision) FROM budget_limit_revisions WHERE budget_id = ?),
			(SELECT revision FROM budgets WHERE id = ?)`,
		preflight.TaskID, preflight.TaskID, preflight.TaskID,
		preflight.BudgetID, preflight.BudgetID).Scan(
		&policyRevision, &forecastRevision, &preflightRevision,
		&limitRevision, &snapshotRevision,
	); err != nil {
		return typedError(ErrExecutionPreflightIncomplete,
			"verify execution preflight", err)
	}
	if policyRevision != preflight.PolicyRevision ||
		forecastRevision != preflight.ForecastRevision ||
		preflightRevision != preflight.Revision ||
		limitRevision != preflight.BudgetLimitRevision ||
		snapshotRevision != preflight.BudgetSnapshotRevision {
		return typedError(ErrExecutionPreflightStale,
			"verify execution preflight",
			errors.New("policy, forecast, preflight, or budget revision changed"))
	}
	return nil
}

func findStartedTaskRun(
	ctx context.Context,
	queries queryRower,
	taskID domain.TaskID,
	key string,
) (StartedTaskRun, bool, error) {
	var (
		started           StartedTaskRun
		createdMicros     int64
		boundTaskRevision uint64
		preflightRevision uint64
	)
	err := queries.QueryRowContext(ctx,
		`SELECT run.id, run.task_id, run.state, run.task_revision,
		        run.attempt, binding.preflight_revision,
		        run.created_at_unix_micros
		 FROM runs AS run
		 JOIN run_execution_bindings AS binding ON binding.run_id = run.id
		 WHERE run.task_id = ? AND run.idempotency_key = ?`,
		taskID, key).Scan(
		&started.RunID, &started.TaskID, &started.State,
		&boundTaskRevision, &started.Attempt, &preflightRevision,
		&createdMicros,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return StartedTaskRun{}, false, nil
	}
	if err != nil {
		return StartedTaskRun{}, false, classify("find started task run", err)
	}
	preflight, err := scanPreflight(queries.QueryRowContext(ctx,
		`SELECT task_id, revision, expected_task_revision, policy_revision,
		        forecast_revision, budget_id, budget_limit_revision,
		        budget_snapshot_revision, presentation_json,
		        content_sha256, idempotency_key,
		        created_at_unix_micros
		 FROM task_execution_preflights
		 WHERE task_id = ? AND revision = ?`,
		taskID, preflightRevision), "load started run preflight")
	if err != nil {
		return StartedTaskRun{}, false, err
	}
	started.Preflight = preflight
	started.TaskRevision = boundTaskRevision + 1
	started.CreatedAt = repositoryTime(createdMicros)
	event, found, err := findTaskEventByIdempotencyKey(
		ctx, queries, taskID, "task.state-transition", key,
	)
	if err != nil {
		return StartedTaskRun{}, false, err
	}
	if found {
		started.TaskEvent = event
	}
	return started, true, nil
}

func findTaskEventByIdempotencyKey(
	ctx context.Context,
	queries queryRower,
	taskID domain.TaskID,
	eventType string,
	runKey string,
) (TaskEvent, bool, error) {
	var event TaskEvent
	var runRaw sql.NullString
	var micros int64
	err := queries.QueryRowContext(ctx,
		`SELECT event.id, event.task_id, event.run_id, event.sequence,
		        event.event_type, event.payload_json, event.idempotency_key,
		        event.created_at_unix_micros
		 FROM task_events AS event
		 JOIN runs AS run ON run.id = event.run_id
		 WHERE event.task_id = ? AND event.event_type = ?
		   AND run.idempotency_key = ?`,
		taskID, eventType, runKey).Scan(
		&event.ID, &event.TaskID, &runRaw, &event.Sequence,
		&event.EventType, &event.PayloadJSON, &event.IdempotencyKey,
		&micros,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return TaskEvent{}, false, nil
	}
	if err != nil {
		return TaskEvent{}, false, classify("find started run event", err)
	}
	if runRaw.Valid {
		runID, err := domain.ParseRunID(runRaw.String)
		if err != nil {
			return TaskEvent{}, false, typedError(ErrCorrupt,
				"parse started run event", err)
		}
		event.RunID = &runID
	}
	event.CreatedAt = repositoryTime(micros)
	return event, true, nil
}

func findExecutionPolicyByIdempotency(
	ctx context.Context,
	queries queryRower,
	taskID domain.TaskID,
	key string,
) (ExecutionPolicyRevision, bool, error) {
	value, err := scanExecutionPolicy(queries.QueryRowContext(ctx,
		`SELECT task_id, revision, policy_version, selection_source,
		        canonical_json, content_sha256, idempotency_key,
		        created_at_unix_micros
		 FROM execution_policy_revisions
		 WHERE task_id = ? AND idempotency_key = ?`,
		taskID, key), "find execution policy")
	if errors.Is(err, ErrNotFound) {
		return ExecutionPolicyRevision{}, false, nil
	}
	return value, err == nil, err
}

func scanExecutionPolicy(row rowScanner, operation string) (ExecutionPolicyRevision, error) {
	var value ExecutionPolicyRevision
	var micros int64
	if err := row.Scan(
		&value.TaskID, &value.Revision, &value.PolicyVersion,
		&value.SelectionSource, &value.CanonicalJSON, &value.ContentSHA256,
		&value.IdempotencyKey, &micros,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ExecutionPolicyRevision{}, typedError(ErrNotFound, operation, err)
		}
		return ExecutionPolicyRevision{}, classify(operation, err)
	}
	value.CreatedAt = repositoryTime(micros)
	return value, nil
}

func findForecastByIdempotency(
	ctx context.Context,
	queries queryRower,
	taskID domain.TaskID,
	key string,
) (EffortForecastRevision, bool, error) {
	value, err := scanForecast(queries.QueryRowContext(ctx,
		`SELECT task_id, revision, policy_revision, algorithm_version,
		        canonical_json, content_sha256, features_json,
		        features_sha256, counterfactual_eligible, eligibility_json,
		        idempotency_key, created_at_unix_micros
		 FROM effort_forecast_revisions
		 WHERE task_id = ? AND idempotency_key = ?`,
		taskID, key), "find effort forecast")
	if errors.Is(err, ErrNotFound) {
		return EffortForecastRevision{}, false, nil
	}
	return value, err == nil, err
}

func scanForecast(row rowScanner, operation string) (EffortForecastRevision, error) {
	var value EffortForecastRevision
	var micros int64
	if err := row.Scan(
		&value.TaskID, &value.Revision, &value.PolicyRevision,
		&value.AlgorithmVersion, &value.CanonicalJSON,
		&value.ContentSHA256, &value.FeaturesJSON,
		&value.FeaturesSHA256, &value.CounterfactualEligible,
		&value.EligibilityJSON, &value.IdempotencyKey, &micros,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return EffortForecastRevision{}, typedError(ErrNotFound, operation, err)
		}
		return EffortForecastRevision{}, classify(operation, err)
	}
	value.CreatedAt = repositoryTime(micros)
	return value, nil
}

func findPreflightByIdempotency(
	ctx context.Context,
	queries queryRower,
	taskID domain.TaskID,
	key string,
) (ExecutionPreflight, bool, error) {
	value, err := scanPreflight(queries.QueryRowContext(ctx,
		`SELECT task_id, revision, expected_task_revision, policy_revision,
		        forecast_revision, budget_id, budget_limit_revision,
		        budget_snapshot_revision, presentation_json,
		        content_sha256, idempotency_key,
		        created_at_unix_micros
		 FROM task_execution_preflights
		 WHERE task_id = ? AND idempotency_key = ?`,
		taskID, key), "find task execution preflight")
	if errors.Is(err, ErrNotFound) {
		return ExecutionPreflight{}, false, nil
	}
	return value, err == nil, err
}

func scanPreflight(row rowScanner, operation string) (ExecutionPreflight, error) {
	var value ExecutionPreflight
	var micros int64
	if err := row.Scan(
		&value.TaskID, &value.Revision, &value.ExpectedTaskRevision,
		&value.PolicyRevision, &value.ForecastRevision, &value.BudgetID,
		&value.BudgetLimitRevision, &value.BudgetSnapshotRevision,
		&value.PresentationJSON,
		&value.ContentSHA256, &value.IdempotencyKey, &micros,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ExecutionPreflight{}, typedError(ErrNotFound, operation, err)
		}
		return ExecutionPreflight{}, classify(operation, err)
	}
	value.CreatedAt = repositoryTime(micros)
	return value, nil
}

func samePreflightInput(value ExecutionPreflight, input PrepareTaskExecution) bool {
	return value.ExpectedTaskRevision == input.ExpectedTaskRevision &&
		value.PolicyRevision == input.PolicyRevision &&
		value.ForecastRevision == input.ForecastRevision &&
		value.BudgetID == input.BudgetID &&
		value.BudgetLimitRevision == input.BudgetLimitRevision
}

func findForecastOutcome(
	ctx context.Context,
	queries queryRower,
	runID domain.RunID,
) (ForecastOutcome, bool, error) {
	var value ForecastOutcome
	var micros int64
	err := queries.QueryRowContext(ctx,
		`SELECT run_id, task_id, forecast_revision, outcome_json,
		        outcome_sha256, comparison_json, comparison_sha256,
		        idempotency_key, created_at_unix_micros
		 FROM forecast_outcomes WHERE run_id = ?`,
		runID).Scan(
		&value.RunID, &value.TaskID, &value.ForecastRevision,
		&value.OutcomeJSON, &value.OutcomeSHA256,
		&value.ComparisonJSON, &value.ComparisonSHA256,
		&value.IdempotencyKey, &micros,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ForecastOutcome{}, false, nil
	}
	if err != nil {
		return ForecastOutcome{}, false, classify("find forecast outcome", err)
	}
	value.CreatedAt = repositoryTime(micros)
	return value, true, nil
}

// TaskForecastRevisions names the newest policy and effort forecast recorded
// for a task.
//
// The two are returned together, read from one row, because a forecast is made
// against a specific policy. Reading the newest of each independently could
// pair a forecast with a policy it was never computed under, and that pair is
// what a person approves and what execution is then bound to.
type TaskForecastRevisions struct {
	PolicyRevision   uint64
	ForecastRevision uint64
}

// GetCurrentTaskForecast returns the newest policy and forecast pair.
//
// It exists because approving a plan has to bind the exact policy and forecast
// the person was shown, and nothing could read them: they were returned once by
// intake and then only reachable by a caller who had held onto the numbers.
func (repositories *Repositories) GetCurrentTaskForecast(
	ctx context.Context,
	taskID domain.TaskID,
) (TaskForecastRevisions, error) {
	if repositories == nil || repositories.database == nil {
		return TaskForecastRevisions{}, errors.New("repositories are unavailable")
	}
	var revisions TaskForecastRevisions
	row := repositories.database.sql.QueryRowContext(ctx,
		`SELECT policy_revision, revision
		 FROM effort_forecast_revisions
		 WHERE task_id = ?
		 ORDER BY revision DESC
		 LIMIT 1`, taskID)
	if err := row.Scan(&revisions.PolicyRevision, &revisions.ForecastRevision); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// A task with no forecast has nothing to approve. Saying so is
			// better than binding execution to a zero revision, which would
			// pass every check and mean nothing.
			return TaskForecastRevisions{}, typedError(ErrNotFound,
				"read current task forecast",
				errors.New("the task has no recorded effort forecast"))
		}
		return TaskForecastRevisions{}, classify("read current task forecast", err)
	}
	return revisions, nil
}
