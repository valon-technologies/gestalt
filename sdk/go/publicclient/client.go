package publicclient

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/valon-technologies/gestalt/sdk/go/publicclient/generated"
)

// Transport selects the public Gestalt wire protocol.
type Transport interface {
	isTransport()
}

type restTransport struct{}

// REST selects the browser-safe /api/v2 protobuf JSON surface.
func REST() Transport { return restTransport{} }

func (restTransport) isTransport() {}

type grpcTransport struct{}

// GRPC selects the public provider gRPC surface.
func GRPC() Transport { return grpcTransport{} }

func (grpcTransport) isTransport() {}

// Options configures New.
type Options struct {
	Address   string
	Transport Transport
	Auth      Auth
}

// Client exposes the generated public App client over one transport.
type Client struct {
	App *generated.AppClient

	transport interface {
		Close() error
	}
}

// New builds a public client for REST or external gRPC.
func New(opts Options) (*Client, error) {
	transport := opts.Transport
	if transport == nil {
		transport = REST()
	}

	switch transport.(type) {
	case restTransport:
		baseURL, err := normalizeAddress(opts.Address)
		if err != nil {
			return nil, err
		}
		rest := &restUnaryTransport{baseURL: baseURL, auth: opts.Auth, client: http.DefaultClient}
		return &Client{
			App:       generated.NewAppClient(rest),
			transport: rest,
		}, nil
	case grpcTransport:
		address := strings.TrimSpace(opts.Address)
		if address == "" {
			return nil, fmt.Errorf("publicclient: address is required for external gRPC (use GestaltFromContext for bound provider access)")
		}
		conn, err := dialPublicGRPC(address)
		if err != nil {
			return nil, err
		}
		grpcT := &grpcUnaryTransport{conn: conn, owned: conn, auth: opts.Auth}
		return &Client{
			App:       generated.NewAppClient(grpcT),
			transport: grpcT,
		}, nil
	default:
		return nil, fmt.Errorf("publicclient: unsupported transport %T", transport)
	}
}

// Close releases transport resources when the client owns them.
func (c *Client) Close() error {
	if c == nil || c.transport == nil {
		return nil
	}
	return c.transport.Close()
}

// NewRESTClientForTest constructs a REST client with a custom http.Client.
func NewRESTClientForTest(baseURL string, auth Auth, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	rest := &restUnaryTransport{baseURL: baseURL, auth: auth, client: httpClient}
	return &Client{
		App:       generated.NewAppClient(rest),
		transport: rest,
	}
}
