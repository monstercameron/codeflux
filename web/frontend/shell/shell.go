// Package shell provides the GWC v5 application and route shells.
package shell

import (
	"fmt"
	"strconv"
	"strings"

	"codeflux.dev/codeflux/web/frontend/codecollection"
	frontendcomposer "codeflux.dev/codeflux/web/frontend/composer"
	"codeflux.dev/codeflux/web/frontend/design"
	"codeflux.dev/codeflux/web/frontend/executionview"
	"codeflux.dev/codeflux/web/frontend/graphcanvas"
	frontendi18n "codeflux.dev/codeflux/web/frontend/i18n"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"codeflux.dev/codeflux/web/frontend/routes"
	"codeflux.dev/codeflux/web/frontend/settingsview"
	"codeflux.dev/codeflux/web/frontend/state"
	"codeflux.dev/codeflux/web/frontend/taskcontrols"
	"codeflux.dev/codeflux/web/frontend/telemetryview"
	"codeflux.dev/codeflux/web/frontend/timelinecard"
	"codeflux.dev/codeflux/web/frontend/timelineview"
	"github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/css/u"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type RootProps struct {
	Snapshot     state.Snapshot
	Composer     frontendcomposer.Props
	Timeline     TimelineControlProps
	TaskControls *taskcontrols.Props
	Telemetry    telemetryview.Props
	// Appearance is the settings page's theme, density, and motion panel,
	// supplied by the application that owns those choices.
	Appearance ui.Node
	// LocalData is the settings page's browser-storage panel.
	LocalData          ui.Node
	Settings           settingsview.Props
	Collection         codecollection.Props
	ThreadRail         ui.Node
	AuthoritativeGraph *graphcanvas.AuthoritativeProps
	RepositoryChoices  *RepositoryChoiceSet
	SelectedScope      NavigationScope
	GraphInspector     ui.Node
	// Memory is the project-memory surface, supplied by the application
	// because reading memory is a coordinator call the shell must not make.
	Memory *MemoryWorkspaceProps
	// Atoms is the atom collection surface. It is a mounted node rather than a
	// prop tree because the surface owns its own search and selection: passing
	// that state through here would make every keystroke re-render the shell.
	Atoms ui.Node
	// FileTree is the repository drawn as its own directory tree, with one
	// file open. It is a mounted node for the same reason Atoms is.
	FileTree ui.Node
	// Repositories is the repositories surface, supplied for the same reason:
	// listing repositories and reading a working tree are coordinator calls.
	Repositories         *RepositoryWorkspaceProps
	Route                routes.Route
	Tokens               design.Tokens
	Translator           frontendi18n.Translator
	Probe                RenderProbe
	OnLayoutChange       func(state.LayoutPreferences)
	OnGraphSelect        func(string)
	OnThreadNavigate     func(routes.Route)
	OnNavigatePath       func(string)
	OnThemeChange        func()
	OnReconnectRequested func()
	OnRetry              func()
	OnPauseRequested     func()
	OnStopRequested      func()
	// Execution carries the live run surfaces down to the workspace.
	Execution *ExecutionPanelProps
	// TaskActionsOpen and its two callbacks are the workspace's overflow menu,
	// owned by the application so no route surface has to hold a hook.
	TaskActionsOpen      bool
	OnTaskActionsOpen    func()
	OnTaskActionsDismiss func()
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
				TaskActionsOpen:      props.TaskActionsOpen,
				OnTaskActionsOpen:    props.OnTaskActionsOpen,
				OnTaskActionsDismiss: props.OnTaskActionsDismiss,
				Snapshot:             props.Snapshot, Route: props.Route, Tokens: props.Tokens,
				Composer: props.Composer, Timeline: props.Timeline,
				TaskControls: props.TaskControls, ThreadRail: props.ThreadRail,
				AuthoritativeGraph: props.AuthoritativeGraph, GraphInspector: props.GraphInspector,
				Memory:            props.Memory,
				Atoms:             props.Atoms,
				FileTree:          props.FileTree,
				Repositories:      props.Repositories,
				Appearance:        props.Appearance,
				LocalData:         props.LocalData,
				RepositoryChoices: props.RepositoryChoices,
				SelectedScope:     props.SelectedScope,
				Telemetry:         props.Telemetry,
				Settings:          props.Settings,
				Collection:        props.Collection,
				Translator:        props.Translator, Probe: props.Probe,
				OnLayoutChange: props.OnLayoutChange, OnGraphSelect: props.OnGraphSelect,
				OnThreadNavigate:     props.OnThreadNavigate,
				OnNavigatePath:       props.OnNavigatePath,
				OnThemeChange:        props.OnThemeChange,
				OnReconnectRequested: props.OnReconnectRequested,
				OnPauseRequested:     props.OnPauseRequested, OnStopRequested: props.OnStopRequested,
				Execution: props.Execution,
			}),
		})
	default:
		return ui.CreateElement(TopLevelState, TopLevelStateProps{
			Kind: "unknown", Title: "CodeFlux cannot start", Body: "The application entered an unknown startup state.",
		})
	}
}

type AppShellProps struct {
	Snapshot     state.Snapshot
	Composer     frontendcomposer.Props
	Timeline     TimelineControlProps
	TaskControls *taskcontrols.Props
	Telemetry    telemetryview.Props
	// Appearance is the settings page's theme, density, and motion panel,
	// supplied by the application that owns those choices.
	Appearance ui.Node
	// LocalData is the settings page's browser-storage panel.
	LocalData          ui.Node
	Settings           settingsview.Props
	Collection         codecollection.Props
	ThreadRail         ui.Node
	AuthoritativeGraph *graphcanvas.AuthoritativeProps
	RepositoryChoices  *RepositoryChoiceSet
	SelectedScope      NavigationScope
	GraphInspector     ui.Node
	// Memory is the project-memory surface, supplied by the application
	// because reading memory is a coordinator call the shell must not make.
	Memory *MemoryWorkspaceProps
	// Atoms is the atom collection surface. It is a mounted node rather than a
	// prop tree because the surface owns its own search and selection: passing
	// that state through here would make every keystroke re-render the shell.
	Atoms ui.Node
	// FileTree is the repository drawn as its own directory tree, with one
	// file open. It is a mounted node for the same reason Atoms is.
	FileTree ui.Node
	// Repositories is the repositories surface, supplied for the same reason:
	// listing repositories and reading a working tree are coordinator calls.
	Repositories         *RepositoryWorkspaceProps
	Route                routes.Route
	Tokens               design.Tokens
	Translator           frontendi18n.Translator
	Probe                RenderProbe
	OnLayoutChange       func(state.LayoutPreferences)
	OnGraphSelect        func(string)
	OnThreadNavigate     func(routes.Route)
	OnNavigatePath       func(string)
	OnThemeChange        func()
	OnReconnectRequested func()
	OnPauseRequested     func()
	OnStopRequested      func()
	// Execution carries the live run surfaces down to the workspace.
	Execution *ExecutionPanelProps
	// TaskActionsOpen and its two callbacks are the workspace's overflow menu,
	// owned by the application so no route surface has to hold a hook.
	TaskActionsOpen      bool
	OnTaskActionsOpen    func()
	OnTaskActionsDismiss func()
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
	compactRailOpen := ui.UseState(false)
	compactViewport := layout.Viewport == state.ViewportNarrow || layout.Viewport == state.ViewportMinimum
	railOpen := !layout.RailCollapsed
	if compactViewport {
		railOpen = compactRailOpen.Get()
	}
	emitShellLayout := func(next state.LayoutPreferences) {
		emitLayout(props.OnLayoutChange, next)
	}
	toggleRail := func() {
		if compactViewport {
			compactRailOpen.Update(toggleLocalVisibility)
			return
		}
		next := layout
		next.RailCollapsed = !next.RailCollapsed
		emitShellLayout(next)
	}
	collapseRail := func() {
		if compactViewport {
			compactRailOpen.Update(closeLocalVisibility)
			focusManager.FocusByID("thread-rail-toggle")
			return
		}
		next := layout
		next.RailCollapsed = true
		emitShellLayout(next)
		focusManager.FocusByID("thread-rail-toggle")
	}
	resizeRail := func(direction int) func() {
		return func() {
			next := layout
			next.RailWidth = nextRailWidth(layout.RailWidth, direction)
			emitShellLayout(next)
		}
	}
	inspectorCollapsed := ui.UseState(false)
	inspectorHidden := inspectorCollapsed.Get() || !routeIsAboutARun(props.Route.Name)
	toggleInspector := func() { inspectorCollapsed.Update(toggleLocalVisibility) }
	collapseInspector := func() {
		inspectorCollapsed.Update(openLocalVisibility)
		focusManager.FocusByID("assurance-rail-toggle")
	}
	helpOpen := ui.UseState(false)
	openShortcutHelp := func() { helpOpen.Update(openLocalVisibility) }
	closeShortcutHelp := func() { helpOpen.Update(closeLocalVisibility) }
	searchOpen := ui.UseState(false)
	searchQuery := ui.UseState("")
	openSearch := func() { searchOpen.Set(true) }
	closeSearch := func() {
		searchOpen.Set(false)
		searchQuery.Set("")
	}
	// The graph and the execution panels are built here so the observation rail
	// can hold them. They are only built for the width that draws that rail;
	// below it the workspace still owns them, and building both would mount the
	// same authoritative surfaces twice.
	var observationGraph ui.Node
	var observationExecution ui.Node
	if layout.Viewport == state.ViewportWide && props.Route.Name != routes.Graphs {
		observationGraph = RailGraphSummary(
			props.Snapshot.GraphNodes(), props.AuthoritativeGraph,
			props.Snapshot.GraphRevision(),
			props.Tokens, primitiveMode(props.Tokens), props.OnNavigatePath,
		)
		if props.Execution != nil {
			observationExecution = compactExecutionPanels(*props.Execution, primitiveMode(props.Tokens))
		}
	}
	pauseEnabled := props.Snapshot.TopBar.CanPause && props.OnPauseRequested != nil
	stopEnabled := props.Snapshot.TopBar.CanStop && props.OnStopRequested != nil
	candidate := announcementCandidate(props.Snapshot)
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
					OnShortcutHelp:       openShortcutHelp,
					SearchOpen:           searchOpen.Get(),
					SearchQuery:          searchQuery.Get(),
					RailOpen:             railOpen,
					OnSearchOpen:         openSearch,
					OnSearchDismiss:      closeSearch,
					OnSearchQueryChange:  searchQuery.Set,
					OnRailToggle:         toggleRail,
					OnInspectorToggle:    toggleInspector,
					OnNavigatePath:       props.OnNavigatePath,
					OnReconnectRequested: props.OnReconnectRequested,
					OnPauseRequested:     props.OnPauseRequested, OnStopRequested: props.OnStopRequested,
				}),
				html.Div(html.Props{Class: applicationFrameClass(
					props.Snapshot.Layout, props.Tokens, inspectorHidden,
				)},
					ui.CreateElement(ProductSidebar, ProductSidebarProps{
						Snapshot:         props.Snapshot,
						Route:            props.Route,
						SelectedScope:    props.SelectedScope,
						Mode:             primitiveMode(props.Tokens),
						ThreadRail:       props.ThreadRail,
						CompactOpen:      compactRailOpen.Get(),
						OnThreadNavigate: props.OnThreadNavigate,
						OnNavigatePath:   props.OnNavigatePath,
						OnCollapse:       collapseRail,
						OnNarrower:       resizeRail(-32),
						OnWider:          resizeRail(32),
					}),
					html.Div(html.Props{
						DataAttr: html.DataAttribute{Name: "component", Value: "route-frame"},
						Class:    routeFrameClass(layout),
					},
						ui.CreateElement(AppRouter, RouteShellProps{
							TaskActionsOpen:      props.TaskActionsOpen,
							OnTaskActionsOpen:    props.OnTaskActionsOpen,
							OnTaskActionsDismiss: props.OnTaskActionsDismiss,
							Snapshot:             props.Snapshot,
							Composer:             props.Composer,
							Timeline:             props.Timeline,
							TaskControls:         props.TaskControls,
							AuthoritativeGraph:   props.AuthoritativeGraph,
							RepositoryChoices:    props.RepositoryChoices,
							SelectedScope:        props.SelectedScope,
							GraphInspector:       props.GraphInspector,
							Memory:               props.Memory,
							Atoms:                props.Atoms,
							FileTree:             props.FileTree,
							Repositories:         props.Repositories,
							Appearance:           props.Appearance,
							LocalData:            props.LocalData,
							Telemetry:            props.Telemetry,
							Settings:             props.Settings,
							Collection:           props.Collection,
							Route:                props.Route,
							Tokens:               props.Tokens,
							Probe:                props.Probe,
							OnLayoutChange:       props.OnLayoutChange,
							OnGraphSelect:        props.OnGraphSelect,
							OnNavigatePath:       props.OnNavigatePath,
							OnPauseRequested:     props.OnPauseRequested,
							OnStopRequested:      props.OnStopRequested,
							Execution:            props.Execution,
						}),
					),
					ui.CreateElement(AssuranceRail, AssuranceRailProps{
						Snapshot:   props.Snapshot,
						Mode:       primitiveMode(props.Tokens),
						Graph:      observationGraph,
						Execution:  observationExecution,
						Collapsed:  inspectorHidden,
						OnCollapse: collapseInspector,
					}),
				),
			}}),
		ui.CreateElement(DialogHost, HostProps{}),
		ui.CreateElement(ToastHost, HostProps{}),
		ui.CreateElement(AccessibilityAnnouncer, AnnouncerProps{
			Message: candidate.Message,
		}),
	)
}

var railWidthStops = [...]int{224, 240, 272, 304, 336, 368, 400, 432, 464, 480}

func toggleLocalVisibility(current bool) bool { return !current }
func openLocalVisibility(bool) bool           { return true }
func closeLocalVisibility(bool) bool          { return false }

// nextRailWidth walks stable width stops instead of applying a delta that
// clamps asymmetrically around the 240px default. A narrower/wider pair is
// therefore reversible, while either boundary is an intentional no-op.
func nextRailWidth(width int, direction int) int {
	width = (state.LayoutPreferences{RailWidth: width}).Normalize().RailWidth
	if direction < 0 {
		for index := len(railWidthStops) - 1; index >= 0; index-- {
			if railWidthStops[index] < width {
				return railWidthStops[index]
			}
		}
		return railWidthStops[0]
	}
	if direction > 0 {
		for _, stop := range railWidthStops {
			if stop > width {
				return stop
			}
		}
		return railWidthStops[len(railWidthStops)-1]
	}
	return width
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
	Session              state.SessionView
	Workspace            state.WorkspaceView
	View                 state.TopBarView
	CostLabel            string
	Revision             uint64
	Mode                 primitives.Mode
	Viewport             state.ViewportClass
	Probe                RenderProbe
	OnThemeChange        func()
	OnReconnectRequested func()
	OnShortcutHelp       func()
	SearchOpen           bool
	SearchQuery          string
	RailOpen             bool
	OnSearchOpen         func()
	OnSearchDismiss      func()
	OnSearchQueryChange  func(string)
	OnRailToggle         func()
	OnInspectorToggle    func()
	OnNavigatePath       func(string)
	OnPauseRequested     func()
	OnStopRequested      func()
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
			field("provider", props.View.Provider, tokens),
			field("model", props.View.Model, tokens),
			field("effort", props.View.Effort, tokens),
			field("forecast-cost", props.View.ForecastCost, tokens),
			field("actual-tokens", props.View.ActualTokens, tokens),
			field("actual-cost", fallback(props.View.ActualCost, props.CostLabel), tokens),
			field("pricing-snapshot", props.View.PricingSnapshot, tokens),
			field("hard-budget", props.View.HardBudget, tokens),
			field("remaining-budget", props.View.RemainingBudget, tokens),
			field("budget-warning", props.View.BudgetWarning, tokens),
			primitives.Button(primitives.ButtonProps{
				Label: taskControlLabel(props.View.TaskState), AccessibleLabel: taskControlLabel(props.View.TaskState) + " task",
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
			"branch":           "Branch pending",
			"worktree":         "Worktree pending",
			"task-state":       "Task · Draft",
			"provider":         "Provider pending",
			"model":            "Model pending",
			"effort":           "Effort pending",
			"forecast-cost":    "Forecast pending",
			"actual-tokens":    "Usage pending",
			"actual-cost":      "Cost pending",
			"pricing-snapshot": "Pricing snapshot pending",
			"hard-budget":      "Budget pending",
			"remaining-budget": "Remaining budget pending",
			"budget-warning":   "Budget threshold pending",
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

func taskControlLabel(taskState string) string {
	if strings.EqualFold(strings.TrimSpace(taskState), "paused") {
		return "Resume"
	}
	return "Pause"
}

type RouteShellProps struct {
	Snapshot           state.Snapshot
	Composer           frontendcomposer.Props
	Timeline           TimelineControlProps
	TaskControls       *taskcontrols.Props
	AuthoritativeGraph *graphcanvas.AuthoritativeProps
	RepositoryChoices  *RepositoryChoiceSet
	SelectedScope      NavigationScope
	GraphInspector     ui.Node
	// Memory is the project-memory surface, supplied by the application
	// because reading memory is a coordinator call the shell must not make.
	Memory *MemoryWorkspaceProps
	// Atoms is the atom collection surface. It is a mounted node rather than a
	// prop tree because the surface owns its own search and selection: passing
	// that state through here would make every keystroke re-render the shell.
	Atoms ui.Node
	// FileTree is the repository drawn as its own directory tree, with one
	// file open. It is a mounted node for the same reason Atoms is.
	FileTree ui.Node
	// Repositories is the repositories surface, supplied for the same reason:
	// listing repositories and reading a working tree are coordinator calls.
	Repositories *RepositoryWorkspaceProps
	Telemetry    telemetryview.Props
	// Appearance is the settings page's theme, density, and motion panel,
	// supplied by the application that owns those choices.
	Appearance ui.Node
	// LocalData is the settings page's browser-storage panel.
	LocalData        ui.Node
	Settings         settingsview.Props
	Collection       codecollection.Props
	Route            routes.Route
	Tokens           design.Tokens
	Probe            RenderProbe
	OnLayoutChange   func(state.LayoutPreferences)
	OnGraphSelect    func(string)
	OnNavigatePath   func(string)
	OnPauseRequested func()
	OnStopRequested  func()
	// Execution is what the run is doing right now: the measurements that
	// decide whether to intervene, the steps it has taken, the lines it is
	// emitting, and what is in flight. It is optional because a task that has
	// not started has none of it, and showing an empty execution panel would
	// suggest a run that produced nothing rather than one that has not begun.
	Execution *ExecutionPanelProps
	// TaskActionsOpen and its two callbacks are the workspace's overflow menu,
	// owned by the application so no route surface has to hold a hook.
	TaskActionsOpen      bool
	OnTaskActionsOpen    func()
	OnTaskActionsDismiss func()
}

func RouteShell(props RouteShellProps) ui.Node {
	mode := primitiveMode(props.Tokens)
	switch props.Route.Name {
	case routes.RepositoryChooser:
		if props.Repositories != nil {
			repositories := *props.Repositories
			repositories.Tokens = props.Tokens
			repositories.OnNavigatePath = props.OnNavigatePath
			return ui.CreateElement(RepositoryWorkspaceShell, repositories)
		}
		chooser := RepositoryChooserProps{State: props.Snapshot.ThreadsState, Mode: mode}
		if props.RepositoryChoices != nil {
			chooser.State, chooser.Choices =
				props.RepositoryChoices.State, props.RepositoryChoices.Choices
		}
		return ui.CreateElement(RepositoryChooserShell, chooser)
	case routes.ThreadWorkspace:
		return ui.CreateElement(TaskWorkspaceShell, TaskWorkspaceProps{
			Snapshot: props.Snapshot, Tokens: props.Tokens, Probe: props.Probe,
			TaskActionsOpen:      props.TaskActionsOpen,
			OnTaskActionsOpen:    props.OnTaskActionsOpen,
			OnTaskActionsDismiss: props.OnTaskActionsDismiss,
			Composer:             props.Composer,
			Timeline:             props.Timeline,
			TaskControls:         props.TaskControls,
			AuthoritativeGraph:   props.AuthoritativeGraph,
			GraphInspector:       props.GraphInspector,
			OnLayoutChange:       props.OnLayoutChange,
			OnGraphSelect:        props.OnGraphSelect,
			OnNavigatePath:       props.OnNavigatePath,
			OnPauseRequested:     props.OnPauseRequested, OnStopRequested: props.OnStopRequested,
			Execution: props.Execution,
		})
	case routes.Graphs:
		return ui.CreateElement(GraphWorkspaceShell, GraphWorkspaceProps{
			Snapshot: props.Snapshot, Tokens: props.Tokens, Probe: props.Probe,
			AuthoritativeGraph: props.AuthoritativeGraph,
			GraphInspector:     props.GraphInspector,
			OnGraphSelect:      props.OnGraphSelect,
			OnNavigatePath:     props.OnNavigatePath,
		})
	case routes.Code:
		// The code route is the repository as its own tree: a person new to it
		// asks what is in here before they ask what a package offers, and the
		// tree answers that in the shape the repository already has. The
		// package and declaration listing remains available to any surface
		// that wants it.
		if props.FileTree != nil {
			return props.FileTree
		}
		collection := props.Collection
		collection.Mode = mode
		return ui.CreateElement(CodeCollectionShell, CodeCollectionProps{
			Title: "Code collection", Mode: mode, Collection: collection,
		})
	case routes.Atoms:
		if props.Atoms != nil {
			return props.Atoms
		}
		return ui.CreateElement(TopLevelState, TopLevelStateProps{
			Kind: "empty", Title: "Atoms",
			Body: "The atom collection is unavailable in this preview.",
		})
	case routes.Memory:
		if props.Memory != nil {
			memory := *props.Memory
			memory.Tokens = props.Tokens
			return ui.CreateElement(MemoryWorkspaceShell, memory)
		}
		return ui.CreateElement(MemoryShell, SimpleRouteProps{
			Title: "Memory", State: props.Snapshot.Memory.State, Mode: mode,
		})
	case routes.Settings:
		return ui.CreateElement(SettingsInteractiveShell, SettingsProps{
			SimpleRouteProps: SimpleRouteProps{Title: "Settings", State: props.Snapshot.Settings.State, Mode: mode},
			Telemetry:        props.Telemetry,
			Configuration:    props.Settings,
			Appearance:       props.Appearance,
			LocalData:        props.LocalData,
		})
	case routes.Diagnostics:
		return ui.CreateElement(DiagnosticsInteractiveShell, DiagnosticsProps{
			SimpleRouteProps: SimpleRouteProps{
				Title: "Diagnostics", State: props.Snapshot.Diagnostics.State, Mode: mode,
			},
			Diagnostics: props.Snapshot.Diagnostics,
		})
	case routes.FirstRun:
		return ui.CreateElement(FirstRunInteractiveShell, FirstRunProps{
			Title: "Welcome to CodeFlux", State: props.Snapshot.FirstRun.State, Mode: mode,
			OnNavigatePath: props.OnNavigatePath,
		})
	default:
		return ui.CreateElement(TopLevelState, TopLevelStateProps{
			Kind: "not-found", Title: "Page not found", Body: "Choose a repository to continue.",
		})
	}
}

// GraphWorkspaceProps configures the graph as a surface of its own.
type GraphWorkspaceProps struct {
	Snapshot           state.Snapshot
	AuthoritativeGraph *graphcanvas.AuthoritativeProps
	GraphInspector     ui.Node
	Tokens             design.Tokens
	Probe              RenderProbe
	OnGraphSelect      func(string)
	OnNavigatePath     func(string)
}

// GraphWorkspaceShell is the graph read on its own terms.
//
// The graph used to be this route's name and the task workspace's body: asking
// for the graph gave a person the transcript with a graph panel beside it, and
// the panel was the same size it is when nobody asked for it. Structure,
// execution and correctness are what this surface is for, so here the canvas
// takes the width and the node inspector stands beside it, while the thread
// workspace keeps the transcript as its subject.
func GraphWorkspaceShell(props GraphWorkspaceProps) ui.Node {
	mode := primitiveMode(props.Tokens)
	layout := props.Snapshot.Layout.Normalize()
	tokens := props.Tokens
	// The page counts the graph it is actually drawing. Counting the local
	// store instead reported "0 nodes" over a canvas holding a real revision.
	nodeCount := len(props.Snapshot.GraphNodes())
	summaryRevision := props.Snapshot.GraphRevision()
	if props.AuthoritativeGraph != nil {
		nodeCount = len(props.AuthoritativeGraph.Revision.Nodes())
		if ordinal := props.AuthoritativeGraph.Revision.Metadata().Ordinal(); ordinal > 0 {
			summaryRevision = ordinal
		}
	}
	summary := strconv.Itoa(nodeCount) + " nodes"
	if nodeCount == 1 {
		summary = "1 node"
	}
	summary += " · revision " + strconv.FormatUint(summaryRevision, 10)
	canvas := ui.CreateElement(GraphPane, GraphPaneProps{
		State: props.Snapshot.GraphState, Nodes: props.Snapshot.GraphNodes(),
		SelectedID: props.Snapshot.SelectedGraphID, Revision: props.Snapshot.GraphRevision(),
		Collapsed: false, Viewport: layout.Viewport, Mode: mode, Probe: props.Probe,
		Embedded: true, SuppressLegend: true, FullHeight: true, OnSelect: props.OnGraphSelect,
		Authoritative: props.AuthoritativeGraph,
	})
	body := []ui.Node{html.Div(html.Props{
		Class: css.New(
			css.MinWidth(css.Zero), css.MinHeight(css.Zero),
			css.H(css.Full), css.Overflow.Hidden,
		).String(),
	}, canvas)}
	if props.GraphInspector != nil {
		body = append(body, html.Aside(html.Props{
			Aria: map[string]string{"label": "Graph node inspector"},
			Data: map[string]string{"component": "graph-workspace-inspector"},
			Class: css.New(
				css.MinWidth(css.Zero), css.MinHeight(css.Zero),
				css.H(css.Full), css.OverflowY.Auto,
				css.Padding(css.RawLength("0 0 0 20px")),
				css.BorderLeft(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
			).String(),
		},
			html.H2(html.Props{
				Class: css.New(
					css.Margin(css.RawLength("0 0 10px")),
					css.Font(css.FontStack(tokens.Fonts.UI)),
					css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
					css.LineHeightLen(css.Px(tokens.Typography.Metadata.LineHeight)),
					css.FontWeight.Semibold,
					css.Tracking(css.Ems(0.09)),
					css.TextTransform.Uppercase,
					css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
				).String(),
				Text: "Selected node",
			}),
			props.GraphInspector,
		))
	}
	columns := []css.Track{css.MinMax(css.TrackLen(css.Zero), css.Fr(1))}
	if props.GraphInspector != nil && layout.Viewport == state.ViewportWide {
		columns = append(columns, css.TrackLen(css.Px(360)))
	}
	return html.Main(html.Props{
		ID: "main-content", TabIndex: -1,
		Data: map[string]string{
			"component": "graph-workspace-shell", "viewport": string(layout.Viewport),
			"focus-region": "conversation", "focus-order": "2",
		},
		Class: css.New(
			u.Grid,
			css.GridRows(css.TrackAuto, css.MinMax(css.TrackLen(css.Zero), css.Fr(1))),
			css.Gap(css.Px(tokens.Spacing.MD)),
			css.W(css.Full), css.H(css.Full),
			css.MinWidth(css.Zero), css.MinHeight(css.Zero),
			css.Padding(css.RawLength("12px 20px 20px")),
			css.Bg(css.Hex(string(tokens.Colors.Canvas))),
			css.Overflow.Hidden,
		).String(),
	},
		html.Header(html.Props{
			Aria:  map[string]string{"label": "Project graph"},
			Class: css.New(u.Flex, u.ItemsCenter, u.JustifyBetween, css.Gap(css.Px(tokens.Spacing.MD))).String(),
		},
			html.Div(html.Props{Class: css.New(u.Flex, u.FlexCol, css.Gap(css.Px(2))).String()},
				html.H1(html.Props{
					Class: css.New(
						css.Margin(css.Zero),
						css.Font(css.FontStack(tokens.Fonts.Display)),
						css.FontSize(css.Px(tokens.Typography.TaskTitle.Size)),
						css.LineHeightLen(css.Px(tokens.Typography.TaskTitle.LineHeight)),
						css.FontWeight.Normal,
						css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
					).String(),
					Text: "Project graph",
				}),
				html.P(html.Props{
					Class: css.New(
						css.Margin(css.Zero),
						css.Font(css.FontStack(tokens.Fonts.Code)),
						css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
						css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
					).String(),
					Text: summary,
				}),
			),
			html.Div(html.Props{
				Class: css.New(
					u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.SM)),
					css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
					css.Font(css.FontStack(tokens.Fonts.UI)),
					css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
					css.WhiteSpace.NoWrap, css.Overflow.Hidden, css.TextOverflowEllipsis(),
				).String(),
				Text: "▣ Work · ⬡ Tool · ◇ Plan · ● Proof · ◉ Memory",
			}),
		),
		html.Div(html.Props{
			Class: css.New(
				u.Grid, css.GridCols(columns...),
				css.Gap(css.Px(tokens.Spacing.LG)),
				css.MinWidth(css.Zero), css.MinHeight(css.Zero),
				css.H(css.Full), css.Overflow.Hidden,
			).String(),
		}, body...),
	)
}

type TaskWorkspaceProps struct {
	Snapshot           state.Snapshot
	Composer           frontendcomposer.Props
	Timeline           TimelineControlProps
	TaskControls       *taskcontrols.Props
	AuthoritativeGraph *graphcanvas.AuthoritativeProps
	RepositoryChoices  *RepositoryChoiceSet
	SelectedScope      NavigationScope
	GraphInspector     ui.Node
	Tokens             design.Tokens
	Probe              RenderProbe
	OnLayoutChange     func(state.LayoutPreferences)
	OnGraphSelect      func(string)
	OnNavigatePath     func(string)
	OnPauseRequested   func()
	OnStopRequested    func()
	// Execution is what the run is doing right now: the measurements that
	// decide whether to intervene, the steps it has taken, the lines it is
	// emitting, and what is in flight. It is optional because a task that has
	// not started has none of it, and an empty execution panel would suggest a
	// run that produced nothing rather than one that has not begun.
	Execution *ExecutionPanelProps
	// TaskActionsOpen and its two callbacks are the workspace's overflow menu,
	// owned by the application so no route surface has to hold a hook.
	TaskActionsOpen      bool
	OnTaskActionsOpen    func()
	OnTaskActionsDismiss func()
}

// ExecutionPanelProps binds the execution surfaces into the workspace.
type ExecutionPanelProps struct {
	Measurements      []executionview.Measurement
	Steps             []executionview.Step
	Lines             []executionview.LogLine
	Filter            executionview.Filter
	Streaming         bool
	Current           executionview.CurrentWork
	OnToggleSeverity  func(executionview.Severity)
	OnClearSeverities func()
}

// TaskWorkspaceShell composes the run's reading column.
//
// It deliberately owns no hooks. Route surfaces are mounted at one position in
// the tree, and GWC's hooks are positional: a surface that owned one state cell
// while its neighbours owned none rendered nothing at all when a person moved
// between them — a blank page after clicking a thread from settings, with no
// error anywhere, recoverable only by reloading. Its one piece of interaction
// state is owned by the caller and arrives as props.
func TaskWorkspaceShell(props TaskWorkspaceProps) ui.Node {
	layout := props.Snapshot.Layout.Normalize()
	mode := primitiveMode(props.Tokens)
	composerProps := composerPropsForConnection(props.Composer, props.Snapshot.Session.Connection)
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
	conversation := html.Div(html.Props{
		TabIndex: -1,
		Data:     map[string]string{"focus-region": "conversation", "focus-order": "2"},
		Class: css.New(
			css.MinWidth(css.Zero), css.W(css.Full), css.H(css.Full), css.Overflow.Auto,
		).String(),
	},
		ui.CreateElement(ConversationPane, ConversationPaneProps{
			State: props.Snapshot.ConversationState, Messages: props.Snapshot.Messages(),
			Revision: props.Snapshot.ConversationRevision(), Mode: mode, Probe: props.Probe,
			Composer: composerProps, Timeline: props.Timeline, OnGraphSelect: props.OnGraphSelect,
		}),
	)
	graph := ui.CreateElement(GraphPane, GraphPaneProps{
		State: props.Snapshot.GraphState, Nodes: props.Snapshot.GraphNodes(),
		SelectedID: props.Snapshot.SelectedGraphID, Revision: props.Snapshot.GraphRevision(),
		Collapsed: layout.GraphCollapsed, Viewport: layout.Viewport, Mode: mode, Probe: props.Probe,
		OnSelect:      props.OnGraphSelect,
		Authoritative: props.AuthoritativeGraph, Inspector: props.GraphInspector,
	})
	// At full width the graph is no longer a second pane competing with the
	// transcript for the middle of the screen. It moved to the observation rail
	// with the rest of what the run is doing, and the transcript — the thing a
	// person actually reads and answers — gets the room. Below full width the
	// rail is not drawn, so the two surfaces still trade places through tabs.
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
	if props.Execution != nil && layout.Viewport != state.ViewportWide {
		// Below full width there is no observation rail, so what the run is
		// doing sits under the transcript instead of beside it.
		conversation = html.Div(html.Props{
			TabIndex: -1,
			Data:     map[string]string{"focus-region": "conversation", "focus-order": "2"},
			Class: css.New(
				css.MinWidth(css.Zero), css.W(css.Full), css.H(css.Full), css.Overflow.Auto,
			).String(),
		},
			ui.CreateElement(ConversationPane, ConversationPaneProps{
				State: props.Snapshot.ConversationState, Messages: props.Snapshot.Messages(),
				Revision: props.Snapshot.ConversationRevision(), Mode: mode, Probe: props.Probe,
				Composer: composerProps, Timeline: props.Timeline,
				OnGraphSelect: props.OnGraphSelect,
			}),
			executionPanels(*props.Execution, mode),
		)
	}
	workspace := responsiveWorkspace(
		layout, rail, conversation, graph, split, mode, props.OnLayoutChange,
	)
	if layout.Viewport == state.ViewportWide {
		workspace = html.Div(html.Props{
			Data:  map[string]string{"component": "reading-column"},
			Class: readingColumnClass(props.Tokens),
		},
			// The rail's landmark stays in the tree at every width: the shell
			// promises a focus order and it has to hold whether or not this
			// width draws a split.
			html.Div(html.Props{Hidden: true}, rail),
			conversation,
		)
	}
	headerChildren := []ui.Node{ui.CreateElement(TaskWorkspaceHeader, TaskWorkspaceHeaderProps{
		Snapshot: props.Snapshot, Mode: mode,
		TaskActionsOpen:      props.TaskActionsOpen,
		OnTaskActionsOpen:    props.OnTaskActionsOpen,
		OnTaskActionsDismiss: props.OnTaskActionsDismiss,
		OnNavigatePath:       props.OnNavigatePath,
		OnPauseRequested:     props.OnPauseRequested,
		OnStopRequested:      props.OnStopRequested,
		OnReviewRequested:    props.Timeline.OnOpenReview,
	})}
	if props.TaskControls != nil {
		controlProps := *props.TaskControls
		controlProps.Mode = mode
		headerChildren = append(headerChildren, ui.CreateElement(taskcontrols.TaskControlDisclosure, controlProps))
	}
	// The surface is a main landmark, like every other route surface.
	//
	// It used to be a div while settings, memory and the graph were mains, and
	// swapping a div in where a main had been at the same position left the
	// route frame empty: the workspace rendered, and nothing arrived in the
	// document. Clicking a thread from any other page produced a blank page.
	return html.Main(html.Props{
		ID: "main-content", TabIndex: -1,
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
			css.PaddingY(css.Px(props.Tokens.Spacing.SM)),
			css.PaddingX(css.Px(props.Tokens.Spacing.MD)),
			css.Bg(css.Hex(string(props.Tokens.Colors.Canvas))),
		).String(),
	},
		// The run's identity, its controls and its measurements sit at the head
		// of the same column the transcript reads down. A header that spans a
		// width the content does not is a header that belongs to nothing.
		html.Div(html.Props{
			Data:  map[string]string{"component": "task-observability-region"},
			Class: observabilityRegionClass(props.Tokens, layout.Viewport == state.ViewportWide),
		}, headerChildren...),
		workspace,
	)
}

func composerPropsForConnection(
	composerProps frontendcomposer.Props,
	connection state.ConnectionState,
) frontendcomposer.Props {
	if connection == state.ConnectionLive {
		return composerProps
	}
	composerProps.MutationDisabled = true
	if composerProps.MutationDisabledReason == "" {
		switch connection {
		case state.ConnectionIdle:
			composerProps.MutationDisabledReason = "Open a thread before sending a message"
		case state.ConnectionConnecting:
			composerProps.MutationDisabledReason = "Opening the live session; your draft is kept"
		case state.ConnectionDisconnected:
			composerProps.MutationDisabledReason = "Local Disconnected: reconnect to send this draft"
		case state.ConnectionUnauthorized:
			composerProps.MutationDisabledReason = "Local Unauthorized: authenticate again to send this draft"
		case state.ConnectionIncompatible:
			composerProps.MutationDisabledReason = "Local Incompatible: update the client or coordinator to send"
		case state.ConnectionReplaying:
			composerProps.MutationDisabledReason = "Replaying durable updates before mutations resume"
		case state.ConnectionDegraded:
			composerProps.MutationDisabledReason = "Connection certainty is degraded; this draft is preserved"
		default:
			composerProps.MutationDisabledReason = "Live session connection is unavailable"
		}
	}
	// Keep OnTextChange intact: drafts remain browser-local and editable while
	// durable mutation callbacks are severed at the shell boundary.
	composerProps.OnSubmitRequested = nil
	composerProps.OnRetryRequested = nil
	return composerProps
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

// observabilityRegionClass keeps the run header on the reading column's
// measure at full width and lets it span everything below it.
func observabilityRegionClass(tokens design.Tokens, wide bool) string {
	rules := []css.Rule{
		u.Flex, u.FlexCol,
		css.Gap(css.Px(tokens.Spacing.SM)),
		css.W(css.Full), css.MinWidth(css.Zero),
	}
	if wide {
		rules = append(rules,
			css.MaxWidth(css.Px(920)),
			css.Margin(css.RawLength("0 auto")),
		)
	}
	return css.New(rules...).String()
}

// readingColumnClass measures the transcript.
//
// Prose set the full width of a 1600-pixel display is prose nobody finishes a
// line of. The column is capped near the width a serif stays readable at and
// centred in whatever room is left, so the console reads like a document and
// the machine's surfaces stay at the edges where they belong.
func readingColumnClass(tokens design.Tokens) string {
	return css.New(
		u.Flex, u.FlexCol,
		css.W(css.Full), css.H(css.Full),
		css.MaxWidth(css.Px(920)),
		css.Margin(css.RawLength("0 auto")),
		css.MinWidth(css.Zero), css.MinHeight(css.Zero),
		css.Overflow.Hidden,
	).String()
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
	Snapshot             state.Snapshot
	Mode                 primitives.Mode
	TaskActionsOpen      bool
	OnTaskActionsOpen    func()
	OnTaskActionsDismiss func()
	OnNavigatePath       func(string)
	OnPauseRequested     func()
	OnStopRequested      func()
	OnReviewRequested    func()
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
					css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
					css.Font(css.FontStack(tokens.Fonts.Display)),
					css.FontSize(css.Px(tokens.Typography.WorkspaceTitle.Size)),
					css.LineHeightLen(css.Px(tokens.Typography.WorkspaceTitle.LineHeight)),
					css.FontWeight.Normal,
					css.Tracking(css.Ems(0.004)),
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
	OnSelect   func(string)
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
			threadID := thread.ID
			buttonProps := html.PropsOf(html.OnClick(func() {
				if props.OnSelect != nil {
					props.OnSelect(threadID)
				}
			}))
			buttonProps.Type = "button"
			buttonProps.Aria = aria
			buttonProps.Disabled = props.OnSelect == nil
			buttonProps.Class = threadLinkClass(tokens)
			buttonProps.Text = thread.Title
			items = append(items, html.Li(html.Props{
				Data: map[string]string{"thread-id": thread.ID, "status": thread.Status},
				Class: css.New(
					css.MarginY(css.Px(tokens.Spacing.XS)),
				).String(),
			}, html.Button(buttonProps)))
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
						css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
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
	State         state.DataState
	Messages      []state.MessageView
	Revision      uint64
	Mode          primitives.Mode
	Probe         RenderProbe
	Composer      frontendcomposer.Props
	Timeline      TimelineControlProps
	OnGraphSelect func(string)
}

func ConversationPane(props ConversationPaneProps) ui.Node {
	recordRender(props.Probe, "conversation-pane", props.Revision)
	focus := ui.UseFocusManager()
	selectedStableKey := strings.TrimSpace(props.Timeline.SelectedStableKey)
	ui.UseEffectOf(func() func() {
		if selectedStableKey != "" {
			focus.FocusByID(timelineview.CardFocusTargetID(selectedStableKey))
		}
		return nil
	}, selectedStableKey)
	tokens := props.Mode.Tokens()
	timelineProps := props.Timeline
	timelineProps.Mode = props.Mode
	// The count names what is on screen. It used to count the store's message
	// list, which the authoritative timeline does not fill, so a transcript
	// showing six entries reported "0 messages" directly above them.
	entryCount := len(props.Messages)
	content := asyncStateContent(props.State, "messages", len(props.Messages), props.Mode)
	if props.State == state.DataReady || props.State == state.DataPartialStale {
		cards := []timelinecard.Card(nil)
		if !props.Timeline.Authoritative {
			cards = timelineCardsForMessages(props.Messages)
		}
		cards = append(cards, props.Timeline.Cards...)
		actions := props.Timeline.Actions
		if actions.OnCopy == nil {
			actions.OnCopy = copyTimelineText
		}
		if actions.OnSelectNode == nil {
			actions.OnSelectNode = props.OnGraphSelect
		}
		items := make([]ui.Node, 0, len(cards))
		for index, card := range cards {
			items = append(items, transcriptSpineRow(
				ui.CreateElement(timelineview.Renderer, timelineview.Props{
					Card: card, Mode: props.Mode, Actions: actions,
					Selected:            selectedStableKey != "" && card.StableKey == selectedStableKey,
					PipelineSkipSummary: props.Timeline.PipelineSkipSummary,
				}),
				card,
				index == len(cards)-1 && cardStillArriving(card),
				tokens,
			))
		}
		entryCount = len(cards)
		timelineChildren := make([]ui.Node, 0, 3)
		if strings.TrimSpace(props.Timeline.SelectionNotice) != "" {
			timelineChildren = append(timelineChildren, html.Div(html.Props{
				Role: "status", Aria: map[string]string{"live": "polite"},
				Data: map[string]string{"component": "timeline-selection-notice"},
				Text: props.Timeline.SelectionNotice,
			}))
		}
		if !props.Timeline.HasOlder {
			timelineChildren = append(timelineChildren, html.Div(html.Props{
				Role: "status", Aria: map[string]string{"live": "off"},
				Data: map[string]string{"component": "beginning-of-thread"},
				Class: css.New(
					css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
					css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				).String(),
				Text: "Beginning of thread",
			}))
		}
		// One continuous spine runs behind the whole stack. Drawing it per
		// entry left a dashed line wherever two entries were spaced apart,
		// which read as a broken sequence rather than a continuous one.
		stack := make([]ui.Node, 0, len(items)+1)
		if len(items) > 0 {
			stack = append(stack, html.Span(html.Props{
				Aria: map[string]string{"hidden": "true"},
				Data: map[string]string{"component": "transcript-spine"},
				Class: css.New(
					u.Absolute,
					css.Left(css.Px(5)), css.Top(css.Px(10)), css.Bottom(css.Px(10)),
					css.W(css.Px(1)),
					css.Bg(css.Hex(string(tokens.Colors.BorderStrong))),
					css.OpacityNum(css.Num(0.6)),
				).String(),
			}))
		}
		stack = append(stack, items...)
		timelineChildren = append(timelineChildren, html.Div(html.Props{
			Data:  map[string]string{"component": "timeline-card-stack", "gap": "8px"},
			Class: timelineCardStackClass(tokens),
		}, stack...))
		content = html.Div(html.Props{
			Data: map[string]string{
				"component":       "conversation-timeline",
				"durable-order":   "sequence",
				"auto-follow":     "near-bottom-only",
				"routine-live":    "polite",
				"assertive-toast": "false",
			},
			Class: css.New(
				u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.MD)),
				css.PaddingY(css.Px(tokens.Spacing.LG)),
				css.PaddingX(css.Px(tokens.Spacing.XS)),
			).String(),
		}, timelineChildren...)
	}
	return html.Section(html.Props{
		Aria: map[string]string{"label": "Conversation"},
		Data: map[string]string{
			"component": "conversation-pane", "state": string(props.State),
			"revision": strconv.FormatUint(props.Revision, 10),
		},
		Class: transcriptPanelClass(props.Mode),
	},
		html.Div(html.Props{
			Class: css.New(
				u.Flex, u.ItemsCenter, u.JustifyBetween,
				css.Gap(css.Px(tokens.Spacing.MD)),
				css.Padding(css.RawLength("2px 4px 12px")),
				css.BorderBottom(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
			).String(),
		},
			html.H2(html.Props{
				Class: css.New(
					css.Margin(css.Zero),
					css.Font(css.FontStack(tokens.Fonts.UI)),
					css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
					css.LineHeightLen(css.Px(tokens.Typography.Metadata.LineHeight)),
					css.FontWeight.Semibold,
					css.Tracking(css.Ems(0.09)),
					css.TextTransform.Uppercase,
					css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
				).String(),
				Text: "Transcript",
			}),
			html.Span(html.Props{
				Data: map[string]string{"component": "transcript-count"},
				Class: css.New(
					css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
					css.Font(css.FontStack(tokens.Fonts.Code)),
					css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				).String(),
				Text: transcriptCountLabel(entryCount),
			}),
		),
		html.Div(html.Props{
			Class: css.New(
				css.FlexGrow(css.Num(1)), css.MinHeight(css.Zero), css.OverflowY.Auto,
				css.PaddingX(css.Px(tokens.Spacing.XS)),
			).String(),
		},
			ui.CreateElement(TimelineControls, timelineProps),
			content,
		),
		// The composer is docked to the record rather than floated above it in
		// its own raised card. A panel inside a panel inside a panel was the
		// shape the whole console had, and it left nothing looking foreground.
		html.Div(html.Props{
			Data: map[string]string{"focus-region": "composer", "focus-order": "3"},
			Class: css.New(
				css.Padding(css.RawLength("12px 0 0")),
				css.BorderTop(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
			).String(),
		}, ui.CreateElement(frontendcomposer.Composer, composerPropsForConversation(props))),
	)
}

// transcriptCountLabel names how many durable entries are on screen.
func transcriptCountLabel(count int) string {
	if count == 1 {
		return "1 entry"
	}
	return fmt.Sprintf("%d entries", count)
}

// cardStillArriving reports whether a card is the head of a stream that has not
// finished, which is the only condition under which the spine pulses.
func cardStillArriving(card timelinecard.Card) bool {
	return card.Message != nil && card.Message.Status == timelinecard.MessageProvisional
}

// transcriptSpineRow hangs one entry off the sequence spine.
//
// The spine is the signature of this console and it encodes what the product
// is: a durable, ordered event sequence that can be replayed. Each entry gets a
// node on a continuous line, coloured by what kind of thing it is, so the shape
// of a run — planning, then tools, then validation, then a decision — is
// readable before a single card is. The head of an unfinished stream is the one
// element in the interface allowed to move.
func transcriptSpineRow(card ui.Node, model timelinecard.Card, head bool, tokens design.Tokens) ui.Node {
	tone := transcriptSpineTone(model, tokens)
	nodeRules := []css.Rule{
		css.W(css.Px(9)), css.H(css.Px(9)),
		css.MarginY(css.Px(6)),
		css.Rounded(css.Px(tokens.Geometry.PillRadius)),
		css.Bg(css.Hex(string(tone))),
		css.Border(css.Px(2), css.Hex(string(tokens.Colors.Surface1))),
		css.FlexShrink(css.Num(0)),
		css.ZIndex(1),
	}
	if head {
		nodeRules = append(nodeRules,
			css.Shadow(css.ShadowOf(
				css.Zero, css.Zero, css.Px(10), css.Zero, css.Hex(string(tone)),
			)),
		)
		if !tokens.ReducedMotion {
			nodeRules = append(nodeRules,
				css.Keyframes("codeflux-spine-head",
					css.At("0%", css.OpacityNum(css.Num(1))),
					css.At("50%", css.OpacityNum(css.Num(0.35))),
					css.At("100%", css.OpacityNum(css.Num(1))),
				),
				css.Animation(css.Ms(1800), css.EaseInOut),
			)
		}
	}
	return html.Div(html.Props{
		Data: map[string]string{
			"component": "transcript-entry",
			"kind":      string(model.Kind),
			"head":      boolAttribute(head),
		},
		Class: css.New(
			u.Flex, css.Gap(css.Px(tokens.Spacing.MD)),
			css.MinWidth(css.Zero),
		).String(),
	},
		html.Div(html.Props{
			Aria: map[string]string{"hidden": "true"},
			Class: css.New(
				u.Flex, u.FlexCol, u.ItemsCenter,
				css.W(css.Px(11)),
				css.FlexShrink(css.Num(0)),
			).String(),
		},
			html.Span(html.Props{Class: css.New(nodeRules...).String()}),
		),
		html.Div(html.Props{
			Class: css.New(css.MinWidth(css.Zero), css.FlexGrow(css.Num(1))).String(),
		}, card),
	)
}

// transcriptSpineTone colours a spine node by what the entry is.
//
// It reuses the kind accents the graph legend already uses, so the same content
// category is the same colour wherever a person meets it.
func transcriptSpineTone(card timelinecard.Card, tokens design.Tokens) design.Color {
	switch card.Kind {
	case timelinecard.KindMessage:
		if card.Message != nil && card.Message.Role == "user" {
			return tokens.Colors.Accent
		}
		return tokens.Colors.TextMuted
	case timelinecard.KindPlan, timelinecard.KindPlanRevision, timelinecard.KindRequirement:
		return tokens.Colors.Plan
	case timelinecard.KindTool, timelinecard.KindContext:
		return tokens.Colors.Execution
	case timelinecard.KindValidation, timelinecard.KindDiff:
		return tokens.Colors.Validation
	case timelinecard.KindApproval, timelinecard.KindCompletion:
		return tokens.Colors.Success
	case timelinecard.KindError, timelinecard.KindRecovery:
		return tokens.Colors.Failure
	case timelinecard.KindForecast, timelinecard.KindCostBudget, timelinecard.KindUsage:
		return tokens.Colors.Forecast
	case timelinecard.KindCheckpoint, timelinecard.KindGraphChange:
		return tokens.Colors.Memory
	default:
		return tokens.Colors.Pending
	}
}

func timelineCardStackClass(tokens design.Tokens) string {
	return css.New(
		u.Relative,
		u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.MD)), css.MinWidth(css.Zero),
	).String()
}

func composerPropsForConversation(props ConversationPaneProps) frontendcomposer.Props {
	composerProps := props.Composer
	composerProps.Mode = props.Mode
	if props.State != state.DataReady && props.State != state.DataReadyEmpty {
		composerProps.MutationDisabled = true
		if composerProps.MutationDisabledReason == "" {
			composerProps.MutationDisabledReason = "Conversation state is not current"
		}
	}
	return composerProps
}

type GraphPaneProps struct {
	State         state.DataState
	Nodes         []state.GraphNodeView
	SelectedID    string
	Revision      uint64
	Collapsed     bool
	Viewport      state.ViewportClass
	Mode          primitives.Mode
	Probe         RenderProbe
	OnSelect      func(string)
	Authoritative *graphcanvas.AuthoritativeProps
	Inspector     ui.Node
	// Embedded marks a graph drawn inside a surface that already names it, so
	// it drops its own title. SuppressLegend does the same for the legend,
	// which the dedicated graph workspace carries in its header.
	Embedded       bool
	SuppressLegend bool
	// FullHeight asks for the canvas the graph's own page can afford, rather
	// than the boxed height a companion panel gets.
	FullHeight bool
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
				css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
				css.Margin(css.Zero),
				css.FontSize(css.Px(tokens.Typography.SectionTitle.Size)),
			).String(),
			Text: "Task graph",
		}),
		content,
	)
}

// transcriptPanelClass draws the record itself rather than a card holding it.
//
// The transcript is the page: it has the reading column to itself, so a border,
// a corner and a drop shadow around it only announce that some other, more
// important surface must be outside. Nothing is outside.
func transcriptPanelClass(mode primitives.Mode) string {
	tokens := mode.Tokens()
	return css.New(
		u.Flex, u.FlexCol,
		css.Gap(css.Px(tokens.Spacing.MD)),
		css.Bg(css.Transparent),
		css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
		css.Padding(css.RawLength("0 8px")),
		css.H(css.Full),
		css.MinWidth(css.Zero), css.MaxWidth(css.Full), css.Overflow.Hidden,
	).String()
}

func panelClass(mode primitives.Mode) string {
	tokens := mode.Tokens()
	return css.New(
		u.Flex, u.FlexCol,
		css.Gap(css.Px(tokens.Spacing.MD)),
		css.Bg(css.Hex(string(tokens.Colors.Surface1))),
		css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
		css.Border(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
		css.Padding(css.Px(tokens.Spacing.MD)),
		css.Rounded(css.Px(tokens.Geometry.DialogRadius)),
		css.Shadow(css.ShadowOf(
			css.Zero, css.Px(14), css.Px(40), css.Px(-30), css.RGBA(0, 0, 0, 0.62),
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
		title, body := emptyInvitation(subject)
		return primitives.EmptyState(primitives.EmptyStateProps{
			Title: title, Body: body, Mode: mode,
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

// emptyInvitation says what will fill a surface rather than restating that it
// is empty. "No task graph nodes. There is nothing to show yet." named the
// absence twice and told nobody what would end it.
func emptyInvitation(subject string) (string, string) {
	switch subject {
	case "task graph nodes":
		return "Nothing mapped yet",
			"The graph draws itself as the run plans, edits, and checks its work."
	case "messages":
		return "Nothing here yet",
			"Describe a change below and the transcript records every step of it."
	case "memory":
		return "No memory yet",
			"What the agent learns about this repository is kept here."
	case "repositories":
		return "No repository open",
			"Open a local repository to give the agent somewhere to work."
	default:
		return "No " + subject + " yet", "This fills in as the run produces " + subject + "."
	}
}

// routeIsAboutARun reports whether the observation rail belongs on a route.
//
// The rail reports the current run: its spend, its working tree, its timeline.
// On the graph, memory, repositories, settings, and diagnostics pages it took
// three hundred and eighty pixels from the subject to report "No task yet" and
// four unknowns, and the graph fitted itself into what was left at a third of
// its size. Only the routes whose subject is a run keep it.
func routeIsAboutARun(name routes.Name) bool {
	switch name {
	case routes.ThreadWorkspace:
		return true
	default:
		return false
	}
}

func primitiveMode(tokens design.Tokens) primitives.Mode {
	return primitives.Mode{
		Theme: tokens.Theme, Density: tokens.Density,
		HighContrast:  tokens.Theme == design.ThemeHighContrast,
		ReducedMotion: tokens.ReducedMotion,
	}
}

// RepositoryChoice is one repository a person can enter.
//
// The chooser used to draw a hardcoded count of zero, so somebody with a
// repository already open was told they had none, beside a browse control that
// could not create one either. A choice carries the path it leads to, because
// a row that cannot be entered is not a choice.
type RepositoryChoice struct {
	Name     string
	Detail   string
	Revision string
	Path     string
}

// RepositoryChoiceSet is the coordinator's answer about what can be opened.
//
// It carries its own load state rather than borrowing the thread list's,
// because the two ask different questions of the coordinator and can disagree:
// threads can be loading while repositories are already known, and reusing one
// state for both is how a populated list came to be drawn as empty.
type RepositoryChoiceSet struct {
	State   state.DataState
	Choices []RepositoryChoice
}

type RepositoryChooserProps struct {
	State   state.DataState
	Choices []RepositoryChoice
	Mode    primitives.Mode
}

func RepositoryChooserShell(props RepositoryChooserProps) ui.Node {
	return routeMain("repository-chooser-shell", "Choose a repository", props.Mode,
		routeRegion(props.Mode, "recent-workspaces", "Recent valid workspaces",
			repositoryChoiceList(props)),
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

// repositoryChoiceList draws the repositories a person can enter.
//
// A choice with no path is not drawn as a link, because a link that goes
// nowhere is worse than a line of text: it invites a click and answers with
// silence.
func repositoryChoiceList(props RepositoryChooserProps) ui.Node {
	if props.State != state.DataReady || len(props.Choices) == 0 {
		count := len(props.Choices)
		reported := props.State
		// A ready answer holding nothing is empty, not ready. Reporting it as
		// ready is what produced the line "0 recent workspaces" where an
		// invitation to open one belonged.
		if reported == state.DataReady && count == 0 {
			reported = state.DataReadyEmpty
		}
		return asyncStateContent(reported, "recent workspaces", count, props.Mode)
	}
	tokens := props.Mode.Tokens()
	rowClass := css.New(
		u.Flex, u.ItemsCenter, u.JustifyBetween, css.Gap(css.Px(tokens.Spacing.MD)),
		css.PaddingY(css.Px(tokens.Spacing.SM)),
		css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
		css.TextDecoration.None,
		css.BorderBottom(
			css.Px(tokens.Geometry.BorderWidth),
			css.Hex(string(tokens.Colors.BorderSubtle)),
		),
	).String()
	nameClass := css.New(
		css.Font(css.FontStack(tokens.Fonts.Reading)),
		css.FontSize(css.Px(tokens.Typography.PanelHeading.Size)),
		css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
	).String()
	// The detail is metadata about the row, not the row. Given the readout
	// treatment it came out in mono at heading weight and outweighed the
	// repository name it was describing, so the eye landed on "Open thread"
	// rather than on which repository that was.
	detailClass := css.New(
		css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
		css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
	).String()

	rows := make([]ui.Node, 0, len(props.Choices))
	for _, choice := range props.Choices {
		detail := choice.Detail
		if choice.Revision != "" {
			detail = detail + " · " + choice.Revision
		}
		label := []ui.Node{
			html.Span(html.Props{Class: nameClass, Text: choice.Name}),
			html.Span(html.Props{Class: detailClass, Text: detail}),
		}
		if choice.Path == "" {
			rows = append(rows, html.Div(html.Props{Class: rowClass}, label...))
			continue
		}
		rows = append(rows, html.A(html.Props{
			Href: choice.Path, Class: rowClass,
			DataAttr: html.DataAttribute{Name: "repository-choice", Value: choice.Name},
		}, label...))
	}
	return html.Div(html.Props{
		Class: css.New(u.Flex, u.FlexCol, css.MinWidth(css.Zero)).String(),
	}, rows...)
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
	return SettingsInteractiveShell(SettingsProps{SimpleRouteProps: props})
}

type SettingsProps struct {
	SimpleRouteProps
	Telemetry telemetryview.Props
	// Appearance is the theme, density, and motion control panel. It is
	// supplied by the application because those choices live in application
	// state, not in the shell that renders them.
	Appearance ui.Node
	// LocalData is the browser-storage panel, supplied for the same reason:
	// only the application can reach this browser's stored interface state.
	LocalData ui.Node
	// Configuration is the server-backed settings surface, supplied by the
	// application because reading the policy, the providers, and the models is
	// a coordinator call the shell must not make.
	Configuration settingsview.Props
}

// SettingsInteractiveShell is the settings specification sheet.
//
// It was seven panels in a two-column grid, five thousand pixels tall, whose
// right column stood empty for two thousand of them because grid rows yoke
// their heights. Settings are the one surface where somebody compares many
// values at once, and panels prevent exactly that by breaking the vertical
// axis the values would be compared along. The sheet is one column of rows on
// a shared value axis instead, and it owns the route frame so the commit bar
// can stay with the reader rather than sitting below everything.
func SettingsInteractiveShell(props SettingsProps) ui.Node {
	tokens := props.Mode.Tokens()
	telemetry := props.Telemetry
	telemetry.Mode = props.Mode
	configuration := props.Configuration
	configuration.Mode = props.Mode
	configuration.Appearance = settingsAppearance(props)
	configuration.LocalData = settingsLocalData(props)
	configuration.Telemetry = ui.CreateElement(telemetryview.Component, telemetry)
	return html.Main(html.Props{
		ID: "main-content", TabIndex: -1,
		Data: map[string]string{
			"component": "settings-shell", "focus-region": "conversation",
			"focus-order": "2", "state": string(props.State),
		},
		Class: css.New(
			u.Grid,
			css.GridRows(css.MinMax(css.TrackLen(css.Zero), css.Fr(1)), css.TrackAuto),
			css.W(css.Full), css.H(css.Full),
			css.MinWidth(css.Zero), css.MinHeight(css.Zero),
			css.Bg(css.Hex(string(tokens.Colors.Canvas))),
			css.Overflow.Hidden,
		).String(),
	},
		html.Div(html.Props{
			Data: map[string]string{"scroll-owner": "route"},
			Class: css.New(
				css.W(css.Full), css.MinWidth(css.Zero), css.MinHeight(css.Zero),
				css.OverflowY.Auto, css.OverflowX.Hidden,
				css.PaddingX(css.Px(tokens.Spacing.XXL)),
				css.PaddingY(css.Px(tokens.Spacing.XL)),
			).String(),
		}, settingsview.Sheet(configuration)),
		settingsview.CommitBar(configuration),
	)
}

// settingsAppearance draws the appearance controls when the application
// supplied them, and says plainly that they are unavailable when it did not.
// The section used to describe controls that were never there.
func settingsAppearance(props SettingsProps) ui.Node {
	if props.Appearance != nil {
		return props.Appearance
	}
	return routeStateContent(props.SimpleRouteProps, "appearance preferences",
		html.P(html.Props{Text: "Appearance controls are unavailable in this preview."}))
}

// settingsLocalData draws the browser-storage controls when the application
// supplied them. The section previously named three capabilities -- backup,
// retention, and local data controls -- and offered none of them.
func settingsLocalData(props SettingsProps) ui.Node {
	if props.LocalData != nil {
		return props.LocalData
	}
	return routeStateContent(props.SimpleRouteProps, "data controls",
		html.P(html.Props{Text: "Local data controls are unavailable in this preview."}))
}

func DiagnosticsShell(props SimpleRouteProps) ui.Node {
	return DiagnosticsInteractiveShell(DiagnosticsProps{SimpleRouteProps: props})
}

type DiagnosticsProps struct {
	SimpleRouteProps
	Diagnostics state.DiagnosticsView
}

func DiagnosticsInteractiveShell(props DiagnosticsProps) ui.Node {
	return routeScrollOwner("diagnostics-scroll-owner", routeMain("diagnostics-shell", props.Title, props.Mode,
		routeRegion(props.Mode, "health", "Health",
			routeStateContent(props.SimpleRouteProps, "health", html.P(html.Props{Text: "Coordinator and database health."}))),
		routeRegion(props.Mode, "durable-session-sequence", "Durable session sequence",
			routeStateContent(props.SimpleRouteProps, "durable session sequence", DiagnosticsSequenceView(props.Diagnostics))),
		routeRegion(props.Mode, "versions", "Versions",
			routeStateContent(props.SimpleRouteProps, "versions", html.P(html.Props{Text: "Application, API, schema, and frontend versions."}))),
		routeRegion(props.Mode, "tasks", "Tasks",
			routeStateContent(props.SimpleRouteProps, "tasks", html.P(html.Props{Text: "Active Task and Attempt summaries."}))),
		routeRegion(props.Mode, "logs", "Logs",
			routeStateContent(props.SimpleRouteProps, "logs", html.P(html.Props{Text: "Redacted local diagnostic logs."}))),
		routeRegion(props.Mode, "backup", "Backup",
			routeStateContent(props.SimpleRouteProps, "backup status", html.P(html.Props{Text: "Local backup status and recovery guidance."}))),
		routeRegion(props.Mode, "export", "Export",
			routeStateContent(props.SimpleRouteProps, "exports", html.P(html.Props{Text: "Create a redacted support export."}))),
		routeRegion(props.Mode, "terminology", "Terminology",
			routeStateContent(props.SimpleRouteProps, "terminology",
				html.P(html.Props{Text: "A Thread contains conversation. A Task is durable work. An Attempt is one execution. A Plan revision changes the approach. An Approval authorizes a gated action. A Checkpoint is restorable state. Recovery resumes safely."}),
			),
		),
	))
}

// DiagnosticsSequenceView renders the content-free mounted session cursor used
// by the diagnostics route and deterministic browser evidence.
func DiagnosticsSequenceView(view state.DiagnosticsView) ui.Node {
	sequence := "Unknown"
	detail := "No mounted session projection is available."
	if view.LastAppliedSequenceKnown {
		sequence = strconv.FormatUint(view.LastAppliedSequence, 10)
		detail = "This is the last durable session event applied successfully by the browser."
		if view.LastAppliedSequence == 0 {
			detail = "The authoritative snapshot cursor is zero; no later durable session event has been applied."
		}
	}
	stream := "No replay, live delivery, or gap repair is active."
	switch {
	case view.SessionGapRepairRequired:
		stream = "Gap repair required; the displayed sequence remains the last successfully applied cursor."
	case view.SessionLive:
		stream = "Live delivery is active."
	case view.SessionReplayActive:
		stream = "Replay is in progress."
	}
	return html.Section(html.Props{
		Aria: map[string]string{"label": "Durable session sequence status"},
		Data: map[string]string{
			"component":           "durable-session-sequence",
			"sequence":            sequence,
			"sequence-known":      strconv.FormatBool(view.LastAppliedSequenceKnown),
			"replay-active":       strconv.FormatBool(view.SessionReplayActive),
			"live":                strconv.FormatBool(view.SessionLive),
			"gap-repair-required": strconv.FormatBool(view.SessionGapRepairRequired),
		},
	},
		html.Tag("dl", html.Props{},
			html.Tag("dt", html.Props{Text: "Last successfully applied sequence"}),
			html.Tag("dd", html.Props{Text: sequence}),
			html.Tag("dt", html.Props{Text: "Meaning"}),
			html.Tag("dd", html.Props{Text: detail}),
			html.Tag("dt", html.Props{Text: "Session delivery"}),
			html.Tag("dd", html.Props{Text: stream}),
		),
	)
}

func FirstRunShell(props SimpleRouteProps) ui.Node {
	return firstRunLayout(FirstRunProps{
		Title: props.Title, State: props.State, Mode: props.Mode,
	})
}

type FirstRunProps struct {
	Title          string
	State          state.DataState
	Mode           primitives.Mode
	OnNavigatePath func(string)
}

func FirstRunInteractiveShell(props FirstRunProps) ui.Node {
	return firstRunLayout(props)
}

func firstRunLayout(props FirstRunProps) ui.Node {
	stateProps := SimpleRouteProps{Title: props.Title, State: props.State, Mode: props.Mode}
	// Each step is one card carrying its own position, name, state, meaning,
	// and action. The previous arrangement wrapped a generic region around a
	// separate content block, which read as a tall empty box with a heading.
	cards := []firstRunCard{
		{
			Step: 1, Icon: primitives.IconDatabase, Title: "Local boundary",
			Status: "Ready", Tone: design.StatusSuccess,
			Body: "Your coordinator, durable state, and task evidence stay on this machine.",
		},
		{
			Step: 2, Icon: primitives.IconModel, Title: "Model provider",
			Status: "Needs connection", Tone: design.StatusWarning,
			Body:        "Authorize the model provider used for planning and execution.",
			ActionLabel: "Review providers", Path: "/settings?section=providers",
		},
		{
			Step: 3, Icon: primitives.IconRepositories, Title: "Repository",
			Status: "Choose a scope", Tone: design.StatusPending,
			Body:        "Select the repository CodeFlux may inspect and change.",
			ActionLabel: "Choose repository", Path: "/?setup=repository",
		},
		{
			Step: 4, Icon: primitives.IconSettings, Title: "Boundaries",
			Status: "Review required", Tone: design.StatusWarning,
			Body:        "Confirm file, command, network, and worktree authority.",
			ActionLabel: "Review permissions", Path: "/settings?section=policy",
		},
		{
			Step: 5, Icon: primitives.IconTasks, Title: "First thread",
			Status: "Ready after setup", Tone: design.StatusNeutral,
			Body: "Start with a concrete outcome. CodeFlux will plan, execute, " +
				"and preserve evidence.",
			ActionLabel: "Open tasks", Path: "/tasks", Primary: true,
		},
	}
	// Each card carries its own data-state wrapper, so a route that is loading,
	// failed, or not requested says so per step rather than showing a
	// confident setup sequence built from nothing.
	regions := make([]ui.Node, 0, len(cards))
	for _, card := range cards {
		regions = append(regions, routeStateContent(
			stateProps, firstRunSubject(card.Title), renderFirstRunCard(props, card),
		))
	}
	return routeScrollOwner(
		"first-run-scroll-owner",
		routeMain("first-run-shell", props.Title, props.Mode, regions...),
	)
}

// routeScrollOwner gives one static route its own vertical scroll.
//
// The application frame clips route content so task routes can own their pane
// scrolling. A route built from stacked regions is taller than that frame, and
// without an owner of its own everything past the fold is simply unreachable:
// the settings page measured 4114 pixels inside a 936-pixel frame with no
// scrollbar anywhere, which hid policy, appearance, data, and telemetry
// entirely. The wheel is moved explicitly because the frame swallows it.
func routeScrollOwner(component string, content ui.Node) ui.Node {
	scrollOwnerProps := html.PropsOf(html.OnWheel(handleFirstRunWheel))
	scrollOwnerProps.Data = map[string]string{
		"component":    component,
		"scroll-owner": "route",
	}
	scrollOwnerProps.Class = css.New(
		css.W(css.Full), css.H(css.Full),
		css.MinWidth(css.Zero), css.MinHeight(css.Zero),
		css.OverflowX.Hidden, css.OverflowY.Auto,
		css.OverscrollBehaviorY.Contain,
	).String()
	return html.Div(scrollOwnerProps, content)
}

func routeMain(component, title string, mode primitives.Mode, regions ...ui.Node) ui.Node {
	tokens := mode.Tokens()
	// The gap between regions is the density token rather than a fixed step.
	// Density had been computed on every render and read by nothing: choosing
	// compact changed no pixel on any page.
	gridRules := []css.Rule{
		u.Grid,
		css.GridCols(css.MinMax(css.TrackLen(css.Zero), css.Fr(1))),
		css.Gap(css.Px(tokens.Rhythm.PanelGap)),
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
					css.FontSize(css.Px(38)),
					css.LineHeightLen(css.Px(46)),
					// The colour is set rather than inherited. Without it this
					// heading took whatever an ancestor happened to supply,
					// which in the light theme was near-white on near-white:
					// the largest text on the page was invisible.
					css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
					// Natural weight and open tracking: the serif carries the
					// size, and semibold with negative tracking closes its
					// counters.
					css.FontWeight.Normal,
					css.Font(css.FontStack(tokens.Fonts.Display)),
					css.Tracking(css.Ems(0.004)),
				).String(),
				Text: title,
			}),
			html.P(html.Props{
				Class: css.New(
					css.Margin(css.Zero),
					css.MaxWidth(css.Ch(72)),
					css.TextColor(css.Hex(string(tokens.Colors.TextSecondary))),
					css.Font(css.FontStack(tokens.Fonts.Reading)),
					css.FontSize(css.Px(tokens.Typography.Body.Size)),
					css.LineHeightLen(css.Px(tokens.Typography.Body.LineHeight)),
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
		// A section without an accessible name is not exposed as a landmark,
		// so its heading labels the visible box but not the region a screen
		// reader navigates. Naming it with the same title keeps the two in
		// step and makes every route region reachable by name.
		Aria: map[string]string{"label": title},
		Class: css.New(
			u.Flex, u.FlexCol,
			css.Gap(css.Px(tokens.Spacing.SM)),
			// The inset is the density token: this is the one measurement a
			// reading-density preference is supposed to change.
			css.Padding(css.Px(tokens.Rhythm.PanelInset)),
			css.MinHeight(css.Px(210)),
			css.Rounded(css.Px(tokens.Geometry.DialogRadius)), css.MinWidth(css.Zero),
			css.Bg(css.Hex(string(tokens.Colors.SurfaceRaised))),
			css.Border(
				css.Px(tokens.Geometry.BorderWidth),
				css.Hex(string(tokens.Colors.BorderSubtle)),
			),
			css.Shadow(css.ShadowOf(
				css.Zero, css.Px(16), css.Px(40), css.Px(-32), css.RGBA(0, 0, 0, 0.58),
			)),
		).String(),
	}, append([]ui.Node{html.H2(html.Props{
		Class: css.New(
			css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
			css.Margin(css.Zero),
			// A region heading names something a person reads and judges, so
			// it takes the serif with the rest of that material rather than
			// the interface sans used for controls.
			css.Font(css.FontStack(tokens.Fonts.Display)),
			css.FontSize(css.Px(tokens.Typography.SectionTitle.Size)),
			css.LineHeightLen(css.Px(tokens.Typography.SectionTitle.LineHeight)),
			css.FontWeight.Normal,
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
	tokens := props.Mode.Tokens()
	children := []ui.Node{
		html.H1(html.Props{Class: design.HeadingClass(tokens, design.HeadingPage), Text: props.Title}),
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
	case state.ConnectionReplaying:
		return state.Announcement{
			Kind: state.AnnouncementRecovery, Message: "Committed updates are replaying",
		}
	case state.ConnectionDegraded:
		return state.Announcement{
			Kind: state.AnnouncementRecovery, Message: "Live updates are reconnecting",
		}
	case state.ConnectionIdle:
		return state.Announcement{
			Kind: state.AnnouncementConnection, Message: "Ready. Open a thread to follow its run",
		}
	case state.ConnectionDisconnected:
		return state.Announcement{
			Kind: state.AnnouncementConnection, Message: "CodeFlux is disconnected",
		}
	case state.ConnectionIncompatible:
		return state.Announcement{
			Kind: state.AnnouncementConnection, Message: "Client and coordinator are incompatible",
		}
	case state.ConnectionUnauthorized:
		return state.Announcement{
			Kind: state.AnnouncementConnection, Message: "Local session is unauthorized",
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

// executionPanels lays out what the run is doing.
//
// The measurement strip runs full width above the rest, because those figures
// are what a person scans first to decide whether to intervene. Below it the
// timeline and the live log sit side by side: one answers "how far has this
// got", the other "what is it saying", and reading either without the other
// gives a misleading picture.
// compactExecutionPanels is what the observation rail shows while a run is
// going: what it is doing now, the steps behind that, and the log — in that
// order, in one column, because the rail is three hundred and eighty pixels
// wide and every one of these panels used to lay itself out from the width of
// the window instead.
func compactExecutionPanels(props ExecutionPanelProps, mode primitives.Mode) ui.Node {
	tokens := mode.Tokens()
	return html.Div(html.Props{
		Data: map[string]string{"component": "execution-panels", "layout": "rail"},
		Class: css.New(
			u.Flex, u.FlexCol, css.MinWidth(css.Zero),
		).String(),
	},
		executionview.CurrentlyExecuting(executionview.CurrentWorkProps{
			Work: props.Current, Mode: mode, Compact: true,
		}),
		executionview.ExecutionTimeline(executionview.TimelineProps{
			Steps: props.Steps, Mode: mode, Compact: true,
		}),
		executionview.StreamingLog(executionview.LogProps{
			Lines: props.Lines, Filter: props.Filter, Streaming: props.Streaming,
			Mode: mode, OnToggle: props.OnToggleSeverity, OnClearAll: props.OnClearSeverities,
			Compact: true,
		}),
		html.Div(html.Props{Class: css.New(css.Padding(css.RawLength("14px 0 0")),
			css.BorderTop(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle)))).String()},
			executionview.MetricStrip(executionview.MetricStripProps{
				Measurements: props.Measurements, Mode: mode, Compact: true,
			}),
		),
	)
}

func executionPanels(props ExecutionPanelProps, mode primitives.Mode) ui.Node {
	tokens := mode.Tokens()
	columns := []css.Rule{
		u.Grid,
		css.GridCols(css.MinMax(css.TrackLen(css.Zero), css.Fr(1))),
		css.Gap(css.Px(tokens.Rhythm.PanelGap)),
	}
	columns = append(columns, css.Media(
		css.MinW(1100),
		css.GridCols(
			css.MinMax(css.TrackLen(css.Zero), css.Fr(3)),
			css.MinMax(css.TrackLen(css.Zero), css.Fr(2)),
		),
	)...)
	return html.Div(html.Props{
		Data: map[string]string{"component": "execution-panels"},
		Class: css.New(
			u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Rhythm.PanelGap)),
			css.PaddingY(css.Px(tokens.Rhythm.PanelGap)),
			css.MinWidth(css.Zero),
		).String(),
	},
		executionview.MetricStrip(executionview.MetricStripProps{
			Measurements: props.Measurements, Mode: mode,
		}),
		html.Div(html.Props{Class: css.New(columns...).String()},
			executionview.StreamingLog(executionview.LogProps{
				Lines: props.Lines, Filter: props.Filter, Streaming: props.Streaming,
				Mode: mode, OnToggle: props.OnToggleSeverity,
				OnClearAll: props.OnClearSeverities,
			}),
			html.Div(html.Props{
				Class: css.New(
					u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Rhythm.PanelGap)),
					css.MinWidth(css.Zero),
				).String(),
			},
				executionview.CurrentlyExecuting(executionview.CurrentWorkProps{
					Work: props.Current, Mode: mode,
				}),
				executionview.ExecutionTimeline(executionview.TimelineProps{
					Steps: props.Steps, Mode: mode,
				}),
			),
		),
	)
}

// CodeCollectionProps configures the repository's code directory.
type CodeCollectionProps struct {
	Title      string
	Mode       primitives.Mode
	Collection codecollection.Props
}

// CodeCollectionShell is the repository's own collection on its own surface.
//
// The repository map was built and never read by anything in the product. This
// is where a person sees what their code actually contains — its packages, its
// declarations, and the documentation each one carries — and which of those
// declarations are admitted atoms.
//
// It owns the full route frame rather than the centred reading column every
// other simple route uses, because it is a browsing surface: three panes that
// scroll independently, not a page somebody reads top to bottom.
func CodeCollectionShell(props CodeCollectionProps) ui.Node {
	title := props.Title
	if title == "" {
		title = "Code collection"
	}
	tokens := props.Mode.Tokens()
	return html.Main(html.Props{
		ID: "main-content", TabIndex: -1,
		Data: map[string]string{
			"component": "code-collection-shell", "focus-region": "conversation",
			"focus-order": "2",
		},
		Class: css.New(
			u.Grid,
			css.GridRows(css.TrackAuto, css.MinMax(css.TrackLen(css.Zero), css.Fr(1))),
			css.Gap(css.Px(tokens.Spacing.MD)),
			css.W(css.Full), css.H(css.Full),
			css.MinWidth(css.Zero), css.MinHeight(css.Zero),
			css.Padding(css.RawLength("12px 20px 20px")),
			css.Bg(css.Hex(string(tokens.Colors.Canvas))),
			css.Overflow.Hidden,
		).String(),
	},
		html.H1(html.Props{
			Text: title,
			Class: css.New(
				css.FontSize(css.Px(tokens.Typography.SectionTitle.Size)),
				css.LineHeightLen(css.Px(tokens.Typography.SectionTitle.LineHeight)),
				css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
			).String(),
		}),
		ui.CreateElement(codecollection.Component, props.Collection),
	)
}
