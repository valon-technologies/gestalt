package invocation

import (
	"log"

	"github.com/valon-technologies/toolshed/core"
)

var _ core.AuditSink = LogAuditSink{}

type LogAuditSink struct{}

func (LogAuditSink) Log(entry core.AuditEntry) {
	if entry.Allowed {
		log.Printf("audit: %s src=%s user=%s %s/%s depth=%d",
			entry.RequestID, entry.Source, entry.UserID, entry.Provider, entry.Operation, entry.Depth)
		return
	}
	log.Printf("audit: %s src=%s user=%s %s/%s depth=%d DENIED: %s",
		entry.RequestID, entry.Source, entry.UserID, entry.Provider, entry.Operation, entry.Depth, entry.Error)
}
