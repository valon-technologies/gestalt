package coretesting

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/valon-technologies/gestalt/server/core/fileapi"
)

type StubFileAPI struct {
	mu      sync.Mutex
	objects map[string]stubFileObject
	urls    map[string]string
	nextID  int64
	Err     error
	Now     func() time.Time
}

type stubFileObject struct {
	info fileapi.ObjectInfo
	data []byte
}

func (s *StubFileAPI) CreateBlob(_ context.Context, parts []fileapi.BlobPart, opts fileapi.BlobOptions) (fileapi.ObjectInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := s.materializePartsLocked(parts, opts.Endings)
	if err != nil {
		return fileapi.ObjectInfo{}, err
	}
	info := fileapi.ObjectInfo{
		Kind: fileapi.ObjectKindBlob,
		ID:   s.allocateIDLocked("blob"),
		Size: int64(len(data)),
		Type: fileapi.NormalizeType(opts.Type),
	}
	s.storeLocked(info, data)
	return info, nil
}

func (s *StubFileAPI) CreateFile(_ context.Context, parts []fileapi.BlobPart, name string, opts fileapi.FileOptions) (fileapi.ObjectInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := s.materializePartsLocked(parts, opts.Endings)
	if err != nil {
		return fileapi.ObjectInfo{}, err
	}
	info := fileapi.ObjectInfo{
		Kind:         fileapi.ObjectKindFile,
		ID:           s.allocateIDLocked("file"),
		Size:         int64(len(data)),
		Type:         fileapi.NormalizeType(opts.Type),
		Name:         name,
		LastModified: fileapi.ResolveLastModified(opts.LastModified, s.now),
	}
	s.storeLocked(info, data)
	return info, nil
}

func (s *StubFileAPI) Stat(_ context.Context, id string) (fileapi.ObjectInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	obj, ok := s.objects[id]
	if !ok {
		return fileapi.ObjectInfo{}, fileapi.ErrNotFound
	}
	return obj.info, nil
}

func (s *StubFileAPI) Slice(_ context.Context, id string, start, end *int64, contentType string) (fileapi.ObjectInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	obj, ok := s.objects[id]
	if !ok {
		return fileapi.ObjectInfo{}, fileapi.ErrNotFound
	}
	sliced := fileapi.SliceBytes(obj.data, start, end)
	info := fileapi.ObjectInfo{
		Kind: fileapi.ObjectKindBlob,
		ID:   s.allocateIDLocked("blob"),
		Size: int64(len(sliced)),
		Type: fileapi.NormalizeType(contentType),
	}
	s.storeLocked(info, sliced)
	return info, nil
}

func (s *StubFileAPI) ReadBytes(_ context.Context, id string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	obj, ok := s.objects[id]
	if !ok {
		return nil, fileapi.ErrNotFound
	}
	return append([]byte(nil), obj.data...), nil
}

func (s *StubFileAPI) CreateObjectURL(_ context.Context, id string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.objects[id]; !ok {
		return "", fileapi.ErrNotFound
	}
	url := fmt.Sprintf("blob:gestalt/%s", s.allocateIDLocked("url"))
	if s.urls == nil {
		s.urls = make(map[string]string)
	}
	s.urls[url] = id
	return url, nil
}

func (s *StubFileAPI) ResolveObjectURL(_ context.Context, url string) (fileapi.ObjectInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id, ok := s.urls[url]
	if !ok {
		return fileapi.ObjectInfo{}, fileapi.ErrNotFound
	}
	obj, ok := s.objects[id]
	if !ok {
		return fileapi.ObjectInfo{}, fileapi.ErrNotFound
	}
	return obj.info, nil
}

func (s *StubFileAPI) RevokeObjectURL(_ context.Context, url string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.urls, url)
	return nil
}

func (s *StubFileAPI) Ping(context.Context) error { return s.Err }

func (s *StubFileAPI) Close() error { return nil }

func (s *StubFileAPI) materializePartsLocked(parts []fileapi.BlobPart, endings fileapi.LineEndings) ([]byte, error) {
	var data []byte
	for _, part := range parts {
		switch part.Kind() {
		case fileapi.BlobPartString:
			data = append(data, []byte(fileapi.ConvertStringPart(part.StringData(), endings))...)
		case fileapi.BlobPartBytes:
			data = append(data, part.BytesData()...)
		case fileapi.BlobPartBlob:
			obj, ok := s.objects[part.BlobID()]
			if !ok {
				return nil, fileapi.ErrNotFound
			}
			data = append(data, obj.data...)
		default:
			return nil, fmt.Errorf("unsupported blob part kind %q", part.Kind())
		}
	}
	return data, nil
}

func (s *StubFileAPI) storeLocked(info fileapi.ObjectInfo, data []byte) {
	if s.objects == nil {
		s.objects = make(map[string]stubFileObject)
	}
	s.objects[info.ID] = stubFileObject{
		info: info,
		data: append([]byte(nil), data...),
	}
}

func (s *StubFileAPI) allocateIDLocked(prefix string) string {
	s.nextID++
	return fmt.Sprintf("%s-%d", prefix, s.nextID)
}

func (s *StubFileAPI) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

var _ fileapi.FileAPI = (*StubFileAPI)(nil)
