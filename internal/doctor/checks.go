package doctor

import (
	"context"
	"fmt"
	"strings"
)

// Checks returns every health check with its implementation (M23-026..035).
func Checks() []Check {
	return []Check{
		{
			ID: CheckVersions, Todo: "M23-035", Title: "Application and migration versions",
			Run: checkVersions,
		},
		{
			ID: CheckGoToolchain, Todo: "M23-026", Title: "Go toolchain",
			Run: checkGoToolchain,
		},
		{
			ID: CheckGit, Todo: "M23-027", Title: "Git",
			Run: checkGit,
		},
		{
			ID: CheckDatabasePath, Todo: "M23-028", Title: "Database location permissions",
			Run: checkDatabasePath,
		},
		{
			ID: CheckDatabaseIntegrity, Todo: "M23-029", Title: "Database integrity and schema",
			Run: checkDatabaseIntegrity,
		},
		{
			ID: CheckCredentialStore, Todo: "M23-030", Title: "Credential store",
			Run: checkCredentialStore,
		},
		{
			ID: CheckProvider, Todo: "M23-031", Title: "Provider connectivity",
			Run: checkProvider,
		},
		{
			ID: CheckWorktreeRoot, Todo: "M23-032", Title: "Worktree root and disk space",
			Run: checkWorktreeRoot,
		},
		{
			ID: CheckLoopbackPort, Todo: "M23-033", Title: "Loopback port",
			Run: checkLoopbackPort,
		},
		{
			ID: CheckTaskStates, Todo: "M23-034", Title: "Active and recovery-required tasks",
			Run: checkTaskStates,
		},
	}
}

// unavailable builds the result for a check whose input was not supplied.
//
// It is StatusUnknown rather than StatusFailed: a check that could not run
// proves nothing about the thing it was going to inspect, and reporting it as
// a failure would send a user chasing a problem that may not exist.
func unavailable(what string) Result {
	return Result{
		Status:  StatusUnknown,
		Summary: what + " could not be checked",
		Detail:  "this check was not supplied with the means to run",
		Remediation: "run `codeflux doctor` from a normal installation; " +
			"a check with no inputs usually means the command was invoked in an unusual way",
	}
}

func checkVersions(_ context.Context, input Input) Result {
	if input.Versions == nil {
		return unavailable("versions")
	}
	versions := input.Versions()
	summary := fmt.Sprintf("codeflux %s (%s), schema %d",
		versions.Application, versions.Commit, versions.SchemaVersion)

	if versions.SchemaVersion > versions.SupportedSchemaVersion {
		// A database newer than the binary is the dangerous direction: an old
		// binary writing to a new schema can corrupt data a newer version
		// wrote. Refusing is the only safe answer.
		return Result{
			Status:  StatusFailed,
			Summary: summary,
			Detail: fmt.Sprintf(
				"the database is at schema %d but this build supports %d",
				versions.SchemaVersion, versions.SupportedSchemaVersion),
			Remediation: "upgrade CodeFlux to a build that supports this schema; " +
				"do not run an older build against a newer database",
		}
	}
	if versions.SchemaVersion < versions.SupportedSchemaVersion {
		return Result{
			Status:  StatusDegraded,
			Summary: summary,
			Detail: fmt.Sprintf(
				"the database is at schema %d and this build expects %d; migrations will run at next start",
				versions.SchemaVersion, versions.SupportedSchemaVersion),
			Remediation: "run `codeflux start` to apply the pending migrations, " +
				"or `codeflux backup --output <path>` first if you want a snapshot",
		}
	}
	return Result{
		Status: StatusOK, Summary: summary,
		Detail: fmt.Sprintf("%d migrations applied", versions.AppliedMigrations),
	}
}

func checkGoToolchain(_ context.Context, input Input) Result {
	if input.GoVersion == nil {
		return unavailable("the Go toolchain")
	}
	version := strings.TrimSpace(input.GoVersion())
	if version == "" {
		return Result{
			Status:  StatusUnknown,
			Summary: "the Go toolchain version could not be determined",
			Remediation: "this is unexpected in a released build; " +
				"reinstall CodeFlux from a published artifact",
		}
	}
	// The toolchain is compiled in, so this is informational rather than a
	// prerequisite the user must install. It is reported because a bug report
	// is much easier to act on with it than without.
	return Result{
		Status: StatusOK, Summary: "built with " + version,
		Detail: "the toolchain is compiled into this executable; nothing needs to be installed",
	}
}

func checkGit(ctx context.Context, input Input) Result {
	if input.GitVersion == nil {
		return unavailable("Git")
	}
	version, err := input.GitVersion(ctx)
	if err != nil {
		return Result{
			Status:  StatusMissing,
			Summary: "Git was not found",
			Detail:  "CodeFlux uses Git for every repository operation, including task worktrees",
			Remediation: "install Git and make sure it is on your PATH, then run " +
				"`codeflux doctor` again",
		}
	}
	version = strings.TrimSpace(version)
	if version == "" {
		return Result{
			Status:      StatusDegraded,
			Summary:     "Git is present but did not report a version",
			Remediation: "check that `git --version` works in your shell",
		}
	}
	return Result{Status: StatusOK, Summary: "Git " + version}
}

func checkDatabasePath(_ context.Context, input Input) Result {
	if input.PathWritable == nil {
		return unavailable("the database location")
	}
	if strings.TrimSpace(input.DatabasePath) == "" {
		return Result{
			Status:  StatusUnknown,
			Summary: "no database location is configured",
			Remediation: "pass --database, or let CodeFlux use the default location by " +
				"running `codeflux start` without it",
		}
	}
	if err := input.PathWritable(input.DatabasePath); err != nil {
		return Result{
			Status:  StatusFailed,
			Summary: "the database location is not writable",
			Detail:  "CodeFlux must be able to create and update its database and its journal files",
			Remediation: "check the permissions on the directory holding the database, " +
				"or pass --database with a location you can write to",
		}
	}
	return Result{Status: StatusOK, Summary: "the database location is writable"}
}

func checkDatabaseIntegrity(ctx context.Context, input Input) Result {
	if input.DatabaseHealth == nil {
		return unavailable("database integrity")
	}
	health, err := input.DatabaseHealth(ctx, input.DatabasePath)
	if err != nil {
		return Result{
			Status:  StatusMissing,
			Summary: "no database has been created yet",
			Detail:  "this is expected before the first run",
			Remediation: "run `codeflux start` to create it, " +
				"or pass --database if your database is somewhere else",
		}
	}
	if !health.IntegrityOK {
		return Result{
			Status:  StatusFailed,
			Summary: "the database failed its integrity check",
			Detail:  "SQLite reports the file is structurally damaged",
			Remediation: "restore the most recent snapshot written by `codeflux backup`; " +
				"do not continue working against a damaged database",
		}
	}
	if health.FailedMigrations > 0 {
		return Result{
			Status:  StatusFailed,
			Summary: fmt.Sprintf("%d migrations failed", health.FailedMigrations),
			Detail:  "the schema is not in a state this build can rely on",
			Remediation: "run `codeflux backup --output <path>`, then report the failure with " +
				"`codeflux diagnostics export`; do not start work against a partly migrated schema",
		}
	}
	if health.SchemaVersion != health.SupportedSchemaVersion {
		return Result{
			Status: StatusDegraded,
			Summary: fmt.Sprintf("schema %d, this build expects %d",
				health.SchemaVersion, health.SupportedSchemaVersion),
			Remediation: "run `codeflux start` to apply pending migrations",
		}
	}
	return Result{Status: StatusOK, Summary: "the database is intact and current"}
}

func checkCredentialStore(_ context.Context, input Input) Result {
	if input.CredentialStore == nil {
		return unavailable("the credential store")
	}
	available, backend := input.CredentialStore()
	if !available {
		return Result{
			Status:  StatusMissing,
			Summary: "no credential store is available (" + backend + ")",
			Detail: "CodeFlux keeps provider credentials in the operating system store " +
				"rather than in its database or in a file",
			Remediation: "on Windows, make sure the Credential Manager service is running; " +
				"on Linux, install and unlock a Secret Service keyring",
		}
	}
	return Result{Status: StatusOK, Summary: "credential store available (" + backend + ")"}
}

// checkProvider is M23-031: connectivity WITHOUT exposing credentials.
func checkProvider(ctx context.Context, input Input) Result {
	if input.ProviderReachable == nil {
		return unavailable("provider connectivity")
	}
	providers, err := input.ProviderReachable(ctx)
	if err != nil {
		return Result{
			Status:      StatusUnknown,
			Summary:     "provider connectivity could not be checked",
			Remediation: "run `codeflux provider test --name <provider>` for a focused check",
		}
	}
	if len(providers) == 0 {
		return Result{
			Status:      StatusMissing,
			Summary:     "no provider is configured",
			Detail:      "CodeFlux needs one provider before it can do any work",
			Remediation: "run `codeflux provider set --name <provider>`",
		}
	}

	var unreachable, unauthorized []string
	for _, provider := range providers {
		switch {
		case !provider.Reachable:
			unreachable = append(unreachable, provider.Name)
		case !provider.Authorized:
			unauthorized = append(unauthorized, provider.Name)
		}
	}
	// The two failures are reported apart because they need different fixes,
	// and conflating them sends a user to re-paste a key that was fine.
	if len(unauthorized) > 0 {
		return Result{
			Status:      StatusFailed,
			Summary:     "a provider rejected the stored credential: " + strings.Join(unauthorized, ", "),
			Detail:      "the endpoint answered, so this is the credential rather than the network",
			Remediation: "run `codeflux provider set --name <provider>` with a current key",
		}
	}
	if len(unreachable) > 0 {
		return Result{
			Status:  StatusDegraded,
			Summary: "a provider did not answer: " + strings.Join(unreachable, ", "),
			Detail:  "the credential was not rejected, so this looks like the network rather than the key",
			Remediation: "check your connection or proxy settings; the stored credential " +
				"does not need to change",
		}
	}
	return Result{
		Status:  StatusOK,
		Summary: fmt.Sprintf("%d provider(s) reachable and authorized", len(providers)),
	}
}

func checkWorktreeRoot(_ context.Context, input Input) Result {
	if input.PathWritable == nil || input.DiskFree == nil {
		return unavailable("the worktree root")
	}
	if strings.TrimSpace(input.WorktreeRoot) == "" {
		return Result{
			Status:      StatusUnknown,
			Summary:     "no worktree root is configured",
			Remediation: "select a repository; the worktree root is derived from it",
		}
	}
	if err := input.PathWritable(input.WorktreeRoot); err != nil {
		return Result{
			Status:  StatusFailed,
			Summary: "the worktree root is not writable",
			Detail:  "each task needs its own Git worktree created under this directory",
			Remediation: "check the permissions on the worktree root, or choose a repository " +
				"in a location you can write to",
		}
	}
	free, err := input.DiskFree(input.WorktreeRoot)
	if err != nil {
		return Result{
			Status:      StatusUnknown,
			Summary:     "free space at the worktree root could not be determined",
			Remediation: "check the disk holding your repository is mounted and readable",
		}
	}
	if free < MinimumFreeBytes {
		return Result{
			Status: StatusDegraded,
			Summary: fmt.Sprintf("only %d MiB free at the worktree root",
				free/(1<<20)),
			Detail: "a task worktree is a full checkout plus build output; running out of " +
				"space mid-task looks like unrelated build failures",
			Remediation: fmt.Sprintf("free at least %d MiB, or move the repository to a "+
				"disk with more space", MinimumFreeBytes/(1<<20)),
		}
	}
	return Result{
		Status:  StatusOK,
		Summary: fmt.Sprintf("worktree root writable with %d MiB free", free/(1<<20)),
	}
}

func checkLoopbackPort(_ context.Context, input Input) Result {
	if input.PortBindable == nil {
		return unavailable("the loopback port")
	}
	address := input.ListenAddress
	if strings.TrimSpace(address) == "" {
		address = "127.0.0.1:0"
	}
	if err := input.PortBindable(address); err != nil {
		return Result{
			Status:  StatusFailed,
			Summary: "the loopback address could not be bound",
			Detail:  "another process is probably already listening there",
			Remediation: "stop the other CodeFlux instance, or pass --address with a " +
				"different loopback port",
		}
	}
	return Result{Status: StatusOK, Summary: "the loopback address is available"}
}

func checkTaskStates(ctx context.Context, input Input) Result {
	if input.TaskCounts == nil {
		return unavailable("task states")
	}
	counts, err := input.TaskCounts(ctx)
	if err != nil {
		return Result{
			Status:      StatusUnknown,
			Summary:     "task states could not be read",
			Remediation: "check the database integrity result above; it usually explains this",
		}
	}
	if counts.RecoveryRequired > 0 {
		// This is reported as degraded rather than failed: the system works,
		// but there is work the user must decide about before it can continue.
		return Result{
			Status: StatusDegraded,
			Summary: fmt.Sprintf("%d task(s) need recovery, %d active, %d paused",
				counts.RecoveryRequired, counts.Active, counts.Paused),
			Detail: "a task in recovery has an outcome CodeFlux cannot determine on its own",
			Remediation: "open each task needing recovery and choose one of the offered safe " +
				"options; nothing else will run against those tasks until you do",
		}
	}
	return Result{
		Status: StatusOK,
		Summary: fmt.Sprintf("%d active, %d paused, none needing recovery",
			counts.Active, counts.Paused),
	}
}
