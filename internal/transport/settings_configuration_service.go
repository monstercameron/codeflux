package transport

import (
	"context"
	"errors"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
)

// SettingsConfigurationApplication is the narrow surface the settings service
// needs beyond local telemetry.
//
// It is an interface so the service can be exercised without a database or an
// operating-system credential store, and so a settings page cannot reach past
// what settings legitimately read and write.
type SettingsConfigurationApplication interface {
	// ReadEffectivePolicy resolves the policy in force from compiled defaults
	// and the stored user layer.
	ReadEffectivePolicy(context.Context) (PolicyRecord, error)
	// ListProviderModels reports the providers the coordinator has recorded
	// and the models catalogued against them.
	ListProviderModels(context.Context) ([]ProviderModelRecord, error)
	// BindProviderCredential associates an opaque operating-system credential
	// reference with a provider. It never receives a secret.
	BindProviderCredential(context.Context, BindProviderCredential) (ProviderRecord, error)
	// TestProviderCredential reports whether the coordinator can obtain the
	// credential a provider call would present.
	TestProviderCredential(context.Context, domain.ProviderID) (ProviderCredentialTest, error)
}

// PolicyRecord is the execution policy in force.
//
// Every field of it is fixed for this prototype: routing uses one versioned
// model-and-effort policy through prototype exit, and task intake applies the
// risk and assurance floors reported here. The record exists so a person can
// see what governs their runs, not so a settings page can offer to change it.
type PolicyRecord struct {
	Preset         domain.PolicyPreset
	Reasoning      domain.ReasoningEffort
	RiskFloor      domain.RiskLevel
	AssuranceFloor domain.AssuranceLevel
	// PolicyVersion identifies the frozen baseline the reasoning effort and
	// limits come from. A changed value means a different policy stratum, not
	// a user preference.
	PolicyVersion string
	// RequestTimeout is the effective per-request bound. It is reported
	// alongside models because it is the one effective configuration value a
	// model view can carry truthfully.
	RequestTimeout time.Duration
	// Revision is the settings revision that produced this answer. Zero means
	// no user layer has ever been written and compiled defaults are in force.
	Revision uint64
}

// ProviderModelRecord is one row of the settings model list.
//
// A record with an empty ModelID names a provider rather than a model. The
// list carries both because a provider with no catalogued model is exactly the
// provider a settings page must still show: it is the one that cannot be used
// yet, and omitting it would leave no way to configure it.
type ProviderModelRecord struct {
	ProviderID   domain.ProviderID
	ProviderName string
	ModelID      string
	ModelName    string
	// Available reports whether the coordinator could use this row now: the
	// provider is enabled and a credential is bound to it. It is not a claim
	// that the provider answered.
	Available      bool
	RequestTimeout time.Duration
}

// BindProviderCredential is one attributable credential binding.
//
// CredentialReference is an opaque os://service/account identity naming where
// a credential lives. A credential value never crosses this boundary.
//
// ModelID names the catalogued model the credential is being configured for.
// The request contract requires it, and it is checked against that provider's
// catalogue so a configuration cannot be recorded against a model the
// coordinator has never seen. It does not select the model a run uses: that
// remains the versioned routing policy's decision.
type BindProviderCredential struct {
	ProviderID          domain.ProviderID
	CredentialReference string
	ModelID             string
	IdempotencyKey      string
	ExpectedRevision    *uint64
}

// ProviderRecord is one provider configuration without any secret.
type ProviderRecord struct {
	ID                  domain.ProviderID
	DisplayName         string
	CredentialReference string
	Configured          bool
	Revision            uint64
}

// ProviderCredentialTest is what a credential check established.
//
// Resolved reports only that the coordinator obtained the credential a
// provider call would present. No provider request is made, so it is never
// evidence that the provider answered.
type ProviderCredentialTest struct {
	Resolved bool
	Summary  string
}

// ErrSettingsRevisionConflict reports that the caller's expected revision is
// not the revision in force.
var ErrSettingsRevisionConflict = errors.New("settings revision conflict")

// ErrProviderNotFound reports a provider identity that names no configuration.
var ErrProviderNotFound = errors.New("provider not found")

// GetPolicy returns the execution policy in force.
//
// SetPolicy and SetBudgetDefaults remain unimplemented deliberately. Routing
// is one frozen versioned policy through prototype exit and task creation
// applies the fixed baseline, so a control that stored a different preset
// would change a row nothing reads while telling a person their runs had
// changed.
func (service *SettingsService) GetPolicy(
	ctx context.Context,
	_ *codefluxv1.GetPolicyRequest,
) (*codefluxv1.GetPolicyResponse, error) {
	record, err := service.configuration.ReadEffectivePolicy(ctx)
	if err != nil {
		return nil, mapSettingsError(err, "the policy could not be read")
	}
	return &codefluxv1.GetPolicyResponse{
		Policy: &codefluxv1.PolicyView{
			Preset:            string(record.Preset),
			ReasoningEffort:   string(record.Reasoning),
			Risk:              string(record.RiskFloor),
			RequiredAssurance: string(record.AssuranceFloor),
			Revision:          record.Revision,
		},
	}, nil
}

// GetModels returns the providers and models the coordinator has recorded.
func (service *SettingsService) GetModels(
	ctx context.Context,
	_ *codefluxv1.GetModelsRequest,
) (*codefluxv1.GetModelsResponse, error) {
	records, err := service.configuration.ListProviderModels(ctx)
	if err != nil {
		return nil, mapSettingsError(err, "models could not be listed")
	}
	views := make([]*codefluxv1.ModelView, 0, len(records))
	for _, record := range records {
		identity, identityErr := ProviderIDToProto(record.ProviderID)
		if identityErr != nil {
			// A row whose provider identity cannot be represented cannot be
			// configured or tested by a caller, and drawing it would offer a
			// control that does nothing.
			continue
		}
		name := record.ProviderName
		if record.ModelID != "" {
			name = record.ModelName
		}
		view := &codefluxv1.ModelView{
			ProviderId:  identity,
			ModelId:     record.ModelID,
			DisplayName: settingsRedactedText(name),
			Available:   record.Available,
		}
		if record.RequestTimeout > 0 {
			view.DefaultTimeout = durationpb.New(record.RequestTimeout)
		}
		views = append(views, view)
	}
	return &codefluxv1.GetModelsResponse{Models: views}, nil
}

// ConfigureProvider binds an opaque operating-system credential reference to a
// provider.
//
// The reference names where a credential lives; it is never the credential.
// The model identifier the request contract requires is checked against that
// provider's catalogue rather than stored: the model a run uses is chosen by
// the versioned routing policy, and recording a second choice here would put a
// preference in the database that nothing reads.
func (service *SettingsService) ConfigureProvider(
	ctx context.Context,
	request *codefluxv1.ConfigureProviderRequest,
) (*codefluxv1.ConfigureProviderResponse, error) {
	key, expected, err := settingsMutationControl(request.GetControl())
	if err != nil {
		return nil, err
	}
	providerID, err := ProviderIDFromProto(request.GetProviderId())
	if err != nil {
		return nil, &RequestValidationError{
			Field: "provider_id", Reason: "must be a provider identity",
		}
	}
	if request.GetModelId() == "" {
		return nil, &RequestValidationError{Field: "model_id", Reason: "must not be empty"}
	}
	if request.GetCredentialReference() == "" {
		return nil, &RequestValidationError{
			Field: "credential_reference", Reason: "must not be empty",
		}
	}
	record, err := service.configuration.BindProviderCredential(ctx, BindProviderCredential{
		ProviderID:          providerID,
		CredentialReference: request.GetCredentialReference(),
		ModelID:             request.GetModelId(),
		IdempotencyKey:      key,
		ExpectedRevision:    expected,
	})
	if err != nil {
		return nil, mapSettingsError(err, "the provider could not be configured")
	}
	identity, err := ProviderIDToProto(record.ID)
	if err != nil {
		return nil, status.Error(codes.Internal, "the provider could not be configured")
	}
	return &codefluxv1.ConfigureProviderResponse{
		Provider: &codefluxv1.ProviderView{
			ProviderId:          identity,
			DisplayName:         settingsRedactedText(record.DisplayName),
			CredentialReference: record.CredentialReference,
			Configured:          record.Configured,
			Revision:            record.Revision,
		},
	}, nil
}

// TestProvider reports whether the coordinator can obtain the credential a
// provider call would present.
//
// It makes no provider request. Reaching a provider costs money, crosses the
// network, and is an effect a settings page should not perform on its own, so
// the summary always says plainly that the endpoint's live behavior is
// unverified. The check still answers the question that actually blocks
// people: whether the credential they believe they stored is one the
// coordinator can read.
func (service *SettingsService) TestProvider(
	ctx context.Context,
	request *codefluxv1.TestProviderRequest,
) (*codefluxv1.TestProviderResponse, error) {
	providerID, err := ProviderIDFromProto(request.GetProviderId())
	if err != nil {
		return nil, &RequestValidationError{
			Field: "provider_id", Reason: "must be a provider identity",
		}
	}
	result, err := service.configuration.TestProviderCredential(ctx, providerID)
	if err != nil {
		return nil, mapSettingsError(err, "the provider credential could not be checked")
	}
	return &codefluxv1.TestProviderResponse{
		Reachable: result.Resolved,
		Summary:   settingsRedactedText(result.Summary),
	}, nil
}

// settingsMutationControl requires one retained idempotency key per command and
// carries the caller's expected revision through unchanged.
func settingsMutationControl(
	control *codefluxv1.MutationControl,
) (string, *uint64, error) {
	if control == nil || control.GetIdempotencyKey() == "" {
		return "", nil, &RequestValidationError{
			Field: "control.idempotency_key", Reason: "is required",
		}
	}
	if control.ExpectedRevision == nil {
		return control.GetIdempotencyKey(), nil, nil
	}
	expected := control.GetExpectedRevision()
	return control.GetIdempotencyKey(), &expected, nil
}

// settingsRedactedText wraps display text for the settings boundary.
func settingsRedactedText(value string) *codefluxv1.RedactedText {
	return &codefluxv1.RedactedText{
		Value: value, OriginalBytes: uint64(len(value)),
	}
}

// mapSettingsError maps the application's typed failures onto safe transport
// errors and never returns an underlying message to a caller.
func mapSettingsError(err error, safe string) error {
	var validation *RequestValidationError
	switch {
	case errors.Is(err, ErrSettingsRevisionConflict):
		return status.Error(
			codes.Aborted,
			"the settings changed since they were read; reload and try again",
		)
	case errors.Is(err, ErrProviderNotFound):
		return status.Error(codes.NotFound, "that provider is not configured")
	case errors.As(err, &validation):
		return status.Error(codes.InvalidArgument, validation.Error())
	default:
		return status.Error(codes.Internal, safe)
	}
}
