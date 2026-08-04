package coordinator

import "testing"

// TestATestCallingAGenericFunctionCountsAsTestingIt is the blind spot that made
// one gate unsatisfiable.
//
// A generic call is instantiated before it is called, and the instantiation
// wraps the name: Err[int](e) parses as an IndexExpr whose X is the identifier,
// Map[A, B](f) as an IndexListExpr. A switch that knows only Ident and
// SelectorExpr sees no callee at all.
//
// It bites hardest on the functions that cannot be called any other way.
// Err[T any](error) Result[T] has no argument to infer T from, so every call
// site must instantiate it explicitly — so every call to it was invisible, and
// completeness demanded a direct test for a function that already had one.
// completeness never escalates, so ladder rung 18 on 2026-08-04 was asked for
// that same test three times running.
func TestATestCallingAGenericFunctionCountsAsTestingIt(t *testing.T) {
	worktree := writeWorktree(t, map[string]string{
		"fp/result.go": "package fp\n\n" +
			"type Result[T any] struct {\n\tvalue T\n\terr   error\n}\n\n" +
			"// Err returns a Result holding an error.\n" +
			"func Err[T any](e error) Result[T] {\n\treturn Result[T]{err: e}\n}\n\n" +
			"// Map applies f to a Result's value.\n" +
			"func Map[A any, B any](r Result[A], f func(A) B) Result[B] {\n" +
			"\treturn Result[B]{err: r.err}\n}\n",
		"fp/result_test.go": "package fp\n\n" +
			"import (\n\t\"errors\"\n\t\"testing\"\n)\n\n" +
			"func TestErr(t *testing.T) {\n" +
			"\tgot := Err[int](errors.New(\"no\"))\n\t_ = got\n}\n\n" +
			"func TestMap(t *testing.T) {\n" +
			"\tgot := Map[int, string](Err[int](errors.New(\"no\")), nil)\n" +
			"\t_ = got\n}\n",
	})

	naming, err := testsNamingInFiles(worktree,
		[]string{"fp/result.go", "fp/result_test.go"})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Err", "Map"} {
		if len(naming[name]) == 0 {
			t.Errorf("%s is called by a test and the gate cannot see it, so it "+
				"will ask for a test that is already written — every attempt, "+
				"on a gate that never escalates", name)
		}
	}
}

// TestAnOrdinaryCallIsStillSeen is the control.
//
// Unwrapping the instantiation must not lose the two shapes that always
// worked: a plain call and a qualified one.
func TestAnOrdinaryCallIsStillSeen(t *testing.T) {
	worktree := writeWorktree(t, map[string]string{
		"pkg/thing.go": "package pkg\n\n" +
			"// Plain does a thing.\nfunc Plain(v int) int { return v }\n",
		"pkg/thing_test.go": "package pkg\n\n" +
			"import (\n\t\"strings\"\n\t\"testing\"\n)\n\n" +
			"func TestPlain(t *testing.T) {\n\t_ = Plain(1)\n" +
			"\t_ = strings.TrimSpace(\" x \")\n}\n",
	})

	naming, err := testsNamingInFiles(worktree,
		[]string{"pkg/thing.go", "pkg/thing_test.go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(naming["Plain"]) == 0 {
		t.Error("a plain call is no longer seen")
	}
	if len(naming["TrimSpace"]) == 0 {
		t.Error("a package-qualified call is no longer seen")
	}
}

// TestAnUncalledGenericIsStillUncalled keeps the fix from being a hole.
//
// The point is to see calls that are there, not to assume one. A generic
// function nothing calls must still be reported, or the gate stops meaning
// anything for exactly the code this was about.
func TestAnUncalledGenericIsStillUncalled(t *testing.T) {
	worktree := writeWorktree(t, map[string]string{
		"fp/result.go": "package fp\n\n" +
			"// Unused is never called.\nfunc Unused[T any](v T) T { return v }\n",
		"fp/result_test.go": "package fp\n\nimport \"testing\"\n\n" +
			"func TestNothing(t *testing.T) {}\n",
	})

	naming, err := testsNamingInFiles(worktree,
		[]string{"fp/result.go", "fp/result_test.go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(naming["Unused"]) != 0 {
		t.Errorf("a generic nothing calls was counted as tested by %v",
			naming["Unused"])
	}
}

// TestBothStagesReadCallsTheSameWay is the drift that cost rung 18 twice.
//
// completeness and atom-example-tests each ask which functions the tests call,
// and each had its own scanner. Both were blind to a generic call in the same
// way, so fixing one left the other: rung 18 on 2026-08-04 was asked three
// times for a test of Err that existed, and then failed at atom-example-tests
// with "no test mentions Err, so nothing checks them on their own terms" — with
// that test in front of it, blocking two more stages behind it.
//
// They read the same function now, and this asserts they agree rather than
// trusting that they do.
func TestBothStagesReadCallsTheSameWay(t *testing.T) {
	worktree := writeWorktree(t, map[string]string{
		"fp/result.go": "package fp\n\n" +
			"type Result[T any] struct{ err error }\n\n" +
			"// Err returns a Result holding an error.\n" +
			"func Err[T any](e error) Result[T] { return Result[T]{err: e} }\n\n" +
			"// Plain does a thing.\nfunc Plain(v int) int { return v }\n",
		"fp/result_test.go": "package fp\n\n" +
			"import (\n\t\"errors\"\n\t\"testing\"\n)\n\n" +
			"func TestBoth(t *testing.T) {\n" +
			"\t_ = Err[int](errors.New(\"no\"))\n\t_ = Plain(1)\n}\n",
	})
	files := []string{"fp/result.go", "fp/result_test.go"}

	naming, err := testsNamingInFiles(worktree, files)
	if err != nil {
		t.Fatal(err)
	}
	referenced, err := testedNamesInFiles(worktree, files)
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"Err", "Plain"} {
		named := len(naming[name]) > 0
		if named != referenced[name] {
			t.Errorf("the two stages disagree about %s: completeness sees "+
				"%t, atom-example-tests sees %t", name, named, referenced[name])
		}
		if !named {
			t.Errorf("%s is called by a test and neither stage sees it", name)
		}
	}
	// And they still agree about something nothing calls.
	if len(naming["Missing"]) > 0 || referenced["Missing"] {
		t.Error("a function nothing calls was counted as called")
	}
}
