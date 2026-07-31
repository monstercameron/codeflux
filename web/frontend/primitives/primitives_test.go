package primitives

import (
	"strings"
	"testing"

	"codeflux.dev/codeflux/web/frontend/design"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func render(t *testing.T, node ui.Node) string {
	t.Helper()
	output, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	return output
}

func requireContains(t *testing.T, output string, values ...string) {
	t.Helper()
	for _, value := range values {
		if !strings.Contains(output, value) {
			t.Fatalf("output lacks %q:\n%s", value, output)
		}
	}
}

func TestAccessibilityContractInventoryIsExplicit(t *testing.T) {
	contracts := AccessibilityContracts()
	if len(contracts) < 15 {
		t.Fatalf("contract inventory has only %d entries", len(contracts))
	}
	seen := map[string]bool{}
	for _, contract := range contracts {
		if contract.Component == "" || len(contract.Keyboard) == 0 ||
			contract.FocusPolicy == "" || !contract.SupportsHighContrast ||
			!contract.SupportsReducedMotion {
			t.Fatalf("incomplete contract: %#v", contract)
		}
		if contract.MinimumPointerTarget != 0 && contract.MinimumPointerTarget < 44 {
			t.Fatalf("undersized pointer target: %#v", contract)
		}
		if seen[contract.Component] {
			t.Fatalf("duplicate contract for %q", contract.Component)
		}
		seen[contract.Component] = true
	}
}

func TestButtonNamesBusyAndPreferenceStates(t *testing.T) {
	invalid := render(t, Button(ButtonProps{}))
	requireContains(t, invalid, `data-state="invalid-contract"`, "accessible label is required")

	busy := render(t, Button(ButtonProps{
		Label:           "Run",
		AccessibleLabel: "Run analysis",
		Busy:            true,
		Mode:            Mode{HighContrast: true, ReducedMotion: true},
	}))
	requireContains(t, busy,
		`aria-label="Run analysis"`,
		`aria-busy="true"`,
		`disabled`,
		`data-state="busy"`,
		`data-high-contrast="true"`,
		`data-reduced-motion="true"`,
		"Working",
	)
}

func TestTabsKeyboardNavigationSkipsDisabledAndWraps(t *testing.T) {
	items := []TabItem{
		{ID: "a", Label: "A"},
		{ID: "b", Label: "B", Disabled: true},
		{ID: "c", Label: "C"},
	}
	cases := map[string]string{
		"ArrowRight": "c",
		"ArrowLeft":  "c",
		"Home":       "a",
		"End":        "c",
	}
	for key, want := range cases {
		if got := ResolveTabsKey(items, "a", key); got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
	output := render(t, Tabs(TabsProps{Label: "Workspace views", Items: items, SelectedID: "a"}))
	requireContains(t, output,
		`role="tablist"`,
		`aria-label="Workspace views"`,
		`aria-selected="true"`,
		`data-keyboard="arrows-home-end"`,
	)
}

func TestMenuKeyboardContract(t *testing.T) {
	items := []MenuItem{{ID: "one"}, {ID: "two", Disabled: true}, {ID: "three"}}
	if next, action := ResolveMenuKey(items, "one", "ArrowDown"); next != "three" || action != "move" {
		t.Fatalf("down = %q/%q", next, action)
	}
	if next, action := ResolveMenuKey(items, "one", "Escape"); next != "one" || action != "dismiss" {
		t.Fatalf("escape = %q/%q", next, action)
	}
}

func TestResizableSplitKeyboardAndARIA(t *testing.T) {
	if got := AdjustSplitDrag(50, 80, 8, 20, 80); got != 60 {
		t.Fatalf("pointer drag = %v", got)
	}
	if got := AdjustSplitDrag(75, 80, 8, 20, 80); got != 80 {
		t.Fatalf("clamped pointer drag = %v", got)
	}
	if got := AdjustSplitValue(50, 20, 80, 10, SplitHorizontal, "ArrowRight"); got != 60 {
		t.Fatalf("right = %v", got)
	}
	if got := AdjustSplitValue(20, 20, 80, 10, SplitHorizontal, "ArrowLeft"); got != 20 {
		t.Fatalf("clamped left = %v", got)
	}
	if got := AdjustSplitValue(50, 20, 80, 10, SplitVertical, "ArrowDown"); got != 60 {
		t.Fatalf("down = %v", got)
	}
	output := render(t, ResizableSplit(ResizableSplitProps{
		AccessibleLabel: "Resize plan and editor panes",
		Orientation:     SplitHorizontal,
		Value:           55,
		Min:             20,
		Max:             80,
		Step:            5,
		First:           html.Text("Plan"),
		Second:          html.Text("Editor"),
	}))
	requireContains(t, output,
		`role="separator"`,
		`tabIndex="0"`,
		`aria-valuemin="20"`,
		`aria-valuemax="80"`,
		`aria-valuenow="55"`,
		`data-keyboard="arrows-home-end"`,
		`data-pointer="drag"`,
	)
	collapsed := render(t, ResizableSplit(ResizableSplitProps{
		AccessibleLabel: "Resize conversation and graph",
		Value:           60,
		Collapsed:       true,
		First:           html.Text("Conversation"),
		Second:          html.Text("Graph"),
	}))
	requireContains(t, collapsed, `data-pane="first"`, "Conversation", `data-pane="second"`, `hidden`, `data-state="collapsed"`)
}

func TestTechnicalLabelPreservesFullText(t *testing.T) {
	full := "repository/internal/provider/very-long-file-name.go:123"
	output := render(t, TechnicalLabel(TechnicalLabelProps{
		FullLabel:    full,
		VisibleLabel: "…/very-long-file-name.go:123",
	}))
	requireContains(t, output, `aria-label="`+full+`"`, `title="`+full+`"`, `data-full-label="`+full+`"`)
}

func TestLoadingEmptyAndErrorStatesAreDistinct(t *testing.T) {
	skeleton := render(t, Skeleton(SkeletonProps{
		AccessibleLabel: "Loading plan",
		Lines:           2,
		Mode:            Mode{ReducedMotion: true},
	}))
	requireContains(t, skeleton,
		`role="status"`,
		`aria-busy="true"`,
		`data-state="loading"`,
		`data-animation="none"`,
	)
	empty := render(t, EmptyState(EmptyStateProps{Title: "No changes", Body: "Choose a repository."}))
	requireContains(t, empty, `data-component="empty-state"`, `data-state="empty"`, `role="status"`)
	failure := render(t, ErrorState(ErrorStateProps{Title: "Plan failed", Body: "Try again.", ActionLabel: "Retry"}))
	requireContains(t, failure, `data-component="error-state"`, `data-state="error"`, `role="alert"`, "Retry")
}

func TestAlertUsesTextIconShapeAndLiveRole(t *testing.T) {
	output := render(t, InlineAlert(InlineAlertProps{
		Title:   "Validation failed",
		Message: "The evidence is stale.",
		Tone:    design.StatusInvalidated,
	}))
	requireContains(t, output,
		`role="alert"`,
		`aria-live="assertive"`,
		`data-tone="invalidated"`,
		`data-shape=`,
		"Validation failed",
		"The evidence is stale.",
	)
}

func TestControlAndContentPrimitivesExposeSemanticState(t *testing.T) {
	pressed := true
	icon := render(t, IconButton(IconButtonProps{
		Icon: "×", AccessibleLabel: "Close details", Pressed: &pressed,
	}))
	requireContains(t, icon,
		`aria-label="Close details"`,
		`aria-pressed="true"`,
		`data-component="icon-button"`,
		`title="Close details"`,
	)
	toggle := render(t, ToggleButton(ToggleButtonProps{Label: "Pin", Pressed: true}))
	requireContains(t, toggle, `aria-pressed="true"`, `data-component="toggle-button"`)

	field := render(t, TextField(TextFieldProps{
		ID: "repository", Label: "Repository", InvalidMessage: "Repository is required",
	}))
	requireContains(t, field,
		`aria-invalid="true"`,
		`aria-errormessage="repository-error"`,
		`role="alert"`,
		"Repository is required",
	)
	progress := render(t, ProgressIndicator(ProgressIndicatorProps{
		Label: "Loading graph", Value: 120, Max: 100,
	}))
	requireContains(t, progress, `aria-label="Loading graph"`, `max="100"`, `value="100"`)

	badge := render(t, Badge(BadgeProps{Label: "Blocked", Status: design.StatusBlocked}))
	requireContains(t, badge, `data-status="blocked"`, `data-shape="lock"`, "Blocked")

	disclosure := render(t, Disclosure(DisclosureProps{
		ID: "evidence", Label: "Evidence", Expanded: true, Content: html.Text("Proof"),
	}))
	requireContains(t, disclosure,
		`aria-expanded="true"`,
		`aria-controls="evidence-content"`,
		`data-state="expanded"`,
		"Proof",
	)

	code := render(t, CodeBlock(CodeBlockProps{
		Label: "Verification command", Code: "go test ./...",
	}))
	requireContains(t, code,
		`data-component="code-block"`,
		`aria-label="Verification command"`,
		`tabIndex="0"`,
		"Copy code",
		"go test ./...",
	)
}

func TestMenuAndNonModalOverlaysExposeTheirContracts(t *testing.T) {
	menu := render(t, Menu(MenuProps{
		ID:       "task-menu",
		Label:    "Task actions",
		Open:     true,
		ActiveID: "open",
		Items: []MenuItem{
			{ID: "open", Label: "Open"},
			{ID: "delete", Label: "Delete", Disabled: true},
		},
	}))
	requireContains(t, menu,
		`role="menu"`,
		`aria-label="Task actions"`,
		`role="menuitem"`,
		`data-keyboard="arrows-home-end-enter-space-escape"`,
		`disabled`,
	)

	popover := render(t, Popover(OverlayProps{
		ID: "details-popover", Open: true, LabelledBy: "details-trigger", Content: html.Text("Details"),
	}))
	requireContains(t, popover, `data-component="popover"`, `data-focus-policy="restore"`, "Details")
	popoverNode := Popover(OverlayProps{
		ID: "details-popover-policy", Open: true, LabelledBy: "details-trigger", Content: html.Text("Details"),
	})
	rawPopoverProps, ok := popoverNode.Props["__ui_props"]
	if !ok {
		t.Fatal("popover did not preserve a framework component boundary")
	}
	accessiblePopoverProps, ok := rawPopoverProps.(ui.AccessibleOverlayProps)
	if !ok || accessiblePopoverProps.Role != "region" || accessiblePopoverProps.Modal ||
		accessiblePopoverProps.BackgroundInert || accessiblePopoverProps.TrapFocus {
		t.Fatalf("popover became modal or inert: %#v", rawPopoverProps)
	}

	tooltip := render(t, Tooltip(TooltipProps{
		ID: "refresh-tip", Open: true, Label: "Refresh graph",
		Mode: Mode{ReducedMotion: true},
	}))
	requireContains(t, tooltip,
		`data-component="tooltip"`,
		`data-reduced-motion="true"`,
		"Refresh graph",
	)
}

func TestDialogAndDrawerUseAccessibleOverlayPolicies(t *testing.T) {
	policy := ModalOverlayAccessibilityPolicy()
	if policy.Role != "dialog" || !policy.Modal || !policy.TrapFocus ||
		!policy.RestoreFocus || !policy.CloseOnEscape ||
		!policy.CloseOnOutsideClick || !policy.LockScroll ||
		!policy.BackgroundInert {
		t.Fatalf("incomplete modal accessibility policy: %#v", policy)
	}
	dialogNode := Dialog(OverlayProps{
		ID: "confirm", Open: true, LabelledBy: "confirm-title",
		InitialFocusSelector: "#confirm-close", AppRootSelector: "#app-root",
		Content: html.Text("Confirm"),
	})
	rawProps, ok := dialogNode.Props["__ui_props"]
	if !ok {
		t.Fatal("dialog did not preserve a framework component boundary")
	}
	overlayProps, ok := rawProps.(ui.AccessibleOverlayProps)
	if !ok || !overlayProps.RestoreFocus || !overlayProps.TrapFocus ||
		overlayProps.InitialFocusSelector != "#confirm-close" ||
		overlayProps.AppRootSelector != "#app-root" {
		t.Fatalf("dialog lost GWC focus-management props: %#v", rawProps)
	}
	for name, node := range map[string]ui.Node{
		"dialog": dialogNode,
		"drawer": Drawer(OverlayProps{ID: "details", Open: true, LabelledBy: "details-title", Content: html.Text("Details")}),
	} {
		output := render(t, node)
		requireContains(t, output, `data-focus-policy="trap-restore"`, `data-dismiss-policy="escape-outside"`)
		if !strings.Contains(output, `data-component="`+name+`"`) {
			t.Fatalf("%s output lacks component marker: %s", name, output)
		}
	}
}
