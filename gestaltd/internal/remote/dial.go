package remote

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/url"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

func dialOptions(rawURL string) (target string, opts []grpc.DialOption, err error) {
	rawURL = strings.TrimRight(strings.TrimSpace(rawURL), "/")
	parsed, err := url.Parse(rawURL)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" {
		return "", nil, fmt.Errorf("remote URL must be an absolute URL")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", nil, fmt.Errorf("remote URL may not include query or fragment")
	}

	host := parsed.Hostname()
	port := parsed.Port()
	switch parsed.Scheme {
	case "https":
		if port == "" {
			port = "443"
		}
		return net.JoinHostPort(host, port), []grpc.DialOption{
			grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
				MinVersion: tls.VersionTLS12,
				ServerName: host,
				NextProtos: []string{"h2"},
			})),
		}, nil
	case "http":
		if port == "" {
			port = "80"
		}
		return net.JoinHostPort(host, port), []grpc.DialOption{
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		}, nil
	default:
		return "", nil, fmt.Errorf("remote URL scheme %q is not supported", parsed.Scheme)
	}
}

func waitForReady(ctx context.Context, conn *grpc.ClientConn) error {
	conn.Connect()
	for {
		state := conn.GetState()
		if state == connectivity.Ready {
			return nil
		}
		if state == connectivity.Shutdown {
			return fmt.Errorf("remote gestaltd connection closed before ready")
		}
		if !conn.WaitForStateChange(ctx, state) {
			return ctx.Err()
		}
	}
}
