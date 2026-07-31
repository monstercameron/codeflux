// Package telemetryview renders the content-free local telemetry controls in
// Settings. It receives already-redacted closed-schema rows only.
package telemetryview

import (
	"fmt"
	"time"

	"codeflux.dev/codeflux/web/frontend/primitives"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type Row struct {
	LocalID   uint64
	Kind      string
	Outcome   string
	Component string
	Occurred  time.Time
	Duration  time.Duration
}

type Props struct {
	Mode               primitives.Mode
	Loading            bool
	Busy               bool
	Error              bool
	Rows               []Row
	HasMore            bool
	OnReload           func()
	OnLoadMore         func()
	DeleteConfirmation bool
	OnDeleteRequest    func()
	OnDeleteConfirm    func()
	OnDeleteCancel     func()
}

func Component(props Props) ui.Node {
	children := []ui.Node{
		html.P(html.Props{Text: "Local product-use measurements contain event categories, timing, and status only. They never include keystrokes, prompts, source, tool output, or hidden reasoning."}),
	}
	switch {
	case props.Loading:
		children = append(children, html.P(html.Props{Text: "Loading local telemetry…", Aria: map[string]string{"live": "polite"}}))
	case props.Error:
		children = append(children,
			html.P(html.Props{Text: "Local telemetry could not be loaded. Existing data was not changed.", Aria: map[string]string{"live": "polite"}}),
			primitives.Button(primitives.ButtonProps{Label: "Retry telemetry", Mode: props.Mode, Disabled: props.OnReload == nil, OnClick: props.OnReload}),
		)
	case len(props.Rows) == 0:
		children = append(children, html.P(html.Props{Text: "No local telemetry has been recorded."}))
	default:
		items := make([]ui.Node, 0, len(props.Rows))
		for _, row := range props.Rows {
			label := fmt.Sprintf("%s · %s · %s · %s", row.Kind, row.Outcome, row.Component, row.Occurred.UTC().Format(time.RFC3339))
			if row.Duration > 0 {
				label += " · " + row.Duration.String()
			}
			items = append(items, html.Li(html.Props{
				Text: label,
				Data: map[string]string{"telemetry-id": fmt.Sprintf("%d", row.LocalID)},
			}))
		}
		children = append(children, html.Ul(html.Props{Aria: map[string]string{"label": "Local telemetry events"}}, items...))
		if props.HasMore {
			children = append(children, primitives.Button(primitives.ButtonProps{
				Label: "Load older telemetry", Mode: props.Mode, Busy: props.Busy,
				Disabled: props.OnLoadMore == nil, OnClick: props.OnLoadMore,
			}))
		}
	}
	if props.DeleteConfirmation {
		children = append(children,
			html.P(html.Props{Text: "Delete every local telemetry event? This cannot be undone.", Aria: map[string]string{"live": "polite"}}),
			primitives.Button(primitives.ButtonProps{
				Label: "Confirm telemetry deletion", Mode: props.Mode, Busy: props.Busy,
				Disabled: props.OnDeleteConfirm == nil, OnClick: props.OnDeleteConfirm,
			}),
			primitives.Button(primitives.ButtonProps{
				Label: "Cancel deletion", Mode: props.Mode, Disabled: props.Busy || props.OnDeleteCancel == nil,
				OnClick: props.OnDeleteCancel,
			}),
		)
	} else {
		children = append(children, primitives.Button(primitives.ButtonProps{
			Label: "Delete all local telemetry", AccessibleLabel: "Delete all local product telemetry",
			Mode: props.Mode, Busy: props.Busy, Disabled: props.OnDeleteRequest == nil || len(props.Rows) == 0,
			OnClick: props.OnDeleteRequest,
		}))
	}
	return html.Div(html.Props{Data: map[string]string{"component": "telemetry-settings"}}, children...)
}
