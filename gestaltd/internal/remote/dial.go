package remote

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/url"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

func dialRemote(_ context.Context, rawURL, token string) (*grpc.ClientConn, error) {
	target, creds, err := remoteGRPCTarget(rawURL)
	if err != nil {
		return nil, err
	}
	unary, stream := bearerTokenInterceptors(token)
	conn, err := grpc.NewClient(
		target,
		grpc.WithTransportCredentials(creds),
		grpc.WithChainUnaryInterceptor(unary),
		grpc.WithChainStreamInterceptor(stream),
	)
	if err != nil {
		return nil, fmt.Errorf("remote: dial %s: %w", target, err)
	}
	return conn, nil
}

func remoteGRPCTarget(rawURL string) (target string, creds credentials.TransportCredentials, err error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", nil, fmt.Errorf("remote: parse URL %q: %w", rawURL, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", nil, fmt.Errorf("remote: URL %q must include scheme and host", rawURL)
	}

	host := parsed.Hostname()
	port := parsed.Port()
	switch parsed.Scheme {
	case "https":
		if port == "" {
			port = "443"
		}
		creds = credentials.NewTLS(&tls.Config{
			MinVersion: tls.VersionTLS12,
			ServerName: host,
		})
	case "http":
		if port == "" {
			port = "80"
		}
		creds = insecure.NewCredentials()
	default:
		return "", nil, fmt.Errorf("remote: unsupported URL scheme %q", parsed.Scheme)
	}
	return net.JoinHostPort(host, port), creds, nil
}

func bearerTokenInterceptors(token string) (grpc.UnaryClientInterceptor, grpc.StreamClientInterceptor) {
	unary := func(
		ctx context.Context,
		method string,
		req, reply any,
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		return invoker(WithBearer(ctx, token), method, req, reply, cc, opts...)
	}
	stream := func(
		ctx context.Context,
		desc *grpc.StreamDesc,
		cc *grpc.ClientConn,
		method string,
		streamer grpc.Streamer,
		opts ...grpc.CallOption,
	) (grpc.ClientStream, error) {
		return streamer(WithBearer(ctx, token), desc, cc, method, opts...)
	}
	return unary, stream
}
