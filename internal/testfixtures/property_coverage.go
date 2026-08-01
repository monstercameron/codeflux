package testfixtures

import (
	"fmt"
	"sort"
	"strings"
)

// PropertyArea names one behaviour M22-015..027 requires be covered by unit
// or property tests.
type PropertyArea string

const (
	AreaDomainTransitions      PropertyArea = "domain-transition-validators"
	AreaMoneyAndBudget         PropertyArea = "exact-money-and-budget-arithmetic"
	AreaFingerprintCanonical   PropertyArea = "task-fingerprint-canonicalization"
	AreaContextRanking         PropertyArea = "context-ranking-determinism"
	AreaPermissionMatching     PropertyArea = "permission-matching-and-expiration"
	AreaGraphProjection        PropertyArea = "graph-projection-determinism"
	AreaGraphLayout            PropertyArea = "graph-layout-stability"
	AreaAssuranceInvalidation  PropertyArea = "assurance-and-evidence-invalidation"
	AreaRetrievalApplicability PropertyArea = "retrieval-applicability-predicates"
	AreaFuzzIdentifiers        PropertyArea = "fuzz-identifier-and-cursor-parsers"
	AreaFuzzProtoConversion    PropertyArea = "fuzz-protobuf-domain-conversion"
	AreaFuzzPathResolution     PropertyArea = "fuzz-safe-path-resolution"
	AreaFuzzEventReplay        PropertyArea = "fuzz-event-replay-projection"
)

// CoverageRequirement binds one required area to the package that must
// carry its tests and the TODO that demands it.
//
// This exists because "we have tests for that" is exactly the claim this
// milestone must not take on faith. The registry is machine-checked against
// the real tree by TestM22_CoverageRegistryMatchesTheRepository, so an area
// whose tests are deleted or whose package is renamed fails loudly instead
// of quietly becoming a lie.
type CoverageRequirement struct {
	Area    PropertyArea
	TodoID  string
	Package string
	// Evidence names a symbol or test-name fragment that must exist in the
	// package for the requirement to be considered met.
	Evidence string
}

// PropertyCoverageRegistry is the full M22-015..027 requirement set.
func PropertyCoverageRegistry() []CoverageRequirement {
	return []CoverageRequirement{
		{AreaDomainTransitions, "M22-015", "internal/domain", "Transition"},
		{AreaMoneyAndBudget, "M22-016", "internal/domain", "Money"},
		{AreaFingerprintCanonical, "M22-017", "internal/fingerprint", "Canonical"},
		{AreaContextRanking, "M22-018", "internal/retrieval", "discover"},
		{AreaPermissionMatching, "M22-019", "internal/executor", "Authority"},
		{AreaGraphProjection, "M22-020", "internal/graph", "Revision"},
		{AreaGraphLayout, "M22-021", "internal/graphlayout", "Layout"},
		{AreaAssuranceInvalidation, "M22-022", "internal/domain", "Maturity"},
		{AreaRetrievalApplicability, "M22-023", "internal/retrievalgate", "Applicability"},
		{AreaFuzzIdentifiers, "M22-024", "internal/domain", "Fuzz"},
		{AreaFuzzProtoConversion, "M22-025", "internal/transport", "Fuzz"},
		{AreaFuzzPathResolution, "M22-026", "internal/gitwork", "Fuzz"},
		{AreaFuzzEventReplay, "M22-027", "internal/events", "Fuzz"},
	}
}

// Validate rejects a malformed registry.
func (requirement CoverageRequirement) Validate() error {
	switch {
	case strings.TrimSpace(string(requirement.Area)) == "":
		return fmt.Errorf("coverage requirement has no area")
	case !strings.HasPrefix(requirement.TodoID, "M22-"):
		return fmt.Errorf("area %q must cite its M22 TODO, got %q", requirement.Area, requirement.TodoID)
	case !strings.HasPrefix(requirement.Package, "internal/"):
		return fmt.Errorf("area %q must name an internal package, got %q", requirement.Area, requirement.Package)
	case strings.TrimSpace(requirement.Evidence) == "":
		return fmt.Errorf("area %q must name the evidence proving it", requirement.Area)
	}
	return nil
}

// ValidatePropertyCoverageRegistry checks the registry covers every required
// area exactly once and cites a distinct TODO for each.
func ValidatePropertyCoverageRegistry() error {
	registry := PropertyCoverageRegistry()
	areas := map[PropertyArea]bool{}
	todos := map[string]PropertyArea{}
	for _, requirement := range registry {
		if err := requirement.Validate(); err != nil {
			return err
		}
		if areas[requirement.Area] {
			return fmt.Errorf("area %q is registered twice", requirement.Area)
		}
		areas[requirement.Area] = true
		if other, clash := todos[requirement.TodoID]; clash {
			return fmt.Errorf("areas %q and %q both claim %s", other, requirement.Area, requirement.TodoID)
		}
		todos[requirement.TodoID] = requirement.Area
	}
	if len(registry) != 13 {
		return fmt.Errorf("M22-015..027 declares 13 areas, registry has %d", len(registry))
	}
	return nil
}

// RegisteredTodoIDs returns the cited TODO identifiers, sorted.
func RegisteredTodoIDs() []string {
	registry := PropertyCoverageRegistry()
	ids := make([]string, 0, len(registry))
	for _, requirement := range registry {
		ids = append(ids, requirement.TodoID)
	}
	sort.Strings(ids)
	return ids
}
