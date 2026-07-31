package coordinator

import (
	"context"
	"errors"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
	"codeflux.dev/codeflux/internal/storage"
)

type taskProjectionInvalidationStore interface {
	RecordAndPublishSessionProjectionInvalidation(
		context.Context,
		storage.RecordSessionProjectionInvalidation,
		storage.CommittedEventPublisher,
	) (events.SessionEvent, error)
	ReconcileAllSessionProjectionInvalidations(context.Context, storage.CommittedEventPublisher) error
}

// TaskProjectionInvalidationService provides the idempotent post-commit
// boundary used when an older normalized mutation cannot append a session
// event inside its original transaction.
type TaskProjectionInvalidationService struct {
	store     taskProjectionInvalidationStore
	publisher storage.CommittedEventPublisher
}

func NewTaskProjectionInvalidationService(
	store taskProjectionInvalidationStore,
	publisher storage.CommittedEventPublisher,
) (*TaskProjectionInvalidationService, error) {
	if store == nil || publisher == nil {
		return nil, errors.New("task projection invalidation dependencies are required")
	}
	return &TaskProjectionInvalidationService{store: store, publisher: publisher}, nil
}

func (service *TaskProjectionInvalidationService) NotifyTaskProjectionInvalidated(
	ctx context.Context,
	taskID domain.TaskID,
	entity string,
	revision uint64,
	idempotencyKey string,
) error {
	_, err := service.store.RecordAndPublishSessionProjectionInvalidation(
		ctx,
		storage.RecordSessionProjectionInvalidation{
			TaskID: taskID, Entity: entity, EntityRevision: revision,
			IdempotencyKey: idempotencyKey,
		},
		service.publisher,
	)
	// A task may be mutated before its thread has acquired a reconnectable
	// session (for example during local bootstrap). There is no ordered stream
	// to append to yet, so the normalized mutation remains authoritative and a
	// later startup/snapshot reconciliation will backfill the invalidation once
	// the session exists. Do not turn that already-committed mutation into an
	// Internal response merely because the stream has not been bound yet.
	if errors.Is(err, storage.ErrNotFound) {
		return nil
	}
	return err
}

func (service *TaskProjectionInvalidationService) ReconcileStartup(ctx context.Context) error {
	return service.store.ReconcileAllSessionProjectionInvalidations(ctx, service.publisher)
}
