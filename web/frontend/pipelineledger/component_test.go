package pipelineledger

import (
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/pipeline"
	"codeflux.dev/codeflux/web/frontend/primitives"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func render(t *testing.T, node ui.Node) string {
	t.Helper()
	markup, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

// TestLedgerCardCoversLoadingEmptyErrorAndUnavailableStates walks every
// non-ready state the surface can reach, per AGENTS.md's Frontend Rules: a
// data-owning component needs loading, empty, error, and disconnected states,
// not only the one it was built in.
func TestLedgerCardCoversLoadingEmptyErrorAndUnavailableStates(t *testing.T) {
	cases := []struct {
		name  string
		props Props
		want  []string
		deny  []string
	}{
		{
			name:  "unavailable",
			props: Props{},
			want:  []string{`data-state="unavailable"`, "No task selected"},
		},
		{
			name:  "loading",
			props: Props{State: Loading},
			want:  []string{`data-state="loading"`, "Reading the pipeline ledger"},
		},
		{
			name: "failed",
			props: Props{
				State: Failed, ErrorMessage: "the coordinator refused the read", OnRetry: func() {},
			},
			want: []string{`data-state="failed"`, "the coordinator refused the read", "Try again"},
		},
		{
			name:  "failed with no message falls back to an honest default",
			props: Props{State: Failed},
			want:  []string{"The coordinator did not answer the pipeline ledger read"},
		},
		{
			name:  "ready with no rows is a real empty state, not an error",
			props: Props{State: Ready, Attempt: 1},
			want:  []string{`data-state="empty"`, "No stages recorded yet"},
			deny:  []string{"pipeline ledger unavailable", "Pipeline ledger unavailable"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			markup := render(t, ui.CreateElement(LedgerCard, testCase.props))
			for _, want := range testCase.want {
				if !strings.Contains(markup, want) {
					t.Errorf("markup missing %q\n%s", want, markup)
				}
			}
			for _, deny := range testCase.deny {
				if strings.Contains(markup, deny) {
					t.Errorf("markup unexpectedly contains %q\n%s", deny, markup)
				}
			}
		})
	}
}

// TestLedgerCardDistinguishesUnmeasuredFromInstant is PIPE-006a's central
// requirement: a stage whose start was never tracked must read as "not
// measured", never as an elapsed value of zero, and the distinction must not
// depend on colour -- it renders as a differently labelled badge, not a
// differently coloured one carrying the same word.
func TestLedgerCardDistinguishesUnmeasuredFromInstant(t *testing.T) {
	rows := []StageRow{
		{
			Number: 1, Name: "instructions", State: pipeline.StateSatisfied,
			FinishedAt:      time.Date(2026, 8, 1, 12, 0, 1, 0, time.UTC),
			ElapsedMeasured: true, Elapsed: 0,
		},
		{
			Number: 2, Name: "clarification", State: pipeline.StateSatisfied,
			FinishedAt:      time.Date(2026, 8, 1, 12, 0, 1, 0, time.UTC),
			ElapsedMeasured: false, Elapsed: 0,
		},
	}
	markup := render(t, ui.CreateElement(LedgerCard, Props{State: Ready, Attempt: 1, Rows: rows}))
	if !strings.Contains(markup, "Not measured") {
		t.Errorf("unmeasured stage must render an explicit \"Not measured\" marker\n%s", markup)
	}
	if !strings.Contains(markup, `data-elapsed-measured="false"`) {
		t.Errorf("unmeasured row must carry elapsed-measured=false\n%s", markup)
	}
	if !strings.Contains(markup, `data-elapsed-measured="true"`) {
		t.Errorf("measured row must carry elapsed-measured=true\n%s", markup)
	}
	// The measured zero-duration stage must show its real, honest zero and
	// must not be collapsed into the same "Not measured" text the unmeasured
	// stage uses -- a genuinely instant stage and an untracked one are
	// different facts. Badge renders its label as both a title attribute and
	// body text, so one unmeasured stage yields exactly two occurrences, not
	// one; a third would mean the measured stage was mislabelled too.
	if count := strings.Count(markup, "Not measured"); count != 2 {
		t.Errorf(`"Not measured" appeared %d times, want exactly 2 (title + text) for one unmeasured stage`+"\n%s", count, markup)
	}
	if !strings.Contains(markup, "0s") {
		t.Errorf("measured zero-duration stage should show its real elapsed value\n%s", markup)
	}
}

// TestLedgerCardKeepsThreeSkipClassificationsSeparate is PIPE-044's central
// requirement: a principled decline, a no-check-by-design stage, and an
// unimplemented stage must each appear under their own label, never
// collapsed into the single skip-ratio number.
func TestLedgerCardKeepsThreeSkipClassificationsSeparate(t *testing.T) {
	rows := rowsFromFlow(t)
	rows = setState(rows, pipeline.StageAtomOptimization, pipeline.StateSkipped)
	rows = setState(rows, pipeline.StageHumanAcceptance, pipeline.StateSkipped)
	rows = setState(rows, pipeline.StageAtomFuzz, pipeline.StateNotImplemented)

	markup := render(t, ui.CreateElement(LedgerCard, Props{State: Ready, Attempt: 3, Rows: rows}))
	for _, want := range []string{
		`data-component="pipeline-skip-headline"`,
		`data-component="pipeline-skip-breakdown"`,
		"Declined on principle",
		"No check by design",
		"Not implemented",
		"PIPE-010", // the governing ticket for atom-optimization's decline
		"atom-optimization",
		"human-acceptance",
		"atom-fuzz",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("markup missing %q\n%s", want, markup)
		}
	}
	// The three groups must be distinguishable by more than colour: each
	// carries its own data-classification attribute.
	for _, group := range []string{
		`data-classification="principled-decline"`,
		`data-classification="no-check-by-design"`,
		`data-classification="unimplemented"`,
	} {
		if !strings.Contains(markup, group) {
			t.Errorf("markup missing group marker %q\n%s", group, markup)
		}
	}
}

// TestLedgerCardHandlesTheLongStringCase keeps an unusually long stage name
// fully present in the DOM rather than silently truncated, matching
// AGENTS.md's requirement to cover the long-string case that breaks layouts.
func TestLedgerCardHandlesTheLongStringCase(t *testing.T) {
	longName := strings.Repeat("extraordinarily-long-stage-name-", 8) + "end"
	rows := []StageRow{{
		Number: 1, Name: longName, State: pipeline.StateSatisfied,
		ElapsedMeasured: true, Elapsed: time.Millisecond,
	}}
	markup := render(t, ui.CreateElement(LedgerCard, Props{State: Ready, Attempt: 1, Rows: rows}))
	if !strings.Contains(markup, longName) {
		t.Errorf("long stage name must render in full, not truncated\n%s", markup)
	}
}

// TestRunCompletionCaveatIsSilentWithoutAComputedSummary keeps the timeline
// caveat from claiming a ratio when none could honestly be computed.
func TestRunCompletionCaveatIsSilentWithoutAComputedSummary(t *testing.T) {
	node := RunCompletionCaveat(SkipSummary{}, primitives.Mode{})
	if node != nil {
		t.Errorf("an uncomputed summary must render nothing, got %v", node)
	}
}

// TestRunCompletionCaveatNamesTheRatio is PIPE-044's final-message
// requirement: a reader of the timeline, not only someone who opens the
// ledger, must see what fraction of the run's stages established nothing.
func TestRunCompletionCaveatNamesTheRatio(t *testing.T) {
	rows := rowsFromFlow(t)
	rows = setState(rows, pipeline.StageAtomOptimization, pipeline.StateSkipped)
	summary := ComputeSkipSummary(rows)
	markup := render(t, RunCompletionCaveat(summary, primitives.Mode{}))
	if !strings.Contains(markup, `data-component="run-completion-caveat"`) {
		t.Errorf("caveat missing its component marker\n%s", markup)
	}
	if !strings.Contains(markup, "established, 1 declined, 0 not implemented") {
		t.Errorf("caveat must name established/declined/not-implemented counts\n%s", markup)
	}
}
