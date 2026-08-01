package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/buildinfo"
	"codeflux.dev/codeflux/internal/credentials"
	"codeflux.dev/codeflux/internal/doctor"
	"codeflux.dev/codeflux/internal/storage"
)

// doctorInput wires the real system into the health checks.
//
// Every dependency is a small function rather than a package call inside the
// check, so the checks stay testable and the wiring stays inspectable in one
// place.
func doctorInput(databasePath string) doctor.Input {
	return doctor.Input{
		DatabasePath:  databasePath,
		WorktreeRoot:  filepath.Dir(databasePath),
		ListenAddress: "127.0.0.1:0",

		GoVersion:  runtime.Version,
		GitVersion: gitVersion,
		PathWritable: func(path string) error {
			return directoryWritable(filepath.Dir(path))
		},
		DatabaseHealth:  databaseHealth,
		CredentialStore: credentials.PlatformStatus,
		DiskFree:        freeBytes,
		PortBindable:    portBindable,
		Versions:        currentVersions,
		// Provider connectivity and task counts are supplied only when a
		// database is present; a doctor run on a machine with nothing set up
		// must not invent answers for them.
		ProviderReachable: nil,
		TaskCounts:        nil,
	}
}

func gitVersion(ctx context.Context) (string, error) {
	path, err := exec.LookPath("git")
	if err != nil {
		return "", err
	}
	output, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimPrefix(strings.TrimSpace(string(output)), "git version "), nil
}

// directoryWritable checks a directory can actually be written to, by writing.
//
// Inspecting permission bits is not enough: they do not account for a
// read-only mount, a full disk, or an ACL the bits do not describe. Creating
// and removing a file is the only answer that means anything.
func directoryWritable(directory string) error {
	if strings.TrimSpace(directory) == "" {
		return errors.New("no directory was supplied")
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}
	probe, err := os.CreateTemp(directory, ".codeflux-write-probe-*")
	if err != nil {
		return fmt.Errorf("write probe: %w", err)
	}
	name := probe.Name()
	closeErr := probe.Close()
	removeErr := os.Remove(name)
	if closeErr != nil {
		return closeErr
	}
	return removeErr
}

func databaseHealth(ctx context.Context, path string) (doctor.DatabaseHealth, error) {
	if strings.TrimSpace(path) == "" {
		return doctor.DatabaseHealth{}, errors.New("no database path")
	}
	if _, err := os.Stat(path); err != nil {
		return doctor.DatabaseHealth{}, err
	}
	database, err := storage.Open(ctx, storage.OpenOptions{Path: path})
	if err != nil {
		return doctor.DatabaseHealth{}, err
	}
	defer func() { _ = database.Close(ctx) }()

	diagnostics, err := database.Diagnose(ctx)
	if err != nil {
		return doctor.DatabaseHealth{}, err
	}
	integrityErr := database.IntegrityCheck(ctx)
	return doctor.DatabaseHealth{
		IntegrityOK:            integrityErr == nil,
		SchemaVersion:          diagnostics.SchemaVersion,
		SupportedSchemaVersion: diagnostics.SupportedSchemaVersion,
		FailedMigrations:       diagnostics.FailedMigrations,
	}, nil
}

// portBindable reports whether an address can be bound, by binding it.
//
// The listener is closed immediately. Anything short of actually binding —
// scanning a port list, asking the OS what is in use — races with whatever
// else on the machine is starting up.
func portBindable(address string) error {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	return listener.Close()
}

func currentVersions() doctor.Versions {
	info := buildinfo.Current()
	return doctor.Versions{
		Application:            info.Version,
		Commit:                 info.Commit,
		SchemaVersion:          int(info.SchemaVersion),
		SupportedSchemaVersion: int(info.SchemaVersion),
	}
}

// runDoctorChecks implements `codeflux doctor` on top of internal/doctor
// (M23-004, M23-026..036).
func runDoctorChecks(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	arguments, err := parseMaintenanceArguments(args, false)
	if err != nil {
		fmt.Fprintf(stderr, "codeflux doctor: %v\n", err)
		return exitUsage
	}
	databasePath := arguments.database
	if databasePath == "" {
		resolved, resolveErr := storage.DefaultDatabasePath()
		if resolveErr != nil {
			fmt.Fprintln(stderr,
				"codeflux doctor: no default database location is available on this system")
			return exitUnavailable
		}
		databasePath = resolved
	}

	input := doctorInput(databasePath)
	if _, statErr := os.Stat(databasePath); statErr == nil {
		input.TaskCounts = func(ctx context.Context) (doctor.TaskCounts, error) {
			return databaseTaskCounts(ctx, databasePath)
		}
		// Providers are read from the credential store, which is only
		// meaningful once there is a database to have configured them against.
		input.ProviderReachable = configuredProviderReachability
	} else {
		// With no database, these checks are still WIRED, and report the real
		// situation. Leaving them nil produced "invoked in an unusual way",
		// which is exactly the wrong thing to tell a new user whose only
		// problem is that they have not run anything yet.
		input.TaskCounts = func(context.Context) (doctor.TaskCounts, error) {
			return doctor.TaskCounts{}, nil
		}
		input.ProviderReachable = func(context.Context) ([]doctor.ProviderReachability, error) {
			return nil, nil
		}
	}

	report, err := doctor.Run(ctx, input, time.Now)
	if err != nil {
		fmt.Fprintf(stderr, "codeflux doctor: %v\n", err)
		return exitFailure
	}

	// The title column is sized from the widest title actually present. A
	// fixed width shears the alignment of every row after a long check name,
	// which makes a report that is mostly fine look broken.
	titleWidth := 0
	for _, result := range report.Results {
		check, _ := doctor.CheckFor(result.ID)
		if len(check.Title) > titleWidth {
			titleWidth = len(check.Title)
		}
	}
	indent := strings.Repeat(" ", titleWidth+11)
	for _, result := range report.Results {
		check, _ := doctor.CheckFor(result.ID)
		fmt.Fprintf(stdout, "%-*s %-9s %s\n",
			titleWidth, check.Title, result.Status, result.Summary)
		if result.Detail != "" {
			fmt.Fprintf(stdout, "%s%s\n", indent, result.Detail)
		}
		if result.Status != doctor.StatusOK {
			// The remediation is printed with every non-ok result rather than
			// only for the first, because a user fixing one thing wants to see
			// everything they will have to fix.
			fmt.Fprintf(stdout, "%s-> %s\n", indent, result.Remediation)
		}
	}

	if report.Healthy() {
		fmt.Fprintln(stdout, "\ndoctor: everything required is available")
		return exitOK
	}
	first, _ := report.FirstBlocking()
	fmt.Fprintf(stdout, "\ndoctor: %d check(s) block work; start with %q\n",
		report.Blocking, first.ID)
	return exitUnavailable
}

func databaseTaskCounts(ctx context.Context, path string) (doctor.TaskCounts, error) {
	database, err := storage.Open(ctx, storage.OpenOptions{Path: path})
	if err != nil {
		return doctor.TaskCounts{}, err
	}
	defer func() { _ = database.Close(ctx) }()

	repositories, err := storage.NewRepositories(database, time.Now)
	if err != nil {
		return doctor.TaskCounts{}, err
	}
	window := storage.MetricsWindow{
		// The window is deliberately wide: a doctor reports the situation now,
		// not the situation this week.
		From: time.Unix(0, 0).UTC(),
		To:   time.Now().UTC().Add(time.Hour),
	}
	metrics, err := repositories.InterruptionMetrics(ctx, window)
	if err != nil {
		return doctor.TaskCounts{}, err
	}
	outcomes, err := repositories.TaskOutcomeMetrics(ctx, window)
	if err != nil {
		return doctor.TaskCounts{}, err
	}
	terminal := outcomes.TasksCompleted.Value + outcomes.TasksFailed.Value +
		outcomes.TasksCancelled.Value + outcomes.TasksRolledBack.Value
	active := outcomes.TasksStarted.Value - terminal - metrics.TasksPaused.Value -
		metrics.RecoveryRequired.Value
	if active < 0 {
		active = 0
	}
	return doctor.TaskCounts{
		Active:           int(active),
		Paused:           int(metrics.TasksPaused.Value),
		RecoveryRequired: int(metrics.RecoveryRequired.Value),
	}, nil
}

func freeBytes(path string) (uint64, error) {
	return platformFreeBytes(path)
}

// configuredProviderReachability reports what is configured without ever
// contacting a provider.
//
// A doctor run must not spend a user's money or block on a network round trip.
// It reports whether a credential is present and readable; proving the key
// works against the real endpoint is `codeflux provider test`'s job.
func configuredProviderReachability(ctx context.Context) ([]doctor.ProviderReachability, error) {
	available, _ := credentials.PlatformStatus()
	if !available {
		return nil, errors.New("no credential store")
	}
	store := credentials.NewPlatformStore()
	var reachability []doctor.ProviderReachability
	for _, name := range supportedProviders() {
		reference, err := credentials.NewReference(providerCredentialService, name)
		if err != nil {
			continue
		}
		if err := store.Test(ctx, reference); err != nil {
			continue
		}
		// Reachable and Authorized describe the LOCAL credential: it is present
		// and the store returned it. The names are honest about scope because
		// the summary says the check did not contact the provider.
		reachability = append(reachability, doctor.ProviderReachability{
			Name: name, Reachable: true, Authorized: true,
		})
	}
	return reachability, nil
}
