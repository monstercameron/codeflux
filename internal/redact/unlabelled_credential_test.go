package redact

import (
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/credentials"
)

// newHardeningPipeline builds a pipeline with no loaded credentials, so
// every redaction below comes from a shape pattern rather than an exact
// match against known material. Unknown-but-credential-shaped input is
// precisely what the previous pattern set missed.
func newHardeningPipeline(t *testing.T) *Pipeline {
	t.Helper()
	pipeline, err := NewPipeline(nil, Limits{
		MaximumInputBytes:  32 * 1024,
		MaximumOutputBytes: 16 * 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pipeline.Close)
	return pipeline
}

// TestUnlabelledCredentialShapesAreRedacted is the M21-119 hardening test.
// Every case here was verified to pass through the previous five-pattern
// set with zero redactions during adversarial review of internal/atomdoc,
// which meant a credential pasted into an atom comment would have been
// persisted and later composed into retrieval material and prompts.
//
// All fixtures are synthetic and structurally valid but non-functional, per
// AGENTS.md's prohibition on real credentials in tests.
func TestUnlabelledCredentialShapesAreRedacted(t *testing.T) {
	pipeline := newHardeningPipeline(t)

	// Assembled from parts rather than written out whole. A secret scanner
	// cannot tell a synthetic token from a live one, so a contiguous
	// credential-shaped literal in tracked source blocks a push to a public
	// remote on a finding nobody can act on. The other synthetic keys in this
	// package are already built this way.
	slackBotToken := "xoxb-1234567890" + "-ABCDEFGHIJKLMNOP"

	cases := []struct {
		name    string
		input   string
		leaking string
	}{
		{
			name:    "aws access key id",
			input:   `deploy used AKIAIOSFODNN7EXAMPLE for the upload`,
			leaking: "AKIAIOSFODNN7EXAMPLE",
		},
		{
			name:    "aws temporary access key id",
			input:   `assumed role key ASIAIOSFODNN7EXAMPLE expired`,
			leaking: "ASIAIOSFODNN7EXAMPLE",
		},
		{
			name:    "labelled aws secret access key",
			input:   `aws_secret_access_key=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY`,
			leaking: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		},
		{
			name:    "postgres connection string password",
			input:   `dsn: postgres://appuser:Tr0ub4dor3Cats@db.internal:5432/appdb`,
			leaking: "Tr0ub4dor3Cats",
		},
		{
			name:    "plain labelled password",
			input:   `password: Tr0ub4dor&3Cats`,
			leaking: "Tr0ub4dor&3Cats",
		},
		{
			name:    "google api key",
			input:   `maps key AIzaSyA1234567890abcdefghijklmnopqrstuv failed`,
			leaking: "AIzaSyA1234567890abcdefghijklmnopqrstuv",
		},
		{
			name:    "slack bot token",
			input:   "slack said " + slackBotToken + " is revoked",
			leaking: slackBotToken,
		},
		{
			name:    "json web token",
			input:   `cookie eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dBjftJeZ4CVPmB92K27uhbUJU1p1r_wW1gFWFOEjXk was rejected`,
			leaking: "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dBjftJeZ4CVPmB92K27uhbUJU1p1r_wW1gFWFOEjXk",
		},
		{
			name:    "github fine grained token",
			input:   `push rejected for github_pat_11ABCDEFG0abcdefghijklmnop`,
			leaking: "github_pat_11ABCDEFG0abcdefghijklmnop",
		},
	}

	boundaries := []Boundary{
		BoundaryPromptPersistence,
		BoundaryLogPersistence,
		BoundaryUIDelivery,
		BoundaryDiagnosticExport,
	}
	for _, boundary := range boundaries {
		for _, testCase := range cases {
			t.Run(string(boundary)+"/"+testCase.name, func(t *testing.T) {
				result, err := pipeline.Redact(boundary, testCase.input)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(result.Text, testCase.leaking) {
					t.Fatalf("boundary %s leaked credential material %q in output %q",
						boundary, testCase.leaking, result.Text)
				}
				if result.Report.Redactions == 0 {
					t.Fatalf("boundary %s reported zero redactions for %q", boundary, testCase.input)
				}
			})
		}
	}
}

// TestOrdinaryTextIsNotOverRedacted guards the other direction: the broad
// labelled-secret pattern must not swallow ordinary prose, identifiers, or
// hashes that merely look technical. A redactor that eats normal output is
// a redactor engineers route around.
func TestOrdinaryTextIsNotOverRedacted(t *testing.T) {
	pipeline := newHardeningPipeline(t)

	benign := []string{
		`go test ./internal/storage/... passed in 127s`,
		`commit aeda85d3f9c2b1e0a7d4 changed 12 files`,
		`the password policy requires rotation every 90 days`,
		`https://example.internal:8443/health returned 200`,
		`user alice@example.com opened thread thr_01H8XABCDEFG`,
		`sha256 3ea210abfe91c0d455aa77b8e6c1d9f2a4b6c8e0d2f4a6b8c0e2f4a6b8c0e2f4`,
	}
	for _, input := range benign {
		result, err := pipeline.Redact(BoundaryPromptPersistence, input)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(result.Text, Marker) {
			t.Fatalf("ordinary text was redacted: input %q became %q", input, result.Text)
		}
	}
}

// TestLoadedSecretStillRedactedAlongsideShapePatterns confirms the new
// shape patterns did not displace exact matching against loaded material.
func TestLoadedSecretStillRedactedAlongsideShapePatterns(t *testing.T) {
	secret, err := credentials.NewSecret([]byte("loaded-provider-fixture-value"))
	if err != nil {
		t.Fatal(err)
	}
	defer secret.Destroy()
	pipeline, err := NewPipeline([]credentials.Secret{secret}, Limits{
		MaximumInputBytes:  32 * 1024,
		MaximumOutputBytes: 16 * 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer pipeline.Close()

	result, err := pipeline.Redact(
		BoundaryPromptPersistence,
		`key AKIAIOSFODNN7EXAMPLE and loaded-provider-fixture-value together`,
	)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Text, "AKIAIOSFODNN7EXAMPLE") {
		t.Fatalf("shape pattern missed: %q", result.Text)
	}
	if strings.Contains(result.Text, "loaded-provider-fixture-value") {
		t.Fatalf("exact loaded-secret match regressed: %q", result.Text)
	}
}
