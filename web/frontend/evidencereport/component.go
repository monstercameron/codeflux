package evidencereport

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"codeflux.dev/codeflux/internal/domain"
	reportevidence "codeflux.dev/codeflux/internal/evidence"
	"codeflux.dev/codeflux/web/frontend/design"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"codeflux.dev/codeflux/web/frontend/readout"
	"github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/css/u"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// ReportCard renders an immutable report. Guarantee labels are deliberately
// scoped to individual claims; the card never presents one global guarantee.
func ReportCard(props Props) ui.Node {
	if err := props.Validate(); err != nil {
		return primitives.InlineAlert(primitives.InlineAlertProps{
			Title: "Final evidence report unavailable", Message: err.Error(),
			Tone: design.StatusFailure, Mode: props.Mode,
		})
	}
	report := props.Report.Clone()
	tokens := props.Mode.Tokens()
	return html.Article(html.Props{
		Aria: map[string]string{"label": "Final evidence report " + report.ID},
		Data: map[string]string{
			"component": "evidence-report-card", "report-id": report.ID,
			"task-id": report.TaskID.String(), "schema-version": strconv.FormatUint(uint64(reportevidence.SchemaVersion), 10),
			"authoritative-store": "sqlite", "guarantee-scope": "claim-only", "read-only": "true",
		},
		Class: cardClass(tokens),
	},
		header(props.Mode, report),
		bindingSection(props.Mode, report),
		changedFilesSection(props.Mode, report),
		validationsSection(props.Mode, report),
		claimsSection(props.Mode, report),
		authoritySection(props.Mode, report),
		versionsSection(props.Mode, report),
		metricsSection(props.Mode, report),
		narrativesSection(props.Mode, report),
	)
}

func header(mode primitives.Mode, report reportevidence.Report) ui.Node {
	tokens := mode.Tokens()
	return html.Header(html.Props{Class: rowClass(tokens)},
		html.Div(html.Props{Class: stackClass(tokens)},
			html.P(html.Props{Class: eyebrowClass(tokens), Text: "Immutable completion evidence"}),
			html.H2(html.Props{Class: headingClass(tokens), Text: "Final evidence report"}),
			html.P(html.Props{Class: monoClass(tokens), Text: report.ID}),
		),
		primitives.Badge(primitives.BadgeProps{Label: humanize(string(report.Risk)) + " risk", Status: riskTone(report.Risk), Mode: mode}),
	)
}

func bindingSection(mode primitives.Mode, report reportevidence.Report) ui.Node {
	return section(mode, "Revision and risk bindings", "bindings",
		definitionList(mode,
			definition{"Task", report.TaskID.String(), "task-id"},
			definition{"Requirement revision", strconv.FormatUint(report.RequirementRevision, 10), "requirement-revision"},
			definition{"Accepted plan revision", strconv.FormatUint(report.AcceptedPlanRevision, 10), "accepted-plan-revision"},
			definition{"Plan approval", report.PlanApprovalID.String(), "plan-approval-id"},
			definition{"Base revision", report.BaseRevision, "base-revision"},
			definition{"Final diff identity", report.DiffIdentity, "diff-identity"},
			definition{"Risk classification revision", strconv.FormatUint(report.RiskClassificationRevision, 10), "risk-revision"},
			definition{"Risk level", humanize(string(report.Risk)), "risk-level"},
			definition{"Risk explanation", report.RiskExplanation, "risk-explanation"},
			definition{"Graph revision", report.GraphRevisionID.String(), "graph-revision-id"},
			definition{"Created", report.CreatedAt.Format("2006-01-02 15:04:05.000000 UTC"), "created-at"},
		),
	)
}

func changedFilesSection(mode primitives.Mode, report reportevidence.Report) ui.Node {
	counts := map[reportevidence.ChangedFileStatus]int{}
	for _, file := range report.ChangedFiles {
		counts[file.Status]++
	}
	children := []ui.Node{definitionList(mode,
		definition{"Total changed files", strconv.Itoa(len(report.ChangedFiles)), "changed-file-total"},
		definition{"Added", strconv.Itoa(counts[reportevidence.FileAdded]), "changed-file-added"},
		definition{"Modified", strconv.Itoa(counts[reportevidence.FileModified]), "changed-file-modified"},
		definition{"Deleted", strconv.Itoa(counts[reportevidence.FileDeleted]), "changed-file-deleted"},
		definition{"Renamed", strconv.Itoa(counts[reportevidence.FileRenamed]), "changed-file-renamed"},
	)}
	if len(report.ChangedFiles) == 0 {
		children = append(children, emptyText(mode, "No changed files were recorded."))
	} else {
		items := make([]ui.Node, 0, len(report.ChangedFiles))
		for _, file := range report.ChangedFiles {
			pathLabel := file.Path
			if file.Status == reportevidence.FileRenamed {
				pathLabel = file.PriorPath + " -> " + file.Path
			}
			items = append(items, html.Li(html.Props{
				Data:  map[string]string{"changed-file": file.Path, "status": string(file.Status), "generated": strconv.FormatBool(file.Generated)},
				Class: itemClass(mode.Tokens()),
			},
				html.Div(html.Props{Class: rowClass(mode.Tokens())},
					html.Strong(html.Props{Class: monoClass(mode.Tokens()), Text: pathLabel}),
					primitives.Badge(primitives.BadgeProps{Label: humanize(string(file.Status)), Status: design.StatusNeutral, Mode: mode}),
				),
				html.P(html.Props{Class: secondaryClass(mode.Tokens()), Text: fmt.Sprintf("%d insertions, %d deletions; generated: %t", file.Insertions, file.Deletions, file.Generated)}),
			))
		}
		children = append(children, html.Ul(html.Props{Class: listClass(mode.Tokens()), Data: map[string]string{"report-list": "changed-files"}}, items...))
	}
	return section(mode, "Changed-file summary", "changed-files", children...)
}

func validationsSection(mode primitives.Mode, report reportevidence.Report) ui.Node {
	items := make([]ui.Node, 0, len(report.Validations))
	for _, check := range report.Validations {
		disposition := "Required"
		if !check.Required {
			disposition = "Advisory"
		}
		children := []ui.Node{
			html.Div(html.Props{Class: rowClass(mode.Tokens())},
				html.Strong(html.Props{Text: check.CheckID}),
				primitives.Badge(primitives.BadgeProps{Label: humanize(string(check.Status)), Status: validationTone(check.Status), Mode: mode}),
			),
			html.P(html.Props{Class: secondaryClass(mode.Tokens()), Text: disposition + " - " + check.Summary}),
			html.P(html.Props{Class: monoClass(mode.Tokens()), Text: "Final diff: " + check.DiffIdentity}),
		}
		if check.ValidationRunID != "" {
			children = append(children, html.P(html.Props{Class: monoClass(mode.Tokens()), Text: "Validation run: " + check.ValidationRunID}))
		}
		if check.CommandDigest != "" {
			children = append(children, html.P(html.Props{Class: monoClass(mode.Tokens()), Text: "Command digest: " + check.CommandDigest}))
		}
		if check.StatusReason != "" {
			children = append(children, html.P(html.Props{Data: map[string]string{"validation-status-reason": check.CheckID}, Class: secondaryClass(mode.Tokens()), Text: "Disposition reason: " + check.StatusReason}))
		}
		items = append(items, html.Li(html.Props{
			Data:  map[string]string{"validation-check": check.CheckID, "validation-status": string(check.Status), "required": strconv.FormatBool(check.Required), "diff-identity": check.DiffIdentity},
			Class: itemClass(mode.Tokens()),
		}, children...))
	}
	return section(mode, "Validation outcomes", "validations",
		html.P(html.Props{Class: secondaryClass(mode.Tokens()), Text: "Every required and advisory validation is shown with its exact final disposition."}),
		html.Ul(html.Props{Class: listClass(mode.Tokens()), Data: map[string]string{"report-list": "validations", "count": strconv.Itoa(len(items))}}, items...),
	)
}

func claimsSection(mode primitives.Mode, report reportevidence.Report) ui.Node {
	items := make([]ui.Node, 0, len(report.Claims))
	for _, claim := range report.Claims {
		boundaryLabel := "Internal behavior"
		if claim.Boundary == reportevidence.BoundaryExternal {
			boundaryLabel = "External-system behavior"
		}
		children := []ui.Node{
			html.Div(html.Props{Class: rowClass(mode.Tokens())},
				html.Strong(html.Props{Text: claim.Statement}),
				primitives.Badge(primitives.BadgeProps{Label: humanize(string(claim.Guarantee)), Status: guaranteeTone(claim.Guarantee), Mode: mode}),
			),
			html.P(html.Props{Class: secondaryClass(mode.Tokens()), Text: boundaryLabel + " - scope: " + claim.Scope}),
			html.P(html.Props{Data: map[string]string{"claim-guarantee-reason": claim.ID}, Class: bodyClass(mode.Tokens()), Text: "Guarantee basis: " + claim.GuaranteeReason}),
			narrativeList(mode, "Evidence", "claim-evidence", typedStrings(claim.EvidenceIDs)),
			narrativeList(mode, "Validation runs", "claim-validations", claim.ValidationRunIDs),
			narrativeList(mode, "Graph nodes in revision "+report.GraphRevisionID.String(), "claim-graph-nodes", typedStrings(claim.GraphNodeIDs)),
			narrativeList(mode, "Claim assumptions", "claim-assumptions", claim.Assumptions),
			narrativeList(mode, "Claim limitations", "claim-limitations", claim.Limitations),
		}
		items = append(items, html.Li(html.Props{
			Data: map[string]string{
				"report-claim": "true", "claim-id": claim.ID, "claim-guarantee": string(claim.Guarantee),
				"boundary": string(claim.Boundary), "graph-revision-id": report.GraphRevisionID.String(),
			}, Class: itemClass(mode.Tokens()),
		}, children...))
	}
	return section(mode, "Claim-level guarantees and provenance", "claims",
		html.P(html.Props{Class: secondaryClass(mode.Tokens()), Text: "Each guarantee applies only to the claim shown with it; external-system boundaries are never presented as fully evaluated."}),
		html.Ul(html.Props{Class: listClass(mode.Tokens()), Data: map[string]string{"report-list": "claims", "count": strconv.Itoa(len(items))}}, items...),
	)
}

func authoritySection(mode primitives.Mode, report reportevidence.Report) ui.Node {
	items := make([]ui.Node, 0, len(report.Approvals))
	for _, approval := range report.Approvals {
		authority := "No authority exercised"
		if approval.AuthorityUsed != "" {
			authority = approval.AuthorityUsed
		}
		items = append(items, html.Li(html.Props{Data: map[string]string{"approval-id": approval.ApprovalID.String(), "approval-state": string(approval.State)}, Class: itemClass(mode.Tokens())},
			html.Div(html.Props{Class: rowClass(mode.Tokens())},
				html.Strong(html.Props{Class: monoClass(mode.Tokens()), Text: approval.ApprovalID.String()}),
				primitives.Badge(primitives.BadgeProps{Label: humanize(string(approval.State)), Status: approvalTone(approval.State), Mode: mode}),
			),
			html.P(html.Props{Class: bodyClass(mode.Tokens()), Text: "Scope: " + approval.Scope}),
			html.P(html.Props{Class: secondaryClass(mode.Tokens()), Text: "Authority used: " + authority}),
		))
	}
	return section(mode, "Approvals and authority used", "authority",
		html.Ul(html.Props{Class: listClass(mode.Tokens()), Data: map[string]string{"report-list": "approvals"}}, items...),
	)
}

func versionsSection(mode primitives.Mode, report reportevidence.Report) ui.Node {
	items := make([]ui.Node, 0, len(report.Versions))
	for _, version := range report.Versions {
		value := version.Version
		known := "true"
		if !version.Known {
			known = "false"
			value = "Unknown - " + version.UnknownReason
		}
		items = append(items, html.Li(html.Props{
			Data: map[string]string{"version-kind": string(version.Kind), "version-known": known}, Class: itemClass(mode.Tokens()),
			Text: humanize(string(version.Kind)) + ": " + version.Name + " - " + value,
		}))
	}
	return section(mode, "Model, provider, tool, and policy versions", "versions",
		html.Ul(html.Props{Class: listClass(mode.Tokens()), Data: map[string]string{"report-list": "versions"}}, items...),
	)
}

func metricsSection(mode primitives.Mode, report reportevidence.Report) ui.Node {
	metrics := report.Metrics
	return section(mode, "Forecast and actual attribution", "metrics",
		definitionList(mode,
			definition{"Forecast time", durationRange(metrics.ForecastDurationKnown, metrics.ForecastP50.String(), metrics.ForecastP90.String(), metrics.ForecastDurationUnknownReason), "forecast-time"},
			definition{"Actual time", knownValue(metrics.ActualDurationKnown, metrics.ActualDuration.String(), metrics.ActualDurationUnknownReason), "actual-time"},
			definition{"Forecast tokens", uintRange(metrics.ForecastTokensKnown, metrics.ForecastTokensP50, metrics.ForecastTokensP90, metrics.ForecastTokensUnknownReason), "forecast-tokens"},
			definition{"Actual tokens", tokenUsageText(metrics.ActualTokens, metrics.ActualTokensUnknownReason), "actual-tokens"},
			definition{"Forecast cost", moneyRange(metrics.ForecastCostKnown, metrics.ForecastCostP50, metrics.ForecastCostP90, metrics.ForecastCostUnknownReason), "forecast-cost"},
			definition{"Actual cost", knownValue(metrics.ActualCostKnown, moneyText(metrics.ActualCost), metrics.ActualCostUnknownReason), "actual-cost"},
		),
	)
}

func narrativesSection(mode primitives.Mode, report reportevidence.Report) ui.Node {
	return section(mode, "Assumptions and limitations", "narratives",
		narrativeList(mode, "Report assumptions", "report-assumptions", report.Assumptions),
		narrativeList(mode, "Report limitations", "report-limitations", report.Limitations),
	)
}

type definition struct{ label, value, kind string }

func definitionList(mode primitives.Mode, values ...definition) ui.Node {
	children := make([]ui.Node, 0, len(values)*2)
	for _, value := range values {
		children = append(children,
			html.Tag("dt", html.Props{Class: definitionLabelClass(mode.Tokens()), Text: value.label}),
			html.Tag("dd", html.Props{Data: map[string]string{"report-field": value.kind}, Class: definitionValueClass(mode.Tokens()), Text: value.value}),
		)
	}
	return html.Tag("dl", html.Props{Class: definitionsClass(mode.Tokens())}, children...)
}

func section(mode primitives.Mode, title, kind string, children ...ui.Node) ui.Node {
	content := []ui.Node{html.H3(html.Props{Class: sectionHeadingClass(mode.Tokens()), Text: title})}
	content = append(content, children...)
	return html.Section(html.Props{Aria: map[string]string{"label": title}, Data: map[string]string{"report-section": kind}, Class: sectionClass(mode.Tokens())}, content...)
}

func typedStrings[T fmt.Stringer](values []T) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = value.String()
	}
	return result
}

func narrativeList(mode primitives.Mode, title, kind string, values []string) ui.Node {
	children := []ui.Node{html.H4(html.Props{Class: subheadingClass(mode.Tokens()), Text: title})}
	if len(values) == 0 {
		children = append(children, emptyText(mode, "None recorded."))
	} else {
		items := make([]ui.Node, len(values))
		for index, value := range values {
			items[index] = html.Li(html.Props{Text: value})
		}
		children = append(children, html.Ul(html.Props{Class: compactListClass(mode.Tokens()), Data: map[string]string{"claim-link-list": kind, "count": strconv.Itoa(len(values))}}, items...))
	}
	return html.Div(html.Props{Data: map[string]string{"report-subsection": kind}, Class: stackClass(mode.Tokens())}, children...)
}

func emptyText(mode primitives.Mode, value string) ui.Node {
	return html.P(html.Props{Class: secondaryClass(mode.Tokens()), Text: value})
}

func knownValue(known bool, value, reason string) string {
	if known {
		return value
	}
	if reason == "" {
		return "Unknown"
	}
	return "Unknown - " + reason
}

func durationRange(known bool, p50, p90, reason string) string {
	return knownValue(known, "P50 "+p50+", P90 "+p90, reason)
}

func uintRange(known bool, p50, p90 uint64, reason string) string {
	return knownValue(known, fmt.Sprintf("P50 %d, P90 %d", p50, p90), reason)
}

func moneyRange(known bool, p50, p90 domain.Money, reason string) string {
	return knownValue(known, "P50 "+moneyText(p50)+" · P90 "+moneyText(p90), reason)
}

func moneyText(value domain.Money) string {
	return readout.FormatExactMoneyForReading(value)
}

func tokenUsageText(value domain.TokenUsage, reason string) string {
	if !value.Known {
		return knownValue(false, "", reason)
	}
	total, _, err := value.Total()
	if err != nil {
		return "Unknown - token usage is invalid"
	}
	text := fmt.Sprintf("%d total: input %d, cached input %d, cache write %d, output %d, reasoning %d", total, value.Input, value.CachedInput, value.CacheWrite, value.Output, value.Reasoning)
	keys := make([]string, 0, len(value.ProviderSpecific))
	for key := range value.ProviderSpecific {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		text += fmt.Sprintf(", %s %d", key, value.ProviderSpecific[key])
	}
	return text
}

func humanize(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), "-", " ")
	if value == "" {
		return "Unknown"
	}
	runes := []rune(value)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

func validationTone(status reportevidence.ValidationStatus) design.Status {
	switch status {
	case reportevidence.ValidationPassed:
		return design.StatusSuccess
	case reportevidence.ValidationFailed:
		return design.StatusFailure
	case reportevidence.ValidationInvalidated:
		return design.StatusInvalidated
	case reportevidence.ValidationCancelled:
		return design.StatusBlocked
	case reportevidence.ValidationWaived, reportevidence.ValidationSkipped, reportevidence.ValidationUnavailable:
		return design.StatusWarning
	default:
		return design.StatusPending
	}
}

func guaranteeTone(level domain.AssuranceLevel) design.Status {
	switch level {
	case domain.AssuranceLevelFullyEvaluated, domain.AssuranceLevelModelVerified:
		return design.StatusSuccess
	case domain.AssuranceLevelContractChecked:
		return design.StatusActive
	case domain.AssuranceLevelRuntimeOnly:
		return design.StatusWarning
	case domain.AssuranceLevelInvalidated:
		return design.StatusInvalidated
	default:
		return design.StatusPending
	}
}

func riskTone(risk domain.RiskLevel) design.Status {
	switch risk {
	case domain.RiskLevelRoutine:
		return design.StatusNeutral
	case domain.RiskLevelElevated:
		return design.StatusWarning
	case domain.RiskLevelProtected:
		return design.StatusBlocked
	default:
		return design.StatusPending
	}
}

func approvalTone(state domain.ApprovalRequestState) design.Status {
	switch state {
	case domain.ApprovalRequestStateGranted:
		return design.StatusSuccess
	case domain.ApprovalRequestStateDenied:
		return design.StatusFailure
	case domain.ApprovalRequestStateExpired, domain.ApprovalRequestStateCancelled:
		return design.StatusWarning
	default:
		return design.StatusPending
	}
}

func cardClass(tokens design.Tokens) string {
	return css.New(u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Rhythm.PanelGap)), css.Padding(css.Px(tokens.Rhythm.PanelInset)), css.MaxWidth(css.Percent(100)), css.Bg(css.Hex(string(tokens.Colors.Surface1))), css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))), css.Border(css.Px(tokens.Geometry.BorderWidth), css.Hex(string(tokens.Colors.BorderStrong))), css.Rounded(css.Px(tokens.Geometry.PanelRadius)), css.Font(css.FontStack(tokens.Fonts.UI))).String()
}

func sectionClass(tokens design.Tokens) string {
	return css.New(u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.SM)), css.Padding(css.Px(tokens.Spacing.MD)), css.Bg(css.Hex(string(tokens.Colors.Surface2))), css.Border(css.Px(tokens.Geometry.BorderWidth), css.Hex(string(tokens.Colors.BorderSubtle))), css.Rounded(css.Px(tokens.Geometry.RadiusSmall))).String()
}

func rowClass(tokens design.Tokens) string {
	return css.New(u.Flex, u.ItemsCenter, u.JustifyBetween, css.Gap(css.Px(tokens.Spacing.MD))).String()
}

func stackClass(tokens design.Tokens) string {
	return css.New(u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.XS)), css.MinWidth(css.Zero)).String()
}

func listClass(tokens design.Tokens) string {
	return css.New(u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.SM)), css.Margin(css.Zero), css.Padding(css.Px(tokens.Spacing.SM))).String()
}

func compactListClass(tokens design.Tokens) string {
	return css.New(u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.XS)), css.Margin(css.Zero), css.Padding(css.Px(tokens.Spacing.SM))).String()
}

func itemClass(tokens design.Tokens) string {
	return css.New(u.Flex, u.FlexCol, css.Gap(css.Px(tokens.Spacing.XS)), css.Padding(css.Px(tokens.Spacing.SM)), css.Bg(css.Hex(string(tokens.Colors.Surface3))), css.Rounded(css.Px(tokens.Geometry.RadiusSmall)), css.OverflowWrap.Anywhere).String()
}

func definitionsClass(tokens design.Tokens) string {
	return css.New(u.Grid, css.GridCols(css.Repeat(2, css.MinMax(css.TrackLen(css.Zero), css.Fr(1)))), css.Gap(css.Px(tokens.Spacing.SM)), css.Margin(css.Zero)).String()
}

func headingClass(tokens design.Tokens) string {
	return css.New(css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))), css.Margin(css.Zero), css.FontSize(css.Px(tokens.Typography.PanelHeading.Size)), css.LineHeightLen(css.Px(tokens.Typography.PanelHeading.LineHeight)), css.FontWeight.Semibold).String()
}

func sectionHeadingClass(tokens design.Tokens) string {
	return css.New(css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))), css.Margin(css.Zero), css.FontSize(css.Px(tokens.Typography.SectionTitle.Size)), css.LineHeightLen(css.Px(tokens.Typography.SectionTitle.LineHeight)), css.FontWeight.Semibold).String()
}

func subheadingClass(tokens design.Tokens) string {
	return css.New(css.TextColor(css.Hex(string(tokens.Colors.TextPrimary))), css.Margin(css.Zero), css.FontSize(css.Px(tokens.Typography.CompactBody.Size)), css.LineHeightLen(css.Px(tokens.Typography.CompactBody.LineHeight)), css.FontWeight.Semibold).String()
}

func eyebrowClass(tokens design.Tokens) string {
	return css.New(css.Margin(css.Zero), css.TextColor(css.Hex(string(tokens.Colors.TextMuted))), css.FontSize(css.Px(tokens.Typography.Metadata.Size)), css.FontWeight.Semibold).String()
}

func bodyClass(tokens design.Tokens) string {
	return css.New(css.Margin(css.Zero), css.FontSize(css.Px(tokens.Typography.CompactBody.Size)), css.LineHeightLen(css.Px(tokens.Typography.CompactBody.LineHeight))).String()
}

func secondaryClass(tokens design.Tokens) string {
	return css.New(css.Margin(css.Zero), css.TextColor(css.Hex(string(tokens.Colors.TextSecondary))), css.FontSize(css.Px(tokens.Typography.CompactBody.Size)), css.LineHeightLen(css.Px(tokens.Typography.CompactBody.LineHeight))).String()
}

func monoClass(tokens design.Tokens) string {
	return css.New(css.Margin(css.Zero), css.Font(css.FontStack(tokens.Fonts.Code)), css.FontSize(css.Px(tokens.Typography.Code.Size)), css.LineHeightLen(css.Px(tokens.Typography.Code.LineHeight)), css.OverflowWrap.Anywhere).String()
}

func definitionLabelClass(tokens design.Tokens) string {
	return css.New(css.TextColor(css.Hex(string(tokens.Colors.TextMuted))), css.FontSize(css.Px(tokens.Typography.Metadata.Size)), css.FontWeight.Semibold).String()
}

func definitionValueClass(tokens design.Tokens) string {
	return css.New(css.Margin(css.Zero), css.FontSize(css.Px(tokens.Typography.CompactBody.Size)), css.OverflowWrap.Anywhere).String()
}
