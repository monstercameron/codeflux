package storage

import (
	"context"
	"database/sql"
	"errors"
	"sort"

	"codeflux.dev/codeflux/internal/domain"
	reportevidence "codeflux.dev/codeflux/internal/evidence"
)

// ReviewValidationAttribution is the bounded, redacted validation projection
// used to make review-diff provenance actionable without exposing commands or
// raw validation output.
type ReviewValidationAttribution struct {
	ID      domain.ValidationID
	State   domain.ValidationState
	Label   string
	Summary string
}

// ReviewStepAttribution groups authoritative event and validation links by the
// accepted plan step that owns them.
type ReviewStepAttribution struct {
	StepID      string
	EventIDs    []domain.EventID
	Validations []ReviewValidationAttribution
}

// GetLatestFinalEvidenceReport returns only a sealed report. Ordering is
// deterministic when repository clocks have equal precision.
func (repositories *Repositories) GetLatestFinalEvidenceReport(
	ctx context.Context,
	taskID domain.TaskID,
) (reportevidence.Report, error) {
	if taskID.IsZero() {
		return reportevidence.Report{}, errors.New("task ID must not be empty")
	}
	var reportID string
	err := repositories.database.sql.QueryRowContext(ctx, `
		SELECT report.id
		FROM final_evidence_reports AS report
		JOIN final_evidence_report_seals AS seal ON seal.report_id = report.id
		WHERE report.task_id = ?
		ORDER BY report.created_at_unix_micros DESC, report.id DESC
		LIMIT 1`, taskID).Scan(&reportID)
	if errors.Is(err, sql.ErrNoRows) {
		return reportevidence.Report{}, typedError(
			ErrNotFound, "get latest final evidence report", err,
		)
	}
	if err != nil {
		return reportevidence.Report{}, classify("get latest final evidence report", err)
	}
	return repositories.GetFinalEvidenceReport(ctx, taskID, reportID)
}

// ListReviewStepAttributions reads only links bound to the exact task, plan,
// and sealed graph revision carried by the final report.
func (repositories *Repositories) ListReviewStepAttributions(
	ctx context.Context,
	taskID domain.TaskID,
	planRevision uint64,
	graphRevisionID domain.GraphRevisionID,
) ([]ReviewStepAttribution, error) {
	if taskID.IsZero() || planRevision == 0 || graphRevisionID.IsZero() {
		return nil, errors.New("review attribution scope is incomplete")
	}
	grouped := map[string]*ReviewStepAttribution{}
	eventRows, err := repositories.database.sql.QueryContext(ctx, `
		SELECT DISTINCT step.step_id, event.event_id
		FROM graph_node_plan_step_links AS step
		JOIN graph_revision_seals AS seal
		  ON seal.graph_revision_id = step.graph_revision_id
		JOIN graph_node_event_links AS event
		  ON event.graph_revision_id = step.graph_revision_id
		 AND event.node_id = step.node_id
		 AND event.task_id = step.task_id
		WHERE step.task_id = ? AND step.plan_revision = ?
		  AND step.graph_revision_id = ?
		ORDER BY step.step_id, event.event_id`, taskID, planRevision, graphRevisionID)
	if err != nil {
		return nil, classify("list review step events", err)
	}
	for eventRows.Next() {
		var stepID, rawEventID string
		if err := eventRows.Scan(&stepID, &rawEventID); err != nil {
			eventRows.Close()
			return nil, classify("scan review step event", err)
		}
		eventID, parseErr := domain.ParseEventID(rawEventID)
		if parseErr != nil {
			eventRows.Close()
			return nil, typedError(ErrCorrupt, "parse review step event", parseErr)
		}
		value := reviewStepAttribution(grouped, stepID)
		value.EventIDs = append(value.EventIDs, eventID)
	}
	if err := eventRows.Close(); err != nil {
		return nil, classify("close review step events", err)
	}
	if err := eventRows.Err(); err != nil {
		return nil, classify("iterate review step events", err)
	}

	validationRows, err := repositories.database.sql.QueryContext(ctx, `
		SELECT attribution.plan_step_id, validation.id, validation.state,
		       validation.profile_name, COALESCE(validation.summary_redacted, '')
		FROM plan_validation_attributions AS attribution
		JOIN validations AS validation ON validation.id = attribution.validation_id
		WHERE attribution.task_id = ? AND attribution.plan_revision = ?
		ORDER BY attribution.plan_step_id, validation.id`, taskID, planRevision)
	if err != nil {
		return nil, classify("list review step validations", err)
	}
	for validationRows.Next() {
		var stepID, rawValidationID string
		var item ReviewValidationAttribution
		if err := validationRows.Scan(
			&stepID, &rawValidationID, &item.State, &item.Label, &item.Summary,
		); err != nil {
			validationRows.Close()
			return nil, classify("scan review step validation", err)
		}
		validationID, parseErr := domain.ParseValidationID(rawValidationID)
		if parseErr != nil || !item.State.IsValid() {
			validationRows.Close()
			if parseErr == nil {
				parseErr = errors.New("validation state is invalid")
			}
			return nil, typedError(ErrCorrupt, "parse review step validation", parseErr)
		}
		item.ID = validationID
		value := reviewStepAttribution(grouped, stepID)
		value.Validations = append(value.Validations, item)
	}
	if err := validationRows.Close(); err != nil {
		return nil, classify("close review step validations", err)
	}
	if err := validationRows.Err(); err != nil {
		return nil, classify("iterate review step validations", err)
	}

	result := make([]ReviewStepAttribution, 0, len(grouped))
	for _, value := range grouped {
		result = append(result, *value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].StepID < result[j].StepID })
	return result, nil
}

func reviewStepAttribution(
	grouped map[string]*ReviewStepAttribution,
	stepID string,
) *ReviewStepAttribution {
	if existing := grouped[stepID]; existing != nil {
		return existing
	}
	created := &ReviewStepAttribution{StepID: stepID}
	grouped[stepID] = created
	return created
}
