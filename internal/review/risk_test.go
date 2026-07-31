package review

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
)

func TestRiskSignalVocabularyAndFloorsAreExact(t *testing.T) {
	assertRiskSignals(t, "routine", RoutineRiskSignals(), []RiskSignal{
		RiskSignalNarrowScopedChange, RiskSignalTestAdditionOnly, RiskSignalDocumentationOnly,
	}, domain.RiskLevelRoutine)
	assertRiskSignals(t, "elevated", ElevatedRiskSignals(), []RiskSignal{
		RiskSignalBroadChange, RiskSignalGeneratedCode, RiskSignalDependencyChange, RiskSignalConfiguration,
	}, domain.RiskLevelElevated)
	assertRiskSignals(t, "protected", ProtectedRiskSignals(), []RiskSignal{
		RiskSignalAuthentication, RiskSignalAuthorization, RiskSignalPayment, RiskSignalMigration,
		RiskSignalCredential, RiskSignalConcurrency, RiskSignalExternalEffect,
		RiskSignalTestRemoval, RiskSignalUnknownOrConflict,
	}, domain.RiskLevelProtected)
	if len(AllRiskSignals()) != len(RoutineRiskSignals())+len(ElevatedRiskSignals())+len(ProtectedRiskSignals()) {
		t.Fatal("risk signal vocabulary is not an exact partition")
	}
	if CurrentRiskPolicyVersion != RiskPolicyVersionV1 || !CurrentRiskPolicyVersion.IsValid() || RiskPolicyVersion("future").IsValid() {
		t.Fatalf("risk policy version contract is invalid: %q", CurrentRiskPolicyVersion)
	}
}

func TestChangeRiskClassificationSelectsStrongestDeterministicFloor(t *testing.T) {
	classification, err := ClassifyChangeRisk([]RiskSignal{
		RiskSignalDocumentationOnly,
		RiskSignalDependencyChange,
		RiskSignalAuthentication,
		RiskSignalDocumentationOnly,
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if classification.SelectedRisk() != domain.RiskLevelProtected ||
		classification.PolicyVersion() != RiskPolicyVersionV1 {
		t.Fatalf("classification = risk %s policy %s", classification.SelectedRisk(), classification.PolicyVersion())
	}
	wantSignals := []RiskSignal{
		RiskSignalDocumentationOnly, RiskSignalDependencyChange, RiskSignalAuthentication,
	}
	if got := classification.Signals(); !reflect.DeepEqual(got, wantSignals) {
		t.Fatalf("canonical signals = %v, want %v", got, wantSignals)
	}
	if explanation := classification.Explanation(); !strings.Contains(explanation, "authentication requires protected") {
		t.Fatalf("explanation = %q", explanation)
	}
	if err := classification.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestUserRiskOverrideRaisesButNeverLowersPolicyFloor(t *testing.T) {
	raised, err := ClassifyChangeRisk([]RiskSignal{RiskSignalNarrowScopedChange}, domain.RiskLevelElevated)
	if err != nil {
		t.Fatal(err)
	}
	if raised.SelectedRisk() != domain.RiskLevelElevated ||
		raised.Reasons()[len(raised.Reasons())-1].Code != RiskReasonUserOverrideRaised {
		t.Fatalf("raised classification = %#v", raised)
	}

	retained, err := ClassifyChangeRisk([]RiskSignal{RiskSignalPayment}, domain.RiskLevelRoutine)
	if err != nil {
		t.Fatal(err)
	}
	if retained.SelectedRisk() != domain.RiskLevelProtected ||
		retained.Reasons()[len(retained.Reasons())-1].Code != RiskReasonUserOverrideRetained ||
		!strings.Contains(retained.Explanation(), "cannot lower") {
		t.Fatalf("retained classification = %#v explanation=%q", retained, retained.Explanation())
	}
	if override, ok := retained.UserOverride(); !ok || override != domain.RiskLevelRoutine {
		t.Fatalf("retained user override = %q, %v", override, ok)
	}
}

func TestMissingOrUnrecognizedRiskInputFailsConservatively(t *testing.T) {
	missing, err := ClassifyChangeRisk(nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if missing.SelectedRisk() != domain.RiskLevelProtected ||
		missing.Reasons()[0].Code != RiskReasonConservativeDefault {
		t.Fatalf("missing-signal classification = %#v", missing)
	}
	if _, err := ClassifyChangeRisk([]RiskSignal{"authentication-adjacent"}, ""); !errors.Is(err, ErrInvalidRiskClassification) {
		t.Fatalf("unknown signal error = %v", err)
	}
	if _, err := ClassifyChangeRisk(RoutineRiskSignals(), domain.RiskLevel("lower-than-routine")); !errors.Is(err, ErrInvalidRiskClassification) {
		t.Fatalf("unknown override error = %v", err)
	}
}

func TestRiskClassificationOwnsAcceptedSlices(t *testing.T) {
	input := []RiskSignal{RiskSignalNarrowScopedChange, RiskSignalConfiguration}
	classification, err := ClassifyChangeRisk(input, "")
	if err != nil {
		t.Fatal(err)
	}
	input[0] = RiskSignalPayment
	returnedSignals := classification.Signals()
	returnedSignals[0] = RiskSignalCredential
	returnedReasons := classification.Reasons()
	returnedReasons[0].Floor = domain.RiskLevelProtected
	if got := classification.Signals(); !reflect.DeepEqual(got, []RiskSignal{RiskSignalNarrowScopedChange, RiskSignalConfiguration}) {
		t.Fatalf("classification signals were mutated: %v", got)
	}
	if classification.Reasons()[0].Floor != domain.RiskLevelRoutine || classification.Reasons()[1].Floor != domain.RiskLevelElevated {
		t.Fatalf("classification reasons were mutated: %#v", classification.Reasons())
	}
}

func TestRiskEscalationAddsEvidenceAndNeverDemotes(t *testing.T) {
	routine, err := ClassifyChangeRisk([]RiskSignal{RiskSignalNarrowScopedChange}, "")
	if err != nil {
		t.Fatal(err)
	}
	elevated, err := EscalateChangeRisk(routine, []RiskSignal{RiskSignalDependencyChange}, "")
	if err != nil || elevated.SelectedRisk() != domain.RiskLevelElevated {
		t.Fatalf("elevated = %#v, %v", elevated, err)
	}
	protected, err := EscalateChangeRisk(elevated, []RiskSignal{RiskSignalExternalEffect}, domain.RiskLevelRoutine)
	if err != nil || protected.SelectedRisk() != domain.RiskLevelProtected {
		t.Fatalf("protected = %#v, %v", protected, err)
	}
	retained, err := EscalateChangeRisk(protected, []RiskSignal{RiskSignalDocumentationOnly}, "")
	if err != nil || retained.SelectedRisk() != domain.RiskLevelProtected {
		t.Fatalf("retained = %#v, %v", retained, err)
	}
	if !reflect.DeepEqual(retained.Signals(), []RiskSignal{
		RiskSignalNarrowScopedChange, RiskSignalDocumentationOnly, RiskSignalDependencyChange, RiskSignalExternalEffect,
	}) {
		t.Fatalf("retained signals = %v", retained.Signals())
	}
}

func TestEveryProtectedSignalHasPositiveAndNegativeClassificationFixture(t *testing.T) {
	for _, protected := range ProtectedRiskSignals() {
		t.Run(string(protected), func(t *testing.T) {
			positive, err := ClassifyChangeRisk([]RiskSignal{protected}, "")
			if err != nil {
				t.Fatal(err)
			}
			if positive.SelectedRisk() != domain.RiskLevelProtected {
				t.Fatalf("positive %s classified %s", protected, positive.SelectedRisk())
			}
			negative, err := ClassifyChangeRisk([]RiskSignal{RiskSignalNarrowScopedChange}, "")
			if err != nil {
				t.Fatal(err)
			}
			if negative.SelectedRisk() == domain.RiskLevelProtected {
				t.Fatalf("negative fixture for %s classified protected", protected)
			}
		})
	}
}

func assertRiskSignals(t *testing.T, name string, got, want []RiskSignal, floor domain.RiskLevel) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s signals = %v, want %v", name, got, want)
	}
	seen := map[RiskSignal]bool{}
	for _, signal := range got {
		if !signal.IsValid() || seen[signal] || signal.Floor() != floor {
			t.Fatalf("%s signal %q is invalid, duplicated, or has floor %q", name, signal, signal.Floor())
		}
		seen[signal] = true
	}
}
