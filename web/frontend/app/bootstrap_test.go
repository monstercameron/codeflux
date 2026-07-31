package app_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/app"
	"codeflux.dev/codeflux/web/frontend/routes"
	"codeflux.dev/codeflux/web/frontend/state"
)

type fakeSubscription struct{ closed bool }

func (s *fakeSubscription) Close() error { s.closed = true; return nil }

type fakeTransport struct {
	steps        []string
	health       app.Health
	versions     app.VersionSet
	initial      app.InitialState
	sessionErr   error
	subscribeErr error
	subscription *fakeSubscription
}

func (f *fakeTransport) Health(context.Context) (app.Health, error) {
	f.steps = append(f.steps, "health")
	return f.health, nil
}
func (f *fakeTransport) Versions(context.Context) (app.VersionSet, error) {
	f.steps = append(f.steps, "versions")
	return f.versions, nil
}
func (f *fakeTransport) EstablishSession(context.Context) error {
	f.steps = append(f.steps, "session")
	return f.sessionErr
}
func (f *fakeTransport) InitialState(context.Context) (app.InitialState, error) {
	f.steps = append(f.steps, "initial")
	return f.initial, nil
}
func (f *fakeTransport) Subscribe(context.Context) (app.Subscription, error) {
	f.steps = append(f.steps, "subscribe")
	return f.subscription, f.subscribeErr
}

type fakePreferences struct{ restored app.StoredPreferences }

func (f fakePreferences) Load(context.Context) (app.StoredPreferences, error) { return f.restored, nil }
func (f fakePreferences) Save(context.Context, app.StoredPreferences) error   { return nil }

func TestBootstrapOrderAndSafeRestoration(t *testing.T) {
	repositoryID, _ := domain.NewRepositoryID()
	threadID, _ := domain.NewThreadID()
	path, _ := routes.Path(routes.Route{
		Name: routes.ThreadWorkspace, RepositoryID: repositoryID, ThreadID: threadID,
	})
	versions := app.VersionSet{App: "1", API: "2", Schema: "3", Frontend: "4"}
	subscription := &fakeSubscription{}
	transport := &fakeTransport{
		health:   app.Health{CoordinatorReady: true, DatabaseReady: true},
		versions: versions,
		initial: app.InitialState{
			FirstRunComplete:       true,
			AccessibleRepositories: map[string]bool{repositoryID.String(): true},
			AccessibleThreads:      map[string]bool{threadID.String(): true},
		},
		subscription: subscription,
	}
	result, err := (app.Bootstrapper{
		Transport: transport, ClientVersions: versions,
		Preferences: fakePreferences{restored: app.StoredPreferences{
			LastRoute: path, Layout: state.LayoutPreferences{RailWidth: 9999, SplitPercent: 50},
		}},
	}).Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(transport.steps, []string{"health", "versions", "session", "initial", "subscribe"}) {
		t.Fatalf("startup order = %v", transport.steps)
	}
	if result.Session.Bootstrap != state.BootstrapReady || result.Route.Reason != routes.RestoreAccepted {
		t.Fatalf("result = %+v", result)
	}
	if result.Layout.RailWidth != 480 {
		t.Fatalf("restored layout was not bounded: %+v", result.Layout)
	}
	if err := result.Close(); err != nil || !subscription.closed {
		t.Fatalf("Close() error = %v, closed = %v", err, subscription.closed)
	}
}

func TestBootstrapExplicitFailureStatesStopBeforeApplicationData(t *testing.T) {
	versions := app.VersionSet{App: "1", API: "2", Schema: "3", Frontend: "4"}
	tests := []struct {
		name      string
		transport *fakeTransport
		wantState state.BootstrapState
		wantErr   error
		wantSteps []string
	}{
		{
			name:      "database",
			transport: &fakeTransport{health: app.Health{CoordinatorReady: true}},
			wantState: state.BootstrapDatabaseUnavailable, wantErr: app.ErrDatabaseUnavailable,
			wantSteps: []string{"health"},
		},
		{
			name: "version",
			transport: &fakeTransport{
				health:   app.Health{CoordinatorReady: true, DatabaseReady: true},
				versions: app.VersionSet{App: "different"},
			},
			wantState: state.BootstrapIncompatible, wantErr: app.ErrIncompatible,
			wantSteps: []string{"health", "versions"},
		},
		{
			name: "unauthorized",
			transport: &fakeTransport{
				health:   app.Health{CoordinatorReady: true, DatabaseReady: true},
				versions: versions, sessionErr: app.ErrUnauthorized,
			},
			wantState: state.BootstrapUnauthorized, wantErr: app.ErrUnauthorized,
			wantSteps: []string{"health", "versions", "session"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := (app.Bootstrapper{
				Transport: test.transport, ClientVersions: versions,
			}).Start(context.Background())
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want %v", err, test.wantErr)
			}
			if result.Session.Bootstrap != test.wantState {
				t.Fatalf("state = %q, want %q", result.Session.Bootstrap, test.wantState)
			}
			if !slices.Equal(test.transport.steps, test.wantSteps) {
				t.Fatalf("steps = %v, want %v", test.transport.steps, test.wantSteps)
			}
		})
	}
}

func TestSubscribeFailureKeepsReadyAppInRecoveringMode(t *testing.T) {
	versions := app.VersionSet{App: "1", API: "2", Schema: "3", Frontend: "4"}
	result, err := (app.Bootstrapper{
		ClientVersions: versions,
		Transport: &fakeTransport{
			health:       app.Health{CoordinatorReady: true, DatabaseReady: true},
			versions:     versions,
			initial:      app.InitialState{FirstRunComplete: true},
			subscribeErr: errors.New("offline"),
		},
	}).Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Session.Bootstrap != state.BootstrapReady || result.Session.Connection != state.ConnectionDegraded {
		t.Fatalf("result session = %+v", result.Session)
	}
}
