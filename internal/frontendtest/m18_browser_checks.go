package frontendtest

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/composer"
	"codeflux.dev/codeflux/web/frontend/routes"
	"codeflux.dev/codeflux/web/frontend/taskactionview"
	"codeflux.dev/codeflux/web/frontend/taskprojection"
	"github.com/mxschmitt/playwright-go"
)

func exerciseMountedM18TaskActionMatrixAcceptance(t *testing.T, page playwright.Page) {
	t.Helper()
	fixture := page.Locator(`[data-component="m18-task-action-matrix"]`)
	if err := browserAssertions().Locator(fixture).ToBeVisible(); err != nil {
		t.Fatalf("wait for mounted authoritative task matrix: %v", err)
	}
	expected := map[domain.TaskState][]taskprojection.ActionKind{
		domain.TaskStateDraft:                {taskprojection.ActionSend, taskprojection.ActionChangePolicy, taskprojection.ActionChangeBudget},
		domain.TaskStateForecasting:          {taskprojection.ActionStop},
		domain.TaskStateAwaitingPlanApproval: {taskprojection.ActionApprovePlan, taskprojection.ActionRequestChange, taskprojection.ActionStop},
		domain.TaskStateReady:                {taskprojection.ActionStart, taskprojection.ActionChangeBudget, taskprojection.ActionStop},
		domain.TaskStateRunning:              {taskprojection.ActionPause, taskprojection.ActionStop, taskprojection.ActionInspectGraph},
		domain.TaskStateAwaitingAuthority:    {taskprojection.ActionAllowOnce, taskprojection.ActionAllowForTask, taskprojection.ActionDeny, taskprojection.ActionStop},
		domain.TaskStatePaused:               {taskprojection.ActionResume, taskprojection.ActionChangeBudget, taskprojection.ActionReview, taskprojection.ActionStop},
		domain.TaskStateValidating:           {taskprojection.ActionPause, taskprojection.ActionStop, taskprojection.ActionInspectChecks},
		domain.TaskStateAwaitingReview:       {taskprojection.ActionReview, taskprojection.ActionAccept, taskprojection.ActionRepair, taskprojection.ActionReject, taskprojection.ActionRollback},
		domain.TaskStateRecoveryRequired:     {taskprojection.ActionSafeResume, taskprojection.ActionReconcile, taskprojection.ActionPreservePatch, taskprojection.ActionAbandon},
		domain.TaskStateCompleted:            {taskprojection.ActionInspectEvidence, taskprojection.ActionStartRelatedTask},
		domain.TaskStateFailed:               {taskprojection.ActionInspect, taskprojection.ActionRepair, taskprojection.ActionPreservePatch},
		domain.TaskStateCancelled:            {taskprojection.ActionInspect, taskprojection.ActionPreservePatch, taskprojection.ActionNewAttempt},
		domain.TaskStateRolledBack:           {taskprojection.ActionResumeNewPlan, taskprojection.ActionFinish},
	}
	supported := map[taskprojection.ActionKind]bool{
		taskprojection.ActionChangeBudget:  true,
		taskprojection.ActionStop:          true,
		taskprojection.ActionPause:         true,
		taskprojection.ActionInspectGraph:  true,
		taskprojection.ActionResume:        true,
		taskprojection.ActionReview:        true,
		taskprojection.ActionReconcile:     true,
		taskprojection.ActionPreservePatch: true,
	}
	stateSelector := page.GetByTestId("m18-matrix-state")
	for _, taskState := range []domain.TaskState{
		domain.TaskStateDraft,
		domain.TaskStateForecasting,
		domain.TaskStateAwaitingPlanApproval,
		domain.TaskStateReady,
		domain.TaskStateRunning,
		domain.TaskStateAwaitingAuthority,
		domain.TaskStatePaused,
		domain.TaskStateValidating,
		domain.TaskStateAwaitingReview,
		domain.TaskStateRecoveryRequired,
		domain.TaskStateCompleted,
		domain.TaskStateFailed,
		domain.TaskStateCancelled,
		domain.TaskStateRolledBack,
	} {
		if _, err := stateSelector.SelectOption(playwright.SelectOptionValues{Values: &[]string{string(taskState)}}); err != nil {
			t.Fatalf("select authoritative matrix row %s: %v", taskState, err)
		}
		if err := browserAssertions().Locator(fixture).ToHaveAttribute("data-task-state", string(taskState)); err != nil {
			t.Fatalf("mounted authoritative row %s: %v", taskState, err)
		}
		taskGroup := fixture.Locator(`[aria-label="Task actions"]`)
		if got, err := taskGroup.GetAttribute("data-task-message"); err != nil || got != composer.AvailableTaskActions(taskState).PrimaryMessage {
			t.Fatalf("%s primary message = %q, error=%v", taskState, got, err)
		}

		if taskState == domain.TaskStateDraft {
			assertMountedMatrixDraftActions(t, fixture)
		}
		matrixActions := make([]taskprojection.ActionKind, 0, len(expected[taskState]))
		for _, action := range expected[taskState] {
			if action != taskprojection.ActionSend && action != taskprojection.ActionChangePolicy {
				matrixActions = append(matrixActions, action)
			}
		}
		if count, err := fixture.Locator(`[data-task-action]`).Count(); err != nil || count != len(matrixActions) {
			t.Fatalf("%s mounted actions=%d want=%d error=%v", taskState, count, len(matrixActions), err)
		}
		for _, action := range matrixActions {
			ensureMountedMatrixActionsOpen(t, fixture)
			button := fixture.Locator("#composer-task-action-" + string(action))
			span := fixture.Locator(`[data-task-action="` + string(action) + `"]`)
			reason, err := span.GetAttribute("data-disabled-reason")
			if err != nil {
				t.Fatalf("%s %s disabled reason: %v", taskState, action, err)
			}
			enabled := supported[action]
			if action == taskprojection.ActionSafeResume {
				enabled = false
				if !strings.Contains(reason, "recovery assessment") {
					t.Fatalf("%s safe-resume reason=%q", taskState, reason)
				}
			}
			if enabled {
				assertMountedMatrixActionInvocation(t, fixture, button, action)
				continue
			}
			if err := browserAssertions().Locator(button).ToBeDisabled(); err != nil {
				t.Fatalf("%s %s must be disabled before click: %v", taskState, action, err)
			}
			if strings.TrimSpace(reason) == "" {
				t.Fatalf("%s %s has no accessible disabled reason", taskState, action)
			}
			if action != taskprojection.ActionSafeResume && reason != taskactionview.UnavailableReason(action) {
				t.Fatalf("%s %s reason=%q want=%q", taskState, action, reason, taskactionview.UnavailableReason(action))
			}
			describedBy, err := button.GetAttribute("aria-describedby")
			if err != nil || strings.TrimSpace(describedBy) == "" {
				t.Fatalf("%s %s aria-describedby=%q error=%v", taskState, action, describedBy, err)
			}
			if text, err := fixture.Locator("#" + describedBy).TextContent(); err != nil || strings.TrimSpace(text) != reason {
				t.Fatalf("%s %s reason text=%q want=%q error=%v", taskState, action, text, reason, err)
			}
		}
	}
}

func assertMountedMatrixDraftActions(t *testing.T, fixture playwright.Locator) {
	t.Helper()
	send := fixture.Locator("#composer-submit")
	assertMountedMatrixActionInvocation(t, fixture, send, taskprojection.ActionSend)
	options := fixture.Locator(`[data-component="composer-advanced-options"]`)
	policy := fixture.Locator("#composer-policy")
	if visible, err := policy.IsVisible(); err != nil || !visible {
		if err := options.Locator("summary").Click(playwright.LocatorClickOptions{Force: playwright.Bool(true)}); err != nil {
			t.Fatalf("open matrix policy options: %v", err)
		}
	}
	if err := policy.Focus(); err != nil {
		t.Fatalf("focus matrix policy: %v", err)
	}
	if err := browserAssertions().Locator(policy).ToBeFocused(); err != nil {
		t.Fatalf("matrix policy focus: %v", err)
	}
	before := mountedMatrixInvocationCount(t, fixture)
	if _, err := policy.SelectOption(playwright.SelectOptionValues{Values: &[]string{string(domain.PolicyPresetBalanced)}}); err != nil {
		t.Fatalf("change matrix policy: %v", err)
	}
	assertM18Attributes(t, fixture, map[string]string{
		"data-invoked-action":   string(taskprojection.ActionChangePolicy),
		"data-invocation-count": strconv.Itoa(before + 1),
	})
}

func ensureMountedMatrixActionsOpen(t *testing.T, fixture playwright.Locator) {
	t.Helper()
	details := fixture.Locator(`[data-component="composer-task-controls"]`)
	if visible, err := details.Locator(`[aria-label="Task actions"]`).IsVisible(); err != nil || !visible {
		if err := details.Locator("summary").Click(playwright.LocatorClickOptions{Force: playwright.Bool(true)}); err != nil {
			t.Fatalf("open mounted task actions: %v", err)
		}
	}
}

func assertMountedMatrixActionInvocation(
	t *testing.T,
	fixture playwright.Locator,
	button playwright.Locator,
	action taskprojection.ActionKind,
) {
	t.Helper()
	if err := browserAssertions().Locator(button).ToBeEnabled(); err != nil {
		t.Fatalf("%s enabled authoritative action: %v", action, err)
	}
	if err := browserAssertions().Locator(button).ToBeVisible(); err != nil {
		t.Fatalf("%s visible authoritative action: %v", action, err)
	}
	if err := button.Focus(); err != nil {
		t.Fatalf("focus %s before activation: %v", action, err)
	}
	before := mountedMatrixInvocationCount(t, fixture)
	if err := button.Press("Enter"); err != nil {
		t.Fatalf("activate %s: %v", action, err)
	}
	assertM18Attributes(t, fixture, map[string]string{
		"data-invoked-action":   string(action),
		"data-invocation-count": strconv.Itoa(before + 1),
	})
	if err := browserAssertions().Locator(button).ToBeFocused(); err != nil {
		t.Fatalf("%s focus after activation: %v", action, err)
	}
}

func mountedMatrixInvocationCount(t *testing.T, fixture playwright.Locator) int {
	t.Helper()
	raw, err := fixture.GetAttribute("data-invocation-count")
	if err != nil {
		t.Fatalf("read matrix invocation count: %v", err)
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		t.Fatalf("parse matrix invocation count %q: %v", raw, err)
	}
	return value
}

// exerciseMountedM18SessionAcceptance drives the Go/WASM fixture through the
// M18 reconnect contract without changing the coordinator or browser server.
func exerciseMountedM18SessionAcceptance(t *testing.T, page playwright.Page) {
	t.Helper()
	fixture := page.Locator(`[data-component="m18-session-fixture"]`)
	if err := browserAssertions().Locator(fixture).ToBeVisible(); err != nil {
		t.Fatalf("wait for M18 fixture: %v", err)
	}
	assertM18Attributes(t, fixture, map[string]string{
		"data-connection-state":  "replaying",
		"data-mutations-allowed": "false",
		"data-last-sequence":     "0",
		"data-draft":             "retained offline draft",
	})
	assertM18Diagnostics(t, fixture, 0, true, false, false)
	clickM18Fixture(t, page, "m18-open-thread")
	durableThread := fixture.Locator(`[data-component="m18-durable-thread"]`)
	if err := browserAssertions().Locator(durableThread).ToHaveAttribute("data-card-count", "0"); err != nil {
		t.Fatalf("open first durable thread: %v", err)
	}
	taskPanel := fixture.Locator(`[data-component="task-control-panel"]`)
	if err := browserAssertions().Locator(taskPanel).ToHaveAttribute("data-task-state", "running"); err != nil {
		t.Fatalf("backend task state: %v", err)
	}
	actualCost, err := taskPanel.Locator(`[data-metric="actual-cost"]`).TextContent()
	if err != nil || !strings.Contains(actualCost, "Unknown") || strings.Contains(actualCost, "$0") {
		t.Fatalf("unknown actual cost = %q, error=%v", actualCost, err)
	}
	hardBudget, err := taskPanel.Locator(`[data-budget-limit="exact"]`).TextContent()
	if err != nil || !strings.Contains(hardBudget, "USD 400 minor units") {
		t.Fatalf("hard budget = %q, error=%v", hardBudget, err)
	}
	pause := fixture.GetByRole(*playwright.AriaRoleButton, playwright.LocatorGetByRoleOptions{
		Name: "Pause task", Exact: playwright.Bool(true),
	})
	assertM18Disabled(t, pause, true, "replay")

	clickM18Fixture(t, page, "m18-disconnect")
	assertM18Attributes(t, fixture, map[string]string{
		"data-connection-state":  "disconnected",
		"data-mutations-allowed": "false",
		"data-last-sequence":     "0",
		"data-draft":             "retained offline draft",
	})
	assertM18Diagnostics(t, fixture, 0, false, false, false)
	clickM18Fixture(t, page, "m18-reconnect")
	assertM18Attributes(t, fixture, map[string]string{
		"data-connection-state":  "degraded",
		"data-retry-disposition": "automatic",
		"data-retry-attempt":     "1",
		"data-retry-delay-ms":    "10",
	})
	clickM18Fixture(t, page, "m18-replay")
	assertM18Attributes(t, fixture, map[string]string{
		"data-connection-state":  "replaying",
		"data-mutations-allowed": "false",
	})

	clickM18Fixture(t, page, "m18-apply-event")
	assertM18Attributes(t, fixture, map[string]string{
		"data-last-sequence": "1",
		"data-duplicates":    "0",
		"data-gaps":          "0",
	})
	if err := browserAssertions().Locator(durableThread).ToHaveAttribute("data-card-count", "1"); err != nil {
		t.Fatalf("first durable event card count: %v", err)
	}
	if count, err := durableThread.Locator(`[data-component="timeline-card"][data-sequence="1"]`).Count(); err != nil || count != 1 {
		t.Fatalf("first durable event cards = %d, error=%v", count, err)
	}
	clickM18Fixture(t, page, "m18-apply-duplicate")
	assertM18Attributes(t, fixture, map[string]string{
		"data-last-sequence": "1",
		"data-duplicates":    "1",
	})
	if count, err := durableThread.Locator(`[data-component="timeline-card"]`).Count(); err != nil || count != 1 {
		t.Fatalf("duplicate delivery cards = %d, error=%v", count, err)
	}

	clickM18Fixture(t, page, "m18-apply-gap")
	assertM18Attributes(t, fixture, map[string]string{
		"data-connection-state":  "degraded",
		"data-mutations-allowed": "false",
		"data-last-sequence":     "1",
		"data-gaps":              "1",
		"data-repair-required":   "true",
		"data-draft":             "retained offline draft",
	})
	assertM18Diagnostics(t, fixture, 1, false, false, true)
	assertM18Disabled(t, pause, true, "gap")
	clickM18Fixture(t, page, "m18-repair")
	assertM18Attributes(t, fixture, map[string]string{
		"data-connection-state": "replaying",
		"data-last-sequence":    "3",
		"data-repair-required":  "false",
	})
	assertM18Diagnostics(t, fixture, 3, true, false, false)
	assertM18Disabled(t, pause, true, "snapshot replay")
	clickM18Fixture(t, page, "m18-live")
	assertM18Attributes(t, fixture, map[string]string{
		"data-connection-state":  "live",
		"data-mutations-allowed": "true",
		"data-last-sequence":     "3",
	})
	assertM18Diagnostics(t, fixture, 3, false, true, false)
	assertM18Disabled(t, pause, false, "live")
	if err := pause.Click(); err != nil {
		t.Fatalf("live pause action: %v", err)
	}
	assertM18Attributes(t, fixture, map[string]string{"data-command-count": "1"})

	// A second loss after reaching live must not erase the repaired cursor,
	// durable timeline, draft, or the command already issued while authorized.
	clickM18Fixture(t, page, "m18-disconnect")
	assertM18Attributes(t, fixture, map[string]string{
		"data-connection-state":  "disconnected",
		"data-mutations-allowed": "false",
		"data-last-sequence":     "3",
		"data-draft":             "retained offline draft",
		"data-command-count":     "1",
	})
	assertM18Diagnostics(t, fixture, 3, false, false, false)
	assertM18Disabled(t, pause, true, "second disconnect")
	if count, err := durableThread.Locator(`[data-component="timeline-card"]`).Count(); err != nil || count != 1 {
		t.Fatalf("second disconnect durable cards = %d, error=%v", count, err)
	}
	t.Log(m18BrowserCheckDetail("disconnected", 3, 1, 1))
	exerciseMountedM18JourneyAcceptance(t, page)
}

func assertM18Diagnostics(
	t *testing.T,
	fixture playwright.Locator,
	sequence uint64,
	replay bool,
	live bool,
	gapRepair bool,
) {
	t.Helper()
	diagnostics := fixture.Locator(`[data-component="durable-session-sequence"]`)
	assertM18Attributes(t, diagnostics, map[string]string{
		"data-sequence":            fmt.Sprintf("%d", sequence),
		"data-sequence-known":      "true",
		"data-replay-active":       fmt.Sprintf("%t", replay),
		"data-live":                fmt.Sprintf("%t", live),
		"data-gap-repair-required": fmt.Sprintf("%t", gapRepair),
	})
}

func exerciseMountedM18JourneyAcceptance(t *testing.T, page playwright.Page) {
	t.Helper()
	fixture := page.Locator(`[data-component="m18-journey-fixture"]`)
	if err := browserAssertions().Locator(fixture).ToBeVisible(); err != nil {
		t.Fatalf("wait for M18 journey fixture: %v", err)
	}
	activateM18Stage(t, page, fixture, "first-run")
	if count, err := fixture.Locator(`[data-component="first-run-shell"] [data-region]`).Count(); err != nil || count != 5 {
		t.Fatalf("first-run steps = %d, error=%v", count, err)
	}

	activateM18Stage(t, page, fixture, "new-task")
	assertM18CardKind(t, fixture, "requirement")
	activateM18Stage(t, page, fixture, "plan-review")
	assertM18CardKind(t, fixture, "plan")
	assertM18NamedButton(t, fixture, "Approve plan", true)
	assertM18NamedButton(t, fixture, "Request plan change", true)

	activateM18Stage(t, page, fixture, "live-work")
	assertM18CardKind(t, fixture, "tool-activity")
	activateM18Stage(t, page, fixture, "approval")
	approval := fixture.Locator(`[data-component="approval-card-interaction"]`)
	if err := browserAssertions().Locator(approval).ToHaveAttribute("data-approval-state", "pending"); err != nil {
		t.Fatalf("pending approval: %v", err)
	}
	for _, label := range []string{"Allow once", "Allow for this task", "Deny"} {
		assertM18NamedButton(t, fixture, label, true)
	}
	if err := fixture.GetByRole(*playwright.AriaRoleButton, playwright.LocatorGetByRoleOptions{
		Name: "Allow once", Exact: playwright.Bool(true),
	}).Click(); err != nil {
		t.Fatalf("resolve mounted approval: %v", err)
	}
	if err := browserAssertions().Locator(approval).ToHaveAttribute("data-approval-state", "granted"); err != nil {
		t.Fatalf("committed approval state: %v", err)
	}
	if count, err := approval.GetByRole(*playwright.AriaRoleButton).Count(); err != nil || count != 0 {
		t.Fatalf("resolved/stale approval actions = %d, error=%v", count, err)
	}
	if err := browserAssertions().Locator(approval).ToContainText("local user"); err != nil {
		t.Fatalf("resolved approval attribution: %v", err)
	}
	if err := browserAssertions().Locator(approval).ToBeFocused(); err != nil {
		t.Fatalf("approval resolution focus return: %v", err)
	}

	activateM18Stage(t, page, fixture, "review")
	assertM18CardKind(t, fixture, "diff-summary")
	assertM18NamedButton(t, fixture, "Open review", true)
	activateM18Stage(t, page, fixture, "repair")
	assertM18CardKind(t, fixture, "plan-revision")
	if err := browserAssertions().Locator(fixture).ToContainText("Approval reset"); err != nil {
		t.Fatalf("repair approval reset: %v", err)
	}

	activateM18Stage(t, page, fixture, "reconnect")
	taskPanel := fixture.Locator(`[data-component="task-control-panel"]`)
	if err := browserAssertions().Locator(taskPanel).ToHaveAttribute("data-delivery", "disconnected"); err != nil {
		t.Fatalf("reconnect delivery state: %v", err)
	}
	for _, label := range []string{"Pause task", "Stop task"} {
		button := fixture.GetByRole(*playwright.AriaRoleButton, playwright.LocatorGetByRoleOptions{
			Name: label, Exact: playwright.Bool(true),
		})
		assertM18Disabled(t, button, true, "journey reconnect")
	}

	activateM18Stage(t, page, fixture, "recovery")
	recovery := fixture.Locator(`[data-component="recovery-panel"]`)
	if err := browserAssertions().Locator(recovery).ToContainText("checkpoint cp-m18"); err != nil {
		t.Fatalf("recovery checkpoint: %v", err)
	}
	for _, text := range []string{
		"verify mounted journey", "one user-edited file differs", "External action outcome is ambiguous",
		"Reconcile user edits", "Preserve patch",
	} {
		if err := browserAssertions().Locator(recovery).ToContainText(text); err != nil {
			t.Fatalf("recovery text %q: %v", text, err)
		}
	}
	if count, err := recovery.GetByRole(*playwright.AriaRoleButton, playwright.LocatorGetByRoleOptions{
		Name: "Safe resume task", Exact: playwright.Bool(true),
	}).Count(); err != nil || count != 0 {
		t.Fatalf("unverified safe resume actions = %d, error=%v", count, err)
	}
	if count, err := recovery.GetByText("Retry", playwright.LocatorGetByTextOptions{Exact: playwright.Bool(true)}).Count(); err != nil || count != 0 {
		t.Fatalf("unsafe retry labels = %d, error=%v", count, err)
	}
	assertM18NamedButton(t, recovery, "Reconcile user edits task", true)
	assertM18NamedButton(t, recovery, "Preserve patch task", true)
	assertM18NamedButton(t, recovery, "Open recovery event Related recovery event", true)
	assertM18NamedButton(t, recovery, "Open recovery event Missing recovery event", true)
	assertM18NamedButton(t, recovery, "Open recovery file web/client/main.go", true)
	assertM18NamedButton(t, recovery, "Open recovery file ../outside", false)
	if err := recovery.GetByRole(*playwright.AriaRoleButton, playwright.LocatorGetByRoleOptions{
		Name: "Reconcile user edits task", Exact: playwright.Bool(true),
	}).Click(); err != nil {
		t.Fatalf("reconcile recovery action: %v", err)
	}
	assertM18Attributes(t, fixture, map[string]string{"data-recovery-actions": "1"})
	if err := recovery.GetByRole(*playwright.AriaRoleButton, playwright.LocatorGetByRoleOptions{
		Name: "Preserve patch task", Exact: playwright.Bool(true),
	}).Click(); err != nil {
		t.Fatalf("preserve recovery patch action: %v", err)
	}
	assertM18Attributes(t, fixture, map[string]string{"data-recovery-actions": "2"})

	if err := recovery.GetByRole(*playwright.AriaRoleButton, playwright.LocatorGetByRoleOptions{
		Name: "Open recovery event Related recovery event", Exact: playwright.Bool(true),
	}).Click(); err != nil {
		t.Fatalf("open related recovery event: %v", err)
	}
	assertM18Attributes(t, fixture, map[string]string{"data-recovery-detail": "event"})
	selection := assertM18RecoveryLocation(t, fixture)
	if selection.EventID == nil || selection.EventID.String() != "evt_01890f3c-4a00-7abc-8def-0123456789ab" || selection.ReviewFile != "" {
		t.Fatalf("related event route selection = %+v", selection)
	}
	relatedCard := fixture.Locator(`[data-component="timeline-card"][data-stable-key="m18:related-event"]`)
	if err := browserAssertions().Locator(relatedCard).ToHaveAttribute("data-selected", "true"); err != nil {
		t.Fatalf("related event selection: %v", err)
	}
	if err := browserAssertions().Locator(relatedCard).ToBeFocused(); err != nil {
		t.Fatalf("related event focus: %v", err)
	}

	if err := recovery.GetByRole(*playwright.AriaRoleButton, playwright.LocatorGetByRoleOptions{
		Name: "Open recovery event Missing recovery event", Exact: playwright.Bool(true),
	}).Click(); err != nil {
		t.Fatalf("open missing recovery event: %v", err)
	}
	assertM18Attributes(t, fixture, map[string]string{"data-recovery-detail": "missing-event"})
	selection = assertM18RecoveryLocation(t, fixture)
	if selection.EventID == nil || selection.EventID.String() != "evt_01890f3c-4a00-7abc-8def-1123456789ab" {
		t.Fatalf("missing event route selection = %+v", selection)
	}
	if err := browserAssertions().Locator(fixture.Locator(`[data-component="timeline-selection-notice"]`)).ToBeVisible(); err != nil {
		t.Fatalf("missing event notice: %v", err)
	}
	if err := browserAssertions().Locator(relatedCard).ToHaveAttribute("data-selected", "false"); err != nil {
		t.Fatalf("missing event falsely selected a card: %v", err)
	}
	if err := browserAssertions().Locator(relatedCard).Not().ToBeFocused(); err != nil {
		t.Fatalf("missing event falsely focused a card: %v", err)
	}

	if err := recovery.GetByRole(*playwright.AriaRoleButton, playwright.LocatorGetByRoleOptions{
		Name: "Open recovery file web/client/main.go", Exact: playwright.Bool(true),
	}).Click(); err != nil {
		t.Fatalf("open recovery file review: %v", err)
	}
	assertM18Attributes(t, fixture, map[string]string{"data-recovery-detail": "file"})
	selection = assertM18RecoveryLocation(t, fixture)
	if selection.EventID != nil || selection.ReviewFile != "web/client/main.go" {
		t.Fatalf("recovery file route selection = %+v", selection)
	}
	drawer := page.Locator(`[data-component="review-drawer"]`)
	if err := browserAssertions().Locator(drawer).ToContainText("web/client/main.go"); err != nil {
		t.Fatalf("recovery file review drawer: %v", err)
	}
	if err := page.GetByRole(*playwright.AriaRoleButton, playwright.PageGetByRoleOptions{
		Name: "Close review", Exact: playwright.Bool(true),
	}).Click(); err != nil {
		t.Fatalf("close recovery file review: %v", err)
	}
	assertM18Attributes(t, fixture, map[string]string{"data-recovery-detail": ""})
	selection = assertM18RecoveryLocation(t, fixture)
	if selection.EventID != nil || selection.ReviewFile != "" {
		t.Fatalf("closed recovery detail route selection = %+v", selection)
	}
	if err := browserAssertions().Locator(drawer).Not().ToBeVisible(); err != nil {
		t.Fatalf("closed recovery drawer remained visible: %v", err)
	}

	activateM18Stage(t, page, fixture, "budget")
	taskPanel = fixture.Locator(`[data-component="task-control-panel"]`)
	if err := browserAssertions().Locator(taskPanel.Locator(`[data-component="hard-cap-decision"]`)).ToBeVisible(); err != nil {
		t.Fatalf("hard-cap decision: %v", err)
	}
	actual, err := taskPanel.Locator(`[data-metric="actual-cost"]`).TextContent()
	if err != nil || !strings.Contains(actual, "Unknown") || strings.Contains(actual, "USD 0") || strings.Contains(actual, "$0") {
		t.Fatalf("journey unknown cost = %q, error=%v", actual, err)
	}

	activateM18Stage(t, page, fixture, "graph")
	graphNode := fixture.Locator(`[data-node-id="journey-node-2"]`)
	if err := graphNode.Click(); err != nil {
		t.Fatalf("select journey graph node: %v", err)
	}
	if err := browserAssertions().Locator(graphNode).ToHaveAttribute("aria-pressed", "true"); err != nil {
		t.Fatalf("journey graph selection: %v", err)
	}
}

func assertM18RecoveryLocation(t *testing.T, fixture playwright.Locator) routes.TaskDetailSelection {
	t.Helper()
	location, err := fixture.GetAttribute("data-recovery-location")
	if err != nil || !strings.HasPrefix(location, "/tasks?") {
		t.Fatalf("typed recovery location = %q, error=%v", location, err)
	}
	selection, err := routes.ParseTaskDetailSelection(strings.TrimPrefix(location, "/tasks?"))
	if err != nil {
		t.Fatalf("parse typed recovery location %q: %v", location, err)
	}
	return selection
}

func assertM18CardKind(t *testing.T, fixture playwright.Locator, kind string) {
	t.Helper()
	card := fixture.Locator(`[data-component="timeline-card"][data-card-kind="` + kind + `"]`)
	if err := browserAssertions().Locator(card).ToBeVisible(); err != nil {
		t.Fatalf("M18 %s card: %v", kind, err)
	}
}

func assertM18NamedButton(t *testing.T, fixture playwright.Locator, name string, enabled bool) {
	t.Helper()
	button := fixture.GetByRole(*playwright.AriaRoleButton, playwright.LocatorGetByRoleOptions{
		Name: name, Exact: playwright.Bool(true),
	})
	if err := browserAssertions().Locator(button).ToBeVisible(); err != nil {
		t.Fatalf("M18 button %q: %v", name, err)
	}
	assertM18Disabled(t, button, !enabled, name)
}

func clickM18Fixture(t *testing.T, page playwright.Page, testID string) {
	t.Helper()
	if err := page.GetByTestId(testID).Click(); err != nil {
		t.Fatalf("click %s: %v", testID, err)
	}
}

func activateM18Stage(
	t *testing.T,
	page playwright.Page,
	fixture playwright.Locator,
	stage string,
) {
	t.Helper()
	selector := page.GetByTestId("m18-stage-select")
	if _, err := selector.SelectOption(playwright.SelectOptionValues{Values: &[]string{stage}}); err != nil {
		t.Fatalf("select M18 %s flow: %v", stage, err)
	}
	if err := browserAssertions().Locator(fixture).ToHaveAttribute("data-stage", stage); err != nil {
		t.Fatalf("activate M18 %s flow: %v", stage, err)
	}
	facts := fixture.Locator(`[data-component="m18-decision-facts"]`)
	if err := browserAssertions().Locator(facts).ToBeVisible(); err != nil {
		t.Fatalf("%s decision facts: %v", stage, err)
	}
	for _, field := range []string{"state", "cost", "authority", "evidence", "uncertainty", "next-action"} {
		value, err := facts.Locator(`[data-fact="` + field + `"]`).TextContent()
		if err != nil || strings.TrimSpace(value) == "" {
			t.Fatalf("%s %s fact = %q, error=%v", stage, field, value, err)
		}
	}
}

func assertM18Attributes(
	t *testing.T,
	locator playwright.Locator,
	want map[string]string,
) {
	t.Helper()
	for name, value := range want {
		if err := browserAssertions().Locator(locator).ToHaveAttribute(name, value); err != nil {
			t.Fatalf("%s=%q: %v", name, value, err)
		}
	}
}

func assertM18Disabled(t *testing.T, locator playwright.Locator, want bool, phase string) {
	t.Helper()
	var err error
	if want {
		err = browserAssertions().Locator(locator).ToBeDisabled()
	} else {
		err = browserAssertions().Locator(locator).ToBeEnabled()
	}
	if err != nil {
		t.Fatalf("pause disabled=%t during %s: %v", want, phase, err)
	}
}

func m18BrowserCheckDetail(
	state string,
	sequence uint64,
	duplicates uint64,
	gaps uint64,
) string {
	return fmt.Sprintf(
		"connection=%s last_sequence=%d duplicates=%d gaps=%d",
		state,
		sequence,
		duplicates,
		gaps,
	)
}
