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

// Config configures an authenticated client to a remote public gestaltd API.
type Config struct {
	URL   string
	Token string
	// DialOptions overrides the default transport derived from URL. Intended for tests.
	DialOptions []grpc.DialOption
}

// ClientSet exposes generated public gRPC clients for remote delegation.
type ClientSet struct {
	App       proto.AppClient
	Agent     proto.AgentClient
	Workflow  proto.WorkflowClient
	IndexedDB proto.IndexedDBClient
	Close     func() error
}

// NewClientSet dials a remote public gestaltd and returns typed gRPC clients.
func NewClientSet(_ context.Context, cfg Config) (*ClientSet, error) {
	target, dialOpts, err := resolveDialConfig(cfg)
	if err != nil {
		return nil, err
	}
	token := strings.TrimSpace(cfg.Token)
	if token == "" {
		return nil, fmt.Errorf("remote token is required")
	}
	conn, err := grpc.NewClient(target, dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("dial remote gestaltd %q: %w", cfg.URL, err)
	}
	return &ClientSet{
		App:       proto.NewAppClient(conn),
		Agent:     proto.NewAgentClient(conn),
		Workflow:  proto.NewWorkflowClient(conn),
		IndexedDB: proto.NewIndexedDBClient(conn),
		Close: func() error {
			return conn.Close()
		},
	}, nil
}

// WithBearer attaches the remote bearer token to outgoing gRPC metadata.
func WithBearer(ctx context.Context, token string) context.Context {
	token = strings.TrimSpace(token)
	if token == "" {
		return ctx
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
}

func resolveDialConfig(cfg Config) (string, []grpc.DialOption, error) {
	if len(cfg.DialOptions) > 0 {
		target, _, err := dialOptions(cfg.URL)
		if err != nil {
			return "", nil, err
		}
		return target, cfg.DialOptions, nil
	}
	return dialOptions(cfg.URL)
}

func dialOptions(rawURL string) (string, []grpc.DialOption, error) {
	rawURL = strings.TrimRight(strings.TrimSpace(rawURL), "/")
	if rawURL == "" {
		return "", nil, fmt.Errorf("remote URL is required")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", nil, fmt.Errorf("parse remote URL %q: %w", rawURL, err)
	}
	if !parsed.IsAbs() || parsed.Host == "" {
		return "", nil, fmt.Errorf("remote URL %q must be an absolute URL", rawURL)
	}
	if path := strings.TrimSpace(parsed.EscapedPath()); path != "" && path != "/" {
		return "", nil, fmt.Errorf("remote URL %q must not include a path", rawURL)
	}
	host := parsed.Host
	switch strings.ToLower(parsed.Scheme) {
	case "https":
		serverName, _, splitErr := net.SplitHostPort(host)
		if splitErr != nil {
			serverName = host
		}
		return host, []grpc.DialOption{
			grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
				MinVersion: tls.VersionTLS12,
				ServerName: serverName,
				NextProtos: []string{"h2"},
			})),
		}, nil
	case "http":
		return host, []grpc.DialOption{
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		}, nil
	default:
		return "", nil, fmt.Errorf("remote URL %q must use http or https", rawURL)
	}
}
