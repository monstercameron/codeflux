package storage

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"

	"codeflux.dev/codeflux/internal/credentials"
	"codeflux.dev/codeflux/internal/domain"
)

func TestProviderCredentialBindingPersistsOnlyOpaqueReference(t *testing.T) {
	ctx := context.Background()
	database := openMigratedSchema(t)
	providerID, err := domain.NewProviderID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.sql.ExecContext(
		ctx,
		`INSERT INTO providers (
			id, display_name, provider_type, enabled,
			created_at_unix_micros, updated_at_unix_micros
		) VALUES (?, 'Provider fixture', 'openai', 1, 1, 1)`,
		providerID,
	); err != nil {
		t.Fatal(err)
	}
	repositories, err := NewRepositories(database, nil)
	if err != nil {
		t.Fatal(err)
	}
	reference, err := credentials.NewReference("openai", "personal")
	if err != nil {
		t.Fatal(err)
	}
	material := []byte("known-provider-material-that-must-never-persist")
	secret, err := credentials.NewSecret(material)
	if err != nil {
		t.Fatal(err)
	}
	if err := secret.Use(func(value []byte) error {
		if !bytes.Equal(value, material) {
			t.Fatal("provider received changed credential material")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	secret.Destroy()
	input := BindProviderCredentialReference{
		ProviderID:      providerID,
		OpaqueReference: reference.Opaque(),
	}
	binding, err := repositories.BindProviderCredentialReference(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	retried, err := repositories.BindProviderCredentialReference(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if retried != binding {
		t.Fatalf("retried binding = %#v, want %#v", retried, binding)
	}
	read, err := repositories.GetProviderCredentialReference(ctx, providerID)
	if err != nil {
		t.Fatal(err)
	}
	if read != binding {
		t.Fatalf("read binding = %#v, want %#v", read, binding)
	}
	changed := input
	changed.OpaqueReference = "os://openai/other"
	if _, err := repositories.BindProviderCredentialReference(ctx, changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed binding error = %v", err)
	}
	path := database.Path()
	if err := database.Close(ctx); err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []string{path, path + "-wal", path + "-shm"} {
		content, err := os.ReadFile(candidate)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(content, material) {
			t.Fatalf("credential material appears in SQLite file %q", candidate)
		}
	}
}
