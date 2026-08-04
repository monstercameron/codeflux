package coordinator

import (
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/atomdoc"
)

// TestTheAtomTemplateShowsListFieldsAsLists is the instruction that produced
// documentation its own validator refused.
//
// Eight of the nineteen schema-v1 fields are lists and need "- " items. Every
// label used to be followed by the same indented placeholder, which is right
// for prose and wrong for those eight: a list field with a sentence under it
// parses to text and no items, and validation reports it as empty.
//
// So a run wrote exactly what it was shown and had its atoms refused for
// `field "Inputs" is empty`, with Inputs written. Ladder rung 18 on 2026-08-04
// lost all three of its atoms that way, and the registry has been empty for
// every run of this session — which is the compounding-effort thesis unstarted
// rather than unproven, since no run can reuse what no run registered.
func TestTheAtomTemplateShowsListFieldsAsLists(t *testing.T) {
	instruction := atomDocumentationInstruction([]string{"fp/result.go:Ok"})

	lists := 0
	for _, field := range atomdoc.CanonicalFieldTemplate() {
		if !field.List {
			continue
		}
		lists++
		want := "//   " + field.Label + ":\n//     - "
		if !strings.Contains(instruction, want) {
			t.Errorf("%s is a list field and the template shows it as prose, "+
				"so a run that copies the template has it read as empty",
				field.Label)
		}
	}
	if lists == 0 {
		t.Fatal("the schema declares no list fields, so this asserts nothing")
	}
	// And the difference is stated, not only shown.
	if !strings.Contains(instruction, "read as empty") {
		t.Errorf("nothing says what happens to a list field filled in with a "+
			"sentence:\n%s", instruction)
	}
}

// TestTheAtomTemplateStillShowsProseFieldsAsProse is the control.
//
// Turning every field into a list would be the same defect mirrored: a prose
// field given "- " items is not what the schema asks for either.
func TestTheAtomTemplateStillShowsProseFieldsAsProse(t *testing.T) {
	instruction := atomDocumentationInstruction([]string{"fp/result.go:Ok"})

	prose := 0
	for _, field := range atomdoc.CanonicalFieldTemplate() {
		if field.List {
			continue
		}
		prose++
		want := "//   " + field.Label + ":\n//     …\n"
		if !strings.Contains(instruction, want) {
			t.Errorf("%s is a prose field and the template no longer shows it "+
				"as one", field.Label)
		}
	}
	if prose == 0 {
		t.Fatal("the schema declares no prose fields, so this asserts nothing")
	}
}

// TestEveryFieldIsStillNamed keeps the template complete.
//
// The schema requires every field. A template that dropped one would send a run
// to write documentation that is refused for the field nobody mentioned.
func TestEveryFieldIsStillNamed(t *testing.T) {
	instruction := atomDocumentationInstruction([]string{"fp/result.go:Ok"})
	for _, label := range atomdoc.CanonicalFieldLabels() {
		if !strings.Contains(instruction, "//   "+label+":") {
			t.Errorf("the template never names %q", label)
		}
	}
}
