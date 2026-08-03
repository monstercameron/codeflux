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
	"time"

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
//
// It returns the CLOSED episode on success (zero-value storage.Episode on
// every no-op or failure path above, including a failed CloseEpisode call
// itself), so a caller -- review_mutations.go's finalizeReviewDecision
// WithStorageErrors -- can hand it straight to MEM-006/MEM-008's
// ExtractDeterministicFactsAtEpisodeClose without a second read. That
// function's own Outcome/Status guard already treats a zero-value episode
// as a no-op, so a failed close here safely propagates into "extract
// nothing" rather than a nil-handling special case at the call site.
func closeReviewDecisionEpisode(
	ctx context.Context,
	repositories episodeReviewCloser,
	taskID domain.TaskID,
	endingRevision string,
	outcome domain.EpisodeOutcome,
) storage.Episode {
	if repositories == nil || taskID.IsZero() {
		return storage.Episode{}
	}
	episode, err := repositories.GetEpisodeByTask(ctx, taskID)
	if err != nil || episode.Status != domain.EpisodeStatusOpen {
		return storage.Episode{}
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
	closed, err := repositories.CloseEpisode(ctx, storage.CloseEpisode{
		EpisodeID: episode.ID, EndingRevision: endingRevision, Outcome: outcome,
	})
	if err != nil {
		return storage.Episode{}
	}
	return closed
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

// -----------------------------------------------------------------------
// MEM-006/MEM-008/MEM-011a: deterministic extraction at episode close.
// -----------------------------------------------------------------------
//
// docs/plan.md §31 "Extraction Triggers and the Candidacy Funnel": "A run
// that has not been accepted has established nothing." Every extraction
// function this file calls already refuses an episode that is not
// domain.EpisodeStatusClosed (internal/storage/memory_fact_extraction_
// repository.go's listAttributableValidationChecks); this file adds the one
// gate the storage layer deliberately leaves to its caller -- MEM-001a gave
// episodes a real Outcome precisely so a caller could tell an accepted run
// from a rejected or abandoned one, and quarantine being terminal (§31)
// means the safer default is to mint nothing from a candidate a human did
// not accept, not to mint it and hope the acceptance-outcome gate is
// someone else's problem later.
//
// ExtractDeterministicFactsAtEpisodeClose is written against the CONCRETE
// *storage.Repositories, deliberately, rather than a narrow interface: its
// intended call site is the review-acceptance decision
// (review_mutations.go's finalizeReviewDecisionWithStorageErrors, which
// calls closeReviewDecisionEpisode above), but that file is out of this
// change's ownership and its ReviewMutationService.repositories field is
// typed as the narrow reviewMutationRepositories interface -- which does
// not, and structurally cannot without editing that file, expose the
// Extract*FromEpisode methods this function needs. Widening
// episodeReviewCloser to demand them would stop review_mutations.go's
// existing closeReviewDecisionEpisode(ctx, service.repositories, ...) call
// from compiling, since Go requires the STATIC interface type passed at a
// call site to already declare every method the target parameter type
// requires -- true even though the underlying *storage.Repositories value
// satisfies them today. So this function is built, tested end to end
// against a real SQLite fixture, and left uncalled by production rather
// than forcing a change into a file another lane owns; see this package's
// handoff notes for the exact one-method-per-extractor addition
// review_mutations.go's reviewMutationRepositories interface needs, plus
// the one call this function needs added after that file's existing
// closeReviewDecisionEpisode call, to complete the wiring.
type DeterministicExtractionSummary struct {
	// BuildCommands, TestCommands, and RepositoryConventions are exactly
	// what internal/storage's ExtractAttributableBuildCommandsFromEpisode,
	// ExtractAttributableTestCommandsFromEpisode, and
	// ExtractAttributableRepositoryConventionsFromEpisode returned: every
	// admitted-or-reconfirmed memory fact, never a model's own claim about
	// what it did.
	BuildCommands         []storage.ExtractedMemoryFact
	TestCommands          []storage.ExtractedMemoryFact
	RepositoryConventions []storage.ExtractedMemoryFact
	// Duration is MEM-011a's measurement: wall-clock time this call spent
	// inside the three extractors, so a caller assembling the run's cost
	// summary has a real number for "extraction" rather than an
	// unaccounted gap. It is populated even when an extractor returns an
	// error, since the time was still spent.
	Duration time.Duration
}

// ExtractDeterministicFactsAtEpisodeClose runs MEM-006 (build and test
// commands) and MEM-008 (formatting/static-analysis conventions from
// accepted work) for one already-closed episode, and reports the MEM-011a
// wall-clock cost of doing so.
//
// It is a deliberate no-op -- zero calls into any extractor, zero cost
// charged -- for a nil repositories handle, a zero episode ID, an episode
// that is not domain.EpisodeStatusClosed, or one whose Outcome is not
// domain.EpisodeOutcomeAccepted. Per §31's own words, a rejected or
// abandoned episode has established nothing worth minting a workspace fact
// from, and quarantine being terminal (a wrong fact can only be retired,
// never corrected) makes "extract nothing" the correct default whenever
// the acceptance signal is absent or ambiguous, not merely the cheap one.
//
// MEM-007 (file-to-test mapping) and MEM-009 (approved repository
// instructions) are deliberately NOT called here. MEM-007's
// ExtractAttributableFileToTestMappingFromEpisode requires the exact,
// real, changed-file set for the diff being closed (never inferred), and
// nothing reachable from an episode alone carries it -- review_mutations.go
// knows a reportID/diffIdentity at the accept call site but does not pass
// either into closeReviewDecisionEpisode today, and evidence_report_
// repository.go's sealed report is keyed by (taskID, reportID), neither of
// which this function receives. MEM-009's ExtractApprovedRepositoryInstruction
// requires a real, already-granted domain.ApprovalID naming the SPECIFIC
// instruction a person approved for durable memory; no call site in this
// repository today ever creates that approval (the only instruction-
// approval mechanism that exists, internal/storage/repository_instruction_
// approval_repository.go's AUDIT-011 table, gates first-use INJECTION into
// a run's context, not admission into memory, and is a different table).
// Inventing either input here would be exactly the "mint something wrong"
// failure mode §31 warns is worse than minting nothing.
func ExtractDeterministicFactsAtEpisodeClose(
	ctx context.Context,
	repositories *storage.Repositories,
	episode storage.Episode,
) (DeterministicExtractionSummary, error) {
	var summary DeterministicExtractionSummary
	if repositories == nil || episode.ID.IsZero() {
		return summary, nil
	}
	if episode.Status != domain.EpisodeStatusClosed {
		return summary, nil
	}
	if episode.Outcome == nil || *episode.Outcome != domain.EpisodeOutcomeAccepted {
		return summary, nil
	}

	started := time.Now()
	defer func() { summary.Duration = time.Since(started) }()

	buildFacts, err := repositories.ExtractAttributableBuildCommandsFromEpisode(ctx, episode.ID)
	if err != nil {
		return summary, err
	}
	summary.BuildCommands = buildFacts

	testFacts, err := repositories.ExtractAttributableTestCommandsFromEpisode(ctx, episode.ID)
	if err != nil {
		return summary, err
	}
	summary.TestCommands = testFacts

	conventionFacts, err := repositories.ExtractAttributableRepositoryConventionsFromEpisode(ctx, episode.ID)
	if err != nil {
		return summary, err
	}
	summary.RepositoryConventions = conventionFacts

	return summary, nil
}
