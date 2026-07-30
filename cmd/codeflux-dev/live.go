package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

type liveGateResult struct {
	SchemaVersion int    `json:"schema_version"`
	Status        string `json:"status"`
	Provider      string `json:"provider"`
	CredentialRef string `json:"credential_reference"`
	Database      string `json:"database"`
	Warning       string `json:"warning"`
	Reason        string `json:"reason"`
}

func runLiveGate(stdout, stderr io.Writer, invocation commandInvocation) int {
	if code := validateCommandRoot("run-live", invocation, stderr); code != exitSuccess {
		return code
	}
	if invocation.Provider == "" || invocation.CredentialRef == "" || invocation.Database == "" {
		fmt.Fprintln(stderr, "codeflux-dev run-live: --provider, --credential-ref, and --database are all required")
		return exitUsage
	}
	if invocation.Provider != "openai" && invocation.Provider != "anthropic" {
		fmt.Fprintf(stderr, "codeflux-dev run-live: unsupported explicit provider %q\n", invocation.Provider)
		return exitUsage
	}
	if !strings.HasPrefix(invocation.CredentialRef, "os://") ||
		strings.ContainsAny(invocation.CredentialRef, "\r\n") ||
		looksLikeProviderSecret(invocation.CredentialRef) {
		fmt.Fprintln(stderr, "codeflux-dev run-live: credential reference must be a non-secret os:// reference")
		return exitUsage
	}
	repository, err := repositoryRoot()
	if err != nil {
		fmt.Fprintf(stderr, "codeflux-dev run-live: repository: %v\n", err)
		return exitFailure
	}
	database, err := filepath.Abs(invocation.Database)
	if err != nil || !filepath.IsAbs(invocation.Database) {
		fmt.Fprintln(stderr, "codeflux-dev run-live: --database must be an absolute non-test path")
		return exitUsage
	}
	relative, err := filepath.Rel(repository, database)
	if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		fmt.Fprintln(stderr, "codeflux-dev run-live: --database must be outside the source repository")
		return exitUsage
	}
	const warning = "LIVE PROVIDER REQUESTS CAN INCUR REAL COST; no request is sent until the provider runtime is implemented and explicitly started"
	result := liveGateResult{
		SchemaVersion: 1,
		Status:        "unavailable",
		Provider:      invocation.Provider,
		CredentialRef: invocation.CredentialRef,
		Database:      database,
		Warning:       warning,
		Reason:        "provider and operating-system credential adapters are implemented by M04 and M12",
	}
	if invocation.JSON {
		if err := json.NewEncoder(stdout).Encode(result); err != nil {
			fmt.Fprintf(stderr, "codeflux-dev run-live: encode result: %v\n", err)
			return exitFailure
		}
	} else {
		fmt.Fprintf(stdout, "WARNING: %s\n", warning)
		fmt.Fprintf(stdout, "Provider: %s\nCredential reference: %s\nDatabase: %s\n", result.Provider, result.CredentialRef, result.Database)
		fmt.Fprintf(stderr, "codeflux-dev run-live: unavailable: %s\n", result.Reason)
	}
	return exitUnavailable
}

func looksLikeProviderSecret(value string) bool {
	for _, secret := range secretPatterns {
		if secret.pattern.Match(bytes.TrimSpace([]byte(value))) {
			return true
		}
	}
	return false
}
