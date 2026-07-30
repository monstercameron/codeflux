package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunLiveGateRequiresExplicitSafeInputsAndWarns(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	database := filepath.Join(t.TempDir(), "live.sqlite")
	code := runLiveGate(&stdout, &stderr, commandInvocation{
		JSON:          true,
		Provider:      "openai",
		CredentialRef: "os://codeflux/openai/default",
		Database:      database,
	})
	if code != exitUnavailable {
		t.Fatalf("run-live exit = %d, stderr=%q", code, stderr.String())
	}
	var result liveGateResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	if result.Provider != "openai" || result.CredentialRef != "os://codeflux/openai/default" ||
		result.Database != database || !strings.Contains(result.Warning, "REAL COST") {
		t.Fatalf("live gate result = %#v", result)
	}
}

func TestRunLiveGateRejectsRawSecretAndRepositoryDatabase(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	rawSecret := "os://sk-" + strings.Repeat("A", 24)
	code := runLiveGate(&stdout, &stderr, commandInvocation{
		Provider:      "openai",
		CredentialRef: rawSecret,
		Database:      filepath.Join(t.TempDir(), "live.sqlite"),
	})
	if code != exitUsage || !strings.Contains(stderr.String(), "non-secret") {
		t.Fatalf("raw-secret gate = %d, %q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	repository, err := repositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	code = runLiveGate(&stdout, &stderr, commandInvocation{
		Provider:      "anthropic",
		CredentialRef: "os://codeflux/anthropic/default",
		Database:      filepath.Join(repository, ".artifacts", "live.sqlite"),
	})
	if code != exitUsage || !strings.Contains(stderr.String(), "outside the source repository") {
		t.Fatalf("repository database gate = %d, %q", code, stderr.String())
	}
}

func TestParseCommandInvocationReadsLiveOptions(t *testing.T) {
	invocation, err := parseCommandInvocation([]string{
		"--provider=anthropic",
		"--credential-ref", "os://codeflux/anthropic/default",
		"--database", "C:\\data\\codeflux.sqlite",
		"--json",
	})
	if err != nil {
		t.Fatal(err)
	}
	if invocation.Provider != "anthropic" ||
		invocation.CredentialRef != "os://codeflux/anthropic/default" ||
		invocation.Database != "C:\\data\\codeflux.sqlite" ||
		!invocation.JSON {
		t.Fatalf("parsed invocation = %#v", invocation)
	}
}
