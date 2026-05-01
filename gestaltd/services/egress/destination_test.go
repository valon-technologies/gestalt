package egress

import (
	"context"
	"errors"
	"net/netip"
	"testing"
)

func TestCheckAddrAllowedRejectsUnsafeRanges(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		ip   string
	}{
		{name: "loopback", ip: "127.0.0.1"},
		{name: "private", ip: "10.0.0.8"},
		{name: "link-local", ip: "169.254.1.2"},
		{name: "metadata", ip: "169.254.169.254"},
		{name: "shared", ip: "100.100.100.200"},
		{name: "unspecified", ip: "0.0.0.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := CheckAddrAllowed(netip.MustParseAddr(tt.ip), DestinationPolicy{})
			if !errors.Is(err, ErrUnsafeDestination) {
				t.Fatalf("CheckAddrAllowed(%s) = %v, want ErrUnsafeDestination", tt.ip, err)
			}
		})
	}
}

func TestResolveSafeTCPAddrRejectsLocalhostByDefault(t *testing.T) {
	t.Parallel()
	_, err := ResolveSafeTCPAddr(context.Background(), "tcp", "localhost:80", DestinationPolicy{})
	if !errors.Is(err, ErrUnsafeDestination) {
		t.Fatalf("ResolveSafeTCPAddr localhost = %v, want ErrUnsafeDestination", err)
	}
}

func TestResolveSafeTCPAddrAllowsLoopbackWhenExplicitlyEnabled(t *testing.T) {
	t.Parallel()
	addr, err := ResolveSafeTCPAddr(context.Background(), "tcp", "127.0.0.1:80", DestinationPolicy{AllowLoopback: true})
	if err != nil {
		t.Fatalf("ResolveSafeTCPAddr: %v", err)
	}
	if addr != "127.0.0.1:80" {
		t.Fatalf("addr = %q, want 127.0.0.1:80", addr)
	}
}
