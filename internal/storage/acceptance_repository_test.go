package storage

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/acceptance"
	"codeflux.dev/codeflux/internal/domain"
	reportevidence "codeflux.dev/codeflux/internal/evidence"
)

func TestAcceptancePersistsExactReportDiffAndAcknowledgements(t *testing.T) {
	fixture, report, review, _ := createAcceptanceReviewFixture(t, 37_000)
	input := RecordAcceptance{
		ID: acceptance.AcceptanceID(strings.Repeat("4", 64)), TaskID: fixture.task.ID,
		ReviewID: review.ID, ExpectedReviewRevision: review.Revision,
		ExpectedReportID: report.ID, ExpectedDiffIdentity: report.DiffIdentity,
		ActorReference: "user:fixture", AuthorityReference: "review:explicit-user-authority",
		ReasonRedacted: "Accepted after reviewing the exact report and diff.", IdempotencyKey: "acceptance-decision",
	}
	if _, err := fixture.repositories.RecordAcceptance(t.Context(), input); !errors.Is(err, acceptance.ErrAcknowledgementNeeded) {
		t.Fatalf("missing acknowledgement error = %v", err)
	}
	assertAcceptanceRows(t, fixture.repositories, input.ID, 0)

	input.AcknowledgedCheckIDs = []string{"check-waived", "check-failed"}
	decision, err := fixture.repositories.RecordAcceptance(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if decision.ReportID != report.ID || decision.DiffIdentity != report.DiffIdentity || decision.ReviewRevision != review.Revision ||
		!reflect.DeepEqual(decision.AcknowledgedCheckIDs, []string{"check-failed", "check-waived"}) {
		t.Fatalf("acceptance decision = %#v", decision)
	}
	replayed, err := fixture.repositories.RecordAcceptance(t.Context(), input)
	if err != nil || !reflect.DeepEqual(replayed, decision) {
		t.Fatalf("acceptance replay = %#v, %v", replayed, err)
	}

	changed := input
	changed.ReasonRedacted = "Changed retry content."
	if _, err := fixture.repositories.RecordAcceptance(t.Context(), changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed acceptance retry error = %v", err)
	}
	if _, err := fixture.repositories.database.sql.ExecContext(t.Context(), `DELETE FROM acceptance_decisions WHERE id = ?`, input.ID); err == nil {
		t.Fatal("immutable acceptance decision delete succeeded")
	}
}

func TestAcceptanceDetectsChangedDiffAndRequiresRenewedReview(t *testing.T) {
	fixture, report, review, observer := createAcceptanceReviewFixture(t, 37_100)
	input := acceptanceInput(review, report, "stale-acceptance")
	observer.observation.DiffIdentity = strings.Repeat("8", 64)
	if _, err := fixture.repositories.RecordAcceptance(t.Context(), input); !errors.Is(err, acceptance.ErrStaleReview) || !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("changed observed diff error = %v", err)
	}
	assertAcceptanceRows(t, fixture.repositories, input.ID, 0)

	newReport := report.Clone()
	newReport.ID = strings.Repeat("5", 64)
	newReport.DiffIdentity = strings.Repeat("6", 64)
	newReport.IdempotencyKey = "renewed-final-report"
	newReport.CreatedAt = report.CreatedAt
	for index := range newReport.Validations {
		newReport.Validations[index].DiffIdentity = newReport.DiffIdentity
	}
	if _, err := fixture.repositories.RecordFinalEvidenceReport(t.Context(), newReport); err != nil {
		t.Fatal(err)
	}
	observer.observation = observationFromReport(newReport)

	oldInput := acceptanceInput(review, report, "old-review-after-new-report")
	if _, err := fixture.repositories.RecordAcceptance(t.Context(), oldInput); !errors.Is(err, acceptance.ErrStaleReview) {
		t.Fatalf("old review after new report error = %v", err)
	}
	renewedID := acceptance.ReviewID(strings.Repeat("7", 64))
	renewed, err := fixture.repositories.OpenAcceptanceReview(t.Context(), OpenAcceptanceReview{
		ID: renewedID, TaskID: fixture.task.ID, ReportID: newReport.ID,
		ExpectedReviewRevision: review.Revision,
		OpenedBy:               "user:fixture", IdempotencyKey: "renewed-review",
	})
	if err != nil {
		t.Fatal(err)
	}
	if renewed.Revision != review.Revision+1 || renewed.ReportID != newReport.ID || renewed.DiffIdentity != newReport.DiffIdentity {
		t.Fatalf("renewed review = %#v", renewed)
	}
	renewedInput := acceptanceInput(renewed, newReport, "renewed-acceptance")
	if _, err := fixture.repositories.RecordAcceptance(t.Context(), renewedInput); err != nil {
		t.Fatal(err)
	}
}

func TestAcceptanceUsesAuthoritativeLiveValidationInsteadOfSealedTerminalRows(t *testing.T) {
	fixture, report, review, observer := createAcceptanceReviewFixture(t, 37_150)
	for index := range observer.observation.RequiredChecks {
		if observer.observation.RequiredChecks[index].ID == "check-passed" {
			observer.observation.RequiredChecks[index].Status = acceptance.CheckRunning
		}
	}
	input := acceptanceInput(review, report, "live-running-acceptance")
	if _, err := fixture.repositories.RecordAcceptance(t.Context(), input); !errors.Is(err, acceptance.ErrValidationRunning) {
		t.Fatalf("live running validation error = %v", err)
	}
	assertAcceptanceRows(t, fixture.repositories, input.ID, 0)
}

func TestRepairCreatesPlanCheckpointLineageAndRollbackIntentOutcome(t *testing.T) {
	fixture, report, review, _ := createAcceptanceReviewFixture(t, 37_200)
	checkpoint := createReadyAcceptanceCheckpoint(t, fixture, strings.Repeat("a", 64), "pre-repair-checkpoint")
	newPlan := createRepairPlanRevision(t, fixture, 37_290, "repair-plan-revision")
	if newPlan.Revision <= fixture.plan.Revision {
		t.Fatalf("new plan revision = %d", newPlan.Revision)
	}

	request := acceptance.RepairRequest{
		ID: acceptance.RepairRequestID(strings.Repeat("b", 64)), ReviewID: review.ID,
		TaskID: fixture.task.ID, ReviewRevision: review.Revision, ReportID: report.ID, DiffIdentity: report.DiffIdentity,
		PreviousPlanRevision: fixture.plan.Revision, NewPlanRevision: newPlan.Revision,
		PreRepairCheckpointID: checkpoint.ID, Feedback: "Repair the selected failed validation and affected hunk.",
		Targets:     []acceptance.RepairTarget{{Kind: acceptance.RepairTargetValidation, ID: "check-failed"}, {Kind: acceptance.RepairTargetHunk, ID: "hunk-parser-1"}},
		RequestedBy: "user:fixture", IdempotencyKey: "acceptance-repair-request",
	}
	repair, err := fixture.repositories.RequestAcceptanceRepair(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if repair.PreviousPlanRevision != fixture.plan.Revision || repair.NewPlanRevision != newPlan.Revision || repair.PreRepairCheckpointID != checkpoint.ID || !reflect.DeepEqual(repair.Targets, request.Targets) {
		t.Fatalf("repair lineage = %#v", repair)
	}
	if _, err := fixture.repositories.RecordAcceptance(t.Context(), acceptanceInput(review, report, "accept-resolved-repair")); !errors.Is(err, acceptance.ErrStaleReview) {
		t.Fatalf("resolved repair review acceptance error = %v", err)
	}

	intentInput := PrepareRepairRollback{
		ID: acceptance.RollbackIntentID(strings.Repeat("c", 64)), RepairRequestID: repair.ID,
		TaskID: fixture.task.ID, TargetCheckpointID: checkpoint.ID,
		ExpectedRepairDiffIdentity: strings.Repeat("d", 64), Reason: domain.RollbackReasonUserRequested,
		RequestedBy: "user:fixture", IdempotencyKey: "repair-rollback-intent",
	}
	intent, err := fixture.repositories.PrepareAcceptanceRepairRollback(t.Context(), intentInput)
	if err != nil {
		t.Fatal(err)
	}
	if intent.TargetCheckpointID != checkpoint.ID {
		t.Fatalf("rollback intent = %#v", intent)
	}
	wrong := CompleteRepairRollback{IntentID: intent.ID, Outcome: RepairRollbackRestored, ObservedDiffIdentity: strings.Repeat("e", 64), DetailRedacted: "Restore reported the wrong diff.", IdempotencyKey: "rollback-outcome"}
	if _, err := fixture.repositories.CompleteAcceptanceRepairRollback(t.Context(), wrong); !errors.Is(err, ErrConstraint) {
		t.Fatalf("wrong restored diff error = %v", err)
	}
	var outcomes int
	if err := fixture.repositories.database.sql.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM acceptance_repair_rollback_outcomes WHERE rollback_intent_id = ?`, intent.ID).Scan(&outcomes); err != nil {
		t.Fatal(err)
	}
	if outcomes != 0 {
		t.Fatalf("rollback outcome rows after failed transaction = %d", outcomes)
	}
	correct := wrong
	correct.ObservedDiffIdentity = checkpoint.WorktreeDiffHash
	correct.DetailRedacted = "Worktree restored to the exact pre-repair checkpoint diff."
	if _, err := fixture.repositories.CompleteAcceptanceRepairRollback(t.Context(), correct); err != nil {
		t.Fatal(err)
	}
}

func TestRepairRejectsInvalidTargetAtomicallyAndRejectionPreservesPatch(t *testing.T) {
	fixture, report, review, _ := createAcceptanceReviewFixture(t, 37_300)
	checkpoint := createReadyAcceptanceCheckpoint(t, fixture, strings.Repeat("1", 64), "repair-atomic-checkpoint")
	newPlan := createRepairPlanRevision(t, fixture, 37_390, "repair-atomic-plan")
	badRepair := acceptance.RepairRequest{
		ID: acceptance.RepairRequestID(strings.Repeat("2", 64)), ReviewID: review.ID, TaskID: fixture.task.ID,
		ReviewRevision: review.Revision, ReportID: report.ID, DiffIdentity: report.DiffIdentity,
		PreviousPlanRevision: fixture.plan.Revision, NewPlanRevision: newPlan.Revision, PreRepairCheckpointID: checkpoint.ID,
		Feedback:    "This target is passed and cannot be selected as a failure.",
		Targets:     []acceptance.RepairTarget{{Kind: acceptance.RepairTargetValidation, ID: "check-passed"}},
		RequestedBy: "user:fixture", IdempotencyKey: "bad-repair-target",
	}
	if _, err := fixture.repositories.RequestAcceptanceRepair(t.Context(), badRepair); !errors.Is(err, ErrConstraint) {
		t.Fatalf("bad repair target error = %v", err)
	}
	var repairRows, targetRows int
	if err := fixture.repositories.database.sql.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM acceptance_repair_requests WHERE id = ?`, badRepair.ID).Scan(&repairRows); err != nil {
		t.Fatal(err)
	}
	if err := fixture.repositories.database.sql.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM acceptance_repair_targets WHERE repair_request_id = ?`, badRepair.ID).Scan(&targetRows); err != nil {
		t.Fatal(err)
	}
	if repairRows != 0 || targetRows != 0 {
		t.Fatalf("partial repair rows = %d/%d", repairRows, targetRows)
	}

	rejectionInput := RecordReviewRejection{
		ID: acceptance.RejectionID(strings.Repeat("3", 64)), TaskID: fixture.task.ID, ReviewID: review.ID,
		ExpectedReviewRevision: review.Revision, ExpectedReportID: report.ID, ExpectedDiffIdentity: report.DiffIdentity,
		ReasonRedacted: "Reject this candidate while preserving it for inspection.", RejectedBy: "user:fixture", IdempotencyKey: "preserve-rejection",
	}
	if _, err := fixture.repositories.RejectAcceptanceReview(t.Context(), rejectionInput); err == nil {
		t.Fatal("destructive rejection without preserve_patch succeeded")
	}
	rejectionInput.PreservePatch = true
	rejection, err := fixture.repositories.RejectAcceptanceReview(t.Context(), rejectionInput)
	if err != nil {
		t.Fatal(err)
	}
	if !rejection.PreservePatch || rejection.ReportID != report.ID || rejection.DiffIdentity != report.DiffIdentity {
		t.Fatalf("rejection = %#v", rejection)
	}
	var reportRows, checkpointRows int
	if err := fixture.repositories.database.sql.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM final_evidence_reports WHERE id = ?`, report.ID).Scan(&reportRows); err != nil {
		t.Fatal(err)
	}
	if err := fixture.repositories.database.sql.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM checkpoints WHERE id = ?`, checkpoint.ID).Scan(&checkpointRows); err != nil {
		t.Fatal(err)
	}
	if reportRows != 1 || checkpointRows != 1 {
		t.Fatalf("preserved report/checkpoint rows = %d/%d", reportRows, checkpointRows)
	}
}

func createAcceptanceReviewFixture(t *testing.T, base int) (graphQueryFixture, reportevidence.Report, acceptance.Review, *acceptanceObserverStub) {
	t.Helper()
	fixture := createGraphQueryFixture(t, base)
	report := createFinalEvidenceReportFixture(t, fixture)
	for index := range report.Validations {
		report.Validations[index].ValidationRunID = ""
	}
	for claimIndex := range report.Claims {
		report.Claims[claimIndex].ValidationRunIDs = nil
		report.Claims[claimIndex].Guarantee = domain.AssuranceLevelRuntimeOnly
		report.Claims[claimIndex].GuaranteeReason = "This fixture exercises acceptance gating without claiming executed validation provenance."
	}
	for index := range report.Validations {
		if report.Validations[index].CheckID == "check-waived" {
			report.Validations[index].Required = true
		}
	}
	if _, err := fixture.repositories.RecordFinalEvidenceReport(t.Context(), report); err != nil {
		t.Fatal(err)
	}
	observer := &acceptanceObserverStub{observation: observationFromReport(report)}
	fixture.repositories.SetAcceptanceObservationSource(observer)
	review, err := fixture.repositories.OpenAcceptanceReview(t.Context(), OpenAcceptanceReview{
		ID: acceptance.ReviewID(strings.Repeat("1", 64)), TaskID: fixture.task.ID,
		ReportID: report.ID,
		OpenedBy: "user:fixture", IdempotencyKey: "acceptance-review",
	})
	if err != nil {
		t.Fatal(err)
	}
	return fixture, report, review, observer
}

type acceptanceObserverStub struct {
	observation AcceptanceObservation
	err         error
}

func (stub *acceptanceObserverStub) ObserveAcceptance(context.Context, domain.TaskID) (AcceptanceObservation, error) {
	return stub.observation, stub.err
}

func observationFromReport(report reportevidence.Report) AcceptanceObservation {
	checks := make([]acceptance.RequiredCheck, 0, len(report.Validations))
	for _, check := range report.Validations {
		if check.Required {
			checks = append(checks, acceptance.RequiredCheck{ID: check.CheckID, Status: acceptance.CheckStatus(check.Status)})
		}
	}
	return AcceptanceObservation{ReportID: report.ID, DiffIdentity: report.DiffIdentity, RequiredChecks: checks}
}

func createRepairPlanRevision(
	t *testing.T,
	fixture graphQueryFixture,
	messageBase int,
	idempotencyKey string,
) PlanRevision {
	t.Helper()
	message, err := fixture.repositories.AppendMessage(t.Context(), AppendMessage{
		ID:             testMessageID(t, messageBase),
		ThreadID:       fixture.task.ThreadID,
		Role:           MessageRoleUser,
		BodyRedacted:   "Repair only the selected review failures and hunks.",
		IdempotencyKey: idempotencyKey + "-message",
	})
	if err != nil {
		t.Fatal(err)
	}
	previous := fixture.plan.Revision
	input := fixture.planInput
	redirectedPlan := fixture.plan.Plan
	extraCommands, err := canonicalRequirementValidationCommands(
		[]string{"go test ./internal/acceptance"},
	)
	if err != nil {
		t.Fatal(err)
	}
	redirectedPlan.ValidationCommands = normalizedStrings(append(
		redirectedPlan.ValidationCommands,
		extraCommands...,
	))
	input.Plan = redirectedPlan
	input.SupersedesRevision = &previous
	input.RedirectMessageID = &message.ID
	input.IdempotencyKey = idempotencyKey
	plan, err := fixture.repositories.RecordPlanRevision(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func acceptanceInput(review acceptance.Review, report reportevidence.Report, key string) RecordAcceptance {
	return RecordAcceptance{
		ID: acceptance.AcceptanceID(strings.Repeat("9", 64)), TaskID: review.TaskID,
		ReviewID: review.ID, ExpectedReviewRevision: review.Revision,
		ExpectedReportID: report.ID, ExpectedDiffIdentity: report.DiffIdentity,
		AcknowledgedCheckIDs: []string{"check-failed", "check-waived"},
		ActorReference:       "user:fixture", AuthorityReference: "review:explicit-user-authority",
		ReasonRedacted: "Accepted with exact required-check acknowledgements.", IdempotencyKey: key,
	}
}

func createReadyAcceptanceCheckpoint(t *testing.T, fixture graphQueryFixture, diff, key string) Checkpoint {
	t.Helper()
	id, err := domain.NewCheckpointID()
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := fixture.repositories.CreateCheckpoint(t.Context(), CreateCheckpoint{
		ID: id, TaskID: fixture.task.ID, State: domain.CheckpointStateReady,
		RepositoryRevision: strings.Repeat("f", 40), WorktreeDiffHash: diff,
		EventSequence: 0, IdempotencyKey: key,
	})
	if err != nil {
		t.Fatal(err)
	}
	return checkpoint
}

func assertAcceptanceRows(t *testing.T, repositories *Repositories, id acceptance.AcceptanceID, want int) {
	t.Helper()
	var count int
	if err := repositories.database.sql.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM acceptance_decisions WHERE id = ?`, id).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("acceptance decision rows = %d, want %d", count, want)
	}
}
