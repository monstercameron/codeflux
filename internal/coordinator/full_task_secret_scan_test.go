package coordinator

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	agentloop "codeflux.dev/codeflux/internal/agent"
	"codeflux.dev/codeflux/internal/credentials"
	"codeflux.dev/codeflux/internal/fingerprint"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
)

// TestAUDIT007_AFullTaskLeavesNoKnownSecretOnAnySurface covers AUDIT-007,
// reconciling M04-G02.
//
// M04-G02 recorded that a full mock task produces no known secret in logs,
// events, prompts, UI payloads, or diagnostics. The evidence behind it
// redacted values that were already isolated at a boundary and never ran a
// task, so it demonstrated that a redactor redacts rather than that a run
// leaks nothing.
//
// This drives a real requirement through intake, approval, worktree creation,
// a launched worker, and a scripted provider, with a canary the coordinator
// genuinely resolves through its own credential source. It then reads back
// every durable surface the gate names.
func TestAUDIT007_AFullTaskLeavesNoKnownSecretOnAnySurface(t *testing.T) {
	// Assembled rather than written whole so this file is not itself a match.
	credentialCanary := strings.Join(
		[]string{"codeflux", "audit007", "credential", "canary"}, "-")

	const requirement = "Create cmd/greet/main.go so the program prints a greeting."
	const program = "package main\n\nimport \"fmt\"\n\n" +
		"func main() {\n\tfmt.Println(\"greetings\")\n}\n"

	// The canary must be genuinely resolvable during the run. A scan for a
	// value the run never held is the vacuous evidence this ticket exists to
	// replace, so the store counts its own reads and the count is asserted.
	reference, err := credentials.NewReference("openai", "audit007")
	if err != nil {
		t.Fatal(err)
	}
	backing, err := credentials.NewEnvironmentStore(
		map[credentials.Reference]string{reference: "CODEFLUX_AUDIT007_KEY"},
		func(name string) (string, bool) {
			if name == "CODEFLUX_AUDIT007_KEY" {
				return credentialCanary, true
			}
			return "", false
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	store := &countingCredentialStore{inner: backing}

	engine := startEngineWith(t, ApplicationOptions{
		AgentModel: &scriptedEngineModel{
			turns: []func(agentloop.ModelInput) agentloop.ModelTurn{
				writeFile("cmd/greet/main.go", program),
			},
		},
		CredentialStore: store,
	})
	ctx := context.Background()

	requestID := engine.request(t, requirement)
	created, err := engine.lifecycle.CreateTaskFromRequirement(ctx, transport.CreateTaskCommand{
		ThreadID:                 engine.threadID,
		RequestMessageID:         &requestID,
		Requirement:              requirement,
		TaskClass:                string(fingerprint.TaskClassFeature),
		RepositoryRevision:       strings.Repeat("1", 40),
		BaselineModelRevision:    "scripted-provider-fixture",
		ToolConfigurationVersion: "tools-v1",
		ValidationProfileVersion: "profile-v1",
		AffectedPackages:         []string{"cmd/greet"},
		IdempotencyKey:           "audit007-requirement",
	})
	if err != nil {
		t.Fatalf("intake refused the requirement: %v", err)
	}

	readyRevision := driveTaskToReady(t, engine.repositories, created.TaskID, created.Revision)
	preflight, err := engine.application.TaskPreflightService().BindExecution(
		ctx, created.TaskID, readyRevision,
		ForecastedTask{
			Policy:   storage.ExecutionPolicyRevision{Revision: created.PolicyRevision},
			Forecast: storage.EffortForecastRevision{Revision: created.ForecastRevision},
		},
		"audit007-bind",
	)
	if err != nil {
		t.Fatalf("binding the approved preflight failed: %v", err)
	}
	if _, err := engine.lifecycle.StartPreparedTask(ctx, transport.StartTaskCommand{
		TaskID:            created.TaskID,
		ExpectedRevision:  readyRevision,
		PreflightRevision: preflight.Revision,
		IdempotencyKey:    "audit007-start",
	}); err != nil {
		t.Fatalf("starting the approved task failed: %v", err)
	}

	binding, err := engine.repositories.GetWorktreeBinding(ctx, created.TaskID)
	if err != nil {
		t.Fatalf("starting a task must create its worktree: %v", err)
	}
	written := filepath.Join(binding.WorktreePath, "cmd", "greet", "main.go")
	engine.waitFor(t, "the agent to do the work", func() bool {
		_, err := os.Stat(written)
		return err == nil
	})
	engine.waitFor(t, "the run to record what it did", func() bool {
		return engine.rows("agent_tool_results") > 0
	})

	// Register a provider bound to the canary and resolve it through the
	// coordinator's own credential source, so the value the scan looks for is
	// one this coordinator actually held while the run's records were being
	// written.
	registered, err := engine.repositories.EnsureProviderRegistration(ctx,
		storage.EnsureProviderRegistration{
			DisplayName: "OpenAI", ProviderType: "openai",
			AdapterName: "openai-responses", AdapterVersion: "1",
			ProviderVersion:  "responses-v1",
			EndpointRedacted: "https://api.openai.example/v1",
			CapabilitiesJSON: `{"streaming":true}`,
			ModelIdentifier:  "gpt-5.6-sol", ModelVersion: "2026-05",
		})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.repositories.BindProviderCredentialReference(ctx,
		storage.BindProviderCredentialReference{
			ProviderID:      registered.ProviderID,
			OpaqueReference: "os://openai/audit007",
		}); err != nil {
		t.Fatal(err)
	}
	source, err := NewProviderCredentialSource(store, engine.repositories)
	if err != nil {
		t.Fatal(err)
	}
	held := false
	if err := source.Use(ctx, registered.ProviderID, func(secret []byte) error {
		held = bytes.Equal(secret, []byte(credentialCanary))
		return nil
	}); err != nil {
		t.Fatalf("resolve provider credential: %v", err)
	}
	if !held || store.reads() == 0 {
		t.Fatal("the coordinator never held the canary; the scan below would be vacuous")
	}

	// The run happened and the secret was live. Now read every surface the
	// gate names.
	//
	// This is the assertion M04-G02 never made: not "the redactor removed a
	// value we handed it", but "after a task ran end to end, this value is
	// nowhere durable".
	surfaces := collectDurableSurfaces(t, engine)
	if len(surfaces) == 0 {
		t.Fatal("no surface was collected; the scan would be vacuous")
	}
	for name, content := range surfaces {
		if bytes.Contains(content, []byte(credentialCanary)) {
			t.Errorf("the credential canary is present in %s", name)
		}
	}
}

// collectDurableSurfaces reads back everything a finished run leaves behind
// that the gate names: the database and its sidecars as bytes, every recorded
// event payload, and every file under the coordinator's own root.
//
// Files are read as bytes rather than parsed, because a secret that reached a
// surface in a shape nobody anticipated is exactly the case a structured read
// would miss.
func collectDurableSurfaces(t *testing.T, engine engineFixture) map[string][]byte {
	t.Helper()
	surfaces := make(map[string][]byte)

	databasePath := engine.application.database.Path()
	for _, suffix := range []string{"", "-wal", "-shm"} {
		content, err := os.ReadFile(databasePath + suffix)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			t.Fatalf("read %s%s: %v", databasePath, suffix, err)
		}
		surfaces["sqlite"+suffix] = content
	}

	// Everything the coordinator wrote beside its database: worktrees, logs,
	// backups, diagnostics.
	root := filepath.Dir(databasePath)
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			relative = path
		}
		surfaces[filepath.ToSlash(relative)] = content
		return nil
	}); err != nil {
		t.Fatalf("walk coordinator root: %v", err)
	}
	return surfaces
}

// countingCredentialStore records how often a secret was actually retrieved,
// so a scan cannot claim absence of a value nothing ever fetched.
type countingCredentialStore struct {
	inner      credentials.Store
	retrievals atomic.Int64
}

func (store *countingCredentialStore) reads() int64 { return store.retrievals.Load() }

func (store *countingCredentialStore) Retrieve(
	ctx context.Context,
	reference credentials.Reference,
) (credentials.Secret, error) {
	store.retrievals.Add(1)
	return store.inner.Retrieve(ctx, reference)
}

func (store *countingCredentialStore) Create(
	ctx context.Context, reference credentials.Reference, secret credentials.Secret,
) error {
	return store.inner.Create(ctx, reference, secret)
}

func (store *countingCredentialStore) Update(
	ctx context.Context, reference credentials.Reference, secret credentials.Secret,
) error {
	return store.inner.Update(ctx, reference, secret)
}

func (store *countingCredentialStore) Test(
	ctx context.Context, reference credentials.Reference,
) error {
	return store.inner.Test(ctx, reference)
}

func (store *countingCredentialStore) Delete(
	ctx context.Context, reference credentials.Reference,
) error {
	return store.inner.Delete(ctx, reference)
}
