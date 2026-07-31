package validationbaseline

import (
	"errors"
	"fmt"
	"slices"
)

type Stability string

const (
	StabilityStable                  Stability = "stable"
	StabilityFlaky                   Stability = "flaky"
	StabilityNondeterministicFailure Stability = "nondeterministic-failure"
	StabilityInsufficientRepeats     Stability = "insufficient-repeat-evidence"
	StabilityUnavailable             Stability = "unavailable"
)

type EvidenceSummary struct {
	TerminalStatus    AttemptStatus
	Stability         Stability
	Fingerprints      []string
	ObservationCount  int
	UnavailableReason string
}

func summarize(evidence Evidence) EvidenceSummary {
	attempts := evidence.Attempts()
	summary := EvidenceSummary{ObservationCount: len(attempts)}
	if attempts[0].Status == AttemptUnavailable {
		summary.TerminalStatus = AttemptUnavailable
		summary.Stability = StabilityUnavailable
		summary.UnavailableReason = attempts[0].UnavailableReason
		return summary
	}
	passed := 0
	fingerprints := make([]string, 0, len(attempts))
	for _, attempt := range attempts {
		if attempt.Status == AttemptPassed {
			passed++
			continue
		}
		if !slices.Contains(fingerprints, attempt.FailureFingerprint) {
			fingerprints = append(fingerprints, attempt.FailureFingerprint)
		}
	}
	slices.Sort(fingerprints)
	summary.Fingerprints = fingerprints
	if passed == len(attempts) {
		summary.TerminalStatus, summary.Stability = AttemptPassed, StabilityStable
		return summary
	}
	summary.TerminalStatus = AttemptFailed
	variable := passed > 0 || len(fingerprints) > 1
	if !variable {
		summary.Stability = StabilityStable
		return summary
	}
	if len(attempts) < MinimumNondeterminismEvidence {
		summary.Stability = StabilityInsufficientRepeats
		return summary
	}
	if passed > 0 {
		summary.Stability = StabilityFlaky
	} else {
		summary.Stability = StabilityNondeterministicFailure
	}
	return summary
}

type Classification string

const (
	ComparisonNoRegression        Classification = "no-regression"
	ComparisonPreExistingFailure  Classification = "pre-existing-failure"
	ComparisonResolvedFailure     Classification = "resolved-baseline-failure"
	ComparisonIntroducedFailure   Classification = "introduced-failure"
	ComparisonChangedFailure      Classification = "changed-failure"
	ComparisonBaselineUnavailable Classification = "baseline-unavailable"
	ComparisonNondeterministic    Classification = "nondeterministic"
	ComparisonInsufficientRepeats Classification = "insufficient-repeat-evidence"
)

type Comparison struct {
	Baseline        Evidence
	Candidate       Evidence
	BaselineResult  EvidenceSummary
	CandidateResult EvidenceSummary
	Classification  Classification
}

func Compare(baseline, candidate Evidence) (Comparison, error) {
	if err := baseline.Validate(); err != nil {
		return Comparison{}, err
	}
	if err := candidate.Validate(); err != nil {
		return Comparison{}, err
	}
	if baseline.Binding().Command != candidate.Binding().Command {
		return Comparison{}, invalid("comparison.command", "baseline and candidate must bind the exact command definition and executable")
	}
	if baseline.Binding().Revision == candidate.Binding().Revision {
		return Comparison{}, invalid("comparison.revision", "baseline and candidate must bind distinct exact revisions")
	}
	baselineResult := summarize(baseline)
	candidateResult := summarize(candidate)
	comparison := Comparison{Baseline: baseline, Candidate: candidate, BaselineResult: baselineResult, CandidateResult: candidateResult}
	switch {
	case baselineResult.Stability == StabilityUnavailable:
		comparison.Classification = ComparisonBaselineUnavailable
	case baselineResult.Stability == StabilityInsufficientRepeats || candidateResult.Stability == StabilityInsufficientRepeats:
		comparison.Classification = ComparisonInsufficientRepeats
	case baselineResult.Stability == StabilityFlaky || baselineResult.Stability == StabilityNondeterministicFailure ||
		candidateResult.Stability == StabilityFlaky || candidateResult.Stability == StabilityNondeterministicFailure:
		comparison.Classification = ComparisonNondeterministic
	case baselineResult.TerminalStatus == AttemptPassed && candidateResult.TerminalStatus == AttemptPassed:
		comparison.Classification = ComparisonNoRegression
	case baselineResult.TerminalStatus == AttemptFailed && candidateResult.TerminalStatus == AttemptPassed:
		comparison.Classification = ComparisonResolvedFailure
	case baselineResult.TerminalStatus == AttemptPassed && candidateResult.TerminalStatus == AttemptFailed:
		comparison.Classification = ComparisonIntroducedFailure
	case slices.Equal(baselineResult.Fingerprints, candidateResult.Fingerprints):
		comparison.Classification = ComparisonPreExistingFailure
	default:
		comparison.Classification = ComparisonChangedFailure
	}
	return comparison, nil
}

// UnresolvedBaselineFailure is a structured final-report fact. It is emitted
// only when the exact baseline command actually failed and remains unresolved.
type UnresolvedBaselineFailure struct {
	Command             CommandBinding
	BaselineRevision    RevisionBinding
	CandidateRevision   RevisionBinding
	FailureFingerprints []string
	Comparison          Classification
}

type ReportRecord struct {
	Command                    CommandBinding
	BaselineRevision           RevisionBinding
	CandidateRevision          RevisionBinding
	Comparison                 Classification
	UnresolvedBaselineFailures []UnresolvedBaselineFailure
	Limitations                []string
}

func FinalReportRecord(comparison Comparison) (ReportRecord, error) {
	verified, err := Compare(comparison.Baseline, comparison.Candidate)
	if err != nil {
		return ReportRecord{}, err
	}
	if verified.Classification != comparison.Classification {
		return ReportRecord{}, errors.New("comparison classification is not derived from its evidence")
	}
	record := ReportRecord{
		Command:           verified.Baseline.Binding().Command,
		BaselineRevision:  verified.Baseline.Binding().Revision,
		CandidateRevision: verified.Candidate.Binding().Revision,
		Comparison:        verified.Classification,
	}
	if verified.BaselineResult.TerminalStatus == AttemptFailed &&
		verified.CandidateResult.TerminalStatus == AttemptFailed {
		record.UnresolvedBaselineFailures = append(record.UnresolvedBaselineFailures, UnresolvedBaselineFailure{
			Command: record.Command, BaselineRevision: record.BaselineRevision,
			CandidateRevision:   record.CandidateRevision,
			FailureFingerprints: slices.Clone(verified.BaselineResult.Fingerprints),
			Comparison:          verified.Classification,
		})
	}
	switch verified.Classification {
	case ComparisonBaselineUnavailable:
		record.Limitations = append(record.Limitations, "Baseline evidence is unavailable: "+verified.BaselineResult.UnavailableReason+". Non-regression is not claimed.")
	case ComparisonNondeterministic:
		record.Limitations = append(record.Limitations, "Repeated exact-bound validation evidence is flaky or nondeterministic.")
	case ComparisonInsufficientRepeats:
		record.Limitations = append(record.Limitations, fmt.Sprintf("Fewer than %d exact-bound observations cannot establish flakiness.", MinimumNondeterminismEvidence))
	}
	return record, nil
}
