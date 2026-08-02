package executor

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/redact"
)

// recordingProgressSink captures published progress and can signal the moment
// output first arrives, which is what makes "while running" checkable.
type recordingProgressSink struct {
	mu        sync.Mutex
	updates   []ToolProgress
	firstText chan struct{}
	once      sync.Once
	// block delays every publish, standing in for a slow consumer.
	block time.Duration
}

func newRecordingProgressSink() *recordingProgressSink {
	return &recordingProgressSink{firstText: make(chan struct{})}
}

func (sink *recordingProgressSink) PublishToolProgress(
	_ context.Context, update ToolProgress,
) error {
	if sink.block > 0 {
		time.Sleep(sink.block)
	}
	sink.mu.Lock()
	sink.updates = append(sink.updates, update)
	sink.mu.Unlock()
	if update.Content != "" {
		sink.once.Do(func() { close(sink.firstText) })
	}
	return nil
}

func (sink *recordingProgressSink) content(stream string) string {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	var builder strings.Builder
	for _, update := range sink.updates {
		if update.Stream == stream {
			builder.WriteString(update.Content)
		}
	}
	return builder.String()
}

func liveProgressPipeline(t *testing.T) *redact.Pipeline {
	t.Helper()
	pipeline, err := redact.NewPipeline(nil, redact.Limits{
		MaximumInputBytes: 4 << 20, MaximumOutputBytes: 4 << 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pipeline.Close)
	return pipeline
}

func newTestLiveWriter(
	t *testing.T, ctx context.Context, sink ToolProgressSink,
) (*liveProgressWriter, *redact.Stream) {
	t.Helper()
	pipeline := liveProgressPipeline(t)
	durable, err := pipeline.NewStream(redact.BoundaryLogPersistence)
	if err != nil {
		t.Fatal(err)
	}
	writer := newLiveProgressWriter(ctx, durable, AuthorizedToolRequest{
		Request:  ToolRequest{ID: "req-1"},
		Redactor: pipeline,
		Progress: sink,
	}, "stdout", redact.BoundaryLogPersistence)
	return writer, durable
}

// TestAUDIT014_OutputIsPublishedBeforeTheProcessEnds covers the central claim
// of AUDIT-014, reconciling M10-035.
//
// Output used to be published only after Finalize, so a long command showed
// nothing until it exited. Here a complete line is published as soon as it is
// written, with no finalization involved.
func TestAUDIT014_OutputIsPublishedBeforeTheProcessEnds(t *testing.T) {
	sink := newRecordingProgressSink()
	writer, _ := newTestLiveWriter(t, t.Context(), sink)

	if _, err := writer.Write([]byte("first line of output\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-sink.firstText:
	case <-time.After(2 * time.Second):
		t.Fatal("no output was published before the process ended")
	}
	if !strings.Contains(sink.content("stdout"), "first line of output") {
		t.Fatalf("published content = %q", sink.content("stdout"))
	}
}

// TestAUDIT014_APartialLineIsHeldBackUntilItCompletes proves the boundary rule
// that keeps a split secret from being published in halves.
func TestAUDIT014_APartialLineIsHeldBackUntilItCompletes(t *testing.T) {
	sink := newRecordingProgressSink()
	writer, _ := newTestLiveWriter(t, t.Context(), sink)

	if _, err := writer.Write([]byte("an incomplete line with no newline")); err != nil {
		t.Fatal(err)
	}
	if sink.content("stdout") != "" {
		t.Fatalf("a partial line was published: %q", sink.content("stdout"))
	}

	if _, err := writer.Write([]byte(" now finished\n")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sink.content("stdout"), "an incomplete line with no newline now finished") {
		t.Fatalf("the completed line was not published: %q", sink.content("stdout"))
	}
}

// TestAUDIT014_TheStreamingBudgetBoundsWhatIsPublished covers backpressure.
//
// A command emitting far more than the budget must not turn the timeline into
// a log file, and what it withheld must be reported rather than left as
// silence.
func TestAUDIT014_TheStreamingBudgetBoundsWhatIsPublished(t *testing.T) {
	sink := newRecordingProgressSink()
	writer, _ := newTestLiveWriter(t, t.Context(), sink)

	line := strings.Repeat("x", 1023) + "\n"
	for written := 0; written < liveProgressBudget*2; written += len(line) {
		if _, err := writer.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}

	if streamed := writer.streamedBytes(); streamed > liveProgressBudget {
		t.Errorf("streamed %d bytes, over the %d budget", streamed, liveProgressBudget)
	}
	if writer.droppedBytes() == 0 {
		t.Error("nothing was recorded as dropped despite exceeding the budget twice over")
	}

	// The withheld amount is stated rather than left as silence.
	reportWithheldOutput(t.Context(), sink, "req-1", "stdout", writer)
	if !strings.Contains(sink.content("stdout"), "bytes withheld") {
		t.Error("the console was not told output had been withheld")
	}
}

// TestAUDIT014_CancellationStopsStreaming covers the cancellation half.
func TestAUDIT014_CancellationStopsStreaming(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	sink := newRecordingProgressSink()
	writer, _ := newTestLiveWriter(t, ctx, sink)

	if _, err := writer.Write([]byte("before cancellation\n")); err != nil {
		t.Fatal(err)
	}
	before := sink.content("stdout")
	if !strings.Contains(before, "before cancellation") {
		t.Fatalf("the first line was not published: %q", before)
	}

	cancel()
	if _, err := writer.Write([]byte("after cancellation\n")); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sink.content("stdout"), "after cancellation") {
		t.Fatal("streaming continued after the command was cancelled")
	}
}

// TestAUDIT014_DurableCaptureSurvivesAFailingSink proves streaming is not
// allowed to cost the authoritative record.
func TestAUDIT014_DurableCaptureSurvivesAFailingSink(t *testing.T) {
	writer, durable := newTestLiveWriter(t, t.Context(), failingProgressSink{})

	if _, err := writer.Write([]byte("recorded regardless\n")); err != nil {
		t.Fatal(err)
	}
	writer.flush()

	result, err := durable.Finalize()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Text, "recorded regardless") {
		t.Fatalf("the durable record lost output when the sink failed: %q", result.Text)
	}
}

type failingProgressSink struct{}

func (failingProgressSink) PublishToolProgress(context.Context, ToolProgress) error {
	return context.DeadlineExceeded
}
