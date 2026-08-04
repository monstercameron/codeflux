package coordinator

import (
	"fmt"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"

	"codeflux.dev/codeflux/internal/atomdoc"
)

// atomsWithoutRegistrableDocumentation names the produced atoms that carry no
// //codeflux:atom schema comment.
//
// Registration skips a declaration without one, so with nothing asking for it
// the registry never filled: every run of this session recorded
// "0 of N produced atom(s) carried schema-v1 documentation eligible for
// registration", atom_names held zero rows, and the recall stage searched an
// empty project every time and reported that it had nothing to reuse.
//
// That is the compounding-effort thesis not merely unproven but unstarted. A
// run cannot reuse what no run ever registered, and no run registered because
// no run was told the schema exists — the completeness gate asks for "a
// comment starting with its name", which is an ordinary Go doc comment and
// not this.
//
// Only atoms are named. A composing function is not registered, and main is
// not an atom at all, so asking either for nineteen fields of documentation
// would spend a run's attempts on something registration would refuse anyway.
func atomsWithoutRegistrableDocumentation(
	worktree string, functions []producedFunction,
) []string {
	documented := map[string]bool{}
	seenFile := map[string]bool{}
	for _, function := range functions {
		if function.File == "" || seenFile[function.File] {
			continue
		}
		seenFile[function.File] = true
		fileSet := token.NewFileSet()
		tree, err := parser.ParseFile(
			fileSet, filepath.Join(worktree, function.File), nil, parser.ParseComments)
		if err != nil {
			continue
		}
		candidates, err := atomdoc.LocateAtomDeclarationCandidates(fileSet, tree)
		if err != nil {
			continue
		}
		for _, candidate := range candidates {
			documented[candidate.Identifier] = true
		}
	}

	var missing []string
	for _, function := range functions {
		if isTestScaffolding(function) || function.Name == "main" {
			continue
		}
		// An atom is a function that composes nothing: it is a leaf, and
		// leaves are what a later run reuses. A composing function is
		// registered by the molecule stage on its own terms.
		if len(function.Calls) > 0 || documented[function.Name] {
			continue
		}
		if !worthAdmitting(function) {
			continue
		}
		missing = append(missing, function.Name)
	}
	sort.Strings(missing)
	return missing
}

// worthAdmitting decides whether a function is worth nineteen fields of
// documentation and a permanent row in the project's registry.
//
// Being an atom is not the same as being worth remembering. Every leaf function
// was asked for the full schema, which put printFizzBuzz — a task-local
// procedure that prints and returns nothing — in the same queue as a CLI
// argument parser, and ladder rung 2 spent its attempts oscillating between
// that ask and the completeness gate on the lowest rung of the model ladder.
//
// The questions worth asking are whether the behaviour can recur, whether it
// can be stated precisely enough for a later run to match against, and whether
// reusing it would save anything:
//
//   - A function that returns nothing produces no value a caller could want.
//     Its whole contribution is its effect, and an effect is reproduced by
//     doing it, not by calling this.
//   - A function that takes nothing has no applicability to state. There is no
//     input shape a later task could match against.
//   - A function that reaches outside its arguments carries the environment it
//     reached into, and that environment is not part of what gets reused.
//
// Deliberately conservative in one direction: refusing to register something
// reusable costs a later run the work of rebuilding it, and registering
// something task-local costs every later run the context of reading past it,
// plus the attempts spent writing it. The second is charged repeatedly.
func worthAdmitting(function producedFunction) bool {
	if len(function.Results) == 0 {
		return false
	}
	if len(function.Parameters) == 0 {
		return false
	}
	// Returning only an error is an effect with a receipt, not a value.
	if len(function.Results) == 1 && function.ReturnsError {
		return false
	}
	return function.Pure
}

// atomDocumentationInstruction asks for the documentation registration needs.
//
// The field list is given in full and in order, because the schema requires
// every field and requires them in this order, and a run told "write the atom
// documentation" without it writes something close and is refused for a
// reason it cannot see.
//
// It says what the documentation is for. A run that believes it is writing
// comments writes comments; a run that knows the text is what a later run will
// search to decide whether this function already exists writes something
// searchable, which is the whole point of the field called retrieval concepts.
func atomDocumentationInstruction(missing []string) string {
	if len(missing) == 0 {
		return ""
	}
	var instruction strings.Builder
	fmt.Fprintf(&instruction,
		"These atoms carry no //codeflux:atom documentation, so nothing can "+
			"register them and no later task can find and reuse them: %s.\n\n",
		strings.Join(missing, ", "))
	instruction.WriteString(
		"This is not a comment for a reader. It is the record a later run " +
			"searches to decide whether the thing it is about to build " +
			"already exists here, so write it to be found.\n\n" +
			"Put it directly above the declaration, after the ordinary doc " +
			"comment, in exactly this form and this field order. Every field " +
			"is required; where one genuinely does not apply, write \"None: \" " +
			"and a short reason rather than leaving it out.\n\n")
	// A list field is shown as a list and a prose field as prose.
	//
	// Every label used to be followed by the same indented placeholder, which
	// is the right shape for prose and the wrong shape for the eight fields the
	// schema declares as lists. A list field with prose under it parses to text
	// and no items, and validation then reports it as empty — so a run wrote
	// exactly what it was shown and had its atoms refused for
	// `field "Inputs" is empty`, with Inputs written. Ladder rung 18 on
	// 2026-08-04 lost all three of its atoms that way, and the registry has
	// been empty for every run of this session.
	instruction.WriteString("//codeflux:atom\n// Codeflux atom documentation (schema v1):\n")
	for _, field := range atomdoc.CanonicalFieldTemplate() {
		if field.List {
			fmt.Fprintf(&instruction,
				"//   %s:\n//     - …\n//     - …\n", field.Label)
			continue
		}
		fmt.Fprintf(&instruction, "//   %s:\n//     …\n", field.Label)
	}
	instruction.WriteString(
		"\nThe fields shown with \"- \" are lists and need at least one item " +
			"each, written as a \"- \" line; the rest are prose. A list field " +
			"filled in with a sentence instead of items is read as empty and " +
			"the whole record is refused.")
	return instruction.String()
}
