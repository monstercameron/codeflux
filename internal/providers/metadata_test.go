package providers

import (
	"errors"
	"strings"
	"testing"
)

func TestSanitizeProviderMetadataRedactsAndCopiesBoundedFields(t *testing.T) {
	const secret = "synthetic-secret"
	input := RedactedProviderMetadata{
		RequestID:   "request-" + secret,
		ResponseID:  "response\nid",
		Fingerprint: "fingerprint",
		Fields: map[string]string{
			"service_tier": "private-" + secret,
		},
	}
	safe, err := SanitizeProviderMetadata(input, func(value string) string {
		return strings.ReplaceAll(value, secret, "[REDACTED]")
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(safe.RequestID, secret) ||
		strings.Contains(safe.Fields["service_tier"], secret) ||
		strings.ContainsAny(safe.ResponseID, "\r\n") {
		t.Fatalf("unsafe metadata = %#v", safe)
	}
	input.Fields["service_tier"] = "changed"
	if safe.Fields["service_tier"] == "changed" {
		t.Fatal("sanitized metadata aliases provider input")
	}
}

func TestSanitizeProviderMetadataRejectsMissingRedactorAndInvalidKeys(
	t *testing.T,
) {
	if _, err := SanitizeProviderMetadata(
		RedactedProviderMetadata{},
		nil,
	); !errors.Is(err, ErrUnsafeProviderMetadata) {
		t.Fatalf("missing redactor error = %v", err)
	}
	if _, err := SanitizeProviderMetadata(
		RedactedProviderMetadata{
			Fields: map[string]string{"authorization header": "value"},
		},
		func(value string) string { return value },
	); !errors.Is(err, ErrUnsafeProviderMetadata) {
		t.Fatalf("invalid key error = %v", err)
	}
}
