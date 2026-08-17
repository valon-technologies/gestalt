package appregistryremote

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
)

func TestSignedUploadHeadersIncludeDigest(t *testing.T) {
	t.Parallel()
	digest := sha256.Sum256([]byte("artifact"))
	digestHex := hex.EncodeToString(digest[:])
	headers, err := signedUploadHeaders("https://storage.googleapis.com/bucket/object?X-Goog-Algorithm=GOOG4-RSA-SHA256", digestHex)
	if err != nil {
		t.Fatalf("signedUploadHeaders() error = %v", err)
	}
	if headers["Content-Type"] != "application/octet-stream" {
		t.Fatalf("Content-Type = %q", headers["Content-Type"])
	}
	want := base64.StdEncoding.EncodeToString(digest[:])
	if headers["x-goog-content-sha256"] != want {
		t.Fatalf("x-goog-content-sha256 = %q, want %q", headers["x-goog-content-sha256"], want)
	}
}

func TestSignedUploadHeadersAllowMissingDigest(t *testing.T) {
	t.Parallel()
	headers, err := signedUploadHeaders("https://upload.example/object", "")
	if err != nil {
		t.Fatalf("signedUploadHeaders() error = %v", err)
	}
	if _, ok := headers["x-goog-content-sha256"]; ok {
		t.Fatalf("unexpected digest header: %#v", headers)
	}
}

func TestSignedUploadHeadersRejectInvalidDigest(t *testing.T) {
	t.Parallel()
	_, err := signedUploadHeaders("https://upload.example/object", "not-a-digest")
	if err == nil || !strings.Contains(err.Error(), "decode artifact sha256") {
		t.Fatalf("signedUploadHeaders() = %v, want decode error", err)
	}
}
