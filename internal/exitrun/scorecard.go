package exitrun

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
)

// MetricGroup names one part of the exit scorecard (M24-080..088).
type MetricGroup string

const (
	GroupCorrectness MetricGroup = "correctness"
	GroupLatency     MetricGroup = "latency"
	GroupCost        MetricGroup = "tokens-and-cost"
	GroupCalibration MetricGroup = "forecast-calibration"
	GroupUsability   MetricGroup = "usability"
	GroupRecovery    MetricGroup = "interruption-and-recovery"
	GroupSecurity    MetricGroup = "permission-and-security-boundary"
	GroupGraph       MetricGroup = "graph-usefulness"
	GroupMemory      MetricGroup = "memory-influence"
)

// AllMetricGroups returns every scorecard group, in order.
func AllMetricGroups() []MetricGroup {
	return []MetricGroup{
		GroupCorrectness, GroupLatency, GroupCost, GroupCalibration,
		GroupUsability, GroupRecovery, GroupSecurity, GroupGraph, GroupMemory,
	}
}

// GroupTodo maps a group to the TODO that populates it.
func GroupTodo(group MetricGroup) string {
	index := slices.Index(AllMetricGroups(), group)
	if index < 0 {
		return ""
	}
	return fmt.Sprintf("M24-%03d", 80+index)
}

// Section is one populated scorecard group.
type Section struct {
	Group MetricGroup
	// Findings are what the run actually observed. Free text is deliberate:
	// forcing every observation into a number is how the interesting ones get
	// dropped.
	Findings []string
	// Populated records that the group was addressed. A section with no
	// findings and Populated false is an omission; with Populated true it is
	// an explicit "nothing to report", which is different.
	Populated bool
}

// Validate rejects a section that would misrepresent the run.
func (section Section) Validate() error {
	if !slices.Contains(AllMetricGroups(), section.Group) {
		return fmt.Errorf("unknown metric group %q", section.Group)
	}
	if !section.Populated {
		return fmt.Errorf("group %q (%s) was not populated",
			section.Group, GroupTodo(section.Group))
	}
	for index, finding := range section.Findings {
		if strings.TrimSpace(finding) == "" {
			return fmt.Errorf("group %q finding %d is empty", section.Group, index)
		}
	}
	return nil
}

// FailureClass classifies why something went wrong (M24-092).
//
// The classification decides what to do next, which is why guessing is not
// allowed: a specification defect fixed as an implementation bug produces a
// system that does the wrong thing correctly.
type FailureClass string

const (
	ClassImplementation FailureClass = "implementation-bug"
	ClassSpecification  FailureClass = "specification-defect"
	ClassModel          FailureClass = "model-limitation"
	ClassTooling        FailureClass = "tooling-limitation"
	ClassUX             FailureClass = "ux-failure"
	ClassExperiment     FailureClass = "experiment-design-problem"
)

// AllFailureClasses returns every class.
func AllFailureClasses() []FailureClass {
	return []FailureClass{
		ClassImplementation, ClassSpecification, ClassModel,
		ClassTooling, ClassUX, ClassExperiment,
	}
}

// Valid reports whether a class is declared.
func (class FailureClass) Valid() bool {
	return slices.Contains(AllFailureClasses(), class)
}

// Remedy states what a class implies about the fix.
func (class FailureClass) Remedy() string {
	switch class {
	case ClassImplementation:
		return "fix the code; the design was right"
	case ClassSpecification:
		return "change the plan; the code did what was specified and the specification was wrong"
	case ClassModel:
		return "no local fix; either constrain the task, change models, or accept the limit"
	case ClassTooling:
		return "fix or replace the tool; the agent's reasoning was not the problem"
	case ClassUX:
		return "the system behaved correctly and the user could not tell; fix the presentation"
	case ClassExperiment:
		return "the run itself was flawed; repeat it before drawing any conclusion"
	default:
		return ""
	}
}

// Failure is one classified problem (M24-092).
type Failure struct {
	Summary string
	Class   FailureClass
	// Evidence is what supports the classification. Required, because
	// classifying by intuition produces a plan built on intuition.
	Evidence string
	Blocking bool
}

// Validate rejects an unusable failure record.
func (failure Failure) Validate() error {
	switch {
	case strings.TrimSpace(failure.Summary) == "":
		return errors.New("a failure requires a summary")
	case !failure.Class.Valid():
		return fmt.Errorf("failure %q has unknown class %q", failure.Summary, failure.Class)
	case strings.TrimSpace(failure.Evidence) == "":
		return fmt.Errorf(
			"failure %q is classified with no evidence; classifying by intuition builds "+
				"the next plan on intuition", failure.Summary)
	}
	return nil
}

// Subsystem is a part of the product the decision applies to (M24-094).
type Subsystem string

const (
	SubsystemPlanning   Subsystem = "planning-and-forecasting"
	SubsystemExecution  Subsystem = "execution-and-permissions"
	SubsystemValidation Subsystem = "validation-and-evidence"
	SubsystemRecovery   Subsystem = "recovery"
	SubsystemGraph      Subsystem = "graph"
	SubsystemMemory     Subsystem = "memory"
	SubsystemAtoms      Subsystem = "atoms"
	SubsystemInterface  Subsystem = "interface"
)

// AllSubsystems returns every subsystem a decision must be made about.
func AllSubsystems() []Subsystem {
	return []Subsystem{
		SubsystemPlanning, SubsystemExecution, SubsystemValidation,
		SubsystemRecovery, SubsystemGraph, SubsystemMemory,
		SubsystemAtoms, SubsystemInterface,
	}
}

// Verdict is what happens to a subsystem (M24-094).
type Verdict string

const (
	// VerdictContinue means it works and stays as designed.
	VerdictContinue Verdict = "continue"
	// VerdictNarrow means it works within a smaller scope than planned.
	VerdictNarrow Verdict = "narrow"
	// VerdictRedesign means the idea holds but this shape of it does not.
	VerdictRedesign Verdict = "redesign"
	// VerdictStop means the evidence says do not continue.
	VerdictStop Verdict = "stop"
)

// AllVerdicts returns every possible decision.
func AllVerdicts() []Verdict {
	return []Verdict{VerdictContinue, VerdictNarrow, VerdictRedesign, VerdictStop}
}

// Valid reports whether a verdict is declared.
func (verdict Verdict) Valid() bool { return slices.Contains(AllVerdicts(), verdict) }

// Decision is one subsystem's outcome (M24-094).
type Decision struct {
	Subsystem Subsystem
	Verdict   Verdict
	// Rationale must cite what in the run led here.
	Rationale string
	// EvidenceRefs point at the scorecard groups or failures behind it.
	EvidenceRefs []string
}

// Validate rejects a decision made without evidence.
func (decision Decision) Validate() error {
	switch {
	case !slices.Contains(AllSubsystems(), decision.Subsystem):
		return fmt.Errorf("unknown subsystem %q", decision.Subsystem)
	case !decision.Verdict.Valid():
		return fmt.Errorf("subsystem %q has unknown verdict %q",
			decision.Subsystem, decision.Verdict)
	case strings.TrimSpace(decision.Rationale) == "":
		return fmt.Errorf("subsystem %q was decided with no rationale", decision.Subsystem)
	case len(decision.EvidenceRefs) == 0:
		return fmt.Errorf(
			"subsystem %q was decided with no evidence reference; a decision with no "+
				"evidence is a preference", decision.Subsystem)
	}
	return nil
}

// KillCriterion is a §30 threshold the run is compared against (M24-093).
type KillCriterion struct {
	Name string
	// Condition is what would trigger it, stated so it can be checked rather
	// than argued about.
	Condition string
	// Consequence is what happens if it triggers.
	Consequence string
	// Triggered is whether the run met the condition.
	Triggered bool
	// Evidence is what shows it did or did not.
	Evidence string
}

// DeclaredKillCriteria returns the §30 criteria the exit run must be compared
// against (M24-093).
func DeclaredKillCriteria() []KillCriterion {
	return []KillCriterion{
		{
			Name:      "atom-reuse-failure",
			Condition: "meaningful atom reuse occurs in fewer than 20% of eligible tasks after 500 tasks",
			Consequence: "stop investing in atoms; the verification cost is not being " +
				"amortised and the mechanism does not pay for itself",
		},
		{
			Name:      "unresolved-correctness-failure",
			Condition: "hidden acceptance tests fail on the exit run",
			Consequence: "the prototype does not exit; a system that looks finished and is " +
				"not is the outcome this whole exercise exists to detect",
		},
		{
			Name:      "unapproved-external-effect",
			Condition: "any external effect occurred that the user did not explicitly approve",
			Consequence: "stop and redesign the authority model; this failure has consequences " +
				"outside the machine and cannot be undone by rejecting a diff",
		},
		{
			Name:        "credential-leakage",
			Condition:   "seeded credential material is found in any durable artifact",
			Consequence: "stop and fix redaction before anything else; a leak outlives the run",
		},
		{
			Name:      "unbacked-correctness-claim",
			Condition: "the interface made a correctness claim with no evidence behind it",
			Consequence: "redesign the evidence model; a confident wrong claim is worse than " +
				"no claim",
		},
	}
}

// Validate rejects an unusable criterion.
func (criterion KillCriterion) Validate() error {
	switch {
	case strings.TrimSpace(criterion.Name) == "":
		return errors.New("a kill criterion requires a name")
	case strings.TrimSpace(criterion.Condition) == "":
		return fmt.Errorf("criterion %q states no condition", criterion.Name)
	case strings.TrimSpace(criterion.Consequence) == "":
		return fmt.Errorf("criterion %q states no consequence", criterion.Name)
	}
	// A criterion evaluated either way needs evidence: recording "not
	// triggered" without looking is how a kill criterion becomes decorative.
	if strings.TrimSpace(criterion.Evidence) == "" {
		return fmt.Errorf(
			"criterion %q was recorded with no evidence, so it was not actually checked",
			criterion.Name)
	}
	return nil
}

// GatedFeature is a capability that stays off unless its own gate is met
// (M24-095, M24-096).
type GatedFeature struct {
	Name string
	Todo string
	// Gate is the evidence that would justify enabling it.
	Gate string
	// Enabled is whether the exit run enabled it.
	Enabled bool
	// GateMet is whether the evidence exists.
	GateMet bool
}

// DeclaredGatedFeatures returns the features that must stay disabled
// (M24-095, M24-096).
func DeclaredGatedFeatures() []GatedFeature {
	return []GatedFeature{
		{
			Name: "adaptive-routing", Todo: "M24-095",
			Gate: "measured evidence that routing between models improves outcome per unit " +
				"cost, from runs where the routing decision was the only variable",
		},
		{
			Name: "deep-graph-verification", Todo: "M24-096",
			Gate: "an independent graph gate showing the verification establishes something " +
				"the ordinary validation does not already establish",
		},
	}
}

// Validate refuses a feature enabled without its gate.
func (feature GatedFeature) Validate() error {
	switch {
	case strings.TrimSpace(feature.Name) == "":
		return errors.New("a gated feature requires a name")
	case strings.TrimSpace(feature.Gate) == "":
		return fmt.Errorf("feature %q declares no gate", feature.Name)
	case !strings.HasPrefix(feature.Todo, "M24-"):
		return fmt.Errorf("feature %q cites %q, want an M24 TODO", feature.Name, feature.Todo)
	}
	if feature.Enabled && !feature.GateMet {
		return fmt.Errorf(
			"feature %q is enabled but its gate is not met; enabling on hope is how a "+
				"prototype ships a capability nobody validated", feature.Name)
	}
	return nil
}

// Defect is one entry in the post-prototype list (M24-097).
type Defect struct {
	Summary  string
	Class    FailureClass
	Priority int
	// Impact says who is affected and how.
	Impact string
}

// Validate rejects an unprioritised or unclassified defect.
func (defect Defect) Validate() error {
	switch {
	case strings.TrimSpace(defect.Summary) == "":
		return errors.New("a defect requires a summary")
	case !defect.Class.Valid():
		return fmt.Errorf("defect %q has unknown class %q", defect.Summary, defect.Class)
	case defect.Priority <= 0:
		return fmt.Errorf("defect %q has no priority; an unprioritised list is a wish list",
			defect.Summary)
	case strings.TrimSpace(defect.Impact) == "":
		return fmt.Errorf("defect %q states no impact", defect.Summary)
	}
	return nil
}

// Scorecard is the whole exit result (M24-080..100).
type Scorecard struct {
	// SourceRevision is the tagged prototype revision (M24-098).
	SourceRevision string
	// MethodologyPath is where the archived methodology lives (M24-099).
	MethodologyPath string
	At              time.Time

	Sections []Section
	// Workarounds is every manual step a user had to take (M24-089).
	Workarounds []string
	// Flaky is every result that did not reproduce (M24-090).
	Flaky []string
	// MisleadingClaims are statements a user could plausibly misread (M24-091).
	MisleadingClaims []string
	Failures         []Failure
	KillCriteria     []KillCriterion
	Decisions        []Decision
	GatedFeatures    []GatedFeature
	Defects          []Defect
	// PlanUpdates are the evidence-driven changes made to docs/plan.md
	// (M24-100).
	PlanUpdates []string
}

// Validate rejects a scorecard that cannot support the prototype decision.
func (card Scorecard) Validate() error {
	if strings.TrimSpace(card.SourceRevision) == "" {
		return errors.New("the scorecard does not name the source revision it describes (M24-098)")
	}
	if strings.TrimSpace(card.MethodologyPath) == "" {
		return errors.New("the scorecard does not name where its methodology is archived (M24-099)")
	}
	if card.At.IsZero() {
		return errors.New("the scorecard has no date")
	}

	// M24-080..088: every group must be populated.
	populated := map[MetricGroup]bool{}
	for _, section := range card.Sections {
		if err := section.Validate(); err != nil {
			return err
		}
		if populated[section.Group] {
			return fmt.Errorf("group %q appears twice", section.Group)
		}
		populated[section.Group] = true
	}
	for _, group := range AllMetricGroups() {
		if !populated[group] {
			return fmt.Errorf("group %q (%s) is missing from the scorecard",
				group, GroupTodo(group))
		}
	}

	for _, failure := range card.Failures {
		if err := failure.Validate(); err != nil {
			return err
		}
	}
	for _, defect := range card.Defects {
		if err := defect.Validate(); err != nil {
			return err
		}
	}
	for _, feature := range card.GatedFeatures {
		if err := feature.Validate(); err != nil {
			return err
		}
	}

	// M24-093: every declared criterion must be compared against, with
	// evidence either way.
	declared := DeclaredKillCriteria()
	compared := map[string]bool{}
	for _, criterion := range card.KillCriteria {
		if err := criterion.Validate(); err != nil {
			return err
		}
		compared[criterion.Name] = true
	}
	for _, criterion := range declared {
		if !compared[criterion.Name] {
			return fmt.Errorf(
				"kill criterion %q was not compared against (M24-093)", criterion.Name)
		}
	}

	// M24-094: every subsystem needs a decision.
	decided := map[Subsystem]bool{}
	for _, decision := range card.Decisions {
		if err := decision.Validate(); err != nil {
			return err
		}
		if decided[decision.Subsystem] {
			return fmt.Errorf("subsystem %q was decided twice", decision.Subsystem)
		}
		decided[decision.Subsystem] = true
	}
	for _, subsystem := range AllSubsystems() {
		if !decided[subsystem] {
			return fmt.Errorf("subsystem %q has no decision (M24-094)", subsystem)
		}
	}

	// M24-095, M24-096: both gated features must be accounted for.
	accounted := map[string]bool{}
	for _, feature := range card.GatedFeatures {
		accounted[feature.Name] = true
	}
	for _, feature := range DeclaredGatedFeatures() {
		if !accounted[feature.Name] {
			return fmt.Errorf("gated feature %q is unaccounted for (%s)",
				feature.Name, feature.Todo)
		}
	}

	// M24-100: the plan must be updated with what the run established. A run
	// that changed nothing about the plan either learned nothing or was not
	// allowed to.
	if len(card.PlanUpdates) == 0 {
		return errors.New(
			"no plan updates were recorded (M24-100); an exit run that changes nothing " +
				"in the plan either learned nothing or was not permitted to say so")
	}
	return nil
}

// TriggeredCriteria returns the kill criteria the run met.
func (card Scorecard) TriggeredCriteria() []KillCriterion {
	var triggered []KillCriterion
	for _, criterion := range card.KillCriteria {
		if criterion.Triggered {
			triggered = append(triggered, criterion)
		}
	}
	sort.Slice(triggered, func(left, right int) bool {
		return triggered[left].Name < triggered[right].Name
	})
	return triggered
}

// ExitVerdict is the prototype's overall outcome.
type ExitVerdict string

const (
	// ExitPassed means the prototype exits.
	ExitPassed ExitVerdict = "passed"
	// ExitBlocked means a kill criterion triggered.
	ExitBlocked ExitVerdict = "blocked"
	// ExitInconclusive means the run itself was flawed.
	ExitInconclusive ExitVerdict = "inconclusive"
)

// Decide produces the overall outcome from the evidence.
//
// The order is deliberate. An inconclusive run is checked FIRST: if the
// experiment was broken, neither a pass nor a failure means anything, and
// reporting either would be the worst possible outcome of an exit run.
func (card Scorecard) Decide() (ExitVerdict, string, error) {
	if err := card.Validate(); err != nil {
		return "", "", err
	}
	for _, failure := range card.Failures {
		if failure.Class == ClassExperiment {
			return ExitInconclusive, fmt.Sprintf(
				"the run itself was flawed (%s); repeat it before concluding anything",
				failure.Summary), nil
		}
	}
	if triggered := card.TriggeredCriteria(); len(triggered) > 0 {
		names := make([]string, 0, len(triggered))
		for _, criterion := range triggered {
			names = append(names, criterion.Name)
		}
		return ExitBlocked, "kill criteria triggered: " + strings.Join(names, ", "), nil
	}
	for _, decision := range card.Decisions {
		if decision.Verdict == VerdictStop {
			return ExitBlocked, fmt.Sprintf(
				"subsystem %q was decided stop", decision.Subsystem), nil
		}
	}
	return ExitPassed, "no kill criterion triggered and no subsystem was stopped", nil
}
