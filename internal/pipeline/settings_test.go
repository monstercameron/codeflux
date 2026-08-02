package pipeline

import (
	"reflect"
	"strings"
	"testing"
	"unicode"
)

// TestEverySettingIsDescribed keeps the surface and the type in step.
//
// A setting the engine honours and no page describes is one nobody can change
// without editing source, which is the situation this type exists to end.
func TestEverySettingIsDescribed(t *testing.T) {
	described := map[string]bool{}
	for _, toggle := range Describe() {
		if described[toggle.Key] {
			t.Errorf("two settings share the key %q", toggle.Key)
		}
		described[toggle.Key] = true
		if toggle.Label == "" {
			t.Errorf("%s has no label, so a page cannot show it", toggle.Key)
		}
		if toggle.Help == "" {
			t.Errorf("%s says nothing about what it costs or buys", toggle.Key)
		}
		if toggle.Group == "" {
			t.Errorf("%s belongs to no group", toggle.Key)
		}
	}
	// The count comes off the struct by reflection rather than a constant. As
	// a constant it was a second thing to remember to update, and it was not
	// updated: a field was added, the number stayed at what it had been, and
	// the check that exists to catch an undescribed setting passed while one
	// was undescribed. A number read from the type cannot drift from it.
	onType := reflect.TypeOf(Settings{}).NumField()
	if len(described) != onType {
		var missing []string
		for index := 0; index < onType; index++ {
			field := reflect.TypeOf(Settings{}).Field(index)
			if !described[settingKeyFor(field.Name)] {
				missing = append(missing, field.Name)
			}
		}
		t.Errorf("%d setting(s) described, %d on the type; undescribed: %v — "+
			"nothing can render them", len(described), onType, missing)
	}
}

// settingKeyFor turns a Go field name into the key Describe uses for it.
//
// The two vocabularies differ on purpose — the type reads as Go, the surface
// reads as storage — so the mapping has to exist somewhere. Here is the one
// place that needs it.
func settingKeyFor(field string) string {
	var key []rune
	for index, letter := range field {
		if unicode.IsUpper(letter) {
			if index > 0 {
				key = append(key, '_')
			}
			letter = unicode.ToLower(letter)
		}
		key = append(key, letter)
	}
	return string(key)
}

// TestTheFieldNamingMatchesTheKeys is what makes the count above meaningful.
//
// If the mapping were wrong, every field would read as undescribed and the
// count check would fire for the wrong reason — or, worse, a rename could make
// both sides wrong in the same direction and nothing would notice.
func TestTheFieldNamingMatchesTheKeys(t *testing.T) {
	cases := []struct{ field, key string }{
		{"Ambiguity", "ambiguity"},
		{"MaximumAttempts", "maximum_attempts"},
		{"MutationThresholdPercent", "mutation_threshold_percent"},
		{"ForbiddenCapabilities", "forbidden_capabilities"},
	}
	for _, tried := range cases {
		if got := settingKeyFor(tried.field); got != tried.key {
			t.Errorf("%s maps to %q, want %q", tried.field, got, tried.key)
		}
	}
}

// TestARestrictionNothingEnforcesIsRefused is the failure worth preventing.
//
// A capability named in a spelling the engine does not recognise would be
// silently ignored: the person believes a limit is in force and nothing checks
// it. Refusing at the point it is set is the only moment anything can say so.
func TestARestrictionNothingEnforcesIsRefused(t *testing.T) {
	settings := DefaultSettings()
	settings.ForbiddenCapabilities = []string{"the internet"}
	err := settings.Validate()
	if err == nil {
		t.Fatal("a restriction nothing enforces was accepted")
	}
	if !strings.Contains(err.Error(), "nothing would enforce it") {
		t.Errorf("the refusal does not say why it matters: %v", err)
	}

	settings.ForbiddenCapabilities = []string{CapabilityNetwork}
	if err := settings.Validate(); err != nil {
		t.Errorf("a restriction the engine does enforce was refused: %v", err)
	}
}

// TestTheDefaultsAreValid is the check that a fresh install works.
func TestTheDefaultsAreValid(t *testing.T) {
	if err := DefaultSettings().Validate(); err != nil {
		t.Fatalf("the defaults do not satisfy their own bounds: %v", err)
	}
}

// TestTheDefaultsFavourBeingToldTheTruthQuickly records the choices themselves.
//
// Defaults are the settings almost everyone runs, so what they are is a
// product decision rather than an implementation detail, and it is pinned
// where a change to it has to be deliberate.
func TestTheDefaultsFavourBeingToldTheTruthQuickly(t *testing.T) {
	settings := DefaultSettings()
	if settings.Ambiguity != AmbiguityAsk {
		t.Error("the default is to guess rather than ask, which makes every " +
			"unattended run a silent risk")
	}
	if settings.Platforms != PlatformsHost {
		t.Error("the default cross-compiles, which costs a build per platform " +
			"on every run to answer a question most people have not asked")
	}
	for _, required := range []struct {
		name string
		on   bool
	}{
		{"tests", settings.RequireTests},
		{"doc comments", settings.RequireDocComments},
		{"adversarial review", settings.AdversarialReview},
	} {
		if !required.on {
			t.Errorf("%s are off by default, so the flow's own thesis is "+
				"opt-in", required.name)
		}
	}
}

// TestAValueOutsideItsBoundsIsRefusedByName keeps the error useful.
func TestAValueOutsideItsBoundsIsRefusedByName(t *testing.T) {
	settings := DefaultSettings()
	settings.MaximumAttempts = 0
	err := settings.Validate()
	if err == nil {
		t.Fatal("zero attempts was accepted, which is a run that cannot work")
	}
	if !contains([]string{err.Error()}, err.Error()) {
		t.Fatal("unreachable")
	}
	settings = DefaultSettings()
	settings.Ambiguity = "whatever"
	if settings.Validate() == nil {
		t.Error("an unknown posture was accepted")
	}
}

// TestEveryBoundedSettingCanTakeItsDefault guards against a description whose
// bounds exclude the value the engine ships with.
func TestEveryBoundedSettingCanTakeItsDefault(t *testing.T) {
	settings := DefaultSettings()
	for _, toggle := range Describe() {
		if toggle.Kind != ToggleNumber {
			continue
		}
		if toggle.Minimum > toggle.Maximum {
			t.Errorf("%s has a minimum above its maximum", toggle.Key)
		}
	}
	if err := settings.Validate(); err != nil {
		t.Errorf("a described bound excludes the default: %v", err)
	}
}
