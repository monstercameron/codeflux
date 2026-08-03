package executor

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestWritingNonGoIntoAGoFileIsRefused is the discrimination this guard needed.
//
// Observed twice in one ladder run on 2026-08-03: the model wrote a file whose
// first character was an asterisk and the build failed with "expected
// 'package', found '*'". Each occurrence cost a whole attempt — a write, a
// build, a send-back and a full rewrite — and one landed on the run's last
// attempt, so every structural stage recorded blocked because the module did
// not build.
//
// Proven to discriminate: before this guard, writeWorktreeFile wrote whatever
// it was handed and reported "wrote N bytes", so each of these cases succeeded
// here and failed minutes later at the compiler, attributed to the run rather
// than to the write.
func TestWritingNonGoIntoAGoFileIsRefused(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		content string
	}{
		{"a markdown heading", "**cmd/app/main.go**\n\npackage main\n"},
		{"a fenced block", "```go\npackage main\n\nfunc main() {}\n```\n"},
		{"a diff marker", "+++ package main\n"},
		{"prose before the source", "Here is the file:\npackage main\n"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			target := filepath.Join(t.TempDir(), "main.go")
			_, err := writeWorktreeFile(target, testCase.content)
			if err == nil {
				t.Fatal("a .go file that does not parse as Go must be refused " +
					"at the write, not discovered by the build one attempt later")
			}
			// Two shapes of refusal, both acceptable and each better for what
			// it covers: content that is not source at all is named by its
			// opening, and content that is nearly source is named by the
			// parser. What matters is that both say what to write instead.
			message := err.Error()
			if !strings.Contains(message, "does not parse as Go") &&
				!strings.Contains(message, "begins with a package clause") &&
				!strings.Contains(message, "patch syntax") {
				t.Errorf("the refusal should say what is wrong, got %q", err)
			}
			if !strings.Contains(message, "no surrounding prose") &&
				!strings.Contains(message, "complete text") &&
				!strings.Contains(message, "Send it to apply-patch") {
				t.Errorf("the refusal should say what to write instead, got %q", err)
			}
		})
	}
}

// TestWritingRealGoIsAccepted is the control.
//
// The guard checks that the content parses, and nothing more. Code that is
// wrong, will not compile, or has an unused import is the compiler's business
// and the flow's; refusing it here would turn a guard on the file into an
// opinion about the code.
func TestWritingRealGoIsAccepted(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		content string
	}{
		{"an ordinary command", "package main\n\nimport \"fmt\"\n\n" +
			"func main() {\n\tfmt.Println(\"hello\")\n}\n"},
		{"code that will not compile but does parse",
			"package main\n\nfunc main() {\n\tundefinedName()\n}\n"},
		{"an unused import, which vet dislikes and the parser accepts",
			"package main\n\nimport \"os\"\n\nfunc main() {}\n"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			target := filepath.Join(t.TempDir(), "main.go")
			if _, err := writeWorktreeFile(target, testCase.content); err != nil {
				t.Fatalf("this parses as Go and must be written: %v", err)
			}
		})
	}
}

// TestTheGuardOnlyAppliesToGoFilesOutsideTestdata pins its boundaries.
//
// A non-Go file is none of this guard's business, and a file under testdata is
// deliberately outside the build by the same convention the Go toolchain uses
// — a fixture that does not parse is sometimes exactly the point.
func TestTheGuardOnlyAppliesToGoFilesOutsideTestdata(t *testing.T) {
	root := t.TempDir()
	for _, testCase := range []struct {
		name string
		path string
	}{
		{"markdown", filepath.Join(root, "README.md")},
		{"a go file under testdata", filepath.Join(root, "testdata", "broken.go")},
		{"a go file in a nested testdata", filepath.Join(root, "pkg", "testdata", "broken.go")},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := writeWorktreeFile(
				testCase.path, "**this is not Go**\n",
			); err != nil {
				t.Fatalf("%s is outside this guard's business: %v",
					testCase.path, err)
			}
		})
	}
}
