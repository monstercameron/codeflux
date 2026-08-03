package frontendtest

import (
	"bytes"
	"fmt"
	"math"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mxschmitt/playwright-go"
)

const (
	interactionThreadViewportWidth  = 720
	interactionThreadViewportHeight = 900
	reducedVisualViewportHeight     = 520
)

func runAdversarialInteractionChecks(
	page playwright.Page,
	base *url.URL,
	artifactDir string,
	result *Result,
) {
	runMountedGraphCanvasContractCheck(page, base, artifactDir, result)
	runGraphCanvasInteractionCheck(page, base, artifactDir, result)
	runGraphCanvasResponsiveBoundsCheck(page, base, artifactDir, result)
	runGraphSelectionPersistenceCheck(page, base, artifactDir, result)
	runReducedVisualViewportComposerCheck(page, base, artifactDir, result)
	runKeyboardTraversalCheck(page, base, artifactDir, result)
	runShortcutDialogFocusRestorationCheck(page, base, artifactDir, result)
	runResponsiveRailFocusRestorationCheck(page, base, artifactDir, result)
	runGraphCloseFocusRestorationCheck(page, base, artifactDir, result)
}

func runGraphSelectionPersistenceCheck(
	page playwright.Page,
	base *url.URL,
	artifactDir string,
	result *Result,
) {
	route, root, err := loadInteractionThread(
		page,
		base,
		interactionThreadViewportWidth,
		interactionThreadViewportHeight,
		"minimum",
	)
	detail := []string{"selected=requirement node"}
	graphTab := page.GetByRole(*playwright.AriaRoleTab, playwright.PageGetByRoleOptions{
		Name: "Task graph", Exact: playwright.Bool(true),
	})
	conversationTab := page.GetByRole(*playwright.AriaRoleTab, playwright.PageGetByRoleOptions{
		Name: "Conversation", Exact: playwright.Bool(true),
	})
	// The requirement node (REPO-041's seeded thread) is deliberately not the
	// default-selected node, unlike the plan region: selecting it is what
	// this check needs to exercise, not what it starts already true.
	selectedNode := page.GetByRole(*playwright.AriaRoleButton, playwright.PageGetByRoleOptions{
		Name: seededRequirementNodeLabel, Exact: playwright.Bool(true),
		IncludeHidden: playwright.Bool(true),
	})
	graph := page.GetByRole(*playwright.AriaRoleComplementary, playwright.PageGetByRoleOptions{
		Name: "Task graph", Exact: playwright.Bool(true),
	})
	if err == nil {
		err = focusAndPress(graphTab, "Enter")
	}
	if err == nil {
		err = browserAssertions().Locator(graph).ToBeVisible()
	}
	if err == nil {
		err = focusAndPress(selectedNode, "Enter")
	}
	if err == nil {
		err = browserAssertions().Locator(selectedNode).ToHaveAttribute("aria-pressed", "true")
	}
	if err == nil {
		err = focusAndPress(conversationTab, "Enter")
	}
	if err == nil {
		err = browserAssertions().Locator(graph).ToBeHidden()
	}
	if err == nil {
		err = browserAssertions().Locator(selectedNode).ToHaveAttribute("aria-pressed", "true")
	}
	if err == nil {
		err = focusAndPress(graphTab, "Enter")
	}
	if err == nil {
		err = browserAssertions().Locator(selectedNode).ToHaveAttribute("aria-pressed", "true")
	}
	for _, viewport := range []Viewport{
		{Name: "compact", Mode: "compact", Width: 960, Height: 900},
		{Name: "standard", Mode: "standard", Width: 1280, Height: 900},
		{Name: "wide", Mode: "wide", Width: 1600, Height: 1000},
		{Name: "minimum", Mode: "minimum", Width: 720, Height: 900},
	} {
		if err != nil {
			break
		}
		if resizeErr := page.SetViewportSize(viewport.Width, viewport.Height); resizeErr != nil {
			err = resizeErr
			break
		}
		if resizeErr := browserAssertions().Locator(root).ToHaveAttribute("data-responsive-mode", viewport.Mode); resizeErr != nil {
			err = resizeErr
			break
		}
		if selectionErr := browserAssertions().Locator(selectedNode).ToHaveAttribute("aria-pressed", "true"); selectionErr != nil {
			err = selectionErr
			break
		}
		detail = append(detail, viewport.Name+"=preserved")
	}
	if err == nil {
		err = browserAssertions().Locator(graphTab).ToHaveAttribute("aria-selected", "true")
	}
	if err == nil {
		err = browserAssertions().Locator(graph).ToBeVisible()
	}
	appendInteractionResult(
		page,
		artifactDir,
		result,
		CheckResult{
			ID:       "graph-selection-survives-narrow-tabs-and-viewport-churn",
			Route:    route,
			Viewport: "minimum-to-wide-to-minimum",
			Passed:   err == nil,
			Detail:   strings.Join(detail, " ") + " error=" + errorDetail(err),
		},
	)
}

func runReducedVisualViewportComposerCheck(
	page playwright.Page,
	base *url.URL,
	artifactDir string,
	result *Result,
) {
	route, root, err := loadInteractionThread(
		page,
		base,
		interactionThreadViewportWidth,
		interactionThreadViewportHeight,
		"minimum",
	)
	composer := page.GetByRole(*playwright.AriaRoleTextbox, playwright.PageGetByRoleOptions{
		Name: "Message", Exact: playwright.Bool(true),
	})
	if err == nil {
		err = selectDurableSessionForComposer(page, composer)
	}
	if err == nil {
		err = composer.Focus()
	}
	if err == nil {
		err = browserAssertions().Locator(composer).ToBeFocused()
	}
	if err == nil {
		err = page.SetViewportSize(interactionThreadViewportWidth, reducedVisualViewportHeight)
	}
	if err == nil {
		err = browserAssertions().Locator(composer).ToBeFocused()
	}
	box, boxErr := composer.BoundingBox()
	if err == nil && boxErr != nil {
		err = boxErr
	}
	if err == nil && (box == nil || box.Y < -1 || box.Y+box.Height > reducedVisualViewportHeight+1) {
		err = fmt.Errorf("composer box %v exceeds reduced visual viewport height %d", box, reducedVisualViewportHeight)
	}
	overflow, overflowErr := root.GetAttribute("data-horizontal-overflow")
	if err == nil && overflowErr != nil {
		err = overflowErr
	}
	if err == nil && overflow != "false" {
		err = fmt.Errorf("data-horizontal-overflow=%q", overflow)
	}
	appendInteractionResult(
		page,
		artifactDir,
		result,
		CheckResult{
			ID:       "composer-reachable-in-reduced-visual-viewport",
			Route:    route,
			Viewport: "minimum-software-keyboard",
			Passed:   err == nil,
			Detail: fmt.Sprintf(
				"layout_viewport=%dx%d reduced_visual_viewport=%dx%d box=%v focused=true horizontal_overflow=%q error=%v",
				interactionThreadViewportWidth,
				interactionThreadViewportHeight,
				interactionThreadViewportWidth,
				reducedVisualViewportHeight,
				box,
				overflow,
				err,
			),
		},
	)
	_ = page.SetViewportSize(interactionThreadViewportWidth, interactionThreadViewportHeight)
}

func runKeyboardTraversalCheck(
	page playwright.Page,
	base *url.URL,
	artifactDir string,
	result *Result,
) {
	route, _, err := loadInteractionThread(page, base, 1600, 1000, "wide")
	var stops []playwright.Locator
	var labels []string
	if err == nil {
		stops, labels, err = keyboardTabStops(page)
	}
	if err == nil && len(stops) == 0 {
		err = fmt.Errorf("no keyboard tab stops found")
	}
	forward := 0
	reverse := 0
	visual := 0
	for index, stop := range stops {
		if err != nil {
			break
		}
		var before []byte
		if index == 0 {
			before, err = page.Screenshot(playwright.PageScreenshotOptions{
				Animations: playwright.ScreenshotAnimationsDisabled,
				Caret:      playwright.ScreenshotCaretHide,
				Scale:      playwright.ScreenshotScaleCss,
			})
		} else {
			before, err = focusVisualClip(page, stop, 1600, 1000)
		}
		if err != nil {
			break
		}
		if pressErr := page.Keyboard().Press("Tab"); pressErr != nil {
			err = pressErr
			break
		}
		if focusErr := browserAssertions().Locator(stop).ToBeFocused(); focusErr != nil {
			err = fmt.Errorf("forward stop %d %q: %w", index, labels[index], focusErr)
			break
		}
		isVisible, visibilityErr := stop.IsVisible()
		if visibilityErr != nil || !isVisible {
			err = fmt.Errorf("forward stop %d %q visible=%t: %v", index, labels[index], isVisible, visibilityErr)
			break
		}
		forward++
		var after []byte
		if index == 0 {
			after, err = page.Screenshot(playwright.PageScreenshotOptions{
				Animations: playwright.ScreenshotAnimationsDisabled,
				Caret:      playwright.ScreenshotCaretHide,
				Scale:      playwright.ScreenshotScaleCss,
			})
		} else {
			after, err = focusVisualClip(page, stop, 1600, 1000)
		}
		if err != nil {
			break
		}
		if bytes.Equal(before, after) {
			err = fmt.Errorf("forward stop %d %q has no screenshot-visible keyboard focus change", index, labels[index])
			break
		}
		visual++
	}
	for index := len(stops) - 2; err == nil && index >= 0; index-- {
		if pressErr := page.Keyboard().Press("Shift+Tab"); pressErr != nil {
			err = pressErr
			break
		}
		if focusErr := browserAssertions().Locator(stops[index]).ToBeFocused(); focusErr != nil {
			err = fmt.Errorf("reverse stop %d %q: %w", index, labels[index], focusErr)
			break
		}
		reverse++
	}
	appendInteractionResult(
		page,
		artifactDir,
		result,
		CheckResult{
			ID:       "keyboard-full-shell-forward-reverse-visible-focus",
			Route:    route,
			Viewport: "wide",
			Passed: err == nil && forward == len(stops) &&
				reverse == maximum(0, len(stops)-1) && visual == len(stops),
			Detail: fmt.Sprintf(
				"tab_stops=%d forward=%d reverse=%d visual_focus_changes=%d labels=%q error=%v",
				len(stops),
				forward,
				reverse,
				visual,
				labels,
				err,
			),
		},
	)
}

func runShortcutDialogFocusRestorationCheck(
	page playwright.Page,
	base *url.URL,
	artifactDir string,
	result *Result,
) {
	route, _, err := loadInteractionThread(page, base, 1600, 1000, "wide")
	help := page.GetByRole(*playwright.AriaRoleButton, playwright.PageGetByRoleOptions{
		Name: "Shortcut help", Exact: playwright.Bool(true),
	})
	dialog := page.GetByRole(*playwright.AriaRoleDialog, playwright.PageGetByRoleOptions{
		Name: "Keyboard shortcuts", Exact: playwright.Bool(true),
	})
	closeButton := page.GetByRole(*playwright.AriaRoleButton, playwright.PageGetByRoleOptions{
		Name: "Close keyboard shortcut help", Exact: playwright.Bool(true),
	})
	if err == nil {
		err = focusAndPress(help, "Enter")
	}
	if err == nil {
		err = browserAssertions().Locator(dialog).ToBeVisible()
	}
	if err == nil {
		err = browserAssertions().Locator(closeButton).ToBeFocused()
	}
	if err == nil {
		err = closeButton.Press("Enter")
	}
	if err == nil {
		err = browserAssertions().Locator(dialog).ToBeHidden()
	}
	if err == nil {
		err = browserAssertions().Locator(help).ToBeFocused()
	}
	if err == nil {
		err = help.Press("Enter")
	}
	if err == nil {
		err = browserAssertions().Locator(closeButton).ToBeFocused()
	}
	if err == nil {
		err = page.Keyboard().Press("Escape")
	}
	if err == nil {
		err = browserAssertions().Locator(help).ToBeFocused()
	}
	appendInteractionResult(
		page,
		artifactDir,
		result,
		CheckResult{
			ID:       "shortcut-dialog-restores-invoker-focus",
			Route:    route,
			Viewport: "wide",
			Passed:   err == nil,
			Detail:   "keyboard_open=Enter keyboard_close=Enter_and_Escape restore=Shortcut_help error=" + errorDetail(err),
		},
	)
}

func runResponsiveRailFocusRestorationCheck(
	page playwright.Page,
	base *url.URL,
	artifactDir string,
	result *Result,
) {
	route, _, err := loadInteractionThread(page, base, 1280, 900, "standard")
	toggle := page.GetByRole(*playwright.AriaRoleButton, playwright.PageGetByRoleOptions{
		Name: "Toggle thread rail", Exact: playwright.Bool(true),
	})
	rail := page.GetByRole(*playwright.AriaRoleNavigation, playwright.PageGetByRoleOptions{
		Name: "Primary navigation", Exact: playwright.Bool(true),
	})
	closeButton := page.GetByRole(*playwright.AriaRoleButton, playwright.PageGetByRoleOptions{
		Name: "Collapse thread rail", Exact: playwright.Bool(true),
	})
	if err == nil {
		err = browserAssertions().Locator(rail).ToBeVisible()
	}
	if err == nil {
		err = focusAndPress(toggle, "Enter")
	}
	if err == nil {
		err = browserAssertions().Locator(rail).ToBeHidden()
	}
	if err == nil {
		err = browserAssertions().Locator(toggle).ToBeFocused()
	}
	if err == nil {
		err = toggle.Press("Enter")
	}
	if err == nil {
		err = browserAssertions().Locator(rail).ToBeVisible()
	}
	if err == nil {
		err = focusAndPress(closeButton, "Enter")
	}
	if err == nil {
		err = browserAssertions().Locator(rail).ToBeHidden()
	}
	if err == nil {
		err = browserAssertions().Locator(toggle).ToBeFocused()
	}
	appendInteractionResult(
		page,
		artifactDir,
		result,
		CheckResult{
			ID:       "responsive-thread-rail-close-restores-focus",
			Route:    route,
			Viewport: "standard",
			Passed:   err == nil,
			Detail:   "toggle_close_restore=Toggle_thread_rail internal_close_restore=Toggle_thread_rail error=" + errorDetail(err),
		},
	)
}

func runGraphCloseFocusRestorationCheck(
	page playwright.Page,
	base *url.URL,
	artifactDir string,
	result *Result,
) {
	route, _, err := loadInteractionThread(
		page,
		base,
		interactionThreadViewportWidth,
		interactionThreadViewportHeight,
		"minimum",
	)
	graphTab := page.GetByRole(*playwright.AriaRoleTab, playwright.PageGetByRoleOptions{
		Name: "Task graph", Exact: playwright.Bool(true),
	})
	conversationTab := page.GetByRole(*playwright.AriaRoleTab, playwright.PageGetByRoleOptions{
		Name: "Conversation", Exact: playwright.Bool(true),
	})
	graph := page.GetByRole(*playwright.AriaRoleComplementary, playwright.PageGetByRoleOptions{
		Name: "Task graph", Exact: playwright.Bool(true),
	})
	if err == nil {
		err = focusAndPress(graphTab, "Enter")
	}
	if err == nil {
		err = browserAssertions().Locator(graph).ToBeVisible()
	}
	if err == nil {
		err = focusAndPress(conversationTab, "Enter")
	}
	if err == nil {
		err = browserAssertions().Locator(graph).ToBeHidden()
	}
	if err == nil {
		err = browserAssertions().Locator(conversationTab).ToBeFocused()
	}
	appendInteractionResult(
		page,
		artifactDir,
		result,
		CheckResult{
			ID:       "responsive-graph-close-restores-focus",
			Route:    route,
			Viewport: "minimum",
			Passed:   err == nil,
			Detail:   "keyboard_close=Conversation_tab restore=Conversation_tab error=" + errorDetail(err),
		},
	)
}

func loadInteractionThread(
	page playwright.Page,
	base *url.URL,
	width int,
	height int,
	mode string,
) (string, playwright.Locator, error) {
	route := interactionTaskRoute()
	root := page.GetByTestId("app-root")
	if route == "" {
		return route, root, fmt.Errorf("thread route is absent from acceptance routes")
	}
	if err := page.SetViewportSize(width, height); err != nil {
		return route, root, err
	}
	target := base.ResolveReference(&url.URL{Path: route}).String()
	if _, err := page.Goto(target, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
	}); err != nil {
		return route, root, err
	}
	if err := root.WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	}); err != nil {
		return route, root, err
	}
	if err := browserAssertions().Locator(root).ToHaveAttribute("data-responsive-mode", mode); err != nil {
		return route, root, err
	}
	// Bare /tasks is deliberately a thread picker: it does not auto-adopt a
	// thread the way /graphs and /memory adopt the coordinator's
	// SelectedThreadID (see the route comment on selectDurableSessionForComposer).
	// Every check that reaches this function needs a real, coordinator-backed
	// thread with a real task graph and real timeline cards, not the empty
	// shell-preview /tasks renders before one is picked, so the picker is
	// driven here, once, at the single shared entry point every interaction
	// check calls (REPO-041).
	if err := selectDurableThreadRow(page, mode); err != nil {
		return route, root, err
	}
	// Every check in this file shares one Playwright page across the whole
	// suite, and the navigation above does not by itself guarantee
	// document.activeElement starts empty: a check earlier in the run can
	// fail mid keyboard-traversal and leave a real control focused, and that
	// survives into the next check's reloaded document. A focus-restoration
	// check asserts a *specific* control ends up focused, so inheriting an
	// unrelated one it never expected corrupts an otherwise-correct result --
	// this is how shortcut-dialog-restores-invoker-focus failed only inside
	// the suite, immediately after the independently broken
	// runKeyboardTraversalCheck left focus wherever its own failed Tab press
	// landed. Blur whatever is focused so every check starts from the same
	// neutral state instead of depending on what the previous one left.
	if _, err := page.Evaluate(
		`() => { if (document.activeElement && document.activeElement.blur) { document.activeElement.blur(); } }`,
	); err != nil {
		return route, root, err
	}
	return route, root, nil
}

// selectDurableSessionForComposer establishes the authority required for an
// editable mounted composer. M18 deliberately disables mutations before a
// session is selected, so browser checks select a coordinator-backed thread
// instead of relying on the disconnected preview.
//
// loadInteractionThread now selects a thread itself (REPO-041), so by the
// time most checks reach this function the composer is already enabled and
// this is a no-op. It stays as the explicit, defensive entry point for a
// composer-specific acceptance, and still does the work itself if the
// composer is ever reached before a thread has been picked.
func selectDurableSessionForComposer(page playwright.Page, composer playwright.Locator) error {
	disabled, err := composer.IsDisabled()
	if err != nil || !disabled {
		return err
	}
	mode, modeErr := page.GetByTestId("app-root").GetAttribute("data-responsive-mode")
	if modeErr != nil {
		return modeErr
	}
	if err := selectDurableThreadRow(page, mode); err != nil {
		return err
	}
	if err := composer.WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	}); err != nil {
		return fmt.Errorf("wait for selected-session composer: %w", err)
	}
	return browserAssertions().Locator(composer).ToBeEnabled()
}

// selectDurableThreadRow picks a real coordinator-backed thread from the
// rail, opening it first when the responsive mode hides it behind a toggle.
//
// Route note (established investigating REPO-041): bare /tasks is
// deliberately a thread picker, not a route that auto-adopts a thread --
// unlike /graphs and /memory, which adopt the coordinator's
// SelectedThreadID. web/client/main.go is correct as written and is not
// this ticket's defect; the mounted browser matrix simply never drove the
// picker, so every check asserting on graph or timeline content ran against
// the empty shell-preview state no supported path could ever populate.
func selectDurableThreadRow(page playwright.Page, mode string) error {
	narrow := mode == "compact" || mode == "minimum"
	if narrow {
		toggle := page.GetByRole(*playwright.AriaRoleButton, playwright.PageGetByRoleOptions{
			Name: "Toggle thread rail", Exact: playwright.Bool(true),
		})
		if err := focusAndPress(toggle, "Enter"); err != nil {
			return fmt.Errorf("open responsive thread rail: %w", err)
		}
	}
	rows := page.Locator(`[data-component="thread-row"]`)
	if err := rows.First().WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	}); err != nil {
		return fmt.Errorf("wait for durable session row: %w", err)
	}
	count, err := rows.Count()
	if err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("no durable session is available to select")
	}
	if err := rows.First().Click(); err != nil {
		return err
	}
	if narrow {
		collapse := page.GetByRole(*playwright.AriaRoleButton, playwright.PageGetByRoleOptions{
			Name: "Collapse thread rail", Exact: playwright.Bool(true),
		})
		if err := collapse.Click(); err != nil {
			return fmt.Errorf("close responsive thread rail: %w", err)
		}
		if err := browserAssertions().Locator(
			page.GetByRole(*playwright.AriaRoleNavigation, playwright.PageGetByRoleOptions{
				Name: "Primary navigation", Exact: playwright.Bool(true),
			}),
		).ToBeHidden(); err != nil {
			return fmt.Errorf("wait for responsive thread rail close: %w", err)
		}
	}
	return nil
}

func interactionTaskRoute() string {
	for _, route := range Routes {
		if route == "/tasks" {
			return route
		}
		if strings.Contains(route, "/thread/") {
			return route
		}
	}
	return ""
}

func focusAndPress(locator playwright.Locator, key string) error {
	count, err := locator.Count()
	if err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("keyboard target count=%d, want 1", count)
	}
	visible, err := locator.IsVisible()
	if err != nil {
		return err
	}
	if !visible {
		return fmt.Errorf("keyboard target is not visible")
	}
	if err := locator.Focus(playwright.LocatorFocusOptions{
		Timeout: playwright.Float(float64(browserAssertionTimeout.Milliseconds())),
	}); err != nil {
		return err
	}
	if err := browserAssertions().Locator(locator).ToBeFocused(); err != nil {
		return err
	}
	return locator.Press(key)
}

func keyboardTabStops(page playwright.Page) ([]playwright.Locator, []string, error) {
	candidates, err := page.Locator(
		`a[href],button,input,textarea,select,summary,[tabindex]:not([tabindex="-1"])`,
	).All()
	if err != nil {
		return nil, nil, err
	}
	stops := make([]playwright.Locator, 0, len(candidates))
	labels := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		testID, testIDErr := candidate.GetAttribute("data-testid")
		if testIDErr != nil {
			return nil, nil, testIDErr
		}
		visible, visibilityErr := candidate.IsVisible()
		if visibilityErr != nil {
			return nil, nil, visibilityErr
		}
		enabled, enabledErr := candidate.IsEnabled()
		if enabledErr != nil {
			return nil, nil, enabledErr
		}
		if (!visible && testID != "skip-link") || !enabled {
			continue
		}
		tabIndex, tabIndexErr := candidate.GetAttribute("tabindex")
		if tabIndexErr != nil {
			return nil, nil, tabIndexErr
		}
		if tabIndex != "" {
			value, parseErr := strconv.Atoi(tabIndex)
			if parseErr != nil {
				return nil, nil, fmt.Errorf("invalid tabindex %q: %w", tabIndex, parseErr)
			}
			if value > 0 {
				return nil, nil, fmt.Errorf("positive tabindex %d disrupts logical DOM order", value)
			}
		}
		stops = append(stops, candidate)
		labels = append(labels, keyboardStopLabel(candidate))
	}
	return stops, labels, nil
}

func keyboardStopLabel(locator playwright.Locator) string {
	for _, attribute := range []string{"aria-label", "title", "id", "data-component"} {
		value, err := locator.GetAttribute(attribute)
		if err == nil && strings.TrimSpace(value) != "" {
			return attribute + "=" + strings.TrimSpace(value)
		}
	}
	text, err := locator.InnerText()
	if err == nil && strings.TrimSpace(text) != "" {
		text = strings.Join(strings.Fields(text), " ")
		if len(text) > 80 {
			text = text[:80]
		}
		return "text=" + text
	}
	return "unnamed-tab-stop"
}

func focusVisualClip(
	page playwright.Page,
	locator playwright.Locator,
	viewportWidth int,
	viewportHeight int,
) ([]byte, error) {
	if err := locator.ScrollIntoViewIfNeeded(); err != nil {
		return nil, err
	}
	box, err := locator.BoundingBox()
	if err != nil {
		return nil, err
	}
	if box == nil {
		return nil, fmt.Errorf("focused control has no bounding box")
	}
	clip, err := expandedFocusClip(*box, viewportWidth, viewportHeight)
	if err != nil {
		return nil, err
	}
	return page.Screenshot(playwright.PageScreenshotOptions{
		Animations: playwright.ScreenshotAnimationsDisabled,
		Caret:      playwright.ScreenshotCaretHide,
		Scale:      playwright.ScreenshotScaleCss,
		Clip:       &clip,
	})
}

func expandedFocusClip(box playwright.Rect, viewportWidth int, viewportHeight int) (playwright.Rect, error) {
	const margin = 6.0
	x := math.Max(0, box.X-margin)
	y := math.Max(0, box.Y-margin)
	right := math.Min(float64(viewportWidth), box.X+box.Width+margin)
	bottom := math.Min(float64(viewportHeight), box.Y+box.Height+margin)
	if right <= x || bottom <= y {
		return playwright.Rect{}, fmt.Errorf(
			"focus clip outside viewport: box=%v viewport=%dx%d",
			box,
			viewportWidth,
			viewportHeight,
		)
	}
	return playwright.Rect{X: x, Y: y, Width: right - x, Height: bottom - y}, nil
}

func appendInteractionResult(
	page playwright.Page,
	artifactDir string,
	result *Result,
	check CheckResult,
) {
	appendResult(result, check)
	if check.Passed {
		return
	}
	filename := "failure-" + check.ID + ".png"
	captureLastFailure(page, result, artifactDir, filepath.Clean(filename))
}

func maximum(a int, b int) int {
	if a > b {
		return a
	}
	return b
}
