package storage

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	reportevidence "codeflux.dev/codeflux/internal/evidence"
)

type EvidenceReportOperations interface {
	RecordFinalEvidenceReport(context.Context, reportevidence.Report) (reportevidence.Report, error)
	GetFinalEvidenceReport(context.Context, domain.TaskID, string) (reportevidence.Report, error)
}

var _ EvidenceReportOperations = (*Repositories)(nil)

// RecordFinalEvidenceReport persists the complete structured report and all
// claim provenance atomically. No Markdown or sidecar representation is used.
func (repositories *Repositories) RecordFinalEvidenceReport(
	ctx context.Context,
	report reportevidence.Report,
) (reportevidence.Report, error) {
	if !report.CreatedAt.IsZero() {
		return reportevidence.Report{}, errors.New("final evidence report creation time is repository-assigned")
	}
	now, micros := repositories.timestamp()
	report.CreatedAt = now
	if err := report.Validate(); err != nil {
		return reportevidence.Report{}, err
	}
	replayed := false
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		var existingID string
		err := transaction.sql.QueryRowContext(ctx, `SELECT report.id FROM final_evidence_reports AS report
			JOIN final_evidence_report_seals AS seal ON seal.report_id = report.id
			WHERE report.task_id = ? AND report.idempotency_key = ?`, report.TaskID, report.IdempotencyKey).Scan(&existingID)
		if err == nil {
			if existingID != report.ID {
				return typedError(ErrConflict, "record idempotent final evidence report", errors.New("idempotency key belongs to another report"))
			}
			replayed = true
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return classify("find idempotent final evidence report", err)
		}
		if err := validateEvidenceReportValidationProvenance(ctx, transaction.sql, report); err != nil {
			return typedError(ErrConstraint, "validate final evidence report validation provenance", err)
		}
		if err := insertEvidenceReportRoot(ctx, transaction, report, micros); err != nil {
			return err
		}
		if err := insertEvidenceReportChildren(ctx, transaction, report); err != nil {
			return err
		}
		if _, err := transaction.sql.ExecContext(ctx, `INSERT INTO final_evidence_report_seals
			(report_id, sealed_at_unix_micros) VALUES (?, ?)`, report.ID, micros); err != nil {
			return repositoryWriteError("seal final evidence report", err)
		}
		return nil
	})
	if err != nil {
		return reportevidence.Report{}, err
	}
	stored, err := repositories.GetFinalEvidenceReport(ctx, report.TaskID, report.ID)
	if err != nil {
		return reportevidence.Report{}, err
	}
	if replayed && !equivalentEvidenceReport(report, stored) {
		return reportevidence.Report{}, typedError(ErrConflict, "record idempotent final evidence report", errors.New("report content differs from committed report"))
	}
	return stored, nil
}

func insertEvidenceReportRoot(ctx context.Context, transaction *Transaction, report reportevidence.Report, micros int64) error {
	metrics := report.Metrics
	_, err := transaction.sql.ExecContext(ctx, `INSERT INTO final_evidence_reports (
		id, schema_version, task_id, requirement_revision, accepted_plan_revision, plan_approval_id,
		base_revision, diff_identity, risk_classification_revision, risk_level, risk_explanation,
		graph_revision_id, forecast_duration_known, forecast_p50_nanos, forecast_p90_nanos, forecast_duration_unknown_reason,
		forecast_tokens_known, forecast_tokens_p50, forecast_tokens_p90, forecast_tokens_unknown_reason, forecast_cost_known,
		forecast_cost_p50_minor, forecast_cost_p90_minor, forecast_currency, forecast_cost_unknown_reason, actual_duration_known,
		actual_duration_nanos, actual_duration_unknown_reason, actual_tokens_known, actual_input_tokens, actual_cached_input_tokens,
		actual_cache_write_tokens, actual_output_tokens, actual_reasoning_tokens, actual_tokens_unknown_reason,
		actual_cost_known, actual_cost_minor, actual_currency, actual_cost_unknown_reason, idempotency_key, created_at_unix_micros
	) VALUES (?, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		report.ID, report.TaskID, report.RequirementRevision, report.AcceptedPlanRevision, report.PlanApprovalID,
		report.BaseRevision, report.DiffIdentity, report.RiskClassificationRevision, report.Risk,
		report.RiskExplanation, report.GraphRevisionID,
		boolInteger(metrics.ForecastDurationKnown), evidenceReportNullableDuration(metrics.ForecastDurationKnown, metrics.ForecastP50), evidenceReportNullableDuration(metrics.ForecastDurationKnown, metrics.ForecastP90), evidenceReportNullableString(metrics.ForecastDurationUnknownReason),
		boolInteger(metrics.ForecastTokensKnown), evidenceReportNullableUint64(metrics.ForecastTokensKnown, metrics.ForecastTokensP50), evidenceReportNullableUint64(metrics.ForecastTokensKnown, metrics.ForecastTokensP90), evidenceReportNullableString(metrics.ForecastTokensUnknownReason),
		boolInteger(metrics.ForecastCostKnown), nullableMoneyMinor(metrics.ForecastCostKnown, metrics.ForecastCostP50), nullableMoneyMinor(metrics.ForecastCostKnown, metrics.ForecastCostP90), nullableMoneyCurrency(metrics.ForecastCostKnown, metrics.ForecastCostP50), evidenceReportNullableString(metrics.ForecastCostUnknownReason),
		boolInteger(metrics.ActualDurationKnown), evidenceReportNullableDuration(metrics.ActualDurationKnown, metrics.ActualDuration), evidenceReportNullableString(metrics.ActualDurationUnknownReason),
		boolInteger(metrics.ActualTokens.Known), nullableToken(metrics.ActualTokens.Known, metrics.ActualTokens.Input), nullableToken(metrics.ActualTokens.Known, metrics.ActualTokens.CachedInput),
		nullableToken(metrics.ActualTokens.Known, metrics.ActualTokens.CacheWrite), nullableToken(metrics.ActualTokens.Known, metrics.ActualTokens.Output), nullableToken(metrics.ActualTokens.Known, metrics.ActualTokens.Reasoning), evidenceReportNullableString(metrics.ActualTokensUnknownReason),
		boolInteger(metrics.ActualCostKnown), nullableMoneyMinor(metrics.ActualCostKnown, metrics.ActualCost), nullableMoneyCurrency(metrics.ActualCostKnown, metrics.ActualCost),
		evidenceReportNullableString(metrics.ActualCostUnknownReason), report.IdempotencyKey, micros)
	if err != nil {
		return repositoryWriteError("insert final evidence report", err)
	}
	return nil
}

func insertEvidenceReportChildren(ctx context.Context, transaction *Transaction, report reportevidence.Report) error {
	for category, count := range report.Metrics.ActualTokens.ProviderSpecific {
		if _, err := transaction.sql.ExecContext(ctx, `INSERT INTO final_evidence_report_token_categories
			(report_id, category, token_count) VALUES (?, ?, ?)`, report.ID, category, count); err != nil {
			return repositoryWriteError("insert evidence report token category", err)
		}
	}
	for ordinal, file := range report.ChangedFiles {
		if _, err := transaction.sql.ExecContext(ctx, `INSERT INTO final_evidence_report_changed_files
			(report_id, ordinal, repository_relative_path, prior_repository_relative_path, status,
			 insertions, deletions, generated) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			report.ID, ordinal, file.Path, evidenceReportNullableString(file.PriorPath), file.Status, file.Insertions, file.Deletions, boolInteger(file.Generated)); err != nil {
			return repositoryWriteError("insert evidence report changed file", err)
		}
	}
	for ordinal, check := range report.Validations {
		if _, err := transaction.sql.ExecContext(ctx, `INSERT INTO final_evidence_report_validations
			(report_id, ordinal, check_id, validation_run_id, required, status, summary_redacted,
			 status_reason_redacted, command_digest, diff_identity) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			report.ID, ordinal, check.CheckID, evidenceReportNullableString(check.ValidationRunID), boolInteger(check.Required),
			check.Status, check.Summary, evidenceReportNullableString(check.StatusReason), evidenceReportNullableString(check.CommandDigest), check.DiffIdentity); err != nil {
			return repositoryWriteError("insert evidence report validation", err)
		}
	}
	for ordinal, approval := range report.Approvals {
		if _, err := transaction.sql.ExecContext(ctx, `INSERT INTO final_evidence_report_approvals
			(report_id, ordinal, approval_id, state, scope, authority_used) VALUES (?, ?, ?, ?, ?, ?)`,
			report.ID, ordinal, approval.ApprovalID, approval.State, approval.Scope, evidenceReportNullableString(approval.AuthorityUsed)); err != nil {
			return repositoryWriteError("insert evidence report approval", err)
		}
	}
	for ordinal, version := range report.Versions {
		if _, err := transaction.sql.ExecContext(ctx, `INSERT INTO final_evidence_report_versions
			(report_id, ordinal, version_kind, name, known, version, unknown_reason) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			report.ID, ordinal, version.Kind, version.Name, boolInteger(version.Known), evidenceReportNullableString(version.Version), evidenceReportNullableString(version.UnknownReason)); err != nil {
			return repositoryWriteError("insert evidence report version", err)
		}
	}
	if err := insertReportNarratives(ctx, transaction, report.ID, "assumption", report.Assumptions); err != nil {
		return err
	}
	if err := insertReportNarratives(ctx, transaction, report.ID, "limitation", report.Limitations); err != nil {
		return err
	}
	for ordinal, claim := range report.Claims {
		if _, err := transaction.sql.ExecContext(ctx, `INSERT INTO final_evidence_report_claims
			(report_id, ordinal, claim_id, statement_redacted, scope_redacted, boundary,
			 guarantee_level, guarantee_reason_redacted) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			report.ID, ordinal, claim.ID, claim.Statement, claim.Scope, claim.Boundary, claim.Guarantee, claim.GuaranteeReason); err != nil {
			return repositoryWriteError("insert evidence report claim", err)
		}
		for linkOrdinal, evidenceID := range claim.EvidenceIDs {
			if _, err := transaction.sql.ExecContext(ctx, `INSERT INTO final_evidence_report_claim_evidence
				(report_id, claim_id, ordinal, evidence_id) VALUES (?, ?, ?, ?)`, report.ID, claim.ID, linkOrdinal, evidenceID); err != nil {
				return repositoryWriteError("insert evidence report claim evidence", err)
			}
		}
		for linkOrdinal, runID := range claim.ValidationRunIDs {
			if _, err := transaction.sql.ExecContext(ctx, `INSERT INTO final_evidence_report_claim_validations
				(report_id, claim_id, ordinal, validation_run_id) VALUES (?, ?, ?, ?)`, report.ID, claim.ID, linkOrdinal, runID); err != nil {
				return repositoryWriteError("insert evidence report claim validation", err)
			}
		}
		for linkOrdinal, nodeID := range claim.GraphNodeIDs {
			if _, err := transaction.sql.ExecContext(ctx, `INSERT INTO final_evidence_report_claim_graph_nodes
				(report_id, claim_id, ordinal, graph_revision_id, node_id) VALUES (?, ?, ?, ?, ?)`,
				report.ID, claim.ID, linkOrdinal, report.GraphRevisionID, nodeID); err != nil {
				return repositoryWriteError("insert evidence report claim graph node", err)
			}
		}
		if err := insertClaimNarratives(ctx, transaction, report.ID, claim.ID, "assumption", claim.Assumptions); err != nil {
			return err
		}
		if err := insertClaimNarratives(ctx, transaction, report.ID, claim.ID, "limitation", claim.Limitations); err != nil {
			return err
		}
	}
	return nil
}

func insertReportNarratives(ctx context.Context, transaction *Transaction, reportID, kind string, values []string) error {
	for ordinal, value := range values {
		if _, err := transaction.sql.ExecContext(ctx, `INSERT INTO final_evidence_report_narratives
			(report_id, narrative_kind, ordinal, statement_redacted) VALUES (?, ?, ?, ?)`, reportID, kind, ordinal, value); err != nil {
			return repositoryWriteError("insert evidence report narrative", err)
		}
	}
	return nil
}

func insertClaimNarratives(ctx context.Context, transaction *Transaction, reportID, claimID, kind string, values []string) error {
	for ordinal, value := range values {
		if _, err := transaction.sql.ExecContext(ctx, `INSERT INTO final_evidence_report_claim_narratives
			(report_id, claim_id, narrative_kind, ordinal, statement_redacted) VALUES (?, ?, ?, ?, ?)`, reportID, claimID, kind, ordinal, value); err != nil {
			return repositoryWriteError("insert evidence report claim narrative", err)
		}
	}
	return nil
}

func (repositories *Repositories) GetFinalEvidenceReport(ctx context.Context, taskID domain.TaskID, reportID string) (reportevidence.Report, error) {
	if taskID.IsZero() || reportID == "" {
		return reportevidence.Report{}, errors.New("final evidence report task and report identities are required")
	}
	report, err := repositories.loadEvidenceReportRoot(ctx, taskID, reportID)
	if err != nil {
		return reportevidence.Report{}, err
	}
	if err := repositories.loadEvidenceReportChildren(ctx, &report); err != nil {
		return reportevidence.Report{}, err
	}
	if err := report.Validate(); err != nil {
		return reportevidence.Report{}, typedError(ErrCorrupt, "validate stored final evidence report", err)
	}
	if err := validateEvidenceReportValidationProvenance(ctx, repositories.database.sql, report); err != nil {
		return reportevidence.Report{}, typedError(ErrCorrupt, "validate stored final evidence report provenance", err)
	}
	return report, nil
}

type evidenceReportRowQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type evidenceReportValidationProvenance struct {
	passedExactNonInvalidated bool
}

// validateEvidenceReportValidationProvenance binds every cited report check
// to the immutable migration-22 intent/result pair. A report may describe a
// skipped, waived, or unavailable check without a run identity, but it cannot
// manufacture an executed validation identity or strengthen stale evidence.
func validateEvidenceReportValidationProvenance(
	ctx context.Context,
	queries evidenceReportRowQuerier,
	report reportevidence.Report,
) error {
	provenance := make(map[string]evidenceReportValidationProvenance, len(report.Validations))
	for _, check := range report.Validations {
		if check.ValidationRunID == "" {
			continue
		}
		validationID, err := domain.ParseValidationID(check.ValidationRunID)
		if err != nil {
			return errors.New("validation run identity is not canonical")
		}
		var taskID domain.TaskID
		var checkID string
		var required int
		var intentDiff, commandFingerprint string
		var resultState domain.ValidationState
		var observedDiff, invalidatedPrevious, invalidatedCurrent string
		err = queries.QueryRowContext(ctx, `SELECT intent.task_id, intent.check_id, intent.required,
			intent.diff_identity, intent.command_fingerprint, result.state,
			result.observed_diff_identity,
			COALESCE((SELECT invalidation.previous_diff_identity
				FROM validation_run_invalidations AS invalidation
				WHERE invalidation.validation_run_id = intent.id
				  AND invalidation.created_at_unix_micros <= ?
				ORDER BY invalidation.created_at_unix_micros DESC, invalidation.id DESC
				LIMIT 1), ''),
			COALESCE((SELECT invalidation.current_diff_identity
				FROM validation_run_invalidations AS invalidation
				WHERE invalidation.validation_run_id = intent.id
				  AND invalidation.created_at_unix_micros <= ?
				ORDER BY invalidation.created_at_unix_micros DESC, invalidation.id DESC
				LIMIT 1), '')
			FROM validation_run_intents AS intent
			JOIN validation_run_results AS result ON result.validation_run_id = intent.id
			WHERE intent.id = ?`,
			report.CreatedAt.UnixMicro(), report.CreatedAt.UnixMicro(), validationID,
		).Scan(&taskID, &checkID, &required, &intentDiff, &commandFingerprint,
			&resultState, &observedDiff, &invalidatedPrevious, &invalidatedCurrent)
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("cited validation run has no authoritative intent and result")
		}
		if err != nil {
			return classify("read final evidence report validation provenance", err)
		}
		if taskID != report.TaskID || checkID != check.CheckID || (required == 1) != check.Required ||
			check.CommandDigest != commandFingerprint || observedDiff != intentDiff {
			return errors.New("cited validation run does not match the report task, check, command, or result binding")
		}

		effectiveStatus := reportevidence.ValidationStatus(resultState)
		passedExact := false
		if invalidatedCurrent != "" {
			effectiveStatus = reportevidence.ValidationInvalidated
			if invalidatedPrevious != intentDiff || invalidatedCurrent != report.DiffIdentity {
				return errors.New("cited validation invalidation does not bind the report diff")
			}
		} else {
			if intentDiff != report.DiffIdentity || observedDiff != report.DiffIdentity {
				return errors.New("cited validation run is not exact-bound to the report diff")
			}
			passedExact = resultState == domain.ValidationStatePassed
		}
		if check.DiffIdentity != report.DiffIdentity || check.Status != effectiveStatus {
			return errors.New("cited validation run status or diff binding does not match the report")
		}
		provenance[check.ValidationRunID] = evidenceReportValidationProvenance{
			passedExactNonInvalidated: passedExact,
		}
	}

	for _, claim := range report.Claims {
		if !strongEvidenceReportGuarantee(claim.Guarantee) {
			continue
		}
		for _, runID := range claim.ValidationRunIDs {
			binding, ok := provenance[runID]
			if !ok || !binding.passedExactNonInvalidated {
				return errors.New("strong claim cites validation that is not passed, current, and exact-bound")
			}
		}
	}
	return nil
}

func strongEvidenceReportGuarantee(level domain.AssuranceLevel) bool {
	switch level {
	case domain.AssuranceLevelFullyEvaluated,
		domain.AssuranceLevelModelVerified,
		domain.AssuranceLevelContractChecked:
		return true
	default:
		return false
	}
}

func (repositories *Repositories) loadEvidenceReportRoot(ctx context.Context, taskID domain.TaskID, reportID string) (reportevidence.Report, error) {
	var report reportevidence.Report
	var createdMicros int64
	var fdKnown, ftKnown, fcKnown, adKnown, atKnown, acKnown int
	var fp50, fp90, ftp50, ftp90, fcp50, fcp90, ad, ai, aci, acw, ao, ar, ac sql.NullInt64
	var fcurrency, acurrency sql.NullString
	var fdUnknown, ftUnknown, fcUnknown, adUnknown, atUnknown, acUnknown sql.NullString
	err := repositories.database.sql.QueryRowContext(ctx, `SELECT id, task_id, requirement_revision,
		accepted_plan_revision, plan_approval_id, base_revision, diff_identity,
		risk_classification_revision, risk_level, risk_explanation, graph_revision_id,
		forecast_duration_known, forecast_p50_nanos, forecast_p90_nanos, forecast_duration_unknown_reason,
		forecast_tokens_known, forecast_tokens_p50, forecast_tokens_p90, forecast_tokens_unknown_reason,
		forecast_cost_known, forecast_cost_p50_minor, forecast_cost_p90_minor, forecast_currency, forecast_cost_unknown_reason,
		actual_duration_known, actual_duration_nanos, actual_duration_unknown_reason, actual_tokens_known, actual_input_tokens,
		actual_cached_input_tokens, actual_cache_write_tokens, actual_output_tokens, actual_reasoning_tokens,
		actual_tokens_unknown_reason, actual_cost_known, actual_cost_minor, actual_currency, actual_cost_unknown_reason,
		idempotency_key, created_at_unix_micros
		FROM final_evidence_reports AS report
		JOIN final_evidence_report_seals AS seal ON seal.report_id = report.id
		WHERE report.task_id = ? AND report.id = ?`, taskID, reportID).Scan(
		&report.ID, &report.TaskID, &report.RequirementRevision, &report.AcceptedPlanRevision,
		&report.PlanApprovalID, &report.BaseRevision, &report.DiffIdentity,
		&report.RiskClassificationRevision, &report.Risk, &report.RiskExplanation, &report.GraphRevisionID,
		&fdKnown, &fp50, &fp90, &fdUnknown, &ftKnown, &ftp50, &ftp90, &ftUnknown, &fcKnown, &fcp50, &fcp90, &fcurrency, &fcUnknown,
		&adKnown, &ad, &adUnknown, &atKnown, &ai, &aci, &acw, &ao, &ar, &atUnknown, &acKnown, &ac, &acurrency, &acUnknown,
		&report.IdempotencyKey, &createdMicros)
	if errors.Is(err, sql.ErrNoRows) {
		return reportevidence.Report{}, typedError(ErrNotFound, "get final evidence report", err)
	}
	if err != nil {
		return reportevidence.Report{}, classify("get final evidence report", err)
	}
	report.CreatedAt = repositoryTime(createdMicros)
	metrics := &report.Metrics
	metrics.ForecastDurationKnown = fdKnown == 1
	metrics.ForecastP50, metrics.ForecastP90 = evidenceReportDuration(fp50), evidenceReportDuration(fp90)
	metrics.ForecastDurationUnknownReason = fdUnknown.String
	metrics.ForecastTokensKnown = ftKnown == 1
	metrics.ForecastTokensP50, metrics.ForecastTokensP90 = uint64Null(ftp50), uint64Null(ftp90)
	metrics.ForecastTokensUnknownReason = ftUnknown.String
	metrics.ForecastCostKnown = fcKnown == 1
	metrics.ForecastCostUnknownReason = fcUnknown.String
	if metrics.ForecastCostKnown {
		currency, parseErr := domain.ParseCurrencyCode(fcurrency.String)
		if parseErr != nil {
			return reportevidence.Report{}, typedError(ErrCorrupt, "parse evidence report forecast currency", parseErr)
		}
		metrics.ForecastCostP50 = domain.Money{Currency: currency, MinorUnits: fcp50.Int64}
		metrics.ForecastCostP90 = domain.Money{Currency: currency, MinorUnits: fcp90.Int64}
	}
	metrics.ActualDurationKnown, metrics.ActualDuration = adKnown == 1, evidenceReportDuration(ad)
	metrics.ActualDurationUnknownReason = adUnknown.String
	metrics.ActualTokens = domain.TokenUsage{Known: atKnown == 1, Input: domain.TokenCount(uint64Null(ai)), CachedInput: domain.TokenCount(uint64Null(aci)), CacheWrite: domain.TokenCount(uint64Null(acw)), Output: domain.TokenCount(uint64Null(ao)), Reasoning: domain.TokenCount(uint64Null(ar))}
	metrics.ActualTokensUnknownReason = atUnknown.String
	metrics.ActualCostKnown = acKnown == 1
	metrics.ActualCostUnknownReason = acUnknown.String
	if metrics.ActualCostKnown {
		currency, parseErr := domain.ParseCurrencyCode(acurrency.String)
		if parseErr != nil {
			return reportevidence.Report{}, typedError(ErrCorrupt, "parse evidence report actual currency", parseErr)
		}
		metrics.ActualCost = domain.Money{Currency: currency, MinorUnits: ac.Int64}
	}
	return report, nil
}

func (repositories *Repositories) loadEvidenceReportChildren(ctx context.Context, report *reportevidence.Report) error {
	if err := repositories.loadReportTokens(ctx, report); err != nil {
		return err
	}
	if err := repositories.loadReportChangedFiles(ctx, report); err != nil {
		return err
	}
	if err := repositories.loadReportValidations(ctx, report); err != nil {
		return err
	}
	if err := repositories.loadReportApprovals(ctx, report); err != nil {
		return err
	}
	if err := repositories.loadReportVersions(ctx, report); err != nil {
		return err
	}
	if err := repositories.loadReportNarratives(ctx, report); err != nil {
		return err
	}
	return repositories.loadReportClaims(ctx, report)
}

func (repositories *Repositories) loadReportTokens(ctx context.Context, report *reportevidence.Report) error {
	rows, err := repositories.database.sql.QueryContext(ctx, `SELECT category, token_count FROM final_evidence_report_token_categories WHERE report_id = ? ORDER BY category`, report.ID)
	if err != nil {
		return classify("load evidence report token categories", err)
	}
	defer rows.Close()
	for rows.Next() {
		var category string
		var count domain.TokenCount
		if err := rows.Scan(&category, &count); err != nil {
			return classify("scan evidence report token category", err)
		}
		if report.Metrics.ActualTokens.ProviderSpecific == nil {
			report.Metrics.ActualTokens.ProviderSpecific = map[string]domain.TokenCount{}
		}
		report.Metrics.ActualTokens.ProviderSpecific[category] = count
	}
	return classify("iterate evidence report token categories", rows.Err())
}

func (repositories *Repositories) loadReportChangedFiles(ctx context.Context, report *reportevidence.Report) error {
	rows, err := repositories.database.sql.QueryContext(ctx, `SELECT repository_relative_path, prior_repository_relative_path, status, insertions, deletions, generated FROM final_evidence_report_changed_files WHERE report_id = ? ORDER BY ordinal`, report.ID)
	if err != nil {
		return classify("load evidence report changed files", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item reportevidence.ChangedFile
		var prior sql.NullString
		var generated int
		if err := rows.Scan(&item.Path, &prior, &item.Status, &item.Insertions, &item.Deletions, &generated); err != nil {
			return classify("scan evidence report changed file", err)
		}
		item.PriorPath, item.Generated = prior.String, generated == 1
		report.ChangedFiles = append(report.ChangedFiles, item)
	}
	return classify("iterate evidence report changed files", rows.Err())
}

func (repositories *Repositories) loadReportValidations(ctx context.Context, report *reportevidence.Report) error {
	rows, err := repositories.database.sql.QueryContext(ctx, `SELECT check_id, validation_run_id, required, status, summary_redacted, status_reason_redacted, command_digest, diff_identity FROM final_evidence_report_validations WHERE report_id = ? ORDER BY ordinal`, report.ID)
	if err != nil {
		return classify("load evidence report validations", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item reportevidence.ValidationCheck
		var run, reason, digest sql.NullString
		var required int
		if err := rows.Scan(&item.CheckID, &run, &required, &item.Status, &item.Summary, &reason, &digest, &item.DiffIdentity); err != nil {
			return classify("scan evidence report validation", err)
		}
		item.ValidationRunID, item.Required, item.StatusReason, item.CommandDigest = run.String, required == 1, reason.String, digest.String
		report.Validations = append(report.Validations, item)
	}
	return classify("iterate evidence report validations", rows.Err())
}

func (repositories *Repositories) loadReportApprovals(ctx context.Context, report *reportevidence.Report) error {
	rows, err := repositories.database.sql.QueryContext(ctx, `SELECT approval_id, state, scope, authority_used FROM final_evidence_report_approvals WHERE report_id = ? ORDER BY ordinal`, report.ID)
	if err != nil {
		return classify("load evidence report approvals", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item reportevidence.ApprovalUse
		var authority sql.NullString
		if err := rows.Scan(&item.ApprovalID, &item.State, &item.Scope, &authority); err != nil {
			return classify("scan evidence report approval", err)
		}
		item.AuthorityUsed = authority.String
		report.Approvals = append(report.Approvals, item)
	}
	return classify("iterate evidence report approvals", rows.Err())
}

func (repositories *Repositories) loadReportVersions(ctx context.Context, report *reportevidence.Report) error {
	rows, err := repositories.database.sql.QueryContext(ctx, `SELECT version_kind, name, known, version, unknown_reason FROM final_evidence_report_versions WHERE report_id = ? ORDER BY ordinal`, report.ID)
	if err != nil {
		return classify("load evidence report versions", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item reportevidence.VersionBinding
		var known int
		var version, reason sql.NullString
		if err := rows.Scan(&item.Kind, &item.Name, &known, &version, &reason); err != nil {
			return classify("scan evidence report version", err)
		}
		item.Known, item.Version, item.UnknownReason = known == 1, version.String, reason.String
		report.Versions = append(report.Versions, item)
	}
	return classify("iterate evidence report versions", rows.Err())
}

func (repositories *Repositories) loadReportNarratives(ctx context.Context, report *reportevidence.Report) error {
	rows, err := repositories.database.sql.QueryContext(ctx, `SELECT narrative_kind, statement_redacted FROM final_evidence_report_narratives WHERE report_id = ? ORDER BY narrative_kind, ordinal`, report.ID)
	if err != nil {
		return classify("load evidence report narratives", err)
	}
	defer rows.Close()
	for rows.Next() {
		var kind, value string
		if err := rows.Scan(&kind, &value); err != nil {
			return classify("scan evidence report narrative", err)
		}
		if kind == "assumption" {
			report.Assumptions = append(report.Assumptions, value)
		} else {
			report.Limitations = append(report.Limitations, value)
		}
	}
	return classify("iterate evidence report narratives", rows.Err())
}

func (repositories *Repositories) loadReportClaims(ctx context.Context, report *reportevidence.Report) error {
	rows, err := repositories.database.sql.QueryContext(ctx, `SELECT claim_id, statement_redacted, scope_redacted, boundary, guarantee_level, guarantee_reason_redacted FROM final_evidence_report_claims WHERE report_id = ? ORDER BY ordinal`, report.ID)
	if err != nil {
		return classify("load evidence report claims", err)
	}
	defer rows.Close()
	for rows.Next() {
		var claim reportevidence.Claim
		if err := rows.Scan(&claim.ID, &claim.Statement, &claim.Scope, &claim.Boundary, &claim.Guarantee, &claim.GuaranteeReason); err != nil {
			return classify("scan evidence report claim", err)
		}
		report.Claims = append(report.Claims, claim)
	}
	if err := rows.Err(); err != nil {
		return classify("iterate evidence report claims", err)
	}
	for index := range report.Claims {
		if err := repositories.loadClaimLinks(ctx, report, &report.Claims[index]); err != nil {
			return err
		}
	}
	return nil
}

func (repositories *Repositories) loadClaimLinks(ctx context.Context, report *reportevidence.Report, claim *reportevidence.Claim) error {
	if err := scanStringLinks(ctx, repositories.database.sql, `SELECT evidence_id FROM final_evidence_report_claim_evidence WHERE report_id = ? AND claim_id = ? ORDER BY ordinal`, report.ID, claim.ID, func(value string) error {
		id, err := domain.ParseEvidenceID(value)
		if err == nil {
			claim.EvidenceIDs = append(claim.EvidenceIDs, id)
		}
		return err
	}); err != nil {
		return err
	}
	if err := scanStringLinks(ctx, repositories.database.sql, `SELECT validation_run_id FROM final_evidence_report_claim_validations WHERE report_id = ? AND claim_id = ? ORDER BY ordinal`, report.ID, claim.ID, func(value string) error { claim.ValidationRunIDs = append(claim.ValidationRunIDs, value); return nil }); err != nil {
		return err
	}
	if err := scanStringLinks(ctx, repositories.database.sql, `SELECT node_id FROM final_evidence_report_claim_graph_nodes WHERE report_id = ? AND claim_id = ? ORDER BY ordinal`, report.ID, claim.ID, func(value string) error {
		id, err := domain.ParseNodeID(value)
		if err == nil {
			claim.GraphNodeIDs = append(claim.GraphNodeIDs, id)
		}
		return err
	}); err != nil {
		return err
	}
	rows, err := repositories.database.sql.QueryContext(ctx, `SELECT narrative_kind, statement_redacted FROM final_evidence_report_claim_narratives WHERE report_id = ? AND claim_id = ? ORDER BY narrative_kind, ordinal`, report.ID, claim.ID)
	if err != nil {
		return classify("load evidence report claim narratives", err)
	}
	defer rows.Close()
	for rows.Next() {
		var kind, value string
		if err := rows.Scan(&kind, &value); err != nil {
			return classify("scan evidence report claim narrative", err)
		}
		if kind == "assumption" {
			claim.Assumptions = append(claim.Assumptions, value)
		} else {
			claim.Limitations = append(claim.Limitations, value)
		}
	}
	return classify("iterate evidence report claim narratives", rows.Err())
}

func scanStringLinks(ctx context.Context, database *sql.DB, query, reportID, claimID string, appendValue func(string) error) error {
	rows, err := database.QueryContext(ctx, query, reportID, claimID)
	if err != nil {
		return classify("load evidence report claim links", err)
	}
	defer rows.Close()
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return classify("scan evidence report claim link", err)
		}
		if err := appendValue(value); err != nil {
			return typedError(ErrCorrupt, "parse evidence report claim link", err)
		}
	}
	return classify("iterate evidence report claim links", rows.Err())
}

func evidenceReportNullableDuration(known bool, value time.Duration) any {
	if !known {
		return nil
	}
	return int64(value)
}
func evidenceReportNullableUint64(known bool, value uint64) any {
	if !known {
		return nil
	}
	return value
}
func nullableToken(known bool, value domain.TokenCount) any {
	if !known {
		return nil
	}
	return value
}
func nullableMoneyMinor(known bool, value domain.Money) any {
	if !known {
		return nil
	}
	return value.MinorUnits
}
func nullableMoneyCurrency(known bool, value domain.Money) any {
	if !known {
		return nil
	}
	return value.Currency
}
func evidenceReportNullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
func evidenceReportDuration(value sql.NullInt64) time.Duration {
	if !value.Valid {
		return 0
	}
	return time.Duration(value.Int64)
}
func uint64Null(value sql.NullInt64) uint64 {
	if !value.Valid {
		return 0
	}
	return uint64(value.Int64)
}

func equivalentEvidenceReport(input, stored reportevidence.Report) bool {
	input.CreatedAt = stored.CreatedAt
	return reflect.DeepEqual(canonicalEvidenceReport(input), canonicalEvidenceReport(stored))
}

func canonicalEvidenceReport(report reportevidence.Report) reportevidence.Report {
	report = report.Clone()
	if len(report.Metrics.ActualTokens.ProviderSpecific) == 0 {
		report.Metrics.ActualTokens.ProviderSpecific = nil
	}
	if len(report.ChangedFiles) == 0 {
		report.ChangedFiles = nil
	}
	if len(report.Assumptions) == 0 {
		report.Assumptions = nil
	}
	if len(report.Limitations) == 0 {
		report.Limitations = nil
	}
	for index := range report.Claims {
		claim := &report.Claims[index]
		if len(claim.EvidenceIDs) == 0 {
			claim.EvidenceIDs = nil
		}
		if len(claim.ValidationRunIDs) == 0 {
			claim.ValidationRunIDs = nil
		}
		if len(claim.GraphNodeIDs) == 0 {
			claim.GraphNodeIDs = nil
		}
		if len(claim.Assumptions) == 0 {
			claim.Assumptions = nil
		}
		if len(claim.Limitations) == 0 {
			claim.Limitations = nil
		}
	}
	return report
}
