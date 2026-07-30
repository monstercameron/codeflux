package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"

	"codeflux.dev/codeflux/internal/buildinfo"
)

const (
	exitOK          = 0
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
		return runDoctor(stdout)
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
}

func printVersion(output io.Writer, info buildinfo.Info) {
	fmt.Fprintf(output, "version: %s\n", info.Version)
	fmt.Fprintf(output, "commit: %s\n", info.Commit)
	fmt.Fprintf(output, "build-date: %s\n", info.BuildDate)
	fmt.Fprintf(output, "go-version: %s\n", info.GoVersion)
	fmt.Fprintf(output, "schema-version: %d\n", info.SchemaVersion)
	fmt.Fprintf(output, "frontend-version: %s\n", info.FrontendVersion)
}

func runDoctor(output io.Writer) int {
	info := buildinfo.Current()
	fmt.Fprintf(output, "executable: ok (%s, %s)\n", info.Version, info.Commit)
	fmt.Fprintf(output, "go-runtime: ok (%s)\n", info.GoVersion)
	if path, err := exec.LookPath("git"); err == nil {
		fmt.Fprintf(output, "git: ok (%s)\n", path)
	} else {
		fmt.Fprintln(output, "git: missing")
	}
	fmt.Fprintln(output, "storage: unavailable (Milestone 03 not implemented)")
	fmt.Fprintln(output, "credential-store: unavailable (Milestone 04 not implemented)")
	fmt.Fprintln(output, "browser-transport: unavailable (Milestone 06 not implemented)")
	return exitUnavailable
}
