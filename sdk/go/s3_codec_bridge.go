package gestalt

import (
	proto "github.com/valon-technologies/gestalt/sdk/go/internal/gen/v1"
	"github.com/valon-technologies/gestalt/sdk/go/internal/hosts3"
)

func objectRefFromProto(ref *proto.S3ObjectRef) ObjectRef {
	return hosts3.ObjectRefFromProto(ref)
}

func objectMetaToProto(meta ObjectMeta) *proto.S3ObjectMeta {
	return hosts3.ObjectMetaToProto(meta)
}

func objectMetaFromProto(meta *proto.S3ObjectMeta) ObjectMeta {
	return hosts3.ObjectMetaFromProto(meta)
}

func readOptionsFromProto(req *proto.ReadObjectRequest) *ReadOptions {
	return hosts3.ReadOptionsFromProto(req)
}

func writeOptionsFromProto(open *proto.WriteObjectOpen) *WriteOptions {
	return hosts3.WriteOptionsFromProto(open)
}

func listPageFromProto(resp *proto.ListObjectsResponse) ListPage {
	return hosts3.ListPageFromProto(resp)
}

func listPageToProto(page ListPage) *proto.ListObjectsResponse {
	return hosts3.ListPageToProto(page)
}

func presignOptionsFromProto(req *proto.PresignObjectRequest) *PresignOptions {
	return hosts3.PresignOptionsFromProto(req)
}

func presignResultToProto(result PresignResult) *proto.PresignObjectResponse {
	return hosts3.PresignResultToProto(result)
}

func presignResultFromProto(resp *proto.PresignObjectResponse, requested PresignMethod) PresignResult {
	return hosts3.PresignResultFromProto(resp, requested)
}

func objectAccessURLFromProto(resp *proto.CreateObjectAccessURLResponse, requested PresignMethod) ObjectAccessURL {
	return hosts3.ObjectAccessURLFromProto(resp, requested)
}
