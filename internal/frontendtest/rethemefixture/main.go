//go:build js && wasm

// Command rethemefixture mounts an isolated, non-authoritative demonstration
// of the retuned design tokens (ground+emerald dark theme, repaired light
// theme, and the new per-kind icon-chip palette) for browser screenshot
// verification. It renders no application state and imports only the
// design/primitives packages the retheme touched.
package main

import (
	"fmt"

	"codeflux.dev/codeflux/web/frontend/design"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/css/u"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/GoWebComponents/v5/utils"
)

func main() {
	ui.Render(ui.CreateElement(rethemeFixture), "#app")
	utils.WaitForever()
}

func rethemeFixture() ui.Node {
	return html.Main(
		html.Props{DataAttr: html.DataAttribute{Name: "testid", Value: "retheme-fixture"}},
		themePanel(design.ThemeDark),
		themePanel(design.ThemeLight),
	)
}

type demoCard struct {
	kind    design.Kind
	title   string
	body    string
	status  design.Status
	statusL string
}

var demoCards = []demoCard{
	{design.KindForecast, "Forecast: idempotent order creation", "Estimated effort is 2.1 min to implement, verify, and validate idempotency.", design.StatusSuccess, "High confidence"},
	{design.KindPlan, "Plan: implement idempotency", "Design idempotency model, add repository schema, and cover retries with tests.", design.StatusPending, "Approval pending"},
	{design.KindExecution, "Execution: running tests", "Implementing changes and running the idempotency test suite.", design.StatusActive, "Running"},
	{design.KindValidation, "Validation: correctness gates", "Validation tests, security review, and effect analysis all passed.", design.StatusSuccess, "Passed"},
	{design.KindEvidence, "Evidence: idempotency proof", "Idempotency implemented and verified; evidence recorded and indexed.", design.StatusEvidence, "Recorded"},
	{design.KindCode, "Code: order repository", "Order repository and idempotency store changes are complete.", design.StatusSuccess, "Complete"},
	{design.KindTest, "Test: duplicate request handling", "128 tests passed, 0 failed, coverage 94%.", design.StatusSuccess, "128 passed"},
	{design.KindMemory, "Memory: prior incident", "A related prior task informs this retry strategy.", design.StatusNeutral, "Referenced"},
}

func themePanel(theme design.Theme) ui.Node {
	mode := primitives.Mode{Theme: theme, Density: design.DensityComfortable}
	tokens := mode.Tokens()
	cards := make([]ui.Node, 0, len(demoCards))
	for index, demo := range demoCards {
		cards = append(cards, demoCardNode(mode, tokens, index, demo))
	}
	return html.Section(
		html.Props{
			Aria: map[string]string{"label": fmt.Sprintf("%s theme demonstration", theme)},
			Data: map[string]string{"component": "retheme-panel", "theme": string(theme)},
			Class: css.New(
				u.Flex, u.FlexCol,
				css.Gap(css.Px(tokens.Spacing.LG)),
				css.Padding(css.Px(tokens.Spacing.XL)),
				css.Bg(css.Hex(string(tokens.Colors.Canvas))),
				css.Font(css.FontStack(tokens.Fonts.UI)),
			).String(),
		},
		themeHeader(mode, tokens, theme),
		html.Div(
			html.Props{
				Class: css.New(
					u.Grid,
					css.GridCols(css.RepeatFit(css.MinMax(css.TrackLen(css.Px(200)), css.Fr(1)))),
					css.Gap(css.Px(tokens.Spacing.MD)),
				).String(),
			},
			cards...,
		),
	)
}

func themeHeader(mode primitives.Mode, tokens design.Tokens, theme design.Theme) ui.Node {
	return html.Header(
		html.Props{
			Class: css.New(
				u.Flex, u.ItemsCenter, u.JustifyBetween,
				css.Gap(css.Px(tokens.Spacing.MD)),
				css.PaddingY(css.Px(tokens.Spacing.SM)),
				css.BorderBottom(css.Px(tokens.Geometry.BorderWidth), css.Hex(string(tokens.Colors.BorderSubtle))),
			).String(),
		},
		html.H1(
			html.Props{
				Class: css.New(
					css.Margin(css.Zero),
					css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
					css.FontSize(css.Px(tokens.Typography.WorkspaceTitle.Size)),
					css.LineHeightLen(css.Px(tokens.Typography.WorkspaceTitle.LineHeight)),
					css.FontWeight.Semibold,
				).String(),
			},
			html.Text(fmt.Sprintf("Codeflux — %s theme", theme)),
		),
		html.Div(
			html.Props{Class: css.New(u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.SM))).String()},
			primitives.Button(primitives.ButtonProps{Label: "New Task", Primary: true, Mode: mode}),
			primitives.Badge(primitives.BadgeProps{Label: "All Systems Operational", Status: design.StatusSuccess, Mode: mode}),
		),
	)
}

func demoCardNode(mode primitives.Mode, tokens design.Tokens, index int, demo demoCard) ui.Node {
	return html.Article(
		html.Props{
			Key: fmt.Sprintf("card-%d", index),
			Data: map[string]string{
				"component": "retheme-demo-card",
				"kind":      string(demo.kind),
			},
			Class: css.New(
				u.Flex, u.FlexCol,
				css.Gap(css.Px(tokens.Spacing.SM)),
				css.Padding(css.Px(tokens.Rhythm.PanelInset)),
				css.Bg(css.Hex(string(tokens.Colors.Surface1))),
				css.Border(css.Px(tokens.Geometry.BorderWidth), css.Hex(string(tokens.Colors.BorderSubtle))),
				css.Rounded(css.Px(tokens.Geometry.PanelRadius)),
			).String(),
		},
		html.Div(
			html.Props{Class: css.New(u.Flex, u.ItemsCenter, u.JustifyBetween).String()},
			primitives.IconChip(primitives.IconChipProps{Label: kindLabel(demo.kind), Kind: demo.kind, Mode: mode}),
			primitives.Badge(primitives.BadgeProps{Label: demo.statusL, Status: demo.status, Mode: mode}),
		),
		html.H2(
			html.Props{
				Class: css.New(
					css.Margin(css.Zero),
					css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
					css.FontSize(css.Px(tokens.Typography.PanelHeading.Size)),
					css.LineHeightLen(css.Px(tokens.Typography.PanelHeading.LineHeight)),
					css.FontWeight.Semibold,
				).String(),
			},
			html.Text(demo.title),
		),
		html.P(
			html.Props{
				Class: css.New(
					css.Margin(css.Zero),
					css.TextColor(css.Hex(string(tokens.Colors.TextSecondary))),
					css.FontSize(css.Px(tokens.Typography.Body.Size)),
					css.LineHeightLen(css.Px(tokens.Typography.Body.LineHeight)),
				).String(),
			},
			html.Text(demo.body),
		),
	)
}

func kindLabel(kind design.Kind) string {
	switch kind {
	case design.KindCode:
		return "Code"
	case design.KindTest:
		return "Test"
	case design.KindPlan:
		return "Plan"
	case design.KindEvidence:
		return "Evidence"
	case design.KindMemory:
		return "Memory"
	case design.KindForecast:
		return "Forecast"
	case design.KindExecution:
		return "Execution"
	case design.KindValidation:
		return "Validation"
	default:
		return string(kind)
	}
}
