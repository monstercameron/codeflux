package coordinator

import (
	"context"
	"errors"
	"strings"

	"codeflux.dev/codeflux/internal/credentials"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
)

// ProviderCredentialSource resolves non-secret database references and exposes
// credential bytes only inside a bounded callback.
type ProviderCredentialSource struct {
	store      credentials.Store
	references storage.ProviderCredentialOperations
}

// NewProviderCredentialSource keeps credential lookup at the coordinator
// boundary. Provider adapters receive only the temporary callback bytes.
func NewProviderCredentialSource(
	store credentials.Store,
	references storage.ProviderCredentialOperations,
) (*ProviderCredentialSource, error) {
	if store == nil || references == nil {
		return nil, errors.New("provider credential dependencies are required")
	}
	return &ProviderCredentialSource{store: store, references: references}, nil
}

// Use resolves the provider's opaque OS-store reference and clears both the
// retrieved secret and callback copy after the operation returns.
func (source *ProviderCredentialSource) Use(
	ctx context.Context,
	providerID domain.ProviderID,
	operation func([]byte) error,
) error {
	if source == nil || source.store == nil || source.references == nil {
		return errors.New("provider credential source is unavailable")
	}
	if providerID.IsZero() {
		return errors.New("provider ID must not be empty")
	}
	if operation == nil {
		return errors.New("credential operation must not be nil")
	}
	binding, err := source.references.GetProviderCredentialReference(ctx, providerID)
	if err != nil {
		return err
	}
	reference, err := parseProviderCredentialReference(binding.OpaqueReference)
	if err != nil {
		return err
	}
	secret, err := source.store.Retrieve(ctx, reference)
	if err != nil {
		return err
	}
	defer secret.Destroy()
	return secret.Use(operation)
}

func parseProviderCredentialReference(value string) (credentials.Reference, error) {
	if !strings.HasPrefix(value, "os://") {
		return credentials.Reference{}, errors.New("provider credential reference must use os://")
	}
	parts := strings.Split(strings.TrimPrefix(value, "os://"), "/")
	if len(parts) != 2 {
		return credentials.Reference{}, errors.New("provider credential reference is invalid")
	}
	return credentials.NewReference(parts[0], parts[1])
}
