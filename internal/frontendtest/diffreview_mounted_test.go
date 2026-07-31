package frontendtest

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mxschmitt/playwright-go"
)

// TestMountedDiffReviewSurfaceAcceptance mounts the real GWC-compiled
// web/frontend/diffreview.DiffReview component in headless Chromium and
// exercises the M20-058..067 diff review affordances: changed-file status
// and line counts, category filtering, a bounded unified diff with
// whitespace visibility, hunk links to plan steps/tool events/validation
// evidence, and out-of-scope/formatting-churn/binary-or-generated warnings.
//
// It binds its own ephemeral loopback port via httptest.NewServer and tears
// it down on completion; it never touches the developer's running :63131
// server.
func TestMountedDiffReviewSurfaceAcceptance(t *testing.T) {
	if os.Getenv(mountedRenderIsolationEnvironment) != "1" {
		t.Skip("set " + mountedRenderIsolationEnvironment + "=1 to build and run mounted browser evidence")
	}

	repository := repositoryRootForMountedTest(t)
	artifactDirectory := filepath.Join(repository, ".artifacts", "diffreview-mounted")
	prepareMountedFixtureAssetsForPackage(t, repository, artifactDirectory, "./internal/frontendtest/diffreviewfixture")

	server := httptest.NewServer(http.FileServer(http.Dir(artifactDirectory)))
	defer server.Close()

	instance, err := playwright.Run(&playwright.RunOptions{
		DriverDirectory:     os.Getenv("PLAYWRIGHT_DRIVER_PATH"),
		SkipInstallBrowsers: true,
	})
	if err != nil {
		t.Fatalf("start Playwright: %v", err)
	}
	defer func() { _ = instance.Stop() }()

	launch := playwright.BrowserTypeLaunchOptions{Headless: playwright.Bool(true)}
	if executable := os.Getenv("CODEFLUX_CHROMIUM_EXECUTABLE"); executable != "" {
		launch.ExecutablePath = playwright.String(executable)
	}
	browser, err := instance.Chromium.Launch(launch)
	if err != nil {
		t.Fatalf("launch Chromium: %v", err)
	}
	defer func() { _ = browser.Close() }()

	page, err := browser.NewPage(playwright.BrowserNewPageOptions{
		Viewport: &playwright.Size{Width: 1280, Height: 900},
	})
	if err != nil {
		t.Fatalf("create browser page: %v", err)
	}
	page.SetDefaultTimeout(10_000)
	if _, err := page.Goto(server.URL); err != nil {
		t.Fatalf("load mounted diff review fixture: %v", err)
	}
	if err := browserAssertions().Locator(page.GetByTestId("diff-review-fixture")).ToBeVisible(); err != nil {
		t.Fatalf("wait for mounted diff review fixture: %v", err)
	}

	root := page.Locator(`[data-component="diff-review"]`)
	if err := browserAssertions().Locator(root).ToBeVisible(); err != nil {
		t.Fatalf("wait for diff review root: %v", err)
	}

	// M20-058: changed-file status and line counts.
	if err := browserAssertions().Locator(root).ToHaveAttribute("data-file-count", "6"); err != nil {
		t.Fatalf("changed-file count: %v", err)
	}
	if err := browserAssertions().Locator(root).ToHaveAttribute("data-filtered-count", "6"); err != nil {
		t.Fatalf("unfiltered changed-file count: %v", err)
	}
	sourceRow := page.Locator(`[data-path="internal/service/handler.go"]`).First()
	if err := browserAssertions().Locator(sourceRow).ToHaveAttribute("data-status", "modified"); err != nil {
		t.Fatalf("source file status: %v", err)
	}
	rowText, err := sourceRow.TextContent()
	if err != nil {
		t.Fatalf("read source row text: %v", err)
	}
	if !strings.Contains(rowText, "+12 -3") {
		t.Fatalf("source row missing line counts, got %q", rowText)
	}

	// M20-059: category filtering narrows the changed-file list.
	if err := browserAssertions().Locator(page.Locator("#diff-review-filter-test")).ToHaveAttribute("aria-pressed", "false"); err != nil {
		t.Fatalf("initial test category filter state: %v", err)
	}
	if err := page.Locator("#diff-review-filter-test").Click(); err != nil {
		t.Fatalf("click test category filter: %v", err)
	}
	if err := browserAssertions().Locator(root).ToHaveAttribute("data-filtered-count", "1"); err != nil {
		t.Fatalf("filtered changed-file count: %v", err)
	}
	if err := browserAssertions().Locator(page.Locator("#diff-review-filter-test")).ToHaveAttribute("aria-pressed", "true"); err != nil {
		t.Fatalf("active test category filter state: %v", err)
	}
	if err := page.Locator("#diff-review-filter-test").Click(); err != nil {
		t.Fatalf("clear test category filter: %v", err)
	}
	if err := browserAssertions().Locator(root).ToHaveAttribute("data-filtered-count", "6"); err != nil {
		t.Fatalf("restored changed-file count: %v", err)
	}

	// M20-060, M20-062..064: selecting a file renders its bounded unified
	// diff with plan-step, tool-event, and validation links.
	selectedPanel := page.Locator(`[data-component="diff-review-selected-file"]`)
	if err := browserAssertions().Locator(selectedPanel).ToHaveAttribute("data-path", "internal/service/handler.go"); err != nil {
		t.Fatalf("initial selected file: %v", err)
	}
	testRow := page.Locator(`[data-component="diff-review-file-row"][data-path="internal/service/handler_test.go"]`).First()
	if err := testRow.Click(); err != nil {
		t.Fatalf("select test file row: %v", err)
	}
	if err := browserAssertions().Locator(selectedPanel).ToHaveAttribute("data-path", "internal/service/handler_test.go"); err != nil {
		t.Fatalf("selected test file: %v", err)
	}
	if err := sourceRow.Click(); err != nil {
		t.Fatalf("restore source file selection: %v", err)
	}
	if err := browserAssertions().Locator(selectedPanel).ToHaveAttribute("data-path", "internal/service/handler.go"); err != nil {
		t.Fatalf("restored selected source file: %v", err)
	}
	hunkHeader := page.Locator(`[data-component="diff-review-hunk-header"]`).First()
	if err := browserAssertions().Locator(hunkHeader).ToHaveAttribute("data-plan-step-count", "1"); err != nil {
		t.Fatalf("hunk plan-step link count: %v", err)
	}
	if err := browserAssertions().Locator(hunkHeader).ToHaveAttribute("data-tool-event-count", "1"); err != nil {
		t.Fatalf("hunk tool-event link count: %v", err)
	}
	if err := browserAssertions().Locator(hunkHeader).ToHaveAttribute("data-validation-count", "1"); err != nil {
		t.Fatalf("hunk validation link count: %v", err)
	}

	// M20-061: whitespace visibility control reveals the leading tab marker
	// without changing the underlying diff content.
	before, err := page.Locator(`[data-component="diff-review-line"][data-kind="deletion"]`).First().TextContent()
	if err != nil {
		t.Fatalf("read deletion line before whitespace toggle: %v", err)
	}
	if strings.Contains(before, "→") {
		t.Fatalf("whitespace marker present before toggling visibility: %q", before)
	}
	if err := page.Locator("#diff-review-whitespace-toggle").Click(); err != nil {
		t.Fatalf("click whitespace toggle: %v", err)
	}
	if err := browserAssertions().Locator(page.Locator("#diff-review-whitespace-toggle")).ToHaveAttribute("aria-pressed", "true"); err != nil {
		t.Fatalf("whitespace toggle pressed state: %v", err)
	}
	after, err := page.Locator(`[data-component="diff-review-line"][data-kind="deletion"]`).First().TextContent()
	if err != nil {
		t.Fatalf("read deletion line after whitespace toggle: %v", err)
	}
	if !strings.Contains(after, "→") {
		t.Fatalf("whitespace marker missing after toggling visibility: %q", after)
	}

	// M20-065..067: out-of-scope, formatting-churn, and binary/generated
	// warnings are surfaced.
	warnings := page.Locator(`[data-component="diff-review-warnings"]`)
	if err := browserAssertions().Locator(warnings).ToHaveAttribute("data-state", "present"); err != nil {
		t.Fatalf("review warnings state: %v", err)
	}
	warningsText, err := warnings.TextContent()
	if err != nil {
		t.Fatalf("read warnings text: %v", err)
	}
	for _, want := range []string{"Out of plan scope", "Broad formatting churn", "Binary or generated changes"} {
		if !strings.Contains(warningsText, want) {
			t.Fatalf("review warnings missing %q, got %q", want, warningsText)
		}
	}
}
