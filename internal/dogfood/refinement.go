package dogfood

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
)

// ObservationID names one product observation from the dogfood run
// (M24-161..170).
type ObservationID string

const (
	ObserveGraphCollapsed   ObservationID = "graph-collapsed-still-usable"
	ObserveGraphModes       ObservationID = "graph-modes-change-a-decision"
	ObserveCrossNavigation  ObservationID = "message-and-node-navigation"
	ObservePauseReplay      ObservationID = "pause-restart-ordered-replay"
	ObserveWorkerCrash      ObservationID = "worker-crash-no-duplicate-effect"
	ObserveCoordinatorCrash ObservationID = "coordinator-crash-reconciliation"
	ObserveConcurrentEdit   ObservationID = "concurrent-user-edit-preserved"
	ObserveBudgetCap        ObservationID = "hard-budget-no-post-cap-request"
	ObserveScopedApproval   ObservationID = "scoped-network-approval"
	ObserveOpacity          ObservationID = "points-where-state-is-unknowable"
)

// AllObservations returns every declared observation.
func AllObservations() []ObservationID {
	return []ObservationID{
		ObserveGraphCollapsed, ObserveGraphModes, ObserveCrossNavigation,
		ObservePauseReplay, ObserveWorkerCrash, ObserveCoordinatorCrash,
		ObserveConcurrentEdit, ObserveBudgetCap, ObserveScopedApproval,
		ObserveOpacity,
	}
}

// Observation is one thing the run must establish about the product
// (M24-161..170).
type Observation struct {
	ID   ObservationID
	Todo string
	// Question is what the observation answers.
	Question string
	// AcceptableAnswer is what must be true.
	AcceptableAnswer string
	// RecordsNegative marks an observation whose value is recording what did
	// NOT work. These matter most: an observation that can only confirm is an
	// observation nobody learns from.
	RecordsNegative bool
}

// Observations returns the declared product observations (M24-161..170).
func Observations() []Observation {
	return []Observation{
		{
			ObserveGraphCollapsed, "M24-161",
			"with the graph collapsed, is every required action still available?",
			"planning, approval, execution, validation, review, and recovery are all " +
				"reachable without the graph; the graph is an aid, not a dependency",
			false,
		},
		{
			ObserveGraphModes, "M24-162",
			"does each graph mode change a decision, or only add visual activity?",
			"for each of Program, Execution, and Evidence, either a decision it changed " +
				"is named, or the mode is recorded as decoration",
			true,
		},
		{
			ObserveCrossNavigation, "M24-163",
			"does navigation work in both directions for every kind of node?",
			"a planning decision, an active tool action, a changed symbol, a failed check, " +
				"and an accepted evidence claim all navigate both ways",
			false,
		},
		{
			ObservePauseReplay, "M24-164",
			"after a pause and a browser restart, is the state exactly what it was?",
			"replay is ordered, complete, and retains control state; nothing is duplicated",
			false,
		},
		{
			ObserveWorkerCrash, "M24-165",
			"does a worker crash at a named boundary duplicate anything?",
			"no edit, command, test effect, or provider request is repeated",
			false,
		},
		{
			ObserveCoordinatorCrash, "M24-166",
			"after a coordinator crash, do the database, checkpoint, worktree, budget, " +
				"and interface agree?",
			"all five reconcile, and any disagreement is surfaced rather than resolved silently",
			false,
		},
		{
			ObserveConcurrentEdit, "M24-167",
			"is a user's concurrent edit preserved?",
			"the edit is detected, preserved, and resolved by an explicit decision",
			false,
		},
		{
			ObserveBudgetCap, "M24-168",
			"does anything paid start after the hard cap?",
			"no provider request begins after the cap, and there is no silent fallback to " +
				"a cheaper model",
			false,
		},
		{
			ObserveScopedApproval, "M24-169",
			"is a scoped network approval presented and enforced correctly?",
			"allow-once, allow-for-task, denial, expiry, and replay are each distinguishable " +
				"and behave as presented",
			false,
		},
		{
			ObserveOpacity, "M24-170",
			"where could the operator not tell what was happening?",
			"every point where current state, authority, cost, next action, failure " +
				"ownership, or recovery safety required opening raw storage or logs is recorded",
			true,
		},
	}
}

// ValidateObservations checks the set covers M24-161..170.
func ValidateObservations() error {
	todos := map[string]bool{}
	negatives := 0
	for _, observation := range Observations() {
		if !slices.Contains(AllObservations(), observation.ID) {
			return fmt.Errorf("unknown observation %q", observation.ID)
		}
		if strings.TrimSpace(observation.Question) == "" ||
			strings.TrimSpace(observation.AcceptableAnswer) == "" {
			return fmt.Errorf("observation %q is incomplete", observation.ID)
		}
		if todos[observation.Todo] {
			return fmt.Errorf("%s is claimed twice", observation.Todo)
		}
		todos[observation.Todo] = true
		if observation.RecordsNegative {
			negatives++
		}
	}
	for number := 161; number <= 170; number++ {
		todo := fmt.Sprintf("M24-%d", number)
		if !todos[todo] {
			return fmt.Errorf("no observation claims %s", todo)
		}
	}
	if negatives == 0 {
		return errors.New(
			"no observation records what did not work; a set that can only confirm is a " +
				"set nobody learns from")
	}
	return nil
}

// Ownership is who a failure belongs to (M24-174).
//
// Getting this wrong wastes the most time in a trial: a provider limitation
// fixed as a CodeFlux bug produces a change that helps nothing, and an
// evaluation-protocol defect blamed on the product invalidates the run.
type Ownership string

const (
	OwnerCodeflux    Ownership = "codeflux"
	OwnerProvider    Ownership = "provider-or-model"
	OwnerRequirement Ownership = "reserveflow-requirement"
	OwnerVisibleTest Ownership = "visible-test"
	OwnerHiddenTest  Ownership = "hidden-test"
	OwnerEnvironment Ownership = "environment"
	OwnerProtocol    Ownership = "evaluation-protocol"
)

// AllOwnerships returns every declared owner.
func AllOwnerships() []Ownership {
	return []Ownership{
		OwnerCodeflux, OwnerProvider, OwnerRequirement, OwnerVisibleTest,
		OwnerHiddenTest, OwnerEnvironment, OwnerProtocol,
	}
}

// Valid reports whether an ownership is declared.
func (ownership Ownership) Valid() bool { return slices.Contains(AllOwnerships(), ownership) }

// RepairableHere reports whether a repair to CodeFlux could fix it.
//
// Only a CodeFlux-owned failure is. Attempting to repair the others in
// CodeFlux produces a change that helps nothing and adds risk.
func (ownership Ownership) RepairableHere() bool { return ownership == OwnerCodeflux }

// InvalidatesRun reports whether this ownership means the run itself is
// suspect (M24-174).
func (ownership Ownership) InvalidatesRun() bool {
	return ownership == OwnerProtocol || ownership == OwnerHiddenTest
}

// FailureReport is one recorded dogfood failure (M24-171..175).
type FailureReport struct {
	// ID is stable and links the report to everything that produced it.
	ID string
	// Task, AcceptedBase, CodefluxVersion, Run, and Episode identify exactly
	// what was running (M24-171).
	Task            int
	AcceptedBase    string
	CodefluxVersion string
	Run             string
	Episode         string
	// EvaluatorResult is what the independent verdict said.
	EvaluatorResult string

	// Frozen is the preserved evidence (M24-172). Repair must not begin
	// before this exists: the first repair attempt usually destroys the state
	// that would have explained the failure.
	Frozen FrozenEvidence

	// Category, Severity, Frequency, Reproducibility, Symptom, and Layer are
	// the classification (M24-173).
	Category         string
	Severity         string
	Frequency        string
	Reproducibility  string
	Symptom          string
	ResponsibleLayer int

	Ownership Ownership

	// Workarounds are what was done to get past it (M24-175).
	Workarounds []Workaround
}

// FrozenEvidence is what must exist before repair begins (M24-172).
type FrozenEvidence struct {
	EventSequence string
	WorktreeDiff  string
	ProviderModel string
	Policy        string
	Budget        string
	Environment   string
	ToolVersions  string
	Diagnostics   string
	FrozenAt      time.Time
}

// FrozenFields names each required piece, so a gap is reported by name.
func FrozenFields() []string {
	return []string{
		"event-sequence", "worktree-diff", "provider-and-model", "policy",
		"budget", "environment", "tool-versions", "diagnostics",
	}
}

// Validate rejects incomplete frozen evidence.
func (frozen FrozenEvidence) Validate() error {
	values := map[string]string{
		"event-sequence": frozen.EventSequence, "worktree-diff": frozen.WorktreeDiff,
		"provider-and-model": frozen.ProviderModel, "policy": frozen.Policy,
		"budget": frozen.Budget, "environment": frozen.Environment,
		"tool-versions": frozen.ToolVersions, "diagnostics": frozen.Diagnostics,
	}
	var missing []string
	for _, field := range FrozenFields() {
		if strings.TrimSpace(values[field]) == "" {
			missing = append(missing, field)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf(
			"evidence was not frozen before repair: %s; the first repair attempt usually "+
				"destroys the state that would have explained the failure",
			strings.Join(missing, ", "))
	}
	if frozen.FrozenAt.IsZero() {
		return errors.New("frozen evidence has no time")
	}
	return nil
}

// Workaround is one thing done to get past a failure (M24-175).
type Workaround struct {
	Description string
	// Contaminated records that it made the run ineligible for the
	// no-intervention claim.
	Contaminated bool
	// ChangedAcceptance records that it altered what "passing" meant, which is
	// worse than contamination: it changes the question rather than the answer.
	ChangedAcceptance bool
}

// Validate rejects an unusable workaround record.
func (workaround Workaround) Validate() error {
	if strings.TrimSpace(workaround.Description) == "" {
		return errors.New("a workaround requires a description")
	}
	return nil
}

// Validate rejects a report that cannot support a repair decision.
func (report FailureReport) Validate() error {
	switch {
	case strings.TrimSpace(report.ID) == "":
		return errors.New("a failure report requires a stable identity")
	case report.Task < 1 || report.Task > PacketCount:
		return fmt.Errorf("report %q names task %d", report.ID, report.Task)
	case strings.TrimSpace(report.AcceptedBase) == "":
		return fmt.Errorf("report %q does not name the accepted base it started from", report.ID)
	case strings.TrimSpace(report.CodefluxVersion) == "":
		return fmt.Errorf("report %q does not name the CodeFlux version", report.ID)
	case strings.TrimSpace(report.Run) == "" || strings.TrimSpace(report.Episode) == "":
		return fmt.Errorf("report %q does not identify its run and episode", report.ID)
	case strings.TrimSpace(report.EvaluatorResult) == "":
		return fmt.Errorf("report %q does not record what the evaluator said", report.ID)
	}
	if err := report.Frozen.Validate(); err != nil {
		return fmt.Errorf("report %q: %w", report.ID, err)
	}
	for _, field := range map[string]string{
		"category": report.Category, "severity": report.Severity,
		"frequency": report.Frequency, "reproducibility": report.Reproducibility,
		"symptom": report.Symptom,
	} {
		if strings.TrimSpace(field) == "" {
			return fmt.Errorf("report %q is not fully classified", report.ID)
		}
	}
	// §0 layers are 0..20; a failure with no responsible layer cannot be
	// repaired at the right level.
	if report.ResponsibleLayer < 0 || report.ResponsibleLayer > 20 {
		return fmt.Errorf("report %q names layer %d, outside §0's 0..20",
			report.ID, report.ResponsibleLayer)
	}
	if !report.Ownership.Valid() {
		return fmt.Errorf("report %q has unknown ownership %q", report.ID, report.Ownership)
	}
	for _, workaround := range report.Workarounds {
		if err := workaround.Validate(); err != nil {
			return fmt.Errorf("report %q: %w", report.ID, err)
		}
	}
	return nil
}

// Contaminating reports whether this failure's workarounds voided the run.
func (report FailureReport) Contaminating() bool {
	for _, workaround := range report.Workarounds {
		if workaround.Contaminated || workaround.ChangedAcceptance {
			return true
		}
	}
	return false
}

// RepairStage is one step of the repair protocol (M24-176..190).
//
// The order is the protocol. Implementing before reproducing produces a repair
// nobody can show works; implementing before a failing test produces one
// nobody can show keeps working.
type RepairStage string

const (
	StageReproduce      RepairStage = "reproduce-outside-the-run"
	StageFailingTest    RepairStage = "add-a-failing-regression-test"
	StageStateClass     RepairStage = "state-the-failure-class-and-invariant"
	StageImplement      RepairStage = "implement-the-smallest-general-repair"
	StageTargetedSuite  RepairStage = "run-the-targeted-and-owning-suites"
	StageSelectedSuites RepairStage = "run-the-suites-the-invariant-implies"
	StageRebuild        RepairStage = "rebuild-and-version"
	StageResetAndRerun  RepairStage = "reset-and-rerun-from-the-accepted-base"
	StageMemoryOff      RepairStage = "verify-with-memory-disabled"
	StageMemoryOn       RepairStage = "rerun-with-accepted-memory-only"
	StageEarlierTests   RepairStage = "rerun-earlier-affected-acceptance-tests"
	StageUnrelatedCase  RepairStage = "run-an-unrelated-fixture-of-the-same-class"
	StageTradeoffs      RepairStage = "record-the-tradeoffs"
	StageClose          RepairStage = "close-the-defect-explicitly"
)

// RepairProtocol returns the stages in the order they must happen.
func RepairProtocol() []RepairStage {
	return []RepairStage{
		StageReproduce, StageFailingTest, StageStateClass, StageImplement,
		StageTargetedSuite, StageSelectedSuites, StageRebuild, StageResetAndRerun,
		StageMemoryOff, StageMemoryOn, StageEarlierTests, StageUnrelatedCase,
		StageTradeoffs, StageClose,
	}
}

// StageTodo maps a stage to the TODO that requires it.
func StageTodo(stage RepairStage) string {
	index := slices.Index(RepairProtocol(), stage)
	if index < 0 {
		return ""
	}
	return fmt.Sprintf("M24-%d", 176+index)
}

// Repair is one attempted repair (M24-176..190).
type Repair struct {
	FailureID string
	// Completed records which stages were performed.
	Completed []RepairStage
	// FailureClass and Invariant are what the repair generalises to
	// (M24-178).
	FailureClass string
	Invariant    string
	// TouchedPolicies records whether the repair weakened any policy
	// (M24-179). Any true value is disqualifying.
	WeakenedValidation      bool
	WeakenedPermission      bool
	WeakenedEvidence        bool
	WeakenedBudget          bool
	WeakenedProjectBoundary bool
	WeakenedRecovery        bool
	// PassesOnlyHiddenCase, UsesFutureKnowledge, and AddsTaskSpecificPrompt are
	// the three disqualifying shortcuts (M24-188).
	PassesOnlyHiddenCase   bool
	UsesFutureKnowledge    bool
	AddsTaskSpecificPrompt bool
	// Tradeoffs are the recorded costs (M24-189).
	Tradeoffs map[string]string
	// Closure is how the defect was closed (M24-190).
	Closure Closure
}

// Closure is how a defect ends (M24-190).
type Closure string

const (
	ClosureFixed              Closure = "fixed"
	ClosureAcceptedLimitation Closure = "accepted-limitation"
	ClosureDeferred           Closure = "deferred"
	ClosureEvaluatorDefect    Closure = "evaluator-defect"
	ClosureOutOfScope         Closure = "product-scope-rejection"
)

// AllClosures returns every declared closure.
func AllClosures() []Closure {
	return []Closure{
		ClosureFixed, ClosureAcceptedLimitation, ClosureDeferred,
		ClosureEvaluatorDefect, ClosureOutOfScope,
	}
}

// Valid reports whether a closure is declared. There is deliberately no
// "silently discarded": every defect ends in one of these five.
func (closure Closure) Valid() bool { return slices.Contains(AllClosures(), closure) }

// TradeoffDimensions are what must be recorded before closing (M24-189).
func TradeoffDimensions() []string {
	return []string{"correctness", "speed", "cost", "ux", "devx", "new-tradeoff"}
}

// Validate rejects a repair that skipped the protocol or took a shortcut.
func (repair Repair) Validate() error {
	if strings.TrimSpace(repair.FailureID) == "" {
		return errors.New("a repair must name the failure it addresses")
	}

	// M24-178: a repair with no stated class is a patch, not a repair.
	if strings.TrimSpace(repair.FailureClass) == "" || strings.TrimSpace(repair.Invariant) == "" {
		return fmt.Errorf(
			"repair for %q states no failure class or invariant; without one it fixes an "+
				"instance rather than a class", repair.FailureID)
	}

	// M24-179: no repair may buy a pass by weakening a policy.
	weakened := map[string]bool{
		"validation": repair.WeakenedValidation, "permission": repair.WeakenedPermission,
		"evidence": repair.WeakenedEvidence, "budget": repair.WeakenedBudget,
		"project-boundary": repair.WeakenedProjectBoundary, "recovery": repair.WeakenedRecovery,
	}
	var loosened []string
	for name, was := range weakened {
		if was {
			loosened = append(loosened, name)
		}
	}
	if len(loosened) > 0 {
		sort.Strings(loosened)
		return fmt.Errorf(
			"repair for %q weakens %s policy; a repair that buys a pass by lowering a "+
				"guarantee has made the system worse", repair.FailureID, strings.Join(loosened, ", "))
	}

	// M24-188: the three shortcuts that make a repair worthless.
	if repair.PassesOnlyHiddenCase {
		return fmt.Errorf(
			"repair for %q passes only the hidden case; it fixes the test, not the system",
			repair.FailureID)
	}
	if repair.UsesFutureKnowledge {
		return fmt.Errorf(
			"repair for %q relies on knowledge of a future requirement, which no real user "+
				"would have", repair.FailureID)
	}
	if repair.AddsTaskSpecificPrompt {
		return fmt.Errorf(
			"repair for %q adds task-specific prompt text without a general invariant; it "+
				"would not survive the next task", repair.FailureID)
	}

	// M24-190: every defect closes explicitly.
	if !repair.Closure.Valid() {
		return fmt.Errorf("repair for %q closes as %q, which is not a declared closure",
			repair.FailureID, repair.Closure)
	}

	// M24-189: tradeoffs are recorded before closing.
	var unrecorded []string
	for _, dimension := range TradeoffDimensions() {
		if strings.TrimSpace(repair.Tradeoffs[dimension]) == "" {
			unrecorded = append(unrecorded, dimension)
		}
	}
	if len(unrecorded) > 0 {
		sort.Strings(unrecorded)
		return fmt.Errorf("repair for %q closed without recording %s",
			repair.FailureID, strings.Join(unrecorded, ", "))
	}

	// The protocol's order is the protocol. A stage performed out of order
	// produces a repair nobody can show works.
	completed := map[RepairStage]int{}
	for index, stage := range repair.Completed {
		if slices.Index(RepairProtocol(), stage) < 0 {
			return fmt.Errorf("repair for %q performed unknown stage %q",
				repair.FailureID, stage)
		}
		if _, duplicate := completed[stage]; duplicate {
			return fmt.Errorf("repair for %q performed %q twice", repair.FailureID, stage)
		}
		completed[stage] = index
	}
	for _, stage := range RepairProtocol() {
		if _, done := completed[stage]; !done {
			return fmt.Errorf("repair for %q skipped %q (%s)",
				repair.FailureID, stage, StageTodo(stage))
		}
	}
	for index := 1; index < len(RepairProtocol()); index++ {
		previous := RepairProtocol()[index-1]
		current := RepairProtocol()[index]
		if completed[current] < completed[previous] {
			return fmt.Errorf("repair for %q performed %q before %q",
				repair.FailureID, current, previous)
		}
	}
	return nil
}

// ReviewerConfig freezes the adversarial reviewer before use (M24-216).
type ReviewerConfig struct {
	Prompt         string
	Model          string
	InputAllowlist []string
	OutputSchema   string
	// CanEdit and CanApprove must both be false: an evaluation-only reviewer
	// that could act would be a second agent in the run.
	CanEdit    bool
	CanApprove bool
	// ExecutionTiming states when it runs relative to the evaluated run.
	ExecutionTiming string
	BudgetMinor     int64
	CostAccounting  string
	FrozenAt        time.Time
}

// Validate rejects a reviewer that could influence the run it reviews
// (M24-216, M24-217).
func (config ReviewerConfig) Validate() error {
	switch {
	case strings.TrimSpace(config.Prompt) == "":
		return errors.New("the reviewer has no frozen prompt")
	case strings.TrimSpace(config.Model) == "":
		return errors.New("the reviewer has no frozen model")
	case len(config.InputAllowlist) == 0:
		return errors.New(
			"the reviewer has no input allowlist; without one it can read anything, " +
				"including the hidden assertions")
	case strings.TrimSpace(config.OutputSchema) == "":
		return errors.New("the reviewer has no output schema")
	case strings.TrimSpace(config.ExecutionTiming) == "":
		return errors.New("the reviewer's execution timing is not frozen")
	case config.BudgetMinor <= 0:
		return errors.New("the reviewer has no budget")
	case strings.TrimSpace(config.CostAccounting) == "":
		return errors.New("the reviewer's cost is not accounted anywhere")
	case config.FrozenAt.IsZero():
		return errors.New("the reviewer configuration has no freeze time")
	}
	if config.CanEdit {
		return errors.New(
			"the reviewer can edit; an evaluation-only reviewer that acts is a second " +
				"agent in the run, and its contribution becomes inseparable")
	}
	if config.CanApprove {
		return errors.New("the reviewer can approve, which makes it an authority in the run")
	}
	// M24-217: the allowlist must not reach anything that would let it leak
	// the answers into its findings.
	for _, allowed := range config.InputAllowlist {
		lowered := strings.ToLower(allowed)
		for _, forbidden := range []string{
			"evaluator", "hidden", "answer", "future", "packet",
		} {
			if strings.Contains(lowered, forbidden) {
				return fmt.Errorf(
					"the reviewer's allowlist includes %q, which reaches %s material",
					allowed, forbidden)
			}
		}
	}
	return nil
}

// Preregistration is a candidate change registered before it is tried
// (M24-219, M24-220).
//
// Preregistration is what stops a tuning loop from finding whatever it went
// looking for. Without it, a candidate is judged on the cohort that suggested
// it, and every result is a coincidence dressed as an improvement.
type Preregistration struct {
	CandidateVersion string
	// Diff is the exact change. One general invariant at a time.
	Diff string
	// TuningCohort is what the candidate may be selected on.
	TuningCohort string
	// HeldOutCohort stays frozen until after selection.
	HeldOutCohort               string
	PrimaryEndpoint             string
	MinimumEffect               string
	Repetitions                 int
	Analysis                    string
	StopRule                    string
	MultipleComparisonTreatment string
	ExecutionEnvelope           string
}

// Validate rejects a preregistration that would not constrain anything.
func (registration Preregistration) Validate() error {
	required := map[string]string{
		"candidate-version":   registration.CandidateVersion,
		"diff":                registration.Diff,
		"tuning-cohort":       registration.TuningCohort,
		"held-out-cohort":     registration.HeldOutCohort,
		"primary-endpoint":    registration.PrimaryEndpoint,
		"minimum-effect":      registration.MinimumEffect,
		"analysis":            registration.Analysis,
		"stop-rule":           registration.StopRule,
		"multiple-comparison": registration.MultipleComparisonTreatment,
		"execution-envelope":  registration.ExecutionEnvelope,
	}
	var missing []string
	for name, value := range required {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf(
			"the preregistration omits %s; an unregistered field is one that can be chosen "+
				"after the result is known", strings.Join(missing, ", "))
	}
	if registration.Repetitions < 2 {
		return fmt.Errorf(
			"the preregistration plans %d repetition(s); a single run cannot separate an "+
				"effect from variance", registration.Repetitions)
	}
	if registration.TuningCohort == registration.HeldOutCohort {
		return errors.New(
			"the tuning and held-out cohorts are the same, so selection and confirmation " +
				"happen on identical data")
	}
	return nil
}

// SelectionOutcome is the result of judging a candidate (M24-220, M24-221).
type SelectionOutcome string

const (
	// SelectionRetained means the candidate met its preregistered gate.
	SelectionRetained SelectionOutcome = "retained"
	// SelectionInconclusive means the evidence does not decide.
	SelectionInconclusive SelectionOutcome = "inconclusive"
	// SelectionRetired means the candidate is dropped.
	SelectionRetired SelectionOutcome = "retired"
)

// RegressionDimensions are the dimensions in which any regression is
// disqualifying (M24-221).
func RegressionDimensions() []string {
	return []string{
		"correctness", "validation", "authority", "security",
		"secrecy", "recovery", "independent-acceptance",
	}
}

// JudgeCandidate decides a candidate's fate (M24-220, M24-221).
//
// Any regression in a named dimension is disqualifying regardless of the
// improvement elsewhere: a faster system that is less correct is not a better
// system, and trading a security property for speed is not a trade this
// project makes.
func JudgeCandidate(
	registration Preregistration,
	regressions map[string]bool,
	metGate bool,
	usedHiddenResults bool,
) (SelectionOutcome, string, error) {
	if err := registration.Validate(); err != nil {
		return "", "", err
	}
	// M24-220: hidden-evaluator results must never inform prompt selection.
	// Using them tunes the system to the answer key.
	if usedHiddenResults {
		return "", "", errors.New(
			"hidden-evaluator results were used to select a candidate; that tunes the " +
				"system to the answer key and invalidates every later result")
	}
	var regressed []string
	for _, dimension := range RegressionDimensions() {
		if regressions[dimension] {
			regressed = append(regressed, dimension)
		}
	}
	if len(regressed) > 0 {
		sort.Strings(regressed)
		return SelectionRetired,
			"regressed in " + strings.Join(regressed, ", "), nil
	}
	if !metGate {
		return SelectionInconclusive,
			"did not meet its preregistered gate on the named stratum", nil
	}
	return SelectionRetained, "met its preregistered gate with no regression", nil
}
