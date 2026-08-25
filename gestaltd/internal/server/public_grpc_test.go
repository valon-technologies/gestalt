package server

import (
	"context"
	"testing"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/providergateway"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func TestStripInternalIdentityMetadataRemovesForgedCallerIdentity(t *testing.T) {
	t.Parallel()

	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		gestalt.TrustedCallerSubjectMetadataKey, "user:bob",
		gestalt.CallerBearerTokenMetadataKey, "token-for-bob",
	))
	ctx = principal.WithPrincipal(ctx, &principal.Principal{SubjectID: "user:alice"})

	stripped := stripInternalIdentityMetadata(ctx)
	md, ok := metadata.FromIncomingContext(stripped)
	if !ok {
		t.Fatal("expected incoming metadata")
	}
	if len(md.Get(gestalt.TrustedCallerSubjectMetadataKey)) != 0 {
		t.Fatalf("forged caller metadata = %v, want stripped", md.Get(gestalt.TrustedCallerSubjectMetadataKey))
	}
	if len(md.Get(gestalt.CallerBearerTokenMetadataKey)) != 0 {
		t.Fatalf("forged caller bearer metadata = %v, want stripped", md.Get(gestalt.CallerBearerTokenMetadataKey))
	}
}

func TestPublicPrepareUnaryInterceptorSanitizesBeforeProvider(t *testing.T) {
	t.Parallel()

	registry, err := publicrpc.NewGeneratedRegistry()
	if err != nil {
		t.Fatalf("NewGeneratedRegistry: %v", err)
	}
	var providerMetadata metadata.MD
	identity := &coretesting.StubIdentityProvider{
		IntrospectFn: func(ctx context.Context, _ *core.IntrospectRequest) (*core.IntrospectResponse, error) {
			providerMetadata, _ = metadata.FromIncomingContext(ctx)
			return &core.IntrospectResponse{Active: true, Subject: "user:alice"}, nil
		},
	}
	transport := providergateway.NewProviderGatewayTransport()
	transport.SetIdentityProvider(identity)
	transport.SetPublicMethods(registry)

	ctx := publicrpc.WithPublicOrigin(context.Background(), proto.App_Invoke_FullMethodName)
	ctx = metadata.NewIncomingContext(ctx, metadata.Pairs(
		"authorization", "Bearer public-token",
		gestalt.TrustedCallerSubjectMetadataKey, "user:forged",
		gestalt.CallerBearerTokenMetadataKey, "token-forged",
	))
	var handlerMetadata metadata.MD
	var handlerSubject string
	_, err = publicPrepareUnaryInterceptor(transport)(
		ctx,
		&proto.AppInvokeRequest{App: "example", Operation: "sync"},
		&grpc.UnaryServerInfo{FullMethod: proto.App_Invoke_FullMethodName},
		func(ctx context.Context, req any) (any, error) {
			handlerMetadata, _ = metadata.FromIncomingContext(ctx)
			handlerSubject = gestalt.TrustedCallerSubjectFromContext(ctx)
			return req, nil
		},
	)
	if err != nil {
		t.Fatalf("publicPrepareUnaryInterceptor() error = %v", err)
	}
	if len(providerMetadata.Get(gestalt.TrustedCallerSubjectMetadataKey)) != 0 {
		t.Fatalf("provider saw forged caller subject = %v", providerMetadata.Get(gestalt.TrustedCallerSubjectMetadataKey))
	}
	if len(providerMetadata.Get(gestalt.CallerBearerTokenMetadataKey)) != 0 {
		t.Fatalf("provider saw forged caller bearer = %v", providerMetadata.Get(gestalt.CallerBearerTokenMetadataKey))
	}
	if got := providerMetadata.Get("authorization"); len(got) != 1 || got[0] != "Bearer public-token" {
		t.Fatalf("provider authorization = %v, want public bearer", got)
	}
	if len(handlerMetadata.Get(gestalt.TrustedCallerSubjectMetadataKey)) != 0 ||
		len(handlerMetadata.Get(gestalt.CallerBearerTokenMetadataKey)) != 0 {
		t.Fatalf("handler received internal identity metadata: %v", handlerMetadata)
	}
	if handlerSubject != "user:alice" {
		t.Fatalf("handler trusted subject = %q, want user:alice", handlerSubject)
	}
}

type oneMessageServerStream struct {
	ctx  context.Context
	fill func(any) error
}

func (s *oneMessageServerStream) SetHeader(metadata.MD) error  { return nil }
func (s *oneMessageServerStream) SendHeader(metadata.MD) error { return nil }
func (s *oneMessageServerStream) SetTrailer(metadata.MD)       {}
func (s *oneMessageServerStream) Context() context.Context     { return s.ctx }
func (s *oneMessageServerStream) SendMsg(any) error            { return nil }
func (s *oneMessageServerStream) RecvMsg(msg any) error        { return s.fill(msg) }

func TestPublicPrepareStreamSanitizesBeforeProvider(t *testing.T) {
	t.Parallel()

	registry, err := publicrpc.NewGeneratedRegistry()
	if err != nil {
		t.Fatalf("NewGeneratedRegistry: %v", err)
	}
	var providerMetadata metadata.MD
	identity := &coretesting.StubIdentityProvider{
		IntrospectFn: func(ctx context.Context, _ *core.IntrospectRequest) (*core.IntrospectResponse, error) {
			providerMetadata, _ = metadata.FromIncomingContext(ctx)
			return &core.IntrospectResponse{Active: true, Subject: "user:alice"}, nil
		},
	}
	transport := providergateway.NewProviderGatewayTransport()
	transport.SetIdentityProvider(identity)
	transport.SetPublicMethods(registry)
	ctx := publicrpc.WithPublicOrigin(context.Background(), proto.App_Invoke_FullMethodName)
	ctx = metadata.NewIncomingContext(ctx, metadata.Pairs(
		"authorization", "Bearer public-token",
		gestalt.TrustedCallerSubjectMetadataKey, "user:forged",
		gestalt.CallerBearerTokenMetadataKey, "token-forged",
	))
	stream := &publicAuthStream{
		ServerStream: &oneMessageServerStream{
			ctx: ctx,
			fill: func(msg any) error {
				req, ok := msg.(*proto.AppInvokeRequest)
				if !ok {
					t.Fatalf("stream request type = %T, want *proto.AppInvokeRequest", msg)
				}
				*req = proto.AppInvokeRequest{App: "example", Operation: "sync"}
				return nil
			},
		},
		transport:  transport,
		fullMethod: proto.App_Invoke_FullMethodName,
	}
	var req proto.AppInvokeRequest
	if err := stream.RecvMsg(&req); err != nil {
		t.Fatalf("publicAuthStream.RecvMsg() error = %v", err)
	}
	if len(providerMetadata.Get(gestalt.TrustedCallerSubjectMetadataKey)) != 0 {
		t.Fatalf("provider saw forged caller subject = %v", providerMetadata.Get(gestalt.TrustedCallerSubjectMetadataKey))
	}
	if len(providerMetadata.Get(gestalt.CallerBearerTokenMetadataKey)) != 0 {
		t.Fatalf("provider saw forged caller bearer = %v", providerMetadata.Get(gestalt.CallerBearerTokenMetadataKey))
	}
	if got := providerMetadata.Get("authorization"); len(got) != 1 || got[0] != "Bearer public-token" {
		t.Fatalf("provider authorization = %v, want public bearer", got)
	}
}
