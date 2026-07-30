package storage

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

func (repositories *Repositories) BindProviderCredentialReference(
	ctx context.Context,
	input BindProviderCredentialReference,
) (ProviderCredentialReference, error) {
	if input.ProviderID.IsZero() {
		return ProviderCredentialReference{}, errors.New("provider ID must not be empty")
	}
	if err := validateOpaqueCredentialReference(input.OpaqueReference); err != nil {
		return ProviderCredentialReference{}, err
	}
	now, micros := repositories.timestamp()
	now = time.UnixMicro(micros).UTC()
	var binding ProviderCredentialReference
	err := repositories.database.RunInTransaction(ctx, func(transaction *Transaction) error {
		existing, found, err := findProviderCredentialReference(
			ctx,
			transaction.sql,
			input.ProviderID,
		)
		if err != nil {
			return err
		}
		if found {
			if existing.OpaqueReference != input.OpaqueReference {
				return typedError(
					ErrConflict,
					"bind provider credential reference",
					errors.New("provider already has a different credential reference"),
				)
			}
			binding = existing
			return nil
		}
		if _, err := transaction.sql.ExecContext(
			ctx,
			`INSERT INTO provider_credential_references (
				provider_id, opaque_reference, created_at_unix_micros,
				updated_at_unix_micros
			) VALUES (?, ?, ?, ?)`,
			input.ProviderID,
			input.OpaqueReference,
			micros,
			micros,
		); err != nil {
			return repositoryWriteError("bind provider credential reference", err)
		}
		binding = ProviderCredentialReference{
			ProviderID:      input.ProviderID,
			OpaqueReference: input.OpaqueReference,
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		return nil
	})
	return binding, err
}

func (repositories *Repositories) GetProviderCredentialReference(
	ctx context.Context,
	providerID domain.ProviderID,
) (ProviderCredentialReference, error) {
	if providerID.IsZero() {
		return ProviderCredentialReference{}, errors.New("provider ID must not be empty")
	}
	binding, found, err := findProviderCredentialReference(
		ctx,
		repositories.database.sql,
		providerID,
	)
	if err != nil {
		return ProviderCredentialReference{}, err
	}
	if !found {
		return ProviderCredentialReference{}, typedError(
			ErrNotFound,
			"get provider credential reference",
			sql.ErrNoRows,
		)
	}
	return binding, nil
}

func validateOpaqueCredentialReference(value string) error {
	if len(value) < 8 ||
		len(value) > 512 ||
		!strings.HasPrefix(value, "os://") ||
		strings.Count(strings.TrimPrefix(value, "os://"), "/") != 1 ||
		strings.ContainsAny(value, "\x00\r\n") {
		return errors.New("opaque credential reference is invalid")
	}
	parts := strings.Split(strings.TrimPrefix(value, "os://"), "/")
	if len(parts) != 2 ||
		strings.TrimSpace(parts[0]) == "" ||
		strings.TrimSpace(parts[1]) == "" {
		return errors.New("opaque credential reference is invalid")
	}
	return nil
}

func findProviderCredentialReference(
	ctx context.Context,
	queries queryRower,
	providerID domain.ProviderID,
) (ProviderCredentialReference, bool, error) {
	var (
		binding       ProviderCredentialReference
		createdMicros int64
		updatedMicros int64
	)
	if err := queries.QueryRowContext(
		ctx,
		`SELECT provider_id, opaque_reference, created_at_unix_micros,
		        updated_at_unix_micros, revision
		 FROM provider_credential_references
		 WHERE provider_id = ?`,
		providerID,
	).Scan(
		&binding.ProviderID,
		&binding.OpaqueReference,
		&createdMicros,
		&updatedMicros,
		&binding.Revision,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ProviderCredentialReference{}, false, nil
		}
		return ProviderCredentialReference{}, false, classify(
			"find provider credential reference",
			err,
		)
	}
	binding.CreatedAt = time.UnixMicro(createdMicros).UTC()
	binding.UpdatedAt = time.UnixMicro(updatedMicros).UTC()
	return binding, true, nil
}
