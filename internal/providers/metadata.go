package providers

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

const (
	maximumProviderMetadataFields = 32
	maximumProviderMetadataKey    = 64
	maximumProviderMetadataValue  = 512
	maximumProviderIdentifier     = 512
)

var ErrUnsafeProviderMetadata = errors.New("provider metadata is not safe to retain")

// SanitizeProviderMetadata bounds, redacts, and copies provider metadata before
// it may cross into storage, events, diagnostics, or UI delivery.
func SanitizeProviderMetadata(
	metadata RedactedProviderMetadata,
	redact func(string) string,
) (RedactedProviderMetadata, error) {
	if redact == nil {
		return RedactedProviderMetadata{}, fmt.Errorf(
			"%w: redaction callback is required",
			ErrUnsafeProviderMetadata,
		)
	}
	requestID, err := sanitizeProviderMetadataValue(
		"request ID", metadata.RequestID, maximumProviderIdentifier, redact,
	)
	if err != nil {
		return RedactedProviderMetadata{}, err
	}
	responseID, err := sanitizeProviderMetadataValue(
		"response ID", metadata.ResponseID, maximumProviderIdentifier, redact,
	)
	if err != nil {
		return RedactedProviderMetadata{}, err
	}
	fingerprint, err := sanitizeProviderMetadataValue(
		"fingerprint", metadata.Fingerprint, maximumProviderIdentifier, redact,
	)
	if err != nil {
		return RedactedProviderMetadata{}, err
	}
	if len(metadata.Fields) > maximumProviderMetadataFields {
		return RedactedProviderMetadata{}, fmt.Errorf(
			"%w: field count exceeds supported bounds",
			ErrUnsafeProviderMetadata,
		)
	}
	sanitized := RedactedProviderMetadata{
		RequestID: requestID, ResponseID: responseID, Fingerprint: fingerprint,
		Fields: make(map[string]string, len(metadata.Fields)),
	}
	for key, value := range metadata.Fields {
		if !validProviderMetadataKey(key) {
			return RedactedProviderMetadata{}, fmt.Errorf(
				"%w: field key %q is invalid",
				ErrUnsafeProviderMetadata,
				key,
			)
		}
		safe, err := sanitizeProviderMetadataValue(
			"field "+key, value, maximumProviderMetadataValue, redact,
		)
		if err != nil {
			return RedactedProviderMetadata{}, err
		}
		sanitized.Fields[key] = safe
	}
	return sanitized, nil
}

// SanitizeProviderIdentifier prepares one provider request identifier for the
// dedicated attributable-attempt column.
func SanitizeProviderIdentifier(
	value string,
	redact func(string) string,
) (string, error) {
	return sanitizeProviderMetadataValue(
		"provider request ID",
		value,
		maximumProviderIdentifier,
		redact,
	)
}

func sanitizeProviderMetadataValue(
	label string,
	value string,
	maximum int,
	redact func(string) string,
) (string, error) {
	if len(value) > maximum*4 {
		return "", fmt.Errorf(
			"%w: %s input exceeds pre-redaction bounds",
			ErrUnsafeProviderMetadata,
			label,
		)
	}
	value = strings.ToValidUTF8(redact(value), "\uFFFD")
	value = strings.TrimSpace(strings.Map(func(character rune) rune {
		if unicode.IsControl(character) {
			return -1
		}
		return character
	}, value))
	if len(value) > maximum {
		return "", fmt.Errorf(
			"%w: %s exceeds retained bounds",
			ErrUnsafeProviderMetadata,
			label,
		)
	}
	return value, nil
}

func validProviderMetadataKey(value string) bool {
	if value == "" || len(value) > maximumProviderMetadataKey {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' ||
			character == '_' || character == '-' || character == '.' {
			continue
		}
		return false
	}
	return true
}

func cloneRedactedProviderMetadata(
	metadata *RedactedProviderMetadata,
) *RedactedProviderMetadata {
	if metadata == nil {
		return nil
	}
	copy := *metadata
	copy.Fields = make(map[string]string, len(metadata.Fields))
	for key, value := range metadata.Fields {
		copy.Fields[key] = value
	}
	return &copy
}
