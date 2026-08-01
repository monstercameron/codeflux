package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeObligationFile(t *testing.T, root, relative, contents string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(relative), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write %s: %v", relative, err)
	}
}

func TestM01_071_TheObligationStaysSilentUntilAnAtomExists(t *testing.T) {
	// The plan blocks writing a reviewed example before a real atom exists, so
	// a tree with no atoms must pass without one. Failing here would push
	// somebody to invent the fake contract the plan forbids.
	root := t.TempDir()
	writeObligationFile(t, root, "internal/thing/thing.go",
		"package thing\n\n// Ordinary is not an atom.\nfunc Ordinary() {}\n")

	if err := checkAtomDocumentationObligations(root,
		[]string{filepath.FromSlash("internal/thing/thing.go")}); err != nil {
		t.Fatalf("a tree with no atoms was required to document one: %v", err)
	}
}

func TestM01_071_TheObligationFiresOnTheFirstRealAtom(t *testing.T) {
	root := t.TempDir()
	writeObligationFile(t, root, "internal/thing/thing.go",
		"package thing\n\n//codeflux:atom\n//\n// Compute does one thing.\nfunc Compute() {}\n")

	tracked := []string{filepath.FromSlash("internal/thing/thing.go")}
	err := checkAtomDocumentationObligations(root, tracked)
	if err == nil {
		t.Fatal("the first atom landed with no reviewed comment example required")
	}
	if !strings.Contains(err.Error(), "M01-071") {
		t.Errorf("the failure does not name the obligation: %v", err)
	}
	// The path is reported in the platform's own form, matching the neighbouring
	// checks, so a Windows reader can paste it straight back into a command.
	if !strings.Contains(err.Error(), filepath.FromSlash("internal/thing/thing.go")) {
		t.Errorf("the failure does not name the file that triggered it: %v", err)
	}

	// Once the example exists, the obligation is met.
	writeObligationFile(t, root, "docs/atoms.md",
		"# Atoms\n\n"+atomExampleAnchor+"\n\nA real, reviewed example goes here.\n")
	if err := checkAtomDocumentationObligations(root, tracked); err != nil {
		t.Fatalf("the documented case still failed: %v", err)
	}
}

func TestM01_071_ToolingAndFixturesAreNotMistakenForAtoms(t *testing.T) {
	// atoms.go names the marker in a string constant and the docs quote it.
	// Counting either as a declaration would make the obligation permanently
	// unsatisfiable, and the usual response to that is deleting the check.
	for name, source := range map[string]string{
		"marker in a string constant": "package main\n\nconst marker = \"" + atomMarker + "\"\n",
		"marker quoted in prose": "package main\n\n// The `//" + atomMarker +
			"` directive marks an atom.\nfunc Doc() {}\n",
		"marker as part of another directive": "package main\n\n//" + atomMarker +
			"-name-exception kind: reason\nfunc Other() {}\n",
	} {
		t.Run(name, func(t *testing.T) {
			if declaresAtom(source) {
				t.Fatalf("%s was counted as an atom declaration", name)
			}
		})
	}

	if !declaresAtom("package p\n\n//" + atomMarker + "\nfunc Real() {}\n") {
		t.Fatal("a real atom declaration was not recognised")
	}
	if !declaresAtom("package p\r\n\r\n//" + atomMarker + "\r\nfunc Real() {}\r\n") {
		t.Fatal("a real atom declaration with CRLF endings was not recognised")
	}
}

func TestM01_071_TestAndGeneratedFilesDoNotTriggerTheObligation(t *testing.T) {
	root := t.TempDir()
	atom := "package thing\n\n//" + atomMarker + "\nfunc Compute() {}\n"
	for _, relative := range []string{
		"internal/thing/thing_test.go",
		"internal/thing/versions_gen.go",
		"internal/thing/testdata/sample.go",
		"internal/thing/service.pb.go",
	} {
		writeObligationFile(t, root, relative, atom)
	}

	var tracked []string
	for _, relative := range []string{
		"internal/thing/thing_test.go",
		"internal/thing/versions_gen.go",
		"internal/thing/testdata/sample.go",
		"internal/thing/service.pb.go",
	} {
		tracked = append(tracked, filepath.FromSlash(relative))
	}
	if err := checkAtomDocumentationObligations(root, tracked); err != nil {
		t.Fatalf("a fixture or generated atom triggered the production obligation: %v", err)
	}
}

func TestM01_077_TheChecklistIsRequiredOnceATemplateExists(t *testing.T) {
	root := t.TempDir()
	if err := checkAtomDocumentationObligations(root, nil); err != nil {
		t.Fatalf("a repository with no template was required to have a checklist: %v", err)
	}

	for _, relative := range []string{
		".github/pull_request_template.md",
		".github/PULL_REQUEST_TEMPLATE.md",
		".github/PULL_REQUEST_TEMPLATE/default.md",
	} {
		t.Run(relative, func(t *testing.T) {
			templateRoot := t.TempDir()
			writeObligationFile(t, templateRoot, relative, "## Summary\n\n## Testing\n")
			tracked := []string{filepath.FromSlash(relative)}

			err := checkAtomDocumentationObligations(templateRoot, tracked)
			if err == nil {
				t.Fatalf("%s landed with no naming review section required", relative)
			}
			if !strings.Contains(err.Error(), "M01-077") {
				t.Errorf("the failure does not name the obligation: %v", err)
			}

			writeObligationFile(t, templateRoot, relative,
				"## Summary\n\n## "+namingChecklistAnchor+"\n\n- [ ] names reviewed\n")
			if err := checkAtomDocumentationObligations(templateRoot, tracked); err != nil {
				t.Fatalf("a template carrying the checklist still failed: %v", err)
			}
		})
	}
}

func TestM01_071_TheRepositoryItselfSatisfiesBothObligations(t *testing.T) {
	// This is the check that matters day to day: the real tree, in whatever
	// state it is in. It passes today because neither trigger has fired, and it
	// will start failing on the commit that fires one.
	root := repositoryRootForCommandGraph(t)
	tracked, err := trackedFiles(t.Context(), root)
	if err != nil {
		t.Skipf("cannot list tracked files: %v", err)
	}
	if err := checkAtomDocumentationObligations(root, tracked); err != nil {
		t.Fatalf("the repository does not satisfy its atom documentation obligations: %v", err)
	}
}
