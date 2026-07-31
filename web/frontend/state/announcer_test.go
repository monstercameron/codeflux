package state_test

import (
	"testing"
	"time"

	"codeflux.dev/codeflux/web/frontend/state"
)

func TestAnnouncerAllowsOnlyRateLimitedMeaningfulStateChanges(t *testing.T) {
	now := time.Unix(100, 0)
	announcer := state.AnnouncerState{MinimumInterval: 5 * time.Second}
	var accepted bool
	announcer, accepted = announcer.Accept(state.Announcement{
		Kind: state.AnnouncementConnection, Message: "Connection lost", At: now,
	})
	if !accepted {
		t.Fatal("first connection change was rejected")
	}
	if _, accepted = announcer.Accept(state.Announcement{
		Kind: state.AnnouncementConnection, Message: "Connection lost", At: now.Add(time.Second),
	}); accepted {
		t.Fatal("repeated connection announcement was not rate limited")
	}
	announcer, accepted = announcer.Accept(state.Announcement{
		Kind: state.AnnouncementConnection, Message: "Connection restored", At: now.Add(time.Second),
	})
	if !accepted {
		t.Fatal("connection restoration was suppressed as a repeated loss announcement")
	}
	if _, accepted = announcer.Accept(state.Announcement{
		Kind: state.AnnouncementFailure, Message: "Task failed", At: now.Add(2 * time.Second),
	}); !accepted {
		t.Fatal("task failure announcement was rejected")
	}
	if _, accepted = announcer.Accept(state.Announcement{
		Kind: "token-delta", Message: "one more token", At: now.Add(10 * time.Second),
	}); accepted {
		t.Fatal("routine token delta became an accessibility announcement")
	}
	if _, accepted = announcer.Accept(state.Announcement{
		Kind: state.AnnouncementApproval, Message: "Approval required", At: now.Add(time.Second),
	}); !accepted {
		t.Fatal("distinct meaningful state change was rejected")
	}
}
