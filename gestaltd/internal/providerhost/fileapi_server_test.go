package providerhost

import (
	"context"
	"testing"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestFileAPIServerPrefixesObjectIDsPerPlugin(t *testing.T) {
	t.Parallel()

	api := &coretesting.StubFileAPI{}
	srv := NewFileAPIServer(api, "roadmap").(*fileAPIServer)
	ctx := context.Background()

	resp, err := srv.CreateBlob(ctx, &proto.CreateBlobRequest{
		Parts: []*proto.BlobPart{
			{Kind: &proto.BlobPart_StringData{StringData: "hello"}},
		},
	})
	if err != nil {
		t.Fatalf("CreateBlob: %v", err)
	}
	if got := resp.GetObject().GetId(); got != "plugin_roadmap_obj-1" {
		t.Fatalf("prefixed object id = %q, want %q", got, "plugin_roadmap_obj-1")
	}
	if _, err := api.Stat(ctx, "obj-1"); err != nil {
		t.Fatalf("expected raw object id to exist: %v", err)
	}
	if _, err := api.Stat(ctx, "plugin_roadmap_obj-1"); err == nil {
		t.Fatal("prefixed object id should not exist in underlying provider")
	}
}

func TestFileAPIServerWrapsObjectURLsPerPlugin(t *testing.T) {
	t.Parallel()

	api := &coretesting.StubFileAPI{}
	srv := NewFileAPIServer(api, "roadmap").(*fileAPIServer)
	ctx := context.Background()

	blobResp, err := srv.CreateBlob(ctx, &proto.CreateBlobRequest{
		Parts: []*proto.BlobPart{
			{Kind: &proto.BlobPart_StringData{StringData: "hello"}},
		},
	})
	if err != nil {
		t.Fatalf("CreateBlob: %v", err)
	}

	urlResp, err := srv.CreateObjectURL(ctx, &proto.CreateObjectURLRequest{Id: blobResp.GetObject().GetId()})
	if err != nil {
		t.Fatalf("CreateObjectURL: %v", err)
	}
	if got := urlResp.GetUrl(); got == "blob:gestalt/1" || got == "" {
		t.Fatalf("wrapped url = %q, want namespaced wrapper", got)
	}

	resolved, err := srv.ResolveObjectURL(ctx, &proto.ObjectURLRequest{Url: urlResp.GetUrl()})
	if err != nil {
		t.Fatalf("ResolveObjectURL: %v", err)
	}
	if got := resolved.GetObject().GetId(); got != blobResp.GetObject().GetId() {
		t.Fatalf("resolved object id = %q, want %q", got, blobResp.GetObject().GetId())
	}
}

func TestFileAPIServerStripsBlobRefPrefixesPerPlugin(t *testing.T) {
	t.Parallel()

	api := &coretesting.StubFileAPI{}
	srv := NewFileAPIServer(api, "roadmap").(*fileAPIServer)
	ctx := context.Background()

	first, err := srv.CreateBlob(ctx, &proto.CreateBlobRequest{
		Parts: []*proto.BlobPart{
			{Kind: &proto.BlobPart_StringData{StringData: "hello"}},
		},
	})
	if err != nil {
		t.Fatalf("CreateBlob first: %v", err)
	}

	second, err := srv.CreateBlob(ctx, &proto.CreateBlobRequest{
		Parts: []*proto.BlobPart{
			{Kind: &proto.BlobPart_BlobId{BlobId: first.GetObject().GetId()}},
			{Kind: &proto.BlobPart_StringData{StringData: "-world"}},
		},
	})
	if err != nil {
		t.Fatalf("CreateBlob with blob ref: %v", err)
	}

	rawID, err := srv.stripID(second.GetObject().GetId())
	if err != nil {
		t.Fatalf("stripID: %v", err)
	}
	data, err := api.ReadBytes(ctx, rawID)
	if err != nil {
		t.Fatalf("ReadBytes: %v", err)
	}
	if got := string(data); got != "hello-world" {
		t.Fatalf("blob ref round-trip = %q, want %q", got, "hello-world")
	}
}

func TestFileAPIServerRejectsCrossNamespaceBlobRefs(t *testing.T) {
	t.Parallel()

	api := &coretesting.StubFileAPI{}
	srv := NewFileAPIServer(api, "roadmap").(*fileAPIServer)
	ctx := context.Background()

	if _, err := srv.CreateBlob(ctx, &proto.CreateBlobRequest{
		Parts: []*proto.BlobPart{
			{Kind: &proto.BlobPart_BlobId{BlobId: "obj-1"}},
		},
	}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("CreateBlob raw blob id error = %v, want PermissionDenied", err)
	}
}
