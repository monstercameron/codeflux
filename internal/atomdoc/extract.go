package atomdoc

import (
	"fmt"
	"go/ast"
	"go/token"
	"regexp"
	"strconv"
	"strings"
)

// SourceCandidate is one Go declaration whose doc-comment group carries the
// //codeflux:atom directive, located but not yet parsed or validated
// (M21-108, M21-109).
type SourceCandidate struct {
	Identifier string
	DocGroup   *ast.CommentGroup
	Position   token.Position
}

// LocateAtomDeclarationCandidates finds every declaration in file whose
// doc-comment group carries the //codeflux:atom directive, using the Go
// syntax tree (go/ast, go/parser, go/token) rather than regular-expression
// scanning of raw source text (M21-108). A func declaration or a named-type
// declaration may carry the directive; every other declaration kind cannot.
//
// It also locates the doc group attached to the declared identifier itself
// rather than any nearby comment (M21-109): a directive present in a comment
// group that is not attached to a supported declaration is reported as an
// error, because it cannot bind to an atom identifier.
func LocateAtomDeclarationCandidates(fset *token.FileSet, file *ast.File) ([]SourceCandidate, error) {
	if file == nil {
		return nil, fmt.Errorf("%w: source file is nil", ErrInvalidAtomDocumentation)
	}
	var candidates []SourceCandidate
	consumed := make(map[*ast.CommentGroup]bool)
	for _, declaration := range file.Decls {
		switch typed := declaration.(type) {
		case *ast.FuncDecl:
			if hasAtomDirective(typed.Doc) {
				consumed[typed.Doc] = true
				candidates = append(candidates, SourceCandidate{
					Identifier: typed.Name.Name,
					DocGroup:   typed.Doc,
					Position:   fset.Position(typed.Pos()),
				})
			}
		case *ast.GenDecl:
			for _, specification := range typed.Specs {
				typeSpec, ok := specification.(*ast.TypeSpec)
				if !ok {
					continue
				}
				doc := typeSpec.Doc
				if doc == nil {
					doc = typed.Doc
				}
				if hasAtomDirective(doc) {
					consumed[doc] = true
					candidates = append(candidates, SourceCandidate{
						Identifier: typeSpec.Name.Name,
						DocGroup:   doc,
						Position:   fset.Position(typeSpec.Pos()),
					})
				}
			}
		}
	}
	for _, group := range file.Comments {
		if hasAtomDirective(group) && !consumed[group] {
			return nil, fmt.Errorf("%w: %s:%s directive is not attached to a func or named-type declaration",
				ErrInvalidAtomDocumentation, fset.Position(group.Pos()), AtomDirectiveMarker)
		}
	}
	return candidates, nil
}

func hasAtomDirective(group *ast.CommentGroup) bool {
	for _, line := range commentGroupLines(group) {
		if strings.TrimSpace(line) == AtomDirectiveMarker {
			return true
		}
	}
	return false
}

// commentGroupLines returns each comment line with its leading "//" or block
// "/* */" marker removed, preserving line order and original interior
// content so downstream normalization decides what whitespace is
// insignificant.
func commentGroupLines(group *ast.CommentGroup) []string {
	if group == nil {
		return nil
	}
	var lines []string
	for _, comment := range group.List {
		text := comment.Text
		switch {
		case strings.HasPrefix(text, "//"):
			lines = append(lines, strings.TrimPrefix(text, "//"))
		case strings.HasPrefix(text, "/*"):
			trimmed := strings.TrimSuffix(strings.TrimPrefix(text, "/*"), "*/")
			for _, line := range strings.Split(trimmed, "\n") {
				lines = append(lines, strings.TrimPrefix(strings.TrimSpace(line), "*"))
			}
		}
	}
	return lines
}

// rawCommentText renders the doc-comment group back into one exact string,
// used only to compute the pre-normalization SourceCommentHash (M21-120).
func rawCommentText(group *ast.CommentGroup) string {
	var builder strings.Builder
	for index, comment := range group.List {
		if index > 0 {
			builder.WriteByte('\n')
		}
		builder.WriteString(comment.Text)
	}
	return builder.String()
}

// RawField is one field header and its raw body lines exactly as they
// appeared in the source comment, before whitespace normalization or list
// splitting.
type RawField struct {
	Label     string
	BodyLines []string
}

// ParsedComment is the schema-header-validated, field-structured content of
// one atom source comment, before schema-policy validation or scrubbing.
type ParsedComment struct {
	Identifier      string
	OpeningSentence string
	SchemaVersion   uint32
	Fields          []RawField
	RawText         string
}

var schemaHeaderPattern = regexp.MustCompile(`^Codeflux atom documentation \(schema v([0-9]+)\):$`)

// fieldHeaderCandidatePattern recognizes a line shaped like a schema-v1 field
// header: one to five Title-Case-leaning words with no other punctuation,
// terminated by a single colon and nothing else. Known field labels are
// resolved against this candidate set later so that an unrecognized but
// header-shaped line is rejected as an unknown field (M21-114) instead of
// being silently folded into the previous field's content.
var fieldHeaderCandidatePattern = regexp.MustCompile(`^[A-Za-z]+( [A-Za-z]+){0,4}:$`)

// fieldHeaderIndent is the exact leading whitespace GenerateAtomDocumentationComment
// and the AGENTS.md "Atom Documentation Style" example both use for a
// schema-v1 field header line ("//   Label:"), measured on the comment text
// after its leading "//" marker has been stripped. Field body and list-item
// lines are always indented at least one space deeper ("//     text",
// "//     - item"). M21-108 requires field-header discovery to be structural
// rather than a positional regex guess applied uniformly across the whole
// comment body; anchoring header detection to this exact structural depth is
// what lets a colon-terminated, header-shaped line written as prose or a
// natural sub-heading INSIDE a field's body (for example "Phase One:" inside
// a long Semantics field, both indented at ordinary body depth) be treated
// as content instead of being misparsed as a bogus new top-level field.
const fieldHeaderIndent = "   "

// isFieldHeaderLine reports whether raw (the original, not-yet-trimmed
// comment line) is a genuine schema-v1 field header: indented at exactly
// fieldHeaderIndent depth, with header-shaped content per
// fieldHeaderCandidatePattern. A line indented deeper than fieldHeaderIndent
// is always body content, regardless of its shape, even if it happens to
// read like "Word Word:" on its own line.
func isFieldHeaderLine(raw, trimmed string) bool {
	if !strings.HasPrefix(raw, fieldHeaderIndent) {
		return false
	}
	rest := raw[len(fieldHeaderIndent):]
	if rest == "" || rest[0] == ' ' || rest[0] == '\t' {
		return false
	}
	return fieldHeaderCandidatePattern.MatchString(trimmed)
}

// ParseAtomDocumentationComment parses one located atom doc comment: it
// validates the schema-version header (M21-110), splits the body into
// ordered fields without losing list structure (M21-111), and locates the Go
// doc-comment opening sentence for later validation. It does not yet enforce
// field completeness, order, or content policy; ValidateAtomDocumentationSchema
// does that.
func ParseAtomDocumentationComment(candidate SourceCandidate) (ParsedComment, error) {
	raw := rawCommentText(candidate.DocGroup)
	if len(raw) > MaximumSourceCommentBytes {
		return ParsedComment{}, fmt.Errorf("%w: source comment exceeds %d bytes", ErrInvalidAtomDocumentation, MaximumSourceCommentBytes)
	}
	lines := commentGroupLines(candidate.DocGroup)

	opening := firstDescriptiveLine(lines)
	if opening == "" {
		return ParsedComment{}, fmt.Errorf("%w: atom %s has no descriptive opening sentence", ErrInvalidAtomDocumentation, candidate.Identifier)
	}

	headerIndex, version, err := locateSchemaHeader(lines)
	if err != nil {
		return ParsedComment{}, fmt.Errorf("atom %s: %w", candidate.Identifier, err)
	}

	fields := parseRawFields(lines[headerIndex+1:])
	return ParsedComment{
		Identifier:      candidate.Identifier,
		OpeningSentence: opening,
		SchemaVersion:   version,
		Fields:          fields,
		RawText:         raw,
	}, nil
}

// firstDescriptiveLine returns the Go doc-comment opening sentence: the
// first non-empty, non-directive line through the end of that paragraph
// (the run of non-empty lines up to the next blank line, directive marker,
// or naming-exception directive), joined and whitespace-normalized. The
// opening sentence commonly wraps across more than one raw comment line, so
// a single-line read is not sufficient to see its terminal punctuation.
func firstDescriptiveLine(lines []string) string {
	start := -1
	for index, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || line == AtomDirectiveMarker || strings.HasPrefix(line, atomNameExceptionPrefix) {
			continue
		}
		start = index
		break
	}
	if start < 0 {
		return ""
	}
	var parts []string
	for _, raw := range lines[start:] {
		line := strings.TrimSpace(raw)
		if line == "" || line == AtomDirectiveMarker || strings.HasPrefix(line, atomNameExceptionPrefix) {
			break
		}
		parts = append(parts, line)
	}
	return collapseSpaces(strings.Join(parts, " "))
}

func locateSchemaHeader(lines []string) (int, uint32, error) {
	for index, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if matches := schemaHeaderPattern.FindStringSubmatch(line); matches != nil {
			version, err := strconv.ParseUint(matches[1], 10, 32)
			if err != nil {
				return 0, 0, fmt.Errorf("%w: malformed schema-version number", ErrInvalidAtomDocumentation)
			}
			if uint32(version) != SchemaVersion {
				return 0, 0, fmt.Errorf("%w: unsupported documentation schema version %d (expected %d)",
					ErrInvalidAtomDocumentation, version, SchemaVersion)
			}
			return index, uint32(version), nil
		}
		if strings.Contains(line, "Codeflux atom documentation") {
			return 0, 0, fmt.Errorf("%w: malformed schema-version header %q", ErrInvalidAtomDocumentation, line)
		}
	}
	return 0, 0, fmt.Errorf("%w: missing schema-versioned Codeflux atom documentation header", ErrInvalidAtomDocumentation)
}

// parseRawFields groups the comment lines following the schema header into
// ordered RawField entries, preserving the exact source order (needed for
// the M21-114 out-of-order check) and every list-item boundary marked by a
// leading "-" (M21-111).
func parseRawFields(lines []string) []RawField {
	var fields []RawField
	currentIndex := -1
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, atomNameExceptionPrefix) {
			continue
		}
		if isFieldHeaderLine(raw, line) {
			fields = append(fields, RawField{Label: strings.TrimSuffix(line, ":")})
			currentIndex = len(fields) - 1
			continue
		}
		if currentIndex < 0 {
			continue
		}
		fields[currentIndex].BodyLines = append(fields[currentIndex].BodyLines, line)
	}
	return fields
}

// normalizeField converts one RawField's body lines into a Field, collapsing
// insignificant whitespace while preserving punctuation, domain terms, and
// negative examples (M21-112, M21-113), and preserving list-item boundaries
// for list-kind fields (M21-111). A body whose full normalized text begins
// with "None:" is treated as an explained absence regardless of field kind.
func normalizeField(kind FieldKind, bodyLines []string) Field {
	full := collapseSpaces(strings.Join(bodyLines, " "))
	if full == "" {
		return Field{}
	}
	if rest, ok := strings.CutPrefix(full, "None:"); ok {
		return Field{None: true, Reason: strings.TrimSpace(rest)}
	}
	if kind == FieldKindList {
		return Field{Items: splitListItems(bodyLines)}
	}
	return Field{Text: full}
}

func splitListItems(bodyLines []string) []string {
	var items []string
	for _, raw := range bodyLines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if rest, ok := strings.CutPrefix(line, "-"); ok {
			items = append(items, strings.TrimSpace(rest))
			continue
		}
		if len(items) == 0 {
			continue
		}
		items[len(items)-1] = collapseSpaces(items[len(items)-1] + " " + line)
	}
	for index, item := range items {
		items[index] = collapseSpaces(item)
	}
	return items
}
