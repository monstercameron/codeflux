package primitives

import (
	"encoding/hex"
	"fmt"
	"math"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/GoWebComponents/v5/virtualization"
)

// VirtualListState identifies the explicit presentation owned by VirtualList.
type VirtualListState string

const (
	VirtualListReady        VirtualListState = "ready"
	VirtualListLoading      VirtualListState = "loading"
	VirtualListError        VirtualListState = "error"
	VirtualListDisconnected VirtualListState = "disconnected"
)

// VirtualListItemProps describes one rendered option. FullLabel is the stable,
// untruncated name exposed to assistive technology and hover users.
type VirtualListItemProps[T any] struct {
	Index     int
	Item      T
	Key       string
	FullLabel string
	Active    bool
}

// VirtualListProps configures a fixed-height, controlled virtualized listbox.
// Items are treated as immutable, matching the GWC virtualization contract.
type VirtualListProps[T any] struct {
	ID                string
	Label             string
	Items             []T
	State             VirtualListState
	Height            float64
	RowHeight         float64
	Overscan          int
	Mode              Mode
	ActiveKey         string
	ItemKey           func(T) string
	ItemLabel         func(T) string
	RenderItem        func(VirtualListItemProps[T]) ui.Node
	OnActiveChange    func(string)
	OnActivate        func(string)
	EmptyTitle        string
	EmptyBody         string
	LoadingLabel      string
	ErrorTitle        string
	ErrorBody         string
	DisconnectedTitle string
	DisconnectedBody  string
	RetryLabel        string
	OnRetry           func()
}

// VirtualListKeyDecision is the pure result of listbox keyboard navigation.
type VirtualListKeyDecision struct {
	ActiveKey   string
	TargetIndex int
	Handled     bool
	Activate    bool
}

// ResolveVirtualListKey implements vertical listbox navigation. Page movement
// is clamped to the available items and never wraps.
func ResolveVirtualListKey(keys []string, activeKey, key string, pageSize int) VirtualListKeyDecision {
	if len(keys) == 0 {
		return VirtualListKeyDecision{TargetIndex: -1}
	}
	current := 0
	for index, itemKey := range keys {
		if itemKey == activeKey {
			current = index
			break
		}
	}
	if pageSize < 1 {
		pageSize = 1
	}
	target := current
	decision := VirtualListKeyDecision{ActiveKey: keys[current], TargetIndex: current}
	switch key {
	case "ArrowUp":
		target--
	case "ArrowDown":
		target++
	case "PageUp":
		target -= pageSize
	case "PageDown":
		target += pageSize
	case "Home":
		target = 0
	case "End":
		target = len(keys) - 1
	case "Enter", " ":
		decision.Handled = true
		decision.Activate = true
		return decision
	default:
		return decision
	}
	if target < 0 {
		target = 0
	}
	if target >= len(keys) {
		target = len(keys) - 1
	}
	decision.ActiveKey = keys[target]
	decision.TargetIndex = target
	decision.Handled = true
	return decision
}

// VirtualListScrollTop returns the smallest scroll adjustment that fully
// reveals the target fixed-height row.
func VirtualListScrollTop(current, viewportHeight, rowHeight float64, targetIndex int) float64 {
	if current < 0 {
		current = 0
	}
	if viewportHeight <= 0 || rowHeight <= 0 || targetIndex < 0 {
		return current
	}
	top := float64(targetIndex) * rowHeight
	bottom := top + rowHeight
	if top < current {
		return top
	}
	if bottom > current+viewportHeight {
		return bottom - viewportHeight
	}
	return current
}

// VirtualList renders a bounded GWC virtualized listbox with explicit
// loading, empty, error, and disconnected presentations.
func VirtualList[T any](props VirtualListProps[T]) ui.Node {
	state := props.State
	if state == "" {
		state = VirtualListReady
	}
	if invalid := validateVirtualListProps(props, state); invalid != "" {
		return contractError("virtual-list", invalid)
	}
	if state != VirtualListReady || len(props.Items) == 0 {
		return virtualListState(props, state)
	}

	keys := make([]string, len(props.Items))
	labels := make([]string, len(props.Items))
	for index, item := range props.Items {
		keys[index] = strings.TrimSpace(props.ItemKey(item))
		labels[index] = strings.TrimSpace(props.ItemLabel(item))
	}
	activeKey, activeIndex := resolvedVirtualListActive(keys, props.ActiveKey)
	pageSize := int(math.Floor(props.Height / props.RowHeight))
	outerProps := html.PropsOf(html.OnKeyDown(func(event ui.KeyboardEvent) {
		decision := ResolveVirtualListKey(keys, activeKey, event.GetKey(), pageSize)
		if !decision.Handled {
			return
		}
		event.PreventDefault()
		if decision.Activate {
			if props.OnActivate != nil {
				props.OnActivate(decision.ActiveKey)
			}
			return
		}
		if decision.ActiveKey != activeKey && props.OnActiveChange != nil {
			props.OnActiveChange(decision.ActiveKey)
		}
		scrollVirtualListToIndex(props.ID, decision.TargetIndex, props.RowHeight)
	}))
	outerProps.ID = props.ID
	outerProps.Role = "listbox"
	outerProps.TabIndex = html.TabIndexZero
	outerProps.Aria = map[string]string{
		"label":            strings.TrimSpace(props.Label),
		"activedescendant": virtualListItemID(props.ID, activeKey),
	}
	outerProps.Data = map[string]string{
		"component":       "virtual-list",
		"state":           "ready",
		"keyboard":        "arrows-page-home-end-enter-space",
		"focus-policy":    "active-descendant",
		"high-contrast":   boolARIA(props.Mode.HighContrast),
		"reduced-motion":  boolARIA(props.Mode.ReducedMotion),
		"pointer-minimum": fmt.Sprintf("%g", props.RowHeight),
	}
	tokens := props.Mode.Tokens()
	rules := []css.Rule{
		css.MaxWidth(css.Percent(100)),
		css.Bg(css.Hex(string(tokens.Colors.Surface1))),
		css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
		css.Border(css.Px(tokens.Geometry.BorderWidth), css.Hex(string(tokens.Colors.BorderStrong))),
		css.Rounded(css.Px(tokens.Geometry.PanelRadius)),
		css.Font(css.FontStack(tokens.Fonts.UI)),
	}
	rules = append(rules, css.FocusVisible(
		css.Outline(css.Px(tokens.Geometry.FocusRingWidth), css.Hex(string(tokens.Colors.FocusRing))),
		css.OutlineOffset(css.Px(tokens.Geometry.FocusRingOffset)),
	)...)
	outerProps.Class = css.New(rules...).String()

	return ui.CreateElement(virtualization.List[T], virtualization.ListProps[T]{
		ID: props.ID, Items: props.Items, Height: props.Height, RowHeight: props.RowHeight,
		Overscan: props.Overscan, OuterProps: outerProps,
		ItemKey: func(item T) string { return strings.TrimSpace(props.ItemKey(item)) },
		RenderRow: func(row virtualization.RowRenderProps[T]) ui.Node {
			itemKey := keys[row.Index]
			fullLabel := labels[row.Index]
			active := row.Index == activeIndex
			rowProps := html.PropsOf(html.OnClick(func() {
				if props.OnActiveChange != nil && itemKey != activeKey {
					props.OnActiveChange(itemKey)
				}
				if props.OnActivate != nil {
					props.OnActivate(itemKey)
				}
			}))
			rowProps.ID = virtualListItemID(props.ID, itemKey)
			rowProps.Role = "option"
			rowProps.Title = fullLabel
			rowProps.Aria = map[string]string{
				"label": fullLabel, "selected": boolARIA(active),
				"posinset": fmt.Sprintf("%d", row.Index+1),
				"setsize":  fmt.Sprintf("%d", len(props.Items)),
			}
			rowProps.Data = map[string]string{
				"component":  "virtual-list-option",
				"state":      map[bool]string{true: "active", false: "ready"}[active],
				"full-label": fullLabel,
			}
			return html.Div(rowProps, props.RenderItem(VirtualListItemProps[T]{
				Index: row.Index, Item: row.Item, Key: itemKey, FullLabel: fullLabel, Active: active,
			}))
		},
	})
}

func validateVirtualListProps[T any](props VirtualListProps[T], state VirtualListState) string {
	if strings.TrimSpace(props.ID) == "" {
		return "id is required"
	}
	if strings.TrimSpace(props.Label) == "" {
		return "accessible label is required"
	}
	switch state {
	case VirtualListLoading:
		if strings.TrimSpace(props.LoadingLabel) == "" {
			return "loading label is required"
		}
		return ""
	case VirtualListError:
		if strings.TrimSpace(props.ErrorTitle) == "" {
			return "error title is required"
		}
		return validateVirtualListRetry(props.RetryLabel, props.OnRetry)
	case VirtualListDisconnected:
		if strings.TrimSpace(props.DisconnectedTitle) == "" {
			return "disconnected title is required"
		}
		return validateVirtualListRetry(props.RetryLabel, props.OnRetry)
	case VirtualListReady:
	default:
		return "state is unsupported"
	}
	if len(props.Items) == 0 {
		if strings.TrimSpace(props.EmptyTitle) == "" {
			return "empty title is required"
		}
		return ""
	}
	if props.Height <= 0 {
		return "height must be greater than zero"
	}
	if props.RowHeight < float64(props.Mode.Tokens().Interaction.MinimumPointerTarget) {
		return "row height must meet the minimum pointer target"
	}
	if props.ItemKey == nil || props.ItemLabel == nil || props.RenderItem == nil {
		return "item key, full label, and renderer are required"
	}
	if props.OnActiveChange == nil {
		return "active change handler is required for keyboard navigation"
	}
	seen := make(map[string]struct{}, len(props.Items))
	for _, item := range props.Items {
		key := strings.TrimSpace(props.ItemKey(item))
		if key == "" {
			return "every item requires a stable key"
		}
		if _, exists := seen[key]; exists {
			return "item keys must be unique"
		}
		seen[key] = struct{}{}
		if strings.TrimSpace(props.ItemLabel(item)) == "" {
			return "every item requires a full accessible label"
		}
	}
	return ""
}

func validateVirtualListRetry(label string, handler func()) string {
	if strings.TrimSpace(label) != "" && handler == nil {
		return "retry action requires a handler"
	}
	if handler != nil && strings.TrimSpace(label) == "" {
		return "retry handler requires an accessible label"
	}
	return ""
}

func virtualListState[T any](props VirtualListProps[T], state VirtualListState) ui.Node {
	if state == VirtualListReady {
		state = "empty"
	}
	var content ui.Node
	switch state {
	case VirtualListLoading:
		content = Skeleton(SkeletonProps{AccessibleLabel: props.LoadingLabel, Lines: 3, Mode: props.Mode})
	case VirtualListError:
		content = ErrorState(ErrorStateProps{Title: props.ErrorTitle, Body: props.ErrorBody, ActionLabel: props.RetryLabel, Mode: props.Mode, OnAction: props.OnRetry})
	case VirtualListDisconnected:
		children := []ui.Node{
			html.H2(html.Props{Text: props.DisconnectedTitle}),
		}
		if props.DisconnectedBody != "" {
			children = append(children, html.P(html.Props{Text: props.DisconnectedBody}))
		}
		if props.RetryLabel != "" {
			children = append(children, Button(ButtonProps{Label: props.RetryLabel, Mode: props.Mode, OnClick: props.OnRetry}))
		}
		content = html.Section(html.Props{Role: "status", Data: map[string]string{"component": "disconnected-state"}}, children...)
	default:
		content = EmptyState(EmptyStateProps{Title: props.EmptyTitle, Body: props.EmptyBody, Mode: props.Mode})
	}
	return html.Section(html.Props{
		ID: props.ID, Role: "region", Aria: map[string]string{"label": strings.TrimSpace(props.Label)},
		Data: map[string]string{
			"component": "virtual-list", "state": string(state),
			"high-contrast": boolARIA(props.Mode.HighContrast), "reduced-motion": boolARIA(props.Mode.ReducedMotion),
		},
	}, content)
}

func resolvedVirtualListActive(keys []string, requested string) (string, int) {
	for index, key := range keys {
		if key == requested {
			return key, index
		}
	}
	return keys[0], 0
}

func virtualListItemID(listID, itemKey string) string {
	return listID + "-option-" + hex.EncodeToString([]byte(itemKey))
}
