package s3

import (
	"fmt"
	"time"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func objectRefToProto(ref ObjectRef) *proto.S3ObjectRef {
	return &proto.S3ObjectRef{
		Key:       ref.Key,
		VersionId: ref.VersionID,
	}
}

func objectRefFromProto(ref *proto.S3ObjectRef) ObjectRef {
	if ref == nil {
		return ObjectRef{}
	}
	return ObjectRef{
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

func firstS3Name(name []string) string {
	if len(name) == 0 {
		return ""
	}
	return name[0]
}

func grpcS3Err(err error) error {
	return ClientError(err)
}
