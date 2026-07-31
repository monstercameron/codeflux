package frontendtest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/png"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/mxschmitt/playwright-go"
)

var ErrBrowserUnavailable = errors.New("playwright driver or Chromium browser is unavailable")

const browserAssertionTimeout = 3 * time.Second

// Config controls a browser run. Run never installs a driver or browser.
type Config struct {
	BaseURL         string
	ArtifactDir     string
	DriverDirectory string
	ExecutablePath  string
	Timeout         time.Duration
}

// CheckResult is one stable, machine-readable browser assertion.
type CheckResult struct {
	ID       string `json:"id"`
	Route    string `json:"route,omitempty"`
	Viewport string `json:"viewport,omitempty"`
	Passed   bool   `json:"passed"`
	Detail   string `json:"detail,omitempty"`
	Artifact string `json:"artifact,omitempty"`
}

// Result is the complete browser run report.
type Result struct {
	SchemaVersion    int           `json:"schema_version"`
	Status           string        `json:"status"`
	BaseURL          string        `json:"base_url"`
	Checks           []CheckResult `json:"checks"`
	ExternalRequests []string      `json:"external_requests,omitempty"`
	BrowserErrors    []string      `json:"browser_errors,omitempty"`
}

// ValidateBaseURL enforces the local-only M16 browser boundary.
func ValidateBaseURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse frontend URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("frontend URL scheme must be http or https")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("frontend URL must not contain userinfo, query, or fragment")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return nil, fmt.Errorf("frontend URL must be an origin without a path")
	}
	if parsed.Port() == "" {
		return nil, fmt.Errorf("frontend URL must include an explicit loopback port")
	}
	hostname := strings.ToLower(parsed.Hostname())
	if hostname != "localhost" {
		address := net.ParseIP(hostname)
		if address == nil || !address.IsLoopback() {
			return nil, fmt.Errorf("frontend URL host must be loopback")
		}
	}
	parsed.Path = ""
	return parsed, nil
}

// Run executes locator-based checks against an already-running frontend.
// It does not use page script evaluation or inject browser source.
func Run(ctx context.Context, config Config) (Result, error) {
	result := Result{
		SchemaVersion: 1,
		Status:        "failed",
		BaseURL:       config.BaseURL,
	}
	base, err := ValidateBaseURL(config.BaseURL)
	if err != nil {
		return result, err
	}
	if config.Timeout <= 0 {
		config.Timeout = 10 * time.Second
	}
	if config.ArtifactDir == "" {
		return result, fmt.Errorf("artifact directory is required")
	}
	if err := os.MkdirAll(config.ArtifactDir, 0o755); err != nil {
		return result, fmt.Errorf("create browser artifact directory: %w", err)
	}
	appendLoopbackHostProtectionCheck(ctx, base, config.Timeout, &result)

	runOptions := &playwright.RunOptions{
		DriverDirectory:     config.DriverDirectory,
		SkipInstallBrowsers: true,
		Verbose:             false,
	}
	instance, err := playwright.Run(runOptions)
	if err != nil {
		result.Status = "unavailable"
		return result, fmt.Errorf("%w: %v", ErrBrowserUnavailable, err)
	}
	defer func() {
		_ = instance.Stop()
	}()

	launchOptions := playwright.BrowserTypeLaunchOptions{
		Headless:       playwright.Bool(true),
		ArtifactsDir:   playwright.String(config.ArtifactDir),
		ExecutablePath: nil,
	}
	if config.ExecutablePath != "" {
		launchOptions.ExecutablePath = playwright.String(config.ExecutablePath)
	}
	browser, err := instance.Chromium.Launch(launchOptions)
	if err != nil {
		result.Status = "unavailable"
		return result, fmt.Errorf("%w: %v", ErrBrowserUnavailable, err)
	}
	defer func() {
		_ = browser.Close()
	}()

	browserContext, err := browser.NewContext(playwright.BrowserNewContextOptions{
		BaseURL:        playwright.String(base.String()),
		ServiceWorkers: playwright.ServiceWorkerPolicyBlock,
	})
	if err != nil {
		return result, fmt.Errorf("create isolated browser context: %w", err)
	}
	defer func() {
		_ = browserContext.Close()
	}()

	diagnostics := &browserDiagnostics{}
	browserContext.OnConsole(diagnostics.onConsole)
	browserContext.OnWebError(diagnostics.onWebError)
	guard := newOriginGuard(base)
	if err := browserContext.Route("**/*", guard.route); err != nil {
		return result, fmt.Errorf("install external-network guard: %w", err)
	}
	page, err := browserContext.NewPage()
	if err != nil {
		return result, fmt.Errorf("create browser page: %w", err)
	}
	page.SetDefaultTimeout(float64(config.Timeout.Milliseconds()))
	page.SetDefaultNavigationTimeout(float64(config.Timeout.Milliseconds()))

	loadedAtLeastOnce := false
	for _, viewport := range Viewports {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if !runViewportChecks(page, base, viewport, config.ArtifactDir, &result) {
			break
		}
		loadedAtLeastOnce = true
		if !runRouteContractChecks(page, base, viewport, config.ArtifactDir, &result) {
			break
		}
	}
	if loadedAtLeastOnce {
		runAdversarialInteractionChecks(page, base, config.ArtifactDir, &result)
		runM17MountedChecks(page, base, config.ArtifactDir, &result)
	}
	result.ExternalRequests = guard.externalURLs()
	result.BrowserErrors = diagnostics.errors()
	result.Checks = append(result.Checks, CheckResult{
		ID:     "G01-no-external-network",
		Passed: loadedAtLeastOnce && len(result.ExternalRequests) == 0,
		Detail: networkGateDetail(loadedAtLeastOnce, result.ExternalRequests),
	})
	result.Checks = append(result.Checks, CheckResult{
		ID:     "browser-runtime-errors",
		Passed: len(result.BrowserErrors) == 0,
		Detail: strings.Join(result.BrowserErrors, " | "),
	})
	if allPassed(result.Checks) {
		result.Status = "passed"
	}
	return result, nil
}

func runViewportChecks(
	page playwright.Page,
	base *url.URL,
	viewport Viewport,
	artifactDir string,
	result *Result,
) bool {
	if err := page.SetViewportSize(viewport.Width, viewport.Height); err != nil {
		appendFailure(result, "viewport-size", "/", viewport.Name, err, page, artifactDir)
		return false
	}
	target := base.ResolveReference(&url.URL{Path: "/"}).String()
	response, err := page.Goto(target, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
	})
	if err != nil {
		appendFailure(result, "route-load", "/", viewport.Name, err, page, artifactDir)
		return false
	}
	appendResponseSecurityChecks(result, response, base, viewport)

	root := page.GetByTestId("app-root")
	if err := root.WaitFor(playwright.LocatorWaitForOptions{
		State:   playwright.WaitForSelectorStateVisible,
		Timeout: playwright.Float(15_000),
	}); err != nil {
		appendFailure(
			result,
			"app-root-ready",
			"/",
			viewport.Name,
			err,
			page,
			artifactDir,
		)
		return false
	}
	appendLocatorCount(result, "app-root-singleton", "/", viewport.Name, root, 1)
	appendVisible(result, "app-root-visible", "/", viewport.Name, root)
	appendLocatorCount(
		result,
		"generated-starter-overlay-absent",
		"/",
		viewport.Name,
		page.GetByRole(*playwright.AriaRoleHeading, playwright.PageGetByRoleOptions{
			Name:  "CodefluxSpike",
			Exact: playwright.Bool(true),
		}),
		0,
	)

	mode, err := root.GetAttribute("data-responsive-mode")
	appendResult(result, CheckResult{
		ID:       "responsive-mode",
		Route:    "/",
		Viewport: viewport.Name,
		Passed:   err == nil && mode == viewport.Mode,
		Detail:   attributeDetail("data-responsive-mode", viewport.Mode, mode, err),
	})
	overflow, err := root.GetAttribute("data-horizontal-overflow")
	appendResult(result, CheckResult{
		ID:       "no-horizontal-overflow",
		Route:    "/",
		Viewport: viewport.Name,
		Passed:   err == nil && overflow == "false",
		Detail:   attributeDetail("data-horizontal-overflow", "false", overflow, err),
	})
	box, err := root.BoundingBox()
	boxOK := err == nil && box != nil && box.X >= 0 && box.X+box.Width <= float64(viewport.Width)+0.5
	appendResult(result, CheckResult{
		ID:       "root-within-viewport",
		Route:    "/",
		Viewport: viewport.Name,
		Passed:   boxOK,
		Detail:   boundingBoxDetail(viewport, box, err),
	})
	appendDocumentAttributeChecks(result, root, viewport)
	appendThemeCycleCheck(result, page, root, viewport)
	appendVisibleDescendantBoundsCheck(result, root, viewport, "/")
	appendFullPageScrollWidthCheck(result, page, viewport, "/")

	level := 1
	heading := page.GetByRole(*playwright.AriaRoleHeading, playwright.PageGetByRoleOptions{
		Level: &level,
	})
	appendLocatorCount(result, "single-level-one-heading", "/", viewport.Name, heading, 1)
	appendLocatorCount(result, "main-landmark", "/", viewport.Name, page.GetByRole(*playwright.AriaRoleMain), 1)
	appendLocatorCount(result, "banner-landmark", "/", viewport.Name, page.GetByRole(*playwright.AriaRoleBanner), 1)

	snapshot, snapshotErr := root.AriaSnapshot()
	appendResult(result, CheckResult{
		ID:       "aria-snapshot",
		Route:    "/",
		Viewport: viewport.Name,
		Passed:   snapshotErr == nil && strings.TrimSpace(snapshot) != "",
		Detail:   errorDetail(snapshotErr),
	})

	assertNamedRole(result, page, viewport, playwright.AriaRoleButton, "named-buttons")
	assertNamedRole(result, page, viewport, playwright.AriaRoleLink, "named-links")
	assertNamedRole(result, page, viewport, playwright.AriaRoleTextbox, "named-textboxes")

	skipLink := page.GetByTestId("skip-link")
	if appendLocatorCount(result, "skip-link-singleton", "/", viewport.Name, skipLink, 1) {
		hiddenBeforeFocus, hiddenErr := skipLink.IsHidden()
		appendResult(result, CheckResult{
			ID:       "skip-link-hidden-until-focus",
			Route:    "/",
			Viewport: viewport.Name,
			Passed:   hiddenErr == nil && hiddenBeforeFocus,
			Detail: fmt.Sprintf(
				"hidden_before_focus=%t error=%v",
				hiddenBeforeFocus,
				hiddenErr,
			),
		})
		focusErr := skipLink.Focus()
		if focusErr == nil {
			focusErr = browserAssertions().
				Locator(skipLink).
				ToBeFocused()
		}
		appendResult(result, CheckResult{
			ID:       "skip-link-focus-visible",
			Route:    "/",
			Viewport: viewport.Name,
			Passed:   focusErr == nil && visible(skipLink),
			Detail:   errorDetail(focusErr),
		})
		activationErr := skipLink.Press("Enter")
		if activationErr == nil {
			activationErr = browserAssertions().
				Locator(page.GetByRole(*playwright.AriaRoleMain)).
				ToBeFocused()
		}
		appendResult(result, CheckResult{
			ID:       "skip-link-activation-focuses-main",
			Route:    "/",
			Viewport: viewport.Name,
			Passed:   activationErr == nil,
			Detail:   errorDetail(activationErr),
		})
	}

	if !allPassedForViewport(result.Checks, viewport.Name) {
		path := filepath.Join(artifactDir, "failure-"+viewport.Name+".png")
		if _, err := page.Screenshot(playwright.PageScreenshotOptions{
			Path:     playwright.String(path),
			FullPage: playwright.Bool(true),
		}); err == nil {
			for index := range result.Checks {
				check := &result.Checks[index]
				if check.Viewport == viewport.Name && !check.Passed && check.Artifact == "" {
					check.Artifact = path
				}
			}
		}
	}
	return true
}

func appendResponseSecurityChecks(
	result *Result,
	response playwright.Response,
	base *url.URL,
	viewport Viewport,
) {
	required := []struct {
		header string
		want   string
	}{
		{header: "content-security-policy", want: "default-src 'self'"},
		{header: "cross-origin-opener-policy", want: "same-origin"},
		{header: "referrer-policy", want: "no-referrer"},
		{header: "x-content-type-options", want: "nosniff"},
	}
	for _, requirement := range required {
		value, err := response.HeaderValue(requirement.header)
		appendResult(result, CheckResult{
			ID:       "security-header-" + requirement.header,
			Route:    "/",
			Viewport: viewport.Name,
			Passed:   err == nil && strings.Contains(value, requirement.want),
			Detail: fmt.Sprintf(
				"%s=%q required=%q error=%v",
				requirement.header,
				value,
				requirement.want,
				err,
			),
		})
	}
	csp, err := response.HeaderValue("content-security-policy")
	broadWebSockets := hasBroadWebSocketSource(csp)
	exactWebSocket, websocketSources := hasExactWebSocketSource(csp, base)
	appendResult(result, CheckResult{
		ID:       "csp-connect-src-exact-origin",
		Route:    "/",
		Viewport: viewport.Name,
		Passed:   err == nil && !broadWebSockets && exactWebSocket,
		Detail: fmt.Sprintf(
			"csp=%q websocket_sources=%q broad_websocket_scheme=%t exact_launch_origin=%t error=%v",
			csp,
			websocketSources,
			broadWebSockets,
			exactWebSocket,
			err,
		),
	})
}

func hasBroadWebSocketSource(csp string) bool {
	for _, directive := range strings.Split(csp, ";") {
		fields := strings.Fields(directive)
		if len(fields) == 0 || fields[0] != "connect-src" {
			continue
		}
		for _, source := range fields[1:] {
			if source == "ws:" || source == "wss:" {
				return true
			}
		}
	}
	return false
}

func hasExactWebSocketSource(csp string, base *url.URL) (bool, []string) {
	expectedScheme := "ws"
	if strings.EqualFold(base.Scheme, "https") {
		expectedScheme = "wss"
	}
	expected := expectedScheme + "://" + strings.ToLower(base.Host)
	var websocketSources []string
	for _, directive := range strings.Split(csp, ";") {
		fields := strings.Fields(directive)
		if len(fields) == 0 || fields[0] != "connect-src" {
			continue
		}
		for _, source := range fields[1:] {
			lower := strings.ToLower(source)
			if lower == "ws:" || lower == "wss:" ||
				strings.HasPrefix(lower, "ws://") || strings.HasPrefix(lower, "wss://") {
				websocketSources = append(websocketSources, lower)
			}
		}
	}
	return len(websocketSources) == 1 && websocketSources[0] == expected, websocketSources
}

func appendLoopbackHostProtectionCheck(
	ctx context.Context,
	base *url.URL,
	timeout time.Duration,
	result *Result,
) {
	target := base.ResolveReference(&url.URL{Path: "/bootstrap"}).String()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		appendResult(result, CheckResult{
			ID: "loopback-host-rejects-forged-origin", Detail: err.Error(),
		})
		return
	}
	request.Host = "attacker.invalid"
	request.Header.Set("Origin", "http://attacker.invalid")
	client := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return errors.New("redirect refused for forged-host probe")
		},
	}
	response, requestErr := client.Do(request)
	if response != nil {
		defer response.Body.Close()
	}
	status := 0
	if response != nil {
		status = response.StatusCode
	}
	appendResult(result, CheckResult{
		ID:     "loopback-host-rejects-forged-origin",
		Passed: requestErr == nil && status >= http.StatusBadRequest && status < http.StatusInternalServerError,
		Detail: fmt.Sprintf("forged_host=%q forged_origin=%q status=%d error=%v", request.Host, request.Header.Get("Origin"), status, requestErr),
	})
}

func appendDocumentAttributeChecks(result *Result, root playwright.Locator, viewport Viewport) {
	requirements := []struct {
		id      string
		name    string
		allowed []string
	}{
		{id: "theme-attribute", name: "data-theme", allowed: []string{"dark", "light", "high-contrast"}},
		{id: "density-attribute", name: "data-density", allowed: []string{"comfortable", "compact"}},
		{id: "locale-attribute", name: "data-locale", allowed: []string{"en-US"}},
		{id: "language-attribute", name: "lang", allowed: []string{"en-US"}},
		{id: "direction-attribute", name: "dir", allowed: []string{"ltr"}},
	}
	for _, requirement := range requirements {
		value, err := root.GetAttribute(requirement.name)
		passed := err == nil
		if passed {
			passed = false
			for _, allowed := range requirement.allowed {
				if value == allowed {
					passed = true
					break
				}
			}
		}
		appendResult(result, CheckResult{
			ID:       requirement.id,
			Route:    "/",
			Viewport: viewport.Name,
			Passed:   passed,
			Detail:   fmt.Sprintf("%s=%q allowed=%q error=%v", requirement.name, value, requirement.allowed, err),
		})
	}
}

func appendThemeCycleCheck(
	result *Result,
	page playwright.Page,
	root playwright.Locator,
	viewport Viewport,
) {
	button := page.GetByRole(*playwright.AriaRoleButton, playwright.PageGetByRoleOptions{
		Name:  "Change color theme",
		Exact: playwright.Bool(true),
	})
	initial, err := root.GetAttribute("data-theme")
	visited := []string{initial}
	for step := 0; err == nil && step < 3; step++ {
		next, ok := nextTheme(visited[len(visited)-1])
		if !ok {
			err = fmt.Errorf("unsupported initial theme %q", initial)
			break
		}
		if clickErr := button.Click(); clickErr != nil {
			err = clickErr
			break
		}
		if assertionErr := browserAssertions().Locator(root).ToHaveAttribute("data-theme", next); assertionErr != nil {
			err = assertionErr
			break
		}
		visited = append(visited, next)
	}
	appendResult(result, CheckResult{
		ID:       "theme-cycle",
		Route:    "/",
		Viewport: viewport.Name,
		Passed:   err == nil && len(visited) == 4 && visited[len(visited)-1] == initial,
		Detail:   fmt.Sprintf("visited=%q error=%v", visited, err),
	})
}

func nextTheme(current string) (string, bool) {
	switch current {
	case "dark":
		return "light", true
	case "light":
		return "high-contrast", true
	case "high-contrast":
		return "dark", true
	default:
		return "", false
	}
}

func appendVisibleDescendantBoundsCheck(
	result *Result,
	root playwright.Locator,
	viewport Viewport,
	route string,
) {
	descendants, err := root.Locator("*").All()
	if err != nil {
		appendResult(result, CheckResult{
			ID: "visible-descendant-bounds", Route: route, Viewport: viewport.Name, Detail: err.Error(),
		})
		return
	}
	const tolerance = 1.0
	var outside []string
	for index, descendant := range descendants {
		isVisible, visibilityErr := descendant.IsVisible()
		if visibilityErr != nil {
			outside = append(outside, fmt.Sprintf("index=%d visibility_error=%v", index, visibilityErr))
			continue
		}
		if !isVisible {
			continue
		}
		box, boxErr := descendant.BoundingBox()
		if boxErr != nil {
			outside = append(outside, fmt.Sprintf("index=%d box_error=%v", index, boxErr))
			continue
		}
		if box == nil {
			continue
		}
		if box.X < -tolerance || box.X+box.Width > float64(viewport.Width)+tolerance {
			outside = append(outside, fmt.Sprintf("index=%d x=%.2f width=%.2f", index, box.X, box.Width))
			if len(outside) == 8 {
				break
			}
		}
	}
	appendResult(result, CheckResult{
		ID:       "visible-descendant-bounds",
		Route:    route,
		Viewport: viewport.Name,
		Passed:   len(outside) == 0,
		Detail: fmt.Sprintf(
			"checked=%d viewport_width=%d outside=%q (locator bounding boxes; no page script evaluation)",
			len(descendants),
			viewport.Width,
			outside,
		),
	})
}

func appendFullPageScrollWidthCheck(
	result *Result,
	page playwright.Page,
	viewport Viewport,
	route string,
) {
	imageBytes, screenshotErr := page.Screenshot(playwright.PageScreenshotOptions{
		FullPage: playwright.Bool(true),
	})
	imageConfig, _, decodeErr := image.DecodeConfig(bytes.NewReader(imageBytes))
	passed := screenshotErr == nil && decodeErr == nil && imageConfig.Width <= viewport.Width+1
	appendResult(result, CheckResult{
		ID:       "full-page-scroll-width",
		Route:    route,
		Viewport: viewport.Name,
		Passed:   passed,
		Detail: fmt.Sprintf(
			"full_page_bitmap_width=%d viewport_width=%d screenshot_error=%v decode_error=%v (no page script evaluation)",
			imageConfig.Width,
			viewport.Width,
			screenshotErr,
			decodeErr,
		),
	})
}

func runRouteContractChecks(
	page playwright.Page,
	base *url.URL,
	viewport Viewport,
	artifactDir string,
	result *Result,
) bool {
	for routeIndex, route := range Routes {
		target := base.ResolveReference(&url.URL{Path: route}).String()
		if _, err := page.Goto(target, playwright.PageGotoOptions{
			WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		}); err != nil {
			appendFailure(result, "direct-route-load", route, viewport.Name, err, page, artifactDir)
			return false
		}
		root := page.GetByTestId("app-root")
		if !appendLocatorCount(result, "direct-route-app-root", route, viewport.Name, root, 1) {
			captureLastFailure(
				page,
				result,
				artifactDir,
				fmt.Sprintf("failure-%s-route-%d.png", viewport.Name, routeIndex),
			)
			continue
		}
		level := 1
		heading := page.GetByRole(*playwright.AriaRoleHeading, playwright.PageGetByRoleOptions{
			Level: &level,
			Name:  RouteHeadings[route],
			Exact: playwright.Bool(true),
		})
		if !appendLocatorCount(result, "direct-route-heading", route, viewport.Name, heading, 1) {
			captureLastFailure(
				page,
				result,
				artifactDir,
				fmt.Sprintf("failure-%s-route-%d-heading.png", viewport.Name, routeIndex),
			)
		}
		appendVisibleDescendantBoundsCheck(result, root, viewport, route)
		appendFullPageScrollWidthCheck(result, page, viewport, route)
		if route == "/tasks" || strings.Contains(route, "/thread/") {
			runThreadRouteChecks(page, route, viewport, result)
		}
	}
	return true
}

func runThreadRouteChecks(
	page playwright.Page,
	route string,
	viewport Viewport,
	result *Result,
) {
	composer := page.GetByRole(*playwright.AriaRoleTextbox, playwright.PageGetByRoleOptions{
		Name:  "Message",
		Exact: playwright.Bool(true),
	})
	appendAtLeastOneVisible(
		result,
		"thread-composer",
		route,
		viewport.Name,
		composer,
	)
	composerBox, composerErr := composer.BoundingBox()
	composerInViewport := composerErr == nil && composerBox != nil &&
		composerBox.Y >= -1 && composerBox.Y+composerBox.Height <= float64(viewport.Height)+1
	appendResult(result, CheckResult{
		ID:       "thread-composer-within-viewport",
		Route:    route,
		Viewport: viewport.Name,
		Passed:   composerInViewport,
		Detail: fmt.Sprintf(
			"viewport_height=%d box=%v error=%v",
			viewport.Height,
			composerBox,
			composerErr,
		),
	})
	conversation := page.GetByRole(*playwright.AriaRoleRegion, playwright.PageGetByRoleOptions{
		Name:  "Conversation",
		Exact: playwright.Bool(true),
	})
	appendVisible(result, "conversation-pane-visible", route, viewport.Name, conversation)
	threadRail := page.GetByRole(*playwright.AriaRoleNavigation, playwright.PageGetByRoleOptions{
		Name:  "Threads",
		Exact: playwright.Bool(true),
	})
	graph := page.GetByRole(*playwright.AriaRoleComplementary, playwright.PageGetByRoleOptions{
		Name:  "Task graph",
		Exact: playwright.Bool(true),
	})
	switch viewport.Mode {
	case "wide":
		appendVisible(result, "wide-thread-rail-visible", route, viewport.Name, threadRail)
		appendVisible(result, "wide-graph-visible", route, viewport.Name, graph)
	case "standard":
		appendHidden(result, "standard-thread-rail-overlay-closed", route, viewport.Name, threadRail)
		appendVisible(result, "standard-graph-visible", route, viewport.Name, graph)
	case "compact", "minimum":
		appendAtLeastOneVisible(
			result,
			"narrow-pane-tabs",
			route,
			viewport.Name,
			page.GetByRole(*playwright.AriaRoleTablist),
		)
		appendHidden(result, "narrow-thread-drawer-closed", route, viewport.Name, threadRail)
		appendHidden(result, "narrow-inactive-graph-hidden", route, viewport.Name, graph)
	}
}

func assertNamedRole(
	result *Result,
	page playwright.Page,
	viewport Viewport,
	role *playwright.AriaRole,
	id string,
) {
	all := page.GetByRole(*role)
	named := page.GetByRole(*role, playwright.PageGetByRoleOptions{
		Name: regexp.MustCompile(`\S`),
	})
	allCount, allErr := all.Count()
	namedCount, namedErr := named.Count()
	appendResult(result, CheckResult{
		ID:       id,
		Route:    "/",
		Viewport: viewport.Name,
		Passed:   allErr == nil && namedErr == nil && allCount == namedCount,
		Detail: fmt.Sprintf(
			"all=%d named=%d all_error=%v named_error=%v",
			allCount,
			namedCount,
			allErr,
			namedErr,
		),
	})
}

func appendLocatorCount(
	result *Result,
	id string,
	route string,
	viewport string,
	locator playwright.Locator,
	want int,
) bool {
	assertionErr := browserAssertions().Locator(locator).ToHaveCount(want)
	count, err := locator.Count()
	passed := assertionErr == nil && err == nil && count == want
	appendResult(result, CheckResult{
		ID:       id,
		Route:    route,
		Viewport: viewport,
		Passed:   passed,
		Detail: fmt.Sprintf(
			"count=%d want=%d assertion_error=%v count_error=%v",
			count,
			want,
			assertionErr,
			err,
		),
	})
	return passed
}

func appendVisible(
	result *Result,
	id string,
	route string,
	viewport string,
	locator playwright.Locator,
) {
	assertionErr := browserAssertions().Locator(locator).ToBeVisible()
	isVisible, err := locator.IsVisible()
	appendResult(result, CheckResult{
		ID:       id,
		Route:    route,
		Viewport: viewport,
		Passed:   assertionErr == nil && err == nil && isVisible,
		Detail: fmt.Sprintf(
			"visible=%t assertion_error=%v visibility_error=%v",
			isVisible,
			assertionErr,
			err,
		),
	})
}

func appendHidden(
	result *Result,
	id string,
	route string,
	viewport string,
	locator playwright.Locator,
) {
	assertionErr := browserAssertions().Locator(locator).ToBeHidden()
	isHidden, err := locator.IsHidden()
	appendResult(result, CheckResult{
		ID:       id,
		Route:    route,
		Viewport: viewport,
		Passed:   assertionErr == nil && err == nil && isHidden,
		Detail: fmt.Sprintf(
			"hidden=%t assertion_error=%v visibility_error=%v",
			isHidden,
			assertionErr,
			err,
		),
	})
}

func appendAtLeastOneVisible(
	result *Result,
	id string,
	route string,
	viewport string,
	locator playwright.Locator,
) {
	assertionErr := browserAssertions().Locator(locator.First()).ToBeVisible()
	count, err := locator.Count()
	appendResult(result, CheckResult{
		ID:       id,
		Route:    route,
		Viewport: viewport,
		Passed:   assertionErr == nil && err == nil && count >= 1,
		Detail: fmt.Sprintf(
			"count=%d assertion_error=%v count_error=%v",
			count,
			assertionErr,
			err,
		),
	})
}

func browserAssertions() playwright.PlaywrightAssertions {
	return playwright.NewPlaywrightAssertions(
		float64(browserAssertionTimeout / time.Millisecond),
	)
}

func appendFailure(
	result *Result,
	id string,
	route string,
	viewport string,
	err error,
	page playwright.Page,
	artifactDir string,
) {
	check := CheckResult{
		ID:       id,
		Route:    route,
		Viewport: viewport,
		Detail:   err.Error(),
	}
	path := filepath.Join(artifactDir, "failure-"+viewport+".png")
	if _, screenshotErr := page.Screenshot(playwright.PageScreenshotOptions{
		Path: playwright.String(path),
	}); screenshotErr == nil {
		check.Artifact = path
	}
	appendResult(result, check)
}

func captureLastFailure(
	page playwright.Page,
	result *Result,
	artifactDir string,
	filename string,
) {
	if len(result.Checks) == 0 || result.Checks[len(result.Checks)-1].Passed {
		return
	}
	path := filepath.Join(artifactDir, filename)
	if _, err := page.Screenshot(playwright.PageScreenshotOptions{
		Path:     playwright.String(path),
		FullPage: playwright.Bool(true),
	}); err == nil {
		result.Checks[len(result.Checks)-1].Artifact = path
	}
}

func appendResult(result *Result, check CheckResult) {
	result.Checks = append(result.Checks, check)
}

func visible(locator playwright.Locator) bool {
	value, err := locator.IsVisible()
	return err == nil && value
}

func allPassed(checks []CheckResult) bool {
	for _, check := range checks {
		if !check.Passed {
			return false
		}
	}
	return len(checks) > 0
}

func allPassedForViewport(checks []CheckResult, viewport string) bool {
	for _, check := range checks {
		if check.Viewport == viewport && !check.Passed {
			return false
		}
	}
	return true
}

func attributeDetail(name, want, got string, err error) string {
	return fmt.Sprintf("%s=%q want=%q error=%v", name, got, want, err)
}

func boundingBoxDetail(viewport Viewport, box *playwright.Rect, err error) string {
	if box == nil {
		return fmt.Sprintf("box=nil viewport_width=%d error=%v", viewport.Width, err)
	}
	return fmt.Sprintf(
		"x=%.2f width=%.2f viewport_width=%d error=%v",
		box.X,
		box.Width,
		viewport.Width,
		err,
	)
}

func errorDetail(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func externalRequestDetail(urls []string) string {
	if len(urls) == 0 {
		return ""
	}
	return "blocked attempted external requests: " + strings.Join(urls, ", ")
}

func networkGateDetail(loaded bool, urls []string) string {
	if !loaded {
		return "not evaluated because the frontend never completed an application load"
	}
	return externalRequestDetail(urls)
}

type originGuard struct {
	origin   string
	mu       sync.Mutex
	external []string
}

type browserDiagnostics struct {
	mu       sync.Mutex
	messages []string
}

func (diagnostics *browserDiagnostics) onConsole(message playwright.ConsoleMessage) {
	if message.Type() != "error" {
		return
	}
	diagnostics.append("console: " + message.Text())
}

func (diagnostics *browserDiagnostics) onWebError(webError playwright.WebError) {
	diagnostics.append("page: " + webError.Error().Error())
}

func (diagnostics *browserDiagnostics) append(message string) {
	diagnostics.mu.Lock()
	defer diagnostics.mu.Unlock()
	diagnostics.messages = append(diagnostics.messages, message)
}

func (diagnostics *browserDiagnostics) errors() []string {
	diagnostics.mu.Lock()
	defer diagnostics.mu.Unlock()
	return append([]string(nil), diagnostics.messages...)
}

func newOriginGuard(base *url.URL) *originGuard {
	return &originGuard{origin: canonicalOrigin(base)}
}

func (guard *originGuard) route(route playwright.Route) {
	requestURL := route.Request().URL()
	if guard.allowed(requestURL) {
		_ = route.Continue()
		return
	}
	guard.mu.Lock()
	guard.external = append(guard.external, requestURL)
	guard.mu.Unlock()
	_ = route.Abort("blockedbyclient")
}

func (guard *originGuard) allowed(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "about", "data":
		return true
	case "blob":
		inner, innerErr := url.Parse(strings.TrimPrefix(raw, "blob:"))
		return innerErr == nil && canonicalOrigin(inner) == guard.origin
	case "http", "https", "ws", "wss":
		return canonicalOrigin(parsed) == guard.origin
	default:
		return false
	}
}

func canonicalOrigin(parsed *url.URL) string {
	scheme := strings.ToLower(parsed.Scheme)
	if scheme == "ws" {
		scheme = "http"
	}
	if scheme == "wss" {
		scheme = "https"
	}
	return scheme + "://" + strings.ToLower(parsed.Host)
}

func (guard *originGuard) externalURLs() []string {
	guard.mu.Lock()
	defer guard.mu.Unlock()
	return append([]string(nil), guard.external...)
}
