package providerhost

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core/fileapi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

const fileAPIReadChunkSize = 64 * 1024

type fileAPIServer struct {
	proto.UnimplementedFileAPIServer
	api      fileapi.FileAPI
	idPrefix string

	urlMu    sync.RWMutex
	nextURL  int
	urlMap   map[string]string
}

func NewFileAPIServer(api fileapi.FileAPI, pluginName string) proto.FileAPIServer {
	prefix := ""
	if pluginName != "" {
		prefix = "plugin_" + pluginName + "_"
	}
	return &fileAPIServer{api: api, idPrefix: prefix}
}

func (s *fileAPIServer) CreateBlob(ctx context.Context, req *proto.CreateBlobRequest) (*proto.FileObjectResponse, error) {
	info, err := s.api.CreateBlob(ctx, protoToBlobParts(req.GetParts()), protoToBlobOptions(req.GetOptions()))
	if err != nil {
		return nil, fileapiToGRPCErr(err)
	}
	return &proto.FileObjectResponse{Object: objectInfoToProto(s.addIDPrefix(info))}, nil
}

func (s *fileAPIServer) CreateFile(ctx context.Context, req *proto.CreateFileRequest) (*proto.FileObjectResponse, error) {
	info, err := s.api.CreateFile(ctx, protoToBlobParts(req.GetFileBits()), req.GetFileName(), protoToFileOptions(req.GetOptions()))
	if err != nil {
		return nil, fileapiToGRPCErr(err)
	}
	return &proto.FileObjectResponse{Object: objectInfoToProto(s.addIDPrefix(info))}, nil
}

func (s *fileAPIServer) Stat(ctx context.Context, req *proto.FileObjectRequest) (*proto.FileObjectResponse, error) {
	id, err := s.stripID(req.GetId())
	if err != nil {
		return nil, fileapiToGRPCErr(err)
	}
	info, err := s.api.Stat(ctx, id)
	if err != nil {
		return nil, fileapiToGRPCErr(err)
	}
	return &proto.FileObjectResponse{Object: objectInfoToProto(s.addIDPrefix(info))}, nil
}

func (s *fileAPIServer) Slice(ctx context.Context, req *proto.SliceRequest) (*proto.FileObjectResponse, error) {
	id, err := s.stripID(req.GetId())
	if err != nil {
		return nil, fileapiToGRPCErr(err)
	}
	info, err := s.api.Slice(ctx, id, req.Start, req.End, req.GetContentType())
	if err != nil {
		return nil, fileapiToGRPCErr(err)
	}
	return &proto.FileObjectResponse{Object: objectInfoToProto(s.addIDPrefix(info))}, nil
}

func (s *fileAPIServer) ReadBytes(ctx context.Context, req *proto.FileObjectRequest) (*proto.BytesResponse, error) {
	id, err := s.stripID(req.GetId())
	if err != nil {
		return nil, fileapiToGRPCErr(err)
	}
	data, err := s.api.ReadBytes(ctx, id)
	if err != nil {
		return nil, fileapiToGRPCErr(err)
	}
	return &proto.BytesResponse{Data: data}, nil
}

func (s *fileAPIServer) OpenReadStream(req *proto.ReadStreamRequest, stream proto.FileAPI_OpenReadStreamServer) error {
	id, err := s.stripID(req.GetId())
	if err != nil {
		return fileapiToGRPCErr(err)
	}
	data, err := s.api.ReadBytes(stream.Context(), id)
	if err != nil {
		return fileapiToGRPCErr(err)
	}
	for len(data) > 0 {
		chunk := data
		if len(chunk) > fileAPIReadChunkSize {
			chunk = chunk[:fileAPIReadChunkSize]
		}
		if err := stream.Send(&proto.ReadChunk{Data: chunk}); err != nil {
			return err
		}
		data = data[len(chunk):]
	}
	return nil
}

func (s *fileAPIServer) CreateObjectURL(ctx context.Context, req *proto.CreateObjectURLRequest) (*proto.ObjectURLResponse, error) {
	id, err := s.stripID(req.GetId())
	if err != nil {
		return nil, fileapiToGRPCErr(err)
	}
	url, err := s.api.CreateObjectURL(ctx, id)
	if err != nil {
		return nil, fileapiToGRPCErr(err)
	}
	return &proto.ObjectURLResponse{Url: s.wrapURL(url)}, nil
}

func (s *fileAPIServer) ResolveObjectURL(ctx context.Context, req *proto.ObjectURLRequest) (*proto.FileObjectResponse, error) {
	url, err := s.unwrapURL(req.GetUrl())
	if err != nil {
		return nil, fileapiToGRPCErr(err)
	}
	info, err := s.api.ResolveObjectURL(ctx, url)
	if err != nil {
		return nil, fileapiToGRPCErr(err)
	}
	return &proto.FileObjectResponse{Object: objectInfoToProto(s.addIDPrefix(info))}, nil
}

func (s *fileAPIServer) RevokeObjectURL(ctx context.Context, req *proto.ObjectURLRequest) (*emptypb.Empty, error) {
	url, err := s.unwrapURL(req.GetUrl())
	if err != nil {
		return nil, fileapiToGRPCErr(err)
	}
	if err := s.api.RevokeObjectURL(ctx, url); err != nil {
		return nil, fileapiToGRPCErr(err)
	}
	s.deleteWrappedURL(req.GetUrl())
	return &emptypb.Empty{}, nil
}

func protoToBlobParts(parts []*proto.BlobPart) []fileapi.BlobPart {
	if len(parts) == 0 {
		return nil
	}
	out := make([]fileapi.BlobPart, 0, len(parts))
	for _, part := range parts {
		switch value := part.GetKind().(type) {
		case *proto.BlobPart_StringData:
			out = append(out, fileapi.StringPart(value.StringData))
		case *proto.BlobPart_BytesData:
			out = append(out, fileapi.BytesPart(value.BytesData))
		case *proto.BlobPart_BlobId:
			out = append(out, fileapi.BlobRefPart(value.BlobId))
		}
	}
	return out
}

func protoToBlobOptions(options *proto.BlobOptions) fileapi.BlobOptions {
	if options == nil {
		return fileapi.BlobOptions{}
	}
	return fileapi.BlobOptions{
		Type:    options.GetMimeType(),
		Endings: protoToLineEndings(options.GetEndings()),
	}
}

func protoToFileOptions(options *proto.FileOptions) fileapi.FileOptions {
	if options == nil {
		return fileapi.FileOptions{}
	}
	return fileapi.FileOptions{
		Type:         options.GetMimeType(),
		Endings:      protoToLineEndings(options.GetEndings()),
		LastModified: options.GetLastModified(),
	}
}

func objectInfoToProto(info fileapi.ObjectInfo) *proto.FileObject {
	objectKind := proto.FileObjectKind_FILE_OBJECT_KIND_BLOB
	if info.Kind == fileapi.ObjectKindFile {
		objectKind = proto.FileObjectKind_FILE_OBJECT_KIND_FILE
	}
	return &proto.FileObject{
		Id:           info.ID,
		Kind:         objectKind,
		Size:         info.Size,
		Type:         info.Type,
		Name:         info.Name,
		LastModified: info.LastModified,
	}
}

func protoToLineEndings(endings proto.LineEndings) fileapi.LineEndings {
	switch endings {
	case proto.LineEndings_LINE_ENDINGS_NATIVE:
		return fileapi.LineEndingsNative
	default:
		return fileapi.LineEndingsTransparent
	}
}

func fileapiToGRPCErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, fileapi.ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, fileapi.ErrSecurity):
		return status.Error(codes.PermissionDenied, err.Error())
	default:
		return err
	}
}

func (s *fileAPIServer) addIDPrefix(info fileapi.ObjectInfo) fileapi.ObjectInfo {
	if s.idPrefix == "" {
		return info
	}
	info.ID = s.idPrefix + info.ID
	return info
}

func (s *fileAPIServer) stripID(id string) (string, error) {
	if s.idPrefix == "" {
		return id, nil
	}
	if !strings.HasPrefix(id, s.idPrefix) {
		return "", fileapi.ErrSecurity
	}
	return strings.TrimPrefix(id, s.idPrefix), nil
}

func (s *fileAPIServer) wrapURL(url string) string {
	if s.idPrefix == "" {
		return url
	}
	s.urlMu.Lock()
	defer s.urlMu.Unlock()
	if s.urlMap == nil {
		s.urlMap = make(map[string]string)
	}
	s.nextURL++
	wrapped := fmt.Sprintf("blob:gestalt:%s%d", s.idPrefix, s.nextURL)
	s.urlMap[wrapped] = url
	return wrapped
}

func (s *fileAPIServer) unwrapURL(url string) (string, error) {
	if s.idPrefix == "" {
		return url, nil
	}
	s.urlMu.RLock()
	defer s.urlMu.RUnlock()
	unwrapped, ok := s.urlMap[url]
	if !ok {
		return "", fileapi.ErrNotFound
	}
	return unwrapped, nil
}

func (s *fileAPIServer) deleteWrappedURL(url string) {
	if s.idPrefix == "" {
		return
	}
	s.urlMu.Lock()
	defer s.urlMu.Unlock()
	delete(s.urlMap, url)
}
