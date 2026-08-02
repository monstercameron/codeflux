package main

import (
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/plantrace"
)

// TestAUDIT002_LintRejectsADeferredCriticalPathDependency covers the wiring
// half of AUDIT-002.
//
// internal/plantrace proves the rules reject what they should. This proves
// something actually runs them: before this, M00-G01 and M00-G02 were gates
// recorded as passed with no executable behind them, which is the class of
// defect the completion audit exists to find.
func TestAUDIT002_LintRejectsADeferredCriticalPathDependency(t *testing.T) {
	if err := checkPrototypeScopeManifest(); err != nil {
		t.Fatalf("the shipped manifest does not pass the lint check: %v", err)
	}

	original := plantrace.PrototypeFeatures
	plantrace.PrototypeFeatures = append(
		append([]plantrace.Feature{}, original...),
		plantrace.Feature{
			Name:        "A prototype journey that leans on withheld work",
			Journey:     "j",
			Measurement: "m",
			Milestones:  []string{"M01"},
			DependsOn:   []string{"POST-015"},
		},
	)
	t.Cleanup(func() { plantrace.PrototypeFeatures = original })

	err := checkPrototypeScopeManifest()
	if err == nil {
		t.Fatal("lint accepted a prototype feature depending on deferred work")
	}
	for _, want := range []string{"M00-G01", "M00-G02", "POST-015"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the failure does not name %q: %v", want, err)
		}
	}
}

// TestAUDIT002_LintRejectsAnUnmappedFeature is the M00-G01 half of the same
// wiring.
func TestAUDIT002_LintRejectsAnUnmappedFeature(t *testing.T) {
	original := plantrace.PrototypeFeatures
	plantrace.PrototypeFeatures = append(
		append([]plantrace.Feature{}, original...),
		plantrace.Feature{Name: "A capability nothing accepts"},
	)
	t.Cleanup(func() { plantrace.PrototypeFeatures = original })

	err := checkPrototypeScopeManifest()
	if err == nil {
		t.Fatal("lint accepted a feature with no journey, measurement, or milestone")
	}
	for _, want := range []string{
		"maps to no user journey",
		"names no required measurement",
		"names no implementing milestone",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the failure does not report %q: %v", want, err)
		}
	}
}
