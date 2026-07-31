package shell_test

import (
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"codeflux.dev/codeflux/web/frontend/primitives"
	"codeflux.dev/codeflux/web/frontend/routes"
	"codeflux.dev/codeflux/web/frontend/shell"
	"codeflux.dev/codeflux/web/frontend/state"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestPassiveRouteShellsHaveNoFetchOrNetworkAuthority(t *testing.T) {
	wantFields := map[string]reflect.Type{
		"Title": reflect.TypeFor[string](),
		"State": reflect.TypeFor[state.DataState](),
		"Mode":  reflect.TypeFor[primitives.Mode](),
	}
	propsType := reflect.TypeFor[shell.SimpleRouteProps]()
	if propsType.NumField() != len(wantFields) {
		t.Fatalf("SimpleRouteProps exposes %d fields, want only title, state, and mode", propsType.NumField())
	}
	for name, wantType := range wantFields {
		field, ok := propsType.FieldByName(name)
		if !ok || field.Type != wantType {
			t.Fatalf("SimpleRouteProps.%s = %v, want %v", name, field.Type, wantType)
		}
	}

	var passiveRoutes = map[string]func(shell.SimpleRouteProps) ui.Node{
		"settings":    shell.SettingsShell,
		"memory":      shell.MemoryShell,
		"diagnostics": shell.DiagnosticsShell,
		"first-run":   shell.FirstRunShell,
	}
	for name, component := range passiveRoutes {
		markup := render(t, component(shell.SimpleRouteProps{
			Title: name, State: state.DataReady,
		}))
		if strings.Contains(markup, "http://") || strings.Contains(markup, "https://") ||
			strings.Contains(markup, "<script") {
			t.Fatalf("%s route emitted network-capable markup: %s", name, markup)
		}
	}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fileSet := token.NewFileSet()
	for _, entry := range entries {
		filename := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(filename, ".go") ||
			strings.HasSuffix(filename, "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(fileSet, filename, nil, parser.ImportsOnly)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", filename, parseErr)
		}
		for _, imported := range file.Imports {
			path, unquoteErr := strconv.Unquote(imported.Path.Value)
			if unquoteErr != nil {
				t.Fatalf("%s has malformed import %s: %v", filename, imported.Path.Value, unquoteErr)
			}
			if shellNetworkAuthorityImport(path) {
				t.Errorf("passive shell package imports network authority %q in %s", path, filename)
			}
		}
	}
}

func shellNetworkAuthorityImport(path string) bool {
	path = strings.ToLower(strings.TrimSpace(path))
	return path == "net/http" || path == "net/rpc" || path == "syscall/js" ||
		strings.Contains(path, "/fetch") || strings.Contains(path, "grpc") ||
		strings.Contains(path, "websocket") || strings.Contains(path, "/sessionclient")
}

func TestEveryDataOwningShellPresentsEveryDataState(t *testing.T) {
	components := map[string]func(state.DataState) ui.Node{
		"repository chooser": func(dataState state.DataState) ui.Node {
			return shell.RepositoryChooserShell(shell.RepositoryChooserProps{State: dataState})
		},
		"thread rail": func(dataState state.DataState) ui.Node {
			return shell.ThreadRail(shell.ThreadRailProps{
				State:   dataState,
				Threads: []state.ThreadView{{ID: "thread-fixture", Title: "Thread fixture"}},
			})
		},
		"conversation": func(dataState state.DataState) ui.Node {
			return shell.ConversationPane(shell.ConversationPaneProps{
				State:    dataState,
				Messages: []state.MessageView{{ID: "message-fixture", Role: "user", Body: "Message fixture"}},
			})
		},
		"graph": func(dataState state.DataState) ui.Node {
			return shell.GraphPane(shell.GraphPaneProps{
				State: dataState,
				Nodes: []state.GraphNodeView{{ID: "node-fixture", Title: "Graph node fixture", Status: "active"}},
			})
		},
		"memory": func(dataState state.DataState) ui.Node {
			return shell.MemoryShell(shell.SimpleRouteProps{Title: "Memory", State: dataState})
		},
		"settings": func(dataState state.DataState) ui.Node {
			return shell.SettingsShell(shell.SimpleRouteProps{Title: "Settings", State: dataState})
		},
		"diagnostics": func(dataState state.DataState) ui.Node {
			return shell.DiagnosticsShell(shell.SimpleRouteProps{Title: "Diagnostics", State: dataState})
		},
		"first-run": func(dataState state.DataState) ui.Node {
			return shell.FirstRunShell(shell.SimpleRouteProps{Title: "Welcome to CodeFlux", State: dataState})
		},
	}
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
	for name, component := range components {
		for _, dataState := range states {
			t.Run(name+"/"+string(dataState), func(t *testing.T) {
				markup := render(t, component(dataState))
				if !hasExplicitDataStatePresentation(markup, dataState) {
					t.Fatalf("%s has no explicit %s presentation: %s", name, dataState, markup)
				}
			})
		}
	}
}

func hasExplicitDataStatePresentation(markup string, dataState state.DataState) bool {
	if strings.Contains(markup, `data-state="`+string(dataState)+`"`) {
		return true
	}
	markers := map[state.DataState][]string{
		state.DataNotRequested:     {"Not requested"},
		state.DataLoading:          {`aria-busy="true"`, "Loading"},
		state.DataReadyEmpty:       {`data-state="empty"`, "There is nothing to show yet."},
		state.DataReady:            {"0 recent workspaces"},
		state.DataPartialStale:     {"updates are delayed"},
		state.DataRecoverableError: {`data-state="error"`, "Retry"},
		state.DataDenied:           {"Access denied"},
		state.DataIncompatible:     {"Update required"},
		state.DataDisconnected:     {"offline; reconnecting"},
	}
	for _, marker := range markers[dataState] {
		if !strings.Contains(markup, marker) {
			return false
		}
	}
	return len(markers[dataState]) > 0
}

func TestWholeShellDistinguishesDurableProductTerminology(t *testing.T) {
	workspace := render(t, ui.CreateElement(shell.AppRoot, shell.RootProps{
		Snapshot: readySnapshot(), Route: routes.Route{Name: routes.ThreadWorkspace}, Tokens: tokens(t),
	}))
	for _, want := range []string{
		`aria-label="Threads"`,
		`aria-label="Task summary"`,
		"Conversation",
	} {
		if !strings.Contains(workspace, want) {
			t.Errorf("workspace does not distinguish Thread and Task ownership with %q", want)
		}
	}

	diagnosticsStore := state.NewStore(readySnapshot()).ReduceRemote(state.DiagnosticsChanged{
		Diagnostics: state.DiagnosticsView{State: state.DataReady},
	})
	diagnostics := render(t, ui.CreateElement(shell.AppRoot, shell.RootProps{
		Snapshot: diagnosticsStore.Snapshot(), Route: routes.Route{Name: routes.Diagnostics}, Tokens: tokens(t),
	}))
	definitions := []string{
		"A Thread contains conversation.",
		"A Task is durable work.",
		"An Attempt is one execution.",
		"A Plan revision changes the approach.",
		"An Approval authorizes a gated action.",
		"A Checkpoint is restorable state.",
		"Recovery resumes safely.",
	}
	for _, definition := range definitions {
		if !strings.Contains(diagnostics, definition) {
			t.Errorf("whole diagnostics shell is missing terminology distinction %q", definition)
		}
	}
}
