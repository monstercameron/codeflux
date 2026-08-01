package atomname

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/atomdoc"
	"codeflux.dev/codeflux/internal/domain"
)

// ErrInvalidRenameProposal classifies a structurally invalid rename
// proposal or an attempted rename/version pairing that violates the
// AGENTS.md "Atom Naming Style" rename rules.
var ErrInvalidRenameProposal = errors.New("invalid atom canonical rename proposal")

// ErrRenameReviewRequired classifies a semantic-preserving rename that was
// authorized without the required explicit review (M21-158).
var ErrRenameReviewRequired = errors.New("semantic-preserving rename requires explicit review")

// ErrRenameRequiresNewIdentity classifies a semantic-breaking rename that
// attempted to keep the exact same atom version and atom identity
// (M21-159).
var ErrRenameRequiresNewIdentity = errors.New("semantic-breaking rename requires a new atom version or atom identity")

// RenameClassification is the outcome of classifying one proposed canonical
// rename (M21-157).
type RenameClassification string

const (
	// RenameClassificationFormattingOnly means the old and new canonical
	// names split into the exact same words (case-insensitively) and the
	// atom's pinned contract is unchanged: only capitalization, delimiter,
	// or equivalent surface formatting differs.
	RenameClassificationFormattingOnly RenameClassification = "formatting-only"
	// RenameClassificationSemanticPreserving means the words differ (a real
	// rewording) but the atom's pinned contract is unchanged and no
	// semantic change was asserted: the same business behavior now has a
	// clearer or more accurate name.
	RenameClassificationSemanticPreserving RenameClassification = "semantic-preserving"
	// RenameClassificationSemanticBreaking means the atom's pinned contract
	// changed, or the proposer explicitly asserted a semantic change: the
	// rename cannot be treated as cosmetic regardless of how similar the
	// words look.
	RenameClassificationSemanticBreaking RenameClassification = "semantic-breaking"
)

// RenameProposal is the pure input to ClassifyRename. OldContractHash and
// NewContractHash pin the atom's contract (docs/plan.md §7 "Pinned Atom
// Contract") before and after the proposed rename; comparing them is the
// mechanical signal this package uses to detect a semantic change, reusing
// atomdoc.ContractHash rather than inventing a second semantic-identity
// concept. SemanticChangeAsserted lets a caller (for example a reviewer who
// knows the rename accompanies a behavior change not yet reflected in a
// recomputed contract hash) force the breaking classification defensively;
// it can only push the classification toward semantic-breaking, never away
// from it.
type RenameProposal struct {
	OldName                CanonicalName
	NewName                CanonicalName
	OldContractHash        atomdoc.ContractHash
	NewContractHash        atomdoc.ContractHash
	SemanticChangeAsserted bool
}

// ClassifyRename classifies proposal as formatting-only, semantic-
// preserving, or semantic-breaking (M21-157). Classification never depends
// on map iteration, wall-clock time, or any input besides proposal, so
// calling it twice with the same proposal always returns the same result.
func ClassifyRename(proposal RenameProposal) (RenameClassification, error) {
	if proposal.OldName.IsZero() || proposal.NewName.IsZero() {
		return "", fmt.Errorf("%w: both the old and new canonical names are required", ErrInvalidRenameProposal)
	}
	if proposal.OldName.String() == proposal.NewName.String() {
		return "", fmt.Errorf("%w: the new canonical name must differ from the old name", ErrInvalidRenameProposal)
	}
	if proposal.OldContractHash.IsZero() || proposal.NewContractHash.IsZero() {
		return "", fmt.Errorf("%w: both the old and new contract hashes are required to classify a rename", ErrInvalidRenameProposal)
	}

	contractChanged := proposal.OldContractHash.String() != proposal.NewContractHash.String()
	if contractChanged || proposal.SemanticChangeAsserted {
		return RenameClassificationSemanticBreaking, nil
	}
	if sameNormalizedWords(proposal.OldName, proposal.NewName) {
		return RenameClassificationFormattingOnly, nil
	}
	return RenameClassificationSemanticPreserving, nil
}

func sameNormalizedWords(a, b CanonicalName) bool {
	wordsA := SplitIdentifierWords(a)
	wordsB := SplitIdentifierWords(b)
	if len(wordsA) != len(wordsB) {
		return false
	}
	for index := range wordsA {
		if !strings.EqualFold(wordsA[index], wordsB[index]) {
			return false
		}
	}
	return true
}

// RenameAcceptance carries the review evidence AuthorizeCanonicalRename
// requires before it will authorize a semantic-preserving rename (M21-158).
// It is a structural nudge, not an enforcement mechanism: construction
// through NewRenameAcceptance requires a non-empty reviewer identity, so a
// rename can no longer be approved by an accidental or default-valued
// struct literal the way a bare exported bool could, but this package has
// no way to verify that review genuinely happened, that the named reviewer
// is who they claim to be, or that they were authorized to review — a
// caller that wants to bypass review can still fabricate any non-empty
// string. Real enforcement requires an actual reviewer authentication and
// authorization system, which is out of this package's scope; that system
// is what should construct a RenameAcceptance, not this package's callers.
type RenameAcceptance struct {
	reviewerIdentity string
}

// ErrInvalidRenameAcceptance classifies a rejected rename-acceptance
// construction.
var ErrInvalidRenameAcceptance = errors.New("invalid atom rename acceptance")

// NewRenameAcceptance constructs a RenameAcceptance carrying reviewerIdentity
// as the reviewer of record. reviewerIdentity must be non-empty; this
// package does not otherwise validate, authenticate, or authorize it (see
// RenameAcceptance's doc comment).
func NewRenameAcceptance(reviewerIdentity string) (RenameAcceptance, error) {
	trimmed := strings.TrimSpace(reviewerIdentity)
	if trimmed == "" {
		return RenameAcceptance{}, fmt.Errorf("%w: reviewer identity is required to approve a rename", ErrInvalidRenameAcceptance)
	}
	return RenameAcceptance{reviewerIdentity: trimmed}, nil
}

// Approved reports whether acceptance carries a reviewer identity. The zero
// value RenameAcceptance{} reports false.
func (acceptance RenameAcceptance) Approved() bool {
	return acceptance.reviewerIdentity != ""
}

// ReviewerIdentity returns the reviewer identity recorded on acceptance, or
// "" for the zero value.
func (acceptance RenameAcceptance) ReviewerIdentity() string {
	return acceptance.reviewerIdentity
}

// RenameOutcome is the typed result of one authorized canonical rename.
type RenameOutcome struct {
	Classification                   RenameClassification
	AtomID                           domain.AtomID
	EffectiveAtomVersion             atomdoc.AtomVersionID
	CreatedAlias                     *AtomNameAliasRecord
	RequiresNewDocumentationRevision bool
}

// AuthorizeCanonicalRename is the single enforcement point that stops a
// semantic-breaking rename from silently retaining the old identity
// (M21-157..160). That identity-retention enforcement (the bullet below on
// semantic-breaking classification) is real: it is a mechanical, unbypassable
// check on typed atom-version identity. The review requirement on a
// semantic-preserving rename (the next bullet) is not equivalently strong:
// acceptance.Approved() only proves a non-empty reviewer identity was
// supplied, not that a genuine review occurred (see RenameAcceptance's doc
// comment). Concretely:
//
//   - it classifies the proposal (M21-157);
//   - for a semantic-preserving classification, it rejects the rename
//     unless acceptance.Approved() is true (M21-158) and requires
//     oldVersion and newVersion to be the exact same atom-version identity
//     — a preserving rename never gets a new version;
//   - for a formatting-only classification, it likewise requires oldVersion
//     and newVersion to be the exact same atom-version identity;
//   - for a semantic-breaking classification, it rejects the rename with
//     ErrRenameRequiresNewIdentity unless newVersion differs from
//     oldVersion in atom identity, version number, or both — so a caller
//     cannot pass the same atomdoc.AtomVersionID through for a breaking
//     rename and have it silently accepted (M21-159, proven by M21-173);
//   - for formatting-only and semantic-preserving renames (both of which
//     keep the atom's identity), it constructs the prior-name alias record
//     the storage lane must persist (M21-156); a semantic-breaking rename
//     never gets an automatic alias, because aliasing the old name to a
//     semantically different atom would mislead retrieval, not assist it;
//   - it reports that every accepted rename requires a new documentation
//     revision (M21-160), leaving the revision's actual creation to
//     internal/atomdoc.
func AuthorizeCanonicalRename(
	proposal RenameProposal,
	acceptance RenameAcceptance,
	oldVersion atomdoc.AtomVersionID,
	newVersion atomdoc.AtomVersionID,
	occurredAt time.Time,
) (RenameOutcome, error) {
	classification, err := ClassifyRename(proposal)
	if err != nil {
		return RenameOutcome{}, err
	}
	if oldVersion.IsZero() || newVersion.IsZero() {
		return RenameOutcome{}, fmt.Errorf("%w: both the old and new atom-version identities are required", ErrInvalidRenameProposal)
	}
	if occurredAt.IsZero() {
		return RenameOutcome{}, fmt.Errorf("%w: occurred-at timestamp is required", ErrInvalidRenameProposal)
	}

	switch classification {
	case RenameClassificationFormattingOnly:
		if newVersion != oldVersion {
			return RenameOutcome{}, fmt.Errorf("%w: a formatting-only rename must keep the exact same atom version", ErrInvalidRenameProposal)
		}
	case RenameClassificationSemanticPreserving:
		if !acceptance.Approved() {
			return RenameOutcome{}, fmt.Errorf("%w: %q", ErrRenameReviewRequired, classification)
		}
		if newVersion != oldVersion {
			return RenameOutcome{}, fmt.Errorf("%w: a semantic-preserving rename must keep the exact same atom version", ErrInvalidRenameProposal)
		}
	case RenameClassificationSemanticBreaking:
		if newVersion == oldVersion {
			return RenameOutcome{}, fmt.Errorf("%w: %q", ErrRenameRequiresNewIdentity, classification)
		}
	default:
		return RenameOutcome{}, fmt.Errorf("%w: unknown rename classification %q", ErrInvalidRenameProposal, classification)
	}

	outcome := RenameOutcome{
		Classification:                   classification,
		AtomID:                           newVersion.AtomID(),
		EffectiveAtomVersion:             newVersion,
		RequiresNewDocumentationRevision: true,
	}
	if classification != RenameClassificationSemanticBreaking {
		alias, err := NewAliasFromPriorCanonicalName(newVersion.AtomID(), proposal.OldName, occurredAt)
		if err != nil {
			return RenameOutcome{}, err
		}
		outcome.CreatedAlias = &alias
	}
	return outcome, nil
}
