package coordinator

import (
	"strings"
	"testing"
)

// TestPIPE118_ANamedTestExampleIsParsedAndAnchored covers PIPE-118's parsing.
//
// The command form can only be satisfied by work with a command-line surface,
// so a library change, a refactor, a migration, and behaviour-linked
// documentation could state no acceptance example at all.
func TestPIPE118_ANamedTestExampleIsParsedAndAnchored(t *testing.T) {
	requirement := "Make the parser reject an empty record.\n\n" +
		acceptanceOpen + "\ntest: TestParse\npackage: ./internal/parse\n" +
		acceptanceClose + "\n"

	examples := parseNamedTestExamples(requirement)
	if len(examples) != 1 {
		t.Fatalf("parsed %d named-test examples, want 1", len(examples))
	}
	if examples[0].Name != "TestParse" {
		t.Errorf("test name = %q", examples[0].Name)
	}
	if examples[0].Package != "./internal/parse" {
		t.Errorf("package = %q", examples[0].Package)
	}
}

// TestPIPE118_AnExampleWithoutAPackageCoversTheWholeModule keeps the form
// usable by a requester who knows the test name but not where it will live.
func TestPIPE118_AnExampleWithoutAPackageCoversTheWholeModule(t *testing.T) {
	examples := parseNamedTestExamples(
		acceptanceOpen + "\ntest: TestThing\n" + acceptanceClose)
	if len(examples) != 1 || examples[0].Package != "./..." {
		t.Fatalf("a package-less example did not default to the module: %+v", examples)
	}
}

// TestPIPE118_ANameThatIsNotATestIsRefused stops a typo becoming a pattern
// that matches nothing and reports success.
func TestPIPE118_ANameThatIsNotATestIsRefused(t *testing.T) {
	for _, name := range []string{"parse", "myFunction", "", "   "} {
		examples := parseNamedTestExamples(
			acceptanceOpen + "\ntest: " + name + "\n" + acceptanceClose)
		if len(examples) != 0 {
			t.Errorf("%q was accepted as a test name: %+v", name, examples)
		}
	}
	// The three Go test-function prefixes are accepted.
	for _, name := range []string{"TestThing", "ExampleThing", "FuzzThing"} {
		examples := parseNamedTestExamples(
			acceptanceOpen + "\ntest: " + name + "\n" + acceptanceClose)
		if len(examples) != 1 {
			t.Errorf("%q was refused", name)
		}
	}
}

// TestPIPE118_ANamedTestIsRunAndItsOutcomeReported is the executable half.
func TestPIPE118_ANamedTestIsRunAndItsOutcomeReported(t *testing.T) {
	worktree := testedNamesFixture(t, map[string]string{
		"go.mod": "module codeflux.test/named\n\ngo 1.26.0\n",
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

func TestAnswerRejects(t *testing.T) {
	t.Fatal("this one always fails")
}
`,
	})

	t.Run("a passing named test is reported as passed", func(t *testing.T) {
		passed, failures := runNamedTestExamples(t.Context(), worktree,
			[]namedTestExample{{Package: "./...", Name: "TestAnswer"}})
		if len(failures) != 0 {
			t.Fatalf("a passing test reported failures: %v", failures)
		}
		if len(passed) != 1 {
			t.Fatalf("passed = %v", passed)
		}
	})

	t.Run("a failing named test is reported as failed", func(t *testing.T) {
		_, failures := runNamedTestExamples(t.Context(), worktree,
			[]namedTestExample{{Package: "./...", Name: "TestAnswerRejects"}})
		if len(failures) != 1 {
			t.Fatalf("a failing test was not reported: %v", failures)
		}
	})

	t.Run("a name that matches nothing is a failure, not a pass", func(t *testing.T) {
		// A -run pattern matching nothing exits zero, so a misspelled name
		// would otherwise satisfy the example without running anything.
		_, failures := runNamedTestExamples(t.Context(), worktree,
			[]namedTestExample{{Package: "./...", Name: "TestNoSuchThing"}})
		if len(failures) != 1 {
			t.Fatalf("a name matching no test was accepted: %v", failures)
		}
		if !strings.Contains(failures[0], "matched no test") {
			t.Errorf("the failure does not say why: %q", failures[0])
		}
	})

	t.Run("the pattern is anchored on both sides", func(t *testing.T) {
		// TestAnswer must not be satisfied by TestAnswerRejects, which is the
		// prefix-matching mistake an unanchored -run would make. Naming the
		// passing test must not drag in the failing one.
		passed, failures := runNamedTestExamples(t.Context(), worktree,
			[]namedTestExample{{Package: "./...", Name: "TestAnswer"}})
		if len(failures) != 0 || len(passed) != 1 {
			t.Fatalf("an anchored name pulled in its prefix sibling: passed=%v failures=%v",
				passed, failures)
		}
	})
}
