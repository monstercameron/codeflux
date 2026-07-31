package frontendtest

import (
	"fmt"
	"slices"
	"strings"
)

const (
	PlanConversationModel = "docs/plan.md §27A Conversation Model"
	PlanTimelineContracts = "docs/plan.md §27C Timeline Contracts"
	PlanComposerContract  = "docs/plan.md §27C Composer Contract"
	PlanHumanIntent       = "docs/plan.md §5 Human Intent"
)

// M17AcceptanceCase is one independently reportable browser or reducer
// contract for the thread rail, timeline, and composer milestone.
type M17AcceptanceCase struct {
	ID           string   `json:"id"`
	Surface      string   `json:"surface"`
	Todos        []string `json:"todos"`
	Gates        []string `json:"gates"`
	PlanSections []string `json:"plan_sections"`
	Adversary    string   `json:"adversary"`
	Expected     string   `json:"expected"`
}

// M17AcceptanceMatrix makes the milestone's cross-cutting acceptance boundary
// explicit. Package-level fixtures provide exhaustive reducer permutations;
// these cases define the mounted interaction and failure combinations.
func M17AcceptanceMatrix() []M17AcceptanceCase {
	return []M17AcceptanceCase{
		m17Case(
			"thread-rail-resume-pagination",
			"thread-rail",
			append(requiredRange("M17", 1, 18), "M17-110"),
			[]string{"G01", "G04"},
			"restore a selected thread beyond page one while two pages overlap, an equal timestamp tie exists, and a pending create commits",
			"the canonical selection, row key, focus, stable order, and deduplicated cursor position survive reload and pending-row replacement",
		),
		m17Case(
			"timeline-pagination-replay-anchor",
			"timeline",
			append(requiredRange("M17", 19, 32), "M17-100"),
			[]string{"G01", "G04"},
			"prepend variable-height history while replay joins every page boundary and live events arrive above a deliberate scroll position",
			"durable sequence remains canonical, duplicates collapse, gaps are visible, the anchor is preserved, and live follow occurs only near the bottom",
		),
		m17Case(
			"message-presentation-hostile-content",
			"message",
			append(requiredRange("M17", 33, 47), "M17-092", "M17-101"),
			[]string{"G02", "G04"},
			"stream interrupted text containing executable markup, unsafe URL schemes, long code, and a stale graph-node revision",
			"one visibly provisional message becomes a durable or interrupted card, markup stays inert, links are safe, code remains readable, and graph identity remains inspectable",
		),
		m17Case(
			"typed-card-exhaustive-disclosure",
			"typed-cards",
			append(
				append(append(requiredRange("M17", 48, 70), requiredRange("M17", 90, 93)...), "M17-097"),
				requiredRange("M17", 98, 115)...,
			),
			[]string{"G02", "G03", "G04"},
			"render every durable event and status, double-activate approvals, paginate hostile redacted output, revise a plan, and interleave routine progress",
			"every kind has a distinct card or safe fallback, approvals resolve once without stealing focus, raw output stays collapsed and bounded, history is retained, and routine progress creates no assertive interruption",
		),
		m17Case(
			"composer-draft-retry-task-state",
			"composer",
			append(requiredRange("M17", 71, 89), requiredRange("M17", 94, 96)...),
			[]string{"G01", "G03", "G04"},
			"switch threads with multiline drafts, fail and retry a send, attach server identities, cycle every task state, and press Enter during IME composition",
			"drafts remain isolated, the same idempotency key is retained through retry, committed sends alone clear text, unsafe browser paths never enter requests, and Stop stays directly reachable while running",
		),
	}
}

func m17Case(
	id string,
	surface string,
	todos []string,
	gates []string,
	adversary string,
	expected string,
) M17AcceptanceCase {
	return M17AcceptanceCase{
		ID:      id,
		Surface: surface,
		Todos:   slices.Clone(todos),
		Gates:   slices.Clone(gates),
		PlanSections: []string{
			PlanConversationModel,
			PlanTimelineContracts,
			PlanComposerContract,
			PlanHumanIntent,
		},
		Adversary: adversary,
		Expected:  expected,
	}
}

// ValidateM17AcceptanceMatrix rejects incomplete milestone coverage before a
// browser run can be used as gate evidence.
func ValidateM17AcceptanceMatrix(cases []M17AcceptanceCase) error {
	if len(cases) == 0 {
		return fmt.Errorf("M17 acceptance matrix is empty")
	}
	ids := make(map[string]struct{}, len(cases))
	todos := make(map[string]struct{}, 115)
	gates := make(map[string]struct{}, 4)
	requiredPlans := []string{
		PlanConversationModel,
		PlanTimelineContracts,
		PlanComposerContract,
		PlanHumanIntent,
	}
	for index, testCase := range cases {
		if strings.TrimSpace(testCase.ID) == "" || strings.TrimSpace(testCase.Surface) == "" {
			return fmt.Errorf("M17 case %d lacks identity or surface", index)
		}
		if _, duplicate := ids[testCase.ID]; duplicate {
			return fmt.Errorf("duplicate M17 case id %q", testCase.ID)
		}
		ids[testCase.ID] = struct{}{}
		if strings.TrimSpace(testCase.Adversary) == "" || strings.TrimSpace(testCase.Expected) == "" {
			return fmt.Errorf("M17 case %q lacks adversary or expected outcome", testCase.ID)
		}
		for _, plan := range requiredPlans {
			if !slices.Contains(testCase.PlanSections, plan) {
				return fmt.Errorf("M17 case %q omits plan reference %q", testCase.ID, plan)
			}
		}
		for _, todo := range testCase.Todos {
			todos[todo] = struct{}{}
		}
		for _, gate := range testCase.Gates {
			gates[gate] = struct{}{}
		}
	}
	for value := 1; value <= 115; value++ {
		todo := fmt.Sprintf("M17-%03d", value)
		if _, ok := todos[todo]; !ok {
			return fmt.Errorf("M17 acceptance matrix omits %s", todo)
		}
	}
	for value := 1; value <= 4; value++ {
		gate := fmt.Sprintf("G%02d", value)
		if _, ok := gates[gate]; !ok {
			return fmt.Errorf("M17 acceptance matrix omits %s", gate)
		}
	}
	return nil
}
