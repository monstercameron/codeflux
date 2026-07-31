package shortcuts

import "sort"

// FocusRegion identifies one major application landmark in sequential focus
// order.
type FocusRegion string

const (
	FocusRail         FocusRegion = "rail"
	FocusConversation FocusRegion = "conversation"
	FocusComposer     FocusRegion = "composer"
	FocusGraph        FocusRegion = "graph"
	FocusInspector    FocusRegion = "inspector"
	FocusReview       FocusRegion = "review"
)

// TabOrder returns the stable major-region order. Local controls remain in DOM
// order within their owning region.
func TabOrder() []FocusRegion {
	return []FocusRegion{FocusRail, FocusConversation, FocusComposer, FocusGraph, FocusInspector, FocusReview}
}

// HelpDialogModel contains the accessibility contract a GWC dialog renderer
// must honor.
type HelpDialogModel struct {
	ID             string
	Role           string
	AriaModal      bool
	Title          string
	TitleID        string
	LabelledBy     string
	Description    string
	DescriptionID  string
	DescribedBy    string
	CloseLabel     string
	CloseControlID string
	InitialFocusID string
	CloseOnEscape  bool
	TrapFocus      bool
	RestoreFocus   bool
	Groups         []HelpGroup
}

// HelpGroup is one ordered shortcut category.
type HelpGroup struct {
	ID      string
	Heading string
	Entries []HelpEntry
}

// HelpEntry is one visible and screen-reader-readable action/chord pair.
type HelpEntry struct {
	Action             Action
	Description        string
	KeyLabel           string
	AccessibleKeyLabel string
}

// HelpDialog returns an accessible, deterministic help-dialog model.
func (policy Policy) HelpDialog(platform Platform) HelpDialogModel {
	groups := []HelpGroup{
		{ID: "shortcut-help-navigation", Heading: "Navigation"},
		{ID: "shortcut-help-task-controls", Heading: "Task controls"},
		{ID: "shortcut-help-general", Heading: "General"},
	}
	bindings := policy.Bindings()
	sort.SliceStable(bindings, func(left, right int) bool {
		return actionRank(bindings[left].Action) < actionRank(bindings[right].Action)
	})
	for _, binding := range bindings {
		groupIndex := helpGroupIndex(binding.Action)
		groups[groupIndex].Entries = append(groups[groupIndex].Entries, HelpEntry{
			Action:             binding.Action,
			Description:        actionDescription(binding.Action),
			KeyLabel:           binding.Chord.Label(platform),
			AccessibleKeyLabel: binding.Chord.AccessibleLabel(platform),
		})
	}
	return HelpDialogModel{
		ID:             "shortcut-help-dialog",
		Role:           "dialog",
		AriaModal:      true,
		Title:          "Keyboard shortcuts",
		TitleID:        "shortcut-help-title",
		LabelledBy:     "shortcut-help-title",
		Description:    "Application shortcuts are paused while you type unless a control documents a scoped shortcut.",
		DescriptionID:  "shortcut-help-description",
		DescribedBy:    "shortcut-help-description",
		CloseLabel:     "Close keyboard shortcut help",
		CloseControlID: "shortcut-help-close",
		InitialFocusID: "shortcut-help-close",
		CloseOnEscape:  true,
		TrapFocus:      true,
		RestoreFocus:   true,
		Groups:         groups,
	}
}

func actionRank(action Action) int {
	switch action {
	case ActionFocusConversation:
		return 0
	case ActionFocusGraph:
		return 1
	case ActionPause:
		return 2
	case ActionStop:
		return 3
	case ActionHelp:
		return 4
	default:
		return 5
	}
}

func helpGroupIndex(action Action) int {
	switch action {
	case ActionFocusConversation, ActionFocusGraph:
		return 0
	case ActionPause, ActionStop:
		return 1
	default:
		return 2
	}
}

func actionDescription(action Action) string {
	switch action {
	case ActionFocusConversation:
		return "Focus conversation"
	case ActionFocusGraph:
		return "Focus graph"
	case ActionPause:
		return "Pause the active task"
	case ActionStop:
		return "Stop the active task"
	case ActionHelp:
		return "Open keyboard shortcut help"
	default:
		return ""
	}
}
