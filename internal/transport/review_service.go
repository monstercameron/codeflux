package transport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/acceptance"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/gitwork"
	"codeflux.dev/codeflux/internal/review"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// EditorOpenApplication owns validated source resolution and the external
// editor effect behind the thin ReviewService handler.
type EditorOpenApplication interface {
	OpenInEditor(context.Context, review.OpenEditorCommand) (review.EditorTarget, error)
}

// ReviewEvidenceApplication exposes the bounded read-only review composition
// while keeping Git, plan, graph, and SQLite policy outside transport.
type ReviewEvidenceApplication interface {
	GetEvidenceReport(context.Context, review.EvidenceQuery) (review.EvidenceBundle, error)
}

type ReviewAcceptCommand struct {
	TaskID                                                         domain.TaskID
	ReviewID                                                       acceptance.ReviewID
	ReviewRevision                                                 uint64
	ReportID, DiffIdentity                                         string
	Acknowledged                                                   []string
	Mode                                                           gitwork.AcceptanceMode
	AuthorName, AuthorEmail, CommitMessage, Reason, IdempotencyKey string
}
type ReviewRepairCommand struct {
	TaskID                                           domain.TaskID
	ReviewID                                         acceptance.ReviewID
	ReviewRevision                                   uint64
	ReportID, DiffIdentity, Feedback, IdempotencyKey string
	Targets                                          []acceptance.RepairTarget
	CheckpointID                                     domain.CheckpointID
	MessageID                                        domain.MessageID
}
type ReviewRejectCommand struct {
	TaskID                                         domain.TaskID
	ReviewID                                       acceptance.ReviewID
	ReviewRevision                                 uint64
	ReportID, DiffIdentity, Reason, IdempotencyKey string
	CheckpointID                                   domain.CheckpointID
}
type ReviewRollbackCommand struct {
	TaskID                               domain.TaskID
	RepairID                             acceptance.RepairRequestID
	CheckpointID                         domain.CheckpointID
	ExpectedDiff, Reason, IdempotencyKey string
}
type ReviewMutationResult struct {
	Task                                                  TaskControlView
	ID, ReportID, DiffIdentity, CommitRevision, PatchPath string
	PatchApplied                                          bool
	PlanRevision                                          uint64
	CheckpointID                                          domain.CheckpointID
}
type ReviewMutationApplication interface {
	OpenReview(context.Context, domain.TaskID, string, string) (acceptance.Review, error)
	AcceptChange(context.Context, ReviewAcceptCommand) (ReviewMutationResult, error)
	RequestRepair(context.Context, ReviewRepairCommand) (ReviewMutationResult, error)
	RejectChange(context.Context, ReviewRejectCommand) (ReviewMutationResult, error)
	RollbackTask(context.Context, ReviewRollbackCommand) (ReviewMutationResult, error)
}

// ReviewService exposes the implemented review RPCs without moving review or
// filesystem policy into the transport layer.
type ReviewService struct {
	codefluxv1.UnimplementedReviewServiceServer
	editor    EditorOpenApplication
	evidence  ReviewEvidenceApplication
	mutations ReviewMutationApplication
}

// NewReviewService validates the implemented ReviewService dependencies.
func NewReviewService(editor EditorOpenApplication, evidence ReviewEvidenceApplication, mutations ...ReviewMutationApplication) (*ReviewService, error) {
	if editor == nil || evidence == nil {
		return nil, errors.New("editor-open and review-evidence applications are required")
	}
	service := &ReviewService{editor: editor, evidence: evidence}
	if len(mutations) > 0 {
		service.mutations = mutations[0]
	}
	return service, nil
}

// GetEvidenceReport returns a sealed structured report and an optional exact
// live diff. Authentication is enforced by the shared unary boundary.
func (service *ReviewService) GetEvidenceReport(
	ctx context.Context,
	request *codefluxv1.GetEvidenceReportRequest,
) (*codefluxv1.GetEvidenceReportResponse, error) {
	if request == nil {
		return nil, &RequestValidationError{Field: "request", Reason: "is required"}
	}
	taskID, err := TaskIDFromProto(request.GetTaskId())
	if err != nil {
		return nil, requestIdentityError("task_id", err)
	}
	if reportID := request.GetReportId(); reportID != "" && !reviewReportDigest(reportID) {
		return nil, &RequestValidationError{Field: "report_id", Reason: "has an invalid format"}
	}
	bundle, err := service.evidence.GetEvidenceReport(ctx, review.EvidenceQuery{
		TaskID: taskID, ReportID: request.GetReportId(), IncludeDiff: request.GetIncludeDiff(),
	})
	if err != nil {
		return nil, mapReviewEvidenceError(err)
	}
	encoded, err := json.Marshal(bundle.Report)
	if err != nil {
		return nil, status.Error(codes.Internal, "sealed evidence report could not be encoded")
	}
	if len(encoded) > review.MaximumDiffBytes {
		return nil, status.Error(codes.ResourceExhausted, "sealed evidence report exceeds the product query bound")
	}
	response := &codefluxv1.GetEvidenceReportResponse{
		StructuredReportJson: encoded,
		DiffMatchesReport:    bundle.DiffMatchesReport,
	}
	if service.mutations != nil {
		opened, openErr := service.mutations.OpenReview(ctx, taskID, bundle.Report.ID, bundle.Report.DiffIdentity)
		if openErr != nil {
			return nil, mapReviewEvidenceError(openErr)
		}
		response.ReviewId, response.ReviewRevision = opened.ID.String(), opened.Revision
	}
	if bundle.Diff != nil {
		response.Diff, err = reviewDiffToProto(bundle)
		if err != nil {
			return nil, status.Error(codes.Internal, "review diff could not be projected safely")
		}
	}
	return response, nil
}

func (service *ReviewService) AcceptChange(ctx context.Context, request *codefluxv1.AcceptChangeRequest) (*codefluxv1.AcceptChangeResponse, error) {
	if service.mutations == nil || request == nil || request.GetControl() == nil {
		return nil, status.Error(codes.FailedPrecondition, "review mutations are unavailable")
	}
	taskID, err := TaskIDFromProto(request.GetTaskId())
	if err != nil {
		return nil, err
	}
	reviewID, err := acceptance.ParseReviewID(request.GetReviewId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "review identity is invalid")
	}
	result, err := service.mutations.AcceptChange(ctx, ReviewAcceptCommand{TaskID: taskID, ReviewID: reviewID, ReviewRevision: request.GetExpectedReviewRevision(), ReportID: request.GetExpectedReportId(), DiffIdentity: request.GetExpectedDiffIdentity(), Acknowledged: request.GetAcknowledgedCheckIds(), Mode: gitwork.AcceptanceMode(request.GetAcceptanceMode()), AuthorName: request.GetAuthorName(), AuthorEmail: request.GetAuthorEmail(), CommitMessage: request.GetCommitMessage().GetValue(), Reason: request.GetReason().GetValue(), IdempotencyKey: request.GetControl().GetIdempotencyKey()})
	if err != nil {
		return nil, mapReviewMutationError(err)
	}
	task, err := taskControlViewToProto(result.Task)
	if err != nil {
		return nil, status.Error(codes.Internal, "accepted task projection failed")
	}
	return &codefluxv1.AcceptChangeResponse{Task: task, AcceptanceSummary: &codefluxv1.RedactedText{Value: "Accepted exact reviewed report and diff."}, AcceptanceId: result.ID, ReportId: result.ReportID, DiffIdentity: result.DiffIdentity, CommitRevision: result.CommitRevision, PatchApplied: result.PatchApplied}, nil
}

func (service *ReviewService) RequestRepair(ctx context.Context, envelope *codefluxv1.ReviewServiceRequestRepairRequest) (*codefluxv1.ReviewServiceRequestRepairResponse, error) {
	request := envelope.GetRequest()
	if service.mutations == nil || request == nil || request.GetControl() == nil {
		return nil, status.Error(codes.FailedPrecondition, "review mutations are unavailable")
	}
	taskID, err := TaskIDFromProto(request.GetTaskId())
	if err != nil {
		return nil, err
	}
	reviewID, err := acceptance.ParseReviewID(request.GetReviewId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "review identity is invalid")
	}
	checkpoint, err := CheckpointIDFromProto(request.GetCheckpointId())
	if err != nil {
		return nil, err
	}
	message, err := MessageIDFromProto(request.GetFeedbackMessageId())
	if err != nil {
		return nil, err
	}
	targets := make([]acceptance.RepairTarget, 0, len(request.GetTargets()))
	for _, target := range request.GetTargets() {
		targets = append(targets, acceptance.RepairTarget{Kind: acceptance.RepairTargetKind(target.GetKind()), ID: target.GetId()})
	}
	result, err := service.mutations.RequestRepair(ctx, ReviewRepairCommand{TaskID: taskID, ReviewID: reviewID, ReviewRevision: request.GetExpectedReviewRevision(), ReportID: request.GetExpectedReportId(), DiffIdentity: request.GetExpectedDiffIdentity(), Feedback: request.GetFeedback(), Targets: targets, CheckpointID: checkpoint, MessageID: message, IdempotencyKey: request.GetControl().GetIdempotencyKey()})
	if err != nil {
		return nil, mapReviewMutationError(err)
	}
	task, _ := taskControlViewToProto(result.Task)
	checkpointProto, _ := CheckpointIDToProto(result.CheckpointID)
	return &codefluxv1.ReviewServiceRequestRepairResponse{Result: &codefluxv1.RequestRepairResponse{Task: task, PlanRevision: result.PlanRevision, RepairRequestId: result.ID, PreRepairCheckpointId: checkpointProto}}, nil
}

func (service *ReviewService) RejectChange(ctx context.Context, request *codefluxv1.RejectChangeRequest) (*codefluxv1.RejectChangeResponse, error) {
	if service.mutations == nil || request == nil || !request.GetPreservePatch() {
		return nil, status.Error(codes.FailedPrecondition, "patch preservation is required")
	}
	taskID, err := TaskIDFromProto(request.GetTaskId())
	if err != nil {
		return nil, err
	}
	reviewID, err := acceptance.ParseReviewID(request.GetReviewId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "review identity is invalid")
	}
	checkpoint, err := CheckpointIDFromProto(request.GetCheckpointId())
	if err != nil {
		return nil, err
	}
	result, err := service.mutations.RejectChange(ctx, ReviewRejectCommand{TaskID: taskID, ReviewID: reviewID, ReviewRevision: request.GetExpectedReviewRevision(), ReportID: request.GetExpectedReportId(), DiffIdentity: request.GetExpectedDiffIdentity(), Reason: request.GetReason(), CheckpointID: checkpoint, IdempotencyKey: request.GetControl().GetIdempotencyKey()})
	if err != nil {
		return nil, mapReviewMutationError(err)
	}
	task, _ := taskControlViewToProto(result.Task)
	return &codefluxv1.RejectChangeResponse{Task: task, RejectionId: result.ID, PreservedPatchPath: result.PatchPath}, nil
}

func (service *ReviewService) RollbackTask(ctx context.Context, envelope *codefluxv1.ReviewServiceRollbackTaskRequest) (*codefluxv1.ReviewServiceRollbackTaskResponse, error) {
	request := envelope.GetRequest()
	if service.mutations == nil || request == nil {
		return nil, status.Error(codes.FailedPrecondition, "review mutations are unavailable")
	}
	taskID, err := TaskIDFromProto(request.GetTaskId())
	if err != nil {
		return nil, err
	}
	checkpoint, err := CheckpointIDFromProto(request.GetCheckpointId())
	if err != nil {
		return nil, err
	}
	repairID, err := acceptance.ParseRepairRequestID(request.GetRepairRequestId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "repair identity is invalid")
	}
	result, err := service.mutations.RollbackTask(ctx, ReviewRollbackCommand{TaskID: taskID, RepairID: repairID, CheckpointID: checkpoint, ExpectedDiff: request.GetExpectedRepairDiffIdentity(), Reason: request.GetReason().GetValue(), IdempotencyKey: request.GetControl().GetIdempotencyKey()})
	if err != nil {
		return nil, mapReviewMutationError(err)
	}
	task, _ := taskControlViewToProto(result.Task)
	return &codefluxv1.ReviewServiceRollbackTaskResponse{Result: &codefluxv1.RollbackTaskResponse{Task: task, RollbackIntentId: result.ID, ObservedDiffIdentity: result.DiffIdentity}}, nil
}

func mapReviewMutationError(err error) error {
	switch {
	case errors.Is(err, acceptance.ErrStaleReview):
		return status.Error(codes.Aborted, "review binding changed; open the renewed review")
	case errors.Is(err, acceptance.ErrValidationRunning), errors.Is(err, acceptance.ErrAcknowledgementNeeded), errors.Is(err, acceptance.ErrRequiredCheckBlocked):
		return status.Error(codes.FailedPrecondition, err.Error())
	default:
		return status.Error(codes.FailedPrecondition, "review mutation failed")
	}
}

// OpenInEditor validates transport identity and delegates repository
// confinement and coordinate validation to the review application function.
func (service *ReviewService) OpenInEditor(
	ctx context.Context,
	request *codefluxv1.OpenInEditorRequest,
) (*codefluxv1.OpenInEditorResponse, error) {
	if request == nil {
		return nil, &RequestValidationError{Field: "request", Reason: "is required"}
	}
	control := request.GetControl()
	if control == nil || !validCorrelationID(control.GetIdempotencyKey()) {
		return nil, &RequestValidationError{
			Field: "control.idempotency_key", Reason: "has an invalid format",
		}
	}
	workspaceID, err := WorkspaceIDFromProto(request.GetWorkspaceId())
	if err != nil {
		return nil, requestIdentityError("workspace_id", err)
	}
	target, err := service.editor.OpenInEditor(ctx, review.OpenEditorCommand{
		WorkspaceID: workspaceID, RelativePath: request.GetWorkspaceRelativePath(),
		Line: request.GetLine(), Column: request.GetColumn(),
		IdempotencyKey: control.GetIdempotencyKey(),
	})
	if err != nil {
		return nil, mapEditorOpenError(err)
	}
	return &codefluxv1.OpenInEditorResponse{
		Decision: "opened",
		Path: &codefluxv1.SafePath{
			WorkspaceRelativeSlashPath: target.RelativePath(),
		},
	}, nil
}

func mapEditorOpenError(err error) error {
	switch {
	case errors.Is(err, review.ErrEditorSourceOutsideRepository):
		return status.Error(codes.PermissionDenied, "editor source is outside the bound repository")
	case errors.Is(err, review.ErrInvalidEditorSource):
		return status.Error(codes.InvalidArgument, "editor source location is invalid")
	case errors.Is(err, review.ErrEditorSourceUnavailable):
		return status.Error(codes.FailedPrecondition, "editor source is unavailable")
	default:
		return status.Error(codes.Unavailable, "external editor could not be opened")
	}
}

func mapReviewEvidenceError(err error) error {
	switch {
	case errors.Is(err, review.ErrEvidenceTooLarge):
		return status.Error(codes.ResourceExhausted, "review evidence exceeds the product query bound")
	case errors.Is(err, review.ErrEvidenceUnavailable):
		return status.Error(codes.FailedPrecondition, "sealed review evidence is unavailable")
	default:
		return status.Error(codes.Unavailable, "review evidence could not be loaded")
	}
}

func reviewDiffToProto(bundle review.EvidenceBundle) (*codefluxv1.ReviewDiffView, error) {
	diff := bundle.Diff
	if diff == nil {
		return nil, errors.New("review diff is missing")
	}
	taskID, err := TaskIDToProto(diff.TaskID)
	if err != nil {
		return nil, err
	}
	result := &codefluxv1.ReviewDiffView{
		TaskId: taskID, DiffIdentity: diff.Identity, BaseRevision: diff.BaseRevision,
		HeadRevision: diff.HeadRevision, UnifiedDiff: redactedReviewText(diff.UnifiedDiff),
		Files: make([]*codefluxv1.ReviewDiffFileView, 0, len(diff.Files)),
	}
	statuses := make(map[string]string, len(bundle.Report.ChangedFiles))
	for _, file := range bundle.Report.ChangedFiles {
		statuses[file.Path] = string(file.Status)
	}
	for _, file := range diff.Files {
		statusValue := statuses[file.Path]
		if statusValue == "" {
			return nil, fmt.Errorf("review file %q is absent from the sealed report", file.Path)
		}
		view := &codefluxv1.ReviewDiffFileView{
			Path:   &codefluxv1.SafePath{WorkspaceRelativeSlashPath: file.Path},
			Status: statusValue, Category: file.Category, AddedLines: uint64(file.AddedLines),
			DeletedLines: uint64(file.DeletedLines), LineCountsKnown: !file.Binary,
			Binary: file.Binary, FormattingChurn: file.FormattingChurn,
			ScopeKnown: true, InScope: !file.OutsideApprovedScope,
		}
		if file.PreviousPath != "" {
			view.PreviousPath = &codefluxv1.SafePath{WorkspaceRelativeSlashPath: file.PreviousPath}
		}
		if file.Binary {
			view.LineCountsUnknownReason = redactedReviewText("Binary files do not expose text line counts.")
		}
		attribution := review.FileAttributionForPath(bundle.FileAttributions, file.Path)
		for _, link := range attribution.PlanSteps {
			view.PlanSteps = append(view.PlanSteps, &codefluxv1.ReviewPlanStepLinkView{
				PlanRevision: link.PlanRevision, StepId: link.StepID,
			})
		}
		for _, eventID := range attribution.EventIDs {
			identity, conversionErr := EventIDToProto(eventID)
			if conversionErr != nil {
				return nil, conversionErr
			}
			view.ToolEventIds = append(view.ToolEventIds, identity)
		}
		for _, validation := range attribution.Validations {
			identity, conversionErr := ValidationIDToProto(validation.ID)
			if conversionErr != nil {
				return nil, conversionErr
			}
			view.Validations = append(view.Validations, &codefluxv1.ReviewValidationLinkView{
				ValidationId: identity, State: string(validation.State),
				Label: redactedReviewText(validation.Label), Summary: redactedReviewText(validation.Summary),
			})
		}
		result.Files = append(result.Files, view)
	}
	return result, nil
}

func redactedReviewText(value string) *codefluxv1.RedactedText {
	return &codefluxv1.RedactedText{Value: value}
}

func reviewReportDigest(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}
