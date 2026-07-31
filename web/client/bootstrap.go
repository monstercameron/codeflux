package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/routes"
	frontendstate "codeflux.dev/codeflux/web/frontend/state"
)

const defaultBootstrapTimeout = 12 * time.Second

type bootstrapEnvelope struct {
	ApplicationVersion  string                     `json:"application_version"`
	APIVersion          string                     `json:"api_version"`
	SchemaVersion       int                        `json:"schema_version"`
	FrontendVersion     string                     `json:"frontend_version"`
	BridgePath          string                     `json:"bridge_path"`
	SelectedSessionID   *codefluxv1.StableIdentity `json:"selected_session_id,omitempty"`
	SelectedWorkspaceID *codefluxv1.StableIdentity `json:"selected_workspace_id,omitempty"`
	RouteAccess         routeAccessEnvelope        `json:"route_access"`
}

// routeAccessEnvelope contains identifiers only. Repository paths, thread
// contents, credentials, and launch secrets never enter browser persistence.
type routeAccessEnvelope struct {
	FirstRunComplete       bool                         `json:"first_run_complete"`
	AccessibleRepositories []*codefluxv1.StableIdentity `json:"accessible_repositories,omitempty"`
	AccessibleThreads      []*codefluxv1.StableIdentity `json:"accessible_threads,omitempty"`
	ArchivedThreads        []*codefluxv1.StableIdentity `json:"archived_threads,omitempty"`
}

type healthEnvelope struct {
	Status     string `json:"status"`
	Database   string `json:"database"`
	Migrations string `json:"migrations"`
}

type startupFailureKind string

const (
	startupCoordinator startupFailureKind = "coordinator-unavailable"
	startupDatabase    startupFailureKind = "database-unavailable"
	startupMigration   startupFailureKind = "migration-required"
)

type startupFailure struct {
	Kind  startupFailureKind
	Cause error
}

func (failure *startupFailure) Error() string {
	switch failure.Kind {
	case startupDatabase:
		return "the local database is unavailable"
	case startupMigration:
		return "the local database requires migration"
	default:
		return "the local coordinator is unavailable"
	}
}

func (failure *startupFailure) Unwrap() error { return failure.Cause }

type startupHTTPResponse struct {
	Status int
	Body   string
}

type startupRequest func(context.Context, string) (startupHTTPResponse, error)

func loadBootstrapWith(
	parent context.Context,
	timeout time.Duration,
	request startupRequest,
) (bootstrapEnvelope, error) {
	if request == nil {
		return bootstrapEnvelope{}, &startupFailure{Kind: startupCoordinator}
	}
	if timeout <= 0 {
		timeout = defaultBootstrapTimeout
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	healthResponse, err := request(ctx, "/healthz")
	if err != nil {
		return bootstrapEnvelope{}, classifyStartupFailure(err, healthEnvelope{})
	}
	var health healthEnvelope
	if decodeErr := json.Unmarshal([]byte(healthResponse.Body), &health); decodeErr != nil {
		return bootstrapEnvelope{}, &startupFailure{Kind: startupCoordinator, Cause: decodeErr}
	}
	if healthResponse.Status != 200 || !strings.EqualFold(health.Status, "ok") {
		return bootstrapEnvelope{}, classifyStartupFailure(nil, health)
	}
	if !strings.EqualFold(health.Database, "ready") {
		return bootstrapEnvelope{}, &startupFailure{Kind: startupDatabase}
	}
	if !strings.EqualFold(health.Migrations, "current") {
		return bootstrapEnvelope{}, &startupFailure{Kind: startupMigration}
	}

	bootstrapResponse, err := request(ctx, "/bootstrap")
	if err != nil {
		return bootstrapEnvelope{}, classifyStartupFailure(err, healthEnvelope{})
	}
	if bootstrapResponse.Status != 200 {
		return bootstrapEnvelope{}, &startupFailure{Kind: startupCoordinator}
	}
	var envelope bootstrapEnvelope
	if err := json.Unmarshal([]byte(bootstrapResponse.Body), &envelope); err != nil {
		return bootstrapEnvelope{}, &startupFailure{Kind: startupCoordinator, Cause: err}
	}
	return envelope, nil
}

func classifyStartupFailure(cause error, health healthEnvelope) error {
	if errors.Is(cause, context.DeadlineExceeded) || errors.Is(cause, context.Canceled) {
		return &startupFailure{Kind: startupCoordinator, Cause: cause}
	}
	switch strings.ToLower(strings.TrimSpace(health.Migrations)) {
	case "required", "failed", "incompatible":
		return &startupFailure{Kind: startupMigration, Cause: cause}
	}
	switch strings.ToLower(strings.TrimSpace(health.Database)) {
	case "unavailable", "failed", "error":
		return &startupFailure{Kind: startupDatabase, Cause: cause}
	default:
		return &startupFailure{Kind: startupCoordinator, Cause: cause}
	}
}

//lint:ignore U1000 Used by the js/wasm application entry point; host lint does not load that file.
func sessionForStartupFailure(err error) frontendstate.SessionView {
	view := frontendstate.SessionView{
		Bootstrap:  frontendstate.BootstrapCoordinatorUnavailable,
		Connection: frontendstate.ConnectionDisconnected,
		Message:    "The local coordinator could not complete secure startup.",
	}
	var failure *startupFailure
	if !errors.As(err, &failure) {
		return view
	}
	switch failure.Kind {
	case startupDatabase:
		view.Bootstrap = frontendstate.BootstrapDatabaseUnavailable
		view.Message = "The local database is unavailable. Retry or open diagnostics."
	case startupMigration:
		view.Bootstrap = frontendstate.BootstrapDatabaseUnavailable
		view.Message = "The local database requires a compatible migration before CodeFlux can continue."
	}
	return view
}

//lint:ignore U1000 Used by the js/wasm application entry point; host lint does not load that file.
func routeAccessKey(envelope bootstrapEnvelope) string {
	var builder strings.Builder
	builder.WriteString(envelope.ApplicationVersion)
	builder.WriteByte('|')
	builder.WriteString(envelope.APIVersion)
	builder.WriteByte('|')
	builder.WriteString(strconv.Itoa(envelope.SchemaVersion))
	builder.WriteByte('|')
	builder.WriteString(strconv.FormatBool(envelope.RouteAccess.FirstRunComplete))
	if workspaceID := envelope.SelectedWorkspaceID; workspaceID != nil {
		builder.WriteByte('|')
		builder.WriteString(strconv.Itoa(int(workspaceID.GetKind())))
		builder.WriteByte(':')
		builder.WriteString(workspaceID.GetValue())
	}
	for _, identities := range [][]*codefluxv1.StableIdentity{
		envelope.RouteAccess.AccessibleRepositories,
		envelope.RouteAccess.AccessibleThreads,
		envelope.RouteAccess.ArchivedThreads,
	} {
		builder.WriteByte('|')
		for _, identity := range identities {
			if identity == nil {
				builder.WriteString("nil,")
				continue
			}
			builder.WriteString(strconv.Itoa(int(identity.GetKind())))
			builder.WriteByte(':')
			builder.WriteString(identity.GetValue())
			builder.WriteByte(',')
		}
	}
	return builder.String()
}

func restorationContext(
	envelope bootstrapEnvelope,
	compatible bool,
) (routes.RestorationContext, error) {
	context := routes.RestorationContext{
		Authenticated:          true,
		Compatible:             compatible,
		FirstRunComplete:       envelope.RouteAccess.FirstRunComplete,
		CoordinatorAvailable:   true,
		AccessibleRepositories: make(map[string]bool, len(envelope.RouteAccess.AccessibleRepositories)),
		AccessibleThreads:      make(map[string]bool, len(envelope.RouteAccess.AccessibleThreads)),
		ArchivedThreads:        make(map[string]bool, len(envelope.RouteAccess.ArchivedThreads)),
	}
	for _, identity := range envelope.RouteAccess.AccessibleRepositories {
		if identity == nil || identity.GetKind() != codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_REPOSITORY {
			return routes.RestorationContext{}, fmt.Errorf("bootstrap route access: invalid repository identity")
		}
		parsed, err := domain.ParseRepositoryID(identity.GetValue())
		if err != nil {
			return routes.RestorationContext{}, fmt.Errorf("bootstrap route access: invalid repository identity")
		}
		context.AccessibleRepositories[parsed.String()] = true
	}
	for _, identity := range envelope.RouteAccess.AccessibleThreads {
		if err := addThreadAccess(context.AccessibleThreads, identity); err != nil {
			return routes.RestorationContext{}, err
		}
	}
	for _, identity := range envelope.RouteAccess.ArchivedThreads {
		if err := addThreadAccess(context.ArchivedThreads, identity); err != nil {
			return routes.RestorationContext{}, err
		}
	}
	return context, nil
}

func addThreadAccess(target map[string]bool, identity *codefluxv1.StableIdentity) error {
	if identity == nil || identity.GetKind() != codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_THREAD {
		return fmt.Errorf("bootstrap route access: invalid thread identity")
	}
	parsed, err := domain.ParseThreadID(identity.GetValue())
	if err != nil {
		return fmt.Errorf("bootstrap route access: invalid thread identity")
	}
	target[parsed.String()] = true
	return nil
}
