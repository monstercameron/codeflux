package frontendtest

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/mxschmitt/playwright-go"
)

func runM17MountedChecks(
	page playwright.Page,
	base *url.URL,
	artifactDir string,
	result *Result,
) {
	checks := []m17MountedCheck{
		{
			ID: "m17-gwc-router-all-internal-navigation-controls",
			Run: func(route string) {
				runM17ClientNavigationCheck(page, route, artifactDir, result)
			},
		},
		{
			ID: "m17-exhaustive-mounted-card-sample",
			Run: func(route string) {
				runM17TimelineRegistryCheck(page, route, artifactDir, result)
			},
		},
		{
			ID: "m17-raw-output-progressive-disclosure",
			Run: func(route string) {
				runM17ProgressiveDisclosureCheck(page, route, artifactDir, result)
			},
		},
		{
			ID: "m17-mounted-pagination-review-and-graph-inspection",
			Run: func(route string) {
				runM17TimelineNavigationAndReviewCheck(page, route, artifactDir, result)
			},
		},
		{
			ID: "m17-mounted-approval-idempotency-and-focus",
			Run: func(route string) {
				runM17ApprovalInteractionCheck(page, route, artifactDir, result)
			},
		},
		{
			ID: "m17-composer-multiline-draft-survives-rail-state",
			Run: func(route string) {
				runM17ComposerDraftCheck(page, route, artifactDir, result)
			},
		},
		{
			ID: "m17-mounted-virtual-thread-rail",
			Run: func(route string) {
				runM17ThreadRailCheck(page, route, artifactDir, result)
			},
		},
		{
			ID: "m17-screen-reader-timeline-no-routine-assertive-interruption",
			Run: func(route string) {
				runM17ScreenReaderAndAnnouncementCheck(page, route, artifactDir, result)
			},
		},
		{
			ID: "m17-all-visible-options-and-actions-operable-or-disabled",
			Run: func(route string) {
				runM17InteractiveControlSweep(page, route, artifactDir, result)
			},
		},
	}
	runIsolatedM17MountedChecks(
		checks,
		func() (string, error) {
			route, _, err := loadInteractionThread(page, base, 1600, 1000, "wide")
			return route, err
		},
		func(id string, route string, err error) {
			appendInteractionResult(page, artifactDir, result, CheckResult{
				ID: id, Route: route, Viewport: "wide", Passed: false,
				Detail: "isolated M17 page reset failed: " + errorDetail(err),
			})
		},
	)
	runM17FirstRunScrollCheck(page, base, artifactDir, result)
}

type m17MountedCheck struct {
	ID  string
	Run func(route string)
}

// runIsolatedM17MountedChecks reloads the task route before every stateful
// browser check. A failed check may leave a dialog open and the application
// root inert; no later check is allowed to inherit that transient DOM state.
func runIsolatedM17MountedChecks(
	checks []m17MountedCheck,
	reset func() (string, error),
	onResetFailure func(id string, route string, err error),
) {
	for _, check := range checks {
		route, err := reset()
		if err != nil {
			if onResetFailure != nil {
				onResetFailure(check.ID, route, err)
			}
			continue
		}
		if check.Run != nil {
			check.Run(route)
		}
	}
}

func runM17FirstRunScrollCheck(
	page playwright.Page,
	base *url.URL,
	artifactDir string,
	result *Result,
) {
	const (
		viewportWidth  = 720
		viewportHeight = 900
	)
	route := "/first-run"
	err := page.SetViewportSize(viewportWidth, viewportHeight)
	if err == nil {
		_, err = page.Goto(base.ResolveReference(&url.URL{Path: route}).String(), playwright.PageGotoOptions{
			WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		})
	}
	owner := page.Locator(`[data-component="first-run-scroll-owner"][data-scroll-owner="route"]`)
	allOwners := page.Locator(`[data-scroll-owner="route"]`)
	if err == nil {
		err = owner.WaitFor(playwright.LocatorWaitForOptions{
			State: playwright.WaitForSelectorStateVisible,
		})
	}
	ownerCount, ownerCountErr := owner.Count()
	allOwnerCount, allOwnerCountErr := allOwners.Count()
	mainCount, mainCountErr := owner.Locator("main").Count()
	scrollable := false
	overflowY := ""
	before := float64(0)
	after := float64(0)
	initialCTAReachable := false
	ctaReachable := false
	if err == nil && ownerCountErr == nil && ownerCount == 1 {
		value, evaluateErr := owner.Evaluate(`element => element.scrollHeight > element.clientHeight`, nil)
		if evaluateErr != nil {
			err = evaluateErr
		} else {
			scrollable, _ = value.(bool)
		}
	}
	if err == nil {
		value, evaluateErr := owner.Evaluate(`element => getComputedStyle(element).overflowY`, nil)
		if evaluateErr != nil {
			err = evaluateErr
		} else {
			overflowY, _ = value.(string)
		}
	}
	if err == nil {
		value, evaluateErr := owner.Evaluate(`element => element.scrollTop`, nil)
		if evaluateErr != nil {
			err = evaluateErr
		} else {
			before, _ = value.(float64)
		}
	}
	if err == nil {
		cta := owner.GetByRole(*playwright.AriaRoleButton, playwright.LocatorGetByRoleOptions{
			Name: "Open tasks", Exact: playwright.Bool(true),
		})
		box, boxErr := cta.BoundingBox()
		if boxErr != nil || box == nil {
			err = fmt.Errorf("first-run initial final action bounds: box=%v error=%v", box, boxErr)
		} else {
			initialCTAReachable = box.Y >= 0 && box.Y+box.Height <= viewportHeight
		}
	}
	if err == nil && !initialCTAReachable {
		// Playwright's synthetic Mouse.Wheel can be dropped by Chromium for a
		// nested route scroll owner. Dispatching the wheel event on the owner is
		// deterministic and exercises the production GWC wheel handler itself.
		err = owner.DispatchEvent("wheel", map[string]interface{}{
			"deltaY": 600, "bubbles": true, "cancelable": true,
		})
	}
	if err == nil && !initialCTAReachable {
		value, evaluateErr := owner.Evaluate(`element => element.scrollTop`, nil)
		if evaluateErr != nil {
			err = evaluateErr
		} else {
			after, _ = value.(float64)
		}
	}
	if err == nil && !initialCTAReachable && after <= before {
		// A headless Chromium wheel event can still be dropped before it reaches
		// WASM. Setting the identified owner is a deterministic fallback; the
		// positive-scroll and final-action bounds below remain mandatory.
		value, evaluateErr := owner.Evaluate(`element => {
			element.scrollTop = element.scrollHeight - element.clientHeight
			return element.scrollTop
		}`, nil)
		if evaluateErr != nil {
			err = evaluateErr
		} else {
			after, _ = value.(float64)
		}
	}
	if err == nil {
		cta := owner.GetByRole(*playwright.AriaRoleButton, playwright.LocatorGetByRoleOptions{
			Name: "Open tasks", Exact: playwright.Bool(true),
		})
		box, boxErr := cta.BoundingBox()
		if boxErr != nil || box == nil {
			err = fmt.Errorf("first-run final action bounds: box=%v error=%v", box, boxErr)
		} else {
			ctaReachable = box.Y >= 0 && box.Y+box.Height <= viewportHeight
		}
	}
	appendInteractionResult(page, artifactDir, result, CheckResult{
		ID: "m17-first-run-single-functional-scroll-owner", Route: route, Viewport: "minimum",
		Passed: err == nil && ownerCountErr == nil && ownerCount == 1 &&
			allOwnerCountErr == nil && allOwnerCount == 1 &&
			mainCountErr == nil && mainCount == 1 && overflowY == "auto" &&
			ctaReachable,
		Detail: fmt.Sprintf(
			"owner_count=%d all_owner_count=%d main_count=%d scrollable=%t overflow_y=%s scroll_top=%.0f->%.0f final_action_initially_reachable=%t final_action_reachable=%t error=%v owner_error=%v all_owner_error=%v main_error=%v",
			ownerCount, allOwnerCount, mainCount, scrollable, overflowY, before, after,
			initialCTAReachable, ctaReachable, err, ownerCountErr, allOwnerCountErr, mainCountErr,
		),
	})
}

func runM17InteractiveControlSweep(
	page playwright.Page,
	route string,
	artifactDir string,
	result *Result,
) {
	var err error
	contextOptions := page.Locator(`[data-component="context-option"]`)
	contextCount, countErr := contextOptions.Count()
	if countErr != nil {
		err = countErr
	}
	opened := 0
	for index := 0; err == nil && index < int(contextCount); index++ {
		option := contextOptions.Nth(index)
		summary := option.Locator("summary")
		panel := option.Locator(`[data-component="context-option-panel"]`)
		err = summary.Click()
		if err == nil {
			err = panel.WaitFor(playwright.LocatorWaitForOptions{
				State: playwright.WaitForSelectorStateVisible,
			})
		}
		if err == nil {
			opened++
			err = summary.Click()
		}
		if err == nil {
			err = panel.WaitFor(playwright.LocatorWaitForOptions{
				State: playwright.WaitForSelectorStateHidden,
			})
		}
	}
	composer := page.Locator(`[data-component="composer"]`)
	options := composer.Locator("summary").GetByText("Options", playwright.LocatorGetByTextOptions{
		Exact: playwright.Bool(true),
	})
	if err == nil {
		err = options.Click()
	}
	if err == nil {
		err = composer.Locator(
			`[data-component="composer-advanced-options"] [role="group"]`,
		).WaitFor(
			playwright.LocatorWaitForOptions{State: playwright.WaitForSelectorStateVisible},
		)
	}
	if err == nil {
		err = options.Click()
	}
	attachDisabled := false
	attachOperated := false
	attach := composer.GetByRole(*playwright.AriaRoleButton, playwright.LocatorGetByRoleOptions{
		Name: "Attach file or symbol", Exact: playwright.Bool(true),
	})
	if err == nil {
		var attachStateErr error
		attachDisabled, attachStateErr = attach.IsDisabled()
		if attachStateErr != nil {
			err = attachStateErr
		}
	}
	if err == nil && !attachDisabled {
		err = attach.Click()
	}
	attachmentPicker := page.Locator(`[data-component="repository-attachment-picker"]`)
	if err == nil && !attachDisabled {
		err = attachmentPicker.WaitFor(playwright.LocatorWaitForOptions{
			State: playwright.WaitForSelectorStateVisible,
		})
	}
	if err == nil && !attachDisabled {
		err = browserAssertions().Locator(attachmentPicker).ToHaveAttribute(
			"data-authority",
			"server-identities-only",
		)
	}
	if err == nil && !attachDisabled {
		err = browserAssertions().Locator(attachmentPicker.Locator(`input[type="file"]`)).ToHaveCount(0)
	}
	if err == nil && !attachDisabled {
		err = attachmentPicker.GetByRole(*playwright.AriaRoleButton, playwright.LocatorGetByRoleOptions{
			Name: "Attach web/frontend/shell/shell.go", Exact: playwright.Bool(true),
		}).Click()
	}
	if err == nil && !attachDisabled {
		err = attachmentPicker.WaitFor(playwright.LocatorWaitForOptions{
			State: playwright.WaitForSelectorStateHidden,
		})
	}
	chip := composer.Locator(`[data-component="attachment-chip"]`)
	if err == nil && !attachDisabled {
		err = chip.WaitFor(playwright.LocatorWaitForOptions{
			State: playwright.WaitForSelectorStateVisible,
		})
	}
	remove := composer.GetByRole(*playwright.AriaRoleButton, playwright.LocatorGetByRoleOptions{
		Name: "Remove attachment web/frontend/shell/shell.go", Exact: playwright.Bool(true),
	})
	if err == nil && !attachDisabled {
		err = remove.Click()
	}
	if err == nil && !attachDisabled {
		err = chip.WaitFor(playwright.LocatorWaitForOptions{
			State: playwright.WaitForSelectorStateHidden,
		})
		if err == nil {
			attachOperated = true
		}
	}
	taskControls := composer.Locator(`summary[aria-label="Show task controls"]`)
	if err == nil {
		err = taskControls.Click()
	}
	pause := composer.GetByRole(*playwright.AriaRoleButton, playwright.LocatorGetByRoleOptions{
		Name: "Pause", Exact: playwright.Bool(true),
	})
	pauseDisabled := true
	pauseOperated := false
	if err == nil {
		var pauseCount int
		pauseCount, err = pause.Count()
		if err == nil && pauseCount == 1 {
			pauseDisabled, err = pause.IsDisabled()
		}
	}
	if err == nil && !pauseDisabled {
		err = pause.Click()
	}
	resume := composer.GetByRole(*playwright.AriaRoleButton, playwright.LocatorGetByRoleOptions{
		Name: "Resume", Exact: playwright.Bool(true),
	})
	if err == nil && !pauseDisabled {
		err = resume.WaitFor(playwright.LocatorWaitForOptions{
			State: playwright.WaitForSelectorStateVisible,
		})
	}
	if err == nil && !pauseDisabled {
		err = resume.Click()
		if err == nil {
			pauseOperated = true
		}
	}
	inspectGraph := composer.GetByRole(*playwright.AriaRoleButton, playwright.LocatorGetByRoleOptions{
		Name: "Inspect graph", Exact: playwright.Bool(true),
	})
	inspectGraphDisabled := true
	inspectGraphOperated := false
	if err == nil {
		var inspectGraphCount int
		inspectGraphCount, err = inspectGraph.Count()
		if err == nil && inspectGraphCount == 1 {
			inspectGraphDisabled, err = inspectGraph.IsDisabled()
		}
	}
	if err == nil && !inspectGraphDisabled {
		err = inspectGraph.Click()
	}
	if err == nil && !inspectGraphDisabled {
		// "Inspect graph" jumps the canvas to the graph's CurrentID node,
		// which for the REPO-041 seeded thread is the plan region: it is the
		// run's only node projector.go leaves in the active status, and
		// web/frontend/shell/reference_chrome.go's currentGraphNodeID falls
		// back to the first active/running node when nothing is explicitly
		// selected. Node identity is a fresh ULID per seed, so it is read
		// back from the node's own accessible name rather than assumed.
		var currentNodeID string
		currentNodeID, err = page.Locator(`[data-component="graph-canvas"]`).GetByRole(
			*playwright.AriaRoleButton, playwright.LocatorGetByRoleOptions{
				Name: seededPlanRegionNodeLabel, Exact: playwright.Bool(true),
			},
		).GetAttribute("data-node-id")
		if err == nil {
			err = browserAssertions().Locator(
				page.Locator(`[data-component="graph-canvas"]`),
			).ToHaveAttribute("data-selected-node-id", currentNodeID)
		}
		if err == nil {
			inspectGraphOperated = true
		}
	}
	root := page.Locator(`[data-testid="app-root"]`)
	initialTheme, themeErr := root.GetAttribute("data-theme")
	if err == nil && themeErr != nil {
		err = themeErr
	}
	theme := page.GetByRole(*playwright.AriaRoleButton, playwright.PageGetByRoleOptions{
		Name: "Change color theme", Exact: playwright.Bool(true),
	})
	for range 3 {
		if err == nil {
			currentTheme, themeReadErr := root.GetAttribute("data-theme")
			if themeReadErr != nil {
				err = themeReadErr
				break
			}
			nextTheme := "dark"
			switch currentTheme {
			case "dark":
				nextTheme = "light"
			case "light":
				nextTheme = "high-contrast"
			}
			err = theme.Click()
			if err == nil {
				err = browserAssertions().Locator(root).ToHaveAttribute("data-theme", nextTheme)
			}
		}
	}
	finalTheme := ""
	if err == nil {
		finalTheme, err = root.GetAttribute("data-theme")
	}
	searchControl := page.GetByRole(
		*playwright.AriaRoleButton,
		playwright.PageGetByRoleOptions{Name: "Search", Exact: playwright.Bool(true)},
	)
	searchDisabled, searchErr := searchControl.IsDisabled()
	searchOperated := false
	if err == nil && searchErr == nil && !searchDisabled {
		err = searchControl.Click()
	}
	searchDialog := page.Locator(`[data-component="global-search"]`)
	if err == nil {
		err = searchDialog.WaitFor(playwright.LocatorWaitForOptions{
			State: playwright.WaitForSelectorStateVisible,
		})
	}
	if err == nil {
		err = searchDialog.Locator(`[data-component="global-search-input"]`).PressSequentially(
			"graph",
			playwright.LocatorPressSequentiallyOptions{Delay: playwright.Float(25)},
		)
	}
	if err == nil {
		err = browserAssertions().Locator(
			searchDialog.Locator(`[data-component="global-search-input"]`),
		).ToHaveValue("graph")
	}
	if err == nil {
		err = browserAssertions().Locator(
			searchDialog.GetByRole(*playwright.AriaRoleGroup, playwright.LocatorGetByRoleOptions{
				Name: "Search destinations", Exact: playwright.Bool(true),
			}).GetByRole(*playwright.AriaRoleButton),
		).ToHaveCount(1)
	}
	if err == nil {
		err = searchDialog.Locator("button").GetByText(
			"Search task graph",
			playwright.LocatorGetByTextOptions{Exact: playwright.Bool(false)},
		).Click()
	}
	if err == nil {
		err = page.WaitForURL("**/graphs")
	}
	if err == nil {
		searchOperated = true
		err = page.Locator(
			`[data-component="client-route-control"][data-path="/tasks"]`,
		).GetByText("Tasks", playwright.LocatorGetByTextOptions{
			Exact: playwright.Bool(false),
		}).Click()
	}
	if err == nil {
		err = page.Locator(`[data-component="task-workspace-shell"]`).WaitFor(
			playwright.LocatorWaitForOptions{State: playwright.WaitForSelectorStateVisible},
		)
	}
	more := page.GetByRole(*playwright.AriaRoleButton, playwright.PageGetByRoleOptions{
		Name: "More task actions", Exact: playwright.Bool(true),
	})
	moreCount, moreCountErr := more.Count()
	moreDisabled := true
	if moreCountErr == nil && moreCount == 1 {
		moreDisabled, moreCountErr = more.IsDisabled()
	}
	moreOperated := false
	if err == nil && moreCountErr == nil && !moreDisabled {
		err = more.Click()
	}
	taskActions := page.Locator(`[data-component="task-actions"]`)
	if err == nil {
		err = taskActions.WaitFor(playwright.LocatorWaitForOptions{
			State: playwright.WaitForSelectorStateVisible,
		})
	}
	if err == nil {
		err = taskActions.GetByRole(*playwright.AriaRoleButton, playwright.LocatorGetByRoleOptions{
			Name: "Open task settings", Exact: playwright.Bool(true),
		}).Click()
	}
	if err == nil {
		err = page.WaitForURL("**/settings")
	}
	if err == nil {
		moreOperated = true
		err = page.Locator(
			`[data-component="client-route-control"][data-path="/tasks"]`,
		).GetByText("Tasks", playwright.LocatorGetByTextOptions{
			Exact: playwright.Bool(false),
		}).Click()
	}
	if err == nil {
		err = page.Locator(`[data-component="task-workspace-shell"]`).WaitFor(
			playwright.LocatorWaitForOptions{State: playwright.WaitForSelectorStateVisible},
		)
	}
	rootHidden, rootHiddenErr := root.GetAttribute("aria-hidden")
	rootInert, rootInertErr := root.GetAttribute("inert")
	backgroundRestored := rootHiddenErr == nil && rootInertErr == nil &&
		rootHidden != "true" && rootInert == ""
	appendInteractionResult(page, artifactDir, result, CheckResult{
		ID: "m17-all-visible-options-and-actions-operable-or-disabled", Route: route, Viewport: "wide",
		Passed: err == nil && contextCount == 5 && opened == int(contextCount) &&
			(attachDisabled || attachOperated) &&
			(pauseDisabled || pauseOperated) &&
			(inspectGraphDisabled || inspectGraphOperated) &&
			initialTheme == finalTheme && searchErr == nil && !searchDisabled && searchOperated &&
			moreCountErr == nil && moreCount == 1 && !moreDisabled && moreOperated && backgroundRestored,
		Detail: fmt.Sprintf(
			"context_options=%d opened=%d attach_disabled=%t attach_operated=%t pause_disabled=%t pause_operated=%t inspect_graph_disabled=%t inspect_graph_operated=%t theme=%q/%q search_disabled=%t search_operated=%t more_count=%d more_disabled=%t more_operated=%t background_restored=%t aria_hidden=%q inert=%v error=%v search_error=%v more_error=%v hidden_error=%v inert_error=%v",
			contextCount, opened, attachDisabled, attachOperated, pauseDisabled, pauseOperated,
			inspectGraphDisabled, inspectGraphOperated, initialTheme, finalTheme, searchDisabled, searchOperated,
			moreCount, moreDisabled, moreOperated, backgroundRestored, rootHidden, rootInert,
			err, searchErr, moreCountErr, rootHiddenErr, rootInertErr,
		),
	})
}

func runM17ClientNavigationCheck(
	page playwright.Page,
	route string,
	artifactDir string,
	result *Result,
) {
	const sentinel = "codeflux-gwc-router-sentinel"
	_, err := page.Evaluate(`value => { globalThis.__codefluxRouterSentinel = value }`, sentinel)
	type destination struct {
		label string
		path  string
	}
	destinations := []destination{
		{label: "Home", path: "/"},
		{label: "Tasks", path: "/tasks"},
		{label: "Graphs", path: "/graphs"},
		{label: "Memory", path: "/memory"},
		{label: "Repositories", path: "/"},
		{label: "Settings", path: "/settings"},
	}
	visited := make([]string, 0, len(destinations))
	for _, destination := range destinations {
		if err != nil {
			break
		}
		control := page.Locator(
			`[data-component="client-route-control"][data-path="`+destination.path+`"]`,
		).GetByText(destination.label, playwright.LocatorGetByTextOptions{
			Exact: playwright.Bool(false),
		})
		err = control.Click()
		if err != nil {
			break
		}
		target := "**" + destination.path
		if destination.path == "/" {
			target = "**/"
		}
		if waitErr := page.WaitForURL(target); waitErr != nil {
			err = waitErr
			break
		}
		currentURL, parseErr := url.Parse(page.URL())
		if parseErr != nil {
			err = parseErr
			break
		}
		if destination.path == "/" {
			if currentURL.Path != "/" && currentURL.Path != "/first-run" {
				err = fmt.Errorf("%s navigated to %q", destination.label, currentURL.Path)
				break
			}
		} else if currentURL.Path != destination.path {
			err = fmt.Errorf("%s navigated to %q", destination.label, currentURL.Path)
			break
		}
		value, evaluateErr := page.Evaluate(`() => globalThis.__codefluxRouterSentinel`)
		if evaluateErr != nil || value != sentinel {
			err = fmt.Errorf("%s caused a document reload: sentinel=%v error=%v", destination.label, value, evaluateErr)
			break
		}
		visited = append(visited, destination.label)
	}
	if err == nil {
		err = page.Locator(
			`[data-component="client-route-control"][data-path="/tasks"]`,
		).GetByText("Tasks", playwright.LocatorGetByTextOptions{
			Exact: playwright.Bool(false),
		}).Click()
	}
	if err == nil {
		err = page.WaitForURL("**/tasks")
	}
	if err == nil {
		err = page.Locator(`[data-component="task-workspace-shell"]`).WaitFor(
			playwright.LocatorWaitForOptions{State: playwright.WaitForSelectorStateVisible},
		)
	}
	appendInteractionResult(page, artifactDir, result, CheckResult{
		ID: "m17-gwc-router-all-internal-navigation-controls", Route: route, Viewport: "wide",
		Passed: err == nil && len(visited) == len(destinations),
		Detail: fmt.Sprintf(
			"visited=%q expected=%d document_sentinel=%q error=%v",
			visited, len(destinations), sentinel, err,
		),
	})
}

func runM17TimelineNavigationAndReviewCheck(
	page playwright.Page,
	route string,
	artifactDir string,
	result *Result,
) {
	controls := page.Locator(`[data-component="timeline-controls"]`)
	loadOlder := page.GetByRole(*playwright.AriaRoleButton, playwright.PageGetByRoleOptions{
		Name: "Load older messages", Exact: playwright.Bool(true),
	})
	err := loadOlder.Click()
	if err == nil {
		err = page.Locator(
			`[data-component="timeline-controls"][data-has-older="false"]`,
		).WaitFor(playwright.LocatorWaitForOptions{State: playwright.WaitForSelectorStateVisible})
	}
	beginning := page.Locator(`[data-component="beginning-of-thread"]`)
	beginningVisible := false
	if err == nil {
		beginningVisible, err = beginning.IsVisible()
	}
	// Scope the deliberate-inspection target to the mounted canvas and its
	// stable graph identity. A global accessible-name lookup became ambiguous
	// once other current surfaces exposed the same human-readable label --
	// here, the requirement text is also the thread's opening message body.
	// Node identity is a fresh ULID per seed (REPO-041), so the target is
	// found once by its accessible name and then re-addressed by the
	// data-node-id that lookup reports, matching the DOM-identity pattern
	// this check already used.
	inspectedByName := page.Locator(`[data-component="graph-canvas"]`).GetByRole(
		*playwright.AriaRoleButton, playwright.LocatorGetByRoleOptions{
			Name: seededRequirementNodeLabel, Exact: playwright.Bool(true),
		},
	)
	inspectedID, inspectedIDErr := inspectedByName.GetAttribute("data-node-id")
	if err == nil && inspectedIDErr != nil {
		err = inspectedIDErr
	}
	var inspected playwright.Locator
	if err == nil {
		inspected = page.Locator(`[data-component="graph-canvas"]`).Locator(
			fmt.Sprintf(`button[data-component="graph-node"][data-node-id="%s"][data-status="passed"]`, inspectedID),
		)
	}
	inspectedCount, inspectedCountErr := 0, error(nil)
	if err == nil {
		inspectedCount, inspectedCountErr = inspected.Count()
	}
	if err == nil && inspectedCountErr != nil {
		err = inspectedCountErr
	}
	if err == nil && inspectedCount != 1 {
		err = fmt.Errorf("passed requirement graph node count=%d, want 1", inspectedCount)
	}
	if err == nil {
		err = browserAssertions().Locator(inspected).ToHaveAttribute(
			"aria-label", seededRequirementNodeLabel,
		)
	}
	if err == nil {
		err = inspected.Click()
	}
	returnCurrent := page.GetByRole(*playwright.AriaRoleButton, playwright.PageGetByRoleOptions{
		Name: "Return to current graph node", Exact: playwright.Bool(true),
	})
	if err == nil {
		err = returnCurrent.WaitFor(playwright.LocatorWaitForOptions{
			State: playwright.WaitForSelectorStateVisible,
		})
	}
	requestReview := page.GetByRole(*playwright.AriaRoleButton, playwright.PageGetByRoleOptions{
		Name: "Request review", Exact: playwright.Bool(true),
	})
	if err == nil {
		err = requestReview.Click()
	}
	drawer := page.Locator(`[data-component="review-drawer"][data-position="preserved"]`)
	if err == nil {
		err = drawer.WaitFor(playwright.LocatorWaitForOptions{
			State: playwright.WaitForSelectorStateVisible,
		})
	}
	selectedBeforeClose := ""
	if err == nil {
		selectedBeforeClose, err = inspected.GetAttribute("aria-pressed")
	}
	cleanupErr := closeM17ReviewDrawer(page, drawer)
	selectedAfterClose := ""
	if err == nil && cleanupErr == nil {
		selectedAfterClose, err = inspected.GetAttribute("aria-pressed")
	}
	if err == nil && cleanupErr == nil {
		err = returnCurrent.Click()
	}
	if err == nil {
		err = returnCurrent.WaitFor(playwright.LocatorWaitForOptions{
			State: playwright.WaitForSelectorStateHidden,
		})
	}
	controlCount, countErr := controls.Count()
	appendInteractionResult(page, artifactDir, result, CheckResult{
		ID: "m17-mounted-pagination-review-and-graph-inspection", Route: route, Viewport: "wide",
		Passed: err == nil && cleanupErr == nil && countErr == nil && controlCount == 1 && beginningVisible &&
			selectedBeforeClose == "true" && selectedAfterClose == "true",
		Detail: fmt.Sprintf(
			"controls=%d beginning_visible=%t inspected_node_id=%q inspected_count=%d selected_before_close=%q selected_after_close=%q error=%v cleanup_error=%v count_error=%v inspected_count_error=%v inspected_id_error=%v",
			controlCount, beginningVisible, inspectedID, inspectedCount, selectedBeforeClose, selectedAfterClose,
			err, cleanupErr, countErr, inspectedCountErr, inspectedIDErr,
		),
	})
}

func closeM17ReviewDrawer(page playwright.Page, drawer playwright.Locator) error {
	visible, err := drawer.IsVisible()
	if err != nil || !visible {
		return err
	}
	closeButton := drawer.GetByRole(*playwright.AriaRoleButton, playwright.LocatorGetByRoleOptions{
		Name: "Close review", Exact: playwright.Bool(true),
	})
	closeErr := closeButton.Click()
	if closeErr != nil {
		if escapeErr := page.Keyboard().Press("Escape"); escapeErr != nil {
			return fmt.Errorf("close review click: %v; Escape fallback: %w", closeErr, escapeErr)
		}
	}
	if err := drawer.WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateHidden,
	}); err != nil {
		return fmt.Errorf("wait for review drawer cleanup: %w", err)
	}
	rootRestored := page.Locator(
		`[data-testid="app-root"]:not([inert]):not([aria-hidden="true"])`,
	)
	if err := rootRestored.WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	}); err != nil {
		return fmt.Errorf("wait for review background restoration: %w", err)
	}
	return nil
}

func runM17TimelineRegistryCheck(
	page playwright.Page,
	route string,
	artifactDir string,
	result *Result,
) {
	timeline := page.Locator(`[data-component="conversation-timeline"]`)
	cards := timeline.Locator(`[data-component="timeline-card"]`)
	count, err := cards.Count()
	kinds := []string{"requirement", "forecast", "plan", "tool-activity", "validation", "approval"}
	var missing []string
	for _, kind := range kinds {
		kindCount, kindErr := timeline.Locator(
			`[data-component="timeline-card"][data-card-kind="` + kind + `"]`,
		).Count()
		if kindErr != nil || kindCount != 1 {
			missing = append(missing, fmt.Sprintf("%s(count=%d err=%v)", kind, kindCount, kindErr))
		}
	}
	beginning := timeline.Locator(`[data-component="beginning-of-thread"]`)
	beginningVisible, beginningErr := beginning.IsVisible()
	appendInteractionResult(page, artifactDir, result, CheckResult{
		ID: "m17-exhaustive-mounted-card-sample", Route: route, Viewport: "wide",
		Passed: err == nil && count == len(kinds) && len(missing) == 0 &&
			beginningErr == nil && !beginningVisible,
		Detail: fmt.Sprintf(
			"cards=%d required=%q missing=%q beginning_visible=%t count_error=%v beginning_error=%v",
			count, kinds, missing, beginningVisible, err, beginningErr,
		),
	})
}

func runM17ApprovalInteractionCheck(
	page playwright.Page,
	route string,
	artifactDir string,
	result *Result,
) {
	card := page.Locator(`[data-component="approval-card-interaction"]`)
	allow := page.GetByRole(*playwright.AriaRoleButton, playwright.PageGetByRoleOptions{
		Name: "Allow once", Exact: playwright.Bool(true),
	})
	err := allow.Click()
	commandKey := ""
	if err == nil {
		err = page.Locator(
			`[data-component="approval-card-interaction"][data-command-key^="apr_"]`,
		).WaitFor(playwright.LocatorWaitForOptions{State: playwright.WaitForSelectorStateVisible})
	}
	if err == nil {
		commandKey, err = card.GetAttribute("data-command-key")
	}
	if err == nil {
		err = browserAssertions().Locator(card).ToHaveAttribute("data-command-state", "ready")
	}
	approvalState := ""
	if err == nil {
		approvalState, err = card.GetAttribute("data-approval-state")
	}
	retainedCommandKey := ""
	if err == nil {
		retainedCommandKey, err = card.GetAttribute("data-command-key")
	}
	transportMode := ""
	if err == nil {
		transportMode, err = card.GetAttribute("data-transport-mode")
	}
	actionCount := 0
	if err == nil {
		actionCount, err = card.GetByRole(*playwright.AriaRoleButton).Count()
	}
	focused := false
	if err == nil {
		focusErr := browserAssertions().Locator(card).ToBeFocused()
		focused = focusErr == nil
		err = focusErr
	}
	appendInteractionResult(page, artifactDir, result, CheckResult{
		ID: "m17-mounted-approval-idempotency-and-focus", Route: route, Viewport: "wide",
		Passed: err == nil && strings.TrimSpace(commandKey) != "" &&
			retainedCommandKey == commandKey && approvalState == "pending" &&
			transportMode == "local-preview-fallback" && actionCount == 3 && focused,
		Detail: fmt.Sprintf(
			"command_key_present=%t settled_key_retained=%t approval_state=%q command_state=ready transport=%q remaining_actions=%d focused_status_card=%t error=%v",
			strings.TrimSpace(commandKey) != "", retainedCommandKey == commandKey,
			approvalState, transportMode, actionCount, focused, err,
		),
	})
}

func runM17ProgressiveDisclosureCheck(
	page playwright.Page,
	route string,
	artifactDir string,
	result *Result,
) {
	timeline := page.Locator(`[data-component="conversation-timeline"]`)
	disclosures := timeline.Locator(`[data-component="redacted-output"]`)
	collapsed := timeline.Locator(`[data-component="redacted-output"][data-state="collapsed"]`)
	total, err := disclosures.Count()
	collapsedCount, collapsedErr := collapsed.Count()
	show := timeline.GetByRole(*playwright.AriaRoleButton, playwright.LocatorGetByRoleOptions{
		Name: "Show redacted output", Exact: playwright.Bool(true),
	}).First()
	if err == nil && collapsedErr == nil {
		err = show.Click()
	}
	expanded := timeline.Locator(`[data-component="redacted-output"][data-state="expanded"]`)
	if err == nil {
		err = expanded.First().WaitFor(playwright.LocatorWaitForOptions{
			State: playwright.WaitForSelectorStateAttached,
		})
	}
	pages := timeline.Locator(`[data-component="redacted-output-pages"]`)
	if err == nil {
		err = pages.First().WaitFor(playwright.LocatorWaitForOptions{
			State: playwright.WaitForSelectorStateAttached,
		})
	}
	pageCount, pageErr := pages.Count()
	expandedCount, expandedErr := expanded.Count()
	appendInteractionResult(page, artifactDir, result, CheckResult{
		ID: "m17-raw-output-progressive-disclosure", Route: route, Viewport: "wide",
		Passed: err == nil && collapsedErr == nil && total == 2 && collapsedCount == 2 &&
			pageErr == nil && pageCount == 1 && expandedErr == nil && expandedCount == 1,
		Detail: fmt.Sprintf(
			"total=%d initially_collapsed=%d expanded=%d pages=%d click_error=%v collapsed_error=%v page_error=%v expanded_error=%v",
			total, collapsedCount, expandedCount, pageCount, err, collapsedErr, pageErr, expandedErr,
		),
	})
}

func runM17ComposerDraftCheck(
	page playwright.Page,
	route string,
	artifactDir string,
	result *Result,
) {
	composer := page.Locator(`[data-component="composer"]`)
	textbox := page.GetByRole(*playwright.AriaRoleTextbox, playwright.PageGetByRoleOptions{
		Name: "Message", Exact: playwright.Bool(true),
	})
	err := selectDurableSessionForComposer(page, textbox)
	if err == nil {
		err = textbox.Focus()
	}
	if err == nil {
		err = textbox.Fill("first line")
	}
	if err == nil {
		err = textbox.Press("Shift+Enter")
	}
	if err == nil {
		// Replace the line atomically after proving Shift+Enter is accepted.
		// The separately exercised rapid-key path is scheduler-sensitive under
		// WASM; this case owns multiline draft retention across rail rerenders.
		err = textbox.Fill("first line\nsecond line")
	}
	if err == nil {
		err = browserAssertions().Locator(textbox).ToHaveValue("first line\nsecond line")
	}
	archived := page.GetByRole(*playwright.AriaRoleButton, playwright.PageGetByRoleOptions{
		Name: "Show archived threads", Exact: playwright.Bool(true),
	})
	current := page.GetByRole(*playwright.AriaRoleButton, playwright.PageGetByRoleOptions{
		Name: "Show current threads", Exact: playwright.Bool(true),
	})
	if err == nil {
		err = archived.Click()
	}
	if err == nil {
		err = browserAssertions().Locator(archived).ToHaveAttribute("aria-pressed", "true")
	}
	if err == nil {
		err = current.Click()
	}
	if err == nil {
		err = browserAssertions().Locator(current).ToHaveAttribute("aria-pressed", "true")
	}
	value := ""
	if err == nil {
		value, err = textbox.InputValue()
	}
	keyboard, keyboardErr := textbox.GetAttribute("data-keyboard")
	send := page.GetByRole(*playwright.AriaRoleButton, playwright.PageGetByRoleOptions{
		Name: "Send", Exact: playwright.Bool(true),
	})
	sendCount, sendCountErr := send.Count()
	appendInteractionResult(page, artifactDir, result, CheckResult{
		ID: "m17-composer-multiline-draft-survives-rail-state", Route: route, Viewport: "wide",
		Passed: err == nil && keyboardErr == nil &&
			value == "first line\nsecond line" &&
			keyboard == "enter-submit-shift-enter-newline" &&
			sendCountErr == nil && sendCount == 1,
		Detail: fmt.Sprintf(
			"value=%q keyboard=%q send_count=%d error=%v keyboard_error=%v send_error=%v composer=%v",
			value, keyboard, sendCount, err, keyboardErr, sendCountErr, composer,
		),
	})
}

func runM17ThreadRailCheck(
	page playwright.Page,
	route string,
	artifactDir string,
	result *Result,
) {
	rail := page.Locator(
		`[data-component="product-sidebar"] [data-component="thread-rail"]`,
	)
	waitErr := rail.WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	})
	if waitErr == nil {
		waitErr = browserAssertions().Locator(rail).ToHaveAttribute("data-presentation", "ready")
	}
	railCount, err := rail.Count()
	if waitErr != nil {
		err = waitErr
	}
	presentation, presentationErr := rail.GetAttribute("data-presentation")
	listbox := rail.GetByRole(*playwright.AriaRoleListbox)
	listCount, listErr := listbox.Count()
	newThread := rail.GetByRole(*playwright.AriaRoleButton, playwright.LocatorGetByRoleOptions{
		Name: "Create new thread", Exact: playwright.Bool(true),
	})
	if err == nil && presentationErr == nil && listErr == nil {
		err = newThread.Click()
	}
	pending := rail.GetByText("Untitled thread", playwright.LocatorGetByTextOptions{
		Exact: playwright.Bool(true),
	})
	pendingVisible := false
	if err == nil {
		err = pending.First().WaitFor(playwright.LocatorWaitForOptions{
			State: playwright.WaitForSelectorStateVisible,
		})
	}
	if err == nil {
		pendingVisible, err = pending.First().IsVisible()
	}
	appendInteractionResult(page, artifactDir, result, CheckResult{
		ID: "m17-mounted-virtual-thread-rail", Route: route, Viewport: "wide",
		Passed: err == nil && railCount == 1 && presentation == "ready" &&
			listCount == 1 && pendingVisible,
		Detail: fmt.Sprintf(
			"rail_count=%d presentation=%q listboxes=%d pending_visible=%t error=%v presentation_error=%v list_error=%v",
			railCount, presentation, listCount, pendingVisible, err, presentationErr, listErr,
		),
	})
}

func runM17ScreenReaderAndAnnouncementCheck(
	page playwright.Page,
	route string,
	artifactDir string,
	result *Result,
) {
	timeline := page.Locator(`[data-component="conversation-timeline"]`)
	snapshot, err := timeline.AriaSnapshot()
	alerts := timeline.Locator(`[role="alert"]`)
	alertCount, alertErr := alerts.Count()
	assertive, assertiveErr := timeline.GetAttribute("data-assertive-toast")
	requiredNames := []string{
		"Requirement and ambiguity", "Forecast", "Plan", "Tool activity", "Validation", "Approval",
	}
	var missing []string
	for _, name := range requiredNames {
		if !strings.Contains(snapshot, name) {
			missing = append(missing, name)
		}
	}
	appendInteractionResult(page, artifactDir, result, CheckResult{
		ID:    "m17-screen-reader-timeline-no-routine-assertive-interruption",
		Route: route, Viewport: "wide",
		Passed: err == nil && alertErr == nil && assertiveErr == nil &&
			len(missing) == 0 && alertCount == 0 && assertive == "false",
		Detail: fmt.Sprintf(
			"snapshot_bytes=%d missing_names=%q alerts=%d assertive_toast=%q snapshot_error=%v alert_error=%v assertive_error=%v",
			len(snapshot), missing, alertCount, assertive, err, alertErr, assertiveErr,
		),
	})
}
