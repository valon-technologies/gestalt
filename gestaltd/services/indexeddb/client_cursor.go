package indexeddb

import (
	"context"
	"fmt"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	proto "github.com/valon-technologies/gestalt/server/internal/gen/v1"
)

func openRemoteCursor(ctx context.Context, client proto.IndexedDBClient, store, index string, r *idb.KeyRange, dir idb.CursorDirection, keysOnly bool, values []any) (*remoteCursor, error) {
	kr, err := keyRangeToProto(r)
	if err != nil {
		return nil, err
	}
	var pbValues []*proto.TypedValue
	if len(values) > 0 {
		pbValues, err = toProtoValues(values)
		if err != nil {
			return nil, err
		}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	streamCtx, streamCancel := context.WithCancel(ctx)
	stream, err := client.OpenCursor(streamCtx)
	if err != nil {
		streamCancel()
		return nil, idb.RPCError(err)
	}
	if err := stream.Send(&proto.CursorClientMessage{
		Msg: &proto.CursorClientMessage_Open{Open: &proto.OpenCursorRequest{
			Store:     store,
			Range:     kr,
			Direction: cursorDirectionToProto(dir),
			KeysOnly:  keysOnly,
			Index:     index,
			Values:    pbValues,
		}},
	}); err != nil {
		_ = stream.CloseSend()
		streamCancel()
		return nil, idb.RPCError(err)
	}
	// Read the open ack to surface creation errors synchronously.
	resp, err := stream.Recv()
	if err != nil {
		_ = stream.CloseSend()
		streamCancel()
		return nil, idb.RPCError(err)
	}
	if resp == nil {
		_ = stream.CloseSend()
		streamCancel()
		return nil, fmt.Errorf("cursor stream ended during open")
	}
	done, ok := resp.GetResult().(*proto.CursorResponse_Done)
	if !ok || done.Done {
		_ = stream.CloseSend()
		streamCancel()
		return nil, fmt.Errorf("unexpected cursor open ack")
	}
	return &remoteCursor{stream: stream, cancel: streamCancel, keysOnly: keysOnly, indexCursor: index != ""}, nil
}

type remoteCursor struct {
	stream      proto.IndexedDB_OpenCursorClient
	cancel      context.CancelFunc
	keysOnly    bool
	indexCursor bool
	entry       *proto.CursorEntry
	err         error
	done        bool
}

func (c *remoteCursor) cleanup() {
	if c.stream != nil {
		_ = c.stream.CloseSend()
		c.stream = nil
	}
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
}

func (c *remoteCursor) setErr(err error) error {
	c.err = err
	c.cleanup()
	return c.err
}

func (c *remoteCursor) sendAndRecv(msg *proto.CursorClientMessage) bool {
	if c.done || c.err != nil {
		return false
	}
	if err := c.stream.Send(msg); err != nil {
		c.err = idb.RPCError(err)
		c.cleanup()
		return false
	}
	resp, err := c.stream.Recv()
	if err != nil {
		c.err = idb.RPCError(err)
		c.cleanup()
		return false
	}
	if resp == nil {
		c.err = fmt.Errorf("cursor stream ended")
		c.cleanup()
		return false
	}
	switch v := resp.GetResult().(type) {
	case *proto.CursorResponse_Entry:
		c.entry = v.Entry
		return true
	case *proto.CursorResponse_Done:
		if !v.Done {
			_ = c.setErr(fmt.Errorf("unexpected non-exhaustion cursor ack"))
			return false
		}
		c.done = true
		c.entry = nil
		return false
	default:
		_ = c.setErr(fmt.Errorf("unexpected cursor response"))
		return false
	}
}

func (c *remoteCursor) Continue() bool {
	return c.sendAndRecv(&proto.CursorClientMessage{
		Msg: &proto.CursorClientMessage_Command{Command: &proto.CursorCommand{
			Command: &proto.CursorCommand_Next{Next: true},
		}},
	})
}

func (c *remoteCursor) ContinueToKey(key any) bool {
	kvs, err := cursorKeyToProto(key, c.indexCursor)
	if err != nil {
		c.err = err
		return false
	}
	return c.sendAndRecv(&proto.CursorClientMessage{
		Msg: &proto.CursorClientMessage_Command{Command: &proto.CursorCommand{
			Command: &proto.CursorCommand_ContinueToKey{ContinueToKey: &proto.CursorKeyTarget{Key: kvs}},
		}},
	})
}

func (c *remoteCursor) Advance(count int) bool {
	return c.sendAndRecv(&proto.CursorClientMessage{
		Msg: &proto.CursorClientMessage_Command{Command: &proto.CursorCommand{
			Command: &proto.CursorCommand_Advance{Advance: int32(count)},
		}},
	})
}

func (c *remoteCursor) Key() any {
	if c.entry == nil || len(c.entry.Key) == 0 {
		return nil
	}
	parts, err := keyValuesToAny(c.entry.Key)
	if err != nil {
		c.err = err
		return nil
	}
	if !c.indexCursor && len(parts) == 1 {
		return parts[0]
	}
	return parts
}

func (c *remoteCursor) PrimaryKey() string {
	if c.entry == nil {
		return ""
	}
	return c.entry.PrimaryKey
}

func (c *remoteCursor) Value() (idb.Record, error) {
	if c.keysOnly {
		return nil, idb.ErrKeysOnly
	}
	if c.entry == nil || c.entry.Record == nil {
		return nil, idb.ErrNotFound
	}
	return recordFromProto(c.entry.Record)
}

func (c *remoteCursor) Delete() error {
	if c.err != nil {
		return c.err
	}
	if c.done {
		return idb.ErrNotFound
	}
	if err := c.stream.Send(&proto.CursorClientMessage{
		Msg: &proto.CursorClientMessage_Command{Command: &proto.CursorCommand{
			Command: &proto.CursorCommand_Delete{Delete: true},
		}},
	}); err != nil {
		return c.setErr(idb.RPCError(err))
	}
	resp, err := c.stream.Recv()
	if err != nil {
		return c.setErr(idb.RPCError(err))
	}
	if resp == nil {
		return c.setErr(fmt.Errorf("cursor stream ended during mutation"))
	}
	switch v := resp.GetResult().(type) {
	case *proto.CursorResponse_Entry:
		c.entry = v.Entry
	case *proto.CursorResponse_Done:
		if v.Done {
			c.done = true
			c.entry = nil
		}
	default:
		return c.setErr(fmt.Errorf("unexpected cursor mutation ack"))
	}
	return nil
}

func (c *remoteCursor) Update(value idb.Record) error {
	if c.err != nil {
		return c.err
	}
	if c.done {
		return idb.ErrNotFound
	}
	pbRec, err := recordToProto(value)
	if err != nil {
		return fmt.Errorf("marshal update record: %w", err)
	}
	if err := c.stream.Send(&proto.CursorClientMessage{
		Msg: &proto.CursorClientMessage_Command{Command: &proto.CursorCommand{
			Command: &proto.CursorCommand_Update{Update: pbRec},
		}},
	}); err != nil {
		return c.setErr(idb.RPCError(err))
	}
	resp, err := c.stream.Recv()
	if err != nil {
		return c.setErr(idb.RPCError(err))
	}
	if resp == nil {
		return c.setErr(fmt.Errorf("cursor stream ended during mutation"))
	}
	switch v := resp.GetResult().(type) {
	case *proto.CursorResponse_Entry:
		c.entry = v.Entry
	case *proto.CursorResponse_Done:
		if v.Done {
			c.done = true
			c.entry = nil
		} else if c.entry != nil {
			c.entry.Record = pbRec
		}
	default:
		return c.setErr(fmt.Errorf("unexpected cursor mutation ack"))
	}
	return nil
}

func (c *remoteCursor) Err() error { return c.err }

func (c *remoteCursor) Close() error {
	if c.stream == nil {
		return nil
	}
	c.done = true
	c.entry = nil
	_ = c.stream.Send(&proto.CursorClientMessage{
		Msg: &proto.CursorClientMessage_Command{Command: &proto.CursorCommand{
			Command: &proto.CursorCommand_Close{Close: true},
		}},
	})
	c.cleanup()
	return nil
}
