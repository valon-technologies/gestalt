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

// Config configures a client to a remote public gestaltd API.
type Config struct {
	URL       string
	Token     string
	TLSConfig *tls.Config
}

// ClientSet exposes typed public gRPC clients for a remote gestaltd instance.
type ClientSet struct {
	App           proto.AppClient
	Agent         proto.AgentClient
	Workflow      proto.WorkflowClient
	IndexedDB     proto.IndexedDBClient
	Identity      proto.IdentityClient
	Authorization proto.AuthorizationClient

	conn *grpc.ClientConn
}

// Dial opens a gRPC connection to the remote public gestaltd surface.
// Token is optional; when set, bearer authorization is attached to every RPC.
func Dial(_ context.Context, cfg Config) (*grpc.ClientConn, error) {
	url := strings.TrimSpace(cfg.URL)
	if url == "" {
		return nil, fmt.Errorf("remote: URL is required")
	}

	target, creds, err := grpcTarget(url, cfg.TLSConfig)
	if err != nil {
		return nil, err
	}

	opts := []grpc.DialOption{grpc.WithTransportCredentials(creds)}
	if token := strings.TrimSpace(cfg.Token); token != "" {
		unary, stream := bearerTokenInterceptors(token)
		opts = append(opts,
			grpc.WithChainUnaryInterceptor(unary),
			grpc.WithChainStreamInterceptor(stream),
		)
	}

	conn, err := grpc.NewClient(target, opts...)
	if err != nil {
		return nil, fmt.Errorf("remote: dial %s: %w", target, err)
	}
	return conn, nil
}

// NewClientSet dials the remote public gestaltd gRPC surface and returns typed clients.
func NewClientSet(ctx context.Context, cfg Config) (*ClientSet, error) {
	if strings.TrimSpace(cfg.Token) == "" {
		return nil, fmt.Errorf("remote: token is required")
	}
	conn, err := Dial(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return clientSetFromConn(conn), nil
}

// Close releases the underlying gRPC connection.
func (c *ClientSet) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	conn := c.conn
	c.conn = nil
	return conn.Close()
}

func clientSetFromConn(conn *grpc.ClientConn) *ClientSet {
	return &ClientSet{
		App:           proto.NewAppClient(conn),
		Agent:         proto.NewAgentClient(conn),
		Workflow:      proto.NewWorkflowClient(conn),
		IndexedDB:     proto.NewIndexedDBClient(conn),
		Identity:      proto.NewIdentityClient(conn),
		Authorization: proto.NewAuthorizationClient(conn),
		conn:          conn,
	}
}

func grpcTarget(rawURL string, tlsConfig *tls.Config) (target string, creds credentials.TransportCredentials, err error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", nil, fmt.Errorf("remote: parse URL %q: %w", rawURL, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", nil, fmt.Errorf("remote: URL %q must include scheme and host", rawURL)
	}

	host := parsed.Hostname()
	port := parsed.Port()
	switch parsed.Scheme {
	case "https":
		if port == "" {
			port = "443"
		}
		if tlsConfig == nil {
			tlsConfig = &tls.Config{
				MinVersion: tls.VersionTLS12,
				ServerName: host,
			}
		}
		creds = credentials.NewTLS(tlsConfig)
	case "http":
		if port == "" {
			port = "80"
		}
		creds = insecure.NewCredentials()
	default:
		return "", nil, fmt.Errorf("remote: unsupported URL scheme %q", parsed.Scheme)
	}
	return net.JoinHostPort(host, port), creds, nil
}

func bearerTokenInterceptors(token string) (grpc.UnaryClientInterceptor, grpc.StreamClientInterceptor) {
	withBearer := func(ctx context.Context) context.Context {
		token = strings.TrimSpace(token)
		if token == "" {
			return ctx
		}
		return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
	}
	unary := func(
		ctx context.Context,
		method string,
		req, reply any,
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		return invoker(withBearer(ctx), method, req, reply, cc, opts...)
	}
	stream := func(
		ctx context.Context,
		desc *grpc.StreamDesc,
		cc *grpc.ClientConn,
		method string,
		streamer grpc.Streamer,
		opts ...grpc.CallOption,
	) (grpc.ClientStream, error) {
		return streamer(withBearer(ctx), desc, cc, method, opts...)
	}
	return unary, stream
}
