package coordinator

import "testing"

// TestPIPE139_ATypeConversionIsNotReadAsAnEffect is the discrimination proof
// for one half of PIPE-139's complaint: the old callsAnythingImpure treated
// any call-shaped expression into an effect package as an effect, so
// time.Duration(n) — a type conversion, not a call to anything — was read as
// reaching for the clock purely because it shares a package with time.Now().
//
// Proven to discriminate: the old, package-blanket rule (still preserved
// verbatim as callsAnythingImpure for its other, unowned callers) would mark
// this function impure. observedEffects, used for producedFunction.Pure, does
// not: pureConversionTypes excludes time.Duration by name, so a function
// doing nothing but that conversion is still pure.
func TestPIPE139_ATypeConversionIsNotReadAsAnEffect(t *testing.T) {
	worktree := testedNamesFixture(t, map[string]string{
		"go.mod": "module codeflux.test/effects\n\ngo 1.26.0\n",
		"lib.go": "package lib\n\nimport \"time\"\n\n" +
			"func FromSeconds(value int) time.Duration {\n" +
			"\treturn time.Duration(value) * time.Second\n}\n",
	})

	functions, err := readProducedFunctions(worktree)
	if err != nil {
		t.Fatalf("reading produced functions: %v", err)
	}
	byName := map[string]producedFunction{}
	for _, function := range functions {
		byName[function.Name] = function
	}
	fromSeconds, found := byName["FromSeconds"]
	if !found {
		t.Fatal("the function was not read at all")
	}
	if !fromSeconds.Pure {
		t.Errorf("a function that only converts to time.Duration was called "+
			"impure, reaching for: %v", fromSeconds.Effects)
	}
}

// TestPIPE139_AClockReadIsStillAnEffect is the companion control: time.Now is
// a real effect and must still be caught, so the type-conversion exclusion
// above is narrow rather than exempting the whole package.
func TestPIPE139_AClockReadIsStillAnEffect(t *testing.T) {
	worktree := testedNamesFixture(t, map[string]string{
		"go.mod": "module codeflux.test/effects\n\ngo 1.26.0\n",
		"lib.go": "package lib\n\nimport \"time\"\n\n" +
			"func Started() time.Time {\n\treturn time.Now()\n}\n",
	})

	functions, err := readProducedFunctions(worktree)
	if err != nil {
		t.Fatalf("reading produced functions: %v", err)
	}
	byName := map[string]producedFunction{}
	for _, function := range functions {
		byName[function.Name] = function
	}
	started, found := byName["Started"]
	if !found {
		t.Fatal("the function was not read at all")
	}
	if started.Pure {
		t.Error("a function that reads the clock was called pure")
	}
	found = false
	for _, effect := range started.Effects {
		if effect == "time.Now" {
			found = true
		}
	}
	if !found {
		t.Errorf("time.Now is not named in the observed effects: %v", started.Effects)
	}
}

// TestPIPE139_ARenamedImportIsStillResolved is the discrimination proof for
// the other half of PIPE-139's complaint: the old check matched a bare
// identifier against a fixed list of package names, so a renamed import
// reached an effect package under a name the list never contained.
// observedEffects resolves through the file's own import declarations
// instead, so a renamed import is still recognised for what it actually is.
//
// Proven to discriminate: matching selector.X.Name directly against a fixed
// set of literal package identifiers — the shape callsAnythingImpure still
// uses for its own, unowned callers — would not find "housetime" in any such
// list and would report this function pure. Resolving through the file's own
// import map finds it correctly impure.
func TestPIPE139_ARenamedImportIsStillResolved(t *testing.T) {
	worktree := testedNamesFixture(t, map[string]string{
		"go.mod": "module codeflux.test/effects\n\ngo 1.26.0\n",
		"lib.go": "package lib\n\nimport housetime \"time\"\n\n" +
			"func Started() housetime.Time {\n\treturn housetime.Now()\n}\n",
	})

	functions, err := readProducedFunctions(worktree)
	if err != nil {
		t.Fatalf("reading produced functions: %v", err)
	}
	byName := map[string]producedFunction{}
	for _, function := range functions {
		byName[function.Name] = function
	}
	started, found := byName["Started"]
	if !found {
		t.Fatal("the function was not read at all")
	}
	if started.Pure {
		t.Errorf("a renamed import's clock read was not resolved to an "+
			"effect: %v", started.Effects)
	}
}
