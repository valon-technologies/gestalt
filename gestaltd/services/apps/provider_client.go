package plugins

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/agentwire"
	"github.com/valon-technologies/gestalt/server/internal/protoutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	appaccessservice "github.com/valon-technologies/gestalt/server/services/appaccess"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowgrants"
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
	client         proto.AppProviderClient
	support        integrationProviderSupport
	name           string
	displayName    string
	description    string
	connection     core.ConnectionMode
	catalog        *catalog.Catalog
	iconSVG        string
	authTypes      []string
	connParams     map[string]core.ConnectionParamDef
	credFields     []core.CredentialFieldDef
	discovery      *core.DiscoveryConfig
	closer         io.Closer
	publicBaseURL  string
	invTokens      *appaccessservice.InvocationTokenManager
	callerApp      string
	invokeGrants   appaccessservice.InvocationGrants
	workflowGrants workflowgrants.Grants
}

var (
	_ core.SessionCatalogProvider = (*remoteProviderBase)(nil)
)

type integrationProviderSupport struct {
	sessionCatalog bool
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

func WithInvocationTokens(tokens *appaccessservice.InvocationTokenManager) RemoteProviderOption {
	return func(b *remoteProviderBase) { b.invTokens = tokens }
}

func WithInvocationTokenSubject(pluginName string, grants appaccessservice.InvocationGrants) RemoteProviderOption {
	return func(b *remoteProviderBase) {
		b.callerApp = strings.TrimSpace(pluginName)
		b.invokeGrants = appaccessservice.CloneInvocationGrants(grants)
	}
}

func WithWorkflowManagerGrants(grants workflowgrants.Grants) RemoteProviderOption {
	return func(b *remoteProviderBase) {
		b.workflowGrants = workflowgrants.Clone(grants)
	}
}

func NewRemote(ctx context.Context, client proto.AppProviderClient, spec StaticProviderSpec, config map[string]any, opts ...RemoteProviderOption) (core.Provider, error) {
	support, err := getAppProviderSupportWithRetry(ctx, client)
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

func NewRemoteProvider(ctx context.Context, client proto.AppProviderClient, spec StaticProviderSpec, config map[string]any, opts ...RemoteProviderOption) (core.Provider, error) {
	return NewRemote(ctx, client, spec, config, opts...)
}

func getAppProviderSupportWithRetry(ctx context.Context, client proto.AppProviderClient) (*integrationProviderSupport, error) {
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()

	for {
		meta, err := client.GetMetadata(ctx, &emptypb.Empty{})
		if err == nil {
			return &integrationProviderSupport{
				sessionCatalog: meta.GetSupportsSessionCatalog(),
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
		return core.ConnectionModeSubject
	}
	return core.NormalizeConnectionMode(p.connection)
}

func (p *remoteProviderBase) Execute(ctx context.Context, operation string, params map[string]any, token string) (*core.OperationResult, error) {
	msg, err := protoutil.StructFromMap(params)
	if err != nil {
		return nil, err
	}
	requestToken := ""
	if p != nil && p.invTokens != nil && p.callerApp != "" {
		mintCtx := invocation.WithCallerProvider(ctx, invocation.ProviderKindApp, p.callerApp)
		requestToken, err = p.invTokens.MintRootTokenWithWorkflowGrants(mintCtx, p.callerApp, p.invokeGrants, p.workflowGrants)
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
		return nil, remoteProviderExecuteError(err)
	}
	return &core.OperationResult{
		Status:  int(resp.GetStatus()),
		Headers: protoutil.StringListsFromProto(resp.GetHeaders()),
		Body:    resp.GetBody(),
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
			decorated.Operations[i].Transport = catalog.TransportApp
		}
	}
	return decorated
}

func callStartProvider(ctx context.Context, client proto.AppProviderClient, name string, config map[string]any) error {
	cfgStruct, err := protoutil.StructFromMap(config)
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
			Email:       subjectEmail(p),
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
		value, err := protoutil.StructFromMap(workflow)
		if err != nil {
			return nil, fmt.Errorf("workflow request context: %w", err)
		}
		out.Workflow = value
	}
	if toolRefs := invocation.ToolRefsContextFromContext(ctx); toolRefs.Set {
		out.ToolRefs = agentwire.ToolRefsToProto(toolRefs.Refs)
		out.ToolRefsSet = true
	}

	if publicBaseURL = normalizePublicBaseURL(publicBaseURL); publicBaseURL == "" {
		publicBaseURL = normalizePublicBaseURL(invocation.HostContextFromContext(ctx).PublicBaseURL)
	}
	if publicBaseURL != "" {
		out.Host = &proto.HostContext{PublicBaseUrl: publicBaseURL}
	}
	if audit := invocation.RunAsAuditFromContext(ctx); audit.AgentSubject != nil {
		out.AgentSubject = agentwire.RunAsSubjectToProto(audit.AgentSubject)
	}

	if out.Subject == nil && out.Credential == nil && out.Access == nil && out.Workflow == nil && !out.ToolRefsSet && len(out.ToolRefs) == 0 && out.Host == nil && out.AgentSubject == nil {
		return nil, nil
	}
	return &out, nil
}

func normalizePublicBaseURL(baseURL string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/")
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

func subjectEmail(p *principal.Principal) string {
	if p == nil || p.Identity == nil {
		return ""
	}
	return strings.TrimSpace(p.Identity.Email)
}
