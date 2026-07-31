package frontendtest

import (
	"fmt"
	"slices"
	"strings"
)

const (
	PlanUnifiedSessionStream  = "docs/plan.md section 27A: Unified Session Stream"
	PlanFrontendReducers      = "docs/plan.md section 27C: Frontend Stores and Reducers"
	PlanFrontendReconnectFlow = "docs/plan.md section 27C: Reconnect"
)

// M18AcceptanceCase is one browser-visible connection, action, or durable-flow
// contract tied to the authoritative plan and milestone TODOs.
type M18AcceptanceCase struct {
	ID           string
	Todos        []string
	Gates        []string
	PlanSections []string
	Adversary    string
	Expected     string
}

// M18AcceptanceMatrix declares the M18 connection/replay browser boundary.
func M18AcceptanceMatrix() []M18AcceptanceCase {
	return []M18AcceptanceCase{
		m18Case(
			"ordered-reconnect-duplicate-gap-repair",
			append(append(requiredRange("M18", 1, 15), "M18-052", "M18-054"), "M18-068"),
			[]string{"G01", "G05"},
			"disconnect before delivery, replay one event, redeliver it, skip a sequence, and attempt mutation before repair",
			"the last trusted cursor and draft survive, one card renders, the duplicate is counted, the gap requests snapshot repair, and mutations remain disabled until cursor-matched live state",
		),
		m18Case(
			"first-durable-thread-reconnect-journey",
			[]string{"M18-069"},
			[]string{"G01"},
			"open the first durable thread before transport loss and carry it through bounded reconnect, replay, and a second disconnect",
			"the same thread, draft, ordered card, task state, and budget presentation remain visible without duplicated content",
		),
		m18Case(
			"state-cost-authority-next-action-comprehension",
			[]string{"M18-070"},
			[]string{"G04", "G05"},
			"inspect disconnected, replaying, repair-required, and live presentations while actual price is unknown",
			"connection certainty, backend task state, unknown cost, hard budget, repair need, and the next safe enabled or disabled action are identifiable without raw logs",
		),
		m18Case(
			"schema-honest-recovery-presentation",
			requiredRange("M18", 43, 51),
			[]string{"G05"},
			"enter recovery-required with only task and plan revisions, then compare it with a fully typed recovery projection",
			"schema-backed facts render, absent checkpoint/divergence/classification facts stay explicitly unknown, unsafe retry is absent, and only classified safe choices become actionable",
		),
		m18Case(
			"complete-task-journey-and-stale-approval",
			[]string{"M18-069"},
			[]string{"G03", "G05"},
			"walk first-run, new task, plan, live, approval, review, repair, reconnect, recovery, graph, and budget stages, then resolve and revisit an approval",
			"every stage exposes state, cost, authority, evidence, uncertainty, and next action; a resolved or stale approval retains attribution but no mutation buttons; controls remain keyboard named and focus-safe",
		),
	}
}

func m18Case(
	id string,
	todos []string,
	gates []string,
	adversary string,
	expected string,
) M18AcceptanceCase {
	return M18AcceptanceCase{
		ID: id, Todos: slices.Clone(todos), Gates: slices.Clone(gates),
		PlanSections: []string{
			PlanUnifiedSessionStream,
			PlanFrontendReducers,
			PlanFrontendReconnectFlow,
		},
		Adversary: adversary, Expected: expected,
	}
}

// ValidateM18AcceptanceMatrix rejects missing requested coverage before a
// mounted browser run can be treated as M18 evidence.
func ValidateM18AcceptanceMatrix(cases []M18AcceptanceCase) error {
	if len(cases) == 0 {
		return fmt.Errorf("M18 acceptance matrix is empty")
	}
	ids := make(map[string]struct{}, len(cases))
	todos := make(map[string]struct{})
	gates := make(map[string]struct{})
	plans := []string{PlanUnifiedSessionStream, PlanFrontendReducers, PlanFrontendReconnectFlow}
	for index, testCase := range cases {
		if strings.TrimSpace(testCase.ID) == "" || strings.TrimSpace(testCase.Adversary) == "" ||
			strings.TrimSpace(testCase.Expected) == "" {
			return fmt.Errorf("M18 case %d is incomplete", index)
		}
		if _, duplicate := ids[testCase.ID]; duplicate {
			return fmt.Errorf("duplicate M18 case id %q", testCase.ID)
		}
		ids[testCase.ID] = struct{}{}
		for _, plan := range plans {
			if !slices.Contains(testCase.PlanSections, plan) {
				return fmt.Errorf("M18 case %q omits plan reference %q", testCase.ID, plan)
			}
		}
		for _, todo := range testCase.Todos {
			todos[todo] = struct{}{}
		}
		for _, gate := range testCase.Gates {
			gates[gate] = struct{}{}
		}
	}
	requiredTodos := append(requiredRange("M18", 1, 15), requiredRange("M18", 43, 51)...)
	requiredTodos = append(requiredTodos, "M18-052", "M18-054", "M18-068", "M18-069", "M18-070")
	for _, todo := range requiredTodos {
		if _, ok := todos[todo]; !ok {
			return fmt.Errorf("M18 acceptance matrix omits %s", todo)
		}
	}
	for _, gate := range []string{"G01", "G03", "G04", "G05"} {
		if _, ok := gates[gate]; !ok {
			return fmt.Errorf("M18 acceptance matrix omits feasible %s", gate)
		}
	}
	return nil
}
