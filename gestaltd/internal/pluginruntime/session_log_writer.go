package pluginruntime

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/valon-technologies/gestalt/server/services/runtimehost/runtimelogs"
)

type sessionLogWriter struct {
	store               runtimelogs.Store
	runtimeProviderName string
	sessionID           string
	stream              runtimelogs.Stream
	counter             *uint64
}

func newSessionLogWriter(store runtimelogs.Store, runtimeProviderName, sessionID string, stream runtimelogs.Stream, counter *uint64) *sessionLogWriter {
	if store == nil || runtimeProviderName == "" || sessionID == "" || counter == nil {
		return nil
	}
	return &sessionLogWriter{
		store:               store,
		runtimeProviderName: runtimeProviderName,
		sessionID:           sessionID,
		stream:              stream,
		counter:             counter,
	}
}

func (w *sessionLogWriter) Write(p []byte) (int, error) {
	if w == nil || w.store == nil || len(p) == 0 {
		return len(p), nil
	}
	entry := runtimelogs.AppendEntry{
		SourceSeq:  int64(atomic.AddUint64(w.counter, 1)),
		Stream:     w.stream,
		Message:    string(append([]byte(nil), p...)),
		ObservedAt: time.Now().UTC(),
	}
	_, _ = w.store.AppendSessionLogs(context.Background(), w.runtimeProviderName, w.sessionID, []runtimelogs.AppendEntry{entry})
	return len(p), nil
}
