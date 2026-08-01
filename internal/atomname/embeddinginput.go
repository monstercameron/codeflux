package atomname

import (
	"errors"
	"fmt"
)

// ErrCannotComposeEmbeddingInput classifies an atom-name record that cannot
// contribute embedding input.
var ErrCannotComposeEmbeddingInput = errors.New("cannot compose atom-name embedding input")

// EmbeddingSegmentRole distinguishes the canonical name and normalized
// phrase — included exactly once and at full weight — from low-weight alias
// discovery text (M21-161, M21-162).
type EmbeddingSegmentRole string

const (
	EmbeddingSegmentRoleCanonicalName    EmbeddingSegmentRole = "canonical-name"
	EmbeddingSegmentRoleNormalizedPhrase EmbeddingSegmentRole = "normalized-phrase"
	EmbeddingSegmentRoleAliasDiscovery   EmbeddingSegmentRole = "alias-discovery"
)

// EmbeddingInputSegment is one piece of text this package contributes to an
// atom's embedding input, along with the role that tells the (out-of-scope,
// vector-runtime-gated) embedding assembler how to weight it.
type EmbeddingInputSegment struct {
	Role      EmbeddingSegmentRole
	Text      string
	LowWeight bool
}

// NameEmbeddingInput is the naming lane's contribution to one atom's
// embedding input. It is not the complete embedding-input schema (M21-127,
// out of scope, owned by a separate lane that also composes documentation
// fields); it is only the canonical-name/normalized-phrase/alias fragment
// AGENTS.md requires that lane to include.
type NameEmbeddingInput struct {
	Segments []EmbeddingInputSegment
	// ExcludedAliasCount counts aliases left out of Segments because they
	// were invalidated/obsolete or because their normalized text duplicated
	// content already included (M21-163). The excluded aliases are not
	// deleted or otherwise altered by this exclusion: their lineage remains
	// exactly as stored by the caller's full alias slice.
	ExcludedAliasCount int
}

// ComposeNameEmbeddingInput composes the naming-derived embedding-input
// segments for record: the canonical name and normalized phrase exactly
// once each (M21-161), followed by each currently active, non-duplicate
// alias as low-weight discovery text (M21-162), excluding obsolete or
// invalidated aliases and any alias whose normalized text duplicates
// content already included (M21-163). aliases may contain aliases for other
// atoms; only aliases matching record.AtomID are considered.
func ComposeNameEmbeddingInput(record AtomNameRecord, aliases []AtomNameAliasRecord) (NameEmbeddingInput, error) {
	if !record.Valid {
		return NameEmbeddingInput{}, fmt.Errorf("%w: atom-name record is not the current valid revision", ErrCannotComposeEmbeddingInput)
	}
	if record.CanonicalName.IsZero() || record.NormalizedPhrase.IsZero() {
		return NameEmbeddingInput{}, fmt.Errorf("%w: atom-name record is missing its canonical name or normalized phrase", ErrCannotComposeEmbeddingInput)
	}

	segments := []EmbeddingInputSegment{
		{Role: EmbeddingSegmentRoleCanonicalName, Text: record.CanonicalName.String()},
		{Role: EmbeddingSegmentRoleNormalizedPhrase, Text: record.NormalizedPhrase.String()},
	}

	seen := map[string]struct{}{record.NormalizedPhrase.String(): {}}
	excluded := 0
	for _, alias := range aliases {
		if alias.AtomID != record.AtomID {
			continue
		}
		if !alias.Active() {
			excluded++
			continue
		}
		if _, duplicate := seen[alias.NormalizedAliasText]; duplicate {
			excluded++
			continue
		}
		seen[alias.NormalizedAliasText] = struct{}{}
		segments = append(segments, EmbeddingInputSegment{
			Role:      EmbeddingSegmentRoleAliasDiscovery,
			Text:      alias.AliasText,
			LowWeight: true,
		})
	}

	return NameEmbeddingInput{Segments: segments, ExcludedAliasCount: excluded}, nil
}

// EmbeddingInvalidationTrigger names the reason a naming change requires
// the vector-runtime lane (behind the docs/plan.md §0 branch gate) to
// invalidate or regenerate an atom's derived embedding (M21-164). This
// package never invalidates, regenerates, or otherwise touches a vector
// itself: DetermineEmbeddingInvalidationTriggers only names which triggers
// fired between two NameEmbeddingInput snapshots, for that gated lane to
// act on.
type EmbeddingInvalidationTrigger string

const (
	EmbeddingInvalidationTriggerCanonicalContentChanged EmbeddingInvalidationTrigger = "canonical-name-or-normalized-phrase-changed"
	EmbeddingInvalidationTriggerActiveAliasSetChanged   EmbeddingInvalidationTrigger = "active-alias-set-changed"
)

// DetermineEmbeddingInvalidationTriggers compares two NameEmbeddingInput
// snapshots for the same atom and reports which invalidation triggers fired
// (M21-164). It performs no regeneration or similarity computation.
func DetermineEmbeddingInvalidationTriggers(before, after NameEmbeddingInput) []EmbeddingInvalidationTrigger {
	var triggers []EmbeddingInvalidationTrigger
	beforeCanonical, beforeAliases := splitSegmentsByRole(before)
	afterCanonical, afterAliases := splitSegmentsByRole(after)
	if beforeCanonical != afterCanonical {
		triggers = append(triggers, EmbeddingInvalidationTriggerCanonicalContentChanged)
	}
	if !equalStringSlices(beforeAliases, afterAliases) {
		triggers = append(triggers, EmbeddingInvalidationTriggerActiveAliasSetChanged)
	}
	return triggers
}

func splitSegmentsByRole(input NameEmbeddingInput) (canonical string, aliasTexts []string) {
	for _, segment := range input.Segments {
		switch segment.Role {
		case EmbeddingSegmentRoleCanonicalName, EmbeddingSegmentRoleNormalizedPhrase:
			canonical += segment.Text + "\x1f"
		case EmbeddingSegmentRoleAliasDiscovery:
			aliasTexts = append(aliasTexts, segment.Text)
		}
	}
	return canonical, aliasTexts
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if a[index] != b[index] {
			return false
		}
	}
	return true
}
