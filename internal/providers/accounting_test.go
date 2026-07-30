package providers

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

func TestReconcileUsagePrefersReportedAndSurfacesDiscrepancies(t *testing.T) {
	estimated := Usage{
		Known: true, Source: UsageSourceEstimated,
		InputTokens: 100, OutputTokens: 20,
	}
	reported := Usage{
		Known: true, Source: UsageSourceProvider,
		InputTokens: 110, OutputTokens: 18,
	}
	result, err := ReconcileUsage(estimated, reported)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.Effective, reported) {
		t.Fatalf("effective usage = %#v", result.Effective)
	}
	if len(result.Discrepancies) != 2 ||
		result.Discrepancies[0].Category != "input-tokens" ||
		result.Discrepancies[1].Category != "output-tokens" {
		t.Fatalf("discrepancies = %#v", result.Discrepancies)
	}
}

func TestReconcileUsageRetainsEstimateWhenProviderUsageUnknown(t *testing.T) {
	estimated := Usage{
		Known: true, Source: UsageSourceEstimated, InputTokens: 25,
	}
	result, err := ReconcileUsage(
		estimated,
		Usage{Source: UsageSourceUnknown},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.Effective, estimated) ||
		len(result.Discrepancies) != 1 ||
		result.Discrepancies[0].Category != "provider-usage" {
		t.Fatalf("reconciliation = %#v", result)
	}
}

func TestReconcileUsageRejectsUnknownUsageWithNumericValues(t *testing.T) {
	_, err := ReconcileUsage(
		Usage{Source: UsageSourceUnknown, InputTokens: 1},
		Usage{Source: UsageSourceUnknown},
	)
	if !errors.Is(err, ErrInvalidProviderUsage) {
		t.Fatalf("usage validation error = %v", err)
	}
}

func TestReconcileUsageNormalizesProviderSpecificCategories(t *testing.T) {
	estimated := Usage{
		Known: true, Source: UsageSourceEstimated,
		ProviderSpecific: []byte(`{"audio":5,"search":1}`),
	}
	reported := Usage{
		Known: true, Source: UsageSourceProvider,
		ProviderSpecific: []byte(`{"search":1,"audio":7}`),
	}
	result, err := ReconcileUsage(estimated, reported)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Discrepancies) != 1 ||
		result.Discrepancies[0].Category != "provider-specific:audio" ||
		result.Discrepancies[0].Estimated != 5 ||
		result.Discrepancies[0].Reported != 7 {
		t.Fatalf("provider-specific discrepancies = %#v", result.Discrepancies)
	}
	if _, err := ReconcileUsage(
		Usage{
			Known: true, Source: UsageSourceEstimated,
			ProviderSpecific: []byte(`["not","an","object"]`),
		},
		reported,
	); !errors.Is(err, ErrInvalidProviderUsage) {
		t.Fatalf("provider-specific validation error = %v", err)
	}
}

func TestSummarizePhysicalAttemptAccountingIncludesRetryUsageAndExactCost(
	t *testing.T,
) {
	identity := providerAccountingIdentity(t)
	snapshot := providerAccountingPriceSnapshot(t, identity.Model)
	summary, err := SummarizePhysicalAttemptAccounting(
		[]PhysicalAttemptAccounting{
			{
				Number: 1, Identity: identity, Partial: true,
				Usage: Usage{
					Known: true, Source: UsageSourceProvider,
					InputTokens: 10, OutputTokens: 2,
					ProviderSpecific: []byte(`{"audio":2}`),
				},
				PriceSnapshot: snapshot,
			},
			{
				Number: 2, Identity: identity,
				Usage: Usage{
					Known: true, Source: UsageSourceProvider,
					InputTokens: 100, OutputTokens: 20,
					ProviderSpecific: []byte(`{"audio":3}`),
				},
				PriceSnapshot: snapshot,
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if summary.AttemptCount != 2 ||
		!summary.TotalUsage.Known ||
		summary.TotalUsage.InputTokens != 110 ||
		summary.TotalUsage.OutputTokens != 22 {
		t.Fatalf("usage summary = %#v", summary)
	}
	specific, err := NormalizeProviderSpecificUsage(
		summary.TotalUsage.ProviderSpecific,
	)
	if err != nil || specific["audio"] != 5 {
		t.Fatalf("provider-specific usage summary = %#v, %v", specific, err)
	}
	if !summary.TotalCost.Known ||
		summary.TotalCost.Currency != "USD" ||
		summary.TotalCost.Numerator != 77 ||
		summary.TotalCost.Denominator != 250_000 {
		t.Fatalf("exact total cost = %#v", summary.TotalCost)
	}
	if !reflect.DeepEqual(summary.TotalCost, summary.KnownCostSubtotal) ||
		len(summary.Discrepancies) != 0 {
		t.Fatalf("accounting summary = %#v", summary)
	}
}

func TestSummarizePhysicalAttemptAccountingKeepsUnknownDistinctFromZero(
	t *testing.T,
) {
	identity := providerAccountingIdentity(t)
	snapshot := providerAccountingPriceSnapshot(t, identity.Model)
	snapshot.Price.Output = UnknownAmount("USD")
	summary, err := SummarizePhysicalAttemptAccounting(
		[]PhysicalAttemptAccounting{
			{
				Number: 1, Identity: identity,
				Usage: Usage{
					Known: true, Source: UsageSourceProvider,
					OutputTokens: 10,
				},
				PriceSnapshot: snapshot,
			},
			{
				Number: 2, Identity: identity,
				Usage:         Usage{Source: UsageSourceUnknown},
				PriceSnapshot: snapshot,
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if summary.TotalUsage.Known ||
		summary.TotalCost.Known ||
		len(summary.Discrepancies) != 3 {
		t.Fatalf("unknown accounting summary = %#v", summary)
	}
	if !summary.KnownUsageSubtotal.Known ||
		summary.KnownUsageSubtotal.OutputTokens != 10 {
		t.Fatalf("known usage subtotal = %#v", summary.KnownUsageSubtotal)
	}
}

func TestSummarizePhysicalAttemptAccountingRejectsSwitchingAndGaps(
	t *testing.T,
) {
	identity := providerAccountingIdentity(t)
	snapshot := providerAccountingPriceSnapshot(t, identity.Model)
	usage := Usage{Known: true, Source: UsageSourceProvider}
	switched := identity
	switched.Model.Model = "different-model"
	_, err := SummarizePhysicalAttemptAccounting(
		[]PhysicalAttemptAccounting{
			{
				Number: 1, Identity: identity, Usage: usage,
				PriceSnapshot: snapshot,
			},
			{
				Number: 2, Identity: switched, Usage: usage,
				PriceSnapshot: snapshot,
			},
		},
	)
	if !errors.Is(err, ErrProviderIdentityChanged) {
		t.Fatalf("switch error = %v", err)
	}
	_, err = SummarizePhysicalAttemptAccounting(
		[]PhysicalAttemptAccounting{
			{
				Number: 2, Identity: identity, Usage: usage,
				PriceSnapshot: snapshot,
			},
		},
	)
	if !errors.Is(err, ErrProviderAttemptOrder) {
		t.Fatalf("attempt gap error = %v", err)
	}
}

func providerAccountingIdentity(t *testing.T) RequestIdentity {
	t.Helper()
	requestID, err := domain.NewModelRequestID()
	if err != nil {
		t.Fatal(err)
	}
	provider := ProviderIdentity{
		Adapter: "scripted", AdapterVersion: "1",
		Provider: "fixture", ProviderVersion: "2026-07-30",
	}
	model := ModelIdentity{
		Provider: provider, Model: "fixture-model", Revision: "revision-1",
	}
	return RequestIdentity{
		ModelRequestID: requestID,
		Provider:       provider,
		Model:          model,
		RequestHash:    strings.Repeat("b", 64),
		Idempotency: RequestIdempotency{
			ProviderSupported: true,
			Key:               "synthetic-idempotency-key",
			ProviderScope:     "fixture",
		},
		IdempotencyKey: "synthetic-idempotency-key",
	}
}

func providerAccountingPriceSnapshot(
	t *testing.T,
	model ModelIdentity,
) PriceSnapshot {
	t.Helper()
	input, err := NewExactAmount("USD", 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	output, err := NewExactAmount("USD", 4, 1)
	if err != nil {
		t.Fatal(err)
	}
	zero, err := NewExactAmount("USD", 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	return PriceSnapshot{
		ID: "price-fixture", Model: model,
		Price: TokenPrice{
			Input: input, CachedInput: zero, CacheWrite: zero,
			Output: output, Reasoning: zero,
			ProviderSpecific: map[string]ExactAmount{"audio": zero},
		},
		EffectiveAt: time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC),
		CapturedAt:  time.Date(2026, time.July, 30, 0, 0, 0, 0, time.UTC),
		Source:      "test fixture",
	}
}
