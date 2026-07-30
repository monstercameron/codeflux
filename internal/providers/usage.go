package providers

import (
	"encoding/json"
	"fmt"
)

// NewProviderUsage distinguishes an omitted usage object from a present
// provider-reported object whose exact token counts are all zero.
func NewProviderUsage(
	present bool,
	input int64,
	cachedInput int64,
	cacheWrite int64,
	output int64,
	reasoning int64,
	providerSpecific json.RawMessage,
) (Usage, error) {
	if !present {
		usage := Usage{Source: UsageSourceUnknown}
		if input != 0 || cachedInput != 0 || cacheWrite != 0 ||
			output != 0 || reasoning != 0 || len(providerSpecific) != 0 {
			return Usage{}, fmt.Errorf(
				"%w: omitted provider usage carries values",
				ErrInvalidProviderUsage,
			)
		}
		return usage, nil
	}
	usage := Usage{
		Known: true, Source: UsageSourceProvider,
		InputTokens: input, CachedInputTokens: cachedInput,
		CacheWriteTokens: cacheWrite, OutputTokens: output,
		ReasoningTokens:  reasoning,
		ProviderSpecific: append(json.RawMessage(nil), providerSpecific...),
	}
	if err := ValidateUsage(usage); err != nil {
		return Usage{}, err
	}
	return usage, nil
}
