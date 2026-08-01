package release

import (
	"errors"
	"strings"
	"testing"
)

func currentBuild() Build {
	return Build{Version: "0.2.0", Schema: SchemaRange{Minimum: 20, Current: 29}}
}

// TestM23_064_ManualUpdateIsTheDefault covers M23-064.
func TestM23_064_ManualUpdateIsTheDefault(t *testing.T) {
	if DefaultUpdatePolicy() != PolicyManual {
		t.Fatalf("the default update policy is %q", DefaultUpdatePolicy())
	}
	if !PolicyManual.Valid() || !PolicyNotify.Valid() {
		t.Fatal("a declared policy does not validate")
	}
	// There must be no automatic policy. An agent that can change a repository
	// and hold credentials must not be able to replace its own executable
	// without being asked, and adding that is a product decision rather than a
	// configuration value.
	for _, policy := range []UpdatePolicy{
		"automatic", "auto", "silent", "background", "",
	} {
		if UpdatePolicy(policy).Valid() {
			t.Fatalf("policy %q validated; automatic updating must not be configurable", policy)
		}
	}
}

// TestM23_065_068_CompatibilityIsCheckedBeforeMigration covers M23-065 and
// M23-068.
func TestM23_065_068_CompatibilityIsCheckedBeforeMigration(t *testing.T) {
	build := currentBuild()

	compatible, err := CheckCompatibility(build, 29)
	if err != nil {
		t.Fatalf("check compatibility: %v", err)
	}
	if compatible.Verdict != VerdictCompatible {
		t.Fatalf("a current database reported %q", compatible.Verdict)
	}
	if compatible.MigrationRequired || compatible.BackupRequired || compatible.Blocking() {
		t.Fatalf("a current database requires work: %+v", compatible)
	}

	forward, err := CheckCompatibility(build, 25)
	if err != nil {
		t.Fatalf("check compatibility: %v", err)
	}
	if forward.Verdict != VerdictMigrationRequired {
		t.Fatalf("an older database reported %q", forward.Verdict)
	}
	if !forward.MigrationRequired || !forward.BackupRequired {
		t.Fatalf("a forward migration does not require a backup: %+v", forward)
	}
	if forward.Blocking() {
		t.Fatal("a supported migration was treated as blocking")
	}

	// M23-068: the dangerous direction must be refused, not attempted.
	downgrade, err := CheckCompatibility(build, 40)
	if err != nil {
		t.Fatalf("check compatibility: %v", err)
	}
	if downgrade.Verdict != VerdictRefuseDowngrade {
		t.Fatalf("a newer database reported %q", downgrade.Verdict)
	}
	if !downgrade.Blocking() {
		t.Fatal("a downgrade was not treated as blocking")
	}
	if downgrade.MigrationRequired {
		t.Fatal("a refused downgrade still planned a migration")
	}
	for _, phrase := range []string{"newer version", "restore"} {
		text := downgrade.Explanation + " " + downgrade.Remediation
		if !strings.Contains(strings.ToLower(text), phrase) {
			t.Fatalf("the downgrade refusal does not mention %q: %q", phrase, text)
		}
	}

	tooOld, err := CheckCompatibility(build, 5)
	if err != nil {
		t.Fatalf("check compatibility: %v", err)
	}
	if tooOld.Verdict != VerdictTooOld || !tooOld.Blocking() {
		t.Fatalf("a database below the migration window reported %+v", tooOld)
	}
	// Even a refusal must offer a way forward.
	if strings.TrimSpace(tooOld.Remediation) == "" {
		t.Fatal("a too-old database is refused with no path forward")
	}

	// Malformed input is refused rather than guessed at.
	if _, err := CheckCompatibility(build, 0); err == nil {
		t.Fatal("a zero schema version was accepted")
	}
	if _, err := CheckCompatibility(Build{Schema: SchemaRange{Minimum: 30, Current: 20}}, 25); err == nil {
		t.Fatal("an inverted schema range was accepted")
	}
	if err := (SchemaRange{}).Validate(); err == nil {
		t.Fatal("an empty schema range validated")
	}
}

// TestM23_066_MigrationRequiresABackupFirst covers M23-066.
func TestM23_066_MigrationRequiresABackupFirst(t *testing.T) {
	forward, err := CheckCompatibility(currentBuild(), 25)
	if err != nil {
		t.Fatalf("check compatibility: %v", err)
	}

	taken := 0
	path, err := PrepareMigration(forward, func() (string, error) {
		taken++
		return "/fixture/backups/pre-migration.sqlite3", nil
	})
	if err != nil {
		t.Fatalf("prepare migration: %v", err)
	}
	if taken != 1 || path == "" {
		t.Fatalf("the backup was not taken: count=%d path=%q", taken, path)
	}

	// There is no way to skip it. A migration is the one operation a user
	// cannot undo.
	if _, err := PrepareMigration(forward, nil); !errors.Is(err, ErrBackupRequired) {
		t.Fatalf("migrating with no backup returned %v", err)
	}
	if _, err := PrepareMigration(forward, func() (string, error) {
		return "", errors.New("disk full")
	}); !errors.Is(err, ErrBackupRequired) {
		t.Fatal("a failed backup still permitted migration")
	}
	if _, err := PrepareMigration(forward, func() (string, error) {
		return "  ", nil
	}); !errors.Is(err, ErrBackupRequired) {
		t.Fatal("a backup reporting no location still permitted migration")
	}

	// No migration means no backup is demanded.
	compatible, _ := CheckCompatibility(currentBuild(), 29)
	path, err = PrepareMigration(compatible, nil)
	if err != nil || path != "" {
		t.Fatalf("an unnecessary backup was demanded: %q %v", path, err)
	}

	// A blocking verdict never reaches a migration at all.
	downgrade, _ := CheckCompatibility(currentBuild(), 40)
	if _, err := PrepareMigration(downgrade, func() (string, error) {
		return "/fixture", nil
	}); err == nil {
		t.Fatal("a refused downgrade proceeded to migration")
	}
}

// TestM23_067_ReleaseNotesShowChangesAndTheMigrationWarning covers M23-067.
func TestM23_067_ReleaseNotesShowChangesAndTheMigrationWarning(t *testing.T) {
	notes := ReleaseNotes{
		Version: "0.2.0",
		Summary: "faster context selection and a real doctor",
		Changes: []string{
			"context selection is roughly twice as fast on large repositories",
			"`codeflux doctor` now reports every prerequisite with a remediation",
		},
		Breaking: []string{
			"the `integrity` command is now `integrity-check`; the old name still works",
		},
		MigrationWarning: "the migration rewrites the task index and cannot be undone",
	}
	forward, err := CheckCompatibility(currentBuild(), 25)
	if err != nil {
		t.Fatalf("check compatibility: %v", err)
	}

	rendered, err := notes.Render(forward)
	if err != nil {
		t.Fatalf("render notes: %v", err)
	}
	text := strings.Join(rendered, "\n")
	for _, expected := range []string{
		"0.2.0", notes.Summary, notes.Changes[0], notes.Breaking[0],
		"changes the database", notes.MigrationWarning, "backup is taken automatically",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("the rendered notes omit %q:\n%s", expected, text)
		}
	}
	// Breaking changes must appear before the migration warning: a user who
	// stops reading has still seen the part that will cost them time.
	if strings.Index(text, notes.Breaking[0]) > strings.Index(text, "changes the database") {
		t.Fatalf("the migration warning precedes the breaking changes:\n%s", text)
	}

	// With no migration, the database section is absent rather than empty.
	compatible, _ := CheckCompatibility(currentBuild(), 29)
	rendered, err = notes.Render(compatible)
	if err != nil {
		t.Fatalf("render notes: %v", err)
	}
	if strings.Contains(strings.Join(rendered, "\n"), "changes the database") {
		t.Fatal("a no-migration release still warned about the database")
	}

	// A blocking verdict must be shown in the notes, since that is where a
	// user finds out the upgrade will not work.
	downgrade, _ := CheckCompatibility(currentBuild(), 40)
	rendered, err = notes.Render(downgrade)
	if err != nil {
		t.Fatalf("render notes: %v", err)
	}
	if !strings.Contains(strings.Join(rendered, "\n"), "cannot open your database") {
		t.Fatal("a blocking verdict was not surfaced in the notes")
	}

	for name, corrupt := range map[string]func(ReleaseNotes) ReleaseNotes{
		"no version": func(candidate ReleaseNotes) ReleaseNotes {
			candidate.Version = ""
			return candidate
		},
		"no summary": func(candidate ReleaseNotes) ReleaseNotes {
			candidate.Summary = ""
			return candidate
		},
		"no changes": func(candidate ReleaseNotes) ReleaseNotes {
			candidate.Changes = nil
			return candidate
		},
		"empty change": func(candidate ReleaseNotes) ReleaseNotes {
			candidate.Changes = []string{""}
			return candidate
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := corrupt(notes).Validate(); err == nil {
				t.Fatalf("unusable notes validated: %s", name)
			}
		})
	}
}

// TestM23_069_RestoreInstructionsCoverBothHalves covers M23-069.
func TestM23_069_RestoreInstructionsCoverBothHalves(t *testing.T) {
	instructions, err := RestoreInstructions("/fixture/backups/pre-migration.sqlite3", "0.1.0")
	if err != nil {
		t.Fatalf("restore instructions: %v", err)
	}
	text := strings.Join(instructions, "\n")

	// Both halves must be present. Restoring only the database leaves a newer
	// binary in front of an older schema, which is the downgrade the
	// compatibility check refuses.
	if !strings.Contains(text, "0.1.0") {
		t.Fatalf("the instructions do not name the previous version:\n%s", text)
	}
	if !strings.Contains(text, "/fixture/backups/pre-migration.sqlite3") {
		t.Fatalf("the instructions do not name the backup:\n%s", text)
	}
	if !strings.Contains(strings.ToLower(text), "both steps are needed") {
		t.Fatalf("the instructions do not say both halves are required:\n%s", text)
	}
	if !strings.Contains(strings.ToLower(text), "repositories are not affected") {
		t.Fatalf("the instructions do not reassure about repositories:\n%s", text)
	}

	if _, err := RestoreInstructions("", "0.1.0"); err == nil {
		t.Fatal("instructions were produced with no backup location")
	}
	if _, err := RestoreInstructions("/fixture", ""); err == nil {
		t.Fatal("instructions were produced with no previous version")
	}
}

// TestM23_070_EverySupportedUpgradePathBehavesAsDeclared covers M23-070.
func TestM23_070_EverySupportedUpgradePathBehavesAsDeclared(t *testing.T) {
	if err := VerifyUpgradePaths(); err != nil {
		t.Fatalf("upgrade paths: %v", err)
	}

	paths := SupportedUpgradePaths()
	if len(paths) < 5 {
		t.Fatalf("only %d upgrade paths are declared", len(paths))
	}

	// The declared set must exercise every verdict, or it only tests the
	// transitions that were easy to get right.
	verdicts := map[CompatibilityVerdict]bool{}
	for _, path := range paths {
		verdicts[path.ExpectedVerdict] = true
	}
	for _, required := range []CompatibilityVerdict{
		VerdictCompatible, VerdictMigrationRequired,
		VerdictRefuseDowngrade, VerdictTooOld,
	} {
		if !verdicts[required] {
			t.Fatalf("no upgrade path exercises %q", required)
		}
	}

	// Each path must be individually checkable, so a failure names the
	// transition rather than the whole set.
	for _, path := range paths {
		compatibility, err := CheckCompatibility(path.To, path.From.Schema.Current)
		if err != nil {
			t.Fatalf("%s -> %s: %v", path.From.Version, path.To.Version, err)
		}
		if compatibility.Verdict != path.ExpectedVerdict {
			t.Fatalf("%s -> %s: verdict %q, expected %q",
				path.From.Version, path.To.Version,
				compatibility.Verdict, path.ExpectedVerdict)
		}
		// A migration path must also demand a backup, tying M23-066 to the
		// paths that actually trigger it.
		if compatibility.MigrationRequired && !compatibility.BackupRequired {
			t.Fatalf("%s -> %s migrates without requiring a backup",
				path.From.Version, path.To.Version)
		}
	}
}
