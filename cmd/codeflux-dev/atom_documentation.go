package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// M01-071 and M01-077 are documentation obligations the plan deliberately
// blocks until their subject exists: a reviewed atom-comment example may not be
// written before a real atom exists, and a naming review checklist may not be
// added to a pull-request template before the repository has one.
//
// Writing either early produces the thing the plan names as the failure —
// an invented production contract, reviewed by nobody, that later reads as
// precedent. Leaving them as prose, though, produces the other failure: the
// precondition is met months later and nobody notices. These checks watch for
// the precondition and fail the moment it holds without the obligation met.
const (
	// atomExampleAnchor is the heading the reviewed example lives under. It is
	// matched rather than the prose so the example can be rewritten freely.
	atomExampleAnchor = "### Reviewed atom comment example"
	// namingChecklistAnchor is the heading the naming review checklist lives
	// under in a pull-request template.
	namingChecklistAnchor = "Atom naming review"
)

// pullRequestTemplatePaths are the locations GitHub reads a template from.
func pullRequestTemplatePaths() []string {
	return []string{
		filepath.Join(".github", "pull_request_template.md"),
		filepath.Join(".github", "PULL_REQUEST_TEMPLATE.md"),
		filepath.Join(".github", "PULL_REQUEST_TEMPLATE", "default.md"),
		"pull_request_template.md",
	}
}

// checkAtomDocumentationObligations enforces M01-071 and M01-077.
//
// Both are no-ops until their trigger fires, and both fail loudly the moment it
// does. Reporting the obligation with its TODO identifier matters: whoever adds
// the first atom is not necessarily the person who read the plan section that
// requires the example.
func checkAtomDocumentationObligations(root string, tracked []string) error {
	if err := checkAtomExampleObligation(root, tracked); err != nil {
		return err
	}
	return checkNamingChecklistObligation(root, tracked)
}

// checkAtomExampleObligation fires once a real atom exists (M01-071).
func checkAtomExampleObligation(root string, tracked []string) error {
	declaring, err := trackedFilesDeclaringAtoms(root, tracked)
	if err != nil {
		return err
	}
	if len(declaring) == 0 {
		return nil
	}

	documented, err := documentationContains(root, atomExampleAnchor)
	if err != nil {
		return err
	}
	if documented {
		return nil
	}
	return fmt.Errorf(
		"M01-071: %s declares the first atom, so docs must carry a reviewed atom comment "+
			"example under %q; the plan blocks writing one earlier because an invented "+
			"contract reads as precedent once it is in the tree",
		declaring[0], atomExampleAnchor)
}

// checkNamingChecklistObligation fires once a pull-request template exists
// (M01-077).
func checkNamingChecklistObligation(root string, tracked []string) error {
	var templates []string
	for _, relative := range tracked {
		normalized := filepath.ToSlash(relative)
		for _, candidate := range pullRequestTemplatePaths() {
			if strings.EqualFold(normalized, filepath.ToSlash(candidate)) {
				templates = append(templates, relative)
			}
		}
	}
	if len(templates) == 0 {
		return nil
	}

	for _, relative := range templates {
		contents, err := os.ReadFile(filepath.Join(root, relative))
		if err != nil {
			return fmt.Errorf("read %s: %w", relative, err)
		}
		if !strings.Contains(string(contents), namingChecklistAnchor) {
			return fmt.Errorf(
				"M01-077: %s is a pull-request template, so it must carry a %q section; "+
					"a naming rule nobody is asked about at review time is a naming rule "+
					"nobody applies", relative, namingChecklistAnchor)
		}
	}
	return nil
}

// trackedFilesDeclaringAtoms returns the tracked Go files carrying a real atom
// declaration, in tracked order.
//
// Testdata and generated files are excluded for the same reason
// checkAtomDeclarations excludes them: a fixture atom is not a production
// contract, and requiring documentation for one would push people to stop
// writing fixtures.
func trackedFilesDeclaringAtoms(root string, tracked []string) ([]string, error) {
	var declaring []string
	for _, relative := range tracked {
		slashed := filepath.ToSlash(relative)
		if filepath.Ext(relative) != ".go" || strings.Contains(slashed, "/testdata/") ||
			strings.HasSuffix(relative, ".pb.go") || strings.HasSuffix(relative, "_gen.go") ||
			strings.HasSuffix(relative, "_test.go") {
			continue
		}
		contents, err := os.ReadFile(filepath.Join(root, relative))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", relative, err)
		}
		if declaresAtom(string(contents)) {
			declaring = append(declaring, relative)
		}
	}
	return declaring, nil
}

// declaresAtom reports whether source carries the atom marker on its own
// comment line.
//
// The marker is matched as a whole line so the tooling that names it in a
// string constant, and the documentation that quotes it, do not count as
// declarations.
func declaresAtom(source string) bool {
	for _, line := range strings.Split(source, "\n") {
		trimmed := strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if trimmed == "//"+atomMarker {
			return true
		}
	}
	return false
}

// documentationContains reports whether any tracked Markdown file under docs/
// carries the anchor.
func documentationContains(root, anchor string) (bool, error) {
	entries, err := os.ReadDir(filepath.Join(root, "docs"))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read docs: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		contents, err := os.ReadFile(filepath.Join(root, "docs", entry.Name()))
		if err != nil {
			return false, fmt.Errorf("read docs/%s: %w", entry.Name(), err)
		}
		if strings.Contains(string(contents), anchor) {
			return true, nil
		}
	}
	return false, nil
}
