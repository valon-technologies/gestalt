package host

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/protov1/v1"
	. "github.com/valon-technologies/gestalt/sdk/go/s3"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var _ Client = (*HostClient)(nil)

// HostClient speaks to a running S3 provider over the unified host-service socket.
type HostClient struct {
	client             proto.S3Client
	objectAccessClient proto.S3ObjectAccessClient
	rpcConfig
}

// Close is a no-op because this client uses shared transport.
func (c *HostClient) Close() error { return nil }

// Object returns a convenience handle for one object key.
func (c *HostClient) Object(bucket, key string) *ObjectHandleRef {
	return NewObject(c, ObjectRef{Bucket: bucket, Key: key})
}

// ObjectVersion returns a convenience handle for one object version.
func (c *HostClient) ObjectVersion(bucket, key, versionID string) *ObjectHandleRef {
	return NewObject(c, ObjectRef{Bucket: bucket, Key: key, VersionID: versionID})
}

// HeadObject fetches metadata for one object.
func (c *HostClient) HeadObject(ctx context.Context, ref ObjectRef) (ObjectMeta, error) {
	ctx, cancel := c.withDeadline(ctx)
	defer cancel()
	resp, err := c.client.HeadObject(ctx, &proto.HeadObjectRequest{Ref: objectRefToProto(ref)})
	if err != nil {
		return ObjectMeta{}, grpcS3Err(err)
	}
	return requiredObjectMeta(resp.GetMeta(), "head object")
}

// ReadObject implements Client using request-oriented parameters.
func (c *HostClient) ReadObject(ctx context.Context, req ReadRequest) (ReadResult, error) {
	opts := &ReadOptions{
		Range: req.Range, IfMatch: req.IfMatch, IfNoneMatch: req.IfNoneMatch,
		IfModifiedSince: req.IfModifiedSince, IfUnmodifiedSince: req.IfUnmodifiedSince,
	}
	meta, body, err := c.readObjectStream(ctx, req.Ref, opts)
	if err != nil {
		return ReadResult{}, err
	}
	return ReadResult{Meta: meta, Body: body}, nil
}

// readObjectStream opens a streaming object reader (legacy convenience shape).
func (c *HostClient) readObjectStream(ctx context.Context, ref ObjectRef, opts *ReadOptions) (ObjectMeta, io.ReadCloser, error) {
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
	stream, err := c.client.ReadObject(readCtx, req)
	if err != nil {
		cancel()
		return ObjectMeta{}, nil, grpcS3Err(err)
	}
	first, err := stream.Recv()
	if err != nil {
		cancel()
		return ObjectMeta{}, nil, grpcS3Err(err)
	}
	meta := first.GetMeta()
	if meta == nil {
		cancel()
		return ObjectMeta{}, nil, fmt.Errorf("s3: read stream did not start with metadata")
	}
	return objectMetaFromProto(meta), &s3ReadCloser{stream: stream, cancel: cancel}, nil
}

// WriteObject implements Client.
func (c *HostClient) WriteObject(ctx context.Context, req WriteRequest) (ObjectMeta, error) {
	opts := &WriteOptions{
		ContentType: req.ContentType, CacheControl: req.CacheControl,
		ContentDisposition: req.ContentDisposition, ContentEncoding: req.ContentEncoding,
		ContentLanguage: req.ContentLanguage, Metadata: req.Metadata,
		IfMatch: req.IfMatch, IfNoneMatch: req.IfNoneMatch,
	}
	return c.writeObjectStream(ctx, req.Ref, req.Body, opts)
}

func (c *HostClient) writeObjectStream(ctx context.Context, ref ObjectRef, body io.Reader, opts *WriteOptions) (ObjectMeta, error) {
	stream, err := c.client.WriteObject(ctx)
	if err != nil {
		return ObjectMeta{}, grpcS3Err(err)
	}
	open := &proto.WriteObjectOpen{Ref: objectRefToProto(ref)}
	if opts != nil {
		open.ContentType = opts.ContentType
		open.CacheControl = opts.CacheControl
		open.ContentDisposition = opts.ContentDisposition
		open.ContentEncoding = opts.ContentEncoding
		open.ContentLanguage = opts.ContentLanguage
		open.Metadata = CloneStringMap(opts.Metadata)
		open.IfMatch = opts.IfMatch
		open.IfNoneMatch = opts.IfNoneMatch
	}
	if err := stream.Send(&proto.WriteObjectRequest{
		Msg: &proto.WriteObjectRequest_Open{Open: open},
	}); err != nil {
		return ObjectMeta{}, grpcS3Err(err)
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
				return ObjectMeta{}, grpcS3Err(err)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return ObjectMeta{}, readErr
		}
	}
	resp, err := stream.CloseAndRecv()
	if err != nil {
		return ObjectMeta{}, grpcS3Err(err)
	}
	return requiredObjectMeta(resp.GetMeta(), "write object")
}

// DeleteObject removes one object.
func (c *HostClient) DeleteObject(ctx context.Context, ref ObjectRef) error {
	ctx, cancel := c.withDeadline(ctx)
	defer cancel()
	_, err := c.client.DeleteObject(ctx, &proto.DeleteObjectRequest{Ref: objectRefToProto(ref)})
	return grpcS3Err(err)
}

// ListObjects implements Client.
func (c *HostClient) ListObjects(ctx context.Context, req ListRequest) (ListPage, error) {
	ctx, cancel := c.withDeadline(ctx)
	defer cancel()
	resp, err := c.client.ListObjects(ctx, &proto.ListObjectsRequest{
		Bucket:            req.Bucket,
		Prefix:            req.Prefix,
		Delimiter:         req.Delimiter,
		ContinuationToken: req.ContinuationToken,
		StartAfter:        req.StartAfter,
		MaxKeys:           req.MaxKeys,
	})
	if err != nil {
		return ListPage{}, grpcS3Err(err)
	}
	return listPageFromProto(resp), nil
}

// CopyObject implements Client.
func (c *HostClient) CopyObject(ctx context.Context, req CopyRequest) (ObjectMeta, error) {
	ctx, cancel := c.withDeadline(ctx)
	defer cancel()
	pb := &proto.CopyObjectRequest{
		Source:      objectRefToProto(req.Source),
		Destination: objectRefToProto(req.Destination),
		IfMatch:     req.IfMatch,
		IfNoneMatch: req.IfNoneMatch,
	}
	resp, err := c.client.CopyObject(ctx, pb)
	if err != nil {
		return ObjectMeta{}, grpcS3Err(err)
	}
	return requiredObjectMeta(resp.GetMeta(), "copy object")
}

// PresignObject implements Client.
func (c *HostClient) PresignObject(ctx context.Context, req PresignRequest) (PresignResult, error) {
	pb := &proto.PresignObjectRequest{
		Ref: objectRefToProto(req.Ref),
	}
	var requestedMethod PresignMethod = req.Method
	pb.Method = presignMethodToProto(req.Method)
	pb.ExpiresSeconds = int64(req.Expires / time.Second)
	pb.ContentType = req.ContentType
	pb.ContentDisposition = req.ContentDisposition
	pb.Headers = CloneStringMap(req.Headers)
	return c.presignObject(ctx, pb, requestedMethod)
}

func (c *HostClient) presignObject(ctx context.Context, req *proto.PresignObjectRequest, requestedMethod PresignMethod) (PresignResult, error) {
	ctx, cancel := c.withDeadline(ctx)
	defer cancel()
	resp, err := c.client.PresignObject(ctx, req)
	if err != nil {
		return PresignResult{}, grpcS3Err(err)
	}
	return presignResultFromProto(resp, requestedMethod), nil
}

// CreateObjectAccessURL creates a host-mediated object-access URL.
func (c *HostClient) CreateObjectAccessURL(ctx context.Context, ref ObjectRef, opts *ObjectAccessURLOptions) (ObjectAccessURL, error) {
	ctx, cancel := c.withDeadline(ctx)
	defer cancel()
	req := &proto.CreateObjectAccessURLRequest{
		Ref: objectRefToProto(ref),
	}
	var requestedMethod PresignMethod
	if opts != nil {
		requestedMethod = opts.Method
		req.Method = presignMethodToProto(opts.Method)
		req.ExpiresSeconds = int64(opts.Expires / time.Second)
		req.ContentType = opts.ContentType
		req.ContentDisposition = opts.ContentDisposition
		req.Headers = CloneStringMap(opts.Headers)
	}
	resp, err := c.objectAccessClient.CreateObjectAccessURL(ctx, req)
	if err != nil {
		return ObjectAccessURL{}, grpcS3Err(err)
	}
	return objectAccessURLFromProto(resp, requestedMethod), nil
}

// CreateObjectAccessUrl is an alias for CreateObjectAccessURL.
func (c *HostClient) CreateObjectAccessUrl(ctx context.Context, ref ObjectRef, opts *ObjectAccessURLOptions) (ObjectAccessURL, error) {
	return c.CreateObjectAccessURL(ctx, ref, opts)
}

// CreateAccessURL is a short alias for CreateObjectAccessURL.
func (c *HostClient) CreateAccessURL(ctx context.Context, ref ObjectRef, opts *ObjectAccessURLOptions) (ObjectAccessURL, error) {
	return c.CreateObjectAccessURL(ctx, ref, opts)
}

// CreateAccessUrl is an alias for CreateAccessURL.
func (c *HostClient) CreateAccessUrl(ctx context.Context, ref ObjectRef, opts *ObjectAccessURLOptions) (ObjectAccessURL, error) {
	return c.CreateObjectAccessURL(ctx, ref, opts)
}

// Object is a convenience wrapper around repeated operations on one object key.
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
			return 0, grpcS3Err(err)
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

func objectRefToProto(ref ObjectRef) *proto.S3ObjectRef {
	return &proto.S3ObjectRef{
		Bucket:    ref.Bucket,
		Key:       ref.Key,
		VersionId: ref.VersionID,
	}
}

func objectRefFromProto(ref *proto.S3ObjectRef) ObjectRef {
	if ref == nil {
		return ObjectRef{}
	}
	return ObjectRef{
		Bucket:    ref.GetBucket(),
		Key:       ref.GetKey(),
		VersionID: ref.GetVersionId(),
	}
}

func objectMetaToProto(meta ObjectMeta) *proto.S3ObjectMeta {
	out := &proto.S3ObjectMeta{
		Ref:          objectRefToProto(meta.Ref),
		Etag:         meta.ETag,
		Size:         meta.Size,
		ContentType:  meta.ContentType,
		Metadata:     CloneStringMap(meta.Metadata),
		StorageClass: meta.StorageClass,
	}
	if !meta.LastModified.IsZero() {
		out.LastModified = timestamppb.New(meta.LastModified)
	}
	return out
}

func objectMetaFromProto(meta *proto.S3ObjectMeta) ObjectMeta {
	if meta == nil {
		return ObjectMeta{}
	}
	out := ObjectMeta{
		Ref: ObjectRef{
			Bucket:    meta.GetRef().GetBucket(),
			Key:       meta.GetRef().GetKey(),
			VersionID: meta.GetRef().GetVersionId(),
		},
		ETag:         meta.GetEtag(),
		Size:         meta.GetSize(),
		ContentType:  meta.GetContentType(),
		Metadata:     CloneStringMap(meta.GetMetadata()),
		StorageClass: meta.GetStorageClass(),
	}
	if ts := meta.GetLastModified(); ts != nil {
		out.LastModified = ts.AsTime()
	}
	return out
}

func requiredObjectMeta(meta *proto.S3ObjectMeta, op string) (ObjectMeta, error) {
	if meta == nil {
		return ObjectMeta{}, fmt.Errorf("s3: %s response missing metadata", op)
	}
	return objectMetaFromProto(meta), nil
}

func byteRangeToProto(r *ByteRange) *proto.ByteRange {
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

func byteRangeFromProto(r *proto.ByteRange) *ByteRange {
	if r == nil {
		return nil
	}
	out := &ByteRange{}
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

func readOptionsFromProto(req *proto.ReadObjectRequest) *ReadOptions {
	if req == nil {
		return nil
	}
	opts := &ReadOptions{
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

func writeOptionsFromProto(open *proto.WriteObjectOpen) *WriteOptions {
	if open == nil {
		return nil
	}
	return &WriteOptions{
		ContentType:        open.GetContentType(),
		CacheControl:       open.GetCacheControl(),
		ContentDisposition: open.GetContentDisposition(),
		ContentEncoding:    open.GetContentEncoding(),
		ContentLanguage:    open.GetContentLanguage(),
		Metadata:           CloneStringMap(open.GetMetadata()),
		IfMatch:            open.GetIfMatch(),
		IfNoneMatch:        open.GetIfNoneMatch(),
	}
}

func listPageFromProto(resp *proto.ListObjectsResponse) ListPage {
	out := ListPage{
		CommonPrefixes:        append([]string(nil), resp.GetCommonPrefixes()...),
		NextContinuationToken: resp.GetNextContinuationToken(),
		HasMore:               resp.GetHasMore(),
	}
	out.Objects = make([]ObjectMeta, 0, len(resp.GetObjects()))
	for _, obj := range resp.GetObjects() {
		out.Objects = append(out.Objects, objectMetaFromProto(obj))
	}
	return out
}

func listPageToProto(page ListPage) *proto.ListObjectsResponse {
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

func presignMethodToProto(method PresignMethod) proto.PresignMethod {
	switch method {
	case PresignMethodGet:
		return proto.PresignMethod_PRESIGN_METHOD_GET
	case PresignMethodPut:
		return proto.PresignMethod_PRESIGN_METHOD_PUT
	case PresignMethodDelete:
		return proto.PresignMethod_PRESIGN_METHOD_DELETE
	case PresignMethodHead:
		return proto.PresignMethod_PRESIGN_METHOD_HEAD
	default:
		return proto.PresignMethod_PRESIGN_METHOD_UNSPECIFIED
	}
}

func presignOptionsFromProto(req *proto.PresignObjectRequest) *PresignOptions {
	if req == nil {
		return nil
	}
	return &PresignOptions{
		Method:             presignMethodFromProto(req.GetMethod()),
		Expires:            time.Duration(req.GetExpiresSeconds()) * time.Second,
		ContentType:        req.GetContentType(),
		ContentDisposition: req.GetContentDisposition(),
		Headers:            CloneStringMap(req.GetHeaders()),
	}
}

func presignMethodFromProto(method proto.PresignMethod) PresignMethod {
	switch method {
	case proto.PresignMethod_PRESIGN_METHOD_GET:
		return PresignMethodGet
	case proto.PresignMethod_PRESIGN_METHOD_PUT:
		return PresignMethodPut
	case proto.PresignMethod_PRESIGN_METHOD_DELETE:
		return PresignMethodDelete
	case proto.PresignMethod_PRESIGN_METHOD_HEAD:
		return PresignMethodHead
	default:
		return ""
	}
}

func presignResultToProto(result PresignResult) *proto.PresignObjectResponse {
	resp := &proto.PresignObjectResponse{
		Url:     result.URL,
		Method:  presignMethodToProto(result.Method),
		Headers: CloneStringMap(result.Headers),
	}
	if !result.ExpiresAt.IsZero() {
		resp.ExpiresAt = timestamppb.New(result.ExpiresAt)
	}
	return resp
}

func presignResultFromProto(resp *proto.PresignObjectResponse, requested PresignMethod) PresignResult {
	if resp == nil {
		return PresignResult{}
	}
	method := presignMethodFromProto(resp.GetMethod())
	if method == "" {
		method = requested
	}
	out := PresignResult{
		URL:     resp.GetUrl(),
		Method:  method,
		Headers: CloneStringMap(resp.GetHeaders()),
	}
	if ts := resp.GetExpiresAt(); ts != nil {
		out.ExpiresAt = ts.AsTime()
	}
	return out
}

func objectAccessURLFromProto(resp *proto.CreateObjectAccessURLResponse, requested PresignMethod) ObjectAccessURL {
	if resp == nil {
		return ObjectAccessURL{}
	}
	method := presignMethodFromProto(resp.GetMethod())
	if method == "" {
		method = requested
	}
	out := ObjectAccessURL{
		URL:     resp.GetUrl(),
		Method:  method,
		Headers: CloneStringMap(resp.GetHeaders()),
	}
	if ts := resp.GetExpiresAt(); ts != nil {
		out.ExpiresAt = ts.AsTime()
	}
	return out
}

func grpcS3Err(err error) error {
	return ClientError(err)
}
