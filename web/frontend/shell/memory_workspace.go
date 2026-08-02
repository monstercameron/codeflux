package shell

import (
	"strconv"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/design"
	frontendi18n "codeflux.dev/codeflux/web/frontend/i18n"
	"codeflux.dev/codeflux/web/frontend/memoryinspector"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/css/u"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// SurfaceLoadState is what a route surface currently knows about its subject.
//
// "Unavailable" is distinct from "empty": a project with nothing learned yet
// and a project whose memory could not be read look identical if both render
// an empty list, and only one of them is a problem the reader can act on. One
// vocabulary is shared across surfaces so the same situation is never called
// two different things on two pages.
type SurfaceLoadState string

const (
	// SurfaceUnavailable means the surface has no subject to read: no project
	// is selected, so there is nothing to succeed or fail at.
	SurfaceUnavailable SurfaceLoadState = "unavailable"
	SurfaceLoading     SurfaceLoadState = "loading"
	SurfaceReady       SurfaceLoadState = "ready"
	SurfaceFailed      SurfaceLoadState = "failed"
)

// MemoryArtifactRow is one artifact as the list shows it.
type MemoryArtifactRow struct {
	ArtifactID            string
	RevisionID            string
	Kind                  domain.MemoryArtifactKind
	Maturity              domain.MaturityState
	Summary               string
	ScopeRepository       string
	RevisionNumber        uint64
	CreatedAt             time.Time
	CreatedFromCorrection bool
}

// MemoryDetailField is one labelled line of an artifact's content.
type MemoryDetailField struct {
	Label string
	Value string
}

// MemoryArtifactDetail is everything the inspector shows about one artifact:
// what it says, and where it came from.
type MemoryArtifactDetail struct {
	Row                             MemoryArtifactRow
	Fields                          []MemoryDetailField
	DerivedFrom                     []string
	InfluencedBy                    []string
	OriginKnown                     bool
	OriginArtifactID                string
	OriginUnknownReason             string
	SupportingEpisodesKnown         bool
	SupportingEpisodes              []string
	SupportingEpisodesUnknownReason string
	SupersedesRevisionID            string
	BindingState                    string
	ContentDigest                   string
}

// MemoryWorkspaceProps drives the project-memory surface.
type MemoryWorkspaceProps struct {
	Tokens       design.Tokens
	Translator   frontendi18n.Translator
	State        SurfaceLoadState
	ErrorMessage string
	Rows         []MemoryArtifactRow
	SelectedID   string
	Detail       *MemoryArtifactDetail
	DetailState  SurfaceLoadState
	DetailError  string
	KindFilter   domain.MemoryArtifactKind
	OnSelect     func(string)
	OnFilterKind func(domain.MemoryArtifactKind)
	OnRetry      func()
}

// MemoryWorkspaceShell renders what this project has learned.
//
// The page is a list beside one artifact, not a table: a memory artifact is a
// claim the agent will act on, so the surface leads with the claim in the
// reading face and keeps identity, governance, and provenance in the margin
// where they can be checked without competing with it.
func MemoryWorkspaceShell(props MemoryWorkspaceProps) ui.Node {
	tokens := props.Tokens
	mode := primitiveMode(tokens)
	rows := memoryRowsMatchingFilter(props.Rows, props.KindFilter)
	columns := []css.Track{css.MinMax(css.TrackLen(css.Zero), css.Fr(1))}
	body := []ui.Node{memoryListRegion(props, rows, mode)}
	if props.State == SurfaceReady && len(props.Rows) > 0 {
		columns = append(columns, css.TrackLen(css.Px(420)))
		body = append(body, memoryDetailRegion(props, mode))
	}
	return html.Main(html.Props{
		ID: "main-content", TabIndex: -1,
		Data: map[string]string{
			"component": "memory-shell", "state": string(surfaceStateOrUnavailable(props.State)),
			"focus-region": "conversation", "focus-order": "2",
			"artifact-count": strconv.Itoa(len(props.Rows)),
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
		memoryHeader(props, len(rows), tokens),
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

func surfaceStateOrUnavailable(value SurfaceLoadState) SurfaceLoadState {
	if value == "" {
		return SurfaceUnavailable
	}
	return value
}

func memoryHeader(props MemoryWorkspaceProps, shown int, tokens design.Tokens) ui.Node {
	return html.Header(html.Props{
		Aria:  map[string]string{"label": "Project memory"},
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
				Text: "Project memory",
			}),
			html.P(html.Props{
				Class: css.New(
					css.Margin(css.Zero),
					css.Font(css.FontStack(tokens.Fonts.Code)),
					css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
					css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
				).String(),
				Text: memorySummaryLine(props, shown),
			}),
		),
	)
}

// memorySummaryLine says how much memory there is and how much of it currently
// carries authority. The second number is the one that matters: a candidate
// fact is something the agent noticed, not something it may act on.
func memorySummaryLine(props MemoryWorkspaceProps, shown int) string {
	switch surfaceStateOrUnavailable(props.State) {
	case SurfaceLoading:
		return "Reading project memory…"
	case SurfaceFailed:
		return "Memory could not be read"
	case SurfaceUnavailable:
		return "No project selected"
	}
	total := len(props.Rows)
	authoritative := 0
	for _, row := range props.Rows {
		if row.Maturity.IsAuthorityBearing() {
			authoritative++
		}
	}
	line := memoryCountPhrase(total, "artifact")
	if shown != total {
		line = strconv.Itoa(shown) + " of " + line
	}
	return line + " · " + strconv.Itoa(authoritative) + " carry authority"
}

func memoryCountPhrase(count int, noun string) string {
	if count == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(count) + " " + noun + "s"
}

// memoryKindFilter offers only the kinds this project actually has. Eight
// permanent buttons, six of which never match anything, teach the reader that
// the filter row is decoration.
func memoryKindFilter(props MemoryWorkspaceProps, tokens design.Tokens) ui.Node {
	if props.State != SurfaceReady || len(props.Rows) == 0 {
		return nil
	}
	kinds := make([]domain.MemoryArtifactKind, 0, 8)
	seen := make(map[domain.MemoryArtifactKind]struct{}, 8)
	for _, row := range props.Rows {
		if _, duplicate := seen[row.Kind]; duplicate {
			continue
		}
		seen[row.Kind] = struct{}{}
		kinds = append(kinds, row.Kind)
	}
	if len(kinds) < 2 {
		return nil
	}
	mode := primitiveMode(tokens)
	children := []ui.Node{memoryFilterButton(props, "", "All", mode)}
	for _, kind := range kinds {
		children = append(children, memoryFilterButton(
			props, kind, memoryinspector.KindLabel(props.Translator, kind), mode,
		))
	}
	return html.Div(html.Props{
		Role: "group",
		Aria: map[string]string{"label": "Filter memory by kind"},
		Data: map[string]string{"component": "memory-kind-filter"},
		Class: css.New(
			u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.XS)),
			css.FlexWrap.Wrap,
		).String(),
	}, children...)
}

func memoryFilterButton(
	props MemoryWorkspaceProps,
	kind domain.MemoryArtifactKind,
	label string,
	mode primitives.Mode,
) ui.Node {
	return primitives.ToggleButton(primitives.ToggleButtonProps{
		Label: label, Pressed: props.KindFilter == kind, Mode: mode,
		OnToggle: func(bool) {
			if props.OnFilterKind != nil {
				props.OnFilterKind(kind)
			}
		},
	})
}

func memoryRowsMatchingFilter(rows []MemoryArtifactRow, kind domain.MemoryArtifactKind) []MemoryArtifactRow {
	if kind == "" {
		return rows
	}
	result := make([]MemoryArtifactRow, 0, len(rows))
	for _, row := range rows {
		if row.Kind == kind {
			result = append(result, row)
		}
	}
	return result
}

func memoryListRegion(props MemoryWorkspaceProps, rows []MemoryArtifactRow, mode primitives.Mode) ui.Node {
	tokens := props.Tokens
	content := []ui.Node{memoryKindFilter(props, tokens)}
	content = append(content, memoryListContent(props, rows, mode)...)
	return html.Section(html.Props{
		Aria:     map[string]string{"label": "Memory artifacts"},
		DataAttr: html.DataAttribute{Name: "region", Value: "memory-list"},
		Data:     map[string]string{"component": "memory-list", "row-count": strconv.Itoa(len(rows))},
		Class: css.New(
			u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.SM)),
			css.MinWidth(css.Zero), css.MinHeight(css.Zero),
			// A row is one claim to read, so the column stops at a readable
			// measure instead of stretching a short sentence across the pane.
			css.MaxWidth(css.Px(820)),
			css.H(css.Full), css.OverflowY.Auto,
		).String(),
	}, content...)
}

func memoryListContent(props MemoryWorkspaceProps, rows []MemoryArtifactRow, mode primitives.Mode) []ui.Node {
	switch surfaceStateOrUnavailable(props.State) {
	case SurfaceLoading:
		return []ui.Node{primitives.InlineAlert(primitives.InlineAlertProps{
			Title: "Reading project memory", Tone: design.StatusNeutral, Mode: mode,
			Message: "Loading everything this project has learned.",
		})}
	case SurfaceFailed:
		return []ui.Node{primitives.ErrorState(primitives.ErrorStateProps{
			Title: "Memory unavailable", Body: memoryFailureBody(props.ErrorMessage), Mode: mode,
			ActionLabel: memoryRetryLabel(props.OnRetry), OnAction: props.OnRetry,
		})}
	case SurfaceUnavailable:
		return []ui.Node{primitives.EmptyState(primitives.EmptyStateProps{
			Title: "No project selected", Mode: mode,
			Body: "Open a repository to read the memory the coordinator has built for it.",
		})}
	}
	if len(props.Rows) == 0 {
		return []ui.Node{primitives.EmptyState(primitives.EmptyStateProps{
			Title: "Nothing learned yet", Mode: mode,
			Body: "The coordinator records a memory artifact when a run produces evidence worth keeping. Run a task to start building this project's memory.",
		})}
	}
	if len(rows) == 0 {
		return []ui.Node{primitives.EmptyState(primitives.EmptyStateProps{
			Title: "No artifacts of this kind", Mode: mode,
			Body: "This project has memory, but none of it is of the kind you filtered to. Choose All to see everything.",
		})}
	}
	items := make([]ui.Node, 0, len(rows))
	for _, row := range rows {
		items = append(items, memoryRow(props, row, mode))
	}
	return []ui.Node{html.Div(html.Props{
		Role: "list",
		Class: css.New(
			u.Flex, u.FlexCol, css.Gap(css.Px(props.Tokens.Spacing.XS)),
		).String(),
	}, items...)}
}

func memoryFailureBody(message string) string {
	if strings.TrimSpace(message) == "" {
		return "The coordinator did not answer the memory read."
	}
	return message
}

func memoryRetryLabel(retry func()) string {
	if retry == nil {
		return ""
	}
	return "Try again"
}

// memoryRow is one claim: its kind, what it says, and how far it has been
// governed. The summary is set in the reading face because it is the sentence
// the reader is here to judge; everything else is metadata around it.
func memoryRow(props MemoryWorkspaceProps, row MemoryArtifactRow, mode primitives.Mode) ui.Node {
	tokens := props.Tokens
	selected := row.ArtifactID == props.SelectedID
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
			props.OnSelect(row.ArtifactID)
		}
	}))
	buttonProps.Type = "button"
	buttonProps.Aria = map[string]string{"pressed": memoryPressedValue(selected)}
	buttonProps.Data = map[string]string{
		"component": "memory-row", "artifact-id": row.ArtifactID,
		"kind": string(row.Kind), "maturity": string(row.Maturity),
		"selected": memoryPressedValue(selected),
	}
	buttonProps.Class = css.New(rules...).String()
	return html.Div(html.Props{Role: "listitem"},
		html.Button(buttonProps,
			html.Span(html.Props{
				Class: css.New(u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.XS)), css.FlexWrap.Wrap).String(),
			},
				primitives.IconChip(primitives.IconChipProps{
					Label: memoryinspector.KindLabel(props.Translator, row.Kind),
					Kind:  memoryinspector.DesignKindFor(row.Kind), Mode: mode,
				}),
				primitives.Badge(primitives.BadgeProps{
					Label:  memoryinspector.MaturityLabel(props.Translator, row.Maturity),
					Status: memoryinspector.MaturityTone(row.Maturity), Mode: mode,
				}),
			),
			html.Span(html.Props{
				Class: css.New(
					css.Font(css.FontStack(tokens.Fonts.Reading)),
					css.FontSize(css.Px(tokens.Typography.Body.Size)),
					css.LineHeightLen(css.Px(tokens.Typography.Body.LineHeight)),
					css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
				).String(),
				Text: row.Summary,
			}),
			html.Span(html.Props{
				Class: css.New(
					u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.XS)), css.FlexWrap.Wrap,
					css.Font(css.FontStack(tokens.Fonts.Code)),
					css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
					css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
				).String(),
				Text: memoryRowMetaLine(row),
			}),
		),
	)
}

func memoryPressedValue(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

// memoryRowMetaLine states the facts that decide whether to trust a row:
// which revision of the claim this is, whether a person corrected it, what it
// is scoped to, and when it was recorded.
func memoryRowMetaLine(row MemoryArtifactRow) string {
	parts := []string{"revision " + strconv.FormatUint(row.RevisionNumber, 10)}
	if row.CreatedFromCorrection {
		parts = append(parts, "user-corrected")
	}
	if scope := strings.TrimSpace(row.ScopeRepository); scope != "" {
		parts = append(parts, scope)
	} else {
		parts = append(parts, "project-wide")
	}
	if !row.CreatedAt.IsZero() {
		parts = append(parts, row.CreatedAt.UTC().Format("2006-01-02 15:04Z"))
	}
	return strings.Join(parts, " · ")
}

func memoryDetailRegion(props MemoryWorkspaceProps, mode primitives.Mode) ui.Node {
	tokens := props.Tokens
	return html.Aside(html.Props{
		Aria: map[string]string{"label": "Selected memory artifact"},
		Data: map[string]string{"component": "memory-detail", "state": string(surfaceStateOrUnavailable(props.DetailState))},
		Class: css.New(
			u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.MD)),
			css.MinWidth(css.Zero), css.MinHeight(css.Zero),
			css.H(css.Full), css.OverflowY.Auto,
			css.Padding(css.RawLength("0 0 0 20px")),
			css.BorderLeft(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
		).String(),
	}, memoryDetailContent(props, mode)...)
}

func memoryDetailContent(props MemoryWorkspaceProps, mode primitives.Mode) []ui.Node {
	tokens := props.Tokens
	switch surfaceStateOrUnavailable(props.DetailState) {
	case SurfaceLoading:
		return []ui.Node{
			markedEyebrow(primitives.IconMemory, "Selected artifact", tokens),
			primitives.InlineAlert(primitives.InlineAlertProps{
				Title: "Reading artifact", Message: "Loading this artifact's content and provenance.",
				Tone: design.StatusNeutral, Mode: mode,
			}),
		}
	case SurfaceFailed:
		return []ui.Node{
			markedEyebrow(primitives.IconMemory, "Selected artifact", tokens),
			primitives.InlineAlert(primitives.InlineAlertProps{
				Title: "Artifact unavailable", Message: memoryFailureBody(props.DetailError),
				Tone: design.StatusFailure, Mode: mode,
			}),
		}
	}
	if props.Detail == nil {
		return []ui.Node{
			markedEyebrow(primitives.IconMemory, "Selected artifact", tokens),
			html.P(html.Props{
				Class: memoryProseClass(tokens),
				Text:  "Choose an artifact to read what it claims and where that claim came from.",
			}),
		}
	}
	detail := *props.Detail
	children := []ui.Node{
		markedEyebrow(primitives.IconMemory, "Selected artifact", tokens),
		html.H2(html.Props{
			Class: css.New(
				css.Margin(css.Zero),
				css.Font(css.FontStack(tokens.Fonts.Reading)),
				css.FontSize(css.Px(tokens.Typography.SectionTitle.Size)),
				css.LineHeightLen(css.Px(tokens.Typography.SectionTitle.LineHeight)),
				css.FontWeight.Normal,
				css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
			).String(),
			Text: detail.Row.Summary,
		}),
		html.Div(html.Props{Class: css.New(u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.XS)), css.FlexWrap.Wrap).String()},
			primitives.IconChip(primitives.IconChipProps{
				Label: memoryinspector.KindLabel(props.Translator, detail.Row.Kind),
				Kind:  memoryinspector.DesignKindFor(detail.Row.Kind), Mode: mode,
			}),
			primitives.Badge(primitives.BadgeProps{
				Label:  memoryinspector.MaturityLabel(props.Translator, detail.Row.Maturity),
				Status: memoryinspector.MaturityTone(detail.Row.Maturity), Mode: mode,
			}),
		),
	}
	if len(detail.Fields) > 0 {
		rows := make([]MemoryDetailField, 0, len(detail.Fields))
		rows = append(rows, detail.Fields...)
		children = append(children, memoryDetailSection("Content", primitives.IconTranscript, rows, tokens))
	}
	children = append(children, memoryDetailSection("Provenance", primitives.IconTree, memoryProvenanceFields(detail), tokens))
	children = append(children, memoryDetailSection("Identity", primitives.IconProof, memoryIdentityFields(detail), tokens))
	children = append(children, html.P(html.Props{
		Class: memoryProseClass(tokens),
		Text:  "Memory is written by the coordinator from evidence it observed. Correcting, quarantining, or deleting an artifact is not available from this console yet.",
	}))
	return children
}

// memoryProvenanceFields answers "why does the agent believe this?". An
// unresolved origin reports that it is unresolved rather than reading as "none",
// because those are different claims and only one of them is true.
func memoryProvenanceFields(detail MemoryArtifactDetail) []MemoryDetailField {
	fields := make([]MemoryDetailField, 0, 5)
	if detail.OriginKnown {
		origin := detail.OriginArtifactID
		if strings.TrimSpace(origin) == "" {
			origin = "This artifact is its own origin"
		}
		fields = append(fields, MemoryDetailField{Label: "Origin", Value: origin})
	} else {
		fields = append(fields, MemoryDetailField{
			Label: "Origin", Value: memoryUnknownText("Not yet determined", detail.OriginUnknownReason),
		})
	}
	if len(detail.DerivedFrom) > 0 {
		fields = append(fields, MemoryDetailField{Label: "Derived from", Value: strings.Join(detail.DerivedFrom, "\n")})
	}
	if len(detail.InfluencedBy) > 0 {
		fields = append(fields, MemoryDetailField{Label: "Influenced by", Value: strings.Join(detail.InfluencedBy, "\n")})
	}
	if detail.SupportingEpisodesKnown {
		value := "None recorded"
		if len(detail.SupportingEpisodes) > 0 {
			value = strings.Join(detail.SupportingEpisodes, "\n")
		}
		fields = append(fields, MemoryDetailField{Label: "Supporting episodes", Value: value})
	} else {
		fields = append(fields, MemoryDetailField{
			Label: "Supporting episodes",
			Value: memoryUnknownText("Not yet determined", detail.SupportingEpisodesUnknownReason),
		})
	}
	if binding := strings.TrimSpace(detail.BindingState); binding != "" {
		fields = append(fields, MemoryDetailField{Label: "Revision binding", Value: binding})
	}
	return fields
}

func memoryUnknownText(headline, reason string) string {
	if strings.TrimSpace(reason) == "" {
		return headline
	}
	return headline + ": " + reason
}

func memoryIdentityFields(detail MemoryArtifactDetail) []MemoryDetailField {
	fields := []MemoryDetailField{
		{Label: "Artifact", Value: detail.Row.ArtifactID},
		{Label: "Revision", Value: detail.Row.RevisionID},
		{Label: "Revision number", Value: strconv.FormatUint(detail.Row.RevisionNumber, 10)},
	}
	if detail.SupersedesRevisionID != "" {
		fields = append(fields, MemoryDetailField{Label: "Supersedes", Value: detail.SupersedesRevisionID})
	}
	if detail.Row.CreatedFromCorrection {
		fields = append(fields, MemoryDetailField{Label: "Written by", Value: "A person's correction"})
	}
	if scope := strings.TrimSpace(detail.Row.ScopeRepository); scope != "" {
		fields = append(fields, MemoryDetailField{Label: "Scoped to", Value: scope})
	} else {
		fields = append(fields, MemoryDetailField{Label: "Scoped to", Value: "Every repository in this project"})
	}
	if !detail.Row.CreatedAt.IsZero() {
		fields = append(fields, MemoryDetailField{
			Label: "Recorded", Value: detail.Row.CreatedAt.UTC().Format("2006-01-02 15:04Z"),
		})
	}
	if digest := strings.TrimSpace(detail.ContentDigest); digest != "" {
		fields = append(fields, MemoryDetailField{Label: "Content digest", Value: digest})
	}
	return fields
}

func memoryDetailSection(title string, icon primitives.IconName, fields []MemoryDetailField, tokens design.Tokens) ui.Node {
	if len(fields) == 0 {
		return nil
	}
	children := []ui.Node{markedEyebrow(icon, title, tokens)}
	rows := make([]ui.Node, 0, len(fields)*2)
	for _, field := range fields {
		rows = append(rows,
			html.Tag("dt", html.Props{
				Class: css.New(
					css.Font(css.FontStack(tokens.Fonts.UI)),
					css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
					css.LineHeightLen(css.Px(tokens.Typography.Metadata.LineHeight)),
					css.FontWeight.Semibold,
					css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
				).String(),
				Text: field.Label,
			}),
			html.Tag("dd", html.Props{
				Class: css.New(
					css.Margin(css.Zero),
					css.Font(css.FontStack(tokens.Fonts.Code)),
					css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
					css.LineHeightLen(css.Px(tokens.Typography.Body.LineHeight)),
					css.TextColor(css.Hex(string(tokens.Colors.TextSecondary))),
					css.WhiteSpace.PreWrap,
					css.OverflowWrap.Anywhere,
				).String(),
				Text: field.Value,
			}),
		)
	}
	children = append(children, html.Tag("dl", html.Props{
		Class: css.New(
			u.Grid,
			css.GridCols(css.TrackLen(css.Px(116)), css.MinMax(css.TrackLen(css.Zero), css.Fr(1))),
			css.Gap(css.RawLength("4px 12px")),
			css.Margin(css.Zero),
		).String(),
	}, rows...))
	return html.Section(html.Props{
		Aria:  map[string]string{"label": title},
		Data:  map[string]string{"component": "memory-detail-section", "section": strings.ToLower(title)},
		Class: css.New(u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.XS))).String(),
	}, children...)
}

func memoryProseClass(tokens design.Tokens) string {
	return css.New(
		css.Margin(css.Zero),
		css.Font(css.FontStack(tokens.Fonts.Reading)),
		css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
		css.LineHeightLen(css.Px(tokens.Typography.Body.LineHeight)),
		css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
	).String()
}
