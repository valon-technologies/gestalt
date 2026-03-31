package pluginhost

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	pluginapiv1 "github.com/valon-technologies/gestalt/sdk/pluginapi/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var blockedHeaders = map[string]bool{
	"authorization":       true,
	"host":                true,
	"x-forwarded-for":     true,
	"x-forwarded-host":    true,
	"x-forwarded-proto":   true,
	"x-real-ip":           true,
	"proxy-authorization": true,
}

func blockedProxyHeader(name string) bool {
	return blockedHeaders[strings.ToLower(name)]
}

const maxProxyResponseBytes = 50 << 20 // 50 MB

type invocationIDKey struct{}

func WithInvocationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, invocationIDKey{}, id)
}

func InvocationID(ctx context.Context) string {
	id, _ := ctx.Value(invocationIDKey{}).(string)
	return id
}

type ProviderHostServer struct {
	pluginapiv1.UnimplementedProviderHostServer
	tokens       *sync.Map
	httpClient   *http.Client
	allowedHosts []string
	validateURL  func(*url.URL) error
}

func (s *ProviderHostServer) ProxyHTTP(ctx context.Context, req *pluginapiv1.ProxyHTTPRequest) (*pluginapiv1.ProxyHTTPResponse, error) {
	if req.GetInvocationId() == "" {
		return nil, status.Error(codes.InvalidArgument, "invocation_id is required")
	}
	val, ok := s.tokens.Load(req.GetInvocationId())
	if !ok {
		return nil, status.Error(codes.PermissionDenied, "unknown invocation_id")
	}
	token, _ := val.(string)

	parsed, err := url.Parse(req.GetUrl())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid url: %v", err)
	}
	validate := s.validateURL
	if validate == nil {
		validate = validateProxyURL
	}
	if err := validate(parsed); err != nil {
		return nil, status.Errorf(codes.PermissionDenied, "%v", err)
	}

	if len(s.allowedHosts) > 0 && !s.hostAllowed(parsed.Hostname()) {
		return nil, status.Errorf(codes.PermissionDenied, "host %q is not in the allowed list", parsed.Hostname())
	}

	httpReq, err := http.NewRequestWithContext(ctx, req.GetMethod(), req.GetUrl(), bytes.NewReader(req.GetBody()))
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "building request: %v", err)
	}
	for k, v := range req.GetHeaders() {
		if !blockedProxyHeader(k) {
			httpReq.Header.Set(k, v)
		}
	}
	if token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+token)
	}

	if s.httpClient == nil {
		return nil, status.Error(codes.Internal, "proxy http client not configured")
	}
	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "executing request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxProxyResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	headers := make(map[string]string, len(resp.Header))
	for k := range resp.Header {
		headers[k] = resp.Header.Get(k)
	}

	return &pluginapiv1.ProxyHTTPResponse{
		StatusCode: int32(resp.StatusCode),
		Headers:    headers,
		Body:       body,
	}, nil
}

func (s *ProviderHostServer) hostAllowed(hostname string) bool {
	for _, h := range s.allowedHosts {
		if strings.EqualFold(h, hostname) {
			return true
		}
	}
	return false
}

func validateProxyURL(u *url.URL) error {
	if u.Scheme != "https" {
		return fmt.Errorf("scheme %q is not allowed; only https is permitted", u.Scheme)
	}

	hostname := u.Hostname()
	if hostname == "" {
		return fmt.Errorf("empty hostname")
	}

	ip := net.ParseIP(hostname)
	if ip == nil {
		// DNS name: block localhost
		if strings.EqualFold(hostname, "localhost") {
			return fmt.Errorf("requests to localhost are not permitted")
		}
		// Block common internal suffixes
		lower := strings.ToLower(hostname)
		for _, suffix := range []string{".local", ".internal", ".cluster.local"} {
			if strings.HasSuffix(lower, suffix) {
				return fmt.Errorf("requests to internal host %q are not permitted", hostname)
			}
		}
		return nil
	}

	if isPrivateIP(ip) {
		return fmt.Errorf("requests to private/reserved IP %s are not permitted", ip)
	}
	return nil
}

var privateRanges []*net.IPNet

func init() {
	for _, cidr := range []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"169.254.0.0/16",
		"127.0.0.0/8",
		"0.0.0.0/8",
		"100.64.0.0/10",
		"fc00::/7",
		"::1/128",
		"::/128",
		"fe80::/10",
	} {
		_, n, _ := net.ParseCIDR(cidr)
		privateRanges = append(privateRanges, n)
	}
}

func isPrivateIP(ip net.IP) bool {
	for _, r := range privateRanges {
		if r.Contains(ip) {
			return true
		}
	}
	return false
}

func newProxyHTTPClient(allowedHosts []string) *http.Client {
	allowed := make(map[string]bool, len(allowedHosts))
	for _, h := range allowedHosts {
		allowed[strings.ToLower(h)] = true
	}
	return &http.Client{
		Timeout:   30 * time.Second,
		Transport: ssrfSafeTransport(),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if err := validateProxyURL(req.URL); err != nil {
				return fmt.Errorf("redirect blocked: %w", err)
			}
			if len(allowed) > 0 && !allowed[strings.ToLower(req.URL.Hostname())] {
				return fmt.Errorf("redirect to host %q is not in the allowed list", req.URL.Hostname())
			}
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			if len(via) > 0 && req.URL.Host != via[len(via)-1].URL.Host {
				req.Header.Del("Authorization")
			}
			return nil
		},
	}
}

func ssrfSafeTransport() *http.Transport {
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.Proxy = nil
	base.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("splitting host:port: %w", err)
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("no addresses found for host %q", host)
		}
		for _, ip := range ips {
			if isPrivateIP(ip.IP) {
				return nil, fmt.Errorf("resolved IP %s for host %q is private/reserved", ip.IP, host)
			}
		}
		var d net.Dialer
		var lastErr error
		for _, ip := range ips {
			conn, err := d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		return nil, lastErr
	}
	return base
}
