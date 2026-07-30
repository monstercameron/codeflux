package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

const ciFailureArtifactSchemaVersion = 1

type ciFailureArtifact struct {
	SchemaVersion int    `json:"schema_version"`
	Commit        string `json:"commit"`
	GoVersion     string `json:"go_version"`
	OperatingSys  string `json:"operating_system"`
	Architecture  string `json:"architecture"`
}

func runCIFailureArtifact(
	ctx context.Context,
	stderr io.Writer,
	invocation commandInvocation,
) int {
	root, err := repositoryRoot()
	if err != nil {
		fmt.Fprintf(stderr, "codeflux-dev ci-failure-artifact: %v\n", err)
		return exitFailure
	}
	outputDir, err := resolveCommandRoot(root, "test-failures", invocation.Root)
	if err != nil {
		fmt.Fprintf(stderr, "codeflux-dev ci-failure-artifact: root: %v\n", err)
		return exitUsage
	}
	commit, err := gitOutput(ctx, root, "rev-parse", "HEAD")
	if err != nil {
		fmt.Fprintf(stderr, "codeflux-dev ci-failure-artifact: %v\n", err)
		return exitFailure
	}
	if err := writeCIFailureArtifact(outputDir, ciFailureArtifact{
		SchemaVersion: ciFailureArtifactSchemaVersion,
		Commit:        commit,
		GoVersion:     runtime.Version(),
		OperatingSys:  runtime.GOOS,
		Architecture:  runtime.GOARCH,
	}); err != nil {
		fmt.Fprintf(stderr, "codeflux-dev ci-failure-artifact: %v\n", err)
		return exitFailure
	}
	return exitSuccess
}

func writeCIFailureArtifact(outputDir string, artifact ciFailureArtifact) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create failure artifact directory: %w", err)
	}
	content, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return fmt.Errorf("encode failure artifact: %w", err)
	}
	content = append(content, '\n')
	if err := os.WriteFile(filepath.Join(outputDir, "context.json"), content, 0o600); err != nil {
		return fmt.Errorf("write failure artifact: %w", err)
	}
	return nil
}
