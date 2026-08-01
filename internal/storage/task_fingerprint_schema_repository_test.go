package storage

import "testing"

func TestTaskFingerprintSchemaVersionRegistrySeedsVersionOne(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	version, err := repositories.GetTaskFingerprintSchemaVersion(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if version.Version != 1 || version.Description == "" {
		t.Fatalf("seeded fingerprint schema version = %#v", version)
	}
	versions, err := repositories.ListTaskFingerprintSchemaVersions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 1 || versions[0].Version != 1 {
		t.Fatalf("fingerprint schema versions = %#v", versions)
	}
	if _, err := repositories.GetTaskFingerprintSchemaVersion(ctx, 2); err == nil {
		t.Fatal("expected an unregistered fingerprint schema version to be not found")
	}
}
