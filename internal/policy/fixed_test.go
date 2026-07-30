package policy

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/providers"
)

func TestSelectFixedBaselineIsDeterministicAndInspectable(t *testing.T) {
	input := SelectionInput{BaselineModelRevision: "revision-42"}
	first, err := Select(input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Select(input)
	if err != nil {
		t.Fatal(err)
	}
	firstJSON, err := first.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	secondJSON, err := second.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstJSON, secondJSON) {
		t.Fatalf("identical selection JSON differs:\n%s\n%s", firstJSON, secondJSON)
	}
	firstDigest, err := first.Digest()
	if err != nil {
		t.Fatal(err)
	}
	secondDigest, err := second.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if firstDigest != secondDigest || len(firstDigest) != 64 {
		t.Fatalf("digests = %q, %q", firstDigest, secondDigest)
	}
	if first.Version != FixedBaselineVersion ||
		first.Source != SelectionSourceFixedBaseline ||
		first.Model != fixedBaselineModel(input.BaselineModelRevision) ||
		first.Reasoning != domain.ReasoningEffortMaximum {
		t.Fatalf("selection = %#v", first)
	}
	if first.Limits.MaximumPlanningRounds != 2 ||
		first.Limits.MaximumRepairRounds != 3 ||
		first.Limits.MaximumToolCallsPerRound != 24 ||
		first.Limits.MaximumContextTokens != 128_000 {
		t.Fatalf("limits = %#v", first.Limits)
	}
	if len(first.Phases) != 4 ||
		first.Phases[0].Phase != PhasePlanning ||
		first.Phases[1].Phase != PhaseExecution ||
		!first.Phases[1].MayMutateWorkspace ||
		!first.Phases[2].RequiresValidationEvidence ||
		first.Phases[3].MayMutateWorkspace {
		t.Fatalf("phase behavior = %#v", first.Phases)
	}
}

func TestFixedBaselineRejectsProviderModelAndEffortDrift(t *testing.T) {
	selected, err := Select(SelectionInput{
		BaselineModelRevision: "revision-42",
	})
	if err != nil {
		t.Fatal(err)
	}
	drifted := []Snapshot{selected, selected, selected}
	drifted[0].Model.Provider.Provider = "other-provider"
	drifted[1].Model.Model = "other-model"
	drifted[2].Reasoning = domain.ReasoningEffortStandard
	for index, snapshot := range drifted {
		if err := snapshot.Validate(); !errors.Is(err, ErrInvalidPolicy) {
			t.Fatalf("drift %d validation error = %v", index, err)
		}
	}

	otherRevision, err := Select(SelectionInput{
		BaselineModelRevision: "revision-43",
	})
	if err != nil {
		t.Fatal(err)
	}
	firstDigest, _ := selected.Digest()
	secondDigest, _ := otherRevision.Digest()
	if firstDigest == secondDigest {
		t.Fatal("different provider revision did not create a new stratum")
	}
}

func TestSelectManualOverrideRequiresAttribution(t *testing.T) {
	baseline := policyTestModel("baseline-revision")
	overrideModel := policyTestModel("override-revision")
	overrideModel.Model = "configured-alternate"
	override := &ManualOverride{
		Model: overrideModel, Reasoning: domain.ReasoningEffortExtended,
		Actor: "user:fixture", AuthorityReference: "approval:fixture",
		Reason: "explicit latency experiment",
	}
	selected, err := Select(SelectionInput{
		BaselineModelRevision: baseline.Revision,
		Override:              override,
	})
	if err != nil {
		t.Fatal(err)
	}
	if selected.Source != SelectionSourceManualOverride ||
		selected.Model != overrideModel ||
		selected.Reasoning != domain.ReasoningEffortExtended ||
		!reflect.DeepEqual(selected.ManualOverride, override) {
		t.Fatalf("manual selection = %#v", selected)
	}
	for _, phase := range selected.Phases {
		if phase.Reasoning != domain.ReasoningEffortExtended {
			t.Fatalf("phase reasoning = %q", phase.Reasoning)
		}
	}

	override.AuthorityReference = ""
	if _, err := Select(SelectionInput{
		BaselineModelRevision: baseline.Revision,
		Override:              override,
	}); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("missing attribution error = %v", err)
	}
}

func TestBudgetDefaultsAndBeforeApprovalAdjustment(t *testing.T) {
	selected, err := Select(SelectionInput{
		BaselineModelRevision: "revision-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	id, err := domain.NewBudgetID()
	if err != nil {
		t.Fatal(err)
	}
	current, err := selected.BudgetDefaults.Materialize(id)
	if err != nil {
		t.Fatal(err)
	}
	if current.HardStopCost.MinorUnits != 5_000 ||
		current.HardStopTokens != 1_000_000 ||
		current.MaximumRepairRounds != selected.Limits.MaximumRepairRounds {
		t.Fatalf("default budget = %#v", current)
	}
	requested := current
	requested.HardStopTokens = 1_200_000
	adjustment, err := AdjustBudgetBeforeApproval(
		domain.TaskStateAwaitingPlanApproval,
		current,
		requested,
		"user:fixture",
		"approval:budget-fixture",
		"larger repository scope",
	)
	if err != nil {
		t.Fatal(err)
	}
	if adjustment.Previous.HardStopTokens != 1_000_000 ||
		adjustment.Adjusted.HardStopTokens != 1_200_000 {
		t.Fatalf("adjustment = %#v", adjustment)
	}
	if _, err := AdjustBudgetBeforeApproval(
		domain.TaskStateReady,
		current,
		requested,
		"user:fixture",
		"approval:budget-fixture",
		"too late",
	); !errors.Is(err, ErrBudgetAdjustmentTooLate) {
		t.Fatalf("post-approval adjustment error = %v", err)
	}
}

func policyTestModel(revision string) providers.ModelIdentity {
	return providers.ModelIdentity{
		Provider: providers.ProviderIdentity{
			Adapter: "fixture-adapter", AdapterVersion: "adapter-v1",
			Provider: "fixture-provider", ProviderVersion: "provider-v1",
		},
		Model:    "configured-model",
		Revision: revision,
	}
}
