package coordinator

import "testing"

// TestASuccessfulTestRunIsNotSummarisedAsNoTestFiles is the readability fix
// this needed.
//
// Go prints a package's status before it prints the package that matters, so a
// passing `go test ./...` in a module whose root holds no tests summarised as
// "? codeflux.test/workspace [no test files]" — a sentence that reads as
// "nothing ran" for a run whose tests had just passed. It was misread as a
// failure twice on 2026-08-03.
func TestASuccessfulTestRunIsNotSummarisedAsNoTestFiles(t *testing.T) {
	output := "?   \tcodeflux.test/workspace\t[no test files]\n" +
		"ok  \tcodeflux.test/workspace/cmd/generated\t0.312s\n"
	got := succeedingLineOf(output)
	if got == "" || got[0] == '?' {
		t.Fatalf("a passing run must not be summarised by a no-test-files "+
			"notice, got %q", got)
	}
	if want := "ok  \tcodeflux.test/workspace/cmd/generated\t0.312s"; got != want {
		t.Errorf("got %q, want the ok line %q", got, want)
	}
}

// TestOutputWithNothingButNoticesStillSaysSomething is the control.
//
// A command whose entire output is package notices has nothing better to
// quote, and returning empty would leave the timeline with a bare verb.
func TestOutputWithNothingButNoticesStillSaysSomething(t *testing.T) {
	output := "?   \tcodeflux.test/workspace\t[no test files]\n"
	if got := succeedingLineOf(output); got == "" {
		t.Error("with nothing else to quote the notice is still better than " +
			"an empty summary")
	}
}
