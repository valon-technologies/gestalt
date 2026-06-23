package runtimeprovider

import (
	"fmt"
	"io"
	"sync"
)

// providerLogWriter prefixes every line written to it with provider=<name>
// so that provider process output tee'd to the container stderr can be
// filtered in Cloud Logging.
type providerLogWriter struct {
	w      io.Writer
	prefix string
	mu     sync.Mutex
	buf    []byte
}

func newProviderLogWriter(w io.Writer, providerName string) *providerLogWriter {
	if w == nil {
		return nil
	}
	if providerName == "" {
		providerName = "unknown"
	}
	return &providerLogWriter{
		w:      w,
		prefix: fmt.Sprintf("provider=%s ", providerName),
	}
}

func (w *providerLogWriter) Write(p []byte) (int, error) {
	if w == nil || w.w == nil {
		return len(p), nil
	}
	if len(p) == 0 {
		return 0, nil
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	prevLen := len(w.buf)
	data := append(append([]byte(nil), w.buf...), p...)
	w.buf = nil

	start := 0
	for i := 0; i <= len(data); i++ {
		if i < len(data) && data[i] != '\n' {
			continue
		}
		if i == len(data) {
			if start < len(data) {
				w.buf = append([]byte(nil), data[start:]...)
			}
			return len(p), nil
		}
		if _, err := fmt.Fprintf(w.w, "%s%s\n", w.prefix, data[start:i]); err != nil {
			consumed := i - prevLen
			if consumed < 0 {
				consumed = 0
			}
			if consumed > len(p) {
				consumed = len(p)
			}
			return consumed, err
		}
		start = i + 1
	}

	return len(p), nil
}

func (w *providerLogWriter) Flush() error {
	if w == nil || w.w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.buf) == 0 {
		return nil
	}
	_, err := fmt.Fprintf(w.w, "%s%s", w.prefix, w.buf)
	w.buf = nil
	return err
}

func (w *providerLogWriter) Close() error {
	return w.Flush()
}
