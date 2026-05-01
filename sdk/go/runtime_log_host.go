package gestalt

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const EnvRuntimeLogHostSocket = "GESTALT_RUNTIME_LOG_SOCKET"
const EnvRuntimeLogHostSocketToken = EnvRuntimeLogHostSocket + "_TOKEN"
const EnvRuntimeSessionID = "GESTALT_RUNTIME_SESSION_ID"

type RuntimeLogHostClient struct {
	client    proto.PluginRuntimeLogHostClient
	sourceSeq atomic.Int64
}

var sharedRuntimeLogHostTransport sharedManagerTransport[proto.PluginRuntimeLogHostClient]

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

func (c *RuntimeLogHostClient) Close() error {
	return nil
}

func (c *RuntimeLogHostClient) AppendLogs(ctx context.Context, req *proto.AppendPluginRuntimeLogsRequest) (*proto.AppendPluginRuntimeLogsResponse, error) {
	return c.client.AppendLogs(ctx, req)
}

type RuntimeLogAppendOption func(*runtimeLogAppendOptions)

type runtimeLogAppendOptions struct {
	sessionID string
	stream    proto.PluginRuntimeLogStream
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
func WithRuntimeLogStream(stream proto.PluginRuntimeLogStream) RuntimeLogAppendOption {
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
func (c *RuntimeLogHostClient) Append(ctx context.Context, message string, opts ...RuntimeLogAppendOption) (*proto.AppendPluginRuntimeLogsResponse, error) {
	cfg := runtimeLogAppendOptions{
		stream: proto.PluginRuntimeLogStream_PLUGIN_RUNTIME_LOG_STREAM_RUNTIME,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if cfg.sessionID == "" {
		sessionID, err := RuntimeSessionID()
		if err != nil {
			return nil, err
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
	return c.AppendLogs(ctx, &proto.AppendPluginRuntimeLogsRequest{
		SessionId: cfg.sessionID,
		Logs: []*proto.PluginRuntimeLogEntry{{
			Stream:     cfg.stream,
			Message:    message,
			ObservedAt: cfg.observed,
			SourceSeq:  sourceSeq,
		}},
	})
}

func (c *RuntimeLogHostClient) advanceSourceSeq(sourceSeq int64) {
	for {
		current := c.sourceSeq.Load()
		if current >= sourceSeq || c.sourceSeq.CompareAndSwap(current, sourceSeq) {
			return
		}
	}
}
