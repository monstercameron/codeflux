package coordinator

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// mutationBuildTimeout and mutationSuiteTimeout bound the mediated build and
// test-suite commands checkMutations runs once per sampled mutant. The
// suite gets the longer budget because a repository's full `go test ./...`
// legitimately takes far longer than compiling it.
const (
	mutationBuildTimeout = 5 * time.Minute
	mutationSuiteTimeout = 10 * time.Minute
)

// operatorMutationRule names the replacement token and the plain-language
// reason for one deliberately introduced defect, keyed by the token being
// replaced.
//
// The set is the same ten mistakes the byte-level predecessor tried — an
// inverted comparison, a boundary moved in or out, the wrong arithmetic
// operator, a dropped negation of a conjunction or disjunction — because
// PIPE-128 changes *where* a mutation is allowed to land, not *what* it
// tries. Keyed by go/token.Token rather than by source text, so a match can
// only ever be a real Go operator the parser itself recognised as one: a
// go/token.Token exists only where the syntax tree says an operator does,
// never inside a string literal or a comment, both of which the parser
// represents as opaque text with no operator token at all.
type operatorMutationRule struct {
	To  token.Token
	Why string
}

var operatorMutations = map[token.Token]operatorMutationRule{
	token.EQL:  {token.NEQ, "equality inverted"},
	token.NEQ:  {token.EQL, "inequality inverted"},
	token.LSS:  {token.LEQ, "boundary moved outward"},
	token.GTR:  {token.GEQ, "boundary moved outward"},
	token.LEQ:  {token.LSS, "boundary moved inward"},
	token.GEQ:  {token.GTR, "boundary moved inward"},
	token.ADD:  {token.SUB, "addition became subtraction"},
	token.MUL:  {token.ADD, "multiplication became addition"},
	token.LAND: {token.LOR, "conjunction became disjunction"},
	token.LOR:  {token.LAND, "disjunction became conjunction"},
}

// astMutationCandidate is one place in a produced source file where the
// syntax tree — not the file's bytes — says a mutable operator sits, and
// where attribution (PIPE-114) says this run is answerable for the line it
// sits on.
//
// Offset is the byte offset of the operator token itself, inside the exact
// file content it was parsed from, which is what makes the eventual
// replacement (mutateAtOffset) surgical: one token is spliced out and its
// replacement spliced in, nothing else in the file is touched or
// reformatted.
type astMutationCandidate struct {
	File   string
	Line   int
	Offset int
	From   token.Token
	To     token.Token
	Why    string
}

// discoverASTMutationCandidates walks one produced Go source file's syntax
// tree and returns every binary-operator token PIPE-128 knows how to mutate,
// restricted to lines attribution (PIPE-114) says this run changed.
//
// Walking ast.Inspect rather than scanning file bytes is the whole of
// PIPE-128: go/parser represents a string literal as one opaque
// *ast.BasicLit token and a comment as text the parser attaches to the file
// but never wires into an expression at all, so neither can ever produce an
// *ast.BinaryExpr. A candidate returned from here is therefore never inside
// a string literal or a comment, by construction of the tree rather than by
// a rule this function has to enforce.
//
// A file that fails to parse contributes no candidates rather than failing
// the whole scan: mutation discovery is best-effort over whatever source a
// run produced, and one file with a transient syntax problem must not blind
// the check to every candidate in every other file.
func discoverASTMutationCandidates(
	worktree, file string, attribution changeAttribution,
) []astMutationCandidate {
	source, err := os.ReadFile(filepath.Join(worktree, file))
	if err != nil {
		return nil
	}
	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, file, source, 0)
	if err != nil {
		return nil
	}
	var candidates []astMutationCandidate
	ast.Inspect(parsed, func(node ast.Node) bool {
		binary, ok := node.(*ast.BinaryExpr)
		if !ok {
			return true
		}
		rule, known := operatorMutations[binary.Op]
		if !known {
			return true
		}
		position := fileSet.Position(binary.OpPos)
		if !attribution.TouchesLine(file, position.Line) {
			return true
		}
		candidates = append(candidates, astMutationCandidate{
			File: file, Line: position.Line, Offset: position.Offset,
			From: binary.Op, To: rule.To, Why: rule.Why,
		})
		return true
	})
	return candidates
}

// sampleMutationCandidatesAcrossAttributedLines picks up to budget candidates
// spread across the full ordered list rather than the first budget of them
// (PIPE-129).
//
// candidates is expected sorted by file and then position, which is what
// makes "spread across" meaningful: taking every len(candidates)/budget'th
// entry visits the start, the middle, and the end of the attributed change
// in roughly equal measure, so the sample — and therefore the score — is not
// an artifact of which file or which line happened to come first. When
// candidates already fits inside the budget, every candidate is used and no
// sampling decision is made at all.
func sampleMutationCandidatesAcrossAttributedLines(
	candidates []astMutationCandidate, budget int,
) []astMutationCandidate {
	if budget <= 0 || len(candidates) <= budget {
		return candidates
	}
	sampled := make([]astMutationCandidate, 0, budget)
	for index := 0; index < budget; index++ {
		sampled = append(sampled, candidates[index*len(candidates)/budget])
	}
	return sampled
}

// mutateAtOffset splices candidate's replacement token into path's current
// content in place of its original token, at the exact byte offset the
// candidate was discovered at, and returns the file's untouched original
// content so the caller can restore it.
//
// It re-reads the file rather than trusting content captured at discovery
// time, and re-checks that the bytes at Offset still spell the expected
// operator, because discovery and mutation are separated by every earlier
// candidate's own write-and-restore cycle; a mismatch means the file changed
// out from under this run and the candidate is refused rather than spliced
// in blind.
func mutateAtOffset(path string, candidate astMutationCandidate) (original, mutated []byte, err error) {
	original, err = os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	from := candidate.From.String()
	to := candidate.To.String()
	end := candidate.Offset + len(from)
	if end > len(original) || string(original[candidate.Offset:end]) != from {
		return nil, nil, fmt.Errorf(
			"offset %d no longer holds %q in %s", candidate.Offset, from, path)
	}
	mutated = make([]byte, 0, len(original)-len(from)+len(to))
	mutated = append(mutated, original[:candidate.Offset]...)
	mutated = append(mutated, []byte(to)...)
	mutated = append(mutated, original[end:]...)
	return original, mutated, nil
}

// checkMutations measures whether the tests can detect a defect in what this
// run changed.
//
// Every other check in the flow asks whether the tests pass. This asks the
// question underneath it: if the code were wrong, would anybody know? A
// suite that passes against deliberately broken code is not evidence of
// anything, and until this ran there was no way to tell that suite apart
// from a good one.
//
// Three defects made the score before PIPE-127/128/129 inflatable for free,
// and each is closed at a different point below:
//
//   - PIPE-127: a mutant that does not compile is discarded before the
//     suite ever runs, via worktreeBuilds, and is counted in neither
//     mutationsApplied nor mutationsCaught. A mutant that never ran proves
//     nothing about the tests, so it is not scored as if it had.
//   - PIPE-128: candidates come only from discoverASTMutationCandidates,
//     which walks the syntax tree rather than the file's bytes, so a
//     candidate can never be a string literal's contents or a comment.
//   - PIPE-129: sampleMutationCandidatesAcrossAttributedLines spreads the
//     bounded number of attempts across the whole ordered candidate list
//     rather than only its first entries.
//
// Mutations are restricted to attribution's changed line ranges (PIPE-114):
// a mutant planted in pre-existing code the run never touched measures
// whether somebody else's tests catch somebody else's defects, which is not
// this run's claim to make and not a score this run can be held to. When
// attribution could not be established, every produced source line is
// eligible, which is the prior whole-file behaviour and the correct
// fail-toward-inclusion default.
func (execution *AgentExecution) checkMutations(
	ctx context.Context,
	worktree string,
	attribution changeAttribution,
) stageOutcome {
	// Scanned from attribution's own file set when established, rather than
	// from producedGoFiles directly: producedGoFiles reads `git status`,
	// which only sees uncommitted edits and goes blind the moment a run
	// commits to its own worktree, which would otherwise report nothing
	// worth mutating instead of narrowing correctly (PIPE-111's design
	// caution).
	files, err := attributionOrProducedFiles(worktree, attribution)
	if err != nil {
		return broke("the produced source could not be read: "+err.Error(), nil)
	}
	var sources []string
	for _, file := range files {
		if !strings.HasSuffix(file, "_test.go") {
			sources = append(sources, file)
		}
	}
	if len(sources) == 0 {
		return skipped("the run produced no source to mutate")
	}

	var candidates []astMutationCandidate
	for _, file := range sources {
		candidates = append(candidates,
			discoverASTMutationCandidates(worktree, file, attribution)...)
	}
	sort.Slice(candidates, func(first, second int) bool {
		if candidates[first].File != candidates[second].File {
			return candidates[first].File < candidates[second].File
		}
		return candidates[first].Offset < candidates[second].Offset
	})
	if len(candidates) == 0 {
		if attribution.Established {
			return skipped(
				"none of this run's changed lines held anything worth mutating")
		}
		return skipped("the produced source held nothing worth mutating")
	}

	// A bounded number of mutations, because each one that reaches the suite
	// costs a full test run and the answer stops changing shape well before
	// the budget is spent.
	const maximumMutations = 12
	sample := sampleMutationCandidatesAcrossAttributedLines(candidates, maximumMutations)

	applied, caught, discardedUncompilable := 0, 0, 0
	var survivors []string
	for _, candidate := range sample {
		path := filepath.Join(worktree, candidate.File)
		original, mutated, mutateErr := mutateAtOffset(path, candidate)
		if mutateErr != nil {
			continue
		}
		if writeErr := os.WriteFile(path, mutated, 0o600); writeErr != nil {
			continue
		}

		// PIPE-127: build before the suite ever runs. A mutant that does not
		// compile is discarded — never scored as caught, never scored as a
		// survivor — because a build failure is not the tests noticing
		// anything; it is the compiler refusing to hand them a program to
		// run at all.
		if !execution.worktreeBuilds(ctx, worktree) {
			discardedUncompilable++
			if restoreErr := os.WriteFile(path, original, 0o600); restoreErr != nil {
				return broke("a mutation could not be undone, so the worktree "+
					"may hold deliberately broken code: "+restoreErr.Error(), nil)
			}
			continue
		}

		applied++
		if execution.suiteRejects(ctx, worktree) {
			caught++
		} else {
			survivors = append(survivors, fmt.Sprintf(
				"%s:%d: %s", candidate.File, candidate.Line, candidate.Why))
		}
		// The file is restored before the next mutation regardless of the
		// outcome. Leaving a mutation in place would ship deliberately
		// broken code, which is the one failure this check must not cause.
		if restoreErr := os.WriteFile(path, original, 0o600); restoreErr != nil {
			return broke("a mutation could not be undone, so the worktree "+
				"may hold deliberately broken code: "+restoreErr.Error(), nil)
		}
	}

	evidenceBase := map[string]any{
		"mutations_candidates":             len(candidates),
		"mutations_sampled":                len(sample),
		"mutations_applied":                applied,
		"mutations_caught":                 caught,
		"mutations_discarded_uncompilable": discardedUncompilable,
		"attribution_established":          attribution.Established,
	}
	if applied == 0 {
		if discardedUncompilable > 0 {
			return skippedWith(fmt.Sprintf(
				"every sampled mutation (%d) failed to build, so none of them "+
					"ever reached the suite", discardedUncompilable), evidenceBase)
		}
		if attribution.Established {
			return skipped(
				"none of this run's changed lines held anything worth mutating")
		}
		return skipped("the produced source held nothing worth mutating")
	}
	score := float64(caught) / float64(applied) * 100
	evidenceBase["score_percent"] = score
	evidenceBase["survivors"] = survivors
	// A suite that catches nothing is worthless; one that catches most is
	// doing its job. The threshold is deliberately low, because the point of
	// the first version of this check is to catch the suite that detects
	// nothing at all, and a high bar would make it fire on work that is merely
	// imperfect.
	const threshold = 50.0
	if score < threshold {
		return broke(fmt.Sprintf(
			"the tests caught %d of %d deliberate defects (%.0f%%), so they "+
				"cannot be relied on to notice a real one: %s",
			caught, applied, score, strings.Join(survivors, "; ")), evidenceBase)
	}
	return held(fmt.Sprintf(
		"the tests caught %d of %d deliberate defects (%.0f%%)",
		caught, applied, score), evidenceBase)
}

// firstAttributedLine returns the zero-based index of the first line in
// lines that both contains pattern and falls on a line attribution says this
// run changed, in line order — or -1 when no such line exists.
//
// checkMutations no longer calls this: PIPE-128 moved mutant placement to
// discoverASTMutationCandidates, which finds an operator by walking the
// syntax tree rather than by matching text, and so cannot land inside a
// string literal or a comment the way a byte-level scan can. This is kept,
// unchanged, because TestPIPE114_MutationTargetsOnlyAttributedLines exercises
// it directly as the byte-level selection rule it documents, and
// TestPIPE128 (agent_stage_mutation_test.go) calls it again deliberately, to
// show the exact false match the AST replacement does not make.
func firstAttributedLine(
	lines []string, file, pattern string, attribution changeAttribution,
) int {
	for index, line := range lines {
		lineNumber := index + 1
		if !attribution.TouchesLine(file, lineNumber) {
			continue
		}
		if strings.Contains(line, pattern) {
			return index
		}
	}
	return -1
}

// worktreeBuilds reports whether the worktree's module builds as it stands.
//
// Run before the suite (PIPE-127), never after: `go test` would also fail on
// a mutant that does not compile, but by the time it does, the test binary
// build failure and a real test failure look the same to a caller reading
// only an exit code, and today's score conflates them. Checking the build in
// isolation first is what lets checkMutations tell "never ran" apart from
// "ran and survived."
//
// PIPE-131: mediated rather than a direct exec.CommandContext call. This
// runs against a worktree that, moments earlier, checkMutations spliced a
// deliberately broken operator into -- the build itself is trusted (it is
// this package's own fixed argument array), but a mutated build can still
// trigger arbitrary compiler-directive or generated-code paths in a
// repository this run does not control the contents of, and the tests this
// step's sibling runs next are unreviewed repository code by definition.
func (execution *AgentExecution) worktreeBuilds(ctx context.Context, worktree string) bool {
	result, err := execution.runMediatedVerificationCommand(
		ctx, worktree, worktree, "mutation-build",
		[]string{"go", "build", "./..."}, "",
		mutationBuildTimeout, goToolchainEnvironmentNames)
	return err == nil && result.Succeeded
}

// suiteRejects reports whether the repository's tests fail as they stand.
//
// PIPE-131: mediated rather than a direct exec.CommandContext call. This is
// the one call in this file that runs repository test code directly --
// every TestMain, every init, every test function in the module -- against
// a worktree currently holding a deliberately introduced defect, so it gets
// the same minimal, credential-scrubbed environment every mediated
// subprocess gets rather than this coordinator's own full environment.
func (execution *AgentExecution) suiteRejects(
	ctx context.Context,
	worktree string,
) bool {
	result, err := execution.runMediatedVerificationCommand(
		ctx, worktree, worktree, "mutation-suite",
		[]string{"go", "test", "-count=1", "./..."}, "",
		mutationSuiteTimeout, goToolchainEnvironmentNames)
	return err != nil || !result.Succeeded
}
