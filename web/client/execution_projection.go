package main

import (
	"strings"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
	"codeflux.dev/codeflux/web/frontend/executionview"
	"codeflux.dev/codeflux/web/frontend/shell"
	"codeflux.dev/codeflux/web/frontend/taskprojection"
	"codeflux.dev/codeflux/web/frontend/timeline"
)

// projectExecution turns authoritative task and event state into the surfaces
// that show what a run is doing.
//
// It is a pure function of durable state. Nothing here invents a measurement:
// a figure the coordinator has not reported is marked unknown rather than
// shown as a zero, because "no cost yet" and "costs nothing" are different
// claims and only one of them is safe to make.
func projectExecution(
	task taskprojection.TaskProjection,
	ready bool,
	events timeline.State,
	filter executionview.Filter,
	streaming bool,
) *shell.ExecutionPanelProps {
	if !ready {
		// A task nobody has loaded has no execution to show. Returning empty
		// panels would suggest a run that produced nothing rather than one
		// that has not begun.
		return nil
	}
	return &shell.ExecutionPanelProps{
		Measurements: executionMeasurements(task),
		Steps:        executionSteps(task, events),
		Lines:        executionLines(events),
		Filter:       filter,
		Streaming:    streaming,
		Current:      currentExecutionWork(task, events),
	}
}

// executionMeasurements builds the figures above a run.
func executionMeasurements(task taskprojection.TaskProjection) []executionview.Measurement {
	state := executionview.Measurement{Label: "state", Value: string(task.State)}
	if strings.TrimSpace(string(task.State)) == "" {
		state = executionview.Measurement{Label: "state", Unknown: true}
	}
	switch task.State {
	case domain.TaskStateRunning:
		state.Tone = executionview.ToneGood
	case domain.TaskStateFailed, domain.TaskStateCancelled:
		state.Tone = executionview.ToneBad
	case domain.TaskStatePaused, domain.TaskStateAwaitingPlanApproval,
		domain.TaskStateAwaitingAuthority, domain.TaskStateAwaitingReview:
		state.Tone = executionview.ToneCaution
	}

	cost := executionview.Measurement{Label: "cost", Unknown: true}
	budget := executionview.Measurement{Label: "budget", Unknown: true}
	if task.Budget.Present {
		cost = executionview.Measurement{
			Label: "cost", Value: formatMoney(task.Budget.Actual),
		}
		budget = executionview.Measurement{
			Label: "budget", Value: formatMoney(task.Budget.HardLimit),
		}
		// Spend above the cap is the one budget state that must be impossible
		// to miss.
		if task.Budget.Actual.MinorUnits > task.Budget.HardLimit.MinorUnits {
			cost.Tone = executionview.ToneBad
		}
	}

	revision := executionview.Measurement{Label: "revision", Unknown: true}
	if task.Revision > 0 {
		revision = executionview.Measurement{
			Label: "revision", Value: formatRevision(task.Revision),
		}
	}

	approval := executionview.Measurement{Label: "approval", Value: "none"}
	if task.Approval.Present {
		approval = executionview.Measurement{
			Label: "approval", Value: "waiting", Tone: executionview.ToneCaution,
		}
	}
	return []executionview.Measurement{state, cost, budget, approval, revision}
}

func formatRevision(revision uint64) string {
	digits := ""
	value := revision
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	if digits == "" {
		digits = "0"
	}
	return "r" + digits
}

// executionSteps derives the ordered steps from durable events.
//
// The order is the event order, which is the only order the coordinator
// guarantees. Sorting by time would reorder two events that share a
// millisecond and make a run look like it did things in an order it did not.
func executionSteps(
	task taskprojection.TaskProjection,
	events timeline.State,
) []executionview.Step {
	steps := make([]executionview.Step, 0, len(events.Events))
	for index, event := range events.Events {
		state := executionview.StepDone
		if index == len(events.Events)-1 && task.State == domain.TaskStateRunning {
			state = executionview.StepRunning
		}
		steps = append(steps, executionview.Step{
			ID:    eventIdentity(event),
			Label: eventLabel(event),
			State: state,
			At:    event.Timestamp,
		})
	}
	return steps
}

// executionLines derives the streaming log from durable events.
func executionLines(events timeline.State) []executionview.LogLine {
	lines := make([]executionview.LogLine, 0, len(events.Events))
	for _, event := range events.Events {
		lines = append(lines, executionview.LogLine{
			ID:       eventIdentity(event),
			At:       event.Timestamp,
			Severity: severityOfEvent(event),
			Text:     eventLabel(event),
		})
	}
	return lines
}

// currentExecutionWork reports what is in flight.
func currentExecutionWork(
	task taskprojection.TaskProjection,
	events timeline.State,
) executionview.CurrentWork {
	if task.State != domain.TaskStateRunning {
		return executionview.CurrentWork{}
	}
	if len(events.Events) == 0 {
		return executionview.CurrentWork{
			Present: true, Title: "Starting", Detail: "No step has reported yet.",
		}
	}
	latest := events.Events[len(events.Events)-1]
	progress := executionview.ProgressOf(executionSteps(task, events))
	return executionview.CurrentWork{
		Present: true,
		Title:   eventLabel(latest),
		Detail:  eventIdentity(latest),
		// Completion within the current step is not measured, so the bar
		// carries run progress instead of inventing a per-step fraction.
		Fraction: progress.Fraction(),
		Elapsed:  elapsedSince(latest.Timestamp),
		// The coordinator does not forecast a per-step remaining time, and an
		// estimate this layer invented would be a number nobody could check.
		RemainingKnown: false,
	}
}

// elapsedSince is separated so a test can reason about it without a clock.
func elapsedSince(start time.Time) time.Duration {
	if start.IsZero() {
		return 0
	}
	elapsed := time.Since(start)
	if elapsed < 0 {
		return 0
	}
	return elapsed
}

// eventIdentity names one durable event for the view.
//
// The sequence is used rather than a generated key: it is the coordinator's
// own ordering, it is stable across a reload, and two views of the same run
// therefore identify the same line the same way.
func eventIdentity(event events.SessionEvent) string {
	return "seq-" + formatUint(event.Sequence)
}

// eventLabel renders what an event says, in the reader's terms.
func eventLabel(event events.SessionEvent) string {
	kind := strings.TrimSpace(string(event.Kind))
	if kind == "" {
		return "unnamed event"
	}
	// The kind is the honest label. Inventing prose here would put words in
	// the coordinator's mouth that no durable record contains.
	return strings.ReplaceAll(kind, "_", " ")
}

// severityOfEvent classifies an event for the log.
//
// Only kinds that genuinely report a failure are coloured as one. Guessing
// from a substring would let an event named "failure_recovery_succeeded" read
// as a failure.
func severityOfEvent(event events.SessionEvent) executionview.Severity {
	switch event.Kind {
	case events.KindTaskStateChanged:
		return executionview.SeverityRun
	case events.KindGraphPatch:
		return executionview.SeverityInfo
	default:
		return executionview.SeverityInfo
	}
}

// formatMoney renders an exact monetary value.
//
// Minor units are divided out rather than approximated: this is a figure a
// person decides a budget on, and a rounded one would be a different number.
func formatMoney(value domain.Money) string {
	units := value.MinorUnits
	sign := ""
	if units < 0 {
		sign = "-"
		units = -units
	}
	whole := units / 100
	fraction := units % 100
	return sign + string(value.Currency) + " " +
		formatUint(uint64(whole)) + "." + twoDigits(uint64(fraction))
}

func formatUint(value uint64) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}

func twoDigits(value uint64) string {
	rendered := formatUint(value)
	if len(rendered) < 2 {
		return "0" + rendered
	}
	return rendered
}
