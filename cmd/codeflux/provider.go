package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"codeflux.dev/codeflux/internal/credentials"
)

// providerCredentialService is the credential-store service name every
// provider credential is filed under.
const providerCredentialService = "codeflux-provider"

// supportedProviders is the closed set of provider names.
//
// It is closed so a typo becomes an error rather than a credential filed under
// a name nothing will ever read.
func supportedProviders() []string {
	return []string{"anthropic", "openai", "opencompat"}
}

// providerArguments are the parsed flags for the provider commands.
type providerArguments struct {
	name           string
	nonInteractive bool
}

func parseProviderArguments(args []string) (providerArguments, error) {
	remaining, nonInteractive := hasFlag(args, "--non-interactive")
	arguments := providerArguments{nonInteractive: nonInteractive}
	for index := 0; index < len(remaining); index++ {
		switch remaining[index] {
		case "--name":
			if index+1 >= len(remaining) {
				return providerArguments{}, errors.New("--name requires a value")
			}
			arguments.name = strings.TrimSpace(remaining[index+1])
			index++
		default:
			return providerArguments{}, fmt.Errorf("unknown flag %q", remaining[index])
		}
	}
	if arguments.name == "" {
		return providerArguments{}, errors.New("--name is required")
	}
	for _, supported := range supportedProviders() {
		if arguments.name == supported {
			return arguments, nil
		}
	}
	return providerArguments{}, fmt.Errorf(
		"unknown provider %q; supported providers are %s",
		arguments.name, strings.Join(supportedProviders(), ", "))
}

// runProvider dispatches the provider subcommands (M23-009, M23-010, M23-011).
func runProvider(
	ctx context.Context,
	stdout, stderr io.Writer,
	stdin *os.File,
	args []string,
	store credentials.Store,
) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "codeflux provider: expected set, test, or delete")
		return exitUsage
	}
	action := args[0]
	arguments, err := parseProviderArguments(args[1:])
	if err != nil {
		fmt.Fprintf(stderr, "codeflux provider %s: %v\n", action, err)
		return exitUsage
	}
	reference, err := credentials.NewReference(providerCredentialService, arguments.name)
	if err != nil {
		fmt.Fprintf(stderr, "codeflux provider %s: %v\n", action, err)
		return exitUsage
	}
	if store == nil {
		available, backend := credentials.PlatformStatus()
		if !available {
			fmt.Fprintf(stderr,
				"codeflux provider %s: no credential store is available on this system (%s)\n",
				action, backend)
			return exitUnavailable
		}
		store = credentials.NewPlatformStore()
	}

	switch action {
	case "set":
		return runProviderSet(ctx, stdout, stderr, stdin, arguments, reference, store)
	case "test":
		return runProviderTest(ctx, stdout, stderr, arguments, reference, store)
	case "delete":
		return runProviderDelete(ctx, stdout, stderr, arguments, reference, store)
	default:
		fmt.Fprintf(stderr, "codeflux provider: unknown action %q; expected set, test, or delete\n",
			action)
		return exitUsage
	}
}

// runProviderSet stores a credential (M23-009).
//
// The credential is read from standard input, never from a flag. A
// command-line argument is visible in the process table to every user on the
// machine and is written verbatim into shell history; neither is acceptable
// for a provider key.
func runProviderSet(
	ctx context.Context,
	stdout, stderr io.Writer,
	stdin *os.File,
	arguments providerArguments,
	reference credentials.Reference,
	store credentials.Store,
) int {
	policy := newInteractionPolicy(arguments.nonInteractive, stdin)
	if stdin == nil {
		fmt.Fprintln(stderr, "codeflux provider set: no input stream is available")
		return exitUsage
	}
	if policy.Interactive {
		// A terminal session is told where the value goes before it is typed.
		fmt.Fprintf(stdout, "paste the %s credential and press enter (it is not echoed to history)\n",
			arguments.name)
	}

	material, err := readCredentialLine(stdin)
	if err != nil {
		if errors.Is(err, io.EOF) {
			fmt.Fprintln(stderr,
				"codeflux provider set: no credential was supplied on standard input")
			if !policy.Interactive {
				return exitInteractionRequired
			}
			return exitUsage
		}
		fmt.Fprintln(stderr, "codeflux provider set: the credential could not be read")
		return exitFailure
	}
	secret, err := credentials.NewSecret(material)
	// The material is cleared as soon as it is copied into the opaque Secret,
	// so it does not linger in a buffer for the rest of the process.
	clearBytes(material)
	if err != nil {
		fmt.Fprintln(stderr, "codeflux provider set: the credential is empty or too long")
		return exitUsage
	}

	if err := store.Create(ctx, reference, secret); err != nil {
		// A credential that already exists is updated rather than refused:
		// rotating a key is the common case, and making the user delete first
		// would leave a window with no credential at all.
		if updateErr := store.Update(ctx, reference, secret); updateErr != nil {
			fmt.Fprintln(stderr, "codeflux provider set: the credential could not be stored")
			return exitFailure
		}
		fmt.Fprintf(stdout, "provider %s: credential updated\n", arguments.name)
		return exitOK
	}
	fmt.Fprintf(stdout, "provider %s: credential stored\n", arguments.name)
	return exitOK
}

// runProviderTest verifies a stored credential is present and usable by the
// store (M23-010).
//
// It deliberately does NOT call the provider. A local test that reached the
// network would fail for reasons that have nothing to do with the credential —
// no connectivity, a proxy, an outage — and would teach the user to distrust
// the result. Reaching the provider is `codeflux-dev run-live`'s job.
func runProviderTest(
	ctx context.Context,
	stdout, stderr io.Writer,
	arguments providerArguments,
	reference credentials.Reference,
	store credentials.Store,
) int {
	if err := store.Test(ctx, reference); err != nil {
		fmt.Fprintf(stderr, "provider %s: no usable credential is stored\n", arguments.name)
		return exitUnavailable
	}
	fmt.Fprintf(stdout, "provider %s: a credential is stored and readable\n", arguments.name)
	fmt.Fprintln(stdout, "this check does not contact the provider; it verifies local storage only")
	return exitOK
}

// runProviderDelete removes a stored credential (M23-011).
func runProviderDelete(
	ctx context.Context,
	stdout, stderr io.Writer,
	arguments providerArguments,
	reference credentials.Reference,
	store credentials.Store,
) int {
	if err := store.Delete(ctx, reference); err != nil {
		fmt.Fprintf(stderr, "provider %s: the credential could not be removed\n", arguments.name)
		return exitFailure
	}
	fmt.Fprintf(stdout, "provider %s: credential removed\n", arguments.name)
	// Deleting something that was not there is reported as success: the
	// requested end state holds either way, and failing would make cleanup
	// scripts fragile.
	return exitOK
}

// readCredentialLine reads one line without keeping it in a reusable buffer.
func readCredentialLine(stdin *os.File) ([]byte, error) {
	reader := bufio.NewReader(stdin)
	line, err := reader.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return nil, err
	}
	trimmed := strings.TrimRight(string(line), "\r\n")
	if strings.TrimSpace(trimmed) == "" {
		return nil, io.EOF
	}
	return []byte(trimmed), nil
}

func clearBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
