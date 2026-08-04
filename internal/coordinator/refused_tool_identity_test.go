package coordinator

import (
	"testing"

	"codeflux.dev/codeflux/internal/executor"
)

// TestARefusedToolStillAnswersTheRequestItRefused is what keeps a refusal
// costing a round instead of the attempt.
//
// The narrator turns some calls down itself: a second whole-file rewrite, a
// patch broader than the round allows, a write outside the round's scope. Each
// is a useful thing to tell a run, and each should cost one round.
//
// The loop validates a result's identity before it will accept it — a result
// naming a different request, or no request at all, is how a confused executor
// would look — so a refusal built without the request's own ID and the schema
// version was rejected as an invalid result rather than read as a refusal. The
// loop then returned an error, and the coordinator ended the whole attempt on
// it, reported only as "tool result identity or schema does not match the
// request".
//
// Proven to discriminate: against the previous implementation these results
// carried an empty RequestID and a zero SchemaVersion. Ladder rung 4 on
// 2026-08-03 lost two attempts of one run to it, one of them the escalation to
// the most expensive rung.
func TestARefusedToolStillAnswersTheRequestItRefused(t *testing.T) {
	narrator := &narratingExecutor{}
	request := executor.AuthorizedToolRequest{
		Request: executor.ToolRequest{
			ID:            "call_abc123",
			SchemaVersion: executor.ToolSchemaVersion,
			Name:          executor.ToolApplyPatch,
		},
	}

	result := narrator.refuse(request, "that patch is broader than this round allows")

	if result.RequestID != "call_abc123" {
		t.Errorf("the refusal answers request %q, not the one it refused",
			result.RequestID)
	}
	if result.SchemaVersion != executor.ToolSchemaVersion {
		t.Errorf("the refusal declares schema version %d, not %d",
			result.SchemaVersion, executor.ToolSchemaVersion)
	}
	if result.State != "failed" {
		t.Errorf("a refusal must read as failed, got %q", result.State)
	}
	// The reason has to survive: a refusal the run cannot read is a round spent
	// learning nothing.
	if result.StdoutRedacted == "" {
		t.Error("the refusal carries no reason, so the next round is told only " +
			"that something did not work")
	}
}
