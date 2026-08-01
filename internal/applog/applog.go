// Package applog is the application log: stable event names, correlation
// identifiers, redaction before serialization, bounded rotation, retention,
// and an explicit clear (M23-043..051).
//
// It is deliberately separate from internal/devdiag. That package times the
// system for a developer; this one records what happened for a user, survives
// restarts, and is subject to retention. Merging them would make a diagnostic
// aid into durable state nobody chose to keep.
package applog

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// Level is a structured log level (M23-043).
//
// There are four, and no more. A level set nobody can distinguish in practice
// gets used at random, and then filtering by level stops meaning anything.
type Level string

const (
	// LevelError is something the user must know about.
	LevelError Level = "error"
	// LevelWarn is something that will matter if it continues.
	LevelWarn Level = "warn"
	// LevelInfo is a durable milestone: a task started, a plan was approved.
	LevelInfo Level = "info"
	// LevelDebug is development-only detail (M23-051). It is never enabled by
	// default and carries a standing warning.
	LevelDebug Level = "debug"
)

// AllLevels returns every level, most severe first.
func AllLevels() []Level { return []Level{LevelError, LevelWarn, LevelInfo, LevelDebug} }

// Severity orders levels for filtering. Lower is more severe.
func (level Level) Severity() int {
	return slices.Index(AllLevels(), level)
}

// Valid reports whether a level is one of the declared set.
func (level Level) Valid() bool { return slices.Contains(AllLevels(), level) }

// EventName is a stable identifier for a kind of log record (M23-043).
//
// Stable is the operative word: a user filtering their log, or a support
// conversation referring to one, depends on the name not changing between
// releases. Renaming one is a compatibility change.
type EventName string

const (
	EventApplicationStarted EventName = "application.started"
	EventApplicationStopped EventName = "application.stopped"
	EventMigrationApplied   EventName = "migration.applied"
	EventTaskCreated        EventName = "task.created"
	EventTaskStateChanged   EventName = "task.state-changed"
	EventPlanProposed       EventName = "plan.proposed"
	EventApprovalRequested  EventName = "approval.requested"
	EventApprovalResolved   EventName = "approval.resolved"
	EventToolStarted        EventName = "tool.started"
	EventToolCompleted      EventName = "tool.completed"
	EventProviderRequested  EventName = "provider.requested"
	EventProviderFailed     EventName = "provider.failed"
	EventBudgetExhausted    EventName = "budget.exhausted"
	EventRecoveryRequired   EventName = "recovery.required"
	EventWorkerLeaseLost    EventName = "worker.lease-lost"
	EventLogsCleared        EventName = "logs.cleared"
)

// AllEventNames returns every declared event name, sorted.
func AllEventNames() []EventName {
	names := []EventName{
		EventApplicationStarted, EventApplicationStopped, EventMigrationApplied,
		EventTaskCreated, EventTaskStateChanged, EventPlanProposed,
		EventApprovalRequested, EventApprovalResolved, EventToolStarted,
		EventToolCompleted, EventProviderRequested, EventProviderFailed,
		EventBudgetExhausted, EventRecoveryRequired, EventWorkerLeaseLost,
		EventLogsCleared,
	}
	sort.Slice(names, func(left, right int) bool { return names[left] < names[right] })
	return names
}

// Valid reports whether an event name is declared.
//
// Unknown names are refused rather than logged. A log whose names are whatever
// a caller typed cannot be filtered, and the first typo becomes permanent.
func (name EventName) Valid() bool { return slices.Contains(AllEventNames(), name) }

// Correlation carries the identifiers that let one record be tied to the work
// it belongs to (M23-044).
//
// All four are optional individually, because not every event happens inside a
// task. What is not optional is that a record about a task carries the task's
// identity: a log line that cannot be attributed is a log line nobody can use.
type Correlation struct {
	RequestID string `json:"request_id,omitempty"`
	TaskID    string `json:"task_id,omitempty"`
	RunID     string `json:"run_id,omitempty"`
	// CorrelationID ties together every record produced by one user action,
	// across processes.
	CorrelationID string `json:"correlation_id,omitempty"`
}

// Empty reports whether no identifier was supplied.
func (correlation Correlation) Empty() bool {
	return correlation.RequestID == "" && correlation.TaskID == "" &&
		correlation.RunID == "" && correlation.CorrelationID == ""
}

// Record is one log entry.
type Record struct {
	At          time.Time   `json:"at"`
	Level       Level       `json:"level"`
	Event       EventName   `json:"event"`
	Message     string      `json:"message"`
	Correlation Correlation `json:"correlation,omitempty"`
	// Fields carry additional facts. Every value passes redaction before the
	// record is serialized (M23-045).
	Fields map[string]string `json:"fields,omitempty"`
}

// taskScopedEvents must carry a task identity, because a record about a task
// that cannot be attributed to one is unusable.
func taskScopedEvents() []EventName {
	return []EventName{
		EventTaskCreated, EventTaskStateChanged, EventPlanProposed,
		EventApprovalRequested, EventApprovalResolved, EventToolStarted,
		EventToolCompleted, EventBudgetExhausted, EventRecoveryRequired,
	}
}

// Validate rejects a record that could not be filtered or attributed.
func (record Record) Validate() error {
	switch {
	case record.At.IsZero():
		return errors.New("a log record requires a timestamp")
	case !record.Level.Valid():
		return fmt.Errorf("unknown log level %q", record.Level)
	case !record.Event.Valid():
		return fmt.Errorf("unknown log event %q; event names are a stable, closed set", record.Event)
	case strings.TrimSpace(record.Message) == "":
		return fmt.Errorf("event %q has no message", record.Event)
	}
	if slices.Contains(taskScopedEvents(), record.Event) && record.Correlation.TaskID == "" {
		return fmt.Errorf(
			"event %q is task-scoped but carries no task identity, so it cannot be attributed",
			record.Event)
	}
	for name := range record.Fields {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("event %q has an unnamed field", record.Event)
		}
	}
	return nil
}

// ForbiddenFieldNames are field names a record must never carry (M23-046).
//
// The rule is by name rather than by content because prompts and source are
// large and varied: catching them by inspection is unreliable, while refusing
// the field they would arrive in is exact.
func ForbiddenFieldNames() []string {
	return []string{
		"prompt", "prompts", "completion", "response_text", "model_output",
		"source", "source_code", "file_contents", "diff", "patch",
		"requirement", "requirement_text", "command_output", "stdout", "stderr",
	}
}

// Redactor turns a value into its safe form before serialization (M23-045).
type Redactor func(string) (string, error)

// Options configure a logger.
type Options struct {
	// MinimumLevel filters records. Debug is refused unless Verbose is set.
	MinimumLevel Level
	// Verbose enables debug logging (M23-051). It is off by default and the
	// logger emits a standing warning when it is on.
	Verbose bool
	// Redact is applied to every field value and to the message before the
	// record is serialized. A logger with no redactor is refused.
	Redact Redactor
	// MaximumRecords bounds the log (M23-047). Older records are dropped
	// first.
	MaximumRecords int
	// Retention bounds how long a record is kept (M23-048).
	Retention time.Duration
	// Now supplies the clock.
	Now func() time.Time
}

// DefaultMaximumRecords bounds an unconfigured log.
//
// It is a count rather than a byte size because a count is what a user can
// reason about, and because a redacted record has a predictable size.
const DefaultMaximumRecords = 10_000

// DefaultRetention is how long records are kept by default.
const DefaultRetention = 14 * 24 * time.Hour

// VerboseWarning is the standing warning shown while debug logging is on
// (M23-051).
const VerboseWarning = "verbose logging is ON: records may include detail intended for " +
	"development only. Turn it off before sharing this log, and clear the log afterwards."

// Logger is the application log.
type Logger struct {
	mutex   sync.Mutex
	options Options
	records []Record
	// dropped counts records evicted by rotation, so a reader can tell a short
	// log from a truncated one (M23-047).
	dropped int
}

// New builds a logger.
func New(options Options) (*Logger, error) {
	if options.Redact == nil {
		return nil, errors.New(
			"a logger requires a redactor; serializing before redacting is how a secret " +
				"reaches a file that outlives the process")
	}
	if options.Now == nil {
		return nil, errors.New("a logger requires a clock")
	}
	if options.MinimumLevel == "" {
		options.MinimumLevel = LevelInfo
	}
	if !options.MinimumLevel.Valid() {
		return nil, fmt.Errorf("unknown minimum level %q", options.MinimumLevel)
	}
	if options.MinimumLevel == LevelDebug && !options.Verbose {
		return nil, errors.New(
			"debug logging requires Verbose to be set explicitly, so it is never on by accident")
	}
	if options.MaximumRecords <= 0 {
		options.MaximumRecords = DefaultMaximumRecords
	}
	if options.Retention <= 0 {
		options.Retention = DefaultRetention
	}
	return &Logger{options: options}, nil
}

// Verbose reports whether debug logging is on.
func (logger *Logger) Verbose() bool {
	logger.mutex.Lock()
	defer logger.mutex.Unlock()
	return logger.options.Verbose
}

// Warning returns the standing warning when verbose is on, and empty otherwise
// (M23-051).
func (logger *Logger) Warning() string {
	if logger.Verbose() {
		return VerboseWarning
	}
	return ""
}

// ErrForbiddenField reports an attempt to log prompts or source (M23-046).
var ErrForbiddenField = errors.New("field name is not permitted in the application log")

// Log records one entry (M23-043..046).
//
// Redaction happens here, before the record is stored or serialized, so there
// is no window in which unredacted text exists in a durable structure.
func (logger *Logger) Log(record Record) error {
	if err := record.Validate(); err != nil {
		return err
	}
	logger.mutex.Lock()
	defer logger.mutex.Unlock()

	if record.Level == LevelDebug && !logger.options.Verbose {
		// Dropped silently: a caller logging debug detail in a non-verbose
		// build is not making a mistake, and failing would make debug logging
		// something people wrap in conditionals.
		return nil
	}
	if record.Level.Severity() > logger.options.MinimumLevel.Severity() {
		return nil
	}

	// M23-046: prompts and source are refused by field name, before anything
	// is redacted or stored.
	for name := range record.Fields {
		lowered := strings.ToLower(strings.TrimSpace(name))
		for _, forbidden := range ForbiddenFieldNames() {
			if lowered == forbidden {
				return fmt.Errorf("%w: %q", ErrForbiddenField, name)
			}
		}
	}

	// M23-045: redact BEFORE serialization.
	redacted := Record{
		At: logger.options.Now(), Level: record.Level, Event: record.Event,
		Correlation: record.Correlation,
	}
	message, err := logger.options.Redact(record.Message)
	if err != nil {
		return fmt.Errorf("redact message: %w", err)
	}
	redacted.Message = message
	if len(record.Fields) > 0 {
		redacted.Fields = make(map[string]string, len(record.Fields))
		for name, value := range record.Fields {
			safe, err := logger.options.Redact(value)
			if err != nil {
				return fmt.Errorf("redact field %q: %w", name, err)
			}
			redacted.Fields[name] = safe
		}
	}

	logger.records = append(logger.records, redacted)
	logger.rotateLocked()
	return nil
}

// rotateLocked enforces the record bound and the retention window (M23-047,
// M23-048).
func (logger *Logger) rotateLocked() {
	cutoff := logger.options.Now().Add(-logger.options.Retention)
	kept := logger.records[:0]
	for _, record := range logger.records {
		if record.At.Before(cutoff) {
			logger.dropped++
			continue
		}
		kept = append(kept, record)
	}
	logger.records = kept

	if excess := len(logger.records) - logger.options.MaximumRecords; excess > 0 {
		logger.dropped += excess
		// The oldest are dropped: the most recent records are the ones a user
		// looking at a problem right now actually needs.
		logger.records = append(logger.records[:0], logger.records[excess:]...)
	}
}

// Records returns everything currently retained.
func (logger *Logger) Records() []Record {
	logger.mutex.Lock()
	defer logger.mutex.Unlock()
	records := make([]Record, len(logger.records))
	copy(records, logger.records)
	return records
}

// Dropped reports how many records rotation removed, so a short log is
// distinguishable from a truncated one.
func (logger *Logger) Dropped() int {
	logger.mutex.Lock()
	defer logger.mutex.Unlock()
	return logger.dropped
}

// ClearOutcome describes what a clear did (M23-049, M23-050).
type ClearOutcome struct {
	RecordsCleared int
	// EvidencePreserved is always true and is reported explicitly, because the
	// question a user actually has when clearing logs is whether they are
	// about to lose the record of their work.
	EvidencePreserved bool
	// Explanation is what the user is told.
	Explanation string
}

// Clear removes every retained record (M23-049).
//
// It touches the application log ONLY. Task evidence — events, plans,
// approvals, validations, diffs — lives in the durable store and is not
// reachable from here, which is what makes M23-050 a structural guarantee
// rather than a promise: this type has no reference to the store at all.
func (logger *Logger) Clear() ClearOutcome {
	logger.mutex.Lock()
	defer logger.mutex.Unlock()
	cleared := len(logger.records)
	logger.records = nil
	logger.dropped = 0
	return ClearOutcome{
		RecordsCleared:    cleared,
		EvidencePreserved: true,
		Explanation: "the application log was cleared. Task evidence — events, plans, " +
			"approvals, validations, and diffs — is stored separately and is untouched.",
	}
}

// Serialize renders the retained records as JSON lines.
//
// The records are already redacted, so serialization cannot introduce a leak:
// there is no path here that reaches unredacted text.
func (logger *Logger) Serialize() ([]byte, error) {
	var builder strings.Builder
	for _, record := range logger.Records() {
		encoded, err := json.Marshal(record)
		if err != nil {
			return nil, fmt.Errorf("encode record: %w", err)
		}
		builder.Write(encoded)
		builder.WriteByte('\n')
	}
	return []byte(builder.String()), nil
}

// SetVerbose turns debug logging on or off at runtime (M23-051).
//
// Turning it on returns the warning, so a caller cannot enable it without
// having the warning in hand to show.
func (logger *Logger) SetVerbose(verbose bool) string {
	logger.mutex.Lock()
	defer logger.mutex.Unlock()
	logger.options.Verbose = verbose
	if !verbose && logger.options.MinimumLevel == LevelDebug {
		// Leaving the filter at debug with verbose off would silently discard
		// everything; the filter follows the switch.
		logger.options.MinimumLevel = LevelInfo
	}
	if verbose {
		return VerboseWarning
	}
	return ""
}

// RetentionSettings are the user-visible retention controls (M23-048).
type RetentionSettings struct {
	MaximumRecords int
	Retention      time.Duration
}

// Settings returns the current retention configuration.
func (logger *Logger) Settings() RetentionSettings {
	logger.mutex.Lock()
	defer logger.mutex.Unlock()
	return RetentionSettings{
		MaximumRecords: logger.options.MaximumRecords,
		Retention:      logger.options.Retention,
	}
}

// MinimumRetention is the shortest retention a user may configure.
//
// Below an hour, a log stops being useful for the thing people use it for:
// looking at what happened during the session they are still in.
const MinimumRetention = time.Hour

// MinimumRecords is the smallest record bound a user may configure.
const MinimumRecords = 100

// ApplySettings changes retention (M23-048).
func (logger *Logger) ApplySettings(settings RetentionSettings) error {
	if settings.MaximumRecords < MinimumRecords {
		return fmt.Errorf("a log of fewer than %d records is not useful", MinimumRecords)
	}
	if settings.Retention < MinimumRetention {
		return fmt.Errorf("a retention shorter than %s is not useful", MinimumRetention)
	}
	logger.mutex.Lock()
	defer logger.mutex.Unlock()
	logger.options.MaximumRecords = settings.MaximumRecords
	logger.options.Retention = settings.Retention
	logger.rotateLocked()
	return nil
}
