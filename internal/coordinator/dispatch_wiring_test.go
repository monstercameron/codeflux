package coordinator

import (
	"os"
	"strings"
	"testing"
)

// TestAUDIT018a_TheApplicationWiresEveryDispatchNotifier covers the half of
// AUDIT-018a that a behavioural test cannot reach.
//
// The pump's own tests inject a callback directly, which proves the transition
// signals whatever it was given. It cannot prove the application gave it
// anything: with the wiring absent, every one of those tests still passes and
// production silently falls back to the two-second tick — which is the exact
// defect AUDIT-018a exists to remove, restored without breaking a single test.
// That is why AUDIT-018 shipped with the completion observer wired and the
// pause and approval transitions not: nothing failed.
//
// Constructing a real Application here would need a database, a supervisor, and
// a scheduler, and would test startup rather than this binding. Reading the
// construction site is weaker evidence than observing a signal, and it is the
// evidence available; the limitation is stated rather than implied.
func TestAUDIT018a_TheApplicationWiresEveryDispatchNotifier(t *testing.T) {
	source, err := os.ReadFile("application.go")
	if err != nil {
		t.Fatalf("read application.go: %v", err)
	}
	body := string(source)

	required := []struct {
		fragment   string
		transition string
	}{
		{
			"application.runtime.SetDispatchNotifier(application.notifyDispatch)",
			"a restored run becomes eligible at registration, which is not a start",
		},
		{
			"application.taskLifecycle.SetDispatchNotifier(application.notifyDispatch)",
			"granting an approval unblocks the task the approval was blocking",
		},
		{
			"application.notifyDispatch,",
			"lifting a pause makes the paused task startable again, threaded " +
				"into newApplicationTaskControls",
		},
	}
	for _, requirement := range required {
		if strings.Contains(body, requirement.fragment) {
			continue
		}
		t.Errorf("application.go no longer wires the dispatch pump for %s\n"+
			"  expected to find: %s\n"+
			"Without it the transition still records durably and the work it "+
			"unblocks waits for the pump's fallback tick instead of starting. "+
			"Every pump test keeps passing, so restore the wiring rather than "+
			"deleting this assertion",
			requirement.transition, requirement.fragment)
	}
}
