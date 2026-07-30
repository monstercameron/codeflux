package redact

import (
	"encoding/json"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/credentials"
)

func TestSecretCorpusRedactsAtEveryBoundary(t *testing.T) {
	exactMaterial := "loaded-provider-fixture-value"
	secret, err := credentials.NewSecret([]byte(exactMaterial))
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
	corpus := []string{
		`command failed: OPENAI_API_KEY="sk-proj-ABCDEFGHIJKLMNOPQRSTUVWX"`,
		`provider exception included loaded-provider-fixture-value`,
		`Authorization: Bearer abcdefghijklmnopqrstuvwxyz012345`,
		`x-api-key = 'abcdefghijklmnopqrstuvwxyz012345'`,
		"-----BEGIN PRIVATE KEY-----\nfixture-private-body\n-----END PRIVATE KEY-----",
		"github response gh" + "p_" + "ABCDEFGHIJKLMNOPQRSTUVWXYZ123456",
	}
	for _, boundary := range []Boundary{
		BoundaryPromptPersistence,
		BoundaryLogPersistence,
		BoundaryUIDelivery,
		BoundaryDiagnosticExport,
	} {
		for _, fixture := range corpus {
			result, err := pipeline.Redact(boundary, fixture)
			if err != nil {
				t.Fatal(err)
			}
			if result.Report.Redactions == 0 || !strings.Contains(result.Text, Marker) {
				t.Fatalf("%s did not redact %q: %#v", boundary, fixture, result)
			}
			for _, forbidden := range []string{
				exactMaterial,
				"ABCDEFGHIJKLMNOPQRSTUVWX",
				"abcdefghijklmnopqrstuvwxyz012345",
				"fixture-private-body",
				"ABCDEFGHIJKLMNOPQRSTUVWXYZ123456",
			} {
				if strings.Contains(result.Text, forbidden) {
					t.Fatalf("%s retained material %q in %q", boundary, forbidden, result.Text)
				}
			}
			if strings.Contains(result.Text, strings.Repeat("*", len(exactMaterial))) {
				t.Fatal("redaction marker leaked original length")
			}
		}
	}
}

func TestStreamRedactsExactAndPatternValuesAcrossEveryChunkBoundary(t *testing.T) {
	exact := "split-exact-provider-value"
	secret, err := credentials.NewSecret([]byte(exact))
	if err != nil {
		t.Fatal(err)
	}
	defer secret.Destroy()
	pipeline, err := NewPipeline([]credentials.Secret{secret}, Limits{
		MaximumInputBytes:  16 * 1024,
		MaximumOutputBytes: 8 * 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer pipeline.Close()
	fixtures := []string{
		"before " + exact + " after",
		"before sk-proj-ABCDEFGHIJKLMNOPQRSTUVWX after",
		"before Authorization: Bearer abcdefghijklmnopqrstuvwxyz after",
	}
	for _, fixture := range fixtures {
		for split := 1; split < len(fixture); split++ {
			stream, err := pipeline.NewStream(BoundaryLogPersistence)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := stream.Write([]byte(fixture[:split])); err != nil {
				t.Fatal(err)
			}
			if _, err := stream.Write([]byte(fixture[split:])); err != nil {
				t.Fatal(err)
			}
			result, err := stream.Finalize()
			if err != nil {
				t.Fatal(err)
			}
			if result.Report.Redactions == 0 || !strings.Contains(result.Text, Marker) {
				t.Fatalf("split %d retained fixture: %q", split, result.Text)
			}
		}
	}
}

func TestStructuredFieldsAreRedactedBeforeSerialization(t *testing.T) {
	pipeline, err := NewPipeline(nil, Limits{
		MaximumInputBytes:  16 * 1024,
		MaximumOutputBytes: 8 * 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer pipeline.Close()
	encoded, report, err := pipeline.RedactFields(
		BoundaryDiagnosticExport,
		map[string]any{
			"provider": map[string]any{
				"api_key": "structured-secret-fixture",
				"status":  "failed with Bearer abcdefghijklmnopqrstuvwxyz",
			},
			"attempt": float64(1),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if report.Redactions != 2 ||
		strings.Contains(string(encoded), "structured-secret-fixture") ||
		strings.Contains(string(encoded), "abcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("structured redaction = %s, report=%#v", encoded, report)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}

	oversize, oversizeReport, err := pipeline.RedactFields(
		BoundaryDiagnosticExport,
		map[string]any{"safe": strings.Repeat("x", 9*1024)},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !oversizeReport.OutputTruncated || !json.Valid(oversize) {
		t.Fatalf("oversize structured result = %s, report=%#v", oversize, oversizeReport)
	}
}

func TestQuotingWhitespaceAndSizeLimitsCannotBypassRedaction(t *testing.T) {
	pipeline, err := NewPipeline(nil, Limits{
		MaximumInputBytes:  4096,
		MaximumOutputBytes: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer pipeline.Close()
	for _, fixture := range []string{
		`api_key="abcdefghijklmnopqrstuvwxyz012345"`,
		`api-key = 'abcdefghijklmnopqrstuvwxyz012345'`,
		`X-API-Key : abcdefghijklmnopqrstuvwxyz012345`,
		`Authorization = "Bearer abcdefghijklmnopqrstuvwxyz012345"`,
		`Bearer    "abcdefghijklmnopqrstuvwxyz012345"`,
	} {
		result, err := pipeline.Redact(BoundaryPromptPersistence, fixture)
		if err != nil {
			t.Fatal(err)
		}
		if result.Report.Redactions == 0 ||
			strings.Contains(result.Text, "abcdefghijklmnopqrstuvwxyz") {
			t.Fatalf("variation bypassed redaction: %q -> %q", fixture, result.Text)
		}
	}
	oversize := strings.Repeat("safe-prefix-", 500) +
		"sk-proj-ABCDEFGHIJKLMNOPQRSTUVWX"
	result, err := pipeline.Redact(BoundaryUIDelivery, oversize)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Report.InputTruncated ||
		len(result.Text) > 1024 ||
		strings.Contains(result.Text, "ABCDEFGHIJKLMNOPQRSTUVWX") {
		t.Fatalf("bounded result = %#v", result)
	}
	if result.Report.EntropyEnabled {
		t.Fatal("entropy detection must remain disabled until false positives are gated")
	}
}
