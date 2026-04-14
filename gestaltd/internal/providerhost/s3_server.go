package providerhost

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	s3store "github.com/valon-technologies/gestalt/server/core/s3"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type s3Server struct {
	proto.UnimplementedS3Server
	client    s3store.Client
	keyPrefix string
}

const s3ContinuationTokenPrefix = "gestalt_s3_ct_"

func NewS3Server(client s3store.Client, pluginName string) proto.S3Server {
	return &s3Server{
		client:    client,
		keyPrefix: s3NamespacePrefix(pluginName),
	}
}

func (s *s3Server) HeadObject(ctx context.Context, req *proto.HeadObjectRequest) (*proto.HeadObjectResponse, error) {
	meta, err := s.client.HeadObject(ctx, s.namespacedRef(objectRefFromProto(req.GetRef())))
	if err != nil {
		return nil, s3ToGRPCErr(err)
	}
	meta, err = s.requireOwnedMetaNamespace(meta)
	if err != nil {
		return nil, err
	}
	return &proto.HeadObjectResponse{Meta: objectMetaToProto(meta)}, nil
}

func (s *s3Server) ReadObject(req *proto.ReadObjectRequest, stream proto.S3_ReadObjectServer) error {
	result, err := s.client.ReadObject(stream.Context(), s.namespacedReadRequest(req))
	if err != nil {
		return s3ToGRPCErr(err)
	}
	defer func() { _ = result.Body.Close() }()
	meta, err := s.requireOwnedMetaNamespace(result.Meta)
	if err != nil {
		return err
	}
	if err := stream.Send(&proto.ReadObjectChunk{
		Result: &proto.ReadObjectChunk_Meta{Meta: objectMetaToProto(meta)},
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

	meta, err := s.client.WriteObject(stream.Context(), s.namespacedWriteRequest(open, pr))
	if err != nil {
		_ = pr.CloseWithError(err)
		return s3ToGRPCErr(err)
	}
	meta, err = s.requireOwnedMetaNamespace(meta)
	if err != nil {
		_ = pr.CloseWithError(err)
		return err
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
	if err := s.client.DeleteObject(ctx, s.namespacedRef(objectRefFromProto(req.GetRef()))); err != nil {
		return nil, s3ToGRPCErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *s3Server) ListObjects(ctx context.Context, req *proto.ListObjectsRequest) (*proto.ListObjectsResponse, error) {
	listReq, err := s.namespacedListRequest(req)
	if err != nil {
		return nil, err
	}
	page, err := s.client.ListObjects(ctx, listReq)
	if err != nil {
		return nil, s3ToGRPCErr(err)
	}
	resp := &proto.ListObjectsResponse{
		CommonPrefixes:        make([]string, 0, len(page.CommonPrefixes)),
		NextContinuationToken: s.wrapContinuationToken(page.NextContinuationToken),
		HasMore:               page.HasMore,
		Objects:               make([]*proto.S3ObjectMeta, 0, len(page.Objects)),
	}
	for i := range page.CommonPrefixes {
		if prefix, ok := s.stripOwnedKeyNamespace(page.CommonPrefixes[i]); ok {
			resp.CommonPrefixes = append(resp.CommonPrefixes, prefix)
		}
	}
	for i := range page.Objects {
		if obj, ok := s.stripOwnedMetaNamespace(page.Objects[i]); ok {
			resp.Objects = append(resp.Objects, objectMetaToProto(obj))
		}
	}
	return resp, nil
}

func (s *s3Server) CopyObject(ctx context.Context, req *proto.CopyObjectRequest) (*proto.CopyObjectResponse, error) {
	meta, err := s.client.CopyObject(ctx, s3store.CopyRequest{
		Source:      s.namespacedRef(objectRefFromProto(req.GetSource())),
		Destination: s.namespacedRef(objectRefFromProto(req.GetDestination())),
		IfMatch:     req.GetIfMatch(),
		IfNoneMatch: req.GetIfNoneMatch(),
	})
	if err != nil {
		return nil, s3ToGRPCErr(err)
	}
	meta, err = s.requireOwnedMetaNamespace(meta)
	if err != nil {
		return nil, err
	}
	return &proto.CopyObjectResponse{Meta: objectMetaToProto(meta)}, nil
}

func (s *s3Server) PresignObject(ctx context.Context, req *proto.PresignObjectRequest) (*proto.PresignObjectResponse, error) {
	if s.keyPrefix != "" {
		return nil, status.Error(codes.FailedPrecondition, "presign is not supported for plugin-scoped s3 bindings")
	}
	result, err := s.client.PresignObject(ctx, s3store.PresignRequest{
		Ref:                s.namespacedRef(objectRefFromProto(req.GetRef())),
		Method:             presignMethodFromProto(req.GetMethod()),
		Expires:            timeDurationSeconds(req.GetExpiresSeconds()),
		ContentType:        req.GetContentType(),
		ContentDisposition: req.GetContentDisposition(),
		Headers:            s3store.CloneStringMap(req.GetHeaders()),
	})
	if err != nil {
		return nil, s3ToGRPCErr(err)
	}
	resp := &proto.PresignObjectResponse{
		Url:     result.URL,
		Method:  presignMethodToProto(result.Method),
		Headers: s3store.CloneStringMap(result.Headers),
	}
	if !result.ExpiresAt.IsZero() {
		resp.ExpiresAt = timestamppb.New(result.ExpiresAt)
	}
	return resp, nil
}

func (s *s3Server) namespacedRef(ref s3store.ObjectRef) s3store.ObjectRef {
	ref.Key = s.applyKeyNamespace(ref.Key)
	return ref
}

func (s *s3Server) stripOwnedMetaNamespace(meta s3store.ObjectMeta) (s3store.ObjectMeta, bool) {
	key, ok := s.stripOwnedKeyNamespace(meta.Ref.Key)
	if !ok {
		return s3store.ObjectMeta{}, false
	}
	meta.Ref.Key = key
	return meta, true
}

func (s *s3Server) requireOwnedMetaNamespace(meta s3store.ObjectMeta) (s3store.ObjectMeta, error) {
	meta, ok := s.stripOwnedMetaNamespace(meta)
	if !ok {
		return s3store.ObjectMeta{}, status.Error(codes.Internal, "s3 provider returned object outside plugin namespace")
	}
	return meta, nil
}

func (s *s3Server) applyKeyNamespace(key string) string {
	return s.keyPrefix + key
}

func (s *s3Server) stripOwnedKeyNamespace(key string) (string, bool) {
	if s.keyPrefix == "" {
		return key, true
	}
	if !strings.HasPrefix(key, s.keyPrefix) {
		return "", false
	}
	return strings.TrimPrefix(key, s.keyPrefix), true
}

func (s *s3Server) namespacedReadRequest(req *proto.ReadObjectRequest) s3store.ReadRequest {
	out := s3store.ReadRequest{
		Ref:         s.namespacedRef(objectRefFromProto(req.GetRef())),
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

func (s *s3Server) namespacedWriteRequest(open *proto.WriteObjectOpen, body io.Reader) s3store.WriteRequest {
	return s3store.WriteRequest{
		Ref:                s.namespacedRef(objectRefFromProto(open.GetRef())),
		ContentType:        open.GetContentType(),
		CacheControl:       open.GetCacheControl(),
		ContentDisposition: open.GetContentDisposition(),
		ContentEncoding:    open.GetContentEncoding(),
		ContentLanguage:    open.GetContentLanguage(),
		Metadata:           s3store.CloneStringMap(open.GetMetadata()),
		IfMatch:            open.GetIfMatch(),
		IfNoneMatch:        open.GetIfNoneMatch(),
		Body:               body,
	}
}

func (s *s3Server) namespacedListRequest(req *proto.ListObjectsRequest) (s3store.ListRequest, error) {
	continuationToken, err := s.unwrapContinuationToken(req.GetContinuationToken())
	if err != nil {
		return s3store.ListRequest{}, err
	}
	startAfter := req.GetStartAfter()
	if startAfter != "" {
		startAfter = s.applyKeyNamespace(startAfter)
	}
	return s3store.ListRequest{
		Bucket:            req.GetBucket(),
		Prefix:            s.applyKeyNamespace(req.GetPrefix()),
		Delimiter:         req.GetDelimiter(),
		ContinuationToken: continuationToken,
		StartAfter:        startAfter,
		MaxKeys:           req.GetMaxKeys(),
	}, nil
}

func s3NamespacePrefix(pluginName string) string {
	if pluginName == "" {
		return ""
	}
	return "plugin_" + strconv.Itoa(len(pluginName)) + "_" + pluginName + "/"
}

func (s *s3Server) wrapContinuationToken(token string) string {
	if token == "" || s.keyPrefix == "" {
		return token
	}
	return s3ContinuationTokenPrefix + base64.RawURLEncoding.EncodeToString([]byte(token))
}

func (s *s3Server) unwrapContinuationToken(token string) (string, error) {
	if token == "" || s.keyPrefix == "" {
		return token, nil
	}
	if !strings.HasPrefix(token, s3ContinuationTokenPrefix) {
		return token, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(token, s3ContinuationTokenPrefix))
	if err != nil {
		return "", status.Error(codes.InvalidArgument, "invalid continuation token")
	}
	return string(decoded), nil
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
	case errors.Is(err, s3store.ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, s3store.ErrPreconditionFailed):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, s3store.ErrInvalidRange):
		return status.Error(codes.OutOfRange, err.Error())
	default:
		return status.Error(codes.Internal, err.Error())
	}
}
