package providerhost

import (
	"context"
	"fmt"
	"io"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core/fileapi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type FileAPIExecConfig struct {
	Command      string
	Args         []string
	Env          map[string]string
	Config       map[string]any
	AllowedHosts []string
	HostBinary   string
	Cleanup      func()
	Name         string
}

type remoteFileAPI struct {
	client  proto.FileAPIClient
	runtime proto.ProviderLifecycleClient
	closer  io.Closer
}

func NewExecutableFileAPI(ctx context.Context, cfg FileAPIExecConfig) (fileapi.FileAPI, error) {
	proc, err := startProviderProcess(ctx, ExecConfig{
		Command:      cfg.Command,
		Args:         cfg.Args,
		Env:          cfg.Env,
		Config:       cfg.Config,
		AllowedHosts: cfg.AllowedHosts,
		HostBinary:   cfg.HostBinary,
		Cleanup:      cfg.Cleanup,
	})
	if err != nil {
		return nil, err
	}

	runtimeClient := proto.NewProviderLifecycleClient(proc.conn)
	fileClient := proto.NewFileAPIClient(proc.conn)

	_, err = configureRuntimeProvider(ctx, runtimeClient, proto.ProviderKind_PROVIDER_KIND_FILEAPI, cfg.Name, cfg.Config)
	if err != nil {
		_ = proc.Close()
		return nil, err
	}

	return &remoteFileAPI{client: fileClient, runtime: runtimeClient, closer: proc}, nil
}

func (r *remoteFileAPI) CreateBlob(ctx context.Context, parts []fileapi.BlobPart, options fileapi.BlobOptions) (fileapi.ObjectInfo, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	resp, err := r.client.CreateBlob(ctx, &proto.CreateBlobRequest{
		Parts:   blobPartsToProto(parts),
		Options: blobOptionsToProto(options),
	})
	if err != nil {
		return fileapi.ObjectInfo{}, grpcToFileAPIErr(err)
	}
	return objectInfoFromProto(resp.GetObject())
}

func (r *remoteFileAPI) CreateFile(ctx context.Context, parts []fileapi.BlobPart, name string, options fileapi.FileOptions) (fileapi.ObjectInfo, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	resp, err := r.client.CreateFile(ctx, &proto.CreateFileRequest{
		FileBits: blobPartsToProto(parts),
		FileName: name,
		Options:  fileOptionsToProto(options),
	})
	if err != nil {
		return fileapi.ObjectInfo{}, grpcToFileAPIErr(err)
	}
	return objectInfoFromProto(resp.GetObject())
}

func (r *remoteFileAPI) Stat(ctx context.Context, id string) (fileapi.ObjectInfo, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	resp, err := r.client.Stat(ctx, &proto.FileObjectRequest{Id: id})
	if err != nil {
		return fileapi.ObjectInfo{}, grpcToFileAPIErr(err)
	}
	return objectInfoFromProto(resp.GetObject())
}

func (r *remoteFileAPI) Slice(ctx context.Context, id string, start, end *int64, contentType string) (fileapi.ObjectInfo, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	req := &proto.SliceRequest{
		Id:          id,
		ContentType: contentType,
	}
	if start != nil {
		req.Start = start
	}
	if end != nil {
		req.End = end
	}
	resp, err := r.client.Slice(ctx, req)
	if err != nil {
		return fileapi.ObjectInfo{}, grpcToFileAPIErr(err)
	}
	return objectInfoFromProto(resp.GetObject())
}

func (r *remoteFileAPI) ReadBytes(ctx context.Context, id string) ([]byte, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	resp, err := r.client.ReadBytes(ctx, &proto.FileObjectRequest{Id: id})
	if err != nil {
		return nil, grpcToFileAPIErr(err)
	}
	return append([]byte(nil), resp.GetData()...), nil
}

func (r *remoteFileAPI) CreateObjectURL(ctx context.Context, id string) (string, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	resp, err := r.client.CreateObjectURL(ctx, &proto.CreateObjectURLRequest{Id: id})
	if err != nil {
		return "", grpcToFileAPIErr(err)
	}
	return resp.GetUrl(), nil
}

func (r *remoteFileAPI) ResolveObjectURL(ctx context.Context, url string) (fileapi.ObjectInfo, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	resp, err := r.client.ResolveObjectURL(ctx, &proto.ObjectURLRequest{Url: url})
	if err != nil {
		return fileapi.ObjectInfo{}, grpcToFileAPIErr(err)
	}
	return objectInfoFromProto(resp.GetObject())
}

func (r *remoteFileAPI) RevokeObjectURL(ctx context.Context, url string) error {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	_, err := r.client.RevokeObjectURL(ctx, &proto.ObjectURLRequest{Url: url})
	return grpcToFileAPIErr(err)
}

func (r *remoteFileAPI) Ping(ctx context.Context) error {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()
	_, err := r.runtime.HealthCheck(ctx, &emptypb.Empty{})
	return err
}

func (r *remoteFileAPI) Close() error {
	if r == nil || r.closer == nil {
		return nil
	}
	return r.closer.Close()
}

func blobPartsToProto(parts []fileapi.BlobPart) []*proto.BlobPart {
	if len(parts) == 0 {
		return nil
	}
	out := make([]*proto.BlobPart, 0, len(parts))
	for _, part := range parts {
		switch part.Kind {
		case fileapi.BlobPartKindString:
			out = append(out, &proto.BlobPart{Kind: &proto.BlobPart_StringData{StringData: part.StringData}})
		case fileapi.BlobPartKindBytes:
			out = append(out, &proto.BlobPart{Kind: &proto.BlobPart_BytesData{BytesData: append([]byte(nil), part.BytesData...)}})
		case fileapi.BlobPartKindBlobID:
			out = append(out, &proto.BlobPart{Kind: &proto.BlobPart_BlobId{BlobId: part.BlobID}})
		}
	}
	return out
}

func blobOptionsToProto(options fileapi.BlobOptions) *proto.BlobOptions {
	return &proto.BlobOptions{
		MimeType: options.Type,
		Endings:  lineEndingsToProto(options.Endings),
	}
}

func fileOptionsToProto(options fileapi.FileOptions) *proto.FileOptions {
	return &proto.FileOptions{
		MimeType:     options.Type,
		Endings:      lineEndingsToProto(options.Endings),
		LastModified: options.LastModified,
	}
}

func objectInfoFromProto(object *proto.FileObject) (fileapi.ObjectInfo, error) {
	if object == nil {
		return fileapi.ObjectInfo{}, fmt.Errorf("fileapi: missing file object")
	}
	info := fileapi.ObjectInfo{
		ID:           object.GetId(),
		Size:         object.GetSize(),
		Type:         object.GetType(),
		Name:         object.GetName(),
		LastModified: object.GetLastModified(),
	}
	switch object.GetKind() {
	case proto.FileObjectKind_FILE_OBJECT_KIND_FILE:
		info.Kind = fileapi.ObjectKindFile
	default:
		info.Kind = fileapi.ObjectKindBlob
	}
	return info, nil
}

func lineEndingsToProto(endings fileapi.LineEndings) proto.LineEndings {
	switch endings {
	case fileapi.LineEndingsNative:
		return proto.LineEndings_LINE_ENDINGS_NATIVE
	default:
		return proto.LineEndings_LINE_ENDINGS_TRANSPARENT
	}
}

func grpcToFileAPIErr(err error) error {
	if err == nil {
		return nil
	}
	st, ok := status.FromError(err)
	if !ok {
		return err
	}
	switch st.Code() {
	case codes.NotFound:
		return fileapi.ErrNotFound
	case codes.PermissionDenied:
		return fileapi.ErrSecurity
	default:
		return err
	}
}
