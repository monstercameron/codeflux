package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"codeflux.dev/codeflux/internal/buildinfo"
	"codeflux.dev/codeflux/internal/credentials"
	"codeflux.dev/codeflux/internal/storage"
)

// DiagnosticBundle is the M23-008 export.
//
// Every field is a version, a count, a boolean, or an enumerated status.
// Nothing here is user content: not a requirement, not a file path from a
// repository, not a command line, not model output. That is what makes the
// bundle safe to attach to a bug report without reading it first, which is how
// people actually use one.
type DiagnosticBundle struct {
	SchemaVersion int `json:"schema_version"`

	Executable ExecutableIdentity `json:"executable"`
	Storage    StorageDiagnostic  `json:"storage"`
	Host       HostDiagnostic     `json:"host"`
	// Prerequisites are the same checks `doctor` reports, so a bundle and a
	// doctor run cannot disagree.
	Prerequisites map[string]string `json:"prerequisites"`
}

// ExecutableIdentity is the running build.
type ExecutableIdentity struct {
	Version         string `json:"version"`
	Commit          string `json:"commit"`
	BuildDate       string `json:"build_date"`
	GoVersion       string `json:"go_version"`
	SchemaVersion   uint32 `json:"schema_version"`
	FrontendVersion string `json:"frontend_version"`
}

// StorageDiagnostic describes the database without revealing its contents.
type StorageDiagnostic struct {
	Present bool `json:"present"`
	// Location is deliberately absent. A database path routinely contains a
	// user's name, and an export that carried it would leak an identity into
	// every attached bug report.
	DatabaseBytes          uint64 `json:"database_bytes"`
	TotalSQLiteBytes       uint64 `json:"total_sqlite_bytes"`
	SchemaVersion          int    `json:"schema_version"`
	SupportedSchemaVersion int    `json:"supported_schema_version"`
	SuccessfulMigrations   uint64 `json:"successful_migrations"`
	FailedMigrations       uint64 `json:"failed_migrations"`
	Status                 string `json:"status"`
}

// HostDiagnostic describes the machine in the terms that affect behaviour.
type HostDiagnostic struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
	CPUs int    `json:"cpus"`
}

// diagnosticBundleSchemaVersion versions the export format itself, so a reader
// receiving an old bundle knows what to expect.
const diagnosticBundleSchemaVersion = 1

// diagnosticsArguments are the parsed flags.
type diagnosticsArguments struct {
	database string
	output   string
}

func parseDiagnosticsArguments(args []string) (diagnosticsArguments, error) {
	var arguments diagnosticsArguments
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--database", "--output":
			name := args[index]
			if index+1 >= len(args) {
				return diagnosticsArguments{}, fmt.Errorf("%s requires a value", name)
			}
			value := strings.TrimSpace(args[index+1])
			index++
			if value == "" {
				return diagnosticsArguments{}, fmt.Errorf("%s requires a non-empty value", name)
			}
			if name == "--database" {
				arguments.database = value
			} else {
				arguments.output = value
			}
		default:
			return diagnosticsArguments{}, fmt.Errorf("unknown flag %q", args[index])
		}
	}
	if arguments.output == "" {
		return diagnosticsArguments{}, errors.New("--output is required")
	}
	return arguments, nil
}

// runDiagnostics implements `codeflux diagnostics export` (M23-008).
func runDiagnostics(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	if len(args) == 0 || args[0] != "export" {
		fmt.Fprintln(stderr, "codeflux diagnostics: expected export")
		return exitUsage
	}
	arguments, err := parseDiagnosticsArguments(args[1:])
	if err != nil {
		fmt.Fprintf(stderr, "codeflux diagnostics export: %v\n", err)
		return exitUsage
	}

	bundle := collectDiagnosticBundle(ctx, arguments.database)
	encoded, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		fmt.Fprintln(stderr, "codeflux diagnostics export: the bundle could not be encoded")
		return exitFailure
	}

	// The bundle is scanned before it is written. Nothing in the structure
	// should be able to carry a credential, and this check is what makes that
	// a verified property rather than an intention.
	if err := assertBundleCarriesNoCredential(encoded); err != nil {
		fmt.Fprintf(stderr, "codeflux diagnostics export: %v\n", err)
		return exitFailure
	}

	// An existing file is never overwritten: a diagnostic export is evidence,
	// and silently replacing the previous one destroys the comparison a reader
	// most often wants.
	file, err := os.OpenFile(arguments.output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			fmt.Fprintf(stderr,
				"codeflux diagnostics export: %s already exists; choose another path\n",
				arguments.output)
			return exitUsage
		}
		fmt.Fprintln(stderr, "codeflux diagnostics export: the bundle could not be created")
		return exitFailure
	}
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		_ = file.Close()
		fmt.Fprintln(stderr, "codeflux diagnostics export: the bundle could not be written")
		return exitFailure
	}
	if err := file.Close(); err != nil {
		fmt.Fprintln(stderr, "codeflux diagnostics export: the bundle could not be closed")
		return exitFailure
	}
	fmt.Fprintf(stdout, "diagnostics: written to %s\n", arguments.output)
	fmt.Fprintln(stdout, "the bundle carries versions, counts, and statuses only; no requirement, path, or output")
	return exitOK
}

// collectDiagnosticBundle gathers everything the export reports.
func collectDiagnosticBundle(ctx context.Context, databasePath string) DiagnosticBundle {
	info := buildinfo.Current()
	bundle := DiagnosticBundle{
		SchemaVersion: diagnosticBundleSchemaVersion,
		Executable: ExecutableIdentity{
			Version: info.Version, Commit: info.Commit, BuildDate: info.BuildDate,
			GoVersion: info.GoVersion, SchemaVersion: info.SchemaVersion,
			FrontendVersion: info.FrontendVersion,
		},
		Host:          collectHostDiagnostic(),
		Prerequisites: map[string]string{},
	}

	if _, err := exec.LookPath("git"); err == nil {
		bundle.Prerequisites["git"] = "ok"
	} else {
		bundle.Prerequisites["git"] = "missing"
	}
	if available, backend := credentials.PlatformStatus(); available {
		bundle.Prerequisites["credential-store"] = "ok (" + backend + ")"
	} else {
		bundle.Prerequisites["credential-store"] = "unavailable (" + backend + ")"
	}

	bundle.Storage = collectStorageDiagnostic(ctx, databasePath)
	bundle.Prerequisites["storage"] = bundle.Storage.Status
	return bundle
}

func collectStorageDiagnostic(ctx context.Context, databasePath string) StorageDiagnostic {
	if databasePath == "" {
		resolved, err := storage.DefaultDatabasePath()
		if err != nil {
			return StorageDiagnostic{Status: "error (no default location)"}
		}
		databasePath = resolved
	}
	if _, err := os.Stat(databasePath); errors.Is(err, os.ErrNotExist) {
		return StorageDiagnostic{Status: "missing"}
	} else if err != nil {
		return StorageDiagnostic{Status: "error (cannot inspect)"}
	}

	database, err := storage.Open(ctx, storage.OpenOptions{Path: databasePath})
	if err != nil {
		return StorageDiagnostic{Present: true, Status: "error (open failed)"}
	}
	diagnostics, diagnoseErr := database.Diagnose(ctx)
	closeErr := database.Close(ctx)
	if diagnoseErr != nil || closeErr != nil {
		return StorageDiagnostic{Present: true, Status: "error (checks failed)"}
	}
	return StorageDiagnostic{
		Present:                true,
		DatabaseBytes:          diagnostics.DatabaseBytes,
		TotalSQLiteBytes:       diagnostics.TotalSQLiteBytes,
		SchemaVersion:          diagnostics.SchemaVersion,
		SupportedSchemaVersion: diagnostics.SupportedSchemaVersion,
		SuccessfulMigrations:   diagnostics.SuccessfulMigrations,
		FailedMigrations:       diagnostics.FailedMigrations,
		Status:                 "ok",
	}
}

// assertBundleCarriesNoCredential scans the encoded bundle.
//
// It looks for the shapes a credential takes rather than for a specific value,
// because the export is written on a user's machine where the real credentials
// are the ones that matter.
func assertBundleCarriesNoCredential(encoded []byte) error {
	text := string(encoded)
	// A credential-shaped label appearing in a bundle whose every field is a
	// version or a count means a field was added without thinking about this.
	for _, marker := range []string{
		"api_key", "apikey", "secret", "password", "token", "authorization",
		"sk-", "AKIA", "xoxb-", "github_pat_",
	} {
		if strings.Contains(strings.ToLower(text), strings.ToLower(marker)) {
			return fmt.Errorf(
				"the bundle contains %q, which no diagnostic field should ever hold", marker)
		}
	}
	return nil
}

func collectHostDiagnostic() HostDiagnostic {
	return HostDiagnostic{
		OS: runtime.GOOS, Arch: runtime.GOARCH, CPUs: runtime.NumCPU(),
	}
}
