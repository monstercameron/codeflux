// Package devdiag holds development-only diagnostics: structured timing logs,
// loopback profiling, and the browser performance marks that correlate an
// event sequence with the work it caused (M22-119, M22-120, M22-121).
//
// Everything here is off unless explicitly enabled. Diagnostics that default
// to on become production behaviour nobody chose: they cost time on every
// request and they write internal detail to disk on machines whose owners
// never asked for it.
package devdiag

import (
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
)

// Stage names one place worth timing (M22-119).
//
// The set is closed and covers the full path a task takes: if a stage is
// missing here, its latency is invisible, and "the system felt slow" cannot be
// resolved into which part was slow.
type Stage string

const (
	StageTransaction  Stage = "transaction"
	StageEventAppend  Stage = "event-append"
	StageEventPublish Stage = "event-publish"
	StageWorkerLease  Stage = "worker-lease"
	StageProvider     Stage = "provider-request"
	StageTool         Stage = "tool-execution"
	StageReducer      Stage = "frontend-reducer"
	StageRender       Stage = "frontend-render"
	StageGraph        Stage = "graph-projection"
	StageRetrieval    Stage = "memory-retrieval"
)

// AllStages returns every timed stage.
func AllStages() []Stage {
	return []Stage{
		StageTransaction, StageEventAppend, StageEventPublish, StageWorkerLease,
		StageProvider, StageTool, StageReducer, StageRender, StageGraph,
		StageRetrieval,
	}
}

// Valid reports whether a stage is one of the declared set.
func (stage Stage) Valid() bool {
	for _, candidate := range AllStages() {
		if candidate == stage {
			return true
		}
	}
	return false
}

// Sample is one completed stage timing.
type Sample struct {
	Stage    Stage
	Duration time.Duration
	// Sequence correlates this sample with the durable event that caused it,
	// which is what lets a slow render be traced back to the event that
	// triggered it rather than merely observed.
	Sequence uint64
	// Attributes are additional non-sensitive facts. Values are checked
	// against the forbidden set before anything is logged.
	Attributes map[string]string
}

// Validate rejects a sample that could not be logged safely.
func (sample Sample) Validate() error {
	if !sample.Stage.Valid() {
		return fmt.Errorf("unknown diagnostic stage %q", sample.Stage)
	}
	if sample.Duration < 0 {
		return fmt.Errorf("stage %q reported a negative duration", sample.Stage)
	}
	for name := range sample.Attributes {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("stage %q has an unnamed attribute", sample.Stage)
		}
	}
	return nil
}

// ErrDiagnosticsDisabled is returned when a recorder is used while off.
var ErrDiagnosticsDisabled = errors.New("development diagnostics are disabled")

// ErrForbiddenAttribute reports a sample carrying material that must not be
// logged.
var ErrForbiddenAttribute = errors.New("diagnostic attribute carries forbidden material")

// Recorder collects stage timings when development diagnostics are enabled
// (M22-119).
type Recorder struct {
	mutex     sync.Mutex
	enabled   bool
	forbidden []string
	samples   []Sample
	logger    *slog.Logger
}

// RecorderOptions configures a recorder.
type RecorderOptions struct {
	// Enabled turns diagnostics on. It is false by default, and a disabled
	// recorder records nothing rather than recording quietly.
	Enabled bool
	// Forbidden is credential material that must never appear in an
	// attribute. A recorder that logged a secret would move it to a file with
	// none of the store's protections.
	Forbidden []string
	// Logger receives each sample. When nil, samples are retained in memory
	// only, which is what a test wants.
	Logger *slog.Logger
}

// NewRecorder builds a recorder.
func NewRecorder(options RecorderOptions) *Recorder {
	forbidden := make([]string, 0, len(options.Forbidden))
	for _, value := range options.Forbidden {
		if strings.TrimSpace(value) != "" {
			forbidden = append(forbidden, value)
		}
	}
	return &Recorder{
		enabled: options.Enabled, forbidden: forbidden, logger: options.Logger,
	}
}

// Enabled reports whether diagnostics are on.
func (recorder *Recorder) Enabled() bool {
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	return recorder.enabled
}

// Record adds one sample.
//
// A disabled recorder returns ErrDiagnosticsDisabled rather than silently
// dropping: a caller that believes it is measuring should find out that it is
// not.
func (recorder *Recorder) Record(sample Sample) error {
	if err := sample.Validate(); err != nil {
		return err
	}
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	if !recorder.enabled {
		return ErrDiagnosticsDisabled
	}
	for name, value := range sample.Attributes {
		for _, secret := range recorder.forbidden {
			if strings.Contains(value, secret) {
				return fmt.Errorf("%w: attribute %q on stage %q",
					ErrForbiddenAttribute, name, sample.Stage)
			}
		}
	}
	recorder.samples = append(recorder.samples, sample)
	if recorder.logger != nil {
		attributes := []any{
			slog.String("stage", string(sample.Stage)),
			slog.Int64("duration_ns", sample.Duration.Nanoseconds()),
			slog.Uint64("sequence", sample.Sequence),
		}
		names := make([]string, 0, len(sample.Attributes))
		for name := range sample.Attributes {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			attributes = append(attributes, slog.String(name, sample.Attributes[name]))
		}
		recorder.logger.Debug("codeflux stage timing", attributes...)
	}
	return nil
}

// Time runs work and records how long it took.
func (recorder *Recorder) Time(stage Stage, sequence uint64, work func() error) error {
	if !recorder.Enabled() {
		return work()
	}
	started := time.Now()
	err := work()
	_ = recorder.Record(Sample{
		Stage: stage, Sequence: sequence, Duration: time.Since(started),
	})
	return err
}

// Samples returns everything recorded.
func (recorder *Recorder) Samples() []Sample {
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	samples := make([]Sample, len(recorder.samples))
	copy(samples, recorder.samples)
	return samples
}

// StageTotals sums duration per stage, which is the summary a developer
// actually reads when asking where the time went.
func (recorder *Recorder) StageTotals() map[Stage]time.Duration {
	totals := map[Stage]time.Duration{}
	for _, sample := range recorder.Samples() {
		totals[sample.Stage] += sample.Duration
	}
	return totals
}

// CoveredStages returns the stages that produced at least one sample, sorted.
func (recorder *Recorder) CoveredStages() []Stage {
	totals := recorder.StageTotals()
	covered := make([]Stage, 0, len(totals))
	for stage := range totals {
		covered = append(covered, stage)
	}
	sort.Slice(covered, func(left, right int) bool {
		return covered[left] < covered[right]
	})
	return covered
}
