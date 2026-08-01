// Package testharness assembles whole-system fixtures: a real coordinator over
// a real database and a real repository, driven by deterministic fakes.
//
// It lives apart from internal/testfixtures because it depends on the packages
// under test. testfixtures stays dependency-light so anything may use it;
// testharness is allowed to wire the world together.
package testharness

import (
	"context"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"codeflux.dev/codeflux/internal/coordinator"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/testfixtures"
)

// CoordinatorHarness is one isolated coordinator with everything it needs
// (M22-110): its own database, repository, worker surface, provider, ports,
// clock, and identifier sequence.
//
// Isolation is the point. Two harnesses running concurrently must not be able
// to observe each other, or a test suite's failures become a function of which
// tests happened to run together.
type CoordinatorHarness struct {
	Application *coordinator.Application
	Repository  testfixtures.StatefulRepository
	Provider    *testfixtures.StepProvider
	Credentials *testfixtures.FakeCredentialStore
	Events      *testfixtures.EventRecorder
	Clock       *testfixtures.SteppingClock
	IDs         *testfixtures.SequenceIDGenerator

	root      string
	preserve  bool
	closeOnce sync.Once
	closeErr  error
}

// CoordinatorOptions configures a harness.
type CoordinatorOptions struct {
	// Root is the directory the harness owns. It must be temporary: the
	// harness deletes it recursively on Close and refuses anything else.
	Root string
	// RepositoryState is the git condition the fixture repository is built in.
	// Defaults to clean.
	RepositoryState testfixtures.RepositoryState
	// ProviderScript is the model script. Defaults to full coverage, so a
	// harness a test forgot to script still exercises the whole surface
	// rather than silently doing nothing.
	ProviderScript []testfixtures.ProviderStep
	// ClockStep is how far the deterministic clock advances per read.
	ClockStep time.Duration
	// IDPrefix seeds the deterministic identifier sequence.
	IDPrefix string
	// PreserveOnClose keeps the harness directory for inspection.
	PreserveOnClose bool
}

// NewCoordinatorHarness builds and starts an isolated coordinator (M22-110).
func NewCoordinatorHarness(
	ctx context.Context,
	options CoordinatorOptions,
) (*CoordinatorHarness, error) {
	if strings.TrimSpace(options.Root) == "" {
		return nil, errors.New("a coordinator harness requires a root directory")
	}
	if err := testfixtures.ValidateCleanupTarget(options.Root); err != nil {
		return nil, fmt.Errorf("harness root is not safely cleanable: %w", err)
	}
	if options.RepositoryState == "" {
		options.RepositoryState = testfixtures.StateClean
	}
	if len(options.ProviderScript) == 0 {
		options.ProviderScript = testfixtures.FullCoverageScript()
	}
	if options.ClockStep <= 0 {
		options.ClockStep = time.Millisecond
	}
	if strings.TrimSpace(options.IDPrefix) == "" {
		options.IDPrefix = "hrn"
	}

	harness := &CoordinatorHarness{root: options.Root, preserve: options.PreserveOnClose}

	clock, err := testfixtures.NewSteppingClock(options.ClockStep)
	if err != nil {
		return nil, fmt.Errorf("build harness clock: %w", err)
	}
	harness.Clock = clock

	identifiers, err := testfixtures.NewSequenceIDGenerator(options.IDPrefix)
	if err != nil {
		return nil, fmt.Errorf("build harness identifiers: %w", err)
	}
	harness.IDs = identifiers

	repository, err := testfixtures.NewStatefulRepository(
		ctx, filepath.Join(options.Root, "repository"), options.RepositoryState)
	if err != nil {
		return nil, fmt.Errorf("build harness repository: %w", err)
	}
	harness.Repository = repository

	provider, err := testfixtures.NewStepProvider(options.ProviderScript...)
	if err != nil {
		return nil, fmt.Errorf("build harness provider: %w", err)
	}
	harness.Provider = provider

	credentials := testfixtures.NewFakeCredentialStore()
	if err := credentials.Seed(); err != nil {
		return nil, fmt.Errorf("seed harness credentials: %w", err)
	}
	harness.Credentials = credentials
	harness.Events = testfixtures.NewEventRecorder(clock)

	// Port 0 on loopback: the OS assigns a free port, so two harnesses never
	// collide and no test has to know a port number in advance.
	application, err := coordinator.StartApplication(ctx, coordinator.ApplicationOptions{
		DatabasePath:      filepath.Join(options.Root, "coordinator.sqlite3"),
		BackupDirectory:   filepath.Join(options.Root, "backups"),
		ListenAddress:     "127.0.0.1:0",
		TaskListenAddress: "127.0.0.1:0",
		Now:               clock.Now,
	})
	if err != nil {
		return nil, fmt.Errorf("start harness coordinator: %w", err)
	}
	harness.Application = application
	return harness, nil
}

// Repositories exposes the durable store the harness's coordinator owns.
func (harness *CoordinatorHarness) Repositories() *storage.Repositories {
	return harness.Application.Repositories()
}

// BrowserAddress is the loopback address the frontend server is listening on.
func (harness *CoordinatorHarness) BrowserAddress() string {
	return harness.Application.Address()
}

// TaskControlAddress is the loopback address the gRPC task surface listens on.
func (harness *CoordinatorHarness) TaskControlAddress() string {
	return harness.Application.TaskControlAddress()
}

// AssertIsolated checks the harness bound loopback-only ephemeral ports.
//
// A harness that bound a fixed port or a non-loopback interface would make the
// suite order-dependent and would expose a running agent to the network, so
// this is asserted rather than assumed.
func (harness *CoordinatorHarness) AssertIsolated() error {
	for name, address := range map[string]string{
		"browser":      harness.BrowserAddress(),
		"task control": harness.TaskControlAddress(),
	} {
		if address == "" {
			return fmt.Errorf("%s surface is not listening", name)
		}
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return fmt.Errorf("parse %s address %q: %w", name, address, err)
		}
		parsed := net.ParseIP(strings.Trim(host, "[]"))
		if parsed == nil || !parsed.IsLoopback() {
			return fmt.Errorf("%s surface bound non-loopback host %q", name, host)
		}
		if port == "0" || port == "" {
			return fmt.Errorf("%s surface reports no assigned port", name)
		}
	}
	return nil
}

// AssertNoCredentialLeak fails if any watched boundary saw fixture secret
// material during the harness's lifetime.
func (harness *CoordinatorHarness) AssertNoCredentialLeak() error {
	return harness.Credentials.AssertNoCrossings()
}

// Close shuts the coordinator down and removes the harness directory.
//
// It is idempotent, so a deferred Close and an explicit one cannot double-free
// or double-report.
func (harness *CoordinatorHarness) Close(ctx context.Context) error {
	harness.closeOnce.Do(func() {
		if harness.Events != nil {
			harness.Events.Finish()
		}
		if harness.Application != nil {
			harness.closeErr = harness.Application.Shutdown(ctx)
		}
		if err := testfixtures.RemoveFixtureDirectory(harness.root, harness.preserve); err != nil {
			if harness.closeErr != nil {
				harness.closeErr = fmt.Errorf("%w; and cleanup: %v", harness.closeErr, err)
				return
			}
			harness.closeErr = err
		}
	})
	return harness.closeErr
}
