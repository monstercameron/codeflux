package atomdoc

import (
	"fmt"
	"strings"
)

// GenerateAtomDocumentationComment renders the exact schema-v1 Go doc-comment
// text for one SQLite-authored atom-documentation revision (M21-124). The
// rendered text is a generated projection (AuthoringGeneratedProjection): it
// is derived entirely from identifier, openingSentence, schemaVersion, and
// doc, and it must never be re-admitted as though it were source-authored
// content — the SQLite record remains authoritative.
//
// The generated comment uses exactly one line per field body and exactly one
// line per list item; this keeps the renderer free of word-wrap decisions
// while remaining fully valid Go and round-trip-parseable by
// LocateAtomDeclarationCandidates and ParseAtomDocumentationComment.
func GenerateAtomDocumentationComment(identifier, openingSentence string, schemaVersion uint32, doc Document) (string, error) {
	if identifier == "" {
		return "", fmt.Errorf("%w: identifier is required", ErrInvalidAtomDocumentation)
	}
	if err := ValidateAtomDocumentationOpeningSentence(identifier, openingSentence); err != nil {
		return "", err
	}
	if schemaVersion != SchemaVersion {
		return "", fmt.Errorf("%w: unsupported documentation schema version %d (expected %d)", ErrInvalidAtomDocumentation, schemaVersion, SchemaVersion)
	}

	var builder strings.Builder
	fmt.Fprintf(&builder, "// %s\n", openingSentence)
	builder.WriteString("//\n")
	fmt.Fprintf(&builder, "//%s\n", AtomDirectiveMarker)
	fmt.Fprintf(&builder, "// Codeflux atom documentation (schema v%d):\n", schemaVersion)
	for _, spec := range fieldSpecs {
		field := *spec.get(&doc)
		if field.IsEmpty() {
			return "", invalidField(spec.Label, "is empty")
		}
		fmt.Fprintf(&builder, "//   %s:\n", spec.Label)
		switch {
		case field.None:
			fmt.Fprintf(&builder, "//     None: %s\n", field.Reason)
		case spec.Kind == FieldKindList:
			for _, item := range field.Items {
				fmt.Fprintf(&builder, "//     - %s\n", item)
			}
		default:
			fmt.Fprintf(&builder, "//     %s\n", field.Text)
		}
	}
	return builder.String(), nil
}
