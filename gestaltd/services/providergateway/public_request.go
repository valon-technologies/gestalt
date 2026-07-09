package providergateway

import (
	"context"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/appaccess"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// PreparePublicRequest authenticates, authorizes, and adapts a public request.
// Internal/provider-runtime requests should not call this helper.
func (t *ProviderGatewayTransport) PreparePublicRequest(
	ctx context.Context,
	fullMethod string,
	req gproto.Message,
) (*principal.Principal, gproto.Message, error) {
	if t == nil {
		return nil, nil, status.Error(codes.Internal, "provider gateway: transport is nil")
	}
	if req == nil {
		return nil, nil, status.Error(codes.InvalidArgument, "request is required")
	}
	fullMethod = strings.TrimSpace(fullMethod)
	if fullMethod == "" {
		return nil, nil, status.Error(codes.Internal, "provider gateway: full method is required")
	}
	origin, ok := publicrpc.PublicOriginFromContext(ctx)
	if !ok {
		return nil, nil, status.Error(codes.Internal, "provider gateway: public origin marker is required")
	}
	if origin.FullMethod != fullMethod {
		return nil, nil, status.Errorf(codes.Internal, "provider gateway: public origin method %q does not match %q", origin.FullMethod, fullMethod)
	}
	if t.publicMethods == nil {
		return nil, nil, status.Error(codes.NotFound, "method is not public")
	}
	policy, ok := t.publicMethods.Lookup(fullMethod)
	if !ok {
		return nil, nil, status.Error(codes.NotFound, "method is not public")
	}

	if publicIdentityLoginMethod(fullMethod) {
		adapted, err := adaptPublicRequest(ctx, t.publicBaseURL, nil, req, policy, fullMethod)
		if err != nil {
			return nil, nil, err
		}
		return nil, adapted, nil
	}

	token := bearerTokenFromContext(ctx)
	if token == "" {
		return nil, nil, status.Error(codes.Unauthenticated, "bearer token is required")
	}
	p, err := t.resolvePublicPrincipal(ctx, token)
	if err != nil {
		return nil, nil, err
	}
	resourceID, err := publicResourceID(req, fullMethod)
	if err != nil {
		return nil, nil, err
	}
	if err := t.enforcePublicAuthorization(ctx, p, resourceID, fullMethod); err != nil {
		return nil, nil, err
	}
	adapted, err := adaptPublicRequest(ctx, t.publicBaseURL, p, req, policy, fullMethod)
	if err != nil {
		return nil, nil, err
	}
	return p, adapted, nil
}

func (t *ProviderGatewayTransport) resolvePublicPrincipal(ctx context.Context, token string) (*principal.Principal, error) {
	if t.identity == nil {
		return nil, status.Error(codes.Unauthenticated, "identity provider is not configured")
	}
	p, err := principal.NewResolver(t.identity).ResolveToken(ctx, token)
	if err != nil {
		if err == principal.ErrInvalidToken {
			return nil, status.Error(codes.Unauthenticated, "invalid bearer token")
		}
		return nil, status.Errorf(codes.Internal, "provider gateway: introspect: %v", err)
	}
	return p, nil
}

func (t *ProviderGatewayTransport) enforcePublicAuthorization(
	ctx context.Context,
	p *principal.Principal,
	providerID, operation string,
) error {
	if publicIdentityProviderID(providerID) {
		return nil
	}
	if t == nil || t.authorization == nil {
		return status.Error(codes.PermissionDenied, "authorization provider is not configured")
	}
	p = principal.Canonicalized(p)
	subjectID := strings.TrimSpace(p.SubjectID)
	if subjectID == "" {
		return status.Error(codes.Unauthenticated, "authenticated subject is required")
	}
	allowed, _, err := t.runAuthorizationCheck(ctx, &proto.Subject{
		Type: "subject",
		Id:   subjectID,
	}, providerID, operation)
	if err != nil {
		return status.Errorf(codes.Internal, "provider gateway: authorize: %v", err)
	}
	if !allowed {
		return status.Error(codes.PermissionDenied, "access denied")
	}
	return nil
}

func bearerTokenFromContext(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	for _, value := range md.Get("authorization") {
		value = strings.TrimSpace(value)
		if len(value) > 7 && strings.EqualFold(value[:7], "Bearer ") {
			if token := strings.TrimSpace(value[7:]); token != "" {
				return token
			}
		}
	}
	return ""
}

func publicResourceID(req gproto.Message, fullMethod string) (string, error) {
	msg := req.ProtoReflect()
	if fd := msg.Descriptor().Fields().ByName("app"); fd != nil {
		app := strings.TrimSpace(msg.Get(fd).String())
		if app == "" {
			return "", status.Error(codes.InvalidArgument, "app is required")
		}
		return app, nil
	}
	for _, field := range []protoreflect.Name{"app_name", "provider_name"} {
		fd := msg.Descriptor().Fields().ByName(field)
		if fd == nil {
			continue
		}
		if value := strings.TrimSpace(msg.Get(fd).String()); value != "" {
			return value, nil
		}
	}
	service, _ := splitFullMethod(fullMethod)
	if idx := strings.LastIndex(service, "."); idx >= 0 && idx+1 < len(service) {
		return strings.ToLower(service[idx+1:]), nil
	}
	if service == "" {
		return "", status.Error(codes.InvalidArgument, "provider id is required")
	}
	return service, nil
}

func publicIdentityProviderID(providerID string) bool {
	return strings.EqualFold(strings.TrimSpace(providerID), "identity")
}

func publicIdentityLoginMethod(fullMethod string) bool {
	_, method := splitFullMethod(fullMethod)
	switch method {
	case "Authorize", "Token":
		return true
	default:
		return false
	}
}

func splitFullMethod(fullMethod string) (service, method string) {
	fullMethod = strings.TrimSpace(fullMethod)
	if !strings.HasPrefix(fullMethod, "/") {
		return "", ""
	}
	service, method, ok := strings.Cut(strings.TrimPrefix(fullMethod, "/"), "/")
	if !ok {
		return "", ""
	}
	return service, method
}

func adaptPublicRequest(
	ctx context.Context,
	publicBaseURL string,
	p *principal.Principal,
	req gproto.Message,
	policy publicrpc.PublicMethodPolicy,
	fullMethod string,
) (gproto.Message, error) {
	adapted := gproto.Clone(req)
	msg := adapted.ProtoReflect()
	for _, name := range policy.Reject {
		if fieldSet(msg, name) {
			return nil, status.Errorf(codes.InvalidArgument, "%s is not public input", name)
		}
	}
	for _, name := range policy.Fill {
		if fieldSet(msg, name) {
			return nil, status.Errorf(codes.InvalidArgument, "%s is server-filled", name)
		}
		if err := fillPublicField(ctx, publicBaseURL, p, msg, name, fullMethod); err != nil {
			return nil, err
		}
	}
	return adapted, nil
}

func publicCallerProvider(_ protoreflect.Message, _ string) invocation.CallerProvider {
	return invocation.CallerProvider{Kind: invocation.ProviderKindApp, Name: "gestaltd"}
}

func fillPublicField(ctx context.Context, publicBaseURL string, p *principal.Principal, msg protoreflect.Message, name, fullMethod string) error {
	fd := msg.Descriptor().Fields().ByName(protoreflect.Name(name))
	if fd == nil {
		return status.Errorf(codes.Internal, "no public fill rule for %q", name)
	}
	switch name {
	case "context":
		callCtx := principal.WithPrincipal(ctx, principal.Canonicalized(p))
		reqCtx, err := appaccess.RequestContextProto(callCtx, publicBaseURL, publicCallerProvider(msg, fullMethod))
		if err != nil {
			return status.Errorf(codes.Internal, "provider gateway: build request context: %v", err)
		}
		if reqCtx == nil {
			reqCtx = &proto.RequestContext{}
		}
		msg.Set(fd, protoreflect.ValueOfMessage(reqCtx.ProtoReflect()))
	default:
		return status.Errorf(codes.Internal, "no public fill rule for %q", name)
	}
	return nil
}

func fieldSet(msg protoreflect.Message, name string) bool {
	fd := msg.Descriptor().Fields().ByName(protoreflect.Name(name))
	if fd == nil {
		return false
	}
	if fd.IsList() {
		return msg.Get(fd).List().Len() > 0
	}
	if fd.IsMap() {
		return msg.Get(fd).Map().Len() > 0
	}
	switch fd.Kind() {
	case protoreflect.MessageKind, protoreflect.GroupKind:
		return msg.Has(fd)
	case protoreflect.StringKind, protoreflect.BytesKind:
		return msg.Has(fd) && strings.TrimSpace(msg.Get(fd).String()) != ""
	default:
		return msg.Has(fd)
	}
}
