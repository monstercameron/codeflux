package coordinator

import (
	"encoding/json"
	"strings"
	"testing"

	agentloop "codeflux.dev/codeflux/internal/agent"
)

// TestPlanningAsksForBehavioursNotFiles pins what the request is for.
//
// Files are what the old parser already knew, and they are not where the
// difficulty is. What a run needs before it starts is the list of things that
// can independently be got wrong, because that is the list its tests have to
// cover and the unit at which it can get feedback at all.
func TestPlanningAsksForBehavioursNotFiles(t *testing.T) {
	instruction := planningInstruction(
		"Create cmd/wc/main.go that counts lines, words and bytes.")
	for _, required := range []string{
		"distinct behaviours",
		"could be got right while another",
		"one behaviour per line",
		"Create cmd/wc/main.go",
	} {
		if !strings.Contains(instruction, required) {
			t.Errorf("the planning request never says %q:\n%s",
				required, instruction)
		}
	}
}

// TestADecoratedAnswerIsStillAnAnswer keeps the parser forgiving about form.
func TestADecoratedAnswerIsStillAnAnswer(t *testing.T) {
	behaviours := parseBehaviours(
		"Behaviours:\n" +
			"1. Count the lines\n" +
			"- Count the words\n" +
			"  * Count the bytes\n" +
			"\n" +
			"2) Print the three counts on one line\n")
	want := []string{
		"Count the lines", "Count the words", "Count the bytes",
		"Print the three counts on one line",
	}
	if len(behaviours) != len(want) {
		t.Fatalf("read %d behaviour(s) %v, want %d", len(behaviours),
			behaviours, len(want))
	}
	for index, expected := range want {
		if behaviours[index] != expected {
			t.Errorf("behaviour %d read as %q, want %q",
				index+1, behaviours[index], expected)
		}
	}
}

// TestAParagraphIsNotOneBehaviour is the case that would quietly ruin a plan.
//
// A model that answers with prose has not decomposed anything. Taking the
// paragraph as a single behaviour would produce a plan claiming the work is
// one unit, which is exactly the claim that sends a run into the stall the
// decomposition rung exists to break.
func TestAParagraphIsNotOneBehaviour(t *testing.T) {
	paragraph := "The program should read every line of standard input and " +
		"then it needs to count the words on each of them while also keeping " +
		"a running total of the bytes it has seen so that at the end it can " +
		"print all three of those numbers together on a single line of output."
	if got := parseBehaviours(paragraph); len(got) != 0 {
		t.Errorf("a paragraph was read as %d behaviour(s): %v", len(got), got)
	}
}

// TestAnEmptyAnswerLeavesThePlanAlone keeps a bad planning round from costing
// the run.
//
// The parsed plan is the fallback rather than the error path: a provider
// hiccup during planning should cost a worse decomposition, not the work.
func TestAnEmptyAnswerLeavesThePlanAlone(t *testing.T) {
	if got := parseBehaviours(""); len(got) != 0 {
		t.Errorf("an empty answer produced %d behaviour(s)", len(got))
	}
	if got := parseBehaviours("\n\n  \n"); len(got) != 0 {
		t.Errorf("a blank answer produced %d behaviour(s)", len(got))
	}
}

// TestEveryStepCarriesTheWholeDecomposition is what the planning call buys.
//
// One step per file is unchanged, because a step is completed by one tool call
// and a step covering two files leaves the second write with nowhere to bind.
// What changes is that a run writing one file now knows what the others owe,
// which is the thing a per-file summary could never say.
func TestEveryStepCarriesTheWholeDecomposition(t *testing.T) {
	parsed := agentPlanSteps("",
		"Create cmd/bank/main.go and cmd/bank/account.go for a ledger.")
	behaviours := []string{
		"Deposit adds to the balance",
		"Withdraw refuses to overdraw",
		"Balance prints the current total",
	}
	steps := stepsForBehaviours("", behaviours, parsed)

	edits := 0
	for _, step := range steps {
		if step.Kind != agentloop.StepKindEdit {
			continue
		}
		edits++
		if len(step.ExpectedFiles) != 1 {
			t.Errorf("%s covers %d files, so one of its writes has nowhere to "+
				"bind", step.ID, len(step.ExpectedFiles))
		}
		for _, behaviour := range behaviours {
			if !strings.Contains(step.SummaryRedacted, behaviour) {
				t.Errorf("%s does not carry %q, so a run writing this file "+
					"cannot know what the others owe", step.ID, behaviour)
			}
		}
	}
	// Four, not two: the parser pairs every named source file with its test
	// file, because a run asked for a function and not for its test writes
	// neither. Planning does not change which files exist — that is a fact
	// about the request and the repository, not something to decide.
	if edits != 4 {
		t.Errorf("%d edit step(s) for two named files and their tests", edits)
	}
	// The verify step survives: without it nothing ever runs the tests.
	last := steps[len(steps)-1]
	if last.Kind != agentloop.StepKindTest {
		t.Errorf("the plan ends with a %s step rather than a test step",
			last.Kind)
	}
}

// TestAPlanWithNoFilesFallsBackRatherThanInventingPaths keeps the model out of
// a decision it cannot make correctly.
//
// Which files exist is a fact about the request and the repository. A model
// asked to invent paths produces a plan whose steps bind to nothing on disk.
func TestAPlanWithNoFilesFallsBackRatherThanInventingPaths(t *testing.T) {
	parsed := agentPlanSteps("", "Make it faster.")
	steps := stepsForBehaviours("", []string{"Be quicker"}, parsed)
	for _, step := range steps {
		for _, file := range step.ExpectedFiles {
			if !strings.Contains(
				strings.Join(fileNamesOf(parsed), " "), file,
			) {
				t.Errorf("planning invented the path %q", file)
			}
		}
	}
}

// fileNamesOf is every file a plan expects to write.
func fileNamesOf(steps []agentloop.PlanStep) []string {
	var files []string
	for _, step := range steps {
		files = append(files, step.ExpectedFiles...)
	}
	return files
}

// TestAPlanningAnswerIsReadFromItsSchema covers the structured path.
func TestAPlanningAnswerIsReadFromItsSchema(t *testing.T) {
	behaviours, _, ok := decodedBehaviours(
		`{"behaviours":["print the greeting","refuse an empty name"]}`)
	if !ok || len(behaviours) != 2 {
		t.Fatalf("a schema-shaped answer decoded to %v (ok=%t)", behaviours, ok)
	}
	if behaviours[0] != "print the greeting" {
		t.Errorf("the first behaviour reads %q", behaviours[0])
	}
}

// TestAProseAnswerFallsBackToTheLineParser keeps a model that cannot honour a
// schema working, which is why the line parser stays.
func TestAProseAnswerFallsBackToTheLineParser(t *testing.T) {
	if _, _, ok := decodedBehaviours("- print the greeting\n- refuse an empty name"); ok {
		t.Error("prose was read as a schema answer")
	}
	if got := parseBehaviours("- print the greeting\n- refuse an empty name"); len(got) != 2 {
		t.Errorf("the fallback parser found %d behaviour(s), wanted 2", len(got))
	}
}

// TestTheBehaviourSchemaIsStrictAndClosed guards the two properties the
// Responses API requires of a strict schema, and which a schema permitting
// extra fields would lose: the model could answer a different question in them.
func TestTheBehaviourSchemaIsStrictAndClosed(t *testing.T) {
	schema := behaviourSchema()
	if !schema.Strict {
		t.Error("the planning schema is not strict, so it is a suggestion")
	}
	var decoded map[string]any
	if err := json.Unmarshal(schema.Schema, &decoded); err != nil {
		t.Fatalf("the planning schema is not valid JSON: %v", err)
	}
	if extra, ok := decoded["additionalProperties"].(bool); !ok || extra {
		t.Error("the planning schema permits properties nobody asked for")
	}
	// Every property is required, which is the Responses API's own rule for a
	// strict schema: it refuses one that leaves any property optional. "files"
	// is required and may be empty, which is how a request that already says
	// where the work goes is answered.
	required, ok := decoded["required"].([]any)
	if !ok {
		t.Fatalf("the planning schema requires %v", decoded["required"])
	}
	properties, ok := decoded["properties"].(map[string]any)
	if !ok {
		t.Fatal("the planning schema declares no properties")
	}
	if len(required) != len(properties) {
		t.Errorf("a strict schema must require every property it declares: "+
			"requires %v, declares %v", required, properties)
	}
	for name := range properties {
		found := false
		for _, entry := range required {
			if entry == name {
				found = true
			}
		}
		if !found {
			t.Errorf("%q is declared and not required, which the Responses "+
				"API refuses", name)
		}
	}
}
