//go:build js && wasm

// Command client is the generated-asset CodeFlux GWC v5 browser application.
package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/buildinfo"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
	"codeflux.dev/codeflux/web/frontend/design"
	"codeflux.dev/codeflux/web/frontend/executionview"
	frontendi18n "codeflux.dev/codeflux/web/frontend/i18n"
	"codeflux.dev/codeflux/web/frontend/preferences"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"codeflux.dev/codeflux/web/frontend/routes"
	"codeflux.dev/codeflux/web/frontend/sessionclient"
	"codeflux.dev/codeflux/web/frontend/shell"
	frontendstate "codeflux.dev/codeflux/web/frontend/state"
	"codeflux.dev/codeflux/web/frontend/taskcontrols"
	"codeflux.dev/codeflux/web/frontend/threadrail"
	"github.com/monstercameron/GoWebComponents/v5/fetch"
	"github.com/monstercameron/GoWebComponents/v5/router"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/GoWebComponents/v5/utils"
)

func main() {
	history := router.NewHistoryRouter(router.RouterOptions{DefaultRoute: "/"})
	for _, route := range []string{
		"/",
		"/tasks",
		"/graphs",
		"/memory",
		"/settings",
		"/diagnostics",
		"/first-run",
	} {
		history.Register(route, productPage, router.Options{Title: "CodeFlux"})
	}
	// Deep links are deliberately handled by the catch-all and the product's
	// typed route parser. This keeps the browser router responsible only for
	// mounting the Go application and avoids duplicating route authority.
	history.Register("*", productPage, router.Options{Title: "CodeFlux"})
	history.Mount("#app")
	utils.WaitForever()
}

func productPage(router.Attrs) ui.Node {
	return ui.CreateElement(productApplication)
}

func productApplication() ui.Node {
	renderStarted := time.Now()
	focusManager := ui.UseFocusManager()
	location := router.UseLocation()
	navigator := router.UseNavigate()
	bootstrap := fetch.UseResource(loadBootstrap)
	streamStatus := ui.UseState(sessionclient.Status{State: sessionclient.StateIdle})
	reconnectVersion := ui.UseState(uint64(0))
	reducedMotion := ui.UsePrefersReducedMotion()
	preferredScheme := ui.UsePrefersColorScheme()
	activeTheme, setTheme := ui.UseTheme(string(preferredScheme))
	layoutState := ui.UseState(frontendstate.DefaultLayoutPreferences())
	logSeverities := ui.UseState(map[executionview.Severity]bool{})
	selectedGraphID := ui.UseState("")
	graphMessageStableKey := ui.UseState("")
	graphMessageNotice := ui.UseState("")
	selectedThread := ui.UseState(threadrail.Thread{})
	latestThreadEvent := ui.UseState(events.SessionEvent{})
	taskObservedAt := ui.UseRef(time.Now())
	reconnectStartedAt := ui.UseRef(time.Time{})
	preferencesReady := ui.UseState(false)
	resource := bootstrap.Get()
	restoreContext := routes.RestorationContext{}
	restoreKey := ""
	var restoreContextErr error
	if resource.Ready && compatible(resource.Value) {
		restoreContext, restoreContextErr = restorationContext(resource.Value, true)
		if restoreContextErr == nil {
			restoreKey = routeAccessKey(resource.Value)
		}
	}
	useLayoutPreferences(
		location.Path,
		layoutState,
		preferencesReady,
		restoreKey,
		restoreContext,
	)
	theme := design.Theme(activeTheme)
	if theme != design.ThemeLight && theme != design.ThemeDark && theme != design.ThemeHighContrast {
		theme = design.ThemeDark
	}
	tokens, tokenErr := design.TokensFor(design.Options{
		Theme: theme, Density: design.DensityComfortable,
		ReducedMotion: reducedMotion,
	})
	if tokenErr != nil {
		tokens, _ = design.TokensFor(design.Options{})
	}

	store := coordinatorStore()
	session := frontendstate.SessionView{
		Bootstrap:  frontendstate.BootstrapBooting,
		Connection: frontendstate.ConnectionConnecting,
	}
	switch {
	case resource.Loading:
		session = frontendstate.SessionView{
			Bootstrap:  frontendstate.BootstrapBooting,
			Connection: frontendstate.ConnectionConnecting,
			Message:    "Checking the local coordinator and restoring safe application state.",
		}
	case resource.Error != nil:
		session = sessionForStartupFailure(resource.Error)
	case resource.Ready:
		if restoreContextErr != nil {
			session = frontendstate.SessionView{
				Bootstrap:  frontendstate.BootstrapIncompatible,
				Connection: frontendstate.ConnectionIncompatible,
				Message:    "The coordinator returned incompatible route access state.",
			}
		} else if compatible(resource.Value) {
			if selectedThread.Get().SessionID().IsZero() {
				session = frontendstate.SessionView{
					Bootstrap:  frontendstate.BootstrapReady,
					Connection: frontendstate.ConnectionDisconnected,
					Message:    "Secure startup completed. Select a session to begin live updates.",
				}
			} else {
				session = sessionViewForLifecycle(streamStatus.Get())
			}
		} else {
			session = frontendstate.SessionView{
				Bootstrap:  frontendstate.BootstrapIncompatible,
				Connection: frontendstate.ConnectionIncompatible,
				Message:    "The frontend and coordinator versions do not match.",
			}
		}
	}
	if session.Bootstrap == frontendstate.BootstrapReady &&
		restoreKey != "" &&
		isShellPreviewRoute(location.Path) &&
		!preferencesReady.Get() {
		// Keep the interactive shell behind the bounded startup surface until
		// persisted layout has either loaded or been rejected. Otherwise a
		// late preference read can overwrite a tab or splitter change made
		// immediately after first paint.
		session = frontendstate.SessionView{
			Bootstrap:  frontendstate.BootstrapBooting,
			Connection: session.Connection,
			Message:    "Restoring local layout preferences.",
		}
	}
	store = store.ReduceRemote(frontendstate.SessionChanged{Session: session})
	responsive := responsiveLayout()
	layout := layoutState.Get().Normalize()
	layout.Viewport = responsive.Viewport
	var layoutErr error
	store, layoutErr = store.ReduceUI(
		frontendstate.LayoutChanged{Preferences: layout},
	)
	if layoutErr != nil {
		store = coordinatorStore()
		store = store.ReduceRemote(frontendstate.SessionChanged{Session: session})
	}
	if selectedGraphID.Get() != "" {
		if selectedStore, selectionErr := store.ReduceUI(frontendstate.GraphNodeSelected{
			NodeID: selectedGraphID.Get(),
		}); selectionErr == nil {
			store = selectedStore
		}
	}
	appRoute := routeFor(location.Path, location.Query)
	taskDetailSelection := routes.TaskDetailSelection{}
	if location.Path == "/tasks" {
		if selection, err := routes.ParseTaskDetailSelection(location.Query); err == nil {
			taskDetailSelection = selection
		}
	}
	telemetryProps := useMountedFrontendTelemetry(appRoute.Name == routes.Settings)
	if location.Path == "/" &&
		strings.TrimSpace(location.Query) == "" &&
		resource.Ready &&
		restoreContextErr == nil &&
		!restoreContext.FirstRunComplete {
		// Render the safe first-run destination on the first committed frame.
		// Route restoration may canonicalize the URL afterward, but it must not
		// expose a transient repository chooser for an unfinished installation.
		appRoute = routes.Route{Name: routes.FirstRun}
	}
	threadRailSource := useMountedThreadRailFirstPage(resource.Value, store.Snapshot(), appRoute)
	repositoryChoices := useMountedRepositoryChoices(resource.Value)
	// The repository, branch, and worktree status come from the coordinator's
	// own reading of the working tree. Anything it did not answer is left for
	// the top bar to render as unknown.
	store = store.ReduceRemote(frontendstate.TopBarChanged{
		TopBar: applyWorkspaceSummary(store.Snapshot().TopBar, repositoryChoices.Summary),
	})
	selectedRefreshRevision := uint64(0)
	if row, ok := mountedThreadRailRow(threadRailSource.State.Value, selectedThread.Get().ID()); ok {
		selectedRefreshRevision = row.Thread().Revision()
	}
	// The route is part of the dependency because it is what a person typed or
	// followed, and the rail's identity is part of it because the route can
	// only be honoured once the rail has loaded the thread it names.
	selectedRefreshDependency := selectedThread.Get().ID().String() + "|" +
		strconv.FormatUint(selectedRefreshRevision, 10) + "|" +
		appRoute.ThreadID.String() + "|" +
		threadRailSource.State.Value.RepositoryID().String()
	ui.UseEffectOf(func() func() {
		selected := selectedThread.Get()
		if threadRailSource.State.Value.RepositoryID().IsZero() {
			return nil
		}
		if selected.ID().IsZero() {
			// Nothing had ever adopted the thread the URL names. The rail
			// restored it internally, so the interface drew the right
			// conversation, but no session was opened against it and the whole
			// app reported itself disconnected against sample content.
			if appRoute.ThreadID.IsZero() {
				return nil
			}
			if row, ok := mountedThreadRailRow(
				threadRailSource.State.Value, appRoute.ThreadID,
			); ok {
				selectedThread.Set(row.Thread())
			}
			return nil
		}
		if row, ok := mountedThreadRailRow(threadRailSource.State.Value, selected.ID()); ok && row.Thread() != selected {
			selectedThread.Set(row.Thread())
		}
		return nil
	}, selectedRefreshDependency)
	var authoritativeTaskControls *taskcontrols.Props
	// Pause and stop stay unbound until a real task supplies them. They used to
	// flip a browser-local string, so the interface reported a run as paused
	// while nothing had been asked to pause — the worst possible lie for a
	// control whose entire purpose is stopping work. A nil handler renders the
	// control disabled, which is the truth.
	var onPauseRequested func()
	var onStopRequested func()
	var reloadTaskControls func()
	var reloadGraph func()
	previewTimeline := livePreviewTimeline()
	timelineSource := mountedAuthoritativeTimeline(
		selectedThread.Get(), previewTimeline, reconnectVersion.Get(), taskDetailSelection.EventID, func(event events.SessionEvent) {
			latestThreadEvent.Set(event)
			for _, telemetryEvent := range telemetryForSessionEvent(event, time.Since(taskObservedAt.Get())) {
				emitBrowserFrontendTelemetry(telemetryEvent)
			}
			switch event.Kind {
			case events.KindMessageFinal, events.KindThreadRenamed, events.KindThreadArchived,
				events.KindTaskStateChanged, events.KindBudgetUpdated, events.KindValidationUpdated,
				events.KindRecoveryRequired, events.KindChangeAcceptanceUpdated:
				threadRailSource.Reload()
			}
			switch event.Kind {
			case events.KindTaskStateChanged, events.KindForecastUpdated,
				events.KindUsageUpdated, events.KindBudgetUpdated, events.KindRecoveryRequired:
				if reloadTaskControls != nil {
					reloadTaskControls()
				}
			}
			switch event.Kind {
			case events.KindGraphSnapshot, events.KindGraphPatch:
				if reloadGraph != nil {
					reloadGraph()
				}
			}
		}, streamStatus.Set,
	)
	taskControlSource := useSelectedTaskControls(
		selectedThread.Get(), session, timelineSource.Task, timelineSource.TaskReady,
	)
	reloadTaskControls = taskControlSource.Reload
	if taskControlSource.State.Ready {
		controls := taskControlSource.State.Value
		authoritativeTaskControls = &controls
	}
	store = store.ReduceRemote(frontendstate.DiagnosticsChanged{Diagnostics: diagnosticsViewForSessionProjection(
		timelineSource.SessionDiagnostics,
		timelineSource.SessionConnection,
		timelineSource.SessionReady,
	)})
	timelineProps := timelineSource.Props
	if graphMessageStableKey.Get() != "" {
		timelineProps.SelectedStableKey = graphMessageStableKey.Get()
		timelineProps.SelectionNotice = graphMessageNotice.Get()
	}
	instrumentTimelineTelemetry(&timelineProps, selectedThread.Get().TaskID(), emitBrowserFrontendTelemetry)
	if taskDetailSelection.ReviewFile != "" {
		timelineProps.ReviewOpen = true
		timelineProps.ReviewFile = taskDetailSelection.ReviewFile
		timelineProps.OnCloseReview = func() {
			if path, err := routes.TaskSelectionPath(taskDetailSelection.Route); err == nil {
				navigator.Navigate(path)
			}
		}
	}
	timelineProps.ReviewContent = useMountedReview(
		selectedThread.Get(),
		primitives.Mode{
			Theme: tokens.Theme, Density: tokens.Density,
			HighContrast: tokens.Theme == design.ThemeHighContrast, ReducedMotion: tokens.ReducedMotion,
		},
		timelineProps.ReviewFile,
		reviewNavigation{
			OpenPlan: func(revision uint64, stepID string) {
				graphMessageStableKey.Set("plan:" + strconv.FormatUint(revision, 10))
				graphMessageNotice.Set("Showing plan step " + stepID + " from the reviewed diff.")
			},
			OpenEvent: func(eventID domain.EventID) {
				if path, err := routes.TaskEventSelectionPath(taskDetailSelection.Route, eventID); err == nil {
					navigator.Navigate(path)
					return
				}
				graphMessageStableKey.Set("event:" + eventID.String())
				graphMessageNotice.Set("Showing the reviewed tool event in the loaded timeline.")
			},
			OpenValidation: func(validationID domain.ValidationID) {
				graphMessageStableKey.Set("validation:" + validationID.String())
				graphMessageNotice.Set("Showing validation evidence linked from the reviewed diff.")
			},
		},
	)
	if authoritativeTaskControls != nil && timelineSource.TaskReady {
		decorateTaskControlsFromProjection(authoritativeTaskControls, timelineSource.Task)
		if taskControlSource.DecorateRecovery != nil {
			taskControlSource.DecorateRecovery(authoritativeTaskControls)
		}
		if taskDetailSelection.Route.Name == routes.ThreadWorkspace {
			authoritativeTaskControls.OnOpenDetail = func(detail taskcontrols.RecoveryDetail) {
				var path string
				var err error
				switch detail.Kind {
				case taskcontrols.RecoveryDetailEvent:
					eventID, parseErr := domain.ParseEventID(detail.Identity)
					if parseErr != nil {
						return
					}
					path, err = routes.TaskEventSelectionPath(taskDetailSelection.Route, eventID)
				case taskcontrols.RecoveryDetailFile:
					path, err = routes.TaskFileSelectionPath(taskDetailSelection.Route, detail.Identity)
				default:
					return
				}
				if err == nil {
					navigator.Navigate(path)
				}
			}
		}
	}
	if authoritativeTaskControls != nil {
		instrumentTaskControlTelemetry(authoritativeTaskControls, emitBrowserFrontendTelemetry)
		topBar := taskControlsTopBar(store.Snapshot().TopBar, *authoritativeTaskControls)
		topBar.TaskTitle = selectedThread.Get().Title()
		store = store.ReduceRemote(frontendstate.TopBarChanged{TopBar: topBar})
		if authoritativeTaskControls.OnResume != nil {
			onPauseRequested = authoritativeTaskControls.OnResume
		} else {
			onPauseRequested = authoritativeTaskControls.OnPause
		}
		onStopRequested = authoritativeTaskControls.OnStop
	}
	composerProps := livePreviewComposer(selectedThread.Get(), latestThreadEvent.Get(), selectedGraphID)
	if timelineSource.TaskReady {
		composerProps = bindAuthoritativeComposerTaskActionsWithCallbacks(
			composerProps,
			timelineSource.Task,
			session.Connection,
			authoritativeTaskControls,
			authoritativeComposerCallbacks{
				InspectGraph: func() { selectedGraphID.Set("implementation") },
				Review:       timelineProps.OnOpenReview,
			},
		)
	}
	taskState := domain.TaskState("")
	projectionGraphRevision := uint64(0)
	if timelineSource.TaskReady {
		taskState = timelineSource.Task.State
		projectionGraphRevision = timelineSource.Task.Graph.Revision
	}
	graphSource := useMountedGraph(
		selectedThread.Get(), taskState, projectionGraphRevision,
		primitives.Mode{
			Theme: tokens.Theme, Density: tokens.Density,
			HighContrast: tokens.Theme == design.ThemeHighContrast, ReducedMotion: tokens.ReducedMotion,
		},
		func(explanation string) {
			if composerProps.OnTextChange == nil {
				return
			}
			composerProps.OnTextChange(graphExplanationDraft(composerProps.View.Draft.Text(), explanation))
			focusManager.FocusByID("thread-composer")
		},
		func(messageIDs []domain.MessageID) {
			if len(messageIDs) == 0 {
				graphMessageStableKey.Set("")
				graphMessageNotice.Set("The selected graph node has no linked conversation messages.")
				return
			}
			graphMessageStableKey.Set("message:" + messageIDs[0].String())
			graphMessageNotice.Set(fmt.Sprintf(
				"Showing the first of %d conversation message(s) linked to the selected graph node.",
				len(messageIDs),
			))
		},
	)
	reloadGraph = graphSource.Reload
	// The conversation and graph panes follow their own authoritative sources
	// rather than the store's opening guess. Left at the opening guess they
	// drew loading skeletons that nothing would ever resolve, which reads as a
	// coordinator that is thinking when it is in fact idle.
	store = store.ReduceRemote(frontendstate.MessagesAppended{
		State: panePresence(
			timelineSource.SessionReady, len(timelineSource.Props.Cards),
		),
		Messages: nil,
	})
	// A thread with no task has no graph to load, so the pane is answered and
	// empty rather than perpetually loading. Reporting it as loading drew
	// skeleton bars that nothing would ever replace.
	graphAnswered := graphSource.Authoritative != nil || !timelineSource.TaskReady
	store = store.ReduceRemote(frontendstate.GraphReplaced{
		State: panePresence(graphAnswered, graphNodePresence(graphSource)),
		Nodes: nil,
	})
	var onReconnectRequested func()
	if !selectedThread.Get().SessionID().IsZero() {
		onReconnectRequested = func() {
			reconnectStartedAt.Set(time.Now())
			reconnectVersion.Set(reconnectVersion.Get() + 1)
		}
	}
	useReconnectCompletionTelemetry(streamStatus.Get(), selectedThread.Get().SessionID(), reconnectStartedAt)
	useSlowRenderTelemetry(renderStarted, location.Path+"|"+selectedThread.Get().ID().String()+"|"+strconv.FormatUint(selectedRefreshRevision, 10))
	navigatePath := func(path string) {
		if appRoute.Name == routes.FirstRun {
			emitFirstRunCompletionTelemetry()
		}
		if path == "/graphs" {
			emitGraphOpenedTelemetry(selectedThread.Get().TaskID())
		}
		navigator.Navigate(path)
	}
	// The execution panels read the same durable events the timeline does, so
	// what a person sees happening and what the log says cannot disagree.
	execution := projectExecution(
		timelineSource.Task, timelineSource.TaskReady, timelineSource.Events,
		executionview.Filter{Selected: logSeverities.Get()},
		// The log is streaming while the session is live or replaying; a
		// reconnecting session is not receiving, and saying it is would make a
		// stalled run look busy.
		streamStatus.Get().State == sessionclient.StateLive ||
			streamStatus.Get().State == sessionclient.StateReplaying,
	)
	if execution != nil {
		execution.OnToggleSeverity = func(severity executionview.Severity) {
			next := map[executionview.Severity]bool{}
			for key, selected := range logSeverities.Get() {
				next[key] = selected
			}
			next[severity] = !next[severity]
			logSeverities.Set(next)
		}
		execution.OnClearSeverities = func() {
			logSeverities.Set(map[executionview.Severity]bool{})
		}
	}
	root := shell.RootProps{
		Snapshot:     store.Snapshot(),
		Execution:    execution,
		Composer:     composerProps,
		Timeline:     timelineProps,
		TaskControls: authoritativeTaskControls,
		Telemetry:    telemetryProps,
		ThreadRail: ui.CreateElement(mountedThreadRail, mountedThreadRailProps{
			Envelope: resource.Value, Snapshot: store.Snapshot(), Route: appRoute,
			Mode: primitives.Mode{
				Theme: tokens.Theme, Density: tokens.Density,
				HighContrast: tokens.Theme == design.ThemeHighContrast, ReducedMotion: tokens.ReducedMotion,
			},
			OnNavigate: func(route routes.Route) {
				path, err := routes.Path(route)
				if location.Path == "/tasks" {
					path, err = routes.TaskSelectionPath(route)
				}
				if err == nil {
					navigator.Navigate(path)
				}
			},
			OnAuthoritativeSelection: func(thread threadrail.Thread) {
				if thread.TaskID() != selectedThread.Get().TaskID() {
					taskObservedAt.Set(time.Now())
				}
				selectedThread.Set(thread)
			},
			FirstPage: threadRailSource.State,
		}),
		AuthoritativeGraph: graphSource.Authoritative,
		GraphInspector:     graphSource.Inspector,
		RepositoryChoices:  repositoryChoices.Choices,
		SelectedScope:      selectedNavigationScope(resource.Value),
		Route:              appRoute,
		Tokens:             tokens,
		Translator: frontendi18n.EnglishRegistry().Resolve(
			string(frontendi18n.LocaleEnglishUnitedStates),
		),
		OnLayoutChange: layoutState.Set,
		OnGraphSelect: func(id string) {
			emitGraphNavigatedTelemetry(selectedThread.Get().TaskID())
			selectedGraphID.Set(id)
		},
		OnThreadNavigate: func(route routes.Route) {
			path, err := routes.Path(route)
			if location.Path == "/tasks" {
				path, err = routes.TaskSelectionPath(route)
			}
			if err == nil {
				navigator.Navigate(path)
			}
		},
		OnNavigatePath:       navigatePath,
		OnReconnectRequested: onReconnectRequested,
		OnRetry:              bootstrap.Reload,
		OnPauseRequested:     onPauseRequested,
		OnStopRequested:      onStopRequested,
		OnThemeChange: func() {
			switch theme {
			case design.ThemeDark:
				setTheme(string(design.ThemeLight))
			case design.ThemeLight:
				setTheme(string(design.ThemeHighContrast))
			default:
				setTheme(string(design.ThemeDark))
			}
		},
	}
	return ui.CreateElement(shell.SessionBootstrap, shell.SessionBootstrapProps{
		Root:    root,
		Dispose: bootstrap.Cancel,
	})
}

type preferenceEffectDependency struct {
	Ready  bool
	Route  string
	Layout frontendstate.LayoutPreferences
}

func useLayoutPreferences(
	route string,
	layout ui.State[frontendstate.LayoutPreferences],
	ready ui.State[bool],
	restorationKey string,
	restorationContext routes.RestorationContext,
) {
	ui.UseEffectOf(func() func() {
		if restorationKey == "" {
			return nil
		}
		store, err := preferences.OpenBrowserStore()
		if isShellPreviewRoute(route) {
			if err == nil {
				if record, loadErr := store.Load(context.Background()); loadErr == nil {
					layout.Set(record.Layout)
				}
			}
			ready.Set(true)
			return nil
		}
		var restoredRoute routes.Route
		if err != nil {
			restoredRoute = routes.Restore(route, restorationContext).Route
		} else if route == "/" {
			restored, restoreErr := store.LoadAndRestore(context.Background(), restorationContext)
			if restoreErr == nil {
				layout.Set(restored.Record.Layout)
				restoredRoute = restored.Route
			} else {
				record, loadErr := store.Load(context.Background())
				if loadErr == nil {
					layout.Set(record.Layout)
					restoredRoute = routes.Restore(record.LastRoute, restorationContext).Route
				} else {
					restoredRoute = routes.Restore(route, restorationContext).Route
				}
			}
		} else {
			if record, loadErr := store.Load(context.Background()); loadErr == nil {
				layout.Set(record.Layout)
			}
			restoredRoute = routes.Restore(route, restorationContext).Route
		}
		if restoredPath, pathErr := routes.Path(restoredRoute); pathErr == nil && restoredPath != route {
			router.NavigateReplace(restoredPath)
		}
		ready.Set(true)
		return nil
	}, "codeflux-layout-preferences-load-v2|"+restorationKey)

	dependency := preferenceEffectDependency{
		Ready:  ready.Get(),
		Route:  route,
		Layout: layout.Get().Normalize(),
	}
	ui.UseEffectOf(func() func() {
		if !dependency.Ready {
			return nil
		}
		store, err := preferences.OpenBrowserStore()
		if err != nil {
			return nil
		}
		record, err := preferences.New(dependency.Route, dependency.Layout)
		if err != nil {
			return nil
		}
		_ = store.Save(context.Background(), record)
		return nil
	}, dependency)
}

func isShellPreviewRoute(path string) bool {
	switch path {
	case "/tasks", "/graphs", "/memory", "/settings", "/diagnostics", "/first-run":
		return true
	default:
		return false
	}
}

func loadBootstrap(context context.Context) (bootstrapEnvelope, error) {
	envelope, err := loadBootstrapWith(context, defaultBootstrapTimeout, browserStartupRequest)
	if err != nil {
		emitFirstRunFailureTelemetry(err)
	}
	return envelope, err
}

func browserStartupRequest(
	context context.Context,
	path string,
) (startupHTTPResponse, error) {
	resultChannel := fetch.Fetch(path, fetch.Options{Method: "GET"})
	select {
	case <-context.Done():
		return startupHTTPResponse{}, context.Err()
	case result := <-resultChannel:
		if result.Err != nil {
			return startupHTTPResponse{}, result.Err
		}
		return startupHTTPResponse{Status: result.Status, Body: result.Text()}, nil
	}
}

func compatible(server bootstrapEnvelope) bool {
	client := buildinfo.Current()
	return server.ApplicationVersion == client.Version &&
		server.APIVersion == "codeflux.v1" &&
		server.SchemaVersion == int(client.SchemaVersion) &&
		server.FrontendVersion == client.FrontendVersion &&
		server.BridgePath == "/grpc"
}

func responsiveLayout() frontendstate.LayoutPreferences {
	layout := frontendstate.DefaultLayoutPreferences()
	wide := ui.UseMediaQuery("(min-width: 1440px)")
	standard := ui.UseMediaQuery("(min-width: 1180px)")
	compact := ui.UseMediaQuery("(min-width: 800px)")
	switch {
	case wide:
		layout.Viewport = frontendstate.ViewportWide
	case standard:
		layout.Viewport = frontendstate.ViewportMedium
	case compact:
		layout.Viewport = frontendstate.ViewportNarrow
		layout.ActivePane = frontendstate.PaneConversation
	default:
		layout.Viewport = frontendstate.ViewportMinimum
		layout.ActivePane = frontendstate.PaneConversation
	}
	return layout
}

func routeFor(path, query string) routes.Route {
	switch path {
	case "/tasks":
		if selected, err := routes.ParseTaskSelection(query); err == nil {
			return selected
		}
		return routes.Route{Name: routes.ThreadWorkspace}
	case "/graphs":
		return routes.Route{Name: routes.Graphs}
	case "/memory":
		// The local preview route deliberately omits a repository identifier so
		// every frontend data state can be exercised before repository setup.
		return routes.Route{Name: routes.Memory}
	}
	route, err := routes.Parse(path)
	if err != nil {
		return routes.Route{Name: routes.NotFound}
	}
	return route
}

// coordinatorStore is the application's starting state.
//
// It is deliberately empty. This used to be a fixture — five invented threads,
// five invented messages, a ten-node graph, a task called "Implement the
// Codeflux frontend shell" and a cost of $0.42 — drawn on every boot before any
// real data arrived, and left in place wherever a real source had nothing to
// say. The result was an interface that looked like it was working on
// something while connected to nothing, which is the one thing a supervision
// tool must never do. Every pane now fills from the coordinator or shows its
// own empty state.
func coordinatorStore() frontendstate.Store {
	store := frontendstate.NewStore(frontendstate.NewSnapshot(nil, nil, nil))
	// Each surface is marked loading rather than ready: they are answered by
	// their own mounted resources, and reporting them ready while empty would
	// draw "nothing here" over data that is still on its way.
	store = store.ReduceRemote(frontendstate.SettingsChanged{
		Settings: frontendstate.SettingsView{State: frontendstate.DataLoading},
	})
	store = store.ReduceRemote(frontendstate.MemoryChanged{
		Memory: frontendstate.MemoryView{State: frontendstate.DataLoading},
	})
	store = store.ReduceRemote(frontendstate.DiagnosticsChanged{
		Diagnostics: frontendstate.DiagnosticsView{State: frontendstate.DataLoading},
	})
	store = store.ReduceRemote(frontendstate.FirstRunChanged{
		FirstRun: frontendstate.FirstRunView{State: frontendstate.DataLoading},
	})
	store = store.ReduceRemote(frontendstate.ThreadsReplaced{
		State: frontendstate.DataLoading, Threads: nil,
	})
	store = store.ReduceRemote(frontendstate.MessagesAppended{
		State: frontendstate.DataLoading, Messages: nil,
	})
	store = store.ReduceRemote(frontendstate.GraphReplaced{
		State: frontendstate.DataLoading, Nodes: nil,
	})
	return store
}

// panePresence reports what a pane should draw for itself.
//
// A pane with no answer yet is loading; a pane with an answer holding nothing
// is empty. Collapsing the two is what made an idle coordinator look busy: the
// skeleton never resolved because there was nothing coming.
func panePresence(answered bool, count int) frontendstate.DataState {
	switch {
	case !answered:
		return frontendstate.DataLoading
	case count == 0:
		return frontendstate.DataReadyEmpty
	default:
		return frontendstate.DataReady
	}
}

// graphNodePresence counts the nodes an authoritative graph actually holds.
func graphNodePresence(source mountedGraphSource) int {
	if source.Authoritative == nil {
		return 0
	}
	return len(source.Authoritative.Layout.Nodes)
}

// selectedNavigationScope is what the coordinator says is open.
//
// The navigation rail needs it because the route a person is standing on does
// not always name a repository — the chooser never does — so a rail reading
// only the route disabled Tasks and Memory with "Choose a repository first" on
// the page every session begins at, while a repository was already open.
func selectedNavigationScope(envelope bootstrapEnvelope) shell.NavigationScope {
	scope := shell.NavigationScope{}
	if identity := envelope.SelectedRepositoryID; identity != nil {
		if repositoryID, err := domain.ParseRepositoryID(identity.GetValue()); err == nil {
			scope.RepositoryID = repositoryID
		}
	}
	if identity := envelope.SelectedThreadID; identity != nil {
		if threadID, err := domain.ParseThreadID(identity.GetValue()); err == nil {
			scope.ThreadID = threadID
		}
	}
	// A thread without its repository cannot be navigated to, and offering it
	// would produce a link to a path that does not parse.
	if scope.RepositoryID.IsZero() {
		scope.ThreadID = domain.ThreadID{}
	}
	return scope
}
