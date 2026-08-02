// Package routes defines CodeFlux's typed route map and authorization-safe
// restoration policy independently from browser history.
package routes

import (
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	"codeflux.dev/codeflux/internal/domain"
)

type Name string

const (
	RepositoryChooser Name = "repository-chooser"
	ThreadWorkspace   Name = "thread-workspace"
	Graphs            Name = "graphs"
	Memory            Name = "memory"
	Code              Name = "code"
	Atoms             Name = "atoms"
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
		{Name: RepositoryChooser, Pattern: "/repositories"},
		{Name: ThreadWorkspace, Pattern: "/workspace/{repository_id}/thread/{thread_id}"},
		{Name: Graphs, Pattern: "/graphs"},
		{Name: Memory, Pattern: "/workspace/{repository_id}/memory"},
		// Code is the repository's own collection: its packages, its
		// declarations, and the documentation each one carries about itself.
		{Name: Code, Pattern: "/workspace/{repository_id}/code"},
		// Atoms is the collection read atom-first: every declaration admitted
		// as an atom across every package, with the documentation it carries.
		// The code route answers "what is in this package"; this one answers
		// "what can be called, and what does it promise".
		{Name: Atoms, Pattern: "/workspace/{repository_id}/atoms"},
		{Name: Settings, Pattern: "/settings"},
		{Name: Diagnostics, Pattern: "/diagnostics"},
		{Name: FirstRun, Pattern: "/first-run"},
	}
}

var ErrInvalidRoute = errors.New("frontend routes: invalid route")

const (
	taskSelectionQueryKey = "selection"
	taskEventQueryKey     = "event"
	taskFileQueryKey      = "file"
)

// TaskDetailSelection is non-authority UI state layered over an independently
// authorized thread selection. Event and file are mutually exclusive.
type TaskDetailSelection struct {
	Route      Route
	EventID    *domain.EventID
	ReviewFile string
}

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
	case path == "/", path == "/repositories":
		// The front door and the chooser are the same view. Which one a person
		// actually lands on is decided by Restore, because "/" should put
		// somebody into the work they already have open rather than asking them
		// to pick a repository they picked days ago.
		return Route{Name: RepositoryChooser}, nil
	case path == "/settings":
		return Route{Name: Settings}, nil
	case path == "/graphs":
		return Route{Name: Graphs}, nil
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
	case len(parts) == 3 && parts[0] == "workspace" && parts[2] == "code":
		repositoryID, parseErr := parseRepositoryID(parts[1])
		if parseErr != nil {
			return Route{}, parseErr
		}
		return Route{Name: Code, RepositoryID: repositoryID}, nil
	case len(parts) == 3 && parts[0] == "workspace" && parts[2] == "atoms":
		repositoryID, parseErr := parseRepositoryID(parts[1])
		if parseErr != nil {
			return Route{}, parseErr
		}
		return Route{Name: Atoms, RepositoryID: repositoryID}, nil
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
	case Graphs:
		return "/graphs", nil
	case Diagnostics:
		return "/diagnostics", nil
	case FirstRun:
		return "/first-run", nil
	case Memory:
		if route.RepositoryID.IsZero() {
			return "", fmt.Errorf("%w: memory requires repository id", ErrInvalidRoute)
		}
		return "/workspace/" + route.RepositoryID.String() + "/memory", nil
	case Code:
		if route.RepositoryID.IsZero() {
			return "", fmt.Errorf("%w: code requires repository id", ErrInvalidRoute)
		}
		return "/workspace/" + route.RepositoryID.String() + "/code", nil
	case Atoms:
		if route.RepositoryID.IsZero() {
			return "", fmt.Errorf("%w: atoms requires repository id", ErrInvalidRoute)
		}
		return "/workspace/" + route.RepositoryID.String() + "/atoms", nil
	case ThreadWorkspace:
		if route.RepositoryID.IsZero() || route.ThreadID.IsZero() {
			return "", fmt.Errorf("%w: workspace requires repository and thread ids", ErrInvalidRoute)
		}
		return "/workspace/" + route.RepositoryID.String() + "/thread/" + route.ThreadID.String(), nil
	default:
		return "", fmt.Errorf("%w: unknown route %q", ErrInvalidRoute, route.Name)
	}
}

// TaskSelectionPath encodes a canonical thread-workspace path as non-authority
// UI state for the local /tasks surface. Callers must still restore the result
// only against independently authorized, loaded thread rows.
func TaskSelectionPath(route Route) (string, error) {
	return TaskDetailSelectionPath(TaskDetailSelection{Route: route})
}

// TaskEventSelectionPath addresses one durable related event without changing
// the independently authorized thread selection.
func TaskEventSelectionPath(route Route, eventID domain.EventID) (string, error) {
	if eventID.IsZero() {
		return "", fmt.Errorf("%w: task event selection requires an event id", ErrInvalidRoute)
	}
	return TaskDetailSelectionPath(TaskDetailSelection{Route: route, EventID: &eventID})
}

// TaskFileSelectionPath addresses one safe workspace-relative review file.
func TaskFileSelectionPath(route Route, file string) (string, error) {
	file, err := ValidateWorkspaceRelativePath(file)
	if err != nil {
		return "", err
	}
	return TaskDetailSelectionPath(TaskDetailSelection{Route: route, ReviewFile: file})
}

// TaskDetailSelectionPath encodes typed detail state for the local /tasks UI.
func TaskDetailSelectionPath(selection TaskDetailSelection) (string, error) {
	route := selection.Route
	if route.Name != ThreadWorkspace {
		return "", fmt.Errorf("%w: task selection requires a thread workspace", ErrInvalidRoute)
	}
	canonical, err := Path(route)
	if err != nil {
		return "", err
	}
	values := url.Values{}
	values.Set(taskSelectionQueryKey, canonical)
	if selection.EventID != nil && strings.TrimSpace(selection.ReviewFile) != "" {
		return "", fmt.Errorf("%w: task detail selects both event and file", ErrInvalidRoute)
	}
	if selection.EventID != nil {
		if selection.EventID.IsZero() {
			return "", fmt.Errorf("%w: task event selection requires an event id", ErrInvalidRoute)
		}
		values.Set(taskEventQueryKey, selection.EventID.String())
	}
	if strings.TrimSpace(selection.ReviewFile) != "" {
		file, validateErr := ValidateWorkspaceRelativePath(selection.ReviewFile)
		if validateErr != nil {
			return "", validateErr
		}
		values.Set(taskFileQueryKey, file)
	}
	return "/tasks?" + values.Encode(), nil
}

// ParseTaskSelection parses the non-authority /tasks query through the same
// typed route parser used for canonical deep links.
func ParseTaskSelection(rawQuery string) (Route, error) {
	selection, err := ParseTaskDetailSelection(rawQuery)
	if err != nil {
		return Route{}, err
	}
	return selection.Route, nil
}

// ParseTaskDetailSelection parses typed non-authority task detail state.
func ParseTaskDetailSelection(rawQuery string) (TaskDetailSelection, error) {
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return TaskDetailSelection{}, fmt.Errorf("%w: task selection query: %v", ErrInvalidRoute, err)
	}
	raw := values.Get(taskSelectionQueryKey)
	if raw == "" {
		return TaskDetailSelection{}, fmt.Errorf("%w: task selection is missing", ErrInvalidRoute)
	}
	route, err := Parse(raw)
	if err != nil || route.Name != ThreadWorkspace {
		return TaskDetailSelection{}, fmt.Errorf("%w: task selection", ErrInvalidRoute)
	}
	eventValue := strings.TrimSpace(values.Get(taskEventQueryKey))
	fileValue := strings.TrimSpace(values.Get(taskFileQueryKey))
	if eventValue != "" && fileValue != "" {
		return TaskDetailSelection{}, fmt.Errorf("%w: task detail selects both event and file", ErrInvalidRoute)
	}
	selection := TaskDetailSelection{Route: route}
	if eventValue != "" {
		eventID, parseErr := domain.ParseEventID(eventValue)
		if parseErr != nil {
			return TaskDetailSelection{}, fmt.Errorf("%w: task event selection", ErrInvalidRoute)
		}
		selection.EventID = &eventID
	}
	if fileValue != "" {
		file, validateErr := ValidateWorkspaceRelativePath(fileValue)
		if validateErr != nil {
			return TaskDetailSelection{}, validateErr
		}
		selection.ReviewFile = file
	}
	return selection, nil
}

// ValidateWorkspaceRelativePath returns a normalized identity only when raw
// already names a canonical workspace-relative path. Browser route state must
// never become an authority to escape the selected workspace.
func ValidateWorkspaceRelativePath(raw string) (string, error) {
	if raw == "" || strings.TrimSpace(raw) != raw || !utf8.ValidString(raw) || strings.Contains(raw, "\\") ||
		strings.HasPrefix(raw, "/") || strings.Contains(raw, ":") {
		return "", fmt.Errorf("%w: unsafe workspace-relative file", ErrInvalidRoute)
	}
	for _, value := range raw {
		if unicode.IsControl(value) {
			return "", fmt.Errorf("%w: unsafe workspace-relative file", ErrInvalidRoute)
		}
	}
	cleaned := path.Clean(raw)
	if cleaned == "." || cleaned == ".." || cleaned != raw || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("%w: unsafe workspace-relative file", ErrInvalidRoute)
	}
	return cleaned, nil
}

// RestorationContext contains only server-confirmed access information.
type RestorationContext struct {
	Authenticated        bool
	Compatible           bool
	FirstRunComplete     bool
	CoordinatorAvailable bool
	// OpenRepositoryID and OpenThreadID are the conversation the coordinator
	// says is open. They are what lets the front door land in the work instead
	// of on a chooser.
	OpenRepositoryID       domain.RepositoryID
	OpenThreadID           domain.ThreadID
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

// isFrontDoor reports whether a path is the bare entry point.
//
// Only "/" redirects. Somebody who asks for "/repositories" is asking for the
// chooser and gets it, so the rail item that goes there is not a control that
// bounces you somewhere else.
func isFrontDoor(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	path := strings.TrimSuffix(parsed.EscapedPath(), "/")
	return path == ""
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
	if err == nil && route.Name == RepositoryChooser && isFrontDoor(raw) &&
		!context.OpenRepositoryID.IsZero() && !context.OpenThreadID.IsZero() {
		// The front door opens onto the work, not onto a chooser. Landing on a
		// repository picker that reads "Disconnected" — because no conversation
		// is selected, so no session is streaming — made a working product look
		// dead, with the open thread hidden behind one faint row.
		return Restoration{
			Route: Route{
				Name:         ThreadWorkspace,
				RepositoryID: context.OpenRepositoryID,
				ThreadID:     context.OpenThreadID,
			},
			Reason: RestoreAccepted,
		}
	}
	if err != nil {
		return Restoration{Route: Route{Name: RepositoryChooser}, Reason: RestoreMalformed}
	}
	switch route.Name {
	case Memory, Code, Atoms, ThreadWorkspace:
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

// IsApplicationPath reports whether a path is one this application owns.
//
// It exists so the server that decides which paths receive the document and
// the client that decides what to render read the same table. They were two
// hand-maintained lists that disagreed: the server allowed /tasks and /memory,
// which are not routes, and refused /graphs, which is — so the navigation rail
// sent a person to a server 404.
func IsApplicationPath(path string) bool {
	if isUnscopedEntryPath(path) {
		return true
	}
	route, err := Parse(path)
	return err == nil && route.Name != NotFound
}

// isUnscopedEntryPath reports the paths the browser application routes without
// a repository in them.
//
// The client offers "/tasks" and "/memory" as entry points and resolves each to
// a scoped route once a repository is open, but the canonical forms carry the
// repository, so Parse refuses the bare ones. The server answered a person who
// reloaded or deep-linked either of them with a plain-text 404 from its own
// router — the interface never loaded at all. Serving the document lets the
// application do what it already knows how to do with them.
func isUnscopedEntryPath(path string) bool {
	switch strings.TrimSuffix(path, "/") {
	case "/tasks", "/memory", "/atoms", "/code":
		return true
	default:
		return false
	}
}
