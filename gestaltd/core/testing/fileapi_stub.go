package coretesting

import (
	"context"
	"fmt"
	"sync"

	"github.com/valon-technologies/gestalt/server/core/fileapi"
)

type StubFileAPI struct {
	mu      sync.RWMutex
	nextID  int
	nextURL int
	objects map[string]stubFileObject
	urls    map[string]string
	Err     error
}

type stubFileObject struct {
	info fileapi.ObjectInfo
	data []byte
}

func (s *StubFileAPI) CreateBlob(_ context.Context, parts []fileapi.BlobPart, options fileapi.BlobOptions) (fileapi.ObjectInfo, error) {
	if s.Err != nil {
		return fileapi.ObjectInfo{}, s.Err
	}
	data, err := s.resolveParts(parts, options.Endings)
	if err != nil {
		return fileapi.ObjectInfo{}, err
	}
	info := fileapi.ObjectInfo{
		ID:   s.nextObjectID(),
		Kind: fileapi.ObjectKindBlob,
		Size: int64(len(data)),
		Type: fileapi.NormalizeType(options.Type),
	}
	s.storeObject(info, data)
	return info, nil
}

func (s *StubFileAPI) CreateFile(_ context.Context, parts []fileapi.BlobPart, name string, options fileapi.FileOptions) (fileapi.ObjectInfo, error) {
	if s.Err != nil {
		return fileapi.ObjectInfo{}, s.Err
	}
	data, err := s.resolveParts(parts, options.Endings)
	if err != nil {
		return fileapi.ObjectInfo{}, err
	}
	info := fileapi.ObjectInfo{
		ID:           s.nextObjectID(),
		Kind:         fileapi.ObjectKindFile,
		Size:         int64(len(data)),
		Type:         fileapi.NormalizeType(options.Type),
		Name:         name,
		LastModified: fileapi.ResolveLastModified(options.LastModified),
	}
	s.storeObject(info, data)
	return info, nil
}

func (s *StubFileAPI) Stat(_ context.Context, id string) (fileapi.ObjectInfo, error) {
	if s.Err != nil {
		return fileapi.ObjectInfo{}, s.Err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	object, ok := s.objects[id]
	if !ok {
		return fileapi.ObjectInfo{}, fileapi.ErrNotFound
	}
	return object.info, nil
}

func (s *StubFileAPI) Slice(_ context.Context, id string, start, end *int64, contentType string) (fileapi.ObjectInfo, error) {
	if s.Err != nil {
		return fileapi.ObjectInfo{}, s.Err
	}
	s.mu.RLock()
	object, ok := s.objects[id]
	s.mu.RUnlock()
	if !ok {
		return fileapi.ObjectInfo{}, fileapi.ErrNotFound
	}
	data := fileapi.SliceBytes(object.data, start, end)
	info := fileapi.ObjectInfo{
		ID:   s.nextObjectID(),
		Kind: fileapi.ObjectKindBlob,
		Size: int64(len(data)),
		Type: fileapi.NormalizeType(contentType),
	}
	s.storeObject(info, data)
	return info, nil
}

func (s *StubFileAPI) ReadBytes(_ context.Context, id string) ([]byte, error) {
	if s.Err != nil {
		return nil, s.Err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	object, ok := s.objects[id]
	if !ok {
		return nil, fileapi.ErrNotFound
	}
	return append([]byte(nil), object.data...), nil
}

func (s *StubFileAPI) CreateObjectURL(_ context.Context, id string) (string, error) {
	if s.Err != nil {
		return "", s.Err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.objects[id]; !ok {
		return "", fileapi.ErrNotFound
	}
	s.nextURL++
	url := fmt.Sprintf("blob:gestalt/%d", s.nextURL)
	if s.urls == nil {
		s.urls = make(map[string]string)
	}
	s.urls[url] = id
	return url, nil
}

func (s *StubFileAPI) ResolveObjectURL(_ context.Context, url string) (fileapi.ObjectInfo, error) {
	if s.Err != nil {
		return fileapi.ObjectInfo{}, s.Err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.urls[url]
	if !ok {
		return fileapi.ObjectInfo{}, fileapi.ErrNotFound
	}
	object, ok := s.objects[id]
	if !ok {
		return fileapi.ObjectInfo{}, fileapi.ErrNotFound
	}
	return object.info, nil
}

func (s *StubFileAPI) RevokeObjectURL(_ context.Context, url string) error {
	if s.Err != nil {
		return s.Err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.urls, url)
	return nil
}

func (s *StubFileAPI) Ping(context.Context) error { return s.Err }
func (s *StubFileAPI) Close() error               { return nil }

func (s *StubFileAPI) resolveParts(parts []fileapi.BlobPart, endings fileapi.LineEndings) ([]byte, error) {
	var data []byte
	for _, part := range parts {
		switch part.Kind {
		case fileapi.BlobPartKindString:
			data = append(data, fileapi.ConvertStringPart(part.StringData, endings)...)
		case fileapi.BlobPartKindBytes:
			data = append(data, part.BytesData...)
		case fileapi.BlobPartKindBlobID:
			s.mu.RLock()
			object, ok := s.objects[part.BlobID]
			s.mu.RUnlock()
			if !ok {
				return nil, fileapi.ErrNotFound
			}
			data = append(data, object.data...)
		default:
			return nil, fmt.Errorf("unsupported blob part kind %q", part.Kind)
		}
	}
	return data, nil
}

func (s *StubFileAPI) storeObject(info fileapi.ObjectInfo, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.objects == nil {
		s.objects = make(map[string]stubFileObject)
	}
	s.objects[info.ID] = stubFileObject{
		info: info,
		data: append([]byte(nil), data...),
	}
}

func (s *StubFileAPI) nextObjectID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	return fmt.Sprintf("obj-%d", s.nextID)
}
