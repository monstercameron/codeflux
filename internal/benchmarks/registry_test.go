package benchmarks

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestBenchmarkRegistryIsCompleteAndWellFormed covers M22-076..087's coverage
// claim without running a single benchmark, so the claim is checked on every
// build rather than only when someone remembers to pass -bench.
func TestBenchmarkRegistryIsCompleteAndWellFormed(t *testing.T) {
	if err := ValidateRegistry(); err != nil {
		t.Fatalf("benchmark registry is invalid: %v", err)
	}
}

// TestBenchmarkRegistryMatchesTheRepository is the load-bearing check: every
// registered benchmark must actually exist, in the package the registry names.
// Without it the registry is a wish list.
func TestBenchmarkRegistryMatchesTheRepository(t *testing.T) {
	root := repositoryRootForTest(t)
	for _, measurement := range Registry() {
		t.Run(measurement.TodoID, func(t *testing.T) {
			directory := filepath.Join(root, filepath.FromSlash(measurement.Package))
			entries, err := os.ReadDir(directory)
			if err != nil {
				t.Fatalf("read %s: %v", measurement.Package, err)
			}
			declaration := "func " + measurement.Function + "(b *testing.B)"
			found := ""
			for _, entry := range entries {
				if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
					continue
				}
				source, readErr := os.ReadFile(filepath.Join(directory, entry.Name()))
				if readErr != nil {
					t.Fatalf("read %s: %v", entry.Name(), readErr)
				}
				if strings.Contains(string(source), declaration) {
					found = entry.Name()
					break
				}
			}
			if found == "" {
				t.Fatalf("%s registers %s in %s, but no such benchmark exists there",
					measurement.TodoID, measurement.Function, measurement.Package)
			}
			// A benchmark the plan describes with explicit sizes must run at
			// each of them, or the scale claim is decoration.
			if len(measurement.Scales) == 0 {
				return
			}
			source, err := os.ReadFile(filepath.Join(directory, found))
			if err != nil {
				t.Fatalf("re-read %s: %v", found, err)
			}
			for _, scale := range measurement.Scales {
				if !strings.Contains(string(source), scale) {
					t.Fatalf("%s must measure at scale %q, which %s does not mention",
						measurement.TodoID, scale, found)
				}
			}
		})
	}
}

// TestRecordedDimensionsAreActuallyReported is M22-088. The declared dimension
// list and the metrics the harness emits must agree, or a results table would
// promise a column nobody fills in.
func TestRecordedDimensionsAreActuallyReported(t *testing.T) {
	declared := RecordedDimensions()
	if len(declared) == 0 {
		t.Fatal("no dimensions are declared")
	}
	emitted := DimensionNames()
	pairs := map[string]string{
		"wall-time-ns":    "wall-ns/op",
		"cpu-seconds":     "cpu-s",
		"live-heap-bytes": "live-heap-B",
		"allocations":     "mallocs/op",
	}
	for _, dimension := range declared {
		metric, ok := pairs[dimension]
		if !ok {
			t.Fatalf("declared dimension %q has no emitted metric", dimension)
		}
		if !slices.Contains(emitted, metric) {
			t.Fatalf("dimension %q maps to metric %q, which the harness does not emit (emits %v)",
				dimension, metric, emitted)
		}
	}
}

// TestEnvironmentCaptureIsHonestAboutTheMachine is M22-089. The plan targets an
// ordinary laptop, so a run on something much larger must say so rather than
// letting a fast number pass as representative.
func TestEnvironmentCaptureIsHonestAboutTheMachine(t *testing.T) {
	environment := CaptureEnvironment()
	if environment.GOOS == "" || environment.GOARCH == "" || environment.GoVersion == "" {
		t.Fatalf("environment capture is incomplete: %+v", environment)
	}
	if environment.LogicalCPUs < 1 {
		t.Fatalf("environment reports %d CPUs", environment.LogicalCPUs)
	}
	for cpus, want := range map[int]string{
		1: "below-target", 2: "below-target",
		4: "ordinary-laptop", 16: "ordinary-laptop",
		17: "above-target", 128: "above-target",
	} {
		if got := classifyTarget(cpus); got != want {
			t.Fatalf("classifyTarget(%d) = %q, want %q", cpus, got, want)
		}
	}
	if !strings.Contains(environment.String(), environment.GOARCH) {
		t.Fatalf("environment line omits the architecture: %q", environment)
	}
}

// TestBenchmarkMethodologyIsGitTracked is M22-090. Results belong in the
// repository next to the code they describe, not in runtime SQLite where they
// would be invisible to review and lost on a reset.
func TestBenchmarkMethodologyIsGitTracked(t *testing.T) {
	root := repositoryRootForTest(t)
	path := filepath.Join(root, "docs", "benchmarks.md")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("docs/benchmarks.md must exist and be tracked: %v", err)
	}
	document := string(source)

	// Every measurement must be documented, or the document is a partial
	// record that reads as a complete one.
	for _, measurement := range Registry() {
		if !strings.Contains(document, measurement.TodoID) {
			t.Fatalf("docs/benchmarks.md does not document %s", measurement.TodoID)
		}
		if !strings.Contains(document, measurement.Function) {
			t.Fatalf("docs/benchmarks.md does not name %s", measurement.Function)
		}
	}
	for _, section := range []string{"Methodology", "Environment", "Results"} {
		if !strings.Contains(document, section) {
			t.Fatalf("docs/benchmarks.md has no %s section", section)
		}
	}
	// The plan forbids treating these as runtime state.
	if strings.Contains(document, "codeflux.sqlite3") {
		t.Fatal("benchmark results must not be stored in runtime SQLite")
	}
}

// TestMeasurementLookupIsExact guards the accessor.
func TestMeasurementLookupIsExact(t *testing.T) {
	measurement, ok := MeasurementFor("M22-082")
	if !ok {
		t.Fatal("M22-082 is not registered")
	}
	if !slices.Equal(measurement.Scales, []string{"100", "1000", "10000"}) {
		t.Fatalf("M22-082 scales = %v", measurement.Scales)
	}
	if _, ok := MeasurementFor("M22-999"); ok {
		t.Fatal("an unregistered TODO resolved")
	}
}

// TestClockGranularityIsUsable guards the measurement the tail-latency
// benchmark sizes itself against.
func TestClockGranularityIsUsable(t *testing.T) {
	granularity := ClockGranularity()
	if granularity <= 0 {
		t.Fatalf("clock granularity = %v", granularity)
	}
	if granularity > time.Second {
		t.Fatalf("clock granularity %v is too coarse for any latency measurement", granularity)
	}
}

func repositoryRootForTest(t *testing.T) string {
	t.Helper()
	// internal/benchmarks -> internal -> repository root.
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("repository root %q has no go.mod: %v", root, err)
	}
	return root
}
