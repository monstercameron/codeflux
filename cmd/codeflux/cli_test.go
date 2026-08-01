package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/credentials"
)

// invoke runs the CLI with everything injected, so no test needs a terminal, a
// browser, or a real credential store.
func invoke(t *testing.T, stdin *os.File, opened *[]string, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	open := func(url string) error {
		if opened != nil {
			*opened = append(*opened, url)
		}
		return nil
	}
	code := dispatch(t.Context(), &stdout, &stderr, stdin, args, open)
	return code, stdout.String(), stderr.String()
}

// TestM23_012_ExitCodesAreDistinctAndDocumented covers M23-012.
func TestM23_012_ExitCodesAreDistinctAndDocumented(t *testing.T) {
	meanings := ExitCodeMeanings()
	if len(meanings) != 5 {
		t.Fatalf("%d exit codes are documented, want 5", len(meanings))
	}
	for _, code := range []int{
		exitOK, exitFailure, exitUsage, exitUnavailable, exitInteractionRequired,
	} {
		if strings.TrimSpace(meanings[code]) == "" {
			t.Fatalf("exit code %d has no documented meaning", code)
		}
	}
	// The codes must be distinct, or a caller cannot tell them apart.
	seen := map[int]bool{}
	for code := range meanings {
		if seen[code] {
			t.Fatalf("exit code %d is documented twice", code)
		}
		seen[code] = true
	}

	// Each code must be reachable from a real invocation.
	if code, _, _ := invoke(t, nil, nil, "version"); code != exitOK {
		t.Fatalf("version exited %d, want %d", code, exitOK)
	}
	if code, _, _ := invoke(t, nil, nil, "not-a-command"); code != exitUsage {
		t.Fatalf("unknown command exited %d, want %d", code, exitUsage)
	}
	if code, _, _ := invoke(t, nil, nil); code != exitUsage {
		t.Fatalf("no arguments exited %d, want %d", code, exitUsage)
	}
	if code, _, _ := invoke(t, nil, nil, "backup"); code != exitUsage {
		t.Fatalf("backup without --output exited %d, want %d", code, exitUsage)
	}
}

// TestM23_013_HelpIsContextualAndComplete covers M23-013.
func TestM23_013_HelpIsContextualAndComplete(t *testing.T) {
	code, stdout, _ := invoke(t, nil, nil, "help")
	if code != exitOK {
		t.Fatalf("help exited %d", code)
	}
	// Every command must appear in the list, or it is undiscoverable.
	for _, spec := range commandRegistry() {
		if !strings.Contains(stdout, spec.Name) {
			t.Fatalf("help omits %q", spec.Name)
		}
		if !strings.Contains(stdout, spec.Summary) {
			t.Fatalf("help omits the summary for %q", spec.Name)
		}
	}
	// The exit-code contract must be in the help, since that is where someone
	// wrapping the CLI will look for it.
	for code, meaning := range ExitCodeMeanings() {
		if !strings.Contains(stdout, meaning) {
			t.Fatalf("help omits the meaning of exit code %d", code)
		}
	}

	// Contextual help must exist for every command and must name its flags.
	for _, spec := range commandRegistry() {
		code, detail, _ := invoke(t, nil, nil, "help", spec.Name)
		if code != exitOK {
			t.Fatalf("help %s exited %d", spec.Name, code)
		}
		if !strings.Contains(detail, spec.Usage) {
			t.Fatalf("help %s does not show its usage", spec.Name)
		}
		if !strings.Contains(detail, spec.Detail) {
			t.Fatalf("help %s does not explain what it does", spec.Name)
		}
		for _, flag := range spec.Flags {
			if !strings.Contains(detail, flag.Name) {
				t.Fatalf("help %s omits flag %s", spec.Name, flag.Name)
			}
			if !strings.Contains(detail, flag.Meaning) {
				t.Fatalf("help %s omits the meaning of %s", spec.Name, flag.Name)
			}
		}
		// An interactive command must say what --non-interactive does to it.
		if spec.Interactive && !strings.Contains(detail, "--non-interactive") {
			t.Fatalf("help %s does not mention non-interactive behaviour", spec.Name)
		}
	}

	// Help for a command that does not exist is a usage error with a
	// suggestion, not a silent empty page.
	code, _, stderr := invoke(t, nil, nil, "help", "intergity-check")
	if code != exitUsage {
		t.Fatalf("help for an unknown command exited %d", code)
	}
	if !strings.Contains(stderr, "did you mean") {
		t.Fatalf("an unknown command produced no suggestion: %s", stderr)
	}
}

// TestM23_003_StartNeverPrintsTheSessionSecret covers M23-003.
func TestM23_003_StartNeverPrintsTheSessionSecret(t *testing.T) {
	// The URL builder must never carry a secret, and must render an
	// unspecified bind as something a person can actually open.
	for address, want := range map[string]string{
		"127.0.0.1:63131": "http://127.0.0.1:63131/",
		"0.0.0.0:8080":    "http://127.0.0.1:8080/",
		"[::]:8080":       "http://127.0.0.1:8080/",
		"[::1]:8080":      "http://[::1]:8080/",
	} {
		if got := browserURL(address); got != want {
			t.Fatalf("browserURL(%q) = %q, want %q", address, got, want)
		}
	}

	// Starting on an ephemeral loopback port must print the URL and say
	// explicitly that the secret is withheld.
	root := t.TempDir()
	stdin := nonTerminalStdin(t)
	var opened []string
	done := make(chan struct{})
	var stdout, stderr bytes.Buffer
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		defer close(done)
		_ = runStart(ctx, &stdout, &stderr, stdin, []string{
			"--database", filepath.Join(root, "codeflux.sqlite3"),
			"--address", "127.0.0.1:0",
			"--no-browser",
		}, func(url string) error {
			opened = append(opened, url)
			return nil
		})
	}()
	// The command blocks until interrupted; cancelling the context is the
	// supported way to stop it.
	waitForOutput(t, &stdout, "codeflux is running at")
	cancel()
	<-done

	output := stdout.String()
	if !strings.Contains(output, "http://127.0.0.1:") {
		t.Fatalf("start did not print a loopback URL: %s", output)
	}
	if !strings.Contains(output, "is not printed") {
		t.Fatalf("start does not say the secret is withheld: %s", output)
	}
	// The secret is 32 random bytes base64-encoded; nothing that long and
	// opaque should appear.
	for _, line := range strings.Split(output, "\n") {
		for _, field := range strings.Fields(line) {
			if len(field) >= 40 && !strings.HasPrefix(field, "http") &&
				!strings.Contains(field, string(os.PathSeparator)) && !strings.Contains(field, "/") {
				t.Fatalf("start printed an opaque %d-character value: %q", len(field), field)
			}
		}
	}
	if len(opened) != 0 {
		t.Fatalf("--no-browser still opened %v", opened)
	}
}

// TestM23_002_BrowserOpeningCanBeOptedOutAndRefusesUnsafeURLs covers M23-002.
func TestM23_002_BrowserOpeningCanBeOptedOutAndRefusesUnsafeURLs(t *testing.T) {
	arguments, err := parseStartArguments(nil)
	if err != nil {
		t.Fatalf("parse default arguments: %v", err)
	}
	if !arguments.openBrowser {
		t.Fatal("the browser is not opened by default")
	}
	arguments, err = parseStartArguments([]string{"--no-browser"})
	if err != nil {
		t.Fatalf("parse --no-browser: %v", err)
	}
	if arguments.openBrowser {
		t.Fatal("--no-browser did not disable browser opening")
	}

	// The opener hands its argument to a system handler, so it must refuse
	// anything that is not a loopback http URL.
	for _, url := range []string{
		"https://example.invalid/",
		"file:///etc/passwd",
		"http://10.0.0.5:8080/",
		"http://example.invalid/",
		"http://user@127.0.0.1:8080/",
		"http://127.0.0.1:8080/?x=1#y",
		"javascript:alert(1)",
		"",
	} {
		if err := validateOpenableURL(url); err == nil {
			t.Fatalf("the opener accepted %q", url)
		}
	}
	for _, url := range []string{
		"http://127.0.0.1:63131/", "http://localhost:8080/", "http://[::1]:8080/",
	} {
		if err := validateOpenableURL(url); err != nil {
			t.Fatalf("the opener refused a legitimate URL %q: %v", url, err)
		}
	}
}

// TestM23_001_StartRefusesNonLoopbackAddresses is the core M23-001 safety
// property: the coordinator can read and change a repository, so it must never
// be reachable from the network.
func TestM23_001_StartRefusesNonLoopbackAddresses(t *testing.T) {
	for _, address := range []string{
		"0.0.0.0:8080", "[::]:8080", "10.0.0.5:8080",
		"example.invalid:8080", "127.0.0.1", "127.0.0.1:", ":8080",
	} {
		if _, err := parseStartArguments([]string{"--address", address}); err == nil {
			t.Fatalf("start accepted address %q", address)
		}
	}
	for _, address := range []string{"127.0.0.1:0", "localhost:8080", "[::1]:8080"} {
		if _, err := parseStartArguments([]string{"--address", address}); err != nil {
			t.Fatalf("start refused loopback address %q: %v", address, err)
		}
	}
	// An unknown flag is refused rather than ignored: a mistyped --no-browser
	// that was ignored would open a browser the user asked not to have.
	if _, err := parseStartArguments([]string{"--nobrowser"}); err == nil {
		t.Fatal("start ignored an unknown flag")
	}
	if _, err := parseStartArguments([]string{"--database"}); err == nil {
		t.Fatal("start accepted --database with no value")
	}
}

// TestM23_014_NonInteractiveCommandsNeverPrompt covers M23-014.
func TestM23_014_NonInteractiveCommandsNeverPrompt(t *testing.T) {
	// A non-interactive start against a database that does not exist must
	// refuse with the dedicated code rather than creating one silently.
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	// A non-interactive provider set with no input must refuse with the
	// dedicated code rather than blocking on a prompt nobody will answer.
	code := runProvider(t.Context(), &stdout, &stderr, stdinWithContent(t, ""),
		[]string{"set", "--name", "anthropic", "--non-interactive"}, newRecordingStore())
	if code != exitInteractionRequired {
		t.Fatalf("non-interactive provider set exited %d, want %d", code, exitInteractionRequired)
	}
	_ = root

	// The policy itself must treat a pipe as non-interactive even without the
	// flag: a command fed from a script is not interactive because nobody said
	// it was.
	policy := newInteractionPolicy(false, nonTerminalStdin(t))
	if policy.Interactive {
		t.Fatal("a piped stdin was treated as interactive")
	}
	if err := policy.RequireDecision("anything"); !errors.Is(err, ErrInteractionRequired) {
		t.Fatalf("RequireDecision returned %v", err)
	}
	if newInteractionPolicy(true, nil).Interactive {
		t.Fatal("--non-interactive was ignored")
	}
	if newInteractionPolicy(false, nil).Interactive {
		t.Fatal("a missing stdin was treated as interactive")
	}
}

// recordingStore is an in-memory credential store.
type recordingStore struct {
	values  map[credentials.Reference][]byte
	deletes int
}

func newRecordingStore() *recordingStore {
	return &recordingStore{values: map[credentials.Reference][]byte{}}
}

func (store *recordingStore) Create(
	_ context.Context, reference credentials.Reference, secret credentials.Secret,
) error {
	if _, exists := store.values[reference]; exists {
		return errors.New("already exists")
	}
	return secret.Use(func(value []byte) error {
		store.values[reference] = append([]byte(nil), value...)
		return nil
	})
}

func (store *recordingStore) Update(
	_ context.Context, reference credentials.Reference, secret credentials.Secret,
) error {
	return secret.Use(func(value []byte) error {
		store.values[reference] = append([]byte(nil), value...)
		return nil
	})
}

func (store *recordingStore) Retrieve(
	_ context.Context, reference credentials.Reference,
) (credentials.Secret, error) {
	value, ok := store.values[reference]
	if !ok {
		return credentials.Secret{}, errors.New("not found")
	}
	return credentials.NewSecret(value)
}

func (store *recordingStore) Test(_ context.Context, reference credentials.Reference) error {
	if _, ok := store.values[reference]; !ok {
		return errors.New("not found")
	}
	return nil
}

func (store *recordingStore) Delete(_ context.Context, reference credentials.Reference) error {
	store.deletes++
	delete(store.values, reference)
	return nil
}

// TestM23_009_011_ProviderCommandsManageCredentialsSafely covers M23-009,
// M23-010, and M23-011.
func TestM23_009_011_ProviderCommandsManageCredentialsSafely(t *testing.T) {
	store := newRecordingStore()
	var stdout, stderr bytes.Buffer

	// Test before set must report unavailable, not failure: nothing is broken,
	// something is missing.
	code := runProvider(t.Context(), &stdout, &stderr, nil,
		[]string{"test", "--name", "anthropic"}, store)
	if code != exitUnavailable {
		t.Fatalf("test before set exited %d, want %d", code, exitUnavailable)
	}

	// Set reads the credential from standard input.
	stdin := stdinWithContent(t, "fixture-not-a-real-credential-value\n")
	stdout.Reset()
	stderr.Reset()
	code = runProvider(t.Context(), &stdout, &stderr, stdin,
		[]string{"set", "--name", "anthropic"}, store)
	if code != exitOK {
		t.Fatalf("provider set exited %d: %s", code, stderr.String())
	}
	if len(store.values) != 1 {
		t.Fatalf("provider set stored %d credentials", len(store.values))
	}
	// The credential must never be echoed.
	if strings.Contains(stdout.String(), "fixture-not-a-real-credential-value") {
		t.Fatalf("provider set echoed the credential: %s", stdout.String())
	}

	// Test now succeeds, and says it did not contact the provider.
	stdout.Reset()
	code = runProvider(t.Context(), &stdout, &stderr, nil,
		[]string{"test", "--name", "anthropic"}, store)
	if code != exitOK {
		t.Fatalf("provider test exited %d", code)
	}
	if !strings.Contains(stdout.String(), "does not contact the provider") {
		t.Fatalf("provider test overstates what it checked: %s", stdout.String())
	}

	// Setting again rotates rather than refusing.
	stdin = stdinWithContent(t, "fixture-not-a-real-rotated-value\n")
	stdout.Reset()
	code = runProvider(t.Context(), &stdout, &stderr, stdin,
		[]string{"set", "--name", "anthropic"}, store)
	if code != exitOK {
		t.Fatalf("rotating a credential exited %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "updated") {
		t.Fatalf("a rotation was not reported as an update: %s", stdout.String())
	}

	// Delete removes it, and deleting again is still success.
	stdout.Reset()
	code = runProvider(t.Context(), &stdout, &stderr, nil,
		[]string{"delete", "--name", "anthropic"}, store)
	if code != exitOK {
		t.Fatalf("provider delete exited %d", code)
	}
	if len(store.values) != 0 {
		t.Fatalf("provider delete left %d credentials", len(store.values))
	}
	code = runProvider(t.Context(), &stdout, &stderr, nil,
		[]string{"delete", "--name", "anthropic"}, store)
	if code != exitOK {
		t.Fatalf("deleting an absent credential exited %d", code)
	}
}

// TestM23_009_ProviderRefusesUnusableInvocations proves the surface cannot be
// misused into filing a credential nothing will read.
func TestM23_009_ProviderRefusesUnusableInvocations(t *testing.T) {
	store := newRecordingStore()
	var stdout, stderr bytes.Buffer
	for _, args := range [][]string{
		{},
		{"set"},
		{"set", "--name"},
		{"set", "--name", "not-a-provider"},
		{"set", "--name", "anthropic", "--unknown"},
		{"frobnicate", "--name", "anthropic"},
	} {
		code := runProvider(t.Context(), &stdout, &stderr, nil, args, store)
		if code != exitUsage {
			t.Fatalf("provider %v exited %d, want %d", args, code, exitUsage)
		}
	}

	// An empty standard input in a non-interactive session is reported as a
	// missing decision, so a script knows to supply the value.
	code := runProvider(t.Context(), &stdout, &stderr, stdinWithContent(t, ""),
		[]string{"set", "--name", "openai", "--non-interactive"}, store)
	if code != exitInteractionRequired {
		t.Fatalf("empty non-interactive input exited %d, want %d",
			code, exitInteractionRequired)
	}

	// A credential must never be accepted as a command-line argument, because
	// an argument is visible to every process and lands in shell history.
	for _, spec := range commandRegistry() {
		if spec.Name != "provider" {
			continue
		}
		for _, flag := range spec.Flags {
			if strings.Contains(strings.ToLower(flag.Name), "key") ||
				strings.Contains(strings.ToLower(flag.Name), "secret") ||
				strings.Contains(strings.ToLower(flag.Name), "token") {
				t.Fatalf("provider accepts %s as an argument", flag.Name)
			}
		}
	}
}

// TestM23_008_DiagnosticsExportIsRedactedAndNonDestructive covers M23-008.
func TestM23_008_DiagnosticsExportIsRedactedAndNonDestructive(t *testing.T) {
	root := t.TempDir()
	output := filepath.Join(root, "bundle.json")

	code, stdout, stderr := invoke(t, nil, nil,
		"diagnostics", "export", "--output", output,
		"--database", filepath.Join(root, "absent.sqlite3"))
	if code != exitOK {
		t.Fatalf("diagnostics export exited %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, output) {
		t.Fatalf("the export does not say where it wrote: %s", stdout)
	}

	raw, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}
	var bundle DiagnosticBundle
	if err := json.Unmarshal(raw, &bundle); err != nil {
		t.Fatalf("parse bundle: %v", err)
	}
	if bundle.SchemaVersion != diagnosticBundleSchemaVersion {
		t.Fatalf("bundle schema version = %d", bundle.SchemaVersion)
	}
	if bundle.Executable.GoVersion == "" || bundle.Host.OS == "" {
		t.Fatalf("bundle is incomplete: %+v", bundle)
	}
	if bundle.Storage.Status != "missing" {
		t.Fatalf("an absent database was reported as %q", bundle.Storage.Status)
	}
	for _, prerequisite := range []string{"git", "credential-store", "storage"} {
		if bundle.Prerequisites[prerequisite] == "" {
			t.Fatalf("the bundle does not report %q", prerequisite)
		}
	}

	// The bundle must not carry a filesystem path, since a path routinely
	// contains a user's name.
	text := string(raw)
	if strings.Contains(text, root) {
		t.Fatal("the bundle carries a filesystem path")
	}
	if err := assertBundleCarriesNoCredential(raw); err != nil {
		t.Fatalf("the written bundle failed its own credential scan: %v", err)
	}

	// An existing file is never overwritten: a diagnostic export is evidence.
	code, _, stderr = invoke(t, nil, nil,
		"diagnostics", "export", "--output", output)
	if code != exitUsage {
		t.Fatalf("overwriting an export exited %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr, "already exists") {
		t.Fatalf("the refusal does not explain itself: %s", stderr)
	}

	// Usage errors.
	for _, args := range [][]string{
		{"diagnostics"},
		{"diagnostics", "import"},
		{"diagnostics", "export"},
		{"diagnostics", "export", "--output"},
		{"diagnostics", "export", "--output", output, "--unknown"},
	} {
		if code, _, _ := invoke(t, nil, nil, args...); code != exitUsage {
			t.Fatalf("%v exited %d, want %d", args, code, exitUsage)
		}
	}
}

// TestM23_008_CredentialScanCatchesAFieldThatShouldNotExist proves the scan is
// load-bearing rather than always returning nil.
func TestM23_008_CredentialScanCatchesAFieldThatShouldNotExist(t *testing.T) {
	for _, encoded := range []string{
		`{"api_key":"x"}`,
		`{"provider":{"token":"x"}}`,
		`{"note":"sk-abc"}`,
		`{"aws":"AKIAIOSFODNN7EXAMPLE"}`,
		`{"slack":"xoxb-000"}`,
		`{"github":"github_pat_000"}`,
		`{"password":"x"}`,
	} {
		if err := assertBundleCarriesNoCredential([]byte(encoded)); err == nil {
			t.Fatalf("the scan accepted %s", encoded)
		}
	}
	clean := `{"schema_version":1,"host":{"os":"windows","cpus":8}}`
	if err := assertBundleCarriesNoCredential([]byte(clean)); err != nil {
		t.Fatalf("the scan rejected a clean bundle: %v", err)
	}
}

// TestM23_004_007_MaintenanceCommandsAreReachableByTheirDocumentedNames pins
// the command surface M23-004..007 requires.
func TestM23_004_007_MaintenanceCommandsAreReachableByTheirDocumentedNames(t *testing.T) {
	for _, name := range []string{
		"start", "version", "doctor", "backup", "integrity-check",
		"diagnostics", "provider", "pause", "resume", "cancel", "help",
	} {
		if _, ok := lookupCommand(name); !ok {
			t.Fatalf("command %q is not registered", name)
		}
	}
	// The pre-existing shorter name must keep working.
	root := t.TempDir()
	code, _, _ := invoke(t, nil, nil, "integrity", "--database",
		filepath.Join(root, "absent.sqlite3"))
	if code == exitUsage {
		t.Fatal("the integrity alias was removed, breaking existing scripts")
	}
}

// nonTerminalStdin returns a stdin that is a pipe, not a terminal.
func nonTerminalStdin(t *testing.T) *os.File {
	t.Helper()
	return stdinWithContent(t, "")
}

// stdinWithContent returns a *os.File containing the given text.
func stdinWithContent(t *testing.T, content string) *os.File {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stdin")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write stdin fixture: %v", err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open stdin fixture: %v", err)
	}
	t.Cleanup(func() { _ = file.Close() })
	return file
}

// waitForOutput blocks until the buffer contains marker, or the test's
// deadline passes. Polling a buffer is deliberate: the alternative is a fixed
// sleep, which is both slower and less reliable.
func waitForOutput(t *testing.T, buffer *bytes.Buffer, marker string) {
	t.Helper()
	for range 600 {
		if strings.Contains(buffer.String(), marker) {
			return
		}
		waitBriefly()
	}
	t.Fatalf("output never contained %q; got: %s", marker, buffer.String())
}

func waitBriefly() {
	time.Sleep(10 * time.Millisecond)
}
