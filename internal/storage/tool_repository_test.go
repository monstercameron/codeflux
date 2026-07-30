package storage

import (
	"context"
	"errors"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
)

const fixtureActionSHA256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestToolSchemaAndPermissionDecisionsAreExactAndDurable(t *testing.T) {
	ctx := context.Background()
	repositories, task := createTaskFixture(t, 1300)
	runID := createToolTestRun(t, repositories, task.ID, 1304)
	first, err := repositories.RecordRunToolSchema(ctx, runID, 1)
	if err != nil {
		t.Fatal(err)
	}
	retried, err := repositories.RecordRunToolSchema(ctx, runID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if retried != first {
		t.Fatalf("schema retry = %#v, want %#v", retried, first)
	}
	if _, err := repositories.RecordRunToolSchema(ctx, runID, 2); !errors.Is(err, ErrConflict) {
		t.Fatalf("schema conflict = %v", err)
	}

	approval := createResolvedToolApproval(
		t, repositories, task.ID, 1305, domain.ApprovalRequestStateGranted,
	)
	mode := "allow-once"
	decision, err := repositories.RecordPermissionDecision(ctx, RecordPermissionDecision{
		ID: "permission-1300", ApprovalID: &approval.ID, TaskID: task.ID,
		Decision: "granted", Capability: "network",
		ResourceScope: "worktree", Reason: "user approved test",
		Requester: "local-user", ToolName: "run-command",
		ActionSHA256:          fixtureActionSHA256,
		ArgumentsRedactedJSON: `["curl","[REDACTED]"]`,
		SideEffectsJSON:       `["network"]`, GrantMode: &mode,
	})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Used {
		t.Fatal("new one-time permission is already used")
	}
	if err := repositories.UseOncePermissionDecision(
		ctx, decision.ID, task.ID, fixtureActionSHA256,
	); err != nil {
		t.Fatal(err)
	}
	if err := repositories.UseOncePermissionDecision(
		ctx, decision.ID, task.ID, fixtureActionSHA256,
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("second one-time use = %v", err)
	}
	listed, err := repositories.ListPermissionDecisions(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || !listed[0].Used ||
		listed[0].Requester != "local-user" ||
		listed[0].ArgumentsRedactedJSON != `["curl","[REDACTED]"]` {
		t.Fatalf("listed decisions = %#v", listed)
	}
}

func TestCustomCommandsAreTypedApprovedVersionedAndImmutable(t *testing.T) {
	ctx := context.Background()
	repositories, task := createTaskFixture(t, 1320)
	approval := createResolvedToolApproval(
		t, repositories, task.ID, 1325, domain.ApprovalRequestStateGranted,
	)
	literal := "test"
	placeholder := "package"
	command, err := repositories.CreateCustomCommand(ctx, CreateCustomCommand{
		ID: "command-1320", RepositoryID: task.RepositoryID,
		Name: "test-package", Executable: "go",
		ArgumentsTemplate: []CommandArgumentTemplate{
			{Literal: &literal}, {Placeholder: &placeholder},
		},
		Placeholders: []string{"package"}, CommandVersion: "v1",
		Source: "repository", ApprovalID: &approval.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	read, err := repositories.GetCustomCommand(ctx, command.ID)
	if err != nil {
		t.Fatal(err)
	}
	if read.CommandVersion != "v1" || read.Source != "repository" ||
		len(read.ArgumentsTemplate) != 2 ||
		*read.ArgumentsTemplate[1].Placeholder != "package" {
		t.Fatalf("custom command = %#v", read)
	}
	if _, err := repositories.database.sql.ExecContext(
		ctx, `UPDATE custom_commands SET executable = 'evil' WHERE id = ?`, command.ID,
	); !errors.Is(classify("rewrite custom command", err), ErrConstraint) {
		t.Fatalf("immutable command error = %v", err)
	}
	malicious := "$(touch should-not-run)"
	userCommand, err := repositories.CreateCustomCommand(ctx, CreateCustomCommand{
		ID: "command-1321", RepositoryID: task.RepositoryID,
		Name: "literal-safety", Executable: "printf",
		ArgumentsTemplate: []CommandArgumentTemplate{{Literal: &malicious}},
		Placeholders:      []string{}, CommandVersion: "v1", Source: "user",
	})
	if err != nil {
		t.Fatal(err)
	}
	if *userCommand.ArgumentsTemplate[0].Literal != malicious {
		t.Fatal("literal argument was interpreted or changed")
	}
}

func TestRepositoryCustomCommandRequiresSameRepositoryApproval(t *testing.T) {
	repositories, task := createTaskFixture(t, 1340)
	literal := "test"
	if _, err := repositories.CreateCustomCommand(context.Background(), CreateCustomCommand{
		ID: "command-1340", RepositoryID: task.RepositoryID,
		Name: "unapproved", Executable: "go",
		ArgumentsTemplate: []CommandArgumentTemplate{{Literal: &literal}},
		CommandVersion:    "v1", Source: "repository",
	}); err == nil || !strings.Contains(err.Error(), "first-use approval") {
		t.Fatalf("unapproved repository command error = %v", err)
	}
}

func createToolTestRun(
	t *testing.T,
	repositories *Repositories,
	taskID domain.TaskID,
	number int,
) domain.RunID {
	t.Helper()
	runID, err := domain.NewRunID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.database.sql.ExecContext(
		context.Background(),
		`INSERT INTO runs (
			id, task_id, state, attempt, task_revision, idempotency_key,
			created_at_unix_micros, updated_at_unix_micros
		) VALUES (?, ?, 'pending', 1, 0, ?, 1, 1)`,
		runID, taskID, "tool-run-"+runID.String(),
	); err != nil {
		t.Fatal(err)
	}
	return runID
}

func createResolvedToolApproval(
	t *testing.T,
	repositories *Repositories,
	taskID domain.TaskID,
	number int,
	state domain.ApprovalRequestState,
) Approval {
	t.Helper()
	approval, err := repositories.CreateApproval(context.Background(), CreateApproval{
		ID: testApprovalID(t, number), TaskID: taskID,
		Scope: "tool-authority", RequestReason: "fixture",
		IdempotencyKey: "tool-approval-" + testUUID(number),
	})
	if err != nil {
		t.Fatal(err)
	}
	approval, err = repositories.ResolveApproval(context.Background(), ResolveApproval{
		ID: approval.ID, ExpectedRevision: approval.Revision,
		To: state, ResolutionReason: "fixture decision",
	})
	if err != nil {
		t.Fatal(err)
	}
	return approval
}
