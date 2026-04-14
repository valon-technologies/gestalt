package fileapi

import (
	"context"
	"encoding/base64"
	"errors"
	"runtime"
	"strings"
	"time"
)

var (
	ErrNotFound    = errors.New("fileapi: not found")
	ErrNotReadable = errors.New("fileapi: not readable")
	ErrSecurity    = errors.New("fileapi: security error")
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
	BlobPartString BlobPartKind = "string"
	BlobPartBytes  BlobPartKind = "bytes"
	BlobPartBlob   BlobPartKind = "blob"
)

type BlobPart struct {
	kind   BlobPartKind
	text   string
	data   []byte
	blobID string
}

func StringPart(value string) BlobPart {
	return BlobPart{kind: BlobPartString, text: value}
}

func BytesPart(value []byte) BlobPart {
	return BlobPart{kind: BlobPartBytes, data: append([]byte(nil), value...)}
}

func BlobRefPart(id string) BlobPart {
	return BlobPart{kind: BlobPartBlob, blobID: id}
}

func (p BlobPart) Kind() BlobPartKind { return p.kind }

func (p BlobPart) StringData() string { return p.text }

func (p BlobPart) BytesData() []byte { return append([]byte(nil), p.data...) }

func (p BlobPart) BlobID() string { return p.blobID }

type BlobOptions struct {
	Type    string
	Endings LineEndings
}

type FileOptions struct {
	Type         string
	Endings      LineEndings
	LastModified time.Time
	Now          func() time.Time
}

type ObjectInfo struct {
	Kind         ObjectKind
	ID           string
	Size         int64
	Type         string
	Name         string
	LastModified time.Time
}

func (o ObjectInfo) IsFile() bool { return o.Kind == ObjectKindFile }

type FileAPI interface {
	CreateBlob(ctx context.Context, parts []BlobPart, opts BlobOptions) (ObjectInfo, error)
	CreateFile(ctx context.Context, parts []BlobPart, name string, opts FileOptions) (ObjectInfo, error)
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
		if r < 0x20 || r > 0x7e {
			return ""
		}
	}
	return strings.ToLower(value)
}

func ConvertStringPart(value string, endings LineEndings) string {
	if endings != LineEndingsNative {
		return value
	}
	native := "\n"
	if nativeLineEndingCRLF() {
		native = "\r\n"
	}
	return normalizeLineEndings(value, native)
}

func ResolveLastModified(value time.Time, now func() time.Time) time.Time {
	if !value.IsZero() {
		return value.UTC()
	}
	if now == nil {
		now = time.Now
	}
	return now().UTC()
}

func SliceBounds(size int64, start, end *int64) (int64, int64) {
	relativeStart := int64(0)
	if start != nil {
		switch {
		case *start < 0:
			relativeStart = maxInt64(size+*start, 0)
		default:
			relativeStart = minInt64(*start, size)
		}
	}

	relativeEnd := size
	if end != nil {
		switch {
		case *end < 0:
			relativeEnd = maxInt64(size+*end, 0)
		default:
			relativeEnd = minInt64(*end, size)
		}
	}

	if relativeEnd < relativeStart {
		relativeEnd = relativeStart
	}
	return relativeStart, relativeEnd
}

func SliceBytes(data []byte, start, end *int64) []byte {
	lower, upper := SliceBounds(int64(len(data)), start, end)
	sliced := data[lower:upper]
	return append([]byte(nil), sliced...)
}

func PackageDataURL(mimeType string, data []byte) string {
	if mimeType = NormalizeType(mimeType); mimeType != "" {
		return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data)
	}
	return "data:;base64," + base64.StdEncoding.EncodeToString(data)
}

func normalizeLineEndings(value, native string) string {
	var b strings.Builder
	for i := 0; i < len(value); i++ {
		switch value[i] {
		case '\r':
			b.WriteString(native)
			if i+1 < len(value) && value[i+1] == '\n' {
				i++
			}
		case '\n':
			b.WriteString(native)
		default:
			b.WriteByte(value[i])
		}
	}
	return b.String()
}

func nativeLineEndingCRLF() bool {
	return runtime.GOOS == "windows"
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
