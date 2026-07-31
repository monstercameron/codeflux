package coordinator

import (
	"context"
	"strings"
	"testing"

	agentloop "codeflux.dev/codeflux/internal/agent"
	"codeflux.dev/codeflux/internal/checkpoint"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
)

func TestLaneACheckpointAdapterMapsControlAndAgentTriggers(t *testing.T) {
	capture := &checkpointCaptureStub{}
	plans := checkpointPlanRevisionStub{revision: 17}
	adapter, err := NewLaneACheckpointAdapter(capture, plans)
	if err != nil {
		t.Fatal(err)
	}
	taskID := newTaskControlTaskID(t)
	runID := newTaskControlRunID(t)
	if _, err := adapter.CapturePauseCheckpoint(
		t.Context(),
		storage.TaskControlSnapshot{TaskID: taskID, RunID: runID},
		"pause-checkpoint",
	); err != nil {
		t.Fatal(err)
	}
	if err := adapter.CreateCheckpoint(
		t.Context(),
		agentloop.CheckpointRequest{
			TaskID:        taskID,
			RunID:         runID,
			PlanRevision:  17,
			PlanStepID:    "edit",
			ToolRequestID: "tool-material",
			Trigger:       agentloop.CheckpointMaterialEdit,
		},
	); err != nil {
		t.Fatal(err)
	}
	if err := adapter.CreateCheckpoint(
		t.Context(),
		agentloop.CheckpointRequest{
			TaskID:        taskID,
			RunID:         runID,
			PlanRevision:  17,
			PlanStepID:    "command",
			ToolRequestID: "tool-risky",
			Trigger:       agentloop.CheckpointBeforeRisky,
			PermissionID:  "grant-17",
			ActionSHA256:  strings.Repeat("a", 64),
		},
	); err != nil {
		t.Fatal(err)
	}
	want := []checkpoint.Trigger{
		checkpoint.TriggerUserPaused,
		checkpoint.TriggerMaterialEditApplied,
		checkpoint.TriggerBeforeRiskyAction,
	}
	if len(capture.commands) != len(want) {
		t.Fatalf("capture commands = %#v", capture.commands)
	}
	for index, trigger := range want {
		if capture.commands[index].Trigger != trigger ||
			capture.commands[index].ExpectedPlanRevision != 17 {
			t.Fatalf(
				"capture[%d] = %#v, want trigger %s",
				index,
				capture.commands[index],
				trigger,
			)
		}
	}
	risky := capture.commands[2].Attribution
	if risky.PermissionDecisionID != "grant-17" ||
		risky.ActionSHA256 != strings.Repeat("a", 64) ||
		risky.ToolRequestID != "tool-risky" {
		t.Fatalf("risky attribution = %#v", risky)
	}
}

func TestRepairCompletionCapturesSuccessfulValidationCheckpoint(
	t *testing.T,
) {
	fixture := newRepairCompletionFixture(t)
	fixture.runner.results = []ValidationCommandRun{
		passedValidationRun(t, 91),
	}
	checkpoints := &successfulValidationCheckpointStub{
		repairCheckpointStub: fixture.checkpoints,
	}
	service, err := NewRepairCompletionService(
		RepairCompletionDependencies{
			Validations: fixture.runner,
			Checkpoints: checkpoints,
			Repairs:     fixture.repairs,
			Control:     fixture.control,
			Store:       fixture.store,
			Repository:  fixture.repository,
			Budget:      fixture.budget,
			Redactor:    fixture.redactor,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := service.ValidateAndRepair(
		t.Context(),
		fixture.validationInput(0),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.ReadyForCompletion ||
		len(checkpoints.validationCalls) != 1 ||
		checkpoints.validationCalls[0].PlanRevision != 1 ||
		checkpoints.validationCalls[0].ValidationID.IsZero() {
		t.Fatalf(
			"outcome=%#v validation checkpoints=%#v",
			outcome,
			checkpoints.validationCalls,
		)
	}
}

type checkpointCaptureStub struct {
	commands []checkpoint.CaptureCommand
}

func (stub *checkpointCaptureStub) Capture(
	_ context.Context,
	command checkpoint.CaptureCommand,
) (checkpoint.CaptureResult, error) {
	stub.commands = append(stub.commands, command)
	return checkpoint.CaptureResult{
		Checkpoint: checkpoint.PersistedCheckpoint{
			ID:     command.CheckpointID,
			TaskID: command.TaskID,
			RunID:  command.RunID,
		},
		Created: true,
	}, nil
}

type checkpointPlanRevisionStub struct {
	revision uint64
}

func (stub checkpointPlanRevisionStub) CurrentCheckpointPlanRevision(
	context.Context,
	domain.TaskID,
	domain.RunID,
) (uint64, error) {
	return stub.revision, nil
}

type successfulValidationCheckpointCall struct {
	PlanRevision   uint64
	ValidationID   domain.ValidationID
	IdempotencyKey string
}

type successfulValidationCheckpointStub struct {
	*repairCheckpointStub
	validationCalls []successfulValidationCheckpointCall
}

func (stub *successfulValidationCheckpointStub) CreateSuccessfulValidationCheckpoint(
	_ context.Context,
	_ domain.TaskID,
	_ domain.RunID,
	planRevision uint64,
	validationID domain.ValidationID,
	idempotencyKey string,
) (domain.CheckpointID, error) {
	stub.validationCalls = append(
		stub.validationCalls,
		successfulValidationCheckpointCall{
			PlanRevision:   planRevision,
			ValidationID:   validationID,
			IdempotencyKey: idempotencyKey,
		},
	)
	return newTaskControlCheckpointID(), nil
}
