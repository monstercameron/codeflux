package providers

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"sort"
	"strings"
)

var (
	ErrInvalidProviderUsage    = errors.New("provider usage is invalid")
	ErrProviderIdentityChanged = errors.New("provider request identity changed across physical attempts")
	ErrProviderAttemptOrder    = errors.New("provider physical attempts are not contiguous")
)

// AccountingDiscrepancy preserves an explicit mismatch or unknown accounting
// input rather than silently selecting a zero value.
type AccountingDiscrepancy struct {
	Attempt   int
	Category  string
	Estimated int64
	Reported  int64
	Reason    string
}

// UsageReconciliation retains estimated and provider-reported values while
// selecting the strongest available effective evidence.
type UsageReconciliation struct {
	Estimated     Usage
	Reported      Usage
	Effective     Usage
	Discrepancies []AccountingDiscrepancy
}

// ReconcileUsage compares an estimate with provider-reported usage. Reported
// usage is authoritative when known; otherwise the estimate remains explicitly
// estimated.
func ReconcileUsage(
	estimated Usage,
	reported Usage,
) (UsageReconciliation, error) {
	result := UsageReconciliation{Estimated: estimated, Reported: reported}
	if err := ValidateUsage(estimated); err != nil {
		return result, fmt.Errorf("estimated usage: %w", err)
	}
	if err := ValidateUsage(reported); err != nil {
		return result, fmt.Errorf("reported usage: %w", err)
	}
	switch {
	case reported.Known:
		result.Effective = cloneUsage(reported)
		if estimated.Known {
			result.Discrepancies = compareUsage(0, estimated, reported)
		}
	case estimated.Known:
		result.Effective = cloneUsage(estimated)
		result.Discrepancies = append(
			result.Discrepancies,
			AccountingDiscrepancy{
				Category: "provider-usage",
				Reason:   "provider usage is unknown; estimate retained",
			},
		)
	default:
		result.Effective = Usage{Source: UsageSourceUnknown}
		result.Discrepancies = append(
			result.Discrepancies,
			AccountingDiscrepancy{
				Category: "usage",
				Reason:   "both provider and estimated usage are unknown",
			},
		)
	}
	return result, nil
}

// PhysicalAttemptAccounting is the attributable usage and immutable price
// snapshot for one physical provider request.
type PhysicalAttemptAccounting struct {
	Number        int
	Identity      RequestIdentity
	Usage         Usage
	PriceSnapshot PriceSnapshot
	Partial       bool
}

// AttemptAccountingSummary keeps known subtotals even when an unknown retry
// attempt prevents an exact total.
type AttemptAccountingSummary struct {
	Identity           RequestIdentity
	AttemptCount       int
	TotalUsage         Usage
	KnownUsageSubtotal Usage
	TotalCost          ExactAmount
	KnownCostSubtotal  ExactAmount
	Discrepancies      []AccountingDiscrepancy
}

// SummarizePhysicalAttemptAccounting attributes all retry usage and cost to one
// immutable logical request.
func SummarizePhysicalAttemptAccounting(
	attempts []PhysicalAttemptAccounting,
) (AttemptAccountingSummary, error) {
	var summary AttemptAccountingSummary
	if len(attempts) == 0 {
		summary.TotalUsage = Usage{Source: UsageSourceUnknown}
		summary.TotalCost = UnknownAmount("")
		summary.KnownCostSubtotal = UnknownAmount("")
		return summary, nil
	}
	summary.Identity = attempts[0].Identity
	if err := ValidateRequestIdentity(summary.Identity); err != nil {
		return summary, err
	}
	allUsageKnown := true
	allCostKnown := true
	usageSource := UsageSourceProvider
	var knownUsage Usage
	knownUsage.Known = true
	knownUsage.Source = UsageSourceProvider
	knownCost := new(big.Rat)
	currency := ""
	for index, attempt := range attempts {
		if attempt.Number != index+1 {
			return summary, fmt.Errorf(
				"%w: got attempt %d at index %d",
				ErrProviderAttemptOrder,
				attempt.Number,
				index,
			)
		}
		if !reflect.DeepEqual(attempt.Identity, summary.Identity) {
			return summary, fmt.Errorf(
				"%w at attempt %d",
				ErrProviderIdentityChanged,
				attempt.Number,
			)
		}
		if !reflect.DeepEqual(attempt.PriceSnapshot.Model, attempt.Identity.Model) {
			return summary, fmt.Errorf(
				"%w: price snapshot model differs at attempt %d",
				ErrProviderIdentityChanged,
				attempt.Number,
			)
		}
		if err := ValidateUsage(attempt.Usage); err != nil {
			return summary, fmt.Errorf(
				"attempt %d usage: %w",
				attempt.Number,
				err,
			)
		}
		if attempt.Usage.Known {
			var err error
			knownUsage, err = addKnownUsage(knownUsage, attempt.Usage)
			if err != nil {
				return summary, fmt.Errorf("attempt %d: %w", attempt.Number, err)
			}
			if attempt.Usage.Source == UsageSourceEstimated {
				usageSource = UsageSourceEstimated
			}
		} else {
			allUsageKnown = false
			summary.Discrepancies = append(
				summary.Discrepancies,
				AccountingDiscrepancy{
					Attempt:  attempt.Number,
					Category: "usage",
					Reason:   "physical attempt usage is unknown",
				},
			)
		}

		cost := attempt.PriceSnapshot.Cost(attempt.Usage)
		if !cost.Known {
			allCostKnown = false
			summary.Discrepancies = append(
				summary.Discrepancies,
				AccountingDiscrepancy{
					Attempt:  attempt.Number,
					Category: "cost",
					Reason:   "physical attempt cost is unknown",
				},
			)
			continue
		}
		if currency == "" {
			currency = cost.Currency
		}
		if cost.Currency != currency {
			allCostKnown = false
			summary.Discrepancies = append(
				summary.Discrepancies,
				AccountingDiscrepancy{
					Attempt:  attempt.Number,
					Category: "currency",
					Reason:   "physical attempt price currency changed",
				},
			)
			continue
		}
		knownCost.Add(
			knownCost,
			new(big.Rat).SetFrac(
				big.NewInt(cost.Numerator),
				big.NewInt(cost.Denominator),
			),
		)
	}
	summary.AttemptCount = len(attempts)
	knownUsage.Source = usageSource
	summary.KnownUsageSubtotal = knownUsage
	if allUsageKnown {
		summary.TotalUsage = knownUsage
	} else {
		summary.TotalUsage = Usage{Source: UsageSourceUnknown}
	}
	summary.KnownCostSubtotal = exactAmountFromRat(currency, knownCost)
	if allCostKnown && summary.KnownCostSubtotal.Known {
		summary.TotalCost = summary.KnownCostSubtotal
	} else {
		summary.TotalCost = UnknownAmount(currency)
	}
	return summary, nil
}

// ValidateUsage keeps unknown usage distinct from a provider-reported zero and
// rejects negative or malformed provider-specific accounting.
func ValidateUsage(usage Usage) error {
	if !usage.Known {
		if usage.Source != "" && usage.Source != UsageSourceUnknown {
			return fmt.Errorf("%w: unknown usage has source %q", ErrInvalidProviderUsage, usage.Source)
		}
		if usage.InputTokens != 0 ||
			usage.CachedInputTokens != 0 ||
			usage.CacheWriteTokens != 0 ||
			usage.OutputTokens != 0 ||
			usage.ReasoningTokens != 0 ||
			len(usage.ProviderSpecific) != 0 {
			return fmt.Errorf("%w: unknown usage carries values", ErrInvalidProviderUsage)
		}
		return nil
	}
	if usage.Source != UsageSourceProvider &&
		usage.Source != UsageSourceEstimated {
		return fmt.Errorf("%w: known usage has source %q", ErrInvalidProviderUsage, usage.Source)
	}
	if usage.InputTokens < 0 ||
		usage.CachedInputTokens < 0 ||
		usage.CacheWriteTokens < 0 ||
		usage.OutputTokens < 0 ||
		usage.ReasoningTokens < 0 {
		return fmt.Errorf("%w: token counts must be non-negative", ErrInvalidProviderUsage)
	}
	if len(usage.ProviderSpecific) > 64<<10 ||
		len(usage.ProviderSpecific) != 0 &&
			!json.Valid(usage.ProviderSpecific) {
		return fmt.Errorf("%w: provider-specific usage must be bounded valid JSON", ErrInvalidProviderUsage)
	}
	if _, err := NormalizeProviderSpecificUsage(
		usage.ProviderSpecific,
	); err != nil {
		return err
	}
	return nil
}

// NormalizeProviderSpecificUsage requires a bounded JSON object of exact,
// non-negative integer token categories.
func NormalizeProviderSpecificUsage(
	raw json.RawMessage,
) (map[string]int64, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var categories map[string]int64
	if err := json.Unmarshal(raw, &categories); err != nil ||
		categories == nil {
		return nil, fmt.Errorf(
			"%w: provider-specific usage must be an integer object",
			ErrInvalidProviderUsage,
		)
	}
	if len(categories) > 128 {
		return nil, fmt.Errorf(
			"%w: provider-specific usage has too many categories",
			ErrInvalidProviderUsage,
		)
	}
	for category, count := range categories {
		if strings.TrimSpace(category) == "" || len(category) > 255 ||
			count < 0 {
			return nil, fmt.Errorf(
				"%w: provider-specific category is invalid",
				ErrInvalidProviderUsage,
			)
		}
	}
	return categories, nil
}

func compareUsage(
	attempt int,
	estimated Usage,
	reported Usage,
) []AccountingDiscrepancy {
	categories := []struct {
		name      string
		estimated int64
		reported  int64
	}{
		{"input-tokens", estimated.InputTokens, reported.InputTokens},
		{"cached-input-tokens", estimated.CachedInputTokens, reported.CachedInputTokens},
		{"cache-write-tokens", estimated.CacheWriteTokens, reported.CacheWriteTokens},
		{"output-tokens", estimated.OutputTokens, reported.OutputTokens},
		{"reasoning-tokens", estimated.ReasoningTokens, reported.ReasoningTokens},
	}
	var discrepancies []AccountingDiscrepancy
	for _, category := range categories {
		if category.estimated == category.reported {
			continue
		}
		discrepancies = append(
			discrepancies,
			AccountingDiscrepancy{
				Attempt:   attempt,
				Category:  category.name,
				Estimated: category.estimated,
				Reported:  category.reported,
				Reason:    "estimated and provider-reported usage differ",
			},
		)
	}
	estimatedSpecific, _ := NormalizeProviderSpecificUsage(
		estimated.ProviderSpecific,
	)
	reportedSpecific, _ := NormalizeProviderSpecificUsage(
		reported.ProviderSpecific,
	)
	specificCategorySet := make(map[string]struct{})
	for category := range estimatedSpecific {
		specificCategorySet[category] = struct{}{}
	}
	for category := range reportedSpecific {
		specificCategorySet[category] = struct{}{}
	}
	specificCategories := make([]string, 0, len(specificCategorySet))
	for category := range specificCategorySet {
		specificCategories = append(specificCategories, category)
	}
	sort.Strings(specificCategories)
	for _, category := range specificCategories {
		if estimatedSpecific[category] == reportedSpecific[category] {
			continue
		}
		discrepancies = append(
			discrepancies,
			AccountingDiscrepancy{
				Attempt:   attempt,
				Category:  "provider-specific:" + category,
				Estimated: estimatedSpecific[category],
				Reported:  reportedSpecific[category],
				Reason:    "estimated and provider-reported usage differ",
			},
		)
	}
	return discrepancies
}

func addKnownUsage(left Usage, right Usage) (Usage, error) {
	values := []struct {
		name  string
		left  *int64
		right int64
	}{
		{"input", &left.InputTokens, right.InputTokens},
		{"cached input", &left.CachedInputTokens, right.CachedInputTokens},
		{"cache write", &left.CacheWriteTokens, right.CacheWriteTokens},
		{"output", &left.OutputTokens, right.OutputTokens},
		{"reasoning", &left.ReasoningTokens, right.ReasoningTokens},
	}
	for _, value := range values {
		if value.right > math.MaxInt64-*value.left {
			return Usage{}, fmt.Errorf("%w: %s token count overflow", ErrInvalidProviderUsage, value.name)
		}
		*value.left += value.right
	}
	leftSpecific, err := NormalizeProviderSpecificUsage(left.ProviderSpecific)
	if err != nil {
		return Usage{}, err
	}
	rightSpecific, err := NormalizeProviderSpecificUsage(right.ProviderSpecific)
	if err != nil {
		return Usage{}, err
	}
	if leftSpecific == nil {
		leftSpecific = make(map[string]int64)
	}
	for category, count := range rightSpecific {
		if count > math.MaxInt64-leftSpecific[category] {
			return Usage{}, fmt.Errorf(
				"%w: provider-specific %s token count overflow",
				ErrInvalidProviderUsage,
				category,
			)
		}
		leftSpecific[category] += count
	}
	if len(leftSpecific) == 0 {
		left.ProviderSpecific = nil
	} else {
		encoded, err := json.Marshal(leftSpecific)
		if err != nil {
			return Usage{}, fmt.Errorf(
				"%w: encode provider-specific usage",
				ErrInvalidProviderUsage,
			)
		}
		left.ProviderSpecific = encoded
	}
	return left, nil
}

func exactAmountFromRat(currency string, value *big.Rat) ExactAmount {
	if currency == "" ||
		!value.Num().IsInt64() ||
		!value.Denom().IsInt64() {
		return UnknownAmount(currency)
	}
	amount, err := NewExactAmount(
		currency,
		value.Num().Int64(),
		value.Denom().Int64(),
	)
	if err != nil {
		return UnknownAmount(currency)
	}
	return amount
}

func cloneUsage(usage Usage) Usage {
	clone := usage
	clone.ProviderSpecific = append([]byte(nil), usage.ProviderSpecific...)
	return clone
}
