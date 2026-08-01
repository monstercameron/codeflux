package atomdoc

import "fmt"

// AtomTier is the plan.md §7 "Semantic Atom Categories" implementation tier.
type AtomTier string

const (
	// AtomTierKernel is Tier 0: semantics defined directly by the language
	// implementation.
	AtomTierKernel AtomTier = "kernel"
	// AtomTierGraphNative is Tier 1: implemented entirely from graph nodes,
	// kernel atoms, and other graph-native atoms, with no Go implementation
	// of its own.
	AtomTierGraphNative AtomTier = "graph-native"
	// AtomTierModeledGo is Tier 2: a native Go implementation plus an
	// independently authored graph reference model.
	AtomTierModeledGo AtomTier = "modeled-go"
	// AtomTierExternal is Tier 3: a typed external-system adapter with a
	// contract, capability requirements, and runtime implementation.
	AtomTierExternal AtomTier = "external"
)

// IsValid reports whether tier is one of the four declared plan.md §7 tiers.
func (tier AtomTier) IsValid() bool {
	switch tier {
	case AtomTierKernel, AtomTierGraphNative, AtomTierModeledGo, AtomTierExternal:
		return true
	default:
		return false
	}
}

// AuthoringMode records which side owns authoritative atom-documentation
// content for one atom (M21-101; AGENTS.md "Atom Documentation Style").
type AuthoringMode string

const (
	// AuthoringSourceAuthored means the Go doc comment attached to the
	// atom's declaration is authoritative. Codeflux parses it with the Go
	// syntax tree and admits an immutable documentation revision before it
	// can influence retrieval.
	AuthoringSourceAuthored AuthoringMode = "source-authored"
	// AuthoringSQLiteAuthored means the structured SQLite record is
	// authoritative. Any generated Go comment is a non-authoritative
	// projection of that record.
	AuthoringSQLiteAuthored AuthoringMode = "sqlite-authored"
	// AuthoringGeneratedProjection marks Go comment text rendered from a
	// SQLite-authored record by GenerateAtomDocumentationComment. Generated
	// text never becomes authoritative on its own and must never be
	// re-admitted as though it were source-authored content; doing so would
	// let a projection silently promote itself into authority, which
	// AGENTS.md forbids.
	AuthoringGeneratedProjection AuthoringMode = "generated-projection"
)

// IsValid reports whether mode is one of the three declared authoring modes.
func (mode AuthoringMode) IsValid() bool {
	switch mode {
	case AuthoringSourceAuthored, AuthoringSQLiteAuthored, AuthoringGeneratedProjection:
		return true
	default:
		return false
	}
}

// ClassifyAtomDocumentationAuthoring maps a plan.md §7 atom tier to which
// side owns authoritative documentation for that tier (M21-101).
//
// Tier 0 kernel atoms, Tier 2 modeled Go atoms, and Tier 3 external atoms
// each have a concrete Go implementation carrying the atom's declaration, so
// their documentation is source-authored at that declaration and admitted
// through this package's extraction pipeline. Tier 1 graph-native atoms have
// no Go implementation of their own — they are composed entirely from graph
// nodes and other atoms — so their documentation is authored as a structured
// SQLite record, and any Go binding comment that names them is a generated,
// non-authoritative projection (AuthoringGeneratedProjection) rendered by
// GenerateAtomDocumentationComment.
func ClassifyAtomDocumentationAuthoring(tier AtomTier) (AuthoringMode, error) {
	switch tier {
	case AtomTierKernel, AtomTierModeledGo, AtomTierExternal:
		return AuthoringSourceAuthored, nil
	case AtomTierGraphNative:
		return AuthoringSQLiteAuthored, nil
	default:
		return "", fmt.Errorf("atomdoc: unknown atom tier %q", tier)
	}
}
