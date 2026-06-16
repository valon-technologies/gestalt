package provider

import (
	"context"
	"reflect"
	"sort"
	"sync"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"github.com/valon-technologies/gestalt/sdk/go/indexeddb"
)

type Provider struct {
	mu     sync.RWMutex
	stores map[string]*objectStore
}

type objectStore struct {
	records map[string]gestalt.Record
	schema  gestalt.ObjectStoreOptions
}

func New() *Provider {
	return &Provider{stores: make(map[string]*objectStore)}
}

func (p *Provider) Configure(context.Context, string, map[string]any) error { return nil }

func (p *Provider) CreateObjectStore(_ context.Context, name string, schema gestalt.ObjectStoreOptions) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if s, ok := p.stores[name]; ok {
		s.schema = schema
		return nil
	}
	p.stores[name] = &objectStore{
		records: make(map[string]gestalt.Record),
		schema:  schema,
	}
	return nil
}

func (p *Provider) DeleteObjectStore(_ context.Context, name string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.stores, name)
	return nil
}

func (p *Provider) Get(_ context.Context, req gestalt.IndexedDBObjectStoreRequest) (gestalt.Record, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	s, ok := p.stores[req.Store]
	if !ok {
		return nil, gestalt.NotFound("not found")
	}
	rec, ok := s.records[req.ID]
	if !ok {
		return nil, gestalt.NotFound("not found")
	}
	return cloneRecord(rec), nil
}

func (p *Provider) GetKey(_ context.Context, req gestalt.IndexedDBObjectStoreRequest) (string, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	s, ok := p.stores[req.Store]
	if !ok {
		return "", gestalt.NotFound("not found")
	}
	if _, ok := s.records[req.ID]; !ok {
		return "", gestalt.NotFound("not found")
	}
	return req.ID, nil
}

func (p *Provider) Add(_ context.Context, req gestalt.IndexedDBRecordRequest) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.getStoreLocked(req.Store)
	id := fieldString(req.Record, "id")
	if _, ok := s.records[id]; ok {
		return gestalt.AlreadyExists("already exists")
	}
	for _, idx := range s.schema.Indexes {
		if !idx.Unique {
			continue
		}
		for _, existing := range s.records {
			if fieldsMatch(existing, req.Record, idx.KeyPath) {
				return gestalt.AlreadyExists("unique index violation")
			}
		}
	}
	s.records[id] = cloneRecord(req.Record)
	return nil
}

func (p *Provider) Put(_ context.Context, req gestalt.IndexedDBRecordRequest) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.getStoreLocked(req.Store)
	s.records[fieldString(req.Record, "id")] = cloneRecord(req.Record)
	return nil
}

func (p *Provider) Delete(_ context.Context, req gestalt.IndexedDBObjectStoreRequest) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.getStoreLocked(req.Store)
	delete(s.records, req.ID)
	return nil
}

func (p *Provider) Clear(_ context.Context, store string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.getStoreLocked(store)
	s.records = make(map[string]gestalt.Record)
	return nil
}

func (p *Provider) GetAll(_ context.Context, req gestalt.IndexedDBObjectStoreRangeRequest) ([]gestalt.Record, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	s, ok := p.stores[req.Store]
	if !ok {
		return nil, nil
	}
	recs := make([]gestalt.Record, 0, len(s.records))
	for id, r := range s.records {
		ok, err := indexeddb.MatchQuery(id, req.Query)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		recs = append(recs, cloneRecord(r))
	}
	sort.SliceStable(recs, func(i, j int) bool {
		return indexeddb.CompareKeys(recs[i]["id"], recs[j]["id"]) < 0
	})
	return limitRecords(recs, req.Count), nil
}

func (p *Provider) GetAllKeys(ctx context.Context, req gestalt.IndexedDBObjectStoreRangeRequest) ([]string, error) {
	records, err := p.GetAll(ctx, req)
	if err != nil {
		return nil, err
	}
	keys := make([]string, len(records))
	for i, rec := range records {
		keys[i] = fieldString(rec, "id")
	}
	return keys, nil
}

func (p *Provider) Count(_ context.Context, req gestalt.IndexedDBObjectStoreRangeRequest) (int64, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	s, ok := p.stores[req.Store]
	if !ok {
		return 0, nil
	}
	var count int64
	for id := range s.records {
		ok, err := indexeddb.MatchQuery(id, req.Query)
		if err != nil {
			return 0, err
		}
		if ok {
			count++
		}
	}
	return count, nil
}

func (p *Provider) DeleteRange(context.Context, gestalt.IndexedDBObjectStoreRangeRequest) (int64, error) {
	return 0, nil
}

func (p *Provider) IndexGet(_ context.Context, req gestalt.IndexedDBIndexQueryRequest) (gestalt.Record, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	s, ok := p.stores[req.Store]
	if !ok {
		return nil, gestalt.NotFound("not found")
	}
	for _, rec := range s.records {
		match, err := indexMatches(rec, s.schema, req.Index, req.Query)
		if err != nil {
			return nil, err
		}
		if match {
			return cloneRecord(rec), nil
		}
	}
	return nil, gestalt.NotFound("not found")
}

func (p *Provider) IndexGetKey(ctx context.Context, req gestalt.IndexedDBIndexQueryRequest) (string, error) {
	rec, err := p.IndexGet(ctx, req)
	if err != nil {
		return "", err
	}
	return fieldString(rec, "id"), nil
}

func (p *Provider) IndexGetAll(_ context.Context, req gestalt.IndexedDBIndexQueryRequest) ([]gestalt.Record, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	s, ok := p.stores[req.Store]
	if !ok {
		return nil, nil
	}
	var recs []gestalt.Record
	for _, rec := range s.records {
		match, err := indexMatches(rec, s.schema, req.Index, req.Query)
		if err != nil {
			return nil, err
		}
		if match {
			recs = append(recs, cloneRecord(rec))
		}
	}
	sort.SliceStable(recs, func(i, j int) bool {
		ki, _ := indexKey(recs[i], s.schema, req.Index)
		kj, _ := indexKey(recs[j], s.schema, req.Index)
		if c := indexeddb.CompareKeys(ki, kj); c != 0 {
			return c < 0
		}
		return indexeddb.CompareKeys(recs[i]["id"], recs[j]["id"]) < 0
	})
	return limitRecords(recs, req.Count), nil
}

func (p *Provider) IndexGetAllKeys(ctx context.Context, req gestalt.IndexedDBIndexQueryRequest) ([]string, error) {
	records, err := p.IndexGetAll(ctx, req)
	if err != nil {
		return nil, err
	}
	keys := make([]string, len(records))
	for i, rec := range records {
		keys[i] = fieldString(rec, "id")
	}
	return keys, nil
}

func (p *Provider) IndexCount(_ context.Context, req gestalt.IndexedDBIndexQueryRequest) (int64, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	s, ok := p.stores[req.Store]
	if !ok {
		return 0, nil
	}
	var count int64
	for _, rec := range s.records {
		match, err := indexMatches(rec, s.schema, req.Index, req.Query)
		if err != nil {
			return 0, err
		}
		if match {
			count++
		}
	}
	return count, nil
}

func (p *Provider) IndexDelete(_ context.Context, req gestalt.IndexedDBIndexQueryRequest) (int64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.getStoreLocked(req.Store)
	var toDelete []string
	for id, rec := range s.records {
		match, err := indexMatches(rec, s.schema, req.Index, req.Query)
		if err != nil {
			return 0, err
		}
		if match {
			toDelete = append(toDelete, id)
		}
	}
	for _, id := range toDelete {
		delete(s.records, id)
	}
	return int64(len(toDelete)), nil
}

func (p *Provider) OpenCursor(context.Context, gestalt.IndexedDBOpenCursorRequest) (gestalt.IndexedDBCursor, error) {
	return nil, gestalt.Unimplemented("open cursor is not implemented")
}

func (p *Provider) BeginTransaction(context.Context, gestalt.IndexedDBBeginTransactionRequest) (gestalt.IndexedDBTransaction, error) {
	return nil, gestalt.Unimplemented("transactions are not implemented")
}

func (p *Provider) getStoreLocked(name string) *objectStore {
	if s, ok := p.stores[name]; ok {
		return s
	}
	s := &objectStore{records: make(map[string]gestalt.Record)}
	p.stores[name] = s
	return s
}

func fieldString(rec gestalt.Record, key string) string {
	value, ok := rec[key].(string)
	if !ok {
		return ""
	}
	return value
}

func indexKey(rec gestalt.Record, schema gestalt.ObjectStoreOptions, indexName string) (any, bool) {
	var keyPath []string
	for _, idx := range schema.Indexes {
		if idx.Name == indexName {
			keyPath = idx.KeyPath
			break
		}
	}
	if keyPath == nil {
		return nil, false
	}
	parts := make([]any, len(keyPath))
	for i, field := range keyPath {
		recordValue, ok := rec[field]
		if !ok {
			return nil, false
		}
		parts[i] = recordValue
	}
	if len(parts) == 1 {
		return parts[0], true
	}
	return parts, true
}

func indexMatches(rec gestalt.Record, schema gestalt.ObjectStoreOptions, indexName string, query *gestalt.IndexedDBQuery) (bool, error) {
	key, ok := indexKey(rec, schema, indexName)
	if !ok {
		return false, nil
	}
	return indexeddb.MatchQuery(key, query)
}

func fieldsMatch(a, b gestalt.Record, keyPath []string) bool {
	for _, field := range keyPath {
		left, leftOK := a[field]
		right, rightOK := b[field]
		if !leftOK || !rightOK || !reflect.DeepEqual(left, right) {
			return false
		}
	}
	return true
}

func cloneRecord(record gestalt.Record) gestalt.Record {
	if record == nil {
		return nil
	}
	cloned := make(gestalt.Record, len(record))
	for k, v := range record {
		cloned[k] = v
	}
	return cloned
}

func limitRecords(recs []gestalt.Record, count *uint32) []gestalt.Record {
	if count == nil || *count == 0 || int(*count) >= len(recs) {
		return recs
	}
	return recs[:*count]
}
