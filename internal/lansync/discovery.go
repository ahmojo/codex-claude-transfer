package lansync

import (
	"context"
	"encoding/json"
	"net"
	"time"
)

// LAN peer discovery, so a device can find a serving/daemon peer without anyone
// typing an IP:port. It is a small cct-specific multicast beacon, NOT DNS-SD/mDNS:
// cct only ever talks to cct, so a tiny JSON beacon on an admin-scoped multicast
// group is simpler and dependency-free (matching this package's no-mDNS-library
// stance) while giving the same "it just finds the other device" experience.
//
// A beacon advertises only non-secret coordinates (host, port, tool, hostname,
// and the device's stable cert fingerprint so a discoverer can recognize a
// remembered peer). Pairing/auth still happen over the authenticated TLS channel;
// discovery only removes the address-typing step.

// discoveryGroup is an administratively-scoped (239.x) multicast address — local
// to the LAN, never routed off it.
var discoveryGroup = &net.UDPAddr{IP: net.IPv4(239, 255, 77, 73), Port: 54547}

// beaconInterval is how often an advertiser re-announces itself.
const beaconInterval = 2 * time.Second

// maxBeaconBytes caps a received datagram so a hostile sender cannot force a large
// allocation.
const maxBeaconBytes = 4 << 10

// Beacon is the advertised, non-secret description of a discoverable peer.
type Beacon struct {
	Magic       string `json:"cct"`         // protocol marker, always beaconMagic
	Hostname    string `json:"hostname"`    // human label
	Tool        string `json:"tool"`        // codex | claude
	Port        int    `json:"port"`        // the sync TCP port to dial
	Fingerprint string `json:"fingerprint"` // sender's cert fingerprint (hex), for trust recognition
	// Addr is filled in by the discoverer from the datagram source; it is not sent
	// on the wire (the sender does not know which interface a peer sees it on).
	Addr string `json:"-"`
}

const beaconMagic = "sync-v1"

// encodeBeacon marshals a beacon for the wire.
func encodeBeacon(b Beacon) ([]byte, error) {
	b.Magic = beaconMagic
	return json.Marshal(b)
}

// decodeBeacon parses a datagram into a beacon, rejecting anything that is not a
// well-formed cct beacon.
func decodeBeacon(data []byte) (Beacon, bool) {
	if len(data) == 0 || len(data) > maxBeaconBytes {
		return Beacon{}, false
	}
	var b Beacon
	if json.Unmarshal(data, &b) != nil {
		return Beacon{}, false
	}
	if b.Magic != beaconMagic || b.Port <= 0 || b.Port > 65535 {
		return Beacon{}, false
	}
	return b, true
}

// dedupeBeacons collapses repeated announcements (same host:port) to the first
// seen, preserving order, optionally filtering by tool. It is pure for testing.
func dedupeBeacons(in []Beacon, tool string) []Beacon {
	seen := map[string]bool{}
	var out []Beacon
	for _, b := range in {
		if tool != "" && b.Tool != "" && b.Tool != tool {
			continue
		}
		key := b.Addr + "|" + b.Fingerprint
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, b)
	}
	return out
}

// Advertise multicasts the given beacon every beaconInterval until ctx is done.
// It is best-effort: a failure to open the socket returns an error, but transient
// write errors are ignored so a flaky interface does not kill a daemon.
func Advertise(ctx context.Context, b Beacon) error {
	payload, err := encodeBeacon(b)
	if err != nil {
		return err
	}
	conn, err := net.DialUDP("udp4", nil, discoveryGroup)
	if err != nil {
		return err
	}
	defer conn.Close()

	// Announce immediately, then on a ticker.
	_, _ = conn.Write(payload)
	t := time.NewTicker(beaconInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			_, _ = conn.Write(payload)
		}
	}
}

// Discover listens for beacons for up to timeout and returns the distinct peers
// seen (optionally filtered to one tool). It never blocks longer than timeout.
func Discover(ctx context.Context, timeout time.Duration, tool string) ([]Beacon, error) {
	conn, err := net.ListenMulticastUDP("udp4", nil, discoveryGroup)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	_ = conn.SetReadBuffer(1 << 20)

	deadline := time.Now().Add(timeout)
	conn.SetReadDeadline(deadline)

	var found []Beacon
	buf := make([]byte, maxBeaconBytes)
	for {
		if ctx.Err() != nil {
			break
		}
		if time.Now().After(deadline) {
			break
		}
		n, src, rerr := conn.ReadFromUDP(buf)
		if rerr != nil {
			break // deadline or transient: stop collecting
		}
		b, ok := decodeBeacon(buf[:n])
		if !ok {
			continue
		}
		if src != nil {
			b.Addr = src.IP.String()
		}
		found = append(found, b)
	}
	return dedupeBeacons(found, tool), nil
}
