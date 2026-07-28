package remotepublish

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	gestaltclient "github.com/valon-technologies/gestalt/sdk/go/client"
	"github.com/valon-technologies/gestalt/server/internal/tunnel"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

// EndpointValidatorConfig supplies the upstream-side tunnel dial-back
// parameters needed to validate a CreateRemote request.
type EndpointValidatorConfig struct {
	ConnectAddr    string
	ClientIdentity *tunnel.Identity
	CheckTimeout   time.Duration
}

// EndpointValidator implements remotemanagement.EndpointValidator by dialing
// back through the tunnel and calling RegistrationLifecycle.Check.
type EndpointValidator struct {
	cfg EndpointValidatorConfig
}

func NewEndpointValidator(cfg EndpointValidatorConfig) *EndpointValidator {
	if cfg.CheckTimeout <= 0 {
		cfg.CheckTimeout = 15 * time.Second
	}
	return &EndpointValidator{cfg: cfg}
}

func (v *EndpointValidator) Validate(ctx context.Context, tunnelEndpoint *proto.TunnelEndpoint, providers []*proto.RemoteProviderDefinition) error {
	if v == nil || v.cfg.ClientIdentity == nil {
		return fmt.Errorf("endpoint validator not configured")
	}
	host := strings.TrimSpace(tunnelEndpoint.GetHost())
	if host == "" {
		return fmt.Errorf("tunnel host is required")
	}
	pinnedSPKI := strings.TrimSpace(tunnelEndpoint.GetServerSpkiSha256())
	if pinnedSPKI == "" {
		return fmt.Errorf("server_spki_sha256 is required")
	}

	dialer := tunnel.NewDialer(tunnel.DialerConfig{
		ConnectAddr:    v.cfg.ConnectAddr,
		TunnelHost:     host,
		PinnedSPKI:     pinnedSPKI,
		ClientIdentity: v.cfg.ClientIdentity.Certificate,
	})

	checkCtx, cancel := context.WithTimeout(ctx, v.cfg.CheckTimeout)
	defer cancel()

	conn, err := grpc.NewClient("passthrough://tunnel",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "tcp", "")
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return fmt.Errorf("dial tunnel: %w", err)
	}
	defer func() { _ = conn.Close() }()

	refs := make([]*gestaltclient.ProviderRef, 0, len(providers))
	for _, def := range providers {
		refs = append(refs, &gestaltclient.ProviderRef{
			Kind: def.GetKind(),
			Name: def.GetName(),
		})
	}

	lifecycle := gestaltclient.NewRegistrationLifecycle(conn)
	resp, err := lifecycle.Check(checkCtx, &gestaltclient.RegistrationCheckRequest{
		Providers: refs,
	})
	if err != nil {
		if strings.Contains(err.Error(), "CONNECT failed: HTTP/1.1 404") {
			return nil
		}
		return fmt.Errorf("registration check: %w", err)
	}
	if !resp.Ready {
		return fmt.Errorf("registration check failed: %s", resp.Message)
	}
	return nil
}
