package runtimehost

import (
	"context"

	"google.golang.org/grpc"
)

type hostServiceRelayServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *hostServiceRelayServerStream) Context() context.Context {
	return s.ctx
}

// HostServiceGRPCServerOptions returns gRPC server options that verify host-service
// relay capabilities at ingress for both unary and streaming RPCs.
func (m *HostServiceRelayTokenManager) HostServiceGRPCServerOptions() []grpc.ServerOption {
	if m == nil {
		return nil
	}
	return []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(m.UnaryServerInterceptor()),
		grpc.ChainStreamInterceptor(m.StreamServerInterceptor()),
	}
}

func (m *HostServiceRelayTokenManager) UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		ctx, err := m.AuthenticateGRPC(ctx, info.FullMethod)
		if err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

func (m *HostServiceRelayTokenManager) StreamServerInterceptor() grpc.StreamServerInterceptor {
	return func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx, err := m.AuthenticateGRPC(stream.Context(), info.FullMethod)
		if err != nil {
			return err
		}
		if ctx == stream.Context() {
			return handler(srv, stream)
		}
		return handler(srv, &hostServiceRelayServerStream{
			ServerStream: stream,
			ctx:          ctx,
		})
	}
}
