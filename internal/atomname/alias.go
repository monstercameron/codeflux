package atomname

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

// ErrInvalidAtomNameAlias classifies a rejected atom-name alias.
var ErrInvalidAtomNameAlias = errors.New("invalid atom name alias")

// AtomNameAliasSource records why one alias exists.
type AtomNameAliasSource string

const (
	// AtomNameAliasSourceRename marks an alias created automatically from a
	// prior canonical name by an accepted rename (M21-156).
	AtomNameAliasSourceRename AtomNameAliasSource = "rename"
	// AtomNameAliasSourceReviewedManualEntry marks an alias a human reviewer
	// added directly as search-discovery text, distinct from any prior
	// canonical name.
	AtomNameAliasSourceReviewedManualEntry AtomNameAliasSource = "reviewed-manual-entry"
)

// IsValid reports whether source is one of the two declared alias sources.
func (source AtomNameAliasSource) IsValid() bool {
	return source == AtomNameAliasSourceRename || source == AtomNameAliasSourceReviewedManualEntry
}

// AtomNameAliasRecord is the typed, storage-lane-persisted record of one
// searchable prior or reviewed alias name (M21-154: alias text, normalized
// form, source, active interval, and target atom identity). An alias
// created from a rename (AtomNameAliasSourceRename) is immutable lineage
// per M21-156: this package never mutates AliasText or ActiveFrom in
// place; Invalidate returns a new value carrying the closed interval.
type AtomNameAliasRecord struct {
	AtomID              domain.AtomID
	AliasText           string
	NormalizedAliasText string
	Source              AtomNameAliasSource
	ActiveFrom          time.Time
	ActiveUntil         *time.Time
	InvalidatedReason   string
}

// NewAliasFromPriorCanonicalName constructs the alias record for a prior
// canonical name preserved by an accepted rename (M21-156).
func NewAliasFromPriorCanonicalName(atomID domain.AtomID, priorName CanonicalName, occurredAt time.Time) (AtomNameAliasRecord, error) {
	if priorName.IsZero() {
		return AtomNameAliasRecord{}, fmt.Errorf("%w: prior canonical name is required", ErrInvalidAtomNameAlias)
	}
	return newAliasRecord(atomID, priorName.String(), AtomNameAliasSourceRename, occurredAt)
}

// NewReviewedManualAlias constructs a reviewed, manually entered discovery
// alias that is not itself a prior canonical name.
func NewReviewedManualAlias(atomID domain.AtomID, aliasText string, occurredAt time.Time) (AtomNameAliasRecord, error) {
	return newAliasRecord(atomID, aliasText, AtomNameAliasSourceReviewedManualEntry, occurredAt)
}

func newAliasRecord(atomID domain.AtomID, aliasText string, source AtomNameAliasSource, occurredAt time.Time) (AtomNameAliasRecord, error) {
	if atomID.IsZero() {
		return AtomNameAliasRecord{}, fmt.Errorf("%w: atom identity is required", ErrInvalidAtomNameAlias)
	}
	trimmed := strings.TrimSpace(aliasText)
	if trimmed == "" {
		return AtomNameAliasRecord{}, fmt.Errorf("%w: alias text must not be empty", ErrInvalidAtomNameAlias)
	}
	if len(trimmed) > MaximumAliasTextBytes {
		return AtomNameAliasRecord{}, fmt.Errorf("%w: alias text exceeds the maximum bound of %d bytes", ErrInvalidAtomNameAlias, MaximumAliasTextBytes)
	}
	if occurredAt.IsZero() {
		return AtomNameAliasRecord{}, fmt.Errorf("%w: created-at timestamp is required", ErrInvalidAtomNameAlias)
	}
	return AtomNameAliasRecord{
		AtomID:              atomID,
		AliasText:           trimmed,
		NormalizedAliasText: normalizeAliasText(trimmed),
		Source:              source,
		ActiveFrom:          occurredAt,
	}, nil
}

// Active reports whether the alias is still eligible for active candidate
// generation (M21-163).
func (alias AtomNameAliasRecord) Active() bool {
	return alias.ActiveUntil == nil
}

// Invalidate returns a new AtomNameAliasRecord value with the active
// interval closed at invalidatedAt and reason recorded, without mutating
// alias. It never removes the alias's history: the storage lane retains the
// original row as immutable lineage and records this closed interval
// alongside it (M21-163: "Exclude obsolete or invalidated aliases from
// active candidate generation while retaining lineage.").
func (alias AtomNameAliasRecord) Invalidate(reason string, invalidatedAt time.Time) (AtomNameAliasRecord, error) {
	trimmed := strings.TrimSpace(reason)
	if trimmed == "" {
		return AtomNameAliasRecord{}, fmt.Errorf("%w: invalidation reason must not be empty", ErrInvalidAtomNameAlias)
	}
	if !alias.Active() {
		return AtomNameAliasRecord{}, fmt.Errorf("%w: alias is already invalidated", ErrInvalidAtomNameAlias)
	}
	if invalidatedAt.Before(alias.ActiveFrom) {
		return AtomNameAliasRecord{}, fmt.Errorf("%w: invalidation time must not precede the alias's active-from time", ErrInvalidAtomNameAlias)
	}
	updated := alias
	until := invalidatedAt
	updated.ActiveUntil = &until
	updated.InvalidatedReason = trimmed
	return updated, nil
}

// normalizeAliasText normalizes raw through the same word-split
// tokenization DeriveNormalizedPhrase applies to a canonical Go identifier
// (splitIdentifierWordsRaw), lowercasing and space-joining the result. This
// is required, not cosmetic: NewAliasFromPriorCanonicalName always feeds a
// spaceless Go identifier (a prior CanonicalName) through this function, and
// the ComposeNameEmbeddingInput alias-deduplication logic compares
// NormalizedAliasText against NormalizedPhrase (itself derived by
// DeriveNormalizedPhrase) to decide whether an alias merely restates the
// canonical phrase. A scheme that only lowercased and collapsed
// already-present whitespace (the prior implementation) left a
// space-joined phrase and its equivalent spaceless identifier permanently
// unequal — "reserve account funds" never equaled "reserveaccountfunds" —
// so a rename-created alias could never be recognized as a duplicate of the
// atom's own canonical phrase. Splitting every whitespace-delimited field
// individually keeps ordinary, already-spaced human-authored alias text
// (for example a NewReviewedManualAlias phrase with no internal case
// transitions) normalizing exactly as before.
func normalizeAliasText(raw string) string {
	var words []string
	for _, field := range strings.Fields(raw) {
		for _, word := range splitIdentifierWordsRaw(field) {
			words = append(words, strings.ToLower(word))
		}
	}
	return strings.Join(words, " ")
}
