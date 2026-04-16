package core

import (
	"context"
	"time"
)

type AuditEntry struct {
	Timestamp                    time.Time
	RequestID                    string
	Source                       string
	AuthSource                   string
	UserID                       string
	SubjectID                    string
	SubjectKind                  string
	AccessPolicy                 string
	AccessRole                   string
	AuthorizationDecision        string
	CredentialMode               string
	CredentialSubjectID          string
	CredentialConnection         string
	CredentialInstance           string
	WorkflowCreatedBySubjectID   string
	WorkflowCreatedBySubjectKind string
	WorkflowCreatedByDisplayName string
	WorkflowCreatedByAuthSource  string
	TargetID                     string
	TargetKind                   string
	TargetName                   string
	Provider                     string
	Operation                    string
	Depth                        int
	Allowed                      bool
	Error                        string
	ClientIP                     string
	RemoteAddr                   string
	UserAgent                    string
}

type AuditSink interface {
	Log(ctx context.Context, entry AuditEntry)
}
