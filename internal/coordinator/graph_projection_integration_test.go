package coordinator

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
	"codeflux.dev/codeflux/internal/graph"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
)

func TestGraphProjectionRuntimeCommitsBeforePublishAndFeedsGraphQueryService(t *testing.T) {
	ctx := t.Context()
	repositories := openGraphProjectionRepositories(t)
	projectID, _ := domain.NewProjectID()
	repositoryID, _ := domain.NewRepositoryID()
	threadID, _ := domain.NewThreadID()
	sessionID, _ := domain.NewSessionID()
	taskID, _ := domain.NewTaskID()
	if _, err := repositories.CreateProject(ctx, storage.CreateProject{ID: projectID, Name: "Projection integration"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateRepository(ctx, storage.CreateRepository{ID: repositoryID, ProjectID: projectID, CanonicalPath: filepath.Join(t.TempDir(), "repository"), GitIdentity: "projection-integration"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateThread(ctx, storage.CreateThread{ID: threadID, ProjectID: projectID, RepositoryID: repositoryID, Title: "Projection integration"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateSession(ctx, storage.CreateSession{ID: sessionID, ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateTask(ctx, storage.CreateTask{ID: taskID, ThreadID: threadID, RepositoryID: repositoryID,
		PolicyPreset: domain.PolicyPresetCorrectness, ReasoningEffort: domain.ReasoningEffortMaximum,
		RiskLevel: domain.RiskLevelRoutine, RequiredAssurance: domain.AssuranceLevelContractChecked,
		IdempotencyKey: "projection-integration-task"}); err != nil {
		t.Fatal(err)
	}

	scope := storage.GraphQueryScope{ProjectID: projectID, TaskID: taskID}
	graphID, _ := domain.NewGraphID()
	identity, err := graph.NewGraph(graphID, taskID, time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	projection, err := graph.NewGraphProjection(identity, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	publisher := &queryingGraphPublisher{repositories: repositories, scope: scope}
	service, err := NewGraphProjectionService(repositories, publisher)
	if err != nil {
		t.Fatal(err)
	}

	sourceID, _ := domain.NewEventID()
	durable, err := repositories.AppendTaskEvent(ctx, storage.AppendTaskEvent{ID: sourceID, TaskID: taskID,
		EventType: "task.requirement-accepted", PayloadJSON: `{"classification":"requirement"}`,
		IdempotencyKey: "projection-requirement"})
	if err != nil {
		t.Fatal(err)
	}
	nodeID, _ := domain.NewNodeID()
	revisionID, _ := domain.NewGraphRevisionID()
	result, err := service.ProjectCommittedTaskEvent(ctx, scope, projection, durable, graph.ProjectionEvent{
		Kind:        graph.ProjectionRequirementAccepted,
		Requirement: &graph.RequirementFact{NodeID: nodeID, DisplayName: "Accepted requirement"},
	}, revisionID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Committed == nil || result.Committed.Event.Kind != events.KindGraphSnapshot {
		t.Fatalf("root projection commit = %#v", result.Committed)
	}
	if publisher.calls != 1 || publisher.queryErr != nil || publisher.observedRevision != revisionID {
		t.Fatalf("publisher observed calls=%d revision=%s err=%v", publisher.calls, publisher.observedRevision, publisher.queryErr)
	}

	queryService, err := NewGraphQueryService(repositories)
	if err != nil {
		t.Fatal(err)
	}
	view, err := queryService.GetGraphSlice(ctx, transport.GraphSliceQuery{
		Scope: transport.GraphQueryScope{ProjectID: projectID, TaskID: taskID},
		Mode:  graph.ModeProgram, MaxNodes: 10, MaxEdges: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.Header.RevisionID != revisionID || len(view.Nodes) != 1 || view.Nodes[0].Node.ID() != nodeID || view.Nodes[0].Node.DisplayName() != "Accepted requirement" {
		t.Fatalf("query service derived graph = %#v", view)
	}
	if len(view.Nodes[0].Node.Sources().EventIDs()) != 1 || view.Nodes[0].Node.Sources().EventIDs()[0] != durable.ID {
		t.Fatalf("query service provenance = %#v", view.Nodes[0].Node.Sources().EventIDs())
	}
	artifactEventID, _ := domain.NewEventID()
	artifactDurable, err := repositories.AppendTaskEvent(ctx, storage.AppendTaskEvent{ID: artifactEventID, TaskID: taskID,
		EventType: "task.artifact-observed", PayloadJSON: `{"kind":"changed-file"}`,
		IdempotencyKey: "projection-artifact"})
	if err != nil {
		t.Fatal(err)
	}
	artifactNodeID, _ := domain.NewNodeID()
	artifactEdgeID, _ := domain.NewEdgeID()
	secondRevisionID, _ := domain.NewGraphRevisionID()
	second, err := service.ProjectNextCommittedTaskEvent(ctx, scope, artifactDurable, graph.ProjectionEvent{
		Kind: graph.ProjectionArtifactObserved,
		Artifact: &graph.ArtifactFact{Kind: graph.ArtifactChangedFile, NodeID: artifactNodeID,
			ProvenanceEdge: artifactEdgeID, ProducerNode: nodeID, DisplayName: "Changed file", State: graph.ProjectionPassedState},
	}, secondRevisionID)
	if err != nil {
		t.Fatal(err)
	}
	if second.Committed == nil || second.Committed.Event.Kind != events.KindGraphPatch || publisher.calls != 2 || publisher.observedRevision != secondRevisionID {
		t.Fatalf("second projection commit = %#v calls=%d observed=%s", second.Committed, publisher.calls, publisher.observedRevision)
	}
	view, err = queryService.GetGraphSlice(ctx, transport.GraphSliceQuery{
		Scope: transport.GraphQueryScope{ProjectID: projectID, TaskID: taskID},
		Mode:  graph.ModeProgram, MaxNodes: 10, MaxEdges: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.Header.RevisionID != secondRevisionID || view.Header.ParentID == nil || *view.Header.ParentID != revisionID || len(view.Nodes) != 2 || len(view.Edges) != 1 {
		t.Fatalf("query service patched graph = %#v", view)
	}

	sequenceBefore, err := repositories.CurrentSessionSequence(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	forgedEventID, _ := domain.NewEventID()
	forgedNodeID, _ := domain.NewNodeID()
	forgedEdgeID, _ := domain.NewEdgeID()
	forgedRevisionID, _ := domain.NewGraphRevisionID()
	_, err = service.ProjectCommittedTaskEvent(ctx, scope, second.Projection, storage.TaskEvent{
		ID: forgedEventID, TaskID: taskID, Sequence: artifactDurable.Sequence + 1,
		EventType: "task.artifact-observed", PayloadJSON: `{}`, CreatedAt: artifactDurable.CreatedAt.Add(time.Microsecond),
	}, graph.ProjectionEvent{Kind: graph.ProjectionArtifactObserved,
		Artifact: &graph.ArtifactFact{Kind: graph.ArtifactDiff, NodeID: forgedNodeID,
			ProvenanceEdge: forgedEdgeID, ProducerNode: artifactNodeID, DisplayName: "Uncommitted diff", State: graph.ProjectionPassedState}},
		forgedRevisionID)
	if !errors.Is(err, storage.ErrConstraint) {
		t.Fatalf("uncommitted durable source error = %v", err)
	}
	unchanged, err := queryService.GetGraphSlice(ctx, transport.GraphSliceQuery{
		Scope: transport.GraphQueryScope{ProjectID: projectID, TaskID: taskID}, Mode: graph.ModeProgram,
		MaxNodes: 10, MaxEdges: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	sequenceAfterRollback, err := repositories.CurrentSessionSequence(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Header.RevisionID != secondRevisionID || sequenceAfterRollback != sequenceBefore || publisher.calls != 2 {
		t.Fatalf("failed projection leaked state: revision=%s sequence=%d/%d calls=%d", unchanged.Header.RevisionID, sequenceBefore, sequenceAfterRollback, publisher.calls)
	}
	tokenSourceID, _ := domain.NewEventID()
	tokenDurable, err := repositories.AppendTaskEvent(ctx, storage.AppendTaskEvent{ID: tokenSourceID, TaskID: taskID,
		EventType: "provider.token-delta", PayloadJSON: `{"redacted":true}`, IdempotencyKey: "projection-token"})
	if err != nil {
		t.Fatal(err)
	}
	tokenResult, err := service.ProjectCommittedTaskEvent(ctx, scope, second.Projection, tokenDurable, graph.ProjectionEvent{
		Kind: graph.ProjectionTokenDelta, TokenDelta: &graph.TokenDeltaFact{},
	}, domain.GraphRevisionID{})
	if err != nil {
		t.Fatal(err)
	}
	sequenceAfter, err := repositories.CurrentSessionSequence(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if tokenResult.Committed != nil || tokenResult.Projection.LastSequence() != 3 || sequenceAfter != sequenceBefore || publisher.calls != 2 {
		t.Fatalf("token delta materialized: commit=%#v sequence=%d/%d calls=%d", tokenResult.Committed, sequenceBefore, sequenceAfter, publisher.calls)
	}
}

func TestOrdinaryDraftTaskActivityMaterializesRequirementGraph(t *testing.T) {
	ctx := t.Context()
	repositories := openGraphProjectionRepositories(t)
	projectID, _ := domain.NewProjectID()
	repositoryID, _ := domain.NewRepositoryID()
	threadID, _ := domain.NewThreadID()
	sessionID, _ := domain.NewSessionID()
	workspaceID, _ := domain.NewWorkspaceID()
	if _, err := repositories.CreateProject(ctx, storage.CreateProject{ID: projectID, Name: "Ordinary graph activity"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateRepository(ctx, storage.CreateRepository{ID: repositoryID, ProjectID: projectID, CanonicalPath: filepath.Join(t.TempDir(), "repository"), GitIdentity: "ordinary-graph-activity"}); err != nil {
		t.Fatal(err)
	}
	seedThreadID, _ := domain.NewThreadID()
	if _, err := repositories.CreateThread(ctx, storage.CreateThread{ID: seedThreadID, ProjectID: projectID, RepositoryID: repositoryID, Title: "Workspace seed"}); err != nil {
		t.Fatal(err)
	}
	seedTaskID, _ := domain.NewTaskID()
	if _, err := repositories.CreateTask(ctx, storage.CreateTask{ID: seedTaskID, ThreadID: seedThreadID, RepositoryID: repositoryID,
		PolicyPreset: domain.PolicyPresetBalanced, ReasoningEffort: domain.ReasoningEffortStandard,
		RiskLevel: domain.RiskLevelRoutine, RequiredAssurance: domain.AssuranceLevelRuntimeOnly,
		IdempotencyKey: "ordinary-workspace-seed"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateWorktreeBinding(ctx, storage.CreateWorktreeBinding{WorkspaceID: workspaceID,
		TaskID: seedTaskID, RepositoryID: repositoryID, BaseRevision: strings.Repeat("a", 40),
		HeadRevision: strings.Repeat("a", 40), BranchName: "codeflux/ordinary-seed", WorktreePath: filepath.Join(t.TempDir(), "worktree")}); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateThread(ctx, storage.CreateThread{ID: threadID, SessionID: sessionID,
		ProjectID: projectID, RepositoryID: repositoryID, WorkspaceID: workspaceID,
		Title: "Ordinary graph activity", IdempotencyKey: "ordinary-thread"}); err != nil {
		t.Fatal(err)
	}
	publisher := &collectingGraphPublisher{}
	projectionService, err := NewGraphProjectionService(repositories, publisher)
	if err != nil {
		t.Fatal(err)
	}
	threads, err := NewThreadApplication(repositories, "ordinary-graph-activity-secret-32-bytes", publisher)
	if err != nil {
		t.Fatal(err)
	}
	if err := threads.ConfigureGraphProjection(projectionService); err != nil {
		t.Fatal(err)
	}
	command := transport.SendMessageCommand{ThreadID: threadID, IdempotencyKey: "ordinary-draft-task",
		ExpectedRevision: 0, Body: "Implement the accepted requirement", CreateDraftTask: true}
	result, err := threads.SendMessage(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	if result.DraftTask == nil {
		t.Fatal("ordinary message did not create a draft task")
	}
	queryService, err := NewGraphQueryService(repositories)
	if err != nil {
		t.Fatal(err)
	}
	view, err := queryService.GetGraphSlice(ctx, transport.GraphSliceQuery{
		Scope: transport.GraphQueryScope{ProjectID: projectID, TaskID: result.DraftTask.TaskID},
		Mode:  graph.ModeProgram, MaxNodes: 10, MaxEdges: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Nodes) != 1 || view.Nodes[0].Node.Class() != graph.NodeClassRequirement || view.Nodes[0].Node.DisplayName() != "Accepted task requirement" {
		t.Fatalf("ordinary task graph = %#v", view)
	}
	if len(publisher.kinds) != 2 || publisher.kinds[0] != events.KindMessageFinal || publisher.kinds[1] != events.KindGraphSnapshot {
		t.Fatalf("ordinary publication order = %#v", publisher.kinds)
	}
	retried, err := threads.SendMessage(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	if retried.DraftTask == nil || retried.DraftTask.TaskID != result.DraftTask.TaskID || len(publisher.kinds) != 2 {
		t.Fatalf("ordinary task retry = %#v publications=%#v", retried, publisher.kinds)
	}
}

type collectingGraphPublisher struct{ kinds []events.Kind }

func (publisher *collectingGraphPublisher) PublishCommitted(event events.SessionEvent) error {
	publisher.kinds = append(publisher.kinds, event.Kind)
	return nil
}

type queryingGraphPublisher struct {
	repositories     *storage.Repositories
	scope            storage.GraphQueryScope
	calls            int
	observedRevision domain.GraphRevisionID
	queryErr         error
}

func (publisher *queryingGraphPublisher) PublishCommitted(event events.SessionEvent) error {
	publisher.calls++
	result, err := publisher.repositories.GetTaskGraphSlice(context.Background(), storage.TaskGraphSliceQuery{
		Scope: publisher.scope, Mode: graph.ModeProgram, MaxNodes: 10, MaxEdges: 10,
	})
	publisher.queryErr = err
	if err == nil {
		publisher.observedRevision = result.Header.RevisionID
	}
	return err
}

func openGraphProjectionRepositories(t *testing.T) *storage.Repositories {
	t.Helper()
	database, err := storage.Open(t.Context(), storage.OpenOptions{Path: filepath.Join(t.TempDir(), "projection.db"), MaximumConnections: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(context.Background()); err != nil {
			t.Errorf("close projection database: %v", err)
		}
	})
	if _, err := database.Migrate(t.Context(), storage.MigrationOptions{ApplicationVersion: "graph-projection-integration", BackupDirectory: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	current := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	var clockMu sync.Mutex
	repositories, err := storage.NewRepositories(database, func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		current = current.Add(time.Microsecond)
		return current
	})
	if err != nil {
		t.Fatal(err)
	}
	return repositories
}
