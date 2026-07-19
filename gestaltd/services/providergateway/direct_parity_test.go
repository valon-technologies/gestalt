package providergateway

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	protoV1 "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// testKind is a PG-1 test-local provider kind.
const testKind ProviderKind = "test"

const parityServiceName = "gestalt.providergateway.parity.v1.Parity"

var parityUnaryMethod = "/" + parityServiceName + "/UnaryEcho"

type parityRequest = wrapperspb.StringValue
type parityResponse = protoV1.HelloWorldResponse

// ParityServer is the interface grpc.Server requires for HandlerType.
type ParityServer interface {
	UnaryEcho(context.Context, *parityRequest) (*parityResponse, error)
}

type parityServer struct {
	unary func(context.Context, *parityRequest) (*parityResponse, error)
}

func (s *parityServer) UnaryEcho(ctx context.Context, req *parityRequest) (*parityResponse, error) {
	if s.unary != nil {
		return s.unary(ctx, req)
	}
	return &parityResponse{Message: req.GetValue()}, nil
}

var parityServiceDesc = grpc.ServiceDesc{
	ServiceName: parityServiceName,
	HandlerType: (*ParityServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "UnaryEcho",
			Handler: func(srv any, ctx context.Context, dec func(any) error, _ grpc.UnaryServerInterceptor) (any, error) {
				in := new(parityRequest)
				if err := dec(in); err != nil {
					return nil, err
				}
				return srv.(*parityServer).UnaryEcho(ctx, in)
			},
		},
	},
	Streams: []grpc.StreamDesc{},
}

func setupParityGateway(t *testing.T, server *parityServer) (Gateway, ProviderTarget) {
	t.Helper()
	registry := NewLocalRegistry()
	target := ProviderTarget{Kind: testKind, Name: "parity"}
	if err := registry.RegisterDirect(target, DirectEndpoint{
		Desc:   &parityServiceDesc,
		Server: server,
	}); err != nil {
		t.Fatalf("RegisterDirect: %v", err)
	}
	return NewRoutingGateway(registry, nil), target
}

func newParityBaselineConn(t *testing.T, server *parityServer) *publicrpc.InProcessConn {
	t.Helper()
	grpcServer := grpc.NewServer()
	grpcServer.RegisterService(&parityServiceDesc, server)
	conn, err := publicrpc.NewInProcessConn(grpcServer)
	if err != nil {
		t.Fatalf("NewInProcessConn: %v", err)
	}
	t.Cleanup(conn.Close)
	return conn
}

type marshalFailsRequest struct {
	Message string
}

func (m *marshalFailsRequest) Reset()         { *m = marshalFailsRequest{} }
func (m *marshalFailsRequest) String() string { return fmt.Sprintf("message:%q", m.Message) }
func (*marshalFailsRequest) ProtoMessage()    {}
func (m *marshalFailsRequest) ProtoReflect() protoreflect.Message {
	panic("marshalFailsRequest.ProtoReflect is not used in direct route tests")
}

func TestDirectRouteUnaryDoesNotMarshal(t *testing.T) {
	t.Parallel()

	registry := NewLocalRegistry()
	target := ProviderTarget{Kind: testKind, Name: "marshal-free"}
	desc := parityServiceDesc
	desc.Methods = []grpc.MethodDesc{
		{
			MethodName: "UnaryEcho",
			Handler: func(_ any, _ context.Context, dec func(any) error, _ grpc.UnaryServerInterceptor) (any, error) {
				in := new(marshalFailsRequest)
				if err := dec(in); err != nil {
					return nil, err
				}
				return &parityResponse{Message: in.Message}, nil
			},
		},
	}
	if err := registry.RegisterDirect(target, DirectEndpoint{Desc: &desc, Server: &parityServer{}}); err != nil {
		t.Fatalf("RegisterDirect: %v", err)
	}
	req := &marshalFailsRequest{Message: "marshal-free"}
	var resp parityResponse
	if err := NewRoutingGateway(registry, nil).Conn(target).Invoke(context.Background(), parityUnaryMethod, req, &resp); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if resp.GetMessage() != req.Message {
		t.Fatalf("Message = %q, want %q", resp.GetMessage(), req.Message)
	}
}

func TestDirectRouteUnaryMetadataParity(t *testing.T) {
	t.Parallel()

	const outgoingKey = "x-outgoing-token"
	const leakingKey = "x-leaking-incoming"
	want := "parity-outgoing"

	observe := func(ctx context.Context, _ *parityRequest) (*parityResponse, error) {
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return &parityResponse{Message: ""}, nil
		}
		return &parityResponse{Message: md.Get(outgoingKey)[0]}, nil
	}
	server := &parityServer{unary: observe}
	gateway, target := setupParityGateway(t, server)
	baseline := newParityBaselineConn(t, server)

	// Caller ctx carries outgoing + incoming metadata; only outgoing reaches the handler.
	outgoingCtx := metadata.NewOutgoingContext(
		metadata.NewIncomingContext(context.Background(), metadata.Pairs(leakingKey, "leak")),
		metadata.Pairs(outgoingKey, want),
	)

	var directResp, baselineResp parityResponse
	if err := gateway.Conn(target).Invoke(outgoingCtx, parityUnaryMethod, &parityRequest{Value: "x"}, &directResp); err != nil {
		t.Fatalf("direct Invoke: %v", err)
	}
	if err := baseline.ClientConn().Invoke(outgoingCtx, parityUnaryMethod, &parityRequest{Value: "x"}, &baselineResp); err != nil {
		t.Fatalf("baseline Invoke: %v", err)
	}
	if directResp.GetMessage() != want {
		t.Fatalf("direct route incoming[%q] = %q, want %q", outgoingKey, directResp.GetMessage(), want)
	}
	if baselineResp.GetMessage() != want {
		t.Fatalf("baseline incoming[%q] = %q, want %q", outgoingKey, baselineResp.GetMessage(), want)
	}
	if directResp.GetMessage() != baselineResp.GetMessage() {
		t.Fatalf("metadata mismatch: direct %q vs baseline %q", directResp.GetMessage(), baselineResp.GetMessage())
	}
}

func TestDirectRouteUnaryHeaderTrailerParity(t *testing.T) {
	t.Parallel()

	const headerKey = "x-response-header"
	const trailerKey = "x-response-trailer"
	const headerVal = "header-value"
	const trailerVal = "trailer-value"
	server := &parityServer{
		unary: func(ctx context.Context, req *parityRequest) (*parityResponse, error) {
			if err := grpc.SetHeader(ctx, metadata.Pairs(headerKey, headerVal)); err != nil {
				return nil, err
			}
			if err := grpc.SetTrailer(ctx, metadata.Pairs(trailerKey, trailerVal)); err != nil {
				return nil, err
			}
			return &parityResponse{Message: req.GetValue()}, nil
		},
	}
	gateway, target := setupParityGateway(t, server)
	baseline := newParityBaselineConn(t, server)

	ctx := context.Background()
	var directHeader, directTrailer, baselineHeader, baselineTrailer metadata.MD
	var directResp, baselineResp parityResponse
	if err := gateway.Conn(target).Invoke(ctx, parityUnaryMethod, &parityRequest{Value: "parity"},
		&directResp, grpc.Header(&directHeader), grpc.Trailer(&directTrailer)); err != nil {
		t.Fatalf("direct Invoke: %v", err)
	}
	if err := baseline.ClientConn().Invoke(ctx, parityUnaryMethod, &parityRequest{Value: "parity"},
		&baselineResp, grpc.Header(&baselineHeader), grpc.Trailer(&baselineTrailer)); err != nil {
		t.Fatalf("baseline Invoke: %v", err)
	}
	if fmt.Sprint(directHeader.Get(headerKey)) != fmt.Sprint(baselineHeader.Get(headerKey)) {
		t.Fatalf("header mismatch: direct %#v vs baseline %#v", directHeader.Get(headerKey), baselineHeader.Get(headerKey))
	}
	if fmt.Sprint(directTrailer.Get(trailerKey)) != fmt.Sprint(baselineTrailer.Get(trailerKey)) {
		t.Fatalf("trailer mismatch: direct %#v vs baseline %#v", directTrailer.Get(trailerKey), baselineTrailer.Get(trailerKey))
	}
}

func TestDirectRouteUnaryDeadlineParity(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	server := &parityServer{
		unary: func(context.Context, *parityRequest) (*parityResponse, error) {
			<-release
			return &parityResponse{Message: "late"}, nil
		},
	}
	gateway, target := setupParityGateway(t, server)
	baseline := newParityBaselineConn(t, server)
	t.Cleanup(func() { close(release) })

	directCtx, directCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer directCancel()
	baselineCtx, baselineCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer baselineCancel()

	var directResp, baselineResp parityResponse
	directErr := gateway.Conn(target).Invoke(directCtx, parityUnaryMethod, &parityRequest{Value: "slow"}, &directResp)
	baselineErr := baseline.ClientConn().Invoke(baselineCtx, parityUnaryMethod, &parityRequest{Value: "slow"}, &baselineResp)

	if status.Code(directErr) != codes.DeadlineExceeded {
		t.Fatalf("direct err = %v, want DeadlineExceeded", directErr)
	}
	if status.Code(baselineErr) != codes.DeadlineExceeded {
		t.Fatalf("baseline err = %v, want DeadlineExceeded", baselineErr)
	}
}

func TestDirectRouteUnaryCancelParity(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	server := &parityServer{
		unary: func(context.Context, *parityRequest) (*parityResponse, error) {
			<-release
			return &parityResponse{Message: "late"}, nil
		},
	}
	gateway, target := setupParityGateway(t, server)
	baseline := newParityBaselineConn(t, server)
	t.Cleanup(func() { close(release) })

	directCtx, directCancel := context.WithCancel(context.Background())
	baselineCtx, baselineCancel := context.WithCancel(context.Background())

	var directResp, baselineResp parityResponse
	directDone := make(chan error, 1)
	baselineDone := make(chan error, 1)
	go func() {
		directDone <- gateway.Conn(target).Invoke(directCtx, parityUnaryMethod, &parityRequest{Value: "slow"}, &directResp)
	}()
	go func() {
		baselineDone <- baseline.ClientConn().Invoke(baselineCtx, parityUnaryMethod, &parityRequest{Value: "slow"}, &baselineResp)
	}()
	// Let both calls block on the handler before canceling.
	time.Sleep(20 * time.Millisecond)
	directCancel()
	baselineCancel()

	directErr := <-directDone
	baselineErr := <-baselineDone
	if status.Code(directErr) != codes.Canceled {
		t.Fatalf("direct err = %v, want Canceled", directErr)
	}
	if status.Code(baselineErr) != codes.Canceled {
		t.Fatalf("baseline err = %v, want Canceled", baselineErr)
	}
}

func TestDirectRouteUnaryStatusDetailsParity(t *testing.T) {
	t.Parallel()

	detail := &protoV1.HelloWorldResponse{Message: "detail-payload"}
	handlerStatus, detailErr := status.New(codes.FailedPrecondition, "parity failure").WithDetails(detail)
	if detailErr != nil {
		t.Fatalf("WithDetails: %v", detailErr)
	}
	server := &parityServer{
		unary: func(context.Context, *parityRequest) (*parityResponse, error) {
			return nil, handlerStatus.Err()
		},
	}
	gateway, target := setupParityGateway(t, server)

	var resp parityResponse
	invokeErr := gateway.Conn(target).Invoke(context.Background(), parityUnaryMethod, &parityRequest{Value: "x"}, &resp)
	if status.Code(invokeErr) != codes.FailedPrecondition {
		t.Fatalf("direct err = %v, want FailedPrecondition", invokeErr)
	}
	st, ok := status.FromError(invokeErr)
	if !ok {
		t.Fatalf("direct err is not a status: %v", invokeErr)
	}
	if len(st.Details()) != 1 {
		t.Fatalf("direct details len = %d, want 1", len(st.Details()))
	}
	if got, ok := st.Details()[0].(*protoV1.HelloWorldResponse); !ok || got.GetMessage() != detail.GetMessage() {
		t.Fatalf("direct details[0] = %#v, want %q", st.Details()[0], detail.GetMessage())
	}
}

func TestDirectRouteMissingTargetReturnsNotFound(t *testing.T) {
	t.Parallel()

	target := ProviderTarget{Kind: testKind, Name: "missing"}
	var resp parityResponse
	err := NewRoutingGateway(NewLocalRegistry(), nil).Conn(target).Invoke(context.Background(), parityUnaryMethod, &parityRequest{}, &resp)
	if status.Code(err) != codes.NotFound {
		t.Fatalf("status.Code(err) = %v, want NotFound (%v)", status.Code(err), err)
	}
}

func TestRoutingGatewayAuthorization(t *testing.T) {
	t.Parallel()

	registry := NewLocalRegistry()
	parityTarget := ProviderTarget{Kind: testKind, Name: "parity"}
	authzTarget := ProviderTarget{Kind: ProviderKindAuthorization, Name: "authz-primary"}
	for _, target := range []ProviderTarget{parityTarget, authzTarget} {
		if err := registry.RegisterDirect(target, DirectEndpoint{
			Desc:   &parityServiceDesc,
			Server: &parityServer{},
		}); err != nil {
			t.Fatalf("RegisterDirect(%s): %v", target.Name, err)
		}
	}
	authorization := &stubAuthorizationProvider{allowedResult: boolPtr(false)}
	gateway := NewRoutingGateway(registry, authorization)
	ctx := principal.WithPrincipal(context.Background(), &principal.Principal{SubjectID: "user:alice"})
	var resp parityResponse

	err := gateway.Conn(authzTarget).Invoke(ctx, parityUnaryMethod, &parityRequest{Value: "ok"}, &resp)
	if err != nil {
		t.Fatalf("authorization target Invoke: %v", err)
	}
	if authorization.called {
		t.Fatal("authorization provider called for authorization target")
	}

	err = gateway.Conn(parityTarget).Invoke(ctx, parityUnaryMethod, &parityRequest{Value: "ok"}, &resp)
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("status.Code(err) = %v, want PermissionDenied (%v)", status.Code(err), err)
	}
	if !authorization.called {
		t.Fatal("authorization provider not called for non-authorization target")
	}
}

func TestDirectRouteNewStreamUnimplemented(t *testing.T) {
	t.Parallel()

	gateway, target := setupParityGateway(t, &parityServer{})
	stream, err := gateway.Conn(target).NewStream(
		context.Background(),
		&grpc.StreamDesc{ServerStreams: true, ClientStreams: true},
		parityUnaryMethod,
	)
	if stream != nil {
		t.Fatalf("NewStream stream = %v, want nil", stream)
	}
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("status.Code(err) = %v, want Unimplemented (%v)", status.Code(err), err)
	}
	msg := status.Convert(err).Message()
	if !strings.Contains(msg, "PG-5") || !strings.Contains(msg, "issue-148") {
		t.Fatalf("NewStream error message = %q, want substrings PG-5 and issue-148", msg)
	}
}
