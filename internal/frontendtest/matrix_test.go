package frontendtest

import (
	"slices"
	"testing"
)

func TestAcceptanceMatrixIsCompleteAndDeterministic(t *testing.T) {
	first := AcceptanceMatrix()
	second := AcceptanceMatrix()
	if err := ValidateAcceptanceMatrix(first); err != nil {
		t.Fatal(err)
	}
	if len(first) != len(Routes)*len(BootstrapStates)*len(Viewports)+8 {
		t.Fatalf("case count = %d", len(first))
	}
	for index := range first {
		if first[index].ID != second[index].ID {
			t.Fatalf("matrix order changed at %d: %q != %q", index, first[index].ID, second[index].ID)
		}
	}
}

func TestAcceptanceMatrixCrossProductHasEveryRouteStateAndViewport(t *testing.T) {
	cases := AcceptanceMatrix()
	for _, route := range Routes {
		for _, state := range BootstrapStates {
			for _, viewport := range Viewports {
				want := matrixID(route, state, viewport.Name)
				if !slices.ContainsFunc(cases, func(testCase AcceptanceCase) bool {
					return testCase.ID == want
				}) {
					t.Errorf("missing %s", want)
				}
			}
		}
	}
}

func TestValidateAcceptanceMatrixRejectsLostPlanReference(t *testing.T) {
	cases := AcceptanceMatrix()
	cases[0].PlanSections = []string{PlanResponsive}
	if err := ValidateAcceptanceMatrix(cases); err == nil {
		t.Fatal("missing plan acceptance section was accepted")
	}
}

func TestViewportBoundariesExerciseBothSides(t *testing.T) {
	want := map[string]int{
		"wide":     1600,
		"standard": 1280,
		"compact":  960,
		"minimum":  720,
	}
	for _, viewport := range Viewports {
		if width := want[viewport.Mode]; width != viewport.Width {
			t.Errorf("%s width = %d, want %d", viewport.Mode, viewport.Width, width)
		}
	}
}

func TestEveryRouteHasAPlannedHeading(t *testing.T) {
	for _, route := range Routes {
		if RouteHeadings[route] == "" {
			t.Errorf("route %q has no planned heading", route)
		}
	}
	if len(RouteHeadings) != len(Routes) {
		t.Fatalf("route headings=%d routes=%d", len(RouteHeadings), len(Routes))
	}
}
