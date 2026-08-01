package storage

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Scorecard is the M22-100 redacted local prototype scorecard.
//
// It is assembled from the M22-091..099 queries and carries nothing but
// counts, durations, and enumerated reasons. No requirement text, file path,
// command line, or model output reaches it, so the whole structure is safe to
// read aloud, screenshot, or paste into an issue — which is the only way a
// prototype scorecard is actually used.
type Scorecard struct {
	Window       MetricsWindow
	GeneratedAt  time.Time
	Outcomes     TaskOutcomeMetrics
	Regressions  RegressionMetrics
	Durations    DurationMetrics
	Costs        CostMetrics
	Forecasts    ForecastAccuracyMetrics
	Authority    AuthorityMetrics
	Interruption InterruptionMetrics
	Memory       MemoryMetrics
	Graph        GraphUsageMetrics
	// Surprises is M22-102: the things that did not fit the aggregate.
	// A scorecard that reports only success rates is the scorecard that
	// misses the reason to stop.
	Surprises []Surprise
}

// SurpriseSeverity separates a fact worth noticing from one that should stop
// the run.
type SurpriseSeverity string

const (
	// SurpriseNotable is worth a human look but does not block anything.
	SurpriseNotable SurpriseSeverity = "notable"
	// SurpriseConcerning contradicts an assumption the product depends on.
	SurpriseConcerning SurpriseSeverity = "concerning"
)

// Surprise is one recorded failure or contradiction (M22-102).
//
// Detail is deliberately constrained to enumerated, non-user-content text.
type Surprise struct {
	Code     string
	Severity SurpriseSeverity
	Detail   string
}

// Validate rejects a surprise carrying anything but its own enumerated facts.
func (surprise Surprise) Validate() error {
	switch {
	case strings.TrimSpace(surprise.Code) == "":
		return errors.New("a surprise must carry a stable code")
	case surprise.Severity != SurpriseNotable && surprise.Severity != SurpriseConcerning:
		return fmt.Errorf("surprise %q has unknown severity %q", surprise.Code, surprise.Severity)
	case strings.TrimSpace(surprise.Detail) == "":
		return fmt.Errorf("surprise %q states no detail", surprise.Code)
	case len(surprise.Detail) > 512:
		return fmt.Errorf("surprise %q detail is too long to be a summary", surprise.Code)
	}
	return nil
}

// BuildScorecard runs every M22-091..099 query and assembles M22-100.
func (repositories *Repositories) BuildScorecard(
	ctx context.Context,
	window MetricsWindow,
) (Scorecard, error) {
	if err := window.Validate(); err != nil {
		return Scorecard{}, err
	}
	now, _ := repositories.timestamp()
	card := Scorecard{Window: window, GeneratedAt: now}

	var err error
	if card.Outcomes, err = repositories.TaskOutcomeMetrics(ctx, window); err != nil {
		return Scorecard{}, fmt.Errorf("scorecard outcomes: %w", err)
	}
	if card.Regressions, err = repositories.RegressionMetrics(ctx, window); err != nil {
		return Scorecard{}, fmt.Errorf("scorecard regressions: %w", err)
	}
	if card.Durations, err = repositories.DurationMetrics(ctx, window); err != nil {
		return Scorecard{}, fmt.Errorf("scorecard durations: %w", err)
	}
	if card.Costs, err = repositories.CostMetrics(ctx, window); err != nil {
		return Scorecard{}, fmt.Errorf("scorecard costs: %w", err)
	}
	if card.Forecasts, err = repositories.ForecastAccuracyMetrics(ctx, window); err != nil {
		return Scorecard{}, fmt.Errorf("scorecard forecasts: %w", err)
	}
	if card.Authority, err = repositories.AuthorityMetrics(ctx, window); err != nil {
		return Scorecard{}, fmt.Errorf("scorecard authority: %w", err)
	}
	if card.Interruption, err = repositories.InterruptionMetrics(ctx, window); err != nil {
		return Scorecard{}, fmt.Errorf("scorecard interruptions: %w", err)
	}
	if card.Memory, err = repositories.MemoryMetrics(ctx, window); err != nil {
		return Scorecard{}, fmt.Errorf("scorecard memory: %w", err)
	}
	if card.Graph, err = repositories.GraphUsageMetrics(ctx, window); err != nil {
		return Scorecard{}, fmt.Errorf("scorecard graph: %w", err)
	}
	card.Surprises = card.detectSurprises()
	return card, nil
}

// detectSurprises is M22-102. Each rule names a specific way the aggregate can
// look acceptable while the underlying situation is not.
func (card Scorecard) detectSurprises() []Surprise {
	var surprises []Surprise
	add := func(code string, severity SurpriseSeverity, detail string) {
		surprises = append(surprises, Surprise{Code: code, Severity: severity, Detail: detail})
	}

	if card.Costs.UsageUnknownCount.Value > 0 {
		add("usage-unknown", SurpriseConcerning, fmt.Sprintf(
			"%d provider responses reported no token usage; token totals understate real consumption",
			card.Costs.UsageUnknownCount.Value))
	}
	if card.Costs.CostUnknownCount.Value > 0 {
		add("cost-unknown", SurpriseConcerning, fmt.Sprintf(
			"%d provider responses reported no price; spend totals are a lower bound, not a total",
			card.Costs.CostUnknownCount.Value))
	}
	if card.Costs.Currency == "" && card.Costs.CostMinorUnits.Value > 0 {
		add("mixed-currency", SurpriseConcerning,
			"more than one currency appears in this window, so the summed minor units are not a single amount")
	}
	if card.Outcomes.TasksCompleted.Value > 0 && card.Outcomes.ChangesAccepted.Value == 0 {
		add("completed-but-unaccepted", SurpriseConcerning,
			"tasks completed but no change was accepted; the system finished work the user did not take")
	}
	if card.Regressions.TasksLeftFailing.Value > 0 {
		add("unresolved-failures", SurpriseConcerning, fmt.Sprintf(
			"%d failed tasks were never resolved by an acceptance",
			card.Regressions.TasksLeftFailing.Value))
	}
	if card.Memory.CandidatesRetrieved.Value > 0 && card.Memory.CandidatesAccepted.Value == 0 {
		add("memory-retrieved-never-used", SurpriseNotable, fmt.Sprintf(
			"%d memory candidates were retrieved and none was accepted",
			card.Memory.CandidatesRetrieved.Value))
	}
	if card.Forecasts.ForecastsIssued.Value > 0 && card.Forecasts.OutcomesRecorded.Value == 0 {
		add("forecasts-never-scored", SurpriseNotable, fmt.Sprintf(
			"%d forecasts were issued and none was compared against an outcome",
			card.Forecasts.ForecastsIssued.Value))
	}
	if card.Authority.ApprovalsRequested.Value > 0 &&
		card.Authority.ApprovalsRequested.Value == card.Authority.ApprovalsGranted.Value {
		add("every-approval-granted", SurpriseNotable, fmt.Sprintf(
			"all %d approval requests were granted; an approval nobody ever refuses is not a control",
			card.Authority.ApprovalsRequested.Value))
	}
	if card.Graph.GraphRevisions.Value > 0 && card.Graph.NodesProjected.Value == 0 {
		add("graph-collapsed", SurpriseConcerning,
			"graph revisions were produced with no nodes; the projection is collapsing the detail it exists to show")
	}
	for _, reason := range card.Durations.UnmeasurableReasons {
		add("duration-unmeasurable", SurpriseNotable, reason)
	}

	sort.SliceStable(surprises, func(left, right int) bool {
		if surprises[left].Severity != surprises[right].Severity {
			// Concerning first: a scorecard read top-down must lead with what
			// contradicts an assumption, not with what is merely interesting.
			return surprises[left].Severity == SurpriseConcerning
		}
		return surprises[left].Code < surprises[right].Code
	})
	return surprises
}

// Validate checks a scorecard is internally consistent and carries no
// unredacted content.
func (card Scorecard) Validate() error {
	if err := card.Window.Validate(); err != nil {
		return err
	}
	if card.GeneratedAt.IsZero() {
		return errors.New("scorecard has no generation time")
	}
	for _, surprise := range card.Surprises {
		if err := surprise.Validate(); err != nil {
			return err
		}
	}
	started := card.Outcomes.TasksStarted.Value
	terminal := card.Outcomes.TasksCompleted.Value + card.Outcomes.TasksFailed.Value +
		card.Outcomes.TasksCancelled.Value + card.Outcomes.TasksRolledBack.Value
	if terminal > started {
		return fmt.Errorf(
			"scorecard counts %d terminal tasks from %d started; the window is inconsistent",
			terminal, started)
	}
	return nil
}

// ComparisonVerdict is the result of M22-101.
type ComparisonVerdict string

const (
	// VerdictBetter means the run improved on the baseline in the compared
	// dimension.
	VerdictBetter ComparisonVerdict = "better"
	// VerdictWorse means it regressed.
	VerdictWorse ComparisonVerdict = "worse"
	// VerdictUnchanged means the two are equal.
	VerdictUnchanged ComparisonVerdict = "unchanged"
	// VerdictNotComparable means at least one side did not measure the
	// dimension. This is a distinct answer from "unchanged", because a missing
	// measurement is not evidence of parity.
	VerdictNotComparable ComparisonVerdict = "not-comparable"
)

// ComparisonLine is one compared dimension.
type ComparisonLine struct {
	Dimension string
	Run       Count
	Baseline  Count
	Verdict   ComparisonVerdict
	// HigherIsBetter records the direction, so a reader never has to guess
	// whether a larger number is good.
	HigherIsBetter bool
}

// Comparison is the M22-101 frozen-run against frozen-baseline result.
type Comparison struct {
	RunWindow      MetricsWindow
	BaselineWindow MetricsWindow
	Lines          []ComparisonLine
}

// CompareScorecards implements M22-101.
//
// Both sides must be complete scorecards over explicit windows. A dimension
// either side did not measure is reported as not-comparable rather than being
// silently treated as zero, because a baseline that never ran a query is not a
// baseline of zero.
func CompareScorecards(run, baseline Scorecard) (Comparison, error) {
	if err := run.Validate(); err != nil {
		return Comparison{}, fmt.Errorf("run scorecard: %w", err)
	}
	if err := baseline.Validate(); err != nil {
		return Comparison{}, fmt.Errorf("baseline scorecard: %w", err)
	}
	comparison := Comparison{RunWindow: run.Window, BaselineWindow: baseline.Window}

	dimensions := []struct {
		name           string
		run            Count
		baseline       Count
		higherIsBetter bool
	}{
		{"tasks-completed", run.Outcomes.TasksCompleted, baseline.Outcomes.TasksCompleted, true},
		{"changes-accepted", run.Outcomes.ChangesAccepted, baseline.Outcomes.ChangesAccepted, true},
		{"changes-rejected", run.Outcomes.ChangesRejected, baseline.Outcomes.ChangesRejected, false},
		{"tasks-failed", run.Outcomes.TasksFailed, baseline.Outcomes.TasksFailed, false},
		{"unresolved-failures", run.Regressions.TasksLeftFailing, baseline.Regressions.TasksLeftFailing, false},
		{"validations-failed", run.Regressions.ValidationsFailed, baseline.Regressions.ValidationsFailed, false},
		{"repair-attempts", run.Costs.RepairAttempts, baseline.Costs.RepairAttempts, false},
		{"cost-minor-units", run.Costs.CostMinorUnits, baseline.Costs.CostMinorUnits, false},
		{"output-tokens", run.Costs.OutputTokens, baseline.Costs.OutputTokens, false},
		{"approvals-denied", run.Authority.ApprovalsDenied, baseline.Authority.ApprovalsDenied, false},
		{"memory-accepted", run.Memory.CandidatesAccepted, baseline.Memory.CandidatesAccepted, true},
	}
	for _, dimension := range dimensions {
		comparison.Lines = append(comparison.Lines, ComparisonLine{
			Dimension:      dimension.name,
			Run:            dimension.run,
			Baseline:       dimension.baseline,
			Verdict:        compareCounts(dimension.run, dimension.baseline, dimension.higherIsBetter),
			HigherIsBetter: dimension.higherIsBetter,
		})
	}
	return comparison, nil
}

func compareCounts(run, baseline Count, higherIsBetter bool) ComparisonVerdict {
	if !run.Known || !baseline.Known {
		return VerdictNotComparable
	}
	if run.Value == baseline.Value {
		return VerdictUnchanged
	}
	improved := run.Value > baseline.Value
	if !higherIsBetter {
		improved = run.Value < baseline.Value
	}
	if improved {
		return VerdictBetter
	}
	return VerdictWorse
}

// Regressions returns the compared dimensions that got worse, which is the
// part of a comparison that decides anything.
func (comparison Comparison) Regressions() []ComparisonLine {
	var worse []ComparisonLine
	for _, line := range comparison.Lines {
		if line.Verdict == VerdictWorse {
			worse = append(worse, line)
		}
	}
	return worse
}
