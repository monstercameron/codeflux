package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"codeflux.dev/codeflux/internal/coordinator"
	"codeflux.dev/codeflux/internal/storage"
)

// startArguments are the parsed flags for `codeflux start` (M23-001).
type startArguments struct {
	database       string
	address        string
	assets         string
	openBrowser    bool
	nonInteractive bool
}

// parseStartArguments reads the flags, refusing anything it does not know
// rather than ignoring it. Silently accepting an unknown flag is how a
// mistyped `--no-browser` ends up opening a browser.
func parseStartArguments(args []string) (startArguments, error) {
	arguments := startArguments{openBrowser: true}
	remaining, noBrowser := hasFlag(args, "--no-browser")
	if noBrowser {
		arguments.openBrowser = false
	}
	remaining, nonInteractive := hasFlag(remaining, "--non-interactive")
	arguments.nonInteractive = nonInteractive

	for index := 0; index < len(remaining); index++ {
		switch remaining[index] {
		case "--database", "--address", "--assets":
			name := remaining[index]
			if index+1 >= len(remaining) {
				return startArguments{}, fmt.Errorf("%s requires a value", name)
			}
			value := remaining[index+1]
			index++
			if strings.TrimSpace(value) == "" {
				return startArguments{}, fmt.Errorf("%s requires a non-empty value", name)
			}
			switch name {
			case "--database":
				arguments.database = value
			case "--assets":
				arguments.assets = value
			default:
				arguments.address = value
			}
		default:
			return startArguments{}, fmt.Errorf("unknown flag %q", remaining[index])
		}
	}
	if arguments.address == "" {
		arguments.address = "127.0.0.1:0"
	}
	if err := validateLoopbackAddress(arguments.address); err != nil {
		return startArguments{}, err
	}
	return arguments, nil
}

// validateLoopbackAddress refuses a non-loopback bind.
//
// The coordinator can read and change a repository and hold provider
// credentials. Binding it to a routable interface would expose all of that to
// the network, so the refusal is here at the CLI as well as in the server:
// a user should be told no before anything starts, not after.
func validateLoopbackAddress(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("address %q must be HOST:PORT", address)
	}
	if port == "" {
		return fmt.Errorf("address %q must include a port", address)
	}
	host = strings.Trim(host, "[]")
	if host == "" {
		return fmt.Errorf(
			"address %q binds every interface; codeflux serves loopback only", address)
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	parsed := net.ParseIP(host)
	if parsed == nil {
		return fmt.Errorf("address host %q is not an IP address or localhost", host)
	}
	if !parsed.IsLoopback() {
		return fmt.Errorf(
			"address %q is not loopback; codeflux must not be reachable from the network", address)
	}
	return nil
}

// runStart implements `codeflux start` (M23-001, M23-002, M23-003).
func runStart(
	ctx context.Context,
	stdout, stderr io.Writer,
	stdin *os.File,
	args []string,
	openURL func(string) error,
) int {
	arguments, err := parseStartArguments(args)
	if err != nil {
		fmt.Fprintf(stderr, "codeflux start: %v\n", err)
		return exitUsage
	}
	policy := newInteractionPolicy(arguments.nonInteractive, stdin)

	databasePath := arguments.database
	if databasePath == "" {
		databasePath, err = storage.DefaultDatabasePath()
		if err != nil {
			fmt.Fprintln(stderr,
				"codeflux start: no default database location is available on this system")
			return exitUnavailable
		}
	}
	if _, statErr := os.Stat(databasePath); errors.Is(statErr, os.ErrNotExist) {
		// Creating the database is not a decision worth prompting about: it is
		// what starting means. It is announced rather than asked, so a user who
		// mistyped --database sees the wrong path immediately. Prompting here
		// would also break the ordinary `codeflux start >log 2>&1 &`, whose
		// stdin is not a terminal and where nobody is present to answer.
		fmt.Fprintf(stdout, "creating database: %s\n", databasePath)
	}
	// start needs no decision from a person, so --non-interactive changes
	// nothing here. The flag is still accepted so a script can pass it to every
	// codeflux command without special-casing this one.
	_ = policy

	// The assets are resolved before anything is started. A coordinator that
	// came up and then served 404 at its own URL is the failure this command
	// used to have: the process looked healthy and the product was not there.
	workingDirectory, _ := os.Getwd()
	resolved, origin, err := resolveFrontendAssets(arguments.assets, os.LookupEnv, workingDirectory)
	if err != nil {
		fmt.Fprintf(stderr, "codeflux start: %v\n", err)
		if errors.Is(err, ErrNoFrontendAssets) {
			return exitUnavailable
		}
		return exitFailure
	}

	application, err := coordinator.StartApplication(ctx, coordinator.ApplicationOptions{
		DatabasePath:      databasePath,
		BackupDirectory:   filepath.Join(filepath.Dir(databasePath), "backups"),
		ListenAddress:     arguments.address,
		TaskListenAddress: "127.0.0.1:0",
		FrontendAssets:    resolved,
	})
	if err != nil {
		fmt.Fprintf(stderr, "codeflux start: %v\n", err)
		return exitFailure
	}
	defer func() {
		if shutdownErr := application.Shutdown(context.Background()); shutdownErr != nil {
			fmt.Fprintf(stderr, "codeflux start: shutdown: %v\n", shutdownErr)
		}
	}()

	url := browserURL(application.Address())
	// M23-003: the URL is printed, the session secret is not. A secret echoed
	// to a terminal survives in scrollback and in shell history, and the
	// browser receives it as an HttpOnly cookie without ever needing to be
	// typed.
	fmt.Fprintf(stdout, "codeflux is running at %s\n", url)
	fmt.Fprintf(stdout, "serving the interface from %s\n", origin)
	fmt.Fprintln(stdout, "the session secret is held by this process and sent to the browser as a cookie; it is not printed")

	if arguments.openBrowser {
		if openErr := openURL(url); openErr != nil {
			// Failing to open a browser is not a failure to start. The URL is
			// already on screen, so the user can continue.
			fmt.Fprintf(stdout, "could not open a browser automatically (%v); open %s yourself\n",
				openErr, url)
		}
	} else {
		fmt.Fprintln(stdout, "browser opening is disabled; open the URL above")
	}

	fmt.Fprintln(stdout, "press Ctrl+C to stop")
	waitForInterrupt(ctx)
	fmt.Fprintln(stdout, "stopping")
	return exitOK
}

// browserURL renders the address a person should open.
func browserURL(address string) string {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return "http://" + address + "/"
	}
	// An unspecified or bracketed host is rendered as loopback, which is what
	// the user must actually type: nobody can open http://[::]/ .
	trimmed := strings.Trim(host, "[]")
	parsed := net.ParseIP(trimmed)
	if trimmed == "" || (parsed != nil && parsed.IsUnspecified()) {
		return "http://127.0.0.1:" + port + "/"
	}
	if parsed != nil && parsed.To4() == nil {
		return "http://[" + trimmed + "]:" + port + "/"
	}
	return "http://" + trimmed + ":" + port + "/"
}

// waitForInterrupt blocks until the process is asked to stop.
func waitForInterrupt(ctx context.Context) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	select {
	case <-signals:
	case <-ctx.Done():
	}
}

// openInBrowser is the default browser opener (M23-002).
//
// It shells out to the platform's own handler rather than guessing a browser
// executable, so it honours whatever the user has configured.
func openInBrowser(url string) error {
	if err := validateOpenableURL(url); err != nil {
		return err
	}
	var command *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// rundll32 is used rather than `cmd /c start` because start would
		// interpret the URL's characters as shell syntax.
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		command = exec.Command("open", url)
	default:
		command = exec.Command("xdg-open", url)
	}
	return command.Start()
}

// validateOpenableURL refuses to hand anything but a loopback http URL to the
// platform handler.
//
// The opener passes its argument to a system component that will act on it. A
// URL assembled from an unexpected address, or one carrying another scheme,
// must never reach that component.
func validateOpenableURL(url string) error {
	if !strings.HasPrefix(url, "http://") {
		return fmt.Errorf("refusing to open %q: only http loopback URLs are opened", url)
	}
	rest := strings.TrimPrefix(url, "http://")
	// The whole remainder is checked, not only the authority: a query or a
	// fragment is extra input to whatever the platform handler passes this to,
	// and the URL this command opens never needs either.
	if strings.ContainsAny(rest, "@?#\\") {
		return fmt.Errorf("refusing to open %q: the address contains unexpected characters", url)
	}
	authority := rest
	if slash := strings.Index(rest, "/"); slash >= 0 {
		authority = rest[:slash]
	}
	host := authority
	if index := strings.LastIndex(authority, ":"); index >= 0 {
		host = authority[:index]
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	parsed := net.ParseIP(host)
	if parsed == nil || !parsed.IsLoopback() {
		return fmt.Errorf("refusing to open %q: the host is not loopback", url)
	}
	return nil
}
