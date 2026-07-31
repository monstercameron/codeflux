package primitives

// AccessibilityContract records the behavior that must be preserved when a
// primitive is reused or restyled.
type AccessibilityContract struct {
	Component             string
	Keyboard              []string
	FocusPolicy           string
	NameRequired          bool
	SupportsDisabled      bool
	SupportsBusy          bool
	SupportsHighContrast  bool
	SupportsReducedMotion bool
	MinimumPointerTarget  int
}

// AccessibilityContracts returns the audited behavior inventory for the shared
// primitives implemented by this package.
func AccessibilityContracts() []AccessibilityContract {
	return []AccessibilityContract{
		{Component: "button", Keyboard: []string{"Enter", "Space"}, FocusPolicy: "native", NameRequired: true, SupportsDisabled: true, SupportsBusy: true, SupportsHighContrast: true, SupportsReducedMotion: true, MinimumPointerTarget: 44},
		{Component: "icon-button", Keyboard: []string{"Enter", "Space"}, FocusPolicy: "native", NameRequired: true, SupportsDisabled: true, SupportsBusy: true, SupportsHighContrast: true, SupportsReducedMotion: true, MinimumPointerTarget: 44},
		{Component: "toggle-button", Keyboard: []string{"Enter", "Space"}, FocusPolicy: "native", NameRequired: true, SupportsDisabled: true, SupportsBusy: true, SupportsHighContrast: true, SupportsReducedMotion: true, MinimumPointerTarget: 44},
		{Component: "tabs", Keyboard: []string{"ArrowLeft", "ArrowRight", "Home", "End"}, FocusPolicy: "roving", NameRequired: true, SupportsDisabled: true, SupportsHighContrast: true, SupportsReducedMotion: true, MinimumPointerTarget: 44},
		{Component: "menu", Keyboard: []string{"ArrowUp", "ArrowDown", "Home", "End", "Enter", "Space", "Escape"}, FocusPolicy: "trap-restore", NameRequired: true, SupportsDisabled: true, SupportsHighContrast: true, SupportsReducedMotion: true, MinimumPointerTarget: 44},
		{Component: "dialog", Keyboard: []string{"Tab", "Shift+Tab", "Escape"}, FocusPolicy: "trap-restore", NameRequired: true, SupportsHighContrast: true, SupportsReducedMotion: true},
		{Component: "drawer", Keyboard: []string{"Tab", "Shift+Tab", "Escape"}, FocusPolicy: "trap-restore", NameRequired: true, SupportsHighContrast: true, SupportsReducedMotion: true},
		{Component: "popover", Keyboard: []string{"Tab", "Escape"}, FocusPolicy: "restore", NameRequired: true, SupportsHighContrast: true, SupportsReducedMotion: true},
		{Component: "tooltip", Keyboard: []string{"Escape"}, FocusPolicy: "none", NameRequired: true, SupportsHighContrast: true, SupportsReducedMotion: true},
		{Component: "text-field", Keyboard: []string{"native text editing"}, FocusPolicy: "native", NameRequired: true, SupportsDisabled: true, SupportsHighContrast: true, SupportsReducedMotion: true, MinimumPointerTarget: 44},
		{Component: "badge", Keyboard: []string{"none"}, FocusPolicy: "none", NameRequired: true, SupportsHighContrast: true, SupportsReducedMotion: true},
		{Component: "resizable-split", Keyboard: []string{"ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown", "Home", "End", "Pointer drag"}, FocusPolicy: "separator", NameRequired: true, SupportsHighContrast: true, SupportsReducedMotion: true, MinimumPointerTarget: 44},
		{Component: "disclosure", Keyboard: []string{"Enter", "Space"}, FocusPolicy: "native", NameRequired: true, SupportsDisabled: true, SupportsHighContrast: true, SupportsReducedMotion: true, MinimumPointerTarget: 44},
		{Component: "copy-button", Keyboard: []string{"Enter", "Space"}, FocusPolicy: "native", NameRequired: true, SupportsDisabled: true, SupportsBusy: true, SupportsHighContrast: true, SupportsReducedMotion: true, MinimumPointerTarget: 44},
		{Component: "code-block", Keyboard: []string{"Tab", "Arrow keys"}, FocusPolicy: "scroll-region", NameRequired: true, SupportsHighContrast: true, SupportsReducedMotion: true},
		{Component: "inline-alert", Keyboard: []string{"announcement"}, FocusPolicy: "none", SupportsHighContrast: true, SupportsReducedMotion: true},
		{Component: "progress-indicator", Keyboard: []string{"announcement"}, FocusPolicy: "none", NameRequired: true, SupportsHighContrast: true, SupportsReducedMotion: true},
		{Component: "skeleton", Keyboard: []string{"announcement"}, FocusPolicy: "none", NameRequired: true, SupportsHighContrast: true, SupportsReducedMotion: true},
		{Component: "empty-state", Keyboard: []string{"action-dependent"}, FocusPolicy: "action", NameRequired: true, SupportsHighContrast: true, SupportsReducedMotion: true},
		{Component: "error-state", Keyboard: []string{"action-dependent"}, FocusPolicy: "action", NameRequired: true, SupportsHighContrast: true, SupportsReducedMotion: true},
		{Component: "technical-label", Keyboard: []string{"none"}, FocusPolicy: "none", NameRequired: true, SupportsHighContrast: true, SupportsReducedMotion: true},
		{Component: "virtual-list", Keyboard: []string{"ArrowUp", "ArrowDown", "PageUp", "PageDown", "Home", "End", "Enter", "Space"}, FocusPolicy: "active-descendant", NameRequired: true, SupportsHighContrast: true, SupportsReducedMotion: true, MinimumPointerTarget: 44},
	}
}
