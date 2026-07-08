package agentmanager

import (
	"testing"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

func TestAuditSubjectIDUsesRequestContextSubject(t *testing.T) {
	reqCtx := &proto.RequestContext{
		Subject: &proto.SubjectContext{Id: "user:alice"},
	}
	if got := AuditSubjectID(reqCtx); got != "user:alice" {
		t.Fatalf("AuditSubjectID() = %q, want user:alice", got)
	}
}

func TestIdempotencySubjectIDPrefersTopLevelSubject(t *testing.T) {
	reqCtx := &proto.RequestContext{
		Subject: &proto.SubjectContext{Id: "user:alice"},
	}
	subject := &proto.SubjectContext{Id: "borrower:borrower-1"}
	if got := IdempotencySubjectID(reqCtx, subject); got != "borrower:borrower-1" {
		t.Fatalf("IdempotencySubjectID() = %q, want borrower:borrower-1", got)
	}
}

func TestIdempotencySubjectIDFallsBackToRequestContext(t *testing.T) {
	reqCtx := &proto.RequestContext{
		Subject: &proto.SubjectContext{Id: "user:alice"},
	}
	if got := IdempotencySubjectID(reqCtx, nil); got != "user:alice" {
		t.Fatalf("IdempotencySubjectID() = %q, want user:alice", got)
	}
}
