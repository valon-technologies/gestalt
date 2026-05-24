package s3

import (
	"fmt"
	"time"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	s3sdk "github.com/valon-technologies/gestalt/sdk/go/s3"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func objectRefToProto(ref s3sdk.ObjectRef) *proto.S3ObjectRef {
	return &proto.S3ObjectRef{
		Bucket:    ref.Bucket,
		Key:       ref.Key,
		VersionId: ref.VersionID,
	}
}

func objectRefFromProto(ref *proto.S3ObjectRef) s3sdk.ObjectRef {
	if ref == nil {
		return s3sdk.ObjectRef{}
	}
	return s3sdk.ObjectRef{
		Bucket:    ref.GetBucket(),
		Key:       ref.GetKey(),
		VersionID: ref.GetVersionId(),
	}
}

func objectMetaFromProto(meta *proto.S3ObjectMeta) s3sdk.ObjectMeta {
	if meta == nil {
		return s3sdk.ObjectMeta{}
	}
	out := s3sdk.ObjectMeta{
		Ref:          objectRefFromProto(meta.GetRef()),
		ETag:         meta.GetEtag(),
		Size:         meta.GetSize(),
		ContentType:  meta.GetContentType(),
		Metadata:     s3sdk.CloneStringMap(meta.GetMetadata()),
		StorageClass: meta.GetStorageClass(),
	}
	if ts := meta.GetLastModified(); ts != nil {
		out.LastModified = ts.AsTime()
	}
	return out
}

func requiredObjectMeta(meta *proto.S3ObjectMeta, op string) (s3sdk.ObjectMeta, error) {
	if meta == nil {
		return s3sdk.ObjectMeta{}, fmt.Errorf("s3: %s response missing metadata", op)
	}
	return objectMetaFromProto(meta), nil
}

func objectMetaToProto(meta s3sdk.ObjectMeta) *proto.S3ObjectMeta {
	out := &proto.S3ObjectMeta{
		Ref:          objectRefToProto(meta.Ref),
		Etag:         meta.ETag,
		Size:         meta.Size,
		ContentType:  meta.ContentType,
		Metadata:     s3sdk.CloneStringMap(meta.Metadata),
		StorageClass: meta.StorageClass,
	}
	if !meta.LastModified.IsZero() {
		out.LastModified = timestamppb.New(meta.LastModified)
	}
	return out
}

func readObjectRequestToProto(req s3sdk.ReadRequest) *proto.ReadObjectRequest {
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

func byteRangeToProto(r *s3sdk.ByteRange) *proto.ByteRange {
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

func byteRangeFromProto(r *proto.ByteRange) *s3sdk.ByteRange {
	if r == nil {
		return nil
	}
	out := &s3sdk.ByteRange{}
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

func writeObjectOpenToProto(req s3sdk.WriteRequest) *proto.WriteObjectOpen {
	return &proto.WriteObjectOpen{
		Ref:                objectRefToProto(req.Ref),
		ContentType:        req.ContentType,
		CacheControl:       req.CacheControl,
		ContentDisposition: req.ContentDisposition,
		ContentEncoding:    req.ContentEncoding,
		ContentLanguage:    req.ContentLanguage,
		Metadata:           s3sdk.CloneStringMap(req.Metadata),
		IfMatch:            req.IfMatch,
		IfNoneMatch:        req.IfNoneMatch,
	}
}

func listObjectsRequestToProto(req s3sdk.ListRequest) *proto.ListObjectsRequest {
	return &proto.ListObjectsRequest{
		Bucket:            req.Bucket,
		Prefix:            req.Prefix,
		Delimiter:         req.Delimiter,
		ContinuationToken: req.ContinuationToken,
		StartAfter:        req.StartAfter,
		MaxKeys:           req.MaxKeys,
	}
}

func listPageFromProto(resp *proto.ListObjectsResponse) s3sdk.ListPage {
	if resp == nil {
		return s3sdk.ListPage{}
	}
	out := s3sdk.ListPage{
		CommonPrefixes:        append([]string(nil), resp.GetCommonPrefixes()...),
		NextContinuationToken: resp.GetNextContinuationToken(),
		HasMore:               resp.GetHasMore(),
	}
	out.Objects = make([]s3sdk.ObjectMeta, 0, len(resp.GetObjects()))
	for _, obj := range resp.GetObjects() {
		out.Objects = append(out.Objects, objectMetaFromProto(obj))
	}
	return out
}

func copyObjectRequestToProto(req s3sdk.CopyRequest) *proto.CopyObjectRequest {
	return &proto.CopyObjectRequest{
		Source:      objectRefToProto(req.Source),
		Destination: objectRefToProto(req.Destination),
		IfMatch:     req.IfMatch,
		IfNoneMatch: req.IfNoneMatch,
	}
}

func presignMethodToProto(method s3sdk.PresignMethod) proto.PresignMethod {
	switch method {
	case s3sdk.PresignMethodGet:
		return proto.PresignMethod_PRESIGN_METHOD_GET
	case s3sdk.PresignMethodPut:
		return proto.PresignMethod_PRESIGN_METHOD_PUT
	case s3sdk.PresignMethodDelete:
		return proto.PresignMethod_PRESIGN_METHOD_DELETE
	case s3sdk.PresignMethodHead:
		return proto.PresignMethod_PRESIGN_METHOD_HEAD
	default:
		return proto.PresignMethod_PRESIGN_METHOD_UNSPECIFIED
	}
}

func presignMethodFromProto(method proto.PresignMethod) s3sdk.PresignMethod {
	switch method {
	case proto.PresignMethod_PRESIGN_METHOD_GET:
		return s3sdk.PresignMethodGet
	case proto.PresignMethod_PRESIGN_METHOD_PUT:
		return s3sdk.PresignMethodPut
	case proto.PresignMethod_PRESIGN_METHOD_DELETE:
		return s3sdk.PresignMethodDelete
	case proto.PresignMethod_PRESIGN_METHOD_HEAD:
		return s3sdk.PresignMethodHead
	default:
		return ""
	}
}

func presignRequestToProto(req s3sdk.PresignRequest) *proto.PresignObjectRequest {
	return &proto.PresignObjectRequest{
		Ref:                objectRefToProto(req.Ref),
		Method:             presignMethodToProto(req.Method),
		ExpiresSeconds:     int64(req.Expires / time.Second),
		ContentType:        req.ContentType,
		ContentDisposition: req.ContentDisposition,
		Headers:            s3sdk.CloneStringMap(req.Headers),
	}
}

func presignResultFromProto(resp *proto.PresignObjectResponse, requested s3sdk.PresignMethod) s3sdk.PresignResult {
	if resp == nil {
		return s3sdk.PresignResult{}
	}
	method := presignMethodFromProto(resp.GetMethod())
	if method == "" {
		method = requested
	}
	out := s3sdk.PresignResult{
		URL:     resp.GetUrl(),
		Method:  method,
		Headers: s3sdk.CloneStringMap(resp.GetHeaders()),
	}
	if ts := resp.GetExpiresAt(); ts != nil {
		out.ExpiresAt = ts.AsTime()
	}
	return out
}
