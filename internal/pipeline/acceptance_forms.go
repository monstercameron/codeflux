package pipeline

import "fmt"

// AcceptanceForm is a way a requirement can state something external that
// checks the produced work (PIPE-117).
type AcceptanceForm string

const (
	// FormCommand is the existing form: arguments, standard input, and the
	// expected output of running the produced program.
	//
	// Only work with a command-line surface can use it, which is why making an
	// example mandatory without a second form would make most task classes
	// unstartable.
	FormCommand AcceptanceForm = "command"
	// FormNamedTest identifies a test by package and name that must pass.
	//
	// It is the general form: any Go work can be judged against a test the
	// requester wrote, whether or not the change has an executable surface.
	FormNamedTest AcceptanceForm = "named-test"
	// FormPreChangeSuite is the whole existing suite, passing unchanged.
	//
	// It is the honest oracle for work that is meant to preserve behaviour. It
	// is weaker than a written example — it cannot say the refactor achieved
	// anything — and it is the strongest claim available for a change whose
	// whole point is that nothing observable moves.
	FormPreChangeSuite AcceptanceForm = "pre-change-suite"
	// FormDocumentationExample is a Go Example function with an Output
	// comment.
	//
	// Behaviour-linked documentation is the one class where the documentation
	// itself can be executed, and Go already has the mechanism, so inventing
	// another would be worse than using it.
	FormDocumentationExample AcceptanceForm = "documentation-example"
)

// AcceptanceCapability records which forms a task class can express, and why
// it cannot use the ones it cannot.
//
// This is the recorded outcome of the PIPE-117 spike. The question it answers
// is narrow and load-bearing: PIPE-019 makes an acceptance example mandatory,
// and doing that against a single command-line form would make four of the
// declared classes unstartable.
type AcceptanceCapability struct {
	Class string
	// Forms are admissible for this class, most specific first.
	Forms []AcceptanceForm
	// Rationale says why this class needs the forms it has, in the terms the
	// class is defined by rather than in terms of the mechanism.
	Rationale string
}

// AcceptanceCapabilities is the spike's finding, per declared task class.
//
// A finding rather than a design: it records what each class can express with
// the forms available, so PIPE-019's mandatory example can be argued against
// evidence. Two things it establishes:
//
//   - Every class can express at least one form, so a mandatory example is
//     achievable — but only once FormNamedTest exists (PIPE-118). Today only
//     FormCommand is implemented, and four classes cannot use it.
//   - Two classes are judged against preserved behaviour rather than new
//     behaviour, which is a different kind of claim and needs the acceptance
//     oracle restated for repository work (PIPE-119, PIPE-120).
//
// The flow declares seven classes, not the six the ticket text assumes.
var AcceptanceCapabilities = []AcceptanceCapability{
	{
		Class: "documentation",
		Forms: []AcceptanceForm{FormDocumentationExample, FormNamedTest},
		Rationale: "documentation tied to behaviour is checked by executing " +
			"the behaviour it describes; Go Example functions are exactly that " +
			"and already run in the suite",
	},
	{
		Class: "small-change",
		Forms: []AcceptanceForm{FormNamedTest, FormCommand},
		Rationale: "too small to assume a command-line surface, so the general " +
			"form is the reliable one",
	},
	{
		Class: "bug-fix",
		Forms: []AcceptanceForm{FormNamedTest, FormCommand},
		Rationale: "the reproduction is the example: a named test that fails " +
			"at the base revision and passes at the candidate",
	},
	{
		Class: "feature",
		Forms: []AcceptanceForm{FormCommand, FormNamedTest},
		Rationale: "new observable behaviour, which is what the command form " +
			"was built for when it has a surface to observe",
	},
	{
		Class: "refactor",
		Forms: []AcceptanceForm{FormPreChangeSuite, FormNamedTest},
		Rationale: "behaviour is meant to be unchanged, so the pre-change suite " +
			"passing unchanged is the claim; a written example would assert a " +
			"change the work is not supposed to make",
	},
	{
		Class: "migration",
		Forms: []AcceptanceForm{FormNamedTest, FormPreChangeSuite},
		Rationale: "the claim is that old data still reads and old peers still " +
			"interoperate, which is a test over both versions rather than an " +
			"output to compare",
	},
	{
		Class: "security",
		Forms: []AcceptanceForm{FormNamedTest, FormCommand},
		Rationale: "the example is an abuse case that must be refused, and a " +
			"refusal is asserted rather than printed",
	},
}

// ImplementedAcceptanceForms are the forms the flow can actually check today.
//
// Kept apart from AcceptanceCapabilities so the gap between what a class could
// express and what the product can verify is visible rather than implied.
func ImplementedAcceptanceForms() []AcceptanceForm {
	// FormNamedTest landed with PIPE-118, which is what makes an acceptance
	// example expressible for every declared class.
	return []AcceptanceForm{FormCommand, FormNamedTest}
}

// ClassesWithoutAnImplementedForm reports the task classes that cannot state
// an acceptance example the flow can check today.
//
// This is the measurement PIPE-019 turns on: making an example mandatory while
// this is non-empty would refuse work the product is meant to support.
func ClassesWithoutAnImplementedForm() []string {
	implemented := map[AcceptanceForm]struct{}{}
	for _, form := range ImplementedAcceptanceForms() {
		implemented[form] = struct{}{}
	}
	var blocked []string
	for _, capability := range AcceptanceCapabilities {
		usable := false
		for _, form := range capability.Forms {
			if _, ok := implemented[form]; ok {
				usable = true
				break
			}
		}
		if !usable {
			blocked = append(blocked, capability.Class)
		}
	}
	return blocked
}

// ValidateAcceptanceCapabilities rejects a capability table that has stopped
// describing the flow.
func ValidateAcceptanceCapabilities(declaredClasses []string) []string {
	var findings []string
	covered := map[string]struct{}{}
	for _, capability := range AcceptanceCapabilities {
		if len(capability.Forms) == 0 {
			findings = append(findings, fmt.Sprintf(
				"task class %q can express no acceptance form at all", capability.Class))
		}
		if capability.Rationale == "" {
			findings = append(findings, fmt.Sprintf(
				"task class %q states no rationale", capability.Class))
		}
		if _, repeated := covered[capability.Class]; repeated {
			findings = append(findings, fmt.Sprintf(
				"task class %q appears twice", capability.Class))
		}
		covered[capability.Class] = struct{}{}
	}
	for _, class := range declaredClasses {
		if _, present := covered[class]; !present {
			findings = append(findings, fmt.Sprintf(
				"task class %q is declared by the product and has no recorded "+
					"acceptance capability", class))
		}
	}
	return findings
}
