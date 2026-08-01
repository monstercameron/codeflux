package retrieval

import (
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/retrievalgate"
	"codeflux.dev/codeflux/internal/storage"
)

// TestListTaskMemoryInfluenceSeparatesInfluenceFromMereRetrieval closes the
// M21-G03 read path: "the user can identify every memory item that
// influenced a completed task."
//
// The raw two-query reconstruction was already possible (see
// TestGateEvidenceG03_EveryMemoryItemForATaskIsReconstructableByTaskID), but
// it required a caller to already know how retrieval records are laid out.
// This proves a single call keyed only on TaskID returns the same answer,
// with influence and mere retrieval kept apart as docs/plan.md §31 requires.
func TestListTaskMemoryInfluenceSeparatesInfluenceFromMereRetrieval(t *testing.T) {
	ctx := t.Context()
	repositories := newTestRepositories(t)
	service, err := NewService(repositories)
	if err != nil {
		t.Fatal(err)
	}
	projectID, repositoryID := mustCreateProjectAndRepository(t, repositories)
	task := mustBuildTaskFingerprint(t, projectID, repositoryID, domain.AssuranceLevelRuntimeOnly)

	threadID, err := domain.NewThreadID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateThread(ctx, storage.CreateThread{
		ID: threadID, ProjectID: projectID, RepositoryID: repositoryID, Title: "influence report thread",
	}); err != nil {
		t.Fatal(err)
	}
	taskID, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	createdTask, err := repositories.CreateTask(ctx, storage.CreateTask{
		ID: taskID, ThreadID: threadID, RepositoryID: repositoryID,
		PolicyPreset: domain.PolicyPresetBalanced, ReasoningEffort: domain.ReasoningEffortStandard,
		RiskLevel: domain.RiskLevelRoutine, RequiredAssurance: domain.AssuranceLevelRuntimeOnly,
		IdempotencyKey: "influence-report-task",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, usedRevision := mustCreateRepositoryFactArtifact(t, repositories, projectID, repositoryID, "go build ./... (influenced)")
	_, ignoredRevision := mustCreateRepositoryFactArtifact(t, repositories, projectID, repositoryID, "go build ./... (ignored)")

	queryID := newTestQueryID(t, "influence")
	result, err := service.RunPreWorkGate(ctx, PreWorkGateInput{
		QueryID: queryID, ProjectID: projectID, TaskID: createdTask.ID,
		Boundary: domain.MemoryQueryProjectBoundary{Project: projectID},
		Task:     task,
	})
	if err != nil {
		t.Fatal(err)
	}
	var usedItem InfluentialMemoryItem
	for _, item := range result.Eligible {
		if item.RevisionID == usedRevision {
			usedItem = item
		}
	}
	if usedItem.RevisionID.IsZero() {
		t.Fatal("fixture precondition: the to-be-used revision must be eligible")
	}
	if err := service.RecordInfluence(
		ctx, usedItem, retrievalgate.AgentInfluenceActionUsed, "the agent reused this build command verbatim",
	); err != nil {
		t.Fatal(err)
	}

	report, err := service.ListTaskMemoryInfluence(ctx, createdTask.ID)
	if err != nil {
		t.Fatal(err)
	}
	if report.TaskID != createdTask.ID {
		t.Fatalf("report.TaskID = %s, want %s", report.TaskID, createdTask.ID)
	}
	if len(report.Records) != 2 {
		t.Fatalf("report.Records = %d, want 2 (every candidate the task retrieved)", len(report.Records))
	}
	if len(report.Influenced) != 1 {
		t.Fatalf("report.Influenced = %d, want exactly 1", len(report.Influenced))
	}
	if report.Influenced[0].RevisionID != usedRevision {
		t.Fatalf("influenced revision = %s, want %s", report.Influenced[0].RevisionID, usedRevision)
	}
	if report.Influenced[0].Disposition != InfluenceDispositionInfluenced {
		t.Fatalf("disposition = %s, want influenced", report.Influenced[0].Disposition)
	}

	// THE GATE'S REAL CONTENT: the eligible-but-never-acted-on candidate is
	// visible in the full record set, and is NOT reported as influence.
	var ignored *TaskMemoryInfluenceRecord
	for index := range report.Records {
		if report.Records[index].RevisionID == ignoredRevision {
			ignored = &report.Records[index]
		}
	}
	if ignored == nil {
		t.Fatal("the retrieved-but-unused candidate must still appear in the full record set")
	}
	if ignored.Disposition != InfluenceDispositionRetrievedOnly {
		t.Fatalf("ignored candidate disposition = %s, want retrieved-only", ignored.Disposition)
	}
	for _, record := range report.Influenced {
		if record.RevisionID == ignoredRevision {
			t.Fatal("a merely retrieved candidate must never be reported as influential")
		}
	}
	if len(ignored.Channels) == 0 {
		t.Fatal("every reported record must carry the discovery channels that surfaced it")
	}
}

// TestListTaskMemoryInfluenceRecordsFallbackQueries proves a retrieval that
// found nothing eligible is reported as a fallback rather than silently
// omitted. M21-076 makes falling back a normal outcome, and an honest
// account of what memory did must include the times it did nothing.
func TestListTaskMemoryInfluenceRecordsFallbackQueries(t *testing.T) {
	ctx := t.Context()
	repositories := newTestRepositories(t)
	service, err := NewService(repositories)
	if err != nil {
		t.Fatal(err)
	}
	projectID, repositoryID := mustCreateProjectAndRepository(t, repositories)
	task := mustBuildTaskFingerprint(t, projectID, repositoryID, domain.AssuranceLevelRuntimeOnly)

	threadID, err := domain.NewThreadID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateThread(ctx, storage.CreateThread{
		ID: threadID, ProjectID: projectID, RepositoryID: repositoryID, Title: "fallback thread",
	}); err != nil {
		t.Fatal(err)
	}
	taskID, err := domain.NewTaskID()
	if err != nil {
		t.Fatal(err)
	}
	createdTask, err := repositories.CreateTask(ctx, storage.CreateTask{
		ID: taskID, ThreadID: threadID, RepositoryID: repositoryID,
		PolicyPreset: domain.PolicyPresetBalanced, ReasoningEffort: domain.ReasoningEffortStandard,
		RiskLevel: domain.RiskLevelRoutine, RequiredAssurance: domain.AssuranceLevelRuntimeOnly,
		IdempotencyKey: "influence-fallback-task",
	})
	if err != nil {
		t.Fatal(err)
	}

	// No artifacts seeded at all, so discovery finds nothing.
	queryID := newTestQueryID(t, "fallback")
	result, err := service.RunPreWorkGate(ctx, PreWorkGateInput{
		QueryID: queryID, ProjectID: projectID, TaskID: createdTask.ID,
		Boundary: domain.MemoryQueryProjectBoundary{Project: projectID},
		Task:     task,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.FellBack {
		t.Fatal("fixture precondition: an empty project must fall back")
	}

	report, err := service.ListTaskMemoryInfluence(ctx, createdTask.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Influenced) != 0 {
		t.Fatalf("report.Influenced = %d, want 0", len(report.Influenced))
	}
	if len(report.FellBackQueries) != 1 || report.FellBackQueries[0] != queryID {
		t.Fatalf("report.FellBackQueries = %#v, want exactly %s", report.FellBackQueries, queryID)
	}
}

// TestListTaskMemoryInfluenceRejectsZeroTaskID keeps the read path from
// answering an unscoped question.
func TestListTaskMemoryInfluenceRejectsZeroTaskID(t *testing.T) {
	repositories := newTestRepositories(t)
	service, err := NewService(repositories)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ListTaskMemoryInfluence(t.Context(), domain.TaskID{}); err == nil {
		t.Fatal("an empty task ID must be rejected")
	}
}
