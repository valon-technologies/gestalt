package gestalt

import (
	"github.com/valon-technologies/gestalt/sdk/go/s3"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

func objectRefFromProto(ref *proto.S3ObjectRef) ObjectRef {
	return s3.ObjectRefFromProto(ref)
}

func objectMetaToProto(meta ObjectMeta) *proto.S3ObjectMeta {
	return s3.ObjectMetaToProto(meta)
}

func objectMetaFromProto(meta *proto.S3ObjectMeta) ObjectMeta {
	return s3.ObjectMetaFromProto(meta)
}

func readRequestFromProto(req *proto.ReadObjectRequest) ReadRequest {
	return s3.ReadRequestFromProto(req)
}

func writeRequestFromProto(open *proto.WriteObjectOpen) WriteRequest {
	return s3.WriteRequestFromProto(open)
}

func listPageFromProto(resp *proto.ListObjectsResponse) ListPage {
	return s3.ListPageFromProto(resp)
}

func listPageToProto(page ListPage) *proto.ListObjectsResponse {
	return s3.ListPageToProto(page)
}

func presignRequestFromProto(req *proto.PresignObjectRequest) PresignRequest {
	return s3.PresignRequestFromProto(req)
}

func presignResultToProto(result PresignResult) *proto.PresignObjectResponse {
	return s3.PresignResultToProto(result)
}

func presignResultFromProto(resp *proto.PresignObjectResponse, requested PresignMethod) PresignResult {
	return s3.PresignResultFromProto(resp, requested)
}

func objectAccessURLFromProto(resp *proto.CreateObjectAccessURLResponse, requested PresignMethod) PresignResult {
	return s3.PresignResultFromObjectAccessURLProto(resp, requested)
}
