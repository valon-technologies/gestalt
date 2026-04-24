package pluginruntime

import (
	"io"
	"strings"
	"sync"
	"time"
)

const (
	defaultLogBufferEntries   = 512
	maxSessionLogPendingBytes = 64 * 1024
)

type sessionLogBuffer struct {
	mu      sync.Mutex
	max     int
	total   int
	entries []LogEntry
	pending map[LogStream]string
}

func newSessionLogBuffer(maxEntries int) *sessionLogBuffer {
	if maxEntries <= 0 {
		maxEntries = defaultLogBufferEntries
	}
	return &sessionLogBuffer{
		max:     maxEntries,
		entries: make([]LogEntry, 0, maxEntries),
		pending: map[LogStream]string{},
	}
}

func (b *sessionLogBuffer) writer(stream LogStream, mirror io.Writer) io.Writer {
	return &sessionLogWriter{
		buffer: b,
		stream: stream,
		mirror: mirror,
	}
}

func (b *sessionLogBuffer) add(stream LogStream, message string, observedAt time.Time) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.appendLocked(LogEntry{
		Stream:     normalizeLogStream(stream),
		Message:    message,
		ObservedAt: observedAt.UTC(),
	})
}

func (b *sessionLogBuffer) snapshot(tailEntries int) ([]LogEntry, bool) {
	if b == nil {
		return nil, false
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	entries := make([]LogEntry, 0, len(b.entries)+len(b.pending))
	entries = append(entries, b.entries...)
	for _, stream := range []LogStream{LogStreamStdout, LogStreamStderr, LogStreamRuntime} {
		if fragment := b.pending[stream]; strings.TrimSpace(fragment) != "" {
			entries = append(entries, LogEntry{
				Stream:     stream,
				Message:    fragment,
				ObservedAt: time.Now().UTC(),
			})
		}
	}
	if len(entries) == 0 {
		return nil, false
	}

	truncated := b.total > len(b.entries)
	if tailEntries > 0 && tailEntries < len(entries) {
		entries = append([]LogEntry(nil), entries[len(entries)-tailEntries:]...)
		return entries, true
	}
	return append([]LogEntry(nil), entries...), truncated
}

func (b *sessionLogBuffer) capture(stream LogStream, chunk string, observedAt time.Time) {
	if b == nil || chunk == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	stream = normalizeLogStream(stream)
	pending := b.pending[stream] + chunk
	for {
		idx := strings.IndexByte(pending, '\n')
		if idx >= 0 {
			line := strings.TrimRight(pending[:idx], "\r")
			b.appendLocked(LogEntry{
				Stream:     stream,
				Message:    line,
				ObservedAt: observedAt.UTC(),
			})
			pending = pending[idx+1:]
			continue
		}
		if len(pending) <= maxSessionLogPendingBytes {
			break
		}
		b.appendLocked(LogEntry{
			Stream:     stream,
			Message:    pending[:maxSessionLogPendingBytes],
			ObservedAt: observedAt.UTC(),
		})
		pending = pending[maxSessionLogPendingBytes:]
	}
	b.pending[stream] = pending
}

func (b *sessionLogBuffer) appendLocked(entry LogEntry) {
	b.total++
	if len(b.entries) == b.max {
		copy(b.entries, b.entries[1:])
		b.entries[len(b.entries)-1] = entry
		return
	}
	b.entries = append(b.entries, entry)
}

func normalizeLogStream(stream LogStream) LogStream {
	switch stream {
	case LogStreamStdout, LogStreamStderr:
		return stream
	default:
		return LogStreamRuntime
	}
}

type sessionLogWriter struct {
	buffer *sessionLogBuffer
	stream LogStream
	mirror io.Writer
}

func (w *sessionLogWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if w.buffer != nil {
		w.buffer.capture(w.stream, string(p), time.Now())
	}
	if w.mirror == nil {
		return len(p), nil
	}
	n, err := w.mirror.Write(p)
	if err != nil {
		return n, err
	}
	return len(p), nil
}
