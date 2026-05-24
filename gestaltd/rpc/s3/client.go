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

var _ s3.Client = (*rpcClient)(nil)

// rpcClient speaks to a running S3 provider over gRPC.
type rpcClient struct {
	grpc               proto.S3Client
	objectAccessClient proto.S3ObjectAccessClient
	opts               Options
}

// Close is a no-op because this client uses shared transport.
func (c *rpcClient) Close() error { return nil }

// Object returns a convenience handle for one object key.
func (c *rpcClient) Object(bucket, key string) *s3.ObjectHandleRef {
	return s3.NewObject(c, s3.ObjectRef{Bucket: bucket, Key: key})
}

// ObjectVersion returns a convenience handle for one object version.
func (c *rpcClient) ObjectVersion(bucket, key, versionID string) *s3.ObjectHandleRef {
	return s3.NewObject(c, s3.ObjectRef{Bucket: bucket, Key: key, VersionID: versionID})
}

// HeadObject fetches metadata for one object.
func (c *rpcClient) HeadObject(ctx context.Context, ref s3.ObjectRef) (s3.ObjectMeta, error) {
	resp, err := c.grpc.HeadObject(ctx, &proto.HeadObjectRequest{Ref: objectRefToProto(ref)})
	if err != nil {
		return s3.ObjectMeta{}, s3.ClientError(err)
	}
	return requiredObjectMeta(resp.GetMeta(), "head object")
}

// ReadObject implements s3.Client using request-oriented parameters.
func (c *rpcClient) ReadObject(ctx context.Context, req s3.ReadRequest) (s3.ReadResult, error) {
	opts := &s3.ReadOptions{
		Range: req.Range, IfMatch: req.IfMatch, IfNoneMatch: req.IfNoneMatch,
		IfModifiedSince: req.IfModifiedSince, IfUnmodifiedSince: req.IfUnmodifiedSince,
	}
	meta, body, err := c.readObjectStream(ctx, req.Ref, opts)
	if err != nil {
		return s3.ReadResult{}, err
	}
	return s3.ReadResult{Meta: meta, Body: body}, nil
}

// readObjectStream opens a streaming object reader (legacy convenience shape).
func (c *rpcClient) readObjectStream(ctx context.Context, ref s3.ObjectRef, opts *s3.ReadOptions) (s3.ObjectMeta, io.ReadCloser, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	readCtx, cancel := context.WithCancel(ctx)
	req := &proto.ReadObjectRequest{
		Ref: objectRefToProto(ref),
	}
	if opts != nil {
		req.Range = byteRangeToProto(opts.Range)
		req.IfMatch = opts.IfMatch
		req.IfNoneMatch = opts.IfNoneMatch
		if opts.IfModifiedSince != nil {
			req.IfModifiedSince = timestamppb.New(*opts.IfModifiedSince)
		}
		if opts.IfUnmodifiedSince != nil {
			req.IfUnmodifiedSince = timestamppb.New(*opts.IfUnmodifiedSince)
		}
	}
	stream, err := c.grpc.ReadObject(readCtx, req)
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

// WriteObject implements s3.Client.
func (c *rpcClient) WriteObject(ctx context.Context, req s3.WriteRequest) (s3.ObjectMeta, error) {
	opts := &s3.WriteOptions{
		ContentType: req.ContentType, CacheControl: req.CacheControl,
		ContentDisposition: req.ContentDisposition, ContentEncoding: req.ContentEncoding,
		ContentLanguage: req.ContentLanguage, Metadata: req.Metadata,
		IfMatch: req.IfMatch, IfNoneMatch: req.IfNoneMatch,
	}
	return c.writeObjectStream(ctx, req.Ref, req.Body, opts)
}

func (c *rpcClient) writeObjectStream(ctx context.Context, ref s3.ObjectRef, body io.Reader, opts *s3.WriteOptions) (s3.ObjectMeta, error) {
	stream, err := c.grpc.WriteObject(ctx)
	if err != nil {
		return s3.ObjectMeta{}, s3.ClientError(err)
	}
	open := &proto.WriteObjectOpen{Ref: objectRefToProto(ref)}
	if opts != nil {
		open.ContentType = opts.ContentType
		open.CacheControl = opts.CacheControl
		open.ContentDisposition = opts.ContentDisposition
		open.ContentEncoding = opts.ContentEncoding
		open.ContentLanguage = opts.ContentLanguage
		open.Metadata = s3.CloneStringMap(opts.Metadata)
		open.IfMatch = opts.IfMatch
		open.IfNoneMatch = opts.IfNoneMatch
	}
	if err := stream.Send(&proto.WriteObjectRequest{
		Msg: &proto.WriteObjectRequest_Open{Open: open},
	}); err != nil {
		return s3.ObjectMeta{}, s3.ClientError(err)
	}
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
	_, err := c.grpc.DeleteObject(ctx, &proto.DeleteObjectRequest{Ref: objectRefToProto(ref)})
	return s3.ClientError(err)
}

// ListObjects implements s3.Client.
func (c *rpcClient) ListObjects(ctx context.Context, req s3.ListRequest) (s3.ListPage, error) {
	resp, err := c.grpc.ListObjects(ctx, &proto.ListObjectsRequest{
		Bucket:            req.Bucket,
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

// CopyObject implements s3.Client.
func (c *rpcClient) CopyObject(ctx context.Context, req s3.CopyRequest) (s3.ObjectMeta, error) {
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

// PresignObject implements s3.Client.
func (c *rpcClient) PresignObject(ctx context.Context, req s3.PresignRequest) (s3.PresignResult, error) {
	pb := &proto.PresignObjectRequest{
		Ref: objectRefToProto(req.Ref),
	}
	var requestedMethod s3.PresignMethod = req.Method
	pb.Method = presignMethodToProto(req.Method)
	pb.ExpiresSeconds = int64(req.Expires / time.Second)
	pb.ContentType = req.ContentType
	pb.ContentDisposition = req.ContentDisposition
	pb.Headers = s3.CloneStringMap(req.Headers)
	return c.presignObject(ctx, pb, requestedMethod)
}

func (c *rpcClient) presignObject(ctx context.Context, req *proto.PresignObjectRequest, requestedMethod s3.PresignMethod) (s3.PresignResult, error) {
	resp, err := c.grpc.PresignObject(ctx, req)
	if err != nil {
		return s3.PresignResult{}, s3.ClientError(err)
	}
	return presignResultFromProto(resp, requestedMethod), nil
}

// CreateObjectAccessURL creates a host-mediated object-access URL.
func (c *rpcClient) CreateObjectAccessURL(ctx context.Context, ref s3.ObjectRef, opts *s3.ObjectAccessURLOptions) (s3.ObjectAccessURL, error) {
	req := &proto.CreateObjectAccessURLRequest{
		Ref: objectRefToProto(ref),
	}
	var requestedMethod s3.PresignMethod
	if opts != nil {
		requestedMethod = opts.Method
		req.Method = presignMethodToProto(opts.Method)
		req.ExpiresSeconds = int64(opts.Expires / time.Second)
		req.ContentType = opts.ContentType
		req.ContentDisposition = opts.ContentDisposition
		req.Headers = s3.CloneStringMap(opts.Headers)
	}
	resp, err := c.objectAccessClient.CreateObjectAccessURL(ctx, req)
	if err != nil {
		return s3.ObjectAccessURL{}, s3.ClientError(err)
	}
	return objectAccessURLFromProto(resp, requestedMethod), nil
}

// CreateObjectAccessUrl is an alias for CreateObjectAccessURL.
func (c *rpcClient) CreateObjectAccessUrl(ctx context.Context, ref s3.ObjectRef, opts *s3.ObjectAccessURLOptions) (s3.ObjectAccessURL, error) {
	return c.CreateObjectAccessURL(ctx, ref, opts)
}

// CreateAccessURL is a short alias for CreateObjectAccessURL.
func (c *rpcClient) CreateAccessURL(ctx context.Context, ref s3.ObjectRef, opts *s3.ObjectAccessURLOptions) (s3.ObjectAccessURL, error) {
	return c.CreateObjectAccessURL(ctx, ref, opts)
}

// CreateAccessUrl is an alias for CreateAccessURL.
func (c *rpcClient) CreateAccessUrl(ctx context.Context, ref s3.ObjectRef, opts *s3.ObjectAccessURLOptions) (s3.ObjectAccessURL, error) {
	return c.CreateObjectAccessURL(ctx, ref, opts)
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
		Bucket:    ref.Bucket,
		Key:       ref.Key,
		VersionId: ref.VersionID,
	}
}

func objectRefFromProto(ref *proto.S3ObjectRef) s3.ObjectRef {
	if ref == nil {
		return s3.ObjectRef{}
	}
	return s3.ObjectRef{
		Bucket:    ref.GetBucket(),
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
			Bucket:    meta.GetRef().GetBucket(),
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

func readOptionsFromProto(req *proto.ReadObjectRequest) *s3.ReadOptions {
	if req == nil {
		return nil
	}
	opts := &s3.ReadOptions{
		Range:       byteRangeFromProto(req.GetRange()),
		IfMatch:     req.GetIfMatch(),
		IfNoneMatch: req.GetIfNoneMatch(),
	}
	if ts := req.GetIfModifiedSince(); ts != nil {
		t := ts.AsTime()
		opts.IfModifiedSince = &t
	}
	if ts := req.GetIfUnmodifiedSince(); ts != nil {
		t := ts.AsTime()
		opts.IfUnmodifiedSince = &t
	}
	return opts
}

func writeOptionsFromProto(open *proto.WriteObjectOpen) *s3.WriteOptions {
	if open == nil {
		return nil
	}
	return &s3.WriteOptions{
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

func presignOptionsFromProto(req *proto.PresignObjectRequest) *s3.PresignOptions {
	if req == nil {
		return nil
	}
	return &s3.PresignOptions{
		Method:             presignMethodFromProto(req.GetMethod()),
		Expires:            time.Duration(req.GetExpiresSeconds()) * time.Second,
		ContentType:        req.GetContentType(),
		ContentDisposition: req.GetContentDisposition(),
		Headers:            s3.CloneStringMap(req.GetHeaders()),
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

func objectAccessURLFromProto(resp *proto.CreateObjectAccessURLResponse, requested s3.PresignMethod) s3.ObjectAccessURL {
	if resp == nil {
		return s3.ObjectAccessURL{}
	}
	method := presignMethodFromProto(resp.GetMethod())
	if method == "" {
		method = requested
	}
	out := s3.ObjectAccessURL{
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

