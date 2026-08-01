// Package executionview presents what a run is doing while it does it: the
// measurements that decide whether to intervene, the ordered steps it has
// taken, the lines it is emitting, and what it is executing right now.
//
// Everything here is a pure function of typed values. The package performs no
// transport, holds no hooks, and reads no browser state, so every presentation
// decision in it can be exercised natively.
package executionview

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// StepState is where one step of a run has got to.
//
// The set is closed and deliberately small. A step is one of these five or the
// value is invalid: a run that showed a sixth state nobody declared would be a
// run whose progress nobody can total.
type StepState string

const (
	StepPending StepState = "pending"
	StepRunning StepState = "running"
	StepDone    StepState = "done"
	StepFailed  StepState = "failed"
	StepSkipped StepState = "skipped"
)

// AllStepStates returns every declared state.
func AllStepStates() []StepState {
	return []StepState{StepPending, StepRunning, StepDone, StepFailed, StepSkipped}
}

// Valid reports whether a state is declared.
func (state StepState) Valid() bool {
	for _, candidate := range AllStepStates() {
		if state == candidate {
			return true
		}
	}
	return false
}

// Terminal reports whether a step will not change again.
func (state StepState) Terminal() bool {
	return state == StepDone || state == StepFailed || state == StepSkipped
}

// Step is one entry in the execution timeline.
type Step struct {
	ID    string
	Label string
	State StepState
	// At is when the step reached its current state. A zero time means the
	// step has not started, which is only valid while it is pending.
	At time.Time
	// Detail is an optional short clarification, such as which test suite is
	// running. It is never required: a step that needs prose to be understood
	// is a step that was named badly.
	Detail string
}

// Validate rejects a step that could not be presented honestly.
func (step Step) Validate() error {
	switch {
	case strings.TrimSpace(step.ID) == "":
		return errors.New("a step requires a stable identity")
	case strings.TrimSpace(step.Label) == "":
		return fmt.Errorf("step %q has no label", step.ID)
	case !step.State.Valid():
		return fmt.Errorf("step %q has unknown state %q", step.ID, step.State)
	}
	// A step that has started must say when. Rendering a running or finished
	// step with no time would show a run whose order cannot be checked.
	if step.State != StepPending && step.At.IsZero() {
		return fmt.Errorf("step %q is %s but records no time", step.ID, step.State)
	}
	return nil
}

// Progress totals a sequence of steps.
type Progress struct {
	Completed int
	Total     int
	// Running is the step currently in flight, if any.
	Running string
}

// Fraction returns completion between zero and one.
func (progress Progress) Fraction() float64 {
	if progress.Total <= 0 {
		return 0
	}
	if progress.Completed >= progress.Total {
		return 1
	}
	return float64(progress.Completed) / float64(progress.Total)
}

// Label renders progress as a count rather than only as a bar.
//
// A bar alone cannot be read aloud, cannot be compared between two runs, and
// gives no sense of how much is left when the total is small.
func (progress Progress) Label() string {
	if progress.Total <= 0 {
		return "no steps"
	}
	return fmt.Sprintf("%d/%d steps", progress.Completed, progress.Total)
}

// ProgressOf totals a step sequence.
//
// A failed or skipped step counts as complete: it will not run again, and
// leaving it out would make a run that had stopped look like one still
// working.
func ProgressOf(steps []Step) Progress {
	progress := Progress{Total: len(steps)}
	for _, step := range steps {
		if step.State.Terminal() {
			progress.Completed++
		}
		if step.State == StepRunning && progress.Running == "" {
			progress.Running = step.Label
		}
	}
	return progress
}

// Severity classifies one emitted log line.
type Severity string

const (
	SeverityInfo    Severity = "info"
	SeverityRun     Severity = "run"
	SeverityPass    Severity = "pass"
	SeverityWarn    Severity = "warn"
	SeverityFailure Severity = "failure"
)

// AllSeverities returns every declared severity, in the order they are offered
// as filters.
func AllSeverities() []Severity {
	return []Severity{
		SeverityInfo, SeverityRun, SeverityPass, SeverityWarn, SeverityFailure,
	}
}

// Valid reports whether a severity is declared.
func (severity Severity) Valid() bool {
	for _, candidate := range AllSeverities() {
		if severity == candidate {
			return true
		}
	}
	return false
}

// Tag is the short marker shown beside a line.
func (severity Severity) Tag() string {
	switch severity {
	case SeverityRun:
		return "RUN"
	case SeverityPass:
		return "PASS"
	case SeverityWarn:
		return "WARN"
	case SeverityFailure:
		return "FAIL"
	default:
		return "INFO"
	}
}

// LogLine is one emitted line.
type LogLine struct {
	ID       string
	At       time.Time
	Severity Severity
	// Text is already redacted by the time it reaches this package. Nothing
	// here inspects it for secrets, because a presentation layer that decided
	// what was safe to show would be the wrong place to decide it.
	Text string
	// Duration is an optional measured elapsed time for the line's subject.
	Duration time.Duration
}

// Validate rejects an unpresentable line.
func (line LogLine) Validate() error {
	switch {
	case strings.TrimSpace(line.ID) == "":
		return errors.New("a log line requires a stable identity")
	case !line.Severity.Valid():
		return fmt.Errorf("log line %q has unknown severity %q", line.ID, line.Severity)
	case line.At.IsZero():
		return fmt.Errorf("log line %q records no time", line.ID)
	case strings.TrimSpace(line.Text) == "":
		return fmt.Errorf("log line %q has no text", line.ID)
	}
	return nil
}

// Filter selects which severities are shown.
//
// An empty filter shows everything. That is the deliberate default: a log
// filtered by accident is a log whose absence of errors means nothing.
type Filter struct {
	Selected map[Severity]bool
}

// Active reports whether the filter narrows anything.
func (filter Filter) Active() bool {
	for _, severity := range AllSeverities() {
		if filter.Selected[severity] {
			return true
		}
	}
	return false
}

// Admits reports whether a severity passes the filter.
func (filter Filter) Admits(severity Severity) bool {
	if !filter.Active() {
		return true
	}
	return filter.Selected[severity]
}

// Apply returns the lines a filter admits, and how many it hid.
//
// The hidden count is returned rather than discarded so the view can say "12
// lines hidden by a filter" instead of quietly showing a shorter log.
func (filter Filter) Apply(lines []LogLine) ([]LogLine, int) {
	if !filter.Active() {
		return lines, 0
	}
	admitted := make([]LogLine, 0, len(lines))
	hidden := 0
	for _, line := range lines {
		if filter.Admits(line.Severity) {
			admitted = append(admitted, line)
			continue
		}
		hidden++
	}
	return admitted, hidden
}

// Measurement is one figure in the strip above a run.
type Measurement struct {
	// Label names what was measured, in the reader's terms.
	Label string
	// Value is the figure as it should be shown, already formatted. This
	// package does not format money or durations: the units and precision are
	// decided where the value is known, not where it is drawn.
	Value string
	// Unknown marks a figure that has not been measured. It is distinct from
	// a zero value, because "no cost yet" and "costs nothing" are different
	// claims and only one of them is safe to make.
	Unknown bool
	// Tone optionally colours the value, for a figure whose state matters.
	Tone MeasurementTone
	// Trend is an optional short series for a sparkline, oldest first.
	Trend []float64
}

// MeasurementTone colours a figure whose state carries meaning.
type MeasurementTone string

const (
	ToneNeutral MeasurementTone = "neutral"
	ToneGood    MeasurementTone = "good"
	ToneCaution MeasurementTone = "caution"
	ToneBad     MeasurementTone = "bad"
)

// Display returns the text to show for a measurement.
//
// An unmeasured figure reads as "unknown" rather than as a dash or a zero. A
// dash is ambiguous and a zero is a claim.
func (measurement Measurement) Display() string {
	if measurement.Unknown || strings.TrimSpace(measurement.Value) == "" {
		return "unknown"
	}
	return measurement.Value
}

// Validate rejects a measurement that would mislead.
func (measurement Measurement) Validate() error {
	if strings.TrimSpace(measurement.Label) == "" {
		return errors.New("a measurement requires a label")
	}
	if !measurement.Unknown && strings.TrimSpace(measurement.Value) == "" {
		return fmt.Errorf("measurement %q is neither known nor marked unknown",
			measurement.Label)
	}
	return nil
}

// CurrentWork is what the run is executing right now.
type CurrentWork struct {
	Present bool
	Title   string
	Detail  string
	// Fraction is completion of this unit of work, between zero and one.
	Fraction float64
	Elapsed  time.Duration
	// Remaining is an estimate. RemainingKnown separates "we estimate zero"
	// from "we cannot estimate", which a bare zero would conflate.
	Remaining      time.Duration
	RemainingKnown bool
}

// FormatDuration renders a duration for a readout.
//
// Seconds resolution below an hour: a supervision console is watched over
// minutes, and millisecond precision in a running total is noise that changes
// too fast to read.
func FormatDuration(value time.Duration) string {
	if value < 0 {
		return "unknown"
	}
	total := int(value.Round(time.Second) / time.Second)
	hours := total / 3600
	minutes := (total % 3600) / 60
	seconds := total % 60
	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, seconds)
	}
	return fmt.Sprintf("%d:%02d", minutes, seconds)
}
