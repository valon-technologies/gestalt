package appregistry

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

const (
	UploadHeaderContentLength          = "Content-Length"
	UploadHeaderXGoogIfGenerationMatch = "x-goog-if-generation-match"
	UploadHeaderXGoogMetaSHA256        = "x-goog-meta-sha256"
	UploadHeaderXGoogContentSHA256     = "x-goog-content-sha256"
)

var signedUploadHeaderOrder = []string{
	UploadHeaderContentLength,
	UploadHeaderXGoogIfGenerationMatch,
	UploadHeaderXGoogMetaSHA256,
	UploadHeaderXGoogContentSHA256,
}

// BuildSignedUploadHeaders returns canonical signed PUT headers for staged uploads.
func BuildSignedUploadHeaders(contentLength int64, sha256Hex string) (map[string]string, error) {
	if contentLength <= 0 {
		return nil, fmt.Errorf("content length is required")
	}
	digestHex, err := normalizeUploadSHA256(sha256Hex)
	if err != nil {
		return nil, err
	}
	return map[string]string{
		UploadHeaderContentLength:          fmt.Sprintf("%d", contentLength),
		UploadHeaderXGoogIfGenerationMatch: "0",
		UploadHeaderXGoogMetaSHA256:        digestHex,
		UploadHeaderXGoogContentSHA256:     digestHex,
	}, nil
}

func normalizeUploadSHA256(sha256Hex string) (digestHex string, err error) {
	digestHex = strings.ToLower(strings.TrimSpace(sha256Hex))
	if digestHex == "" {
		return "", fmt.Errorf("sha256 is required")
	}
	sum, err := hex.DecodeString(digestHex)
	if err != nil {
		return "", fmt.Errorf("decode sha256: %w", err)
	}
	if len(sum) != sha256.Size {
		return "", fmt.Errorf("sha256 must be %d bytes", sha256.Size)
	}
	return digestHex, nil
}

func signedUploadHeaderLines(headers map[string]string) []string {
	if len(headers) == 0 {
		return nil
	}
	lines := make([]string, 0, len(signedUploadHeaderOrder))
	for _, name := range signedUploadHeaderOrder {
		value, ok := headers[name]
		if !ok {
			continue
		}
		lines = append(lines, name+":"+value)
	}
	return lines
}

func cloneSignedUploadHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	out := make(map[string]string, len(headers))
	for _, name := range signedUploadHeaderOrder {
		if value, ok := headers[name]; ok {
			out[name] = value
		}
	}
	return out
}

// SignedUploadHeadersForResponse returns canonical upload headers for authenticated API responses.
func SignedUploadHeadersForResponse(headers map[string]string) map[string]string {
	return cloneSignedUploadHeaders(headers)
}

func validateSignedUploadHeaders(headers map[string]string, contentLength int64, sha256Hex string) error {
	expected, err := BuildSignedUploadHeaders(contentLength, sha256Hex)
	if err != nil {
		return err
	}
	for _, name := range signedUploadHeaderOrder {
		if headers[name] != expected[name] {
			return fmt.Errorf("signed upload header %q = %q, want %q", name, headers[name], expected[name])
		}
	}
	extra := make([]string, 0)
	for name := range headers {
		found := false
		for _, expectedName := range signedUploadHeaderOrder {
			if name == expectedName {
				found = true
				break
			}
		}
		if !found {
			extra = append(extra, name)
		}
	}
	if len(extra) > 0 {
		sort.Strings(extra)
		return fmt.Errorf("unexpected signed upload headers: %s", strings.Join(extra, ", "))
	}
	return nil
}
