package pipeline

import "fmt"

// StageCheck binds one stage to the check that performs it and to whether that
// check may report the stage satisfied (PIPE-004).
//
// The flow declares what each stage claims. Nothing declared what performs the
// claim, so a stage could record satisfied for a gate its check does not
// establish and only a reader comparing the two by hand would notice. This
// table is the second half of the vocabulary: the flow says what is claimed,
// this says who answers it and whether the answer may be "yes".
type StageCheck struct {
	// Stage is the position in the flow this entry describes.
	Stage Number
	// Performer names the function that decides the stage, so a reader can go
	// from a ledger row to the code that produced it.
	Performer string
	// MaySatisfy reports whether this stage's check is allowed to record
	// satisfied at all. A stage with no check may record only skipped or
	// not-implemented, because a stage nothing performs has nothing to claim.
	MaySatisfy bool
	// Unestablished names the TODO that must land before this stage's check
	// actually establishes the gate the flow states for it. Empty means the
	// check and the gate already agree.
	//
	// docs/plan.md §33 lists six stages that record satisfied for gates they
	// only partly perform. They are named here rather than left in prose so
	// the repairs have one place to flip, and so a reader of the ledger can
	// find out that a satisfied row is weaker than it looks.
	Unestablished string
}

// Checks binds every stage in Flow to its performer.
var Checks = []StageCheck{
	{StageInstructions, "AgentExecution.Run", true, ""},
	{StageClarification, "resolveAmbiguity", true, ""},
	{StageAtomicInstructions, "AgentExecution.planFromRequirement", true, ""},
	{StageDecompositionCover, "AgentExecution.Run", true, ""},
	{StageContracts, "describeContracts", true, "PIPE-137"},
	{StageRecall, "AgentExecution.recallKnownAtoms", true, "PIPE-050"},
	{StageAtoms, "checkAtoms", true, "PIPE-138"},
	{StageAtomDocumentation, "checkAtomDocumentation", true, ""},
	{StageAtomCaseSynthesis, "checkCaseCoverage", true, ""},
	{StageAtomExampleTests, "checkAtomTests", true, ""},
	{StageAtomPropertyTests, "checkPropertyTests", true, ""},
	{StageAtomFuzz, "checkFuzzing", true, ""},
	{StageAtomVerification, "checkAtomVerification", true, "PIPE-141"},
	{StageAtomMutation, "checkMutations", true, "PIPE-127"},
	{StageAtomOptimization, "checkSimplification", false, ""},
	{StageAtomComplexity, "checkComplexity", false, ""},
	{StageMolecules, "checkMolecules", true, ""},
	{StageCompositionObligations, "composeCompositionObligations", true, ""},
	{StageMoleculeTests, "checkMoleculeTests", true, ""},
	{StageMoleculeVerification, "dischargeMoleculeVerification", true, ""},
	{StageControlObligations, "composeControlObligations", true, ""},
	{StageControlTests, "checkControlTests", true, ""},
	{StageControlFlow, "dischargeControlFlow", true, ""},
	{StagePathCoverage, "checkFunctionCoverage", true, "PIPE-115"},
	{StageProgram, "AgentExecution.Run", true, ""},
	{StageAssembly, "AgentExecution.Run", true, ""},
	{StageGlobalInvariants, "checkGlobalInvariants", true, "PIPE-134"},
	{StageIntegrationTests, "AgentExecution.Run", true, ""},
	// PIPE-019 made the instructions stage require an example, which is what
	// makes this gate non-vacuous: a run can no longer reach this stage having
	// declared, at the flow's very first stage, that nothing external checks
	// it.
	{StageEndToEndTests, "AgentExecution.checkAcceptance", true, ""},
	{StageRepetition, "checkRepetition", true, ""},
	{StageAdversarial, "AgentExecution.probeProducedCommands", true, "PIPE-093a"},
	{StageNonFunctional, "checkNonFunctional", true, ""},
	{StagePlatformMatrix, "checkPlatformMatrix", true, ""},
	{StageAntiPatterns, "checkAntiPatterns", true, "PIPE-113"},
	{StageEvidenceBundle, "AgentExecution.assembleEvidence", true, ""},
	{StageHumanAcceptance, "AgentExecution.Run", false, ""},
	{StageDeliver, "AgentExecution.Run", false, ""},
	// PIPE-020.
	{StageAcceptanceOracle, "AgentExecution.checkAcceptanceOracle", true, ""},
}

// ValidateChecks reports every disagreement between Flow and Checks.
//
// Every stage must be bound exactly once, and every binding must name a stage
// that exists. A flow that gains a stage without a performer would otherwise
// record not-implemented for ever and read as a deliberate gap.
func ValidateChecks() []string {
	var findings []string

	bound := make(map[Number]int, len(Checks))
	for _, check := range Checks {
		bound[check.Stage]++
		if check.Performer == "" {
			findings = append(findings,
				fmt.Sprintf("stage %d names no performer", check.Stage))
		}
	}
	for _, stage := range Flow {
		switch bound[stage.Number] {
		case 1:
		case 0:
			findings = append(findings, fmt.Sprintf(
				"stage %d (%s) has no entry in Checks, so nothing records what "+
					"performs it", stage.Number, stage.Name))
		default:
			findings = append(findings, fmt.Sprintf(
				"stage %d (%s) is bound %d times", stage.Number, stage.Name,
				bound[stage.Number]))
		}
	}
	declared := make(map[Number]struct{}, len(Flow))
	for _, stage := range Flow {
		declared[stage.Number] = struct{}{}
	}
	for _, check := range Checks {
		if _, exists := declared[check.Stage]; !exists {
			findings = append(findings, fmt.Sprintf(
				"Checks binds stage %d, which is not in the flow", check.Stage))
		}
	}
	return findings
}

// CheckFor returns the binding for one stage.
func CheckFor(stage Number) (StageCheck, bool) {
	for _, check := range Checks {
		if check.Stage == stage {
			return check, true
		}
	}
	return StageCheck{}, false
}

// Section33Resolved names the stages docs/plan.md §33 listed as recording
// satisfied for a gate they only partly performed, and the ticket that
// reconciled each.
//
// A §33 stage is resolved in one of two ways, and both have to be recorded or
// the stage silently leaves the list:
//
//   - the check is made unable to claim, so it records skipped instead
//     (atom-optimization, atom-complexity); or
//   - the gate is made to state what the check actually establishes, after
//     which the stage may honestly satisfy (platform-matrix).
//
// The second is easy to mistake for the defect it repairs, which is why it is
// written down rather than inferred from MaySatisfy.
var Section33Resolved = map[Number]string{
	StageAtomOptimization:       "PIPE-010",
	StageAtomComplexity:         "PIPE-011",
	StagePlatformMatrix:         "PIPE-012",
	StageNonFunctional:          "PIPE-013",
	StageCompositionObligations: "PIPE-016",
	StageControlObligations:     "PIPE-016",
}

// UnestablishedStages returns every stage whose check does not yet establish
// the gate the flow states for it, with the TODO that closes the gap.
func UnestablishedStages() map[Number]string {
	gaps := make(map[Number]string)
	for _, check := range Checks {
		if check.Unestablished != "" {
			gaps[check.Stage] = check.Unestablished
		}
	}
	return gaps
}

// ExaminesProducedSource is the set of stages that examine what a run
// produced, named rather than expressed as a numeric range (PIPE-005).
//
// The blocking sweep for a module that does not build used to select these
// with `Number >= StageContracts && Number <= StageEvidenceBundle`. That is
// correct only while the flow's numbering happens to hold: inserting a stage
// inside the range silently enrols it, and moving one outside silently drops
// it, with nothing failing either way. Naming the set makes an addition a
// deliberate edit here.
//
// Membership is exactly the stages examineStructure decides. Stages inside the
// old numeric range that are written elsewhere are deliberately absent:
// assembly and program are recorded before examineStructure runs, recall and
// adversarial have their own not-built branches, integration-tests is gated
// before it, and evidence-bundle reads the ledger rather than the worktree and
// is assembled whether or not anything built. Including any of them here would
// be a second write for a stage already recorded.
var ExaminesProducedSource = []Number{
	StageContracts,
	StageAtoms,
	StageAtomDocumentation,
	StageAtomCaseSynthesis,
	StageAtomExampleTests,
	StageAtomPropertyTests,
	StageAtomFuzz,
	StageAtomVerification,
	StageAtomMutation,
	StageAtomOptimization,
	StageAtomComplexity,
	StageMolecules,
	StageCompositionObligations,
	StageMoleculeTests,
	StageMoleculeVerification,
	StageControlObligations,
	StageControlTests,
	StageControlFlow,
	StagePathCoverage,
	StageGlobalInvariants,
	StageRepetition,
	StageNonFunctional,
	StagePlatformMatrix,
	StageAntiPatterns,
}

// ExaminesProducedSourceSet returns the same set for membership tests.
func ExaminesProducedSourceSet() map[Number]struct{} {
	set := make(map[Number]struct{}, len(ExaminesProducedSource))
	for _, stage := range ExaminesProducedSource {
		set[stage] = struct{}{}
	}
	return set
}
