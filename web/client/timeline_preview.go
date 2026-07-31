//go:build js && wasm

package main

import (
	"context"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/shell"
	"codeflux.dev/codeflux/web/frontend/timelinecard"
	"codeflux.dev/codeflux/web/frontend/timelineview"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// livePreviewTimeline supplies the interactive local preview fallback. The
// same callback seam accepts authoritative page, replay, review, and graph
// inspection owners when a selected coordinator session is available.
func livePreviewTimeline() shell.TimelineControlProps {
	reviewOpen := ui.UseState(false)
	hasOlder := ui.UseState(true)
	taskID, taskErr := domain.ParseTaskID("tsk_" + previewComposerUUID)
	approvalID, approvalErr := domain.ParseApprovalID("apr_" + previewComposerUUID)
	approvalState := ui.UseState(approvalPreviewState{
		Approval: timelinecard.Approval{
			ID:            approvalID.String(),
			Action:        "Write generated frontend files",
			SafeArguments: "web/frontend and web/client",
			Scope:         "repository:current-task",
			Reason:        "The requested implementation changes tracked source files.",
			Consequences:  "The worktree will contain reviewable Go source changes.",
			ExpiresAt:     time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC),
			State:         "pending",
		},
		TransportMode: "authoritative-bridge-with-local-preview-fallback",
	})
	focus := ui.UseFocusManager()
	resolveApproval := func(rawID string, action timelinecard.ApprovalAction) {
		current := approvalState.Get()
		if taskErr != nil || approvalErr != nil || current.Busy ||
			!current.Approval.ActionsAvailable() || rawID != current.Approval.ID {
			return
		}
		keyID, err := domain.NewApprovalID()
		if err != nil {
			return
		}
		current.Busy = true
		current.CommandKey = keyID.String()
		approvalState.Set(current)
		command := approvalCommand{
			TaskID: taskID, ApprovalID: approvalID, Key: current.CommandKey,
			Action: action, Scope: current.Approval.Scope,
		}
		ui.SafeGo("resolve mounted timeline approval", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			_, transportErr := resolveApprovalCommand(ctx, command)
			cancel()
			next := approvalState.Get()
			if next.CommandKey != command.Key {
				return
			}
			next, _, _ = settleApprovalPreview(next, action, transportErr, time.Now().UTC())
			approvalState.Set(next)
			// Resolution success and transport failure both replace the busy
			// controls. Restore focus to the stable status card so keyboard
			// users do not fall back to the document body.
			time.Sleep(20 * time.Millisecond)
			focus.FocusByID(timelineview.ApprovalFocusTargetID(rawID))
		})
	}
	currentApproval := approvalState.Get()
	return shell.TimelineControlProps{
		Cards: []timelinecard.Card{{
			Kind:       timelinecard.KindApproval,
			Sequence:   7,
			StableKey:  currentApproval.Approval.ID,
			OccurredAt: time.Date(2026, 7, 31, 12, 7, 0, 0, time.UTC),
			Approval:   &currentApproval.Approval,
		}},
		Actions: timelineview.Actions{
			OnApproval: resolveApproval,
			ApprovalCommand: func(rawID string) timelineview.ApprovalCommandState {
				view := approvalState.Get()
				if rawID != view.Approval.ID {
					return timelineview.ApprovalCommandState{}
				}
				return timelineview.ApprovalCommandState{
					Busy: view.Busy, IdempotencyKey: view.CommandKey,
					TransportMode: view.TransportMode,
				}
			},
		},
		Enabled:       true,
		HasOlder:      hasOlder.Get(),
		ReviewOpen:    reviewOpen.Get(),
		OnLoadOlder:   func() { hasOlder.Set(false) },
		OnOpenReview:  func() { reviewOpen.Set(true) },
		OnCloseReview: func() { reviewOpen.Set(false) },
	}
}
