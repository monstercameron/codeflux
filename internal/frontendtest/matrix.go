// Package frontendtest defines the deterministic browser acceptance contract for
// the Codeflux frontend. It intentionally contains no browser asset source.
package frontendtest

import (
	"fmt"
	"slices"
	"strings"
)

const (
	PlanResponsive = "docs/plan.md §27A"
	PlanAcceptance = "docs/plan.md §27C"
)

// Viewport is a content-driven responsive test size.
type Viewport struct {
	Name   string `json:"name"`
	Mode   string `json:"mode"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// Viewports exercises every breakpoint declared by the M16 plan.
var Viewports = []Viewport{
	{Name: "wide", Mode: "wide", Width: 1600, Height: 1000},
	{Name: "standard", Mode: "standard", Width: 1280, Height: 900},
	{Name: "compact", Mode: "compact", Width: 960, Height: 900},
	{Name: "minimum", Mode: "minimum", Width: 720, Height: 900},
}

// Routes is the complete M16 frontend route contract.
var Routes = []string{
	"/",
	"/tasks",
	"/memory",
	"/settings",
	"/diagnostics",
	"/first-run",
}

// RouteHeadings are the direct-load identities for each route, taken from the
// H1 the product actually renders for a fresh, unauthenticated boot with no
// repository open and no thread selected — not from the plan or from an
// invented fixture. "/tasks" resolves to the unscoped ThreadWorkspace route
// (web/frontend/routes/routes.go's isUnscopedEntryPath), whose header
// (TaskWorkspaceHeader in web/frontend/shell/reference_chrome.go) falls back
// taskTitle to "No task yet" when no task is selected. "/memory" resolves to
// the unscoped Memory route, whose header (memoryHeader in
// web/frontend/shell/memory_workspace.go) always renders the literal H1 text
// "Project memory" regardless of load state.
var RouteHeadings = map[string]string{
	"/":            "Welcome to CodeFlux",
	"/tasks":       "No task yet",
	"/memory":      "Project memory",
	"/settings":    "Settings",
	"/diagnostics": "Diagnostics",
	"/first-run":   "Welcome to CodeFlux",
}

// BootstrapStates is the complete frontend data-owner state vocabulary from the
// plan. Ready data and ready empty remain distinct so empty UI cannot masquerade
// as a loading or failure state.
var BootstrapStates = []string{
	"not-requested",
	"loading",
	"ready-data",
	"ready-empty",
	"partial-stale",
	"recoverable-error",
	"denied",
	"incompatible",
	"disconnected",
}

// AcceptanceCase is one independently reportable M16 browser contract.
type AcceptanceCase struct {
	ID           string   `json:"id"`
	Kind         string   `json:"kind"`
	Route        string   `json:"route,omitempty"`
	Viewport     string   `json:"viewport,omitempty"`
	Bootstrap    string   `json:"bootstrap,omitempty"`
	Todos        []string `json:"todos"`
	Gates        []string `json:"gates"`
	PlanSections []string `json:"plan_sections"`
	Adversary    string   `json:"adversary"`
	Expected     string   `json:"expected"`
}

var requiredTodos = []string{
	"M16-046", "M16-047", "M16-048", "M16-049", "M16-050",
	"M16-051", "M16-052", "M16-053", "M16-054", "M16-055",
	"M16-056", "M16-057", "M16-058", "M16-059", "M16-060",
	"M16-061", "M16-062", "M16-063", "M16-064", "M16-065",
	"M16-071", "M16-072", "M16-073", "M16-074", "M16-092",
	"M16-093", "M16-094", "M16-095", "M16-096", "M16-097",
	"M16-098", "M16-099", "M16-100",
}

var requiredGates = []string{"G01", "G02", "G03", "G04", "G05"}

// AcceptanceMatrix returns a stable, exhaustive matrix. The route/state/viewport
// cross-product catches ownership-state omissions; focused adversarial cases
// exercise interactions that a static screenshot cannot establish.
func AcceptanceMatrix() []AcceptanceCase {
	cases := make([]AcceptanceCase, 0, len(Routes)*len(BootstrapStates)*len(Viewports)+8)
	for _, route := range Routes {
		for _, state := range BootstrapStates {
			for _, viewport := range Viewports {
				cases = append(cases, AcceptanceCase{
					ID:           matrixID(route, state, viewport.Name),
					Kind:         "route-bootstrap-viewport",
					Route:        route,
					Viewport:     viewport.Name,
					Bootstrap:    state,
					Todos:        []string{"M16-046", "M16-047", "M16-048", "M16-049", "M16-050", "M16-051", "M16-052", "M16-053", "M16-092", "M16-094", "M16-095", "M16-096", "M16-097", "M16-098"},
					Gates:        []string{"G03", "G05"},
					PlanSections: []string{PlanResponsive, PlanAcceptance},
					Adversary:    "reload the route directly while the data owner is in the declared bootstrap state",
					Expected:     "the route preserves explicit state, selected context, draft safety, and zero horizontal overflow",
				})
			}
		}
	}

	cases = append(cases,
		focusedCase(
			"keyboard-full-shell",
			"keyboard",
			requiredRange("M16", 54, 65),
			[]string{"G02", "G05"},
			"reverse and repeat Tab traversal, activate every control with the keyboard, then type shortcut characters in the composer",
			"focus is always visible, order is logical, typing suppresses global shortcuts, and focus returns after every overlay closes",
		),
		focusedCase(
			"responsive-selection-thrash",
			"responsive",
			requiredRange("M16", 46, 53),
			[]string{"G03", "G05"},
			"select a graph node and alternate 1439/1440, 1179/1180, and 799/800 widths without debounce pauses",
			"layout mode changes at content breakpoints without losing graph selection, conversation context, or draft",
		),
		focusedCase(
			"software-keyboard-composer",
			"responsive",
			[]string{"M16-050", "M16-052", "M16-053"},
			[]string{"G03"},
			"focus the composer at minimum width while reducing viewport height to emulate an on-screen keyboard",
			"composer remains reachable above the reduced viewport with no horizontal overflow",
		),
		focusedCase(
			"focus-restoration-stack",
			"focus",
			[]string{"M16-054", "M16-055", "M16-056", "M16-057", "M16-058", "M16-093"},
			[]string{"G02", "G05"},
			"open and close dialogs, drawers, the thread rail, and graph detail in nested and repeated order",
			"focus is trapped where required and restored to the exact invoking control",
		),
		focusedCase(
			"render-isolation-burst",
			"render-isolation",
			requiredRange("M16", 71, 74),
			[]string{"G04", "G05"},
			"interleave cost updates, chat appends, and graph selections in a rapid burst",
			"only the owning subtree render counters advance for each event",
		),
		focusedCase(
			"network-host-confusion",
			"network",
			[]string{"M16-096", "M16-099"},
			[]string{"G01", "G05"},
			"attempt redirects, subdomains, alternate ports, websocket upgrades, and userinfo-shaped URLs",
			"only the exact configured loopback origin is allowed and every attempted external request fails the suite",
		),
		focusedCase(
			"primitive-state-modes",
			"component",
			[]string{"M16-094", "M16-097", "M16-098"},
			[]string{"G05"},
			"render every primitive in loading, empty, stale, recoverable error, denied, incompatible, and disconnected modes",
			"each mode is explicit, semantically named, and cannot trigger an unsupported durable transition",
		),
		focusedCase(
			"terminology-drift",
			"content",
			[]string{"M16-100"},
			[]string{"G05"},
			"scan landmarks, controls, dialogs, empty states, and error states for competing product terms",
			"the plan's canonical terminology remains consistent across visible and accessible names",
		),
	)
	return cases
}

func focusedCase(
	id string,
	kind string,
	todos []string,
	gates []string,
	adversary string,
	expected string,
) AcceptanceCase {
	return AcceptanceCase{
		ID:           id,
		Kind:         kind,
		Todos:        todos,
		Gates:        gates,
		PlanSections: []string{PlanResponsive, PlanAcceptance},
		Adversary:    adversary,
		Expected:     expected,
	}
}

func requiredRange(prefix string, start, end int) []string {
	result := make([]string, 0, end-start+1)
	for value := start; value <= end; value++ {
		result = append(result, fmt.Sprintf("%s-%03d", prefix, value))
	}
	return result
}

func matrixID(route, state, viewport string) string {
	route = strings.Trim(route, "/")
	if route == "" {
		route = "home"
	}
	replacer := strings.NewReplacer("/", "-", "{", "", "}", "")
	return "route-" + replacer.Replace(route) + "-" + state + "-" + viewport
}

// ValidateAcceptanceMatrix rejects incomplete or ambiguous acceptance contracts.
func ValidateAcceptanceMatrix(cases []AcceptanceCase) error {
	if len(cases) == 0 {
		return fmt.Errorf("acceptance matrix is empty")
	}
	ids := make(map[string]struct{}, len(cases))
	todos := make(map[string]struct{})
	gates := make(map[string]struct{})
	for index, testCase := range cases {
		if strings.TrimSpace(testCase.ID) == "" {
			return fmt.Errorf("case %d has no id", index)
		}
		if _, duplicate := ids[testCase.ID]; duplicate {
			return fmt.Errorf("duplicate case id %q", testCase.ID)
		}
		ids[testCase.ID] = struct{}{}
		if strings.TrimSpace(testCase.Adversary) == "" || strings.TrimSpace(testCase.Expected) == "" {
			return fmt.Errorf("case %q lacks adversary or expected outcome", testCase.ID)
		}
		if !slices.Contains(testCase.PlanSections, PlanResponsive) ||
			!slices.Contains(testCase.PlanSections, PlanAcceptance) {
			return fmt.Errorf("case %q must reference %s and %s", testCase.ID, PlanResponsive, PlanAcceptance)
		}
		for _, todo := range testCase.Todos {
			todos[todo] = struct{}{}
		}
		for _, gate := range testCase.Gates {
			gates[gate] = struct{}{}
		}
	}
	for _, todo := range requiredTodos {
		if _, ok := todos[todo]; !ok {
			return fmt.Errorf("acceptance matrix omits %s", todo)
		}
	}
	for _, gate := range requiredGates {
		if _, ok := gates[gate]; !ok {
			return fmt.Errorf("acceptance matrix omits %s", gate)
		}
	}
	return nil
}
