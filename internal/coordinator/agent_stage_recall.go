package coordinator

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"

	"codeflux.dev/codeflux/internal/atomdoc"
	"codeflux.dev/codeflux/internal/pipeline"
	"codeflux.dev/codeflux/internal/retrieval/recallkey"
	"codeflux.dev/codeflux/internal/storage"
)

// recallKnownAtoms is the recall stage (PIPE-050, PIPE-051, PIPE-051a), and
// is also where PIPE-048, PIPE-049, PIPE-052, PIPE-054, and PIPE-055's own
// work happens, all from this one call site Run() (agent_execution.go, not
// owned by this change) already gives it:
//
//   - PIPE-052 gates a "reuse" decision on this run's own re-verification
//     (admitReuseDecisions), so a coincidental contract-hash agreement
//     cannot import an earlier run's blind spots;
//   - PIPE-048/PIPE-049 register this run's own produced atoms and
//     molecules once they have been examined
//     (registerAndRecordProducedDeclarations), so a later run's recall has
//     a populated registry to search rather than an empty one; and
//   - PIPE-054/PIPE-055 classify every write decision as a registry gap or
//     a recall miss against the registry as it stood before this run added
//     to it (classifyReuseRegret), never blocking recall's own outcome.
//
// PIPE-050 requires it to be binding: every contract this run needed leaves
// with a recorded decision, either `reuse` — naming the project function
// whose contract matched exactly — or `write` — carrying why nothing already
// stored could be reused. Before this change the stage ran, searched, and
// bound nothing: it reported a list of names that happened to already exist
// and nothing downstream had to answer to that list, which is exactly the
// defect docs/plan.md §2's compounding-effort thesis depends on not having —
// a recall stage nobody must answer to is a search that reports and changes
// nothing, and the mechanism the whole thesis rests on had never
// demonstrably fired.
//
// It replaces the substring match `strings.Contains(content, "func "+name+
// "(")` PIPE-051 names as the defect: that match finds a renamed atom never
// (the substring is built from the new identifier, which never appears in
// the old text) and a same-named different atom always (nothing about a name
// match compares what either function promises). This version indexes the
// project's earlier work by recallkey.ComputeContractHash — the type vector
// with names erased, the return-error classification, and the declared
// effects — so a rename with an unchanged contract is found, and a
// same-named function whose contract actually changed is correctly refused.
// recallkey.SignatureShape (PIPE-051a) narrows what a rejected contract's
// justification can name — "N functions share this shape" — but a shape
// match never admits a reuse decision by itself: AGENTS.md's boundary is
// enforced structurally in bindRecallDecisions below, which only ever
// branches to "reuse" from an exact ContractHash lookup.
//
// What binding still cannot do, and says so rather than pretending
// otherwise: this stage still runs after the attempt loop has already
// written the code (agent_execution.go calls it once assembly, integration
// tests, the adversarial probe, and end-to-end tests have all already run),
// not before the loop's first attempt as PIPE-050's "before anything is
// built" asks for. Moving that call requires restructuring
// AgentExecution.Run and the loop construction it feeds, both in
// agent_execution.go, which is out of this change's file ownership; see this
// repository's PIPE-050 completion report for the exact lines. What this
// stage does establish, today, at the point it runs: no contract is left
// without a decision, and no decision is ever admitted by a shape or
// similarity match alone.
//
// A `reuse` decision's own binding still only ever names a matching
// function, its stored artifact, and its contract hash from
// ListProjectSourceArtifactsExcludingTask -- the project's own earlier
// stored source -- because moving the binding search onto the structured
// atom registry PIPE-048 now populates is a change to bindRecallDecisions'
// own matching, not merely to what runs beside it, and is left for the
// ticket that widens recall's own search surface (which classifyReuseRegret
// below names, per write decision, as exactly the "recall-miss" case: the
// registry already has what recall's own artifact-based search did not
// find). What PIPE-048 does give this run, today, is the registry row
// itself -- documentation, contract hash, and the exact revision the atom
// was verified at -- written by registerAndRecordProducedDeclarations below,
// so a *later* run's own artifact-based search at least has real registered
// source to find, and this run's own regret classification can tell a
// registry gap from a recall miss rather than only ever reporting the former.
func (execution *AgentExecution) recallKnownAtoms(
	ctx context.Context,
	scope agentScope,
	worktree string,
) (stageOutcome, registrationOutcomes) {
	var registration registrationOutcomes
	functions, err := readProducedFunctions(worktree)
	if err != nil {
		return broke("the produced source could not be parsed: "+err.Error(), nil),
			registration
	}
	wanted := map[string]producedFunction{}
	for _, function := range functions {
		if isTestScaffolding(function) {
			continue
		}
		if _, seen := wanted[function.Name]; !seen {
			wanted[function.Name] = function
		}
	}
	if len(wanted) == 0 {
		return skipped("the run needed no atom, so there was nothing to recall"), registration
	}

	// Earlier work means earlier: the artifacts this run has already stored
	// are excluded. Without that, a run matches against the file it wrote
	// moments ago and reports that everything it needed already existed —
	// which is true, and is the least useful true thing it could say.
	known, err := execution.repositories.ListProjectSourceArtifactsExcludingTask(
		ctx, scope.projectID, scope.taskID, 200)
	if err != nil {
		return broke("the project's earlier work could not be read: "+
				err.Error(), nil),
			registration
	}

	contractIndex, shapeIndex, unparseable := indexKnownArtifactContracts(known)

	decisions, bindErr := bindRecallDecisions(wanted, contractIndex, shapeIndex)
	if bindErr != nil {
		// This is the invariant that makes "binding" checkable rather than
		// asserted: every contract this run needed must leave with a
		// decision, and a recall that silently drops one is indistinguishable
		// from one that quietly admitted it.
		return broke(bindErr.Error(), map[string]any{
			"functions_needed": len(wanted),
		}), registration
	}

	// PIPE-054/PIPE-055: the registry "as it now stands" is read once, before
	// this run registers anything of its own further down, so a write
	// decision is classified against prior work rather than against rows
	// this same call is about to add. classifyReuseRegret turns every write
	// decision into a registry-gap (nothing usable was ever deposited) or a
	// recall-miss (the structured registry has an exact match this recall
	// pass's own artifact-based search did not find) — two different defects
	// with two different repairs, which is the distinction PIPE-055 asks for.
	registry := loadRegisteredContractIndex(ctx, execution, scope)
	regrets := classifyReuseRegret(wanted, decisions, registry)

	// PIPE-052: a reuse decision is admitted only once this run's own
	// case-synthesis-derived verification (StageAtomVerification) is
	// confirmed to have held for this attempt — otherwise a contract-hash
	// match is downgraded to a written justification, so a run whose own
	// tests did not pass cannot vouch, by a coincidental hash agreement,
	// that its own atom demonstrated anything.
	verificationChecked, verificationHeld := reVerificationHeldThisAttempt(ctx, execution, scope)
	decisions = admitReuseDecisions(decisions, verificationChecked, verificationHeld)

	// PIPE-048/PIPE-049: this run's own produced work is registered now that
	// it has been examined by every earlier stage, so a later run's recall
	// has a populated registry — not merely stored source text — to search.
	// This never blocks: a registration failure is recorded in its own
	// stage's evidence and does not change recall's own outcome below.
	// Registration requires that the atom pipeline actually verified these
	// atoms, not merely that a run happened.
	//
	// Ladder rung 5 registered three atoms while atom-case synthesis had
	// failed, atom-example-tests and atom-verification were blocked behind it,
	// and the task itself ended failed. A registry row says "a later task may
	// reuse this instead of building it", and saying that about code whose own
	// verification never ran poisons every project that reads it — the whole
	// value of the registry is that its contents were checked.
	//
	// The rows are not lost. They are simply not written yet: a later run over
	// the same project, whose atom phase does hold, will register the same
	// declarations from the same contracts.
	// Withheld when verification was performed and did not hold, rather than
	// whenever it is not known to have held. The distinction matters: a flow
	// that never reached the stage has established nothing either way, and
	// refusing on that would stop registration in every context except a full
	// run — including the fixtures that prove registration works at all.
	var atomRecords, moleculeRecords []registrationRecord
	if verificationChecked && !verificationHeld {
		registration = registrationOutcomes{
			atom: skipped("this run's atom verification did not hold, so its " +
				"atoms are not admitted: a registry row asserts that a later " +
				"task may reuse this instead of building it"),
			molecule: skipped("this run's atom verification did not hold, so " +
				"its molecules are not admitted"),
		}
		tracef("memory", "registration withheld — atom verification did not hold")
	} else {
		atomRecords, moleculeRecords = execution.registerAndRecordProducedDeclarations(
			ctx, scope, worktree, wanted, registry)
	}
	// Handed back so the run's own ledger records these stages, rather than
	// leaving the ledger to declare them not-implemented while registration
	// writes a second row saying it ran. Rung 3 showed exactly that
	// disagreement: atom-registration and molecule-registration both read
	// "no part of this build performs this stage" in the same run in which
	// registerAndRecordProducedDeclarations had just been called.
	if !verificationChecked || verificationHeld {
		registration = registrationOutcomes{
			atom:     registrationStageOutcome("atom", atomRecords),
			molecule: registrationStageOutcome("molecule", moleculeRecords),
		}
	}

	reused := reusedFunctionNames(decisions)
	evidence := map[string]any{
		"functions_needed":       len(wanted),
		"decisions":              decisions,
		"already_in_project":     reused,
		"artifacts_searched":     len(known),
		"unparseable_artifacts":  unparseable,
		"reuse_regrets":          regrets,
		"reuse_regret_count":     len(regrets),
		"reverification_checked": verificationChecked,
		"reverification_held":    verificationHeld,
		"atoms_registered":       countRegistered(atomRecords),
		"molecules_registered":   countRegistered(moleculeRecords),
	}
	if len(known) == 0 {
		return held("the project holds no earlier work, so every contract "+
			"carries a write decision and nothing was rebuilt unnecessarily",
			evidence), registration
	}
	return held(fmt.Sprintf(
			"%d contract(s) bound: %d reused by an exact contract-hash match, "+
				"%d carry a written justification to rebuild",
			len(decisions), len(reused), len(decisions)-len(reused)), evidence),
		registration
}

// registrationOutcomes carries what the two registration stages decided, so
// the run's own ledger can record them rather than declaring them unperformed.
type registrationOutcomes struct {
	atom     stageOutcome
	molecule stageOutcome
}

// registrationStageOutcome turns registration's records into the stage result
// the ledger records.
//
// The three cases say three different things and must not collapse: nothing was
// produced to register, something was produced and registered, and something
// was produced and refused for carrying no schema documentation. The last is
// the one that mattered — it is why the registry stayed empty for a whole
// session — and reading it as "skipped" hides it.
func registrationStageOutcome(kind string, records []registrationRecord) stageOutcome {
	registered := 0
	for _, record := range records {
		if record.Registered {
			registered++
		}
	}
	// The reasons are carried, not just the count. A registry that never fills
	// is the defect this whole path exists to prevent, and "0 of 2" says it
	// happened without saying why -- which is how it went unnoticed for a
	// session.
	reasons := map[string]string{}
	for _, record := range records {
		if !record.Registered && record.SkipReason != "" {
			reasons[record.Name] = record.SkipReason
		}
	}
	evidence := map[string]any{
		"produced": len(records), "registered": registered,
		"refused": reasons,
	}
	for name, why := range reasons {
		tracef("memory", "  %s not registered — %s", name, traceOneLine(why, 110))
	}
	switch {
	case len(records) == 0:
		return skipped("this run produced no candidate " + kind)
	case registered > 0:
		return held(fmt.Sprintf(
			"%d of %d produced %s(s) registered with schema-v1 documentation, "+
				"so a later task can find them",
			registered, len(records), kind), evidence)
	default:
		return broke(fmt.Sprintf(
			"0 of %d produced %s(s) reached the registry, so no later task can "+
				"find this work: %s", len(records), kind,
			refusalSummary(reasons)), evidence)
	}
}

// recallDecision is the binding verdict PIPE-050 requires for one contract:
// either it is reused, naming the matching function and the exact key that
// matched it, or it is written, carrying why nothing reusable was found.
// Exactly one of the two field groups is populated, selected by Decision.
type recallDecision struct {
	// Decision is "reuse" or "write". Nothing else is a valid value; every
	// value bindRecallDecisions assigns comes from one of exactly those two
	// branches.
	Decision string

	// MatchedFunction, MatchedArtifact and ContractHash are set only when
	// Decision == "reuse".
	MatchedFunction string
	MatchedArtifact string
	ContractHash    string

	// Justification is set only when Decision == "write".
	Justification string
}

// knownFunction is one function found in the project's earlier stored
// source, indexed for recall.
type knownFunction struct {
	Name       string
	ArtifactID string
}

// bindRecallDecisions is recall's binding core (PIPE-050).
//
// It is kept pure and separate from the storage and parsing around it so the
// binding guarantee can be driven directly by a table test without a
// database or a worktree: given every contract this run needs and an index
// of what the project already has, it returns exactly one decision per
// contract, or an error naming which contract was left undecided.
//
// A `reuse` decision is reached only through an exact match in
// contractIndex — never from shapeIndex, which exists solely to make a
// `write` decision's justification more informative. That is the structural
// enforcement of AGENTS.md's "vector similarity may discover candidates; it
// never establishes compatibility, validity, assurance, or permission" for
// this stage's own shape key: shapeIndex is read here in exactly one place,
// and the value it produces is a candidate count for a justification
// string, never a decision.
func bindRecallDecisions(
	wanted map[string]producedFunction,
	contractIndex map[atomdoc.ContractHash][]knownFunction,
	shapeIndex map[recallkey.SignatureShape][]knownFunction,
) (map[string]recallDecision, error) {
	decisions := make(map[string]recallDecision, len(wanted))
	for name, function := range wanted {
		contract := normalizeWantedContract(function)
		hash := recallkey.ComputeContractHash(contract)

		if matches := contractIndex[hash]; len(matches) > 0 {
			match := matches[0]
			decisions[name] = recallDecision{
				Decision:        "reuse",
				MatchedFunction: match.Name,
				MatchedArtifact: match.ArtifactID,
				ContractHash:    hash.String(),
			}
			continue
		}

		justification := "no project function shares this contract"
		if candidates := len(shapeIndex[contract.Shape()]); candidates > 0 {
			justification = fmt.Sprintf(
				"%d project function(s) share this signature's parameter and "+
					"result types but differ in return-error handling or "+
					"declared effects, so none is an exact contract match",
				candidates)
		}
		decisions[name] = recallDecision{
			Decision:      "write",
			Justification: justification,
		}
	}

	if len(decisions) != len(wanted) {
		var missing []string
		for name := range wanted {
			if _, bound := decisions[name]; !bound {
				missing = append(missing, name)
			}
		}
		sort.Strings(missing)
		return nil, fmt.Errorf(
			"recall is not binding: %d contract(s) needed, %d decision(s) "+
				"recorded; missing a decision for %s",
			len(wanted), len(decisions), strings.Join(missing, ", "))
	}
	return decisions, nil
}

// normalizeWantedContract projects a producedFunction onto the fields
// recallkey.ComputeContractHash and .Shape() need. Parameters and Results
// are already name-erased at the parse site (agent_stage_structure.go's
// fieldNames: "Names are deliberately left out"), so this is a re-shaping,
// not a second normalization pass.
func normalizeWantedContract(function producedFunction) recallkey.NormalizedContract {
	return recallkey.NormalizedContract{
		Parameters:   function.Parameters,
		Results:      function.Results,
		ReturnsError: function.ReturnsError,
		Effects:      effectsOf(function),
	}
}

// indexKnownArtifactContracts parses every stored Go artifact the project
// already has and indexes each function it declares by both recall keys.
//
// An artifact that fails to parse is skipped and counted rather than failing
// the whole stage: stored content that predates a language change, or
// content that was never meant to compile standing alone, must not stop
// recall from matching everything that does parse.
func indexKnownArtifactContracts(
	artifacts []storage.Artifact,
) (
	contractIndex map[atomdoc.ContractHash][]knownFunction,
	shapeIndex map[recallkey.SignatureShape][]knownFunction,
	unparseable int,
) {
	contractIndex = map[atomdoc.ContractHash][]knownFunction{}
	shapeIndex = map[recallkey.SignatureShape][]knownFunction{}
	for _, artifact := range artifacts {
		functions, err := parseKnownContracts(string(artifact.Content))
		if err != nil {
			unparseable++
			continue
		}
		for _, function := range functions {
			known := knownFunction{
				Name:       function.Name,
				ArtifactID: artifact.ID.String(),
			}
			contract := recallkey.NormalizedContract{
				Parameters:   function.Parameters,
				Results:      function.Results,
				ReturnsError: function.ReturnsError,
				Effects:      effectsOf(function),
			}
			hash := recallkey.ComputeContractHash(contract)
			contractIndex[hash] = append(contractIndex[hash], known)
			shapeIndex[contract.Shape()] = append(shapeIndex[contract.Shape()], known)
		}
	}
	return contractIndex, shapeIndex, unparseable
}

// parseKnownContracts parses one stored artifact's Go source text into its
// declared functions' contracts.
//
// It exists because recall's only searchable registry today is stored
// source content (ListProjectSourceArtifactsExcludingTask), not a file on
// disk, so it cannot reuse readProducedFunctions' worktree-relative path. It
// reuses the same AST-level primitives readProducedFunctions itself uses —
// fieldNames for a name-erased type vector, callsAnythingImpure for effect
// classification — so a stored function and a produced one are normalized
// identically and a match is never an artifact of two different derivations
// happening to agree.
func parseKnownContracts(content string) ([]producedFunction, error) {
	fileSet := token.NewFileSet()
	tree, err := parser.ParseFile(fileSet, "", content, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	var functions []producedFunction
	for _, declaration := range tree.Decls {
		function, isFunction := declaration.(*ast.FuncDecl)
		if !isFunction || function.Name == nil {
			continue
		}
		results := fieldNames(function.Type.Results)
		returnsError := false
		for _, result := range results {
			if result == "error" {
				returnsError = true
			}
		}
		functions = append(functions, producedFunction{
			Name:         function.Name.Name,
			Parameters:   fieldNames(function.Type.Params),
			Results:      results,
			ReturnsError: returnsError,
			Pure:         !callsAnythingImpure(function),
		})
	}
	return functions, nil
}

// reusedFunctionNames returns every contract name recall decided to reuse,
// sorted for a deterministic report.
func reusedFunctionNames(decisions map[string]recallDecision) []string {
	var names []string
	for name, decision := range decisions {
		if decision.Decision == "reuse" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// reVerificationHeldThisAttempt reports whether this run's own
// StageAtomVerification row is recorded, for the attempt
// currentAttemptBestEffort resolves, and if so, whether it held (PIPE-052).
//
// checked is false whenever nothing could be read at all: no repository, no
// task identity, a storage error, or simply no row recorded yet for that
// stage this attempt (a fixture calling recallKnownAtoms directly, never
// through the full Run flow, most commonly). admitReuseDecisions treats an
// unchecked result as "not disproven" rather than as a failure, which is
// what keeps this addition from silently downgrading every reuse decision
// in a test or a build that never wired StageAtomVerification's own ledger
// write — the same fail-open posture this file already takes for every
// other storage read that recall's own binding guarantee does not depend
// on.
func reVerificationHeldThisAttempt(
	ctx context.Context, execution *AgentExecution, scope agentScope,
) (checked bool, held bool) {
	if execution == nil || execution.repositories == nil || scope.taskID.IsZero() {
		return false, false
	}
	attempt := currentAttemptBestEffort(ctx, execution.repositories, scope.taskID)
	records, err := execution.repositories.ListPipelineStages(ctx, scope.taskID, attempt)
	if err != nil {
		return false, false
	}
	for _, record := range records {
		if record.Stage == pipeline.StageAtomVerification {
			return true, record.State == pipeline.StateSatisfied
		}
	}
	return false, false
}

// admitReuseDecisions applies PIPE-052's re-verification boundary on top of
// bindRecallDecisions' own contract-hash matching.
//
// A reuse decision bindRecallDecisions already reached from an exact
// contract-hash match is only left standing once this run's own
// case-synthesis-derived verification (StageAtomVerification) is confirmed
// to have held for this attempt. When it is confirmed NOT to have held,
// every reuse decision is downgraded to a written justification: a run
// whose own tests did not pass cannot vouch, from a coincidental hash
// agreement alone, that the atom sharing that contract ever demonstrated
// the behaviour the hash claims — which is exactly the "reuse cannot import
// the earlier run's blind spots" PIPE-052 asks for. When re-verification
// could not be checked at all (verificationChecked is false), every
// decision is returned unchanged: PIPE-050/PIPE-051's existing binding
// guarantee must not regress because this addition's own read found
// nothing to say either way.
func admitReuseDecisions(
	decisions map[string]recallDecision,
	verificationChecked bool,
	verificationHeld bool,
) map[string]recallDecision {
	if !verificationChecked || verificationHeld {
		return decisions
	}
	admitted := make(map[string]recallDecision, len(decisions))
	for name, decision := range decisions {
		if decision.Decision != "reuse" {
			admitted[name] = decision
			continue
		}
		admitted[name] = recallDecision{
			Decision: "write",
			Justification: fmt.Sprintf(
				"the contract hash matched %s (artifact %s), but this run's "+
					"own atom-verification did not hold this attempt, so the "+
					"match is not admitted (PIPE-052): reuse must not import an "+
					"earlier run's blind spots through a contract this run has "+
					"not itself shown passes",
				decision.MatchedFunction, decision.MatchedArtifact),
		}
	}
	return admitted
}

// classifyReuseRegret is PIPE-054/PIPE-055's reuse-regret classification.
//
// For every contract this recall pass left as a write decision (nothing in
// the artifact-based search — indexKnownArtifactContracts — matched), it
// asks one further question against the structured atom registry read by
// loadRegisteredContractIndex, "as it now stands" before this run's own
// registration further down adds to it: does the registry already carry a
// revision with this exact contract hash?
//
//   - No: nothing usable for this contract was ever deposited anywhere
//     recall could have looked. Classified "registry-gap" — the repair is
//     registering more, or earlier, not widening recall's own search.
//   - Yes: the registry has an exact match this recall pass's own
//     artifact-based search did not find, because that search reads stored
//     source text, not the structured registry. Classified "recall-miss" —
//     the repair is recall's own search surface, not the registry.
//
// A reuse decision is never a regret: recall already found what it needed.
// Only a write decision — one recall could not admit — is worth asking
// whether a better answer existed somewhere this pass did not look.
func classifyReuseRegret(
	wanted map[string]producedFunction,
	decisions map[string]recallDecision,
	registryContractIndex map[atomdoc.ContractHash]storage.AtomDocumentationRevision,
) map[string]string {
	regrets := map[string]string{}
	for name, decision := range decisions {
		if decision.Decision != "write" {
			continue
		}
		function, ok := wanted[name]
		if !ok {
			continue
		}
		hash := recallkey.ComputeContractHash(normalizeWantedContract(function))
		if _, found := registryContractIndex[hash]; found {
			regrets[name] = "recall-miss"
			continue
		}
		regrets[name] = "registry-gap"
	}
	return regrets
}

// countRegistered counts how many of a registration pass's records actually
// registered something, for a compact evidence summary alongside the full
// per-declaration record list a caller can already read from the stage's
// own ledger row (recordRegistrationStageResult).
func countRegistered(records []registrationRecord) int {
	count := 0
	for _, record := range records {
		if record.Registered {
			count++
		}
	}
	return count
}

// refusalSummary renders why nothing was registered, shortest first.
func refusalSummary(reasons map[string]string) string {
	if len(reasons) == 0 {
		return "no reason was recorded, which is itself a defect"
	}
	names := make([]string, 0, len(reasons))
	for name := range reasons {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, name+": "+reasons[name])
	}
	return strings.Join(parts, "; ")
}
