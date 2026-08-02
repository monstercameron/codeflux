//go:build windows

package storage

import (
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// TestAUDIT004_EveryDatabaseArtifactIsRestrictedToTheCurrentUser covers
// AUDIT-004, reconciling M03-003 on the primary supported platform.
//
// M03-003 was recorded complete against code that skipped permissions entirely
// when GOOS was windows, and against a test that skipped its own assertion
// there. The database, its write-ahead log, its shared-memory file, its
// backups, and the migration lock therefore inherited the parent directory's
// DACL, which on a default profile includes Users.
func TestAUDIT004_EveryDatabaseArtifactIsRestrictedToTheCurrentUser(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "data", "codeflux.sqlite3")

	database, err := Open(t.Context(), OpenOptions{Path: path})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close(t.Context()) })

	// Force a write so SQLite materialises the write-ahead log and the shared
	// memory file. Asserting on sidecars that never existed would pass without
	// proving anything.
	if _, err := database.Migrate(t.Context(), MigrationOptions{
		ApplicationVersion: "audit-004-test",
		BackupDirectory:    filepath.Join(root, "backups"),
		AvailableBytes:     func(string) (uint64, error) { return ^uint64(0), nil },
	}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := restrictDatabaseArtifacts(path); err != nil {
		t.Fatalf("restrict artifacts: %v", err)
	}

	self, err := currentUserSID()
	if err != nil {
		t.Fatalf("resolve current user: %v", err)
	}
	expected := self.String()

	checked := 0
	for _, target := range append(
		[]string{filepath.Dir(path), path},
		databaseSidecarPaths(path)...,
	) {
		trustees, err := namedTrusteesFor(target)
		if err != nil {
			// A sidecar that does not exist on this platform is not a failure;
			// a sidecar that exists and cannot be read is.
			if strings.Contains(err.Error(), "cannot find the file") ||
				strings.Contains(err.Error(), "cannot find the path") {
				continue
			}
			t.Errorf("%s: %v", target, err)
			continue
		}
		checked++
		if len(trustees) == 0 {
			t.Errorf("%s has an empty DACL", target)
			continue
		}
		for _, trustee := range trustees {
			if trustee != expected {
				t.Errorf("%s grants %s; only the current user (%s) may be named",
					target, trustee, expected)
			}
		}
	}
	if checked < 3 {
		t.Fatalf("only %d artifacts were checked; the assertion is too weak to mean anything", checked)
	}
}

// TestAUDIT004_TheBackupCarriesTheSameGrantAsTheDatabase proves the copy is not
// the way the contents escape.
func TestAUDIT004_TheBackupCarriesTheSameGrantAsTheDatabase(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "data", "codeflux.sqlite3")
	database, err := Open(t.Context(), OpenOptions{Path: path})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close(t.Context()) })
	if _, err := database.Migrate(t.Context(), MigrationOptions{
		ApplicationVersion: "audit-004-test",
		BackupDirectory:    filepath.Join(root, "backups"),
		AvailableBytes:     func(string) (uint64, error) { return ^uint64(0), nil },
	}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	destination := filepath.Join(root, "backups", "codeflux.backup.sqlite3")
	if err := database.Backup(t.Context(), destination); err != nil {
		t.Fatalf("back up: %v", err)
	}

	self, err := currentUserSID()
	if err != nil {
		t.Fatalf("resolve current user: %v", err)
	}
	trustees, err := namedTrusteesFor(destination)
	if err != nil {
		t.Fatalf("read backup DACL: %v", err)
	}
	if len(trustees) == 0 {
		t.Fatal("the backup has an empty DACL")
	}
	for _, trustee := range trustees {
		if trustee != self.String() {
			t.Errorf("the backup grants %s; only the current user may be named", trustee)
		}
	}
}

// TestAUDIT004_ARestrictedPathRefusesAnInheritedGrant proves the protection
// flag is doing work.
//
// Setting a DACL without PROTECTED_DACL_SECURITY_INFORMATION leaves inherited
// entries in place, which looks identical in code review and is the difference
// between a restricted file and an unrestricted one.
func TestAUDIT004_ARestrictedPathRefusesAnInheritedGrant(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "codeflux.sqlite3")

	// Grant Users on the parent so there is an inheritable entry to reject.
	users, err := windows.CreateWellKnownSid(windows.WinBuiltinUsersSid)
	if err != nil {
		t.Skipf("cannot construct the Users SID on this host: %v", err)
	}
	permissive, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.GENERIC_ALL,
		AccessMode:        windows.GRANT_ACCESS,
		Inheritance:       windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_WELL_KNOWN_GROUP,
			TrusteeValue: windows.TrusteeValueFromSID(users),
		},
	}}, nil)
	if err != nil {
		t.Fatalf("build permissive ACL: %v", err)
	}
	if err := windows.SetNamedSecurityInfo(
		root, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION,
		nil, nil, permissive, nil,
	); err != nil {
		t.Skipf("cannot widen the temporary directory on this host: %v", err)
	}

	if err := ensureDatabaseFile(path); err != nil {
		t.Fatalf("create database file: %v", err)
	}
	trustees, err := namedTrusteesFor(path)
	if err != nil {
		t.Fatalf("read DACL: %v", err)
	}
	for _, trustee := range trustees {
		if trustee == users.String() {
			t.Fatal("the created database inherited the Users grant from its parent")
		}
	}
}
