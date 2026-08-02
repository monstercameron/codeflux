package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/testfixtures"
	"codeflux.dev/codeflux/internal/testharness"
)

// TestAUDIT029_TheDeveloperLoopCommandsAreCallable covers AUDIT-029.
//
// M22-113 through M22-118 built the scenarios, the replay controls, the
// projection and graph comparisons, and the read-only inspection surface, and
// every one of them was reachable only from a Go test. The registry reported
// seed, replay, and inspect-db as unavailable skeletons, so the local vertical
// loop the milestone describes could not be run by a person at all.
func TestAUDIT029_TheDeveloperLoopCommandsAreCallable(t *testing.T) {
	for _, name := range []string{"seed", "replay", "inspect-db"} {
		t.Run(name+" is no longer a skeleton", func(t *testing.T) {
			for _, spec := range developmentCommandRegistry() {
				if spec.Name != name {
					continue
				}
				if spec.Availability != "implemented" {
					t.Fatalf("%s is still declared %q", name, spec.Availability)
				}
				return
			}
			t.Fatalf("%s is not in the registry", name)
		})
	}
}

// TestAUDIT029_SeedListsAndDescribesTheClosedScenarioSet exercises M22-113.
func TestAUDIT029_SeedListsAndDescribesTheClosedScenarioSet(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runSeed(&stdout, &stderr, commandInvocation{}); code != exitSuccess {
		t.Fatalf("seed exited %d: %s", code, stderr.String())
	}
	listing := stdout.String()
	for _, name := range testharness.AllScenarioNames() {
		if !strings.Contains(listing, string(name)) {
			t.Errorf("the listing omits scenario %q", name)
		}
	}

	// One scenario, as machine-readable output, must carry the fields that
	// make it reproducible rather than merely named.
	stdout.Reset()
	stderr.Reset()
	if code := runSeed(&stdout, &stderr, commandInvocation{
		JSON: true, Positional: []string{"worker-crash"},
	}); code != exitSuccess {
		t.Fatalf("seed worker-crash exited %d: %s", code, stderr.String())
	}
	var described map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &described); err != nil {
		t.Fatalf("seed --json did not emit one object: %v", err)
	}
	for _, field := range []string{
		"name", "summary", "repository_state", "expected_outcome", "event_kinds",
	} {
		if _, present := described[field]; !present {
			t.Errorf("the description omits %q", field)
		}
	}

	// An unknown scenario is a usage error naming the closed set, not a
	// silent success.
	stdout.Reset()
	stderr.Reset()
	if code := runSeed(&stdout, &stderr, commandInvocation{
		Positional: []string{"no-such-scenario"},
	}); code != exitUsage {
		t.Fatalf("an unknown scenario exited %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "worker-crash") {
		t.Error("the refusal does not name the scenarios that do exist")
	}
}

// TestAUDIT029_ReplayAppliesASessionAndRefusesABadOne exercises M22-114.
func TestAUDIT029_ReplayAppliesASessionAndRefusesABadOne(t *testing.T) {
	root := t.TempDir()

	clean := filepath.Join(root, "clean.json")
	writeFixtureFile(t, clean, `{
	  "name": "clean", "redacted": true,
	  "snapshot": {"through_sequence": 0},
	  "events": [
	    {"sequence": 1, "kind": "task-created", "revision": 1},
	    {"sequence": 2, "kind": "plan-created", "revision": 2}
	  ]
	}`)

	var stdout, stderr bytes.Buffer
	if code := runReplay(&stdout, &stderr, commandInvocation{
		JSON: true, Positional: []string{clean},
	}); code != exitSuccess {
		t.Fatalf("replay exited %d: %s", code, stderr.String())
	}
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("replay --json did not emit one object: %v", err)
	}
	if result["applied"].(float64) != 2 {
		t.Errorf("replay applied %v events, want 2", result["applied"])
	}

	// A fixture carrying credential material is refused before it is read as
	// a session. This is the property that lets a recorded session be shared.
	poisoned := filepath.Join(root, "poisoned.json")
	writeFixtureFile(t, poisoned, `{
	  "name": "poisoned", "redacted": true,
	  "snapshot": {"through_sequence": 0},
	  "events": [
	    {"sequence": 1, "kind": "task-created", "revision": 1,
	     "payload": {"note": "`+testfixtures.FixtureCredentialMaterial+`"}}
	  ]
	}`)
	stdout.Reset()
	stderr.Reset()
	if code := runReplay(&stdout, &stderr, commandInvocation{
		Positional: []string{poisoned},
	}); code == exitSuccess {
		t.Fatal("replay accepted a fixture containing credential material")
	}
	if !strings.Contains(stderr.String(), "credential material") {
		t.Errorf("the refusal does not say why: %s", stderr.String())
	}

	// A missing path is a usage error, not a crash.
	stdout.Reset()
	stderr.Reset()
	if code := runReplay(&stdout, &stderr, commandInvocation{}); code != exitUsage {
		t.Fatalf("replay with no fixture exited %d, want %d", code, exitUsage)
	}
}

// TestAUDIT029_InspectDBReportsAnUnmigratedDatabaseAsAState exercises M22-118.
//
// The first database a developer points this at is usually unmigrated, and
// reporting that as "no such table: tasks" sends them looking for a schema bug
// that is not there.
func TestAUDIT029_InspectDBReportsAnUnmigratedDatabaseAsAState(t *testing.T) {
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := runInspectDB(t.Context(), &stdout, &stderr, commandInvocation{
		Positional: []string{filepath.Join(root, "fresh.sqlite3")},
	})
	if code == exitSuccess {
		t.Fatal("inspect-db reported success against a database with no schema")
	}
	if !strings.Contains(stderr.String(), "no application schema") {
		t.Errorf("the message does not name the actual state: %s", stderr.String())
	}

	// And a path argument is required rather than defaulted to a real one.
	stdout.Reset()
	stderr.Reset()
	if code := runInspectDB(t.Context(), &stdout, &stderr, commandInvocation{}); code != exitUsage {
		t.Fatalf("inspect-db with no path exited %d, want %d", code, exitUsage)
	}
}

func writeFixtureFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}
