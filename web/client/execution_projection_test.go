package main

import (
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
	"codeflux.dev/codeflux/web/frontend/executionview"
	"codeflux.dev/codeflux/web/frontend/taskprojection"
	"codeflux.dev/codeflux/web/frontend/timeline"
)

func fixtureEvent(sequence uint64, kind events.Kind, second int) events.SessionEvent {
	return events.SessionEvent{
		Sequence:  sequence,
		Kind:      kind,
		Timestamp: time.Date(2026, 8, 1, 10, 43, second, 0, time.UTC),
	}
}

func TestAnUnloadedTaskShowsNoExecutionAtAll(t *testing.T) {
	// Empty panels would suggest a run that produced nothing rather than one
	// that has not begun. There is a real difference and the interface has to
	// keep it.
	if panels := projectExecution(
		taskprojection.TaskProjection{}, false, timeline.State{},
		executionview.Filter{}, false,
	); panels != nil {
		t.Fatalf("an unloaded task produced execution panels: %+v", panels)
	}
}

func TestAnUnreportedFigureIsUnknownRatherThanZero(t *testing.T) {
	// "No cost yet" and "costs nothing" are different claims. A budget the
	// coordinator has not reported must not read as a free run.
	panels := projectExecution(
		taskprojection.TaskProjection{State: domain.TaskStateRunning, Revision: 4},
		true, timeline.State{}, executionview.Filter{}, false,
	)
	if panels == nil {
		t.Fatal("a loaded task produced no execution panels")
	}
	byLabel := map[string]executionview.Measurement{}
	for _, measurement := range panels.Measurements {
		byLabel[measurement.Label] = measurement
	}
	for _, label := range []string{"cost", "budget"} {
		measurement, present := byLabel[label]
		if !present {
			t.Fatalf("no %s measurement was projected", label)
		}
		if !measurement.Unknown {
			t.Errorf("%s reads as %q for a task with no budget", label, measurement.Value)
		}
		if measurement.Display() != "unknown" {
			t.Errorf("%s displays as %q", label, measurement.Display())
		}
	}
	// Everything projected must be presentable.
	for _, measurement := range panels.Measurements {
		if err := measurement.Validate(); err != nil {
			t.Errorf("projected measurement %q is unusable: %v", measurement.Label, err)
		}
	}
}

func TestSpendAboveTheCapIsImpossibleToMiss(t *testing.T) {
	overspent := taskprojection.TaskProjection{
		State: domain.TaskStateRunning, Revision: 2,
		Budget: taskprojection.BudgetProjection{
			Present:   true,
			HardLimit: domain.Money{Currency: "USD", MinorUnits: 400},
			Actual:    domain.Money{Currency: "USD", MinorUnits: 512},
		},
	}
	panels := projectExecution(overspent, true, timeline.State{}, executionview.Filter{}, false)
	for _, measurement := range panels.Measurements {
		if measurement.Label != "cost" {
			continue
		}
		if measurement.Tone != executionview.ToneBad {
			t.Errorf("spend above the cap is toned %q", measurement.Tone)
		}
		if measurement.Value != "USD 5.12" {
			t.Errorf("cost rendered as %q, want the exact figure", measurement.Value)
		}
		return
	}
	t.Fatal("no cost measurement was projected")
}

func TestMoneyIsExactRatherThanRounded(t *testing.T) {
	// This is a figure somebody decides a budget on. A rounded one would be a
	// different number.
	for minor, want := range map[int64]string{
		0:     "USD 0.00",
		5:     "USD 0.05",
		42:    "USD 0.42",
		512:   "USD 5.12",
		10000: "USD 100.00",
		-250:  "-USD 2.50",
	} {
		got := formatMoney(domain.Money{Currency: "USD", MinorUnits: minor})
		if got != want {
			t.Errorf("formatMoney(%d) = %q, want %q", minor, got, want)
		}
	}
}

func TestStepsFollowTheCoordinatorsOwnOrder(t *testing.T) {
	// Event order is the only order the coordinator guarantees. Sorting by
	// timestamp would reorder two events sharing a millisecond and make a run
	// look like it did things in an order it did not.
	same := time.Date(2026, 8, 1, 10, 43, 7, 0, time.UTC)
	state := timeline.State{Events: []events.SessionEvent{
		{Sequence: 1, Kind: events.KindTaskStateChanged, Timestamp: same},
		{Sequence: 2, Kind: events.KindGraphPatch, Timestamp: same},
		{Sequence: 3, Kind: events.KindTaskStateChanged, Timestamp: same},
	}}
	steps := executionSteps(
		taskprojection.TaskProjection{State: domain.TaskStateRunning}, state,
	)
	if len(steps) != 3 {
		t.Fatalf("projected %d steps, want 3", len(steps))
	}
	for index, step := range steps {
		if err := step.Validate(); err != nil {
			t.Fatalf("step %d is unpresentable: %v", index, err)
		}
	}
	if steps[0].ID != "seq-1" || steps[2].ID != "seq-3" {
		t.Errorf("steps were reordered: %s, %s, %s", steps[0].ID, steps[1].ID, steps[2].ID)
	}
	// Only the last step of a running task is in flight; the rest are done.
	if steps[2].State != executionview.StepRunning {
		t.Errorf("the newest step of a running task is %q", steps[2].State)
	}
	if steps[0].State != executionview.StepDone {
		t.Errorf("an earlier step of a running task is %q", steps[0].State)
	}
}

func TestAFinishedTaskHasNothingInFlight(t *testing.T) {
	state := timeline.State{Events: []events.SessionEvent{
		fixtureEvent(1, events.KindTaskStateChanged, 1),
	}}
	for _, finished := range []domain.TaskState{
		domain.TaskStateAwaitingReview, domain.TaskStateCancelled, domain.TaskStateFailed,
	} {
		work := currentExecutionWork(taskprojection.TaskProjection{State: finished}, state)
		if work.Present {
			t.Errorf("a %s task reports work in flight", finished)
		}
	}
	// A running task with no events yet is honest about having started
	// without reporting.
	starting := currentExecutionWork(
		taskprojection.TaskProjection{State: domain.TaskStateRunning}, timeline.State{},
	)
	if !starting.Present || !strings.Contains(starting.Detail, "No step") {
		t.Errorf("a started run with no events said %+v", starting)
	}
}

func TestNoRemainingTimeIsInvented(t *testing.T) {
	// The coordinator does not forecast a per-step remaining time. A number
	// this layer made up would be one nobody could check.
	work := currentExecutionWork(
		taskprojection.TaskProjection{State: domain.TaskStateRunning},
		timeline.State{Events: []events.SessionEvent{
			fixtureEvent(1, events.KindTaskStateChanged, 1),
		}},
	)
	if work.RemainingKnown {
		t.Fatalf("an unforecast remaining time was reported as known: %+v", work)
	}
}

func TestEveryProjectedLogLineIsPresentable(t *testing.T) {
	state := timeline.State{Events: []events.SessionEvent{
		fixtureEvent(1, events.KindTaskStateChanged, 1),
		fixtureEvent(2, events.KindGraphPatch, 2),
	}}
	lines := executionLines(state)
	if len(lines) != 2 {
		t.Fatalf("projected %d lines, want 2", len(lines))
	}
	for _, line := range lines {
		if err := line.Validate(); err != nil {
			t.Errorf("projected line %q is unpresentable: %v", line.ID, err)
		}
	}
	// The label is the event's own kind rather than invented prose: nothing
	// here should put words in the coordinator's mouth.
	if strings.Contains(lines[0].Text, "_") {
		t.Errorf("the label was not made readable: %q", lines[0].Text)
	}
	if !strings.Contains(lines[0].Text, "task") {
		t.Errorf("the label lost the event kind: %q", lines[0].Text)
	}
}

func TestAnEventWithNoKindStillReadsAsSomething(t *testing.T) {
	// A blank label would render an empty row that a reader cannot act on and
	// a validator would reject.
	line := executionLines(timeline.State{
		Events: []events.SessionEvent{{Sequence: 9, Timestamp: fixtureEvent(9, "", 1).Timestamp}},
	})
	if len(line) != 1 {
		t.Fatalf("projected %d lines", len(line))
	}
	if err := line[0].Validate(); err != nil {
		t.Fatalf("an unnamed event produced an unpresentable line: %v", err)
	}
}

func TestElapsedNeverRunsBackwards(t *testing.T) {
	if got := elapsedSince(time.Time{}); got != 0 {
		t.Errorf("an unstarted step reported %s elapsed", got)
	}
	// A clock that stepped backwards must not produce a negative duration,
	// which would render as "unknown" and hide a step that is genuinely
	// running.
	if got := elapsedSince(time.Now().Add(time.Hour)); got != 0 {
		t.Errorf("a future timestamp reported %s elapsed", got)
	}
}
