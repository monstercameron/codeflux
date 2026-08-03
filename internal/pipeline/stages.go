// Package pipeline declares the stages a requirement passes through on its way
// to a delivered program, and what each stage has to establish.
//
// It owns the vocabulary only: the ordered list of stages, the closed set of
// outcomes, and the gate each stage must satisfy. It performs no stage and
// decides nothing about any run. The coordinator performs the stages it can
// and records the rest as not implemented, which is the point — a flow whose
// missing stages are invisible looks identical to one that has none.
package pipeline

// Number is a stage's position in the flow.
//
// The positions are gapless and order the flow. They are not a stage's
// identity: inserting a stage shifts every number after it, which is why the
// ledger records the stage's name alongside its number and why recorded
// evidence should be read by name. A number answers "how far did this get";
// only the name answers "which check was this".
type Number int

// The stages, in the order a requirement passes through them.
//
// The flow has four movements. Phases A and B establish what is being built
// and build its smallest pieces; C and D compose them and discharge the
// obligations composition creates; E assembles and exercises the program; F
// and G measure how much the checking was worth and hand the work over.
const (
	// Phase A — intake and specification. Everything here is about deciding
	// what to build, and it is the only part of the flow with any contact with
	// the person's actual intent.
	StageInstructions       Number = 1
	StageClarification      Number = 2
	StageAtomicInstructions Number = 3
	StageDecompositionCover Number = 4
	StageContracts          Number = 5
	StageRecall             Number = 6

	// Phase B — atoms. The smallest units that can be specified, tested, and
	// implemented independently.
	//
	// StageAtomCaseSynthesis comes first, before any test is written.
	//
	// A test written by reading an implementation checks what the code does. A
	// case derived from the signature checks what the signature promised, and
	// those two differ exactly where the bug is. Deriving the inputs first —
	// the empty slice, the zero, the negative, the value that must be refused —
	// gives the stage that writes tests something to write them from other than
	// the code it is meant to be checking.
	StageAtomCaseSynthesis Number = 7
	StageAtomExampleTests  Number = 8
	StageAtomPropertyTests Number = 9
	StageAtoms             Number = 10
	StageAtomVerification  Number = 11
	StageAtomFuzz          Number = 12
	StageAtomMutation      Number = 13
	// StageAntiPatterns catches code that works and is written in a way with a
	// known way of going wrong later.
	//
	// It sits after verification because these are not compile errors and not
	// test failures: a swallowed error, a shadowed name, a package-level
	// variable, an unchecked assertion. No test written against the current
	// behaviour would ever fail on one, which is exactly why something has to
	// look for them on purpose.
	StageAntiPatterns Number = 14
	// StageAtomOptimization rewrites an atom to be simpler, and may only run
	// once the tests are known to detect defects.
	//
	// Optimising before mutation scoring means rewriting code guarded by tests
	// nobody has shown can catch a mistake, which is how an optimisation that
	// silently changes behaviour reaches delivery with a green suite behind
	// it. Its own gate is therefore not "the code got smaller" but "the code
	// got simpler and every test that passed still passes".
	StageAtomOptimization Number = 15
	// StageAtomComplexity measures the atom that will actually ship.
	//
	// It runs after optimisation because a bound measured on code that was
	// then rewritten describes something nobody is going to run.
	StageAtomComplexity Number = 16
	// StageAtomDocumentation comes last in this phase, and deliberately after
	// every check rather than beside the implementation.
	//
	// Documentation written next to the code describes what its author meant.
	// Documentation written after the tests, the fuzzing, and the mutation
	// score describes what the atom is known to do — including the edges that
	// only showed up under checking. It is also the artifact a later run
	// recalls at stage six, so an atom nobody documented is an atom nobody can
	// reuse, and the work is done again from nothing every time.
	StageAtomDocumentation Number = 17

	// Phase C — molecules. Compositions of atoms, and the obligations that
	// composition creates but type compatibility does not discharge.
	StageCompositionObligations Number = 18
	StageMoleculeTests          Number = 19
	StageMolecules              Number = 20
	StageMoleculeVerification   Number = 21

	// Phase D — control flow. Ordering, termination, and what happens on every
	// failure path rather than only the intended one.
	StageControlObligations Number = 22
	StageControlTests       Number = 23
	StageControlFlow        Number = 24
	StagePathCoverage       Number = 25

	// Phase E — the program. Assembly is its own stage because wiring fails on
	// its own, and finding out during integration testing tells you the wrong
	// thing about where the fault is.
	StageAssembly         Number = 26
	StageProgram          Number = 27
	StageIntegrationTests Number = 28
	StageEndToEndTests    Number = 29

	// Phase F — verification depth. This phase answers "was any of the
	// checking worth anything", which no amount of passing tests answers.
	StageGlobalInvariants Number = 30
	StageAdversarial      Number = 31
	StageRepetition       Number = 32
	StagePlatformMatrix   Number = 33
	StageNonFunctional    Number = 34

	// Phase G — delivery.
	StageEvidenceBundle  Number = 35
	StageHumanAcceptance Number = 36
	StageDeliver         Number = 37
)

// State is what became of one stage in one attempt.
type State string

const (
	// StateSatisfied means the gate held and the stage produced evidence.
	StateSatisfied State = "satisfied"
	// StateFailed means the gate did not hold.
	StateFailed State = "failed"
	// StateSkipped means this run had no need of the stage. A program with no
	// parsing has nothing to fuzz.
	StateSkipped State = "skipped"
	// StateBlocked means the stage could not run because something upstream
	// did not happen, which is different from the stage itself failing.
	StateBlocked State = "blocked"
	// StateNotImplemented means the product cannot perform this stage at all.
	//
	// It is deliberately distinct from skipped. Collapsing the two would let a
	// build that implements a third of the flow report the same shape of
	// result as one that implements all of it.
	StateNotImplemented State = "not-implemented"
)

// Stage is one step of the flow and the condition it must establish.
type Stage struct {
	Number Number
	Name   string
	// Gate is what must hold for this stage to count as satisfied, written so
	// that a reader of the ledger learns what was checked rather than only
	// that something was.
	Gate string
}

// Flow is every stage in order.
//
// It is a single list rather than a set of per-phase lists because a run's
// evidence has to be readable as one sequence: the question a person asks of
// this record is "how far did it actually get", and that question has one
// answer only if the stages are totally ordered.
var Flow = []Stage{
	{StageInstructions, "instructions",
		"the request is recorded with at least one executable acceptance example"},
	{StageClarification, "clarification",
		"no material ambiguity is left unresolved: it was asked about or a bounded reading was stated"},
	{StageAtomicInstructions, "atomic-instructions",
		"the request is split into single-purpose units, each citing the part of the request it comes from"},
	{StageDecompositionCover, "decomposition-coverage",
		"every acceptance criterion is covered by at least one unit and no unit exists without a criterion"},
	{StageContracts, "contracts",
		"each unit has a signature, types, preconditions, postconditions, declared effects, and error cases"},
	{StageRecall, "recall",
		"each contract is matched against already-known atoms and reused rather than rebuilt where one fits"},

	{StageAtomCaseSynthesis, "atom-case-synthesis",
		"each atom has a ladder of inputs derived from its signature — straightforward, degenerate, edge, complex, wrong, and pathological — and every one is tried by a test asserting what that class of input demands"},
	{StageAtomExampleTests, "atom-example-tests",
		"tests written from the contract, before the atom, that fail against a stub"},
	{StageAtomPropertyTests, "atom-property-tests",
		"at least one property over generated inputs for each postcondition"},
	{StageAtoms, "atoms",
		"each atom satisfies its contract and reads nothing outside its arguments"},
	{StageAtomVerification, "atom-verification",
		"every atom test passes, repeated with recorded seeds"},
	{StageAtomFuzz, "atom-fuzz",
		"every parsing boundary is fuzzed without panic or hang"},
	{StageAtomMutation, "atom-mutation",
		"the mutation score meets its threshold, so the tests are known to detect defects"},
	{StageAntiPatterns, "anti-patterns",
		"the produced source contains no swallowed error, package-level mutable state, unchecked type assertion, panic outside main, shadowed name, untyped parameter, flag argument, or body nested past following"},
	{StageAtomOptimization, "atom-optimization",
		"the atom is rewritten to be simpler where it can be, and every test that passed before still passes with an unchanged result"},
	{StageAtomComplexity, "atom-complexity",
		"the shipped atom carries a time and space bound that its structure implies and that measured growth across input sizes agrees with"},
	{StageAtomDocumentation, "atom-documentation",
		"each verified atom carries its purpose, inputs, outputs, and the algorithm it uses, with the metadata a later run needs to find and reuse it"},

	{StageCompositionObligations, "composition-obligations",
		"each composition raises a durable obligation stating what joining its parts must guarantee"},
	{StageMoleculeTests, "molecule-tests",
		"tests written from the obligations, before the molecule, that fail against a stub"},
	{StageMolecules, "molecules",
		"each molecule composes its atoms leaving no obligation unclosed"},
	{StageMoleculeVerification, "molecule-verification",
		"every composition obligation is discharged by name, or the ones left open are named"},

	{StageControlObligations, "control-obligations",
		"each function that declares a path raises a durable obligation stating what every path through it must do"},
	{StageControlTests, "control-tests",
		"every function that declares a path is reached by a test, so no branching function is left unexamined"},
	{StageControlFlow, "control-flow",
		"every control obligation is discharged by name, or the ones left open are named"},
	{StagePathCoverage, "path-coverage",
		"branch coverage is measured against its threshold rather than assumed"},

	{StageAssembly, "assembly",
		"the pieces are wired into a module that compiles, checked before anything is run"},
	{StageProgram, "program",
		"a build artifact exists"},
	{StageIntegrationTests, "integration-tests",
		"components are exercised together, with nothing mocked"},
	{StageEndToEndTests, "end-to-end-tests",
		"the built executable reproduces every acceptance example exactly"},

	{StageGlobalInvariants, "global-invariants",
		"properties that span the whole program hold across every run"},
	{StageAdversarial, "adversarial",
		"hostile input produces no violation and no sensitive value reaches the output"},
	{StageRepetition, "repetition",
		"the suite is run repeatedly with recorded seeds and does not flake"},
	{StagePlatformMatrix, "platform-matrix",
		"every platform the program claims is answered: the host by running its suite, a cross target only by compiling, because this host cannot execute it"},
	{StageNonFunctional, "non-functional",
		"the suite's duration is within tolerance of this repository's recorded baseline, measured on the same host"},

	{StageEvidenceBundle, "evidence-bundle",
		"every claim links to the artifact and test supporting it, and what was not checked is stated"},
	{StageHumanAcceptance, "human-acceptance",
		"a person accepted the work, rejected it, or asked for a change"},
	{StageDeliver, "deliver",
		"the accepted change, its evidence, and its provenance are handed over"},
}

// StageByNumber returns one stage of the flow.
func StageByNumber(number Number) (Stage, bool) {
	for _, stage := range Flow {
		if stage.Number == number {
			return stage, true
		}
	}
	return Stage{}, false
}

// Valid reports whether a state is one the ledger accepts.
func (value State) Valid() bool {
	switch value {
	case StateSatisfied, StateFailed, StateSkipped, StateBlocked,
		StateNotImplemented:
		return true
	default:
		return false
	}
}
