package storage

import (
	"errors"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
)

func TestTransitionMemoryArtifactMaturityRequiresCorroboratedEvidence(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 1100)
	repositoryID := testRepositoryID(t, 1101)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)
	artifact := createMemoryArtifactFixture(t, repositories, projectID, repositoryID, 1102)
	revision, err := repositories.GetLatestMemoryArtifactRevision(ctx, artifact)
	if err != nil {
		t.Fatal(err)
	}

	// No supporting evidence recorded yet: both the domain authority-proof
	// check and the storage-layer defense-in-depth trigger must reject this.
	if _, err := repositories.TransitionMemoryArtifactMaturity(ctx, TransitionMemoryArtifactMaturity{
		RevisionID: revision.RevisionID, From: domain.MaturityStateCandidate, To: domain.MaturityStateValidated,
		IdempotencyKey: "no-evidence-yet",
	}); err == nil {
		t.Fatal("expected maturity grant without evidence to be rejected")
	}

	// Self-report evidence, even if present, can never authorize.
	selfReportEvidence := createMemoryEvidenceFixture(t, repositories, repositoryID, 1110)
	if err := repositories.RecordMemoryArtifactSupportingEvidence(ctx, revision.RevisionID, domain.SupportingEvidenceRecord{
		Evidence: selfReportEvidence, Source: domain.EvidenceSourceKindAgentSelfReport, Strength: domain.EvidenceStrengthNone,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.TransitionMemoryArtifactMaturity(ctx, TransitionMemoryArtifactMaturity{
		RevisionID: revision.RevisionID, From: domain.MaturityStateCandidate, To: domain.MaturityStateValidated,
		IdempotencyKey: "self-report-only",
	}); err == nil {
		t.Fatal("expected self-report-only evidence to be rejected")
	}

	corroboratingEvidence := createMemoryEvidenceFixture(t, repositories, repositoryID, 1120)
	if err := repositories.RecordMemoryArtifactSupportingEvidence(ctx, revision.RevisionID, domain.SupportingEvidenceRecord{
		Evidence: corroboratingEvidence, Source: domain.EvidenceSourceKindAutomatedValidationRun, Strength: domain.EvidenceStrengthCorroborated,
	}); err != nil {
		t.Fatal(err)
	}
	transition, err := repositories.TransitionMemoryArtifactMaturity(ctx, TransitionMemoryArtifactMaturity{
		RevisionID: revision.RevisionID, From: domain.MaturityStateCandidate, To: domain.MaturityStateValidated,
		IdempotencyKey: "authorized-grant",
	})
	if err != nil {
		t.Fatal(err)
	}
	if transition.To != domain.MaturityStateValidated {
		t.Fatalf("transition = %#v", transition)
	}

	updated, err := repositories.GetMemoryArtifactRevision(ctx, revision.RevisionID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Maturity != domain.MaturityStateValidated {
		t.Fatalf("revision maturity = %q, want validated", updated.Maturity)
	}

	// Idempotent retry with the same key returns the original transition.
	retried, err := repositories.TransitionMemoryArtifactMaturity(ctx, TransitionMemoryArtifactMaturity{
		RevisionID: revision.RevisionID, From: domain.MaturityStateCandidate, To: domain.MaturityStateValidated,
		IdempotencyKey: "authorized-grant",
	})
	if err != nil || retried.ID != transition.ID {
		t.Fatalf("retried transition = %#v, %v", retried, err)
	}
}

func TestTransitionMemoryArtifactMaturityQuarantineIsTerminal(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 1130)
	repositoryID := testRepositoryID(t, 1131)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)
	artifact := createMemoryArtifactFixture(t, repositories, projectID, repositoryID, 1132)
	revision, err := repositories.GetLatestMemoryArtifactRevision(ctx, artifact)
	if err != nil {
		t.Fatal(err)
	}

	// Quarantine, invalidation, and retirement require a reason (M21-021).
	if _, err := repositories.TransitionMemoryArtifactMaturity(ctx, TransitionMemoryArtifactMaturity{
		RevisionID: revision.RevisionID, From: domain.MaturityStateCandidate, To: domain.MaturityStateQuarantined,
		IdempotencyKey: "quarantine-no-reason",
	}); err == nil {
		t.Fatal("expected quarantine without a reason to be rejected")
	}

	counterexample := createMemoryEvidenceFixture(t, repositories, repositoryID, 1140)
	quarantine, err := repositories.TransitionMemoryArtifactMaturity(ctx, TransitionMemoryArtifactMaturity{
		RevisionID: revision.RevisionID, From: domain.MaturityStateCandidate, To: domain.MaturityStateQuarantined,
		ReasonKind: MemoryArtifactInvalidationReasonLessonArmWorse, DetailRedacted: "arm B underperformed arm A across five replays",
		CounterexampleEvidenceID: &counterexample, IdempotencyKey: "quarantine-with-reason",
	})
	if err != nil {
		t.Fatal(err)
	}
	if quarantine.ReasonKind != MemoryArtifactInvalidationReasonLessonArmWorse || quarantine.CounterexampleEvidenceID == nil {
		t.Fatalf("quarantine transition = %#v", quarantine)
	}

	// Per §31, quarantine is terminal: no path exists back to candidate,
	// validated, or preferred-for-experiment. The migration's from/to CHECK
	// only allows quarantined -> invalidated/retired, and the domain
	// transition table agrees.
	if err := domain.ValidateMemoryArtifactMaturityTransition(domain.MaturityTransitionRequest{
		From: domain.MaturityStateQuarantined, To: domain.MaturityStateValidated,
	}); err == nil {
		t.Fatal("expected quarantined -> validated to be rejected by the domain transition table")
	}
	if _, err := repositories.TransitionMemoryArtifactMaturity(ctx, TransitionMemoryArtifactMaturity{
		RevisionID: revision.RevisionID, From: domain.MaturityStateQuarantined, To: domain.MaturityStateCandidate,
		IdempotencyKey: "attempt-restore",
	}); err == nil {
		t.Fatal("expected an attempt to restore a quarantined revision to be rejected")
	}
	quarantinedAfterAttempt, err := repositories.GetMemoryArtifactRevision(ctx, revision.RevisionID)
	if err != nil {
		t.Fatal(err)
	}
	if quarantinedAfterAttempt.Maturity != domain.MaturityStateQuarantined {
		t.Fatalf("maturity after rejected restore attempt = %q, want quarantined", quarantinedAfterAttempt.Maturity)
	}

	retire, err := repositories.TransitionMemoryArtifactMaturity(ctx, TransitionMemoryArtifactMaturity{
		RevisionID: revision.RevisionID, From: domain.MaturityStateQuarantined, To: domain.MaturityStateRetired,
		ReasonKind: MemoryArtifactInvalidationReasonEvidenceAmbiguous, DetailRedacted: "investigation budget exhausted without demonstrating benefit",
		IdempotencyKey: "retire-after-quarantine",
	})
	if err != nil {
		t.Fatal(err)
	}
	if retire.To != domain.MaturityStateRetired {
		t.Fatalf("retire transition = %#v", retire)
	}

	history, err := repositories.ListMemoryArtifactMaturityTransitions(ctx, revision.RevisionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 {
		t.Fatalf("maturity transition history = %#v, want 2 logged transitions", history)
	}
}

func TestTransitionMemoryArtifactMaturityAttemptDirectlyOnDatabaseIsRejected(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 1150)
	repositoryID := testRepositoryID(t, 1151)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)
	artifact := createMemoryArtifactFixture(t, repositories, projectID, repositoryID, 1152)
	revision, err := repositories.GetLatestMemoryArtifactRevision(ctx, artifact)
	if err != nil {
		t.Fatal(err)
	}
	// A raw UPDATE that never logged a matching transition must be rejected
	// by the migration trigger, independent of the Go-level API.
	if _, err := repositories.database.sql.ExecContext(
		ctx, `UPDATE memory_artifact_revisions SET maturity = 'validated' WHERE id = ?`, revision.RevisionID,
	); !errors.Is(classify("bypass memory artifact maturity transition", err), ErrConstraint) {
		t.Fatalf("unlogged maturity mutation error = %v, want constraint", err)
	}
}

// TestTransitionMemoryArtifactMaturityRejectsCrossProjectCounterexampleEvidence
// covers the reviewed defect where a memory_artifact_maturity_transitions
// row for a Project-A revision accepted a counterexample_evidence_id
// pointing at Project-B's evidence with no error -- unlike every other
// table reaching a memory artifact, which enforces its project boundary.
// Both halves of the fix are exercised: the Go-side check in
// TransitionMemoryArtifactMaturity, and the migration's
// memory_artifact_maturity_transitions_counterexample_project_boundary
// trigger reached directly via raw SQL (the exact reproduction the review
// found).
func TestTransitionMemoryArtifactMaturityRejectsCrossProjectCounterexampleEvidence(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectA := testProjectID(t, 1160)
	repositoryA := testRepositoryID(t, 1161)
	mustCreateProjectRepository(t, repositories, projectA, repositoryA)
	projectB := testProjectID(t, 1170)
	repositoryB := testRepositoryID(t, 1171)
	mustCreateProjectRepository(t, repositories, projectB, repositoryB)

	artifact := createMemoryArtifactFixture(t, repositories, projectA, repositoryA, 1180)
	revision, err := repositories.GetLatestMemoryArtifactRevision(ctx, artifact)
	if err != nil {
		t.Fatal(err)
	}
	crossProjectEvidence := createMemoryEvidenceFixture(t, repositories, repositoryB, 1190)

	// Go-side check: TransitionMemoryArtifactMaturity must reject before
	// ever attempting the write.
	if _, err := repositories.TransitionMemoryArtifactMaturity(ctx, TransitionMemoryArtifactMaturity{
		RevisionID: revision.RevisionID, From: domain.MaturityStateCandidate, To: domain.MaturityStateQuarantined,
		ReasonKind: MemoryArtifactInvalidationReasonEvidenceAmbiguous, DetailRedacted: "cross-project counterexample probe",
		CounterexampleEvidenceID: &crossProjectEvidence, IdempotencyKey: "cross-project-counterexample-api",
	}); !errors.Is(err, ErrConstraint) {
		t.Fatalf("cross-project counterexample evidence error = %v, want constraint", err)
	}
	history, err := repositories.ListMemoryArtifactMaturityTransitions(ctx, revision.RevisionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 0 {
		t.Fatalf("rejected transition must not be logged: %#v", history)
	}

	// Raw SQL: the migration trigger must reject this independently of the
	// Go-level API, exactly like every sibling project-boundary trigger.
	rawID := memoryMaturityTransitionID(revision.RevisionID, "cross-project-counterexample-raw-sql")
	_, rawErr := repositories.database.sql.ExecContext(
		ctx,
		`INSERT INTO memory_artifact_maturity_transitions (
			id, revision_id, from_state, to_state, reason_kind, detail_redacted,
			counterexample_evidence_id, idempotency_key, transitioned_at_unix_micros
		 ) VALUES (?, ?, 'candidate', 'quarantined', 'other', 'raw sql probe', ?, 'cross-project-counterexample-raw-sql', 1)`,
		rawID, revision.RevisionID, crossProjectEvidence,
	)
	if !errors.Is(classify("bypass memory artifact maturity counterexample project boundary", rawErr), ErrConstraint) {
		t.Fatalf("raw SQL cross-project counterexample evidence error = %v, want constraint", rawErr)
	}

	// Sanity: the same-project case is accepted, proving the trigger and
	// Go-side check target the cross-project mismatch specifically, not
	// CounterexampleEvidenceID in general.
	sameProjectEvidence := createMemoryEvidenceFixture(t, repositories, repositoryA, 1200)
	accepted, err := repositories.TransitionMemoryArtifactMaturity(ctx, TransitionMemoryArtifactMaturity{
		RevisionID: revision.RevisionID, From: domain.MaturityStateCandidate, To: domain.MaturityStateQuarantined,
		ReasonKind: MemoryArtifactInvalidationReasonEvidenceAmbiguous, DetailRedacted: "same-project counterexample",
		CounterexampleEvidenceID: &sameProjectEvidence, IdempotencyKey: "same-project-counterexample",
	})
	if err != nil {
		t.Fatal(err)
	}
	if accepted.CounterexampleEvidenceID == nil || *accepted.CounterexampleEvidenceID != sameProjectEvidence {
		t.Fatalf("accepted transition = %#v", accepted)
	}
}
