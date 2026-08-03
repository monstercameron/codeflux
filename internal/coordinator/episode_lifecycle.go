// Package coordinator: episode lifecycle wiring (MEM-001, MEM-002,
// MEM-003). §31 assumes an append-only chronological record exists for
// every run; before this file, OpenEpisode and CloseEpisode had zero
// production callers, so it did not. This file is the run-boundary wiring
// AgentExecution.Run calls into -- kept out of that file so its own
// already-large diff stays about what a run does, not about episode
// bookkeeping.
//
// Every function here is best-effort by the same rule
// internal/coordinator/agent_pipeline_ledger.go already established for the
// pipeline ledger: a record this file cannot write must not stop the work
// it is describing. A task whose exact fingerprint was never bound
// (episode_fingerprint_binding_repository.go), a database briefly
// unavailable, or a genuinely concurrent open racing this one all produce a
// missing or best-recovered episode rather than a failed run.
package coordinator

import (
	"context"
	"strconv"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
)

// openRunEpisode opens the append-only episode record for one run
// (MEM-001), bound to the pipeline ledger's own attempt number (MEM-003,
// PIPE-003) rather than a second, independently invented notion of
// "attempt": a task started twice calls this twice, once per attempt, and
// gets two independent episodes.
//
// The exact fingerprint (schema version + hash) comes from
// GetTaskExactFingerprintBinding, not from recomputing one here: this
// function has no access to the AffectedPaths/AffectedPackages/
// AffectedSymbols/ToolchainBindings/RequestedAuthority
// internal/coordinator/task_intake.go fed into the ORIGINAL fingerprint at
// forecast time (none of them are persisted onto the task row), so any
// value it invented itself would not be the hash retrieval already indexed
// this task under -- a wrong-but-present fingerprint would be worse than an
// absent episode, because it would look authoritative while never being
// found by ListEpisodesByFingerprintHash.
func (execution *AgentExecution) openRunEpisode(
	ctx context.Context,
	scope agentScope,
	taskID domain.TaskID,
	runID domain.RunID,
	attempt uint64,
) storage.Episode {
	if execution == nil || execution.repositories == nil {
		return storage.Episode{}
	}
	binding, err := execution.repositories.GetTaskExactFingerprintBinding(ctx, taskID)
	if err != nil {
		return storage.Episode{}
	}
	episodeID, err := domain.NewEpisodeID()
	if err != nil {
		return storage.Episode{}
	}
	episode, err := execution.repositories.OpenEpisode(ctx, storage.OpenEpisode{
		ID: episodeID, ProjectID: scope.projectID, RepositoryID: scope.repositoryID, TaskID: taskID,
		Attempt:                  attempt,
		FingerprintSchemaVersion: binding.FingerprintSchemaVersion, FingerprintHash: binding.FingerprintHash,
		StartingRevision: scope.revision,
		IdempotencyKey:   agentExecutionKey("agent-run-episode-", runID.String()),
	})
	if err == nil {
		return episode
	}
	// A retried or racing open for the same (task, attempt) minted a
	// different episode ID than this call did (each call mints its own
	// random domain.NewEpisodeID rather than deriving a deterministic one),
	// so OpenEpisode reports a conflict even though a real episode for this
	// attempt does exist. Read back whichever one actually won, so this
	// run's later facts still attach to a real episode.
	if existing, getErr := execution.repositories.GetEpisodeByTaskAttempt(ctx, taskID, attempt); getErr == nil {
		return existing
	}
	return storage.Episode{}
}

// closeRunEpisode closes episode at the end of a run (MEM-001).
//
// Closing here is deliberately always Unresolved, never Accepted or
// Rejected. domain.NewEpisode's own doc comment ties closure to "the
// moment a terminal USER decision is recorded", and no user decision is
// made inside AgentExecution.Run: acceptance is a person's decision,
// recorded later by a different, later-stage code path this function does
// not own (see the StageHumanAcceptance narration a few lines above every
// call site below -- "acceptance is a person's decision and this run
// cannot make it"). A run whose evidence came out clean and verified is
// still Unresolved by this same rule: nobody has looked at it yet.
//
// KNOWN LIMITATION, reported rather than silently worked around: because
// episodes_immutable_after_close freezes the row the instant this closes
// it, the human decision that comes later can never be written back onto
// THIS episode's own outcome column -- only a later ticket that moves (or
// duplicates) episode closure to the real acceptance-decision site can
// make CloseEpisode's outcome carry the user's actual decision. Recording
// Unresolved here is still strictly better than the prior state, where no
// episode existed to close at all.
func (execution *AgentExecution) closeRunEpisode(
	ctx context.Context,
	episode storage.Episode,
	scope agentScope,
) {
	if execution == nil || execution.repositories == nil || episode.ID.IsZero() {
		return
	}
	_, _ = execution.repositories.CloseEpisode(ctx, storage.CloseEpisode{
		EpisodeID: episode.ID, EndingRevision: scope.revision, Outcome: domain.EpisodeOutcomeUnresolved,
	})
}

// recordAttemptTransition persists one attempt's gate, normalised failure,
// rung, and outcome as a durable episode_attempt_transitions row (MEM-002)
// -- the fact every later extractor needs to already be in the store
// rather than reconstructed from a chat message's prose. normalizedFailure
// is empty exactly when outcome is EpisodeAttemptOutcomeConverged; every
// other outcome requires it, matching the storage-layer CHECK constraint
// this function's caller must satisfy.
func (execution *AgentExecution) recordAttemptTransition(
	ctx context.Context,
	episode storage.Episode,
	attempt int,
	gate string,
	normalizedFailure string,
	rung string,
	outcome storage.EpisodeAttemptOutcome,
) {
	if execution == nil || execution.repositories == nil || episode.ID.IsZero() {
		return
	}
	var failure *string
	if normalizedFailure != "" {
		failure = &normalizedFailure
	}
	// Deterministic per (episode, attempt, outcome): a retried call for the
	// same attempt reaching the same outcome is an idempotent no-op rather
	// than a second row, and the storage layer's own ordinal allocation
	// still lets a genuinely different outcome for the same attempt (e.g. a
	// later escalation after an earlier malformed-turn retry within the
	// same numbered attempt) land as its own row.
	id := "attempt-transition:" + episode.ID.String() + ":" + strconv.Itoa(attempt) + ":" + string(outcome)
	_, _ = execution.repositories.RecordEpisodeAttemptTransition(ctx, storage.RecordEpisodeAttemptTransition{
		ID: id, EpisodeID: episode.ID, Attempt: uint64(attempt), Gate: gate,
		NormalizedFailure: failure, Rung: rung, Outcome: outcome,
		IdempotencyKey: id,
	})
}
