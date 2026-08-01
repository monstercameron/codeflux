package testharness

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"codeflux.dev/codeflux/internal/testfixtures"
)

// BrowserScenario is one named browser flow a test drives (M22-111).
//
// The harness is deliberately transport-shaped rather than Playwright-shaped:
// it owns bootstrap, event delivery, keyboard actions, accessibility checks,
// and failure artifacts, and it can be exercised without a browser present.
// A harness that only worked with Chromium installed would be untested on
// every machine that lacks it, which is most of them.
type BrowserScenario struct {
	Name string
	// Bootstrap is the session state the page starts from.
	Bootstrap SessionBootstrap
	// Events are delivered in order once the page has mounted.
	Events []ScenarioEvent
	// Keys are the keyboard actions driven after the events land.
	Keys []KeyAction
}

// SessionBootstrap is the state a scenario's page begins in.
type SessionBootstrap struct {
	// ConnectionState is one of the seven declared connection projections.
	ConnectionState string
	// SelectedThread and SelectedTask may be empty, which is the empty-shell
	// case and is a state the UI must handle rather than an omission.
	SelectedThread string
	SelectedTask   string
	// Route is the path the page loads.
	Route string
}

// Validate rejects a bootstrap the frontend could not actually be handed.
func (bootstrap SessionBootstrap) Validate() error {
	valid := map[string]bool{
		"connecting": true, "live": true, "replaying": true, "degraded": true,
		"disconnected": true, "incompatible": true, "unauthorized": true,
	}
	if !valid[bootstrap.ConnectionState] {
		return fmt.Errorf("connection state %q is not one of the seven declared projections",
			bootstrap.ConnectionState)
	}
	if !strings.HasPrefix(bootstrap.Route, "/") {
		return fmt.Errorf("route %q must be absolute", bootstrap.Route)
	}
	if bootstrap.SelectedTask != "" && bootstrap.SelectedThread == "" {
		return errors.New("a selected task requires the thread it belongs to")
	}
	return nil
}

// ScenarioEvent is one event delivered to the page during a scenario.
type ScenarioEvent struct {
	Sequence uint64
	Kind     string
	// Duplicate marks an event deliberately delivered twice, so a scenario can
	// exercise the deduplication path rather than only the happy one.
	Duplicate bool
}

// KeyAction is one keyboard interaction.
type KeyAction struct {
	// Key is the key name, e.g. "Tab", "Enter", "Escape".
	Key string
	// Repeat drives the same key more than once, for traversal.
	Repeat int
	// ExpectFocusTestID names the element focus must land on afterwards. Empty
	// means the scenario does not assert focus for this action.
	ExpectFocusTestID string
}

// Validate rejects an unusable key action.
func (action KeyAction) Validate() error {
	if strings.TrimSpace(action.Key) == "" {
		return errors.New("a key action requires a key")
	}
	if action.Repeat < 0 {
		return fmt.Errorf("key %q has a negative repeat", action.Key)
	}
	return nil
}

// Validate rejects a scenario that could not be run as described.
func (scenario BrowserScenario) Validate() error {
	if strings.TrimSpace(scenario.Name) == "" {
		return errors.New("a browser scenario requires a name")
	}
	if err := scenario.Bootstrap.Validate(); err != nil {
		return fmt.Errorf("scenario %q bootstrap: %w", scenario.Name, err)
	}
	seen := map[uint64]int{}
	for index, event := range scenario.Events {
		if event.Sequence == 0 {
			return fmt.Errorf("scenario %q event %d has no sequence", scenario.Name, index)
		}
		if strings.TrimSpace(event.Kind) == "" {
			return fmt.Errorf("scenario %q event %d has no kind", scenario.Name, index)
		}
		seen[event.Sequence]++
		if seen[event.Sequence] > 1 && !event.Duplicate {
			return fmt.Errorf(
				"scenario %q repeats sequence %d without marking it a duplicate; "+
					"an accidental repeat and a deliberate one test different things",
				scenario.Name, event.Sequence)
		}
	}
	for index, action := range scenario.Keys {
		if err := action.Validate(); err != nil {
			return fmt.Errorf("scenario %q key %d: %w", scenario.Name, index, err)
		}
	}
	return nil
}

// AccessibilityFinding is one violation observed during a scenario.
type AccessibilityFinding struct {
	Rule    string
	Element string
	Detail  string
}

// AccessibilityRules are the checks every scenario runs (M22-111).
//
// The list is short and specific on purpose: a rule nobody can act on is a
// rule everyone learns to ignore.
func AccessibilityRules() []string {
	return []string{
		"control-has-accessible-name",
		"region-has-label",
		"state-not-conveyed-by-color-alone",
		"focus-is-visible",
		"focus-order-follows-layout",
	}
}

// ScenarioResult is what a scenario run produced.
type ScenarioResult struct {
	Scenario           string
	EventsDelivered    int
	DuplicatesFiltered int
	KeysDriven         int
	Findings           []AccessibilityFinding
	ArtifactPaths      []string
	Failed             bool
}

// BrowserHarness runs scenarios and owns their failure artifacts (M22-111).
type BrowserHarness struct {
	mutex        sync.Mutex
	artifactRoot string
	preserve     bool
	results      []ScenarioResult
	// capture is the screenshot hook. It is injected so the harness can be
	// exercised without a browser: the contract under test is "an artifact is
	// written on failure and only on failure", not "Chromium works".
	capture func(scenario string, path string) error
}

// BrowserHarnessOptions configures a harness.
type BrowserHarnessOptions struct {
	// ArtifactRoot is where failure artifacts are written. It must be safely
	// cleanable.
	ArtifactRoot string
	// PreserveArtifacts keeps artifacts after Close.
	PreserveArtifacts bool
	// Capture writes a screenshot for a failing scenario. When nil, a
	// placeholder artifact is written instead, so the failure path is still
	// exercised on machines with no browser.
	Capture func(scenario string, path string) error
}

// NewBrowserHarness builds a scenario harness.
func NewBrowserHarness(options BrowserHarnessOptions) (*BrowserHarness, error) {
	if strings.TrimSpace(options.ArtifactRoot) == "" {
		return nil, errors.New("a browser harness requires an artifact root")
	}
	if err := os.MkdirAll(options.ArtifactRoot, 0o755); err != nil {
		return nil, fmt.Errorf("create artifact root: %w", err)
	}
	capture := options.Capture
	if capture == nil {
		capture = func(scenario, path string) error {
			return os.WriteFile(path, []byte(
				"no browser was available; this artifact records that scenario "+
					scenario+" failed and where its screenshot would have gone\n"), 0o600)
		}
	}
	return &BrowserHarness{
		artifactRoot: options.ArtifactRoot,
		preserve:     options.PreserveArtifacts,
		capture:      capture,
	}, nil
}

// Inspector is what a scenario driver supplies: the harness asks it questions
// about the mounted page, and it answers from whatever it is driving.
type Inspector interface {
	// UnnamedControls returns identifiers of controls with no accessible name.
	UnnamedControls() ([]string, error)
	// UnlabelledRegions returns identifiers of landmark regions with no label.
	UnlabelledRegions() ([]string, error)
	// ColourOnlyStates returns identifiers of elements conveying state only
	// through presentation.
	ColourOnlyStates() ([]string, error)
	// FocusedTestID returns the test identifier of the focused element.
	FocusedTestID() (string, error)
	// FocusIsVisible reports whether the focused element shows a focus ring.
	FocusIsVisible() (bool, error)
	// DeliverEvent hands one event to the page and reports whether it was
	// applied (false means it was filtered as a duplicate).
	DeliverEvent(event ScenarioEvent) (bool, error)
	// PressKey drives one keyboard action.
	PressKey(key string) error
}

// Run executes one scenario end to end (M22-111).
//
// A failure writes exactly one artifact; a pass writes none. Writing artifacts
// unconditionally is how an artifact directory becomes noise nobody reads, and
// writing none on failure is how a CI failure becomes unreproducible.
func (harness *BrowserHarness) Run(
	scenario BrowserScenario,
	inspector Inspector,
) (ScenarioResult, error) {
	if err := scenario.Validate(); err != nil {
		return ScenarioResult{}, err
	}
	if inspector == nil {
		return ScenarioResult{}, errors.New("a scenario run requires an inspector")
	}
	result := ScenarioResult{Scenario: scenario.Name}

	for _, event := range scenario.Events {
		applied, err := inspector.DeliverEvent(event)
		if err != nil {
			return harness.fail(result, fmt.Errorf("deliver event %d: %w", event.Sequence, err))
		}
		if applied {
			result.EventsDelivered++
			continue
		}
		result.DuplicatesFiltered++
		if !event.Duplicate {
			return harness.fail(result, fmt.Errorf(
				"event %d was filtered but was not a duplicate: the page dropped an event it should have applied",
				event.Sequence))
		}
	}

	for _, action := range scenario.Keys {
		repeat := action.Repeat
		if repeat == 0 {
			repeat = 1
		}
		for range repeat {
			if err := inspector.PressKey(action.Key); err != nil {
				return harness.fail(result, fmt.Errorf("press %s: %w", action.Key, err))
			}
			result.KeysDriven++
		}
		visible, err := inspector.FocusIsVisible()
		if err != nil {
			return harness.fail(result, fmt.Errorf("read focus visibility: %w", err))
		}
		if !visible {
			result.Findings = append(result.Findings, AccessibilityFinding{
				Rule: "focus-is-visible", Element: action.Key,
				Detail: "focus moved to an element with no visible focus indicator",
			})
		}
		if action.ExpectFocusTestID == "" {
			continue
		}
		focused, err := inspector.FocusedTestID()
		if err != nil {
			return harness.fail(result, fmt.Errorf("read focused element: %w", err))
		}
		if focused != action.ExpectFocusTestID {
			return harness.fail(result, fmt.Errorf(
				"after %s focus is on %q, expected %q",
				action.Key, focused, action.ExpectFocusTestID))
		}
	}

	findings, err := harness.checkAccessibility(inspector)
	if err != nil {
		return harness.fail(result, err)
	}
	result.Findings = append(result.Findings, findings...)
	if len(result.Findings) > 0 {
		return harness.fail(result, fmt.Errorf(
			"scenario %q produced %d accessibility findings", scenario.Name, len(result.Findings)))
	}

	harness.record(result)
	return result, nil
}

func (harness *BrowserHarness) checkAccessibility(inspector Inspector) ([]AccessibilityFinding, error) {
	var findings []AccessibilityFinding

	unnamed, err := inspector.UnnamedControls()
	if err != nil {
		return nil, fmt.Errorf("read unnamed controls: %w", err)
	}
	for _, element := range unnamed {
		findings = append(findings, AccessibilityFinding{
			Rule: "control-has-accessible-name", Element: element,
			Detail: "an interactive control cannot be addressed by name",
		})
	}

	unlabelled, err := inspector.UnlabelledRegions()
	if err != nil {
		return nil, fmt.Errorf("read unlabelled regions: %w", err)
	}
	for _, element := range unlabelled {
		findings = append(findings, AccessibilityFinding{
			Rule: "region-has-label", Element: element,
			Detail: "a landmark region is not exposed with a name",
		})
	}

	colourOnly, err := inspector.ColourOnlyStates()
	if err != nil {
		return nil, fmt.Errorf("read colour-only states: %w", err)
	}
	for _, element := range colourOnly {
		findings = append(findings, AccessibilityFinding{
			Rule: "state-not-conveyed-by-color-alone", Element: element,
			Detail: "state is distinguishable only by presentation",
		})
	}

	sort.SliceStable(findings, func(left, right int) bool {
		if findings[left].Rule != findings[right].Rule {
			return findings[left].Rule < findings[right].Rule
		}
		return findings[left].Element < findings[right].Element
	})
	return findings, nil
}

func (harness *BrowserHarness) fail(result ScenarioResult, cause error) (ScenarioResult, error) {
	result.Failed = true
	path := filepath.Join(harness.artifactRoot, sanitiseArtifactName(result.Scenario)+".failure")
	if err := harness.capture(result.Scenario, path); err == nil {
		result.ArtifactPaths = append(result.ArtifactPaths, path)
	}
	harness.record(result)
	return result, cause
}

func (harness *BrowserHarness) record(result ScenarioResult) {
	harness.mutex.Lock()
	defer harness.mutex.Unlock()
	harness.results = append(harness.results, result)
}

// Results returns every scenario run, in order.
func (harness *BrowserHarness) Results() []ScenarioResult {
	harness.mutex.Lock()
	defer harness.mutex.Unlock()
	results := make([]ScenarioResult, len(harness.results))
	copy(results, harness.results)
	return results
}

// Close removes the artifact directory unless preservation was requested.
func (harness *BrowserHarness) Close() error {
	return removeArtifactRoot(harness.artifactRoot, harness.preserve)
}

func sanitiseArtifactName(name string) string {
	var builder strings.Builder
	for _, character := range name {
		switch {
		case character >= 'a' && character <= 'z',
			character >= 'A' && character <= 'Z',
			character >= '0' && character <= '9',
			character == '-', character == '_':
			builder.WriteRune(character)
		default:
			builder.WriteRune('-')
		}
	}
	sanitised := builder.String()
	if sanitised == "" {
		return "scenario"
	}
	if len(sanitised) > 96 {
		sanitised = sanitised[:96]
	}
	return sanitised
}

// ScenarioTimeout is the ceiling a scenario driver should apply. It is stated
// here so every driver uses the same bound rather than inventing one.
const ScenarioTimeout = 45 * time.Second

// removeArtifactRoot deletes an artifact directory through the shared safety
// check, so a harness cannot recursively delete a path a fixture would refuse.
func removeArtifactRoot(root string, preserve bool) error {
	return testfixtures.RemoveFixtureDirectory(root, preserve)
}
