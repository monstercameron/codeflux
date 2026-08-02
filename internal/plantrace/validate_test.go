package plantrace

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestAUDIT002_TheShippedManifestSatisfiesBothScopeGates covers AUDIT-002.
//
// This is the check that matters day to day: the manifest as it actually
// stands. Everything below proves the check can fail; this proves it passes.
func TestAUDIT002_TheShippedManifestSatisfiesBothScopeGates(t *testing.T) {
	if findings := Validate(); len(findings) > 0 {
		t.Fatalf("the shipped manifest fails its own scope gates:\n%s",
			strings.Join(findings, "\n"))
	}
	if len(PrototypeFeatures) == 0 || len(DeferredFeatures) == 0 {
		t.Fatal("an empty manifest satisfies every rule vacuously")
	}
}

// TestAUDIT002_AnUnmappedFeatureIsRejected is M00-G01's falsifying case.
func TestAUDIT002_AnUnmappedFeatureIsRejected(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		feature Feature
		want    string
	}{
		{
			name:    "no journey",
			feature: Feature{Name: "X", Measurement: "m", Milestones: []string{"M01"}},
			want:    "maps to no user journey",
		},
		{
			name:    "no measurement",
			feature: Feature{Name: "X", Journey: "j", Milestones: []string{"M01"}},
			want:    "names no required measurement",
		},
		{
			name:    "no milestone",
			feature: Feature{Name: "X", Journey: "j", Measurement: "m"},
			want:    "names no implementing milestone",
		},
		{
			name: "milestone that is not an identifier",
			feature: Feature{Name: "X", Journey: "j", Measurement: "m",
				Milestones: []string{"later"}},
			want: "is not an MNN identifier",
		},
		{
			name: "dependency in neither manifest",
			feature: Feature{Name: "X", Journey: "j", Measurement: "m",
				Milestones: []string{"M01"}, DependsOn: []string{"nothing named this"}},
			want: "is in neither manifest",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			restore := withFeature(t, testCase.feature)
			defer restore()
			assertFinding(t, testCase.want)
		})
	}
}

// TestAUDIT002_ADeferredCriticalPathDependencyIsRejected is M00-G02's
// falsifying case: the edge the plan forbids in prose, refused in code.
func TestAUDIT002_ADeferredCriticalPathDependencyIsRejected(t *testing.T) {
	t.Run("by name", func(t *testing.T) {
		restore := withFeature(t, Feature{
			Name: "Prototype thing", Journey: "j", Measurement: "m",
			Milestones: []string{"M01"},
			DependsOn:  []string{"Direct graph editing"},
		})
		defer restore()
		assertFinding(t, "depends on deferred POST-015")
	})

	t.Run("by identifier", func(t *testing.T) {
		restore := withFeature(t, Feature{
			Name: "Prototype thing", Journey: "j", Measurement: "m",
			Milestones: []string{"M01"},
			DependsOn:  []string{"POST-013"},
		})
		defer restore()
		assertFinding(t, "depends on deferred POST-013")
	})
}

// TestAUDIT002_ADeferralWithNoGateIsRejected proves a deferral cannot become a
// permanent silent removal.
func TestAUDIT002_ADeferralWithNoGateIsRejected(t *testing.T) {
	original := DeferredFeatures
	DeferredFeatures = append(append([]Deferred{}, original...),
		Deferred{ID: "POST-999", Name: "Something withheld forever"})
	defer func() { DeferredFeatures = original }()
	assertFinding(t, "names no branch gate")
}

// TestAUDIT002_ADependencyCycleIsRejected proves the manifest cannot describe a
// build order that cannot be built.
func TestAUDIT002_ADependencyCycleIsRejected(t *testing.T) {
	original := PrototypeFeatures
	PrototypeFeatures = []Feature{
		{Name: "A", Journey: "j", Measurement: "m", Milestones: []string{"M01"},
			DependsOn: []string{"B"}},
		{Name: "B", Journey: "j", Measurement: "m", Milestones: []string{"M02"},
			DependsOn: []string{"A"}},
	}
	defer func() { PrototypeFeatures = original }()
	assertFinding(t, "dependency cycle")
}

// TestAUDIT002_TheManifestAgreesWithThePlanAndTheChecklist stops the manifest
// from becoming a second source of truth.
//
// A machine-readable manifest that drifts from the document it encodes is
// worse than no manifest: it passes its own checks while describing a product
// nobody is building.
func TestAUDIT002_TheManifestAgreesWithThePlanAndTheChecklist(t *testing.T) {
	root := repositoryRoot(t)

	plan, err := os.ReadFile(filepath.Join(root, "docs", "plan.md"))
	if err != nil {
		t.Fatalf("read plan: %v", err)
	}
	for _, feature := range PrototypeFeatures {
		if !strings.Contains(string(plan), feature.Name) {
			t.Errorf("manifest feature %q appears nowhere in docs/plan.md", feature.Name)
		}
	}

	todos, err := os.ReadFile(filepath.Join(root, "TODOS.md"))
	if err != nil {
		t.Fatalf("read TODOS: %v", err)
	}
	for _, item := range DeferredFeatures {
		if !strings.Contains(string(todos), "`"+item.ID+" DEFER`") {
			t.Errorf("manifest defers %s, which TODOS.md does not declare as a DEFER item", item.ID)
		}
	}

	// Every POST item in the checklist must be represented, or the manifest
	// under-reports what is withheld.
	declared := make(map[string]struct{}, len(DeferredFeatures))
	for _, item := range DeferredFeatures {
		declared[item.ID] = struct{}{}
	}
	deferItem := regexp.MustCompile("`(POST-[0-9]{3}) DEFER`")
	for _, match := range deferItem.FindAllStringSubmatch(string(todos), -1) {
		if _, ok := declared[match[1]]; !ok {
			t.Errorf("TODOS.md declares %s as deferred; the manifest omits it", match[1])
		}
	}
}

func withFeature(t *testing.T, feature Feature) func() {
	t.Helper()
	original := PrototypeFeatures
	PrototypeFeatures = append(append([]Feature{}, original...), feature)
	return func() { PrototypeFeatures = original }
}

func assertFinding(t *testing.T, want string) {
	t.Helper()
	findings := Validate()
	for _, finding := range findings {
		if strings.Contains(finding, want) {
			return
		}
	}
	t.Fatalf("no finding contains %q; got:\n%s", want, strings.Join(findings, "\n"))
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("repository root %q has no go.mod: %v", root, err)
	}
	return root
}
