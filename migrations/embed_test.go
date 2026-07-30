package migrations

import (
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
}
