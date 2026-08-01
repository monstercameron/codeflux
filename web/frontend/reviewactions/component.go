// Package reviewactions renders acceptance, repair, rollback, and rejection
// controls bound to one immutable review. It intentionally contains no
// browser-script escape hatch: every interaction is a typed GWC callback.
package reviewactions

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"codeflux.dev/codeflux/internal/acceptance"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/design"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type SelectableRepairTarget struct {
	Kind     acceptance.RepairTargetKind
	ID       string
	Label    string
	Status   acceptance.CheckStatus
	Selected bool
}

type RollbackTarget struct {
	RepairRequestID acceptance.RepairRequestID
	CheckpointID    domain.CheckpointID
}

type Props struct {
	Review               acceptance.Review
	CurrentReportID      string
	CurrentDiffIdentity  string
	AcknowledgedCheckIDs []string
	// LiveRequiredChecks is the authoritative validation-controller snapshot
	// for the exact check IDs in Review.RequiredChecks.
	LiveRequiredChecks   []acceptance.RequiredCheck
	RepairTargets        []SelectableRepairTarget
	RepairFeedback       string
	Rollback             *RollbackTarget
	BusyAction           string
	Mode                 primitives.Mode
	OnToggleAcknowledge  func(string, bool)
	OnAccept             func()
	OnToggleRepairTarget func(acceptance.RepairTargetKind, string, bool)
	OnRepairFeedback     func(string)
	OnRequestRepair      func()
	OnRollback           func()
	OnRejectPreserving   func()
}

func Component(props Props) ui.Node {
	checks := props.LiveRequiredChecks
	if checks == nil {
		checks = props.Review.RequiredChecks
	}
	gate, err := acceptance.EvaluateAcceptanceWithLiveChecks(
		props.Review, props.CurrentReportID, props.CurrentDiffIdentity,
		checks, props.AcknowledgedCheckIDs,
	)
	if err != nil {
		return primitives.InlineAlert(primitives.InlineAlertProps{
			Title: "Review actions unavailable", Message: err.Error(),
			Tone: design.StatusFailure, Mode: props.Mode,
		})
	}

	return html.Section(html.Props{
		Aria: map[string]string{"label": "Review decision controls"},
		Data: map[string]string{
			"component":       "review-actions",
			"review-id":       props.Review.ID.String(),
			"review-revision": strconv.FormatUint(props.Review.Revision, 10),
			"report-id":       props.Review.ReportID,
			"diff-identity":   props.Review.DiffIdentity,
			"plan-revision":   strconv.FormatUint(props.Review.PlanRevision, 10),
			"stale":           strconv.FormatBool(gate.Stale),
		},
	},
		html.H2(html.Props{Class: design.HeadingClass(props.Mode.Tokens(), design.HeadingSection), Text: "Accept or request repair"}),
		bindingSummary(props),
		acceptanceControls(props, gate),
		repairControls(props),
		rollbackControls(props),
		rejectionControls(props),
	)
}

func bindingSummary(props Props) ui.Node {
	return html.Div(html.Props{Data: map[string]string{"section": "exact-review-binding"}},
		html.P(html.Props{Text: fmt.Sprintf("Review revision %d · plan revision %d", props.Review.Revision, props.Review.PlanRevision)}),
		html.P(html.Props{Text: "Evidence report: " + props.Review.ReportID}),
		html.P(html.Props{Text: "Reviewed diff: " + props.Review.DiffIdentity}),
	)
}

func acceptanceControls(props Props, gate acceptance.Gate) ui.Node {
	children := []ui.Node{html.H3(html.Props{Class: design.HeadingClass(props.Mode.Tokens(), design.HeadingPanel), Text: "Acceptance"})}
	checks := props.LiveRequiredChecks
	if checks == nil {
		checks = props.Review.RequiredChecks
	}
	for _, check := range checks {
		if check.Status != acceptance.CheckFailed && check.Status != acceptance.CheckWaived {
			continue
		}
		check := check
		pressed := slices.Contains(props.AcknowledgedCheckIDs, check.ID)
		children = append(children, primitives.ToggleButton(primitives.ToggleButtonProps{
			Label:           fmt.Sprintf("Acknowledge %s required check %s", check.Status, check.ID),
			AccessibleLabel: fmt.Sprintf("Acknowledge %s required check %s", check.Status, check.ID),
			Pressed:         pressed, Disabled: props.BusyAction != "", Mode: props.Mode,
			OnToggle: func(next bool) {
				if props.OnToggleAcknowledge != nil {
					props.OnToggleAcknowledge(check.ID, next)
				}
			},
		}))
	}
	if gate.Reason != "" {
		children = append(children, html.P(html.Props{
			ID: "acceptance-gate-reason", Role: "status", Text: gate.Reason,
			Data: map[string]string{"gate": gateName(gate)},
		}))
	}
	children = append(children, primitives.Button(primitives.ButtonProps{
		Label: "Accept exact report and diff", Primary: true,
		Disabled: !gate.CanAccept || props.OnAccept == nil,
		Busy:     props.BusyAction == "accept", DescribedBy: "acceptance-gate-reason",
		Mode: props.Mode, OnClick: props.OnAccept,
	}))
	return html.Div(html.Props{Data: map[string]string{"section": "acceptance"}}, children...)
}

func repairControls(props Props) ui.Node {
	children := []ui.Node{html.H3(html.Props{Class: design.HeadingClass(props.Mode.Tokens(), design.HeadingPanel), Text: "Request a bounded repair"})}
	selected := 0
	for _, target := range props.RepairTargets {
		target := target
		if target.Selected {
			selected++
		}
		disabled := props.BusyAction != "" ||
			(target.Kind == acceptance.RepairTargetValidation && target.Status == acceptance.CheckPassed)
		children = append(children, primitives.ToggleButton(primitives.ToggleButtonProps{
			Label: repairTargetLabel(target), Pressed: target.Selected,
			Disabled: disabled, Mode: props.Mode,
			OnToggle: func(next bool) {
				if props.OnToggleRepairTarget != nil {
					props.OnToggleRepairTarget(target.Kind, target.ID, next)
				}
			},
		}))
	}
	children = append(children,
		primitives.TextField(primitives.TextFieldProps{
			ID: "repair-feedback", Label: "Repair instructions",
			Value: props.RepairFeedback, Required: true,
			Disabled: props.BusyAction != "", Mode: props.Mode,
			OnInput: props.OnRepairFeedback,
		}),
		primitives.Button(primitives.ButtonProps{
			Label:    "Create new repair plan from selected targets",
			Disabled: selected == 0 || strings.TrimSpace(props.RepairFeedback) == "" || props.OnRequestRepair == nil,
			Busy:     props.BusyAction == "repair", Mode: props.Mode,
			OnClick: props.OnRequestRepair,
		}),
	)
	return html.Div(html.Props{
		Data: map[string]string{"section": "repair", "selected-target-count": strconv.Itoa(selected)},
	}, children...)
}

func rollbackControls(props Props) ui.Node {
	if props.Rollback == nil {
		return html.Div(html.Props{Data: map[string]string{"section": "rollback", "available": "false"}},
			html.H3(html.Props{Class: design.HeadingClass(props.Mode.Tokens(), design.HeadingPanel), Text: "Rollback"}),
			html.P(html.Props{Text: "No pre-repair checkpoint is available."}),
		)
	}
	return html.Div(html.Props{Data: map[string]string{
		"section": "rollback", "available": "true",
		"repair-request-id": props.Rollback.RepairRequestID.String(),
		"checkpoint-id":     props.Rollback.CheckpointID.String(),
	}},
		html.H3(html.Props{Class: design.HeadingClass(props.Mode.Tokens(), design.HeadingPanel), Text: "Rollback"}),
		html.P(html.Props{Text: "Pre-repair checkpoint: " + props.Rollback.CheckpointID.String()}),
		primitives.Button(primitives.ButtonProps{
			Label:    "Roll back to pre-repair checkpoint",
			Disabled: props.OnRollback == nil, Busy: props.BusyAction == "rollback",
			Mode: props.Mode, OnClick: props.OnRollback,
		}),
	)
}

func rejectionControls(props Props) ui.Node {
	return html.Div(html.Props{Data: map[string]string{
		"section": "rejection", "preserve-patch": "true",
	}},
		html.H3(html.Props{Class: design.HeadingClass(props.Mode.Tokens(), design.HeadingPanel), Text: "Reject this candidate"}),
		html.P(html.Props{Text: "The patch and its evidence remain available for inspection."}),
		primitives.Button(primitives.ButtonProps{
			Label:    "Reject and preserve patch",
			Disabled: props.OnRejectPreserving == nil, Busy: props.BusyAction == "reject",
			Mode: props.Mode, OnClick: props.OnRejectPreserving,
		}),
	)
}

func gateName(gate acceptance.Gate) string {
	switch {
	case gate.Stale:
		return "stale"
	case len(gate.RunningCheckIDs) > 0:
		return "running"
	case len(gate.AcknowledgementCheckIDs) > 0:
		return "acknowledgement-required"
	case len(gate.BlockingCheckIDs) > 0:
		return "blocked"
	default:
		return "ready"
	}
}

func repairTargetLabel(target SelectableRepairTarget) string {
	label := strings.TrimSpace(target.Label)
	if label == "" {
		label = target.ID
	}
	if target.Kind == acceptance.RepairTargetValidation {
		return fmt.Sprintf("Validation %s: %s (%s)", target.ID, label, target.Status)
	}
	return fmt.Sprintf("Diff hunk %s: %s", target.ID, label)
}
