package lansync

import (
	"net"
	"testing"
)

func TestIPAllowed(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1":          true,  // loopback
		"::1":                true,  // loopback v6
		"10.0.0.5":           true,  // RFC1918
		"192.168.1.20":       true,  // RFC1918
		"172.16.3.4":         true,  // RFC1918
		"169.254.1.1":        true,  // link-local
		"fe80::1":            true,  // link-local v6
		"fd12:3456::1":       true,  // ULA (fc00::/7)
		"100.64.0.1":         true,  // CGNAT / overlay (Tailscale)
		"100.127.255.254":    true,  // CGNAT upper edge
		"::ffff:192.168.0.9": true,  // IPv4-mapped private
		"8.8.8.8":            false, // public
		"1.1.1.1":            false, // public
		"203.0.113.7":        false, // public (TEST-NET-3)
		"172.32.0.1":         false, // just outside RFC1918
		"100.128.0.1":        false, // just outside CGNAT /10
		"::ffff:8.8.8.8":     false, // IPv4-mapped public
		"2606:4700::1111":    false, // public v6
	}
	for s, want := range cases {
		ip := net.ParseIP(s)
		if ip == nil {
			t.Fatalf("bad test IP %q", s)
		}
		if got := ipAllowed(ip); got != want {
			t.Errorf("ipAllowed(%s) = %v, want %v", s, got, want)
		}
	}
}

func TestCheckRemoteAddr(t *testing.T) {
	pub := &net.TCPAddr{IP: net.ParseIP("8.8.8.8"), Port: 5000}
	if err := checkRemoteAddr(pub, false); err == nil {
		t.Error("expected a public remote address to be rejected")
	}
	if err := checkRemoteAddr(pub, true); err != nil {
		t.Errorf("--allow-public should permit a public address: %v", err)
	}
	priv := &net.TCPAddr{IP: net.ParseIP("192.168.0.10"), Port: 5000}
	if err := checkRemoteAddr(priv, false); err != nil {
		t.Errorf("private address should be allowed: %v", err)
	}
}

func TestResolveAllowedIP(t *testing.T) {
	if _, err := resolveAllowedIP("8.8.8.8", false); err == nil {
		t.Error("expected public literal to be rejected")
	}
	if ip, err := resolveAllowedIP("127.0.0.1", false); err != nil || ip == nil {
		t.Errorf("loopback should resolve: ip=%v err=%v", ip, err)
	}
	// --allow-public lets a public literal through.
	if _, err := resolveAllowedIP("8.8.8.8", true); err != nil {
		t.Errorf("--allow-public should permit a public literal: %v", err)
	}
}
