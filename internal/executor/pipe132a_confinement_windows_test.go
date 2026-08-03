//go:build windows

package executor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- PIPE-132a: bound what a launched verification process may reach, not
// merely where it may launch from. ---
//
// TestExecuteAuthorizedToolConfinementKillsDescendantLeftRunningAfterSuccess
// is the discriminating test for the Windows Job Object confinement added in
// process_windows.go. It proves the one gap that mechanism actually closes:
// terminateProcessTree (command.go's taskkill-based tree kill) only ever ran
// from the timeout and cancellation branches, so a mediated command that
// exited zero on its own -- the ordinary, passing case -- never had its
// descendants reaped at all. A background helper process started with
// Start() rather than Wait() kept running, unconfined, for as long as it
// liked.
//
// This is a Windows-only test because processConfinement is a Windows-only
// mechanism (see process_unix.go): on any other platform the same fixture
// would show the orphan surviving under mediation too, since nothing
// changed there.
//
// This test does NOT claim anything about PIPE-133's network-reach or
// filesystem-escape findings. Both of those complete synchronously, inside
// the one process this job has no reason to close yet, before the mediated
// call is done with the command -- a Job Object has no bearing on syscalls
// a still-running process makes. Only a process still running *after* the
// mediated call has finished with the command is affected, which is exactly
// what this test isolates.
func TestExecuteAuthorizedToolConfinementKillsDescendantLeftRunningAfterSuccess(t *testing.T) {
	worktree := t.TempDir()
	marker := filepath.Join(t.TempDir(), "orphan-survived")
	request := commandToolRequest(t, worktree, "orphan-parent", 5*time.Second)
	classification := grantedClassification(t, request)
	pipeline := commandTestRedactor(t)

	result, err := ExecuteAuthorizedTool(t.Context(), AuthorizedToolRequest{
		Request: request, Classification: classification,
		WorktreePath: worktree,
		Environment: map[string]string{
			"CODEFLUX_TREE_MARKER": marker,
			commandHelperMode:      "orphan-parent",
		},
		AllowedEnvironment: []string{"CODEFLUX_TREE_MARKER", commandHelperMode},
		Redactor:           pipeline,
	})
	if err != nil {
		t.Fatal(err)
	}
	// The mediated parent itself must have succeeded outright, not timed out
	// or been cancelled: those paths already killed the tree before
	// PIPE-132a. What is new is this path -- a clean, ordinary success that
	// still leaves a background helper running.
	if result.State != "succeeded" || result.TimedOut || result.Cancelled {
		t.Fatalf("mediated parent result = %#v, want a clean success so the "+
			"orphaned child left running is the only thing this test measures",
			result)
	}

	// The orphaned child sleeps 2s before writing its marker; wait
	// comfortably longer than that before checking it never appeared.
	time.Sleep(4 * time.Second)
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("PIPE-132a confinement failed: a process the mediated "+
			"command left running after its own successful exit survived "+
			"long enough to write %s (stat err = %v)", marker, statErr)
	}

	// Discrimination: the identical parent, launched directly with no
	// confinement in front of it at all -- exec.Command exactly as this
	// stage launched every command before PIPE-131, no Job Object assigned
	// -- lets the orphaned child survive and write its marker. This is what
	// proves the absence above is the confinement doing something, not the
	// fixture being unable to leave a process running for some unrelated
	// reason.
	rawMarker := filepath.Join(t.TempDir(), "orphan-survived-raw")
	raw := exec.CommandContext(t.Context(), os.Args[0],
		"-test.run=^TestCommandProcessTreeHelper$")
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, commandHelperMode+"=") &&
			!strings.HasPrefix(entry, "CODEFLUX_TREE_MARKER=") {
			raw.Env = append(raw.Env, entry)
		}
	}
	raw.Env = append(raw.Env,
		commandHelperMode+"=orphan-parent",
		"CODEFLUX_TREE_MARKER="+rawMarker,
	)
	if output, err := raw.CombinedOutput(); err != nil {
		t.Fatalf("fixture assumption broken: the unmediated parent itself "+
			"failed, so this proves nothing about confinement: %v\n%s", err, output)
	}
	time.Sleep(4 * time.Second)
	if _, statErr := os.Stat(rawMarker); statErr != nil {
		t.Fatalf("fixture assumption broken: the unmediated orphaned child "+
			"did not survive to write its marker either (stat err = %v), so "+
			"the mediated absence above cannot be attributed to confinement",
			statErr)
	}
}
