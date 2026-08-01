package devdiag

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/testfixtures"
)

// TestM22_119_StructuredLogsCoverEveryDeclaredStage covers M22-119.
func TestM22_119_StructuredLogsCoverEveryDeclaredStage(t *testing.T) {
	var buffer bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buffer, &slog.HandlerOptions{Level: slog.LevelDebug}))
	recorder := NewRecorder(RecorderOptions{Enabled: true, Logger: logger})

	for index, stage := range AllStages() {
		if err := recorder.Record(Sample{
			Stage:      stage,
			Duration:   time.Duration(index+1) * time.Millisecond,
			Sequence:   uint64(index + 1),
			Attributes: map[string]string{"outcome": "ok"},
		}); err != nil {
			t.Fatalf("record %s: %v", stage, err)
		}
	}

	covered := recorder.CoveredStages()
	if len(covered) != len(AllStages()) {
		t.Fatalf("covered %d stages, want %d: %v", len(covered), len(AllStages()), covered)
	}
	totals := recorder.StageTotals()
	for _, stage := range AllStages() {
		if totals[stage] <= 0 {
			t.Fatalf("stage %s recorded no time", stage)
		}
	}

	// Each sample must be logged structurally, with the sequence that
	// correlates it back to the event that caused it.
	output := buffer.String()
	for _, stage := range AllStages() {
		if !strings.Contains(output, string(stage)) {
			t.Fatalf("log output omits stage %q", stage)
		}
	}
	for _, field := range []string{"duration_ns", "sequence"} {
		if !strings.Contains(output, field) {
			t.Fatalf("log output omits field %q", field)
		}
	}
}

// TestM22_119_DiagnosticsAreOffByDefaultAndSaySo proves a disabled recorder
// does not record and does not pretend to.
func TestM22_119_DiagnosticsAreOffByDefaultAndSaySo(t *testing.T) {
	recorder := NewRecorder(RecorderOptions{})
	if recorder.Enabled() {
		t.Fatal("diagnostics are enabled by default")
	}
	err := recorder.Record(Sample{Stage: StageProvider, Duration: time.Second, Sequence: 1})
	if !errors.Is(err, ErrDiagnosticsDisabled) {
		t.Fatalf("disabled recorder returned %v, want ErrDiagnosticsDisabled", err)
	}
	if len(recorder.Samples()) != 0 {
		t.Fatal("a disabled recorder retained a sample")
	}

	// Time must still run the work when disabled, and must not swallow its
	// error.
	sentinel := errors.New("work failed")
	if err := recorder.Time(StageTool, 1, func() error { return sentinel }); !errors.Is(err, sentinel) {
		t.Fatalf("disabled Time returned %v", err)
	}
}

// TestM22_119_RecorderRejectsUnusableSamples proves validation is real.
func TestM22_119_RecorderRejectsUnusableSamples(t *testing.T) {
	recorder := NewRecorder(RecorderOptions{Enabled: true})
	bad := []Sample{
		{Stage: Stage("invented"), Sequence: 1},
		{Stage: StageTool, Sequence: 1, Duration: -time.Second},
		{Stage: StageTool, Sequence: 1, Attributes: map[string]string{"": "x"}},
	}
	for index, sample := range bad {
		if err := recorder.Record(sample); err == nil {
			t.Fatalf("unusable sample %d was recorded", index)
		}
	}
	if Stage("invented").Valid() {
		t.Fatal("an unknown stage validated")
	}
}

// TestM22_122_DiagnosticsRefuseToLogSeededCredentials is M22-122 for the log
// surface.
func TestM22_122_DiagnosticsRefuseToLogSeededCredentials(t *testing.T) {
	var buffer bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buffer, &slog.HandlerOptions{Level: slog.LevelDebug}))
	recorder := NewRecorder(RecorderOptions{
		Enabled:   true,
		Forbidden: testfixtures.FixtureCredentialShapes(),
		Logger:    logger,
	})

	err := recorder.Record(Sample{
		Stage: StageProvider, Sequence: 1, Duration: time.Millisecond,
		Attributes: map[string]string{
			"error": "auth failed for " + testfixtures.FixtureCredentialMaterial,
		},
	})
	if !errors.Is(err, ErrForbiddenAttribute) {
		t.Fatalf("a credential-bearing attribute was accepted: %v", err)
	}
	if len(recorder.Samples()) != 0 {
		t.Fatal("a refused sample was still retained")
	}
	if strings.Contains(buffer.String(), testfixtures.FixtureCredentialMaterial) {
		t.Fatal("the credential reached the log")
	}
	// The refusal message itself must not quote the material it caught.
	if strings.Contains(err.Error(), testfixtures.FixtureCredentialMaterial) {
		t.Fatalf("the refusal leaked the credential: %v", err)
	}

	// A clean attribute still logs.
	if err := recorder.Record(Sample{
		Stage: StageProvider, Sequence: 2, Duration: time.Millisecond,
		Attributes: map[string]string{"error": "auth failed"},
	}); err != nil {
		t.Fatalf("a clean sample was refused: %v", err)
	}
}

// TestM22_120_ProfilingIsDisabledByDefaultAndRefusesRequests covers M22-120's
// default posture.
func TestM22_120_ProfilingIsDisabledByDefaultAndRefusesRequests(t *testing.T) {
	profiler, err := NewProfiler(ProfilingOptions{})
	if err != nil {
		t.Fatalf("build disabled profiler: %v", err)
	}
	t.Cleanup(profiler.Close)
	if profiler.Enabled() {
		t.Fatal("profiling is enabled by default")
	}
	if paths := profiler.ProfilePaths(); paths != nil {
		t.Fatalf("a disabled profiler advertises paths: %v", paths)
	}
	if !strings.Contains(profiler.Describe(), "disabled") {
		t.Fatalf("describe = %q", profiler.Describe())
	}

	// The handler must refuse rather than be nil, so mounting it cannot panic.
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:9/debug/pprof/heap", nil)
	request.Host = "127.0.0.1:9"
	profiler.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("disabled profiler answered with %d", recorder.Code)
	}
}

// TestM22_120_ProfilingRequiresLoopbackAndToken is the core M22-120 property.
func TestM22_120_ProfilingRequiresLoopbackAndToken(t *testing.T) {
	const token = "profiling-token-000000000000000000000000"
	profiler, err := NewProfiler(ProfilingOptions{
		Enabled: true, Token: token,
		MutexProfileFraction: 1, BlockProfileRate: 1,
	})
	if err != nil {
		t.Fatalf("build profiler: %v", err)
	}
	t.Cleanup(profiler.Close)
	handler := profiler.Handler()

	request := func(host, authorization string) int {
		recorder := httptest.NewRecorder()
		httpRequest := httptest.NewRequest(
			http.MethodGet, "http://"+host+"/debug/pprof/goroutine?debug=1", nil)
		httpRequest.Host = host
		if authorization != "" {
			httpRequest.Header.Set("Authorization", authorization)
		}
		handler.ServeHTTP(recorder, httpRequest)
		return recorder.Code
	}

	// Non-loopback is refused regardless of the token.
	for _, host := range []string{"example.com", "10.0.0.5:6060", "0.0.0.0:6060"} {
		if code := request(host, "Bearer "+token); code != http.StatusMisdirectedRequest {
			t.Fatalf("host %q answered with %d", host, code)
		}
	}
	// Loopback without the right token is refused.
	for _, authorization := range []string{
		"", "Bearer wrong", "Bearer " + token + "x", "Bearer " + token[:len(token)-1],
	} {
		if code := request("127.0.0.1:6060", authorization); code != http.StatusUnauthorized {
			t.Fatalf("authorization %q answered with %d", authorization, code)
		}
	}
	// Loopback with the right token succeeds, or the whole surface is useless.
	if code := request("127.0.0.1:6060", "Bearer "+token); code != http.StatusOK {
		t.Fatalf("an authorised loopback request answered with %d", code)
	}

	// Every declared profile must be advertised.
	paths := profiler.ProfilePaths()
	if len(paths) != len(AllProfileKinds()) {
		t.Fatalf("advertised %d paths for %d kinds: %v", len(paths), len(AllProfileKinds()), paths)
	}
	for _, kind := range AllProfileKinds() {
		if !kind.Valid() {
			t.Fatalf("declared kind %q is not valid", kind)
		}
		found := false
		for _, path := range paths {
			if path == kind.Path() {
				found = true
			}
		}
		if !found {
			t.Fatalf("profile %q is not advertised at %q", kind, kind.Path())
		}
	}
	if ProfileKind("invented").Valid() {
		t.Fatal("an unknown profile kind validated")
	}
}

// TestM22_120_ProfilingRejectsAGuessableToken proves the surface will not start
// with a weak secret.
func TestM22_120_ProfilingRejectsAGuessableToken(t *testing.T) {
	for _, options := range []ProfilingOptions{
		{Enabled: true},
		{Enabled: true, Token: "short"},
		{Enabled: true, Token: strings.Repeat("a", 31)},
		{Enabled: true, Token: strings.Repeat("a", 32), MutexProfileFraction: -1},
		{Enabled: true, Token: strings.Repeat("a", 32), BlockProfileRate: -1},
	} {
		if _, err := NewProfiler(options); err == nil {
			t.Fatalf("unsafe profiling options were accepted: %+v", options)
		}
	}
}

// TestM22_121_MarksCorrelateSequenceWithReducerAndRender covers M22-121.
func TestM22_121_MarksCorrelateSequenceWithReducerAndRender(t *testing.T) {
	ledger := NewMarkLedger(true)
	marks := []Mark{
		{Sequence: 3, Kind: "graph-updated", ReducerDuration: 8 * time.Millisecond,
			RenderDuration: 40 * time.Millisecond, Boundaries: []string{"graph"}},
		{Sequence: 1, Kind: "message-delta", ReducerDuration: time.Millisecond,
			RenderDuration: 2 * time.Millisecond, Boundaries: []string{"message"}},
		{Sequence: 2, Kind: "cost-updated", ReducerDuration: time.Millisecond,
			RenderDuration: time.Millisecond, Boundaries: []string{"cost"}},
	}
	for _, mark := range marks {
		if err := ledger.Add(mark); err != nil {
			t.Fatalf("add mark %d: %v", mark.Sequence, err)
		}
	}

	ordered := ledger.Marks()
	for index := 1; index < len(ordered); index++ {
		if ordered[index-1].Sequence > ordered[index].Sequence {
			t.Fatalf("marks are not ordered by sequence: %+v", ordered)
		}
	}

	// The slowest report must lead with the event that actually cost the most.
	slowest := ledger.SlowestByTotal(2)
	if len(slowest) != 2 {
		t.Fatalf("slowest returned %d marks", len(slowest))
	}
	if slowest[0].Sequence != 3 {
		t.Fatalf("slowest mark is sequence %d, want 3", slowest[0].Sequence)
	}
	if slowest[0].Total() != 48*time.Millisecond {
		t.Fatalf("total = %v, want 48ms", slowest[0].Total())
	}
	if ledger.SlowestByTotal(0) != nil {
		t.Fatal("a zero limit returned marks")
	}

	summary := ledger.Summary(3)
	if len(summary) != 3 {
		t.Fatalf("summary has %d lines", len(summary))
	}
	if !strings.Contains(summary[0], "seq 3") || !strings.Contains(summary[0], "graph") {
		t.Fatalf("summary line = %q", summary[0])
	}

	// A duplicate sequence would double-count one event's cost.
	if err := ledger.Add(marks[0]); err == nil {
		t.Fatal("a duplicate mark was accepted")
	}
}

// TestM22_121_MarksExposeRenderIsolationFailures is the reason boundaries are
// recorded at all.
func TestM22_121_MarksExposeRenderIsolationFailures(t *testing.T) {
	ledger := NewMarkLedger(true)
	if err := ledger.Add(Mark{
		Sequence: 1, Kind: "cost-updated", ReducerDuration: time.Millisecond,
		RenderDuration: time.Millisecond,
		// A cost update re-rendering the graph is the isolation failure.
		Boundaries: []string{"cost", "graph"},
	}); err != nil {
		t.Fatalf("add mark: %v", err)
	}
	if err := ledger.Add(Mark{
		Sequence: 2, Kind: "message-delta", ReducerDuration: time.Millisecond,
		RenderDuration: time.Millisecond, Boundaries: []string{"message"},
	}); err != nil {
		t.Fatalf("add mark: %v", err)
	}

	ownership := map[string][]string{
		"cost-updated":  {"cost"},
		"message-delta": {"message"},
	}
	unexpected := ledger.UnexpectedBoundaries(ownership)
	if len(unexpected) != 1 {
		t.Fatalf("found %d unexpected boundaries: %v", len(unexpected), unexpected)
	}
	if !strings.Contains(unexpected[0], "graph") || !strings.Contains(unexpected[0], "sequence 1") {
		t.Fatalf("finding = %q", unexpected[0])
	}

	// An event kind with no declared ownership is not checked, so a partial
	// map narrows the check rather than inventing findings.
	if findings := ledger.UnexpectedBoundaries(map[string][]string{}); len(findings) != 0 {
		t.Fatalf("an empty ownership map produced findings: %v", findings)
	}

	churn := ledger.BoundaryChurn()
	if churn["graph"] != 1 || churn["cost"] != 1 || churn["message"] != 1 {
		t.Fatalf("boundary churn = %v", churn)
	}
}

// TestM22_121_MarkLedgerIsOffByDefaultAndValidatesInput completes the M22-121
// contract.
func TestM22_121_MarkLedgerIsOffByDefaultAndValidatesInput(t *testing.T) {
	disabled := NewMarkLedger(false)
	if disabled.Enabled() {
		t.Fatal("the mark ledger is enabled by default")
	}
	err := disabled.Add(Mark{Sequence: 1, Kind: "message-delta"})
	if !errors.Is(err, ErrDiagnosticsDisabled) {
		t.Fatalf("disabled ledger returned %v", err)
	}

	ledger := NewMarkLedger(true)
	bad := []Mark{
		{Kind: "message-delta"},
		{Sequence: 1},
		{Sequence: 1, Kind: "message-delta", ReducerDuration: -time.Second},
		{Sequence: 1, Kind: "message-delta", Boundaries: []string{""}},
	}
	for index, mark := range bad {
		if err := ledger.Add(mark); err == nil {
			t.Fatalf("unusable mark %d was accepted", index)
		}
	}
}
