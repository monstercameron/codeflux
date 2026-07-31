package validationbaseline

import (
	"errors"
	"strings"
	"testing"
)

func TestCompareDistinguishesPreExistingIntroducedResolvedAndChangedFailures(t *testing.T) {
	for _, test := range []struct {
		name      string
		baseline  []Attempt
		candidate []Attempt
		want      Classification
	}{
		{"clean", passed(), passed(), ComparisonNoRegression},
		{"pre-existing", failed("pkg:test-a"), failed("pkg:test-a"), ComparisonPreExistingFailure},
		{"resolved", failed("pkg:test-a"), passed(), ComparisonResolvedFailure},
		{"introduced", passed(), failed("pkg:test-a"), ComparisonIntroducedFailure},
		{"changed", failed("pkg:test-a"), failed("pkg:test-b"), ComparisonChangedFailure},
	} {
		t.Run(test.name, func(t *testing.T) {
			comparison, err := Compare(evidence(t, "base", "diff-base", test.baseline), evidence(t, "candidate", "diff-candidate", test.candidate))
			if err != nil || comparison.Classification != test.want {
				t.Fatalf("comparison = %#v, %v; want %s", comparison, err, test.want)
			}
		})
	}
}

func TestCompareRequiresExactCommandAndRevisionBindings(t *testing.T) {
	baseline := evidence(t, "base", "diff-base", passed())
	candidate := evidence(t, "candidate", "diff-candidate", passed())
	candidate.binding.Command.ExecutableIdentity = "go1.26.1"
	if _, err := Compare(baseline, candidate); !errors.Is(err, ErrInvalidBaselineEvidence) {
		t.Fatalf("mismatched executable error = %v", err)
	}
	candidate = evidence(t, "base", "diff-base", passed())
	if _, err := Compare(baseline, candidate); !errors.Is(err, ErrInvalidBaselineEvidence) {
		t.Fatalf("same revision error = %v", err)
	}
}

func TestUnavailableBaselineNeverClaimsNonRegression(t *testing.T) {
	baseline := evidence(t, "base", "diff-base", []Attempt{{Ordinal: 1, Status: AttemptUnavailable, UnavailableReason: "baseline command exceeded the affordability limit"}})
	comparison, err := Compare(baseline, evidence(t, "candidate", "diff-candidate", passed()))
	if err != nil || comparison.Classification != ComparisonBaselineUnavailable {
		t.Fatalf("comparison = %#v, %v", comparison, err)
	}
	report, err := FinalReportRecord(comparison)
	if err != nil || len(report.Limitations) != 1 || !strings.Contains(report.Limitations[0], "Non-regression is not claimed") {
		t.Fatalf("report = %#v, %v", report, err)
	}
}

func TestFlakyAndNondeterministicLabelsRequireRepeatedEvidence(t *testing.T) {
	inconclusive := []Attempt{{Ordinal: 1, Status: AttemptFailed, FailureFingerprint: "pkg:a"}, {Ordinal: 2, Status: AttemptPassed}}
	comparison, err := Compare(evidence(t, "base", "diff-base", inconclusive), evidence(t, "candidate", "diff-candidate", passed()))
	if err != nil || comparison.Classification != ComparisonInsufficientRepeats {
		t.Fatalf("two-observation comparison = %#v, %v", comparison, err)
	}
	flaky := append(inconclusive, Attempt{Ordinal: 3, Status: AttemptFailed, FailureFingerprint: "pkg:a"})
	comparison, err = Compare(evidence(t, "base", "diff-base", flaky), evidence(t, "candidate", "diff-candidate", passed()))
	if err != nil || comparison.Classification != ComparisonNondeterministic || comparison.BaselineResult.Stability != StabilityFlaky {
		t.Fatalf("flaky comparison = %#v, %v", comparison, err)
	}
	variableFailure := []Attempt{
		{Ordinal: 1, Status: AttemptFailed, FailureFingerprint: "pkg:a"},
		{Ordinal: 2, Status: AttemptFailed, FailureFingerprint: "pkg:b"},
		{Ordinal: 3, Status: AttemptFailed, FailureFingerprint: "pkg:a"},
	}
	comparison, err = Compare(evidence(t, "base", "diff-base", variableFailure), evidence(t, "candidate", "diff-candidate", passed()))
	if err != nil || comparison.BaselineResult.Stability != StabilityNondeterministicFailure {
		t.Fatalf("variable-failure comparison = %#v, %v", comparison, err)
	}
}

func TestFinalReportRetainsUnresolvedBaselineFailure(t *testing.T) {
	comparison, err := Compare(evidence(t, "base", "diff-base", failed("pkg:legacy")), evidence(t, "candidate", "diff-candidate", failed("pkg:legacy")))
	if err != nil {
		t.Fatal(err)
	}
	report, err := FinalReportRecord(comparison)
	if err != nil || len(report.UnresolvedBaselineFailures) != 1 {
		t.Fatalf("report = %#v, %v", report, err)
	}
	failure := report.UnresolvedBaselineFailures[0]
	if failure.FailureFingerprints[0] != "pkg:legacy" || failure.BaselineRevision.WorktreeRevision != "base" {
		t.Fatalf("unresolved failure = %#v", failure)
	}
}

func evidence(t *testing.T, revision, diff string, attempts []Attempt) Evidence {
	t.Helper()
	value, err := NewEvidence(Binding{
		Command:  CommandBinding{DefinitionID: "go-test:./...", ExecutableIdentity: "go1.26.0@sha256:fixture"},
		Revision: RevisionBinding{WorktreeRevision: revision, DiffIdentity: diff},
	}, attempts)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func passed() []Attempt { return []Attempt{{Ordinal: 1, Status: AttemptPassed}} }
func failed(fingerprint string) []Attempt {
	return []Attempt{{Ordinal: 1, Status: AttemptFailed, FailureFingerprint: fingerprint}}
}
