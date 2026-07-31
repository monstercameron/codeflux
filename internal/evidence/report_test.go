package evidence

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

func TestReportValidatesCompleteClaimLevelProvenance(t *testing.T) {
	report := validReport(t)
	if err := report.Validate(); err != nil {
		t.Fatal(err)
	}

	clone := report.Clone()
	clone.Claims[0].EvidenceIDs[0] = mustEvidenceID(t)
	clone.Metrics.ActualTokens.ProviderSpecific["accepted"] = 99
	if report.Claims[0].EvidenceIDs[0] == clone.Claims[0].EvidenceIDs[0] || report.Metrics.ActualTokens.ProviderSpecific["accepted"] == 99 {
		t.Fatal("Clone did not own nested provenance and token-category data")
	}
}

func TestReportRejectsGuaranteeInflationAndUnlinkedClaims(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Report)
	}{
		{"external fully evaluated", func(report *Report) { report.Claims[1].Guarantee = domain.AssuranceLevelFullyEvaluated }},
		{"external boundary omitted", func(report *Report) { report.Claims[1].Limitations = nil }},
		{"strong claim missing evidence", func(report *Report) { report.Claims[0].EvidenceIDs = nil }},
		{"claim validation absent from report", func(report *Report) { report.Claims[0].ValidationRunIDs = []string{mustValidationRunIDString(t)} }},
		{"evaluated claim cites unavailable run", func(report *Report) {
			unavailableRunID := mustValidationRunIDString(t)
			report.Validations[1].ValidationRunID = unavailableRunID
			report.Validations[1].CommandDigest = strings.Repeat("a", 64)
			report.Claims[1].ValidationRunIDs = []string{unavailableRunID}
		}},
		{"claim without provenance", func(report *Report) {
			report.Claims[1].Guarantee = domain.AssuranceLevelRuntimeOnly
			report.Claims[1].ValidationRunIDs = nil
			report.Claims[1].GraphNodeIDs = nil
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			report := validReport(t)
			test.mutate(&report)
			if err := report.Validate(); !errors.Is(err, ErrInvalidReport) {
				t.Fatalf("Validate() error = %v, want ErrInvalidReport", err)
			}
		})
	}
}

func TestReportRejectsIncompleteOrInexactValidationAndVersionEvidence(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Report)
	}{
		{"non-passed reason omitted", func(report *Report) { report.Validations[1].StatusReason = "" }},
		{"validation bound to stale diff", func(report *Report) { report.Validations[0].DiffIdentity = strings.Repeat("1", 64) }},
		{"duplicate validation run", func(report *Report) { report.Validations[1].ValidationRunID = report.Validations[0].ValidationRunID }},
		{"fabricated validation run identity", func(report *Report) { report.Validations[0].ValidationRunID = "run-go-test" }},
		{"policy version omitted", func(report *Report) { report.Versions = report.Versions[:3] }},
		{"unknown version without reason", func(report *Report) {
			report.Versions[0].Known = false
			report.Versions[0].Version = ""
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			report := validReport(t)
			test.mutate(&report)
			if err := report.Validate(); !errors.Is(err, ErrInvalidReport) {
				t.Fatalf("Validate() error = %v, want ErrInvalidReport", err)
			}
		})
	}
}

func TestReportPreservesUnknownMetricsAndSQLiteIntegerBounds(t *testing.T) {
	report := validReport(t)
	report.Metrics.ForecastCostKnown = false
	report.Metrics.ForecastCostP50 = domain.Money{}
	report.Metrics.ForecastCostP90 = domain.Money{}
	report.Metrics.ForecastCostUnknownReason = "Forecast pricing was unavailable from the provider."
	if err := report.Validate(); err != nil {
		t.Fatal(err)
	}

	report.Metrics.ForecastCostP50 = domain.Money{Currency: "USD", MinorUnits: 0}
	if err := report.Validate(); !errors.Is(err, ErrInvalidReport) {
		t.Fatalf("unknown cost with value error = %v", err)
	}

	report = validReport(t)
	report.Metrics.ForecastTokensP90 = uint64(math.MaxInt64) + 1
	if err := report.Validate(); !errors.Is(err, ErrInvalidReport) {
		t.Fatalf("unstorable forecast count error = %v", err)
	}

	report = validReport(t)
	report.ChangedFiles[0].Insertions = uint64(math.MaxInt64) + 1
	if err := report.Validate(); !errors.Is(err, ErrInvalidReport) {
		t.Fatalf("unstorable line count error = %v", err)
	}
}

func validReport(t *testing.T) Report {
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
	evidenceID := mustEvidenceID(t)
	passedRunID := mustValidationRunIDString(t)
	usd, err := domain.ParseCurrencyCode("USD")
	if err != nil {
		t.Fatal(err)
	}
	diff := strings.Repeat("d", 64)
	return Report{
		ID: strings.Repeat("e", 64), TaskID: taskID, RequirementRevision: 3,
		AcceptedPlanRevision: 2, PlanApprovalID: approvalID,
		BaseRevision: strings.Repeat("b", 40), DiffIdentity: diff,
		RiskClassificationRevision: 4, Risk: domain.RiskLevelElevated,
		RiskExplanation: "Elevated because the change crosses a durable storage boundary.",
		GraphRevisionID: graphRevisionID,
		Metrics: ForecastActual{
			ForecastDurationKnown: true, ForecastP50: 2 * time.Minute, ForecastP90: 5 * time.Minute,
			ForecastTokensKnown: true, ForecastTokensP50: 1000, ForecastTokensP90: 2500,
			ForecastCostKnown:   true,
			ForecastCostP50:     domain.Money{Currency: usd, MinorUnits: 5},
			ForecastCostP90:     domain.Money{Currency: usd, MinorUnits: 12},
			ActualDurationKnown: true, ActualDuration: 3 * time.Minute,
			ActualTokens:    domain.TokenUsage{Known: true, Input: 800, CachedInput: 100, Output: 250, Reasoning: 75, ProviderSpecific: map[string]domain.TokenCount{"accepted": 1225}},
			ActualCostKnown: true, ActualCost: domain.Money{Currency: usd, MinorUnits: 8},
		},
		ChangedFiles: []ChangedFile{{Path: "internal/evidence/report.go", Status: FileModified, Insertions: 20, Deletions: 2}},
		Validations: []ValidationCheck{
			{CheckID: "go-test", ValidationRunID: passedRunID, Required: true, Status: ValidationPassed, Summary: "Focused tests passed.", CommandDigest: strings.Repeat("c", 64), DiffIdentity: diff},
			{CheckID: "external-smoke", Required: false, Status: ValidationUnavailable, Summary: "External endpoint was not reachable.", StatusReason: "No external credential was authorized.", DiffIdentity: diff},
		},
		Approvals: []ApprovalUse{{ApprovalID: approvalID, State: domain.ApprovalRequestStateGranted, Scope: "Approve exact plan revision 2.", AuthorityUsed: "user plan approval"}},
		Versions: []VersionBinding{
			{Kind: VersionModel, Name: "coding-model", Known: true, Version: "gpt-5"},
			{Kind: VersionProvider, Name: "OpenAI", Known: true, Version: "responses-v1"},
			{Kind: VersionTool, Name: "go", Known: true, Version: "go1.25"},
			{Kind: VersionPolicy, Name: "risk-classification", Known: true, Version: "v1"},
		},
		Assumptions: []string{"The checked-out repository is the accepted execution scope."},
		Limitations: []string{"No production deployment was performed."},
		Claims: []Claim{
			{ID: "structured-round-trip", Statement: "The final evidence report round-trips as structured data.", Scope: "SQLite evidence-report tables", Boundary: BoundaryInternal, Guarantee: domain.AssuranceLevelFullyEvaluated, GuaranteeReason: "A focused repository test evaluates the exact final diff.", EvidenceIDs: []domain.EvidenceID{evidenceID}, ValidationRunIDs: []string{passedRunID}, GraphNodeIDs: []domain.NodeID{nodeID}},
			{ID: "external-contract", Statement: "The external provider contract remains compatible.", Scope: "provider API boundary", Boundary: BoundaryExternal, Guarantee: domain.AssuranceLevelContractChecked, GuaranteeReason: "The local contract shape is checked; provider runtime behavior is not evaluated.", ValidationRunIDs: []string{passedRunID}, GraphNodeIDs: []domain.NodeID{nodeID}, Limitations: []string{"External provider behavior was not executed."}},
		},
		IdempotencyKey: "final-report", CreatedAt: time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC),
	}
}

func mustEvidenceID(t *testing.T) domain.EvidenceID {
	t.Helper()
	id, err := domain.NewEvidenceID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustValidationRunIDString(t *testing.T) string {
	t.Helper()
	id, err := domain.NewValidationID()
	if err != nil {
		t.Fatal(err)
	}
	return id.String()
}
