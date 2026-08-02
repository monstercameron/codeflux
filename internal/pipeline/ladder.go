package pipeline

import (
	"fmt"
	"strings"
)

// Effort is how hard a model is asked to think before it answers.
//
// It is a separate axis from the model and a much cheaper one to climb.
// Raising effort multiplies reasoning tokens, which are billed at the current
// model's rate; changing model multiplies the rate on every token. A ladder
// that exhausts effort on the cheap model before reaching for the expensive
// one costs less to climb and, on the evidence of the rung ladder, usually
// does not have to climb far.
const (
	EffortLow    = "low"
	EffortMedium = "medium"
	EffortHigh   = "high"
	EffortMax    = "max"
)

// KnownEfforts is every level, least to most.
func KnownEfforts() []string {
	return []string{EffortLow, EffortMedium, EffortHigh, EffortMax}
}

// Rung is one step of the escalation ladder: a model and how hard it thinks.
type Rung struct {
	Model  string
	Effort string
}

// String renders a rung the way it is stored and shown.
//
// One string rather than a pair, because the ladder is an ordered list of
// them and a list of pairs has no honest rendering in a settings surface that
// has to let a person reorder it.
func (rung Rung) String() string {
	return rung.Model + ":" + rung.Effort
}

// Describe renders a rung for a person choosing between them.
func (rung Rung) Describe() string {
	return rung.Model + " thinking at " + rung.Effort + " effort"
}

// ParseRung reads a stored rung.
func ParseRung(value string) (Rung, error) {
	model, effort, found := strings.Cut(value, ":")
	if !found {
		return Rung{}, fmt.Errorf(
			"%q is not a rung: a rung is a model and an effort level, "+
				"written model:effort", value)
	}
	if !contains(KnownModels(), model) {
		return Rung{}, fmt.Errorf("%q names the model %q, which is not one of %v",
			value, model, KnownModels())
	}
	if !contains(KnownEfforts(), effort) {
		return Rung{}, fmt.Errorf("%q names the effort %q, which is not one of %v",
			value, effort, KnownEfforts())
	}
	return Rung{Model: model, Effort: effort}, nil
}

// KnownRungs is every rung a ladder may name, cheapest first.
//
// Cheapest first means every level of the cheap model before any level of the
// expensive one, which is the ordering the cost argument implies: the most
// expensive thing luna can do still bills at luna's rate.
func KnownRungs() []string {
	var rungs []string
	for _, model := range KnownModels() {
		for _, effort := range KnownEfforts() {
			rungs = append(rungs, Rung{Model: model, Effort: effort}.String())
		}
	}
	return rungs
}

// DefaultLadder is the four rungs a run climbs, and what each one costs.
//
// Effort is exhausted on the cheap model before the expensive one is touched,
// because raising effort bills more tokens at the current rate while changing
// model raises the rate on every token. Reaching the second rung costs
// nothing extra per token; reaching the third multiplies the rate fivefold.
//
// Medium is skipped on each model on purpose. Every rung costs a full stall to
// detect — three attempts failing identically — so a rung that is only
// marginally different from the one below it is paid for in attempts and
// returns nothing. Low to max is a real change; low to medium to max is the
// same change with an extra stall in the middle.
func DefaultLadder() []string {
	return []string{
		Rung{Model: ModelLuna, Effort: EffortLow}.String(),
		Rung{Model: ModelLuna, Effort: EffortMax}.String(),
		Rung{Model: ModelSol, Effort: EffortLow}.String(),
		Rung{Model: ModelSol, Effort: EffortHigh}.String(),
	}
}

// DefaultApprovalRungs is the rungs nobody reaches without being asked.
//
// The top of the expensive model at maximum effort is the point where a run
// stops being an experiment and starts being a decision about money. It is
// not on the default ladder at all: a person who wants it adds it, and is
// asked before a run spends it.
func DefaultApprovalRungs() []string {
	return []string{Rung{Model: ModelSol, Effort: EffortMax}.String()}
}
