package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"codeflux.dev/codeflux/internal/buildinfo"
	"codeflux.dev/codeflux/internal/storage"
)

func main() {
	os.Exit(run(os.Stdout, os.Stderr, os.Args[1:]))
}

func run(stdout, stderr io.Writer, args []string) int {
	return dispatch(context.Background(), stdout, stderr, os.Stdin, args, openInBrowser)
}

// dispatch routes one invocation. Its dependencies are parameters so a test
// can drive the whole CLI without a terminal, a browser, or a real key store.
func dispatch(
	ctx context.Context,
	stdout, stderr io.Writer,
	stdin *os.File,
	args []string,
	openURL func(string) error,
) int {
	if len(args) == 0 {
		printHelp(stderr)
		return exitUsage
	}
	switch args[0] {
	case "help", "--help", "-h":
		// Contextual help: `help <command>` explains one command (M23-013).
		if len(args) > 1 {
			spec, ok := lookupCommand(args[1])
			if !ok {
				fmt.Fprintln(stderr, "codeflux help: "+unknownCommandMessage(args[1]))
				return exitUsage
			}
			printCommandHelp(stdout, spec)
			return exitOK
		}
		printHelp(stdout)
		return exitOK
	case "start":
		return runStart(ctx, stdout, stderr, stdin, args[1:], openURL)
	case "version":
		printVersion(stdout, buildinfo.Current())
		return exitOK
	case "doctor":
		return runDoctorChecks(ctx, stdout, stderr, args[1:])
	case "backup":
		return runBackup(stdout, stderr, args[1:])
	// "integrity" is retained as an alias. The plan names this command
	// integrity-check, but the shorter name already shipped, and silently
	// breaking a command someone has scripted is worse than carrying an alias.
	case "integrity-check", "integrity":
		return runIntegrity(stdout, stderr, args[1:])
	case "diagnostics":
		return runDiagnostics(ctx, stdout, stderr, args[1:])
	case "provider":
		return runProvider(ctx, stdout, stderr, stdin, args[1:], nil)
	case "pause", "resume", "cancel":
		return runTaskControl(stdout, stderr, args[0], args[1:])
	default:
		fmt.Fprintln(stderr, "codeflux: "+unknownCommandMessage(args[0]))
		printHelp(stderr)
		return exitUsage
	}
}

func printVersion(output io.Writer, info buildinfo.Info) {
	fmt.Fprintf(output, "version: %s\n", info.Version)
	fmt.Fprintf(output, "commit: %s\n", info.Commit)
	fmt.Fprintf(output, "build-date: %s\n", info.BuildDate)
	fmt.Fprintf(output, "go-version: %s\n", info.GoVersion)
	fmt.Fprintf(output, "schema-version: %d\n", info.SchemaVersion)
	fmt.Fprintf(output, "frontend-version: %s\n", info.FrontendVersion)
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
