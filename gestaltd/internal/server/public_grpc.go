package server

import (
	"context"
	"net/http"
	"strings"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	agentservice "github.com/valon-technologies/gestalt/server/services/agents"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	appaccessservice "github.com/valon-technologies/gestalt/server/services/appaccess"
	authorizationservice "github.com/valon-technologies/gestalt/server/services/authorization"
	externalcredentialsservice "github.com/valon-technologies/gestalt/server/services/externalcredentials"
	identityservice "github.com/valon-technologies/gestalt/server/services/identity"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	indexeddbservice "github.com/valon-technologies/gestalt/server/services/indexeddb"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/providergateway"
	workflowservice "github.com/valon-technologies/gestalt/server/services/workflows"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowmanager"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	gproto "google.golang.org/protobuf/proto"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

type publicGRPCConfig struct {
	Transport           *providergateway.ProviderGatewayTransport
	Invoker             invocation.Invoker
	AgentManager        agentmanager.Service
	WorkflowManager     workflowmanager.Service
	Authentication      core.IdentityProvider
	Authorization       core.AuthorizationProvider
	IndexedDB           indexeddb.IndexedDB
	ExternalCredentials core.ExternalCredentialProvider
	RemoteManagement    proto.RemoteManagementServer
}

func appAccessServer(cfg publicGRPCConfig) proto.AppServer {
	return appaccessservice.NewServer(
		cfg.Invoker,
		appaccessservice.WithAgentAppInvocationAuthorizer(cfg.AgentManager),
	)
}

func agentProviderServer(cfg publicGRPCConfig) proto.AgentServer {
	return agentservice.NewProviderServer(
		"gestaltd",
		cfg.AgentManager,
	)
}

func workflowProviderServer(cfg publicGRPCConfig) proto.WorkflowServer {
	return workflowservice.NewProviderServer(
		"gestaltd",
		cfg.WorkflowManager,
		cfg.Authorization,
		workflowservice.WithAgentWorkflowInvocationAuthorizer(cfg.AgentManager),
	)
}

func indexedDBServer(cfg publicGRPCConfig) proto.IndexedDBServer {
	return indexeddbservice.NewServer(
		cfg.IndexedDB,
		"gestaltd",
		indexeddbservice.ServerOptions{},
	)
}

func identityProviderServer(cfg publicGRPCConfig) proto.IdentityServer {
	return identityservice.NewProviderServer(cfg.Authentication)
}

func authorizationProviderServer(cfg publicGRPCConfig) proto.AuthorizationServer {
	return authorizationservice.NewProviderServer(cfg.Authorization)
}

func externalCredentialsProviderServer(cfg publicGRPCConfig) proto.ExternalCredentialsServer {
	return externalcredentialsservice.NewProviderServer(cfg.ExternalCredentials)
}

func publicPrepareUnaryInterceptor(transport *providergateway.ProviderGatewayTransport) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		origin, ok := publicrpc.PublicOriginFromContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "bearer token is required")
		}
		msg, ok := req.(gproto.Message)
		if !ok {
			return nil, status.Error(codes.Internal, "request type mismatch")
		}
		p, adapted, err := transport.PreparePublicRequest(ctx, origin.FullMethod, msg)
		if err != nil {
			return nil, err
		}
		if p != nil {
			ctx = stripInternalIdentityMetadata(ctx)
			canonical := principal.Canonicalized(p)
			ctx = principal.WithPrincipal(ctx, canonical)
			if subjectID := strings.TrimSpace(canonical.SubjectID); subjectID != "" {
				ctx = gestalt.WithTrustedCallerSubject(ctx, subjectID)
			}
			return handler(ctx, adapted)
		}
		return handler(stripInternalIdentityMetadata(ctx), adapted)
	}
}

// publicPrepareStreamInterceptor mirrors publicPrepareUnaryInterceptor for
// server-streaming public methods: it verifies a public origin, authenticates
// the bearer, and attaches the resolved principal to the stream context.
func publicPrepareStreamInterceptor(transport *providergateway.ProviderGatewayTransport) grpc.StreamServerInterceptor {
	return func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		// Stream interceptors run before wrapPublicStreamHandler attaches the
		// public-origin marker, so derive the origin from the gRPC full method
		// directly and verify it is a registered public method.
		fullMethod := info.FullMethod
		reg, err := publicrpc.LoadGeneratedRegistry()
		if err != nil {
			return status.Error(codes.Internal, "publicrpc registry unavailable")
		}
		if _, ok := reg.Lookup(fullMethod); !ok {
			return status.Error(codes.Unauthenticated, "bearer token is required")
		}
		authStream := &publicAuthStream{ServerStream: stream, transport: transport, fullMethod: fullMethod}
		return handler(srv, authStream)
	}
}

// publicAuthStream authenticates on the first message and stamps the resolved
// principal onto the context for downstream handlers.
type publicAuthStream struct {
	grpc.ServerStream
	transport  *providergateway.ProviderGatewayTransport
	fullMethod string
	authed     bool
	principal  *principal.Principal
}

func (s *publicAuthStream) Context() context.Context {
	// Always strip caller-supplied internal identity metadata, mirroring the
	// unary public interceptor: a public caller must not forge a trusted
	// caller subject. The resolved principal is reattached below.
	ctx := stripInternalIdentityMetadata(s.ServerStream.Context())
	if s.principal != nil {
		canonical := principal.Canonicalized(s.principal)
		ctx = principal.WithPrincipal(ctx, canonical)
		if subjectID := strings.TrimSpace(canonical.SubjectID); subjectID != "" {
			ctx = gestalt.WithTrustedCallerSubject(ctx, subjectID)
		}
	}
	return ctx
}

func (s *publicAuthStream) RecvMsg(msg any) error {
	if err := s.ServerStream.RecvMsg(msg); err != nil {
		return err
	}
	if !s.authed {
		s.authed = true
		m, ok := msg.(gproto.Message)
		if !ok {
			return status.Error(codes.Internal, "request type mismatch")
		}
		p, adapted, err := s.transport.PreparePublicRequest(s.ServerStream.Context(), s.fullMethod, m)
		if err != nil {
			return err
		}
		s.principal = p
		if adapted != nil && m != adapted {
			gproto.Reset(m)
			// Copy adapted fields into the decoded message via proto.Marshal/Unmarshal.
			data, err := gproto.Marshal(adapted)
			if err != nil {
				return status.Error(codes.Internal, "failed to adapt request")
			}
			if err := gproto.Unmarshal(data, m); err != nil {
				return status.Error(codes.Internal, "failed to adapt request")
			}
		}
	}
	return nil
}

func (s *Server) publicGRPCMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s == nil || r == nil {
			next.ServeHTTP(w, r)
			return
		}
		if !isGRPCRequest(r) || s.hostServiceRelayToken(r) != "" {
			next.ServeHTTP(w, r)
			return
		}
		if s.publicGRPCHandler == nil {
			writeGRPCTrailersOnly(w, codes.Unauthenticated, "public-grpc-unavailable")
			return
		}
		s.publicGRPCHandler.ServeHTTP(w, r)
	})
}

func stripInternalIdentityMetadata(ctx context.Context) context.Context {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ctx
	}
	if len(md.Get(gestalt.TrustedCallerSubjectMetadataKey)) == 0 {
		return ctx
	}
	copied := md.Copy()
	copied.Delete(gestalt.TrustedCallerSubjectMetadataKey)
	return metadata.NewIncomingContext(ctx, copied)
}
