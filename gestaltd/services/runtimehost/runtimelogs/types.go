package runtimelogs

import (
	"context"
	"time"
)

type Stream string

const (
	StreamStdout  Stream = "stdout"
	StreamStderr  Stream = "stderr"
	StreamRuntime Stream = "runtime"
)

type SessionRegistration struct {
	RuntimeProviderName string
	SessionID           string
	Metadata            map[string]string
}

type AppendEntry struct {
	SourceSeq  int64
	Stream     Stream
	Message    string
	ObservedAt time.Time
}

type Record struct {
	Seq        int64
	SourceSeq  int64
	Stream     Stream
	Message    string
	ObservedAt time.Time
	AppendedAt time.Time
}

type Store interface {
	RegisterSession(ctx context.Context, registration SessionRegistration) error
	AppendSessionLogs(ctx context.Context, runtimeProviderName, sessionID string, entries []AppendEntry) (int64, error)
	ListSessionLogs(ctx context.Context, runtimeProviderName, sessionID string, afterSeq int64, limit int) ([]Record, error)
	TailSessionLogs(ctx context.Context, runtimeProviderName, sessionID string, limit int) ([]Record, error)
	// MarkSessionStopped marks the runtime lifecycle boundary without removing
	// retained logs. Stores may no-op or keep stopped state internally, and may
	// evict retained sessions through their normal bounded retention policy.
	MarkSessionStopped(ctx context.Context, runtimeProviderName, sessionID string, stoppedAt time.Time) error
}
