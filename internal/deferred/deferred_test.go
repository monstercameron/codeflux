package deferred

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestPOST_001_RegistryCoversEveryDeferredItem(t *testing.T) {
	if err := ValidateRegistry(); err != nil {
		t.Fatalf("the deferral registry is not valid: %v", err)
	}
	if got, want := len(Registry()), 15; got != want {
		t.Fatalf("%d items declared, POST-001..015 needs %d", got, want)
	}
	for index, item := range Registry() {
		if got, want := string(item.ID), fmt.Sprintf("POST-%03d", index+1); got != want {
			t.Errorf("item %d is %q, want %q", index, got, want)
		}
	}
}

func TestPOST_001_ADeferralWithoutATriggerIsRefused(t *testing.T) {
	for name, damage := range map[string]func(*Item){
		"no capability": func(item *Item) { item.Capability = "" },
		"no trigger":    func(item *Item) { item.Trigger = "" },
		"no forbidden claims": func(item *Item) {
			item.ForbiddenClaims = nil
		},
		"no creep markers": func(item *Item) { item.CreepMarkers = nil },
		"short marker": func(item *Item) {
			item.CreepMarkers = []string{"graph"}
		},
		"self dependency": func(item *Item) { item.DependsOn = []ItemID{item.ID} },
		"not a POST id":   func(item *Item) { item.ID = "M24-001" },
	} {
		t.Run(name, func(t *testing.T) {
			item := Registry()[0]
			damage(&item)
			if err := item.Validate(); err == nil {
				t.Fatalf("a deferral with %s was accepted", name)
			}
		})
	}
}

func TestPOST_002_DependenciesAreThemselvesDeferredAndAcyclic(t *testing.T) {
	declared := map[ItemID]bool{}
	for _, item := range Registry() {
		declared[item.ID] = true
	}
	for _, item := range Registry() {
		for _, dependency := range item.DependsOn {
			if !declared[dependency] {
				t.Errorf("item %q depends on %q, which is not deferred", item.ID, dependency)
			}
		}
	}

	cyclic := []Item{
		{ID: "POST-001", Capability: "a", Trigger: "t", ForbiddenClaims: []string{"c"},
			CreepMarkers: []string{"MarkerA"}, DependsOn: []ItemID{"POST-002"}},
		{ID: "POST-002", Capability: "b", Trigger: "t", ForbiddenClaims: []string{"c"},
			CreepMarkers: []string{"MarkerB"}, DependsOn: []ItemID{"POST-001"}},
	}
	if err := ValidateDependencyOrder(cyclic); err == nil {
		t.Fatal("a cycle among deferred items was accepted")
	}
}

func TestPOST_003_AMarkerBelongsToExactlyOneItem(t *testing.T) {
	// A marker claimed twice makes a creep report ambiguous about which
	// deferral was broken, which is the one thing the report exists to say.
	owners := Markers()
	total := 0
	for _, item := range Registry() {
		total += len(item.CreepMarkers)
	}
	if len(owners) != total {
		t.Fatalf("%d markers map to %d owners; at least one is claimed twice",
			total, len(owners))
	}
}

func TestPOST_009_DeferredWorkCannotStartOnAnUnmetTrigger(t *testing.T) {
	err := Authorize("POST-009", nil)
	if !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("learned routing was authorized with no trigger met: %v", err)
	}
	if !strings.Contains(err.Error(), "shadow calibration") {
		t.Errorf("the refusal does not state the trigger: %v", err)
	}

	if err := Authorize("POST-009", map[ItemID]bool{"POST-009": true}); err != nil {
		t.Errorf("an item with its trigger met and no dependencies was refused: %v", err)
	}

	if err := Authorize("POST-999", map[ItemID]bool{"POST-999": true}); err == nil {
		t.Error("an undeclared item was authorized")
	}
}

func TestPOST_003_ADependencyChainCannotBeStartedAtTheInterestingEnd(t *testing.T) {
	// Graph-native atoms depend on the kernel, which depends on the experiment.
	// Authorising the atoms alone is the exact shortcut the chain exists to
	// prevent.
	err := Authorize("POST-003", map[ItemID]bool{"POST-003": true})
	if !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("graph-native atoms were authorized ahead of the kernel: %v", err)
	}
	if !strings.Contains(err.Error(), "POST-002") {
		t.Errorf("the refusal does not name the unmet dependency: %v", err)
	}

	if err := Authorize("POST-003", map[ItemID]bool{
		"POST-003": true, "POST-002": true,
	}); err != nil {
		t.Errorf("an item with its own chain authorized was refused: %v", err)
	}
}

// TestPOST_001_NoDeferredCapabilityHasCreptIntoTheTree is the check that gives
// the registry its value: it reads the real source tree rather than trusting
// the declaration.
func TestPOST_001_NoDeferredCapabilityHasCreptIntoTheTree(t *testing.T) {
	root := repositoryRootForDeferral(t)
	owners := Markers()

	var found []Creep
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			// .artifacts holds disposable output, and design/ is user-owned
			// material this repository does not govern.
			case ".git", ".artifacts", "design", "node_modules":
				return fs.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		// This registry names every marker by construction, so scanning it
		// would report itself.
		if strings.HasPrefix(relative, filepath.Join("internal", "deferred")) {
			return nil
		}
		contents, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		text := string(contents)
		for marker, id := range owners {
			if strings.Contains(text, marker) {
				found = append(found, Creep{Item: id, Marker: marker, Path: relative})
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk the repository: %v", err)
	}

	for _, creep := range found {
		t.Errorf(
			"%s declares %q as deferred, but %s appears in %s; if the trigger is now met, "+
				"authorize the item and remove it from the registry rather than leaving the "+
				"deferral in place",
			creep.Item, creep.Marker, creep.Marker, creep.Path)
	}
}

func TestPOST_001_TheCreepScanWouldActuallyFire(t *testing.T) {
	// A scan that has never fired is a scan nobody has shown works. This plants
	// a marker in a temporary tree and checks the same matching logic reports
	// it, so the passing result above means something.
	root := t.TempDir()
	planted := filepath.Join(root, "planted.go")
	marker := Registry()[0].CreepMarkers[0]
	if err := os.WriteFile(planted,
		[]byte("package planted\n\ntype "+marker+" struct{}\n"), 0o600); err != nil {
		t.Fatalf("write the planted file: %v", err)
	}

	var found []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || filepath.Ext(path) != ".go" {
			return walkErr
		}
		contents, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for candidate := range Markers() {
			if strings.Contains(string(contents), candidate) {
				found = append(found, candidate)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk the planted tree: %v", err)
	}
	if !slices.Contains(found, marker) {
		t.Fatalf("the scan did not report a planted %q", marker)
	}
}

func repositoryRootForDeferral(t *testing.T) string {
	t.Helper()
	// internal/deferred -> internal -> repository root.
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("repository root %q has no go.mod: %v", root, err)
	}
	return root
}
