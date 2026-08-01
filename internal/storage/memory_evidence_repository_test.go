package storage

import (
	"errors"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
)

func TestRecordMemoryArtifactSupportingEvidenceValidatesSelfReportStrength(t *testing.T) {
	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 1000)
	repositoryID := testRepositoryID(t, 1001)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)
	artifact := createMemoryArtifactFixture(t, repositories, projectID, repositoryID, 1002)
	revision, err := repositories.GetLatestMemoryArtifactRevision(ctx, artifact)
	if err != nil {
		t.Fatal(err)
	}
	evidenceID := createMemoryEvidenceFixture(t, repositories, repositoryID, 1005)

	// Domain-level rejection: agent self-report can never carry corroborated strength.
	if err := repositories.RecordMemoryArtifactSupportingEvidence(ctx, revision.RevisionID, domain.SupportingEvidenceRecord{
		Evidence: evidenceID, Source: domain.EvidenceSourceKindAgentSelfReport, Strength: domain.EvidenceStrengthCorroborated,
	}); err == nil {
		t.Fatal("expected self-report corroborated strength to be rejected")
	}

	if err := repositories.RecordMemoryArtifactSupportingEvidence(ctx, revision.RevisionID, domain.SupportingEvidenceRecord{
		Evidence: evidenceID, Source: domain.EvidenceSourceKindAutomatedValidationRun, Strength: domain.EvidenceStrengthCorroborated,
	}); err != nil {
		t.Fatal(err)
	}
	// Idempotent re-record with identical fields.
	if err := repositories.RecordMemoryArtifactSupportingEvidence(ctx, revision.RevisionID, domain.SupportingEvidenceRecord{
		Evidence: evidenceID, Source: domain.EvidenceSourceKindAutomatedValidationRun, Strength: domain.EvidenceStrengthCorroborated,
	}); err != nil {
		t.Fatal(err)
	}
	// Conflicting re-record for the same evidence link.
	if err := repositories.RecordMemoryArtifactSupportingEvidence(ctx, revision.RevisionID, domain.SupportingEvidenceRecord{
		Evidence: evidenceID, Source: domain.EvidenceSourceKindUserApproval, Strength: domain.EvidenceStrengthCorroborated,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting evidence retry error = %v, want conflict", err)
	}

	links, err := repositories.ListMemoryArtifactSupportingEvidence(ctx, revision.RevisionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || links[0].EvidenceID != evidenceID || links[0].Strength != domain.EvidenceStrengthCorroborated {
		t.Fatalf("supporting evidence links = %#v", links)
	}
}

// createMemoryEvidenceFixture builds a minimal thread/task/validation/
// evidence chain within repositoryID's project so a real evidence.id
// exists for memory_artifact_supporting_evidence's foreign key and
// project-boundary trigger.
func createMemoryEvidenceFixture(
	t *testing.T,
	repositories *Repositories,
	repositoryID domain.RepositoryID,
	number int,
) domain.EvidenceID {
	t.Helper()
	ctx := t.Context()
	threadID := testThreadID(t, number)
	repository, err := repositories.GetRepository(ctx, repositoryID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateThread(ctx, CreateThread{
		ID: threadID, ProjectID: repository.ProjectID, RepositoryID: repositoryID,
		Title: "Evidence fixture",
	}); err != nil {
		t.Fatal(err)
	}
	task, err := repositories.CreateTask(ctx, CreateTask{
		ID: testTaskID(t, number+1), ThreadID: threadID, RepositoryID: repositoryID,
		PolicyPreset: domain.PolicyPresetBalanced, ReasoningEffort: domain.ReasoningEffortStandard,
		RiskLevel: domain.RiskLevelRoutine, RequiredAssurance: domain.AssuranceLevelRuntimeOnly,
		IdempotencyKey: testUUID(number + 1),
	})
	if err != nil {
		t.Fatal(err)
	}
	validation, err := repositories.CreateValidation(ctx, CreateValidation{
		ID: testValidationID(t, number+2), TaskID: task.ID,
		State: domain.ValidationStatePassed, Severity: domain.ValidationSeverityBlocking,
		ProfileName: "memory-evidence-fixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	evidenceID := testEvidenceID(t, number+3)
	if _, err := repositories.CreateEvidence(ctx, CreateEvidence{
		ID: evidenceID, ValidationID: validation.ID, TaskID: task.ID,
		AssuranceLevel: domain.AssuranceLevelRuntimeOnly, EvidenceType: "test-run",
		ContentHash: testHexHash(number + 3), SummaryRedacted: "fixture evidence",
	}); err != nil {
		t.Fatal(err)
	}
	return evidenceID
}

func testHexHash(seed int) string {
	const hexDigits = "0123456789abcdef"
	return strings.Repeat(string(hexDigits[seed%16]), 64)
}
