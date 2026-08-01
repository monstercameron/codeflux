package testfixtures

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repositoryRoot walks up to the module root so the registry can be checked
// against the real tree rather than against a hardcoded path.
func repositoryRoot(t *testing.T) string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for depth := 0; depth < 12; depth++ {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			break
		}
		directory = parent
	}
	t.Fatal("could not locate the module root")
	return ""
}

// TestM22_015_027_CoverageRegistryIsCoherent proves the requirement set
// itself is well formed: thirteen areas, each citing a distinct TODO.
func TestM22_015_027_CoverageRegistryIsCoherent(t *testing.T) {
	if err := ValidatePropertyCoverageRegistry(); err != nil {
		t.Fatal(err)
	}
	ids := RegisteredTodoIDs()
	if len(ids) != 13 {
		t.Fatalf("registered %d TODO ids, want 13", len(ids))
	}
	for _, id := range ids {
		if !strings.HasPrefix(id, "M22-0") {
			t.Fatalf("unexpected TODO id %q", id)
		}
	}
}

// TestM22_015_027_CoverageRegistryMatchesTheRepository is the honest half:
// it checks every registered area's package EXISTS and carries test files
// mentioning the declared evidence.
//
// Without this the registry would be a list of intentions. With it, deleting
// the tests behind an area, or renaming its package, fails here — so the
// registry cannot drift into a claim the tree does not support.
func TestM22_015_027_CoverageRegistryMatchesTheRepository(t *testing.T) {
	root := repositoryRoot(t)
	for _, requirement := range PropertyCoverageRegistry() {
		packageDirectory := filepath.Join(root, filepath.FromSlash(requirement.Package))
		info, err := os.Stat(packageDirectory)
		if err != nil || !info.IsDir() {
			t.Errorf("%s: package %s does not exist", requirement.TodoID, requirement.Package)
			continue
		}
		entries, err := os.ReadDir(packageDirectory)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			contents, err := os.ReadFile(filepath.Join(packageDirectory, entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(contents), requirement.Evidence) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s (%s): no test file in %s mentions %q; the registry claims coverage the tree does not show",
				requirement.TodoID, requirement.Area, requirement.Package, requirement.Evidence)
		}
	}
}

// TestM22_024_027_FuzzTargetsExist proves each declared fuzz area really has
// a Go fuzz target, not merely a table-driven test that resembles one.
func TestM22_024_027_FuzzTargetsExist(t *testing.T) {
	root := repositoryRoot(t)
	required := map[string]string{
		"M22-024": "internal/domain",
		"M22-025": "internal/transport",
		"M22-026": "internal/gitwork",
		"M22-027": "internal/events",
	}
	for todo, packagePath := range required {
		entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(packagePath)))
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			contents, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(packagePath), entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(contents), "func Fuzz") && strings.Contains(string(contents), "f.Fuzz(") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s: %s declares no Go fuzz target", todo, packagePath)
		}
	}
}
