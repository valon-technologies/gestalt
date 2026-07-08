package remote

import (
	"context"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/protoutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	appservice "github.com/valon-technologies/gestalt/server/services/apps"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

// AppProvider routes app operations to a remote gestaltd public App API.
type AppProvider struct {
	client proto.AppClient
	spec   appservice.StaticProviderSpec
}

var (
	_ core.Provider            = (*AppProvider)(nil)
	_ core.GraphQLSurfaceInvoker = (*AppProvider)(nil)
)

// NewAppProvider constructs a gestaltd-to-gestaltd app provider backed by the public App client.
func NewAppProvider(client proto.AppClient, spec appservice.StaticProviderSpec) *AppProvider {
	return &AppProvider{
		client: client,
		spec:   spec,
	}
}

func (p *AppProvider) Close() error { return nil }

func (p *AppProvider) Name() string { return p.spec.Name }

func (p *AppProvider) DisplayName() string { return p.spec.DisplayName }

func (p *AppProvider) Description() string { return p.spec.Description }

func (p *AppProvider) ConnectionMode() core.ConnectionMode {
	if p.spec.ConnectionMode == "" {
		return core.ConnectionModeSubject
	}
	return core.NormalizeConnectionMode(p.spec.ConnectionMode)
}

func (p *AppProvider) Catalog() *catalog.Catalog { return p.spec.Catalog }

func (p *AppProvider) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return p.spec.ConnectionParams
}

func (p *AppProvider) AuthTypes() []string { return p.spec.AuthTypes }

func (p *AppProvider) CredentialFields() []core.CredentialFieldDef { return p.spec.CredentialFields }

func (p *AppProvider) DiscoveryConfig() *core.DiscoveryConfig { return p.spec.DiscoveryConfig }

func (p *AppProvider) ConnectionForOperation(string) string { return "" }

func (p *AppProvider) Execute(ctx context.Context, operation string, params map[string]any, _ string) (*core.OperationResult, error) {
	if p == nil || p.client == nil {
		return nil, invocation.ErrInternal
	}
	msg, err := protoutil.StructFromMap(params)
	if err != nil {
		return nil, err
	}
	cred := invocation.CredentialContextFromContext(ctx)
	req := &proto.AppInvokeRequest{
		App:       p.spec.Name,
		Operation: strings.TrimSpace(operation),
		Params:    msg,
		Connection: firstNonEmpty(
			invocation.ConnectionFromContext(ctx),
			cred.Connection,
		),
		Instance:       strings.TrimSpace(cred.Instance),
		IdempotencyKey: invocation.IdempotencyKeyFromContext(ctx),
	}
	if mode := invocation.CredentialModeOverrideFromContext(ctx); mode != "" {
		req.CredentialMode = string(mode)
	}
	resp, err := p.client.Invoke(ctx, req)
	if err != nil {
		return nil, invokeError(err)
	}
	return operationResultFromProto(resp), nil
}

func (p *AppProvider) InvokeGraphQL(ctx context.Context, request core.GraphQLRequest, _ string) (*core.OperationResult, error) {
	if p == nil || p.client == nil {
		return nil, invocation.ErrInternal
	}
	variables, err := protoutil.StructFromMap(request.Variables)
	if err != nil {
		return nil, err
	}
	cred := invocation.CredentialContextFromContext(ctx)
	req := &proto.AppInvokeGraphQLRequest{
		App:      p.spec.Name,
		Document: strings.TrimSpace(request.Document),
		Variables: variables,
		Connection: firstNonEmpty(
			invocation.ConnectionFromContext(ctx),
			cred.Connection,
		),
		Instance:       strings.TrimSpace(cred.Instance),
		IdempotencyKey: invocation.IdempotencyKeyFromContext(ctx),
	}
	resp, err := p.client.InvokeGraphQL(ctx, req)
	if err != nil {
		return nil, invokeError(err)
	}
	return operationResultFromProto(resp), nil
}

func operationResultFromProto(resp *proto.OperationResult) *core.OperationResult {
	if resp == nil {
		return nil
	}
	return &core.OperationResult{
		Status:  int(resp.GetStatus()),
		Headers: protoutil.StringListsFromProto(resp.GetHeaders()),
		Body:    resp.GetBody(),
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
