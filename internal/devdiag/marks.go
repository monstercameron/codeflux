package devdiag

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Mark is one browser performance measurement correlated with the durable
// event that caused it (M22-121).
//
// The correlation is the whole value. A render duration on its own says the UI
// was slow; a render duration attached to sequence 4,812 says which event made
// it slow, and that is a fact someone can act on.
type Mark struct {
	Sequence uint64
	Kind     string
	// ReducerDuration is time spent turning the event into new state.
	ReducerDuration time.Duration
	// RenderDuration is time spent turning that state into DOM.
	RenderDuration time.Duration
	// Boundaries names the render boundaries that actually re-rendered. An
	// event that re-rendered boundaries it does not own is a render-isolation
	// failure, and this is what makes that visible.
	Boundaries []string
}

// Total is the time attributable to this event.
func (mark Mark) Total() time.Duration {
	return mark.ReducerDuration + mark.RenderDuration
}

// Validate rejects a mark that could not have been measured.
func (mark Mark) Validate() error {
	if mark.Sequence == 0 {
		return errors.New("a performance mark must name the event sequence it belongs to")
	}
	if strings.TrimSpace(mark.Kind) == "" {
		return fmt.Errorf("mark for sequence %d has no event kind", mark.Sequence)
	}
	if mark.ReducerDuration < 0 || mark.RenderDuration < 0 {
		return fmt.Errorf("mark for sequence %d has a negative duration", mark.Sequence)
	}
	for _, boundary := range mark.Boundaries {
		if strings.TrimSpace(boundary) == "" {
			return fmt.Errorf("mark for sequence %d names an empty boundary", mark.Sequence)
		}
	}
	return nil
}

// MarkLedger collects browser performance marks (M22-121).
type MarkLedger struct {
	enabled bool
	marks   []Mark
	seen    map[uint64]bool
}

// NewMarkLedger builds a ledger. Like every other diagnostic here, it is off
// unless asked for.
func NewMarkLedger(enabled bool) *MarkLedger {
	return &MarkLedger{enabled: enabled, seen: map[uint64]bool{}}
}

// Enabled reports whether marks are being collected.
func (ledger *MarkLedger) Enabled() bool { return ledger.enabled }

// Add records one mark.
func (ledger *MarkLedger) Add(mark Mark) error {
	if err := mark.Validate(); err != nil {
		return err
	}
	if !ledger.enabled {
		return ErrDiagnosticsDisabled
	}
	if ledger.seen[mark.Sequence] {
		return fmt.Errorf("sequence %d already has a performance mark: a duplicate would "+
			"double-count the work one event caused", mark.Sequence)
	}
	ledger.seen[mark.Sequence] = true
	ledger.marks = append(ledger.marks, mark)
	return nil
}

// Marks returns everything recorded, ordered by sequence.
func (ledger *MarkLedger) Marks() []Mark {
	marks := make([]Mark, len(ledger.marks))
	copy(marks, ledger.marks)
	sort.Slice(marks, func(left, right int) bool {
		return marks[left].Sequence < marks[right].Sequence
	})
	return marks
}

// SlowestByTotal returns the marks costing the most, worst first, bounded by
// limit. This is the report a developer opens: not every mark, only the ones
// worth looking at.
func (ledger *MarkLedger) SlowestByTotal(limit int) []Mark {
	if limit <= 0 {
		return nil
	}
	marks := ledger.Marks()
	sort.SliceStable(marks, func(left, right int) bool {
		if marks[left].Total() != marks[right].Total() {
			return marks[left].Total() > marks[right].Total()
		}
		// Ties break by sequence so the report is stable between runs.
		return marks[left].Sequence < marks[right].Sequence
	})
	if len(marks) > limit {
		marks = marks[:limit]
	}
	return marks
}

// BoundaryChurn counts how many times each render boundary re-rendered.
//
// A boundary appearing far more often than the events that own it is the
// signature of a render-isolation regression: something is re-rendering on
// state it does not depend on.
func (ledger *MarkLedger) BoundaryChurn() map[string]int {
	churn := map[string]int{}
	for _, mark := range ledger.marks {
		for _, boundary := range mark.Boundaries {
			churn[boundary]++
		}
	}
	return churn
}

// UnexpectedBoundaries returns boundaries that re-rendered for an event kind
// they do not own (M22-121).
//
// ownership maps an event kind to the boundaries legitimately affected by it.
// A kind absent from the map is not checked, so a partially specified
// ownership map narrows the check rather than producing false findings.
func (ledger *MarkLedger) UnexpectedBoundaries(ownership map[string][]string) []string {
	var unexpected []string
	for _, mark := range ledger.marks {
		owned, known := ownership[mark.Kind]
		if !known {
			continue
		}
		allowed := make(map[string]bool, len(owned))
		for _, boundary := range owned {
			allowed[boundary] = true
		}
		for _, boundary := range mark.Boundaries {
			if !allowed[boundary] {
				unexpected = append(unexpected, fmt.Sprintf(
					"sequence %d (%s) re-rendered %s, which it does not own",
					mark.Sequence, mark.Kind, boundary))
			}
		}
	}
	sort.Strings(unexpected)
	return unexpected
}

// Summary renders a one-line report per mark, safe to log: it carries
// sequences, kinds, durations, and boundary names, and no user content.
func (ledger *MarkLedger) Summary(limit int) []string {
	slowest := ledger.SlowestByTotal(limit)
	lines := make([]string, 0, len(slowest))
	for _, mark := range slowest {
		lines = append(lines, fmt.Sprintf(
			"seq %d %s: reducer %s, render %s, boundaries [%s]",
			mark.Sequence, mark.Kind, mark.ReducerDuration, mark.RenderDuration,
			strings.Join(mark.Boundaries, " ")))
	}
	return lines
}
