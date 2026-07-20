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

// Options configures New for the App-only compatibility client.
type Options struct {
	Address   string
	Transport Transport
	Auth      Auth
}

// AddressOptions configures NewREST and NewGRPC.
type AddressOptions struct {
	Address string
	Auth    Auth
}

// Client exposes the generated public App client over one transport.
type Client struct {
	App *generated.AppClient

	transport interface {
		Close() error
	}
}

// RestClient exposes the five REST-capable public service clients.
type RestClient struct {
	App           generated.AppClientREST
	Agent         generated.AgentClientREST
	Workflow      generated.WorkflowClientREST
	Identity      generated.IdentityClientREST
	Authorization generated.AuthorizationClientREST

	transport interface {
		Close() error
	}
}

// GrpcClient exposes all seven public service clients over gRPC.
type GrpcClient struct {
	App           *generated.AppClient
	Agent         *generated.AgentClient
	Workflow      *generated.WorkflowClient
	Identity      *generated.IdentityClient
	Authorization *generated.AuthorizationClient
	IndexedDB     *generated.IndexedDBClient
	ExternalCreds *generated.ExternalCredentialsClient

	transport interface {
		Close() error
	}
}

// BoundClient exposes the deliberate host-service App relay surface.
type BoundClient struct {
	App *generated.AppClient

	transport interface {
		Close() error
	}
}

// New builds a public App-only client for REST or external gRPC.
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

// NewREST builds a REST public client.
func NewREST(opts AddressOptions) (*RestClient, error) {
	baseURL, err := normalizeAddress(opts.Address)
	if err != nil {
		return nil, err
	}
	rest := &restUnaryTransport{baseURL: baseURL, auth: opts.Auth, client: http.DefaultClient}
	return bindRestClient(rest, rest), nil
}

// NewGRPC builds an external gRPC public client.
func NewGRPC(opts AddressOptions) (*GrpcClient, error) {
	address := strings.TrimSpace(opts.Address)
	if address == "" {
		return nil, fmt.Errorf("publicclient: address is required for external gRPC (use GestaltFromContext for bound provider access)")
	}
	conn, err := dialPublicGRPC(address)
	if err != nil {
		return nil, err
	}
	grpcT := &grpcUnaryTransport{conn: conn, owned: conn, auth: opts.Auth}
	return bindGrpcClient(grpcT, grpcT), nil
}

func bindRestClient(transport generated.Transport, closer interface{ Close() error }) *RestClient {
	return &RestClient{
		App:           generated.NewAppClient(transport),
		Agent:         generated.NewAgentClient(transport),
		Workflow:      generated.NewWorkflowClient(transport),
		Identity:      generated.NewIdentityClient(transport),
		Authorization: generated.NewAuthorizationClient(transport),
		transport:     closer,
	}
}

func bindGrpcClient(transport generated.Transport, closer interface{ Close() error }) *GrpcClient {
	return &GrpcClient{
		App:           generated.NewAppClient(transport),
		Agent:         generated.NewAgentClient(transport),
		Workflow:      generated.NewWorkflowClient(transport),
		Identity:      generated.NewIdentityClient(transport),
		Authorization: generated.NewAuthorizationClient(transport),
		IndexedDB:     generated.NewIndexedDBClient(transport),
		ExternalCreds: generated.NewExternalCredentialsClient(transport),
		transport:     closer,
	}
}

func bindBoundClient(transport generated.Transport, closer interface{ Close() error }) *BoundClient {
	return &BoundClient{
		App:       generated.NewAppClient(transport),
		transport: closer,
	}
}

// Close releases transport resources when the client owns them.
func (c *Client) Close() error {
	if c == nil || c.transport == nil {
		return nil
	}
	return c.transport.Close()
}

// Close releases transport resources when the client owns them.
func (c *RestClient) Close() error {
	if c == nil || c.transport == nil {
		return nil
	}
	return c.transport.Close()
}

// Close releases transport resources when the client owns them.
func (c *GrpcClient) Close() error {
	if c == nil || c.transport == nil {
		return nil
	}
	return c.transport.Close()
}

// Close is a no-op for bound host-service clients.
func (c *BoundClient) Close() error {
	if c == nil || c.transport == nil {
		return nil
	}
	return c.transport.Close()
}

// NewRESTClientForTest constructs a REST multi-service client with a custom http.Client.
func NewRESTClientForTest(baseURL string, auth Auth, httpClient *http.Client) *RestClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	rest := &restUnaryTransport{baseURL: baseURL, auth: auth, client: httpClient}
	return bindRestClient(rest, rest)
}
