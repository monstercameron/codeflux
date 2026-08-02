package pipeline

import (
	"slices"
	"strings"
	"testing"
)

// declaredTaskClasses mirrors fingerprint.AllTaskClasses.
//
// It is written out rather than imported because internal/pipeline is the
// vocabulary layer and importing the fingerprint package for a list of strings
// would invert that. A test in internal/coordinator keeps the two in step.
var declaredTaskClasses = []string{
	"documentation", "small-change", "bug-fix", "feature",
	"refactor", "migration", "security",
}

// TestPIPE117_EveryDeclaredTaskClassCanExpressAnAcceptanceExample records the
// spike's finding.
//
// PIPE-019 makes an acceptance example mandatory. The current form is
// arguments, standard input, and expected output, which only work with a
// command-line surface can satisfy, so the question the spike had to answer
// was whether a mandatory example is expressible at all.
func TestPIPE117_EveryDeclaredTaskClassCanExpressAnAcceptanceExample(t *testing.T) {
	if findings := ValidateAcceptanceCapabilities(declaredTaskClasses); len(findings) > 0 {
		t.Fatalf("the acceptance capability table does not describe the flow:\n  %s",
			strings.Join(findings, "\n  "))
	}
	// The ticket assumes six classes. There are seven, and a table that
	// silently covered six would answer the wrong question.
	if len(declaredTaskClasses) != 7 {
		t.Fatalf("this test is written against seven declared classes, found %d",
			len(declaredTaskClasses))
	}
}

// TestPIPE117_EveryClassCanStateACheckableExample is what the blocked-until
// reminder became once PIPE-118 landed.
//
// The reminder fired and PIPE-118 added the named-test form, so every declared
// class can now state an example the flow can check. PIPE-019 — making the
// example mandatory — is not done: see that item. This invariant is what
// PIPE-019 will depend on, and it has to keep holding before that gate can be
// turned on honestly.
func TestPIPE117_EveryClassCanStateACheckableExample(t *testing.T) {
	blocked := ClassesWithoutAnImplementedForm()
	if len(blocked) > 0 {
		t.Fatalf("the instructions stage requires an acceptance example and "+
			"these classes cannot state one the flow can check: %s. Either "+
			"implement the form they need or stop refusing them.",
			strings.Join(blocked, ", "))
	}
	if !slices.Contains(ImplementedAcceptanceForms(), FormNamedTest) {
		t.Error("the named-test form is what makes an example expressible for " +
			"work with no command-line surface; without it the mandatory " +
			"example refuses three of the seven classes")
	}
}

// TestPIPE117_BehaviourPreservingClassesUseADifferentOracle records the second
// finding, which PIPE-119 and PIPE-120 build on.
func TestPIPE117_BehaviourPreservingClassesUseADifferentOracle(t *testing.T) {
	for _, class := range []string{"refactor", "migration"} {
		capability, found := acceptanceCapabilityFor(class)
		if !found {
			t.Fatalf("%q has no recorded capability", class)
		}
		if !slices.Contains(capability.Forms, FormPreChangeSuite) {
			t.Errorf("%q is judged against preserved behaviour and does not "+
				"admit the pre-change suite as its oracle", class)
		}
	}
	// A feature is judged against new behaviour, so the pre-change suite is
	// not an acceptance oracle for it: it would pass before the work started.
	capability, _ := acceptanceCapabilityFor("feature")
	if slices.Contains(capability.Forms, FormPreChangeSuite) {
		t.Error("a feature admits the pre-change suite, which passes before the " +
			"work is done and so proves nothing about it")
	}
}

func acceptanceCapabilityFor(class string) (AcceptanceCapability, bool) {
	for _, capability := range AcceptanceCapabilities {
		if capability.Class == class {
			return capability, true
		}
	}
	return AcceptanceCapability{}, false
}
