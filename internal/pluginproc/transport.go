package pluginproc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
)

const (
	headerContentLength = "content-length"
	maxPayloadSize      = 64 << 20 // 64 MiB
)

type framedCodec struct {
	r       *bufio.Reader
	w       io.Writer
	writeMu sync.Mutex
}

func newFramedCodec(r io.Reader, w io.Writer) *framedCodec {
	return &framedCodec{
		r: bufio.NewReader(r),
		w: w,
	}
}

func (c *framedCodec) writeMessage(v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if _, err := fmt.Fprintf(c.w, "Content-Length: %d\r\n\r\n", len(payload)); err != nil {
		return fmt.Errorf("write header: %w", err)
	}
	if _, err := c.w.Write(payload); err != nil {
		return fmt.Errorf("write payload: %w", err)
	}
	return nil
}

func (c *framedCodec) readMessage() (json.RawMessage, error) {
	contentLength, err := c.readContentLength()
	if err != nil {
		return nil, err
	}

	payload := make([]byte, contentLength)
	if _, err := io.ReadFull(c.r, payload); err != nil {
		return nil, fmt.Errorf("read payload: %w", err)
	}
	return payload, nil
}

func (c *framedCodec) readContentLength() (int, error) {
	contentLength := -1

	for {
		line, err := c.r.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return 0, io.EOF
			}
			return 0, fmt.Errorf("read header line: %w", err)
		}

		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == "" {
			break
		}

		key, value, ok := strings.Cut(trimmed, ":")
		if !ok {
			return 0, fmt.Errorf("invalid header line %q", trimmed)
		}
		if strings.ToLower(strings.TrimSpace(key)) != headerContentLength {
			continue
		}

		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return 0, fmt.Errorf("parse content length: %w", err)
		}
		contentLength = n
	}

	if contentLength < 0 {
		return 0, fmt.Errorf("missing Content-Length header")
	}
	if contentLength > maxPayloadSize {
		return 0, fmt.Errorf("payload size %d exceeds limit %d", contentLength, maxPayloadSize)
	}
	return contentLength, nil
}
