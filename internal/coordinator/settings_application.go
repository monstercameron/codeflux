package coordinator

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"codeflux.dev/codeflux/internal/config"
	"codeflux.dev/codeflux/internal/credentials"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/policy"
	"codeflux.dev/codeflux/internal/providers"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
)

// settingsApplication answers the settings service's questions.
//
// Three different authorities meet here. What governs a run is the frozen
// routing policy's answer; which providers exist and what is catalogued
// against them is the database's; and whether a credential can actually be
// obtained is the operating-system store's, and only that store's. Reporting
// the third from a database column is how a settings page comes to say a
// provider is configured about a machine whose credential was deleted.
type settingsApplication struct {
	repositories *storage.Repositories
	credentials  credentials.Store
	// compiledPreset and compiledRequestTimeout are the safe defaults in force
	// when no user settings layer has been written. They are named here rather
	// than in the config package because they are what this coordinator
	// actually does: the preset is the one task intake applies, and the
	// timeout is the one a provider request runs under.
	compiledPreset         domain.PolicyPreset
	compiledRequestTimeout time.Duration
	// flow is the running engine, which adopts a changed configuration without
	// a restart. It is nil when no agent is configured, and the surface still
	// serves: a person can read and change what a run will do before the first
	// run exists.
	flow flowSettingsApplier
}

// newSettingsApplication composes the three authorities.
func newSettingsApplication(
	repositories *storage.Repositories,
	credentialStore credentials.Store,
	flow flowSettingsApplier,
) (settingsApplication, error) {
	if repositories == nil {
		return settingsApplication{}, errors.New("repositories are required")
	}
	if credentialStore == nil {
		return settingsApplication{}, errors.New("a credential store is required")
	}
	return settingsApplication{
		repositories:           repositories,
		credentials:            credentialStore,
		compiledPreset:         domain.PolicyPresetBalanced,
		compiledRequestTimeout: providers.DefaultRequestTimeout,
		flow:                   flow,
	}, nil
}

// ReadEffectivePolicy resolves the policy in force.
//
// Precedence is the fixed one: compiled safe defaults, then the stored user
// layer. Nothing writes that layer through this service — routing is one
// frozen versioned policy through prototype exit — but it is read rather than
// assumed absent, so a layer written by any other authorized path is reported
// instead of being silently overridden by a default.
func (application settingsApplication) ReadEffectivePolicy(
	ctx context.Context,
) (transport.PolicyRecord, error) {
	record := transport.PolicyRecord{
		Preset:    application.compiledPreset,
		Reasoning: policy.FixedBaselineReasoning,
		// A task's risk is classified from its own requirement, so what a
		// settings page can state truthfully is the floor every task starts
		// from, not one value that governs them all.
		RiskFloor:      domain.RiskLevelRoutine,
		AssuranceFloor: domain.AssuranceLevelRuntimeOnly,
		PolicyVersion:  policy.FixedBaselineVersion,
		RequestTimeout: application.compiledRequestTimeout,
	}
	stored, err := application.repositories.GetLatestSettingsRevisionForScope(
		ctx, storage.SettingsScopeUser, nil,
	)
	if errors.Is(err, storage.ErrNotFound) {
		return record, nil
	}
	if err != nil {
		return transport.PolicyRecord{}, err
	}
	var overlay config.Overlay
	if err := json.Unmarshal([]byte(stored.ConfigurationJSON), &overlay); err != nil {
		// A stored layer that cannot be read is not treated as an empty one.
		// Reporting compiled defaults here would tell a person their settings
		// were in force while the row that holds them was unreadable.
		return transport.PolicyRecord{}, err
	}
	record.Revision = stored.Revision
	if overlay.PolicyPreset != nil && overlay.PolicyPreset.IsValid() {
		record.Preset = *overlay.PolicyPreset
	}
	if overlay.RequestTimeout != nil && *overlay.RequestTimeout > 0 {
		record.RequestTimeout = time.Duration(*overlay.RequestTimeout) * time.Millisecond
	}
	return record, nil
}

// ListProviderModels reports the providers and the models catalogued for them.
//
// A provider with no catalogued model still produces a row, because that is
// the provider a settings page most needs to show: it is the one that cannot
// be used yet, and omitting it would leave nothing to configure.
func (application settingsApplication) ListProviderModels(
	ctx context.Context,
) ([]transport.ProviderModelRecord, error) {
	configured, err := application.repositories.ListConfiguredProviders(
		ctx, storage.MaximumProviderPage,
	)
	if err != nil {
		return nil, err
	}
	if len(configured) == 0 {
		return nil, nil
	}
	catalogued, err := application.repositories.ListModelCatalog(
		ctx, storage.MaximumModelCatalogPage,
	)
	if err != nil {
		return nil, err
	}
	timeout := application.compiledRequestTimeout
	if effective, policyErr := application.ReadEffectivePolicy(ctx); policyErr == nil {
		timeout = effective.RequestTimeout
	}
	models := map[string][]storage.CatalogModel{}
	for _, model := range catalogued {
		key := model.ProviderID.String()
		models[key] = append(models[key], model)
	}
	records := make([]transport.ProviderModelRecord, 0, len(configured)+len(catalogued))
	for _, provider := range configured {
		available := provider.Enabled && provider.HasCredential()
		records = append(records, transport.ProviderModelRecord{
			ProviderID:     provider.ID,
			ProviderName:   provider.DisplayName,
			Available:      available,
			RequestTimeout: timeout,
		})
		for _, model := range models[provider.ID.String()] {
			records = append(records, transport.ProviderModelRecord{
				ProviderID:   provider.ID,
				ProviderName: provider.DisplayName,
				ModelID:      model.ID,
				ModelName:    catalogModelName(model),
				// A model is usable only when its provider is, so the provider's
				// answer bounds the model's rather than being reported twice.
				Available:      available,
				RequestTimeout: timeout,
			})
		}
	}
	return records, nil
}

// BindProviderCredential associates an opaque operating-system reference with
// a provider.
func (application settingsApplication) BindProviderCredential(
	ctx context.Context,
	command transport.BindProviderCredential,
) (transport.ProviderRecord, error) {
	provider, err := application.findProvider(ctx, command.ProviderID)
	if err != nil {
		return transport.ProviderRecord{}, err
	}
	if command.ExpectedRevision != nil && *command.ExpectedRevision != provider.Revision {
		return transport.ProviderRecord{}, transport.ErrSettingsRevisionConflict
	}
	if _, err := parseProviderCredentialReference(command.CredentialReference); err != nil {
		// The reference is parsed before it is stored so a malformed one is
		// refused here rather than becoming a provider that looks configured
		// and fails at the first request.
		return transport.ProviderRecord{}, &transport.RequestValidationError{
			Field:  "credential_reference",
			Reason: "must be an os://service/account identity",
		}
	}
	known, err := application.providerHasModel(ctx, command.ProviderID, command.ModelID)
	if err != nil {
		return transport.ProviderRecord{}, err
	}
	if !known {
		// A configuration recorded against a model this coordinator has never
		// catalogued would describe a capability nobody has evidence for.
		return transport.ProviderRecord{}, &transport.RequestValidationError{
			Field:  "model_id",
			Reason: "must name a model catalogued for this provider",
		}
	}
	binding, err := application.repositories.BindProviderCredentialReference(
		ctx,
		storage.BindProviderCredentialReference{
			ProviderID:      command.ProviderID,
			OpaqueReference: command.CredentialReference,
		},
	)
	if err != nil {
		if errors.Is(err, storage.ErrConflict) {
			// Rebinding is refused by the store, which keeps a provider's
			// recorded requests attributable to the credential identity they
			// were made with. Saying so is more useful than an internal error.
			return transport.ProviderRecord{}, &transport.RequestValidationError{
				Field:  "credential_reference",
				Reason: "differs from the reference already bound to this provider, which cannot be replaced yet",
			}
		}
		return transport.ProviderRecord{}, err
	}
	return transport.ProviderRecord{
		ID:                  provider.ID,
		DisplayName:         provider.DisplayName,
		CredentialReference: binding.OpaqueReference,
		Configured:          binding.OpaqueReference != "",
		Revision:            provider.Revision,
	}, nil
}

// TestProviderCredential reports whether the coordinator can obtain the
// credential a provider call would present.
//
// It performs no provider request, and every summary says so. What it
// establishes is the thing that actually blocks people: whether the credential
// they believe they stored is one this machine's credential store will return.
func (application settingsApplication) TestProviderCredential(
	ctx context.Context,
	providerID domain.ProviderID,
) (transport.ProviderCredentialTest, error) {
	if _, err := application.findProvider(ctx, providerID); err != nil {
		return transport.ProviderCredentialTest{}, err
	}
	binding, err := application.repositories.GetProviderCredentialReference(ctx, providerID)
	if errors.Is(err, storage.ErrNotFound) {
		return transport.ProviderCredentialTest{
			Resolved: false,
			Summary: "No operating-system credential is bound to this provider. " +
				"No provider request was made.",
		}, nil
	}
	if err != nil {
		return transport.ProviderCredentialTest{}, err
	}
	reference, err := parseProviderCredentialReference(binding.OpaqueReference)
	if err != nil {
		return transport.ProviderCredentialTest{
			Resolved: false,
			Summary: "The stored credential reference is not a usable os://service/account identity. " +
				"No provider request was made.",
		}, nil
	}
	if err := application.credentials.Test(ctx, reference); err != nil {
		// The store's own message is not returned. It can name accounts,
		// services, and platform detail that a browser has no reason to hold.
		return transport.ProviderCredentialTest{
			Resolved: false,
			Summary: "The operating-system credential store did not return the bound credential. " +
				"No provider request was made.",
		}, nil
	}
	return transport.ProviderCredentialTest{
		Resolved: true,
		Summary: "The bound credential resolved from the operating-system credential store. " +
			"No provider request was made, so the provider's live response is unverified.",
	}, nil
}

// findProvider resolves one provider row or reports the service's absence.
func (application settingsApplication) findProvider(
	ctx context.Context,
	providerID domain.ProviderID,
) (storage.ConfiguredProvider, error) {
	if providerID.IsZero() {
		return storage.ConfiguredProvider{}, transport.ErrProviderNotFound
	}
	configured, err := application.repositories.ListConfiguredProviders(
		ctx, storage.MaximumProviderPage,
	)
	if err != nil {
		return storage.ConfiguredProvider{}, err
	}
	for _, provider := range configured {
		if provider.ID == providerID {
			return provider, nil
		}
	}
	return storage.ConfiguredProvider{}, transport.ErrProviderNotFound
}

// providerHasModel reports whether a model identifier names a model
// catalogued for one provider.
func (application settingsApplication) providerHasModel(
	ctx context.Context,
	providerID domain.ProviderID,
	modelID string,
) (bool, error) {
	catalogued, err := application.repositories.ListModelCatalog(
		ctx, storage.MaximumModelCatalogPage,
	)
	if err != nil {
		return false, err
	}
	for _, model := range catalogued {
		if model.ID == modelID && model.ProviderID == providerID {
			return true, nil
		}
	}
	return false, nil
}

// catalogModelName renders a model with the revision it is bound to.
//
// The revision is part of the name because two rows differing only by it are
// two different models for every purpose this product has, and a list showing
// the same label twice would be unreadable.
func catalogModelName(model storage.CatalogModel) string {
	if model.ModelRevision == "" {
		return model.ModelName
	}
	return model.ModelName + " · " + model.ModelRevision
}

var _ transport.SettingsConfigurationApplication = settingsApplication{}
