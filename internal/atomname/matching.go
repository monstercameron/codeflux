package atomname

import (
	"strings"

	"codeflux.dev/codeflux/internal/domain"
)

// NameMatchChannel identifies which exact-identity channel produced one
// NameMatch, for the retrieval gate to record as candidate attribution
// (M21-168) and to search before falling back to vector similarity
// (M21-167).
type NameMatchChannel string

const (
	NameMatchChannelCanonicalName    NameMatchChannel = "canonical-name"
	NameMatchChannelNormalizedPhrase NameMatchChannel = "normalized-phrase"
	NameMatchChannelActiveAlias      NameMatchChannel = "active-alias"
)

// NameMatch is a pure discovery result: it names which atom a query text
// exactly matched and through which channel. It deliberately carries no
// applicability, compatibility, evidence, or assurance field, because
// AGENTS.md is explicit that "Aliases assist discovery but never bypass
// exact identity, compatibility, applicability, evidence, or assurance
// gates" — this type structurally cannot represent "approved for use," only
// "found." The retrieval gate that owns those gates (M21-167, M21-168) must
// run its own checks on AtomID before a NameMatch influences anything
// (M21-174).
type NameMatch struct {
	AtomID      domain.AtomID
	Channel     NameMatchChannel
	MatchedText string
}

// MatchAtomNamesAndAliases finds every currently valid canonical name,
// normalized phrase, and active alias that exactly matches query once both
// sides are normalized (case-folded, whitespace-collapsed) (M21-167). It
// performs no vector similarity and no map-iteration-order-dependent work:
// results are emitted in the order names, then aliases, appear in the input
// slices, so the same inputs always produce the same output order.
//
// The canonical-name channel deliberately compares query against the raw
// CanonicalName case-insensitively (strings.EqualFold) rather than through
// normalizeAliasText: since normalizeAliasText now applies the same
// word-split tokenization as DeriveNormalizedPhrase (so a rename-created
// alias can be recognized as duplicating the canonical phrase, see
// alias.go), normalizeAliasText(record.CanonicalName.String()) is always
// textually identical to record.NormalizedPhrase.String(). Routing the
// canonical-name channel through normalizeAliasText would make it always
// match whenever the normalized-phrase channel would, permanently shadowing
// the phrase channel and collapsing two distinct, meaningful attribution
// signals (an exact identifier match vs. a phrase match) into one.
func MatchAtomNamesAndAliases(query string, names []AtomNameRecord, aliases []AtomNameAliasRecord) []NameMatch {
	trimmedQuery := strings.TrimSpace(query)
	normalizedQuery := normalizeAliasText(query)
	if normalizedQuery == "" {
		return nil
	}

	var matches []NameMatch
	for _, record := range names {
		if !record.Valid {
			continue
		}
		switch {
		case strings.EqualFold(record.CanonicalName.String(), trimmedQuery):
			matches = append(matches, NameMatch{
				AtomID:      record.AtomID,
				Channel:     NameMatchChannelCanonicalName,
				MatchedText: record.CanonicalName.String(),
			})
		case record.NormalizedPhrase.String() == normalizedQuery:
			matches = append(matches, NameMatch{
				AtomID:      record.AtomID,
				Channel:     NameMatchChannelNormalizedPhrase,
				MatchedText: record.NormalizedPhrase.String(),
			})
		}
	}
	for _, alias := range aliases {
		if !alias.Active() {
			continue
		}
		if alias.NormalizedAliasText == normalizedQuery {
			matches = append(matches, NameMatch{
				AtomID:      alias.AtomID,
				Channel:     NameMatchChannelActiveAlias,
				MatchedText: alias.AliasText,
			})
		}
	}
	return matches
}
