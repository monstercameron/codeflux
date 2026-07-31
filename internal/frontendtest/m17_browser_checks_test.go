package frontendtest

import (
	"errors"
	"reflect"
	"testing"
)

func TestRunIsolatedM17MountedChecksResetsAfterDirtyAndFailedChecks(t *testing.T) {
	dirty := true
	resetCalls := 0
	runs := make([]string, 0, 2)
	failures := make([]string, 0, 1)
	checks := []m17MountedCheck{
		{
			ID: "first",
			Run: func(route string) {
				if dirty {
					t.Fatal("first check inherited dirty browser state")
				}
				runs = append(runs, route+":first")
				// Simulate a failed graph/dialog assertion that exits without
				// closing its modal and leaves the application root inert.
				dirty = true
			},
		},
		{
			ID: "reset-fails",
			Run: func(string) {
				t.Fatal("check ran after its isolated page reset failed")
			},
		},
		{
			ID: "third",
			Run: func(route string) {
				if dirty {
					t.Fatal("later check inherited dirty browser state")
				}
				runs = append(runs, route+":third")
			},
		},
	}
	runIsolatedM17MountedChecks(
		checks,
		func() (string, error) {
			resetCalls++
			if resetCalls == 2 {
				return "/tasks", errors.New("reload failed")
			}
			dirty = false
			return "/tasks", nil
		},
		func(id string, route string, err error) {
			failures = append(failures, id+":"+route+":"+err.Error())
		},
	)

	if resetCalls != len(checks) {
		t.Fatalf("reset calls = %d, want %d", resetCalls, len(checks))
	}
	if want := []string{"/tasks:first", "/tasks:third"}; !reflect.DeepEqual(runs, want) {
		t.Fatalf("runs = %#v, want %#v", runs, want)
	}
	if want := []string{"reset-fails:/tasks:reload failed"}; !reflect.DeepEqual(failures, want) {
		t.Fatalf("reset failures = %#v, want %#v", failures, want)
	}
}
