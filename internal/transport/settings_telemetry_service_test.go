package transport

import (
	"context"
	"testing"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/frontendtelemetry"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type telemetryApplicationFake struct {
	recorded frontendtelemetry.Event
	page     frontendtelemetry.Page
	deleted  frontendtelemetry.DeleteRequest
}

func (fake *telemetryApplicationFake) RecordFrontendTelemetry(_ context.Context, event frontendtelemetry.Event) (frontendtelemetry.Event, error) {
	fake.recorded = event
	event.ID = 41
	event.OccurredAt = time.Unix(100, 0).UTC()
	return event, nil
}

func (fake *telemetryApplicationFake) ListFrontendTelemetry(_ context.Context, query frontendtelemetry.Query) (frontendtelemetry.Page, error) {
	return fake.page, nil
}

func (fake *telemetryApplicationFake) DeleteFrontendTelemetry(_ context.Context, request frontendtelemetry.DeleteRequest) (frontendtelemetry.DeleteResult, error) {
	fake.deleted = request
	return frontendtelemetry.DeleteResult{Deleted: 3, Remaining: 2}, nil
}

func TestSettingsTelemetryRecordListAndDeleteAreTypedAndBounded(t *testing.T) {
	taskID, _ := domain.NewTaskID()
	taskIdentity, _ := TaskIDToProto(taskID)
	fake := &telemetryApplicationFake{}
	service, err := NewSettingsService(fake)
	if err != nil {
		t.Fatal(err)
	}
	control := &codefluxv1.MutationControl{IdempotencyKey: "telemetry-record-1"}
	recorded, err := service.RecordFrontendTelemetry(t.Context(), &codefluxv1.RecordFrontendTelemetryRequest{
		Control: control,
		Event: &codefluxv1.FrontendTelemetryEvent{
			Kind:         codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_TASK_CONTROL,
			Outcome:      codefluxv1.FrontendTelemetryOutcome_FRONTEND_TELEMETRY_OUTCOME_PAUSED,
			Component:    codefluxv1.FrontendTelemetryComponent_FRONTEND_TELEMETRY_COMPONENT_TOP_BAR,
			FailureClass: codefluxv1.FrontendTelemetryFailureClass_FRONTEND_TELEMETRY_FAILURE_CLASS_NONE,
			TaskId:       taskIdentity, Revision: 7,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if fake.recorded.Kind != frontendtelemetry.KindTaskControl || fake.recorded.TaskID != taskID ||
		recorded.GetEvent().GetLocalId() != 41 || recorded.GetEvent().GetRevision() != 7 {
		t.Fatalf("recorded domain/wire event = %#v / %#v", fake.recorded, recorded.GetEvent())
	}

	fake.page = frontendtelemetry.Page{Events: []frontendtelemetry.Event{
		{ID: 41, Kind: frontendtelemetry.KindTaskControl, OccurredAt: time.Unix(100, 0).UTC(),
			Outcome: frontendtelemetry.OutcomePaused, Component: frontendtelemetry.ComponentTopBar,
			FailureClass: frontendtelemetry.FailureNone, TaskID: taskID, Revision: 7},
	}, NextBeforeID: 41}
	listed, err := service.ListFrontendTelemetry(t.Context(), &codefluxv1.ListFrontendTelemetryRequest{
		Page:  &codefluxv1.PageRequest{Limit: frontendtelemetry.MaxQueryLimit},
		Kinds: []codefluxv1.FrontendTelemetryKind{codefluxv1.FrontendTelemetryKind_FRONTEND_TELEMETRY_KIND_TASK_CONTROL},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.GetEvents()) != 1 || !listed.GetPage().GetHasMore() || listed.GetPage().GetNextCursor() == "41" {
		t.Fatalf("listed telemetry = %#v", listed)
	}
	if _, err := service.ListFrontendTelemetry(t.Context(), &codefluxv1.ListFrontendTelemetryRequest{
		Page: &codefluxv1.PageRequest{Limit: frontendtelemetry.MaxQueryLimit + 1},
	}); status.Code(MapError(err)) != codes.InvalidArgument {
		t.Fatalf("oversize list error = %v", err)
	}
	deleted, err := service.DeleteFrontendTelemetry(t.Context(), &codefluxv1.DeleteFrontendTelemetryRequest{
		Control:      &codefluxv1.MutationControl{IdempotencyKey: "telemetry-delete-1"},
		Scope:        codefluxv1.FrontendTelemetryDeleteScope_FRONTEND_TELEMETRY_DELETE_SCOPE_ALL,
		Confirmation: codefluxv1.FrontendTelemetryDeleteConfirmation_FRONTEND_TELEMETRY_DELETE_CONFIRMATION_CONFIRMED,
	})
	if err != nil || deleted.GetDeleted() != 3 || fake.deleted.Scope != frontendtelemetry.DeleteAll {
		t.Fatalf("delete result/request = %#v / %#v / %v", deleted, fake.deleted, err)
	}
	if _, err := service.DeleteFrontendTelemetry(t.Context(), &codefluxv1.DeleteFrontendTelemetryRequest{
		Control: &codefluxv1.MutationControl{IdempotencyKey: "telemetry-delete-unconfirmed"},
		Scope:   codefluxv1.FrontendTelemetryDeleteScope_FRONTEND_TELEMETRY_DELETE_SCOPE_ALL,
	}); status.Code(MapError(err)) != codes.InvalidArgument {
		t.Fatalf("unconfirmed delete error = %v", err)
	}
}

func TestFrontendTelemetryEventSchemaHasNoContentField(t *testing.T) {
	descriptor := (&codefluxv1.FrontendTelemetryEvent{}).ProtoReflect().Descriptor()
	for index := 0; index < descriptor.Fields().Len(); index++ {
		field := descriptor.Fields().Get(index)
		if field.Kind() == protoreflect.StringKind || field.Kind() == protoreflect.BytesKind {
			t.Fatalf("content-capable event field %s has kind %s", field.Name(), field.Kind())
		}
	}
}
