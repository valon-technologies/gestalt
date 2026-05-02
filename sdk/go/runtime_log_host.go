package gestalt

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// EnvRuntimeLogHostSocket names the environment variable containing the
// runtime-log service target.
const EnvRuntimeLogHostSocket = "GESTALT_RUNTIME_LOG_SOCKET"

// EnvRuntimeLogHostSocketToken names the optional runtime-log relay-token variable.
const EnvRuntimeLogHostSocketToken = EnvRuntimeLogHostSocket + "_TOKEN"

// EnvRuntimeSessionID names the environment variable containing the current
// plugin-runtime session id.
const EnvRuntimeSessionID = "GESTALT_RUNTIME_SESSION_ID"

// RuntimeLogHostClient appends plugin-runtime logs to the host.
type RuntimeLogHostClient struct {
	client    proto.PluginRuntimeLogHostClient
	sourceSeq atomic.Int64
}

var sharedRuntimeLogHostTransport sharedManagerTransport[proto.PluginRuntimeLogHostClient]

// RuntimeLogStream identifies the stream that produced a runtime log entry.
type RuntimeLogStream string

const (
	RuntimeLogStreamRuntime RuntimeLogStream = "runtime"
	RuntimeLogStreamStdout  RuntimeLogStream = "stdout"
	RuntimeLogStreamStderr  RuntimeLogStream = "stderr"
)

// RuntimeLogEntry is one log entry appended by a hosted runtime.
type RuntimeLogEntry struct {
	Stream     RuntimeLogStream
	Message    string
	ObservedAt time.Time
	SourceSeq  int64
}

// RuntimeLogHost returns a shared client for the runtime-log host service.
func RuntimeLogHost() (*RuntimeLogHostClient, error) {
	target := strings.TrimSpace(os.Getenv(EnvRuntimeLogHostSocket))
	if target == "" {
		return nil, fmt.Errorf("runtime log host: %s is not set", EnvRuntimeLogHostSocket)
	}
	token := strings.TrimSpace(os.Getenv(EnvRuntimeLogHostSocketToken))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := managerTransportClient(ctx, "runtime log host", target, token, &sharedRuntimeLogHostTransport, proto.NewPluginRuntimeLogHostClient)
	if err != nil {
		return nil, err
	}
	return &RuntimeLogHostClient{client: client}, nil
}

// Close is a no-op compatibility method because this client uses shared transport.
func (c *RuntimeLogHostClient) Close() error {
	return nil
}

// AppendLogs appends runtime logs for one hosted runtime session.
func (c *RuntimeLogHostClient) AppendLogs(ctx context.Context, sessionID string, logs []RuntimeLogEntry) error {
	_, err := c.client.AppendLogs(ctx, &proto.AppendPluginRuntimeLogsRequest{
		SessionId: strings.TrimSpace(sessionID),
		Logs:      runtimeLogEntriesToProto(logs),
	})
	return err
}

// RuntimeLogAppendOption configures a single Append call.
type RuntimeLogAppendOption func(*runtimeLogAppendOptions)

type runtimeLogAppendOptions struct {
	sessionID string
	stream    RuntimeLogStream
	observed  *timestamppb.Timestamp
	sourceSeq *int64
}

// RuntimeSessionID returns the runtime session id injected into hosted plugin
// processes by gestaltd.
func RuntimeSessionID() (string, error) {
	sessionID := strings.TrimSpace(os.Getenv(EnvRuntimeSessionID))
	if sessionID == "" {
		return "", fmt.Errorf("runtime session: %s is not set", EnvRuntimeSessionID)
	}
	return sessionID, nil
}

// WithRuntimeLogSessionID overrides the runtime session id used by Append.
func WithRuntimeLogSessionID(sessionID string) RuntimeLogAppendOption {
	return func(opts *runtimeLogAppendOptions) {
		opts.sessionID = strings.TrimSpace(sessionID)
	}
}

// WithRuntimeLogStream overrides the log stream used by Append.
func WithRuntimeLogStream(stream RuntimeLogStream) RuntimeLogAppendOption {
	return func(opts *runtimeLogAppendOptions) {
		opts.stream = stream
	}
}

// WithRuntimeLogObservedAt overrides the observed timestamp used by Append.
func WithRuntimeLogObservedAt(observedAt time.Time) RuntimeLogAppendOption {
	return func(opts *runtimeLogAppendOptions) {
		opts.observed = timestamppb.New(observedAt)
	}
}

// WithRuntimeLogSourceSeq overrides the source sequence used by Append.
func WithRuntimeLogSourceSeq(sourceSeq int64) RuntimeLogAppendOption {
	return func(opts *runtimeLogAppendOptions) {
		opts.sourceSeq = &sourceSeq
	}
}

// Append records one runtime log entry for the current hosted runtime session.
func (c *RuntimeLogHostClient) Append(ctx context.Context, message string, opts ...RuntimeLogAppendOption) error {
	cfg := runtimeLogAppendOptions{
		stream: RuntimeLogStreamRuntime,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if cfg.sessionID == "" {
		sessionID, err := RuntimeSessionID()
		if err != nil {
			return err
		}
		cfg.sessionID = sessionID
	}
	if cfg.observed == nil {
		cfg.observed = timestamppb.Now()
	}
	sourceSeq := int64(0)
	if cfg.sourceSeq != nil {
		sourceSeq = *cfg.sourceSeq
		c.advanceSourceSeq(sourceSeq)
	} else {
		sourceSeq = c.sourceSeq.Add(1)
	}
	return c.AppendLogs(ctx, cfg.sessionID, []RuntimeLogEntry{{
		Stream:     cfg.stream,
		Message:    message,
		ObservedAt: cfg.observed.AsTime(),
		SourceSeq:  sourceSeq,
	}})
}

func (c *RuntimeLogHostClient) advanceSourceSeq(sourceSeq int64) {
	for {
		current := c.sourceSeq.Load()
		if current >= sourceSeq || c.sourceSeq.CompareAndSwap(current, sourceSeq) {
			return
		}
	}
}

func runtimeLogEntriesToProto(logs []RuntimeLogEntry) []*proto.PluginRuntimeLogEntry {
	out := make([]*proto.PluginRuntimeLogEntry, 0, len(logs))
	for _, log := range logs {
		entry := &proto.PluginRuntimeLogEntry{
			Stream:    runtimeLogStreamToProto(log.Stream),
			Message:   log.Message,
			SourceSeq: log.SourceSeq,
		}
		if !log.ObservedAt.IsZero() {
			entry.ObservedAt = timestamppb.New(log.ObservedAt)
		}
		out = append(out, entry)
	}
	return out
}

func runtimeLogStreamToProto(stream RuntimeLogStream) proto.PluginRuntimeLogStream {
	switch stream {
	case RuntimeLogStreamStdout:
		return proto.PluginRuntimeLogStream_PLUGIN_RUNTIME_LOG_STREAM_STDOUT
	case RuntimeLogStreamStderr:
		return proto.PluginRuntimeLogStream_PLUGIN_RUNTIME_LOG_STREAM_STDERR
	default:
		return proto.PluginRuntimeLogStream_PLUGIN_RUNTIME_LOG_STREAM_RUNTIME
	}
}
