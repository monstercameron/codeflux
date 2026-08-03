package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckProviderKeyTestsAreGatedFiresOnUngatedReadProviderKey(t *testing.T) {
	root := t.TempDir()
	relative := filepath.Join("internal", "coordinator", "ungated_test.go")
	writeTestFile(t, filepath.Join(root, relative), `package coordinator

import "testing"

func TestSpendsMoneyOnEveryRun(t *testing.T) {
	key := ReadProviderKey(".")
	if key == "" {
		t.Skip("no provider key")
	}
	_ = key
}
`)
	err := checkProviderKeyTestsAreGated(root, []string{relative})
	if err == nil {
		t.Fatal("ungated ReadProviderKey call was accepted")
	}
	if !strings.Contains(err.Error(), "ungated_test.go") ||
		!strings.Contains(err.Error(), "TestSpendsMoneyOnEveryRun") ||
		!strings.Contains(err.Error(), "opt-in gate") {
		t.Fatalf("ungated ReadProviderKey error = %v, want file, function, and opt-in-gate finding", err)
	}
}

// TestCheckProviderKeyTestsAreGatedFiresOnUngatedQualifiedReadProviderKey
// proves the call path is matched even when it is not a bare identifier --
// a test in a different package calling coordinator.ReadProviderKey is
// exactly as ungated a route to a real credential as calling it from within
// the coordinator package itself.
func TestCheckProviderKeyTestsAreGatedFiresOnUngatedQualifiedReadProviderKey(t *testing.T) {
	root := t.TempDir()
	relative := filepath.Join("internal", "otherpkg", "ungated_qualified_test.go")
	writeTestFile(t, filepath.Join(root, relative), `package otherpkg

import (
	"testing"

	"codeflux.dev/codeflux/internal/coordinator"
)

func TestSpendsMoneyFromAnotherPackage(t *testing.T) {
	key := coordinator.ReadProviderKey(".")
	_ = key
}
`)
	err := checkProviderKeyTestsAreGated(root, []string{relative})
	if err == nil {
		t.Fatal("ungated qualified ReadProviderKey call was accepted")
	}
	if !strings.Contains(err.Error(), "ungated_qualified_test.go") {
		t.Fatalf("qualified ReadProviderKey error = %v, want file finding", err)
	}
}

func TestCheckProviderKeyTestsAreGatedFiresWhenGateFollowsAccess(t *testing.T) {
	root := t.TempDir()
	relative := filepath.Join("internal", "coordinator", "misordered_test.go")
	writeTestFile(t, filepath.Join(root, relative), `package coordinator

import (
	"os"
	"testing"
)

func TestGatesAfterReadingTheKey(t *testing.T) {
	key := ReadProviderKey(".")
	mode := os.Getenv("CODEFLUX_LADDER")
	if mode == "" {
		t.Skip("mode required")
	}
	_ = key
}
`)
	err := checkProviderKeyTestsAreGated(root, []string{relative})
	if err == nil {
		t.Fatal("a gate that follows the key access was accepted as gating it")
	}
	if !strings.Contains(err.Error(), "misordered_test.go") {
		t.Fatalf("misordered gate error = %v, want misordered_test.go finding", err)
	}
}

func TestCheckProviderKeyTestsAreGatedPassesOnGatedFixture(t *testing.T) {
	root := t.TempDir()
	relative := filepath.Join("internal", "coordinator", "gated_test.go")
	writeTestFile(t, filepath.Join(root, relative), `package coordinator

import (
	"os"
	"strings"
	"testing"
)

func TestTheLadderIsOptIn(t *testing.T) {
	mode := strings.TrimSpace(os.Getenv("CODEFLUX_LADDER"))
	if mode == "" {
		t.Skip("set CODEFLUX_LADDER to run this")
	}
	key := ReadProviderKey(".")
	if key == "" {
		t.Skip("no provider key")
	}
	_ = key
}
`)
	if err := checkProviderKeyTestsAreGated(root, []string{relative}); err != nil {
		t.Fatalf("correctly gated provider-key test rejected: %v", err)
	}
}

func TestCheckProviderKeyTestsAreGatedIgnoresTestsWithNoProviderKeyAccess(t *testing.T) {
	root := t.TempDir()
	relative := filepath.Join("internal", "coordinator", "unrelated_test.go")
	writeTestFile(t, filepath.Join(root, relative), `package coordinator

import (
	"os"
	"testing"
)

func TestUnrelatedEnvironmentRead(t *testing.T) {
	if os.Getenv("SOME_OTHER_SETTING") == "" {
		t.Skip("unrelated")
	}
}
`)
	if err := checkProviderKeyTestsAreGated(root, []string{relative}); err != nil {
		t.Fatalf("test with no provider-key access rejected: %v", err)
	}
}

// TestCheckProviderKeyTestsAreGatedIgnoresRawEnvironmentReadOfKeyShapedName
// is the discriminating regression this rule exists to get right: a test
// that only ever reads a provider-key-shaped environment variable directly
// -- never through ReadProviderKey -- must not be flagged, because that
// shape covers real, legitimate tests in this repository (see the two real
// fixtures below) that have nothing to do with calling a provider. A rule
// that matched raw environment reads by name alone would force those tests
// to carry a spend opt-in they do not need, or worse, silently skip a
// credential-leak check.
func TestCheckProviderKeyTestsAreGatedIgnoresRawEnvironmentReadOfKeyShapedName(t *testing.T) {
	root := t.TempDir()
	relative := filepath.Join("internal", "coordinator", "sideroute_test.go")
	writeTestFile(t, filepath.Join(root, relative), `package coordinator

import (
	"os"
	"testing"
)

func TestReadsAKeyShapedNameDirectly(t *testing.T) {
	key := os.Getenv("OPENAI_API_KEY")
	if key != "" {
		t.Fatal("did not expect a provider key in this environment")
	}
}
`)
	if err := checkProviderKeyTestsAreGated(root, []string{relative}); err != nil {
		t.Fatalf("raw environment read of a key-shaped name was rejected: %v", err)
	}
}

// TestCheckProviderKeyTestsAreGatedPassesOnRealWorkerCredentialLeakTest is
// the required non-vacuous positive case: the real
// internal/worker/environment_test.go sets a synthetic OPENAI_API_KEY
// literal into a child process environment and reads it back specifically
// to assert BuildMinimumWorkerEnvironment stripped it before the worker saw
// it. It never contacts a provider and must pass ungated -- gating it would
// silently disable a credential-leak test rather than prevent spend.
func TestCheckProviderKeyTestsAreGatedPassesOnRealWorkerCredentialLeakTest(t *testing.T) {
	root := repositoryRootForCommandGraph(t)
	relative := filepath.Join("internal", "worker", "environment_test.go")
	if err := checkProviderKeyTestsAreGated(root, []string{relative}); err != nil {
		t.Fatalf("the real worker credential-leak test was rejected: %v", err)
	}
}

func TestCheckProviderKeyTestsAreGatedPassesOnRealRepositoryLadderTest(t *testing.T) {
	root := repositoryRootForCommandGraph(t)
	relative := filepath.Join("internal", "coordinator", "engine_produces_program_test.go")
	if err := checkProviderKeyTestsAreGated(root, []string{relative}); err != nil {
		t.Fatalf("the real, already-gated ladder test was rejected: %v", err)
	}
}

func TestCheckProviderKeyTestsAreGatedPassesOnRealRedactionFixture(t *testing.T) {
	root := repositoryRootForCommandGraph(t)
	relative := filepath.Join("internal", "executor", "command_test.go")
	if err := checkProviderKeyTestsAreGated(root, []string{relative}); err != nil {
		t.Fatalf("the real redaction-fixture helper was rejected: %v", err)
	}
}
