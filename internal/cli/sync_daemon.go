package cli

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"time"

	"github.com/ahmojo/codex-claude-transfer/internal/agent"
	"github.com/ahmojo/codex-claude-transfer/internal/codexhome"
	"github.com/ahmojo/codex-claude-transfer/internal/lansync"
)

// runSyncDaemon runs the ambient sync daemon until interrupted (Ctrl-C), or for a
// single sweep with --once.
func runSyncDaemon(home codexhome.Home, opts lansync.Options, f commonFlags, uxOut, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	dopts := lansync.DaemonOptions{Once: f.once}
	if f.interval > 0 {
		dopts.Interval = time.Duration(f.interval) * time.Second
	}
	if err := lansync.Daemon(ctx, home, opts, dopts); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	if f.once {
		fmt.Fprintln(uxOut, "Daemon sweep complete.")
	} else {
		fmt.Fprintln(uxOut, "Daemon stopped.")
	}
	return 0
}

// resolveConnectTarget returns the host:port for `cct sync connect`. An explicit
// positional argument is used as-is; otherwise it auto-discovers a serving/daemon
// peer on the LAN and uses (or, with several, the first) one found.
func resolveConnectTarget(f commonFlags, opts lansync.Options, uxOut, stderr io.Writer) (string, int) {
	if len(f.positional) == 1 {
		return f.positional[0], 0
	}
	if len(f.positional) > 1 {
		fmt.Fprintln(stderr, "usage: cct sync connect [host:port] --i-understand")
		return "", 2
	}
	fmt.Fprintln(uxOut, "No address given — looking for a cct peer on your local network…")
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	beacons, err := lansync.Discover(ctx, 3*time.Second, string(agent.Normalize(opts.Tool)))
	if err != nil {
		fmt.Fprintf(stderr, "error: discovery failed: %v\n", err)
		fmt.Fprintln(stderr, "Pass the address explicitly: cct sync connect <host:port> --i-understand")
		return "", 1
	}
	if len(beacons) == 0 {
		fmt.Fprintln(stderr, "error: no peer found on the local network.")
		fmt.Fprintln(stderr, "Make sure the other device is running `cct sync serve` (or the daemon),")
		fmt.Fprintln(stderr, "or pass the address explicitly: cct sync connect <host:port> --i-understand")
		return "", 1
	}
	b := beacons[0]
	hostport := net.JoinHostPort(b.Addr, fmt.Sprintf("%d", b.Port))
	fmt.Fprintf(uxOut, "Found %s at %s.\n", safeTerminal(b.Hostname), hostport)
	return hostport, 0
}
