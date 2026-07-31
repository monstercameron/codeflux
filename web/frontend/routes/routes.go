// Package routes defines CodeFlux's typed route map and authorization-safe
// restoration policy independently from browser history.
package routes

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"codeflux.dev/codeflux/internal/domain"
)

type Name string

const (
	RepositoryChooser Name = "repository-chooser"
	ThreadWorkspace   Name = "thread-workspace"
	Memory            Name = "memory"
	Settings          Name = "settings"
	Diagnostics       Name = "diagnostics"
	FirstRun          Name = "first-run"
	NotFound          Name = "not-found"
)

// Route is a parsed, typed navigation destination.
type Route struct {
	Name         Name
	RepositoryID domain.RepositoryID
	ThreadID     domain.ThreadID
}

// Spec is public route-map documentation suitable for router registration.
type Spec struct {
	Name    Name
	Pattern string
}

// Map returns the complete stable route map.
func Map() []Spec {
	return []Spec{
		{Name: RepositoryChooser, Pattern: "/"},
		{Name: ThreadWorkspace, Pattern: "/workspace/{repository_id}/thread/{thread_id}"},
		{Name: Memory, Pattern: "/workspace/{repository_id}/memory"},
		{Name: Settings, Pattern: "/settings"},
		{Name: Diagnostics, Pattern: "/diagnostics"},
		{Name: FirstRun, Pattern: "/first-run"},
	}
}

var ErrInvalidRoute = errors.New("frontend routes: invalid route")

// Parse parses only path state. Query and fragment values never participate in
// authorization or route identity.
func Parse(raw string) (Route, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return Route{}, fmt.Errorf("%w: %v", ErrInvalidRoute, err)
	}
	path := strings.TrimSuffix(parsed.EscapedPath(), "/")
	if path == "" {
		path = "/"
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	switch {
	case path == "/":
		return Route{Name: RepositoryChooser}, nil
	case path == "/settings":
		return Route{Name: Settings}, nil
	case path == "/diagnostics":
		return Route{Name: Diagnostics}, nil
	case path == "/first-run":
		return Route{Name: FirstRun}, nil
	case len(parts) == 3 && parts[0] == "workspace" && parts[2] == "memory":
		repositoryID, parseErr := parseRepositoryID(parts[1])
		if parseErr != nil {
			return Route{}, parseErr
		}
		return Route{Name: Memory, RepositoryID: repositoryID}, nil
	case len(parts) == 4 && parts[0] == "workspace" && parts[2] == "thread":
		repositoryID, parseErr := parseRepositoryID(parts[1])
		if parseErr != nil {
			return Route{}, parseErr
		}
		threadID, parseErr := domain.ParseThreadID(parts[3])
		if parseErr != nil {
			return Route{}, fmt.Errorf("%w: thread id: %v", ErrInvalidRoute, parseErr)
		}
		return Route{Name: ThreadWorkspace, RepositoryID: repositoryID, ThreadID: threadID}, nil
	default:
		return Route{Name: NotFound}, fmt.Errorf("%w: %q", ErrInvalidRoute, parsed.Path)
	}
}

func parseRepositoryID(raw string) (domain.RepositoryID, error) {
	repositoryID, err := domain.ParseRepositoryID(raw)
	if err != nil {
		return domain.RepositoryID{}, fmt.Errorf("%w: repository id: %v", ErrInvalidRoute, err)
	}
	return repositoryID, nil
}

// Path returns the canonical path for a typed route.
func Path(route Route) (string, error) {
	switch route.Name {
	case RepositoryChooser:
		return "/", nil
	case Settings:
		return "/settings", nil
	case Diagnostics:
		return "/diagnostics", nil
	case FirstRun:
		return "/first-run", nil
	case Memory:
		if route.RepositoryID.IsZero() {
			return "", fmt.Errorf("%w: memory requires repository id", ErrInvalidRoute)
		}
		return "/workspace/" + route.RepositoryID.String() + "/memory", nil
	case ThreadWorkspace:
		if route.RepositoryID.IsZero() || route.ThreadID.IsZero() {
			return "", fmt.Errorf("%w: workspace requires repository and thread ids", ErrInvalidRoute)
		}
		return "/workspace/" + route.RepositoryID.String() + "/thread/" + route.ThreadID.String(), nil
	default:
		return "", fmt.Errorf("%w: unknown route %q", ErrInvalidRoute, route.Name)
	}
}

// RestorationContext contains only server-confirmed access information.
type RestorationContext struct {
	Authenticated          bool
	Compatible             bool
	FirstRunComplete       bool
	CoordinatorAvailable   bool
	AccessibleRepositories map[string]bool
	AccessibleThreads      map[string]bool
	ArchivedThreads        map[string]bool
}

type RestoreReason string

const (
	RestoreAccepted               RestoreReason = "accepted"
	RestoreFirstRun               RestoreReason = "first-run"
	RestoreCoordinatorUnavailable RestoreReason = "coordinator-unavailable"
	RestoreUnauthorized           RestoreReason = "unauthorized"
	RestoreIncompatible           RestoreReason = "incompatible"
	RestoreRepositoryUnavailable  RestoreReason = "repository-unavailable"
	RestoreThreadUnavailable      RestoreReason = "thread-unavailable"
	RestoreThreadArchived         RestoreReason = "thread-archived"
	RestoreMalformed              RestoreReason = "malformed"
)

type Restoration struct {
	Route  Route
	Reason RestoreReason
}

// Restore accepts a stored path only after current server authorization and
// compatibility are established. A stored path never grants access.
func Restore(raw string, context RestorationContext) Restoration {
	if !context.CoordinatorAvailable {
		return Restoration{Route: Route{Name: Diagnostics}, Reason: RestoreCoordinatorUnavailable}
	}
	if !context.Compatible {
		return Restoration{Route: Route{Name: Diagnostics}, Reason: RestoreIncompatible}
	}
	if !context.Authenticated {
		return Restoration{Route: Route{Name: RepositoryChooser}, Reason: RestoreUnauthorized}
	}
	if !context.FirstRunComplete {
		return Restoration{Route: Route{Name: FirstRun}, Reason: RestoreFirstRun}
	}
	route, err := Parse(raw)
	if err != nil {
		return Restoration{Route: Route{Name: RepositoryChooser}, Reason: RestoreMalformed}
	}
	switch route.Name {
	case Memory, ThreadWorkspace:
		if !context.AccessibleRepositories[route.RepositoryID.String()] {
			return Restoration{Route: Route{Name: RepositoryChooser}, Reason: RestoreRepositoryUnavailable}
		}
	}
	if route.Name == ThreadWorkspace {
		threadID := route.ThreadID.String()
		if context.ArchivedThreads[threadID] {
			return Restoration{Route: Route{Name: RepositoryChooser}, Reason: RestoreThreadArchived}
		}
		if !context.AccessibleThreads[threadID] {
			return Restoration{Route: Route{Name: RepositoryChooser}, Reason: RestoreThreadUnavailable}
		}
	}
	return Restoration{Route: route, Reason: RestoreAccepted}
}
