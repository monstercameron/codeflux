package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// Exit codes are M23-012.
//
// They are a contract: a script wrapping this CLI decides what to do next from
// the code alone, so "something went wrong" must be distinguishable from "you
// asked for something impossible" and from "the thing you need is not there
// yet". Adding a code is a compatibility change.
const (
	// exitOK means the command did what it said.
	exitOK = 0
	// exitFailure means the command was valid and the operation failed.
	exitFailure = 1
	// exitUsage means the invocation itself was wrong: unknown command, bad
	// flag, missing required argument.
	exitUsage = 2
	// exitUnavailable means the command was valid but a prerequisite is
	// absent. It is distinct from exitFailure because retrying will not help
	// until something is installed or configured.
	exitUnavailable = 3
	// exitInteractionRequired means the command needs a decision a person has
	// to make, and was told not to ask. A script sees this and knows to
	// surface the question rather than retry.
	exitInteractionRequired = 4
)

// ExitCodeMeanings documents the contract in one place, so `help` and the
// documentation cannot drift from the constants.
func ExitCodeMeanings() map[int]string {
	return map[int]string{
		exitOK:                  "the command completed",
		exitFailure:             "the command was valid and the operation failed",
		exitUnavailable:         "a prerequisite is missing; retrying will not help",
		exitUsage:               "the invocation was wrong",
		exitInteractionRequired: "a decision is required and prompting was disabled",
	}
}

// commandSpec is one CLI command (M23-013).
type commandSpec struct {
	Name string
	// Summary is one line, shown in the command list.
	Summary string
	// Usage is the invocation form.
	Usage string
	// Detail explains what the command does and what it will refuse to do.
	// Contextual help is the difference between a CLI a person can learn and
	// one they have to read the source of.
	Detail string
	// Flags are the accepted flags with their meanings.
	Flags []flagSpec
	// RequiresDatabase records whether the command touches durable state.
	RequiresDatabase bool
	// Interactive records whether the command may need a decision from a
	// person. Only these can exit with exitInteractionRequired.
	Interactive bool
}

// flagSpec is one accepted flag.
type flagSpec struct {
	Name    string
	Value   string
	Meaning string
}

// commandRegistry is the complete CLI surface (M23-001..014).
func commandRegistry() []commandSpec {
	return []commandSpec{
		{
			Name: "start", Summary: "Start the local coordinator and open the browser",
			Usage: "codeflux start [--database PATH] [--address HOST:PORT] [--no-browser] [--non-interactive]",
			Detail: "Starts the coordinator, serves the local browser interface on loopback, " +
				"and opens it. The URL is printed; the session secret is not, because a " +
				"secret printed to a terminal ends up in shell history and in scrollback.",
			Flags: []flagSpec{
				{"--database", "PATH", "database location; defaults to the standard user path"},
				{"--address", "HOST:PORT", "loopback address to serve on; must be loopback"},
				{"--no-browser", "", "do not open a browser; print the URL only"},
				{"--non-interactive", "", "never prompt; exit 4 if a decision is required"},
			},
			RequiresDatabase: true, Interactive: true,
		},
		{
			Name: "version", Summary: "Print executable and schema identity",
			Usage:  "codeflux version",
			Detail: "Prints the version, commit, build date, Go version, schema version, and frontend version.",
		},
		{
			Name: "doctor", Summary: "Check local prerequisites and report what is missing",
			Usage: "codeflux doctor [--database PATH]",
			Detail: "Reports each prerequisite as ok, missing, or error, and never guesses. " +
				"Exits 3 when something required is absent, so a script can tell " +
				"'not set up yet' from 'broken'.",
			Flags:            []flagSpec{{"--database", "PATH", "database to inspect"}},
			RequiresDatabase: true,
		},
		{
			Name: "backup", Summary: "Create an explicit SQLite recovery snapshot",
			Usage:  "codeflux backup --output PATH [--database PATH]",
			Detail: "Writes a consistent snapshot. It never overwrites an existing file.",
			Flags: []flagSpec{
				{"--output", "PATH", "destination file; required"},
				{"--database", "PATH", "database to snapshot"},
			},
			RequiresDatabase: true,
		},
		{
			Name: "integrity-check", Summary: "Run a full SQLite integrity check",
			Usage: "codeflux integrity-check [--database PATH]",
			Detail: "Runs SQLite's own integrity and foreign-key checks and reports the " +
				"result verbatim. A pass here means the file is structurally sound, " +
				"not that its contents are correct.",
			Flags:            []flagSpec{{"--database", "PATH", "database to check"}},
			RequiresDatabase: true,
		},
		{
			Name: "diagnostics", Summary: "Export a redacted diagnostic bundle",
			Usage: "codeflux diagnostics export --output PATH [--database PATH]",
			Detail: "Writes a bundle safe to attach to a bug report: versions, schema state, " +
				"prerequisite results, and counts. Every value passes the same " +
				"redaction boundary as any other export, and the bundle is scanned " +
				"before it is written.",
			Flags: []flagSpec{
				{"--output", "PATH", "destination file; required"},
				{"--database", "PATH", "database to describe"},
			},
			RequiresDatabase: true,
		},
		{
			Name: "provider", Summary: "Configure, test, or remove a model provider",
			Usage: "codeflux provider <set|test|delete> --name NAME [--non-interactive]",
			Detail: "Credentials are read from the platform credential store or from " +
				"standard input, never from a command-line argument: an argument is " +
				"visible to every process on the machine and lands in shell history.",
			Flags: []flagSpec{
				{"--name", "NAME", "provider name; required"},
				{"--non-interactive", "", "never prompt; exit 4 if a decision is required"},
			},
			Interactive: true,
		},
		{
			Name: "pause", Summary: "Pause an active task at a safe checkpoint",
			Usage: "codeflux pause --task ID", Detail: "Pauses at the next safe point; it does not interrupt an in-flight effect.",
			Flags: []flagSpec{{"--task", "ID", "task to pause; required"}},
		},
		{
			Name: "resume", Summary: "Resume a compatible paused task",
			Usage: "codeflux resume --task ID", Detail: "Refuses if the task's assumptions no longer hold.",
			Flags: []flagSpec{{"--task", "ID", "task to resume; required"}},
		},
		{
			Name: "cancel", Summary: "Cancel an active or paused task",
			Usage: "codeflux cancel --task ID", Detail: "Stops the task and preserves its work for review.",
			Flags: []flagSpec{{"--task", "ID", "task to cancel; required"}},
		},
		{
			Name: "help", Summary: "Show this help, or help for one command",
			Usage: "codeflux help [COMMAND]", Detail: "With a command name, explains that command and its flags.",
		},
	}
}

// lookupCommand finds one command by name.
func lookupCommand(name string) (commandSpec, bool) {
	for _, spec := range commandRegistry() {
		if spec.Name == name {
			return spec, true
		}
	}
	return commandSpec{}, false
}

// printHelp writes the command list (M23-013).
func printHelp(output io.Writer) {
	fmt.Fprintln(output, "Usage: codeflux <command> [flags]")
	fmt.Fprintln(output)
	fmt.Fprintln(output, "Commands:")
	specs := commandRegistry()
	width := 0
	for _, spec := range specs {
		if len(spec.Name) > width {
			width = len(spec.Name)
		}
	}
	for _, spec := range specs {
		fmt.Fprintf(output, "  %-*s  %s\n", width, spec.Name, spec.Summary)
	}
	fmt.Fprintln(output)
	fmt.Fprintln(output, "Run 'codeflux help <command>' for details.")
	fmt.Fprintln(output)
	fmt.Fprintln(output, "Exit codes:")
	codes := make([]int, 0, len(ExitCodeMeanings()))
	for code := range ExitCodeMeanings() {
		codes = append(codes, code)
	}
	sort.Ints(codes)
	for _, code := range codes {
		fmt.Fprintf(output, "  %d  %s\n", code, ExitCodeMeanings()[code])
	}
}

// printCommandHelp writes contextual help for one command (M23-013).
func printCommandHelp(output io.Writer, spec commandSpec) {
	fmt.Fprintf(output, "%s — %s\n\n", spec.Name, spec.Summary)
	fmt.Fprintf(output, "Usage:\n  %s\n\n", spec.Usage)
	fmt.Fprintf(output, "%s\n", spec.Detail)
	if len(spec.Flags) > 0 {
		fmt.Fprintln(output)
		fmt.Fprintln(output, "Flags:")
		width := 0
		for _, flag := range spec.Flags {
			label := flag.Name
			if flag.Value != "" {
				label += " " + flag.Value
			}
			if len(label) > width {
				width = len(label)
			}
		}
		for _, flag := range spec.Flags {
			label := flag.Name
			if flag.Value != "" {
				label += " " + flag.Value
			}
			fmt.Fprintf(output, "  %-*s  %s\n", width, label, flag.Meaning)
		}
	}
	if spec.Interactive {
		fmt.Fprintln(output)
		fmt.Fprintf(output,
			"This command may need a decision. With --non-interactive it exits %d instead of asking.\n",
			exitInteractionRequired)
	}
}

// ErrInteractionRequired is returned when a decision is needed and prompting
// was disabled (M23-014).
var ErrInteractionRequired = errors.New("a decision is required and prompting was disabled")

// interactionPolicy decides whether a command may ask a question (M23-014).
//
// The rule is that an explicitly non-interactive invocation NEVER blocks on
// input. A command that prompted anyway would hang a CI job forever, which is
// worse than failing.
type interactionPolicy struct {
	// Interactive is false when --non-interactive was passed, or when stdin is
	// not a terminal. The second half matters: a command piped input from a
	// script is not interactive even if nobody said so.
	Interactive bool
}

// newInteractionPolicy resolves the policy from the flag and the environment.
func newInteractionPolicy(nonInteractiveFlag bool, stdin *os.File) interactionPolicy {
	if nonInteractiveFlag {
		return interactionPolicy{Interactive: false}
	}
	if stdin == nil {
		return interactionPolicy{Interactive: false}
	}
	info, err := stdin.Stat()
	if err != nil {
		return interactionPolicy{Interactive: false}
	}
	// A character device is a terminal; a pipe or a file is not.
	return interactionPolicy{Interactive: info.Mode()&os.ModeCharDevice != 0}
}

// RequireDecision reports whether the command may ask for `what`.
func (policy interactionPolicy) RequireDecision(what string) error {
	if policy.Interactive {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrInteractionRequired, what)
}

// hasFlag reports whether a bare flag is present, and returns the remaining
// arguments. It is used for flags with no value.
func hasFlag(args []string, name string) ([]string, bool) {
	remaining := make([]string, 0, len(args))
	found := false
	for _, argument := range args {
		if argument == name {
			found = true
			continue
		}
		remaining = append(remaining, argument)
	}
	return remaining, found
}

// unknownCommandMessage suggests the closest real command, because a typo
// should not require reading the whole help.
func unknownCommandMessage(name string) string {
	var closest string
	best := -1
	for _, spec := range commandRegistry() {
		score := commonPrefixLength(name, spec.Name)
		if score > best {
			best = score
			closest = spec.Name
		}
	}
	if best >= 2 {
		return fmt.Sprintf("unknown command %q; did you mean %q?", name, closest)
	}
	return fmt.Sprintf("unknown command %q", name)
}

func commonPrefixLength(left, right string) int {
	left = strings.ToLower(left)
	right = strings.ToLower(right)
	limit := len(left)
	if len(right) < limit {
		limit = len(right)
	}
	for index := range limit {
		if left[index] != right[index] {
			return index
		}
	}
	return limit
}
