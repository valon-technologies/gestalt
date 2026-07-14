package s3

//go:generate go run ../../tools/routinggen -grpc ../../rpc/protov1/v1/s3_grpc.pb.go -service S3Server -receiver routingS3Server -binding s3 -package s3 -server-type proto.S3Server -output routing_s3_gen.go
//go:generate go run ../../tools/routinggen -grpc ../../rpc/protov1/v1/s3_grpc.pb.go -service S3ObjectAccessServer -receiver routingS3ObjectAccessServer -binding s3 -package s3 -server-type proto.S3ObjectAccessServer -output routing_s3_object_access_gen.go

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	s3sdk "github.com/valon-technologies/gestalt/sdk/go/s3"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type s3Server struct {
	proto.UnimplementedS3Server
	client      s3sdk.S3
	pluginName  string
	bindingName string
	accessURLs  *ObjectAccessURLManager
}

type ServerOptions struct {
	BindingName string
	AccessURLs  *ObjectAccessURLManager
}

func NewServer(client s3sdk.S3, pluginName string) proto.S3Server {
	return NewServerWithOptions(client, pluginName, ServerOptions{})
}

func NewServerWithOptions(client s3sdk.S3, pluginName string, opts ServerOptions) proto.S3Server {
	return &s3Server{
		client:      client,
		pluginName:  strings.TrimSpace(pluginName),
		bindingName: strings.TrimSpace(opts.BindingName),
		accessURLs:  opts.AccessURLs,
	}
}

type routingS3Server struct {
	proto.UnimplementedS3Server
	servers        map[string]proto.S3Server
	defaultBinding string
}

type routingS3ObjectAccessServer struct {
	proto.UnimplementedS3ObjectAccessServer
	servers        map[string]proto.S3ObjectAccessServer
	defaultBinding string
}

func NewRoutingServers(clients map[string]s3sdk.S3, defaultBinding string, pluginName string, accessURLs *ObjectAccessURLManager) (proto.S3Server, proto.S3ObjectAccessServer) {
	s3Servers := make(map[string]proto.S3Server, len(clients))
	accessServers := make(map[string]proto.S3ObjectAccessServer, len(clients))
	for binding, client := range clients {
		binding = strings.TrimSpace(binding)
		if binding == "" || client == nil {
			continue
		}
		s3Servers[binding] = NewServerWithOptions(client, pluginName, ServerOptions{
			BindingName: binding,
			AccessURLs:  accessURLs,
		})
		accessServers[binding] = NewObjectAccessServer(accessURLs, pluginName, binding)
	}
	defaultBinding = strings.TrimSpace(defaultBinding)
	if defaultBinding == "" && len(s3Servers) == 1 {
		for binding := range s3Servers {
			defaultBinding = binding
		}
	}
	return &routingS3Server{servers: s3Servers, defaultBinding: defaultBinding}, &routingS3ObjectAccessServer{servers: accessServers, defaultBinding: defaultBinding}
}

func (s *routingS3Server) server(ctx context.Context) (proto.S3Server, error) {
	return runtimehost.ResolveBinding(ctx, "s3", s.defaultBinding, s.servers)
}

func (s *routingS3Server) ReadObject(req *proto.ReadObjectRequest, stream proto.S3_ReadObjectServer) error {
	server, err := s.server(stream.Context())
	if err != nil {
		return err
	}
	return server.ReadObject(req, stream)
}

func (s *routingS3Server) WriteObject(stream proto.S3_WriteObjectServer) error {
	server, err := s.server(stream.Context())
	if err != nil {
		return err
	}
	return server.WriteObject(stream)
}

func (s *routingS3ObjectAccessServer) server(ctx context.Context) (proto.S3ObjectAccessServer, error) {
	return runtimehost.ResolveBinding(ctx, "s3", s.defaultBinding, s.servers)
}

func (s *s3Server) HeadObject(ctx context.Context, req *proto.HeadObjectRequest) (*proto.HeadObjectResponse, error) {
	meta, err := s.client.HeadObject(ctx, objectRefFromProto(req.GetRef()))
	if err != nil {
		return nil, s3ToGRPCErr(err)
	}
	return &proto.HeadObjectResponse{Meta: objectMetaToProto(meta)}, nil
}

func (s *s3Server) ReadObject(req *proto.ReadObjectRequest, stream proto.S3_ReadObjectServer) error {
	result, err := s.client.ReadObject(stream.Context(), s.readRequest(req))
	if err != nil {
		return s3ToGRPCErr(err)
	}
	defer func() { _ = result.Body.Close() }()
	if err := stream.Send(&proto.ReadObjectChunk{
		Result: &proto.ReadObjectChunk_Meta{Meta: objectMetaToProto(result.Meta)},
	}); err != nil {
		return err
	}
	buf := make([]byte, 64*1024)
	for {
		n, readErr := result.Body.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			if err := stream.Send(&proto.ReadObjectChunk{
				Result: &proto.ReadObjectChunk_Data{Data: chunk},
			}); err != nil {
				return err
			}
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return s3ToGRPCErr(readErr)
		}
	}
}

func (s *s3Server) WriteObject(stream proto.S3_WriteObjectServer) error {
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
		for {
			msg, err := stream.Recv()
			if err == io.EOF {
				done <- pw.Close()
				return
			}
			if err != nil {
				_ = pw.CloseWithError(err)
				done <- err
				return
			}
			data := msg.GetData()
			if len(data) == 0 {
				continue
			}
			if _, err := pw.Write(data); err != nil {
				_ = pw.CloseWithError(err)
				done <- err
				return
			}
		}
	}()

	meta, err := s.client.WriteObject(stream.Context(), s.writeRequest(open, pr))
	if err != nil {
		_ = pr.CloseWithError(err)
		select {
		case <-done:
		case <-stream.Context().Done():
		}
		return s3ToGRPCErr(err)
	}
	_ = pr.Close()
	sendErr := stream.SendAndClose(&proto.WriteObjectResponse{Meta: objectMetaToProto(meta)})
	var recvErr error
	select {
	case recvErr = <-done:
	default:
	}
	if sendErr != nil {
		return sendErr
	}
	if recvErr != nil &&
		!errors.Is(recvErr, io.ErrClosedPipe) &&
		!errors.Is(recvErr, context.Canceled) &&
		status.Code(recvErr) != codes.Canceled {
		return recvErr
	}
	return nil
}

func (s *s3Server) DeleteObject(ctx context.Context, req *proto.DeleteObjectRequest) (*emptypb.Empty, error) {
	if err := s.client.DeleteObject(ctx, objectRefFromProto(req.GetRef())); err != nil {
		return nil, s3ToGRPCErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *s3Server) ListObjects(ctx context.Context, req *proto.ListObjectsRequest) (*proto.ListObjectsResponse, error) {
	page, err := s.client.ListObjects(ctx, s.listRequest(req))
	if err != nil {
		return nil, s3ToGRPCErr(err)
	}
	resp := &proto.ListObjectsResponse{
		CommonPrefixes:        make([]string, 0, len(page.CommonPrefixes)),
		NextContinuationToken: page.NextContinuationToken,
		HasMore:               page.HasMore,
		Objects:               make([]*proto.S3ObjectMeta, 0, len(page.Objects)),
	}
	resp.CommonPrefixes = append(resp.CommonPrefixes, page.CommonPrefixes...)
	for i := range page.Objects {
		resp.Objects = append(resp.Objects, objectMetaToProto(page.Objects[i]))
	}
	return resp, nil
}

func (s *s3Server) CopyObject(ctx context.Context, req *proto.CopyObjectRequest) (*proto.CopyObjectResponse, error) {
	meta, err := s.client.CopyObject(ctx, s3sdk.CopyRequest{
		Source:      objectRefFromProto(req.GetSource()),
		Destination: objectRefFromProto(req.GetDestination()),
		IfMatch:     req.GetIfMatch(),
		IfNoneMatch: req.GetIfNoneMatch(),
	})
	if err != nil {
		return nil, s3ToGRPCErr(err)
	}
	return &proto.CopyObjectResponse{Meta: objectMetaToProto(meta)}, nil
}

func (s *s3Server) PresignObject(ctx context.Context, req *proto.PresignObjectRequest) (*proto.PresignObjectResponse, error) {
	if s.pluginName != "" {
		if s.accessURLs == nil || s.bindingName == "" {
			return nil, status.Error(codes.FailedPrecondition, "presign is not supported for app s3 bindings")
		}
		result, err := s.accessURLs.MintURL(ObjectAccessURLRequest{
			AppName:            s.pluginName,
			BindingName:        s.bindingName,
			Ref:                objectRefFromProto(req.GetRef()),
			Method:             presignMethodFromProto(req.GetMethod()),
			Expires:            timeDurationSeconds(req.GetExpiresSeconds()),
			ContentType:        req.GetContentType(),
			ContentDisposition: req.GetContentDisposition(),
			Headers:            s3sdk.CloneStringMap(req.GetHeaders()),
		})
		if err != nil {
			return nil, status.Error(codes.FailedPrecondition, err.Error())
		}
		resp := &proto.PresignObjectResponse{
			Url:     result.URL,
			Method:  presignMethodToProto(result.Method),
			Headers: s3sdk.CloneStringMap(result.Headers),
		}
		if !result.ExpiresAt.IsZero() {
			resp.ExpiresAt = timestamppb.New(result.ExpiresAt)
		}
		return resp, nil
	}
	result, err := s.client.PresignObject(ctx, s3sdk.PresignRequest{
		Ref:                objectRefFromProto(req.GetRef()),
		Method:             presignMethodFromProto(req.GetMethod()),
		Expires:            timeDurationSeconds(req.GetExpiresSeconds()),
		ContentType:        req.GetContentType(),
		ContentDisposition: req.GetContentDisposition(),
		Headers:            s3sdk.CloneStringMap(req.GetHeaders()),
	})
	if err != nil {
		return nil, s3ToGRPCErr(err)
	}
	resp := &proto.PresignObjectResponse{
		Url:     result.URL,
		Method:  presignMethodToProto(result.Method),
		Headers: s3sdk.CloneStringMap(result.Headers),
	}
	if !result.ExpiresAt.IsZero() {
		resp.ExpiresAt = timestamppb.New(result.ExpiresAt)
	}
	return resp, nil
}

func (s *s3Server) readRequest(req *proto.ReadObjectRequest) s3sdk.ReadRequest {
	out := s3sdk.ReadRequest{
		Ref:         objectRefFromProto(req.GetRef()),
		Range:       byteRangeFromProto(req.GetRange()),
		IfMatch:     req.GetIfMatch(),
		IfNoneMatch: req.GetIfNoneMatch(),
	}
	if ts := req.GetIfModifiedSince(); ts != nil {
		t := ts.AsTime()
		out.IfModifiedSince = &t
	}
	if ts := req.GetIfUnmodifiedSince(); ts != nil {
		t := ts.AsTime()
		out.IfUnmodifiedSince = &t
	}
	return out
}

func (s *s3Server) writeRequest(open *proto.WriteObjectOpen, body io.Reader) s3sdk.WriteRequest {
	return s3sdk.WriteRequest{
		Ref:                objectRefFromProto(open.GetRef()),
		ContentType:        open.GetContentType(),
		CacheControl:       open.GetCacheControl(),
		ContentDisposition: open.GetContentDisposition(),
		ContentEncoding:    open.GetContentEncoding(),
		ContentLanguage:    open.GetContentLanguage(),
		Metadata:           s3sdk.CloneStringMap(open.GetMetadata()),
		IfMatch:            open.GetIfMatch(),
		IfNoneMatch:        open.GetIfNoneMatch(),
		Body:               body,
	}
}

func (s *s3Server) listRequest(req *proto.ListObjectsRequest) s3sdk.ListRequest {
	return s3sdk.ListRequest{
		Prefix:            req.GetPrefix(),
		Delimiter:         req.GetDelimiter(),
		ContinuationToken: req.GetContinuationToken(),
		StartAfter:        req.GetStartAfter(),
		MaxKeys:           req.GetMaxKeys(),
	}
}

func timeDurationSeconds(seconds int64) time.Duration {
	if seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

func s3ToGRPCErr(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, s3sdk.ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, s3sdk.ErrPreconditionFailed):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, s3sdk.ErrInvalidRange):
		return status.Error(codes.OutOfRange, err.Error())
	default:
		return status.Error(codes.Internal, err.Error())
	}
}
