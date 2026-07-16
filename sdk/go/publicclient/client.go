package publicclient

import (
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/sdk/go/publicclient/generated"
	"google.golang.org/grpc"
)

// Transport selects the public Gestalt wire protocol.
type Transport string

const (
	// TransportREST uses the browser-safe /api/v2 JSON surface.
	TransportREST Transport = "rest"
	// TransportGRPC uses the public provider gRPC surface.
	TransportGRPC Transport = "grpc"
)

// ClientOptions configures CreateGestaltClient.
type ClientOptions struct {
	Address   string
	Transport Transport
	Auth      Auth
}

// Client exposes generated public service clients for one transport.
type Client struct {
	REST *RESTClients
	GRPC *GRPCClients

	closeConn *grpc.ClientConn
}

// RESTClients groups REST service clients backed by one transport.
type RESTClients struct {
	Transport *RESTTransport

	App           *generated.AppREST
	Agent         *generated.AgentREST
	Workflow      *generated.WorkflowREST
	Identity      *generated.IdentityREST
	Authorization *generated.AuthorizationREST
}

// GRPCClients groups gRPC service clients backed by one connection.
type GRPCClients struct {
	Transport *GRPCTransport

	App                 *generated.AppGRPC
	Agent               *generated.AgentGRPC
	Workflow            *generated.WorkflowGRPC
	Identity            *generated.IdentityGRPC
	Authorization       *generated.AuthorizationGRPC
	IndexedDB           *generated.IndexedDBGRPC
	ExternalCredentials *generated.ExternalCredentialsGRPC
}

// CreateGestaltClient builds a public client for REST or gRPC.
func CreateGestaltClient(opts ClientOptions) (*Client, error) {
	if opts.Transport == "" {
		opts.Transport = TransportREST
	}
	switch opts.Transport {
	case TransportREST:
		baseURL, err := normalizeAddress(opts.Address)
		if err != nil {
			return nil, err
		}
		return NewRESTClient(baseURL, opts.Auth), nil
	case TransportGRPC:
		address := strings.TrimSpace(opts.Address)
		if address == "" {
			return nil, fmt.Errorf("publicclient: address is required for external gRPC (use GestaltFromContext for bound provider access)")
		}
		conn, err := dialPublicGRPC(address)
		if err != nil {
			return nil, err
		}
		client := NewGRPCClient(conn, opts.Auth)
		client.closeConn = conn
		return client, nil
	default:
		return nil, fmt.Errorf("unsupported transport %q", opts.Transport)
	}
}

// NewRESTClient creates a Client whose REST field is populated.
func NewRESTClient(baseURL string, auth Auth) *Client {
	transport := &RESTTransport{BaseURL: baseURL, Auth: auth}
	return &Client{REST: newRESTClients(transport)}
}

// NewGRPCClient creates a Client whose GRPC field is populated.
func NewGRPCClient(conn grpc.ClientConnInterface, auth Auth) *Client {
	if auth != nil {
		conn = &authClientConn{Conn: conn, Auth: auth}
	}
	transport := &GRPCTransport{Conn: conn, Auth: auth}
	return &Client{GRPC: newGRPCClients(transport)}
}

func newRESTClients(transport *RESTTransport) *RESTClients {
	return &RESTClients{
		Transport:     transport,
		App:           generated.NewAppREST(transport),
		Agent:         generated.NewAgentREST(transport),
		Workflow:      generated.NewWorkflowREST(transport),
		Identity:      generated.NewIdentityREST(transport),
		Authorization: generated.NewAuthorizationREST(transport),
	}
}

func newGRPCClients(transport *GRPCTransport) *GRPCClients {
	return &GRPCClients{
		Transport:           transport,
		App:                 generated.NewAppGRPC(transport.Conn),
		Agent:               generated.NewAgentGRPC(transport.Conn),
		Workflow:            generated.NewWorkflowGRPC(transport.Conn),
		Identity:            generated.NewIdentityGRPC(transport.Conn),
		Authorization:       generated.NewAuthorizationGRPC(transport.Conn),
		IndexedDB:           generated.NewIndexedDBGRPC(transport.Conn),
		ExternalCredentials: generated.NewExternalCredentialsGRPC(transport.Conn),
	}
}

func grpcTargetFromAddress(address string) string {
	trimmed := strings.TrimPrefix(address, "https://")
	trimmed = strings.TrimPrefix(trimmed, "http://")
	return trimmed
}

// Close releases transport resources when the client owns a gRPC connection.
func (c *Client) Close() error {
	if c == nil || c.closeConn == nil {
		return nil
	}
	err := c.closeConn.Close()
	c.closeConn = nil
	return err
}

// GestaltFromContext is defined in bound.go.
