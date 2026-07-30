package worker

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

const workerEnvironmentHelper = "CODEFLUX_TEST_WORKER_ENVIRONMENT_HELPER"

func TestWorkerChildProcessReceivesNoRawProviderCredential(t *testing.T) {
	if os.Getenv(workerEnvironmentHelper) == "1" {
		if os.Getenv("OPENAI_API_KEY") != "" ||
			os.Getenv("CUSTOM_PROVIDER_ACCESS_TOKEN") != "" ||
			os.Getenv("SSH_AUTH_SOCK") != "" {
			t.Fatal("worker child received provider credential material")
		}
		if os.Getenv("CODEFLUX_WORKER_VISIBLE_FIXTURE") != "visible" {
			t.Fatal("worker child lost non-secret environment")
		}
		if os.Getenv("CODEFLUX_UNREVIEWED_FIXTURE") != "" {
			t.Fatal("worker child received an unreviewed environment value")
		}
		return
	}
	parent := append(
		os.Environ(),
		workerEnvironmentHelper+"=1",
		"OPENAI_API_KEY=worker-secret-fixture",
		"CUSTOM_PROVIDER_ACCESS_TOKEN=worker-token-fixture",
		"SSH_AUTH_SOCK=/credential-agent",
		"CODEFLUX_WORKER_VISIBLE_FIXTURE=visible",
		"CODEFLUX_UNREVIEWED_FIXTURE=hidden",
	)
	command := exec.Command(
		os.Args[0],
		"-test.run=^TestWorkerChildProcessReceivesNoRawProviderCredential$",
	)
	environment, err := BuildMinimumWorkerEnvironment(
		parent,
		[]string{workerEnvironmentHelper, "CODEFLUX_WORKER_VISIBLE_FIXTURE"},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	command.Env = environment
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("worker helper failed: %v\n%s", err, output)
	}
}

func TestSanitizeEnvironmentRemovesExplicitAndPatternNames(t *testing.T) {
	environment := SanitizeEnvironment([]string{
		"PATH=/tools",
		"OPENAI_API_KEY=fixture",
		"NPM_TOKEN=fixture",
		"VENDOR_PASSWORD=fixture",
		"MY_EXPLICIT_VALUE=fixture",
		"VISIBLE=value",
	}, []string{"MY_EXPLICIT_VALUE"})
	joined := strings.Join(environment, "\n")
	for _, forbidden := range []string{
		"OPENAI_API_KEY", "NPM_TOKEN", "VENDOR_PASSWORD", "MY_EXPLICIT_VALUE",
	} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("sanitized environment contains %s: %s", forbidden, joined)
		}
	}
	if !strings.Contains(joined, "PATH=/tools") || !strings.Contains(joined, "VISIBLE=value") {
		t.Fatalf("sanitized environment removed non-secret values: %s", joined)
	}
}

func TestMinimumWorkerEnvironmentAllowsOnlyRuntimeAndReviewedNames(t *testing.T) {
	environment, err := BuildMinimumWorkerEnvironment([]string{
		"PATH=/tools",
		"TEMP=/task-temp",
		"HOME=/user-home",
		"SSH_AUTH_SOCK=/credential-agent",
		"UNREVIEWED=value",
		"REVIEWED=value",
		"NPM_TOKEN=fixture",
	}, []string{"REVIEWED"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(environment, "\n")
	for _, present := range []string{"PATH=/tools", "TEMP=/task-temp", "REVIEWED=value"} {
		if !strings.Contains(joined, present) {
			t.Fatalf("minimum environment omitted %s: %s", present, joined)
		}
	}
	for _, absent := range []string{
		"HOME=", "SSH_AUTH_SOCK=", "UNREVIEWED=", "NPM_TOKEN=",
	} {
		if strings.Contains(joined, absent) {
			t.Fatalf("minimum environment retained %s: %s", absent, joined)
		}
	}
	for _, unsafe := range []string{"SSH_AUTH_SOCK", "NPM_TOKEN", "BAD-NAME"} {
		if _, err := BuildMinimumWorkerEnvironment(
			[]string{unsafe + "=value"}, []string{unsafe}, nil,
		); err == nil {
			t.Fatalf("unsafe explicit environment name was accepted: %s", unsafe)
		}
	}
}
