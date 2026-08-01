package events

import (
	"encoding/json"
	"testing"
)

// FuzzReduceTaskEventsNeverPanicsAndPreservesOrdering is M22-027's event
// replay fuzz.
//
// Replay is how the system reconstructs authoritative state after a
// disconnect or restart, and docs/plan.md sets the maximum acceptable lost or
// duplicated correctness-bearing event at ZERO. The properties asserted:
// reduction never panics on arbitrary stored bytes, an accepted reduction
// never moves the sequence backwards, and an accepted reduction advances by
// exactly the number of events consumed — a gap or a repeat must be an
// error, never a silently accepted state.
func FuzzReduceTaskEventsNeverPanicsAndPreservesOrdering(f *testing.F) {
	f.Add([]byte(`{"session_id":"","thread_id":"","through_sequence":0}`), []byte(`[]`))
	f.Add([]byte(`{}`), []byte(`[]`))
	f.Add([]byte(`{}`), []byte(`[{}]`))
	f.Add([]byte(`null`), []byte(`null`))
	f.Add([]byte(`{"through_sequence":18446744073709551615}`), []byte(`[{"sequence":0}]`))
	f.Add([]byte(`{"snapshot_version":4294967295}`), []byte(`[{"sequence":1},{"sequence":1}]`))

	f.Fuzz(func(t *testing.T, snapshotJSON []byte, streamJSON []byte) {
		var base SessionSnapshot
		if err := json.Unmarshal(snapshotJSON, &base); err != nil {
			return // Malformed stored bytes are the storage layer's problem.
		}
		var stream []SessionEvent
		if err := json.Unmarshal(streamJSON, &stream); err != nil {
			return
		}

		before := base.ThroughSequence
		result, err := ReduceTaskEvents(base, stream)
		if err != nil {
			return // Refusing is always acceptable.
		}

		if result.ThroughSequence < before {
			t.Fatalf("replay moved the sequence backwards: %d -> %d", before, result.ThroughSequence)
		}
		// An accepted replay must have consumed a contiguous run.
		if result.ThroughSequence != before+uint64(len(stream)) {
			t.Fatalf(
				"replay accepted %d events but advanced from %d to %d: a gap or repeat was tolerated",
				len(stream), before, result.ThroughSequence,
			)
		}
		if result.SessionID != base.SessionID || result.ThreadID != base.ThreadID {
			t.Fatal("replay must never change the session or thread it reconstructs")
		}
		// The reduced snapshot must itself be valid, or the next replay
		// round would start from a state the system rejects.
		if err := result.Validate(); err != nil {
			t.Fatalf("replay produced an invalid snapshot: %v", err)
		}
	})
}
