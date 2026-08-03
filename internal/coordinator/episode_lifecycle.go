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
	"strings"

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

// closeRunEpisode closes the episode at the end of a run, but only when the
// run ended WITHOUT handing anything to a human (MEM-001a).
//
// MEM-001 closed every episode here, always Unresolved, with a documented
// limitation: episodes_immutable_after_close freezes the row the instant it
// closes, so the real, later human decision (accept, reject, request a
// repair, roll back) could never be written back onto it. That made the
// outcome column uniformly Unresolved regardless of what actually happened
// -- useless to every reader of it. MEM-001a fixes that by moving closure to
// the real acceptance-decision site (closeReviewDecisionEpisode, called from
// review_mutations.go's finalizeReviewDecisionWithStorageErrors) for every
// run that reaches one, and keeping this function only for the runs that
// never do.
//
// domain.NewEpisode's own doc comment ties closure to "the moment a
// terminal USER decision is recorded". A run that lands its task in
// AwaitingReview has produced a candidate but not yet a decision -- the
// decision is a person's, made later, by finalizeReviewDecisionWithStorageErrors
// -- so this function leaves that episode open rather than closing it
// early and losing the chance for CloseEpisode's Outcome to ever carry the
// real answer.
//
// Every other way Run can end here -- failed, cancelled outside review,
// or a task state this function could not even read back -- ends the run
// without a human ever having looked at a candidate. Closing it Unresolved
// would say "nobody has judged this yet", which is true only in the sense
// that stops being useful the moment nothing will ever judge it either:
// this attempt's episode is dead, and Abandoned says so. A GetTask failure
// is treated as "leave it open" rather than "guess Abandoned": a
// wrong-but-present outcome would be worse than an episode this build
// never got back to close, because it would look authoritative while
// describing a run that may still be waiting on review.
func (execution *AgentExecution) closeRunEpisode(
	ctx context.Context,
	episode storage.Episode,
	scope agentScope,
) {
	if execution == nil || execution.repositories == nil || episode.ID.IsZero() {
		return
	}
	task, err := execution.repositories.GetTask(ctx, scope.taskID)
	if err != nil {
		return
	}
	if task.State == domain.TaskStateAwaitingReview {
		return
	}
	_, _ = execution.repositories.CloseEpisode(ctx, storage.CloseEpisode{
		EpisodeID: episode.ID, EndingRevision: scope.revision, Outcome: domain.EpisodeOutcomeAbandoned,
	})
}

// closeReviewDecisionEpisode closes the run episode a human's terminal
// review decision -- accept, reject, request a repair, or roll back -- just
// resolved (MEM-001a). This is the site domain.NewEpisode's doc comment
// describes ("the moment a terminal user decision is recorded"): called
// from review_mutations.go's finalizeReviewDecisionWithStorageErrors, after
// RecordTaskReviewDecision has durably recorded the decision itself, so an
// episode-closing failure here can never make the decision look like it
// did not happen.
//
// It closes the task's most recently opened episode (GetEpisodeByTask,
// i.e. the highest attempt) only if that episode is still Open. Two cases
// leave it Closed already and this call a deliberate no-op rather than an
// error: no episode was ever opened for the attempt that produced this
// review candidate (openRunEpisode is itself best-effort, MEM-001), or
// RollbackTask's target is a checkpoint from an attempt whose episode a
// PRIOR decision on this same task already closed (rollback is reachable
// from TaskStateCompleted, not only TaskStateAwaitingReview). Either way,
// attempting to close an already-closed episode a second time is exactly
// what episodes_immutable_after_close exists to refuse; recording that
// later correction as an invalidation overlay instead is out of MEM-001a's
// scope.
func closeReviewDecisionEpisode(
	ctx context.Context,
	repositories episodeReviewCloser,
	taskID domain.TaskID,
	endingRevision string,
	outcome domain.EpisodeOutcome,
) {
	if repositories == nil || taskID.IsZero() {
		return
	}
	episode, err := repositories.GetEpisodeByTask(ctx, taskID)
	if err != nil || episode.Status != domain.EpisodeStatusOpen {
		return
	}
	if strings.TrimSpace(endingRevision) == "" {
		// CloseEpisode/domain.EpisodeClosure.Validate refuse an empty ending
		// revision. Every review decision that reaches this call carries some
		// diff-identity string in production; falling back to the episode's
		// own StartingRevision only matters for a caller (or a future one)
		// that ever legitimately has nothing better, and keeps a missing value
		// from silently discarding this close instead of recording it.
		endingRevision = episode.StartingRevision
	}
	_, _ = repositories.CloseEpisode(ctx, storage.CloseEpisode{
		EpisodeID: episode.ID, EndingRevision: endingRevision, Outcome: outcome,
	})
}

// episodeReviewCloser is the minimal repository surface
// closeReviewDecisionEpisode needs, so review_mutations.go's own
// reviewMutationRepositories interface can satisfy it without depending on
// the concrete *storage.Repositories type.
type episodeReviewCloser interface {
	GetEpisodeByTask(context.Context, domain.TaskID) (storage.Episode, error)
	CloseEpisode(context.Context, storage.CloseEpisode) (storage.Episode, error)
}

// episodeOutcomeForReviewDecision maps one review decision kind to the
// episode outcome its close should carry (MEM-001a). RequestRepair and the
// explicit reject both resolve to Rejected: from the closing episode's own
// point of view -- the specific attempt that produced the candidate under
// review -- a human declining to accept that candidate as-is is the same
// fact whether the task then stops (Abandon) or tries again under a fresh
// attempt and its own fresh episode (RequestRepair). The finer distinction
// between those two lives in the task's own state transition
// (TaskStateCancelled vs TaskStateRunning), which RecordTaskReviewDecision
// already records; domain.EpisodeOutcome has no matching fifth value, and
// inventing one is out of MEM-001a's scope. Rollback maps to Abandoned: it
// discards a candidate rather than judging it on its merits.
func episodeOutcomeForReviewDecision(decision storage.TaskReviewDecisionKind) (domain.EpisodeOutcome, bool) {
	switch decision {
	case storage.TaskReviewAccept:
		return domain.EpisodeOutcomeAccepted, true
	case storage.TaskReviewRequestRepair, storage.TaskReviewAbandon:
		return domain.EpisodeOutcomeRejected, true
	case storage.TaskReviewRollback:
		return domain.EpisodeOutcomeAbandoned, true
	default:
		return "", false
	}
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
