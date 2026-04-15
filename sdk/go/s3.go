package gestalt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// EnvS3Socket is the default Unix-socket environment variable used by [S3].
const EnvS3Socket = "GESTALT_S3_SOCKET"

// S3SocketEnv returns the environment variable name used for a named S3
// transport socket.
func S3SocketEnv(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return EnvS3Socket
	}
	var b strings.Builder
	b.WriteString(EnvS3Socket)
	b.WriteByte('_')
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - ('a' - 'A'))
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

var (
	// ErrS3NotFound indicates that the requested object does not exist.
	ErrS3NotFound = fmt.Errorf("s3: not found")
	// ErrS3PreconditionFailed indicates that the request failed an If-Match or
	// If-None-Match style precondition.
	ErrS3PreconditionFailed = fmt.Errorf("s3: precondition failed")
	// ErrS3InvalidRange indicates that the requested byte range is invalid.
	ErrS3InvalidRange = fmt.Errorf("s3: invalid range")
)

// ObjectRef identifies one object or object version.
type ObjectRef struct {
	Bucket    string
	Key       string
	VersionID string
}

// ObjectMeta describes an object returned by the provider.
type ObjectMeta struct {
	Ref          ObjectRef
	ETag         string
	Size         int64
	ContentType  string
	LastModified time.Time
	Metadata     map[string]string
	StorageClass string
}

// ByteRange requests a half-open slice of an object's bytes.
type ByteRange struct {
	Start *int64
	End   *int64
}

// ReadOptions configures conditional and ranged reads.
type ReadOptions struct {
	Range             *ByteRange
	IfMatch           string
	IfNoneMatch       string
	IfModifiedSince   *time.Time
	IfUnmodifiedSince *time.Time
}

// WriteOptions configures object writes.
type WriteOptions struct {
	ContentType        string
	CacheControl       string
	ContentDisposition string
	ContentEncoding    string
	ContentLanguage    string
	Metadata           map[string]string
	IfMatch            string
	IfNoneMatch        string
}

// ListOptions configures list-objects requests.
type ListOptions struct {
	Bucket            string
	Prefix            string
	Delimiter         string
	ContinuationToken string
	StartAfter        string
	MaxKeys           int32
}

// ListPage is one page of list-objects results.
type ListPage struct {
	Objects               []ObjectMeta
	CommonPrefixes        []string
	NextContinuationToken string
	HasMore               bool
}

// CopyOptions configures conditional copy requests.
type CopyOptions struct {
	IfMatch     string
	IfNoneMatch string
}

// PresignMethod identifies the HTTP verb encoded into a presigned URL.
type PresignMethod string

const (
	// PresignMethodGet creates a download URL.
	PresignMethodGet PresignMethod = "GET"
	// PresignMethodPut creates an upload URL.
	PresignMethodPut PresignMethod = "PUT"
	// PresignMethodDelete creates a delete URL.
	PresignMethodDelete PresignMethod = "DELETE"
	// PresignMethodHead creates a metadata-only URL.
	PresignMethodHead PresignMethod = "HEAD"
)

// PresignOptions configures presigned URL generation.
type PresignOptions struct {
	Method             PresignMethod
	Expires            time.Duration
	ContentType        string
	ContentDisposition string
	Headers            map[string]string
}

// PresignResult is a presigned URL plus any required headers.
type PresignResult struct {
	URL       string
	Method    PresignMethod
	ExpiresAt time.Time
	Headers   map[string]string
}

// S3Client speaks to a running S3 provider over a Unix socket.
type S3Client struct {
	client proto.S3Client
	conn   *grpc.ClientConn
}

// S3 connects to the S3 provider exposed by gestaltd.
func S3(name ...string) (*S3Client, error) {
	envName := EnvS3Socket
	if len(name) > 0 {
		envName = S3SocketEnv(name[0])
	}
	socketPath := os.Getenv(envName)
	if socketPath == "" {
		return nil, fmt.Errorf("s3: %s is not set", envName)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, "unix:"+socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, fmt.Errorf("s3: connect to host: %w", err)
	}
	return &S3Client{client: proto.NewS3Client(conn), conn: conn}, nil
}

// Close closes the underlying gRPC transport.
func (c *S3Client) Close() error { return c.conn.Close() }

// Object returns a convenience handle for one object key.
func (c *S3Client) Object(bucket, key string) *Object {
	return &Object{client: c, ref: ObjectRef{Bucket: bucket, Key: key}}
}

// ObjectVersion returns a convenience handle for one object version.
func (c *S3Client) ObjectVersion(bucket, key, versionID string) *Object {
	return &Object{client: c, ref: ObjectRef{Bucket: bucket, Key: key, VersionID: versionID}}
}

// HeadObject fetches metadata for one object.
func (c *S3Client) HeadObject(ctx context.Context, ref ObjectRef) (ObjectMeta, error) {
	resp, err := c.client.HeadObject(ctx, &proto.HeadObjectRequest{Ref: objectRefToProto(ref)})
	if err != nil {
		return ObjectMeta{}, grpcS3Err(err)
	}
	return requiredObjectMeta(resp.GetMeta(), "head object")
}

// ReadObject opens a streaming object reader.
func (c *S3Client) ReadObject(ctx context.Context, ref ObjectRef, opts *ReadOptions) (ObjectMeta, io.ReadCloser, error) {
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

// WriteObject uploads an object from body.
func (c *S3Client) WriteObject(ctx context.Context, ref ObjectRef, body io.Reader, opts *WriteOptions) (ObjectMeta, error) {
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
		open.Metadata = cloneStringMap(opts.Metadata)
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
func (c *S3Client) DeleteObject(ctx context.Context, ref ObjectRef) error {
	_, err := c.client.DeleteObject(ctx, &proto.DeleteObjectRequest{Ref: objectRefToProto(ref)})
	return grpcS3Err(err)
}

// ListObjects lists objects in a bucket.
func (c *S3Client) ListObjects(ctx context.Context, opts ListOptions) (ListPage, error) {
	resp, err := c.client.ListObjects(ctx, &proto.ListObjectsRequest{
		Bucket:            opts.Bucket,
		Prefix:            opts.Prefix,
		Delimiter:         opts.Delimiter,
		ContinuationToken: opts.ContinuationToken,
		StartAfter:        opts.StartAfter,
		MaxKeys:           opts.MaxKeys,
	})
	if err != nil {
		return ListPage{}, grpcS3Err(err)
	}
	return listPageFromProto(resp), nil
}

// CopyObject copies source to destination.
func (c *S3Client) CopyObject(ctx context.Context, source, destination ObjectRef, opts *CopyOptions) (ObjectMeta, error) {
	req := &proto.CopyObjectRequest{
		Source:      objectRefToProto(source),
		Destination: objectRefToProto(destination),
	}
	if opts != nil {
		req.IfMatch = opts.IfMatch
		req.IfNoneMatch = opts.IfNoneMatch
	}
	resp, err := c.client.CopyObject(ctx, req)
	if err != nil {
		return ObjectMeta{}, grpcS3Err(err)
	}
	return requiredObjectMeta(resp.GetMeta(), "copy object")
}

// PresignObject creates a provider-generated presigned URL.
func (c *S3Client) PresignObject(ctx context.Context, ref ObjectRef, opts *PresignOptions) (PresignResult, error) {
	req := &proto.PresignObjectRequest{
		Ref: objectRefToProto(ref),
	}
	var requestedMethod PresignMethod
	if opts != nil {
		requestedMethod = opts.Method
		req.Method = presignMethodToProto(opts.Method)
		req.ExpiresSeconds = int64(opts.Expires / time.Second)
		req.ContentType = opts.ContentType
		req.ContentDisposition = opts.ContentDisposition
		req.Headers = cloneStringMap(opts.Headers)
	}
	resp, err := c.client.PresignObject(ctx, req)
	if err != nil {
		return PresignResult{}, grpcS3Err(err)
	}
	return presignResultFromProto(resp, requestedMethod), nil
}

// Object is a convenience wrapper around repeated operations on one object key.
type Object struct {
	client *S3Client
	ref    ObjectRef
}

// Stat returns metadata for the current object.
func (o *Object) Stat(ctx context.Context) (ObjectMeta, error) {
	return o.client.HeadObject(ctx, o.ref)
}

// Exists reports whether the current object exists.
func (o *Object) Exists(ctx context.Context) (bool, error) {
	_, err := o.Stat(ctx)
	if err == nil {
		return true, nil
	}
	if err == ErrS3NotFound {
		return false, nil
	}
	return false, err
}

// Stream opens a streaming reader for the current object.
func (o *Object) Stream(ctx context.Context, opts *ReadOptions) (ObjectMeta, io.ReadCloser, error) {
	return o.client.ReadObject(ctx, o.ref, opts)
}

// Bytes reads the entire current object into memory.
func (o *Object) Bytes(ctx context.Context, opts *ReadOptions) ([]byte, error) {
	_, body, err := o.Stream(ctx, opts)
	if err != nil {
		return nil, err
	}
	defer func() { _ = body.Close() }()
	return io.ReadAll(body)
}

// Text reads the entire current object as UTF-8 text.
func (o *Object) Text(ctx context.Context, opts *ReadOptions) (string, error) {
	data, err := o.Bytes(ctx, opts)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// JSON reads and decodes the entire current object as JSON.
func (o *Object) JSON(ctx context.Context, opts *ReadOptions) (any, error) {
	data, err := o.Bytes(ctx, opts)
	if err != nil {
		return nil, err
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	return value, nil
}

// Write uploads a new object body from body.
func (o *Object) Write(ctx context.Context, body io.Reader, opts *WriteOptions) (ObjectMeta, error) {
	return o.client.WriteObject(ctx, o.ref, body, opts)
}

// WriteBytes uploads body as raw bytes.
func (o *Object) WriteBytes(ctx context.Context, body []byte, opts *WriteOptions) (ObjectMeta, error) {
	return o.Write(ctx, bytes.NewReader(body), opts)
}

// WriteString uploads body as text.
func (o *Object) WriteString(ctx context.Context, body string, opts *WriteOptions) (ObjectMeta, error) {
	return o.WriteBytes(ctx, []byte(body), opts)
}

// WriteJSON uploads value as JSON, defaulting the content type when omitted.
func (o *Object) WriteJSON(ctx context.Context, value any, opts *WriteOptions) (ObjectMeta, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return ObjectMeta{}, err
	}
	if opts == nil {
		opts = &WriteOptions{ContentType: "application/json"}
	} else if opts.ContentType == "" {
		opts.ContentType = "application/json"
	}
	return o.WriteBytes(ctx, body, opts)
}

// Delete removes the current object.
func (o *Object) Delete(ctx context.Context) error {
	return o.client.DeleteObject(ctx, o.ref)
}

// Presign creates a presigned URL for the current object.
func (o *Object) Presign(ctx context.Context, opts *PresignOptions) (PresignResult, error) {
	return o.client.PresignObject(ctx, o.ref, opts)
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
		Metadata:     cloneStringMap(meta.GetMetadata()),
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
		Headers: cloneStringMap(resp.GetHeaders()),
	}
	if ts := resp.GetExpiresAt(); ts != nil {
		out.ExpiresAt = ts.AsTime()
	}
	return out
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func grpcS3Err(err error) error {
	if err == nil {
		return nil
	}
	st, ok := status.FromError(err)
	if !ok {
		return err
	}
	switch st.Code() {
	case codes.NotFound:
		return ErrS3NotFound
	case codes.FailedPrecondition:
		return ErrS3PreconditionFailed
	case codes.OutOfRange:
		return ErrS3InvalidRange
	default:
		return err
	}
}
