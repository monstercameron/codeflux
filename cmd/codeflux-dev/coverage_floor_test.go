package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var coverageFloorConstantPattern = regexp.MustCompile(`const coverageFloorPercent = ([0-9]+(?:\.[0-9]+)?)`)

// extractCoverageFloorConstant parses the coverageFloorPercent value out of
// coverage_floor.go source text, so TestCoverageFloorNeverDecreases can read
// both the working-tree value and an older committed value without a build.
func extractCoverageFloorConstant(source string) (float64, error) {
	match := coverageFloorConstantPattern.FindStringSubmatch(source)
	if match == nil {
		return 0, strconv.ErrSyntax
	}
	return strconv.ParseFloat(match[1], 64)
}

func TestExtractCoverageFloorConstant_ParsesDeclaration(t *testing.T) {
	value, err := extractCoverageFloorConstant("const coverageFloorPercent = 60.0\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if value != 60.0 {
		t.Fatalf("got %v, want 60.0", value)
	}
}

// TestExtractCoverageFloorConstant_MissingDeclarationErrors is the
// violating-input case: source with no such constant must be reported as
// unparseable rather than silently returning 0.
func TestExtractCoverageFloorConstant_MissingDeclarationErrors(t *testing.T) {
	if _, err := extractCoverageFloorConstant("package main\n"); err == nil {
		t.Fatal("source with no coverageFloorPercent declaration was accepted")
	}
}

// TestCoverageFloorNeverDecreases is the ratchet's actual enforcement: a
// commit that lowers coverageFloorPercent relative to the value most
// recently committed must fail, per REPO-028's "make an accidental decrease
// fail rather than silently rewrite the stored value." There is nothing to
// compare against before this file has been committed at least once, which
// is not a violation -- it is skipped rather than failed.
func TestCoverageFloorNeverDecreases(t *testing.T) {
	root := repositoryRootForCommandGraph(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}

	logCommand := exec.Command("git", "log", "-1", "--format=%H", "--", "cmd/codeflux-dev/coverage_floor.go")
	logCommand.Dir = root
	logOutput, err := logCommand.Output()
	if err != nil {
		t.Skip("no git history available to compare against")
	}
	priorCommit := strings.TrimSpace(string(logOutput))
	if priorCommit == "" {
		t.Skip("coverage_floor.go has no prior commit yet; nothing to ratchet against")
	}

	showCommand := exec.Command("git", "show", priorCommit+":cmd/codeflux-dev/coverage_floor.go")
	showCommand.Dir = root
	priorSource, err := showCommand.Output()
	if err != nil {
		t.Fatalf("read prior coverage_floor.go from %s: %v", priorCommit, err)
	}
	priorValue, err := extractCoverageFloorConstant(string(priorSource))
	if err != nil {
		t.Fatalf("parse prior coverage floor from %s: %v", priorCommit, err)
	}

	if !ratchetDidNotDecrease(coverageFloorPercent, priorValue) {
		t.Fatalf(
			"coverage floor decreased from %v to %v (commit %s); a ratchet must not decrease -- "+
				"raise coverageFloorPercent back up, or get explicit, deliberate authorization to lower it",
			priorValue, coverageFloorPercent, priorCommit,
		)
	}
}

// ratchetDidNotDecrease reports whether current is at least prior -- the
// comparison TestCoverageFloorNeverDecreases relies on, pulled out so it has
// its own direct test rather than only being exercised through git history.
func ratchetDidNotDecrease(current, prior float64) bool {
	return current >= prior
}

// TestRatchetDidNotDecrease_LowerCurrentFails is the discriminating
// violating-input case: a current value below the prior one must be
// reported as a decrease.
func TestRatchetDidNotDecrease_LowerCurrentFails(t *testing.T) {
	if ratchetDidNotDecrease(59.9, 60.0) {
		t.Fatal("59.9 following 60.0 was reported as not a decrease")
	}
}

func TestRatchetDidNotDecrease_EqualOrHigherPasses(t *testing.T) {
	if !ratchetDidNotDecrease(60.0, 60.0) {
		t.Fatal("a value equal to the prior one was reported as a decrease")
	}
	if !ratchetDidNotDecrease(61.0, 60.0) {
		t.Fatal("a higher value was reported as a decrease")
	}
}

// TestPrePushDefaultFloorMatchesRatchetConstant keeps .githooks/pre-push's
// hardcoded default (a shell script cannot import a Go constant) in sync
// with coverageFloorPercent, so raising the ratchet in one place can't be
// forgotten in the other.
func TestPrePushDefaultFloorMatchesRatchetConstant(t *testing.T) {
	root := repositoryRootForCommandGraph(t)
	sourceBytes, err := os.ReadFile(filepath.Join(root, ".githooks", "pre-push"))
	if err != nil {
		t.Fatalf("read .githooks/pre-push: %v", err)
	}
	source := string(sourceBytes)
	match := regexp.MustCompile(`MIN_COVERAGE="\$\{CODEFLUX_MIN_COVERAGE:-([0-9]+(?:\.[0-9]+)?)\}"`).FindStringSubmatch(source)
	if match == nil {
		t.Fatal(".githooks/pre-push does not declare a MIN_COVERAGE default in the expected shape")
	}
	hookDefault, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		t.Fatalf("parse .githooks/pre-push default: %v", err)
	}
	if hookDefault != coverageFloorPercent {
		t.Fatalf(
			"pre-push default floor is %v but cmd/codeflux-dev/coverage_floor.go's coverageFloorPercent is %v; they must agree",
			hookDefault, coverageFloorPercent,
		)
	}
}
