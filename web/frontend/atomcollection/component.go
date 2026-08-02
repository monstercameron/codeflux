// Package atomcollection renders the repository's atoms: every declaration
// admitted as an atom, and the documentation each one carries about when to
// use it, what it promises, and how it fails.
//
// It receives already-projected rows and holds no state. The code collection
// answers "what is in this package"; this surface answers "what can be called,
// what does it promise, and what did it refuse to promise", which is the
// question somebody has before letting an agent call it.
package atomcollection

import (
	"strconv"
	"strings"

	"codeflux.dev/codeflux/web/frontend/design"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/css/u"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// LoadState is what the surface currently knows about its subject.
type LoadState string

const (
	LoadUnavailable LoadState = "unavailable"
	LoadLoading     LoadState = "loading"
	LoadReady       LoadState = "ready"
	LoadFailed      LoadState = "failed"
)

// AtomRow is one declaration in the collection.
//
// Problem carries why a declaration that asked to be an atom was refused.
// Those rows are kept rather than filtered out: a directive that did not
// admit is the single most useful thing this surface can show an author,
// and hiding it would leave them wondering why their atom never appeared.
type AtomRow struct {
	Key        string
	Name       string
	Kind       string
	Receiver   string
	ImportPath string
	Path       string
	Line       uint32
	Admitted   bool
	Problem    string
	// MatchedName and MatchedPromise say why a search returned this row. A
	// result list that cannot say why a row is in it is one a person has to
	// take on faith: searching "stock" returns every atom whose name contains
	// it, which is correct and reads exactly like a search that did nothing.
	MatchedName    bool
	MatchedPromise string
}

// AtomField is one documented field of an admitted atom, as written.
type AtomField struct {
	Label string
	Text  string
	Items []string
}

// Reference is one declaration on either side of a call edge.
type Reference struct {
	Name string
	Path string
	Line uint32
}

// AtomDetail is everything the inspector shows about one declaration.
type AtomDetail struct {
	Row             AtomRow
	Signature       string
	Documentation   []string
	OpeningSentence string
	SchemaVersion   uint32
	Fields          []AtomField
	Callers         []Reference
	Callees         []Reference
	// Tests are the declarations in test files that reference this atom. They
	// answer "what exercises this", which is a different question from "what
	// depends on this" and the only one of the two that is evidence.
	Tests         []Reference
	Implements    []string
	ImplementedBy []string
	// Source is the declaration exactly as written, comment included, and
	// SourceStartLine is the file line it begins at.
	Source          string
	SourceStartLine uint32
	SourceTruncated bool
	// NamingChecked reports that the product's own atom-naming grammar was run
	// against the name, and NamingFinding states the violation when there is
	// one.
	NamingChecked bool
	NamingFinding string
	// Structure is what the run's own atom stages measure: the loop nesting
	// that becomes a time bound, the decision points that become cases to
	// cover, and whether the declaration reaches outside its arguments.
	Structure Structure
	// DocumentedFields and MissingFields split the schema's canonical field
	// list by whether this atom declares them.
	DocumentedFields []string
	MissingFields    []string
	// AccessQuery reads this atom back out of the coordinator's atom index and
	// IndexSchema is the table it runs against. Both are the statements the
	// coordinator itself executes.
	AccessQuery string
	IndexSchema string
}

// Structure is one declaration's measured shape.
//
// Measured separates "nothing was measured" from "measured, and the answer is
// zero": a declaration with no loops is O(1), one nobody looked at is unknown,
// and those are not the same claim.
type Structure struct {
	Measured   bool
	TimeBound  string
	SpaceClaim string
	LoopDepth  uint32
	Branches   uint32
	Pure       bool
}

// Props drives the atom collection surface.
type Props struct {
	Tokens design.Tokens

	State        LoadState
	ErrorMessage string
	Rows         []AtomRow
	// Revision and Dirty say which state of the tree was read. A directory of
	// code that does not name its revision will eventually describe a tree
	// that moved on.
	Revision string
	Dirty    bool
	Warnings []string
	// Search and PackageFilter are applied by the caller, not here: the
	// collection can be larger than one page, so filtering has to happen where
	// the query is made.
	Search        string
	PackageFilter string
	Packages      []string
	ShowRefused   bool

	// SearchQuery is the SQL the current search performed, empty when nothing
	// was searched, and OnCopy puts a query on the clipboard.
	SearchQuery string
	OnCopy      func(string)
	// CopiedQuery is the statement most recently put on the clipboard, so the
	// control that did it can say so. A copy with no acknowledgement leaves a
	// person pressing it twice to find out whether it worked.
	CopiedQuery string
	// SearchPending reports that the typed term has not reached the coordinator
	// yet, so the list is showing what could be narrowed locally rather than
	// the full answer. Saying so is the difference between "no atom promises
	// that" and "not asked yet".
	SearchPending bool
	// NotesExpanded controls the folded raw comment. It lives in the caller so
	// this package can stay free of state.
	NotesExpanded bool
	OnNotesToggle func(bool)

	SelectedKey  string
	Detail       *AtomDetail
	DetailState  LoadState
	DetailError  string
	TotalAtoms   uint32
	TotalRefused uint32
	// CollectionAtoms is how many atoms the repository has, as opposed to how
	// many the current search matched, and Searched says which of the two
	// TotalAtoms currently is.
	CollectionAtoms uint32
	Searched        bool
	OnSearch        func(string)
	OnPackage       func(string)
	OnShowRefused   func(bool)
	OnSelect        func(string)
	OnRetry         func()
}

// Component renders the atom collection.
func Component(props Props) ui.Node {
	tokens := props.Tokens
	mode := primitives.Mode{
		Theme: tokens.Theme, Density: tokens.Density,
		HighContrast: tokens.Theme == design.ThemeHighContrast, ReducedMotion: tokens.ReducedMotion,
	}
	// The list is the index and the inspector is the subject: an atom's code,
	// its documented promises, and the tests that hold it to them need the
	// width. A row needs only its name and where it lives.
	columns := []css.Track{css.MinMax(css.TrackLen(css.Zero), css.Fr(1))}
	body := []ui.Node{listRegion(props, mode)}
	if props.State == LoadReady && len(props.Rows) > 0 {
		columns = []css.Track{
			css.TrackLen(css.Px(400)),
			css.MinMax(css.TrackLen(css.Zero), css.Fr(1)),
		}
		body = append(body, detailRegion(props, mode))
	}
	return html.Main(html.Props{
		ID: "main-content", TabIndex: -1,
		Data: map[string]string{
			"component": "atom-collection-shell", "state": string(stateOrUnavailable(props.State)),
			"focus-region": "conversation", "focus-order": "2",
			"atom-count": strconv.Itoa(len(props.Rows)),
		},
		Class: css.New(
			u.Grid,
			css.GridRows(css.TrackAuto, css.MinMax(css.TrackLen(css.Zero), css.Fr(1))),
			css.Gap(css.Px(tokens.Spacing.MD)),
			css.W(css.Full), css.H(css.Full),
			css.MinWidth(css.Zero), css.MinHeight(css.Zero),
			css.Padding(css.RawLength("12px 20px 20px")),
			css.Bg(css.Hex(string(tokens.Colors.Canvas))),
			css.Overflow.Hidden,
		).String(),
	},
		header(props, tokens),
		html.Div(html.Props{
			Class: css.New(
				u.Grid, css.GridCols(columns...),
				css.Gap(css.Px(tokens.Spacing.LG)),
				css.MinWidth(css.Zero), css.MinHeight(css.Zero),
				css.H(css.Full), css.Overflow.Hidden,
			).String(),
		}, body...),
	)
}

func stateOrUnavailable(value LoadState) LoadState {
	if value == "" {
		return LoadUnavailable
	}
	return value
}

func header(props Props, tokens design.Tokens) ui.Node {
	return html.Header(html.Props{
		Aria:  map[string]string{"label": "Atoms"},
		Class: css.New(u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.MD))).String(),
	},
		html.Div(html.Props{Class: css.New(u.Flex, u.FlexCol, css.Gap(css.Px(2))).String()},
			html.H1(html.Props{
				Class: css.New(
					css.Margin(css.Zero),
					css.Font(css.FontStack(tokens.Fonts.Display)),
					css.FontSize(css.Px(tokens.Typography.TaskTitle.Size)),
					css.LineHeightLen(css.Px(tokens.Typography.TaskTitle.LineHeight)),
					css.FontWeight.Normal,
					css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
				).String(),
				Text: "Atoms",
			}),
			html.P(html.Props{
				Class: css.New(
					css.Margin(css.Zero),
					css.Font(css.FontStack(tokens.Fonts.Code)),
					css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
					css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
				).String(),
				Text: summaryLine(props),
			}),
		),
	)
}

// summaryLine counts what is documented, what asked to be and was refused, and
// which revision of the tree was read.
func summaryLine(props Props) string {
	switch stateOrUnavailable(props.State) {
	case LoadLoading:
		return "Reading the collection…"
	case LoadFailed:
		return "The collection could not be read"
	case LoadUnavailable:
		return "No repository is open"
	}
	parts := []string{countPhrase(int(props.TotalAtoms), "documented atom")}
	switch {
	case props.Searched:
		// A search reports what it found against what there is. Reporting only
		// the matches said the repository had as many atoms as the search
		// happened to return.
		found := strconv.Itoa(len(props.Rows)) + " matching"
		if props.CollectionAtoms > 0 {
			found += " of " + countPhrase(int(props.CollectionAtoms), "documented atom")
		} else {
			found += " " + countPhrase(len(props.Rows), "atom")
		}
		parts = []string{found}
	case len(props.Rows) != int(props.TotalAtoms)+int(props.TotalRefused):
		parts = []string{
			strconv.Itoa(len(props.Rows)) + " shown of " +
				countPhrase(int(props.TotalAtoms), "documented atom"),
		}
	}
	if props.TotalRefused > 0 {
		parts = append(parts, countPhrase(int(props.TotalRefused), "refused directive"))
	}
	if revision := strings.TrimSpace(props.Revision); revision != "" {
		state := "at " + shortRevision(revision)
		if props.Dirty {
			state += " with uncommitted changes"
		}
		parts = append(parts, state)
	}
	return strings.Join(parts, " · ")
}

func countPhrase(count int, noun string) string {
	if count == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(count) + " " + noun + "s"
}

func shortRevision(revision string) string {
	const shortLength = 7
	if len(revision) <= shortLength {
		return revision
	}
	return revision[:shortLength]
}

func listRegion(props Props, mode primitives.Mode) ui.Node {
	tokens := props.Tokens
	children := []ui.Node{controls(props, mode)}
	children = append(children, listContent(props, mode)...)
	return html.Section(html.Props{
		Aria:     map[string]string{"label": "Atoms in this repository"},
		DataAttr: html.DataAttribute{Name: "region", Value: "atom-list"},
		Data: map[string]string{
			"component": "atom-list", "row-count": strconv.Itoa(len(props.Rows)),
		},
		Class: css.New(
			u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.SM)),
			css.MinWidth(css.Zero), css.MinHeight(css.Zero),
			css.H(css.Full), css.OverflowY.Auto,
		).String(),
	}, children...)
}

// controls are the search, the package filter, and the refused toggle. They
// are drawn even while the list is empty so a search that matched nothing can
// be corrected without reloading the page.
// controls are drawn whenever a repository is open, in every load state.
//
// They used to render only when the collection was ready, which meant that the
// moment a search reached the coordinator the field the person was typing into
// was removed from the document. Focus went with it, the caret went with it,
// and every character typed after that landed nowhere — so the search looked
// broken and the results looked unfiltered, because the term never finished
// arriving.
func controls(props Props, mode primitives.Mode) ui.Node {
	if props.State == LoadUnavailable {
		return nil
	}
	tokens := props.Tokens
	searchField := []ui.Node{primitives.TextField(primitives.TextFieldProps{
		ID: "atom-search", AccessibleLabel: "Search atoms",
		Placeholder: "Search names and promises",
		Value:       props.Search, Mode: mode, OnInput: props.OnSearch,
	})}
	// The clear control appears only when there is something to clear, so the
	// field is not carrying a permanently dead button, and it sits inside the
	// field's own box so it reads as part of it.
	if strings.TrimSpace(props.Search) != "" && props.OnSearch != nil {
		searchField = append(searchField, clearSearchControl(props, mode))
	}
	children := []ui.Node{
		html.Div(html.Props{
			Class: css.New(
				css.Position.Relative,
				css.FlexGrow(css.Num(1)), css.MinWidth(css.Px(220)),
			).String(),
		}, searchField...),
	}
	if len(props.Packages) > 1 {
		children = append(children, packageFilter(props, mode))
	}
	if props.OnShowRefused != nil {
		// The control is always offered rather than appearing once refusals are
		// known: the refusals are only read when it is pressed, so hiding it
		// until then would mean it could never appear.
		children = append(children, primitives.ToggleButton(primitives.ToggleButtonProps{
			Label:   refusedLabel(props),
			Pressed: props.ShowRefused, Mode: mode,
			OnToggle: func(bool) {
				if props.OnShowRefused != nil {
					props.OnShowRefused(!props.ShowRefused)
				}
			},
		}))
	}
	return html.Div(html.Props{
		Data: map[string]string{"component": "atom-controls"},
		Class: css.New(
			u.Flex, u.ItemsCenter, css.FlexWrap.Wrap, css.Gap(css.Px(tokens.Spacing.XS)),
		).String(),
	}, children...)
}

// refusedLabel names the control and, once the wider read has happened, how
// many declarations asked to be atoms and were refused.
func refusedLabel(props Props) string {
	if props.ShowRefused {
		return "Refused directives (" + strconv.Itoa(int(props.TotalRefused)) + ")"
	}
	return "Show refused directives"
}

// clearSearchControl empties the field and returns the whole collection.
//
// Selecting the text and deleting it is the same action in three steps, and on
// a surface whose search is the way in, the way back out should be one.
func clearSearchControl(props Props, mode primitives.Mode) ui.Node {
	tokens := props.Tokens
	// The control is drawn here rather than through IconButton, which renders
	// its Icon as literal text: passing it a mark's name printed the word
	// "close" inside the field.
	rules := []css.Rule{
		u.InlineFlex, u.ItemsCenter, u.JustifyCenter,
		css.W(css.Px(24)), css.H(css.Px(24)),
		css.Padding(css.Zero),
		css.Bg(css.Transparent),
		css.Border(css.Px(0), css.Hex(string(tokens.Colors.BorderSubtle))),
		css.Rounded(css.Px(tokens.Geometry.RadiusSmall)),
		css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
		css.Cursor.Pointer,
	}
	rules = append(rules, css.Hover(
		css.Bg(css.Hex(string(tokens.Colors.Surface2))),
		css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
	)...)
	rules = append(rules, css.FocusVisible(
		css.Outline(css.Px(tokens.Geometry.FocusRingWidth), css.Hex(string(tokens.Colors.FocusRing))),
		css.OutlineOffset(css.Px(1)),
	)...)
	buttonProps := html.PropsOf(html.OnClick(func() { props.OnSearch("") }))
	buttonProps.ID = "atom-search-clear"
	buttonProps.Type = "button"
	buttonProps.Title = "Clear the search"
	buttonProps.Aria = map[string]string{"label": "Clear the search"}
	buttonProps.Class = css.New(rules...).String()
	return html.Div(html.Props{
		Data: map[string]string{"component": "atom-search-clear"},
		Class: css.New(
			css.Position.Absolute,
			css.Right(css.Px(6)), css.Top(css.Px(7)),
		).String(),
	}, html.Button(buttonProps,
		primitives.Icon(primitives.IconProps{
			Name: primitives.IconClose, Size: primitives.IconSizeSmall,
		}),
	))
}

func packageFilter(props Props, mode primitives.Mode) ui.Node {
	tokens := props.Tokens
	children := []ui.Node{packageButton(props, "", "All packages", mode)}
	for _, importPath := range props.Packages {
		children = append(children, packageButton(props, importPath, shortImportPath(importPath), mode))
	}
	return html.Div(html.Props{
		Role: "group",
		Aria: map[string]string{"label": "Filter atoms by package"},
		Data: map[string]string{"component": "atom-package-filter"},
		Class: css.New(
			u.Flex, u.ItemsCenter, css.FlexWrap.Wrap, css.Gap(css.Px(tokens.Spacing.XS)),
		).String(),
	}, children...)
}

func packageButton(props Props, importPath, label string, mode primitives.Mode) ui.Node {
	return primitives.ToggleButton(primitives.ToggleButtonProps{
		Label: label, Pressed: props.PackageFilter == importPath, Mode: mode,
		OnToggle: func(bool) {
			if props.OnPackage != nil {
				props.OnPackage(importPath)
			}
		},
	})
}

// shortImportPath keeps the part of an import path that distinguishes it. A
// filter row of twelve identical module prefixes is a filter nobody can read.
func shortImportPath(importPath string) string {
	trimmed := strings.TrimSuffix(importPath, "/")
	if index := strings.LastIndex(trimmed, "/"); index >= 0 && index+1 < len(trimmed) {
		return trimmed[index+1:]
	}
	return trimmed
}

func listContent(props Props, mode primitives.Mode) []ui.Node {
	switch stateOrUnavailable(props.State) {
	case LoadLoading:
		// A reload with rows already on screen keeps them. Blanking a list to
		// answer a narrower question about the same list makes the page jump
		// under the reader every time they type.
		if len(props.Rows) > 0 {
			break
		}
		return []ui.Node{primitives.InlineAlert(primitives.InlineAlertProps{
			Title: "Reading the collection", Tone: design.StatusNeutral, Mode: mode,
			Message: "Parsing the repository at its current revision.",
		})}
	case LoadFailed:
		return []ui.Node{primitives.ErrorState(primitives.ErrorStateProps{
			Title: "Atoms unavailable", Body: failureBody(props.ErrorMessage), Mode: mode,
			ActionLabel: retryLabel(props.OnRetry), OnAction: props.OnRetry,
		})}
	case LoadUnavailable:
		return []ui.Node{primitives.EmptyState(primitives.EmptyStateProps{
			Title: "No repository is open", Mode: mode,
			Body: "Atoms are read from a repository's own source. Open one to explore them.",
		})}
	}
	if len(props.Rows) == 0 {
		return []ui.Node{emptyList(props, mode)}
	}
	items := make([]ui.Node, 0, len(props.Rows)+1)
	if props.State == LoadLoading || props.SearchPending {
		items = append(items, refreshingNotice(props.Tokens))
	}
	for _, row := range props.Rows {
		items = append(items, atomRow(props, row, mode))
	}
	return []ui.Node{html.Div(html.Props{
		Role: "list",
		Class: css.New(
			u.Flex, u.FlexCol, css.Gap(css.Px(props.Tokens.Spacing.XS)),
		).String(),
	}, items...)}
}

// refreshingNotice marks a list that is about to be replaced, in place, at one
// line. It exists so the surface can stay still while it answers.
func refreshingNotice(tokens design.Tokens) ui.Node {
	return html.P(html.Props{
		Role: "status",
		Aria: map[string]string{"live": "polite"},
		Data: map[string]string{"component": "atom-refreshing"},
		Class: css.New(
			css.Margin(css.Zero),
			css.Font(css.FontStack(tokens.Fonts.Code)),
			css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
			css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
		).String(),
		Text: "Searching every atom's documented promises…",
	})
}

// emptyList distinguishes a repository with no atoms from a search that
// matched none of them. They call for opposite next actions.
func emptyList(props Props, mode primitives.Mode) ui.Node {
	if props.SearchPending {
		return primitives.InlineAlert(primitives.InlineAlertProps{
			Title: "Searching", Tone: design.StatusNeutral, Mode: mode,
			Message: "Looking through every atom's documented promises, not just their names.",
		})
	}
	if strings.TrimSpace(props.Search) != "" || props.PackageFilter != "" {
		return primitives.EmptyState(primitives.EmptyStateProps{
			Title: "Nothing matched", Mode: mode,
			Body: "No atom in this repository has that in its name, its package, or anything it promises. " +
				"Clear the search to see the whole collection.",
		})
	}
	return primitives.EmptyState(primitives.EmptyStateProps{
		Title: "No atom is documented yet", Mode: mode,
		Body: "An atom is a declaration carrying the //codeflux:atom directive and a " +
			"schema-versioned comment stating when to use it, what it promises, and how it fails. " +
			"Nothing in this repository carries one yet.",
	})
}

func failureBody(message string) string {
	if strings.TrimSpace(message) == "" {
		return "The coordinator did not answer the collection read."
	}
	return message
}

func retryLabel(retry func()) string {
	if retry == nil {
		return ""
	}
	return "Try again"
}

func atomRow(props Props, row AtomRow, mode primitives.Mode) ui.Node {
	tokens := props.Tokens
	selected := row.Key == props.SelectedKey
	background := tokens.Colors.Surface1
	border := tokens.Colors.BorderSubtle
	if selected {
		background = tokens.Colors.Surface2
		border = tokens.Colors.Active
	}
	rules := []css.Rule{
		u.Flex, u.FlexCol, css.Gap(css.Px(4)),
		css.W(css.Full), css.TextAlign.Left,
		css.Padding(css.RawLength("8px 12px")),
		css.Bg(css.Hex(string(background))),
		css.Border(css.Px(1), css.Hex(string(border))),
		css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
		css.Cursor.Pointer,
	}
	rules = append(rules, css.Hover(
		css.Bg(css.Hex(string(tokens.Colors.Surface2))),
		css.BorderColor(css.Hex(string(tokens.Colors.BorderStrong))),
	)...)
	rules = append(rules, css.FocusVisible(
		css.Outline(css.Px(tokens.Geometry.FocusRingWidth), css.Hex(string(tokens.Colors.FocusRing))),
		css.OutlineOffset(css.Px(2)),
	)...)
	buttonProps := html.PropsOf(html.OnClick(func() {
		if props.OnSelect != nil {
			props.OnSelect(row.Key)
		}
	}))
	buttonProps.Type = "button"
	buttonProps.Aria = map[string]string{"pressed": pressedValue(selected)}
	buttonProps.Data = map[string]string{
		"component": "atom-row", "atom-key": row.Key,
		// The name is published as an attribute as well as drawn, so a test
		// asserting which atoms a search returned reads the declaration rather
		// than guessing at which line of the row's text is the name.
		"atom-name": row.Name,
		"admitted":  pressedValue(row.Admitted), "selected": pressedValue(selected),
	}
	buttonProps.Class = css.New(rules...).String()
	marks := []ui.Node{
		primitives.IconChip(primitives.IconChipProps{
			Label: row.Kind, Kind: design.KindCode, Mode: mode,
		}),
	}
	if !row.Admitted {
		marks = append(marks, primitives.Badge(primitives.BadgeProps{
			Label: "Refused", Status: design.StatusWarning, Mode: mode,
		}))
	}
	return html.Div(html.Props{Role: "listitem"},
		html.Button(buttonProps,
			html.Span(html.Props{
				Class: css.New(u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.XS)), css.FlexWrap.Wrap).String(),
			}, marks...),
			html.Span(html.Props{
				Class: css.New(
					css.Font(css.FontStack(tokens.Fonts.Code)),
					css.FontSize(css.Px(tokens.Typography.Body.Size)),
					css.LineHeightLen(css.Px(tokens.Typography.Body.LineHeight)),
					css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
					css.OverflowWrap.Anywhere,
				).String(),
				Text: qualifiedName(row),
			}),
			html.Span(html.Props{
				Class: css.New(
					css.Font(css.FontStack(tokens.Fonts.Code)),
					css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
					css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
					css.OverflowWrap.Anywhere,
				).String(),
				Text: rowMetaLine(row),
			}),
			matchReason(row, tokens),
		),
	)
}

// matchReason says why a search returned this row.
//
// A name match needs no explanation: the word is in the heading the reader is
// looking at. A match on what the atom promises does, because otherwise the
// row looks unrelated to what was typed — and a correct result that looks
// unrelated reads as a broken search.
func matchReason(row AtomRow, tokens design.Tokens) ui.Node {
	promise := strings.TrimSpace(row.MatchedPromise)
	if promise == "" {
		return nil
	}
	label := "promises"
	if row.MatchedName {
		label = "also promises"
	}
	return html.Span(html.Props{
		Data: map[string]string{"component": "atom-match", "matched": label},
		Class: css.New(
			u.Flex, css.Gap(css.Px(tokens.Spacing.XS)),
			css.MarginY(css.Px(2)),
			css.Font(css.FontStack(tokens.Fonts.Reading)),
			css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
			css.LineHeightLen(css.Px(tokens.Typography.Body.LineHeight)),
			css.TextColor(css.Hex(string(tokens.Colors.TextSecondary))),
		).String(),
	},
		html.Span(html.Props{
			Class: css.New(
				css.FlexShrink(css.Num(0)),
				css.Font(css.FontStack(tokens.Fonts.UI)),
				css.FontWeight.Semibold,
				css.TextTransform.Uppercase,
				css.Tracking(css.Ems(0.08)),
				css.TextColor(css.Hex(string(tokens.Colors.Active))),
			).String(),
			Text: label,
		}),
		html.Span(html.Props{Text: promise}),
	)
}

// qualifiedName renders a method as its receiver carries it, because two
// packages can both declare Close and only the receiver tells them apart.
func qualifiedName(row AtomRow) string {
	if receiver := strings.TrimSpace(row.Receiver); receiver != "" {
		return "(" + receiver + ") " + row.Name
	}
	return row.Name
}

func rowMetaLine(row AtomRow) string {
	parts := make([]string, 0, 3)
	if importPath := strings.TrimSpace(row.ImportPath); importPath != "" {
		parts = append(parts, importPath)
	}
	if path := strings.TrimSpace(row.Path); path != "" {
		location := path
		if row.Line > 0 {
			location += ":" + strconv.FormatUint(uint64(row.Line), 10)
		}
		parts = append(parts, location)
	}
	return strings.Join(parts, " · ")
}

func pressedValue(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func detailRegion(props Props, mode primitives.Mode) ui.Node {
	tokens := props.Tokens
	return html.Aside(html.Props{
		Aria: map[string]string{"label": "Selected atom"},
		Data: map[string]string{
			"component": "atom-detail", "state": string(stateOrUnavailable(props.DetailState)),
		},
		Class: css.New(
			u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.MD)),
			css.MinWidth(css.Zero), css.MinHeight(css.Zero),
			css.H(css.Full), css.OverflowY.Auto,
			css.Padding(css.RawLength("0 0 0 20px")),
			css.BorderLeft(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
		).String(),
	}, detailContent(props, mode)...)
}

func detailContent(props Props, mode primitives.Mode) []ui.Node {
	tokens := props.Tokens
	eyebrow := eyebrowLabel("Selected atom", tokens)
	switch stateOrUnavailable(props.DetailState) {
	case LoadLoading:
		return []ui.Node{eyebrow, primitives.InlineAlert(primitives.InlineAlertProps{
			Title: "Reading the declaration", Tone: design.StatusNeutral, Mode: mode,
			Message: "Parsing this declaration and the documentation it carries.",
		})}
	case LoadFailed:
		return []ui.Node{eyebrow, primitives.InlineAlert(primitives.InlineAlertProps{
			Title: "Declaration unavailable", Tone: design.StatusFailure, Mode: mode,
			Message: failureBody(props.DetailError),
		})}
	}
	if props.Detail == nil {
		return []ui.Node{eyebrow, html.P(html.Props{
			Class: proseClass(tokens),
			Text:  "Choose an atom to read what it promises, when not to use it, and how it fails.",
		})}
	}
	detail := *props.Detail
	children := []ui.Node{
		eyebrow,
		html.H2(html.Props{
			Class: css.New(
				css.Margin(css.Zero),
				css.Font(css.FontStack(tokens.Fonts.Code)),
				css.FontSize(css.Px(tokens.Typography.SectionTitle.Size)),
				css.LineHeightLen(css.Px(tokens.Typography.SectionTitle.LineHeight)),
				css.FontWeight.Normal,
				css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
				css.OverflowWrap.Anywhere,
			).String(),
			Text: qualifiedName(detail.Row),
		}),
	}
	if sentence := strings.TrimSpace(detail.OpeningSentence); sentence != "" {
		children = append(children, html.P(html.Props{
			Class: css.New(
				css.Margin(css.Zero),
				css.Font(css.FontStack(tokens.Fonts.Reading)),
				css.FontSize(css.Px(tokens.Typography.Body.Size)),
				css.LineHeightLen(css.Px(tokens.Typography.Body.LineHeight)),
				css.TextColor(css.Hex(string(tokens.Colors.TextSecondary))),
			).String(),
			Text: sentence,
		}))
	}
	if !detail.Row.Admitted {
		// A stacked block rather than an inline alert: the alert lays its title
		// beside its message, which in a 460-pixel column left four words on
		// the left and a cramped paragraph on the right.
		children = append(children, refusalBlock(detail.Row.Problem, tokens))
	}
	if signature := strings.TrimSpace(detail.Signature); signature != "" {
		children = append(children, section("Signature", codeBlock(signature, tokens), tokens))
	}
	if source := strings.TrimSpace(detail.Source); source != "" {
		children = append(children, section(
			sourceHeading(detail), sourceBlock(detail, tokens), tokens,
		))
	}
	if len(detail.Fields) > 0 {
		children = append(children, section(
			documentationHeading(detail.SchemaVersion), fieldList(detail.Fields, tokens), tokens,
		))
		if len(detail.Documentation) > 0 {
			// The parsed fields are this product's reading of the comment; the
			// comment is what the author wrote. An author correcting a field
			// needs the second one, so it is here — folded away, because
			// repeating forty lines the reader just read is not a second
			// reading, it is the same one twice.
			children = append(children, primitives.Disclosure(primitives.DisclosureProps{
				ID: "atom-notes", Label: "Notes as written", Mode: mode,
				Expanded: props.NotesExpanded, OnToggle: props.OnNotesToggle,
				Content: commentBlock(detail.Documentation, tokens),
			}))
		}
	} else if len(detail.Documentation) > 0 {
		// A declaration whose documentation did not parse still shows the
		// comment exactly as written, because that text is what an author has
		// to fix.
		children = append(children, section(
			"Comment as written", commentBlock(detail.Documentation, tokens), tokens,
		))
	}
	if structure := structureSection(detail, tokens); structure != nil {
		children = append(children, structure)
	}
	if schema := schemaSection(detail, mode, tokens); schema != nil {
		children = append(children, schema)
	}
	if tests := testSection(detail, mode, tokens); tests != nil {
		children = append(children, tests)
	}
	if relations := relationSection(detail, tokens); relations != nil {
		children = append(children, relations)
	}
	children = append(children, section(
		"Where it lives", locationList(detail, tokens), tokens,
	))
	if query := querySection(props, detail, mode, tokens); query != nil {
		children = append(children, query)
	}
	return children
}

func refusalBlock(problem string, tokens design.Tokens) ui.Node {
	return html.Div(html.Props{
		Data: map[string]string{"component": "atom-refusal"},
		Class: css.New(
			u.Flex, u.FlexCol, css.Gap(css.Px(2)),
			css.Padding(css.RawLength("8px 10px")),
			css.Bg(css.Hex(string(tokens.Colors.Surface1))),
			css.BorderLeft(css.Px(2), css.Hex(string(tokens.Colors.Warning))),
			css.Rounded(css.Px(tokens.Geometry.RadiusSmall)),
		).String(),
	},
		html.Span(html.Props{
			Class: css.New(
				css.Font(css.FontStack(tokens.Fonts.UI)),
				css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				css.FontWeight.Semibold,
				css.TextColor(css.Hex(string(tokens.Colors.Warning))),
			).String(),
			Text: "This directive was refused",
		}),
		html.Span(html.Props{
			Class: css.New(
				css.Font(css.FontStack(tokens.Fonts.Reading)),
				css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				css.LineHeightLen(css.Px(tokens.Typography.Body.LineHeight)),
				css.TextColor(css.Hex(string(tokens.Colors.TextSecondary))),
			).String(),
			Text: refusalMessage(problem),
		}),
	)
}

func refusalMessage(problem string) string {
	if trimmed := strings.TrimSpace(problem); trimmed != "" {
		return trimmed
	}
	return "This declaration carries the atom directive, but its documentation did not admit."
}

func documentationHeading(schemaVersion uint32) string {
	if schemaVersion == 0 {
		return "Documentation"
	}
	return "Documentation (schema v" + strconv.FormatUint(uint64(schemaVersion), 10) + ")"
}

// fieldList renders the documented fields in the order the author wrote them.
// Reordering them would put this surface's opinion above the author's.
func fieldList(fields []AtomField, tokens design.Tokens) ui.Node {
	rows := make([]ui.Node, 0, len(fields)*2)
	for _, field := range fields {
		rows = append(rows, html.Tag("dt", html.Props{
			Class: css.New(
				css.Font(css.FontStack(tokens.Fonts.UI)),
				css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				css.LineHeightLen(css.Px(tokens.Typography.Metadata.LineHeight)),
				css.FontWeight.Semibold,
				css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
			).String(),
			Text: field.Label,
		}))
		rows = append(rows, html.Tag("dd", html.Props{
			Class: css.New(
				css.Margin(css.Zero),
				css.Font(css.FontStack(tokens.Fonts.Reading)),
				css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				css.LineHeightLen(css.Px(tokens.Typography.Body.LineHeight)),
				css.TextColor(css.Hex(string(tokens.Colors.TextSecondary))),
			).String(),
		}, fieldBody(field, tokens)...))
	}
	return html.Tag("dl", html.Props{
		Class: css.New(
			u.Grid,
			css.GridCols(css.TrackLen(css.Px(124)), css.MinMax(css.TrackLen(css.Zero), css.Fr(1))),
			css.Gap(css.RawLength("6px 12px")),
			css.Margin(css.Zero),
		).String(),
	}, rows...)
}

func fieldBody(field AtomField, tokens design.Tokens) []ui.Node {
	if len(field.Items) == 0 {
		return []ui.Node{html.Span(html.Props{
			Class: css.New(css.WhiteSpace.PreWrap).String(), Text: field.Text,
		})}
	}
	items := make([]ui.Node, 0, len(field.Items))
	for _, item := range field.Items {
		items = append(items, html.Li(html.Props{
			Class: css.New(css.MarginY(css.Px(2))).String(), Text: item,
		}))
	}
	return []ui.Node{html.Ul(html.Props{
		Class: css.New(css.Margin(css.Zero), css.Padding(css.RawLength("0 0 0 16px"))).String(),
	}, items...)}
}

// querySection shows the SQL behind what is on screen.
//
// The index is the coordinator's, held in memory for exactly as long as the
// parsed repository it describes, so the section says where the statement runs
// rather than implying a database file somebody can open. The statements are
// the ones the coordinator executes, not a rendering of them.
func querySection(props Props, detail AtomDetail, mode primitives.Mode, tokens design.Tokens) ui.Node {
	blocks := make([]ui.Node, 0, 3)
	if query := strings.TrimSpace(detail.AccessQuery); query != "" {
		blocks = append(blocks, queryBlock(props, "Read this atom", query, mode, tokens))
	}
	if query := strings.TrimSpace(props.SearchQuery); query != "" {
		blocks = append(blocks, queryBlock(props, "This search", query, mode, tokens))
	}
	if schema := strings.TrimSpace(detail.IndexSchema); schema != "" {
		blocks = append(blocks, queryBlock(props, "The table they run against", schema, mode, tokens))
	}
	if len(blocks) == 0 {
		return nil
	}
	blocks = append(blocks, html.P(html.Props{
		Class: proseClass(tokens),
		Text: "These run against the coordinator's in-memory atom index, which is " +
			"rebuilt from the working tree whenever the revision moves. The durable " +
			"database has no atom tables: nothing in the product writes them yet.",
	}))
	return section("Query", html.Div(html.Props{
		Class: css.New(u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.SM))).String(),
	}, blocks...), tokens)
}

// copyLabel acknowledges the copy on the control that performed it.
func copyLabel(props Props, query string) string {
	if props.CopiedQuery != "" && props.CopiedQuery == query {
		return "Copied"
	}
	return "Copy"
}

// queryBlock is one statement with a control that copies it.
func queryBlock(props Props, label, query string, mode primitives.Mode, tokens design.Tokens) ui.Node {
	header := []ui.Node{
		html.Span(html.Props{
			Class: css.New(
				css.Font(css.FontStack(tokens.Fonts.UI)),
				css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				css.FontWeight.Semibold,
				css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
			).String(),
			Text: label,
		}),
	}
	if props.OnCopy != nil {
		header = append(header, html.Div(html.Props{
			Class: css.New(css.Margin(css.RawLength("0 0 0 auto"))).String(),
		}, primitives.Button(primitives.ButtonProps{
			Label: copyLabel(props, query), AccessibleLabel: "Copy the " + strings.ToLower(label) + " query",
			Quiet: true, Mode: mode, LeadingIcon: primitives.IconCopy,
			OnClick: func() { props.OnCopy(query) },
		})))
	}
	return html.Div(html.Props{
		Data:  map[string]string{"component": "atom-query", "query": strings.ToLower(label)},
		Class: css.New(u.Flex, u.FlexCol, css.Gap(css.Px(4))).String(),
	},
		html.Div(html.Props{
			Class: css.New(u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.XS))).String(),
		}, header...),
		codeBlock(query, tokens),
	)
}

// structureSection reports what the run's own atom stages measure.
//
// The time bound is structural: it is the deepest loop nesting, which is what
// StageAtomComplexity turns into a claim. It is not a benchmark, and the
// section says so, because "O(n) by structure" and "measured to grow linearly"
// are different statements and only one of them was made here.
func structureSection(detail AtomDetail, tokens design.Tokens) ui.Node {
	if !detail.Structure.Measured {
		return nil
	}
	structure := detail.Structure
	fields := []MemoryDetailFieldLike{
		{Label: "Time", Value: structure.TimeBound + " by structure, from " +
			loopPhrase(structure.LoopDepth)},
		{Label: "Space", Value: structure.SpaceClaim},
		{Label: "Branches", Value: branchPhrase(structure.Branches)},
		{Label: "Reach", Value: reachPhrase(structure.Pure)},
	}
	return section("Measured shape", fieldValueList(fields, tokens), tokens)
}

// loopPhrase names the nesting a time bound was read from.
func loopPhrase(depth uint32) string {
	switch depth {
	case 0:
		return "no loop"
	case 1:
		return "one loop"
	default:
		return strconv.FormatUint(uint64(depth), 10) + " nested loops"
	}
}

// branchPhrase states the decision points, which is the number of distinct
// cases a test suite has to reach to cover the declaration.
func branchPhrase(branches uint32) string {
	switch branches {
	case 0:
		return "None: one path through it"
	case 1:
		return "1 decision point"
	default:
		return strconv.FormatUint(uint64(branches), 10) + " decision points"
	}
}

func reachPhrase(pure bool) string {
	if pure {
		return "Nothing outside its arguments"
	}
	return "Reaches outside its arguments"
}

// schemaSection says which of the schema's declared fields this atom answers.
//
// An atom that documents twelve of nineteen fields looks documented and is
// not, and only naming the missing ones tells an author which promises were
// never made.
func schemaSection(detail AtomDetail, mode primitives.Mode, tokens design.Tokens) ui.Node {
	total := len(detail.DocumentedFields) + len(detail.MissingFields)
	if total == 0 {
		return nil
	}
	children := []ui.Node{
		html.P(html.Props{
			Class: proseClass(tokens),
			Text: strconv.Itoa(len(detail.DocumentedFields)) + " of " +
				strconv.Itoa(total) + " declared fields answered.",
		}),
	}
	if len(detail.MissingFields) > 0 {
		children = append(children, html.P(html.Props{
			Class: css.New(
				css.Margin(css.Zero),
				css.Font(css.FontStack(tokens.Fonts.Code)),
				css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				css.LineHeightLen(css.Px(tokens.Typography.Body.LineHeight)),
				css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
			).String(),
			Text: "Not answered: " + strings.Join(detail.MissingFields, ", "),
		}))
	}
	return section("Schema coverage", html.Div(html.Props{
		Class: css.New(u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.XS))).String(),
	}, children...), tokens)
}

// MemoryDetailFieldLike is one labelled line.
type MemoryDetailFieldLike struct {
	Label string
	Value string
}

func fieldValueList(fields []MemoryDetailFieldLike, tokens design.Tokens) ui.Node {
	rows := make([]ui.Node, 0, len(fields))
	for _, field := range fields {
		rows = append(rows, definition(field.Label, field.Value, tokens))
	}
	return html.Div(html.Props{
		Class: css.New(u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.XS))).String(),
	}, rows...)
}

// testSection lists what exercises this atom. An atom whose documentation
// names a verification and whose test file references nothing is a promise
// with no evidence behind it, and that is worth seeing at a glance.
func testSection(detail AtomDetail, mode primitives.Mode, tokens design.Tokens) ui.Node {
	if len(detail.Tests) == 0 {
		if !detail.Row.Admitted {
			return nil
		}
		return section("Tests", html.P(html.Props{
			Class: proseClass(tokens),
			Text:  "No test in this repository references this atom by name.",
		}), tokens)
	}
	rows := make([]ui.Node, 0, len(detail.Tests))
	for _, reference := range detail.Tests {
		rows = append(rows, html.Div(html.Props{
			Data: map[string]string{"component": "atom-test"},
			Class: css.New(
				u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.XS)),
				css.FlexWrap.Wrap,
				css.Font(css.FontStack(tokens.Fonts.Code)),
				css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				css.LineHeightLen(css.Px(tokens.Typography.Body.LineHeight)),
			).String(),
		},
			primitives.IconChip(primitives.IconChipProps{
				Label: "test", Kind: design.KindTest, Mode: mode,
			}),
			html.Span(html.Props{
				Class: css.New(css.TextColor(css.Hex(string(tokens.Colors.TextPrimary)))).String(),
				Text:  reference.Name,
			}),
			html.Span(html.Props{
				Class: css.New(
					css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
					css.OverflowWrap.Anywhere,
				).String(),
				Text: referenceLocation(reference),
			}),
		))
	}
	return section("Tests", html.Div(html.Props{
		Class: css.New(u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.XS))).String(),
	}, rows...), tokens)
}

func referenceLocation(reference Reference) string {
	if reference.Path == "" {
		return ""
	}
	if reference.Line == 0 {
		return reference.Path
	}
	return reference.Path + ":" + strconv.FormatUint(uint64(reference.Line), 10)
}

// sourceHeading names the file lines the block covers, so a reader can cite
// what they are looking at rather than counting from the top.
func sourceHeading(detail AtomDetail) string {
	if detail.SourceStartLine == 0 {
		return "Code"
	}
	start := detail.SourceStartLine
	end := start + uint32(strings.Count(detail.Source, "\n"))
	heading := "Code (lines " + strconv.FormatUint(uint64(start), 10) +
		"-" + strconv.FormatUint(uint64(end), 10) + ")"
	if detail.SourceTruncated {
		heading += " · truncated"
	}
	return heading
}

// sourceBlock draws the declaration with its own file line numbers.
//
// The numbers are the file's, not the block's: a reader who has the file open
// beside this needs the two to agree.
func sourceBlock(detail AtomDetail, tokens design.Tokens) ui.Node {
	lines := strings.Split(detail.Source, "\n")
	rows := make([]ui.Node, 0, len(lines))
	for index, line := range lines {
		number := detail.SourceStartLine + uint32(index)
		rows = append(rows, html.Div(html.Props{
			Class: css.New(u.Flex, css.Gap(css.Px(tokens.Spacing.SM))).String(),
		},
			html.Span(html.Props{
				Aria: map[string]string{"hidden": "true"},
				Class: css.New(
					css.FlexShrink(css.Num(0)),
					css.MinWidth(css.Px(34)),
					css.TextAlign.Right,
					css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
				).String(),
				Text: strconv.FormatUint(uint64(number), 10),
			}),
			html.Span(html.Props{
				Class: css.New(
					css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
					css.WhiteSpace.Pre,
				).String(),
				Text: line,
			}),
		))
	}
	return html.Div(html.Props{
		Data: map[string]string{"component": "atom-source"},
		Class: css.New(
			css.Padding(css.RawLength("8px 10px")),
			css.Bg(css.Hex(string(tokens.Colors.Surface1))),
			css.Border(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
			css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
			css.Font(css.FontStack(tokens.Fonts.Code)),
			css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
			css.LineHeightLen(css.Px(tokens.Typography.Metadata.LineHeight)),
			css.MaxHeight(css.Px(420)),
			css.OverflowY.Auto, css.OverflowX.Auto,
		).String(),
	}, rows...)
}

func relationSection(detail AtomDetail, tokens design.Tokens) ui.Node {
	rows := make([]ui.Node, 0, 4)
	if len(detail.Callers) > 0 {
		rows = append(rows, definition("Called by", referenceLines(detail.Callers), tokens))
	}
	if len(detail.Callees) > 0 {
		rows = append(rows, definition("Calls", referenceLines(detail.Callees), tokens))
	}
	if len(detail.Implements) > 0 {
		rows = append(rows, definition("Implements", strings.Join(detail.Implements, "\n"), tokens))
	}
	if len(detail.ImplementedBy) > 0 {
		rows = append(rows, definition("Implemented by", strings.Join(detail.ImplementedBy, "\n"), tokens))
	}
	if len(rows) == 0 {
		return nil
	}
	return section("Relationships", html.Div(html.Props{
		Class: css.New(u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.XS))).String(),
	}, rows...), tokens)
}

func referenceLines(references []Reference) string {
	lines := make([]string, 0, len(references))
	for _, reference := range references {
		line := reference.Name
		if reference.Path != "" {
			line += "  " + reference.Path
			if reference.Line > 0 {
				line += ":" + strconv.FormatUint(uint64(reference.Line), 10)
			}
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func locationList(detail AtomDetail, tokens design.Tokens) ui.Node {
	row := detail.Row
	rows := []ui.Node{definition("Package", row.ImportPath, tokens)}
	location := row.Path
	if row.Line > 0 {
		location += ":" + strconv.FormatUint(uint64(row.Line), 10)
	}
	rows = append(rows, definition("File", location, tokens))
	rows = append(rows, definition("Declaration", row.Kind, tokens))
	if detail.NamingChecked {
		rows = append(rows, definition("Naming", namingText(detail.NamingFinding), tokens))
	}
	return html.Div(html.Props{
		Class: css.New(u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.XS))).String(),
	}, rows...)
}

// namingText states what the product's own naming grammar makes of the name.
// A pass is stated rather than left silent: "nothing was said" and "this was
// checked and it is fine" are different, and only one of them is reassuring.
func namingText(finding string) string {
	if trimmed := strings.TrimSpace(finding); trimmed != "" {
		return trimmed
	}
	return "Follows the atom naming grammar"
}

func definition(label, value string, tokens design.Tokens) ui.Node {
	return html.Div(html.Props{
		Class: css.New(u.Flex, u.FlexCol, css.Gap(css.Px(2))).String(),
	},
		html.Span(html.Props{
			Class: css.New(
				css.Font(css.FontStack(tokens.Fonts.UI)),
				css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				css.FontWeight.Semibold,
				css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
			).String(),
			Text: label,
		}),
		html.Span(html.Props{
			Class: css.New(
				css.Font(css.FontStack(tokens.Fonts.Code)),
				css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				css.LineHeightLen(css.Px(tokens.Typography.Body.LineHeight)),
				css.TextColor(css.Hex(string(tokens.Colors.TextSecondary))),
				css.WhiteSpace.PreWrap, css.OverflowWrap.Anywhere,
			).String(),
			Text: value,
		}),
	)
}

// codeBlock draws the signature as code: it is read as syntax, not prose, and
// it never wraps mid-token where a reader would misread the type.
func codeBlock(text string, tokens design.Tokens) ui.Node {
	return html.Pre(html.Props{
		Class: css.New(
			css.Margin(css.Zero),
			css.Padding(css.RawLength("8px 10px")),
			css.Bg(css.Hex(string(tokens.Colors.Surface1))),
			css.Border(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
			css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
			css.Font(css.FontStack(tokens.Fonts.Code)),
			css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
			css.LineHeightLen(css.Px(tokens.Typography.Body.LineHeight)),
			css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
			css.OverflowX.Auto, css.WhiteSpace.Pre,
		).String(),
		Text: text,
	})
}

// commentBlock wraps rather than scrolls: a comment is prose an author has to
// read and fix, and a horizontal scrollbar hides the half of the sentence that
// says what is wrong.
func commentBlock(lines []string, tokens design.Tokens) ui.Node {
	return html.Pre(html.Props{
		Class: css.New(
			css.Margin(css.Zero),
			css.Padding(css.RawLength("8px 10px")),
			css.Bg(css.Hex(string(tokens.Colors.Surface1))),
			css.Border(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
			css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
			css.Font(css.FontStack(tokens.Fonts.Code)),
			css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
			css.LineHeightLen(css.Px(tokens.Typography.Body.LineHeight)),
			css.TextColor(css.Hex(string(tokens.Colors.TextSecondary))),
			css.WhiteSpace.PreWrap, css.OverflowWrap.Anywhere,
		).String(),
		Text: strings.Join(lines, "\n"),
	})
}

func section(title string, content ui.Node, tokens design.Tokens) ui.Node {
	if content == nil {
		return nil
	}
	return html.Section(html.Props{
		Aria:  map[string]string{"label": title},
		Data:  map[string]string{"component": "atom-detail-section", "section": strings.ToLower(title)},
		Class: css.New(u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.XS))).String(),
	},
		eyebrowLabel(title, tokens),
		content,
	)
}

func eyebrowLabel(label string, tokens design.Tokens) ui.Node {
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
		).String(),
		Text: label,
	})
}

func proseClass(tokens design.Tokens) string {
	return css.New(
		css.Margin(css.Zero),
		css.Font(css.FontStack(tokens.Fonts.Reading)),
		css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
		css.LineHeightLen(css.Px(tokens.Typography.Body.LineHeight)),
		css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
	).String()
}
