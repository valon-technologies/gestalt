package gestalt

import (
	proto "github.com/valon-technologies/gestalt/sdk/go/protov1/v1"
	"github.com/valon-technologies/gestalt/sdk/go/s3/host"
)

func objectRefFromProto(ref *proto.S3ObjectRef) ObjectRef {
	return host.ObjectRefFromProto(ref)
}

func objectMetaToProto(meta ObjectMeta) *proto.S3ObjectMeta {
	return host.ObjectMetaToProto(meta)
}

func objectMetaFromProto(meta *proto.S3ObjectMeta) ObjectMeta {
	return host.ObjectMetaFromProto(meta)
}

func readOptionsFromProto(req *proto.ReadObjectRequest) *ReadOptions {
	return host.ReadOptionsFromProto(req)
}

func writeOptionsFromProto(open *proto.WriteObjectOpen) *WriteOptions {
	return host.WriteOptionsFromProto(open)
}

func listPageFromProto(resp *proto.ListObjectsResponse) ListPage {
	return host.ListPageFromProto(resp)
}

func listPageToProto(page ListPage) *proto.ListObjectsResponse {
	return host.ListPageToProto(page)
}

func presignOptionsFromProto(req *proto.PresignObjectRequest) *PresignOptions {
	return host.PresignOptionsFromProto(req)
}

func presignResultToProto(result PresignResult) *proto.PresignObjectResponse {
	return host.PresignResultToProto(result)
}

func presignResultFromProto(resp *proto.PresignObjectResponse, requested PresignMethod) PresignResult {
	return host.PresignResultFromProto(resp, requested)
}

func objectAccessURLFromProto(resp *proto.CreateObjectAccessURLResponse, requested PresignMethod) ObjectAccessURL {
	return host.ObjectAccessURLFromProto(resp, requested)
}
