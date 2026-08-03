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
			at := function.StartLine - 1
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
	for _, field := range [][2]string{
		{"Purpose", derivedPurpose(function)},
		{"Use when", derivedUseWhen(function)},
		{"Do not use when", derivedDoNotUseWhen(function)},
		{"Semantics", derivedSemantics(function)},
		{"Inputs", derivedInputs(function)},
		{"Outputs", derivedOutputs(function)},
		{"Preconditions", derivedPreconditions(function)},
		{"Postconditions", derivedPostconditions(function)},
		{"Effects", derivedEffects(function)},
		{"Failure semantics", derivedFailure(function)},
		{"Determinism", derivedDeterminism(function)},
		{"Idempotency and retry", derivedIdempotency(function)},
		{"Reconciliation and compensation", derivedCompensation(function)},
		{"Security and privacy", derivedSecurity(function)},
		{"Dependencies and bindings", derivedDependencies(function)},
		{"Complexity and limits", derivedComplexity(function)},
		{"Examples", derivedExamples(function)},
		{"Verification", derivedVerification(function)},
		{"Retrieval concepts", derivedRetrievalConcepts(function)},
	} {
		fmt.Fprintf(&comment, "//   %s:\n//     %s\n", field[0], field[1])
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

func derivedInputs(function producedFunction) string {
	return phraseList(function.Parameters, "None: it takes no arguments.")
}

func derivedOutputs(function producedFunction) string {
	return phraseList(function.Results, "None: it returns nothing, so its "+
		"whole contribution is its effects.")
}

func derivedPreconditions(function producedFunction) string {
	if len(function.Parameters) == 0 {
		return "None: there is nothing to constrain."
	}
	return "the arguments are of the declared types; nothing further is " +
		"enforced by the signature, and what the body requires beyond that is " +
		"recorded in failure semantics."
}

func derivedPostconditions(function producedFunction) string {
	if function.ReturnsError {
		return "on success the results are meaningful; on failure the error " +
			"is non-nil and the other results must not be relied on."
	}
	if len(function.Results) == 0 {
		return "the effects below have happened."
	}
	return "the results are meaningful for every input the preconditions admit."
}

func derivedEffects(function producedFunction) string {
	if len(function.Effects) == 0 {
		return "None: it reaches nothing outside its arguments."
	}
	return strings.Join(function.Effects, ", ") + "."
}

func derivedFailure(function producedFunction) string {
	if function.ReturnsError {
		return "failure is returned as an error the caller must handle."
	}
	return "None: it returns no error, so it cannot report a failure to its " +
		"caller and must not be given input it cannot handle."
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

func derivedExamples(function producedFunction) string {
	return "see the tests that name " + function.Name + "; they are the " +
		"worked examples, and they are executed rather than described."
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
