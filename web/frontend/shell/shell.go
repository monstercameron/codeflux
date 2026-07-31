// Package shell provides the GWC v5 application and route shells.
package shell

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"codeflux.dev/codeflux/web/frontend/design"
	frontendi18n "codeflux.dev/codeflux/web/frontend/i18n"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"codeflux.dev/codeflux/web/frontend/routes"
	"codeflux.dev/codeflux/web/frontend/state"
	"github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/css/u"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type RootProps struct {
	Snapshot         state.Snapshot
	Route            routes.Route
	Tokens           design.Tokens
	Translator       frontendi18n.Translator
	Probe            RenderProbe
	OnLayoutChange   func(state.LayoutPreferences)
	OnGraphSelect    func(string)
	OnThemeChange    func()
	OnRetry          func()
	OnPauseRequested func()
	OnStopRequested  func()
}

// AppRoot keeps bootstrap failure states outside authenticated route shells.
func AppRoot(props RootProps) ui.Node {
	switch props.Snapshot.Session.Bootstrap {
	case state.BootstrapBooting:
		return ui.CreateElement(TopLevelState, TopLevelStateProps{
			Kind: "loading", Title: "Starting CodeFlux", Body: "Checking the local coordinator and restoring your session.",
			Mode: primitiveMode(props.Tokens),
		})
	case state.BootstrapIncompatible:
		return ui.CreateElement(TopLevelState, TopLevelStateProps{
			Kind: "incompatible", Title: "Update required",
			Body:        "Reload CodeFlux after the application and coordinator versions match.",
			ActionLabel: "Reload", OnAction: props.OnRetry, Mode: primitiveMode(props.Tokens),
		})
	case state.BootstrapUnauthorized:
		return ui.CreateElement(TopLevelState, TopLevelStateProps{
			Kind: "unauthorized", Title: "Session expired", Body: props.Snapshot.Session.Message,
			ActionLabel: "Retry session", OnAction: props.OnRetry, Mode: primitiveMode(props.Tokens),
		})
	case state.BootstrapCoordinatorUnavailable:
		return ui.CreateElement(TopLevelState, TopLevelStateProps{
			Kind: "coordinator-unavailable", Title: "Coordinator unavailable", Body: props.Snapshot.Session.Message,
			ActionLabel: "Retry", OnAction: props.OnRetry, Mode: primitiveMode(props.Tokens),
		})
	case state.BootstrapDatabaseUnavailable:
		return ui.CreateElement(TopLevelState, TopLevelStateProps{
			Kind: "database-unavailable", Title: "Database unavailable", Body: props.Snapshot.Session.Message,
			ActionLabel: "Retry", OnAction: props.OnRetry, Mode: primitiveMode(props.Tokens),
		})
	case state.BootstrapReady:
		return ui.CreateElement(GlobalErrorBoundary, GlobalErrorBoundaryProps{
			Route: props.Route,
			UI:    state.NewUIStore(props.Snapshot.Layout, nil),
			Child: ui.CreateElement(AppShell, AppShellProps{
				Snapshot: props.Snapshot, Route: props.Route, Tokens: props.Tokens,
				Translator: props.Translator, Probe: props.Probe,
				OnLayoutChange: props.OnLayoutChange, OnGraphSelect: props.OnGraphSelect,
				OnThemeChange:    props.OnThemeChange,
				OnPauseRequested: props.OnPauseRequested, OnStopRequested: props.OnStopRequested,
			}),
		})
	default:
		return ui.CreateElement(TopLevelState, TopLevelStateProps{
			Kind: "unknown", Title: "CodeFlux cannot start", Body: "The application entered an unknown startup state.",
		})
	}
}

type AppShellProps struct {
	Snapshot         state.Snapshot
	Route            routes.Route
	Tokens           design.Tokens
	Translator       frontendi18n.Translator
	Probe            RenderProbe
	OnLayoutChange   func(state.LayoutPreferences)
	OnGraphSelect    func(string)
	OnThemeChange    func()
	OnPauseRequested func()
	OnStopRequested  func()
}

// AppShell composes independent render boundaries for chrome and route content.
func AppShell(props AppShellProps) ui.Node {
	translator := translatorOrEnglish(props.Translator)
	language := translator.DocumentLanguage()
	focusManager := ui.UseFocusManager()
	focusMain := func() {
		focusManager.FocusByID("main-content")
	}
	focusGraph := func() { focusManager.FocusByID("graph-region") }
	layout := props.Snapshot.Layout.Normalize()
	emitShellLayout := func(next state.LayoutPreferences) {
		emitLayout(props.OnLayoutChange, next)
	}
	toggleRail := func() {
		next := layout
		next.RailCollapsed = !next.RailCollapsed
		emitShellLayout(next)
	}
	collapseRail := func() {
		next := layout
		next.RailCollapsed = true
		emitShellLayout(next)
		focusManager.FocusByID("thread-rail-toggle")
	}
	resizeRail := func(delta int) func() {
		return func() {
			next := layout
			next.RailWidth += delta
			emitShellLayout(next)
		}
	}
	helpOpen := ui.UseState(false)
	openShortcutHelp := func() { helpOpen.Set(true) }
	closeShortcutHelp := func() { helpOpen.Set(false) }
	pauseEnabled := props.Snapshot.TopBar.CanPause && props.OnPauseRequested != nil
	stopEnabled := props.Snapshot.TopBar.CanStop && props.OnStopRequested != nil
	announcerPolicy := ui.UseState(state.AnnouncerState{MinimumInterval: 5 * time.Second})
	announcerMessage := ui.UseState("")
	candidate := announcementCandidate(props.Snapshot)
	ui.UseEffectOf(func() func() {
		if candidate.Kind == "" || candidate.Message == "" {
			return nil
		}
		candidate.At = time.Now()
		next, accepted := announcerPolicy.Get().Accept(candidate)
		announcerPolicy.Set(next)
		if accepted {
			announcerMessage.Set(candidate.Message)
		}
		return nil
	}, string(candidate.Kind)+"|"+candidate.Message)
	skipProps := html.PropsOf(
		html.OnClick(focusMain),
		html.OnKeyDown(func(event ui.KeyboardEvent) {
			if event.GetKey() == "Enter" || event.GetKey() == " " {
				event.PreventDefault()
				focusMain()
			}
		}),
	)
	skipProps.Href = "#main-content"
	skipProps.DataAttr = html.DataAttribute{Name: "testid", Value: "skip-link"}
	skipProps.Class = skipLinkClass(props.Tokens)
	return html.Div(html.Props{
		Lang: language.LanguageTag,
		Dir:  language.Direction,
		Data: map[string]string{
			"component":           "app-shell",
			"testid":              "app-root",
			"theme":               string(props.Tokens.Theme),
			"density":             string(props.Tokens.Density),
			"responsive-mode":     string(props.Snapshot.Layout.Viewport),
			"horizontal-overflow": "false",
			"locale":              language.LanguageTag,
		},
		Class: shellClass(props.Tokens),
	},
		ui.CreateElement(GlobalShortcutManager, ShortcutManagerProps{
			Mode: primitiveMode(props.Tokens), HelpOpen: helpOpen.Get(),
			PauseEnabled: pauseEnabled, StopEnabled: stopEnabled,
			OnFocusConversation: focusMain, OnFocusGraph: focusGraph,
			OnPauseRequested: props.OnPauseRequested, OnStopRequested: props.OnStopRequested,
			OnOpenHelp: openShortcutHelp, OnCloseHelp: closeShortcutHelp,
			Children: []ui.Node{
				html.A(skipProps, translator.TextNode(frontendi18n.MsgShellSkipMain)),
				ui.CreateElement(ApplicationBar, ApplicationBarProps{
					Session: props.Snapshot.Session, Workspace: props.Snapshot.Workspace,
					View: props.Snapshot.TopBar, CostLabel: props.Snapshot.CostLabel,
					Revision: props.Snapshot.TopBarRevision(), Mode: primitiveMode(props.Tokens),
					Viewport: props.Snapshot.Layout.Viewport,
					Probe:    props.Probe, OnThemeChange: props.OnThemeChange,
					OnShortcutHelp:   openShortcutHelp,
					OnRailToggle:     toggleRail,
					OnPauseRequested: props.OnPauseRequested, OnStopRequested: props.OnStopRequested,
				}),
				html.Div(html.Props{Class: applicationFrameClass(props.Snapshot.Layout, props.Tokens)},
					ui.CreateElement(ProductSidebar, ProductSidebarProps{
						Snapshot:   props.Snapshot,
						Route:      props.Route,
						Mode:       primitiveMode(props.Tokens),
						OnCollapse: collapseRail,
						OnNarrower: resizeRail(-32),
						OnWider:    resizeRail(32),
					}),
					html.Div(html.Props{
						DataAttr: html.DataAttribute{Name: "component", Value: "route-frame"},
						Class:    routeFrameClass(layout),
					},
						ui.CreateElement(AppRouter, RouteShellProps{
							Snapshot:         props.Snapshot,
							Route:            props.Route,
							Tokens:           props.Tokens,
							Probe:            props.Probe,
							OnLayoutChange:   props.OnLayoutChange,
							OnGraphSelect:    props.OnGraphSelect,
							OnPauseRequested: props.OnPauseRequested,
							OnStopRequested:  props.OnStopRequested,
						}),
					),
					ui.CreateElement(AssuranceRail, AssuranceRailProps{
						Snapshot: props.Snapshot,
						Mode:     primitiveMode(props.Tokens),
					}),
				),
			}}),
		ui.CreateElement(DialogHost, HostProps{}),
		ui.CreateElement(ToastHost, HostProps{}),
		ui.CreateElement(AccessibilityAnnouncer, AnnouncerProps{
			Message: announcerMessage.Get(),
		}),
	)
}

func translatorOrEnglish(translator frontendi18n.Translator) frontendi18n.Translator {
	if translator.Selection().Resolved != "" {
		return translator
	}
	return frontendi18n.EnglishRegistry().Resolve(
		string(frontendi18n.LocaleEnglishUnitedStates),
	)
}

func skipLinkClass(tokens design.Tokens) string {
	rules := []css.Rule{
		u.Absolute,
		css.Left(css.Px(tokens.Spacing.MD)),
		css.Top(css.Px(tokens.Spacing.MD)),
		css.ZIndex(100),
		css.Transform(css.Scale(0)),
		css.Bg(css.Hex(string(tokens.Colors.Accent))),
		css.TextColor(css.Hex(string(tokens.Colors.OnAccent))),
		css.PaddingY(css.Px(tokens.Spacing.SM)),
		css.PaddingX(css.Px(tokens.Spacing.MD)),
		css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
	}
	rules = append(rules, css.Focus(
		css.Transform(css.Scale(1)),
		css.Shadow(css.ShadowOf(
			css.Zero, css.Zero, css.Zero,
			css.Px(tokens.Geometry.FocusRingWidth),
			css.Hex(string(tokens.Colors.FocusRing)),
		)),
	)...)
	return css.New(rules...).String()
}

func shellClass(tokens design.Tokens) string {
	return css.New(
		css.Bg(css.Hex(string(tokens.Colors.Canvas))),
		css.BgImage(css.RadialGradient(
			css.CircleSizedAt(css.Px(620), css.Percent(12), css.Percent(0)),
			css.Stop(css.Hex(string(tokens.Colors.Surface3))),
			css.StopAt(css.Transparent, css.Percent(72)),
		)),
		css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
		css.Font(css.FontStack(tokens.Fonts.UI)),
		css.FontSize(css.Px(tokens.Typography.Body.Size)),
		css.LineHeightLen(css.Px(tokens.Typography.Body.LineHeight)),
		css.MinHeight(css.Vh(100)),
		css.W(css.Full),
		css.MaxWidth(css.Vw(100)),
		css.OverflowX.Hidden,
	).String()
}

type ApplicationBarProps struct {
	Session          state.SessionView
	Workspace        state.WorkspaceView
	View             state.TopBarView
	CostLabel        string
	Revision         uint64
	Mode             primitives.Mode
	Viewport         state.ViewportClass
	Probe            RenderProbe
	OnThemeChange    func()
	OnShortcutHelp   func()
	OnRailToggle     func()
	OnPauseRequested func()
	OnStopRequested  func()
}

var _ = legacyApplicationBar

func legacyApplicationBar(props ApplicationBarProps) ui.Node {
	recordRender(props.Probe, "application-bar", props.Revision)
	tokens := props.Mode.Tokens()
	connection := string(props.View.Connection)
	if connection == "" {
		connection = string(props.Session.Connection)
	}
	repository := props.View.Repository
	if repository == "" {
		repository = props.Workspace.RepositoryName
	}
	if repository == "" {
		repository = "No repository selected"
	}
	return html.Header(html.Props{
		Data: map[string]string{
			"component": "application-bar",
			"revision":  strconv.FormatUint(props.Revision, 10),
		},
		Aria: map[string]string{"label": "Application"},
		Class: css.New(
			u.Flex, u.ItemsCenter, u.JustifyBetween, css.FlexWrap.Wrap,
			css.Gap(css.Px(tokens.Spacing.MD)), css.MinWidth(css.Zero), css.W(css.Full),
			css.MinHeight(css.Px(64)),
			css.PaddingY(css.Px(tokens.Spacing.MD)),
			css.PaddingX(css.Px(tokens.Spacing.XL)),
			css.Bg(css.Hex(string(tokens.Colors.SurfaceRaised))),
			css.BorderBottom(
				css.Px(tokens.Geometry.BorderWidth),
				css.Hex(string(tokens.Colors.BorderSubtle)),
			),
			css.Shadow(css.ShadowOf(
				css.Zero, css.Px(1), css.Px(3), css.Zero, css.RGBA(0, 0, 0, 0.16),
			)),
		).String(),
	},
		html.Div(html.Props{Class: css.New(
			u.Flex, u.ItemsCenter, css.FlexWrap.Wrap, css.MinWidth(css.Zero),
			css.Gap(css.Px(tokens.Spacing.MD)),
		).String()},
			html.Span(html.Props{
				Aria: map[string]string{"hidden": "true"},
				Class: css.New(
					css.W(css.Px(10)), css.H(css.Px(10)),
					css.Rounded(css.Px(tokens.Geometry.PillRadius)),
					css.Bg(css.Hex(string(tokens.Colors.Accent))),
					css.Shadow(css.ShadowOf(
						css.Zero, css.Zero, css.Px(10), css.Zero,
						css.Hex(string(tokens.Colors.Accent)),
					)),
				).String(),
				Text: " ",
			}),
			html.Strong(html.Props{
				Class: css.New(
					css.FontSize(css.Px(tokens.Typography.SectionTitle.Size)),
					css.LineHeightLen(css.Px(tokens.Typography.SectionTitle.LineHeight)),
					css.FontWeight.Semibold,
				).String(),
				Text: "CodeFlux",
			}),
			html.Span(html.Props{
				Class: css.New(
					css.TextColor(css.Hex(string(tokens.Colors.BorderStrong))),
				).String(),
				Text: "/",
			}),
			html.Span(html.Props{
				Class: css.New(css.FontWeight.Medium).String(),
				Text:  repository,
			}),
			field("branch", fallback(props.View.Branch, props.Workspace.Branch), tokens),
			field("worktree", props.View.WorktreeStatus, tokens),
			field("task-state", props.View.TaskState, tokens),
		),
		html.Div(html.Props{
			Aria: map[string]string{"label": "Session summary"},
			Class: css.New(
				u.Flex, u.ItemsCenter, css.FlexWrap.Wrap,
				css.Gap(css.Px(tokens.Spacing.SM)), css.MinWidth(css.Zero),
			).String(),
		},
			html.Span(html.Props{
				Data: map[string]string{"connection": connection},
				Class: css.New(
					u.InlineFlex, u.ItemsCenter,
					css.MinHeight(css.Px(32)),
					css.PaddingX(css.Px(tokens.Spacing.MD)),
					css.Rounded(css.Px(tokens.Geometry.PillRadius)),
					css.Bg(css.Hex(string(tokens.Colors.Selection))),
					css.TextColor(css.Hex(string(tokens.Colors.OnSelection))),
					css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
					css.FontWeight.Semibold,
				).String(),
				Text: "Connection · " + humanize(connection),
			}),
			field("model", props.View.Model, tokens),
			field("effort", props.View.Effort, tokens),
			field("forecast-cost", props.View.ForecastCost, tokens),
			field("actual-cost", fallback(props.View.ActualCost, props.CostLabel), tokens),
			field("hard-budget", props.View.HardBudget, tokens),
			primitives.Button(primitives.ButtonProps{
				Label: "Pause", AccessibleLabel: "Pause task",
				Disabled: !props.View.CanPause || props.OnPauseRequested == nil, Mode: props.Mode,
				OnClick: props.OnPauseRequested,
			}),
			primitives.Button(primitives.ButtonProps{
				Label: "Stop", AccessibleLabel: "Stop task",
				Disabled: !props.View.CanStop || props.OnStopRequested == nil, Mode: props.Mode,
				OnClick: props.OnStopRequested,
			}),
			primitives.Button(primitives.ButtonProps{
				Label: "More", AccessibleLabel: "More task actions", Mode: props.Mode,
			}),
			primitives.Button(primitives.ButtonProps{
				Label: "Theme", AccessibleLabel: "Change color theme", Mode: props.Mode,
				OnClick: props.OnThemeChange,
			}),
		),
	)
}

func field(name, value string, tokens design.Tokens) ui.Node {
	label := ""
	if strings.TrimSpace(value) != "" {
		label = humanize(value)
	} else {
		label = map[string]string{
			"branch":        "Branch pending",
			"worktree":      "Worktree pending",
			"task-state":    "Task · Draft",
			"model":         "Model pending",
			"effort":        "Effort pending",
			"forecast-cost": "Forecast pending",
			"actual-cost":   "Cost pending",
			"hard-budget":   "Budget pending",
		}[name]
		if label == "" {
			label = "Pending"
		}
	}
	return html.Span(html.Props{
		DataAttr: html.DataAttribute{Name: "field", Value: name},
		Class: css.New(
			css.MinHeight(css.Px(28)),
			u.InlineFlex, u.ItemsCenter,
			css.PaddingX(css.Px(tokens.Spacing.SM)),
			css.Rounded(css.Px(tokens.Geometry.RadiusSmall)),
			css.Bg(css.Hex(string(tokens.Colors.Surface2))),
			css.TextColor(css.Hex(string(tokens.Colors.TextSecondary))),
			css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
			css.LineHeightLen(css.Px(tokens.Typography.Metadata.LineHeight)),
		).String(),
		Text: label,
	})
}

type RouteShellProps struct {
	Snapshot         state.Snapshot
	Route            routes.Route
	Tokens           design.Tokens
	Probe            RenderProbe
	OnLayoutChange   func(state.LayoutPreferences)
	OnGraphSelect    func(string)
	OnPauseRequested func()
	OnStopRequested  func()
}

func RouteShell(props RouteShellProps) ui.Node {
	mode := primitiveMode(props.Tokens)
	switch props.Route.Name {
	case routes.RepositoryChooser:
		return ui.CreateElement(RepositoryChooserShell, RepositoryChooserProps{
			State: props.Snapshot.ThreadsState, Mode: mode,
		})
	case routes.ThreadWorkspace:
		return ui.CreateElement(TaskWorkspaceShell, TaskWorkspaceProps{
			Snapshot: props.Snapshot, Tokens: props.Tokens, Probe: props.Probe,
			OnLayoutChange:   props.OnLayoutChange,
			OnGraphSelect:    props.OnGraphSelect,
			OnPauseRequested: props.OnPauseRequested, OnStopRequested: props.OnStopRequested,
		})
	case routes.Memory:
		return ui.CreateElement(MemoryShell, SimpleRouteProps{
			Title: "Memory", State: props.Snapshot.Memory.State, Mode: mode,
		})
	case routes.Settings:
		return ui.CreateElement(SettingsShell, SimpleRouteProps{
			Title: "Settings", State: props.Snapshot.Settings.State, Mode: mode,
		})
	case routes.Diagnostics:
		return ui.CreateElement(DiagnosticsShell, SimpleRouteProps{
			Title: "Diagnostics", State: props.Snapshot.Diagnostics.State, Mode: mode,
		})
	case routes.FirstRun:
		return ui.CreateElement(FirstRunShell, SimpleRouteProps{
			Title: "Welcome to CodeFlux", State: props.Snapshot.FirstRun.State, Mode: mode,
		})
	default:
		return ui.CreateElement(TopLevelState, TopLevelStateProps{
			Kind: "not-found", Title: "Page not found", Body: "Choose a repository to continue.",
		})
	}
}

type TaskWorkspaceProps struct {
	Snapshot         state.Snapshot
	Tokens           design.Tokens
	Probe            RenderProbe
	OnLayoutChange   func(state.LayoutPreferences)
	OnGraphSelect    func(string)
	OnPauseRequested func()
	OnStopRequested  func()
}

func TaskWorkspaceShell(props TaskWorkspaceProps) ui.Node {
	layout := props.Snapshot.Layout.Normalize()
	mode := primitiveMode(props.Tokens)
	rail := ui.CreateElement(ThreadRail, ThreadRailProps{
		State: props.Snapshot.ThreadsState, Threads: props.Snapshot.Threads(),
		SelectedID: props.Snapshot.SelectedThreadID, Revision: props.Snapshot.ThreadRevision(),
		Collapsed: layout.RailCollapsed, Mode: mode, Probe: props.Probe,
		OnCollapse: func() {
			layout.RailCollapsed = true
			emitLayout(props.OnLayoutChange, layout)
		},
		OnRestore: func() {
			layout.RailCollapsed = false
			emitLayout(props.OnLayoutChange, layout)
		},
	})
	conversation := html.Main(html.Props{
		ID: "main-content", TabIndex: -1,
		Data: map[string]string{"focus-region": "conversation", "focus-order": "2"},
		Class: css.New(
			css.MinWidth(css.Zero), css.W(css.Full), css.H(css.Full), css.Overflow.Auto,
		).String(),
	},
		ui.CreateElement(ConversationPane, ConversationPaneProps{
			State: props.Snapshot.ConversationState, Messages: props.Snapshot.Messages(),
			Revision: props.Snapshot.ConversationRevision(), Mode: mode, Probe: props.Probe,
		}),
	)
	graph := ui.CreateElement(GraphPane, GraphPaneProps{
		State: props.Snapshot.GraphState, Nodes: props.Snapshot.GraphNodes(),
		SelectedID: props.Snapshot.SelectedGraphID, Revision: props.Snapshot.GraphRevision(),
		Collapsed: layout.GraphCollapsed, Mode: mode, Probe: props.Probe,
		OnSelect: props.OnGraphSelect,
	})
	split := primitives.ResizableSplit(primitives.ResizableSplitProps{
		ID: "workspace-split", AccessibleLabel: "Resize conversation and task graph",
		Orientation: primitives.SplitHorizontal,
		Value:       float64(layout.SplitPercent), Min: 35, Max: 75, Step: 5,
		Collapsed: layout.GraphCollapsed, Mode: mode,
		OnChange: func(value float64) {
			layout.SplitPercent = int(value)
			emitLayout(props.OnLayoutChange, layout)
		},
		OnCollapse: func() {
			layout.GraphCollapsed = true
			emitLayout(props.OnLayoutChange, layout)
		},
		OnRestore: func() {
			layout.GraphCollapsed = false
			emitLayout(props.OnLayoutChange, layout)
		},
		First: conversation, Second: graph,
	})
	workspace := responsiveWorkspace(
		layout, rail, conversation, graph, split, mode, props.OnLayoutChange,
	)
	return html.Div(html.Props{
		Data: map[string]string{
			"component":       "task-workspace-shell",
			"viewport":        string(layout.Viewport),
			"active-pane":     string(layout.ActivePane),
			"rail-collapsed":  strconv.FormatBool(layout.RailCollapsed),
			"graph-collapsed": strconv.FormatBool(layout.GraphCollapsed),
			"split-percent":   strconv.Itoa(layout.SplitPercent),
		},
		Class: css.New(
			u.Grid,
			css.GridCols(css.MinMax(css.TrackLen(css.Zero), css.Fr(1))),
			css.GridRows(
				css.TrackAuto,
				css.MinMax(css.TrackLen(css.Zero), css.Fr(1)),
			),
			css.Gap(css.Px(props.Tokens.Spacing.SM)),
			css.W(css.Full), css.H(css.Full), css.MaxWidth(css.Full), css.MinWidth(css.Zero),
			css.MinHeight(css.Zero),
			css.Overflow.Hidden,
			css.Padding(css.Px(props.Tokens.Spacing.SM)),
		).String(),
	},
		ui.CreateElement(TaskWorkspaceHeader, TaskWorkspaceHeaderProps{
			Snapshot: props.Snapshot, Mode: mode,
			OnPauseRequested: props.OnPauseRequested, OnStopRequested: props.OnStopRequested,
		}),
		workspace,
	)
}

func responsiveWorkspace(
	layout state.LayoutPreferences,
	rail ui.Node,
	conversation ui.Node,
	graph ui.Node,
	split ui.Node,
	mode primitives.Mode,
	onLayoutChange func(state.LayoutPreferences),
) ui.Node {
	switch layout.Viewport {
	case state.ViewportNarrow, state.ViewportMinimum:
		return narrowWorkspace(layout, rail, conversation, graph, mode, onLayoutChange)
	case state.ViewportMedium:
		return html.Div(html.Props{Class: workspaceGridClass(layout)},
			html.Div(html.Props{Hidden: true}, rail),
			split,
		)
	default:
		return html.Div(html.Props{Class: workspaceGridClass(layout)},
			html.Div(html.Props{Hidden: true}, rail),
			split,
		)
	}
}

func narrowWorkspace(
	layout state.LayoutPreferences,
	rail ui.Node,
	conversation ui.Node,
	graph ui.Node,
	mode primitives.Mode,
	onLayoutChange func(state.LayoutPreferences),
) ui.Node {
	selected := string(layout.ActivePane)
	return html.Div(html.Props{
		Class: css.New(
			u.Grid,
			css.GridCols(css.MinMax(css.TrackLen(css.Zero), css.Fr(1))),
			css.GridRows(
				css.TrackAuto,
				css.MinMax(css.TrackLen(css.Zero), css.Fr(1)),
			),
			css.Gap(css.Px(mode.Tokens().Spacing.MD)),
			css.MinWidth(css.Zero), css.W(css.Full),
			css.H(css.Full), css.MinHeight(css.Zero),
			css.Overflow.Hidden,
		).String(),
	},
		primitives.Tabs(primitives.TabsProps{
			Label: "Workspace panes",
			Items: []primitives.TabItem{
				{ID: string(state.PaneConversation), Label: "Conversation"},
				{ID: string(state.PaneGraph), Label: "Task graph"},
			},
			SelectedID: selected,
			Mode:       mode,
			OnSelect: func(value string) {
				layout.ActivePane = state.Pane(value)
				emitLayout(onLayoutChange, layout)
			},
		}),
		html.Div(html.Props{Hidden: true}, rail),
		html.Div(html.Props{
			Hidden: layout.ActivePane != state.PaneConversation,
			Class:  narrowPaneClass(),
		}, conversation),
		html.Div(html.Props{
			Hidden: layout.ActivePane != state.PaneGraph,
			Class:  narrowPaneClass(),
		}, graph),
	)
}

func narrowPaneClass() string {
	return css.New(
		css.W(css.Full), css.H(css.Full),
		css.MinWidth(css.Zero), css.MinHeight(css.Zero),
		css.Overflow.Hidden,
	).String()
}

func emitLayout(handler func(state.LayoutPreferences), layout state.LayoutPreferences) {
	if handler != nil {
		handler(layout.Normalize())
	}
}

func workspaceGridClass(_ state.LayoutPreferences) string {
	tracks := []css.Track{css.MinMax(css.TrackLen(css.Zero), css.Fr(1))}
	return css.New(
		u.Grid, css.GridCols(tracks...),
		css.Gap(css.Px(12)),
		css.W(css.Full), css.MaxWidth(css.Full), css.MinWidth(css.Zero),
		css.H(css.Full), css.MinHeight(css.Zero),
		css.Overflow.Hidden,
	).String()
}

type TaskWorkspaceHeaderProps struct {
	Snapshot         state.Snapshot
	Mode             primitives.Mode
	OnPauseRequested func()
	OnStopRequested  func()
}

var _ = legacyTaskWorkspaceHeader

func legacyTaskWorkspaceHeader(props TaskWorkspaceHeaderProps) ui.Node {
	title := props.Snapshot.Workspace.RepositoryName
	if title == "" {
		title = "Task workspace"
	}
	tokens := props.Mode.Tokens()
	return html.Section(html.Props{
		DataAttr: html.DataAttribute{Name: "component", Value: "task-workspace-header"},
		Aria:     map[string]string{"label": "Task summary"},
		Class: css.New(
			u.Flex, u.ItemsCenter, u.JustifyBetween, css.FlexWrap.Wrap,
			css.Gap(css.Px(tokens.Spacing.MD)),
			css.PaddingY(css.Px(tokens.Spacing.LG)),
			css.PaddingX(css.Px(tokens.Spacing.SM)), css.MinWidth(css.Zero),
		).String(),
	},
		html.Div(html.Props{},
			html.P(html.Props{
				Class: css.New(
					css.Margin(css.Zero),
					css.TextColor(css.Hex(string(tokens.Colors.Accent))),
					css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
					css.FontWeight.Semibold,
					css.Tracking(css.Ems(0.08)),
					css.TextTransform.Uppercase,
				).String(),
				Text: "Active workspace",
			}),
			html.H1(html.Props{
				Class: css.New(
					css.Margin(css.Zero),
					css.FontSize(css.Px(tokens.Typography.WorkspaceTitle.Size)),
					css.LineHeightLen(css.Px(tokens.Typography.WorkspaceTitle.LineHeight)),
					css.FontWeight.Semibold,
				).String(),
				Text: title,
			}),
		),
		html.P(html.Props{
			Class: css.New(
				css.Margin(css.Zero),
				css.PaddingY(css.Px(tokens.Spacing.SM)),
				css.PaddingX(css.Px(tokens.Spacing.MD)),
				css.Rounded(css.Px(tokens.Geometry.PillRadius)),
				css.Bg(css.Hex(string(tokens.Colors.Surface2))),
				css.TextColor(css.Hex(string(tokens.Colors.TextSecondary))),
				css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
			).String(),
			Text: branchSummary(props.Snapshot.Workspace),
		}),
	)
}

func branchSummary(workspace state.WorkspaceView) string {
	if workspace.Branch == "" {
		return "Repository context is loading"
	}
	if workspace.Dirty {
		return workspace.Branch + " / uncommitted changes"
	}
	return workspace.Branch + " / clean"
}

type ThreadRailProps struct {
	State      state.DataState
	Threads    []state.ThreadView
	SelectedID string
	Revision   uint64
	Collapsed  bool
	Mode       primitives.Mode
	Probe      RenderProbe
	OnCollapse func()
	OnRestore  func()
}

func ThreadRail(props ThreadRailProps) ui.Node {
	recordRender(props.Probe, "thread-rail", props.Revision)
	tokens := props.Mode.Tokens()
	if props.Collapsed {
		return html.Nav(html.Props{
			ID: "thread-rail-region", TabIndex: -1,
			Aria: map[string]string{"label": "Threads"},
			Data: map[string]string{
				"component": "thread-rail", "focus-region": "rail", "focus-order": "1",
			},
			Class: panelClass(props.Mode),
		}, primitives.Button(primitives.ButtonProps{
			Label: "Show threads", AccessibleLabel: "Expand thread rail",
			Mode: props.Mode, OnClick: props.OnRestore,
		}))
	}
	content := asyncStateContent(props.State, "threads", len(props.Threads), props.Mode)
	if props.State == state.DataReady || props.State == state.DataPartialStale {
		items := make([]ui.Node, 0, len(props.Threads))
		for _, thread := range props.Threads {
			aria := map[string]string{}
			if thread.ID == props.SelectedID {
				aria["current"] = "page"
			}
			items = append(items, html.Li(html.Props{
				Data: map[string]string{"thread-id": thread.ID, "status": thread.Status},
				Class: css.New(
					css.MarginY(css.Px(tokens.Spacing.XS)),
				).String(),
			}, html.A(html.Props{
				Href: "#", Aria: aria,
				Class: threadLinkClass(tokens),
				Text:  thread.Title,
			})))
		}
		content = html.Ul(html.Props{
			Class: css.New(
				css.Margin(css.Zero), css.Padding(css.Zero),
			).String(),
		}, items...)
	}
	return html.Nav(html.Props{
		ID: "thread-rail-region", TabIndex: -1,
		Aria: map[string]string{"label": "Threads"},
		Data: map[string]string{
			"component": "thread-rail", "state": string(props.State),
			"revision":     strconv.FormatUint(props.Revision, 10),
			"focus-region": "rail", "focus-order": "1",
		},
		Class: panelClass(props.Mode),
	},
		html.Div(html.Props{
			Class: css.New(u.Flex, u.ItemsCenter, u.JustifyBetween).String(),
		},
			html.Div(html.Props{},
				html.P(html.Props{
					Class: css.New(
						css.Margin(css.Zero),
						css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
						css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
						css.TextTransform.Uppercase,
						css.Tracking(css.Ems(0.08)),
					).String(),
					Text: "Workspace",
				}),
				html.H2(html.Props{
					Class: css.New(
						css.Margin(css.Zero),
						css.FontSize(css.Px(tokens.Typography.PanelHeading.Size)),
					).String(),
					Text: "Threads",
				}),
			),
			primitives.Button(primitives.ButtonProps{
				Label: "Hide threads", AccessibleLabel: "Collapse thread rail",
				Mode: props.Mode, OnClick: props.OnCollapse,
			}),
		),
		html.Div(html.Props{
			Class: css.New(css.FlexGrow(css.Num(1)), css.MinHeight(css.Zero)).String(),
		}, content),
	)
}

func threadLinkClass(tokens design.Tokens) string {
	rules := []css.Rule{
		u.Flex, u.ItemsCenter,
		css.MinHeight(css.Px(tokens.Interaction.MinimumPointerTarget)),
		css.PaddingX(css.Px(tokens.Spacing.MD)),
		css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
		css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
		css.TextDecoration.None,
	}
	rules = append(rules, css.Hover(
		css.Bg(css.Hex(string(tokens.Colors.Surface3))),
	)...)
	return css.New(rules...).String()
}

type ConversationPaneProps struct {
	State    state.DataState
	Messages []state.MessageView
	Revision uint64
	Mode     primitives.Mode
	Probe    RenderProbe
}

func ConversationPane(props ConversationPaneProps) ui.Node {
	recordRender(props.Probe, "conversation-pane", props.Revision)
	tokens := props.Mode.Tokens()
	content := asyncStateContent(props.State, "messages", len(props.Messages), props.Mode)
	if props.State == state.DataReady || props.State == state.DataPartialStale {
		items := make([]ui.Node, 0, len(props.Messages))
		for _, message := range props.Messages {
			items = append(items, html.Article(html.Props{
				Data: map[string]string{
					"message-id": message.ID, "role": message.Role,
					"pending": strconv.FormatBool(message.Pending),
				},
				Class: messageCardClass(message.Role, tokens),
			},
				html.H2(html.Props{
					Class: css.New(
						css.Margin(css.Zero),
						css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
						css.LineHeightLen(css.Px(tokens.Typography.Metadata.LineHeight)),
						css.TextColor(css.Hex(string(tokens.Colors.Accent))),
						css.TextTransform.Uppercase,
						css.Tracking(css.Ems(0.08)),
					).String(),
					Text: humanize(message.Role),
				}),
				html.P(html.Props{
					Class: css.New(
						css.MarginY(css.Px(tokens.Spacing.SM)),
						css.TextColor(css.Hex(string(tokens.Colors.TextSecondary))),
					).String(),
					Text: message.Body,
				}),
			))
		}
		content = html.Div(html.Props{}, items...)
	}
	return html.Section(html.Props{
		Aria: map[string]string{"label": "Conversation"},
		Data: map[string]string{
			"component": "conversation-pane", "state": string(props.State),
			"revision": strconv.FormatUint(props.Revision, 10),
		},
		Class: panelClass(props.Mode),
	},
		html.Div(html.Props{
			Class: css.New(u.Flex, u.ItemsCenter, u.JustifyBetween).String(),
		},
			html.Div(html.Props{},
				html.P(html.Props{
					Class: css.New(
						css.Margin(css.Zero),
						css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
						css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
						css.TextTransform.Uppercase,
						css.Tracking(css.Ems(0.08)),
					).String(),
					Text: "Live session",
				}),
				html.H2(html.Props{
					Class: css.New(
						css.Margin(css.Zero),
						css.FontSize(css.Px(tokens.Typography.SectionTitle.Size)),
					).String(),
					Text: "Conversation",
				}),
			),
			html.Span(html.Props{
				Class: css.New(
					css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
					css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				).String(),
				Text: fmt.Sprintf("%d messages", len(props.Messages)),
			}),
		),
		html.Div(html.Props{
			Class: css.New(
				css.FlexGrow(css.Num(1)), css.MinHeight(css.Zero), css.OverflowY.Auto,
			).String(),
		}, content),
		html.Div(html.Props{
			Role: "group",
			Aria: map[string]string{"label": "Message composer"},
			Data: map[string]string{"focus-region": "composer", "focus-order": "3"},
			Class: css.New(
				u.Flex, u.ItemsEnd,
				css.Gap(css.Px(tokens.Spacing.SM)),
				css.PaddingY(css.Px(tokens.Spacing.MD)),
				css.BorderTop(
					css.Px(tokens.Geometry.BorderWidth),
					css.Hex(string(tokens.Colors.BorderSubtle)),
				),
			).String(),
		},
			html.Div(html.Props{
				Class: css.New(css.FlexGrow(css.Num(1)), css.MinWidth(css.Zero)).String(),
			}, primitives.TextField(primitives.TextFieldProps{
				ID:          "thread-composer",
				Label:       "Message",
				Placeholder: "Describe the next change or ask a question",
				Mode:        props.Mode,
			})),
			primitives.Button(primitives.ButtonProps{
				Label: "Send", AccessibleLabel: "Send message",
				Primary: true, Mode: props.Mode,
			}),
		),
	)
}

func messageCardClass(role string, tokens design.Tokens) string {
	tone := tokens.Colors.BorderStrong
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "requirement", "execution":
		tone = tokens.Colors.Active
	case "forecast", "validation":
		tone = tokens.Colors.Success
	case "plan":
		tone = tokens.Colors.Plan
	case "evidence":
		tone = tokens.Colors.Evidence
	}
	return css.New(
		css.MarginY(css.Px(tokens.Spacing.SM)),
		css.Padding(css.Px(tokens.Spacing.MD)),
		css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
		css.Bg(css.Hex(string(tokens.Colors.Surface2))),
		css.Border(css.Px(tokens.Geometry.BorderWidth), css.Hex(string(tokens.Colors.BorderSubtle))),
		css.BorderLeft(css.Px(3), css.Hex(string(tone))),
	).String()
}

type GraphPaneProps struct {
	State      state.DataState
	Nodes      []state.GraphNodeView
	SelectedID string
	Revision   uint64
	Collapsed  bool
	Mode       primitives.Mode
	Probe      RenderProbe
	OnSelect   func(string)
}

var _ = legacyGraphPane

func legacyGraphPane(props GraphPaneProps) ui.Node {
	recordRender(props.Probe, "graph-pane", props.Revision)
	tokens := props.Mode.Tokens()
	if props.Collapsed {
		return html.Aside(html.Props{
			Hidden: true, Aria: map[string]string{"label": "Task graph"},
			DataAttr: html.DataAttribute{Name: "component", Value: "graph-pane"},
			Class:    panelClass(props.Mode),
		})
	}
	content := asyncStateContent(props.State, "task graph nodes", len(props.Nodes), props.Mode)
	if props.State == state.DataReady || props.State == state.DataPartialStale {
		items := make([]ui.Node, 0, len(props.Nodes))
		for _, node := range props.Nodes {
			label := node.Title + " - " + humanize(node.Status)
			items = append(items, html.Li(html.Props{
				Data: map[string]string{
					"node-id": node.ID, "status": node.Status,
					"selected": strconv.FormatBool(node.ID == props.SelectedID),
				},
				Class: css.New(
					css.MarginY(css.Px(tokens.Spacing.SM)),
				).String(),
			},
				html.Button(html.Props{
					Type: "button", Text: label,
					Aria: map[string]string{"pressed": strconv.FormatBool(node.ID == props.SelectedID)},
					Class: css.New(
						u.Flex, u.ItemsCenter,
						css.W(css.Full),
						css.MinHeight(css.Px(tokens.Interaction.MinimumPointerTarget)),
						css.PaddingX(css.Px(tokens.Spacing.MD)),
						css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
						css.Bg(css.Hex(string(tokens.Colors.Surface2))),
						css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
						css.Border(
							css.Px(tokens.Geometry.BorderWidth),
							css.Hex(string(tokens.Colors.BorderSubtle)),
						),
						css.Cursor.Pointer,
					).String(),
				}),
			))
		}
		content = html.Ul(html.Props{
			Class: css.New(
				css.Margin(css.Zero), css.Padding(css.Zero),
			).String(),
		}, items...)
	}
	return html.Aside(html.Props{
		Aria: map[string]string{"label": "Task graph"},
		Data: map[string]string{
			"component": "graph-pane", "state": string(props.State),
			"revision": strconv.FormatUint(props.Revision, 10),
		},
		Class: panelClass(props.Mode),
	},
		html.P(html.Props{
			Class: css.New(
				css.Margin(css.Zero),
				css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
				css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				css.TextTransform.Uppercase,
				css.Tracking(css.Ems(0.08)),
			).String(),
			Text: "Execution map",
		}),
		html.H2(html.Props{
			Class: css.New(
				css.Margin(css.Zero),
				css.FontSize(css.Px(tokens.Typography.SectionTitle.Size)),
			).String(),
			Text: "Task graph",
		}),
		content,
	)
}

func panelClass(mode primitives.Mode) string {
	tokens := mode.Tokens()
	return css.New(
		u.Flex, u.FlexCol,
		css.Gap(css.Px(tokens.Spacing.MD)),
		css.Bg(css.Hex(string(tokens.Colors.Surface1))),
		css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
		css.Border(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
		css.Padding(css.Px(tokens.Rhythm.PanelInset)),
		css.Rounded(css.Px(tokens.Geometry.PanelRadius)),
		css.Shadow(css.ShadowOf(
			css.Zero, css.Px(1), css.Px(3), css.Zero, css.RGBA(0, 0, 0, 0.12),
		)),
		css.H(css.Full),
		css.MinWidth(css.Zero), css.MaxWidth(css.Full), css.Overflow.Hidden,
	).String()
}

func asyncStateContent(kind state.DataState, subject string, count int, mode primitives.Mode) ui.Node {
	switch kind {
	case state.DataNotRequested:
		return html.P(html.Props{Text: "Not requested"})
	case state.DataLoading:
		return primitives.Skeleton(primitives.SkeletonProps{
			AccessibleLabel: "Loading " + subject, Lines: 4, Mode: mode,
		})
	case state.DataReadyEmpty:
		return primitives.EmptyState(primitives.EmptyStateProps{
			Title: "No " + subject, Body: "There is nothing to show yet.", Mode: mode,
		})
	case state.DataReady:
		return html.P(html.Props{Text: fmt.Sprintf("%d %s", count, subject)})
	case state.DataPartialStale:
		return html.Div(html.Props{
			Role: "status", Text: fmt.Sprintf("Showing %d %s; updates are delayed", count, subject),
		})
	case state.DataRecoverableError:
		return primitives.ErrorState(primitives.ErrorStateProps{
			Title: "Could not load " + subject, Body: "Retry is available.", ActionLabel: "Retry", Mode: mode,
		})
	case state.DataDenied:
		return primitives.InlineAlert(primitives.InlineAlertProps{
			Title: "Access denied", Message: "Access to " + subject + " is denied",
			Tone: design.StatusWarning, Mode: mode,
		})
	case state.DataIncompatible:
		return primitives.InlineAlert(primitives.InlineAlertProps{
			Title: "Update required", Message: subject + " require a compatible CodeFlux version",
			Tone: design.StatusFailure, Mode: mode,
		})
	case state.DataDisconnected:
		return html.Div(html.Props{Role: "status", Text: subject + " are offline; reconnecting"})
	default:
		return html.Div(html.Props{Role: "alert", Text: "Unknown " + subject + " state"})
	}
}

func primitiveMode(tokens design.Tokens) primitives.Mode {
	return primitives.Mode{
		Theme: tokens.Theme, Density: tokens.Density,
		HighContrast:  tokens.Theme == design.ThemeHighContrast,
		ReducedMotion: tokens.ReducedMotion,
	}
}

type RepositoryChooserProps struct {
	State state.DataState
	Mode  primitives.Mode
}

func RepositoryChooserShell(props RepositoryChooserProps) ui.Node {
	return routeMain("repository-chooser-shell", "Choose a repository", props.Mode,
		routeRegion(props.Mode, "recent-workspaces", "Recent valid workspaces",
			asyncStateContent(props.State, "recent workspaces", 0, props.Mode)),
		routeRegion(props.Mode, "browse-open", "Browse or open",
			html.P(html.Props{Text: "Open an authorized local repository."}),
			primitives.Button(primitives.ButtonProps{
				Label: "Browse repositories", Primary: true, Mode: props.Mode,
			}),
		),
		routeRegion(props.Mode, "canonical-path", "Canonical path result",
			html.P(html.Props{Text: "CodeFlux confirms the canonical path after authorization."}),
		),
		routeRegion(props.Mode, "warnings", "Warnings and recovery",
			html.P(html.Props{Text: "Unavailable repositories stay closed and can be retried."}),
		),
	)
}

type SimpleRouteProps struct {
	Title string
	State state.DataState
	Mode  primitives.Mode
}

func routeStateContent(props SimpleRouteProps, subject string, ready ...ui.Node) ui.Node {
	var content ui.Node
	switch props.State {
	case state.DataReady:
		content = html.Div(html.Props{}, ready...)
	case state.DataPartialStale:
		children := []ui.Node{html.P(html.Props{
			Role: "status", Text: "Showing cached " + subject + "; updates are delayed.",
		})}
		content = html.Div(html.Props{}, append(children, ready...)...)
	default:
		content = asyncStateContent(props.State, subject, 0, props.Mode)
	}
	return html.Div(html.Props{
		Data: map[string]string{"owner": subject, "state": string(props.State)},
	}, content)
}

func MemoryShell(props SimpleRouteProps) ui.Node {
	return routeMain("memory-shell", props.Title, props.Mode,
		routeRegion(props.Mode, "memory-list", "Memory list",
			routeStateContent(props, "memory entries",
				html.P(html.Props{Text: "Durable memory entries appear here with source labels."}),
			),
		),
		routeRegion(props.Mode, "memory-details", "Memory details",
			routeStateContent(props, "memory details",
				html.P(html.Props{Text: "Select an entry to inspect provenance and scope."}),
			),
		),
		routeRegion(props.Mode, "memory-actions", "Memory actions",
			routeStateContent(props, "memory actions",
				html.P(html.Props{Text: "Authorized memory actions appear here."}),
			),
		),
	)
}

func SettingsShell(props SimpleRouteProps) ui.Node {
	return routeMain("settings-shell", props.Title, props.Mode,
		routeRegion(props.Mode, "providers", "Providers",
			routeStateContent(props, "providers", html.P(html.Props{Text: "Provider connections and capabilities."}))),
		routeRegion(props.Mode, "models", "Models",
			routeStateContent(props, "models", html.P(html.Props{Text: "Model and effort defaults."}))),
		routeRegion(props.Mode, "policy", "Policy",
			routeStateContent(props, "policy", html.P(html.Props{Text: "Approval, budget, and execution policy."}))),
		routeRegion(props.Mode, "appearance", "Appearance",
			routeStateContent(props, "appearance preferences", html.P(html.Props{Text: "Theme, density, and motion preferences."}))),
		routeRegion(props.Mode, "data", "Data",
			routeStateContent(props, "data controls", html.P(html.Props{Text: "Backup, retention, and local data controls."}))),
	)
}

func DiagnosticsShell(props SimpleRouteProps) ui.Node {
	return routeMain("diagnostics-shell", props.Title, props.Mode,
		routeRegion(props.Mode, "health", "Health",
			routeStateContent(props, "health", html.P(html.Props{Text: "Coordinator and database health."}))),
		routeRegion(props.Mode, "versions", "Versions",
			routeStateContent(props, "versions", html.P(html.Props{Text: "Application, API, schema, and frontend versions."}))),
		routeRegion(props.Mode, "tasks", "Tasks",
			routeStateContent(props, "tasks", html.P(html.Props{Text: "Active Task and Attempt summaries."}))),
		routeRegion(props.Mode, "logs", "Logs",
			routeStateContent(props, "logs", html.P(html.Props{Text: "Redacted local diagnostic logs."}))),
		routeRegion(props.Mode, "backup", "Backup",
			routeStateContent(props, "backup status", html.P(html.Props{Text: "Local backup status and recovery guidance."}))),
		routeRegion(props.Mode, "export", "Export",
			routeStateContent(props, "exports", html.P(html.Props{Text: "Create a redacted support export."}))),
		routeRegion(props.Mode, "terminology", "Terminology",
			routeStateContent(props, "terminology",
				html.P(html.Props{Text: "A Thread contains conversation. A Task is durable work. An Attempt is one execution. A Plan revision changes the approach. An Approval authorizes a gated action. A Checkpoint is restorable state. Recovery resumes safely."}),
			),
		),
	)
}

func FirstRunShell(props SimpleRouteProps) ui.Node {
	return routeMain("first-run-shell", props.Title, props.Mode,
		routeRegion(props.Mode, "local-promise", "Local-first promise",
			routeStateContent(props, "local-first setup", html.P(html.Props{Text: "Your coordinator and durable state stay local."}))),
		routeRegion(props.Mode, "provider", "Provider",
			routeStateContent(props, "provider setup", html.P(html.Props{Text: "Connect an authorized model provider."}))),
		routeRegion(props.Mode, "repository", "Repository",
			routeStateContent(props, "repository setup", html.P(html.Props{Text: "Choose an authorized repository."}))),
		routeRegion(props.Mode, "worktree-permissions", "Worktree and permissions",
			routeStateContent(props, "worktree permissions", html.P(html.Props{Text: "Review file and command boundaries."}))),
		routeRegion(props.Mode, "first-thread", "First Thread",
			routeStateContent(props, "first Thread setup", html.P(html.Props{Text: "Create the first Thread after setup is complete."}))),
	)
}

func routeMain(component, title string, mode primitives.Mode, regions ...ui.Node) ui.Node {
	tokens := mode.Tokens()
	gridRules := []css.Rule{
		u.Grid,
		css.GridCols(css.MinMax(css.TrackLen(css.Zero), css.Fr(1))),
		css.Gap(css.Px(tokens.Spacing.LG)),
	}
	gridRules = append(gridRules, css.Media(
		css.MinW(900),
		css.GridCols(
			css.MinMax(css.TrackLen(css.Zero), css.Fr(1)),
			css.MinMax(css.TrackLen(css.Zero), css.Fr(1)),
		),
	)...)
	return html.Main(html.Props{
		ID: "main-content", TabIndex: -1,
		DataAttr: html.DataAttribute{Name: "component", Value: component},
		Class: css.New(
			css.W(css.Full), css.MaxWidth(css.Px(1180)),
			css.MarginX(css.Auto),
			css.PaddingY(css.Px(tokens.Spacing.XXL)),
			css.PaddingX(css.Px(tokens.Spacing.XL)),
			css.MinWidth(css.Zero), css.Overflow.Hidden,
		).String(),
	},
		html.Header(html.Props{
			Class: css.New(
				css.MarginY(css.Px(tokens.Spacing.XXL)),
				css.MaxWidth(css.Px(720)),
			).String(),
		},
			html.P(html.Props{
				Class: css.New(
					css.Margin(css.Zero),
					css.TextColor(css.Hex(string(tokens.Colors.Accent))),
					css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
					css.FontWeight.Semibold,
					css.TextTransform.Uppercase,
					css.Tracking(css.Ems(0.1)),
				).String(),
				Text: "Local agent workspace",
			}),
			html.H1(html.Props{
				Class: css.New(
					css.MarginY(css.Px(tokens.Spacing.SM)),
					css.FontSize(css.Px(tokens.Typography.WorkspaceTitle.Size)),
					css.LineHeightLen(css.Px(tokens.Typography.WorkspaceTitle.LineHeight)),
					css.FontWeight.Semibold,
				).String(),
				Text: title,
			}),
			html.P(html.Props{
				Class: css.New(
					css.Margin(css.Zero),
					css.TextColor(css.Hex(string(tokens.Colors.TextSecondary))),
					css.FontSize(css.Px(tokens.Typography.Body.Size)),
				).String(),
				Text: "Private by default. Explicit at every boundary. Built for focused technical work.",
			}),
		),
		html.Div(html.Props{Class: css.New(gridRules...).String()}, regions...),
	)
}

func routeRegion(mode primitives.Mode, name, title string, children ...ui.Node) ui.Node {
	tokens := mode.Tokens()
	return html.Section(html.Props{
		DataAttr: html.DataAttribute{Name: "region", Value: name},
		Class: css.New(
			u.Flex, u.FlexCol,
			css.Gap(css.Px(tokens.Spacing.SM)),
			css.Padding(css.Px(tokens.Spacing.XL)),
			css.MinHeight(css.Px(176)),
			css.Rounded(css.Px(tokens.Geometry.PanelRadius)), css.MinWidth(css.Zero),
			css.Bg(css.Hex(string(tokens.Colors.Surface1))),
			css.Border(
				css.Px(tokens.Geometry.BorderWidth),
				css.Hex(string(tokens.Colors.BorderSubtle)),
			),
			css.Shadow(css.ShadowOf(
				css.Zero, css.Px(1), css.Px(4), css.Zero, css.RGBA(0, 0, 0, 0.12),
			)),
		).String(),
	}, append([]ui.Node{html.H2(html.Props{
		Class: css.New(
			css.Margin(css.Zero),
			css.FontSize(css.Px(tokens.Typography.SectionTitle.Size)),
			css.LineHeightLen(css.Px(tokens.Typography.SectionTitle.LineHeight)),
			css.FontWeight.Semibold,
		).String(),
		Text: title,
	})}, children...)...)
}

type TopLevelStateProps struct {
	Kind        string
	Title       string
	Body        string
	ActionLabel string
	OnAction    func()
	Mode        primitives.Mode
}

func TopLevelState(props TopLevelStateProps) ui.Node {
	children := []ui.Node{
		html.H1(html.Props{Text: props.Title}),
		html.P(html.Props{Role: "alert", Text: fallback(props.Body, "Try again or open diagnostics.")}),
	}
	if props.ActionLabel != "" {
		children = append(children, primitives.Button(primitives.ButtonProps{
			Label: props.ActionLabel, Mode: props.Mode, OnClick: props.OnAction,
		}))
	}
	return html.Main(html.Props{
		ID: "main-content", TabIndex: -1,
		Data: map[string]string{"component": "top-level-state", "state": props.Kind},
	}, children...)
}

type AnnouncerProps struct{ Message string }

func Announcer(props AnnouncerProps) ui.Node {
	return html.Div(html.Props{
		DataAttr: html.DataAttribute{Name: "component", Value: "announcer"},
		Aria: map[string]string{
			"live": "polite", "atomic": "true",
		},
		Class: css.New(
			u.Absolute,
			css.W(css.Px(1)), css.H(css.Px(1)),
			css.Overflow.Hidden,
		).String(),
		Text: props.Message,
	})
}

func announcementCandidate(snapshot state.Snapshot) state.Announcement {
	if snapshot.Review.ApprovalID != "" {
		return state.Announcement{
			Kind: state.AnnouncementApproval, Message: "Approval required",
		}
	}
	taskState := strings.ToLower(strings.TrimSpace(snapshot.TopBar.TaskState))
	switch {
	case strings.Contains(taskState, "paused"):
		return state.Announcement{
			Kind: state.AnnouncementPause, Message: "Task paused",
		}
	case strings.Contains(taskState, "complete"):
		return state.Announcement{
			Kind: state.AnnouncementCompletion, Message: "Task completed",
		}
	case strings.Contains(taskState, "validation") && strings.Contains(taskState, "fail"):
		return state.Announcement{
			Kind: state.AnnouncementValidationFailure, Message: "Validation failed",
		}
	case strings.Contains(taskState, "failed") || strings.Contains(taskState, "failure"):
		return state.Announcement{
			Kind: state.AnnouncementFailure, Message: "Task failed",
		}
	}
	switch snapshot.Session.Connection {
	case state.ConnectionLive:
		return state.Announcement{
			Kind: state.AnnouncementConnection, Message: "Connection restored",
		}
	case state.ConnectionRecovering:
		return state.Announcement{
			Kind: state.AnnouncementRecovery, Message: "Live updates are reconnecting",
		}
	case state.ConnectionOffline:
		return state.Announcement{
			Kind: state.AnnouncementConnection, Message: "CodeFlux is offline",
		}
	default:
		return state.Announcement{}
	}
}

func fallback(value, replacement string) string {
	if strings.TrimSpace(value) == "" {
		return replacement
	}
	return value
}

func humanize(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "-", " "))
	if value == "" {
		return "Unknown"
	}
	return strings.ToUpper(value[:1]) + value[1:]
}
