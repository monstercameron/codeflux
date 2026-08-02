package pipeline

import "slices"

// Phase is one movement of the flow, named for what it builds rather than for
// where it sits.
//
// The stage list already documents seven phases in its comments. Naming them
// as values lets evidence be summarised at the granularity a person actually
// asks about — "what did the atoms cost" — without collapsing stages whose
// costs are not comparable. Phases are deliberately not folded into a coarser
// three, because control flow and verification consume real model time and
// attributing that time to "program" would say the assembly was expensive when
// the checking was.
type Phase string

const (
	// PhaseSpecification is stages 1-6: deciding what to build.
	PhaseSpecification Phase = "specification"
	// PhaseAtoms is stages 7-17: the smallest independently testable units.
	PhaseAtoms Phase = "atoms"
	// PhaseMolecules is stages 18-21: compositions of atoms and the
	// obligations composition creates.
	PhaseMolecules Phase = "molecules"
	// PhaseControlFlow is stages 22-25: ordering, termination, and the failure
	// paths.
	PhaseControlFlow Phase = "control-flow"
	// PhaseProgram is stages 26-29: assembly through end-to-end exercise.
	PhaseProgram Phase = "program"
	// PhaseVerification is stages 30-34: whether the checking was worth
	// anything.
	PhaseVerification Phase = "verification"
	// PhaseDelivery is stages 35-37: evidence, acceptance, and handover.
	PhaseDelivery Phase = "delivery"
)

// Phases is every phase in flow order.
var Phases = []Phase{
	PhaseSpecification,
	PhaseAtoms,
	PhaseMolecules,
	PhaseControlFlow,
	PhaseProgram,
	PhaseVerification,
	PhaseDelivery,
}

// phaseBounds is the last stage number belonging to each phase, in order.
//
// Bounds are held as data rather than as a switch so that a stage added to the
// flow without a phase falls outside every bound and is reported unphased,
// instead of silently joining whichever branch happened to be last.
var phaseBounds = []struct {
	last  Number
	phase Phase
}{
	{StageRecall, PhaseSpecification},
	{StageAtomDocumentation, PhaseAtoms},
	{StageMoleculeVerification, PhaseMolecules},
	{StagePathCoverage, PhaseControlFlow},
	{StageEndToEndTests, PhaseProgram},
	{StageNonFunctional, PhaseVerification},
	{StageDeliver, PhaseDelivery},
}

// PhaseOf reports which phase a stage belongs to.
//
// The second result is false for a number outside the flow, which a caller
// must report as unattributed rather than defaulting into a phase. A cost
// summary that quietly files an unknown stage under delivery is worse than one
// that admits it does not know where the money went.
func PhaseOf(stage Number) (Phase, bool) {
	if stage < StageInstructions {
		return "", false
	}
	for _, bound := range phaseBounds {
		if stage <= bound.last {
			return bound.phase, true
		}
	}
	return "", false
}

// Valid reports whether a phase is one of the flow's own.
func (phase Phase) Valid() bool {
	return slices.Contains(Phases, phase)
}
