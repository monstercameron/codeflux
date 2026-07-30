package coordinator

import (
	"context"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/providers"
	"codeflux.dev/codeflux/internal/storage"
)

func TestProviderRecoveryServicePausesBeforeOfferingChoices(t *testing.T) {
	requestID, err := domain.NewModelRequestID()
	if err != nil {
		t.Fatal(err)
	}
	store := &providerRecoveryStoreFixture{
		request: storage.ProviderLogicalRequest{
			ID: requestID, State: storage.ProviderLogicalRequestInFlight,
			AccountingStatus: storage.ProviderAccountingDiscrepant,
			Revision:         3,
		},
	}
	service, err := NewProviderRecoveryService(store)
	if err != nil {
		t.Fatal(err)
	}
	choices, err := service.PauseAfterRetryExhaustion(
		t.Context(), requestID, true,
	)
	if err != nil {
		t.Fatal(err)
	}
	if store.transition.To != storage.ProviderLogicalRequestRetryExhausted ||
		store.transition.ExpectedRevision != 3 ||
		store.transition.AccountingStatus != storage.ProviderAccountingDiscrepant {
		t.Fatalf("transition = %#v", store.transition)
	}
	if len(choices) != 3 ||
		choices[2].Action != providers.RecoverySwitchProvider ||
		!choices[2].RequiresExplicitAuthority {
		t.Fatalf("choices = %#v", choices)
	}
}

func TestProviderRecoveryServiceOffersChoicesForDurableRetryExhaustion(
	t *testing.T,
) {
	requestID, err := domain.NewModelRequestID()
	if err != nil {
		t.Fatal(err)
	}
	store := &providerRecoveryStoreFixture{
		request: storage.ProviderLogicalRequest{
			ID:       requestID,
			State:    storage.ProviderLogicalRequestRetryExhausted,
			Revision: 4,
		},
	}
	service, err := NewProviderRecoveryService(store)
	if err != nil {
		t.Fatal(err)
	}
	choices, err := service.PauseAfterRetryExhaustion(
		t.Context(),
		requestID,
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if store.transition.ID != (domain.ModelRequestID{}) {
		t.Fatalf("already-terminal request was transitioned: %#v", store.transition)
	}
	if len(choices) != 2 ||
		choices[1].Action != providers.RecoverySwitchProvider ||
		!choices[1].RequiresExplicitAuthority {
		t.Fatalf("choices = %#v", choices)
	}
}

type providerRecoveryStoreFixture struct {
	request    storage.ProviderLogicalRequest
	transition storage.TransitionProviderLogicalRequest
}

func (store *providerRecoveryStoreFixture) GetProviderLogicalRequest(
	_ context.Context,
	requestID domain.ModelRequestID,
) (storage.ProviderLogicalRequest, error) {
	if requestID != store.request.ID {
		return storage.ProviderLogicalRequest{}, storage.ErrNotFound
	}
	return store.request, nil
}

func (store *providerRecoveryStoreFixture) TransitionProviderLogicalRequest(
	_ context.Context,
	input storage.TransitionProviderLogicalRequest,
) (storage.ProviderLogicalRequest, error) {
	store.transition = input
	store.request.State = input.To
	store.request.Revision++
	return store.request, nil
}
