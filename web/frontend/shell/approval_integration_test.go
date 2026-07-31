package shell_test

import (
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/web/frontend/routes"
	"codeflux.dev/codeflux/web/frontend/shell"
	"codeflux.dev/codeflux/web/frontend/timelinecard"
	"codeflux.dev/codeflux/web/frontend/timelineview"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestAppRootMountsTypedApprovalActionsInConversationTimeline(t *testing.T) {
	approval := timelinecard.Approval{
		ID:     "apr_01890f3c-4a00-7abc-8def-0123456789ab",
		Action: "Write generated frontend files", SafeArguments: "web/frontend",
		Scope: "repository:current-task", Reason: "Tracked source changes",
		Consequences: "Reviewable worktree changes", State: "pending",
		ExpiresAt: time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC),
	}
	markup := render(t, ui.CreateElement(shell.AppRoot, shell.RootProps{
		Snapshot: readySnapshot(), Route: routes.Route{Name: routes.ThreadWorkspace},
		Tokens: tokens(t), Composer: mountedComposerProps(t),
		Timeline: shell.TimelineControlProps{
			Cards: []timelinecard.Card{{
				Kind: timelinecard.KindApproval, Sequence: 7, StableKey: approval.ID,
				OccurredAt: time.Date(2026, 7, 31, 12, 7, 0, 0, time.UTC), Approval: &approval,
			}},
			Actions: timelineview.Actions{
				OnApproval: func(string, timelinecard.ApprovalAction) {},
				ApprovalCommand: func(string) timelineview.ApprovalCommandState {
					return timelineview.ApprovalCommandState{
						IdempotencyKey: "approval-command-1",
						TransportMode:  "authoritative-bridge-with-local-preview-fallback",
					}
				},
			},
		},
	}))
	for _, want := range []string{
		`data-component="conversation-timeline"`,
		`data-card-kind="approval"`,
		`data-component="approval-card-interaction"`,
		`data-approval-state="pending"`,
		`data-command-key="approval-command-1"`,
		`data-transport-mode="authoritative-bridge-with-local-preview-fallback"`,
		"Allow once", "Allow for this task", "Deny",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("mounted approval missing %q: %s", want, markup)
		}
	}
}
