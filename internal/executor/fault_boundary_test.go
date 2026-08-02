package executor

import (
	"errors"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/testfixtures"
)

// TestAUDIT027_AnInjectedFaultReachesTheRealCommandBoundary covers the command
// half of AUDIT-027, reconciling part of M22-036 through M22-050 and M22-G03.
//
// Fifteen fault points were declared and no production boundary consulted any
// of them: RunWithFault appeared only inside its own package's tests. So every
// crash and recovery claim rested on a ledger that never entered the code it
// described.
//
// This arms the real vocabulary and runs the real mediated executor.
func TestAUDIT027_AnInjectedFaultReachesTheRealCommandBoundary(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		point testfixtures.FaultPoint
	}{
		{"before the process exists", testfixtures.FaultWorkerDuringCommand},
		{"with the process running", testfixtures.FaultPoint(FaultPointAfterCommandStart)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			injector := testfixtures.NewFaultInjector()
			injector.Arm(testCase.point, 1, "audit-027 boundary check")

			_, err := ExecuteAuthorizedTool(t.Context(),
				faultBoundaryRequest(t, testfixtures.StringPointInjector{Injector: injector}))
			if !errors.Is(err, testfixtures.ErrInjectedFault) {
				t.Fatalf("execution returned %v, want the injected fault", err)
			}
			if injector.Fired(testCase.point) != 1 {
				t.Fatalf("the point fired %d times; the boundary was not reached",
					injector.Fired(testCase.point))
			}
			if injector.Remaining(testCase.point) != 0 {
				t.Error("the arm was not consumed, so the boundary did not check it")
			}
		})
	}
}

// TestAUDIT027_AnUnarmedInjectorChangesNothing proves wiring the check into a
// production path costs nothing when no fault is armed.
func TestAUDIT027_AnUnarmedInjectorChangesNothing(t *testing.T) {
	injector := testfixtures.NewFaultInjector()
	result, err := ExecuteAuthorizedTool(t.Context(),
		faultBoundaryRequest(t, testfixtures.StringPointInjector{Injector: injector}))
	if err != nil {
		t.Fatalf("an unarmed injector broke execution: %v", err)
	}
	if result.State != "succeeded" {
		t.Fatalf("state = %q, want succeeded", result.State)
	}
}

// TestAUDIT027_ANilInjectorIsTheProductionPath proves the default.
func TestAUDIT027_ANilInjectorIsTheProductionPath(t *testing.T) {
	request := faultBoundaryRequest(t, nil)
	if request.Faults != nil {
		t.Fatal("the production request carries an injector")
	}
	result, err := ExecuteAuthorizedTool(t.Context(), request)
	if err != nil {
		t.Fatalf("execution with no injector failed: %v", err)
	}
	if result.State != "succeeded" {
		t.Fatalf("state = %q, want succeeded", result.State)
	}
}

// TestAUDIT027_TheWiredPointsMatchTheDeclaredVocabulary keeps the boundary and
// the fault catalogue from drifting apart.
//
// A boundary consulting a point nobody can arm is a control that silently does
// nothing, which is the defect this ticket is about in a different shape.
func TestAUDIT027_TheWiredPointsMatchTheDeclaredVocabulary(t *testing.T) {
	declared := make(map[string]struct{})
	for _, point := range testfixtures.AllFaultPoints() {
		declared[string(point)] = struct{}{}
	}
	if _, ok := declared[FaultPointBeforeCommandStart]; !ok {
		t.Errorf("%q is consulted by the executor but is not a declared fault point",
			FaultPointBeforeCommandStart)
	}
}

// faultBoundaryRequest builds a real authorized request using the package's own
// fixtures, so the fault check is exercised on the same path every other
// command-execution test uses.
func faultBoundaryRequest(
	t *testing.T,
	faults FaultInjector,
) AuthorizedToolRequest {
	t.Helper()
	worktree := t.TempDir()
	request := commandToolRequest(t, worktree, "output", 20*time.Second)
	return AuthorizedToolRequest{
		Request:            request,
		Classification:     grantedClassification(t, request),
		WorktreePath:       worktree,
		Environment:        map[string]string{commandHelperMode: "output"},
		AllowedEnvironment: []string{commandHelperMode},
		Redactor:           commandTestRedactor(t),
		Faults:             faults,
	}
}
