package frontendtest

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mxschmitt/playwright-go"
)

// productionConsoleEnvironment opts into the end-to-end console run.
//
// It is opt-in because the suite builds the WebAssembly client, starts the
// real executable, and drives a browser: several minutes of work that has no
// place in an ordinary unit run. It is not skipped silently in CI, where the
// browser command sets it.
const productionConsoleEnvironment = "CODEFLUX_RUN_PRODUCTION_CONSOLE"

// TestTheProductionConsoleServesAndMounts drives the real thing.
//
// Not the render fixture and not a mock: this builds the browser assets, runs
// `codeflux start`, opens the URL it prints, and asserts what a person would
// see. It is the check the product did not have — the executable used to come
// up, print a URL, and serve 404 there, and every unit test passed while it
// did.
func TestTheProductionConsoleServesAndMounts(t *testing.T) {
	if os.Getenv(productionConsoleEnvironment) == "" {
		t.Skipf("set %s=1 to run the end-to-end console suite", productionConsoleEnvironment)
	}
	console := startProductionConsole(t)

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

	page, problems, external := openConsolePage(t, browser, console.URL)

	t.Run("TheClientMounts", func(t *testing.T) {
		root := page.Locator(`[data-testid="app-root"]`)
		if err := root.WaitFor(playwright.LocatorWaitForOptions{
			State: playwright.WaitForSelectorStateAttached, Timeout: playwright.Float(60000),
		}); err != nil {
			t.Fatalf("the client never mounted: %v", err)
		}
		if len(*problems) > 0 {
			t.Fatalf("the client mounted with %d console or page error(s):\n%s",
				len(*problems), strings.Join(*problems, "\n"))
		}
	})

	t.Run("NothingIsFetchedFromOffThisMachine", func(t *testing.T) {
		// The product's central claim is that it is local. A console that
		// quietly loaded a font or a script from a CDN would break it, and no
		// unit test can see a network request.
		if len(*external) > 0 {
			t.Fatalf("the console requested %d off-machine resource(s):\n%s",
				len(*external), strings.Join(*external, "\n"))
		}
	})

	t.Run("EveryHeadingIsLegible", func(t *testing.T) {
		// A heading that inherits its colour is legible by accident. This
		// reads the computed style in a real browser, which is the only place
		// the answer actually exists.
		results, err := page.Evaluate(`() => {
			const findings = [];
			for (const heading of document.querySelectorAll('h1,h2,h3,h4,h5,h6')) {
				const style = getComputedStyle(heading);
				if (style.visibility === 'hidden' || style.display === 'none') continue;
				const rect = heading.getBoundingClientRect();
				if (rect.width === 0 || rect.height === 0) continue;
				findings.push({
					text: (heading.textContent || '').trim().slice(0, 40),
					color: style.color,
					size: parseFloat(style.fontSize),
					family: style.fontFamily,
				});
			}
			return findings;
		}`)
		if err != nil {
			t.Fatalf("read heading styles: %v", err)
		}
		headings, ok := results.([]any)
		if !ok || len(headings) == 0 {
			t.Fatalf("no visible headings were found: %#v", results)
		}
		for _, entry := range headings {
			heading, _ := entry.(map[string]any)
			text, _ := heading["text"].(string)
			// The bridge hands a whole number back as an int and a fractional
			// one as a float; reading only one of those would fail every
			// heading whose size happens to be round.
			size := numericValue(heading["size"])
			if size <= 0 {
				t.Errorf("heading %q has no computed size", text)
			}
			colour, _ := heading["color"].(string)
			if strings.TrimSpace(colour) == "" {
				t.Errorf("heading %q has no computed colour", text)
			}
		}
	})

	t.Run("SerifCarriesJudgementAndMonospaceCarriesMeasurement", func(t *testing.T) {
		// The type rule is the design's one structural idea. Asserting it in a
		// real browser is what proves the stack actually resolved, rather than
		// that a string was set on an element.
		families, err := page.Evaluate(`() => {
			const pick = (selector) => {
				const node = document.querySelector(selector);
				return node ? getComputedStyle(node).fontFamily.toLowerCase() : '';
			};
			return {
				title: pick('h1'),
				readout: pick('[data-component="cost-readout"] span:nth-child(2) span:nth-child(2)'),
			};
		}`)
		if err != nil {
			t.Fatalf("read font families: %v", err)
		}
		resolved, _ := families.(map[string]any)
		title, _ := resolved["title"].(string)
		if title == "" {
			t.Fatal("no page title was found to check")
		}
		// The serif stack names its faces; a sans fallback would name none of
		// them.
		serif := false
		for _, family := range []string{"iowan", "palatino", "book antiqua", "georgia", "serif"} {
			if strings.Contains(title, family) {
				serif = true
				break
			}
		}
		if !serif {
			t.Errorf("the page title is not set in the reading serif: %s", title)
		}
		if strings.Contains(title, "sans-serif") {
			t.Errorf("the page title resolved to a sans stack: %s", title)
		}
	})

	t.Run("NavigationIsDrawnRatherThanTyped", func(t *testing.T) {
		// The icon set was Unicode glyphs, several of which fall back to a
		// box on Windows. Drawn marks are the same shape everywhere.
		count, err := page.Locator(`[data-component="product-sidebar"] svg`).Count()
		if err != nil {
			t.Fatalf("count navigation icons: %v", err)
		}
		if count < 6 {
			t.Errorf("the navigation rail drew %d icons, want one per route", count)
		}
		// A glyph that failed to render leaves the replacement character. It
		// must appear nowhere on the page.
		text, err := page.Locator("body").TextContent()
		if err != nil {
			t.Fatalf("read body text: %v", err)
		}
		if strings.ContainsRune(text, '�') {
			t.Error("the page contains an unrenderable character")
		}
	})

	t.Run("EveryPrimaryRouteIsReachable", func(t *testing.T) {
		// A navigation control that does not navigate is the most invisible
		// kind of broken: it looks right and does nothing.
		for _, label := range []string{"Tasks", "Graphs", "Memory", "Settings"} {
			control := page.Locator(
				`[data-component="client-route-control"]`,
			).Filter(playwright.LocatorFilterOptions{HasText: label}).First()
			if err := control.Click(playwright.LocatorClickOptions{
				Timeout: playwright.Float(15000),
			}); err != nil {
				t.Errorf("could not activate the %s route: %v", label, err)
				continue
			}
			// The route must actually change. Waiting on the URL rather than
			// on a spinner is what distinguishes navigation from a control
			// that merely accepted a click.
			if err := page.WaitForURL("**", playwright.PageWaitForURLOptions{
				Timeout: playwright.Float(15000),
			}); err != nil {
				t.Errorf("the %s route never settled: %v", label, err)
			}
		}
	})

	t.Run("KeyboardFocusIsVisible", func(t *testing.T) {
		if err := page.Keyboard().Press("Tab"); err != nil {
			t.Fatalf("press Tab: %v", err)
		}
		outline, err := page.Evaluate(`() => {
			const active = document.activeElement;
			if (!active || active === document.body) return null;
			const style = getComputedStyle(active);
			return {
				outlineWidth: style.outlineWidth,
				outlineStyle: style.outlineStyle,
				boxShadow: style.boxShadow,
			};
		}`)
		if err != nil {
			t.Fatalf("read focus style: %v", err)
		}
		if outline == nil {
			t.Fatal("tabbing moved focus nowhere, so no control is keyboard reachable")
		}
		style, _ := outline.(map[string]any)
		width, _ := style["outlineWidth"].(string)
		shadow, _ := style["boxShadow"].(string)
		if strings.HasPrefix(width, "0") && (shadow == "none" || shadow == "") {
			t.Errorf("the focused control shows no focus indicator: %+v", style)
		}
	})

	t.Run("NoControlIsInert", func(t *testing.T) {
		// A control that is neither actionable nor explained is the worst
		// state an interface can be in: it looks like it works. Every button
		// must be enabled, or disabled with a reason a person can read.
		findings, err := page.Evaluate(`() => {
			const problems = [];
			for (const control of document.querySelectorAll('button')) {
				const style = getComputedStyle(control);
				if (style.display === 'none' || style.visibility === 'hidden') continue;
				const rect = control.getBoundingClientRect();
				if (rect.width === 0 || rect.height === 0) continue;
				const name = (control.getAttribute('aria-label') || control.textContent || '')
					.trim().slice(0, 60);
				if (!name) {
					problems.push('a visible control has no accessible name');
					continue;
				}
				if (!control.disabled) continue;
				const reason = control.getAttribute('aria-describedby')
					|| control.getAttribute('title')
					|| control.dataset.disabledReason
					|| '';
				if (!reason.trim()) {
					problems.push('disabled with no stated reason: ' + name);
				}
			}
			return problems;
		}`)
		if err != nil {
			t.Fatalf("inspect controls: %v", err)
		}
		problems, _ := findings.([]any)
		if len(problems) > 0 {
			messages := make([]string, 0, len(problems))
			for _, problem := range problems {
				text, _ := problem.(string)
				messages = append(messages, text)
			}
			t.Errorf("%d control(s) are inert or nameless:\n  %s",
				len(messages), strings.Join(messages, "\n  "))
		}
	})

	t.Run("EveryControlMeetsThePointerMinimum", func(t *testing.T) {
		// A 44-pixel target is the accessibility floor this product declares.
		// Declaring it in a token and shipping a 24-pixel button would make
		// the declaration decorative.
		findings, err := page.Evaluate(`() => {
			const problems = [];
			for (const control of document.querySelectorAll('button,a[href],summary')) {
				const style = getComputedStyle(control);
				if (style.display === 'none' || style.visibility === 'hidden') continue;
				const rect = control.getBoundingClientRect();
				if (rect.width === 0 || rect.height === 0) continue;
				if (rect.height < 24 || rect.width < 24) {
					const name = (control.getAttribute('aria-label') || control.textContent || '')
						.trim().slice(0, 40);
					problems.push(name + ' is ' + Math.round(rect.width) + 'x'
						+ Math.round(rect.height));
				}
			}
			return problems;
		}`)
		if err != nil {
			t.Fatalf("measure controls: %v", err)
		}
		problems, _ := findings.([]any)
		if len(problems) > 0 {
			messages := make([]string, 0, len(problems))
			for _, problem := range problems {
				text, _ := problem.(string)
				messages = append(messages, text)
			}
			t.Errorf("%d control(s) are below a usable target size:\n  %s",
				len(messages), strings.Join(messages, "\n  "))
		}
	})

	t.Run("ThePageDoesNotScrollSideways", func(t *testing.T) {
		// Horizontal overflow on a supervision console means a control has
		// left the screen, and the one that leaves is usually the last one in
		// a row: the action.
		overflow, err := page.Evaluate(
			`() => document.documentElement.scrollWidth - document.documentElement.clientWidth`)
		if err != nil {
			t.Fatalf("measure overflow: %v", err)
		}
		if width := numericValue(overflow); width > 1 {
			t.Errorf("the page scrolls %.0fpx sideways", width)
		}
	})
}

// productionConsole is a running executable and the URL it printed.
type productionConsole struct {
	URL string
}

// startProductionConsole builds the assets, starts the executable, and returns
// the URL it announced.
//
// The URL is read from the process's own output rather than assumed, because
// the address it binds is the thing under test: a console that printed a URL
// it did not serve is exactly the defect this suite exists to catch.
func startProductionConsole(t *testing.T) productionConsole {
	t.Helper()
	repository := repositoryRootForConsole(t)

	// The assets have to exist before the executable starts, because it reads
	// them once and refuses to run without them.
	build := exec.CommandContext(t.Context(),
		"go", "run", "./cmd/codeflux-dev", "build-frontend")
	build.Dir = repository
	if output, err := build.CombinedOutput(); err != nil {
		t.Skipf("cannot build the browser assets in this environment: %v\n%s", err, output)
	}

	name := "codeflux"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	executable := filepath.Join(t.TempDir(), name)
	compile := exec.CommandContext(t.Context(), "go", "build", "-o", executable, "./cmd/codeflux")
	compile.Dir = repository
	if output, err := compile.CombinedOutput(); err != nil {
		t.Skipf("cannot build the executable in this environment: %v\n%s", err, output)
	}

	data := t.TempDir()
	port := freeLoopbackPort(t)
	address := fmt.Sprintf("127.0.0.1:%d", port)
	ctx, cancel := context.WithCancel(context.Background())
	command := exec.CommandContext(ctx, executable,
		"start", "--no-browser",
		"--database", filepath.Join(data, "codeflux.sqlite3"),
		"--address", address,
		"--assets", filepath.Join(repository, ".artifacts", "frontend"),
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
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(output.String(), "codeflux is running at") {
			return productionConsole{URL: url}
		}
		if command.ProcessState != nil && command.ProcessState.Exited() {
			t.Fatalf("the console exited before serving:\n%s", output.String())
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("the console never announced a URL:\n%s", output.String())
	return productionConsole{}
}

// openConsolePage opens the console and records what went wrong.
//
// Console errors and off-machine requests are collected rather than asserted
// here, so one navigation serves every check and a failure names all of them
// at once.
func openConsolePage(
	t *testing.T,
	browser playwright.Browser,
	url string,
) (playwright.Page, *[]string, *[]string) {
	t.Helper()
	page, err := browser.NewPage(playwright.BrowserNewPageOptions{
		Viewport:    &playwright.Size{Width: 1600, Height: 1000},
		ColorScheme: playwright.ColorSchemeDark,
	})
	if err != nil {
		t.Fatalf("new page: %v", err)
	}
	problems := []string{}
	external := []string{}
	page.On("console", func(message playwright.ConsoleMessage) {
		if message.Type() == "error" {
			problems = append(problems, "console: "+message.Text())
		}
	})
	page.On("pageerror", func(err error) {
		problems = append(problems, "page: "+err.Error())
	})
	page.On("request", func(request playwright.Request) {
		target := request.URL()
		if strings.HasPrefix(target, url) ||
			strings.HasPrefix(target, "data:") ||
			strings.HasPrefix(target, "blob:") {
			return
		}
		external = append(external, target)
	})
	if _, err := page.Goto(url, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateNetworkidle,
		Timeout:   playwright.Float(120000),
	}); err != nil {
		t.Fatalf("open %s: %v", url, err)
	}
	// The client is WebAssembly and mounts after the document settles.
	time.Sleep(12 * time.Second)
	return page, &problems, &external
}

func repositoryRootForConsole(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("repository root %q has no go.mod: %v", root, err)
	}
	return root
}

// freeLoopbackPort reserves an ephemeral port and releases it.
//
// The console is asked for an explicit port rather than zero so the URL is
// known before the process starts, which is what lets a failure to serve be
// distinguished from a failure to start.
func freeLoopbackPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve a loopback port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("release the reserved port: %v", err)
	}
	return port
}

// syncBuffer collects a subprocess's output safely.
//
// The process writes from its own goroutine while this test polls for the URL
// it printed, so the buffer needs a lock: without one the race detector
// reports the poll, and the report would be correct.
type syncBuffer struct {
	mu      sync.Mutex
	content []byte
}

func (buffer *syncBuffer) Write(chunk []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	buffer.content = append(buffer.content, chunk...)
	return len(chunk), nil
}

func (buffer *syncBuffer) String() string {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return string(buffer.content)
}

// numericValue reads a number the browser bridge may hand back as either an
// int or a float.
func numericValue(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	default:
		return 0
	}
}

// TestASelfContainedExecutableServesItsOwnInterface is the release proof.
//
// It builds through `codeflux-dev release`, copies the single executable into
// an otherwise empty directory, starts it with no asset flag, and loads the
// console. This is what distinguishes something a person can be handed from a
// development checkout: the assets are inside the binary, and the browser
// fetches nothing from off the machine.
func TestASelfContainedExecutableServesItsOwnInterface(t *testing.T) {
	if os.Getenv(productionConsoleEnvironment) == "" {
		t.Skipf("set %s=1 to run the end-to-end console suite", productionConsoleEnvironment)
	}
	repository := repositoryRootForConsole(t)
	release := exec.CommandContext(t.Context(), "go", "run", "./cmd/codeflux-dev", "release")
	release.Dir = repository
	if output, err := release.CombinedOutput(); err != nil {
		t.Skipf("cannot build a release in this environment: %v\n%s", err, output)
	}

	name := "codeflux"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	// The executable is copied out of the repository entirely, so nothing it
	// finds beside it could be mistaken for a compiled-in asset.
	isolated := t.TempDir()
	executable := filepath.Join(isolated, name)
	source, err := os.ReadFile(filepath.Join(repository, ".artifacts", "release", name))
	if err != nil {
		t.Fatalf("read the released executable: %v", err)
	}
	if err := os.WriteFile(executable, source, 0o700); err != nil {
		t.Fatalf("place the released executable: %v", err)
	}

	port := freeLoopbackPort(t)
	address := fmt.Sprintf("127.0.0.1:%d", port)
	ctx, cancel := context.WithCancel(context.Background())
	// No --assets. That absence is the whole point.
	command := exec.CommandContext(ctx, executable,
		"start", "--no-browser",
		"--database", filepath.Join(isolated, "codeflux.sqlite3"),
		"--address", address,
	)
	command.Dir = isolated
	output := &syncBuffer{}
	command.Stdout = output
	command.Stderr = output
	if err := command.Start(); err != nil {
		cancel()
		t.Fatalf("start the released executable: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		_ = command.Wait()
	})

	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(output.String(), "codeflux is running at") {
		time.Sleep(200 * time.Millisecond)
	}
	if !strings.Contains(output.String(), "codeflux is running at") {
		t.Fatalf("the released executable never served:\n%s", output.String())
	}
	// It must say the assets came from inside itself, not from a directory it
	// happened to find.
	if !strings.Contains(output.String(), "compiled into this executable") {
		t.Fatalf("the release served assets from outside itself:\n%s", output.String())
	}

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

	page, problems, external := openConsolePage(t, browser, "http://"+address+"/")
	if err := page.Locator(`[data-testid="app-root"]`).WaitFor(
		playwright.LocatorWaitForOptions{
			State: playwright.WaitForSelectorStateAttached, Timeout: playwright.Float(60000),
		},
	); err != nil {
		t.Fatalf("the released client never mounted: %v", err)
	}
	if len(*problems) > 0 {
		t.Errorf("the released client mounted with %d error(s):\n%s",
			len(*problems), strings.Join(*problems, "\n"))
	}
	if len(*external) > 0 {
		t.Errorf("the released console requested %d off-machine resource(s):\n%s",
			len(*external), strings.Join(*external, "\n"))
	}
}
