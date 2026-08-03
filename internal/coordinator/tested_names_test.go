package coordinator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/fingerprint"
	"codeflux.dev/codeflux/internal/pipeline"
)

// TestPIPE008_AnAtomCountsAsTestedOnlyWhenATestCallsIt covers PIPE-008.
//
// testedNames collected every identifier in every test file, so an atom
// counted as tested when any identifier of that name appeared anywhere: a
// local variable, a field, a struct literal key. The gate was satisfiable by
// coincidence.
func TestPIPE008_AnAtomCountsAsTestedOnlyWhenATestCallsIt(t *testing.T) {
	worktree := testedNamesFixture(t, map[string]string{
		"go.mod": "module codeflux.test/tested\n\ngo 1.26.0\n",
		"lib.go": `package lib

func Called() int { return 1 }

func Coincidence() int { return 2 }

func ViaMethod() int { return 3 }
`,
		"lib_test.go": `package lib

import "testing"

type holder struct{ Coincidence int }

func TestThings(t *testing.T) {
	// A real call.
	if Called() != 1 {
		t.Fatal("no")
	}
	// Only ever a field name and a local variable, never a call.
	Coincidence := holder{Coincidence: 2}
	_ = Coincidence
	// A method call whose selector matches a produced function name.
	var value helper
	if value.ViaMethod() != 3 {
		t.Fatal("no")
	}
}

type helper struct{}

func (helper) ViaMethod() int { return 3 }
`,
	})

	referenced, err := testedNames(worktree)
	if err != nil {
		t.Fatalf("reading tested names: %v", err)
	}

	if !referenced["Called"] {
		t.Error("a function the test calls is not counted as tested")
	}
	if referenced["Coincidence"] {
		t.Error("a name that only ever appears as a field and a local variable " +
			"is counted as tested; the gate is satisfiable by coincidence")
	}
	if !referenced["ViaMethod"] {
		t.Error("a method call's selector is not counted, so a program composed " +
			"through methods reads as untested")
	}
}

// testedNamesFixture writes a Git repository whose files all read as produced,
// so producedGoFiles returns them.
func testedNamesFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	worktree := t.TempDir()
	initializeCoordinatorGitRepository(t, worktree)
	for name, content := range files {
		path := filepath.Join(worktree, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return worktree
}

// TestPIPE009_AProgramComposedThroughMethodsIsNotAtomic covers PIPE-009.
//
// describeBody counted only plain identifier calls, so a function that does
// all its work through methods showed zero calls and was classified atomic.
// The atom/molecule split feeds every phase B and C gate, so the whole
// structural half of the flow rested on it.
func TestPIPE009_AProgramComposedThroughMethodsIsNotAtomic(t *testing.T) {
	worktree := testedNamesFixture(t, map[string]string{
		"go.mod": "module codeflux.test/compose\n\ngo 1.26.0\n",
		"lib.go": `package lib

type engine struct{}

func (engine) Step() int { return 1 }

func (engine) Other() int { return 2 }

// Composed does all its work through methods, so it is a molecule.
func Composed() int {
	var value engine
	return value.Step() + value.Other()
}

// Leaf calls nothing, so it is an atom.
func Leaf() int { return 3 }
`,
	})

	functions, err := readProducedFunctions(worktree)
	if err != nil {
		t.Fatalf("reading produced functions: %v", err)
	}

	byName := map[string]producedFunction{}
	for _, function := range functions {
		byName[function.Name] = function
	}

	composed, found := byName["Composed"]
	if !found {
		t.Fatal("the composing function was not read at all")
	}
	if len(composed.Calls) == 0 {
		t.Error("a function whose whole body is method calls shows no calls, " +
			"so it is classified atomic and every phase B and C gate inherits that")
	}

	leaf, found := byName["Leaf"]
	if !found {
		t.Fatal("the leaf function was not read at all")
	}
	if len(leaf.Calls) != 0 {
		t.Errorf("a function that calls nothing reports %v", leaf.Calls)
	}

	atoms, molecules := atomsAndMolecules(functions)
	if len(molecules) == 0 {
		t.Error("no molecule was found in a program that composes through methods")
	}
	if len(atoms) == 0 {
		t.Error("no atom was found; the split has collapsed the other way")
	}
}

// TestPIPE010_AtomOptimizationDoesNotClaimARewriteItNeverPerformed covers
// PIPE-010.
//
// The gate says the atom is rewritten to be simpler where it can be. No
// rewrite is performed, and the stage reported satisfied anyway — with
// "rewritten": false in its own evidence. That is the exact failure the ledger
// exists to prevent: "we did not check this" rendering as "this passed".
func TestPIPE010_AtomOptimizationDoesNotClaimARewriteItNeverPerformed(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		source string
	}{
		{
			name: "with something worth simplifying",
			source: `package lib

func Tangled(values []int) int {
	total := 0
	for _, value := range values {
		if value > 0 {
			total++
		}
		if value > 1 {
			total++
		}
		if value > 2 {
			total++
		}
		if value > 3 {
			total++
		}
		if value > 4 {
			total++
		}
		if value > 5 {
			total++
		}
		if value > 6 {
			total++
		}
	}
	return total
}
`,
		},
		{
			name: "with nothing worth simplifying",
			source: `package lib

func Simple() int { return 1 }
`,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			worktree := testedNamesFixture(t, map[string]string{
				"go.mod": "module codeflux.test/simplify\n\ngo 1.26.0\n",
				"lib.go": testCase.source,
			})

			outcome := checkSimplification(worktree, changeAttribution{})
			if outcome.Held {
				t.Error("atom-optimization reported satisfied while performing " +
					"no rewrite, which claims the gate's action")
			}
			if !outcome.Skipped {
				t.Errorf("atom-optimization is neither satisfied nor skipped: %+v",
					outcome)
			}
			if len(outcome.Evidence) == 0 {
				t.Error("the skip carries no evidence, so what the check did " +
					"find was thrown away")
			}
			if rewritten, present := outcome.Evidence["rewritten"]; !present ||
				rewritten != false {
				t.Errorf("the evidence does not record that nothing was rewritten: %+v",
					outcome.Evidence)
			}
		})
	}
}

// TestPIPE011_AtomComplexityDoesNotAssertAnUnmeasuredBound covers PIPE-011.
//
// The gate requires a bound that measured growth across input sizes agrees
// with. Nothing runs the produced code at two sizes, so the stage reported
// satisfied on the strength of loop nesting alone — and asserted a space claim
// nothing had looked at.
func TestPIPE011_AtomComplexityDoesNotAssertAnUnmeasuredBound(t *testing.T) {
	worktree := testedNamesFixture(t, map[string]string{
		"go.mod": "module codeflux.test/complexity\n\ngo 1.26.0\n",
		"lib.go": `package lib

func Flat() int { return 1 }

func Nested(values []int) int {
	total := 0
	for range values {
		for range values {
			total++
		}
	}
	return total
}
`,
	})

	outcome := checkComplexity(worktree, changeAttribution{})
	if outcome.Held {
		t.Error("atom-complexity reported satisfied without measuring growth, " +
			"which claims agreement the check never established")
	}
	if !outcome.Skipped {
		t.Errorf("atom-complexity is neither satisfied nor skipped: %+v", outcome)
	}
	if _, asserted := outcome.Evidence["space_claim"]; asserted {
		t.Error("a space claim is still asserted; nothing here examines allocation")
	}
	if measured, present := outcome.Evidence["growth_measured"]; !present ||
		measured != false {
		t.Errorf("the evidence does not record that growth was not measured: %+v",
			outcome.Evidence)
	}
	// The structural label is still useful, and its limits travel with it.
	if _, labelled := outcome.Evidence["time_labels"]; !labelled {
		t.Error("the structural labels were dropped; the readable part of the " +
			"check should survive the stage declining to claim")
	}
	if _, limits := outcome.Evidence["label_limits"]; !limits {
		t.Error("the label's limits are not recorded, so a reader cannot tell " +
			"that recursion and a library sort are both labelled O(1)")
	}
}

// TestPIPE012_PlatformMatrixSeparatesRunningFromCompiling covers PIPE-012.
//
// The gate said the program passes on every platform it claims, and the check
// only compiled. Compiling says nothing about whether a program works, and a
// cross-compiled binary cannot be executed by this host at all, so the two
// claims have to be recorded separately.
func TestPIPE012_PlatformMatrixSeparatesRunningFromCompiling(t *testing.T) {
	// One package in the worktree root: the fixture's Git init already writes
	// main.go there, and a second package name in the same directory is a
	// build error rather than a platform result.
	worktree := testedNamesFixture(t, map[string]string{
		"go.mod": "module codeflux.test/matrix\n\ngo 1.26.0\n",
		"main.go": `package main

func Answer() int { return 42 }

func main() {}
`,
		"main_test.go": `package main

import "testing"

func TestAnswer(t *testing.T) {
	if Answer() != 42 {
		t.Fatal("no")
	}
}
`,
	})

	t.Run("the host answers by running its suite", func(t *testing.T) {
		outcome := checkPlatformMatrix(t.Context(), worktree, hostPlatform())
		if !outcome.Held {
			t.Fatalf("the host platform did not hold: %+v", outcome)
		}
		ran, _ := outcome.Evidence["ran_suite"].([]string)
		if len(ran) != 1 {
			t.Errorf("the host is not recorded as having run the suite: %+v",
				outcome.Evidence)
		}
		compiled, _ := outcome.Evidence["compiled_only"].([]string)
		if len(compiled) != 0 {
			t.Errorf("the host is recorded as compile-only: %+v", outcome.Evidence)
		}
	})

	t.Run("a cross target answers only that it compiles", func(t *testing.T) {
		cross := platformTarget{System: "linux", Architecture: "amd64"}
		if isHostPlatform(cross) {
			cross = platformTarget{System: "darwin", Architecture: "arm64"}
		}
		outcome := checkPlatformMatrix(t.Context(), worktree,
			[]platformTarget{cross})
		if outcome.Held {
			t.Error("a run that executed nothing reported the gate satisfied; " +
				"compiling everywhere is not passing everywhere")
		}
		if !outcome.Skipped {
			t.Errorf("a compile-only matrix is neither held nor skipped: %+v", outcome)
		}
		compiled, _ := outcome.Evidence["compiled_only"].([]string)
		if len(compiled) != 1 {
			t.Errorf("the cross target is not recorded as compile-only: %+v",
				outcome.Evidence)
		}
	})

	t.Run("the two claims are kept apart in the evidence", func(t *testing.T) {
		cross := platformTarget{System: "linux", Architecture: "amd64"}
		if isHostPlatform(cross) {
			cross = platformTarget{System: "darwin", Architecture: "arm64"}
		}
		outcome := checkPlatformMatrix(t.Context(), worktree,
			append(hostPlatform(), cross))
		if !outcome.Held {
			t.Fatalf("a host that runs plus a cross that compiles did not hold: %+v",
				outcome)
		}
		ran, _ := outcome.Evidence["ran_suite"].([]string)
		compiled, _ := outcome.Evidence["compiled_only"].([]string)
		if len(ran) != 1 || len(compiled) != 1 {
			t.Errorf("run and compile claims are not separated: %+v", outcome.Evidence)
		}
		if _, stated := outcome.Evidence["claim_per_target"]; !stated {
			t.Error("the evidence does not say which claim each target answered")
		}
	})
}

// TestPIPE014_AtomFuzzEnumeratesBoundariesRatherThanSkippingOnAbsence covers
// PIPE-014.
//
// checkFuzzing skipped whenever no fuzz target existed, so the cheapest way to
// satisfy the stage was to write no fuzzing, and no decoding boundary was ever
// enumerated. The absence of a target is only a non-question when there is
// nothing to fuzz.
func TestPIPE014_AtomFuzzEnumeratesBoundariesRatherThanSkippingOnAbsence(t *testing.T) {
	t.Run("a program with no boundary is skipped with the count", func(t *testing.T) {
		worktree := testedNamesFixture(t, map[string]string{
			"go.mod": "module codeflux.test/fuzz\n\ngo 1.26.0\n",
			"main.go": `package main

func Add(left int, right int) int { return left + right }

func main() {}
`,
		})
		outcome := checkFuzzing(t.Context(), worktree)
		if !outcome.Skipped {
			t.Fatalf("a program with nothing to fuzz was not skipped: %+v", outcome)
		}
		if count, present := outcome.Evidence["decoding_boundary_count"]; !present ||
			count != 0 {
			t.Errorf("the enumerated count is not recorded as evidence: %+v",
				outcome.Evidence)
		}
	})

	t.Run("a boundary with no fuzz target fails", func(t *testing.T) {
		worktree := testedNamesFixture(t, map[string]string{
			"go.mod": "module codeflux.test/fuzz\n\ngo 1.26.0\n",
			"main.go": `package main

import "errors"

// ParseRecord decodes untrusted input and is exactly what fuzzing is for.
func ParseRecord(raw []byte) (int, error) {
	if len(raw) == 0 {
		return 0, errors.New("empty")
	}
	return len(raw), nil
}

func main() {}
`,
		})
		outcome := checkFuzzing(t.Context(), worktree)
		if outcome.Skipped {
			t.Fatal("a decoding boundary with no fuzz target was skipped; " +
				"writing no fuzzing is the cheapest way to satisfy the stage")
		}
		if outcome.Held {
			t.Fatalf("an unfuzzed boundary reported satisfied: %+v", outcome)
		}
		boundaries, _ := outcome.Evidence["decoding_boundaries"].([]string)
		if len(boundaries) == 0 {
			t.Errorf("the boundary was not named in the evidence: %+v", outcome.Evidence)
		}
	})
}

// TestPIPE014_BoundaryDetectionUsesBothDeclaredSignals states the rule the
// enumeration applies, so a later reader can argue with it rather than guess.
func TestPIPE014_BoundaryDetectionUsesBothDeclaredSignals(t *testing.T) {
	worktree := testedNamesFixture(t, map[string]string{
		"go.mod": "module codeflux.test/signals\n\ngo 1.26.0\n",
		"main.go": `package main

import "errors"

// Named by its verb, even though it takes no raw input.
func DecodeThing(count int) int { return count }

// Shaped like a decoder: raw input in, error out.
func Interpret(raw string) (int, error) {
	if raw == "" {
		return 0, errors.New("empty")
	}
	return len(raw), nil
}

// Neither: no decoding verb, no raw input.
func Total(values []int) int {
	sum := 0
	for _, value := range values {
		sum += value
	}
	return sum
}

// Raw input but no error result, so not a decoding boundary.
func Length(raw string) int { return len(raw) }

func main() {}
`,
	})

	boundaries, err := decodingBoundaries(worktree)
	if err != nil {
		t.Fatalf("enumerating boundaries: %v", err)
	}
	found := map[string]bool{}
	for _, name := range boundaries {
		found[name] = true
	}
	if !found["DecodeThing"] {
		t.Error("a function named by a decoding verb was not counted")
	}
	if !found["Interpret"] {
		t.Error("a function shaped like a decoder was not counted")
	}
	if found["Total"] {
		t.Error("a function with neither signal was counted")
	}
	if found["Length"] {
		t.Error("raw input without an error result was counted; that shape " +
			"cannot report a decoding failure")
	}
}

// TestPIPE015_ControlTestsAreEstablishedPerBranchingFunction covers PIPE-015.
//
// checkControlTests counted `if` and `case` nodes in test files, so one
// `if err != nil` anywhere satisfied a claim about every failure path in the
// program, and adding a branch to the code could not make the stage fail.
func TestPIPE015_ControlTestsAreEstablishedPerBranchingFunction(t *testing.T) {
	t.Run("a branching function no test reaches fails", func(t *testing.T) {
		worktree := testedNamesFixture(t, map[string]string{
			"go.mod": "module codeflux.test/control\n\ngo 1.26.0\n",
			"lib.go": `package lib

func Reached(value int) int {
	if value > 0 {
		return 1
	}
	return 0
}

func Unreached(value int) int {
	if value > 0 {
		return 2
	}
	return 0
}
`,
			"lib_test.go": `package lib

import "testing"

func TestReached(t *testing.T) {
	// A single decision node in the test used to satisfy the whole gate.
	if Reached(1) != 1 {
		t.Fatal("no")
	}
}
`,
		})

		outcome := checkControlTests(worktree)
		if outcome.Held {
			t.Fatal("a branching function no test reaches satisfied the gate")
		}
		untested, _ := outcome.Evidence["untested"].([]string)
		if len(untested) != 1 || untested[0] != "Unreached" {
			t.Errorf("the unexamined function is not named: %+v", outcome.Evidence)
		}
	})

	t.Run("every branching function reached holds", func(t *testing.T) {
		worktree := testedNamesFixture(t, map[string]string{
			"go.mod": "module codeflux.test/control\n\ngo 1.26.0\n",
			"lib.go": `package lib

func Only(value int) int {
	if value > 0 {
		return 1
	}
	return 0
}
`,
			"lib_test.go": `package lib

import "testing"

func TestOnly(t *testing.T) {
	if Only(1) != 1 {
		t.Fatal("no")
	}
}
`,
		})

		outcome := checkControlTests(worktree)
		if !outcome.Held {
			t.Fatalf("every branching function is reached and the gate did not hold: %+v",
				outcome)
		}
	})

	t.Run("a program with no decision is skipped", func(t *testing.T) {
		worktree := testedNamesFixture(t, map[string]string{
			"go.mod": "module codeflux.test/control\n\ngo 1.26.0\n",
			"lib.go": `package lib

func Flat() int { return 1 }
`,
		})

		outcome := checkControlTests(worktree)
		if !outcome.Skipped {
			t.Fatalf("a program with no decision was not skipped: %+v", outcome)
		}
		if _, stated := outcome.Evidence["unit"]; !stated {
			t.Error("the evidence does not state what the check establishes")
		}
	})
}

// TestPIPE017_OptimizationIsDecidedAfterMutation covers PIPE-017.
//
// TestAnAtomIsOptimisedOnlyOnceItsTestsCanCatchAMistake enforces that
// atom-optimization sits after atom-mutation in the flow. examineStructure
// decided them the other way round, so the declared ordering and the execution
// order disagreed and only the declaration was checked.
//
// Reading the source is weaker than observing the sequence, and it is what is
// available: both stages are decided inside one function with no seam between
// them, and the ledger records by stage number rather than by the order the
// decisions were taken.
func TestPIPE017_OptimizationIsDecidedAfterMutation(t *testing.T) {
	root := repositoryRootForCoordinatorTest(t)
	source, err := os.ReadFile(filepath.Join(
		root, "internal", "coordinator", "agent_stage_runner.go"))
	if err != nil {
		t.Fatalf("read the stage runner: %v", err)
	}
	body := string(source)

	mutation := strings.Index(body, "pipeline.StageAtomMutation,\n\t\texecution.checkMutations")
	optimization := strings.Index(body, "pipeline.StageAtomOptimization,\n\t\tcheckSimplification")
	if mutation < 0 {
		t.Fatal("the mutation decision has moved; re-check the ordering by hand")
	}
	if optimization < 0 {
		t.Fatal("the optimization decision has moved; re-check the ordering by hand")
	}
	if optimization < mutation {
		t.Error("atom-optimization is decided before atom-mutation, so a rewrite " +
			"would be chosen under tests nobody has shown can detect a fault")
	}
}

// TestPIPE117_TheAcceptanceCapabilityTableCoversEveryDeclaredTaskClass keeps
// the spike's finding in step with the product.
//
// internal/pipeline writes the class names out rather than importing
// fingerprint, because the vocabulary layer importing a domain package for a
// list of strings would invert the dependency. This is what stops the two
// drifting: a class added to fingerprint with no recorded acceptance
// capability fails here.
func TestPIPE117_TheAcceptanceCapabilityTableCoversEveryDeclaredTaskClass(t *testing.T) {
	declared := make([]string, 0, len(fingerprint.AllTaskClasses()))
	for _, class := range fingerprint.AllTaskClasses() {
		declared = append(declared, string(class))
	}
	if len(declared) == 0 {
		t.Fatal("no task class is declared; the check would be vacuous")
	}
	if findings := pipeline.ValidateAcceptanceCapabilities(declared); len(findings) > 0 {
		t.Fatalf("the acceptance capability table has drifted from the declared "+
			"task classes:\n  %s", strings.Join(findings, "\n  "))
	}
}
