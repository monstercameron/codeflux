// Package app coordinates safe application startup around narrow transport ports.
package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"codeflux.dev/codeflux/web/frontend/routes"
	"codeflux.dev/codeflux/web/frontend/state"
)

// VersionSet is the compatibility tuple that must match before app data is read.
type VersionSet struct {
	App      string
	API      string
	Schema   string
	Frontend string
}

func (v VersionSet) CompatibleWith(other VersionSet) bool {
	return v.App != "" &&
		v.App == other.App &&
		v.API == other.API &&
		v.Schema == other.Schema &&
		v.Frontend == other.Frontend
}

type Health struct {
	CoordinatorReady bool
	DatabaseReady    bool
}

type InitialState struct {
	FirstRunComplete       bool
	AccessibleRepositories map[string]bool
	AccessibleThreads      map[string]bool
	ArchivedThreads        map[string]bool
}

type StoredPreferences struct {
	LastRoute string
	Layout    state.LayoutPreferences
}

// PreferenceStore is intentionally incapable of storing arbitrary values or
// credentials. It owns only non-sensitive route and layout preferences.
type PreferenceStore interface {
	Load(context.Context) (StoredPreferences, error)
	Save(context.Context, StoredPreferences) error
}

// Subscription represents the reconnectable server event stream.
type Subscription interface{ Close() error }

// BootstrapTransport is implemented by the same-origin coordinator adapter.
// EstablishSession relies on an HttpOnly, SameSite=Strict cookie; no secret is
// exposed to application state.
type BootstrapTransport interface {
	Health(context.Context) (Health, error)
	Versions(context.Context) (VersionSet, error)
	EstablishSession(context.Context) error
	InitialState(context.Context) (InitialState, error)
	Subscribe(context.Context) (Subscription, error)
}

var (
	ErrUnauthorized           = errors.New("frontend app: unauthorized")
	ErrCoordinatorUnavailable = errors.New("frontend app: coordinator unavailable")
	ErrDatabaseUnavailable    = errors.New("frontend app: database unavailable")
	ErrIncompatible           = errors.New("frontend app: incompatible versions")
)

type BootstrapResult struct {
	Session      state.SessionView
	Route        routes.Restoration
	Layout       state.LayoutPreferences
	Subscription Subscription
}

// Bootstrapper owns the bounded startup sequence.
type Bootstrapper struct {
	Transport      BootstrapTransport
	Preferences    PreferenceStore
	ClientVersions VersionSet
	Timeout        time.Duration
}

// Start performs health -> version -> cookie session -> initial state -> route
// restoration -> subscription in a fixed order.
func (b Bootstrapper) Start(parent context.Context) (BootstrapResult, error) {
	if b.Transport == nil {
		return failure(state.BootstrapCoordinatorUnavailable, "Coordinator adapter is unavailable"), ErrCoordinatorUnavailable
	}
	timeout := b.Timeout
	if timeout <= 0 {
		timeout = 12 * time.Second
	}
	context, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	health, err := b.Transport.Health(context)
	if err != nil || !health.CoordinatorReady {
		return failure(state.BootstrapCoordinatorUnavailable, "Coordinator is unavailable"), errors.Join(ErrCoordinatorUnavailable, err)
	}
	if !health.DatabaseReady {
		return failure(state.BootstrapDatabaseUnavailable, "Database is unavailable"), ErrDatabaseUnavailable
	}
	serverVersions, err := b.Transport.Versions(context)
	if err != nil {
		return failure(state.BootstrapCoordinatorUnavailable, "Could not read coordinator versions"), errors.Join(ErrCoordinatorUnavailable, err)
	}
	if !b.ClientVersions.CompatibleWith(serverVersions) {
		return failure(state.BootstrapIncompatible, "CodeFlux components are incompatible"), fmt.Errorf(
			"%w: client=%+v server=%+v", ErrIncompatible, b.ClientVersions, serverVersions,
		)
	}
	if err := b.Transport.EstablishSession(context); err != nil {
		if errors.Is(err, ErrUnauthorized) {
			return failure(state.BootstrapUnauthorized, "This session is not authorized"), err
		}
		return failure(state.BootstrapCoordinatorUnavailable, "Could not establish a session"), errors.Join(ErrCoordinatorUnavailable, err)
	}
	initial, err := b.Transport.InitialState(context)
	if err != nil {
		if errors.Is(err, ErrUnauthorized) {
			return failure(state.BootstrapUnauthorized, "This session has expired"), err
		}
		return failure(state.BootstrapCoordinatorUnavailable, "Could not load application state"), errors.Join(ErrCoordinatorUnavailable, err)
	}
	preferences := StoredPreferences{Layout: state.DefaultLayoutPreferences()}
	if b.Preferences != nil {
		if restored, loadErr := b.Preferences.Load(context); loadErr == nil {
			preferences = restored
		}
	}
	preferences.Layout = preferences.Layout.Normalize()
	restoration := routes.Restore(preferences.LastRoute, routes.RestorationContext{
		Authenticated:          true,
		Compatible:             true,
		FirstRunComplete:       initial.FirstRunComplete,
		CoordinatorAvailable:   true,
		AccessibleRepositories: initial.AccessibleRepositories,
		AccessibleThreads:      initial.AccessibleThreads,
		ArchivedThreads:        initial.ArchivedThreads,
	})
	subscription, err := b.Transport.Subscribe(context)
	if err != nil {
		return BootstrapResult{
			Session: state.SessionView{
				Bootstrap: state.BootstrapReady, Connection: state.ConnectionRecovering,
				Message: "Live updates are reconnecting",
			},
			Route: restoration, Layout: preferences.Layout,
		}, nil
	}
	return BootstrapResult{
		Session: state.SessionView{Bootstrap: state.BootstrapReady, Connection: state.ConnectionLive},
		Route:   restoration, Layout: preferences.Layout, Subscription: subscription,
	}, nil
}

func failure(kind state.BootstrapState, message string) BootstrapResult {
	return BootstrapResult{
		Session: state.SessionView{
			Bootstrap: kind, Connection: state.ConnectionOffline, Message: message,
		},
		Route:  routes.Restoration{Route: routes.Route{Name: routes.Diagnostics}},
		Layout: state.DefaultLayoutPreferences(),
	}
}

// Close releases the live stream. It is safe for result values without a stream.
func (r BootstrapResult) Close() error {
	if r.Subscription == nil {
		return nil
	}
	return r.Subscription.Close()
}
