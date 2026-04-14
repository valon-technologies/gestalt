package gestalt

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

const EnvFileAPISocket = "GESTALT_FILEAPI_SOCKET"

var (
	ErrFileAPINotFound = fmt.Errorf("fileapi: not found")
	ErrFileAPISecurity = fmt.Errorf("fileapi: security error")
)

type LineEndings string

const (
	LineEndingsTransparent LineEndings = "transparent"
	LineEndingsNative      LineEndings = "native"
)

type BlobPart struct {
	kind       string
	stringData string
	bytesData  []byte
	blobID     string
}

func StringBlobPart(value string) BlobPart {
	return BlobPart{kind: "string", stringData: value}
}

func BytesBlobPart(value []byte) BlobPart {
	return BlobPart{kind: "bytes", bytesData: append([]byte(nil), value...)}
}

func BlobRefPart(value *Blob) BlobPart {
	id := ""
	if value != nil {
		id = value.ID()
	}
	return BlobPart{kind: "blob", blobID: id}
}

type BlobOptions struct {
	Type    string
	Endings LineEndings
}

type FileOptions struct {
	Type         string
	Endings      LineEndings
	LastModified int64
}

type Blob struct {
	client proto.FileAPIClient
	info   *proto.FileObject
}

type FileAPIClient struct {
	client proto.FileAPIClient
	conn   *grpc.ClientConn
}

func FileAPISocketEnv(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return EnvFileAPISocket
	}
	var b strings.Builder
	b.WriteString(EnvFileAPISocket)
	b.WriteByte('_')
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - ('a' - 'A'))
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func FileAPI(name ...string) (*FileAPIClient, error) {
	envName := EnvFileAPISocket
	if len(name) > 0 {
		envName = FileAPISocketEnv(name[0])
	}
	socketPath := os.Getenv(envName)
	if socketPath == "" {
		return nil, fmt.Errorf("fileapi: %s is not set", envName)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, "unix:"+socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, fmt.Errorf("fileapi: connect to host: %w", err)
	}
	return &FileAPIClient{
		client: proto.NewFileAPIClient(conn),
		conn:   conn,
	}, nil
}

func (f *FileAPIClient) Close() error {
	return f.conn.Close()
}

func (f *FileAPIClient) CreateBlob(ctx context.Context, parts []BlobPart, options BlobOptions) (*Blob, error) {
	resp, err := f.client.CreateBlob(ctx, &proto.CreateBlobRequest{
		Parts:   blobPartsToProto(parts),
		Options: &proto.BlobOptions{MimeType: options.Type, Endings: lineEndingsToProto(options.Endings)},
	})
	if err != nil {
		return nil, fileapiErr(err)
	}
	return blobFromProto(f.client, resp.GetObject()), nil
}

func (f *FileAPIClient) CreateFile(ctx context.Context, parts []BlobPart, name string, options FileOptions) (*Blob, error) {
	resp, err := f.client.CreateFile(ctx, &proto.CreateFileRequest{
		FileBits: blobPartsToProto(parts),
		FileName: name,
		Options: &proto.FileOptions{
			MimeType:     options.Type,
			Endings:      lineEndingsToProto(options.Endings),
			LastModified: options.LastModified,
		},
	})
	if err != nil {
		return nil, fileapiErr(err)
	}
	return blobFromProto(f.client, resp.GetObject()), nil
}

func (f *FileAPIClient) Stat(ctx context.Context, id string) (*Blob, error) {
	resp, err := f.client.Stat(ctx, &proto.FileObjectRequest{Id: id})
	if err != nil {
		return nil, fileapiErr(err)
	}
	return blobFromProto(f.client, resp.GetObject()), nil
}

func (f *FileAPIClient) ResolveObjectURL(ctx context.Context, url string) (*Blob, error) {
	resp, err := f.client.ResolveObjectURL(ctx, &proto.ObjectURLRequest{Url: url})
	if err != nil {
		return nil, fileapiErr(err)
	}
	return blobFromProto(f.client, resp.GetObject()), nil
}

func (b *Blob) ID() string {
	if b == nil || b.info == nil {
		return ""
	}
	return b.info.GetId()
}

func (b *Blob) IsFile() bool {
	return b != nil && b.info != nil && b.info.GetKind() == proto.FileObjectKind_FILE_OBJECT_KIND_FILE
}

func (b *Blob) Size() int64 {
	if b == nil || b.info == nil {
		return 0
	}
	return b.info.GetSize()
}

func (b *Blob) Type() string {
	if b == nil || b.info == nil {
		return ""
	}
	return b.info.GetType()
}

func (b *Blob) Name() string {
	if b == nil || b.info == nil {
		return ""
	}
	return b.info.GetName()
}

func (b *Blob) LastModified() int64 {
	if b == nil || b.info == nil {
		return 0
	}
	return b.info.GetLastModified()
}

func (b *Blob) Slice(ctx context.Context, start, end *int64, contentType string) (*Blob, error) {
	req := &proto.SliceRequest{
		Id:          b.ID(),
		ContentType: contentType,
	}
	if start != nil {
		req.Start = start
	}
	if end != nil {
		req.End = end
	}
	resp, err := b.client.Slice(ctx, req)
	if err != nil {
		return nil, fileapiErr(err)
	}
	return blobFromProto(b.client, resp.GetObject()), nil
}

func (b *Blob) Bytes(ctx context.Context) ([]byte, error) {
	resp, err := b.client.ReadBytes(ctx, &proto.FileObjectRequest{Id: b.ID()})
	if err != nil {
		return nil, fileapiErr(err)
	}
	return append([]byte(nil), resp.GetData()...), nil
}

func (b *Blob) Text(ctx context.Context) (string, error) {
	data, err := b.Bytes(ctx)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (b *Blob) DataURL(ctx context.Context) (string, error) {
	data, err := b.Bytes(ctx)
	if err != nil {
		return "", err
	}
	if b.Type() == "" {
		return "data:;base64," + base64.StdEncoding.EncodeToString(data), nil
	}
	return fmt.Sprintf("data:%s;base64,%s", strings.ToLower(b.Type()), base64.StdEncoding.EncodeToString(data)), nil
}

func (b *Blob) OpenReadStream(ctx context.Context) (grpc.ServerStreamingClient[proto.ReadChunk], error) {
	stream, err := b.client.OpenReadStream(ctx, &proto.ReadStreamRequest{Id: b.ID()})
	if err != nil {
		return nil, fileapiErr(err)
	}
	return stream, nil
}

func (b *Blob) CreateObjectURL(ctx context.Context) (string, error) {
	resp, err := b.client.CreateObjectURL(ctx, &proto.CreateObjectURLRequest{Id: b.ID()})
	if err != nil {
		return "", fileapiErr(err)
	}
	return resp.GetUrl(), nil
}

func (b *Blob) RevokeObjectURL(ctx context.Context, url string) error {
	_, err := b.client.RevokeObjectURL(ctx, &proto.ObjectURLRequest{Url: url})
	return fileapiErr(err)
}

func blobFromProto(client proto.FileAPIClient, info *proto.FileObject) *Blob {
	return &Blob{client: client, info: info}
}

func blobPartsToProto(parts []BlobPart) []*proto.BlobPart {
	if len(parts) == 0 {
		return nil
	}
	out := make([]*proto.BlobPart, 0, len(parts))
	for _, part := range parts {
		switch part.kind {
		case "string":
			out = append(out, &proto.BlobPart{Kind: &proto.BlobPart_StringData{StringData: part.stringData}})
		case "bytes":
			out = append(out, &proto.BlobPart{Kind: &proto.BlobPart_BytesData{BytesData: append([]byte(nil), part.bytesData...)}})
		case "blob":
			out = append(out, &proto.BlobPart{Kind: &proto.BlobPart_BlobId{BlobId: part.blobID}})
		}
	}
	return out
}

func lineEndingsToProto(endings LineEndings) proto.LineEndings {
	switch endings {
	case LineEndingsNative:
		return proto.LineEndings_LINE_ENDINGS_NATIVE
	default:
		return proto.LineEndings_LINE_ENDINGS_TRANSPARENT
	}
}

func fileapiErr(err error) error {
	if err == nil {
		return nil
	}
	st, ok := status.FromError(err)
	if !ok {
		return err
	}
	switch st.Code() {
	case codes.NotFound:
		return ErrFileAPINotFound
	case codes.PermissionDenied:
		return ErrFileAPISecurity
	default:
		return err
	}
}
