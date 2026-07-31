package coordinator

import (
	"context"
	"errors"

	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
)

// PauseTaskControl resolves the latest run revision, creates attributable
// event identities, and executes the durable pause flow.
func (service *TaskControlService) PauseTaskControl(
	ctx context.Context,
	command transport.TaskControlCommand,
) (transport.TaskControlView, error) {
	replay, err := service.controlReplay(
		ctx, command, storage.TaskControlReplayPause,
	)
	if err != nil || replay.Found {
		return controlTransportView(replay.Control), taskControlPortError(err)
	}
	current, err := service.currentControl(ctx, command)
	if err != nil {
		return transport.TaskControlView{}, taskControlPortError(err)
	}
	requestEventID, err := service.dependencies.NewEventID()
	if err != nil {
		return transport.TaskControlView{}, err
	}
	pausedEventID, err := service.dependencies.NewEventID()
	if err != nil {
		return transport.TaskControlView{}, err
	}
	result, err := service.PauseTask(
		ctx,
		PauseTaskInput{
			RequestEventID:       requestEventID,
			PausedEventID:        pausedEventID,
			TaskID:               current.TaskID,
			RunID:                current.RunID,
			ExpectedTaskRevision: current.TaskRevision,
			ExpectedRunRevision:  current.RunRevision,
			ReasonRedacted:       command.ReasonRedacted,
			IdempotencyKey:       command.IdempotencyKey,
		},
	)
	return controlTransportView(result), taskControlPortError(err)
}

func (service *TaskControlService) ResumeTaskControl(
	ctx context.Context,
	command transport.TaskControlCommand,
) (transport.TaskControlView, error) {
	replay, err := service.controlReplay(
		ctx, command, storage.TaskControlReplayResume,
	)
	if err != nil {
		return transport.TaskControlView{}, taskControlPortError(err)
	}
	if replay.Found {
		replayErr := error(nil)
		if replay.Blocked {
			replayErr = ErrResumeReconciliationRequired
		}
		return controlTransportView(replay.Control), replayErr
	}
	current, err := service.currentControl(ctx, command)
	if err != nil {
		return transport.TaskControlView{}, taskControlPortError(err)
	}
	resumedEventID, err := service.dependencies.NewEventID()
	if err != nil {
		return transport.TaskControlView{}, err
	}
	blockedEventID, err := service.dependencies.NewEventID()
	if err != nil {
		return transport.TaskControlView{}, err
	}
	result, err := service.ResumeTask(
		ctx,
		ResumeTaskInput{
			ResumedEventID:       resumedEventID,
			BlockedEventID:       blockedEventID,
			TaskID:               current.TaskID,
			RunID:                current.RunID,
			ExpectedTaskRevision: current.TaskRevision,
			ExpectedRunRevision:  current.RunRevision,
			IdempotencyKey:       command.IdempotencyKey,
		},
	)
	return controlTransportView(result.Control), taskControlPortError(err)
}

func (service *TaskControlService) CancelTaskControl(
	ctx context.Context,
	command transport.TaskControlCommand,
) (transport.TaskControlView, error) {
	replay, err := service.controlReplay(
		ctx, command, storage.TaskControlReplayCancel,
	)
	if err != nil || replay.Found {
		return controlTransportView(replay.Control), taskControlPortError(err)
	}
	current, err := service.currentControl(ctx, command)
	if err != nil {
		return transport.TaskControlView{}, taskControlPortError(err)
	}
	eventID, err := service.dependencies.NewEventID()
	if err != nil {
		return transport.TaskControlView{}, err
	}
	result, err := service.CancelTask(
		ctx,
		CancelTaskInput{
			EventID:              eventID,
			TaskID:               current.TaskID,
			RunID:                current.RunID,
			ExpectedTaskRevision: current.TaskRevision,
			ExpectedRunRevision:  current.RunRevision,
			ReasonRedacted:       command.ReasonRedacted,
			IdempotencyKey:       command.IdempotencyKey,
		},
	)
	return controlTransportView(result), taskControlPortError(err)
}

func taskControlPortError(err error) error {
	switch {
	case errors.Is(err, storage.ErrStaleRevision):
		return transport.ErrTaskControlStaleRevision
	case errors.Is(err, storage.ErrNotFound):
		return transport.ErrTaskControlNotFound
	case errors.Is(err, storage.ErrConflict):
		return transport.ErrTaskControlConflict
	default:
		return err
	}
}

func (service *TaskControlService) controlReplay(
	ctx context.Context,
	command transport.TaskControlCommand,
	operation storage.TaskControlReplayOperation,
) (storage.TaskControlReplay, error) {
	return service.dependencies.Store.ReadTaskControlReplay(
		ctx,
		storage.TaskControlReplayRequest{
			TaskID: command.TaskID, Operation: operation,
			ExpectedTaskRevision: command.ExpectedRevision,
			ReasonRedacted:       command.ReasonRedacted,
			IdempotencyKey:       command.IdempotencyKey,
		},
	)
}

func (service *TaskControlService) currentControl(
	ctx context.Context,
	command transport.TaskControlCommand,
) (storage.TaskControlSnapshot, error) {
	current, err := service.dependencies.Store.ReadTaskControl(
		ctx,
		command.TaskID,
	)
	if err != nil {
		return storage.TaskControlSnapshot{}, err
	}
	if current.TaskRevision != command.ExpectedRevision {
		return storage.TaskControlSnapshot{}, storage.ErrStaleRevision
	}
	return current, nil
}

func controlTransportView(
	control storage.TaskControlSnapshot,
) transport.TaskControlView {
	return transport.TaskControlView{
		TaskID:    control.TaskID,
		State:     control.TaskState,
		Revision:  control.TaskRevision,
		UpdatedAt: control.UpdatedAt,
	}
}

var _ transport.TaskControlApplication = (*TaskControlService)(nil)
