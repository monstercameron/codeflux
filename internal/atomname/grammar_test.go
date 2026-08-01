package atomname

import (
	"errors"
	"testing"
)

func mustCanonicalName(t *testing.T, raw string) CanonicalName {
	t.Helper()
	name, err := NewCanonicalName(raw)
	if err != nil {
		t.Fatalf("NewCanonicalName(%q) failed: %v", raw, err)
	}
	return name
}

// TestValidateNamingGrammarAllowlistedAbbreviationPasses proves M21-151's
// default allowlist accepts an established abbreviation like ID without any
// exception.
func TestValidateNamingGrammarAllowlistedAbbreviationPasses(t *testing.T) {
	name := mustCanonicalName(t, "DeriveSessionID")
	err := ValidateNamingGrammar(name, NameSubjectKindExecutableAtom, DefaultNamingLexicon(), nil)
	if err != nil {
		t.Errorf("ValidateNamingGrammar unexpectedly failed: %v", err)
	}
}

// TestValidateNamingGrammarRejectsUnestablishedAbbreviation proves an
// initialism outside the allowlist is rejected without a matching
// exception.
func TestValidateNamingGrammarRejectsUnestablishedAbbreviation(t *testing.T) {
	name := mustCanonicalName(t, "DeriveVIPScore")
	err := ValidateNamingGrammar(name, NameSubjectKindExecutableAtom, DefaultNamingLexicon(), nil)
	if !errors.Is(err, ErrNamingGrammarViolation) {
		t.Fatalf("expected ErrNamingGrammarViolation, got %v", err)
	}
}

// TestValidateNamingGrammarProjectAbbreviationExtensionMechanism proves
// M21-151's project extension mechanism: extending the lexicon makes a
// project-specific abbreviation acceptable without a per-name exception.
func TestValidateNamingGrammarProjectAbbreviationExtensionMechanism(t *testing.T) {
	name := mustCanonicalName(t, "DeriveVIPScore")
	lexicon := DefaultNamingLexicon()

	if err := ValidateNamingGrammar(name, NameSubjectKindExecutableAtom, lexicon, nil); err == nil {
		t.Fatalf("expected the unextended lexicon to reject VIP")
	}

	extended, err := lexicon.WithProjectAbbreviations(map[string]string{"VIP": "very important customer tier"})
	if err != nil {
		t.Fatalf("WithProjectAbbreviations failed: %v", err)
	}
	if err := ValidateNamingGrammar(name, NameSubjectKindExecutableAtom, extended, nil); err != nil {
		t.Errorf("ValidateNamingGrammar unexpectedly failed after extending the lexicon: %v", err)
	}

	// The original lexicon value must remain unextended (immutability).
	if err := ValidateNamingGrammar(name, NameSubjectKindExecutableAtom, lexicon, nil); err == nil {
		t.Errorf("extending a derived lexicon must not mutate the original lexicon value")
	}
}

func TestNamingLexiconWithProjectAbbreviationsRejectsInvalidEntries(t *testing.T) {
	lexicon := DefaultNamingLexicon()
	if _, err := lexicon.WithProjectAbbreviations(map[string]string{"vip": "lowercase token"}); err == nil {
		t.Error("expected a lowercase abbreviation token to be rejected")
	}
	if _, err := lexicon.WithProjectAbbreviations(map[string]string{"VIP": ""}); err == nil {
		t.Error("expected an empty expansion to be rejected")
	}
}

func TestValidateNamingGrammarAbbreviationExceptionBypassesAllowlist(t *testing.T) {
	name := mustCanonicalName(t, "DeriveVIPScore")
	exceptions := []NamingException{{
		Kind:   NamingExceptionKindAbbreviation,
		Token:  "VIP",
		Reason: "domain team confirmed VIP is the standard term for this tier",
	}}
	if err := ValidateNamingGrammar(name, NameSubjectKindExecutableAtom, DefaultNamingLexicon(), exceptions); err != nil {
		t.Errorf("ValidateNamingGrammar unexpectedly failed with a matching abbreviation exception: %v", err)
	}
}

func TestValidateNamingGrammarActionVerbRequirement(t *testing.T) {
	name := mustCanonicalName(t, "PaymentAttemptKey")
	lexicon := DefaultNamingLexicon()

	if err := ValidateNamingGrammar(name, NameSubjectKindExecutableAtom, lexicon, nil); !errors.Is(err, ErrNamingGrammarViolation) {
		t.Fatalf("expected a missing action-verb start to be rejected for an executable atom, got %v", err)
	}
	if err := ValidateNamingGrammar(name, NameSubjectKindNonExecutableAtom, lexicon, nil); err != nil {
		t.Errorf("a non-executable atom must not require an action-verb start: %v", err)
	}

	exceptions := []NamingException{{Kind: NamingExceptionKindActionVerb, Reason: "domain-object-shaped modeled atom, reviewed"}}
	if err := ValidateNamingGrammar(name, NameSubjectKindExecutableAtom, lexicon, exceptions); err != nil {
		t.Errorf("a matching action-verb exception must bypass the requirement: %v", err)
	}
}

func TestValidateNamingGrammarProviderSpecificRequiresException(t *testing.T) {
	name := mustCanonicalName(t, "SendStripeRefund")
	lexicon := DefaultNamingLexicon()

	if err := ValidateNamingGrammar(name, NameSubjectKindExecutableAtom, lexicon, nil); !errors.Is(err, ErrNamingGrammarViolation) {
		t.Fatalf("expected a provider name without an exception to be rejected, got %v", err)
	}

	exceptions := []NamingException{{Kind: NamingExceptionKindProviderSpecific, Reason: "refund semantics are genuinely Stripe-specific, not an adapter binding"}}
	if err := ValidateNamingGrammar(name, NameSubjectKindExecutableAtom, lexicon, exceptions); err != nil {
		t.Errorf("a matching provider-specific exception must bypass the requirement: %v", err)
	}
}

func TestValidateNamingGrammarGuaranteeClaimRequiresException(t *testing.T) {
	name := mustCanonicalName(t, "GuaranteedDeliveryStatus")
	lexicon := DefaultNamingLexicon()

	if err := ValidateNamingGrammar(name, NameSubjectKindNonExecutableAtom, lexicon, nil); !errors.Is(err, ErrNamingGrammarViolation) {
		t.Fatalf("expected an unsupported guarantee-claim word to be rejected, got %v", err)
	}

	exceptions := []NamingException{{Kind: NamingExceptionKindGuaranteeClaim, Reason: "contract and evidence establish at-least-once delivery guarantees"}}
	if err := ValidateNamingGrammar(name, NameSubjectKindNonExecutableAtom, lexicon, exceptions); err != nil {
		t.Errorf("a matching guarantee-claim exception must bypass the requirement: %v", err)
	}
}

func TestParseNamingExceptionDirective(t *testing.T) {
	exception, err := ParseNamingExceptionDirective("codeflux:atom-name-exception abbreviation VIP: domain team confirmed usage")
	if err != nil {
		t.Fatalf("ParseNamingExceptionDirective failed: %v", err)
	}
	if exception.Kind != NamingExceptionKindAbbreviation || exception.Token != "VIP" || exception.Reason != "domain team confirmed usage" {
		t.Errorf("unexpected parsed exception: %+v", exception)
	}

	if _, err := ParseNamingExceptionDirective("codeflux:atom-name-exception action-verb: reviewed"); err != nil {
		t.Errorf("ParseNamingExceptionDirective failed on a no-token kind: %v", err)
	}

	if _, err := ParseNamingExceptionDirective("not a directive"); err == nil {
		t.Error("expected a malformed directive to be rejected")
	}
	if _, err := ParseNamingExceptionDirective("codeflux:atom-name-exception unknown-kind: reason"); err == nil {
		t.Error("expected an unknown exception kind to be rejected")
	}
	if _, err := ParseNamingExceptionDirective("codeflux:atom-name-exception abbreviation VIP:"); err == nil {
		t.Error("expected an empty reason to be rejected")
	}
}
