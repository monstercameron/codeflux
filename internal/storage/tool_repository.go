package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

// RunToolSchema binds one run to an immutable tool contract version.
type RunToolSchema struct {
	RunID         domain.RunID
	SchemaVersion int
	CreatedAt     time.Time
}

// PermissionDecision is one immutable exact-action authority fact.
type PermissionDecision struct {
	ID                    string
	ApprovalID            *domain.ApprovalID
	TaskID                domain.TaskID
	Decision              string
	Capability            string
	ResourceScope         string
	Reason                string
	Requester             string
	ToolName              string
	ActionSHA256          string
	ArgumentsRedactedJSON string
	SideEffectsJSON       string
	GrantMode             *string
	CreatedAt             time.Time
	Used                  bool
}

// RecordPermissionDecision declares one attributed approval outcome.
type RecordPermissionDecision struct {
	ID                    string
	ApprovalID            *domain.ApprovalID
	TaskID                domain.TaskID
	Decision              string
	Capability            string
	ResourceScope         string
	Reason                string
	Requester             string
	ToolName              string
	ActionSHA256          string
	ArgumentsRedactedJSON string
	SideEffectsJSON       string
	GrantMode             *string
}

// CommandArgumentTemplate is exactly one literal or typed placeholder.
type CommandArgumentTemplate struct {
	Literal     *string `json:"literal,omitempty"`
	Placeholder *string `json:"placeholder,omitempty"`
}

// CustomCommand is one immutable reviewed array-based command definition.
type CustomCommand struct {
	ID                string
	RepositoryID      domain.RepositoryID
	Name              string
	Executable        string
	ArgumentsTemplate []CommandArgumentTemplate
	Placeholders      []string
	CommandVersion    string
	Source            string
	ApprovalID        *domain.ApprovalID
	Approved          bool
	CreatedAt         time.Time
}

// CreateCustomCommand declares a reviewed command definition.
type CreateCustomCommand struct {
	ID                string
	RepositoryID      domain.RepositoryID
	Name              string
	Executable        string
	ArgumentsTemplate []CommandArgumentTemplate
	Placeholders      []string
	CommandVersion    string
	Source            string
	ApprovalID        *domain.ApprovalID
}

// ToolAuthorityOperations groups tool-schema, permission, and custom-command facts.
type ToolAuthorityOperations interface {
	RecordRunToolSchema(context.Context, domain.RunID, int) (RunToolSchema, error)
	RecordPermissionDecision(context.Context, RecordPermissionDecision) (PermissionDecision, error)
	ListPermissionDecisions(context.Context, domain.TaskID) ([]PermissionDecision, error)
	UseOncePermissionDecision(context.Context, string, domain.TaskID, string) error
	CreateCustomCommand(context.Context, CreateCustomCommand) (CustomCommand, error)
	GetCustomCommand(context.Context, string) (CustomCommand, error)
}

func (repositories *Repositories) RecordRunToolSchema(
	ctx context.Context,
	runID domain.RunID,
	version int,
) (RunToolSchema, error) {
	if runID.IsZero() || version < 1 {
		return RunToolSchema{}, errors.New("run ID and positive tool schema version are required")
	}
	now, micros := repositories.timestamp()
	binding := RunToolSchema{RunID: runID, SchemaVersion: version, CreatedAt: now}
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		var existing RunToolSchema
		var createdMicros int64
		err := transaction.sql.QueryRowContext(
			ctx,
			`SELECT run_id, schema_version, created_at_unix_micros
			 FROM run_tool_schemas WHERE run_id = ?`,
			runID,
		).Scan(&existing.RunID, &existing.SchemaVersion, &createdMicros)
		if err == nil {
			existing.CreatedAt = repositoryTime(createdMicros)
			if existing.SchemaVersion != version {
				return typedError(
					ErrConflict,
					"record run tool schema",
					errors.New("run already uses a different tool schema"),
				)
			}
			binding = existing
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return classify("find run tool schema", err)
		}
		_, err = transaction.sql.ExecContext(
			ctx,
			`INSERT INTO run_tool_schemas (
				run_id, schema_version, created_at_unix_micros
			) VALUES (?, ?, ?)`,
			runID,
			version,
			micros,
		)
		return repositoryWriteError("record run tool schema", err)
	})
	return binding, err
}

func (repositories *Repositories) RecordPermissionDecision(
	ctx context.Context,
	input RecordPermissionDecision,
) (PermissionDecision, error) {
	if err := validatePermissionDecision(input); err != nil {
		return PermissionDecision{}, err
	}
	now, micros := repositories.timestamp()
	decision := PermissionDecision{
		ID: input.ID, ApprovalID: input.ApprovalID, TaskID: input.TaskID,
		Decision: input.Decision, Capability: input.Capability,
		ResourceScope: input.ResourceScope, Reason: input.Reason,
		Requester: input.Requester, ToolName: input.ToolName,
		ActionSHA256:          input.ActionSHA256,
		ArgumentsRedactedJSON: input.ArgumentsRedactedJSON,
		SideEffectsJSON:       input.SideEffectsJSON, GrantMode: input.GrantMode,
		CreatedAt: now,
	}
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		if input.ApprovalID != nil {
			var taskID domain.TaskID
			var state domain.ApprovalRequestState
			if err := transaction.sql.QueryRowContext(
				ctx,
				`SELECT task_id, state FROM approvals WHERE id = ?`,
				*input.ApprovalID,
			).Scan(&taskID, &state); err != nil {
				return classify("read permission approval", err)
			}
			expected := domain.ApprovalRequestStateGranted
			if input.Decision == "denied" {
				expected = domain.ApprovalRequestStateDenied
			}
			if taskID != input.TaskID || state != expected {
				return typedError(
					ErrConstraint,
					"record permission decision",
					errors.New("approval does not match task and decision"),
				)
			}
		}
		_, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO permission_decisions (
				id, approval_id, task_id, decision, capability,
				resource_scope, reason, created_at_unix_micros,
				requester, tool_name, action_sha256,
				arguments_redacted_json, side_effects_json, grant_mode
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			input.ID,
			nullableApprovalID(input.ApprovalID),
			input.TaskID,
			input.Decision,
			input.Capability,
			input.ResourceScope,
			input.Reason,
			micros,
			input.Requester,
			input.ToolName,
			input.ActionSHA256,
			input.ArgumentsRedactedJSON,
			input.SideEffectsJSON,
			nullableString(input.GrantMode),
		)
		return repositoryWriteError("record permission decision", err)
	})
	return decision, err
}

func (repositories *Repositories) ListPermissionDecisions(
	ctx context.Context,
	taskID domain.TaskID,
) ([]PermissionDecision, error) {
	if taskID.IsZero() {
		return nil, errors.New("task ID must not be empty")
	}
	rows, err := repositories.database.sql.QueryContext(
		ctx,
		`SELECT decision.id, decision.approval_id, decision.task_id,
		        decision.decision, decision.capability,
		        decision.resource_scope, decision.reason,
		        decision.requester, decision.tool_name, decision.action_sha256,
		        decision.arguments_redacted_json, decision.side_effects_json,
		        decision.grant_mode, decision.created_at_unix_micros,
		        EXISTS (
		            SELECT 1 FROM permission_grant_uses use
		            WHERE use.permission_decision_id = decision.id
		        )
		 FROM permission_decisions decision
		 WHERE decision.task_id = ?
		 ORDER BY decision.created_at_unix_micros, decision.id`,
		taskID,
	)
	if err != nil {
		return nil, classify("list permission decisions", err)
	}
	defer rows.Close()
	var decisions []PermissionDecision
	for rows.Next() {
		decision, err := scanPermissionDecision(rows)
		if err != nil {
			return nil, err
		}
		decisions = append(decisions, decision)
	}
	if err := rows.Err(); err != nil {
		return nil, classify("iterate permission decisions", err)
	}
	return decisions, nil
}

func (repositories *Repositories) UseOncePermissionDecision(
	ctx context.Context,
	id string,
	taskID domain.TaskID,
	actionSHA256 string,
) error {
	if err := validateBounded("permission decision ID", id, 255); err != nil {
		return err
	}
	if taskID.IsZero() {
		return errors.New("task ID must not be empty")
	}
	if err := validateSHA256("permission action hash", actionSHA256); err != nil {
		return err
	}
	_, micros := repositories.timestamp()
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		var count int
		if err := transaction.sql.QueryRowContext(
			ctx,
			`SELECT count(*)
			 FROM permission_decisions decision
			 JOIN tasks task ON task.id = decision.task_id
			 WHERE decision.id = ? AND decision.task_id = ?
			   AND decision.decision = 'granted'
			   AND decision.grant_mode = 'allow-once'
			   AND decision.action_sha256 = ?
			   AND task.state NOT IN (
			       'completed', 'failed', 'cancelled', 'rolled-back'
			   )`,
			id,
			taskID,
			actionSHA256,
		).Scan(&count); err != nil {
			return classify("verify one-use permission", err)
		}
		if count != 1 {
			return typedError(
				ErrConstraint,
				"use one-time permission",
				errors.New("permission is unavailable or expired"),
			)
		}
		_, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO permission_grant_uses (
				permission_decision_id, task_id, action_sha256,
				used_at_unix_micros
			) VALUES (?, ?, ?, ?)`,
			id,
			taskID,
			actionSHA256,
			micros,
		)
		return repositoryWriteError("use one-time permission", err)
	})
	return err
}

func (repositories *Repositories) CreateCustomCommand(
	ctx context.Context,
	input CreateCustomCommand,
) (CustomCommand, error) {
	if err := validateCustomCommand(input); err != nil {
		return CustomCommand{}, err
	}
	argumentsJSON, err := json.Marshal(input.ArgumentsTemplate)
	if err != nil {
		return CustomCommand{}, fmt.Errorf("encode command arguments: %w", err)
	}
	placeholdersJSON, err := json.Marshal(input.Placeholders)
	if err != nil {
		return CustomCommand{}, fmt.Errorf("encode command placeholders: %w", err)
	}
	now, micros := repositories.timestamp()
	command := CustomCommand{
		ID: input.ID, RepositoryID: input.RepositoryID, Name: input.Name,
		Executable:        input.Executable,
		ArgumentsTemplate: append([]CommandArgumentTemplate(nil), input.ArgumentsTemplate...),
		Placeholders:      append([]string(nil), input.Placeholders...),
		CommandVersion:    input.CommandVersion, Source: input.Source,
		ApprovalID: input.ApprovalID, Approved: true, CreatedAt: now,
	}
	err = repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		if input.Source == "repository" {
			var count int
			if err := transaction.sql.QueryRowContext(
				ctx,
				`SELECT count(*)
				 FROM approvals approval
				 JOIN tasks task ON task.id = approval.task_id
				 WHERE approval.id = ? AND approval.state = 'granted'
				   AND task.repository_id = ?`,
				*input.ApprovalID,
				input.RepositoryID,
			).Scan(&count); err != nil {
				return classify("verify custom command approval", err)
			}
			if count != 1 {
				return typedError(
					ErrConstraint,
					"create custom command",
					errors.New("repository command lacks first-use approval"),
				)
			}
		}
		_, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO custom_commands (
				id, repository_id, name, executable,
				arguments_template_json, placeholders_json,
				command_version, source, approval_id, approved,
				created_at_unix_micros
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?)`,
			input.ID,
			input.RepositoryID,
			input.Name,
			input.Executable,
			string(argumentsJSON),
			string(placeholdersJSON),
			input.CommandVersion,
			input.Source,
			nullableApprovalID(input.ApprovalID),
			micros,
		)
		return repositoryWriteError("create custom command", err)
	})
	return command, err
}

func (repositories *Repositories) GetCustomCommand(
	ctx context.Context,
	id string,
) (CustomCommand, error) {
	if err := validateBounded("custom command ID", id, 255); err != nil {
		return CustomCommand{}, err
	}
	var (
		command          CustomCommand
		approvalRaw      sql.NullString
		argumentsJSON    string
		placeholdersJSON string
		createdMicros    int64
	)
	err := repositories.database.sql.QueryRowContext(
		ctx,
		`SELECT id, repository_id, name, executable,
		        arguments_template_json, placeholders_json,
		        command_version, source, approval_id, approved,
		        created_at_unix_micros
		 FROM custom_commands WHERE id = ?`,
		id,
	).Scan(
		&command.ID,
		&command.RepositoryID,
		&command.Name,
		&command.Executable,
		&argumentsJSON,
		&placeholdersJSON,
		&command.CommandVersion,
		&command.Source,
		&approvalRaw,
		&command.Approved,
		&createdMicros,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return CustomCommand{}, typedError(ErrNotFound, "get custom command", err)
	}
	if err != nil {
		return CustomCommand{}, classify("get custom command", err)
	}
	if err := json.Unmarshal([]byte(argumentsJSON), &command.ArgumentsTemplate); err != nil {
		return CustomCommand{}, classify("decode custom command arguments", err)
	}
	if err := json.Unmarshal([]byte(placeholdersJSON), &command.Placeholders); err != nil {
		return CustomCommand{}, classify("decode custom command placeholders", err)
	}
	if approvalRaw.Valid {
		approval, err := domain.ParseApprovalID(approvalRaw.String)
		if err != nil {
			return CustomCommand{}, classify("decode custom command approval", err)
		}
		command.ApprovalID = &approval
	}
	command.CreatedAt = repositoryTime(createdMicros)
	return command, nil
}

func validatePermissionDecision(input RecordPermissionDecision) error {
	if input.TaskID.IsZero() {
		return errors.New("task ID must not be empty")
	}
	for label, value := range map[string]string{
		"permission decision ID": input.ID,
		"permission capability":  input.Capability,
		"permission scope":       input.ResourceScope,
		"permission reason":      input.Reason,
		"permission requester":   input.Requester,
		"permission tool name":   input.ToolName,
	} {
		maximum := 255
		if label == "permission scope" || label == "permission reason" {
			maximum = 2048
		}
		if err := validateBounded(label, value, maximum); err != nil {
			return err
		}
	}
	if input.Decision != "granted" && input.Decision != "denied" {
		return errors.New("permission decision must be granted or denied")
	}
	if err := validateSHA256("permission action hash", input.ActionSHA256); err != nil {
		return err
	}
	if err := validateJSONArray("permission arguments", input.ArgumentsRedactedJSON, 64<<10); err != nil {
		return err
	}
	if err := validateJSONArray("permission side effects", input.SideEffectsJSON, 8<<10); err != nil {
		return err
	}
	if input.Decision == "granted" {
		if input.GrantMode == nil ||
			(*input.GrantMode != "allow-once" && *input.GrantMode != "allow-for-task") {
			return errors.New("granted permission requires a valid grant mode")
		}
	} else if input.GrantMode != nil {
		return errors.New("denied permission must not have a grant mode")
	}
	return nil
}

func validateCustomCommand(input CreateCustomCommand) error {
	if input.RepositoryID.IsZero() {
		return errors.New("repository ID must not be empty")
	}
	for label, value := range map[string]string{
		"custom command ID":      input.ID,
		"custom command name":    input.Name,
		"custom executable":      input.Executable,
		"custom command version": input.CommandVersion,
	} {
		maximum := 255
		if label == "custom executable" {
			maximum = 4096
		}
		if err := validateBounded(label, value, maximum); err != nil {
			return err
		}
	}
	if input.Source != "user" && input.Source != "repository" && input.Source != "plugin" {
		return errors.New("custom command source is invalid")
	}
	if input.Source == "repository" && (input.ApprovalID == nil || input.ApprovalID.IsZero()) {
		return errors.New("repository command requires first-use approval")
	}
	placeholderSet := make(map[string]struct{}, len(input.Placeholders))
	for _, placeholder := range input.Placeholders {
		if err := validateBounded("command placeholder", placeholder, 255); err != nil {
			return err
		}
		placeholderSet[placeholder] = struct{}{}
	}
	if len(input.ArgumentsTemplate) > 128 {
		return errors.New("custom command has too many arguments")
	}
	for _, argument := range input.ArgumentsTemplate {
		hasLiteral := argument.Literal != nil
		hasPlaceholder := argument.Placeholder != nil
		if hasLiteral == hasPlaceholder {
			return errors.New("command argument must be exactly one literal or placeholder")
		}
		if hasLiteral && len(*argument.Literal) > 32<<10 {
			return errors.New("command literal exceeds maximum length")
		}
		if hasPlaceholder {
			if _, exists := placeholderSet[*argument.Placeholder]; !exists {
				return errors.New("command argument references an undeclared placeholder")
			}
		}
	}
	return nil
}

func validateJSONArray(label, value string, maximum int) error {
	if len(value) == 0 || len(value) > maximum {
		return errors.New(label + " JSON is outside supported bounds")
	}
	var array []json.RawMessage
	if err := json.Unmarshal([]byte(value), &array); err != nil || array == nil {
		return errors.New(label + " must be one JSON array")
	}
	return nil
}

func scanPermissionDecision(row rowScanner) (PermissionDecision, error) {
	var (
		decision    PermissionDecision
		approvalRaw sql.NullString
		grantRaw    sql.NullString
		created     int64
	)
	if err := row.Scan(
		&decision.ID,
		&approvalRaw,
		&decision.TaskID,
		&decision.Decision,
		&decision.Capability,
		&decision.ResourceScope,
		&decision.Reason,
		&decision.Requester,
		&decision.ToolName,
		&decision.ActionSHA256,
		&decision.ArgumentsRedactedJSON,
		&decision.SideEffectsJSON,
		&grantRaw,
		&created,
		&decision.Used,
	); err != nil {
		return PermissionDecision{}, classify("scan permission decision", err)
	}
	if approvalRaw.Valid {
		id, err := domain.ParseApprovalID(approvalRaw.String)
		if err != nil {
			return PermissionDecision{}, classify("decode permission approval", err)
		}
		decision.ApprovalID = &id
	}
	if grantRaw.Valid {
		decision.GrantMode = &grantRaw.String
	}
	decision.CreatedAt = repositoryTime(created)
	return decision, nil
}

func nullableApprovalID(id *domain.ApprovalID) any {
	if id == nil {
		return nil
	}
	return *id
}

var _ ToolAuthorityOperations = (*Repositories)(nil)
