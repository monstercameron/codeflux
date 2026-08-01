package atomname

import (
	"errors"
	"fmt"
)

// ErrNamingGrammarViolation classifies a canonical name that fails an
// AGENTS.md "Atom Naming Style" grammar-policy rule not already enforced by
// NewCanonicalName.
var ErrNamingGrammarViolation = errors.New("atom name violates naming grammar policy")

// NameSubjectKind distinguishes an executable atom, which AGENTS.md
// requires to begin with a concrete action verb, from a non-executable
// (modeled or data-shape) atom, which does not carry that requirement.
type NameSubjectKind string

const (
	NameSubjectKindExecutableAtom    NameSubjectKind = "executable-atom"
	NameSubjectKindNonExecutableAtom NameSubjectKind = "non-executable-atom"
)

// IsValid reports whether kind is one of the two declared subject kinds.
func (kind NameSubjectKind) IsValid() bool {
	return kind == NameSubjectKindExecutableAtom || kind == NameSubjectKindNonExecutableAtom
}

// ValidateNamingGrammar enforces the AGENTS.md "Atom Naming Style" grammar
// rules that go beyond bare Go-identifier syntax (NewCanonicalName):
// executable atoms begin with a recognized action verb, all-uppercase
// initialism tokens are established domain abbreviations, the name does not
// name a known external provider, and the name does not contain a
// guarantee-claim word. Each rule is bypassed only by a validated
// NamingException of the matching kind — a reviewed record, never a blanket
// bypass (M21-151, M21-152's neighboring exception directives).
func ValidateNamingGrammar(name CanonicalName, kind NameSubjectKind, lexicon NamingLexicon, exceptions []NamingException) error {
	if name.IsZero() {
		return fmt.Errorf("%w: canonical name is required", ErrNamingGrammarViolation)
	}
	if !kind.IsValid() {
		return fmt.Errorf("%w: unknown name-subject kind %q", ErrNamingGrammarViolation, kind)
	}
	if len(exceptions) > MaximumNamingExceptions {
		return fmt.Errorf("%w: %q carries more than the maximum of %d naming exceptions", ErrNamingGrammarViolation, name.String(), MaximumNamingExceptions)
	}
	for _, exception := range exceptions {
		if err := exception.Validate(); err != nil {
			return err
		}
	}

	hasException := func(want NamingExceptionKind) bool {
		for _, exception := range exceptions {
			if exception.Kind == want {
				return true
			}
		}
		return false
	}
	hasAbbreviationException := func(token string) bool {
		for _, exception := range exceptions {
			if exception.Kind == NamingExceptionKindAbbreviation && exception.Token == token {
				return true
			}
		}
		return false
	}

	words := SplitIdentifierWords(name)
	if kind == NameSubjectKindExecutableAtom && !hasException(NamingExceptionKindActionVerb) {
		if !lexicon.isRecognizedActionVerb(words[0]) {
			return fmt.Errorf(
				"%w: %q does not begin with a recognized action verb; extend the project lexicon or add an %q naming exception",
				ErrNamingGrammarViolation, name.String(), NamingExceptionKindActionVerb)
		}
	}
	for _, word := range words {
		if isAcronymToken(word) && !lexicon.isApprovedAbbreviation(word) && !hasAbbreviationException(word) {
			return fmt.Errorf(
				"%w: %q uses unestablished abbreviation %q; extend the project lexicon or add an %q naming exception",
				ErrNamingGrammarViolation, name.String(), word, NamingExceptionKindAbbreviation)
		}
		if lexicon.isKnownProviderName(word) && !hasException(NamingExceptionKindProviderSpecific) {
			return fmt.Errorf(
				"%w: %q names provider %q; add a %q naming exception only if the atom's semantics are genuinely provider-specific",
				ErrNamingGrammarViolation, name.String(), word, NamingExceptionKindProviderSpecific)
		}
		if lexicon.isGuaranteeClaimWord(word) && !hasException(NamingExceptionKindGuaranteeClaim) {
			return fmt.Errorf(
				"%w: %q claims unsupported guarantee word %q; add a %q naming exception only if the contract and evidence support it",
				ErrNamingGrammarViolation, name.String(), word, NamingExceptionKindGuaranteeClaim)
		}
	}
	return nil
}

// isAcronymToken reports whether word is an all-uppercase (letters and
// digits, at least one letter) initialism token subject to the
// approved-abbreviation allowlist, as opposed to an ordinary capitalized
// word such as "Payment".
func isAcronymToken(word string) bool {
	if len(word) < 2 {
		return false
	}
	hasLetter := false
	for _, r := range word {
		switch {
		case r >= 'A' && r <= 'Z':
			hasLetter = true
		case r >= '0' && r <= '9':
			// digits are permitted within an acronym token
		default:
			return false
		}
	}
	return hasLetter
}
