package atomdoc

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// EmbeddingInputSchemaVersion is the only currently supported version of the
// FULL atom embedding-input schema: the schema governing the complete text
// an embedding model would receive for one atom, combining the naming
// lane's canonical-name/normalized-phrase/alias segments
// (atomname.ComposeNameEmbeddingInput) with this package's
// documentation-derived segments (ComposeDocumentationEmbeddingInput) into
// one ordered sequence (M21-127; AGENTS.md "Atom Naming Style": "Include the
// canonical name and normalized phrase exactly once"; docs/plan.md §7 "Atom
// Documentation as Retrieval Material").
//
// It is defined independently from SchemaVersion (this package's 19-field
// atom-documentation schema, above) and from atomname.SchemaVersion (the
// atom-naming schema): all three currently equal 1, but they version
// independently and must never be conflated or reused for one another.
// internal/atomname owns actually assembling the combined input — it already
// imports this package, and this package must not import it back — but the
// version number is defined once, here, as the shared source of truth both
// lanes stamp their contribution with.
const EmbeddingInputSchemaVersion uint32 = 1

// MaximumNormalizedContextFieldBytes bounds one concisely normalized
// retry/reconciliation/security/dependency/limit segment (M21-129): these
// five fields enter embedding input only through this bounded, sentence-level
// normalization, never as a verbatim dump of the full documentation field
// (which may be up to MaximumFieldTextBytes long).
const MaximumNormalizedContextFieldBytes = 300

// EmbeddingInputFieldRole names which schema-v1 documentation concept one
// EmbeddingInputSegment carries, for the (out-of-scope, vector-runtime-
// gated) embedding assembler to weight or label. It mirrors the shape of
// atomname.EmbeddingSegmentRole but enumerates the documentation-lane
// concepts AGENTS.md and docs/plan.md §7 name for embedding input (M21-128,
// M21-129), not the naming-lane ones. Because this type is distinct from
// atomname.EmbeddingSegmentRole, no EmbeddingInputSegment produced by this
// package can ever be mistaken for — or silently collide with — the
// canonical-name/normalized-phrase segments only atomname.
// ComposeNameEmbeddingInput may produce.
type EmbeddingInputFieldRole string

const (
	EmbeddingInputFieldRolePurpose               EmbeddingInputFieldRole = "purpose"
	EmbeddingInputFieldRoleUseWhen               EmbeddingInputFieldRole = "use-when"
	EmbeddingInputFieldRoleDoNotUseWhen          EmbeddingInputFieldRole = "do-not-use-when"
	EmbeddingInputFieldRoleSemantics             EmbeddingInputFieldRole = "semantics"
	EmbeddingInputFieldRoleInputOutputMeaning    EmbeddingInputFieldRole = "input-output-meaning"
	EmbeddingInputFieldRoleEffects               EmbeddingInputFieldRole = "effects"
	EmbeddingInputFieldRoleFailureSemantics      EmbeddingInputFieldRole = "failure-semantics"
	EmbeddingInputFieldRoleRetrievalConcepts     EmbeddingInputFieldRole = "retrieval-concepts"
	EmbeddingInputFieldRoleRetryContext          EmbeddingInputFieldRole = "retry-context"
	EmbeddingInputFieldRoleReconciliationContext EmbeddingInputFieldRole = "reconciliation-context"
	EmbeddingInputFieldRoleSecurityContext       EmbeddingInputFieldRole = "security-context"
	EmbeddingInputFieldRoleDependencyContext     EmbeddingInputFieldRole = "dependency-context"
	EmbeddingInputFieldRoleLimitContext          EmbeddingInputFieldRole = "limit-context"
)

// EmbeddingInputSegment is one piece of text this package contributes to an
// atom's embedding input, mirroring atomname.EmbeddingInputSegment's shape:
// a role the (out-of-scope) assembler can weight or label, the segment's own
// text, and whether that text passed through M21-129's concise semantic
// normalization rather than being included in full.
type EmbeddingInputSegment struct {
	Role       EmbeddingInputFieldRole
	Text       string
	Normalized bool
}

// DocumentationEmbeddingInput is the documentation lane's contribution to
// one atom's embedding input (M21-128..131). It is not the complete
// embedding-input schema: internal/atomname combines this with its own
// NameEmbeddingInput to produce the full atom embedding input AGENTS.md
// describes (M21-127).
type DocumentationEmbeddingInput struct {
	SchemaVersion uint32
	Segments      []EmbeddingInputSegment
}

// ErrCannotComposeEmbeddingInput classifies a Document that cannot
// contribute embedding input.
var ErrCannotComposeEmbeddingInput = errors.New("cannot compose atom-documentation embedding input")

// ComposeDocumentationEmbeddingInput composes the documentation-derived
// embedding-input segments for doc:
//
//   - Purpose, Use when, Do not use when, Semantics, Inputs, Outputs,
//     Effects, Failure semantics, and Retrieval concepts enter at full
//     weight, verbatim (M21-128). Do not use when is never dropped as noise
//     (M21-131): a populated negative-selection field always produces a
//     segment, because it is what keeps a semantically close but invalid
//     atom distinguishable from a valid one at retrieval time.
//   - Idempotency and retry, Reconciliation and compensation, Security and
//     privacy, Dependencies and bindings, and Complexity and limits enter
//     only through concise semantic normalization — never as a verbatim
//     dump (M21-129).
//   - Preconditions, Postconditions, Determinism, Examples, and
//     Verification are process/evidence fields AGENTS.md and docs/plan.md §7
//     do not name as embedding-input material; this function never emits a
//     segment for them.
//   - A field recorded as an explained "None" absence contributes no
//     segment: there is no substantive domain content to embed.
//
// It takes only a Document — never a DocumentationRevision or any other
// value carrying source location, timestamps, evidence-run identity, or a
// content hash — so the M21-130 excluded categories (source paths, line
// numbers, timestamps, evidence run IDs, hashes) cannot leak into the
// result no matter what the caller passes: Document's only fields are the
// nineteen semantic Field values (schema.go), which hold scrubbed domain
// prose and list items and nothing else. This is a structural guarantee,
// not a convention this function must remember to honor. Segment text also
// never carries a field-label prefix (no "Purpose:", no "Effects:"): the
// Role communicates which field a segment came from, so the label string
// itself is never repeated into the embedded text.
func ComposeDocumentationEmbeddingInput(doc Document) (DocumentationEmbeddingInput, error) {
	if doc.Purpose.IsEmpty() {
		return DocumentationEmbeddingInput{}, fmt.Errorf("%w: document is missing Purpose content", ErrCannotComposeEmbeddingInput)
	}

	var segments []EmbeddingInputSegment
	full := func(role EmbeddingInputFieldRole, field Field) {
		if text := fieldEmbeddingText(field); text != "" {
			segments = append(segments, EmbeddingInputSegment{Role: role, Text: text})
		}
	}
	normalized := func(role EmbeddingInputFieldRole, field Field) {
		if text := fieldEmbeddingText(field); text != "" {
			segments = append(segments, EmbeddingInputSegment{Role: role, Text: normalizeContextField(text), Normalized: true})
		}
	}

	// M21-128: default full-weight embedding fields, in schema order.
	full(EmbeddingInputFieldRolePurpose, doc.Purpose)
	full(EmbeddingInputFieldRoleUseWhen, doc.UseWhen)
	full(EmbeddingInputFieldRoleDoNotUseWhen, doc.DoNotUseWhen)
	full(EmbeddingInputFieldRoleSemantics, doc.Semantics)
	full(EmbeddingInputFieldRoleInputOutputMeaning, doc.Inputs)
	full(EmbeddingInputFieldRoleInputOutputMeaning, doc.Outputs)
	full(EmbeddingInputFieldRoleEffects, doc.Effects)
	full(EmbeddingInputFieldRoleFailureSemantics, doc.FailureSemantics)

	// M21-129: retry, reconciliation, security, dependency, and limit
	// fields enter only through concise semantic normalization.
	normalized(EmbeddingInputFieldRoleRetryContext, doc.IdempotencyAndRetry)
	normalized(EmbeddingInputFieldRoleReconciliationContext, doc.ReconciliationAndCompensation)
	normalized(EmbeddingInputFieldRoleSecurityContext, doc.SecurityAndPrivacy)
	normalized(EmbeddingInputFieldRoleDependencyContext, doc.DependenciesAndBindings)
	normalized(EmbeddingInputFieldRoleLimitContext, doc.ComplexityAndLimits)

	full(EmbeddingInputFieldRoleRetrievalConcepts, doc.RetrievalConcepts)

	return DocumentationEmbeddingInput{SchemaVersion: EmbeddingInputSchemaVersion, Segments: segments}, nil
}

// fieldEmbeddingText renders one Field as embedding-input text: a list
// field's items are space-joined (each item is already a substantive
// sentence, not a raw dump); a prose field's text is returned as-is; an
// explained "None" absence has no substantive domain content and renders as
// "" so its caller adds no segment.
func fieldEmbeddingText(field Field) string {
	if field.None {
		return ""
	}
	if len(field.Items) > 0 {
		return strings.Join(field.Items, " ")
	}
	return field.Text
}

// normalizeContextField reduces a full documentation field to a concise
// semantic normalization for embedding input (M21-129): collapse
// insignificant whitespace, keep only the field's own topic sentence (its
// first complete sentence), and hard-cap the result at
// MaximumNormalizedContextFieldBytes — well below MaximumFieldTextBytes —
// so verbose retry/reconciliation/security/dependency/limit prose can never
// enter embedding input as a verbatim dump.
func normalizeContextField(text string) string {
	collapsed := collapseSpaces(text)
	if collapsed == "" {
		return ""
	}
	sentence := collapsed
	if idx := strings.IndexAny(collapsed, ".!?"); idx >= 0 {
		sentence = collapsed[:idx+1]
	}
	return truncateRuneSafe(sentence, MaximumNormalizedContextFieldBytes)
}

// truncateRuneSafe truncates s to at most maxBytes bytes without splitting a
// multi-byte UTF-8 rune. It trims back byte-by-byte until the result is
// valid UTF-8 rather than only checking whether the final byte could start a
// rune: a lead byte of a multi-byte sequence is itself a valid rune-start
// byte, so a naive "is this byte a rune start" check alone still admits a
// truncation that cuts a multi-byte rune in half after its first byte.
func truncateRuneSafe(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	truncated := s[:maxBytes]
	for len(truncated) > 0 && !utf8.ValidString(truncated) {
		truncated = truncated[:len(truncated)-1]
	}
	return truncated
}
