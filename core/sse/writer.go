package sse

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// Writer writes Server-Sent Events to an http.ResponseWriter.
type Writer struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

// NewWriter creates an SSE writer. It sets the required headers and
// verifies the ResponseWriter supports flushing.
func NewWriter(w http.ResponseWriter) (*Writer, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("sse: response writer does not support flushing")
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	return &Writer{w: w, flusher: flusher}, nil
}

// WriteEvent writes a single SSE event and flushes.
func (w *Writer) WriteEvent(event, data string) error {
	if _, err := fmt.Fprintf(w.w, "event: %s\ndata: %s\n\n", event, data); err != nil {
		return err
	}
	w.flusher.Flush()
	return nil
}

// WriteJSON marshals v as JSON and writes it as an SSE event.
func (w *Writer) WriteJSON(event string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return w.WriteEvent(event, string(data))
}
