package shell

import (
	"strings"
	"testing"

	"codeflux.dev/codeflux/web/frontend/state"
)

func TestAnnouncementCandidateCoversRestorationAndTaskFailureWithoutRawState(t *testing.T) {
	tests := []struct {
		name       string
		taskState  string
		connection state.ConnectionState
		wantKind   state.AnnouncementKind
		wantText   string
	}{
		{
			name: "connection restored", connection: state.ConnectionLive,
			wantKind: state.AnnouncementConnection, wantText: "Connection restored",
		},
		{
			name: "task failed", taskState: "failed: credential-like-sensitive-detail",
			connection: state.ConnectionLive,
			wantKind:   state.AnnouncementFailure, wantText: "Task failed",
		},
		{
			name: "validation failure is specific", taskState: "validation failed: private-check-detail",
			connection: state.ConnectionLive,
			wantKind:   state.AnnouncementValidationFailure, wantText: "Validation failed",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := state.NewStore(state.NewSnapshot(nil, nil, nil))
			store = store.ReduceRemote(state.SessionChanged{Session: state.SessionView{
				Bootstrap: state.BootstrapReady, Connection: test.connection,
			}})
			store = store.ReduceRemote(state.TopBarChanged{TopBar: state.TopBarView{
				TaskState: test.taskState,
			}})
			got := announcementCandidate(store.Snapshot())
			if got.Kind != test.wantKind || got.Message != test.wantText {
				t.Fatalf("announcementCandidate() = %#v, want kind %q text %q", got, test.wantKind, test.wantText)
			}
			if test.taskState != "" && strings.Contains(got.Message, test.taskState) {
				t.Fatalf("announcement exposed raw task state %q", test.taskState)
			}
		})
	}
}
