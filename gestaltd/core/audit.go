package core

import (
	"context"
	"time"
)

type AuditEntry struct {
	Timestamp  time.Time
	RequestID  string
	Source     string
	AuthSource string
	UserID     string
	Provider   string
	Operation  string
	Depth      int
	Allowed    bool
	Error      string
	ClientIP   string
	RemoteAddr string
	UserAgent  string
}

type AuditSink interface {
	Log(ctx context.Context, entry AuditEntry)
}
