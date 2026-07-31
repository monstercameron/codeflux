package shell_test

import (
	"strings"
	"testing"

	"codeflux.dev/codeflux/web/frontend/design"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"codeflux.dev/codeflux/web/frontend/routes"
	"codeflux.dev/codeflux/web/frontend/shell"
	"codeflux.dev/codeflux/web/frontend/state"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type renderProbe map[string][]uint64

func (p renderProbe) Rendered(boundary string, revision uint64) {
	p[boundary] = append(p[boundary], revision)
}

func render(t *testing.T, node ui.Node) string {
	t.Helper()
	markup, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func tokens(t *testing.T) design.Tokens {
	t.Helper()
	value, err := design.TokensFor(design.Options{
		Theme: design.ThemeDark, Density: design.DensityComfortable,
	})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func readySnapshot() state.Snapshot {
	store := state.NewStore(state.NewSnapshot(
		[]state.ThreadView{{ID: "t1", Title: "Implement shell", Status: "running"}},
		[]state.MessageView{{ID: "m1", Role: "assistant", Body: "Working"}},
		[]state.GraphNodeView{{ID: "n1", Title: "Bootstrap", Status: "passed"}},
	))
	store = store.ReduceRemote(state.SessionChanged{Session: state.SessionView{
		Bootstrap: state.BootstrapReady, Connection: state.ConnectionLive,
	}})
	store = store.ReduceRemote(state.WorkspaceChanged{Workspace: state.WorkspaceView{
		RepositoryName: "codeflux", Branch: "main",
	}})
	store = store.ReduceRemote(state.ThreadsReplaced{
		State:   state.DataReady,
		Threads: []state.ThreadView{{ID: "t1", Title: "Implement shell", Status: "running"}},
	})
	store = store.ReduceRemote(state.MessagesAppended{
		State:    state.DataReady,
		Messages: []state.MessageView{{ID: "m2", Role: "user", Body: "Continue"}},
	})
	store = store.ReduceRemote(state.GraphReplaced{
		State: state.DataReady,
		Nodes: []state.GraphNodeView{{ID: "n1", Title: "Bootstrap", Status: "passed"}},
	})
	return store.Snapshot()
}

func TestAppShellRendersIndependentLandmarksAndBoundaries(t *testing.T) {
	probe := renderProbe{}
	markup := render(t, ui.CreateElement(shell.AppRoot, shell.RootProps{
		Snapshot: readySnapshot(),
		Route:    routes.Route{Name: routes.ThreadWorkspace},
		Tokens:   tokens(t),
		Probe:    probe,
	}))
	for _, want := range []string{
		`data-component="app-shell"`,
		`data-testid="app-root"`,
		`data-responsive-mode="wide"`,
		`data-horizontal-overflow="false"`,
		`data-testid="skip-link"`,
		`data-component="application-bar"`,
		`data-component="thread-rail"`,
		`data-component="conversation-pane"`,
		`data-component="graph-pane"`,
		`aria-label="Threads"`,
		`aria-label="Conversation"`,
		`aria-label="Task graph"`,
		`href="#main-content"`,
		`aria-live="polite"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("markup missing %q:\n%s", want, markup)
		}
	}
	for _, boundary := range []string{"application-bar", "thread-rail", "conversation-pane", "graph-pane"} {
		if len(probe[boundary]) != 1 {
			t.Errorf("render probe count for %s = %d", boundary, len(probe[boundary]))
		}
	}
}

func TestTopLevelStatesDoNotRenderAuthenticatedShell(t *testing.T) {
	snapshot := state.NewSnapshot(nil, nil, nil)
	store := state.NewStore(snapshot).ReduceRemote(state.SessionChanged{
		Session: state.SessionView{
			Bootstrap: state.BootstrapUnauthorized, Connection: state.ConnectionUnauthorized,
			Message: "Sign in again",
		},
	})
	markup := render(t, ui.CreateElement(shell.AppRoot, shell.RootProps{
		Snapshot: store.Snapshot(), Tokens: tokens(t),
	}))
	if !strings.Contains(markup, `data-state="unauthorized"`) {
		t.Fatalf("missing unauthorized state: %s", markup)
	}
	if strings.Contains(markup, `data-component="app-shell"`) {
		t.Fatalf("authenticated shell leaked into unauthorized state: %s", markup)
	}
}

func TestEveryAsyncPaneStateHasVisibleSemantics(t *testing.T) {
	states := []state.DataState{
		state.DataNotRequested, state.DataLoading, state.DataReadyEmpty, state.DataReady,
		state.DataPartialStale, state.DataRecoverableError, state.DataDenied,
		state.DataIncompatible, state.DataDisconnected,
	}
	for _, dataState := range states {
		t.Run(string(dataState), func(t *testing.T) {
			markup := render(t, ui.CreateElement(shell.ConversationPane, shell.ConversationPaneProps{
				State: dataState,
			}))
			if !strings.Contains(markup, `data-state="`+string(dataState)+`"`) {
				t.Fatalf("state not exposed in markup: %s", markup)
			}
			if strings.TrimSpace(markup) == "" {
				t.Fatal("state rendered no content")
			}
		})
	}
}

func TestConversationTimelineUsesExplicitCompactCardSpacing(t *testing.T) {
	markup := render(t, ui.CreateElement(shell.ConversationPane, shell.ConversationPaneProps{
		State: state.DataReady,
		Messages: []state.MessageView{
			{ID: "first", Role: "agent", Body: "First", Sequence: 1},
			{ID: "second", Role: "agent", Body: "Second", Sequence: 2},
		},
		Mode: primitives.Mode{Theme: design.ThemeDark, Density: design.DensityComfortable},
	}))
	for _, want := range []string{`data-component="timeline-card-stack"`, `data-gap="8px"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("timeline stack missing %q: %s", want, markup)
		}
	}
}

func TestEveryDataOwningPaneHasEmptyErrorAndDisconnectedPresentation(t *testing.T) {
	for _, dataState := range []state.DataState{
		state.DataReadyEmpty, state.DataRecoverableError, state.DataDisconnected,
	} {
		nodes := []ui.Node{
			ui.CreateElement(shell.ThreadRail, shell.ThreadRailProps{State: dataState}),
			ui.CreateElement(shell.ConversationPane, shell.ConversationPaneProps{State: dataState}),
			ui.CreateElement(shell.GraphPane, shell.GraphPaneProps{State: dataState}),
		}
		for index, node := range nodes {
			markup := render(t, node)
			if !strings.Contains(markup, `data-state="`+string(dataState)+`"`) {
				t.Errorf("pane %d missing %s state: %s", index, dataState, markup)
			}
		}
	}
}

func TestResponsiveLayoutAndCollapseStateAreExplicit(t *testing.T) {
	snapshot := readySnapshot()
	store := state.NewStore(snapshot)
	updated, err := store.ReduceUI(state.LayoutChanged{Preferences: state.LayoutPreferences{
		Viewport: state.ViewportNarrow, ActivePane: state.PaneGraph,
		RailCollapsed: true, GraphCollapsed: true, SplitPercent: 70,
	}})
	if err != nil {
		t.Fatal(err)
	}
	markup := render(t, ui.CreateElement(shell.TaskWorkspaceShell, shell.TaskWorkspaceProps{
		Snapshot: updated.Snapshot(), Tokens: tokens(t),
	}))
	for _, want := range []string{
		`data-viewport="compact"`, `data-active-pane="graph"`,
		`data-rail-collapsed="true"`, `data-graph-collapsed="true"`,
		`data-split-percent="70"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("markup missing %q: %s", want, markup)
		}
	}
}

func TestEveryRouteRendersInEveryBootstrapState(t *testing.T) {
	routeNames := []routes.Name{
		routes.RepositoryChooser, routes.ThreadWorkspace, routes.Memory,
		routes.Settings, routes.Diagnostics, routes.FirstRun,
	}
	bootstrapStates := []state.BootstrapState{
		state.BootstrapBooting, state.BootstrapReady, state.BootstrapIncompatible,
		state.BootstrapUnauthorized, state.BootstrapCoordinatorUnavailable,
		state.BootstrapDatabaseUnavailable,
	}
	for _, routeName := range routeNames {
		for _, bootstrapState := range bootstrapStates {
			t.Run(string(routeName)+"/"+string(bootstrapState), func(t *testing.T) {
				store := state.NewStore(readySnapshot()).ReduceRemote(state.SessionChanged{
					Session: state.SessionView{
						Bootstrap: bootstrapState, Connection: state.ConnectionDisconnected,
						Message: "Safe recovery guidance",
					},
				})
				markup := render(t, ui.CreateElement(shell.AppRoot, shell.RootProps{
					Snapshot: store.Snapshot(),
					Route:    routes.Route{Name: routeName},
					Tokens:   tokens(t),
				}))
				if strings.Count(markup, "<main") != 1 {
					t.Fatalf("main landmarks = %d: %s", strings.Count(markup, "<main"), markup)
				}
				if strings.Count(markup, "<h1") != 1 {
					t.Fatalf("h1 count = %d: %s", strings.Count(markup, "<h1"), markup)
				}
			})
		}
	}
}

func TestResponsiveBrowserContracts(t *testing.T) {
	modes := []state.ViewportClass{
		state.ViewportWide, state.ViewportMedium, state.ViewportNarrow, state.ViewportMinimum,
	}
	for _, mode := range modes {
		t.Run(string(mode), func(t *testing.T) {
			store := state.NewStore(readySnapshot())
			updated, err := store.ReduceUI(state.LayoutChanged{Preferences: state.LayoutPreferences{
				Viewport: mode, ActivePane: state.PaneConversation, SplitPercent: 64,
			}})
			if err != nil {
				t.Fatal(err)
			}
			markup := render(t, ui.CreateElement(shell.AppRoot, shell.RootProps{
				Snapshot: updated.Snapshot(), Route: routes.Route{Name: routes.ThreadWorkspace},
				Tokens: tokens(t),
			}))
			for _, want := range []string{
				`data-responsive-mode="` + string(mode) + `"`,
				`data-horizontal-overflow="false"`,
				`data-testid="skip-link"`,
			} {
				if !strings.Contains(markup, want) {
					t.Errorf("markup missing %q: %s", want, markup)
				}
			}
		})
	}
}

func TestShellsDoNotEmbedExternalNetworkRequests(t *testing.T) {
	for _, routeName := range []routes.Name{
		routes.RepositoryChooser, routes.ThreadWorkspace, routes.Memory,
		routes.Settings, routes.Diagnostics, routes.FirstRun,
	} {
		markup := render(t, ui.CreateElement(shell.AppRoot, shell.RootProps{
			Snapshot: readySnapshot(), Route: routes.Route{Name: routeName}, Tokens: tokens(t),
		}))
		// The standard SVG namespace is declarative metadata, not a fetch URL.
		requestMarkup := strings.ReplaceAll(
			markup,
			`xmlns="http://www.w3.org/2000/svg"`,
			"",
		)
		if strings.Contains(requestMarkup, "http://") || strings.Contains(requestMarkup, "https://") ||
			strings.Contains(markup, `<script`) {
			t.Fatalf("%s shell embeds an external request: %s", routeName, markup)
		}
	}
}

func TestDiagnosticsDistinguishesDurableTerminology(t *testing.T) {
	markup := render(t, ui.CreateElement(shell.DiagnosticsShell, shell.SimpleRouteProps{
		Title: "Diagnostics", State: state.DataReady,
	}))
	for _, term := range []string{
		"Thread", "Task", "Attempt", "Plan revision", "Approval", "Checkpoint", "Recovery",
	} {
		if !strings.Contains(markup, term) {
			t.Errorf("diagnostics terminology missing %q: %s", term, markup)
		}
	}
}

func TestDiagnosticsReportsKnownZeroAndGapRepairWithoutTaskContent(t *testing.T) {
	markup := render(t, ui.CreateElement(shell.DiagnosticsInteractiveShell, shell.DiagnosticsProps{
		SimpleRouteProps: shell.SimpleRouteProps{Title: "Diagnostics", State: state.DataReady},
		Diagnostics: state.DiagnosticsView{
			State: state.DataReady, LastAppliedSequenceKnown: true,
			SessionGapRepairRequired: true,
		},
	}))
	for _, want := range []string{
		`data-component="durable-session-sequence"`,
		`data-sequence="0"`,
		`data-sequence-known="true"`,
		`data-replay-active="false"`,
		`data-live="false"`,
		`data-gap-repair-required="true"`,
		"Last successfully applied sequence",
		"Gap repair required",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("diagnostics sequence missing %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, "message body") || strings.Contains(markup, "task prompt") {
		t.Fatalf("diagnostics leaked task content: %s", markup)
	}
}

func TestDiagnosticsReportsUnknownSequenceDistinctFromZero(t *testing.T) {
	markup := render(t, ui.CreateElement(shell.DiagnosticsInteractiveShell, shell.DiagnosticsProps{
		SimpleRouteProps: shell.SimpleRouteProps{Title: "Diagnostics", State: state.DataReady},
		Diagnostics:      state.DiagnosticsView{State: state.DataReady},
	}))
	if !strings.Contains(markup, `data-sequence="Unknown"`) ||
		!strings.Contains(markup, `data-sequence-known="false"`) ||
		strings.Contains(markup, `data-sequence="0"`) {
		t.Fatalf("unknown diagnostics sequence = %s", markup)
	}
}

func TestDiagnosticsRouteUsesSnapshotDurableSequence(t *testing.T) {
	store := state.NewStore(readySnapshot()).ReduceRemote(state.DiagnosticsChanged{
		Diagnostics: state.DiagnosticsView{
			State: state.DataReady, LastAppliedSequenceKnown: true,
			LastAppliedSequence: 27, SessionLive: true,
		},
	})
	markup := render(t, ui.CreateElement(shell.RouteShell, shell.RouteShellProps{
		Snapshot: store.Snapshot(), Route: routes.Route{Name: routes.Diagnostics}, Tokens: tokens(t),
	}))
	for _, want := range []string{
		`data-component="diagnostics-shell"`,
		`data-component="durable-session-sequence"`,
		`data-sequence="27"`,
		`data-live="true"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("diagnostics route missing %q: %s", want, markup)
		}
	}
}

func TestDataOwningRouteShellsExposeEveryAsyncState(t *testing.T) {
	states := []state.DataState{
		state.DataNotRequested,
		state.DataLoading,
		state.DataReadyEmpty,
		state.DataReady,
		state.DataPartialStale,
		state.DataRecoverableError,
		state.DataDenied,
		state.DataIncompatible,
		state.DataDisconnected,
	}
	for _, dataState := range states {
		t.Run(string(dataState), func(t *testing.T) {
			markup := render(t, ui.CreateElement(shell.SettingsShell, shell.SimpleRouteProps{
				Title: "Settings", State: dataState,
			}))
			if !strings.Contains(markup, `data-state="`+string(dataState)+`"`) {
				t.Fatalf("settings shell omitted %s state: %s", dataState, markup)
			}
		})
	}
}

func TestRouteShellsHaveDedicatedMainLandmarks(t *testing.T) {
	tests := []struct {
		route routes.Name
		want  string
	}{
		{routes.RepositoryChooser, "repository-chooser-shell"},
		{routes.Memory, "memory-shell"},
		{routes.Settings, "settings-shell"},
		{routes.Diagnostics, "diagnostics-shell"},
		{routes.FirstRun, "first-run-shell"},
	}
	for _, test := range tests {
		t.Run(string(test.route), func(t *testing.T) {
			markup := render(t, ui.CreateElement(shell.RouteShell, shell.RouteShellProps{
				Route: routes.Route{Name: test.route}, Tokens: tokens(t),
			}))
			if !strings.Contains(markup, `data-component="`+test.want+`"`) ||
				!strings.Contains(markup, `<main`) {
				t.Fatalf("route shell markup = %s", markup)
			}
		})
	}
}

func TestFirstRunDeclaresSingleRouteScrollOwner(t *testing.T) {
	markup := render(t, ui.CreateElement(shell.FirstRunShell, shell.SimpleRouteProps{
		Title: "Welcome to CodeFlux", State: state.DataReady,
	}))
	for _, want := range []string{
		`data-component="first-run-scroll-owner"`,
		`data-scroll-owner="route"`,
		`data-component="first-run-shell"`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("first-run scroll contract missing %q: %s", want, markup)
		}
	}
	if count := strings.Count(markup, `data-scroll-owner="route"`); count != 1 {
		t.Fatalf("first-run route scroll owners = %d, want 1: %s", count, markup)
	}
}
