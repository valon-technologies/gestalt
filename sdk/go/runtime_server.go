package gestalt

import (
	"context"
	"fmt"
	"os"
	"sync"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"
)

// RuntimeServer adapts a [Runtime] implementation to the gRPC
// RuntimePlugin service. Most plugin authors should use [ServeRuntime]
// instead of constructing this directly.
type RuntimeServer struct {
	proto.UnimplementedRuntimePluginServer
	runtime Runtime
	mu      sync.Mutex
	host    *runtimeHostClient
}

// NewRuntimeServer wraps a [Runtime] in a [RuntimeServer] ready to be
// registered on a gRPC server.
func NewRuntimeServer(runtime Runtime) *RuntimeServer {
	return &RuntimeServer{runtime: runtime}
}

func (s *RuntimeServer) Start(ctx context.Context, req *proto.StartRuntimeRequest) (*emptypb.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.host != nil {
		_ = s.host.conn.Close()
		s.host = nil
	}

	socket := os.Getenv(proto.EnvRuntimeHostSocket)
	if socket == "" {
		return nil, status.Errorf(codes.FailedPrecondition, "%s is required", proto.EnvRuntimeHostSocket)
	}
	conn, err := dialUnixSocket(ctx, socket)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "dial runtime host: %v", err)
	}

	s.host = &runtimeHostClient{
		client: proto.NewRuntimeHostClient(conn),
		conn:   conn,
	}

	caps := capabilitiesFromProto(req.GetInitialCapabilities())
	if err := s.runtime.Start(ctx, req.GetName(), mapFromStruct(req.GetConfig()), caps, s.host); err != nil {
		_ = conn.Close()
		s.host = nil
		return nil, status.Errorf(codes.Unknown, "start runtime: %v", err)
	}
	return &emptypb.Empty{}, nil
}

func (s *RuntimeServer) Stop(ctx context.Context, _ *emptypb.Empty) (*emptypb.Empty, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	err := s.runtime.Stop(ctx)
	if s.host != nil {
		_ = s.host.conn.Close()
		s.host = nil
	}
	if err != nil {
		return nil, status.Errorf(codes.Unknown, "stop runtime: %v", err)
	}
	return &emptypb.Empty{}, nil
}

type runtimeHostClient struct {
	client proto.RuntimeHostClient
	conn   *grpc.ClientConn
}

func (c *runtimeHostClient) Invoke(ctx context.Context, p Principal, provider, instance, operation string, params map[string]any) (*OperationResult, error) {
	var pbParams *structpb.Struct
	if len(params) > 0 {
		var err error
		pbParams, err = structpb.NewStruct(params)
		if err != nil {
			return nil, fmt.Errorf("encode params: %w", err)
		}
	}
	resp, err := c.client.Invoke(ctx, &proto.InvokeRequest{
		Principal: principalToProto(p),
		Provider:  provider,
		Instance:  instance,
		Operation: operation,
		Params:    pbParams,
	})
	if err != nil {
		return nil, err
	}
	return &OperationResult{
		Status: int(resp.GetStatus()),
		Body:   resp.GetBody(),
	}, nil
}

func (c *runtimeHostClient) ListCapabilities(ctx context.Context) ([]Capability, error) {
	resp, err := c.client.ListCapabilities(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	return capabilitiesFromProto(resp.GetCapabilities()), nil
}
