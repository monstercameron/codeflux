package applog

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/testfixtures"
)

// fixtureRedactor removes the fixture credential shapes, so a test can prove
// redaction happened rather than assuming it.
func fixtureRedactor(value string) (string, error) {
	for _, secret := range testfixtures.FixtureCredentialShapes() {
		value = strings.ReplaceAll(value, secret, "[REDACTED]")
	}
	return value, nil
}

func steppingNow() func() time.Time {
	current := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return func() time.Time {
		current = current.Add(time.Second)
		return current
	}
}

func newTestLogger(t *testing.T, mutate func(*Options)) *Logger {
	t.Helper()
	options := Options{Redact: fixtureRedactor, Now: steppingNow()}
	if mutate != nil {
		mutate(&options)
	}
	logger, err := New(options)
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}
	return logger
}

// TestM23_043_LevelsAndEventNamesAreAStableClosedSet covers M23-043.
func TestM23_043_LevelsAndEventNamesAreAStableClosedSet(t *testing.T) {
	if len(AllLevels()) != 4 {
		t.Fatalf("%d levels are declared, want 4", len(AllLevels()))
	}
	// Severity must order them, or filtering by level means nothing.
	if LevelError.Severity() >= LevelWarn.Severity() ||
		LevelWarn.Severity() >= LevelInfo.Severity() ||
		LevelInfo.Severity() >= LevelDebug.Severity() {
		t.Fatal("levels are not ordered by severity")
	}
	if Level("invented").Valid() {
		t.Fatal("an unknown level validated")
	}

	names := AllEventNames()
	if len(names) < 12 {
		t.Fatalf("%d event names are declared; the set is too small to describe a run", len(names))
	}
	seen := map[EventName]bool{}
	for _, name := range names {
		if seen[name] {
			t.Fatalf("event %q is declared twice", name)
		}
		seen[name] = true
		// A stable name is a lower-case dotted slug: anything else drifts.
		if strings.ToLower(string(name)) != string(name) ||
			!strings.Contains(string(name), ".") {
			t.Fatalf("event name %q is not a stable dotted slug", name)
		}
	}
	if EventName("task.invented").Valid() {
		t.Fatal("an undeclared event name validated")
	}

	// An undeclared name must be refused at log time, not accepted and later
	// discovered by whoever tries to filter on it.
	logger := newTestLogger(t, nil)
	if err := logger.Log(Record{
		At: time.Now(), Level: LevelInfo,
		Event: EventName("something.invented"), Message: "hello",
	}); err == nil {
		t.Fatal("an undeclared event name was logged")
	}
}

// TestM23_044_RecordsCarryCorrelationIdentifiers covers M23-044.
func TestM23_044_RecordsCarryCorrelationIdentifiers(t *testing.T) {
	logger := newTestLogger(t, nil)
	if err := logger.Log(Record{
		At: time.Now(), Level: LevelInfo, Event: EventTaskStateChanged,
		Message: "task moved to running",
		Correlation: Correlation{
			RequestID: "req-1", TaskID: "tsk-1", RunID: "run-1", CorrelationID: "cor-1",
		},
	}); err != nil {
		t.Fatalf("log: %v", err)
	}
	records := logger.Records()
	if len(records) != 1 {
		t.Fatalf("recorded %d entries", len(records))
	}
	correlation := records[0].Correlation
	if correlation.RequestID != "req-1" || correlation.TaskID != "tsk-1" ||
		correlation.RunID != "run-1" || correlation.CorrelationID != "cor-1" {
		t.Fatalf("correlation was not preserved: %+v", correlation)
	}
	if correlation.Empty() {
		t.Fatal("a populated correlation reports itself empty")
	}
	if !(Correlation{}).Empty() {
		t.Fatal("an empty correlation does not report itself empty")
	}

	// A task-scoped event with no task identity is unusable and must be
	// refused: a log line nobody can attribute answers no question.
	for _, event := range taskScopedEvents() {
		err := logger.Log(Record{
			At: time.Now(), Level: LevelInfo, Event: event, Message: "orphan",
		})
		if err == nil {
			t.Fatalf("task-scoped event %q was logged with no task identity", event)
		}
	}
	// An event that is not task-scoped needs no identity.
	if err := logger.Log(Record{
		At: time.Now(), Level: LevelInfo, Event: EventApplicationStarted,
		Message: "started",
	}); err != nil {
		t.Fatalf("a non-task event required a task identity: %v", err)
	}
}

// TestM23_045_RedactionHappensBeforeSerialization covers M23-045.
func TestM23_045_RedactionHappensBeforeSerialization(t *testing.T) {
	logger := newTestLogger(t, nil)
	secret := testfixtures.FixtureCredentialMaterial

	if err := logger.Log(Record{
		At: time.Now(), Level: LevelError, Event: EventProviderFailed,
		Message:     "provider rejected " + secret,
		Correlation: Correlation{CorrelationID: "cor-1"},
		Fields:      map[string]string{"detail": "key was " + secret},
	}); err != nil {
		t.Fatalf("log: %v", err)
	}

	// The stored record must already be clean: there must be no window in
	// which unredacted text exists in a durable structure.
	records := logger.Records()
	if strings.Contains(records[0].Message, secret) {
		t.Fatal("the stored message carries the secret")
	}
	if strings.Contains(records[0].Fields["detail"], secret) {
		t.Fatal("the stored field carries the secret")
	}
	if !strings.Contains(records[0].Message, "[REDACTED]") {
		t.Fatalf("the message was not redacted: %q", records[0].Message)
	}

	serialized, err := logger.Serialize()
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	if strings.Contains(string(serialized), secret) {
		t.Fatal("the serialized log carries the secret")
	}

	// A logger with no redactor must not be constructible at all.
	if _, err := New(Options{Now: steppingNow()}); err == nil {
		t.Fatal("a logger without a redactor was built")
	}
	if _, err := New(Options{Redact: fixtureRedactor}); err == nil {
		t.Fatal("a logger without a clock was built")
	}

	// A redactor that fails must fail the log call rather than storing the raw
	// value.
	failing, err := New(Options{
		Now:    steppingNow(),
		Redact: func(string) (string, error) { return "", errors.New("redactor broken") },
	})
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}
	if err := failing.Log(Record{
		At: time.Now(), Level: LevelInfo, Event: EventApplicationStarted, Message: secret,
	}); err == nil {
		t.Fatal("a failing redactor still produced a record")
	}
	if len(failing.Records()) != 0 {
		t.Fatal("a failed redaction left a record behind")
	}
}

// TestM23_046_PromptsAndSourceAreRefusedByFieldName covers M23-046.
func TestM23_046_PromptsAndSourceAreRefusedByFieldName(t *testing.T) {
	logger := newTestLogger(t, nil)
	for _, name := range ForbiddenFieldNames() {
		err := logger.Log(Record{
			At: time.Now(), Level: LevelInfo, Event: EventApplicationStarted,
			Message: "started", Fields: map[string]string{name: "anything"},
		})
		if !errors.Is(err, ErrForbiddenField) {
			t.Fatalf("field %q was accepted: %v", name, err)
		}
	}
	// The check is case-insensitive and ignores surrounding space, because a
	// caller writing "Prompt" is making the same mistake.
	for _, name := range []string{"Prompt", " SOURCE ", "Diff"} {
		if err := logger.Log(Record{
			At: time.Now(), Level: LevelInfo, Event: EventApplicationStarted,
			Message: "started", Fields: map[string]string{name: "anything"},
		}); !errors.Is(err, ErrForbiddenField) {
			t.Fatalf("field %q was accepted", name)
		}
	}
	// A refused record must not be stored.
	if len(logger.Records()) != 0 {
		t.Fatal("a refused record was stored")
	}
	// Ordinary fields still work.
	if err := logger.Log(Record{
		At: time.Now(), Level: LevelInfo, Event: EventApplicationStarted,
		Message: "started", Fields: map[string]string{"duration_ms": "42"},
	}); err != nil {
		t.Fatalf("an ordinary field was refused: %v", err)
	}
}

// TestM23_047_048_RotationAndRetentionAreBounded covers M23-047 and M23-048.
func TestM23_047_048_RotationAndRetentionAreBounded(t *testing.T) {
	logger := newTestLogger(t, func(options *Options) {
		options.MaximumRecords = 5
	})
	for index := range 20 {
		if err := logger.Log(Record{
			At: time.Now(), Level: LevelInfo, Event: EventApplicationStarted,
			Message: "entry", Fields: map[string]string{"index": itoa(index)},
		}); err != nil {
			t.Fatalf("log %d: %v", index, err)
		}
	}
	records := logger.Records()
	if len(records) != 5 {
		t.Fatalf("the log holds %d records, want 5", len(records))
	}
	// The NEWEST must be kept: someone reading a log is looking at what just
	// happened.
	if records[len(records)-1].Fields["index"] != itoa(19) {
		t.Fatalf("the newest record is %q", records[len(records)-1].Fields["index"])
	}
	// Dropped records must be counted, or a truncated log reads as a short one.
	if logger.Dropped() != 15 {
		t.Fatalf("dropped = %d, want 15", logger.Dropped())
	}

	// Retention drops by age. The stepping clock advances one second per read,
	// so a two-second retention keeps only the most recent entries.
	aged := newTestLogger(t, func(options *Options) {
		options.MaximumRecords = 1000
		options.Retention = 3 * time.Second
	})
	for range 10 {
		if err := aged.Log(Record{
			At: time.Now(), Level: LevelInfo, Event: EventApplicationStarted,
			Message: "entry",
		}); err != nil {
			t.Fatalf("log: %v", err)
		}
	}
	if len(aged.Records()) >= 10 {
		t.Fatalf("retention kept %d of 10 records", len(aged.Records()))
	}
	if aged.Dropped() == 0 {
		t.Fatal("retention dropped nothing")
	}

	// Settings are user-visible and validated.
	settings := logger.Settings()
	if settings.MaximumRecords != 5 {
		t.Fatalf("settings report %d records", settings.MaximumRecords)
	}
	if err := logger.ApplySettings(RetentionSettings{
		MaximumRecords: 500, Retention: 24 * time.Hour,
	}); err != nil {
		t.Fatalf("apply settings: %v", err)
	}
	if logger.Settings().MaximumRecords != 500 {
		t.Fatal("settings were not applied")
	}
	for _, bad := range []RetentionSettings{
		{MaximumRecords: 1, Retention: time.Hour},
		{MaximumRecords: 500, Retention: time.Minute},
		{},
	} {
		if err := logger.ApplySettings(bad); err == nil {
			t.Fatalf("unusable settings were accepted: %+v", bad)
		}
	}
}

// TestM23_049_050_ClearingLogsPreservesTaskEvidence covers M23-049 and M23-050.
func TestM23_049_050_ClearingLogsPreservesTaskEvidence(t *testing.T) {
	logger := newTestLogger(t, nil)
	for range 5 {
		if err := logger.Log(Record{
			At: time.Now(), Level: LevelInfo, Event: EventTaskCreated,
			Message: "task created", Correlation: Correlation{TaskID: "tsk-1"},
		}); err != nil {
			t.Fatalf("log: %v", err)
		}
	}

	outcome := logger.Clear()
	if outcome.RecordsCleared != 5 {
		t.Fatalf("cleared %d records, want 5", outcome.RecordsCleared)
	}
	if len(logger.Records()) != 0 {
		t.Fatal("records survived a clear")
	}
	if logger.Dropped() != 0 {
		t.Fatal("the dropped counter survived a clear")
	}

	// M23-050: the user must be told, in the outcome itself, that their task
	// evidence is untouched. That is the question they actually have.
	if !outcome.EvidencePreserved {
		t.Fatal("the outcome does not state that evidence is preserved")
	}
	for _, phrase := range []string{"evidence", "untouched"} {
		if !strings.Contains(strings.ToLower(outcome.Explanation), phrase) {
			t.Fatalf("the explanation does not mention %q: %q", phrase, outcome.Explanation)
		}
	}

	// The structural guarantee: this type cannot reach the durable store, so
	// clearing logs CANNOT delete evidence. A logger that held a store handle
	// would make M23-050 a promise instead of a fact.
	serialized, err := json.Marshal(logger.Settings())
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}
	if strings.Contains(string(serialized), "store") ||
		strings.Contains(string(serialized), "database") {
		t.Fatal("the logger's configuration references durable storage")
	}

	// Logging still works after a clear.
	if err := logger.Log(Record{
		At: time.Now(), Level: LevelInfo, Event: EventLogsCleared,
		Message: "log cleared by user",
	}); err != nil {
		t.Fatalf("log after clear: %v", err)
	}
	if len(logger.Records()) != 1 {
		t.Fatal("logging after a clear did not record")
	}
}

// TestM23_051_VerboseLoggingIsOffByDefaultAndWarns covers M23-051.
func TestM23_051_VerboseLoggingIsOffByDefaultAndWarns(t *testing.T) {
	logger := newTestLogger(t, nil)
	if logger.Verbose() {
		t.Fatal("verbose logging is on by default")
	}
	if logger.Warning() != "" {
		t.Fatal("a non-verbose logger emits a warning")
	}

	// A debug record is dropped silently while verbose is off, so a caller
	// need not wrap every debug call in a conditional.
	if err := logger.Log(Record{
		At: time.Now(), Level: LevelDebug, Event: EventApplicationStarted,
		Message: "detail",
	}); err != nil {
		t.Fatalf("a debug record errored while verbose was off: %v", err)
	}
	if len(logger.Records()) != 0 {
		t.Fatal("a debug record was stored while verbose was off")
	}

	// Turning verbose on returns the warning, so a caller cannot enable it
	// without the warning in hand to show.
	warning := logger.SetVerbose(true)
	if warning == "" {
		t.Fatal("enabling verbose returned no warning")
	}
	for _, phrase := range []string{"development", "sharing", "clear"} {
		if !strings.Contains(strings.ToLower(warning), phrase) {
			t.Fatalf("the warning does not mention %q: %q", phrase, warning)
		}
	}
	if logger.Warning() != warning {
		t.Fatal("the standing warning differs from the one returned")
	}

	verbose := newTestLogger(t, func(options *Options) {
		options.Verbose = true
		options.MinimumLevel = LevelDebug
	})
	if err := verbose.Log(Record{
		At: time.Now(), Level: LevelDebug, Event: EventApplicationStarted,
		Message: "detail",
	}); err != nil {
		t.Fatalf("log debug: %v", err)
	}
	if len(verbose.Records()) != 1 {
		t.Fatal("a debug record was dropped while verbose was on")
	}

	// Debug can never be the minimum level without verbose being explicit.
	if _, err := New(Options{
		Redact: fixtureRedactor, Now: steppingNow(), MinimumLevel: LevelDebug,
	}); err == nil {
		t.Fatal("debug was accepted as a minimum level without verbose")
	}
	if _, err := New(Options{
		Redact: fixtureRedactor, Now: steppingNow(), MinimumLevel: Level("invented"),
	}); err == nil {
		t.Fatal("an unknown minimum level was accepted")
	}

	// Turning verbose off must also lower a debug filter, or every record
	// would be silently discarded.
	verbose.SetVerbose(false)
	if err := verbose.Log(Record{
		At: time.Now(), Level: LevelInfo, Event: EventApplicationStarted,
		Message: "still logging",
	}); err != nil {
		t.Fatalf("log after disabling verbose: %v", err)
	}
	if len(verbose.Records()) != 2 {
		t.Fatalf("info logging stopped after verbose was disabled: %d records",
			len(verbose.Records()))
	}
}

// TestM23_043_RecordValidationIsLoadBearing proves malformed records cannot
// reach the log.
func TestM23_043_RecordValidationIsLoadBearing(t *testing.T) {
	valid := Record{
		At: time.Unix(1, 0).UTC(), Level: LevelInfo,
		Event: EventApplicationStarted, Message: "started",
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("a valid record was rejected: %v", err)
	}
	corruptions := map[string]func(Record) Record{
		"no timestamp": func(record Record) Record {
			record.At = time.Time{}
			return record
		},
		"unknown level": func(record Record) Record {
			record.Level = Level("invented")
			return record
		},
		"unknown event": func(record Record) Record {
			record.Event = EventName("invented")
			return record
		},
		"no message": func(record Record) Record {
			record.Message = ""
			return record
		},
		"unnamed field": func(record Record) Record {
			record.Fields = map[string]string{"": "value"}
			return record
		},
		"task event without task": func(record Record) Record {
			record.Event = EventTaskCreated
			return record
		},
	}
	for name, corrupt := range corruptions {
		t.Run(name, func(t *testing.T) {
			if err := corrupt(valid).Validate(); err == nil {
				t.Fatalf("an unusable record validated: %s", name)
			}
		})
	}
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}
