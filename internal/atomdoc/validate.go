package atomdoc

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// ErrInvalidAtomDocumentation classifies a rejected atom-documentation
// comment: malformed schema header, missing/duplicate/unknown/out-of-order
// fields, empty or unexplained-None field content, or an invalid opening
// sentence.
var ErrInvalidAtomDocumentation = errors.New("invalid atom documentation")

var wordPattern = regexp.MustCompile(`[A-Za-z][A-Za-z0-9_-]*`)

func invalidField(field, reason string) error {
	return fmt.Errorf("%w: field %q %s", ErrInvalidAtomDocumentation, field, reason)
}

// ValidateAtomDocumentationSchema enforces the schema-v1 field policy:
// exactly the canonical fields, each exactly once, in exactly the canonical
// AGENTS.md order, each with substantive content or an explained "None"
// absence (M21-114, M21-115).
func ValidateAtomDocumentationSchema(fields []RawField) (Document, error) {
	specByLabel := make(map[string]fieldSpec, len(fieldSpecs))
	for _, spec := range fieldSpecs {
		specByLabel[spec.Label] = spec
	}

	seen := make(map[string]int, len(fields))
	for _, raw := range fields {
		seen[raw.Label]++
	}
	for label, count := range seen {
		if _, known := specByLabel[label]; !known {
			return Document{}, fmt.Errorf("%w: unknown field %q", ErrInvalidAtomDocumentation, label)
		}
		if count > 1 {
			return Document{}, fmt.Errorf("%w: field %q is duplicated", ErrInvalidAtomDocumentation, label)
		}
	}
	for _, spec := range fieldSpecs {
		if seen[spec.Label] == 0 {
			return Document{}, fmt.Errorf("%w: missing required field %q", ErrInvalidAtomDocumentation, spec.Label)
		}
	}
	if len(fields) != len(fieldSpecs) {
		return Document{}, fmt.Errorf("%w: expected exactly %d fields, found %d", ErrInvalidAtomDocumentation, len(fieldSpecs), len(fields))
	}
	for index, spec := range fieldSpecs {
		if fields[index].Label != spec.Label {
			return Document{}, fmt.Errorf("%w: field %q is out of order (expected %q at this position)",
				ErrInvalidAtomDocumentation, fields[index].Label, spec.Label)
		}
	}

	var doc Document
	for index, spec := range fieldSpecs {
		normalized := normalizeField(spec.Kind, fields[index].BodyLines)
		if err := validateFieldContent(spec, normalized); err != nil {
			return Document{}, err
		}
		*spec.get(&doc) = normalized
	}
	return doc, nil
}

func validateFieldContent(spec fieldSpec, field Field) error {
	if field.None {
		if b, bad := firstDisallowedControlByte(field.Reason); bad {
			return invalidField(spec.Label, fmt.Sprintf("None reason contains a disallowed control byte 0x%02x", b))
		}
		if len(wordPattern.FindAllString(field.Reason, -1)) < 2 {
			return invalidField(spec.Label, "uses None without a substantive reason")
		}
		return nil
	}
	switch spec.Kind {
	case FieldKindList:
		if len(field.Items) == 0 {
			return invalidField(spec.Label, "is empty")
		}
		if len(field.Items) > MaximumListItems {
			return invalidField(spec.Label, "exceeds the maximum list-item bound")
		}
		for _, item := range field.Items {
			if strings.TrimSpace(item) == "" {
				return invalidField(spec.Label, "contains an empty list item")
			}
			if b, bad := firstDisallowedControlByte(item); bad {
				return invalidField(spec.Label, fmt.Sprintf("contains a list item with a disallowed control byte 0x%02x", b))
			}
			if len(item) > MaximumListItemBytes {
				return invalidField(spec.Label, "contains a list item exceeding the maximum length")
			}
			if len(wordPattern.FindAllString(item, -1)) < 3 {
				return invalidField(spec.Label, "contains a list item that is not substantive")
			}
		}
	default:
		if strings.TrimSpace(field.Text) == "" {
			return invalidField(spec.Label, "is empty")
		}
		if b, bad := firstDisallowedControlByte(field.Text); bad {
			return invalidField(spec.Label, fmt.Sprintf("contains a disallowed control byte 0x%02x", b))
		}
		if len(field.Text) > MaximumFieldTextBytes {
			return invalidField(spec.Label, "exceeds the maximum length")
		}
		if len(wordPattern.FindAllString(field.Text, -1)) < 3 {
			return invalidField(spec.Label, "is not substantive")
		}
	}
	return nil
}

// firstDisallowedControlByte reports the first C0 control byte (0x00-0x1F)
// or DEL (0x7F) found in s, other than tab (0x09) or newline (0x0A). Every
// legitimate normalized field value has already had its interior whitespace
// (including any raw tab or newline) collapsed to an ordinary space by
// normalizeField/collapseSpaces before this check runs, so the tab/newline
// carve-out is defensive rather than load-bearing.
//
// This check exists specifically to close a normalized-hash collision: the
// canonical hash input built by canonicalizeDocument (hash.go) joins list
// items with a literal 0x1E byte and separates field labels/values with
// 0x1D/0x1F, and canonicalizeDocument performs no escaping of those bytes if
// they appear inside field content. Without this rejection, a crafted list
// item containing a raw 0x1E byte can splice two items' text into one item
// and produce byte-identical canonical hash input — and therefore an
// identical NormalizedInputHash and DocumentationRevisionID (M21-102,
// M21-121) — to an honest document whose two phrases are two separate list
// items. Rejecting the full C0 range plus DEL closes the entire class rather
// than only the three separator bytes this package happens to use today.
func firstDisallowedControlByte(s string) (byte, bool) {
	for i := 0; i < len(s); i++ {
		b := s[i]
		if b == '\t' || b == '\n' {
			continue
		}
		if b < 0x20 || b == 0x7f {
			return b, true
		}
	}
	return 0, false
}

// ValidateAtomDocumentationOpeningSentence enforces the Go doc-comment
// convention that the opening sentence begins with the declared identifier
// and forms a complete sentence (M21-116).
func ValidateAtomDocumentationOpeningSentence(identifier, opening string) error {
	if identifier == "" {
		return fmt.Errorf("%w: declaration identifier is empty", ErrInvalidAtomDocumentation)
	}
	rest, ok := strings.CutPrefix(opening, identifier+" ")
	if !ok {
		return fmt.Errorf("%w: opening sentence must begin with %q", ErrInvalidAtomDocumentation, identifier)
	}
	if !strings.ContainsAny(rest, ".!?") {
		return fmt.Errorf("%w: opening sentence must be a complete sentence ending in '.', '!', or '?'", ErrInvalidAtomDocumentation)
	}
	return nil
}

// QualityFlagKind classifies one non-fatal atom-documentation quality issue.
type QualityFlagKind string

const (
	// QualityFlagKeywordStuffing marks a field whose word distribution is
	// dominated by one repeated word, suggesting retrieval manipulation
	// rather than substantive description.
	QualityFlagKeywordStuffing QualityFlagKind = "keyword-stuffing"
	// QualityFlagRepeatedBoilerplate marks near-identical content copied
	// across multiple fields instead of describing each field distinctly.
	QualityFlagRepeatedBoilerplate QualityFlagKind = "repeated-boilerplate"
)

// QualityFlag is one non-fatal review flag attached to an otherwise valid
// Document (M21-117). A flag never removes or silently drops field content;
// it marks content that a human should review before the field is trusted
// as clean retrieval material.
type QualityFlag struct {
	Field  string
	Kind   QualityFlagKind
	Detail string
}

// FlagAtomDocumentationQualityIssues flags fields likely to be
// keyword-stuffed or duplicated as boilerplate across fields, for human
// review (M21-117). It does not reject or modify doc; the caller must keep
// flagged content in the admitted document rather than silently embedding it
// as if reviewed or silently dropping it.
func FlagAtomDocumentationQualityIssues(doc Document) []QualityFlag {
	var flags []QualityFlag
	contentByNormalizedText := make(map[string][]string)

	for _, spec := range fieldSpecs {
		field := spec.get(&doc)
		if field.None {
			continue
		}
		var pieces []string
		if spec.Kind == FieldKindList {
			pieces = field.Items
		} else if field.Text != "" {
			pieces = []string{field.Text}
		}
		for _, text := range pieces {
			if detail := detectKeywordStuffing(text); detail != "" {
				flags = append(flags, QualityFlag{Field: spec.Label, Kind: QualityFlagKeywordStuffing, Detail: detail})
			}
			normalized := strings.ToLower(collapseSpaces(text))
			if len(wordPattern.FindAllString(normalized, -1)) >= 6 {
				contentByNormalizedText[normalized] = append(contentByNormalizedText[normalized], spec.Label)
			}
		}
	}

	for _, labels := range contentByNormalizedText {
		if len(labels) < 2 {
			continue
		}
		sorted := append([]string(nil), labels...)
		sort.Strings(sorted)
		flags = append(flags, QualityFlag{
			Field:  strings.Join(sorted, ", "),
			Kind:   QualityFlagRepeatedBoilerplate,
			Detail: fmt.Sprintf("identical content repeated across %d fields", len(sorted)),
		})
	}

	sort.Slice(flags, func(i, j int) bool {
		if flags[i].Field != flags[j].Field {
			return flags[i].Field < flags[j].Field
		}
		return flags[i].Kind < flags[j].Kind
	})
	return flags
}

func detectKeywordStuffing(text string) string {
	words := wordPattern.FindAllString(strings.ToLower(text), -1)
	if len(words) < 6 {
		return ""
	}
	counts := make(map[string]int, len(words))
	maxCount := 0
	maxWord := ""
	for _, word := range words {
		counts[word]++
		if counts[word] > maxCount {
			maxCount = counts[word]
			maxWord = word
		}
	}
	if maxCount*2 > len(words) {
		return fmt.Sprintf("word %q repeated %d of %d words", maxWord, maxCount, len(words))
	}
	return ""
}
