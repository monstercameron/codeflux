package diffreview

import (
	"fmt"
	"strconv"
	"strings"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/design"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/css/u"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// DiffReview renders the complete bounded, read-only diff review surface.
func DiffReview(props Props) ui.Node {
	if err := props.Validate(); err != nil {
		return primitives.InlineAlert(primitives.InlineAlertProps{
			Title: "Diff review unavailable", Message: err.Error(),
			Tone: design.StatusFailure, Mode: props.Mode,
		})
	}
	tokens := props.Mode.Tokens()
	filtered := FilterFiles(props.Files, props.CategoryFilters)
	return html.Section(html.Props{
		Aria: map[string]string{"label": "Task diff review"},
		Data: map[string]string{
			"component": "diff-review", "diff-identity": props.DiffIdentity,
			"base-revision": props.BaseRevision, "head-revision": props.HeadRevision,
			"file-count": strconv.Itoa(len(props.Files)), "filtered-count": strconv.Itoa(len(filtered)),
			"read-only": "true", "bounded": "true",
		},
		Class: reviewClass(tokens),
	},
		reviewHeader(props),
		warningsSection(props),
		filterBar(props),
		changedFileList(props, filtered),
		selectedFilePanel(props),
	)
}

func reviewHeader(props Props) ui.Node {
	tokens := props.Mode.Tokens()
	added, deleted, known := TotalLineCounts(props.Files)
	totals := fmt.Sprintf("+%d -%d", added, deleted)
	if !known {
		totals += " — some files have unknown line counts"
	}
	return html.Header(html.Props{Class: headerClass(tokens)},
		html.Div(html.Props{Class: titleClass(tokens)},
			html.P(html.Props{Class: eyebrowClass(tokens), Text: "Task diff review"}),
			html.H2(html.Props{Class: headingClass(tokens), Text: fmt.Sprintf("%d changed file(s)", len(props.Files))}),
		),
		definitionList(tokens,
			definition{"Diff identity", knownText(props.DiffIdentity, "no diff identity supplied"), "diff-identity"},
			definition{"Base revision", shortRevision(props.BaseRevision), "base-revision"},
			definition{"Head revision", shortRevision(props.HeadRevision), "head-revision"},
			definition{"Line totals", totals, "line-totals"},
		),
	)
}

func warningsSection(props Props) ui.Node {
	outOfScope := OutOfScopeFiles(props.Files)
	churn := FormattingChurnFiles(props.Files)
	binaryOrGenerated := BinaryOrGeneratedFiles(props.Files)
	total := len(outOfScope) + len(churn) + len(binaryOrGenerated)
	if total == 0 {
		return html.Div(html.Props{Hidden: true, Data: map[string]string{"component": "diff-review-warnings", "state": "none"}})
	}
	children := make([]ui.Node, 0, 3)
	if len(outOfScope) > 0 {
		children = append(children, warningAlert(props.Mode, "out-of-plan-scope", "Out of plan scope",
			fmt.Sprintf("%d file(s) are flagged outside the proposed plan scope", len(outOfScope)), outOfScope))
	}
	if len(churn) > 0 {
		children = append(children, warningAlert(props.Mode, "formatting-churn", "Broad formatting churn",
			fmt.Sprintf("%d file(s) show broad formatting churn", len(churn)), churn))
	}
	if len(binaryOrGenerated) > 0 {
		children = append(children, warningAlert(props.Mode, "binary-or-generated", "Binary or generated changes",
			fmt.Sprintf("%d file(s) are binary or generated content", len(binaryOrGenerated)), binaryOrGenerated))
	}
	return html.Section(html.Props{
		Aria:  map[string]string{"label": "Review warnings"},
		Data:  map[string]string{"component": "diff-review-warnings", "state": "present", "count": strconv.Itoa(total)},
		Class: css.New(u.Flex, u.FlexCol, css.Gap(css.Px(props.Mode.Tokens().Spacing.SM))).String(),
	}, children...)
}

func warningAlert(mode primitives.Mode, kind, title, message string, files []ChangedFile) ui.Node {
	paths := make([]string, len(files))
	for index, file := range files {
		paths[index] = file.Path.String()
	}
	return html.Div(html.Props{
		Data: map[string]string{"warning": kind, "count": strconv.Itoa(len(files))},
	},
		primitives.InlineAlert(primitives.InlineAlertProps{
			Title: title, Message: message + ": " + strings.Join(paths, ", "),
			Tone: design.StatusWarning, Mode: mode,
		}),
	)
}

func filterBar(props Props) ui.Node {
	tokens := props.Mode.Tokens()
	children := make([]ui.Node, 0, len(props.CategoryFilters))
	for _, filter := range props.CategoryFilters {
		children = append(children, ui.CreateElement(categoryFilterToggle, categoryFilterToggleProps{
			Review: props,
			Filter: filter,
		}))
	}
	return html.Div(html.Props{
		Role: "group", Aria: map[string]string{"label": "Change category filters"},
		Data:  map[string]string{"component": "diff-review-filters"},
		Class: css.New(u.Flex, css.FlexWrap.Wrap, css.Gap(css.Px(tokens.Spacing.XS))).String(),
	}, children...)
}

type categoryFilterToggleProps struct {
	Review Props
	Filter CategoryFilter
}

func categoryFilterToggle(props categoryFilterToggleProps) ui.Node {
	filter := props.Filter
	stableToggle := ui.UseEvent(func() {
		ToggleCategory(props.Review, filter.Category)
	})
	return reviewToggleButton(reviewToggleButtonProps{
		ID:              "diff-review-filter-" + string(filter.Category),
		Label:           humanizeCategory(filter.Category),
		AccessibleLabel: "Filter " + humanizeCategory(filter.Category) + " files",
		Pressed:         filter.Active,
		Disabled:        props.Review.OnToggleCategory == nil,
		Mode:            props.Review.Mode,
		OnClick:         stableToggle,
	})
}

func changedFileList(props Props, filtered []ChangedFile) ui.Node {
	tokens := props.Mode.Tokens()
	rowHeight := float64(tokens.Interaction.MinimumPointerTarget)
	if rowHeight < 72 {
		rowHeight = 72
	}
	onSelect := func(path string) { SelectFile(props, path) }
	return primitives.VirtualList(primitives.VirtualListProps[ChangedFile]{
		ID: "diff-review-files", Label: "Changed files", Items: filtered,
		Height: 420, RowHeight: rowHeight, Mode: props.Mode, ActiveKey: props.SelectedPath,
		ItemKey:        func(file ChangedFile) string { return file.Key() },
		ItemLabel:      fileAccessibleLabel,
		RenderItem:     func(item primitives.VirtualListItemProps[ChangedFile]) ui.Node { return fileRow(props.Mode, item) },
		OnActiveChange: onSelect, OnActivate: onSelect,
		EmptyTitle: "No changed files match the selected filters",
		EmptyBody:  "Clear a category filter to see more changed files.",
	})
}

func fileAccessibleLabel(file ChangedFile) string {
	label := humanizeStatus(file.Status) + " " + file.Path.String()
	if file.Status == FileChangeStatusRenamed {
		label = "Renamed from " + file.PreviousPath.String() + " to " + file.Path.String()
	}
	return label + ", " + humanizeCategory(file.Category) + ", " + lineCountsText(file.Lines)
}

func fileRow(mode primitives.Mode, item primitives.VirtualListItemProps[ChangedFile]) ui.Node {
	file := item.Item
	tokens := mode.Tokens()
	badges := []ui.Node{
		primitives.Badge(primitives.BadgeProps{Label: humanizeStatus(file.Status), Status: fileStatusTone(file.Status), Mode: mode}),
		primitives.Badge(primitives.BadgeProps{Label: humanizeCategory(file.Category), Status: design.StatusNeutral, Mode: mode}),
	}
	if file.Binary {
		badges = append(badges, primitives.Badge(primitives.BadgeProps{Label: "Binary", Status: design.StatusWarning, Mode: mode}))
	}
	if file.Category == FileCategoryGenerated {
		badges = append(badges, primitives.Badge(primitives.BadgeProps{Label: "Generated", Status: design.StatusWarning, Mode: mode}))
	}
	if file.FormattingChurn {
		badges = append(badges, primitives.Badge(primitives.BadgeProps{Label: "Formatting churn", Status: design.StatusWarning, Mode: mode}))
	}
	if file.Scope.OutOfScope() {
		badges = append(badges, primitives.Badge(primitives.BadgeProps{Label: "Out of plan scope", Status: design.StatusWarning, Mode: mode}))
	}
	label := file.Path.String()
	if file.Status == FileChangeStatusRenamed {
		label = file.PreviousPath.String() + " -> " + file.Path.String()
	}
	return html.Div(html.Props{
		Data: map[string]string{
			"component": "diff-review-file-row", "path": file.Path.String(), "status": string(file.Status),
			"category": string(file.Category), "binary": strconv.FormatBool(file.Binary),
			"formatting-churn":  strconv.FormatBool(file.FormattingChurn),
			"out-of-plan-scope": strconv.FormatBool(file.Scope.OutOfScope()),
			"scope-known":       strconv.FormatBool(file.Scope.Known),
			"line-counts-known": strconv.FormatBool(file.Lines.Known),
			"selected":          strconv.FormatBool(item.Active),
		},
		Class: fileRowClass(tokens, item.Active),
	},
		primitives.TechnicalLabel(primitives.TechnicalLabelProps{FullLabel: label, VisibleLabel: label, Mode: mode}),
		html.Div(html.Props{Class: css.New(u.Flex, css.FlexWrap.Wrap, css.Gap(css.Px(tokens.Spacing.XS))).String()}, badges...),
		html.Span(html.Props{
			Data: map[string]string{"field": "line-counts"}, Class: secondaryTextClass(tokens), Text: lineCountsText(file.Lines),
		}),
		html.Span(html.Props{
			Data: map[string]string{"field": "scope"}, Class: secondaryTextClass(tokens), Text: scopeText(file.Scope),
		}),
	)
}

func selectedFilePanel(props Props) ui.Node {
	file, ok := FindChangedFile(props.Files, props.SelectedPath)
	if !ok {
		return primitives.EmptyState(primitives.EmptyStateProps{
			Title: "No file selected", Body: "Select a changed file to view its unified diff.", Mode: props.Mode,
		})
	}
	tokens := props.Mode.Tokens()
	children := []ui.Node{selectedFileHeader(props, file)}
	switch {
	case file.Binary:
		children = append(children, primitives.InlineAlert(primitives.InlineAlertProps{
			Title: "Binary file", Message: "Binary content changed; a unified diff is not shown.",
			Tone: design.StatusNeutral, Mode: props.Mode,
		}))
	case len(file.Hunks) == 0:
		body := "This file has no textual hunks to display."
		if file.DiffUnavailableReason != "" {
			body = file.DiffUnavailableReason
		}
		children = append(children, primitives.EmptyState(primitives.EmptyStateProps{Title: "No diff hunks", Body: body, Mode: props.Mode}))
	default:
		children = append(children, diffHunkList(props, file))
	}
	return html.Section(html.Props{
		Aria:  map[string]string{"label": "Selected file diff"},
		Data:  map[string]string{"component": "diff-review-selected-file", "path": file.Path.String()},
		Class: css.New(u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.MD))).String(),
	}, children...)
}

func selectedFileHeader(props Props, file ChangedFile) ui.Node {
	tokens := props.Mode.Tokens()
	whitespaceDisabled := props.OnToggleWhitespace == nil || file.Binary || len(file.Hunks) == 0
	line := firstReviewLine(file)
	return html.Header(html.Props{Class: headerClass(tokens)},
		html.H3(html.Props{Class: headingClass(tokens), Text: file.Path.String()}),
		html.Div(html.Props{Class: css.New(u.Flex, css.FlexWrap.Wrap, css.Gap(css.Px(tokens.Spacing.XS))).String()},
			primitives.Button(primitives.ButtonProps{
				ID: "diff-review-open-editor", Label: "Open in editor", Mode: props.Mode,
				Disabled: props.OnOpenInEditor == nil,
				OnClick: func() {
					if props.OnOpenInEditor != nil {
						props.OnOpenInEditor(file.Path.String(), line)
					}
				},
			}),
			ui.CreateElement(whitespaceToggle, whitespaceToggleProps{Review: props, Disabled: whitespaceDisabled}),
		),
	)
}

func firstReviewLine(file ChangedFile) uint32 {
	for _, hunk := range file.Hunks {
		for _, line := range hunk.Lines {
			if line.NewLineNumberKnown && line.NewLineNumber <= uint64(^uint32(0)) {
				return uint32(line.NewLineNumber)
			}
			if line.OldLineNumberKnown && line.OldLineNumber <= uint64(^uint32(0)) {
				return uint32(line.OldLineNumber)
			}
		}
	}
	return 1
}

type whitespaceToggleProps struct {
	Review   Props
	Disabled bool
}

func whitespaceToggle(props whitespaceToggleProps) ui.Node {
	stableToggle := ui.UseEvent(func() { ToggleWhitespace(props.Review) })
	return reviewToggleButton(reviewToggleButtonProps{
		ID:              "diff-review-whitespace-toggle",
		Label:           "Show whitespace",
		AccessibleLabel: "Show whitespace characters in the diff",
		Pressed:         props.Review.WhitespaceVisible,
		Disabled:        props.Disabled,
		Mode:            props.Review.Mode,
		OnClick:         stableToggle,
	})
}

type reviewToggleButtonProps struct {
	ID              string
	Label           string
	AccessibleLabel string
	Pressed         bool
	Disabled        bool
	Mode            primitives.Mode
	OnClick         ui.Handler
}

// reviewToggleButton keeps aria-pressed on the interactive element while
// accepting the stable handler created by the hook-owning child component.
func reviewToggleButton(props reviewToggleButtonProps) ui.Node {
	htmlProps := html.Props{
		ID:       props.ID,
		Type:     "button",
		Disabled: props.Disabled,
		OnClick:  props.OnClick,
		Class:    reviewToggleClass(props.Mode.Tokens(), props.Pressed),
		Aria: map[string]string{
			"label":   props.AccessibleLabel,
			"pressed": strconv.FormatBool(props.Pressed),
		},
		Data: map[string]string{
			"component": "diff-review-toggle",
			"state":     map[bool]string{true: "disabled", false: "ready"}[props.Disabled],
		},
	}
	return html.Button(htmlProps, html.Text(props.Label))
}

func diffHunkList(props Props, file ChangedFile) ui.Node {
	tokens := props.Mode.Tokens()
	rows := FlattenHunks(file.Hunks)
	onActive := props.OnActiveHunkRowChange
	if onActive == nil {
		onActive = func(string) {}
	}
	rowHeight := float64(tokens.Interaction.MinimumPointerTarget)
	if rowHeight < 44 {
		rowHeight = 44
	}
	return primitives.VirtualList(primitives.VirtualListProps[HunkRow]{
		ID: "diff-review-hunks-" + sanitizeDOMID(file.Path.String()), Label: "Unified diff for " + file.Path.String(),
		Items: rows, Height: 480, RowHeight: rowHeight, Mode: props.Mode, ActiveKey: props.ActiveHunkRowKey,
		ItemKey:        func(row HunkRow) string { return row.Key },
		ItemLabel:      hunkRowLabel,
		RenderItem:     func(item primitives.VirtualListItemProps[HunkRow]) ui.Node { return hunkRowView(props, item) },
		OnActiveChange: onActive,
		EmptyTitle:     "No diff lines",
	})
}

func hunkRowLabel(row HunkRow) string {
	if row.Kind == HunkRowHeader {
		return "Hunk " + row.Hunk.Header
	}
	kind := map[DiffLineKind]string{
		DiffLineContext: "context line", DiffLineAddition: "added line", DiffLineDeletion: "removed line",
	}[row.Line.Kind]
	return kind + ": " + row.Line.Text
}

func hunkRowView(props Props, item primitives.VirtualListItemProps[HunkRow]) ui.Node {
	if item.Item.Kind == HunkRowHeader {
		return hunkHeaderView(props, item.Item.Hunk)
	}
	return hunkLineView(props, item.Item.Line)
}

func hunkHeaderView(props Props, hunk DiffHunk) ui.Node {
	tokens := props.Mode.Tokens()
	badges := []ui.Node{
		primitives.Badge(primitives.BadgeProps{
			Label: fmt.Sprintf("%d plan step(s)", len(hunk.PlanSteps)), Status: design.StatusPlan, Mode: props.Mode,
		}),
		primitives.Badge(primitives.BadgeProps{
			Label: fmt.Sprintf("%d tool event(s)", len(hunk.ToolEventIDs)), Status: design.StatusNeutral, Mode: props.Mode,
		}),
	}
	worstValidation := design.StatusNeutral
	for _, link := range hunk.Validations {
		worstValidation = worseValidationTone(worstValidation, validationTone(link.State))
	}
	badges = append(badges, primitives.Badge(primitives.BadgeProps{
		Label: fmt.Sprintf("%d validation link(s)", len(hunk.Validations)), Status: worstValidation, Mode: props.Mode,
	}))
	planStepItems := make([]ui.Node, 0, len(hunk.PlanSteps))
	for _, step := range hunk.PlanSteps {
		step := step
		planStepItems = append(planStepItems, html.Li(html.Props{Class: secondaryTextClass(tokens)},
			primitives.Button(primitives.ButtonProps{
				Label:           fmt.Sprintf("Plan revision %d · step %s", step.PlanRevision, step.StepID),
				AccessibleLabel: "Open " + step.StepID + " in the timeline", Mode: props.Mode,
				Disabled: props.OnOpenPlanStep == nil,
				OnClick: func() {
					if props.OnOpenPlanStep != nil {
						props.OnOpenPlanStep(step)
					}
				},
			}),
		))
	}
	eventItems := make([]ui.Node, 0, len(hunk.ToolEventIDs))
	for _, eventID := range hunk.ToolEventIDs {
		eventID := eventID
		eventItems = append(eventItems, html.Li(html.Props{Class: identityTextClass(tokens)},
			primitives.Button(primitives.ButtonProps{
				Label: eventID.String(), AccessibleLabel: "Open tool event " + eventID.String(), Mode: props.Mode,
				Disabled: props.OnOpenToolEvent == nil,
				OnClick: func() {
					if props.OnOpenToolEvent != nil {
						props.OnOpenToolEvent(eventID)
					}
				},
			}),
		))
	}
	validationItems := make([]ui.Node, 0, len(hunk.Validations))
	for _, link := range hunk.Validations {
		link := link
		validationItems = append(validationItems, html.Li(html.Props{
			Data: map[string]string{"validation-id": link.ID.String(), "validation-state": string(link.State)},
		},
			primitives.Button(primitives.ButtonProps{
				Label: link.Label, AccessibleLabel: "Open validation " + link.Label, Mode: props.Mode,
				Disabled: props.OnOpenValidation == nil,
				OnClick: func() {
					if props.OnOpenValidation != nil {
						props.OnOpenValidation(link.ID)
					}
				},
			}),
			html.Span(html.Props{Class: secondaryTextClass(tokens), Text: " — " + humanizeValue(string(link.State)) + " — " + knownText(link.Summary, "summary not supplied")}),
		))
	}
	return html.Div(html.Props{
		Data: map[string]string{
			"component": "diff-review-hunk-header", "hunk-id": hunk.ID,
			"plan-step-count": strconv.Itoa(len(hunk.PlanSteps)), "tool-event-count": strconv.Itoa(len(hunk.ToolEventIDs)),
			"validation-count": strconv.Itoa(len(hunk.Validations)),
		},
		Class: hunkHeaderClass(tokens),
	},
		html.Strong(html.Props{Class: identityTextClass(tokens), Text: hunk.Header}),
		html.Div(html.Props{Class: css.New(u.Flex, css.FlexWrap.Wrap, css.Gap(css.Px(tokens.Spacing.XS))).String()}, badges...),
		html.Tag("ul", html.Props{Data: map[string]string{"hunk-links": "plan-steps"}, Class: listClass(tokens)}, planStepItems...),
		html.Tag("ul", html.Props{Data: map[string]string{"hunk-links": "tool-events"}, Class: listClass(tokens)}, eventItems...),
		html.Tag("ul", html.Props{Data: map[string]string{"hunk-links": "validations"}, Class: listClass(tokens)}, validationItems...),
	)
}

func hunkLineView(props Props, line DiffLine) ui.Node {
	tokens := props.Mode.Tokens()
	prefix := map[DiffLineKind]string{DiffLineContext: " ", DiffLineAddition: "+", DiffLineDeletion: "-"}[line.Kind]
	text := RenderLineText(line.Text, props.WhitespaceVisible)
	oldNumber := "·"
	if line.OldLineNumberKnown {
		oldNumber = strconv.FormatUint(line.OldLineNumber, 10)
	}
	newNumber := "·"
	if line.NewLineNumberKnown {
		newNumber = strconv.FormatUint(line.NewLineNumber, 10)
	}
	return html.Div(html.Props{
		Data: map[string]string{
			"component": "diff-review-line", "kind": string(line.Kind),
			"old-line-known": strconv.FormatBool(line.OldLineNumberKnown), "new-line-known": strconv.FormatBool(line.NewLineNumberKnown),
		},
		Class: lineClass(tokens, line.Kind),
	},
		html.Span(html.Props{Class: lineGutterClass(tokens), Text: oldNumber}),
		html.Span(html.Props{Class: lineGutterClass(tokens), Text: newNumber}),
		html.Span(html.Props{Aria: map[string]string{"hidden": "true"}, Class: identityTextClass(tokens), Text: prefix}),
		html.Span(html.Props{Class: identityTextClass(tokens), Text: text}),
	)
}

func lineCountsText(counts LineCounts) string {
	if !counts.Known {
		return knownText("", counts.UnknownReason)
	}
	return fmt.Sprintf("+%d -%d", counts.Added, counts.Deleted)
}

func scopeText(scope ScopeAssessment) string {
	if !scope.Known {
		return knownText("", scope.UnknownReason)
	}
	if scope.InScope {
		return "In plan scope"
	}
	return "Out of plan scope"
}

func knownText(value, unknownReason string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	if unknownReason = strings.TrimSpace(unknownReason); unknownReason != "" {
		return "Unknown — " + unknownReason + "."
	}
	return "Unknown"
}

func shortRevision(value string) string {
	if len(value) <= 12 {
		return value
	}
	return value[:12] + "…"
}

func humanizeStatus(status FileChangeStatus) string {
	return humanizeValue(string(status))
}

func humanizeCategory(category FileCategory) string {
	return humanizeValue(string(category))
}

func humanizeValue(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), "-", " ")
	if value == "" {
		return "Unknown"
	}
	return strings.ToUpper(value[:1]) + value[1:]
}

func fileStatusTone(status FileChangeStatus) design.Status {
	switch status {
	case FileChangeStatusAdded:
		return design.StatusSuccess
	case FileChangeStatusDeleted:
		return design.StatusFailure
	case FileChangeStatusModified:
		return design.StatusActive
	case FileChangeStatusRenamed:
		return design.StatusNeutral
	default:
		return design.StatusPending
	}
}

func validationTone(state domain.ValidationState) design.Status {
	switch state {
	case domain.ValidationStateRunning:
		return design.StatusActive
	case domain.ValidationStatePassed:
		return design.StatusSuccess
	case domain.ValidationStateFailed:
		return design.StatusFailure
	case domain.ValidationStateWaived, domain.ValidationStateSkipped:
		return design.StatusWarning
	case domain.ValidationStateInvalidated:
		return design.StatusInvalidated
	default:
		return design.StatusPending
	}
}

func worseValidationTone(current, candidate design.Status) design.Status {
	rank := map[design.Status]int{
		design.StatusNeutral: 0, design.StatusPending: 1, design.StatusActive: 1,
		design.StatusSuccess: 1, design.StatusWarning: 2, design.StatusInvalidated: 3, design.StatusFailure: 4,
	}
	if rank[candidate] > rank[current] {
		return candidate
	}
	return current
}

func sanitizeDOMID(value string) string {
	replacer := strings.NewReplacer("/", "-", " ", "-", ".", "-")
	return replacer.Replace(value)
}

type definition struct{ label, value, kind string }

func definitionList(tokens design.Tokens, definitions ...definition) ui.Node {
	children := make([]ui.Node, 0, len(definitions)*2)
	for _, item := range definitions {
		children = append(children,
			html.Tag("dt", html.Props{Class: definitionLabelClass(tokens), Text: item.label}),
			html.Tag("dd", html.Props{
				Data: map[string]string{"diff-review-field": item.kind}, Class: definitionValueClass(tokens),
				Text: knownText(item.value, "not supplied"),
			}),
		)
	}
	return html.Tag("dl", html.Props{Class: definitionGridClass(tokens)}, children...)
}

func reviewClass(tokens design.Tokens) string {
	return css.New(
		u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Rhythm.PanelGap)),
		css.MaxWidth(css.Percent(100)), css.Padding(css.Px(tokens.Rhythm.PanelInset)),
		css.Bg(css.Hex(string(tokens.Colors.Surface1))),
		css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
		css.Border(css.Px(tokens.Geometry.BorderWidth), css.Hex(string(tokens.Colors.BorderStrong))),
		css.Rounded(css.Px(tokens.Geometry.PanelRadius)), css.Font(css.FontStack(tokens.Fonts.UI)),
	).String()
}

func headerClass(tokens design.Tokens) string {
	return css.New(u.Flex, u.ItemsCenter, u.JustifyBetween, css.FlexWrap.Wrap, css.Gap(css.Px(tokens.Spacing.MD))).String()
}

func reviewToggleClass(tokens design.Tokens, pressed bool) string {
	background := tokens.Colors.Surface2
	foreground := tokens.Colors.TextPrimary
	border := tokens.Colors.BorderSubtle
	if pressed {
		background = tokens.Colors.Accent
		foreground = tokens.Colors.OnAccent
		border = tokens.Colors.Accent
	}
	rules := []css.Rule{
		u.InlineFlex, u.ItemsCenter, u.JustifyCenter,
		css.MinHeight(css.Px(tokens.Interaction.MinimumPointerTarget)),
		css.MinWidth(css.Px(tokens.Interaction.MinimumPointerTarget)),
		css.PaddingX(css.Px(tokens.Spacing.MD)),
		css.Bg(css.Hex(string(background))),
		css.TextColor(css.Hex(string(foreground))),
		css.Border(css.Px(tokens.Geometry.BorderWidth), css.Hex(string(border))),
		css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
		css.Font(css.FontStack(tokens.Fonts.UI)),
		css.FontSize(css.Px(tokens.Typography.ControlLabel.Size)),
		css.LineHeightLen(css.Px(tokens.Typography.ControlLabel.LineHeight)),
		css.FontWeight.Medium,
		css.Cursor.Pointer,
	}
	rules = append(rules, css.FocusVisible(
		css.Outline(css.Px(tokens.Geometry.FocusRingWidth), css.Hex(string(tokens.Colors.FocusRing))),
		css.OutlineOffset(css.Px(tokens.Geometry.FocusRingOffset)),
	)...)
	rules = append(rules, css.Disabled(css.OpacityNum(css.Num(0.5)), css.Cursor.NotAllowed)...)
	return css.New(rules...).String()
}

func titleClass(tokens design.Tokens) string {
	return css.New(u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.XS)), css.MinWidth(css.Zero)).String()
}

func headingClass(tokens design.Tokens) string {
	return css.New(css.Margin(css.Zero), css.FontSize(css.Px(tokens.Typography.PanelHeading.Size)), css.LineHeightLen(css.Px(tokens.Typography.PanelHeading.LineHeight)), css.FontWeight.Semibold).String()
}

func eyebrowClass(tokens design.Tokens) string {
	return css.New(css.Margin(css.Zero), css.TextColor(css.Hex(string(tokens.Colors.TextMuted))), css.FontSize(css.Px(tokens.Typography.Metadata.Size)), css.LineHeightLen(css.Px(tokens.Typography.Metadata.LineHeight)), css.FontWeight.Semibold).String()
}

func secondaryTextClass(tokens design.Tokens) string {
	return css.New(css.Margin(css.Zero), css.TextColor(css.Hex(string(tokens.Colors.TextSecondary))), css.FontSize(css.Px(tokens.Typography.CompactBody.Size)), css.LineHeightLen(css.Px(tokens.Typography.CompactBody.LineHeight))).String()
}

func identityTextClass(tokens design.Tokens) string {
	return css.New(css.Margin(css.Zero), css.Font(css.FontStack(tokens.Fonts.Code)), css.FontSize(css.Px(tokens.Typography.Code.Size)), css.LineHeightLen(css.Px(tokens.Typography.Code.LineHeight)), css.OverflowWrap.Anywhere).String()
}

func definitionGridClass(tokens design.Tokens) string {
	return css.New(u.Grid, css.GridCols(css.Repeat(2, css.MinMax(css.TrackLen(css.Zero), css.Fr(1)))), css.Gap(css.Px(tokens.Spacing.SM)), css.Margin(css.Zero)).String()
}

func definitionLabelClass(tokens design.Tokens) string {
	return css.New(css.TextColor(css.Hex(string(tokens.Colors.TextMuted))), css.FontSize(css.Px(tokens.Typography.Metadata.Size)), css.LineHeightLen(css.Px(tokens.Typography.Metadata.LineHeight)), css.FontWeight.Semibold).String()
}

func definitionValueClass(tokens design.Tokens) string {
	return css.New(css.Margin(css.Zero), css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))), css.FontSize(css.Px(tokens.Typography.CompactBody.Size)), css.LineHeightLen(css.Px(tokens.Typography.CompactBody.LineHeight)), css.OverflowWrap.Anywhere).String()
}

func listClass(tokens design.Tokens) string {
	return css.New(u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.XS)), css.Margin(css.Zero), css.Padding(css.Px(tokens.Spacing.XS))).String()
}

func fileRowClass(tokens design.Tokens, active bool) string {
	rules := []css.Rule{
		u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.XS)),
		css.Padding(css.Px(tokens.Spacing.SM)), css.MinWidth(css.Zero), css.W(css.Full),
		css.Bg(css.Hex(string(tokens.Colors.Surface2))),
		css.Border(css.Px(tokens.Geometry.BorderWidth), css.Hex(string(tokens.Colors.BorderSubtle))),
		css.Rounded(css.Px(tokens.Geometry.RadiusSmall)),
	}
	if active {
		rules = append(rules, css.Border(css.Px(tokens.Geometry.BorderStrongWidth), css.Hex(string(tokens.Colors.BorderStrong))))
	}
	return css.New(rules...).String()
}

func hunkHeaderClass(tokens design.Tokens) string {
	return css.New(
		u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.XS)), css.Padding(css.Px(tokens.Spacing.SM)),
		css.Bg(css.Hex(string(tokens.Colors.Surface3))), css.Rounded(css.Px(tokens.Geometry.RadiusSmall)),
	).String()
}

func lineClass(tokens design.Tokens, kind DiffLineKind) string {
	background := tokens.Colors.Surface1
	switch kind {
	case DiffLineAddition:
		background = tokens.Colors.Success
	case DiffLineDeletion:
		background = tokens.Colors.Failure
	}
	return css.New(
		u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.XS)), css.Padding(css.Px(tokens.Spacing.XS)),
		css.Bg(css.Hex(string(background))), css.OpacityNum(css.Num(0.92)),
		css.Font(css.FontStack(tokens.Fonts.Code)), css.WhiteSpace.Pre,
	).String()
}

func lineGutterClass(tokens design.Tokens) string {
	return css.New(
		css.MinWidth(css.Px(48)), css.TextAlign.Right, css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
		css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
	).String()
}
