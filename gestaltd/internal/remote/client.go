package remote

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/url"
	"strings"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

type Config struct {
	URL   string
	Token string
}

type ClientSet struct {
	App       proto.AppClient
	Agent     proto.AgentClient
	Workflow  proto.WorkflowClient
	IndexedDB proto.IndexedDBClient

	conn *grpc.ClientConn
}

func NewClientSet(ctx context.Context, cfg Config) (*ClientSet, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	target, dialOpts, err := dialOptions(cfg)
	if err != nil {
		return nil, err
	}
	conn, err := grpc.NewClient(target, dialOpts...)
	if err != nil {
		return nil, err
	}
	conn.Connect()
	if err := ctx.Err(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return &ClientSet{
		App:       proto.NewAppClient(conn),
		Agent:     proto.NewAgentClient(conn),
		Workflow:  proto.NewWorkflowClient(conn),
		IndexedDB: proto.NewIndexedDBClient(conn),
		conn:      conn,
	}, nil
}

func (cs *ClientSet) Close() error {
	if cs == nil || cs.conn == nil {
		return nil
	}
	return cs.conn.Close()
}

func WithBearer(ctx context.Context, token string) context.Context {
	token = strings.TrimSpace(token)
	if token == "" {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
}

func dialOptions(cfg Config) (string, []grpc.DialOption, error) {
	rawURL := strings.TrimSpace(cfg.URL)
	if rawURL == "" {
		return "", nil, fmt.Errorf("remote gestaltd URL is required")
	}
	token := strings.TrimSpace(cfg.Token)
	if token == "" {
		return "", nil, fmt.Errorf("remote gestaltd token is required")
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", nil, fmt.Errorf("parse remote gestaltd URL: %w", err)
	}
	if !parsed.IsAbs() || parsed.Host == "" {
		return "", nil, fmt.Errorf("remote gestaltd URL must be absolute")
	}

	target, creds, err := transportCredentials(parsed)
	if err != nil {
		return "", nil, err
	}

	opts := []grpc.DialOption{
		creds,
		grpc.WithUnaryInterceptor(bearerUnaryInterceptor(token)),
		grpc.WithStreamInterceptor(bearerStreamInterceptor(token)),
	}
	return target, opts, nil
}

func transportCredentials(parsed *url.URL) (target string, opt grpc.DialOption, err error) {
	host := parsed.Hostname()
	port := parsed.Port()
	switch strings.ToLower(parsed.Scheme) {
	case "https":
		if port == "" {
			port = "443"
		}
		return net.JoinHostPort(host, port), grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
			MinVersion: tls.VersionTLS12,
			ServerName: host,
		})), nil
	case "http":
		if port == "" {
			port = "80"
		}
		return net.JoinHostPort(host, port), grpc.WithTransportCredentials(insecure.NewCredentials()), nil
	default:
		return "", nil, fmt.Errorf("remote gestaltd URL must use http or https")
	}
}

func bearerUnaryInterceptor(token string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		return invoker(WithBearer(ctx, token), method, req, reply, cc, opts...)
	}
}

func bearerStreamInterceptor(token string) grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		return streamer(WithBearer(ctx, token), desc, cc, method, opts...)
	}
}
