package evidencereport

import (
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	reportevidence "codeflux.dev/codeflux/internal/evidence"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestReportCardRendersReadableExactClaimLevelEvidence(t *testing.T) {
	props := reportCardFixture(t)
	markup := renderReportCard(t, props)

	wants := []string{
		`data-component="evidence-report-card"`, `data-authoritative-store="sqlite"`,
		`data-guarantee-scope="claim-only"`, `data-report-id="` + props.Report.ID + `"`,
		"Final evidence report", "Revision and risk bindings", "Requirement revision",
		"Accepted plan revision", "Plan approval", "Base revision", "Final diff identity",
		"Risk classification revision", "Risk explanation", "Graph revision",
		"Changed-file summary", "2 insertions, 1 deletions", "old.go -&gt; new.go",
		"Validation outcomes", "Every required and advisory validation is shown",
		"Passed", "Failed", "Waived", "Skipped", "Unavailable", "Cancelled", "Invalidated",
		`data-validation-status="failed"`, "Disposition reason: Exact disposition: failed.",
		"Claim-level guarantees and provenance", `data-claim-id="internal-round-trip"`,
		`data-claim-guarantee="fully-evaluated"`, `data-boundary="internal"`,
		`data-claim-id="external-contract"`, `data-claim-guarantee="contract-checked"`,
		`data-boundary="external-system"`, "External-system behavior - scope: provider API",
		"External provider runtime behavior was not executed.", props.Report.Claims[0].EvidenceIDs[0].String(),
		props.Report.Claims[0].GraphNodeIDs[0].String(), props.Report.GraphRevisionID.String(),
		"Approvals and authority used", "explicit user approval", "Model, provider, tool, and policy versions",
		"Forecast and actual attribution", "P50 2m0s, P90 5m0s", "$0.08",
		"Assumptions and limitations", "No deployment was performed.",
	}
	for _, want := range wants {
		if !strings.Contains(markup, want) {
			t.Errorf("report card missing %q\n%s", want, markup)
		}
	}
	if count := strings.Count(markup, `data-report-claim="true"`); count != len(props.Report.Claims) {
		t.Fatalf("rendered claim count = %d, want %d", count, len(props.Report.Claims))
	}
	for _, forbidden := range []string{`data-global-guarantee=`, `data-report-guarantee=`, `data-guarantee="fully-evaluated"`} {
		if strings.Contains(markup, forbidden) {
			t.Fatalf("card inflated a claim guarantee to report scope via %q\n%s", forbidden, markup)
		}
	}
}

func TestReportCardShowsUnknownMetricsWithoutInventingZero(t *testing.T) {
	props := reportCardFixture(t)
	reason := "Provider accounting was unavailable."
	props.Report.Metrics = reportevidence.ForecastActual{
		ForecastDurationUnknownReason: reason,
		ForecastTokensUnknownReason:   reason,
		ForecastCostUnknownReason:     reason,
		ActualDurationUnknownReason:   reason,
		ActualTokensUnknownReason:     reason,
		ActualCostUnknownReason:       reason,
	}
	markup := renderReportCard(t, props)

	if count := strings.Count(markup, "Unknown - Provider accounting was unavailable."); count != 6 {
		t.Fatalf("unknown metric count = %d, want 6\n%s", count, markup)
	}
	for _, forbidden := range []string{
		`data-report-field="forecast-time">P50 0s`,
		`data-report-field="actual-time">0s`,
		`data-report-field="forecast-tokens">P50 0`,
		`data-report-field="actual-tokens">0 total`,
		`data-report-field="forecast-cost">P50  0`,
		`data-report-field="actual-cost">$0.00`,
	} {
		if strings.Contains(markup, forbidden) {
			t.Fatalf("unknown metric was invented as zero via %q\n%s", forbidden, markup)
		}
	}
}

func TestReportCardRefusesInflatedExternalGuarantee(t *testing.T) {
	props := reportCardFixture(t)
	props.Report.Claims[1].Guarantee = domain.AssuranceLevelFullyEvaluated
	markup := renderReportCard(t, props)
	for _, want := range []string{`data-component="inline-alert"`, "Final evidence report unavailable", "external-system behavior may only be contract-checked"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("invalid report alert missing %q\n%s", want, markup)
		}
	}
	if strings.Contains(markup, `data-component="evidence-report-card"`) {
		t.Fatalf("invalid guarantee reached report-card rendering\n%s", markup)
	}
}

func renderReportCard(t *testing.T, props Props) string {
	t.Helper()
	markup, err := ui.RenderToString(ui.CreateElement(ReportCard, props))
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func reportCardFixture(t *testing.T) Props {
	t.Helper()
	taskID, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	approvalID, err := domain.NewApprovalID()
	if err != nil {
		t.Fatal(err)
	}
	graphRevisionID, err := domain.NewGraphRevisionID()
	if err != nil {
		t.Fatal(err)
	}
	nodeID, err := domain.NewNodeID()
	if err != nil {
		t.Fatal(err)
	}
	evidenceID, err := domain.NewEvidenceID()
	if err != nil {
		t.Fatal(err)
	}
	passedValidationID, err := domain.NewValidationID()
	if err != nil {
		t.Fatal(err)
	}
	unavailableValidationID, err := domain.NewValidationID()
	if err != nil {
		t.Fatal(err)
	}
	usd, err := domain.ParseCurrencyCode("USD")
	if err != nil {
		t.Fatal(err)
	}
	diff := strings.Repeat("d", 64)
	statuses := []reportevidence.ValidationStatus{
		reportevidence.ValidationPassed, reportevidence.ValidationFailed, reportevidence.ValidationWaived,
		reportevidence.ValidationSkipped, reportevidence.ValidationUnavailable,
		reportevidence.ValidationCancelled, reportevidence.ValidationInvalidated,
	}
	validations := make([]reportevidence.ValidationCheck, 0, len(statuses))
	for index, status := range statuses {
		check := reportevidence.ValidationCheck{CheckID: "check-" + string(status), Required: index < 2, Status: status, Summary: "Final status: " + string(status) + ".", DiffIdentity: diff}
		if status == reportevidence.ValidationPassed {
			check.ValidationRunID = passedValidationID.String()
			check.CommandDigest = strings.Repeat("c", 64)
		} else {
			check.StatusReason = "Exact disposition: " + string(status) + "."
			if status == reportevidence.ValidationUnavailable {
				check.ValidationRunID = unavailableValidationID.String()
				check.CommandDigest = strings.Repeat("a", 64)
			}
		}
		validations = append(validations, check)
	}
	report := reportevidence.Report{
		ID: strings.Repeat("e", 64), TaskID: taskID, RequirementRevision: 3,
		AcceptedPlanRevision: 2, PlanApprovalID: approvalID,
		BaseRevision: strings.Repeat("b", 40), DiffIdentity: diff,
		RiskClassificationRevision: 4, Risk: domain.RiskLevelElevated,
		RiskExplanation: "Elevated because durable storage changed.", GraphRevisionID: graphRevisionID,
		Metrics: reportevidence.ForecastActual{
			ForecastDurationKnown: true, ForecastP50: 2 * time.Minute, ForecastP90: 5 * time.Minute,
			ForecastTokensKnown: true, ForecastTokensP50: 1000, ForecastTokensP90: 2500,
			ForecastCostKnown: true, ForecastCostP50: domain.Money{Currency: usd, MinorUnits: 4}, ForecastCostP90: domain.Money{Currency: usd, MinorUnits: 12},
			ActualDurationKnown: true, ActualDuration: 3 * time.Minute,
			ActualTokens:    domain.TokenUsage{Known: true, Input: 700, CachedInput: 100, Output: 250, Reasoning: 50, ProviderSpecific: map[string]domain.TokenCount{"accepted": 1100}},
			ActualCostKnown: true, ActualCost: domain.Money{Currency: usd, MinorUnits: 8},
		},
		ChangedFiles: []reportevidence.ChangedFile{
			{Path: "internal/evidence/report.go", Status: reportevidence.FileModified, Insertions: 2, Deletions: 1},
			{Path: "new.go", PriorPath: "old.go", Status: reportevidence.FileRenamed},
		},
		Validations: validations,
		Approvals:   []reportevidence.ApprovalUse{{ApprovalID: approvalID, State: domain.ApprovalRequestStateGranted, Scope: "Approve exact accepted plan revision.", AuthorityUsed: "explicit user approval"}},
		Versions: []reportevidence.VersionBinding{
			{Kind: reportevidence.VersionModel, Name: "coding model", Known: true, Version: "gpt-5"},
			{Kind: reportevidence.VersionProvider, Name: "OpenAI", Known: true, Version: "responses-v1"},
			{Kind: reportevidence.VersionTool, Name: "Go", Known: true, Version: "go1.25"},
			{Kind: reportevidence.VersionPolicy, Name: "risk classification", Known: true, Version: "v1"},
		},
		Assumptions: []string{"The final diff identity covers every listed validation."},
		Limitations: []string{"No deployment was performed."},
		Claims: []reportevidence.Claim{
			{ID: "internal-round-trip", Statement: "The report round-trips through structured SQLite rows.", Scope: "final report persistence", Boundary: reportevidence.BoundaryInternal, Guarantee: domain.AssuranceLevelFullyEvaluated, GuaranteeReason: "Focused storage tests evaluate the exact final diff.", EvidenceIDs: []domain.EvidenceID{evidenceID}, ValidationRunIDs: []string{passedValidationID.String()}, GraphNodeIDs: []domain.NodeID{nodeID}},
			{ID: "external-contract", Statement: "The provider contract remains compatible.", Scope: "provider API", Boundary: reportevidence.BoundaryExternal, Guarantee: domain.AssuranceLevelContractChecked, GuaranteeReason: "The local contract is checked without claiming provider execution.", ValidationRunIDs: []string{passedValidationID.String()}, GraphNodeIDs: []domain.NodeID{nodeID}, Limitations: []string{"External provider runtime behavior was not executed."}},
		},
		IdempotencyKey: "report-card", CreatedAt: time.Date(2026, 7, 31, 15, 30, 0, 0, time.UTC),
	}
	return Props{Report: report}
}
