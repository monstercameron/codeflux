//go:build js && wasm

package main

import (
	"context"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/web/frontend/settingsview"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// recentSpendWindow is the period the settings page reports spend for.
//
// A month is long enough that a figure is not empty for anyone who used the
// product this week, and short enough that it answers "what has this been
// costing me" rather than "what has it ever cost". The label is returned with
// the bounds so the page states the period it is showing; a spend figure with
// no period attached is read as covering now, whatever it covers.
func recentSpendWindow() (time.Time, time.Time, string) {
	until := time.Now().UTC()
	return until.AddDate(0, 0, -30), until, "the last 30 days"
}

// readMountedSettings asks the coordinator for the policy and the recorded
// providers in one exchange.
//
// Both answers are needed before the page can be drawn honestly: the policy
// alone would leave the provider sections claiming nothing is configured, and
// the providers alone would leave the policy section blank under a confident
// heading.
func readMountedSettings(
	ctx context.Context,
	workspace *codefluxv1.StableIdentity,
) (settingsAnswer, error) {
	connection, err := openBrowserSettingsConnection(ctx)
	if err != nil {
		return settingsAnswer{}, err
	}
	defer func() { _ = connection.Close() }()
	client := codefluxv1.NewSettingsServiceClient(connection)
	policy, err := client.GetPolicy(ctx, &codefluxv1.GetPolicyRequest{WorkspaceId: workspace})
	if err != nil {
		return settingsAnswer{}, err
	}
	models, err := client.GetModels(ctx, &codefluxv1.GetModelsRequest{WorkspaceId: workspace})
	if err != nil {
		return settingsAnswer{}, err
	}
	flow, err := client.GetFlowSettings(ctx, &codefluxv1.GetFlowSettingsRequest{
		WorkspaceId: workspace,
	})
	if err != nil {
		return settingsAnswer{}, err
	}
	answer := settingsAnswer{
		Policy:           projectSettingsPolicy(policy),
		Providers:        projectSettingsProviders(models),
		Flow:             projectFlowSettings(flow),
		FlowUnrenderable: flow.GetUnrenderable(),
		FlowRevision:     flow.GetRevision(),
	}
	// Spend is supplementary: the page is still worth drawing without it. A
	// failed read therefore leaves the panel unknown, which renders as "no
	// figure" rather than failing the whole surface or, worse, showing a zero.
	since, until, windowLabel := recentSpendWindow()
	spend, spendErr := client.GetSpendSummary(ctx, &codefluxv1.GetSpendSummaryRequest{
		WorkspaceId: workspace,
		Since:       timestamppb.New(since),
		Until:       timestamppb.New(until),
	})
	if spendErr == nil {
		answer.Spend = projectSpendSummary(spend, windowLabel)
	}
	if timeout, present := settingsRequestTimeout(models); present {
		answer.Policy.RequestTimeout = time.Duration(timeout)
	}
	return answer, nil
}

// checkMountedProviderCredential asks whether the coordinator can obtain the
// credential a provider call would present.
//
// The coordinator's own summary is carried through unchanged, because it is
// the sentence that says what was and was not checked.
func checkMountedProviderCredential(
	ctx context.Context,
	provider *codefluxv1.StableIdentity,
) (settingsview.CredentialCheck, error) {
	connection, err := openBrowserSettingsConnection(ctx)
	if err != nil {
		return settingsview.CredentialCheck{}, err
	}
	defer func() { _ = connection.Close() }()
	response, err := codefluxv1.NewSettingsServiceClient(connection).TestProvider(
		ctx, &codefluxv1.TestProviderRequest{ProviderId: provider},
	)
	if err != nil {
		return settingsview.CredentialCheck{}, err
	}
	return settingsview.CredentialCheck{
		Resolved: response.GetReachable(),
		Summary:  response.GetSummary().GetValue(),
	}, nil
}

// configureMountedProvider binds an operating-system credential reference to a
// provider.
//
// The reference names where a credential lives. No credential value is read by
// this page, held by it, or sent through this call.
func configureMountedProvider(
	ctx context.Context,
	workspace *codefluxv1.StableIdentity,
	provider *codefluxv1.StableIdentity,
	reference string,
	modelID string,
	idempotencyKey string,
) error {
	connection, err := openBrowserSettingsConnection(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = connection.Close() }()
	_, err = codefluxv1.NewSettingsServiceClient(connection).ConfigureProvider(
		ctx,
		&codefluxv1.ConfigureProviderRequest{
			Control:             &codefluxv1.MutationControl{IdempotencyKey: idempotencyKey},
			WorkspaceId:         workspace,
			ProviderId:          provider,
			CredentialReference: reference,
			ModelId:             modelID,
		},
	)
	return err
}

// safeCommandReason renders why a command failed without repeating anything
// the coordinator did not intend for display.
func safeCommandReason(err error) string {
	if err == nil {
		return ""
	}
	if reported, ok := status.FromError(err); ok && reported.Message() != "" {
		return reported.Message()
	}
	return "the coordinator did not answer."
}

// saveMountedFlowSettings records changed run settings.
//
// The expected revision is the one the page read, so a change made against a
// stale view is refused rather than overwriting somebody else's.
func saveMountedFlowSettings(
	ctx context.Context,
	workspace *codefluxv1.StableIdentity,
	changes []*codefluxv1.FlowSettingChange,
	expectedRevision uint64,
	idempotencyKey string,
) ([]settingsview.FlowSetting, uint64, error) {
	connection, err := openBrowserSettingsConnection(ctx)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = connection.Close() }()
	response, err := codefluxv1.NewSettingsServiceClient(connection).SetFlowSettings(
		ctx,
		&codefluxv1.SetFlowSettingsRequest{
			Control: &codefluxv1.MutationControl{
				IdempotencyKey: idempotencyKey, ExpectedRevision: &expectedRevision,
			},
			WorkspaceId: workspace,
			Changes:     changes,
		},
	)
	if err != nil {
		return nil, 0, err
	}
	saved := projectFlowSettings(&codefluxv1.GetFlowSettingsResponse{
		Settings: response.GetSettings(), Revision: response.GetRevision(),
	})
	return saved, response.GetRevision(), nil
}
