package storage

import (
	"strings"
	"testing"
	"time"
)

func metricsWindowFixture() MetricsWindow {
	return MetricsWindow{
		From: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

// TestM22_091_099_MetricsQueriesRunAgainstTheRealSchema is the load-bearing
// check for M22-091..099.
//
// Each query is executed against a genuinely migrated database. A metrics
// query that references a renamed column or a table that never existed would
// compile fine and fail only when someone opened the scorecard; running every
// one here means a schema change breaks the build instead.
func TestM22_091_099_MetricsQueriesRunAgainstTheRealSchema(t *testing.T) {
	repositories := openTestRepositories(t)
	window := metricsWindowFixture()

	t.Run("M22-091 task outcomes", func(t *testing.T) {
		result, err := repositories.TaskOutcomeMetrics(t.Context(), window)
		if err != nil {
			t.Fatalf("task outcome metrics: %v", err)
		}
		for name, count := range map[string]Count{
			"started": result.TasksStarted, "completed": result.TasksCompleted,
			"failed": result.TasksFailed, "cancelled": result.TasksCancelled,
			"rolled-back": result.TasksRolledBack,
			"accepted":    result.ChangesAccepted, "rejected": result.ChangesRejected,
		} {
			if !count.Known {
				t.Fatalf("%s count is not known", name)
			}
			if count.Value != 0 {
				t.Fatalf("%s = %d on an empty database", name, count.Value)
			}
		}
	})

	t.Run("M22-092 regressions", func(t *testing.T) {
		result, err := repositories.RegressionMetrics(t.Context(), window)
		if err != nil {
			t.Fatalf("regression metrics: %v", err)
		}
		if !result.ValidationsRun.Known || !result.TasksLeftFailing.Known {
			t.Fatalf("regression metrics returned unknown counts: %+v", result)
		}
	})

	t.Run("M22-093 durations", func(t *testing.T) {
		result, err := repositories.DurationMetrics(t.Context(), window)
		if err != nil {
			t.Fatalf("duration metrics: %v", err)
		}
		// With no events, every milestone must be UNKNOWN rather than zero: a
		// zero mean would claim the system reached the milestone instantly.
		for name, sample := range map[string]DurationSample{
			"plan": result.TimeToPlan, "first action": result.TimeToFirstAction,
			"first diff": result.TimeToFirstDiff, "validation": result.TimeToValidation,
			"completion": result.TimeToCompletion,
		} {
			if sample.Known {
				t.Fatalf("time to %s reported %v with no events recorded", name, sample.Mean)
			}
		}
		if len(result.UnmeasurableReasons) != 5 {
			t.Fatalf("expected a stated reason per unmeasurable milestone, got %d: %v",
				len(result.UnmeasurableReasons), result.UnmeasurableReasons)
		}
	})

	t.Run("M22-094 cost", func(t *testing.T) {
		result, err := repositories.CostMetrics(t.Context(), window)
		if err != nil {
			t.Fatalf("cost metrics: %v", err)
		}
		if !result.InputTokens.Known || !result.CostMinorUnits.Known {
			t.Fatalf("cost metrics returned unknown totals: %+v", result)
		}
		if result.Currency != "" {
			t.Fatalf("currency = %q with no usage records", result.Currency)
		}
	})

	t.Run("M22-095 forecast accuracy", func(t *testing.T) {
		result, err := repositories.ForecastAccuracyMetrics(t.Context(), window)
		if err != nil {
			t.Fatalf("forecast metrics: %v", err)
		}
		if !result.ForecastsIssued.Known || !result.OutcomesRecorded.Known {
			t.Fatalf("forecast metrics returned unknown counts: %+v", result)
		}
	})

	t.Run("M22-096 authority", func(t *testing.T) {
		result, err := repositories.AuthorityMetrics(t.Context(), window)
		if err != nil {
			t.Fatalf("authority metrics: %v", err)
		}
		if !result.ApprovalsRequested.Known || !result.PermissionDenials.Known {
			t.Fatalf("authority metrics returned unknown counts: %+v", result)
		}
	})

	t.Run("M22-097 interruptions", func(t *testing.T) {
		result, err := repositories.InterruptionMetrics(t.Context(), window)
		if err != nil {
			t.Fatalf("interruption metrics: %v", err)
		}
		if !result.RecoveryAttempts.Known || !result.RecoveryDecisions.Known {
			t.Fatalf("interruption metrics returned unknown counts: %+v", result)
		}
	})

	t.Run("M22-098 memory", func(t *testing.T) {
		result, err := repositories.MemoryMetrics(t.Context(), window)
		if err != nil {
			t.Fatalf("memory metrics: %v", err)
		}
		if !result.CandidatesRetrieved.Known || !result.CandidatesAccepted.Known {
			t.Fatalf("memory metrics returned unknown counts: %+v", result)
		}
	})

	t.Run("M22-099 graph usage", func(t *testing.T) {
		result, err := repositories.GraphUsageMetrics(t.Context(), window)
		if err != nil {
			t.Fatalf("graph metrics: %v", err)
		}
		if !result.GraphRevisions.Known || !result.NodesProjected.Known {
			t.Fatalf("graph metrics returned unknown counts: %+v", result)
		}
		// With no revisions the ratio is undefined, and must be reported as
		// unknown rather than as a zero that reads like a collapse.
		if result.NodesPerRevision.Known {
			t.Fatalf("nodes-per-revision reported %d with no revisions",
				result.NodesPerRevision.Value)
		}
	})
}

// TestM22_091_MetricsRejectUnboundedWindows proves no query defaults to
// "all time", which is what makes a scorecard comparable between runs.
func TestM22_091_MetricsRejectUnboundedWindows(t *testing.T) {
	repositories := openTestRepositories(t)
	bad := []MetricsWindow{
		{},
		{From: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		{To: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		{
			From: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
			To:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	}
	for index, window := range bad {
		if _, err := repositories.TaskOutcomeMetrics(t.Context(), window); err == nil {
			t.Fatalf("window %d was accepted by task outcome metrics", index)
		}
		if _, err := repositories.CostMetrics(t.Context(), window); err == nil {
			t.Fatalf("window %d was accepted by cost metrics", index)
		}
		if _, err := repositories.BuildScorecard(t.Context(), window); err == nil {
			t.Fatalf("window %d was accepted by the scorecard", index)
		}
	}
}

// TestM22_093_MilestoneDurationsMeasureRealEvents proves the duration queries
// compute from actual task events rather than always returning unknown, which
// is the failure mode the empty-database test above cannot distinguish.
func TestM22_093_MilestoneDurationsMeasureRealEvents(t *testing.T) {
	repositories, task := createTaskFixture(t, 9300)
	window := metricsWindowFixture()

	created, err := repositories.scalar(t.Context(),
		`SELECT created_at_unix_micros FROM tasks WHERE id = ?`, task.ID.String())
	if err != nil {
		t.Fatalf("read task creation time: %v", err)
	}

	// Two events at known offsets, so the computed mean is checkable rather
	// than merely non-zero.
	events := []struct {
		eventType string
		offset    int64
		sequence  int
	}{
		{"plan-created", 2_000_000, 1},
		{"tool-started", 5_000_000, 2},
	}
	for index, event := range events {
		if _, err := repositories.database.sql.ExecContext(t.Context(),
			`INSERT INTO task_events (
				id, task_id, sequence, event_type, payload_json,
				idempotency_key, created_at_unix_micros
			) VALUES (?, ?, ?, ?, '{}', ?, ?)`,
			"evt_"+event.eventType, task.ID.String(), event.sequence, event.eventType,
			"metrics-fixture-"+event.eventType, created+event.offset,
		); err != nil {
			t.Fatalf("insert event %d: %v", index, err)
		}
	}

	result, err := repositories.DurationMetrics(t.Context(), window)
	if err != nil {
		t.Fatalf("duration metrics: %v", err)
	}
	if !result.TimeToPlan.Known || result.TimeToPlan.Mean != 2*time.Second {
		t.Fatalf("time to plan = %+v, want 2s", result.TimeToPlan)
	}
	if result.TimeToPlan.Sample != 1 {
		t.Fatalf("time to plan sample = %d, want 1", result.TimeToPlan.Sample)
	}
	if !result.TimeToFirstAction.Known || result.TimeToFirstAction.Mean != 5*time.Second {
		t.Fatalf("time to first action = %+v, want 5s", result.TimeToFirstAction)
	}
	// A milestone with no event must stay unknown even when its siblings
	// measured, or an absent step would read as instantaneous.
	if result.TimeToCompletion.Known {
		t.Fatalf("time to completion reported %v with no completion event",
			result.TimeToCompletion.Mean)
	}
}

// TestM22_093_MilestoneDurationsRespectTheWindow proves the window is applied
// rather than accepted and ignored.
func TestM22_093_MilestoneDurationsRespectTheWindow(t *testing.T) {
	repositories, task := createTaskFixture(t, 9350)
	created, err := repositories.scalar(t.Context(),
		`SELECT created_at_unix_micros FROM tasks WHERE id = ?`, task.ID.String())
	if err != nil {
		t.Fatalf("read task creation time: %v", err)
	}
	if _, err := repositories.database.sql.ExecContext(t.Context(),
		`INSERT INTO task_events (
			id, task_id, sequence, event_type, payload_json,
			idempotency_key, created_at_unix_micros
		) VALUES ('evt_windowed', ?, 1, 'plan-created', '{}', 'windowed', ?)`,
		task.ID.String(), created+1_000_000,
	); err != nil {
		t.Fatalf("insert event: %v", err)
	}

	inside, err := repositories.DurationMetrics(t.Context(), metricsWindowFixture())
	if err != nil {
		t.Fatalf("in-window metrics: %v", err)
	}
	if !inside.TimeToPlan.Known {
		t.Fatal("the event inside the window was not measured")
	}

	// A window that ends before the task was created must measure nothing.
	outside, err := repositories.DurationMetrics(t.Context(), MetricsWindow{
		From: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("out-of-window metrics: %v", err)
	}
	if outside.TimeToPlan.Known {
		t.Fatal("a task outside the window was measured")
	}
	if outside.MeasuredTaskCount.Value != 0 {
		t.Fatalf("out-of-window task count = %d", outside.MeasuredTaskCount.Value)
	}
}

// TestM22_100_ScorecardIsRedactedAndComplete covers M22-100.
func TestM22_100_ScorecardIsRedactedAndComplete(t *testing.T) {
	repositories, _ := createTaskFixture(t, 9400)
	card, err := repositories.BuildScorecard(t.Context(), metricsWindowFixture())
	if err != nil {
		t.Fatalf("build scorecard: %v", err)
	}
	if err := card.Validate(); err != nil {
		t.Fatalf("scorecard is invalid: %v", err)
	}
	if card.GeneratedAt.IsZero() {
		t.Fatal("scorecard carries no generation time")
	}
	if card.Outcomes.TasksStarted.Value != 1 {
		t.Fatalf("scorecard counted %d started tasks, want 1", card.Outcomes.TasksStarted.Value)
	}
	// Every surprise must be enumerated text, never user content.
	for _, surprise := range card.Surprises {
		if err := surprise.Validate(); err != nil {
			t.Fatalf("surprise is invalid: %v", err)
		}
		if strings.Contains(surprise.Detail, "Task fixture") {
			t.Fatalf("a surprise leaked fixture content: %q", surprise.Detail)
		}
	}
}

// TestM22_102_SurprisesAreDetectedAndOrdered covers M22-102: the scorecard must
// report what did not fit, and lead with what contradicts an assumption.
func TestM22_102_SurprisesAreDetectedAndOrdered(t *testing.T) {
	card := Scorecard{
		Window:      metricsWindowFixture(),
		GeneratedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		Outcomes: TaskOutcomeMetrics{
			TasksStarted:    knownCount(10),
			TasksCompleted:  knownCount(4),
			ChangesAccepted: knownCount(0),
		},
		Regressions: RegressionMetrics{TasksLeftFailing: knownCount(3)},
		Costs: CostMetrics{
			UsageUnknownCount: knownCount(2),
			CostUnknownCount:  knownCount(1),
		},
		Forecasts: ForecastAccuracyMetrics{
			ForecastsIssued:  knownCount(5),
			OutcomesRecorded: knownCount(0),
		},
		Authority: AuthorityMetrics{
			ApprovalsRequested: knownCount(7),
			ApprovalsGranted:   knownCount(7),
		},
		Memory: MemoryMetrics{
			CandidatesRetrieved: knownCount(12),
			CandidatesAccepted:  knownCount(0),
		},
		Graph: GraphUsageMetrics{
			GraphRevisions: knownCount(3),
			NodesProjected: knownCount(0),
		},
	}
	card.Surprises = card.detectSurprises()

	expected := map[string]SurpriseSeverity{
		"usage-unknown":               SurpriseConcerning,
		"cost-unknown":                SurpriseConcerning,
		"completed-but-unaccepted":    SurpriseConcerning,
		"unresolved-failures":         SurpriseConcerning,
		"graph-collapsed":             SurpriseConcerning,
		"memory-retrieved-never-used": SurpriseNotable,
		"forecasts-never-scored":      SurpriseNotable,
		"every-approval-granted":      SurpriseNotable,
	}
	found := map[string]SurpriseSeverity{}
	for _, surprise := range card.Surprises {
		if err := surprise.Validate(); err != nil {
			t.Fatalf("surprise %q is invalid: %v", surprise.Code, err)
		}
		found[surprise.Code] = surprise.Severity
	}
	for code, severity := range expected {
		got, ok := found[code]
		if !ok {
			t.Fatalf("surprise %q was not detected", code)
		}
		if got != severity {
			t.Fatalf("surprise %q severity = %q, want %q", code, got, severity)
		}
	}

	// Concerning findings must come first: a reader who stops halfway must
	// have seen the ones that contradict an assumption.
	seenNotable := false
	for _, surprise := range card.Surprises {
		if surprise.Severity == SurpriseNotable {
			seenNotable = true
			continue
		}
		if seenNotable {
			t.Fatalf("concerning surprise %q was ordered after a notable one", surprise.Code)
		}
	}

	// A healthy scorecard raises nothing, or the signal is worthless.
	healthy := Scorecard{
		Window:      metricsWindowFixture(),
		GeneratedAt: card.GeneratedAt,
		Outcomes: TaskOutcomeMetrics{
			TasksStarted: knownCount(4), TasksCompleted: knownCount(4),
			ChangesAccepted: knownCount(4),
		},
		Costs: CostMetrics{Currency: "USD", CostMinorUnits: knownCount(100)},
		Authority: AuthorityMetrics{
			ApprovalsRequested: knownCount(5), ApprovalsGranted: knownCount(3),
			ApprovalsDenied: knownCount(2),
		},
		Memory: MemoryMetrics{
			CandidatesRetrieved: knownCount(4), CandidatesAccepted: knownCount(2),
		},
		Graph: GraphUsageMetrics{
			GraphRevisions: knownCount(2), NodesProjected: knownCount(20),
		},
		Forecasts: ForecastAccuracyMetrics{
			ForecastsIssued: knownCount(2), OutcomesRecorded: knownCount(2),
		},
	}
	if surprises := healthy.detectSurprises(); len(surprises) != 0 {
		t.Fatalf("a healthy scorecard raised %d surprises: %+v", len(surprises), surprises)
	}
}

// TestM22_101_ComparisonDistinguishesUnknownFromUnchanged covers M22-101.
//
// The distinction is the whole point: a baseline that never measured a
// dimension is not evidence of parity, and reporting it as "unchanged" would
// let a regression pass review.
func TestM22_101_ComparisonDistinguishesUnknownFromUnchanged(t *testing.T) {
	base := func() Scorecard {
		return Scorecard{
			Window:      metricsWindowFixture(),
			GeneratedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
			Outcomes: TaskOutcomeMetrics{
				TasksStarted: knownCount(12), TasksCompleted: knownCount(8),
				ChangesAccepted: knownCount(6), ChangesRejected: knownCount(1),
				TasksFailed: knownCount(2),
			},
			Regressions: RegressionMetrics{
				TasksLeftFailing: knownCount(1), ValidationsFailed: knownCount(3),
			},
			Costs: CostMetrics{
				CostMinorUnits: knownCount(500), OutputTokens: knownCount(1000),
				RepairAttempts: knownCount(2), Currency: "USD",
			},
			Authority: AuthorityMetrics{
				ApprovalsRequested: knownCount(4), ApprovalsGranted: knownCount(3),
				ApprovalsDenied: knownCount(1),
			},
			Memory: MemoryMetrics{CandidatesAccepted: knownCount(3)},
		}
	}

	run := base()
	baseline := base()
	run.Outcomes.TasksCompleted = knownCount(9)  // better
	run.Costs.CostMinorUnits = knownCount(700)   // worse (lower is better)
	baseline.Memory.CandidatesAccepted = Count{} // not measured by the baseline

	comparison, err := CompareScorecards(run, baseline)
	if err != nil {
		t.Fatalf("compare scorecards: %v", err)
	}
	verdicts := map[string]ComparisonVerdict{}
	for _, line := range comparison.Lines {
		verdicts[line.Dimension] = line.Verdict
	}
	for dimension, want := range map[string]ComparisonVerdict{
		"tasks-completed":  VerdictBetter,
		"cost-minor-units": VerdictWorse,
		"changes-accepted": VerdictUnchanged,
		"memory-accepted":  VerdictNotComparable,
	} {
		if got := verdicts[dimension]; got != want {
			t.Fatalf("%s verdict = %q, want %q", dimension, got, want)
		}
	}

	regressions := comparison.Regressions()
	if len(regressions) != 1 || regressions[0].Dimension != "cost-minor-units" {
		t.Fatalf("regressions = %+v, want only cost-minor-units", regressions)
	}
	// Direction must be recorded, so no reader has to infer whether more is
	// better for a given dimension.
	for _, line := range comparison.Lines {
		if line.Dimension == "tasks-completed" && !line.HigherIsBetter {
			t.Fatal("tasks-completed is not marked higher-is-better")
		}
		if line.Dimension == "tasks-failed" && line.HigherIsBetter {
			t.Fatal("tasks-failed is marked higher-is-better")
		}
	}
}

// TestM22_101_ComparisonRefusesInvalidScorecards proves the comparison will not
// produce a verdict from a scorecard that failed its own consistency check.
func TestM22_101_ComparisonRefusesInvalidScorecards(t *testing.T) {
	valid := Scorecard{
		Window:      metricsWindowFixture(),
		GeneratedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	}
	inconsistent := valid
	inconsistent.Outcomes = TaskOutcomeMetrics{
		TasksStarted:   knownCount(1),
		TasksCompleted: knownCount(5),
	}
	if _, err := CompareScorecards(inconsistent, valid); err == nil {
		t.Fatal("a scorecard counting more terminal tasks than started was compared")
	}
	if _, err := CompareScorecards(valid, inconsistent); err == nil {
		t.Fatal("an inconsistent baseline was compared")
	}

	noWindow := valid
	noWindow.Window = MetricsWindow{}
	if _, err := CompareScorecards(noWindow, valid); err == nil {
		t.Fatal("a scorecard with no window was compared")
	}
}
