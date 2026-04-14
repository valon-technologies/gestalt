package fileapi

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"time"
)

var (
	ErrNotFound = errors.New("fileapi: not found")
	ErrSecurity = errors.New("fileapi: security error")
)

type ObjectKind string

const (
	ObjectKindBlob ObjectKind = "blob"
	ObjectKindFile ObjectKind = "file"
)

type LineEndings string

const (
	LineEndingsTransparent LineEndings = "transparent"
	LineEndingsNative      LineEndings = "native"
)

type BlobPartKind string

const (
	BlobPartKindString BlobPartKind = "string"
	BlobPartKindBytes  BlobPartKind = "bytes"
	BlobPartKindBlobID BlobPartKind = "blob_id"
)

type BlobPart struct {
	Kind       BlobPartKind
	StringData string
	BytesData  []byte
	BlobID     string
}

func StringPart(value string) BlobPart {
	return BlobPart{Kind: BlobPartKindString, StringData: value}
}

func BytesPart(value []byte) BlobPart {
	return BlobPart{Kind: BlobPartKindBytes, BytesData: append([]byte(nil), value...)}
}

func BlobRefPart(id string) BlobPart {
	return BlobPart{Kind: BlobPartKindBlobID, BlobID: id}
}

type BlobOptions struct {
	Type     string
	Endings  LineEndings
}

type FileOptions struct {
	Type         string
	Endings      LineEndings
	LastModified int64
}

type ObjectInfo struct {
	ID           string
	Kind         ObjectKind
	Size         int64
	Type         string
	Name         string
	LastModified int64
}

type FileAPI interface {
	CreateBlob(ctx context.Context, parts []BlobPart, options BlobOptions) (ObjectInfo, error)
	CreateFile(ctx context.Context, parts []BlobPart, name string, options FileOptions) (ObjectInfo, error)
	Stat(ctx context.Context, id string) (ObjectInfo, error)
	Slice(ctx context.Context, id string, start, end *int64, contentType string) (ObjectInfo, error)
	ReadBytes(ctx context.Context, id string) ([]byte, error)
	CreateObjectURL(ctx context.Context, id string) (string, error)
	ResolveObjectURL(ctx context.Context, url string) (ObjectInfo, error)
	RevokeObjectURL(ctx context.Context, url string) error
	Ping(ctx context.Context) error
	Close() error
}

func NormalizeType(value string) string {
	if value == "" {
		return ""
	}
	for _, r := range value {
		if r < 0x20 || r > 0x7E {
			return ""
		}
	}
	return strings.ToLower(value)
}

func ConvertStringPart(value string, endings LineEndings) []byte {
	if endings != LineEndingsNative {
		return []byte(value)
	}
	normalized := strings.ReplaceAll(value, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	if runtime.GOOS == "windows" {
		normalized = strings.ReplaceAll(normalized, "\n", "\r\n")
	}
	return []byte(normalized)
}

func ResolveLastModified(lastModified int64) int64 {
	if lastModified > 0 {
		return lastModified
	}
	return time.Now().UnixMilli()
}

func SliceBounds(size int64, start, end *int64) (int64, int64) {
	relativeStart := int64(0)
	if start != nil {
		if *start < 0 {
			relativeStart = max(size+*start, 0)
		} else {
			relativeStart = min(*start, size)
		}
	}
	relativeEnd := size
	if end != nil {
		if *end < 0 {
			relativeEnd = max(size+*end, 0)
		} else {
			relativeEnd = min(*end, size)
		}
	}
	if relativeEnd < relativeStart {
		relativeEnd = relativeStart
	}
	return relativeStart, relativeEnd
}

func SliceBytes(data []byte, start, end *int64) []byte {
	s, e := SliceBounds(int64(len(data)), start, end)
	return append([]byte(nil), data[s:e]...)
}

func PackageDataURL(mimeType string, data []byte) string {
	mimeType = NormalizeType(mimeType)
	if mimeType == "" {
		return "data:;base64," + base64.StdEncoding.EncodeToString(data)
	}
	return fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(data))
}
