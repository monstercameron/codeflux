package forecast

import (
	"testing"
)

func TestCounterfactualEligibilityNeverSelectsPolicy(t *testing.T) {
	eligibility, err := NewCounterfactualEligibility(
		true,
		[]string{"complete-outcome", "complete-pricing"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !eligibility.Eligible ||
		!eligibility.AdvisoryOnly ||
		eligibility.CanSelectExecutionPolicy() {
		t.Fatalf("eligibility = %#v", eligibility)
	}
}

func TestCalibrationReportSchemaValidatesCoverage(t *testing.T) {
	report := CalibrationReportSchema{
		AlgorithmVersion: AlgorithmVersion,
		EligibleTasks:    30, AcceptedTasks: 28, CostKnownTasks: 29,
		LatencyP50CoverageBasisPoints: 5_000,
		LatencyP90CoverageBasisPoints: 9_000,
		TokenP50CoverageBasisPoints:   4_800,
		TokenP90CoverageBasisPoints:   9_100,
		CostP50CoverageBasisPoints:    5_200,
		CostP90CoverageBasisPoints:    9_000,
	}
	if err := report.Validate(); err != nil {
		t.Fatal(err)
	}
	report.CostP90CoverageBasisPoints = 10_001
	if err := report.Validate(); err == nil {
		t.Fatal("coverage above 100 percent was accepted")
	}
}
