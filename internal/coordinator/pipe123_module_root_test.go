package coordinator

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/pipeline"
)

// writeNestedModuleWorktree lays out a repository for PIPE-123: unlike
// writeWorktree, it does not inject a go.mod at the worktree root, so a test
// can place the module wherever it wants -- including nowhere, to prove the
// honest failure case.
func writeNestedModuleWorktree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for path, body := range files {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	initialize := exec.CommandContext(t.Context(), "git", "init")
	initialize.Dir = root
	if output, err := initialize.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, output)
	}
	return root
}

// TestPIPE123_ModuleRootInFindsTheWorktreeRootFirst is the fast, overwhelmingly
// common path: a go.mod at the worktree root is used directly, with no
// directory walk.
func TestPIPE123_ModuleRootInFindsTheWorktreeRootFirst(t *testing.T) {
	worktree := writeWorktree(t, nil)
	root, err := moduleRootIn(worktree)
	if err != nil {
		t.Fatalf("moduleRootIn: %v", err)
	}
	if root != worktree {
		t.Errorf("module root = %q, want the worktree root %q", root, worktree)
	}
}

// TestPIPE123_ModuleRootInFindsANestedModule is the defect this ticket exists
// to fix: a repository whose module lives at a subdirectory go.mod, with
// nothing at the worktree root, must still be found rather than reported as
// "no go.mod" for a repository that builds correctly.
func TestPIPE123_ModuleRootInFindsANestedModule(t *testing.T) {
	worktree := writeNestedModuleWorktree(t, map[string]string{
		"backend/go.mod": "module codeflux.test/backend\n\ngo 1.26.0\n",
		"README.md":      "not a module\n",
	})
	root, err := moduleRootIn(worktree)
	if err != nil {
		t.Fatalf("moduleRootIn: %v", err)
	}
	want := filepath.Join(worktree, "backend")
	if root != want {
		t.Errorf("module root = %q, want %q", root, want)
	}
}

// TestPIPE123_ModuleRootInPrefersTheShallowestModule covers a repository
// nesting more than one go.mod -- a vendored tool, a documentation example --
// where picking the wrong one would silently build or check something other
// than the run's own module.
func TestPIPE123_ModuleRootInPrefersTheShallowestModule(t *testing.T) {
	worktree := writeNestedModuleWorktree(t, map[string]string{
		"service/go.mod":           "module codeflux.test/service\n\ngo 1.26.0\n",
		"service/tools/dev/go.mod": "module codeflux.test/service/tools/dev\n\ngo 1.26.0\n",
	})
	root, err := moduleRootIn(worktree)
	if err != nil {
		t.Fatalf("moduleRootIn: %v", err)
	}
	want := filepath.Join(worktree, "service")
	if root != want {
		t.Errorf("module root = %q, want the shallower module %q", root, want)
	}
}

// TestPIPE123_ModuleRootInFailsHonestlyWithNoModule proves the miss is
// reported rather than silently answered with the worktree root, which would
// send every later build to a directory with no go.mod and turn a clear
// "no module" fact into a confusing build failure instead.
func TestPIPE123_ModuleRootInFailsHonestlyWithNoModule(t *testing.T) {
	worktree := writeNestedModuleWorktree(t, map[string]string{
		"README.md": "no module here\n",
	})
	if _, err := moduleRootIn(worktree); err == nil {
		t.Fatal("moduleRootIn did not fail for a worktree with no go.mod anywhere")
	}
}

// TestPIPE123_ProducedCommandsReportsANonRootModuleRelativeToIt is the
// dependency every acceptance and adversarial build shares: before this,
// producedCommands ran `go list ./...` at the worktree root unconditionally,
// so a repository whose module sits at a subdirectory go.mod refused outright
// with "go.mod file not found" instead of listing the command this run wrote.
//
// Discriminates directly: reverting producedCommands' listing.Dir back to
// worktree reproduces that exact refusal for this fixture, since the worktree
// root here deliberately holds no go.mod.
func TestPIPE123_ProducedCommandsReportsANonRootModuleRelativeToIt(t *testing.T) {
	worktree := writeNestedModuleWorktree(t, map[string]string{
		"backend/go.mod": "module codeflux.test/backend\n\ngo 1.26.0\n",
		"backend/cmd/greet/main.go": "package main\n\nimport \"fmt\"\n\n" +
			"func main() {\n\tfmt.Println(\"hi\")\n}\n",
	})
	execution := &AgentExecution{}
	commands, err := execution.producedCommands(t.Context(), worktree, "")
	if err != nil {
		t.Fatalf("producedCommands: %v", err)
	}
	if len(commands) != 1 || commands[0] != "./cmd/greet" {
		t.Fatalf("commands = %#v, want exactly [\"./cmd/greet\"] (relative to "+
			"the nested module, not the worktree)", commands)
	}
}

// TestPIPE123_CheckAcceptanceBuildsAndRunsANonRootModuleProgram is the
// end-to-end claim: an acceptance example must be checkable against a
// repository whose module is not at the worktree root, exactly as it would be
// for one that is. Before this fix, checkAcceptance's own `go build` call ran
// with Dir set to the worktree, so even a producedCommands that had somehow
// named the right command would fail to build it.
func TestPIPE123_CheckAcceptanceBuildsAndRunsANonRootModuleProgram(t *testing.T) {
	worktree := writeNestedModuleWorktree(t, map[string]string{
		"backend/go.mod": "module codeflux.test/backend\n\ngo 1.26.0\n",
		"backend/cmd/greet/main.go": "package main\n\nimport \"fmt\"\n\n" +
			"func main() {\n\tfmt.Println(\"hi\")\n}\n",
	})
	requirement := "Create cmd/greet/main.go that prints a greeting.\n\n" +
		"<<<ACCEPTANCE\nexpected:\nhi\n>>>"
	examples := parseAcceptanceExamples(requirement)
	if len(examples) != 1 {
		t.Fatalf("%d example(s) parsed, want 1", len(examples))
	}

	execution := &AgentExecution{settings: pipeline.DefaultSettings()}
	outcome, failures := execution.checkAcceptance(
		t.Context(), worktree, requirement, examples)
	if len(failures) != 0 {
		t.Errorf("failures = %v, want none", failures)
	}
	if !outcome.Held {
		t.Fatalf("acceptance did not hold for a working nested-module "+
			"program: %s", outcome.Detail)
	}
}

// TestPIPE123_AcceptanceTagsReachTheBuild proves an acceptance example naming
// a tags: value actually constrains the build, using a package that only
// compiles when the tag is supplied. Without -tags reaching the build, this
// fails with "the program does not build on its own" (no non-test Go files
// satisfy the default build constraints); with it threaded through, the build
// succeeds and the example is checked for real.
func TestPIPE123_AcceptanceTagsReachTheBuild(t *testing.T) {
	worktree := writeWorktree(t, map[string]string{
		"cmd/gated/main.go": "//go:build acceptancetag\n\n" +
			"package main\n\nimport \"fmt\"\n\n" +
			"func main() {\n\tfmt.Println(\"gated\")\n}\n",
	})
	requirement := "Create cmd/gated/main.go behind a build tag.\n\n" +
		"<<<ACCEPTANCE\ntags: acceptancetag\nexpected:\ngated\n>>>"
	examples := parseAcceptanceExamples(requirement)
	if len(examples) != 1 || examples[0].Tags != "acceptancetag" {
		t.Fatalf("parsed %#v, want one example with tags=acceptancetag", examples)
	}

	execution := &AgentExecution{settings: pipeline.DefaultSettings()}
	outcome, failures := execution.checkAcceptance(
		t.Context(), worktree, requirement, examples)
	if len(failures) != 0 {
		t.Errorf("failures = %v, want none: %s", failures, outcome.Detail)
	}
	if !outcome.Held {
		t.Fatalf("acceptance did not hold for a program that only builds "+
			"under its declared tag: %s", outcome.Detail)
	}
}

// TestPIPE123_WithoutTheTagTheGatedProgramIsNotEvenFound is the control for
// the test above: it proves the fixture genuinely needs the tag, so the
// previous test's pass is evidence the tag reached both `go list` and
// `go build` rather than an artefact of a program that would have worked
// anyway.
//
// The honest failure without the tag is "the run produced no runnable
// command", not "does not build": a file excluded by its own build
// constraints is not a main package under the default constraints at all --
// `go list ./...` never reports it, so checkAcceptance never reaches a build
// step to fail. Asserting "does not build" here would be asserting the wrong
// mechanism for a real reason: PIPE-123's own producedCommands fix has to
// carry -tags into the listing, not only into the later go build call, or
// this exact case is silently unreachable.
func TestPIPE123_WithoutTheTagTheGatedProgramIsNotEvenFound(t *testing.T) {
	worktree := writeWorktree(t, map[string]string{
		"cmd/gated/main.go": "//go:build acceptancetag\n\n" +
			"package main\n\nimport \"fmt\"\n\n" +
			"func main() {\n\tfmt.Println(\"gated\")\n}\n",
	})
	requirement := "Create cmd/gated/main.go behind a build tag.\n\n" +
		"<<<ACCEPTANCE\nexpected:\ngated\n>>>"
	examples := parseAcceptanceExamples(requirement)
	if len(examples) != 1 || examples[0].Tags != "" {
		t.Fatalf("parsed %#v, want one example with no tags", examples)
	}

	execution := &AgentExecution{settings: pipeline.DefaultSettings()}
	outcome, failures := execution.checkAcceptance(
		t.Context(), worktree, requirement, examples)
	if outcome.Held {
		t.Fatalf("acceptance held without the tag the fixture needs to build "+
			"at all: %s", outcome.Detail)
	}
	if !strings.Contains(outcome.Detail, "no runnable command") {
		t.Errorf("detail = %q, want it to say the run produced no runnable "+
			"command: without the tag, go list itself never reports the "+
			"gated package as a main package", outcome.Detail)
	}
	if len(failures) != 0 {
		t.Errorf("failures = %v, want none: this fails before there is a "+
			"command to build, so there is nothing per-example to report",
			failures)
	}
}
