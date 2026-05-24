package host

import (
	"context"
	"fmt"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	proto "github.com/valon-technologies/gestalt/sdk/go/protov1/v1"
)

type hostCursor struct {
	stream      proto.IndexedDB_OpenCursorClient
	cancel      context.CancelFunc
	keysOnly    bool
	indexCursor bool
	entry       *proto.CursorEntry
	err         error
	done        bool
}

// Continue advances the cursor by one row.
func (c *hostCursor) Continue() bool {
	return c.sendAndRecv(&proto.CursorCommand{
		Command: &proto.CursorCommand_Next{Next: true},
	})
}

// ContinueToKey advances the cursor to the supplied key, or exhausts it if the
// key does not exist.
func (c *hostCursor) ContinueToKey(key any) bool {
	kvs, err := cursorKeyToProto(key, c.indexCursor)
	if err != nil {
		c.err = err
		return false
	}
	return c.sendAndRecv(&proto.CursorCommand{
		Command: &proto.CursorCommand_ContinueToKey{ContinueToKey: &proto.CursorKeyTarget{Key: kvs}},
	})
}

// Advance skips count rows ahead.
func (c *hostCursor) Advance(count int) bool {
	return c.sendAndRecv(&proto.CursorCommand{
		Command: &proto.CursorCommand_Advance{Advance: int32(count)},
	})
}

// Key returns the current cursor key.
func (c *hostCursor) Key() any {
	if c.entry == nil || len(c.entry.GetKey()) == 0 {
		return nil
	}
	parts, err := keyValuesToAny(c.entry.GetKey())
	if err != nil {
		c.err = err
		return nil
	}
	if !c.indexCursor && len(parts) == 1 {
		return parts[0]
	}
	return parts
}

// PrimaryKey returns the current record's primary key.
func (c *hostCursor) PrimaryKey() string {
	if c.entry == nil {
		return ""
	}
	return c.entry.GetPrimaryKey()
}

// Value returns the current record.
func (c *hostCursor) Value() (idb.Record, error) {
	if c.keysOnly {
		return nil, idb.ErrKeysOnly
	}
	if c.entry == nil || c.entry.GetRecord() == nil {
		return nil, idb.ErrNotFound
	}
	return recordFromProto(c.entry.GetRecord())
}

// Delete removes the current row and keeps the cursor open.
func (c *hostCursor) Delete() error {
	if c.err != nil {
		return c.err
	}
	if c.done {
		return idb.ErrNotFound
	}
	err := c.stream.Send(&proto.CursorClientMessage{
		Msg: &proto.CursorClientMessage_Command{
			Command: &proto.CursorCommand{
				Command: &proto.CursorCommand_Delete{Delete: true},
			},
		},
	})
	if err != nil {
		return c.setErr(grpcErr(err))
	}
	resp, err := c.stream.Recv()
	if err != nil {
		return c.setErr(grpcErr(err))
	}
	if resp == nil {
		return c.setErr(fmt.Errorf("indexeddb: cursor stream ended during mutation"))
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
		return c.setErr(fmt.Errorf("indexeddb: unexpected cursor mutation ack"))
	}
	return nil
}

// Update replaces the current row and keeps the cursor open.
func (c *hostCursor) Update(value idb.Record) error {
	if c.err != nil {
		return c.err
	}
	if c.done {
		return idb.ErrNotFound
	}
	pbRecord, err := recordToProto(value)
	if err != nil {
		return fmt.Errorf("indexeddb: marshal cursor update: %w", err)
	}
	err = c.stream.Send(&proto.CursorClientMessage{
		Msg: &proto.CursorClientMessage_Command{
			Command: &proto.CursorCommand{
				Command: &proto.CursorCommand_Update{Update: pbRecord},
			},
		},
	})
	if err != nil {
		return c.setErr(grpcErr(err))
	}
	resp, err := c.stream.Recv()
	if err != nil {
		return c.setErr(grpcErr(err))
	}
	if resp == nil {
		return c.setErr(fmt.Errorf("indexeddb: cursor stream ended during mutation"))
	}
	switch v := resp.GetResult().(type) {
	case *proto.CursorResponse_Entry:
		c.entry = v.Entry
	case *proto.CursorResponse_Done:
		if v.Done {
			c.done = true
			c.entry = nil
		} else if c.entry != nil {
			c.entry.Record = pbRecord
		}
	default:
		return c.setErr(fmt.Errorf("indexeddb: unexpected cursor mutation ack"))
	}
	return nil
}

// Err returns the terminal cursor error, if any.
func (c *hostCursor) Err() error {
	return c.err
}

func (c *hostCursor) cleanup() error {
	var err error
	if c.stream != nil {
		err = grpcErr(c.stream.CloseSend())
		c.stream = nil
	}
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
	return err
}

func (c *hostCursor) setErr(err error) error {
	c.err = err
	_ = c.cleanup()
	return c.err
}

// Close closes the cursor stream and releases its transport resources.
func (c *hostCursor) Close() error {
	c.done = true
	c.entry = nil
	if c.stream == nil {
		return c.cleanup()
	}
	sendErr := c.stream.Send(&proto.CursorClientMessage{
		Msg: &proto.CursorClientMessage_Command{
			Command: &proto.CursorCommand{
				Command: &proto.CursorCommand_Close{Close: true},
			},
		},
	})
	closeErr := c.cleanup()
	if sendErr != nil {
		return grpcErr(sendErr)
	}
	return closeErr
}

func (c *hostCursor) sendAndRecv(cmd *proto.CursorCommand) bool {
	if c.done || c.err != nil {
		return false
	}
	err := c.stream.Send(&proto.CursorClientMessage{
		Msg: &proto.CursorClientMessage_Command{Command: cmd},
	})
	if err != nil {
		_ = c.setErr(grpcErr(err))
		return false
	}
	resp, err := c.stream.Recv()
	if err != nil {
		_ = c.setErr(grpcErr(err))
		return false
	}
	if resp == nil {
		_ = c.setErr(fmt.Errorf("indexeddb: cursor stream ended"))
		return false
	}
	switch v := resp.GetResult().(type) {
	case *proto.CursorResponse_Entry:
		c.entry = v.Entry
		return true
	case *proto.CursorResponse_Done:
		if !v.Done {
			_ = c.setErr(fmt.Errorf("indexeddb: unexpected non-exhaustion cursor ack"))
			c.entry = nil
			return false
		}
		c.done = true
		c.entry = nil
		return false
	default:
		_ = c.setErr(fmt.Errorf("indexeddb: unexpected cursor response"))
		c.entry = nil
		return false
	}
}

func cursorDirectionToProto(dir idb.CursorDirection) proto.CursorDirection {
	switch dir {
	case idb.CursorNextUnique:
		return proto.CursorDirection_CURSOR_NEXT_UNIQUE
	case idb.CursorPrev:
		return proto.CursorDirection_CURSOR_PREV
	case idb.CursorPrevUnique:
		return proto.CursorDirection_CURSOR_PREV_UNIQUE
	default:
		return proto.CursorDirection_CURSOR_NEXT
	}
}

func openCursor(ctx context.Context, client proto.IndexedDBClient, store, index string, r *idb.KeyRange, dir idb.CursorDirection, keysOnly bool, values []any) (idb.Cursor, error) {
	kr, err := krToProto(r)
	if err != nil {
		return nil, err
	}
	vals, err := typedValuesFromAny(values)
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	streamCtx, streamCancel := context.WithCancel(ctx)
	stream, err := client.OpenCursor(streamCtx)
	if err != nil {
		streamCancel()
		return nil, grpcErr(err)
	}
	err = stream.Send(&proto.CursorClientMessage{
		Msg: &proto.CursorClientMessage_Open{
			Open: &proto.OpenCursorRequest{
				Store:     store,
				Range:     kr,
				Direction: cursorDirectionToProto(dir),
				KeysOnly:  keysOnly,
				Index:     index,
				Values:    vals,
			},
		},
	})
	if err != nil {
		_ = stream.CloseSend()
		streamCancel()
		return nil, grpcErr(err)
	}
	// Read the open ack to surface creation errors synchronously.
	resp, err := stream.Recv()
	if err != nil {
		_ = stream.CloseSend()
		streamCancel()
		return nil, grpcErr(err)
	}
	if resp == nil {
		_ = stream.CloseSend()
		streamCancel()
		return nil, fmt.Errorf("indexeddb: cursor stream ended during open")
	}
	done, ok := resp.GetResult().(*proto.CursorResponse_Done)
	if !ok || done.Done {
		_ = stream.CloseSend()
		streamCancel()
		return nil, fmt.Errorf("indexeddb: unexpected cursor open ack")
	}
	return &hostCursor{stream: stream, cancel: streamCancel, keysOnly: keysOnly, indexCursor: index != ""}, nil
}
