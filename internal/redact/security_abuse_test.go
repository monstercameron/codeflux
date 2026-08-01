package redact

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/credentials"
)

// abuseSecret is the loaded credential an attacker is trying to get out of the
// process. It is fixture material, not a real credential.
const abuseSecret = "codeflux-fixture-loaded-secret-9f3a2b7c4d1e"

func abusePipeline(t *testing.T) *Pipeline {
	t.Helper()
	secret, err := credentials.NewSecret([]byte(abuseSecret))
	if err != nil {
		t.Fatalf("new secret: %v", err)
	}
	pipeline, err := NewPipeline([]credentials.Secret{secret}, Limits{
		MaximumInputBytes:  64 << 10,
		MaximumOutputBytes: 32 << 10,
	})
	if err != nil {
		t.Fatalf("new pipeline: %v", err)
	}
	t.Cleanup(pipeline.Close)
	return pipeline
}

// TestM22_057_CredentialExfiltrationThroughCommandOutputIsBlocked is M22-057.
//
// A command the agent is allowed to run can print anything, so the redaction
// boundary is what stands between a loaded credential and every durable
// surface. Each case is a way real output smuggles a secret past a naive
// substring check.
func TestM22_057_CredentialExfiltrationThroughCommandOutputIsBlocked(t *testing.T) {
	pipeline := abusePipeline(t)

	exfiltration := []struct {
		name   string
		output string
	}{
		{"bare echo", abuseSecret},
		{"inside a line", "error: authentication failed for token " + abuseSecret + " at line 4"},
		{"repeated", abuseSecret + "\n" + abuseSecret + "\n" + abuseSecret},
		{"embedded in JSON", `{"token":"` + abuseSecret + `","ok":true}`},
		{"embedded in a URL", "https://user:" + abuseSecret + "@example.com/path"},
		{"split across many lines of context", strings.Repeat("log line\n", 100) + abuseSecret},
		{"surrounded by punctuation", "[" + abuseSecret + "]"},
	}
	for _, testCase := range exfiltration {
		t.Run(testCase.name, func(t *testing.T) {
			for _, boundary := range []Boundary{
				BoundaryPromptPersistence, BoundaryLogPersistence,
				BoundaryUIDelivery, BoundaryDiagnosticExport,
			} {
				result, err := pipeline.Redact(boundary, testCase.output)
				if err != nil {
					t.Fatalf("redact at %s: %v", boundary, err)
				}
				if strings.Contains(result.Text, abuseSecret) {
					t.Fatalf("the loaded credential survived redaction at %s", boundary)
				}
				if result.Report.Redactions == 0 {
					t.Fatalf("no redaction was reported at %s despite the secret being present", boundary)
				}
			}
		})
	}
}

// TestM22_058_CredentialExfiltrationThroughDiagnosticExportIsBlocked is
// M22-058. A diagnostic export is the surface most likely to be mailed to
// someone else, so it must be no weaker than any other boundary — including
// for credential SHAPES that were never loaded into this process.
func TestM22_058_CredentialExfiltrationThroughDiagnosticExportIsBlocked(t *testing.T) {
	pipeline := abusePipeline(t)

	unloaded := []struct {
		name  string
		value string
	}{
		{"aws access key", "AKIAIOSFODNN7EXAMPLE"},
		{"labelled aws secret", "aws_secret_access_key = wJalrXUtnFEMIK7MDENGbPxRfiCYEXAMPLEKEY"},
		{"google api key", "AIza" + strings.Repeat("B", 35)},
		{"slack token", "xoxb-" + strings.Repeat("0", 12) + "-" +
			strings.Repeat("0", 12) + "-abcdefghijklmnopqrstuvwx"},
		{"bearer jwt", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.7f8sVc0Xk3mQ2n1pLbTgYw5RhZaJqO4dEuKcMvFxNiA"},
		{"github fine-grained token", "github_pat_" + strings.Repeat("A", 22) + "_" + strings.Repeat("b", 59)},
		{"labelled password", `password: "hunter2-not-a-real-password"`},
		{"uri embedded password", "postgres://admin:s3cr3t-not-real@db.internal:5432/app"},
	}
	for _, testCase := range unloaded {
		t.Run(testCase.name, func(t *testing.T) {
			result, err := pipeline.Redact(BoundaryDiagnosticExport,
				"collected diagnostics\n"+testCase.value+"\nend of section")
			if err != nil {
				t.Fatalf("redact: %v", err)
			}
			if strings.Contains(result.Text, testCase.value) {
				t.Fatalf("credential shape %q survived the diagnostic export", testCase.name)
			}
			if result.Report.Redactions == 0 {
				t.Fatalf("credential shape %q produced no redaction", testCase.name)
			}
		})
	}
}

// TestM22_058_StructuredDiagnosticFieldsAreRedacted proves the export path
// through structured fields is not a way around the text pipeline.
func TestM22_058_StructuredDiagnosticFieldsAreRedacted(t *testing.T) {
	pipeline := abusePipeline(t)

	fields := map[string]any{
		"command":  "deploy --token " + abuseSecret,
		"password": "hunter2-not-a-real-password",
		"token":    abuseSecret,
		"exitCode": 1,
		"ok":       false,
	}
	encoded, report, err := pipeline.RedactFields(BoundaryDiagnosticExport, fields)
	if err != nil {
		t.Fatalf("redact fields: %v", err)
	}
	if report.Redactions == 0 {
		t.Fatal("structured redaction reported no redactions")
	}
	// The assertion is made against the ENCODED export, which is what actually
	// leaves the process, rather than against an intermediate map.
	if strings.Contains(string(encoded), abuseSecret) {
		t.Fatalf("the export carried the loaded credential: %s", encoded)
	}
	if strings.Contains(string(encoded), "hunter2-not-a-real-password") {
		t.Fatalf("the export carried a labelled password: %s", encoded)
	}

	// Non-sensitive scalars must survive, or a diagnostic export would be
	// useless and nobody would trust the redaction that produced it.
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decode export: %v", err)
	}
	if decoded["exitCode"] != float64(1) || decoded["ok"] != false {
		t.Fatalf("redaction destroyed non-sensitive diagnostic fields: %#v", decoded)
	}
}

// TestM22_057_StreamedOutputCannotSmuggleASecretAcrossChunkBoundaries proves
// the streaming path, which is how command output actually arrives, does not
// leak a secret that straddles two writes. A per-chunk redactor would pass
// every other test in this file and still leak here.
func TestM22_057_StreamedOutputCannotSmuggleASecretAcrossChunkBoundaries(t *testing.T) {
	for split := 1; split < len(abuseSecret); split++ {
		pipeline := abusePipeline(t)
		stream, err := pipeline.NewStream(BoundaryLogPersistence)
		if err != nil {
			t.Fatalf("new stream: %v", err)
		}
		if _, err := stream.Write([]byte("token=" + abuseSecret[:split])); err != nil {
			t.Fatalf("write first chunk: %v", err)
		}
		if _, err := stream.Write([]byte(abuseSecret[split:] + " done")); err != nil {
			t.Fatalf("write second chunk: %v", err)
		}
		result, err := stream.Finalize()
		if err != nil {
			t.Fatalf("finalize: %v", err)
		}
		if strings.Contains(result.Text, abuseSecret) {
			t.Fatalf("a secret split at byte %d survived streamed redaction", split)
		}
	}
}

// TestM22_062_OversizedToolOutputIsBoundedNotAccepted is the tool-output third
// of M22-062.
//
// A command the agent runs can emit unbounded output, so "reject it" is not an
// option — the output already exists. The requirement is that it is bounded
// before it reaches any durable surface, and that the truncation is REPORTED
// rather than silent, so nobody reads a clipped log as a complete one.
func TestM22_062_OversizedToolOutputIsBoundedNotAccepted(t *testing.T) {
	pipeline := abusePipeline(t)
	const inputBound = 64 << 10
	const outputBound = 32 << 10

	flood := strings.Repeat("A", inputBound*4)
	result, err := pipeline.Redact(BoundaryLogPersistence, flood)
	if err != nil {
		t.Fatalf("redact flood: %v", err)
	}
	if len(result.Text) > outputBound {
		t.Fatalf("redacted output is %d bytes, above the %d byte bound", len(result.Text), outputBound)
	}
	if !result.Report.InputTruncated && !result.Report.OutputTruncated {
		t.Fatal("output was clipped without reporting truncation")
	}

	// The same must hold for the streaming path, where the flood arrives in
	// pieces and no single write is oversized.
	stream, err := pipeline.NewStream(BoundaryLogPersistence)
	if err != nil {
		t.Fatalf("new stream: %v", err)
	}
	for range 256 {
		if _, err := stream.Write([]byte(strings.Repeat("B", 4096))); err != nil {
			t.Fatalf("write chunk: %v", err)
		}
	}
	streamed, err := stream.Finalize()
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if len(streamed.Text) > outputBound {
		t.Fatalf("streamed output is %d bytes, above the %d byte bound",
			len(streamed.Text), outputBound)
	}
	if !streamed.Report.InputTruncated && !streamed.Report.OutputTruncated {
		t.Fatal("streamed output was clipped without reporting truncation")
	}

	// A secret arriving AFTER the truncation point must still not appear: a
	// bound that stops redacting once it stops storing would leak whatever it
	// did keep from that region.
	late, err := pipeline.Redact(BoundaryLogPersistence,
		strings.Repeat("C", outputBound)+abuseSecret+strings.Repeat("D", 1024))
	if err != nil {
		t.Fatalf("redact late secret: %v", err)
	}
	if strings.Contains(late.Text, abuseSecret) {
		t.Fatal("a secret past the output bound survived")
	}
}

// TestM22_057_RedactionReportCarriesNoSecretMaterial guards the metadata
// itself: a report that leaked the secret, its length, or its offsets would
// hand back exactly what redaction removed.
func TestM22_057_RedactionReportCarriesNoSecretMaterial(t *testing.T) {
	pipeline := abusePipeline(t)
	result, err := pipeline.Redact(BoundaryLogPersistence, "token="+abuseSecret)
	if err != nil {
		t.Fatalf("redact: %v", err)
	}
	encoded, err := json.Marshal(result.Report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	if strings.Contains(string(encoded), abuseSecret) {
		t.Fatal("the redaction report contained the secret")
	}
	if strings.Contains(string(encoded), base64.StdEncoding.EncodeToString([]byte(abuseSecret))) {
		t.Fatal("the redaction report contained the secret in encoded form")
	}
}
