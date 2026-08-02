package coordinator

import (
	"strings"
	"testing"
)

// findingsFor runs the detector over one file and returns what it said.
func findingsFor(t *testing.T, source string) []antiPattern {
	t.Helper()
	worktree := writeWorktree(t, map[string]string{
		"cmd/thing/main.go": source,
	})
	found, err := findAntiPatterns(worktree)
	if err != nil {
		t.Fatal(err)
	}
	return found
}

// mentions reports whether any finding names the given text.
func mentions(found []antiPattern, text string) bool {
	for _, pattern := range found {
		if strings.Contains(pattern.What, text) ||
			strings.Contains(pattern.Why, text) {
			return true
		}
	}
	return false
}

// TestASwallowedErrorIsCaught is the one the model actually produces.
//
// Every generated program in this session that mishandled input did it this
// way: notice the error, return nothing, exit zero. No test written against
// that behaviour would ever fail, which is why it needs looking for.
func TestASwallowedErrorIsCaught(t *testing.T) {
	found := findingsFor(t, "package main\n\n"+
		"// Load reads a thing.\n"+
		"func Load() error { return nil }\n\n"+
		"func main() {\n\tif err := Load(); err != nil {\n\t\treturn\n\t}\n}\n")
	if !mentions(found, "returns without it") {
		t.Errorf("an error noticed and dropped was not reported: %+v", found)
	}
}

// TestAnEmptyErrorHandlerIsCaught covers the version that looks even more
// careful and does even less.
func TestAnEmptyErrorHandlerIsCaught(t *testing.T) {
	found := findingsFor(t, "package main\n\n"+
		"// Load reads a thing.\n"+
		"func Load() error { return nil }\n\n"+
		"func main() {\n\tif err := Load(); err != nil {\n\t}\n}\n")
	if !mentions(found, "handler is empty") {
		t.Errorf("an empty error handler was not reported: %+v", found)
	}
}

// TestPackageLevelStateIsCaught is the reason a test passes alone and fails in
// a suite.
func TestPackageLevelStateIsCaught(t *testing.T) {
	found := findingsFor(t, "package main\n\nvar counter int\n\n"+
		"// Bump increases the counter.\nfunc Bump() { counter++ }\n\n"+
		"func main() {}\n")
	if !mentions(found, "counter") {
		t.Errorf("package-level mutable state was not reported: %+v", found)
	}
}

// TestAnUncheckedAssertionIsCaught turns a crash back into a decision.
func TestAnUncheckedAssertionIsCaught(t *testing.T) {
	found := findingsFor(t, "package main\n\n"+
		"// Widen returns the value as a string.\n"+
		"func Widen(value any) string {\n\ttext := value.(string)\n\treturn text\n}\n\n"+
		"func main() {}\n")
	if !mentions(found, "type assertion") {
		t.Errorf("an unchecked type assertion was not reported: %+v", found)
	}
}

// TestAPanicInLibraryCodeIsCaught keeps the decision with the caller.
func TestAPanicInLibraryCodeIsCaught(t *testing.T) {
	found := findingsFor(t, "package main\n\n"+
		"// Must refuses anything it dislikes.\n"+
		"func Must(ok bool) {\n\tif !ok {\n\t\tpanic(\"no\")\n\t}\n}\n\n"+
		"func main() {}\n")
	if !mentions(found, "panic inside Must") {
		t.Errorf("a panic outside main was not reported: %+v", found)
	}
}

// TestAShadowedNameIsCaught is the one that appeared in produced code.
//
// A loop variable named for the very type it ranges over: legal, compiles, and
// makes the name mean two things within four lines.
func TestAShadowedNameIsCaught(t *testing.T) {
	found := findingsFor(t, "package main\n\ntype entry struct{ Name string }\n\n"+
		"// Names lists every name.\n"+
		"func Names(entries []entry) []string {\n\tvar names []string\n"+
		"\tfor _, entry := range entries {\n\t\tnames = append(names, entry.Name)\n\t}\n"+
		"\treturn names\n}\n\nfunc main() {}\n")
	if !mentions(found, "hides a name") {
		t.Errorf("a shadowed declaration was not reported: %+v", found)
	}
}

// TestCleanCodeIsLeftAlone guards the other direction.
//
// A detector that fires on good code is one people learn to ignore, at which
// point it stops catching the bad code too.
func TestCleanCodeIsLeftAlone(t *testing.T) {
	found := findingsFor(t, "package main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\n"+
		"// Greet returns a greeting for a name.\n"+
		"func Greet(name string) (string, error) {\n"+
		"\tif name == \"\" {\n\t\treturn \"\", fmt.Errorf(\"no name\")\n\t}\n"+
		"\treturn \"hello \" + name, nil\n}\n\n"+
		"func main() {\n\tgreeting, err := Greet(\"world\")\n"+
		"\tif err != nil {\n\t\tfmt.Fprintln(os.Stderr, err)\n\t\tos.Exit(1)\n\t}\n"+
		"\tfmt.Println(greeting)\n}\n")
	if len(found) != 0 {
		t.Errorf("clean code was reported as containing anti-patterns: %+v", found)
	}
}
