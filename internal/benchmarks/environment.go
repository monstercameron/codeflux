package benchmarks

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Environment is the machine a benchmark ran on (M22-089).
//
// docs/plan.md targets an ordinary hobbyist laptop, so a number without the
// machine that produced it is not evidence. This is captured with every run
// and written into the results document rather than into runtime SQLite,
// because a benchmark result is a repository fact, not application state
// (M22-090).
type Environment struct {
	GOOS         string
	GOARCH       string
	GoVersion    string
	LogicalCPUs  int
	TargetClass  string
	CapturedNote string
}

// CaptureEnvironment records the current machine.
func CaptureEnvironment() Environment {
	return Environment{
		GOOS:        runtime.GOOS,
		GOARCH:      runtime.GOARCH,
		GoVersion:   runtime.Version(),
		LogicalCPUs: runtime.NumCPU(),
		TargetClass: classifyTarget(runtime.NumCPU()),
		CapturedNote: "captured by internal/benchmarks; " +
			"see docs/benchmarks.md for the methodology this number was produced under",
	}
}

// classifyTarget names the machine class a result should be read against.
// The plan's target is an ordinary laptop, so a result produced on a much
// larger machine must not be presented as if it were one.
func classifyTarget(cpus int) string {
	switch {
	case cpus <= 2:
		return "below-target"
	case cpus <= 16:
		return "ordinary-laptop"
	default:
		return "above-target"
	}
}

// String renders the environment as one stable line for a benchmark log.
func (environment Environment) String() string {
	return fmt.Sprintf("%s/%s %s cpus=%d class=%s",
		environment.GOOS, environment.GOARCH, environment.GoVersion,
		environment.LogicalCPUs, environment.TargetClass)
}

// Report records the M22-088 dimensions on a benchmark result.
//
// It is called with the wall time the benchmark spent so the reported
// cpu-seconds and peak heap can be attributed to that same window. Go's
// testing package already reports ns/op and allocations under -benchmem; this
// adds the two dimensions it does not.
func Report(b *testing.B, elapsed time.Duration, before, after runtime.MemStats) {
	b.Helper()
	if b.N <= 0 {
		return
	}
	iterations := float64(b.N)
	b.ReportMetric(float64(elapsed.Nanoseconds())/iterations, "wall-ns/op")
	// Go does not expose per-goroutine CPU time portably. Total process CPU is
	// approximated by wall time multiplied by the parallelism the benchmark
	// actually used, which for these single-threaded benchmarks is 1. Reporting
	// it explicitly keeps the dimension present and honest rather than absent.
	b.ReportMetric(elapsed.Seconds(), "cpu-s")
	// HeapAlloc, not HeapSys: HeapSys is everything the runtime has reserved
	// from the OS across the whole test binary, which for a package with many
	// benchmarks reports gigabytes that have nothing to do with this one.
	b.ReportMetric(float64(after.HeapAlloc), "live-heap-B")
	b.ReportMetric(float64(after.TotalAlloc-before.TotalAlloc)/iterations, "alloc-B/op")
	b.ReportMetric(float64(after.Mallocs-before.Mallocs)/iterations, "mallocs/op")
}

// Measure runs one benchmark body while capturing the M22-088 dimensions.
//
// The timer is stopped for setup so a fixture build is never counted as the
// thing being measured, which is the most common way a benchmark quietly
// reports the wrong number.
func Measure(b *testing.B, setup func(), body func()) {
	b.Helper()
	b.StopTimer()
	if setup != nil {
		setup()
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	b.ReportAllocs()
	b.ResetTimer()
	b.StartTimer()
	started := time.Now()
	for range b.N {
		body()
	}
	elapsed := time.Since(started)
	b.StopTimer()
	runtime.ReadMemStats(&after)
	Report(b, elapsed, before, after)
}

// LogEnvironment writes the capture line once per benchmark so a saved log can
// always be traced back to the machine that produced it.
func LogEnvironment(b *testing.B) {
	b.Helper()
	b.Logf("environment: %s", CaptureEnvironment())
}

// DimensionNames returns the metric names Report emits, in the order
// RecordedDimensions declares them.
func DimensionNames() []string {
	return []string{"wall-ns/op", "cpu-s", "live-heap-B", "alloc-B/op", "mallocs/op"}
}

// DescribeScale renders a sub-benchmark name for one input size.
func DescribeScale(scale string) string {
	return strings.ReplaceAll(strings.TrimSpace(scale), " ", "-")
}

// ClockGranularity measures the smallest non-zero interval this platform's
// monotonic clock can report.
//
// It exists because a latency percentile derived from spans shorter than the
// clock can resolve is not a measurement, it is rounding. Benchmarks that
// report a tail use this to size their sampling window and publish the
// granularity alongside the result.
func ClockGranularity() time.Duration {
	const samples = 32
	smallest := time.Duration(0)
	for range samples {
		start := time.Now()
		var observed time.Duration
		for observed == 0 {
			observed = time.Since(start)
		}
		if smallest == 0 || observed < smallest {
			smallest = observed
		}
	}
	if smallest <= 0 {
		// A clock that never advances cannot be sized against; fall back to a
		// conservative figure rather than dividing by zero downstream.
		return time.Millisecond
	}
	return smallest
}
