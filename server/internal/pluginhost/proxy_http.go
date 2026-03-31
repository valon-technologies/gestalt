package pluginhost

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
)

type PluginHostServer struct {
	proto.UnimplementedPluginHostServer
	allowedHosts []string
	httpClient   *http.Client
}

func NewPluginHostServer(allowedHosts []string) *PluginHostServer {
	s := &PluginHostServer{
		allowedHosts: allowedHosts,
	}
	s.httpClient = &http.Client{
		CheckRedirect: s.checkRedirect,
	}
	return s
}

func (s *PluginHostServer) checkRedirect(req *http.Request, _ []*http.Request) error {
	if len(s.allowedHosts) == 0 {
		return nil
	}
	if !s.isHostAllowed(req.URL.Hostname()) {
		return fmt.Errorf("redirect to host %q is not in allowed_hosts", req.URL.Hostname())
	}
	return nil
}

func (s *PluginHostServer) ProxyHTTP(ctx context.Context, req *proto.ProxyHTTPRequest) (*proto.ProxyHTTPResponse, error) {
	parsed, err := url.Parse(req.GetUrl())
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	if len(s.allowedHosts) > 0 {
		host := parsed.Hostname()
		if !s.isHostAllowed(host) {
			return nil, fmt.Errorf("host %q is not in allowed_hosts", host)
		}
	}

	httpReq, err := http.NewRequestWithContext(ctx, req.GetMethod(), req.GetUrl(), bytes.NewReader(req.GetBody()))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	for k, v := range req.GetHeaders() {
		httpReq.Header.Set(k, v)
	}

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	headers := make(map[string]string, len(resp.Header))
	for k, vals := range resp.Header {
		headers[k] = strings.Join(vals, ", ")
	}

	return &proto.ProxyHTTPResponse{
		StatusCode: int32(resp.StatusCode),
		Headers:    headers,
		Body:       body,
	}, nil
}

func (s *PluginHostServer) isHostAllowed(host string) bool {
	for _, allowed := range s.allowedHosts {
		if strings.EqualFold(host, allowed) {
			return true
		}
		if strings.HasPrefix(allowed, "*.") {
			suffix := allowed[1:]
			if strings.HasSuffix(strings.ToLower(host), strings.ToLower(suffix)) {
				return true
			}
		}
	}
	return false
}
