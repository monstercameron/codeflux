package plantrace

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// milestoneID matches the milestone identifiers the plan's milestone-to-layer
// map declares.
var milestoneID = regexp.MustCompile(`^M[0-9]{2}$`)

// Validate reports every way the manifest fails the plan's two scope rules.
//
// It returns all findings rather than the first, because a manifest is edited
// in bulk and stopping at the first error hides the rest until the next run.
func Validate() []string {
	var findings []string

	features := make(map[string]Feature, len(PrototypeFeatures))
	for _, feature := range PrototypeFeatures {
		if _, duplicate := features[feature.Name]; duplicate {
			findings = append(findings,
				fmt.Sprintf("prototype feature %q is declared twice", feature.Name))
			continue
		}
		features[feature.Name] = feature
	}

	deferredByName := make(map[string]Deferred, len(DeferredFeatures))
	deferredByID := make(map[string]Deferred, len(DeferredFeatures))
	for _, item := range DeferredFeatures {
		if item.ID == "" || item.Name == "" {
			findings = append(findings,
				fmt.Sprintf("deferred entry %+v is missing an identifier or a name", item))
			continue
		}
		// M00-G01: a deferral with no gate is a capability nobody can ever
		// activate and nobody documented as removed.
		if strings.TrimSpace(item.BranchGate) == "" {
			findings = append(findings,
				fmt.Sprintf("deferred %s (%q) names no branch gate", item.ID, item.Name))
		}
		if _, duplicate := deferredByID[item.ID]; duplicate {
			findings = append(findings,
				fmt.Sprintf("deferred identifier %s is declared twice", item.ID))
			continue
		}
		deferredByID[item.ID] = item
		deferredByName[item.Name] = item
	}

	for _, feature := range PrototypeFeatures {
		// M00-G01: an unmapped feature is one the product ships without
		// anything accepting it.
		if strings.TrimSpace(feature.Journey) == "" {
			findings = append(findings,
				fmt.Sprintf("prototype feature %q maps to no user journey", feature.Name))
		}
		if strings.TrimSpace(feature.Measurement) == "" {
			findings = append(findings,
				fmt.Sprintf("prototype feature %q names no required measurement", feature.Name))
		}
		if len(feature.Milestones) == 0 {
			findings = append(findings,
				fmt.Sprintf("prototype feature %q names no implementing milestone", feature.Name))
		}
		for _, milestone := range feature.Milestones {
			if !milestoneID.MatchString(milestone) {
				findings = append(findings, fmt.Sprintf(
					"prototype feature %q names milestone %q, which is not an MNN identifier",
					feature.Name, milestone))
			}
		}

		for _, dependency := range feature.DependsOn {
			// M00-G02: this is the edge the plan forbids. A prototype journey
			// that depends on withheld work is a journey that cannot complete,
			// and the deferral is what stops anyone noticing.
			if item, deferred := deferredByName[dependency]; deferred {
				findings = append(findings, fmt.Sprintf(
					"prototype feature %q depends on deferred %s (%q), which the plan forbids; "+
						"it may only become active once %s",
					feature.Name, item.ID, item.Name, item.BranchGate))
				continue
			}
			if item, deferred := deferredByID[dependency]; deferred {
				findings = append(findings, fmt.Sprintf(
					"prototype feature %q depends on deferred %s, which the plan forbids",
					feature.Name, item.ID))
				continue
			}
			if _, known := features[dependency]; !known {
				findings = append(findings, fmt.Sprintf(
					"prototype feature %q depends on %q, which is in neither manifest",
					feature.Name, dependency))
			}
		}
	}

	findings = append(findings, findDependencyCycles(features)...)
	sort.Strings(findings)
	return findings
}

// findDependencyCycles reports dependency cycles among prototype features.
//
// A cycle is not merely untidy: the milestone order in TODOS.md is a total
// order, so a cycle means at least one feature must be built before something
// it requires, which is the dependency-inversion the build order exists to
// prevent.
func findDependencyCycles(features map[string]Feature) []string {
	const (
		unvisited = 0
		onStack   = 1
		done      = 2
	)
	state := make(map[string]int, len(features))
	var findings []string

	var walk func(name string, path []string)
	walk = func(name string, path []string) {
		switch state[name] {
		case done:
			return
		case onStack:
			start := 0
			for index, entry := range path {
				if entry == name {
					start = index
					break
				}
			}
			findings = append(findings, "prototype feature dependency cycle: "+
				strings.Join(append(append([]string{}, path[start:]...), name), " -> "))
			return
		}
		state[name] = onStack
		path = append(path, name)
		for _, dependency := range features[name].DependsOn {
			if _, known := features[dependency]; known {
				walk(dependency, path)
			}
		}
		state[name] = done
	}

	names := make([]string, 0, len(features))
	for name := range features {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		walk(name, nil)
	}
	return findings
}
