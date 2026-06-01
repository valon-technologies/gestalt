package s3

import proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"

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

// ReadRequestFromProto decodes a protobuf read request.
func ReadRequestFromProto(req *proto.ReadObjectRequest) ReadRequest {
	return readRequestFromProto(req)
}

// WriteRequestFromProto decodes a protobuf write-open message.
func WriteRequestFromProto(open *proto.WriteObjectOpen) WriteRequest {
	return writeRequestFromProto(open)
}

// ListPageFromProto decodes a list page.
func ListPageFromProto(resp *proto.ListObjectsResponse) ListPage {
	return listPageFromProto(resp)
}

// ListPageToProto encodes a list page.
func ListPageToProto(page ListPage) *proto.ListObjectsResponse {
	return listPageToProto(page)
}

// PresignRequestFromProto decodes a protobuf presign request.
func PresignRequestFromProto(req *proto.PresignObjectRequest) PresignRequest {
	return presignRequestFromProto(req)
}

// PresignResultToProto encodes a presign result.
func PresignResultToProto(result PresignResult) *proto.PresignObjectResponse {
	return presignResultToProto(result)
}

// PresignResultFromProto decodes a presign result.
func PresignResultFromProto(resp *proto.PresignObjectResponse, requested PresignMethod) PresignResult {
	return presignResultFromProto(resp, requested)
}

// PresignResultFromObjectAccessURLProto decodes an object access URL response.
func PresignResultFromObjectAccessURLProto(resp *proto.CreateObjectAccessURLResponse, requested PresignMethod) PresignResult {
	return objectAccessURLFromProto(resp, requested)
}
