package storage

import (
	"context"
	"errors"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
)

// insertProviderFixture creates one provider row directly, because provider
// registration belongs to the accounting path rather than to the settings
// reads under test here.
func insertProviderFixture(
	t *testing.T,
	database *Database,
	displayName string,
	enabled int,
) domain.ProviderID {
	t.Helper()
	providerID, err := domain.NewProviderID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.sql.ExecContext(
		context.Background(),
		`INSERT INTO providers (
			id, display_name, provider_type, enabled,
			created_at_unix_micros, updated_at_unix_micros, revision
		) VALUES (?, ?, 'openai', ?, 1, 1, 3)`,
		providerID, displayName, enabled,
	); err != nil {
		t.Fatal(err)
	}
	return providerID
}

func insertCatalogModelFixture(
	t *testing.T,
	database *Database,
	providerID domain.ProviderID,
	id string,
	name string,
	revision string,
) {
	t.Helper()
	if _, err := database.sql.ExecContext(
		context.Background(),
		`INSERT INTO model_catalog (
			id, provider_id, model_name, model_revision, context_tokens,
			maximum_output_tokens, tool_calling, structured_output, streaming,
			image_input, prompt_caching, reasoning,
			created_at_unix_micros, updated_at_unix_micros
		) VALUES (?, ?, ?, ?, 400000, 100000, 1, 1, 1, 0, 1, 1, 1, 1)`,
		id, providerID, name, revision,
	); err != nil {
		t.Fatal(err)
	}
}

func TestConfiguredProvidersReportWhetherACredentialIsBound(t *testing.T) {
	ctx := context.Background()
	database := openMigratedSchema(t)
	repositories, err := NewRepositories(database, nil)
	if err != nil {
		t.Fatal(err)
	}
	// The unconfigured provider is created second and named first
	// alphabetically, so the ordering assertion cannot pass by insertion order.
	bound := insertProviderFixture(t, database, "OpenAI", 1)
	unbound := insertProviderFixture(t, database, "Anthropic", 0)
	if _, err := repositories.BindProviderCredentialReference(ctx, BindProviderCredentialReference{
		ProviderID:      bound,
		OpaqueReference: "os://codeflux/openai",
	}); err != nil {
		t.Fatal(err)
	}

	listed, err := repositories.ListConfiguredProviders(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 {
		t.Fatalf("want two providers, got %d", len(listed))
	}
	if listed[0].ID != unbound || listed[1].ID != bound {
		t.Fatal("providers must be ordered by display name")
	}
	// A provider with no credential is exactly the row a settings page must
	// still show, so its absence from the list would be the defect.
	if listed[0].HasCredential() || listed[0].Enabled {
		t.Fatalf("unconfigured provider reported as usable: %+v", listed[0])
	}
	if !listed[1].HasCredential() ||
		listed[1].CredentialReference != "os://codeflux/openai" ||
		!listed[1].Enabled || listed[1].Revision != 3 {
		t.Fatalf("configured provider lost a field: %+v", listed[1])
	}
}

func TestTheModelCatalogIsOrderedAndBounded(t *testing.T) {
	ctx := context.Background()
	database := openMigratedSchema(t)
	repositories, err := NewRepositories(database, nil)
	if err != nil {
		t.Fatal(err)
	}
	providerID := insertProviderFixture(t, database, "OpenAI", 1)
	insertCatalogModelFixture(t, database, providerID, "mdl-2", "gpt-5.6-sol", "2026-05")
	insertCatalogModelFixture(t, database, providerID, "mdl-1", "gpt-5.6-mini", "2026-05")

	listed, err := repositories.ListModelCatalog(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 {
		t.Fatalf("want two models, got %d", len(listed))
	}
	if listed[0].ModelName != "gpt-5.6-mini" || listed[1].ModelName != "gpt-5.6-sol" {
		t.Fatal("models must be ordered by name within a provider")
	}
	if listed[0].ProviderID != providerID || listed[0].ModelRevision != "2026-05" ||
		listed[0].ContextTokens != 400000 || !listed[0].Reasoning ||
		listed[0].ID != "mdl-1" {
		t.Fatalf("model lost a field: %+v", listed[0])
	}

	// An unbounded list is one that grows with use. The caller's bound is
	// honoured, and a caller asking for more than the maximum gets the maximum.
	bounded, err := repositories.ListModelCatalog(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(bounded) != 1 {
		t.Fatalf("want one bounded row, got %d", len(bounded))
	}
}

func TestTheLatestUserSettingsRevisionIsTheOneInForce(t *testing.T) {
	ctx := context.Background()
	database := openMigratedSchema(t)
	repositories, err := NewRepositories(database, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Absence is reported as absence rather than as an empty configuration,
	// because the two lead a caller to different conclusions about whose value
	// is in force.
	if _, err := repositories.GetLatestSettingsRevisionForScope(
		ctx, SettingsScopeUser, nil,
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound for an unwritten layer, got %v", err)
	}

	first, err := repositories.CreateSettingsRevision(ctx, CreateSettingsRevision{
		Scope:             SettingsScopeUser,
		ConfigurationJSON: `{"policy_preset":"fast"}`,
		IdempotencyKey:    "user-settings-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := repositories.CreateSettingsRevision(ctx, CreateSettingsRevision{
		Scope:             SettingsScopeUser,
		ConfigurationJSON: `{"policy_preset":"correctness"}`,
		IdempotencyKey:    "user-settings-2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Revision <= first.Revision {
		t.Fatal("a later revision must be higher numbered")
	}

	latest, err := repositories.GetLatestSettingsRevisionForScope(ctx, SettingsScopeUser, nil)
	if err != nil {
		t.Fatal(err)
	}
	if latest.Revision != second.Revision ||
		latest.ConfigurationJSON != `{"policy_preset":"correctness"}` {
		t.Fatalf("want the newest user layer, got %+v", latest)
	}
	// The earlier revision is history a task can still be bound to, so it must
	// remain readable rather than having been replaced.
	earlier, err := repositories.GetSettingsRevision(ctx, first.Revision)
	if err != nil || earlier.ConfigurationJSON != `{"policy_preset":"fast"}` {
		t.Fatalf("earlier revision = %+v, %v", earlier, err)
	}
}
