package atomdoc

import "strings"

// SchemaVersion is the only currently supported atom-documentation schema
// version (AGENTS.md "Atom Documentation Style", schema v1). M21-100.
const SchemaVersion uint32 = 1

// AtomDirectiveMarker is the exact directive that admits a Go declaration as
// a Codeflux atom. A doc comment without this marker is ordinary Go.
const AtomDirectiveMarker = "codeflux:atom"

// atomNameExceptionPrefix marks a neighboring naming-exception directive.
// This package tolerates and skips these lines during field extraction; it
// does not validate atom-naming exceptions, which belong to the separate
// atom-naming milestone tasks.
const atomNameExceptionPrefix = "codeflux:atom-name-exception "

// Explicit bounds on one parsed atom-documentation comment. Codeflux never
// parses, hashes, or scrubs an unbounded amount of comment text.
const (
	MaximumSourceCommentBytes = 64 * 1024
	MaximumFieldTextBytes     = 8192
	MaximumListItems          = 128
	MaximumListItemBytes      = 2048
	MaximumDependencyBindings = 64
	MaximumFieldCount         = 19
)

// FieldKind distinguishes a substantive-prose field from an ordered-list
// field in the schema-v1 documentation shape.
type FieldKind int

const (
	// FieldKindProse holds one normalized paragraph of substantive text.
	FieldKindProse FieldKind = iota
	// FieldKindList holds an ordered sequence of substantive list items;
	// M21-111 requires this structure to survive parsing intact.
	FieldKindList
)

// Field is one schema-v1 documentation field. Exactly one of three shapes
// applies: an explained "None" absence, prose Text, or list Items. AGENTS.md
// permits "None" only together with a non-empty Reason; it never permits a
// silently empty field.
type Field struct {
	None   bool
	Reason string
	Text   string
	Items  []string
}

// IsEmpty reports whether the field carries no content and no None reason,
// which is never a valid parsed or generated state.
func (field Field) IsEmpty() bool {
	return !field.None && field.Text == "" && len(field.Items) == 0
}

// Document is the schema-v1 structured atom-documentation body: the exact
// nineteen fields named in AGENTS.md, in the exact declared order. Field
// names mirror the AGENTS.md labels one-for-one; no field was invented,
// renamed, reordered, or dropped.
type Document struct {
	Purpose                       Field
	UseWhen                       Field
	DoNotUseWhen                  Field
	Semantics                     Field
	Inputs                        Field
	Outputs                       Field
	Preconditions                 Field
	Postconditions                Field
	Effects                       Field
	FailureSemantics              Field
	Determinism                   Field
	IdempotencyAndRetry           Field
	ReconciliationAndCompensation Field
	SecurityAndPrivacy            Field
	DependenciesAndBindings       Field
	ComplexityAndLimits           Field
	Examples                      Field
	Verification                  Field
	RetrievalConcepts             Field
}

// fieldSpec binds one canonical AGENTS.md field label and its structural
// kind to a typed accessor into a Document, so parsing, validation, hashing,
// and generation can iterate the schema in one declared canonical order
// without reflection.
type fieldSpec struct {
	Label string
	Kind  FieldKind
	get   func(*Document) *Field
}

// fieldSpecs is the canonical schema-v1 field order. This exact order is
// both the required source-comment order (M21-114) and the canonical-hash
// and generated-comment order (M21-121, M21-124).
var fieldSpecs = []fieldSpec{
	{"Purpose", FieldKindProse, func(d *Document) *Field { return &d.Purpose }},
	{"Use when", FieldKindProse, func(d *Document) *Field { return &d.UseWhen }},
	{"Do not use when", FieldKindProse, func(d *Document) *Field { return &d.DoNotUseWhen }},
	{"Semantics", FieldKindProse, func(d *Document) *Field { return &d.Semantics }},
	{"Inputs", FieldKindList, func(d *Document) *Field { return &d.Inputs }},
	{"Outputs", FieldKindList, func(d *Document) *Field { return &d.Outputs }},
	{"Preconditions", FieldKindList, func(d *Document) *Field { return &d.Preconditions }},
	{"Postconditions", FieldKindList, func(d *Document) *Field { return &d.Postconditions }},
	{"Effects", FieldKindList, func(d *Document) *Field { return &d.Effects }},
	{"Failure semantics", FieldKindList, func(d *Document) *Field { return &d.FailureSemantics }},
	{"Determinism", FieldKindProse, func(d *Document) *Field { return &d.Determinism }},
	{"Idempotency and retry", FieldKindProse, func(d *Document) *Field { return &d.IdempotencyAndRetry }},
	{"Reconciliation and compensation", FieldKindProse, func(d *Document) *Field { return &d.ReconciliationAndCompensation }},
	{"Security and privacy", FieldKindProse, func(d *Document) *Field { return &d.SecurityAndPrivacy }},
	{"Dependencies and bindings", FieldKindProse, func(d *Document) *Field { return &d.DependenciesAndBindings }},
	{"Complexity and limits", FieldKindProse, func(d *Document) *Field { return &d.ComplexityAndLimits }},
	{"Examples", FieldKindList, func(d *Document) *Field { return &d.Examples }},
	{"Verification", FieldKindProse, func(d *Document) *Field { return &d.Verification }},
	{"Retrieval concepts", FieldKindProse, func(d *Document) *Field { return &d.RetrievalConcepts }},
}

func init() {
	if len(fieldSpecs) != MaximumFieldCount {
		panic("atomdoc: fieldSpecs must declare exactly the schema-v1 field count")
	}
}

// CanonicalFieldLabels returns the exact schema-v1 field labels in canonical
// order, for callers (such as the embedding-input lane) that need the
// declared field set without depending on this package's internal layout.
func CanonicalFieldLabels() []string {
	labels := make([]string, len(fieldSpecs))
	for index, spec := range fieldSpecs {
		labels[index] = spec.Label
	}
	return labels
}

// collapseSpaces normalizes insignificant whitespace: it collapses any run
// of whitespace (including newlines introduced by wrapped source lines) to a
// single space and trims the result, while leaving every other character —
// words, punctuation, and quoted negative examples — untouched (M21-112,
// M21-113).
func collapseSpaces(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
