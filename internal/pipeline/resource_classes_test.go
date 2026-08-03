package pipeline

import (
	"strings"
	"testing"
)

// cloneResourceClasses returns a deep-enough copy of the real table so a
// test can break it without mutating the package variable other tests read
// (the same shape cloneRequirements uses in requirements_test.go).
func cloneResourceClasses() []StageResourceClass {
	cloned := make([]StageResourceClass, len(ResourceClasses))
	copy(cloned, ResourceClasses)
	return cloned
}

// TestPIPE059_TheRealTableIsValid is the sanity check the rest of this file
// gives teeth to: the declared table, as shipped, satisfies every property
// ValidateResourceClasses checks, and it covers the flow completely.
func TestPIPE059_TheRealTableIsValid(t *testing.T) {
	if findings := ValidateResourceClasses(); len(findings) > 0 {
		t.Fatalf("the shipped ResourceClasses table has findings:\n  %s",
			strings.Join(findings, "\n  "))
	}
	if len(ResourceClasses) != len(Flow) {
		t.Fatalf("%d resource-class entries for %d flow stages",
			len(ResourceClasses), len(Flow))
	}
	if _, found := ResourceClassFor(StageInstructions); !found {
		t.Fatal("ResourceClassFor cannot find the root stage")
	}
	if _, found := ResourceClassFor(Number(999)); found {
		t.Fatal("ResourceClassFor reports a stage that does not exist")
	}
}

// TestPIPE059_ExactlyOneStageIsExclusiveMutating discriminates the one
// classification this ticket cares most about getting right: checkMutations
// is the sole checker in the flow that writes a produced source file in
// place, so StageAtomMutation must be the sole exclusive-mutating entry.
//
// This is not a tautology against the table's own data: it fails if a
// second entry is marked exclusive-mutating, or if StageAtomMutation itself
// is not.
func TestPIPE059_ExactlyOneStageIsExclusiveMutating(t *testing.T) {
	var exclusive []Number
	for _, entry := range ResourceClasses {
		if entry.Class.Exclusive() {
			exclusive = append(exclusive, entry.Stage)
		}
	}
	if len(exclusive) != 1 || exclusive[0] != StageAtomMutation {
		t.Fatalf("exclusive-mutating stages = %v, want exactly [%d] "+
			"(atom-mutation)", exclusive, StageAtomMutation)
	}
}

// TestPIPE059_CoverageDiscriminatesAMissingOrDuplicateEntry proves
// ValidateResourceClassesTable actually catches a broken table rather than
// only ever returning nil against whatever happens to be handed to it.
func TestPIPE059_CoverageDiscriminatesAMissingOrDuplicateEntry(t *testing.T) {
	t.Run("missing entry", func(t *testing.T) {
		broken := cloneResourceClasses()
		for index, entry := range broken {
			if entry.Stage == StageDeliver {
				broken = append(broken[:index], broken[index+1:]...)
				break
			}
		}
		findings := ValidateResourceClassesTable(broken)
		if !containsSubstring(findings, "stage 37 (deliver) has no entry") {
			t.Fatalf("dropping deliver's entry was not caught: %v", findings)
		}
	})

	t.Run("duplicate entry", func(t *testing.T) {
		broken := cloneResourceClasses()
		broken = append(broken, StageResourceClass{
			Stage: StageInstructions, Class: ResourceClassPureAST,
			Reason: "duplicate for the test",
		})
		findings := ValidateResourceClassesTable(broken)
		if !containsSubstring(findings, "is bound 2 times") {
			t.Fatalf("the duplicate was not caught: %v", findings)
		}
	})

	t.Run("unknown stage", func(t *testing.T) {
		broken := cloneResourceClasses()
		broken = append(broken, StageResourceClass{
			Stage: Number(999), Class: ResourceClassPureAST,
			Reason: "not a real stage",
		})
		findings := ValidateResourceClassesTable(broken)
		if !containsSubstring(findings, "binds stage 999, which is not in the flow") {
			t.Fatalf("the unknown stage was not caught: %v", findings)
		}
	})

	t.Run("invalid class", func(t *testing.T) {
		broken := cloneResourceClasses()
		for index, entry := range broken {
			if entry.Stage == StageAtoms {
				broken[index].Class = ResourceClass("network-bound")
				break
			}
		}
		findings := ValidateResourceClassesTable(broken)
		if !containsSubstring(findings, "none of the four PIPE-059 defines") {
			t.Fatalf("the invalid class was not caught: %v", findings)
		}
	})

	t.Run("missing reason", func(t *testing.T) {
		broken := cloneResourceClasses()
		for index, entry := range broken {
			if entry.Stage == StageAtoms {
				broken[index].Reason = ""
				break
			}
		}
		findings := ValidateResourceClassesTable(broken)
		if !containsSubstring(findings, "no reason recorded") {
			t.Fatalf("the missing reason was not caught: %v", findings)
		}
	})
}

// TestPIPE059_ResourceClassMapMatchesTheSlice proves the map projection
// agrees with the table it was built from for every declared stage, so a
// scheduler reading the map cannot see a different answer than a reader of
// the slice would.
func TestPIPE059_ResourceClassMapMatchesTheSlice(t *testing.T) {
	classes := ResourceClassMap()
	if len(classes) != len(ResourceClasses) {
		t.Fatalf("map has %d entries, slice has %d", len(classes), len(ResourceClasses))
	}
	for _, entry := range ResourceClasses {
		if classes[entry.Stage] != entry.Class {
			t.Errorf("stage %d: map says %q, slice says %q",
				entry.Stage, classes[entry.Stage], entry.Class)
		}
	}
}

// TestPIPE059_ExclusiveIsTrueOnlyForExclusiveMutating discriminates
// ResourceClass.Exclusive itself, independent of the table: it must read
// true for exactly one of the four declared classes.
func TestPIPE059_ExclusiveIsTrueOnlyForExclusiveMutating(t *testing.T) {
	cases := []struct {
		class ResourceClass
		want  bool
	}{
		{ResourceClassPureAST, false},
		{ResourceClassBuildOnly, false},
		{ResourceClassSuiteRead, false},
		{ResourceClassExclusiveMutating, true},
	}
	for _, testCase := range cases {
		if got := testCase.class.Exclusive(); got != testCase.want {
			t.Errorf("%s.Exclusive() = %v, want %v", testCase.class, got, testCase.want)
		}
	}
}
