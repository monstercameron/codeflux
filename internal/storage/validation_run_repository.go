package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/validation"
)

func (repositories *Repositories) CreateValidationRunIntent(ctx context.Context, input validation.RunIntent) (validation.RunIntent, error) {
	if err := input.Validate(); err != nil {
		return validation.RunIntent{}, err
	}
	var value validation.RunIntent
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		existing, found, err := findValidationRunIntentByIdempotency(ctx, transaction.sql, input.RunID, input.IdempotencyKey)
		if err != nil {
			return err
		}
		if found {
			if !sameValidationRunIntent(existing, input) {
				return typedError(ErrConflict, "create validation run intent", errors.New("idempotency key already binds another intent"))
			}
			value = existing
			return nil
		}
		_, micros := repositories.timestamp()
		_, err = transaction.sql.ExecContext(ctx, `INSERT INTO validation_run_intents (
			id, task_id, run_id, profile_name, profile_version, profile_digest,
			check_id, check_class, required, worktree_revision, diff_identity,
			command_definition_json, command_fingerprint, executable_path,
			executable_sha256, timeout_nanos, intent_digest, idempotency_key,
			created_at_unix_micros
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			input.ID, input.TaskID, input.RunID, input.ProfileName, input.ProfileVersion,
			input.ProfileDigest, input.CheckID, input.CheckClass, boolInt(input.Required),
			input.WorktreeRevision, input.DiffIdentity, input.CommandDefinitionJSON,
			input.CommandFingerprint, input.Executable.Path, input.Executable.SHA256,
			int64(input.Timeout), input.IntentDigest, input.IdempotencyKey, micros)
		if err != nil {
			return repositoryWriteError("create validation run intent", err)
		}
		value, _, err = findValidationRunIntentByIdempotency(ctx, transaction.sql, input.RunID, input.IdempotencyKey)
		return err
	})
	return value, err
}

func (repositories *Repositories) GetValidationRunIntent(ctx context.Context, id domain.ValidationID) (validation.RunIntent, error) {
	if id.IsZero() {
		return validation.RunIntent{}, validation.ErrInvalidValidationRun
	}
	value, err := scanValidationRunIntent(repositories.database.sql.QueryRowContext(ctx, validationRunIntentSelect+` WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return validation.RunIntent{}, typedError(ErrNotFound, "get validation run intent", err)
	}
	return value, classify("get validation run intent", err)
}

func (repositories *Repositories) CommitValidationRunResult(ctx context.Context, input validation.RunResult) (validation.RunResult, error) {
	if err := input.Validate(); err != nil {
		return validation.RunResult{}, err
	}
	var value validation.RunResult
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		existing, found, err := findValidationRunResult(ctx, transaction.sql, input.ValidationRunID)
		if err != nil {
			return err
		}
		if found {
			if !sameValidationRunResult(existing, input) {
				return typedError(ErrConflict, "commit validation run result", errors.New("validation run already has another result"))
			}
			value = existing
			return nil
		}
		var intentDiff string
		if err := transaction.sql.QueryRowContext(ctx, `SELECT diff_identity FROM validation_run_intents WHERE id = ?`, input.ValidationRunID).Scan(&intentDiff); err != nil {
			return classify("verify validation run intent", err)
		}
		_, micros := repositories.timestamp()
		_, err = transaction.sql.ExecContext(ctx, `INSERT INTO validation_run_results (
			validation_run_id, state, exit_code, duration_nanos, timed_out, cancelled,
			stdout_redacted, stderr_redacted, stdout_truncated, stderr_truncated,
			parser_name, parse_succeeded, parsed_result_json, raw_redacted_summary,
			observed_diff_identity, result_digest, completed_at_unix_micros
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			input.ValidationRunID, input.State, input.ExitCode, int64(input.Duration),
			boolInt(input.TimedOut), boolInt(input.Cancelled), input.StdoutRedacted,
			input.StderrRedacted, boolInt(input.StdoutTruncated), boolInt(input.StderrTruncated),
			input.ParserName, boolInt(input.ParseSucceeded), input.ParsedResultJSON,
			input.RawRedactedSummary, input.ObservedDiffIdentity, input.ResultDigest, micros)
		if err != nil {
			return repositoryWriteError("commit validation run result", err)
		}
		value, _, err = findValidationRunResult(ctx, transaction.sql, input.ValidationRunID)
		return err
	})
	return value, err
}

func (repositories *Repositories) GetValidationRunResult(ctx context.Context, id domain.ValidationID) (validation.RunResult, error) {
	if id.IsZero() {
		return validation.RunResult{}, validation.ErrInvalidValidationRun
	}
	value, found, err := findValidationRunResult(ctx, repositories.database.sql, id)
	if err != nil {
		return validation.RunResult{}, err
	}
	if !found {
		return validation.RunResult{}, typedError(ErrNotFound, "get validation run result", sql.ErrNoRows)
	}
	return value, nil
}

func (repositories *Repositories) InvalidateValidationRuns(ctx context.Context, runID domain.RunID, currentDiff string) ([]validation.Invalidation, error) {
	if runID.IsZero() || !validSHA256(currentDiff) {
		return nil, validation.ErrInvalidValidationRun
	}
	var values []validation.Invalidation
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		rows, err := transaction.sql.QueryContext(ctx, `SELECT intent.id, intent.diff_identity
			FROM validation_run_intents AS intent
			JOIN validation_run_results AS result ON result.validation_run_id = intent.id
			WHERE intent.run_id = ? AND intent.diff_identity <> ?
			ORDER BY intent.created_at_unix_micros, intent.id`, runID, currentDiff)
		if err != nil {
			return classify("list stale validation runs", err)
		}
		defer rows.Close()
		type stale struct {
			id       domain.ValidationID
			previous string
		}
		var staleRuns []stale
		for rows.Next() {
			var item stale
			if err := rows.Scan(&item.id, &item.previous); err != nil {
				return classify("scan stale validation run", err)
			}
			staleRuns = append(staleRuns, item)
		}
		if err := rows.Err(); err != nil {
			return classify("list stale validation runs", err)
		}
		for _, item := range staleRuns {
			_, micros := repositories.timestamp()
			inserted, err := transaction.sql.ExecContext(ctx, `INSERT OR IGNORE INTO validation_run_invalidations (
				validation_run_id, previous_diff_identity, current_diff_identity, reason, created_at_unix_micros
			) VALUES (?, ?, ?, 'underlying-diff-changed', ?)`, item.id, item.previous, currentDiff, micros)
			if err != nil {
				return repositoryWriteError("invalidate validation run", err)
			}
			affected, err := inserted.RowsAffected()
			if err != nil {
				return classify("count validation invalidation", err)
			}
			if affected == 0 {
				continue
			}
			var createdMicros int64
			if err := transaction.sql.QueryRowContext(ctx, `SELECT created_at_unix_micros FROM validation_run_invalidations
				WHERE validation_run_id = ? AND current_diff_identity = ?`, item.id, currentDiff).Scan(&createdMicros); err != nil {
				return classify("read validation invalidation", err)
			}
			values = append(values, validation.Invalidation{
				ValidationRunID: item.id, PreviousDiffIdentity: item.previous,
				CurrentDiffIdentity: currentDiff, Reason: "underlying-diff-changed",
				CreatedAt: repositoryTime(createdMicros),
			})
		}
		return nil
	})
	return values, err
}

func (repositories *Repositories) ListRequiredValidationReruns(ctx context.Context, runID domain.RunID, currentDiff string) ([]validation.RequiredRerun, error) {
	if runID.IsZero() || !validSHA256(currentDiff) {
		return nil, validation.ErrInvalidValidationRun
	}
	rows, err := repositories.database.sql.QueryContext(ctx, `SELECT stale.check_id, stale.id, stale.diff_identity
		FROM validation_run_intents AS stale
		JOIN validation_run_results AS stale_result ON stale_result.validation_run_id = stale.id
		WHERE stale.run_id = ? AND stale.required = 1 AND stale.diff_identity <> ?
		  AND NOT EXISTS (
			SELECT 1 FROM validation_run_intents AS current
			JOIN validation_run_results AS current_result ON current_result.validation_run_id = current.id
			WHERE current.run_id = stale.run_id AND current.check_id = stale.check_id
			  AND current.diff_identity = ? AND current_result.state = 'passed'
		  )
		ORDER BY stale.check_id, stale.created_at_unix_micros DESC`, runID, currentDiff, currentDiff)
	if err != nil {
		return nil, classify("list required validation reruns", err)
	}
	defer rows.Close()
	seen := make(map[string]struct{})
	var values []validation.RequiredRerun
	for rows.Next() {
		var value validation.RequiredRerun
		if err := rows.Scan(&value.CheckID, &value.PreviousRunID, &value.PreviousDiffIdentity); err != nil {
			return nil, classify("scan required validation rerun", err)
		}
		if _, duplicate := seen[value.CheckID]; duplicate {
			continue
		}
		seen[value.CheckID] = struct{}{}
		value.CurrentDiffIdentity = currentDiff
		values = append(values, value)
	}
	return values, rows.Err()
}

const validationRunIntentSelect = `SELECT id, task_id, run_id, profile_name, profile_version,
	profile_digest, check_id, check_class, required, worktree_revision, diff_identity,
	command_definition_json, command_fingerprint, executable_path, executable_sha256,
	timeout_nanos, intent_digest, idempotency_key, created_at_unix_micros
	FROM validation_run_intents`

func findValidationRunIntentByIdempotency(ctx context.Context, query rowQueryer, runID domain.RunID, key string) (validation.RunIntent, bool, error) {
	value, err := scanValidationRunIntent(query.QueryRowContext(ctx, validationRunIntentSelect+` WHERE run_id = ? AND idempotency_key = ?`, runID, key))
	if errors.Is(err, sql.ErrNoRows) {
		return validation.RunIntent{}, false, nil
	}
	return value, err == nil, classify("find validation run intent", err)
}

func scanValidationRunIntent(row rowScanner) (validation.RunIntent, error) {
	var value validation.RunIntent
	var required int
	var timeoutNanos, micros int64
	err := row.Scan(&value.ID, &value.TaskID, &value.RunID, &value.ProfileName, &value.ProfileVersion,
		&value.ProfileDigest, &value.CheckID, &value.CheckClass, &required, &value.WorktreeRevision,
		&value.DiffIdentity, &value.CommandDefinitionJSON, &value.CommandFingerprint,
		&value.Executable.Path, &value.Executable.SHA256, &timeoutNanos, &value.IntentDigest,
		&value.IdempotencyKey, &micros)
	value.Required = required != 0
	value.Timeout = time.Duration(timeoutNanos)
	value.CreatedAt = repositoryTime(micros)
	return value, err
}

func findValidationRunResult(ctx context.Context, query rowQueryer, id domain.ValidationID) (validation.RunResult, bool, error) {
	var value validation.RunResult
	var timedOut, cancelled, stdoutTruncated, stderrTruncated, parseSucceeded int
	var durationNanos, micros int64
	err := query.QueryRowContext(ctx, `SELECT validation_run_id, state, exit_code, duration_nanos,
		timed_out, cancelled, stdout_redacted, stderr_redacted, stdout_truncated,
		stderr_truncated, parser_name, parse_succeeded, parsed_result_json,
		raw_redacted_summary, observed_diff_identity, result_digest, completed_at_unix_micros
		FROM validation_run_results WHERE validation_run_id = ?`, id).Scan(
		&value.ValidationRunID, &value.State, &value.ExitCode, &durationNanos, &timedOut,
		&cancelled, &value.StdoutRedacted, &value.StderrRedacted, &stdoutTruncated,
		&stderrTruncated, &value.ParserName, &parseSucceeded, &value.ParsedResultJSON,
		&value.RawRedactedSummary, &value.ObservedDiffIdentity, &value.ResultDigest, &micros)
	if errors.Is(err, sql.ErrNoRows) {
		return validation.RunResult{}, false, nil
	}
	if err != nil {
		return validation.RunResult{}, false, classify("find validation run result", err)
	}
	value.Duration = time.Duration(durationNanos)
	value.TimedOut, value.Cancelled = timedOut != 0, cancelled != 0
	value.StdoutTruncated, value.StderrTruncated = stdoutTruncated != 0, stderrTruncated != 0
	value.ParseSucceeded = parseSucceeded != 0
	value.CompletedAt = repositoryTime(micros)
	return value, true, nil
}

func sameValidationRunIntent(left, right validation.RunIntent) bool {
	return left.IntentDigest == right.IntentDigest && left.ID == right.ID && left.RunID == right.RunID && left.IdempotencyKey == right.IdempotencyKey
}

func sameValidationRunResult(left, right validation.RunResult) bool {
	return left.ResultDigest == right.ResultDigest && left.ValidationRunID == right.ValidationRunID
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

var _ validation.RunRepository = (*Repositories)(nil)
