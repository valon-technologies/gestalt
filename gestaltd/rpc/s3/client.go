package s3

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	s3 "github.com/valon-technologies/gestalt/sdk/go/s3"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var _ s3.S3 = (*rpcClient)(nil)

// rpcClient speaks to a running S3 provider over gRPC.
type rpcClient struct {
	grpc               proto.S3Client
	objectAccessClient proto.S3ObjectAccessClient
	opts               Options
}

// Close is a no-op because this client uses shared transport.
func (c *rpcClient) Close() error { return nil }

// Object returns a convenience handle for one object key.
func (c *rpcClient) Object(key string) *s3.ObjectHandleRef {
	return s3.NewObject(c, s3.ObjectRef{Key: key})
}

// ObjectVersion returns a convenience handle for one object version.
func (c *rpcClient) ObjectVersion(key, versionID string) *s3.ObjectHandleRef {
	return s3.NewObject(c, s3.ObjectRef{Key: key, VersionID: versionID})
}

// HeadObject fetches metadata for one object.
func (c *rpcClient) HeadObject(ctx context.Context, ref s3.ObjectRef) (s3.ObjectMeta, error) {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	resp, err := c.grpc.HeadObject(ctx, &proto.HeadObjectRequest{Ref: objectRefToProto(ref)})
	if err != nil {
		return s3.ObjectMeta{}, s3.ClientError(err)
	}
	return requiredObjectMeta(resp.GetMeta(), "head object")
}

// ReadObject implements s3.S3 using request-oriented parameters.
func (c *rpcClient) ReadObject(ctx context.Context, req s3.ReadRequest) (s3.ReadResult, error) {
	meta, body, err := c.readObjectStream(ctx, req)
	if err != nil {
		return s3.ReadResult{}, err
	}
	return s3.ReadResult{Meta: meta, Body: body}, nil
}

func (c *rpcClient) readObjectStream(ctx context.Context, req s3.ReadRequest) (s3.ObjectMeta, io.ReadCloser, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	readCtx, cancel := context.WithCancel(ctx)
	stream, err := c.grpc.ReadObject(readCtx, readRequestToProto(req))
	if err != nil {
		cancel()
		return s3.ObjectMeta{}, nil, s3.ClientError(err)
	}
	first, err := stream.Recv()
	if err != nil {
		cancel()
		return s3.ObjectMeta{}, nil, s3.ClientError(err)
	}
	meta := first.GetMeta()
	if meta == nil {
		cancel()
		return s3.ObjectMeta{}, nil, fmt.Errorf("s3: read stream did not start with metadata")
	}
	return objectMetaFromProto(meta), &s3ReadCloser{stream: stream, cancel: cancel}, nil
}

// WriteObject implements s3.S3.
func (c *rpcClient) WriteObject(ctx context.Context, req s3.WriteRequest) (s3.ObjectMeta, error) {
	return c.writeObjectStream(ctx, req)
}

func (c *rpcClient) writeObjectStream(ctx context.Context, req s3.WriteRequest) (s3.ObjectMeta, error) {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	stream, err := c.grpc.WriteObject(ctx)
	if err != nil {
		return s3.ObjectMeta{}, s3.ClientError(err)
	}
	if err := stream.Send(&proto.WriteObjectRequest{
		Msg: &proto.WriteObjectRequest_Open{Open: writeRequestToProtoOpen(req)},
	}); err != nil {
		return s3.ObjectMeta{}, s3.ClientError(err)
	}
	body := req.Body
	if body == nil {
		body = bytes.NewReader(nil)
	}
	buf := make([]byte, 64*1024)
	for {
		n, readErr := body.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			if err := stream.Send(&proto.WriteObjectRequest{
				Msg: &proto.WriteObjectRequest_Data{Data: chunk},
			}); err != nil {
				return s3.ObjectMeta{}, s3.ClientError(err)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return s3.ObjectMeta{}, readErr
		}
	}
	resp, err := stream.CloseAndRecv()
	if err != nil {
		return s3.ObjectMeta{}, s3.ClientError(err)
	}
	return requiredObjectMeta(resp.GetMeta(), "write object")
}

// DeleteObject removes one object.
func (c *rpcClient) DeleteObject(ctx context.Context, ref s3.ObjectRef) error {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	_, err := c.grpc.DeleteObject(ctx, &proto.DeleteObjectRequest{Ref: objectRefToProto(ref)})
	return s3.ClientError(err)
}

// ListObjects implements s3.S3.
func (c *rpcClient) ListObjects(ctx context.Context, req s3.ListRequest) (s3.ListPage, error) {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	resp, err := c.grpc.ListObjects(ctx, &proto.ListObjectsRequest{
		Prefix:            req.Prefix,
		Delimiter:         req.Delimiter,
		ContinuationToken: req.ContinuationToken,
		StartAfter:        req.StartAfter,
		MaxKeys:           req.MaxKeys,
	})
	if err != nil {
		return s3.ListPage{}, s3.ClientError(err)
	}
	return listPageFromProto(resp), nil
}

// CopyObject implements s3.S3.
func (c *rpcClient) CopyObject(ctx context.Context, req s3.CopyRequest) (s3.ObjectMeta, error) {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	pb := &proto.CopyObjectRequest{
		Source:      objectRefToProto(req.Source),
		Destination: objectRefToProto(req.Destination),
		IfMatch:     req.IfMatch,
		IfNoneMatch: req.IfNoneMatch,
	}
	resp, err := c.grpc.CopyObject(ctx, pb)
	if err != nil {
		return s3.ObjectMeta{}, s3.ClientError(err)
	}
	return requiredObjectMeta(resp.GetMeta(), "copy object")
}

// PresignObject implements s3.S3.
func (c *rpcClient) PresignObject(ctx context.Context, req s3.PresignRequest) (s3.PresignResult, error) {
	return c.presignObject(ctx, presignRequestToProto(req), req.Method)
}

func (c *rpcClient) presignObject(ctx context.Context, req *proto.PresignObjectRequest, requestedMethod s3.PresignMethod) (s3.PresignResult, error) {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	resp, err := c.grpc.PresignObject(ctx, req)
	if err != nil {
		return s3.PresignResult{}, s3.ClientError(err)
	}
	return presignResultFromProto(resp, requestedMethod), nil
}

// CreateObjectAccessURL creates a host-mediated object-access URL.
func (c *rpcClient) CreateObjectAccessURL(ctx context.Context, req s3.PresignRequest) (s3.PresignResult, error) {
	ctx, cancel := c.callCtx(ctx)
	defer cancel()
	resp, err := c.objectAccessClient.CreateObjectAccessURL(ctx, objectAccessURLRequestToProto(req))
	if err != nil {
		return s3.PresignResult{}, s3.ClientError(err)
	}
	return objectAccessURLFromProto(resp, req.Method), nil
}

type s3ReadCloser struct {
	stream  proto.S3_ReadObjectClient
	cancel  context.CancelFunc
	pending []byte
	closed  bool
}

func (r *s3ReadCloser) Read(p []byte) (int, error) {
	if len(r.pending) > 0 {
		n := copy(p, r.pending)
		r.pending = r.pending[n:]
		return n, nil
	}
	if r.closed {
		return 0, io.EOF
	}
	for {
		resp, err := r.stream.Recv()
		if err == io.EOF {
			if r.cancel != nil {
				r.cancel()
				r.cancel = nil
			}
			r.closed = true
			return 0, io.EOF
		}
		if err != nil {
			if r.cancel != nil {
				r.cancel()
				r.cancel = nil
			}
			r.closed = true
			return 0, s3.ClientError(err)
		}
		if data := resp.GetData(); len(data) > 0 {
			n := copy(p, data)
			r.pending = append(r.pending[:0], data[n:]...)
			return n, nil
		}
	}
}

func (r *s3ReadCloser) Close() error {
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	r.pending = nil
	r.closed = true
	return nil
}

func objectRefToProto(ref s3.ObjectRef) *proto.S3ObjectRef {
	return &proto.S3ObjectRef{
		Key:       ref.Key,
		VersionId: ref.VersionID,
	}
}

func objectRefFromProto(ref *proto.S3ObjectRef) s3.ObjectRef {
	if ref == nil {
		return s3.ObjectRef{}
	}
	return s3.ObjectRef{
		Key:       ref.GetKey(),
		VersionID: ref.GetVersionId(),
	}
}

func objectMetaToProto(meta s3.ObjectMeta) *proto.S3ObjectMeta {
	out := &proto.S3ObjectMeta{
		Ref:          objectRefToProto(meta.Ref),
		Etag:         meta.ETag,
		Size:         meta.Size,
		ContentType:  meta.ContentType,
		Metadata:     s3.CloneStringMap(meta.Metadata),
		StorageClass: meta.StorageClass,
	}
	if !meta.LastModified.IsZero() {
		out.LastModified = timestamppb.New(meta.LastModified)
	}
	return out
}

func objectMetaFromProto(meta *proto.S3ObjectMeta) s3.ObjectMeta {
	if meta == nil {
		return s3.ObjectMeta{}
	}
	out := s3.ObjectMeta{
		Ref: s3.ObjectRef{
			Key:       meta.GetRef().GetKey(),
			VersionID: meta.GetRef().GetVersionId(),
		},
		ETag:         meta.GetEtag(),
		Size:         meta.GetSize(),
		ContentType:  meta.GetContentType(),
		Metadata:     s3.CloneStringMap(meta.GetMetadata()),
		StorageClass: meta.GetStorageClass(),
	}
	if ts := meta.GetLastModified(); ts != nil {
		out.LastModified = ts.AsTime()
	}
	return out
}

func requiredObjectMeta(meta *proto.S3ObjectMeta, op string) (s3.ObjectMeta, error) {
	if meta == nil {
		return s3.ObjectMeta{}, fmt.Errorf("s3: %s response missing metadata", op)
	}
	return objectMetaFromProto(meta), nil
}

func byteRangeToProto(r *s3.ByteRange) *proto.ByteRange {
	if r == nil {
		return nil
	}
	out := &proto.ByteRange{}
	if r.Start != nil {
		out.Start = r.Start
	}
	if r.End != nil {
		out.End = r.End
	}
	return out
}

func byteRangeFromProto(r *proto.ByteRange) *s3.ByteRange {
	if r == nil {
		return nil
	}
	out := &s3.ByteRange{}
	if r.Start != nil {
		start := r.GetStart()
		out.Start = &start
	}
	if r.End != nil {
		end := r.GetEnd()
		out.End = &end
	}
	return out
}

func readRequestFromProto(req *proto.ReadObjectRequest) s3.ReadRequest {
	if req == nil {
		return s3.ReadRequest{}
	}
	out := s3.ReadRequest{
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

func readRequestToProto(req s3.ReadRequest) *proto.ReadObjectRequest {
	out := &proto.ReadObjectRequest{
		Ref:         objectRefToProto(req.Ref),
		Range:       byteRangeToProto(req.Range),
		IfMatch:     req.IfMatch,
		IfNoneMatch: req.IfNoneMatch,
	}
	if req.IfModifiedSince != nil {
		out.IfModifiedSince = timestamppb.New(*req.IfModifiedSince)
	}
	if req.IfUnmodifiedSince != nil {
		out.IfUnmodifiedSince = timestamppb.New(*req.IfUnmodifiedSince)
	}
	return out
}

func writeRequestFromProto(open *proto.WriteObjectOpen) s3.WriteRequest {
	if open == nil {
		return s3.WriteRequest{}
	}
	return s3.WriteRequest{
		Ref:                objectRefFromProto(open.GetRef()),
		ContentType:        open.GetContentType(),
		CacheControl:       open.GetCacheControl(),
		ContentDisposition: open.GetContentDisposition(),
		ContentEncoding:    open.GetContentEncoding(),
		ContentLanguage:    open.GetContentLanguage(),
		Metadata:           s3.CloneStringMap(open.GetMetadata()),
		IfMatch:            open.GetIfMatch(),
		IfNoneMatch:        open.GetIfNoneMatch(),
	}
}

func writeRequestToProtoOpen(req s3.WriteRequest) *proto.WriteObjectOpen {
	return &proto.WriteObjectOpen{
		Ref:                objectRefToProto(req.Ref),
		ContentType:        req.ContentType,
		CacheControl:       req.CacheControl,
		ContentDisposition: req.ContentDisposition,
		ContentEncoding:    req.ContentEncoding,
		ContentLanguage:    req.ContentLanguage,
		Metadata:           s3.CloneStringMap(req.Metadata),
		IfMatch:            req.IfMatch,
		IfNoneMatch:        req.IfNoneMatch,
	}
}

func listPageFromProto(resp *proto.ListObjectsResponse) s3.ListPage {
	out := s3.ListPage{
		CommonPrefixes:        append([]string(nil), resp.GetCommonPrefixes()...),
		NextContinuationToken: resp.GetNextContinuationToken(),
		HasMore:               resp.GetHasMore(),
	}
	out.Objects = make([]s3.ObjectMeta, 0, len(resp.GetObjects()))
	for _, obj := range resp.GetObjects() {
		out.Objects = append(out.Objects, objectMetaFromProto(obj))
	}
	return out
}

func listPageToProto(page s3.ListPage) *proto.ListObjectsResponse {
	resp := &proto.ListObjectsResponse{
		CommonPrefixes:        append([]string(nil), page.CommonPrefixes...),
		NextContinuationToken: page.NextContinuationToken,
		HasMore:               page.HasMore,
		Objects:               make([]*proto.S3ObjectMeta, 0, len(page.Objects)),
	}
	for _, obj := range page.Objects {
		resp.Objects = append(resp.Objects, objectMetaToProto(obj))
	}
	return resp
}

func presignMethodToProto(method s3.PresignMethod) proto.PresignMethod {
	switch method {
	case s3.PresignMethodGet:
		return proto.PresignMethod_PRESIGN_METHOD_GET
	case s3.PresignMethodPut:
		return proto.PresignMethod_PRESIGN_METHOD_PUT
	case s3.PresignMethodDelete:
		return proto.PresignMethod_PRESIGN_METHOD_DELETE
	case s3.PresignMethodHead:
		return proto.PresignMethod_PRESIGN_METHOD_HEAD
	default:
		return proto.PresignMethod_PRESIGN_METHOD_UNSPECIFIED
	}
}

func presignRequestFromProto(req *proto.PresignObjectRequest) s3.PresignRequest {
	if req == nil {
		return s3.PresignRequest{}
	}
	return s3.PresignRequest{
		Ref:                objectRefFromProto(req.GetRef()),
		Method:             presignMethodFromProto(req.GetMethod()),
		Expires:            time.Duration(req.GetExpiresSeconds()) * time.Second,
		ContentType:        req.GetContentType(),
		ContentDisposition: req.GetContentDisposition(),
		Headers:            s3.CloneStringMap(req.GetHeaders()),
	}
}

func presignRequestToProto(req s3.PresignRequest) *proto.PresignObjectRequest {
	return &proto.PresignObjectRequest{
		Ref:                objectRefToProto(req.Ref),
		Method:             presignMethodToProto(req.Method),
		ExpiresSeconds:     int64(req.Expires / time.Second),
		ContentType:        req.ContentType,
		ContentDisposition: req.ContentDisposition,
		Headers:            s3.CloneStringMap(req.Headers),
	}
}

func objectAccessURLRequestToProto(req s3.PresignRequest) *proto.CreateObjectAccessURLRequest {
	return &proto.CreateObjectAccessURLRequest{
		Ref:                objectRefToProto(req.Ref),
		Method:             presignMethodToProto(req.Method),
		ExpiresSeconds:     int64(req.Expires / time.Second),
		ContentType:        req.ContentType,
		ContentDisposition: req.ContentDisposition,
		Headers:            s3.CloneStringMap(req.Headers),
	}
}

func presignMethodFromProto(method proto.PresignMethod) s3.PresignMethod {
	switch method {
	case proto.PresignMethod_PRESIGN_METHOD_GET:
		return s3.PresignMethodGet
	case proto.PresignMethod_PRESIGN_METHOD_PUT:
		return s3.PresignMethodPut
	case proto.PresignMethod_PRESIGN_METHOD_DELETE:
		return s3.PresignMethodDelete
	case proto.PresignMethod_PRESIGN_METHOD_HEAD:
		return s3.PresignMethodHead
	default:
		return ""
	}
}

func presignResultToProto(result s3.PresignResult) *proto.PresignObjectResponse {
	resp := &proto.PresignObjectResponse{
		Url:     result.URL,
		Method:  presignMethodToProto(result.Method),
		Headers: s3.CloneStringMap(result.Headers),
	}
	if !result.ExpiresAt.IsZero() {
		resp.ExpiresAt = timestamppb.New(result.ExpiresAt)
	}
	return resp
}

func presignResultFromProto(resp *proto.PresignObjectResponse, requested s3.PresignMethod) s3.PresignResult {
	if resp == nil {
		return s3.PresignResult{}
	}
	method := presignMethodFromProto(resp.GetMethod())
	if method == "" {
		method = requested
	}
	out := s3.PresignResult{
		URL:     resp.GetUrl(),
		Method:  method,
		Headers: s3.CloneStringMap(resp.GetHeaders()),
	}
	if ts := resp.GetExpiresAt(); ts != nil {
		out.ExpiresAt = ts.AsTime()
	}
	return out
}

func objectAccessURLFromProto(resp *proto.CreateObjectAccessURLResponse, requested s3.PresignMethod) s3.PresignResult {
	if resp == nil {
		return s3.PresignResult{}
	}
	method := presignMethodFromProto(resp.GetMethod())
	if method == "" {
		method = requested
	}
	out := s3.PresignResult{
		URL:     resp.GetUrl(),
		Method:  method,
		Headers: s3.CloneStringMap(resp.GetHeaders()),
	}
	if ts := resp.GetExpiresAt(); ts != nil {
		out.ExpiresAt = ts.AsTime()
	}
	return out
}

func firstS3Name(name []string) string {
	if len(name) == 0 {
		return ""
	}
	return name[0]
}
