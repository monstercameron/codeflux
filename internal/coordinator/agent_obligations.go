package coordinator

import (
	"runtime"
	"strings"
)

// This file is the single answer to "what does the flow require of this
// function". It exists because there were two answers.
//
// The refinement loop asked one question and the ledger asked another, and the
// ledger's was stricter. So a run was told its work was finished, and the
// record then said three stages had failed — for functions the run was never
// asked about and, in one case, could not have been asked about. A ledger that
// accuses a run of something no gate can raise is worse than one that says
// nothing: the reader cannot tell an unfixed defect from an unaskable one.
//
// Every rule below is used by both the gate that sends work back and the stage
// that records the verdict. They cannot disagree because there is one of each.

// isTestScaffolding reports whether a function is part of the checking
// apparatus rather than of the program.
//
// Anything declared in a _test.go file is scaffolding: helpers, stubs, fakes,
// and the tests themselves. The previous rule caught only the Test, Fuzz and
// Benchmark prefixes, so a failing-writer stub declared beside them counted as
// program code — and the flow demanded a test for the test helper and a doc
// comment on it. Rung 5 spent three of its six attempts on that, and the
// function under discussion was a four-line stub whose whole purpose was to
// return an error.
//
// The suite is what exercises scaffolding, and the suite is already run.
func isTestScaffolding(function producedFunction) bool {
	return function.IsTest || strings.HasSuffix(function.File, "_test.go")
}

// needsOwnTest reports whether a function owes a test that calls it directly.
//
// main is exempt because running the program is what exercises it, and the
// acceptance examples do exactly that. A test that calls main directly would
// be testing the process, not the code.
func needsOwnTest(function producedFunction) bool {
	return !isTestScaffolding(function) && function.Name != "main"
}

// needsDocComment reports whether a function owes a doc comment.
//
// main is not exempt. It was exempt in the gate and required by the ledger,
// which is the exact contradiction this file exists to end: the record kept
// reporting an undocumented main that the loop was forbidden from asking for,
// so no number of attempts could ever have closed it. Of the two rules,
// requiring it is the one worth keeping — main is where a reader starts, and a
// program whose entry point says nothing about what it does is the least
// helpful place for the flow to make an exception.
func needsDocComment(function producedFunction) bool {
	return !isTestScaffolding(function)
}

// needsVerification reports whether a function must be proven by a test that
// names it, which is the stricter claim behind the atom and molecule
// verification stages.
//
// It is deliberately the same predicate as needsOwnTest. The two stages ask
// the same question with different evidence — one that a test exists, one that
// the test names the function — and they must at least agree about which
// functions are in scope, or the stricter one accuses work the weaker one
// never asked about.
func needsVerification(function producedFunction) bool {
	return needsOwnTest(function)
}

// builtBinaryName is what a produced command must be written to on this
// platform in order to be runnable.
//
// Windows will not execute a file whose extension is not in PATHEXT, and
// go build writes exactly the path it is given, so a binary built as "ledger"
// exists on disk and cannot be started. Every caller that builds a produced
// command and then runs it needs this, and until now none of them had it:
// only the rung test appended the extension, so the checks that matter most
// were the ones that could not run anything.
//
// The bug hid behind another. The acceptance check returned early on a
// spurious ambiguity, so it never reached the build; the adversarial probe
// reached it, failed to start every binary, and recorded that as survival.
func builtBinaryName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}
