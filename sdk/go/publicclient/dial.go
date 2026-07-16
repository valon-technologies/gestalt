package publicclient

import (
	"crypto/tls"
	"fmt"
	"net/url"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

func dialPublicGRPC(address string) (*grpc.ClientConn, error) {
	parsed, err := url.Parse(strings.TrimSpace(address))
	if err != nil {
		return nil, fmt.Errorf("publicclient: invalid address %q: %w", address, err)
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("publicclient: invalid address %q", address)
	}
	target := parsed.Host
	var creds credentials.TransportCredentials
	switch strings.ToLower(parsed.Scheme) {
	case "https":
		creds = credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12})
	case "http":
		creds = insecure.NewCredentials()
	default:
		return nil, fmt.Errorf("publicclient: unsupported address scheme %q", parsed.Scheme)
	}
	return grpc.NewClient(target, grpc.WithTransportCredentials(creds))
}
