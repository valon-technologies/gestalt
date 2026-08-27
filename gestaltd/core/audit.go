package core

import (
	"context"
	"time"
)

type AuditEntry struct {
	Timestamp               time.Time
	RequestID               string
	Source                  string
	SubjectID               string
	CreatedBy               string
	AgentSubjectID          string
	RunAsSubjectID          string
	AccessPolicy            string
	AccessRole              string
	AuthorizationDecision   string
	CredentialMode          string
	CredentialSubjectID     string
	CredentialConnection    string
	CredentialInstance      string
	WorkflowKeySHA256       string
	CallerApp               string
	WorkflowTargetKind      string
	WorkflowTargetComponent string
	WorkflowTargetProvider  string
	WorkflowTargetOperation string
	TargetID                string
	TargetKind              string
	TargetName              string
	Provider                string
	Operation               string
	Depth                   int
	Allowed                 bool
	Outcome                 string
	FailureCause            string
	FailureReason           string
	Error                   string
	ClientIP                string
	RemoteAddr              string
	UserAgent               string
}

type AuditSink interface {
	Log(ctx context.Context, entry AuditEntry)
}
