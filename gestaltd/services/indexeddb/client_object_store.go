package indexeddb

import (
	"context"
	"fmt"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	proto "github.com/valon-technologies/gestalt/server/internal/gen/v1"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
)

type remoteObjectStore struct {
	client proto.IndexedDBClient
	store  string
}

func (o *remoteObjectStore) Get(ctx context.Context, id string) (idb.Record, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	resp, err := o.client.Get(ctx, &proto.ObjectStoreRequest{Store: o.store, Id: id})
	if err != nil {
		return nil, idb.RPCError(err)
	}
	record, err := recordFromProto(resp.GetRecord())
	if err != nil {
		return nil, fmt.Errorf("unmarshal record: %w", err)
	}
	return record, nil
}

func (o *remoteObjectStore) GetKey(ctx context.Context, id string) (string, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	resp, err := o.client.GetKey(ctx, &proto.ObjectStoreRequest{Store: o.store, Id: id})
	if err != nil {
		return "", idb.RPCError(err)
	}
	return resp.GetKey(), nil
}

func (o *remoteObjectStore) Add(ctx context.Context, record idb.Record) error {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	pbRecord, err := recordToProto(record)
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}
	_, err = o.client.Add(ctx, &proto.RecordRequest{Store: o.store, Record: pbRecord})
	return idb.RPCError(err)
}

func (o *remoteObjectStore) Put(ctx context.Context, record idb.Record) error {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	pbRecord, err := recordToProto(record)
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}
	_, err = o.client.Put(ctx, &proto.RecordRequest{Store: o.store, Record: pbRecord})
	return idb.RPCError(err)
}

func (o *remoteObjectStore) Delete(ctx context.Context, id string) error {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	_, err := o.client.Delete(ctx, &proto.ObjectStoreRequest{Store: o.store, Id: id})
	return idb.RPCError(err)
}

func (o *remoteObjectStore) Clear(ctx context.Context) error {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	_, err := o.client.Clear(ctx, &proto.ObjectStoreNameRequest{Store: o.store})
	return idb.RPCError(err)
}

func (o *remoteObjectStore) GetAll(ctx context.Context, r *idb.KeyRange) ([]idb.Record, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	kr, err := keyRangeToProto(r)
	if err != nil {
		return nil, err
	}
	resp, err := o.client.GetAll(ctx, &proto.ObjectStoreRangeRequest{Store: o.store, Range: kr})
	if err != nil {
		return nil, idb.RPCError(err)
	}
	records, err := recordsFromProto(resp.GetRecords())
	if err != nil {
		return nil, fmt.Errorf("unmarshal records: %w", err)
	}
	return records, nil
}

func (o *remoteObjectStore) GetAllKeys(ctx context.Context, r *idb.KeyRange) ([]string, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	kr, err := keyRangeToProto(r)
	if err != nil {
		return nil, err
	}
	resp, err := o.client.GetAllKeys(ctx, &proto.ObjectStoreRangeRequest{Store: o.store, Range: kr})
	if err != nil {
		return nil, idb.RPCError(err)
	}
	return resp.GetKeys(), nil
}

func (o *remoteObjectStore) Count(ctx context.Context, r *idb.KeyRange) (int64, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	kr, err := keyRangeToProto(r)
	if err != nil {
		return 0, err
	}
	resp, err := o.client.Count(ctx, &proto.ObjectStoreRangeRequest{Store: o.store, Range: kr})
	if err != nil {
		return 0, idb.RPCError(err)
	}
	return resp.GetCount(), nil
}

func (o *remoteObjectStore) DeleteRange(ctx context.Context, r idb.KeyRange) (int64, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	kr, err := keyRangeToProto(&r)
	if err != nil {
		return 0, err
	}
	resp, err := o.client.DeleteRange(ctx, &proto.ObjectStoreRangeRequest{Store: o.store, Range: kr})
	if err != nil {
		return 0, idb.RPCError(err)
	}
	return resp.GetDeleted(), nil
}

func (o *remoteObjectStore) Index(name string) idb.Index {
	return &remoteIndex{client: o.client, store: o.store, index: name}
}

func (o *remoteObjectStore) OpenCursor(ctx context.Context, r *idb.KeyRange, dir idb.CursorDirection) (idb.Cursor, error) {
	return openRemoteCursor(ctx, o.client, o.store, "", r, dir, false, nil)
}

func (o *remoteObjectStore) OpenKeyCursor(ctx context.Context, r *idb.KeyRange, dir idb.CursorDirection) (idb.Cursor, error) {
	return openRemoteCursor(ctx, o.client, o.store, "", r, dir, true, nil)
}

// --- Index ---

type remoteIndex struct {
	client proto.IndexedDBClient
	store  string
	index  string
}

func (idx *remoteIndex) Get(ctx context.Context, values ...any) (idb.Record, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	pbValues, err := toProtoValues(values)
	if err != nil {
		return nil, err
	}
	resp, err := idx.client.IndexGet(ctx, &proto.IndexQueryRequest{
		Store: idx.store, Index: idx.index, Values: pbValues,
	})
	if err != nil {
		return nil, idb.RPCError(err)
	}
	record, err := recordFromProto(resp.GetRecord())
	if err != nil {
		return nil, fmt.Errorf("unmarshal record: %w", err)
	}
	return record, nil
}

func (idx *remoteIndex) GetKey(ctx context.Context, values ...any) (string, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	pbValues, err := toProtoValues(values)
	if err != nil {
		return "", err
	}
	resp, err := idx.client.IndexGetKey(ctx, &proto.IndexQueryRequest{
		Store: idx.store, Index: idx.index, Values: pbValues,
	})
	if err != nil {
		return "", idb.RPCError(err)
	}
	return resp.GetKey(), nil
}

func (idx *remoteIndex) GetAll(ctx context.Context, r *idb.KeyRange, values ...any) ([]idb.Record, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	pbValues, err := toProtoValues(values)
	if err != nil {
		return nil, err
	}
	kr, err := keyRangeToProto(r)
	if err != nil {
		return nil, err
	}
	resp, err := idx.client.IndexGetAll(ctx, &proto.IndexQueryRequest{
		Store: idx.store, Index: idx.index, Values: pbValues, Range: kr,
	})
	if err != nil {
		return nil, idb.RPCError(err)
	}
	records, err := recordsFromProto(resp.GetRecords())
	if err != nil {
		return nil, fmt.Errorf("unmarshal records: %w", err)
	}
	return records, nil
}

func (idx *remoteIndex) GetAllKeys(ctx context.Context, r *idb.KeyRange, values ...any) ([]string, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	pbValues, err := toProtoValues(values)
	if err != nil {
		return nil, err
	}
	kr, err := keyRangeToProto(r)
	if err != nil {
		return nil, err
	}
	resp, err := idx.client.IndexGetAllKeys(ctx, &proto.IndexQueryRequest{
		Store: idx.store, Index: idx.index, Values: pbValues, Range: kr,
	})
	if err != nil {
		return nil, idb.RPCError(err)
	}
	return resp.GetKeys(), nil
}

func (idx *remoteIndex) Count(ctx context.Context, r *idb.KeyRange, values ...any) (int64, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	pbValues, err := toProtoValues(values)
	if err != nil {
		return 0, err
	}
	kr, err := keyRangeToProto(r)
	if err != nil {
		return 0, err
	}
	resp, err := idx.client.IndexCount(ctx, &proto.IndexQueryRequest{
		Store: idx.store, Index: idx.index, Values: pbValues, Range: kr,
	})
	if err != nil {
		return 0, idb.RPCError(err)
	}
	return resp.GetCount(), nil
}

func (idx *remoteIndex) OpenCursor(ctx context.Context, r *idb.KeyRange, dir idb.CursorDirection, values ...any) (idb.Cursor, error) {
	return openRemoteCursor(ctx, idx.client, idx.store, idx.index, r, dir, false, values)
}

func (idx *remoteIndex) OpenKeyCursor(ctx context.Context, r *idb.KeyRange, dir idb.CursorDirection, values ...any) (idb.Cursor, error) {
	return openRemoteCursor(ctx, idx.client, idx.store, idx.index, r, dir, true, values)
}

func (idx *remoteIndex) Delete(ctx context.Context, values ...any) (int64, error) {
	return idx.DeleteRange(ctx, nil, values...)
}

func (idx *remoteIndex) DeleteRange(ctx context.Context, r *idb.KeyRange, values ...any) (int64, error) {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	pbValues, err := toProtoValues(values)
	if err != nil {
		return 0, err
	}
	kr, err := keyRangeToProto(r)
	if err != nil {
		return 0, err
	}
	resp, err := idx.client.IndexDelete(ctx, &proto.IndexQueryRequest{
		Store: idx.store, Index: idx.index, Values: pbValues, Range: kr,
	})
	if err != nil {
		return 0, idb.RPCError(err)
	}
	return resp.GetDeleted(), nil
}

// --- Transaction ---
