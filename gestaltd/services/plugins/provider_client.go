package plugins

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	plugininvokerservice "github.com/valon-technologies/gestalt/server/services/plugininvoker"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type StaticProviderSpec struct {
	Name               string
	DisplayName        string
	Description        string
	IconSVG            string
	ConnectionMode     core.ConnectionMode
	Catalog            *catalog.Catalog
	AuthTypes          []string
	ConnectionParams   map[string]core.ConnectionParamDef
	CredentialFields   []core.CredentialFieldDef
	DiscoveryConfig    *core.DiscoveryConfig
	PostConnectConfigs map[string]*core.PostConnectConfig
}

type remoteProviderBase struct {
	client        proto.IntegrationProviderClient
	support       integrationProviderSupport
	name          string
	displayName   string
	description   string
	connection    core.ConnectionMode
	catalog       *catalog.Catalog
	iconSVG       string
	authTypes     []string
	connParams    map[string]core.ConnectionParamDef
	credFields    []core.CredentialFieldDef
	discovery     *core.DiscoveryConfig
	closer        io.Closer
	publicBaseURL string
	invTokens     *plugininvokerservice.InvocationTokenManager
	callerPlugin  string
	invokeGrants  plugininvokerservice.InvocationGrants
}

var (
	_ core.SessionCatalogProvider = (*remoteProviderBase)(nil)
	_ core.PostConnectCapable     = (*remoteProviderBase)(nil)
)

type integrationProviderSupport struct {
	sessionCatalog bool
	postConnect    bool
}

// RemoteProviderOption configures a remote provider returned by NewRemoteProvider.
type RemoteProviderOption func(*remoteProviderBase)

// WithCloser attaches a closer that is called when the provider is closed.
// This is used to tie process lifecycle to provider lifecycle.
func WithCloser(c io.Closer) RemoteProviderOption {
	return func(b *remoteProviderBase) { b.closer = c }
}

func WithHostContext(publicBaseURL string) RemoteProviderOption {
	return func(b *remoteProviderBase) { b.publicBaseURL = normalizePublicBaseURL(publicBaseURL) }
}

func WithInvocationTokens(tokens *plugininvokerservice.InvocationTokenManager) RemoteProviderOption {
	return func(b *remoteProviderBase) { b.invTokens = tokens }
}

func WithInvocationTokenSubject(pluginName string, grants plugininvokerservice.InvocationGrants) RemoteProviderOption {
	return func(b *remoteProviderBase) {
		b.callerPlugin = strings.TrimSpace(pluginName)
		b.invokeGrants = plugininvokerservice.CloneInvocationGrants(grants)
	}
}

func NewRemote(ctx context.Context, client proto.IntegrationProviderClient, spec StaticProviderSpec, config map[string]any, opts ...RemoteProviderOption) (core.Provider, error) {
	support, err := getIntegrationProviderSupportWithRetry(ctx, client)
	if err != nil {
		return nil, err
	}
	if err := callStartProvider(ctx, client, spec.Name, config); err != nil {
		return nil, err
	}

	base := &remoteProviderBase{
		client:      client,
		support:     *support,
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

	return base, nil
}

func NewRemoteProvider(ctx context.Context, client proto.IntegrationProviderClient, spec StaticProviderSpec, config map[string]any, opts ...RemoteProviderOption) (core.Provider, error) {
	return NewRemote(ctx, client, spec, config, opts...)
}

func getIntegrationProviderSupportWithRetry(ctx context.Context, client proto.IntegrationProviderClient) (*integrationProviderSupport, error) {
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()

	for {
		meta, err := client.GetMetadata(ctx, &emptypb.Empty{})
		if err == nil {
			return &integrationProviderSupport{
				sessionCatalog: meta.GetSupportsSessionCatalog(),
				postConnect:    meta.GetSupportsPostConnect(),
			}, nil
		}
		if status.Code(err) == codes.Unimplemented {
			return &integrationProviderSupport{}, nil
		}
		if status.Code(err) != codes.Unavailable {
			return nil, fmt.Errorf("get provider metadata: %w", err)
		}

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("get provider metadata: %w", err)
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
	requestToken := ""
	if p != nil && p.invTokens != nil && p.callerPlugin != "" {
		requestToken, err = p.invTokens.MintRootToken(ctx, p.callerPlugin, p.invokeGrants)
		if err != nil {
			return nil, err
		}
	}
	reqCtx, err := p.requestContextProto(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Execute(ctx, &proto.ExecuteRequest{
		Operation:        operation,
		Params:           msg,
		Token:            token,
		ConnectionParams: core.ConnectionParams(ctx),
		InvocationId:     invocationIDFromContext(ctx),
		InvocationToken:  requestToken,
		IdempotencyKey:   invocation.IdempotencyKeyFromContext(ctx),
		Context:          reqCtx,
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

func (p *remoteProviderBase) SupportsSessionCatalog() bool {
	return p != nil && p.support.sessionCatalog
}

func (p *remoteProviderBase) CatalogForRequest(ctx context.Context, token string) (*catalog.Catalog, error) {
	if !p.SupportsSessionCatalog() {
		return nil, core.WrapSessionCatalogUnsupported(core.ErrSessionCatalogUnsupported)
	}
	return p.sessionCatalog(ctx, token)
}

func (p *remoteProviderBase) sessionCatalog(ctx context.Context, token string) (*catalog.Catalog, error) {
	reqCtx, err := p.requestContextProto(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.GetSessionCatalog(ctx, &proto.GetSessionCatalogRequest{
		Token:            token,
		ConnectionParams: core.ConnectionParams(ctx),
		InvocationId:     invocationIDFromContext(ctx),
		Context:          reqCtx,
	})
	if err != nil {
		return nil, err
	}
	cat, err := catalogFromProto(resp.GetCatalog())
	if err != nil {
		return nil, err
	}
	return p.decorateCatalog(cat), nil
}

func (p *remoteProviderBase) SupportsPostConnect() bool {
	return p != nil && p.support.postConnect
}

func (p *remoteProviderBase) PostConnect(ctx context.Context, token *core.ExternalCredential) (map[string]string, error) {
	if !p.SupportsPostConnect() {
		return nil, core.ErrPostConnectUnsupported
	}
	return p.postConnect(ctx, token)
}

func (p *remoteProviderBase) postConnect(ctx context.Context, token *core.ExternalCredential) (map[string]string, error) {
	resp, err := p.client.PostConnect(ctx, &proto.PostConnectRequest{
		Token: postConnectCredentialToProto(token),
	})
	if err != nil {
		if status.Code(err) == codes.Unimplemented {
			return nil, core.ErrPostConnectUnsupported
		}
		return nil, err
	}
	metadata := resp.GetMetadata()
	if len(metadata) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(metadata))
	for key, value := range metadata {
		out[key] = value
	}
	return out, nil
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

func (p *remoteProviderBase) requestContextProto(ctx context.Context) (*proto.RequestContext, error) {
	if p == nil {
		return requestContextProto(ctx, "")
	}
	return requestContextProto(ctx, p.publicBaseURL)
}

func requestContextProto(ctx context.Context, publicBaseURL string) (*proto.RequestContext, error) {
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
	if workflow := invocation.WorkflowContextFromContext(ctx); workflow != nil {
		value, err := structFromMap(workflow)
		if err != nil {
			return nil, fmt.Errorf("workflow request context: %w", err)
		}
		out.Workflow = value
	}

	if publicBaseURL = normalizePublicBaseURL(publicBaseURL); publicBaseURL == "" {
		publicBaseURL = normalizePublicBaseURL(invocation.HostContextFromContext(ctx).PublicBaseURL)
	}
	if publicBaseURL != "" {
		out.Host = &proto.HostContext{PublicBaseUrl: publicBaseURL}
	}
	if audit := invocation.RunAsAuditFromContext(ctx); audit.AgentSubject != nil {
		out.AgentSubject = runAsSubjectContextProto(audit.AgentSubject)
	}
	if identity := invocation.AgentExternalIdentityContextFromContext(ctx); identity.Type != "" && identity.ID != "" {
		out.AgentExternalIdentity = &proto.ExternalIdentityContext{
			Type: identity.Type,
			Id:   identity.ID,
		}
	}
	if identity := invocation.ExternalIdentityContextFromContext(ctx); identity.Type != "" && identity.ID != "" {
		out.ExternalIdentity = &proto.ExternalIdentityContext{
			Type: identity.Type,
			Id:   identity.ID,
		}
	}

	if out.Subject == nil && out.Credential == nil && out.Access == nil && out.Workflow == nil && out.Host == nil && out.AgentSubject == nil && out.AgentExternalIdentity == nil && out.ExternalIdentity == nil {
		return nil, nil
	}
	return &out, nil
}

func normalizePublicBaseURL(baseURL string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/")
}

func postConnectCredentialToProto(token *core.ExternalCredential) *proto.PostConnectCredential {
	if token == nil {
		return nil
	}
	out := &proto.PostConnectCredential{
		Id:                token.ID,
		SubjectId:         token.SubjectID,
		Integration:       token.Integration,
		Connection:        token.Connection,
		Instance:          token.Instance,
		AccessToken:       token.AccessToken,
		RefreshToken:      token.RefreshToken,
		Scopes:            token.Scopes,
		RefreshErrorCount: int32(token.RefreshErrorCount),
		MetadataJson:      token.MetadataJSON,
	}
	if token.ExpiresAt != nil {
		out.ExpiresAt = timestamppb.New(*token.ExpiresAt)
	}
	if token.LastRefreshedAt != nil {
		out.LastRefreshedAt = timestamppb.New(*token.LastRefreshedAt)
	}
	if !token.CreatedAt.IsZero() {
		out.CreatedAt = timestamppb.New(token.CreatedAt)
	}
	if !token.UpdatedAt.IsZero() {
		out.UpdatedAt = timestamppb.New(token.UpdatedAt)
	}
	return out
}

func subjectIDForPrincipal(p *principal.Principal) string {
	p = principal.Canonicalized(p)
	if p == nil {
		return ""
	}
	return p.SubjectID
}

func subjectKindForPrincipal(p *principal.Principal) string {
	p = principal.Canonicalized(p)
	if p == nil {
		return ""
	}
	if p.Kind != "" {
		return string(p.Kind)
	}
	if p.Identity != nil {
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

func runAsSubjectContextProto(subject *core.RunAsSubject) *proto.SubjectContext {
	subject = core.NormalizeRunAsSubject(subject)
	if subject == nil {
		return nil
	}
	return &proto.SubjectContext{
		Id:          subject.SubjectID,
		Kind:        subject.SubjectKind,
		DisplayName: subject.DisplayName,
		AuthSource:  subject.AuthSource,
	}
}
