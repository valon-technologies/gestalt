package tunnel

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

// sendConnectRequest issues an HTTP/1.1 CONNECT to the tunnel host and waits
// for a 200 response. It returns a *bufio.Reader wrapping the connection so
// any bytes the server sends after the response header are preserved. The
// deadline is derived from ctx (capped at 15s); it is cleared before returning
// so the caller's subsequent I/O is unbounded.
func sendConnectRequest(ctx context.Context, conn net.Conn, host string) (*bufio.Reader, error) {
	deadline := time.Now().Add(15 * time.Second)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, fmt.Errorf("set CONNECT deadline: %w", err)
	}
	defer func() { _ = conn.SetDeadline(time.Time{}) }()

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if _, err := fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", host, host); err != nil {
		return nil, fmt.Errorf("write CONNECT: %w", err)
	}
	br := bufio.NewReader(conn)
	if err := readConnectStatus(br); err != nil {
		return nil, err
	}
	return br, nil
}

func readConnectStatus(br *bufio.Reader) error {
	line, err := br.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read CONNECT status: %w", err)
	}
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "HTTP/1.1 200") && !strings.HasPrefix(line, "HTTP/1.0 200") {
		return fmt.Errorf("CONNECT failed: %s", line)
	}
	for {
		hdr, err := br.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("read CONNECT headers: %w", err)
		}
		if strings.TrimSpace(hdr) == "" {
			return nil
		}
	}
}
