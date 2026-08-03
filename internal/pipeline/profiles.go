package pipeline

import "fmt"

// ProfileDecline is one stage a RunProfile marks inapplicable before the run
// starts, and the reason (PIPE-046).
//
// docs/plan.md §33, "Declared profiles": "A pure library does not need
// crash-recovery; a filter does not need compatibility. The run declares its
// profile up front and marks stages inapplicable before it starts. A skip
// declared in advance is a decision; a skip discovered at runtime is an
// excuse." Reason is what turns a decline into the former: a stage recorded
// skipped with no reason attached is indistinguishable from one a check
// simply never got to.
type ProfileDecline struct {
	// Stage is the position in the flow this decline names. It must be a
	// stage Flow declares — ValidateProfiles reports one that is not.
	Stage Number
	// Reason states, in one sentence, what about the task class makes this
	// stage not apply, so a reader of a declined-in-advance row sees why
	// without having to trust the profile's name alone.
	Reason string
}

// RunProfile declares, before a run starts, which stages of the flow do not
// apply to the task class this run is (PIPE-046).
//
// A profile only ever narrows the flow — it removes stages a task class
// genuinely has no use for, such as a pure library's lack of a runnable
// command for hostile-input probing, and it cannot add one Flow does not
// already declare. It never marks a stage satisfied: a declined stage is
// still recorded, still visible in the ledger, and still counted by the skip
// audit — declining it in advance only changes why it established nothing,
// from an unexplained gap into a decision made before any check ran.
type RunProfile struct {
	// Name identifies the profile. It is the value a run and the ledger it
	// writes carry to say which profile applied.
	Name string
	// Description states, in a sentence, what a task has to be for this
	// profile to fit it.
	Description string
	// Declines lists every stage this profile marks inapplicable in advance.
	// A profile with no entries — ProfileDefault — declines nothing: every
	// stage the flow declares still applies.
	Declines []ProfileDecline
}

// ProfileDefault is every run's profile unless one is declared otherwise. It
// declines nothing, so it changes no behavior on its own: it exists so
// "no profile was declared" and "the default profile was declared" read the
// same way, rather than leaving the zero value of RunProfile — an empty
// Name — to stand in for both.
var ProfileDefault = RunProfile{
	Name:        "default",
	Description: "Every stage the flow declares applies; nothing is declined in advance.",
}

// ProfileLibrary fits a task whose deliverable is an importable package with
// no runnable command of its own and no declared cross-compilation target —
// exactly the docs/plan.md §33 example, applied to the two stages of the
// current 41-stage flow that example already reaches rather than to
// crash-recovery or compatibility, neither of which the flow has a number
// for yet (PIPE-045).
//
// It declines:
//
//   - adversarial: the gate is "hostile input produces no violation and no
//     sensitive value reaches the output," which presumes a program a run
//     can feed input to. A library with no `main` and no produced command has
//     no such surface, so the check has nothing to probe — today that shows
//     up as a runtime skip inside AgentExecution.probeProducedCommands; this
//     profile makes the same fact a decision recorded before the attempt
//     starts.
//   - platform-matrix: the gate is "every platform the program claims is
//     answered." A library that declares no cross-compilation target claims
//     no platform beyond the host it was built on, so there is nothing this
//     stage exists to answer for.
var ProfileLibrary = RunProfile{
	Name: "library",
	Description: "An importable package with no runnable command of its own " +
		"and no declared cross-compilation target.",
	Declines: []ProfileDecline{
		{StageAdversarial, "no runnable command exists for hostile input to reach"},
		{StagePlatformMatrix, "no cross-compilation target is declared to answer for"},
	},
}

// Profiles is every profile the product ships (PIPE-046). ProfileDefault is
// always first, so a caller that wants "the profile" for a task carrying no
// explicit choice can take Profiles[0] rather than special-casing an empty
// name.
var Profiles = []RunProfile{ProfileDefault, ProfileLibrary}

// ProfileFor returns the shipped profile of the given name.
func ProfileFor(name string) (RunProfile, bool) {
	for _, profile := range Profiles {
		if profile.Name == name {
			return profile, true
		}
	}
	return RunProfile{}, false
}

// DeclinedReason reports whether this profile marks a stage inapplicable in
// advance, and why.
func (profile RunProfile) DeclinedReason(stage Number) (string, bool) {
	for _, decline := range profile.Declines {
		if decline.Stage == stage {
			return decline.Reason, true
		}
	}
	return "", false
}

// ApplicableStages returns every stage of Flow this profile does not decline,
// in flow order (PIPE-046).
//
// This is the set a scheduler actually wants: fed to RestrictToStages in
// place of the whole flow, it produces a dependency table with the declined
// stages already removed, so RunConcurrently never invokes a Runner for one
// of them at all — the stage is a decision recorded before the run starts,
// not a check that ran and found nothing to do.
func (profile RunProfile) ApplicableStages() []Number {
	applicable := make([]Number, 0, len(Flow))
	for _, stage := range Flow {
		if _, declined := profile.DeclinedReason(stage.Number); declined {
			continue
		}
		applicable = append(applicable, stage.Number)
	}
	return applicable
}

// ValidateProfiles reports every disagreement it can find between Profiles
// and the rest of the package's declared vocabulary, following the shape
// ValidateChecks, ValidateRequirements, and ValidateResourceClasses already
// established.
//
// It checks, independently:
//
//  1. every profile has a non-empty, unique Name;
//  2. every decline names a stage Flow actually declares;
//  3. no profile declines the same stage twice;
//  4. every decline carries a non-empty Reason;
//  5. ProfileDefault itself declines nothing, which is what lets a caller
//     treat "no profile declared" and "the default profile declared" as the
//     same fact.
func ValidateProfiles() []string {
	var findings []string

	declaredStages := make(map[Number]struct{}, len(Flow))
	for _, stage := range Flow {
		declaredStages[stage.Number] = struct{}{}
	}

	names := make(map[string]int, len(Profiles))
	for _, profile := range Profiles {
		if profile.Name == "" {
			findings = append(findings, "a profile has no name")
		} else {
			names[profile.Name]++
		}

		seen := make(map[Number]struct{}, len(profile.Declines))
		for _, decline := range profile.Declines {
			if _, exists := declaredStages[decline.Stage]; !exists {
				findings = append(findings, fmt.Sprintf(
					"profile %q declines stage %d, which is not in the flow",
					profile.Name, decline.Stage))
				continue
			}
			if _, duplicate := seen[decline.Stage]; duplicate {
				findings = append(findings, fmt.Sprintf(
					"profile %q declines stage %d more than once",
					profile.Name, decline.Stage))
			}
			seen[decline.Stage] = struct{}{}
			if decline.Reason == "" {
				stage, _ := StageByNumber(decline.Stage)
				findings = append(findings, fmt.Sprintf(
					"profile %q declines stage %d (%s) with no reason recorded",
					profile.Name, decline.Stage, stage.Name))
			}
		}
	}
	for name, count := range names {
		if count > 1 {
			findings = append(findings, fmt.Sprintf(
				"profile name %q is declared %d times", name, count))
		}
	}

	if len(ProfileDefault.Declines) != 0 {
		findings = append(findings, fmt.Sprintf(
			"ProfileDefault declines %d stage(s); the default profile must "+
				"decline nothing, or a run declaring no profile and a run "+
				"declaring \"default\" would behave differently",
			len(ProfileDefault.Declines)))
	}

	return findings
}
