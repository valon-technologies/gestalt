package cache

import (
	"context"
	"testing"
	"time"

	sdkcache "github.com/valon-technologies/gestalt/sdk/go/cache"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

type cacheStub struct {
	proto.CacheClient

	values   map[string][]byte
	setTTL   time.Duration
	touchTTL time.Duration
	getCtx   context.Context
}

func newCacheStub() *cacheStub {
	return &cacheStub{values: map[string][]byte{}}
}

func (s *cacheStub) Get(ctx context.Context, req *proto.CacheGetRequest, _ ...grpc.CallOption) (*proto.CacheGetResponse, error) {
	s.getCtx = ctx
	value, ok := s.values[req.GetKey()]
	return &proto.CacheGetResponse{Found: ok, Value: value}, nil
}

func (s *cacheStub) GetMany(context.Context, *proto.CacheGetManyRequest, ...grpc.CallOption) (*proto.CacheGetManyResponse, error) {
	return &proto.CacheGetManyResponse{Entries: []*proto.CacheResult{
		{Key: "alpha", Found: true, Value: s.values["alpha"]},
		{Key: "missing"},
	}}, nil
}

func (s *cacheStub) Set(_ context.Context, req *proto.CacheSetRequest, _ ...grpc.CallOption) (*emptypb.Empty, error) {
	s.values[req.GetKey()] = req.GetValue()
	if req.GetTtl() != nil {
		s.setTTL = req.GetTtl().AsDuration()
	}
	return &emptypb.Empty{}, nil
}

func (s *cacheStub) SetMany(_ context.Context, req *proto.CacheSetManyRequest, _ ...grpc.CallOption) (*emptypb.Empty, error) {
	for _, entry := range req.GetEntries() {
		s.values[entry.GetKey()] = entry.GetValue()
	}
	return &emptypb.Empty{}, nil
}

func (s *cacheStub) Delete(_ context.Context, req *proto.CacheDeleteRequest, _ ...grpc.CallOption) (*proto.CacheDeleteResponse, error) {
	_, ok := s.values[req.GetKey()]
	delete(s.values, req.GetKey())
	return &proto.CacheDeleteResponse{Deleted: ok}, nil
}

func (s *cacheStub) DeleteMany(_ context.Context, req *proto.CacheDeleteManyRequest, _ ...grpc.CallOption) (*proto.CacheDeleteManyResponse, error) {
	var deleted int64
	for _, key := range req.GetKeys() {
		if _, ok := s.values[key]; ok {
			deleted++
			delete(s.values, key)
		}
	}
	return &proto.CacheDeleteManyResponse{Deleted: deleted}, nil
}

func (s *cacheStub) Touch(_ context.Context, req *proto.CacheTouchRequest, _ ...grpc.CallOption) (*proto.CacheTouchResponse, error) {
	if req.GetTtl() != nil {
		s.touchTTL = req.GetTtl().AsDuration()
	}
	_, ok := s.values[req.GetKey()]
	return &proto.CacheTouchResponse{Touched: ok}, nil
}

func TestClientRoundTripAndCopiesValues(t *testing.T) {
	t.Parallel()

	stub := newCacheStub()
	client := NewClient(stub, Options{})

	value := []byte("one")
	if err := client.Set(context.Background(), "alpha", value, sdkcache.SetOptions{TTL: time.Minute}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	value[0] = 'x'
	if stub.setTTL != time.Minute {
		t.Fatalf("set ttl = %s, want %s", stub.setTTL, time.Minute)
	}

	got, found, err := client.Get(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !found || string(got) != "one" {
		t.Fatalf("Get = (%q, %v), want (%q, true)", got, found, "one")
	}
	got[0] = 'x'
	got, found, err = client.Get(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("Get after mutate: %v", err)
	}
	if !found || string(got) != "one" {
		t.Fatalf("Get after mutate = (%q, %v), want (%q, true)", got, found, "one")
	}

	if err := client.SetMany(context.Background(), []sdkcache.Entry{{Key: "beta", Value: []byte("two")}}, sdkcache.SetOptions{}); err != nil {
		t.Fatalf("SetMany: %v", err)
	}
	many, err := client.GetMany(context.Background(), []string{"alpha", "missing"})
	if err != nil {
		t.Fatalf("GetMany: %v", err)
	}
	if string(many["alpha"]) != "one" {
		t.Fatalf(`GetMany["alpha"] = %q, want one`, many["alpha"])
	}
	if _, ok := many["missing"]; ok {
		t.Fatal(`GetMany["missing"] should be absent`)
	}

	touched, err := client.Touch(context.Background(), "alpha", 2*time.Minute)
	if err != nil {
		t.Fatalf("Touch: %v", err)
	}
	if !touched || stub.touchTTL != 2*time.Minute {
		t.Fatalf("Touch = (%v, %s), want (true, %s)", touched, stub.touchTTL, 2*time.Minute)
	}

	deleted, err := client.Delete(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !deleted {
		t.Fatal("Delete returned false, want true")
	}
	deletedCount, err := client.DeleteMany(context.Background(), []string{"beta", "missing"})
	if err != nil {
		t.Fatalf("DeleteMany: %v", err)
	}
	if deletedCount != 1 {
		t.Fatalf("DeleteMany = %d, want 1", deletedCount)
	}
}

func TestClientUsesUnaryTimeout(t *testing.T) {
	t.Parallel()

	const timeout = 30 * time.Second
	stub := newCacheStub()
	client := NewClient(stub, Options{UnaryTimeout: timeout})
	if _, _, err := client.Get(context.Background(), "alpha"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	deadline, ok := stub.getCtx.Deadline()
	if !ok {
		t.Fatal("Get context has no deadline")
	}
	remaining := time.Until(deadline)
	if remaining <= timeout-2*time.Second || remaining > timeout {
		t.Fatalf("deadline remaining = %s, want within 2s of %s", remaining, timeout)
	}
}
