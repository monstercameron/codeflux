package main

import (
	"context"
	"fmt"
	"io"
)

func runIntegrationTests(
	ctx context.Context,
	stdout io.Writer,
	stderr io.Writer,
	invocation commandInvocation,
) int {
	if code := validateCommandRoot("test-integration", invocation, stderr); code != exitSuccess {
		return code
	}
	return runGo(ctx, stdout, stderr, "test", "-tags=integration", "./...")
}

func runSecurityTests(
	ctx context.Context,
	stdout io.Writer,
	stderr io.Writer,
	invocation commandInvocation,
) int {
	if code := validateCommandRoot("test-security", invocation, stderr); code != exitSuccess {
		return code
	}
	const securityPattern = "Security|Secret|Path|Origin|Permission|Redaction|Payload|Artifact|Instruction|Atom|Credential"
	return runGo(ctx, stdout, stderr, "test", "./...", "-run", securityPattern)
}

func runAllTests(
	ctx context.Context,
	stdout io.Writer,
	stderr io.Writer,
	invocation commandInvocation,
) int {
	if code := validateCommandRoot("test-all", invocation, stderr); code != exitSuccess {
		return code
	}
	steps := []struct {
		name string
		run  func() int
	}{
		{name: "lint", run: func() int { return runLint(ctx, stdout, stderr, invocation) }},
		{name: "generate-check", run: func() int {
			return runGenerateCheck(ctx, stdout, stderr, invocation)
		}},
		{name: "test-fast", run: func() int {
			return runGo(ctx, stdout, stderr, "test", "./...")
		}},
		{name: "test-integration", run: func() int {
			return runIntegrationTests(ctx, stdout, stderr, invocation)
		}},
		{name: "test-security", run: func() int {
			return runSecurityTests(ctx, stdout, stderr, invocation)
		}},
	}
	for _, step := range steps {
		fmt.Fprintf(stdout, "codeflux-dev test-all: %s\n", step.name)
		if code := step.run(); code != exitSuccess {
			fmt.Fprintf(stderr, "codeflux-dev test-all: sub-step %s failed\n", step.name)
			return code
		}
	}
	fmt.Fprintln(stdout, "codeflux-dev test-all: browser tests are opt-in through test-browser against a running local frontend; race tests run in the declared Ubuntu CI job")
	return exitSuccess
}

func validateCommandRoot(command string, invocation commandInvocation, stderr io.Writer) int {
	repository, err := repositoryRoot()
	if err != nil {
		fmt.Fprintf(stderr, "codeflux-dev %s: repository: %v\n", command, err)
		return exitFailure
	}
	if _, err := resolveCommandRoot(repository, command, invocation.Root); err != nil {
		fmt.Fprintf(stderr, "codeflux-dev %s: root: %v\n", command, err)
		return exitUsage
	}
	return exitSuccess
}
