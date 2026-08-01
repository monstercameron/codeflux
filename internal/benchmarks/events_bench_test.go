package benchmarks

import (
	"sort"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
)

type replayIdentities struct {
	session   domain.SessionID
	thread    domain.ThreadID
	task      domain.TaskID
	messageID domain.MessageID
}

func newReplayIdentities(tb testing.TB) replayIdentities {
	tb.Helper()
	session, err := domain.NewSessionID()
	if err != nil {
		tb.Fatalf("new session ID: %v", err)
	}
	thread, err := domain.NewThreadID()
	if err != nil {
		tb.Fatalf("new thread ID: %v", err)
	}
	task, err := domain.NewTaskID()
	if err != nil {
		tb.Fatalf("new task ID: %v", err)
	}
	messageID, err := domain.NewMessageID()
	if err != nil {
		tb.Fatalf("new message ID: %v", err)
	}
	return replayIdentities{session: session, thread: thread, task: task, messageID: messageID}
}

func replayBase(identities replayIdentities) events.SessionSnapshot {
	return events.SessionSnapshot{
		SessionID:       identities.session,
		ThreadID:        identities.thread,
		TaskID:          &identities.task,
		TaskState:       domain.TaskStateDraft,
		SnapshotVersion: 1,
		CreatedAt:       time.UnixMicro(1).UTC(),
	}
}

// replayStream builds a contiguous stream of the event a task actually
// produces most of: streamed assistant output.
func replayStream(identities replayIdentities, count int) []events.SessionEvent {
	stream := make([]events.SessionEvent, 0, count)
	for index := range count {
		stream = append(stream, events.SessionEvent{
			Sequence:       uint64(index + 1),
			SessionID:      identities.session,
			ThreadID:       identities.thread,
			TaskID:         &identities.task,
			Timestamp:      time.UnixMicro(int64(index + 2)).UTC(),
			Kind:           events.KindMessageDelta,
			Revision:       uint64(index + 1),
			PayloadVersion: 1,
			Payload: events.Payload{MessageDelta: &events.MessageDelta{
				MessageID:     identities.messageID,
				RedactedDelta: "streamed assistant output segment",
			}},
		})
	}
	return stream
}

// BenchmarkEventAppend is M22-081: throughput AND tail latency.
//
// A mean is not enough here. Event append sits between the worker and the
// browser, so a p99 that is orders of magnitude worse than the mean is felt as
// a stalled UI even when the average looks fine. Per-append latencies are
// collected and the tail is reported alongside the throughput.
func BenchmarkEventAppend(b *testing.B) {
	LogEnvironment(b)
	identities := newReplayIdentities(b)

	// A tail latency is only as trustworthy as the clock behind it. This
	// platform's monotonic clock is coarse enough that a short batch reads as
	// zero, which would quantize the reported p99 into noise. The granularity
	// is therefore measured, and the batch is sized to span it many times over
	// so the derived per-append figure carries real precision. Both are
	// reported, so a reader can see how much to trust the tail.
	granularity := ClockGranularity()
	batchSize := int(granularity.Nanoseconds() / 4)
	if batchSize < 4096 {
		batchSize = 4096
	}
	const minimumBatches = 8
	batches := b.N / batchSize
	if batches < minimumBatches {
		batches = minimumBatches
	}

	snapshot := replayBase(identities)
	stream := replayStream(identities, batches*batchSize)
	latencies := make([]time.Duration, 0, batches)

	b.ReportAllocs()
	b.ResetTimer()
	started := time.Now()
	for batch := range batches {
		batchStarted := time.Now()
		for offset := range batchSize {
			index := batch*batchSize + offset
			next, err := events.ReduceTaskEvents(snapshot, stream[index:index+1])
			if err != nil {
				b.Fatalf("append event %d: %v", index, err)
			}
			snapshot = next
		}
		latencies = append(latencies, time.Since(batchStarted)/time.Duration(batchSize))
	}
	elapsed := time.Since(started)
	b.StopTimer()

	appended := uint64(batches * batchSize)
	if snapshot.ThroughSequence != appended {
		b.Fatalf("appended %d events but reached sequence %d", appended, snapshot.ThroughSequence)
	}
	if elapsed <= 0 {
		b.Fatal("append benchmark measured no elapsed time; the sample is too small to be evidence")
	}
	b.ReportMetric(float64(appended)/elapsed.Seconds(), "appends/s")
	b.ReportMetric(float64(batchSize), "batch-size")
	b.ReportMetric(float64(granularity.Nanoseconds()), "clock-granularity-ns")
	sort.Slice(latencies, func(left, right int) bool { return latencies[left] < latencies[right] })
	b.ReportMetric(float64(percentile(latencies, 0.50).Nanoseconds()), "p50-ns")
	b.ReportMetric(float64(percentile(latencies, 0.99).Nanoseconds()), "p99-ns")
	b.ReportMetric(float64(latencies[len(latencies)-1].Nanoseconds()), "max-ns")
}

// percentile returns the value at a fraction of a sorted slice.
func percentile(sorted []time.Duration, fraction float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	index := int(float64(len(sorted)-1) * fraction)
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}

// BenchmarkReconnectReplay is M22-082.
//
// The three sizes are the plan's, and they are the point: replay is the path a
// user takes after every disconnect, so the cost must be shown to grow with
// the backlog rather than only measured at one convenient size.
func BenchmarkReconnectReplay(b *testing.B) {
	LogEnvironment(b)
	for _, count := range []int{100, 1000, 10000} {
		b.Run(DescribeScale(itoa(count)), func(b *testing.B) {
			identities := newReplayIdentities(b)
			base := replayBase(identities)
			stream := replayStream(identities, count)

			var reduced events.SessionSnapshot
			Measure(b, nil, func() {
				result, err := events.ReduceTaskEvents(base, stream)
				if err != nil {
					b.Fatalf("replay %d events: %v", count, err)
				}
				reduced = result
			})
			if reduced.ThroughSequence != uint64(count) {
				b.Fatalf("replay of %d events reached sequence %d", count, reduced.ThroughSequence)
			}
			b.ReportMetric(float64(count), "events")
		})
	}
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := make([]byte, 0, 8)
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}
