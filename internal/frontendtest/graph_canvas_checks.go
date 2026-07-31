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

func runMountedGraphCanvasContractCheck(
	page playwright.Page,
	base *url.URL,
	artifactDir string,
	result *Result,
) {
	route, _, err := loadInteractionThread(page, base, 1600, 1000, "wide")
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
			surfaceVisibleErr == nil && surfaceVisible && nodeCountErr == nil && nodeCount == 10 &&
			accessibleNodes && len(shapes) >= 4 && selectedCount == 1 &&
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
	route, _, err := loadInteractionThread(page, base, 1600, 1000, "wide")
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
		err = waitForNumericAttributeNear(
			canvas, "data-zoom", initial.Zoom, graphTransformTolerance,
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
	inspected := page.GetByRole(*playwright.AriaRoleButton, playwright.PageGetByRoleOptions{
		Name: "Shell requirements - Complete", Exact: playwright.Bool(true),
	})
	keyboardSelected := page.GetByRole(*playwright.AriaRoleButton, playwright.PageGetByRoleOptions{
		Name: "Responsive layout - Active", Exact: playwright.Bool(true),
	})
	keyboardSelectedDOM := canvas.Locator(
		`button[data-component="graph-node"][data-node-id="responsive"]`,
	)
	current := page.GetByRole(*playwright.AriaRoleButton, playwright.PageGetByRoleOptions{
		Name: "GWC workspace - Active", Exact: playwright.Bool(true),
	})
	if err == nil {
		err = inspected.Click()
	}
	if err == nil {
		err = browserAssertions().Locator(inspected).ToHaveAttribute("data-selected", "true")
	}
	if err == nil {
		err = browserAssertions().Locator(canvas).ToHaveAttribute("data-selected-node-id", "requirements")
	}
	if err == nil {
		err = focusAndPress(keyboardSelected, "Enter")
	}
	if err == nil {
		err = browserAssertions().Locator(keyboardSelected).ToHaveAttribute("aria-pressed", "true")
	}
	if err == nil {
		err = browserAssertions().Locator(canvas).ToHaveAttribute("data-selected-node-id", "responsive")
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
		err = browserAssertions().Locator(current).ToHaveAttribute("aria-pressed", "true")
	}
	if err == nil {
		err = browserAssertions().Locator(canvas).ToHaveAttribute("data-selected-node-id", "implementation")
	}
	appendInteractionResult(page, artifactDir, result, CheckResult{
		ID: "graph-canvas-viewport-selection-and-deliberate-inspection", Route: route, Viewport: "wide",
		Passed: err == nil,
		Detail: fmt.Sprintf(
			"initial=%+v zoomed=%+v panned=%+v fitted=%+v reset=%+v click_node=requirements keyboard_node=responsive returned=implementation error=%v",
			initial, zoomed, panned, fitted, resetState, err,
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
		_, _, checkErr = loadInteractionThread(
			page, base, viewport.Width, viewport.Height, viewport.Mode,
		)
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
		details = append(details, fmt.Sprintf("%s=%v", viewport.Name, box))
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
