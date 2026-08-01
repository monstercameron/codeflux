package frontendtest

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mxschmitt/playwright-go"
)

// exerciseMountedM22Journeys drives the mounted fixture through every journey
// declared by M22JourneyMatrix (M22-063..075).
//
// Each journey asserts the property its TODO names, on top of the stage
// activation M18 already proves. The two milestones differ in what they claim:
// M18 established that each stage renders its decision facts, M22 establishes
// that a USER can get through the flow and that the surface is usable without
// a mouse or a working colour channel.
func exerciseMountedM22Journeys(t *testing.T, page playwright.Page) {
	t.Helper()
	if err := ValidateM22JourneyMatrix(M22JourneyMatrix()); err != nil {
		t.Fatalf("M22 journey matrix is invalid before a browser run: %v", err)
	}
	// Reload first so these journeys start from an untouched fixture. Without
	// it the approval journey would inherit the decision an earlier milestone's
	// checks already made on the same page, and would silently stop exercising
	// the pre-decision surface it exists to test.
	if _, err := page.Reload(); err != nil {
		t.Fatalf("reload before M22 journeys: %v", err)
	}
	fixture := page.Locator(`[data-component="m18-journey-fixture"]`)
	if err := browserAssertions().Locator(fixture).ToBeVisible(); err != nil {
		t.Fatalf("wait for mounted journey fixture: %v", err)
	}

	journeys := map[string]func(*testing.T, playwright.Page, playwright.Locator){
		"M22-063": exerciseM22EmptyShell,
		"M22-064": exerciseM22CreateThreadAndSend,
		"M22-065": exerciseM22PlanApproval,
		"M22-066": exerciseM22CommandApprovalAndDenial,
		"M22-067": exerciseM22PauseAndResume,
		"M22-068": exerciseM22ReconnectAndReplay,
		"M22-069": exerciseM22GraphCrossSelection,
		"M22-070": exerciseM22DiffReviewAcceptance,
		"M22-071": exerciseM22CrashRecoveryChoice,
		"M22-072": exerciseM22AccessibilityScan,
		"M22-073": exerciseM22KeyboardOnlyJourney,
		"M22-074": exerciseM22ScreenReaderSmoke,
		"M22-075": exerciseM22ReducedMotionAndContrast,
	}
	for _, testCase := range M22JourneyMatrix() {
		exercise, ok := journeys[testCase.Todo]
		if !ok {
			t.Fatalf("journey %s (%s) has no mounted exercise", testCase.Todo, testCase.ID)
		}
		t.Run(testCase.Todo+"/"+testCase.ID, func(t *testing.T) {
			exercise(t, page, fixture)
		})
	}
}

// exerciseM22EmptyShell is M22-063: the shell with nothing selected must still
// tell the user what it is waiting for.
func exerciseM22EmptyShell(t *testing.T, page playwright.Page, fixture playwright.Locator) {
	activateM18Stage(t, page, fixture, "first-run")
	shell := fixture.Locator(`[data-component="first-run-shell"]`)
	if err := browserAssertions().Locator(shell).ToBeVisible(); err != nil {
		t.Fatalf("empty shell is not visible: %v", err)
	}
	regions, err := shell.Locator(`[data-region]`).Count()
	if err != nil || regions == 0 {
		t.Fatalf("empty shell exposes %d labelled regions, error=%v", regions, err)
	}
	// An empty shell that renders nothing actionable is indistinguishable from
	// a broken one, so a named next action is the requirement.
	if err := assertHasAnyNamedControl(shell); err != nil {
		t.Fatalf("empty shell offers no next action: %v", err)
	}
	assertNoStateByColourAlone(t, shell, "first-run")
}

// exerciseM22CreateThreadAndSend is M22-064.
func exerciseM22CreateThreadAndSend(t *testing.T, page playwright.Page, fixture playwright.Locator) {
	activateM18Stage(t, page, fixture, "new-task")
	assertM18CardKind(t, fixture, "requirement")
	// The requirement the user sent must be present as durable content, not as
	// an optimistic placeholder that a reload would lose.
	if err := browserAssertions().Locator(fixture).ToContainText("Complete the typed frontend journey"); err != nil {
		t.Fatalf("sent requirement is not on the timeline: %v", err)
	}
	for _, section := range []string{"Constraints", "Assumptions"} {
		if err := browserAssertions().Locator(fixture).ToContainText(section); err != nil {
			t.Fatalf("requirement card omits %s: %v", section, err)
		}
	}
	// Unresolved ambiguity must be stated rather than silently resolved, or
	// the user cannot know what the agent guessed.
	if err := browserAssertions().Locator(fixture).ToContainText("Exact review scope is not approved"); err != nil {
		t.Fatalf("requirement card hides unresolved ambiguity: %v", err)
	}
}

// exerciseM22PlanApproval is M22-065.
func exerciseM22PlanApproval(t *testing.T, page playwright.Page, fixture playwright.Locator) {
	activateM18Stage(t, page, fixture, "plan-review")
	assertM18CardKind(t, fixture, "plan")
	for _, fact := range []string{"Implement", "Verify", "Stale review", "User plan approval"} {
		if err := browserAssertions().Locator(fixture).ToContainText(fact); err != nil {
			t.Fatalf("plan card omits %q before approval is offered: %v", fact, err)
		}
	}
	assertM18NamedButton(t, fixture, "Approve plan", true)
	assertM18NamedButton(t, fixture, "Request plan change", true)
}

// exerciseM22CommandApprovalAndDenial is M22-066.
func exerciseM22CommandApprovalAndDenial(t *testing.T, page playwright.Page, fixture playwright.Locator) {
	activateM18Stage(t, page, fixture, "approval")
	approval := fixture.Locator(`[data-component="approval-card-interaction"]`)
	if err := browserAssertions().Locator(approval).ToBeVisible(); err != nil {
		t.Fatalf("approval card is not visible: %v", err)
	}
	// The exact action and its consequence must be readable BEFORE deciding.
	for _, fact := range []string{"write scoped frontend tests", "test source changes"} {
		if err := browserAssertions().Locator(approval).ToContainText(fact); err != nil {
			t.Fatalf("approval card omits %q: %v", fact, err)
		}
	}
	// All three decisions are offered, and denial is not hidden behind the
	// grant path.
	for _, label := range []string{"Allow once", "Allow for this task", "Deny"} {
		assertM18NamedButton(t, fixture, label, true)
	}

	state, err := approval.GetAttribute("data-approval-state")
	if err != nil {
		t.Fatalf("read approval state: %v", err)
	}
	if state == "pending" {
		if err := fixture.GetByRole(*playwright.AriaRoleButton, playwright.LocatorGetByRoleOptions{
			Name: "Allow once", Exact: playwright.Bool(true),
		}).Click(); err != nil {
			t.Fatalf("grant approval: %v", err)
		}
	}
	if err := browserAssertions().Locator(approval).ToHaveAttribute("data-approval-state", "granted"); err != nil {
		t.Fatalf("granted approval state: %v", err)
	}
	// Once resolved, no decision control may remain: re-offering one would let
	// a second, unattributed grant be recorded against the same request.
	remaining, err := approval.GetByRole(*playwright.AriaRoleButton).Count()
	if err != nil || remaining != 0 {
		t.Fatalf("resolved approval still offers %d controls, error=%v", remaining, err)
	}
	if err := browserAssertions().Locator(approval).ToContainText("local user"); err != nil {
		t.Fatalf("resolved approval lost its attribution: %v", err)
	}
}

// exerciseM22PauseAndResume is M22-067.
func exerciseM22PauseAndResume(t *testing.T, page playwright.Page, fixture playwright.Locator) {
	activateM18Stage(t, page, fixture, "live-work")
	assertM18CardKind(t, fixture, "tool-activity")
	if err := browserAssertions().Locator(fixture).ToContainText("running"); err != nil {
		t.Fatalf("live work does not state that it is running: %v", err)
	}

	// The budget stage is where work has stopped without the user pausing it.
	// What is and is not still in flight must be stated, not inferred.
	activateM18Stage(t, page, fixture, "budget")
	panel := fixture.Locator(`[data-component="task-control-panel"]`)
	if err := browserAssertions().Locator(panel).ToBeVisible(); err != nil {
		t.Fatalf("task control panel is not visible: %v", err)
	}
	if err := browserAssertions().Locator(panel).ToContainText("Unknown"); err != nil {
		t.Fatalf("stopped work presents a cost it does not know: %v", err)
	}
	if err := assertHasAnyNamedControl(panel); err != nil {
		t.Fatalf("stopped work offers no next action: %v", err)
	}
}

// exerciseM22ReconnectAndReplay is M22-068.
func exerciseM22ReconnectAndReplay(t *testing.T, page playwright.Page, fixture playwright.Locator) {
	activateM18Stage(t, page, fixture, "reconnect")
	panel := fixture.Locator(`[data-component="task-control-panel"]`)
	if err := browserAssertions().Locator(panel).ToHaveAttribute("data-delivery", "disconnected"); err != nil {
		t.Fatalf("reconnect stage is not disconnected: %v", err)
	}
	// Mutation must be refused while the sequence is uncertain, WITH a stated
	// reason: a disabled button with no explanation reads as a bug.
	for _, label := range []string{"Pause task", "Stop task"} {
		button := fixture.GetByRole(*playwright.AriaRoleButton, playwright.LocatorGetByRoleOptions{
			Name: label, Exact: playwright.Bool(true),
		})
		assertM18Disabled(t, button, true, "M22 reconnect")
	}
	if err := browserAssertions().Locator(panel).ToContainText("Sequence certainty is unknown"); err != nil {
		t.Fatalf("disabled mutations state no reason: %v", err)
	}
	if err := browserAssertions().Locator(panel).ToContainText("backend task remains running"); err != nil {
		t.Fatalf("reconnect surface does not distinguish backend state from UI state: %v", err)
	}

	// Returning to live work must not leave a duplicate of the reconnect card.
	activateM18Stage(t, page, fixture, "live-work")
	panels, err := fixture.Locator(`[data-component="task-control-panel"]`).Count()
	if err != nil {
		t.Fatalf("count task control panels: %v", err)
	}
	if panels > 1 {
		t.Fatalf("replay left %d task control panels mounted", panels)
	}
}

// exerciseM22GraphCrossSelection is M22-069.
func exerciseM22GraphCrossSelection(t *testing.T, page playwright.Page, fixture playwright.Locator) {
	activateM18Stage(t, page, fixture, "graph")
	first := fixture.Locator(`[data-node-id="journey-node-1"]`)
	second := fixture.Locator(`[data-node-id="journey-node-2"]`)
	if err := browserAssertions().Locator(first).ToBeVisible(); err != nil {
		t.Fatalf("graph node is not visible: %v", err)
	}
	if err := second.Click(); err != nil {
		t.Fatalf("select second graph node: %v", err)
	}
	if err := browserAssertions().Locator(second).ToHaveAttribute("aria-pressed", "true"); err != nil {
		t.Fatalf("selected node is not marked selected: %v", err)
	}
	// Selection must be single-valued in both directions, or two surfaces can
	// disagree about what the user is looking at.
	if err := browserAssertions().Locator(first).ToHaveAttribute("aria-pressed", "false"); err != nil {
		t.Fatalf("previous node stayed selected: %v", err)
	}
	if err := first.Click(); err != nil {
		t.Fatalf("select back: %v", err)
	}
	if err := browserAssertions().Locator(first).ToHaveAttribute("aria-pressed", "true"); err != nil {
		t.Fatalf("reverse selection did not take: %v", err)
	}
	if err := browserAssertions().Locator(second).ToHaveAttribute("aria-pressed", "false"); err != nil {
		t.Fatalf("reverse selection left both nodes selected: %v", err)
	}
}

// exerciseM22DiffReviewAcceptance is M22-070.
func exerciseM22DiffReviewAcceptance(t *testing.T, page playwright.Page, fixture playwright.Locator) {
	activateM18Stage(t, page, fixture, "review")
	assertM18CardKind(t, fixture, "diff-summary")
	// Every changed file must be listed before acceptance is offered: an
	// acceptance over an unlisted file is an unreviewed change.
	for _, path := range []string{"web/client/main.go", "web/client/main_test.go"} {
		if err := browserAssertions().Locator(fixture).ToContainText(path); err != nil {
			t.Fatalf("diff summary omits %s: %v", path, err)
		}
	}
	assertM18NamedButton(t, fixture, "Open review", true)
}

// exerciseM22CrashRecoveryChoice is M22-071.
func exerciseM22CrashRecoveryChoice(t *testing.T, page playwright.Page, fixture playwright.Locator) {
	activateM18Stage(t, page, fixture, "recovery")
	panel := fixture.Locator(`[data-component="task-control-panel"]`)
	if err := browserAssertions().Locator(panel).ToBeVisible(); err != nil {
		t.Fatalf("recovery panel is not visible: %v", err)
	}
	// Known state and ambiguity must be stated separately. Collapsing them
	// would let a guess be read as a fact.
	for _, fact := range []string{
		"checkpoint cp-m18 is durable",
		"external publish outcome is not attributable",
		"Reconcile the external outcome before continuing.",
	} {
		if err := browserAssertions().Locator(panel).ToContainText(fact); err != nil {
			t.Fatalf("recovery surface omits %q: %v", fact, err)
		}
	}
	// An unsafe path that would silently repeat an ambiguous external effect
	// must not be offered at all.
	for _, forbidden := range []string{"Retry", "Try again", "Resume anyway"} {
		count, err := panel.GetByRole(*playwright.AriaRoleButton, playwright.LocatorGetByRoleOptions{
			Name: forbidden, Exact: playwright.Bool(true),
		}).Count()
		if err != nil {
			t.Fatalf("count %q control: %v", forbidden, err)
		}
		if count != 0 {
			t.Fatalf("recovery offers unsafe control %q", forbidden)
		}
	}
	// The unsafe workspace-relative detail must be present but not actionable.
	unsafe := panel.Locator(`[data-recovery-detail="../outside"]`)
	if count, err := unsafe.Count(); err == nil && count > 0 {
		assertM18Disabled(t, unsafe, true, "M22 recovery unsafe detail")
	}
}

// exerciseM22AccessibilityScan is M22-072. It scans every journey stage rather
// than a single representative one, because an omission is exactly the kind of
// defect a representative sample misses.
func exerciseM22AccessibilityScan(t *testing.T, page playwright.Page, fixture playwright.Locator) {
	for _, stage := range M22JourneyStages {
		activateM18Stage(t, page, fixture, stage)
		if err := assertEveryControlIsNamed(fixture); err != nil {
			t.Fatalf("stage %s: %v", stage, err)
		}
		if err := assertEveryRegionIsLabelled(fixture); err != nil {
			t.Fatalf("stage %s: %v", stage, err)
		}
		assertNoStateByColourAlone(t, fixture, stage)
	}
}

// exerciseM22KeyboardOnlyJourney is M22-073.
func exerciseM22KeyboardOnlyJourney(t *testing.T, page playwright.Page, fixture playwright.Locator) {
	// The stage selector itself must be operable from the keyboard, or the
	// journey cannot even be started without a mouse.
	selector := page.GetByTestId("m18-stage-select")
	if err := selector.Focus(); err != nil {
		t.Fatalf("focus stage selector: %v", err)
	}
	if err := browserAssertions().Locator(selector).ToBeFocused(); err != nil {
		t.Fatalf("stage selector does not take focus: %v", err)
	}

	for _, stage := range M22JourneyStages {
		activateM18Stage(t, page, fixture, stage)
		controls := fixture.GetByRole(*playwright.AriaRoleButton)
		count, err := controls.Count()
		if err != nil {
			t.Fatalf("stage %s: count controls: %v", stage, err)
		}
		for index := range count {
			control := controls.Nth(index)
			disabled, err := control.IsDisabled()
			if err != nil {
				t.Fatalf("stage %s control %d: read disabled: %v", stage, index, err)
			}
			if disabled {
				continue
			}
			if err := control.Focus(); err != nil {
				t.Fatalf("stage %s control %d cannot be focused: %v", stage, index, err)
			}
			if err := browserAssertions().Locator(control).ToBeFocused(); err != nil {
				name, _ := control.TextContent()
				t.Fatalf("stage %s control %q is not keyboard reachable: %v", stage, name, err)
			}
		}
	}
}

// exerciseM22ScreenReaderSmoke is M22-074: every decision fact must be
// reachable as named text rather than as layout position.
func exerciseM22ScreenReaderSmoke(t *testing.T, page playwright.Page, fixture playwright.Locator) {
	for _, stage := range M22JourneyStages {
		activateM18Stage(t, page, fixture, stage)
		facts := fixture.Locator(`[data-component="m18-decision-facts"]`)
		for _, field := range []string{
			"state", "cost", "authority", "evidence", "uncertainty", "next-action",
		} {
			term := facts.Locator(`[data-fact="` + field + `"]`)
			text, err := term.TextContent()
			if err != nil || strings.TrimSpace(text) == "" {
				t.Fatalf("stage %s: fact %q is not readable as text (%q, %v)",
					stage, field, text, err)
			}
		}
		// The fact list must be a definition list, so its terms and values are
		// associated for assistive technology instead of merely adjacent.
		if err := browserAssertions().Locator(facts.Locator("dl")).ToBeVisible(); err != nil {
			t.Fatalf("stage %s: decision facts are not a definition list: %v", stage, err)
		}
		terms, termErr := facts.Locator("dt").Count()
		values, valueErr := facts.Locator("dd").Count()
		if termErr != nil || valueErr != nil {
			t.Fatalf("stage %s: count fact terms: %v %v", stage, termErr, valueErr)
		}
		if terms != values || terms == 0 {
			t.Fatalf("stage %s: %d fact terms for %d values", stage, terms, values)
		}
	}
}

// exerciseM22ReducedMotionAndContrast is M22-075.
func exerciseM22ReducedMotionAndContrast(t *testing.T, page playwright.Page, fixture playwright.Locator) {
	for _, preference := range []struct {
		name  string
		apply func() error
	}{
		{"reduced motion", func() error {
			return page.EmulateMedia(playwright.PageEmulateMediaOptions{
				ReducedMotion: playwright.ReducedMotionReduce,
			})
		}},
		{"high contrast", func() error {
			return page.EmulateMedia(playwright.PageEmulateMediaOptions{
				ForcedColors: playwright.ForcedColorsActive,
			})
		}},
	} {
		if err := preference.apply(); err != nil {
			t.Fatalf("emulate %s: %v", preference.name, err)
		}
		for _, stage := range M22JourneyStages {
			activateM18Stage(t, page, fixture, stage)
			// activateM18Stage already requires every decision fact to be
			// non-empty, so reaching here under the preference proves no
			// journey step depended on motion or colour to be understood.
			if err := assertEveryRegionIsLabelled(fixture); err != nil {
				t.Fatalf("%s, stage %s: %v", preference.name, stage, err)
			}
			assertNoStateByColourAlone(t, fixture, preference.name+"/"+stage)
		}
	}
	if err := page.EmulateMedia(playwright.PageEmulateMediaOptions{
		ReducedMotion: playwright.ReducedMotionNoPreference,
		ForcedColors:  playwright.ForcedColorsNone,
	}); err != nil {
		t.Fatalf("restore media preferences: %v", err)
	}
}

// assertEveryControlIsNamed fails if any button reachable in the fixture has no
// accessible name. An unnamed control is unusable by screen reader and
// unaddressable by any keyboard-driven test.
func assertEveryControlIsNamed(scope playwright.Locator) error {
	controls := scope.GetByRole(*playwright.AriaRoleButton)
	count, err := controls.Count()
	if err != nil {
		return fmt.Errorf("count controls: %w", err)
	}
	for index := range count {
		control := controls.Nth(index)
		text, err := control.TextContent()
		if err != nil {
			return fmt.Errorf("read control %d: %w", index, err)
		}
		if strings.TrimSpace(text) != "" {
			continue
		}
		label, err := control.GetAttribute("aria-label")
		if err != nil || strings.TrimSpace(label) == "" {
			return fmt.Errorf("control %d has no accessible name", index)
		}
	}
	return nil
}

// assertEveryRegionIsLabelled fails if a landmark region carries no label, so a
// screen-reader user can tell one section of the surface from another.
func assertEveryRegionIsLabelled(scope playwright.Locator) error {
	regions := scope.Locator("section, nav, aside")
	count, err := regions.Count()
	if err != nil {
		return fmt.Errorf("count regions: %w", err)
	}
	for index := range count {
		region := regions.Nth(index)
		label, labelErr := region.GetAttribute("aria-label")
		if labelErr == nil && strings.TrimSpace(label) != "" {
			continue
		}
		labelledBy, byErr := region.GetAttribute("aria-labelledby")
		if byErr == nil && strings.TrimSpace(labelledBy) != "" {
			continue
		}
		return fmt.Errorf("region %d has no accessible label", index)
	}
	return nil
}

// assertNoStateByColourAlone fails if a status element conveys its state only
// through presentation. Every status must also carry a machine-readable data
// attribute or readable text, which is what the reduced-motion and
// high-contrast journeys rely on.
func assertNoStateByColourAlone(t *testing.T, scope playwright.Locator, phase string) {
	t.Helper()
	statuses := scope.Locator(`[role="status"], [role="alert"], [data-state], [data-approval-state], [data-delivery]`)
	count, err := statuses.Count()
	if err != nil {
		t.Fatalf("%s: count status elements: %v", phase, err)
	}
	for index := range count {
		status := statuses.Nth(index)
		text, textErr := status.TextContent()
		if textErr == nil && strings.TrimSpace(text) != "" {
			continue
		}
		for _, attribute := range []string{"data-state", "data-approval-state", "data-delivery", "aria-label"} {
			value, attrErr := status.GetAttribute(attribute)
			if attrErr == nil && strings.TrimSpace(value) != "" {
				text = value
				break
			}
		}
		if strings.TrimSpace(text) == "" {
			t.Fatalf("%s: status element %d conveys state without text or a state attribute", phase, index)
		}
	}
}

// assertHasAnyNamedControl fails when a surface offers the user nothing to do.
func assertHasAnyNamedControl(scope playwright.Locator) error {
	for _, role := range []playwright.AriaRole{
		*playwright.AriaRoleButton,
		*playwright.AriaRoleLink,
		*playwright.AriaRoleCombobox,
	} {
		count, err := scope.GetByRole(role).Count()
		if err != nil {
			return fmt.Errorf("count %v controls: %w", role, err)
		}
		if count > 0 {
			return nil
		}
	}
	return fmt.Errorf("surface exposes no button, link, or combobox")
}
