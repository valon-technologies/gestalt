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
	"google.golang.org/protobuf/types/known/timestamppb"
)

const defaultFileAPIChunkSize = 64 * 1024

type fileAPIServer struct {
	proto.UnimplementedFileAPIServer

	api       fileapi.FileAPI
	prefix    string
	mu        sync.Mutex
	urls      map[string]string
	nextURLID int64
}

func NewFileAPIServer(api fileapi.FileAPI, pluginName, bindingName string) proto.FileAPIServer {
	prefix := ""
	if pluginName != "" {
		prefix = "plugin_" + pluginName + "_"
		if bindingName != "" {
			prefix += bindingName + "_"
		}
	}
	return &fileAPIServer{
		api:    api,
		prefix: prefix,
	}
}

func (s *fileAPIServer) CreateBlob(ctx context.Context, req *proto.CreateBlobRequest) (*proto.FileObjectResponse, error) {
	parts, err := s.protoToBlobParts(req.GetParts())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unmarshal blob parts: %v", err)
	}
	info, err := s.api.CreateBlob(ctx, parts, fileapi.BlobOptions{
		Type:    req.GetOptions().GetMimeType(),
		Endings: protoToLineEndings(req.GetOptions().GetEndings()),
	})
	if err != nil {
		return nil, fileapiToGRPCErr(err)
	}
	return &proto.FileObjectResponse{Object: s.objectInfoToProto(info)}, nil
}

func (s *fileAPIServer) CreateFile(ctx context.Context, req *proto.CreateFileRequest) (*proto.FileObjectResponse, error) {
	parts, err := s.protoToBlobParts(req.GetParts())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unmarshal blob parts: %v", err)
	}
	opts := fileapi.FileOptions{
		Type:    req.GetOptions().GetMimeType(),
		Endings: protoToLineEndings(req.GetOptions().GetEndings()),
	}
	if ts := req.GetOptions().GetLastModified(); ts != nil {
		opts.LastModified = ts.AsTime().UTC()
	}
	info, err := s.api.CreateFile(ctx, parts, req.GetName(), opts)
	if err != nil {
		return nil, fileapiToGRPCErr(err)
	}
	return &proto.FileObjectResponse{Object: s.objectInfoToProto(info)}, nil
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
	return &proto.FileObjectResponse{Object: s.objectInfoToProto(info)}, nil
}

func (s *fileAPIServer) Slice(ctx context.Context, req *proto.SliceRequest) (*proto.FileObjectResponse, error) {
	id, err := s.stripID(req.GetId())
	if err != nil {
		return nil, fileapiToGRPCErr(err)
	}
	info, err := s.api.Slice(ctx, id, req.Start, req.End, req.GetMimeType())
	if err != nil {
		return nil, fileapiToGRPCErr(err)
	}
	return &proto.FileObjectResponse{Object: s.objectInfoToProto(info)}, nil
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
	chunkSize := int(req.GetChunkSize())
	if chunkSize <= 0 {
		chunkSize = defaultFileAPIChunkSize
	}
	for len(data) > 0 {
		n := chunkSize
		if n > len(data) {
			n = len(data)
		}
		if err := stream.Send(&proto.ReadChunk{Data: data[:n]}); err != nil {
			return err
		}
		data = data[n:]
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
	return &proto.FileObjectResponse{Object: s.objectInfoToProto(info)}, nil
}

func (s *fileAPIServer) RevokeObjectURL(ctx context.Context, req *proto.ObjectURLRequest) (*emptypb.Empty, error) {
	url, ok := s.revokeWrappedURL(req.GetUrl())
	if !ok {
		return &emptypb.Empty{}, nil
	}
	if err := s.api.RevokeObjectURL(ctx, url); err != nil && !errors.Is(err, fileapi.ErrNotFound) {
		return nil, fileapiToGRPCErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *fileAPIServer) protoToBlobParts(parts []*proto.BlobPart) ([]fileapi.BlobPart, error) {
	if len(parts) == 0 {
		return nil, nil
	}
	out := make([]fileapi.BlobPart, 0, len(parts))
	for _, part := range parts {
		switch item := part.GetKind().(type) {
		case *proto.BlobPart_StringData:
			out = append(out, fileapi.StringPart(item.StringData))
		case *proto.BlobPart_BytesData:
			out = append(out, fileapi.BytesPart(item.BytesData))
		case *proto.BlobPart_BlobId:
			id, err := s.stripID(item.BlobId)
			if err != nil {
				return nil, err
			}
			out = append(out, fileapi.BlobRefPart(id))
		default:
			return nil, fmt.Errorf("unsupported blob part")
		}
	}
	return out, nil
}

func (s *fileAPIServer) objectInfoToProto(info fileapi.ObjectInfo) *proto.FileObject {
	item := &proto.FileObject{
		Id:       s.addIDPrefix(info.ID),
		Size:     uint64(info.Size),
		MimeType: info.Type,
		Name:     info.Name,
	}
	switch info.Kind {
	case fileapi.ObjectKindFile:
		item.Kind = proto.FileObjectKind_FILE_OBJECT_KIND_FILE
		if !info.LastModified.IsZero() {
			item.LastModified = timestamppb.New(info.LastModified)
		}
	default:
		item.Kind = proto.FileObjectKind_FILE_OBJECT_KIND_BLOB
	}
	return item
}

func (s *fileAPIServer) addIDPrefix(id string) string {
	if s.prefix == "" || id == "" {
		return id
	}
	return s.prefix + id
}

func (s *fileAPIServer) stripID(id string) (string, error) {
	if s.prefix == "" {
		return id, nil
	}
	if !strings.HasPrefix(id, s.prefix) {
		return "", fileapi.ErrNotFound
	}
	return strings.TrimPrefix(id, s.prefix), nil
}

func (s *fileAPIServer) wrapURL(raw string) string {
	if s.prefix == "" {
		return raw
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.urls == nil {
		s.urls = make(map[string]string)
	}
	s.nextURLID++
	exposed := fmt.Sprintf("blob:gestalt/%surl-%d", s.prefix, s.nextURLID)
	s.urls[exposed] = raw
	return exposed
}

func (s *fileAPIServer) lookupWrappedURL(url string) (string, bool) {
	if s.prefix == "" {
		return url, true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, ok := s.urls[url]
	return raw, ok
}

func (s *fileAPIServer) revokeWrappedURL(url string) (string, bool) {
	if s.prefix == "" {
		return url, true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, ok := s.urls[url]
	if ok {
		delete(s.urls, url)
	}
	return raw, ok
}

func (s *fileAPIServer) unwrapURL(url string) (string, error) {
	raw, ok := s.lookupWrappedURL(url)
	if !ok {
		return "", fileapi.ErrNotFound
	}
	return raw, nil
}

func protoToLineEndings(value proto.LineEndings) fileapi.LineEndings {
	switch value {
	case proto.LineEndings_LINE_ENDINGS_NATIVE:
		return fileapi.LineEndingsNative
	default:
		return fileapi.LineEndingsTransparent
	}
}

func fileapiToGRPCErr(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, fileapi.ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, fileapi.ErrSecurity):
		return status.Error(codes.PermissionDenied, err.Error())
	case errors.Is(err, fileapi.ErrNotReadable):
		return status.Error(codes.FailedPrecondition, err.Error())
	default:
		return status.Error(codes.Internal, err.Error())
	}
}
