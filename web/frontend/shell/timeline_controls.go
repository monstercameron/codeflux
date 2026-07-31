package shell

import (
	"fmt"
	"strconv"
	"strings"

	"codeflux.dev/codeflux/web/frontend/primitives"
	"codeflux.dev/codeflux/web/frontend/timeline"
	"codeflux.dev/codeflux/web/frontend/timelinecard"
	"codeflux.dev/codeflux/web/frontend/timelineview"
	"github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/css/u"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// TimelineControlProps is the mounted command surface for timeline pagination,
// replay gaps, deliberate live following, review position, and graph inspection.
// The coordinator remains authoritative through the callbacks.
type TimelineControlProps struct {
	Mode                     primitives.Mode
	Authoritative            bool
	Cards                    []timelinecard.Card
	Actions                  timelineview.Actions
	Enabled                  bool
	HasOlder                 bool
	LoadingOlder             bool
	OlderError               string
	Gaps                     []timeline.SequenceGap
	NewEvents                int
	ReviewOpen               bool
	ReviewFile               string
	SelectedStableKey        string
	SelectionNotice          string
	ReturnToCurrentAvailable bool
	Latency                  timelinecard.LatencyPresentation
	ReviewBindings           ReviewBindingView
	ReviewDecisions          ReviewDecisionProps
	OnLoadOlder              func()
	OnRetryOlder             func()
	OnReturnLive             func()
	OnOpenReview             func()
	OnCloseReview            func()
	OnReturnToCurrent        func()
	OnStop                   func()
}

// ReviewBindingView identifies the exact authoritative revisions presented in
// the drawer. Zero remains honestly unknown rather than an invented revision.
type ReviewBindingView struct {
	Task       uint64
	Diff       uint64
	Plan       uint64
	Validation uint64
	Evidence   uint64
}

type ReviewDecisionProps struct {
	Accept     timelineview.ActionCommandState
	Repair     timelineview.ActionCommandState
	Reject     timelineview.ActionCommandState
	Rollback   timelineview.ActionCommandState
	OnAccept   func()
	OnRepair   func()
	OnReject   func()
	OnRollback func()
}

// TimelineControls keeps correctness-bearing navigation inline with the
// conversation. It never uses a toast, modal, or assertive live region.
func TimelineControls(props TimelineControlProps) ui.Node {
	focus := ui.UseFocusManager()
	if !props.Enabled {
		return html.Div(html.Props{Hidden: true})
	}
	tokens := props.Mode.Tokens()
	children := make([]ui.Node, 0, 8)
	if props.HasOlder {
		label := "Load older messages"
		if props.LoadingOlder {
			label = "Loading older messages"
		}
		children = append(children, primitives.Button(primitives.ButtonProps{
			Label: label, Mode: props.Mode,
			Disabled: props.LoadingOlder || props.OnLoadOlder == nil,
			Busy:     props.LoadingOlder,
			OnClick:  props.OnLoadOlder,
		}))
	}
	if strings.TrimSpace(props.OlderError) != "" {
		children = append(children,
			html.Span(html.Props{
				Role: "status", Aria: map[string]string{"live": "polite"},
				Data: map[string]string{"component": "older-page-error"},
				Text: props.OlderError,
			}),
			primitives.Button(primitives.ButtonProps{
				Label: "Retry older messages", Mode: props.Mode,
				Disabled: props.OnRetryOlder == nil, OnClick: props.OnRetryOlder,
			}),
		)
	}
	if len(props.Gaps) > 0 {
		children = append(children, html.Span(html.Props{
			Role: "status", Aria: map[string]string{"live": "polite"},
			Data: map[string]string{
				"component": "sequence-gap-recovery",
				"gap-count": strconv.Itoa(len(props.Gaps)),
			},
			Text: fmt.Sprintf("Recovering %d missing event range(s)", len(props.Gaps)),
		}))
	}
	if props.NewEvents > 0 {
		children = append(children, primitives.Button(primitives.ButtonProps{
			Label: fmt.Sprintf("%d new events · Return to live", props.NewEvents),
			Mode:  props.Mode, Disabled: props.OnReturnLive == nil,
			OnClick: props.OnReturnLive,
		}))
	}
	if props.ReturnToCurrentAvailable {
		children = append(children, primitives.Button(primitives.ButtonProps{
			Label: "Return to current graph node", Mode: props.Mode,
			Disabled: props.OnReturnToCurrent == nil, OnClick: props.OnReturnToCurrent,
		}))
	}
	if props.OnOpenReview != nil {
		reviewExpanded := props.ReviewOpen
		children = append(children, primitives.Button(primitives.ButtonProps{
			ID: "task-review-trigger", Label: "Open task review", Mode: props.Mode,
			Expanded: &reviewExpanded, Controls: "task-review-drawer", OnClick: props.OnOpenReview,
		}))
	}
	if props.ReviewOpen {
		dismissReview := func() {
			if props.OnCloseReview == nil {
				return
			}
			props.OnCloseReview()
			if !focus.FocusByID("task-review-trigger") {
				focus.FocusByID("task-actions-trigger")
			}
		}
		reviewDescription := "Thread anchor and graph position are preserved while review is open."
		if strings.TrimSpace(props.ReviewFile) != "" {
			reviewDescription = "Reviewing workspace-relative recovery file " + props.ReviewFile + ". Thread anchor and graph position are preserved."
		}
		children = append(children, primitives.Drawer(primitives.OverlayProps{
			ID: "task-review-drawer", Open: true,
			LabelledBy:           "task-review-title",
			DescribedBy:          "task-review-description",
			InitialFocusSelector: "#task-review-close",
			AppRootSelector:      `[data-component="app-shell"]`,
			Mode:                 props.Mode,
			OnDismiss:            dismissReview,
			Content: html.Aside(html.Props{
				Aria: map[string]string{"label": "Task review"},
				Data: map[string]string{
					"component":   "review-drawer",
					"position":    "preserved",
					"review-file": props.ReviewFile,
				},
			},
				html.H2(html.Props{ID: "task-review-title", Text: "Review file"}),
				html.P(html.Props{
					ID:   "task-review-description",
					Text: reviewDescription,
				}),
				reviewBindingSummary(props.ReviewBindings, props.Mode),
				reviewDecisionControls(props.ReviewDecisions, props.Mode),
				primitives.Button(primitives.ButtonProps{
					ID: "task-review-close", Label: "Close review", Mode: props.Mode,
					Disabled: props.OnCloseReview == nil, OnClick: dismissReview,
				}),
			),
		}))
	}
	if strings.TrimSpace(props.Latency.Phase) != "" {
		latencyChildren := []ui.Node{
			html.Span(html.Props{Text: "Current phase: " + props.Latency.Phase}),
		}
		if props.Latency.ShowStop {
			latencyChildren = append(latencyChildren, primitives.Button(primitives.ButtonProps{
				Label: "Stop delayed task", Mode: props.Mode,
				Disabled: props.OnStop == nil, OnClick: props.OnStop,
			}))
		}
		children = append(children, html.Div(html.Props{
			Data: map[string]string{
				"component": "first-message-latency",
				"waiting":   strconv.FormatBool(props.Latency.Waiting),
			},
		}, latencyChildren...))
	}
	return html.Div(html.Props{
		Role: "group", Aria: map[string]string{"label": "Timeline navigation"},
		Data: map[string]string{
			"component":   "timeline-controls",
			"review-open": strconv.FormatBool(props.ReviewOpen),
			"has-older":   strconv.FormatBool(props.HasOlder),
		},
		Class: css.New(
			u.Flex, u.ItemsCenter, css.FlexWrap.Wrap,
			css.Gap(css.Px(tokens.Spacing.SM)),
			css.PaddingY(css.Px(tokens.Spacing.XS)),
		).String(),
	}, children...)
}

func reviewBindingSummary(bindings ReviewBindingView, mode primitives.Mode) ui.Node {
	knownRevision := func(value uint64) string {
		if value == 0 {
			return "Unknown"
		}
		return strconv.FormatUint(value, 10)
	}
	return html.Tag("dl", html.Props{
		Aria:  map[string]string{"label": "Review revision bindings"},
		Data:  map[string]string{"component": "review-revision-bindings"},
		Class: css.New(u.Grid, css.Gap(css.Px(mode.Tokens().Spacing.XS))).String(),
	},
		html.Tag("dt", html.Props{Text: "Task revision"}), html.Tag("dd", html.Props{Text: knownRevision(bindings.Task)}),
		html.Tag("dt", html.Props{Text: "Diff revision"}), html.Tag("dd", html.Props{Text: knownRevision(bindings.Diff)}),
		html.Tag("dt", html.Props{Text: "Plan revision"}), html.Tag("dd", html.Props{Text: knownRevision(bindings.Plan)}),
		html.Tag("dt", html.Props{Text: "Validation revision"}), html.Tag("dd", html.Props{Text: knownRevision(bindings.Validation)}),
		html.Tag("dt", html.Props{Text: "Evidence revision"}), html.Tag("dd", html.Props{Text: knownRevision(bindings.Evidence)}),
	)
}

func reviewDecisionControls(props ReviewDecisionProps, mode primitives.Mode) ui.Node {
	return html.Div(html.Props{
		Role: "group", Aria: map[string]string{"label": "Review decisions"},
		Data: map[string]string{"component": "review-decision-controls"},
	},
		reviewDecisionButton("accept", "Accept", props.Accept, mode, props.OnAccept),
		reviewDecisionButton("repair", "Request repair", props.Repair, mode, props.OnRepair),
		reviewDecisionButton("reject", "Reject and preserve patch", props.Reject, mode, props.OnReject),
		reviewDecisionButton("rollback", "Roll back", props.Rollback, mode, props.OnRollback),
	)
}

func reviewDecisionButton(
	id, label string,
	command timelineview.ActionCommandState,
	mode primitives.Mode,
	onClick func(),
) ui.Node {
	reason := strings.TrimSpace(command.DisabledReason)
	if onClick == nil && reason == "" {
		reason = "This review command is not currently available."
	}
	reasonID := "review-command-" + id + "-reason"
	children := []ui.Node{primitives.Button(primitives.ButtonProps{
		ID: "review-command-" + id, Label: label, Busy: command.Busy,
		Disabled:    command.Busy || reason != "" || onClick == nil,
		DescribedBy: map[bool]string{true: reasonID}[reason != ""],
		Mode:        mode, OnClick: onClick,
	})}
	if reason != "" {
		children = append(children, html.P(html.Props{ID: reasonID, Text: reason}))
	}
	return html.Div(html.Props{Data: map[string]string{
		"review-command": id, "command-key": command.IdempotencyKey,
		"transport-mode": command.TransportMode, "disabled-reason": reason,
	}}, children...)
}
