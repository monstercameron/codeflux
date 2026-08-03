package coordinator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// findingKind groups what a review found, so the instruction can be ordered by
// what is worth fixing first.
//
// The order is not cosmetic. A swallowed error is a defect and an untried
// empty slice is a gap, and asking for both in one flat list gets the gap
// fixed and the defect left alone, because the gap is the smaller job.
type findingKind string

const (
	// findingDefect is code that is wrong in a way no current test would
	// notice: a swallowed error, a shadowed name, an unchecked assertion.
	findingDefect findingKind = "defects — code that is wrong in a way no test would notice"
	// findingBlindSpot is behaviour nothing checks: a mutation that survived,
	// an input never tried.
	findingBlindSpot findingKind = "blind spots — behaviour nothing checks"
)

// adversarialFinding is one way the work is weaker than it looks.
type adversarialFinding struct {
	Kind findingKind
	// Where names the function or file the finding is about, so the next
	// attempt is told which thing to fix rather than that something is wrong.
	Where string
	What  string
	// Fix says what to do about it, where that is not obvious from the finding.
	Fix string

	// ID, EvidenceLevel, and Lineage are the finding's proof-obligation
	// identity and provenance (PIPE-096, PIPE-097; plan.md §9, §10).
	//
	// None of the three yet outlives one attempt: nothing downstream
	// persists a finding past the run that raised it (plan.md PIPE-098 is
	// unimplemented), because the coordinator code that would carry a
	// finding into the pipeline ledger and evidence bundle,
	// agent_execution.go, is owned by another lane this session and this
	// change does not touch it. What is here is the identity and provenance
	// a persistence layer needs in order to discharge, re-derive, or
	// invalidate a finding across attempts, computed now so that later
	// wiring has something correct to attach to rather than free text with
	// no identity.
	//
	// ID is set by findingObligationID as reviewAdversariallyWithPlan
	// finalizes the list; producer functions below leave it zero.
	ID string
	// EvidenceLevel says how the finding was established: measured (an
	// actual defect the tests were run against and did not catch),
	// mechanical-rule (a fixed, named syntax-tree pattern), or
	// synthesized-case (an input derived from a signature that nothing was
	// observed trying).
	EvidenceLevel findingEvidenceLevel
	// Lineage names which finder raised the finding, which is what a later
	// re-derivation or invalidation (plan.md §31 lineage independence) has
	// to key against when that finder's rule changes.
	Lineage findingLineage
}

// findingEvidenceLevel says how a finding was established (PIPE-097; plan.md
// §10's provenance semantics): the strength of the claim differs by how it
// was produced, and a reader deciding whether to trust it needs to know
// which one produced this without re-deriving it from scratch.
type findingEvidenceLevel string

const (
	// findingEvidenceMeasured is a defect the tests were actually run
	// against and did not catch: not a claim about the code, a demonstrated
	// fact about it (the mutation-survivor finder).
	findingEvidenceMeasured findingEvidenceLevel = "measured"
	// findingEvidenceMechanicalRule is a pattern a fixed, named rule
	// recognised in the syntax tree. Plan.md §31 requires measured
	// false-positive control before a mechanical rule may be promoted;
	// PIPE-105 measures it for the two finders repaired under PIPE-103 and
	// PIPE-104.
	findingEvidenceMechanicalRule findingEvidenceLevel = "mechanical-rule"
	// findingEvidenceSynthesizedCase is an input derived from a produced
	// signature that the suite was never observed to try.
	findingEvidenceSynthesizedCase findingEvidenceLevel = "synthesized-case"
)

// findingLineage names which finder raised a finding (PIPE-097; plan.md §31
// lineage independence): a finding's disposition can be re-derived or
// invalidated when the rule that raised it changes, which is only possible
// when the rule is named rather than left implicit in free text.
type findingLineage string

const (
	findingLineageMutationSurvivor findingLineage = "mutation-survivor"
	findingLineageSwallowedError   findingLineage = "swallowed-error"
	findingLineageBoundary         findingLineage = "boundary-coverage"
	findingLineageAntiPattern      findingLineage = "anti-pattern"
	findingLineageSynthesizedCase  findingLineage = "synthesized-case"
)

// findingCostRank orders findings within one Kind by expected defect cost
// (PIPE-106; plan.md §22 orders work by that cost): a demonstrated defect
// that survived actual test execution outranks a pattern a static rule
// merely recognised, which outranks an input nothing has tried yet. Higher
// ranks are shown first, both in reviewAdversariallyWithPlan's ordering and
// in adversarialInstruction's per-kind cap.
var findingCostRank = map[findingLineage]int{
	findingLineageMutationSurvivor: 4,
	findingLineageSwallowedError:   3,
	findingLineageAntiPattern:      2,
	findingLineageSynthesizedCase:  1,
	findingLineageBoundary:         0,
}

// findingObligationID computes a finding's stable proof-obligation identity
// (PIPE-096; plan.md §9 makes the obligation the unit of assurance).
//
// The digest is over Kind, Where, and What only -- not Fix, EvidenceLevel, or
// Lineage, which describe the finding rather than distinguish it -- so the
// same weakness raised again on a later attempt (the code unchanged, or
// changed in some way that does not alter this specific claim) resolves to
// the same obligation identity rather than minting a new one each time a
// review runs, and two findings differ only when what they actually claim
// differs.
func findingObligationID(finding adversarialFinding) string {
	digest := sha256.Sum256([]byte(
		string(finding.Kind) + "\x00" + finding.Where + "\x00" + finding.What))
	return "ADV-" + hex.EncodeToString(digest[:])[:16]
}

// reviewAdversarially attacks the work rather than waiting for a gate to trip.
//
// The gates ask whether declared conditions hold. This asks the different and
// harder question a reviewer asks: given that everything passed, where is this
// still weak? A suite can be green because the code is right or because the
// tests do not look anywhere interesting, and only one of those is worth
// shipping.
//
// It is deliberately run when the work already compiles and passes. Attacking
// broken code tells you what you already know.
//
// This runs every finder unconditionally, which is the pre-PIPE-095 policy:
// callers that have not been updated to select checks by task risk get
// exactly the coverage they got before. reviewAdversariallyForRisk
// (PIPE-095) is the risk-selecting entry point; it is additive and is not
// yet reached from the pipeline (see its own comment).
func (execution *AgentExecution) reviewAdversarially(
	ctx context.Context,
	worktree string,
) ([]adversarialFinding, error) {
	return execution.reviewAdversariallyWithPlan(
		ctx, worktree, adversarialCheckPlan{MutationAnalysis: true})
}

// reviewAdversariallyWithPlan is reviewAdversarially with which finders run
// selected by an adversarialCheckPlan (PIPE-095), and is where the findings
// this review produces are given their proof-obligation identity (PIPE-096)
// and finalized ordering (PIPE-106).
func (execution *AgentExecution) reviewAdversariallyWithPlan(
	ctx context.Context,
	worktree string,
	plan adversarialCheckPlan,
) ([]adversarialFinding, error) {
	functions, err := readProducedFunctions(worktree)
	if err != nil {
		return nil, err
	}
	var findings []adversarialFinding
	// Hygiene belongs here rather than in a pass of its own. Three separate
	// reviews each cost an attempt and each showed the model a third of the
	// picture, so it fixed things piecemeal and had fewer attempts left to fix
	// anything properly. One review, everything at once, ordered by what
	// actually matters.
	findings = append(findings, findAntiPatternFindings(worktree)...)
	if plan.MutationAnalysis {
		// The one finder here that costs a real whole-suite execution per
		// candidate (up to twelve, per checkMutations' own bound) rather than
		// a syntax-tree read -- gated by task risk under PIPE-095 rather
		// than run unconditionally for every task regardless of what it is.
		findings = append(findings, execution.findUnkilledMutants(ctx, worktree)...)
	}
	findings = append(findings, findUnhandledFailures(functions)...)
	findings = append(findings, findUncheckedBoundaries(worktree, functions)...)
	findings = append(findings, findUntriedCaseFindings(worktree)...)
	for index := range findings {
		findings[index].ID = findingObligationID(findings[index])
	}
	sortAdversarialFindings(findings)
	return findings, nil
}

// sortAdversarialFindings orders findings defects-before-blind-spots, then
// by expected defect cost within a Kind (PIPE-106), then deterministically
// by Where and What. Factored out of reviewAdversariallyWithPlan so a test
// can rank a synthetic finding list the same way a real review ranks its own
// output, rather than keeping a second copy of the comparison that could
// drift from the one actually used.
func sortAdversarialFindings(findings []adversarialFinding) {
	sort.Slice(findings, func(first, second int) bool {
		if findings[first].Kind != findings[second].Kind {
			return findings[first].Kind == findingDefect
		}
		// PIPE-106: within one Kind, rank by expected defect cost rather
		// than alphabetically by Where -- a demonstrated mutation survivor
		// belongs ahead of a hygiene-level anti-pattern finding even though
		// "a" sorts before "m".
		if rankFirst, rankSecond := findingCostRank[findings[first].Lineage],
			findingCostRank[findings[second].Lineage]; rankFirst != rankSecond {
			return rankFirst > rankSecond
		}
		if findings[first].Where != findings[second].Where {
			return findings[first].Where < findings[second].Where
		}
		return findings[first].What < findings[second].What
	})
}

// findUnkilledMutants reports defects the tests did not notice.
//
// This is the sharpest question available: not whether the tests pass, but
// whether they would still pass if the code were wrong. A mutation that
// survives names a specific line whose behaviour nothing checks, which is a
// far more actionable finding than a coverage percentage.
func (execution *AgentExecution) findUnkilledMutants(
	ctx context.Context,
	worktree string,
) []adversarialFinding {
	// This caller has no task-scoped attribution to hand in (it is reached
	// with only a worktree path), so it passes the zero value, which
	// checkMutations treats as "attribution not established" and falls back
	// to its prior whole-file behaviour (PIPE-114) rather than silently
	// mutating nothing.
	outcome := execution.checkMutations(ctx, worktree, changeAttribution{})
	survivors, present := outcome.Evidence["survivors"].([]string)
	if !present || len(survivors) == 0 {
		return nil
	}
	findings := make([]adversarialFinding, 0, len(survivors))
	for _, survivor := range survivors {
		file, why, _ := strings.Cut(survivor, ": ")
		findings = append(findings, adversarialFinding{
			Kind:  findingBlindSpot,
			Where: strings.TrimSpace(file),
			What: "a deliberate defect survived every test — " +
				strings.TrimSpace(why) + " — so nothing checks that behaviour",
			EvidenceLevel: findingEvidenceMeasured,
			Lineage:       findingLineageMutationSurvivor,
		})
	}
	return findings
}

// knownFallibleStdlibCalls names qualified standard-library calls (PIPE-103)
// that hand back a declared error the caller must handle, keyed the same way
// observedEffects (agent_stage_structure.go) names an effect: import path
// plus exported name, e.g. "os.Open" or "net/http.Get".
//
// This is a short, reviewable list rather than a type-checker's answer, for
// the same reason effectPackages and pureConversionTypes beside it are: full
// type information would resolve every call correctly, and this check does
// not load it. The list is deliberately narrow -- constructors, loggers, and
// non-fallible accessors from the same packages are left out on purpose --
// because widening it past what a reviewer can read in one sitting trades a
// bounded false-negative rate (a real fallible call this list does not yet
// name) for an unbounded false-positive one (a call flagged as fallible that
// is not), and PIPE-105's measurement is a false-positive control.
var knownFallibleStdlibCalls = map[string]bool{
	"os.Open": true, "os.OpenFile": true, "os.Create": true,
	"os.ReadFile": true, "os.WriteFile": true, "os.Remove": true,
	"os.RemoveAll": true, "os.Mkdir": true, "os.MkdirAll": true,
	"os.Rename": true, "os.Chdir": true, "os.Stat": true,
	"os.Chmod": true, "os.Symlink": true, "os.Truncate": true,
	"fmt.Fprintln": true, "fmt.Fprintf": true, "fmt.Fprint": true,
	"fmt.Fscanln": true, "fmt.Fscanf": true, "fmt.Sscanf": true,
	"time.Parse": true, "time.ParseDuration": true,
	"net/http.Get": true, "net/http.Post": true, "net/http.PostForm": true,
	"net/http.NewRequest": true,
	"os/exec.LookPath":    true,
}

// findUnhandledFailures reports work that can fail without saying so.
//
// A function returning an error whose caller ignores it turns a failure into
// a wrong answer, which is the failure mode that survives longest in the wild
// because nothing about it looks broken.
//
// PIPE-103 repair: the prior version skipped every impure function outright
// (`!function.Pure`), which excluded exactly the functions most likely to
// reach a real fallible operation before this finder ever asked what the
// call was, and it could only recognise a swallow of another *produced*
// function's error -- a produced function that called a standard-library
// operation able to fail was invisible to it regardless of purity. Both are
// fixed here: every non-scaffolding function is examined, and a call into
// knownFallibleStdlibCalls is treated as fallible on the same terms a
// produced function that ReturnsError already was.
func findUnhandledFailures(functions []producedFunction) []adversarialFinding {
	canFail := map[string]bool{}
	for _, function := range functions {
		if function.ReturnsError {
			canFail[function.Name] = true
		}
	}
	var findings []adversarialFinding
	for _, function := range functions {
		if isTestScaffolding(function) || function.ReturnsError {
			continue
		}
		// main is exempt, because the remedy this finding names is impossible
		// there. Go defines main as func main(), with no results; a main that
		// returned an error would not compile, so "returns no error of its own"
		// is a description of the language rather than of a defect.
		//
		// Ladder rung 3 was told this twice per attempt — that main swallowed
		// fmt.Fprintln's error and run's error — could not comply, was sent
		// back three times for the same finding, escalated to the most
		// expensive rung on the resulting stall, and spent the rest of its
		// budget there. A gate whose instruction cannot be followed does not
		// merely fail to improve the work; it consumes the run.
		//
		// What main genuinely owes is checked by mainReportsAndExits below: an
		// error it receives must reach the user and must set a non-zero exit
		// status. That is the real obligation, and it is satisfiable.
		if function.Name == "main" {
			findings = append(findings,
				mainReportsAndExits(function, canFail)...)
			continue
		}
		// A function that calls another produced function able to fail, and
		// returns no error of its own, has swallowed it.
		for _, called := range function.Calls {
			if canFail[called] {
				findings = append(findings, adversarialFinding{
					Kind:  findingDefect,
					Where: function.Name,
					What: "calls " + called + ", which can fail, but returns no " +
						"error of its own, so the failure disappears here",
					EvidenceLevel: findingEvidenceMechanicalRule,
					Lineage:       findingLineageSwallowedError,
				})
			}
		}
		// A function that calls a known-fallible standard-library operation
		// directly, and returns no error of its own, has swallowed that too.
		for _, effect := range function.Effects {
			if !knownFallibleStdlibCalls[effect] {
				continue
			}
			findings = append(findings, adversarialFinding{
				Kind:  findingDefect,
				Where: function.Name,
				What: "calls " + effect + ", which returns an error, but " +
					function.Name + " returns no error of its own, so the " +
					"failure disappears here",
				EvidenceLevel: findingEvidenceMechanicalRule,
				Lineage:       findingLineageSwallowedError,
			})
		}
	}
	return findings
}

// findUncheckedBoundaries reports the inputs a test never reaches for.
//
// Tests written from a working example examine the middle of the input space.
// The interesting behaviour is at its edges, and a suite that never mentions
// an empty collection, a zero, or a negative number has not been there.
// boundaryEdge is one input worth asking whether the tests ever tried.
type boundaryEdge struct {
	// token is what the suite would have to mention to have tried it.
	token string
	what  string
	// because names the parameter that makes the question apply, so a finding
	// can be checked rather than taken on faith.
	because string
}

// edgesWorthChecking derives the boundaries from the produced signatures.
//
// The point of asking about an edge is that some function could be handed it.
// A program that takes no strings cannot be handed the empty string, and
// reporting that gap taught nothing about the program and buried the findings
// that did.
func edgesWorthChecking(functions []producedFunction) []boundaryEdge {
	// One edge per kind, attributed to the first parameter that raises it, so
	// the same question is not asked once per function.
	seen := map[string]bool{}
	var edges []boundaryEdge
	add := func(token, what, because string) {
		if seen[token] {
			return
		}
		seen[token] = true
		edges = append(edges, boundaryEdge{token, what, because})
	}
	for _, function := range functions {
		if isTestScaffolding(function) {
			continue
		}
		for _, parameter := range function.Parameters {
			naming := fmt.Sprintf("%s takes %s", function.Name, parameter)
			base := strings.TrimPrefix(parameter, "...")
			switch {
			case base == "string":
				add(`""`, "the empty string", naming)
			case strings.HasPrefix(base, "[]"), strings.HasPrefix(base, "map["):
				add(`nil`, "a nil value", naming)
				add(`{}`, "an empty collection", naming)
			case strings.HasPrefix(base, "*"), base == "error", base == "any":
				add(`nil`, "a nil value", naming)
			case isSignedInteger(base):
				add(`-1`, "a negative number", naming)
				add(`0`, "zero", naming)
			case isFloat(base):
				add(`0`, "zero", naming)
			}
		}
	}
	return edges
}

// isSignedInteger reports whether a type can hold a value below zero, which is
// the only reason to ask whether a negative one was tried.
func isSignedInteger(typeName string) bool {
	switch typeName {
	case "int", "int8", "int16", "int32", "int64", "rune":
		return true
	}
	return false
}

// isFloat reports whether a type carries a fractional value.
func isFloat(typeName string) bool {
	return typeName == "float32" || typeName == "float64"
}

// canonicalLiteralToken normalizes one syntax-tree node into the same token
// vocabulary edgesWorthChecking emits (`""`, `nil`, `{}`, `-1`, `0`), or the
// empty string when the node is not one of those shapes (PIPE-104).
//
// A negative integer is not itself a BasicLit in Go's grammar -- the minus
// sign is a separate unary operator applied to a positive literal -- so `-1`
// is recognised as *ast.UnaryExpr{Op: token.SUB} wrapping the literal `1`,
// not as a literal whose text happens to read "-1".
func canonicalLiteralToken(node ast.Node) string {
	switch typed := node.(type) {
	case *ast.BasicLit:
		switch typed.Kind {
		case token.STRING:
			if unquoted, err := strconv.Unquote(typed.Value); err == nil && unquoted == "" {
				return `""`
			}
		case token.INT, token.FLOAT:
			if typed.Value == "0" {
				return "0"
			}
		}
	case *ast.Ident:
		if typed.Name == "nil" {
			return "nil"
		}
	case *ast.UnaryExpr:
		if typed.Op == token.SUB {
			if literal, isLiteral := typed.X.(*ast.BasicLit); isLiteral && literal.Value == "1" {
				return "-1"
			}
		}
	case *ast.CompositeLit:
		if len(typed.Elts) == 0 {
			return "{}"
		}
	}
	return ""
}

// literalTokenPresent reports whether a parsed test file's syntax tree
// contains a literal expression matching the given canonical token (PIPE-104).
//
// The prior version matched the token as a substring of the concatenated
// test source, so the token "0" matched a file mode (0o644), a line number
// inside a comment, or the digit inside an unrelated identifier like
// "TestOrder0" -- none of which mean the empty-int edge was ever tried.
// Reading literal expressions from the syntax tree instead means only an
// actual literal of that shape counts.
func literalTokenPresent(tree *ast.File, wantedToken string) bool {
	found := false
	ast.Inspect(tree, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		if canonicalLiteralToken(node) == wantedToken {
			found = true
			return false
		}
		return true
	})
	return found
}

// namesAnError reports whether an expression is an identifier or selector
// whose own name suggests it holds an error, for use only as one operand of
// a nil comparison (testFileAssertsOnError) -- it is not a general "mentions
// the word error" test.
func namesAnError(expr ast.Expr) bool {
	var name string
	switch typed := expr.(type) {
	case *ast.Ident:
		name = typed.Name
	case *ast.SelectorExpr:
		name = typed.Sel.Name
	default:
		return false
	}
	return strings.Contains(strings.ToLower(name), "err")
}

// isNilIdent reports whether an expression is the literal identifier nil.
func isNilIdent(expr ast.Expr) bool {
	ident, isIdent := expr.(*ast.Ident)
	return isIdent && ident.Name == "nil"
}

// testFileAssertsOnError reports whether a parsed test file compares
// something that names an error against nil, or calls .Error() on a value
// (PIPE-104).
//
// The prior version searched for the literal text "Error" anywhere in the
// concatenated test source, which is satisfied by t.Errorf's own method
// name -- present in nearly every table-driven test regardless of whether
// any error was ever inspected -- so the finding this guards was
// unreachable in practice. Matching only an exact `.Error()` call, or a nil
// comparison against an identifier/selector whose own name contains "err",
// requires the test to have actually named or inspected an error value.
func testFileAssertsOnError(tree *ast.File) bool {
	asserts := false
	ast.Inspect(tree, func(node ast.Node) bool {
		if asserts || node == nil {
			return false
		}
		switch typed := node.(type) {
		case *ast.BinaryExpr:
			if typed.Op != token.NEQ && typed.Op != token.EQL {
				return true
			}
			if (namesAnError(typed.X) && isNilIdent(typed.Y)) ||
				(namesAnError(typed.Y) && isNilIdent(typed.X)) {
				asserts = true
				return false
			}
		case *ast.CallExpr:
			if selector, isSelector := typed.Fun.(*ast.SelectorExpr); isSelector &&
				selector.Sel.Name == "Error" {
				asserts = true
				return false
			}
		}
		return true
	})
	return asserts
}

// findUncheckedBoundaries reports the inputs a test never reaches for.
//
// PIPE-104 repair: the prior version searched for a boundary's token as a
// raw substring of the test files' concatenated source. This parses each
// test file and asks the same two questions of its syntax tree instead, so
// an incidental appearance of the same text -- a file mode, a line number, a
// method name -- can no longer stand in for the edge actually being tried.
func findUncheckedBoundaries(
	worktree string,
	functions []producedFunction,
) []adversarialFinding {
	files, err := producedGoFiles(worktree)
	if err != nil {
		return nil
	}
	fileSet := token.NewFileSet()
	var trees []*ast.File
	for _, file := range files {
		if !strings.HasSuffix(file, "_test.go") {
			continue
		}
		tree, parseErr := parser.ParseFile(
			fileSet, filepath.Join(worktree, file), nil, 0)
		if parseErr != nil {
			// A test file this package cannot itself parse is not this
			// finder's failure to report; assembly and verification already
			// gate on the code compiling at all.
			continue
		}
		trees = append(trees, tree)
	}
	if len(trees) == 0 {
		return nil
	}

	var findings []adversarialFinding
	// Which edges are worth asking about comes from what the produced
	// functions actually accept, not from a fixed list. A fixed list said
	// "never mentions the empty string" about a program with no string
	// parameter anywhere — a finding that was true, unactionable, and had
	// nothing to do with the code under review.
	for _, edge := range edgesWorthChecking(functions) {
		tried := false
		for _, tree := range trees {
			if literalTokenPresent(tree, edge.token) {
				tried = true
				break
			}
		}
		if tried {
			continue
		}
		findings = append(findings, adversarialFinding{
			Kind:  findingBlindSpot,
			Where: "the test suite",
			What: "never mentions " + edge.what + ", though " + edge.because +
				", so the behaviour at that edge is unexamined",
			EvidenceLevel: findingEvidenceMechanicalRule,
			Lineage:       findingLineageBoundary,
		})
	}
	// A suite with no failing-path assertion has only ever watched things work.
	assertsOnError := false
	for _, tree := range trees {
		if testFileAssertsOnError(tree) {
			assertsOnError = true
			break
		}
	}
	if !assertsOnError && anyReturnsError(functions) {
		findings = append(findings, adversarialFinding{
			Kind:  findingBlindSpot,
			Where: "the test suite",
			What: "never asserts on a returned error, though the code can fail, " +
				"so no test has seen it fail",
			EvidenceLevel: findingEvidenceMechanicalRule,
			Lineage:       findingLineageBoundary,
		})
	}
	return findings
}

// anyReturnsError reports whether the produced code can fail at all.
func anyReturnsError(functions []producedFunction) bool {
	for _, function := range functions {
		if !isTestScaffolding(function) && function.ReturnsError {
			return true
		}
	}
	return false
}

// maximumFindingsPerKindInInstruction bounds how many findings of one Kind
// reach the next attempt's instruction (PIPE-106).
//
// Every finding reviewAdversariallyWithPlan returns is still available to
// its caller in full -- this bound applies only to what adversarialInstruction
// renders. An instruction listing every finding competes with the run's own
// code context for the loop's byte limits, and findings is sorted by
// findingCostRank before this function ever sees it, so the entries cut off
// here are, by that ranking, the least costly ones in their Kind.
const maximumFindingsPerKindInInstruction = 8

// adversarialInstruction is what the next attempt is told to do about the
// findings.
//
// Grouped by kind, defects first, because a flat list gets the easy items
// fixed and the important ones left: an untried empty slice is a smaller job
// than a swallowed error, and an agent working a list in order does the small
// one and runs out of attempts. Within a kind, findings arrive pre-ranked by
// expected defect cost (PIPE-106); this function preserves that order rather
// than re-sorting.
func adversarialInstruction(findings []adversarialFinding, testsPassed bool) string {
	var report strings.Builder
	report.WriteString(
		validationPreamble(testsPassed) + "a review found it is still " +
			"weaker than it looks:\n")

	grouped := map[findingKind][]adversarialFinding{}
	for _, finding := range findings {
		grouped[finding.Kind] = append(grouped[finding.Kind], finding)
	}
	// A review that found only blind spots is asking for tests, not for edits.
	//
	// A blind spot is behaviour nothing checks. That is not the same as
	// behaviour that is wrong, and the program is usually already correct about
	// it: a parser asked for an empty-input test very often rejects empty input
	// perfectly well and has simply never been asked. Ladder rung 5 was given
	// two blind spots — empty input and malformed JSON — rewrote its production
	// code to address them, changed its output in the process, and spent four
	// attempts getting back to a program it already had.
	//
	// So the order is stated. Write the test, run it, and change the program
	// only if the test actually fails. A test that passes immediately has
	// closed the blind spot, which is what was asked for.
	if len(grouped[findingDefect]) == 0 && len(grouped[findingBlindSpot]) > 0 {
		report.WriteString(
			"\nNothing here is a defect. Every item is behaviour nothing " +
				"checks, which is not the same as behaviour that is wrong.\n\n" +
				"Add the missing test first and run it. If it passes, the gap " +
				"is closed and there is nothing else to do — the program was " +
				"already right about that input and had simply never been " +
				"asked. Change the program only for a test that actually " +
				"fails, and then change as little as possible.\n\n" +
				"Do not rewrite working code to make room for a test.\n")
	}
	for _, kind := range []findingKind{findingDefect, findingBlindSpot} {
		items := grouped[kind]
		if len(items) == 0 {
			continue
		}
		fmt.Fprintf(&report, "\n%s\n", kind)
		shown, omitted := items, 0
		if len(items) > maximumFindingsPerKindInInstruction {
			shown = items[:maximumFindingsPerKindInInstruction]
			omitted = len(items) - maximumFindingsPerKindInInstruction
		}
		for _, finding := range shown {
			fmt.Fprintf(&report, "\n- %s: %s", finding.Where, finding.What)
			if finding.Fix != "" {
				fmt.Fprintf(&report, "\n  %s", finding.Fix)
			}
		}
		if omitted > 0 {
			fmt.Fprintf(&report,
				"\n\n(%d more %s not shown here, ranked below these by expected "+
					"defect cost; fix these first, then rerun to see the rest.)",
				omitted, kind)
		}
	}
	report.WriteString(
		"\n\nFix the defects first, then close the blind spots. Do not weaken " +
			"an assertion to make something pass, and do not change behaviour " +
			"the request asked for.")
	return report.String()
}

// findAntiPatternFindings brings hygiene into the review.
func findAntiPatternFindings(worktree string) []adversarialFinding {
	patterns, err := findAntiPatterns(worktree)
	if err != nil {
		return nil
	}
	findings := make([]adversarialFinding, 0, len(patterns))
	for _, pattern := range patterns {
		findings = append(findings, adversarialFinding{
			Kind: findingDefect, Where: pattern.Where,
			What: pattern.What, Fix: pattern.Why,
			EvidenceLevel: findingEvidenceMechanicalRule,
			Lineage:       findingLineageAntiPattern,
		})
	}
	return findings
}

// findUntriedCaseFindings brings the synthesised inputs into the review.
func findUntriedCaseFindings(worktree string) []adversarialFinding {
	owed, err := untriedCases(worktree)
	if err != nil {
		return nil
	}
	var findings []adversarialFinding
	for name, cases := range owed {
		for _, candidate := range cases {
			findings = append(findings, adversarialFinding{
				Kind: findingBlindSpot, Where: name,
				What: fmt.Sprintf("is never given %s (%s)",
					candidate.Shape, candidate.Why),
				Fix:           string(candidate.Class) + " — " + candidate.Class.assertion(),
				EvidenceLevel: findingEvidenceSynthesizedCase,
				Lineage:       findingLineageSynthesizedCase,
			})
		}
	}
	return findings
}

// mainReportsAndExits is what a Go entry point actually owes.
//
// It cannot return an error, so the question is not whether it returns one but
// whether the error reaches anybody. A main that calls a fallible function and
// neither prints the failure nor exits non-zero has produced a program that
// fails silently and tells its caller it succeeded, which is a real defect and
// a satisfiable one: report it and call os.Exit with a non-zero status.
//
// Only asked when main actually calls something that can fail. A main that
// calls nothing fallible owes nothing here, and saying otherwise would be the
// same unfollowable instruction in a different sentence.
func mainReportsAndExits(
	main producedFunction, canFail map[string]bool,
) []adversarialFinding {
	fallible := false
	for _, called := range main.Calls {
		if canFail[called] {
			fallible = true
			break
		}
	}
	for _, effect := range main.Effects {
		if knownFallibleStdlibCalls[effect] {
			fallible = true
			break
		}
	}
	if !fallible {
		return nil
	}
	exits := false
	for _, effect := range main.Effects {
		if effect == "os.Exit" || effect == "log.Fatal" ||
			effect == "log.Fatalf" || effect == "log.Fatalln" {
			exits = true
			break
		}
	}
	if exits {
		return nil
	}
	return []adversarialFinding{{
		Kind:  findingDefect,
		Where: main.Name,
		What: "calls something that can fail and then exits zero whatever " +
			"happens, so a failed program reports success to whatever ran " +
			"it. main cannot return an error: write the message to " +
			"os.Stderr and call os.Exit(1)",
		EvidenceLevel: findingEvidenceMechanicalRule,
		Lineage:       findingLineageSwallowedError,
	}}
}

// findingFingerprint is what a finding is about, with the names taken out.
//
// "main calls run, which can fail" and "main calls runCommand, which can fail"
// are one criticism, and counting them as two lets a rename reset the
// repetition count that decides whether a run is stalling. Rung 3 was sent back
// three times for what a reader would call the same objection.
//
// The lineage and the kind are kept because they are what the finding is; the
// function names inside the prose are removed because they are what it happens
// to be about this time.
func findingFingerprint(finding adversarialFinding) string {
	words := strings.Fields(finding.What)
	kept := make([]string, 0, len(words))
	skipNext := false
	for _, word := range words {
		trimmed := strings.Trim(word, ".,;:\"'()")
		if trimmed == "" {
			continue
		}
		// The word after "calls" is the callee, and the callee is exactly what
		// differs between two statements of one criticism. It cannot be caught
		// by looking at the word itself: "run" is a name here and a common verb
		// everywhere else, and no amount of shape analysis tells them apart.
		// Its position does.
		if skipNext {
			skipNext = false
			continue
		}
		if strings.EqualFold(trimmed, "calls") {
			skipNext = true
			continue
		}
		// Everything else that is plainly a name rather than prose: a dotted
		// selector, an inner capital, an underscore.
		if looksLikeIdentifier(trimmed) {
			continue
		}
		kept = append(kept, strings.ToLower(trimmed))
	}
	return string(finding.Lineage) + "|" + finding.Where + "|" +
		strings.Join(kept, " ")
}

// looksLikeIdentifier reports whether a word is a name rather than prose.
//
// Deliberately crude: a dotted selector, an inner capital, or an underscore.
// Missing one leaves two statements of a criticism looking different, which is
// the behaviour this replaces rather than a new failure.
func looksLikeIdentifier(word string) bool {
	if strings.Contains(word, ".") || strings.Contains(word, "_") {
		return true
	}
	for index := 1; index < len(word); index++ {
		if word[index] >= 'A' && word[index] <= 'Z' {
			return true
		}
	}
	return false
}

// advisoryFindings are the ones a run may finish without fixing.
//
// A demonstrated defect the tests were run against is not advisory: it is a
// measured fact that the suite misses something. Everything else is a rule's
// opinion, and after one honest attempt at repair a run that builds, passes its
// tests and matches its acceptance examples should finish and carry the opinion
// as a note rather than spend its remaining budget on it.
//
// This is the completion floor and the refinement ceiling kept apart. Crossing
// the floor makes a run recoverably successful; failing to reach the ceiling
// makes it successful with advisories.
func advisoryFindings(findings []adversarialFinding) []adversarialFinding {
	var advisory []adversarialFinding
	for _, finding := range findings {
		if finding.EvidenceLevel == findingEvidenceMeasured {
			continue
		}
		advisory = append(advisory, finding)
	}
	return advisory
}

// blockingFindings are the ones worth another attempt.
func blockingFindings(findings []adversarialFinding) []adversarialFinding {
	var blocking []adversarialFinding
	for _, finding := range findings {
		if finding.EvidenceLevel == findingEvidenceMeasured {
			blocking = append(blocking, finding)
		}
	}
	return blocking
}
