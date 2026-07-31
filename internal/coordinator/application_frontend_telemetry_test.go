package coordinator

import (
	"context"
	"path/filepath"
	"testing"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/transport"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestApplicationFrontendTelemetryRequiresAuthAndSupportsInspectionDeletion(t *testing.T) {
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
	connection, err := grpc.NewClient(application.TaskControlAddress(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	client := codefluxv1.NewSettingsServiceClient(connection)
	record := &codefluxv1.RecordFrontendTelemetryRequest{
		Control: &codefluxv1.MutationControl{IdempotencyKey: "browser-first-run-1"},
		Event: &codefluxv1.FrontendTelemetryEvent{
			Kind:         codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_FIRST_RUN_STEP,
			Outcome:      codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_SUCCEEDED,
			Component:    codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_FIRST_RUN,
			FailureClass: codefluxv1.FrontendTelemetryFailureClass_FRONTEND_TELEMETRY_FAILURE_CLASS_NONE,
		},
	}
	if _, err := client.RecordFrontendTelemetry(t.Context(), record); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("unauthenticated record error = %v", err)
	}
	ctx := metadata.AppendToOutgoingContext(t.Context(), transport.SessionMetadataKey, application.BrowserSessionSecret())
	recorded, err := client.RecordFrontendTelemetry(ctx, record)
	if err != nil || recorded.GetEvent().GetLocalId() == 0 || recorded.GetEvent().GetOccurredAt() == nil {
		t.Fatalf("authenticated record = %#v, %v", recorded, err)
	}
	listed, err := client.ListFrontendTelemetry(ctx, &codefluxv1.ListFrontendTelemetryRequest{Page: &codefluxv1.PageRequest{Limit: 10}})
	if err != nil || len(listed.GetEvents()) != 1 || listed.GetEvents()[0].GetKind() != record.GetEvent().GetKind() {
		t.Fatalf("authenticated list = %#v, %v", listed, err)
	}
	deleted, err := client.DeleteFrontendTelemetry(ctx, &codefluxv1.DeleteFrontendTelemetryRequest{
		Control:      &codefluxv1.MutationControl{IdempotencyKey: "browser-delete-1"},
		Scope:        codefluxv1.FrontendTelemetryDeleteScope_FRONTEND_TELEMETRY_DELETE_SCOPE_ALL,
		Confirmation: codefluxv1.FrontendTelemetryDeleteConfirmation_FRONTEND_TELEMETRY_DELETE_CONFIRMATION_CONFIRMED,
	})
	if err != nil || deleted.GetDeleted() != 1 || deleted.GetRemaining() != 0 {
		t.Fatalf("authenticated delete = %#v, %v", deleted, err)
	}
}
