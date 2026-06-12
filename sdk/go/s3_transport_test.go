package gestalt_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"github.com/valon-technologies/gestalt/sdk/go/client"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"
)

func s3WriteBytes(t *testing.T, s *client.S3, key string, body []byte, open *client.WriteObjectOpen) (*client.S3ObjectMeta, error) {
	t.Helper()
	if open == nil {
		open = &client.WriteObjectOpen{}
	}
	open.Ref = &client.S3ObjectRef{Key: key}
	stream, err := s.WriteObject(context.Background(), open)
	if err != nil {
		return nil, err
	}
	if len(body) > 0 {
		if err := stream.Send(body); err != nil && !errors.Is(err, io.EOF) {
			_, recvErr := stream.CloseAndRecv()
			if recvErr != nil {
				return nil, recvErr
			}
			return nil, err
		}
	}
	resp, err := stream.CloseAndRecv()
	if err != nil {
		return nil, err
	}
	return resp.Meta, nil
}

func s3ReadAll(t *testing.T, s *client.S3, request *client.ReadObjectRequest) (*client.S3ObjectMeta, []byte, error) {
	t.Helper()
	meta, data, err := s.ReadObject(context.Background(), request)
	if err != nil {
		return nil, nil, err
	}
	body, err := data.ReadAll()
	return meta, body, err
}

func gestaltErrorCode(err error) (client.GestaltErrorCode, bool) {
	var gerr *client.GestaltError
	if errors.As(err, &gerr) {
		return gerr.Code, true
	}
	return 0, false
}

func TestS3Transport_NamedSocketEnv(t *testing.T) {
	t.Setenv(gestalt.EnvHostServiceSocket, "unix://"+testS3Socket)
	s, err := client.ConnectS3(context.Background(), "test")
	if err != nil {
		t.Fatalf("connect named s3: %v", err)
	}

	if _, err := s3WriteBytes(t, s, "checks/ok.txt", []byte("ok"), nil); err != nil {
		t.Fatalf("WriteObject: %v", err)
	}
	_, body, err := s3ReadAll(t, s, &client.ReadObjectRequest{Ref: &client.S3ObjectRef{Key: "checks/ok.txt"}})
	if err != nil {
		t.Fatalf("ReadObject: %v", err)
	}
	if string(body) != "ok" {
		t.Fatalf("body = %q, want ok", body)
	}
}

func TestS3TransportNamedBindingMetadata(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	harness := &s3BindingMetadataHarness{bindings: make(chan string, 2)}
	srv := grpc.NewServer()
	proto.RegisterS3Server(srv, harness)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	t.Setenv(gestalt.EnvHostServiceSocket, "tcp://"+lis.Addr().String())
	s, err := client.ConnectS3(context.Background(), "archive")
	if err != nil {
		t.Fatalf("connect s3: %v", err)
	}

	ctx := context.Background()
	if err := s.DeleteObject(ctx, &client.S3ObjectRef{Key: "old.txt"}); err != nil {
		t.Fatalf("DeleteObject: %v", err)
	}
	if _, err := s3WriteBytes(t, s, "new.txt", []byte("body"), nil); err != nil {
		t.Fatalf("WriteObject: %v", err)
	}
	if got := <-harness.bindings; got != "archive" {
		t.Fatalf("unary binding metadata = %q, want archive", got)
	}
	if got := <-harness.bindings; got != "archive" {
		t.Fatalf("stream binding metadata = %q, want archive", got)
	}
}

type s3BindingMetadataHarness struct {
	proto.UnimplementedS3Server
	bindings chan string
}

func (h *s3BindingMetadataHarness) DeleteObject(ctx context.Context, _ *proto.DeleteObjectRequest) (*emptypb.Empty, error) {
	h.bindings <- firstMetadataValue(ctx, gestalt.HostServiceBindingMetadata)
	return &emptypb.Empty{}, nil
}

func (h *s3BindingMetadataHarness) WriteObject(stream proto.S3_WriteObjectServer) error {
	h.bindings <- firstMetadataValue(stream.Context(), gestalt.HostServiceBindingMetadata)
	for {
		if _, err := stream.Recv(); errors.Is(err, io.EOF) {
			return stream.SendAndClose(&proto.WriteObjectResponse{
				Meta: &proto.S3ObjectMeta{
					Ref:  &proto.S3ObjectRef{Key: "new.txt"},
					Size: 4,
				},
			})
		} else if err != nil {
			return err
		}
	}
}

func TestS3Transport_TCPTargetEnv(t *testing.T) {
	bin, target, cmd := buildAndStartTCPHarness("s3transportd", "")
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = os.Remove(bin)
	})

	t.Setenv(gestalt.EnvHostServiceSocket, target)
	s, err := client.ConnectS3(context.Background(), "tcp")
	if err != nil {
		t.Fatalf("connect tcp s3: %v", err)
	}

	if _, err := s3WriteBytes(t, s, "checks/ok.txt", []byte("ok"), nil); err != nil {
		t.Fatalf("WriteObject: %v", err)
	}
	_, body, err := s3ReadAll(t, s, &client.ReadObjectRequest{Ref: &client.S3ObjectRef{Key: "checks/ok.txt"}})
	if err != nil {
		t.Fatalf("ReadObject: %v", err)
	}
	if string(body) != "ok" {
		t.Fatalf("body = %q, want ok", body)
	}
}

func TestS3Transport_TCPTargetTokenEnv(t *testing.T) {
	const token = "relay-token-go"
	bin, target, cmd := buildAndStartTCPHarness("s3transportd", token)
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = os.Remove(bin)
	})

	t.Setenv(gestalt.EnvHostServiceSocket, target)
	t.Setenv(gestalt.EnvHostServiceToken, token)
	s, err := client.ConnectS3(context.Background(), "tcp-token")
	if err != nil {
		t.Fatalf("connect tcp s3 with token: %v", err)
	}

	if _, err := s3WriteBytes(t, s, "checks/token.txt", []byte("relay"), nil); err != nil {
		t.Fatalf("WriteObject: %v", err)
	}
	_, body, err := s3ReadAll(t, s, &client.ReadObjectRequest{Ref: &client.S3ObjectRef{Key: "checks/token.txt"}})
	if err != nil {
		t.Fatalf("ReadObject: %v", err)
	}
	if string(body) != "relay" {
		t.Fatalf("body = %q, want relay", body)
	}
}

func TestS3Transport_CreateObjectAccessURL(t *testing.T) {
	conn, err := grpc.NewClient("unix://"+testS3Socket, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial s3 socket: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	objectAccess := client.NewS3ObjectAccess(conn)

	ctx := context.Background()
	url, err := objectAccess.CreateObjectAccessURLRaw(ctx, &client.CreateObjectAccessURLRequest{
		Ref:            &client.S3ObjectRef{Key: "access/" + t.Name() + ".txt"},
		Method:         client.PresignMethodPut,
		ExpiresSeconds: 60,
		ContentType:    "text/plain",
		Headers:        map[string]string{"Content-Length": "5"},
	})
	if err != nil {
		t.Fatalf("CreateObjectAccessURL: %v", err)
	}
	if url.Method != client.PresignMethodPut {
		t.Fatalf("method = %v, want PUT", url.Method)
	}
	if !strings.HasPrefix(url.URL, "https://gestalt.example.test/api/v1/s3/object-access/") {
		t.Fatalf("url = %q, want hosted object access URL", url.URL)
	}
	if strings.Contains(url.URL, "access/"+t.Name()) {
		t.Fatalf("url leaks object key: %q", url.URL)
	}
	if url.Headers["Content-Length"] != "5" {
		t.Fatalf("Content-Length header = %q, want 5", url.Headers["Content-Length"])
	}
	if url.ExpiresAt == nil || url.ExpiresAt.IsZero() {
		t.Fatal("ExpiresAt is zero")
	}
}

func TestS3Transport_WriteReadAndStat(t *testing.T) {
	ctx := context.Background()
	key := "docs/" + t.Name() + ".json"

	payload, err := json.Marshal(map[string]any{
		"ok":   true,
		"name": t.Name(),
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	wrote, err := s3WriteBytes(t, testS3Client, key, payload, &client.WriteObjectOpen{
		ContentType: "application/json",
		Metadata:    map[string]string{"env": "test"},
	})
	if err != nil {
		t.Fatalf("WriteObject: %v", err)
	}
	if wrote.Ref.Key != key {
		t.Fatalf("WriteObject key = %q, want %q", wrote.Ref.Key, key)
	}
	if wrote.ContentType != "application/json" {
		t.Fatalf("WriteObject content type = %q, want application/json", wrote.ContentType)
	}

	head, err := testS3Client.HeadObject(ctx, &client.S3ObjectRef{Key: key})
	if err != nil {
		t.Fatalf("HeadObject: %v", err)
	}
	meta := head.Meta
	if meta.Metadata["env"] != "test" {
		t.Fatalf("HeadObject metadata env = %q, want test", meta.Metadata["env"])
	}
	if meta.Size <= 0 {
		t.Fatalf("HeadObject size = %d, want > 0", meta.Size)
	}
	if meta.LastModified == nil || meta.LastModified.IsZero() {
		t.Fatal("HeadObject last modified is zero")
	}

	_, body, err := s3ReadAll(t, testS3Client, &client.ReadObjectRequest{Ref: &client.S3ObjectRef{Key: key}})
	if err != nil {
		t.Fatalf("ReadObject: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if got["name"] != t.Name() {
		t.Fatalf("JSON name = %v, want %q", got["name"], t.Name())
	}
}

func TestS3Transport_StreamedReadAndEmptyObject(t *testing.T) {
	blobKey := "chunks/" + t.Name() + ".bin"
	blob := strings.Repeat("abcdef0123456789", 8192)
	if _, err := s3WriteBytes(t, testS3Client, blobKey, []byte(blob), &client.WriteObjectOpen{
		ContentType: "application/octet-stream",
	}); err != nil {
		t.Fatalf("WriteObject: %v", err)
	}

	meta, body, err := s3ReadAll(t, testS3Client, &client.ReadObjectRequest{
		Ref: &client.S3ObjectRef{Key: blobKey},
	})
	if err != nil {
		t.Fatalf("ReadObject: %v", err)
	}
	if meta.Size != int64(len(blob)) {
		t.Fatalf("ReadObject size = %d, want %d", meta.Size, len(blob))
	}
	if string(body) != blob {
		t.Fatalf("ReadObject body mismatch: got %d bytes", len(body))
	}

	emptyKey := "empty/" + t.Name()
	wrote, err := s3WriteBytes(t, testS3Client, emptyKey, nil, &client.WriteObjectOpen{
		ContentType: "text/plain",
	})
	if err != nil {
		t.Fatalf("WriteObject(empty): %v", err)
	}
	if wrote.Size != 0 {
		t.Fatalf("empty size = %d, want 0", wrote.Size)
	}
	_, emptyBody, err := s3ReadAll(t, testS3Client, &client.ReadObjectRequest{
		Ref: &client.S3ObjectRef{Key: emptyKey},
	})
	if err != nil {
		t.Fatalf("ReadObject(empty): %v", err)
	}
	if len(emptyBody) != 0 {
		t.Fatalf("ReadObject(empty) = %q, want empty", emptyBody)
	}
}

func TestS3Transport_CancelStopsRead(t *testing.T) {
	key := "streams/" + t.Name() + ".txt"
	payload := strings.Repeat("abcdef0123456789", 4096)
	if _, err := s3WriteBytes(t, testS3Client, key, []byte(payload), nil); err != nil {
		t.Fatalf("WriteObject: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	_, data, err := testS3Client.ReadObject(ctx, &client.ReadObjectRequest{
		Ref: &client.S3ObjectRef{Key: key},
	})
	if err != nil {
		t.Fatalf("ReadObject: %v", err)
	}
	chunk, err := data.Recv()
	if err != nil {
		t.Fatalf("Recv(first): %v", err)
	}
	if len(chunk) == 0 {
		t.Fatal("Recv(first) returned 0 bytes")
	}
	cancel()
	// The stream terminates after cancellation: either with the cancellation
	// error, or with io.EOF when every frame was already buffered.
	for {
		_, err = data.Recv()
		if err != nil {
			break
		}
	}

	_, body, err := s3ReadAll(t, testS3Client, &client.ReadObjectRequest{
		Ref: &client.S3ObjectRef{Key: key},
	})
	if err != nil {
		t.Fatalf("ReadObject(after cancel): %v", err)
	}
	if string(body) != payload {
		t.Fatalf("ReadObject(after cancel) length = %d, want %d", len(body), len(payload))
	}
}

func TestS3Transport_RangeRead(t *testing.T) {
	key := "ranges/" + t.Name() + ".txt"
	if _, err := s3WriteBytes(t, testS3Client, key, []byte("0123456789"), nil); err != nil {
		t.Fatalf("WriteObject: %v", err)
	}

	start, end := int64(2), int64(5)
	_, body, err := s3ReadAll(t, testS3Client, &client.ReadObjectRequest{
		Ref:   &client.S3ObjectRef{Key: key},
		Range: &client.ByteRange{Start: &start, End: &end},
	})
	if err != nil {
		t.Fatalf("ReadObject(range): %v", err)
	}
	if string(body) != "2345" {
		t.Fatalf("ReadObject(range) = %q, want 2345", body)
	}
}

func TestS3Transport_ListPrefixDelimiterAndPagination(t *testing.T) {
	ctx := context.Background()
	for _, key := range []string{
		"list/" + t.Name() + "/a.txt",
		"list/" + t.Name() + "/nested/b.txt",
		"list/" + t.Name() + "/nested/c.txt",
		"list/" + t.Name() + "/z.txt",
	} {
		if _, err := s3WriteBytes(t, testS3Client, key, []byte(key), nil); err != nil {
			t.Fatalf("seed %s: %v", key, err)
		}
	}

	basePrefix := "list/" + t.Name() + "/"
	page, err := testS3Client.ListObjects(ctx, basePrefix, "/", "", "", 0)
	if err != nil {
		t.Fatalf("ListObjects(delimiter): %v", err)
	}
	if len(page.CommonPrefixes) != 1 || page.CommonPrefixes[0] != basePrefix+"nested/" {
		t.Fatalf("CommonPrefixes = %v, want [%s]", page.CommonPrefixes, basePrefix+"nested/")
	}
	if len(page.Objects) != 2 {
		t.Fatalf("Objects(delimiter) len = %d, want 2", len(page.Objects))
	}

	first, err := testS3Client.ListObjects(ctx, basePrefix, "", "", "", 2)
	if err != nil {
		t.Fatalf("ListObjects(first page): %v", err)
	}
	if !first.HasMore {
		t.Fatal("first page HasMore = false, want true")
	}
	if len(first.Objects) != 2 {
		t.Fatalf("first page len = %d, want 2", len(first.Objects))
	}
	second, err := testS3Client.ListObjects(ctx, basePrefix, "", first.NextContinuationToken, "", 2)
	if err != nil {
		t.Fatalf("ListObjects(second page): %v", err)
	}
	if second.HasMore {
		t.Fatal("second page HasMore = true, want false")
	}
	if len(second.Objects) != 2 {
		t.Fatalf("second page len = %d, want 2", len(second.Objects))
	}
	if second.Objects[0].Ref.Key <= first.Objects[len(first.Objects)-1].Ref.Key {
		t.Fatalf("pagination order regressed: first=%q second=%q", first.Objects[len(first.Objects)-1].Ref.Key, second.Objects[0].Ref.Key)
	}

	delimitedFirst, err := testS3Client.ListObjects(ctx, basePrefix, "/", "", "", 1)
	if err != nil {
		t.Fatalf("ListObjects(delimited first page): %v", err)
	}
	if !delimitedFirst.HasMore {
		t.Fatal("delimited first page HasMore = false, want true")
	}
	if len(delimitedFirst.Objects) != 1 || delimitedFirst.Objects[0].Ref.Key != basePrefix+"a.txt" {
		t.Fatalf("delimited first page objects = %v, want [%s]", delimitedFirst.Objects, basePrefix+"a.txt")
	}
	if len(delimitedFirst.CommonPrefixes) != 0 {
		t.Fatalf("delimited first page prefixes = %v, want none", delimitedFirst.CommonPrefixes)
	}
	delimitedSecond, err := testS3Client.ListObjects(ctx, basePrefix, "/", delimitedFirst.NextContinuationToken, "", 1)
	if err != nil {
		t.Fatalf("ListObjects(delimited second page): %v", err)
	}
	if len(delimitedSecond.CommonPrefixes) != 1 || delimitedSecond.CommonPrefixes[0] != basePrefix+"nested/" {
		t.Fatalf("delimited second page prefixes = %v, want [%s]", delimitedSecond.CommonPrefixes, basePrefix+"nested/")
	}
	if !delimitedSecond.HasMore {
		t.Fatal("delimited second page HasMore = false, want true")
	}
	delimitedThird, err := testS3Client.ListObjects(ctx, basePrefix, "/", delimitedSecond.NextContinuationToken, "", 1)
	if err != nil {
		t.Fatalf("ListObjects(delimited third page): %v", err)
	}
	for _, prefix := range delimitedThird.CommonPrefixes {
		if prefix == basePrefix+"nested/" {
			t.Fatalf("common prefix %q repeated across pages", prefix)
		}
	}
	if len(delimitedThird.Objects) != 1 || delimitedThird.Objects[0].Ref.Key != basePrefix+"z.txt" {
		t.Fatalf("delimited third page objects = %v, want [%s]", delimitedThird.Objects, basePrefix+"z.txt")
	}
}

func TestS3Transport_CopyDeletePresignAndExists(t *testing.T) {
	ctx := context.Background()
	sourceKey := "copy/" + t.Name() + "/source.txt"
	sourceMeta, err := s3WriteBytes(t, testS3Client, sourceKey, []byte("copied"), &client.WriteObjectOpen{
		ContentType: "text/plain",
		Metadata:    map[string]string{"copied": "true"},
	})
	if err != nil {
		t.Fatalf("WriteObject(source): %v", err)
	}

	destKey := "copy/" + t.Name() + "/dest.txt"
	copied, err := testS3Client.CopyObject(ctx, "", "",
		&client.S3ObjectRef{Key: sourceKey},
		&client.S3ObjectRef{Key: destKey},
	)
	if err != nil {
		t.Fatalf("CopyObject: %v", err)
	}
	if copied.Meta.Ref.Key != destKey {
		t.Fatalf("CopyObject key = %q, want %q", copied.Meta.Ref.Key, destKey)
	}

	if _, err := testS3Client.HeadObject(ctx, &client.S3ObjectRef{Key: destKey}); err != nil {
		t.Fatalf("HeadObject(dest): %v", err)
	}

	_, body, err := s3ReadAll(t, testS3Client, &client.ReadObjectRequest{Ref: &client.S3ObjectRef{Key: destKey}})
	if err != nil {
		t.Fatalf("ReadObject(dest): %v", err)
	}
	if string(body) != "copied" {
		t.Fatalf("ReadObject(dest) = %q, want copied", body)
	}

	etagCopyKey := "copy/" + t.Name() + "/etag-copy.txt"
	etagCopied, err := testS3Client.CopyObject(ctx, sourceMeta.Etag, "",
		&client.S3ObjectRef{Key: sourceKey},
		&client.S3ObjectRef{Key: etagCopyKey},
	)
	if err != nil {
		t.Fatalf("CopyObject(source etag): %v", err)
	}
	if etagCopied.Meta.Ref.Key != etagCopyKey {
		t.Fatalf("CopyObject(source etag) key = %q, want %q", etagCopied.Meta.Ref.Key, etagCopyKey)
	}

	presigned, err := testS3Client.PresignObjectRaw(ctx, &client.PresignObjectRequest{
		Ref:            &client.S3ObjectRef{Key: destKey},
		Method:         client.PresignMethodPut,
		ExpiresSeconds: int64((15 * time.Minute).Seconds()),
		ContentType:    "text/plain",
		Headers:        map[string]string{"x-test": "true"},
	})
	if err != nil {
		t.Fatalf("PresignObject: %v", err)
	}
	if presigned.Method != client.PresignMethodPut {
		t.Fatalf("Presign method = %v, want PUT", presigned.Method)
	}
	if !strings.Contains(presigned.URL, "method=PUT") {
		t.Fatalf("Presign URL = %q, want method=PUT", presigned.URL)
	}
	if presigned.Headers["x-test"] != "true" {
		t.Fatalf("Presign headers = %v", presigned.Headers)
	}

	if err := testS3Client.DeleteObject(ctx, &client.S3ObjectRef{Key: destKey}); err != nil {
		t.Fatalf("DeleteObject: %v", err)
	}
	_, err = testS3Client.HeadObject(ctx, &client.S3ObjectRef{Key: destKey})
	if code, ok := gestaltErrorCode(err); !ok || code != client.GestaltErrorCodeNotFound {
		t.Fatalf("HeadObject(after delete) error = %v, want not-found GestaltError", err)
	}
}

func TestS3Transport_ErrorMapping(t *testing.T) {
	ctx := context.Background()

	_, err := testS3Client.HeadObject(ctx, &client.S3ObjectRef{Key: "missing/" + t.Name()})
	if code, ok := gestaltErrorCode(err); !ok || code != client.GestaltErrorCodeNotFound {
		t.Fatalf("HeadObject missing error = %v, want not-found GestaltError", err)
	}

	existingKey := "errors/" + t.Name() + ".txt"
	meta, err := s3WriteBytes(t, testS3Client, existingKey, []byte("abc"), nil)
	if err != nil {
		t.Fatalf("WriteObject(existing): %v", err)
	}

	_, err = s3WriteBytes(t, testS3Client, existingKey, []byte("overwrite"), &client.WriteObjectOpen{
		IfNoneMatch: "*",
	})
	if code, ok := gestaltErrorCode(err); !ok || code != client.GestaltErrorCodeFailedPrecondition {
		t.Fatalf("IfNoneMatch error = %v, want failed-precondition GestaltError", err)
	}

	start, end := int64(9), int64(1)
	_, _, err = s3ReadAll(t, testS3Client, &client.ReadObjectRequest{
		Ref:   &client.S3ObjectRef{Key: existingKey},
		Range: &client.ByteRange{Start: &start, End: &end},
	})
	if code, ok := gestaltErrorCode(err); !ok || code != client.GestaltErrorCodeOutOfRange {
		t.Fatalf("range error = %v, want out-of-range GestaltError", err)
	}

	_, err = testS3Client.CopyObject(ctx, "wrong-etag", "",
		&client.S3ObjectRef{Key: existingKey},
		&client.S3ObjectRef{Key: "errors/" + t.Name() + "-copy.txt"},
	)
	if code, ok := gestaltErrorCode(err); !ok || code != client.GestaltErrorCodeFailedPrecondition {
		t.Fatalf("CopyObject IfMatch error = %v, want failed-precondition GestaltError", err)
	}

	_, err = testS3Client.CopyObject(ctx, "", "",
		&client.S3ObjectRef{Key: "errors/absent-" + t.Name()},
		&client.S3ObjectRef{Key: "errors/" + t.Name() + "-copy-2.txt"},
	)
	if code, ok := gestaltErrorCode(err); !ok || code != client.GestaltErrorCodeNotFound {
		t.Fatalf("CopyObject missing error = %v, want not-found GestaltError", err)
	}

	if meta.Etag == "" {
		t.Fatal("WriteObject(existing) ETag is empty")
	}
}
