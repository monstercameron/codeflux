package frontendtest

import "testing"

func TestM17AcceptanceMatrixIsCompleteAndDeterministic(t *testing.T) {
	first := M17AcceptanceMatrix()
	second := M17AcceptanceMatrix()
	if err := ValidateM17AcceptanceMatrix(first); err != nil {
		t.Fatal(err)
	}
	if len(first) != 5 {
		t.Fatalf("case count = %d, want 5", len(first))
	}
	for index := range first {
		if first[index].ID != second[index].ID {
			t.Fatalf("matrix order changed at %d: %q != %q", index, first[index].ID, second[index].ID)
		}
	}
}

func TestValidateM17AcceptanceMatrixRejectsLostTodoAndPlanReference(t *testing.T) {
	cases := M17AcceptanceMatrix()
	cases[0].Todos = cases[0].Todos[1:]
	if err := ValidateM17AcceptanceMatrix(cases); err == nil {
		t.Fatal("missing M17-001 was accepted")
	}

	cases = M17AcceptanceMatrix()
	cases[0].PlanSections = cases[0].PlanSections[1:]
	if err := ValidateM17AcceptanceMatrix(cases); err == nil {
		t.Fatal("missing M17 plan reference was accepted")
	}
}
