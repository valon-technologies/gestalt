package plugins

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/protoutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	appaccessservice "github.com/valon-technologies/gestalt/server/services/appaccess"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	protobuf "google.golang.org/protobuf/proto"
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
	client              proto.AppProviderClient
	support             integrationProviderSupport
	name                string
	displayName         string
	description         string
	connection          core.ConnectionMode
	catalog             *catalog.Catalog
	iconSVG             string
	authTypes           []string
	connParams          map[string]core.ConnectionParamDef
	credFields          []core.CredentialFieldDef
	discovery           *core.DiscoveryConfig
	closer              io.Closer
	publicBaseURL       string
	callerKind          invocation.ProviderKind
	callerName          string
	runtimeSessionID    string
	relayTokenManager   *runtimehost.HostServiceRelayTokenManager
	workflowDefinitions []*proto.WorkflowDefinitionSpec
}

var (
	_ core.SessionCatalogProvider = (*remoteProviderBase)(nil)
)

type integrationProviderSupport struct {
	sessionCatalog      bool
	workflowDefinitions []*proto.WorkflowDefinitionSpec
}

// RemoteProviderOption configures a remote provider returned by NewRemote.
type RemoteProviderOption func(*remoteProviderBase)

// WithCloser attaches a closer that is called when the provider is closed.
// This is used to tie process lifecycle to provider lifecycle.
func WithCloser(c io.Closer) RemoteProviderOption {
	return func(b *remoteProviderBase) { b.closer = c }
}

func WithHostContext(publicBaseURL string) RemoteProviderOption {
	return func(b *remoteProviderBase) { b.publicBaseURL = normalizePublicBaseURL(publicBaseURL) }
}

func WithCallerProvider(kind invocation.ProviderKind, name string) RemoteProviderOption {
	return func(b *remoteProviderBase) {
		b.callerKind = invocation.ProviderKind(strings.TrimSpace(string(kind)))
		b.callerName = strings.TrimSpace(name)
	}
}

// WithRuntimeSessionID binds invocation capabilities to a hosted runtime session.
func WithRuntimeSessionID(sessionID string) RemoteProviderOption {
	return func(b *remoteProviderBase) {
		b.runtimeSessionID = strings.TrimSpace(sessionID)
	}
}

// WithRelayTokenManager enables per-invocation capability minting for hosted providers.
func WithRelayTokenManager(manager *runtimehost.HostServiceRelayTokenManager) RemoteProviderOption {
	return func(b *remoteProviderBase) {
		b.relayTokenManager = manager
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
		client:              client,
		support:             *support,
		name:                spec.Name,
		displayName:         spec.DisplayName,
		description:         spec.Description,
		connection:          spec.ConnectionMode,
		catalog:             spec.Catalog,
		iconSVG:             spec.IconSVG,
		authTypes:           spec.AuthTypes,
		connParams:          spec.ConnectionParams,
		credFields:          spec.CredentialFields,
		discovery:           spec.DiscoveryConfig,
		workflowDefinitions: append([]*proto.WorkflowDefinitionSpec(nil), support.workflowDefinitions...),
	}
	for _, opt := range opts {
		opt(base)
	}

	return base, nil
}

func getAppProviderSupportWithRetry(ctx context.Context, client proto.AppProviderClient) (*integrationProviderSupport, error) {
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()

	for {
		meta, err := client.GetMetadata(ctx, &emptypb.Empty{})
		if err == nil {
			specs, decodeErr := decodeWorkflowDefinitionSpecs(meta.GetWorkflowDefinitionSpecs())
			if decodeErr != nil {
				return nil, decodeErr
			}
			return &integrationProviderSupport{
				sessionCatalog:      meta.GetSupportsSessionCatalog(),
				workflowDefinitions: specs,
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

func (p *remoteProviderBase) DeclaredWorkflowDefinitions() []*proto.WorkflowDefinitionSpec {
	if p == nil || len(p.workflowDefinitions) == 0 {
		return nil
	}
	return append([]*proto.WorkflowDefinitionSpec(nil), p.workflowDefinitions...)
}

// DeclaredWorkflowDefinitions returns workflow specs declared by a remote app
// provider, or nil when the provider does not surface declarations.
func DeclaredWorkflowDefinitions(p core.Provider) []*proto.WorkflowDefinitionSpec {
	if p == nil {
		return nil
	}
	if remote, ok := p.(interface {
		DeclaredWorkflowDefinitions() []*proto.WorkflowDefinitionSpec
	}); ok {
		return remote.DeclaredWorkflowDefinitions()
	}
	return nil
}

func decodeWorkflowDefinitionSpecs(raw [][]byte) ([]*proto.WorkflowDefinitionSpec, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	specs := make([]*proto.WorkflowDefinitionSpec, 0, len(raw))
	for i, encoded := range raw {
		spec := &proto.WorkflowDefinitionSpec{}
		if err := protobuf.Unmarshal(encoded, spec); err != nil {
			return nil, fmt.Errorf("decode workflow_definition_specs[%d]: %w", i, err)
		}
		specs = append(specs, spec)
	}
	return specs, nil
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
	reqCtx, err := p.requestContextProto(ctx)
	if err != nil {
		return nil, err
	}
	ctx, err = p.attachInvocationCapability(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Execute(ctx, &proto.ExecuteRequest{
		Operation:        operation,
		Params:           msg,
		Token:            token,
		ConnectionParams: core.ConnectionParams(ctx),
		InvocationId:     invocationIDFromContext(ctx),
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
	ctx, err = p.attachInvocationCapability(ctx)
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

func (p *remoteProviderBase) attachInvocationCapability(ctx context.Context) (context.Context, error) {
	if p == nil || p.relayTokenManager == nil {
		return ctx, nil
	}
	caller := principal.FromContext(ctx)
	if caller == nil {
		return ctx, nil
	}
	claims := principalRelayClaimsFromPrincipal(caller)
	if claims == nil {
		return ctx, fmt.Errorf("mint invocation capability: caller principal subject is required")
	}
	capability, err := p.relayTokenManager.MintToken(runtimehost.HostServiceRelayTokenRequest{
		AppName:      p.name,
		SessionID:    p.runtimeSessionID,
		Service:      "host_service",
		MethodPrefix: "/",
		Caller:       claims,
	})
	if err != nil {
		return ctx, fmt.Errorf("mint invocation capability: %w", err)
	}
	return metadata.AppendToOutgoingContext(ctx, runtimehost.HostServiceRelayTokenHeader, capability), nil
}

func (p *remoteProviderBase) requestContextProto(ctx context.Context) (*proto.RequestContext, error) {
	if p == nil {
		return requestContextProto(ctx, "", invocation.CallerProvider{})
	}
	return requestContextProto(ctx, p.publicBaseURL, invocation.CallerProvider{Kind: p.callerKind, Name: p.callerName})
}

func requestContextProto(ctx context.Context, publicBaseURL string, caller invocation.CallerProvider) (*proto.RequestContext, error) {
	return appaccessservice.RequestContextProto(ctx, publicBaseURL, caller)
}

func normalizePublicBaseURL(baseURL string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/")
}

type staticSpecProvider struct {
	spec StaticProviderSpec
}

func newStaticSpecProvider(spec StaticProviderSpec) staticSpecProvider {
	name := strings.TrimSpace(spec.Name)
	if name == "" {
		return staticSpecProvider{}
	}
	spec.Name = name
	if strings.TrimSpace(spec.DisplayName) == "" {
		spec.DisplayName = name
	}
	return staticSpecProvider{spec: spec}
}

func (p staticSpecProvider) Name() string        { return p.spec.Name }
func (p staticSpecProvider) DisplayName() string { return p.spec.DisplayName }
func (p staticSpecProvider) Description() string { return p.spec.Description }

func (p staticSpecProvider) ConnectionMode() core.ConnectionMode {
	if p.spec.ConnectionMode == "" {
		return core.ConnectionModeSubject
	}
	return core.NormalizeConnectionMode(p.spec.ConnectionMode)
}

func (p staticSpecProvider) AuthTypes() []string {
	return append([]string(nil), p.spec.AuthTypes...)
}

func (p staticSpecProvider) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return p.spec.ConnectionParams
}

func (p staticSpecProvider) CredentialFields() []core.CredentialFieldDef {
	return append([]core.CredentialFieldDef(nil), p.spec.CredentialFields...)
}

func (p staticSpecProvider) DiscoveryConfig() *core.DiscoveryConfig {
	return p.spec.DiscoveryConfig
}

func (p staticSpecProvider) ConnectionForOperation(string) string { return "" }

func (p staticSpecProvider) Catalog() *catalog.Catalog {
	return p.spec.Catalog
}

type gestaltRemoteProvider struct {
	staticSpecProvider
	client proto.AppClient
}

var (
	_ core.Provider                        = (*gestaltRemoteProvider)(nil)
	_ core.GraphQLSurfaceInvoker           = (*gestaltRemoteProvider)(nil)
	_ invocation.RemoteCredentialDelegated = (*gestaltRemoteProvider)(nil)
)

// NewGestaltRemote routes app invocations through a remote gestaltd public App API.
func NewGestaltRemote(client proto.AppClient, spec StaticProviderSpec) core.Provider {
	base := newStaticSpecProvider(spec)
	if client == nil || base.spec.Name == "" {
		return nil
	}
	return &gestaltRemoteProvider{staticSpecProvider: base, client: client}
}

func (p *gestaltRemoteProvider) RemoteCredentialDelegated() bool { return true }

func (p *gestaltRemoteProvider) Execute(ctx context.Context, operation string, params map[string]any, _ string) (*core.OperationResult, error) {
	paramsStruct, err := protoutil.StructFromMap(params)
	if err != nil {
		return nil, err
	}
	connection, instance := remoteAppInvokeSelectors(ctx)
	resp, err := p.client.Invoke(ctx, &proto.AppInvokeRequest{
		App:            p.spec.Name,
		Operation:      strings.TrimSpace(operation),
		Params:         paramsStruct,
		Connection:     connection,
		Instance:       instance,
		IdempotencyKey: invocation.IdempotencyKeyFromContext(ctx),
	})
	if err != nil {
		return nil, remoteProviderExecuteError(err)
	}
	return remoteOperationResult(resp), nil
}

func (p *gestaltRemoteProvider) InvokeGraphQL(ctx context.Context, request core.GraphQLRequest, _ string) (*core.OperationResult, error) {
	variables, err := protoutil.StructFromMap(request.Variables)
	if err != nil {
		return nil, err
	}
	connection, instance := remoteAppInvokeSelectors(ctx)
	resp, err := p.client.InvokeGraphQL(ctx, &proto.AppInvokeGraphQLRequest{
		App:            p.spec.Name,
		Document:       strings.TrimSpace(request.Document),
		Variables:      variables,
		Connection:     connection,
		Instance:       instance,
		IdempotencyKey: invocation.IdempotencyKeyFromContext(ctx),
	})
	if err != nil {
		return nil, remoteProviderExecuteError(err)
	}
	return remoteOperationResult(resp), nil
}

func remoteAppInvokeSelectors(ctx context.Context) (connection, instance string) {
	cred := invocation.CredentialContextFromContext(ctx)
	connection = strings.TrimSpace(cred.Connection)
	if connection == "" {
		connection = strings.TrimSpace(invocation.ConnectionFromContext(ctx))
	}
	return connection, strings.TrimSpace(cred.Instance)
}

func remoteOperationResult(resp *proto.OperationResult) *core.OperationResult {
	if resp == nil {
		return &core.OperationResult{}
	}
	return &core.OperationResult{
		Status:  int(resp.GetStatus()),
		Headers: protoutil.StringListsFromProto(resp.GetHeaders()),
		Body:    resp.GetBody(),
	}
}

func principalRelayClaimsFromPrincipal(p *principal.Principal) *runtimehost.PrincipalClaims {
	if p == nil {
		return nil
	}
	subjectID := strings.TrimSpace(p.SubjectID)
	if subjectID == "" && strings.TrimSpace(p.UserID) != "" {
		subjectID = principal.UserSubjectID(strings.TrimSpace(p.UserID))
	}
	if subjectID == "" {
		return nil
	}
	claims := &runtimehost.PrincipalClaims{
		SubjectID: subjectID,
		ClientID:  strings.TrimSpace(p.ClientID),
	}
	if len(p.Scopes) > 0 {
		claims.Scopes = append([]string(nil), p.Scopes...)
	}
	if len(p.Audience) > 0 {
		claims.Audience = append([]string(nil), p.Audience...)
	}
	return claims
}
