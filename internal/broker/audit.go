package broker

import (
	"log"

	"github.com/valon-technologies/toolshed/core"
)

var _ core.AuditSink = LogAuditSink{}

type LogAuditSink struct{}

func (LogAuditSink) Log(e core.AuditEntry) {
	if e.Allowed {
		log.Printf("audit: %s src=%s user=%s %s/%s depth=%d",
			e.RequestID, e.Source, e.UserID, e.Provider, e.Operation, e.Depth)
	} else {
		log.Printf("audit: %s src=%s user=%s %s/%s depth=%d DENIED: %s",
			e.RequestID, e.Source, e.UserID, e.Provider, e.Operation, e.Depth, e.Error)
	}
}
