package lansync

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/ahmojo/codex-claude-transfer/internal/agent"
	"github.com/ahmojo/codex-claude-transfer/internal/codexhome"
)

// The daemon is the "ambient" mode: instead of running `cct sync` by hand, it
// watches your sessions and keeps them in step with your other devices on the LAN
// automatically. It only ever talks to ALREADY-REMEMBERED peers (trust-on-first-
// use), so it never needs a pairing code and never sends data to a device you have
// not explicitly paired with. It is opt-in (--i-understand), like all sync.
//
// Mechanism: it (1) advertises a discovery beacon and runs a persistent listener
// that accepts code-less syncs from remembered peers, and (2) polls the sessions
// directory; when something changes it discovers peers and connects to each
// remembered one to sync. Both peers running the daemon therefore converge after
// either side changes. Each sync reuses the exact same Serve/Connect path as a
// manual sync, inheriting every safety property (checksum-verified bundle, merge,
// conflict handling, mtime preservation, cwd remap).

// DaemonOptions configures a daemon run, on top of the shared sync Options.
type DaemonOptions struct {
	// Interval is how often the sessions directory is polled for changes.
	Interval time.Duration
	// DiscoverWindow is how long each discovery sweep listens for peers.
	DiscoverWindow time.Duration
	// Once runs a single discover-and-sync sweep, then returns (no listener). It is
	// used by `--once` and keeps the daemon testable/scriptable.
	Once bool
}

const (
	defaultDaemonInterval = 5 * time.Second
	defaultDiscoverWindow = 1500 * time.Millisecond
)

// Daemon runs the ambient sync loop until ctx is cancelled (or, with Once, for a
// single sweep). It requires --i-understand and at least one remembered peer.
func Daemon(ctx context.Context, home codexhome.Home, opts Options, dopts DaemonOptions) error {
	if !opts.Confirmed {
		return fmt.Errorf("%s", ExperimentalNotice)
	}
	if len(loadPeers(opts.ConfigDir)) == 0 {
		return fmt.Errorf("no remembered peers yet: pair once with `cct sync serve --remember` / `cct sync connect ... --remember`, then run the daemon")
	}
	if dopts.Interval <= 0 {
		dopts.Interval = defaultDaemonInterval
	}
	if dopts.DiscoverWindow <= 0 {
		dopts.DiscoverWindow = defaultDiscoverWindow
	}
	cert, err := loadOrCreateIdentity(opts.ConfigDir)
	if err != nil {
		return fmt.Errorf("load device identity: %w", err)
	}
	myFP := fpHex(fingerprint(cert.Certificate[0]))
	tool := string(agent.Normalize(opts.Tool))

	roots := sessionRoots(home, opts)

	if dopts.Once {
		syncWithDiscoveredPeers(ctx, home, opts, dopts, myFP, tool)
		return nil
	}

	// Persistent listener so remembered peers can push to us at any time.
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer ln.Close()
	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port := atoiSafe(portStr)

	host, _ := os.Hostname()
	beacon := Beacon{Hostname: host, Tool: tool, Port: port, Fingerprint: myFP}
	go Advertise(ctx, beacon)
	go acceptLoop(ctx, ln, home, opts, cert)

	fmt.Fprintf(opts.Out, "cct sync daemon running (tool %s, port %d). Watching for changes; syncing with remembered peers on your LAN.\n", tool, port)
	fmt.Fprintln(opts.Out, "Press Ctrl-C to stop.")

	last := snapshot(roots)
	ticker := time.NewTicker(dopts.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			cur := snapshot(roots)
			if snapshotChanged(last, cur) {
				last = cur
				syncWithDiscoveredPeers(ctx, home, opts, dopts, myFP, tool)
			}
		}
	}
}

// syncWithDiscoveredPeers discovers peers and connects to every remembered one to
// run a single sync. Best-effort: per-peer errors are reported, not fatal.
func syncWithDiscoveredPeers(ctx context.Context, home codexhome.Home, opts Options, dopts DaemonOptions, myFP, tool string) {
	beacons, err := Discover(ctx, dopts.DiscoverWindow, tool)
	if err != nil {
		fmt.Fprintf(opts.Out, "discovery unavailable: %v\n", err)
		return
	}
	for _, b := range rememberedBeacons(beacons, opts.ConfigDir, myFP) {
		hostport := net.JoinHostPort(b.Addr, fmt.Sprintf("%d", b.Port))
		copts := opts
		copts.Code = "" // remembered peer: code-less trusted path
		res, err := Connect(home, copts, hostport)
		if err != nil {
			fmt.Fprintf(opts.Out, "sync with %s failed: %v\n", safe(b.Hostname), err)
			continue
		}
		if res.Sent > 0 || res.Received.Imported > 0 || res.Received.Updated > 0 {
			fmt.Fprintf(opts.Out, "synced with %s: sent %d, received %d, updated %d\n",
				safe(b.Hostname), res.Sent, res.Received.Imported, res.Received.Updated)
		}
	}
}

// acceptLoop accepts inbound syncs from remembered peers (code-less). Non-private
// or unknown peers are rejected by the address guard and the trusted-path auth.
func acceptLoop(ctx context.Context, ln net.Listener, home codexhome.Home, opts Options, cert tls.Certificate) {
	for {
		if ctx.Err() != nil {
			return
		}
		raw, err := ln.Accept()
		if err != nil {
			return
		}
		if err := checkRemoteAddr(raw.RemoteAddr(), opts.AllowPublic); err != nil {
			raw.Close()
			continue
		}
		go func() {
			conn := tls.Server(raw, tlsConfigServer(cert))
			// code "" => only a remembered (Known) peer authenticates.
			_, _ = handleConn(conn, home, opts, roleServer, "", cert)
			conn.Close()
		}()
	}
}

// rememberedBeacons keeps only discovered peers whose fingerprint we already
// remember and that are not us. Pure given a config dir, so it is unit-testable.
func rememberedBeacons(beacons []Beacon, configDir, myFP string) []Beacon {
	peers := loadPeers(configDir)
	var out []Beacon
	for _, b := range beacons {
		if b.Fingerprint == "" || b.Fingerprint == myFP {
			continue
		}
		if _, ok := peers[b.Fingerprint]; ok {
			out = append(out, b)
		}
	}
	return out
}

// sessionRoots returns the directories the daemon watches for the active tool.
func sessionRoots(home codexhome.Home, opts Options) []string {
	if agent.Normalize(opts.Tool) == agent.Claude {
		return []string{opts.ClaudeHome.ProjectsDir}
	}
	return []string{home.SessionsDir}
}

func atoiSafe(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return n
		}
		n = n*10 + int(r-'0')
	}
	return n
}
