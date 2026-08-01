package shell

import (
	"strconv"
	"strings"

	"codeflux.dev/codeflux/web/frontend/design"
	"codeflux.dev/codeflux/web/frontend/graphcanvas"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"codeflux.dev/codeflux/web/frontend/routes"
	"codeflux.dev/codeflux/web/frontend/state"
	"github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/css/u"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func applicationFrameClass(
	layout state.LayoutPreferences,
	tokens design.Tokens,
	inspectorCollapsed bool,
) string {
	layout = layout.Normalize()
	tracks := []css.Track{css.MinMax(css.TrackLen(css.Zero), css.Fr(1))}
	switch layout.Viewport {
	case state.ViewportWide:
		if layout.RailCollapsed {
			tracks = []css.Track{css.MinMax(css.TrackLen(css.Zero), css.Fr(1))}
		} else {
			tracks = []css.Track{
				css.TrackLen(css.Px(layout.RailWidth)),
				css.MinMax(css.TrackLen(css.Zero), css.Fr(1)),
			}
		}
		if !inspectorCollapsed {
			tracks = append(tracks, css.TrackLen(css.Px(316)))
		}
	}
	return css.New(
		u.Relative,
		u.Grid,
		css.GridCols(tracks...),
		css.W(css.Full),
		// Dynamic viewport units follow the visual viewport as a software
		// keyboard opens, keeping the grid's anchored composer reachable.
		css.H(css.RawLength("calc(100dvh - 64px)")),
		css.MinWidth(css.Zero),
		css.MinHeight(css.Zero),
		css.Overflow.Hidden,
		css.Bg(css.Hex(string(tokens.Colors.Canvas))),
	).String()
}

func routeFrameClass(layout state.LayoutPreferences) string {
	layout = layout.Normalize()
	rules := []css.Rule{
		css.MinWidth(css.Zero),
		css.MinHeight(css.Zero),
		css.W(css.Full),
		css.H(css.Full),
		css.Overflow.Hidden,
	}
	if layout.Viewport == state.ViewportMedium && !layout.RailCollapsed {
		rules = append(rules, css.Padding(css.RawLength(
			"0 0 0 "+strconv.Itoa(layout.RailWidth)+"px",
		)))
	}
	return css.New(rules...).String()
}

func ApplicationBar(props ApplicationBarProps) ui.Node {
	recordRender(props.Probe, "application-bar", props.Revision)
	shortcutHelpHandler := ui.UseCallback(func() {
		if props.OnShortcutHelp != nil {
			props.OnShortcutHelp()
		}
	})
	tokens := props.Mode.Tokens()
	compact := props.Viewport == state.ViewportNarrow || props.Viewport == state.ViewportMinimum
	tracks := []css.Track{
		css.TrackLen(css.Px(410)),
		css.MinMax(css.TrackLen(css.Zero), css.Fr(1)),
		css.TrackLen(css.Px(390)),
	}
	if props.Viewport == state.ViewportWide {
		tracks = []css.Track{
			css.TrackLen(css.Px(350)),
			css.MinMax(css.TrackLen(css.Zero), css.Fr(1)),
			css.TrackLen(css.Px(560)),
		}
	}
	if compact {
		tracks = []css.Track{
			css.MinMax(css.TrackLen(css.Zero), css.Fr(1)),
			css.TrackAuto,
		}
	}
	repository := fallback(props.View.Repository, props.Workspace.RepositoryName)
	if repository == "" {
		repository = "Choose repository"
	}
	branch := fallback(props.View.Branch, props.Workspace.Branch)
	if branch == "" {
		branch = "branch"
	}
	worktree := props.View.WorktreeStatus
	if worktree == "" {
		if props.Workspace.Dirty {
			worktree = "uncommitted changes"
		} else {
			worktree = "worktree status"
		}
	}
	provider := fallback(props.View.Provider, "provider")
	model := fallback(props.View.Model, "model")
	effort := fallback(props.View.Effort, "effort")
	forecast := fallback(props.View.ForecastCost, "forecast")
	tokensUsed := fallback(props.View.ActualTokens, "usage")
	actual := fallback(props.View.ActualCost, fallback(props.CostLabel, "actual"))
	pricing := fallback(props.View.PricingSnapshot, "pricing snapshot")
	budget := fallback(props.View.HardBudget, "budget")
	remaining := fallback(props.View.RemainingBudget, "remaining")
	warning := fallback(props.View.BudgetWarning, "threshold")
	connection := string(props.View.Connection)
	if connection == "" {
		connection = string(props.Session.Connection)
	}
	return html.Header(html.Props{
		Data: map[string]string{
			"component": "application-bar",
			"revision":  strconv.FormatUint(props.Revision, 10),
		},
		Aria:  map[string]string{"label": "Application"},
		Class: applicationBarClass(tracks, tokens),
	},
		html.Div(html.Props{
			Class: css.New(u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.MD))).String(),
		},
			brandMark(tokens),
			html.Strong(html.Props{
				Class: css.New(
					css.FontSize(css.Px(21)),
					css.FontWeight.Semibold,
					css.Tracking(css.Ems(-0.02)),
					css.Font(css.FontStack(tokens.Fonts.Display)),
					css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
				).String(),
				Text: "Codeflux",
			}),
			html.Div(html.Props{},
				primitives.Button(primitives.ButtonProps{
					ID: "thread-rail-toggle", Label: "☰", AccessibleLabel: "Toggle thread rail",
					Expanded: &props.RailOpen, Controls: "product-sidebar-navigation",
					Mode: props.Mode, Disabled: props.OnRailToggle == nil, OnClick: props.OnRailToggle,
				}),
			),
			html.Span(html.Props{
				Hidden: compact,
				Class: desktopOnlyClass(
					css.H(css.Px(24)),
					css.BorderLeft(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
				),
				Aria: map[string]string{"hidden": "true"},
			}),
			html.Span(html.Props{
				Hidden: compact,
				Class: desktopOnlyClass(
					css.FontSize(css.Px(tokens.Typography.CompactBody.Size)),
					css.TextColor(css.Hex(string(tokens.Colors.TextSecondary))),
				),
				Text: "Correctness. Evidence. Progress.",
			}),
		),
		html.Div(html.Props{
			Hidden: compact,
			Aria:   map[string]string{"label": "Repository context"},
			Class: desktopOnlyClass(
				u.Flex, u.ItemsCenter, u.JustifyCenter, css.Gap(css.Px(tokens.Spacing.SM)),
			),
		},
			contextControl("repository", "▣", repository, "/", props.Mode, props.OnNavigatePath),
			contextControl("branch", "⑂", branch, "/settings", props.Mode, props.OnNavigatePath),
			contextControl("worktree", "✓", worktree, "/settings", props.Mode, props.OnNavigatePath),
			html.Div(html.Props{Class: wideOnlyClass()},
				contextControl("model", "◈", provider+" · "+model+" · "+effort, "/settings", props.Mode, props.OnNavigatePath),
			),
			html.Div(html.Props{Class: wideOnlyClass()},
				// The header carries the two figures that decide whether to
				// intervene: what this has cost, and what is left of the cap.
				// The forecast, token count, pricing snapshot and warning
				// threshold used to be concatenated into the same control with
				// slashes, which produced an unreadable run of seven values and
				// meant neither number could be found at a glance. They are
				// still reachable — the control opens the settings page, and the
				// full breakdown is its accessible name.
				costReadout(costReadoutProps{
					Spent:     actual,
					Remaining: remaining,
					Breakdown: "forecast " + forecast + ", usage " + tokensUsed +
						", actual " + actual + ", pricing " + pricing +
						", budget " + budget + ", remaining " + remaining +
						", warning threshold " + warning,
					Mode:       props.Mode,
					OnNavigate: props.OnNavigatePath,
					TargetPath: "/settings",
				}),
			),
		),
		html.Div(html.Props{
			Aria:  map[string]string{"label": "Session controls"},
			Class: css.New(u.Flex, u.ItemsCenter, u.JustifyEnd, css.Gap(css.Px(tokens.Spacing.SM))).String(),
		},
			html.Span(html.Props{
				Hidden: compact,
				Data:   map[string]string{"connection": connection},
				Class: desktopOnlyClass(
					u.InlineFlex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.XS)),
					css.PaddingX(css.Px(tokens.Spacing.SM)),
					css.MinHeight(css.Px(32)),
					css.TextColor(css.Hex(string(tokens.Colors.Success))),
					css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
					css.FontWeight.Medium,
				),
				Text: "●  Local " + humanize(connection),
			}),
			manualReconnectControl(props, connection),
			headerIconButtonWithID("global-search-trigger", "⌕", "Search", props.Mode, props.OnSearchOpen),
			headerIconButton("◐", "Change color theme", props.Mode, props.OnThemeChange),
			primitives.Button(primitives.ButtonProps{
				ID: "shortcut-help-trigger", Label: "?", AccessibleLabel: "Shortcut help", Mode: props.Mode,
				Disabled: props.OnShortcutHelp == nil, OnClick: shortcutHelpHandler,
			}),
			html.Div(html.Props{Class: wideOnlyClass()},
				headerIconButtonWithID(
					"assurance-rail-toggle", "▥", "Toggle task details sidebar", props.Mode, props.OnInspectorToggle,
				),
			),
			html.Span(html.Props{
				Hidden: compact,
				Aria:   map[string]string{"label": "Current user"},
				Class: desktopOnlyClass(
					u.InlineFlex, u.ItemsCenter, u.JustifyCenter,
					css.W(css.Px(34)), css.H(css.Px(34)),
					css.Rounded(css.Px(tokens.Geometry.PillRadius)),
					css.Bg(css.Hex(string(tokens.Colors.Surface3))),
					css.Border(css.Px(1), css.Hex(string(tokens.Colors.BorderStrong))),
					css.FontSize(css.Px(tokens.Typography.ControlLabel.Size)),
				),
				Text: "CF",
			}),
		),
		ui.CreateElement(SearchDialog, SearchDialogProps{
			Open: props.SearchOpen, Query: props.SearchQuery, Mode: props.Mode,
			OnDismiss: props.OnSearchDismiss, OnQueryChange: props.OnSearchQueryChange,
			OnNavigatePath: props.OnNavigatePath,
		}),
	)
}

func manualReconnectControl(props ApplicationBarProps, connection string) ui.Node {
	if connection != string(state.ConnectionDisconnected) || props.OnReconnectRequested == nil {
		return nil
	}
	return primitives.Button(primitives.ButtonProps{
		ID: "session-reconnect", Label: "Reconnect", AccessibleLabel: "Reconnect live session",
		Mode: props.Mode, OnClick: props.OnReconnectRequested,
	})
}

func applicationBarClass(tracks []css.Track, tokens design.Tokens) string {
	rules := []css.Rule{
		u.Grid,
		css.GridCols(tracks...),
		u.ItemsCenter,
		css.Gap(css.Px(tokens.Spacing.LG)),
		css.W(css.Full),
		css.MinHeight(css.Px(64)),
		css.PaddingX(css.Px(tokens.Spacing.XL)),
		css.Bg(css.Hex(string(tokens.Colors.Shell))),
		css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
		css.BorderBottom(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
		css.Shadow(css.ShadowOf(
			css.Zero, css.Px(8), css.Px(30), css.Px(-24), css.RGBA(0, 0, 0, 0.55),
		)),
	}
	rules = append(rules, css.Media(
		css.MaxW(1179),
		css.GridCols(
			css.MinMax(css.TrackLen(css.Zero), css.Fr(1)),
			css.TrackAuto,
		),
	)...)
	return css.New(rules...).String()
}

func desktopOnlyClass(rules ...css.Rule) string {
	rules = append(rules, css.Media(css.MaxW(1179), css.Display.None)...)
	return css.New(rules...).String()
}

func wideOnlyClass(rules ...css.Rule) string {
	rules = append(rules, css.Media(css.MaxW(1439), css.Display.None)...)
	return css.New(rules...).String()
}

func brandMark(tokens design.Tokens) ui.Node {
	return html.Span(html.Props{
		Aria: map[string]string{"hidden": "true"},
		Class: css.New(
			u.InlineFlex, u.ItemsCenter, u.JustifyCenter,
			css.W(css.Px(34)), css.H(css.Px(34)),
			css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
			css.Bg(css.Hex(string(tokens.Colors.Accent))),
			css.Border(css.Px(1), css.Hex(string(tokens.Colors.AccentHover))),
			css.TextColor(css.Hex(string(tokens.Colors.OnAccent))),
			css.FontSize(css.Px(18)),
			css.FontWeight.Bold,
			css.Shadow(css.ShadowOf(
				css.Zero, css.Px(8), css.Px(18), css.Px(-10),
				css.RGBA(30, 110, 170, 0.6),
			)),
		).String(),
		Text: "⌁",
	})
}

func contextControl(
	kind string,
	icon string,
	label string,
	targetPath string,
	mode primitives.Mode,
	onNavigate func(string),
) ui.Node {
	tokens := mode.Tokens()
	return html.Details(html.Props{
		Data:  map[string]string{"component": "context-option", "kind": kind},
		Class: css.New(u.Relative).String(),
	},
		html.Summary(html.Props{
			Aria: map[string]string{"label": label},
			Class: css.New(
				u.InlineFlex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.SM)),
				css.MinHeight(css.Px(34)),
				css.PaddingX(css.Px(tokens.Spacing.MD)),
				css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
				css.Bg(css.Hex(string(tokens.Colors.Surface2))),
				css.TextColor(css.Hex(string(tokens.Colors.TextSecondary))),
				css.Border(css.Px(1), css.Transparent),
				css.FontSize(css.Px(tokens.Typography.ControlLabel.Size)),
				css.Cursor.Pointer,
			).String(),
			Text: icon + "  " + label + "  ⌄",
		}),
		html.Div(html.Props{
			Role: "group", Aria: map[string]string{"label": label + " options"},
			Data: map[string]string{"component": "context-option-panel"},
			Class: css.New(
				u.Absolute,
				css.Top(css.RawLength("calc(100% + 4px)")), css.Left(css.Zero), css.ZIndex(40),
				css.MinWidth(css.Px(220)),
				css.Padding(css.Px(tokens.Spacing.MD)),
				css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
				css.Bg(css.Hex(string(tokens.Colors.SurfaceRaised))),
				css.Border(css.Px(1), css.Hex(string(tokens.Colors.BorderStrong))),
				css.Shadow(css.ShadowOf(
					css.Zero, css.Px(8), css.Px(24), css.Zero, css.RGBA(0, 0, 0, 0.28),
				)),
			).String(),
		},
			html.Strong(html.Props{Text: label}),
			html.P(html.Props{
				Class: css.New(
					css.MarginY(css.Px(tokens.Spacing.SM)),
					css.TextColor(css.Hex(string(tokens.Colors.TextSecondary))),
					css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				).String(),
				Text: "Current " + kind + " selection",
			}),
			primitives.Button(primitives.ButtonProps{
				Label: "Open " + map[bool]string{true: "repositories", false: "settings"}[targetPath == "/"],
				Mode:  mode, Disabled: onNavigate == nil,
				OnClick: func() {
					if onNavigate != nil {
						onNavigate(targetPath)
					}
				},
			}),
		),
	)
}

// costReadoutProps configures the header's money readout.
type costReadoutProps struct {
	Spent      string
	Remaining  string
	Breakdown  string
	Mode       primitives.Mode
	OnNavigate func(string)
	TargetPath string
}

// costReadout renders spend against the remaining cap.
//
// Both figures are measurements, so they are set in the monospace face with
// tabular figures: they change while you watch them, and digits that shift
// their own width as they change are hard to read at a glance. The labels are
// sans, because a label is chrome rather than a value.
func costReadout(props costReadoutProps) ui.Node {
	tokens := props.Mode.Tokens()
	pair := func(label, value string, tone design.Color) ui.Node {
		return html.Span(html.Props{
			Class: css.New(
				u.InlineFlex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.XS)),
			).String(),
		},
			html.Span(html.Props{
				Class: css.New(
					css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
					css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				).String(),
				Text: label,
			}),
			html.Span(html.Props{
				Class: css.New(
					css.TextColor(css.Hex(string(tone))),
					css.Font(css.FontStack(tokens.Fonts.Code)),
					css.FontSize(css.Px(tokens.Typography.ControlLabel.Size)),
					css.FontWeight.Medium,
				).String(),
				Text: value,
			}),
		)
	}
	return html.Button(html.Props{
		Type: "button",
		Data: map[string]string{"component": "cost-readout"},
		Aria: map[string]string{"label": "Cost and budget: " + props.Breakdown},
		Class: css.New(
			u.InlineFlex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.MD)),
			css.MinHeight(css.Px(34)),
			css.PaddingX(css.Px(tokens.Spacing.MD)),
			css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
			css.Bg(css.Hex(string(tokens.Colors.Surface2))),
			css.Border(css.Px(1), css.Transparent),
			css.Cursor.Pointer,
		).String(),
		OnClick: ui.WrapHandler(func() {
			if props.OnNavigate != nil {
				props.OnNavigate(props.TargetPath)
			}
		}),
	},
		pair("spent", props.Spent, tokens.Colors.TextPrimary),
		html.Span(html.Props{
			Aria: map[string]string{"hidden": "true"},
			Class: css.New(
				css.H(css.Px(16)),
				css.BorderLeft(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
			).String(),
		}),
		pair("left", props.Remaining, tokens.Colors.Accent),
	)
}

func headerIconButton(label, accessible string, mode primitives.Mode, handler func()) ui.Node {
	return primitives.Button(primitives.ButtonProps{
		Label: label, AccessibleLabel: accessible, Mode: mode,
		Disabled: handler == nil, OnClick: handler,
	})
}

func headerIconButtonWithID(
	id string,
	label string,
	accessible string,
	mode primitives.Mode,
	handler func(),
) ui.Node {
	return primitives.Button(primitives.ButtonProps{
		ID: id, Label: label, AccessibleLabel: accessible, Mode: mode,
		Disabled: handler == nil, OnClick: handler,
	})
}

type SearchDialogProps struct {
	Open           bool
	Query          string
	Mode           primitives.Mode
	OnDismiss      func()
	OnQueryChange  func(string)
	OnNavigatePath func(string)
}

func SearchDialog(props SearchDialogProps) ui.Node {
	tokens := props.Mode.Tokens()
	searchProps := html.PropsOf(
		html.OnInput(func(event ui.InputEvent) {
			if props.OnQueryChange != nil {
				props.OnQueryChange(event.GetValue())
			}
		}),
	)
	searchProps.ID = "global-search-input"
	searchProps.Type = "search"
	searchProps.Placeholder = "Search tasks, graphs, memory, or repositories"
	searchProps.Aria = map[string]string{
		"label": "Search Codeflux",
	}
	searchProps.Data = map[string]string{"component": "global-search-input"}
	searchProps.Class = css.New(
		css.W(css.Full), css.MinHeight(css.Px(44)),
		css.PaddingX(css.Px(tokens.Spacing.MD)),
		css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
		css.Bg(css.Hex(string(tokens.Colors.SurfaceInset))),
		css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
		css.Border(css.Px(1), css.Hex(string(tokens.Colors.BorderStrong))),
	).String()
	destinations := []struct {
		glyph string
		label string
		path  string
	}{
		{glyph: "▣", label: "Search tasks", path: "/tasks"},
		{glyph: "⌘", label: "Search task graph", path: "/graphs"},
		{glyph: "◫", label: "Search memory", path: "/memory"},
		{glyph: "▤", label: "Search repositories", path: "/"},
	}
	query := strings.ToLower(strings.TrimSpace(props.Query))
	results := make([]ui.Node, 0, len(destinations))
	for _, destination := range destinations {
		destination := destination
		if query != "" && !strings.Contains(strings.ToLower(destination.label), query) {
			continue
		}
		results = append(results, primitives.Button(primitives.ButtonProps{
			Label:           destination.glyph + "  " + destination.label,
			AccessibleLabel: destination.label,
			Mode:            props.Mode, Disabled: props.OnNavigatePath == nil,
			OnClick: func() {
				if props.OnDismiss != nil {
					props.OnDismiss()
				}
				if props.OnNavigatePath != nil {
					props.OnNavigatePath(destination.path)
				}
			},
		}))
	}
	if len(results) == 0 {
		results = append(results, html.P(html.Props{
			Role: "status", Text: "No matching search area. Try tasks, graph, memory, or repositories.",
			Class: css.New(css.TextColor(css.Hex(string(tokens.Colors.TextSecondary)))).String(),
		}))
	}
	return primitives.Dialog(primitives.OverlayProps{
		ID: "global-search-dialog", Open: props.Open,
		LabelledBy: "global-search-title", DescribedBy: "global-search-description",
		InitialFocusSelector: "#global-search-input", AppRootSelector: `[data-component="app-shell"]`,
		Mode: props.Mode, OnDismiss: props.OnDismiss,
		Content: html.Section(html.Props{
			Data:  map[string]string{"component": "global-search"},
			Class: css.New(u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.MD))).String(),
		},
			html.Div(html.Props{Class: css.New(u.Flex, u.ItemsCenter, u.JustifyBetween, css.Gap(css.Px(tokens.Spacing.MD))).String()},
				html.Div(html.Props{},
					html.H2(html.Props{Class: design.HeadingClass(tokens, design.HeadingPanel), ID: "global-search-title", Text: "Search Codeflux"}),
					html.P(html.Props{
						ID: "global-search-description", Text: "Choose a scoped search destination. Your query stays local.",
						Class: css.New(css.TextColor(css.Hex(string(tokens.Colors.TextSecondary)))).String(),
					}),
				),
				primitives.Button(primitives.ButtonProps{
					Label: "×", AccessibleLabel: "Close search", Mode: props.Mode,
					Disabled: props.OnDismiss == nil, OnClick: props.OnDismiss,
				}),
			),
			html.Label(html.Props{For: "global-search-input", Text: "Search"}),
			html.Input(searchProps),
			html.Div(html.Props{
				Role: "group", Aria: map[string]string{"label": "Search destinations"},
				Class: css.New(u.Grid, css.GridCols(css.Repeat(2, css.MinMax(css.TrackLen(css.Zero), css.Fr(1)))), css.Gap(css.Px(tokens.Spacing.SM))).String(),
			}, results...),
		),
	})
}

type ProductSidebarProps struct {
	Snapshot         state.Snapshot
	Route            routes.Route
	Mode             primitives.Mode
	ThreadRail       ui.Node
	CompactOpen      bool
	OnThreadNavigate func(routes.Route)
	OnNavigatePath   func(string)
	OnCollapse       func()
	OnNarrower       func()
	OnWider          func()
}

func ProductSidebar(props ProductSidebarProps) ui.Node {
	layout := props.Snapshot.Layout.Normalize()
	compact := layout.Viewport == state.ViewportNarrow || layout.Viewport == state.ViewportMinimum
	hidden := layout.RailCollapsed
	if compact {
		hidden = !props.CompactOpen
	}
	tokens := props.Mode.Tokens()
	threadRailNode := props.ThreadRail
	if threadRailNode == nil {
		threadRailNode = ui.CreateElement(TypedThreadRailPreview, TypedThreadRailPreviewProps{
			Snapshot: props.Snapshot, Route: props.Route, Mode: props.Mode,
			OnNavigate: props.OnThreadNavigate,
		})
	}
	if hidden && !compact {
		return html.Nav(html.Props{
			Hidden: true,
			Aria:   map[string]string{"label": "Primary navigation"},
			Data: map[string]string{
				"component": "product-sidebar",
				"viewport":  string(layout.Viewport),
			},
		})
	}
	navigationLabel := "Primary navigation"
	if layout.Viewport == state.ViewportWide {
		navigationLabel = "Threads"
	}
	navItems := []struct {
		icon  string
		label string
		path  string
	}{
		{"⌂", "Home", "/"},
		{"▣", "Tasks", "/tasks"},
		{"⌘", "Graphs", "/graphs"},
		{"◫", "Memory", "/memory"},
		{"▤", "Repositories", "/"},
		{"⚙", "Settings", "/settings"},
	}
	items := make([]ui.Node, 0, len(navItems))
	for _, item := range navItems {
		selected := routeSelected(props.Route, item.label)
		path := item.path
		buttonProps := html.PropsOf(html.OnClick(func() {
			if props.OnNavigatePath != nil {
				props.OnNavigatePath(path)
			}
			if compact && props.OnCollapse != nil {
				props.OnCollapse()
			}
		}))
		buttonProps.Type = "button"
		buttonProps.Disabled = props.OnNavigatePath == nil
		buttonProps.Aria = map[string]string{"current": selectedAria(selected)}
		buttonProps.Data = map[string]string{"component": "client-route-control", "path": path}
		buttonProps.Class = sidebarLinkClass(tokens, selected)
		buttonProps.Text = item.icon + "   " + item.label
		items = append(items, html.Div(html.Props{},
			html.Button(buttonProps),
		))
	}
	navigation := html.Nav(html.Props{
		ID:   "product-sidebar-navigation",
		Aria: map[string]string{"label": navigationLabel},
		Data: map[string]string{
			"component": "product-sidebar",
			"viewport":  string(layout.Viewport),
			"overlay":   strconv.FormatBool(layout.Viewport == state.ViewportMedium),
			"width":     strconv.Itoa(layout.RailWidth),
		},
		Class: productSidebarClass(layout, tokens),
	},
		html.Div(html.Props{
			Aria:  map[string]string{"label": "Thread rail layout controls"},
			Class: css.New(u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.XS))).String(),
		},
			headerIconButtonWithID("product-sidebar-close", "×", "Collapse thread rail", props.Mode, props.OnCollapse),
			headerIconButton("‹", "Narrow thread rail", props.Mode, props.OnNarrower),
			headerIconButton("›", "Widen thread rail", props.Mode, props.OnWider),
		),
		html.H2(html.Props{
			ID: "product-sidebar-title", Text: navigationLabel,
			Class: shellAssistiveClass(),
		}),
		html.Div(html.Props{
			Class: css.New(
				css.MarginY(css.Px(tokens.Spacing.LG)),
				css.Padding(css.Zero),
			).String(),
		}, items...),
		html.Div(html.Props{
			Class: css.New(
				css.BorderTop(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
				css.PaddingY(css.Px(tokens.Spacing.LG)),
				css.FlexGrow(css.Num(1)),
			).String(),
		},
			threadRailNode,
		),
		html.Div(html.Props{
			Class: css.New(
				css.Padding(css.Px(tokens.Spacing.MD)),
				css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
				css.Bg(css.Hex(string(tokens.Colors.Surface1))),
				css.Border(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
			).String(),
		},
			html.Strong(html.Props{
				Class: css.New(
					css.FontSize(css.Px(tokens.Typography.ControlLabel.Size)),
					css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
				).String(),
				Text: "▣  Local data",
			}),
			html.P(html.Props{
				Class: css.New(
					css.MarginY(css.Px(tokens.Spacing.XS)),
					css.TextColor(css.Hex(string(tokens.Colors.Success))),
					css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				).String(),
				Text: "● Coordinator available",
			}),
			html.P(html.Props{
				Class: css.New(
					css.Margin(css.Zero),
					css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
					css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				).String(),
				Text: "SQLite · encrypted at rest",
			}),
		),
	)
	if !compact {
		return navigation
	}
	return primitives.Drawer(primitives.OverlayProps{
		ID: "product-sidebar-drawer", Open: props.CompactOpen,
		LabelledBy:           "product-sidebar-title",
		InitialFocusSelector: "#product-sidebar-close",
		AppRootSelector:      `[data-component="app-shell"]`,
		Mode:                 props.Mode,
		Content:              navigation,
		OnDismiss:            props.OnCollapse,
	})
}

func shellAssistiveClass() string {
	return css.New(
		css.Position.Absolute, css.W(css.Px(1)), css.H(css.Px(1)),
		css.Margin(css.Px(-1)), css.Padding(css.Zero), css.Overflow.Hidden,
	).String()
}

func productSidebarClass(layout state.LayoutPreferences, tokens design.Tokens) string {
	rules := []css.Rule{
		u.Flex, u.FlexCol,
		css.MinWidth(css.Zero), css.H(css.Full),
		css.PaddingY(css.Px(tokens.Spacing.LG)),
		css.PaddingX(css.Px(tokens.Spacing.MD)),
		css.Bg(css.Hex(string(tokens.Colors.Shell))),
		css.BorderRight(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
		css.OverflowY.Auto,
	}
	if layout.Viewport == state.ViewportMedium {
		rules = append(rules,
			u.Absolute,
			css.Left(css.Zero), css.Top(css.Zero), css.Bottom(css.Zero),
			css.W(css.Px(layout.RailWidth)),
			css.ZIndex(20),
			css.Shadow(css.ShadowOf(
				css.Px(8), css.Zero, css.Px(24), css.Zero, css.RGBA(0, 0, 0, 0.28),
			)),
		)
	}
	return css.New(rules...).String()
}

func routeSelected(route routes.Route, label string) bool {
	switch label {
	case "Home", "Repositories":
		return route.Name == routes.RepositoryChooser
	case "Tasks":
		return route.Name == routes.ThreadWorkspace
	case "Graphs":
		return route.Name == routes.Graphs
	case "Memory":
		return route.Name == routes.Memory
	case "Settings":
		return route.Name == routes.Settings
	}
	return false
}

func selectedAria(selected bool) string {
	if selected {
		return "page"
	}
	return ""
}

func sidebarLinkClass(tokens design.Tokens, selected bool) string {
	rules := []css.Rule{
		u.Flex, u.ItemsCenter,
		css.W(css.Full),
		css.MinHeight(css.Px(44)),
		css.MarginY(css.Px(2)),
		css.PaddingX(css.Px(tokens.Spacing.LG)),
		css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
		css.Bg(css.Transparent),
		css.Border(css.Zero, css.Transparent),
		css.TextColor(css.Hex(string(tokens.Colors.TextSecondary))),
		css.TextDecoration.None,
		css.FontSize(css.Px(tokens.Typography.Body.Size)),
		css.FontWeight.Medium,
		css.Cursor.Pointer,
	}
	if selected {
		rules = append(rules,
			css.Bg(css.Hex(string(tokens.Colors.Selection))),
			css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
			css.BorderLeft(css.Px(3), css.Hex(string(tokens.Colors.Accent))),
		)
	}
	return css.New(rules...).String()
}

type AssuranceRailProps struct {
	Snapshot   state.Snapshot
	Mode       primitives.Mode
	Collapsed  bool
	OnCollapse func()
}

func AssuranceRail(props AssuranceRailProps) ui.Node {
	layout := props.Snapshot.Layout.Normalize()
	hidden := layout.Viewport != state.ViewportWide || props.Collapsed
	tokens := props.Mode.Tokens()
	if hidden {
		return html.Aside(html.Props{
			ID: "inspector-region", TabIndex: -1,
			Hidden: true,
			Aria:   map[string]string{"label": "Task assurance and context"},
			Data: map[string]string{
				"component": "assurance-rail", "focus-region": "inspector", "focus-order": "5",
			},
		})
	}
	return html.Aside(html.Props{
		ID: "inspector-region", TabIndex: -1,
		Aria: map[string]string{"label": "Task assurance and context"},
		Data: map[string]string{
			"component": "assurance-rail", "focus-region": "inspector", "focus-order": "5",
		},
		Class: css.New(
			u.Flex, u.FlexCol,
			css.Gap(css.Px(tokens.Spacing.SM)),
			css.H(css.Full),
			css.Padding(css.Px(tokens.Spacing.SM)),
			css.Bg(css.Hex(string(tokens.Colors.Shell))),
			css.BorderLeft(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
			css.OverflowY.Auto,
		).String(),
	},
		html.Div(html.Props{
			Class: css.New(u.Flex, u.ItemsCenter, u.JustifyBetween).String(),
		},
			html.Strong(html.Props{Text: "Task details"}),
			headerIconButton("×", "Collapse task details sidebar", props.Mode, props.OnCollapse),
		),
		inspectorCard("Identity and plan", []detailRow{
			{"Milestone", "M16"},
			{"Plan reference", "§27A · §27C"},
			{"Repository", fallback(props.Snapshot.Workspace.RepositoryName, "Not selected")},
			{"Branch", fallback(props.Snapshot.Workspace.Branch, "Unknown")},
		}, tokens),
		gatesCard(tokens),
		inspectorCard("Measured metrics", []detailRow{
			{"Frontend", "GWC v5"},
			{"Connection", humanize(string(props.Snapshot.Session.Connection))},
			{"Actual cost", fallback(props.Snapshot.CostLabel, "Unknown")},
			{"External requests", "None"},
		}, tokens),
		inspectorCard("Related artifacts", []detailRow{
			{"Browser suite", "Running"},
			{"Task graph", "Live context"},
			{"Plan", "Referenced"},
			{"Local database", "Available"},
		}, tokens),
	)
}

type detailRow struct {
	label string
	value string
}

func inspectorCard(title string, rows []detailRow, tokens design.Tokens) ui.Node {
	nodes := make([]ui.Node, 0, len(rows))
	for _, row := range rows {
		nodes = append(nodes, html.Div(html.Props{
			Class: css.New(
				u.Flex, u.ItemsCenter, u.JustifyBetween,
				css.Gap(css.Px(tokens.Spacing.SM)),
				css.MinHeight(css.Px(28)),
				css.BorderBottom(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
				css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
			).String(),
		},
			html.Span(html.Props{
				Class: css.New(css.TextColor(css.Hex(string(tokens.Colors.TextMuted)))).String(),
				Text:  row.label,
			}),
			html.Strong(html.Props{
				Class: css.New(
					css.FontWeight.Medium,
					css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
				).String(),
				Text: row.value,
			}),
		))
	}
	return html.Section(html.Props{
		Class: css.New(
			css.Padding(css.Px(tokens.Spacing.MD)),
			css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
			css.Bg(css.Hex(string(tokens.Colors.Surface1))),
			css.Border(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
		).String(),
	},
		html.H2(html.Props{
			Class: css.New(
				css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
				css.MarginY(css.Px(tokens.Spacing.SM)),
				css.FontSize(css.Px(tokens.Typography.PanelHeading.Size)),
			).String(),
			Text: title + "  ⌃",
		}),
		html.Div(html.Props{}, nodes...),
	)
}

func gatesCard(tokens design.Tokens) ui.Node {
	rows := []detailRow{
		{"Route bootstrap", "Passed ✓"},
		{"Origin boundary", "Passed ✓"},
		{"Visual fidelity", "Running ◌"},
		{"Browser matrix", "Pending ○"},
	}
	return inspectorCard("Correctness gates", rows, tokens)
}

func TaskWorkspaceHeader(props TaskWorkspaceHeaderProps) ui.Node {
	tokens := props.Mode.Tokens()
	pauseLabel, pauseAccessible := taskPausePresentation(props.Snapshot.TopBar.TaskState, false)
	taskStateLabel := humanize(fallback(props.Snapshot.TopBar.TaskState, "unknown"))
	taskTitle := fallback(props.Snapshot.TopBar.TaskTitle, "Selected task")
	taskSummary := fallback(props.Snapshot.TopBar.TaskSummary, "No authoritative task summary is available.")
	taskStateColor := tokens.Colors.Active
	switch {
	case strings.Contains(strings.ToLower(taskStateLabel), "paused"):
		taskStateColor = tokens.Colors.Warning
	case strings.Contains(strings.ToLower(taskStateLabel), "complete"):
		taskStateColor = tokens.Colors.Success
	case strings.Contains(strings.ToLower(taskStateLabel), "fail"),
		strings.Contains(strings.ToLower(taskStateLabel), "blocked"):
		taskStateColor = tokens.Colors.Failure
	}
	moreExpanded := props.TaskActionsOpen
	return html.Div(html.Props{
		DataAttr: html.DataAttribute{Name: "component", Value: "task-workspace-header"},
		Class: css.New(
			u.Flex, u.FlexCol,
			css.Gap(css.Px(6)),
			css.MinWidth(css.Zero),
		).String(),
	},
		html.Section(html.Props{
			Aria: map[string]string{"label": "Task summary"},
			Class: css.New(
				u.Flex, u.ItemsCenter, u.JustifyBetween, css.FlexWrap.Wrap,
				css.Gap(css.Px(tokens.Spacing.MD)),
				css.MinHeight(css.Px(58)),
				css.PaddingY(css.Px(tokens.Spacing.XS)),
				css.PaddingX(css.Px(tokens.Spacing.SM)),
			).String(),
		},
			html.Div(html.Props{
				Class: css.New(u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.LG))).String(),
			},
				html.Span(html.Props{
					Aria: map[string]string{"hidden": "true"},
					Class: css.New(
						u.InlineFlex, u.ItemsCenter, u.JustifyCenter,
						css.W(css.Px(40)), css.H(css.Px(40)),
						css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
						css.Bg(css.Hex(string(tokens.Colors.Surface2))),
						css.TextColor(css.Hex(string(tokens.Colors.Accent))),
						css.FontSize(css.Px(21)),
					).String(),
					Text: "⌁",
				}),
				html.Div(html.Props{
					Class: css.New(
						u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.XS)),
					).String(),
				},
					html.Div(html.Props{
						Class: css.New(u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.SM))).String(),
					},
						html.H1(html.Props{
							Class: css.New(
								css.Margin(css.Zero),
								css.FontSize(css.Px(tokens.Typography.WorkspaceTitle.Size)),
								css.LineHeightLen(css.Px(tokens.Typography.WorkspaceTitle.LineHeight)),
								// The serif carries the title at its natural
								// weight with a hair of positive tracking.
								// Semibold and negative tracking are a sans
								// treatment; on a serif they close the counters
								// and the title reads cramped.
								css.FontWeight.Normal,
								css.Tracking(css.Ems(0.004)),
								css.Font(css.FontStack(tokens.Fonts.Display)),
								css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
							).String(),
							Text: taskTitle,
						}),
						statusPill("● "+taskStateLabel, taskStateColor, tokens),
					),
					html.P(html.Props{
						Class: css.New(
							// The requirement is prose a person wrote and must
							// judge, so it is set in the reading serif rather
							// than in the interface sans.
							css.Margin(css.Zero),
							// Prose is measured, not stretched. A serif line
							// past about 80 characters loses the reader between
							// the end of one line and the start of the next.
							css.MaxWidth(css.Ch(78)),
							css.TextColor(css.Hex(string(tokens.Colors.TextSecondary))),
							css.Font(css.FontStack(tokens.Fonts.Reading)),
							css.FontSize(css.Px(tokens.Typography.Body.Size)),
							css.LineHeightLen(css.Px(tokens.Typography.Body.LineHeight)),
						).String(),
						Text: taskSummary,
					}),
				),
			),
			html.Div(html.Props{
				Class: css.New(u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.SM))).String(),
			},
				primitives.Button(primitives.ButtonProps{
					Label: pauseLabel, AccessibleLabel: pauseAccessible, Mode: props.Mode,
					Disabled: !props.Snapshot.TopBar.CanPause || props.OnPauseRequested == nil,
					OnClick:  props.OnPauseRequested,
				}),
				primitives.Button(primitives.ButtonProps{
					Label: "Stop", AccessibleLabel: "Stop task", Mode: props.Mode,
					Disabled: !props.Snapshot.TopBar.CanStop || props.OnStopRequested == nil,
					OnClick:  props.OnStopRequested,
				}),
				primitives.Button(primitives.ButtonProps{
					Label: "◇  Request review", AccessibleLabel: "Request review", Mode: props.Mode,
					Disabled: props.OnReviewRequested == nil, OnClick: props.OnReviewRequested,
				}),
				html.Div(html.Props{Class: css.New(u.Relative).String()},
					primitives.Button(primitives.ButtonProps{
						ID: "task-actions-trigger", Label: "•••", AccessibleLabel: "More task actions", Mode: props.Mode,
						Expanded: &moreExpanded, Controls: "task-actions-popover",
						Disabled: props.OnTaskActionsOpen == nil, OnClick: props.OnTaskActionsOpen,
					}),
					ui.CreateElement(TaskActionsPopover, TaskActionsPopoverProps{
						Open: props.TaskActionsOpen, Snapshot: props.Snapshot, Mode: props.Mode,
						OnDismiss: props.OnTaskActionsDismiss, OnNavigatePath: props.OnNavigatePath,
						OnPauseRequested: props.OnPauseRequested, OnStopRequested: props.OnStopRequested,
						OnReviewRequested: props.OnReviewRequested,
					}),
				),
			),
		),
		taskMetricStrip(props.Snapshot.TopBar, taskStateLabel, taskStateColor, tokens),
	)
}

type TaskActionsPopoverProps struct {
	Open              bool
	Snapshot          state.Snapshot
	Mode              primitives.Mode
	OnDismiss         func()
	OnNavigatePath    func(string)
	OnPauseRequested  func()
	OnStopRequested   func()
	OnReviewRequested func()
}

func TaskActionsPopover(props TaskActionsPopoverProps) ui.Node {
	tokens := props.Mode.Tokens()
	pauseLabel, pauseAccessible := taskPausePresentation(props.Snapshot.TopBar.TaskState, false)
	invoke := func(action func()) func() {
		if action == nil {
			return nil
		}
		return func() {
			if props.OnDismiss != nil {
				props.OnDismiss()
			}
			action()
		}
	}
	navigate := func(path string) func() {
		if props.OnNavigatePath == nil {
			return nil
		}
		return func() {
			if props.OnDismiss != nil {
				props.OnDismiss()
			}
			props.OnNavigatePath(path)
		}
	}
	return primitives.Dialog(primitives.OverlayProps{
		ID: "task-actions-popover", Open: props.Open,
		LabelledBy: "task-actions-title", DescribedBy: "task-actions-description",
		InitialFocusSelector: "#task-actions-open-graph", AppRootSelector: `[data-component="app-shell"]`,
		Mode: props.Mode, OnDismiss: props.OnDismiss,
		Content: html.Section(html.Props{
			Data:  map[string]string{"component": "task-actions"},
			Class: css.New(u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.SM)), css.MinWidth(css.Px(260))).String(),
		},
			html.H2(html.Props{
				ID: "task-actions-title", Text: "Task actions",
				Class: css.New(
					css.Margin(css.Zero),
					css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
					css.FontSize(css.Px(tokens.Typography.PanelHeading.Size)),
				).String(),
			}),
			html.P(html.Props{
				ID: "task-actions-description", Text: "Inspect, review, pause, or stop the current task.",
				Class: css.New(css.Margin(css.Zero), css.TextColor(css.Hex(string(tokens.Colors.TextSecondary)))).String(),
			}),
			primitives.Button(primitives.ButtonProps{
				ID: "task-actions-open-graph", Label: "⌘  Open task graph", AccessibleLabel: "Open task graph", Mode: props.Mode,
				Disabled: props.OnNavigatePath == nil, OnClick: navigate("/graphs"),
			}),
			primitives.Button(primitives.ButtonProps{
				Label: "◇  Request review", AccessibleLabel: "Request review from task actions", Mode: props.Mode,
				Disabled: props.OnReviewRequested == nil, OnClick: invoke(props.OnReviewRequested),
			}),
			primitives.Button(primitives.ButtonProps{
				Label: pauseLabel, AccessibleLabel: pauseAccessible + " from task actions", Mode: props.Mode,
				Disabled: !props.Snapshot.TopBar.CanPause || props.OnPauseRequested == nil,
				OnClick:  invoke(props.OnPauseRequested),
			}),
			primitives.Button(primitives.ButtonProps{
				Label: "■  Stop", AccessibleLabel: "Stop task from task actions", Mode: props.Mode,
				Disabled: !props.Snapshot.TopBar.CanStop || props.OnStopRequested == nil,
				OnClick:  invoke(props.OnStopRequested),
			}),
			primitives.Button(primitives.ButtonProps{
				Label: "⚙  Task settings", AccessibleLabel: "Open task settings", Mode: props.Mode,
				Disabled: props.OnNavigatePath == nil, OnClick: navigate("/settings"),
			}),
		),
	})
}

func taskPausePresentation(taskState string, compact bool) (string, string) {
	if strings.Contains(strings.ToLower(strings.TrimSpace(taskState)), "paused") {
		if compact {
			return "▶", "Resume task"
		}
		return "▶  Resume", "Resume task"
	}
	if compact {
		return "Ⅱ", "Pause task"
	}
	return "Ⅱ  Pause", "Pause task"
}

func taskMetricStrip(topBar state.TopBarView, taskState string, taskStateColor design.Color, tokens design.Tokens) ui.Node {
	metrics := []detailRow{
		{"Correctness profile", "Unknown"},
		{"Task state", taskState},
		{"Progress", "Unknown"},
		{"Elapsed", "Unknown"},
		{"Cost", fallback(topBar.ActualCost, "Unknown")},
		{"Gates", "Unknown"},
	}
	nodes := make([]ui.Node, 0, len(metrics))
	for index, metric := range metrics {
		valueColor := tokens.Colors.TextPrimary
		if index == 1 {
			valueColor = taskStateColor
		}
		if index == 2 {
			valueColor = tokens.Colors.Active
		}
		if index == 5 {
			valueColor = tokens.Colors.Success
		}
		nodes = append(nodes, html.Div(html.Props{
			Class: css.New(
				u.Flex, u.ItemsCenter, u.JustifyBetween,
				css.Gap(css.Px(tokens.Spacing.SM)),
				css.PaddingY(css.Px(tokens.Spacing.XS)),
				css.PaddingX(css.Px(tokens.Spacing.MD)),
			).String(),
		},
			html.P(html.Props{
				Class: css.New(
					css.Margin(css.Zero),
					css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
					css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				).String(),
				Text: metric.label,
			}),
			html.Strong(html.Props{
				Class: css.New(
					css.FontSize(css.Px(tokens.Typography.ControlLabel.Size)),
					css.LineHeightLen(css.Px(tokens.Typography.MetricValue.LineHeight)),
					css.FontWeight.Semibold,
					css.TextColor(css.Hex(string(valueColor))),
				).String(),
				Text: metric.value,
			}),
		))
	}
	rules := []css.Rule{
		u.Grid,
		css.GridCols(css.Repeat(6, css.MinMax(css.TrackLen(css.Zero), css.Fr(1)))),
		u.ItemsCenter,
		css.MinHeight(css.Px(44)),
		css.Rounded(css.Px(tokens.Geometry.PanelRadius)),
		css.Bg(css.Hex(string(tokens.Colors.Surface1))),
		css.Shadow(css.ShadowOf(
			css.Zero, css.Px(8), css.Px(30), css.Px(-26), css.RGBA(0, 0, 0, 0.5),
		)),
	}
	rules = append(rules, css.Media(
		css.MaxW(799),
		css.GridCols(css.Repeat(3, css.MinMax(css.TrackLen(css.Zero), css.Fr(1)))),
	)...)
	return html.Section(html.Props{
		Aria:  map[string]string{"label": "Task metrics"},
		Class: css.New(rules...).String(),
	}, nodes...)
}

func statusPill(label string, color design.Color, tokens design.Tokens) ui.Node {
	return html.Span(html.Props{
		Class: css.New(
			u.InlineFlex, u.ItemsCenter,
			css.MinHeight(css.Px(24)),
			css.PaddingX(css.Px(tokens.Spacing.SM)),
			css.Rounded(css.Px(tokens.Geometry.PillRadius)),
			css.Bg(css.Hex(string(tokens.Colors.Selection))),
			css.TextColor(css.Hex(string(color))),
			css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
			css.FontWeight.Semibold,
		).String(),
		Text: label,
	})
}

func GraphPane(props GraphPaneProps) ui.Node {
	recordRender(props.Probe, "graph-pane", props.Revision)
	tokens := props.Mode.Tokens()
	if props.Collapsed {
		return html.Aside(html.Props{
			ID: "graph-region", TabIndex: -1,
			Hidden: true,
			Aria:   map[string]string{"label": "Task graph"},
			Data: map[string]string{
				"component": "graph-pane", "focus-region": "graph", "focus-order": "4",
			},
		})
	}
	content := asyncStateContent(props.State, "task graph nodes", len(props.Nodes), props.Mode)
	if props.State == state.DataReady || props.State == state.DataPartialStale {
		height := 510
		if props.Viewport == state.ViewportWide {
			height = 640
		}
		if props.Viewport == state.ViewportNarrow || props.Viewport == state.ViewportMinimum {
			height = 420
		}
		if props.Authoritative != nil {
			authoritative := *props.Authoritative
			authoritative.ResponsiveMode = string(props.Viewport)
			authoritative.VisualMode = props.Mode
			authoritative.Height = height
			content = ui.CreateElement(graphcanvas.Renderer, graphcanvas.Props{Authoritative: &authoritative})
		} else {
			content = ui.CreateElement(graphcanvas.Renderer, graphcanvas.Props{
				Nodes:          props.Nodes,
				Edges:          graphCanvasEdges(props.Nodes),
				SelectedID:     props.SelectedID,
				CurrentID:      currentGraphNodeID(props.Nodes),
				ResponsiveMode: string(props.Viewport),
				Mode:           props.Mode,
				Height:         height,
				OnSelect:       props.OnSelect,
			})
		}
		if props.Inspector != nil {
			content = html.Div(html.Props{
				Data:  map[string]string{"component": "authoritative-graph-workspace"},
				Class: css.New(u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.MD)), css.OverflowY.Auto).String(),
			}, content, props.Inspector)
		}
	}
	return html.Aside(html.Props{
		ID: "graph-region", TabIndex: -1,
		Aria: map[string]string{"label": "Task graph"},
		Data: map[string]string{
			"component":    "graph-pane",
			"state":        string(props.State),
			"revision":     strconv.FormatUint(props.Revision, 10),
			"focus-region": "graph",
			"focus-order":  "4",
		},
		Class: css.New(
			u.Flex, u.FlexCol,
			css.MinWidth(css.Zero), css.H(css.Full),
			css.Rounded(css.Px(tokens.Geometry.DialogRadius)),
			css.Bg(css.Hex(string(tokens.Colors.SurfaceRaised))),
			css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
			css.Border(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
			css.Shadow(css.ShadowOf(
				css.Zero, css.Px(18), css.Px(44), css.Px(-28), css.RGBA(0, 0, 0, 0.72),
			)),
			css.Overflow.Hidden,
		).String(),
	},
		html.Div(html.Props{
			Class: css.New(
				u.Flex, u.ItemsCenter, u.JustifyBetween,
				css.MinHeight(css.Px(50)),
				css.PaddingX(css.Px(tokens.Spacing.MD)),
				css.BorderBottom(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
			).String(),
		},
			html.Div(html.Props{
				Class: css.New(u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.SM))).String(),
			},
				html.H2(html.Props{
					Class: css.New(
						css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
						css.Margin(css.Zero),
						css.FontSize(css.Px(tokens.Typography.PanelHeading.Size)),
					).String(),
					Text: "⌘  Project graph",
				}),
				statusPill("● Live", tokens.Colors.Active, tokens),
			),
			html.Div(html.Props{
				Class: css.New(
					u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.SM)),
					css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
					css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				).String(),
				Text: "▣ Work  ⬡ Tool  ◇ Plan  ● Proof  ◉ Memory",
			}),
		),
		content,
	)
}

func graphCanvasEdges(nodes []state.GraphNodeView) []graphcanvas.Edge {
	present := make(map[string]bool, len(nodes))
	for _, node := range nodes {
		present[node.ID] = true
	}
	candidates := []graphcanvas.Edge{
		{ID: "requirements-design", FromID: "requirements", ToID: "design", Kind: graphcanvas.EdgeData},
		{ID: "requirements-routes", FromID: "requirements", ToID: "routes", Kind: graphcanvas.EdgeData},
		{ID: "design-implementation", FromID: "design", ToID: "implementation", Kind: graphcanvas.EdgeControl},
		{ID: "routes-implementation", FromID: "routes", ToID: "implementation", Kind: graphcanvas.EdgeControl},
		{ID: "bootstrap-implementation", FromID: "bootstrap", ToID: "implementation", Kind: graphcanvas.EdgeControl},
		{ID: "plan-implementation", FromID: "plan", ToID: "implementation", Kind: graphcanvas.EdgeControl},
		{ID: "implementation-responsive", FromID: "implementation", ToID: "responsive", Kind: graphcanvas.EdgeControl},
		{ID: "implementation-browser", FromID: "implementation", ToID: "browser", Kind: graphcanvas.EdgeControl},
		{ID: "responsive-review", FromID: "responsive", ToID: "review", Kind: graphcanvas.EdgeEvidence},
		{ID: "browser-review", FromID: "browser", ToID: "review", Kind: graphcanvas.EdgeEvidence},
		{ID: "review-evidence", FromID: "review", ToID: "evidence", Kind: graphcanvas.EdgeEvidence},
	}
	edges := make([]graphcanvas.Edge, 0, len(candidates))
	for _, edge := range candidates {
		if present[edge.FromID] && present[edge.ToID] {
			edges = append(edges, edge)
		}
	}
	if len(edges) > 0 || len(nodes) < 2 {
		return edges
	}
	for index := 1; index < len(nodes); index++ {
		edges = append(edges, graphcanvas.Edge{
			ID:     "sequence-" + strconv.Itoa(index),
			FromID: nodes[index-1].ID,
			ToID:   nodes[index].ID,
			Kind:   graphcanvas.EdgeData,
		})
	}
	return edges
}

func currentGraphNodeID(nodes []state.GraphNodeView) string {
	for _, node := range nodes {
		if node.ID == "implementation" {
			return node.ID
		}
	}
	for _, node := range nodes {
		if node.Selected {
			return node.ID
		}
	}
	for _, node := range nodes {
		status := strings.ToLower(strings.TrimSpace(node.Status))
		if status == "active" || status == "running" || status == "in progress" {
			return node.ID
		}
	}
	if len(nodes) > 0 {
		return nodes[0].ID
	}
	return ""
}

// Legacy DOM graph helpers remain as non-mounted compatibility fixtures while
// the production surface is the interactive Canvas 2D renderer.
var (
	_ = graphCanvas
	_ = graphNode
	_ = graphEdges
	_ = graphArrowPoints
	_ = graphToolbar
	_ = graphToolButton
	_ = graphMinimap
	_ = graphPlacement
	_ = statusDot
)

func graphCanvas(
	nodes []state.GraphNodeView,
	selectedID string,
	tokens design.Tokens,
	onSelect func(string),
) ui.Node {
	items := make([]ui.Node, 0, len(nodes)+3)
	items = append(items, graphEdges(tokens, len(nodes)))
	for index, node := range nodes {
		items = append(items, graphNode(node, selectedID, index, tokens, onSelect))
	}
	items = append(items, graphToolbar(tokens), graphMinimap(tokens))
	return html.Div(html.Props{
		DataAttr: html.DataAttribute{Name: "component", Value: "graph-canvas"},
		Class: css.New(
			u.Relative,
			u.Grid,
			css.GridCols(css.Repeat(4, css.MinMax(css.TrackLen(css.Px(84)), css.Fr(1)))),
			css.GridRows(css.Repeat(3, css.MinMax(css.TrackLen(css.Px(62)), css.Fr(1)))),
			u.ItemsCenter,
			css.Gap(css.Px(tokens.Spacing.MD)),
			css.FlexGrow(css.Num(1)),
			css.MinHeight(css.Px(390)),
			css.Padding(css.Px(tokens.Spacing.LG)),
			css.Bg(css.Hex(string(tokens.Colors.SurfaceInset))),
			css.Overflow.Auto,
		).String(),
	}, items...)
}

func graphNode(
	node state.GraphNodeView,
	selectedID string,
	index int,
	tokens design.Tokens,
	onSelect func(string),
) ui.Node {
	selected := node.Selected
	if selectedID != "" {
		selected = node.ID == selectedID
	}
	tone, emphasis := graphNodeEmphasis(node, selected, tokens)
	rules := []css.Rule{
		u.Relative,
		u.Flex, u.ItemsCenter, u.JustifyBetween,
		css.MinHeight(css.Px(48)),
		css.PaddingX(css.Px(tokens.Spacing.MD)),
		css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
		css.Bg(css.Hex(string(tokens.Colors.Surface2))),
		css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
		css.Border(css.Px(1), css.Hex(string(tone))),
		css.FontSize(css.Px(tokens.Typography.CompactBody.Size)),
		css.FontWeight.Medium,
		css.Cursor.Pointer,
		css.ZIndex(2),
	}
	rules = append(rules, graphPlacement(index)...)
	if selected {
		rules = append(rules, css.Shadow(css.ShadowOf(
			css.Zero, css.Zero, css.Px(10), css.Zero, css.RGBA(60, 151, 255, 0.22),
		)))
	}
	children := []ui.Node{
		html.Span(html.Props{
			Text: statusDot(node.Status) + "  " + node.Title + "   ›",
		}),
	}
	selectNode := func() {
		if onSelect != nil {
			onSelect(node.ID)
		}
	}
	buttonProps := html.PropsOf(
		html.OnClick(func(ui.MouseEvent) {
			selectNode()
		}),
		html.OnKeyDown(func(event ui.KeyboardEvent) {
			if event.GetKey() == "Enter" || event.GetKey() == " " {
				event.PreventDefault()
				selectNode()
			}
		}),
	)
	buttonProps.Type = "button"
	buttonProps.Data = map[string]string{
		"node-id":         node.ID,
		"status":          node.Status,
		"selected":        strconv.FormatBool(selected),
		"visual-emphasis": emphasis,
		"position":        strconv.Itoa(index),
	}
	buttonProps.Aria = map[string]string{
		"label":   node.Title + " - " + humanize(node.Status),
		"pressed": strconv.FormatBool(selected),
	}
	buttonProps.Class = css.New(rules...).String()
	return html.Button(buttonProps, children...)
}

func graphNodeEmphasis(node state.GraphNodeView, selected bool, tokens design.Tokens) (design.Color, string) {
	if selected {
		return tokens.Colors.Active, "selected"
	}
	status := strings.ToLower(strings.TrimSpace(node.Status))
	if status == "active" || status == "running" || status == "in progress" {
		return tokens.Colors.Active, "active"
	}
	if status == "blocked" || status == "failed" {
		return tokens.Colors.Failure, "blocked"
	}
	if strings.Contains(strings.ToLower(node.Title), "evidence") {
		return tokens.Colors.Evidence, "evidence"
	}
	return tokens.Colors.BorderSubtle, "idle"
}

func graphEdges(tokens design.Tokens, nodeCount int) ui.Node {
	type edge struct {
		points string
		endX   int
		endY   int
	}
	edges := []edge{
		{points: "230,300 250,300 250,100 270,100", endX: 270, endY: 100},
		{points: "230,300 270,300", endX: 270, endY: 300},
		{points: "230,300 250,300 250,500 270,500", endX: 270, endY: 500},
		{points: "480,100 520,100", endX: 520, endY: 100},
		{points: "480,300 520,300", endX: 520, endY: 300},
		{points: "480,500 520,500", endX: 520, endY: 500},
		{points: "730,100 770,100", endX: 770, endY: 100},
		{points: "730,300 770,300", endX: 770, endY: 300},
		{points: "730,500 770,500", endX: 770, endY: 500},
	}
	if edgeCount := min(max(nodeCount-1, 0), len(edges)); edgeCount < len(edges) {
		edges = edges[:edgeCount]
	}
	color := string(tokens.Colors.BorderStrong)
	children := make([]ui.Node, 0, len(edges)*2)
	for index, item := range edges {
		children = append(children,
			html.Polyline(html.Props{
				Key: "edge-" + strconv.Itoa(index),
				Raw: map[string]any{
					"points": item.points, "fill": "none", "stroke": color,
					"stroke-width": "2", "vector-effect": "non-scaling-stroke",
				},
			}),
			html.Polygon(html.Props{
				Key: "arrow-" + strconv.Itoa(index),
				Raw: map[string]any{
					"points": graphArrowPoints(item.endX, item.endY),
					"fill":   color,
				},
			}),
		)
	}
	return html.Svg(html.Props{
		Aria: map[string]string{"hidden": "true"},
		DataAttr: html.DataAttribute{
			Name: "component", Value: "graph-directed-edges",
		},
		Class: css.New(
			u.Absolute,
			css.Inset(css.Px(tokens.Spacing.LG)),
			css.PointerEvents.None,
			css.ZIndex(1),
		).String(),
		Raw: map[string]any{
			"viewBox":             "0 0 1000 600",
			"preserveAspectRatio": "none",
		},
	}, children...)
}

func graphArrowPoints(x, y int) string {
	return strconv.Itoa(x-9) + "," + strconv.Itoa(y-6) + " " +
		strconv.Itoa(x) + "," + strconv.Itoa(y) + " " +
		strconv.Itoa(x-9) + "," + strconv.Itoa(y+6)
}

func graphToolbar(tokens design.Tokens) ui.Node {
	return html.Div(html.Props{
		Aria: map[string]string{"label": "Graph viewport controls"},
		Class: css.New(
			u.Absolute, u.Flex, u.ItemsCenter,
			css.Left(css.Px(tokens.Spacing.MD)),
			css.Bottom(css.Px(tokens.Spacing.MD)),
			css.ZIndex(4),
			css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
			css.Bg(css.Hex(string(tokens.Colors.Surface1))),
			css.Border(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
			css.Overflow.Hidden,
		).String(),
	},
		graphToolButton("Fit", "Fit graph to viewport", tokens),
		graphToolButton("−", "Zoom out", tokens),
		graphToolButton("+", "Zoom in", tokens),
	)
}

func graphToolButton(label, accessible string, tokens design.Tokens) ui.Node {
	return html.Button(html.Props{
		Type: "button",
		Aria: map[string]string{"label": accessible},
		Class: css.New(
			css.MinWidth(css.Px(38)),
			css.MinHeight(css.Px(34)),
			css.PaddingX(css.Px(tokens.Spacing.SM)),
			css.Bg(css.Transparent),
			css.TextColor(css.Hex(string(tokens.Colors.TextSecondary))),
			css.Border(css.Zero, css.Transparent),
			css.Cursor.Pointer,
			css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
		).String(),
		Text: label,
	})
}

func graphMinimap(tokens design.Tokens) ui.Node {
	return html.Div(html.Props{
		Aria: map[string]string{"label": "Graph minimap, ten nodes"},
		Class: css.New(
			u.Absolute, u.Flex, u.ItemsCenter, u.JustifyCenter,
			css.Right(css.Px(tokens.Spacing.MD)),
			css.Bottom(css.Px(tokens.Spacing.MD)),
			css.ZIndex(3),
			css.W(css.Px(92)), css.H(css.Px(52)),
			css.Rounded(css.Px(tokens.Geometry.RadiusSmall)),
			css.Bg(css.Hex(string(tokens.Colors.Surface1))),
			css.Border(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
			css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
			css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
		).String(),
		Text: "•  •  •\n  •  •",
	})
}

func graphPlacement(index int) []css.Rule {
	placements := [][2]int{
		{1, 2},
		{2, 1},
		{2, 2},
		{2, 3},
		{3, 1},
		{3, 2},
		{3, 3},
		{4, 1},
		{4, 2},
		{4, 3},
	}
	if index < 0 || index >= len(placements) {
		return nil
	}
	return []css.Rule{
		css.GridColumn(css.GridLineAt(placements[index][0])),
		css.GridRow(css.GridLineAt(placements[index][1])),
	}
}

func statusDot(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "complete", "completed", "passed":
		return "✓"
	case "active", "running", "in progress":
		return "◌"
	case "blocked", "failed":
		return "!"
	default:
		return "○"
	}
}
