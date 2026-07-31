package frontendtest

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/mxschmitt/playwright-go"
)

const (
	m19GraphPatchP95Limit        = 100 * time.Millisecond
	m19InputFeedbackP95Limit     = 100 * time.Millisecond
	m19OrderedStreamP95Limit     = 250 * time.Millisecond
	m19MaximumAuthoritativeDOM   = 2400
	m19ExpectedSVGNodeCount      = 300
	m19ExpectedSVGEdgeCount      = 299
	m19ExpectedVisibleLabelCount = 600
)

func TestMountedGWCM19GraphPerformanceAcceptance(t *testing.T) {
	if os.Getenv(mountedRenderIsolationEnvironment) != "1" {
		t.Skip("set " + mountedRenderIsolationEnvironment + "=1 to build and run mounted browser evidence")
	}

	repository := repositoryRootForMountedTest(t)
	artifactDirectory := filepath.Join(repository, ".artifacts", "m19-mounted-graph-performance")
	prepareMountedFixtureAssetsForPackage(
		t, repository, artifactDirectory, "./internal/frontendtest/m19renderfixture",
	)
	server := newMountedFixtureServer(t, artifactDirectory, &mountedRenderCounter{counts: map[string]int{}})
	defer server.Close()

	instance, err := playwright.Run(&playwright.RunOptions{
		DriverDirectory: os.Getenv("PLAYWRIGHT_DRIVER_PATH"), SkipInstallBrowsers: true,
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
	page, err := browser.NewPage(playwright.BrowserNewPageOptions{Viewport: &playwright.Size{Width: 1440, Height: 1000}})
	if err != nil {
		t.Fatalf("create browser page: %v", err)
	}
	page.SetDefaultTimeout(15_000)
	if _, err := page.Goto(server.URL); err != nil {
		t.Fatalf("load M19 mounted fixture: %v", err)
	}
	fixture := page.GetByTestId("m19-performance-fixture")
	if err := browserAssertions().Locator(fixture).ToBeVisible(); err != nil {
		t.Fatalf("wait for M19 mounted fixture: %v", err)
	}
	graphRoot := fixture.Locator(`[data-component="authoritative-task-graph"]`)
	if err := browserAssertions().Locator(graphRoot).ToBeVisible(); err != nil {
		t.Fatalf("wait for authoritative graph: %v", err)
	}
	if err := browserAssertions().Locator(graphRoot).ToHaveAttribute("data-measured-fit-applied", "true"); err != nil {
		t.Fatalf("wait for measured graph viewport: %v", err)
	}

	nodeCount := m19LocatorCount(t, graphRoot.Locator(`[data-component="graph-node"]`))
	edgeCount := m19LocatorCount(t, graphRoot.Locator(`[data-component="graph-edge"]`))
	labelCount := m19LocatorCount(t, graphRoot.Locator(`[data-component="graph-node"] text`))
	domCount := m19LocatorCount(t, graphRoot.Locator("*"))
	if nodeCount != m19ExpectedSVGNodeCount || edgeCount != m19ExpectedSVGEdgeCount ||
		labelCount != m19ExpectedVisibleLabelCount || domCount > m19MaximumAuthoritativeDOM {
		t.Fatalf(
			"M19 SVG/DOM counts nodes=%d edges=%d labels=%d DOM=%d; want %d/%d/%d and DOM<=%d",
			nodeCount, edgeCount, labelCount, domCount,
			m19ExpectedSVGNodeCount, m19ExpectedSVGEdgeCount, m19ExpectedVisibleLabelCount, m19MaximumAuthoritativeDOM,
		)
	}
	t.Logf("M19-105 mounted SVG counts: nodes=%d edges=%d visible-labels=%d descendant-DOM=%d threshold=%d", nodeCount, edgeCount, labelCount, domCount, m19MaximumAuthoritativeDOM)

	graphStream := fixture.Locator(`[data-component="m19-graph-stream"]`)
	tokenStream := fixture.Locator(`[data-component="m19-token-stream"]`)
	if err := fixture.GetByTestId("m19-start-tokens").Click(); err != nil {
		t.Fatalf("start token stream: %v", err)
	}

	patchLatencies := make([]time.Duration, 0, 100)
	tokenLatencies := make([]time.Duration, 0, 100)
	patchValue := m19NumericAttribute(t, graphStream, "data-patch-count")
	tokenValue := m19NumericAttribute(t, tokenStream, "data-token-count")
	lastTokenAdvance := time.Now()
	applyPatch := fixture.GetByTestId("m19-apply-patch")
	for patchValue < 100 {
		started := time.Now()
		if err := applyPatch.Click(); err != nil {
			t.Fatalf("apply graph patch %d: %v", patchValue+1, err)
		}
		patchValue = m19WaitForCounter(t, graphStream, "data-patch-count", patchValue+1, 2*time.Second)
		patchLatencies = append(patchLatencies, time.Since(started))
		currentToken := m19NumericAttribute(t, tokenStream, "data-token-count")
		if currentToken > tokenValue {
			tokenLatencies = append(tokenLatencies, time.Since(lastTokenAdvance))
			tokenValue = currentToken
			lastTokenAdvance = time.Now()
		} else if time.Since(lastTokenAdvance) >= m19OrderedStreamP95Limit {
			t.Fatalf("chat stream stalled for %s during graph patches", time.Since(lastTokenAdvance))
		}
	}
	_, patchP95, patchMax := m19LatencySummary(patchLatencies)
	if patchP95 >= m19GraphPatchP95Limit {
		t.Fatalf("100 mounted graph patch commits p95=%s exceeds %s", patchP95, m19GraphPatchP95Limit)
	}
	if len(tokenLatencies) == 0 {
		t.Fatal("chat stream did not advance during graph patches")
	}
	_, tokenP95, tokenMax := m19LatencySummary(tokenLatencies)
	if tokenP95 >= m19OrderedStreamP95Limit {
		t.Fatalf("chat streaming during graph patches p95=%s exceeds %s", tokenP95, m19OrderedStreamP95Limit)
	}

	interactionLatencies := make([]time.Duration, 0, 20)
	for range 20 {
		selectedBefore, err := graphRoot.GetAttribute("data-selected-node-id")
		if err != nil || selectedBefore == "" {
			t.Fatalf("read selected graph node: value=%q err=%v", selectedBefore, err)
		}
		selectedNode := graphRoot.Locator(`[data-node-id="` + selectedBefore + `"]`)
		started := time.Now()
		if err := selectedNode.Press("ArrowRight"); err != nil {
			t.Fatalf("traverse graph during token stream: %v", err)
		}
		m19WaitForAttributeChange(t, graphRoot, "data-selected-node-id", selectedBefore, 2*time.Second)
		interactionLatencies = append(interactionLatencies, time.Since(started))
	}
	_, interactionP95, interactionMax := m19LatencySummary(interactionLatencies)
	if interactionP95 >= m19InputFeedbackP95Limit {
		t.Fatalf("graph interaction during token streaming p95=%s exceeds %s", interactionP95, m19InputFeedbackP95Limit)
	}

	t.Logf("M19-106 chat token response during patches: p95=%s max=%s threshold=%s", tokenP95, tokenMax, m19OrderedStreamP95Limit)
	t.Logf("M19-107 graph keyboard response during tokens: p95=%s max=%s threshold=%s", interactionP95, interactionMax, m19InputFeedbackP95Limit)
	t.Logf("M19-104 mounted 100-patch commit: observed=%d p95=%s max=%s threshold=%s", patchValue, patchP95, patchMax, m19GraphPatchP95Limit)
}

func m19LocatorCount(t *testing.T, locator playwright.Locator) int {
	t.Helper()
	count, err := locator.Count()
	if err != nil {
		t.Fatal(err)
	}
	return count
}

func m19NumericAttribute(t *testing.T, locator playwright.Locator, name string) int {
	t.Helper()
	raw, err := locator.GetAttribute(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		t.Fatalf("parse %s=%q: %v", name, raw, err)
	}
	return value
}

func m19WaitForCounter(t *testing.T, locator playwright.Locator, name string, minimum int, timeout time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		value := m19NumericAttribute(t, locator, name)
		if value >= minimum {
			return value
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("%s did not reach %d within %s", name, minimum, timeout)
	return 0
}

func m19WaitForAttributeChange(t *testing.T, locator playwright.Locator, name, previous string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		value, err := locator.GetAttribute(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if value != "" && value != previous {
			return value
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("%s did not change from %q within %s", name, previous, timeout)
	return ""
}

func m19LatencySummary(samples []time.Duration) (p50, p95, maximum time.Duration) {
	ordered := append([]time.Duration(nil), samples...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	return ordered[(len(ordered)-1)*50/100], ordered[(len(ordered)-1)*95/100], ordered[len(ordered)-1]
}
