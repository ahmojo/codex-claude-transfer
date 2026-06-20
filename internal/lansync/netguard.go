package lansync

import (
	"fmt"
	"net"
)

// cgnat is the RFC6598 shared-address space 100.64.0.0/10, used by overlay
// networks (Tailscale, some ZeroTier setups) that ARE the user's own network even
// though they are not RFC1918. We treat it as allowed so users on those networks
// do not have to reach for --allow-public. (IPv4-mapped IPv6 forms collapse to
// their v4 here via To4, so they are checked consistently.)
var cgnat = func() *net.IPNet {
	_, n, _ := net.ParseCIDR("100.64.0.0/10")
	return n
}()

// ipAllowed reports whether an address is on the user's own machine or local
// network: loopback, RFC1918/ULA private, link-local, or CGNAT/overlay space.
// This is the enforced "only your local network" anti-exfiltration rule.
func ipAllowed(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	if v4 := ip.To4(); v4 != nil && cgnat.Contains(v4) {
		return true
	}
	return false
}

// addrIP extracts the IP from a net.Addr (TCP).
func addrIP(addr net.Addr) net.IP {
	switch a := addr.(type) {
	case *net.TCPAddr:
		return a.IP
	default:
		host, _, err := net.SplitHostPort(addr.String())
		if err != nil {
			return nil
		}
		return net.ParseIP(host)
	}
}

// checkRemoteAddr enforces the private-address rule on a connection's remote peer,
// unless the caller explicitly opted into public addresses.
func checkRemoteAddr(addr net.Addr, allowPublic bool) error {
	if allowPublic {
		return nil
	}
	ip := addrIP(addr)
	if !ipAllowed(ip) {
		return fmt.Errorf("refusing to talk to non-private address %s (LAN sync is local-network only; pass --allow-public to override)", addr.String())
	}
	return nil
}

// resolveAllowedIP resolves a connect target to a SINGLE concrete IP that passed
// the private-address guard, so the caller can dial that exact IP. Returning the
// chosen address (rather than re-resolving at dial time) closes the DNS-rebinding
// window: a hostname cannot pass the check as private and then connect to a public
// IP, because we never resolve it a second time.
func resolveAllowedIP(host string, allowPublic bool) (net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		if !allowPublic && !ipAllowed(ip) {
			return nil, fmt.Errorf("refusing to connect to non-private address %s (pass --allow-public to override)", host)
		}
		return ip, nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", host, err)
	}
	for _, ip := range ips {
		if allowPublic || ipAllowed(ip) {
			return ip, nil
		}
	}
	return nil, fmt.Errorf("%s resolves only to non-private addresses (pass --allow-public to override)", host)
}
