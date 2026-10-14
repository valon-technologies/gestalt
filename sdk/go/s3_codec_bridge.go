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

func readRequestFromProto(req *proto.ReadObjectRequest) ReadRequest {
	return s3.ReadRequestFromProto(req)
}

func writeRequestFromProto(open *proto.WriteObjectOpen) WriteRequest {
	return s3.WriteRequestFromProto(open)
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
