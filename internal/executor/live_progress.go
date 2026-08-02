package executor

import (
	"bytes"
	"context"
	"sync"

	"codeflux.dev/codeflux/internal/redact"
)

// liveProgressBudget bounds how much redacted output one command may stream
// while it runs. Beyond it the tail is dropped and the drop is counted.
//
// The bound is the point. Streaming exists so a person can see a long command
// making progress, and a command that emits megabytes would otherwise turn a
// supervision console into a firehose and the event journal into a log file.
// The authoritative record is the finalized redacted result, which has its own
// bounds, so dropping here loses nothing that is not recorded elsewhere.
const liveProgressBudget = 256 << 10

// liveProgressWriter tees process output into the durable redaction stream and
// publishes bounded, incrementally redacted chunks while the process runs
// (M10-035, AUDIT-014).
//
// Before this, output was published only after the process exited: a fifteen
// minute test run showed nothing at all until it finished, which is the exact
// case a live console exists for.
//
// Chunks are flushed on line boundaries only. A secret split across a chunk
// boundary would otherwise be published in halves that neither pattern
// matches, and a trailing partial line is held back until it completes. This
// makes streamed output slightly behind the process rather than slightly
// unsafe. The finalized result remains the authoritative redacted record; this
// stream is a view of it in progress.
type liveProgressWriter struct {
	sink      ToolProgressSink
	redactor  *redact.Pipeline
	boundary  redact.Boundary
	requestID string
	stream    string

	// ctx bounds publication. A cancelled command stops streaming rather than
	// continuing to publish for a process nobody is waiting on.
	ctx context.Context

	mu      sync.Mutex
	durable *redact.Stream
	pending bytes.Buffer
	// published counts redacted bytes already emitted, against the budget.
	published int
	// dropped counts bytes the budget refused, so the console can say output
	// was withheld rather than implying the command went quiet.
	dropped int
}

func newLiveProgressWriter(
	ctx context.Context,
	durable *redact.Stream,
	authorized AuthorizedToolRequest,
	stream string,
	boundary redact.Boundary,
) *liveProgressWriter {
	return &liveProgressWriter{
		sink: authorized.Progress, redactor: authorized.Redactor,
		boundary: boundary, requestID: authorized.Request.ID,
		stream: stream, ctx: ctx, durable: durable,
	}
}

// Write records the bytes durably first, then streams what it safely can.
//
// Durable capture is never skipped for the sake of streaming: if publishing
// fails or the budget is spent, the finalized result is still complete.
func (writer *liveProgressWriter) Write(chunk []byte) (int, error) {
	written, err := writer.durable.Write(chunk)
	if err != nil {
		return written, err
	}
	if writer.sink == nil || writer.redactor == nil {
		return written, nil
	}

	writer.mu.Lock()
	writer.pending.Write(chunk[:written])
	ready := writer.takeCompleteLinesLocked()
	writer.mu.Unlock()

	if len(ready) != 0 {
		writer.publish(ready)
	}
	return written, nil
}

// takeCompleteLinesLocked removes and returns whole lines from the buffer.
func (writer *liveProgressWriter) takeCompleteLinesLocked() []byte {
	buffered := writer.pending.Bytes()
	end := bytes.LastIndexByte(buffered, '\n')
	if end < 0 {
		// No complete line yet. A process emitting one enormous line without a
		// newline would otherwise never stream, so the buffer is flushed once
		// it exceeds a chunk regardless.
		if writer.pending.Len() < redactedProgressChunkBytes {
			return nil
		}
		end = redactedProgressChunkBytes - 1
	}
	ready := make([]byte, end+1)
	_, _ = writer.pending.Read(ready)
	return ready
}

// flush publishes whatever is left, including a trailing partial line.
//
// It runs once the process has exited, when there is no longer a later chunk
// that could complete the line.
func (writer *liveProgressWriter) flush() {
	if writer.sink == nil || writer.redactor == nil {
		return
	}
	writer.mu.Lock()
	remaining := writer.pending.Bytes()
	ready := make([]byte, len(remaining))
	copy(ready, remaining)
	writer.pending.Reset()
	writer.mu.Unlock()
	if len(ready) != 0 {
		writer.publish(ready)
	}
}

// publish redacts and emits, respecting the budget and cancellation.
func (writer *liveProgressWriter) publish(raw []byte) {
	// Cancellation stops streaming immediately. The durable record still
	// completes; there is simply nobody left watching this.
	if writer.ctx.Err() != nil {
		return
	}

	result, err := writer.redactor.Redact(writer.boundary, string(raw))
	if err != nil {
		// A chunk that cannot be redacted is never published in the raw. The
		// finalized result will carry it, redacted, at the end.
		return
	}
	content := result.Text

	writer.mu.Lock()
	remaining := liveProgressBudget - writer.published
	if remaining <= 0 {
		writer.dropped += len(content)
		writer.mu.Unlock()
		return
	}
	if len(content) > remaining {
		writer.dropped += len(content) - remaining
		content = content[:remaining]
	}
	writer.published += len(content)
	writer.mu.Unlock()

	for len(content) != 0 {
		if writer.ctx.Err() != nil {
			return
		}
		size := min(len(content), redactedProgressChunkBytes)
		publishProgress(writer.ctx, writer.sink, ToolProgress{
			RequestID: writer.requestID, State: "running",
			Stream: writer.stream, Content: content[:size],
		})
		content = content[size:]
	}
}

// droppedBytes reports what the budget withheld.
func (writer *liveProgressWriter) droppedBytes() int {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return writer.dropped
}

// streamedBytes reports what was published while the process ran.
func (writer *liveProgressWriter) streamedBytes() int {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return writer.published
}

// reportWithheldOutput states what the streaming budget refused.
//
// A console that simply stopped receiving output cannot tell a quiet command
// from a truncated one, and "no more output" is a different fact from "output
// withheld". Nothing is published when nothing was dropped.
func reportWithheldOutput(
	ctx context.Context,
	sink ToolProgressSink,
	requestID string,
	stream string,
	writer *liveProgressWriter,
) {
	if sink == nil || writer == nil {
		return
	}
	dropped := writer.droppedBytes()
	if dropped == 0 {
		return
	}
	publishProgress(ctx, sink, ToolProgress{
		RequestID: requestID, State: "running", Stream: stream,
		Content: "\n[" + itoa(dropped) + " further bytes withheld: " +
			"live output budget reached; the full redacted output is in the result]\n",
	})
}

// itoa avoids importing strconv into this file for one call.
func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := [20]byte{}
	index := len(digits)
	for value > 0 {
		index--
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[index:])
}
