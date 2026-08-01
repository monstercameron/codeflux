package domain

import (
	"strings"
	"testing"
)

// FuzzParseIdentifiersNeverPanicsAndRoundTrips is M22-024's identifier fuzz.
//
// Identifiers arrive from URLs, cursors, stored rows, and untrusted client
// requests, so a parser that panics on malformed input is a denial-of-service
// and one that accepts garbage is a correctness hole. The properties proven
// here are: parsing never panics, an accepted value round-trips exactly, and
// an accepted value re-parses to itself.
func FuzzParseIdentifiersNeverPanicsAndRoundTrips(f *testing.F) {
	f.Add("")
	f.Add("tsk_019fbc20-39ff-764d-8d13-e55c7ddcc4b1")
	f.Add("prj_00000000-0000-0000-0000-000000000000")
	f.Add("tsk_")
	f.Add("tsk_zzzzzzzz-zzzz-zzzz-zzzz-zzzzzzzzzzzz")
	f.Add(strings.Repeat("t", 4096))
	f.Add("tsk_019fbc20-39ff-764d-8d13-e55c7ddcc4b1\x00")
	f.Add("TSK_019fbc20-39ff-764d-8d13-e55c7ddcc4b1")
	f.Add("../../etc/passwd")

	f.Fuzz(func(t *testing.T, raw string) {
		taskID, taskErr := ParseTaskID(raw)
		if taskErr == nil {
			if taskID.String() != raw {
				t.Fatalf("accepted task ID did not round-trip: parsed %q from %q", taskID.String(), raw)
			}
			again, err := ParseTaskID(taskID.String())
			if err != nil || again != taskID {
				t.Fatalf("re-parsing an accepted task ID failed: %v", err)
			}
			if taskID.IsZero() {
				t.Fatalf("parser accepted %q as a zero identity", raw)
			}
		}

		projectID, projectErr := ParseProjectID(raw)
		if projectErr == nil {
			if projectID.String() != raw {
				t.Fatalf("accepted project ID did not round-trip: parsed %q from %q", projectID.String(), raw)
			}
			if projectID.IsZero() {
				t.Fatalf("parser accepted %q as a zero identity", raw)
			}
		}

		// A value can never be valid as two different kinds: prefixes are
		// what keep a task ID from being mistaken for a project ID.
		if taskErr == nil && projectErr == nil {
			t.Fatalf("%q parsed as BOTH a task and a project identity", raw)
		}
	})
}
