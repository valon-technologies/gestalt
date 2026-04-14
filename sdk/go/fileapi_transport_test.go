package gestalt_test

import (
	"context"
	"io"
	"testing"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
)

var testFileAPI *gestalt.FileAPIClient

func TestFileAPITransport_NamedSocketEnv(t *testing.T) {
	client, err := gestalt.FileAPI("test")
	if err != nil {
		t.Fatalf("connect named fileapi: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	blob, err := client.CreateBlob(ctx, []gestalt.BlobPart{gestalt.StringBlobPart("named")}, gestalt.BlobOptions{})
	if err != nil {
		t.Fatalf("CreateBlob: %v", err)
	}
	got, err := blob.Text(ctx)
	if err != nil {
		t.Fatalf("Text: %v", err)
	}
	if got != "named" {
		t.Fatalf("text = %q, want named", got)
	}
}

func TestFileAPITransport_BlobRoundTrip(t *testing.T) {
	ctx := context.Background()
	blob, err := testFileAPI.CreateBlob(ctx, []gestalt.BlobPart{
		gestalt.StringBlobPart("hello"),
		gestalt.BytesBlobPart([]byte(" world")),
	}, gestalt.BlobOptions{Type: "text/plain"})
	if err != nil {
		t.Fatalf("CreateBlob: %v", err)
	}
	if blob.Type() != "text/plain" {
		t.Fatalf("Type = %q, want text/plain", blob.Type())
	}
	data, err := blob.Bytes(ctx)
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	if string(data) != "hello world" {
		t.Fatalf("Bytes = %q, want hello world", string(data))
	}
	text, err := blob.Text(ctx)
	if err != nil {
		t.Fatalf("Text: %v", err)
	}
	if text != "hello world" {
		t.Fatalf("Text = %q, want hello world", text)
	}
}

func TestFileAPITransport_SliceAndFileMetadata(t *testing.T) {
	ctx := context.Background()
	file, err := testFileAPI.CreateFile(ctx, []gestalt.BlobPart{
		gestalt.StringBlobPart("abcdef"),
	}, "notes.txt", gestalt.FileOptions{Type: "text/plain", LastModified: 1234})
	if err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	if !file.IsFile() {
		t.Fatal("expected file object")
	}
	if file.Name() != "notes.txt" {
		t.Fatalf("Name = %q, want notes.txt", file.Name())
	}
	if file.LastModified() != 1234 {
		t.Fatalf("LastModified = %d, want 1234", file.LastModified())
	}

	start, end := int64(1), int64(4)
	slice, err := file.Slice(ctx, &start, &end, "text/plain")
	if err != nil {
		t.Fatalf("Slice: %v", err)
	}
	text, err := slice.Text(ctx)
	if err != nil {
		t.Fatalf("Text: %v", err)
	}
	if text != "bcd" {
		t.Fatalf("slice text = %q, want bcd", text)
	}
}

func TestFileAPITransport_ObjectURLLifecycle(t *testing.T) {
	ctx := context.Background()
	blob, err := testFileAPI.CreateBlob(ctx, []gestalt.BlobPart{gestalt.StringBlobPart("url-data")}, gestalt.BlobOptions{})
	if err != nil {
		t.Fatalf("CreateBlob: %v", err)
	}
	url, err := blob.CreateObjectURL(ctx)
	if err != nil {
		t.Fatalf("CreateObjectURL: %v", err)
	}
	resolved, err := testFileAPI.ResolveObjectURL(ctx, url)
	if err != nil {
		t.Fatalf("ResolveObjectURL: %v", err)
	}
	if resolved.ID() != blob.ID() {
		t.Fatalf("resolved id = %q, want %q", resolved.ID(), blob.ID())
	}
	if err := blob.RevokeObjectURL(ctx, url); err != nil {
		t.Fatalf("RevokeObjectURL: %v", err)
	}
	if _, err := testFileAPI.ResolveObjectURL(ctx, url); err != gestalt.ErrFileAPINotFound {
		t.Fatalf("ResolveObjectURL after revoke = %v, want ErrFileAPINotFound", err)
	}
}

func TestFileAPITransport_ReadStream(t *testing.T) {
	ctx := context.Background()
	blob, err := testFileAPI.CreateBlob(ctx, []gestalt.BlobPart{
		gestalt.StringBlobPart("stream me"),
	}, gestalt.BlobOptions{})
	if err != nil {
		t.Fatalf("CreateBlob: %v", err)
	}
	stream, err := blob.OpenReadStream(ctx)
	if err != nil {
		t.Fatalf("OpenReadStream: %v", err)
	}
	var data []byte
	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Recv: %v", err)
		}
		data = append(data, chunk.GetData()...)
	}
	if string(data) != "stream me" {
		t.Fatalf("stream data = %q, want stream me", string(data))
	}
}
