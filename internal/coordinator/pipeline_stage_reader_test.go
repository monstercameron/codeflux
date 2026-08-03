package coordinator

import (
	"context"
	"database/sql"
	"net"
	"path/filepath"
	"testing"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/pipeline"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
	"codeflux.dev/codeflux/migrations"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	_ "modernc.org/sqlite"
)

// createPipelineStageGRPCFixture seeds the minimal project/repository/thread/
// task chain the pipeline_stage_records foreign keys need, against a real
// temporary SQLite database.
func createPipelineStageGRPCFixture(t *testing.T) (*storage.Repositories, domain.TaskID) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pipeline-stage-grpc.sqlite3")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	sources, err := migrations.Sources()
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range sources {
		if _, err := raw.ExecContext(t.Context(), source.SQL); err != nil {
			t.Fatalf("apply %s: %v", source.Descriptor.Name, err)
		}
	}
	if _, err := raw.ExecContext(t.Context(), "PRAGMA foreign_keys = ON"); err != nil {
		t.Fatal(err)
	}

	projectID, _ := domain.NewProjectID()
	repositoryID, _ := domain.NewRepositoryID()
	threadID, _ := domain.NewThreadID()
	taskID, _ := domain.NewTaskID()
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO projects (id, name, created_at_unix_micros, updated_at_unix_micros, revision) VALUES (?, 'Pipeline stage project', 1, 1, 1)`, []any{projectID}},
		{`INSERT INTO repositories (id, project_id, canonical_path, git_identity, created_at_unix_micros, updated_at_unix_micros, revision) VALUES (?, ?, 'C:/pipeline-stage', 'pipeline-stage-git', 1, 1, 1)`, []any{repositoryID, projectID}},
		{`INSERT INTO threads (id, project_id, repository_id, title, created_at_unix_micros, updated_at_unix_micros, revision) VALUES (?, ?, ?, 'Pipeline stage thread', 1, 1, 1)`, []any{threadID, projectID, repositoryID}},
		{`INSERT INTO tasks (id, thread_id, repository_id, state, policy_preset, reasoning_effort, risk_level, required_assurance, idempotency_key, created_at_unix_micros, updated_at_unix_micros, revision) VALUES (?, ?, ?, 'running', 'balanced', 'standard', 'routine', 'runtime-only', 'pipeline-stage-task', 1, 1, 1)`, []any{taskID, threadID, repositoryID}},
	}
	for _, statement := range statements {
		if _, err := raw.ExecContext(t.Context(), statement.query, statement.args...); err != nil {
			t.Fatalf("seed pipeline stage fixture: %v\n%s", err, statement.query)
		}
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	database, err := storage.Open(t.Context(), storage.OpenOptions{Path: path, MaximumConnections: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close(context.Background()) })
	repositories, err := storage.NewRepositories(database, nil)
	if err != nil {
		t.Fatal(err)
	}
	return repositories, taskID
}

// TestPipelineStageServiceRealSQLiteEndToEndAndAuthentication drives the
// production wiring PIPE-006b adds: real SQLite pipeline_stage_records, read
// through PipelineStageReadService, through transport.PipelineStageService,
// over a real gRPC connection, including the unauthenticated refusal every
// other registered service gets from the shared boundary.
//
// This is the coordinator-side half of the read path the transport-level
// stub tests cannot exercise: it proves storage.Repositories actually
// satisfies PipelineStageLedgerStore in production and that a stage recorded
// through the real RecordPipelineStageResult path (not a hand-built summary)
// still reports elapsed_measured correctly after the full round trip.
func TestPipelineStageServiceRealSQLiteEndToEndAndAuthentication(t *testing.T) {
	repositories, taskID := createPipelineStageGRPCFixture(t)

	// storage.NewRepositories(database, nil) above defaults to a real
	// time.Now clock -- internal/coordinator cannot substitute a fixed one,
	// since Repositories.now is unexported to internal/storage. The elapsed
	// assertions below use a generous tolerance rather than an exact value
	// for that reason; internal/storage's own
	// TestPipelineStageMeasuredElapsedSpanIsPositive already proves the exact
	// arithmetic under a controlled clock.
	before := time.Now()
	if _, err := repositories.RecordPipelineStageResult(t.Context(), storage.RecordPipelineStage{
		TaskID: taskID, Attempt: 1, Stage: pipeline.StageInstructions,
		State: pipeline.StateSatisfied, DetailRedacted: "measured fixture",
		Evidence:  map[string]any{"fixture": true},
		StartedAt: before.Add(-2 * time.Minute),
	}); err != nil {
		t.Fatalf("record measured stage: %v", err)
	}
	if _, err := repositories.RecordPipelineStageResult(t.Context(), storage.RecordPipelineStage{
		TaskID: taskID, Attempt: 1, Stage: pipeline.StageClarification,
		State: pipeline.StateSkipped, DetailRedacted: "unmeasured fixture",
	}); err != nil {
		t.Fatalf("record unmeasured stage: %v", err)
	}

	boundary, err := transport.NewBoundary(transport.BoundaryOptions{SessionToken: "pipeline-stage-service-test-token-000"})
	if err != nil {
		t.Fatal(err)
	}
	application, err := NewPipelineStageReadService(repositories)
	if err != nil {
		t.Fatal(err)
	}
	service, err := transport.NewPipelineStageService(application)
	if err != nil {
		t.Fatal(err)
	}
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer(grpc.UnaryInterceptor(boundary.UnaryInterceptor()))
	codefluxv1.RegisterPipelineStageServiceServer(server, service)
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close(); <-serveDone })

	connection, err := grpc.NewClient(
		"passthrough:///pipeline-stage",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	client := codefluxv1.NewPipelineStageServiceClient(connection)

	request := &codefluxv1.ListPipelineStagesRequest{TaskId: taskIdentity(taskID)}
	if _, err := client.ListPipelineStages(t.Context(), request); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("unauthenticated code = %v (%v)", status.Code(err), err)
	}

	ctx := metadata.NewOutgoingContext(t.Context(), metadata.Pairs(
		transport.SessionMetadataKey, "pipeline-stage-service-test-token-000",
	))
	response, err := client.ListPipelineStages(ctx, request)
	if err != nil {
		t.Fatalf("list pipeline stages: %v", err)
	}
	if response.GetAttempt() != 1 {
		t.Errorf("attempt = %d, want 1", response.GetAttempt())
	}
	if len(response.GetStages()) != 2 {
		t.Fatalf("stages = %d, want 2", len(response.GetStages()))
	}

	instructions := response.GetStages()[0]
	if instructions.GetStageNumber() != uint32(pipeline.StageInstructions) ||
		instructions.GetStageName() != "instructions" || instructions.GetState() != "satisfied" {
		t.Fatalf("instructions stage = %+v", instructions)
	}
	if !instructions.GetElapsedMeasured() {
		t.Error("instructions stage: elapsed_measured = false, want true")
	}
	if elapsed := instructions.GetElapsed().AsDuration(); elapsed < 100*time.Second || elapsed > 5*time.Minute {
		t.Errorf("instructions stage elapsed = %v, want roughly 2m", elapsed)
	}

	clarification := response.GetStages()[1]
	if clarification.GetStageNumber() != uint32(pipeline.StageClarification) ||
		clarification.GetState() != "skipped" {
		t.Fatalf("clarification stage = %+v", clarification)
	}
	if clarification.GetElapsedMeasured() {
		t.Error("clarification stage: elapsed_measured = true, want false")
	}
	if clarification.Elapsed != nil {
		t.Errorf("clarification stage sent an explicit elapsed %v, want unset",
			clarification.Elapsed.AsDuration())
	}
}
