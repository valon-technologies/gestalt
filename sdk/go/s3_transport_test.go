package gestalt_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
)

func TestS3Transport_NamedSocketEnv(t *testing.T) {
	client, err := gestalt.S3("test")
	if err != nil {
		t.Fatalf("connect named s3: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	obj := client.Object("named", "checks/ok.txt")
	if _, err := obj.WriteString(ctx, "ok", nil); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	got, err := obj.Text(ctx, nil)
	if err != nil {
		t.Fatalf("Text: %v", err)
	}
	if got != "ok" {
		t.Fatalf("Text = %q, want ok", got)
	}
}

func TestS3Transport_WriteReadAndStat(t *testing.T) {
	ctx := context.Background()
	key := "docs/" + t.Name() + ".json"
	obj := testS3Client.Object("fixtures", key)

	wrote, err := obj.WriteJSON(ctx, map[string]any{
		"ok":   true,
		"name": t.Name(),
	}, &gestalt.WriteOptions{
		Metadata: map[string]string{"env": "test"},
	})
	if err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	if wrote.Ref.Key != key {
		t.Fatalf("WriteJSON key = %q, want %q", wrote.Ref.Key, key)
	}
	if wrote.ContentType != "application/json" {
		t.Fatalf("WriteJSON content type = %q, want application/json", wrote.ContentType)
	}

	meta, err := obj.Stat(ctx)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if meta.Metadata["env"] != "test" {
		t.Fatalf("Stat metadata env = %q, want test", meta.Metadata["env"])
	}
	if meta.Size <= 0 {
		t.Fatalf("Stat size = %d, want > 0", meta.Size)
	}
	if meta.LastModified.IsZero() {
		t.Fatal("Stat last modified is zero")
	}

	got, err := obj.JSON(ctx, nil)
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	payload, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("JSON type = %T, want map[string]any", got)
	}
	if payload["name"] != t.Name() {
		t.Fatalf("JSON name = %v, want %q", payload["name"], t.Name())
	}
}

func TestS3Transport_StreamedReadAndEmptyObject(t *testing.T) {
	ctx := context.Background()
	blobKey := "chunks/" + t.Name() + ".bin"
	blob := strings.Repeat("abcdef0123456789", 8192)
	obj := testS3Client.Object("fixtures", blobKey)
	if _, err := obj.WriteString(ctx, blob, &gestalt.WriteOptions{
		ContentType: "application/octet-stream",
	}); err != nil {
		t.Fatalf("WriteString: %v", err)
	}

	meta, body, err := testS3Client.ReadObject(ctx, gestalt.ObjectRef{
		Bucket: "fixtures",
		Key:    blobKey,
	}, nil)
	if err != nil {
		t.Fatalf("ReadObject: %v", err)
	}
	defer func() { _ = body.Close() }()
	if meta.Size != int64(len(blob)) {
		t.Fatalf("ReadObject size = %d, want %d", meta.Size, len(blob))
	}
	data, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(data) != blob {
		t.Fatalf("ReadObject body mismatch: got %d bytes", len(data))
	}

	empty := testS3Client.Object("fixtures", "empty/"+t.Name())
	meta, err = empty.WriteBytes(ctx, nil, &gestalt.WriteOptions{
		ContentType: "text/plain",
	})
	if err != nil {
		t.Fatalf("WriteBytes(empty): %v", err)
	}
	if meta.Size != 0 {
		t.Fatalf("empty size = %d, want 0", meta.Size)
	}
	text, err := empty.Text(ctx, nil)
	if err != nil {
		t.Fatalf("Text(empty): %v", err)
	}
	if text != "" {
		t.Fatalf("Text(empty) = %q, want empty", text)
	}
}

func TestS3Transport_EarlyCloseCancelsRead(t *testing.T) {
	ctx := context.Background()
	key := "streams/" + t.Name() + ".txt"
	payload := strings.Repeat("abcdef0123456789", 4096)
	obj := testS3Client.Object("fixtures", key)
	if _, err := obj.WriteString(ctx, payload, nil); err != nil {
		t.Fatalf("WriteString: %v", err)
	}

	_, body, err := testS3Client.ReadObject(ctx, gestalt.ObjectRef{
		Bucket: "fixtures",
		Key:    key,
	}, nil)
	if err != nil {
		t.Fatalf("ReadObject: %v", err)
	}

	buf := make([]byte, 1024)
	n, err := body.Read(buf)
	if err != nil {
		t.Fatalf("Read(first): %v", err)
	}
	if n == 0 {
		t.Fatal("Read(first) returned 0 bytes")
	}
	if err := body.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	n, err = body.Read(buf)
	if err != io.EOF {
		t.Fatalf("Read(after close) error = %v, want EOF", err)
	}
	if n != 0 {
		t.Fatalf("Read(after close) bytes = %d, want 0", n)
	}

	got, err := obj.Text(ctx, nil)
	if err != nil {
		t.Fatalf("Text(after close): %v", err)
	}
	if got != payload {
		t.Fatalf("Text(after close) length = %d, want %d", len(got), len(payload))
	}
}

func TestS3Transport_RangeRead(t *testing.T) {
	ctx := context.Background()
	obj := testS3Client.Object("fixtures", "ranges/"+t.Name()+".txt")
	if _, err := obj.WriteString(ctx, "0123456789", nil); err != nil {
		t.Fatalf("WriteString: %v", err)
	}

	start, end := int64(2), int64(5)
	got, err := obj.Text(ctx, &gestalt.ReadOptions{
		Range: &gestalt.ByteRange{Start: &start, End: &end},
	})
	if err != nil {
		t.Fatalf("Text(range): %v", err)
	}
	if got != "2345" {
		t.Fatalf("Text(range) = %q, want 2345", got)
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
		if _, err := testS3Client.Object("fixtures", key).WriteString(ctx, key, nil); err != nil {
			t.Fatalf("seed %s: %v", key, err)
		}
	}

	basePrefix := "list/" + t.Name() + "/"
	page, err := testS3Client.ListObjects(ctx, gestalt.ListOptions{
		Bucket:    "fixtures",
		Prefix:    basePrefix,
		Delimiter: "/",
	})
	if err != nil {
		t.Fatalf("ListObjects(delimiter): %v", err)
	}
	if len(page.CommonPrefixes) != 1 || page.CommonPrefixes[0] != basePrefix+"nested/" {
		t.Fatalf("CommonPrefixes = %v, want [%s]", page.CommonPrefixes, basePrefix+"nested/")
	}
	if len(page.Objects) != 2 {
		t.Fatalf("Objects(delimiter) len = %d, want 2", len(page.Objects))
	}

	first, err := testS3Client.ListObjects(ctx, gestalt.ListOptions{
		Bucket:  "fixtures",
		Prefix:  basePrefix,
		MaxKeys: 2,
	})
	if err != nil {
		t.Fatalf("ListObjects(first page): %v", err)
	}
	if !first.HasMore {
		t.Fatal("first page HasMore = false, want true")
	}
	if len(first.Objects) != 2 {
		t.Fatalf("first page len = %d, want 2", len(first.Objects))
	}
	second, err := testS3Client.ListObjects(ctx, gestalt.ListOptions{
		Bucket:            "fixtures",
		Prefix:            basePrefix,
		MaxKeys:           2,
		ContinuationToken: first.NextContinuationToken,
	})
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

	delimitedFirst, err := testS3Client.ListObjects(ctx, gestalt.ListOptions{
		Bucket:    "fixtures",
		Prefix:    basePrefix,
		Delimiter: "/",
		MaxKeys:   1,
	})
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
	delimitedSecond, err := testS3Client.ListObjects(ctx, gestalt.ListOptions{
		Bucket:            "fixtures",
		Prefix:            basePrefix,
		Delimiter:         "/",
		MaxKeys:           1,
		ContinuationToken: delimitedFirst.NextContinuationToken,
	})
	if err != nil {
		t.Fatalf("ListObjects(delimited second page): %v", err)
	}
	if len(delimitedSecond.CommonPrefixes) != 1 || delimitedSecond.CommonPrefixes[0] != basePrefix+"nested/" {
		t.Fatalf("delimited second page prefixes = %v, want [%s]", delimitedSecond.CommonPrefixes, basePrefix+"nested/")
	}
	if !delimitedSecond.HasMore {
		t.Fatal("delimited second page HasMore = false, want true")
	}
	delimitedThird, err := testS3Client.ListObjects(ctx, gestalt.ListOptions{
		Bucket:            "fixtures",
		Prefix:            basePrefix,
		Delimiter:         "/",
		MaxKeys:           1,
		ContinuationToken: delimitedSecond.NextContinuationToken,
	})
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
	source := testS3Client.Object("fixtures", "copy/"+t.Name()+"/source.txt")
	sourceMeta, err := source.WriteString(ctx, "copied", &gestalt.WriteOptions{
		ContentType: "text/plain",
		Metadata:    map[string]string{"copied": "true"},
	})
	if err != nil {
		t.Fatalf("WriteString(source): %v", err)
	}

	destRef := gestalt.ObjectRef{Bucket: "fixtures", Key: "copy/" + t.Name() + "/dest.txt"}
	meta, err := testS3Client.CopyObject(ctx, gestalt.ObjectRef{
		Bucket: "fixtures",
		Key:    "copy/" + t.Name() + "/source.txt",
	}, destRef, nil)
	if err != nil {
		t.Fatalf("CopyObject: %v", err)
	}
	if meta.Ref.Key != destRef.Key {
		t.Fatalf("CopyObject key = %q, want %q", meta.Ref.Key, destRef.Key)
	}

	dest := testS3Client.Object(destRef.Bucket, destRef.Key)
	exists, err := dest.Exists(ctx)
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !exists {
		t.Fatal("Exists = false, want true")
	}

	got, err := dest.Text(ctx, nil)
	if err != nil {
		t.Fatalf("Text(dest): %v", err)
	}
	if got != "copied" {
		t.Fatalf("Text(dest) = %q, want copied", got)
	}

	etagCopyRef := gestalt.ObjectRef{Bucket: "fixtures", Key: "copy/" + t.Name() + "/etag-copy.txt"}
	etagMeta, err := testS3Client.CopyObject(ctx, gestalt.ObjectRef{
		Bucket: "fixtures",
		Key:    "copy/" + t.Name() + "/source.txt",
	}, etagCopyRef, &gestalt.CopyOptions{
		IfMatch: sourceMeta.ETag,
	})
	if err != nil {
		t.Fatalf("CopyObject(source etag): %v", err)
	}
	if etagMeta.Ref.Key != etagCopyRef.Key {
		t.Fatalf("CopyObject(source etag) key = %q, want %q", etagMeta.Ref.Key, etagCopyRef.Key)
	}

	presigned, err := dest.Presign(ctx, &gestalt.PresignOptions{
		Method:      gestalt.PresignMethodPut,
		Expires:     15 * time.Minute,
		ContentType: "text/plain",
		Headers:     map[string]string{"x-test": "true"},
	})
	if err != nil {
		t.Fatalf("Presign: %v", err)
	}
	if presigned.Method != gestalt.PresignMethodPut {
		t.Fatalf("Presign method = %q, want PUT", presigned.Method)
	}
	if !strings.Contains(presigned.URL, "method=PUT") {
		t.Fatalf("Presign URL = %q, want method=PUT", presigned.URL)
	}
	if presigned.Headers["x-test"] != "true" {
		t.Fatalf("Presign headers = %v", presigned.Headers)
	}

	if err := dest.Delete(ctx); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	exists, err = dest.Exists(ctx)
	if err != nil {
		t.Fatalf("Exists(after delete): %v", err)
	}
	if exists {
		t.Fatal("Exists(after delete) = true, want false")
	}
}

func TestS3Transport_ErrorMapping(t *testing.T) {
	ctx := context.Background()
	missing := testS3Client.Object("fixtures", "missing/"+t.Name())

	_, err := missing.Stat(ctx)
	if !errors.Is(err, gestalt.ErrS3NotFound) {
		t.Fatalf("Stat missing error = %v, want ErrS3NotFound", err)
	}

	existing := testS3Client.Object("fixtures", "errors/"+t.Name()+".txt")
	meta, err := existing.WriteString(ctx, "abc", nil)
	if err != nil {
		t.Fatalf("WriteString(existing): %v", err)
	}

	_, err = existing.WriteString(ctx, "overwrite", &gestalt.WriteOptions{
		IfNoneMatch: "*",
	})
	if !errors.Is(err, gestalt.ErrS3PreconditionFailed) {
		t.Fatalf("IfNoneMatch error = %v, want ErrS3PreconditionFailed", err)
	}

	start, end := int64(9), int64(1)
	_, err = existing.Text(ctx, &gestalt.ReadOptions{
		Range: &gestalt.ByteRange{Start: &start, End: &end},
	})
	if !errors.Is(err, gestalt.ErrS3InvalidRange) {
		t.Fatalf("range error = %v, want ErrS3InvalidRange", err)
	}

	_, err = testS3Client.CopyObject(ctx, gestalt.ObjectRef{
		Bucket: "fixtures",
		Key:    "errors/" + t.Name() + ".txt",
	}, gestalt.ObjectRef{
		Bucket: "fixtures",
		Key:    "errors/" + t.Name() + "-copy.txt",
	}, &gestalt.CopyOptions{
		IfMatch: "wrong-etag",
	})
	if !errors.Is(err, gestalt.ErrS3PreconditionFailed) {
		t.Fatalf("CopyObject IfMatch error = %v, want ErrS3PreconditionFailed", err)
	}

	_, err = testS3Client.CopyObject(ctx, gestalt.ObjectRef{
		Bucket: "fixtures",
		Key:    "errors/absent-" + t.Name(),
	}, gestalt.ObjectRef{
		Bucket: "fixtures",
		Key:    "errors/" + t.Name() + "-copy-2.txt",
	}, nil)
	if !errors.Is(err, gestalt.ErrS3NotFound) {
		t.Fatalf("CopyObject missing error = %v, want ErrS3NotFound", err)
	}

	if meta.ETag == "" {
		t.Fatal("WriteString(existing) ETag is empty")
	}
}
