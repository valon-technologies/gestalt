package provider

import (
	"context"
	"sync"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"
)

type Provider struct {
	proto.UnimplementedIndexedDBServer
	mu     sync.RWMutex
	stores map[string]*objectStore
}

type objectStore struct {
	records map[string]*structpb.Struct
	schema  *proto.ObjectStoreSchema
}

func New() *Provider {
	return &Provider{stores: make(map[string]*objectStore)}
}

func (p *Provider) Configure(context.Context, string, map[string]any) error { return nil }

func (p *Provider) getStore(name string) *objectStore {
	p.mu.Lock()
	defer p.mu.Unlock()
	if s, ok := p.stores[name]; ok {
		return s
	}
	s := &objectStore{records: make(map[string]*structpb.Struct)}
	p.stores[name] = s
	return s
}

func (p *Provider) CreateObjectStore(_ context.Context, req *proto.CreateObjectStoreRequest) (*emptypb.Empty, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if s, ok := p.stores[req.GetName()]; ok {
		s.schema = req.GetSchema()
	} else {
		p.stores[req.GetName()] = &objectStore{
			records: make(map[string]*structpb.Struct),
			schema:  req.GetSchema(),
		}
	}
	return &emptypb.Empty{}, nil
}

func (p *Provider) DeleteObjectStore(_ context.Context, req *proto.DeleteObjectStoreRequest) (*emptypb.Empty, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.stores, req.GetName())
	return &emptypb.Empty{}, nil
}

func (p *Provider) Get(_ context.Context, req *proto.ObjectStoreRequest) (*proto.RecordResponse, error) {
	s := p.getStore(req.GetStore())
	p.mu.RLock()
	defer p.mu.RUnlock()
	rec, ok := s.records[req.GetId()]
	if !ok {
		return nil, status.Error(codes.NotFound, "not found")
	}
	return &proto.RecordResponse{Record: rec}, nil
}

func (p *Provider) GetKey(_ context.Context, req *proto.ObjectStoreRequest) (*proto.KeyResponse, error) {
	s := p.getStore(req.GetStore())
	p.mu.RLock()
	defer p.mu.RUnlock()
	if _, ok := s.records[req.GetId()]; !ok {
		return nil, status.Error(codes.NotFound, "not found")
	}
	return &proto.KeyResponse{Key: req.GetId()}, nil
}

func (p *Provider) Add(_ context.Context, req *proto.RecordRequest) (*emptypb.Empty, error) {
	s := p.getStore(req.GetStore())
	id := fieldString(req.GetRecord(), "id")
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := s.records[id]; ok {
		return nil, status.Error(codes.AlreadyExists, "already exists")
	}
	if schema := s.schema; schema != nil {
		for _, idx := range schema.GetIndexes() {
			if !idx.GetUnique() {
				continue
			}
			for _, existing := range s.records {
				if fieldsMatch(existing, req.GetRecord(), idx.GetKeyPath()) {
					return nil, status.Error(codes.AlreadyExists, "unique index violation")
				}
			}
		}
	}
	s.records[id] = req.GetRecord()
	return &emptypb.Empty{}, nil
}

func (p *Provider) Put(_ context.Context, req *proto.RecordRequest) (*emptypb.Empty, error) {
	s := p.getStore(req.GetStore())
	id := fieldString(req.GetRecord(), "id")
	p.mu.Lock()
	defer p.mu.Unlock()
	s.records[id] = req.GetRecord()
	return &emptypb.Empty{}, nil
}

func (p *Provider) Delete(_ context.Context, req *proto.ObjectStoreRequest) (*emptypb.Empty, error) {
	s := p.getStore(req.GetStore())
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(s.records, req.GetId())
	return &emptypb.Empty{}, nil
}

func (p *Provider) Clear(_ context.Context, req *proto.ObjectStoreNameRequest) (*emptypb.Empty, error) {
	s := p.getStore(req.GetStore())
	p.mu.Lock()
	defer p.mu.Unlock()
	s.records = make(map[string]*structpb.Struct)
	return &emptypb.Empty{}, nil
}

func (p *Provider) GetAll(_ context.Context, req *proto.ObjectStoreRangeRequest) (*proto.RecordsResponse, error) {
	s := p.getStore(req.GetStore())
	p.mu.RLock()
	defer p.mu.RUnlock()
	recs := make([]*structpb.Struct, 0, len(s.records))
	for _, r := range s.records {
		recs = append(recs, r)
	}
	return &proto.RecordsResponse{Records: recs}, nil
}

func (p *Provider) GetAllKeys(_ context.Context, req *proto.ObjectStoreRangeRequest) (*proto.KeysResponse, error) {
	s := p.getStore(req.GetStore())
	p.mu.RLock()
	defer p.mu.RUnlock()
	keys := make([]string, 0, len(s.records))
	for k := range s.records {
		keys = append(keys, k)
	}
	return &proto.KeysResponse{Keys: keys}, nil
}

func (p *Provider) Count(_ context.Context, req *proto.ObjectStoreRangeRequest) (*proto.CountResponse, error) {
	s := p.getStore(req.GetStore())
	p.mu.RLock()
	defer p.mu.RUnlock()
	return &proto.CountResponse{Count: int64(len(s.records))}, nil
}

func (p *Provider) DeleteRange(_ context.Context, _ *proto.ObjectStoreRangeRequest) (*proto.DeleteResponse, error) {
	return &proto.DeleteResponse{Deleted: 0}, nil
}

func (p *Provider) IndexGet(_ context.Context, req *proto.IndexQueryRequest) (*proto.RecordResponse, error) {
	s := p.getStore(req.GetStore())
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, rec := range s.records {
		if indexMatches(rec, s.schema, req.GetIndex(), req.GetValues()) {
			return &proto.RecordResponse{Record: rec}, nil
		}
	}
	return nil, status.Error(codes.NotFound, "not found")
}

func (p *Provider) IndexGetKey(ctx context.Context, req *proto.IndexQueryRequest) (*proto.KeyResponse, error) {
	resp, err := p.IndexGet(ctx, req)
	if err != nil {
		return nil, err
	}
	return &proto.KeyResponse{Key: fieldString(resp.GetRecord(), "id")}, nil
}

func (p *Provider) IndexGetAll(_ context.Context, req *proto.IndexQueryRequest) (*proto.RecordsResponse, error) {
	s := p.getStore(req.GetStore())
	p.mu.RLock()
	defer p.mu.RUnlock()
	var recs []*structpb.Struct
	for _, rec := range s.records {
		if indexMatches(rec, s.schema, req.GetIndex(), req.GetValues()) {
			recs = append(recs, rec)
		}
	}
	return &proto.RecordsResponse{Records: recs}, nil
}

func (p *Provider) IndexGetAllKeys(ctx context.Context, req *proto.IndexQueryRequest) (*proto.KeysResponse, error) {
	resp, err := p.IndexGetAll(ctx, req)
	if err != nil {
		return nil, err
	}
	keys := make([]string, len(resp.GetRecords()))
	for i, rec := range resp.GetRecords() {
		keys[i] = fieldString(rec, "id")
	}
	return &proto.KeysResponse{Keys: keys}, nil
}

func (p *Provider) IndexCount(ctx context.Context, req *proto.IndexQueryRequest) (*proto.CountResponse, error) {
	resp, err := p.IndexGetAll(ctx, req)
	if err != nil {
		return nil, err
	}
	return &proto.CountResponse{Count: int64(len(resp.GetRecords()))}, nil
}

func (p *Provider) IndexDelete(_ context.Context, req *proto.IndexQueryRequest) (*proto.DeleteResponse, error) {
	s := p.getStore(req.GetStore())
	p.mu.Lock()
	defer p.mu.Unlock()
	var toDelete []string
	for id, rec := range s.records {
		if indexMatches(rec, s.schema, req.GetIndex(), req.GetValues()) {
			toDelete = append(toDelete, id)
		}
	}
	for _, id := range toDelete {
		delete(s.records, id)
	}
	return &proto.DeleteResponse{Deleted: int64(len(toDelete))}, nil
}

func fieldString(rec *structpb.Struct, key string) string {
	if rec == nil {
		return ""
	}
	if v, ok := rec.GetFields()[key]; ok {
		return v.GetStringValue()
	}
	return ""
}

func indexMatches(rec *structpb.Struct, schema *proto.ObjectStoreSchema, indexName string, values []*structpb.Value) bool {
	if schema == nil || rec == nil {
		return false
	}
	var keyPath []string
	for _, idx := range schema.GetIndexes() {
		if idx.GetName() == indexName {
			keyPath = idx.GetKeyPath()
			break
		}
	}
	if keyPath == nil {
		return false
	}
	fields := rec.GetFields()
	for i, field := range keyPath {
		if i >= len(values) {
			break
		}
		rv, ok := fields[field]
		if !ok {
			return false
		}
		if rv.GetStringValue() != values[i].GetStringValue() {
			return false
		}
	}
	return true
}

func fieldsMatch(a, b *structpb.Struct, keyPath []string) bool {
	af, bf := a.GetFields(), b.GetFields()
	for _, field := range keyPath {
		av, aok := af[field]
		bv, bok := bf[field]
		if !aok || !bok {
			return false
		}
		if av.GetStringValue() != bv.GetStringValue() {
			return false
		}
	}
	return true
}
