package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

// EnsureProviderRegistration describes the provider a coordinator is about to
// send work to.
//
// Everything a request must be attributable to — which provider, configured
// how, priced at what — has to exist before the first request is planned. The
// accounting tables could each be written individually, and the tests did
// exactly that with raw SQL, but nothing in the product ever assembled them.
// So a run could call a model and then had nowhere to record that it had.
type EnsureProviderRegistration struct {
	DisplayName      string
	ProviderType     string
	AdapterName      string
	AdapterVersion   string
	ProviderVersion  string
	EndpointRedacted string
	CapabilitiesJSON string
	ModelIdentifier  string
	ModelVersion     string
	// Currency and Prices are the operator's rates. They are supplied rather
	// than discovered: a price nobody stated is not a price, and guessing one
	// would put an invented number into every budget the product enforces.
	//
	// Declaring no rate at all is allowed and is recorded as pricing that is
	// not known, which is a true statement. A zero rate would be a false one.
	Currency domain.CurrencyCode
	Prices   []ProviderPriceComponent
}

// RegisteredProvider is what a planned request needs to name.
type RegisteredProvider struct {
	ProviderID              domain.ProviderID
	ConfigurationRevisionID string
	PricingRevisionID       string
}

// EnsureProviderRegistration registers a provider, its configuration, and its
// prices, and is safe to call on every start.
//
// It is idempotent on the provider's type and display name, and on the content
// of the configuration and the prices: calling it again with the same facts
// returns the same identities rather than accumulating a new revision per boot,
// which would make the accounting history unreadable.
func (repositories *Repositories) EnsureProviderRegistration(
	ctx context.Context,
	input EnsureProviderRegistration,
) (RegisteredProvider, error) {
	if repositories == nil || repositories.database == nil {
		return RegisteredProvider{}, errors.New("repositories are unavailable")
	}
	if err := validateEnsureProviderRegistration(input); err != nil {
		return RegisteredProvider{}, err
	}

	providerID, err := repositories.ensureProviderRow(ctx, input)
	if err != nil {
		return RegisteredProvider{}, err
	}

	// The configuration and the prices are identified by what they say, so the
	// same declaration always resolves to the same revision and a changed one
	// always resolves to a new revision that the old requests still point at.
	configurationDigest := providerDigest(
		input.ProviderType, input.DisplayName, input.AdapterName,
		input.AdapterVersion, input.ProviderVersion, input.EndpointRedacted,
		input.CapabilitiesJSON,
	)
	configurationID := "provider-configuration-" + configurationDigest[:32]
	if _, err := repositories.CreateProviderConfigurationRevision(
		ctx, CreateProviderConfigurationRevision{
			ID: configurationID, ProviderID: providerID,
			ExpectedLatestRevision: 0,
			AdapterName:            input.AdapterName,
			AdapterVersion:         input.AdapterVersion,
			ProviderVersion:        input.ProviderVersion,
			EndpointRedacted:       input.EndpointRedacted,
			CapabilitiesJSON:       input.CapabilitiesJSON,
			ContentSHA256:          configurationDigest,
			IdempotencyKey:         "provider-configuration:" + configurationDigest[:32],
		},
	); err != nil {
		return RegisteredProvider{}, fmt.Errorf(
			"register the provider configuration: %w", err)
	}

	pricingID := "provider-pricing-" + providerPricingDigest(input)[:32]
	known := len(input.Prices) > 0
	var currency *domain.CurrencyCode
	if known {
		declared := input.Currency
		currency = &declared
	}
	if _, err := repositories.CreateProviderPricingRevision(
		ctx, CreateProviderPricingRevision{
			ID: pricingID, ProviderID: providerID,
			ModelIdentifier: input.ModelIdentifier,
			ModelVersion:    input.ModelVersion,
			PricingKnown:    known,
			Currency:        currency,
			// The rates are effective from the epoch rather than from now: a
			// time that moves every boot would make the same declared price a
			// different revision on every start.
			EffectiveAt: time.Unix(0, 0).UTC(),
			Components:  input.Prices,
		},
	); err != nil {
		return RegisteredProvider{}, fmt.Errorf(
			"register the provider prices: %w", err)
	}

	return RegisteredProvider{
		ProviderID:              providerID,
		ConfigurationRevisionID: configurationID,
		PricingRevisionID:       pricingID,
	}, nil
}

// ensureProviderRow finds the provider or creates it.
//
// A provider is identified by its type and display name, which is the pair the
// schema already declares unique. Minting a new identity per call would leave
// every previous request pointing at a provider nothing else referred to.
func (repositories *Repositories) ensureProviderRow(
	ctx context.Context,
	input EnsureProviderRegistration,
) (domain.ProviderID, error) {
	var providerID domain.ProviderID
	err := repositories.database.RunInTransaction(
		ctx,
		func(transaction *Transaction) error {
			err := transaction.sql.QueryRowContext(
				ctx,
				`SELECT id FROM providers
				 WHERE provider_type = ? AND display_name = ?`,
				input.ProviderType, input.DisplayName,
			).Scan(&providerID)
			if err == nil {
				return nil
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return classify("find provider", err)
			}
			created, err := domain.NewProviderID()
			if err != nil {
				return err
			}
			_, micros := repositories.timestamp()
			if _, err := transaction.sql.ExecContext(
				ctx,
				`INSERT INTO providers (
					id, display_name, provider_type, enabled,
					created_at_unix_micros, updated_at_unix_micros
				) VALUES (?, ?, ?, 1, ?, ?)`,
				created, input.DisplayName, input.ProviderType, micros, micros,
			); err != nil {
				return repositoryWriteError("register provider", err)
			}
			providerID = created
			return nil
		},
	)
	if err != nil {
		return domain.ProviderID{}, err
	}
	return providerID, nil
}

// providerDigest hashes one declaration into a stable identity.
func providerDigest(values ...string) string {
	hash := sha256.New()
	for _, value := range values {
		// The separator is a byte no identifier may contain, so that two
		// different declarations cannot hash to the same run of characters.
		_, _ = hash.Write([]byte(value))
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// providerPricingDigest hashes the declared rates.
func providerPricingDigest(input EnsureProviderRegistration) string {
	values := []string{
		input.ProviderType, input.DisplayName,
		input.ModelIdentifier, input.ModelVersion, string(input.Currency),
	}
	for _, price := range input.Prices {
		specific := ""
		if price.ProviderSpecificKind != nil {
			specific = *price.ProviderSpecificKind
		}
		values = append(values, fmt.Sprintf("%s|%s|%d|%d",
			price.UsageKind, specific,
			price.MinorNumerator, price.TokenDenominator))
	}
	return providerDigest(values...)
}

// validateEnsureProviderRegistration refuses an incomplete declaration.
func validateEnsureProviderRegistration(input EnsureProviderRegistration) error {
	for name, value := range map[string]string{
		"provider display name": input.DisplayName,
		"provider type":         input.ProviderType,
		"adapter name":          input.AdapterName,
		"adapter version":       input.AdapterVersion,
		"provider version":      input.ProviderVersion,
		"provider endpoint":     input.EndpointRedacted,
		"provider capabilities": input.CapabilitiesJSON,
		"model identifier":      input.ModelIdentifier,
		"model version":         input.ModelVersion,
	} {
		if err := validateBounded(name, value, 255); err != nil {
			return err
		}
	}
	if len(input.Prices) == 0 {
		return nil
	}
	if _, err := domain.ParseCurrencyCode(string(input.Currency)); err != nil {
		return err
	}
	return nil
}
