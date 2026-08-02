package primitives

import (
	"github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/css/u"
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
	IconPause        IconName = "pause"
	IconPlay         IconName = "play"
	IconStop         IconName = "stop"
	IconReview       IconName = "review"
	IconMore         IconName = "more"
	IconMinus        IconName = "minus"
	IconAttach       IconName = "attach"
	IconCopy         IconName = "copy"
	IconThread       IconName = "thread"
	IconArchive      IconName = "archive"
	IconRename       IconName = "rename"
	IconFilter       IconName = "filter"
	IconClock        IconName = "clock"
	IconSpend        IconName = "spend"
	IconTree         IconName = "tree"
	IconTranscript   IconName = "transcript"
	IconWork         IconName = "work"
	IconTool         IconName = "tool"
	IconPlan         IconName = "plan"
	IconProof        IconName = "proof"
	IconSend         IconName = "send"
	IconOptions      IconName = "options"
	IconWarning      IconName = "warning"
)

// Icon sizes.
//
// Three sizes, each bound to a type role, so a mark never has to be eyeballed
// against the text beside it: Small sits with metadata, Base with control
// labels and body text, and Large stands alone inside an icon-only control.
// Anything outside this scale is a mistake that shows up as a row of marks at
// four different weights.
const (
	IconSizeSmall = 14
	IconSizeBase  = 16
	IconSizeLarge = 20
)

// AllIconNames returns every drawn mark, so a test can prove each has a path.
func AllIconNames() []IconName {
	return []IconName{
		IconHome, IconTasks, IconGraph, IconMemory, IconRepositories,
		IconSettings, IconSearch, IconTheme, IconHelp, IconDatabase,
		IconBranch, IconCheck, IconModel, IconPlus, IconMenu,
		IconChevronLeft, IconChevronRight, IconChevronDown, IconClose,
		IconPause, IconPlay, IconStop, IconReview, IconMore, IconMinus,
		IconAttach, IconCopy, IconThread, IconArchive, IconRename, IconFilter,
		IconClock, IconSpend, IconTree, IconTranscript, IconWork, IconTool,
		IconPlan, IconProof, IconSend, IconOptions, IconWarning,
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
	IconPause:        {"M9.5 5.5v13", "M14.5 5.5v13"},
	IconPlay:         {"M8 5.5 18.5 12 8 18.5z"},
	IconStop:         {"M6.5 6.5h11v11h-11z"},
	IconReview:       {"M4.5 6.5h15v11h-15z", "M8 10.5h8", "M8 14h5"},
	IconMore:         {"M6 12h.01", "M12 12h.01", "M18 12h.01"},
	IconMinus:        {"M5.5 12h13"},
	IconAttach:       {"M16.5 9.5 10 16a3 3 0 0 1-4.2-4.2l7.4-7.4a4.5 4.5 0 0 1 6.4 6.4l-7.4 7.4"},
	IconCopy:         {"M9 9h9.5v9.5H9z", "M6 15H5.5V5.5H15V6"},
	IconThread:       {"M5 6.5h14v9H9l-4 3.5z"},
	IconArchive:      {"M4.5 6h15v3.5h-15z", "M6 9.5v9h12v-9", "M10 13h4"},
	IconRename:       {"M5 19h4l9.5-9.5a2 2 0 0 0-2.8-2.8L6 16z", "M14 6.5 17.5 10"},
	IconFilter:       {"M5 6.5h14", "M7.5 12h9", "M10 17.5h4"},
	IconClock:        {"M12 4.5a7.5 7.5 0 1 0 0 15 7.5 7.5 0 0 0 0-15", "M12 8v4.4l3 1.8"},
	IconSpend:        {"M12 4.5a7.5 7.5 0 1 0 0 15 7.5 7.5 0 0 0 0-15", "M14.5 9.3a3 3 0 0 0-2.5-1.1c-1.6 0-2.6.8-2.6 1.9 0 2.6 5.2 1.4 5.2 4 0 1.2-1.1 2-2.6 2a3 3 0 0 1-2.6-1.2", "M12 6.6v10.8"},
	IconTree:         {"M12 4.5v6", "M12 10.5a2 2 0 1 0 0 .01", "M6.5 19.5v-3.2a2 2 0 0 1 2-2h7a2 2 0 0 1 2 2v3.2", "M6.5 19.5a1.6 1.6 0 1 0 0 .01", "M17.5 19.5a1.6 1.6 0 1 0 0 .01"},
	IconTranscript:   {"M5 5.5h14v13H5z", "M8 9.5h8", "M8 13h8", "M8 16h4"},
	IconWork:         {"M5.5 5.5h13v13h-13z", "M9 9h6v6H9z"},
	IconTool:         {"M12 4.5 19 8.4v7.2L12 19.5 5 15.6V8.4z"},
	IconPlan:         {"M12 4.5 19.5 12 12 19.5 4.5 12z"},
	IconProof:        {"M12 4.5a7.5 7.5 0 1 0 0 15 7.5 7.5 0 0 0 0-15", "M8.5 12.2 11 14.8l4.5-5"},
	IconSend:         {"M5 12 19.5 5 14 19l-3-6z", "M11 13 19.5 5"},
	IconOptions:      {"M5 8h14", "M5 16h14", "M9.5 8a1.8 1.8 0 1 0 0 .01", "M15 16a1.8 1.8 0 1 0 0 .01"},
	IconWarning:      {"M12 4.5 20.5 19h-17z", "M12 10v4.5", "M12 16.8v.01"},
}

// IconProps configures one drawn mark.
type IconProps struct {
	Name IconName
	// Size is the rendered edge in CSS pixels, and should be one of
	// IconSizeSmall, IconSizeBase or IconSizeLarge. It defaults to the base
	// size, which sits with a control label without out-weighing it.
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
		size = IconSizeBase
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
	// The mark is drawn inside a box of its own exact size that cannot grow or
	// shrink. An SVG dropped straight into a flex column stretches to the
	// column's width, which turned a fourteen-pixel label mark into a
	// two-hundred-pixel drawing behind the words it was labelling.
	return html.Span(html.Props{
		Aria: map[string]string{"hidden": "true"},
		Class: css.New(
			u.InlineFlex, u.ItemsCenter, u.JustifyCenter,
			css.W(css.Px(size)), css.H(css.Px(size)),
			css.FlexShrink(css.Num(0)), css.FlexGrow(css.Num(0)),
		).String(),
	}, html.Svg(attributes, children...))
}

// strokeWidthFor keeps a mark legible at the size it is drawn.
func strokeWidthFor(size int) float64 {
	switch {
	case size <= IconSizeSmall:
		return 1.8
	case size <= IconSizeLarge:
		return 1.7
	default:
		return 1.5
	}
}
