package invocation

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/protoutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// RemoteAppRouter decides whether a configured app should delegate to remote gestaltd.
type RemoteAppRouter interface {
	ShouldRouteRemoteApp(name string) bool
}

// RemoteRequestContextBuilder builds the trusted request context forwarded to remote gestaltd.
type RemoteRequestContextBuilder func(ctx context.Context, publicBaseURL string) (*proto.RequestContext, error)

type remoteAppPlacement struct {
	shouldRoute func(string) bool
}

func (r remoteAppPlacement) ShouldRouteRemoteApp(name string) bool {
	if r.shouldRoute == nil {
		return false
	}
	return r.shouldRoute(name)
}

// WithRemoteAppRouting configures broker delegation to a remote public App API.
func WithRemoteAppRouting(
	shouldRoute func(string) bool,
	client proto.AppClient,
	publicBaseURL string,
	buildContext RemoteRequestContextBuilder,
) BrokerOption {
	return func(b *Broker) {
		if shouldRoute == nil || client == nil || buildContext == nil {
			return
		}
		b.remoteApps = client
		b.remoteAppRouter = remoteAppPlacement{shouldRoute: shouldRoute}
		b.remotePublicBaseURL = strings.TrimRight(strings.TrimSpace(publicBaseURL), "/")
		b.remoteRequestContext = buildContext
	}
}

func (b *Broker) shouldDelegateApp(providerName string, lookupErr error) bool {
	if b == nil || b.remoteApps == nil || b.remoteAppRouter == nil || b.remoteRequestContext == nil || !errors.Is(lookupErr, core.ErrNotFound) {
		return false
	}
	return b.remoteAppRouter.ShouldRouteRemoteApp(providerName)
}

func (b *Broker) invokeRemoteApp(
	ctx context.Context,
	p *principal.Principal,
	providerName, instance, operation string,
	params map[string]any,
) (*core.OperationResult, error) {
	reqCtx, err := b.remoteRequestContext(ctx, b.remotePublicBaseURL)
	if err != nil {
		return nil, fmt.Errorf("%w: building remote request context: %v", ErrInternal, err)
	}
	paramsStruct, err := protoutil.StructFromMap(params)
	if err != nil {
		return nil, fmt.Errorf("%w: encoding remote params: %v", ErrInvalidInvocation, err)
	}
	conn := ConnectionFromContext(ctx)
	req := &proto.AppInvokeRequest{
		App:        providerName,
		Operation:  operation,
		Params:     paramsStruct,
		Context:    reqCtx,
		Connection: strings.TrimSpace(conn),
		Instance:   strings.TrimSpace(instance),
	}
	if mode := CredentialModeOverrideFromContext(ctx); mode != "" {
		req.CredentialMode = string(mode)
	}
	if key := IdempotencyKeyFromContext(ctx); key != "" {
		req.IdempotencyKey = key
	}

	resp, err := b.remoteApps.Invoke(ctx, req)
	if err != nil {
		return nil, remoteInvocationError(err)
	}
	return coreOperationResultFromProto(resp), nil
}

func (b *Broker) invokeRemoteGraphQL(
	ctx context.Context,
	p *principal.Principal,
	providerName, instance string,
	request GraphQLRequest,
) (*core.OperationResult, error) {
	reqCtx, err := b.remoteRequestContext(ctx, b.remotePublicBaseURL)
	if err != nil {
		return nil, fmt.Errorf("%w: building remote request context: %v", ErrInternal, err)
	}
	variables, err := protoutil.StructFromMap(request.Variables)
	if err != nil {
		return nil, fmt.Errorf("%w: encoding remote graphql variables: %v", ErrInvalidInvocation, err)
	}
	conn := ConnectionFromContext(ctx)
	req := &proto.AppInvokeGraphQLRequest{
		App:        providerName,
		Document:   request.Document,
		Variables:  variables,
		Context:    reqCtx,
		Connection: strings.TrimSpace(conn),
		Instance:   strings.TrimSpace(instance),
	}
	if key := IdempotencyKeyFromContext(ctx); key != "" {
		req.IdempotencyKey = key
	}

	resp, err := b.remoteApps.InvokeGraphQL(ctx, req)
	if err != nil {
		return nil, remoteInvocationError(err)
	}
	return coreOperationResultFromProto(resp), nil
}

func remoteInvocationError(err error) error {
	if err == nil {
		return nil
	}
	st, ok := status.FromError(err)
	if !ok {
		return fmt.Errorf("%w: remote app invocation: %v", ErrInternal, err)
	}
	switch st.Code() {
	case codes.Unauthenticated:
		return fmt.Errorf("%w: %s", ErrNotAuthenticated, st.Message())
	case codes.PermissionDenied:
		return fmt.Errorf("%w: %s", ErrAuthorizationDenied, st.Message())
	case codes.NotFound:
		return fmt.Errorf("%w: %s", ErrProviderNotFound, st.Message())
	case codes.InvalidArgument:
		return fmt.Errorf("%w: %s", ErrInvalidInvocation, st.Message())
	case codes.FailedPrecondition:
		return fmt.Errorf("%w: %s", ErrNoCredential, st.Message())
	case codes.Aborted:
		return fmt.Errorf("%w: %s", ErrAmbiguousInstance, st.Message())
	case codes.ResourceExhausted:
		return fmt.Errorf("%w: %s", ErrInternal, st.Message())
	default:
		return fmt.Errorf("%w: remote app invocation: %s", ErrInternal, st.Message())
	}
}

func coreOperationResultFromProto(result *proto.OperationResult) *core.OperationResult {
	if result == nil {
		return nil
	}
	return &core.OperationResult{
		Status:  int(result.GetStatus()),
		Headers: protoutil.StringListsFromProto(result.GetHeaders()),
		Body:    append([]byte(nil), result.GetBody()...),
	}
}

func (b *Broker) resolveRemoteConnectionMode(ctx context.Context, providerName string) core.ConnectionMode {
	if override := CredentialModeOverrideFromContext(ctx); override != "" {
		return override
	}
	if b != nil && b.connectionRuntime != nil {
		conn := ConnectionFromContext(ctx)
		if conn == "" && b.connMapper != nil {
			conn = b.connMapper.ConnectionForProvider(providerName)
		}
		if info, ok := b.connectionRuntime(providerName, conn); ok && info.Mode != "" {
			return core.NormalizeConnectionMode(info.Mode)
		}
	}
	return core.ConnectionModeSubject
}
