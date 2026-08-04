package coordinator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/executor"
)

// TestADocumentationRoundIsNotBoundedBySize is the contradiction between two
// rules of this pipeline.
//
// atom-documentation names every undocumented declaration in a file and asks
// for a comment on each. On a small file with several of them, a full
// documentation pass is most of the lines — and the comments-only round then
// refused the patch for changing more than half the file. Ladder rung 16 on
// 2026-08-04 was refused thirteen times for changing 71% of a stats.go it had
// just been told to document.
//
// The size limits were never what made the round safe. permits applies the
// patch in memory and requires the syntax trees to be byte-identical with
// comments stripped, which no number of comment lines can defeat.
func TestADocumentationRoundIsNotBoundedBySize(t *testing.T) {
	if limits := editCommentsOnly.patchLimits(); limits != executor.UnboundedPatch {
		t.Errorf("a documentation round is bounded by proof, not by size, so "+
			"a size limit here can only refuse work the gate asked for: %+v",
			limits)
	}
}

// TestADocumentationRoundStillRefusesACodeChange is the control, and it is the
// whole reason the size limits could go.
//
// Removing them is only safe because something stronger is still checking. A
// patch that changes what the file does — not only what it says — must still be
// refused, however small it is.
func TestADocumentationRoundStillRefusesACodeChange(t *testing.T) {
	worktree := t.TempDir()
	path := "stats.go"
	source := "package stats\n\nfunc Mean(v []int) int {\n\treturn 0\n}\n"
	if err := os.WriteFile(
		filepath.Join(worktree, path), []byte(source), 0o644,
	); err != nil {
		t.Fatal(err)
	}

	// A comment above every declaration, however much of the file that is.
	documented := "package stats\n\n" +
		"// Mean returns the arithmetic mean of v, truncated toward zero. It\n" +
		"// returns zero for an empty slice, because there is no mean of no\n" +
		"// values and a panic would be a worse answer than a documented one.\n" +
		"func Mean(v []int) int {\n\treturn 0\n}\n"
	if allowed, why := editCommentsOnly.permits(
		worktree, path, []byte(documented),
	); !allowed {
		t.Errorf("a pure documentation pass was refused: %s", why)
	}

	// The same round must still refuse a change to what the code does.
	altered := "package stats\n\n// Mean returns the mean of v.\n" +
		"func Mean(v []int) int {\n\treturn 1\n}\n"
	allowed, why := editCommentsOnly.permits(worktree, path, []byte(altered))
	if allowed {
		t.Fatal("a documentation round accepted a change to what the code does")
	}
	if !strings.Contains(why, "changes what") {
		t.Errorf("the refusal should name what is wrong with it: %q", why)
	}
}

// TestATestRoundKeepsItsSizeLimits is the second control.
//
// A test round has no equivalent proof — permits only checks the path ends
// _test.go — so its size limits are the only thing bounding it and must stay.
func TestATestRoundKeepsItsSizeLimits(t *testing.T) {
	if editTestsOnly.patchLimits() == executor.UnboundedPatch {
		t.Error("a test round is bounded by size alone, so unbounding it " +
			"removes the only limit there is")
	}
}
