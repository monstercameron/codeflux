package frontendtest

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mxschmitt/playwright-go"
)

// TestTheCodePageOpensAFileFromTheRepositoryTree drives the code page in a
// real browser against a real repository.
//
// The page's whole claim is that what it draws is the repository — the tree is
// the directory structure on disk, the file is the bytes in it, and the line
// numbers are the file's own. Every check here is that claim: a fixture file
// whose text and declaration lines are known, read back through the
// coordinator, the bridge, and the page.
func TestTheCodePageOpensAFileFromTheRepositoryTree(t *testing.T) {
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

	page, problems, _ := openConsolePage(t, browser, console.URL+"code")
	rows := page.Locator(`[data-component="tree-directory"], [data-component="tree-file"]`)
	if err := rows.First().WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible, Timeout: playwright.Float(90000),
	}); err != nil {
		t.Fatalf("the code page never drew a tree: %v\n%s",
			err, strings.Join(*problems, "\n"))
	}

	t.Run("TheTreeIsTheRepositoryRatherThanItsPackages", func(t *testing.T) {
		// go.mod belongs to no package, so a tree built from the package graph
		// would not have it. It is the cheapest proof the tree is the repository.
		paths := treePaths(t, page)
		if !containsName(paths, "go.mod") {
			t.Fatalf("the tree = %v, want the repository's own module file in it", paths)
		}
		if !containsName(paths, "internal") {
			t.Fatalf("the tree = %v, want its top directory", paths)
		}
	})

	t.Run("FilteringOpensTheDirectoriesThatHoldTheMatches", func(t *testing.T) {
		// A filter that leaves every match behind a closed directory has done
		// nothing a reader can see.
		paths := filterTree(t, page, "reserve")
		if !containsName(paths, "internal/inventory/reserve.go") {
			t.Fatalf("the filtered tree = %v, want the matching file visible", paths)
		}
		if containsName(paths, "go.mod") {
			t.Fatalf("the filtered tree = %v, want the misses gone", paths)
		}
	})

	t.Run("OpeningAFileShowsTheBytesThatAreOnDisk", func(t *testing.T) {
		openTreeFile(t, page, "internal/inventory/reserve.go")
		text := regionText(t, page, `[data-component="file-source"]`)
		for _, want := range []string{
			"package inventory",
			"func ReserveStockUntilCheckoutExpires",
			"//codeflux:atom",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("the open file does not contain %q", want)
			}
		}
		heading := regionText(t, page, `[data-component="file-content"] h2`)
		if !strings.Contains(heading, "internal/inventory/reserve.go") {
			t.Fatalf("the open file is titled %q", heading)
		}
	})

	t.Run("TheLineNumbersAreTheFilesOwn", func(t *testing.T) {
		// A viewer that numbered from its own first drawn row would send every
		// reader to the wrong line in their editor.
		numbered, err := page.Evaluate(
			`() => [...document.querySelectorAll('[data-component="file-source"] [data-line]')]` +
				`.map(row => Number(row.dataset.line))`,
		)
		if err != nil {
			t.Fatalf("read the line numbers: %v", err)
		}
		entries, _ := numbered.([]any)
		if len(entries) < 20 {
			t.Fatalf("the file drew %d lines, want the fixture's own", len(entries))
		}
		for index, entry := range entries {
			if numberOf(entry) != index+1 {
				t.Fatalf("line %d is numbered %v", index+1, entry)
			}
		}
	})

	t.Run("TheGoSourceIsColouredByTheLanguagesOwnScanner", func(t *testing.T) {
		// The "//" inside a string is the classic thing a pattern-matching
		// highlighter calls a comment. Here it must stay a string.
		classes := tokenClasses(t, page)
		for _, want := range []string{"keyword", "comment", "literal", "builtin", "declared"} {
			if classes[want] == 0 {
				t.Fatalf("nothing in the file was coloured %s: %v", want, classes)
			}
		}
		coloured, err := page.Evaluate(
			`() => { const spans = [...document.querySelectorAll('[data-component="file-source"] [data-token]')];` +
				`const hit = spans.find(span => span.textContent.includes('inventory: insufficient'));` +
				`return hit ? hit.dataset.token : ''; }`,
		)
		if err != nil {
			t.Fatalf("read the string span: %v", err)
		}
		if value, _ := coloured.(string); value != "literal" {
			t.Fatalf("a string literal was coloured %q", value)
		}
	})

	t.Run("ADeclarationChipJumpsToWhereItIsDeclared", func(t *testing.T) {
		chip := page.Locator(
			`[data-component="file-declaration"][data-declaration="CommitReservedStock"]`)
		if err := chip.Click(); err != nil {
			t.Fatalf("click the declaration: %v", err)
		}
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			marked, err := page.Evaluate(
				`() => { const row = [...document.querySelectorAll('[data-component="file-source"] [data-line]')]` +
					`.find(row => getComputedStyle(row).backgroundColor !== 'rgba(0, 0, 0, 0)');` +
					`return row ? row.textContent : ''; }`,
			)
			if err != nil {
				t.Fatalf("read the marked line: %v", err)
			}
			if text, _ := marked.(string); strings.Contains(text, "func CommitReservedStock") {
				return
			}
			time.Sleep(400 * time.Millisecond)
		}
		t.Fatal("clicking a declaration never marked the line that declares it")
	})

	t.Run("TheConsoleReportedNoProblems", func(t *testing.T) {
		if len(*problems) > 0 {
			t.Fatalf("the browser reported problems:\n%s", strings.Join(*problems, "\n"))
		}
	})
}

// numberOf reads a browser number out of an evaluated value, which arrives as
// an int or a float depending on how it was written.
func numberOf(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case float64:
		return int(typed)
	}
	return 0
}

// treePaths reads every row the tree is currently drawing.
func treePaths(t *testing.T, page playwright.Page) []string {
	t.Helper()
	found, err := page.Evaluate(
		`() => [...document.querySelectorAll('[data-component="tree-directory"], [data-component="tree-file"]')]` +
			`.map(row => row.dataset.path || '')`,
	)
	if err != nil {
		t.Fatalf("read the tree: %v", err)
	}
	entries, _ := found.([]any)
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if path, _ := entry.(string); path != "" {
			paths = append(paths, path)
		}
	}
	return paths
}

// filterTree types a term and waits for the tree to settle on an answer.
func filterTree(t *testing.T, page playwright.Page, term string) []string {
	t.Helper()
	if err := page.Locator("#file-search").Fill(term); err != nil {
		t.Fatalf("type the filter %q: %v", term, err)
	}
	deadline := time.Now().Add(20 * time.Second)
	var settled []string
	for attempt := 0; time.Now().Before(deadline); attempt++ {
		time.Sleep(400 * time.Millisecond)
		current := treePaths(t, page)
		if attempt > 0 && sameNames(settled, current) {
			return current
		}
		settled = current
	}
	t.Fatalf("the filter %q never settled", term)
	return nil
}

// openTreeFile clicks one file and waits for its contents to arrive.
func openTreeFile(t *testing.T, page playwright.Page, path string) {
	t.Helper()
	row := page.Locator(`[data-component="tree-file"][data-path="` + path + `"]`)
	if err := row.Click(); err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	source := page.Locator(`[data-component="file-source"]`)
	if err := source.WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible, Timeout: playwright.Float(45000),
	}); err != nil {
		t.Fatalf("%s never opened: %v", path, err)
	}
}

func regionText(t *testing.T, page playwright.Page, selector string) string {
	t.Helper()
	text, err := page.Locator(selector).First().InnerText()
	if err != nil {
		t.Fatalf("read %s: %v", selector, err)
	}
	return text
}

// tokenClasses counts how many runs of each colour the viewer drew.
func tokenClasses(t *testing.T, page playwright.Page) map[string]int {
	t.Helper()
	found, err := page.Evaluate(
		`() => [...document.querySelectorAll('[data-component="file-source"] [data-token]')]` +
			`.map(span => span.dataset.token)`,
	)
	if err != nil {
		t.Fatalf("read the coloured runs: %v", err)
	}
	entries, _ := found.([]any)
	counts := map[string]int{}
	for _, entry := range entries {
		if class, _ := entry.(string); class != "" {
			counts[class]++
		}
	}
	return counts
}
