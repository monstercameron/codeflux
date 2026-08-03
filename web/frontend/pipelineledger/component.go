package pipelineledger

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"codeflux.dev/codeflux/internal/pipeline"
	"codeflux.dev/codeflux/web/frontend/design"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/css/u"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// LedgerCard renders one task attempt's pipeline stage ledger: every recorded
// stage's verdict and duration, and, once at least one stage besides the
// audit itself is recorded, the skip-audit headline and its per-stage
// breakdown (PIPE-006a, PIPE-044).
func LedgerCard(props Props) ui.Node {
	tokens := props.Mode.Tokens()
	switch stateOrUnavailable(props.State) {
	case Unavailable:
		return html.Div(html.Props{
			Data: map[string]string{"component": "pipeline-ledger", "state": "unavailable"},
		}, primitives.EmptyState(primitives.EmptyStateProps{
			Title: "No task selected", Mode: props.Mode,
			Body: "Open a task to read the pipeline stages its attempts recorded.",
		}))
	case Loading:
		return html.Div(html.Props{
			Data: map[string]string{"component": "pipeline-ledger", "state": "loading"},
		}, primitives.InlineAlert(primitives.InlineAlertProps{
			Title: "Reading the pipeline ledger", Tone: design.StatusNeutral, Mode: props.Mode,
			Message: "Loading every stage this attempt recorded.",
		}))
	case Failed:
		return html.Div(html.Props{
			Data: map[string]string{"component": "pipeline-ledger", "state": "failed"},
		}, primitives.ErrorState(primitives.ErrorStateProps{
			Title: "Pipeline ledger unavailable", Mode: props.Mode,
			Body: ledgerFailureBody(props.ErrorMessage), ActionLabel: retryLabel(props.OnRetry), OnAction: props.OnRetry,
		}))
	}
	if len(props.Rows) == 0 {
		return html.Div(html.Props{
			Data: map[string]string{"component": "pipeline-ledger", "state": "empty", "attempt": strconv.FormatUint(props.Attempt, 10)},
		}, primitives.EmptyState(primitives.EmptyStateProps{
			Title: "No stages recorded yet", Mode: props.Mode,
			Body: "This attempt has not written a ledger row yet. Stages appear here as the run records each one.",
		}))
	}
	rows := append([]StageRow(nil), props.Rows...)
	sort.Slice(rows, func(first, second int) bool { return rows[first].Number < rows[second].Number })
	summary := ComputeSkipSummary(rows)
	return html.Article(html.Props{
		Aria: map[string]string{"label": "Pipeline stage ledger, attempt " + strconv.FormatUint(props.Attempt, 10)},
		Data: map[string]string{
			"component": "pipeline-ledger", "state": "ready",
			"attempt": strconv.FormatUint(props.Attempt, 10), "stage-count": strconv.Itoa(len(rows)),
		},
		Class: cardClass(tokens),
	},
		ledgerHeader(props, tokens),
		skipHeadline(props.Mode, summary),
		declineBreakdown(props.Mode, summary),
		stageTable(props.Mode, rows),
	)
}

func ledgerFailureBody(message string) string {
	if strings.TrimSpace(message) == "" {
		return "The coordinator did not answer the pipeline ledger read."
	}
	return message
}

func retryLabel(retry func()) string {
	if retry == nil {
		return ""
	}
	return "Try again"
}

func ledgerHeader(props Props, tokens design.Tokens) ui.Node {
	return html.Header(html.Props{Class: css.New(u.Flex, u.FlexCol, css.Gap(css.Px(2))).String()},
		html.P(html.Props{
			Class: css.New(
				css.Margin(css.Zero), css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
				css.FontSize(css.Px(tokens.Typography.Metadata.Size)), css.FontWeight.Semibold,
			).String(),
			Text: "Pipeline ledger",
		}),
		html.H2(html.Props{
			Class: css.New(
				css.Margin(css.Zero), css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
				css.FontSize(css.Px(tokens.Typography.PanelHeading.Size)),
				css.LineHeightLen(css.Px(tokens.Typography.PanelHeading.LineHeight)),
				css.FontWeight.Semibold,
			).String(),
			Text: "Attempt " + strconv.FormatUint(props.Attempt, 10),
		}),
	)
}

// skipHeadline is the PIPE-044 headline: how many stages established
// something, how many declined, and how many were never implemented for this
// run -- kept as three separate counts, never folded into one number, so a
// run that declined honestly cannot be mistaken for one whose checks were
// never built.
func skipHeadline(mode primitives.Mode, summary SkipSummary) ui.Node {
	tokens := mode.Tokens()
	if !summary.Computed {
		return primitives.InlineAlert(primitives.InlineAlertProps{
			Title: "Skip ratio not yet available", Tone: design.StatusNeutral, Mode: mode,
			Message: "This attempt has not recorded enough stages to compute a skip ratio.",
		})
	}
	established := summary.Satisfied
	declined := summary.Skipped
	unbuilt := summary.NotImplemented
	return html.Section(html.Props{
		Aria: map[string]string{"label": "Skip audit"},
		Data: map[string]string{
			"component": "pipeline-skip-headline", "computed": "true",
			"skip-ratio-percent": strconv.Itoa(int(summary.SkipRatio*100 + 0.5)),
			"skipped":            strconv.Itoa(summary.Skipped), "not-implemented": strconv.Itoa(summary.NotImplemented),
		},
		Class: css.New(u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.SM)),
			css.Padding(css.Px(tokens.Spacing.MD)), css.Bg(css.Hex(string(tokens.Colors.Surface2))),
			css.Border(css.Px(tokens.Geometry.BorderWidth), css.Hex(string(tokens.Colors.BorderSubtle))),
			css.Rounded(css.Px(tokens.Geometry.RadiusSmall)),
		).String(),
	},
		html.P(html.Props{
			Class: css.New(css.Margin(css.Zero), css.FontSize(css.Px(tokens.Typography.Body.Size)),
				css.LineHeightLen(css.Px(tokens.Typography.Body.LineHeight)),
				css.TextColor(css.Hex(string(tokens.Colors.TextPrimary)))).String(),
			Text: fmt.Sprintf(
				"%d of %d stage(s) established nothing (%d%% skip ratio).",
				summary.Skipped+summary.NotImplemented, summary.TotalStages, int(summary.SkipRatio*100+0.5),
			),
		}),
		html.Div(html.Props{
			Role:  "list",
			Aria:  map[string]string{"label": "Stage outcomes"},
			Class: css.New(u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.XS)), css.FlexWrap.Wrap).String(),
		},
			headlineStat(mode, "Established", established, design.StatusSuccess),
			headlineStat(mode, "Declined", declined, design.StatusWarning),
			headlineStat(mode, "Not implemented", unbuilt, design.StatusPending),
			headlineStat(mode, "Failed", summary.Failed, design.StatusFailure),
			headlineStat(mode, "Blocked", summary.Blocked, design.StatusBlocked),
		),
	)
}

func headlineStat(mode primitives.Mode, label string, count int, status design.Status) ui.Node {
	return html.Div(html.Props{Role: "listitem"},
		primitives.Badge(primitives.BadgeProps{
			Label: label + ": " + strconv.Itoa(count), Status: status, Mode: mode,
		}),
	)
}

// declineBreakdown lists every stage that established nothing, classified
// into the three PIPE-042 categories, so a reader can reach the breakdown
// behind the headline without leaving this surface. It never merges the
// categories: a principled decline and an unimplemented stage are kept in
// separately labelled groups even when both are present.
//
// The classification is computed from internal/pipeline's own static tables
// (see Classification's doc comment); it cannot show the free-text reason a
// run recorded for a decline, because PipelineStageView deliberately does not
// carry it. Where that matters, the full detail remains in the stage table
// row itself and, for now, only in the ledger the coordinator holds.
func declineBreakdown(mode primitives.Mode, summary SkipSummary) ui.Node {
	if !summary.Computed || len(summary.Declines) == 0 {
		return nil
	}
	tokens := mode.Tokens()
	groups := []struct {
		classification Classification
		status         design.Status
	}{
		{ClassificationPrincipledDecline, design.StatusWarning},
		{ClassificationNoCheckByDesign, design.StatusNeutral},
		{ClassificationUnimplemented, design.StatusPending},
	}
	children := []ui.Node{html.H3(html.Props{
		Class: css.New(css.Margin(css.Zero), css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
			css.FontSize(css.Px(tokens.Typography.SectionTitle.Size)),
			css.LineHeightLen(css.Px(tokens.Typography.SectionTitle.LineHeight)), css.FontWeight.Semibold).String(),
		Text: "Why each stage established nothing",
	})}
	any := false
	for _, group := range groups {
		members := membersOf(summary.Declines, group.classification)
		if len(members) == 0 {
			continue
		}
		any = true
		children = append(children, declineGroup(mode, group.classification, group.status, members))
	}
	if !any {
		return nil
	}
	return html.Section(html.Props{
		Aria:  map[string]string{"label": "Skip audit breakdown"},
		Data:  map[string]string{"component": "pipeline-skip-breakdown", "decline-count": strconv.Itoa(len(summary.Declines))},
		Class: css.New(u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.SM))).String(),
	}, children...)
}

func membersOf(declines []ClassifiedStage, classification Classification) []ClassifiedStage {
	members := make([]ClassifiedStage, 0, len(declines))
	for _, decline := range declines {
		if decline.Classification == classification {
			members = append(members, decline)
		}
	}
	return members
}

func declineGroup(mode primitives.Mode, classification Classification, status design.Status, members []ClassifiedStage) ui.Node {
	tokens := mode.Tokens()
	items := make([]ui.Node, 0, len(members))
	for _, member := range members {
		label := member.Stage.Name
		if member.Ticket != "" {
			label = label + " (" + member.Ticket + ")"
		}
		items = append(items, html.Li(html.Props{
			Data: map[string]string{
				"pipeline-decline-stage": strconv.Itoa(int(member.Stage.Number)),
				"classification":         string(member.Classification),
			},
			Class: css.New(css.FontSize(css.Px(tokens.Typography.CompactBody.Size)),
				css.LineHeightLen(css.Px(tokens.Typography.CompactBody.LineHeight))).String(),
			Text: label,
		}))
	}
	return html.Div(html.Props{
		Data:  map[string]string{"component": "pipeline-decline-group", "classification": string(classification)},
		Class: css.New(u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.XS))).String(),
	},
		primitives.Badge(primitives.BadgeProps{
			Label: classification.Label() + " (" + strconv.Itoa(len(members)) + ")", Status: status, Mode: mode,
		}),
		html.Ul(html.Props{
			Class: css.New(u.Flex, u.FlexCol, css.Gap(css.Px(2)),
				css.Margin(css.RawLength("0 0 0 4px")), css.Padding(css.RawLength("0 0 0 16px"))).String(),
		}, items...),
	)
}

// stageTable is the full recorded flow, in stage order: number, name, state,
// and duration. An unmeasured duration renders as an explicit badge rather
// than a bare "0", so it can never be misread as an instantaneous stage
// (PIPE-006a); the distinction is carried by the badge's icon and text, not
// only its colour.
func stageTable(mode primitives.Mode, rows []StageRow) ui.Node {
	items := make([]ui.Node, 0, len(rows))
	for _, row := range rows {
		items = append(items, stageRow(mode, row))
	}
	return html.Section(html.Props{
		Aria:  map[string]string{"label": "Recorded stages"},
		Data:  map[string]string{"component": "pipeline-stage-table", "row-count": strconv.Itoa(len(rows))},
		Class: css.New(u.Flex, u.FlexCol, css.Gap(css.Px(2))).String(),
	},
		stageTableColumnLabels(mode),
		html.Div(html.Props{Role: "list", Aria: map[string]string{"label": "Recorded stages"}}, items...),
	)
}

// stageTableColumnLabels names each row's four columns once, above the list,
// rather than leaving a reader to infer what the trailing badge and value
// mean from their shape alone.
func stageTableColumnLabels(mode primitives.Mode) ui.Node {
	tokens := mode.Tokens()
	label := func(text string, minWidth int, grow bool) ui.Node {
		rules := []css.Rule{
			css.MinWidth(css.Px(minWidth)),
			css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
			css.TextColor(css.Hex(string(tokens.Colors.TextMuted))), css.FontWeight.Semibold,
		}
		if grow {
			rules = append(rules, css.FlexGrow(css.Num(1)))
		}
		return html.Span(html.Props{Class: css.New(rules...).String()}, html.Text(text))
	}
	return html.Div(html.Props{
		Aria: map[string]string{"hidden": "true"},
		Class: css.New(
			u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.SM)),
			css.Padding(css.RawLength("0 8px 2px")),
		).String(),
	},
		label("#", 28, false),
		label("Stage", 0, true),
		label("State", 96, false),
		label("Elapsed", 96, false),
	)
}

func stageRow(mode primitives.Mode, row StageRow) ui.Node {
	tokens := mode.Tokens()
	return html.Div(html.Props{
		Role: "listitem",
		Data: map[string]string{
			"component": "pipeline-stage-row", "stage-number": strconv.Itoa(int(row.Number)),
			"stage-name": row.Name, "state": string(row.State),
			"elapsed-measured": strconv.FormatBool(row.ElapsedMeasured),
		},
		Class: css.New(
			u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.SM)),
			css.Padding(css.RawLength("6px 8px")),
			css.Border(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
			css.Rounded(css.Px(tokens.Geometry.RadiusSmall)),
			css.MinWidth(css.Zero),
		).String(),
	},
		html.Span(html.Props{
			Class: css.New(
				css.Font(css.FontStack(tokens.Fonts.Code)), css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				css.TextColor(css.Hex(string(tokens.Colors.TextMuted))), css.MinWidth(css.Px(28)),
			).String(),
			Text: fmt.Sprintf("%02d", row.Number),
		}),
		html.Span(html.Props{
			Class: css.New(
				css.FlexGrow(css.Num(1)), css.MinWidth(css.Zero), css.OverflowWrap.Anywhere,
				css.FontSize(css.Px(tokens.Typography.CompactBody.Size)),
				css.LineHeightLen(css.Px(tokens.Typography.CompactBody.LineHeight)),
				css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
			).String(),
			Text: row.Name,
		}),
		primitives.Badge(primitives.BadgeProps{Label: stateLabel(row.State), Status: stateStatus(row.State), Mode: mode}),
		durationCell(mode, row),
	)
}

func durationCell(mode primitives.Mode, row StageRow) ui.Node {
	if !row.ElapsedMeasured {
		return primitives.Badge(primitives.BadgeProps{Label: "Not measured", Status: design.StatusPending, Mode: mode})
	}
	tokens := mode.Tokens()
	return html.Span(html.Props{
		Data: map[string]string{"component": "pipeline-stage-elapsed"},
		Class: css.New(
			css.Font(css.FontStack(tokens.Fonts.Code)), css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
			css.TextColor(css.Hex(string(tokens.Colors.TextSecondary))), css.WhiteSpace.NoWrap,
		).String(),
		Text: row.Elapsed.String(),
	})
}

func stateLabel(state pipeline.State) string {
	switch state {
	case pipeline.StateSatisfied:
		return "Satisfied"
	case pipeline.StateFailed:
		return "Failed"
	case pipeline.StateBlocked:
		return "Blocked"
	case pipeline.StateSkipped:
		return "Skipped"
	case pipeline.StateNotImplemented:
		return "Not implemented"
	default:
		return "Unknown"
	}
}

func stateStatus(state pipeline.State) design.Status {
	switch state {
	case pipeline.StateSatisfied:
		return design.StatusSuccess
	case pipeline.StateFailed:
		return design.StatusFailure
	case pipeline.StateBlocked:
		return design.StatusBlocked
	case pipeline.StateSkipped:
		return design.StatusWarning
	case pipeline.StateNotImplemented:
		return design.StatusPending
	default:
		return design.StatusNeutral
	}
}

func cardClass(tokens design.Tokens) string {
	return css.New(
		u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Rhythm.PanelGap)), css.Padding(css.Px(tokens.Rhythm.PanelInset)),
		css.MaxWidth(css.Percent(100)), css.MinWidth(css.Zero),
		css.Bg(css.Hex(string(tokens.Colors.Surface1))), css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
		css.Border(css.Px(tokens.Geometry.BorderWidth), css.Hex(string(tokens.Colors.BorderStrong))),
		css.Rounded(css.Px(tokens.Geometry.PanelRadius)), css.Font(css.FontStack(tokens.Fonts.UI)),
	).String()
}

// RunCompletionCaveat is the compact strip PIPE-044 asks to appear beside a
// completed run's final message, so a reader of the timeline learns what the
// run did not do without opening the ledger view. It renders nothing when the
// audit could not be computed, because an empty audit is not evidence of a
// clean run.
func RunCompletionCaveat(summary SkipSummary, mode primitives.Mode) ui.Node {
	if !summary.Computed {
		return nil
	}
	tokens := mode.Tokens()
	established := summary.Satisfied
	declined := summary.Skipped
	unbuilt := summary.NotImplemented
	status := design.StatusSuccess
	if summary.Skipped+summary.NotImplemented > 0 {
		status = design.StatusWarning
	}
	return html.Div(html.Props{
		Role: "status",
		Data: map[string]string{
			"component":          "run-completion-caveat",
			"skip-ratio-percent": strconv.Itoa(int(summary.SkipRatio*100 + 0.5)),
		},
		Class: css.New(
			u.Flex, u.ItemsCenter, css.Gap(css.Px(tokens.Spacing.XS)), css.FlexWrap.Wrap,
			css.Padding(css.RawLength("4px 0")),
		).String(),
	},
		primitives.Badge(primitives.BadgeProps{
			Label:  fmt.Sprintf("%d%% of stages established nothing", int(summary.SkipRatio*100+0.5)),
			Status: status, Mode: mode,
		}),
		html.Span(html.Props{
			Class: css.New(
				css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
			).String(),
			Text: fmt.Sprintf(
				"%d established, %d declined, %d not implemented -- open the pipeline ledger for the full breakdown.",
				established, declined, unbuilt,
			),
		}),
	)
}
