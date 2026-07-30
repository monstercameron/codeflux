package storage

import (
	"context"
	"errors"
	"fmt"

	"codeflux.dev/codeflux/internal/domain"
)

const (
	// TaskRunRecoveryMissingOwnership identifies execution metadata that cannot
	// be resumed safely because both worker ownership and the task worktree are
	// absent after coordinator startup.
	TaskRunRecoveryMissingOwnership = "nonterminal task run has neither active worker ownership nor an active worktree binding; outcome requires user recovery choice"
	maximumUnownedRunCandidates     = 1000
)

// UnownedTaskRunRecoveryCandidate is one execution attempt whose durable
// metadata was made explicitly recovery-required during startup.
type UnownedTaskRunRecoveryCandidate struct {
	TaskID            domain.TaskID
	RunID             domain.RunID
	PreviousTaskState domain.TaskState
	PreviousRunState  domain.RunState
	Reason            string
}

// RecoverUnownedTaskRuns atomically discovers active execution metadata that
// has no live queue position, worker ownership, or worktree and makes the
// uncertainty explicit. Pre-execution task states and durable queued work are
// intentionally left unchanged.
func (repositories *Repositories) RecoverUnownedTaskRuns(
	ctx context.Context,
) ([]UnownedTaskRunRecoveryCandidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var candidates []UnownedTaskRunRecoveryCandidate
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		rows, err := transaction.sql.QueryContext(
			ctx,
			`SELECT task.id, task.state, task.revision,
			        run.id, run.state, run.revision
			 FROM tasks task
			 JOIN runs run ON run.task_id = task.id
			 WHERE task.state IN (
			         'running','paused','awaiting-authority','validating',
			         'awaiting-review','failed'
			       )
			   AND run.state IN (
			         'pending','starting','running','pausing','paused','validating'
			       )
			   AND NOT EXISTS (
			         SELECT 1 FROM task_queue_entries queued
			         WHERE queued.task_id = task.id AND queued.state = 'queued'
			           AND (
			             run.state = 'pending'
			             OR (run.state = 'paused' AND queued.resuming = 1)
			           )
			       )
			   AND NOT EXISTS (
			         SELECT 1 FROM worker_leases lease
			         WHERE lease.task_id = task.id
			           AND lease.state IN ('starting','running','paused','stopping')
			       )
			 ORDER BY task.created_at_unix_micros, task.id, run.attempt, run.id
			 LIMIT ?`,
			maximumUnownedRunCandidates+1,
		)
		if err != nil {
			return classify("find unowned task runs", err)
		}
		type persistedCandidate struct {
			candidate    UnownedTaskRunRecoveryCandidate
			taskRevision uint64
			runRevision  uint64
		}
		var persisted []persistedCandidate
		for rows.Next() {
			var candidate persistedCandidate
			if err := rows.Scan(
				&candidate.candidate.TaskID,
				&candidate.candidate.PreviousTaskState,
				&candidate.taskRevision,
				&candidate.candidate.RunID,
				&candidate.candidate.PreviousRunState,
				&candidate.runRevision,
			); err != nil {
				rows.Close()
				return classify("scan unowned task run", err)
			}
			candidate.candidate.Reason = TaskRunRecoveryMissingOwnership
			persisted = append(persisted, candidate)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return classify("iterate unowned task runs", err)
		}
		if err := rows.Close(); err != nil {
			return classify("close unowned task run rows", err)
		}
		if len(persisted) > maximumUnownedRunCandidates {
			return errors.New("unowned task run count exceeds startup recovery bound")
		}

		_, micros := repositories.timestamp()
		updatedTasks := make(map[domain.TaskID]bool, len(persisted))
		for _, item := range persisted {
			if !updatedTasks[item.candidate.TaskID] {
				result, err := transaction.sql.ExecContext(
					ctx,
					`UPDATE tasks
					 SET state = 'recovery-required', invalidation_reason = ?,
					     updated_at_unix_micros = ?, revision = revision + 1
					 WHERE id = ? AND state = ? AND revision = ?
					   AND NOT EXISTS (
					       SELECT 1 FROM task_queue_entries queued
					       WHERE queued.task_id = tasks.id
					         AND queued.state = 'queued'
					         AND (
					           ? = 'pending'
					           OR (? = 'paused' AND queued.resuming = 1)
					         )
					   )
					   AND NOT EXISTS (
					       SELECT 1 FROM worker_leases lease
					       WHERE lease.task_id = tasks.id
					         AND lease.state IN (
					             'starting','running','paused','stopping'
					         )
					   )
					   `,
					TaskRunRecoveryMissingOwnership,
					micros,
					item.candidate.TaskID,
					item.candidate.PreviousTaskState,
					item.taskRevision,
					item.candidate.PreviousRunState,
					item.candidate.PreviousRunState,
				)
				if err != nil {
					return repositoryWriteError("mark unowned task recovery required", err)
				}
				if err := requireOneAffected(
					result,
					"mark unowned task recovery required",
				); err != nil {
					return err
				}
				updatedTasks[item.candidate.TaskID] = true
			}
			result, err := transaction.sql.ExecContext(
				ctx,
				`UPDATE runs
				 SET state = 'recovery-required',
				     updated_at_unix_micros = ?, revision = revision + 1
				 WHERE id = ? AND task_id = ? AND state = ? AND revision = ?`,
				micros,
				item.candidate.RunID,
				item.candidate.TaskID,
				item.candidate.PreviousRunState,
				item.runRevision,
			)
			if err != nil {
				return repositoryWriteError("mark unowned run recovery required", err)
			}
			if err := requireOneAffected(
				result,
				"mark unowned run recovery required",
			); err != nil {
				return err
			}
			candidates = append(candidates, item.candidate)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("recover unowned task runs: %w", err)
	}
	return candidates, nil
}
