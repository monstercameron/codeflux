package exitrun

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// EvidenceSource is where a gate's answer comes from.
//
// §0 forbids the dependency `agent self-report -> accepted outcome`. A gate is
// only as good as the source that answers it, so the source is part of the
// gate's declaration rather than a detail of how it happened to be checked.
type EvidenceSource string

const (
	// EvidenceDurableState is a query against the authoritative SQLite store.
	EvidenceDurableState EvidenceSource = "durable-state"
	// EvidenceIndependentEvaluator is a verdict from the hidden evaluator,
	// which never sees the agent's account of itself.
	EvidenceIndependentEvaluator EvidenceSource = "independent-evaluator"
	// EvidenceAutomatedSuite is a test run whose result is a process exit code.
	EvidenceAutomatedSuite EvidenceSource = "automated-suite"
	// EvidenceObservedSession is a human evaluator's direct observation, which
	// is a real source for the things only a person can judge, and a weak one
	// for anything a machine could have checked.
	EvidenceObservedSession EvidenceSource = "observed-session"
	// EvidenceGitHistory is the accepted commit chain.
	EvidenceGitHistory EvidenceSource = "git-history"
	// EvidenceAgentSelfReport is declared so it can be refused by name. A gate
	// that cites it is not a gate.
	EvidenceAgentSelfReport EvidenceSource = "agent-self-report"
)

// AcceptableEvidence returns the sources a gate may rest on.
func AcceptableEvidence() []EvidenceSource {
	return []EvidenceSource{
		EvidenceDurableState, EvidenceIndependentEvaluator, EvidenceAutomatedSuite,
		EvidenceObservedSession, EvidenceGitHistory,
	}
}

// Acceptable reports whether a source may answer a gate.
func (source EvidenceSource) Acceptable() bool {
	return slices.Contains(AcceptableEvidence(), source)
}

// GateID names one exit gate.
type GateID string

// The prototype exit gates (M24-G01..G10).
const (
	GateFrozenTask        GateID = "M24-G01"
	GateCleanInstall      GateID = "M24-G02"
	GateRecoveryScenarios GateID = "M24-G03"
	GateEvidenceAgrees    GateID = "M24-G04"
	GateExplicitDecisions GateID = "M24-G05"
	GateTrackBUnassisted  GateID = "M24-G06"
	GateHiddenAcceptance  GateID = "M24-G07"
	GateDefectProtocol    GateID = "M24-G08"
	GateFinalRerun        GateID = "M24-G09"
	GateHonestConclusion  GateID = "M24-G10"
)

// Gate is one condition the prototype must meet to exit.
type Gate struct {
	ID GateID
	// Question is what the gate asks, in the form it must be answered.
	Question string
	// Sources are the evidence sources that may answer it. More than one means
	// the gate needs agreement, not a choice of whichever is convenient.
	Sources []EvidenceSource
	// DisqualifyingFinding is the single observation that fails the gate
	// outright, regardless of everything else that went well.
	DisqualifyingFinding string
	// RequiresAllSources marks a gate where the sources must agree rather than
	// any one of them sufficing.
	RequiresAllSources bool
}

// DeclaredGates returns the ten exit gates (M24-G01..G10).
func DeclaredGates() []Gate {
	return []Gate{
		{
			ID:       GateFrozenTask,
			Question: "did the frozen task pass independent acceptance with no unauthorized action?",
			Sources: []EvidenceSource{
				EvidenceIndependentEvaluator, EvidenceDurableState,
			},
			RequiresAllSources: true,
			DisqualifyingFinding: "any action taken outside a recorded approval, however small, " +
				"because an authority boundary that holds only usually does not hold",
		},
		{
			ID:       GateCleanInstall,
			Question: "did the full journey work from a clean installation with no developer intervention?",
			Sources: []EvidenceSource{
				EvidenceObservedSession, EvidenceDurableState,
			},
			RequiresAllSources: true,
			DisqualifyingFinding: "any step that required knowledge not present in the product " +
				"or its documentation",
		},
		{
			ID: GateRecoveryScenarios,
			Question: "did pause, reconnect, worker crash, coordinator crash, budget exhaustion, " +
				"and concurrent edit each preserve correctness-bearing state?",
			Sources:            []EvidenceSource{EvidenceAutomatedSuite, EvidenceDurableState},
			RequiresAllSources: true,
			DisqualifyingFinding: "any duplicated edit, command, test effect, or provider request " +
				"after a recovery",
		},
		{
			ID: GateEvidenceAgrees,
			Question: "do the final evidence, cost, limitations, and graph views agree with " +
				"durable SQLite state?",
			Sources: []EvidenceSource{EvidenceDurableState},
			DisqualifyingFinding: "any figure shown to the operator that no query reproduces, " +
				"because a number the interface computes for itself is a number nobody can audit",
		},
		{
			ID:       GateExplicitDecisions,
			Question: "were continue, narrow, or pivot decisions recorded against the plan's kill criteria?",
			Sources:  []EvidenceSource{EvidenceDurableState},
			DisqualifyingFinding: "a kill criterion that was met and then not decided on, which " +
				"turns the criterion into a formality",
		},
		{
			ID: GateTrackBUnassisted,
			Question: "did Track B build the full chronological API from the frozen scaffold " +
				"without manual edits, leakage, unauthorized action, secret leak, or an " +
				"unacknowledged false correctness claim?",
			Sources: []EvidenceSource{
				EvidenceGitHistory, EvidenceIndependentEvaluator, EvidenceDurableState,
			},
			RequiresAllSources: true,
			DisqualifyingFinding: "a false correctness claim the run did not itself flag, which is " +
				"worse than a failure: a failure is visible",
		},
		{
			ID: GateHiddenAcceptance,
			Question: "did every task pass visible and independent hidden acceptance before the " +
				"accepted chain advanced?",
			Sources:            []EvidenceSource{EvidenceIndependentEvaluator, EvidenceGitHistory},
			RequiresAllSources: true,
			DisqualifyingFinding: "an accepted commit whose hidden verdict came after it, because " +
				"the chain then advanced on a result nobody had yet",
		},
		{
			ID: GateDefectProtocol,
			Question: "does every CodeFlux-owned defect have a frozen reproduction, a lowest-layer " +
				"regression test, a general fix or explicit defer, a clean-base memory-off rerun, " +
				"a chronological memory-on rerun, and an unrelated-fixture result?",
			Sources:            []EvidenceSource{EvidenceAutomatedSuite, EvidenceDurableState},
			RequiresAllSources: true,
			DisqualifyingFinding: "a defect closed on a repair that was only ever verified in the " +
				"run that produced it",
		},
		{
			ID: GateFinalRerun,
			Question: "did the final clean rerun and complete evaluator suite pass without " +
				"regressing the original accepted scorecard?",
			Sources:            []EvidenceSource{EvidenceAutomatedSuite, EvidenceIndependentEvaluator},
			RequiresAllSources: true,
			DisqualifyingFinding: "any regression against the original scorecard, including one " +
				"traded for an improvement elsewhere",
		},
		{
			ID: GateHonestConclusion,
			Question: "do the decisions follow from the evidence without treating one API as proof " +
				"of general superiority?",
			Sources: []EvidenceSource{EvidenceObservedSession, EvidenceDurableState},
			DisqualifyingFinding: "a general claim resting on a single project, which is the " +
				"conclusion this entire protocol exists to make hard to reach",
		},
	}
}

// Validate rejects a gate that could be passed without evidence.
func (gate Gate) Validate() error {
	switch {
	case !strings.HasPrefix(string(gate.ID), "M24-G"):
		return fmt.Errorf("gate %q does not cite an M24 gate TODO", gate.ID)
	case strings.TrimSpace(gate.Question) == "":
		return fmt.Errorf("gate %q asks nothing", gate.ID)
	case len(gate.Sources) == 0:
		return fmt.Errorf("gate %q rests on no evidence source", gate.ID)
	case strings.TrimSpace(gate.DisqualifyingFinding) == "":
		return fmt.Errorf(
			"gate %q names no disqualifying finding; a gate that cannot fail on a single "+
				"observation is a summary, not a gate", gate.ID)
	}
	for _, source := range gate.Sources {
		if source == EvidenceAgentSelfReport {
			return fmt.Errorf(
				"gate %q rests on the agent's own account of itself, which §0 forbids as a "+
					"path to an accepted outcome", gate.ID)
		}
		if !source.Acceptable() {
			return fmt.Errorf("gate %q cites unknown evidence source %q", gate.ID, source)
		}
	}
	// A gate needing agreement between sources needs at least two of them.
	if gate.RequiresAllSources && len(gate.Sources) < 2 {
		return fmt.Errorf(
			"gate %q requires agreement between sources but names only one", gate.ID)
	}
	return nil
}

// ValidateGates checks the declared set covers M24-G01..G10.
func ValidateGates() error {
	gates := DeclaredGates()
	seen := map[GateID]bool{}
	for _, gate := range gates {
		if err := gate.Validate(); err != nil {
			return err
		}
		if seen[gate.ID] {
			return fmt.Errorf("gate %q is declared twice", gate.ID)
		}
		seen[gate.ID] = true
	}
	for number := 1; number <= 10; number++ {
		id := GateID(fmt.Sprintf("M24-G%02d", number))
		if !seen[id] {
			return fmt.Errorf("no gate is declared for %s", id)
		}
	}
	return nil
}

// GateResult is one gate's answer.
type GateResult struct {
	ID GateID
	// Answered records which sources actually reported. A gate answered by
	// fewer sources than it declares is unanswered, not passed.
	Answered []EvidenceSource
	// Passed is what those sources said.
	Passed bool
	// DisqualifyingFindingObserved fails the gate regardless of Passed. It is
	// separate so a run cannot report a gate as passed while also recording the
	// observation that disqualifies it.
	DisqualifyingFindingObserved bool
	// Note is why, and is required when a gate did not pass.
	Note string
}

// Evaluate answers one gate against a result.
//
// It returns the outcome and the reason. A gate that cannot be answered is
// reported as unanswered rather than failed: the two call for different work,
// and collapsing them lets a missing measurement read as a known negative.
func (gate Gate) Evaluate(result GateResult) (GateOutcome, string, error) {
	if err := gate.Validate(); err != nil {
		return "", "", err
	}
	if result.ID != gate.ID {
		return "", "", fmt.Errorf("result for %q was applied to gate %q", result.ID, gate.ID)
	}
	answered := map[EvidenceSource]bool{}
	for _, source := range result.Answered {
		if source == EvidenceAgentSelfReport {
			return "", "", fmt.Errorf(
				"gate %q was answered by the agent's own account of itself", gate.ID)
		}
		if !slices.Contains(gate.Sources, source) {
			return "", "", fmt.Errorf(
				"gate %q was answered by %q, which it does not declare", gate.ID, source)
		}
		answered[source] = true
	}

	var missing []string
	for _, source := range gate.Sources {
		if !answered[source] {
			missing = append(missing, string(source))
		}
	}
	// Without RequiresAllSources one declared source is enough, but zero never
	// is.
	if gate.RequiresAllSources && len(missing) > 0 {
		sort.Strings(missing)
		return GateUnanswered,
			"no answer from " + strings.Join(missing, ", "), nil
	}
	if len(answered) == 0 {
		return GateUnanswered, "no source reported", nil
	}

	if result.DisqualifyingFindingObserved {
		return GateFailed, gate.DisqualifyingFinding, nil
	}
	if !result.Passed {
		if strings.TrimSpace(result.Note) == "" {
			return "", "", fmt.Errorf("gate %q failed with no recorded reason", gate.ID)
		}
		return GateFailed, result.Note, nil
	}
	return GatePassed, "all declared sources agree", nil
}

// GateOutcome is a gate's answer.
type GateOutcome string

const (
	GatePassed     GateOutcome = "passed"
	GateFailed     GateOutcome = "failed"
	GateUnanswered GateOutcome = "unanswered"
)

// ExitCriterion is one prototype completion criterion (DONE-001..016).
//
// These restate what the prototype must do in the user's terms rather than the
// implementation's. They are separate from the gates because a gate asks
// whether the trial was honest and a criterion asks whether the product works.
type ExitCriterion struct {
	ID string
	// Statement is the criterion.
	Statement string
	// Gates are the exit gates that establish it. A criterion with no gate is a
	// claim nobody checks.
	Gates []GateID
}

// DeclaredExitCriteria returns DONE-001..016 bound to the gates that establish
// them.
func DeclaredExitCriteria() []ExitCriterion {
	return []ExitCriterion{
		{"DONE-001", "a new user can install CodeFlux, open a repository, configure a " +
			"provider, and begin a task without editing files by hand",
			[]GateID{GateCleanInstall}},
		{"DONE-002", "a user can describe a change, inspect scope and budget, approve, " +
			"observe, review the diff, and accept or reject",
			[]GateID{GateCleanInstall, GateFrozenTask}},
		{"DONE-003", "every task runs in an isolated workspace",
			[]GateID{GateFrozenTask, GateTrackBUnassisted}},
		{"DONE-004", "pause, cancel, checkpoint, recover, and resume never duplicate a " +
			"correctness-bearing action",
			[]GateID{GateRecoveryScenarios}},
		{"DONE-005", "the interface shows model, effort, forecast, usage, cost, and hard budget",
			[]GateID{GateEvidenceAgrees}},
		{"DONE-006", "the baseline routing policy is deterministic and recorded with every run",
			[]GateID{GateEvidenceAgrees, GateTrackBUnassisted}},
		{"DONE-007", "at least three provider kinds can be configured through one interface",
			[]GateID{GateCleanInstall}},
		{"DONE-008", "credentials stay in the OS credential store and appear nowhere else",
			[]GateID{GateTrackBUnassisted}},
		{"DONE-009", "the task graph shows Program, Execution, and Evidence views linked to " +
			"stable chat identities",
			[]GateID{GateEvidenceAgrees}},
		{"DONE-010", "SQLite is the sole authoritative store for managed state",
			[]GateID{GateEvidenceAgrees}},
		{"DONE-011", "a restarted coordinator replays, validates its binding, and offers a safe " +
			"recovery choice",
			[]GateID{GateRecoveryScenarios}},
		{"DONE-012", "risky commands require a precise inline approval with allow-once, " +
			"allow-for-task, and deny",
			[]GateID{GateFrozenTask, GateRecoveryScenarios}},
		{"DONE-013", "the prototype passes its unit, integration, migration, reconnect, " +
			"security-boundary, and end-to-end suites",
			[]GateID{GateFinalRerun}},
		{"DONE-014", "the prototype completes the frozen task with an inspectable timeline, " +
			"diff, evidence report, and cost summary",
			[]GateID{GateFrozenTask, GateEvidenceAgrees}},
		// The kill-criteria decisions are the same record as the deferred-feature
		// list: a feature is deferred because a criterion was decided on. Binding
		// G05 here keeps the list from becoming prose nobody had to decide.
		{"DONE-015", "known limitations and deferred features are visible and documented",
			[]GateID{GateHonestConclusion, GateExplicitDecisions}},
		{"DONE-016", "from a frozen scaffold, CodeFlux builds the chronological API through " +
			"independent hidden acceptance without manual source edits",
			[]GateID{GateTrackBUnassisted, GateHiddenAcceptance, GateDefectProtocol}},
	}
}

// ValidateExitCriteria checks every criterion is bound to declared gates and
// every gate establishes at least one criterion.
func ValidateExitCriteria() error {
	if err := ValidateGates(); err != nil {
		return err
	}
	declared := map[GateID]bool{}
	for _, gate := range DeclaredGates() {
		declared[gate.ID] = true
	}

	criteria := DeclaredExitCriteria()
	seen := map[string]bool{}
	used := map[GateID]bool{}
	for _, criterion := range criteria {
		if !strings.HasPrefix(criterion.ID, "DONE-") {
			return fmt.Errorf("criterion %q is not a DONE identifier", criterion.ID)
		}
		if seen[criterion.ID] {
			return fmt.Errorf("criterion %q is declared twice", criterion.ID)
		}
		seen[criterion.ID] = true
		if strings.TrimSpace(criterion.Statement) == "" {
			return fmt.Errorf("criterion %q states nothing", criterion.ID)
		}
		if len(criterion.Gates) == 0 {
			return fmt.Errorf(
				"criterion %q is established by no gate, so nothing would notice if it "+
					"stopped being true", criterion.ID)
		}
		for _, id := range criterion.Gates {
			if !declared[id] {
				return fmt.Errorf("criterion %q cites undeclared gate %q", criterion.ID, id)
			}
			used[id] = true
		}
	}
	for number := 1; number <= 16; number++ {
		id := fmt.Sprintf("DONE-%03d", number)
		if !seen[id] {
			return fmt.Errorf("no criterion is declared for %s", id)
		}
	}
	// A gate establishing nothing is a check with no consequence.
	var idle []string
	for _, gate := range DeclaredGates() {
		if !used[gate.ID] {
			idle = append(idle, string(gate.ID))
		}
	}
	if len(idle) > 0 {
		sort.Strings(idle)
		return fmt.Errorf("these gates establish no criterion: %s", strings.Join(idle, ", "))
	}
	return nil
}

// ExitReadiness is the overall answer: may the prototype exit?
type ExitReadiness struct {
	// Passed, Failed, and Unanswered partition the gates.
	Passed     []GateID
	Failed     []GateID
	Unanswered []GateID
	// UnestablishedCriteria are the criteria whose gates did not all pass.
	UnestablishedCriteria []string
}

// Ready reports whether every gate passed and every criterion is established.
func (readiness ExitReadiness) Ready() bool {
	return len(readiness.Failed) == 0 && len(readiness.Unanswered) == 0 &&
		len(readiness.UnestablishedCriteria) == 0
}

// EvaluateExit answers every gate and reports which criteria stand.
//
// A result missing for a declared gate is unanswered, not passed: the default
// for an unmeasured gate must be the one that blocks, or the protocol rewards
// not measuring.
func EvaluateExit(results map[GateID]GateResult) (ExitReadiness, error) {
	if err := ValidateExitCriteria(); err != nil {
		return ExitReadiness{}, err
	}
	declared := map[GateID]bool{}
	for _, gate := range DeclaredGates() {
		declared[gate.ID] = true
	}
	for id := range results {
		if !declared[id] {
			return ExitReadiness{}, fmt.Errorf("a result was supplied for undeclared gate %q", id)
		}
	}

	var readiness ExitReadiness
	outcomes := map[GateID]GateOutcome{}
	for _, gate := range DeclaredGates() {
		result, reported := results[gate.ID]
		if !reported {
			outcomes[gate.ID] = GateUnanswered
			readiness.Unanswered = append(readiness.Unanswered, gate.ID)
			continue
		}
		outcome, _, err := gate.Evaluate(result)
		if err != nil {
			return ExitReadiness{}, err
		}
		outcomes[gate.ID] = outcome
		switch outcome {
		case GatePassed:
			readiness.Passed = append(readiness.Passed, gate.ID)
		case GateFailed:
			readiness.Failed = append(readiness.Failed, gate.ID)
		default:
			readiness.Unanswered = append(readiness.Unanswered, gate.ID)
		}
	}
	for _, criterion := range DeclaredExitCriteria() {
		for _, id := range criterion.Gates {
			if outcomes[id] != GatePassed {
				readiness.UnestablishedCriteria = append(
					readiness.UnestablishedCriteria, criterion.ID)
				break
			}
		}
	}
	return readiness, nil
}
