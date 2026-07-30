package storage

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/forecast"
	"codeflux.dev/codeflux/internal/policy"
	"codeflux.dev/codeflux/internal/providers"
)

func TestExecutionPreflightRoundTripsStartsAndRecordsOutcome(t *testing.T) {
	ctx := t.Context()
	repositories, task := createTaskFixture(t, 2400)
	task = transitionTaskFixtureToReady(t, repositories, task, 2410)
	policyRevision, forecastRevision, budgetID := recordPreflightInputs(
		t, repositories, task, 2420, nil,
	)
	preflight, err := repositories.PrepareTaskExecution(ctx, PrepareTaskExecution{
		TaskID: task.ID, ExpectedTaskRevision: task.Revision,
		PolicyRevision:   policyRevision.Revision,
		ForecastRevision: forecastRevision.Revision,
		BudgetID:         budgetID, BudgetLimitRevision: 0,
		IdempotencyKey: "preflight-happy",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(preflight.PresentationJSON, forecast.EstimateNotice) ||
		!strings.Contains(preflight.PresentationJSON, `"policy"`) ||
		!strings.Contains(preflight.PresentationJSON, `"budget"`) ||
		!strings.Contains(preflight.PresentationJSON, `"snapshot_revision"`) ||
		!strings.Contains(preflight.PresentationJSON, `"reserved_cost"`) ||
		!strings.Contains(preflight.PresentationJSON, `"actual_cost"`) ||
		!strings.Contains(preflight.PresentationJSON, `"remaining_cost"`) ||
		!strings.Contains(preflight.PresentationJSON, `"actual_tokens"`) ||
		!strings.Contains(preflight.PresentationJSON, `"remaining_tokens"`) {
		t.Fatalf("preflight presentation is not inspectable: %s", preflight.PresentationJSON)
	}
	retried, err := repositories.PrepareTaskExecution(ctx, PrepareTaskExecution{
		TaskID: task.ID, ExpectedTaskRevision: task.Revision,
		PolicyRevision:   policyRevision.Revision,
		ForecastRevision: forecastRevision.Revision,
		BudgetID:         budgetID, BudgetLimitRevision: 0,
		IdempotencyKey: "preflight-happy",
	})
	if err != nil || retried != preflight {
		t.Fatalf("preflight retry = %#v, %v", retried, err)
	}

	path := repositories.database.path
	if err := repositories.database.Close(ctx); err != nil {
		t.Fatal(err)
	}
	reopenedDatabase, err := Open(ctx, OpenOptions{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopenedDatabase.Close(context.Background()) })
	reopened, err := NewRepositories(reopenedDatabase, func() time.Time {
		return time.Date(2026, 7, 30, 17, 0, 0, 0, time.UTC)
	})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := reopened.GetTaskExecutionPreflight(ctx, task.ID, preflight.Revision)
	if err != nil || loaded != preflight {
		t.Fatalf("preflight after restart = %#v, %v; want %#v", loaded, err, preflight)
	}

	runID, err := domain.NewRunID()
	if err != nil {
		t.Fatal(err)
	}
	started, err := reopened.StartPreparedTaskRun(ctx, StartPreparedTaskRun{
		RunID: runID, EventID: testEventID(t, 2450), TaskID: task.ID,
		PreflightRevision:    preflight.Revision,
		ExpectedTaskRevision: task.Revision, Attempt: 1,
		IdempotencyKey:      "run-happy",
		EventIdempotencyKey: "run-happy-event",
	})
	if err != nil {
		t.Fatal(err)
	}
	if started.State != domain.RunStateStarting ||
		started.TaskRevision != task.Revision+1 ||
		started.Preflight.ContentSHA256 != preflight.ContentSHA256 ||
		started.TaskEvent.RunID == nil || *started.TaskEvent.RunID != runID {
		t.Fatalf("started run = %#v", started)
	}
	startedAgain, err := reopened.StartPreparedTaskRun(ctx, StartPreparedTaskRun{
		RunID: runID, EventID: testEventID(t, 2450), TaskID: task.ID,
		PreflightRevision:    preflight.Revision,
		ExpectedTaskRevision: task.Revision, Attempt: 1,
		IdempotencyKey:      "run-happy",
		EventIdempotencyKey: "run-happy-event",
	})
	if err != nil || startedAgain.RunID != started.RunID ||
		startedAgain.Preflight != started.Preflight {
		t.Fatalf("start retry = %#v, %v", startedAgain, err)
	}
	changedRetry := StartPreparedTaskRun{
		RunID: runID, EventID: testEventID(t, 2451), TaskID: task.ID,
		PreflightRevision:    preflight.Revision,
		ExpectedTaskRevision: task.Revision, Attempt: 1,
		IdempotencyKey:      "run-happy",
		EventIdempotencyKey: "run-happy-event",
	}
	if _, err := reopened.StartPreparedTaskRun(ctx, changedRetry); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed start event retry error = %v, want conflict", err)
	}

	outcomeInput := RecordForecastOutcome{
		RunID: runID, TaskID: task.ID,
		Actual: forecast.ActualResult{
			LatencyMillis: 100,
			Usage: providers.Usage{
				Known: true, Source: providers.UsageSourceProvider,
				InputTokens: 10, OutputTokens: 5,
			},
			Cost:      providers.UnknownAmount(""),
			ToolCalls: 2, RepairRounds: 1, HumanInterventions: 1,
			Accepted: true,
		},
		IdempotencyKey: "outcome-happy",
	}
	if _, err := reopened.RecordForecastOutcome(
		ctx,
		outcomeInput,
	); !errors.Is(err, ErrForecastOutcomeNotFinal) {
		t.Fatalf("provisional outcome error = %v", err)
	}
	if _, err := reopened.database.sql.ExecContext(
		ctx,
		`UPDATE runs SET state = 'completed', revision = revision + 1
		 WHERE id = ? AND state = 'starting'`,
		runID,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.database.sql.ExecContext(
		ctx,
		`UPDATE tasks SET state = 'completed', revision = revision + 1
		 WHERE id = ? AND state = 'running'`,
		task.ID,
	); err != nil {
		t.Fatal(err)
	}
	outcome, err := reopened.RecordForecastOutcome(ctx, outcomeInput)
	if err != nil {
		t.Fatal(err)
	}
	var comparison forecast.Comparison
	if err := json.Unmarshal([]byte(outcome.ComparisonJSON), &comparison); err != nil {
		t.Fatal(err)
	}
	if !comparison.Latency.Known || !comparison.Tokens.Known ||
		comparison.Cost.Known || !comparison.Actual.Accepted {
		t.Fatalf("forecast comparison = %#v", comparison)
	}
}

func TestTaskExecutionPresentationTracksCurrentBudgetExposure(t *testing.T) {
	t.Run("reserved-and-settled", func(t *testing.T) {
		repositories, task, preflight := preparedTaskFixture(t, 2470)
		before := executionPresentationFixture(t, repositories, task.ID, preflight.Revision)
		if before.Budget.SnapshotRevision != preflight.BudgetSnapshotRevision ||
			before.Budget.ReservedCost.Numerator != 0 ||
			before.Budget.ActualCost.Numerator != 0 ||
			!before.Budget.RemainingCost.Known {
			t.Fatalf("presentation before reserve = %#v", before)
		}
		usd, err := domain.ParseCurrencyCode("USD")
		if err != nil {
			t.Fatal(err)
		}
		tokenBound := domain.TokenCount(20)
		reservation, reserved, err := repositories.ReserveProviderBudget(
			t.Context(),
			ReserveProviderBudget{
				ID: "presentation-reservation", BudgetID: preflight.BudgetID,
				ExpectedRevision: preflight.BudgetSnapshotRevision,
				OperationID:      "presentation-operation",
				Category:         BudgetCostModel, ProviderCallSlots: 1,
				CostBound: ExactMinorCost{
					Numerator: 10, Denominator: 1, Currency: usd,
				},
				TokenBound:     &tokenBound,
				IdempotencyKey: "presentation-reservation",
				ProvenanceJSON: `{"schema_version":1,"source":"test"}`,
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		active := executionPresentationFixture(t, repositories, task.ID, preflight.Revision)
		if active.Budget.SnapshotRevision != reserved.Revision ||
			active.Budget.ReservedCost.Numerator != 10 ||
			active.Budget.ReservedTokens.Value != 20 ||
			active.Budget.ActualCost.Numerator != 0 {
			t.Fatalf("presentation with active reserve = %#v", active)
		}
		actualCost := ExactMinorCost{
			Numerator: 7, Denominator: 2, Currency: usd,
		}
		actualTokens := domain.TokenCount(12)
		settled, err := repositories.SettleProviderBudget(
			t.Context(),
			SettleProviderBudget{
				ID: "presentation-posting", ReservationID: reservation.ID,
				ActualCost: &actualCost, ActualTokens: &actualTokens,
				ActualProviderCallSlots: 1,
				IdempotencyKey:          "presentation-posting",
				ProvenanceJSON:          `{"schema_version":1,"source":"test"}`,
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		actual := executionPresentationFixture(t, repositories, task.ID, preflight.Revision)
		if actual.Budget.SnapshotRevision != settled.Revision ||
			actual.Budget.ReservedCost.Numerator != 0 ||
			!actual.Budget.ActualCost.Known ||
			actual.Budget.ActualCost.Numerator != 7 ||
			actual.Budget.ActualCost.Denominator != 2 ||
			actual.Budget.ActualTokens.Value != 12 {
			t.Fatalf("presentation after settlement = %#v", actual)
		}
	})

	t.Run("unknown-and-reconciliation", func(t *testing.T) {
		repositories, task, preflight := preparedTaskFixture(t, 2490)
		usd, err := domain.ParseCurrencyCode("USD")
		if err != nil {
			t.Fatal(err)
		}
		tokenBound := domain.TokenCount(20)
		reservation, _, err := repositories.ReserveProviderBudget(
			t.Context(),
			ReserveProviderBudget{
				ID:               "presentation-unknown-reservation",
				BudgetID:         preflight.BudgetID,
				ExpectedRevision: preflight.BudgetSnapshotRevision,
				OperationID:      "presentation-unknown-operation",
				Category:         BudgetCostModel, ProviderCallSlots: 1,
				CostBound: ExactMinorCost{
					Numerator: 10, Denominator: 1, Currency: usd,
				},
				TokenBound:     &tokenBound,
				IdempotencyKey: "presentation-unknown-reservation",
				ProvenanceJSON: `{"schema_version":1,"source":"test"}`,
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		reconciling, err := repositories.RecordBudgetReconciliationIntent(
			t.Context(),
			RecordBudgetReconciliationIntent{
				ID:                      "presentation-unknown-intent",
				ReservationID:           reservation.ID,
				ActualProviderCallSlots: 1,
				ReasonRedacted:          "provider usage unavailable",
				IdempotencyKey:          "presentation-unknown-intent",
				ProvenanceJSON:          `{"schema_version":1,"source":"test"}`,
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		unknown := executionPresentationFixture(t, repositories, task.ID, preflight.Revision)
		if unknown.Budget.SnapshotRevision != reconciling.Revision ||
			unknown.Budget.ActualCost.Known ||
			unknown.Budget.ActualTokens.Known ||
			unknown.Budget.RemainingCost.Known ||
			unknown.Budget.RemainingTokens.Known ||
			!unknown.Budget.CostAccountingUnknown ||
			!unknown.Budget.TokenAccountingUnknown ||
			!unknown.Budget.ReconciliationPending {
			t.Fatalf("unknown presentation = %#v", unknown)
		}
	})
}

type executionPresentationJSON struct {
	Budget struct {
		SnapshotRevision uint64 `json:"snapshot_revision"`
		ReservedCost     struct {
			Known       bool  `json:"known"`
			Numerator   int64 `json:"numerator"`
			Denominator int64 `json:"denominator"`
		} `json:"reserved_cost"`
		ActualCost struct {
			Known       bool  `json:"known"`
			Numerator   int64 `json:"numerator"`
			Denominator int64 `json:"denominator"`
		} `json:"actual_cost"`
		RemainingCost struct {
			Known bool `json:"known"`
		} `json:"remaining_cost"`
		ReservedTokens struct {
			Known bool              `json:"known"`
			Value domain.TokenCount `json:"value"`
		} `json:"reserved_tokens"`
		ActualTokens struct {
			Known bool              `json:"known"`
			Value domain.TokenCount `json:"value"`
		} `json:"actual_tokens"`
		RemainingTokens struct {
			Known bool `json:"known"`
		} `json:"remaining_tokens"`
		CostAccountingUnknown  bool `json:"cost_accounting_unknown"`
		TokenAccountingUnknown bool `json:"token_accounting_unknown"`
		ReconciliationPending  bool `json:"reconciliation_pending"`
	} `json:"budget"`
}

func executionPresentationFixture(
	t *testing.T,
	repositories *Repositories,
	taskID domain.TaskID,
	preflightRevision uint64,
) executionPresentationJSON {
	t.Helper()
	presentation, err := repositories.GetTaskExecutionPresentation(
		t.Context(),
		taskID,
		preflightRevision,
	)
	if err != nil {
		t.Fatal(err)
	}
	if presentation.TaskID != taskID ||
		presentation.PreflightRevision != preflightRevision ||
		presentation.PresentationJSON == "" ||
		presentation.ContentSHA256 != hashJSON(presentation.PresentationJSON) {
		t.Fatalf("presentation attribution = %#v", presentation)
	}
	var decoded executionPresentationJSON
	if err := json.Unmarshal([]byte(presentation.PresentationJSON), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Budget.SnapshotRevision != presentation.BudgetSnapshotRevision {
		t.Fatalf("presentation revision = %#v, decoded = %#v", presentation, decoded)
	}
	return decoded
}

func TestStartPreparedTaskRunRejectsMissingAndStaleInputs(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		repositories, task := createTaskFixture(t, 2500)
		task = transitionTaskFixtureToReady(t, repositories, task, 2510)
		_, err := repositories.StartPreparedTaskRun(t.Context(),
			newStartPreparedTaskRun(t, task, 1, 2520, "missing"))
		if !errors.Is(err, ErrExecutionPreflightIncomplete) {
			t.Fatalf("missing preflight error = %v", err)
		}
		assertTaskHasNoRun(t, repositories, task)
	})

	t.Run("policy", func(t *testing.T) {
		repositories, task, preflight := preparedTaskFixture(t, 2600)
		override := policy.ManualOverride{
			Model: providers.ModelIdentity{
				Provider: providers.ProviderIdentity{
					Adapter: "fixture", AdapterVersion: "v2",
					Provider: "fixture", ProviderVersion: "v2",
				},
				Model: "fixture-model-override", Revision: "r2",
			},
			Reasoning: domain.ReasoningEffortExtended,
			Actor:     "test-user", AuthorityReference: "approval-2600",
			Reason: "explicit test override",
		}
		snapshot, err := policy.Select(policy.SelectionInput{
			BaselineModelRevision: baselineFixtureModel().Revision,
			Override:              &override,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := repositories.RecordExecutionPolicy(t.Context(),
			RecordExecutionPolicy{
				TaskID: task.ID, Policy: snapshot,
				IdempotencyKey: "new-policy",
			}); err != nil {
			t.Fatal(err)
		}
		_, err = repositories.StartPreparedTaskRun(t.Context(),
			newStartPreparedTaskRun(t, task, preflight.Revision, 2610, "stale-policy"))
		if !errors.Is(err, ErrExecutionPreflightStale) {
			t.Fatalf("stale policy error = %v", err)
		}
		assertTaskHasNoRun(t, repositories, task)
	})

	t.Run("forecast", func(t *testing.T) {
		repositories, task, preflight := preparedTaskFixture(t, 2700)
		policyRevision, err := scanExecutionPolicy(
			repositories.database.sql.QueryRowContext(t.Context(),
				`SELECT task_id, revision, policy_version, selection_source,
				        canonical_json, content_sha256, idempotency_key,
				        created_at_unix_micros
				 FROM execution_policy_revisions
				 WHERE task_id = ? AND revision = ?`,
				task.ID, preflight.PolicyRevision),
			"test policy",
		)
		if err != nil {
			t.Fatal(err)
		}
		var snapshot policy.Snapshot
		if err := json.Unmarshal([]byte(policyRevision.CanonicalJSON), &snapshot); err != nil {
			t.Fatal(err)
		}
		value := generateFixtureForecast(t, snapshot, []string{"changed.go"})
		eligibility, err := forecast.NewCounterfactualEligibility(true, []string{"fixed-policy-task"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := repositories.RecordEffortForecast(t.Context(),
			RecordEffortForecast{
				TaskID: task.ID, PolicyRevision: preflight.PolicyRevision,
				Forecast: value, Eligibility: eligibility,
				IdempotencyKey: "new-forecast",
			}); err != nil {
			t.Fatal(err)
		}
		_, err = repositories.StartPreparedTaskRun(t.Context(),
			newStartPreparedTaskRun(t, task, preflight.Revision, 2710, "stale-forecast"))
		if !errors.Is(err, ErrExecutionPreflightStale) {
			t.Fatalf("stale forecast error = %v", err)
		}
		assertTaskHasNoRun(t, repositories, task)
	})

	t.Run("budget", func(t *testing.T) {
		repositories, task, preflight := preparedTaskFixture(t, 2800)
		if _, err := repositories.database.sql.ExecContext(t.Context(),
			`INSERT INTO budget_limit_revisions (
				budget_id, revision,
				warning_cost_minor_numerator, warning_cost_minor_denominator,
				hard_cost_minor_numerator, hard_cost_minor_denominator,
				currency, warning_tokens, hard_tokens, approval_id,
				authority_kind, actor_kind, actor_reference, reason_redacted,
				idempotency_key, provenance_json, created_at_unix_micros
			)
			SELECT budget_id, 1,
			       warning_cost_minor_numerator, warning_cost_minor_denominator,
			       hard_cost_minor_numerator + 1, hard_cost_minor_denominator,
			       currency, warning_tokens, hard_tokens, NULL,
			       'initial-policy', 'system', 'test-budget-revision',
			       'test approved limit revision', 'test-limit-revision',
			       '{"schema_version":1,"source":"test"}',
			       created_at_unix_micros + 1
			FROM budget_limit_revisions
			WHERE budget_id = ? AND revision = 0`,
			preflight.BudgetID); err != nil {
			t.Fatal(err)
		}
		_, err := repositories.StartPreparedTaskRun(t.Context(),
			newStartPreparedTaskRun(t, task, preflight.Revision, 2810, "stale-budget"))
		if !errors.Is(err, ErrExecutionPreflightStale) {
			t.Fatalf("stale budget error = %v", err)
		}
		assertTaskHasNoRun(t, repositories, task)
	})

	t.Run("budget-snapshot", func(t *testing.T) {
		repositories, task, preflight := preparedTaskFixture(t, 2820)
		currency, err := domain.ParseCurrencyCode("USD")
		if err != nil {
			t.Fatal(err)
		}
		amount, err := domain.NewMoney(currency, 1)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := repositories.ReserveBudget(t.Context(), ReserveBudget{
			ID: preflight.BudgetID, ExpectedRevision: preflight.BudgetSnapshotRevision,
			Amount: amount,
		}); err != nil {
			t.Fatal(err)
		}
		_, err = repositories.StartPreparedTaskRun(t.Context(),
			newStartPreparedTaskRun(t, task, preflight.Revision, 2830, "stale-budget-snapshot"))
		if !errors.Is(err, ErrExecutionPreflightStale) {
			t.Fatalf("stale budget snapshot error = %v", err)
		}
		assertTaskHasNoRun(t, repositories, task)
	})
}

func TestPrepareTaskExecutionRejectsEachMissingDependency(t *testing.T) {
	repositories, task := createTaskFixture(t, 2850)
	task = transitionTaskFixtureToReady(t, repositories, task, 2860)
	policyRevision, forecastRevision, budgetID := recordPreflightInputs(
		t, repositories, task, 2870, nil,
	)
	missingBudgetID := testBudgetID(t, 2890)
	tests := []struct {
		name             string
		policyRevision   uint64
		forecastRevision uint64
		budgetID         domain.BudgetID
	}{
		{
			name: "policy", policyRevision: policyRevision.Revision + 100,
			forecastRevision: forecastRevision.Revision, budgetID: budgetID,
		},
		{
			name: "forecast", policyRevision: policyRevision.Revision,
			forecastRevision: forecastRevision.Revision + 100, budgetID: budgetID,
		},
		{
			name: "budget", policyRevision: policyRevision.Revision,
			forecastRevision: forecastRevision.Revision, budgetID: missingBudgetID,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := repositories.PrepareTaskExecution(t.Context(),
				PrepareTaskExecution{
					TaskID: task.ID, ExpectedTaskRevision: task.Revision,
					PolicyRevision:   test.policyRevision,
					ForecastRevision: test.forecastRevision,
					BudgetID:         test.budgetID, BudgetLimitRevision: 0,
					IdempotencyKey: "missing-" + test.name,
				})
			if !errors.Is(err, ErrExecutionPreflightIncomplete) {
				t.Fatalf("missing %s error = %v", test.name, err)
			}
			var count int
			if err := repositories.database.sql.QueryRowContext(t.Context(),
				`SELECT count(*) FROM task_execution_preflights
				 WHERE task_id = ?`,
				task.ID).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Fatalf("missing %s persisted %d preflights", test.name, count)
			}
		})
	}
}

func TestReadyTaskGenericTransitionCannotBypassExecutionPreflight(t *testing.T) {
	repositories, task := createTaskFixture(t, 2900)
	task = transitionTaskFixtureToReady(t, repositories, task, 2910)
	_, err := repositories.TransitionTask(t.Context(), TransitionTask{
		EventID: testEventID(t, 2920), TaskID: task.ID,
		ExpectedRevision: task.Revision,
		From:             domain.TaskStateReady, To: domain.TaskStateRunning,
		IdempotencyKey: "bypass-start",
	})
	if !errors.Is(err, ErrExecutionPreflightIncomplete) {
		t.Fatalf("generic start bypass error = %v", err)
	}
	assertTaskHasNoRun(t, repositories, task)
}

func TestRecordEffortForecastRejectsMutatedPolicyBinding(t *testing.T) {
	repositories, task := createTaskFixture(t, 2950)
	snapshot, err := policy.Select(policy.SelectionInput{
		BaselineModelRevision: "forecast-validation-revision",
	})
	if err != nil {
		t.Fatal(err)
	}
	policyRevision, err := repositories.RecordExecutionPolicy(
		t.Context(),
		RecordExecutionPolicy{
			TaskID: task.ID, Policy: snapshot,
			IdempotencyKey: "validated-policy",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	value := generateFixtureForecast(t, snapshot, []string{"mutated.go"})
	value.Bindings.Reasoning = domain.ReasoningEffortMinimal
	eligibility, err := forecast.NewCounterfactualEligibility(
		true,
		[]string{"fixed-policy-task"},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = repositories.RecordEffortForecast(
		t.Context(),
		RecordEffortForecast{
			TaskID: task.ID, PolicyRevision: policyRevision.Revision,
			Forecast: value, Eligibility: eligibility,
			IdempotencyKey: "mutated-forecast",
		},
	)
	if !errors.Is(err, ErrExecutionPreflightIncomplete) {
		t.Fatalf("mutated forecast persistence error = %v", err)
	}
}

func preparedTaskFixture(
	t *testing.T,
	base int,
) (*Repositories, Task, ExecutionPreflight) {
	t.Helper()
	repositories, task := createTaskFixture(t, base)
	task = transitionTaskFixtureToReady(t, repositories, task, base+10)
	policyRevision, forecastRevision, budgetID := recordPreflightInputs(
		t, repositories, task, base+20, nil,
	)
	preflight, err := repositories.PrepareTaskExecution(t.Context(),
		PrepareTaskExecution{
			TaskID: task.ID, ExpectedTaskRevision: task.Revision,
			PolicyRevision:   policyRevision.Revision,
			ForecastRevision: forecastRevision.Revision,
			BudgetID:         budgetID, BudgetLimitRevision: 0,
			IdempotencyKey: "prepared-task",
		})
	if err != nil {
		t.Fatal(err)
	}
	return repositories, task, preflight
}

func recordPreflightInputs(
	t *testing.T,
	repositories *Repositories,
	task Task,
	base int,
	override *policy.ManualOverride,
) (ExecutionPolicyRevision, EffortForecastRevision, domain.BudgetID) {
	t.Helper()
	snapshot, err := policy.Select(policy.SelectionInput{
		BaselineModelRevision: baselineFixtureModel().Revision,
		Override:              override,
	})
	if err != nil {
		t.Fatal(err)
	}
	policyRevision, err := repositories.RecordExecutionPolicy(t.Context(),
		RecordExecutionPolicy{
			TaskID: task.ID, Policy: snapshot,
			IdempotencyKey: "fixture-policy",
		})
	if err != nil {
		t.Fatal(err)
	}
	value := generateFixtureForecast(t, snapshot, []string{"internal/example.go"})
	eligibility, err := forecast.NewCounterfactualEligibility(
		true, []string{"fixed-policy-task"},
	)
	if err != nil {
		t.Fatal(err)
	}
	forecastRevision, err := repositories.RecordEffortForecast(t.Context(),
		RecordEffortForecast{
			TaskID: task.ID, PolicyRevision: policyRevision.Revision,
			Forecast: value, Eligibility: eligibility,
			IdempotencyKey: "fixture-forecast",
		})
	if err != nil {
		t.Fatal(err)
	}
	budgetID := testBudgetID(t, base)
	budget, err := snapshot.BudgetDefaults.Materialize(budgetID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateBudget(t.Context(), CreateBudget{
		TaskID: task.ID, Budget: budget,
	}); err != nil {
		t.Fatal(err)
	}
	return policyRevision, forecastRevision, budgetID
}

func generateFixtureForecast(
	t *testing.T,
	snapshot policy.Snapshot,
	likelyFiles []string,
) forecast.Forecast {
	t.Helper()
	value, err := forecast.Generate(forecast.Input{
		RepositoryRevision:       "fixture-revision",
		TaskFingerprint:          "fixture-fingerprint",
		TaskClass:                forecast.TaskClassFeature,
		RepositorySize:           forecast.RepositorySize{Files: 100, Bytes: 1024},
		LikelyFiles:              likelyFiles,
		ValidationCommands:       []string{"go test ./..."},
		Policy:                   snapshot,
		ToolConfigurationVersion: "tools-v1",
		ValidationProfileVersion: "validation-v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func baselineFixtureModel() providers.ModelIdentity {
	return providers.ModelIdentity{
		Provider: providers.ProviderIdentity{
			Adapter: "fixture", AdapterVersion: "v1",
			Provider: "fixture", ProviderVersion: "v1",
		},
		Model: "fixture-model", Revision: "r1",
	}
}

func transitionTaskFixtureToReady(
	t *testing.T,
	repositories *Repositories,
	task Task,
	base int,
) Task {
	t.Helper()
	for index, transition := range []struct {
		from domain.TaskState
		to   domain.TaskState
	}{
		{domain.TaskStateDraft, domain.TaskStateForecasting},
		{domain.TaskStateForecasting, domain.TaskStateAwaitingPlanApproval},
		{domain.TaskStateAwaitingPlanApproval, domain.TaskStateReady},
	} {
		changed, err := repositories.TransitionTask(t.Context(), TransitionTask{
			EventID: testEventID(t, base+index), TaskID: task.ID,
			ExpectedRevision: task.Revision,
			From:             transition.from, To: transition.to,
			Approval: func() domain.ApprovalRequestState {
				if transition.from == domain.TaskStateAwaitingPlanApproval {
					return domain.ApprovalRequestStateGranted
				}
				return ""
			}(),
			IdempotencyKey: "ready-transition-" + string(rune('a'+index)),
		})
		if err != nil {
			t.Fatal(err)
		}
		task = changed.Task
	}
	return task
}

func newStartPreparedTaskRun(
	t *testing.T,
	task Task,
	preflightRevision uint64,
	eventNumber int,
	key string,
) StartPreparedTaskRun {
	t.Helper()
	runID, err := domain.NewRunID()
	if err != nil {
		t.Fatal(err)
	}
	return StartPreparedTaskRun{
		RunID: runID, EventID: testEventID(t, eventNumber),
		TaskID: task.ID, PreflightRevision: preflightRevision,
		ExpectedTaskRevision: task.Revision, Attempt: 1,
		IdempotencyKey: key, EventIdempotencyKey: key + "-event",
	}
}

func assertTaskHasNoRun(
	t *testing.T,
	repositories *Repositories,
	task Task,
) {
	t.Helper()
	var state domain.TaskState
	var count int
	if err := repositories.database.sql.QueryRowContext(t.Context(),
		`SELECT state, (SELECT count(*) FROM runs WHERE task_id = tasks.id)
		 FROM tasks WHERE id = ?`,
		task.ID).Scan(&state, &count); err != nil {
		t.Fatal(err)
	}
	if state != domain.TaskStateReady || count != 0 {
		t.Fatalf("failed start changed task/run: state=%s runs=%d", state, count)
	}
}
