package forecast

import (
	"errors"
	"fmt"
	"strings"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/providers"
)

var ErrInvalidShadowTelemetry = errors.New("invalid shadow telemetry")

// CounterfactualEligibility records later-study eligibility without granting
// selection authority.
type CounterfactualEligibility struct {
	Eligible     bool     `json:"eligible"`
	Reasons      []string `json:"reasons"`
	AdvisoryOnly bool     `json:"advisory_only"`
}

// NewCounterfactualEligibility creates immutable advisory-only evidence.
func NewCounterfactualEligibility(eligible bool, reasons []string) (CounterfactualEligibility, error) {
	normalized, err := normalizeValues("eligibility reason", reasons, 64)
	if err != nil {
		return CounterfactualEligibility{}, fmt.Errorf("%w: %v", ErrInvalidShadowTelemetry, err)
	}
	if !eligible && len(normalized) == 0 {
		return CounterfactualEligibility{}, fmt.Errorf("%w: ineligibility needs a reason", ErrInvalidShadowTelemetry)
	}
	return CounterfactualEligibility{
		Eligible: eligible, Reasons: normalized, AdvisoryOnly: true,
	}, nil
}

// CanSelectExecutionPolicy is permanently false in the prototype.
func (CounterfactualEligibility) CanSelectExecutionPolicy() bool {
	return false
}

// OutcomeTelemetry is the later-calibration record produced after completion.
type OutcomeTelemetry struct {
	AlgorithmVersion   string                `json:"algorithm_version"`
	TaskFingerprint    string                `json:"task_fingerprint"`
	Accepted           bool                  `json:"accepted"`
	LatencyMillis      domain.Milliseconds   `json:"latency_ms"`
	Usage              providers.Usage       `json:"usage"`
	Cost               providers.ExactAmount `json:"cost"`
	RepairRounds       uint32                `json:"repair_rounds"`
	HumanInterventions uint32                `json:"human_interventions"`
}

// CalibrationReportSchema defines aggregate interval coverage without
// containing any execution-policy output.
type CalibrationReportSchema struct {
	AlgorithmVersion              string `json:"algorithm_version"`
	EligibleTasks                 uint64 `json:"eligible_tasks"`
	AcceptedTasks                 uint64 `json:"accepted_tasks"`
	LatencyP50CoverageBasisPoints uint32 `json:"latency_p50_coverage_basis_points"`
	LatencyP90CoverageBasisPoints uint32 `json:"latency_p90_coverage_basis_points"`
	TokenP50CoverageBasisPoints   uint32 `json:"token_p50_coverage_basis_points"`
	TokenP90CoverageBasisPoints   uint32 `json:"token_p90_coverage_basis_points"`
	CostKnownTasks                uint64 `json:"cost_known_tasks"`
	CostP50CoverageBasisPoints    uint32 `json:"cost_p50_coverage_basis_points"`
	CostP90CoverageBasisPoints    uint32 `json:"cost_p90_coverage_basis_points"`
}

// Validate checks bounds and prevents reports from masquerading as another
// algorithm's calibration evidence.
func (report CalibrationReportSchema) Validate() error {
	if strings.TrimSpace(report.AlgorithmVersion) == "" {
		return fmt.Errorf("%w: algorithm version is required", ErrInvalidShadowTelemetry)
	}
	if report.AcceptedTasks > report.EligibleTasks ||
		report.CostKnownTasks > report.EligibleTasks {
		return fmt.Errorf("%w: report counts are inconsistent", ErrInvalidShadowTelemetry)
	}
	for _, value := range []uint32{
		report.LatencyP50CoverageBasisPoints,
		report.LatencyP90CoverageBasisPoints,
		report.TokenP50CoverageBasisPoints,
		report.TokenP90CoverageBasisPoints,
		report.CostP50CoverageBasisPoints,
		report.CostP90CoverageBasisPoints,
	} {
		if value > 10_000 {
			return fmt.Errorf("%w: coverage exceeds 100 percent", ErrInvalidShadowTelemetry)
		}
	}
	return nil
}
