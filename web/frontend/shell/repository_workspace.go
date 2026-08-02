package shell

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

// RepositoryRow is one repository the coordinator has recorded.
type RepositoryRow struct {
	RepositoryID string
	Name         string
	// RecordedRevision is the revision stored when the repository was opened.
	// It is not the working tree's current head: only an inspection reads that,
	// and the two disagree the moment somebody commits.
	RecordedRevision string
	Revision         uint64
	// Current marks the repository this coordinator was started against. Only
	// one is open at a time, and which one changes what every other page shows.
	Current bool
	// Accessible reports whether this session may route into the repository.
	Accessible bool
	// ThreadPath enters the repository through an open thread. It is empty when
	// no thread is open, because a repository with no conversation has no
	// workspace to show.
	ThreadPath string
}

// RepositoryInspection is what Git says about one repository right now.
//
// WorkingTreeKnown separates "Git answered, and the tree is clean" from "Git
// did not answer", which an empty status field alone cannot express.
type RepositoryInspection struct {
	Branch           string
	HeadRevision     string
	Detached         bool
	Dirty            bool
	ChangedPathCount uint32
	WorkingTreeKnown bool
	Warnings         []string
}

// RepositoryWorkspaceProps drives the repositories surface.
type RepositoryWorkspaceProps struct {
	Tokens          design.Tokens
	State           SurfaceLoadState
	ErrorMessage    string
	Rows            []RepositoryRow
	SelectedID      string
	Inspection      *RepositoryInspection
	InspectionState SurfaceLoadState
	InspectionError string
	// OpenInstruction states how a person opens a repository that is not on
	// this list. The coordinator refuses to grant itself authority over a
	// directory a browser named, so this is a sentence rather than a button.
	OpenInstruction string
	OnSelect        func(string)
	OnRetry         func()
	OnNavigatePath  func(string)
}

// RepositoryWorkspaceShell renders the repositories this coordinator knows.
//
// The page used to be four boxes, three of which described behaviour instead
// of showing it -- a canonical path that was never printed, warnings that were
// never listed, and a browse button that opened nothing. What a person needs
// here is which repositories exist, which one is open, and what its working
// tree looks like before they set an agent loose in it.
func RepositoryWorkspaceShell(props RepositoryWorkspaceProps) ui.Node {
	tokens := props.Tokens
	mode := primitiveMode(tokens)
	columns := []css.Track{css.MinMax(css.TrackLen(css.Zero), css.Fr(1))}
	body := []ui.Node{repositoryListRegion(props, mode)}
	if props.State == SurfaceReady && len(props.Rows) > 0 {
		columns = append(columns, css.TrackLen(css.Px(420)))
		body = append(body, repositoryInspectionRegion(props, mode))
	}
	return html.Main(html.Props{
		ID: "main-content", TabIndex: -1,
		Data: map[string]string{
			"component":    "repository-chooser-shell",
			"state":        string(surfaceStateOrUnavailable(props.State)),
			"focus-region": "conversation", "focus-order": "2",
			"repository-count": strconv.Itoa(len(props.Rows)),
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
		repositoryHeader(props, tokens),
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

func repositoryHeader(props RepositoryWorkspaceProps, tokens design.Tokens) ui.Node {
	return html.Header(html.Props{
		Aria:  map[string]string{"label": "Repositories"},
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
				Text: "Repositories",
			}),
			html.P(html.Props{
				Class: css.New(
					css.Margin(css.Zero),
					css.Font(css.FontStack(tokens.Fonts.Code)),
					css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
					css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
				).String(),
				Text: repositorySummaryLine(props),
			}),
		),
	)
}

// repositorySummaryLine counts what exists and says which one is open. Which
// repository is open decides what every other page in the console is about.
func repositorySummaryLine(props RepositoryWorkspaceProps) string {
	switch surfaceStateOrUnavailable(props.State) {
	case SurfaceLoading:
		return "Asking the coordinator what it can open…"
	case SurfaceFailed:
		return "Repositories could not be listed"
	case SurfaceUnavailable:
		return "Not connected to a coordinator"
	}
	line := strconv.Itoa(len(props.Rows)) + " repositories"
	if len(props.Rows) == 1 {
		line = "1 repository"
	}
	for _, row := range props.Rows {
		if row.Current {
			return line + " · " + row.Name + " is open"
		}
	}
	return line + " · none open"
}

func repositoryListRegion(props RepositoryWorkspaceProps, mode primitives.Mode) ui.Node {
	tokens := props.Tokens
	return html.Section(html.Props{
		Aria:     map[string]string{"label": "Recorded repositories"},
		DataAttr: html.DataAttribute{Name: "region", Value: "repository-list"},
		Data: map[string]string{
			"component": "repository-list", "row-count": strconv.Itoa(len(props.Rows)),
		},
		Class: css.New(
			u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.SM)),
			css.MinWidth(css.Zero), css.MinHeight(css.Zero),
			css.MaxWidth(css.Px(820)),
			css.H(css.Full), css.OverflowY.Auto,
		).String(),
	}, repositoryListContent(props, mode)...)
}

func repositoryListContent(props RepositoryWorkspaceProps, mode primitives.Mode) []ui.Node {
	switch surfaceStateOrUnavailable(props.State) {
	case SurfaceLoading:
		return []ui.Node{primitives.InlineAlert(primitives.InlineAlertProps{
			Title: "Reading repositories", Tone: design.StatusNeutral, Mode: mode,
			Message: "Asking the coordinator which repositories it has recorded.",
		})}
	case SurfaceFailed:
		return []ui.Node{primitives.ErrorState(primitives.ErrorStateProps{
			Title: "Repositories unavailable", Body: memoryFailureBody(props.ErrorMessage),
			Mode: mode, ActionLabel: memoryRetryLabel(props.OnRetry), OnAction: props.OnRetry,
		})}
	case SurfaceUnavailable:
		return []ui.Node{primitives.EmptyState(primitives.EmptyStateProps{
			Title: "Not connected", Mode: mode,
			Body: "The console lists repositories from the running coordinator. Reconnect to see them.",
		})}
	}
	if len(props.Rows) == 0 {
		return []ui.Node{primitives.EmptyState(primitives.EmptyStateProps{
			Title: "No repository recorded", Mode: mode,
			Body: repositoryOpenInstruction(props),
		})}
	}
	items := make([]ui.Node, 0, len(props.Rows))
	for _, row := range props.Rows {
		items = append(items, repositoryRow(props, row, mode))
	}
	return []ui.Node{html.Div(html.Props{
		Role: "list",
		Class: css.New(
			u.Flex, u.FlexCol, css.Gap(css.Px(props.Tokens.Spacing.XS)),
		).String(),
	}, items...)}
}

// repositoryOpenInstruction says how a repository is added. The coordinator
// refuses to grant itself authority over a directory a browser named, so the
// honest answer is the command that grants it, not a button that cannot.
func repositoryOpenInstruction(props RepositoryWorkspaceProps) string {
	if instruction := strings.TrimSpace(props.OpenInstruction); instruction != "" {
		return instruction
	}
	return "Start the coordinator against a repository to record one: " +
		"codeflux start --repository <path>. Opening a directory from the browser " +
		"would grant the coordinator authority nobody approved."
}

func repositoryRow(props RepositoryWorkspaceProps, row RepositoryRow, mode primitives.Mode) ui.Node {
	tokens := props.Tokens
	selected := row.RepositoryID == props.SelectedID
	background := tokens.Colors.Surface1
	border := tokens.Colors.BorderSubtle
	if selected {
		background = tokens.Colors.Surface2
		border = tokens.Colors.Active
	}
	rules := []css.Rule{
		u.Flex, u.FlexCol, css.Gap(css.Px(6)),
		css.W(css.Full), css.TextAlign.Left,
		css.Padding(css.RawLength("10px 12px")),
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
			props.OnSelect(row.RepositoryID)
		}
	}))
	buttonProps.Type = "button"
	buttonProps.Aria = map[string]string{"pressed": memoryPressedValue(selected)}
	buttonProps.Data = map[string]string{
		"component": "repository-row", "repository-id": row.RepositoryID,
		"current": memoryPressedValue(row.Current), "selected": memoryPressedValue(selected),
	}
	buttonProps.Class = css.New(rules...).String()
	marks := []ui.Node{
		primitives.IconChip(primitives.IconChipProps{
			Label: "Repository", Kind: design.KindCode, Mode: mode,
		}),
	}
	if row.Current {
		// Accent, not "running": this badge identifies the repository the
		// session is working in, which is not a claim about a run's state.
		marks = append(marks, primitives.Badge(primitives.BadgeProps{
			Label: "In this session", Status: design.StatusAccent, Mode: mode,
		}))
	}
	if row.ThreadPath == "" {
		marks = append(marks, primitives.Badge(primitives.BadgeProps{
			Label: "No thread", Status: design.StatusNeutral, Mode: mode,
		}))
	}
	if !row.Accessible {
		marks = append(marks, primitives.Badge(primitives.BadgeProps{
			Label: "Not available this session", Status: design.StatusWarning, Mode: mode,
		}))
	}
	return html.Div(html.Props{Role: "listitem"},
		html.Button(buttonProps,
			html.Span(html.Props{
				Class: css.New(u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.XS)), css.FlexWrap.Wrap).String(),
			}, marks...),
			html.Span(html.Props{
				Class: css.New(
					css.Font(css.FontStack(tokens.Fonts.Reading)),
					css.FontSize(css.Px(tokens.Typography.Body.Size)),
					css.LineHeightLen(css.Px(tokens.Typography.Body.LineHeight)),
					css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
				).String(),
				Text: row.Name,
			}),
			html.Span(html.Props{
				Class: css.New(
					u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.XS)), css.FlexWrap.Wrap,
					css.Font(css.FontStack(tokens.Fonts.Code)),
					css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
					css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
				).String(),
				Text: repositoryRowMetaLine(row),
			}),
		),
	)
}

// repositoryRowMetaLine states what was recorded when the repository was
// opened. It is labelled "recorded" because it is history: the working tree
// has moved on unless nobody has committed since.
// The row deliberately omits the record's own revision counter: it is
// bookkeeping the coordinator uses for concurrent writes and means nothing to
// somebody choosing a repository, so it appears only under identity.
func repositoryRowMetaLine(row RepositoryRow) string {
	if revision := strings.TrimSpace(row.RecordedRevision); revision != "" {
		return "recorded at " + revision
	}
	return "no recorded revision"
}

func repositoryInspectionRegion(props RepositoryWorkspaceProps, mode primitives.Mode) ui.Node {
	tokens := props.Tokens
	return html.Aside(html.Props{
		Aria: map[string]string{"label": "Selected repository"},
		Data: map[string]string{
			"component": "repository-inspection",
			"state":     string(surfaceStateOrUnavailable(props.InspectionState)),
		},
		Class: css.New(
			u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.MD)),
			css.MinWidth(css.Zero), css.MinHeight(css.Zero),
			css.H(css.Full), css.OverflowY.Auto,
			css.Padding(css.RawLength("0 0 0 20px")),
			css.BorderLeft(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
		).String(),
	}, repositoryInspectionContent(props, mode)...)
}

func repositoryInspectionContent(props RepositoryWorkspaceProps, mode primitives.Mode) []ui.Node {
	tokens := props.Tokens
	eyebrow := markedEyebrow(primitives.IconRepositories, "Selected repository", tokens)
	switch surfaceStateOrUnavailable(props.InspectionState) {
	case SurfaceLoading:
		return []ui.Node{eyebrow, primitives.InlineAlert(primitives.InlineAlertProps{
			Title: "Reading the working tree", Tone: design.StatusNeutral, Mode: mode,
			Message: "Asking Git what this repository looks like right now.",
		})}
	case SurfaceFailed:
		return []ui.Node{eyebrow, primitives.InlineAlert(primitives.InlineAlertProps{
			Title: "Working tree unavailable", Tone: design.StatusWarning, Mode: mode,
			Message: memoryFailureBody(props.InspectionError),
		})}
	}
	row, found := repositoryRowByID(props.Rows, props.SelectedID)
	if !found {
		return []ui.Node{eyebrow, html.P(html.Props{
			Class: memoryProseClass(tokens),
			Text:  "Choose a repository to see what its working tree looks like right now.",
		})}
	}
	children := []ui.Node{
		eyebrow,
		html.H2(html.Props{
			Class: css.New(
				css.Margin(css.Zero),
				css.Font(css.FontStack(tokens.Fonts.Reading)),
				css.FontSize(css.Px(tokens.Typography.SectionTitle.Size)),
				css.LineHeightLen(css.Px(tokens.Typography.SectionTitle.LineHeight)),
				css.FontWeight.Normal,
				css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
			).String(),
			Text: row.Name,
		}),
	}
	if action := repositoryEntryAction(props, row, mode); action != nil {
		children = append(children, action)
	}
	if props.Inspection != nil {
		children = append(children,
			memoryDetailSection("Working tree", primitives.IconBranch,
				repositoryWorkingTreeFields(*props.Inspection), tokens),
			repositoryWarningsSection(props.Inspection.Warnings, mode, tokens),
		)
	}
	children = append(children,
		memoryDetailSection("Identity", primitives.IconProof, repositoryIdentityFields(row), tokens),
		html.P(html.Props{Class: memoryProseClass(tokens), Text: repositoryOpenInstruction(props)}),
	)
	return children
}

func repositoryRowByID(rows []RepositoryRow, id string) (RepositoryRow, bool) {
	for _, row := range rows {
		if row.RepositoryID == id {
			return row, true
		}
	}
	return RepositoryRow{}, false
}

// repositoryEntryAction offers the way in, and only when there is one. A
// repository with no open thread has no workspace to enter, and a link that
// leads nowhere is worse than none.
func repositoryEntryAction(props RepositoryWorkspaceProps, row RepositoryRow, mode primitives.Mode) ui.Node {
	if row.ThreadPath == "" || !row.Accessible {
		return html.P(html.Props{
			Class: memoryProseClass(props.Tokens),
			Text:  "No thread is open in this repository, so there is no workspace to enter yet.",
		})
	}
	// The action sits at its own width rather than spanning the column: a
	// button as wide as the panel reads as a banner, not as something to press.
	return html.Div(html.Props{Class: css.New(u.Flex, u.ItemsCenter).String()},
		primitives.Button(primitives.ButtonProps{
			Label: "Open the workspace", Primary: true, Mode: mode,
			LeadingIcon: primitives.IconThread,
			OnClick: func() {
				if props.OnNavigatePath != nil {
					props.OnNavigatePath(row.ThreadPath)
				}
			},
		}),
	)
}

// repositoryWorkingTreeFields reports what Git answered. An unanswered read is
// said to be unanswered rather than drawn as a clean tree, because "clean" is
// the state somebody starts an agent on the strength of.
func repositoryWorkingTreeFields(inspection RepositoryInspection) []MemoryDetailField {
	if !inspection.WorkingTreeKnown {
		return []MemoryDetailField{{
			Label: "State",
			Value: "Git did not answer, so the working tree is unknown rather than clean.",
		}}
	}
	fields := make([]MemoryDetailField, 0, 4)
	switch {
	case inspection.Detached:
		fields = append(fields, MemoryDetailField{
			Label: "Branch",
			Value: "Detached at " + shortRepositoryRevision(inspection.HeadRevision),
		})
	case strings.TrimSpace(inspection.Branch) != "":
		fields = append(fields, MemoryDetailField{Label: "Branch", Value: inspection.Branch})
	default:
		fields = append(fields, MemoryDetailField{Label: "Branch", Value: "Not reported"})
	}
	if revision := strings.TrimSpace(inspection.HeadRevision); revision != "" {
		fields = append(fields, MemoryDetailField{Label: "Head", Value: revision})
	}
	fields = append(fields, MemoryDetailField{Label: "State", Value: repositoryWorkingTreeState(inspection)})
	return fields
}

// repositoryWorkingTreeState counts what is uncommitted. "Uncommitted changes"
// reads the same for one file and four hundred, and those are different
// decisions about whether to start a run.
func repositoryWorkingTreeState(inspection RepositoryInspection) string {
	if !inspection.Dirty {
		return "Clean"
	}
	switch inspection.ChangedPathCount {
	case 0:
		return "Uncommitted changes"
	case 1:
		return "1 uncommitted change"
	default:
		return strconv.FormatUint(uint64(inspection.ChangedPathCount), 10) + " uncommitted changes"
	}
}

func repositoryWarningsSection(warnings []string, mode primitives.Mode, tokens design.Tokens) ui.Node {
	if len(warnings) == 0 {
		return nil
	}
	children := []ui.Node{markedEyebrow(primitives.IconWarning, "Warnings", tokens)}
	for _, warning := range warnings {
		// A stacked block rather than an inline alert: the alert lays its title
		// beside its message, which in a 420-pixel column left four words on
		// the left and a ragged paragraph on the right.
		children = append(children, html.Div(html.Props{
			Data: map[string]string{"component": "repository-warning"},
			Class: css.New(
				u.Flex, css.Gap(css.Px(tokens.Spacing.XS)),
				css.Padding(css.RawLength("8px 10px")),
				css.Bg(css.Hex(string(tokens.Colors.Surface1))),
				css.BorderLeft(css.Px(2), css.Hex(string(tokens.Colors.Warning))),
				css.Rounded(css.Px(tokens.Geometry.RadiusSmall)),
			).String(),
		},
			html.Span(html.Props{
				Aria:  map[string]string{"hidden": "true"},
				Class: css.New(css.TextColor(css.Hex(string(tokens.Colors.Warning)))).String(),
			}, primitives.Icon(primitives.IconProps{
				Name: primitives.IconWarning, Size: primitives.IconSizeSmall,
			})),
			html.Span(html.Props{
				Class: css.New(
					css.Font(css.FontStack(tokens.Fonts.Reading)),
					css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
					css.LineHeightLen(css.Px(tokens.Typography.Body.LineHeight)),
					css.TextColor(css.Hex(string(tokens.Colors.TextSecondary))),
				).String(),
				Text: warning,
			}),
		))
	}
	return html.Section(html.Props{
		Aria:  map[string]string{"label": "Repository warnings"},
		Data:  map[string]string{"component": "repository-warnings", "count": strconv.Itoa(len(warnings))},
		Class: css.New(u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.XS))).String(),
	}, children...)
}

func repositoryIdentityFields(row RepositoryRow) []MemoryDetailField {
	fields := []MemoryDetailField{{Label: "Repository", Value: row.RepositoryID}}
	if revision := strings.TrimSpace(row.RecordedRevision); revision != "" {
		fields = append(fields, MemoryDetailField{Label: "Recorded at", Value: revision})
	}
	fields = append(fields, MemoryDetailField{
		Label: "Entry revision", Value: strconv.FormatUint(row.Revision, 10),
	})
	if row.Current {
		fields = append(fields, MemoryDetailField{
			Label: "Session", Value: "This coordinator was started against this repository",
		})
	}
	return fields
}

func shortRepositoryRevision(revision string) string {
	const shortLength = 7
	if len(revision) <= shortLength {
		return revision
	}
	return revision[:shortLength]
}
