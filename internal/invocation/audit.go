package invocation

import (
	"context"
	"io"
	"log/slog"
	"os"

	"github.com/valon-technologies/gestalt/core"
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

func (s *SlogAuditSink) Log(ctx context.Context, entry core.AuditEntry) {
	attrs := []slog.Attr{
		slog.String("log.type", auditLogType),
		slog.Time("event_time", entry.Timestamp),
		slog.String("request_id", entry.RequestID),
		slog.String("source", entry.Source),
		slog.String("user_id", entry.UserID),
		slog.String("provider", entry.Provider),
		slog.String("operation", entry.Operation),
		slog.Int("depth", entry.Depth),
		slog.Bool("allowed", entry.Allowed),
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
