//go:build js && wasm

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	reportevidence "codeflux.dev/codeflux/internal/evidence"
	taskgraph "codeflux.dev/codeflux/internal/graph"
	"codeflux.dev/codeflux/web/frontend/diffreview"
	"google.golang.org/grpc"
)

var (
	errReviewResourceScopeUnavailable  = errors.New("authoritative review scope is unavailable")
	errReviewResourceBridgeUnavailable = errors.New("authoritative review bridge is unavailable")
)

type reviewResource struct {
	Report            reportevidence.Report
	Diff              *reviewDiffResource
	DiffMatchesReport bool
}

type reviewDiffResource struct {
	Identity     string
	BaseRevision string
	HeadRevision string
	Files        []diffreview.ChangedFile
}

type reviewResourceClient interface {
	GetEvidenceReport(context.Context, *codefluxv1.GetEvidenceReportRequest, ...grpc.CallOption) (*codefluxv1.GetEvidenceReportResponse, error)
	OpenInEditor(context.Context, *codefluxv1.OpenInEditorRequest, ...grpc.CallOption) (*codefluxv1.OpenInEditorResponse, error)
}

type reviewResourceLease struct {
	client reviewResourceClient
	close  func() error
}

type reviewResourceClientOpener func(context.Context) (reviewResourceLease, error)

func loadReviewResource(
	ctx context.Context,
	opener reviewResourceClientOpener,
	taskID domain.TaskID,
) (reviewResource, error) {
	if opener == nil || taskID.IsZero() {
		return reviewResource{}, errReviewResourceScopeUnavailable
	}
	lease, err := opener(ctx)
	if err != nil {
		return reviewResource{}, err
	}
	if lease.client == nil || lease.close == nil {
		return reviewResource{}, errReviewResourceBridgeUnavailable
	}
	defer lease.close()
	response, err := lease.client.GetEvidenceReport(ctx, &codefluxv1.GetEvidenceReportRequest{
		TaskId: taskIdentity(taskID), IncludeDiff: true,
	})
	if err != nil {
		return reviewResource{}, err
	}
	return decodeReviewResource(response, taskID)
}

func decodeReviewResource(response *codefluxv1.GetEvidenceReportResponse, taskID domain.TaskID) (reviewResource, error) {
	if response == nil || taskID.IsZero() || len(response.GetStructuredReportJson()) == 0 {
		return reviewResource{}, errReviewResourceMalformed
	}
	var report reportevidence.Report
	if err := json.Unmarshal(response.GetStructuredReportJson(), &report); err != nil {
		return reviewResource{}, fmt.Errorf("%w: decode report: %v", errReviewResourceMalformed, err)
	}
	if err := report.Validate(); err != nil || report.TaskID != taskID {
		return reviewResource{}, fmt.Errorf("%w: invalid report binding", errReviewResourceMalformed)
	}
	result := reviewResource{Report: report, DiffMatchesReport: response.GetDiffMatchesReport()}
	if response.GetDiff() == nil {
		return result, nil
	}
	if !result.DiffMatchesReport {
		return reviewResource{}, fmt.Errorf("%w: a stale response must not include diff content", errReviewResourceMalformed)
	}
	diff, err := decodeReviewDiff(response.GetDiff(), taskID)
	if err != nil {
		return reviewResource{}, err
	}
	if diff.Identity != report.DiffIdentity {
		return reviewResource{}, fmt.Errorf("%w: diff identity does not match report", errReviewResourceMalformed)
	}
	result.Diff = &diff
	return result, nil
}

func decodeReviewDiff(raw *codefluxv1.ReviewDiffView, taskID domain.TaskID) (reviewDiffResource, error) {
	boundTask, err := decodeTaskIdentity(raw.GetTaskId())
	if err != nil || boundTask != taskID || strings.TrimSpace(raw.GetUnifiedDiff().GetValue()) == "" {
		return reviewDiffResource{}, errReviewResourceMalformed
	}
	files := make([]diffreview.ChangedFile, 0, len(raw.GetFiles()))
	links := make(map[string]reviewFileLinks, len(raw.GetFiles()))
	for _, view := range raw.GetFiles() {
		file, attribution, decodeErr := decodeReviewFile(view)
		if decodeErr != nil {
			return reviewDiffResource{}, decodeErr
		}
		files = append(files, file)
		links[file.Path.String()] = attribution
	}
	hunks, err := parseReviewUnifiedDiff(raw.GetUnifiedDiff().GetValue(), files, links)
	if err != nil {
		return reviewDiffResource{}, err
	}
	for index := range files {
		files[index].Hunks = hunks[files[index].Path.String()]
		if len(files[index].Hunks) == 0 && !files[index].Binary {
			files[index].DiffUnavailableReason = "No textual hunk was present in the bounded unified diff."
		}
	}
	result := reviewDiffResource{
		Identity: raw.GetDiffIdentity(), BaseRevision: raw.GetBaseRevision(),
		HeadRevision: raw.GetHeadRevision(), Files: files,
	}
	props := diffreview.Props{
		DiffIdentity: result.Identity, BaseRevision: result.BaseRevision, HeadRevision: result.HeadRevision,
		Files: files, CategoryFilters: activeReviewCategoryFilters(),
		OnSelectFile: func(string) {}, OnToggleCategory: func(diffreview.FileCategory, bool) {},
		OnToggleWhitespace: func(bool) {},
	}
	if err := props.Validate(); err != nil {
		return reviewDiffResource{}, fmt.Errorf("%w: %v", errReviewResourceMalformed, err)
	}
	return result, nil
}

func decodeReviewFile(raw *codefluxv1.ReviewDiffFileView) (diffreview.ChangedFile, reviewFileLinks, error) {
	if raw == nil || raw.GetPath() == nil {
		return diffreview.ChangedFile{}, reviewFileLinks{}, errReviewResourceMalformed
	}
	path, err := diffreview.NewFilePath(raw.GetPath().GetWorkspaceRelativeSlashPath())
	if err != nil {
		return diffreview.ChangedFile{}, reviewFileLinks{}, err
	}
	previous := diffreview.FilePath{}
	if raw.GetPreviousPath() != nil && raw.GetPreviousPath().GetWorkspaceRelativeSlashPath() != "" {
		previous, err = diffreview.NewFilePath(raw.GetPreviousPath().GetWorkspaceRelativeSlashPath())
		if err != nil {
			return diffreview.ChangedFile{}, reviewFileLinks{}, err
		}
	}
	file := diffreview.ChangedFile{
		Path: path, PreviousPath: previous, Status: diffreview.FileChangeStatus(raw.GetStatus()),
		Category: diffreview.FileCategory(raw.GetCategory()), Binary: raw.GetBinary(),
		FormattingChurn: raw.GetFormattingChurn(),
		Scope:           diffreview.ScopeAssessment{Known: raw.GetScopeKnown(), InScope: raw.GetInScope()},
	}
	if raw.GetLineCountsKnown() {
		file.Lines = diffreview.LineCounts{Known: true, Added: int(raw.GetAddedLines()), Deleted: int(raw.GetDeletedLines())}
	} else {
		file.Lines.UnknownReason = raw.GetLineCountsUnknownReason().GetValue()
	}
	if !raw.GetScopeKnown() {
		file.Scope.UnknownReason = raw.GetScopeUnknownReason().GetValue()
	}
	links := reviewFileLinks{}
	for _, value := range raw.GetPlanSteps() {
		links.steps = append(links.steps, taskgraph.PlanStepLink{PlanRevision: value.GetPlanRevision(), StepID: value.GetStepId()})
	}
	for _, value := range raw.GetToolEventIds() {
		if value.GetKind() != codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_EVENT {
			return diffreview.ChangedFile{}, reviewFileLinks{}, errReviewResourceMalformed
		}
		id, parseErr := domain.ParseEventID(value.GetValue())
		if parseErr != nil {
			return diffreview.ChangedFile{}, reviewFileLinks{}, parseErr
		}
		links.events = append(links.events, id)
	}
	for _, value := range raw.GetValidations() {
		if value.GetValidationId().GetKind() != codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_VALIDATION {
			return diffreview.ChangedFile{}, reviewFileLinks{}, errReviewResourceMalformed
		}
		id, parseErr := domain.ParseValidationID(value.GetValidationId().GetValue())
		if parseErr != nil {
			return diffreview.ChangedFile{}, reviewFileLinks{}, parseErr
		}
		links.validations = append(links.validations, diffreview.ValidationLink{
			ID: id, State: domain.ValidationState(value.GetState()),
			Label: value.GetLabel().GetValue(), Summary: value.GetSummary().GetValue(),
		})
	}
	return file, links, nil
}

func activeReviewCategoryFilters() []diffreview.CategoryFilter {
	values := diffreview.AllFileCategories()
	result := make([]diffreview.CategoryFilter, 0, len(values))
	for _, value := range values {
		result = append(result, diffreview.CategoryFilter{Category: value, Active: true})
	}
	return result
}
