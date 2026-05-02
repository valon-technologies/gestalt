package gestalt

import (
	"context"
	"errors"
	"io"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// ServeS3Provider starts a gRPC server for an [S3Provider].
func ServeS3Provider(ctx context.Context, provider S3Provider) error {
	return serveProvider(withProviderCloser(ctx, provider), func(srv *grpc.Server) {
		proto.RegisterProviderLifecycleServer(srv, newRuntimeServer(ProviderKindS3, provider))
		proto.RegisterS3Server(srv, s3ProviderServer{provider: provider})
	})
}

type s3ProviderServer struct {
	proto.UnimplementedS3Server
	provider S3Provider
}

func (s s3ProviderServer) HeadObject(ctx context.Context, req *proto.HeadObjectRequest) (*proto.HeadObjectResponse, error) {
	meta, err := s.provider.HeadObject(ctx, objectRefFromProto(req.GetRef()))
	if err != nil {
		return nil, providerRPCError("s3 head object", err)
	}
	return &proto.HeadObjectResponse{Meta: objectMetaToProto(meta)}, nil
}

func (s s3ProviderServer) ReadObject(req *proto.ReadObjectRequest, stream proto.S3_ReadObjectServer) error {
	meta, body, err := s.provider.ReadObject(stream.Context(), objectRefFromProto(req.GetRef()), readOptionsFromProto(req))
	if err != nil {
		return providerRPCError("s3 read object", err)
	}
	if body != nil {
		defer func() { _ = body.Close() }()
	}
	if err := stream.Send(&proto.ReadObjectChunk{Result: &proto.ReadObjectChunk_Meta{Meta: objectMetaToProto(meta)}}); err != nil {
		return err
	}
	if body == nil {
		return nil
	}
	buf := make([]byte, 64*1024)
	for {
		n, readErr := body.Read(buf)
		if n > 0 {
			if err := stream.Send(&proto.ReadObjectChunk{Result: &proto.ReadObjectChunk_Data{Data: append([]byte(nil), buf[:n]...)}}); err != nil {
				return err
			}
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return providerRPCError("s3 read object body", readErr)
		}
	}
}

func (s s3ProviderServer) WriteObject(stream proto.S3_WriteObjectServer) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	open := first.GetOpen()
	if open == nil {
		return status.Error(codes.InvalidArgument, "first message must be WriteObjectOpen")
	}
	pr, pw := io.Pipe()
	done := make(chan error, 1)
	go func() {
		defer close(done)
		for {
			msg, recvErr := stream.Recv()
			if errors.Is(recvErr, io.EOF) {
				done <- pw.Close()
				return
			}
			if recvErr != nil {
				_ = pw.CloseWithError(recvErr)
				done <- recvErr
				return
			}
			if data := msg.GetData(); len(data) > 0 {
				if _, writeErr := pw.Write(data); writeErr != nil {
					done <- writeErr
					return
				}
			}
		}
	}()
	meta, writeErr := s.provider.WriteObject(stream.Context(), objectRefFromProto(open.GetRef()), pr, writeOptionsFromProto(open))
	closeErr := pr.Close()
	recvErr := <-done
	switch {
	case writeErr != nil:
		return providerRPCError("s3 write object", writeErr)
	case closeErr != nil:
		return providerRPCError("s3 write object", closeErr)
	case recvErr != nil:
		return providerRPCError("s3 write object", recvErr)
	}
	return stream.SendAndClose(&proto.WriteObjectResponse{Meta: objectMetaToProto(meta)})
}

func (s s3ProviderServer) DeleteObject(ctx context.Context, req *proto.DeleteObjectRequest) (*emptypb.Empty, error) {
	if err := s.provider.DeleteObject(ctx, objectRefFromProto(req.GetRef())); err != nil {
		return nil, providerRPCError("s3 delete object", err)
	}
	return &emptypb.Empty{}, nil
}

func (s s3ProviderServer) ListObjects(ctx context.Context, req *proto.ListObjectsRequest) (*proto.ListObjectsResponse, error) {
	page, err := s.provider.ListObjects(ctx, ListOptions{
		Bucket:            req.GetBucket(),
		Prefix:            req.GetPrefix(),
		Delimiter:         req.GetDelimiter(),
		ContinuationToken: req.GetContinuationToken(),
		StartAfter:        req.GetStartAfter(),
		MaxKeys:           req.GetMaxKeys(),
	})
	if err != nil {
		return nil, providerRPCError("s3 list objects", err)
	}
	return listPageToProto(page), nil
}

func (s s3ProviderServer) CopyObject(ctx context.Context, req *proto.CopyObjectRequest) (*proto.CopyObjectResponse, error) {
	meta, err := s.provider.CopyObject(ctx, objectRefFromProto(req.GetSource()), objectRefFromProto(req.GetDestination()), &CopyOptions{
		IfMatch:     req.GetIfMatch(),
		IfNoneMatch: req.GetIfNoneMatch(),
	})
	if err != nil {
		return nil, providerRPCError("s3 copy object", err)
	}
	return &proto.CopyObjectResponse{Meta: objectMetaToProto(meta)}, nil
}

func (s s3ProviderServer) PresignObject(ctx context.Context, req *proto.PresignObjectRequest) (*proto.PresignObjectResponse, error) {
	result, err := s.provider.PresignObject(ctx, objectRefFromProto(req.GetRef()), presignOptionsFromProto(req))
	if err != nil {
		return nil, providerRPCError("s3 presign object", err)
	}
	return presignResultToProto(result), nil
}
