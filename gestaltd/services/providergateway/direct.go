package providergateway

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// directHandlerContext converts the caller's outgoing metadata into the
// handler's incoming metadata (as a real gRPC server would) and strips the
// caller's own incoming metadata so it cannot leak through.
func directHandlerContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	var incoming metadata.MD
	if md, ok := metadata.FromOutgoingContext(ctx); ok {
		incoming = md.Copy()
	}
	ctx = metadata.NewOutgoingContext(ctx, metadata.MD{})
	if incoming != nil {
		ctx = metadata.NewIncomingContext(ctx, incoming)
	}
	return ctx
}

// invokeDirectUnary dispatches a unary RPC through the generated ServiceDesc
// handler with typed, serialization-free message passing. The handler runs in a
// goroutine and the caller's ctx is watched alongside completion so a handler
// that ignores ctx cannot block the caller past its deadline; the goroutine may
// outlive the returned error, as a real server handler would.
func invokeDirectUnary(
	ctx context.Context,
	endpoint DirectEndpoint,
	fullMethod string,
	req any,
	reply any,
	opts []grpc.CallOption,
) error {
	method, err := resolveUnaryMethod(endpoint.Desc, fullMethod)
	if err != nil {
		return err
	}
	return invokeDirectUnaryHandler(ctx, endpoint.Server, method, fullMethod, req, reply, opts)
}

// invokeDirectUnaryHandler dispatches a unary RPC through a pre-resolved method
// descriptor and server. It is the registry-aware dispatch path: the KindRegistry
// resolves (kind, full method) to a method descriptor and server, then hands them
// here. The deadline/cancellation parity contract is identical to invokeDirectUnary.
func invokeDirectUnaryHandler(
	ctx context.Context,
	server any,
	method grpc.MethodDesc,
	fullMethod string,
	req any,
	reply any,
	opts []grpc.CallOption,
) error {
	handlerCtx, transportStream := withDirectUnaryTransportStream(ctx, fullMethod)
	dec := func(target any) error { return assignReply(target, req) }
	type unaryResult struct {
		resp any
		err  error
	}
	resultCh := make(chan unaryResult, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				resultCh <- unaryResult{err: status.Errorf(codes.Internal, "provider gateway: handler panic: %v", r)}
			}
		}()
		resp, err := method.Handler(server, handlerCtx, dec, nil)
		resultCh <- unaryResult{resp: resp, err: err}
	}()
	select {
	case <-ctx.Done():
		return status.FromContextError(ctx.Err()).Err()
	case result := <-resultCh:
		// Re-check after completion so a late result cannot win against an
		// already-expired ctx.
		if err := ctx.Err(); err != nil {
			return status.FromContextError(err).Err()
		}
		applyUnaryCallOptions(opts, transportStream)
		if result.err != nil {
			return result.err
		}
		return assignReply(reply, result.resp)
	}
}

func resolveUnaryMethod(desc *grpc.ServiceDesc, fullMethod string) (grpc.MethodDesc, error) {
	var zero grpc.MethodDesc
	if desc == nil {
		return zero, status.Error(codes.Internal, "provider gateway: service desc is required")
	}
	service, methodName := splitFullMethod(fullMethod)
	if service == "" || methodName == "" || service != desc.ServiceName {
		return zero, status.Errorf(codes.Unimplemented, "provider gateway: unknown method %q", fullMethod)
	}
	for _, method := range desc.Methods {
		if method.MethodName == methodName {
			return method, nil
		}
	}
	return zero, status.Errorf(codes.Unimplemented, "provider gateway: unknown method %q", fullMethod)
}

// assignReply copies src's value into dst; caller and handler never share a pointer.
func assignReply(dst, src any) error {
	if dst == nil {
		return fmt.Errorf("provider gateway: reply is required")
	}
	if src == nil {
		return nil
	}
	dstVal := reflect.ValueOf(dst)
	srcVal := reflect.ValueOf(src)
	if dstVal.Kind() != reflect.Pointer || srcVal.Kind() != reflect.Pointer {
		return fmt.Errorf("provider gateway: reply and response must be pointers")
	}
	if dstVal.Type() != srcVal.Type() {
		return fmt.Errorf("provider gateway: reply type %T does not match response type %T", dst, src)
	}
	if !dstVal.Elem().CanSet() {
		return fmt.Errorf("provider gateway: reply is not settable")
	}
	dstVal.Elem().Set(srcVal.Elem())
	return nil
}

// directUnaryTransportStream backs grpc.SetHeader/SetTrailer in handlers and
// grpc.Header/Trailer call options for unary RPCs.
type directUnaryTransportStream struct {
	method  string
	mu      sync.Mutex
	header  metadata.MD
	trailer metadata.MD
	sent    bool
}

func (s *directUnaryTransportStream) Method() string { return s.method }

func (s *directUnaryTransportStream) SetHeader(md metadata.MD) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sent {
		return status.Errorf(codes.Internal, "grpc: failed to set send header: already sent")
	}
	s.header = metadata.Join(s.header, md)
	return nil
}

func (s *directUnaryTransportStream) SendHeader(md metadata.MD) error {
	if err := s.SetHeader(md); err != nil {
		return err
	}
	s.mu.Lock()
	s.sent = true
	s.mu.Unlock()
	return nil
}

func (s *directUnaryTransportStream) SetTrailer(md metadata.MD) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.trailer = metadata.Join(s.trailer, md)
	return nil
}

func (s *directUnaryTransportStream) Header() metadata.MD {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.header) == 0 {
		return nil
	}
	return s.header.Copy()
}

func (s *directUnaryTransportStream) Trailer() metadata.MD {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.trailer) == 0 {
		return nil
	}
	return s.trailer.Copy()
}

func withDirectUnaryTransportStream(ctx context.Context, fullMethod string) (context.Context, *directUnaryTransportStream) {
	stream := &directUnaryTransportStream{method: fullMethod}
	return grpc.NewContextWithServerTransportStream(ctx, stream), stream
}

func applyUnaryCallOptions(opts []grpc.CallOption, stream *directUnaryTransportStream) {
	header := stream.Header()
	trailer := stream.Trailer()
	for _, opt := range opts {
		switch o := opt.(type) {
		case grpc.HeaderCallOption:
			if o.HeaderAddr != nil {
				*o.HeaderAddr = header
			}
		case grpc.TrailerCallOption:
			if o.TrailerAddr != nil {
				*o.TrailerAddr = trailer
			}
		}
	}
}
