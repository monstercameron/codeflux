package storage

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

func TestPrepareLiveProviderSmokeRequestIsAtomicIdempotentAndSecretFree(t *testing.T) {
	ctx := context.Background()
	repositories := openTestRepositories(t)
	input := liveProviderSmokeInputFixture(t)
	first, err := repositories.PrepareLiveProviderSmokeRequest(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	retried, err := repositories.PrepareLiveProviderSmokeRequest(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if first.ProjectID != retried.ProjectID ||
		first.RepositoryID != retried.RepositoryID ||
		first.TaskID != retried.TaskID || first.RunID != retried.RunID ||
		first.ProviderID != retried.ProviderID ||
		first.Request.ID != retried.Request.ID {
		t.Fatalf("live smoke retry created different identities:\nfirst=%#v\nretry=%#v", first, retried)
	}
	if first.Request.State != ProviderLogicalRequestInFlight ||
		first.Request.ProviderVersion != input.ProviderVersion ||
		first.Request.ModelIdentifier != input.ModelIdentifier ||
		first.Request.ModelVersion != input.ModelVersion ||
		first.Pricing.PricingKnown || first.Pricing.Currency != nil {
		t.Fatalf("live smoke attribution = %#v", first)
	}
	var (
		taskState     string
		runState      string
		eventType     string
		credentialRef string
		fixtureCount  int
	)
	if err := repositories.database.sql.QueryRowContext(
		ctx, `SELECT state FROM tasks WHERE id = ?`, first.TaskID,
	).Scan(&taskState); err != nil {
		t.Fatal(err)
	}
	if err := repositories.database.sql.QueryRowContext(
		ctx, `SELECT state FROM runs WHERE id = ?`, first.RunID,
	).Scan(&runState); err != nil {
		t.Fatal(err)
	}
	if err := repositories.database.sql.QueryRowContext(
		ctx, `SELECT event_type FROM task_events WHERE task_id = ?`,
		first.TaskID,
	).Scan(&eventType); err != nil {
		t.Fatal(err)
	}
	if err := repositories.database.sql.QueryRowContext(
		ctx,
		`SELECT opaque_reference FROM provider_credential_references
		 WHERE provider_id = ?`,
		first.ProviderID,
	).Scan(&credentialRef); err != nil {
		t.Fatal(err)
	}
	if err := repositories.database.sql.QueryRowContext(
		ctx, `SELECT count(*) FROM provider_live_smoke_fixtures`,
	).Scan(&fixtureCount); err != nil {
		t.Fatal(err)
	}
	if taskState != "running" || runState != "running" ||
		eventType != "provider.live-smoke.started" ||
		credentialRef != input.OpaqueCredentialReference ||
		fixtureCount != 1 {
		t.Fatalf(
			"live smoke durable fixture task=%q run=%q event=%q credential=%q fixtures=%d",
			taskState, runState, eventType, credentialRef, fixtureCount,
		)
	}
	changed := input
	changed.ModelVersion = "changed-model-version"
	if _, err := repositories.PrepareLiveProviderSmokeRequest(ctx, changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed live smoke idempotency error = %v, want conflict", err)
	}
}

func TestLiveProviderSmokeAttributionReportsAttemptUsageLatencyAndFinalStatus(t *testing.T) {
	ctx := context.Background()
	repositories := openTestRepositories(t)
	input := liveProviderSmokeInputFixture(t)
	smoke, err := repositories.PrepareLiveProviderSmokeRequest(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := repositories.CreateProviderRequestAttempt(
		ctx,
		CreateProviderRequestAttempt{
			ID: "live-smoke-attempt-1", LogicalRequestID: smoke.Request.ID,
			AttemptNumber: 1,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	startedAt := time.Date(2026, 7, 30, 15, 0, 0, 0, time.UTC)
	attempt, err = repositories.TransitionProviderRequestAttempt(
		ctx,
		TransitionProviderRequestAttempt{
			ID: attempt.ID, ExpectedRevision: attempt.Revision,
			From:         ProviderRequestAttemptPrepared,
			To:           ProviderRequestAttemptStarted,
			EffectStatus: ProviderRequestEffectPossible,
			ObservedAt:   startedAt,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err = repositories.TransitionProviderRequestAttempt(
		ctx,
		TransitionProviderRequestAttempt{
			ID: attempt.ID, ExpectedRevision: attempt.Revision,
			From:         ProviderRequestAttemptStarted,
			To:           ProviderRequestAttemptSucceeded,
			EffectStatus: ProviderRequestEffectConfirmed,
			ObservedAt:   startedAt.Add(1250 * time.Millisecond),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.FinalizeLiveProviderSmokeRequest(
		ctx,
		FinalizeLiveProviderSmokeRequest{
			RequestID: smoke.Request.ID, ExpectedRevision: smoke.Request.Revision,
			To:               ProviderLogicalRequestSucceeded,
			AccountingStatus: ProviderAccountingUnknown,
		},
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("missing live smoke usage finalization error = %v, want conflict", err)
	}
	if _, err := repositories.AppendProviderAttemptAccounting(
		ctx,
		AppendProviderAttemptAccounting{
			ID: "live-smoke-accounting-1", AttemptID: attempt.ID,
			Sequence: 1, Source: "provider-final",
			Usage:             domain.TokenUsage{},
			PricingRevisionID: &smoke.Pricing.ID,
			ProvenanceJSON:    `{"source":"live-provider","usage":"unknown"}`,
		},
	); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.FinalizeLiveProviderSmokeRequest(
		ctx,
		FinalizeLiveProviderSmokeRequest{
			RequestID:        smoke.Request.ID,
			ExpectedRevision: smoke.Request.Revision,
			To:               ProviderLogicalRequestFailed,
			AccountingStatus: ProviderAccountingProviderReported,
		},
	); !errors.Is(err, ErrConflict) {
		t.Fatalf(
			"contradictory live smoke finalization error = %v, want conflict",
			err,
		)
	}
	finished, err := repositories.TransitionProviderLogicalRequest(
		ctx,
		TransitionProviderLogicalRequest{
			ID: smoke.Request.ID, ExpectedRevision: smoke.Request.Revision,
			From:             ProviderLogicalRequestInFlight,
			To:               ProviderLogicalRequestSucceeded,
			AccountingStatus: ProviderAccountingUnknown,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	report, err := repositories.FinalizeLiveProviderSmokeRequest(
		ctx,
		FinalizeLiveProviderSmokeRequest{
			RequestID: smoke.Request.ID, ExpectedRevision: finished.Revision,
			To:               ProviderLogicalRequestSucceeded,
			AccountingStatus: ProviderAccountingUnknown,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	retriedReport, err := repositories.FinalizeLiveProviderSmokeRequest(
		ctx,
		FinalizeLiveProviderSmokeRequest{
			RequestID: smoke.Request.ID, ExpectedRevision: finished.Revision,
			To:               ProviderLogicalRequestSucceeded,
			AccountingStatus: ProviderAccountingUnknown,
		},
	)
	if err != nil || retriedReport.Request.ID != report.Request.ID {
		t.Fatalf("idempotent live smoke finalization = %#v, %v", retriedReport, err)
	}
	if report.Request.State != ProviderLogicalRequestSucceeded ||
		report.Request.CompletedAt == nil ||
		report.Request.ProviderVersion != input.ProviderVersion ||
		report.Request.ModelVersion != input.ModelVersion ||
		report.Pricing == nil || report.Pricing.PricingKnown ||
		len(report.Attempts) != 1 ||
		report.Attempts[0].Attempt.State != ProviderRequestAttemptSucceeded ||
		report.Attempts[0].Latency != 1250*time.Millisecond ||
		report.Attempts[0].Accounting == nil ||
		report.Attempts[0].Accounting.Usage.Known ||
		report.Accounting.AccountingComplete ||
		report.Accounting.Cost != nil {
		t.Fatalf("live smoke attribution report = %#v, finished=%#v", report, finished)
	}
	var taskState, runState string
	var finalEvents int
	if err := repositories.database.sql.QueryRowContext(
		ctx, `SELECT state FROM tasks WHERE id = ?`, smoke.TaskID,
	).Scan(&taskState); err != nil {
		t.Fatal(err)
	}
	if err := repositories.database.sql.QueryRowContext(
		ctx, `SELECT state FROM runs WHERE id = ?`, smoke.RunID,
	).Scan(&runState); err != nil {
		t.Fatal(err)
	}
	if err := repositories.database.sql.QueryRowContext(
		ctx,
		`SELECT count(*) FROM task_events
		 WHERE task_id = ? AND event_type = 'provider.live-smoke.succeeded'`,
		smoke.TaskID,
	).Scan(&finalEvents); err != nil {
		t.Fatal(err)
	}
	if taskState != "completed" || runState != "completed" || finalEvents != 1 {
		t.Fatalf("final live smoke task=%q run=%q events=%d", taskState, runState, finalEvents)
	}
}

func TestPrepareLiveProviderSmokeRequestPersistsKnownPricingSnapshot(t *testing.T) {
	repositories := openTestRepositories(t)
	input := liveProviderSmokeInputFixture(t)
	usd := mustCurrencyCode(t, "USD")
	input.Pricing = &LiveProviderSmokePricing{
		Currency: usd, SourceRedacted: "provider published price table",
		EffectiveAt: time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC),
		Components: []ProviderPriceComponent{
			{UsageKind: "input", MinorNumerator: 1, TokenDenominator: 2000},
			{UsageKind: "output", MinorNumerator: 3, TokenDenominator: 1000},
		},
	}
	smoke, err := repositories.PrepareLiveProviderSmokeRequest(
		context.Background(), input,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !smoke.Pricing.PricingKnown || smoke.Pricing.Currency == nil ||
		*smoke.Pricing.Currency != usd || len(smoke.Pricing.Components) != 2 {
		t.Fatalf("known live smoke pricing = %#v", smoke.Pricing)
	}
}

func TestAbortLiveProviderSmokeRequestBeforeIOIsIdempotentAndRejectsPreparedAttempt(t *testing.T) {
	ctx := context.Background()
	repositories := openTestRepositories(t)
	smoke, err := repositories.PrepareLiveProviderSmokeRequest(
		ctx, liveProviderSmokeInputFixture(t),
	)
	if err != nil {
		t.Fatal(err)
	}
	report, err := repositories.AbortLiveProviderSmokeRequestBeforeIO(
		ctx,
		AbortLiveProviderSmokeRequestBeforeIO{
			RequestID: smoke.Request.ID, ExpectedRevision: smoke.Request.Revision,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	retried, err := repositories.AbortLiveProviderSmokeRequestBeforeIO(
		ctx,
		AbortLiveProviderSmokeRequestBeforeIO{
			RequestID: smoke.Request.ID, ExpectedRevision: smoke.Request.Revision,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if report.Request.State != ProviderLogicalRequestFailed ||
		report.Request.AccountingStatus != ProviderAccountingUnknown ||
		retried.Request.ID != report.Request.ID {
		t.Fatalf("aborted live smoke report = %#v, retry = %#v", report, retried)
	}
	var taskState, runState string
	if err := repositories.database.sql.QueryRowContext(
		ctx, `SELECT state FROM tasks WHERE id = ?`, smoke.TaskID,
	).Scan(&taskState); err != nil {
		t.Fatal(err)
	}
	if err := repositories.database.sql.QueryRowContext(
		ctx, `SELECT state FROM runs WHERE id = ?`, smoke.RunID,
	).Scan(&runState); err != nil {
		t.Fatal(err)
	}
	if taskState != "failed" || runState != "failed" {
		t.Fatalf("aborted live smoke task=%q run=%q", taskState, runState)
	}

	secondInput := liveProviderSmokeInputFixture(t)
	secondInput.IdempotencyKey = "prepared-attempt-live-smoke"
	second, err := repositories.PrepareLiveProviderSmokeRequest(ctx, secondInput)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateProviderRequestAttempt(
		ctx,
		CreateProviderRequestAttempt{
			ID:               "prepared-live-smoke-attempt",
			LogicalRequestID: second.Request.ID,
			AttemptNumber:    1,
		},
	); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.AbortLiveProviderSmokeRequestBeforeIO(
		ctx,
		AbortLiveProviderSmokeRequestBeforeIO{
			RequestID: second.Request.ID, ExpectedRevision: second.Request.Revision,
		},
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("abort after durable attempt error = %v, want conflict", err)
	}
}

func TestFinalizeLiveProviderSmokeRequestPausesAfterRetryExhaustion(
	t *testing.T,
) {
	ctx := context.Background()
	repositories := openTestRepositories(t)
	input := liveProviderSmokeInputFixture(t)
	input.IdempotencyKey = "live-smoke-retry-exhausted"
	smoke, err := repositories.PrepareLiveProviderSmokeRequest(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := repositories.CreateProviderRequestAttempt(
		ctx,
		CreateProviderRequestAttempt{
			ID:               "live-smoke-retry-exhausted-attempt",
			LogicalRequestID: smoke.Request.ID,
			AttemptNumber:    1,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	observedAt := time.Now().UTC()
	attempt, err = repositories.TransitionProviderRequestAttempt(
		ctx,
		TransitionProviderRequestAttempt{
			ID: attempt.ID, ExpectedRevision: attempt.Revision,
			From: attempt.State, To: ProviderRequestAttemptStarted,
			EffectStatus: ProviderRequestEffectPossible,
			ObservedAt:   observedAt,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	errorClass := "unavailable"
	attempt, err = repositories.TransitionProviderRequestAttempt(
		ctx,
		TransitionProviderRequestAttempt{
			ID: attempt.ID, ExpectedRevision: attempt.Revision,
			From: attempt.State, To: ProviderRequestAttemptFailed,
			EffectStatus: ProviderRequestEffectPossible,
			ErrorClass:   &errorClass, Retryable: true,
			ObservedAt: observedAt.Add(time.Millisecond),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := repositories.EnsureProviderAttemptAccounting(
		ctx,
		EnsureProviderAttemptAccounting{
			AttemptID:  attempt.ID,
			ObservedAt: observedAt.Add(time.Millisecond),
		},
	); err != nil {
		t.Fatal(err)
	}
	exhausted, err := repositories.TransitionProviderLogicalRequest(
		ctx,
		TransitionProviderLogicalRequest{
			ID:               smoke.Request.ID,
			ExpectedRevision: smoke.Request.Revision,
			From:             ProviderLogicalRequestInFlight,
			To:               ProviderLogicalRequestRetryExhausted,
			AccountingStatus: ProviderAccountingUnknown,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.FinalizeLiveProviderSmokeRequest(
		ctx,
		FinalizeLiveProviderSmokeRequest{
			RequestID:        smoke.Request.ID,
			ExpectedRevision: exhausted.Revision,
			To:               ProviderLogicalRequestRetryExhausted,
			AccountingStatus: ProviderAccountingUnknown,
		},
	); err != nil {
		t.Fatal(err)
	}
	var taskState, runState string
	if err := repositories.database.sql.QueryRowContext(
		ctx,
		`SELECT task.state, run.state
		 FROM tasks AS task JOIN runs AS run ON run.task_id = task.id
		 WHERE task.id = ?`,
		smoke.TaskID,
	).Scan(&taskState, &runState); err != nil {
		t.Fatal(err)
	}
	if taskState != "paused" || runState != "paused" {
		t.Fatalf(
			"retry-exhausted task/run = %q/%q, want paused/paused",
			taskState,
			runState,
		)
	}
}

func TestLiveSmokeStateDerivationPreservesCancellationDuringRetryWait(
	t *testing.T,
) {
	state, terminal := liveSmokeStateFromLastAttempt(
		ProviderLogicalRequestCancelled,
		ProviderRequestAttemptFailed,
		true,
	)
	if !terminal || state != ProviderLogicalRequestCancelled {
		t.Fatalf(
			"retry-wait cancellation derived as %q terminal=%t",
			state,
			terminal,
		)
	}
}

func TestFinalizeLiveProviderSmokeRequestAllowsCancellationBeforeFirstAttempt(t *testing.T) {
	ctx := context.Background()
	repositories := openTestRepositories(t)
	smoke, err := repositories.PrepareLiveProviderSmokeRequest(
		ctx, liveProviderSmokeInputFixture(t),
	)
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := repositories.TransitionProviderLogicalRequest(
		ctx,
		TransitionProviderLogicalRequest{
			ID: smoke.Request.ID, ExpectedRevision: smoke.Request.Revision,
			From:             ProviderLogicalRequestInFlight,
			To:               ProviderLogicalRequestCancelled,
			AccountingStatus: ProviderAccountingUnknown,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	report, err := repositories.FinalizeLiveProviderSmokeRequest(
		ctx,
		FinalizeLiveProviderSmokeRequest{
			RequestID: smoke.Request.ID, ExpectedRevision: cancelled.Revision,
			To:               ProviderLogicalRequestCancelled,
			AccountingStatus: ProviderAccountingUnknown,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if report.Request.State != ProviderLogicalRequestCancelled ||
		len(report.Attempts) != 0 || report.Accounting.AttemptCount != 0 {
		t.Fatalf("pre-attempt cancellation report = %#v", report)
	}
	var taskState, runState string
	if err := repositories.database.sql.QueryRowContext(
		ctx, `SELECT state FROM tasks WHERE id = ?`, smoke.TaskID,
	).Scan(&taskState); err != nil {
		t.Fatal(err)
	}
	if err := repositories.database.sql.QueryRowContext(
		ctx, `SELECT state FROM runs WHERE id = ?`, smoke.RunID,
	).Scan(&runState); err != nil {
		t.Fatal(err)
	}
	if taskState != "cancelled" || runState != "cancelled" {
		t.Fatalf("pre-attempt cancellation task=%q run=%q", taskState, runState)
	}
}

func liveProviderSmokeInputFixture(t *testing.T) PrepareLiveProviderSmokeRequest {
	t.Helper()
	return PrepareLiveProviderSmokeRequest{
		IdempotencyKey:        "explicit-live-smoke-fixture",
		RepositoryPath:        filepath.Join(t.TempDir(), "repository"),
		RepositoryGitIdentity: "git-live-smoke-fixture",
		ProviderType:          "openai", ProviderDisplayName: "OpenAI live smoke",
		AdapterName: "openai-responses", AdapterVersion: "adapter-v1",
		ProviderVersion:           "responses-api-v1",
		EndpointRedacted:          "https://api.example.invalid/v1/responses",
		CapabilitiesJSON:          `{"streaming":true,"tools":true}`,
		OpaqueCredentialReference: "os://openai/live-smoke",
		ModelIdentifier:           "fixture-model", ModelVersion: "fixture-model-v1",
		RequestSHA256: hashFixture("9"),
	}
}
