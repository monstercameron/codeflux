package pipeline

import (
	"fmt"
	"strings"
)

// Requirement binds one stage to the stages whose gate must already be
// satisfied before this stage's own gate can be evaluated (PIPE-058).
//
// Requires is declared as data, beside Checks, rather than as a new field on
// Stage. Flow's 37 entries are positional composite literals — {Number,
// "name", "gate"} — so adding a field would mean touching every one of them.
// Checks already establishes the pattern this repository uses instead: a
// second table, keyed by Number and validated against Flow, carries the
// second fact about a stage without disturbing the first table's shape. A
// separate table is also the design PIPE-058's own file-ownership boundary
// asks for: stages.go and checks.go belong to another lane adding stages and
// rows to both while this table is being written, and a table beside them
// cannot collide with that work the way a rewritten Flow literal would.
//
// The table is a slice with one entry per flow stage, not a map keyed by
// stage. A map lets a new stage arrive in Flow with no corresponding key and
// nothing would notice; ValidateRequirements walks Flow and requires exactly
// one Requirements entry per stage, so an added stage with no declared
// dependencies is a failing build rather than a silent gap — which matters
// because another lane can add a stage while this one is in flight.
type Requirement struct {
	// Stage is the position in the flow this entry describes.
	Stage Number
	// Requires lists the stages whose gate this one's gate depends on. A nil
	// or empty list is a root: nothing has to hold first. It is written down
	// explicitly for every stage, including roots, rather than left to be
	// inferred from a missing entry — an omitted stage and a stage with
	// genuinely no requirements must not read the same way.
	Requires []Number
}

// Requirements declares the dependency edge for every stage in Flow
// (PIPE-058).
//
// Each entry is derived from what the stage's own gate in Flow actually
// reads, not from the total order Flow already encodes. "Each stage requires
// the previous one" would restate the numbering, encode nothing beyond it,
// and leave no frontier wider than one stage — which would make the
// concurrency this table exists to support (PIPE-058a) pointless before it
// starts. Where a gate does not genuinely depend on a neighbouring stage's
// output, that is recorded as no edge to it; declaring the absence is the
// point of the exercise, not an oversight to be filled in later.
//
// Every requirement names a strictly lower-numbered stage. That is what
// keeps this table consistent with the total order the ledger reports
// progress in: a stage can only depend on something that could already have
// happened.
var Requirements = []Requirement{
	// Phase A — intake and specification.

	{StageInstructions, nil}, // Root. Nothing precedes the request itself.

	{StageClarification, []Number{
		// Clarification resolves ambiguity in the request instructions
		// recorded; there is nothing to clarify before the request exists.
		StageInstructions,
	}},

	{StageAtomicInstructions, []Number{
		// The request being split into units is the clarified request, not
		// the raw one — clarification is what the units are cited from.
		StageClarification,
	}},

	{StageDecompositionCover, []Number{
		// Coverage compares the acceptance criteria against the units
		// atomic-instructions produced; there is nothing to check coverage
		// of before the units exist.
		StageAtomicInstructions,
	}},

	{StageContracts, []Number{
		// Contracts are written for the unit set once it is known to be
		// neither missing a criterion nor carrying a unit no criterion asked
		// for, which is exactly what decomposition-coverage establishes.
		StageDecompositionCover,
	}},

	{StageRecall, []Number{
		// Recall matches a contract against the registry; without a contract
		// there is nothing to match.
		StageContracts,
	}},

	// Phase B — atoms.

	{StageAtomCaseSynthesis, []Number{
		// The input ladder is derived from the atom's signature, which is
		// part of its contract. Recall precedes this stage in the flow but
		// produces no data this one reads — a reused atom still gets its own
		// case ladder, per the re-verification PIPE-052 requires — so no edge
		// runs from recall here.
		StageContracts,
	}},

	{StageAtomExampleTests, []Number{
		// The example tests are written from the case ladder rather than
		// from the contract directly — that is the entire reason
		// case-synthesis is its own stage before any test is written.
		StageAtomCaseSynthesis,
	}},

	{StageAtomPropertyTests, []Number{
		// Property tests assert each postcondition over generated input, so
		// they depend on the contract's postconditions, not on the discrete
		// case ladder example-tests uses. This is why property-tests can run
		// before example-tests finishes: both are direct children of
		// contracts, not of each other.
		StageContracts,
	}},

	{StageAtoms, []Number{
		// Both kinds of tests are written before the atom and must already
		// fail against a stub, which is only meaningful once they exist.
		StageAtomExampleTests,
		StageAtomPropertyTests,
	}},

	{StageAtomVerification, []Number{
		// Verification needs the atom to run and the tests to run it
		// against; the atom's own stage requires the tests already, but
		// naming them here too is the same reasoning the ticket gives for
		// this exact stage: it needs the atoms and their tests to exist.
		StageAtoms,
		StageAtomExampleTests,
		StageAtomPropertyTests,
	}},

	{StageAtomFuzz, []Number{
		// Fuzzing exercises the atom's parsing boundaries; it needs the atom
		// built, not the ordinary suite passing first, so it can run
		// alongside verification rather than after it.
		StageAtoms,
	}},

	{StageAtomMutation, []Number{
		// Mutation scoring needs a passing suite to mutate against — there is
		// nothing to compare a mutant's result to otherwise.
		StageAtomVerification,
	}},

	{StageAntiPatterns, []Number{
		// Anti-pattern scanning looks at code that already works: a swallowed
		// error or a shadowed name is not a test failure, so nothing about
		// this check requires the mutation score, only that the code passes
		// its tests first.
		StageAtomVerification,
	}},

	{StageAtomOptimization, []Number{
		// The atom may only be rewritten once its tests are known to detect a
		// defect — optimising under tests nobody has shown can catch a
		// mistake is how a silent behaviour change reaches delivery with a
		// green suite behind it.
		StageAtomMutation,
	}},

	{StageAtomComplexity, []Number{
		// The shipped bound is measured on the atom that will actually ship,
		// which is the one optimisation produced, not the one before it.
		StageAtomOptimization,
	}},

	{StageAtomDocumentation, []Number{
		// Documentation records what the atom is known to do, including the
		// edges only checking reveals, so it follows every check this phase
		// runs: the ordinary suite, fuzzing, mutation scoring, the rewrite,
		// and the bound measured on what actually ships.
		StageAtomVerification,
		StageAtomFuzz,
		StageAtomMutation,
		StageAtomOptimization,
		StageAtomComplexity,
	}},

	// Phase C — molecules.

	{StageCompositionObligations, []Number{
		// A composition's obligations are stated over finished parts, and what
		// makes a part finished is that it is verified — not that it is
		// documented.
		//
		// This required StageAtomDocumentation, on the reasoning that the atom
		// phase ends where phases.go draws it. That was true while
		// documentation was asked of the model during the run. It is now
		// derived from measured facts after verification, and it is advisory:
		// a coverage percentage and a registry row do not make a program
		// correct. Leaving the edge in place meant an advisory stage blocked
		// four hard ones — composition obligations, molecule tests, molecules,
		// and molecule verification — so a run with a perfectly good
		// composition was told nothing about it because a comment was missing.
		//
		// A hard gate may not depend on an advisory one. The dependency that
		// survives is the real one: obligations are stated over atoms that
		// have been shown to work.
		StageAtomVerification,
	}},

	{StageMoleculeTests, []Number{
		// The gate is explicit: these tests are written from the obligations,
		// before the molecule.
		StageCompositionObligations,
	}},

	{StageMolecules, []Number{
		// Molecule-tests exist and fail against a stub before the molecule is
		// written, the same test-first ordering the atom phase uses.
		StageMoleculeTests,
	}},

	{StageMoleculeVerification, []Number{
		// Discharge is checked by name against the obligation list, which
		// needs both the obligations themselves and the molecule that is
		// supposed to have closed them.
		StageMolecules,
		StageCompositionObligations,
	}},

	// Phase D — control flow.

	{StageControlObligations, []Number{
		// A control obligation is raised over a function that declares a
		// path — the composed functions the molecule phase produced and
		// verified. The molecule phase is not finished until verification, so
		// that is the boundary this stage waits on.
		StageMoleculeVerification,
	}},

	{StageControlTests, []Number{
		// These tests are written to reach every function that declared a
		// path, which is exactly the obligation list.
		StageControlObligations,
	}},

	{StageControlFlow, []Number{
		// Discharge follows the same test-first ordering as the molecule
		// phase: the tests exist and are failing before the obligations are
		// closed.
		StageControlTests,
	}},

	{StagePathCoverage, []Number{
		// Coverage is measured by running the tests, and it is measured
		// against the flow's finished state rather than a partially
		// discharged one, so it needs both the tests and the discharge.
		StageControlFlow,
		StageControlTests,
	}},

	// Phase E — the program.

	{StageAssembly, []Number{
		// The pieces being wired together are molecules whose control
		// obligations are already discharged. Path-coverage is a
		// measurement of that same finished state, not a repair to it, so
		// assembly does not wait on it — it can run alongside path-coverage
		// once control-flow is done.
		StageControlFlow,
	}},

	{StageProgram, []Number{
		// The build artifact is what assembly's compile step produces.
		StageAssembly,
	}},

	{StageIntegrationTests, []Number{
		// Components are exercised together against the built program;
		// nothing is mocked, so the program has to exist first.
		StageProgram,
	}},

	{StageEndToEndTests, []Number{
		// Reproducing an acceptance example needs the built executable and
		// the acceptance example itself, which was recorded all the way back
		// at instructions — a genuine long-range edge, not a restatement of
		// the chain in between. End-to-end does not wait on integration-tests
		// because the two exercise the program at different granularities
		// from the same build.
		StageProgram,
		StageInstructions,
	}},

	// Phase F — verification depth.

	{StageGlobalInvariants, []Number{
		// Whole-program properties are checked against the built program
		// directly, not against a particular test suite's run of it, so this
		// depends on the program rather than on integration or end-to-end
		// tests.
		StageProgram,
	}},

	{StageAdversarial, []Number{
		// Hostile input is fed to the built program; nothing about probing it
		// requires the ordinary suites to have run first.
		StageProgram,
	}},

	{StageRepetition, []Number{
		// "The suite" run repeatedly for flake detection is the executable's
		// own suite — integration and end-to-end — not the atom- and
		// molecule-level tests already verified in their own phases.
		StageIntegrationTests,
		StageEndToEndTests,
	}},

	{StagePlatformMatrix, []Number{
		// Every platform claim is answered against a built program: the host
		// by running its suite, a cross target by compiling.
		StageProgram,
	}},

	{StageNonFunctional, []Number{
		// Duration is measured against the same suite repetition times, for
		// the same reason repetition depends on them: this is timing the
		// executable-level suite, not the whole flow's history.
		StageIntegrationTests,
		StageEndToEndTests,
	}},

	// Phase G — delivery.

	{StageEvidenceBundle, []Number{
		// The bundle reads the ledger, not the worktree, but it can only
		// state what was checked and what was not once every Phase F stage
		// has recorded an outcome — satisfied, failed, skipped, or
		// not-implemented all count as recorded.
		StageGlobalInvariants,
		StageAdversarial,
		StageRepetition,
		StagePlatformMatrix,
		StageNonFunctional,
	}},

	{StageHumanAcceptance, []Number{
		// A person accepts, rejects, or asks for a change against the
		// evidence bundle; there is nothing to decide from before it exists.
		StageEvidenceBundle,
	}},

	{StageDeliver, []Number{
		// What is handed over is the accepted change and its evidence, named
		// separately in the gate, so delivery depends on both the acceptance
		// decision and the bundle rather than on the decision alone.
		StageHumanAcceptance,
		StageEvidenceBundle,
	}},

	{StageAcceptanceOracle, []Number{
		// The oracle runs the declared examples against the repository before
		// this run touches it, so it needs the examples and nothing else. It is
		// deliberately not a root: with no examples recorded there is nothing to
		// run, and PIPE-019 is what guarantees instructions produced some.
		//
		// This stage's number is the highest in the flow because PIPE-020
		// appended it rather than renumbering, which PIPE-045 owns. Its position
		// in the dependency graph is therefore nothing like its position in the
		// list -- it is unblocked immediately after instructions, and everything
		// from contracts onward could run beside it. That divergence between
		// declared order and dependency order is exactly what this table exists
		// to express.
		StageInstructions,
	}},

	{StageSkipAudit, []Number{
		// The audit classifies every stage this run's own ledger recorded
		// skipped or not-implemented, so it needs the run's ledger to be as
		// complete as this run is ever going to make it, not merely the
		// examples the oracle needs. Human-acceptance and deliver are the
		// run's last two decisions -- both always recorded, blocked, skipped,
		// or not-implemented (PIPE-004) -- and each already transitively
		// requires evidence-bundle and everything Phase F requires in turn, so
		// naming both here is the shortest declaration that nothing this run
		// could still decide is left unrecorded when the audit runs.
		//
		// Like the oracle, this stage's number is the flow's highest rather
		// than its conceptually correct position immediately before delivery,
		// because PIPE-042 appended it rather than renumbering, which
		// PIPE-045 owns.
		StageHumanAcceptance,
		StageDeliver,
	}},

	{StageAtomRegistration, []Number{
		// A registered atom's row is only worth writing once the atom
		// carries the documentation recall depends on -- registering an
		// undocumented atom would write a row with nothing but a name and a
		// contract hash, which is not what PIPE-048 asks for.
		StageAtomDocumentation,
	}},

	{StageMoleculeRegistration, []Number{
		// A molecule is only worth registering once composition's own
		// obligations are verified discharged, and registering it on "the
		// same terms" as an atom (PIPE-049) means it also needs
		// atom-registration to have already run, so the atoms it names as
		// its parts are the ones a reader can actually look up.
		StageMoleculeVerification,
		StageAtomRegistration,
	}},
}

// RequirementFor returns the declared dependency entry for one stage.
func RequirementFor(stage Number) (Requirement, bool) {
	for _, requirement := range Requirements {
		if requirement.Stage == stage {
			return requirement, true
		}
	}
	return Requirement{}, false
}

// ValidateRequirements reports every disagreement it can find between Flow
// and the package's own Requirements table.
//
// It is a thin wrapper over ValidateRequirementsTable, which does the actual
// work against an explicit table argument rather than the package variable.
// The split exists so a test can feed a deliberately broken table and prove
// each check actually discriminates, rather than merely having been run once
// against a table that happens to be correct — a validator only ever
// exercised against correct input is indistinguishable from one that always
// returns nil.
func ValidateRequirements() []string {
	return ValidateRequirementsTable(Requirements)
}

// ValidateRequirementsTable reports every disagreement it can find in a
// dependency table, rather than stopping at the first, following the shape
// ValidateChecks already established: a table can be wrong in more than one
// place at once, and a reader fixing one defect should not have to rerun the
// check to discover the second.
//
// It checks, independently:
//
//  1. every stage in Flow has exactly one entry in the table, and every
//     entry names a stage that is actually in Flow — the same coverage
//     ValidateChecks proves for Checks;
//  2. every requirement names a stage that exists;
//  3. no requirement points at a stage numbered the same or later, which is
//     what keeps the graph consistent with the ledger's total order;
//  4. the requirement graph is acyclic, reporting the actual cycle;
//  5. every stage is reachable from the flow's one root by following
//     requirement edges forward.
func ValidateRequirementsTable(requirements []Requirement) []string {
	var findings []string
	findings = append(findings, validateRequirementsCoverFlow(requirements)...)
	findings = append(findings, validateRequirementsNameKnownStages(requirements)...)
	findings = append(findings, validateRequirementsDoNotPointForward(requirements)...)
	findings = append(findings, validateRequirementsAreAcyclic(requirements)...)
	findings = append(findings, validateEveryStageIsReachable(requirements)...)
	return findings
}

// validateRequirementsCoverFlow proves property 1: every stage in Flow is
// bound exactly once, and every bound stage is actually in Flow.
//
// A map keyed by stage could not fail this way — a missing key would simply
// read as "no requirements" rather than as "nobody declared this stage yet",
// which is exactly the ambiguity a slice validated this way removes.
func validateRequirementsCoverFlow(requirements []Requirement) []string {
	var findings []string

	bound := make(map[Number]int, len(requirements))
	for _, requirement := range requirements {
		bound[requirement.Stage]++
	}
	for _, stage := range Flow {
		switch bound[stage.Number] {
		case 1:
		case 0:
			findings = append(findings, fmt.Sprintf(
				"stage %d (%s) has no entry in Requirements, so its "+
					"dependencies are undeclared", stage.Number, stage.Name))
		default:
			findings = append(findings, fmt.Sprintf(
				"stage %d (%s) is bound %d times in Requirements",
				stage.Number, stage.Name, bound[stage.Number]))
		}
	}

	declared := make(map[Number]struct{}, len(Flow))
	for _, stage := range Flow {
		declared[stage.Number] = struct{}{}
	}
	for _, requirement := range requirements {
		if _, exists := declared[requirement.Stage]; !exists {
			findings = append(findings, fmt.Sprintf(
				"Requirements binds stage %d, which is not in the flow",
				requirement.Stage))
		}
	}
	return findings
}

// validateRequirementsNameKnownStages proves property 2: every stage named
// inside a Requires list is a stage Flow actually declares.
func validateRequirementsNameKnownStages(requirements []Requirement) []string {
	declared := make(map[Number]struct{}, len(Flow))
	for _, stage := range Flow {
		declared[stage.Number] = struct{}{}
	}

	var findings []string
	for _, requirement := range requirements {
		for _, dependency := range requirement.Requires {
			if _, exists := declared[dependency]; !exists {
				findings = append(findings, fmt.Sprintf(
					"stage %d requires stage %d, which is not in the flow",
					requirement.Stage, dependency))
			}
		}
	}
	return findings
}

// validateRequirementsDoNotPointForward proves property 3: a stage may only
// require a strictly lower-numbered stage.
//
// This is what keeps the declared graph consistent with the total order the
// ledger reports progress in — a dependency on a stage numbered the same or
// later could not yet have happened when this stage runs.
func validateRequirementsDoNotPointForward(requirements []Requirement) []string {
	var findings []string
	for _, requirement := range requirements {
		for _, dependency := range requirement.Requires {
			if dependency >= requirement.Stage {
				findings = append(findings, fmt.Sprintf(
					"stage %d requires stage %d, which is not earlier in the "+
						"flow", requirement.Stage, dependency))
			}
		}
	}
	return findings
}

// requirementsCycle returns the stages forming a cycle in a requirement
// table, in the order the cycle runs, with the closing stage repeated at the
// end so the loop is visible. It returns nil when the graph is acyclic.
//
// It walks the table directly rather than a graph built by another
// validator, and it skips a dependency that does not name a known stage,
// because that defect is reported separately and must not make cycle
// detection panic or misreport on an already-broken table.
func requirementsCycle(requirements []Requirement) []Number {
	const (
		unvisited = iota
		inProgress
		finished
	)

	byStage := make(map[Number][]Number, len(requirements))
	declared := make(map[Number]struct{}, len(requirements))
	for _, requirement := range requirements {
		byStage[requirement.Stage] = requirement.Requires
		declared[requirement.Stage] = struct{}{}
	}

	state := make(map[Number]int, len(requirements))
	var path []Number
	var cycle []Number

	var visit func(stage Number) bool
	visit = func(stage Number) bool {
		state[stage] = inProgress
		path = append(path, stage)
		for _, dependency := range byStage[stage] {
			if _, known := declared[dependency]; !known {
				continue
			}
			switch state[dependency] {
			case unvisited:
				if visit(dependency) {
					return true
				}
			case inProgress:
				start := 0
				for index, seen := range path {
					if seen == dependency {
						start = index
						break
					}
				}
				cycle = append(cycle, path[start:]...)
				cycle = append(cycle, dependency)
				return true
			}
		}
		path = path[:len(path)-1]
		state[stage] = finished
		return false
	}

	// Flow's order, not the table's, so detection is deterministic even when
	// the table under test carries entries out of order or omits some.
	for _, stage := range Flow {
		if _, bound := declared[stage.Number]; !bound {
			continue // reported by validateRequirementsCoverFlow
		}
		if state[stage.Number] == unvisited {
			if visit(stage.Number) {
				return cycle
			}
		}
	}
	return nil
}

// validateRequirementsAreAcyclic proves property 4, reporting the actual
// cycle rather than only asserting one exists — a scheduler resolving this
// report needs to know which stages to look at, not just that the table is
// wrong somewhere.
func validateRequirementsAreAcyclic(requirements []Requirement) []string {
	cycle := requirementsCycle(requirements)
	if cycle == nil {
		return nil
	}
	described := make([]string, len(cycle))
	for index, number := range cycle {
		if stage, found := StageByNumber(number); found {
			described[index] = fmt.Sprintf("%d(%s)", number, stage.Name)
		} else {
			described[index] = fmt.Sprintf("%d", number)
		}
	}
	return []string{fmt.Sprintf(
		"the declared requirements contain a cycle: %s",
		strings.Join(described, " -> "))}
}

// validateEveryStageIsReachable proves property 5.
//
// A stage with no requirements is a root. Every non-root stage must be
// reachable from a root by following requirement edges forward — from a
// prerequisite to whatever declares it as a requirement — which is the
// direction a scheduler actually walks: it starts at what needs nothing and
// unblocks what depended on it.
//
// This is checked more strictly than "reachable from any root", because
// this table declares exactly one true root, instructions. A stage that
// wrongly declared no requirements would present as a root of its own and
// would trivially "reach" itself and anything chained under it, without
// ever being caught by the acyclic or forward-pointing checks — so the
// declared root set is required to be exactly {instructions}, and finding
// any other root is reported as a finding in its own right, not only as a
// disconnection.
func validateEveryStageIsReachable(requirements []Requirement) []string {
	declared := make(map[Number]struct{}, len(requirements))
	for _, requirement := range requirements {
		declared[requirement.Stage] = struct{}{}
	}

	var roots []Number
	successors := make(map[Number][]Number, len(requirements))
	for _, requirement := range requirements {
		if len(requirement.Requires) == 0 {
			roots = append(roots, requirement.Stage)
			continue
		}
		for _, dependency := range requirement.Requires {
			if _, known := declared[dependency]; !known {
				continue // reported by validateRequirementsNameKnownStages
			}
			successors[dependency] = append(successors[dependency], requirement.Stage)
		}
	}

	var findings []string
	if len(roots) != 1 || roots[0] != StageInstructions {
		findings = append(findings, fmt.Sprintf(
			"the declared roots are %v; exactly one stage should require "+
				"nothing, instructions (%d) — any other stage with no "+
				"requirements would silently start a second, disconnected "+
				"root", roots, StageInstructions))
	}

	visited := make(map[Number]struct{}, len(requirements))
	queue := append([]Number{}, roots...)
	for _, root := range roots {
		visited[root] = struct{}{}
	}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, next := range successors[current] {
			if _, seen := visited[next]; seen {
				continue
			}
			visited[next] = struct{}{}
			queue = append(queue, next)
		}
	}

	for _, stage := range Flow {
		if _, ok := visited[stage.Number]; !ok {
			findings = append(findings, fmt.Sprintf(
				"stage %d (%s) is not reachable from any root; the scheduler "+
					"would never find a satisfied chain that unblocks it",
				stage.Number, stage.Name))
		}
	}
	return findings
}
