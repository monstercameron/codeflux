// Package filetree renders a repository as the tree of files it is on disk,
// and shows one file's contents when a person opens it.
//
// The collection's other surfaces answer questions about declarations: what a
// package offers, what an atom promises. This one answers the question people
// actually arrive with when they are new to a repository — what is in here —
// and it answers it in the shape the repository already has, because a person
// who knows the tree can find things in it without learning a second
// vocabulary first.
package filetree

import (
	"sort"
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

// File is one file in the collection.
type File struct {
	Path        string
	Kind        string
	Generated   bool
	ImportPath  string
	SymbolCount uint32
	AtomCount   uint32
}

// Declaration is one thing a file declares.
type Declaration struct {
	Key      string
	Name     string
	Kind     string
	Receiver string
	Line     uint32
	Atom     bool
	Exported bool
}

// Content is one file as it is on disk.
type Content struct {
	File         File
	Text         string
	Lines        uint32
	Truncated    bool
	Declarations []Declaration
}

// Props drives the file tree and the file it has open.
type Props struct {
	Tokens design.Tokens

	State        LoadState
	ErrorMessage string
	Files        []File
	Revision     string
	Dirty        bool
	TotalFiles   uint32
	Search       string

	// Expanded is the set of directory paths a person has opened. It lives in
	// the caller so this package holds no state and a reload keeps the tree
	// where the reader left it.
	Expanded     map[string]bool
	SelectedPath string
	Content      *Content
	ContentState LoadState
	ContentError string

	OnSearch     func(string)
	OnToggleDir  func(string)
	OnSelectFile func(string)
	OnSelectLine func(uint32)
	SelectedLine uint32
	OnRetry      func()
}

// Component renders the tree beside the open file.
func Component(props Props) ui.Node {
	tokens := props.Tokens
	mode := primitives.Mode{
		Theme: tokens.Theme, Density: tokens.Density,
		HighContrast: tokens.Theme == design.ThemeHighContrast, ReducedMotion: tokens.ReducedMotion,
	}
	columns := []css.Track{css.MinMax(css.TrackLen(css.Zero), css.Fr(1))}
	body := []ui.Node{treeRegion(props, mode)}
	if props.State == LoadReady && len(props.Files) > 0 {
		// The tree is the index and the file is the subject, so the file takes
		// the width. A path needs three hundred pixels; a line of code needs
		// as many as it has.
		columns = []css.Track{
			css.TrackLen(css.Px(340)),
			css.MinMax(css.TrackLen(css.Zero), css.Fr(1)),
		}
		body = append(body, contentRegion(props, mode))
	}
	return html.Main(html.Props{
		ID: "main-content", TabIndex: -1,
		Data: map[string]string{
			"component": "file-tree-shell", "state": string(stateOrUnavailable(props.State)),
			"focus-region": "conversation", "focus-order": "2",
			"file-count": strconv.Itoa(len(props.Files)),
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
		Aria:  map[string]string{"label": "Code"},
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
				Text: "Code",
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

func summaryLine(props Props) string {
	switch stateOrUnavailable(props.State) {
	case LoadLoading:
		return "Reading the repository…"
	case LoadFailed:
		return "The repository could not be read"
	case LoadUnavailable:
		return "No repository is open"
	}
	parts := []string{countPhrase(len(props.Files), "file")}
	if uint32(len(props.Files)) != props.TotalFiles && props.TotalFiles > 0 {
		parts = []string{
			strconv.Itoa(len(props.Files)) + " of " + countPhrase(int(props.TotalFiles), "file"),
		}
	}
	atoms := 0
	for _, file := range props.Files {
		atoms += int(file.AtomCount)
	}
	if atoms > 0 {
		parts = append(parts, countPhrase(atoms, "documented atom"))
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

func treeRegion(props Props, mode primitives.Mode) ui.Node {
	tokens := props.Tokens
	children := []ui.Node{searchControl(props, mode)}
	children = append(children, treeContent(props, mode)...)
	return html.Section(html.Props{
		Aria:     map[string]string{"label": "Repository files"},
		DataAttr: html.DataAttribute{Name: "region", Value: "file-tree"},
		Data: map[string]string{
			"component": "file-tree", "file-count": strconv.Itoa(len(props.Files)),
		},
		Class: css.New(
			u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.SM)),
			css.MinWidth(css.Zero), css.MinHeight(css.Zero),
			css.H(css.Full), css.OverflowY.Auto,
		).String(),
	}, children...)
}

func searchControl(props Props, mode primitives.Mode) ui.Node {
	if props.State == LoadUnavailable {
		return nil
	}
	tokens := props.Tokens
	field := []ui.Node{primitives.TextField(primitives.TextFieldProps{
		ID: "file-search", AccessibleLabel: "Filter files",
		Placeholder: "Filter files", Value: props.Search,
		Mode: mode, OnInput: props.OnSearch,
	})}
	if strings.TrimSpace(props.Search) != "" && props.OnSearch != nil {
		field = append(field, clearControl(props, tokens))
	}
	return html.Div(html.Props{
		Class: css.New(css.Position.Relative, css.W(css.Full)).String(),
	}, field...)
}

func clearControl(props Props, tokens design.Tokens) ui.Node {
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
	buttonProps := html.PropsOf(html.OnClick(func() { props.OnSearch("") }))
	buttonProps.ID = "file-search-clear"
	buttonProps.Type = "button"
	buttonProps.Title = "Clear the filter"
	buttonProps.Aria = map[string]string{"label": "Clear the filter"}
	buttonProps.Class = css.New(rules...).String()
	return html.Div(html.Props{
		Class: css.New(css.Position.Absolute, css.Right(css.Px(6)), css.Top(css.Px(7))).String(),
	}, html.Button(buttonProps,
		primitives.Icon(primitives.IconProps{
			Name: primitives.IconClose, Size: primitives.IconSizeSmall,
		}),
	))
}

func treeContent(props Props, mode primitives.Mode) []ui.Node {
	switch stateOrUnavailable(props.State) {
	case LoadLoading:
		if len(props.Files) > 0 {
			break
		}
		return []ui.Node{primitives.InlineAlert(primitives.InlineAlertProps{
			Title: "Reading the repository", Tone: design.StatusNeutral, Mode: mode,
			Message: "Mapping the tree at its current revision.",
		})}
	case LoadFailed:
		return []ui.Node{primitives.ErrorState(primitives.ErrorStateProps{
			Title: "Files unavailable", Body: failureBody(props.ErrorMessage), Mode: mode,
			ActionLabel: retryLabel(props.OnRetry), OnAction: props.OnRetry,
		})}
	case LoadUnavailable:
		return []ui.Node{primitives.EmptyState(primitives.EmptyStateProps{
			Title: "No repository is open", Mode: mode,
			Body: "The file tree is read from a repository's own working tree. Open one to browse it.",
		})}
	}
	if len(props.Files) == 0 {
		if strings.TrimSpace(props.Search) != "" {
			return []ui.Node{primitives.EmptyState(primitives.EmptyStateProps{
				Title: "No file matched", Mode: mode,
				Body: "No path in this repository contains that. Clear the filter to see the tree.",
			})}
		}
		return []ui.Node{primitives.EmptyState(primitives.EmptyStateProps{
			Title: "No file recorded", Mode: mode,
			Body: "The mapper recorded no file at this revision.",
		})}
	}
	return []ui.Node{html.Div(html.Props{
		Role:  "tree",
		Aria:  map[string]string{"label": "Repository files"},
		Class: css.New(u.Flex, u.FlexCol, css.Gap(css.Px(1))).String(),
	}, treeRows(props, mode)...)}
}

func failureBody(message string) string {
	if strings.TrimSpace(message) == "" {
		return "The coordinator did not answer the file read."
	}
	return message
}

func retryLabel(retry func()) string {
	if retry == nil {
		return ""
	}
	return "Try again"
}

// node is one entry in the tree, either a directory or a file.
type node struct {
	Name     string
	Path     string
	Dir      bool
	File     File
	Children []*node
}

// buildTree turns a flat list of paths into the directories they imply.
//
// The mapper reports files, not directories, so the intermediate ones are
// inferred here. A directory with exactly one child directory is not collapsed
// into it: the collapsed form reads well in a listing and badly in a tree,
// where the reader is using the indentation to keep their place.
func buildTree(files []File) *node {
	root := &node{Dir: true}
	index := map[string]*node{"": root}
	for _, file := range files {
		segments := strings.Split(file.Path, "/")
		parent := root
		prefix := ""
		for depth, segment := range segments {
			leaf := depth == len(segments)-1
			if prefix == "" {
				prefix = segment
			} else {
				prefix += "/" + segment
			}
			if existing, present := index[prefix]; present && !leaf {
				parent = existing
				continue
			}
			child := &node{Name: segment, Path: prefix, Dir: !leaf}
			if leaf {
				child.File = file
			}
			parent.Children = append(parent.Children, child)
			index[prefix] = child
			parent = child
		}
	}
	sortNode(root)
	return root
}

// sortNode puts directories before files and orders each by name, so the tree
// reads the same way twice.
func sortNode(current *node) {
	sort.SliceStable(current.Children, func(first, second int) bool {
		left, right := current.Children[first], current.Children[second]
		if left.Dir != right.Dir {
			return left.Dir
		}
		return left.Name < right.Name
	})
	for _, child := range current.Children {
		sortNode(child)
	}
}

// treeRows flattens the tree into the rows currently visible.
func treeRows(props Props, mode primitives.Mode) []ui.Node {
	root := buildTree(props.Files)
	rows := make([]ui.Node, 0, len(props.Files)*2)
	var walk func(current *node, depth int)
	walk = func(current *node, depth int) {
		for _, child := range current.Children {
			if child.Dir {
				// A filtered tree opens itself: hiding matches behind a closed
				// directory would make the filter look like it found nothing.
				open := props.Expanded[child.Path] || strings.TrimSpace(props.Search) != ""
				rows = append(rows, directoryRow(props, child, depth, open))
				if open {
					walk(child, depth+1)
				}
				continue
			}
			rows = append(rows, fileRow(props, child, depth, mode))
		}
	}
	walk(root, 0)
	return rows
}

func directoryRow(props Props, current *node, depth int, open bool) ui.Node {
	tokens := props.Tokens
	mark := primitives.IconChevronRight
	if open {
		mark = primitives.IconChevronDown
	}
	buttonProps := html.PropsOf(html.OnClick(func() {
		if props.OnToggleDir != nil {
			props.OnToggleDir(current.Path)
		}
	}))
	buttonProps.Type = "button"
	buttonProps.Aria = map[string]string{"expanded": boolText(open)}
	buttonProps.Data = map[string]string{
		"component": "tree-directory", "path": current.Path, "open": boolText(open),
	}
	buttonProps.Class = rowClass(tokens, depth, false)
	return html.Div(html.Props{Role: "treeitem"},
		html.Button(buttonProps,
			primitives.Icon(primitives.IconProps{Name: mark, Size: primitives.IconSizeSmall}),
			html.Span(html.Props{
				Class: css.New(
					css.Font(css.FontStack(tokens.Fonts.UI)),
					css.FontWeight.Semibold,
					css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
				).String(),
				Text: current.Name,
			}),
		),
	)
}

func fileRow(props Props, current *node, depth int, mode primitives.Mode) ui.Node {
	tokens := props.Tokens
	selected := current.Path == props.SelectedPath
	buttonProps := html.PropsOf(html.OnClick(func() {
		if props.OnSelectFile != nil {
			props.OnSelectFile(current.Path)
		}
	}))
	buttonProps.Type = "button"
	buttonProps.Aria = map[string]string{"pressed": boolText(selected)}
	buttonProps.Data = map[string]string{
		"component": "tree-file", "path": current.Path,
		"selected": boolText(selected), "kind": current.File.Kind,
	}
	buttonProps.Class = rowClass(tokens, depth, selected)
	children := []ui.Node{
		primitives.Icon(primitives.IconProps{
			Name: primitives.IconTranscript, Size: primitives.IconSizeSmall,
		}),
		html.Span(html.Props{
			Class: css.New(
				css.Font(css.FontStack(tokens.Fonts.Code)),
				css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
				css.Overflow.Hidden, css.TextOverflowEllipsis(), css.WhiteSpace.NoWrap,
			).String(),
			Text: current.Name,
		}),
	}
	if badge := fileBadge(current.File, mode); badge != nil {
		children = append(children, html.Span(html.Props{
			Class: css.New(css.Margin(css.RawLength("0 0 0 auto")), css.FlexShrink(css.Num(0))).String(),
		}, badge))
	}
	return html.Div(html.Props{Role: "treeitem"},
		html.Button(buttonProps, children...),
	)
}

// fileBadge marks what a file carries, and marks nothing when it carries
// nothing worth a mark. A badge on every row is a badge nobody reads.
func fileBadge(file File, mode primitives.Mode) ui.Node {
	switch {
	case file.AtomCount > 0:
		return primitives.Badge(primitives.BadgeProps{
			Label:  countPhrase(int(file.AtomCount), "atom"),
			Status: design.StatusAccent, Mode: mode,
		})
	case file.Generated:
		return primitives.Badge(primitives.BadgeProps{
			Label: "generated", Status: design.StatusNeutral, Mode: mode,
		})
	case file.Kind == "test":
		return primitives.Badge(primitives.BadgeProps{
			Label: "test", Status: design.StatusNeutral, Mode: mode,
		})
	default:
		return nil
	}
}

func rowClass(tokens design.Tokens, depth int, selected bool) string {
	background := css.Transparent
	if selected {
		background = css.Hex(string(tokens.Colors.Surface2))
	}
	rules := []css.Rule{
		u.Flex, u.ItemsCenter, css.Gap(css.Px(6)),
		css.W(css.Full), css.TextAlign.Left,
		css.Padding(css.RawLength("3px 8px 3px " + strconv.Itoa(8+depth*14) + "px")),
		css.Bg(background),
		css.Border(css.Px(0), css.Hex(string(tokens.Colors.BorderSubtle))),
		css.Rounded(css.Px(tokens.Geometry.RadiusSmall)),
		css.Font(css.FontStack(tokens.Fonts.Code)),
		css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
		css.LineHeightLen(css.Px(tokens.Typography.Metadata.LineHeight)),
		css.TextColor(css.Hex(string(tokens.Colors.TextSecondary))),
		css.MinWidth(css.Zero),
		css.Cursor.Pointer,
	}
	rules = append(rules, css.Hover(
		css.Bg(css.Hex(string(tokens.Colors.Surface1))),
	)...)
	rules = append(rules, css.FocusVisible(
		css.Outline(css.Px(tokens.Geometry.FocusRingWidth), css.Hex(string(tokens.Colors.FocusRing))),
		css.OutlineOffset(css.Px(-2)),
	)...)
	return css.New(rules...).String()
}

func boolText(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
