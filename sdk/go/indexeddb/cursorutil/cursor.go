package cursorutil

import (
	"bytes"
	"context"
	"fmt"
	"math/big"
	"sort"
	"time"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	gproto "google.golang.org/protobuf/proto"
)

type Entry struct {
	Key             any
	PrimaryKey      string
	PrimaryKeyValue any
	Record          *proto.Record
}

type Snapshot struct {
	IndexCursor bool
	KeysOnly    bool
	Reverse     bool
	Unique      bool
	Entries     []Entry
	Pos         int
}

type Runtime interface {
	SnapshotState() *Snapshot
	DeleteCurrent(context.Context) error
	UpdateCurrent(context.Context, *proto.Record) (*proto.CursorEntry, error)
}

func NewSnapshot(req *proto.OpenCursorRequest) Snapshot {
	return Snapshot{
		IndexCursor: req.GetIndex() != "",
		KeysOnly:    req.GetKeysOnly(),
		Reverse: req.GetDirection() == proto.CursorDirection_CURSOR_PREV ||
			req.GetDirection() == proto.CursorDirection_CURSOR_PREV_UNIQUE,
		Unique: req.GetDirection() == proto.CursorDirection_CURSOR_NEXT_UNIQUE ||
			req.GetDirection() == proto.CursorDirection_CURSOR_PREV_UNIQUE,
		Pos: -1,
	}
}

func EntriesFromRecords(records []*proto.Record, build func(*proto.Record) (Entry, error), skip func(error) bool) ([]Entry, error) {
	entries := make([]Entry, 0, len(records))
	for _, record := range records {
		entry, err := build(record)
		if err != nil {
			if skip != nil && skip(err) {
				continue
			}
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func (s *Snapshot) Load(entries []Entry, kr *proto.KeyRange) error {
	sort.Slice(entries, func(i, j int) bool {
		cmp := CompareValues(entries[i].Key, entries[j].Key)
		if cmp == 0 {
			cmp = CompareValues(entries[i].PrimaryKeyValue, entries[j].PrimaryKeyValue)
		}
		if s.Reverse {
			return cmp > 0
		}
		return cmp < 0
	})

	filtered, err := s.ApplyRange(entries, kr)
	if err != nil {
		return err
	}
	s.Entries = filtered
	return nil
}

func (s *Snapshot) ApplyRange(entries []Entry, kr *proto.KeyRange) ([]Entry, error) {
	if kr == nil {
		return entries, nil
	}

	lower, upper, err := RangeBounds(kr, s.IndexCursor)
	if err != nil {
		return nil, err
	}

	filtered := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		if lower != nil {
			cmp := CompareValues(entry.Key, lower)
			if kr.GetLowerOpen() && cmp <= 0 {
				continue
			}
			if !kr.GetLowerOpen() && cmp < 0 {
				continue
			}
		}
		if upper != nil {
			cmp := CompareValues(entry.Key, upper)
			if kr.GetUpperOpen() && cmp >= 0 {
				continue
			}
			if !kr.GetUpperOpen() && cmp > 0 {
				continue
			}
		}
		filtered = append(filtered, entry)
	}
	return filtered, nil
}

func (s *Snapshot) ContinueNext() (*proto.CursorEntry, bool, error) {
	if s.Unique && s.IndexCursor && s.Pos >= 0 && s.Pos < len(s.Entries) {
		prev := s.Entries[s.Pos].Key
		for s.Pos++; s.Pos < len(s.Entries); s.Pos++ {
			if CompareValues(s.Entries[s.Pos].Key, prev) != 0 {
				entry, err := s.CurrentEntry()
				return entry, err == nil, err
			}
		}
		return nil, false, nil
	}

	s.Pos++
	if s.Pos >= len(s.Entries) {
		return nil, false, nil
	}
	entry, err := s.CurrentEntry()
	return entry, err == nil, err
}

func (s *Snapshot) ContinueToKey(target any) (*proto.CursorEntry, bool, error) {
	var prev any
	if s.Unique && s.IndexCursor && s.Pos >= 0 && s.Pos < len(s.Entries) {
		prev = s.Entries[s.Pos].Key
	}
	for s.Pos++; s.Pos < len(s.Entries); s.Pos++ {
		cur := s.Entries[s.Pos].Key
		if prev != nil && s.Unique && s.IndexCursor && CompareValues(cur, prev) == 0 {
			continue
		}
		cmp := CompareValues(cur, target)
		if s.Reverse {
			if cmp <= 0 {
				entry, err := s.CurrentEntry()
				return entry, err == nil, err
			}
			continue
		}
		if cmp >= 0 {
			entry, err := s.CurrentEntry()
			return entry, err == nil, err
		}
	}
	return nil, false, nil
}

func (s *Snapshot) Advance(count int) (*proto.CursorEntry, bool, error) {
	if count <= 0 {
		return nil, false, status.Error(codes.InvalidArgument, "advance count must be positive")
	}
	for i := 0; i <= count; i++ {
		entry, ok, err := s.ContinueNext()
		if !ok || err != nil {
			return entry, ok, err
		}
		if i == count {
			return entry, true, nil
		}
	}
	return nil, false, nil
}

func (s *Snapshot) Current() (*Entry, error) {
	if s.Pos < 0 || s.Pos >= len(s.Entries) {
		return nil, status.Error(codes.NotFound, "cursor is exhausted")
	}
	return &s.Entries[s.Pos], nil
}

func (s *Snapshot) CurrentEntry() (*proto.CursorEntry, error) {
	entry, err := s.Current()
	if err != nil {
		return nil, err
	}
	out := &proto.CursorEntry{PrimaryKey: entry.PrimaryKey}
	switch key := entry.Key.(type) {
	case []any:
		out.Key = make([]*proto.KeyValue, len(key))
		for i, part := range key {
			kv, err := gestalt.AnyToKeyValue(part)
			if err != nil {
				return nil, status.Errorf(codes.InvalidArgument, "marshal cursor key[%d]: %v", i, err)
			}
			out.Key[i] = kv
		}
	default:
		kv, err := gestalt.AnyToKeyValue(key)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "marshal cursor key: %v", err)
		}
		out.Key = []*proto.KeyValue{kv}
	}

	if !s.KeysOnly {
		out.Record = entry.Record
	}
	return out, nil
}

func Serve(stream grpc.BidiStreamingServer[proto.CursorClientMessage, proto.CursorResponse], open func(context.Context, *proto.OpenCursorRequest) (Runtime, error)) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	openReq := first.GetOpen()
	if openReq == nil {
		return status.Error(codes.InvalidArgument, "first message must be OpenCursorRequest")
	}

	cursor, err := open(stream.Context(), openReq)
	if err != nil {
		return err
	}
	state := cursor.SnapshotState()

	if err := stream.Send(doneResponse(false)); err != nil {
		return err
	}

	for {
		msg, err := stream.Recv()
		if err != nil {
			return err
		}
		cmd := msg.GetCommand()
		if cmd == nil {
			return status.Error(codes.InvalidArgument, "expected CursorCommand after open")
		}

		switch v := cmd.GetCommand().(type) {
		case *proto.CursorCommand_Next:
			entry, ok, err := state.ContinueNext()
			if err := sendSnapshotResult(stream, entry, ok, err); err != nil {
				return err
			}

		case *proto.CursorCommand_ContinueToKey:
			target, err := TargetToAny(v.ContinueToKey.GetKey(), state.IndexCursor)
			if err != nil {
				return err
			}
			entry, ok, err := state.ContinueToKey(target)
			if err := sendSnapshotResult(stream, entry, ok, err); err != nil {
				return err
			}

		case *proto.CursorCommand_Advance:
			entry, ok, err := state.Advance(int(v.Advance))
			if err := sendSnapshotResult(stream, entry, ok, err); err != nil {
				return err
			}

		case *proto.CursorCommand_Delete:
			if err := cursor.DeleteCurrent(stream.Context()); err != nil {
				return err
			}
			if err := stream.Send(doneResponse(false)); err != nil {
				return err
			}

		case *proto.CursorCommand_Update:
			entry, err := cursor.UpdateCurrent(stream.Context(), v.Update)
			if err != nil {
				return err
			}
			if err := stream.Send(entryResponse(entry)); err != nil {
				return err
			}

		case *proto.CursorCommand_Close:
			return nil

		default:
			return status.Error(codes.InvalidArgument, "unknown cursor command")
		}
	}
}

func sendSnapshotResult(stream grpc.BidiStreamingServer[proto.CursorClientMessage, proto.CursorResponse], entry *proto.CursorEntry, ok bool, err error) error {
	if err != nil {
		return err
	}
	if !ok {
		return stream.Send(doneResponse(true))
	}
	return stream.Send(entryResponse(entry))
}

func CloneRecordWithField(record *proto.Record, field string, value any) (*proto.Record, error) {
	if record == nil {
		return nil, status.Error(codes.InvalidArgument, "update record is required")
	}

	cloned := gproto.Clone(record).(*proto.Record)
	if cloned.Fields == nil {
		cloned.Fields = map[string]*proto.TypedValue{}
	}
	keyValue, err := gestalt.TypedValueFromAny(value)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "marshal primary key: %v", err)
	}
	cloned.Fields[field] = keyValue
	return cloned, nil
}

func DirectRecordField(record *proto.Record, field string) (any, error) {
	if record == nil {
		return nil, fmt.Errorf("record is required")
	}
	value, ok := record.Fields[field]
	if !ok {
		return nil, fmt.Errorf("field %q not found", field)
	}
	return gestalt.AnyFromTypedValue(value)
}

func TargetToAny(kvs []*proto.KeyValue, indexCursor bool) (any, error) {
	if len(kvs) == 0 {
		return nil, status.Error(codes.InvalidArgument, "continue key is required")
	}
	parts, err := gestalt.KeyValuesToAny(kvs)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unmarshal continue key: %v", err)
	}
	if indexCursor {
		return parts, nil
	}
	if len(parts) == 1 {
		return parts[0], nil
	}
	return parts, nil
}

func RangeBounds(kr *proto.KeyRange, indexCursor bool) (any, any, error) {
	var lower any
	if kr.GetLower() != nil {
		value, err := gestalt.AnyFromTypedValue(kr.GetLower())
		if err != nil {
			return nil, nil, status.Errorf(codes.InvalidArgument, "key range lower: %v", err)
		}
		if value != nil {
			lower = normalizeBound(value, indexCursor)
		}
	}

	var upper any
	if kr.GetUpper() != nil {
		value, err := gestalt.AnyFromTypedValue(kr.GetUpper())
		if err != nil {
			return nil, nil, status.Errorf(codes.InvalidArgument, "key range upper: %v", err)
		}
		if value != nil {
			upper = normalizeBound(value, indexCursor)
		}
	}
	return lower, upper, nil
}

func normalizeBound(value any, indexCursor bool) any {
	if !indexCursor {
		return value
	}
	if parts, ok := value.([]any); ok {
		return parts
	}
	return []any{value}
}

func CompareValues(a, b any) int {
	switch av := a.(type) {
	case []any:
		if bv, ok := b.([]any); ok {
			for i := range av {
				if i >= len(bv) {
					return 1
				}
				if cmp := CompareValues(av[i], bv[i]); cmp != 0 {
					return cmp
				}
			}
			if len(av) < len(bv) {
				return -1
			}
			return 0
		}
	case string:
		if bv, ok := b.(string); ok {
			switch {
			case av < bv:
				return -1
			case av > bv:
				return 1
			default:
				return 0
			}
		}
	case time.Time:
		if bv, ok := b.(time.Time); ok {
			switch {
			case av.Before(bv):
				return -1
			case av.After(bv):
				return 1
			default:
				return 0
			}
		}
	case []byte:
		if bv, ok := b.([]byte); ok {
			return bytes.Compare(av, bv)
		}
	case bool:
		if bv, ok := b.(bool); ok {
			switch {
			case !av && bv:
				return -1
			case av && !bv:
				return 1
			default:
				return 0
			}
		}
	}

	if af, ok := cursorNumber(a); ok {
		if bf, ok := cursorNumber(b); ok {
			return af.Cmp(bf)
		}
	}

	as := fmt.Sprint(a)
	bs := fmt.Sprint(b)
	switch {
	case as < bs:
		return -1
	case as > bs:
		return 1
	default:
		return 0
	}
}

func entryResponse(entry *proto.CursorEntry) *proto.CursorResponse {
	return &proto.CursorResponse{Result: &proto.CursorResponse_Entry{Entry: entry}}
}

func doneResponse(done bool) *proto.CursorResponse {
	return &proto.CursorResponse{Result: &proto.CursorResponse_Done{Done: done}}
}

func cursorNumber(v any) (*big.Rat, bool) {
	switch n := v.(type) {
	case int:
		return big.NewRat(int64(n), 1), true
	case int8:
		return big.NewRat(int64(n), 1), true
	case int16:
		return big.NewRat(int64(n), 1), true
	case int32:
		return big.NewRat(int64(n), 1), true
	case int64:
		return big.NewRat(n, 1), true
	case float32:
		return cursorFloatRat(float64(n))
	case float64:
		return cursorFloatRat(n)
	default:
		return nil, false
	}
}

func cursorFloatRat(v float64) (*big.Rat, bool) {
	r := new(big.Rat).SetFloat64(v)
	if r == nil {
		return nil, false
	}
	return r, true
}
