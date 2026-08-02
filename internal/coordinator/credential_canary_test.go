package coordinator

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/credentials"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// auditCanarySecret is the value that must never appear at rest.
//
// It is deliberately unlike any real key so a hit is unambiguous, and it is
// assembled rather than written as one literal so this file does not itself
// become the match the scan reports.
var auditCanarySecret = strings.Join(
	[]string{"codeflux", "audit006", "canary", "must", "never", "persist"}, "-",
)

// TestAUDIT006_AResolvedProviderCredentialReachesNothingAtRest covers
// AUDIT-006, reconciling M04-G01.
//
// M04-G01 recorded that a provider can be configured and tested without
// writing its secret to SQLite. Nothing exercised it: the credential store was
// constructed inside StartApplication with no seam, so no test could put a
// known value on the resolving side of the boundary, and the gate was argued
// from the shape of the code rather than from a scan.
//
// This resolves a real canary through the credential source the coordinator
// actually uses, then reads the database, its write-ahead log, its shared
// memory file, and every recorded event back as bytes.
func TestAUDIT006_AResolvedProviderCredentialReachesNothingAtRest(t *testing.T) {
	root := t.TempDir()
	databasePath := filepath.Join(root, "codeflux.sqlite3")

	reference, err := credentials.NewReference("openai", "audit006")
	if err != nil {
		t.Fatal(err)
	}
	store, err := credentials.NewEnvironmentStore(
		map[credentials.Reference]string{reference: "CODEFLUX_AUDIT006_KEY"},
		func(name string) (string, bool) {
			if name == "CODEFLUX_AUDIT006_KEY" {
				return auditCanarySecret, true
			}
			return "", false
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	application, err := StartApplication(t.Context(), ApplicationOptions{
		DatabasePath:    databasePath,
		BackupDirectory: filepath.Join(root, "backups"),
		ListenAddress:   "127.0.0.1:0", TaskListenAddress: "127.0.0.1:0",
		TaskControls:    &applicationTaskControlStub{},
		CredentialStore: store,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = application.Shutdown(context.Background()) })

	registered, err := application.repos.EnsureProviderRegistration(
		t.Context(),
		storage.EnsureProviderRegistration{
			DisplayName: "OpenAI", ProviderType: "openai",
			AdapterName: "openai-responses", AdapterVersion: "1",
			ProviderVersion:  "responses-v1",
			EndpointRedacted: "https://api.openai.example/v1",
			CapabilitiesJSON: `{"streaming":true}`,
			ModelIdentifier:  "gpt-5.6-sol", ModelVersion: "2026-05",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.repos.BindProviderCredentialReference(
		t.Context(),
		storage.BindProviderCredentialReference{
			ProviderID:      registered.ProviderID,
			OpaqueReference: "os://openai/audit006",
		},
	); err != nil {
		t.Fatalf("bind credential reference: %v", err)
	}

	// Resolve it the way the product does. If this does not observe the
	// canary, the scan below proves nothing, so the resolution is asserted
	// before the absence is.
	source, err := NewProviderCredentialSource(store, application.repos)
	if err != nil {
		t.Fatal(err)
	}
	observed := false
	if err := source.Use(t.Context(), registered.ProviderID, func(secret []byte) error {
		if string(secret) == auditCanarySecret {
			observed = true
		}
		return nil
	}); err != nil {
		t.Fatalf("resolve provider credential: %v", err)
	}
	if !observed {
		t.Fatal("the credential source never produced the canary; the scan below would be vacuous")
	}

	// Exercise the configured provider over the real service, which is the
	// path that would persist a secret if anything did.
	exerciseProviderOverTheGeneratedClient(t, application, registered.ProviderID)

	// Close so WAL content is checkpointed into the files being scanned.
	if err := application.Shutdown(context.Background()); err != nil {
		t.Fatalf("shut down: %v", err)
	}
	assertCanaryAbsentAtRest(t, databasePath)
}

// exerciseProviderOverTheGeneratedClient configures and tests the provider
// through the real settings service.
func exerciseProviderOverTheGeneratedClient(
	t *testing.T,
	application *Application,
	providerID domain.ProviderID,
) {
	t.Helper()
	connection, err := grpc.NewClient(
		application.TaskControlAddress(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	ctx := metadata.AppendToOutgoingContext(
		t.Context(), transport.SessionMetadataKey, application.BrowserSessionSecret(),
	)
	identity, err := transport.ProviderIDToProto(providerID)
	if err != nil {
		t.Fatal(err)
	}
	client := codefluxv1.NewSettingsServiceClient(connection)
	if _, err := client.TestProvider(ctx, &codefluxv1.TestProviderRequest{
		ProviderId: identity,
	}); err != nil {
		t.Fatalf("test provider: %v", err)
	}
}

// assertCanaryAbsentAtRest reads the database and its sidecars as bytes.
//
// Reading files rather than querying tables is the point: a query only sees
// the columns someone thought to look at, while a byte scan also catches a
// secret in a free-text column, a JSON blob, an index page, or a stale page
// the vacuum has not reclaimed.
func assertCanaryAbsentAtRest(t *testing.T, databasePath string) {
	t.Helper()
	needle := []byte(auditCanarySecret)
	scanned := 0
	for _, path := range []string{
		databasePath,
		databasePath + "-wal",
		databasePath + "-shm",
	} {
		content, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			t.Fatalf("read %s: %v", path, err)
		}
		scanned++
		if bytes.Contains(content, needle) {
			t.Errorf("the canary secret is present in %s", filepath.Base(path))
		}
	}
	if scanned == 0 {
		t.Fatal("no database file was scanned; the assertion is vacuous")
	}
}
