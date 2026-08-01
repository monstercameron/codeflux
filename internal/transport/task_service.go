package transport

import (
	"context"
	"errors"
	"strings"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	// ErrTaskControlStaleRevision identifies an optimistic-concurrency conflict
	// without coupling the transport adapter to a persistence implementation.
	ErrTaskControlStaleRevision = errors.New("task control stale revision")
	// ErrTaskControlNotFound identifies a missing task at the application port.
	ErrTaskControlNotFound = errors.New("task control target not found")
	// ErrTaskControlConflict identifies an invalid task state transition.
	ErrTaskControlConflict = errors.New("task control state conflict")
	// ErrTaskQueryNotFound identifies a missing authoritative task projection.
	ErrTaskQueryNotFound = errors.New("task query target not found")
	// ErrTaskBudgetNotFound identifies a missing authoritative task budget.
	ErrTaskBudgetNotFound = errors.New("task budget target not found")
	// ErrTaskBudgetStaleRevision identifies an optimistic budget conflict.
	ErrTaskBudgetStaleRevision = errors.New("task budget stale revision")
	// ErrTaskBudgetConflict identifies a forbidden or inconsistent adjustment.
	ErrTaskBudgetConflict = errors.New("task budget adjustment conflict")
	// ErrTaskBudgetApprovalRequired identifies a post-approval raise without an
	// exact granted approval.
	ErrTaskBudgetApprovalRequired = errors.New("task budget approval required")
	// ErrTaskBudgetUnsupported identifies a value that the v1 Money contract
	// cannot represent exactly without rounding or inventing omitted policy.
	ErrTaskBudgetUnsupported = errors.New("task budget representation unsupported")
)

// TaskControlCommand is the transport-independent command accepted by the
// pause/resume/cancel application boundary.
type TaskControlCommand struct {
	TaskID           domain.TaskID
	ExpectedRevision uint64
	IdempotencyKey   string
	ReasonRedacted   string
}

// TaskControlView is the minimum honest task projection returned by control
// mutations.
type TaskControlView struct {
	TaskID    domain.TaskID
	State     domain.TaskState
	Revision  uint64
	UpdatedAt time.Time
}

// TaskControlApplication owns the durable control use cases behind the thin
// gRPC handler.
type TaskControlApplication interface {
	PauseTaskControl(
		context.Context,
		TaskControlCommand,
	) (TaskControlView, error)
	ResumeTaskControl(
		context.Context,
		TaskControlCommand,
	) (TaskControlView, error)
	CancelTaskControl(
		context.Context,
		TaskControlCommand,
	) (TaskControlView, error)
}

// TaskQueryView is the bounded authoritative projection returned by GetTask.
// Nil monetary/token fields are deliberately unknown or not representable;
// they must never be replaced with a numeric zero.
type TaskQueryView struct {
	TaskID                   domain.TaskID
	ThreadID                 domain.ThreadID
	SessionID                domain.SessionID
	State                    domain.TaskState
	Revision                 uint64
	PlanRevision             uint64
	SummaryRedacted          string
	SummaryOriginalBytes     uint64
	SummaryTruncated         bool
	ActualCost               *domain.Money
	HardBudget               *domain.Money
	BudgetRevision           uint64
	ActualTokens             *domain.TokenCount
	SelectedProvider         string
	SelectedModel            string
	SelectedEffort           string
	Forecast                 *TaskForecastQueryView
	ActualPricingSnapshotIDs []string
	RemainingHardBudget      *domain.Money
	WarningThreshold         *domain.Money
	WarningReached           bool
	HardCapReached           bool
	SettlingProviderRequest  *bool
	LatestCheckpointID       *domain.CheckpointID
	LatestCheckpointState    domain.CheckpointState
	LatestCheckpointPlanStep string
	Elapsed                  time.Duration
	UpdatedAt                time.Time
}

// TaskForecastQueryView contains only immutable forecast facts that can be
// represented exactly by the v1 wire contract.
type TaskForecastQueryView struct {
	Range              domain.ForecastRange
	AlgorithmVersion   string
	EstimateNotice     string
	PriceSnapshotID    string
	PriceSource        string
	PriceCapturedAt    time.Time
	UncertaintyReasons []string
	Revision           uint64
}

// TaskQueryApplication owns the read-only GetTask use case.
type TaskQueryApplication interface {
	GetTaskQuery(context.Context, domain.TaskID) (TaskQueryView, error)
}

// TaskBudgetCommand changes only the exact hard cost limit exposed by the v1
// RPC. All omitted policy dimensions remain server-authoritative.
type TaskBudgetCommand struct {
	TaskID           domain.TaskID
	ExpectedRevision uint64
	IdempotencyKey   string
	HardLimit        domain.Money
}

// TaskBudgetView is the exact representable budget projection returned after
// a committed change. Nil values are unknown or rationally non-integral.
type TaskBudgetView struct {
	BudgetID  domain.BudgetID
	TaskID    domain.TaskID
	HardLimit domain.Money
	Reserved  *domain.Money
	Actual    *domain.Money
	Revision  uint64
}

// TaskBudgetApplication owns the authority-preserving SetBudget use case.
type TaskBudgetApplication interface {
	SetTaskBudget(context.Context, TaskBudgetCommand) (TaskBudgetView, error)
}

// RecoveryPatchCommand preserves a checkpoint patch without changing task
// execution state. ExpectedRevision binds the choice to the visible recovery.
type RecoveryPatchCommand struct {
	TaskID           domain.TaskID
	ExpectedRevision uint64
	IdempotencyKey   string
	ReasonRedacted   string
}

type RecoveryPatchView struct {
	TaskID       domain.TaskID
	AssessmentID string
	PatchPath    string
}

type RecoveryReconcileCommand = RecoveryPatchCommand

type RecoveryReconcileView struct {
	TaskID       domain.TaskID
	AssessmentID string
	CheckpointID domain.CheckpointID
	State        domain.TaskState
	Revision     uint64
}

type RecoverySafeResumeCommand = RecoveryPatchCommand
type RecoverySafeResumeView = RecoveryReconcileView

type RecoveryActionApplication interface {
	PreserveTaskRecoveryPatch(context.Context, RecoveryPatchCommand) (RecoveryPatchView, error)
}

type RecoveryReconciliationApplication interface {
	ReconcileTaskRecovery(context.Context, RecoveryReconcileCommand) (RecoveryReconcileView, error)
}

type RecoverySafeResumeApplication interface {
	SafeResumeTaskRecovery(context.Context, RecoverySafeResumeCommand) (RecoverySafeResumeView, error)
}

// TaskProjectionInvalidationApplication appends one durable ordered repair
// signal after a normalized correctness mutation commits.
type TaskProjectionInvalidationApplication interface {
	NotifyTaskProjectionInvalidated(
		context.Context,
		domain.TaskID,
		string,
		uint64,
		string,
	) error
}

// TaskService implements the existing TaskService pause/resume/cancel RPCs.
// Other TaskService methods intentionally retain Unimplemented responses until
// their owning milestones provide application services.
type TaskService struct {
	codefluxv1.UnimplementedTaskServiceServer
	controls      TaskControlApplication
	queries       TaskQueryApplication
	budgets       TaskBudgetApplication
	recovery      RecoveryActionApplication
	reconciler    RecoveryReconciliationApplication
	safeResume    RecoverySafeResumeApplication
	invalidations TaskProjectionInvalidationApplication
	lifecycle     TaskLifecycleApplication
}

func (service *TaskService) ConfigureProjectionInvalidations(
	invalidations TaskProjectionInvalidationApplication,
) error {
	if service == nil || invalidations == nil {
		return errors.New("task projection invalidation application is required")
	}
	if service.invalidations != nil {
		return errors.New("task projection invalidation application is already configured")
	}
	service.invalidations = invalidations
	return nil
}

func NewTaskServiceWithRecovery(
	controls TaskControlApplication,
	queries TaskQueryApplication,
	budgets TaskBudgetApplication,
	recovery RecoveryActionApplication,
) (*TaskService, error) {
	if recovery == nil {
		return nil, errors.New("task recovery application is required")
	}
	service, err := NewTaskServiceWithBudget(controls, queries, budgets)
	if err != nil {
		return nil, err
	}
	service.recovery = recovery
	service.reconciler, _ = recovery.(RecoveryReconciliationApplication)
	service.safeResume, _ = recovery.(RecoverySafeResumeApplication)
	return service, nil
}

// NewTaskServiceWithBudget preserves the original constructor while attaching
// the authoritative budget mutation application at startup.
func NewTaskServiceWithBudget(
	controls TaskControlApplication,
	queries TaskQueryApplication,
	budgets TaskBudgetApplication,
) (*TaskService, error) {
	if budgets == nil {
		return nil, errors.New("task budget application is required")
	}
	service, err := NewTaskService(controls, queries)
	if err != nil {
		return nil, err
	}
	service.budgets = budgets
	return service, nil
}

func NewTaskService(
	controls TaskControlApplication,
	queryApplications ...TaskQueryApplication,
) (*TaskService, error) {
	if controls == nil {
		return nil, errors.New("task control application is required")
	}
	if len(queryApplications) > 1 {
		return nil, errors.New("at most one task query application is permitted")
	}
	query, _ := controls.(TaskQueryApplication)
	if len(queryApplications) == 1 {
		if queryApplications[0] == nil {
			return nil, errors.New("task query application must not be nil")
		}
		query = queryApplications[0]
	}
	return &TaskService{controls: controls, queries: query}, nil
}

func (service *TaskService) GetTask(
	ctx context.Context,
	request *codefluxv1.GetTaskRequest,
) (*codefluxv1.GetTaskResponse, error) {
	if service.queries == nil {
		return nil, status.Error(codes.Unimplemented, "task query application is unavailable")
	}
	taskID, err := TaskIDFromProto(request.GetTaskId())
	if err != nil {
		return nil, err
	}
	view, err := service.queries.GetTaskQuery(ctx, taskID)
	if err != nil {
		return nil, mapTaskQueryError(err, taskID)
	}
	task, err := taskQueryViewToProto(view)
	if err != nil {
		return nil, err
	}
	return &codefluxv1.GetTaskResponse{Task: task}, nil
}

func (service *TaskService) SetBudget(
	ctx context.Context,
	request *codefluxv1.SetBudgetRequest,
) (*codefluxv1.SetBudgetResponse, error) {
	if service.budgets == nil {
		return nil, status.Error(codes.Unimplemented, "task budget application is unavailable")
	}
	command, err := taskBudgetCommand(request)
	if err != nil {
		return nil, err
	}
	view, err := service.budgets.SetTaskBudget(ctx, command)
	if err != nil {
		return nil, mapTaskBudgetError(err, command.TaskID)
	}
	budget, err := taskBudgetViewToProto(view)
	if err != nil {
		return nil, err
	}
	if err := service.notifyProjectionInvalidated(ctx, command.TaskID, "budget", view.Revision, command.IdempotencyKey); err != nil {
		return nil, err
	}
	return &codefluxv1.SetBudgetResponse{Budget: budget}, nil
}

func (service *TaskService) PreserveRecoveryPatch(
	ctx context.Context,
	request *codefluxv1.PreserveRecoveryPatchRequest,
) (*codefluxv1.PreserveRecoveryPatchResponse, error) {
	if service.recovery == nil {
		return nil, status.Error(codes.Unimplemented, "task recovery application is unavailable")
	}
	command, err := recoveryPatchCommand(request)
	if err != nil {
		return nil, err
	}
	view, err := service.recovery.PreserveTaskRecoveryPatch(ctx, command)
	if err != nil {
		return nil, mapTaskControlError(err, command.TaskID)
	}
	taskID, err := TaskIDToProto(view.TaskID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(view.AssessmentID) == "" || strings.TrimSpace(view.PatchPath) == "" {
		return nil, status.Error(codes.Internal, "recovery patch result is incomplete")
	}
	if err := service.notifyProjectionInvalidated(ctx, command.TaskID, "recovery-patch", command.ExpectedRevision, command.IdempotencyKey); err != nil {
		return nil, err
	}
	return &codefluxv1.PreserveRecoveryPatchResponse{
		TaskId: taskID, AssessmentId: view.AssessmentID, PatchPath: view.PatchPath,
	}, nil
}

func (service *TaskService) ReconcileRecovery(
	ctx context.Context,
	request *codefluxv1.ReconcileRecoveryRequest,
) (*codefluxv1.ReconcileRecoveryResponse, error) {
	if service.reconciler == nil {
		return nil, status.Error(codes.Unimplemented, "task recovery application is unavailable")
	}
	command, err := recoveryReconcileCommand(request)
	if err != nil {
		return nil, err
	}
	view, err := service.reconciler.ReconcileTaskRecovery(ctx, command)
	if err != nil {
		return nil, mapTaskControlError(err, command.TaskID)
	}
	taskID, err := TaskIDToProto(view.TaskID)
	if err != nil {
		return nil, err
	}
	checkpointID, err := CheckpointIDToProto(view.CheckpointID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(view.AssessmentID) == "" || view.Revision == 0 {
		return nil, status.Error(codes.Internal, "recovery reconciliation result is incomplete")
	}
	if err := service.notifyProjectionInvalidated(ctx, command.TaskID, "recovery", view.Revision, command.IdempotencyKey); err != nil {
		return nil, err
	}
	return &codefluxv1.ReconcileRecoveryResponse{
		TaskId: taskID, AssessmentId: view.AssessmentID,
		CheckpointId: checkpointID, State: string(view.State), Revision: view.Revision,
	}, nil
}

func (service *TaskService) SafeResumeRecovery(
	ctx context.Context,
	request *codefluxv1.SafeResumeRecoveryRequest,
) (*codefluxv1.SafeResumeRecoveryResponse, error) {
	if service.safeResume == nil {
		return nil, status.Error(codes.Unimplemented, "safe recovery resume application is unavailable")
	}
	command, err := recoverySafeResumeCommand(request)
	if err != nil {
		return nil, err
	}
	view, err := service.safeResume.SafeResumeTaskRecovery(ctx, command)
	if err != nil {
		return nil, mapTaskControlError(err, command.TaskID)
	}
	taskID, err := TaskIDToProto(view.TaskID)
	if err != nil {
		return nil, err
	}
	checkpointID, err := CheckpointIDToProto(view.CheckpointID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(view.AssessmentID) == "" || view.Revision == 0 {
		return nil, status.Error(codes.Internal, "safe recovery resume result is incomplete")
	}
	if err := service.notifyProjectionInvalidated(ctx, command.TaskID, "recovery", view.Revision, command.IdempotencyKey); err != nil {
		return nil, err
	}
	return &codefluxv1.SafeResumeRecoveryResponse{
		TaskId: taskID, AssessmentId: view.AssessmentID,
		CheckpointId: checkpointID, State: string(view.State), Revision: view.Revision,
	}, nil
}

func recoveryPatchCommand(request *codefluxv1.PreserveRecoveryPatchRequest) (RecoveryPatchCommand, error) {
	if request == nil {
		return RecoveryPatchCommand{}, &RequestValidationError{Field: "request", Reason: "is required"}
	}
	control := request.GetControl()
	if control == nil || control.ExpectedRevision == nil {
		return RecoveryPatchCommand{}, &RequestValidationError{
			Field: "control.expected_revision", Reason: "is required",
		}
	}
	if !validCorrelationID(control.GetIdempotencyKey()) {
		return RecoveryPatchCommand{}, &RequestValidationError{
			Field: "control.idempotency_key", Reason: "has an invalid format",
		}
	}
	taskID, err := TaskIDFromProto(request.GetTaskId())
	if err != nil {
		return RecoveryPatchCommand{}, err
	}
	reason := strings.TrimSpace(request.GetReason())
	if reason == "" || len(reason) > 2048 {
		return RecoveryPatchCommand{}, &RequestValidationError{Field: "reason", Reason: "is required and bounded"}
	}
	return RecoveryPatchCommand{
		TaskID: taskID, ExpectedRevision: control.GetExpectedRevision(),
		IdempotencyKey: control.GetIdempotencyKey(), ReasonRedacted: reason,
	}, nil
}

func recoveryReconcileCommand(request *codefluxv1.ReconcileRecoveryRequest) (RecoveryReconcileCommand, error) {
	if request == nil {
		return RecoveryReconcileCommand{}, &RequestValidationError{Field: "request", Reason: "is required"}
	}
	patchCommand, err := recoveryPatchCommand(&codefluxv1.PreserveRecoveryPatchRequest{
		Control: request.GetControl(), TaskId: request.GetTaskId(), Reason: request.GetReason(),
	})
	return RecoveryReconcileCommand(patchCommand), err
}

func recoverySafeResumeCommand(request *codefluxv1.SafeResumeRecoveryRequest) (RecoverySafeResumeCommand, error) {
	if request == nil {
		return RecoverySafeResumeCommand{}, &RequestValidationError{Field: "request", Reason: "is required"}
	}
	patchCommand, err := recoveryPatchCommand(&codefluxv1.PreserveRecoveryPatchRequest{
		Control: request.GetControl(), TaskId: request.GetTaskId(), Reason: request.GetReason(),
	})
	return RecoverySafeResumeCommand(patchCommand), err
}

func taskBudgetCommand(request *codefluxv1.SetBudgetRequest) (TaskBudgetCommand, error) {
	if request == nil {
		return TaskBudgetCommand{}, &RequestValidationError{Field: "request", Reason: "is required"}
	}
	control := request.GetControl()
	if control == nil || control.ExpectedRevision == nil {
		return TaskBudgetCommand{}, &RequestValidationError{
			Field: "control.expected_revision", Reason: "is required",
		}
	}
	if !validCorrelationID(control.GetIdempotencyKey()) {
		return TaskBudgetCommand{}, &RequestValidationError{
			Field: "control.idempotency_key", Reason: "has an invalid format",
		}
	}
	taskID, err := TaskIDFromProto(request.GetTaskId())
	if err != nil {
		return TaskBudgetCommand{}, err
	}
	hardLimit := request.GetHardLimit()
	if hardLimit == nil {
		return TaskBudgetCommand{}, &RequestValidationError{Field: "hard_limit", Reason: "is required"}
	}
	if err := validateMoney(hardLimit, "hard_limit"); err != nil {
		return TaskBudgetCommand{}, err
	}
	if hardLimit.GetDecimalPlaces() != 0 {
		return TaskBudgetCommand{}, &RequestValidationError{
			Field:  "hard_limit.decimal_places",
			Reason: "must be zero because task budgets use exact currency minor units",
		}
	}
	currency, err := domain.ParseCurrencyCode(hardLimit.GetCurrencyCode())
	if err != nil {
		return TaskBudgetCommand{}, &RequestValidationError{Field: "hard_limit.currency_code", Reason: "is invalid"}
	}
	money, err := domain.NewMoney(currency, hardLimit.GetMinorUnits())
	if err != nil {
		return TaskBudgetCommand{}, &RequestValidationError{Field: "hard_limit", Reason: "is invalid"}
	}
	return TaskBudgetCommand{
		TaskID: taskID, ExpectedRevision: control.GetExpectedRevision(),
		IdempotencyKey: control.GetIdempotencyKey(), HardLimit: money,
	}, nil
}

func taskBudgetViewToProto(view TaskBudgetView) (*codefluxv1.BudgetView, error) {
	budgetID, err := BudgetIDToProto(view.BudgetID)
	if err != nil {
		return nil, err
	}
	taskID, err := TaskIDToProto(view.TaskID)
	if err != nil {
		return nil, err
	}
	return &codefluxv1.BudgetView{
		BudgetId: budgetID, TaskId: taskID,
		HardLimit: taskMoneyToProto(&view.HardLimit),
		Reserved:  taskMoneyToProto(view.Reserved),
		Actual:    taskMoneyToProto(view.Actual), Revision: view.Revision,
	}, nil
}

func (service *TaskService) PauseTask(
	ctx context.Context,
	request *codefluxv1.PauseTaskRequest,
) (*codefluxv1.PauseTaskResponse, error) {
	command, err := taskControlCommand(
		request.GetControl(),
		request.GetTaskId(),
		request.GetReason(),
		true,
	)
	if err != nil {
		return nil, err
	}
	view, err := service.controls.PauseTaskControl(ctx, command)
	if err != nil {
		return nil, mapTaskControlError(err, command.TaskID)
	}
	task, err := taskControlViewToProto(view)
	if err != nil {
		return nil, err
	}
	if err := service.notifyProjectionInvalidated(ctx, command.TaskID, "task", view.Revision, command.IdempotencyKey); err != nil {
		return nil, err
	}
	return &codefluxv1.PauseTaskResponse{Task: task}, nil
}

func (service *TaskService) ResumeTask(
	ctx context.Context,
	request *codefluxv1.ResumeTaskRequest,
) (*codefluxv1.ResumeTaskResponse, error) {
	command, err := taskControlCommand(
		request.GetControl(),
		request.GetTaskId(),
		"compatibility-checked task resume",
		true,
	)
	if err != nil {
		return nil, err
	}
	view, err := service.controls.ResumeTaskControl(ctx, command)
	if err != nil {
		return nil, mapTaskControlError(err, command.TaskID)
	}
	task, err := taskControlViewToProto(view)
	if err != nil {
		return nil, err
	}
	if err := service.notifyProjectionInvalidated(ctx, command.TaskID, "task", view.Revision, command.IdempotencyKey); err != nil {
		return nil, err
	}
	return &codefluxv1.ResumeTaskResponse{Task: task}, nil
}

func (service *TaskService) CancelTask(
	ctx context.Context,
	request *codefluxv1.CancelTaskRequest,
) (*codefluxv1.CancelTaskResponse, error) {
	command, err := taskControlCommand(
		request.GetControl(),
		request.GetTaskId(),
		request.GetReason(),
		true,
	)
	if err != nil {
		return nil, err
	}
	view, err := service.controls.CancelTaskControl(ctx, command)
	if err != nil {
		return nil, mapTaskControlError(err, command.TaskID)
	}
	task, err := taskControlViewToProto(view)
	if err != nil {
		return nil, err
	}
	if err := service.notifyProjectionInvalidated(ctx, command.TaskID, "task", view.Revision, command.IdempotencyKey); err != nil {
		return nil, err
	}
	return &codefluxv1.CancelTaskResponse{Task: task}, nil
}

func (service *TaskService) notifyProjectionInvalidated(
	ctx context.Context,
	taskID domain.TaskID,
	entity string,
	revision uint64,
	key string,
) error {
	if service.invalidations == nil {
		return nil
	}
	return service.invalidations.NotifyTaskProjectionInvalidated(
		ctx, taskID, entity, revision, key+"/session-projection",
	)
}

func taskControlCommand(
	control *codefluxv1.MutationControl,
	taskIdentity *codefluxv1.StableIdentity,
	reason string,
	requireReason bool,
) (TaskControlCommand, error) {
	if control == nil || control.ExpectedRevision == nil {
		return TaskControlCommand{}, &RequestValidationError{
			Field:  "control.expected_revision",
			Reason: "is required",
		}
	}
	taskID, err := TaskIDFromProto(taskIdentity)
	if err != nil {
		return TaskControlCommand{}, err
	}
	reason = strings.TrimSpace(reason)
	if requireReason && reason == "" {
		return TaskControlCommand{}, &RequestValidationError{
			Field:  "reason",
			Reason: "is required",
		}
	}
	if len(reason) > 2048 {
		return TaskControlCommand{}, &RequestValidationError{
			Field:  "reason",
			Reason: "is too long",
		}
	}
	return TaskControlCommand{
		TaskID:           taskID,
		ExpectedRevision: control.GetExpectedRevision(),
		IdempotencyKey:   control.GetIdempotencyKey(),
		ReasonRedacted:   reason,
	}, nil
}

func taskControlViewToProto(
	view TaskControlView,
) (*codefluxv1.TaskView, error) {
	taskID, err := TaskIDToProto(view.TaskID)
	if err != nil {
		return nil, err
	}
	updated := timestamppb.New(view.UpdatedAt.UTC())
	if err := updated.CheckValid(); err != nil {
		return nil, err
	}
	return &codefluxv1.TaskView{
		TaskId:    taskID,
		State:     string(view.State),
		Revision:  view.Revision,
		UpdatedAt: updated,
	}, nil
}

func taskQueryViewToProto(view TaskQueryView) (*codefluxv1.TaskView, error) {
	taskID, err := TaskIDToProto(view.TaskID)
	if err != nil {
		return nil, err
	}
	threadID, err := ThreadIDToProto(view.ThreadID)
	if err != nil {
		return nil, err
	}
	var sessionID *codefluxv1.StableIdentity
	if !view.SessionID.IsZero() {
		sessionID, err = SessionIDToProto(view.SessionID)
		if err != nil {
			return nil, err
		}
	}
	updated := timestamppb.New(view.UpdatedAt.UTC())
	if err := updated.CheckValid(); err != nil {
		return nil, err
	}
	elapsed := durationpb.New(view.Elapsed)
	if err := elapsed.CheckValid(); err != nil {
		return nil, err
	}
	result := &codefluxv1.TaskView{
		TaskId: taskID, ThreadId: threadID, SessionId: sessionID,
		State: string(view.State), Revision: view.Revision,
		PlanRevision: view.PlanRevision, Elapsed: elapsed, UpdatedAt: updated,
		ActualCost:               taskMoneyToProto(view.ActualCost),
		HardBudget:               taskMoneyToProto(view.HardBudget),
		BudgetRevision:           view.BudgetRevision,
		SelectedProvider:         view.SelectedProvider,
		SelectedModel:            view.SelectedModel,
		SelectedEffort:           view.SelectedEffort,
		ActualPricingSnapshotIds: append([]string(nil), view.ActualPricingSnapshotIDs...),
		RemainingHardBudget:      taskMoneyToProto(view.RemainingHardBudget),
		WarningThreshold:         taskMoneyToProto(view.WarningThreshold),
		WarningReached:           view.WarningReached,
		HardCapReached:           view.HardCapReached,
		SettlingProviderRequest:  view.SettlingProviderRequest,
	}
	if view.LatestCheckpointID != nil {
		checkpointID, checkpointErr := CheckpointIDToProto(*view.LatestCheckpointID)
		if checkpointErr != nil {
			return nil, checkpointErr
		}
		if !view.LatestCheckpointState.IsValid() {
			return nil, errors.New("latest checkpoint state is invalid")
		}
		result.LatestCheckpointId = checkpointID
		result.LatestCheckpointState = string(view.LatestCheckpointState)
		result.LatestCheckpointPlanStep = view.LatestCheckpointPlanStep
	} else if view.LatestCheckpointState != "" || view.LatestCheckpointPlanStep != "" {
		return nil, errors.New("latest checkpoint facts require an identity")
	}
	if view.SummaryRedacted != "" || view.SummaryOriginalBytes != 0 {
		result.Summary = &codefluxv1.RedactedText{
			Value: view.SummaryRedacted, Truncated: view.SummaryTruncated,
			OriginalBytes: view.SummaryOriginalBytes,
		}
	}
	if view.ActualTokens != nil {
		result.ActualTokens = &codefluxv1.TokenAmount{Tokens: uint64(*view.ActualTokens)}
	}
	if view.Forecast != nil {
		result.Forecast = taskForecastToProto(*view.Forecast)
	}
	return result, nil
}

func taskForecastToProto(view TaskForecastQueryView) *codefluxv1.TaskForecastView {
	result := &codefluxv1.TaskForecastView{
		AlgorithmVersion:   view.AlgorithmVersion,
		EstimateNotice:     view.EstimateNotice,
		LatencyKnown:       view.Range.LatencyKnown,
		LatencyP50Ms:       int64(view.Range.LatencyP50Millis),
		LatencyP90Ms:       int64(view.Range.LatencyP90Millis),
		TokensKnown:        view.Range.TokensKnown,
		TokensP50:          uint64(view.Range.TokensP50),
		TokensP90:          uint64(view.Range.TokensP90),
		PriceSnapshotId:    view.PriceSnapshotID,
		PriceSource:        view.PriceSource,
		UncertaintyReasons: append([]string(nil), view.UncertaintyReasons...),
		Revision:           view.Revision,
	}
	if view.Range.CostKnown {
		result.CostP50 = taskMoneyToProto(&view.Range.CostP50)
		result.CostP90 = taskMoneyToProto(&view.Range.CostP90)
	}
	if !view.PriceCapturedAt.IsZero() {
		result.PriceCapturedAt = timestamppb.New(view.PriceCapturedAt.UTC())
	}
	return result
}

func taskMoneyToProto(value *domain.Money) *codefluxv1.Money {
	if value == nil {
		return nil
	}
	return &codefluxv1.Money{
		CurrencyCode: string(value.Currency), MinorUnits: value.MinorUnits,
		// Domain money is already expressed in exact currency minor units. No
		// currency exponent is inferred at this boundary.
		DecimalPlaces: 0,
	}
}

func mapTaskControlError(
	err error,
	taskID domain.TaskID,
) error {
	entity, _ := TaskIDToProto(taskID)
	switch {
	case errors.Is(err, ErrTaskControlStaleRevision):
		return &ApplicationError{
			Code:        codefluxv1.ErrorCode_ERROR_CODE_STALE_REVISION,
			SafeMessage: "The task changed before this control request.",
			EntityID:    entity,
		}
	case errors.Is(err, ErrTaskControlNotFound):
		return &ApplicationError{
			Code:        codefluxv1.ErrorCode_ERROR_CODE_NOT_FOUND,
			SafeMessage: "The task could not be found.",
			EntityID:    entity,
		}
	case errors.Is(err, ErrTaskControlConflict):
		return &ApplicationError{
			Code:        codefluxv1.ErrorCode_ERROR_CODE_INVALID_TRANSITION,
			SafeMessage: "The task cannot accept that control in its current state.",
			EntityID:    entity,
		}
	default:
		return err
	}
}

func mapTaskQueryError(err error, taskID domain.TaskID) error {
	if !errors.Is(err, ErrTaskQueryNotFound) {
		return err
	}
	entity, _ := TaskIDToProto(taskID)
	return &ApplicationError{
		Code:        codefluxv1.ErrorCode_ERROR_CODE_NOT_FOUND,
		SafeMessage: "The task could not be found.",
		EntityID:    entity,
	}
}

func mapTaskBudgetError(err error, taskID domain.TaskID) error {
	entity, _ := TaskIDToProto(taskID)
	switch {
	case errors.Is(err, ErrTaskBudgetNotFound):
		return &ApplicationError{
			Code:        codefluxv1.ErrorCode_ERROR_CODE_NOT_FOUND,
			SafeMessage: "The task budget could not be found.", EntityID: entity,
		}
	case errors.Is(err, ErrTaskBudgetStaleRevision):
		return &ApplicationError{
			Code:        codefluxv1.ErrorCode_ERROR_CODE_STALE_REVISION,
			SafeMessage: "The budget changed before this adjustment.", EntityID: entity,
		}
	case errors.Is(err, ErrTaskBudgetApprovalRequired):
		return &ApplicationError{
			Code:        codefluxv1.ErrorCode_ERROR_CODE_DENIED,
			SafeMessage: "This exact budget raise requires a granted approval.", EntityID: entity,
		}
	case errors.Is(err, ErrTaskBudgetConflict):
		return &ApplicationError{
			Code:        codefluxv1.ErrorCode_ERROR_CODE_INVALID_TRANSITION,
			SafeMessage: "The requested budget change is not permitted in the current state.", EntityID: entity,
		}
	case errors.Is(err, ErrTaskBudgetUnsupported):
		return &ApplicationError{
			Code:        codefluxv1.ErrorCode_ERROR_CODE_INVALID_ARGUMENT,
			SafeMessage: "The current budget cannot be changed exactly through this API version.", EntityID: entity,
		}
	default:
		return err
	}
}

var _ codefluxv1.TaskServiceServer = (*TaskService)(nil)
