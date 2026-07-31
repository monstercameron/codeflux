package coordinator

import (
	"context"
	"errors"

	"codeflux.dev/codeflux/internal/frontendtelemetry"
)

// FrontendTelemetryService owns the local, content-free UX telemetry use
// cases. The storage contract performs validation and bounded retention.
type FrontendTelemetryService struct {
	store frontendtelemetry.Store
}

func NewFrontendTelemetryService(store frontendtelemetry.Store) (*FrontendTelemetryService, error) {
	if store == nil {
		return nil, errors.New("frontend telemetry store is required")
	}
	return &FrontendTelemetryService{store: store}, nil
}

func (service *FrontendTelemetryService) RecordFrontendTelemetry(
	ctx context.Context,
	event frontendtelemetry.Event,
) (frontendtelemetry.Event, error) {
	return service.store.RecordFrontendTelemetry(ctx, event)
}

func (service *FrontendTelemetryService) ListFrontendTelemetry(
	ctx context.Context,
	query frontendtelemetry.Query,
) (frontendtelemetry.Page, error) {
	return service.store.ListFrontendTelemetry(ctx, query)
}

func (service *FrontendTelemetryService) DeleteFrontendTelemetry(
	ctx context.Context,
	request frontendtelemetry.DeleteRequest,
) (frontendtelemetry.DeleteResult, error) {
	return service.store.DeleteFrontendTelemetry(ctx, request)
}
