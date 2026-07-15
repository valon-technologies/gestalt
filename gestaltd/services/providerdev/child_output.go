package providerdev

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
)

const (
	childOutputTailLimit = 16 << 10
	childDebugLineLimit  = 4 << 10
)

// boundedOutput keeps only the most recent child output so a noisy dev server
// cannot grow gestaltd's memory without bound.
type boundedOutput struct {
	mu         sync.Mutex
	buf        []byte
	wasTrimmed bool
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(p) >= childOutputTailLimit {
		b.buf = append(b.buf[:0], p[len(p)-childOutputTailLimit:]...)
		b.wasTrimmed = true
		return len(p), nil
	}
	b.buf = append(b.buf, p...)
	if len(b.buf) > childOutputTailLimit {
		b.buf = append([]byte(nil), b.buf[len(b.buf)-childOutputTailLimit:]...)
		b.wasTrimmed = true
	}
	return len(p), nil
}

func (b *boundedOutput) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.buf) == 0 {
		return ""
	}
	value := string(b.buf)
	if b.wasTrimmed {
		return "[…child output truncated…]\n" + value
	}
	return value
}

type childOutput struct {
	stdout      *boundedOutput
	stderr      *boundedOutput
	logger      *slog.Logger
	identity    string
	identityKey string
	command     string
}

func newChildOutput(logger *slog.Logger, identityKey, identity, command string) *childOutput {
	output := &childOutput{
		stdout:      &boundedOutput{},
		stderr:      &boundedOutput{},
		logger:      logger,
		identityKey: identityKey,
		identity:    identity,
		command:     command,
	}
	return output
}

func (o *childOutput) writers() (io.Writer, io.Writer) {
	return &childOutputWriter{logger: o.logger, identityKey: o.identityKey, identity: o.identity, command: o.command, stream: "stdout", tail: o.stdout},
		&childOutputWriter{logger: o.logger, identityKey: o.identityKey, identity: o.identity, command: o.command, stream: "stderr", tail: o.stderr}
}

func (o *childOutput) tail() string {
	var parts []string
	if stdout := strings.TrimSpace(o.stdout.String()); stdout != "" {
		parts = append(parts, "stdout:\n"+stdout)
	}
	if stderr := strings.TrimSpace(o.stderr.String()); stderr != "" {
		parts = append(parts, "stderr:\n"+stderr)
	}
	return strings.Join(parts, "\n")
}

type childOutputWriter struct {
	logger      *slog.Logger
	identityKey string
	identity    string
	command     string
	stream      string
	tail        *boundedOutput
	buf         []byte
}

func (w *childOutputWriter) Write(p []byte) (int, error) {
	_, _ = w.tail.Write(p)
	if w.logger == nil || !w.logger.Enabled(context.Background(), slog.LevelDebug) {
		return len(p), nil
	}
	w.buf = append(w.buf, p...)
	for {
		idx := bytes.IndexByte(w.buf, '\n')
		if idx < 0 {
			break
		}
		w.logLine(strings.TrimRight(string(w.buf[:idx]), "\r"))
		w.buf = w.buf[idx+1:]
	}
	if len(w.buf) > childDebugLineLimit {
		w.logLine(string(w.buf[:childDebugLineLimit]) + "…")
		w.buf = w.buf[:0]
	}
	return len(p), nil
}

func (w *childOutputWriter) logLine(line string) {
	if strings.TrimSpace(line) == "" {
		return
	}
	w.logger.Debug("dev child output", w.identityKey, w.identity, "command", w.command, "stream", w.stream, "line", line)
}

func childFailureLogArgs(output *childOutput) []any {
	if output == nil {
		return nil
	}
	if tail := output.tail(); tail != "" {
		return []any{"output_tail", fmt.Sprintf("\n%s", tail)}
	}
	return nil
}
