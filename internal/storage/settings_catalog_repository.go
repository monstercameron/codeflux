package storage

import (
	"context"
	"database/sql"

	"codeflux.dev/codeflux/internal/domain"
)

const (
	// MaximumProviderPage bounds one page of provider configurations.
	//
	// A settings page lists what a person can choose between, and a person
	// configures a handful of providers, not a directory of them. The bound
	// exists so a database that somehow holds more cannot turn one settings
	// render into an unbounded read.
	MaximumProviderPage = 100
	// MaximumModelCatalogPage bounds one page of catalogued models.
	//
	// One provider can answer with a long model list, so this bound is larger
	// than the provider bound and still finite.
	MaximumModelCatalogPage = 500
)

// ConfiguredProvider is one provider row and whether a credential is bound to
// it.
//
// The credential itself is never part of this record. What the settings
// surface needs to know is whether the coordinator has something to
// authenticate with, which the presence of an opaque operating-system
// reference answers without reading a secret.
type ConfiguredProvider struct {
	ID                  domain.ProviderID
	DisplayName         string
	ProviderType        string
	Enabled             bool
	CredentialReference string
	Revision            uint64
}

// HasCredential reports whether an operating-system credential reference is
// bound to this provider.
func (provider ConfiguredProvider) HasCredential() bool {
	return provider.CredentialReference != ""
}

// CatalogModel is one model the coordinator has recorded for a provider.
//
// Capabilities are recorded evidence rather than assumptions: a model is
// listed with what the catalog says it can do, and a field the catalog does
// not record is not invented here.
type CatalogModel struct {
	ID                  string
	ProviderID          domain.ProviderID
	ModelName           string
	ModelRevision       string
	ContextTokens       uint64
	MaximumOutputTokens uint64
	ToolCalling         bool
	StructuredOutput    bool
	Streaming           bool
	Reasoning           bool
	Revision            uint64
}

// ListConfiguredProviders returns the providers a settings surface can show.
//
// Ordering is by display name so a returning person finds the same row in the
// same place. The credential reference is read with a left join because a
// provider without one is exactly the row a settings page most needs to show:
// it is the one that cannot be used yet.
func (repositories *Repositories) ListConfiguredProviders(
	ctx context.Context,
	limit int,
) ([]ConfiguredProvider, error) {
	if limit <= 0 || limit > MaximumProviderPage {
		limit = MaximumProviderPage
	}
	rows, err := repositories.database.sql.QueryContext(
		ctx,
		`SELECT providers.id, providers.display_name, providers.provider_type,
		        providers.enabled, providers.revision,
		        provider_credential_references.opaque_reference
		 FROM providers
		 LEFT JOIN provider_credential_references
		   ON provider_credential_references.provider_id = providers.id
		 ORDER BY providers.display_name, providers.id
		 LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, classify("list configured providers", err)
	}
	defer rows.Close()

	listed := make([]ConfiguredProvider, 0, limit)
	for rows.Next() {
		var (
			provider  ConfiguredProvider
			enabled   int64
			reference sql.NullString
		)
		if err := rows.Scan(
			&provider.ID, &provider.DisplayName, &provider.ProviderType,
			&enabled, &provider.Revision, &reference,
		); err != nil {
			return nil, classify("list configured providers", err)
		}
		provider.Enabled = enabled != 0
		provider.CredentialReference = reference.String
		listed = append(listed, provider)
	}
	if err := rows.Err(); err != nil {
		return nil, classify("list configured providers", err)
	}
	return listed, nil
}

// ListModelCatalog returns the models recorded against configured providers.
//
// Ordering is by provider then model so the settings surface can group without
// sorting, and so two renders of an unchanged catalog are identical.
func (repositories *Repositories) ListModelCatalog(
	ctx context.Context,
	limit int,
) ([]CatalogModel, error) {
	if limit <= 0 || limit > MaximumModelCatalogPage {
		limit = MaximumModelCatalogPage
	}
	rows, err := repositories.database.sql.QueryContext(
		ctx,
		`SELECT model_catalog.id, model_catalog.provider_id,
		        model_catalog.model_name, model_catalog.model_revision,
		        model_catalog.context_tokens, model_catalog.maximum_output_tokens,
		        model_catalog.tool_calling, model_catalog.structured_output,
		        model_catalog.streaming, model_catalog.reasoning,
		        model_catalog.revision
		 FROM model_catalog
		 JOIN providers ON providers.id = model_catalog.provider_id
		 ORDER BY providers.display_name, model_catalog.model_name,
		          model_catalog.model_revision, model_catalog.id
		 LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, classify("list model catalog", err)
	}
	defer rows.Close()

	listed := make([]CatalogModel, 0, limit)
	for rows.Next() {
		var (
			model            CatalogModel
			toolCalling      int64
			structuredOutput int64
			streaming        int64
			reasoning        int64
		)
		if err := rows.Scan(
			&model.ID, &model.ProviderID, &model.ModelName, &model.ModelRevision,
			&model.ContextTokens, &model.MaximumOutputTokens,
			&toolCalling, &structuredOutput, &streaming, &reasoning,
			&model.Revision,
		); err != nil {
			return nil, classify("list model catalog", err)
		}
		model.ToolCalling = toolCalling != 0
		model.StructuredOutput = structuredOutput != 0
		model.Streaming = streaming != 0
		model.Reasoning = reasoning != 0
		listed = append(listed, model)
	}
	if err := rows.Err(); err != nil {
		return nil, classify("list model catalog", err)
	}
	return listed, nil
}
