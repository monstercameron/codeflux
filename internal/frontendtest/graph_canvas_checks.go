package frontendtest

import (
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mxschmitt/playwright-go"
)

type graphViewportState struct {
	Zoom float64
	PanX float64
	PanY float64
}

const graphTransformTolerance = 0.001

// The mounted graph checks assert against the exact content the REPO-041
// seeded thread produces: `codeflux-dev seed success` in serve mode (see
// internal/testharness.StartMountedThread) drives one real requirement --
// "create cmd/report/main.go, add its companion test, run the project's
// tests" -- through the real coordinator, over real SQLite, to
// awaiting-review. These strings were read back from that real run's
// database (internal/coordinator/agent_graph.go's shortGraphLabel and
// web/frontend/graphcanvas/component.go's "<title> - <StatusLabel>" aria
// format), not invented: the commit that follows REPO-039's precedent here
// is REPO-044, which is what made a real run reach six distinct node
// classes in the first place. The old constants ("requirements",
// "responsive", "implementation", "Shell requirements - Complete") named a
// ten-node demo fixture that commit 964e09d deleted at the user's explicit
// request to stop using seeded UI data; reseeding that content would have
// resurrected exactly the anti-pattern the product removed.
//
// Two nodes this run produces share a label with another node of a
// different class -- the atom-operation and artifact-result nodes for each
// written file both read "<path> - Passed", and the effect and obligation
// nodes for the test run both read "go test ./... - Passed" -- because both
// halves of each pair genuinely share the same display name and status.
// That is real and not a defect (see REPO-041's TODOS.md entry), but it
// means only the nodes below are safe to address by accessible name: it is
// exactly one requirement, one plan region, and one plan step, and each is
// unique among the eleven.
const (
	// seededRequirementNodeLabel is the requirement node: always passed,
	// because a requirement is projected once and never re-stated.
	seededRequirementNodeLabel = "Create cmd/report/main.go so the program prints a report line. - Passed"
	// seededPlanRegionNodeLabel is "The plan" region node. It is the run's
	// only node left in the active status projector.go ever assigns a plan
	// region, which is also what makes it the graph's CurrentID fallback
	// (web/frontend/shell/reference_chrome.go's currentGraphNodeID) and
	// therefore the node selected by default when the canvas first mounts.
	seededPlanRegionNodeLabel = "The plan - Active"
	// seededVerifyStepNodeLabel is the plan step that runs the project's own
	// tests, still pending because nothing in this graph vocabulary marks a
	// plan step implemented or validated -- only the operation, effect, and
	// obligation nodes it controls carry a terminal status.
	seededVerifyStepNodeLabel = "Run the tests: executable go, arg1 test, arg2 ./... - Pending"
)

func runMountedGraphCanvasContractCheck(
	page playwright.Page,
	base *url.URL,
	artifactDir string,
	result *Result,
) {
	// The canvas no longer inlines on /tasks at wide viewport (REPO-041a); it
	// lives on the dedicated /graphs route. See loadInteractionGraphRoute.
	route, _, err := loadInteractionGraphRoute(page, base, 1600, 1000, "wide")
	canvas := page.Locator(
		`[data-component="graph-canvas"][data-renderer="canvas-2d"][data-high-dpi="true"]`,
	)
	surface := canvas.Locator(`canvas[data-component="graph-render-surface"]`)
	nodes := canvas.Locator(`button[data-component="graph-node"][data-node-id]`)
	canvasCount, canvasCountErr := canvas.Count()
	surfaceCount, surfaceCountErr := surface.Count()
	nodeCount, nodeCountErr := nodes.Count()
	canvasVisible, canvasVisibleErr := canvas.IsVisible()
	surfaceVisible, surfaceVisibleErr := surface.IsVisible()
	viewport, viewportErr := readGraphViewportState(canvas)
	dpr, dprErr := numericAttribute(canvas, "data-device-pixel-ratio")
	backingWidth, backingWidthErr := numericAttribute(canvas, "data-backing-width")
	backingHeight, backingHeightErr := numericAttribute(canvas, "data-backing-height")
	box, boxErr := surface.BoundingBox()
	backingMatches := boxErr == nil && box != nil && dprErr == nil && dpr >= 1 &&
		backingWidthErr == nil && backingHeightErr == nil &&
		backingWidth+1 >= box.Width*dpr && backingHeight+1 >= box.Height*dpr
	selectedCount := int64(0)
	accessibleNodes := true
	shapes := make(map[string]struct{})
	if nodeCountErr == nil {
		for index := 0; index < int(nodeCount); index++ {
			node := nodes.Nth(index)
			label, labelErr := node.GetAttribute("aria-label")
			description, descriptionErr := node.GetAttribute("aria-description")
			shape, shapeErr := node.GetAttribute("data-shape")
			pressed, pressedErr := node.GetAttribute("aria-pressed")
			if labelErr != nil || strings.TrimSpace(label) == "" || pressedErr != nil ||
				descriptionErr != nil || strings.TrimSpace(description) == "" ||
				shapeErr != nil || strings.TrimSpace(shape) == "" ||
				(pressed != "true" && pressed != "false") {
				accessibleNodes = false
				break
			}
			shapes[shape] = struct{}{}
			if pressed == "true" {
				selectedCount++
			}
		}
	}
	appendInteractionResult(page, artifactDir, result, CheckResult{
		ID: "graph-canvas-mounted-high-dpi-accessible-overlay", Route: route, Viewport: "wide",
		Passed: err == nil && canvasCountErr == nil && canvasCount == 1 &&
			surfaceCountErr == nil && surfaceCount == 1 && canvasVisibleErr == nil && canvasVisible &&
			// The seeded requirement (REPO-041, REPO-044) produces exactly
			// eleven nodes across all six wired classes: one requirement, one
			// plan region plus its three steps, two file-edit operations, one
			// command effect, one validation obligation, and two stored
			// artifacts. REPO-044's own investigation names the ceiling: two
			// classes (ApprovalBoundary, RecoveryObserved) have no production
			// caller, so six is the most any real run can show today, and
			// this assertion is pinned to that ceiling rather than a looser
			// "at least" bound the product has already exceeded.
			surfaceVisibleErr == nil && surfaceVisible && nodeCountErr == nil && nodeCount == 11 &&
			accessibleNodes && len(shapes) == 6 && selectedCount == 1 &&
			viewportErr == nil && viewport.Zoom > 0 &&
			backingMatches,
		Detail: fmt.Sprintf(
			"canvas=%d surface=%d nodes=%d selected=%d purpose_shapes=%d visible=%t/%t dpr=%.3f backing=%.0fx%.0f css_box=%v viewport=%+v accessible=%t load_error=%v canvas_error=%v surface_error=%v node_error=%v visibility_errors=%v/%v viewport_error=%v dpr_error=%v backing_errors=%v/%v box_error=%v",
			canvasCount, surfaceCount, nodeCount, selectedCount, len(shapes), canvasVisible, surfaceVisible,
			dpr, backingWidth, backingHeight, box, viewport, accessibleNodes, err,
			canvasCountErr, surfaceCountErr, nodeCountErr, canvasVisibleErr, surfaceVisibleErr,
			viewportErr, dprErr, backingWidthErr, backingHeightErr, boxErr,
		),
	})
}

func runGraphCanvasInteractionCheck(
	page playwright.Page,
	base *url.URL,
	artifactDir string,
	result *Result,
) {
	// The canvas no longer inlines on /tasks at wide viewport (REPO-041a); it
	// lives on the dedicated /graphs route. See loadInteractionGraphRoute.
	route, _, err := loadInteractionGraphRoute(page, base, 1600, 1000, "wide")
	canvas := page.Locator(`[data-component="graph-canvas"]`)
	surface := canvas.Locator(`canvas[data-component="graph-render-surface"]`)
	zoomIn := page.GetByRole(*playwright.AriaRoleButton, playwright.PageGetByRoleOptions{
		Name: "Zoom in", Exact: playwright.Bool(true),
	})
	zoomOut := page.GetByRole(*playwright.AriaRoleButton, playwright.PageGetByRoleOptions{
		Name: "Zoom out", Exact: playwright.Bool(true),
	})
	fit := page.GetByRole(*playwright.AriaRoleButton, playwright.PageGetByRoleOptions{
		Name: "Fit graph", Exact: playwright.Bool(true),
	})
	reset := page.GetByRole(*playwright.AriaRoleButton, playwright.PageGetByRoleOptions{
		Name: "Reset graph view", Exact: playwright.Bool(true),
	})
	initial := graphViewportState{}
	if err == nil {
		initial, err = readGraphViewportState(canvas)
	}
	zoomed := graphViewportState{}
	if err == nil {
		err = zoomIn.Click()
	}
	if err == nil {
		err = waitForNumericAttributeNear(
			canvas, "data-zoom", initial.Zoom*1.25, graphTransformTolerance,
		)
	}
	if err == nil {
		zoomed, err = readGraphViewportState(canvas)
	}
	if err == nil && zoomed.Zoom <= initial.Zoom {
		err = fmt.Errorf("zoom in did not increase zoom: initial=%v zoomed=%v", initial, zoomed)
	}
	if err == nil {
		err = zoomOut.Click()
	}
	if err == nil {
		err = waitForNumericAttributeNear(
			canvas, "data-zoom", initial.Zoom, graphTransformTolerance,
		)
	}
	box, boxErr := surface.BoundingBox()
	if err == nil && boxErr != nil {
		err = boxErr
	}
	panned := graphViewportState{}
	if err == nil && box == nil {
		err = fmt.Errorf("graph render surface has no bounding box")
	}
	if err == nil {
		startX := box.X + box.Width/2
		startY := box.Y + box.Height/2
		err = page.Mouse().Move(startX, startY)
		if err == nil {
			err = page.Mouse().Down()
		}
		if err == nil {
			err = page.Mouse().Move(startX+48, startY+32, playwright.MouseMoveOptions{
				Steps: playwright.Int(4),
			})
		}
		upErr := page.Mouse().Up()
		if err == nil {
			err = upErr
		}
	}
	if err == nil {
		panned, err = readGraphViewportState(canvas)
	}
	if err == nil && panned.PanX == initial.PanX && panned.PanY == initial.PanY {
		err = fmt.Errorf("pointer pan did not change viewport: initial=%v panned=%v", initial, panned)
	}
	fitted := graphViewportState{}
	if err == nil {
		err = fit.Click()
	}
	if err == nil {
		// Fit is allowed to retain the existing zoom when the graph was already
		// fitted before a pointer pan. Its contract is to restore the fitted
		// viewport, so wait on the pan coordinate changed by the gesture.
		err = waitForNumericAttributeChange(
			canvas, "data-pan-x", panned.PanX, graphTransformTolerance,
		)
	}
	if err == nil {
		fitted, err = readGraphViewportState(canvas)
	}
	if err == nil && fitted.Zoom <= 0 {
		err = fmt.Errorf("fit produced invalid viewport: %v", fitted)
	}
	resetState := graphViewportState{}
	if err == nil {
		err = reset.Click()
	}
	if err == nil {
		err = waitForNumericAttributeNear(canvas, "data-pan-x", 0, graphTransformTolerance)
	}
	if err == nil {
		err = waitForNumericAttributeNear(canvas, "data-pan-y", 0, graphTransformTolerance)
	}
	if err == nil {
		resetState, err = readGraphViewportState(canvas)
	}
	if err == nil && (math.Abs(resetState.PanX) > graphTransformTolerance ||
		math.Abs(resetState.PanY) > graphTransformTolerance) {
		err = fmt.Errorf("reset did not clear pan: %v", resetState)
	}
	// Node identity (nod_...) is a fresh ULID every time the thread is
	// reseeded, so unlike the accessible names above it cannot be a
	// compile-time constant. It is read back from the same button
	// GetByRole/Name resolves, right after the interaction that selects it,
	// and every data-selected-node-id assertion below compares against that
	// captured value rather than a literal.
	inspected := page.GetByRole(*playwright.AriaRoleButton, playwright.PageGetByRoleOptions{
		Name: seededRequirementNodeLabel, Exact: playwright.Bool(true),
	})
	keyboardSelected := page.GetByRole(*playwright.AriaRoleButton, playwright.PageGetByRoleOptions{
		Name: seededVerifyStepNodeLabel, Exact: playwright.Bool(true),
	})
	// The plan region is the run's only active-status node, which makes it
	// web/frontend/shell/reference_chrome.go's currentGraphNodeID fallback
	// and therefore the "current" node "Return to current graph node"
	// returns to.
	current := page.GetByRole(*playwright.AriaRoleButton, playwright.PageGetByRoleOptions{
		Name: seededPlanRegionNodeLabel, Exact: playwright.Bool(true),
	})
	var inspectedID, keyboardSelectedID, currentID string
	if err == nil {
		err = inspected.Click()
	}
	if err == nil {
		inspectedID, err = inspected.GetAttribute("data-node-id")
	}
	if err == nil {
		err = browserAssertions().Locator(inspected).ToHaveAttribute("data-selected", "true")
	}
	if err == nil {
		err = browserAssertions().Locator(canvas).ToHaveAttribute("data-selected-node-id", inspectedID)
	}
	if err == nil {
		err = focusAndPress(keyboardSelected, "Enter")
	}
	if err == nil {
		keyboardSelectedID, err = keyboardSelected.GetAttribute("data-node-id")
	}
	if err == nil {
		err = browserAssertions().Locator(keyboardSelected).ToHaveAttribute("aria-pressed", "true")
	}
	if err == nil {
		err = browserAssertions().Locator(canvas).ToHaveAttribute("data-selected-node-id", keyboardSelectedID)
	}
	var keyboardSelectedDOM playwright.Locator
	if err == nil {
		keyboardSelectedDOM = canvas.Locator(
			fmt.Sprintf(`button[data-component="graph-node"][data-node-id="%s"]`, keyboardSelectedID),
		)
	}
	requestReview := page.GetByRole(*playwright.AriaRoleButton, playwright.PageGetByRoleOptions{
		Name: "Request review", Exact: playwright.Bool(true),
	})
	if err == nil {
		err = requestReview.Click()
	}
	if err == nil {
		// The modal intentionally removes the background graph from the accessibility tree.
		// Address the node by DOM identity while verifying its pressed state is preserved.
		err = browserAssertions().Locator(keyboardSelectedDOM).ToHaveAttribute("aria-pressed", "true")
	}
	closeReview := page.GetByRole(*playwright.AriaRoleButton, playwright.PageGetByRoleOptions{
		Name: "Close review", Exact: playwright.Bool(true),
	})
	if err == nil {
		err = closeReview.Click()
	}
	if err == nil {
		err = browserAssertions().Locator(keyboardSelected).ToHaveAttribute("aria-pressed", "true")
	}
	returnCurrent := page.GetByRole(*playwright.AriaRoleButton, playwright.PageGetByRoleOptions{
		Name: "Return to current graph node", Exact: playwright.Bool(true),
	})
	if err == nil {
		err = returnCurrent.Click()
	}
	if err == nil {
		currentID, err = current.GetAttribute("data-node-id")
	}
	if err == nil {
		err = browserAssertions().Locator(current).ToHaveAttribute("aria-pressed", "true")
	}
	if err == nil {
		err = browserAssertions().Locator(canvas).ToHaveAttribute("data-selected-node-id", currentID)
	}
	appendInteractionResult(page, artifactDir, result, CheckResult{
		ID: "graph-canvas-viewport-selection-and-deliberate-inspection", Route: route, Viewport: "wide",
		Passed: err == nil,
		Detail: fmt.Sprintf(
			"initial=%+v zoomed=%+v panned=%+v fitted=%+v reset=%+v click_node=%s keyboard_node=%s returned=%s error=%v",
			initial, zoomed, panned, fitted, resetState, inspectedID, keyboardSelectedID, currentID, err,
		),
	})
}

func runGraphCanvasResponsiveBoundsCheck(
	page playwright.Page,
	base *url.URL,
	artifactDir string,
	result *Result,
) {
	route := interactionTaskRoute()
	var details []string
	var checkErr error
	for _, viewport := range Viewports {
		if checkErr != nil {
			break
		}
		var viewportRoute string
		if viewport.Mode == "wide" {
			// At wide viewport /tasks no longer inlines the canvas -- it
			// moved to the observation rail's summary (REPO-041a). The only
			// route that still draws `[data-component="graph-canvas"]` at
			// wide is the dedicated /graphs workspace.
			viewportRoute, _, checkErr = loadInteractionGraphRoute(
				page, base, viewport.Width, viewport.Height, viewport.Mode,
			)
		} else {
			viewportRoute, _, checkErr = loadInteractionThread(
				page, base, viewport.Width, viewport.Height, viewport.Mode,
			)
		}
		if checkErr != nil {
			break
		}
		if viewport.Mode == "compact" || viewport.Mode == "minimum" {
			graphTab := page.GetByRole(*playwright.AriaRoleTab, playwright.PageGetByRoleOptions{
				Name: "Task graph", Exact: playwright.Bool(true),
			})
			checkErr = focusAndPress(graphTab, "Enter")
		}
		canvas := page.Locator(`[data-component="graph-canvas"]`)
		if checkErr == nil {
			checkErr = browserAssertions().Locator(canvas).ToBeVisible()
		}
		if checkErr == nil {
			checkErr = browserAssertions().Locator(canvas).ToHaveAttribute("data-responsive-mode", viewport.Mode)
		}
		box, boxErr := canvas.BoundingBox()
		if checkErr == nil && boxErr != nil {
			checkErr = boxErr
		}
		if checkErr == nil && (box == nil || box.Width <= 0 || box.Height <= 0 ||
			box.X < -1 || box.Y < -1 || box.X+box.Width > float64(viewport.Width)+1 ||
			box.Y+box.Height > float64(viewport.Height)+1) {
			checkErr = fmt.Errorf("%s graph bounds %v exceed %dx%d", viewport.Name, box, viewport.Width, viewport.Height)
		}
		details = append(details, fmt.Sprintf("%s@%s=%v", viewport.Name, viewportRoute, box))
	}
	appendInteractionResult(page, artifactDir, result, CheckResult{
		ID: "graph-canvas-responsive-visible-bounds", Route: route,
		Viewport: "wide-standard-compact-minimum", Passed: checkErr == nil,
		Detail: strings.Join(details, " ") + " error=" + errorDetail(checkErr),
	})
}

func readGraphViewportState(locator playwright.Locator) (graphViewportState, error) {
	zoom, err := numericAttribute(locator, "data-zoom")
	if err != nil {
		return graphViewportState{}, err
	}
	panX, err := numericAttribute(locator, "data-pan-x")
	if err != nil {
		return graphViewportState{}, err
	}
	panY, err := numericAttribute(locator, "data-pan-y")
	if err != nil {
		return graphViewportState{}, err
	}
	if math.IsNaN(zoom) || math.IsInf(zoom, 0) || zoom <= 0 ||
		math.IsNaN(panX) || math.IsInf(panX, 0) || math.IsNaN(panY) || math.IsInf(panY, 0) {
		return graphViewportState{}, fmt.Errorf("graph viewport attributes are not finite")
	}
	return graphViewportState{Zoom: zoom, PanX: panX, PanY: panY}, nil
}

func numericAttribute(locator playwright.Locator, name string) (float64, error) {
	raw, err := locator.GetAttribute(name)
	if err != nil {
		return 0, err
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %s=%q: %w", name, raw, err)
	}
	return value, nil
}

func waitForNumericAttributeChange(
	locator playwright.Locator,
	name string,
	previous float64,
	tolerance float64,
) error {
	deadline := time.Now().Add(browserAssertionTimeout)
	for time.Now().Before(deadline) {
		value, err := numericAttribute(locator, name)
		if err != nil {
			return err
		}
		if math.Abs(value-previous) > tolerance {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	value, err := numericAttribute(locator, name)
	if err != nil {
		return err
	}
	return fmt.Errorf(
		"%s did not change from %f within tolerance %f: got=%f",
		name,
		previous,
		tolerance,
		value,
	)
}

func waitForNumericAttributeNear(
	locator playwright.Locator,
	name string,
	want float64,
	tolerance float64,
) error {
	deadline := time.Now().Add(5 * time.Second)
	var got float64
	var readErr error
	for {
		got, readErr = numericAttribute(locator, name)
		if readErr == nil && math.Abs(got-want) <= tolerance {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf(
				"%s did not reach %.6f within tolerance %.6f: got=%.6f read_error=%v",
				name, want, tolerance, got, readErr,
			)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
