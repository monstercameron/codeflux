package atomname

import "testing"

// TestNewCanonicalNameAcceptsAgentsExampleNames proves M21-147 accepts the
// AGENTS.md "Atom Naming Style" example names, including names much longer
// than a short generic identifier would allow.
func TestNewCanonicalNameAcceptsAgentsExampleNames(t *testing.T) {
	examples := []string{
		"DerivePaymentAttemptIdempotencyKey",
		"ReserveAccountFundsUntilAuthorizationExpires",
		"ReconcileAmbiguousGatewayChargeOutcome",
		"LoadRepositorySymbolsAtGitRevision",
		"ValidateTaskBudgetBeforeModelRequest",
		"PersistSessionEventWithMonotonicSequence",
	}
	for _, example := range examples {
		if _, err := NewCanonicalName(example); err != nil {
			t.Errorf("NewCanonicalName(%q) unexpectedly failed: %v", example, err)
		}
	}
}

// TestNewCanonicalNameRejectsAgentsAvoidExamples proves the exact AGENTS.md
// "Avoid names such as" bare-generic-name and filler-word examples are
// rejected.
func TestNewCanonicalNameRejectsAgentsAvoidExamples(t *testing.T) {
	examples := []string{
		"Process", "Handle", "Execute", "RunTask", "DoPayment", "CheckData", "UpdateState",
		"AtomHelper", "PaymentManager",
	}
	for _, example := range examples {
		if _, err := NewCanonicalName(example); err == nil {
			t.Errorf("NewCanonicalName(%q) unexpectedly succeeded", example)
		}
	}
}

func TestNewCanonicalNameRejectsUnexportedAndMalformedIdentifiers(t *testing.T) {
	cases := []string{"", "deriveKey", "123Start", "Has Space", "Has-Dash", "func"}
	for _, raw := range cases {
		if _, err := NewCanonicalName(raw); err == nil {
			t.Errorf("NewCanonicalName(%q) unexpectedly succeeded", raw)
		}
	}
}

func TestNewCanonicalNameImposesNoShortSemanticLengthLimit(t *testing.T) {
	// A long, precise name must not be rejected merely for its length
	// (M21-147: "without imposing a short semantic length limit").
	long := "ReconcileAmbiguousGatewayChargeOutcomeAfterExtendedProviderRetryWindowExpires"
	if len(long) < 40 {
		t.Fatalf("test fixture is not actually long")
	}
	if _, err := NewCanonicalName(long); err != nil {
		t.Errorf("NewCanonicalName(%q) unexpectedly failed: %v", long, err)
	}
}

// TestSplitIdentifierWordsPreservesInitialisms proves M21-150: an
// initialism run (HTTP, ID, URL, ...) survives splitting as one token, not
// as one token per capital letter.
func TestSplitIdentifierWordsPreservesInitialisms(t *testing.T) {
	cases := map[string][]string{
		"ParseHTTPRequestID":                 {"Parse", "HTTP", "Request", "ID"},
		"LoadRepositorySymbolsAtGitRevision": {"Load", "Repository", "Symbols", "At", "Git", "Revision"},
		"FetchURLFromCache":                  {"Fetch", "URL", "From", "Cache"},
		"DeriveHTTPSEndpoint":                {"Derive", "HTTPS", "Endpoint"},
	}
	for raw, want := range cases {
		name, err := NewCanonicalName(raw)
		if err != nil {
			t.Fatalf("NewCanonicalName(%q) failed: %v", raw, err)
		}
		got := SplitIdentifierWords(name)
		if !equalStrings(got, want) {
			t.Errorf("SplitIdentifierWords(%q) = %v, want %v", raw, got, want)
		}
	}
}

// TestDeterministicConversionAmongCanonicalDisplayAndNormalized is M21-169:
// deriving the display name and normalized phrase from the same canonical
// name is a pure, repeatable function.
func TestDeterministicConversionAmongCanonicalDisplayAndNormalized(t *testing.T) {
	name, err := NewCanonicalName("ParseHTTPRequestID")
	if err != nil {
		t.Fatalf("NewCanonicalName failed: %v", err)
	}

	wantDisplay := "Parse HTTP Request ID"
	wantNormalized := "parse http request id"

	for i := 0; i < 5; i++ {
		display := DeriveDisplayName(name)
		normalized := DeriveNormalizedPhrase(name)
		if display.String() != wantDisplay {
			t.Fatalf("iteration %d: DeriveDisplayName = %q, want %q", i, display.String(), wantDisplay)
		}
		if normalized.String() != wantNormalized {
			t.Fatalf("iteration %d: DeriveNormalizedPhrase = %q, want %q", i, normalized.String(), wantNormalized)
		}
	}

	// Two independently constructed CanonicalName values from the same raw
	// identifier must derive identically.
	again, err := NewCanonicalName("ParseHTTPRequestID")
	if err != nil {
		t.Fatalf("NewCanonicalName failed: %v", err)
	}
	if DeriveDisplayName(again) != DeriveDisplayName(name) {
		t.Errorf("DeriveDisplayName is not deterministic across independently constructed identical names")
	}
	if DeriveNormalizedPhrase(again) != DeriveNormalizedPhrase(name) {
		t.Errorf("DeriveNormalizedPhrase is not deterministic across independently constructed identical names")
	}
}

// TestSplitIdentifierWordsHandlesAdjacentInitialismsAndAcronymTransitions is
// the reproduction for the adversarial-review word-splitter defect: adjacent
// allowlisted initialisms must delimit against each other (longest-match on
// the established-abbreviation allowlist) instead of merging into one
// nonsense token, and an acronym immediately followed by a digit or a lone
// plural "s" must not be silently split apart into meaningless fragments.
func TestSplitIdentifierWordsHandlesAdjacentInitialismsAndAcronymTransitions(t *testing.T) {
	cases := map[string][]string{
		// Adjacent approved initialisms ("URL" then "ID") must delimit
		// against each other instead of merging into the unestablished
		// "URLID" token.
		"HTTPSProxyURLID": {"HTTPS", "Proxy", "URL", "ID"},
		// A two-letter uppercase run with no abbreviation match ("OA") must
		// stay attached to its lowercase continuation as one stylized word
		// ("OAuth") instead of being split into "O"+"Auth"; the following
		// digit attaches to that word rather than becoming a lone token.
		"OAuth2Token": {"OAuth2", "Token"},
		// The approved two-letter "IP" abbreviation must not be split
		// across its own two letters; the following lowercase run and
		// trailing digit form their own word.
		"IPv6AddressRange": {"IP", "v6", "Address", "Range"},
		// An approved abbreviation immediately followed by a lone plural
		// "s" must stay merged as one token ("IDs"), not split into "ID"
		// and "s" (still an improvement over the old "I"+"Ds" corruption).
		"MergeDuplicateIDs": {"Merge", "Duplicate", "IDs"},
		"IDs":               {"IDs"},
		// A run of adjacent approved abbreviations with a trailing digit
		// that has no abbreviation match anywhere in its prefix must not
		// silently disappear into a merged blob; it decomposes into the
		// best-effort known pieces plus single-character fallback tokens,
		// with the digit attached to the immediately preceding piece.
		"AWSS3Bucket": {"A", "WS", "S3", "Bucket"},
		// A solitary uppercase letter at the very end of an identifier
		// (not an abbreviation, no lowercase to attach to) stays its own
		// single-letter token.
		"ExportZ": {"Export", "Z"},
		// A leading approved abbreviation must delimit correctly from the
		// capitalized word that follows it.
		"IDValidationResult": {"ID", "Validation", "Result"},
		// A trailing digit run attaches to the immediately preceding word
		// instead of becoming a lone numeric token.
		"ArchiveBatch42": {"Archive", "Batch42"},
	}
	for raw, want := range cases {
		name, err := NewCanonicalName(raw)
		if err != nil {
			t.Fatalf("NewCanonicalName(%q) failed: %v", raw, err)
		}
		got := SplitIdentifierWords(name)
		if !equalStrings(got, want) {
			t.Errorf("SplitIdentifierWords(%q) = %v, want %v", raw, got, want)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
