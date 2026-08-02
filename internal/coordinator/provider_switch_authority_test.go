package coordinator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAUDIT019_ModelChangesAreNarratedAndApprovalGated records what the ladder
// actually does today, for AUDIT-019 (M12-051, M12-052, M12-G04).
//
// M12-G04 states that provider switching never occurs without explicit user
// authority. providers.ValidateProviderSwitch exists to enforce that and has
// no production caller, so the gate is currently true for a different reason
// than the one it claims: the escalation ladder is the only production path
// that changes the model, and it is governed by narration and an
// approval-required rung list rather than by a bound authority record.
//
// That distinction matters. Narration tells a person what happened; a bound
// authority record is what lets a later reader confirm the exact identities a
// person approved. This test pins the current arrangement so a change to it is
// visible, and does not pretend the stronger property holds.
func TestAUDIT019_ModelChangesAreNarratedAndApprovalGated(t *testing.T) {
	source := readCoordinatorSource(t, "agent_execution.go")

	// The escalation site must keep both controls.
	if !strings.Contains(source, "execution.settings.NeedsApproval(decision.Escalated)") {
		t.Error("escalation no longer consults the approval-required rung list, " +
			"so a run could climb to a rung the person gated")
	}
	if !strings.Contains(source, "execution.escalate(decision.Escalated)") {
		t.Fatal("the escalation site has moved; re-check what governs a model change")
	}
	// The change is stated to the person rather than made quietly.
	if !strings.Contains(source, "execution.say(ctx, scope, events.KindMessageFinal, decision.Why)") {
		t.Error("a model change is no longer narrated, so it would be silent")
	}
}

// TestAUDIT019_ProviderSwitchValidationIsStillUnwired states the gap plainly
// rather than leaving it to be rediscovered.
//
// When ValidateProviderSwitch or ProviderRecoveryService gains a production
// caller this test fails, which is the point: it is the reminder to close
// AUDIT-019 rather than a rule against wiring them.
func TestAUDIT019_ProviderSwitchValidationIsStillUnwired(t *testing.T) {
	unwired := map[string]bool{
		"ValidateProviderSwitch":     true,
		"NewProviderRecoveryService": true,
	}
	root := repositoryRootForCoordinatorTest(t)

	for _, directory := range []string{"internal", "cmd"} {
		_ = filepath.WalkDir(filepath.Join(root, directory),
			func(path string, entry os.DirEntry, err error) error {
				if err != nil || entry.IsDir() ||
					!strings.HasSuffix(path, ".go") ||
					strings.HasSuffix(path, "_test.go") {
					return nil
				}
				content, readErr := os.ReadFile(path)
				if readErr != nil {
					return nil
				}
				body := string(content)
				for symbol := range unwired {
					// Skip the file that declares it.
					if strings.Contains(body, "func "+symbol+"(") {
						continue
					}
					if strings.Contains(body, symbol+"(") {
						unwired[symbol] = false
					}
				}
				return nil
			})
	}

	for symbol, stillUnwired := range unwired {
		if !stillUnwired {
			t.Errorf("%s now has a production caller. AUDIT-019 can be closed: "+
				"confirm the from/to identities are persisted and the retry, "+
				"resume, and explicit-switch commands are exposed, then delete "+
				"this assertion.", symbol)
		}
	}
}

func readCoordinatorSource(t *testing.T, name string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(
		repositoryRootForCoordinatorTest(t), "internal", "coordinator", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(content)
}

func repositoryRootForCoordinatorTest(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("repository root %q has no go.mod: %v", root, err)
	}
	return root
}
