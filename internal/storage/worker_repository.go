package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

type WorkerLeaseState string

const (
	WorkerLeaseStarting WorkerLeaseState = "starting"
	WorkerLeaseRunning  WorkerLeaseState = "running"
	WorkerLeasePaused   WorkerLeaseState = "paused"
	WorkerLeaseStopping WorkerLeaseState = "stopping"
	WorkerLeaseExited   WorkerLeaseState = "exited"
	WorkerLeaseCrashed  WorkerLeaseState = "crashed"
	WorkerLeaseExpired  WorkerLeaseState = "expired"
)

const (
	WorkerRecoveryHeartbeatExpired     = "worker heartbeat expired; outcome requires user recovery choice"
	WorkerRecoveryCoordinatorRestarted = "coordinator restarted; prior worker session ownership is uncertain and requires user recovery choice"
)

// WorkerLease is the coordinator-owned durable right to run one task attempt.
type WorkerLease struct {
	ID                 string
	TaskID             domain.TaskID
	RunID              domain.RunID
	State              WorkerLeaseState
	ProcessID          *int
	ProtocolVersion    int
	ToolSchemaVersion  int
	PolicyRevision     uint64
	WorktreePath       string
	Endpoint           string
	SessionTokenSHA256 string
	LastSequence       uint64
	LastHeartbeatAt    *time.Time
	LastCheckpointID   *string
	ExitCode           *int
	StartedAt          time.Time
	EndedAt            *time.Time
	UpdatedAt          time.Time
	Revision           uint64
}

type AcquireWorkerLease struct {
	ID                 string
	TaskID             domain.TaskID
	RunID              domain.RunID
	ProtocolVersion    int
	ToolSchemaVersion  int
	PolicyRevision     uint64
	WorktreePath       string
	Endpoint           string
	SessionTokenSHA256 string
}

type RecordWorkerHeartbeat struct {
	ID               string
	ExpectedRevision uint64
	Sequence         uint64
	State            WorkerLeaseState
	ProcessID        int
	CheckpointID     *string
	ObservedAt       time.Time
}

type RecordWorkerProcessStarted struct {
	ID               string
	ExpectedRevision uint64
	ProcessID        int
}

// RecordWorkerReport is one redacted status or mediated-tool fact.
type RecordWorkerReport struct {
	ID               string
	ExpectedRevision uint64
	Sequence         uint64
	TaskID           domain.TaskID
	RunID            domain.RunID
	Kind             string
	PayloadJSON      string
	OccurredAt       time.Time
}

// WorkerReport is one coordinator-persisted ordered worker report.
type WorkerReport struct {
	LeaseID     string
	Sequence    uint64
	TaskID      domain.TaskID
	RunID       domain.RunID
	Kind        string
	PayloadJSON string
	OccurredAt  time.Time
}

type FinishWorkerLease struct {
	ID               string
	ExpectedRevision uint64
	State            WorkerLeaseState
	ExitCode         *int
}

type RecoveryCandidate struct {
	Lease  WorkerLease
	Reason string
}

func (repositories *Repositories) AcquireWorkerLease(
	ctx context.Context,
	input AcquireWorkerLease,
) (WorkerLease, error) {
	if err := validateAcquireWorkerLease(input); err != nil {
		return WorkerLease{}, err
	}
	now, micros := repositories.timestamp()
	lease := WorkerLease{
		ID: input.ID, TaskID: input.TaskID, RunID: input.RunID,
		State: WorkerLeaseStarting, ProtocolVersion: input.ProtocolVersion,
		ToolSchemaVersion: input.ToolSchemaVersion,
		PolicyRevision:    input.PolicyRevision, WorktreePath: input.WorktreePath,
		Endpoint: input.Endpoint, SessionTokenSHA256: input.SessionTokenSHA256,
		StartedAt: now, UpdatedAt: now,
	}
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		if err := verifyRunBelongsToTask(
			ctx, transaction, input.RunID, input.TaskID, "acquire worker lease",
		); err != nil {
			return err
		}
		var active int
		if err := transaction.sql.QueryRowContext(
			ctx,
			`SELECT count(*) FROM worker_leases
			 WHERE run_id = ? AND state IN ('starting','running','paused','stopping')`,
			input.RunID,
		).Scan(&active); err != nil {
			return classify("check worker ownership", err)
		}
		if active != 0 {
			return typedError(ErrConflict, "acquire worker lease", errors.New("run already has an active worker"))
		}
		_, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO worker_leases (
				id, task_id, run_id, state, protocol_version,
				tool_schema_version, policy_revision, worktree_path, endpoint,
				session_token_sha256, started_at_unix_micros,
				updated_at_unix_micros
			) VALUES (?, ?, ?, 'starting', ?, ?, ?, ?, ?, ?, ?, ?)`,
			input.ID, input.TaskID, input.RunID, input.ProtocolVersion,
			input.ToolSchemaVersion, input.PolicyRevision, input.WorktreePath,
			input.Endpoint, input.SessionTokenSHA256, micros, micros,
		)
		return repositoryWriteError("acquire worker lease", err)
	})
	return lease, err
}

func (repositories *Repositories) RecordWorkerHeartbeat(
	ctx context.Context,
	input RecordWorkerHeartbeat,
) (WorkerLease, error) {
	if input.Sequence == 0 || input.ProcessID < 1 || input.ObservedAt.IsZero() {
		return WorkerLease{}, errors.New("heartbeat sequence, process, and time are required")
	}
	if input.State != WorkerLeaseRunning && input.State != WorkerLeasePaused &&
		input.State != WorkerLeaseStopping {
		return WorkerLease{}, errors.New("heartbeat state is invalid")
	}
	var lease WorkerLease
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		current, err := getWorkerLease(ctx, transaction.sql, input.ID)
		if err != nil {
			return err
		}
		if current.Revision != input.ExpectedRevision {
			return typedError(ErrStaleRevision, "record worker heartbeat", errors.New("worker lease revision changed"))
		}
		if current.State == WorkerLeaseExited || current.State == WorkerLeaseCrashed ||
			current.State == WorkerLeaseExpired || input.Sequence <= current.LastSequence {
			return typedError(ErrConflict, "record worker heartbeat", errors.New("worker lease is terminal or sequence is stale"))
		}
		if current.ProcessID != nil && *current.ProcessID != input.ProcessID {
			return typedError(ErrConflict, "record worker heartbeat", errors.New("worker process identity changed"))
		}
		observed := input.ObservedAt.UTC()
		if observed.Before(current.StartedAt) {
			return errors.New("heartbeat precedes worker start")
		}
		_, updatedMicros := repositories.timestamp()
		result, err := transaction.sql.ExecContext(
			ctx,
			`UPDATE worker_leases SET
				state = ?, process_id = ?, last_sequence = ?,
				last_heartbeat_at_unix_micros = ?, last_checkpoint_id = ?,
				updated_at_unix_micros = ?, revision = revision + 1
			 WHERE id = ? AND revision = ?`,
			input.State, input.ProcessID, input.Sequence, observed.UnixMicro(),
			nullableString(input.CheckpointID), updatedMicros,
			input.ID, input.ExpectedRevision,
		)
		if err != nil {
			return repositoryWriteError("record worker heartbeat", err)
		}
		changed, _ := result.RowsAffected()
		if changed != 1 {
			return typedError(ErrStaleRevision, "record worker heartbeat", errors.New("worker lease revision changed"))
		}
		lease, err = getWorkerLease(ctx, transaction.sql, input.ID)
		return err
	})
	return lease, err
}

// RecordWorkerProcessStarted persists the coordinator-observed child PID
// before the worker's first heartbeat.
func (repositories *Repositories) RecordWorkerProcessStarted(
	ctx context.Context,
	input RecordWorkerProcessStarted,
) (WorkerLease, error) {
	if input.ID == "" || input.ProcessID < 1 {
		return WorkerLease{}, errors.New("worker lease and process IDs are required")
	}
	var lease WorkerLease
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		current, err := getWorkerLease(ctx, transaction.sql, input.ID)
		if err != nil {
			return err
		}
		if current.Revision != input.ExpectedRevision {
			return typedError(ErrStaleRevision, "record worker process", errors.New("worker lease revision changed"))
		}
		if current.State == WorkerLeaseExited || current.State == WorkerLeaseCrashed ||
			current.State == WorkerLeaseExpired {
			return typedError(ErrConflict, "record worker process", errors.New("worker lease is terminal"))
		}
		if current.ProcessID != nil {
			if *current.ProcessID == input.ProcessID {
				returned := current
				lease = returned
				return nil
			}
			return typedError(ErrConflict, "record worker process", errors.New("worker process identity changed"))
		}
		_, micros := repositories.timestamp()
		result, err := transaction.sql.ExecContext(
			ctx,
			`UPDATE worker_leases SET process_id = ?,
				updated_at_unix_micros = ?, revision = revision + 1
			 WHERE id = ? AND revision = ?`,
			input.ProcessID, micros, input.ID, input.ExpectedRevision,
		)
		if err != nil {
			return repositoryWriteError("record worker process", err)
		}
		changed, _ := result.RowsAffected()
		if changed != 1 {
			return typedError(ErrStaleRevision, "record worker process", errors.New("worker lease revision changed"))
		}
		lease, err = getWorkerLease(ctx, transaction.sql, input.ID)
		return err
	})
	return lease, err
}

func (repositories *Repositories) RecordWorkerReport(
	ctx context.Context,
	input RecordWorkerReport,
) (WorkerLease, error) {
	if input.ID == "" || input.Sequence == 0 || input.TaskID.IsZero() ||
		input.RunID.IsZero() || input.OccurredAt.IsZero() {
		return WorkerLease{}, errors.New("worker report identity, sequence, and time are required")
	}
	if input.Kind != "status" && input.Kind != "tool-event" {
		return WorkerLease{}, errors.New("worker report kind is invalid")
	}
	if len(input.PayloadJSON) < 2 || len(input.PayloadJSON) > 64<<10 ||
		!json.Valid([]byte(input.PayloadJSON)) {
		return WorkerLease{}, errors.New("worker report payload must be bounded valid JSON")
	}
	var lease WorkerLease
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		current, err := getWorkerLease(ctx, transaction.sql, input.ID)
		if err != nil {
			return err
		}
		if current.Revision != input.ExpectedRevision {
			return typedError(ErrStaleRevision, "record worker report", errors.New("worker lease revision changed"))
		}
		if current.TaskID != input.TaskID || current.RunID != input.RunID {
			return typedError(ErrConflict, "record worker report", errors.New("worker report escaped its lease"))
		}
		if current.State == WorkerLeaseExited || current.State == WorkerLeaseCrashed ||
			current.State == WorkerLeaseExpired || input.Sequence <= current.LastSequence {
			return typedError(ErrConflict, "record worker report", errors.New("worker lease is terminal or sequence is stale"))
		}
		occurred := input.OccurredAt.UTC()
		if occurred.Before(current.StartedAt) {
			return errors.New("worker report precedes worker start")
		}
		if _, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO worker_reports (
				lease_id, sequence, task_id, run_id, kind, payload_json,
				occurred_at_unix_micros
			) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			input.ID, input.Sequence, input.TaskID, input.RunID, input.Kind,
			input.PayloadJSON, occurred.UnixMicro(),
		); err != nil {
			return repositoryWriteError("record worker report", err)
		}
		_, updatedMicros := repositories.timestamp()
		result, err := transaction.sql.ExecContext(
			ctx,
			`UPDATE worker_leases SET last_sequence = ?,
				updated_at_unix_micros = ?, revision = revision + 1
			 WHERE id = ? AND revision = ?`,
			input.Sequence, updatedMicros, input.ID, input.ExpectedRevision,
		)
		if err != nil {
			return repositoryWriteError("advance worker report sequence", err)
		}
		changed, _ := result.RowsAffected()
		if changed != 1 {
			return typedError(ErrStaleRevision, "record worker report", errors.New("worker lease revision changed"))
		}
		lease, err = getWorkerLease(ctx, transaction.sql, input.ID)
		return err
	})
	return lease, err
}

func (repositories *Repositories) ListWorkerReports(
	ctx context.Context,
	runID domain.RunID,
	limit int,
) ([]WorkerReport, error) {
	if runID.IsZero() || limit < 1 || limit > 1000 {
		return nil, errors.New("worker report run and bounded limit are required")
	}
	rows, err := repositories.database.sql.QueryContext(
		ctx,
		`SELECT lease_id, sequence, task_id, run_id, kind, payload_json,
		        occurred_at_unix_micros
		 FROM worker_reports WHERE run_id = ?
		 ORDER BY sequence LIMIT ?`,
		runID, limit,
	)
	if err != nil {
		return nil, classify("list worker reports", err)
	}
	defer rows.Close()
	var reports []WorkerReport
	for rows.Next() {
		var report WorkerReport
		var occurred int64
		if err := rows.Scan(
			&report.LeaseID, &report.Sequence, &report.TaskID, &report.RunID,
			&report.Kind, &report.PayloadJSON, &occurred,
		); err != nil {
			return nil, classify("scan worker report", err)
		}
		report.OccurredAt = repositoryTime(occurred)
		reports = append(reports, report)
	}
	if err := rows.Err(); err != nil {
		return nil, classify("iterate worker reports", err)
	}
	return reports, nil
}

func (repositories *Repositories) FinishWorkerLease(
	ctx context.Context,
	input FinishWorkerLease,
) (WorkerLease, error) {
	if input.State != WorkerLeaseExited && input.State != WorkerLeaseCrashed {
		return WorkerLease{}, errors.New("worker finish state must be exited or crashed")
	}
	var lease WorkerLease
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		current, err := getWorkerLease(ctx, transaction.sql, input.ID)
		if err != nil {
			return err
		}
		if current.Revision != input.ExpectedRevision {
			return typedError(ErrStaleRevision, "finish worker lease", errors.New("worker lease revision changed"))
		}
		if current.State == WorkerLeaseExited || current.State == WorkerLeaseCrashed ||
			current.State == WorkerLeaseExpired {
			return typedError(ErrConflict, "finish worker lease", errors.New("worker lease is already terminal"))
		}
		_, micros := repositories.timestamp()
		result, err := transaction.sql.ExecContext(
			ctx,
			`UPDATE worker_leases SET state = ?, exit_code = ?,
				ended_at_unix_micros = ?, updated_at_unix_micros = ?,
				revision = revision + 1
			 WHERE id = ? AND revision = ?`,
			input.State, nullableInt(input.ExitCode), micros, micros,
			input.ID, input.ExpectedRevision,
		)
		if err != nil {
			return repositoryWriteError("finish worker lease", err)
		}
		changed, _ := result.RowsAffected()
		if changed != 1 {
			return typedError(ErrStaleRevision, "finish worker lease", errors.New("worker lease revision changed"))
		}
		lease, err = getWorkerLease(ctx, transaction.sql, input.ID)
		return err
	})
	return lease, err
}

// ExpireWorkerHeartbeats atomically makes uncertain task/run state explicit.
func (repositories *Repositories) ExpireWorkerHeartbeats(
	ctx context.Context,
	cutoff time.Time,
) ([]RecoveryCandidate, error) {
	if cutoff.IsZero() {
		return nil, errors.New("heartbeat cutoff is required")
	}
	return repositories.recoverActiveWorkerLeases(
		ctx,
		`AND coalesce(last_heartbeat_at_unix_micros, started_at_unix_micros) < ?`,
		[]any{cutoff.UTC().UnixMicro()},
		WorkerRecoveryHeartbeatExpired,
	)
}

// AbandonActiveWorkerLeasesAfterRestart invalidates every lease whose
// authenticated in-memory coordinator session was lost during restart.
func (repositories *Repositories) AbandonActiveWorkerLeasesAfterRestart(
	ctx context.Context,
) ([]RecoveryCandidate, error) {
	return repositories.recoverActiveWorkerLeases(
		ctx,
		"",
		nil,
		WorkerRecoveryCoordinatorRestarted,
	)
}

func (repositories *Repositories) recoverActiveWorkerLeases(
	ctx context.Context,
	predicate string,
	arguments []any,
	reason string,
) ([]RecoveryCandidate, error) {
	var candidates []RecoveryCandidate
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		rows, err := transaction.sql.QueryContext(
			ctx,
			`SELECT `+workerLeaseColumns+` FROM worker_leases
			 WHERE state IN ('starting','running','paused','stopping')
			   `+predicate+`
			 ORDER BY started_at_unix_micros, id`,
			arguments...,
		)
		if err != nil {
			return classify("find uncertain worker leases", err)
		}
		for rows.Next() {
			lease, err := scanWorkerLease(rows)
			if err != nil {
				rows.Close()
				return err
			}
			candidates = append(candidates, RecoveryCandidate{
				Lease: lease, Reason: reason,
			})
		}
		if err := rows.Close(); err != nil {
			return classify("close expired worker rows", err)
		}
		_, micros := repositories.timestamp()
		for _, candidate := range candidates {
			result, err := transaction.sql.ExecContext(
				ctx,
				`UPDATE worker_leases SET state = 'expired',
					ended_at_unix_micros = ?, updated_at_unix_micros = ?,
					revision = revision + 1
				 WHERE id = ? AND revision = ?`,
				micros, micros, candidate.Lease.ID, candidate.Lease.Revision,
			)
			if err != nil {
				return repositoryWriteError("expire worker lease", err)
			}
			changed, _ := result.RowsAffected()
			if changed != 1 {
				return typedError(ErrStaleRevision, "expire worker lease", errors.New("worker lease revision changed"))
			}
			if _, err := transaction.sql.ExecContext(
				ctx,
				`UPDATE runs SET state = 'recovery-required',
					updated_at_unix_micros = ?, revision = revision + 1
				 WHERE id = ? AND state NOT IN ('completed','failed','cancelled')`,
				micros, candidate.Lease.RunID,
			); err != nil {
				return repositoryWriteError("mark run recovery required", err)
			}
			if _, err := transaction.sql.ExecContext(
				ctx,
				`UPDATE tasks SET state = 'recovery-required',
					invalidation_reason = ?,
					updated_at_unix_micros = ?, revision = revision + 1
				 WHERE id = ? AND state NOT IN (
					'completed','failed','cancelled','rolled-back'
				 )`,
				candidate.Reason, micros, candidate.Lease.TaskID,
			); err != nil {
				return repositoryWriteError("mark task recovery required", err)
			}
		}
		return nil
	})
	return candidates, err
}

func validateAcquireWorkerLease(input AcquireWorkerLease) error {
	if input.TaskID.IsZero() || input.RunID.IsZero() {
		return errors.New("worker task and run IDs are required")
	}
	for label, value := range map[string]string{
		"worker lease ID": input.ID, "worker worktree": input.WorktreePath,
		"worker endpoint": input.Endpoint,
	} {
		maximum := 255
		if label == "worker worktree" {
			maximum = 4096
		} else if label == "worker endpoint" {
			maximum = 2048
		}
		if err := validateBounded(label, value, maximum); err != nil {
			return err
		}
	}
	if input.ProtocolVersion < 1 || input.ToolSchemaVersion < 1 {
		return errors.New("worker protocol and tool schema versions must be positive")
	}
	return validateSHA256("worker session token hash", input.SessionTokenSHA256)
}

const workerLeaseColumns = `
	id, task_id, run_id, state, process_id, protocol_version,
	tool_schema_version, policy_revision, worktree_path, endpoint,
	session_token_sha256, last_sequence, last_heartbeat_at_unix_micros,
	last_checkpoint_id, exit_code, started_at_unix_micros,
	ended_at_unix_micros, updated_at_unix_micros, revision`

func getWorkerLease(ctx context.Context, queryer rowQueryer, id string) (WorkerLease, error) {
	return scanWorkerLease(queryer.QueryRowContext(
		ctx, `SELECT `+workerLeaseColumns+` FROM worker_leases WHERE id = ?`, id,
	))
}

func scanWorkerLease(row rowScanner) (WorkerLease, error) {
	var lease WorkerLease
	var process, heartbeat, exit, ended sql.NullInt64
	var checkpoint sql.NullString
	var started, updated int64
	err := row.Scan(
		&lease.ID, &lease.TaskID, &lease.RunID, &lease.State, &process,
		&lease.ProtocolVersion, &lease.ToolSchemaVersion, &lease.PolicyRevision,
		&lease.WorktreePath, &lease.Endpoint, &lease.SessionTokenSHA256,
		&lease.LastSequence, &heartbeat, &checkpoint, &exit, &started,
		&ended, &updated, &lease.Revision,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return WorkerLease{}, typedError(ErrNotFound, "get worker lease", err)
	}
	if err != nil {
		return WorkerLease{}, classify("scan worker lease", err)
	}
	lease.ProcessID = nullIntPointer(process)
	lease.LastHeartbeatAt = nullTimePointer(heartbeat)
	lease.ExitCode = nullIntPointer(exit)
	lease.EndedAt = nullTimePointer(ended)
	if checkpoint.Valid {
		lease.LastCheckpointID = &checkpoint.String
	}
	lease.StartedAt = repositoryTime(started)
	lease.UpdatedAt = repositoryTime(updated)
	return lease, nil
}
