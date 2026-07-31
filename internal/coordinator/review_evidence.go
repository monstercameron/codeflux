package coordinator

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"codeflux.dev/codeflux/internal/domain"
	reportevidence "codeflux.dev/codeflux/internal/evidence"
	"codeflux.dev/codeflux/internal/gitwork"
	taskgraph "codeflux.dev/codeflux/internal/graph"
	"codeflux.dev/codeflux/internal/redact"
	"codeflux.dev/codeflux/internal/review"
	"codeflux.dev/codeflux/internal/storage"
)

type ReviewEvidenceStore interface {
	GetLatestFinalEvidenceReport(context.Context, domain.TaskID) (reportevidence.Report, error)
	GetFinalEvidenceReport(context.Context, domain.TaskID, string) (reportevidence.Report, error)
	GetPlanRevision(context.Context, domain.TaskID, uint64) (storage.PlanRevision, error)
	ListReviewStepAttributions(context.Context, domain.TaskID, uint64, domain.GraphRevisionID) ([]storage.ReviewStepAttribution, error)
}

type ReviewDiffSource interface {
	GetTaskDiff(context.Context, gitwork.TaskDiffQuery) (gitwork.TaskDiff, error)
}

type ReviewEvidenceRedactor interface {
	Redact(redact.Boundary, string) (redact.Result, error)
}

// ReviewEvidenceService composes the sealed report with a fresh, read-only Git
// observation. A changed diff is never presented as the immutable report diff.
type ReviewEvidenceService struct {
	store    ReviewEvidenceStore
	diffs    ReviewDiffSource
	redactor ReviewEvidenceRedactor
}

func NewReviewEvidenceService(
	store ReviewEvidenceStore,
	diffs ReviewDiffSource,
	redactor ReviewEvidenceRedactor,
) (*ReviewEvidenceService, error) {
	if store == nil || diffs == nil || redactor == nil {
		return nil, errors.New("review evidence store, diff source, and redactor are required")
	}
	return &ReviewEvidenceService{store: store, diffs: diffs, redactor: redactor}, nil
}

func (service *ReviewEvidenceService) GetEvidenceReport(
	ctx context.Context,
	query review.EvidenceQuery,
) (review.EvidenceBundle, error) {
	if query.TaskID.IsZero() || (query.ReportID != "" && !lowerReviewDigest(query.ReportID)) {
		return review.EvidenceBundle{}, fmt.Errorf("%w: query identity is invalid", review.ErrEvidenceUnavailable)
	}
	var report reportevidence.Report
	var err error
	if query.ReportID == "" {
		report, err = service.store.GetLatestFinalEvidenceReport(ctx, query.TaskID)
	} else {
		report, err = service.store.GetFinalEvidenceReport(ctx, query.TaskID, query.ReportID)
	}
	if err != nil {
		return review.EvidenceBundle{}, fmt.Errorf("%w: %v", review.ErrEvidenceUnavailable, err)
	}
	if err := report.Validate(); err != nil {
		return review.EvidenceBundle{}, fmt.Errorf("%w: invalid sealed report", review.ErrEvidenceUnavailable)
	}
	bundle := review.EvidenceBundle{Report: report.Clone()}
	if !query.IncludeDiff {
		return bundle, nil
	}
	plan, err := service.store.GetPlanRevision(ctx, query.TaskID, report.AcceptedPlanRevision)
	if err != nil {
		return review.EvidenceBundle{}, fmt.Errorf("%w: accepted plan unavailable", review.ErrEvidenceUnavailable)
	}
	stepAttributions, err := service.store.ListReviewStepAttributions(
		ctx, query.TaskID, report.AcceptedPlanRevision, report.GraphRevisionID,
	)
	if err != nil {
		return review.EvidenceBundle{}, fmt.Errorf("%w: review provenance unavailable", review.ErrEvidenceUnavailable)
	}
	approvedPaths, fileAttributions, diffAttributions, err := buildReviewAttributions(plan, stepAttributions)
	if err != nil {
		return review.EvidenceBundle{}, err
	}
	diff, err := service.diffs.GetTaskDiff(ctx, gitwork.TaskDiffQuery{
		TaskID: query.TaskID, ApprovedPaths: approvedPaths, AttributionByPath: diffAttributions,
	})
	if err != nil {
		return review.EvidenceBundle{}, fmt.Errorf("%w: live diff unavailable", review.ErrEvidenceUnavailable)
	}
	bundle.DiffMatchesReport = diff.Identity == report.DiffIdentity
	if !bundle.DiffMatchesReport {
		return bundle, nil
	}
	if len(diff.UnifiedDiff) > review.MaximumDiffBytes || len(diff.Files) > review.MaximumChangedFiles {
		return review.EvidenceBundle{}, review.ErrEvidenceTooLarge
	}
	redacted, err := service.redactor.Redact(redact.BoundaryUIDelivery, diff.UnifiedDiff)
	if err != nil {
		return review.EvidenceBundle{}, fmt.Errorf("%w: diff redaction failed", review.ErrEvidenceUnavailable)
	}
	diff.UnifiedDiff = redacted.Text
	bundle.Diff = reviewDiffValue(diff)
	bundle.FileAttributions = fileAttributions
	return bundle, nil
}

func reviewDiffValue(value gitwork.TaskDiff) *review.Diff {
	result := &review.Diff{
		Identity: value.Identity, TaskID: value.TaskID, BaseRevision: value.BaseRevision,
		HeadRevision: value.HeadRevision, UnifiedDiff: value.UnifiedDiff,
		Files: make([]review.DiffFile, 0, len(value.Files)),
	}
	for _, file := range value.Files {
		result.Files = append(result.Files, review.DiffFile{
			Path: file.Path, PreviousPath: file.PreviousPath,
			AddedLines: file.AddedLines, DeletedLines: file.DeletedLines,
			Category: file.Category, Binary: file.Binary,
			FormattingChurn:      file.FormattingChurn,
			OutsideApprovedScope: file.OutsideApprovedScope,
		})
	}
	return result
}

func buildReviewAttributions(
	plan storage.PlanRevision,
	stepAttributions []storage.ReviewStepAttribution,
) ([]string, map[string]review.FileAttribution, map[string]gitwork.DiffAttribution, error) {
	byStep := make(map[string]storage.ReviewStepAttribution, len(stepAttributions))
	for _, attribution := range stepAttributions {
		byStep[attribution.StepID] = attribution
	}
	approved := slices.Clone(plan.Plan.ExpectedFiles)
	files := map[string]review.FileAttribution{}
	diffLinks := map[string]gitwork.DiffAttribution{}
	for _, step := range plan.Plan.Steps {
		approved = append(approved, step.ExpectedFiles...)
		provenance := byStep[step.ID]
		for _, expectedPath := range step.ExpectedFiles {
			value := files[expectedPath]
			value.PlanSteps = append(value.PlanSteps, taskgraph.PlanStepLink{
				PlanRevision: plan.Revision, StepID: step.ID,
			})
			value.EventIDs = appendUniqueEvents(value.EventIDs, provenance.EventIDs...)
			value.Validations = appendUniqueValidations(value.Validations, reviewValidations(provenance.Validations)...)
			if len(value.PlanSteps) > review.MaximumPlanLinksPerFile ||
				len(value.EventIDs) > review.MaximumLinksPerFile ||
				len(value.Validations) > review.MaximumLinksPerFile {
				return nil, nil, nil, review.ErrEvidenceTooLarge
			}
			files[expectedPath] = value
			diffLinks[expectedPath] = gitwork.DiffAttribution{
				EventIDs:      slices.Clone(value.EventIDs),
				ValidationIDs: reviewValidationIDs(value.Validations),
			}
		}
	}
	slices.Sort(approved)
	approved = slices.Compact(approved)
	return approved, files, diffLinks, nil
}

func reviewValidationIDs(values []review.ValidationAttribution) []domain.ValidationID {
	result := make([]domain.ValidationID, 0, len(values))
	for _, value := range values {
		result = append(result, value.ID)
	}
	return result
}

func appendUniqueEvents(values []domain.EventID, additions ...domain.EventID) []domain.EventID {
	for _, addition := range additions {
		if !slices.Contains(values, addition) {
			values = append(values, addition)
		}
	}
	return values
}

func appendUniqueValidations(values []review.ValidationAttribution, additions ...review.ValidationAttribution) []review.ValidationAttribution {
	for _, addition := range additions {
		if !slices.ContainsFunc(values, func(value review.ValidationAttribution) bool { return value.ID == addition.ID }) {
			values = append(values, addition)
		}
	}
	return values
}

func reviewValidations(values []storage.ReviewValidationAttribution) []review.ValidationAttribution {
	result := make([]review.ValidationAttribution, 0, len(values))
	for _, value := range values {
		result = append(result, review.ValidationAttribution{
			ID: value.ID, State: value.State, Label: value.Label, Summary: value.Summary,
		})
	}
	return result
}

func lowerReviewDigest(value string) bool {
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
