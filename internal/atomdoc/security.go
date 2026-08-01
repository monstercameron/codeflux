package atomdoc

import (
	"fmt"

	"codeflux.dev/codeflux/internal/redact"
)

// DefaultRedactionBoundary is the redaction boundary applied before atom
// documentation is admitted. Admitted documentation is retrieval material
// that Codeflux later inserts into provider prompts and persists, so it is
// scrubbed at the same prompt-persistence boundary as other content bound
// for that path (AGENTS.md "Redact secrets before persistence ... prompts").
const DefaultRedactionBoundary = redact.BoundaryPromptPersistence

// ScrubAtomDocumentationForPersistence runs every field of doc through the
// shared internal/redact.Pipeline used elsewhere before persistence
// (M21-118); it does not implement or duplicate a scrubber of its own. It
// returns the scrubbed document and the combined redaction report across
// every field.
func ScrubAtomDocumentationForPersistence(pipeline *redact.Pipeline, doc Document) (Document, redact.Report, error) {
	if pipeline == nil {
		return Document{}, redact.Report{}, fmt.Errorf("%w: redaction pipeline is required", ErrInvalidAtomDocumentation)
	}
	report := redact.Report{Boundary: DefaultRedactionBoundary}
	var scrubbed Document
	for _, spec := range fieldSpecs {
		field := *spec.get(&doc)
		if field.None {
			*spec.get(&scrubbed) = field
			continue
		}
		if spec.Kind == FieldKindList {
			items := make([]string, len(field.Items))
			for index, item := range field.Items {
				result, err := pipeline.Redact(DefaultRedactionBoundary, item)
				if err != nil {
					return Document{}, redact.Report{}, err
				}
				items[index] = result.Text
				mergeRedactionReport(&report, result.Report)
			}
			*spec.get(&scrubbed) = Field{Items: items}
			continue
		}
		result, err := pipeline.Redact(DefaultRedactionBoundary, field.Text)
		if err != nil {
			return Document{}, redact.Report{}, err
		}
		*spec.get(&scrubbed) = Field{Text: result.Text}
		mergeRedactionReport(&report, result.Report)
	}
	return scrubbed, report, nil
}

func mergeRedactionReport(total *redact.Report, part redact.Report) {
	total.Redactions += part.Redactions
	total.InputTruncated = total.InputTruncated || part.InputTruncated
	total.OutputTruncated = total.OutputTruncated || part.OutputTruncated
	total.EntropyEnabled = total.EntropyEnabled || part.EntropyEnabled
}

// ContainsSensitiveContent reports whether the redaction report observed any
// exact-credential or credential-shaped pattern match. Atom documentation
// carrying known credentials, private keys, or prohibited sensitive fixtures
// must be rejected outright rather than merely scrubbed and admitted
// (M21-119): a nonzero redaction count means the source comment is
// inadmissible, not merely in need of cleanup.
func ContainsSensitiveContent(report redact.Report) bool {
	return report.Redactions > 0
}
