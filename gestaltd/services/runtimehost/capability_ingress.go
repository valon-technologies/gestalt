package runtimehost

import (
	"context"
	"strings"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// CapabilityIngressDecorator enriches request context after a capability is verified.
type CapabilityIngressDecorator func(context.Context, HostServiceRelayTarget) context.Context

var errHostServiceCapabilityRequired = status.Error(codes.Unauthenticated, "host service relay token is required")

// SetCapabilityIngressDecorator configures post-verification context restoration.
func (m *HostServiceRelayTokenManager) SetCapabilityIngressDecorator(fn CapabilityIngressDecorator) {
	if m == nil {
		return
	}
	m.decorateContext = fn
}

// AuthenticateGRPC validates an incoming host-service capability for a gRPC method.
// Non-caller-dependent methods may proceed without a token; caller-dependent methods
// require a capability with embedded caller claims.
func (m *HostServiceRelayTokenManager) AuthenticateGRPC(ctx context.Context, fullMethod string) (context.Context, error) {
	requireCaller := CallerCapabilityRequiredMethod(fullMethod)
	if requireCaller && callerCapabilityAuthenticated(ctx) {
		return ctx, nil
	}
	target, err := m.resolveIncomingCapability(ctx, fullMethod, requireCaller)
	if err != nil {
		return nil, err
	}
	if target == nil {
		return ctx, nil
	}
	return m.applyCapabilityContext(ctx, *target), nil
}

func (m *HostServiceRelayTokenManager) resolveIncomingCapability(
	ctx context.Context,
	method string,
	requireCaller bool,
) (*HostServiceRelayTarget, error) {
	if m == nil {
		if requireCaller {
			return nil, errHostServiceCapabilityRequired
		}
		return nil, nil
	}
	token := relayTokenFromIncomingMetadata(ctx)
	if token == "" {
		if requireCaller {
			return nil, errHostServiceCapabilityRequired
		}
		return nil, nil
	}
	target, err := m.ResolveToken(token)
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "invalid host service relay token")
	}
	if !HostServiceRelayMethodAllowed(method, target.MethodPrefix) {
		return nil, status.Error(codes.PermissionDenied, "host-service-relay-method-not-allowed")
	}
	if requireCaller && target.Caller == nil {
		return nil, status.Error(codes.Unauthenticated, "invalid host service relay token")
	}
	if requireCaller && strings.TrimSpace(target.Caller.SubjectID) == "" {
		return nil, status.Error(codes.Unauthenticated, "invalid host service relay token")
	}
	return &target, nil
}

func callerCapabilityAuthenticated(ctx context.Context) bool {
	return strings.TrimSpace(gestalt.TrustedCallerSubjectFromContext(ctx)) != ""
}

func (m *HostServiceRelayTokenManager) applyCapabilityContext(ctx context.Context, target HostServiceRelayTarget) context.Context {
	if m != nil && m.decorateContext != nil {
		return m.decorateContext(ctx, target)
	}
	return ctx
}

// HostServiceRelayMethodAllowed reports whether method is allowed by the capability prefix.
func HostServiceRelayMethodAllowed(method, methodPrefix string) bool {
	method = strings.TrimSpace(method)
	methodPrefix = strings.TrimSpace(methodPrefix)
	if methodPrefix == "" {
		return true
	}
	if method == methodPrefix {
		return true
	}
	if strings.HasSuffix(methodPrefix, "/") {
		return strings.HasPrefix(method, methodPrefix)
	}
	return strings.HasPrefix(method, methodPrefix+"/")
}

// DevProviderSessionID returns a stable session identifier for dev-supervised providers.
func DevProviderSessionID(providerName string) string {
	return "dev:" + strings.TrimSpace(providerName)
}

func relayTokenFromIncomingMetadata(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	for _, value := range md.Get(HostServiceRelayTokenHeader) {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
