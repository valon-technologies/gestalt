package providerhost

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coreintegration "github.com/valon-technologies/gestalt/server/core/integration"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type StaticProviderSpec struct {
	Name             string
	DisplayName      string
	Description      string
	IconSVG          string
	ConnectionMode   core.ConnectionMode
	Catalog          *catalog.Catalog
	AuthTypes        []string
	ConnectionParams map[string]core.ConnectionParamDef
	CredentialFields []core.CredentialFieldDef
	DiscoveryConfig  *core.DiscoveryConfig
}

type remoteProviderBase struct {
	core.NoOAuth
	client      proto.IntegrationProviderClient
	name        string
	displayName string
	description string
	connection  core.ConnectionMode
	catalog     *catalog.Catalog
	iconSVG     string
	authTypes   []string
	connParams  map[string]core.ConnectionParamDef
	credFields  []core.CredentialFieldDef
	discovery   *core.DiscoveryConfig
	closer      io.Closer
}

// RemoteProviderOption configures a remote provider returned by NewRemoteProvider.
type RemoteProviderOption func(*remoteProviderBase)

// WithCloser attaches a closer that is called when the provider is closed.
// This is used to tie process lifecycle to provider lifecycle.
func WithCloser(c io.Closer) RemoteProviderOption {
	return func(b *remoteProviderBase) { b.closer = c }
}

func NewRemoteProvider(ctx context.Context, client proto.IntegrationProviderClient, spec StaticProviderSpec, config map[string]any, opts ...RemoteProviderOption) (core.Provider, error) {
	supportsSessionCatalog, err := getSessionCatalogSupportWithRetry(ctx, client)
	if err != nil {
		return nil, err
	}
	if err := callStartProvider(ctx, client, spec.Name, config); err != nil {
		return nil, err
	}

	base := &remoteProviderBase{
		client:      client,
		name:        spec.Name,
		displayName: spec.DisplayName,
		description: spec.Description,
		connection:  spec.ConnectionMode,
		catalog:     spec.Catalog,
		iconSVG:     spec.IconSVG,
		authTypes:   spec.AuthTypes,
		connParams:  spec.ConnectionParams,
		credFields:  spec.CredentialFields,
		discovery:   spec.DiscoveryConfig,
	}
	for _, opt := range opts {
		opt(base)
	}

	if supportsSessionCatalog {
		return &remoteProviderWithSessionCatalog{remoteProviderBase: base}, nil
	}
	return base, nil
}

func getSessionCatalogSupportWithRetry(ctx context.Context, client proto.IntegrationProviderClient) (bool, error) {
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()

	for {
		meta, err := client.GetMetadata(ctx, &emptypb.Empty{})
		if err == nil {
			return meta.GetSupportsSessionCatalog(), nil
		}
		if status.Code(err) == codes.Unimplemented {
			return false, nil
		}
		if status.Code(err) != codes.Unavailable {
			return false, err
		}

		select {
		case <-ctx.Done():
			return false, err
		case <-ticker.C:
		}
	}
}

func (p *remoteProviderBase) Close() error {
	if p.closer != nil {
		return p.closer.Close()
	}
	return nil
}

func (p *remoteProviderBase) Name() string        { return p.name }
func (p *remoteProviderBase) DisplayName() string { return p.displayName }
func (p *remoteProviderBase) Description() string { return p.description }

func (p *remoteProviderBase) ConnectionMode() core.ConnectionMode {
	if p.connection == "" {
		return core.ConnectionModeUser
	}
	return p.connection
}

func (p *remoteProviderBase) Execute(ctx context.Context, operation string, params map[string]any, token string) (*core.OperationResult, error) {
	msg, err := structFromMap(params)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Execute(ctx, &proto.ExecuteRequest{
		Operation:        operation,
		Params:           msg,
		Token:            token,
		ConnectionParams: core.ConnectionParams(ctx),
		InvocationId:     invocationIDFromContext(ctx),
		Context:          requestContextProto(ctx),
	})
	if err != nil {
		return nil, err
	}
	return &core.OperationResult{
		Status: int(resp.GetStatus()),
		Body:   resp.GetBody(),
	}, nil
}

func (p *remoteProviderBase) Catalog() *catalog.Catalog {
	return p.decorateCatalog(p.catalog)
}

func (p *remoteProviderBase) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return p.connParams
}

func (p *remoteProviderBase) AuthTypes() []string {
	return p.authTypes
}

func (p *remoteProviderBase) CredentialFields() []core.CredentialFieldDef {
	return p.credFields
}

func (p *remoteProviderBase) DiscoveryConfig() *core.DiscoveryConfig {
	return p.discovery
}

func (p *remoteProviderBase) ConnectionForOperation(string) string { return "" }

func (p *remoteProviderBase) sessionCatalog(ctx context.Context, token string) (*catalog.Catalog, error) {
	resp, err := p.client.GetSessionCatalog(ctx, &proto.GetSessionCatalogRequest{
		Token:            token,
		ConnectionParams: core.ConnectionParams(ctx),
		InvocationId:     invocationIDFromContext(ctx),
		Context:          requestContextProto(ctx),
	})
	if err != nil {
		return nil, err
	}
	cat, err := catalogFromProto(resp.GetCatalog())
	if err != nil {
		return nil, err
	}
	cat = core.HydrateSessionCatalog(p.Catalog(), cat)
	coreintegration.CompileSchemas(cat)
	return cat, nil
}

type remoteProviderWithSessionCatalog struct{ *remoteProviderBase }

func (p *remoteProviderWithSessionCatalog) CatalogForRequest(ctx context.Context, token string) (*catalog.Catalog, error) {
	return p.sessionCatalog(ctx, token)
}

func (p *remoteProviderBase) decorateCatalog(cat *catalog.Catalog) *catalog.Catalog {
	if cat == nil {
		return nil
	}
	decorated := cat.Clone()
	if decorated.Name == "" {
		decorated.Name = p.name
	}
	if decorated.DisplayName == "" {
		decorated.DisplayName = p.displayName
	}
	if decorated.Description == "" {
		decorated.Description = p.description
	}
	if p.iconSVG != "" {
		decorated.IconSVG = p.iconSVG
	}
	for i := range decorated.Operations {
		if decorated.Operations[i].Transport == "" {
			decorated.Operations[i].Transport = catalog.TransportPlugin
		}
	}
	return decorated
}

func callStartProvider(ctx context.Context, client proto.IntegrationProviderClient, name string, config map[string]any) error {
	cfgStruct, err := structFromMap(config)
	if err != nil {
		return fmt.Errorf("encode provider config: %w", err)
	}
	resp, err := client.StartProvider(ctx, &proto.StartProviderRequest{
		Name:            name,
		Config:          cfgStruct,
		ProtocolVersion: proto.CurrentProtocolVersion,
	})
	if err != nil {
		if status.Code(err) == codes.Unimplemented {
			return nil
		}
		return fmt.Errorf("start provider: %w", err)
	}
	if v := resp.GetProtocolVersion(); v != proto.CurrentProtocolVersion {
		return fmt.Errorf("provider responded with protocol version %d, host requires %d",
			v, proto.CurrentProtocolVersion)
	}
	return nil
}

func invocationIDFromContext(ctx context.Context) string {
	meta := invocation.MetaFromContext(ctx)
	if meta == nil {
		return ""
	}
	return meta.RequestID
}

func requestContextProto(ctx context.Context) *proto.RequestContext {
	var out proto.RequestContext

	if p := principal.FromContext(ctx); p != nil {
		out.Subject = &proto.SubjectContext{
			Id:          subjectIDForPrincipal(p),
			Kind:        subjectKindForPrincipal(p),
			DisplayName: subjectDisplayName(p),
			AuthSource:  p.AuthSource(),
		}
	}

	if cred := invocation.CredentialContextFromContext(ctx); cred.Mode != "" || cred.SubjectID != "" || cred.Connection != "" || cred.Instance != "" {
		out.Credential = &proto.CredentialContext{
			Mode:       string(cred.Mode),
			SubjectId:  cred.SubjectID,
			Connection: cred.Connection,
			Instance:   cred.Instance,
		}
	}

	if access := invocation.AccessContextFromContext(ctx); access.Policy != "" || access.Role != "" {
		out.Access = &proto.AccessContext{
			Policy: access.Policy,
			Role:   access.Role,
		}
	}

	if out.Subject == nil && out.Credential == nil && out.Access == nil {
		return nil
	}
	return &out
}

func subjectIDForPrincipal(p *principal.Principal) string {
	if p == nil {
		return ""
	}
	if p.SubjectID != "" {
		return p.SubjectID
	}
	if p.UserID != "" {
		return principal.UserSubjectID(p.UserID)
	}
	return ""
}

func subjectKindForPrincipal(p *principal.Principal) string {
	if p == nil {
		return ""
	}
	if p.Kind != "" {
		return string(p.Kind)
	}
	switch {
	case strings.HasPrefix(p.SubjectID, string(principal.KindUser)+":"):
		return string(principal.KindUser)
	case strings.HasPrefix(p.SubjectID, string(principal.KindWorkload)+":"):
		return string(principal.KindWorkload)
	}
	if p.UserID != "" || p.Identity != nil {
		return string(principal.KindUser)
	}
	return ""
}

func subjectDisplayName(p *principal.Principal) string {
	if p == nil {
		return ""
	}
	if p.DisplayName != "" {
		return p.DisplayName
	}
	if p.Identity == nil {
		return ""
	}
	return p.Identity.DisplayName
}
