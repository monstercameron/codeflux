package coordinator

import (
	"context"
	"errors"
	"testing"

	"codeflux.dev/codeflux/internal/workspace"
)

func TestUserWorktreeReconciliationRefusesAmbiguityAndUnsafeGit(t *testing.T) {
	valid := RecoveryAssessment{
		Classification: RecoveryReconcileRequired,
		Findings:       []RecoveryFinding{{Code: RecoveryFindingWorktreeHeadChanged}},
	}
	if err := validateUserWorktreeReconciliation(valid); err != nil {
		t.Fatalf("descendant-head assessment rejected: %v", err)
	}
	ambiguous := valid
	ambiguous.ActionsThatMustNotBeRepeated = []string{"external-action-1"}
	var refusal *RecoveryReconcileRefusal
	if err := validateUserWorktreeReconciliation(ambiguous); !errors.As(err, &refusal) || refusal.Reason != RecoveryReconcileAmbiguousEffects {
		t.Fatalf("ambiguous refusal = %#v, error=%v", refusal, err)
	}
	unsafe := valid
	unsafe.Findings = append(unsafe.Findings, RecoveryFinding{Code: RecoveryFindingGitOperationUnresolved})
	if err := validateUserWorktreeReconciliation(unsafe); !errors.As(err, &refusal) || refusal.Reason != RecoveryReconcileUnsafeFinding {
		t.Fatalf("unsafe refusal = %#v, error=%v", refusal, err)
	}

	runner := &recoveryAncestorRunner{}
	if err := verifyDescendantRecoveryHead(t.Context(), runner, `C:\worktree`, "old", "new"); err != nil {
		t.Fatal(err)
	}
	if runner.executable != "git" || len(runner.arguments) != 4 || runner.arguments[0] != "merge-base" ||
		runner.arguments[1] != "--is-ancestor" || runner.arguments[2] != "old" || runner.arguments[3] != "new" {
		t.Fatalf("ancestry command = %s %#v", runner.executable, runner.arguments)
	}
	runner.err = errors.New("not an ancestor")
	if err := verifyDescendantRecoveryHead(t.Context(), runner, `C:\worktree`, "old", "new"); !errors.As(err, &refusal) || refusal.Reason != RecoveryReconcileNonDescendantHead {
		t.Fatalf("non-descendant refusal = %#v, error=%v", refusal, err)
	}
}

func TestStrictSafeResumeRejectsDivergenceAndAmbiguity(t *testing.T) {
	if err := validateStrictSafeResume(RecoveryAssessment{Classification: RecoverySafeResume}); err != nil {
		t.Fatal(err)
	}
	diverged := RecoveryAssessment{
		Classification: RecoverySafeResume,
		Findings:       []RecoveryFinding{{Code: RecoveryFindingNonOverlappingUserEdit}},
	}
	var refusal *RecoveryReconcileRefusal
	if err := validateStrictSafeResume(diverged); !errors.As(err, &refusal) || refusal.Reason != RecoveryReconcileUnsafeFinding {
		t.Fatalf("divergence refusal=%#v err=%v", refusal, err)
	}
	ambiguous := RecoveryAssessment{
		Classification: RecoveryReconcileRequired,
		Findings:       []RecoveryFinding{{Code: RecoveryFindingAmbiguousExternalAction}},
	}
	if err := validateStrictSafeResume(ambiguous); err == nil {
		t.Fatal("ambiguous recovery was accepted as safe resume")
	}
}

type recoveryAncestorRunner struct {
	executable string
	arguments  []string
	err        error
}

func (runner *recoveryAncestorRunner) Run(
	_ context.Context,
	_ string,
	executable string,
	arguments ...string,
) (workspace.CommandResult, error) {
	runner.executable = executable
	runner.arguments = append([]string(nil), arguments...)
	return workspace.CommandResult{}, runner.err
}
