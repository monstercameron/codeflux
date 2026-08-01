package frontendtest

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/mxschmitt/playwright-go"
	_ "modernc.org/sqlite"
)

// TestATypedRequestReachesARunningWorker is the check this product did not have.
//
// The console suite proved the page mounts, connects, and does not lie. It
// never proved the product does its job, and that is exactly how the gap
// survived: sending a request recorded a message and a bare draft task, three
// of the five steps between a request and a worker were unreachable from any
// client, and every test passed. This one types a request into a real browser
// and fails unless a worker is actually running against a real worktree.
func TestATypedRequestReachesARunningWorker(t *testing.T) {
	if os.Getenv(productionConsoleEnvironment) == "" {
		t.Skipf("set %s=1 to run the end-to-end console suite", productionConsoleEnvironment)
	}
	console := startWorkingConsole(t)

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

	page, problems, _ := openConsolePage(t, browser, console.URL)

	// Open the conversation the coordinator says is there, the same way a
	// person following the interface would arrive at it.
	route, err := page.Evaluate(`async () => {
		const payload = await (await fetch("/bootstrap")).json()
		if (!payload.selected_repository_id || !payload.selected_thread_id) { return "" }
		return "workspace/" + payload.selected_repository_id.value +
			"/thread/" + payload.selected_thread_id.value
	}`)
	path, _ := route.(string)
	if err != nil || path == "" {
		t.Fatalf("the coordinator named no conversation to open: %v", err)
	}
	if _, err := page.Goto(console.URL+path, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateNetworkidle,
		Timeout:   playwright.Float(120000),
	}); err != nil {
		t.Fatalf("open the conversation: %v", err)
	}

	// Declare the kind of change. It is the one thing about a task nothing can
	// observe, so nothing may default it.
	if err := page.Locator(
		`[data-component="composer-advanced-options"] summary`).First().Click(
		playwright.LocatorClickOptions{Timeout: playwright.Float(30000)},
	); err != nil {
		t.Fatalf("open the composer options: %v", err)
	}
	if _, err := page.Locator("#composer-task-class").SelectOption(
		playwright.SelectOptionValues{Values: &[]string{"documentation"}},
		playwright.LocatorSelectOptionOptions{Timeout: playwright.Float(30000)},
	); err != nil {
		t.Fatalf("declare the kind of change: %v", err)
	}

	const request = "Add a doc comment to the ResolveRevision function."
	if err := page.Locator("textarea").First().Fill(request,
		playwright.LocatorFillOptions{Timeout: playwright.Float(30000)},
	); err != nil {
		t.Fatalf("type the request: %v", err)
	}
	if err := page.Locator("button").Filter(
		playwright.LocatorFilterOptions{HasText: "Send"},
	).First().Click(playwright.LocatorClickOptions{
		Timeout: playwright.Float(30000),
	}); err != nil {
		t.Fatalf("send the request: %v", err)
	}

	// The durable record is the authority, so the database is read rather than
	// the interface: a page that says "Running" while nothing runs is exactly
	// the failure this test exists to catch.
	deadline := time.Now().Add(90 * time.Second)
	var worktree string
	for time.Now().Before(deadline) {
		state, path := readTaskProgress(t, console.DatabasePath)
		if state == "running" && path != "" {
			worktree = path
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if worktree == "" {
		state, _ := readTaskProgress(t, console.DatabasePath)
		t.Fatalf("a typed request did not reach a running worktree; the task is %q\n%s",
			state, console.Output())
	}
	if entries, err := os.ReadDir(worktree); err != nil || len(entries) == 0 {
		t.Errorf("the worktree at %s holds no checkout: %v", worktree, err)
	}

	// The interface must agree with the machine it supervises. It used to read
	// "No task yet" against a live worker, because the session snapshot it drew
	// from was fetched before the task existed and nothing refetched it. The
	// no-task strip carries the same component name as the measured one, so the
	// state is waited on rather than the element: waiting for the element alone
	// would pass against a strip that says nothing is running.
	if err := page.Locator(
		`[data-component="task-metrics"]:not([data-state="no-task"])`).First().WaitFor(
		playwright.LocatorWaitForOptions{
			State: playwright.WaitForSelectorStateAttached, Timeout: playwright.Float(60000),
		},
	); err != nil {
		current, _ := page.Locator("body").InnerText()
		t.Fatalf("the workspace reports no task while a worker runs:\n%s", current)
	}
	body, _ := page.Locator("body").InnerText()
	if !strings.Contains(body, request) {
		t.Errorf("the workspace does not show the request that was sent")
	}
	if len(*problems) > 0 {
		t.Errorf("the page reported %d error(s):\n%s",
			len(*problems), strings.Join(*problems, "\n"))
	}
}

// readTaskProgress reads the newest task's state and worktree path.
func readTaskProgress(t *testing.T, databasePath string) (string, string) {
	t.Helper()
	database, err := sql.Open("sqlite", "file:"+databasePath+"?mode=ro")
	if err != nil {
		return "", ""
	}
	defer database.Close()
	var state string
	var worktree sql.NullString
	row := database.QueryRow(`
		SELECT tasks.state, worktree_bindings.worktree_path
		FROM tasks
		LEFT JOIN worktree_bindings ON worktree_bindings.task_id = tasks.id
		ORDER BY tasks.created_at_unix_micros DESC
		LIMIT 1`)
	if err := row.Scan(&state, &worktree); err != nil {
		return "", ""
	}
	return state, worktree.String
}

// workingConsole is a coordinator that can actually run something.
type workingConsole struct {
	URL          string
	DatabasePath string
	Output       func() string
}

// startWorkingConsole starts a coordinator with a worker beside it.
//
// The worker executable is built and placed beside the coordinator because that
// is where the coordinator looks for it — never on PATH, so what starts is
// always the binary that shipped with it.
func startWorkingConsole(t *testing.T) workingConsole {
	t.Helper()
	repository := repositoryRootForConsole(t)

	build := exec.CommandContext(t.Context(),
		"go", "run", "./cmd/codeflux-dev", "build-frontend")
	build.Dir = repository
	if output, err := build.CombinedOutput(); err != nil {
		t.Skipf("cannot build the browser assets in this environment: %v\n%s", err, output)
	}

	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	// The data directory is short on purpose. A task worktree path is this
	// directory plus a repository identifier plus a task identifier plus
	// whatever the repository nests, and Go's default temporary directory is
	// already deep enough on Windows to push that past what a checkout of this
	// repository will fit in.
	data, err := os.MkdirTemp("", "cfx")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(data) })

	executable := filepath.Join(data, "codeflux"+suffix)
	for target, output := range map[string]string{
		"./cmd/codeflux":        executable,
		"./cmd/codeflux-worker": filepath.Join(data, "codeflux-worker"+suffix),
	} {
		compile := exec.CommandContext(t.Context(), "go", "build", "-o", output, target)
		compile.Dir = repository
		if built, err := compile.CombinedOutput(); err != nil {
			t.Skipf("cannot build %s in this environment: %v\n%s", target, err, built)
		}
	}

	databasePath := filepath.Join(data, "codeflux.sqlite3")
	port := freeLoopbackPort(t)
	address := fmt.Sprintf("127.0.0.1:%d", port)
	command := exec.CommandContext(t.Context(), executable,
		"start", "--no-browser",
		"--database", databasePath,
		"--address", address,
		"--assets", filepath.Join(repository, ".artifacts", "frontend"),
	)
	output := &syncBuffer{}
	command.Stdout, command.Stderr = output, output
	if err := command.Start(); err != nil {
		t.Fatalf("start the console: %v", err)
	}
	t.Cleanup(func() {
		_ = command.Process.Kill()
		_, _ = command.Process.Wait()
	})

	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(output.String(), "codeflux is running at") {
			return workingConsole{
				URL:          "http://" + address + "/",
				DatabasePath: databasePath,
				Output:       output.String,
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("the console never announced a URL:\n%s", output.String())
	return workingConsole{}
}
