package frontendtest

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/mxschmitt/playwright-go"
)

// TestTheAtomSearchFindsAtomsByWhatTheyPromise drives the atom collection in a
// real browser against a real repository.
//
// The point of the check is the one thing a name search cannot do. The fixture
// repository contains two documented atoms whose names share no distinguishing
// word with their promises, so a search for a word that appears only inside a
// documented promise proves the full-text index is built, queried, and
// rendered — end to end, through the coordinator, the bridge, and the page.
func TestTheAtomSearchFindsAtomsByWhatTheyPromise(t *testing.T) {
	if os.Getenv(productionConsoleEnvironment) == "" {
		t.Skipf("set %s=1 to run the end-to-end console suite", productionConsoleEnvironment)
	}
	repository := writeAtomFixtureRepository(t)
	console := startProductionConsoleForRepository(t, repository)

	pw, err := playwright.Run()
	if err != nil {
		t.Fatalf("start playwright: %v", err)
	}
	t.Cleanup(func() { _ = pw.Stop() })
	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(true),
	})
	if err != nil {
		t.Fatalf("launch chromium: %v", err)
	}
	t.Cleanup(func() { _ = browser.Close() })

	page, problems, _ := openConsolePage(t, browser, console.URL+"atoms")
	rows := page.Locator(`[data-component="atom-row"]`)
	if err := rows.First().WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible, Timeout: playwright.Float(90000),
	}); err != nil {
		t.Fatalf("the atom collection never listed an atom: %v\n%s",
			err, strings.Join(*problems, "\n"))
	}

	t.Run("TheCollectionListsEveryDocumentedAtom", func(t *testing.T) {
		names := atomRowNames(t, page)
		if len(names) != 2 ||
			!containsName(names, "ReserveStockUntilCheckoutExpires") ||
			!containsName(names, "CommitReservedStock") {
			t.Fatalf("listed atoms = %v, want both documented atoms", names)
		}
	})

	t.Run("AWordFromAPromiseFindsTheAtomThatMakesIt", func(t *testing.T) {
		// "shoppers" appears in one atom's Purpose and in no atom's name. A
		// substring match over declaration names cannot find it at all.
		names := searchAtoms(t, page, "shoppers")
		if len(names) != 1 || names[0] != "ReserveStockUntilCheckoutExpires" {
			t.Fatalf("search for a promised word = %v, want the atom that promises it", names)
		}
	})

	t.Run("ALabelFromTheSchemaFindsEveryAtomThatDeclaresIt", func(t *testing.T) {
		// Both atoms document a Retry field, and neither is named for it.
		names := searchAtoms(t, page, "retry")
		if len(names) != 2 {
			t.Fatalf("search for a documented label = %v, want both atoms", names)
		}
	})

	t.Run("AWordInsideANameStillFindsIt", func(t *testing.T) {
		// A Go identifier is one token, so this only works because the name is
		// indexed split at its case boundaries as well as whole.
		names := searchAtoms(t, page, "checkout")
		if len(names) != 1 || names[0] != "ReserveStockUntilCheckoutExpires" {
			t.Fatalf("search inside a name = %v", names)
		}
	})

	t.Run("APartialWordStillFindsItThroughTheFallback", func(t *testing.T) {
		// "eserve" is inside Reserve but starts no token, so the index cannot
		// match it and the name substring fallback has to.
		names := searchAtoms(t, page, "eserve")
		if len(names) != 2 {
			t.Fatalf("mid-token search = %v, want both Reserve atoms", names)
		}
	})

	t.Run("AWordNothingPromisesFindsNothing", func(t *testing.T) {
		names := searchAtoms(t, page, "zzzunrelated")
		if len(names) != 0 {
			t.Fatalf("search for an absent word = %v, want nothing", names)
		}
		empty := page.Locator(`[data-component="atom-list"]`)
		text, err := empty.InnerText()
		if err != nil {
			t.Fatalf("read the empty list: %v", err)
		}
		if !strings.Contains(text, "Nothing matched") {
			t.Fatalf("the empty result does not say so: %s", text)
		}
	})

	t.Run("TheFieldKeepsFocusAcrossTheWholeQuery", func(t *testing.T) {
		// The search used to render only once the collection was ready, so the
		// moment a query reached the coordinator the field was removed from the
		// document. Focus went with it and every character typed afterwards
		// landed nowhere, which read as a search that did not filter.
		if err := page.Locator("#atom-search").Fill(""); err != nil {
			t.Fatalf("clear the search: %v", err)
		}
		if err := page.Locator("#atom-search").Click(); err != nil {
			t.Fatalf("focus the search: %v", err)
		}
		// Typed a character at a time, straddling the debounce, exactly as a
		// person types.
		for _, character := range "shoppers" {
			if err := page.Keyboard().Type(string(character)); err != nil {
				t.Fatalf("type into the search: %v", err)
			}
			page.WaitForTimeout(120)
		}
		page.WaitForTimeout(2500)
		active, err := page.Evaluate(`() => document.activeElement && document.activeElement.id`)
		if err != nil {
			t.Fatalf("read the focused element: %v", err)
		}
		if identifier, _ := active.(string); identifier != "atom-search" {
			t.Fatalf("focus after the query settled = %q, want the search field", identifier)
		}
		typed, err := page.Evaluate(`() => document.querySelector('#atom-search').value`)
		if err != nil {
			t.Fatalf("read the search value: %v", err)
		}
		if value, _ := typed.(string); value != "shoppers" {
			t.Fatalf("the field holds %q, want every character that was typed", value)
		}
		if names := atomRowNames(t, page); len(names) != 1 {
			t.Fatalf("results after typing = %v, want the one atom that promises it", names)
		}

		// Typing continues after the settle, which is only possible if the
		// caret is still where the person left it.
		if err := page.Keyboard().Type(" cannot"); err != nil {
			t.Fatalf("keep typing: %v", err)
		}
		page.WaitForTimeout(2500)
		extended, err := page.Evaluate(`() => document.querySelector('#atom-search').value`)
		if err != nil {
			t.Fatalf("read the extended search: %v", err)
		}
		if value, _ := extended.(string); value != "shoppers cannot" {
			t.Fatalf("the field holds %q after further typing", value)
		}
	})

	t.Run("TheListNeverBlanksWhileANarrowerAnswerIsFetched", func(t *testing.T) {
		// A list that empties while the coordinator answers makes the page jump
		// under the reader once per typed word.
		if err := page.Locator("#atom-search").Fill(""); err != nil {
			t.Fatalf("clear the search: %v", err)
		}
		page.WaitForTimeout(2000)
		if err := page.Locator("#atom-search").Fill("retry"); err != nil {
			t.Fatalf("type the search: %v", err)
		}
		for sample := 0; sample < 24; sample++ {
			page.WaitForTimeout(100)
			count, err := page.Locator(`[data-component="atom-row"]`).Count()
			if err != nil {
				t.Fatalf("count the rows: %v", err)
			}
			if count == 0 {
				t.Fatal("the list emptied while answering a query that matches both atoms")
			}
		}
	})

	t.Run("TheClientReportedNoErrors", func(t *testing.T) {
		if len(*problems) > 0 {
			t.Fatalf("the atom collection reported %d console or page error(s):\n%s",
				len(*problems), strings.Join(*problems, "\n"))
		}
	})
}

// searchAtoms types a query and returns the names it produced.
//
// It waits for the list to settle rather than for a fixed delay: the typed
// term narrows what is already on screen immediately and the coordinator's
// full-text answer replaces it a moment later, so a check that read too early
// would be testing the local narrowing instead of the index.
func searchAtoms(t *testing.T, page playwright.Page, query string) []string {
	t.Helper()
	field := page.Locator("#atom-search")
	if err := field.Fill(query); err != nil {
		t.Fatalf("type the search %q: %v", query, err)
	}
	deadline := time.Now().Add(20 * time.Second)
	var settled []string
	for attempt := 0; time.Now().Before(deadline); attempt++ {
		time.Sleep(400 * time.Millisecond)
		pending, err := page.Evaluate(
			`() => !!document.querySelector('[data-component="atom-list"]')` +
				`?.innerText.includes('Looking through every atom')`,
		)
		if err != nil {
			t.Fatalf("read the search state: %v", err)
		}
		if waiting, _ := pending.(bool); waiting {
			continue
		}
		current := atomRowNames(t, page)
		if attempt > 0 && sameNames(settled, current) {
			return current
		}
		settled = current
	}
	t.Fatalf("the search for %q never settled", query)
	return nil
}

// atomRowNames reads the declaration name out of every listed row.
func atomRowNames(t *testing.T, page playwright.Page) []string {
	t.Helper()
	found, err := page.Evaluate(
		`() => [...document.querySelectorAll('[data-component="atom-row"]')]` +
			`.map(row => row.dataset.atomName || '')`,
	)
	if err != nil {
		t.Fatalf("read the listed atoms: %v", err)
	}
	entries, _ := found.([]any)
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if name, _ := entry.(string); name != "" {
			names = append(names, name)
		}
	}
	return names
}

func containsName(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}

func sameNames(first, second []string) bool {
	if len(first) != len(second) {
		return false
	}
	for index := range first {
		if first[index] != second[index] {
			return false
		}
	}
	return true
}

// writeAtomFixtureRepository creates a Git repository whose atoms document
// promises their names do not contain.
//
// The names and the promises are deliberately disjoint: if the two shared
// their distinguishing words, a name search would pass this suite and the
// index could be missing entirely.
func writeAtomFixtureRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(name, content string) {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	write("go.mod", "module example.com/atomfixture\n\ngo 1.25\n")
	write("internal/inventory/reserve.go", atomFixtureSource)
	write("internal/inventory/reserve_test.go", atomFixtureTestSource)

	for _, arguments := range [][]string{
		{"init"},
		{"config", "user.email", "fixture@example.com"},
		{"config", "user.name", "Fixture"},
		{"add", "-A"},
		{"commit", "-m", "atom fixture"},
	} {
		command := exec.CommandContext(t.Context(), "git", arguments...)
		command.Dir = root
		if output, err := command.CombinedOutput(); err != nil {
			t.Skipf("git is unavailable for the fixture repository: %v\n%s", err, output)
		}
	}
	return root
}

// startProductionConsoleForRepository runs the real executable against one
// repository.
//
// It mirrors startProductionConsole and adds the repository, because the atom
// collection reads a repository's own source and a console opened against
// nothing has no atoms to search.
func startProductionConsoleForRepository(t *testing.T, repositoryPath string) productionConsole {
	t.Helper()
	root := repositoryRootForConsole(t)

	build := exec.CommandContext(t.Context(),
		"go", "run", "./cmd/codeflux-dev", "build-frontend")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Skipf("cannot build the browser assets in this environment: %v\n%s", err, output)
	}
	name := "codeflux"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	executable := filepath.Join(t.TempDir(), name)
	compile := exec.CommandContext(t.Context(), "go", "build", "-o", executable, "./cmd/codeflux")
	compile.Dir = root
	if output, err := compile.CombinedOutput(); err != nil {
		t.Skipf("cannot build the executable in this environment: %v\n%s", err, output)
	}

	data := t.TempDir()
	address := fmt.Sprintf("127.0.0.1:%d", freeLoopbackPort(t))
	ctx, cancel := context.WithCancel(context.Background())
	command := exec.CommandContext(ctx, executable,
		"start", "--no-browser",
		"--database", filepath.Join(data, "codeflux.sqlite3"),
		"--address", address,
		"--assets", filepath.Join(root, ".artifacts", "frontend"),
		"--repository", repositoryPath,
	)
	output := &syncBuffer{}
	command.Stdout = output
	command.Stderr = output
	if err := command.Start(); err != nil {
		cancel()
		t.Fatalf("start the console: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		_ = command.Wait()
	})

	url := "http://" + address + "/"
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(output.String(), "codeflux is running at") {
			return productionConsole{URL: url, Output: output.String}
		}
		if command.ProcessState != nil && command.ProcessState.Exited() {
			t.Fatalf("the console exited before serving:\n%s", output.String())
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("the console never announced a URL:\n%s", output.String())
	return productionConsole{}
}

const atomFixtureSource = `// Package inventory holds and releases stock against checkout sessions.
package inventory

import "errors"

// ErrInsufficientStock reports a reservation larger than what is available.
var ErrInsufficientStock = errors.New("inventory: insufficient stock")

// Hold identifies one reservation of stock against a checkout session.
type Hold struct {
	ID      string
	Session string
	Count   int
}

// ReserveStockUntilCheckoutExpires holds stock for one checkout session
// without committing the sale.
//
//codeflux:atom
// Codeflux atom documentation (schema v1):
//   Purpose:
//     Hold scarce stock for one checkout session so two shoppers cannot both
//     complete a sale for the same physical unit.
//   Use when:
//     A checkout session has a stable identity and the caller needs a
//     short-lived hold before payment capture completes.
//   Do not use when:
//     The caller wants a permanent stock decrement.
//   Semantics:
//     Reserves the requested count atomically against available stock.
//   Failure modes:
//     Returns ErrInsufficientStock when the requested count exceeds what is
//     available.
//   Retry:
//     Safe to retry with the same session and count.
//   Verification:
//     TestReserveStockRefusesMoreThanAvailable covers the refusal path.
func ReserveStockUntilCheckoutExpires(session string, count int, available int) (Hold, error) {
	if session == "" || count <= 0 {
		return Hold{}, errors.New("inventory: session and count are required")
	}
	if count > available {
		return Hold{}, ErrInsufficientStock
	}
	return Hold{ID: session + "-hold", Session: session, Count: count}, nil
}

// CommitReservedStock turns a hold into a permanent decrement.
//
//codeflux:atom
// Codeflux atom documentation (schema v1):
//   Purpose:
//     Convert an existing hold into a committed sale.
//   Use when:
//     Payment capture has succeeded for the session the hold was taken for.
//   Do not use when:
//     Payment has not settled.
//   Semantics:
//     Commits exactly the count the hold reserved.
//   Failure modes:
//     Returns an error when the hold is empty.
//   Retry:
//     Not safe to retry blindly.
//   Verification:
//     TestCommitReservedStockRefusesAnEmptyHold.
func CommitReservedStock(hold Hold) error {
	if hold.ID == "" {
		return errors.New("inventory: hold is required")
	}
	return nil
}
`

const atomFixtureTestSource = `package inventory

import "testing"

// TestReserveStockRefusesMoreThanAvailable covers the refusal path.
func TestReserveStockRefusesMoreThanAvailable(t *testing.T) {
	if _, err := ReserveStockUntilCheckoutExpires("session-1", 5, 2); err != ErrInsufficientStock {
		t.Fatalf("err = %v", err)
	}
}

// TestCommitReservedStockRefusesAnEmptyHold covers the empty hold.
func TestCommitReservedStockRefusesAnEmptyHold(t *testing.T) {
	if err := CommitReservedStock(Hold{}); err == nil {
		t.Fatal("an empty hold was committed")
	}
}
`
