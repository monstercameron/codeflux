package timelinecard

import (
	"testing"

	"codeflux.dev/codeflux/web/frontend/design"
)

func TestDesignKindResolvesForEveryPresentedCardKind(t *testing.T) {
	presented := map[Kind]design.Kind{
		KindForecast:     design.KindForecast,
		KindPlan:         design.KindPlan,
		KindPlanRevision: design.KindPlan,
		KindTool:         design.KindCode,
		KindDiff:         design.KindCode,
		KindValidation:   design.KindValidation,
		KindCompletion:   design.KindEvidence,
		KindCheckpoint:   design.KindMemory,
		KindRecovery:     design.KindMemory,
		KindContext:      design.KindMemory,
		KindApproval:     design.KindExecution,
		KindTaskState:    design.KindExecution,
		KindGraphChange:  design.KindExecution,
	}
	tokens, err := design.TokensFor(design.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for kind, want := range presented {
		got, ok := kind.DesignKind()
		if !ok || got != want {
			t.Fatalf("%s.DesignKind() = %s, %t; want %s, true", kind, got, ok, want)
		}
		if _, err := design.KindPresentationFor(got, tokens); err != nil {
			t.Fatalf("%s resolves to unpresentable design kind %s: %v", kind, got, err)
		}
	}
}

func TestDesignKindLeavesUnmappedCardKindsExplicit(t *testing.T) {
	for _, kind := range []Kind{
		KindMessage, KindThreadState, KindRequirement, KindCostBudget,
		KindError, KindUsage, KindUnknown,
	} {
		if _, ok := kind.DesignKind(); ok {
			t.Fatalf("%s unexpectedly resolved a design kind", kind)
		}
	}
}

func TestCardDesignKindDelegatesToKind(t *testing.T) {
	card := Card{Kind: KindValidation, StableKey: "validation:1", Validation: &Validation{ID: "v1"}}
	if err := card.Validate(); err != nil {
		t.Fatal(err)
	}
	got, ok := card.DesignKind()
	if !ok || got != design.KindValidation {
		t.Fatalf("card.DesignKind() = %s, %t", got, ok)
	}
}
