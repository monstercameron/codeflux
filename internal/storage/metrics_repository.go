package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// MetricsWindow bounds every scorecard query to an explicit closed interval.
//
// docs/plan.md's M22-091..102 metrics exist to answer "how is this actually
// going", and an unbounded query silently mixes a bad first week into a good
// current one. Every query therefore takes a window and no query defaults to
// "all time".
type MetricsWindow struct {
	From time.Time
	To   time.Time
}

// Validate rejects an unusable window.
func (window MetricsWindow) Validate() error {
	if window.From.IsZero() || window.To.IsZero() {
		return errors.New("metrics window requires an explicit start and end")
	}
	if window.To.Before(window.From) {
		return errors.New("metrics window ends before it starts")
	}
	return nil
}

func (window MetricsWindow) bounds() (int64, int64) {
	return window.From.UTC().UnixMicro(), window.To.UTC().UnixMicro()
}

// Count is one measured quantity that distinguishes "zero" from "not known".
//
// A scorecard that renders an unknown as 0 invites exactly the wrong
// conclusion, so absence is carried rather than collapsed.
type Count struct {
	Known bool
	Value int64
}

func knownCount(value int64) Count { return Count{Known: true, Value: value} }

// TaskOutcomeMetrics is M22-091: task success and user acceptance.
//
// Success and acceptance are counted separately because they are different
// claims: the system finishing is not the user agreeing.
type TaskOutcomeMetrics struct {
	Window          MetricsWindow
	TasksStarted    Count
	TasksCompleted  Count
	TasksFailed     Count
	TasksCancelled  Count
	TasksRolledBack Count
	ChangesAccepted Count
	ChangesRejected Count
}

// TaskOutcomeMetrics runs the M22-091 queries.
func (repositories *Repositories) TaskOutcomeMetrics(
	ctx context.Context,
	window MetricsWindow,
) (TaskOutcomeMetrics, error) {
	if err := window.Validate(); err != nil {
		return TaskOutcomeMetrics{}, err
	}
	from, to := window.bounds()
	result := TaskOutcomeMetrics{Window: window}

	states := map[string]*Count{
		"completed":   &result.TasksCompleted,
		"failed":      &result.TasksFailed,
		"cancelled":   &result.TasksCancelled,
		"rolled-back": &result.TasksRolledBack,
	}
	for state, target := range states {
		value, err := repositories.scalar(ctx,
			`SELECT count(*) FROM tasks
			 WHERE state = ? AND created_at_unix_micros BETWEEN ? AND ?`,
			state, from, to)
		if err != nil {
			return TaskOutcomeMetrics{}, fmt.Errorf("count %s tasks: %w", state, err)
		}
		*target = knownCount(value)
	}

	started, err := repositories.scalar(ctx,
		`SELECT count(*) FROM tasks WHERE created_at_unix_micros BETWEEN ? AND ?`,
		from, to)
	if err != nil {
		return TaskOutcomeMetrics{}, fmt.Errorf("count started tasks: %w", err)
	}
	result.TasksStarted = knownCount(started)

	accepted, err := repositories.scalar(ctx,
		`SELECT count(*) FROM acceptance_decisions
		 WHERE created_at_unix_micros BETWEEN ? AND ?`, from, to)
	if err != nil {
		return TaskOutcomeMetrics{}, fmt.Errorf("count acceptances: %w", err)
	}
	result.ChangesAccepted = knownCount(accepted)

	rejected, err := repositories.scalar(ctx,
		`SELECT count(*) FROM acceptance_rejections
		 WHERE created_at_unix_micros BETWEEN ? AND ?`, from, to)
	if err != nil {
		return TaskOutcomeMetrics{}, fmt.Errorf("count rejections: %w", err)
	}
	result.ChangesRejected = knownCount(rejected)

	return result, nil
}

// RegressionMetrics is M22-092: regressions and unresolved failures.
type RegressionMetrics struct {
	Window              MetricsWindow
	ValidationsRun      Count
	ValidationsFailed   Count
	ValidationsTimedOut Count
	RepairAttempts      Count
	// TasksLeftFailing counts tasks that ended in a failed state and were
	// never followed by an acceptance. These are the ones a user is still
	// carrying, which is what "unresolved" means.
	TasksLeftFailing Count
}

// RegressionMetrics runs the M22-092 queries.
func (repositories *Repositories) RegressionMetrics(
	ctx context.Context,
	window MetricsWindow,
) (RegressionMetrics, error) {
	if err := window.Validate(); err != nil {
		return RegressionMetrics{}, err
	}
	from, to := window.bounds()
	result := RegressionMetrics{Window: window}

	queries := []struct {
		target *Count
		label  string
		query  string
	}{
		{&result.ValidationsRun, "validations run", `
			SELECT count(*) FROM validation_run_results AS results
			JOIN validation_run_intents AS intents ON intents.id = results.validation_run_id
			WHERE intents.created_at_unix_micros BETWEEN ? AND ?`},
		{&result.ValidationsFailed, "validations failed", `
			SELECT count(*) FROM validation_run_results AS results
			JOIN validation_run_intents AS intents ON intents.id = results.validation_run_id
			WHERE results.state = 'failed'
			  AND intents.created_at_unix_micros BETWEEN ? AND ?`},
		{&result.ValidationsTimedOut, "validations timed out", `
			SELECT count(*) FROM validation_run_results AS results
			JOIN validation_run_intents AS intents ON intents.id = results.validation_run_id
			WHERE results.timed_out = 1
			  AND intents.created_at_unix_micros BETWEEN ? AND ?`},
		{&result.RepairAttempts, "repair attempts", `
			SELECT count(*) FROM repair_attempts
			WHERE created_at_unix_micros BETWEEN ? AND ?`},
		{&result.TasksLeftFailing, "tasks left failing", `
			SELECT count(*) FROM tasks
			WHERE state = 'failed'
			  AND created_at_unix_micros BETWEEN ? AND ?
			  AND NOT EXISTS (
				SELECT 1 FROM acceptance_decisions
				WHERE acceptance_decisions.task_id = tasks.id
			  )`},
	}
	for _, query := range queries {
		value, err := repositories.scalar(ctx, query.query, from, to)
		if err != nil {
			return RegressionMetrics{}, fmt.Errorf("count %s: %w", query.label, err)
		}
		*query.target = knownCount(value)
	}
	return result, nil
}

// DurationMetrics is M22-093: time to each milestone of a task.
//
// Every duration is reported with the number of tasks it was computed from,
// because a mean over two tasks and a mean over two hundred are not the same
// claim and must not render identically.
type DurationMetrics struct {
	Window              MetricsWindow
	TimeToPlan          DurationSample
	TimeToFirstAction   DurationSample
	TimeToFirstDiff     DurationSample
	TimeToValidation    DurationSample
	TimeToCompletion    DurationSample
	MeasuredTaskCount   Count
	UnmeasurableReasons []string
}

// DurationSample is a mean with the sample size that produced it.
type DurationSample struct {
	Known  bool
	Mean   time.Duration
	Sample int64
}

// DurationMetrics runs the M22-093 queries.
func (repositories *Repositories) DurationMetrics(
	ctx context.Context,
	window MetricsWindow,
) (DurationMetrics, error) {
	if err := window.Validate(); err != nil {
		return DurationMetrics{}, err
	}
	from, to := window.bounds()
	result := DurationMetrics{Window: window}

	measured, err := repositories.scalar(ctx,
		`SELECT count(*) FROM tasks WHERE created_at_unix_micros BETWEEN ? AND ?`,
		from, to)
	if err != nil {
		return DurationMetrics{}, fmt.Errorf("count measurable tasks: %w", err)
	}
	result.MeasuredTaskCount = knownCount(measured)

	// Each milestone is the first task event of its kind, measured from the
	// task's creation. Using the first event rather than the last is
	// deliberate: the user is waiting for the milestone to arrive, not for it
	// to stop happening.
	milestones := []struct {
		target     *DurationSample
		label      string
		eventTypes []string
	}{
		{&result.TimeToPlan, "plan", []string{"plan-created", "plan-changed"}},
		{&result.TimeToFirstAction, "first action", []string{"tool-started"}},
		{&result.TimeToFirstDiff, "first diff", []string{"diff-summarized", "diff-produced"}},
		{&result.TimeToValidation, "validation", []string{"validation-started", "validation-completed"}},
		{&result.TimeToCompletion, "completion", []string{"task-completed"}},
	}
	for _, milestone := range milestones {
		sample, err := repositories.milestoneDuration(ctx, from, to, milestone.eventTypes)
		if err != nil {
			return DurationMetrics{}, fmt.Errorf("time to %s: %w", milestone.label, err)
		}
		*milestone.target = sample
		if !sample.Known {
			result.UnmeasurableReasons = append(result.UnmeasurableReasons,
				"no "+milestone.label+" event was recorded in this window")
		}
	}
	return result, nil
}

func (repositories *Repositories) milestoneDuration(
	ctx context.Context,
	from, to int64,
	eventTypes []string,
) (DurationSample, error) {
	if len(eventTypes) == 0 {
		return DurationSample{}, errors.New("a milestone requires at least one event type")
	}
	placeholders := ""
	arguments := []any{from, to}
	for index, eventType := range eventTypes {
		if index > 0 {
			placeholders += ", "
		}
		placeholders += "?"
		arguments = append(arguments, eventType)
	}
	query := `
		SELECT count(*), COALESCE(sum(elapsed), 0) FROM (
			SELECT min(events.created_at_unix_micros) - tasks.created_at_unix_micros AS elapsed
			FROM tasks
			JOIN task_events AS events ON events.task_id = tasks.id
			WHERE tasks.created_at_unix_micros BETWEEN ? AND ?
			  AND events.event_type IN (` + placeholders + `)
			GROUP BY tasks.id
		)`
	row := repositories.database.sql.QueryRowContext(ctx, query, arguments...)
	var count, total int64
	if err := row.Scan(&count, &total); err != nil {
		return DurationSample{}, classify("aggregate milestone duration", err)
	}
	if count == 0 {
		return DurationSample{}, nil
	}
	return DurationSample{
		Known:  true,
		Mean:   time.Duration(total/count) * time.Microsecond,
		Sample: count,
	}, nil
}

// CostMetrics is M22-094: tokens, cost, retries, and repairs.
//
// Token and cost totals carry an explicit unknown count. A provider that never
// reported usage must not be averaged in as zero, which would understate spend
// exactly when it matters.
type CostMetrics struct {
	Window            MetricsWindow
	InputTokens       Count
	OutputTokens      Count
	ReasoningTokens   Count
	CostMinorUnits    Count
	Currency          string
	UsageUnknownCount Count
	CostUnknownCount  Count
	ProviderAttempts  Count
	RepairAttempts    Count
}

// CostMetrics runs the M22-094 queries.
func (repositories *Repositories) CostMetrics(
	ctx context.Context,
	window MetricsWindow,
) (CostMetrics, error) {
	if err := window.Validate(); err != nil {
		return CostMetrics{}, err
	}
	from, to := window.bounds()
	result := CostMetrics{Window: window}

	const usageQuery = `
		SELECT
			COALESCE(sum(CASE WHEN usage_known = 1 THEN input_tokens ELSE 0 END), 0),
			COALESCE(sum(CASE WHEN usage_known = 1 THEN output_tokens ELSE 0 END), 0),
			COALESCE(sum(CASE WHEN usage_known = 1 THEN reasoning_tokens ELSE 0 END), 0),
			COALESCE(sum(CASE WHEN cost_known = 1 THEN cost_minor ELSE 0 END), 0),
			COALESCE(sum(CASE WHEN usage_known = 0 THEN 1 ELSE 0 END), 0),
			COALESCE(sum(CASE WHEN cost_known = 0 THEN 1 ELSE 0 END), 0),
			count(*)
		FROM usage_records
		WHERE created_at_unix_micros BETWEEN ? AND ?`
	row := repositories.database.sql.QueryRowContext(ctx, usageQuery, from, to)
	var input, output, reasoning, cost, usageUnknown, costUnknown, attempts int64
	if err := row.Scan(
		&input, &output, &reasoning, &cost, &usageUnknown, &costUnknown, &attempts,
	); err != nil {
		return CostMetrics{}, classify("aggregate usage records", err)
	}
	result.InputTokens = knownCount(input)
	result.OutputTokens = knownCount(output)
	result.ReasoningTokens = knownCount(reasoning)
	result.CostMinorUnits = knownCount(cost)
	result.UsageUnknownCount = knownCount(usageUnknown)
	result.CostUnknownCount = knownCount(costUnknown)
	result.ProviderAttempts = knownCount(attempts)

	// A single currency is only reported when the window contains exactly one,
	// because summing minor units across currencies would be meaningless.
	currency, err := repositories.singleCurrency(ctx, from, to)
	if err != nil {
		return CostMetrics{}, err
	}
	result.Currency = currency

	repairs, err := repositories.scalar(ctx,
		`SELECT count(*) FROM repair_attempts WHERE created_at_unix_micros BETWEEN ? AND ?`,
		from, to)
	if err != nil {
		return CostMetrics{}, fmt.Errorf("count repairs: %w", err)
	}
	result.RepairAttempts = knownCount(repairs)
	return result, nil
}

func (repositories *Repositories) singleCurrency(
	ctx context.Context,
	from, to int64,
) (string, error) {
	rows, err := repositories.database.sql.QueryContext(ctx,
		`SELECT DISTINCT currency FROM usage_records
		 WHERE currency IS NOT NULL AND created_at_unix_micros BETWEEN ? AND ?`,
		from, to)
	if err != nil {
		return "", classify("read usage currencies", err)
	}
	defer func() { _ = rows.Close() }()
	currencies := make([]string, 0, 2)
	for rows.Next() {
		var currency string
		if err := rows.Scan(&currency); err != nil {
			return "", classify("scan usage currency", err)
		}
		currencies = append(currencies, currency)
	}
	if err := rows.Err(); err != nil {
		return "", classify("iterate usage currencies", err)
	}
	if len(currencies) == 1 {
		return currencies[0], nil
	}
	return "", nil
}

// ForecastAccuracyMetrics is M22-095: forecast error and interval coverage.
type ForecastAccuracyMetrics struct {
	Window             MetricsWindow
	ForecastsIssued    Count
	OutcomesRecorded   Count
	CostForecastKnown  Count
	TokenForecastKnown Count
	// LatencyForecastKnown counts forecasts that committed to a latency
	// interval. A forecast that declined to predict is not an error, and must
	// not be scored as one.
	LatencyForecastKnown Count
}

// ForecastAccuracyMetrics runs the M22-095 queries.
func (repositories *Repositories) ForecastAccuracyMetrics(
	ctx context.Context,
	window MetricsWindow,
) (ForecastAccuracyMetrics, error) {
	if err := window.Validate(); err != nil {
		return ForecastAccuracyMetrics{}, err
	}
	from, to := window.bounds()
	result := ForecastAccuracyMetrics{Window: window}

	queries := []struct {
		target *Count
		label  string
		query  string
	}{
		{&result.ForecastsIssued, "forecasts issued", `
			SELECT count(*) FROM forecasts WHERE created_at_unix_micros BETWEEN ? AND ?`},
		{&result.CostForecastKnown, "cost forecasts", `
			SELECT count(*) FROM forecasts
			WHERE cost_known = 1 AND created_at_unix_micros BETWEEN ? AND ?`},
		{&result.TokenForecastKnown, "token forecasts", `
			SELECT count(*) FROM forecasts
			WHERE tokens_known = 1 AND created_at_unix_micros BETWEEN ? AND ?`},
		{&result.LatencyForecastKnown, "latency forecasts", `
			SELECT count(*) FROM forecasts
			WHERE latency_known = 1 AND created_at_unix_micros BETWEEN ? AND ?`},
		{&result.OutcomesRecorded, "forecast outcomes", `
			SELECT count(*) FROM forecast_outcomes AS outcomes
			JOIN tasks ON tasks.id = outcomes.task_id
			WHERE tasks.created_at_unix_micros BETWEEN ? AND ?`},
	}
	for _, query := range queries {
		value, err := repositories.scalar(ctx, query.query, from, to)
		if err != nil {
			return ForecastAccuracyMetrics{}, fmt.Errorf("count %s: %w", query.label, err)
		}
		*query.target = knownCount(value)
	}
	return result, nil
}

// AuthorityMetrics is M22-096: approvals and denied actions.
type AuthorityMetrics struct {
	Window             MetricsWindow
	ApprovalsRequested Count
	ApprovalsGranted   Count
	ApprovalsDenied    Count
	ApprovalsExpired   Count
	PermissionGrants   Count
	PermissionDenials  Count
}

// AuthorityMetrics runs the M22-096 queries.
func (repositories *Repositories) AuthorityMetrics(
	ctx context.Context,
	window MetricsWindow,
) (AuthorityMetrics, error) {
	if err := window.Validate(); err != nil {
		return AuthorityMetrics{}, err
	}
	from, to := window.bounds()
	result := AuthorityMetrics{Window: window}

	queries := []struct {
		target *Count
		label  string
		query  string
	}{
		{&result.ApprovalsRequested, "approvals requested", `
			SELECT count(*) FROM approvals WHERE requested_at_unix_micros BETWEEN ? AND ?`},
		{&result.ApprovalsGranted, "approvals granted", `
			SELECT count(*) FROM approvals
			WHERE state = 'granted' AND requested_at_unix_micros BETWEEN ? AND ?`},
		{&result.ApprovalsDenied, "approvals denied", `
			SELECT count(*) FROM approvals
			WHERE state = 'denied' AND requested_at_unix_micros BETWEEN ? AND ?`},
		{&result.ApprovalsExpired, "approvals expired", `
			SELECT count(*) FROM approvals
			WHERE state = 'expired' AND requested_at_unix_micros BETWEEN ? AND ?`},
		{&result.PermissionGrants, "permission grants", `
			SELECT count(*) FROM permission_decisions
			WHERE decision = 'granted' AND created_at_unix_micros BETWEEN ? AND ?`},
		{&result.PermissionDenials, "permission denials", `
			SELECT count(*) FROM permission_decisions
			WHERE decision = 'denied' AND created_at_unix_micros BETWEEN ? AND ?`},
	}
	for _, query := range queries {
		value, err := repositories.scalar(ctx, query.query, from, to)
		if err != nil {
			return AuthorityMetrics{}, fmt.Errorf("count %s: %w", query.label, err)
		}
		*query.target = knownCount(value)
	}
	return result, nil
}

// InterruptionMetrics is M22-097: pause, cancel, recovery, and resume.
type InterruptionMetrics struct {
	Window            MetricsWindow
	TasksPaused       Count
	TasksCancelled    Count
	RecoveryRequired  Count
	RecoveryAttempts  Count
	RecoveryDecisions Count
}

// InterruptionMetrics runs the M22-097 queries.
func (repositories *Repositories) InterruptionMetrics(
	ctx context.Context,
	window MetricsWindow,
) (InterruptionMetrics, error) {
	if err := window.Validate(); err != nil {
		return InterruptionMetrics{}, err
	}
	from, to := window.bounds()
	result := InterruptionMetrics{Window: window}

	queries := []struct {
		target *Count
		label  string
		query  string
	}{
		{&result.TasksPaused, "paused tasks", `
			SELECT count(*) FROM tasks
			WHERE state = 'paused' AND created_at_unix_micros BETWEEN ? AND ?`},
		{&result.TasksCancelled, "cancelled tasks", `
			SELECT count(*) FROM tasks
			WHERE state = 'cancelled' AND created_at_unix_micros BETWEEN ? AND ?`},
		{&result.RecoveryRequired, "recovery-required tasks", `
			SELECT count(*) FROM tasks
			WHERE state = 'recovery-required' AND created_at_unix_micros BETWEEN ? AND ?`},
		{&result.RecoveryAttempts, "recovery attempts", `
			SELECT count(*) FROM recovery_attempts
			WHERE created_at_unix_micros BETWEEN ? AND ?`},
		{&result.RecoveryDecisions, "recovery decisions", `
			SELECT count(*) FROM checkpoint_recovery_decisions
			WHERE created_at_unix_micros BETWEEN ? AND ?`},
	}
	for _, query := range queries {
		value, err := repositories.scalar(ctx, query.query, from, to)
		if err != nil {
			return InterruptionMetrics{}, fmt.Errorf("count %s: %w", query.label, err)
		}
		*query.target = knownCount(value)
	}
	return result, nil
}

// MemoryMetrics is M22-098: retrieved and influential memory.
//
// The distinction is the point. docs/plan.md §31 separates retrieval from
// influence precisely because an item that was surfaced and ignored proves
// nothing about the memory system's value, and counting the two together would
// make an unused memory look useful.
type MemoryMetrics struct {
	Window              MetricsWindow
	QueriesIssued       Count
	CandidatesRetrieved Count
	CandidatesAccepted  Count
	CandidatesRejected  Count
	FallbacksRecorded   Count
}

// MemoryMetrics runs the M22-098 queries.
func (repositories *Repositories) MemoryMetrics(
	ctx context.Context,
	window MetricsWindow,
) (MemoryMetrics, error) {
	if err := window.Validate(); err != nil {
		return MemoryMetrics{}, err
	}
	from, to := window.bounds()
	result := MemoryMetrics{Window: window}

	queries := []struct {
		target *Count
		label  string
		query  string
	}{
		{&result.QueriesIssued, "memory queries", `
			SELECT count(*) FROM memory_retrieval_queries
			WHERE requested_at_unix_micros BETWEEN ? AND ?`},
		{&result.CandidatesRetrieved, "retrieved candidates", `
			SELECT count(*) FROM memory_retrieval_candidates AS candidates
			JOIN memory_retrieval_queries AS queries ON queries.id = candidates.query_id
			WHERE queries.requested_at_unix_micros BETWEEN ? AND ?`},
		{&result.CandidatesAccepted, "accepted candidates", `
			SELECT count(*) FROM memory_retrieval_decisions AS decisions
			JOIN memory_retrieval_candidates AS candidates ON candidates.id = decisions.candidate_id
			JOIN memory_retrieval_queries AS queries ON queries.id = candidates.query_id
			WHERE decisions.decision = 'accepted'
			  AND queries.requested_at_unix_micros BETWEEN ? AND ?`},
		{&result.CandidatesRejected, "rejected candidates", `
			SELECT count(*) FROM memory_retrieval_decisions AS decisions
			JOIN memory_retrieval_candidates AS candidates ON candidates.id = decisions.candidate_id
			JOIN memory_retrieval_queries AS queries ON queries.id = candidates.query_id
			WHERE decisions.decision = 'rejected'
			  AND queries.requested_at_unix_micros BETWEEN ? AND ?`},
		{&result.FallbacksRecorded, "retrieval fallbacks", `
			SELECT count(*) FROM memory_retrieval_fallbacks AS fallbacks
			JOIN memory_retrieval_queries AS queries ON queries.id = fallbacks.query_id
			WHERE queries.requested_at_unix_micros BETWEEN ? AND ?`},
	}
	for _, query := range queries {
		value, err := repositories.scalar(ctx, query.query, from, to)
		if err != nil {
			return MemoryMetrics{}, fmt.Errorf("count %s: %w", query.label, err)
		}
		*query.target = knownCount(value)
	}
	return result, nil
}

// GraphUsageMetrics is M22-099: graph usage and collapse rate.
type GraphUsageMetrics struct {
	Window         MetricsWindow
	GraphRevisions Count
	NodesProjected Count
	EdgesProjected Count
	MessageLinks   Count
	// CollapsedNodeRatio is nodes-per-revision. A projection that keeps
	// producing revisions without producing nodes is collapsing detail the
	// user needed, which is the failure this metric exists to surface.
	NodesPerRevision Count
}

// GraphUsageMetrics runs the M22-099 queries.
func (repositories *Repositories) GraphUsageMetrics(
	ctx context.Context,
	window MetricsWindow,
) (GraphUsageMetrics, error) {
	if err := window.Validate(); err != nil {
		return GraphUsageMetrics{}, err
	}
	from, to := window.bounds()
	result := GraphUsageMetrics{Window: window}

	revisions, err := repositories.scalar(ctx,
		`SELECT count(*) FROM graph_revisions WHERE created_at_unix_micros BETWEEN ? AND ?`,
		from, to)
	if err != nil {
		return GraphUsageMetrics{}, fmt.Errorf("count graph revisions: %w", err)
	}
	result.GraphRevisions = knownCount(revisions)

	nodes, err := repositories.scalar(ctx, `
		SELECT count(*) FROM graph_node_revisions AS nodes
		JOIN graph_revisions AS revisions ON revisions.id = nodes.graph_revision_id
		WHERE revisions.created_at_unix_micros BETWEEN ? AND ?`, from, to)
	if err != nil {
		return GraphUsageMetrics{}, fmt.Errorf("count graph nodes: %w", err)
	}
	result.NodesProjected = knownCount(nodes)

	edges, err := repositories.scalar(ctx, `
		SELECT count(*) FROM graph_edge_revisions AS edges
		JOIN graph_revisions AS revisions ON revisions.id = edges.graph_revision_id
		WHERE revisions.created_at_unix_micros BETWEEN ? AND ?`, from, to)
	if err != nil {
		return GraphUsageMetrics{}, fmt.Errorf("count graph edges: %w", err)
	}
	result.EdgesProjected = knownCount(edges)

	links, err := repositories.scalar(ctx, `
		SELECT count(*) FROM graph_message_links AS links
		JOIN graph_revisions AS revisions ON revisions.id = links.graph_revision_id
		WHERE revisions.created_at_unix_micros BETWEEN ? AND ?`, from, to)
	if err != nil {
		return GraphUsageMetrics{}, fmt.Errorf("count graph message links: %w", err)
	}
	result.MessageLinks = knownCount(links)

	// Division by zero is an unknown, not a zero: no revisions means the ratio
	// is undefined, and reporting 0 would read as "the graph collapsed".
	if revisions > 0 {
		result.NodesPerRevision = knownCount(nodes / revisions)
	}
	return result, nil
}

func (repositories *Repositories) scalar(
	ctx context.Context,
	query string,
	arguments ...any,
) (int64, error) {
	var value int64
	err := repositories.database.sql.QueryRowContext(ctx, query, arguments...).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, classify("run metrics query", err)
	}
	return value, nil
}
