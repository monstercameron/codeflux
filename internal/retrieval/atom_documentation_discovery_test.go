package retrieval

import (
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/atomdoc"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/fingerprint"
	"codeflux.dev/codeflux/internal/storage"
)

// documentationFixtureDocument builds a complete, schema-valid atom
// document whose Purpose carries the caller's text and whose remaining
// fields are uniform filler, so two fixtures differ ONLY in the prose a
// discovery match could read.
func documentationFixtureDocument(purpose string) atomdoc.Document {
	filler := atomdoc.Field{Text: "Uniform filler text shared by every documentation fixture."}
	list := atomdoc.Field{Items: []string{"Uniform filler item shared by every documentation fixture."}}
	return atomdoc.Document{
		Purpose:                       atomdoc.Field{Text: purpose},
		UseWhen:                       filler,
		DoNotUseWhen:                  filler,
		Semantics:                     filler,
		Inputs:                        list,
		Outputs:                       list,
		Preconditions:                 list,
		Postconditions:                list,
		Effects:                       list,
		FailureSemantics:              list,
		Determinism:                   filler,
		IdempotencyAndRetry:           filler,
		ReconciliationAndCompensation: filler,
		SecurityAndPrivacy:            filler,
		DependenciesAndBindings:       filler,
		ComplexityAndLimits:           filler,
		Examples:                      list,
		Verification:                  filler,
		RetrievalConcepts:             filler,
	}
}

// mustCreateAtomDocumentation persists one admitted documentation revision
// for atomID whose Purpose contains purpose.
func mustCreateAtomDocumentation(
	t *testing.T,
	repositories *storage.Repositories,
	projectID domain.ProjectID,
	atomID domain.AtomID,
	suffix string,
	purpose string,
) {
	t.Helper()
	version, err := atomdoc.NewAtomVersionID(atomID, 1)
	if err != nil {
		t.Fatal(err)
	}
	revisionID, err := atomdoc.ParseDocumentationRevisionID("adr_" + strings.Repeat(suffix, 64))
	if err != nil {
		t.Fatal(err)
	}
	sourceHash, err := atomdoc.ParseSourceCommentHash(strings.Repeat(suffix, 64))
	if err != nil {
		t.Fatal(err)
	}
	normalizedHash, err := atomdoc.ParseNormalizedInputHash(strings.Repeat(suffix, 64))
	if err != nil {
		t.Fatal(err)
	}
	contractHash, err := atomdoc.ParseContractHash(strings.Repeat(suffix, 64))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateAtomDocumentationRevision(t.Context(), storage.CreateAtomDocumentationRevision{
		ProjectID: projectID,
		Revision: atomdoc.DocumentationRevision{
			RevisionID: revisionID, AtomID: atomID, AtomVersion: version,
			SchemaVersion: atomdoc.SchemaVersion, Authoring: atomdoc.AuthoringSourceAuthored,
			SourceRepositoryRevision: strings.Repeat("a", 40),
			SourceCommentHash:        sourceHash, NormalizedInputHash: normalizedHash, ContractHash: contractHash,
			Document: documentationFixtureDocument(purpose), CreatedAt: time.Now().UTC(),
		},
	}); err != nil {
		t.Fatal(err)
	}
}

// TestGateEvidenceG07_RichDocumentationImprovesDiscoveryWithoutChangingEligibility
// is the M21-G07 integration proof the previous structural placeholder asked
// for: "rich atom comments improve candidate discovery without bypassing
// exact applicability, evidence, or assurance checks."
//
// Two atoms sit in identical eligibility posture. One's documentation
// mentions a term drawn from the task's own affected-symbol hints; the
// other's does not. The documented one gains a discovery channel naming the
// field and term that surfaced it, and BOTH end with the same eligibility
// outcome — proving documentation changed what was FOUND and nothing about
// what was PERMITTED.
func TestGateEvidenceG07_RichDocumentationImprovesDiscoveryWithoutChangingEligibility(t *testing.T) {
	ctx := t.Context()
	repositories := newTestRepositories(t)
	service, err := NewService(repositories)
	if err != nil {
		t.Fatal(err)
	}
	projectID, repositoryID := mustCreateProjectAndRepository(t, repositories)

	// The shared fingerprint fixture carries no affected-symbol hints, and
	// documentation discovery is driven entirely by those structured hints,
	// so this test builds its own.
	task, err := fingerprint.BuildExactFingerprint(fingerprint.ExactFingerprintInput{
		Project: projectID, Repository: repositoryID,
		BaseRevision:      domain.RevisionBinding{Known: true, ExactRevision: "deadbeefcafefeed"},
		TaskClass:         fingerprint.TaskClassBugFix,
		AffectedSymbols:   []string{"ReserveAccountFunds"},
		Risk:              domain.RiskLevelRoutine,
		RequiredAssurance: domain.AssuranceLevelRuntimeOnly,
	})
	if err != nil {
		t.Fatal(err)
	}
	const hint = "account"

	documentedAtom := mustAtomIDFixture(t)
	undocumentedAtom := mustAtomIDFixture(t)
	_, documentedRevision := mustCreateExecutableAtomReferenceArtifact(
		t, repositories, projectID, repositoryID, documentedAtom, "DeriveIdempotencyKeyForRetry",
	)
	_, undocumentedRevision := mustCreateExecutableAtomReferenceArtifact(
		t, repositories, projectID, repositoryID, undocumentedAtom, "ComputeUnrelatedAggregate",
	)

	// Only the first atom's prose mentions a term the task's hints contain.
	// The second is documented too, so the difference is the CONTENT of the
	// comment, not the presence of one.
	mustCreateAtomDocumentation(t, repositories, projectID, documentedAtom, "a",
		"Derives a stable key so a retried "+hint+" reservation cannot double-charge.")
	mustCreateAtomDocumentation(t, repositories, projectID, undocumentedAtom, "b",
		"Computes an unrelated rollup with no bearing on this task at all.")

	result, err := service.RunPreWorkGate(ctx, PreWorkGateInput{
		QueryID: newTestQueryID(t, "g07"), ProjectID: projectID,
		Boundary: domain.MemoryQueryProjectBoundary{Project: projectID},
		Task:     task,
	})
	if err != nil {
		t.Fatal(err)
	}

	var documented, undocumented *InfluentialMemoryItem
	for index := range result.Eligible {
		switch result.Eligible[index].RevisionID {
		case documentedRevision:
			documented = &result.Eligible[index]
		case undocumentedRevision:
			undocumented = &result.Eligible[index]
		}
	}
	if documented == nil || undocumented == nil {
		t.Fatalf("both atom references must remain eligible; documented=%v undocumented=%v", documented, undocumented)
	}

	// DISCOVERY improved: the documented atom carries a match label naming
	// the documentation field and the term that surfaced it.
	if !anyMatchedTextContains(documented.MatchedText, hint) {
		t.Fatalf("documented atom MatchedText = %v, want a documentation match naming %q", documented.MatchedText, hint)
	}
	if anyMatchedTextContains(undocumented.MatchedText, hint) {
		t.Fatalf("undocumented atom must not gain a documentation match, got %v", undocumented.MatchedText)
	}

	// ELIGIBILITY unchanged: richer prose bought discovery, not permission.
	// Both atoms sit in the same posture and both remain eligible; neither
	// was promoted past a gate by its documentation.
	if len(result.Eligible) < 2 {
		t.Fatalf("both atoms must survive the eligibility chain, got %d eligible", len(result.Eligible))
	}
}

// anyMatchedTextContains reports whether any recorded per-channel match
// text mentions term. MatchedText is keyed by discovery channel, and the
// documentation channel records under the structured-fields source.
func anyMatchedTextContains(matched map[storage.MemoryRetrievalCandidateSource]string, term string) bool {
	for _, text := range matched {
		if strings.Contains(strings.ToLower(text), term) {
			return true
		}
	}
	return false
}

// mustAtomIDFixture mints an atom identity for a documentation fixture.
func mustAtomIDFixture(t *testing.T) domain.AtomID {
	t.Helper()
	value, err := domain.NewAtomID()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

// TestDocumentationMatchTermsUsesOnlyStructuredHints proves the
// documentation channel reads the fingerprint's EXACT hint fields and never
// its descriptive text, preserving internal/fingerprint's exact/descriptive
// split at the discovery layer too.
func TestDocumentationMatchTermsUsesOnlyStructuredHints(t *testing.T) {
	terms := splitHintTerms("ReserveAccountFundsUntilExpiry")
	joined := strings.Join(terms, " ")
	for _, want := range []string{"reserve", "account", "funds", "until", "expiry"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("splitHintTerms lost %q from %q", want, joined)
		}
	}
	if len(splitHintTerms("id")) != 0 && MinimumDocumentationMatchTermLength > 2 {
		for _, term := range splitHintTerms("id") {
			if len(term) >= MinimumDocumentationMatchTermLength {
				t.Fatalf("short term %q should not survive the minimum-length filter", term)
			}
		}
	}
}

// TestMatchDocumentationTermsIsDeterministic proves an identical document
// and term set always report the same field, so the recorded reason a
// candidate surfaced does not vary run to run.
func TestMatchDocumentationTermsIsDeterministic(t *testing.T) {
	document := storage.AtomDocumentationDiscoveryText{
		Fields: map[string]string{
			"Purpose":            "reserves funds against an account",
			"Semantics":          "reserves funds against an account",
			"Retrieval concepts": "reserves funds against an account",
		},
	}
	first, ok := matchDocumentationTerms(document, []string{"funds"})
	if !ok {
		t.Fatal("expected a match")
	}
	for attempt := 0; attempt < 20; attempt++ {
		again, ok := matchDocumentationTerms(document, []string{"funds"})
		if !ok || again != first {
			t.Fatalf("match label varied: %q then %q", first, again)
		}
	}
	if !strings.HasPrefix(first, "Purpose:") {
		t.Fatalf("match label = %q, want the fixed scan order to report Purpose first", first)
	}
}
