package coordinator

import (
	"sort"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/pipeline"
)

// refinementGates names, for every stage that can record a failure, the gate
// in the attempt loop that can send work back for it.
//
// This map is the rule that the run and the record answer the same questions.
// A stage that can record `failed` and has no gate here is a stage that can
// only accuse: it reports a defect the run was never asked to fix, so a reader
// cannot tell an unfixed defect from an unaskable one, and no number of
// attempts closes it. Rung 5 ended with four failed stages, three of which
// were exactly that.
//
// An empty gate is a deliberate declaration that a stage is report-only, and
// each one carries the reason. It is not an escape hatch: a stage listed as
// report-only is one whose finding is genuinely not the run's to fix.
var refinementGates = map[string]string{
	// Sent back through the loop.
	pipeline.StageNameAssembly:         "assembly",
	pipeline.StageNameAtomVerification: "completeness",
	"atom-documentation":               "completeness",
	"atom-optimization":                "completeness",
	"atom-case-synthesis":              "atom-case-synthesis",
	pipeline.StageNameEndToEndTests:    "acceptance",
	pipeline.StageNameAdversarial:      "adversarial",
	"anti-patterns":                    "adversarial",
	"atom-mutation":                    "adversarial",

	// Report-only, with the reason each one is not the run's to fix.
	"instructions":            "", // the request is the person's, not the run's
	"clarification":           "", // answered by asking, not by rewriting code
	"atomic-instructions":     "", // decided before any code exists
	"decomposition-coverage":  "", // a property of the plan, not of the work
	"contracts":               "", // derived from the code that was written
	"atoms":                   "", // classifies what was written; the gates act on it
	"recall":                  "", // reports what a search found
	"atom-example-tests":      "", // subsumed by atom-verification's gate
	"atom-property-tests":     "", // subsumed by atom-case-synthesis's gate
	"atom-fuzz":               "", // skips when there is no boundary to fuzz
	"atom-complexity":         "", // a label on structure, not a defect
	"composition-obligations": "", // derived from the code that was written
	"molecule-tests":          "", // subsumed by atom-verification's gate
	"molecules":               "", // subsumed by atom-verification's gate
	"molecule-verification":   "", // subsumed by atom-verification's gate
	"control-obligations":     "", // derived from the code that was written
	"control-tests":           "", // subsumed by atom-case-synthesis's gate
	"control-flow":            "", // derived from the code that was written
	"path-coverage":           "", // subsumed by atom-case-synthesis's gate
	"program":                 "", // a consequence of assembly
	"integration-tests":       "", // the loop's own validation step
	"global-invariants":       "", // reports capabilities; only a declared restriction fails
	"repetition":              "", // a flake is not fixed by being told about it
	"platform-matrix":         "", // a build result, reported not refined
	"non-functional":          "", // a measurement against a budget
	"evidence-bundle":         "", // describes the ledger it is written into
	"human-acceptance":        "", // a person's decision
	"deliver":                 "", // the consequence of everything above
	"acceptance-oracle":       "", // decided before any code exists, from the request and the untouched worktree alone
	// Report-only for a reason worth stating rather than assuming. Every row it
	// audits is either a check that legitimately declined or a stage nobody has
	// built, and neither is something the run can repair by trying again. A
	// send-back here would ask the work to fix the flow's own gaps, and a run
	// could raise its ratio only by declining less honestly.
	"skip-audit": "",
}

// TestEveryStageThatCanFailIsEitherRefinableOrDeclaredReportOnly is the guard
// on the defect class rung 5 exposed.
//
// Three of that run's four failed stages reported gaps the loop had no way to
// raise: case synthesis had lost its send-back when three refinement passes
// were consolidated into one, verification used a stricter definition of
// "tested" than the gate did, and documentation demanded a comment on main
// that the gate was forbidden from asking for. Each looked like an ordinary
// failure in the ledger and none of them could ever be fixed.
func TestEveryStageThatCanFailIsEitherRefinableOrDeclaredReportOnly(t *testing.T) {
	for _, stage := range pipeline.Flow {
		gate, declared := refinementGates[stage.Name]
		if !declared {
			t.Errorf("%s is in the flow and this map does not say whether a "+
				"run can be sent back for it; add its gate, or declare it "+
				"report-only with the reason it is not the run's to fix",
				stage.Name)
			continue
		}
		_ = gate
	}
	// And nothing here names a stage the flow does not have, which is how a
	// renamed stage silently loses its gate.
	inFlow := map[string]bool{}
	for _, stage := range pipeline.Flow {
		inFlow[stage.Name] = true
	}
	for name := range refinementGates {
		if !inFlow[name] {
			t.Errorf("%q has a refinement gate and is not a stage of the flow",
				name)
		}
	}
}

// TestEveryDeclaredGateIsOneTheLoopActuallyUses keeps the map honest.
//
// A gate name in the map that the loop never passes to sendBack is a promise
// nothing keeps: the stage reads as refinable and behaves as report-only,
// which is worse than being listed report-only, because nobody goes looking.
func TestEveryDeclaredGateIsOneTheLoopActuallyUses(t *testing.T) {
	// The gates the attempt loop can send work back under. Kept here rather
	// than derived, because deriving it from the source would test the parser.
	used := map[string]bool{
		"assembly":            true,
		"completeness":        true,
		"acceptance":          true,
		"adversarial":         true,
		"atom-case-synthesis": true,
	}
	var missing []string
	for stage, gate := range refinementGates {
		if gate != "" && !used[gate] {
			missing = append(missing, stage+" → "+gate)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("these stages name a gate the loop never sends back under: %s",
			strings.Join(missing, ", "))
	}
}

// TestTheGateAndTheLedgerAgreeOnWhatIsTested is the specific disagreement that
// let rung 5 be told its work was finished while the record said otherwise.
//
// The gate accepted any identifier of the function's name appearing anywhere
// in a test file — including the function's own declaration, when it happened
// to be declared in one — while the verification stage required a test that
// names it. One run, two answers, and the stricter one had no voice.
func TestTheGateAndTheLedgerAgreeOnWhatIsTested(t *testing.T) {
	// A helper declared in a test file and named by nothing.
	worktree := writeWorktree(t, map[string]string{
		"cmd/thing/main.go": "package main\n\n" +
			"// Add returns the sum of two numbers.\nfunc Add(a, b int) int " +
			"{\n\treturn a + b\n}\n\n" +
			"// main prints a sum.\nfunc main() {\n\t_ = Add(1, 2)\n}\n",
		"cmd/thing/main_test.go": "package main\n\nimport \"testing\"\n\n" +
			"// failingWriter reports an error on every write.\n" +
			"type failingWriter struct{}\n\n" +
			"func (failingWriter) Write(p []byte) (int, error) " +
			"{\n\treturn 0, nil\n}\n\n" +
			"func TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 " +
			"{\n\t\tt.Fatal(\"no\")\n\t}\n}\n",
	})

	gaps, err := findCompletenessGaps(
		worktree, pipeline.DefaultSettings(), changeAttribution{})
	if err != nil {
		t.Fatal(err)
	}
	// Write is scaffolding: it lives in a test file, the suite exercises it,
	// and demanding a test for the test helper is what cost rung 5 three of
	// its six attempts.
	for _, name := range gaps.UntestedAtoms {
		if name == "Write" {
			t.Error("the gate demands a test for a helper declared in a test file")
		}
	}
	for _, name := range gaps.UndocumentedAtoms {
		if name == "Write" {
			t.Error("the gate demands a doc comment on a test helper")
		}
	}

	// And the ledger stage reaches the same verdict, which is the whole point.
	outcome := checkAtomVerification(t.Context(), worktree)
	if !outcome.Held {
		t.Errorf("the gate is satisfied and the ledger is not: %s", outcome.Detail)
	}
	documentation := checkAtomDocumentation(worktree)
	if !documentation.Held {
		t.Errorf("the gate is satisfied and documentation is not: %s",
			documentation.Detail)
	}
}

// TestAnUndocumentedMainIsAskedForRatherThanOnlyReported closes the
// contradiction where one stage exempted main and another required it.
//
// The gate skipped main outright and the ledger demanded a comment on it, so
// the record reported a gap no attempt could ever close.
func TestAnUndocumentedMainIsAskedForRatherThanOnlyReported(t *testing.T) {
	worktree := writeWorktree(t, map[string]string{
		"cmd/thing/main.go": "package main\n\n" +
			"func main() {\n\tprintln(\"hi\")\n}\n",
	})
	gaps, err := findCompletenessGaps(
		worktree, pipeline.DefaultSettings(), changeAttribution{})
	if err != nil {
		t.Fatal(err)
	}
	asked := false
	for _, name := range gaps.UndocumentedAtoms {
		if name == "main" {
			asked = true
		}
	}
	if !asked {
		t.Error("the ledger reports an undocumented main and the gate never " +
			"asks for one, so no attempt can close it")
	}
	if checkAtomDocumentation(worktree).Held {
		t.Error("the ledger accepts an undocumented main the gate asks for, " +
			"so the two disagree in the other direction")
	}
}
