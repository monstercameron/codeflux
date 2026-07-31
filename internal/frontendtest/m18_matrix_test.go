package frontendtest

import "testing"

func TestM18AcceptanceMatrixCoversRequestedBrowserContracts(t *testing.T) {
	first := M18AcceptanceMatrix()
	second := M18AcceptanceMatrix()
	if err := ValidateM18AcceptanceMatrix(first); err != nil {
		t.Fatal(err)
	}
	if len(first) != 5 {
		t.Fatalf("case count = %d, want 5", len(first))
	}
	for index := range first {
		if first[index].ID != second[index].ID {
			t.Fatalf("matrix order changed at %d", index)
		}
	}
}

func TestValidateM18AcceptanceMatrixRejectsMissingTodoGateAndPlan(t *testing.T) {
	cases := M18AcceptanceMatrix()
	cases[0].Todos = cases[0].Todos[1:]
	if err := ValidateM18AcceptanceMatrix(cases); err == nil {
		t.Fatal("missing M18-001 was accepted")
	}
	cases = M18AcceptanceMatrix()
	for index := range cases {
		for gateIndex, gate := range cases[index].Gates {
			if gate == "G01" {
				cases[index].Gates = append(
					cases[index].Gates[:gateIndex],
					cases[index].Gates[gateIndex+1:]...,
				)
				break
			}
		}
	}
	if err := ValidateM18AcceptanceMatrix(cases); err == nil {
		t.Fatal("missing feasible G01 was accepted")
	}
	cases = M18AcceptanceMatrix()
	for index := range cases {
		for gateIndex := len(cases[index].Gates) - 1; gateIndex >= 0; gateIndex-- {
			if cases[index].Gates[gateIndex] == "G03" {
				cases[index].Gates = append(
					cases[index].Gates[:gateIndex],
					cases[index].Gates[gateIndex+1:]...,
				)
			}
		}
	}
	if err := ValidateM18AcceptanceMatrix(cases); err == nil {
		t.Fatal("missing stale-approval G03 was accepted")
	}
	cases = M18AcceptanceMatrix()
	cases[2].PlanSections = cases[2].PlanSections[1:]
	if err := ValidateM18AcceptanceMatrix(cases); err == nil {
		t.Fatal("missing plan reference was accepted")
	}
}
