package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

// CommandExecution is one durable mediated subprocess action.
type CommandExecution struct {
	ID                    string
	TaskID                domain.TaskID
	RunID                 domain.RunID
	ApprovalID            *domain.ApprovalID
	State                 domain.CommandExecutionState
	CommandName           string
	ArgumentsRedactedJSON string
	WorkingDirectoryScope string
	IdempotencyKey        string
	ToolSchemaVersion     int
	ExecutablePath        *string
	ExitCode              *int
	DurationMillis        *int64
	TimedOut              *bool
	Cancelled             *bool
	StdoutTruncated       *bool
	StderrTruncated       *bool
	StartedAt             *time.Time
	CompletedAt           *time.Time
	CreatedAt             time.Time
	UpdatedAt             time.Time
	Revision              uint64
}

// CreateCommandExecution declares a fully attributed command intent.
type CreateCommandExecution struct {
	ID                    string
	TaskID                domain.TaskID
	RunID                 domain.RunID
	ApprovalID            *domain.ApprovalID
	State                 domain.CommandExecutionState
	CommandName           string
	ArgumentsRedactedJSON string
	WorkingDirectoryScope string
	IdempotencyKey        string
	ToolSchemaVersion     int
}

// TransitionCommandExecution applies one optimistic command lifecycle change.
type TransitionCommandExecution struct {
	ID               string
	ExpectedRevision uint64
	To               domain.CommandExecutionState
	ExecutablePath   *string
	ExitCode         *int
	DurationMillis   *int64
	TimedOut         *bool
	Cancelled        *bool
	StdoutTruncated  *bool
	StderrTruncated  *bool
}

// RedactedCommandOutput is one bounded, already-redacted output chunk.
type RedactedCommandOutput struct {
	ID                 string
	CommandExecutionID string
	Stream             string
	Sequence           int
	ContentRedacted    string
	ByteCount          int
	Truncated          bool
	CreatedAt          time.Time
}

// AppendRedactedCommandOutput declares one immutable output chunk.
type AppendRedactedCommandOutput struct {
	ID                 string
	CommandExecutionID string
	Stream             string
	Sequence           int
	ContentRedacted    string
	Truncated          bool
}

func (repositories *Repositories) CreateCommandExecution(
	ctx context.Context,
	input CreateCommandExecution,
) (CommandExecution, error) {
	if err := validateCreateCommandExecution(input); err != nil {
		return CommandExecution{}, err
	}
	now, micros := repositories.timestamp()
	var command CommandExecution
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		existing, found, err := findCommandExecutionByIdempotency(
			ctx, transaction, input.RunID, input.IdempotencyKey,
		)
		if err != nil {
			return err
		}
		if found {
			if existing.ID != input.ID || existing.TaskID != input.TaskID ||
				existing.CommandName != input.CommandName ||
				existing.ArgumentsRedactedJSON != input.ArgumentsRedactedJSON ||
				existing.WorkingDirectoryScope != input.WorkingDirectoryScope ||
				existing.ToolSchemaVersion != input.ToolSchemaVersion {
				return typedError(ErrConflict, "create command execution", errors.New("idempotency key reused with different command"))
			}
			command = existing
			return nil
		}
		var count int
		if err := transaction.sql.QueryRowContext(
			ctx,
			`SELECT count(*) FROM runs WHERE id = ? AND task_id = ?`,
			input.RunID, input.TaskID,
		).Scan(&count); err != nil {
			return classify("verify command run", err)
		}
		if count != 1 {
			return typedError(ErrConstraint, "create command execution", errors.New("run does not belong to task"))
		}
		if input.ApprovalID != nil {
			if err := verifyGrantedApproval(ctx, transaction, *input.ApprovalID, input.TaskID); err != nil {
				return err
			}
		}
		_, err = transaction.sql.ExecContext(
			ctx,
			`INSERT INTO command_executions (
				id, task_id, run_id, approval_id, state, command_name,
				arguments_redacted_json, working_directory_scope,
				idempotency_key, created_at_unix_micros,
				updated_at_unix_micros, tool_schema_version
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			input.ID, input.TaskID, input.RunID, nullableApprovalID(input.ApprovalID),
			input.State, input.CommandName, input.ArgumentsRedactedJSON,
			input.WorkingDirectoryScope, input.IdempotencyKey, micros, micros,
			input.ToolSchemaVersion,
		)
		if err != nil {
			return repositoryWriteError("create command execution", err)
		}
		command = CommandExecution{
			ID: input.ID, TaskID: input.TaskID, RunID: input.RunID,
			ApprovalID: input.ApprovalID, State: input.State,
			CommandName:           input.CommandName,
			ArgumentsRedactedJSON: input.ArgumentsRedactedJSON,
			WorkingDirectoryScope: input.WorkingDirectoryScope,
			IdempotencyKey:        input.IdempotencyKey,
			ToolSchemaVersion:     input.ToolSchemaVersion,
			CreatedAt:             now, UpdatedAt: now,
		}
		return nil
	})
	return command, err
}

func (repositories *Repositories) TransitionCommandExecution(
	ctx context.Context,
	input TransitionCommandExecution,
) (CommandExecution, error) {
	if err := validateBounded("command execution ID", input.ID, 255); err != nil {
		return CommandExecution{}, err
	}
	var updated CommandExecution
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		current, err := getCommandExecution(ctx, transaction.sql, input.ID)
		if err != nil {
			return err
		}
		if current.Revision != input.ExpectedRevision {
			return typedError(ErrStaleRevision, "transition command execution", errors.New("command revision changed"))
		}
		approvalState := domain.ApprovalRequestStatePending
		if current.ApprovalID != nil {
			approvalState = domain.ApprovalRequestStateGranted
		}
		if err := domain.ValidateCommandExecutionTransition(domain.CommandExecutionTransition{
			From: current.State, To: input.To, Approval: approvalState,
		}); err != nil {
			return typedError(ErrConstraint, "transition command execution", err)
		}
		if err := validateCommandOutcome(input); err != nil {
			return err
		}
		_, micros := repositories.timestamp()
		started := current.StartedAt
		completed := current.CompletedAt
		if input.To == domain.CommandExecutionStateRunning {
			started = timePointer(repositoryTime(micros))
		}
		if input.To.IsTerminal() {
			completed = timePointer(repositoryTime(micros))
		}
		result, err := transaction.sql.ExecContext(
			ctx,
			`UPDATE command_executions SET
				state = ?, executable_path = COALESCE(?, executable_path),
				exit_code = ?, duration_millis = ?, timed_out = ?,
				cancelled = ?, stdout_truncated = ?, stderr_truncated = ?,
				started_at_unix_micros = ?, completed_at_unix_micros = ?,
				updated_at_unix_micros = ?, revision = revision + 1
			 WHERE id = ? AND revision = ?`,
			input.To, nullableString(input.ExecutablePath), nullableInt(input.ExitCode),
			nullableInt64(input.DurationMillis), nullableBool(input.TimedOut),
			nullableBool(input.Cancelled), nullableBool(input.StdoutTruncated),
			nullableBool(input.StderrTruncated), nullableTime(started),
			nullableTime(completed), micros, input.ID, input.ExpectedRevision,
		)
		if err != nil {
			return repositoryWriteError("transition command execution", err)
		}
		changed, _ := result.RowsAffected()
		if changed != 1 {
			return typedError(ErrStaleRevision, "transition command execution", errors.New("command revision changed"))
		}
		updated, err = getCommandExecution(ctx, transaction.sql, input.ID)
		return err
	})
	return updated, err
}

func (repositories *Repositories) AppendRedactedCommandOutput(
	ctx context.Context,
	input AppendRedactedCommandOutput,
) (RedactedCommandOutput, error) {
	if err := validateBounded("redacted output ID", input.ID, 255); err != nil {
		return RedactedCommandOutput{}, err
	}
	if err := validateBounded("command execution ID", input.CommandExecutionID, 255); err != nil {
		return RedactedCommandOutput{}, err
	}
	if input.Stream != "stdout" && input.Stream != "stderr" {
		return RedactedCommandOutput{}, errors.New("output stream must be stdout or stderr")
	}
	if input.Sequence < 1 || len(input.ContentRedacted) > 64<<10 {
		return RedactedCommandOutput{}, errors.New("output sequence or content exceeds bounds")
	}
	now, micros := repositories.timestamp()
	_, err := repositories.database.sql.ExecContext(
		ctx,
		`INSERT INTO redacted_outputs (
			id, command_execution_id, stream, sequence, content_redacted,
			byte_count, truncated, created_at_unix_micros
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		input.ID, input.CommandExecutionID, input.Stream, input.Sequence,
		input.ContentRedacted, len([]byte(input.ContentRedacted)), input.Truncated, micros,
	)
	if err != nil {
		return RedactedCommandOutput{}, repositoryWriteError("append redacted command output", err)
	}
	return RedactedCommandOutput{
		ID: input.ID, CommandExecutionID: input.CommandExecutionID,
		Stream: input.Stream, Sequence: input.Sequence,
		ContentRedacted: input.ContentRedacted,
		ByteCount:       len([]byte(input.ContentRedacted)),
		Truncated:       input.Truncated, CreatedAt: now,
	}, nil
}

func validateCreateCommandExecution(input CreateCommandExecution) error {
	if input.TaskID.IsZero() || input.RunID.IsZero() {
		return errors.New("task and run IDs are required")
	}
	for label, value := range map[string]string{
		"command execution ID": input.ID, "command name": input.CommandName,
		"command working directory": input.WorkingDirectoryScope,
		"command idempotency key":   input.IdempotencyKey,
	} {
		maximum := 255
		if label == "command working directory" {
			maximum = 4096
		}
		if err := validateBounded(label, value, maximum); err != nil {
			return err
		}
	}
	if input.State != domain.CommandExecutionStateAwaitingAuthority &&
		input.State != domain.CommandExecutionStateAuthorized {
		return errors.New("command must begin awaiting authority or authorized")
	}
	if input.State == domain.CommandExecutionStateAwaitingAuthority && input.ApprovalID != nil {
		return errors.New("awaiting command must not claim a granted approval")
	}
	if input.ToolSchemaVersion < 1 {
		return errors.New("positive tool schema version is required")
	}
	return validateJSONArray("command arguments", input.ArgumentsRedactedJSON, 64<<10)
}

func validateCommandOutcome(input TransitionCommandExecution) error {
	if !input.To.IsTerminal() {
		return nil
	}
	if input.DurationMillis == nil || *input.DurationMillis < 0 ||
		input.TimedOut == nil || input.Cancelled == nil ||
		input.StdoutTruncated == nil || input.StderrTruncated == nil {
		return errors.New("terminal command outcome metadata is required")
	}
	if input.To != domain.CommandExecutionStateCancelled && input.ExitCode == nil {
		return errors.New("non-cancelled command outcome requires an exit code")
	}
	return nil
}

func findCommandExecutionByIdempotency(
	ctx context.Context,
	transaction *Transaction,
	runID domain.RunID,
	key string,
) (CommandExecution, bool, error) {
	command, err := getCommandExecutionByQuery(
		ctx, transaction.sql,
		`SELECT `+commandExecutionColumns+` FROM command_executions
		 WHERE run_id = ? AND idempotency_key = ?`,
		runID, key,
	)
	if errors.Is(err, ErrNotFound) {
		return CommandExecution{}, false, nil
	}
	return command, err == nil, err
}

func getCommandExecution(ctx context.Context, queryer rowQueryer, id string) (CommandExecution, error) {
	return getCommandExecutionByQuery(
		ctx, queryer,
		`SELECT `+commandExecutionColumns+` FROM command_executions WHERE id = ?`,
		id,
	)
}

const commandExecutionColumns = `
	id, task_id, run_id, approval_id, state, command_name,
	arguments_redacted_json, working_directory_scope, idempotency_key,
	exit_code, started_at_unix_micros, completed_at_unix_micros,
	created_at_unix_micros, updated_at_unix_micros, revision,
	tool_schema_version, executable_path, duration_millis, timed_out,
	cancelled, stdout_truncated, stderr_truncated`

type rowQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func getCommandExecutionByQuery(
	ctx context.Context,
	queryer rowQueryer,
	query string,
	arguments ...any,
) (CommandExecution, error) {
	var command CommandExecution
	var approval, executable sql.NullString
	var exitCode, started, completed, duration sql.NullInt64
	var timedOut, cancelled, stdoutTruncated, stderrTruncated sql.NullBool
	var created, updated int64
	err := queryer.QueryRowContext(ctx, query, arguments...).Scan(
		&command.ID, &command.TaskID, &command.RunID, &approval, &command.State,
		&command.CommandName, &command.ArgumentsRedactedJSON,
		&command.WorkingDirectoryScope, &command.IdempotencyKey, &exitCode,
		&started, &completed, &created, &updated, &command.Revision,
		&command.ToolSchemaVersion, &executable, &duration, &timedOut,
		&cancelled, &stdoutTruncated, &stderrTruncated,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return CommandExecution{}, typedError(ErrNotFound, "get command execution", err)
	}
	if err != nil {
		return CommandExecution{}, classify("get command execution", err)
	}
	if approval.Valid {
		id, err := domain.ParseApprovalID(approval.String)
		if err != nil {
			return CommandExecution{}, typedError(ErrCorrupt, "decode command approval", err)
		}
		command.ApprovalID = &id
	}
	command.ExecutablePath = nullStringPointer(executable)
	command.ExitCode = nullIntPointer(exitCode)
	command.DurationMillis = nullInt64Pointer(duration)
	command.TimedOut = nullBoolPointer(timedOut)
	command.Cancelled = nullBoolPointer(cancelled)
	command.StdoutTruncated = nullBoolPointer(stdoutTruncated)
	command.StderrTruncated = nullBoolPointer(stderrTruncated)
	command.StartedAt = nullTimePointer(started)
	command.CompletedAt = nullTimePointer(completed)
	command.CreatedAt = repositoryTime(created)
	command.UpdatedAt = repositoryTime(updated)
	return command, nil
}

func verifyGrantedApproval(
	ctx context.Context,
	transaction *Transaction,
	id domain.ApprovalID,
	taskID domain.TaskID,
) error {
	var count int
	if err := transaction.sql.QueryRowContext(
		ctx,
		`SELECT count(*) FROM approvals
		 WHERE id = ? AND task_id = ? AND state = 'granted'`,
		id, taskID,
	).Scan(&count); err != nil {
		return classify("verify command approval", err)
	}
	if count != 1 {
		return typedError(ErrConstraint, "verify command approval", errors.New("command approval is absent or not granted"))
	}
	return nil
}

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableBool(value *bool) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().UnixMicro()
}

func nullStringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func nullIntPointer(value sql.NullInt64) *int {
	if !value.Valid {
		return nil
	}
	result := int(value.Int64)
	return &result
}

func nullInt64Pointer(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	return &value.Int64
}

func nullBoolPointer(value sql.NullBool) *bool {
	if !value.Valid {
		return nil
	}
	return &value.Bool
}

func nullTimePointer(value sql.NullInt64) *time.Time {
	if !value.Valid {
		return nil
	}
	result := repositoryTime(value.Int64)
	return &result
}

func timePointer(value time.Time) *time.Time {
	return &value
}
