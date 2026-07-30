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
			os.Getenv("CUSTOM_PROVIDER_ACCESS_TOKEN") != "" {
			t.Fatal("worker child received provider credential material")
		}
		if os.Getenv("CODEFLUX_WORKER_VISIBLE_FIXTURE") != "visible" {
			t.Fatal("worker child lost non-secret environment")
		}
		return
	}
	parent := append(
		os.Environ(),
		workerEnvironmentHelper+"=1",
		"OPENAI_API_KEY=worker-secret-fixture",
		"CUSTOM_PROVIDER_ACCESS_TOKEN=worker-token-fixture",
		"CODEFLUX_WORKER_VISIBLE_FIXTURE=visible",
	)
	command := exec.Command(
		os.Args[0],
		"-test.run=^TestWorkerChildProcessReceivesNoRawProviderCredential$",
	)
	command.Env = SanitizeEnvironment(parent, nil)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("worker helper failed: %v\n%s", err, output)
	}
}

func TestSanitizeEnvironmentRemovesExplicitAndPatternNames(t *testing.T) {
	environment := SanitizeEnvironment([]string{
		"PATH=/tools",
		"OPENAI_API_KEY=fixture",
		"VENDOR_PASSWORD=fixture",
		"MY_EXPLICIT_VALUE=fixture",
		"VISIBLE=value",
	}, []string{"MY_EXPLICIT_VALUE"})
	joined := strings.Join(environment, "\n")
	for _, forbidden := range []string{"OPENAI_API_KEY", "VENDOR_PASSWORD", "MY_EXPLICIT_VALUE"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("sanitized environment contains %s: %s", forbidden, joined)
		}
	}
	if !strings.Contains(joined, "PATH=/tools") || !strings.Contains(joined, "VISIBLE=value") {
		t.Fatalf("sanitized environment removed non-secret values: %s", joined)
	}
}
