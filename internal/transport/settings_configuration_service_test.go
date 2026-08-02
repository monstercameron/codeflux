package transport

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// settingsConfigurationFake answers the settings service without a database or
// an operating-system credential store.
type settingsConfigurationFake struct {
	policy      PolicyRecord
	policyErr   error
	models      []ProviderModelRecord
	modelsErr   error
	bound       BindProviderCredential
	provider    ProviderRecord
	bindErr     error
	test        ProviderCredentialTest
	testErr     error
	testedAgent domain.ProviderID
	flow        FlowSettingsRecord
	flowErr     error
	written     WriteFlowSettings
	writeErr    error
}

func (fake *settingsConfigurationFake) ReadFlowSettings(
	context.Context,
) (FlowSettingsRecord, error) {
	return fake.flow, fake.flowErr
}

func (fake *settingsConfigurationFake) WriteFlowSettings(
	_ context.Context,
	command WriteFlowSettings,
) (FlowSettingsRecord, error) {
	fake.written = command
	return fake.flow, fake.writeErr
}

func (fake *settingsConfigurationFake) ReadEffectivePolicy(context.Context) (PolicyRecord, error) {
	return fake.policy, fake.policyErr
}

func (fake *settingsConfigurationFake) ListProviderModels(context.Context) ([]ProviderModelRecord, error) {
	return fake.models, fake.modelsErr
}

func (fake *settingsConfigurationFake) BindProviderCredential(
	_ context.Context,
	command BindProviderCredential,
) (ProviderRecord, error) {
	fake.bound = command
	return fake.provider, fake.bindErr
}

func (fake *settingsConfigurationFake) TestProviderCredential(
	_ context.Context,
	providerID domain.ProviderID,
) (ProviderCredentialTest, error) {
	fake.testedAgent = providerID
	return fake.test, fake.testErr
}

func newSettingsServiceForTest(
	t *testing.T,
	configuration *settingsConfigurationFake,
) *SettingsService {
	t.Helper()
	service, err := NewSettingsService(&telemetryApplicationFake{}, configuration)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestTheSettingsServiceRequiresBothOfItsApplications(t *testing.T) {
	if _, err := NewSettingsService(nil, &settingsConfigurationFake{}); err == nil {
		t.Fatal("a settings service without telemetry must not construct")
	}
	// A nil configuration application would leave the policy, provider, and
	// model calls answering Unimplemented while the service reported itself as
	// constructed.
	if _, err := NewSettingsService(&telemetryApplicationFake{}, nil); err == nil {
		t.Fatal("a settings service without a configuration application must not construct")
	}
}

func TestTheSettingsPolicyReportsWhatGovernsARun(t *testing.T) {
	fake := &settingsConfigurationFake{policy: PolicyRecord{
		Preset:         domain.PolicyPresetBalanced,
		Reasoning:      domain.ReasoningEffortMaximum,
		RiskFloor:      domain.RiskLevelRoutine,
		AssuranceFloor: domain.AssuranceLevelRuntimeOnly,
		Revision:       4,
	}}
	response, err := newSettingsServiceForTest(t, fake).GetPolicy(
		t.Context(), &codefluxv1.GetPolicyRequest{},
	)
	if err != nil {
		t.Fatal(err)
	}
	view := response.GetPolicy()
	if view.GetPreset() != string(domain.PolicyPresetBalanced) ||
		view.GetReasoningEffort() != string(domain.ReasoningEffortMaximum) ||
		view.GetRisk() != string(domain.RiskLevelRoutine) ||
		view.GetRequiredAssurance() != string(domain.AssuranceLevelRuntimeOnly) ||
		view.GetRevision() != 4 {
		t.Fatalf("policy view lost a field: %+v", view)
	}
}

func TestAnUnreadablePolicyIsReportedRatherThanGuessed(t *testing.T) {
	fake := &settingsConfigurationFake{policyErr: errors.New("database is closed")}
	_, err := newSettingsServiceForTest(t, fake).GetPolicy(
		t.Context(), &codefluxv1.GetPolicyRequest{},
	)
	if status.Code(err) != codes.Internal {
		t.Fatalf("want Internal, got %v", err)
	}
	if status.Convert(err).Message() == "database is closed" {
		t.Fatal("an internal failure must not return the underlying message")
	}
}

func TestTheModelListNamesProvidersAndTheirModels(t *testing.T) {
	providerID, err := domain.NewProviderID()
	if err != nil {
		t.Fatal(err)
	}
	fake := &settingsConfigurationFake{models: []ProviderModelRecord{
		{
			ProviderID: providerID, ProviderName: "OpenAI",
			Available: false, RequestTimeout: 90 * time.Second,
		},
		{
			ProviderID: providerID, ProviderName: "OpenAI",
			ModelID: "mdl-1", ModelName: "gpt-5.6-sol · 2026-05",
			Available: true, RequestTimeout: 90 * time.Second,
		},
		// A record whose provider identity is empty cannot be configured or
		// tested by a caller, so it must not become a row offering controls
		// that do nothing.
		{ProviderName: "Unnameable", ModelID: "mdl-2", ModelName: "ghost"},
	}}
	response, err := newSettingsServiceForTest(t, fake).GetModels(
		t.Context(), &codefluxv1.GetModelsRequest{},
	)
	if err != nil {
		t.Fatal(err)
	}
	views := response.GetModels()
	if len(views) != 2 {
		t.Fatalf("want two views, got %d", len(views))
	}
	if views[0].GetModelId() != "" || views[0].GetDisplayName().GetValue() != "OpenAI" {
		t.Fatalf("a provider heading must carry no model identifier: %+v", views[0])
	}
	if views[0].GetAvailable() {
		t.Fatal("a provider with no credential must not be reported as available")
	}
	if views[1].GetModelId() != "mdl-1" ||
		views[1].GetDisplayName().GetValue() != "gpt-5.6-sol · 2026-05" ||
		!views[1].GetAvailable() {
		t.Fatalf("a model row lost a field: %+v", views[1])
	}
	if views[1].GetDefaultTimeout().AsDuration() != 90*time.Second {
		t.Fatalf("want the effective request timeout, got %v", views[1].GetDefaultTimeout())
	}
}

func TestConfiguringAProviderCarriesOnlyANonSecretReference(t *testing.T) {
	providerID, err := domain.NewProviderID()
	if err != nil {
		t.Fatal(err)
	}
	identity, err := ProviderIDToProto(providerID)
	if err != nil {
		t.Fatal(err)
	}
	fake := &settingsConfigurationFake{provider: ProviderRecord{
		ID: providerID, DisplayName: "OpenAI",
		CredentialReference: "os://codeflux/openai", Configured: true, Revision: 2,
	}}
	service := newSettingsServiceForTest(t, fake)
	expected := uint64(2)
	response, err := service.ConfigureProvider(t.Context(), &codefluxv1.ConfigureProviderRequest{
		Control: &codefluxv1.MutationControl{
			IdempotencyKey: "configure-1", ExpectedRevision: &expected,
		},
		ProviderId:          identity,
		CredentialReference: "os://codeflux/openai",
		ModelId:             "mdl-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if fake.bound.ProviderID != providerID ||
		fake.bound.CredentialReference != "os://codeflux/openai" ||
		fake.bound.ModelID != "mdl-1" ||
		fake.bound.IdempotencyKey != "configure-1" ||
		fake.bound.ExpectedRevision == nil || *fake.bound.ExpectedRevision != 2 {
		t.Fatalf("the command lost a field: %+v", fake.bound)
	}
	view := response.GetProvider()
	if !view.GetConfigured() || view.GetCredentialReference() != "os://codeflux/openai" ||
		view.GetDisplayName().GetValue() != "OpenAI" || view.GetRevision() != 2 {
		t.Fatalf("provider view lost a field: %+v", view)
	}
}

func TestConfiguringAProviderRefusesWhatItCannotHonour(t *testing.T) {
	providerID, err := domain.NewProviderID()
	if err != nil {
		t.Fatal(err)
	}
	identity, err := ProviderIDToProto(providerID)
	if err != nil {
		t.Fatal(err)
	}
	service := newSettingsServiceForTest(t, &settingsConfigurationFake{})

	// Without a retained idempotency key a repeated command cannot be
	// recognized as the same command.
	if _, err := service.ConfigureProvider(t.Context(), &codefluxv1.ConfigureProviderRequest{
		ProviderId: identity, CredentialReference: "os://codeflux/openai", ModelId: "mdl-1",
	}); err == nil {
		t.Fatal("a configure command without an idempotency key must be refused")
	}

	// The model the credential is configured for is part of the request
	// contract, so an omitted one is refused rather than defaulted.
	if _, err := service.ConfigureProvider(t.Context(), &codefluxv1.ConfigureProviderRequest{
		Control:             &codefluxv1.MutationControl{IdempotencyKey: "configure-2"},
		ProviderId:          identity,
		CredentialReference: "os://codeflux/openai",
	}); err == nil {
		t.Fatal("a configure command without a model identifier must be refused")
	}

	// A provider configured with no credential reference would look configured
	// and fail at the first request.
	if _, err := service.ConfigureProvider(t.Context(), &codefluxv1.ConfigureProviderRequest{
		Control:    &codefluxv1.MutationControl{IdempotencyKey: "configure-3"},
		ProviderId: identity, ModelId: "mdl-1",
	}); err == nil {
		t.Fatal("a configure command without a credential reference must be refused")
	}

	// An identity of the wrong kind names nothing this service can configure.
	if _, err := service.ConfigureProvider(t.Context(), &codefluxv1.ConfigureProviderRequest{
		Control:             &codefluxv1.MutationControl{IdempotencyKey: "configure-4"},
		ProviderId:          &codefluxv1.StableIdentity{Kind: codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_TASK, Value: "tsk_1"},
		CredentialReference: "os://codeflux/openai",
		ModelId:             "mdl-1",
	}); err == nil {
		t.Fatal("a non-provider identity must be refused")
	}
}

func TestARevisionConflictAsksForAReloadRatherThanOverwriting(t *testing.T) {
	providerID, err := domain.NewProviderID()
	if err != nil {
		t.Fatal(err)
	}
	identity, err := ProviderIDToProto(providerID)
	if err != nil {
		t.Fatal(err)
	}
	fake := &settingsConfigurationFake{bindErr: ErrSettingsRevisionConflict}
	_, err = newSettingsServiceForTest(t, fake).ConfigureProvider(
		t.Context(),
		&codefluxv1.ConfigureProviderRequest{
			Control:             &codefluxv1.MutationControl{IdempotencyKey: "configure-5"},
			ProviderId:          identity,
			CredentialReference: "os://codeflux/openai",
			ModelId:             "mdl-1",
		},
	)
	if status.Code(err) != codes.Aborted {
		t.Fatalf("want Aborted, got %v", err)
	}
}

func TestTestingAProviderReportsACredentialCheckAndSaysSo(t *testing.T) {
	providerID, err := domain.NewProviderID()
	if err != nil {
		t.Fatal(err)
	}
	identity, err := ProviderIDToProto(providerID)
	if err != nil {
		t.Fatal(err)
	}
	fake := &settingsConfigurationFake{test: ProviderCredentialTest{
		Resolved: true,
		Summary: "The bound credential resolved from the operating-system credential store. " +
			"No provider request was made, so the provider's live response is unverified.",
	}}
	response, err := newSettingsServiceForTest(t, fake).TestProvider(
		t.Context(), &codefluxv1.TestProviderRequest{ProviderId: identity},
	)
	if err != nil {
		t.Fatal(err)
	}
	if fake.testedAgent != providerID {
		t.Fatal("the tested provider identity was not carried through")
	}
	if !response.GetReachable() {
		t.Fatal("a resolved credential must be reported as resolved")
	}
	if response.GetSummary().GetValue() != fake.test.Summary {
		t.Fatalf("the summary was rewritten: %q", response.GetSummary().GetValue())
	}
}

func TestTestingAnUnknownProviderIsNotFoundRatherThanAFailure(t *testing.T) {
	providerID, err := domain.NewProviderID()
	if err != nil {
		t.Fatal(err)
	}
	identity, err := ProviderIDToProto(providerID)
	if err != nil {
		t.Fatal(err)
	}
	fake := &settingsConfigurationFake{testErr: ErrProviderNotFound}
	_, err = newSettingsServiceForTest(t, fake).TestProvider(
		t.Context(), &codefluxv1.TestProviderRequest{ProviderId: identity},
	)
	if status.Code(err) != codes.NotFound {
		t.Fatalf("want NotFound, got %v", err)
	}
}

func TestACallerCausedSettingsFailureIsNotReportedAsInternal(t *testing.T) {
	providerID, err := domain.NewProviderID()
	if err != nil {
		t.Fatal(err)
	}
	identity, err := ProviderIDToProto(providerID)
	if err != nil {
		t.Fatal(err)
	}
	fake := &settingsConfigurationFake{bindErr: &RequestValidationError{
		Field: "credential_reference", Reason: "must be an os://service/account identity",
	}}
	_, err = newSettingsServiceForTest(t, fake).ConfigureProvider(
		t.Context(),
		&codefluxv1.ConfigureProviderRequest{
			Control:             &codefluxv1.MutationControl{IdempotencyKey: "configure-6"},
			ProviderId:          identity,
			CredentialReference: "not-a-reference",
			ModelId:             "mdl-1",
		},
	)
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("want InvalidArgument, got %v", err)
	}
}

func TestTheRunSettingsCarryTheirDescriptionToTheClient(t *testing.T) {
	fake := &settingsConfigurationFake{flow: FlowSettingsRecord{
		Revision: 7,
		Settings: []FlowSetting{
			{
				Key: "ambiguity", Label: "When a request reads two ways",
				Help: "Ask stops and puts the question to you.",
				Kind: "choice", Choices: []string{"ask", "assume"},
				Group: "Intake", Text: "ask",
			},
			{
				Key: "maximum_attempts", Label: "Attempts before stopping",
				Kind: "number", Minimum: 1, Maximum: 12, Number: 6,
				Group: "Refinement",
			},
		},
	}}
	response, err := newSettingsServiceForTest(t, fake).GetFlowSettings(
		t.Context(), &codefluxv1.GetFlowSettingsRequest{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if response.GetRevision() != 7 || len(response.GetSettings()) != 2 {
		t.Fatalf("response lost a field: %+v", response)
	}
	choice := response.GetSettings()[0]
	if choice.GetKind() != "choice" || len(choice.GetChoices()) != 2 ||
		choice.GetTextValue() != "ask" || choice.GetGroup() != "Intake" ||
		choice.GetHelp().GetValue() == "" {
		t.Fatalf("the choice lost a field: %+v", choice)
	}
	// The bound travels with the value: a client drawing a control from its own
	// copy would eventually offer a number the engine refuses.
	number := response.GetSettings()[1]
	if number.GetMinimum() != 1 || number.GetMaximum() != 12 || number.GetNumberValue() != 6 {
		t.Fatalf("the bound lost a field: %+v", number)
	}
}

func TestChangingRunSettingsCarriesEveryChangeAndItsControl(t *testing.T) {
	fake := &settingsConfigurationFake{flow: FlowSettingsRecord{Revision: 8}}
	service := newSettingsServiceForTest(t, fake)
	expected := uint64(7)
	response, err := service.SetFlowSettings(t.Context(), &codefluxv1.SetFlowSettingsRequest{
		Control: &codefluxv1.MutationControl{
			IdempotencyKey: "flow-1", ExpectedRevision: &expected,
		},
		Changes: []*codefluxv1.FlowSettingChange{
			{Key: "ambiguity", TextValue: "assume"},
			{Key: "maximum_attempts", NumberValue: 9},
			{Key: "adversarial_review", SwitchValue: false},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.GetRevision() != 8 {
		t.Fatalf("revision = %d", response.GetRevision())
	}
	if fake.written.IdempotencyKey != "flow-1" ||
		fake.written.ExpectedRevision == nil || *fake.written.ExpectedRevision != 7 ||
		len(fake.written.Changes) != 3 {
		t.Fatalf("the command lost a field: %+v", fake.written)
	}
	if fake.written.Changes[0].Text != "assume" || fake.written.Changes[1].Number != 9 {
		t.Fatalf("the changes lost a value: %+v", fake.written.Changes)
	}
}

func TestChangingRunSettingsRefusesACommandItCannotAttribute(t *testing.T) {
	service := newSettingsServiceForTest(t, &settingsConfigurationFake{})

	// Without a retained idempotency key a repeated command cannot be
	// recognized as the same command.
	if _, err := service.SetFlowSettings(t.Context(), &codefluxv1.SetFlowSettingsRequest{
		Changes: []*codefluxv1.FlowSettingChange{{Key: "ambiguity", TextValue: "ask"}},
	}); err == nil {
		t.Fatal("a change without an idempotency key must be refused")
	}
	// A command carrying no change would write a revision that changed nothing.
	if _, err := service.SetFlowSettings(t.Context(), &codefluxv1.SetFlowSettingsRequest{
		Control: &codefluxv1.MutationControl{IdempotencyKey: "flow-empty"},
	}); err == nil {
		t.Fatal("a change list with nothing in it must be refused")
	}
	// A change naming no setting cannot be applied to anything.
	if _, err := service.SetFlowSettings(t.Context(), &codefluxv1.SetFlowSettingsRequest{
		Control: &codefluxv1.MutationControl{IdempotencyKey: "flow-keyless"},
		Changes: []*codefluxv1.FlowSettingChange{{NumberValue: 3}},
	}); err == nil {
		t.Fatal("a change with no key must be refused")
	}
}

func TestARefusedRunSettingReachesTheCallerAsTheirMistake(t *testing.T) {
	fake := &settingsConfigurationFake{writeErr: &RequestValidationError{
		Field: "changes", Reason: "Attempts before stopping is 400, outside 1 to 12",
	}}
	_, err := newSettingsServiceForTest(t, fake).SetFlowSettings(
		t.Context(),
		&codefluxv1.SetFlowSettingsRequest{
			Control: &codefluxv1.MutationControl{IdempotencyKey: "flow-bad"},
			Changes: []*codefluxv1.FlowSettingChange{{Key: "maximum_attempts", NumberValue: 400}},
		},
	)
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("want InvalidArgument, got %v", err)
	}
	// The engine's own words name the setting and the bound, which is what
	// somebody who typed the value needs to read.
	if !strings.Contains(status.Convert(err).Message(), "outside 1 to 12") {
		t.Fatalf("message = %q", status.Convert(err).Message())
	}
}
