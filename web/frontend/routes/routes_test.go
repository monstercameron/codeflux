package routes_test

import (
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
