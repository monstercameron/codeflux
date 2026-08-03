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
	"codeflux.dev/codeflux/internal/retrieval/recallkey"
	"codeflux.dev/codeflux/internal/storage"
)

// recallKnownAtoms is the recall stage (PIPE-050, PIPE-051, PIPE-051a).
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
// It also cannot yet name a registered atom's evidence or verified revision,
// because PIPE-048 (the atom-registration stage that writes those rows) does
// not exist — the only registry this stage can search is
// ListProjectSourceArtifactsExcludingTask, the project's own earlier stored
// source, which carries no evidence link and no verified revision to report.
// A `reuse` decision therefore names the matching function, its artifact,
// and its contract hash, and is explicit that no registered evidence exists
// to attach, rather than inventing either.
func (execution *AgentExecution) recallKnownAtoms(
	ctx context.Context,
	scope agentScope,
	worktree string,
) stageOutcome {
	functions, err := readProducedFunctions(worktree)
	if err != nil {
		return broke("the produced source could not be parsed: "+err.Error(), nil)
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
		return skipped("the run needed no atom, so there was nothing to recall")
	}

	// Earlier work means earlier: the artifacts this run has already stored
	// are excluded. Without that, a run matches against the file it wrote
	// moments ago and reports that everything it needed already existed —
	// which is true, and is the least useful true thing it could say.
	known, err := execution.repositories.ListProjectSourceArtifactsExcludingTask(
		ctx, scope.projectID, scope.taskID, 200)
	if err != nil {
		return broke("the project's earlier work could not be read: "+
			err.Error(), nil)
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
		})
	}

	reused := reusedFunctionNames(decisions)
	evidence := map[string]any{
		"functions_needed":      len(wanted),
		"decisions":             decisions,
		"already_in_project":    reused,
		"artifacts_searched":    len(known),
		"unparseable_artifacts": unparseable,
	}
	if len(known) == 0 {
		return held("the project holds no earlier work, so every contract "+
			"carries a write decision and nothing was rebuilt unnecessarily",
			evidence)
	}
	return held(fmt.Sprintf(
		"%d contract(s) bound: %d reused by an exact contract-hash match, "+
			"%d carry a written justification to rebuild",
		len(decisions), len(reused), len(decisions)-len(reused)), evidence)
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
