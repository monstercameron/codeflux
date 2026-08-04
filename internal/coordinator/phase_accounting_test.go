package coordinator

import (
	"testing"
	"time"
)

// TestPhaseTimeIsTheUnionOfWhenItWasRunning is what makes the time report
// answerable at all.
//
// Two simpler answers were tried and both lie. Summing per-stage durations
// counts the same seconds many times, because stages inside a phase run
// concurrently and are re-decided on every attempt: it reported
// "specification 2991.96s 77.6% of measured" for a run lasting 371 seconds.
// Taking each phase's first-to-last span is no better, because phases
// interleave rather than run in sequence: two phases each claimed 100.0% of the
// run and the unaccounted remainder — the number the report exists for — came
// out as 0.00s.
func TestPhaseTimeIsTheUnionOfWhenItWasRunning(t *testing.T) {
	second := time.Second
	for _, testCase := range []struct {
		name    string
		windows []phaseInterval
		want    time.Duration
	}{
		{
			name:    "nothing ran",
			windows: nil,
			want:    0,
		},
		{
			name:    "one window is its own length",
			windows: []phaseInterval{{from: 2 * second, to: 5 * second}},
			want:    3 * second,
		},
		{
			// Concurrent stages. The sum would say six seconds; only four
			// elapsed.
			name: "overlapping windows count once",
			windows: []phaseInterval{
				{from: 0, to: 3 * second},
				{from: 1 * second, to: 4 * second},
			},
			want: 4 * second,
		},
		{
			// A phase re-decided on a later attempt. The span from first to
			// last would say ten seconds; the phase ran for four.
			name: "a gap between windows is not counted",
			windows: []phaseInterval{
				{from: 0, to: 2 * second},
				{from: 8 * second, to: 10 * second},
			},
			want: 4 * second,
		},
		{
			name: "a window inside another adds nothing",
			windows: []phaseInterval{
				{from: 0, to: 10 * second},
				{from: 3 * second, to: 4 * second},
			},
			want: 10 * second,
		},
		{
			// Order of arrival must not matter: stages finish in whatever
			// order they finish.
			name: "out-of-order windows merge the same way",
			windows: []phaseInterval{
				{from: 8 * second, to: 10 * second},
				{from: 0, to: 2 * second},
				{from: 1 * second, to: 3 * second},
			},
			want: 5 * second,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := mergedDuration(testCase.windows); got != testCase.want {
				t.Errorf("merged to %s, want %s", got, testCase.want)
			}
		})
	}
}

// TestMergedPhaseTimeNeverExceedsTheRun is the property that makes the
// remainder trustworthy.
//
// The unaccounted line is wall clock minus what the phases accounted for. If
// any phase can report more than elapsed, the remainder goes negative and the
// report either prints nonsense or, as it did, silently omits the one line
// worth reading.
func TestMergedPhaseTimeNeverExceedsTheRun(t *testing.T) {
	run := 10 * time.Second
	windows := []phaseInterval{
		{from: 0, to: run},
		{from: 0, to: run},
		{from: 2 * time.Second, to: 9 * time.Second},
		{from: 5 * time.Second, to: run},
	}
	if got := mergedDuration(windows); got > run {
		t.Errorf("a phase accounted for %s of a %s run", got, run)
	}
}

// TestTheRemainderIsWhatNoPhaseWasRunningFor is the report's whole purpose.
//
// Phases overlap each other, not only themselves: specification and delivery
// are both open for most of a run. Summing their per-phase unions therefore
// exceeds the wall clock and drives the remainder to zero — which is exactly
// what it printed, 0.00s, on a run whose model calls took four minutes.
//
// So each phase reports its own union, and the remainder is measured against
// the union of everything. The percentages can add to more than a hundred, and
// that is the honest shape of a concurrent flow rather than an error.
func TestTheRemainderIsWhatNoPhaseWasRunningFor(t *testing.T) {
	second := time.Second
	specification := []phaseInterval{{from: 0, to: 8 * second}}
	delivery := []phaseInterval{{from: 1 * second, to: 7 * second}}

	if got := mergedDuration(specification); got != 8*second {
		t.Fatalf("specification measured %s, want 8s", got)
	}
	if got := mergedDuration(delivery); got != 6*second {
		t.Fatalf("delivery measured %s, want 6s", got)
	}

	// Summed, the two phases claim fourteen seconds of a ten-second run and
	// leave nothing unaccounted. Unioned, they claim eight and leave two.
	everything := append(append([]phaseInterval(nil), specification...), delivery...)
	measured := mergedDuration(everything)
	if measured != 8*second {
		t.Fatalf("the union across phases measured %s, want 8s", measured)
	}
	run := 10 * second
	if remainder := run - measured; remainder != 2*second {
		t.Errorf("the remainder is %s, want 2s — the time no phase was "+
			"running, which is where model calls and tool runs live", remainder)
	}
}
