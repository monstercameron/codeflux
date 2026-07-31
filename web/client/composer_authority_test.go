package main

import (
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/composer"
	frontendstate "codeflux.dev/codeflux/web/frontend/state"
	"codeflux.dev/codeflux/web/frontend/taskcontrols"
	"codeflux.dev/codeflux/web/frontend/taskprojection"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestAuthoritativeComposerActionsInvokeEverySupportedCommand(t *testing.T) {
	invoked := make(map[composer.TaskAction]int)
	controls := &taskcontrols.Props{
		OnPause:         func() { invoked[composer.ActionPause]++ },
		OnResume:        func() { invoked[composer.ActionResume]++ },
		OnStop:          func() { invoked[composer.ActionStop]++ },
		OnBudgetAdjust:  func() { invoked[composer.ActionChangeBudget]++ },
		OnPreservePatch: func() { invoked[composer.ActionPreservePatch]++ },
	}
	inspect := func() { invoked[composer.ActionInspectGraph]++ }
	tests := []struct {
		name    string
		task    taskprojection.TaskProjection
		actions []composer.TaskAction
	}{
		{
			name:    "live running controls",
			task:    taskprojection.TaskProjection{State: domain.TaskStateRunning},
			actions: []composer.TaskAction{composer.ActionPause, composer.ActionStop, composer.ActionInspectGraph},
		},
		{
			name:    "live paused controls",
			task:    taskprojection.TaskProjection{State: domain.TaskStatePaused},
			actions: []composer.TaskAction{composer.ActionResume, composer.ActionChangeBudget, composer.ActionStop},
		},
		{
			name: "recovery patch control",
			task: taskprojection.TaskProjection{
				State: domain.TaskStateRecoveryRequired, Recovery: taskprojection.RecoveryPreserveOnly,
			},
			actions: []composer.TaskAction{composer.ActionPreservePatch},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			props := bindAuthoritativeComposerTaskActions(
				composer.Props{}, test.task, frontendstate.ConnectionLive, controls, inspect,
			)
			markup := renderAuthoritativeComposer(t, props)
			for _, action := range test.actions {
				if reason := props.View.Task.DisabledReason(action); reason != "" {
					t.Fatalf("%s disabled: %q", action, reason)
				}
				if segment := mountedTaskActionSegment(t, markup, action); strings.Contains(segment, " disabled") {
					t.Fatalf("%s mounted disabled: %s", action, segment)
				}
				before := invoked[action]
				props.OnTaskAction(action)
				if invoked[action] != before+1 {
					t.Fatalf("%s invocation count=%d, want %d", action, invoked[action], before+1)
				}
			}
		})
	}
}

func TestAuthoritativeComposerActionCallbackRejectsActionsOutsideCurrentMatrix(t *testing.T) {
	inspections := 0
	props := bindAuthoritativeComposerTaskActions(
		composer.Props{}, taskprojection.TaskProjection{State: domain.TaskStatePaused},
		frontendstate.ConnectionLive, &taskcontrols.Props{}, func() { inspections++ },
	)
	props.OnTaskAction(composer.ActionInspectGraph)
	if inspections != 0 {
		t.Fatalf("out-of-matrix graph callback invoked %d times", inspections)
	}
}

func TestAuthoritativeComposerActionsHonorCommandSettlementsAndConnectionCertainty(t *testing.T) {
	key, err := taskprojection.ParseCommandKey("composer-command-key-0001")
	if err != nil {
		t.Fatal(err)
	}
	controls := &taskcontrols.Props{OnPause: func() {}, OnStop: func() {}}
	tests := []struct {
		name       string
		status     taskprojection.CommandStatus
		connection frontendstate.ConnectionState
		blocked    bool
	}{
		{name: "live", status: taskprojection.CommandIdle, connection: frontendstate.ConnectionLive},
		{name: "busy", status: taskprojection.CommandBusy, connection: frontendstate.ConnectionLive, blocked: true},
		{name: "committed", status: taskprojection.CommandCommitted, connection: frontendstate.ConnectionLive},
		{name: "stale", status: taskprojection.CommandStale, connection: frontendstate.ConnectionLive, blocked: true},
		{name: "denied", status: taskprojection.CommandDenied, connection: frontendstate.ConnectionLive, blocked: true},
		{name: "disconnected settlement", status: taskprojection.CommandDisconnected, connection: frontendstate.ConnectionLive, blocked: true},
		{name: "disconnected session", status: taskprojection.CommandIdle, connection: frontendstate.ConnectionDisconnected, blocked: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pending := taskprojection.CommandState{Status: test.status}
			if test.status != taskprojection.CommandIdle {
				pending.Action = taskprojection.ActionPause
				pending.Key = key
				pending.ExpectedRevision = 7
			}
			props := bindAuthoritativeComposerTaskActions(
				composer.Props{},
				taskprojection.TaskProjection{State: domain.TaskStateRunning, PendingCommand: pending},
				test.connection, controls, func() {},
			)
			markup := renderAuthoritativeComposer(t, props)
			pauseBlocked := props.View.Task.DisabledReason(composer.ActionPause) != ""
			stopBlocked := props.View.Task.DisabledReason(composer.ActionStop) != ""
			if pauseBlocked != test.blocked || stopBlocked != test.blocked {
				t.Fatalf("pause blocked=%t stop blocked=%t, want %t", pauseBlocked, stopBlocked, test.blocked)
			}
			if reason := props.View.Task.DisabledReason(composer.ActionInspectGraph); reason != "" {
				t.Fatalf("read-only graph inspection disabled: %q", reason)
			}
			for _, action := range []composer.TaskAction{composer.ActionPause, composer.ActionStop} {
				mountedDisabled := strings.Contains(mountedTaskActionSegment(t, markup, action), " disabled")
				if mountedDisabled != test.blocked {
					t.Fatalf("mounted %s disabled=%t, want %t", action, mountedDisabled, test.blocked)
				}
			}
		})
	}
}

func TestAuthoritativeComposerActionsKeepUnsupportedAndDeniedActionsDisabled(t *testing.T) {
	controls := &taskcontrols.Props{OnPause: func() {}, OnStop: func() {}}
	denied := taskprojection.TaskProjection{
		State: domain.TaskStateRunning,
		Policy: taskprojection.ActionPolicy{
			Denied:     []taskprojection.ActionKind{taskprojection.ActionPause},
			SafeReason: "Pause is denied by the validated policy.",
		},
	}
	props := bindAuthoritativeComposerTaskActions(
		composer.Props{}, denied, frontendstate.ConnectionLive, controls, func() {},
	)
	if got := props.View.Task.DisabledReason(composer.ActionPause); got != denied.Policy.SafeReason {
		t.Fatalf("pause reason=%q", got)
	}

	recovery := taskprojection.TaskProjection{
		State: domain.TaskStateRecoveryRequired, Recovery: taskprojection.RecoverySafeResume,
	}
	safeResume := 0
	props = bindAuthoritativeComposerTaskActions(
		composer.Props{}, recovery, frontendstate.ConnectionLive,
		&taskcontrols.Props{OnSafeResume: func() { safeResume++ }}, nil,
	)
	if got := props.View.Task.DisabledReason(composer.ActionSafeResume); got != "" {
		t.Fatalf("authoritative safe resume disabled: %q", got)
	}
	props.OnTaskAction(composer.ActionSafeResume)
	if safeResume != 1 {
		t.Fatalf("safe resume invocations = %d", safeResume)
	}
}

func TestAuthoritativeComposerActionReasonsAreMountedAccessibly(t *testing.T) {
	task := taskprojection.TaskProjection{State: domain.TaskStatePaused}
	props := bindAuthoritativeComposerTaskActions(
		composer.Props{}, task, frontendstate.ConnectionDisconnected,
		&taskcontrols.Props{OnResume: func() {}, OnStop: func() {}, OnBudgetAdjust: func() {}}, nil,
	)
	markup := renderAuthoritativeComposer(t, props)
	for _, want := range []string{
		`data-task-action="resume"`,
		`id="composer-task-action-resume"`,
		`aria-label="Resume"`,
		`aria-describedby="composer-task-action-resume-reason"`,
		`id="composer-task-action-resume-reason"`,
		`Disconnected; the backend task may still be running`,
		`data-task-action="change-budget"`,
		`data-task-action="review"`,
		`Review is unavailable until a current authoritative review projection is mounted.`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("mounted composer missing %q: %s", want, markup)
		}
	}
}

func renderAuthoritativeComposer(t *testing.T, props composer.Props) string {
	t.Helper()
	markup, err := ui.RenderToString(ui.CreateElement(composer.Composer, props))
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func mountedTaskActionSegment(t *testing.T, markup string, action composer.TaskAction) string {
	t.Helper()
	marker := `data-task-action="` + string(action) + `"`
	markerAt := strings.Index(markup, marker)
	if markerAt < 0 {
		t.Fatalf("mounted composer is missing %s: %s", marker, markup)
	}
	start := strings.LastIndex(markup[:markerAt], "<span")
	endOffset := strings.Index(markup[markerAt:], "</span>")
	if start < 0 || endOffset < 0 {
		t.Fatalf("mounted composer has malformed %s action: %s", action, markup)
	}
	return markup[start : markerAt+endOffset+len("</span>")]
}

func TestAuthoritativeComposerStopUsesConfirmationBeforeRPC(t *testing.T) {
	confirmed, stopped := 0, 0
	controls := &taskcontrols.Props{
		StopConfirm:   taskcontrols.StopConfirmation{Required: true},
		OnStopConfirm: func() { confirmed++ },
		OnStop:        func() { stopped++ },
	}
	props := bindAuthoritativeComposerTaskActions(
		composer.Props{}, taskprojection.TaskProjection{State: domain.TaskStateRunning},
		frontendstate.ConnectionLive, controls, nil,
	)
	props.OnTaskAction(composer.ActionStop)
	if confirmed != 1 || stopped != 0 {
		t.Fatalf("confirmation=%d stop RPC=%d", confirmed, stopped)
	}
}
