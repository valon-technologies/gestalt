package egress

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
)

// DestinationPolicy controls the narrow local-development exceptions allowed
// by safe outbound dialing. Metadata addresses are always denied.
type DestinationPolicy struct {
	AllowLoopback  bool
	AllowPrivate   bool
	AllowLinkLocal bool
}

var (
	// ErrUnsafeDestination is returned when a destination resolves to an
	// internal, loopback, link-local, metadata, or otherwise unroutable address.
	ErrUnsafeDestination = errors.New("egress unsafe destination")

	awsMetadataIPv4     = netip.MustParseAddr("169.254.169.254")
	awsMetadataIPv6     = netip.MustParseAddr("fd00:ec2::254")
	alibabaMetadataIPv4 = netip.MustParseAddr("100.100.100.200")
	carrierGradeNATIPv4 = netip.MustParsePrefix("100.64.0.0/10")
)

// IsLocalhostName reports whether host is an explicit localhost or loopback
// literal. It intentionally does not resolve arbitrary names.
func IsLocalhostName(host string) bool {
	host = normalizeHost(host)
	if host == "localhost" {
		return true
	}
	addr, err := netip.ParseAddr(host)
	return err == nil && addr.Unmap().IsLoopback()
}

// RejectUnsafeHostLiteral rejects unsafe IP literals without resolving names.
func RejectUnsafeHostLiteral(host string, policy DestinationPolicy) error {
	addr, err := netip.ParseAddr(normalizeHost(host))
	if err != nil {
		return nil
	}
	return CheckAddrAllowed(addr, policy)
}

// CheckAddrAllowed rejects internal and metadata destinations according to
// policy. Metadata addresses remain denied even when a local-dev exception is
// enabled.
func CheckAddrAllowed(addr netip.Addr, policy DestinationPolicy) error {
	addr = addr.Unmap()
	switch {
	case !addr.IsValid():
		return fmt.Errorf("%w: invalid IP address", ErrUnsafeDestination)
	case isMetadataAddr(addr):
		return fmt.Errorf("%w: metadata IP %s is not allowed", ErrUnsafeDestination, addr)
	case addr.IsLoopback() && !policy.AllowLoopback:
		return fmt.Errorf("%w: loopback IP %s is not allowed", ErrUnsafeDestination, addr)
	case addr.IsPrivate() && !policy.AllowPrivate:
		return fmt.Errorf("%w: private IP %s is not allowed", ErrUnsafeDestination, addr)
	case carrierGradeNATIPv4.Contains(addr) && !policy.AllowPrivate:
		return fmt.Errorf("%w: shared/private IP %s is not allowed", ErrUnsafeDestination, addr)
	case (addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast()) && !policy.AllowLinkLocal:
		return fmt.Errorf("%w: link-local IP %s is not allowed", ErrUnsafeDestination, addr)
	case addr.IsUnspecified():
		return fmt.Errorf("%w: unspecified IP %s is not allowed", ErrUnsafeDestination, addr)
	case addr.IsMulticast() || addr.IsInterfaceLocalMulticast():
		return fmt.Errorf("%w: multicast IP %s is not allowed", ErrUnsafeDestination, addr)
	}
	return nil
}

func isMetadataAddr(addr netip.Addr) bool {
	return addr == awsMetadataIPv4 || addr == awsMetadataIPv6 || addr == alibabaMetadataIPv4
}

// ResolveSafeTCPAddr resolves address, rejects unsafe answers, and returns a
// host:port string pinned to the selected safe IP.
func ResolveSafeTCPAddr(ctx context.Context, network, address string, policy DestinationPolicy) (string, error) {
	addrs, err := ResolveSafeTCPAddrs(ctx, network, address, policy)
	if err != nil {
		return "", err
	}
	return addrs[0], nil
}

// ResolveSafeTCPAddrs resolves address, rejects unsafe answers, and returns
// host:port strings pinned to safe IPs.
func ResolveSafeTCPAddrs(ctx context.Context, network, address string, policy DestinationPolicy) ([]string, error) {
	host, port, err := SplitHostPortDefault(address, "")
	if err != nil {
		return nil, err
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		addr = addr.Unmap()
		if !addrMatchesNetwork(addr, network) {
			return nil, fmt.Errorf("%w: IP %s does not match %s", ErrUnsafeDestination, addr, network)
		}
		if err := CheckAddrAllowed(addr, policy); err != nil {
			return nil, err
		}
		return []string{net.JoinHostPort(addr.String(), port)}, nil
	}

	resolverNetwork := "ip"
	switch network {
	case "tcp4":
		resolverNetwork = "ip4"
	case "tcp6":
		resolverNetwork = "ip6"
	}
	ips, err := net.DefaultResolver.LookupNetIP(ctx, resolverNetwork, host)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", host, err)
	}
	var denied []error
	safeAddrs := make([]string, 0, len(ips))
	for _, ip := range ips {
		ip = ip.Unmap()
		if !addrMatchesNetwork(ip, network) {
			continue
		}
		if err := CheckAddrAllowed(ip, policy); err != nil {
			denied = append(denied, err)
			continue
		}
		safeAddrs = append(safeAddrs, net.JoinHostPort(ip.String(), port))
	}
	if len(safeAddrs) > 0 {
		return safeAddrs, nil
	}
	if len(denied) > 0 {
		return nil, denied[0]
	}
	return nil, fmt.Errorf("%w: %s resolved to no usable IPs", ErrUnsafeDestination, host)
}

// SafeDialContext resolves through ResolveSafeTCPAddrs before dialing so DNS
// rebinding cannot swap in an unsafe address after policy checks.
func SafeDialContext(policy DestinationPolicy) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		if strings.HasPrefix(network, "tcp") {
			safeAddrs, err := ResolveSafeTCPAddrs(ctx, network, address, policy)
			if err != nil {
				return nil, err
			}
			var errs []error
			var dialer net.Dialer
			for _, safeAddr := range safeAddrs {
				conn, err := dialer.DialContext(ctx, network, safeAddr)
				if err == nil {
					return conn, nil
				}
				errs = append(errs, err)
			}
			return nil, fmt.Errorf("dial safe destination %s: %w", address, errors.Join(errs...))
		}
		var dialer net.Dialer
		return dialer.DialContext(ctx, network, address)
	}
}

func addrMatchesNetwork(addr netip.Addr, network string) bool {
	addr = addr.Unmap()
	switch network {
	case "tcp4":
		return addr.Is4()
	case "tcp6":
		return addr.Is6() && !addr.Is4()
	default:
		return true
	}
}
