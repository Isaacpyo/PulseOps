package probe

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"syscall"
)

// ErrBlockedAddress is returned when a target resolves to an address that
// probes are not allowed to reach (SSRF protection).
var ErrBlockedAddress = errors.New("target resolves to a private or reserved address")

// Extra reserved ranges not covered by netip.Addr helper methods.
var reservedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),       // "this network"
	netip.MustParsePrefix("100.64.0.0/10"),   // carrier-grade NAT
	netip.MustParsePrefix("192.0.0.0/24"),    // IETF protocol assignments
	netip.MustParsePrefix("192.0.2.0/24"),    // TEST-NET-1
	netip.MustParsePrefix("198.18.0.0/15"),   // benchmarking
	netip.MustParsePrefix("198.51.100.0/24"), // TEST-NET-2
	netip.MustParsePrefix("203.0.113.0/24"),  // TEST-NET-3
	netip.MustParsePrefix("240.0.0.0/4"),     // reserved + broadcast
	netip.MustParsePrefix("64:ff9b::/96"),    // NAT64 can map onto private IPv4
	netip.MustParsePrefix("2001:db8::/32"),   // documentation
}

// IsPublicAddr reports whether addr is a globally routable unicast address.
func IsPublicAddr(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() ||
		addr.IsLoopback() ||
		addr.IsPrivate() ||
		addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast() ||
		addr.IsInterfaceLocalMulticast() ||
		addr.IsMulticast() ||
		addr.IsUnspecified() {
		return false
	}
	for _, p := range reservedPrefixes {
		if p.Contains(addr) {
			return false
		}
	}
	return true
}

// guardControl is a net.Dialer Control hook. It runs after DNS resolution,
// on the exact IP being dialled, so DNS-rebinding tricks cannot bypass it.
func guardControl(allowPrivate bool) func(network, address string, c syscall.RawConn) error {
	return func(_, address string, _ syscall.RawConn) error {
		if allowPrivate {
			return nil
		}
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		addr, err := netip.ParseAddr(host)
		if err != nil {
			return fmt.Errorf("unexpected dial address %q", address)
		}
		if !IsPublicAddr(addr) {
			return ErrBlockedAddress
		}
		return nil
	}
}

// CheckHostLiteral performs the cheap, pre-resolution validation used at the
// API boundary: obvious local hostnames and private IP literals are rejected
// up front so users get a clear validation error. Hostnames that resolve to
// private ranges are still blocked at dial time by guardControl.
func CheckHostLiteral(host string, allowPrivate bool) error {
	if allowPrivate {
		return nil
	}
	h := strings.ToLower(strings.TrimSuffix(host, "."))
	if h == "localhost" || strings.HasSuffix(h, ".localhost") || strings.HasSuffix(h, ".internal") || strings.HasSuffix(h, ".local") {
		return ErrBlockedAddress
	}
	if addr, err := netip.ParseAddr(strings.Trim(h, "[]")); err == nil && !IsPublicAddr(addr) {
		return ErrBlockedAddress
	}
	return nil
}
