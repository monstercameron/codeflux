//go:build js && wasm

// Command client is the generated-asset CodeFlux GWC v5 browser application.
package main

import (
	"context"
	"strconv"
	"sync/atomic"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/buildinfo"
	"codeflux.dev/codeflux/web/frontend/design"
	frontendi18n "codeflux.dev/codeflux/web/frontend/i18n"
	"codeflux.dev/codeflux/web/frontend/preferences"
	"codeflux.dev/codeflux/web/frontend/routes"
	"codeflux.dev/codeflux/web/frontend/sessionclient"
	"codeflux.dev/codeflux/web/frontend/shell"
	frontendstate "codeflux.dev/codeflux/web/frontend/state"
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
	location := router.UseLocation()
	bootstrap := fetch.UseResource(loadBootstrap)
	streamStatus := ui.UseState(sessionclient.Status{State: sessionclient.StateIdle})
	reducedMotion := ui.UsePrefersReducedMotion()
	preferredScheme := ui.UsePrefersColorScheme()
	activeTheme, setTheme := ui.UseTheme(string(preferredScheme))
	layoutState := ui.UseState(frontendstate.DefaultLayoutPreferences())
	selectedGraphID := ui.UseState("")
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
	session := frontendstate.SessionView{
		Bootstrap:  frontendstate.BootstrapBooting,
		Connection: frontendstate.ConnectionConnecting,
	}
	var selectedSession *codefluxv1.StableIdentity
	if resource.Ready && resource.Error == nil && compatible(resource.Value) && restoreContextErr == nil {
		selectedSession, _ = selectedSessionIdentity(resource.Value)
	}
	useAuthenticatedSession(selectedSession, resource.Value, streamStatus.Set)
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
				Connection: frontendstate.ConnectionOffline,
				Message:    "The coordinator returned incompatible route access state.",
			}
		} else if compatible(resource.Value) {
			if resource.Value.SelectedSessionID == nil {
				session = frontendstate.SessionView{
					Bootstrap:  frontendstate.BootstrapReady,
					Connection: frontendstate.ConnectionOffline,
					Message:    "Secure startup completed. Select a session to begin live updates.",
				}
			} else if selectedSession == nil {
				session = frontendstate.SessionView{
					Bootstrap:  frontendstate.BootstrapIncompatible,
					Connection: frontendstate.ConnectionOffline,
					Message:    "The selected session identity is incompatible.",
				}
			} else {
				session = sessionViewForLifecycle(streamStatus.Get())
			}
		} else {
			session = frontendstate.SessionView{
				Bootstrap:  frontendstate.BootstrapIncompatible,
				Connection: frontendstate.ConnectionOffline,
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
	appRoute := routeFor(location.Path)
	if location.Path == "/" &&
		resource.Ready &&
		restoreContextErr == nil &&
		!restoreContext.FirstRunComplete {
		// Render the safe first-run destination on the first committed frame.
		// Route restoration may canonicalize the URL afterward, but it must not
		// expose a transient repository chooser for an unfinished installation.
		appRoute = routes.Route{Name: routes.FirstRun}
	}
	root := shell.RootProps{
		Snapshot: store.Snapshot(),
		Route:    appRoute,
		Tokens:   tokens,
		Translator: frontendi18n.EnglishRegistry().Resolve(
			string(frontendi18n.LocaleEnglishUnitedStates),
		),
		OnLayoutChange: layoutState.Set,
		OnGraphSelect:  selectedGraphID.Set,
		OnRetry:        bootstrap.Reload,
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
	case "/tasks", "/memory", "/settings", "/diagnostics", "/first-run":
		return true
	default:
		return false
	}
}

func useAuthenticatedSession(
	selected *codefluxv1.StableIdentity,
	envelope bootstrapEnvelope,
	setStatus func(sessionclient.Status),
) {
	dependency := ""
	if selected != nil {
		dependency = selected.GetValue()
	}
	ui.UseEffectOf(func() func() {
		if dependency == "" {
			return nil
		}
		ctx, cancel := context.WithCancel(context.Background())
		var mounted atomic.Bool
		mounted.Store(true)
		client, err := startAuthenticatedSession(
			ctx,
			envelope,
			sessionclient.BrowserConnector{},
			func(status sessionclient.Status) {
				if mounted.Load() {
					setStatus(status)
				}
			},
			func(context.Context, *codefluxv1.SessionEvent) error {
				// M16 owns authenticated ordered delivery. Event-to-view reducers
				// remain a separate milestone and must not mutate sample shell data.
				return nil
			},
		)
		if err != nil {
			mounted.Store(false)
			cancel()
			setStatus(sessionclient.Status{
				State:   sessionclient.StateFailed,
				Failure: sessionclient.FailureProtocol,
			})
			return nil
		}
		return func() {
			mounted.Store(false)
			cancel()
			_ = client.Close()
		}
	}, dependency)
}

func loadBootstrap(context context.Context) (bootstrapEnvelope, error) {
	return loadBootstrapWith(context, defaultBootstrapTimeout, browserStartupRequest)
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

func routeFor(path string) routes.Route {
	switch path {
	case "/tasks":
		return routes.Route{Name: routes.ThreadWorkspace}
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
