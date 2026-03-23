package pluginproc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *rpcError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message == "" {
		return fmt.Sprintf("plugin rpc error %d", e.Code)
	}
	return e.Message
}

type requestMessage struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type responseMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type envelope struct {
	ID     *int64           `json:"id,omitempty"`
	Method string           `json:"method,omitempty"`
	Result json.RawMessage  `json:"result,omitempty"`
	Error  *rpcError        `json:"error,omitempty"`
	Params *json.RawMessage `json:"params,omitempty"`
}

type Client struct {
	codec *framedCodec

	nextID atomic.Int64

	pendingMu sync.Mutex
	pending   map[int64]chan responseMessage

	closeOnce sync.Once
	done      chan struct{}
	err       error
}

func newClient(r io.Reader, w io.Writer) *Client {
	c := &Client{
		codec:   newFramedCodec(r, w),
		pending: make(map[int64]chan responseMessage),
		done:    make(chan struct{}),
	}
	go c.readLoop()
	return c
}

func (c *Client) Call(ctx context.Context, method string, params any, out any) error {
	id := c.nextID.Add(1)
	respCh := make(chan responseMessage, 1)

	c.pendingMu.Lock()
	c.pending[id] = respCh
	c.pendingMu.Unlock()

	req := requestMessage{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
	if err := c.codec.writeMessage(req); err != nil {
		c.unregisterPending(id)
		return err
	}

	select {
	case resp := <-respCh:
		if resp.Error != nil {
			return resp.Error
		}
		if out == nil || len(resp.Result) == 0 {
			return nil
		}
		if err := json.Unmarshal(resp.Result, out); err != nil {
			return fmt.Errorf("decode rpc result for %s: %w", method, err)
		}
		return nil
	case <-ctx.Done():
		_ = c.Notify(context.Background(), methodCancelRequest, cancelParams{ID: id})
		c.unregisterPending(id)
		return ctx.Err()
	case <-c.done:
		c.unregisterPending(id)
		if c.err != nil {
			return c.err
		}
		return io.EOF
	}
}

func (c *Client) Notify(_ context.Context, method string, params any) error {
	req := requestMessage{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	return c.codec.writeMessage(req)
}

func (c *Client) readLoop() {
	for {
		raw, err := c.codec.readMessage()
		if err != nil {
			c.stop(err)
			return
		}

		var env envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			c.stop(fmt.Errorf("decode plugin response: %w", err))
			return
		}

		if env.ID == nil {
			continue
		}

		resp := responseMessage{
			JSONRPC: "2.0",
			ID:      *env.ID,
			Result:  env.Result,
			Error:   env.Error,
		}
		c.resolvePending(resp)
	}
}

func (c *Client) resolvePending(resp responseMessage) {
	c.pendingMu.Lock()
	ch, ok := c.pending[resp.ID]
	if ok {
		delete(c.pending, resp.ID)
	}
	c.pendingMu.Unlock()

	if ok {
		ch <- resp
		close(ch)
	}
}

func (c *Client) unregisterPending(id int64) {
	c.pendingMu.Lock()
	if ch, ok := c.pending[id]; ok {
		delete(c.pending, id)
		close(ch)
	}
	c.pendingMu.Unlock()
}

func (c *Client) stop(err error) {
	c.closeOnce.Do(func() {
		if err == nil {
			err = io.EOF
		}
		c.err = err

		c.pendingMu.Lock()
		for id, ch := range c.pending {
			delete(c.pending, id)
			ch <- responseMessage{Error: &rpcError{Code: -1, Message: err.Error()}}
			close(ch)
		}
		c.pendingMu.Unlock()

		close(c.done)
	})
}

func (c *Client) Wait() error {
	<-c.done
	if c.err != nil && !errors.Is(c.err, io.EOF) {
		return c.err
	}
	return nil
}
