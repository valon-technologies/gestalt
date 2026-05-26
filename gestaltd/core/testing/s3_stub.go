package coretesting

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	s3sdk "github.com/valon-technologies/gestalt/sdk/go/s3"
)

type StubS3 struct {
	mu      sync.RWMutex
	objects map[string]*stubS3Object
	Err     error
	Now     func() time.Time
}

type stubS3Object struct {
	meta s3sdk.ObjectMeta
	body []byte
}

func (s *StubS3) HeadObject(_ context.Context, ref s3sdk.ObjectRef) (s3sdk.ObjectMeta, error) {
	if s.Err != nil {
		return s3sdk.ObjectMeta{}, s.Err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	obj, ok := s.lookup(ref)
	if !ok {
		return s3sdk.ObjectMeta{}, s3sdk.ErrNotFound
	}
	return cloneObjectMeta(obj.meta), nil
}

func (s *StubS3) ReadObject(_ context.Context, req s3sdk.ReadRequest) (s3sdk.ReadResult, error) {
	if s.Err != nil {
		return s3sdk.ReadResult{}, s.Err
	}
	s.mu.RLock()
	obj, ok := s.lookup(req.Ref)
	if !ok {
		s.mu.RUnlock()
		return s3sdk.ReadResult{}, s3sdk.ErrNotFound
	}
	meta := cloneObjectMeta(obj.meta)
	body := append([]byte(nil), obj.body...)
	s.mu.RUnlock()
	start, end, err := applyStubRange(req.Range, int64(len(body)))
	if err != nil {
		return s3sdk.ReadResult{}, err
	}
	return s3sdk.ReadResult{
		Meta: meta,
		Body: io.NopCloser(bytes.NewReader(body[start:end])),
	}, nil
}

func (s *StubS3) WriteObject(_ context.Context, req s3sdk.WriteRequest) (s3sdk.ObjectMeta, error) {
	if s.Err != nil {
		return s3sdk.ObjectMeta{}, s.Err
	}
	if req.IfMatch != "" || req.IfNoneMatch != "" {
		if err := s.checkWritePreconditions(req.Ref, req.IfMatch, req.IfNoneMatch); err != nil {
			return s3sdk.ObjectMeta{}, err
		}
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return s3sdk.ObjectMeta{}, err
	}
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}
	meta := s3sdk.ObjectMeta{
		Ref:          req.Ref,
		ETag:         stubETag(body),
		Size:         int64(len(body)),
		ContentType:  req.ContentType,
		LastModified: now,
		Metadata:     s3sdk.CloneStringMap(req.Metadata),
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureObjects()[req.Ref.Key] = &stubS3Object{
		meta: meta,
		body: append([]byte(nil), body...),
	}
	return cloneObjectMeta(meta), nil
}

func (s *StubS3) DeleteObject(_ context.Context, ref s3sdk.ObjectRef) error {
	if s.Err != nil {
		return s.Err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.objects, ref.Key)
	return nil
}

func (s *StubS3) ListObjects(_ context.Context, req s3sdk.ListRequest) (s3sdk.ListPage, error) {
	if s.Err != nil {
		return s3sdk.ListPage{}, s.Err
	}
	s.mu.RLock()
	keys := s.sortedKeys()
	s.mu.RUnlock()

	cursor := req.ContinuationToken
	if cursor == "" {
		cursor = req.StartAfter
	}
	limit := int(req.MaxKeys)
	if limit <= 0 {
		limit = 1000
	}

	page := s3sdk.ListPage{}
	seenPrefixes := map[string]struct{}{}
	count := 0
	lastToken := ""
	for _, key := range keys {
		if req.Prefix != "" && !strings.HasPrefix(key, req.Prefix) {
			continue
		}
		if cursor != "" {
			if key <= cursor {
				continue
			}
			if req.Delimiter != "" && strings.HasSuffix(cursor, req.Delimiter) && strings.HasPrefix(key, cursor) {
				continue
			}
		}
		if req.Delimiter != "" {
			rest := strings.TrimPrefix(key, req.Prefix)
			if idx := strings.Index(rest, req.Delimiter); idx >= 0 {
				prefix := req.Prefix + rest[:idx+len(req.Delimiter)]
				if _, ok := seenPrefixes[prefix]; ok {
					continue
				}
				if count == limit {
					page.HasMore = true
					page.NextContinuationToken = lastToken
					return page, nil
				}
				seenPrefixes[prefix] = struct{}{}
				page.CommonPrefixes = append(page.CommonPrefixes, prefix)
				count++
				lastToken = prefix
				continue
			}
		}
		if count == limit {
			page.HasMore = true
			page.NextContinuationToken = lastToken
			return page, nil
		}
		meta, err := s.HeadObject(context.Background(), s3sdk.ObjectRef{Key: key})
		if err != nil {
			return s3sdk.ListPage{}, err
		}
		page.Objects = append(page.Objects, meta)
		count++
		lastToken = key
	}
	page.NextContinuationToken = ""
	return page, nil
}

func (s *StubS3) CopyObject(_ context.Context, req s3sdk.CopyRequest) (s3sdk.ObjectMeta, error) {
	if s.Err != nil {
		return s3sdk.ObjectMeta{}, s.Err
	}
	if req.IfMatch != "" || req.IfNoneMatch != "" {
		if err := s.checkWritePreconditions(req.Source, req.IfMatch, req.IfNoneMatch); err != nil {
			return s3sdk.ObjectMeta{}, err
		}
	}
	s.mu.RLock()
	obj, ok := s.lookup(req.Source)
	if !ok {
		s.mu.RUnlock()
		return s3sdk.ObjectMeta{}, s3sdk.ErrNotFound
	}
	body := append([]byte(nil), obj.body...)
	meta := cloneObjectMeta(obj.meta)
	s.mu.RUnlock()

	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}
	meta.Ref = req.Destination
	meta.LastModified = now
	meta.ETag = stubETag(body)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureObjects()[req.Destination.Key] = &stubS3Object{meta: meta, body: body}
	return cloneObjectMeta(meta), nil
}

func (s *StubS3) PresignObject(_ context.Context, req s3sdk.PresignRequest) (s3sdk.PresignResult, error) {
	if s.Err != nil {
		return s3sdk.PresignResult{}, s.Err
	}
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}
	expiresAt := now.Add(req.Expires)
	values := url.Values{}
	values.Set("method", string(req.Method))
	if req.Expires > 0 {
		values.Set("expires", expiresAt.Format(time.RFC3339))
	}
	if req.ContentType != "" {
		values.Set("contentType", req.ContentType)
	}
	if req.ContentDisposition != "" {
		values.Set("contentDisposition", req.ContentDisposition)
	}
	for key, value := range req.Headers {
		values.Set("header."+key, value)
	}
	return s3sdk.PresignResult{
		URL:       fmt.Sprintf("https://example.invalid/%s?%s", url.PathEscape(req.Ref.Key), values.Encode()),
		Method:    req.Method,
		ExpiresAt: expiresAt,
		Headers:   s3sdk.CloneStringMap(req.Headers),
	}, nil
}

func (s *StubS3) Ping(context.Context) error { return s.Err }
func (s *StubS3) Close() error               { return nil }

func (s *StubS3) lookup(ref s3sdk.ObjectRef) (*stubS3Object, bool) {
	if s.objects == nil {
		return nil, false
	}
	obj, ok := s.objects[ref.Key]
	return obj, ok
}

func (s *StubS3) ensureObjects() map[string]*stubS3Object {
	if s.objects == nil {
		s.objects = make(map[string]*stubS3Object)
	}
	return s.objects
}

func (s *StubS3) sortedKeys() []string {
	if s.objects == nil {
		return nil
	}
	keys := make([]string, 0, len(s.objects))
	for key := range s.objects {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (s *StubS3) checkWritePreconditions(ref s3sdk.ObjectRef, ifMatch, ifNoneMatch string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	obj, ok := s.lookup(ref)
	if ifMatch != "" {
		if !ok || obj.meta.ETag != ifMatch {
			return s3sdk.ErrPreconditionFailed
		}
	}
	if ifNoneMatch != "" {
		if ifNoneMatch == "*" {
			if ok {
				return s3sdk.ErrPreconditionFailed
			}
			return nil
		}
		if ok && obj.meta.ETag == ifNoneMatch {
			return s3sdk.ErrPreconditionFailed
		}
	}
	return nil
}

func applyStubRange(r *s3sdk.ByteRange, size int64) (int64, int64, error) {
	if r == nil {
		return 0, size, nil
	}
	start := int64(0)
	end := size
	if r.Start != nil {
		start = *r.Start
	}
	if r.End != nil {
		end = *r.End + 1
	}
	if start < 0 || end < 0 || start > end || end > size {
		return 0, 0, s3sdk.ErrInvalidRange
	}
	return start, end, nil
}

func stubETag(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func cloneObjectMeta(meta s3sdk.ObjectMeta) s3sdk.ObjectMeta {
	meta.Metadata = s3sdk.CloneStringMap(meta.Metadata)
	return meta
}
