package release

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// UpdatePolicy is how a build behaves about updates (M23-064).
type UpdatePolicy string

const (
	// PolicyManual is the prototype default: CodeFlux never updates itself.
	//
	// An agent that can change a repository and hold credentials must not also
	// be able to replace its own executable without being asked. Manual is not
	// a limitation here, it is the safe position.
	PolicyManual UpdatePolicy = "manual"
	// PolicyNotify checks for a newer release and says so, changing nothing.
	PolicyNotify UpdatePolicy = "notify-only"
)

// DefaultUpdatePolicy is what a prototype build ships with (M23-064).
func DefaultUpdatePolicy() UpdatePolicy { return PolicyManual }

// Valid reports whether a policy is one of the declared set.
//
// There is deliberately no automatic policy: adding one would be a product
// decision, not a configuration value.
func (policy UpdatePolicy) Valid() bool {
	return policy == PolicyManual || policy == PolicyNotify
}

// SchemaRange is the schema versions one build can read and write.
type SchemaRange struct {
	// Minimum is the oldest schema this build can migrate forward from.
	Minimum int
	// Current is the schema this build writes.
	Current int
}

// Validate rejects an impossible range.
func (window SchemaRange) Validate() error {
	if window.Minimum <= 0 || window.Current <= 0 {
		return errors.New("a schema range requires positive versions")
	}
	if window.Minimum > window.Current {
		return fmt.Errorf("schema range minimum %d is above current %d",
			window.Minimum, window.Current)
	}
	return nil
}

// Build identifies one release for compatibility purposes.
type Build struct {
	Version string
	Schema  SchemaRange
}

// CompatibilityVerdict is the answer to "can this build open that database"
// (M23-065, M23-068).
type CompatibilityVerdict string

const (
	// VerdictCompatible means the build can open the database as it is.
	VerdictCompatible CompatibilityVerdict = "compatible"
	// VerdictMigrationRequired means the build can open it after migrating
	// forward.
	VerdictMigrationRequired CompatibilityVerdict = "migration-required"
	// VerdictRefuseDowngrade means the database was written by a newer build
	// and this one cannot read it (M23-068).
	VerdictRefuseDowngrade CompatibilityVerdict = "refuse-downgrade"
	// VerdictTooOld means the database predates what this build can migrate.
	VerdictTooOld CompatibilityVerdict = "too-old"
)

// Compatibility is the full answer, including what to do about it.
type Compatibility struct {
	Verdict CompatibilityVerdict
	// MigrationRequired is whether the database will be changed.
	MigrationRequired bool
	// BackupRequired is whether a backup must be taken first (M23-066).
	BackupRequired bool
	// Explanation is what the user is told.
	Explanation string
	// Remediation is what they can do.
	Remediation string
}

// Blocking reports whether the build may not proceed.
func (compatibility Compatibility) Blocking() bool {
	return compatibility.Verdict == VerdictRefuseDowngrade ||
		compatibility.Verdict == VerdictTooOld
}

// CheckCompatibility decides whether a build may open a database, BEFORE any
// migration runs (M23-065, M23-068).
func CheckCompatibility(build Build, databaseSchema int) (Compatibility, error) {
	if err := build.Schema.Validate(); err != nil {
		return Compatibility{}, err
	}
	if databaseSchema <= 0 {
		return Compatibility{}, errors.New("a database schema version must be positive")
	}

	switch {
	case databaseSchema > build.Schema.Current:
		// M23-068: this is the dangerous direction. An older build writing to
		// a newer schema can corrupt data a newer version wrote, and the
		// damage is not detectable until much later.
		return Compatibility{
			Verdict: VerdictRefuseDowngrade,
			Explanation: fmt.Sprintf(
				"this database was written by a newer version of CodeFlux (schema %d); "+
					"this build understands schema %d",
				databaseSchema, build.Schema.Current),
			Remediation: "install the newer version again, or restore the backup taken " +
				"before you upgraded",
		}, nil

	case databaseSchema < build.Schema.Minimum:
		return Compatibility{
			Verdict: VerdictTooOld,
			Explanation: fmt.Sprintf(
				"this database is at schema %d, older than the oldest schema this build "+
					"can migrate from (%d)", databaseSchema, build.Schema.Minimum),
			Remediation: "upgrade with an intermediate release first, or start a new " +
				"database and keep the old one as a backup",
		}, nil

	case databaseSchema < build.Schema.Current:
		// M23-066: a forward migration rewrites durable data, so a backup is
		// required rather than suggested. The user cannot undo a migration.
		return Compatibility{
			Verdict:           VerdictMigrationRequired,
			MigrationRequired: true,
			BackupRequired:    true,
			Explanation: fmt.Sprintf(
				"this database is at schema %d and this build uses schema %d; "+
					"migrating changes it permanently",
				databaseSchema, build.Schema.Current),
			Remediation: "a backup is taken automatically before migrating; keep it until " +
				"you are satisfied the new version works",
		}, nil
	}

	return Compatibility{
		Verdict:     VerdictCompatible,
		Explanation: fmt.Sprintf("this database is already at schema %d", databaseSchema),
	}, nil
}

// ErrBackupRequired reports an attempt to migrate without one (M23-066).
var ErrBackupRequired = errors.New("a backup is required before migrating to a newer schema")

// BackupTaker writes a snapshot and returns where it went.
type BackupTaker func() (string, error)

// PrepareMigration takes the mandatory backup before a schema change
// (M23-066).
//
// The backup is not optional and there is no flag to skip it. A migration is
// the one operation a user cannot undo, and the whole cost of the backup is
// one file copy.
func PrepareMigration(compatibility Compatibility, backup BackupTaker) (string, error) {
	if compatibility.Blocking() {
		return "", fmt.Errorf("cannot migrate: %s", compatibility.Explanation)
	}
	if !compatibility.MigrationRequired {
		return "", nil
	}
	if backup == nil {
		return "", ErrBackupRequired
	}
	path, err := backup()
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrBackupRequired, err)
	}
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("%w: the backup reported no location", ErrBackupRequired)
	}
	return path, nil
}

// ReleaseNotes is what a user reads before upgrading (M23-067).
type ReleaseNotes struct {
	Version string
	// Summary is one line.
	Summary string
	// Changes are the user-visible differences.
	Changes []string
	// Breaking are changes that will require action.
	Breaking []string
	// MigrationWarning is shown when the release changes the schema.
	MigrationWarning string
}

// Validate rejects notes that would not help a user decide.
func (notes ReleaseNotes) Validate() error {
	switch {
	case strings.TrimSpace(notes.Version) == "":
		return errors.New("release notes require a version")
	case strings.TrimSpace(notes.Summary) == "":
		return fmt.Errorf("release %s has no summary", notes.Version)
	case len(notes.Changes) == 0:
		return fmt.Errorf("release %s lists no changes", notes.Version)
	}
	for index, change := range notes.Changes {
		if strings.TrimSpace(change) == "" {
			return fmt.Errorf("release %s change %d is empty", notes.Version, index)
		}
	}
	return nil
}

// Render produces what the user sees before upgrading (M23-067).
func (notes ReleaseNotes) Render(compatibility Compatibility) ([]string, error) {
	if err := notes.Validate(); err != nil {
		return nil, err
	}
	lines := []string{
		fmt.Sprintf("CodeFlux %s — %s", notes.Version, notes.Summary),
		"",
		"Changes:",
	}
	for _, change := range notes.Changes {
		lines = append(lines, "  - "+change)
	}
	if len(notes.Breaking) > 0 {
		// Breaking changes come BEFORE the migration warning, because a user
		// who stops reading has still seen the part that will cost them time.
		lines = append(lines, "", "Requires action from you:")
		for _, change := range notes.Breaking {
			lines = append(lines, "  - "+change)
		}
	}
	if compatibility.MigrationRequired {
		lines = append(lines, "", "This release changes the database.")
		lines = append(lines, "  "+compatibility.Explanation)
		if notes.MigrationWarning != "" {
			lines = append(lines, "  "+notes.MigrationWarning)
		}
		lines = append(lines,
			"  A backup is taken automatically before anything is changed.")
	}
	if compatibility.Blocking() {
		lines = append(lines, "", "This release cannot open your database:")
		lines = append(lines, "  "+compatibility.Explanation)
		lines = append(lines, "  "+compatibility.Remediation)
	}
	return lines, nil
}

// RestoreInstructions explain how to go back (M23-069).
//
// Going back has two halves and both are required: the database must be
// restored AND the older executable reinstalled. Restoring only the database
// leaves a newer binary in front of an older schema, which is the same
// downgrade the compatibility check refuses.
func RestoreInstructions(backupPath string, previousVersion string) ([]string, error) {
	if strings.TrimSpace(backupPath) == "" {
		return nil, errors.New("restore instructions require the backup location")
	}
	if strings.TrimSpace(previousVersion) == "" {
		return nil, errors.New("restore instructions require the previous version")
	}
	return []string{
		"To go back to the version you were using:",
		"",
		fmt.Sprintf("  1. Install CodeFlux %s again.", previousVersion),
		"  2. Stop CodeFlux if it is running.",
		fmt.Sprintf("  3. Replace your database with the backup at %s.", backupPath),
		"  4. Start CodeFlux.",
		"",
		"Both steps are needed. Restoring only the database leaves a newer executable " +
			"in front of an older schema, which CodeFlux refuses to open.",
		"",
		"Your repositories are not affected by any of this.",
	}, nil
}

// UpgradePath is one supported version transition (M23-070).
type UpgradePath struct {
	From Build
	To   Build
	// ExpectedVerdict is what CheckCompatibility must say about it.
	ExpectedVerdict CompatibilityVerdict
}

// SupportedUpgradePaths are the transitions a release must be tested against
// (M23-070).
func SupportedUpgradePaths() []UpgradePath {
	current := Build{Version: "0.2.0", Schema: SchemaRange{Minimum: 20, Current: 29}}
	return []UpgradePath{
		{
			// The ordinary upgrade: one release forward, schema moves.
			From:            Build{Version: "0.1.0", Schema: SchemaRange{Minimum: 20, Current: 28}},
			To:              current,
			ExpectedVerdict: VerdictMigrationRequired,
		},
		{
			// Reinstalling the same release must change nothing.
			From:            current,
			To:              current,
			ExpectedVerdict: VerdictCompatible,
		},
		{
			// Skipping a release is supported while the schema is in range.
			From:            Build{Version: "0.0.9", Schema: SchemaRange{Minimum: 20, Current: 22}},
			To:              current,
			ExpectedVerdict: VerdictMigrationRequired,
		},
		{
			// Downgrading must be refused rather than attempted (M23-068).
			From:            current,
			To:              Build{Version: "0.1.0", Schema: SchemaRange{Minimum: 20, Current: 28}},
			ExpectedVerdict: VerdictRefuseDowngrade,
		},
		{
			// A database older than the migration window is refused with a
			// path forward rather than silently mangled.
			From:            Build{Version: "0.0.1", Schema: SchemaRange{Minimum: 1, Current: 5}},
			To:              current,
			ExpectedVerdict: VerdictTooOld,
		},
	}
}

// VerifyUpgradePaths checks every supported transition behaves as declared
// (M23-070).
func VerifyUpgradePaths() error {
	var failures []string
	for _, path := range SupportedUpgradePaths() {
		compatibility, err := CheckCompatibility(path.To, path.From.Schema.Current)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s -> %s: %v",
				path.From.Version, path.To.Version, err))
			continue
		}
		if compatibility.Verdict != path.ExpectedVerdict {
			failures = append(failures, fmt.Sprintf("%s -> %s: verdict %q, expected %q",
				path.From.Version, path.To.Version,
				compatibility.Verdict, path.ExpectedVerdict))
		}
	}
	if len(failures) > 0 {
		sort.Strings(failures)
		return fmt.Errorf("upgrade paths behaved unexpectedly: %s", strings.Join(failures, "; "))
	}
	return nil
}
