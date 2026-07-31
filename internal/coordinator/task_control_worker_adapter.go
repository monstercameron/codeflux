package coordinator

import (
	"errors"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/worker"
)

// WorkerControlQueue is implemented by Supervisor and WorkerGateway.
type WorkerControlQueue interface {
	QueueControl(domain.RunID, worker.Control) error
}

// TaskControlWorkerAdapter maps typed pause/resume/cancel operations to the
// existing authenticated worker control protocol.
type TaskControlWorkerAdapter struct {
	queue WorkerControlQueue
}

func NewTaskControlWorkerAdapter(
	queue WorkerControlQueue,
) (*TaskControlWorkerAdapter, error) {
	if queue == nil {
		return nil, errors.New("worker control queue is required")
	}
	return &TaskControlWorkerAdapter{queue: queue}, nil
}

func (adapter *TaskControlWorkerAdapter) PauseRun(
	runID domain.RunID,
	reason string,
) error {
	return adapter.queue.QueueControl(
		runID,
		worker.Control{Kind: worker.ControlPause, Reason: reason},
	)
}

func (adapter *TaskControlWorkerAdapter) ResumeRun(
	runID domain.RunID,
	reason string,
) error {
	return adapter.queue.QueueControl(
		runID,
		worker.Control{Kind: worker.ControlResume, Reason: reason},
	)
}

func (adapter *TaskControlWorkerAdapter) CancelRun(
	runID domain.RunID,
	reason string,
) error {
	return adapter.queue.QueueControl(
		runID,
		worker.Control{Kind: worker.ControlCancel, Reason: reason},
	)
}

var _ TaskControlWorkers = (*TaskControlWorkerAdapter)(nil)
