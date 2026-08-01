package executionview

import (
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/web/frontend/design"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func testMode(t *testing.T) primitives.Mode {
	t.Helper()
	return primitives.Mode{Theme: design.ThemeDark, Density: design.DensityComfortable}
}

func render(t *testing.T, node ui.Node) string {
	t.Helper()
	markup, err := ui.RenderToString(node)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return markup
}

func fixedTime(second int) time.Time {
	return time.Date(2026, 8, 1, 10, 43, second, 0, time.UTC)
}

func TestAStepThatStartedMustSayWhen(t *testing.T) {
	// A running or finished step with no time shows a run whose order nobody
	// can check, which is the one thing a timeline is for.
	valid := Step{ID: "s1", Label: "Run tests", State: StepRunning, At: fixedTime(1)}
	if err := valid.Validate(); err != nil {
		t.Fatalf("a complete step was rejected: %v", err)
	}
	// A pending step legitimately has no time.
	pending := Step{ID: "s2", Label: "Validate gates", State: StepPending}
	if err := pending.Validate(); err != nil {
		t.Fatalf("a pending step was required to have a time: %v", err)
	}
	for name, damage := range map[string]func(*Step){
		"no identity": func(step *Step) { step.ID = " " },
		"no label":    func(step *Step) { step.Label = "" },
		"unknown state": func(step *Step) {
			step.State = "halfway"
		},
		"started with no time": func(step *Step) { step.At = time.Time{} },
	} {
		t.Run(name, func(t *testing.T) {
			step := valid
			damage(&step)
			if err := step.Validate(); err == nil {
				t.Fatalf("a step with %s was accepted", name)
			}
		})
	}
}

func TestProgressCountsWorkThatWillNotRunAgain(t *testing.T) {
	// A failed or skipped step is complete. Leaving them out makes a run that
	// has stopped look like one still working.
	steps := []Step{
		{ID: "1", Label: "Plan", State: StepDone, At: fixedTime(1)},
		{ID: "2", Label: "Edit", State: StepFailed, At: fixedTime(2)},
		{ID: "3", Label: "Skip", State: StepSkipped, At: fixedTime(3)},
		{ID: "4", Label: "Test", State: StepRunning, At: fixedTime(4)},
		{ID: "5", Label: "Gate", State: StepPending},
	}
	progress := ProgressOf(steps)
	if progress.Completed != 3 || progress.Total != 5 {
		t.Fatalf("progress = %d/%d, want 3/5", progress.Completed, progress.Total)
	}
	if progress.Running != "Test" {
		t.Errorf("running step = %q, want Test", progress.Running)
	}
	if got := progress.Label(); got != "3/5 steps" {
		t.Errorf("label = %q", got)
	}
	if got := progress.Fraction(); got < 0.59 || got > 0.61 {
		t.Errorf("fraction = %v, want about 0.6", got)
	}
	// An empty run is not zero percent of something; it has nothing to total.
	empty := ProgressOf(nil)
	if empty.Fraction() != 0 || empty.Label() != "no steps" {
		t.Errorf("empty progress = %+v", empty)
	}
}

func TestAnEmptyFilterShowsEverything(t *testing.T) {
	// A log filtered by accident is a log whose absence of errors means
	// nothing, so the default must be to hide nothing.
	lines := []LogLine{
		{ID: "1", At: fixedTime(1), Severity: SeverityInfo, Text: "a"},
		{ID: "2", At: fixedTime(2), Severity: SeverityFailure, Text: "b"},
	}
	admitted, hidden := Filter{}.Apply(lines)
	if len(admitted) != 2 || hidden != 0 {
		t.Fatalf("an empty filter hid %d of %d lines", hidden, len(lines))
	}
	if (Filter{}).Active() {
		t.Error("an empty filter reported itself active")
	}
	if (Filter{Selected: map[Severity]bool{SeverityWarn: false}}).Active() {
		t.Error("a filter with nothing selected reported itself active")
	}
}

func TestAFilterReportsWhatItHid(t *testing.T) {
	lines := []LogLine{
		{ID: "1", At: fixedTime(1), Severity: SeverityInfo, Text: "a"},
		{ID: "2", At: fixedTime(2), Severity: SeverityFailure, Text: "b"},
		{ID: "3", At: fixedTime(3), Severity: SeverityInfo, Text: "c"},
	}
	filter := Filter{Selected: map[Severity]bool{SeverityFailure: true}}
	admitted, hidden := filter.Apply(lines)
	if len(admitted) != 1 || admitted[0].ID != "2" {
		t.Fatalf("filter admitted %+v", admitted)
	}
	if hidden != 2 {
		t.Fatalf("filter hid %d lines, want 2", hidden)
	}
}

func TestAnUnmeasuredFigureIsNotZero(t *testing.T) {
	// "No cost yet" and "costs nothing" are different claims, and only one of
	// them is safe to make.
	unknown := Measurement{Label: "Cost", Unknown: true}
	if got := unknown.Display(); got != "unknown" {
		t.Errorf("an unmeasured figure displayed as %q", got)
	}
	if err := unknown.Validate(); err != nil {
		t.Errorf("an explicitly unknown figure was rejected: %v", err)
	}
	// A figure that is neither known nor marked unknown is a gap nobody
	// declared.
	if err := (Measurement{Label: "Cost"}).Validate(); err == nil {
		t.Error("a figure with no value and no unknown marker was accepted")
	}
	if err := (Measurement{Value: "$0.42"}).Validate(); err == nil {
		t.Error("a figure with no label was accepted")
	}
	known := Measurement{Label: "Cost", Value: "$0.42"}
	if got := known.Display(); got != "$0.42" {
		t.Errorf("a measured figure displayed as %q", got)
	}
}

func TestDurationsReadAtTheSpeedTheyChange(t *testing.T) {
	for value, want := range map[time.Duration]string{
		0:                "0:00",
		41 * time.Second: "0:41",
		95 * time.Second: "1:35",
		time.Hour + 2*time.Minute + 3*time.Second: "1:02:03",
		-time.Second: "unknown",
	} {
		if got := FormatDuration(value); got != want {
			t.Errorf("FormatDuration(%s) = %q, want %q", value, got, want)
		}
	}
}

func TestTheMetricStripNamesEveryFigureAndItsState(t *testing.T) {
	markup := render(t, MetricStrip(MetricStripProps{
		Mode: testMode(t),
		Measurements: []Measurement{
			{Label: "Correctness", Value: "High", Tone: ToneGood},
			{Label: "Cost", Unknown: true},
			{Label: "Effort", Value: "2.1 min", Trend: []float64{3, 2, 4, 2.5}},
		},
	}))
	for _, required := range []string{"Correctness", "High", "Cost", "unknown", "Effort"} {
		if !strings.Contains(markup, required) {
			t.Errorf("the strip omits %q", required)
		}
	}
	// An unmeasured figure is marked in the DOM, so a test or a reader can
	// tell it apart from a measured one without reading its colour.
	if !strings.Contains(markup, `data-unknown="true"`) {
		t.Error("the strip does not mark its unmeasured figure")
	}
	if !strings.Contains(markup, "<polyline") {
		t.Error("a figure with a trend drew no sparkline")
	}
	// A flat series must still draw, and must not divide by zero doing it.
	flat := render(t, MetricStrip(MetricStripProps{
		Mode:         testMode(t),
		Measurements: []Measurement{{Label: "Flat", Value: "1", Trend: []float64{1, 1, 1}}},
	}))
	if !strings.Contains(flat, "<polyline") {
		t.Error("a flat trend drew no sparkline")
	}
}

func TestTheTimelineNamesEveryStateNotOnlyColoursIt(t *testing.T) {
	// A reader who cannot separate the colours must still be able to tell a
	// finished step from a failed one.
	markup := render(t, ExecutionTimeline(TimelineProps{
		Mode: testMode(t),
		Steps: []Step{
			{ID: "1", Label: "Plan generated", State: StepDone, At: fixedTime(1)},
			{ID: "2", Label: "Tests started", State: StepRunning, At: fixedTime(41)},
			{ID: "3", Label: "Validating gates", State: StepPending},
		},
	}))
	for _, required := range []string{
		"Plan generated", "done", "Tests started", "running", "Validating gates", "pending",
	} {
		if !strings.Contains(markup, required) {
			t.Errorf("the timeline omits %q", required)
		}
	}
	if !strings.Contains(markup, `data-completed="1"`) ||
		!strings.Contains(markup, `data-total="3"`) {
		t.Error("the timeline does not publish its own totals")
	}
	if !strings.Contains(markup, `role="progressbar"`) {
		t.Error("the timeline shows no progress")
	}
}

func TestAnUnpresentableStepIsReportedNotDropped(t *testing.T) {
	// A timeline that silently loses a step reads as a shorter run.
	markup := render(t, ExecutionTimeline(TimelineProps{
		Mode:  testMode(t),
		Steps: []Step{{ID: "", Label: "Nameless", State: StepDone, At: fixedTime(1)}},
	}))
	if !strings.Contains(markup, "cannot be shown") {
		t.Fatalf("an invalid step vanished from the timeline: %s", markup)
	}
}

func TestTheLogSaysWhenAFilterIsHidingLines(t *testing.T) {
	mode := testMode(t)
	lines := []LogLine{
		{ID: "1", At: fixedTime(1), Severity: SeverityInfo, Text: "using seed data"},
		{ID: "2", At: fixedTime(2), Severity: SeverityFailure, Text: "assertion failed"},
	}
	markup := render(t, StreamingLog(LogProps{
		Mode: mode, Lines: lines, Streaming: true,
		Filter: Filter{Selected: map[Severity]bool{SeverityFailure: true}},
	}))
	if !strings.Contains(markup, "assertion failed") {
		t.Error("the admitted line is missing")
	}
	if strings.Contains(markup, "using seed data") {
		t.Error("a filtered line was still shown")
	}
	if !strings.Contains(markup, "1 line(s) hidden") {
		t.Errorf("the log does not say a filter is hiding lines: %s", markup)
	}
	if !strings.Contains(markup, `data-streaming="true"`) {
		t.Error("a streaming log does not say it is still receiving")
	}
}

func TestAnEmptyLogExplainsWhichKindOfEmptyItIs(t *testing.T) {
	mode := testMode(t)
	for name, expectation := range map[string]struct {
		props LogProps
		want  string
	}{
		"waiting": {
			LogProps{Mode: mode, Streaming: true}, "Waiting for the first line",
		},
		"finished with nothing": {
			LogProps{Mode: mode}, "emitted no log lines",
		},
		"everything filtered": {
			LogProps{
				Mode:   mode,
				Lines:  []LogLine{{ID: "1", At: fixedTime(1), Severity: SeverityInfo, Text: "a"}},
				Filter: Filter{Selected: map[Severity]bool{SeverityFailure: true}},
			},
			"hidden by the current filter",
		},
	} {
		t.Run(name, func(t *testing.T) {
			markup := render(t, StreamingLog(expectation.props))
			if !strings.Contains(markup, expectation.want) {
				t.Fatalf("empty log did not explain itself (%s): %s", name, markup)
			}
		})
	}
}

func TestEverySeverityIsOfferedAsAFilter(t *testing.T) {
	toggled := []Severity{}
	markup := render(t, StreamingLog(LogProps{
		Mode:     testMode(t),
		OnToggle: func(severity Severity) { toggled = append(toggled, severity) },
	}))
	for _, severity := range AllSeverities() {
		if !strings.Contains(markup, `data-severity="`+strings.ToLower(severity.Tag())+`"`) {
			t.Errorf("severity %q is not offered as a filter", severity)
		}
	}
	if !strings.Contains(markup, `data-severity="all"`) {
		t.Error("there is no way to clear the filter")
	}
	_ = toggled
}

func TestCurrentWorkSeparatesNoEstimateFromZero(t *testing.T) {
	mode := testMode(t)
	unknown := render(t, CurrentlyExecuting(CurrentWorkProps{
		Mode: mode,
		Work: Work{
			Present: true, Title: "Tests", Detail: "test_idempotent_order_creation",
			Fraction: 0.62, Elapsed: 41 * time.Second,
		},
	}))
	if !strings.Contains(unknown, "unknown") {
		t.Error("an unestimated remaining time did not read as unknown")
	}
	if !strings.Contains(unknown, "0:41") {
		t.Error("the elapsed time is missing")
	}
	if !strings.Contains(unknown, `aria-valuenow="62"`) {
		t.Errorf("progress was not published: %s", unknown)
	}

	known := render(t, CurrentlyExecuting(CurrentWorkProps{
		Mode: mode,
		Work: Work{
			Present: true, Title: "Tests", Fraction: 0.5,
			Remaining: 25 * time.Second, RemainingKnown: true,
		},
	}))
	if !strings.Contains(known, "0:25") {
		t.Error("a known remaining time is missing")
	}

	idle := render(t, CurrentlyExecuting(CurrentWorkProps{Mode: mode}))
	if !strings.Contains(idle, "Nothing is executing") {
		t.Errorf("an idle run did not say so: %s", idle)
	}
	if !strings.Contains(idle, `data-present="false"`) {
		t.Error("an idle run is not marked in the DOM")
	}
}

func TestProgressIsClampedRatherThanDrawnOutsideItsTrack(t *testing.T) {
	mode := testMode(t)
	for fraction, want := range map[float64]string{
		-1: `aria-valuenow="0"`,
		2:  `aria-valuenow="100"`,
	} {
		markup := render(t, CurrentlyExecuting(CurrentWorkProps{
			Mode: mode, Work: Work{Present: true, Title: "Work", Fraction: fraction},
		}))
		if !strings.Contains(markup, want) {
			t.Errorf("fraction %v was not clamped: want %s", fraction, want)
		}
	}
}
