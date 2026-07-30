package storage

import (
	"context"
	"errors"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
)

func TestCommandExecutionPersistsBoundedRedactedOutcome(t *testing.T) {
	ctx := context.Background()
	repositories, task := createTaskFixture(t, 1360)
	runID := createToolTestRun(t, repositories, task.ID, 1364)
	command, err := repositories.CreateCommandExecution(ctx, CreateCommandExecution{
		ID: "execution-1360", TaskID: task.ID, RunID: runID,
		State:       domain.CommandExecutionStateAuthorized,
		CommandName: "go", ArgumentsRedactedJSON: `["test","./..."]`,
		WorkingDirectoryScope: "/fixture/worktree",
		IdempotencyKey:        "execution-one", ToolSchemaVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	retried, err := repositories.CreateCommandExecution(ctx, CreateCommandExecution{
		ID: "execution-1360", TaskID: task.ID, RunID: runID,
		State:       domain.CommandExecutionStateAuthorized,
		CommandName: "go", ArgumentsRedactedJSON: `["test","./..."]`,
		WorkingDirectoryScope: "/fixture/worktree",
		IdempotencyKey:        "execution-one", ToolSchemaVersion: 1,
	})
	if err != nil || retried.ID != command.ID {
		t.Fatalf("idempotent command = %#v, %v", retried, err)
	}
	running, err := repositories.TransitionCommandExecution(ctx, TransitionCommandExecution{
		ID: command.ID, ExpectedRevision: 0, To: domain.CommandExecutionStateRunning,
	})
	if err != nil {
		t.Fatal(err)
	}
	path := "/usr/bin/go"
	exit := 0
	duration := int64(25)
	no := false
	yes := true
	done, err := repositories.TransitionCommandExecution(ctx, TransitionCommandExecution{
		ID: command.ID, ExpectedRevision: running.Revision,
		To: domain.CommandExecutionStateSucceeded, ExecutablePath: &path,
		ExitCode: &exit, DurationMillis: &duration, TimedOut: &no,
		Cancelled: &no, StdoutTruncated: &yes, StderrTruncated: &no,
	})
	if err != nil {
		t.Fatal(err)
	}
	if done.ExecutablePath == nil || *done.ExecutablePath != path ||
		done.DurationMillis == nil || *done.DurationMillis != duration ||
		done.StdoutTruncated == nil || !*done.StdoutTruncated {
		t.Fatalf("completed command = %#v", done)
	}
	output, err := repositories.AppendRedactedCommandOutput(ctx, AppendRedactedCommandOutput{
		ID: "output-1360", CommandExecutionID: command.ID,
		Stream: "stdout", Sequence: 1,
		ContentRedacted: "token=[REDACTED]", Truncated: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if output.ByteCount != len(output.ContentRedacted) || !output.Truncated {
		t.Fatalf("output = %#v", output)
	}
	if _, err := repositories.TransitionCommandExecution(ctx, TransitionCommandExecution{
		ID: command.ID, ExpectedRevision: running.Revision,
		To:       domain.CommandExecutionStateFailed,
		ExitCode: &exit, DurationMillis: &duration, TimedOut: &no,
		Cancelled: &no, StdoutTruncated: &no, StderrTruncated: &no,
	}); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("stale terminal transition = %v", err)
	}
}

func TestCommandExecutionRejectsUnresolvedApprovalAndUnboundedOutput(t *testing.T) {
	repositories, task := createTaskFixture(t, 1380)
	runID := createToolTestRun(t, repositories, task.ID, 1384)
	approval, err := repositories.CreateApproval(context.Background(), CreateApproval{
		ID: testApprovalID(t, 1385), TaskID: task.ID, Scope: "command",
		RequestReason: "fixture", IdempotencyKey: "pending-command",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateCommandExecution(context.Background(), CreateCommandExecution{
		ID: "execution-1380", TaskID: task.ID, RunID: runID,
		ApprovalID: &approval.ID, State: domain.CommandExecutionStateAuthorized,
		CommandName: "curl", ArgumentsRedactedJSON: `["[REDACTED]"]`,
		WorkingDirectoryScope: "/fixture/worktree",
		IdempotencyKey:        "execution-pending", ToolSchemaVersion: 1,
	}); !errors.Is(err, ErrConstraint) {
		t.Fatalf("pending approval command = %v", err)
	}
	if _, err := repositories.AppendRedactedCommandOutput(
		context.Background(),
		AppendRedactedCommandOutput{
			ID: "output-too-large", CommandExecutionID: "execution-1380",
			Stream: "stdout", Sequence: 1,
			ContentRedacted: string(make([]byte, (64<<10)+1)),
		},
	); err == nil {
		t.Fatal("unbounded output was accepted")
	}
}
