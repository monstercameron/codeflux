package coordinator

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// acceptanceExample is one thing the finished program must do.
//
// It is the only piece of the flow grounded in something outside the run's own
// reasoning. Every other check compares an artifact against a specification
// derived from another specification: the tests come from the code, the
// contracts come from the code, and a run whose understanding is wrong
// produces a consistent set of wrong things and passes all of them. An example
// supplied by the person asking is the one fact none of that can talk itself
// out of.
type acceptanceExample struct {
	Arguments []string
	Stdin     string
	Expected  string
}

// Acceptance markers. They are unmistakable rather than pretty because the
// requirement is prose and this has to be found in it without guessing.
const (
	acceptanceOpen  = "<<<ACCEPTANCE"
	acceptanceClose = ">>>"
)

// parseAcceptanceExamples reads the examples a requirement carries.
//
// The block is deliberately part of the requirement text rather than a field
// beside it, so the model is shown exactly what it will be judged against.
// Hiding the criterion and then failing a run for missing it would be a worse
// product than not checking at all.
func parseAcceptanceExamples(requirement string) []acceptanceExample {
	var examples []acceptanceExample
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
		if example, ok := parseOneExample(body); ok {
			examples = append(examples, example)
		}
	}
}

// parseOneExample reads a single block's fields.
func parseOneExample(body string) (acceptanceExample, bool) {
	var example acceptanceExample
	var expected []string
	collecting := false
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		switch {
		case collecting:
			expected = append(expected, line)
		case strings.HasPrefix(line, "args:"):
			trimmed := strings.TrimSpace(strings.TrimPrefix(line, "args:"))
			if trimmed != "" {
				example.Arguments = strings.Split(trimmed, " ")
			}
		case strings.HasPrefix(line, "stdin:"):
			// Escaped rather than literal, so a multi-line input stays on one
			// line and cannot be confused with the fields around it.
			example.Stdin = unescapeExample(
				strings.TrimSpace(strings.TrimPrefix(line, "stdin:")))
		case strings.HasPrefix(line, "expected:"):
			collecting = true
			if inline := strings.TrimSpace(
				strings.TrimPrefix(line, "expected:")); inline != "" {
				expected = append(expected, unescapeExample(inline))
			}
		}
	}
	example.Expected = strings.TrimSpace(strings.Join(expected, "\n"))
	return example, example.Expected != ""
}

// unescapeExample turns the escaped forms back into the bytes they stand for.
func unescapeExample(value string) string {
	value = strings.ReplaceAll(value, `\n`, "\n")
	value = strings.ReplaceAll(value, `\t`, "\t")
	return value
}

// acceptanceCommand decides which produced program the examples describe.
//
// It used to be commands[0], described in the code as "the one the request was
// about". The list was sorted, so it was whichever sorted first — for a run
// that produced a tool and a server, the examples were run against whichever
// name happened to come earlier in the alphabet, and every failure was
// unfixable because the program being judged was not the program being asked
// about. Naming beats ordering; when nothing names it and there is more than
// one candidate, saying so is better than guessing.
//
// The second return is empty when a command was chosen, and otherwise says why
// none could be.
func acceptanceCommand(commands []string, requirement string) (string, string) {
	if len(commands) == 1 {
		return commands[0], ""
	}
	lowered := strings.ToLower(requirement)
	var named []string
	for _, command := range commands {
		// Either the import path as written or the directory it ends in. A
		// requirement says "cmd/wc" or just "wc"; both mean this one.
		trimmed := strings.TrimPrefix(command, "./")
		// A command at the module root has an empty path once the prefix is
		// gone, and strings.Contains is true of the empty string against any
		// text. So the root package matched every request, every request with
		// a root command read as ambiguous, and the acceptance check — the one
		// stage grounded outside the run's own reasoning — never ran. It is
		// skipped here rather than special-cased later: a path that names
		// nothing cannot be the thing a request named.
		if trimmed == "" || trimmed == "." {
			continue
		}
		if strings.Contains(lowered, strings.ToLower(trimmed)) ||
			wordAppears(lowered, strings.ToLower(path.Base(trimmed))) {
			named = append(named, command)
		}
	}
	switch len(named) {
	case 1:
		return named[0], ""
	case 0:
		return "", fmt.Sprintf(
			"the run produced %d runnable commands (%s) and the request names "+
				"none of them, so there is no way to tell which one the "+
				"examples describe",
			len(commands), strings.Join(commands, ", "))
	default:
		return "", fmt.Sprintf(
			"the request names %d of the produced commands (%s), so it is "+
				"ambiguous which one the examples describe",
			len(named), strings.Join(named, ", "))
	}
}

// wordAppears reports whether a name appears in text as its own word.
//
// Substring matching would let a command called "at" match the word "that",
// which is exactly the kind of accidental hit that makes a name-based choice
// worse than no choice at all.
func wordAppears(text string, word string) bool {
	if word == "" {
		return false
	}
	for index := 0; ; {
		found := strings.Index(text[index:], word)
		if found < 0 {
			return false
		}
		start := index + found
		end := start + len(word)
		beforeIsBoundary := start == 0 || !isNameRune(rune(text[start-1]))
		afterIsBoundary := end == len(text) || !isNameRune(rune(text[end]))
		if beforeIsBoundary && afterIsBoundary {
			return true
		}
		index = start + 1
	}
}

// isNameRune reports whether a byte can be part of a command name.
func isNameRune(letter rune) bool {
	return letter == '_' || letter == '-' ||
		(letter >= '0' && letter <= '9') ||
		(letter >= 'a' && letter <= 'z') ||
		(letter >= 'A' && letter <= 'Z')
}

// checkAcceptance runs the built program against everything it was promised to
// do.
//
// This is the only stage that can catch a run whose understanding of the
// request was wrong. Everything else asks whether the work is consistent with
// itself, and a confidently mistaken run is perfectly consistent with itself.
func (execution *AgentExecution) checkAcceptance(
	ctx context.Context,
	worktree string,
	requirement string,
	examples []acceptanceExample,
) (stageOutcome, []string) {
	// Named-test examples are checked first and independently of any command
	// (PIPE-118). They are the form work with no command-line surface can
	// state, so requiring a runnable command before looking at them would put
	// the general form behind the specific one.
	named := parseNamedTestExamples(requirement)
	namedPassed, namedFailures := runNamedTestExamples(ctx, worktree, named)

	if len(examples) == 0 {
		switch {
		case len(namedFailures) > 0:
			return broke(fmt.Sprintf(
				"%d named acceptance test(s) did not pass: %s",
				len(namedFailures), strings.Join(namedFailures, "; ")),
				map[string]any{"named_tests_passed": namedPassed,
					"named_test_failures": namedFailures}), namedFailures
		case len(namedPassed) > 0:
			return held(fmt.Sprintf(
				"%d named acceptance test(s) pass: %s",
				len(namedPassed), strings.Join(namedPassed, ", ")),
				map[string]any{"named_tests_passed": namedPassed}), nil
		}
		return skipped("no executable acceptance example was supplied, so " +
			"there is nothing for the built program to reproduce"), nil
	}
	commands, err := producedCommands(ctx, worktree)
	if err != nil || len(commands) == 0 {
		return broke("the run produced no runnable command to check against "+
			"its examples", nil), nil
	}
	binaries := filepath.Join(worktree, ".codeflux-acceptance")
	if err := os.MkdirAll(binaries, 0o755); err != nil {
		return broke("the examples could not be run: "+err.Error(), nil), nil
	}
	defer func() { _ = os.RemoveAll(binaries) }()

	command, ambiguity := acceptanceCommand(commands, requirement)
	if ambiguity != "" {
		// Returned as a failure the run is told about, not only as a row in the
		// ledger. Handing back no failures recorded the one externally grounded
		// stage as failed while the loop, which guards on the failure count,
		// was never told there was anything to fix — so the check that exists
		// to catch a confidently mistaken run stayed silent about its own
		// inability to run.
		return broke(ambiguity, map[string]any{"commands": commands}),
			[]string{ambiguity + ". Produce one runnable command for what was " +
				"asked, or name in the code which one the examples describe."}
	}
	// A command at the module root has no directory name to take, so it is
	// named for what it is rather than for ".".
	name := path.Base(strings.TrimSuffix(strings.TrimPrefix(command, "./"), "/"))
	if name == "" || name == "." {
		name = "program"
	}
	binary := filepath.Join(binaries, builtBinaryName(name))
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, command)
	build.Dir = worktree
	if output, buildErr := build.CombinedOutput(); buildErr != nil {
		return broke("the program does not build on its own: "+
			failingLineOf(string(output)), nil), nil
	}

	var failures []string
	passed := 0
	for index, example := range examples {
		actual, runErr := runAcceptanceExample(ctx, binary, example)
		switch {
		case runErr != nil:
			failures = append(failures, fmt.Sprintf(
				"example %d could not be run: %v", index+1, runErr))
		case actual != example.Expected:
			failures = append(failures, describeAcceptanceFailure(
				index+1, example, actual))
		default:
			passed++
		}
	}
	evidence := map[string]any{
		"examples": len(examples), "reproduced": passed, "failures": failures,
	}
	if len(failures) > 0 {
		return broke(fmt.Sprintf(
			"the program reproduces %d of %d example(s): %s",
			passed, len(examples), strings.Join(failures, " | ")),
			evidence), failures
	}
	return held(fmt.Sprintf(
		"the built program reproduces all %d acceptance example(s) exactly",
		len(examples)), evidence), nil
}

// runAcceptanceExample runs the program once and returns what it printed.
func runAcceptanceExample(
	ctx context.Context,
	binary string,
	example acceptanceExample,
) (string, error) {
	deadline, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	run := exec.CommandContext(deadline, binary, example.Arguments...)
	if example.Stdin != "" {
		run.Stdin = strings.NewReader(example.Stdin)
	}
	output, err := run.CombinedOutput()
	if deadline.Err() != nil {
		return "", fmt.Errorf("it did not finish within 30s")
	}
	if err != nil && len(output) == 0 {
		return "", err
	}
	return normalizeAcceptanceOutput(string(output)), nil
}

// normalizeAcceptanceOutput compares what a program printed, not how the
// platform ends its lines.
func normalizeAcceptanceOutput(output string) string {
	return strings.TrimSpace(strings.ReplaceAll(output, "\r\n", "\n"))
}

// describeAcceptanceFailure says exactly how the output differed.
//
// It names the first line that differs rather than printing both in full: a
// diff of two forty-line outputs buries the one character that matters, and
// the one character is what the next attempt needs.
func describeAcceptanceFailure(
	number int,
	example acceptanceExample,
	actual string,
) string {
	wanted := strings.Split(example.Expected, "\n")
	got := strings.Split(actual, "\n")
	for index := range max(len(wanted), len(got)) {
		wantedLine, gotLine := "", ""
		if index < len(wanted) {
			wantedLine = wanted[index]
		}
		if index < len(got) {
			gotLine = got[index]
		}
		if wantedLine == gotLine {
			continue
		}
		return fmt.Sprintf(
			"example %d differs at line %d: expected %q, got %q",
			number, index+1, wantedLine, gotLine)
	}
	return fmt.Sprintf("example %d differs", number)
}

// acceptanceInstruction tells the next attempt what the program must do.
func acceptanceInstruction(
	examples []acceptanceExample,
	failures []string,
) string {
	var report strings.Builder
	report.WriteString(
		"The program compiles and its own tests pass, but it does not do what " +
			"it was asked to do:\n")
	for _, failure := range failures {
		fmt.Fprintf(&report, "\n- %s", failure)
	}
	report.WriteString("\n\nIt must reproduce these exactly:")
	for index, example := range examples {
		fmt.Fprintf(&report, "\n\nexample %d", index+1)
		if len(example.Arguments) > 0 {
			fmt.Fprintf(&report, "\n  arguments: %s",
				strings.Join(example.Arguments, " "))
		}
		if example.Stdin != "" {
			fmt.Fprintf(&report, "\n  input: %q", example.Stdin)
		}
		fmt.Fprintf(&report, "\n  must print exactly:\n%s", example.Expected)
	}
	report.WriteString(
		"\n\nFix the program, not the tests. If one of your tests asserts " +
			"something these examples contradict, the test is wrong.")
	return report.String()
}
