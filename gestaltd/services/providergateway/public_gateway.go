package providergateway

import (
	"context"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/appaccess"
	"github.com/valon-technologies/gestalt/server/services/identity"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/publicrpc"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// IdentityProvider authenticates public bearer tokens.
type IdentityProvider = core.IdentityProvider

// Gateway adapts public/external gRPC requests before shared service dispatch.
type Gateway struct {
	publicMethods publicrpc.PublicMethodRegistry
	identity      IdentityProvider
	authorization AuthorizationProvider
	publicBaseURL string
}

func NewGateway(publicMethods publicrpc.PublicMethodRegistry, identity IdentityProvider, authorization AuthorizationProvider) *Gateway {
	return &Gateway{
		publicMethods: publicMethods,
		identity:      identity,
		authorization: authorization,
	}
}

func (g *Gateway) SetPublicBaseURL(publicBaseURL string) {
	if g == nil {
		return
	}
	g.publicBaseURL = strings.TrimRight(strings.TrimSpace(publicBaseURL), "/")
}

// PreparePublicRequest authenticates, authorizes, and adapts a public request.
// Internal/provider-runtime requests should not call this helper.
func (g *Gateway) PreparePublicRequest(
	ctx context.Context,
	fullMethod string,
	req gproto.Message,
) (*principal.Principal, gproto.Message, error) {
	if g == nil {
		return nil, nil, status.Error(codes.Internal, "provider gateway: gateway is nil")
	}
	if req == nil {
		return nil, nil, status.Error(codes.InvalidArgument, "request is required")
	}
	fullMethod = strings.TrimSpace(fullMethod)
	if fullMethod == "" {
		return nil, nil, status.Error(codes.Internal, "provider gateway: full method is required")
	}
	if !publicrpc.IsPublicOrigin(ctx) {
		return nil, nil, status.Error(codes.Internal, "provider gateway: public origin marker is required")
	}
	if originMethod, ok := publicrpc.FullMethodFromContext(ctx); ok && originMethod != fullMethod {
		return nil, nil, status.Errorf(codes.Internal, "provider gateway: public origin method %q does not match %q", originMethod, fullMethod)
	}

	policy, ok := g.lookupPublicMethod(fullMethod)
	if !ok {
		return nil, nil, status.Error(codes.NotFound, "method is not public")
	}

	token := bearerTokenFromContext(ctx)
	if token == "" {
		return nil, nil, status.Error(codes.Unauthenticated, "bearer token is required")
	}
	p, err := g.resolvePrincipal(ctx, token)
	if err != nil {
		return nil, nil, err
	}

	resourceID := publicResourceID(req, policy)
	if err := g.authorizePublic(ctx, p, fullMethod, resourceID); err != nil {
		return nil, nil, err
	}

	adapted, err := adaptPublicRequest(ctx, g.publicBaseURL, p, req, policy)
	if err != nil {
		return nil, nil, err
	}
	return p, adapted, nil
}

func (g *Gateway) lookupPublicMethod(fullMethod string) (publicrpc.PublicMethodPolicy, bool) {
	if g.publicMethods == nil {
		return publicrpc.PublicMethodPolicy{}, false
	}
	return g.publicMethods.Lookup(fullMethod)
}

func (g *Gateway) resolvePrincipal(ctx context.Context, token string) (*principal.Principal, error) {
	if g.identity == nil {
		return nil, status.Error(codes.Unauthenticated, "identity provider is not configured")
	}
	resolver := principal.NewResolver(g.identity)
	p, err := resolver.ResolveToken(ctx, token)
	if err != nil {
		if err == principal.ErrInvalidToken {
			return nil, status.Error(codes.Unauthenticated, "invalid bearer token")
		}
		return nil, status.Errorf(codes.Internal, "provider gateway: introspect: %v", err)
	}
	return p, nil
}

func (g *Gateway) authorizePublic(ctx context.Context, p *principal.Principal, fullMethod, resourceID string) error {
	if g.authorization == nil {
		return nil
	}
	subject, err := authorizationSubjectFromPrincipal(p)
	if err != nil {
		return err
	}
	resource, err := authorizationResource(resourceID)
	if err != nil {
		return err
	}
	action, err := authorizationAction(fullMethod)
	if err != nil {
		return err
	}
	resp, err := g.authorization.CheckAccess(ctx, &proto.CheckAccessRequest{
		Subject:  subject,
		Resource: resource,
		Action:   action,
	})
	if err != nil {
		return status.Errorf(codes.Internal, "provider gateway: authorize: %v", err)
	}
	if resp == nil || !resp.GetAllowed() {
		return status.Error(codes.PermissionDenied, "access denied")
	}
	return nil
}

func authorizationSubjectFromPrincipal(p *principal.Principal) (*proto.Subject, error) {
	p = principal.Canonicalized(p)
	subjectID := strings.TrimSpace(p.SubjectID)
	if subjectID == "" {
		return nil, status.Error(codes.Unauthenticated, "authenticated subject is required")
	}
	return &proto.Subject{
		Type: "subject",
		Id:   subjectID,
	}, nil
}

func publicResourceID(req gproto.Message, policy publicrpc.PublicMethodPolicy) string {
	msg := req.ProtoReflect()
	if fd := msg.Descriptor().Fields().ByName("app"); fd != nil {
		if app := strings.TrimSpace(msg.Get(fd).String()); app != "" {
			return app
		}
	}
	if fd := msg.Descriptor().Fields().ByName("provider_name"); fd != nil {
		if name := strings.TrimSpace(msg.Get(fd).String()); name != "" {
			return name
		}
	}
	service := strings.TrimSpace(policy.Service)
	if idx := strings.LastIndex(service, "."); idx >= 0 && idx+1 < len(service) {
		return strings.ToLower(service[idx+1:])
	}
	return service
}

func adaptPublicRequest(
	ctx context.Context,
	publicBaseURL string,
	p *principal.Principal,
	req gproto.Message,
	policy publicrpc.PublicMethodPolicy,
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
		if err := fillPublicField(ctx, publicBaseURL, p, msg, name); err != nil {
			return nil, err
		}
	}
	return adapted, nil
}

func fillPublicField(ctx context.Context, publicBaseURL string, p *principal.Principal, msg protoreflect.Message, name string) error {
	fd := msg.Descriptor().Fields().ByName(protoreflect.Name(name))
	if fd == nil {
		return status.Errorf(codes.Internal, "no public fill rule for %q", name)
	}
	switch name {
	case "context":
		return fillRequestContext(ctx, publicBaseURL, p, msg, fd)
	case "subject":
		return fillSubjectContext(p, msg, fd)
	case "created_by_subject_id":
		return fillCreatedBySubjectID(p, msg, fd)
	default:
		return status.Errorf(codes.Internal, "no public fill rule for %q", name)
	}
}

func fillRequestContext(ctx context.Context, publicBaseURL string, p *principal.Principal, msg protoreflect.Message, fd protoreflect.FieldDescriptor) error {
	callCtx := principal.WithPrincipal(ctx, principal.Canonicalized(p))
	reqCtx, err := appaccess.RequestContextProto(callCtx, publicBaseURL, invocation.CallerProvider{})
	if err != nil {
		return status.Errorf(codes.Internal, "provider gateway: build request context: %v", err)
	}
	if reqCtx == nil {
		reqCtx = &proto.RequestContext{}
	}
	msg.Set(fd, protoreflect.ValueOfMessage(reqCtx.ProtoReflect()))
	return nil
}

func fillSubjectContext(p *principal.Principal, msg protoreflect.Message, fd protoreflect.FieldDescriptor) error {
	subject := subjectContextFromPrincipal(p)
	if subject == nil {
		return status.Error(codes.Internal, "provider gateway: subject is required")
	}
	msg.Set(fd, protoreflect.ValueOfMessage(subject.ProtoReflect()))
	return nil
}

func fillCreatedBySubjectID(p *principal.Principal, msg protoreflect.Message, fd protoreflect.FieldDescriptor) error {
	p = principal.Canonicalized(p)
	subjectID := strings.TrimSpace(p.SubjectID)
	if subjectID == "" {
		return status.Error(codes.Internal, "provider gateway: created_by_subject_id is required")
	}
	msg.Set(fd, protoreflect.ValueOfString(subjectID))
	return nil
}

func subjectContextFromPrincipal(p *principal.Principal) *proto.SubjectContext {
	p = principal.Canonicalized(p)
	if p == nil || strings.TrimSpace(p.SubjectID) == "" {
		return nil
	}
	out := &proto.SubjectContext{
		Id:          p.SubjectID,
		Scopes:      append([]string(nil), p.Scopes...),
		Permissions: permissionSetToSubjectPermissionContext(p.EffectivePermissions()),
	}
	if p.Identity != nil {
		out.Email = strings.TrimSpace(p.Identity.Email)
		out.DisplayName = strings.TrimSpace(p.Identity.DisplayName)
	}
	if out.DisplayName == "" {
		out.DisplayName = strings.TrimSpace(p.DisplayName)
	}
	return out
}

func permissionSetToSubjectPermissionContext(set principal.PermissionSet) []*proto.SubjectPermissionContext {
	perms := principal.PermissionsToAccessPermissions(set)
	if len(perms) == 0 {
		return nil
	}
	out := make([]*proto.SubjectPermissionContext, 0, len(perms))
	for _, perm := range perms {
		app := strings.TrimSpace(perm.App)
		if app == "" {
			continue
		}
		ctx := &proto.SubjectPermissionContext{App: app}
		if len(perm.Operations) == 0 {
			ctx.AllOperations = true
		} else {
			ctx.Operations = append([]string(nil), perm.Operations...)
		}
		out = append(out, ctx)
	}
	return out
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

// WithPublicBearerToken attaches the caller bearer token for identity UserInfo.
func WithPublicBearerToken(ctx context.Context, token string) context.Context {
	return identity.WithCallerBearerToken(ctx, token)
}
