package coordinator

import (
	"context"
	"path/filepath"
	"testing"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

func TestApplicationServesAuthoritativeTaskQueryFromSQLite(t *testing.T) {
	root := t.TempDir()
	application, err := StartApplication(t.Context(), ApplicationOptions{
		DatabasePath:    filepath.Join(root, "codeflux.sqlite3"),
		BackupDirectory: filepath.Join(root, "backups"),
		ListenAddress:   "127.0.0.1:0", TaskListenAddress: "127.0.0.1:0",
		TaskControls: &applicationTaskControlStub{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = application.Shutdown(context.Background()) })

	projectID, _ := domain.NewProjectID()
	repositoryID, _ := domain.NewRepositoryID()
	threadID, _ := domain.NewThreadID()
	sessionID, _ := domain.NewSessionID()
	messageID, _ := domain.NewMessageID()
	taskID, _ := domain.NewTaskID()
	budgetID, _ := domain.NewBudgetID()
	usd, _ := domain.ParseCurrencyCode("USD")
	if _, err := application.repos.CreateProject(t.Context(), storage.CreateProject{ID: projectID, Name: "Task query"}); err != nil {
		t.Fatal(err)
	}
	if _, err := application.repos.CreateRepository(t.Context(), storage.CreateRepository{
		ID: repositoryID, ProjectID: projectID,
		CanonicalPath: filepath.Join(root, "repository"), GitIdentity: "task-query-repository",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := application.repos.CreateThread(t.Context(), storage.CreateThread{
		ID: threadID, ProjectID: projectID, RepositoryID: repositoryID, Title: "Task query",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := application.repos.CreateSession(t.Context(), storage.CreateSession{ID: sessionID, ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	message, err := application.repos.AppendMessage(t.Context(), storage.AppendMessage{
		ID: messageID, ThreadID: threadID, Role: storage.MessageRoleUser,
		BodyRedacted: "Return the authoritative task projection.", IdempotencyKey: "task-query-message",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.repos.CreateTask(t.Context(), storage.CreateTask{
		ID: taskID, ThreadID: threadID, RepositoryID: repositoryID,
		RequestMessageID: &message.ID, PolicyPreset: domain.PolicyPresetBalanced,
		ReasoningEffort: domain.ReasoningEffortStandard, RiskLevel: domain.RiskLevelRoutine,
		RequiredAssurance: domain.AssuranceLevelRuntimeOnly, SettingsRevision: 0,
		IdempotencyKey: "task-query-task",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := application.repos.CreateBudget(t.Context(), storage.CreateBudget{
		TaskID: taskID,
		Budget: domain.TaskBudget{
			ID:            budgetID,
			WarningCost:   domain.Money{Currency: usd, MinorUnits: 2_500},
			HardStopCost:  domain.Money{Currency: usd, MinorUnits: 5_000},
			WarningTokens: 1_000, HardStopTokens: 2_000,
			WarningWallClock: 60_000, HardStopWallClock: 120_000,
			MaximumProviderCalls: 2, MaximumRepairRounds: 1, MaximumToolExecutions: 4,
		},
	}); err != nil {
		t.Fatal(err)
	}
	transitionEventID, _ := domain.NewEventID()
	if _, err := application.repos.TransitionTask(t.Context(), storage.TransitionTask{
		EventID: transitionEventID, TaskID: taskID, ExpectedRevision: 0,
		From: domain.TaskStateDraft, To: domain.TaskStateForecasting,
		IdempotencyKey: "task-query-committed-before-session-notification",
	}); err != nil {
		t.Fatal(err)
	}
	validationID, _ := domain.NewValidationID()
	checkpointID, _ := domain.NewCheckpointID()
	graphRevisionID, _ := domain.NewGraphRevisionID()
	bindings := events.RevisionBindings{Diff: 2, Plan: 3, Validation: 4, Evidence: 5, Graph: 6}
	taskPointer := taskID
	for _, event := range []events.NewSessionEvent{
		{
			SessionID: sessionID, ThreadID: threadID, TaskID: &taskPointer,
			Kind: events.KindToolStarted, Revision: 1, PayloadVersion: 1,
			Payload: events.Payload{Tool: &events.Tool{
				ExecutionID: "task-query-test", CommandName: "go test", State: "running",
			}},
		},
		{
			SessionID: sessionID, ThreadID: threadID, TaskID: &taskPointer,
			Kind: events.KindValidationUpdated, Revision: 1, PayloadVersion: 1,
			Payload: events.Payload{Validation: &events.Validation{
				ValidationID: validationID, State: domain.ValidationStateRunning,
				Required: true, DiffRevision: bindings.Diff,
			}},
		},
		{
			SessionID: sessionID, ThreadID: threadID, TaskID: &taskPointer,
			Kind: events.KindCheckpointCreated, Revision: 1, PayloadVersion: 1,
			Payload: events.Payload{Checkpoint: &events.Checkpoint{
				CheckpointID: checkpointID, TaskRevision: 1, PlanStep: "verify snapshot parity",
			}},
		},
		{
			SessionID: sessionID, ThreadID: threadID, TaskID: &taskPointer,
			Kind: events.KindChangeAcceptanceUpdated, Revision: 1, PayloadVersion: 1,
			Payload: events.Payload{ChangeAcceptance: &events.ChangeAcceptance{
				State: domain.ChangeAcceptanceStatePending, Bindings: bindings,
			}},
		},
		{
			SessionID: sessionID, ThreadID: threadID, TaskID: &taskPointer,
			Kind: events.KindGraphSnapshot, Revision: bindings.Graph, PayloadVersion: 1,
			Payload: events.Payload{Graph: &events.Graph{
				RevisionID: graphRevisionID, EncodedChange: []byte("bounded graph snapshot"),
			}},
		},
	} {
		if _, err := application.repos.PersistSessionEvent(t.Context(), event); err != nil {
			t.Fatal(err)
		}
	}

	connection, err := grpc.NewClient(application.TaskControlAddress(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	identity, _ := transport.TaskIDToProto(taskID)
	ctx := metadata.AppendToOutgoingContext(t.Context(), transport.SessionMetadataKey, application.BrowserSessionSecret())
	response, err := codefluxv1.NewTaskServiceClient(connection).GetTask(ctx,
		&codefluxv1.GetTaskRequest{TaskId: identity})
	if err != nil {
		t.Fatal(err)
	}
	task := response.GetTask()
	if task.GetTaskId().GetValue() != taskID.String() || task.GetThreadId().GetValue() != threadID.String() ||
		task.GetSessionId().GetValue() != sessionID.String() || task.GetState() != string(domain.TaskStateForecasting) ||
		task.GetSummary().GetValue() != message.BodyRedacted || task.GetHardBudget().GetMinorUnits() != 5_000 {
		t.Fatalf("authoritative task response = %#v", task)
	}
	sessionIdentity, _ := transport.SessionIDToProto(sessionID)
	snapshotResponse, err := codefluxv1.NewSessionServiceClient(connection).GetSessionSnapshot(ctx,
		&codefluxv1.GetSessionSnapshotRequest{SessionId: sessionIdentity})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := snapshotResponse.GetSnapshot()
	if snapshot.GetSessionId().GetValue() != sessionID.String() ||
		snapshot.GetThreadId().GetValue() != threadID.String() ||
		snapshot.GetTaskId().GetValue() != taskID.String() ||
		snapshot.GetTaskState() != task.GetState() ||
		snapshot.GetTaskRevision() != task.GetRevision() ||
		snapshot.GetBudget().GetHardLimitMinor() != task.GetHardBudget().GetMinorUnits() ||
		snapshot.GetBudgetRevision() != task.GetBudgetRevision() || snapshot.GetThroughSequence() == 0 ||
		snapshot.GetObservedAt() == nil || snapshot.GetTool().GetCommandName() != "go test" ||
		snapshot.GetValidation().GetValidationId().GetValue() != validationID.String() ||
		snapshot.GetCheckpoint().GetCheckpointId().GetValue() != checkpointID.String() ||
		snapshot.GetChangeAcceptance().GetGraphRevision() != bindings.Graph ||
		snapshot.GetReviewBindings().GetEvidenceRevision() != bindings.Evidence ||
		snapshot.GetReviewRevision() != 1 || snapshot.GetGraphRevision() != bindings.Graph {
		t.Fatalf("SQLite to gRPC session snapshot = %#v", snapshot)
	}
}
