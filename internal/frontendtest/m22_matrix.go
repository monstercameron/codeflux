package frontendtest

import (
	"fmt"
	"slices"
	"strings"
)

const (
	// PlanBrowserAcceptance is the plan authority for what a browser journey
	// must establish before it counts as evidence.
	PlanBrowserAcceptance = "docs/plan.md section 27: Browser Acceptance"
	// PlanAccessibilityBaseline is the plan authority for the accessibility
	// floor every route and state must meet.
	PlanAccessibilityBaseline = "docs/plan.md section 27B: Accessibility Baseline"
)

// JourneyKind separates the two things M22-063..075 asks for. They are
// reported apart because a passing journey says nothing about whether the
// journey was reachable without a mouse, and a passing accessibility scan says
// nothing about whether the flow it scanned actually works.
type JourneyKind string

const (
	// JourneyFlow is a user-visible sequence that must complete.
	JourneyFlow JourneyKind = "flow"
	// JourneyAccess is a property the whole surface must hold regardless of
	// which flow is running.
	JourneyAccess JourneyKind = "accessibility"
)

// M22JourneyCase is one independently reportable browser journey.
//
// Stages names the mounted fixture stages the journey drives, so a journey
// cannot claim coverage of a flow whose surface it never visited: the mounted
// checks assert against exactly these, and ValidateM22JourneyMatrix rejects a
// flow case that lists none.
type M22JourneyCase struct {
	ID           string
	Kind         JourneyKind
	Todo         string
	Stages       []string
	Gates        []string
	PlanSections []string
	Adversary    string
	Expected     string
}

// M22JourneyStages are the mounted journey fixture stages a case may cite.
var M22JourneyStages = []string{
	"first-run", "new-task", "plan-review", "live-work", "approval",
	"review", "repair", "reconnect", "recovery", "graph", "budget",
}

// M22JourneyMatrix declares the M22-063..075 browser and accessibility
// coverage. Each case names exactly one TODO so a failure identifies the
// requirement it falsifies without a lookup.
func M22JourneyMatrix() []M22JourneyCase {
	return []M22JourneyCase{
		m22Flow(
			"empty-shell-journey", "M22-063",
			[]string{"first-run"},
			[]string{"G03", "G05"},
			"load the shell with no thread, no task, and no selection, then reload it directly",
			"the empty shell names what it is waiting for and offers a first action instead of rendering an ambiguous blank frame",
		),
		m22Flow(
			"create-thread-and-send-message", "M22-064",
			[]string{"first-run", "new-task"},
			[]string{"G01", "G05"},
			"create a thread, type a requirement, and send it while watching for draft loss",
			"the requirement becomes a durable timeline card, the composer clears only after the send is accepted, and the draft survives until then",
		),
		m22Flow(
			"plan-approval", "M22-065",
			[]string{"plan-review"},
			[]string{"G02", "G05"},
			"read the pending plan, then approve it and re-open the resolved card",
			"the plan states scope, steps, risks, and what authority it needs; approval is explicit; the resolved card keeps attribution and offers no second approval",
		),
		m22Flow(
			"command-approval-and-denial", "M22-066",
			[]string{"approval"},
			[]string{"G02", "G05"},
			"grant one command approval and deny another, then attempt the denied capability again",
			"each decision names the exact action and its consequences, resolution is attributed, and a denial is not re-offered as an automatic action",
		),
		m22Flow(
			"pause-and-resume", "M22-067",
			[]string{"live-work", "budget"},
			[]string{"G01", "G05"},
			"pause running work, inspect what stopped, then resume it",
			"pause is reachable while running, the paused surface states what is and is not still in flight, and resume is the named next action",
		),
		m22Flow(
			"reconnect-and-replay", "M22-068",
			[]string{"reconnect", "live-work"},
			[]string{"G01", "G05"},
			"lose the transport mid-task, replay on reconnect, and attempt a mutation while the sequence is uncertain",
			"the timeline stays readable, mutations stay disabled with a stated reason until the cursor matches, and replay adds no duplicate card",
		),
		m22Flow(
			"graph-message-cross-selection", "M22-069",
			[]string{"graph"},
			[]string{"G04", "G05"},
			"select a graph node, follow it to its timeline card, and select back the other way",
			"selection is bidirectional and single-valued, and the graph never presents itself as authority for a change",
		),
		m22Flow(
			"diff-review-and-acceptance", "M22-070",
			[]string{"review"},
			[]string{"G02", "G04", "G05"},
			"open the diff, inspect the changed files, and accept the review",
			"every changed file is listed with its category before acceptance is offered, and acceptance is an explicit attributable act",
		),
		m22Flow(
			"crash-recovery-choice", "M22-071",
			[]string{"recovery"},
			[]string{"G01", "G05"},
			"return to a task that needs recovery and inspect each offered choice",
			"known state and ambiguity are stated separately, unsafe retry is absent, and only classified safe choices are actionable",
		),
		m22Access(
			"accessibility-scan-every-route-state", "M22-072",
			[]string{"G02", "G05"},
			"scan every journey stage for missing accessible names, unlabelled landmarks, and controls that expose state only through color",
			"every interactive control has an accessible name, every region is labelled, and no state is conveyed by color alone",
		),
		m22Access(
			"keyboard-only-journey", "M22-073",
			[]string{"G02", "G05"},
			"complete a full task journey using only the keyboard, including every overlay and every stage change",
			"focus is always visible, order follows the visible layout, no control is mouse-only, and focus returns to a sensible anchor after every overlay closes",
		),
		m22Access(
			"screen-reader-smoke", "M22-074",
			[]string{"G02", "G04", "G05"},
			"traverse the journey by accessible name and role alone, without relying on visual position",
			"state, cost, authority, evidence, uncertainty, and next action are each reachable as named text rather than as layout",
		),
		m22Access(
			"reduced-motion-and-high-contrast", "M22-075",
			[]string{"G02", "G05"},
			"run the journey with reduced motion and again with high contrast",
			"no journey step depends on an animation to be understood, and every state distinction survives the high-contrast palette",
		),
	}
}

func m22Flow(id, todo string, stages, gates []string, adversary, expected string) M22JourneyCase {
	return m22Case(id, JourneyFlow, todo, stages, gates, adversary, expected)
}

func m22Access(id, todo string, gates []string, adversary, expected string) M22JourneyCase {
	// An accessibility case applies to every stage by construction; naming a
	// subset would let a regression hide in the stages it skipped.
	return m22Case(id, JourneyAccess, todo, M22JourneyStages, gates, adversary, expected)
}

func m22Case(
	id string,
	kind JourneyKind,
	todo string,
	stages []string,
	gates []string,
	adversary string,
	expected string,
) M22JourneyCase {
	return M22JourneyCase{
		ID: id, Kind: kind, Todo: todo,
		Stages: slices.Clone(stages), Gates: slices.Clone(gates),
		PlanSections: []string{PlanBrowserAcceptance, PlanAccessibilityBaseline},
		Adversary:    adversary, Expected: expected,
	}
}

// ValidateM22JourneyMatrix rejects a matrix that cannot serve as evidence:
// missing TODO coverage, a case that names no stage, or a stage name the
// mounted fixture does not actually have.
func ValidateM22JourneyMatrix(cases []M22JourneyCase) error {
	if len(cases) == 0 {
		return fmt.Errorf("M22 journey matrix is empty")
	}
	ids := make(map[string]struct{}, len(cases))
	todos := make(map[string]string, len(cases))
	gates := make(map[string]struct{})
	for index, testCase := range cases {
		if strings.TrimSpace(testCase.ID) == "" ||
			strings.TrimSpace(testCase.Adversary) == "" ||
			strings.TrimSpace(testCase.Expected) == "" {
			return fmt.Errorf("M22 journey case %d is incomplete", index)
		}
		if testCase.Kind != JourneyFlow && testCase.Kind != JourneyAccess {
			return fmt.Errorf("M22 journey case %q has unknown kind %q", testCase.ID, testCase.Kind)
		}
		if _, duplicate := ids[testCase.ID]; duplicate {
			return fmt.Errorf("duplicate M22 journey case id %q", testCase.ID)
		}
		ids[testCase.ID] = struct{}{}
		if !strings.HasPrefix(testCase.Todo, "M22-") {
			return fmt.Errorf("M22 journey case %q cites %q, want an M22 TODO", testCase.ID, testCase.Todo)
		}
		if other, clash := todos[testCase.Todo]; clash {
			return fmt.Errorf("cases %q and %q both claim %s", other, testCase.ID, testCase.Todo)
		}
		todos[testCase.Todo] = testCase.ID
		if len(testCase.Stages) == 0 {
			return fmt.Errorf("M22 journey case %q names no fixture stage", testCase.ID)
		}
		for _, stage := range testCase.Stages {
			if !slices.Contains(M22JourneyStages, stage) {
				return fmt.Errorf("M22 journey case %q names unknown stage %q", testCase.ID, stage)
			}
		}
		for _, plan := range []string{PlanBrowserAcceptance, PlanAccessibilityBaseline} {
			if !slices.Contains(testCase.PlanSections, plan) {
				return fmt.Errorf("M22 journey case %q omits plan reference %q", testCase.ID, plan)
			}
		}
		for _, gate := range testCase.Gates {
			gates[gate] = struct{}{}
		}
	}
	for _, todo := range requiredRange("M22", 63, 75) {
		if _, ok := todos[todo]; !ok {
			return fmt.Errorf("M22 journey matrix omits %s", todo)
		}
	}
	for _, gate := range []string{"G01", "G02", "G03", "G04", "G05"} {
		if _, ok := gates[gate]; !ok {
			return fmt.Errorf("M22 journey matrix omits feasible %s", gate)
		}
	}
	return nil
}

// M22JourneyStagesForTodo returns the fixture stages one TODO's case drives.
func M22JourneyStagesForTodo(todo string) []string {
	for _, testCase := range M22JourneyMatrix() {
		if testCase.Todo == todo {
			return slices.Clone(testCase.Stages)
		}
	}
	return nil
}
