package tunnel

import (
	"bufio"
	"net"
	"time"
)

// bufConn wraps a net.Conn whose read side is buffered by a *bufio.Reader. It
// preserves bytes that the bufio.Reader has already consumed from the wire but
// not yet delivered, so they are not lost when the connection is handed to a
// TLS client. Write, Close, and deadline operations delegate to the underlying
// connection.
type bufConn struct {
	net.Conn
	br *bufio.Reader
}

func (c *bufConn) Read(b []byte) (int, error)         { return c.br.Read(b) }
func (c *bufConn) Write(b []byte) (int, error)        { return c.Conn.Write(b) }
func (c *bufConn) Close() error                       { return c.Conn.Close() }
func (c *bufConn) SetDeadline(t time.Time) error      { return c.Conn.SetDeadline(t) }
func (c *bufConn) SetReadDeadline(t time.Time) error  { return c.Conn.SetReadDeadline(t) }
func (c *bufConn) SetWriteDeadline(t time.Time) error { return c.Conn.SetWriteDeadline(t) }
