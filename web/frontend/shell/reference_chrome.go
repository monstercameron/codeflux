package shell

import (
	"math"
	"strconv"
	"strings"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/design"
	"codeflux.dev/codeflux/web/frontend/graphcanvas"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"codeflux.dev/codeflux/web/frontend/routes"
	"codeflux.dev/codeflux/web/frontend/state"
	"github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/css/u"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func applicationFrameClass(
	layout state.LayoutPreferences,
	tokens design.Tokens,
	inspectorCollapsed bool,
) string {
	layout = layout.Normalize()
	tracks := []css.Track{css.MinMax(css.TrackLen(css.Zero), css.Fr(1))}
	switch layout.Viewport {
	case state.ViewportWide:
		if layout.RailCollapsed {
			tracks = []css.Track{css.MinMax(css.TrackLen(css.Zero), css.Fr(1))}
		} else {
			tracks = []css.Track{
				css.TrackLen(css.Px(layout.RailWidth)),
				css.MinMax(css.TrackLen(css.Zero), css.Fr(1)),
			}
		}
		if !inspectorCollapsed {
			tracks = append(tracks, css.TrackLen(css.Px(380)))
		}
	}
	return css.New(
		u.Relative,
		u.Grid,
		css.GridCols(tracks...),
		css.W(css.Full),
		// Dynamic viewport units follow the visual viewport as a software
		// keyboard opens, keeping the grid's anchored composer reachable.
		css.H(css.RawLength("calc(100dvh - 64px)")),
		css.MinWidth(css.Zero),
		css.MinHeight(css.Zero),
		css.Overflow.Hidden,
		css.Bg(css.Hex(string(tokens.Colors.Canvas))),
	).String()
}

func routeFrameClass(layout state.LayoutPreferences) string {
	layout = layout.Normalize()
	rules := []css.Rule{
		css.MinWidth(css.Zero),
		css.MinHeight(css.Zero),
		css.W(css.Full),
		css.H(css.Full),
		css.Overflow.Hidden,
	}
	if layout.Viewport == state.ViewportMedium && !layout.RailCollapsed {
		rules = append(rules, css.Padding(css.RawLength(
			"0 0 0 "+strconv.Itoa(layout.RailWidth)+"px",
		)))
	}
	return css.New(rules...).String()
}

func ApplicationBar(props ApplicationBarProps) ui.Node {
	recordRender(props.Probe, "application-bar", props.Revision)
	shortcutHelpHandler := ui.UseCallback(func() {
		if props.OnShortcutHelp != nil {
			props.OnShortcutHelp()
		}
	})
	tokens := props.Mode.Tokens()
	compact := props.Viewport == state.ViewportNarrow || props.Viewport == state.ViewportMinimum
	tracks := []css.Track{
		css.TrackLen(css.Px(250)),
		css.MinMax(css.TrackLen(css.Zero), css.Fr(1)),
		css.TrackLen(css.Px(430)),
	}
	if props.Viewport == state.ViewportWide {
		tracks = []css.Track{
			css.TrackLen(css.Px(250)),
			css.MinMax(css.TrackLen(css.Zero), css.Fr(1)),
			css.TrackLen(css.Px(620)),
		}
	}
	if compact {
		tracks = []css.Track{
			css.MinMax(css.TrackLen(css.Zero), css.Fr(1)),
			css.TrackAuto,
		}
	}
	repository := fallback(props.View.Repository, props.Workspace.RepositoryName)
	if repository == "" {
		repository = "Choose repository"
	}
	branch := fallback(props.View.Branch, props.Workspace.Branch)
	if branch == "" {
		branch = "branch"
	}
	worktree := props.View.WorktreeStatus
	if worktree == "" {
		if props.Workspace.Dirty {
			worktree = "uncommitted changes"
		} else {
			worktree = "Unknown"
		}
	}
	// Each of these used to fall back to the name of the field it holds, so an
	// unset model rendered the word "model" and an unmeasured spend rendered
	// the word "actual". Read at a glance those are indistinguishable from
	// values, and the whole row looked populated while nothing had been
	// measured. Unknown is now said out loud.
	const unknown = "Unknown"
	provider := fallback(props.View.Provider, unknown)
	model := fallback(props.View.Model, unknown)
	effort := fallback(props.View.Effort, unknown)
	forecast := fallback(props.View.ForecastCost, unknown)
	tokensUsed := fallback(props.View.ActualTokens, unknown)
	actual := fallback(props.View.ActualCost, fallback(props.CostLabel, unknown))
	pricing := fallback(props.View.PricingSnapshot, unknown)
	budget := fallback(props.View.HardBudget, "Not set")
	remaining := fallback(props.View.RemainingBudget, unknown)
	warning := fallback(props.View.BudgetWarning, "None")
	// A task is what gives the model and cost controls a subject. Without one
	// they are five copies of the word Unknown taking the width the repository
	// and branch need, so they are not drawn at all.
	taskSelected := strings.TrimSpace(props.View.TaskState) != ""
	connection := string(props.View.Connection)
	if connection == "" {
		connection = string(props.Session.Connection)
	}
	return html.Header(html.Props{
		Data: map[string]string{
			"component": "application-bar",
			"revision":  strconv.FormatUint(props.Revision, 10),
		},
		Aria:  map[string]string{"label": "Application"},
		Class: applicationBarClass(tracks, tokens),
	},
		html.Div(html.Props{
			Class: css.New(u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.MD))).String(),
		},
			brandMark(tokens),
			html.Strong(html.Props{
				Class: css.New(
					css.FontSize(css.Px(21)),
					css.FontWeight.Semibold,
					css.Tracking(css.Ems(-0.02)),
					css.Font(css.FontStack(tokens.Fonts.Display)),
					css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
				).String(),
				Text: "Codeflux",
			}),
			html.Div(html.Props{},
				primitives.Button(primitives.ButtonProps{
					ID: "thread-rail-toggle", Icon: primitives.IconMenu,
					AccessibleLabel: "Toggle thread rail",
					Expanded:        &props.RailOpen, Controls: "product-sidebar-navigation",
					Mode: props.Mode, Disabled: props.OnRailToggle == nil, OnClick: props.OnRailToggle,
				}),
			),
		),
		// The instrument strip. Every fact a person checks before intervening
		// sits on one line, each under the smallest label that can name it,
		// separated by hairlines rather than boxed into competing chips.
		html.Div(html.Props{
			Hidden: compact,
			Aria:   map[string]string{"label": "Workspace instruments"},
			Class:  instrumentStripClass(),
		},
			html.Div(html.Props{Class: instrumentGroupClass(tokens, true)},
				instrumentEyebrow("Workspace", tokens),
				html.Div(html.Props{
					Class: css.New(
						u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.XS)),
						css.MinWidth(css.Zero),
					).String(),
				},
					contextControl("repository", repository, "/", props.Mode, props.OnNavigatePath),
					html.Span(html.Props{
						Aria: map[string]string{"hidden": "true"},
						Class: css.New(
							css.TextColor(css.Hex(string(tokens.Colors.TextDisabled))),
							css.FontSize(css.Px(tokens.Typography.ControlLabel.Size)),
						).String(),
						Text: "/",
					}),
					contextControl("branch", branch, "/settings", props.Mode, props.OnNavigatePath),
					contextControl("worktree", worktree, "/settings", props.Mode, props.OnNavigatePath),
				),
			),
			html.Div(html.Props{
				Class:  instrumentGroupClass(tokens, false),
				Hidden: !taskSelected,
			},
				instrumentEyebrow("Model · "+provider, tokens),
				contextControl("model", model+" · "+effort, "/settings", props.Mode, props.OnNavigatePath),
			),
			html.Div(html.Props{
				Class:  wideOnlyClass(),
				Hidden: !taskSelected,
			},
				html.Div(html.Props{Class: instrumentGroupClass(tokens, false)},
					instrumentEyebrow("Spend against cap", tokens),
					// The header carries the two figures that decide whether to
					// intervene: what this has cost, and what the cap is. The
					// forecast, token count, pricing snapshot and warning
					// threshold remain the control's accessible name and open
					// in settings, rather than being concatenated into an
					// unreadable run of seven values.
					costReadout(costReadoutProps{
						Spent:     actual,
						Remaining: budget,
						Fraction:  props.View.SpentFraction,
						Warned:    props.View.BudgetWarned,
						Breakdown: "forecast " + forecast + ", usage " + tokensUsed +
							", actual " + actual + ", pricing " + pricing +
							", budget " + budget + ", remaining " + remaining +
							", warning threshold " + warning,
						Mode:       props.Mode,
						OnNavigate: props.OnNavigatePath,
						TargetPath: "/settings",
					}),
				),
			),
		),
		html.Div(html.Props{
			Aria: map[string]string{"label": "Session controls"},
			Class: css.New(
				u.Flex, u.ItemsCenter, u.JustifyEnd, css.Gap(css.Px(tokens.Spacing.SM)),
			).String(),
		},
			html.Div(html.Props{Hidden: compact, Class: desktopOnlyClass()},
				liveLamp(connection, tokens),
			),
			manualReconnectControl(props, connection),
			headerIconButtonWithID("global-search-trigger", primitives.IconSearch, "Search", props.Mode, props.OnSearchOpen),
			headerIconButton(primitives.IconTheme, "Change color theme", props.Mode, props.OnThemeChange),
			primitives.Button(primitives.ButtonProps{
				ID: "shortcut-help-trigger", Icon: primitives.IconHelp,
				AccessibleLabel: "Shortcut help", Mode: props.Mode,
				Disabled: props.OnShortcutHelp == nil, OnClick: shortcutHelpHandler,
			}),
			html.Div(html.Props{Class: wideOnlyClass()},
				headerIconButtonWithID(
					"assurance-rail-toggle", primitives.IconMemory, "Toggle task details sidebar", props.Mode, props.OnInspectorToggle,
				),
			),
			html.Span(html.Props{
				Hidden: compact,
				Aria:   map[string]string{"label": "Current user"},
				Class: desktopOnlyClass(
					u.InlineFlex, u.ItemsCenter, u.JustifyCenter,
					css.W(css.Px(34)), css.H(css.Px(34)),
					css.Rounded(css.Px(tokens.Geometry.PillRadius)),
					css.Bg(css.Hex(string(tokens.Colors.Surface3))),
					css.Border(css.Px(1), css.Hex(string(tokens.Colors.BorderStrong))),
					css.FontSize(css.Px(tokens.Typography.ControlLabel.Size)),
				),
				Text: "CF",
			}),
		),
		ui.CreateElement(SearchDialog, SearchDialogProps{
			Open: props.SearchOpen, Query: props.SearchQuery, Mode: props.Mode,
			OnDismiss: props.OnSearchDismiss, OnQueryChange: props.OnSearchQueryChange,
			OnNavigatePath: props.OnNavigatePath,
		}),
	)
}

func manualReconnectControl(props ApplicationBarProps, connection string) ui.Node {
	if connection != string(state.ConnectionDisconnected) || props.OnReconnectRequested == nil {
		return nil
	}
	return primitives.Button(primitives.ButtonProps{
		ID: "session-reconnect", Label: "Reconnect", AccessibleLabel: "Reconnect live session",
		Mode: props.Mode, OnClick: props.OnReconnectRequested,
	})
}

// applicationBarLayer is the stacking layer the top bar occupies.
//
// The bar used to declare no position and no z-index at all, which made it
// unclickable in a way that reads as a paint bug and is not one. CSS paints
// positioned elements above non-positioned content whatever the document order
// says, and the application frame below the bar is position: relative. So the
// frame, and everything positioned inside it — the compact sidebar and the
// composer at 20, the graph overlays — painted over a bar that came first in
// the markup, and took the clicks aimed at it.
//
// The number sits above every layer inside the frame and below the skip link's
// 100, which must stay reachable from the very first Tab.
const applicationBarLayer = 50

func applicationBarClass(tracks []css.Track, tokens design.Tokens) string {
	rules := []css.Rule{
		u.Grid,
		css.GridCols(tracks...),
		u.ItemsCenter,
		css.Gap(css.Px(tokens.Spacing.LG)),
		css.W(css.Full),
		css.MinHeight(css.Px(64)),
		// Relative rather than static so the z-index applies at all: a z-index
		// on a static element is ignored, which is the shape this bug would
		// take if the position were dropped later.
		u.Relative,
		css.ZIndex(applicationBarLayer),
		css.PaddingX(css.Px(tokens.Spacing.XL)),
		css.Bg(css.Hex(string(tokens.Colors.Shell))),
		css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
		css.BorderBottom(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
		css.Shadow(css.ShadowOf(
			css.Zero, css.Px(8), css.Px(30), css.Px(-24), css.RGBA(0, 0, 0, 0.55),
		)),
	}
	rules = append(rules, css.Media(
		css.MaxW(1179),
		css.GridCols(
			css.MinMax(css.TrackLen(css.Zero), css.Fr(1)),
			css.TrackAuto,
		),
	)...)
	return css.New(rules...).String()
}

// instrumentStripClass is the row of workspace facts in the middle of the bar.
//
// It deliberately clips nothing. Each instrument is a disclosure whose panel is
// absolutely positioned below the bar, and a clipping ancestor trims an
// absolutely positioned descendant away whatever z-index that descendant
// declares — so the panel was being drawn and then cut off, and the button
// inside it could not be reached. Overflow is instead handled where it belongs,
// by each value ellipsizing itself, which is what contextValueClass was already
// written to do and could not while its items refused to shrink.
func instrumentStripClass() string {
	return desktopOnlyClass(
		u.Flex, u.ItemsCenter,
		css.MinWidth(css.Zero),
	)
}

func desktopOnlyClass(rules ...css.Rule) string {
	rules = append(rules, css.Media(css.MaxW(1179), css.Display.None)...)
	return css.New(rules...).String()
}

func wideOnlyClass(rules ...css.Rule) string {
	rules = append(rules, css.Media(css.MaxW(1439), css.Display.None)...)
	return css.New(rules...).String()
}

func brandMark(tokens design.Tokens) ui.Node {
	return html.Span(html.Props{
		Aria: map[string]string{"hidden": "true"},
		Class: css.New(
			u.InlineFlex, u.ItemsCenter, u.JustifyCenter,
			css.W(css.Px(30)), css.H(css.Px(30)),
			css.Rounded(css.Px(tokens.Geometry.RadiusSmall)),
			css.Bg(css.Hex(string(tokens.Colors.Accent))),
			css.TextColor(css.Hex(string(tokens.Colors.OnAccent))),
			css.FontSize(css.Px(16)),
			css.FontWeight.Bold,
		).String(),
		Text: "⌁",
	})
}

// instrumentEyebrow is the tracked sans label that names a readout.
//
// The console's facts arrive as bare values — a branch name, a model name, a
// spend — and a bare value cannot say what it is. The eyebrow above it does
// that job in the smallest type the system allows, so the value below stays the
// thing the eye lands on.
func instrumentEyebrow(label string, tokens design.Tokens) ui.Node {
	return markedEyebrow("", label, tokens)
}

// markedEyebrow is the tracked label with its own mark.
//
// Every named region in this console carries one, at one size, so the eye can
// find a region by its shape before reading a word of it.
func markedEyebrow(icon primitives.IconName, label string, tokens design.Tokens) ui.Node {
	text := html.Span(html.Props{Text: label})
	children := []ui.Node{text}
	if icon != "" {
		children = []ui.Node{
			primitives.Icon(primitives.IconProps{Name: icon, Size: primitives.IconSizeSmall}),
			text,
		}
	}
	return html.Span(html.Props{
		Aria: map[string]string{"hidden": "true"},
		Class: css.New(
			u.InlineFlex, u.ItemsCenter, css.Gap(css.Px(6)),
			css.Font(css.FontStack(tokens.Fonts.UI)),
			css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
			css.LineHeightLen(css.Px(tokens.Typography.Metadata.LineHeight)),
			css.FontWeight.Semibold,
			css.Tracking(css.Ems(0.09)),
			css.TextTransform.Uppercase,
			css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
			css.WhiteSpace.NoWrap,
		).String(),
	}, children...)
}

// instrumentGroupClass separates one readout from the next with a hairline
// rather than a box. Boxing each group turned the strip into a row of competing
// cards; a rule between them reads as one instrument with several dials.
func instrumentGroupClass(tokens design.Tokens, first bool) string {
	rules := []css.Rule{
		u.Flex, u.FlexCol,
		css.Gap(css.Px(2)),
		css.MinWidth(css.Zero),
		css.PaddingY(css.Px(tokens.Spacing.XS)),
	}
	if !first {
		gap := strconv.Itoa(tokens.Spacing.LG) + "px"
		rules = append(rules,
			css.Padding(css.RawLength("4px 0 4px "+gap)),
			css.Margin(css.RawLength("0 0 0 "+gap)),
			css.BorderLeft(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
		)
	}
	return css.New(rules...).String()
}

// liveLamp reports delivery state as a lamp with a word beside it.
//
// It is the one place the cool signal hue appears, so a live stream is
// identifiable at the edge of vision, and the word carries the same state for
// anyone who cannot use the colour.
func liveLamp(connection string, tokens design.Tokens) ui.Node {
	tone := tokens.Colors.Pending
	label := humanize(connection)
	switch state.ConnectionState(connection) {
	case state.ConnectionLive:
		tone, label = tokens.Colors.Active, "Live"
	case state.ConnectionIdle:
		tone, label = tokens.Colors.Pending, "Ready"
	case state.ConnectionReplaying:
		tone, label = tokens.Colors.Active, "Replaying"
	case state.ConnectionConnecting, state.ConnectionDegraded:
		tone = tokens.Colors.Warning
	case state.ConnectionDisconnected, state.ConnectionUnauthorized,
		state.ConnectionIncompatible:
		tone = tokens.Colors.Failure
	}
	return html.Span(html.Props{
		Data: map[string]string{"component": "live-lamp", "connection": connection},
		Aria: map[string]string{"label": "Local session " + label},
		Class: css.New(
			u.InlineFlex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.SM)),
			css.PaddingX(css.Px(tokens.Spacing.MD)),
			css.MinHeight(css.Px(30)),
			css.Rounded(css.Px(tokens.Geometry.PillRadius)),
			css.Bg(css.Hex(string(tokens.Colors.Surface1))),
			css.Border(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
			css.WhiteSpace.NoWrap,
		).String(),
	},
		html.Span(html.Props{
			Aria: map[string]string{"hidden": "true"},
			Class: css.New(
				css.W(css.Px(7)), css.H(css.Px(7)),
				css.Rounded(css.Px(tokens.Geometry.PillRadius)),
				css.Bg(css.Hex(string(tone))),
				css.Shadow(css.ShadowOf(
					css.Zero, css.Zero, css.Px(8), css.Zero, css.Hex(string(tone)),
				)),
			).String(),
		}),
		html.Span(html.Props{
			Class: css.New(
				css.Font(css.FontStack(tokens.Fonts.UI)),
				css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				css.FontWeight.Semibold,
				css.Tracking(css.Ems(0.08)),
				css.TextTransform.Uppercase,
				css.TextColor(css.Hex(string(tone))),
			).String(),
			Text: label,
		}),
	)
}

// contextValueClass styles one workspace fact as a readout that can be opened.
//
// The face carries the category: a repository is something a person named, so
// it is set in the interface sans; a branch and a working-tree state are what
// Git reports, so they are set in the monospace face the rest of the console
// uses for measured values.
func contextValueClass(kind string, tokens design.Tokens) string {
	face := tokens.Fonts.Code
	tone := tokens.Colors.TextSecondary
	weight := css.FontWeight.Medium
	if kind == "repository" {
		face = tokens.Fonts.UI
		tone = tokens.Colors.TextPrimary
		weight = css.FontWeight.Semibold
	}
	rules := []css.Rule{
		u.InlineFlex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.XS)),
		css.MinHeight(css.Px(22)),
		// Shrinkable, and allowed to shrink below its content: a flex item
		// defaults to min-width auto, which is why the ellipsis declared below
		// never engaged and the row had to be clipped by an ancestor instead.
		css.MinWidth(css.Zero),
		css.Margin(css.RawLength("0 0 0 -4px")),
		css.PaddingX(css.Px(tokens.Spacing.XS)),
		css.Rounded(css.Px(tokens.Geometry.RadiusSmall)),
		css.TextColor(css.Hex(string(tone))),
		css.Font(css.FontStack(face)),
		css.FontSize(css.Px(tokens.Typography.ControlLabel.Size)),
		weight,
		css.WhiteSpace.NoWrap,
		css.Overflow.Hidden,
		css.TextOverflowEllipsis(),
		css.Cursor.Pointer,
	}
	rules = append(rules, css.Hover(
		css.Bg(css.Hex(string(tokens.Colors.Surface2))),
		css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
	)...)
	rules = append(rules, css.FocusVisible(
		css.Outline(css.Px(tokens.Geometry.FocusRingWidth), css.Hex(string(tokens.Colors.FocusRing))),
		css.OutlineOffset(css.Px(tokens.Geometry.FocusRingOffset)),
	)...)
	return css.New(rules...).String()
}

// contextControlProps configures one instrument in the bar.
type contextControlProps struct {
	Kind       string
	Label      string
	TargetPath string
	Mode       primitives.Mode
	OnNavigate func(string)
}

// contextControl builds one instrument as its own component instance.
//
// Each instrument owns its open state, so the four in the bar cannot share one
// and open together.
func contextControl(
	kind string,
	label string,
	targetPath string,
	mode primitives.Mode,
	onNavigate func(string),
) ui.Node {
	return ui.CreateElement(ContextControl, contextControlProps{
		Kind: kind, Label: label, TargetPath: targetPath,
		Mode: mode, OnNavigate: onNavigate,
	})
}

// ContextControl is one workspace fact in the bar and the panel it opens.
//
// It was a bare <details>, which has no way to close itself: the panel stayed
// open until the summary was clicked a second time, through navigation, through
// clicking anywhere else, through Escape. The disclosure is now a button and a
// primitives.Popover, which closes on Escape, on a click outside itself, and
// returns focus to the trigger when it goes.
//
// Escape and outside-click are handled inside the overlay rather than by
// handlers here on purpose. GWC's key handlers cannot read which key was
// pressed, so an Escape rule written at this level could not exist; and closing
// on the trigger's blur would fire the moment focus moved into the panel, which
// would make the panel unreachable by keyboard — the same fault as closing on
// the trigger's mouseleave while the pointer travels toward it.
func ContextControl(props contextControlProps) ui.Node {
	tokens := props.Mode.Tokens()
	open := ui.UseState(false)
	triggerID := "context-option-trigger-" + props.Kind
	dismiss := func() { open.Set(false) }

	triggerProps := html.PropsOf(html.OnClick(func() { open.Set(!open.Get()) }))
	triggerProps.ID = triggerID
	triggerProps.Type = "button"
	triggerProps.Aria = map[string]string{
		"label":    props.Kind + " " + props.Label,
		"expanded": boolAttribute(open.Get()),
		"haspopup": "true",
	}
	// The chip became a readout. A repository, a branch and a working tree are
	// facts about where the agent is standing, so they are set as values under
	// their own labels; the disclosure they open is still there, marked by a
	// chevron that only appears on approach.
	triggerProps.Class = contextValueClass(props.Kind, tokens)

	return html.Div(html.Props{
		Data: map[string]string{"component": "context-option", "kind": props.Kind},
		// Relative anchors the panel below. MinWidth zero lets a long
		// repository name shorten this control rather than push the row past
		// the width of its grid track.
		Class: css.New(u.Relative, css.MinWidth(css.Zero)).String(),
	},
		html.Button(triggerProps,
			// The text truncates; the chevron does not. A disclosure whose
			// marker is the first thing shortened stops reading as one.
			html.Span(html.Props{
				Text: props.Label,
				Class: css.New(
					css.MinWidth(css.Zero),
					css.Overflow.Hidden,
					css.WhiteSpace.NoWrap,
					css.TextOverflowEllipsis(),
				).String(),
			}),
			primitives.Icon(primitives.IconProps{Name: primitives.IconChevronDown, Size: 11}),
		),
		contextControlPanelWhenOpen(props, open.Get(), triggerID, dismiss, tokens),
	)
}

// contextControlPanelWhenOpen mounts the panel only while it is open.
//
// A closed overlay that still mounts is what emptied a whole route frame once
// before, and nothing about a closed panel needs to exist in the document.
func contextControlPanelWhenOpen(
	props contextControlProps,
	open bool,
	triggerID string,
	dismiss func(),
	tokens design.Tokens,
) ui.Node {
	if !open {
		return html.Span(html.Props{Aria: map[string]string{"hidden": "true"}})
	}
	navigate := func() {
		dismiss()
		if props.OnNavigate != nil {
			props.OnNavigate(props.TargetPath)
		}
	}
	return primitives.Popover(primitives.OverlayProps{
		ID:              "context-option-panel-" + props.Kind,
		Open:            true,
		LabelledBy:      triggerID,
		AnchorSelector:  "#" + triggerID,
		AppRootSelector: `[data-component="app-shell"]`,
		Mode:            props.Mode,
		OnDismiss:       dismiss,
		Content: html.Div(html.Props{
			Data: map[string]string{"component": "context-option-panel"},
			Class: css.New(
				u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.SM)),
				css.MinWidth(css.Px(220)),
			).String(),
		},
			html.Strong(html.Props{Text: props.Label}),
			html.P(html.Props{
				Class: css.New(
					css.MarginY(css.Zero),
					css.TextColor(css.Hex(string(tokens.Colors.TextSecondary))),
					css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				).String(),
				Text: "Current " + props.Kind + " selection",
			}),
			primitives.Button(primitives.ButtonProps{
				Label: "Open " + map[bool]string{
					true: "repositories", false: "settings",
				}[props.TargetPath == "/"],
				Mode: props.Mode, Disabled: props.OnNavigate == nil,
				OnClick: navigate,
			}),
		),
	})
}

// costReadoutProps configures the header's money readout.
type costReadoutProps struct {
	Spent     string
	Remaining string
	Breakdown string
	// Fraction is how much of the cap has been spent, between 0 and 1. It is
	// negative when either figure is unmeasured, which draws the track without
	// a fill rather than drawing an empty bar that would read as "nothing
	// spent yet".
	Fraction   float64
	Warned     bool
	Mode       primitives.Mode
	OnNavigate func(string)
	TargetPath string
}

// costReadout renders spend against the remaining cap.
//
// Both figures are measurements, so they are set in the monospace face with
// tabular figures: they change while you watch them, and digits that shift
// their own width as they change are hard to read at a glance. The labels are
// sans, because a label is chrome rather than a value.
func costReadout(props costReadoutProps) ui.Node {
	tokens := props.Mode.Tokens()
	fill := tokens.Colors.Success
	if props.Warned {
		fill = tokens.Colors.Warning
	}
	if props.Fraction >= 1 {
		fill = tokens.Colors.Failure
	}
	rules := []css.Rule{
		u.Flex, u.FlexCol, css.Gap(css.Px(4)),
		css.MinWidth(css.Px(150)),
		css.Padding(css.RawLength("4px 8px")),
		css.Margin(css.RawLength("0 0 0 -8px")),
		css.Rounded(css.Px(tokens.Geometry.RadiusSmall)),
		css.Bg(css.Transparent),
		css.Border(css.Px(1), css.Transparent),
		css.Cursor.Pointer,
		css.TextAlign.Left,
	}
	rules = append(rules, css.Hover(css.Bg(css.Hex(string(tokens.Colors.Surface2))))...)
	rules = append(rules, css.FocusVisible(
		css.Outline(css.Px(tokens.Geometry.FocusRingWidth), css.Hex(string(tokens.Colors.FocusRing))),
		css.OutlineOffset(css.Px(tokens.Geometry.FocusRingOffset)),
	)...)
	return html.Button(html.Props{
		Type: "button",
		Data: map[string]string{
			"component": "cost-readout",
			"warned":    boolAttribute(props.Warned),
		},
		Aria:  map[string]string{"label": "Cost and budget: " + props.Breakdown},
		Class: css.New(rules...).String(),
		OnClick: ui.WrapHandler(func() {
			if props.OnNavigate != nil {
				props.OnNavigate(props.TargetPath)
			}
		}),
	},
		html.Span(html.Props{
			Class: css.New(
				u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.XS)),
				css.Font(css.FontStack(tokens.Fonts.Code)),
				css.FontSize(css.Px(tokens.Typography.ControlLabel.Size)),
				css.FontWeight.Medium,
				css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
				css.WhiteSpace.NoWrap,
			).String(),
		},
			html.Span(html.Props{Text: props.Spent}),
			html.Span(html.Props{
				Class: css.New(
					css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
					css.FontWeight.Normal,
				).String(),
				Text: "of " + props.Remaining,
			}),
		),
		budgetMeter(props.Fraction, fill, tokens),
	)
}

// budgetMeter draws spend against its cap.
//
// A cap is the one number in this console a person acts on without reading:
// the question is not what the figure is, it is how much room is left. A bar
// answers that in peripheral vision, where two currency values could not. An
// unmeasured spend leaves the track empty rather than drawing a zero-length
// fill, because a bar at zero claims a measurement.
func budgetMeter(fraction float64, fill design.Color, tokens design.Tokens) ui.Node {
	width := fraction
	if width < 0 {
		width = 0
	}
	if width > 1 {
		width = 1
	}
	// The width is a percentage, and css.Percent takes a float. Rounding to an
	// int first threw the precision away and then failed to compile anyway.
	percent := math.Round(width*1000) / 10
	children := []ui.Node{}
	if fraction >= 0 {
		children = append(children, html.Span(html.Props{
			Aria: map[string]string{"hidden": "true"},
			Class: css.New(
				css.Display.Block,
				css.H(css.Full),
				css.W(css.Percent(float64(percent))),
				css.MinWidth(css.Px(2)),
				css.Rounded(css.Px(tokens.Geometry.PillRadius)),
				css.Bg(css.Hex(string(fill))),
			).String(),
		}))
	}
	return html.Span(html.Props{
		Data: map[string]string{
			"component": "budget-meter",
			"measured":  boolAttribute(fraction >= 0),
		},
		Aria: map[string]string{"hidden": "true"},
		Class: css.New(
			css.Display.Block,
			css.W(css.Full),
			css.H(css.Px(3)),
			css.Rounded(css.Px(tokens.Geometry.PillRadius)),
			css.Bg(css.Hex(string(tokens.Colors.SurfaceInset))),
			css.Overflow.Hidden,
		).String(),
	}, children...)
}

func boolAttribute(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

// headerIconButton renders a drawn control in the application bar or a rail.
//
// The marks were Unicode glyphs — × ‹ › ⌕ ◐ ? — which rendered at six
// different weights and sizes and made a row of controls look like a row of
// accidents.
func headerIconButton(
	icon primitives.IconName,
	accessible string,
	mode primitives.Mode,
	handler func(),
) ui.Node {
	return primitives.Button(primitives.ButtonProps{
		Icon: icon, AccessibleLabel: accessible, Mode: mode, Quiet: true,
		Disabled: handler == nil, OnClick: handler,
	})
}

func headerIconButtonWithID(
	id string,
	icon primitives.IconName,
	accessible string,
	mode primitives.Mode,
	handler func(),
) ui.Node {
	return primitives.Button(primitives.ButtonProps{
		ID: id, Icon: icon, AccessibleLabel: accessible, Mode: mode, Quiet: true,
		Disabled: handler == nil, OnClick: handler,
	})
}

type SearchDialogProps struct {
	Open           bool
	Query          string
	Mode           primitives.Mode
	OnDismiss      func()
	OnQueryChange  func(string)
	OnNavigatePath func(string)
}

func SearchDialog(props SearchDialogProps) ui.Node {
	tokens := props.Mode.Tokens()
	searchProps := html.PropsOf(
		html.OnInput(func(event ui.InputEvent) {
			if props.OnQueryChange != nil {
				props.OnQueryChange(event.GetValue())
			}
		}),
	)
	searchProps.ID = "global-search-input"
	searchProps.Type = "search"
	searchProps.Placeholder = "Search tasks, graphs, memory, or repositories"
	searchProps.Aria = map[string]string{
		"label": "Search Codeflux",
	}
	searchProps.Data = map[string]string{"component": "global-search-input"}
	searchRules := []css.Rule{
		css.W(css.Full), css.MinHeight(css.Px(44)),
		css.PaddingX(css.Px(tokens.Spacing.MD)),
		css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
		css.Bg(css.Hex(string(tokens.Colors.SurfaceInset))),
		css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
		css.Border(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
		css.Font(css.FontStack(tokens.Fonts.UI)),
		css.FontSize(css.Px(tokens.Typography.Body.Size)),
	}
	searchRules = append(searchRules, css.FocusVisible(
		css.Outline(css.Px(tokens.Geometry.FocusRingWidth), css.Hex(string(tokens.Colors.FocusRing))),
		css.OutlineOffset(css.Px(tokens.Geometry.FocusRingOffset)),
	)...)
	searchProps.Class = css.New(searchRules...).String()
	destinations := []struct {
		icon  primitives.IconName
		label string
		path  string
	}{
		{icon: primitives.IconTasks, label: "Search tasks", path: "/tasks"},
		{icon: primitives.IconGraph, label: "Search task graph", path: "/graphs"},
		{icon: primitives.IconMemory, label: "Search memory", path: "/memory"},
		{icon: primitives.IconRepositories, label: "Search repositories", path: "/"},
	}
	query := strings.ToLower(strings.TrimSpace(props.Query))
	results := make([]ui.Node, 0, len(destinations))
	for _, destination := range destinations {
		destination := destination
		if query != "" && !strings.Contains(strings.ToLower(destination.label), query) {
			continue
		}
		results = append(results, primitives.Button(primitives.ButtonProps{
			Label:           destination.label,
			LeadingIcon:     destination.icon,
			AccessibleLabel: destination.label,
			Mode:            props.Mode, Disabled: props.OnNavigatePath == nil,
			OnClick: func() {
				if props.OnDismiss != nil {
					props.OnDismiss()
				}
				if props.OnNavigatePath != nil {
					props.OnNavigatePath(destination.path)
				}
			},
		}))
	}
	if len(results) == 0 {
		results = append(results, html.P(html.Props{
			Role: "status", Text: "No matching search area. Try tasks, graph, memory, or repositories.",
			Class: css.New(css.TextColor(css.Hex(string(tokens.Colors.TextSecondary)))).String(),
		}))
	}
	return primitives.Modal(primitives.ModalProps{
		ID: "global-search-dialog", Title: "Search", Icon: primitives.IconSearch,
		Description:          "Choose a scoped search destination. Your query stays on this machine.",
		Open:                 props.Open,
		Mode:                 props.Mode,
		Width:                520,
		InitialFocusSelector: "#global-search-input",
		AppRootSelector:      `[data-component="app-shell"]`,
		OnDismiss:            props.OnDismiss,
		Body: html.Div(html.Props{
			Class: css.New(u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.MD))).String(),
		},
			html.Label(html.Props{
				For: "global-search-input", Text: "Search",
				Class: shellAssistiveClass(),
			}),
			html.Input(searchProps),
			html.Div(html.Props{
				Role: "group", Aria: map[string]string{"label": "Search destinations"},
				Class: css.New(
					u.Grid, css.GridCols(css.Repeat(2, css.MinMax(css.TrackLen(css.Zero), css.Fr(1)))),
					css.Gap(css.Px(tokens.Spacing.SM)),
				).String(),
			}, results...),
		),
	})
}

type ProductSidebarProps struct {
	Snapshot         state.Snapshot
	Route            routes.Route
	SelectedScope    NavigationScope
	Mode             primitives.Mode
	ThreadRail       ui.Node
	CompactOpen      bool
	OnThreadNavigate func(routes.Route)
	OnNavigatePath   func(string)
	OnCollapse       func()
	OnNarrower       func()
	OnWider          func()
}

func ProductSidebar(props ProductSidebarProps) ui.Node {
	layout := props.Snapshot.Layout.Normalize()
	compact := layout.Viewport == state.ViewportNarrow || layout.Viewport == state.ViewportMinimum
	hidden := layout.RailCollapsed
	if compact {
		hidden = !props.CompactOpen
	}
	tokens := props.Mode.Tokens()
	threadRailNode := props.ThreadRail
	if threadRailNode == nil {
		threadRailNode = ui.CreateElement(TypedThreadRailPreview, TypedThreadRailPreviewProps{
			Snapshot: props.Snapshot, Route: props.Route, Mode: props.Mode,
			OnNavigate: props.OnThreadNavigate,
		})
	}
	if hidden && !compact {
		return html.Nav(html.Props{
			Hidden: true,
			Aria:   map[string]string{"label": "Primary navigation"},
			Data: map[string]string{
				"component": "product-sidebar",
				"viewport":  string(layout.Viewport),
			},
		})
	}
	navigationLabel := "Primary navigation"
	if layout.Viewport == state.ViewportWide {
		navigationLabel = "Threads"
	}
	// Every destination comes from the route table rather than from a literal.
	// Three of these used to be paths the client does not route -- /tasks and
	// /memory were never routes, and /graphs was one the server refused -- so
	// half the rail sent a person to a page that did not exist.
	navItems := navigationDestinations(props.Route, props.SelectedScope)
	items := make([]ui.Node, 0, len(navItems))
	for _, item := range navItems {
		selected := routeSelected(props.Route, item.label, item.path)
		path := item.path
		reachable := item.reason == ""
		buttonProps := html.PropsOf(html.OnClick(func() {
			if props.OnNavigatePath != nil && reachable {
				props.OnNavigatePath(path)
			}
			if compact && props.OnCollapse != nil {
				props.OnCollapse()
			}
		}))
		buttonProps.Type = "button"
		buttonProps.Disabled = props.OnNavigatePath == nil || !reachable
		if item.reason != "" {
			// A destination that cannot exist yet says so rather than
			// navigating to nothing.
			buttonProps.Title = item.reason
			buttonProps.Aria = map[string]string{"description": item.reason}
		}
		buttonProps.Aria = map[string]string{"current": selectedAria(selected)}
		buttonProps.Data = map[string]string{"component": "client-route-control", "path": path}
		buttonProps.Class = sidebarLinkClass(tokens, selected)
		icon := item.icon
		label := item.label
		items = append(items, html.Button(buttonProps,
			html.Span(html.Props{
				Class: css.New(
					u.InlineFlex, u.ItemsCenter, u.JustifyCenter,
					css.W(css.Px(20)), css.H(css.Px(20)),
					css.FlexShrink(css.Num(0)),
				).String(),
			}, primitives.Icon(primitives.IconProps{Name: icon, Size: 18})),
			html.Span(html.Props{Text: label}),
		))
	}
	navigation := html.Nav(html.Props{
		ID:   "product-sidebar-navigation",
		Aria: map[string]string{"label": navigationLabel},
		Data: map[string]string{
			"component": "product-sidebar",
			"viewport":  string(layout.Viewport),
			"overlay":   strconv.FormatBool(layout.Viewport == state.ViewportMedium),
			"width":     strconv.Itoa(layout.RailWidth),
		},
		Class: productSidebarClass(layout, tokens),
	},
		// The rail's own controls sit under an eyebrow rather than above the
		// navigation as three unexplained icons. Collapsing and resizing a rail
		// is housekeeping; it should not be the first thing in the tab order or
		// the first thing the eye meets.
		// One affordance, and it says what it does. The rail used to open with a
		// close cross and two resize arrows: three controls for housekeeping,
		// ahead of the navigation they belong to, and the cross read as
		// "dismiss this application" rather than "fold this rail away".
		html.Div(html.Props{
			Class: css.New(
				u.Flex, u.ItemsCenter, u.JustifyBetween,
				css.Gap(css.Px(tokens.Spacing.SM)),
				css.MinHeight(css.Px(26)),
			).String(),
		},
			markedEyebrow(primitives.IconMenu, "Navigate", tokens),
			headerIconButtonWithID("product-sidebar-close", primitives.IconChevronLeft,
				"Collapse thread rail", props.Mode, props.OnCollapse),
		),
		html.H2(html.Props{
			ID: "product-sidebar-title", Text: navigationLabel,
			Class: shellAssistiveClass(),
		}),
		html.Div(html.Props{
			Class: css.New(
				u.Flex, u.FlexCol,
				css.Margin(css.RawLength("8px 0 0")),
				css.Padding(css.Zero),
			).String(),
		}, items...),
		html.Div(html.Props{
			Class: css.New(
				u.Flex, u.FlexCol,
				css.MinHeight(css.Zero),
				css.Margin(css.RawLength(strconv.Itoa(tokens.Spacing.LG)+"px 0 0")),
				css.Padding(css.RawLength(strconv.Itoa(tokens.Spacing.LG)+"px 0 0")),
				css.BorderTop(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
				css.FlexGrow(css.Num(1)),
			).String(),
		},
			threadRailNode,
		),
		// Where the data lives is a standing fact, not a card. It is set as one
		// quiet line at the foot of the rail so it can be checked and ignored.
		html.Div(html.Props{
			Class: css.New(
				u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.SM)),
				css.Margin(css.RawLength(strconv.Itoa(tokens.Spacing.MD)+"px 0 0")),
				css.Padding(css.RawLength(strconv.Itoa(tokens.Spacing.MD)+"px 0 0")),
				css.BorderTop(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
			).String(),
		},
			html.Span(html.Props{
				Aria: map[string]string{"hidden": "true"},
				Class: css.New(
					css.W(css.Px(6)), css.H(css.Px(6)),
					css.Rounded(css.Px(tokens.Geometry.PillRadius)),
					css.Bg(css.Hex(string(tokens.Colors.Success))),
				).String(),
			}),
			html.P(html.Props{
				Class: css.New(
					css.Margin(css.Zero),
					css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
					css.Font(css.FontStack(tokens.Fonts.UI)),
					css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				).String(),
				Text: "Local SQLite · encrypted at rest",
			}),
		),
	)
	if !compact {
		return navigation
	}
	return primitives.Drawer(primitives.OverlayProps{
		ID: "product-sidebar-drawer", Open: props.CompactOpen,
		LabelledBy:           "product-sidebar-title",
		InitialFocusSelector: "#product-sidebar-close",
		AppRootSelector:      `[data-component="app-shell"]`,
		Mode:                 props.Mode,
		Content:              navigation,
		OnDismiss:            props.OnCollapse,
	})
}

func shellAssistiveClass() string {
	return css.New(
		css.Position.Absolute, css.W(css.Px(1)), css.H(css.Px(1)),
		css.Margin(css.Px(-1)), css.Padding(css.Zero), css.Overflow.Hidden,
	).String()
}

func productSidebarClass(layout state.LayoutPreferences, tokens design.Tokens) string {
	rules := []css.Rule{
		u.Flex, u.FlexCol,
		css.MinWidth(css.Zero), css.H(css.Full),
		css.PaddingY(css.Px(tokens.Spacing.LG)),
		css.PaddingX(css.Px(tokens.Spacing.MD)),
		css.Bg(css.Hex(string(tokens.Colors.Shell))),
		css.BorderRight(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
		css.OverflowY.Auto,
	}
	if layout.Viewport == state.ViewportMedium {
		rules = append(rules,
			u.Absolute,
			css.Left(css.Zero), css.Top(css.Zero), css.Bottom(css.Zero),
			css.W(css.Px(layout.RailWidth)),
			css.ZIndex(20),
			css.Shadow(css.ShadowOf(
				css.Px(8), css.Zero, css.Px(24), css.Zero, css.RGBA(0, 0, 0, 0.28),
			)),
		)
	}
	return css.New(rules...).String()
}

// routeSelected reports whether a destination is the page being looked at.
//
// A rail with two current pages tells a person nothing about where they are,
// so exactly one destination answers true for any route. The destination path
// is taken rather than derived, so this cannot drift from where a control
// actually goes.
func routeSelected(route routes.Route, label, destination string) bool {
	switch label {
	case "Repositories":
		return route.Name == routes.RepositoryChooser
	case "Tasks":
		return route.Name == routes.ThreadWorkspace
	case "Graphs":
		return route.Name == routes.Graphs
	case "Code":
		return route.Name == routes.Code
	case "Atoms":
		return route.Name == routes.Atoms
	case "Memory":
		return route.Name == routes.Memory
	case "Settings":
		return route.Name == routes.Settings
	}
	return false
}

func selectedAria(selected bool) string {
	if selected {
		return "page"
	}
	return ""
}

func sidebarLinkClass(tokens design.Tokens, selected bool) string {
	rules := []css.Rule{
		u.Flex, u.ItemsCenter,
		css.Gap(css.Px(tokens.Spacing.SM + 2)),
		css.W(css.Full),
		css.MinHeight(css.Px(34)),
		css.MarginY(css.Px(1)),
		css.PaddingX(css.Px(tokens.Spacing.SM)),
		css.TextAlign.Left,
		css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
		css.Bg(css.Transparent),
		css.Border(css.Zero, css.Transparent),
		css.TextColor(css.Hex(string(tokens.Colors.TextSecondary))),
		css.TextDecoration.None,
		css.Font(css.FontStack(tokens.Fonts.UI)),
		css.FontSize(css.Px(tokens.Typography.ControlLabel.Size)),
		css.FontWeight.Medium,
		css.Cursor.Pointer,
	}
	if tokens.Motion.Control > 0 {
		rules = append(rules, css.Transition(
			css.TransitionProps(css.PropColors),
			css.Ms(int(tokens.Motion.Control.Milliseconds())),
			css.EaseOut,
		))
	}
	rules = append(rules, css.Hover(
		css.Bg(css.Hex(string(tokens.Colors.Surface1))),
		css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
	)...)
	rules = append(rules, css.FocusVisible(
		css.Outline(css.Px(tokens.Geometry.FocusRingWidth), css.Hex(string(tokens.Colors.FocusRing))),
		css.OutlineOffset(css.Px(tokens.Geometry.FocusRingOffset)),
	)...)
	rules = append(rules, css.Disabled(
		css.TextColor(css.Hex(string(tokens.Colors.TextDisabled))),
		css.Cursor.NotAllowed,
	)...)
	if selected {
		rules = append(rules,
			css.Bg(css.Hex(string(tokens.Colors.Surface2))),
			css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
			css.FontWeight.Semibold,
			// An inset edge marks the current page without adding a border, so
			// selecting a route does not shift its label sideways. The edge is
			// the action key's own neutral, which keeps state colour free to
			// mean machine state.
			css.Shadow(css.ShadowInset(
				css.Px(2), css.Zero, css.Zero, css.Zero,
				css.Hex(string(tokens.Colors.Accent)),
			)),
		)
	}
	return css.New(rules...).String()
}

type AssuranceRailProps struct {
	Snapshot state.Snapshot
	Mode     primitives.Mode
	// Graph and Execution are what the run is doing right now. They live in
	// this rail rather than beside the transcript: a person reads the
	// transcript and watches these, and the two jobs do not want the same
	// column.
	Graph      ui.Node
	Execution  ui.Node
	Collapsed  bool
	OnCollapse func()
}

func AssuranceRail(props AssuranceRailProps) ui.Node {
	layout := props.Snapshot.Layout.Normalize()
	hidden := layout.Viewport != state.ViewportWide || props.Collapsed
	tokens := props.Mode.Tokens()
	if hidden {
		return html.Aside(html.Props{
			ID: "inspector-region", TabIndex: -1,
			Hidden: true,
			Aria:   map[string]string{"label": "Task assurance and context"},
			Data: map[string]string{
				"component": "assurance-rail", "focus-region": "inspector", "focus-order": "5",
			},
		})
	}
	return html.Aside(html.Props{
		ID: "inspector-region", TabIndex: -1,
		Aria: map[string]string{"label": "Task assurance and context"},
		Data: map[string]string{
			"component": "assurance-rail", "focus-region": "inspector", "focus-order": "5",
		},
		Class: css.New(
			u.Flex, u.FlexCol,
			css.H(css.Full),
			css.Padding(css.RawLength("16px 20px 20px")),
			css.Bg(css.Hex(string(tokens.Colors.Shell))),
			css.BorderLeft(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
			css.OverflowY.Auto,
		).String(),
	},
		html.Div(html.Props{
			Class: css.New(
				u.Flex, u.ItemsCenter, u.JustifyBetween,
				css.Padding(css.RawLength("0 0 12px")),
			).String(),
		},
			html.Strong(html.Props{
				Class: css.New(
					css.Font(css.FontStack(tokens.Fonts.UI)),
					css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
					css.FontWeight.Semibold,
					css.Tracking(css.Ems(0.09)),
					css.TextTransform.Uppercase,
					css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
				).String(),
				Text: "This run",
			}),
			headerIconButton(primitives.IconClose, "Collapse task details sidebar", props.Mode, props.OnCollapse),
		),
		// Every row below is read from the coordinator's answer. This rail used
		// to open with "Milestone M16" and "Plan reference §27A · §27C", follow
		// with four correctness gates whose verdicts were string literals, and
		// close with four artifacts reported as present without anything having
		// looked. It was the development plan for this file, printed into the
		// product, and it read as measurement.
		//
		// It also repeated the header's own strip verbatim in three bordered
		// cards. The rail now carries what the strip cannot: the state a run is
		// in, the cap it is spending against, and where its work is happening.
		runStateBlock(props.Snapshot, tokens),
		inspectorSectionWithIcon(primitives.IconSpend, "Spend", []detailRow{
			{"Spent", fallback(props.Snapshot.TopBar.ActualCost, "Unknown")},
			{"Cap", fallback(props.Snapshot.TopBar.HardBudget, "Not set")},
			{"Left", fallback(props.Snapshot.TopBar.RemainingBudget, "Unknown")},
			{"Forecast", fallback(props.Snapshot.TopBar.ForecastCost, "Unknown")},
			{"Tokens", fallback(props.Snapshot.TopBar.ActualTokens, "Unknown")},
		}, spendMeterWhenMeasured(props.Snapshot.TopBar, tokens), tokens),
		inspectorSectionWithIcon(primitives.IconTree, "Working tree", []detailRow{
			{"Repository", fallback(props.Snapshot.TopBar.Repository, "Not selected")},
			{"Branch", fallback(props.Snapshot.TopBar.Branch, "Unknown")},
			{"State", fallback(props.Snapshot.TopBar.WorktreeStatus, "Unknown")},
		}, nil, tokens),
		railPanel("Project graph", props.Graph, 0, tokens),
		railPanel("", props.Execution, 0, tokens),
	)
}

// statusTone maps a task state to the lamp colour that reports it.
//
// One mapping serves the header pill, the rail and anything else that reports
// the same state, so a run cannot be amber in one place and green in another.
func statusTone(taskState string, tokens design.Tokens) design.Color {
	lowered := strings.ToLower(strings.TrimSpace(taskState))
	switch {
	case lowered == "":
		return tokens.Colors.Pending
	case strings.Contains(lowered, "paused"), strings.Contains(lowered, "waiting"),
		strings.Contains(lowered, "review"):
		return tokens.Colors.Warning
	case strings.Contains(lowered, "complete"), strings.Contains(lowered, "accepted"):
		return tokens.Colors.Success
	case strings.Contains(lowered, "fail"), strings.Contains(lowered, "blocked"),
		strings.Contains(lowered, "error"):
		return tokens.Colors.Failure
	case strings.Contains(lowered, "no task"):
		return tokens.Colors.Pending
	default:
		return tokens.Colors.Active
	}
}

// spendMeterWhenMeasured draws the rail's meter only once both figures exist.
// An empty track under a column of "Unknown" is a dial with no needle.
func spendMeterWhenMeasured(topBar state.TopBarView, tokens design.Tokens) ui.Node {
	if topBar.SpentFraction < 0 {
		return nil
	}
	return budgetMeter(topBar.SpentFraction, budgetMeterTone(topBar, tokens), tokens)
}

// RailGraphSummary counts what the graph holds and offers the way into it.
//
// A canvas needs room to be read. Drawn into a three-hundred-and-eighty pixel
// rail it laid its node labels on top of each other and ran its edges off the
// panel, which taught a person nothing except that something was wrong with the
// interface. The rail now reports the shape of the graph — how much of it there
// is, and of what — and the graph itself has a page.
func RailGraphSummary(
	nodes []state.GraphNodeView,
	authoritative *graphcanvas.AuthoritativeProps,
	revision uint64,
	tokens design.Tokens,
	mode primitives.Mode,
	onNavigate func(string),
) ui.Node {
	// The graph view carries a status per node rather than a kind, so the
	// summary counts what a person is actually watching for: how much of the
	// map has passed, how much is still running, and what is blocked.
	// The authoritative graph is the one the coordinator is building. The
	// store's node list is the local preview and stays empty during a real
	// run, so a rail reading it reported "0 nodes" while the graph filled up.
	mapped := len(nodes)
	if authoritative != nil {
		mapped = len(authoritative.Revision.Nodes())
		if ordinal := authoritative.Revision.Metadata().Ordinal(); ordinal > revision {
			revision = ordinal
		}
	}
	counts := map[string]int{}
	for _, node := range nodes {
		status := strings.TrimSpace(node.Status)
		if status == "" {
			status = "pending"
		}
		counts[status]++
	}
	rows := make([]detailRow, 0, len(counts)+1)
	total := strconv.Itoa(mapped) + " nodes"
	if mapped == 1 {
		total = "1 node"
	}
	rows = append(rows, detailRow{"Mapped", total})
	for _, status := range []string{"running", "passed", "failed", "blocked", "pending"} {
		if count, present := counts[status]; present {
			rows = append(rows, detailRow{humanize(status), strconv.Itoa(count)})
		}
	}
	rows = append(rows, detailRow{"Revision", strconv.FormatUint(revision, 10)})
	body := make([]ui.Node, 0, len(rows)+1)
	for _, row := range rows {
		body = append(body, html.Div(html.Props{
			Class: css.New(
				u.Flex, u.ItemsCenter, u.JustifyBetween,
				css.Gap(css.Px(tokens.Spacing.MD)),
				css.MinHeight(css.Px(24)),
				css.FontSize(css.Px(tokens.Typography.CompactBody.Size)),
			).String(),
		},
			html.Span(html.Props{
				Class: css.New(
					css.Font(css.FontStack(tokens.Fonts.UI)),
					css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
				).String(),
				Text: row.label,
			}),
			html.Strong(html.Props{
				Class: css.New(
					css.Font(css.FontStack(tokens.Fonts.Code)),
					css.FontWeight.Medium,
					css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
				).String(),
				Text: row.value,
			}),
		))
	}
	body = append(body, html.Div(html.Props{
		Class: css.New(css.Padding(css.RawLength("8px 0 0")), css.Margin(css.RawLength("0 0 0 -8px"))).String(),
	}, primitives.Button(primitives.ButtonProps{
		Label: "Open the graph", LeadingIcon: primitives.IconGraph,
		AccessibleLabel: "Open the project graph", Quiet: true, Mode: mode,
		Disabled:       onNavigate == nil,
		DisabledReason: "The graph page is unavailable until the coordinator answers.",
		OnClick: func() {
			if onNavigate != nil {
				onNavigate("/graphs")
			}
		},
	})))
	// The summary carries the graph's landmark in this layout. The shell
	// promises a focus order that includes the graph region, and that promise
	// has to point at whatever is actually drawing the graph here.
	return html.Div(html.Props{
		ID: "graph-region", TabIndex: -1,
		Aria: map[string]string{"label": "Task graph"},
		Data: map[string]string{
			"component": "rail-graph-summary", "focus-region": "graph", "focus-order": "4",
		},
		Class: css.New(u.Flex, u.FlexCol, css.Gap(css.Px(2)), css.MinWidth(css.Zero)).String(),
	}, body...)
}

// railPanel gives a mounted surface its place in the observation rail.
func railPanel(title string, content ui.Node, height int, tokens design.Tokens) ui.Node {
	if content == nil {
		return nil
	}
	children := make([]ui.Node, 0, 2)
	if strings.TrimSpace(title) != "" {
		children = append(children, html.H2(html.Props{
			Class: css.New(
				css.Margin(css.RawLength("0 0 8px")),
				css.Font(css.FontStack(tokens.Fonts.UI)),
				css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				css.LineHeightLen(css.Px(tokens.Typography.Metadata.LineHeight)),
				css.FontWeight.Semibold,
				css.Tracking(css.Ems(0.09)),
				css.TextTransform.Uppercase,
				css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
			).String(),
			Text: title,
		}))
	}
	body := []css.Rule{css.MinWidth(css.Zero), css.MinHeight(css.Zero)}
	if height > 0 {
		body = append(body, css.H(css.Px(height)))
	}
	children = append(children, html.Div(html.Props{
		Class: css.New(body...).String(),
	}, content))
	return html.Section(html.Props{
		Class: css.New(
			u.Flex, u.FlexCol,
			css.Padding(css.RawLength("16px 0")),
			css.MinWidth(css.Zero),
			css.BorderBottom(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
		).String(),
	}, children...)
}

// budgetMeterTone picks the meter's fill from the warning state the coordinator
// reported rather than from the fraction, so a cap the server considers reached
// is drawn as reached even if the two figures round otherwise.
func budgetMeterTone(topBar state.TopBarView, tokens design.Tokens) design.Color {
	if topBar.BudgetWarned {
		return tokens.Colors.Warning
	}
	return tokens.Colors.Success
}

// runStateBlock is the rail's answer to the only question it exists for: what
// is this run doing, and on what.
func runStateBlock(snapshot state.Snapshot, tokens design.Tokens) ui.Node {
	taskState := strings.TrimSpace(snapshot.TopBar.TaskState)
	label := "No task yet"
	tone := tokens.Colors.Pending
	if taskState != "" {
		label = humanize(taskState)
		tone = design.Color(statusTone(taskState, tokens))
	}
	model := fallback(snapshot.TopBar.Model, "Unknown")
	effort := fallback(snapshot.TopBar.Effort, "Unknown")
	return html.Div(html.Props{
		Data: map[string]string{"component": "run-state-block", "state": taskState},
		Class: css.New(
			u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.XS)),
			css.Padding(css.RawLength("0 0 16px")),
			css.BorderBottom(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
		).String(),
	},
		html.Div(html.Props{
			Class: css.New(u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.SM))).String(),
		},
			html.Span(html.Props{
				Aria: map[string]string{"hidden": "true"},
				Class: css.New(
					css.W(css.Px(8)), css.H(css.Px(8)),
					css.Rounded(css.Px(tokens.Geometry.PillRadius)),
					css.Bg(css.Hex(string(tone))),
				).String(),
			}),
			html.Strong(html.Props{
				Class: css.New(
					css.Font(css.FontStack(tokens.Fonts.Display)),
					css.FontSize(css.Px(tokens.Typography.SectionTitle.Size)),
					css.LineHeightLen(css.Px(tokens.Typography.SectionTitle.LineHeight)),
					css.FontWeight.Normal,
					css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
				).String(),
				Text: label,
			}),
		),
		html.Span(html.Props{
			Class: css.New(
				css.Font(css.FontStack(tokens.Fonts.Code)),
				css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
			).String(),
			Text: model + " · " + effort,
		}),
	)
}

type detailRow struct {
	label string
	value string
}

// inspectorSectionWithIcon lists measured facts under one marked eyebrow.
func inspectorSectionWithIcon(
	icon primitives.IconName,
	title string,
	rows []detailRow,
	footer ui.Node,
	tokens design.Tokens,
) ui.Node {
	heading := []ui.Node{html.Span(html.Props{Text: title})}
	if icon != "" {
		heading = []ui.Node{
			primitives.Icon(primitives.IconProps{Name: icon, Size: primitives.IconSizeSmall}),
			html.Span(html.Props{Text: title}),
		}
	}
	children := []ui.Node{
		html.H2(html.Props{
			Class: css.New(
				css.Margin(css.RawLength("0 0 8px")),
				css.Font(css.FontStack(tokens.Fonts.UI)),
				css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				css.LineHeightLen(css.Px(tokens.Typography.Metadata.LineHeight)),
				css.FontWeight.Semibold,
				css.Tracking(css.Ems(0.09)),
				css.TextTransform.Uppercase,
				css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
				u.InlineFlex, u.ItemsCenter, css.Gap(css.Px(6)),
			).String(),
		}, heading...),
	}
	for _, row := range rows {
		children = append(children, html.Div(html.Props{
			Class: css.New(
				u.Flex, u.ItemsCenter, u.JustifyBetween,
				css.Gap(css.Px(tokens.Spacing.MD)),
				css.MinHeight(css.Px(26)),
				css.FontSize(css.Px(tokens.Typography.CompactBody.Size)),
			).String(),
		},
			html.Span(html.Props{
				Class: css.New(
					css.Font(css.FontStack(tokens.Fonts.UI)),
					css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
					css.WhiteSpace.NoWrap,
				).String(),
				Text: row.label,
			}),
			html.Strong(html.Props{
				Class: css.New(
					css.Font(css.FontStack(tokens.Fonts.Code)),
					css.FontWeight.Medium,
					css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
					css.MinWidth(css.Zero),
					css.Overflow.Hidden,
					css.TextOverflowEllipsis(),
					css.WhiteSpace.NoWrap,
				).String(),
				Text: row.value,
			}),
		))
	}
	if footer != nil {
		children = append(children, html.Div(html.Props{
			Class: css.New(css.Padding(css.RawLength("10px 0 2px"))).String(),
		}, footer))
	}
	return html.Section(html.Props{
		Class: css.New(
			u.Flex, u.FlexCol,
			css.Padding(css.RawLength("16px 0")),
			css.BorderBottom(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
		).String(),
	}, children...)
}

func TaskWorkspaceHeader(props TaskWorkspaceHeaderProps) ui.Node {
	tokens := props.Mode.Tokens()
	pauseLabel, pauseAccessible := taskPausePresentation(props.Snapshot.TopBar.TaskState, false)
	// A thread with no task is the ordinary state of a new conversation, not a
	// failure to load one. It used to be headed "Selected task / Unknown / No
	// authoritative task summary is available" — three sentences of system
	// vocabulary reporting an absence as a fault.
	noTask := strings.TrimSpace(props.Snapshot.TopBar.TaskState) == ""
	taskStateLabel := humanize(fallback(props.Snapshot.TopBar.TaskState, "No task"))
	taskTitle := fallback(props.Snapshot.TopBar.TaskTitle, "No task yet")
	taskSummary := fallback(props.Snapshot.TopBar.TaskSummary,
		"Describe a change and Codeflux will plan it, run it, and show its work.")
	if noTask {
		taskSummary = "Describe a change and Codeflux will plan it, run it, and show its work."
	}
	taskStateColor := statusTone(taskStateLabel, tokens)
	moreExpanded := props.TaskActionsOpen
	return html.Div(html.Props{
		DataAttr: html.DataAttribute{Name: "component", Value: "task-workspace-header"},
		Class: css.New(
			u.Flex, u.FlexCol,
			css.Gap(css.Px(6)),
			css.MinWidth(css.Zero),
		).String(),
	},
		html.Section(html.Props{
			Aria: map[string]string{"label": "Task summary"},
			Class: css.New(
				u.Flex, u.ItemsCenter, u.JustifyBetween, css.FlexWrap.Wrap,
				css.Gap(css.Px(tokens.Spacing.MD)),
				css.MinHeight(css.Px(58)),
				css.PaddingY(css.Px(tokens.Spacing.XS)),
				css.PaddingX(css.Px(tokens.Spacing.SM)),
			).String(),
		},
			// The mark that used to sit beside the title repeated the one in the
			// application bar eight centimetres away and named nothing.
			html.Div(html.Props{
				Class: css.New(u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.LG))).String(),
			},
				html.Div(html.Props{
					Class: css.New(
						u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.XS)),
					).String(),
				},
					html.Div(html.Props{
						Class: css.New(u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.SM))).String(),
					},
						html.H1(html.Props{
							Class: css.New(
								css.Margin(css.Zero),
								css.FontSize(css.Px(tokens.Typography.WorkspaceTitle.Size)),
								css.LineHeightLen(css.Px(tokens.Typography.WorkspaceTitle.LineHeight)),
								// The serif carries the title at its natural
								// weight with a hair of positive tracking.
								// Semibold and negative tracking are a sans
								// treatment; on a serif they close the counters
								// and the title reads cramped.
								css.FontWeight.Normal,
								css.Tracking(css.Ems(0.004)),
								css.Font(css.FontStack(tokens.Fonts.Display)),
								css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
							).String(),
							Text: taskTitle,
						}),
						taskStatePill(noTask, taskStateLabel, taskStateColor, tokens),
					),
					html.P(html.Props{
						Class: css.New(
							// The requirement is prose a person wrote and must
							// judge, so it is set in the reading serif rather
							// than in the interface sans.
							css.Margin(css.Zero),
							// Prose is measured, not stretched. A serif line
							// past about 80 characters loses the reader between
							// the end of one line and the start of the next.
							css.MaxWidth(css.Ch(78)),
							css.TextColor(css.Hex(string(tokens.Colors.TextSecondary))),
							css.Font(css.FontStack(tokens.Fonts.Reading)),
							css.FontSize(css.Px(tokens.Typography.Body.Size)),
							css.LineHeightLen(css.Px(tokens.Typography.Body.LineHeight)),
						).String(),
						Text: taskSummary,
					}),
				),
			),
			html.Div(html.Props{
				Class: css.New(u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.SM))).String(),
			},
				primitives.Button(primitives.ButtonProps{
					Label: pauseLabel, LeadingIcon: taskPauseIcon(props.Snapshot.TopBar.TaskState),
					AccessibleLabel: pauseAccessible, Mode: props.Mode,
					Disabled: !props.Snapshot.TopBar.CanPause || props.OnPauseRequested == nil,
					DisabledReason: runControlReason(noTask, props.Snapshot.TopBar.CanPause,
						props.OnPauseRequested != nil, "pause"),
					OnClick: props.OnPauseRequested,
				}),
				primitives.Button(primitives.ButtonProps{
					Label: "Stop", LeadingIcon: primitives.IconStop,
					AccessibleLabel: "Stop task", Mode: props.Mode,
					Disabled: !props.Snapshot.TopBar.CanStop || props.OnStopRequested == nil,
					DisabledReason: runControlReason(noTask, props.Snapshot.TopBar.CanStop,
						props.OnStopRequested != nil, "stop"),
					OnClick: props.OnStopRequested,
				}),
				primitives.Button(primitives.ButtonProps{
					Label: "Request review", LeadingIcon: primitives.IconReview,
					AccessibleLabel: "Request review", Mode: props.Mode,
					Disabled: props.OnReviewRequested == nil,
					DisabledReason: runControlReason(noTask, true,
						props.OnReviewRequested != nil, "review"),
					OnClick: props.OnReviewRequested,
				}),
				html.Div(html.Props{Class: css.New(u.Relative).String()},
					primitives.Button(primitives.ButtonProps{
						ID: "task-actions-trigger", Icon: primitives.IconMore,
						AccessibleLabel: "More task actions", Mode: props.Mode,
						Expanded: &moreExpanded, Controls: "task-actions-popover",
						Disabled: props.OnTaskActionsOpen == nil, OnClick: props.OnTaskActionsOpen,
					}),
					taskActionsPopoverWhenOpen(props),
				),
			),
		),
		taskMetricStrip(props.Snapshot.TopBar, taskStateLabel, taskStateColor, tokens),
	)
}

type TaskActionsPopoverProps struct {
	Open              bool
	Snapshot          state.Snapshot
	Mode              primitives.Mode
	OnDismiss         func()
	OnNavigatePath    func(string)
	OnPauseRequested  func()
	OnStopRequested   func()
	OnReviewRequested func()
}

// taskActionsPopoverWhenOpen mounts the overflow menu only while it is open.
//
// A closed dialog still mounts an overlay, and mounting one inside a surface
// that is itself replacing another route's surface left the entire route frame
// empty in the document: a person who clicked a thread from any other page got
// a blank workspace. Nothing about a closed menu needs to exist until it opens.
func taskActionsPopoverWhenOpen(props TaskWorkspaceHeaderProps) ui.Node {
	if !props.TaskActionsOpen {
		return html.Span(html.Props{Aria: map[string]string{"hidden": "true"}})
	}
	return ui.CreateElement(TaskActionsPopover, TaskActionsPopoverProps{
		Open: props.TaskActionsOpen, Snapshot: props.Snapshot, Mode: props.Mode,
		OnDismiss: props.OnTaskActionsDismiss, OnNavigatePath: props.OnNavigatePath,
		OnPauseRequested: props.OnPauseRequested, OnStopRequested: props.OnStopRequested,
		OnReviewRequested: props.OnReviewRequested,
	})
}

func TaskActionsPopover(props TaskActionsPopoverProps) ui.Node {
	tokens := props.Mode.Tokens()
	pauseLabel, pauseAccessible := taskPausePresentation(props.Snapshot.TopBar.TaskState, false)
	invoke := func(action func()) func() {
		if action == nil {
			return nil
		}
		return func() {
			if props.OnDismiss != nil {
				props.OnDismiss()
			}
			action()
		}
	}
	navigate := func(path string) func() {
		if props.OnNavigatePath == nil {
			return nil
		}
		return func() {
			if props.OnDismiss != nil {
				props.OnDismiss()
			}
			props.OnNavigatePath(path)
		}
	}
	return primitives.Modal(primitives.ModalProps{
		ID: "task-actions-popover", Title: "Task actions", Icon: primitives.IconMore,
		Description:          "Inspect, review, pause, or stop the current task.",
		Open:                 props.Open,
		Mode:                 props.Mode,
		Width:                420,
		InitialFocusSelector: "#task-actions-open-graph",
		AppRootSelector:      `[data-component="app-shell"]`,
		OnDismiss:            props.OnDismiss,
		Body: html.Div(html.Props{
			Data:  map[string]string{"component": "task-actions"},
			Class: css.New(u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.SM))).String(),
		},
			primitives.Button(primitives.ButtonProps{
				ID: "task-actions-open-graph", Label: "Open task graph", LeadingIcon: primitives.IconGraph,
				AccessibleLabel: "Open task graph", Mode: props.Mode,
				Disabled: props.OnNavigatePath == nil, OnClick: navigate("/graphs"),
			}),
			primitives.Button(primitives.ButtonProps{
				Label: "Request review", LeadingIcon: primitives.IconReview,
				AccessibleLabel: "Request review from task actions", Mode: props.Mode,
				Disabled: props.OnReviewRequested == nil, OnClick: invoke(props.OnReviewRequested),
			}),
			primitives.Button(primitives.ButtonProps{
				Label: pauseLabel, LeadingIcon: taskPauseIcon(props.Snapshot.TopBar.TaskState),
				AccessibleLabel: pauseAccessible + " from task actions", Mode: props.Mode,
				Disabled: !props.Snapshot.TopBar.CanPause || props.OnPauseRequested == nil,
				DisabledReason: runControlReason(
					strings.TrimSpace(props.Snapshot.TopBar.TaskState) == "",
					props.Snapshot.TopBar.CanPause, props.OnPauseRequested != nil, "pause"),
				OnClick: invoke(props.OnPauseRequested),
			}),
			primitives.Button(primitives.ButtonProps{
				Label: "Stop", LeadingIcon: primitives.IconStop,
				AccessibleLabel: "Stop task from task actions", Mode: props.Mode,
				Disabled: !props.Snapshot.TopBar.CanStop || props.OnStopRequested == nil,
				DisabledReason: runControlReason(
					strings.TrimSpace(props.Snapshot.TopBar.TaskState) == "",
					props.Snapshot.TopBar.CanStop, props.OnStopRequested != nil, "stop"),
				OnClick: invoke(props.OnStopRequested),
			}),
			primitives.Button(primitives.ButtonProps{
				Label: "Task settings", LeadingIcon: primitives.IconSettings,
				AccessibleLabel: "Open task settings", Mode: props.Mode,
				Disabled: props.OnNavigatePath == nil, OnClick: navigate("/settings"),
			}),
		),
	})
}

// runControlReason says why a run control cannot be used.
//
// A control that is neither actionable nor explained is the worst state an
// interface can be in: it looks like it works. These four sit at the top of the
// workspace and are dark most of the time, because most of the time there is
// nothing running to pause, stop, or send for review.
func runControlReason(noTask, permitted, bound bool, action string) string {
	switch {
	case noTask:
		return map[string]string{
			"pause":  "Nothing is running to pause. Describe a change below to start a task.",
			"stop":   "Nothing is running to stop. Describe a change below to start a task.",
			"review": "There is no work to review yet. Describe a change below to start a task.",
		}[action]
	case !bound:
		return "The local coordinator has not offered this control for the current run."
	case !permitted:
		return "The current task state does not allow this action."
	default:
		return ""
	}
}

func taskPausePresentation(taskState string, compact bool) (string, string) {
	if strings.Contains(strings.ToLower(strings.TrimSpace(taskState)), "paused") {
		return "Resume", "Resume task"
	}
	return "Pause", "Pause task"
}

// taskPauseIcon draws the control's own mark, so a person reading the row at a
// glance sees the shape of the action before the word.
func taskPauseIcon(taskState string) primitives.IconName {
	if strings.Contains(strings.ToLower(strings.TrimSpace(taskState)), "paused") {
		return primitives.IconPlay
	}
	return primitives.IconPause
}

func taskMetricStrip(topBar state.TopBarView, taskState string, taskStateColor design.Color, tokens design.Tokens) ui.Node {
	// With no task there is nothing to measure. This strip used to print six
	// labels against six copies of the word Unknown — the widest, boldest,
	// most colourful row on the page saying nothing at all, and drawing the eye
	// away from the one thing that was true. One sentence replaces it.
	if strings.TrimSpace(topBar.TaskState) == "" {
		// One short line rather than six labels against six copies of the word
		// Unknown. The heading above carries the invitation, so this reports
		// only the state, which is what a strip of measurements is for.
		return html.Div(html.Props{
			Data: map[string]string{"component": "task-metrics", "state": "no-task"},
			Class: css.New(
				css.Padding(css.RawLength("0 4px 4px")),
				css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
				css.Font(css.FontStack(tokens.Fonts.UI)),
				css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				css.FontWeight.Semibold,
				css.Tracking(css.Ems(0.09)),
				css.TextTransform.Uppercase,
			).String(),
		}, html.P(html.Props{
			Class: css.New(css.Margin(css.Zero)).String(),
			Text:  "No task is running",
		}))
	}
	// Three facts, not six. The forecast, the cap and what is left of it are
	// one scroll away in the run rail and one glance away in the header meter;
	// repeating all six here made the widest row on the page the least
	// informative one.
	metrics := []detailRow{
		{"State", taskState},
		{"Spent", fallback(topBar.ActualCost, "Unknown")},
		{"Tokens", fallback(topBar.ActualTokens, "Unknown")},
	}
	nodes := make([]ui.Node, 0, len(metrics))
	for index, metric := range metrics {
		// Only the task state is coloured. Colour on a metric means something
		// needs attention, and spreading it across three of six rows spent the
		// signal on values that were not asking for any.
		valueColor := tokens.Colors.TextPrimary
		if index == 0 {
			valueColor = taskStateColor
		}
		nodes = append(nodes, html.Div(html.Props{
			Class: css.New(
				u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.SM)),
				css.MinWidth(css.Zero),
			).String(),
		},
			html.P(html.Props{
				Class: css.New(
					css.Margin(css.Zero),
					css.Font(css.FontStack(tokens.Fonts.UI)),
					css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
					css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
					css.FontWeight.Semibold,
					css.Tracking(css.Ems(0.09)),
					css.TextTransform.Uppercase,
					css.WhiteSpace.NoWrap,
				).String(),
				Text: metric.label,
			}),
			html.Strong(html.Props{
				Class: css.New(
					css.Font(css.FontStack(tokens.Fonts.Code)),
					css.FontSize(css.Px(tokens.Typography.ControlLabel.Size)),
					css.FontWeight.Medium,
					css.TextColor(css.Hex(string(valueColor))),
					css.WhiteSpace.NoWrap,
				).String(),
				Text: metric.value,
			}),
		))
	}
	rules := []css.Rule{
		u.Flex, u.ItemsCenter, css.FlexWrap.Wrap,
		css.Gap(css.Px(tokens.Spacing.LG)),
		css.MinHeight(css.Px(34)),
		css.Padding(css.RawLength("0 4px")),
	}
	return html.Section(html.Props{
		Aria: map[string]string{"label": "Task metrics"},
		// The state distinguishes a strip reporting measurements from one
		// reporting that there is nothing to measure. Both are this component,
		// and something checking that the interface agrees with the coordinator
		// has to be able to tell them apart.
		Data:  map[string]string{"component": "task-metrics", "state": "measured"},
		Class: css.New(rules...).String(),
	}, nodes...)
}

// taskStatePill reports the state of a running task and stays silent when
// there is none: the heading already reads "No task yet", and a pill repeating
// it beside the heading is the same sentence twice in two typefaces.
func taskStatePill(noTask bool, label string, color design.Color, tokens design.Tokens) ui.Node {
	if noTask {
		// An empty span rather than a nil child. A nil in the middle of a
		// children list reconciles cleanly on a fresh mount and drops the whole
		// subtree when it replaces another route's surface, which is how
		// clicking a thread from another page produced a blank page.
		return html.Span(html.Props{Aria: map[string]string{"hidden": "true"}})
	}
	return statusPill(label, color, tokens)
}

func statusPill(label string, color design.Color, tokens design.Tokens) ui.Node {
	return html.Span(html.Props{
		Class: css.New(
			u.InlineFlex, u.ItemsCenter, css.Gap(css.Px(6)),
			css.MinHeight(css.Px(22)),
			css.PaddingX(css.Px(tokens.Spacing.SM)),
			css.Rounded(css.Px(tokens.Geometry.PillRadius)),
			css.Bg(css.Transparent),
			css.Border(css.Px(1), css.Hex(string(color))),
			css.TextColor(css.Hex(string(color))),
			css.Font(css.FontStack(tokens.Fonts.UI)),
			css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
			css.FontWeight.Semibold,
			css.Tracking(css.Ems(0.06)),
			css.TextTransform.Uppercase,
		).String(),
		Text: label,
	})
}

func GraphPane(props GraphPaneProps) ui.Node {
	recordRender(props.Probe, "graph-pane", props.Revision)
	tokens := props.Mode.Tokens()
	if props.Collapsed {
		return html.Aside(html.Props{
			ID: "graph-region", TabIndex: -1,
			Hidden: true,
			Aria:   map[string]string{"label": "Task graph"},
			Data: map[string]string{
				"component": "graph-pane", "focus-region": "graph", "focus-order": "4",
			},
		})
	}
	content := asyncStateContent(props.State, "task graph nodes", len(props.Nodes), props.Mode)
	if props.State == state.DataReady || props.State == state.DataPartialStale {
		height := 510
		if props.FullHeight {
			// The graph's own page gives the canvas the room the page has. A
			// six-hundred-pixel box on a nine-hundred-pixel page fitted four
			// nodes at a third of their size, which is why every node in the
			// diagram was an unreadable blank rectangle.
			height = 760
		}
		if props.Viewport == state.ViewportWide {
			height = 640
		}
		if props.Viewport == state.ViewportNarrow || props.Viewport == state.ViewportMinimum {
			height = 420
		}
		if props.Authoritative != nil {
			authoritative := *props.Authoritative
			authoritative.ResponsiveMode = string(props.Viewport)
			authoritative.VisualMode = props.Mode
			authoritative.Height = height
			authoritative.SuppressLegend = props.SuppressLegend
			content = ui.CreateElement(graphcanvas.Renderer, graphcanvas.Props{Authoritative: &authoritative})
		} else {
			content = ui.CreateElement(graphcanvas.Renderer, graphcanvas.Props{
				Nodes:          props.Nodes,
				Edges:          graphCanvasEdges(props.Nodes),
				SelectedID:     props.SelectedID,
				CurrentID:      currentGraphNodeID(props.Nodes),
				ResponsiveMode: string(props.Viewport),
				Mode:           props.Mode,
				Height:         height,
				OnSelect:       props.OnSelect,
			})
		}
		if props.Inspector != nil {
			content = html.Div(html.Props{
				Data:  map[string]string{"component": "authoritative-graph-workspace"},
				Class: css.New(u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.MD)), css.OverflowY.Auto).String(),
			}, content, props.Inspector)
		}
	}
	return html.Aside(html.Props{
		ID: "graph-region", TabIndex: -1,
		Aria: map[string]string{"label": "Task graph"},
		Data: map[string]string{
			"component":    "graph-pane",
			"state":        string(props.State),
			"revision":     strconv.FormatUint(props.Revision, 10),
			"focus-region": "graph",
			"focus-order":  "4",
		},
		Class: css.New(
			u.Flex, u.FlexCol,
			css.MinWidth(css.Zero), css.H(css.Full),
			css.Rounded(css.Px(tokens.Geometry.PanelRadius)),
			css.Bg(css.Hex(string(tokens.Colors.SurfaceInset))),
			css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
			css.Border(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
			css.Overflow.Hidden,
		).String(),
	},
		html.Div(html.Props{
			Class: css.New(
				u.Flex, u.ItemsCenter, u.JustifyBetween,
				// The row is a flex item stretched across a flex-column
				// ancestor (the Aside), and stretch alone does not stop it
				// growing past that width: without an explicit MinWidth(Zero)
				// here, the row's own auto minimum width is its content's
				// min-content size — title plus the legend's five entries at
				// full width — which pushed the row, and the panel around it,
				// past the viewport regardless of the legend's own
				// Overflow.Hidden and ellipsis. Those only clip once
				// something in the ancestor chain lets the row itself shrink
				// below that content width.
				css.MinWidth(css.Zero),
				css.MinHeight(css.Px(50)),
				css.PaddingX(css.Px(tokens.Spacing.MD)),
				css.BorderBottom(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
			).String(),
		},
			graphPanelTitle(props.Embedded, html.Div(html.Props{
				Class: css.New(
					u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.SM)),
					css.FlexShrink(css.Num(0)),
				).String(),
			},
				html.H2(html.Props{
					Class: css.New(
						css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
						css.Margin(css.Zero),
						css.Font(css.FontStack(tokens.Fonts.UI)),
						css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
						css.LineHeightLen(css.Px(tokens.Typography.Metadata.LineHeight)),
						css.FontWeight.Semibold,
						css.Tracking(css.Ems(0.09)),
						css.TextTransform.Uppercase,
						css.WhiteSpace.NoWrap,
					).String(),
					Text: "Project graph",
				}),
			)),
			// The legend names five node kinds. It gives way before the panel
			// title does, because a truncated title tells a reader nothing while
			// a truncated legend still names the kinds that fit. A surface that
			// carries its own legend suppresses this one rather than printing
			// the same five marks twice.
			graphPanelTitle(props.SuppressLegend, graphLegend(tokens)),
		),
		content,
	)
}

// graphPanelTitle drops the pane's own title when the rail above it has
// already named the panel. The hidden attribute could not do this job: the row
// is a flex container, and a display rule beats the attribute.
// graphLegend names the node kinds with the same marks the graph draws.
func graphLegend(tokens design.Tokens) ui.Node {
	entries := []struct {
		icon  primitives.IconName
		label string
		tone  design.Color
	}{
		{primitives.IconWork, "Work", tokens.Colors.Code},
		{primitives.IconTool, "Tool", tokens.Colors.Execution},
		{primitives.IconPlan, "Plan", tokens.Colors.Plan},
		{primitives.IconProof, "Proof", tokens.Colors.Validation},
		{primitives.IconMemory, "Memory", tokens.Colors.Memory},
	}
	nodes := make([]ui.Node, 0, len(entries))
	for _, entry := range entries {
		// Overflow.Hidden on the row above clips paint, not layout: a flex
		// item with no MinWidth(Zero) of its own keeps its full content-based
		// box even when the row has no room for it, and that box's real
		// position and width are what a viewport-bounds check measures. Every
		// entry, and the label inside it, needs its own MinWidth(Zero) so the
		// shrink each entry actually gets is real — this is what let the
		// "Memory" entry's own span report a right edge past the row's
		// clipped boundary and past the viewport, even though a person never
		// saw it painted there.
		nodes = append(nodes, html.Span(html.Props{
			Class: css.New(
				u.InlineFlex, u.ItemsCenter, css.Gap(css.Px(4)),
				css.MinWidth(css.Zero), css.Overflow.Hidden,
				css.TextColor(css.Hex(string(entry.tone))),
			).String(),
		},
			primitives.Icon(primitives.IconProps{Name: entry.icon, Size: primitives.IconSizeSmall}),
			html.Span(html.Props{
				Class: css.New(
					css.MinWidth(css.Zero), css.Overflow.Hidden,
					css.WhiteSpace.NoWrap, css.TextOverflowEllipsis(),
					css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
				).String(),
				Text: entry.label,
			}),
		))
	}
	return html.Div(html.Props{
		Data: map[string]string{"component": "graph-legend"},
		Class: css.New(
			u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.MD)),
			css.MinWidth(css.Zero), css.Overflow.Hidden,
			css.WhiteSpace.NoWrap, css.TextOverflowEllipsis(),
			css.Font(css.FontStack(tokens.Fonts.UI)),
			css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
		).String(),
	}, nodes...)
}

func graphPanelTitle(embedded bool, title ui.Node) ui.Node {
	if embedded {
		return nil
	}
	return title
}

func graphCanvasEdges(nodes []state.GraphNodeView) []graphcanvas.Edge {
	present := make(map[string]bool, len(nodes))
	for _, node := range nodes {
		present[node.ID] = true
	}
	candidates := []graphcanvas.Edge{
		{ID: "requirements-design", FromID: "requirements", ToID: "design", Kind: graphcanvas.EdgeData},
		{ID: "requirements-routes", FromID: "requirements", ToID: "routes", Kind: graphcanvas.EdgeData},
		{ID: "design-implementation", FromID: "design", ToID: "implementation", Kind: graphcanvas.EdgeControl},
		{ID: "routes-implementation", FromID: "routes", ToID: "implementation", Kind: graphcanvas.EdgeControl},
		{ID: "bootstrap-implementation", FromID: "bootstrap", ToID: "implementation", Kind: graphcanvas.EdgeControl},
		{ID: "plan-implementation", FromID: "plan", ToID: "implementation", Kind: graphcanvas.EdgeControl},
		{ID: "implementation-responsive", FromID: "implementation", ToID: "responsive", Kind: graphcanvas.EdgeControl},
		{ID: "implementation-browser", FromID: "implementation", ToID: "browser", Kind: graphcanvas.EdgeControl},
		{ID: "responsive-review", FromID: "responsive", ToID: "review", Kind: graphcanvas.EdgeEvidence},
		{ID: "browser-review", FromID: "browser", ToID: "review", Kind: graphcanvas.EdgeEvidence},
		{ID: "review-evidence", FromID: "review", ToID: "evidence", Kind: graphcanvas.EdgeEvidence},
	}
	edges := make([]graphcanvas.Edge, 0, len(candidates))
	for _, edge := range candidates {
		if present[edge.FromID] && present[edge.ToID] {
			edges = append(edges, edge)
		}
	}
	if len(edges) > 0 || len(nodes) < 2 {
		return edges
	}
	for index := 1; index < len(nodes); index++ {
		edges = append(edges, graphcanvas.Edge{
			ID:     "sequence-" + strconv.Itoa(index),
			FromID: nodes[index-1].ID,
			ToID:   nodes[index].ID,
			Kind:   graphcanvas.EdgeData,
		})
	}
	return edges
}

func currentGraphNodeID(nodes []state.GraphNodeView) string {
	for _, node := range nodes {
		if node.ID == "implementation" {
			return node.ID
		}
	}
	for _, node := range nodes {
		if node.Selected {
			return node.ID
		}
	}
	for _, node := range nodes {
		status := strings.ToLower(strings.TrimSpace(node.Status))
		if status == "active" || status == "running" || status == "in progress" {
			return node.ID
		}
	}
	if len(nodes) > 0 {
		return nodes[0].ID
	}
	return ""
}

// Legacy DOM graph helpers remain as non-mounted compatibility fixtures while
// the production surface is the interactive Canvas 2D renderer.
var (
	_ = graphCanvas
	_ = graphNode
	_ = graphEdges
	_ = graphArrowPoints
	_ = graphToolbar
	_ = graphToolButton
	_ = graphMinimap
	_ = graphPlacement
	_ = statusDot
)

func graphCanvas(
	nodes []state.GraphNodeView,
	selectedID string,
	tokens design.Tokens,
	onSelect func(string),
) ui.Node {
	items := make([]ui.Node, 0, len(nodes)+3)
	items = append(items, graphEdges(tokens, len(nodes)))
	for index, node := range nodes {
		items = append(items, graphNode(node, selectedID, index, tokens, onSelect))
	}
	items = append(items, graphToolbar(tokens), graphMinimap(tokens))
	return html.Div(html.Props{
		DataAttr: html.DataAttribute{Name: "component", Value: "graph-canvas"},
		Class: css.New(
			u.Relative,
			u.Grid,
			css.GridCols(css.Repeat(4, css.MinMax(css.TrackLen(css.Px(84)), css.Fr(1)))),
			css.GridRows(css.Repeat(3, css.MinMax(css.TrackLen(css.Px(62)), css.Fr(1)))),
			u.ItemsCenter,
			css.Gap(css.Px(tokens.Spacing.MD)),
			css.FlexGrow(css.Num(1)),
			css.MinHeight(css.Px(390)),
			css.Padding(css.Px(tokens.Spacing.LG)),
			css.Bg(css.Hex(string(tokens.Colors.SurfaceInset))),
			css.Overflow.Auto,
		).String(),
	}, items...)
}

func graphNode(
	node state.GraphNodeView,
	selectedID string,
	index int,
	tokens design.Tokens,
	onSelect func(string),
) ui.Node {
	selected := node.Selected
	if selectedID != "" {
		selected = node.ID == selectedID
	}
	tone, emphasis := graphNodeEmphasis(node, selected, tokens)
	rules := []css.Rule{
		u.Relative,
		u.Flex, u.ItemsCenter, u.JustifyBetween,
		css.MinHeight(css.Px(48)),
		css.PaddingX(css.Px(tokens.Spacing.MD)),
		css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
		css.Bg(css.Hex(string(tokens.Colors.Surface2))),
		css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
		css.Border(css.Px(1), css.Hex(string(tone))),
		css.FontSize(css.Px(tokens.Typography.CompactBody.Size)),
		css.FontWeight.Medium,
		css.Cursor.Pointer,
		css.ZIndex(2),
	}
	rules = append(rules, graphPlacement(index)...)
	if selected {
		rules = append(rules, css.Shadow(css.ShadowOf(
			css.Zero, css.Zero, css.Px(10), css.Zero, css.RGBA(60, 151, 255, 0.22),
		)))
	}
	children := []ui.Node{
		html.Span(html.Props{
			Text: statusDot(node.Status) + "  " + node.Title + "   ›",
		}),
	}
	selectNode := func() {
		if onSelect != nil {
			onSelect(node.ID)
		}
	}
	buttonProps := html.PropsOf(
		html.OnClick(func(ui.MouseEvent) {
			selectNode()
		}),
		html.OnKeyDown(func(event ui.KeyboardEvent) {
			if event.GetKey() == "Enter" || event.GetKey() == " " {
				event.PreventDefault()
				selectNode()
			}
		}),
	)
	buttonProps.Type = "button"
	buttonProps.Data = map[string]string{
		"node-id":         node.ID,
		"status":          node.Status,
		"selected":        strconv.FormatBool(selected),
		"visual-emphasis": emphasis,
		"position":        strconv.Itoa(index),
	}
	buttonProps.Aria = map[string]string{
		"label":   node.Title + " - " + humanize(node.Status),
		"pressed": strconv.FormatBool(selected),
	}
	buttonProps.Class = css.New(rules...).String()
	return html.Button(buttonProps, children...)
}

func graphNodeEmphasis(node state.GraphNodeView, selected bool, tokens design.Tokens) (design.Color, string) {
	if selected {
		return tokens.Colors.Active, "selected"
	}
	status := strings.ToLower(strings.TrimSpace(node.Status))
	if status == "active" || status == "running" || status == "in progress" {
		return tokens.Colors.Active, "active"
	}
	if status == "blocked" || status == "failed" {
		return tokens.Colors.Failure, "blocked"
	}
	if strings.Contains(strings.ToLower(node.Title), "evidence") {
		return tokens.Colors.Evidence, "evidence"
	}
	return tokens.Colors.BorderSubtle, "idle"
}

func graphEdges(tokens design.Tokens, nodeCount int) ui.Node {
	type edge struct {
		points string
		endX   int
		endY   int
	}
	edges := []edge{
		{points: "230,300 250,300 250,100 270,100", endX: 270, endY: 100},
		{points: "230,300 270,300", endX: 270, endY: 300},
		{points: "230,300 250,300 250,500 270,500", endX: 270, endY: 500},
		{points: "480,100 520,100", endX: 520, endY: 100},
		{points: "480,300 520,300", endX: 520, endY: 300},
		{points: "480,500 520,500", endX: 520, endY: 500},
		{points: "730,100 770,100", endX: 770, endY: 100},
		{points: "730,300 770,300", endX: 770, endY: 300},
		{points: "730,500 770,500", endX: 770, endY: 500},
	}
	if edgeCount := min(max(nodeCount-1, 0), len(edges)); edgeCount < len(edges) {
		edges = edges[:edgeCount]
	}
	color := string(tokens.Colors.BorderStrong)
	children := make([]ui.Node, 0, len(edges)*2)
	for index, item := range edges {
		children = append(children,
			html.Polyline(html.Props{
				Key: "edge-" + strconv.Itoa(index),
				Raw: map[string]any{
					"points": item.points, "fill": "none", "stroke": color,
					"stroke-width": "2", "vector-effect": "non-scaling-stroke",
				},
			}),
			html.Polygon(html.Props{
				Key: "arrow-" + strconv.Itoa(index),
				Raw: map[string]any{
					"points": graphArrowPoints(item.endX, item.endY),
					"fill":   color,
				},
			}),
		)
	}
	return html.Svg(html.Props{
		Aria: map[string]string{"hidden": "true"},
		DataAttr: html.DataAttribute{
			Name: "component", Value: "graph-directed-edges",
		},
		Class: css.New(
			u.Absolute,
			css.Inset(css.Px(tokens.Spacing.LG)),
			css.PointerEvents.None,
			css.ZIndex(1),
		).String(),
		Raw: map[string]any{
			"viewBox":             "0 0 1000 600",
			"preserveAspectRatio": "none",
		},
	}, children...)
}

func graphArrowPoints(x, y int) string {
	return strconv.Itoa(x-9) + "," + strconv.Itoa(y-6) + " " +
		strconv.Itoa(x) + "," + strconv.Itoa(y) + " " +
		strconv.Itoa(x-9) + "," + strconv.Itoa(y+6)
}

func graphToolbar(tokens design.Tokens) ui.Node {
	return html.Div(html.Props{
		Aria: map[string]string{"label": "Graph viewport controls"},
		Class: css.New(
			u.Absolute, u.Flex, u.ItemsCenter,
			css.Left(css.Px(tokens.Spacing.MD)),
			css.Bottom(css.Px(tokens.Spacing.MD)),
			css.ZIndex(4),
			css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
			css.Bg(css.Hex(string(tokens.Colors.Surface1))),
			css.Border(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
			css.Overflow.Hidden,
		).String(),
	},
		graphToolButton("Fit", "Fit graph to viewport", tokens),
		graphToolButton("−", "Zoom out", tokens),
		graphToolButton("+", "Zoom in", tokens),
	)
}

func graphToolButton(label, accessible string, tokens design.Tokens) ui.Node {
	return html.Button(html.Props{
		Type: "button",
		Aria: map[string]string{"label": accessible},
		Class: css.New(
			css.MinWidth(css.Px(38)),
			css.MinHeight(css.Px(34)),
			css.PaddingX(css.Px(tokens.Spacing.SM)),
			css.Bg(css.Transparent),
			css.TextColor(css.Hex(string(tokens.Colors.TextSecondary))),
			css.Border(css.Zero, css.Transparent),
			css.Cursor.Pointer,
			css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
		).String(),
		Text: label,
	})
}

func graphMinimap(tokens design.Tokens) ui.Node {
	return html.Div(html.Props{
		Aria: map[string]string{"label": "Graph minimap, ten nodes"},
		Class: css.New(
			u.Absolute, u.Flex, u.ItemsCenter, u.JustifyCenter,
			css.Right(css.Px(tokens.Spacing.MD)),
			css.Bottom(css.Px(tokens.Spacing.MD)),
			css.ZIndex(3),
			css.W(css.Px(92)), css.H(css.Px(52)),
			css.Rounded(css.Px(tokens.Geometry.RadiusSmall)),
			css.Bg(css.Hex(string(tokens.Colors.Surface1))),
			css.Border(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
			css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
			css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
		).String(),
		Text: "•  •  •\n  •  •",
	})
}

func graphPlacement(index int) []css.Rule {
	placements := [][2]int{
		{1, 2},
		{2, 1},
		{2, 2},
		{2, 3},
		{3, 1},
		{3, 2},
		{3, 3},
		{4, 1},
		{4, 2},
		{4, 3},
	}
	if index < 0 || index >= len(placements) {
		return nil
	}
	return []css.Rule{
		css.GridColumn(css.GridLineAt(placements[index][0])),
		css.GridRow(css.GridLineAt(placements[index][1])),
	}
}

func statusDot(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "complete", "completed", "passed":
		return "✓"
	case "active", "running", "in progress":
		return "◌"
	case "blocked", "failed":
		return "!"
	default:
		return "○"
	}
}

// navigationDestination is one rail entry and whether it can be reached.
type navigationDestination struct {
	icon  primitives.IconName
	label string
	path  string
	// reason is empty when the destination exists, and otherwise says why it
	// does not yet.
	reason string
}

// navigationDestinations builds the rail from the route table.
//
// Two destinations need a repository to exist at all: a thread workspace and a
// memory view are both scoped to one. Until a repository is selected there is
// no path to send anybody to, so the entry says why instead of navigating to a
// page the server will refuse.
// NavigationScope is what the coordinator says is open.
//
// The navigation rail needs it because the route a person is standing on does
// not always name a repository — the chooser never does — and a rail that reads
// only the route disables every scoped destination on the page every session
// begins at.
type NavigationScope struct {
	RepositoryID domain.RepositoryID
	ThreadID     domain.ThreadID
}

func navigationDestinations(
	route routes.Route,
	selected NavigationScope,
) []navigationDestination {
	const needsRepository = "Choose a repository first; this view is scoped to one."
	// Scope comes from the route when the route has one, and otherwise from
	// what the coordinator says is open. Reading the route alone meant that
	// standing on the repository chooser — the page every session starts on —
	// disabled Tasks and Memory with "Choose a repository first" while a
	// repository was already open, and the only enabled way to it was the page
	// the person was already looking at.
	repository, thread := route.RepositoryID, route.ThreadID
	if repository.IsZero() {
		repository, thread = selected.RepositoryID, selected.ThreadID
	}

	// There is no Home. It pointed at the repository chooser, which is exactly
	// where Repositories points, so two rail items lit up together and a rail
	// with two current pages tells a person nothing about where they are.
	// Pointing it at the open conversation instead only moved the collision
	// onto Tasks. Each remaining destination is somewhere the others are not.
	destinations := []navigationDestination{}

	tasks := navigationDestination{
		icon: primitives.IconTasks, label: "Tasks", reason: needsRepository,
	}
	memory := navigationDestination{
		icon: primitives.IconMemory, label: "Memory", reason: needsRepository,
	}
	// The collection is a repository's own code, so it is reachable exactly
	// when a repository is.
	collection := navigationDestination{
		icon: primitives.IconGraph, label: "Code", reason: needsRepository,
	}
	atoms := navigationDestination{
		icon: primitives.IconTool, label: "Atoms", reason: needsRepository,
	}
	if !repository.IsZero() {
		if !thread.IsZero() {
			tasks = navigationDestination{
				icon: primitives.IconTasks, label: "Tasks",
				path: pathOrEmpty(routes.Route{
					Name: routes.ThreadWorkspace, RepositoryID: repository, ThreadID: thread,
				}),
			}
		} else {
			tasks.reason = "Open a thread first; the task workspace is scoped to one."
		}
		memory = navigationDestination{
			icon: primitives.IconMemory, label: "Memory",
			path: pathOrEmpty(routes.Route{Name: routes.Memory, RepositoryID: repository}),
		}
		collection = navigationDestination{
			icon: primitives.IconGraph, label: "Code",
			path: pathOrEmpty(routes.Route{Name: routes.Code, RepositoryID: repository}),
		}
		atoms = navigationDestination{
			icon: primitives.IconTool, label: "Atoms",
			path: pathOrEmpty(routes.Route{Name: routes.Atoms, RepositoryID: repository}),
		}
	}

	destinations = append(destinations,
		tasks,
		navigationDestination{
			icon: primitives.IconGraph, label: "Graphs",
			path: pathOrEmpty(routes.Route{Name: routes.Graphs}),
		},
		collection,
		atoms,
		memory,
		navigationDestination{
			icon: primitives.IconRepositories, label: "Repositories",
			path: pathOrEmpty(routes.Route{Name: routes.RepositoryChooser}),
		},
		navigationDestination{
			icon: primitives.IconSettings, label: "Settings",
			path: pathOrEmpty(routes.Route{Name: routes.Settings}),
		},
	)
	return destinations
}

// pathOrEmpty renders a route, or nothing when it cannot be rendered.
//
// An unrenderable route yields an empty path, which the rail treats as
// unreachable. That is better than a literal: a route the table cannot express
// is one the server will refuse.
func pathOrEmpty(route routes.Route) string {
	path, err := routes.Path(route)
	if err != nil {
		return ""
	}
	return path
}
