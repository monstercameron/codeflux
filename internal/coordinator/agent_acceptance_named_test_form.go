package coordinator

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// namedTestExample is an acceptance example that names a test which must pass
// (PIPE-118).
//
// The command form — arguments, standard input, expected output — can only be
// satisfied by work with a command-line surface, so a library change, a
// refactor, a migration, and behaviour-linked documentation could express no
// example at all. This is the general form: any Go work can be judged against
// a test the requester wrote.
type namedTestExample struct {
	// Package is the import path relative to the module, such as "./internal/x".
	// Empty means the whole module, which is the honest default for a
	// requester who knows the test name but not where it will live.
	Package string
	// Name is the test function, matched exactly rather than as a prefix: a
	// requester naming TestParse must not be satisfied by TestParseRejects
	// passing while TestParse itself fails.
	Name string
	// Tags is the go test -tags value the named test needs to build or run
	// under, when the requirement states one (PIPE-123). Empty means the
	// default build constraints.
	Tags string
}

// parseNamedTestExamples reads the named-test examples a requirement carries.
//
// The block shares the acceptance markers with the command form and is told
// apart by carrying a "test:" line, so a requirement can state both kinds and
// a reader sees one vocabulary rather than two.
func parseNamedTestExamples(requirement string) []namedTestExample {
	var examples []namedTestExample
	rest := requirement
	for {
		start := strings.Index(rest, acceptanceOpen)
		if start < 0 {
			return examples
		}
		rest = rest[start+len(acceptanceOpen):]
		end := strings.Index(rest, acceptanceClose)
		if end < 0 {
			return examples
		}
		body := rest[:end]
		rest = rest[end+len(acceptanceClose):]
		if example, ok := parseOneNamedTest(body); ok {
			examples = append(examples, example)
		}
	}
}

// parseOneNamedTest reads a single block, ignoring blocks of the other form.
func parseOneNamedTest(body string) (namedTestExample, bool) {
	var example namedTestExample
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, "test:"):
			example.Name = strings.TrimSpace(strings.TrimPrefix(line, "test:"))
		case strings.HasPrefix(line, "package:"):
			example.Package = strings.TrimSpace(strings.TrimPrefix(line, "package:"))
		case strings.HasPrefix(line, "tags:"):
			example.Tags = strings.TrimSpace(strings.TrimPrefix(line, "tags:"))
		}
	}
	if example.Name == "" {
		return namedTestExample{}, false
	}
	// A test name has to look like one. Accepting anything would turn a typo
	// into a -run pattern that matches nothing and reports success.
	if !strings.HasPrefix(example.Name, "Test") &&
		!strings.HasPrefix(example.Name, "Example") &&
		!strings.HasPrefix(example.Name, "Fuzz") {
		return namedTestExample{}, false
	}
	if example.Package == "" {
		example.Package = "./..."
	}
	return example, true
}

// runNamedTestExamples runs each named test and reports what did not pass.
//
// Each is run on its own rather than as one -run alternation, so a failure
// names the example that failed rather than the set.
func runNamedTestExamples(
	ctx context.Context,
	worktree string,
	examples []namedTestExample,
) (passed []string, failures []string) {
	if len(examples) == 0 {
		return nil, nil
	}
	// The module a named test belongs to is not always the worktree root
	// (PIPE-123); see moduleRootIn. A repository failing this lookup fails
	// every example the same way, which is the honest single explanation
	// rather than a per-example "did not pass" that hides the real cause.
	moduleRoot, err := moduleRootIn(worktree)
	if err != nil {
		for _, example := range examples {
			failures = append(failures, fmt.Sprintf(
				"%s could not be run: the module it belongs to could not be "+
					"found: %v", example.Name, err))
		}
		return nil, failures
	}
	for _, example := range examples {
		label := example.Name
		if example.Package != "./..." {
			label = example.Package + ":" + example.Name
		}
		// Anchored on both sides. An unanchored pattern would let
		// TestParseRejects satisfy a requirement that named TestParse.
		pattern := "^" + example.Name + "$"
		arguments := []string{"test", "-count=1"}
		if example.Tags != "" {
			arguments = append(arguments, "-tags", example.Tags)
		}
		arguments = append(arguments, "-run", pattern, example.Package)
		command := exec.CommandContext(ctx, "go", arguments...)
		command.Dir = moduleRoot
		output, err := command.CombinedOutput()
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s did not pass: %s",
				label, failingLineOf(string(output))))
			continue
		}
		// A -run pattern that matches nothing exits zero and prints
		// "ok <pkg> [no tests to run]", so a successful exit is not evidence
		// the test ran. Without this a misspelled name satisfies the example
		// while checking nothing, which is the failure mode an acceptance
		// example exists to prevent.
		rendered := string(output)
		if strings.Contains(rendered, "no tests to run") ||
			strings.Contains(rendered, "no test files") {
			failures = append(failures, fmt.Sprintf(
				"%s matched no test, so nothing was checked", label))
			continue
		}
		passed = append(passed, label)
	}
	return passed, failures
}
