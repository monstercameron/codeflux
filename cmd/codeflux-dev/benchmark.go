package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// benchmarkTarget is one named benchmark selection: which package to run and
// which benchmarks within it.
type benchmarkTarget struct {
	packagePath string
	pattern     string
}

var benchmarkPatterns = map[string]benchmarkTarget{
	"all":        {packagePath: "./cmd/codeflux-dev", pattern: "."},
	"atom-names": {packagePath: "./cmd/codeflux-dev", pattern: "SplitGoIdentifier"},
	"generation": {packagePath: "./cmd/codeflux-dev", pattern: "RenderMigrationCatalog"},
	// M22-076..087. The performance suite lives in its own package so every
	// measurement shares one environment capture and one set of recorded
	// dimensions; see docs/benchmarks.md.
	"performance": {packagePath: "./internal/benchmarks", pattern: "."},
	"graph":       {packagePath: "./internal/graphlayout", pattern: "BenchmarkM19"},
}

func runBenchmark(
	ctx context.Context,
	stdout io.Writer,
	stderr io.Writer,
	invocation commandInvocation,
) int {
	repository, err := repositoryRoot()
	if err != nil {
		fmt.Fprintf(stderr, "codeflux-dev benchmark: repository: %v\n", err)
		return exitFailure
	}
	outputRoot, err := resolveCommandRoot(repository, "bench", invocation.Root)
	if err != nil {
		fmt.Fprintf(stderr, "codeflux-dev benchmark: root: %v\n", err)
		return exitUsage
	}
	name := "all"
	if len(invocation.Positional) == 1 {
		name = invocation.Positional[0]
	}
	target, ok := benchmarkPatterns[name]
	if !ok {
		fmt.Fprintf(
			stderr,
			"codeflux-dev benchmark: unknown benchmark %q; choose all, atom-names, generation, performance, or graph\n",
			name,
		)
		return exitUsage
	}
	if err := os.MkdirAll(outputRoot, 0o755); err != nil {
		fmt.Fprintf(stderr, "codeflux-dev benchmark: create output root: %v\n", err)
		return exitFailure
	}
	outputPath := filepath.Join(outputRoot, name+".txt")
	file, err := os.OpenFile(outputPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		fmt.Fprintf(stderr, "codeflux-dev benchmark: create output: %v\n", err)
		return exitFailure
	}
	writer := io.MultiWriter(stdout, file)
	code := runGoIn(
		ctx,
		repository,
		writer,
		stderr,
		"test",
		target.packagePath,
		"-run",
		"^$",
		"-bench",
		target.pattern,
		"-benchmem",
	)
	if err := file.Close(); err != nil {
		fmt.Fprintf(stderr, "codeflux-dev benchmark: close output: %v\n", err)
		return exitFailure
	}
	if code != exitSuccess {
		fmt.Fprintln(stderr, "codeflux-dev benchmark: sub-step go-benchmark failed")
		return code
	}
	fmt.Fprintf(stdout, "codeflux-dev benchmark: retained %s\n", outputPath)
	return exitSuccess
}

func runMigrationCheck(
	ctx context.Context,
	stdout io.Writer,
	stderr io.Writer,
	invocation commandInvocation,
) int {
	if code := validateCommandRoot("migration-check", invocation, stderr); code != exitSuccess {
		return code
	}
	repository, err := repositoryRoot()
	if err != nil {
		fmt.Fprintf(stderr, "codeflux-dev migration-check: repository: %v\n", err)
		return exitFailure
	}
	if err := checkMigrationNames(repository); err != nil {
		fmt.Fprintf(stderr, "codeflux-dev migration-check: migration-order: %v\n", err)
		return exitFailure
	}
	if code := runGoIn(ctx, repository, stdout, stderr, "test", "./migrations"); code != exitSuccess {
		fmt.Fprintln(stderr, "codeflux-dev migration-check: sub-step migration-tests failed")
		return code
	}
	return exitSuccess
}
