package shell

import (
	"strconv"
	"strings"

	"codeflux.dev/codeflux/web/frontend/design"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"codeflux.dev/codeflux/web/frontend/routes"
	"codeflux.dev/codeflux/web/frontend/state"
	"github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/css/u"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func applicationFrameClass(layout state.LayoutPreferences, tokens design.Tokens) string {
	layout = layout.Normalize()
	tracks := []css.Track{css.MinMax(css.TrackLen(css.Zero), css.Fr(1))}
	switch layout.Viewport {
	case state.ViewportWide:
		if layout.RailCollapsed {
			tracks = []css.Track{
				css.MinMax(css.TrackLen(css.Zero), css.Fr(1)),
				css.TrackLen(css.Px(316)),
			}
		} else {
			tracks = []css.Track{
				css.TrackLen(css.Px(layout.RailWidth)),
				css.MinMax(css.TrackLen(css.Zero), css.Fr(1)),
				css.TrackLen(css.Px(316)),
			}
		}
	}
	return css.New(
		u.Relative,
		u.Grid,
		css.GridCols(tracks...),
		css.W(css.Full),
		// Dynamic viewport units follow the visual viewport as a software
		// keyboard opens, keeping the grid's anchored composer reachable.
		css.H(css.RawLength("calc(100dvh - 58px)")),
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
	taskState := fallback(props.View.TaskState, "task state")
	model := fallback(props.View.Model, "model")
	effort := fallback(props.View.Effort, "effort")
	forecast := fallback(props.View.ForecastCost, "forecast")
	actual := fallback(props.View.ActualCost, fallback(props.CostLabel, "actual"))
	budget := fallback(props.View.HardBudget, "budget")
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
					css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
				).String(),
				Text: "Codeflux",
			}),
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
			contextControl("▣", repository, tokens),
			contextControl("⑂", branch, tokens),
			contextControl("✓", worktree, tokens),
			html.Div(html.Props{Class: wideOnlyClass()},
				contextControl("◈", model+" · "+effort, tokens),
			),
			html.Div(html.Props{Class: wideOnlyClass()},
				contextControl("$", forecast+" / "+actual+" / "+budget, tokens),
			),
		),
		html.Div(html.Props{
			Aria:  map[string]string{"label": "Session controls"},
			Class: css.New(u.Flex, u.ItemsCenter, u.JustifyEnd, css.Gap(css.Px(tokens.Spacing.SM))).String(),
		},
			html.Div(html.Props{Class: desktopOnlyClass()},
				headerIconButtonWithID(
					"thread-rail-toggle", "☰", "Toggle thread rail", props.Mode, props.OnRailToggle,
				),
			),
			html.Span(html.Props{
				Hidden: compact,
				Class: wideOnlyClass(
					css.TextColor(css.Hex(string(tokens.Colors.Active))),
					css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
					css.FontWeight.Medium,
				),
				Text: "● " + humanize(taskState),
			}),
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
			html.Div(html.Props{Class: wideOnlyClass(u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.XS)))},
				headerIconButton("Ⅱ", "Pause task", props.Mode, props.OnPauseRequested),
				headerIconButton("■", "Stop task", props.Mode, props.OnStopRequested),
				headerIconButton("•••", "More task actions", props.Mode, nil),
			),
			headerIconButton("⌕", "Search", props.Mode, nil),
			headerIconButton("?", "Shortcut help", props.Mode, props.OnShortcutHelp),
			headerIconButton("◐", "Change color theme", props.Mode, props.OnThemeChange),
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
	)
}

func applicationBarClass(tracks []css.Track, tokens design.Tokens) string {
	rules := []css.Rule{
		u.Grid,
		css.GridCols(tracks...),
		u.ItemsCenter,
		css.Gap(css.Px(tokens.Spacing.MD)),
		css.W(css.Full),
		css.MinHeight(css.Px(58)),
		css.PaddingX(css.Px(tokens.Spacing.LG)),
		css.Bg(css.Hex(string(tokens.Colors.Shell))),
		css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
		css.BorderBottom(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
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
			css.W(css.Px(30)), css.H(css.Px(30)),
			css.Rounded(css.Px(tokens.Geometry.PillRadius)),
			css.Border(css.Px(4), css.Hex(string(tokens.Colors.Accent))),
			css.TextColor(css.Hex(string(tokens.Colors.Accent))),
			css.FontSize(css.Px(10)),
			css.Shadow(css.ShadowOf(
				css.Zero, css.Zero, css.Px(12), css.Zero,
				css.RGBA(94, 226, 123, 0.22),
			)),
		).String(),
		Text: "●",
	})
}

func contextControl(icon, label string, tokens design.Tokens) ui.Node {
	return html.Button(html.Props{
		Type: "button",
		Aria: map[string]string{"label": label},
		Class: css.New(
			u.InlineFlex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.SM)),
			css.MinHeight(css.Px(36)),
			css.PaddingX(css.Px(tokens.Spacing.MD)),
			css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
			css.Bg(css.Hex(string(tokens.Colors.SurfaceInset))),
			css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
			css.Border(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
			css.FontSize(css.Px(tokens.Typography.ControlLabel.Size)),
			css.Cursor.Pointer,
		).String(),
		Text: icon + "  " + label + "  ⌄",
	})
}

func headerIconButton(label, accessible string, mode primitives.Mode, handler func()) ui.Node {
	return primitives.Button(primitives.ButtonProps{
		Label: label, AccessibleLabel: accessible, Mode: mode, OnClick: handler,
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
		ID: id, Label: label, AccessibleLabel: accessible, Mode: mode, OnClick: handler,
	})
}

type ProductSidebarProps struct {
	Snapshot   state.Snapshot
	Route      routes.Route
	Mode       primitives.Mode
	OnCollapse func()
	OnNarrower func()
	OnWider    func()
}

func ProductSidebar(props ProductSidebarProps) ui.Node {
	layout := props.Snapshot.Layout.Normalize()
	hidden := layout.RailCollapsed ||
		layout.Viewport == state.ViewportNarrow ||
		layout.Viewport == state.ViewportMinimum
	tokens := props.Mode.Tokens()
	if hidden {
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
		{"⌘", "Graphs", "/tasks"},
		{"◫", "Memory", "/memory"},
		{"▤", "Repositories", "/"},
		{"⚙", "Settings", "/settings"},
	}
	items := make([]ui.Node, 0, len(navItems))
	for _, item := range navItems {
		selected := routeSelected(props.Route, item.label)
		items = append(items, html.Div(html.Props{},
			html.A(html.Props{
				Href:  item.path,
				Aria:  map[string]string{"current": selectedAria(selected)},
				Class: sidebarLinkClass(tokens, selected),
				Text:  item.icon + "   " + item.label,
			}),
		))
	}
	threads := props.Snapshot.Threads()
	recent := make([]ui.Node, 0, len(threads))
	for index, thread := range threads {
		if index == 5 {
			break
		}
		recent = append(recent, html.Div(html.Props{
			Data:  map[string]string{"thread-id": thread.ID, "status": thread.Status},
			Class: css.New(css.MarginY(css.Px(tokens.Spacing.XS))).String(),
		},
			html.A(html.Props{
				Href:  "/tasks",
				Title: thread.Title,
				Aria: map[string]string{
					"label": thread.Title + " - " + humanize(thread.Status),
				},
				Class: css.New(
					u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.SM)),
					css.MinHeight(css.Px(30)),
					css.PaddingX(css.Px(tokens.Spacing.SM)),
					css.Rounded(css.Px(tokens.Geometry.RadiusSmall)),
					css.TextColor(css.Hex(string(tokens.Colors.TextSecondary))),
					css.TextDecoration.None,
					css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				).String(),
				Text: statusDot(thread.Status) + " " + compactLabel(thread.Title, 27),
			}),
		))
	}
	return html.Nav(html.Props{
		Aria: map[string]string{"label": navigationLabel},
		Data: map[string]string{
			"component": "product-sidebar",
			"viewport":  string(layout.Viewport),
			"overlay":   strconv.FormatBool(layout.Viewport == state.ViewportMedium),
			"width":     strconv.Itoa(layout.RailWidth),
		},
		Class: productSidebarClass(layout, tokens),
	},
		primitives.Button(primitives.ButtonProps{
			Label: "+  New Task", AccessibleLabel: "Create new task",
			Primary: true, Mode: props.Mode,
		}),
		html.Div(html.Props{
			Aria:  map[string]string{"label": "Thread rail layout controls"},
			Class: css.New(u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.XS))).String(),
		},
			headerIconButton("‹", "Narrow thread rail", props.Mode, props.OnNarrower),
			headerIconButton("›", "Widen thread rail", props.Mode, props.OnWider),
			headerIconButton("×", "Collapse thread rail", props.Mode, props.OnCollapse),
		),
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
			html.P(html.Props{
				Class: css.New(
					css.Margin(css.Zero),
					css.TextTransform.Uppercase,
					css.Tracking(css.Ems(0.08)),
					css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
					css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				).String(),
				Text: "Recent Tasks",
			}),
			html.Div(html.Props{
				Class: css.New(css.MarginY(css.Px(tokens.Spacing.SM)), css.Padding(css.Zero)).String(),
			}, recent...),
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
}

func productSidebarClass(layout state.LayoutPreferences, tokens design.Tokens) string {
	rules := []css.Rule{
		u.Flex, u.FlexCol,
		css.MinWidth(css.Zero), css.H(css.Full),
		css.Padding(css.Px(tokens.Spacing.LG)),
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
		return false
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
		css.MinHeight(css.Px(40)),
		css.MarginY(css.Px(tokens.Spacing.XS)),
		css.PaddingX(css.Px(tokens.Spacing.MD)),
		css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
		css.TextColor(css.Hex(string(tokens.Colors.TextSecondary))),
		css.TextDecoration.None,
		css.FontSize(css.Px(tokens.Typography.CompactBody.Size)),
	}
	if selected {
		rules = append(rules,
			css.Bg(css.Hex(string(tokens.Colors.Surface3))),
			css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
			css.BorderLeft(css.Px(3), css.Hex(string(tokens.Colors.Accent))),
		)
	}
	return css.New(rules...).String()
}

type AssuranceRailProps struct {
	Snapshot state.Snapshot
	Mode     primitives.Mode
}

func AssuranceRail(props AssuranceRailProps) ui.Node {
	layout := props.Snapshot.Layout.Normalize()
	hidden := layout.Viewport != state.ViewportWide
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
		inspectorCard("Task details", []detailRow{
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
	return html.Div(html.Props{
		DataAttr: html.DataAttribute{Name: "component", Value: "task-workspace-header"},
		Class: css.New(
			u.Flex, u.FlexCol,
			css.Gap(css.Px(tokens.Spacing.SM)),
			css.MinWidth(css.Zero),
		).String(),
	},
		html.Section(html.Props{
			Aria: map[string]string{"label": "Task summary"},
			Class: css.New(
				u.Flex, u.ItemsCenter, u.JustifyBetween, css.FlexWrap.Wrap,
				css.Gap(css.Px(tokens.Spacing.MD)),
				css.MinHeight(css.Px(64)),
				css.PaddingX(css.Px(tokens.Spacing.LG)),
				css.Rounded(css.Px(tokens.Geometry.PanelRadius)),
				css.Bg(css.Hex(string(tokens.Colors.Surface1))),
				css.Border(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
			).String(),
		},
			html.Div(html.Props{
				Class: css.New(u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.MD))).String(),
			},
				html.Span(html.Props{
					Aria: map[string]string{"hidden": "true"},
					Class: css.New(
						u.InlineFlex, u.ItemsCenter, u.JustifyCenter,
						css.W(css.Px(38)), css.H(css.Px(38)),
						css.Rounded(css.Px(tokens.Geometry.PillRadius)),
						css.Bg(css.Hex(string(tokens.Colors.Selection))),
						css.TextColor(css.Hex(string(tokens.Colors.Accent))),
						css.FontSize(css.Px(19)),
					).String(),
					Text: "✓",
				}),
				html.Div(html.Props{},
					html.Div(html.Props{
						Class: css.New(u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.SM))).String(),
					},
						html.H1(html.Props{
							Class: css.New(
								css.Margin(css.Zero),
								css.FontSize(css.Px(tokens.Typography.TaskTitle.Size)),
								css.LineHeightLen(css.Px(tokens.Typography.TaskTitle.LineHeight)),
								css.FontWeight.Semibold,
								css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
							).String(),
							Text: "Implement the Codeflux frontend shell",
						}),
						statusPill("● In progress", tokens.Colors.Success, tokens),
					),
					html.P(html.Props{
						Class: css.New(
							css.Margin(css.Zero),
							css.TextColor(css.Hex(string(tokens.Colors.TextSecondary))),
							css.FontSize(css.Px(tokens.Typography.CompactBody.Size)),
						).String(),
						Text: "Build the local-first GWC workspace with explicit correctness and browser evidence.",
					}),
				),
			),
			html.Div(html.Props{
				Class: css.New(u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.SM))).String(),
			},
				primitives.Button(primitives.ButtonProps{
					Label: "Ⅱ  Pause", AccessibleLabel: "Pause task", Mode: props.Mode,
					Disabled: !props.Snapshot.TopBar.CanPause || props.OnPauseRequested == nil,
					OnClick:  props.OnPauseRequested,
				}),
				primitives.Button(primitives.ButtonProps{
					Label: "Stop", AccessibleLabel: "Stop task", Mode: props.Mode,
					Disabled: !props.Snapshot.TopBar.CanStop || props.OnStopRequested == nil,
					OnClick:  props.OnStopRequested,
				}),
				primitives.Button(primitives.ButtonProps{Label: "◇  Request review", AccessibleLabel: "Request review", Mode: props.Mode}),
				primitives.Button(primitives.ButtonProps{Label: "•••", AccessibleLabel: "More task actions", Mode: props.Mode}),
			),
		),
		taskMetricStrip(tokens),
	)
}

func taskMetricStrip(tokens design.Tokens) ui.Node {
	metrics := []detailRow{
		{"Correctness profile", "Strict"},
		{"Evidence", "In progress"},
		{"Progress", "68%"},
		{"Elapsed", "2.1 min"},
		{"Cost", "$0.42"},
		{"Gates", "3 / 5"},
	}
	nodes := make([]ui.Node, 0, len(metrics))
	for index, metric := range metrics {
		valueColor := tokens.Colors.TextPrimary
		if index == 1 {
			valueColor = tokens.Colors.Evidence
		}
		if index == 2 {
			valueColor = tokens.Colors.Active
		}
		if index == 5 {
			valueColor = tokens.Colors.Success
		}
		nodes = append(nodes, html.Div(html.Props{
			Class: css.New(
				css.PaddingX(css.Px(tokens.Spacing.LG)),
				css.BorderRight(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
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
					css.FontSize(css.Px(tokens.Typography.MetricValue.Size)),
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
		css.MinHeight(css.Px(66)),
		css.Rounded(css.Px(tokens.Geometry.PanelRadius)),
		css.Bg(css.Hex(string(tokens.Colors.SurfaceInset))),
		css.Border(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
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
		content = graphCanvas(props.Nodes, props.SelectedID, tokens, props.OnSelect)
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
			css.Rounded(css.Px(tokens.Geometry.PanelRadius)),
			css.Bg(css.Hex(string(tokens.Colors.Surface1))),
			css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
			css.Border(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
			css.Overflow.Hidden,
		).String(),
	},
		html.Div(html.Props{
			Class: css.New(
				u.Flex, u.ItemsCenter, u.JustifyBetween,
				css.MinHeight(css.Px(44)),
				css.PaddingX(css.Px(tokens.Spacing.MD)),
				css.BorderBottom(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
			).String(),
		},
			html.Div(html.Props{
				Class: css.New(u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.SM))).String(),
			},
				html.H2(html.Props{
					Class: css.New(
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
				Text: "● Code   ● Test   ● Plan   ● Evidence",
			}),
		),
		content,
	)
}

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
			css.BgImage(css.RadialGradient(
				css.CircleSizedAt(css.Px(460), css.Percent(50), css.Percent(45)),
				css.Stop(css.RGBA(60, 151, 255, 0.09)),
				css.StopAt(css.Transparent, css.Percent(72)),
			)),
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
	tone := tokens.Colors.Success
	if strings.Contains(strings.ToLower(node.Title), "plan") ||
		strings.Contains(strings.ToLower(node.Title), "strategy") {
		tone = tokens.Colors.Plan
	} else if strings.Contains(strings.ToLower(node.Title), "evidence") {
		tone = tokens.Colors.Evidence
	} else if node.Status == "active" || node.Status == "running" {
		tone = tokens.Colors.Active
	}
	selected := node.Selected
	if selectedID != "" {
		selected = node.ID == selectedID
	}
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
			css.Zero, css.Zero, css.Px(18), css.Zero, css.RGBA(60, 151, 255, 0.34),
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
		"node-id":  node.ID,
		"status":   node.Status,
		"selected": strconv.FormatBool(selected),
		"position": strconv.Itoa(index),
	}
	buttonProps.Aria = map[string]string{
		"label":   node.Title + " - " + humanize(node.Status),
		"pressed": strconv.FormatBool(selected),
	}
	buttonProps.Class = css.New(rules...).String()
	return html.Button(buttonProps, children...)
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

func compactLabel(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit-1] + "…"
}
