package exitrun

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
)

// Phase groups the exit-run journey (M24-010..051).
type Phase string

const (
	PhaseFirstRun   Phase = "first-run-and-repository"
	PhasePlanning   Phase = "plan-forecast-approval"
	PhaseExecution  Phase = "execution"
	PhaseValidation Phase = "validation-and-review"
)

// AllPhases returns the journey phases in order.
func AllPhases() []Phase {
	return []Phase{PhaseFirstRun, PhasePlanning, PhaseExecution, PhaseValidation}
}

// StepKind separates what an evaluator does from what they measure.
//
// The distinction is load-bearing for the scorecard: an action that failed is
// a product defect, while a measurement that could not be taken is a gap in
// the protocol, and conflating them would let one hide behind the other.
type StepKind string

const (
	// KindAction is something the evaluator does.
	KindAction StepKind = "action"
	// KindObserve is something they must be able to see and understand.
	KindObserve StepKind = "observe"
	// KindMeasure is a number the run records.
	KindMeasure StepKind = "measure"
)

// Step is one step of the exit-run journey.
type Step struct {
	Todo  string
	Phase Phase
	Kind  StepKind
	// Instruction is what the evaluator does, in the imperative.
	Instruction string
	// Expected is what must be true afterwards. For a measurement this is what
	// the number means, not a threshold: thresholds live in the scorecard.
	Expected string
	// Measurement names the recorded value, for KindMeasure steps.
	Measurement string
	// FailureMeaning says what a failure at this step tells you about the
	// product. A step whose failure means nothing is not worth walking.
	FailureMeaning string
}

// Validate rejects a step an evaluator could not follow.
func (step Step) Validate() error {
	switch {
	case !strings.HasPrefix(step.Todo, "M24-"):
		return fmt.Errorf("step %q does not cite an M24 TODO", step.Instruction)
	case !slices.Contains(AllPhases(), step.Phase):
		return fmt.Errorf("%s has unknown phase %q", step.Todo, step.Phase)
	case step.Kind != KindAction && step.Kind != KindObserve && step.Kind != KindMeasure:
		return fmt.Errorf("%s has unknown kind %q", step.Todo, step.Kind)
	case strings.TrimSpace(step.Instruction) == "":
		return fmt.Errorf("%s has no instruction", step.Todo)
	case strings.TrimSpace(step.Expected) == "":
		return fmt.Errorf("%s states no expectation", step.Todo)
	case strings.TrimSpace(step.FailureMeaning) == "":
		return fmt.Errorf("%s does not say what its failure would mean", step.Todo)
	}
	if step.Kind == KindMeasure && strings.TrimSpace(step.Measurement) == "" {
		return fmt.Errorf("%s measures something unnamed", step.Todo)
	}
	if step.Kind != KindMeasure && strings.TrimSpace(step.Measurement) != "" {
		return fmt.Errorf("%s names a measurement but is not a measurement step", step.Todo)
	}
	return nil
}

// Steps returns the whole journey (M24-010..051).
func Steps() []Step {
	return append(append(append(
		firstRunSteps(), planningSteps()...), executionSteps()...), validationSteps()...)
}

func firstRunSteps() []Step {
	return []Step{
		{
			Todo: "M24-010", Phase: PhaseFirstRun, Kind: KindMeasure,
			Instruction: "start the clock at the first command and stop it when the interface is on screen",
			Measurement: "install-to-first-screen",
			Expected:    "the elapsed time from the first command to a usable interface",
			FailureMeaning: "a long time here means a new user gives up before seeing anything, " +
				"whatever the rest of the product does",
		},
		{
			Todo: "M24-011", Phase: PhaseFirstRun, Kind: KindObserve,
			Instruction: "read the first-run explanation without skipping",
			Expected: "the evaluator can say afterwards where their data lives, what stays " +
				"in Git, and what leaves the machine",
			FailureMeaning: "an explanation a reader cannot summarise afterwards has not " +
				"explained anything, and the consent it was meant to inform is not informed",
		},
		{
			Todo: "M24-012", Phase: PhaseFirstRun, Kind: KindAction,
			Instruction: "configure and test the provider through the documented journey",
			Expected: "the test reports a clear result, and a failure names the credential, " +
				"the endpoint, or the network",
			FailureMeaning: "a provider test that cannot distinguish those three sends every " +
				"user to re-paste a key that was already correct",
		},
		{
			Todo: "M24-013", Phase: PhaseFirstRun, Kind: KindAction,
			Instruction: "open the frozen demonstration repository",
			Expected:    "the repository is accepted, and its clean working tree is confirmed",
			FailureMeaning: "if a dirty tree is accepted here, no later diff can be attributed " +
				"to the agent rather than the user",
		},
		{
			Todo: "M24-014", Phase: PhaseFirstRun, Kind: KindObserve,
			Instruction: "inspect the repository status and the proposed worktree policy",
			Expected: "the evaluator can state what will happen without asking and what will " +
				"always ask, before any task runs",
			FailureMeaning: "a user who learns the permission model from the first surprise " +
				"has already been surprised",
		},
		{
			Todo: "M24-015", Phase: PhaseFirstRun, Kind: KindObserve,
			Instruction: "inspect the selected context and ask why each item is there",
			Expected: "every included item has a stated reason, and exclusions are visible " +
				"rather than silent",
			FailureMeaning: "context nobody can explain is context nobody can correct, and a " +
				"wrong answer traced to it is unexplainable",
		},
		{
			Todo: "M24-016", Phase: PhaseFirstRun, Kind: KindAction,
			Instruction:    "create a new thread",
			Expected:       "an empty thread exists and is durable across a reload",
			FailureMeaning: "a thread that does not survive a reload loses the user's work",
		},
		{
			Todo: "M24-017", Phase: PhaseFirstRun, Kind: KindAction,
			Instruction: "submit the frozen task requirement verbatim, without rewording",
			Expected:    "the requirement is stored exactly as written",
			FailureMeaning: "rewording the requirement makes the run unrepeatable and lets " +
				"an evaluator unconsciously help the agent",
		},
	}
}

func planningSteps() []Step {
	return []Step{
		{
			Todo: "M24-018", Phase: PhasePlanning, Kind: KindMeasure,
			Instruction:    "record the time from submission to the first forecast",
			Measurement:    "time-to-first-forecast",
			Expected:       "the elapsed time before the user is told what this might cost",
			FailureMeaning: "a user who waits without a cost estimate is spending money blind",
		},
		{
			Todo: "M24-019", Phase: PhasePlanning, Kind: KindMeasure,
			Instruction:    "record the time from submission to the first plan",
			Measurement:    "time-to-first-plan",
			Expected:       "the elapsed time before the user has something to approve",
			FailureMeaning: "a long gap here is dead time the user cannot act on",
		},
		{
			Todo: "M24-020", Phase: PhasePlanning, Kind: KindObserve,
			Instruction: "inspect the plan's scope, expected files, validation, risk, and assumptions",
			Expected:    "all five are present and specific enough to disagree with",
			FailureMeaning: "a plan that cannot be disagreed with cannot be meaningfully " +
				"approved, which makes the approval a formality",
		},
		{
			Todo: "M24-021", Phase: PhasePlanning, Kind: KindObserve,
			Instruction: "inspect the P50 and P90 time, token, and cost estimates",
			Expected: "each is shown as a range, and unknown pricing is shown as unknown " +
				"rather than as zero",
			FailureMeaning: "an estimate presented as a single number reads as a promise, and " +
				"an unknown shown as zero understates spend exactly when it matters",
		},
		{
			Todo: "M24-022", Phase: PhasePlanning, Kind: KindObserve,
			Instruction: "inspect the fixed provider, model, effort, and policy version",
			Expected:    "all four are pinned and visible before approval",
			FailureMeaning: "without them the run is not reproducible and a comparison with " +
				"a baseline is not a comparison",
		},
		{
			Todo: "M24-023", Phase: PhasePlanning, Kind: KindAction,
			Instruction:    "set the frozen hard budget",
			Expected:       "the budget is recorded and visible during execution",
			FailureMeaning: "an unbudgeted run cannot demonstrate the budget behaviour at all",
		},
		{
			Todo: "M24-024", Phase: PhasePlanning, Kind: KindAction,
			Instruction: "approve or redirect exactly as the benchmark script says",
			Expected:    "the decision is recorded with its attribution",
			FailureMeaning: "an evaluator improvising here is measuring their own judgement " +
				"rather than the product",
		},
		{
			Todo: "M24-025", Phase: PhasePlanning, Kind: KindObserve,
			Instruction: "verify the plan revision and the approval appear in replay from the database",
			Expected:    "replaying the session reconstructs both, with the same revisions",
			FailureMeaning: "a decision that does not survive replay is a decision the system " +
				"cannot later account for",
		},
	}
}

func executionSteps() []Step {
	return []Step{
		{
			Todo: "M24-026", Phase: PhaseExecution, Kind: KindAction,
			Instruction: "start the task", Expected: "the task moves to running and says so",
			FailureMeaning: "a task whose state is unclear at the start is unclear throughout",
		},
		{
			Todo: "M24-027", Phase: PhaseExecution, Kind: KindObserve,
			Instruction: "verify an isolated worktree was created",
			Expected:    "the working checkout is untouched and the task has its own worktree",
			FailureMeaning: "an agent editing the user's checkout can destroy work the user " +
				"had open and had not saved",
		},
		{
			Todo: "M24-028", Phase: PhaseExecution, Kind: KindObserve,
			Instruction:    "verify the execution graph highlights the active path",
			Expected:       "the current work is identifiable in the graph without hunting",
			FailureMeaning: "a graph that does not show where the work is now is decoration",
		},
		{
			Todo: "M24-029", Phase: PhaseExecution, Kind: KindObserve,
			Instruction: "verify tool output stays summarized unless expanded",
			Expected:    "the timeline remains readable while a noisy command runs",
			FailureMeaning: "a timeline flooded by raw output hides the decisions the user " +
				"actually has to make",
		},
		{
			Todo: "M24-030", Phase: PhaseExecution, Kind: KindAction,
			Instruction:    "exercise at least one permission request",
			Expected:       "the request names the exact action, its scope, and its consequence",
			FailureMeaning: "a vague approval prompt trains users to approve without reading",
		},
		{
			Todo: "M24-031", Phase: PhaseExecution, Kind: KindAction,
			Instruction: "allow once or deny exactly as the script says, then observe what follows",
			Expected: "an allow-once grant is not reused, and a denial is not retried through " +
				"another tool",
			FailureMeaning: "either failure makes the permission model theatre rather than a " +
				"control",
		},
		{
			Todo: "M24-032", Phase: PhaseExecution, Kind: KindObserve,
			Instruction:    "watch the cost and budget while the task runs",
			Expected:       "both update as work proceeds, and unknown cost stays unknown",
			FailureMeaning: "a user who cannot see spend accumulating cannot stop it in time",
		},
		{
			Todo: "M24-033", Phase: PhaseExecution, Kind: KindAction,
			Instruction: "pause the task at the scripted point",
			Expected:    "the task pauses at a safe point rather than mid-effect",
			FailureMeaning: "a pause that interrupts an in-flight effect creates the exact " +
				"ambiguity the system exists to avoid",
		},
		{
			Todo: "M24-034", Phase: PhaseExecution, Kind: KindAction,
			Instruction:    "restart CodeFlux entirely while the task is paused",
			Expected:       "the task is still there afterwards",
			FailureMeaning: "state that does not survive a restart is state the user cannot rely on",
		},
		{
			Todo: "M24-035", Phase: PhaseExecution, Kind: KindObserve,
			Instruction: "verify replay reconstructs the exact task state after the restart",
			Expected: "the timeline, the task state, the revision, and the cost all match " +
				"what was on screen before",
			FailureMeaning: "a reconstruction that differs from what the user saw means one of " +
				"the two was wrong, and they cannot tell which",
		},
		{
			Todo: "M24-036", Phase: PhaseExecution, Kind: KindAction,
			Instruction:    "resume from the validated checkpoint",
			Expected:       "work continues from the checkpoint without repeating an external effect",
			FailureMeaning: "a resume that repeats an effect can charge, deploy, or message twice",
		},
		{
			Todo: "M24-037", Phase: PhaseExecution, Kind: KindMeasure,
			Instruction: "record the time from starting the task to the first diff",
			Measurement: "time-to-first-diff",
			Expected:    "the elapsed time before the user can see actual work",
			FailureMeaning: "a long silence before the first change is when users decide the " +
				"tool is not working",
		},
		{
			Todo: "M24-038", Phase: PhaseExecution, Kind: KindMeasure,
			Instruction: "record every unexpected tool call, retry, loop, and user intervention",
			Measurement: "unexpected-events",
			Expected:    "a count with a short description of each, recorded as it happens",
			FailureMeaning: "these are the events an aggregate success rate hides, and they " +
				"are usually the reason a user stops trusting the tool",
		},
	}
}

func validationSteps() []Step {
	return []Step{
		{
			Todo: "M24-039", Phase: PhaseValidation, Kind: KindObserve,
			Instruction: "verify the required validations were selected, and why",
			Expected:    "the selection is explainable and tied to what changed",
			FailureMeaning: "validation chosen by habit rather than by the change proves " +
				"nothing about the change",
		},
		{
			Todo: "M24-040", Phase: PhaseValidation, Kind: KindObserve,
			Instruction: "verify validation ran against the exact current diff",
			Expected:    "the evidence names the diff identity it was produced against",
			FailureMeaning: "evidence from an earlier diff is stale evidence presented as " +
				"current, which is worse than no evidence",
		},
		{
			Todo: "M24-041", Phase: PhaseValidation, Kind: KindObserve,
			Instruction:    "inspect the Program graph mode",
			Expected:       "it shows structure, and does not claim to establish correctness",
			FailureMeaning: "a graph presented as proof invites a user to skip the evidence",
		},
		{
			Todo: "M24-042", Phase: PhaseValidation, Kind: KindObserve,
			Instruction: "inspect the Execution graph mode",
			Expected:    "it shows what happened, in order, with its causes",
			FailureMeaning: "an execution view that cannot answer 'why did this happen' is " +
				"not an explanation",
		},
		{
			Todo: "M24-043", Phase: PhaseValidation, Kind: KindObserve,
			Instruction: "inspect the Evidence graph mode",
			Expected: "each claim is linked to the evidence behind it, and unbacked claims " +
				"are visibly unbacked",
			FailureMeaning: "a claim shown beside evidence it does not have is the most " +
				"dangerous thing this product could do",
		},
		{
			Todo: "M24-044", Phase: PhaseValidation, Kind: KindAction,
			Instruction:    "select a graph node and confirm the related chat is highlighted",
			Expected:       "the two views agree about what is selected",
			FailureMeaning: "two views disagreeing about the selection make both untrustworthy",
		},
		{
			Todo: "M24-045", Phase: PhaseValidation, Kind: KindAction,
			Instruction:    "select a chat node chip and confirm the graph focuses",
			Expected:       "selection works in both directions",
			FailureMeaning: "one-directional linking makes the graph a dead end",
		},
		{
			Todo: "M24-046", Phase: PhaseValidation, Kind: KindObserve,
			Instruction: "inspect the changed-file list and the diff summary",
			Expected:    "every changed file is listed before acceptance is offered",
			FailureMeaning: "accepting a change over an unlisted file is accepting something " +
				"unreviewed",
		},
		{
			Todo: "M24-047", Phase: PhaseValidation, Kind: KindAction,
			Instruction: "open one changed source location in the external editor",
			Expected:    "the editor opens at the exact line, inside the repository",
			FailureMeaning: "an editor jump that lands in the wrong place, or outside the " +
				"repository, is both useless and unsafe",
		},
		{
			Todo: "M24-048", Phase: PhaseValidation, Kind: KindObserve,
			Instruction: "inspect every check: required, passed, failed, skipped, waived, unavailable",
			Expected:    "each state is distinguishable, and skipped is never presented as passed",
			FailureMeaning: "collapsing these states is how an incomplete verification comes " +
				"to look like a complete one",
		},
		{
			Todo: "M24-049", Phase: PhaseValidation, Kind: KindObserve,
			Instruction: "inspect the risk, approvals, model and tool versions, assumptions, and limitations",
			Expected:    "all are present on the final report",
			FailureMeaning: "a report without them cannot be audited later, when the question " +
				"actually gets asked",
		},
		{
			Todo: "M24-050", Phase: PhaseValidation, Kind: KindMeasure,
			Instruction: "compare the forecast with the actual time, tokens, and cost",
			Measurement: "forecast-error",
			Expected:    "the difference in each dimension, with unknowns preserved as unknown",
			FailureMeaning: "a forecast nobody compares against reality never improves, and " +
				"never earns the trust it asks for",
		},
		{
			Todo: "M24-051", Phase: PhaseValidation, Kind: KindAction,
			Instruction: "accept, repair, or roll back exactly as the benchmark result dictates",
			Expected:    "the decision and its attribution are recorded",
			FailureMeaning: "an unattributed final decision leaves nobody accountable for what " +
				"reached the branch",
		},
	}
}

// ValidateJourney checks the journey covers M24-010..051 exactly once each.
func ValidateJourney() error {
	steps := Steps()
	todos := map[string]bool{}
	for _, step := range steps {
		if err := step.Validate(); err != nil {
			return err
		}
		if todos[step.Todo] {
			return fmt.Errorf("%s is claimed twice", step.Todo)
		}
		todos[step.Todo] = true
	}
	for number := 10; number <= 51; number++ {
		todo := fmt.Sprintf("M24-%03d", number)
		if !todos[todo] {
			return fmt.Errorf("no journey step claims %s", todo)
		}
	}
	if len(steps) != 42 {
		return fmt.Errorf("M24-010..051 is 42 steps, the journey declares %d", len(steps))
	}
	// Phases must appear in order: a journey that measured time-to-first-diff
	// before the plan was approved would be measuring something else.
	lastPhase := -1
	for _, step := range steps {
		index := slices.Index(AllPhases(), step.Phase)
		if index < lastPhase {
			return fmt.Errorf("%s is in phase %q, which comes before the previous step's phase",
				step.Todo, step.Phase)
		}
		lastPhase = index
	}
	return nil
}

// StepsFor returns one phase's steps.
func StepsFor(phase Phase) []Step {
	var steps []Step
	for _, step := range Steps() {
		if step.Phase == phase {
			steps = append(steps, step)
		}
	}
	return steps
}

// Measurements returns every named measurement the journey records.
func Measurements() []string {
	var names []string
	for _, step := range Steps() {
		if step.Kind == KindMeasure {
			names = append(names, step.Measurement)
		}
	}
	sort.Strings(names)
	return names
}

// Outcome is what an evaluator records for one step.
type Outcome struct {
	Todo string
	// Passed is whether the expectation held.
	Passed bool
	// Note is the evaluator's observation. It is required for a failure: a
	// failed step with no note cannot be acted on afterwards.
	Note string
	// Value is the recorded measurement, for KindMeasure steps.
	Value time.Duration
	// Count is the recorded count, where a duration does not apply.
	Count int
}

// Run is one complete walked journey.
type Run struct {
	Outcomes []Outcome
}

// Validate rejects a run that could not support a conclusion.
func (run Run) Validate() error {
	if err := ValidateJourney(); err != nil {
		return err
	}
	byTodo := map[string]Outcome{}
	for _, outcome := range run.Outcomes {
		if _, duplicate := byTodo[outcome.Todo]; duplicate {
			return fmt.Errorf("%s was recorded twice", outcome.Todo)
		}
		byTodo[outcome.Todo] = outcome
	}
	for _, step := range Steps() {
		outcome, ok := byTodo[step.Todo]
		if !ok {
			return fmt.Errorf("%s was not walked", step.Todo)
		}
		if !outcome.Passed && strings.TrimSpace(outcome.Note) == "" {
			return fmt.Errorf(
				"%s failed with no note; a failure nobody described cannot be acted on",
				step.Todo)
		}
		if step.Kind == KindMeasure && outcome.Passed &&
			outcome.Value <= 0 && outcome.Count <= 0 {
			return fmt.Errorf("%s is a measurement that recorded no value", step.Todo)
		}
	}
	return nil
}

// Failures returns the steps that did not hold.
func (run Run) Failures() []Outcome {
	var failures []Outcome
	for _, outcome := range run.Outcomes {
		if !outcome.Passed {
			failures = append(failures, outcome)
		}
	}
	sort.Slice(failures, func(left, right int) bool {
		return failures[left].Todo < failures[right].Todo
	})
	return failures
}

// Measurement returns one recorded value.
func (run Run) Measurement(name string) (Outcome, error) {
	for _, step := range Steps() {
		if step.Kind != KindMeasure || step.Measurement != name {
			continue
		}
		for _, outcome := range run.Outcomes {
			if outcome.Todo == step.Todo {
				return outcome, nil
			}
		}
		return Outcome{}, fmt.Errorf("%s was not recorded", step.Todo)
	}
	return Outcome{}, errors.New("no journey step records " + name)
}
