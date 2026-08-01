package atomdoc

import (
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/credentials"
	"codeflux.dev/codeflux/internal/redact"
)

func newTestPipeline(t *testing.T, loaded []credentials.Secret) *redact.Pipeline {
	t.Helper()
	pipeline, err := redact.NewPipeline(loaded, redact.Limits{
		MaximumInputBytes:  32 * 1024,
		MaximumOutputBytes: 16 * 1024,
	})
	if err != nil {
		t.Fatalf("construct redaction pipeline: %v", err)
	}
	t.Cleanup(pipeline.Close)
	return pipeline
}

func validDocumentForTest() Document {
	var doc Document
	fillRemainingFieldsForTest(&doc)
	return doc
}

// TestScrubAtomDocumentationForPersistenceUsesSharedPipeline proves M21-118:
// atom documentation is scrubbed through the exact shared
// internal/redact.Pipeline, and the field-shaped, non-sensitive content
// around a match survives scrubbing.
func TestScrubAtomDocumentationForPersistenceUsesSharedPipeline(t *testing.T) {
	// Obviously fake, non-functional credential-shaped fixtures per
	// AGENTS.md: never real secrets, even in tests.
	const fakeBearerToken = "abcdefghijklmnopqrstuvwxyz012345"
	pipeline := newTestPipeline(t, nil)

	doc := validDocumentForTest()
	doc.SecurityAndPrivacy = Field{
		Text: "Do not log the request header verbatim: Authorization: Bearer " + fakeBearerToken + " must never reach persistence.",
	}

	scrubbed, report, err := ScrubAtomDocumentationForPersistence(pipeline, doc)
	if err != nil {
		t.Fatalf("scrub: %v", err)
	}
	if report.Redactions == 0 {
		t.Fatal("expected the shared pipeline to report at least one redaction")
	}
	if report.Boundary != DefaultRedactionBoundary {
		t.Fatalf("unexpected redaction boundary: %s", report.Boundary)
	}
	if got := scrubbed.SecurityAndPrivacy.Text; strings.Contains(got, fakeBearerToken) {
		t.Fatalf("scrubbed field still contains the fake secret material: %q", got)
	}
	if !strings.Contains(scrubbed.SecurityAndPrivacy.Text, "must never reach persistence") {
		t.Fatalf("scrubbing must preserve surrounding non-sensitive content, got: %q", scrubbed.SecurityAndPrivacy.Text)
	}
}

// TestScrubAtomDocumentationForPersistenceRequiresPipeline proves this
// package refuses to run without the shared pipeline rather than silently
// skipping scrubbing.
func TestScrubAtomDocumentationForPersistenceRequiresPipeline(t *testing.T) {
	_, _, err := ScrubAtomDocumentationForPersistence(nil, validDocumentForTest())
	if err == nil {
		t.Fatal("expected an error when no redaction pipeline is supplied")
	}
}

// TestContainsSensitiveContentDetectsExactLoadedCredential proves M21-119:
// documentation carrying a currently loaded OS-credential exact value is
// detected so the admission pipeline rejects it outright.
func TestContainsSensitiveContentDetectsExactLoadedCredential(t *testing.T) {
	const fakeLoadedSecret = "fake-loaded-provider-fixture-value-only"
	secret, err := credentials.NewSecret([]byte(fakeLoadedSecret))
	if err != nil {
		t.Fatalf("construct fake secret: %v", err)
	}
	defer secret.Destroy()
	pipeline := newTestPipeline(t, []credentials.Secret{secret})

	doc := validDocumentForTest()
	doc.Examples = Field{Items: []string{
		"A caller accidentally logging " + fakeLoadedSecret + " is exactly the case this atom must never allow.",
	}}

	_, report, err := ScrubAtomDocumentationForPersistence(pipeline, doc)
	if err != nil {
		t.Fatalf("scrub: %v", err)
	}
	if !ContainsSensitiveContent(report) {
		t.Fatal("expected ContainsSensitiveContent to report true for an exact loaded credential match")
	}
}

// TestContainsSensitiveContentDetectsPrivateKeyFixture proves M21-119 for
// the built-in private-key pattern using an obviously fake PEM fixture body.
func TestContainsSensitiveContentDetectsPrivateKeyFixture(t *testing.T) {
	pipeline := newTestPipeline(t, nil)
	doc := validDocumentForTest()
	doc.DependenciesAndBindings = Field{
		Text: "-----BEGIN PRIVATE KEY-----\nfixture-only-non-functional-body\n-----END PRIVATE KEY-----",
	}
	_, report, err := ScrubAtomDocumentationForPersistence(pipeline, doc)
	if err != nil {
		t.Fatalf("scrub: %v", err)
	}
	if !ContainsSensitiveContent(report) {
		t.Fatal("expected ContainsSensitiveContent to report true for a private-key fixture")
	}
}

// TestContainsSensitiveContentAllowsCleanDocumentation proves the security
// gate does not false-positive on ordinary, non-sensitive documentation.
func TestContainsSensitiveContentAllowsCleanDocumentation(t *testing.T) {
	pipeline := newTestPipeline(t, nil)
	_, report, err := ScrubAtomDocumentationForPersistence(pipeline, validDocumentForTest())
	if err != nil {
		t.Fatalf("scrub: %v", err)
	}
	if ContainsSensitiveContent(report) {
		t.Fatal("expected clean documentation not to be flagged as sensitive")
	}
}
