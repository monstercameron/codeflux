package coordinator

import (
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/executor"
)

// TestTheUnchangedSuiteNoteSaysWhyItIsUnchanged is the distinction the note did
// not draw.
//
// A suite answered from the last run means the produced files are byte for byte
// what they were. There are two ways to arrive there and they need opposite
// responses. Nothing was written, in which case a write really did fail to land
// and the paths are worth checking. Or writes landed and undid each other, in
// which case the paths are fine and the run needs to stop moving the same lines
// around.
//
// Proven to discriminate: against the previous implementation the second case
// records the first case's text. Ladder rung 9 on 2026-08-03 spent attempt 2
// doing exactly that — a guard added at 62.3s, removed at 65.5s, added at
// 72.4s, removed at 74.7s, all four patches applying cleanly — and was told
// twice that its write had not landed.
func TestTheUnchangedSuiteNoteSaysWhyItIsUnchanged(t *testing.T) {
	nothingWritten := unchangedTestNote(0)
	if !strings.Contains(nothingWritten, "did not land") {
		t.Errorf("with no writes since the last run, a failed write is the "+
			"only explanation and the note should say so, got %q",
			nothingWritten)
	}

	writesCancelled := unchangedTestNote(4)
	if strings.Contains(writesCancelled, "did not land") {
		t.Fatalf("four writes landed, so sending the run to check its paths "+
			"and its content sends it after a failure that did not happen: %q",
			writesCancelled)
	}
	if !strings.Contains(writesCancelled, "undid each other") {
		t.Errorf("the note should name what actually happened, got %q",
			writesCancelled)
	}
	if !strings.Contains(writesCancelled, "4 writes") {
		t.Errorf("the count is the evidence that the writes landed, got %q",
			writesCancelled)
	}
	if got := unchangedTestNote(1); !strings.Contains(got, "1 write landed") {
		t.Errorf("a single write should read as one, got %q", got)
	}
}

// TestBothWriteToolsCountAsWrites is the drift this depends on not having.
//
// The no-op detection, the staleness flag and the write counter all asked
// "== apply-edit", written when that was the only write tool. apply-patch was
// added and none of them were revisited — so from the second attempt onward,
// which is when every run switches to patching, none of those three saw a
// single write.
func TestBothWriteToolsCountAsWrites(t *testing.T) {
	for _, tool := range []executor.ToolName{
		executor.ToolApplyEdit, executor.ToolApplyPatch,
	} {
		if !isWriteTool(tool) {
			t.Errorf("%s changes produced files, so a run using it must be "+
				"seen to be writing", tool)
		}
	}
	for _, tool := range []executor.ToolName{
		executor.ToolTest, executor.ToolReadFile, executor.ToolBuild,
	} {
		if isWriteTool(tool) {
			t.Errorf("%s changes nothing, so counting it as a write would "+
				"mark the last test run stale for no reason", tool)
		}
	}
}
