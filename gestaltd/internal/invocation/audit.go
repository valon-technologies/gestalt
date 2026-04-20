package invocation

import (
	"context"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"go.opentelemetry.io/otel/trace"
)

const auditLogType = "audit"

var _ core.AuditSink = (*SlogAuditSink)(nil)

type SlogAuditSink struct {
	logger *slog.Logger
}

func NewSlogAuditSink(w io.Writer) *SlogAuditSink {
	if w == nil {
		w = os.Stderr
	}
	return &SlogAuditSink{
		logger: slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}
}

func NewLoggerAuditSink(logger *slog.Logger) *SlogAuditSink {
	return newLoggerAuditSink(logger, false)
}

func NewLevelAwareLoggerAuditSink(logger *slog.Logger) *SlogAuditSink {
	return newLoggerAuditSink(logger, true)
}

func newLoggerAuditSink(logger *slog.Logger, respectLevel bool) *SlogAuditSink {
	if logger == nil {
		return NewSlogAuditSink(nil)
	}
	return &SlogAuditSink{
		logger: slog.New(auditHandler{inner: logger.Handler(), respectLevel: respectLevel}),
	}
}

func buildAuditEntry(ctx context.Context, p *principal.Principal, source, providerName, operation string, meta *InvocationMeta) core.AuditEntry {
	reqMeta := RequestMetaFromContext(ctx)
	entry := core.AuditEntry{
		Timestamp:  time.Now(),
		RequestID:  meta.RequestID,
		Source:     source,
		Provider:   providerName,
		Operation:  operation,
		Depth:      meta.Depth,
		ClientIP:   reqMeta.ClientIP,
		RemoteAddr: reqMeta.RemoteAddr,
		UserAgent:  reqMeta.UserAgent,
	}
	if p != nil {
		entry.UserID = p.UserID
		entry.AuthSource = p.AuthSource()
		entry.SubjectID = p.SubjectID
		if p.Kind != "" {
			entry.SubjectKind = string(p.Kind)
		}
	}
	access := AccessContextFromContext(ctx)
	if access.Policy != "" {
		entry.AccessPolicy = access.Policy
	}
	if access.Role != "" {
		entry.AccessRole = access.Role
	}
	if workflow := WorkflowContextFromContext(ctx); workflow != nil {
		if createdBy := WorkflowContextMap(workflow, "createdBy"); createdBy != nil {
			entry.WorkflowCreatedBySubjectID = WorkflowContextString(createdBy, "subjectId")
			entry.WorkflowCreatedBySubjectKind = WorkflowContextString(createdBy, "subjectKind")
			entry.WorkflowCreatedByDisplayName = WorkflowContextString(createdBy, "displayName")
			entry.WorkflowCreatedByAuthSource = WorkflowContextString(createdBy, "authSource")
		}
	}
	return entry
}

func BuildAuditEntry(ctx context.Context, p *principal.Principal, source, providerName, operation string) (context.Context, core.AuditEntry) {
	ctx, meta := ensureMeta(ctx)
	if p == nil {
		p = principal.FromContext(ctx)
	} else {
		p = principal.Canonicalized(p)
	}
	return ctx, buildAuditEntry(ctx, p, source, providerName, operation, meta)
}

func SetCredentialAudit(ctx context.Context, mode core.ConnectionMode, subjectID, connection, instance string) {
	entry := auditEntryFromContext(ctx)
	if entry == nil {
		return
	}
	entry.CredentialMode = string(mode)
	entry.CredentialSubjectID = subjectID
	entry.CredentialConnection = connection
	entry.CredentialInstance = instance
}

func (s *SlogAuditSink) Log(ctx context.Context, entry core.AuditEntry) {
	attrs := []slog.Attr{
		slog.String("log.type", auditLogType),
		slog.Time("event_time", entry.Timestamp),
		slog.String("request_id", entry.RequestID),
		slog.String("source", entry.Source),
		slog.String("provider", entry.Provider),
		slog.String("operation", entry.Operation),
		slog.Int("depth", entry.Depth),
		slog.Bool("allowed", entry.Allowed),
	}

	if entry.AuthSource != "" {
		attrs = append(attrs, slog.String("auth_source", entry.AuthSource))
	}
	if entry.UserID != "" {
		attrs = append(attrs, slog.String("user_id", entry.UserID))
	}
	if entry.SubjectID != "" {
		attrs = append(attrs, slog.String("subject_id", entry.SubjectID))
	}
	if entry.SubjectKind != "" {
		attrs = append(attrs, slog.String("subject_kind", entry.SubjectKind))
	}
	if entry.AccessPolicy != "" {
		attrs = append(attrs, slog.String("access_policy", entry.AccessPolicy))
	}
	if entry.AccessRole != "" {
		attrs = append(attrs, slog.String("access_role", entry.AccessRole))
	}
	if entry.AuthorizationDecision != "" {
		attrs = append(attrs, slog.String("authorization_decision", entry.AuthorizationDecision))
	}
	if entry.CredentialMode != "" {
		attrs = append(attrs, slog.String("credential_mode", entry.CredentialMode))
	}
	if entry.CredentialSubjectID != "" {
		attrs = append(attrs, slog.String("credential_subject_id", entry.CredentialSubjectID))
	}
	if entry.CredentialConnection != "" {
		attrs = append(attrs, slog.String("credential_connection", entry.CredentialConnection))
	}
	if entry.CredentialInstance != "" {
		attrs = append(attrs, slog.String("credential_instance", entry.CredentialInstance))
	}
	if entry.WorkflowCreatedBySubjectID != "" {
		attrs = append(attrs, slog.String("workflow_created_by_subject_id", entry.WorkflowCreatedBySubjectID))
	}
	if entry.WorkflowCreatedBySubjectKind != "" {
		attrs = append(attrs, slog.String("workflow_created_by_subject_kind", entry.WorkflowCreatedBySubjectKind))
	}
	if entry.WorkflowCreatedByDisplayName != "" {
		attrs = append(attrs, slog.String("workflow_created_by_display_name", entry.WorkflowCreatedByDisplayName))
	}
	if entry.WorkflowCreatedByAuthSource != "" {
		attrs = append(attrs, slog.String("workflow_created_by_auth_source", entry.WorkflowCreatedByAuthSource))
	}
	if entry.TargetID != "" {
		attrs = append(attrs, slog.String("target_id", entry.TargetID))
	}
	if entry.TargetKind != "" {
		attrs = append(attrs, slog.String("target_kind", entry.TargetKind))
	}
	if entry.TargetName != "" {
		attrs = append(attrs, slog.String("target_name", entry.TargetName))
	}

	if entry.Error != "" {
		attrs = append(attrs, slog.String("error", entry.Error))
	}

	if entry.ClientIP != "" {
		attrs = append(attrs, slog.String("client_ip", entry.ClientIP))
	}
	if entry.RemoteAddr != "" {
		attrs = append(attrs, slog.String("remote_addr", entry.RemoteAddr))
	}
	if entry.UserAgent != "" {
		attrs = append(attrs, slog.String("user_agent", entry.UserAgent))
	}

	spanCtx := trace.SpanContextFromContext(ctx)
	if spanCtx.IsValid() {
		attrs = append(attrs,
			slog.String("trace_id", spanCtx.TraceID().String()),
			slog.String("span_id", spanCtx.SpanID().String()),
		)
	}

	level := slog.LevelInfo
	if !entry.Allowed {
		level = slog.LevelWarn
	}
	s.logger.LogAttrs(ctx, level, auditLogType, attrs...)
}

type auditHandler struct {
	inner        slog.Handler
	respectLevel bool
}

func (h auditHandler) Enabled(ctx context.Context, level slog.Level) bool {
	if h.respectLevel {
		return h.inner.Enabled(ctx, level)
	}
	return true
}

func (h auditHandler) Handle(ctx context.Context, record slog.Record) error {
	return h.inner.Handle(ctx, record)
}

func (h auditHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return auditHandler{inner: h.inner.WithAttrs(attrs), respectLevel: h.respectLevel}
}

func (h auditHandler) WithGroup(name string) slog.Handler {
	return auditHandler{inner: h.inner.WithGroup(name), respectLevel: h.respectLevel}
}
