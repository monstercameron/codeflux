package main

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
)

const commandRegistrySchemaVersion = 1

type commandExitCode struct {
	Code    int    `json:"code"`
	Meaning string `json:"meaning"`
}

type commandSpec struct {
	Name            string            `json:"name"`
	Purpose         string            `json:"purpose"`
	Prerequisites   []string          `json:"prerequisites"`
	Arguments       []string          `json:"arguments"`
	ExitCodes       []commandExitCode `json:"exit_codes"`
	MachineReadable bool              `json:"machine_readable"`
	Availability    string            `json:"availability"`
}

type commandInvocation struct {
	Help          bool
	JSON          bool
	Once          bool
	Root          string
	Provider      string
	Model         string
	ModelRevision string
	CredentialRef string
	Database      string
	MinCoverage   string
	Positional    []string
}

type commandRegistryDocument struct {
	SchemaVersion int           `json:"schema_version"`
	Commands      []commandSpec `json:"commands"`
}

func developmentCommandRegistry() []commandSpec {
	commonExitCodes := []commandExitCode{
		{Code: exitSuccess, Meaning: "completed successfully"},
		{Code: exitFailure, Meaning: "command or exact sub-step failed"},
		{Code: exitUsage, Meaning: "arguments or command name are invalid"},
		{Code: exitUnavailable, Meaning: "declared subsystem is not implemented or available"},
	}
	specs := []commandSpec{
		{Name: "artifact-check", Purpose: "Reject repository-local disposable output outside .artifacts.", Prerequisites: []string{"Git"}, Arguments: []string{"[--root PATH]"}, ExitCodes: commonExitCodes, MachineReadable: false, Availability: "implemented"},
		{Name: "benchmark", Purpose: "Run a named M01 benchmark beneath the artifact root.", Prerequisites: []string{"Go"}, Arguments: []string{"[all|atom-names|generation]", "[--root PATH]"}, ExitCodes: commonExitCodes, MachineReadable: false, Availability: "implemented"},
		{Name: "bootstrap", Purpose: "Verify the pinned development toolchain and generators.", Prerequisites: []string{"Go", "Git", "network for uncached pinned tools"}, Arguments: []string{"[--root PATH]", "[--json]"}, ExitCodes: commonExitCodes, MachineReadable: true, Availability: "implemented"},
		{Name: "build", Purpose: "Verify generated output, then compile the command binaries and the browser client beneath one artifact root.", Prerequisites: []string{"Go", "Git"}, Arguments: []string{"[--root PATH]"}, ExitCodes: commonExitCodes, MachineReadable: false, Availability: "implemented"},
		{Name: "build-frontend", Purpose: "Generate the GWC-owned shell and compile the browser client to WebAssembly.", Prerequisites: []string{"Go", "GoWebComponents v5.0.1"}, Arguments: []string{"[--root PATH]"}, ExitCodes: commonExitCodes, MachineReadable: false, Availability: "implemented"},
		{Name: "build-spike", Purpose: "Generate the GWC-owned shell and compile the M06 browser spike to WebAssembly.", Prerequisites: []string{"Go", "GoWebComponents v5.0.1"}, Arguments: []string{"[--root PATH]"}, ExitCodes: commonExitCodes, MachineReadable: false, Availability: "implemented"},
		{Name: "ci-failure-artifact", Purpose: "Write allow-listed CI failure context.", Prerequisites: []string{"Go", "Git"}, Arguments: []string{"[--root PATH]"}, ExitCodes: commonExitCodes, MachineReadable: false, Availability: "implemented"},
		{Name: "doctor", Purpose: "Run environment and product subsystem diagnostics.", Prerequisites: []string{"Go", "Git"}, Arguments: []string{"[--root PATH]", "[--json]"}, ExitCodes: commonExitCodes, MachineReadable: true, Availability: "skeleton"},
		{Name: "generate", Purpose: "Regenerate every declared generated source family.", Prerequisites: []string{"Go", "network for uncached pinned tools"}, Arguments: []string{"[--root PATH]"}, ExitCodes: commonExitCodes, MachineReadable: false, Availability: "implemented"},
		{Name: "generate-check", Purpose: "Regenerate in isolation and reject committed drift.", Prerequisites: []string{"Go", "network for uncached pinned tools"}, Arguments: []string{"[--root PATH]"}, ExitCodes: commonExitCodes, MachineReadable: false, Availability: "implemented"},
		{Name: "inspect-db", Purpose: "Print a safe structured application database summary.", Prerequisites: []string{"implemented storage subsystem"}, Arguments: []string{"[database]", "[--root PATH]", "[--json]"}, ExitCodes: commonExitCodes, MachineReadable: true, Availability: "implemented"},
		{Name: "lint", Purpose: "Run format, vet, Staticcheck, protobuf, documentation, schema, and repository checks.", Prerequisites: []string{"Go", "Git", "Staticcheck 2026.1", "Buf 1.72.0"}, Arguments: []string{"[--root PATH]"}, ExitCodes: commonExitCodes, MachineReadable: false, Availability: "implemented"},
		{Name: "migration-check", Purpose: "Validate migration order, embedding, and generated checksums.", Prerequisites: []string{"Go"}, Arguments: []string{"[--root PATH]"}, ExitCodes: commonExitCodes, MachineReadable: false, Availability: "implemented"},
		{Name: "package", Purpose: "Build release packages beneath the artifact root.", Prerequisites: []string{"implemented packaging subsystem"}, Arguments: []string{"[--root PATH]", "[--json]"}, ExitCodes: commonExitCodes, MachineReadable: true, Availability: "skeleton"},
		{Name: "replay", Purpose: "Replay a redacted event fixture or development session.", Prerequisites: []string{"implemented event subsystem"}, Arguments: []string{"[fixture]", "[--root PATH]", "[--json]"}, ExitCodes: commonExitCodes, MachineReadable: true, Availability: "implemented"},
		{Name: "run", Purpose: "Start the isolated deterministic development profile.", Prerequisites: []string{"Go", "loopback TCP listener"}, Arguments: []string{"[--root PATH]", "[--once]", "[--json]"}, ExitCodes: commonExitCodes, MachineReadable: true, Availability: "implemented"},
		{Name: "run-live", Purpose: "Run one attributable opt-in live-provider smoke request.", Prerequisites: []string{"provider and credential adapters at M04/M12"}, Arguments: []string{"--provider NAME", "--model MODEL", "--model-revision REVISION", "--credential-ref os://REF", "--database ABSOLUTE_PATH", "[--root PATH]", "[--json]"}, ExitCodes: commonExitCodes, MachineReadable: true, Availability: "implemented"},
		{Name: "run-spike", Purpose: "Build and serve the M06 GWC transport spike on a random loopback port.", Prerequisites: []string{"Go", "GoWebComponents v5.0.1", "loopback TCP listener"}, Arguments: []string{"[--root PATH]"}, ExitCodes: commonExitCodes, MachineReadable: false, Availability: "implemented"},
		{Name: "release", Purpose: "Build self-contained executables with the browser assets compiled in.", Prerequisites: []string{"Go", "GoWebComponents v5.0.1", "network for uncached pinned tools"}, Arguments: []string{"[--root PATH]"}, ExitCodes: commonExitCodes, MachineReadable: false, Availability: "implemented"},
		{Name: "serve", Purpose: "Build the browser client and start codeflux against it on loopback.", Prerequisites: []string{"Go", "GoWebComponents v5.0.1", "loopback TCP listener"}, Arguments: []string{"[--root PATH]"}, ExitCodes: commonExitCodes, MachineReadable: false, Availability: "implemented"},
		{Name: "seed", Purpose: "Create a named deterministic development scenario.", Prerequisites: []string{"implemented storage and event subsystems"}, Arguments: []string{"[scenario]", "[--root PATH]", "[--json]"}, ExitCodes: commonExitCodes, MachineReadable: true, Availability: "implemented"},
		{Name: "test-all", Purpose: "Run the required current-scope local pre-submit suite.", Prerequisites: []string{"Go", "Git", "Staticcheck 2026.1", "pinned Buf"}, Arguments: []string{"[--root PATH]"}, ExitCodes: commonExitCodes, MachineReadable: false, Availability: "implemented"},
		{Name: "test-browser", Purpose: "Run locator-based responsive, accessibility, focus, and network browser checks.", Prerequisites: []string{"running loopback frontend", "Playwright driver v1.61.0", "Chromium browser"}, Arguments: []string{"[--root PATH]", "[--json]"}, ExitCodes: commonExitCodes, MachineReadable: true, Availability: "implemented"},
		{Name: "test-coverage", Purpose: "Run unit tests, write a coverage profile beneath the artifact root, and refuse below a --min-coverage floor (CODEFLUX_MIN_COVERAGE overrides when the flag is absent; the flag wins when both are set).", Prerequisites: []string{"Go"}, Arguments: []string{"[--root PATH]", "[--min-coverage PERCENT]"}, ExitCodes: commonExitCodes, MachineReadable: false, Availability: "implemented"},
		{Name: "test-fast", Purpose: "Run unit and pure package tests.", Prerequisites: []string{"Go"}, Arguments: []string{"[--root PATH]"}, ExitCodes: commonExitCodes, MachineReadable: false, Availability: "implemented"},
		{Name: "test-integration", Purpose: "Run current and integration-tagged package tests.", Prerequisites: []string{"Go"}, Arguments: []string{"[--root PATH]"}, ExitCodes: commonExitCodes, MachineReadable: false, Availability: "implemented"},
		{Name: "test-race", Purpose: "Run Go race tests on a supported host.", Prerequisites: []string{"Go race detector support"}, Arguments: []string{"[--root PATH]"}, ExitCodes: commonExitCodes, MachineReadable: false, Availability: "implemented"},
		{Name: "test-security", Purpose: "Run current path, permission, redaction, secret, artifact, instruction, atom, and credential cases.", Prerequisites: []string{"Go"}, Arguments: []string{"[--root PATH]"}, ExitCodes: commonExitCodes, MachineReadable: false, Availability: "implemented"},
	}
	sort.Slice(specs, func(i, j int) bool {
		return specs[i].Name < specs[j].Name
	})
	return specs
}

func findCommandSpec(name string) (commandSpec, bool) {
	for _, spec := range developmentCommandRegistry() {
		if spec.Name == name {
			return spec, true
		}
	}
	return commandSpec{}, false
}

func parseCommandInvocation(args []string) (commandInvocation, error) {
	var invocation commandInvocation
	for index := 0; index < len(args); index++ {
		argument := args[index]
		switch {
		case argument == "--help" || argument == "-h":
			invocation.Help = true
		case argument == "--json":
			invocation.JSON = true
		case argument == "--once":
			invocation.Once = true
		case argument == "--root":
			index++
			if index >= len(args) || args[index] == "" {
				return commandInvocation{}, fmt.Errorf("--root requires a path")
			}
			invocation.Root = args[index]
		case strings.HasPrefix(argument, "--root="):
			invocation.Root = strings.TrimPrefix(argument, "--root=")
			if invocation.Root == "" {
				return commandInvocation{}, fmt.Errorf("--root requires a path")
			}
		case argument == "--provider":
			index++
			if index >= len(args) || args[index] == "" {
				return commandInvocation{}, fmt.Errorf("--provider requires a value")
			}
			invocation.Provider = args[index]
		case strings.HasPrefix(argument, "--provider="):
			invocation.Provider = strings.TrimPrefix(argument, "--provider=")
		case argument == "--model":
			index++
			if index >= len(args) || args[index] == "" {
				return commandInvocation{}, fmt.Errorf("--model requires a value")
			}
			invocation.Model = args[index]
		case strings.HasPrefix(argument, "--model="):
			invocation.Model = strings.TrimPrefix(argument, "--model=")
		case argument == "--model-revision":
			index++
			if index >= len(args) || args[index] == "" {
				return commandInvocation{}, fmt.Errorf("--model-revision requires a value")
			}
			invocation.ModelRevision = args[index]
		case strings.HasPrefix(argument, "--model-revision="):
			invocation.ModelRevision = strings.TrimPrefix(argument, "--model-revision=")
		case argument == "--credential-ref":
			index++
			if index >= len(args) || args[index] == "" {
				return commandInvocation{}, fmt.Errorf("--credential-ref requires a value")
			}
			invocation.CredentialRef = args[index]
		case strings.HasPrefix(argument, "--credential-ref="):
			invocation.CredentialRef = strings.TrimPrefix(argument, "--credential-ref=")
		case argument == "--database":
			index++
			if index >= len(args) || args[index] == "" {
				return commandInvocation{}, fmt.Errorf("--database requires a value")
			}
			invocation.Database = args[index]
		case strings.HasPrefix(argument, "--database="):
			invocation.Database = strings.TrimPrefix(argument, "--database=")
		case argument == "--min-coverage":
			index++
			if index >= len(args) || args[index] == "" {
				return commandInvocation{}, fmt.Errorf("--min-coverage requires a value")
			}
			invocation.MinCoverage = args[index]
		case strings.HasPrefix(argument, "--min-coverage="):
			invocation.MinCoverage = strings.TrimPrefix(argument, "--min-coverage=")
			if invocation.MinCoverage == "" {
				return commandInvocation{}, fmt.Errorf("--min-coverage requires a value")
			}
		case strings.HasPrefix(argument, "-"):
			return commandInvocation{}, fmt.Errorf("unknown option %q", argument)
		default:
			invocation.Positional = append(invocation.Positional, argument)
		}
	}
	return invocation, nil
}

func printRegistry(output io.Writer, jsonOutput bool) error {
	registry := commandRegistryDocument{
		SchemaVersion: commandRegistrySchemaVersion,
		Commands:      developmentCommandRegistry(),
	}
	if jsonOutput {
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(registry)
	}
	fmt.Fprintln(output, "Usage: go run ./cmd/codeflux-dev <command> [options]")
	fmt.Fprintln(output)
	fmt.Fprintln(output, "Commands:")
	for _, spec := range registry.Commands {
		fmt.Fprintf(output, "  %-20s %s\n", spec.Name, spec.Purpose)
	}
	fmt.Fprintln(output)
	fmt.Fprintln(output, "Use '<command> --help' for prerequisites, arguments, and exit codes.")
	return nil
}

func printCommandHelp(output io.Writer, spec commandSpec, jsonOutput bool) error {
	if jsonOutput {
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(spec)
	}
	fmt.Fprintf(output, "Usage: go run ./cmd/codeflux-dev %s %s\n", spec.Name, strings.Join(spec.Arguments, " "))
	fmt.Fprintf(output, "\n%s\n", spec.Purpose)
	fmt.Fprintf(output, "\nAvailability: %s\n", spec.Availability)
	fmt.Fprintln(output, "Prerequisites:")
	for _, prerequisite := range spec.Prerequisites {
		fmt.Fprintf(output, "  - %s\n", prerequisite)
	}
	fmt.Fprintln(output, "Exit codes:")
	for _, exitCode := range spec.ExitCodes {
		fmt.Fprintf(output, "  %d  %s\n", exitCode.Code, exitCode.Meaning)
	}
	return nil
}

func runUnavailable(output io.Writer, spec commandSpec, invocation commandInvocation) int {
	if invocation.JSON {
		result := struct {
			SchemaVersion int    `json:"schema_version"`
			Command       string `json:"command"`
			Status        string `json:"status"`
			Reason        string `json:"reason"`
		}{
			SchemaVersion: 1,
			Command:       spec.Name,
			Status:        "unavailable",
			Reason:        "required subsystem is not implemented at the current repository milestone",
		}
		if err := json.NewEncoder(output).Encode(result); err != nil {
			return exitFailure
		}
		return exitUnavailable
	}
	fmt.Fprintf(output, "codeflux-dev %s: unavailable: required subsystem is not implemented at the current repository milestone\n", spec.Name)
	return exitUnavailable
}

func resolveCommandRoot(repositoryRoot, commandName, requested string) (string, error) {
	artifactRoot := filepath.Join(repositoryRoot, ".artifacts")
	target := requested
	if target == "" {
		target = filepath.Join(artifactRoot, commandName)
	} else if !filepath.IsAbs(target) {
		target = filepath.Join(repositoryRoot, target)
	}
	resolved, err := filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("resolve --root: %w", err)
	}
	repositoryRoot, err = filepath.Abs(repositoryRoot)
	if err != nil {
		return "", fmt.Errorf("resolve repository root: %w", err)
	}
	relative, err := filepath.Rel(repositoryRoot, resolved)
	if err != nil {
		if !strings.EqualFold(
			filepath.VolumeName(repositoryRoot),
			filepath.VolumeName(resolved),
		) {
			return resolved, nil
		}
		return "", fmt.Errorf("compare --root with repository: %w", err)
	}
	if relative == "." || relative == "" {
		return "", fmt.Errorf("--root must not be the repository root")
	}
	if relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		artifactRelative, err := filepath.Rel(artifactRoot, resolved)
		if err != nil || artifactRelative == "." || artifactRelative == "" ||
			artifactRelative == ".." || strings.HasPrefix(artifactRelative, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("repository-local --root must be a child of .artifacts")
		}
	}
	return resolved, nil
}
