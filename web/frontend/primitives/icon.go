package primitives

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// IconName selects one drawn mark.
//
// The interface previously used Unicode glyphs — ⌂ ▣ ⌘ ◫ ▤ ⚙ — as navigation
// icons. They render at different weights and optical sizes on every platform,
// several fall back to a box on Windows, and none of them line up with each
// other. A drawn icon is the same shape everywhere and can take the colour of
// the text beside it.
type IconName string

const (
	IconHome         IconName = "home"
	IconTasks        IconName = "tasks"
	IconGraph        IconName = "graph"
	IconMemory       IconName = "memory"
	IconRepositories IconName = "repositories"
	IconSettings     IconName = "settings"
	IconSearch       IconName = "search"
	IconTheme        IconName = "theme"
	IconHelp         IconName = "help"
	IconDatabase     IconName = "database"
	IconBranch       IconName = "branch"
	IconCheck        IconName = "check"
	IconModel        IconName = "model"
	IconPlus         IconName = "plus"
	IconMenu         IconName = "menu"
	IconChevronLeft  IconName = "chevron-left"
	IconChevronRight IconName = "chevron-right"
	IconChevronDown  IconName = "chevron-down"
	IconClose        IconName = "close"
)

// AllIconNames returns every drawn mark, so a test can prove each has a path.
func AllIconNames() []IconName {
	return []IconName{
		IconHome, IconTasks, IconGraph, IconMemory, IconRepositories,
		IconSettings, IconSearch, IconTheme, IconHelp, IconDatabase,
		IconBranch, IconCheck, IconModel, IconPlus, IconMenu,
		IconChevronLeft, IconChevronRight, IconChevronDown, IconClose,
	}
}

// iconPaths are 24-unit line drawings.
//
// One stroke weight, one grid, one optical size. They are deliberately plain:
// an icon in a supervision console is a label, not an illustration, and a
// drawn flourish would compete with the state colours that actually carry
// meaning here.
var iconPaths = map[IconName][]string{
	IconHome:         {"M4 11.5 12 4l8 7.5", "M6.5 10v9.5h11V10"},
	IconTasks:        {"M4.5 6.5h15", "M4.5 12h15", "M4.5 17.5h9"},
	IconGraph:        {"M12 4v5", "M12 15v5", "M7.5 12H4.5", "M19.5 12h-3", "M9 12a3 3 0 1 0 6 0 3 3 0 1 0-6 0", "M12 9V4.5", "M9.8 14.2 6.5 17.5", "M14.2 14.2l3.3 3.3"},
	IconMemory:       {"M5 7.5h14v9H5z", "M8.5 7.5v9", "M15.5 7.5v9", "M5 12h14"},
	IconRepositories: {"M5 5.5h9l2 2.5h3v10.5H5z", "M5 10h14"},
	IconSettings:     {"M12 9a3 3 0 1 0 0 6 3 3 0 0 0 0-6", "M12 3.5v2.2", "M12 18.3v2.2", "M4.6 7.8l1.9 1.1", "M17.5 15.1l1.9 1.1", "M4.6 16.2l1.9-1.1", "M17.5 8.9l1.9-1.1"},
	IconSearch:       {"M11 4.5a6.5 6.5 0 1 0 0 13 6.5 6.5 0 0 0 0-13", "M15.8 15.8 20 20"},
	IconTheme:        {"M12 4.5a7.5 7.5 0 1 0 0 15 7.5 7.5 0 0 0 0-15", "M12 4.5v15a7.5 7.5 0 0 0 0-15"},
	IconHelp:         {"M12 4.5a7.5 7.5 0 1 0 0 15 7.5 7.5 0 0 0 0-15", "M9.6 9.7a2.5 2.5 0 1 1 3.2 2.4c-.6.2-.8.7-.8 1.3v.4", "M12 16.8v.01"},
	IconDatabase:     {"M12 4.5c-3.9 0-7 1-7 2.3s3.1 2.2 7 2.2 7-1 7-2.2-3.1-2.3-7-2.3", "M5 6.8v10.4c0 1.3 3.1 2.3 7 2.3s7-1 7-2.3V6.8", "M5 12c0 1.3 3.1 2.3 7 2.3s7-1 7-2.3"},
	IconBranch:       {"M7 5.5v13", "M7 5.5a1.8 1.8 0 1 0 0 .01", "M7 18.5a1.8 1.8 0 1 0 0 .01", "M17 8.3a1.8 1.8 0 1 0 0 .01", "M17 10.1v1.1c0 2.2-1.8 4-4 4H7"},
	IconCheck:        {"M5 12.5 9.5 17 19 7.5"},
	IconModel:        {"M12 4.2 19 8v8l-7 3.8L5 16V8z", "M12 11.8 19 8", "M12 11.8 5 8", "M12 11.8v8"},
	IconPlus:         {"M12 5.5v13", "M5.5 12h13"},
	IconMenu:         {"M4.5 7h15", "M4.5 12h15", "M4.5 17h15"},
	IconChevronLeft:  {"M14.5 5.5 8 12l6.5 6.5"},
	IconChevronRight: {"M9.5 5.5 16 12l-6.5 6.5"},
	IconChevronDown:  {"M5.5 9.5 12 16l6.5-6.5"},
	IconClose:        {"M6 6l12 12", "M18 6 6 18"},
}

// IconProps configures one drawn mark.
type IconProps struct {
	Name IconName
	// Size is the rendered edge in CSS pixels. It defaults to 18, which sits
	// with body text without out-weighing it.
	Size int
	// AccessibleLabel names the icon for assistive technology. An icon with no
	// label is hidden instead: an unlabelled image announced as "graphic" is
	// noise, and every icon here sits beside its own visible text.
	AccessibleLabel string
}

// Icon renders one mark, inheriting the colour of the text around it.
func Icon(props IconProps) ui.Node {
	size := props.Size
	if size <= 0 {
		size = 18
	}
	paths, drawn := iconPaths[props.Name]
	if !drawn {
		// An unknown name draws nothing rather than a placeholder box. A
		// missing icon should be invisible, not wrong.
		return html.Span(html.Props{Hidden: true})
	}
	children := make([]ui.Node, 0, len(paths))
	for _, definition := range paths {
		children = append(children, html.Path(html.Props{
			Raw: map[string]any{"d": definition},
		}))
	}
	raw := map[string]any{
		"viewBox": "0 0 24 24",
		"width":   size,
		"height":  size,
		"fill":    "none",
		"stroke":  "currentColor",
		// Small marks need a slightly heavier stroke than large ones or they
		// disappear against a dark ground. Below 18 pixels the hairline that
		// reads as precise at 24 reads as absent.
		"stroke-width":        strokeWidthFor(size),
		"stroke-linecap":      "round",
		"stroke-linejoin":     "round",
		"vector-effect":       "non-scaling-stroke",
		"shape-rendering":     "geometricPrecision",
		"preserveAspectRatio": "xMidYMid meet",
	}
	attributes := html.Props{Raw: raw}
	if props.AccessibleLabel != "" {
		attributes.Role = "img"
		attributes.Aria = map[string]string{"label": props.AccessibleLabel}
	} else {
		attributes.Aria = map[string]string{"hidden": "true"}
	}
	return html.Svg(attributes, children...)
}

// strokeWidthFor keeps a mark legible at the size it is drawn.
func strokeWidthFor(size int) float64 {
	switch {
	case size <= 14:
		return 1.9
	case size <= 18:
		return 1.75
	default:
		return 1.5
	}
}
