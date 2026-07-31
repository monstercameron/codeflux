package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/routes"
)

func TestLoadBootstrapWithBoundsStartupAndReturnsTypedFailures(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		_, err := loadBootstrapWith(t.Context(), 10*time.Millisecond, func(ctx context.Context, _ string) (startupHTTPResponse, error) {
			<-ctx.Done()
			return startupHTTPResponse{}, ctx.Err()
		})
		var failure *startupFailure
		if !errors.As(err, &failure) || failure.Kind != startupCoordinator || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("timeout error = %T %v", err, err)
		}
	})

	for _, test := range []struct {
		name   string
		health string
		kind   startupFailureKind
	}{
		{name: "database", health: `{"status":"error","database":"unavailable","migrations":"current"}`, kind: startupDatabase},
		{name: "migration", health: `{"status":"error","database":"ready","migrations":"required"}`, kind: startupMigration},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := loadBootstrapWith(t.Context(), time.Second, func(context.Context, string) (startupHTTPResponse, error) {
				return startupHTTPResponse{Status: 503, Body: test.health}, nil
			})
			var failure *startupFailure
			if !errors.As(err, &failure) || failure.Kind != test.kind {
				t.Fatalf("failure = %T %v", err, err)
			}
			if test.name == "database" && (containsSensitiveDetail(err.Error())) {
				t.Fatalf("failure exposed sensitive detail: %v", err)
			}
		})
	}
}

func TestLoadBootstrapWithRequiresTypedHealthyCoordinator(t *testing.T) {
	want := bootstrapEnvelope{
		ApplicationVersion: "0.16.0", APIVersion: "codeflux.v1",
		SchemaVersion: 16, FrontendVersion: "m16", BridgePath: "/grpc",
	}
	body, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	calls := []string{}
	got, err := loadBootstrapWith(t.Context(), time.Second, func(_ context.Context, path string) (startupHTTPResponse, error) {
		calls = append(calls, path)
		if path == "/healthz" {
			return startupHTTPResponse{Status: 200, Body: `{"status":"ok","database":"ready","migrations":"current"}`}, nil
		}
		return startupHTTPResponse{Status: 200, Body: string(body)}, nil
	})
	if err != nil || got.ApplicationVersion != want.ApplicationVersion || len(calls) != 2 || calls[0] != "/healthz" || calls[1] != "/bootstrap" {
		t.Fatalf("bootstrap = %#v, calls=%v, err=%v", got, calls, err)
	}
}

func TestRestorationContextUsesOnlyTypedServerConfirmedIdentifiers(t *testing.T) {
	repositoryID, err := domain.ParseRepositoryID("repo_01890f3c-4a00-7abc-8def-0123456789ab")
	if err != nil {
		t.Fatal(err)
	}
	threadID, err := domain.ParseThreadID("thr_01890f3c-4a00-7abc-8def-0123456789ab")
	if err != nil {
		t.Fatal(err)
	}
	envelope := bootstrapEnvelope{RouteAccess: routeAccessEnvelope{
		FirstRunComplete: true,
		AccessibleRepositories: []*codefluxv1.StableIdentity{{
			Kind: codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_REPOSITORY, Value: repositoryID.String(),
		}},
		AccessibleThreads: []*codefluxv1.StableIdentity{{
			Kind: codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_THREAD, Value: threadID.String(),
		}},
	}}
	context, err := restorationContext(envelope, true)
	if err != nil {
		t.Fatal(err)
	}
	path := "/workspace/" + repositoryID.String() + "/thread/" + threadID.String()
	if restored := routes.Restore(path, context); restored.Reason != routes.RestoreAccepted {
		t.Fatalf("confirmed route rejected: %#v", restored)
	}

	envelope.RouteAccess.AccessibleRepositories = nil
	context, err = restorationContext(envelope, true)
	if err != nil {
		t.Fatal(err)
	}
	if restored := routes.Restore(path, context); restored.Reason != routes.RestoreRepositoryUnavailable || restored.Route.Name != routes.RepositoryChooser {
		t.Fatalf("missing repository route = %#v", restored)
	}

	envelope.RouteAccess.AccessibleRepositories = []*codefluxv1.StableIdentity{{
		Kind:  codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_TASK,
		Value: repositoryID.String(),
	}}
	if _, err := restorationContext(envelope, true); err == nil {
		t.Fatal("route context accepted a mismatched identity kind")
	}
}

func containsSensitiveDetail(message string) bool {
	for _, candidate := range []string{".sqlite", "SELECT ", "C:\\", "/Users/"} {
		if strings.Contains(message, candidate) {
			return true
		}
	}
	return false
}
