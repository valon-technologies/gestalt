package gestalt

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
)

type ProxiedTransport struct {
	client proto.PluginHostClient
	conn   io.Closer
}

func NewProxiedTransport(ctx context.Context) (*ProxiedTransport, error) {
	socket := os.Getenv(proto.EnvPluginHostSocket)
	if socket == "" {
		return nil, fmt.Errorf("%s is required for proxied transport", proto.EnvPluginHostSocket)
	}
	conn, err := dialUnixSocket(ctx, socket)
	if err != nil {
		return nil, fmt.Errorf("dial plugin host: %w", err)
	}
	return &ProxiedTransport{client: proto.NewPluginHostClient(conn), conn: conn}, nil
}

func (t *ProxiedTransport) Close() error {
	if t.conn != nil {
		return t.conn.Close()
	}
	return nil
}

func NewProxiedTransportFromClient(client proto.PluginHostClient) *ProxiedTransport {
	return &ProxiedTransport{client: client}
}

func (t *ProxiedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var bodyBytes []byte
	if req.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(req.Body)
		_ = req.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("read request body: %w", err)
		}
	}

	headers := make(map[string]string, len(req.Header))
	for k, vals := range req.Header {
		headers[k] = strings.Join(vals, ", ")
	}

	resp, err := t.client.ProxyHTTP(req.Context(), &proto.ProxyHTTPRequest{
		Method:  req.Method,
		Url:     req.URL.String(),
		Headers: headers,
		Body:    bodyBytes,
	})
	if err != nil {
		return nil, err
	}

	respHeaders := make(http.Header, len(resp.GetHeaders()))
	for k, v := range resp.GetHeaders() {
		respHeaders.Set(k, v)
	}

	return &http.Response{
		StatusCode: int(resp.GetStatusCode()),
		Header:     respHeaders,
		Body:       io.NopCloser(bytes.NewReader(resp.GetBody())),
		Request:    req,
	}, nil
}
