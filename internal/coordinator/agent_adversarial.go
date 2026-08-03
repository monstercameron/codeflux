package coordinator

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// hostileRun is one way of using a program that nobody intended.
type hostileRun struct {
	name      string
	arguments []string
	stdin     string
}

// hostileRuns are the inputs every produced command is tried against.
//
// They are deliberately dull: nothing here is an exotic attack, only the four
// things a person does by accident within a minute of being handed a program —
// run it with nothing, feed it the wrong thing, feed it emptiness, and feed it
// far more than it expected. A program that mishandles these has not been
// tested against anything harder for good reason.
var hostileRuns = []hostileRun{
	{name: "nothing at all"},
	{name: "unparseable text", arguments: []string{"!!!"},
		stdin: "this is not the input you wanted\n"},
	{name: "empty but present", stdin: "\n"},
	{name: "far more than expected",
		arguments: []string{strings.Repeat("9", 4096)},
		stdin:     strings.Repeat("x ", 20000) + "\n"},
}

// probeProducedCommands builds and exercises every command the run produced.
//
// It returns the findings, how many commands were probed, and any error that
// stopped the probing itself. A run that produced no command is not a failure:
// there is simply nothing to execute, and reporting that as a finding would
// punish work that was never meant to be a program.
func (execution *AgentExecution) probeProducedCommands(
	ctx context.Context,
	worktree string,
) (findings []string, probed int, err error) {
	// The adversarial probe has no example set to read a tags: value from, so
	// it probes only what builds under the default constraints.
	commands, err := producedCommands(ctx, worktree, "")
	if err != nil {
		return nil, 0, err
	}
	// producedCommands reports commands relative to the module they actually
	// build from, which is not always the worktree root (PIPE-123); the build
	// below has to run from the same place.
	moduleRoot, err := moduleRootIn(worktree)
	if err != nil {
		return nil, 0, err
	}
	binaries := filepath.Join(worktree, ".codeflux-probe")
	if err := os.MkdirAll(binaries, 0o755); err != nil {
		return nil, 0, err
	}
	defer func() { _ = os.RemoveAll(binaries) }()

	for _, command := range commands {
		binary := filepath.Join(binaries, builtBinaryName(filepath.Base(command)))
		build := exec.CommandContext(ctx, "go", "build", "-o", binary, command)
		build.Dir = moduleRoot
		if output, buildErr := build.CombinedOutput(); buildErr != nil {
			findings = append(findings, fmt.Sprintf(
				"%s: does not build on its own: %s", command,
				strings.TrimSpace(string(output))))
			continue
		}
		probed++
		findings = append(findings, probeOneCommand(ctx, command, binary)...)
	}
	return findings, probed, nil
}

// probeOneCommand runs one built program against every hostile input.
func probeOneCommand(
	ctx context.Context,
	name string,
	binary string,
) []string {
	var findings []string
	for _, hostile := range hostileRuns {
		deadline, cancel := context.WithTimeout(ctx, 10*time.Second)
		run := exec.CommandContext(deadline, binary, hostile.arguments...)
		if hostile.stdin != "" {
			run.Stdin = strings.NewReader(hostile.stdin)
		}
		output, err := run.CombinedOutput()
		timedOut := deadline.Err() != nil
		cancel()

		switch {
		case timedOut:
			findings = append(findings, fmt.Sprintf(
				"%s given %s: did not terminate within 10s", name, hostile.name))
		case couldNotStart(err):
			// A binary that will not start has not survived anything. This was
			// falling through to the silent-success arm, so a probe that never
			// executed one instruction of the program reported that it
			// "survived every hostile input" — the worst answer available,
			// because it is a pass that no amount of reading the ledger can
			// tell apart from a real one.
			findings = append(findings, fmt.Sprintf(
				"%s could not be started at all, so nothing was probed: %v",
				name, err))
		case strings.Contains(string(output), "panic:"):
			findings = append(findings, fmt.Sprintf(
				"%s given %s: panicked", name, hostile.name))
		case err == nil && strings.TrimSpace(string(output)) == "":
			// Nothing printed and a zero exit. A caller cannot tell this apart
			// from having done the job, which is the whole problem: the
			// program's silence is indistinguishable from success.
			findings = append(findings, fmt.Sprintf(
				"%s given %s: accepted it, printed nothing, and exited 0",
				name, hostile.name))
		}
	}
	return findings
}

// producedCommands lists the runnable packages in a worktree.
//
// The toolchain is asked rather than the directory layout. Reading cmd/ and
// treating each subdirectory as a command encoded one convention: it found
// nothing in a module whose command sits at the root or under tools/ or
// internal/, so every check built on this list quietly probed nothing and
// reported that there was nothing to probe. It also counted a cmd/
// subdirectory holding a library as a command, which then failed to build.
func producedCommands(ctx context.Context, worktree string, tags string) ([]string, error) {
	// Which directories this run actually wrote in. Asking the toolchain for
	// every main package in the module finds the ones that were already there,
	// and a repository that ships a placeholder main is the ordinary case
	// rather than an odd one: this fixture does, and the probe ran it, found
	// that it accepts anything and prints nothing, and recorded four findings
	// against a file the run never opened.
	//
	// The produced-file scoping that makes every other gate fair has to apply
	// here too, or the adversarial stage reports the state of the repository
	// instead of the state of the work.
	written, err := producedGoFiles(worktree)
	if err != nil {
		return nil, err
	}
	touched := map[string]bool{}
	for _, file := range written {
		touched[path.Dir(file)] = true
	}

	// The module a run's commands belong to is not always the worktree root
	// (PIPE-123): a repository can carry its module at a subdirectory go.mod.
	// `go list ./...` has to run from that module, not from the worktree, or
	// it refuses outright with "go.mod file not found" for a repository that
	// builds correctly.
	moduleRoot, err := moduleRootIn(worktree)
	if err != nil {
		return nil, err
	}
	listArguments := []string{"list", "-f", "{{if eq .Name \"main\"}}{{.Dir}}{{end}}"}
	if tags != "" {
		// A command guarded by a build tag (PIPE-123) is not a main package
		// under the default constraints at all, so it would otherwise never
		// appear in this listing -- not "found but broken", simply absent,
		// which is a worse failure than "does not build on its own" because
		// it names no reason.
		listArguments = append(listArguments, "-tags", tags)
	}
	listArguments = append(listArguments, "./...")
	listing := exec.CommandContext(ctx, "go", listArguments...)
	listing.Dir = moduleRoot
	output, err := listing.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("the runnable packages could not be listed: %s",
			strings.TrimSpace(string(output)))
	}
	var commands []string
	for _, line := range strings.Split(string(output), "\n") {
		directory := strings.TrimSpace(line)
		if directory == "" {
			continue
		}
		// The touched check is keyed against the worktree, because that is
		// what producedGoFiles reports paths relative to regardless of where
		// the module sits within it.
		worktreeRelative, relErr := filepath.Rel(worktree, directory)
		if relErr != nil {
			continue
		}
		worktreeRelative = filepath.ToSlash(worktreeRelative)
		if !touched[worktreeRelative] {
			// A command in a directory this run wrote nothing in. It may be
			// perfectly good or perfectly broken; either way it is not this
			// run's work and not this run's verdict to carry.
			continue
		}
		// The command itself is reported relative to the module root, because
		// that is where every caller must set Dir to build or run it -- which
		// is the worktree root in the ordinary case and only differs from it
		// for the repository layout this ticket exists to support.
		moduleRelative, relErr := filepath.Rel(moduleRoot, directory)
		if relErr != nil {
			continue
		}
		moduleRelative = filepath.ToSlash(moduleRelative)
		if moduleRelative == "." {
			moduleRelative = "./"
		} else {
			moduleRelative = "./" + moduleRelative
		}
		commands = append(commands, moduleRelative)
	}
	sort.Strings(commands)
	return commands, nil
}

// adversarialDetail says what the probing found, or why it found nothing.
func adversarialDetail(findings []string, probed int, err error) string {
	switch {
	case err != nil:
		return "the produced commands could not be probed: " + err.Error()
	case probed == 0:
		return "the run produced no runnable command, so there was nothing to probe"
	case len(findings) == 0:
		return fmt.Sprintf(
			"%d command(s) survived every hostile input", probed)
	default:
		return fmt.Sprintf("%d command(s) probed; %d finding(s): %s",
			probed, len(findings), strings.Join(findings, "; "))
	}
}

// couldNotStart reports that a program was never executed, as opposed to
// having been executed and failed.
//
// The two are opposite findings and they arrive as the same error value from
// exec. A program that ran and exited non-zero has told you something; one
// that could not be started has told you nothing, and counting the second as
// the first is how a probe reports survival for a binary it never launched.
func couldNotStart(err error) bool {
	if err == nil {
		return false
	}
	var startFailure *exec.Error
	if errors.As(err, &startFailure) {
		return true
	}
	return errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist)
}
