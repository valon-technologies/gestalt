package s3

import proto "github.com/valon-technologies/gestalt/sdk/go/internal/gen/v1"

// ObjectRefFromProto decodes an object reference.
func ObjectRefFromProto(ref *proto.S3ObjectRef) ObjectRef {
	return objectRefFromProto(ref)
}

// ObjectMetaToProto encodes object metadata.
func ObjectMetaToProto(meta ObjectMeta) *proto.S3ObjectMeta {
	return objectMetaToProto(meta)
}

// ObjectMetaFromProto decodes object metadata.
func ObjectMetaFromProto(meta *proto.S3ObjectMeta) ObjectMeta {
	return objectMetaFromProto(meta)
}

// ReadOptionsFromProto decodes read options from a protobuf read request.
func ReadOptionsFromProto(req *proto.ReadObjectRequest) *ReadOptions {
	return readOptionsFromProto(req)
}

// WriteOptionsFromProto decodes write options.
func WriteOptionsFromProto(open *proto.WriteObjectOpen) *WriteOptions {
	return writeOptionsFromProto(open)
}

// ListPageFromProto decodes a list page.
func ListPageFromProto(resp *proto.ListObjectsResponse) ListPage {
	return listPageFromProto(resp)
}

// ListPageToProto encodes a list page.
func ListPageToProto(page ListPage) *proto.ListObjectsResponse {
	return listPageToProto(page)
}

// PresignOptionsFromProto decodes presign options.
func PresignOptionsFromProto(req *proto.PresignObjectRequest) *PresignOptions {
	return presignOptionsFromProto(req)
}

// PresignResultToProto encodes a presign result.
func PresignResultToProto(result PresignResult) *proto.PresignObjectResponse {
	return presignResultToProto(result)
}

// PresignResultFromProto decodes a presign result.
func PresignResultFromProto(resp *proto.PresignObjectResponse, requested PresignMethod) PresignResult {
	return presignResultFromProto(resp, requested)
}

// ObjectAccessURLFromProto decodes an object access URL response.
func ObjectAccessURLFromProto(resp *proto.CreateObjectAccessURLResponse, requested PresignMethod) ObjectAccessURL {
	return objectAccessURLFromProto(resp, requested)
}
