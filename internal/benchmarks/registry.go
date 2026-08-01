// Package benchmarks holds the M22-076..090 performance measurements and the
// registry that binds each measurement to the TODO requiring it.
//
// Benchmarks live together rather than beside each subsystem because the plan
// asks for a comparable set: the same environment capture, the same recorded
// dimensions, and one methodology document. A benchmark scattered into its own
// package would drift into its own units and stop being comparable with the
// rest.
package benchmarks

import (
	"fmt"
	"slices"
	"strings"
)

// Measurement is one M22 benchmark requirement.
//
// Function names the Go benchmark that satisfies it and Package names where
// that benchmark lives. Both are verified against the real tree by
// TestBenchmarkRegistryMatchesTheRepository, so a renamed or deleted benchmark
// fails loudly instead of leaving the requirement quietly unmet.
type Measurement struct {
	TodoID   string
	Subject  string
	Function string
	Package  string
	// Scales names the input sizes the benchmark must run at. A benchmark the
	// plan describes with explicit sizes ("100, 1,000, and 10,000 events") is
	// not satisfied by a single-size run.
	Scales []string
}

// Registry returns every M22-076..087 measurement. M22-088..090 are properties
// OF this set rather than additional measurements, and are checked separately.
func Registry() []Measurement {
	return []Measurement{
		{
			TodoID: "M22-076", Subject: "cold coordinator startup",
			Function: "BenchmarkColdCoordinatorStartup", Package: "internal/benchmarks",
		},
		{
			TodoID: "M22-077", Subject: "warm coordinator startup",
			Function: "BenchmarkWarmCoordinatorStartup", Package: "internal/benchmarks",
		},
		{
			TodoID: "M22-078", Subject: "database migration from the prior schema",
			Function: "BenchmarkMigrationFromPriorSchema", Package: "internal/benchmarks",
		},
		{
			TodoID: "M22-079", Subject: "repository map",
			Function: "BenchmarkRepositoryMap", Package: "internal/benchmarks",
			Scales: []string{"small", "medium", "large"},
		},
		{
			TodoID: "M22-080", Subject: "context selection",
			Function: "BenchmarkContextSelection", Package: "internal/benchmarks",
		},
		{
			TodoID: "M22-081", Subject: "event append throughput and tail latency",
			Function: "BenchmarkEventAppend", Package: "internal/benchmarks",
		},
		{
			TodoID: "M22-082", Subject: "reconnect replay",
			Function: "BenchmarkReconnectReplay", Package: "internal/benchmarks",
			Scales: []string{"100", "1000", "10000"},
		},
		{
			TodoID: "M22-083", Subject: "thread initial render and upward pagination",
			Function: "BenchmarkThreadRenderAndPagination", Package: "internal/benchmarks",
		},
		{
			TodoID: "M22-084", Subject: "simultaneous token and cost updates",
			Function: "BenchmarkSimultaneousTokenAndCostUpdates", Package: "internal/benchmarks",
		},
		{
			TodoID: "M22-085", Subject: "300-node graph layout and render",
			Function: "BenchmarkM19Initial300NodeLayout", Package: "internal/graphlayout",
		},
		{
			TodoID: "M22-086", Subject: "100 graph patches without viewport reset",
			Function: "BenchmarkM19Sequential100GraphPatches", Package: "internal/graphlayout",
		},
		{
			TodoID: "M22-087", Subject: "SQLite vector search at prototype scale",
			Function: "BenchmarkVectorSearchAtPrototypeScale", Package: "internal/benchmarks",
		},
	}
}

// RecordedDimensions is M22-088: every benchmark must report these, so one
// result can be compared against another without re-running it.
//
// Wall time and allocation counts come from the testing package itself;
// CPU and memory are sampled by the harness in environment.go.
func RecordedDimensions() []string {
	return []string{"wall-time-ns", "cpu-seconds", "live-heap-bytes", "allocations"}
}

// Validate rejects a malformed measurement.
func (measurement Measurement) Validate() error {
	switch {
	case !strings.HasPrefix(measurement.TodoID, "M22-"):
		return fmt.Errorf("measurement %q must cite an M22 TODO, got %q",
			measurement.Subject, measurement.TodoID)
	case strings.TrimSpace(measurement.Subject) == "":
		return fmt.Errorf("%s names no subject", measurement.TodoID)
	case !strings.HasPrefix(measurement.Function, "Benchmark"):
		return fmt.Errorf("%s names %q, which is not a Go benchmark",
			measurement.TodoID, measurement.Function)
	case !strings.HasPrefix(measurement.Package, "internal/"):
		return fmt.Errorf("%s names package %q outside internal/",
			measurement.TodoID, measurement.Package)
	}
	return nil
}

// ValidateRegistry checks the registry covers M22-076..087 exactly once each.
func ValidateRegistry() error {
	registry := Registry()
	todos := map[string]string{}
	functions := map[string]struct{}{}
	for _, measurement := range registry {
		if err := measurement.Validate(); err != nil {
			return err
		}
		if other, clash := todos[measurement.TodoID]; clash {
			return fmt.Errorf("%s is claimed by both %q and %q",
				measurement.TodoID, other, measurement.Subject)
		}
		todos[measurement.TodoID] = measurement.Subject
		key := measurement.Package + "." + measurement.Function
		if _, duplicate := functions[key]; duplicate {
			return fmt.Errorf("benchmark %s is registered twice", key)
		}
		functions[key] = struct{}{}
	}
	for number := 76; number <= 87; number++ {
		todo := fmt.Sprintf("M22-%03d", number)
		if _, ok := todos[todo]; !ok {
			return fmt.Errorf("benchmark registry omits %s", todo)
		}
	}
	if len(registry) != 12 {
		return fmt.Errorf("M22-076..087 is 12 measurements, registry has %d", len(registry))
	}
	return nil
}

// MeasurementFor returns the registered measurement for one TODO.
func MeasurementFor(todo string) (Measurement, bool) {
	index := slices.IndexFunc(Registry(), func(candidate Measurement) bool {
		return candidate.TodoID == todo
	})
	if index < 0 {
		return Measurement{}, false
	}
	return Registry()[index], true
}
