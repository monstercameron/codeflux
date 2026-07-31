package storage

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	reportevidence "codeflux.dev/codeflux/internal/evidence"
	"codeflux.dev/codeflux/internal/review"
	"codeflux.dev/codeflux/internal/validation"
)

func TestFinalEvidenceReportRoundTripsStructuredClaimProvenance(t *testing.T) {
	fixture := createGraphQueryFixture(t, 36_000)
	report := createFinalEvidenceReportFixture(t, fixture)

	stored, err := fixture.repositories.RecordFinalEvidenceReport(t.Context(), report)
	if err != nil {
		t.Fatal(err)
	}
	if stored.CreatedAt.IsZero() || stored.CreatedAt.Location() != time.UTC {
		t.Fatalf("repository creation time = %v", stored.CreatedAt)
	}
	loaded, err := fixture.repositories.GetFinalEvidenceReport(t.Context(), fixture.task.ID, report.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded, stored) {
		t.Fatalf("loaded report differs\nloaded: %#v\nstored: %#v", loaded, stored)
	}
	if len(loaded.Validations) != 7 || len(loaded.Claims) != 2 || len(loaded.Claims[0].EvidenceIDs) != 1 || len(loaded.Claims[0].GraphNodeIDs) != 1 {
		t.Fatalf("structured provenance was incomplete: %#v", loaded)
	}

	replayed, err := fixture.repositories.RecordFinalEvidenceReport(t.Context(), report)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(replayed, stored) {
		t.Fatalf("idempotent replay = %#v, want %#v", replayed, stored)
	}

	conflict := report
	conflict.ID = strings.Repeat("f", 64)
	if _, err := fixture.repositories.RecordFinalEvidenceReport(t.Context(), conflict); !errors.Is(err, ErrConflict) {
		t.Fatalf("idempotency conflict error = %v", err)
	}
	otherTaskID, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repositories.GetFinalEvidenceReport(t.Context(), otherTaskID, report.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-task lookup error = %v", err)
	}

	unknown := report
	unknown.ID = strings.Repeat("9", 64)
	unknown.IdempotencyKey = "final-evidence-report-unknown-metrics"
	unknown.Metrics = reportevidence.ForecastActual{
		ForecastDurationUnknownReason: "No duration forecast was recorded.",
		ForecastTokensUnknownReason:   "No token forecast was recorded.",
		ForecastCostUnknownReason:     "No pricing forecast was recorded.",
		ActualDurationUnknownReason:   "No execution duration was observed.",
		ActualTokensUnknownReason:     "Provider usage was unavailable.",
		ActualCostUnknownReason:       "Provider pricing was unavailable.",
	}
	unknownStored, err := fixture.repositories.RecordFinalEvidenceReport(t.Context(), unknown)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(unknownStored.Metrics, unknown.Metrics) {
		t.Fatalf("unknown metric reasons = %#v, want %#v", unknownStored.Metrics, unknown.Metrics)
	}
}

func TestFinalEvidenceReportRejectsInconsistentSnapshotsAndRollsBack(t *testing.T) {
	fixture := createGraphQueryFixture(t, 36_100)
	report := createFinalEvidenceReportFixture(t, fixture)
	originalRisk := report.Risk
	report.Risk = domain.RiskLevelProtected
	if _, err := fixture.repositories.RecordFinalEvidenceReport(t.Context(), report); !errors.Is(err, ErrConstraint) {
		t.Fatalf("inconsistent risk snapshot error = %v", err)
	}
	assertEvidenceReportCount(t, fixture.repositories, report.ID, 0)

	report.Risk = originalRisk
	var sourceValidationID domain.ValidationID
	if err := fixture.repositories.database.sql.QueryRowContext(t.Context(), `SELECT validation_id FROM evidence WHERE id = ?`, report.Claims[0].EvidenceIDs[0]).Scan(&sourceValidationID); err != nil {
		t.Fatal(err)
	}
	weakEvidenceID, err := domain.NewEvidenceID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repositories.CreateEvidence(t.Context(), CreateEvidence{
		ID: weakEvidenceID, ValidationID: sourceValidationID, TaskID: fixture.task.ID,
		AssuranceLevel: domain.AssuranceLevelRuntimeOnly, EvidenceType: "runtime-observation",
		ContentHash: strings.Repeat("4", 64), SummaryRedacted: "Runtime-only evidence cannot support a fully-evaluated claim.",
	}); err != nil {
		t.Fatal(err)
	}
	report.Claims[0].EvidenceIDs = []domain.EvidenceID{weakEvidenceID}
	if _, err := fixture.repositories.RecordFinalEvidenceReport(t.Context(), report); !errors.Is(err, ErrConstraint) {
		t.Fatalf("inflated claim evidence error = %v", err)
	}
	assertEvidenceReportCount(t, fixture.repositories, report.ID, 0)

	report.Risk = originalRisk
	report.ID = strings.Repeat("a", 64)
	report.IdempotencyKey = "missing-evidence"
	missingEvidenceID, err := domain.NewEvidenceID()
	if err != nil {
		t.Fatal(err)
	}
	report.Claims[0].EvidenceIDs = []domain.EvidenceID{missingEvidenceID}
	if _, err := fixture.repositories.RecordFinalEvidenceReport(t.Context(), report); !errors.Is(err, ErrConstraint) {
		t.Fatalf("missing claim evidence error = %v", err)
	}
	assertEvidenceReportCount(t, fixture.repositories, report.ID, 0)
}

func TestFinalEvidenceReportRejectsFabricatedOrMismatchedValidationRuns(t *testing.T) {
	t.Run("nonexistent validation run", func(t *testing.T) {
		fixture := createGraphQueryFixture(t, 36_300)
		report := createFinalEvidenceReportFixture(t, fixture)
		report.ID = strings.Repeat("a", 64)
		report.IdempotencyKey = "nonexistent-validation-run"
		missingID := testValidationID(t, 36_399).String()
		report.Validations[0].ValidationRunID = missingID
		for index := range report.Claims {
			report.Claims[index].ValidationRunIDs = []string{missingID}
		}
		if _, err := fixture.repositories.RecordFinalEvidenceReport(t.Context(), report); !errors.Is(err, ErrConstraint) {
			t.Fatalf("nonexistent validation run error = %v", err)
		}
		assertEvidenceReportCount(t, fixture.repositories, report.ID, 0)
	})

	t.Run("validation run belongs to another check", func(t *testing.T) {
		fixture := createGraphQueryFixture(t, 36_400)
		report := createFinalEvidenceReportFixture(t, fixture)
		report.ID = strings.Repeat("b", 64)
		report.IdempotencyKey = "mismatched-validation-run"
		var existingRunID domain.RunID
		if err := fixture.repositories.database.sql.QueryRowContext(t.Context(),
			`SELECT run_id FROM validation_run_intents WHERE id = ?`, report.Validations[0].ValidationRunID,
		).Scan(&existingRunID); err != nil {
			t.Fatal(err)
		}
		mismatched := createFinalEvidenceValidationIntent(t, fixture, existingRunID, 36_499, "different-check", report.DiffIdentity, true)
		report.Validations[0].ValidationRunID = mismatched.ID.String()
		report.Validations[0].CommandDigest = mismatched.CommandFingerprint
		for index := range report.Claims {
			report.Claims[index].ValidationRunIDs = []string{mismatched.ID.String()}
		}
		if _, err := fixture.repositories.RecordFinalEvidenceReport(t.Context(), report); !errors.Is(err, ErrConstraint) {
			t.Fatalf("mismatched validation run error = %v", err)
		}
		assertEvidenceReportCount(t, fixture.repositories, report.ID, 0)
	})

	t.Run("reported status disagrees with authoritative result", func(t *testing.T) {
		fixture := createGraphQueryFixture(t, 36_600)
		report := createFinalEvidenceReportFixture(t, fixture)
		report.ID = strings.Repeat("c", 64)
		report.IdempotencyKey = "mismatched-validation-status"
		report.Validations[0].Status = reportevidence.ValidationFailed
		report.Validations[0].StatusReason = "Fixture reports a failure for a passed authoritative result."
		for index := range report.Claims {
			report.Claims[index].Guarantee = domain.AssuranceLevelRuntimeOnly
		}
		if _, err := fixture.repositories.RecordFinalEvidenceReport(t.Context(), report); !errors.Is(err, ErrConstraint) {
			t.Fatalf("mismatched validation status error = %v", err)
		}
		assertEvidenceReportCount(t, fixture.repositories, report.ID, 0)
	})
}

func TestFinalEvidenceReportRejectsInvalidatedRunForStrongClaim(t *testing.T) {
	fixture := createGraphQueryFixture(t, 36_500)
	report := createFinalEvidenceReportFixture(t, fixture)
	var runID domain.RunID
	if err := fixture.repositories.database.sql.QueryRowContext(t.Context(),
		`SELECT run_id FROM validation_run_intents WHERE id = ?`, report.Validations[0].ValidationRunID,
	).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	invalidations, err := fixture.repositories.InvalidateValidationRuns(t.Context(), runID, strings.Repeat("f", 64))
	if err != nil {
		t.Fatal(err)
	}
	if len(invalidations) != 1 {
		t.Fatalf("invalidations = %#v, want one", invalidations)
	}
	if _, err := fixture.repositories.RecordFinalEvidenceReport(t.Context(), report); !errors.Is(err, ErrConstraint) {
		t.Fatalf("invalidated strong-claim validation error = %v", err)
	}
	assertEvidenceReportCount(t, fixture.repositories, report.ID, 0)
}

func TestFinalEvidenceReportIsSealedAgainstEveryLaterMutation(t *testing.T) {
	fixture := createGraphQueryFixture(t, 36_200)
	report := createFinalEvidenceReportFixture(t, fixture)
	if _, err := fixture.repositories.RecordFinalEvidenceReport(t.Context(), report); err != nil {
		t.Fatal(err)
	}

	statements := []struct {
		name  string
		query string
		args  []any
	}{
		{"root update", `UPDATE final_evidence_reports SET risk_explanation = ? WHERE id = ?`, []any{"rewritten", report.ID}},
		{"child update", `UPDATE final_evidence_report_claims SET guarantee_reason_redacted = ? WHERE report_id = ?`, []any{"rewritten", report.ID}},
		{"child delete", `DELETE FROM final_evidence_report_versions WHERE report_id = ?`, []any{report.ID}},
		{"post-seal append", `INSERT INTO final_evidence_report_changed_files (report_id, ordinal, repository_relative_path, status, insertions, deletions, generated) VALUES (?, 100, 'late.go', 'added', 1, 0, 0)`, []any{report.ID}},
		{"seal delete", `DELETE FROM final_evidence_report_seals WHERE report_id = ?`, []any{report.ID}},
	}
	for _, statement := range statements {
		t.Run(statement.name, func(t *testing.T) {
			if _, err := fixture.repositories.database.sql.ExecContext(t.Context(), statement.query, statement.args...); err == nil {
				t.Fatal("mutation unexpectedly succeeded")
			}
		})
	}
}

func createFinalEvidenceReportFixture(t *testing.T, fixture graphQueryFixture) reportevidence.Report {
	t.Helper()
	ctx := t.Context()
	risk, err := fixture.repositories.RecordInitialTaskRiskClassification(ctx, fixture.task.ID, []review.RiskSignal{review.RiskSignalNarrowScopedChange}, "")
	if err != nil {
		t.Fatal(err)
	}
	approvalID, err := domain.NewApprovalID()
	if err != nil {
		t.Fatal(err)
	}
	scope := PlanApprovalScope(fixture.plan)
	approval, err := fixture.repositories.CreateApproval(ctx, CreateApproval{
		ID: approvalID, TaskID: fixture.task.ID, Scope: scope,
		RequestReason: "final evidence fixture exact plan approval", IdempotencyKey: "final-evidence-plan-approval",
	})
	if err != nil {
		t.Fatal(err)
	}
	approval, err = fixture.repositories.ResolveApproval(ctx, ResolveApproval{
		ID: approval.ID, ExpectedRevision: approval.Revision, To: domain.ApprovalRequestStateGranted,
		ResolutionReason: "explicitly approved for final evidence fixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repositories.ApprovePlanRevision(ctx, ApprovePlanRevision{
		TaskID: fixture.task.ID, PlanRevision: fixture.plan.Revision, ApprovalID: approval.ID,
		IdempotencyKey: "final-evidence-plan-binding",
	}); err != nil {
		t.Fatal(err)
	}
	validationID, err := domain.NewValidationID()
	if err != nil {
		t.Fatal(err)
	}
	validation, err := fixture.repositories.CreateValidation(ctx, CreateValidation{
		ID: validationID, TaskID: fixture.task.ID, State: domain.ValidationStatePassed,
		Severity: domain.ValidationSeverityBlocking, ProfileName: "final-evidence-focused",
	})
	if err != nil {
		t.Fatal(err)
	}
	evidenceID, err := domain.NewEvidenceID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repositories.CreateEvidence(ctx, CreateEvidence{
		ID: evidenceID, ValidationID: validation.ID, TaskID: fixture.task.ID,
		AssuranceLevel: domain.AssuranceLevelFullyEvaluated, EvidenceType: "focused-test",
		ContentHash: strings.Repeat("7", 64), SummaryRedacted: "Focused structured persistence test passed.",
	}); err != nil {
		t.Fatal(err)
	}
	usd, err := domain.ParseCurrencyCode("USD")
	if err != nil {
		t.Fatal(err)
	}
	diff := strings.Repeat("d", 64)
	passedRun := createFinalEvidenceValidationRun(t, fixture, 36_900, "check-passed", diff, true)
	statuses := []reportevidence.ValidationStatus{
		reportevidence.ValidationPassed, reportevidence.ValidationFailed, reportevidence.ValidationWaived,
		reportevidence.ValidationSkipped, reportevidence.ValidationUnavailable,
		reportevidence.ValidationCancelled, reportevidence.ValidationInvalidated,
	}
	checks := make([]reportevidence.ValidationCheck, 0, len(statuses))
	for index, status := range statuses {
		check := reportevidence.ValidationCheck{
			CheckID: "check-" + string(status), Required: index < 2, Status: status,
			Summary: "Final state: " + string(status) + ".", DiffIdentity: diff,
		}
		if status == reportevidence.ValidationPassed {
			check.ValidationRunID = passedRun.ID.String()
			check.CommandDigest = passedRun.CommandFingerprint
		} else {
			check.StatusReason = "Recorded exact non-passed disposition: " + string(status) + "."
		}
		checks = append(checks, check)
	}
	return reportevidence.Report{
		ID: strings.Repeat("e", 64), TaskID: fixture.task.ID,
		RequirementRevision: fixture.agentPlanFixture.requirement.Revision, AcceptedPlanRevision: fixture.plan.Revision,
		PlanApprovalID: approval.ID, BaseRevision: strings.Repeat("8", 40), DiffIdentity: diff,
		RiskClassificationRevision: risk.Revision, Risk: risk.Classification.SelectedRisk(),
		RiskExplanation: risk.Classification.Explanation(), GraphRevisionID: fixture.revisionID,
		Metrics: reportevidence.ForecastActual{
			ForecastDurationKnown: true, ForecastP50: time.Minute + 123*time.Nanosecond, ForecastP90: 4*time.Minute + 456*time.Nanosecond,
			ForecastTokensKnown: true, ForecastTokensP50: 1000, ForecastTokensP90: 3000,
			ForecastCostKnown: true, ForecastCostP50: domain.Money{Currency: usd, MinorUnits: 4}, ForecastCostP90: domain.Money{Currency: usd, MinorUnits: 15},
			ActualDurationKnown: true, ActualDuration: 2*time.Minute + 789*time.Nanosecond,
			ActualTokens:    domain.TokenUsage{Known: true, Input: 600, CachedInput: 50, Output: 200, Reasoning: 100, ProviderSpecific: map[string]domain.TokenCount{"accepted": 950}},
			ActualCostKnown: true, ActualCost: domain.Money{Currency: usd, MinorUnits: 9},
		},
		ChangedFiles: []reportevidence.ChangedFile{
			{Path: "internal/evidence/report.go", Status: reportevidence.FileModified, Insertions: 25, Deletions: 2},
			{Path: "web/frontend/evidencereport/component.go", Status: reportevidence.FileAdded, Insertions: 40},
		},
		Validations: checks,
		Approvals:   []reportevidence.ApprovalUse{{ApprovalID: approval.ID, State: approval.State, Scope: approval.Scope, AuthorityUsed: "explicit user plan approval"}},
		Versions: []reportevidence.VersionBinding{
			{Kind: reportevidence.VersionModel, Name: "coding-model", Known: true, Version: "gpt-5"},
			{Kind: reportevidence.VersionProvider, Name: "OpenAI", Known: true, Version: "responses-v1"},
			{Kind: reportevidence.VersionTool, Name: "go", Known: true, Version: "go1.25"},
			{Kind: reportevidence.VersionPolicy, Name: "risk-classification", Known: true, Version: string(risk.Classification.PolicyVersion())},
		},
		Assumptions: []string{"The sealed graph revision represents the accepted task scope."},
		Limitations: []string{"No external provider request was authorized."},
		Claims: []reportevidence.Claim{
			{ID: "structured-storage", Statement: "The final evidence report round-trips from SQLite.", Scope: "final evidence report schema", Boundary: reportevidence.BoundaryInternal, Guarantee: domain.AssuranceLevelFullyEvaluated, GuaranteeReason: "Focused storage tests evaluate the structured rows.", EvidenceIDs: []domain.EvidenceID{evidenceID}, ValidationRunIDs: []string{passedRun.ID.String()}, GraphNodeIDs: []domain.NodeID{fixture.requirement}},
			{ID: "external-contract", Statement: "The provider contract remains compatible.", Scope: "external provider boundary", Boundary: reportevidence.BoundaryExternal, Guarantee: domain.AssuranceLevelContractChecked, GuaranteeReason: "The local contract is checked without claiming provider execution.", ValidationRunIDs: []string{passedRun.ID.String()}, GraphNodeIDs: []domain.NodeID{fixture.obligation}, Limitations: []string{"External provider runtime behavior was not evaluated."}},
		},
		IdempotencyKey: "final-evidence-report",
	}
}

func createFinalEvidenceValidationRun(
	t *testing.T,
	fixture graphQueryFixture,
	number int,
	checkID string,
	diffIdentity string,
	required bool,
) validation.RunIntent {
	t.Helper()
	runID := createToolTestRun(t, fixture.repositories, fixture.task.ID, number)
	return createFinalEvidenceValidationIntent(t, fixture, runID, number, checkID, diffIdentity, required)
}

func createFinalEvidenceValidationIntent(
	t *testing.T,
	fixture graphQueryFixture,
	runID domain.RunID,
	number int,
	checkID string,
	diffIdentity string,
	required bool,
) validation.RunIntent {
	t.Helper()
	intent := validationRunIntentFixture(t, fixture.task.ID, runID, number, diffIdentity, "final-evidence-validation-"+testUUID(number))
	intent.CheckID = checkID
	intent.Required = required
	var err error
	intent, err = validation.SealRunIntent(intent)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repositories.CreateValidationRunIntent(t.Context(), intent); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repositories.CommitValidationRunResult(t.Context(), validationRunResultFixture(t, intent.ID, diffIdentity)); err != nil {
		t.Fatal(err)
	}
	return intent
}

func assertEvidenceReportCount(t *testing.T, repositories *Repositories, reportID string, want int) {
	t.Helper()
	var count int
	if err := repositories.database.sql.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM final_evidence_reports WHERE id = ?`, reportID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("report row count = %d, want %d", count, want)
	}
}
