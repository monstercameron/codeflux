package frontendtest

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mxschmitt/playwright-go"
)

func TestMountedAuthoritativeGraphLoadsInspectorAndDispatchesActions(t *testing.T) {
	if os.Getenv(mountedRenderIsolationEnvironment) != "1" {
		t.Skip("set " + mountedRenderIsolationEnvironment + "=1 to build and run mounted browser evidence")
	}
	repository := repositoryRootForMountedTest(t)
	artifactDirectory := filepath.Join(repository, ".artifacts", "graph-mount-integration")
	prepareMountedFixtureAssetsForPackage(t, repository, artifactDirectory, "./internal/frontendtest/graphmountfixture")
	server := newMountedFixtureServer(t, artifactDirectory, &mountedRenderCounter{counts: map[string]int{}})
	defer server.Close()

	instance, err := playwright.Run(&playwright.RunOptions{DriverDirectory: os.Getenv("PLAYWRIGHT_DRIVER_PATH"), SkipInstallBrowsers: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = instance.Stop() }()
	launch := playwright.BrowserTypeLaunchOptions{Headless: playwright.Bool(true)}
	if executable := os.Getenv("CODEFLUX_CHROMIUM_EXECUTABLE"); executable != "" {
		launch.ExecutablePath = playwright.String(executable)
	}
	browser, err := instance.Chromium.Launch(launch)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = browser.Close() }()
	page, err := browser.NewPage(playwright.BrowserNewPageOptions{Viewport: &playwright.Size{Width: 1440, Height: 1000}})
	if err != nil {
		t.Fatal(err)
	}
	page.SetDefaultTimeout(15_000)
	if _, err := page.Goto(server.URL); err != nil {
		t.Fatal(err)
	}
	fixture := page.GetByTestId("graph-mount-fixture")
	if err := browserAssertions().Locator(fixture).ToBeVisible(); err != nil {
		t.Fatal(err)
	}
	targetID := "nod_01890f3c-4a00-7abc-8def-000000000002"
	graphRoot := fixture.Locator(`[data-component="authoritative-task-graph"]`)
	targetNode := graphRoot.Locator(`[data-node-id="` + targetID + `"]`)
	if err := browserAssertions().Locator(targetNode).ToBeVisible(); err != nil {
		t.Fatalf("authoritative graph node is not visually rendered: %v", err)
	}
	shape := targetNode.Locator("polygon, rect").Last()
	if err := browserAssertions().Locator(shape).ToHaveAttribute("stroke-width", "2"); err != nil {
		t.Fatalf("authoritative graph shape lost numeric SVG attributes: %v", err)
	}
	if err := targetNode.Press("Enter"); err != nil {
		t.Fatalf("select authoritative graph node: %v", err)
	}
	m19WaitForCounter(t, fixture, "data-detail-loads", 1, 2*time.Second)
	inspector := fixture.Locator(`[data-component="graph-node-inspector"]`)
	if err := browserAssertions().Locator(inspector).ToHaveAttribute("data-node-id", targetID); err != nil {
		t.Fatalf("selected node did not load into inspector: %v", err)
	}
	if err := inspector.Locator("#graph-inspector-action-expand-neighbors").Click(); err != nil {
		t.Fatalf("dispatch graph expansion action: %v", err)
	}
	m19WaitForCounter(t, fixture, "data-action-count", 1, 2*time.Second)
}
