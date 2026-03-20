package core

import "time"

type AuditEntry struct {
	Timestamp time.Time
	RequestID string
	Source    string
	UserID    string
	Provider  string
	Operation string
	Depth     int
	Allowed   bool
	Error     string
}

type AuditSink interface {
	Log(entry AuditEntry)
}
