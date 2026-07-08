package plugins

import (
	"context"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/protoutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

type gestaltRemoteProvider struct {
	client proto.AppClient
	spec   StaticProviderSpec
}

var (
	_ core.Provider             = (*gestaltRemoteProvider)(nil)
	_ core.GraphQLSurfaceInvoker = (*gestaltRemoteProvider)(nil)
)

// NewGestaltRemoteProvider routes app invocations through a remote gestaltd public App API.
func NewGestaltRemoteProvider(client proto.AppClient, spec StaticProviderSpec) core.Provider {
	if client == nil {
		return nil
	}
	name := strings.TrimSpace(spec.Name)
	if name == "" {
		return nil
	}
	spec.Name = name
	if strings.TrimSpace(spec.DisplayName) == "" {
		spec.DisplayName = name
	}
	return &gestaltRemoteProvider{client: client, spec: spec}
}

func (p *gestaltRemoteProvider) Name() string        { return p.spec.Name }
func (p *gestaltRemoteProvider) DisplayName() string { return p.spec.DisplayName }
func (p *gestaltRemoteProvider) Description() string { return p.spec.Description }

func (p *gestaltRemoteProvider) ConnectionMode() core.ConnectionMode {
	if p.spec.ConnectionMode == "" {
		return core.ConnectionModeSubject
	}
	return core.NormalizeConnectionMode(p.spec.ConnectionMode)
}

func (p *gestaltRemoteProvider) AuthTypes() []string {
	return append([]string(nil), p.spec.AuthTypes...)
}

func (p *gestaltRemoteProvider) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return p.spec.ConnectionParams
}

func (p *gestaltRemoteProvider) CredentialFields() []core.CredentialFieldDef {
	return append([]core.CredentialFieldDef(nil), p.spec.CredentialFields...)
}

func (p *gestaltRemoteProvider) DiscoveryConfig() *core.DiscoveryConfig {
	return p.spec.DiscoveryConfig
}

func (p *gestaltRemoteProvider) ConnectionForOperation(string) string { return "" }

func (p *gestaltRemoteProvider) Catalog() *catalog.Catalog {
	return p.spec.Catalog
}

func (p *gestaltRemoteProvider) Execute(ctx context.Context, operation string, params map[string]any, _ string) (*core.OperationResult, error) {
	paramsStruct, err := protoutil.StructFromMap(params)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Invoke(ctx, &proto.AppInvokeRequest{
		App:       p.spec.Name,
		Operation: strings.TrimSpace(operation),
		Params:    paramsStruct,
		Connection: strings.TrimSpace(invocation.ConnectionFromContext(ctx)),
		IdempotencyKey: invocation.IdempotencyKeyFromContext(ctx),
	})
	if err != nil {
		return nil, invocation.RemoteInvokeError(err)
	}
	return operationResultFromProto(resp), nil
}

func (p *gestaltRemoteProvider) InvokeGraphQL(ctx context.Context, request core.GraphQLRequest, _ string) (*core.OperationResult, error) {
	variables, err := protoutil.StructFromMap(request.Variables)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.InvokeGraphQL(ctx, &proto.AppInvokeGraphQLRequest{
		App:        p.spec.Name,
		Document:   strings.TrimSpace(request.Document),
		Variables:  variables,
		Connection: strings.TrimSpace(invocation.ConnectionFromContext(ctx)),
		IdempotencyKey: invocation.IdempotencyKeyFromContext(ctx),
	})
	if err != nil {
		return nil, invocation.RemoteInvokeError(err)
	}
	return operationResultFromProto(resp), nil
}

func operationResultFromProto(resp *proto.OperationResult) *core.OperationResult {
	if resp == nil {
		return &core.OperationResult{}
	}
	return &core.OperationResult{
		Status:  int(resp.GetStatus()),
		Headers: protoutil.StringListsFromProto(resp.GetHeaders()),
		Body:    resp.GetBody(),
	}
}
