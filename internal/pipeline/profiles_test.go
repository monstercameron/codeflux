package pipeline

import (
	"context"
	"sort"
	"strings"
	"sync"
	"testing"
)

// TestPIPE046_TheRealProfilesTableIsValid is the sanity check the rest of
// this file gives teeth to: the shipped Profiles table, as it stands,
// satisfies every property ValidateProfiles checks.
func TestPIPE046_TheRealProfilesTableIsValid(t *testing.T) {
	if findings := ValidateProfiles(); len(findings) > 0 {
		t.Fatalf("the shipped Profiles table has findings:\n  %s",
			strings.Join(findings, "\n  "))
	}
	if _, found := ProfileFor("default"); !found {
		t.Fatal("ProfileFor cannot find the default profile")
	}
	if _, found := ProfileFor("library"); !found {
		t.Fatal("ProfileFor cannot find the library profile")
	}
	if _, found := ProfileFor("no-such-profile"); found {
		t.Fatal("ProfileFor reports a profile that does not exist")
	}
}

// TestPIPE046_ValidateProfilesDiscriminatesABrokenTable proves
// ValidateProfiles actually catches a broken table rather than only ever
// returning nil against whatever happens to be handed to it, the same
// pattern TestPIPE059_CoverageDiscriminatesAMissingOrDuplicateEntry
// establishes for ResourceClasses.
func TestPIPE046_ValidateProfilesDiscriminatesABrokenTable(t *testing.T) {
	saved := Profiles
	savedDefault := ProfileDefault
	t.Cleanup(func() {
		Profiles = saved
		ProfileDefault = savedDefault
	})

	t.Run("unknown stage", func(t *testing.T) {
		Profiles = []RunProfile{ProfileDefault, {
			Name: "broken",
			Declines: []ProfileDecline{
				{Stage: Number(999), Reason: "not a real stage"},
			},
		}}
		findings := ValidateProfiles()
		if !containsSubstring(findings, "declines stage 999, which is not in the flow") {
			t.Fatalf("the unknown stage was not caught: %v", findings)
		}
	})

	t.Run("duplicate decline", func(t *testing.T) {
		Profiles = []RunProfile{ProfileDefault, {
			Name: "broken",
			Declines: []ProfileDecline{
				{Stage: StageAdversarial, Reason: "first"},
				{Stage: StageAdversarial, Reason: "second"},
			},
		}}
		findings := ValidateProfiles()
		if !containsSubstring(findings, "declines stage 31 more than once") {
			t.Fatalf("the duplicate decline was not caught: %v", findings)
		}
	})

	t.Run("missing reason", func(t *testing.T) {
		Profiles = []RunProfile{ProfileDefault, {
			Name:     "broken",
			Declines: []ProfileDecline{{Stage: StageAdversarial}},
		}}
		findings := ValidateProfiles()
		if !containsSubstring(findings, "no reason recorded") {
			t.Fatalf("the missing reason was not caught: %v", findings)
		}
	})

	t.Run("duplicate name", func(t *testing.T) {
		Profiles = []RunProfile{ProfileDefault, ProfileDefault}
		findings := ValidateProfiles()
		if !containsSubstring(findings, `profile name "default" is declared 2 times`) {
			t.Fatalf("the duplicate name was not caught: %v", findings)
		}
	})

	t.Run("default profile declines something", func(t *testing.T) {
		ProfileDefault = RunProfile{
			Name:     "default",
			Declines: []ProfileDecline{{Stage: StageAdversarial, Reason: "broken"}},
		}
		Profiles = []RunProfile{ProfileDefault}
		findings := ValidateProfiles()
		if !containsSubstring(findings, "default profile must decline nothing") {
			t.Fatalf("the corrupted default profile was not caught: %v", findings)
		}
	})
}

// TestPIPE046_LibraryProfileDeclinesFewerStagesThanDefault is the ticket's
// own example, cashed out against the current, un-renumbered flow: a library
// run's applicable-stage set is a strict subset of the default's, and the two
// stages missing from it are named ones, not merely "fewer".
func TestPIPE046_LibraryProfileDeclinesFewerStagesThanDefault(t *testing.T) {
	defaultStages := ProfileDefault.ApplicableStages()
	libraryStages := ProfileLibrary.ApplicableStages()

	if len(defaultStages) != len(Flow) {
		t.Fatalf("the default profile's applicable set has %d stages, want all %d",
			len(defaultStages), len(Flow))
	}
	if want := len(Flow) - 2; len(libraryStages) != want {
		t.Fatalf("the library profile's applicable set has %d stages, want %d "+
			"(the flow minus adversarial and platform-matrix)",
			len(libraryStages), want)
	}

	inLibrary := make(map[Number]bool, len(libraryStages))
	for _, stage := range libraryStages {
		inLibrary[stage] = true
	}
	for _, declined := range []Number{StageAdversarial, StagePlatformMatrix} {
		if inLibrary[declined] {
			t.Errorf("stage %d is declined by the library profile and still "+
				"appears in its applicable set", declined)
		}
	}
	// Every other stage the default carries must survive into the library
	// profile's set unchanged — this is a narrowing, not a different flow.
	for _, stage := range defaultStages {
		if stage == StageAdversarial || stage == StagePlatformMatrix {
			continue
		}
		if !inLibrary[stage] {
			t.Errorf("stage %d is not declined by the library profile but is "+
				"missing from its applicable set", stage)
		}
	}
}

// TestPIPE046_AProfileSelectsADifferentStageSetFromTheSchedulerItself is the
// discriminating test the ticket asks for: not a profile struct nobody
// consults, but real production scheduling code — RestrictToStages and
// RunConcurrently, PIPE-058a/PIPE-058's own machinery — invoked once per
// profile, proving the set of stages actually run differs.
//
// A Runner that is never called for a declined stage is the behavior a
// coordinator integration needs: the stage is never asked to run at all,
// rather than being run and found to have nothing to do.
func TestPIPE046_AProfileSelectsADifferentStageSetFromTheSchedulerItself(t *testing.T) {
	run := func(profile RunProfile) []Number {
		stages := profile.ApplicableStages()
		requirements := RestrictToStages(Requirements, stages)
		classes := ResourceClassMap()

		// Stages run concurrently within a wave (PIPE-058a), so the append
		// below needs its own lock — the same hazard runWave's own results
		// map guards against internally; a Runner has no such guarantee for
		// state it keeps itself.
		var mutex sync.Mutex
		var invoked []Number
		runner := func(_ context.Context, stage Number) (State, string, map[string]any) {
			mutex.Lock()
			invoked = append(invoked, stage)
			mutex.Unlock()
			return StateSatisfied, "", nil
		}
		RunConcurrently(context.Background(), requirements, classes, runner, ScheduleOptions{})
		sort.Slice(invoked, func(i, j int) bool { return invoked[i] < invoked[j] })
		return invoked
	}

	defaultInvoked := run(ProfileDefault)
	libraryInvoked := run(ProfileLibrary)

	if len(defaultInvoked) != len(Flow) {
		t.Fatalf("the default profile's run invoked %d stages, want all %d",
			len(defaultInvoked), len(Flow))
	}
	if len(libraryInvoked) != len(Flow)-2 {
		t.Fatalf("the library profile's run invoked %d stages, want %d",
			len(libraryInvoked), len(Flow)-2)
	}

	invokedUnderLibrary := make(map[Number]bool, len(libraryInvoked))
	for _, stage := range libraryInvoked {
		invokedUnderLibrary[stage] = true
	}
	for _, declined := range []Number{StageAdversarial, StagePlatformMatrix} {
		if invokedUnderLibrary[declined] {
			t.Errorf("the scheduler invoked the Runner for stage %d under the "+
				"library profile, which declines it", declined)
		}
	}
}

// TestPIPE046_ClassifyDeclineForProfileOverridesTheOrdinaryClassification
// proves the same (stage, state) pair classifies differently depending on
// which profile a run declared — the concrete case skip-audit (PIPE-042)
// needs to tell a declared-in-advance decision from a runtime excuse.
func TestPIPE046_ClassifyDeclineForProfileOverridesTheOrdinaryClassification(t *testing.T) {
	// Under the default profile, a not-implemented adversarial stage
	// classifies by the ordinary rule: adversarial's check may satisfy, and
	// not-implemented is not the runtime skip ClassifyDecline treats as a
	// principled decline, so it reads as a gap nobody built.
	class, _ := ClassifyDeclineForProfile(StageAdversarial, StateNotImplemented, ProfileDefault)
	if class != DeclineUnimplemented {
		t.Errorf("adversarial not-implemented under the default profile classified %q, want %q",
			class, DeclineUnimplemented)
	}

	// Under the library profile, the same stage in the same state classifies
	// as a decision made before the run started.
	class, reason := ClassifyDeclineForProfile(StageAdversarial, StateNotImplemented, ProfileLibrary)
	if class != DeclineByProfile {
		t.Errorf("adversarial skipped under the library profile classified %q, want %q",
			class, DeclineByProfile)
	}
	if reason == "" {
		t.Error("a profile decline carries no reason, so a reader cannot see why")
	}

	// The override applies even to not-implemented, and even to a stage that
	// would otherwise be no-check-by-design — the profile's answer comes
	// first regardless of what the stage's own check would have said.
	class, _ = ClassifyDeclineForProfile(StagePlatformMatrix, StateNotImplemented, ProfileLibrary)
	if class != DeclineByProfile {
		t.Errorf("platform-matrix not-implemented under the library profile "+
			"classified %q, want %q", class, DeclineByProfile)
	}

	// A stage the profile does not decline falls through to ClassifyDecline
	// unchanged.
	forProfile, forProfileTicket := ClassifyDeclineForProfile(
		StageAtomOptimization, StateSkipped, ProfileLibrary)
	plain, plainTicket := ClassifyDecline(StageAtomOptimization, StateSkipped)
	if forProfile != plain || forProfileTicket != plainTicket {
		t.Errorf("a stage the library profile does not decline classified "+
			"(%q, %q) with a profile and (%q, %q) without, want them equal",
			forProfile, forProfileTicket, plain, plainTicket)
	}
}

// TestPIPE046_DefaultProfileNeverOverridesAnything proves ProfileDefault is
// truly inert: ClassifyDeclineForProfile against it must agree with
// ClassifyDecline for every stage and every state the ledger accepts, not
// merely the ones the tests above happen to exercise.
func TestPIPE046_DefaultProfileNeverOverridesAnything(t *testing.T) {
	states := []State{StateSatisfied, StateFailed, StateSkipped, StateBlocked, StateNotImplemented}
	for _, stage := range Flow {
		for _, state := range states {
			forProfile, forProfileTicket := ClassifyDeclineForProfile(
				stage.Number, state, ProfileDefault)
			plain, plainTicket := ClassifyDecline(stage.Number, state)
			if forProfile != plain || forProfileTicket != plainTicket {
				t.Errorf("stage %d (%s) state %q: with ProfileDefault = (%q, %q), "+
					"without = (%q, %q)", stage.Number, stage.Name, state,
					forProfile, forProfileTicket, plain, plainTicket)
			}
		}
	}
}
