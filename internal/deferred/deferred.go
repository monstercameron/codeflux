// Package deferred is the registry of capabilities the plan defers until after
// prototype exit (POST-001..015), together with the machine check that keeps
// each deferral honest.
//
// A deferral recorded only as prose decays the same way every time: the
// capability arrives early, in a partial form, justified by a reason nobody
// wrote down, and the trigger that was supposed to authorise it is discovered
// afterwards to have never been met. Declaring each item as data — with its
// authorising trigger, the claims its absence forbids, and the markers that
// would show it had crept in — turns "we agreed not to build this yet" into
// something a test can fail on.
package deferred

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// ItemID is a POST identifier.
type ItemID string

// Item is one deferred capability.
type Item struct {
	ID ItemID
	// Capability is what is deferred, in the plan's terms.
	Capability string
	// Trigger is the evidence that would authorise starting it. It is stated so
	// it can be checked rather than argued about: "if it becomes a problem" is
	// not a trigger, it is a mood.
	Trigger string
	// DependsOn are the deferred items that must land first. A deferral chain
	// matters because the temptation is always to start at the interesting end.
	DependsOn []ItemID
	// ForbiddenClaims are the things nobody may assert while this is deferred.
	// Recording them is what stops the roadmap from describing the capability
	// as though it existed.
	ForbiddenClaims []string
	// CreepMarkers are identifiers whose presence in the tree would show the
	// capability arrived early. They are deliberately specific: a marker broad
	// enough to match ordinary code is a marker that gets deleted the first
	// time it fires falsely.
	CreepMarkers []string
}

// Registry returns the deferred capabilities (POST-001..015).
func Registry() []Item {
	return []Item{
		{
			ID:         "POST-001",
			Capability: "the disposable graph-medium experiment",
			Trigger: "prototype exit is decided, and the graph experiment is run as a " +
				"disposable artifact rather than as the first version of production code",
			ForbiddenClaims: []string{
				"that a semantic graph medium is the right representation",
			},
			CreepMarkers: []string{"GraphMedium", "SemanticMedium"},
		},
		{
			ID:         "POST-002",
			Capability: "a scoped and frozen tier-zero kernel",
			Trigger:    "the graph-medium experiment passes on its own stated terms",
			DependsOn:  []ItemID{"POST-001"},
			ForbiddenClaims: []string{
				"that the kernel's scope is known",
			},
			CreepMarkers: []string{"TierZeroKernel"},
		},
		{
			ID:              "POST-003",
			Capability:      "graph-native atoms",
			Trigger:         "the kernel scope is accepted",
			DependsOn:       []ItemID{"POST-002"},
			ForbiddenClaims: []string{"that atoms have a graph-native representation"},
			CreepMarkers:    []string{"GraphNativeAtom"},
		},
		{
			ID:         "POST-004",
			Capability: "modeled Go atoms and reference models",
			Trigger: "correlation controls are specified, so a model's agreement with the " +
				"code it models is evidence rather than a restatement",
			DependsOn:       []ItemID{"POST-003"},
			ForbiddenClaims: []string{"that a reference model independently confirms an atom"},
			CreepMarkers:    []string{"ReferenceModel", "ModeledAtom"},
		},
		{
			ID:              "POST-005",
			Capability:      "Go lowering and source maps",
			Trigger:         "the lowering conformance suite is frozen",
			DependsOn:       []ItemID{"POST-004"},
			ForbiddenClaims: []string{"that lowering preserves semantics"},
			CreepMarkers:    []string{"LoweringPass", "SourceMapEmitter"},
		},
		{
			ID:         "POST-006",
			Capability: "determinism conformance across architecture and toolchain matrices",
			Trigger:    "the deep-verification track is separately authorised",
			ForbiddenClaims: []string{
				"that results are reproducible across architectures",
			},
			CreepMarkers: []string{"DeterminismMatrix"},
		},
		{
			ID:              "POST-007",
			Capability:      "request-side effect proof obligations",
			Trigger:         "the medium and validator gates both pass",
			DependsOn:       []ItemID{"POST-001"},
			ForbiddenClaims: []string{"that effects are proven rather than approved"},
			CreepMarkers:    []string{"EffectProofObligation"},
		},
		{
			ID:              "POST-008",
			Capability:      "semantic graph diff",
			Trigger:         "immutable semantic revisions exist to diff between",
			DependsOn:       []ItemID{"POST-003"},
			ForbiddenClaims: []string{"that two program versions can be compared semantically"},
			CreepMarkers:    []string{"SemanticDiff"},
		},
		{
			ID:         "POST-009",
			Capability: "learned routing",
			Trigger: "fixed-policy telemetry and shadow calibration both pass, from runs " +
				"where routing was the only variable",
			ForbiddenClaims: []string{
				"that routing adapts to the task",
				"that model selection improves outcome per unit cost",
			},
			CreepMarkers: []string{"LearnedRouter", "AdaptiveRouting"},
		},
		{
			ID:         "POST-010",
			Capability: "advisory patterns",
			Trigger: "clean-room evaluation and lineage independence are both established, " +
				"so an advisory cannot be advice derived from the answer",
			ForbiddenClaims: []string{"that the system advises from learned patterns"},
			CreepMarkers:    []string{"AdvisoryPattern"},
		},
		{
			ID:         "POST-011",
			Capability: "promotion of mechanical rules",
			Trigger: "replay, false-positive, expiry, override, and demotion governance all " +
				"exist, so a promoted rule can also be demoted",
			ForbiddenClaims: []string{"that a learned rule is authoritative"},
			CreepMarkers:    []string{"RulePromotion"},
		},
		{
			ID:         "POST-012",
			Capability: "ANN and dedicated vector infrastructure",
			Trigger: "SQLite brute-force retrieval is measured as a bottleneck on real " +
				"corpora, not predicted to become one",
			ForbiddenClaims: []string{"that retrieval requires an index to be fast enough"},
			// A dependency on an external ANN library is the unambiguous marker;
			// the words "vector" and "index" appear all over ordinary code.
			CreepMarkers: []string{"HNSWIndex", "ANNIndex"},
		},
		{
			ID:         "POST-013",
			Capability: "multi-agent orchestration",
			Trigger: "the single-agent baseline exposes a measured bottleneck that a second " +
				"agent would relieve",
			ForbiddenClaims: []string{
				"that parallel agents complete work faster",
				"that a reviewer agent improves acceptance",
			},
			CreepMarkers: []string{"AgentPool", "MultiAgentOrchestrator"},
		},
		{
			ID:         "POST-014",
			Capability: "hosted sync, teams, enterprise identity, policy administration, and audit export",
			Trigger:    "there is hobbyist product evidence that anyone wants the local product first",
			ForbiddenClaims: []string{
				"that the product supports teams",
				"that state syncs between machines",
			},
			CreepMarkers: []string{"HostedSync", "TenantDirectory", "PolicyAdministration"},
		},
		{
			ID:         "POST-015",
			Capability: "direct graph editing",
			Trigger: "user studies show conversational revision is insufficient, rather than " +
				"merely slower than an editor would feel",
			ForbiddenClaims: []string{"that the graph is an editing surface"},
			CreepMarkers:    []string{"GraphEditor", "EditableGraphNode"},
		},
	}
}

// Validate rejects a deferral that would not constrain anything.
func (item Item) Validate() error {
	switch {
	case !strings.HasPrefix(string(item.ID), "POST-"):
		return fmt.Errorf("item %q is not a POST identifier", item.ID)
	case strings.TrimSpace(item.Capability) == "":
		return fmt.Errorf("item %q defers nothing in particular", item.ID)
	case strings.TrimSpace(item.Trigger) == "":
		return fmt.Errorf(
			"item %q records no authorising trigger, so nothing distinguishes it from an "+
				"item everyone forgot", item.ID)
	case len(item.ForbiddenClaims) == 0:
		return fmt.Errorf(
			"item %q records nothing its absence forbids claiming, so the capability can "+
				"be described as though it existed", item.ID)
	case len(item.CreepMarkers) == 0:
		return fmt.Errorf(
			"item %q declares no marker that would show it arrived early, so the deferral "+
				"cannot be checked", item.ID)
	}
	for _, marker := range item.CreepMarkers {
		// A marker short enough to appear inside ordinary identifiers fires
		// falsely and gets deleted, which is worse than having no check.
		if len(marker) < 6 {
			return fmt.Errorf(
				"item %q declares marker %q, which is short enough to match ordinary code",
				item.ID, marker)
		}
	}
	if slices.Contains(item.DependsOn, item.ID) {
		return fmt.Errorf("item %q depends on itself", item.ID)
	}
	return nil
}

// ValidateRegistry checks the registry covers POST-001..015 and that every
// dependency is itself deferred.
func ValidateRegistry() error {
	items := Registry()
	seen := map[ItemID]bool{}
	for _, item := range items {
		if err := item.Validate(); err != nil {
			return err
		}
		if seen[item.ID] {
			return fmt.Errorf("item %q is declared twice", item.ID)
		}
		seen[item.ID] = true
	}
	for number := 1; number <= 15; number++ {
		id := ItemID(fmt.Sprintf("POST-%03d", number))
		if !seen[id] {
			return fmt.Errorf("no item is declared for %s", id)
		}
	}
	for _, item := range items {
		for _, dependency := range item.DependsOn {
			if !seen[dependency] {
				return fmt.Errorf("item %q depends on %q, which is not deferred",
					item.ID, dependency)
			}
		}
	}
	// A marker claimed by two items would make a creep report ambiguous about
	// which deferral was broken.
	owner := map[string]ItemID{}
	for _, item := range items {
		for _, marker := range item.CreepMarkers {
			if previous, claimed := owner[marker]; claimed {
				return fmt.Errorf("marker %q is claimed by both %q and %q",
					marker, previous, item.ID)
			}
			owner[marker] = item.ID
		}
	}
	return ValidateDependencyOrder(items)
}

// ValidateDependencyOrder rejects a dependency cycle among deferred items.
func ValidateDependencyOrder(items []Item) error {
	byID := map[ItemID]Item{}
	for _, item := range items {
		byID[item.ID] = item
	}
	const (
		unvisited = 0
		visiting  = 1
		done      = 2
	)
	state := map[ItemID]int{}
	var walk func(ItemID, []string) error
	walk = func(id ItemID, path []string) error {
		switch state[id] {
		case done:
			return nil
		case visiting:
			return fmt.Errorf("deferred items form a cycle: %s",
				strings.Join(append(path, string(id)), " -> "))
		}
		state[id] = visiting
		for _, dependency := range byID[id].DependsOn {
			if err := walk(dependency, append(path, string(id))); err != nil {
				return err
			}
		}
		state[id] = done
		return nil
	}
	for _, item := range items {
		if err := walk(item.ID, nil); err != nil {
			return err
		}
	}
	return nil
}

// Creep is one deferred capability found in the tree.
type Creep struct {
	Item   ItemID
	Marker string
	Path   string
}

// Markers returns every declared creep marker with its owning item.
func Markers() map[string]ItemID {
	owners := map[string]ItemID{}
	for _, item := range Registry() {
		for _, marker := range item.CreepMarkers {
			owners[marker] = item.ID
		}
	}
	return owners
}

// ErrNotAuthorized reports an attempt to start deferred work.
var ErrNotAuthorized = errors.New("the work is deferred and its trigger is not met")

// Authorize reports whether deferred work may begin.
//
// It refuses on an unmet trigger and on an unmet dependency, and it refuses to
// take the caller's word for either: the caller states which triggers are met,
// and the function checks the chain. The alternative — a boolean the caller
// sets — is the mechanism by which every deferral in every project has ever
// been quietly skipped.
func Authorize(id ItemID, metTriggers map[ItemID]bool) error {
	if err := ValidateRegistry(); err != nil {
		return err
	}
	byID := map[ItemID]Item{}
	for _, item := range Registry() {
		byID[item.ID] = item
	}
	item, declared := byID[id]
	if !declared {
		return fmt.Errorf("%q is not a deferred item", id)
	}
	if !metTriggers[id] {
		return fmt.Errorf("%w: %s needs %s", ErrNotAuthorized, id, item.Trigger)
	}
	var blocked []string
	for _, dependency := range item.DependsOn {
		if !metTriggers[dependency] {
			blocked = append(blocked, string(dependency))
		}
	}
	if len(blocked) > 0 {
		sort.Strings(blocked)
		return fmt.Errorf("%w: %s depends on %s, which %s not authorized",
			ErrNotAuthorized, id, strings.Join(blocked, ", "),
			map[bool]string{true: "are", false: "is"}[len(blocked) > 1])
	}
	return nil
}
