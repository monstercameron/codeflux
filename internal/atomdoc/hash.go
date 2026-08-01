package atomdoc

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// ErrInvalidSourceCommentHash classifies a malformed source-comment hash.
var ErrInvalidSourceCommentHash = errors.New("invalid atom source-comment hash")

// SourceCommentHash identifies the exact original doc-comment bytes captured
// before any normalization, scrubbing, or field parsing (M21-120). It is a
// distinct Go type from NormalizedInputHash so the two purposes — exact
// source identity versus normalized semantic identity — can never be
// interchanged by the type system. A formatting-only comment edit always
// changes this hash even when it leaves NormalizedInputHash unchanged.
type SourceCommentHash struct{ value string }

// ParseSourceCommentHash validates a lowercase hex SHA-256 digest.
func ParseSourceCommentHash(raw string) (SourceCommentHash, error) {
	if !validLowerHex(raw, 64) {
		return SourceCommentHash{}, fmt.Errorf("%w: must be 64 lowercase hexadecimal characters", ErrInvalidSourceCommentHash)
	}
	return SourceCommentHash{value: raw}, nil
}

// String returns the lowercase hex digest.
func (hash SourceCommentHash) String() string { return hash.value }

// IsZero reports whether no source-comment hash has been assigned.
func (hash SourceCommentHash) IsZero() bool { return hash.value == "" }

// ComputeSourceCommentHash hashes the exact raw comment-group text exactly as
// extracted from the syntax tree, before any whitespace normalization,
// scrubbing, or field parsing (M21-120).
func ComputeSourceCommentHash(rawCommentText string) SourceCommentHash {
	sum := sha256.Sum256([]byte(rawCommentText))
	return SourceCommentHash{value: hex.EncodeToString(sum[:])}
}

// ErrInvalidNormalizedInputHash classifies a malformed normalized-input hash.
var ErrInvalidNormalizedInputHash = errors.New("invalid atom normalized-input hash")

// NormalizedInputHash identifies the scrubbed, whitespace-normalized
// documentation content, independent of the original source formatting
// (M21-121). Two documents with identical semantic content hash identically
// regardless of indentation, wrapping, or trailing punctuation whitespace;
// any semantic difference in a field's substantive content changes the hash.
type NormalizedInputHash struct{ value string }

// ParseNormalizedInputHash validates a lowercase hex SHA-256 digest.
func ParseNormalizedInputHash(raw string) (NormalizedInputHash, error) {
	if !validLowerHex(raw, 64) {
		return NormalizedInputHash{}, fmt.Errorf("%w: must be 64 lowercase hexadecimal characters", ErrInvalidNormalizedInputHash)
	}
	return NormalizedInputHash{value: raw}, nil
}

// String returns the lowercase hex digest.
func (hash NormalizedInputHash) String() string { return hash.value }

// IsZero reports whether no normalized-input hash has been assigned.
func (hash NormalizedInputHash) IsZero() bool { return hash.value == "" }

// Field and record separators used only inside the canonical hash input.
// These are ASCII control characters (unit/record/group separators) that
// never appear in normalized field text, so they cannot be forged by
// crafting field content that straddles a field boundary.
const (
	canonicalGroupSeparator = "\x1d"
	canonicalItemSeparator  = "\x1e"
	canonicalLabelSeparator = "\x1f"
)

// ComputeNormalizedDocumentationInputHash hashes the canonical serialization
// of one normalized, scrubbed Document (M21-121).
func ComputeNormalizedDocumentationInputHash(schemaVersion uint32, doc Document) NormalizedInputHash {
	sum := sha256.Sum256(canonicalizeDocument(schemaVersion, doc))
	return NormalizedInputHash{value: hex.EncodeToString(sum[:])}
}

func canonicalizeDocument(schemaVersion uint32, doc Document) []byte {
	var builder strings.Builder
	fmt.Fprintf(&builder, "codeflux-atomdoc-normalized-input-v1%sschema%s%d%s",
		canonicalLabelSeparator, canonicalLabelSeparator, schemaVersion, canonicalGroupSeparator)
	for _, spec := range fieldSpecs {
		field := spec.get(&doc)
		builder.WriteString(spec.Label)
		builder.WriteString(canonicalLabelSeparator)
		switch {
		case field.None:
			builder.WriteString("None")
			builder.WriteString(canonicalLabelSeparator)
			builder.WriteString(field.Reason)
		case spec.Kind == FieldKindList:
			for index, item := range field.Items {
				if index > 0 {
					builder.WriteString(canonicalItemSeparator)
				}
				builder.WriteString(item)
			}
		default:
			builder.WriteString(field.Text)
		}
		builder.WriteString(canonicalGroupSeparator)
	}
	return []byte(builder.String())
}
