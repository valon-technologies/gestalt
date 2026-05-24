package gestalt

import (
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/sdk/go/s3"
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

func readOptionsFromProto(req *proto.ReadObjectRequest) *ReadOptions {
	return s3.ReadOptionsFromProto(req)
}

func writeOptionsFromProto(open *proto.WriteObjectOpen) *WriteOptions {
	return s3.WriteOptionsFromProto(open)
}

func listPageFromProto(resp *proto.ListObjectsResponse) ListPage {
	return s3.ListPageFromProto(resp)
}

func listPageToProto(page ListPage) *proto.ListObjectsResponse {
	return s3.ListPageToProto(page)
}

func presignOptionsFromProto(req *proto.PresignObjectRequest) *PresignOptions {
	return s3.PresignOptionsFromProto(req)
}

func presignResultToProto(result PresignResult) *proto.PresignObjectResponse {
	return s3.PresignResultToProto(result)
}

func presignResultFromProto(resp *proto.PresignObjectResponse, requested PresignMethod) PresignResult {
	return s3.PresignResultFromProto(resp, requested)
}

func objectAccessURLFromProto(resp *proto.CreateObjectAccessURLResponse, requested PresignMethod) ObjectAccessURL {
	return s3.ObjectAccessURLFromProto(resp, requested)
}
