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

// Client exposes generated public service clients over one transport.
type Client struct {
	App           *generated.AppClient
	Agent         *generated.AgentClient
	Authorization *generated.AuthorizationClient
	Identity      *generated.IdentityClient
	Workflow      *generated.WorkflowClient

	externalCredentials *generated.ExternalCredentialsClient
	indexedDB           *generated.IndexedDBClient

	transport interface {
		Close() error
	}
}

// ExternalCredentials returns the gRPC-only ExternalCredentials client when available.
func (c *Client) ExternalCredentials() (*generated.ExternalCredentialsClient, bool) {
	if c == nil || c.externalCredentials == nil {
		return nil, false
	}
	return c.externalCredentials, true
}

// IndexedDB returns the gRPC-only IndexedDB client when available.
func (c *Client) IndexedDB() (*generated.IndexedDBClient, bool) {
	if c == nil || c.indexedDB == nil {
		return nil, false
	}
	return c.indexedDB, true
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
		return newRESTClient(rest), nil
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
		return newGRPCClient(grpcT), nil
	default:
		return nil, fmt.Errorf("publicclient: unsupported transport %T", transport)
	}
}

func newRESTClient(rest *restUnaryTransport) *Client {
	return &Client{
		App:           generated.NewAppClient(rest),
		Agent:         generated.NewAgentClient(rest),
		Authorization: generated.NewAuthorizationClient(rest),
		Identity:      generated.NewIdentityClient(rest),
		Workflow:      generated.NewWorkflowClient(rest),
		transport:     rest,
	}
}

func newGRPCClient(grpcT *grpcUnaryTransport) *Client {
	return &Client{
		App:                 generated.NewAppClient(grpcT),
		Agent:               generated.NewAgentClient(grpcT),
		Authorization:       generated.NewAuthorizationClient(grpcT),
		Identity:            generated.NewIdentityClient(grpcT),
		Workflow:            generated.NewWorkflowClient(grpcT),
		externalCredentials: generated.NewExternalCredentialsClient(grpcT),
		indexedDB:           generated.NewIndexedDBClient(grpcT),
		transport:           grpcT,
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
	return newRESTClient(rest)
}
