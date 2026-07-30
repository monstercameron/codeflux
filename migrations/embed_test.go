package migrations

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"testing"
)

func TestEmbeddedMigrationsContainBootstrap(t *testing.T) {
	names, err := fs.Glob(Files, "*.sql")
	if err != nil {
		t.Fatalf("glob embedded migrations: %v", err)
	}
	if len(names) != 1 || names[0] != "000000_bootstrap.sql" {
		t.Fatalf("embedded migrations = %v, want [000000_bootstrap.sql]", names)
	}
	source, err := Files.ReadFile(names[0])
	if err != nil {
		t.Fatalf("read embedded bootstrap migration: %v", err)
	}
	if len(source) == 0 {
		t.Fatal("embedded bootstrap migration is empty")
	}
	sum := sha256.Sum256(source)
	if len(Catalog) != 1 || Catalog[0].Name != names[0] ||
		Catalog[0].SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("generated catalog does not identify embedded migration: %#v", Catalog)
	}
}
