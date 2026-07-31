package frontendtest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/mxschmitt/playwright-go"
)

const mountedRenderIsolationEnvironment = "CODEFLUX_RUN_MOUNTED_RENDER_ISOLATION"

type mountedRenderEvidence struct {
	SchemaVersion int                  `json:"schema_version"`
	Runtime       string               `json:"runtime"`
	Events        []mountedRenderEvent `json:"events"`
}

type mountedRenderEvent struct {
	Event  RenderEvent  `json:"event"`
	Before RenderCounts `json:"before"`
	After  RenderCounts `json:"after"`
}

type mountedRenderCounter struct {
	mu     sync.Mutex
	counts map[string]int
}

func (counter *mountedRenderCounter) record(boundary string) {
	counter.mu.Lock()
	defer counter.mu.Unlock()
	counter.counts[boundary]++
}

func TestMountedGWCInteractionsPreserveRenderIsolation(t *testing.T) {
	if os.Getenv(mountedRenderIsolationEnvironment) != "1" {
		t.Skip("set " + mountedRenderIsolationEnvironment + "=1 to build and run mounted browser evidence")
	}

	repository := repositoryRootForMountedTest(t)
	artifactDirectory := filepath.Join(repository, ".artifacts", "m16-mounted-render-isolation")
	prepareMountedFixtureAssets(t, repository, artifactDirectory)

	counter := &mountedRenderCounter{counts: map[string]int{}}
	server := newMountedFixtureServer(t, artifactDirectory, counter)
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
		t.Fatalf("load mounted render fixture: %v", err)
	}
	if err := browserAssertions().Locator(page.GetByTestId("render-isolation-fixture")).ToBeVisible(); err != nil {
		t.Fatalf("wait for mounted GWC fixture: %v", err)
	}
	initial := mountedDOMRevisions(t, page)
	if initial != (RenderCounts{Thread: 1, Graph: 1, Message: 1, Cost: 1}) {
		t.Fatalf("initial mounted DOM revisions = %+v", initial)
	}

	evidence := mountedRenderEvidence{SchemaVersion: 1, Runtime: "GWC v5 mounted in Chromium"}
	evidence.Events = append(evidence.Events, exerciseMountedRenderEvent(
		t,
		page,
		RenderEventCostUpdate,
		func() error { return page.GetByTestId("update-cost").Click() },
	))
	evidence.Events = append(evidence.Events, exerciseMountedRenderEvent(
		t,
		page,
		RenderEventChatAppend,
		func() error {
			_, err := page.GetByTestId("append-message").SelectOption(
				playwright.SelectOptionValues{Values: &[]string{"append"}},
			)
			return err
		},
	))
	evidence.Events = append(evidence.Events, exerciseMountedRenderEvent(
		t,
		page,
		RenderEventGraphSelection,
		func() error {
			node := page.Locator(`[data-node-id="node-2"]`)
			if err := node.Click(); err != nil {
				return err
			}
			return browserAssertions().Locator(node).ToHaveAttribute("aria-pressed", "true")
		},
	))
	t.Run("M18SessionConnectionAcceptance", func(t *testing.T) {
		exerciseMountedM18SessionAcceptance(t, page)
	})

	encoded, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		t.Fatalf("encode render evidence: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(artifactDirectory, "render-isolation-evidence.json"),
		append(encoded, '\n'),
		0o644,
	); err != nil {
		t.Fatalf("write render evidence: %v", err)
	}
	for _, event := range evidence.Events {
		t.Logf("%s: before=%+v after=%+v", event.Event, event.Before, event.After)
	}
}

func TestMountedGWCM18JourneyAcceptance(t *testing.T) {
	if os.Getenv(mountedRenderIsolationEnvironment) != "1" {
		t.Skip("set " + mountedRenderIsolationEnvironment + "=1 to build and run mounted browser evidence")
	}

	repository := repositoryRootForMountedTest(t)
	artifactDirectory := filepath.Join(repository, ".artifacts", "m18-mounted-journey")
	prepareMountedFixtureAssets(t, repository, artifactDirectory)

	server := newMountedFixtureServer(
		t,
		artifactDirectory,
		&mountedRenderCounter{counts: map[string]int{}},
	)
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
		t.Fatalf("load mounted M18 fixture: %v", err)
	}
	if err := browserAssertions().Locator(page.GetByTestId("render-isolation-fixture")).ToBeVisible(); err != nil {
		t.Fatalf("wait for mounted M18 fixture: %v", err)
	}
	exerciseMountedM18SessionAcceptance(t, page)
	t.Run("M18TaskActionMatrixAcceptance", func(t *testing.T) {
		exerciseMountedM18TaskActionMatrixAcceptance(t, page)
	})
}

func exerciseMountedRenderEvent(
	t *testing.T,
	page playwright.Page,
	event RenderEvent,
	interact func() error,
) mountedRenderEvent {
	t.Helper()
	before := mountedDOMRevisions(t, page)
	if err := interact(); err != nil {
		t.Fatalf("%s browser interaction: %v", event, err)
	}
	want := before
	var owner playwright.Locator
	switch event {
	case RenderEventCostUpdate:
		want.Cost++
		owner = page.Locator(`[data-component="application-bar"]`)
	case RenderEventChatAppend:
		want.Message++
		owner = page.Locator(`[data-component="conversation-pane"]`).First()
	case RenderEventGraphSelection:
		want.Graph++
		owner = page.Locator(`[data-component="graph-pane"]`).First()
	default:
		t.Fatalf("unknown mounted render event %q", event)
	}
	var revision int
	switch event {
	case RenderEventCostUpdate:
		revision = want.Cost
	case RenderEventChatAppend:
		revision = want.Message
	case RenderEventGraphSelection:
		revision = want.Graph
	}
	if err := browserAssertions().Locator(owner).ToHaveAttribute("data-revision", strconv.Itoa(revision)); err != nil {
		t.Fatalf("%s owning DOM revision: %v", event, err)
	}
	after := mountedDOMRevisions(t, page)
	if err := ValidateRenderIsolation(before, after, event); err != nil {
		t.Fatalf("%s ownership contract: %v", event, err)
	}
	if err := validateOnlyOwningSubtreeChanged(before, after, event); err != nil {
		t.Fatalf("%s exclusive mounted ownership: %v", event, err)
	}
	return mountedRenderEvent{Event: event, Before: before, After: after}
}

func mountedDOMRevisions(t *testing.T, page playwright.Page) RenderCounts {
	t.Helper()
	read := func(component string, first bool) int {
		locator := page.Locator(`[data-component="` + component + `"]`)
		if first {
			locator = locator.First()
		}
		raw, err := locator.GetAttribute("data-revision")
		if err != nil {
			t.Fatalf("read %s DOM revision: %v", component, err)
		}
		value, err := strconv.Atoi(raw)
		if err != nil {
			t.Fatalf("parse %s DOM revision %q: %v", component, raw, err)
		}
		return value
	}
	return RenderCounts{
		Thread: read("thread-rail", true), Graph: read("graph-pane", true),
		Message: read("conversation-pane", true), Cost: read("application-bar", true),
	}
}

func validateOnlyOwningSubtreeChanged(before, after RenderCounts, event RenderEvent) error {
	want := before
	switch event {
	case RenderEventCostUpdate:
		want.Cost = after.Cost
	case RenderEventChatAppend:
		want.Message = after.Message
	case RenderEventGraphSelection:
		want.Graph = after.Graph
	default:
		return fmt.Errorf("unknown render event %q", event)
	}
	if after != want {
		return fmt.Errorf("non-owning subtree rendered: before=%+v after=%+v", before, after)
	}
	return nil
}

func newMountedFixtureServer(
	t *testing.T,
	assetDirectory string,
	counter *mountedRenderCounter,
) *httptest.Server {
	t.Helper()
	files := http.FileServer(http.Dir(assetDirectory))
	mux := http.NewServeMux()
	mux.HandleFunc("POST /render/{boundary}", func(writer http.ResponseWriter, request *http.Request) {
		boundary := request.PathValue("boundary")
		switch boundary {
		case "application-bar", "thread-rail", "conversation-pane", "graph-pane":
			counter.record(boundary)
			writer.WriteHeader(http.StatusNoContent)
		default:
			http.Error(writer, "unknown render boundary", http.StatusBadRequest)
		}
	})
	mux.Handle("/", files)
	return httptest.NewServer(mux)
}

func prepareMountedFixtureAssets(t *testing.T, repository, artifactDirectory string) {
	prepareMountedFixtureAssetsForPackage(t, repository, artifactDirectory, "./internal/frontendtest/renderfixture")
}

func prepareMountedFixtureAssetsForPackage(t *testing.T, repository, artifactDirectory, fixturePackage string) {
	t.Helper()
	relative, err := filepath.Rel(filepath.Join(repository, ".artifacts"), artifactDirectory)
	if err != nil || relative == "." || relative == ".." || filepath.IsAbs(relative) ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		t.Fatalf("render fixture artifact path escaped .artifacts: %q", artifactDirectory)
	}
	if err := os.RemoveAll(artifactDirectory); err != nil {
		t.Fatalf("clear render fixture artifacts: %v", err)
	}

	moduleDirectory := strings.TrimSpace(runMountedCommand(
		t,
		repository,
		nil,
		"go", "list", "-m", "-f={{.Dir}}", "github.com/monstercameron/GoWebComponents/v5",
	))
	runMountedCommand(
		t,
		moduleDirectory,
		nil,
		"go", "run", "./tools/gwc", "scaffold",
		"-kind", "app",
		"-root", filepath.Dir(artifactDirectory),
		"-dir", filepath.Base(artifactDirectory),
		"-name", "CodefluxRenderIsolation",
		"-module", "codeflux.dev/codeflux/render-isolation-fixture",
		"-no-input",
		"-json",
	)

	goRoot := strings.TrimSpace(runMountedCommand(t, repository, nil, "go", "env", "GOROOT"))
	shim := filepath.Join(goRoot, "lib", "wasm", "wasm_exec.js")
	if _, err := os.Stat(shim); err != nil {
		shim = filepath.Join(goRoot, "misc", "wasm", "wasm_exec.js")
	}
	copyMountedFile(t, shim, filepath.Join(artifactDirectory, "wasm_exec.js"))
	if err := os.MkdirAll(filepath.Join(artifactDirectory, "bin"), 0o755); err != nil {
		t.Fatalf("create render fixture binary directory: %v", err)
	}
	environment := append(os.Environ(), "GOOS=js", "GOARCH=wasm")
	runMountedCommand(
		t,
		repository,
		environment,
		"go", "build", "-trimpath",
		"-o", filepath.Join(artifactDirectory, "bin", "main.wasm"),
		fixturePackage,
	)
}

func runMountedCommand(t *testing.T, directory string, environment []string, name string, args ...string) string {
	t.Helper()
	command := exec.Command(name, args...)
	command.Dir = directory
	if environment != nil {
		command.Env = environment
	}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("run %s %s: %v\n%s", name, strings.Join(args, " "), err, output)
	}
	return string(output)
}

func copyMountedFile(t *testing.T, source, destination string) {
	t.Helper()
	input, err := os.Open(source)
	if err != nil {
		t.Fatalf("open generated runtime %s: %v", source, err)
	}
	defer input.Close()
	output, err := os.Create(destination)
	if err != nil {
		t.Fatalf("create generated runtime %s: %v", destination, err)
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		t.Fatalf("copy generated runtime: %v", err)
	}
	if err := output.Close(); err != nil {
		t.Fatalf("close generated runtime: %v", err)
	}
}

func repositoryRootForMountedTest(t *testing.T) string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			t.Fatal("repository go.mod not found")
		}
		directory = parent
	}
}

func TestValidateOnlyOwningSubtreeChanged(t *testing.T) {
	before := RenderCounts{Thread: 2, Graph: 3, Message: 5, Cost: 7}
	for _, test := range []struct {
		event RenderEvent
		after RenderCounts
	}{
		{RenderEventCostUpdate, RenderCounts{Thread: 2, Graph: 3, Message: 5, Cost: 8}},
		{RenderEventChatAppend, RenderCounts{Thread: 2, Graph: 3, Message: 6, Cost: 7}},
		{RenderEventGraphSelection, RenderCounts{Thread: 2, Graph: 4, Message: 5, Cost: 7}},
	} {
		if err := validateOnlyOwningSubtreeChanged(before, test.after, test.event); err != nil {
			t.Errorf("%s: %v", test.event, err)
		}
	}
	if err := validateOnlyOwningSubtreeChanged(
		before,
		RenderCounts{Thread: 2, Graph: 4, Message: 6, Cost: 7},
		RenderEventGraphSelection,
	); err == nil {
		t.Fatal("cross-boundary graph selection passed exclusive validator")
	}
}
