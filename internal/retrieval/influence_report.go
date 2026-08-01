package retrieval

import (
	"context"
	"errors"
	"fmt"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
)

// MaximumReportedInfluenceRecords bounds one task's influence report.
// docs/plan.md §27A requires every list query to be bounded rather than
// loading all history.
const MaximumReportedInfluenceRecords = 500

// InfluenceDisposition says what actually became of one retrieved memory
// candidate. docs/plan.md §31 treats retrieval and influence as different
// facts, so these are deliberately distinct values rather than a bool.
type InfluenceDisposition string

const (
	// InfluenceDispositionInfluenced means the gate found the candidate
	// eligible AND the agent went on to use or adapt it.
	InfluenceDispositionInfluenced InfluenceDisposition = "influenced"
	// InfluenceDispositionRetrievedOnly means the candidate was surfaced
	// but never acted on. It did not influence the outcome.
	InfluenceDispositionRetrievedOnly InfluenceDisposition = "retrieved-only"
	// InfluenceDispositionRejected means an eligibility gate or the agent
	// rejected the candidate, with ReasonKind saying which.
	InfluenceDispositionRejected InfluenceDisposition = "rejected"
)

// TaskMemoryInfluenceRecord is one retrieved candidate and its fate.
type TaskMemoryInfluenceRecord struct {
	QueryID              string
	CandidateID          string
	RevisionID           domain.MemoryArtifactRevisionID
	Rank                 int
	Channels             []storage.MemoryRetrievalCandidateSource
	Disposition          InfluenceDisposition
	ReasonKind           string
	ReasonDetailRedacted string
}

// TaskMemoryInfluenceReport answers M21-G03: "the user can identify every
// memory item that influenced a completed task."
//
// Records covers every candidate the task ever retrieved, so a reviewer can
// see what was considered and not merely what won. Influenced is the subset
// that actually changed the work. FellBackQueries records retrievals that
// found nothing eligible, which is a normal outcome (M21-076) and part of an
// honest account of what memory did.
type TaskMemoryInfluenceReport struct {
	TaskID           domain.TaskID
	Records          []TaskMemoryInfluenceRecord
	Influenced       []TaskMemoryInfluenceRecord
	FellBackQueries  []string
	Truncated        bool
	TruncationReason string
}

// influenceReportStore is the extra read surface this report needs beyond
// the pre-work gate's own store.
type influenceReportStore interface {
	ListMemoryRetrievalQueriesByTask(context.Context, domain.TaskID) ([]storage.MemoryRetrievalQueryRecord, error)
	ListMemoryRetrievalCandidatesForQuery(context.Context, string) ([]storage.MemoryRetrievalCandidateRecord, error)
	ListMemoryRetrievalCandidateChannels(context.Context, string) ([]storage.MemoryRetrievalCandidateSource, error)
	GetMemoryRetrievalDecision(context.Context, string) (storage.MemoryRetrievalDecisionRecord, bool, error)
	GetMemoryRetrievalFallback(context.Context, string) (storage.MemoryRetrievalFallback, bool, error)
}

// ListTaskMemoryInfluence builds the M21-G03 report for one task.
//
// It reads only durable retrieval records, so the answer is reconstructable
// after a restart and does not depend on anything held in memory during the
// original run.
func (service *Service) ListTaskMemoryInfluence(
	ctx context.Context,
	taskID domain.TaskID,
) (TaskMemoryInfluenceReport, error) {
	if service == nil || service.store == nil {
		return TaskMemoryInfluenceReport{}, errors.New("retrieval service is unavailable")
	}
	if taskID.IsZero() {
		return TaskMemoryInfluenceReport{}, errors.New("task ID must not be empty")
	}
	reader, ok := service.store.(influenceReportStore)
	if !ok {
		return TaskMemoryInfluenceReport{}, errors.New("retrieval store does not expose the influence read surface")
	}

	queries, err := reader.ListMemoryRetrievalQueriesByTask(ctx, taskID)
	if err != nil {
		return TaskMemoryInfluenceReport{}, err
	}

	report := TaskMemoryInfluenceReport{TaskID: taskID}
	for _, query := range queries {
		fallback, fellBack, err := reader.GetMemoryRetrievalFallback(ctx, query.ID)
		if err != nil {
			return TaskMemoryInfluenceReport{}, err
		}
		if fellBack {
			report.FellBackQueries = append(report.FellBackQueries, fallback.QueryID)
		}

		candidates, err := reader.ListMemoryRetrievalCandidatesForQuery(ctx, query.ID)
		if err != nil {
			return TaskMemoryInfluenceReport{}, err
		}
		for _, candidate := range candidates {
			if len(report.Records) >= MaximumReportedInfluenceRecords {
				report.Truncated = true
				report.TruncationReason = fmt.Sprintf(
					"task retrieved more than %d candidates; showing the first %d in query and rank order",
					MaximumReportedInfluenceRecords, MaximumReportedInfluenceRecords,
				)
				break
			}
			record, err := buildInfluenceRecord(ctx, reader, query.ID, candidate)
			if err != nil {
				return TaskMemoryInfluenceReport{}, err
			}
			report.Records = append(report.Records, record)
			if record.Disposition == InfluenceDispositionInfluenced {
				report.Influenced = append(report.Influenced, record)
			}
		}
		if report.Truncated {
			break
		}
	}
	return report, nil
}

// buildInfluenceRecord resolves one candidate's channels and disposition.
func buildInfluenceRecord(
	ctx context.Context,
	reader influenceReportStore,
	queryID string,
	candidate storage.MemoryRetrievalCandidateRecord,
) (TaskMemoryInfluenceRecord, error) {
	record := TaskMemoryInfluenceRecord{
		QueryID:     queryID,
		CandidateID: candidate.ID,
		RevisionID:  candidate.RevisionID,
		Rank:        candidate.Rank,
	}
	channels, err := reader.ListMemoryRetrievalCandidateChannels(ctx, candidate.ID)
	if err != nil {
		return TaskMemoryInfluenceRecord{}, err
	}
	if len(channels) == 0 {
		// Pre-000028 rows carry only the collapsed single-valued column.
		channels = []storage.MemoryRetrievalCandidateSource{candidate.CandidateSource}
	}
	record.Channels = channels

	decision, decided, err := reader.GetMemoryRetrievalDecision(ctx, candidate.ID)
	if err != nil {
		return TaskMemoryInfluenceRecord{}, err
	}
	if !decided {
		// Eligible, surfaced to the caller, never acted on. §31 forbids
		// reading this as influence.
		record.Disposition = InfluenceDispositionRetrievedOnly
		return record, nil
	}

	record.ReasonKind = string(decision.ReasonKind)
	if decision.ReasonDetailRedacted != nil {
		record.ReasonDetailRedacted = *decision.ReasonDetailRedacted
	}
	if decision.Decision == "accepted" {
		record.Disposition = InfluenceDispositionInfluenced
	} else {
		record.Disposition = InfluenceDispositionRejected
	}
	return record, nil
}
