package coordinator

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/policy"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// TestTheSettingsSurfacesAnswerOverTheGeneratedClient starts the real
// application and asks it what a settings page asks.
//
// The settings service was registered but answered only its telemetry calls,
// so every question a settings page asks about policy, providers, and models
// returned Unimplemented against a coordinator that could answer all three.
func TestTheSettingsSurfacesAnswerOverTheGeneratedClient(t *testing.T) {
	root := t.TempDir()
	application, err := StartApplication(t.Context(), ApplicationOptions{
		DatabasePath:    filepath.Join(root, "codeflux.sqlite3"),
		BackupDirectory: filepath.Join(root, "backups"),
		ListenAddress:   "127.0.0.1:0", TaskListenAddress: "127.0.0.1:0",
		TaskControls: &applicationTaskControlStub{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = application.Shutdown(context.Background()) })
	connection, err := grpc.NewClient(
		application.TaskControlAddress(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	client := codefluxv1.NewSettingsServiceClient(connection)

	// The request contract requires a workspace identity on both reads. The
	// answers are coordinator-wide — one policy governs every run and one
	// provider set is configured per machine — so the identity is carried
	// rather than used to scope them.
	workspaceID, err := domain.NewWorkspaceID()
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := transport.WorkspaceIDToProto(workspaceID)
	if err != nil {
		t.Fatal(err)
	}

	// Settings are session-authenticated like every other browser surface.
	if _, err := client.GetPolicy(t.Context(), &codefluxv1.GetPolicyRequest{
		WorkspaceId: workspace,
	}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("unauthenticated policy read = %v", err)
	}
	ctx := metadata.AppendToOutgoingContext(
		t.Context(), transport.SessionMetadataKey, application.BrowserSessionSecret(),
	)

	policyResponse, err := client.GetPolicy(ctx, &codefluxv1.GetPolicyRequest{
		WorkspaceId: workspace,
	})
	if err != nil {
		t.Fatal(err)
	}
	view := policyResponse.GetPolicy()
	if view.GetPreset() != string(domain.PolicyPresetBalanced) {
		t.Fatalf("preset = %q, want the compiled default a new task is created with", view.GetPreset())
	}
	if view.GetReasoningEffort() != string(policy.FixedBaselineReasoning) {
		t.Fatalf("reasoning = %q, want the frozen baseline's", view.GetReasoningEffort())
	}
	if view.GetRisk() != string(domain.RiskLevelRoutine) ||
		view.GetRequiredAssurance() != string(domain.AssuranceLevelRuntimeOnly) {
		t.Fatalf("policy floors = %+v", view)
	}
	// No user settings layer has been written, so the revision is zero rather
	// than an invented number.
	if view.GetRevision() != 0 {
		t.Fatalf("revision = %d, want zero for compiled defaults", view.GetRevision())
	}

	// A coordinator that has recorded no provider says so. An empty list is a
	// state to act on, not a failure.
	models, err := client.GetModels(ctx, &codefluxv1.GetModelsRequest{
		WorkspaceId: workspace,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(models.GetModels()) != 0 {
		t.Fatalf("want no models before a provider is recorded, got %d", len(models.GetModels()))
	}

	providerID, err := domain.NewProviderID()
	if err != nil {
		t.Fatal(err)
	}
	identity, err := transport.ProviderIDToProto(providerID)
	if err != nil {
		t.Fatal(err)
	}
	// Configuring or testing a provider the coordinator has never recorded is
	// an absence, not an internal failure.
	if _, err := client.ConfigureProvider(ctx, &codefluxv1.ConfigureProviderRequest{
		Control:             &codefluxv1.MutationControl{IdempotencyKey: "settings-configure-1"},
		WorkspaceId:         workspace,
		ProviderId:          identity,
		CredentialReference: "os://codeflux/openai",
		ModelId:             "mdl-1",
	}); status.Code(err) != codes.NotFound {
		t.Fatalf("configure unknown provider = %v, want NotFound", err)
	}
	if _, err := client.TestProvider(ctx, &codefluxv1.TestProviderRequest{
		ProviderId: identity,
	}); status.Code(err) != codes.NotFound {
		t.Fatalf("test unknown provider = %v, want NotFound", err)
	}

	// A recorded provider becomes one row naming it. No model is catalogued
	// for it, because nothing in this product writes the model catalogue yet,
	// so the row reports a provider that cannot be used rather than being
	// omitted and leaving nothing to configure.
	registered, err := application.repos.EnsureProviderRegistration(
		t.Context(),
		storage.EnsureProviderRegistration{
			DisplayName: "OpenAI", ProviderType: "openai",
			AdapterName: "openai-responses", AdapterVersion: "1",
			ProviderVersion: "responses-v1", EndpointRedacted: "https://api.openai.example/v1",
			CapabilitiesJSON: `{"streaming":true}`,
			ModelIdentifier:  "gpt-5.6-sol", ModelVersion: "2026-05",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	models, err = client.GetModels(ctx, &codefluxv1.GetModelsRequest{WorkspaceId: workspace})
	if err != nil {
		t.Fatal(err)
	}
	views := models.GetModels()
	if len(views) != 1 {
		t.Fatalf("want one provider row, got %d: %+v", len(views), views)
	}
	if views[0].GetModelId() != "" || views[0].GetDisplayName().GetValue() != "OpenAI" {
		t.Fatalf("provider row lost a field: %+v", views[0])
	}
	if views[0].GetProviderId().GetValue() != registered.ProviderID.String() {
		t.Fatalf("provider row names %q, want %q",
			views[0].GetProviderId().GetValue(), registered.ProviderID.String())
	}
	// No credential is bound, so the provider is not available. Reporting it as
	// available would tell somebody a run could start.
	if views[0].GetAvailable() {
		t.Fatal("a provider with no bound credential must not be reported as available")
	}

	registeredIdentity, err := transport.ProviderIDToProto(registered.ProviderID)
	if err != nil {
		t.Fatal(err)
	}
	checked, err := client.TestProvider(ctx, &codefluxv1.TestProviderRequest{
		ProviderId: registeredIdentity,
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked.GetReachable() {
		t.Fatal("a provider with no bound credential must not check as resolved")
	}
	// Every summary this check produces says a provider request was not made,
	// whatever the outcome. A local credential lookup reported as a connection
	// test would tell somebody their endpoint, network, and key all work when
	// only the last of the three was looked at.
	if !strings.Contains(checked.GetSummary().GetValue(), "No provider request was made") {
		t.Fatalf("summary omits what was not done: %q", checked.GetSummary().GetValue())
	}

	// The request contract names the model a credential is configured for, and
	// nothing in this product writes the model catalogue yet, so this is
	// refused rather than recorded against a model nobody has evidence for.
	if _, err := client.ConfigureProvider(ctx, &codefluxv1.ConfigureProviderRequest{
		Control:             &codefluxv1.MutationControl{IdempotencyKey: "settings-configure-2"},
		WorkspaceId:         workspace,
		ProviderId:          registeredIdentity,
		CredentialReference: "os://codeflux/openai",
		ModelId:             "gpt-5.6-sol",
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("configure against an uncatalogued model = %v, want InvalidArgument", err)
	}
}

// TestAStoredUserSettingsLayerBeatsTheCompiledDefault exercises the fixed
// precedence with a real database.
//
// Nothing writes the user layer through the settings service, but it is read
// rather than assumed absent: a layer written by any other authorized path
// must be what a settings page reports, and a layer that cannot be read must
// be reported as unreadable rather than as compiled defaults being in force.
func TestAStoredUserSettingsLayerBeatsTheCompiledDefault(t *testing.T) {
	root := t.TempDir()
	application, err := StartApplication(t.Context(), ApplicationOptions{
		DatabasePath:    filepath.Join(root, "codeflux.sqlite3"),
		BackupDirectory: filepath.Join(root, "backups"),
		ListenAddress:   "127.0.0.1:0", TaskListenAddress: "127.0.0.1:0",
		TaskControls: &applicationTaskControlStub{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = application.Shutdown(context.Background()) })

	settings, err := newSettingsApplication(application.repos, application.credentials, nil)
	if err != nil {
		t.Fatal(err)
	}
	// A provider the coordinator never recorded is an absence rather than an
	// internal failure, and a credential check on it makes no provider request.
	providerID, err := domain.NewProviderID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := settings.TestProviderCredential(t.Context(), providerID); !errors.Is(err, transport.ErrProviderNotFound) {
		t.Fatalf("unknown provider = %v, want ErrProviderNotFound", err)
	}

	stored, err := application.repos.CreateSettingsRevision(t.Context(), storage.CreateSettingsRevision{
		Scope:             storage.SettingsScopeUser,
		ConfigurationJSON: `{"policy_preset":"correctness","request_timeout_ms":90000}`,
		IdempotencyKey:    "settings-precedence-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	effective, err := settings.ReadEffectivePolicy(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if effective.Preset != domain.PolicyPresetCorrectness {
		t.Fatalf("preset = %q, want the stored user layer's", effective.Preset)
	}
	if effective.RequestTimeout != 90*time.Second {
		t.Fatalf("request timeout = %v, want the stored user layer's", effective.RequestTimeout)
	}
	if effective.Revision != stored.Revision {
		t.Fatalf("revision = %d, want %d", effective.Revision, stored.Revision)
	}
	// The reasoning effort is not a user setting. A stored layer cannot move
	// it, because routing is one frozen versioned policy through prototype
	// exit.
	if effective.Reasoning != policy.FixedBaselineReasoning {
		t.Fatalf("reasoning = %q, want the frozen baseline's", effective.Reasoning)
	}

	// A layer that cannot be read is not treated as an empty one: reporting
	// compiled defaults would tell a person their settings were in force while
	// the row holding them was unreadable.
	if _, err := application.repos.CreateSettingsRevision(t.Context(), storage.CreateSettingsRevision{
		Scope:             storage.SettingsScopeUser,
		ConfigurationJSON: `{"policy_preset":[]}`,
		IdempotencyKey:    "settings-precedence-2",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := settings.ReadEffectivePolicy(t.Context()); err == nil {
		t.Fatal("an unreadable settings layer must be reported, not replaced by a default")
	}
}
