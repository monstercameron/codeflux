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
	selectedGraphID := ui.UseState("")
	graphMessageStableKey := ui.UseState("")
	graphMessageNotice := ui.UseState("")
	selectedThread := ui.UseState(threadrail.Thread{})
	latestThreadEvent := ui.UseState(events.SessionEvent{})
	taskObservedAt := ui.UseRef(time.Now())
	reconnectStartedAt := ui.UseRef(time.Time{})
	previewTaskState := ui.UseState("in progress")
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

	store := sampleStore()
	previewTopBar := store.Snapshot().TopBar
	previewTopBar.TaskState = previewTaskState.Get()
	previewTopBar.CanPause = previewTaskState.Get() != "stopped"
	previewTopBar.CanStop = previewTaskState.Get() != "stopped"
	store = store.ReduceRemote(frontendstate.TopBarChanged{TopBar: previewTopBar})
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
		store = sampleStore()
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
	selectedRefreshRevision := uint64(0)
	if row, ok := mountedThreadRailRow(threadRailSource.State.Value, selectedThread.Get().ID()); ok {
		selectedRefreshRevision = row.Thread().Revision()
	}
	selectedRefreshDependency := selectedThread.Get().ID().String() + "|" + strconv.FormatUint(selectedRefreshRevision, 10)
	ui.UseEffectOf(func() func() {
		selected := selectedThread.Get()
		if selected.ID().IsZero() || threadRailSource.State.Value.RepositoryID().IsZero() {
			return nil
		}
		if row, ok := mountedThreadRailRow(threadRailSource.State.Value, selected.ID()); ok && row.Thread() != selected {
			selectedThread.Set(row.Thread())
		}
		return nil
	}, selectedRefreshDependency)
	var authoritativeTaskControls *taskcontrols.Props
	onPauseRequested := func() {
		if previewTaskState.Get() == "paused" {
			previewTaskState.Set("in progress")
			return
		}
		previewTaskState.Set("paused")
	}
	onStopRequested := func() { previewTaskState.Set("stopped") }
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
	root := shell.RootProps{
		Snapshot:     store.Snapshot(),
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

func sampleStore() frontendstate.Store {
	threads := []frontendstate.ThreadView{
		{ID: "thread-1", Title: "Implement frontend shell", Status: "active", Unread: 2},
		{ID: "thread-2", Title: "Harden recovery boundaries", Status: "complete"},
		{ID: "thread-3", Title: "Compare provider evidence", Status: "waiting"},
		{ID: "thread-4", Title: "Review workspace patch", Status: "blocked"},
		{ID: "thread-5", Title: "Generate API contracts", Status: "waiting"},
	}
	messages := []frontendstate.MessageView{
		{
			ID: "message-1", Role: "requirement",
			Body:     "Build the Codeflux product shell entirely in typed Go with no handwritten JavaScript, HTML, or CSS.",
			Sequence: 1,
		},
		{
			ID: "message-2", Role: "forecast",
			Body:     "The shell can be completed locally. Primary risks are deep-route bootstrap, responsive composition, keyboard focus, and truthful task-state presentation.",
			Sequence: 2,
		},
		{
			ID: "message-3", Role: "plan",
			Body:     "Implement design tokens and route shells, connect the embedded GWC runtime, then verify wide, standard, compact, and minimum viewports.",
			Sequence: 3,
		},
		{
			ID: "message-4", Role: "execution",
			Body:     "Rendering the canonical conversation/graph split and rebuilding the loopback server from the current source.",
			Pending:  true,
			Sequence: 4,
		},
		{
			ID: "message-5", Role: "validation",
			Body:     "Go component tests and the WASM build pass. Browser accessibility and visual comparison are running.",
			Sequence: 5,
		},
	}
	graph := []frontendstate.GraphNodeView{
		{ID: "requirements", Title: "Shell requirements", Status: "complete"},
		{ID: "design", Title: "Design tokens", Status: "complete"},
		{ID: "routes", Title: "Route model", Status: "complete"},
		{ID: "bootstrap", Title: "Session bootstrap", Status: "complete"},
		{ID: "implementation", Title: "GWC workspace", Status: "active", Selected: true},
		{ID: "responsive", Title: "Responsive layout", Status: "active"},
		{ID: "browser", Title: "Browser tests", Status: "running"},
		{ID: "plan", Title: "Refinement plan", Status: "complete"},
		{ID: "review", Title: "Adversarial review", Status: "waiting"},
		{ID: "evidence", Title: "Release evidence", Status: "waiting"},
	}
	store := frontendstate.NewStore(
		frontendstate.NewSnapshot(threads, messages, graph),
	)
	store = store.ReduceRemote(frontendstate.WorkspaceChanged{
		Workspace: frontendstate.WorkspaceView{
			RepositoryID:   "codeflux",
			RepositoryName: "codeflux",
			Branch:         "main",
			Dirty:          true,
		},
	})
	store = store.ReduceRemote(frontendstate.TopBarChanged{
		TopBar: frontendstate.TopBarView{
			Repository:     "codeflux",
			Branch:         "main",
			WorktreeStatus: "uncommitted changes",
			TaskTitle:      "Implement the Codeflux frontend shell",
			TaskSummary:    "Build the local-first GWC workspace with explicit correctness and browser evidence.",
			TaskState:      "in progress",
			Model:          "gpt-5",
			Effort:         "high",
			ForecastCost:   "$0.36",
			ActualCost:     "$0.42",
			HardBudget:     "$4.00",
		},
	})
	store = store.ReduceRemote(frontendstate.SettingsChanged{
		Settings: frontendstate.SettingsView{State: frontendstate.DataReady},
	})
	store = store.ReduceRemote(frontendstate.MemoryChanged{
		Memory: frontendstate.MemoryView{State: frontendstate.DataReady},
	})
	store = store.ReduceRemote(frontendstate.DiagnosticsChanged{
		Diagnostics: frontendstate.DiagnosticsView{State: frontendstate.DataReady},
	})
	store = store.ReduceRemote(frontendstate.FirstRunChanged{
		FirstRun: frontendstate.FirstRunView{State: frontendstate.DataReady},
	})
	store = store.ReduceRemote(frontendstate.ThreadsReplaced{
		State: frontendstate.DataReady, Threads: threads,
	})
	store = store.ReduceRemote(frontendstate.MessagesAppended{
		State: frontendstate.DataReady, Messages: nil,
	})
	store = store.ReduceRemote(frontendstate.GraphReplaced{
		State: frontendstate.DataReady, Nodes: graph,
	})
	store = store.ReduceRemote(frontendstate.CostChanged{
		Label: "$" + strconv.FormatFloat(0.42, 'f', 2, 64),
	})
	return store
}
