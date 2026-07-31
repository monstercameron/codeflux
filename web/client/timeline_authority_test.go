package main

import (
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/shell"
	"codeflux.dev/codeflux/web/frontend/taskprojection"
	"codeflux.dev/codeflux/web/frontend/timelinecard"
	"codeflux.dev/codeflux/web/frontend/timelineview"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestAuthoritativeTimelineApprovalActionsRemainDisabledWithExactDependency(t *testing.T) {
	approvalID, err := domain.ParseApprovalID("apr_01890f3c-4a00-7abc-8def-0123456789ab")
	if err != nil {
		t.Fatal(err)
	}
	task := taskprojection.TaskProjection{
		State: domain.TaskStateAwaitingAuthority,
		Approval: taskprojection.ApprovalProjection{
			Present: true, ID: approvalID, State: domain.ApprovalRequestStatePending,
		},
	}
	props := bindAuthoritativeTimelineActions(
		shell.TimelineControlProps{}, task, taskprojection.ConnectionLive, nil, nil,
	)
	for _, action := range []timelinecard.ApprovalAction{
		timelinecard.ApprovalAllowOnce, timelinecard.ApprovalAllowForTask, timelinecard.ApprovalDeny,
	} {
		command := props.Actions.ApprovalActionCommand(approvalID.String(), action)
		if command.DisabledReason != approvalResolutionDependency || command.TransportMode != "authoritative-disabled" {
			t.Fatalf("%s command = %#v", action, command)
		}
	}
	markup := renderAuthoritativeTimelineCard(t, timelinecard.Card{
		Kind: timelinecard.KindApproval, Sequence: 1, StableKey: "approval:" + approvalID.String(),
		OccurredAt: time.Date(2026, 7, 31, 18, 0, 0, 0, time.UTC),
		Approval: &timelinecard.Approval{
			ID: approvalID.String(), Action: "write repository files", Scope: "task worktree",
			Reason: "A scoped tool action needs authority.", State: "pending",
		},
	}, props.Actions)
	for _, want := range []string{
		`aria-label="Allow once"`, `aria-label="Allow for this task"`, `aria-label="Deny"`,
		`aria-describedby="timeline-approval-` + approvalID.String() + `-allow-once-reason"`,
		`data-transport-mode="authoritative-disabled"`, approvalResolutionDependency,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("mounted approval missing %q: %s", want, markup)
		}
	}

	disconnected := bindAuthoritativeTimelineActions(
		shell.TimelineControlProps{}, task, taskprojection.ConnectionDisconnected, nil, nil,
	)
	if reason := disconnected.Actions.ApprovalActionCommand(
		approvalID.String(), timelinecard.ApprovalDeny,
	).DisabledReason; !strings.Contains(reason, "Disconnected") {
		t.Fatalf("disconnected approval reason = %q", reason)
	}
}

func TestAuthoritativeTimelinePlanActionsBindCurrentRevisionAndStayDisabled(t *testing.T) {
	task := taskprojection.TaskProjection{
		State: domain.TaskStateAwaitingPlanApproval,
		Plan: taskprojection.PlanProjection{
			Present: true, Revision: 7, Approval: domain.ApprovalRequestStatePending,
		},
	}
	props := bindAuthoritativeTimelineActions(
		shell.TimelineControlProps{}, task, taskprojection.ConnectionLive, nil, nil,
	)
	if got := props.Actions.ApprovePlanCommand(7).DisabledReason; got != planApprovalDependency {
		t.Fatalf("approve reason = %q", got)
	}
	if got := props.Actions.PlanChangeCommand(7).DisabledReason; got != planChangeDependency {
		t.Fatalf("change reason = %q", got)
	}
	if got := props.Actions.ApprovePlanCommand(6).DisabledReason; !strings.Contains(got, "no longer current") {
		t.Fatalf("stale plan reason = %q", got)
	}
	markup := renderAuthoritativeTimelineCard(t, timelinecard.Card{
		Kind: timelinecard.KindPlan, Sequence: 2, StableKey: "plan:7",
		OccurredAt: time.Date(2026, 7, 31, 18, 1, 0, 0, time.UTC),
		Plan:       &timelinecard.Plan{Revision: 7, Summary: "Implement the reviewed change", ApprovalPending: true},
	}, props.Actions)
	for _, want := range []string{
		`id="timeline-plan-approve"`, `aria-describedby="timeline-plan-approve-reason"`,
		`id="timeline-plan-request-change"`, planApprovalDependency, planChangeDependency,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("mounted plan missing %q: %s", want, markup)
		}
	}
}

func TestAuthoritativeReviewOpensReadOnlyAndShowsExactDisabledDecisionBindings(t *testing.T) {
	bindings := taskprojection.RevisionBindings{Diff: 3, Plan: 4, Validation: 5, Evidence: 6, Graph: 7}
	task := taskprojection.TaskProjection{
		State: domain.TaskStateAwaitingReview, Revision: 11,
		Review: taskprojection.ReviewProjection{Present: true, Revision: 2, Bindings: bindings},
		Acceptance: taskprojection.AcceptanceProjection{
			Present: true, State: domain.ChangeAcceptanceStatePending, Revision: 1, Bindings: bindings,
		},
		Validation: taskprojection.ValidationProjection{
			Present: true, Required: true, State: domain.ValidationStatePassed, Revision: 5,
		},
	}
	opened, closed := 0, 0
	props := bindAuthoritativeTimelineActions(
		shell.TimelineControlProps{Enabled: true}, task, taskprojection.ConnectionLive,
		func() { opened++ }, func() { closed++ },
	)
	if props.OnOpenReview == nil || props.OnCloseReview == nil || props.Actions.OnOpenReview == nil {
		t.Fatal("authoritative read-only review callbacks are unavailable")
	}
	props.OnOpenReview()
	props.Actions.OnOpenReview("review-2")
	props.OnCloseReview()
	if opened != 2 || closed != 1 {
		t.Fatalf("open=%d close=%d", opened, closed)
	}
	props.ReviewOpen = true
	markup := renderAuthoritativeTimelineControls(t, props)
	for _, want := range []string{
		`data-component="review-drawer"`, `data-focus-policy="trap-restore"`,
		`data-component="review-revision-bindings"`, `>Task revision</dt><dd>11<`,
		`>Diff revision</dt><dd>3<`, `>Plan revision</dt><dd>4<`,
		`>Validation revision</dt><dd>5<`, `>Evidence revision</dt><dd>6<`,
		`id="review-command-accept"`, `id="review-command-repair"`,
		`id="review-command-reject"`, `id="review-command-rollback"`,
		reviewAcceptDependency, reviewRepairDependency, reviewRejectDependency, reviewRollbackDependency,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("mounted review missing %q: %s", want, markup)
		}
	}
}

func TestAuthoritativeReviewUsesStaleDecisionBeforeMissingBackendReason(t *testing.T) {
	reviewBindings := taskprojection.RevisionBindings{Diff: 2, Plan: 3, Validation: 4, Evidence: 5, Graph: 6}
	acceptedBindings := reviewBindings
	acceptedBindings.Diff++
	task := taskprojection.TaskProjection{
		State:  domain.TaskStateAwaitingReview,
		Review: taskprojection.ReviewProjection{Present: true, Revision: 2, Bindings: reviewBindings},
		Acceptance: taskprojection.AcceptanceProjection{
			Present: true, State: domain.ChangeAcceptanceStatePending, Revision: 1, Bindings: acceptedBindings,
		},
		Validation: taskprojection.ValidationProjection{
			Present: true, Required: true, State: domain.ValidationStatePassed,
		},
	}
	props := bindAuthoritativeTimelineActions(
		shell.TimelineControlProps{}, task, taskprojection.ConnectionLive, func() {}, func() {},
	)
	if reason := props.ReviewDecisions.Accept.DisabledReason; !strings.Contains(reason, "changed") {
		t.Fatalf("stale acceptance reason = %q", reason)
	}
	if reason := props.ReviewDecisions.Repair.DisabledReason; reason != reviewRepairDependency {
		t.Fatalf("repair reason = %q", reason)
	}
}

func renderAuthoritativeTimelineCard(
	t *testing.T,
	card timelinecard.Card,
	actions timelineview.Actions,
) string {
	t.Helper()
	markup, err := ui.RenderToString(ui.CreateElement(timelineview.Renderer, timelineview.Props{
		Card: card, Actions: actions,
	}))
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func renderAuthoritativeTimelineControls(t *testing.T, props shell.TimelineControlProps) string {
	t.Helper()
	markup, err := ui.RenderToString(ui.CreateElement(shell.TimelineControls, props))
	if err != nil {
		t.Fatal(err)
	}
	return markup
}
