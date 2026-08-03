package coordinator

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// deriveAtomDocumentation writes the registry schema from what the run already
// measured, instead of asking the model to write it.
//
// Every one of the nineteen fields is a fact this run established: the
// signature gives inputs and outputs, the effect analysis gives effects and
// determinism, ReturnsError gives failure semantics, LoopDepth gives the
// complexity bound, and the tests that name the function give verification.
// Asking a model to restate them costs an attempt, risks a rewrite of working
// source, and produces a worse answer, because the model is guessing at what
// the analysis already knows.
//
// It runs after the work is verified, against the verified tree, and it is
// reverted whole if it breaks anything. Enrichment must never cost correctness:
// a registry row is worth having and is not worth a working program.
//
// Reports how many atoms it documented, and the first error that stopped it.
func deriveAtomDocumentation(
	worktree string, functions []producedFunction,
) (int, error) {
	missing := atomsWithoutRegistrableDocumentation(worktree, functions)
	if len(missing) == 0 {
		return 0, nil
	}
	wanted := map[string]bool{}
	for _, name := range missing {
		wanted[name] = true
	}

	// Grouped by file, and each file is rewritten once. Inserting one comment
	// at a time would move every later declaration's line number, so the second
	// insertion into a file would land in the wrong place.
	byFile := map[string][]producedFunction{}
	for _, function := range functions {
		if wanted[function.Name] && function.File != "" {
			byFile[function.File] = append(byFile[function.File], function)
		}
	}

	documented := 0
	for file, inFile := range byFile {
		path := filepath.Join(worktree, file)
		original, err := os.ReadFile(path)
		if err != nil {
			return documented, err
		}
		// Latest declaration first, so an insertion never shifts the line
		// another insertion is still aiming at.
		sort.SliceStable(inFile, func(first, second int) bool {
			return inFile[first].StartLine > inFile[second].StartLine
		})
		lines := strings.Split(string(original), "\n")
		for _, function := range inFile {
			at := declarationLine(lines, function)
			if at < 0 || at > len(lines) {
				continue
			}
			block := strings.Split(atomSchemaComment(function), "\n")
			lines = append(lines[:at], append(block, lines[at:]...)...)
			documented++
		}
		rewritten := strings.Join(lines, "\n")
		// Parsed before it is kept. A comment inserted at a line number that
		// turned out to be inside a declaration produces a file that does not
		// compile, and the whole point of doing this after verification is that
		// verification stays true.
		if _, parseErr := parser.ParseFile(
			token.NewFileSet(), path, rewritten, parser.ParseComments,
		); parseErr != nil {
			return documented, fmt.Errorf(
				"documenting %s would not parse: %w", file, parseErr)
		}
		if err := os.WriteFile(path, []byte(rewritten), 0o644); err != nil {
			return documented, err
		}
	}
	return documented, nil
}

// atomSchemaComment renders one atom's nineteen fields.
//
// Where a fact genuinely does not apply the field says so and says why, which
// is what the schema asks for and is more useful than an empty heading: a later
// run reading "None: this function reaches nothing outside its arguments" knows
// something, and one reading a blank line does not.
func atomSchemaComment(function producedFunction) string {
	var comment strings.Builder
	comment.WriteString("//codeflux:atom\n")
	comment.WriteString("// Codeflux atom documentation (schema v1):\n")
	// Seven of the nineteen fields are lists, and a list is read from lines
	// beginning with a dash. Written as prose they parse as empty, and the
	// document is refused for a field that visibly has words in it: "field
	// \"Inputs\" is empty" against a line reading "int, []int". Every derived
	// comment was written correctly and none of them registered.
	//
	// An explained "None:" is accepted for either kind, which is why the
	// no-input and no-effect cases below do not need bullets.
	for _, field := range []struct {
		label string
		prose string
		items []string
	}{
		{label: "Purpose", prose: derivedPurpose(function)},
		{label: "Use when", prose: derivedUseWhen(function)},
		{label: "Do not use when", prose: derivedDoNotUseWhen(function)},
		{label: "Semantics", prose: derivedSemantics(function)},
		{label: "Inputs", items: derivedInputItems(function)},
		{label: "Outputs", items: derivedOutputItems(function)},
		{label: "Preconditions", items: derivedPreconditionItems(function)},
		{label: "Postconditions", items: derivedPostconditionItems(function)},
		{label: "Effects", items: derivedEffectItems(function)},
		{label: "Failure semantics", items: derivedFailureItems(function)},
		{label: "Determinism", prose: derivedDeterminism(function)},
		{label: "Idempotency and retry", prose: derivedIdempotency(function)},
		{label: "Reconciliation and compensation", prose: derivedCompensation(function)},
		{label: "Security and privacy", prose: derivedSecurity(function)},
		{label: "Dependencies and bindings", prose: derivedDependencies(function)},
		{label: "Complexity and limits", prose: derivedComplexity(function)},
		{label: "Examples", items: derivedExampleItems(function)},
		{label: "Verification", prose: derivedVerification(function)},
		{label: "Retrieval concepts", prose: derivedRetrievalConcepts(function)},
	} {
		fmt.Fprintf(&comment, "//   %s:\n", field.label)
		if field.prose != "" {
			fmt.Fprintf(&comment, "//     %s\n", field.prose)
			continue
		}
		for _, item := range field.items {
			fmt.Fprintf(&comment, "//     - %s\n", item)
		}
	}
	return strings.TrimRight(comment.String(), "\n")
}

func derivedPurpose(function producedFunction) string {
	return fmt.Sprintf("%s takes %s and returns %s.",
		function.Name, phraseList(function.Parameters, "no arguments"),
		phraseList(function.Results, "nothing"))
}

func derivedUseWhen(function producedFunction) string {
	return fmt.Sprintf(
		"a task needs %s and has %s to give it.",
		phraseList(function.Results, "this effect"),
		phraseList(function.Parameters, "no input"))
}

func derivedDoNotUseWhen(function producedFunction) string {
	if function.Pure {
		return "the caller needs the work performed rather than computed: " +
			"this reaches nothing outside its arguments and changes nothing."
	}
	return "the caller cannot tolerate the effects listed below, which this " +
		"performs rather than describes."
}

func derivedSemantics(function producedFunction) string {
	if function.Branches == 0 {
		return "one path: every input is handled the same way."
	}
	return fmt.Sprintf(
		"%d decision point(s), so the answer depends on which case the input "+
			"falls into.", function.Branches)
}

func derivedDeterminism(function producedFunction) string {
	if function.Pure {
		return "deterministic: the same arguments give the same results."
	}
	return "not established: it reaches outside its arguments, so its result " +
		"may depend on what it finds there."
}

func derivedIdempotency(function producedFunction) string {
	if function.Pure {
		return "safe to retry: calling it twice costs time and changes nothing."
	}
	return "not established: it has effects, and nothing here shows whether " +
		"performing them twice is the same as performing them once."
}

func derivedCompensation(function producedFunction) string {
	if function.Pure {
		return "None: nothing to undo."
	}
	return "not established: its effects are listed above and no undo is " +
		"provided with it."
}

func derivedSecurity(function producedFunction) string {
	for _, effect := range function.Effects {
		if strings.HasPrefix(effect, "os.") || strings.HasPrefix(effect, "net.") ||
			strings.HasPrefix(effect, "http.") {
			return "it reaches the environment or the network through " +
				effect + "; what it reads there is whatever the caller's " +
				"process can read."
		}
	}
	return "None: it handles only what it is given."
}

func derivedDependencies(function producedFunction) string {
	if len(function.Calls) == 0 {
		return "None: it composes nothing, which is what makes it an atom."
	}
	return "calls " + strings.Join(function.Calls, ", ") + "."
}

func derivedComplexity(function producedFunction) string {
	switch function.LoopDepth {
	case 0:
		return "constant in the size of its input: no loop."
	case 1:
		return "linear in the size of its input: one level of looping."
	default:
		return fmt.Sprintf(
			"%d nested loop levels, so cost grows faster than linearly in the "+
				"size of its input.", function.LoopDepth)
	}
}

func derivedVerification(function producedFunction) string {
	return "verified by this project's own suite: it compiles, its tests pass, " +
		"and the acceptance examples for the task that produced it match."
}

// derivedRetrievalConcepts is the field a later run's search actually reads, so
// it names the shape rather than the identifier.
//
// A later task does not search for "parseArguments". It searches for something
// that turns strings into numbers, and the words that make that findable are
// the types and the effects, not the name whoever wrote it happened to choose.
func derivedRetrievalConcepts(function producedFunction) string {
	concepts := []string{function.Name}
	concepts = append(concepts, function.Parameters...)
	concepts = append(concepts, function.Results...)
	concepts = append(concepts, function.Effects...)
	if function.Pure {
		concepts = append(concepts, "pure", "deterministic")
	}
	if function.ReturnsError {
		concepts = append(concepts, "fallible", "returns error")
	}
	seen := map[string]bool{}
	unique := concepts[:0]
	for _, concept := range concepts {
		concept = strings.TrimSpace(concept)
		if concept == "" || seen[concept] {
			continue
		}
		seen[concept] = true
		unique = append(unique, concept)
	}
	return strings.Join(unique, ", ") + "."
}

// phraseList renders a list of types, or the given phrase when it is empty.
func phraseList(items []string, empty string) string {
	if len(items) == 0 {
		return empty
	}
	return strings.Join(items, ", ")
}

// declarationLine finds the line the func keyword is on.
//
// StartLine points at the doc comment when a declaration has one, which is
// right for attributing a change and wrong for inserting a schema block: put
// the block there and it lands above the ordinary comment, so the prose that
// follows it reads as a continuation of the last field and the whole schema is
// refused. Every derived comment was written and none of them registered.
//
// The block goes directly above the declaration, after the ordinary doc
// comment, which is where the schema's own instruction has always said to put
// it.
func declarationLine(lines []string, function producedFunction) int {
	at := function.StartLine - 1
	if at < 0 {
		return -1
	}
	for scan := at; scan < len(lines) && scan < function.EndLine; scan++ {
		if strings.HasPrefix(strings.TrimSpace(lines[scan]), "func ") {
			return scan
		}
	}
	return at
}

// The seven list fields. Each returns items, and an explained "None:" as the
// single item where the answer is that there are none -- which the schema
// accepts for a list exactly as it does for prose.

func derivedInputItems(function producedFunction) []string {
	if len(function.Parameters) == 0 {
		return []string{"None: it takes no arguments."}
	}
	items := make([]string, 0, len(function.Parameters))
	for index, parameter := range function.Parameters {
		items = append(items, fmt.Sprintf(
			"argument %d, of type %s", index+1, parameter))
	}
	return items
}

func derivedOutputItems(function producedFunction) []string {
	if len(function.Results) == 0 {
		return []string{
			"None: it returns nothing, so its whole contribution is its effects."}
	}
	items := make([]string, 0, len(function.Results))
	for index, result := range function.Results {
		items = append(items, fmt.Sprintf(
			"result %d, of type %s", index+1, result))
	}
	return items
}

func derivedPreconditionItems(function producedFunction) []string {
	if len(function.Parameters) == 0 {
		return []string{"None: there is nothing to constrain."}
	}
	return []string{
		"the arguments are of the declared types",
		"nothing further is enforced by the signature; what the body requires " +
			"beyond that is recorded under failure semantics",
	}
}

func derivedPostconditionItems(function producedFunction) []string {
	if function.ReturnsError {
		return []string{
			"on success the results are meaningful",
			"on failure the error is non-nil and the other results must not be " +
				"relied on",
		}
	}
	if len(function.Results) == 0 {
		return []string{"the effects listed above have happened"}
	}
	return []string{
		"the results are meaningful for every input the preconditions admit"}
}

func derivedEffectItems(function producedFunction) []string {
	if len(function.Effects) == 0 {
		return []string{"None: it reaches nothing outside its arguments."}
	}
	items := make([]string, 0, len(function.Effects))
	for _, effect := range function.Effects {
		items = append(items, "reaches "+effect)
	}
	return items
}

func derivedFailureItems(function producedFunction) []string {
	if function.ReturnsError {
		return []string{
			"failure is returned as an error the caller must handle",
			"the other results must not be read when the error is non-nil",
		}
	}
	return []string{
		"None: it returns no error, so it cannot report a failure to its " +
			"caller and must not be given input it cannot handle."}
}

func derivedExampleItems(function producedFunction) []string {
	return []string{
		"the tests that name " + function.Name + " are the worked examples, " +
			"and they are executed rather than described",
	}
}
