package frontendtest

import (
	"reflect"
	"testing"

	"codeflux.dev/codeflux/web/frontend/sessionprojection"
	"codeflux.dev/codeflux/web/frontend/taskprojection"
)

// TestM18ConnectionProjectionVocabularyCannotDrift guards the shared seven-
// state UI contract from docs/plan.md section 27C and M18-001. The session
// projection owns transport certainty while the task projection consumes the
// same vocabulary for action gating; neither package may silently diverge.
func TestM18ConnectionProjectionVocabularyCannotDrift(t *testing.T) {
	want := []string{
		"connecting",
		"live",
		"replaying",
		"degraded",
		"disconnected",
		"incompatible",
		"unauthorized",
	}
	sessionStates := []string{
		string(sessionprojection.ConnectionConnecting),
		string(sessionprojection.ConnectionLive),
		string(sessionprojection.ConnectionReplaying),
		string(sessionprojection.ConnectionDegraded),
		string(sessionprojection.ConnectionDisconnected),
		string(sessionprojection.ConnectionIncompatible),
		string(sessionprojection.ConnectionUnauthorized),
	}
	taskStates := make([]string, 0, len(taskprojection.AllConnectionProjections()))
	for _, state := range taskprojection.AllConnectionProjections() {
		taskStates = append(taskStates, string(state))
	}
	if !reflect.DeepEqual(sessionStates, want) {
		t.Fatalf("session connection vocabulary = %v, want %v", sessionStates, want)
	}
	if !reflect.DeepEqual(taskStates, want) {
		t.Fatalf("task-action connection vocabulary = %v, want %v", taskStates, want)
	}
}
