package pipeline

import "fmt"

// ResourceClass names what a stage's check needs from the worktree while it
// runs, which is what actually forces serialisation, not the dependency
// graph (PIPE-059). Two checks that both only read source may run side by
// side no matter what Requirements says about them; one check that rewrites
// source in place may not run beside anything that reads it, even a stage
// Requirements does not connect it to at all.
type ResourceClass string

const (
	// ResourceClassPureAST performs no external process and mutates nothing:
	// it parses already-written source with go/parser, reads the run's own
	// durable records, or reasons about the request text, and computes its
	// verdict from that alone. It never shells out to `go build` or `go
	// test`, so it never contends with the module's build cache, a test
	// binary, or a produced source file. Every stage whose performer touches
	// the worktree only by parsing it — or touches no worktree file at all,
	// because it decides something about the request or the run's own
	// ledger instead — belongs here. It is also the default for a stage
	// this table cannot verify against its own performer's source (see
	// StageResourceClass.Reason): the weakest resource claim is the safest
	// one to default an unverified entry to, because it never licenses
	// running that stage beside an exclusive-mutating one on a false
	// assumption of safety — it only ever under-parallelizes an unverified
	// stage, never corrupts one.
	ResourceClassPureAST ResourceClass = "pure-ast"
	// ResourceClassBuildOnly compiles the module — `go build` — without
	// executing anything it produces. Compiling reads the worktree's source
	// and writes only to the Go build cache (`go build ./...` against
	// multiple packages discards its link output rather than writing a
	// binary into the worktree), so a build-only check may run concurrently
	// with a pure-ast one or another build-only one.
	ResourceClassBuildOnly ResourceClass = "build-only"
	// ResourceClassSuiteRead runs the module's tests, its fuzz targets, or
	// its own built binary — `go test`, `go test -fuzz`, or executing a
	// produced command. It reads the worktree's source and may write a
	// transient, check-owned artifact outside the produced source a run is
	// answerable for (a coverage profile at a fixed, check-owned path; a
	// fuzz corpus entry under testdata/fuzz), but it never rewrites a
	// produced source file, so it may still run concurrently with anything
	// except an exclusive-mutating check.
	ResourceClassSuiteRead ResourceClass = "suite-read"
	// ResourceClassExclusiveMutating writes a produced source file in place
	// and restores it afterward. Mutation testing (StageAtomMutation) is the
	// one check the flow performs this way, deliberately: PIPE-127/128/129
	// splice a replacement operator into a produced file's bytes, build and
	// test against the mutant, and restore the original content before the
	// next candidate. While a mutant sits on disk, a concurrent read of that
	// file — another check's parse, another check's `go test` — sees a
	// deliberately broken program and may report a false defect against it;
	// a concurrent write races the restore that must return the file to
	// what the run actually produced, and losing that race is how a
	// deliberately broken file ships. A stage of this class may not share a
	// scheduling wave with any other stage, of any class, including another
	// exclusive-mutating one.
	ResourceClassExclusiveMutating ResourceClass = "exclusive-mutating"
)

// Valid reports whether a value is one of the four declared classes.
func (class ResourceClass) Valid() bool {
	switch class {
	case ResourceClassPureAST, ResourceClassBuildOnly, ResourceClassSuiteRead,
		ResourceClassExclusiveMutating:
		return true
	default:
		return false
	}
}

// Exclusive reports whether a stage of this class must run alone: nothing
// else may share its scheduling wave (PIPE-059).
func (class ResourceClass) Exclusive() bool {
	return class == ResourceClassExclusiveMutating
}

// StageResourceClass binds one stage to the resource class its check needs.
type StageResourceClass struct {
	// Stage is the position in the flow this entry describes.
	Stage Number
	// Class is the resource class PIPE-059 schedules this stage's check
	// under.
	Class ResourceClass
	// Reason records what was actually read in the check's own source to
	// justify the classification — which exec.Command calls it makes, if
	// any, and whether it ever calls os.WriteFile against a produced
	// source path — or, for a stage whose performer lives in a file this
	// lane does not own (agent_execution.go, skip_audit.go), names that
	// boundary explicitly and says the entry is inferred from the stage's
	// own gate and Checks.Performer rather than confirmed against the
	// implementation, so a later lane touching that file knows to
	// re-verify it before relying on it.
	Reason string
}

// ResourceClasses binds every stage in Flow to the resource class its check
// needs (PIPE-059).
//
// Every entry for a stage whose performer lives in this lane's owned files —
// internal/pipeline and internal/coordinator's agent_stage_*.go /
// agent_proof_obligations.go / agent_stage_delivery.go files — was
// classified by reading that performer's body for exec.Command and
// os.WriteFile calls, not guessed from the stage's name or its Requirements
// entry: a stage that reads the worktree only through go/parser is
// pure-ast even where its declared gate sounds like it should be heavier
// (StageGlobalInvariants, for example, only parses import declarations),
// and a stage that shells out to `go test` is suite-read even where its
// gate sounds purely structural (StageMoleculeVerification's discharge
// calls checkMoleculeVerification, which runs each molecule's own tests).
//
// checkMutations (StageAtomMutation) is the only entry classified
// exclusive-mutating: it is the only checker in the whole flow that calls
// os.WriteFile against a path under the worktree's produced source.
var ResourceClasses = []StageResourceClass{
	// Phase A — intake and specification. None of these read the worktree at
	// all: they reason about the request text, or about a plan derived from
	// it, entirely inside AgentExecution.Run (agent_execution.go, not owned
	// by this lane). No exec.Command call names them.
	{StageInstructions, ResourceClassPureAST,
		"performed in AgentExecution.Run (agent_execution.go); records the " +
			"request and its acceptance examples, touching no worktree file"},
	{StageClarification, ResourceClassPureAST,
		"performed by resolveAmbiguity (agent_execution.go); reasons about " +
			"the request text, not the worktree"},
	{StageAtomicInstructions, ResourceClassPureAST,
		"performed by AgentExecution.planFromRequirement (agent_execution.go); " +
			"splits the request into units, touching no worktree file"},
	{StageDecompositionCover, ResourceClassPureAST,
		"performed in AgentExecution.Run (agent_execution.go); compares " +
			"units against acceptance criteria, touching no worktree file"},
	{StageContracts, ResourceClassPureAST,
		"describeContracts (agent_stage_delivery.go) calls only " +
			"readProducedFunctions, which parses with go/parser; no " +
			"exec.Command"},
	{StageRecall, ResourceClassPureAST,
		"performed by AgentExecution.recallKnownAtoms (agent_stage_recall.go, " +
			"not owned by this lane); matches a contract against a durable " +
			"registry, not the worktree"},

	// Phase B — atoms.
	{StageAtomCaseSynthesis, ResourceClassPureAST,
		"checkCaseCoverage (agent_stage_cases.go) calls readProducedFunctions " +
			"and testsNaming, both go/parser only; no exec.Command"},
	{StageAtomExampleTests, ResourceClassPureAST,
		"checkAtomTests (agent_stage_structure.go) reads the shared " +
			"producedFunctionCache, which only ever parses; no exec.Command"},
	{StageAtomPropertyTests, ResourceClassPureAST,
		"checkPropertyTests (agent_stage_unit_verification.go) parses test " +
			"files for a for/range loop with go/parser; no exec.Command"},
	{StageAtoms, ResourceClassPureAST,
		"checkAtoms (agent_stage_structure.go) reads the shared " +
			"producedFunctionCache, which only ever parses; no exec.Command"},
	{StageAtomVerification, ResourceClassSuiteRead,
		"checkAtomVerification (agent_stage_unit_verification.go) calls " +
			"runNamedTests, which runs `go test -run <pattern> ./...`"},
	{StageAtomFuzz, ResourceClassSuiteRead,
		"checkFuzzing (agent_stage_checks.go) runs " +
			"`go test -run=^$ -fuzz=. ./...`, which may write new corpus " +
			"entries under testdata/fuzz but never rewrites produced source"},
	{StageAtomMutation, ResourceClassExclusiveMutating,
		"checkMutations (agent_stage_mutation.go) calls os.WriteFile against " +
			"the exact produced-source path it is scoring, runs `go build` " +
			"and `go test` against the mutated file, then writes the " +
			"original bytes back before the next candidate — the only " +
			"checker in the flow that rewrites produced source in place"},
	{StageAntiPatterns, ResourceClassPureAST,
		"checkAntiPatterns (agent_stage_antipatterns.go) parses produced " +
			"source with go/parser and looks for syntactic patterns; no " +
			"exec.Command"},
	{StageAtomOptimization, ResourceClassPureAST,
		"checkSimplification (agent_stage_obligations.go) counts decision " +
			"points per attributed function and reports candidates; it does " +
			"not call os.WriteFile — PIPE-010 records that no rewrite is " +
			"performed yet — and no exec.Command"},
	{StageAtomComplexity, ResourceClassPureAST,
		"checkComplexity (agent_stage_structure.go) labels loop nesting from " +
			"the parsed tree; no exec.Command"},
	{StageAtomDocumentation, ResourceClassPureAST,
		"checkAtomDocumentation (agent_stage_structure.go, via " +
			"documentedNamesCached) reads the shared producedFunctionCache, " +
			"which only ever parses; no exec.Command"},

	// Phase C — molecules.
	{StageCompositionObligations, ResourceClassPureAST,
		"composeCompositionObligations (agent_proof_obligations.go) parses " +
			"produced source and writes to the durable obligation store, not " +
			"the worktree; no exec.Command"},
	{StageMoleculeTests, ResourceClassPureAST,
		"checkMoleculeTests (agent_stage_obligations.go) calls " +
			"readProducedFunctions and testsNaming, both go/parser only; no " +
			"exec.Command"},
	{StageMolecules, ResourceClassPureAST,
		"checkMolecules (agent_stage_structure.go) reads the shared " +
			"producedFunctionCache, which only ever parses; no exec.Command"},
	{StageMoleculeVerification, ResourceClassSuiteRead,
		"dischargeMoleculeVerification (agent_proof_obligations.go) calls " +
			"checkMoleculeVerification, whose verifyEachUnit runs " +
			"`go test -run <pattern> ./...` per molecule"},

	// Phase D — control flow.
	{StageControlObligations, ResourceClassPureAST,
		"composeControlObligations (agent_proof_obligations.go) parses " +
			"produced source and writes to the durable obligation store, not " +
			"the worktree; no exec.Command"},
	{StageControlTests, ResourceClassPureAST,
		"checkControlTests (agent_stage_obligations.go) calls " +
			"readProducedFunctions and testedNames, both go/parser only; no " +
			"exec.Command"},
	{StageControlFlow, ResourceClassPureAST,
		"dischargeControlFlow (agent_proof_obligations.go) calls " +
			"checkControlFlow, which counts branches from the parsed tree; " +
			"no exec.Command"},
	{StagePathCoverage, ResourceClassSuiteRead,
		"checkFunctionCoverage (agent_stage_unit_verification.go) runs " +
			"`go test -coverprofile=<worktree>/.codeflux-coverage.out ./...`"},

	// Phase E — the program.
	{StageAssembly, ResourceClassBuildOnly,
		"performed in AgentExecution.Run (agent_execution.go, not owned by " +
			"this lane); the flow's own gate says the pieces are 'wired into " +
			"a module that compiles, checked before anything is run', which " +
			"names compiling and nothing heavier — not verified against the " +
			"implementation"},
	{StageProgram, ResourceClassBuildOnly,
		"performed in AgentExecution.Run (agent_execution.go, not owned by " +
			"this lane); the flow's own gate is only 'a build artifact " +
			"exists' — not verified against the implementation"},
	{StageIntegrationTests, ResourceClassSuiteRead,
		"performed in AgentExecution.Run (agent_execution.go, not owned by " +
			"this lane); the flow's own gate is 'components are exercised " +
			"together, with nothing mocked', which means running the built " +
			"program — not verified against the implementation"},
	{StageEndToEndTests, ResourceClassSuiteRead,
		"performed by AgentExecution.checkAcceptance (agent_execution.go, not " +
			"owned by this lane); the flow's own gate is 'the built " +
			"executable reproduces every acceptance example exactly', which " +
			"means running the built binary — not verified against the " +
			"implementation"},

	// Phase F — verification depth.
	{StageGlobalInvariants, ResourceClassPureAST,
		"checkGlobalInvariants (agent_stage_checks.go) parses only import " +
			"declarations with parser.ImportsOnly; no exec.Command"},
	{StageAdversarial, ResourceClassSuiteRead,
		"performed by AgentExecution.probeProducedCommands (agent_execution.go, " +
			"not owned by this lane); the flow's own gate is about feeding " +
			"'hostile input' to the program, which means running the built " +
			"binary — not verified against the implementation"},
	{StageRepetition, ResourceClassSuiteRead,
		"checkRepetition (agent_stage_checks.go) runs `go test -count=1 " +
			"./...` three times"},
	{StagePlatformMatrix, ResourceClassSuiteRead,
		"checkPlatformMatrix (agent_stage_checks.go) runs `go test ./...` on " +
			"the host platform and `go build ./...` for each declared cross " +
			"target; classified by the heavier of the two calls it makes"},
	{StageNonFunctional, ResourceClassSuiteRead,
		"checkNonFunctional (agent_stage_checks.go) runs `go test -count=1 " +
			"./...` once, timed"},

	// Phase G — delivery. None of these read the worktree's source: the
	// bundle and the acceptance decision are both about the run's own
	// ledger, which lives in durable storage rather than the worktree.
	{StageEvidenceBundle, ResourceClassPureAST,
		"AgentExecution.assembleEvidence (agent_stage_delivery.go) reads " +
			"ListPipelineStages from durable storage, not the worktree"},
	{StageHumanAcceptance, ResourceClassPureAST,
		"performed in AgentExecution.Run (agent_execution.go, not owned by " +
			"this lane); a person's decision, recorded, touching no " +
			"worktree file"},
	{StageDeliver, ResourceClassPureAST,
		"performed in AgentExecution.Run (agent_execution.go, not owned by " +
			"this lane); the flow's own gate is about handing over the " +
			"accepted change and its evidence — not verified against the " +
			"implementation"},

	// PIPE-020/PIPE-042 stages, appended additively at the numeric end of
	// the flow; see their own doc comments in stages.go for why their
	// number does not match their conceptual position.
	{StageAcceptanceOracle, ResourceClassSuiteRead,
		"performed by AgentExecution.checkAcceptanceOracle (agent_execution.go, " +
			"not owned by this lane); the flow's own gate is about running " +
			"'every declared acceptance example' against the repository, " +
			"which means executing the built program — not verified against " +
			"the implementation"},
	{StageSkipAudit, ResourceClassPureAST,
		"checkSkipAudit (skip_audit.go, not owned by this lane) classifies " +
			"this run's own ledger rows, which live in durable storage, not " +
			"the worktree"},

	// PIPE-048/PIPE-049 stages, appended additively at the numeric end of
	// the flow for the same reason StageAcceptanceOracle/StageSkipAudit
	// were; see their own doc comments in stages.go.
	{StageAtomRegistration, ResourceClassPureAST,
		"AgentExecution.registerVerifiedAtoms (agent_stage_registration.go) " +
			"parses produced source with go/parser to locate a schema-v1 " +
			"//codeflux:atom comment and writes only to SQLite through " +
			"storage.CreateAtomDocumentationRevision; no exec.Command, no " +
			"os.WriteFile against the worktree"},
	{StageMoleculeRegistration, ResourceClassPureAST,
		"AgentExecution.registerVerifiedMolecules (agent_stage_registration.go) " +
			"is the same registerProducedDeclarations core as atom-registration, " +
			"over the produced functions that compose others; no exec.Command, " +
			"no os.WriteFile against the worktree"},
}

// ResourceClassFor returns the declared resource class for one stage.
func ResourceClassFor(stage Number) (ResourceClass, bool) {
	for _, entry := range ResourceClasses {
		if entry.Stage == stage {
			return entry.Class, true
		}
	}
	return "", false
}

// ResourceClassMap renders ResourceClasses as a lookup table, which is the
// shape a scheduler actually wants: one map read per stage rather than a
// linear scan of the declaration order every time.
func ResourceClassMap() map[Number]ResourceClass {
	classes := make(map[Number]ResourceClass, len(ResourceClasses))
	for _, entry := range ResourceClasses {
		classes[entry.Stage] = entry.Class
	}
	return classes
}

// ValidateResourceClasses reports every disagreement it can find between
// Flow and ResourceClasses, following the same shape
// ValidateRequirementsTable and ValidateChecks already established: every
// stage bound exactly once, every binding naming a real stage, and every
// binding using one of the four declared classes.
func ValidateResourceClasses() []string {
	return ValidateResourceClassesTable(ResourceClasses)
}

// ValidateResourceClassesTable is ValidateResourceClasses against an
// explicit table, so a test can feed a deliberately broken one and prove
// each check actually discriminates.
func ValidateResourceClassesTable(classes []StageResourceClass) []string {
	var findings []string

	bound := make(map[Number]int, len(classes))
	for _, entry := range classes {
		bound[entry.Stage]++
	}
	for _, stage := range Flow {
		switch bound[stage.Number] {
		case 1:
		case 0:
			findings = append(findings, fmt.Sprintf(
				"stage %d (%s) has no entry in ResourceClasses, so it cannot "+
					"be scheduled", stage.Number, stage.Name))
		default:
			findings = append(findings, fmt.Sprintf(
				"stage %d (%s) is bound %d times in ResourceClasses",
				stage.Number, stage.Name, bound[stage.Number]))
		}
	}

	declared := make(map[Number]struct{}, len(Flow))
	for _, stage := range Flow {
		declared[stage.Number] = struct{}{}
	}
	for _, entry := range classes {
		if _, exists := declared[entry.Stage]; !exists {
			findings = append(findings, fmt.Sprintf(
				"ResourceClasses binds stage %d, which is not in the flow",
				entry.Stage))
			continue
		}
		if !entry.Class.Valid() {
			findings = append(findings, fmt.Sprintf(
				"stage %d declares resource class %q, which is none of the "+
					"four PIPE-059 defines", entry.Stage, entry.Class))
		}
		if entry.Reason == "" {
			findings = append(findings, fmt.Sprintf(
				"stage %d declares a resource class with no reason recorded",
				entry.Stage))
		}
	}
	return findings
}
