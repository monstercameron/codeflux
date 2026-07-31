package routes_test

import (
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/routes"
)

func TestRouteMapRoundTrip(t *testing.T) {
	repositoryID, err := domain.NewRepositoryID()
	if err != nil {
		t.Fatal(err)
	}
	threadID, err := domain.NewThreadID()
	if err != nil {
		t.Fatal(err)
	}
	want := routes.Route{
		Name: routes.ThreadWorkspace, RepositoryID: repositoryID, ThreadID: threadID,
	}
	path, err := routes.Path(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := routes.Parse(path + "?ignored=yes#ignored")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != want.Name || got.RepositoryID != want.RepositoryID || got.ThreadID != want.ThreadID {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
}

func TestTopLevelGraphRouteRoundTrips(t *testing.T) {
	t.Parallel()
	path, err := routes.Path(routes.Route{Name: routes.Graphs})
	if err != nil || path != "/graphs" {
		t.Fatalf("graph path = %q, %v", path, err)
	}
	parsed, err := routes.Parse(path + "?focus=current")
	if err != nil || parsed.Name != routes.Graphs {
		t.Fatalf("graph route = %+v, %v", parsed, err)
	}
}

func TestThreadPathParametersRejectMalformedOrEncodedSeparators(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		"/workspace/not-a-repository/thread/not-a-thread",
		"/workspace/repo_%2Fescape/thread/thr_%2Fescape",
		"/workspace//thread/",
	} {
		if _, err := routes.Parse(raw); err == nil {
			t.Fatalf("malformed path parameters accepted: %q", raw)
		}
	}
}

func TestTaskSelectionQueryRoundTripsOnlyTypedThreadRoutes(t *testing.T) {
	repositoryID, _ := domain.NewRepositoryID()
	threadID, _ := domain.NewThreadID()
	want := routes.Route{Name: routes.ThreadWorkspace, RepositoryID: repositoryID, ThreadID: threadID}
	path, err := routes.TaskSelectionPath(want)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(path, "/tasks?selection=") {
		t.Fatalf("task selection path = %q", path)
	}
	query := strings.TrimPrefix(path, "/tasks?")
	got, err := routes.ParseTaskSelection(query)
	if err != nil || got != want {
		t.Fatalf("task selection = %+v, %v; want %+v", got, err, want)
	}
	if _, err := routes.TaskSelectionPath(routes.Route{Name: routes.Settings}); err == nil {
		t.Fatal("non-thread task selection was accepted")
	}
	if _, err := routes.ParseTaskSelection("selection=%2Fsettings"); err == nil {
		t.Fatal("non-thread selection query was accepted")
	}
}

func TestTaskDetailSelectionRoundTripsTypedEventAndFile(t *testing.T) {
	repositoryID, _ := domain.NewRepositoryID()
	threadID, _ := domain.NewThreadID()
	eventID, _ := domain.NewEventID()
	route := routes.Route{Name: routes.ThreadWorkspace, RepositoryID: repositoryID, ThreadID: threadID}

	eventPath, err := routes.TaskEventSelectionPath(route, eventID)
	if err != nil {
		t.Fatal(err)
	}
	eventSelection, err := routes.ParseTaskDetailSelection(strings.TrimPrefix(eventPath, "/tasks?"))
	if err != nil || eventSelection.Route != route || eventSelection.EventID == nil || *eventSelection.EventID != eventID || eventSelection.ReviewFile != "" {
		t.Fatalf("event selection = %+v, %v", eventSelection, err)
	}

	filePath, err := routes.TaskFileSelectionPath(route, "web/frontend/routes/routes.go")
	if err != nil {
		t.Fatal(err)
	}
	fileSelection, err := routes.ParseTaskDetailSelection(strings.TrimPrefix(filePath, "/tasks?"))
	if err != nil || fileSelection.Route != route || fileSelection.EventID != nil || fileSelection.ReviewFile != "web/frontend/routes/routes.go" {
		t.Fatalf("file selection = %+v, %v", fileSelection, err)
	}
	if strings.Contains(eventPath, " ") || strings.Contains(filePath, " ") {
		t.Fatalf("detail paths were not URL encoded: %q %q", eventPath, filePath)
	}
}

func TestTaskDetailSelectionRejectsMissingMalformedAndAmbiguousTargets(t *testing.T) {
	repositoryID, _ := domain.NewRepositoryID()
	threadID, _ := domain.NewThreadID()
	eventID, _ := domain.NewEventID()
	route := routes.Route{Name: routes.ThreadWorkspace, RepositoryID: repositoryID, ThreadID: threadID}
	base, _ := routes.TaskSelectionPath(route)
	query := strings.TrimPrefix(base, "/tasks?")

	for _, suffix := range []string{
		"&event=not-an-event",
		"&event=" + eventID.String() + "&file=README.md",
		"&file=..%2Fsecret",
	} {
		if _, err := routes.ParseTaskDetailSelection(query + suffix); err == nil {
			t.Fatalf("unsafe detail query accepted: %q", suffix)
		}
	}
}

func TestWorkspaceRelativePathRejectsEscapesAndNonCanonicalAliases(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		"", ".", "..", "../secret", "web/../secret", "/etc/passwd",
		`C:\\Windows\\system.ini`, `web\\client\\main.go`, "web//client/main.go",
		" web/client/main.go", "web/client/main.go ", "web/client\x00/main.go",
		"scheme:value",
	} {
		if _, err := routes.ValidateWorkspaceRelativePath(raw); err == nil {
			t.Fatalf("unsafe workspace path accepted: %q", raw)
		}
	}
	for _, raw := range []string{"README.md", "web/client/main.go", ".github/workflows/check.yml"} {
		got, err := routes.ValidateWorkspaceRelativePath(raw)
		if err != nil || got != raw {
			t.Fatalf("safe workspace path = %q, %v; want %q", got, err, raw)
		}
	}
}

func TestRestoreRefusesUnauthorizedOrArchivedDestinations(t *testing.T) {
	repositoryID, _ := domain.NewRepositoryID()
	threadID, _ := domain.NewThreadID()
	path, _ := routes.Path(routes.Route{
		Name: routes.ThreadWorkspace, RepositoryID: repositoryID, ThreadID: threadID,
	})
	base := routes.RestorationContext{
		Authenticated: true, Compatible: true, FirstRunComplete: true, CoordinatorAvailable: true,
		AccessibleRepositories: map[string]bool{repositoryID.String(): true},
		AccessibleThreads:      map[string]bool{threadID.String(): true},
	}

	unauthorized := base
	unauthorized.Authenticated = false
	if got := routes.Restore(path, unauthorized); got.Reason != routes.RestoreUnauthorized || got.Route.Name != routes.RepositoryChooser {
		t.Fatalf("unauthorized restoration = %+v", got)
	}

	archived := base
	archived.ArchivedThreads = map[string]bool{threadID.String(): true}
	if got := routes.Restore(path, archived); got.Reason != routes.RestoreThreadArchived || got.Route.Name != routes.RepositoryChooser {
		t.Fatalf("archived restoration = %+v", got)
	}

	missingRepository := base
	missingRepository.AccessibleRepositories = map[string]bool{}
	if got := routes.Restore(path, missingRepository); got.Reason != routes.RestoreRepositoryUnavailable {
		t.Fatalf("missing repository restoration = %+v", got)
	}
}

func TestRestoreUsesExplicitTopLevelDestinations(t *testing.T) {
	tests := []struct {
		name    string
		context routes.RestorationContext
		want    routes.Name
		reason  routes.RestoreReason
	}{
		{
			name:    "coordinator unavailable",
			context: routes.RestorationContext{Compatible: true, Authenticated: true},
			want:    routes.Diagnostics, reason: routes.RestoreCoordinatorUnavailable,
		},
		{
			name:    "incompatible",
			context: routes.RestorationContext{CoordinatorAvailable: true, Authenticated: true},
			want:    routes.Diagnostics, reason: routes.RestoreIncompatible,
		},
		{
			name: "first run",
			context: routes.RestorationContext{
				CoordinatorAvailable: true, Compatible: true, Authenticated: true,
			},
			want: routes.FirstRun, reason: routes.RestoreFirstRun,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := routes.Restore("/", test.context)
			if got.Route.Name != test.want || got.Reason != test.reason {
				t.Fatalf("Restore = %+v", got)
			}
		})
	}
}
