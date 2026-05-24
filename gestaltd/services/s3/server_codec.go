package s3

import (
	s3sdk "github.com/valon-technologies/gestalt/sdk/go/s3"
	s3host "github.com/valon-technologies/gestalt/sdk/go/s3/host"
	proto "github.com/valon-technologies/gestalt/sdk/go/protov1/v1"
)

func objectRefFromProto(ref *proto.S3ObjectRef) s3sdk.ObjectRef {
	return s3host.ObjectRefFromProto(ref)
}

func objectMetaToProto(meta s3sdk.ObjectMeta) *proto.S3ObjectMeta {
	return s3host.ObjectMetaToProto(meta)
}

func objectMetaFromProto(meta *proto.S3ObjectMeta) s3sdk.ObjectMeta {
	return s3host.ObjectMetaFromProto(meta)
}

func readOptionsFromProto(req *proto.ReadObjectRequest) *s3sdk.ReadOptions {
	return s3host.ReadOptionsFromProto(req)
}

func writeOptionsFromProto(open *proto.WriteObjectOpen) *s3sdk.WriteOptions {
	return s3host.WriteOptionsFromProto(open)
}

func listPageFromProto(resp *proto.ListObjectsResponse) s3sdk.ListPage {
	return s3host.ListPageFromProto(resp)
}

func listPageToProto(page s3sdk.ListPage) *proto.ListObjectsResponse {
	return s3host.ListPageToProto(page)
}

func presignOptionsFromProto(req *proto.PresignObjectRequest) *s3sdk.PresignOptions {
	return s3host.PresignOptionsFromProto(req)
}

func presignResultToProto(result s3sdk.PresignResult) *proto.PresignObjectResponse {
	return s3host.PresignResultToProto(result)
}

func presignResultFromProto(resp *proto.PresignObjectResponse, requested s3sdk.PresignMethod) s3sdk.PresignResult {
	return s3host.PresignResultFromProto(resp, requested)
}

func objectAccessURLFromProto(resp *proto.CreateObjectAccessURLResponse, requested s3sdk.PresignMethod) s3sdk.ObjectAccessURL {
	return s3host.ObjectAccessURLFromProto(resp, requested)
}

func presignMethodToProto(method s3sdk.PresignMethod) proto.PresignMethod {
	return s3host.PresignMethodToProto(method)
}

func presignMethodFromProto(method proto.PresignMethod) s3sdk.PresignMethod {
	return s3host.PresignMethodFromProto(method)
}

func byteRangeFromProto(r *proto.ByteRange) *s3sdk.ByteRange {
	return s3host.ByteRangeFromProto(r)
}
