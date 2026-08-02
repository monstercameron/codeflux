//go:build js && wasm

// Command galleryfixture mounts every shared interface component in every
// state it can be in, so a person can look at the whole set at once.
//
// The console's components are otherwise only reachable through the states a
// running coordinator happens to produce, which means the rare ones — a denied
// panel, a retained command, an interrupted message, a cap that has been
// reached — are the ones nobody ever looks at. This fixture renders no
// authoritative state and drives no transport: it is a sheet of specimens for
// screenshot review of the design, not a demonstration of the product.
package main

import (
	"strconv"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/design"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"codeflux.dev/codeflux/web/frontend/shell"
	"codeflux.dev/codeflux/web/frontend/timelinecard"
	"codeflux.dev/codeflux/web/frontend/timelineview"
	"github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/css/u"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/GoWebComponents/v5/utils"
)

func main() {
	ui.Render(ui.CreateElement(galleryFixture), "#app")
	utils.WaitForever()
}

func galleryFixture() ui.Node {
	return html.Main(
		html.Props{
			Data: map[string]string{"testid": "gallery-fixture"},
		},
		themeSheet(design.ThemeDark),
		themeSheet(design.ThemeLight),
		themeSheet(design.ThemeHighContrast),
	)
}

func modeFor(theme design.Theme) primitives.Mode {
	return primitives.Mode{
		Theme: theme, Density: design.DensityComfortable,
		HighContrast: theme == design.ThemeHighContrast,
	}
}

func themeSheet(theme design.Theme) ui.Node {
	mode := modeFor(theme)
	tokens := mode.Tokens()
	return html.Section(
		html.Props{
			Data: map[string]string{"gallery-theme": string(theme)},
			Class: css.New(
				css.Padding(css.Px(32)),
				css.Bg(css.Hex(string(tokens.Colors.Canvas))),
				css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
				css.Font(css.FontStack(tokens.Fonts.UI)),
			).String(),
		},
		sheetHeading(string(theme)+" theme", tokens),
		specimen("Colour", swatches(tokens), tokens),
		specimen("Type scale", typeScale(tokens), tokens),
		specimen("Buttons", buttonSpecimens(mode), tokens),
		specimen("Status badges", badgeSpecimens(mode), tokens),
		specimen("Kind chips", chipSpecimens(mode), tokens),
		specimen("Fields", fieldSpecimens(mode, tokens), tokens),
		specimen("Data states", dataStateSpecimens(mode), tokens),
		specimen("Transcript entries", cardSpecimens(mode), tokens),
		specimen("Project memory", memorySpecimen(tokens), tokens),
	)
}

func sheetHeading(label string, tokens design.Tokens) ui.Node {
	return html.H1(html.Props{
		Class: css.New(
			css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
			css.Margin(css.RawLength("0 0 24px")),
			css.Font(css.FontStack(tokens.Fonts.Display)),
			css.FontSize(css.Px(tokens.Typography.WorkspaceTitle.Size)),
			css.LineHeightLen(css.Px(tokens.Typography.WorkspaceTitle.LineHeight)),
			css.FontWeight.Normal,
		).String(),
		Text: label,
	})
}

func specimen(title string, content ui.Node, tokens design.Tokens) ui.Node {
	return html.Section(
		html.Props{
			Data: map[string]string{"gallery-specimen": title},
			Class: css.New(
				css.Padding(css.RawLength("20px 0")),
				css.BorderTop(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
			).String(),
		},
		html.H2(html.Props{
			Class: css.New(
				css.Margin(css.RawLength("0 0 14px")),
				css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				css.FontWeight.Semibold,
				css.Tracking(css.Ems(0.09)),
				css.TextTransform.Uppercase,
				css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
			).String(),
			Text: title,
		}),
		content,
	)
}

func row(children ...ui.Node) ui.Node {
	return html.Div(html.Props{
		Class: css.New(
			u.Flex, u.ItemsCenter, css.FlexWrap.Wrap, css.Gap(css.Px(12)),
		).String(),
	}, children...)
}

func stack(children ...ui.Node) ui.Node {
	return html.Div(html.Props{
		Class: css.New(u.Flex, u.FlexCol, css.Gap(css.Px(12)), css.MaxWidth(css.Px(760))).String(),
	}, children...)
}

func swatches(tokens design.Tokens) ui.Node {
	type entry struct {
		name  string
		value design.Color
	}
	entries := []entry{
		{"canvas", tokens.Colors.Canvas}, {"shell", tokens.Colors.Shell},
		{"surface-1", tokens.Colors.Surface1}, {"surface-2", tokens.Colors.Surface2},
		{"surface-3", tokens.Colors.Surface3}, {"inset", tokens.Colors.SurfaceInset},
		{"accent", tokens.Colors.Accent}, {"link", tokens.Colors.Link},
		{"success", tokens.Colors.Success}, {"warning", tokens.Colors.Warning},
		{"failure", tokens.Colors.Failure}, {"active/live", tokens.Colors.Active},
		{"plan", tokens.Colors.Plan}, {"evidence", tokens.Colors.Evidence},
		{"code", tokens.Colors.Code}, {"test", tokens.Colors.Test},
		{"execution", tokens.Colors.Execution}, {"validation", tokens.Colors.Validation},
		{"memory", tokens.Colors.Memory}, {"forecast", tokens.Colors.Forecast},
		{"focus ring", tokens.Colors.FocusRing},
	}
	nodes := make([]ui.Node, 0, len(entries))
	for _, item := range entries {
		nodes = append(nodes, html.Div(html.Props{
			Class: css.New(u.Flex, u.FlexCol, css.Gap(css.Px(4)), css.W(css.Px(104))).String(),
		},
			html.Span(html.Props{
				Class: css.New(
					css.Display.Block, css.H(css.Px(40)),
					css.Rounded(css.Px(tokens.Geometry.RadiusSmall)),
					css.Bg(css.Hex(string(item.value))),
					css.Border(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
				).String(),
			}),
			html.Span(html.Props{
				Class: css.New(
					css.Font(css.FontStack(tokens.Fonts.Code)),
					css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
					css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
				).String(),
				Text: item.name,
			}),
		))
	}
	return row(nodes...)
}

func typeScale(tokens design.Tokens) ui.Node {
	type entry struct {
		name  string
		style design.TypeStyle
		face  string
	}
	entries := []entry{
		{"workspace title", tokens.Typography.WorkspaceTitle, tokens.Fonts.Display},
		{"task title", tokens.Typography.TaskTitle, tokens.Fonts.Display},
		{"section title", tokens.Typography.SectionTitle, tokens.Fonts.Display},
		{"panel heading", tokens.Typography.PanelHeading, tokens.Fonts.UI},
		{"body", tokens.Typography.Body, tokens.Fonts.Reading},
		{"compact body", tokens.Typography.CompactBody, tokens.Fonts.Reading},
		{"control label", tokens.Typography.ControlLabel, tokens.Fonts.UI},
		{"metadata", tokens.Typography.Metadata, tokens.Fonts.UI},
		{"metric value", tokens.Typography.MetricValue, tokens.Fonts.Code},
		{"code", tokens.Typography.Code, tokens.Fonts.Code},
	}
	nodes := make([]ui.Node, 0, len(entries))
	for _, item := range entries {
		nodes = append(nodes, html.Div(html.Props{
			Class: css.New(u.Flex, u.ItemsCenter, css.Gap(css.Px(16))).String(),
		},
			html.Span(html.Props{
				Class: css.New(
					css.W(css.Px(130)), css.FlexShrink(css.Num(0)),
					css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
					css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
				).String(),
				Text: item.name,
			}),
			html.Span(html.Props{
				Class: css.New(
					css.Font(css.FontStack(item.face)),
					css.FontSize(css.Px(item.style.Size)),
					css.LineHeightLen(css.Px(item.style.LineHeight)),
				).String(),
				Text: "Supervising a run that edits your repository " +
					strconv.Itoa(item.style.Size) + "px",
			}),
		))
	}
	return stack(nodes...)
}

func buttonSpecimens(mode primitives.Mode) ui.Node {
	return stack(
		row(
			primitives.Button(primitives.ButtonProps{Label: "Send", Primary: true, Mode: mode, OnClick: func() {}}),
			primitives.Button(primitives.ButtonProps{Label: "Stop", Mode: mode, OnClick: func() {}}),
			primitives.Button(primitives.ButtonProps{Label: "Copy", Quiet: true, Mode: mode, OnClick: func() {}}),
			primitives.Button(primitives.ButtonProps{Icon: primitives.IconSearch, AccessibleLabel: "Search", Quiet: true, Mode: mode, OnClick: func() {}}),
		),
		row(
			primitives.Button(primitives.ButtonProps{Label: "Send", Primary: true, Busy: true, Mode: mode, OnClick: func() {}}),
			primitives.Button(primitives.ButtonProps{
				Label: "Pause", Disabled: true, Mode: mode,
				DisabledReason: "Nothing is running to pause. Describe a change to start a task.",
			}),
			primitives.Button(primitives.ButtonProps{
				Label: "Copy", Quiet: true, Disabled: true, Mode: mode,
				DisabledReason: "There is nothing to copy in this entry.",
			}),
		),
	)
}

func badgeSpecimens(mode primitives.Mode) ui.Node {
	statuses := []struct {
		status design.Status
		label  string
	}{
		{design.StatusNeutral, "Complete"}, {design.StatusAccent, "Needs review"},
		{design.StatusSuccess, "Passed"}, {design.StatusWarning, "Paused"},
		{design.StatusFailure, "Failed"}, {design.StatusActive, "Running"},
		{design.StatusBlocked, "Blocked"}, {design.StatusInvalidated, "Superseded"},
		{design.StatusPending, "Pending"}, {design.StatusPlan, "Plan"},
		{design.StatusEvidence, "Recorded"},
	}
	nodes := make([]ui.Node, 0, len(statuses))
	for _, item := range statuses {
		nodes = append(nodes, primitives.Badge(primitives.BadgeProps{
			Label: item.label, Status: item.status, Mode: mode,
		}))
	}
	return row(nodes...)
}

func chipSpecimens(mode primitives.Mode) ui.Node {
	kinds := []design.Kind{
		design.KindCode, design.KindTest, design.KindPlan, design.KindEvidence,
		design.KindMemory, design.KindForecast, design.KindExecution, design.KindValidation,
	}
	nodes := make([]ui.Node, 0, len(kinds))
	for _, kind := range kinds {
		presentation, err := design.KindPresentationFor(kind, mode.Tokens())
		if err != nil {
			continue
		}
		nodes = append(nodes, primitives.IconChip(primitives.IconChipProps{
			Kind: kind, Label: presentation.Label, Mode: mode,
		}))
	}
	return row(nodes...)
}

func fieldSpecimens(mode primitives.Mode, tokens design.Tokens) ui.Node {
	input := func(id, label, value string, disabled bool) ui.Node {
		props := html.Props{}
		props.ID = id
		props.Value = value
		props.Disabled = disabled
		props.Placeholder = "Describe the next change"
		props.Class = css.New(
			css.W(css.Full), css.MinHeight(css.Px(44)),
			css.PaddingX(css.Px(tokens.Spacing.MD)),
			css.Bg(css.Hex(string(tokens.Colors.SurfaceInset))),
			css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))),
			css.Border(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
			css.Rounded(css.Px(tokens.Geometry.ControlRadius)),
		).String()
		return html.Div(html.Props{Class: css.New(u.Flex, u.FlexCol, css.Gap(css.Px(6))).String()},
			html.Label(html.Props{For: id, Text: label, Class: css.New(
				css.FontSize(css.Px(tokens.Typography.Metadata.Size)),
				css.TextColor(css.Hex(string(tokens.Colors.TextMuted))),
			).String()}),
			html.Input(props),
		)
	}
	return stack(
		input("gallery-field-idle", "Idle", "", false),
		input("gallery-field-filled", "Filled", "Add a -upper flag to cmd/hello", false),
		input("gallery-field-disabled", "Disabled", "Waiting for the coordinator", true),
	)
}

func dataStateSpecimens(mode primitives.Mode) ui.Node {
	return stack(
		primitives.Skeleton(primitives.SkeletonProps{AccessibleLabel: "Loading transcript", Lines: 3, Mode: mode}),
		primitives.EmptyState(primitives.EmptyStateProps{
			Title: "Nothing mapped yet",
			Body:  "The graph draws itself as the run plans, edits, and checks its work.",
			Mode:  mode,
		}),
		primitives.ErrorState(primitives.ErrorStateProps{
			Title: "Could not load the transcript", Body: "The local coordinator refused the read. Retry when ready.",
			ActionLabel: "Retry", Mode: mode, OnAction: func() {},
		}),
		primitives.InlineAlert(primitives.InlineAlertProps{
			Title: "Access denied", Message: "This repository is outside the authorized workspace.",
			Tone: design.StatusWarning, Mode: mode,
		}),
		primitives.InlineAlert(primitives.InlineAlertProps{
			Title: "Update required", Message: "The frontend and coordinator stream contracts do not match.",
			Tone: design.StatusFailure, Mode: mode,
		}),
	)
}

func cardSpecimens(mode primitives.Mode) ui.Node {
	occurred := time.Date(2026, 8, 1, 16, 20, 0, 0, time.UTC)
	cards := []timelinecard.Card{
		{
			Kind: timelinecard.KindMessage, Sequence: 1, StableKey: "g:1", OccurredAt: occurred,
			Message: &timelinecard.Message{
				ID: "m1", Role: "user", Body: "Add a -upper flag to cmd/hello and cover it with a test.",
				Status: timelinecard.MessageComplete, OccurredAt: occurred,
			},
		},
		{
			Kind: timelinecard.KindMessage, Sequence: 2, StableKey: "g:2", OccurredAt: occurred,
			Message: &timelinecard.Message{
				ID: "m2", Role: "assistant", Body: "I will add the flag, then a table test that covers both cases.",
				Status: timelinecard.MessageProvisional, OccurredAt: occurred,
			},
		},
		{
			Kind: timelinecard.KindPlan, Sequence: 3, StableKey: "g:3", OccurredAt: occurred,
			Plan: &timelinecard.Plan{
				Revision: 1,
				Steps: []timelinecard.PlanStep{
					{Ordinal: 1, Title: "Add the flag to cmd/hello", Status: timelinecard.PlanStepComplete},
					{Ordinal: 2, Title: "Cover both cases with a table test", Status: timelinecard.PlanStepActive},
				},
			},
		},
		{
			Kind: timelinecard.KindTool, Sequence: 4, StableKey: "g:4", OccurredAt: occurred,
			Tool: &timelinecard.ToolActivity{
				ExecutionID: "exec-1", Tool: "edit_file", Purpose: "Add the flag",
				Scope: "cmd/hello/main.go", State: "running", Summary: "Editing cmd/hello/main.go",
			},
		},
		{
			Kind: timelinecard.KindValidation, Sequence: 5, StableKey: "g:5", OccurredAt: occurred,
			Validation: &timelinecard.Validation{
				ID: "v1", Status: timelinecard.ValidationPassed, Required: true,
				Summary: "go test ./... passed in 4.2s",
			},
		},
		{
			Kind: timelinecard.KindError, Sequence: 6, StableKey: "g:6", OccurredAt: occurred,
			Error: &timelinecard.Error{
				Code: "provider_refused", Message: "The provider refused the request.",
				AffectedAction: "model call", Retryable: true,
				NextSteps: []string{"Retry the step", "Lower the reasoning effort"},
			},
		},
	}
	nodes := make([]ui.Node, 0, len(cards))
	for _, card := range cards {
		nodes = append(nodes, ui.CreateElement(timelineview.Renderer, timelineview.Props{
			Card: card, Mode: mode,
			Actions: timelineview.Actions{OnCopy: func(string) {}},
		}))
	}
	return stack(nodes...)
}

var _ = domain.TaskStateRunning

// memorySpecimen shows the project-memory surface with one artifact open, so
// the list row, the governed badge, and the provenance column can be reviewed
// in every theme without a coordinator that has learned anything yet.
func memorySpecimen(tokens design.Tokens) ui.Node {
	rows := []shell.MemoryArtifactRow{
		{
			ArtifactID: "memory-artifact-01HQ8", RevisionID: "memory-artifact-revision-01HQ9",
			Kind: domain.MemoryArtifactKindRepositoryFact, Maturity: domain.MaturityStateValidated,
			Summary:        "The console is built with go run ./cmd/codeflux-dev build",
			RevisionNumber: 2, CreatedFromCorrection: true,
			CreatedAt: time.Date(2026, 7, 29, 16, 41, 0, 0, time.UTC),
		},
		{
			ArtifactID: "memory-artifact-01HQA", RevisionID: "memory-artifact-revision-01HQB",
			Kind: domain.MemoryArtifactKindReviewedCommand, Maturity: domain.MaturityStateCandidate,
			Summary: "go test ./internal/...", RevisionNumber: 1,
			CreatedAt: time.Date(2026, 7, 31, 9, 3, 0, 0, time.UTC),
		},
		{
			ArtifactID: "memory-artifact-01HQC", RevisionID: "memory-artifact-revision-01HQD",
			Kind: domain.MemoryArtifactKindObservationHypothesis, Maturity: domain.MaturityStateQuarantined,
			Summary:        "The wasm build races when two lanes build at once",
			RevisionNumber: 3,
			CreatedAt:      time.Date(2026, 7, 24, 22, 12, 0, 0, time.UTC),
		},
	}
	detail := shell.MemoryArtifactDetail{
		Row: rows[0],
		Fields: []shell.MemoryDetailField{
			{Label: "Statement", Value: "The console is built with go run ./cmd/codeflux-dev build"},
			{Label: "Category", Value: "build-command"},
		},
		OriginKnown: true, SupersedesRevisionID: "memory-artifact-revision-01HQ7",
		SupportingEpisodesKnown: true, SupportingEpisodes: []string{"episode-01HQ4"},
		BindingState:  "Bound to revision 9f2c1ab",
		ContentDigest: "6f1d2c9a4b77e0",
	}
	return html.Div(html.Props{
		Class: css.New(
			css.H(css.Px(560)),
			css.Border(css.Px(1), css.Hex(string(tokens.Colors.BorderSubtle))),
			css.Rounded(css.Px(tokens.Geometry.PanelRadius)),
			css.Overflow.Hidden,
		).String(),
	}, ui.CreateElement(shell.MemoryWorkspaceShell, shell.MemoryWorkspaceProps{
		Tokens: tokens, State: shell.SurfaceReady, Rows: rows,
		SelectedID: rows[0].ArtifactID, DetailState: shell.SurfaceReady, Detail: &detail,
		OnSelect: func(string) {}, OnFilterKind: func(domain.MemoryArtifactKind) {},
	}))
}
