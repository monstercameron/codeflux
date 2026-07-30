package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

func TestProviderConfigurationRevisionsAreOptimisticIdempotentAndImmutable(t *testing.T) {
	ctx := context.Background()
	repositories := openTestRepositories(t)
	providerID := seedProviderAccountingProvider(t, repositories, 1)
	firstInput := CreateProviderConfigurationRevision{
		ID: "provider-config-1", ProviderID: providerID,
		ExpectedLatestRevision: 0,
		AdapterName:            "openai-responses", AdapterVersion: "adapter-v1",
		ProviderVersion:  "responses-v1",
		EndpointRedacted: "https://api.example.invalid",
		CapabilitiesJSON: `{"streaming":true,"tools":true}`,
		ContentSHA256:    hashFixture("1"), IdempotencyKey: "config-one",
	}
	first, err := repositories.CreateProviderConfigurationRevision(ctx, firstInput)
	if err != nil {
		t.Fatal(err)
	}
	retried, err := repositories.CreateProviderConfigurationRevision(ctx, firstInput)
	if err != nil {
		t.Fatal(err)
	}
	if first != retried || first.Revision != 1 {
		t.Fatalf("idempotent configuration revisions = %#v, %#v", first, retried)
	}
	changed := firstInput
	changed.EndpointRedacted = "https://changed.example.invalid"
	if _, err := repositories.CreateProviderConfigurationRevision(ctx, changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed idempotent configuration error = %v, want conflict", err)
	}
	secondInput := firstInput
	secondInput.ID = "provider-config-2"
	secondInput.IdempotencyKey = "config-two"
	secondInput.ContentSHA256 = hashFixture("2")
	if _, err := repositories.CreateProviderConfigurationRevision(ctx, secondInput); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("stale configuration revision error = %v, want stale revision", err)
	}
	secondInput.ExpectedLatestRevision = 1
	second, err := repositories.CreateProviderConfigurationRevision(ctx, secondInput)
	if err != nil {
		t.Fatal(err)
	}
	if second.Revision != 2 {
		t.Fatalf("second configuration revision = %d, want 2", second.Revision)
	}
	if _, err := repositories.database.sql.ExecContext(
		ctx,
		`UPDATE provider_configuration_revisions
		 SET endpoint_redacted = 'https://rewritten.invalid'
		 WHERE id = ?`,
		first.ID,
	); !errors.Is(classify("rewrite provider configuration", err), ErrConstraint) {
		t.Fatalf("configuration immutability error = %v, want constraint", err)
	}
}

func TestAbortPreparedProviderRequestAttemptBeforeIOIsTerminalAccountedAndIdempotent(
	t *testing.T,
) {
	ctx := context.Background()
	repositories, task := createTaskFixture(t, 950)
	providerID, configuration, pricing := seedProviderRequestDependencies(
		t, repositories, 951,
	)
	request := planAndStartProviderRequest(
		t, repositories, task.ID, providerID, configuration, pricing, 952,
	)
	attempt, err := repositories.CreateProviderRequestAttempt(
		ctx,
		CreateProviderRequestAttempt{
			ID:               "pre-io-abort-attempt",
			LogicalRequestID: request.ID,
			AttemptNumber:    1,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	observedAt := time.Now().UTC()
	input := AbortPreparedProviderRequestAttemptBeforeIO{
		ID: attempt.ID, ExpectedRevision: attempt.Revision,
		ObservedAt: observedAt,
	}
	aborted, err := repositories.AbortPreparedProviderRequestAttemptBeforeIO(
		ctx,
		input,
	)
	if err != nil {
		t.Fatal(err)
	}
	retried, err := repositories.AbortPreparedProviderRequestAttemptBeforeIO(
		ctx,
		input,
	)
	if err != nil {
		t.Fatal(err)
	}
	if aborted.State != ProviderRequestAttemptCancelled ||
		aborted.EffectStatus != ProviderRequestEffectNone ||
		aborted.StartedAt != nil ||
		aborted.CompletedAt != nil ||
		retried.ID != aborted.ID ||
		retried.Revision != aborted.Revision {
		t.Fatalf("pre-I/O aborted attempt = %#v, retry = %#v", aborted, retried)
	}
	report, err := repositories.GetProviderRequestAttribution(ctx, request.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Attempts) != 1 ||
		report.Attempts[0].Accounting == nil ||
		report.Attempts[0].Accounting.Usage.Known ||
		report.Attempts[0].Accounting.Cost != nil ||
		report.Attempts[0].Accounting.Partial ||
		report.Attempts[0].Accounting.PricingRevisionID == nil ||
		*report.Attempts[0].Accounting.PricingRevisionID != pricing.ID {
		t.Fatalf("pre-I/O abort attribution = %#v", report)
	}
}

func TestEnsureProviderAttemptAccountingAddsIdempotentUnknownFallback(t *testing.T) {
	ctx := context.Background()
	repositories, task := createTaskFixture(t, 960)
	providerID, configuration, pricing := seedProviderRequestDependencies(
		t, repositories, 961,
	)
	request := planAndStartProviderRequest(
		t, repositories, task.ID, providerID, configuration, pricing, 962,
	)
	attempt, err := repositories.CreateProviderRequestAttempt(
		ctx,
		CreateProviderRequestAttempt{
			ID:               "missing-accounting-attempt",
			LogicalRequestID: request.ID,
			AttemptNumber:    1,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	attempt = transitionProviderAttemptForTest(
		t,
		repositories,
		attempt,
		ProviderRequestAttemptStarted,
		ProviderRequestEffectPossible,
		false,
		time.Now().UTC(),
	)
	input := EnsureProviderAttemptAccounting{
		AttemptID:  attempt.ID,
		ObservedAt: time.Now().UTC(),
	}
	if err := repositories.EnsureProviderAttemptAccounting(ctx, input); err != nil {
		t.Fatal(err)
	}
	if err := repositories.EnsureProviderAttemptAccounting(ctx, input); err != nil {
		t.Fatal(err)
	}
	report, err := repositories.GetProviderRequestAttribution(ctx, request.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Attempts) != 1 ||
		report.Attempts[0].Accounting == nil ||
		report.Attempts[0].Accounting.Usage.Known ||
		report.Attempts[0].Accounting.Cost != nil ||
		report.Attempts[0].Accounting.PricingRevisionID == nil ||
		*report.Attempts[0].Accounting.PricingRevisionID != pricing.ID {
		t.Fatalf("fallback accounting = %#v", report)
	}
	var count int
	if err := repositories.database.sql.QueryRowContext(
		ctx,
		`SELECT count(*) FROM provider_attempt_accounting WHERE attempt_id = ?`,
		attempt.ID,
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("fallback accounting rows = %d, want 1", count)
	}
}

func TestProviderPricingRevisionsPreserveExactRationalComponentsAndUnknownPrice(t *testing.T) {
	ctx := context.Background()
	repositories := openTestRepositories(t)
	providerID := seedProviderAccountingProvider(t, repositories, 10)
	usd := mustCurrencyCode(t, "USD")
	knownInput := CreateProviderPricingRevision{
		ID: "pricing-known", ProviderID: providerID,
		ModelIdentifier: "model-a", ModelVersion: "2026-07-30",
		PricingKnown: true, Currency: &usd,
		EffectiveAt: time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC),
		Components: []ProviderPriceComponent{
			{UsageKind: "input", MinorNumerator: 1, TokenDenominator: 3},
			{UsageKind: "output", MinorNumerator: 7, TokenDenominator: 10},
		},
	}
	known, err := repositories.CreateProviderPricingRevision(ctx, knownInput)
	if err != nil {
		t.Fatal(err)
	}
	retried, err := repositories.CreateProviderPricingRevision(ctx, knownInput)
	if err != nil {
		t.Fatal(err)
	}
	if !providerPricingMatches(retried, knownInput) ||
		len(known.Components) != 2 ||
		known.Components[0].TokenDenominator != 3 {
		t.Fatalf("pricing round trip = %#v, retry %#v", known, retried)
	}
	if _, err := repositories.database.sql.ExecContext(
		ctx,
		`INSERT INTO provider_price_components (
			pricing_revision_id, usage_kind, minor_numerator, token_denominator
		) VALUES (?, 'reasoning', 1, 1)`,
		known.ID,
	); !errors.Is(
		classify("append sealed provider price component", err),
		ErrConstraint,
	) {
		t.Fatalf("append sealed price error = %v, want constraint", err)
	}
	if _, err := repositories.database.sql.ExecContext(
		ctx,
		`INSERT INTO provider_pricing_revisions (
			id, provider_id, model_identifier, model_version, currency,
			pricing_known, effective_at_unix_micros, created_at_unix_micros
		) VALUES (
			'unsealed-pricing', ?, 'model-unsealed', 'v1', 'USD', 1, 1, 1
		)`,
		providerID,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.database.sql.ExecContext(
		ctx,
		`INSERT INTO provider_pricing_revision_seals (
			pricing_revision_id, component_count, sealed_at_unix_micros
		) VALUES ('unsealed-pricing', 0, 1)`,
	); !errors.Is(
		classify("seal incomplete provider pricing", err),
		ErrConstraint,
	) {
		t.Fatalf("incomplete pricing seal error = %v, want constraint", err)
	}
	unknown, err := repositories.CreateProviderPricingRevision(
		ctx,
		CreateProviderPricingRevision{
			ID: "pricing-unknown", ProviderID: providerID,
			ModelIdentifier: "model-unknown", ModelVersion: "endpoint-reported",
			PricingKnown: false,
			EffectiveAt:  time.Date(2026, 7, 30, 1, 0, 0, 0, time.UTC),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if unknown.PricingKnown || unknown.Currency != nil || len(unknown.Components) != 0 {
		t.Fatalf("unknown pricing became numeric: %#v", unknown)
	}
	if _, err := repositories.database.sql.ExecContext(
		ctx,
		`INSERT INTO provider_price_components (
			pricing_revision_id, usage_kind, minor_numerator, token_denominator
		) VALUES (?, 'input', 0, 1)`,
		unknown.ID,
	); !errors.Is(classify("attach price to unknown pricing", err), ErrConstraint) {
		t.Fatalf("unknown pricing component error = %v, want constraint", err)
	}
	if _, err := repositories.database.sql.ExecContext(
		ctx,
		`UPDATE provider_price_components
		 SET minor_numerator = 99
		 WHERE pricing_revision_id = ? AND usage_kind = 'input'`,
		known.ID,
	); !errors.Is(classify("rewrite price component", err), ErrConstraint) {
		t.Fatalf("price component immutability error = %v, want constraint", err)
	}
}

func TestProviderLogicalRequestAndPhysicalAttemptLifecycleIsAttributable(t *testing.T) {
	ctx := context.Background()
	repositories, task := createTaskFixture(t, 1000)
	providerID, configuration, pricing := seedProviderRequestDependencies(
		t, repositories, 1001,
	)
	requestID, err := domain.NewModelRequestID()
	if err != nil {
		t.Fatal(err)
	}
	plan := PlanProviderLogicalRequest{
		ID: requestID, TaskID: task.ID, ProviderID: providerID,
		ProviderConfigurationRevisionID: configuration.ID,
		AdapterName:                     configuration.AdapterName,
		AdapterVersion:                  configuration.AdapterVersion,
		ProviderVersion:                 configuration.ProviderVersion,
		ModelIdentifier:                 pricing.ModelIdentifier,
		ModelVersion:                    pricing.ModelVersion, PricingRevisionID: &pricing.ID,
		RequestSHA256: hashFixture("3"), IdempotencyKey: "logical-request-one",
	}
	mismatchedPlan := plan
	mismatchedPlan.ID, err = domain.NewModelRequestID()
	if err != nil {
		t.Fatal(err)
	}
	mismatchedPlan.AdapterVersion = "different-adapter-version"
	mismatchedPlan.IdempotencyKey = "mismatched-provider-identity"
	if _, err := repositories.PlanProviderLogicalRequest(ctx, mismatchedPlan); !errors.Is(err, ErrConstraint) {
		t.Fatalf("mismatched provider identity error = %v, want constraint", err)
	}
	request, err := repositories.PlanProviderLogicalRequest(ctx, plan)
	if err != nil {
		t.Fatal(err)
	}
	retried, err := repositories.PlanProviderLogicalRequest(ctx, plan)
	if err != nil {
		t.Fatal(err)
	}
	if !providerLogicalRequestMatches(retried, plan) ||
		request.ID != retried.ID || request.CreatedAt != retried.CreatedAt {
		t.Fatalf("idempotent logical request = %#v, %#v", request, retried)
	}
	changedPlan := plan
	changedPlan.RequestSHA256 = hashFixture("4")
	if _, err := repositories.PlanProviderLogicalRequest(ctx, changedPlan); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed logical retry error = %v, want conflict", err)
	}
	request, err = repositories.TransitionProviderLogicalRequest(
		ctx,
		TransitionProviderLogicalRequest{
			ID: request.ID, ExpectedRevision: request.Revision,
			From:             ProviderLogicalRequestPlanned,
			To:               ProviderLogicalRequestInFlight,
			AccountingStatus: ProviderAccountingUnknown,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.TransitionProviderLogicalRequest(
		ctx,
		TransitionProviderLogicalRequest{
			ID: request.ID, ExpectedRevision: 0,
			From:             ProviderLogicalRequestPlanned,
			To:               ProviderLogicalRequestCancelled,
			AccountingStatus: ProviderAccountingUnknown,
		},
	); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("stale logical transition error = %v, want stale revision", err)
	}

	idempotency := "provider-stable-request-key"
	first, err := repositories.CreateProviderRequestAttempt(
		ctx,
		CreateProviderRequestAttempt{
			ID: "physical-attempt-1", LogicalRequestID: request.ID,
			AttemptNumber: 1, RequestIdempotencyKey: &idempotency,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	retriedAttempt, err := repositories.CreateProviderRequestAttempt(
		ctx,
		CreateProviderRequestAttempt{
			ID: "physical-attempt-1", LogicalRequestID: request.ID,
			AttemptNumber: 1, RequestIdempotencyKey: &idempotency,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if retriedAttempt.ID != first.ID {
		t.Fatalf("idempotent physical attempt = %#v, %#v", first, retriedAttempt)
	}
	if _, err := repositories.CreateProviderRequestAttempt(
		ctx,
		CreateProviderRequestAttempt{
			ID: "physical-attempt-other", LogicalRequestID: request.ID,
			AttemptNumber: 1, RequestIdempotencyKey: &idempotency,
		},
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate physical attempt error = %v, want conflict", err)
	}
	now := time.Date(2026, 7, 30, 12, 30, 0, 0, time.UTC)
	first = transitionProviderAttemptForTest(
		t, repositories, first, ProviderRequestAttemptStarted,
		ProviderRequestEffectNone, false, now,
	)
	retriedStart, err := repositories.TransitionProviderRequestAttempt(
		ctx,
		TransitionProviderRequestAttempt{
			ID: first.ID, ExpectedRevision: 0,
			From:         ProviderRequestAttemptPrepared,
			To:           ProviderRequestAttemptStarted,
			EffectStatus: ProviderRequestEffectNone, ObservedAt: now,
		},
	)
	if err != nil || retriedStart.Revision != first.Revision {
		t.Fatalf("idempotent physical transition = %#v, %v", retriedStart, err)
	}
	if _, err := repositories.TransitionProviderRequestAttempt(
		ctx,
		TransitionProviderRequestAttempt{
			ID: first.ID, ExpectedRevision: 0,
			From:         ProviderRequestAttemptPrepared,
			To:           ProviderRequestAttemptStarted,
			EffectStatus: ProviderRequestEffectNone,
			ObservedAt:   now.Add(time.Microsecond),
		},
	); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf(
			"changed idempotent start timestamp error = %v, want stale revision",
			err,
		)
	}
	if _, err := repositories.TransitionProviderRequestAttempt(
		ctx,
		TransitionProviderRequestAttempt{
			ID: first.ID, ExpectedRevision: 0,
			From: ProviderRequestAttemptStarted, To: ProviderRequestAttemptFailed,
			EffectStatus: ProviderRequestEffectPossible, ObservedAt: now,
		},
	); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("stale physical transition error = %v, want stale revision", err)
	}
	metadata := `{"request_id":"redacted-request-1"}`
	firstResponseAt := now.Add(750 * time.Millisecond)
	if _, err := repositories.TransitionProviderRequestAttempt(
		ctx,
		TransitionProviderRequestAttempt{
			ID: first.ID, ExpectedRevision: first.Revision,
			From: ProviderRequestAttemptStarted, To: ProviderRequestAttemptStreaming,
			EffectStatus:    ProviderRequestEffectPossible,
			FirstResponseAt: now.Add(-time.Microsecond),
			ObservedAt:      now.Add(time.Second),
		},
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("first response before start error = %v, want conflict", err)
	}
	streamingTransition := TransitionProviderRequestAttempt{
		ID: first.ID, ExpectedRevision: first.Revision,
		From: ProviderRequestAttemptStarted, To: ProviderRequestAttemptStreaming,
		ProviderRequestIDRedacted: stringPointer("redacted-request-1"),
		EffectStatus:              ProviderRequestEffectPossible,
		PartialStreamObserved:     true, SafeMetadataJSON: &metadata,
		FirstResponseAt: firstResponseAt, ObservedAt: now.Add(time.Second),
	}
	first, err = repositories.TransitionProviderRequestAttempt(ctx, streamingTransition)
	if err != nil {
		t.Fatal(err)
	}
	retriedStreaming, err := repositories.TransitionProviderRequestAttempt(
		ctx, streamingTransition,
	)
	if err != nil || retriedStreaming.FirstResponseAt == nil ||
		!retriedStreaming.FirstResponseAt.Equal(firstResponseAt) ||
		retriedStreaming.Revision != first.Revision {
		t.Fatalf("idempotent explicit first response = %#v, %v", retriedStreaming, err)
	}
	if _, err := repositories.TransitionProviderRequestAttempt(
		ctx,
		TransitionProviderRequestAttempt{
			ID: first.ID, ExpectedRevision: first.Revision,
			From: ProviderRequestAttemptStreaming, To: ProviderRequestAttemptFailed,
			EffectStatus: ProviderRequestEffectPossible,
			ErrorClass:   stringPointer("unavailable"), Retryable: true,
			ObservedAt: now.Add(500 * time.Millisecond),
		},
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("completion before first response error = %v, want conflict", err)
	}
	partial, err := repositories.AppendProviderAttemptEvidence(
		ctx,
		AppendProviderAttemptEvidence{
			ID: "partial-evidence-1", AttemptID: first.ID, Sequence: 1,
			Kind: "partial-output", Final: false,
			ContentSHA256:   hashFixture("5"),
			SummaryRedacted: "bounded partial text was received before failure",
			ByteCount:       23,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	retriedPartial, err := repositories.AppendProviderAttemptEvidence(
		ctx,
		AppendProviderAttemptEvidence{
			ID: "partial-evidence-1", AttemptID: first.ID, Sequence: 1,
			Kind: "partial-output", Final: false,
			ContentSHA256:   hashFixture("5"),
			SummaryRedacted: "bounded partial text was received before failure",
			ByteCount:       23,
		},
	)
	if err != nil || retriedPartial != partial {
		t.Fatalf("idempotent partial evidence = %#v, %v", retriedPartial, err)
	}
	effectEvidence, err := repositories.AppendProviderAttemptEvidence(
		ctx,
		AppendProviderAttemptEvidence{
			ID: "effect-evidence-1", AttemptID: first.ID, Sequence: 2,
			Kind: "effect-observed", Final: true,
			ContentSHA256:   hashFixture("6"),
			SummaryRedacted: "a provider-side response identity was observed",
			ByteCount:       0,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.database.sql.ExecContext(
		ctx,
		`UPDATE provider_attempt_evidence
		 SET summary_redacted = 'rewritten' WHERE id = ?`,
		effectEvidence.ID,
	); !errors.Is(classify("rewrite provider evidence", err), ErrConstraint) {
		t.Fatalf("provider evidence immutability error = %v, want constraint", err)
	}
	errorClass := "unavailable"
	first, err = repositories.TransitionProviderRequestAttempt(
		ctx,
		TransitionProviderRequestAttempt{
			ID: first.ID, ExpectedRevision: first.Revision,
			From: ProviderRequestAttemptStreaming, To: ProviderRequestAttemptFailed,
			EffectStatus: ProviderRequestEffectPossible, ErrorClass: &errorClass,
			Retryable: true, ObservedAt: now.Add(2 * time.Second),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.TransitionProviderRequestAttempt(
		ctx,
		TransitionProviderRequestAttempt{
			ID: first.ID, ExpectedRevision: first.Revision - 1,
			From:         ProviderRequestAttemptStreaming,
			To:           ProviderRequestAttemptFailed,
			EffectStatus: ProviderRequestEffectPossible,
			ErrorClass:   &errorClass, Retryable: true,
			ObservedAt: now.Add(3 * time.Second),
		},
	); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf(
			"changed idempotent completion timestamp error = %v, want stale revision",
			err,
		)
	}
	if first.SafeMetadataJSON == nil || *first.SafeMetadataJSON != metadata ||
		!first.PartialStreamObserved || first.FirstResponseAt == nil ||
		!first.FirstResponseAt.Equal(firstResponseAt) {
		t.Fatalf("attempt lost safe partial metadata: %#v", first)
	}
	if _, err := repositories.database.sql.ExecContext(
		ctx,
		`UPDATE provider_request_attempts
		 SET attempt_number = 9 WHERE id = ?`,
		first.ID,
	); !errors.Is(classify("rewrite provider attempt", err), ErrConstraint) {
		t.Fatalf("physical attempt immutability error = %v, want constraint", err)
	}
	if _, err := repositories.database.sql.ExecContext(
		ctx,
		`UPDATE provider_request_attempts
		 SET state = 'started' WHERE id = ?`,
		first.ID,
	); !errors.Is(classify("rewrite terminal provider attempt", err), ErrConstraint) {
		t.Fatalf("terminal attempt immutability error = %v, want constraint", err)
	}
}

func TestProviderAttemptFirstResponseTimestampValidation(t *testing.T) {
	observedAt := time.Date(2026, 7, 30, 12, 0, 1, 0, time.UTC)
	base := TransitionProviderRequestAttempt{
		ID: "first-response-validation", From: ProviderRequestAttemptStarted,
		To: ProviderRequestAttemptSucceeded, EffectStatus: ProviderRequestEffectNone,
		ObservedAt: observedAt,
	}
	nonUTC := base
	nonUTC.FirstResponseAt = observedAt.In(time.FixedZone("offset", 60*60))
	if err := validateProviderRequestAttemptTransition(nonUTC); err == nil {
		t.Fatal("non-UTC first-response timestamp was accepted")
	}
	afterObservation := base
	afterObservation.FirstResponseAt = observedAt.Add(time.Microsecond)
	if err := validateProviderRequestAttemptTransition(afterObservation); err == nil {
		t.Fatal("first-response timestamp after observation was accepted")
	}
	base.FirstResponseAt = observedAt.Add(-time.Millisecond)
	if err := validateProviderRequestAttemptTransition(base); err != nil {
		t.Fatalf("valid first-response timestamp error = %v", err)
	}
}

func TestProviderRetryAccountingAggregatesExactCostWithoutFloatDrift(t *testing.T) {
	ctx := context.Background()
	repositories, task := createTaskFixture(t, 2000)
	providerID, configuration, pricing := seedProviderRequestDependencies(
		t, repositories, 2001,
	)
	request := planAndStartProviderRequest(
		t, repositories, task.ID, providerID, configuration, pricing, 6,
	)
	usd := mustCurrencyCode(t, "USD")
	costs := []ExactMinorCost{
		{Numerator: 1, Denominator: 3, Currency: usd},
		{Numerator: 2, Denominator: 3, Currency: usd},
	}
	for index := range 2 {
		attempt, err := repositories.CreateProviderRequestAttempt(
			ctx,
			CreateProviderRequestAttempt{
				ID:               "accounted-attempt-" + string(rune('1'+index)),
				LogicalRequestID: request.ID,
				AttemptNumber:    uint64(index + 1),
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		now := time.Date(2026, 7, 30, 13, index, 0, 0, time.UTC)
		attempt = transitionProviderAttemptForTest(
			t, repositories, attempt, ProviderRequestAttemptStarted,
			ProviderRequestEffectNone, false, now,
		)
		finalState := ProviderRequestAttemptFailed
		errorClass := stringPointer("timeout")
		retryable := true
		if index == 1 {
			finalState = ProviderRequestAttemptSucceeded
			errorClass = nil
			retryable = false
		}
		_, err = repositories.TransitionProviderRequestAttempt(
			ctx,
			TransitionProviderRequestAttempt{
				ID: attempt.ID, ExpectedRevision: attempt.Revision,
				From: ProviderRequestAttemptStarted, To: finalState,
				EffectStatus: ProviderRequestEffectNone, ErrorClass: errorClass,
				Retryable: retryable, ObservedAt: now.Add(time.Second),
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		discrepancy := (*string)(nil)
		if index == 1 {
			discrepancy = stringPointer("provider total differed from local estimate")
		}
		accountingInput := AppendProviderAttemptAccounting{
			ID:        "attempt-accounting-" + string(rune('1'+index)),
			AttemptID: attempt.ID, Sequence: 1, Source: "provider-final",
			Usage: domain.TokenUsage{
				Known: true, Input: domain.TokenCount(index + 1),
				Output: 1,
			},
			PricingRevisionID: &pricing.ID, Cost: &costs[index],
			DiscrepancyRedacted: discrepancy,
			ProvenanceJSON:      `{"source":"provider-final","safe":true}`,
		}
		record, err := repositories.AppendProviderAttemptAccounting(ctx, accountingInput)
		if err != nil {
			t.Fatal(err)
		}
		retried, err := repositories.AppendProviderAttemptAccounting(ctx, accountingInput)
		if err != nil || !providerAttemptAccountingMatches(retried, accountingInput) ||
			record.ID != retried.ID {
			t.Fatalf("idempotent accounting = %#v, %v", retried, err)
		}
	}
	wrongCost := ExactMinorCost{Numerator: 1, Denominator: 3, Currency: usd}
	if _, err := repositories.AppendProviderAttemptAccounting(
		ctx,
		AppendProviderAttemptAccounting{
			ID: "mismatched-attempt-cost", AttemptID: "accounted-attempt-2",
			Sequence: 2, Source: "reconciled",
			Usage:             domain.TokenUsage{Known: true, Input: 2, Output: 1},
			PricingRevisionID: &pricing.ID, Cost: &wrongCost,
			ProvenanceJSON: `{"source":"mismatched-fixture"}`,
		},
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("mismatched calculated cost error = %v, want conflict", err)
	}
	if _, err := repositories.database.sql.ExecContext(
		ctx,
		`UPDATE provider_attempt_accounting
		 SET cost_minor_numerator = 99 WHERE id = 'attempt-accounting-1'`,
	); !errors.Is(classify("rewrite provider accounting", err), ErrConstraint) {
		t.Fatalf("provider accounting immutability error = %v, want constraint", err)
	}
	summary, err := repositories.SummarizeProviderRequestAccounting(ctx, request.ID)
	if err != nil {
		t.Fatal(err)
	}
	if summary.AttemptCount != 2 || !summary.AccountingComplete ||
		summary.Cost == nil || summary.Cost.Numerator != 1 ||
		summary.Cost.Denominator != 1 || summary.Cost.Currency != usd ||
		summary.Usage.Input != 3 || summary.Usage.Output != 2 ||
		!summary.Discrepancy {
		t.Fatalf("exact retry accounting summary = %#v", summary)
	}
	finished, err := repositories.TransitionProviderLogicalRequest(
		ctx,
		TransitionProviderLogicalRequest{
			ID: request.ID, ExpectedRevision: request.Revision,
			From:             ProviderLogicalRequestInFlight,
			To:               ProviderLogicalRequestSucceeded,
			AccountingStatus: ProviderAccountingDiscrepant,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	retriedFinish, err := repositories.TransitionProviderLogicalRequest(
		ctx,
		TransitionProviderLogicalRequest{
			ID: request.ID, ExpectedRevision: request.Revision,
			From:             ProviderLogicalRequestInFlight,
			To:               ProviderLogicalRequestSucceeded,
			AccountingStatus: ProviderAccountingDiscrepant,
		},
	)
	if err != nil || retriedFinish.Revision != finished.Revision ||
		finished.CompletedAt == nil {
		t.Fatalf("idempotent logical completion = %#v, %v", retriedFinish, err)
	}
	if _, err := repositories.database.sql.ExecContext(
		ctx,
		`UPDATE provider_logical_requests
		 SET accounting_status = 'reconciled' WHERE id = ?`,
		request.ID,
	); !errors.Is(classify("rewrite terminal logical request", err), ErrConstraint) {
		t.Fatalf("terminal logical request immutability error = %v, want constraint", err)
	}
}

func TestProviderUnknownAccountingRemainsUnknownRatherThanZero(t *testing.T) {
	ctx := context.Background()
	repositories, task := createTaskFixture(t, 3000)
	providerID := seedProviderAccountingProvider(t, repositories, 3001)
	configuration := seedProviderConfiguration(t, repositories, providerID, 7)
	unknownPricing, err := repositories.CreateProviderPricingRevision(
		ctx,
		CreateProviderPricingRevision{
			ID: "unknown-request-pricing", ProviderID: providerID,
			ModelIdentifier: "local-model", ModelVersion: "unknown-version",
			PricingKnown: false,
			EffectiveAt:  time.Date(2026, 7, 30, 14, 0, 0, 0, time.UTC),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	request := planAndStartProviderRequest(
		t, repositories, task.ID, providerID, configuration, unknownPricing, 8,
	)
	attempt, err := repositories.CreateProviderRequestAttempt(
		ctx,
		CreateProviderRequestAttempt{
			ID: "unknown-accounting-attempt", LogicalRequestID: request.ID,
			AttemptNumber: 1,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 30, 14, 1, 0, 0, time.UTC)
	attempt = transitionProviderAttemptForTest(
		t, repositories, attempt, ProviderRequestAttemptStarted,
		ProviderRequestEffectNone, false, now,
	)
	if _, err := repositories.TransitionProviderRequestAttempt(
		ctx,
		TransitionProviderRequestAttempt{
			ID: attempt.ID, ExpectedRevision: attempt.Revision,
			From: ProviderRequestAttemptStarted, To: ProviderRequestAttemptSucceeded,
			EffectStatus: ProviderRequestEffectNone, ObservedAt: now.Add(time.Second),
		},
	); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.AppendProviderAttemptAccounting(
		ctx,
		AppendProviderAttemptAccounting{
			ID: "unknown-accounting", AttemptID: attempt.ID, Sequence: 1,
			Source: "provider-final", Usage: domain.TokenUsage{},
			ProvenanceJSON: `{"source":"endpoint-without-usage"}`,
		},
	); err != nil {
		t.Fatal(err)
	}
	summary, err := repositories.SummarizeProviderRequestAccounting(ctx, request.ID)
	if err != nil {
		t.Fatal(err)
	}
	if summary.AccountingComplete || summary.Usage.Known || summary.Cost != nil {
		t.Fatalf("unknown accounting became zero-valued: %#v", summary)
	}
}

func seedProviderAccountingProvider(
	t *testing.T,
	repositories *Repositories,
	number int,
) domain.ProviderID {
	t.Helper()
	providerID, err := domain.NewProviderID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.database.sql.ExecContext(
		context.Background(),
		`INSERT INTO providers (
			id, display_name, provider_type, enabled,
			created_at_unix_micros, updated_at_unix_micros
		) VALUES (?, ?, 'fixture', 1, ?, ?)`,
		providerID, "Provider fixture "+string(rune('A'+number%26)), number, number,
	); err != nil {
		t.Fatal(err)
	}
	return providerID
}

func seedProviderConfiguration(
	t *testing.T,
	repositories *Repositories,
	providerID domain.ProviderID,
	number int,
) ProviderConfigurationRevision {
	t.Helper()
	configuration, err := repositories.CreateProviderConfigurationRevision(
		context.Background(),
		CreateProviderConfigurationRevision{
			ID:         "request-provider-config-" + string(rune('a'+number%26)),
			ProviderID: providerID, ExpectedLatestRevision: 0,
			AdapterName: "fixture-adapter", AdapterVersion: "fixture-v1",
			ProviderVersion:  "fixture-provider-v1",
			EndpointRedacted: "https://provider.example.invalid",
			CapabilitiesJSON: `{"streaming":true}`,
			ContentSHA256:    hashFixture(string(rune('a' + number%6))),
			IdempotencyKey:   "provider-config-key-" + string(rune('a'+number%26)),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return configuration
}

func seedProviderRequestDependencies(
	t *testing.T,
	repositories *Repositories,
	number int,
) (domain.ProviderID, ProviderConfigurationRevision, ProviderPricingRevision) {
	t.Helper()
	providerID := seedProviderAccountingProvider(t, repositories, number)
	configuration := seedProviderConfiguration(t, repositories, providerID, number)
	usd := mustCurrencyCode(t, "USD")
	pricing, err := repositories.CreateProviderPricingRevision(
		context.Background(),
		CreateProviderPricingRevision{
			ID:         "request-pricing-" + string(rune('a'+number%26)),
			ProviderID: providerID, ModelIdentifier: "fixture-model",
			ModelVersion: "fixture-model-v1", PricingKnown: true, Currency: &usd,
			EffectiveAt: time.Date(2026, 7, 30, 0, number%60, 0, 0, time.UTC),
			Components: []ProviderPriceComponent{
				{UsageKind: "input", MinorNumerator: 1, TokenDenominator: 3},
				{UsageKind: "output", MinorNumerator: 0, TokenDenominator: 1},
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return providerID, configuration, pricing
}

func planAndStartProviderRequest(
	t *testing.T,
	repositories *Repositories,
	taskID domain.TaskID,
	providerID domain.ProviderID,
	configuration ProviderConfigurationRevision,
	pricing ProviderPricingRevision,
	number int,
) ProviderLogicalRequest {
	t.Helper()
	requestID, err := domain.NewModelRequestID()
	if err != nil {
		t.Fatal(err)
	}
	request, err := repositories.PlanProviderLogicalRequest(
		context.Background(),
		PlanProviderLogicalRequest{
			ID: requestID, TaskID: taskID, ProviderID: providerID,
			ProviderConfigurationRevisionID: configuration.ID,
			AdapterName:                     configuration.AdapterName,
			AdapterVersion:                  configuration.AdapterVersion,
			ProviderVersion:                 configuration.ProviderVersion,
			ModelIdentifier:                 pricing.ModelIdentifier, ModelVersion: pricing.ModelVersion,
			PricingRevisionID: &pricing.ID, RequestSHA256: hashFixture(string(rune('a' + number%6))),
			IdempotencyKey: "logical-request-" + string(rune('a'+number%26)),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	request, err = repositories.TransitionProviderLogicalRequest(
		context.Background(),
		TransitionProviderLogicalRequest{
			ID: request.ID, ExpectedRevision: request.Revision,
			From: ProviderLogicalRequestPlanned, To: ProviderLogicalRequestInFlight,
			AccountingStatus: ProviderAccountingUnknown,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func transitionProviderAttemptForTest(
	t *testing.T,
	repositories *Repositories,
	attempt ProviderRequestAttempt,
	to ProviderRequestAttemptState,
	effect ProviderRequestEffectStatus,
	partial bool,
	observedAt time.Time,
) ProviderRequestAttempt {
	t.Helper()
	updated, err := repositories.TransitionProviderRequestAttempt(
		context.Background(),
		TransitionProviderRequestAttempt{
			ID: attempt.ID, ExpectedRevision: attempt.Revision,
			From: attempt.State, To: to, EffectStatus: effect,
			PartialStreamObserved: partial, ObservedAt: observedAt,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return updated
}

func mustCurrencyCode(t *testing.T, value string) domain.CurrencyCode {
	t.Helper()
	currency, err := domain.ParseCurrencyCode(value)
	if err != nil {
		t.Fatal(err)
	}
	return currency
}

func hashFixture(character string) string {
	return character + "000000000000000000000000000000000000000000000000000000000000000"
}

func stringPointer(value string) *string {
	return &value
}
