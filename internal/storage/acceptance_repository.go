package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/acceptance"
	"codeflux.dev/codeflux/internal/domain"
	reportevidence "codeflux.dev/codeflux/internal/evidence"
)

type acceptanceQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// AcceptanceObservation is resolved from the live worktree and validation
// controller at the mutation boundary. It is never accepted from an RPC
// request or UI projection.
type AcceptanceObservation struct {
	ReportID       string
	DiffIdentity   string
	RequiredChecks []acceptance.RequiredCheck
}

// AcceptanceObservationSource supplies authoritative current state. The Git
// service and validation coordinator implement this boundary in production;
// tests use a deterministic in-memory observer.
type AcceptanceObservationSource interface {
	ObserveAcceptance(context.Context, domain.TaskID) (AcceptanceObservation, error)
}

func (repositories *Repositories) SetAcceptanceObservationSource(source AcceptanceObservationSource) {
	repositories.acceptanceObservations = source
}

// ObserveRequiredChecks resolves the newest authoritative execution state for
// every required check in the sealed report. A newer intent without a result
// is running; terminal report dispositions remain authoritative when no newer
// execution exists.
func (repositories *Repositories) ObserveRequiredChecks(
	ctx context.Context,
	taskID domain.TaskID,
	report reportevidence.Report,
) ([]acceptance.RequiredCheck, error) {
	if taskID.IsZero() || report.TaskID != taskID || !lowerDigest(report.DiffIdentity) {
		return nil, errors.New("live required-check scope is invalid")
	}
	checks := make([]acceptance.RequiredCheck, 0, len(report.Validations))
	for _, recorded := range report.Validations {
		if !recorded.Required {
			continue
		}
		status := acceptance.CheckStatus(recorded.Status)
		var resultState, invalidated sql.NullString
		err := repositories.database.sql.QueryRowContext(ctx, `SELECT result.state,
			CASE WHEN EXISTS (
				SELECT 1 FROM validation_run_invalidations AS invalidation
				WHERE invalidation.validation_run_id = intent.id
			) THEN 'invalidated' ELSE NULL END
			FROM validation_run_intents AS intent
			LEFT JOIN validation_run_results AS result ON result.validation_run_id = intent.id
			WHERE intent.task_id = ? AND intent.check_id = ? AND intent.diff_identity = ?
			ORDER BY intent.created_at_unix_micros DESC, intent.id DESC
			LIMIT 1`, taskID, recorded.CheckID, report.DiffIdentity).Scan(&resultState, &invalidated)
		switch {
		case errors.Is(err, sql.ErrNoRows):
		case err != nil:
			return nil, classify("observe live required validation", err)
		case invalidated.Valid:
			status = acceptance.CheckInvalidated
		case !resultState.Valid:
			status = acceptance.CheckRunning
		default:
			status = acceptance.CheckStatus(resultState.String)
		}
		checks = append(checks, acceptance.RequiredCheck{ID: recorded.CheckID, Status: status})
	}
	return checks, nil
}

type OpenAcceptanceReview struct {
	ID                     acceptance.ReviewID
	TaskID                 domain.TaskID
	ReportID               string
	ExpectedReviewRevision uint64
	OpenedBy               string
	IdempotencyKey         string
}

type RecordAcceptance struct {
	ID                     acceptance.AcceptanceID
	TaskID                 domain.TaskID
	ReviewID               acceptance.ReviewID
	ExpectedReviewRevision uint64
	ExpectedReportID       string
	ExpectedDiffIdentity   string
	AcknowledgedCheckIDs   []string
	ActorReference         string
	AuthorityReference     string
	ReasonRedacted         string
	IdempotencyKey         string
}

type AcceptanceDecision struct {
	ID                   acceptance.AcceptanceID
	TaskID               domain.TaskID
	ReviewID             acceptance.ReviewID
	ReviewRevision       uint64
	ReportID             string
	DiffIdentity         string
	AcknowledgedCheckIDs []string
	ActorReference       string
	AuthorityReference   string
	ReasonRedacted       string
	IdempotencyKey       string
	CreatedAt            time.Time
}

type RecordReviewRejection struct {
	ID                     acceptance.RejectionID
	TaskID                 domain.TaskID
	ReviewID               acceptance.ReviewID
	ExpectedReviewRevision uint64
	ExpectedReportID       string
	ExpectedDiffIdentity   string
	PreservePatch          bool
	ReasonRedacted         string
	RejectedBy             string
	IdempotencyKey         string
}

type ReviewRejection struct {
	RecordReviewRejection
	ReportID     string
	DiffIdentity string
	CreatedAt    time.Time
}

type PrepareRepairRollback struct {
	ID                         acceptance.RollbackIntentID
	RepairRequestID            acceptance.RepairRequestID
	TaskID                     domain.TaskID
	TargetCheckpointID         domain.CheckpointID
	ExpectedRepairDiffIdentity string
	Reason                     domain.RollbackReason
	RequestedBy                string
	IdempotencyKey             string
}

type RepairRollbackIntent struct {
	PrepareRepairRollback
	CreatedAt time.Time
}

type RepairRollbackOutcomeKind string

const (
	RepairRollbackRestored RepairRollbackOutcomeKind = "restored"
	RepairRollbackFailed   RepairRollbackOutcomeKind = "failed"
)

type CompleteRepairRollback struct {
	IntentID             acceptance.RollbackIntentID
	Outcome              RepairRollbackOutcomeKind
	ObservedDiffIdentity string
	DetailRedacted       string
	IdempotencyKey       string
}

type RepairRollbackOutcome struct {
	CompleteRepairRollback
	CreatedAt time.Time
}

func (repositories *Repositories) OpenAcceptanceReview(ctx context.Context, input OpenAcceptanceReview) (acceptance.Review, error) {
	if input.ID.String() == "" || input.TaskID.IsZero() || !lowerDigest(input.ReportID) ||
		strings.TrimSpace(input.OpenedBy) == "" || strings.TrimSpace(input.IdempotencyKey) == "" {
		return acceptance.Review{}, errors.New("acceptance review input is incomplete")
	}
	observation, err := repositories.observeAcceptance(ctx, input.TaskID)
	if err != nil {
		return acceptance.Review{}, err
	}
	if observation.ReportID != input.ReportID {
		return acceptance.Review{}, staleReview("open acceptance review", "the final evidence report changed")
	}
	now, micros := repositories.timestamp()
	var value acceptance.Review
	err = repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		existing, found, err := findAcceptanceReviewByKey(ctx, transaction.sql, input.TaskID, input.IdempotencyKey)
		if err != nil {
			return err
		}
		if found {
			if existing.ID != input.ID || existing.ReportID != input.ReportID || existing.DiffIdentity != observation.DiffIdentity || existing.OpenedBy != input.OpenedBy || existing.Revision != input.ExpectedReviewRevision+1 {
				return typedError(ErrConflict, "open acceptance review", errors.New("idempotency key belongs to another review"))
			}
			value = existing
			return nil
		}
		var currentID string
		var currentRevision uint64
		headErr := transaction.sql.QueryRowContext(ctx, `SELECT review_id, review_revision FROM acceptance_review_heads WHERE task_id = ?`, input.TaskID).Scan(&currentID, &currentRevision)
		if errors.Is(headErr, sql.ErrNoRows) {
			if input.ExpectedReviewRevision != 0 {
				return staleReview("open acceptance review", "review head does not exist")
			}
		} else if headErr != nil {
			return classify("load acceptance review head", headErr)
		} else if currentRevision != input.ExpectedReviewRevision {
			return staleReview("open acceptance review", "review head revision changed")
		}
		var diff string
		var planRevision uint64
		if err := transaction.sql.QueryRowContext(ctx, `SELECT report.diff_identity, report.accepted_plan_revision
			FROM final_evidence_reports AS report JOIN final_evidence_report_seals AS seal ON seal.report_id = report.id
			WHERE report.id = ? AND report.task_id = ?`, input.ReportID, input.TaskID).Scan(&diff, &planRevision); err != nil {
			return repositoryReadConstraint("load acceptance review report", err)
		}
		if diff != observation.DiffIdentity {
			return staleReview("open acceptance review", "observed diff differs from report")
		}
		if err := requireLatestEvidenceReport(ctx, transaction.sql, input.TaskID, input.ReportID); err != nil {
			return err
		}
		revision := input.ExpectedReviewRevision + 1
		if _, err := transaction.sql.ExecContext(ctx, `INSERT INTO acceptance_reviews
			(id, task_id, revision, report_id, diff_identity, plan_revision, opened_by, idempotency_key, opened_at_unix_micros)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, input.ID, input.TaskID, revision, input.ReportID, diff, planRevision, input.OpenedBy, input.IdempotencyKey, micros); err != nil {
			return repositoryWriteError("insert acceptance review", err)
		}
		if input.ExpectedReviewRevision == 0 {
			_, err = transaction.sql.ExecContext(ctx, `INSERT INTO acceptance_review_heads (task_id, review_id, review_revision, updated_at_unix_micros) VALUES (?, ?, ?, ?)`, input.TaskID, input.ID, revision, micros)
		} else {
			result, updateErr := transaction.sql.ExecContext(ctx, `UPDATE acceptance_review_heads SET review_id = ?, review_revision = ?, updated_at_unix_micros = ? WHERE task_id = ? AND review_revision = ?`, input.ID, revision, micros, input.TaskID, input.ExpectedReviewRevision)
			if updateErr != nil {
				return repositoryWriteError("renew acceptance review head", updateErr)
			}
			if affected, _ := result.RowsAffected(); affected != 1 {
				return staleReview("renew acceptance review", "review head revision changed")
			}
			err = nil
		}
		if err != nil {
			return repositoryWriteError("write acceptance review head", err)
		}
		value = acceptance.Review{ID: input.ID, TaskID: input.TaskID, Revision: revision, ReportID: input.ReportID, DiffIdentity: diff, PlanRevision: planRevision, RequiredChecks: slices.Clone(observation.RequiredChecks), OpenedBy: input.OpenedBy, IdempotencyKey: input.IdempotencyKey, OpenedAt: now}
		return nil
	})
	return value, err
}

func (repositories *Repositories) GetAcceptanceReview(ctx context.Context, taskID domain.TaskID, reviewID acceptance.ReviewID) (acceptance.Review, error) {
	if taskID.IsZero() || reviewID.String() == "" {
		return acceptance.Review{}, errors.New("acceptance review identity is required")
	}
	value, err := findAcceptanceReview(ctx, repositories.database.sql, taskID, reviewID)
	if err != nil {
		return acceptance.Review{}, err
	}
	return value, nil
}

func (repositories *Repositories) GetAcceptanceReviewHead(ctx context.Context, taskID domain.TaskID) (acceptance.Review, error) {
	if taskID.IsZero() {
		return acceptance.Review{}, errors.New("acceptance review task identity is required")
	}
	var rawID string
	if err := repositories.database.sql.QueryRowContext(ctx, `SELECT review_id FROM acceptance_review_heads WHERE task_id = ?`, taskID).Scan(&rawID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return acceptance.Review{}, typedError(ErrNotFound, "get acceptance review head", err)
		}
		return acceptance.Review{}, classify("get acceptance review head", err)
	}
	id, err := acceptance.ParseReviewID(rawID)
	if err != nil {
		return acceptance.Review{}, typedError(ErrCorrupt, "parse acceptance review head", err)
	}
	return findAcceptanceReview(ctx, repositories.database.sql, taskID, id)
}

func (repositories *Repositories) RecordAcceptance(ctx context.Context, input RecordAcceptance) (AcceptanceDecision, error) {
	if err := validateRecordAcceptance(input); err != nil {
		return AcceptanceDecision{}, err
	}
	observation, err := repositories.observeAcceptance(ctx, input.TaskID)
	if err != nil {
		return AcceptanceDecision{}, err
	}
	now, micros := repositories.timestamp()
	var value AcceptanceDecision
	err = repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		existing, found, err := findAcceptanceDecisionByKey(ctx, transaction.sql, input.TaskID, input.IdempotencyKey)
		if err != nil {
			return err
		}
		if found {
			expected := decisionFromInput(input, existing.CreatedAt)
			expected.ReportID, expected.DiffIdentity, expected.ReviewRevision = existing.ReportID, existing.DiffIdentity, existing.ReviewRevision
			if !reflect.DeepEqual(canonicalAcceptanceDecision(existing), canonicalAcceptanceDecision(expected)) {
				return typedError(ErrConflict, "record acceptance", errors.New("idempotency key belongs to another acceptance"))
			}
			value = existing
			return nil
		}
		review, err := loadCurrentAcceptanceReview(ctx, transaction.sql, input.TaskID, input.ReviewID, input.ExpectedReviewRevision)
		if err != nil {
			return err
		}
		if review.ReportID != input.ExpectedReportID || review.DiffIdentity != input.ExpectedDiffIdentity ||
			observation.ReportID != review.ReportID || observation.DiffIdentity != review.DiffIdentity {
			return staleReview("record acceptance", "review report or diff binding changed")
		}
		if err := requireLatestEvidenceReport(ctx, transaction.sql, input.TaskID, review.ReportID); err != nil {
			return err
		}
		gate, err := acceptance.EvaluateAcceptanceWithLiveChecks(review, observation.ReportID, observation.DiffIdentity, observation.RequiredChecks, input.AcknowledgedCheckIDs)
		if err != nil {
			return err
		}
		if gateErr := acceptance.AcceptanceGateError(gate); gateErr != nil {
			return gateErr
		}
		if _, err := transaction.sql.ExecContext(ctx, `INSERT INTO acceptance_decisions
			(id, review_id, task_id, review_revision, report_id, diff_identity, actor_reference, authority_reference, reason_redacted, idempotency_key, created_at_unix_micros)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, input.ID, input.ReviewID, input.TaskID, review.Revision, review.ReportID, review.DiffIdentity, input.ActorReference, input.AuthorityReference, input.ReasonRedacted, input.IdempotencyKey, micros); err != nil {
			return repositoryWriteError("insert acceptance decision", err)
		}
		for _, checkID := range input.AcknowledgedCheckIDs {
			if _, err := transaction.sql.ExecContext(ctx, `INSERT INTO acceptance_decision_acknowledgements (acceptance_id, report_id, check_id, acknowledged_at_unix_micros) VALUES (?, ?, ?, ?)`, input.ID, review.ReportID, checkID, micros); err != nil {
				return repositoryWriteError("insert acceptance acknowledgement", err)
			}
		}
		if _, err := transaction.sql.ExecContext(ctx, `INSERT INTO acceptance_decision_seals (acceptance_id, sealed_at_unix_micros) VALUES (?, ?)`, input.ID, micros); err != nil {
			return repositoryWriteError("seal acceptance decision", err)
		}
		if _, err := transaction.sql.ExecContext(ctx, `INSERT INTO acceptance_review_resolutions (review_id, task_id, resolution_kind, resolution_id, created_at_unix_micros) VALUES (?, ?, 'accepted', ?, ?)`, input.ReviewID, input.TaskID, input.ID, micros); err != nil {
			return repositoryWriteError("resolve accepted review", err)
		}
		value = decisionFromInput(input, now)
		value.ReportID, value.DiffIdentity, value.ReviewRevision = review.ReportID, review.DiffIdentity, review.Revision
		return nil
	})
	return value, err
}

func (repositories *Repositories) RequestAcceptanceRepair(ctx context.Context, request acceptance.RepairRequest) (acceptance.RepairRequest, error) {
	if !request.CreatedAt.IsZero() {
		return acceptance.RepairRequest{}, errors.New("repair creation time is repository-assigned")
	}
	now, micros := repositories.timestamp()
	request.CreatedAt = now
	if err := request.Validate(); err != nil {
		return acceptance.RepairRequest{}, err
	}
	observation, err := repositories.observeAcceptance(ctx, request.TaskID)
	if err != nil {
		return acceptance.RepairRequest{}, err
	}
	if observation.ReportID != request.ReportID || observation.DiffIdentity != request.DiffIdentity {
		return acceptance.RepairRequest{}, staleReview("request acceptance repair", "the report or worktree diff changed")
	}
	var value acceptance.RepairRequest
	err = repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		existing, found, err := findRepairRequestByKey(ctx, transaction.sql, request.TaskID, request.IdempotencyKey)
		if err != nil {
			return err
		}
		if found {
			request.CreatedAt = existing.CreatedAt
			if !reflect.DeepEqual(request.Clone(), existing.Clone()) {
				return typedError(ErrConflict, "request acceptance repair", errors.New("idempotency key belongs to another repair"))
			}
			value = existing
			return nil
		}
		review, err := loadCurrentAcceptanceReview(ctx, transaction.sql, request.TaskID, request.ReviewID, request.ReviewRevision)
		if err != nil {
			return err
		}
		if review.ReportID != request.ReportID || review.DiffIdentity != request.DiffIdentity || review.PlanRevision != request.PreviousPlanRevision {
			return staleReview("request acceptance repair", "review binding changed")
		}
		if err := requireLatestEvidenceReport(ctx, transaction.sql, request.TaskID, review.ReportID); err != nil {
			return err
		}
		if _, err := transaction.sql.ExecContext(ctx, `INSERT INTO acceptance_repair_requests
			(id, review_id, task_id, review_revision, report_id, diff_identity, previous_plan_revision, new_plan_revision, pre_repair_checkpoint_id, feedback_redacted, requested_by, idempotency_key, created_at_unix_micros)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, request.ID, request.ReviewID, request.TaskID, request.ReviewRevision, request.ReportID, request.DiffIdentity, request.PreviousPlanRevision, request.NewPlanRevision, request.PreRepairCheckpointID, request.Feedback, request.RequestedBy, request.IdempotencyKey, micros); err != nil {
			return repositoryWriteError("insert acceptance repair request", err)
		}
		for ordinal, target := range request.Targets {
			if _, err := transaction.sql.ExecContext(ctx, `INSERT INTO acceptance_repair_targets (repair_request_id, ordinal, target_kind, target_id) VALUES (?, ?, ?, ?)`, request.ID, ordinal, target.Kind, target.ID); err != nil {
				return repositoryWriteError("insert acceptance repair target", err)
			}
		}
		if _, err := transaction.sql.ExecContext(ctx, `INSERT INTO acceptance_repair_request_seals (repair_request_id, sealed_at_unix_micros) VALUES (?, ?)`, request.ID, micros); err != nil {
			return repositoryWriteError("seal acceptance repair request", err)
		}
		if _, err := transaction.sql.ExecContext(ctx, `INSERT INTO acceptance_review_resolutions (review_id, task_id, resolution_kind, resolution_id, created_at_unix_micros) VALUES (?, ?, 'repair-requested', ?, ?)`, request.ReviewID, request.TaskID, request.ID, micros); err != nil {
			return repositoryWriteError("resolve repair review", err)
		}
		value = request.Clone()
		return nil
	})
	return value, err
}

func (repositories *Repositories) RejectAcceptanceReview(ctx context.Context, input RecordReviewRejection) (ReviewRejection, error) {
	if input.ID.String() == "" || input.TaskID.IsZero() || input.ReviewID.String() == "" || input.ExpectedReviewRevision == 0 || !lowerDigest(input.ExpectedReportID) || !lowerDigest(input.ExpectedDiffIdentity) || !input.PreservePatch || strings.TrimSpace(input.ReasonRedacted) == "" || strings.TrimSpace(input.RejectedBy) == "" || strings.TrimSpace(input.IdempotencyKey) == "" {
		return ReviewRejection{}, errors.New("review rejection input is incomplete or destructive")
	}
	observation, err := repositories.observeAcceptance(ctx, input.TaskID)
	if err != nil {
		return ReviewRejection{}, err
	}
	now, micros := repositories.timestamp()
	var value ReviewRejection
	err = repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		var existingID string
		err := transaction.sql.QueryRowContext(ctx, `SELECT id FROM acceptance_rejections WHERE task_id = ? AND idempotency_key = ?`, input.TaskID, input.IdempotencyKey).Scan(&existingID)
		if err == nil {
			if existingID != input.ID.String() {
				return typedError(ErrConflict, "reject acceptance review", errors.New("idempotency key belongs to another rejection"))
			}
			loaded, loadErr := loadReviewRejection(ctx, transaction.sql, input.TaskID, input.ID)
			if loadErr != nil {
				return loadErr
			}
			value = loaded
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return classify("find review rejection", err)
		}
		review, err := loadCurrentAcceptanceReview(ctx, transaction.sql, input.TaskID, input.ReviewID, input.ExpectedReviewRevision)
		if err != nil {
			return err
		}
		if review.ReportID != input.ExpectedReportID || review.DiffIdentity != input.ExpectedDiffIdentity ||
			observation.ReportID != review.ReportID || observation.DiffIdentity != review.DiffIdentity {
			return staleReview("reject acceptance review", "review binding changed")
		}
		if err := requireLatestEvidenceReport(ctx, transaction.sql, input.TaskID, review.ReportID); err != nil {
			return err
		}
		if _, err := transaction.sql.ExecContext(ctx, `INSERT INTO acceptance_rejections
			(id, review_id, task_id, review_revision, report_id, diff_identity, preserve_patch, reason_redacted, rejected_by, idempotency_key, created_at_unix_micros)
			VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?)`, input.ID, input.ReviewID, input.TaskID, review.Revision, review.ReportID, review.DiffIdentity, input.ReasonRedacted, input.RejectedBy, input.IdempotencyKey, micros); err != nil {
			return repositoryWriteError("insert acceptance rejection", err)
		}
		if _, err := transaction.sql.ExecContext(ctx, `INSERT INTO acceptance_review_resolutions (review_id, task_id, resolution_kind, resolution_id, created_at_unix_micros) VALUES (?, ?, 'rejected', ?, ?)`, input.ReviewID, input.TaskID, input.ID, micros); err != nil {
			return repositoryWriteError("resolve rejected review", err)
		}
		value = ReviewRejection{RecordReviewRejection: input, ReportID: review.ReportID, DiffIdentity: review.DiffIdentity, CreatedAt: now}
		return nil
	})
	return value, err
}

func (repositories *Repositories) PrepareAcceptanceRepairRollback(ctx context.Context, input PrepareRepairRollback) (RepairRollbackIntent, error) {
	if input.ID.String() == "" || input.RepairRequestID.String() == "" || input.TaskID.IsZero() || input.TargetCheckpointID.IsZero() || !lowerDigest(input.ExpectedRepairDiffIdentity) || !input.Reason.IsValid() || strings.TrimSpace(input.RequestedBy) == "" || strings.TrimSpace(input.IdempotencyKey) == "" {
		return RepairRollbackIntent{}, errors.New("repair rollback intent input is incomplete")
	}
	now, micros := repositories.timestamp()
	_, err := repositories.database.sql.ExecContext(ctx, `INSERT INTO acceptance_repair_rollback_intents
		(id, repair_request_id, task_id, target_checkpoint_id, expected_repair_diff_identity, reason, requested_by, idempotency_key, created_at_unix_micros)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, input.ID, input.RepairRequestID, input.TaskID, input.TargetCheckpointID, input.ExpectedRepairDiffIdentity, input.Reason, input.RequestedBy, input.IdempotencyKey, micros)
	if err != nil {
		var existingID, repairID, checkpointID, expectedDiff, reason, requestedBy, key string
		var created int64
		findErr := repositories.database.sql.QueryRowContext(ctx, `SELECT id, repair_request_id, target_checkpoint_id, expected_repair_diff_identity, reason, requested_by, idempotency_key, created_at_unix_micros FROM acceptance_repair_rollback_intents WHERE task_id = ? AND idempotency_key = ?`, input.TaskID, input.IdempotencyKey).Scan(&existingID, &repairID, &checkpointID, &expectedDiff, &reason, &requestedBy, &key, &created)
		if findErr == nil && existingID == input.ID.String() && repairID == input.RepairRequestID.String() && checkpointID == input.TargetCheckpointID.String() && expectedDiff == input.ExpectedRepairDiffIdentity && reason == string(input.Reason) && requestedBy == input.RequestedBy && key == input.IdempotencyKey {
			return RepairRollbackIntent{PrepareRepairRollback: input, CreatedAt: repositoryTime(created)}, nil
		}
		return RepairRollbackIntent{}, repositoryWriteError("prepare acceptance repair rollback", err)
	}
	return RepairRollbackIntent{PrepareRepairRollback: input, CreatedAt: now}, nil
}

func (repositories *Repositories) CompleteAcceptanceRepairRollback(ctx context.Context, input CompleteRepairRollback) (RepairRollbackOutcome, error) {
	if input.IntentID.String() == "" || (input.Outcome != RepairRollbackRestored && input.Outcome != RepairRollbackFailed) || !lowerDigest(input.ObservedDiffIdentity) || strings.TrimSpace(input.DetailRedacted) == "" || strings.TrimSpace(input.IdempotencyKey) == "" {
		return RepairRollbackOutcome{}, errors.New("repair rollback outcome input is incomplete")
	}
	now, micros := repositories.timestamp()
	_, err := repositories.database.sql.ExecContext(ctx, `INSERT INTO acceptance_repair_rollback_outcomes
		(rollback_intent_id, outcome, observed_diff_identity, detail_redacted, idempotency_key, created_at_unix_micros)
		VALUES (?, ?, ?, ?, ?, ?)`, input.IntentID, input.Outcome, input.ObservedDiffIdentity, input.DetailRedacted, input.IdempotencyKey, micros)
	if err != nil {
		var outcome RepairRollbackOutcomeKind
		var observed, detail, key string
		var created int64
		findErr := repositories.database.sql.QueryRowContext(ctx, `SELECT outcome, observed_diff_identity, detail_redacted, idempotency_key, created_at_unix_micros FROM acceptance_repair_rollback_outcomes WHERE rollback_intent_id = ?`, input.IntentID).Scan(&outcome, &observed, &detail, &key, &created)
		if findErr == nil && outcome == input.Outcome && observed == input.ObservedDiffIdentity && detail == input.DetailRedacted && key == input.IdempotencyKey {
			return RepairRollbackOutcome{CompleteRepairRollback: input, CreatedAt: repositoryTime(created)}, nil
		}
		return RepairRollbackOutcome{}, repositoryWriteError("complete acceptance repair rollback", err)
	}
	return RepairRollbackOutcome{CompleteRepairRollback: input, CreatedAt: now}, nil
}

func findAcceptanceReviewByKey(ctx context.Context, queries acceptanceQuerier, taskID domain.TaskID, key string) (acceptance.Review, bool, error) {
	var id string
	err := queries.QueryRowContext(ctx, `SELECT id FROM acceptance_reviews WHERE task_id = ? AND idempotency_key = ?`, taskID, key).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return acceptance.Review{}, false, nil
	}
	if err != nil {
		return acceptance.Review{}, false, classify("find acceptance review", err)
	}
	parsed, err := acceptance.ParseReviewID(id)
	if err != nil {
		return acceptance.Review{}, false, typedError(ErrCorrupt, "parse acceptance review identity", err)
	}
	value, err := findAcceptanceReview(ctx, queries, taskID, parsed)
	return value, err == nil, err
}

func findAcceptanceReview(ctx context.Context, queries acceptanceQuerier, taskID domain.TaskID, reviewID acceptance.ReviewID) (acceptance.Review, error) {
	var value acceptance.Review
	var rawID string
	var openedMicros int64
	err := queries.QueryRowContext(ctx, `SELECT id, task_id, revision, report_id, diff_identity, plan_revision, opened_by, idempotency_key, opened_at_unix_micros FROM acceptance_reviews WHERE id = ? AND task_id = ?`, reviewID, taskID).Scan(&rawID, &value.TaskID, &value.Revision, &value.ReportID, &value.DiffIdentity, &value.PlanRevision, &value.OpenedBy, &value.IdempotencyKey, &openedMicros)
	if errors.Is(err, sql.ErrNoRows) {
		return acceptance.Review{}, typedError(ErrNotFound, "get acceptance review", err)
	}
	if err != nil {
		return acceptance.Review{}, classify("get acceptance review", err)
	}
	value.ID, err = acceptance.ParseReviewID(rawID)
	if err != nil {
		return acceptance.Review{}, typedError(ErrCorrupt, "parse acceptance review identity", err)
	}
	value.OpenedAt = repositoryTime(openedMicros)
	if err := loadRequiredReviewChecks(ctx, queries, &value); err != nil {
		return acceptance.Review{}, err
	}
	if err := value.Validate(); err != nil {
		return acceptance.Review{}, typedError(ErrCorrupt, "validate acceptance review", err)
	}
	return value, nil
}

func loadRequiredReviewChecks(ctx context.Context, queries acceptanceQuerier, review *acceptance.Review) error {
	rows, err := queries.QueryContext(ctx, `SELECT check_id, status FROM final_evidence_report_validations WHERE report_id = ? AND required = 1 ORDER BY ordinal`, review.ReportID)
	if err != nil {
		return classify("load acceptance review checks", err)
	}
	defer rows.Close()
	for rows.Next() {
		var check acceptance.RequiredCheck
		if err := rows.Scan(&check.ID, &check.Status); err != nil {
			return classify("scan acceptance review check", err)
		}
		review.RequiredChecks = append(review.RequiredChecks, check)
	}
	return classify("iterate acceptance review checks", rows.Err())
}

func loadCurrentAcceptanceReview(ctx context.Context, queries acceptanceQuerier, taskID domain.TaskID, reviewID acceptance.ReviewID, revision uint64) (acceptance.Review, error) {
	var currentID string
	var currentRevision uint64
	if err := queries.QueryRowContext(ctx, `SELECT review_id, review_revision FROM acceptance_review_heads WHERE task_id = ?`, taskID).Scan(&currentID, &currentRevision); err != nil {
		return acceptance.Review{}, repositoryReadConstraint("load current acceptance review", err)
	}
	if currentID != reviewID.String() || currentRevision != revision {
		return acceptance.Review{}, staleReview("load current acceptance review", "review head changed")
	}
	var resolved int
	if err := queries.QueryRowContext(ctx, `SELECT COUNT(*) FROM acceptance_review_resolutions WHERE review_id = ?`, reviewID).Scan(&resolved); err != nil {
		return acceptance.Review{}, classify("check acceptance review resolution", err)
	}
	if resolved != 0 {
		return acceptance.Review{}, staleReview("load current acceptance review", "review is already resolved")
	}
	return findAcceptanceReview(ctx, queries, taskID, reviewID)
}

func requireLatestEvidenceReport(ctx context.Context, queries acceptanceQuerier, taskID domain.TaskID, reportID string) error {
	var latest string
	err := queries.QueryRowContext(ctx, `SELECT report.id FROM final_evidence_reports AS report JOIN final_evidence_report_seals AS seal ON seal.report_id = report.id WHERE report.task_id = ? ORDER BY report.created_at_unix_micros DESC, report.id DESC LIMIT 1`, taskID).Scan(&latest)
	if err != nil {
		return repositoryReadConstraint("load latest evidence report", err)
	}
	if latest != reportID {
		return staleReview("verify latest evidence report", "a newer final report requires renewed review")
	}
	return nil
}

func findAcceptanceDecisionByKey(ctx context.Context, queries acceptanceQuerier, taskID domain.TaskID, key string) (AcceptanceDecision, bool, error) {
	var rawID string
	err := queries.QueryRowContext(ctx, `SELECT decision.id FROM acceptance_decisions AS decision JOIN acceptance_decision_seals AS seal ON seal.acceptance_id = decision.id WHERE decision.task_id = ? AND decision.idempotency_key = ?`, taskID, key).Scan(&rawID)
	if errors.Is(err, sql.ErrNoRows) {
		return AcceptanceDecision{}, false, nil
	}
	if err != nil {
		return AcceptanceDecision{}, false, classify("find acceptance decision", err)
	}
	id, err := acceptance.ParseAcceptanceID(rawID)
	if err != nil {
		return AcceptanceDecision{}, false, typedError(ErrCorrupt, "parse acceptance decision identity", err)
	}
	value, err := loadAcceptanceDecision(ctx, queries, taskID, id)
	return value, err == nil, err
}

func loadAcceptanceDecision(ctx context.Context, queries acceptanceQuerier, taskID domain.TaskID, id acceptance.AcceptanceID) (AcceptanceDecision, error) {
	var value AcceptanceDecision
	var rawID, rawReviewID string
	var micros int64
	err := queries.QueryRowContext(ctx, `SELECT decision.id, decision.task_id, decision.review_id, decision.review_revision, decision.report_id, decision.diff_identity, decision.actor_reference, decision.authority_reference, decision.reason_redacted, decision.idempotency_key, decision.created_at_unix_micros FROM acceptance_decisions AS decision JOIN acceptance_decision_seals AS seal ON seal.acceptance_id = decision.id WHERE decision.id = ? AND decision.task_id = ?`, id, taskID).Scan(&rawID, &value.TaskID, &rawReviewID, &value.ReviewRevision, &value.ReportID, &value.DiffIdentity, &value.ActorReference, &value.AuthorityReference, &value.ReasonRedacted, &value.IdempotencyKey, &micros)
	if err != nil {
		return AcceptanceDecision{}, repositoryReadConstraint("load acceptance decision", err)
	}
	value.ID, err = acceptance.ParseAcceptanceID(rawID)
	if err != nil {
		return AcceptanceDecision{}, typedError(ErrCorrupt, "parse acceptance identity", err)
	}
	value.ReviewID, err = acceptance.ParseReviewID(rawReviewID)
	if err != nil {
		return AcceptanceDecision{}, typedError(ErrCorrupt, "parse acceptance review identity", err)
	}
	value.CreatedAt = repositoryTime(micros)
	rows, err := queries.QueryContext(ctx, `SELECT check_id FROM acceptance_decision_acknowledgements WHERE acceptance_id = ? ORDER BY check_id`, id)
	if err != nil {
		return AcceptanceDecision{}, classify("load acceptance acknowledgements", err)
	}
	defer rows.Close()
	for rows.Next() {
		var checkID string
		if err := rows.Scan(&checkID); err != nil {
			return AcceptanceDecision{}, classify("scan acceptance acknowledgement", err)
		}
		value.AcknowledgedCheckIDs = append(value.AcknowledgedCheckIDs, checkID)
	}
	if err := rows.Err(); err != nil {
		return AcceptanceDecision{}, classify("iterate acceptance acknowledgements", err)
	}
	return value, nil
}

func findRepairRequestByKey(ctx context.Context, queries acceptanceQuerier, taskID domain.TaskID, key string) (acceptance.RepairRequest, bool, error) {
	var rawID string
	err := queries.QueryRowContext(ctx, `SELECT repair.id FROM acceptance_repair_requests AS repair JOIN acceptance_repair_request_seals AS seal ON seal.repair_request_id = repair.id WHERE repair.task_id = ? AND repair.idempotency_key = ?`, taskID, key).Scan(&rawID)
	if errors.Is(err, sql.ErrNoRows) {
		return acceptance.RepairRequest{}, false, nil
	}
	if err != nil {
		return acceptance.RepairRequest{}, false, classify("find acceptance repair", err)
	}
	id, err := acceptance.ParseRepairRequestID(rawID)
	if err != nil {
		return acceptance.RepairRequest{}, false, typedError(ErrCorrupt, "parse repair request identity", err)
	}
	value, err := loadRepairRequest(ctx, queries, taskID, id)
	return value, err == nil, err
}

func loadRepairRequest(ctx context.Context, queries acceptanceQuerier, taskID domain.TaskID, id acceptance.RepairRequestID) (acceptance.RepairRequest, error) {
	var value acceptance.RepairRequest
	var rawID, rawReviewID string
	var micros int64
	err := queries.QueryRowContext(ctx, `SELECT repair.id, repair.review_id, repair.task_id, repair.review_revision, repair.report_id, repair.diff_identity, repair.previous_plan_revision, repair.new_plan_revision, repair.pre_repair_checkpoint_id, repair.feedback_redacted, repair.requested_by, repair.idempotency_key, repair.created_at_unix_micros FROM acceptance_repair_requests AS repair JOIN acceptance_repair_request_seals AS seal ON seal.repair_request_id = repair.id WHERE repair.id = ? AND repair.task_id = ?`, id, taskID).Scan(&rawID, &rawReviewID, &value.TaskID, &value.ReviewRevision, &value.ReportID, &value.DiffIdentity, &value.PreviousPlanRevision, &value.NewPlanRevision, &value.PreRepairCheckpointID, &value.Feedback, &value.RequestedBy, &value.IdempotencyKey, &micros)
	if err != nil {
		return acceptance.RepairRequest{}, repositoryReadConstraint("load acceptance repair", err)
	}
	value.ID, err = acceptance.ParseRepairRequestID(rawID)
	if err != nil {
		return acceptance.RepairRequest{}, typedError(ErrCorrupt, "parse repair identity", err)
	}
	value.ReviewID, err = acceptance.ParseReviewID(rawReviewID)
	if err != nil {
		return acceptance.RepairRequest{}, typedError(ErrCorrupt, "parse repair review identity", err)
	}
	value.CreatedAt = repositoryTime(micros)
	rows, err := queries.QueryContext(ctx, `SELECT target_kind, target_id FROM acceptance_repair_targets WHERE repair_request_id = ? ORDER BY ordinal`, id)
	if err != nil {
		return acceptance.RepairRequest{}, classify("load acceptance repair targets", err)
	}
	defer rows.Close()
	for rows.Next() {
		var target acceptance.RepairTarget
		if err := rows.Scan(&target.Kind, &target.ID); err != nil {
			return acceptance.RepairRequest{}, classify("scan acceptance repair target", err)
		}
		value.Targets = append(value.Targets, target)
	}
	if err := rows.Err(); err != nil {
		return acceptance.RepairRequest{}, classify("iterate acceptance repair targets", err)
	}
	return value, nil
}

func loadReviewRejection(ctx context.Context, queries acceptanceQuerier, taskID domain.TaskID, id acceptance.RejectionID) (ReviewRejection, error) {
	var value ReviewRejection
	var rawID, rawReviewID string
	var micros int64
	err := queries.QueryRowContext(ctx, `SELECT id, review_id, task_id, review_revision, report_id, diff_identity, preserve_patch, reason_redacted, rejected_by, idempotency_key, created_at_unix_micros FROM acceptance_rejections WHERE id = ? AND task_id = ?`, id, taskID).Scan(&rawID, &rawReviewID, &value.TaskID, &value.ExpectedReviewRevision, &value.ReportID, &value.DiffIdentity, &value.PreservePatch, &value.ReasonRedacted, &value.RejectedBy, &value.IdempotencyKey, &micros)
	if err != nil {
		return ReviewRejection{}, repositoryReadConstraint("load review rejection", err)
	}
	value.ID, err = acceptance.ParseRejectionID(rawID)
	if err != nil {
		return ReviewRejection{}, err
	}
	value.ReviewID, err = acceptance.ParseReviewID(rawReviewID)
	if err != nil {
		return ReviewRejection{}, err
	}
	value.ExpectedReportID, value.ExpectedDiffIdentity = value.ReportID, value.DiffIdentity
	value.CreatedAt = repositoryTime(micros)
	return value, nil
}

func validateRecordAcceptance(input RecordAcceptance) error {
	if input.ID.String() == "" || input.TaskID.IsZero() || input.ReviewID.String() == "" || input.ExpectedReviewRevision == 0 || !lowerDigest(input.ExpectedReportID) || !lowerDigest(input.ExpectedDiffIdentity) || strings.TrimSpace(input.ActorReference) == "" || strings.TrimSpace(input.AuthorityReference) == "" || strings.TrimSpace(input.ReasonRedacted) == "" || strings.TrimSpace(input.IdempotencyKey) == "" {
		return errors.New("acceptance decision input is incomplete")
	}
	return nil
}

func (repositories *Repositories) observeAcceptance(ctx context.Context, taskID domain.TaskID) (AcceptanceObservation, error) {
	if repositories.acceptanceObservations == nil {
		return AcceptanceObservation{}, errors.New("authoritative acceptance observation source is required")
	}
	observation, err := repositories.acceptanceObservations.ObserveAcceptance(ctx, taskID)
	if err != nil {
		return AcceptanceObservation{}, fmt.Errorf("observe authoritative acceptance state: %w", err)
	}
	if !lowerDigest(observation.ReportID) || !lowerDigest(observation.DiffIdentity) {
		return AcceptanceObservation{}, errors.New("authoritative acceptance observation is invalid")
	}
	seen := map[string]struct{}{}
	for _, check := range observation.RequiredChecks {
		if strings.TrimSpace(check.ID) == "" || !check.Status.IsValid() {
			return AcceptanceObservation{}, errors.New("authoritative required-check observation is invalid")
		}
		if _, duplicate := seen[check.ID]; duplicate {
			return AcceptanceObservation{}, errors.New("authoritative required-check observation contains a duplicate")
		}
		seen[check.ID] = struct{}{}
	}
	observation.RequiredChecks = slices.Clone(observation.RequiredChecks)
	return observation, nil
}

func decisionFromInput(input RecordAcceptance, createdAt time.Time) AcceptanceDecision {
	value := AcceptanceDecision{ID: input.ID, TaskID: input.TaskID, ReviewID: input.ReviewID, ReviewRevision: input.ExpectedReviewRevision, ReportID: input.ExpectedReportID, DiffIdentity: input.ExpectedDiffIdentity, AcknowledgedCheckIDs: slices.Clone(input.AcknowledgedCheckIDs), ActorReference: input.ActorReference, AuthorityReference: input.AuthorityReference, ReasonRedacted: input.ReasonRedacted, IdempotencyKey: input.IdempotencyKey, CreatedAt: createdAt}
	slices.Sort(value.AcknowledgedCheckIDs)
	return value
}

func canonicalAcceptanceDecision(value AcceptanceDecision) AcceptanceDecision {
	value.AcknowledgedCheckIDs = slices.Clone(value.AcknowledgedCheckIDs)
	slices.Sort(value.AcknowledgedCheckIDs)
	if len(value.AcknowledgedCheckIDs) == 0 {
		value.AcknowledgedCheckIDs = nil
	}
	return value
}

func repositoryReadConstraint(operation string, err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return typedError(ErrConstraint, operation, err)
	}
	return classify(operation, err)
}

func staleReview(operation, reason string) error {
	return fmt.Errorf("%w: %w: %s: %s", ErrStaleRevision, acceptance.ErrStaleReview, operation, reason)
}

func lowerDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}
