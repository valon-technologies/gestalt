package providerhost

import (
	"context"
	"fmt"
	"io"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	s3store "github.com/valon-technologies/gestalt/server/core/s3"
	"github.com/valon-technologies/gestalt/server/internal/egress"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type S3ExecConfig struct {
	Command    string
	Args       []string
	Env        map[string]string
	Config     map[string]any
	Egress     egress.Policy
	HostBinary string
	Cleanup    func()
	Name       string
}

type remoteS3 struct {
	client  proto.S3Client
	runtime proto.ProviderLifecycleClient
	closer  io.Closer
}

func NewExecutableS3(ctx context.Context, cfg S3ExecConfig) (s3store.Client, error) {
	execCfg := ExecConfig{
		Command:      cfg.Command,
		Args:         cfg.Args,
		Env:          cfg.Env,
		Config:       cfg.Config,
		Egress:       cloneEgressPolicy(cfg.Egress),
		HostBinary:   cfg.HostBinary,
		Cleanup:      cfg.Cleanup,
		ProviderName: cfg.Name,
	}
	proc, err := startProviderProcess(ctx, execCfg.processConfig())
	if err != nil {
		return nil, err
	}

	runtimeClient := proto.NewProviderLifecycleClient(proc.conn)
	s3Client := proto.NewS3Client(proc.conn)
	if _, err := ConfigureRuntimeProvider(ctx, runtimeClient, proto.ProviderKind_PROVIDER_KIND_S3, cfg.Name, cfg.Config); err != nil {
		_ = proc.Close()
		return nil, err
	}
	return &remoteS3{client: s3Client, runtime: runtimeClient, closer: proc}, nil
}

func (r *remoteS3) HeadObject(ctx context.Context, ref s3store.ObjectRef) (s3store.ObjectMeta, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	resp, err := r.client.HeadObject(ctx, &proto.HeadObjectRequest{Ref: objectRefToProto(ref)})
	if err != nil {
		return s3store.ObjectMeta{}, grpcToS3Err(err)
	}
	return requiredObjectMeta(resp.GetMeta(), "head object")
}

func (r *remoteS3) ReadObject(ctx context.Context, req s3store.ReadRequest) (s3store.ReadResult, error) {
	ctx, cancel := providerStreamContext(ctx)
	stream, err := r.client.ReadObject(ctx, readObjectRequestToProto(req))
	if err != nil {
		cancel()
		return s3store.ReadResult{}, grpcToS3Err(err)
	}
	first, err := stream.Recv()
	if err != nil {
		cancel()
		return s3store.ReadResult{}, grpcToS3Err(err)
	}
	meta := first.GetMeta()
	if meta == nil {
		cancel()
		return s3store.ReadResult{}, fmt.Errorf("s3: read stream did not start with metadata")
	}
	return s3store.ReadResult{
		Meta: objectMetaFromProto(meta),
		Body: &remoteS3Body{stream: stream, cancel: cancel},
	}, nil
}

func (r *remoteS3) WriteObject(ctx context.Context, req s3store.WriteRequest) (s3store.ObjectMeta, error) {
	ctx, cancel := providerStreamContext(ctx)
	defer cancel()
	stream, err := r.client.WriteObject(ctx)
	if err != nil {
		return s3store.ObjectMeta{}, grpcToS3Err(err)
	}
	if err := stream.Send(&proto.WriteObjectRequest{
		Msg: &proto.WriteObjectRequest_Open{Open: writeObjectOpenToProto(req)},
	}); err != nil {
		return s3store.ObjectMeta{}, grpcToS3Err(err)
	}
	body := req.Body
	if body == nil {
		body = emptyReader{}
	}
	buf := make([]byte, 64*1024)
	for {
		n, readErr := body.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			if err := stream.Send(&proto.WriteObjectRequest{
				Msg: &proto.WriteObjectRequest_Data{Data: chunk},
			}); err != nil {
				return s3store.ObjectMeta{}, grpcToS3Err(err)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return s3store.ObjectMeta{}, readErr
		}
	}
	resp, err := stream.CloseAndRecv()
	if err != nil {
		return s3store.ObjectMeta{}, grpcToS3Err(err)
	}
	return requiredObjectMeta(resp.GetMeta(), "write object")
}

func (r *remoteS3) DeleteObject(ctx context.Context, ref s3store.ObjectRef) error {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	_, err := r.client.DeleteObject(ctx, &proto.DeleteObjectRequest{Ref: objectRefToProto(ref)})
	return grpcToS3Err(err)
}

func (r *remoteS3) ListObjects(ctx context.Context, req s3store.ListRequest) (s3store.ListPage, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	resp, err := r.client.ListObjects(ctx, listObjectsRequestToProto(req))
	if err != nil {
		return s3store.ListPage{}, grpcToS3Err(err)
	}
	return listPageFromProto(resp), nil
}

func (r *remoteS3) CopyObject(ctx context.Context, req s3store.CopyRequest) (s3store.ObjectMeta, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	resp, err := r.client.CopyObject(ctx, copyObjectRequestToProto(req))
	if err != nil {
		return s3store.ObjectMeta{}, grpcToS3Err(err)
	}
	return requiredObjectMeta(resp.GetMeta(), "copy object")
}

func (r *remoteS3) PresignObject(ctx context.Context, req s3store.PresignRequest) (s3store.PresignResult, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	resp, err := r.client.PresignObject(ctx, presignRequestToProto(req))
	if err != nil {
		return s3store.PresignResult{}, grpcToS3Err(err)
	}
	return presignResultFromProto(resp, req.Method), nil
}

func (r *remoteS3) Ping(ctx context.Context) error {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	_, err := r.runtime.HealthCheck(ctx, &emptypb.Empty{})
	return err
}

func (r *remoteS3) Close() error {
	if r == nil || r.closer == nil {
		return nil
	}
	return r.closer.Close()
}

type remoteS3Body struct {
	stream  proto.S3_ReadObjectClient
	cancel  context.CancelFunc
	pending []byte
	closed  bool
}

func (b *remoteS3Body) Read(p []byte) (int, error) {
	if len(b.pending) > 0 {
		n := copy(p, b.pending)
		b.pending = b.pending[n:]
		return n, nil
	}
	if b.closed {
		return 0, io.EOF
	}
	for {
		resp, err := b.stream.Recv()
		if err == io.EOF {
			if b.cancel != nil {
				b.cancel()
				b.cancel = nil
			}
			b.closed = true
			return 0, io.EOF
		}
		if err != nil {
			if b.cancel != nil {
				b.cancel()
				b.cancel = nil
			}
			b.closed = true
			return 0, grpcToS3Err(err)
		}
		if chunk := resp.GetData(); len(chunk) > 0 {
			n := copy(p, chunk)
			b.pending = append(b.pending[:0], chunk[n:]...)
			return n, nil
		}
	}
}

func (b *remoteS3Body) Close() error {
	if b.cancel != nil {
		b.cancel()
		b.cancel = nil
	}
	b.pending = nil
	b.closed = true
	return nil
}

type emptyReader struct{}

func (emptyReader) Read(_ []byte) (int, error) { return 0, io.EOF }

func grpcToS3Err(err error) error {
	if err == nil {
		return nil
	}
	st, ok := status.FromError(err)
	if !ok {
		return err
	}
	switch st.Code() {
	case codes.NotFound:
		return s3store.ErrNotFound
	case codes.FailedPrecondition:
		return s3store.ErrPreconditionFailed
	case codes.OutOfRange:
		return s3store.ErrInvalidRange
	default:
		return err
	}
}

func objectRefToProto(ref s3store.ObjectRef) *proto.S3ObjectRef {
	return &proto.S3ObjectRef{
		Bucket:    ref.Bucket,
		Key:       ref.Key,
		VersionId: ref.VersionID,
	}
}

func objectRefFromProto(ref *proto.S3ObjectRef) s3store.ObjectRef {
	if ref == nil {
		return s3store.ObjectRef{}
	}
	return s3store.ObjectRef{
		Bucket:    ref.GetBucket(),
		Key:       ref.GetKey(),
		VersionID: ref.GetVersionId(),
	}
}

func objectMetaFromProto(meta *proto.S3ObjectMeta) s3store.ObjectMeta {
	if meta == nil {
		return s3store.ObjectMeta{}
	}
	out := s3store.ObjectMeta{
		Ref:          objectRefFromProto(meta.GetRef()),
		ETag:         meta.GetEtag(),
		Size:         meta.GetSize(),
		ContentType:  meta.GetContentType(),
		Metadata:     s3store.CloneStringMap(meta.GetMetadata()),
		StorageClass: meta.GetStorageClass(),
	}
	if ts := meta.GetLastModified(); ts != nil {
		out.LastModified = ts.AsTime()
	}
	return out
}

func requiredObjectMeta(meta *proto.S3ObjectMeta, op string) (s3store.ObjectMeta, error) {
	if meta == nil {
		return s3store.ObjectMeta{}, fmt.Errorf("s3: %s response missing metadata", op)
	}
	return objectMetaFromProto(meta), nil
}

func objectMetaToProto(meta s3store.ObjectMeta) *proto.S3ObjectMeta {
	out := &proto.S3ObjectMeta{
		Ref:          objectRefToProto(meta.Ref),
		Etag:         meta.ETag,
		Size:         meta.Size,
		ContentType:  meta.ContentType,
		Metadata:     s3store.CloneStringMap(meta.Metadata),
		StorageClass: meta.StorageClass,
	}
	if !meta.LastModified.IsZero() {
		out.LastModified = timestamppb.New(meta.LastModified)
	}
	return out
}

func readObjectRequestToProto(req s3store.ReadRequest) *proto.ReadObjectRequest {
	out := &proto.ReadObjectRequest{
		Ref:         objectRefToProto(req.Ref),
		IfMatch:     req.IfMatch,
		IfNoneMatch: req.IfNoneMatch,
	}
	if req.Range != nil {
		out.Range = byteRangeToProto(req.Range)
	}
	if req.IfModifiedSince != nil {
		out.IfModifiedSince = timestamppb.New(*req.IfModifiedSince)
	}
	if req.IfUnmodifiedSince != nil {
		out.IfUnmodifiedSince = timestamppb.New(*req.IfUnmodifiedSince)
	}
	return out
}

func byteRangeToProto(r *s3store.ByteRange) *proto.ByteRange {
	if r == nil {
		return nil
	}
	out := &proto.ByteRange{}
	if r.Start != nil {
		start := *r.Start
		out.Start = &start
	}
	if r.End != nil {
		end := *r.End
		out.End = &end
	}
	return out
}

func byteRangeFromProto(r *proto.ByteRange) *s3store.ByteRange {
	if r == nil {
		return nil
	}
	out := &s3store.ByteRange{}
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

func writeObjectOpenToProto(req s3store.WriteRequest) *proto.WriteObjectOpen {
	return &proto.WriteObjectOpen{
		Ref:                objectRefToProto(req.Ref),
		ContentType:        req.ContentType,
		CacheControl:       req.CacheControl,
		ContentDisposition: req.ContentDisposition,
		ContentEncoding:    req.ContentEncoding,
		ContentLanguage:    req.ContentLanguage,
		Metadata:           s3store.CloneStringMap(req.Metadata),
		IfMatch:            req.IfMatch,
		IfNoneMatch:        req.IfNoneMatch,
	}
}

func listObjectsRequestToProto(req s3store.ListRequest) *proto.ListObjectsRequest {
	return &proto.ListObjectsRequest{
		Bucket:            req.Bucket,
		Prefix:            req.Prefix,
		Delimiter:         req.Delimiter,
		ContinuationToken: req.ContinuationToken,
		StartAfter:        req.StartAfter,
		MaxKeys:           req.MaxKeys,
	}
}

func listPageFromProto(resp *proto.ListObjectsResponse) s3store.ListPage {
	if resp == nil {
		return s3store.ListPage{}
	}
	out := s3store.ListPage{
		CommonPrefixes:        append([]string(nil), resp.GetCommonPrefixes()...),
		NextContinuationToken: resp.GetNextContinuationToken(),
		HasMore:               resp.GetHasMore(),
	}
	out.Objects = make([]s3store.ObjectMeta, 0, len(resp.GetObjects()))
	for _, obj := range resp.GetObjects() {
		out.Objects = append(out.Objects, objectMetaFromProto(obj))
	}
	return out
}

func copyObjectRequestToProto(req s3store.CopyRequest) *proto.CopyObjectRequest {
	return &proto.CopyObjectRequest{
		Source:      objectRefToProto(req.Source),
		Destination: objectRefToProto(req.Destination),
		IfMatch:     req.IfMatch,
		IfNoneMatch: req.IfNoneMatch,
	}
}

func presignMethodToProto(method s3store.PresignMethod) proto.PresignMethod {
	switch method {
	case s3store.PresignMethodGet:
		return proto.PresignMethod_PRESIGN_METHOD_GET
	case s3store.PresignMethodPut:
		return proto.PresignMethod_PRESIGN_METHOD_PUT
	case s3store.PresignMethodDelete:
		return proto.PresignMethod_PRESIGN_METHOD_DELETE
	case s3store.PresignMethodHead:
		return proto.PresignMethod_PRESIGN_METHOD_HEAD
	default:
		return proto.PresignMethod_PRESIGN_METHOD_UNSPECIFIED
	}
}

func presignMethodFromProto(method proto.PresignMethod) s3store.PresignMethod {
	switch method {
	case proto.PresignMethod_PRESIGN_METHOD_GET:
		return s3store.PresignMethodGet
	case proto.PresignMethod_PRESIGN_METHOD_PUT:
		return s3store.PresignMethodPut
	case proto.PresignMethod_PRESIGN_METHOD_DELETE:
		return s3store.PresignMethodDelete
	case proto.PresignMethod_PRESIGN_METHOD_HEAD:
		return s3store.PresignMethodHead
	default:
		return ""
	}
}

func presignRequestToProto(req s3store.PresignRequest) *proto.PresignObjectRequest {
	out := &proto.PresignObjectRequest{
		Ref:                objectRefToProto(req.Ref),
		Method:             presignMethodToProto(req.Method),
		ExpiresSeconds:     int64(req.Expires / time.Second),
		ContentType:        req.ContentType,
		ContentDisposition: req.ContentDisposition,
		Headers:            s3store.CloneStringMap(req.Headers),
	}
	return out
}

func presignResultFromProto(resp *proto.PresignObjectResponse, requested s3store.PresignMethod) s3store.PresignResult {
	if resp == nil {
		return s3store.PresignResult{}
	}
	method := presignMethodFromProto(resp.GetMethod())
	if method == "" {
		method = requested
	}
	out := s3store.PresignResult{
		URL:     resp.GetUrl(),
		Method:  method,
		Headers: s3store.CloneStringMap(resp.GetHeaders()),
	}
	if ts := resp.GetExpiresAt(); ts != nil {
		out.ExpiresAt = ts.AsTime()
	}
	return out
}
