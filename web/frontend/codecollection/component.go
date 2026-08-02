// Package codecollection renders a repository's own code: its packages, its
// declarations, and the documentation a declaration carries about itself.
//
// It receives already-projected rows and holds no state, so the route surface
// stays free of hooks and nothing here can invent a declaration the
// coordinator did not report. No declaration body is rendered: this is a
// directory of what the collection offers, not an editor.
package codecollection

import (
	"strconv"

	"codeflux.dev/codeflux/web/frontend/design"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/css/u"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// PackageRow is one Go package in the collection.
type PackageRow struct {
	ImportPath  string
	Name        string
	FileCount   uint32
	TestCount   uint32
	SymbolCount uint32
	AtomCount   uint32
}

// SymbolRow is one declaration.
type SymbolRow struct {
	Key        string
	Name       string
	Kind       string
	Receiver   string
	ImportPath string
	Path       string
	Line       uint32
	Exported   bool
	Atom       bool
	// AtomProblem says why a declaration carrying the atom directive was not
	// admitted. A declaration is never shown as documented on the strength of
	// the directive alone.
	AtomProblem string
}

// AtomField is one documented field of an admitted atom.
type AtomField struct {
	Label string
	Text  string
	Items []string
}

// Reference is one place a declaration is named.
type Reference struct {
	Name string
	Path string
	Line uint32
}

// Detail is one declaration read closely.
type Detail struct {
	Symbol              SymbolRow
	Signature           string
	Documentation       []string
	AtomOpeningSentence string
	AtomSchemaVersion   uint32
	AtomFields          []AtomField
	Callers             []Reference
	Callees             []Reference
	Implements          []string
	ImplementedBy       []string
	Loading             bool
	Failed              bool
}

// Props is everything the collection surface draws from.
type Props struct {
	Mode primitives.Mode

	Loading           bool
	Failed            bool
	Unavailable       bool
	UnavailableReason string

	// Revision names the repository revision every row describes. Dirty says
	// the working tree has moved since that revision.
	Revision string
	Dirty    bool
	Warnings []string

	Packages      []PackageRow
	TotalPackages uint32
	TotalSymbols  uint32
	TotalAtoms    uint32
	Truncated     bool

	SelectedPackage  string
	Symbols          []SymbolRow
	SymbolsMatched   uint32
	SymbolsTruncated bool
	SymbolsLoading   bool

	Search       string
	ExportedOnly bool
	AtomsOnly    bool

	SelectedSymbol string
	Detail         Detail

	OnReload         func()
	OnSelectPackage  func(importPath string)
	OnSelectSymbol   func(key string)
	OnSearch         func(value string)
	OnToggleExported func()
	OnToggleAtoms    func()
}

// Component renders the whole collection surface.
//
// It is a three-pane reading surface rather than a stacked page: a collection
// is browsed by moving between a package, its declarations, and one
// declaration read closely, and a layout that puts the detail below several
// hundred list rows makes the one thing somebody clicked the one thing they
// cannot see. Each pane scrolls on its own so a long list never pushes another
// pane off the screen.
func Component(props Props) ui.Node {
	tokens := props.Mode.Tokens()
	if node, handled := stateNode(props); handled {
		return html.Div(html.Props{
			Data:  map[string]string{"component": "code-collection"},
			Class: css.New(css.Padding(css.Px(tokens.Spacing.LG))).String(),
		}, node)
	}
	columns := []css.Track{
		css.TrackLen(css.Px(360)),
		css.MinMax(css.TrackLen(css.Zero), css.Fr(1)),
	}
	panes := []ui.Node{packageList(props), symbolList(props)}
	if props.SelectedSymbol != "" {
		columns = append(columns, css.MinMax(css.TrackLen(css.Px(420)), css.Fr(1)))
		panes = append(panes, detailPanel(props))
	}
	bodyRules := []css.Rule{
		u.Grid, css.Gap(css.Px(tokens.Spacing.LG)),
		css.GridCols(css.MinMax(css.TrackLen(css.Zero), css.Fr(1))),
		css.MinWidth(css.Zero), css.MinHeight(css.Zero),
		css.H(css.Full), css.Overflow.Hidden,
	}
	// Below this width the three panes stop being readable side by side, so
	// they stack and the whole surface scrolls.
	bodyRules = append(bodyRules, css.Media(css.MinW(1024), css.GridCols(columns...))...)
	return html.Div(
		html.Props{
			Data: map[string]string{"component": "code-collection"},
			Class: css.New(
				u.Grid,
				css.GridRows(css.TrackAuto, css.MinMax(css.TrackLen(css.Zero), css.Fr(1))),
				css.Gap(css.Px(tokens.Spacing.MD)),
				css.W(css.Full), css.H(css.Full),
				css.MinWidth(css.Zero), css.MinHeight(css.Zero),
				css.Overflow.Hidden,
			).String(),
		},
		html.Div(
			html.Props{
				Class: css.New(
					u.Grid, css.Gap(css.Px(tokens.Spacing.SM)),
					css.MinWidth(css.Zero),
				).String(),
			},
			revisionBanner(props),
			filters(props),
		),
		html.Div(
			html.Props{Class: css.New(bodyRules...).String()},
			panes...,
		),
	)
}

// paneClass gives one pane its own bounded scroll.
//
// Without it a package list of ninety-four entries and a declaration list of
// several hundred push every other pane off the screen, and the surface
// becomes one long column again.
func paneClass(props Props) string {
	tokens := props.Mode.Tokens()
	return css.New(
		u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.SM)),
		css.MinWidth(css.Zero), css.MinHeight(css.Zero),
		css.H(css.Full), css.OverflowY.Auto,
		css.PaddingX(css.Px(tokens.Spacing.XS)),
	).String()
}

// revisionBanner states which revision the whole surface describes.
func revisionBanner(props Props) ui.Node {
	if props.Revision == "" {
		return html.P(html.Props{
			Text: "The coordinator has not named a revision for this collection.",
		})
	}
	revision := props.Revision
	if len(revision) > 12 {
		revision = revision[:12]
	}
	summary := countLabel(props.TotalPackages, "package", "packages") + " · " +
		countLabel(props.TotalSymbols, "declaration", "declarations") + " · " +
		countLabel(props.TotalAtoms, "documented atom", "documented atoms")
	children := []ui.Node{
		html.P(html.Props{
			Text: summary + " at revision " + revision,
			Data: map[string]string{"component": "code-collection-revision"},
		}),
	}
	if props.Dirty {
		// The map is built from a revision. An uncommitted change is real and
		// is not in it, and a directory that did not say so would describe a
		// tree that has moved on.
		children = append(children, primitives.InlineAlert(primitives.InlineAlertProps{
			Title:   "Uncommitted changes",
			Message: "This listing describes the committed revision. Changes in the working tree are not in it.",
			Tone:    design.StatusWarning, Mode: props.Mode,
		}))
	}
	for _, warning := range props.Warnings {
		children = append(children, primitives.InlineAlert(primitives.InlineAlertProps{
			Title: "Mapping warning", Message: warning,
			Tone: design.StatusWarning, Mode: props.Mode,
		}))
	}
	return html.Section(
		html.Props{Aria: map[string]string{"label": "Collection revision"}},
		children...,
	)
}

// filters renders the controls that narrow the listing.
func filters(props Props) ui.Node {
	exported := "Show every declaration"
	if !props.ExportedOnly {
		exported = "Show exported only"
	}
	atoms := "Show every declaration"
	if !props.AtomsOnly {
		atoms = "Show documented atoms only"
	}
	return html.Section(
		html.Props{
			Aria: map[string]string{"label": "Collection filters"},
			Data: map[string]string{"component": "code-collection-filters"},
		},
		primitives.TextField(primitives.TextFieldProps{
			ID: "code-collection-search", Label: "Search declarations",
			Value: props.Search, Placeholder: "Reserve", Mode: props.Mode,
			Disabled: props.OnSearch == nil,
			OnInput: func(value string) {
				if props.OnSearch != nil {
					props.OnSearch(value)
				}
			},
		}),
		primitives.Button(primitives.ButtonProps{
			Label: exported, Mode: props.Mode, Disabled: props.OnToggleExported == nil,
			OnClick: func() {
				if props.OnToggleExported != nil {
					props.OnToggleExported()
				}
			},
		}),
		primitives.Button(primitives.ButtonProps{
			Label: atoms, Mode: props.Mode, Disabled: props.OnToggleAtoms == nil,
			OnClick: func() {
				if props.OnToggleAtoms != nil {
					props.OnToggleAtoms()
				}
			},
		}),
	)
}

// packageList renders the repository's packages.
func packageList(props Props) ui.Node {
	if len(props.Packages) == 0 {
		return primitives.EmptyState(primitives.EmptyStateProps{
			Title: "No package is mapped",
			Body: "The coordinator maps a repository with the Go toolchain. A repository " +
				"with no Go module, or one whose packages would not load, maps to nothing.",
			ActionLabel: reloadLabel(props.OnReload), Mode: props.Mode, OnAction: props.OnReload,
		})
	}
	items := make([]ui.Node, 0, len(props.Packages))
	for _, record := range props.Packages {
		importPath := record.ImportPath
		label := record.ImportPath + " · " +
			countLabel(record.SymbolCount, "declaration", "declarations")
		if record.AtomCount > 0 {
			label += " · " + countLabel(record.AtomCount, "atom", "atoms")
		}
		items = append(items, html.Li(
			html.Props{
				Data: map[string]string{
					"component":   "code-package",
					"import-path": record.ImportPath,
					"selected":    boolLabel(record.ImportPath == props.SelectedPackage),
				},
			},
			primitives.Button(primitives.ButtonProps{
				Label: label, Mode: props.Mode,
				AccessibleLabel: "Open package " + record.ImportPath,
				Disabled:        props.OnSelectPackage == nil,
				OnClick: func() {
					if props.OnSelectPackage != nil {
						props.OnSelectPackage(importPath)
					}
				},
			}),
		))
	}
	children := []ui.Node{
		html.H3(html.Props{Text: "Packages"}),
		html.Ul(html.Props{Aria: map[string]string{"label": "Packages"}}, items...),
	}
	if props.Truncated {
		// A bounded page that did not say it was bounded would read as the whole
		// collection.
		children = append(children, html.P(html.Props{
			Text: "This page is bounded. Search to narrow it.",
		}))
	}
	return html.Section(
		html.Props{
			Aria:  map[string]string{"label": "Packages"},
			Data:  map[string]string{"component": "code-collection-packages"},
			Class: paneClass(props),
		},
		children...,
	)
}

// symbolList renders the declarations of the selected package or search.
func symbolList(props Props) ui.Node {
	children := []ui.Node{html.H3(html.Props{Text: "Declarations"})}
	switch {
	case props.SymbolsLoading:
		children = append(children, html.P(html.Props{
			Text: "Reading declarations…", Aria: map[string]string{"live": "polite"},
		}))
	case props.SelectedPackage == "" && props.Search == "" && !props.AtomsOnly:
		children = append(children, html.P(html.Props{
			Text: "Choose a package, search, or narrow to documented atoms.",
		}))
	case len(props.Symbols) == 0:
		children = append(children, html.P(html.Props{
			Text: "No declaration matches.",
		}))
	default:
		items := make([]ui.Node, 0, len(props.Symbols))
		for _, symbol := range props.Symbols {
			items = append(items, symbolItem(props, symbol))
		}
		children = append(children,
			html.Ul(html.Props{Aria: map[string]string{"label": "Declarations"}}, items...),
		)
		if props.SymbolsTruncated {
			children = append(children, html.P(html.Props{
				Text: strconv.FormatUint(uint64(props.SymbolsMatched), 10) +
					" declarations match; this page is bounded.",
			}))
		}
	}
	return html.Section(
		html.Props{
			Aria:  map[string]string{"label": "Declarations"},
			Data:  map[string]string{"component": "code-collection-symbols"},
			Class: paneClass(props),
		},
		children...,
	)
}

// symbolItem renders one declaration row.
func symbolItem(props Props, symbol SymbolRow) ui.Node {
	key := symbol.Key
	label := symbol.Name
	if symbol.Receiver != "" {
		label = symbol.Receiver + "." + symbol.Name
	}
	detail := symbol.Kind
	if !symbol.Exported {
		detail += " · unexported"
	}
	children := []ui.Node{
		primitives.Button(primitives.ButtonProps{
			Label: label + " · " + detail, Mode: props.Mode,
			AccessibleLabel: "Inspect " + label,
			Disabled:        props.OnSelectSymbol == nil,
			OnClick: func() {
				if props.OnSelectSymbol != nil {
					props.OnSelectSymbol(key)
				}
			},
		}),
	}
	switch {
	case symbol.Atom:
		children = append(children, primitives.Badge(primitives.BadgeProps{
			Label: "Documented atom", Status: design.StatusSuccess, Mode: props.Mode,
		}))
	case symbol.AtomProblem != "":
		// The directive is there and the documentation is not. Saying so is the
		// point: calling it an atom would claim an admission it never had.
		children = append(children, primitives.Badge(primitives.BadgeProps{
			Label:  "Atom directive, unparsed documentation",
			Status: design.StatusWarning, Mode: props.Mode,
		}))
	}
	return html.Li(
		html.Props{
			Data: map[string]string{
				"component":  "code-symbol",
				"symbol-key": symbol.Key,
				"atom":       boolLabel(symbol.Atom),
				"selected":   boolLabel(symbol.Key == props.SelectedSymbol),
			},
		},
		children...,
	)
}

// detailPanel renders the selected declaration.
func detailPanel(props Props) ui.Node {
	children := []ui.Node{html.H3(html.Props{Text: "Declaration"})}
	switch {
	case props.SelectedSymbol == "":
		children = append(children, html.P(html.Props{
			Text: "Choose a declaration to read its signature, its documentation, and what calls it.",
		}))
	case props.Detail.Loading:
		children = append(children, html.P(html.Props{
			Text: "Reading the declaration…", Aria: map[string]string{"live": "polite"},
		}))
	case props.Detail.Failed:
		children = append(children, primitives.InlineAlert(primitives.InlineAlertProps{
			Title:   "Declaration could not be read",
			Message: "The coordinator did not answer. Nothing in the repository was changed.",
			Tone:    design.StatusFailure, Mode: props.Mode,
		}))
	default:
		children = append(children, declarationBody(props)...)
	}
	return html.Section(
		html.Props{
			Aria:  map[string]string{"label": "Declaration detail"},
			Data:  map[string]string{"component": "code-collection-detail"},
			Class: paneClass(props),
		},
		children...,
	)
}

// declarationBody renders one read declaration.
func declarationBody(props Props) []ui.Node {
	detail := props.Detail
	children := []ui.Node{
		html.P(html.Props{
			Text: detail.Symbol.Path + ":" + strconv.FormatUint(uint64(detail.Symbol.Line), 10),
			Data: map[string]string{"component": "code-symbol-location"},
		}),
	}
	if detail.Signature != "" {
		children = append(children, signatureBlock(props, detail.Signature))
	}
	if len(detail.Documentation) > 0 {
		lines := make([]ui.Node, 0, len(detail.Documentation))
		for _, line := range detail.Documentation {
			lines = append(lines, html.P(html.Props{Text: line}))
		}
		children = append(children, html.Section(
			html.Props{
				Aria: map[string]string{"label": "Documentation"},
				Data: map[string]string{"component": "code-symbol-documentation"},
			},
			lines...,
		))
	}
	if detail.AtomOpeningSentence != "" || len(detail.AtomFields) > 0 {
		children = append(children, atomDocumentation(props))
	}
	children = append(children,
		referenceList(props, "Called by", detail.Callers, "code-symbol-callers"),
		referenceList(props, "Calls", detail.Callees, "code-symbol-callees"),
	)
	if len(detail.Implements) > 0 {
		children = append(children, html.P(html.Props{
			Text: "Implements: " + joinNames(detail.Implements),
		}))
	}
	if len(detail.ImplementedBy) > 0 {
		children = append(children, html.P(html.Props{
			Text: "Implemented by: " + joinNames(detail.ImplementedBy),
		}))
	}
	return children
}

// atomDocumentation renders an admitted atom's structured fields.
func atomDocumentation(props Props) ui.Node {
	detail := props.Detail
	children := []ui.Node{
		primitives.Badge(primitives.BadgeProps{
			Label: "Documented atom, schema v" +
				strconv.FormatUint(uint64(detail.AtomSchemaVersion), 10),
			Status: design.StatusSuccess, Mode: props.Mode,
		}),
	}
	if detail.AtomOpeningSentence != "" {
		children = append(children, html.P(html.Props{Text: detail.AtomOpeningSentence}))
	}
	terms := make([]ui.Node, 0, len(detail.AtomFields)*2)
	for _, field := range detail.AtomFields {
		terms = append(terms, html.Tag("dt", html.Props{Text: field.Label}))
		if len(field.Items) > 0 {
			items := make([]ui.Node, 0, len(field.Items))
			for _, item := range field.Items {
				items = append(items, html.Li(html.Props{Text: item}))
			}
			terms = append(terms, html.Tag("dd", html.Props{}, html.Ul(html.Props{}, items...)))
			continue
		}
		terms = append(terms, html.Tag("dd", html.Props{Text: field.Text}))
	}
	children = append(children, html.Tag("dl", html.Props{}, terms...))
	return html.Section(
		html.Props{
			Aria: map[string]string{"label": "Atom documentation"},
			Data: map[string]string{"component": "code-symbol-atom"},
		},
		children...,
	)
}

// referenceList renders a bounded call list.
func referenceList(props Props, title string, references []Reference, component string) ui.Node {
	if len(references) == 0 {
		return html.P(html.Props{
			Text: title + ": none recorded at this revision.",
			Data: map[string]string{"component": component},
		})
	}
	items := make([]ui.Node, 0, len(references))
	for _, reference := range references {
		items = append(items, html.Li(html.Props{
			Text: reference.Name + " · " + reference.Path + ":" +
				strconv.FormatUint(uint64(reference.Line), 10),
		}))
	}
	return html.Section(
		html.Props{
			Aria: map[string]string{"label": title},
			Data: map[string]string{"component": component},
		},
		html.H4(html.Props{Text: title}),
		html.Ul(html.Props{}, items...),
	)
}

// stateNode renders the states in which the surface has nothing to draw.
func stateNode(props Props) (ui.Node, bool) {
	switch {
	case props.Unavailable:
		reason := props.UnavailableReason
		if reason == "" {
			reason = "This collection cannot be read yet."
		}
		return primitives.EmptyState(primitives.EmptyStateProps{
			Title: "Not available", Body: reason, Mode: props.Mode,
		}), true
	case props.Loading:
		return html.P(html.Props{
			Text: "Mapping the repository with the Go toolchain…",
			Aria: map[string]string{"live": "polite"},
			Data: map[string]string{"component": "code-collection-loading"},
		}), true
	case props.Failed:
		return primitives.ErrorState(primitives.ErrorStateProps{
			Title: "The collection could not be read",
			Body: "The coordinator could not map this repository. Nothing in it was " +
				"changed; mapping only reads.",
			ActionLabel: reloadLabel(props.OnReload), Mode: props.Mode, OnAction: props.OnReload,
		}), true
	default:
		return nil, false
	}
}

func joinNames(values []string) string {
	joined := ""
	for index, value := range values {
		if index > 0 {
			joined += ", "
		}
		joined += value
	}
	return joined
}

func reloadLabel(onReload func()) string {
	if onReload == nil {
		return ""
	}
	return "Reload the collection"
}

func boolLabel(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

// countLabel renders a count with a noun that agrees with it.
func countLabel(count uint32, singular, plural string) string {
	noun := plural
	if count == 1 {
		noun = singular
	}
	return strconv.FormatUint(uint64(count), 10) + " " + noun
}

// signatureBlock renders a declaration's signature so a long one is readable.
//
// A signature is the first thing somebody reads about a declaration, and Go
// signatures are frequently wider than a pane. It wraps rather than scrolling
// sideways, because a signature half of which is off-screen is a signature
// nobody read.
func signatureBlock(props Props, signature string) ui.Node {
	tokens := props.Mode.Tokens()
	return html.Section(
		html.Props{
			Aria: map[string]string{"label": "Signature"},
			Data: map[string]string{"component": "code-symbol-signature"},
		},
		html.H4(html.Props{Text: "Signature"}),
		html.Pre(
			html.Props{
				TabIndex: html.TabIndexZero,
				Aria:     map[string]string{"label": "Declaration signature"},
				Class: css.New(
					css.Margin(css.Zero), css.Padding(css.Px(tokens.Spacing.MD)),
					css.Bg(css.Hex(string(tokens.Colors.SurfaceInset))),
					css.Border(
						css.Px(tokens.Geometry.BorderWidth),
						css.Hex(string(tokens.Colors.BorderSubtle)),
					),
					css.Rounded(css.Px(tokens.Geometry.RadiusSmall)),
					css.Font(css.FontStack(tokens.Fonts.Code)),
					css.FontSize(css.Px(tokens.Typography.Code.Size)),
					css.LineHeightLen(css.Px(tokens.Typography.Code.LineHeight)),
					css.WhiteSpace.PreWrap,
					css.OverflowWrap.Anywhere,
					css.MinWidth(css.Zero),
				).String(),
			},
			html.Code(html.Props{}, html.Text(signature)),
		),
	)
}
