package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"

	"codeflux.dev/codeflux/internal/buildinfo"
	"codeflux.dev/codeflux/internal/credentials"
	"codeflux.dev/codeflux/internal/storage"
)

const (
	exitOK          = 0
	exitFailure     = 1
	exitUsage       = 2
	exitUnavailable = 3
)

func main() {
	os.Exit(run(os.Stdout, os.Stderr, os.Args[1:]))
}

func run(stdout, stderr io.Writer, args []string) int {
	if len(args) == 0 {
		printHelp(stderr)
		return exitUsage
	}
	switch args[0] {
	case "help", "--help", "-h":
		printHelp(stdout)
		return exitOK
	case "version":
		printVersion(stdout, buildinfo.Current())
		return exitOK
	case "doctor":
		return runDoctor(stdout, stderr, args[1:])
	case "backup":
		return runBackup(stdout, stderr, args[1:])
	case "integrity":
		return runIntegrity(stdout, stderr, args[1:])
	case "pause", "resume", "cancel":
		return runTaskControl(stdout, stderr, args[0], args[1:])
	default:
		fmt.Fprintf(stderr, "codeflux: unknown command %q\n", args[0])
		printHelp(stderr)
		return exitUsage
	}
}

func printHelp(output io.Writer) {
	fmt.Fprintln(output, "Usage: codeflux <command>")
	fmt.Fprintln(output)
	fmt.Fprintln(output, "Commands:")
	fmt.Fprintln(output, "  version  Print executable and schema identity")
	fmt.Fprintln(output, "  doctor   Check available local prerequisites")
	fmt.Fprintln(output, "  backup   Create an explicit SQLite recovery snapshot")
	fmt.Fprintln(output, "  integrity  Run a full SQLite integrity check")
	fmt.Fprintln(output, "  pause    Pause an active task at a safe checkpoint")
	fmt.Fprintln(output, "  resume   Resume a compatible paused task")
	fmt.Fprintln(output, "  cancel   Cancel an active or paused task")
}

func printVersion(output io.Writer, info buildinfo.Info) {
	fmt.Fprintf(output, "version: %s\n", info.Version)
	fmt.Fprintf(output, "commit: %s\n", info.Commit)
	fmt.Fprintf(output, "build-date: %s\n", info.BuildDate)
	fmt.Fprintf(output, "go-version: %s\n", info.GoVersion)
	fmt.Fprintf(output, "schema-version: %d\n", info.SchemaVersion)
	fmt.Fprintf(output, "frontend-version: %s\n", info.FrontendVersion)
}

func runDoctor(output, errorsOutput io.Writer, args []string) int {
	arguments, err := parseMaintenanceArguments(args, false)
	if err != nil {
		fmt.Fprintf(errorsOutput, "codeflux doctor: %v\n", err)
		return exitUsage
	}
	info := buildinfo.Current()
	fmt.Fprintf(output, "executable: ok (%s, %s)\n", info.Version, info.Commit)
	fmt.Fprintf(output, "go-runtime: ok (%s)\n", info.GoVersion)
	if _, err := exec.LookPath("git"); err == nil {
		fmt.Fprintln(output, "git: ok")
	} else {
		fmt.Fprintln(output, "git: missing")
	}
	path := arguments.database
	if path == "" {
		path, err = storage.DefaultDatabasePath()
		if err != nil {
			fmt.Fprintln(output, "storage: error (default location unavailable)")
			printCredentialStoreStatus(output)
			fmt.Fprintln(output, "browser-transport: unavailable (Milestone 06 not implemented)")
			return exitFailure
		}
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(output, "storage: missing (database has not been created)")
	} else if err != nil {
		fmt.Fprintln(output, "storage: error (database cannot be inspected)")
	} else {
		database, openErr := storage.Open(context.Background(), storage.OpenOptions{Path: path})
		if openErr != nil {
			fmt.Fprintln(output, "storage: error (database open failed)")
		} else {
			diagnostics, diagnosticErr := database.Diagnose(context.Background())
			closeErr := database.Close(context.Background())
			if diagnosticErr != nil || closeErr != nil {
				fmt.Fprintln(output, "storage: error (database checks failed)")
			} else {
				fmt.Fprintln(output, "storage: ok")
				fmt.Fprintf(output, "database-bytes: %d\n", diagnostics.DatabaseBytes)
				fmt.Fprintf(output, "sqlite-total-bytes: %d\n", diagnostics.TotalSQLiteBytes)
				fmt.Fprintf(output, "schema-version: %d\n", diagnostics.SchemaVersion)
				fmt.Fprintf(
					output,
					"supported-schema-version: %d\n",
					diagnostics.SupportedSchemaVersion,
				)
				fmt.Fprintf(
					output,
					"successful-migrations: %d\n",
					diagnostics.SuccessfulMigrations,
				)
				fmt.Fprintf(output, "failed-migrations: %d\n", diagnostics.FailedMigrations)
			}
		}
	}
	printCredentialStoreStatus(output)
	fmt.Fprintln(output, "browser-transport: unavailable (Milestone 06 not implemented)")
	return exitUnavailable
}

func printCredentialStoreStatus(output io.Writer) {
	if available, backend := credentials.PlatformStatus(); available {
		fmt.Fprintf(output, "credential-store: ok (%s)\n", backend)
	} else {
		fmt.Fprintf(output, "credential-store: unavailable (%s)\n", backend)
	}
}

func runBackup(output, errorsOutput io.Writer, args []string) int {
	arguments, err := parseMaintenanceArguments(args, true)
	if err != nil {
		fmt.Fprintf(errorsOutput, "codeflux backup: %v\n", err)
		return exitUsage
	}
	if arguments.output == "" {
		fmt.Fprintln(errorsOutput, "codeflux backup: --output is required")
		return exitUsage
	}
	database, code := openMaintenanceDatabase(errorsOutput, "backup", arguments.database)
	if code != exitOK {
		return code
	}
	if err := database.Backup(context.Background(), arguments.output); err != nil {
		_ = database.Close(context.Background())
		fmt.Fprintln(errorsOutput, "codeflux backup: snapshot failed")
		return exitFailure
	}
	if err := database.Close(context.Background()); err != nil {
		fmt.Fprintln(errorsOutput, "codeflux backup: database shutdown failed")
		return exitFailure
	}
	fmt.Fprintln(output, "backup: ok")
	return exitOK
}

func runIntegrity(output, errorsOutput io.Writer, args []string) int {
	arguments, err := parseMaintenanceArguments(args, false)
	if err != nil {
		fmt.Fprintf(errorsOutput, "codeflux integrity: %v\n", err)
		return exitUsage
	}
	database, code := openMaintenanceDatabase(errorsOutput, "integrity", arguments.database)
	if code != exitOK {
		return code
	}
	if err := database.IntegrityCheck(context.Background()); err != nil {
		_ = database.Close(context.Background())
		fmt.Fprintln(errorsOutput, "codeflux integrity: check failed")
		return exitFailure
	}
	if err := database.Close(context.Background()); err != nil {
		fmt.Fprintln(errorsOutput, "codeflux integrity: database shutdown failed")
		return exitFailure
	}
	fmt.Fprintln(output, "integrity: ok")
	return exitOK
}

type maintenanceArguments struct {
	database string
	output   string
}

func parseMaintenanceArguments(
	args []string,
	allowOutput bool,
) (maintenanceArguments, error) {
	var parsed maintenanceArguments
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--database":
			index++
			if index >= len(args) || args[index] == "" {
				return maintenanceArguments{}, errors.New("--database requires a path")
			}
			parsed.database = args[index]
		case "--output":
			if !allowOutput {
				return maintenanceArguments{}, errors.New("--output is not valid for this command")
			}
			index++
			if index >= len(args) || args[index] == "" {
				return maintenanceArguments{}, errors.New("--output requires a path")
			}
			parsed.output = args[index]
		default:
			return maintenanceArguments{}, fmt.Errorf("unknown argument %q", args[index])
		}
	}
	return parsed, nil
}

func openMaintenanceDatabase(
	errorsOutput io.Writer,
	command string,
	selected string,
) (*storage.Database, int) {
	path := selected
	var err error
	if path == "" {
		path, err = storage.DefaultDatabasePath()
		if err != nil {
			fmt.Fprintf(errorsOutput, "codeflux %s: default database unavailable\n", command)
			return nil, exitFailure
		}
	}
	if _, err := os.Stat(path); err != nil {
		fmt.Fprintf(errorsOutput, "codeflux %s: database unavailable\n", command)
		return nil, exitFailure
	}
	database, err := storage.Open(context.Background(), storage.OpenOptions{Path: path})
	if err != nil {
		fmt.Fprintf(errorsOutput, "codeflux %s: database open failed\n", command)
		return nil, exitFailure
	}
	return database, exitOK
}
