package s3

import (
	s3sdk "github.com/valon-technologies/gestalt/sdk/go/s3"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func objectRefToProto(ref s3sdk.ObjectRef) *proto.S3ObjectRef {
	return &proto.S3ObjectRef{
		Key:       ref.Key,
		VersionId: ref.VersionID,
	}
}

func objectRefFromProto(ref *proto.S3ObjectRef) s3sdk.ObjectRef {
	if ref == nil {
		return s3sdk.ObjectRef{}
	}
	return s3sdk.ObjectRef{
		Key:       ref.GetKey(),
		VersionID: ref.GetVersionId(),
	}
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
