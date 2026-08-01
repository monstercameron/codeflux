package frontendtest

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestM22JourneyMatrixIsCompleteAndWellFormed runs without a browser, so the
// coverage claim itself is checked on every build rather than only when
// Chromium happens to be available.
func TestM22JourneyMatrixIsCompleteAndWellFormed(t *testing.T) {
	cases := M22JourneyMatrix()
	if err := ValidateM22JourneyMatrix(cases); err != nil {
		t.Fatalf("M22 journey matrix is invalid: %v", err)
	}
	if len(cases) != 13 {
		t.Fatalf("M22-063..075 is 13 journeys, matrix declares %d", len(cases))
	}

	flows, access := 0, 0
	for _, testCase := range cases {
		switch testCase.Kind {
		case JourneyFlow:
			flows++
		case JourneyAccess:
			access++
		}
	}
	if flows != 9 {
		t.Fatalf("M22-063..071 is 9 flows, matrix declares %d", flows)
	}
	if access != 4 {
		t.Fatalf("M22-072..075 is 4 accessibility journeys, matrix declares %d", access)
	}
}

// TestM22JourneyMatrixRejectsUnusableCases proves the validator is load-bearing
// rather than a function that returns nil.
func TestM22JourneyMatrixRejectsUnusableCases(t *testing.T) {
	corruptions := map[string]func([]M22JourneyCase) []M22JourneyCase{
		"empty": func([]M22JourneyCase) []M22JourneyCase { return nil },
		"missing todo coverage": func(cases []M22JourneyCase) []M22JourneyCase {
			return cases[1:]
		},
		"duplicate id": func(cases []M22JourneyCase) []M22JourneyCase {
			cases[1].ID = cases[0].ID
			return cases
		},
		"two cases claim one todo": func(cases []M22JourneyCase) []M22JourneyCase {
			cases[1].Todo = cases[0].Todo
			return cases
		},
		"case names no stage": func(cases []M22JourneyCase) []M22JourneyCase {
			cases[0].Stages = nil
			return cases
		},
		"case names an unknown stage": func(cases []M22JourneyCase) []M22JourneyCase {
			cases[0].Stages = []string{"a-stage-the-fixture-does-not-have"}
			return cases
		},
		"case drops a plan reference": func(cases []M22JourneyCase) []M22JourneyCase {
			cases[0].PlanSections = []string{PlanBrowserAcceptance}
			return cases
		},
		"case has no adversary": func(cases []M22JourneyCase) []M22JourneyCase {
			cases[0].Adversary = ""
			return cases
		},
		"case cites a foreign todo": func(cases []M22JourneyCase) []M22JourneyCase {
			cases[0].Todo = "M16-046"
			return cases
		},
		"case has an unknown kind": func(cases []M22JourneyCase) []M22JourneyCase {
			cases[0].Kind = JourneyKind("something-else")
			return cases
		},
	}
	for name, corrupt := range corruptions {
		t.Run(name, func(t *testing.T) {
			if err := ValidateM22JourneyMatrix(corrupt(M22JourneyMatrix())); err == nil {
				t.Fatalf("validator accepted a matrix with: %s", name)
			}
		})
	}
}

// TestM22JourneyStagesExistInTheMountedFixture binds the declared stage names
// to the fixture that actually renders them. Without this the matrix could
// drift into naming stages nobody can drive.
// The fixture is a separate wasm main package and cannot be imported, so the
// binding is made against its source: each stage must appear both as a
// declared journey stage and as a rendered switch case.
func TestM22JourneyStagesExistInTheMountedFixture(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("renderfixture", "main.go"))
	if err != nil {
		t.Fatalf("read mounted journey fixture: %v", err)
	}
	fixture := string(source)

	declared := make([]string, 0, len(M22JourneyStages))
	for _, stage := range M22JourneyStages {
		if !strings.Contains(fixture, `{ID: "`+stage+`"`) {
			t.Fatalf("M22 stage %q is not declared by the mounted journey fixture", stage)
		}
		if !strings.Contains(fixture, `case "`+stage+`":`) {
			t.Fatalf("M22 stage %q is declared but never rendered by the mounted journey fixture", stage)
		}
		declared = append(declared, stage)
	}
	if len(declared) != len(M22JourneyStages) {
		t.Fatalf("resolved %d of %d stages", len(declared), len(M22JourneyStages))
	}
	// And every fixture stage is claimed by at least one journey, so a stage
	// cannot be added to the fixture and then go unexercised.
	claimed := map[string]bool{}
	for _, testCase := range M22JourneyMatrix() {
		for _, stage := range testCase.Stages {
			claimed[stage] = true
		}
	}
	for _, stage := range declared {
		if !claimed[stage] {
			t.Fatalf("mounted fixture stage %q is not claimed by any M22 journey", stage)
		}
	}
}

// TestM22JourneyTodoLookupIsExact guards the accessor the mounted checks use to
// find the stages they must visit.
func TestM22JourneyTodoLookupIsExact(t *testing.T) {
	stages := M22JourneyStagesForTodo("M22-069")
	if !slices.Equal(stages, []string{"graph"}) {
		t.Fatalf("M22-069 stages = %v, want [graph]", stages)
	}
	if got := M22JourneyStagesForTodo("M22-999"); got != nil {
		t.Fatalf("unknown TODO returned %v, want nil", got)
	}
	// An accessibility journey covers the whole surface.
	if got := M22JourneyStagesForTodo("M22-072"); len(got) != len(M22JourneyStages) {
		t.Fatalf("M22-072 covers %d stages, want all %d", len(got), len(M22JourneyStages))
	}
	for _, testCase := range M22JourneyMatrix() {
		if strings.TrimSpace(testCase.Expected) == "" {
			t.Fatalf("case %q has no expected outcome", testCase.ID)
		}
	}
}
