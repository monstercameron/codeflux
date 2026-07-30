package coordinator

import (
	"context"
	"errors"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/providers"
	"codeflux.dev/codeflux/internal/storage"
)

type providerRecoveryStore interface {
	GetProviderLogicalRequest(
		context.Context,
		domain.ModelRequestID,
	) (storage.ProviderLogicalRequest, error)
	TransitionProviderLogicalRequest(
		context.Context,
		storage.TransitionProviderLogicalRequest,
	) (storage.ProviderLogicalRequest, error)
}

type ProviderRecoveryService struct {
	store providerRecoveryStore
}

func NewProviderRecoveryService(
	store providerRecoveryStore,
) (*ProviderRecoveryService, error) {
	if store == nil {
		return nil, errors.New("provider recovery store is required")
	}
	return &ProviderRecoveryService{store: store}, nil
}

// PauseAfterRetryExhaustion durably stops automatic work before offering any
// retry, resume, or explicitly authorized switch.
func (service *ProviderRecoveryService) PauseAfterRetryExhaustion(
	ctx context.Context,
	requestID domain.ModelRequestID,
	checkpointAvailable bool,
) ([]providers.RecoveryChoice, error) {
	current, err := service.store.GetProviderLogicalRequest(ctx, requestID)
	if err != nil {
		return nil, err
	}
	switch current.State {
	case storage.ProviderLogicalRequestRetryExhausted:
		return providers.RetryExhaustedChoices(checkpointAvailable), nil
	case storage.ProviderLogicalRequestInFlight:
	default:
		return nil, errors.New(
			"provider request is neither in flight nor retry exhausted",
		)
	}
	_, err = service.store.TransitionProviderLogicalRequest(
		ctx,
		storage.TransitionProviderLogicalRequest{
			ID: requestID, ExpectedRevision: current.Revision,
			From: current.State, To: storage.ProviderLogicalRequestRetryExhausted,
			AccountingStatus: current.AccountingStatus,
		},
	)
	if err != nil {
		return nil, err
	}
	return providers.RetryExhaustedChoices(checkpointAvailable), nil
}
