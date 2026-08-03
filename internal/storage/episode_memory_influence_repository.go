// Package storage: carrying RunPreWorkGate's eligible set into the run
// that consumes it, and recording what the run did with it (MEM-004,
// MEM-004a). See migrations/000035_episode_lifecycle_wiring.sql's doc
// comment on episode_memory_influence_events.
//
// ListEligibleMemoryForTask answers MEM-004's "carry the eligible set...
// into the run that consumes it" from durable state rather than an
// in-process value: retrieval.Service.RunPreWorkGate runs at forecast
// time, long before a run exists to consume its result, and
// ForecastedTask.Retrieval is a plain Go value that does not survive that
// gap. What DOES survive it is what RunPreWorkGate already writes durably
// -- memory_retrieval_queries and memory_retrieval_candidates (migration
// 000025) -- so this file reads the eligible set back from there: eligible
// means discovered as a candidate and never immediately rejected (an
// eligible candidate carries no memory_retrieval_decisions row until an
// agent decides what to do with it).
//
// RecordEpisodeMemoryInfluence answers MEM-004a: it records, for one
// carried candidate, what the run actually did with it and why, as the two
// coupled durable facts that decision requires -- the
// memory_retrieval_decisions row every OTHER influence-recording path
// already writes through, and the episode-scoped link that makes the
// decision traceable back to the specific episode that made it.
package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/retrievalgate"
)

// EligibleMemoryCandidate is one candidate RunPreWorkGate found eligible
// for a task, read back from durable storage rather than from the
// in-process retrieval.PreWorkGateResult that computed it (MEM-004).
type EligibleMemoryCandidate struct {
	CandidateID string
	QueryID     string
	RevisionID  domain.MemoryArtifactRevisionID
	Rank        int
}

// ListEligibleMemoryForTask reads the durable eligible set RunPreWorkGate
// left behind for taskID across every retrieval query recorded against it:
// every logged candidate that carries no memory_retrieval_decisions row.
// A candidate the eligibility gates rejected outright is excluded (it
// received its 'rejected' decision at discovery time, before any run ever
// saw it); a candidate an earlier call to RecordEpisodeMemoryInfluence
// already decided is excluded too, so a caller can call this repeatedly
// across an episode's attempts without re-offering an item already acted
// on.
func (repositories *Repositories) ListEligibleMemoryForTask(
	ctx context.Context,
	taskID domain.TaskID,
) ([]EligibleMemoryCandidate, error) {
	if taskID.IsZero() {
		return nil, errors.New("task ID must not be empty")
	}
	queries, err := repositories.ListMemoryRetrievalQueriesByTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	var eligible []EligibleMemoryCandidate
	for _, query := range queries {
		candidates, err := repositories.ListMemoryRetrievalCandidatesForQuery(ctx, query.ID)
		if err != nil {
			return nil, err
		}
		for _, candidate := range candidates {
			_, decided, err := repositories.GetMemoryRetrievalDecision(ctx, candidate.ID)
			if err != nil {
				return nil, err
			}
			if decided {
				continue
			}
			eligible = append(eligible, EligibleMemoryCandidate{
				CandidateID: candidate.ID, QueryID: candidate.QueryID,
				RevisionID: candidate.RevisionID, Rank: candidate.Rank,
			})
		}
	}
	return eligible, nil
}

// EpisodeMemoryInfluence is one durable record of what an episode's run did
// with one eligible memory candidate.
type EpisodeMemoryInfluence struct {
	ID                    string
	EpisodeID             domain.EpisodeID
	CandidateID           string
	Action                retrievalgate.AgentInfluenceAction
	JustificationRedacted string
	IdempotencyKey        string
	RecordedAt            time.Time
}

// RecordEpisodeMemoryInfluence declares one input record for
// (*Repositories).RecordEpisodeMemoryInfluence.
type RecordEpisodeMemoryInfluence struct {
	ID                    string
	EpisodeID             domain.EpisodeID
	CandidateID           string
	Action                retrievalgate.AgentInfluenceAction
	JustificationRedacted string
	IdempotencyKey        string
}

// RecordEpisodeMemoryInfluence persists what episodeID's run did with
// candidateID -- used it as-is, adapted it, or rejected it -- and why
// (MEM-004a), atomically writing both the memory_retrieval_decisions row
// every other influence-recording path writes through and the
// episode-scoped episode_memory_influence_events link, in one transaction:
// a caller must never observe one written without the other. The
// storage-layer episode_memory_influence_events_candidate_boundary trigger
// refuses a candidate that was not actually queried for the episode's own
// task, surfaced here as ErrConstraint.
func (repositories *Repositories) RecordEpisodeMemoryInfluence(
	ctx context.Context,
	input RecordEpisodeMemoryInfluence,
) (EpisodeMemoryInfluence, error) {
	switch {
	case input.ID == "":
		return EpisodeMemoryInfluence{}, errors.New("episode memory influence ID must not be empty")
	case input.EpisodeID.IsZero():
		return EpisodeMemoryInfluence{}, errors.New("episode ID must not be empty")
	case input.CandidateID == "":
		return EpisodeMemoryInfluence{}, errors.New("memory retrieval candidate ID must not be empty")
	}
	decision, reasonKind, err := episodeMemoryInfluenceDecision(input.Action)
	if err != nil {
		return EpisodeMemoryInfluence{}, err
	}
	if err := validateBounded("episode memory influence justification", input.JustificationRedacted, 2048); err != nil {
		return EpisodeMemoryInfluence{}, err
	}
	if err := validateBounded("episode memory influence idempotency key", input.IdempotencyKey, 255); err != nil {
		return EpisodeMemoryInfluence{}, err
	}
	now, micros := repositories.timestamp()
	var record EpisodeMemoryInfluence
	err = repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		existing, found, err := findEpisodeMemoryInfluenceByIdempotency(ctx, transaction.sql, input.EpisodeID, input.IdempotencyKey)
		if err != nil {
			return err
		}
		if found {
			if existing.ID != input.ID || existing.CandidateID != input.CandidateID || existing.Action != input.Action {
				return typedError(ErrConflict, "record episode memory influence", errors.New("idempotency key belongs to a different influence record"))
			}
			record = existing
			return nil
		}
		detail := input.JustificationRedacted
		if _, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO memory_retrieval_decisions (
				id, candidate_id, decision, reason_kind, reason_detail_redacted, decided_at_unix_micros
			) VALUES (?, ?, ?, ?, ?, ?)`,
			"episode-influence:"+input.ID, input.CandidateID, decision, reasonKind, detail, micros,
		); err != nil {
			return repositoryWriteError("record episode memory influence decision", err)
		}
		if _, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO episode_memory_influence_events (
				id, episode_id, candidate_id, action, justification_redacted, idempotency_key, recorded_at_unix_micros
			) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			input.ID, input.EpisodeID, input.CandidateID, input.Action, detail, input.IdempotencyKey, micros,
		); err != nil {
			return repositoryWriteError("record episode memory influence event", err)
		}
		record = EpisodeMemoryInfluence{
			ID: input.ID, EpisodeID: input.EpisodeID, CandidateID: input.CandidateID, Action: input.Action,
			JustificationRedacted: input.JustificationRedacted, IdempotencyKey: input.IdempotencyKey, RecordedAt: now,
		}
		return nil
	})
	if err != nil {
		return EpisodeMemoryInfluence{}, err
	}
	return record, nil
}

// episodeMemoryInfluenceDecision maps an agent-influence action to the
// memory_retrieval_decisions vocabulary, mirroring
// internal/retrievalgate.AgentInfluenceRecord's Accepted/Reason methods
// exactly (used/adapted -> accepted; rejected -> rejected), without
// requiring the retrievalgate.EligibilityDecision that constructing an
// AgentInfluenceRecord there would need -- this package only has a
// candidate's durable identity, never the in-process decision that made it
// eligible.
func episodeMemoryInfluenceDecision(
	action retrievalgate.AgentInfluenceAction,
) (decision string, reasonKind MemoryRetrievalDecisionReason, err error) {
	switch action {
	case retrievalgate.AgentInfluenceActionUsed:
		return "accepted", RetrievalReasonEligibleAndUsed, nil
	case retrievalgate.AgentInfluenceActionAdapted:
		return "accepted", RetrievalReasonEligibleAndAdapted, nil
	case retrievalgate.AgentInfluenceActionRejected:
		return "rejected", RetrievalReasonEligibleAndRejectedByAgent, nil
	default:
		return "", "", errors.New("episode memory influence action is not declared")
	}
}

// ListEpisodeMemoryInfluence reads every influence recorded for episodeID.
func (repositories *Repositories) ListEpisodeMemoryInfluence(
	ctx context.Context,
	episodeID domain.EpisodeID,
) ([]EpisodeMemoryInfluence, error) {
	if episodeID.IsZero() {
		return nil, errors.New("episode ID must not be empty")
	}
	rows, err := repositories.database.sql.QueryContext(
		ctx, episodeMemoryInfluenceSelect+` WHERE episode_id = ? ORDER BY recorded_at_unix_micros, id`, episodeID,
	)
	if err != nil {
		return nil, classify("list episode memory influence", err)
	}
	defer rows.Close()
	return scanEpisodeMemoryInfluence(rows)
}

const episodeMemoryInfluenceSelect = `SELECT
	id, episode_id, candidate_id, action, justification_redacted, idempotency_key, recorded_at_unix_micros
 FROM episode_memory_influence_events`

func findEpisodeMemoryInfluenceByIdempotency(
	ctx context.Context,
	queries interface {
		QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	},
	episodeID domain.EpisodeID,
	key string,
) (EpisodeMemoryInfluence, bool, error) {
	rows, err := queries.QueryContext(
		ctx, episodeMemoryInfluenceSelect+` WHERE episode_id = ? AND idempotency_key = ?`, episodeID, key,
	)
	if err != nil {
		return EpisodeMemoryInfluence{}, false, classify("find episode memory influence by idempotency", err)
	}
	defer rows.Close()
	records, err := scanEpisodeMemoryInfluence(rows)
	if err != nil {
		return EpisodeMemoryInfluence{}, false, err
	}
	if len(records) == 0 {
		return EpisodeMemoryInfluence{}, false, nil
	}
	return records[0], true, nil
}

func scanEpisodeMemoryInfluence(rows *sql.Rows) ([]EpisodeMemoryInfluence, error) {
	var records []EpisodeMemoryInfluence
	for rows.Next() {
		var (
			record         EpisodeMemoryInfluence
			action         string
			recordedMicros int64
		)
		if err := rows.Scan(
			&record.ID, &record.EpisodeID, &record.CandidateID, &action,
			&record.JustificationRedacted, &record.IdempotencyKey, &recordedMicros,
		); err != nil {
			return nil, classify("scan episode memory influence", err)
		}
		record.Action = retrievalgate.AgentInfluenceAction(action)
		record.RecordedAt = repositoryTime(recordedMicros)
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, classify("list episode memory influence", err)
	}
	return records, nil
}
