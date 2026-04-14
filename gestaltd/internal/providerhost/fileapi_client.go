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
	"google.golang.org/protobuf/types/known/timestamppb"
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
	fileAPIClient := proto.NewFileAPIClient(proc.conn)

	_, err = configureRuntimeProvider(ctx, runtimeClient, proto.ProviderKind_PROVIDER_KIND_FILEAPI, cfg.Name, cfg.Config)
	if err != nil {
		_ = proc.Close()
		return nil, err
	}

	return &remoteFileAPI{client: fileAPIClient, runtime: runtimeClient, closer: proc}, nil
}

func (r *remoteFileAPI) CreateBlob(ctx context.Context, parts []fileapi.BlobPart, opts fileapi.BlobOptions) (fileapi.ObjectInfo, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()

	resp, err := r.client.CreateBlob(ctx, &proto.CreateBlobRequest{
		Parts:   blobPartsToProto(parts),
		Options: blobOptionsToProto(opts),
	})
	if err != nil {
		return fileapi.ObjectInfo{}, grpcToFileAPIErr(err)
	}
	return objectInfoFromProto(resp.GetObject())
}

func (r *remoteFileAPI) CreateFile(ctx context.Context, parts []fileapi.BlobPart, name string, opts fileapi.FileOptions) (fileapi.ObjectInfo, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()

	resp, err := r.client.CreateFile(ctx, &proto.CreateFileRequest{
		Parts:   blobPartsToProto(parts),
		Name:    name,
		Options: fileOptionsToProto(opts),
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
		Id:       id,
		MimeType: contentType,
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
		item := &proto.BlobPart{}
		switch part.Kind() {
		case fileapi.BlobPartString:
			item.Kind = &proto.BlobPart_StringData{StringData: part.StringData()}
		case fileapi.BlobPartBytes:
			item.Kind = &proto.BlobPart_BytesData{BytesData: part.BytesData()}
		case fileapi.BlobPartBlob:
			item.Kind = &proto.BlobPart_BlobId{BlobId: part.BlobID()}
		}
		out = append(out, item)
	}
	return out
}

func blobOptionsToProto(opts fileapi.BlobOptions) *proto.BlobOptions {
	return &proto.BlobOptions{
		MimeType: opts.Type,
		Endings:  lineEndingsToProto(opts.Endings),
	}
}

func fileOptionsToProto(opts fileapi.FileOptions) *proto.FileOptions {
	item := &proto.FileOptions{
		MimeType: opts.Type,
		Endings:  lineEndingsToProto(opts.Endings),
	}
	if !opts.LastModified.IsZero() {
		item.LastModified = timestamppb.New(opts.LastModified)
	}
	return item
}

func objectInfoFromProto(obj *proto.FileObject) (fileapi.ObjectInfo, error) {
	if obj == nil {
		return fileapi.ObjectInfo{}, fmt.Errorf("file object is required")
	}
	info := fileapi.ObjectInfo{
		ID:   obj.GetId(),
		Size: int64(obj.GetSize()),
		Type: obj.GetMimeType(),
		Name: obj.GetName(),
	}
	switch obj.GetKind() {
	case proto.FileObjectKind_FILE_OBJECT_KIND_FILE:
		info.Kind = fileapi.ObjectKindFile
		if ts := obj.GetLastModified(); ts != nil {
			info.LastModified = ts.AsTime().UTC()
		}
	default:
		info.Kind = fileapi.ObjectKindBlob
	}
	return info, nil
}

func lineEndingsToProto(value fileapi.LineEndings) proto.LineEndings {
	switch value {
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
	case codes.FailedPrecondition:
		return fileapi.ErrNotReadable
	default:
		return err
	}
}

var _ fileapi.FileAPI = (*remoteFileAPI)(nil)
