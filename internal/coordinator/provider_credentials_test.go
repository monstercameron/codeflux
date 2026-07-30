package coordinator

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"codeflux.dev/codeflux/internal/credentials"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
)

func TestProviderCredentialSourceResolvesOpaqueReferenceInsideCallback(t *testing.T) {
	providerID, err := domain.NewProviderID()
	if err != nil {
		t.Fatal(err)
	}
	reference, err := credentials.NewReference("codeflux-openai", "primary")
	if err != nil {
		t.Fatal(err)
	}
	secret, err := credentials.NewSecret([]byte("provider-fixture-value"))
	if err != nil {
		t.Fatal(err)
	}
	store := &providerCredentialTestStore{
		reference: reference,
		secret:    secret,
	}
	source, err := NewProviderCredentialSource(store, providerCredentialTestReferences{
		providerID: providerID,
		reference:  reference.Opaque(),
	})
	if err != nil {
		t.Fatal(err)
	}
	var observed []byte
	err = source.Use(t.Context(), providerID, func(value []byte) error {
		observed = append([]byte(nil), value...)
		value[0] = 'X'
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(observed, []byte("provider-fixture-value")) {
		t.Fatalf("callback value = %q", observed)
	}
	if store.retrievals != 1 {
		t.Fatalf("credential retrievals = %d", store.retrievals)
	}
}

func TestProviderCredentialSourceRejectsMalformedReferenceAndMissingOperation(t *testing.T) {
	providerID, err := domain.NewProviderID()
	if err != nil {
		t.Fatal(err)
	}
	source, err := NewProviderCredentialSource(
		&providerCredentialTestStore{},
		providerCredentialTestReferences{
			providerID: providerID,
			reference:  "environment://OPENAI_API_KEY",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := source.Use(t.Context(), providerID, func([]byte) error { return nil }); err == nil {
		t.Fatal("malformed reference was accepted")
	}
	if err := source.Use(t.Context(), providerID, nil); err == nil {
		t.Fatal("nil credential operation was accepted")
	}
}

type providerCredentialTestReferences struct {
	providerID domain.ProviderID
	reference  string
}

func (references providerCredentialTestReferences) BindProviderCredentialReference(
	context.Context,
	storage.BindProviderCredentialReference,
) (storage.ProviderCredentialReference, error) {
	return storage.ProviderCredentialReference{}, errors.New("unexpected bind")
}

func (references providerCredentialTestReferences) GetProviderCredentialReference(
	_ context.Context,
	providerID domain.ProviderID,
) (storage.ProviderCredentialReference, error) {
	if providerID != references.providerID {
		return storage.ProviderCredentialReference{}, storage.ErrNotFound
	}
	return storage.ProviderCredentialReference{
		ProviderID:      providerID,
		OpaqueReference: references.reference,
	}, nil
}

type providerCredentialTestStore struct {
	reference  credentials.Reference
	secret     credentials.Secret
	retrievals int
}

func (*providerCredentialTestStore) Create(
	context.Context,
	credentials.Reference,
	credentials.Secret,
) error {
	return errors.New("unexpected create")
}

func (*providerCredentialTestStore) Update(
	context.Context,
	credentials.Reference,
	credentials.Secret,
) error {
	return errors.New("unexpected update")
}

func (store *providerCredentialTestStore) Retrieve(
	_ context.Context,
	reference credentials.Reference,
) (credentials.Secret, error) {
	if reference != store.reference {
		return credentials.Secret{}, credentials.ErrNotFound
	}
	store.retrievals++
	return store.secret, nil
}

func (*providerCredentialTestStore) Test(
	context.Context,
	credentials.Reference,
) error {
	return errors.New("unexpected test")
}

func (*providerCredentialTestStore) Delete(
	context.Context,
	credentials.Reference,
) error {
	return errors.New("unexpected delete")
}
