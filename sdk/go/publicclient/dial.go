package publicclient

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

func parsePublicURL(address string) (*url.URL, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return nil, fmt.Errorf("publicclient: address is required")
	}
	parsed, err := url.Parse(address)
	if err != nil {
		return nil, fmt.Errorf("publicclient: invalid address %q: %w", address, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("publicclient: invalid address %q", address)
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
	default:
		return nil, fmt.Errorf("publicclient: unsupported address scheme %q", parsed.Scheme)
	}
	return parsed, nil
}

func normalizeAddress(address string) (string, error) {
	parsed, err := parsePublicURL(address)
	if err != nil {
		return "", err
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimSuffix(parsed.String(), "/"), nil
}

func dialPublicGRPC(address string) (*grpc.ClientConn, error) {
	parsed, err := parsePublicURL(address)
	if err != nil {
		return nil, err
	}
	var creds credentials.TransportCredentials
	if strings.EqualFold(parsed.Scheme, "https") {
		creds = credentials.NewTLS(nil)
	} else {
		creds = insecure.NewCredentials()
	}
	return grpc.NewClient(grpcTarget(parsed), grpc.WithTransportCredentials(creds))
}

func grpcTarget(parsed *url.URL) string {
	host := parsed.Hostname()
	port := parsed.Port()
	if port == "" {
		if strings.EqualFold(parsed.Scheme, "https") {
			port = "443"
		} else {
			port = "80"
		}
	}
	return net.JoinHostPort(host, port)
}
