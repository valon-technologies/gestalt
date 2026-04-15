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

	s3store "github.com/valon-technologies/gestalt/server/core/s3"
)

type StubS3 struct {
	mu      sync.RWMutex
	buckets map[string]map[string]*stubS3Object
	Err     error
	Now     func() time.Time
}

type stubS3Object struct {
	meta s3store.ObjectMeta
	body []byte
}

func (s *StubS3) HeadObject(_ context.Context, ref s3store.ObjectRef) (s3store.ObjectMeta, error) {
	if s.Err != nil {
		return s3store.ObjectMeta{}, s.Err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	obj, ok := s.lookup(ref)
	if !ok {
		return s3store.ObjectMeta{}, s3store.ErrNotFound
	}
	return cloneObjectMeta(obj.meta), nil
}

func (s *StubS3) ReadObject(_ context.Context, req s3store.ReadRequest) (s3store.ReadResult, error) {
	if s.Err != nil {
		return s3store.ReadResult{}, s.Err
	}
	s.mu.RLock()
	obj, ok := s.lookup(req.Ref)
	if !ok {
		s.mu.RUnlock()
		return s3store.ReadResult{}, s3store.ErrNotFound
	}
	meta := cloneObjectMeta(obj.meta)
	body := append([]byte(nil), obj.body...)
	s.mu.RUnlock()
	start, end, err := applyStubRange(req.Range, int64(len(body)))
	if err != nil {
		return s3store.ReadResult{}, err
	}
	return s3store.ReadResult{
		Meta: meta,
		Body: io.NopCloser(bytes.NewReader(body[start:end])),
	}, nil
}

func (s *StubS3) WriteObject(_ context.Context, req s3store.WriteRequest) (s3store.ObjectMeta, error) {
	if s.Err != nil {
		return s3store.ObjectMeta{}, s.Err
	}
	if req.IfMatch != "" || req.IfNoneMatch != "" {
		if err := s.checkWritePreconditions(req.Ref, req.IfMatch, req.IfNoneMatch); err != nil {
			return s3store.ObjectMeta{}, err
		}
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return s3store.ObjectMeta{}, err
	}
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}
	meta := s3store.ObjectMeta{
		Ref:          req.Ref,
		ETag:         stubETag(body),
		Size:         int64(len(body)),
		ContentType:  req.ContentType,
		LastModified: now,
		Metadata:     s3store.CloneStringMap(req.Metadata),
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	bucket := s.ensureBucket(req.Ref.Bucket)
	bucket[req.Ref.Key] = &stubS3Object{
		meta: meta,
		body: append([]byte(nil), body...),
	}
	return cloneObjectMeta(meta), nil
}

func (s *StubS3) DeleteObject(_ context.Context, ref s3store.ObjectRef) error {
	if s.Err != nil {
		return s.Err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	bucket, ok := s.buckets[ref.Bucket]
	if !ok {
		return nil
	}
	delete(bucket, ref.Key)
	return nil
}

func (s *StubS3) ListObjects(_ context.Context, req s3store.ListRequest) (s3store.ListPage, error) {
	if s.Err != nil {
		return s3store.ListPage{}, s.Err
	}
	s.mu.RLock()
	keys := s.sortedKeys(req.Bucket)
	s.mu.RUnlock()

	cursor := req.ContinuationToken
	if cursor == "" {
		cursor = req.StartAfter
	}
	limit := int(req.MaxKeys)
	if limit <= 0 {
		limit = 1000
	}

	page := s3store.ListPage{}
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
		meta, err := s.HeadObject(context.Background(), s3store.ObjectRef{Bucket: req.Bucket, Key: key})
		if err != nil {
			return s3store.ListPage{}, err
		}
		page.Objects = append(page.Objects, meta)
		count++
		lastToken = key
	}
	page.NextContinuationToken = ""
	return page, nil
}

func (s *StubS3) CopyObject(_ context.Context, req s3store.CopyRequest) (s3store.ObjectMeta, error) {
	if s.Err != nil {
		return s3store.ObjectMeta{}, s.Err
	}
	if req.IfMatch != "" || req.IfNoneMatch != "" {
		if err := s.checkWritePreconditions(req.Source, req.IfMatch, req.IfNoneMatch); err != nil {
			return s3store.ObjectMeta{}, err
		}
	}
	s.mu.RLock()
	obj, ok := s.lookup(req.Source)
	if !ok {
		s.mu.RUnlock()
		return s3store.ObjectMeta{}, s3store.ErrNotFound
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
	bucket := s.ensureBucket(req.Destination.Bucket)
	bucket[req.Destination.Key] = &stubS3Object{meta: meta, body: body}
	return cloneObjectMeta(meta), nil
}

func (s *StubS3) PresignObject(_ context.Context, req s3store.PresignRequest) (s3store.PresignResult, error) {
	if s.Err != nil {
		return s3store.PresignResult{}, s.Err
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
	return s3store.PresignResult{
		URL:       fmt.Sprintf("https://example.invalid/%s/%s?%s", req.Ref.Bucket, url.PathEscape(req.Ref.Key), values.Encode()),
		Method:    req.Method,
		ExpiresAt: expiresAt,
		Headers:   s3store.CloneStringMap(req.Headers),
	}, nil
}

func (s *StubS3) Ping(context.Context) error { return s.Err }
func (s *StubS3) Close() error               { return nil }

func (s *StubS3) lookup(ref s3store.ObjectRef) (*stubS3Object, bool) {
	if s.buckets == nil {
		return nil, false
	}
	bucket, ok := s.buckets[ref.Bucket]
	if !ok {
		return nil, false
	}
	obj, ok := bucket[ref.Key]
	return obj, ok
}

func (s *StubS3) ensureBucket(name string) map[string]*stubS3Object {
	if s.buckets == nil {
		s.buckets = make(map[string]map[string]*stubS3Object)
	}
	bucket, ok := s.buckets[name]
	if !ok {
		bucket = make(map[string]*stubS3Object)
		s.buckets[name] = bucket
	}
	return bucket
}

func (s *StubS3) sortedKeys(bucket string) []string {
	if s.buckets == nil {
		return nil
	}
	objects := s.buckets[bucket]
	keys := make([]string, 0, len(objects))
	for key := range objects {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (s *StubS3) checkWritePreconditions(ref s3store.ObjectRef, ifMatch, ifNoneMatch string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	obj, ok := s.lookup(ref)
	if ifMatch != "" {
		if !ok || obj.meta.ETag != ifMatch {
			return s3store.ErrPreconditionFailed
		}
	}
	if ifNoneMatch != "" {
		if ifNoneMatch == "*" {
			if ok {
				return s3store.ErrPreconditionFailed
			}
			return nil
		}
		if ok && obj.meta.ETag == ifNoneMatch {
			return s3store.ErrPreconditionFailed
		}
	}
	return nil
}

func applyStubRange(r *s3store.ByteRange, size int64) (int64, int64, error) {
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
		return 0, 0, s3store.ErrInvalidRange
	}
	return start, end, nil
}

func stubETag(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func cloneObjectMeta(meta s3store.ObjectMeta) s3store.ObjectMeta {
	meta.Metadata = s3store.CloneStringMap(meta.Metadata)
	return meta
}
