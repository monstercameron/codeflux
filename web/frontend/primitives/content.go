package primitives

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type CopyButtonProps struct {
	Label    string
	Disabled bool
	Busy     bool
	Mode     Mode
	OnCopy   func()
}

func CopyButton(props CopyButtonProps) ui.Node {
	label := props.Label
	if strings.TrimSpace(label) == "" {
		label = "Copy"
	}
	return Button(ButtonProps{
		Label:           label,
		AccessibleLabel: label,
		Disabled:        props.Disabled,
		Busy:            props.Busy,
		Mode:            props.Mode,
		OnClick:         props.OnCopy,
	})
}

type CodeBlockProps struct {
	Label  string
	Code   string
	Mode   Mode
	OnCopy func()
}

func CodeBlock(props CodeBlockProps) ui.Node {
	if strings.TrimSpace(props.Label) == "" {
		return contractError("code-block", "accessible label is required")
	}
	tokens := props.Mode.Tokens()
	return html.Figure(
		html.Props{
			Data:  map[string]string{"component": "code-block"},
			Class: surfaceClass(tokens),
		},
		html.Figcaption(html.Props{
			Class: css.New(
				css.TextColor(css.Hex(string(tokens.Colors.TextSecondary))),
				css.FontSize(css.Px(tokens.Typography.PanelHeading.Size)),
				css.LineHeightLen(css.Px(tokens.Typography.PanelHeading.LineHeight)),
				css.FontWeight.Semibold,
			).String(),
		}, html.Text(props.Label)),
		CopyButton(CopyButtonProps{Label: "Copy code", Mode: props.Mode, OnCopy: props.OnCopy}),
		html.Pre(
			html.Props{
				TabIndex: html.TabIndexZero,
				Aria:     map[string]string{"label": props.Label},
				Class: css.New(
					css.Margin(css.Zero), css.Padding(css.Px(tokens.Spacing.MD)),
					css.Bg(css.Hex(string(tokens.Colors.SurfaceInset))),
					css.Border(css.Px(tokens.Geometry.BorderWidth), css.Hex(string(tokens.Colors.BorderSubtle))),
					css.Rounded(css.Px(tokens.Geometry.RadiusSmall)),
					css.Overflow.Auto,
					css.Font(css.FontStack(tokens.Fonts.Code)),
					css.FontSize(css.Px(tokens.Typography.Code.Size)),
					css.LineHeightLen(css.Px(tokens.Typography.Code.LineHeight)),
				).String(),
			},
			html.Code(html.Props{}, html.Text(props.Code)),
		),
	)
}
