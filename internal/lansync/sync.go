package lansync

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/ahmojo/codex-claude-transfer/internal/agent"
	"github.com/ahmojo/codex-claude-transfer/internal/bundle"
	"github.com/ahmojo/codex-claude-transfer/internal/claudehome"
	"github.com/ahmojo/codex-claude-transfer/internal/claudesessions"
	"github.com/ahmojo/codex-claude-transfer/internal/codexhome"
	"github.com/ahmojo/codex-claude-transfer/internal/sessions"
)

// Options configures one LAN sync run.
type Options struct {
	Tool        agent.Kind      // codex or claude
	ClaudeHome  claudehome.Home // used to scan when Tool == claude
	ProjectPath string          // absolute; "" means all in-scope sessions
	Port        int             // serve: listen port (0 = OS-assigned)
	Code        string          // connect: the pairing code shown by the peer
	AllowPublic bool            // override the private-address guard
	PullOnly    bool            // do not send my sessions
	PushOnly    bool            // do not apply received sessions
	DryRun      bool            // exchange manifests and preview only; write nothing
	Confirmed   bool            // the experimental --i-understand gate
	Out         io.Writer       // progress/UX output
	// MapCWD / MapCWDHere / HereDir remap a received session's recorded cwd as it
	// is applied, so a peer's project that lives at a different path here still
	// lands under the right local project (and, for Claude, the right sidebar
	// group). Same semantics as on import.
	MapCWD     []bundle.CWDMapping
	MapCWDHere bool
	HereDir    string
	// Remember stores the peer's fingerprint after a successful code pairing
	// (trust-on-first-use), so future syncs between remembered devices skip the
	// code. ConfigDir overrides the location of the identity/peers store (default:
	// the OS config dir); used in tests.
	Remember  bool
	ConfigDir string
	// Redact replaces likely secrets in the sessions this device sends with
	// placeholders before they leave the machine. AllowSecrets disables the
	// pre-egress secret gate (which otherwise refuses to send a session containing
	// a likely secret). Redact and the gate apply to outbound data only.
	Redact       bool
	AllowSecrets bool
}

// Result summarizes a sync from the local side's perspective.
type Result struct {
	PeerHost    string
	Sent        int
	Received    bundle.ImportResult
	DryRun      bool
	PreviewSend int
	PreviewRecv int
}

const (
	roleServer = "server"
	roleClient = "client"
)

// Per-phase network deadlines. They bound every stage so a stalled or hostile
// peer cannot hang the sync forever (an unauthenticated DoS); the transfer phase
// is generous because a legitimate bundle can be large on a slow link.
const (
	dialTimeout      = 10 * time.Second
	handshakeTimeout = 15 * time.Second
	authTimeout      = 30 * time.Second
	manifestTimeout  = 60 * time.Second
	transferTimeout  = 30 * time.Minute
)

// ExperimentalNotice is shown before every run because sync sends data off-machine.
const ExperimentalNotice = "EXPERIMENTAL: cct sync sends your sessions over the local network to a paired device.\n" +
	"It talks only to a peer you confirm with a one-time code, never to a server or the internet,\n" +
	"and refuses non-private addresses. Re-run with --i-understand to proceed."

// Serve waits for one peer to connect, authenticates it with a freshly generated
// pairing code, and syncs. home is the import target (for Claude, a codexhome.Home
// whose Root is the Claude home root).
func Serve(home codexhome.Home, opts Options) (Result, error) {
	if !opts.Confirmed {
		return Result{}, fmt.Errorf("%s", ExperimentalNotice)
	}
	cert, err := loadOrCreateIdentity(opts.ConfigDir)
	if err != nil {
		return Result{}, fmt.Errorf("load device identity: %w", err)
	}
	code, err := generatePairingCode()
	if err != nil {
		return Result{}, err
	}
	// Listen on raw TCP so the remote address is checked BEFORE the TLS handshake
	// (no public handshake) and so a per-connection deadline can bound each attempt.
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", opts.Port))
	if err != nil {
		return Result{}, fmt.Errorf("listen: %w", err)
	}
	defer ln.Close()

	_, port, _ := net.SplitHostPort(ln.Addr().String())
	printServeBanner(opts.Out, port, code)

	// Advertise a discovery beacon while waiting, so a peer can run `cct sync
	// connect` with no address (and the daemon can find us). The beacon carries
	// only non-secret coordinates; pairing still requires the code shown above.
	advCtx, stopAdv := context.WithCancel(context.Background())
	defer stopAdv()
	host, _ := os.Hostname()
	go Advertise(advCtx, Beacon{
		Hostname:    host,
		Tool:        string(agent.Normalize(opts.Tool)),
		Port:        atoiSafe(port),
		Fingerprint: fpHex(fingerprint(cert.Certificate[0])),
	})

	// Keep waiting for the right peer: a failed/stalled pre-auth attempt (a hostile
	// LAN host) is ignored rather than consuming the single sync, so it cannot DoS
	// the pairing window. Once a peer authenticates, that attempt is authoritative.
	for {
		raw, err := ln.Accept()
		if err != nil {
			return Result{}, fmt.Errorf("accept: %w", err)
		}
		if err := checkRemoteAddr(raw.RemoteAddr(), opts.AllowPublic); err != nil {
			fmt.Fprintf(opts.Out, "Rejected a connection: %v\n", err)
			raw.Close()
			continue
		}
		conn := tls.Server(raw, tlsConfigServer(cert))
		res, herr := handleConn(conn, home, opts, roleServer, code, cert)
		conn.Close()
		if herr == nil {
			return res, nil
		}
		if res.PeerHost != "" {
			// The peer authenticated but the sync failed afterwards — surface it.
			return res, herr
		}
		fmt.Fprintf(opts.Out, "Ignoring a failed pairing attempt; still waiting for your device…\n")
	}
}

// Connect dials a peer and syncs using the code the peer displayed.
func Connect(home codexhome.Home, opts Options, hostport string) (Result, error) {
	if !opts.Confirmed {
		return Result{}, fmt.Errorf("%s", ExperimentalNotice)
	}
	host, portStr, err := net.SplitHostPort(hostport)
	if err != nil {
		return Result{}, fmt.Errorf("address must be host:port: %w", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return Result{}, fmt.Errorf("invalid port %q: %w", portStr, err)
	}
	// Resolve once to a single allowed IP and dial THAT exact address, so a DNS
	// rebinding cannot pass the guard and then connect somewhere public.
	ip, err := resolveAllowedIP(host, opts.AllowPublic)
	if err != nil {
		return Result{}, err
	}
	cert, err := loadOrCreateIdentity(opts.ConfigDir)
	if err != nil {
		return Result{}, fmt.Errorf("load device identity: %w", err)
	}
	d := net.Dialer{Timeout: dialTimeout}
	raw, err := d.Dial("tcp", (&net.TCPAddr{IP: ip, Port: port}).String())
	if err != nil {
		return Result{}, fmt.Errorf("connect %s: %w", hostport, err)
	}
	defer raw.Close()
	// Belt-and-suspenders: re-check the actual TCP peer before the TLS handshake.
	if err := checkRemoteAddr(raw.RemoteAddr(), opts.AllowPublic); err != nil {
		return Result{}, err
	}
	conn := tls.Client(raw, tlsConfigClient(cert))
	defer conn.Close()
	return handleConn(conn, home, opts, roleClient, opts.Code, cert)
}

// handleConn runs the full authenticated sync over an established TLS connection.
func handleConn(conn *tls.Conn, home codexhome.Home, opts Options, role, code string, myCert tls.Certificate) (Result, error) {
	var result Result
	conn.SetDeadline(time.Now().Add(handshakeTimeout))
	if err := conn.Handshake(); err != nil {
		return result, fmt.Errorf("tls handshake: %w", err)
	}

	conn.SetDeadline(time.Now().Add(authTimeout))
	peer, peerDryRun, err := authenticate(conn, opts, role, code, myCert)
	if err != nil {
		return result, err
	}
	result.PeerHost = peer
	dryRun := opts.DryRun || peerDryRun
	result.DryRun = dryRun
	fmt.Fprintf(opts.Out, "Paired with %s.\n", safe(peer))

	// Build and exchange manifests (client sends first, then server).
	conn.SetDeadline(time.Now().Add(manifestTimeout))
	local, selErr := buildManifest(home, opts)
	if selErr != nil {
		return result, selErr
	}
	peerMani, err := exchangeManifests(conn, role, local)
	if err != nil {
		return result, err
	}
	if peerMani.Tool != local.Tool {
		return result, fmt.Errorf("peer is syncing %q sessions but this side is %q; both must use the same --tool", peerMani.Tool, local.Tool)
	}

	offer := computeOffer(local.Sessions, peerMani.Sessions)
	result.PreviewSend = len(offer)
	result.PreviewRecv = len(computeOffer(peerMani.Sessions, local.Sessions))

	if dryRun {
		printPreview(opts.Out, result)
		return result, nil
	}

	// Exchange bundles: client sends its offer first (server imports), then server
	// sends its offer (client imports). Strictly alternating to avoid deadlock.
	conn.SetDeadline(time.Now().Add(transferTimeout))
	if role == roleClient {
		sent, err := sendOffer(conn, home, opts, offer)
		if err != nil {
			return result, err
		}
		result.Sent = sent
		if result.Received, err = recvOffer(conn, home, opts); err != nil {
			return result, err
		}
	} else {
		if result.Received, err = recvOffer(conn, home, opts); err != nil {
			return result, err
		}
		sent, err := sendOffer(conn, home, opts, offer)
		if err != nil {
			return result, err
		}
		result.Sent = sent
	}
	return result, nil
}

// authenticate performs the mutual pairing-code confirmation bound to the TLS
// fingerprints, and exchanges identity (hostname/tool/version/dry-run).
func authenticate(conn *tls.Conn, opts Options, role, code string, myCert tls.Certificate) (peerHost string, peerDryRun bool, err error) {
	state := conn.ConnectionState()
	peerDER, err := peerLeaf(state)
	if err != nil {
		return "", false, err
	}
	peerFP := fingerprint(peerDER)
	myFP := fingerprint(myCert.Certificate[0])

	var serverFP, clientFP [32]byte
	if role == roleServer {
		serverFP, clientFP = myFP, peerFP
	} else {
		serverFP, clientFP = peerFP, myFP
	}
	peerRole := roleClient
	if role == roleClient {
		peerRole = roleServer
	}
	// A code is needed only when the peer is not already remembered. When we have a
	// code, send our confirmation MAC; otherwise rely on the pinned fingerprint.
	iKnowPeer := isKnownPeer(opts.ConfigDir, peerFP)
	var myMAC, wantPeerMAC []byte
	if code != "" {
		key := deriveKey(code)
		myMAC = confirmMAC(key, role, serverFP, clientFP)
		wantPeerMAC = confirmMAC(key, peerRole, serverFP, clientFP)
	}

	host, _ := os.Hostname()
	// DryRun travels in the auth message so a dry-run on either side keeps both
	// peers in lockstep (they agree on whether the bundle phase runs).
	mine := authMsg{Version: protocolVersion, Hostname: host, Tool: string(agent.Normalize(opts.Tool)), DryRun: opts.DryRun, Known: iKnowPeer, MAC: myMAC}

	send := func() error { return writeJSON(conn, mine) }
	recv := func() (authMsg, error) {
		var m authMsg
		err := readJSON(conn, &m)
		return m, err
	}

	var their authMsg
	if role == roleServer {
		if err := send(); err != nil {
			return "", false, err
		}
		if their, err = recv(); err != nil {
			return "", false, err
		}
	} else {
		if their, err = recv(); err != nil {
			return "", false, err
		}
		if err := send(); err != nil {
			return "", false, err
		}
	}
	if their.Version != protocolVersion {
		return "", false, fmt.Errorf("peer speaks protocol v%d, this is v%d; upgrade the older cct", their.Version, protocolVersion)
	}
	// Trusted path: both sides already remember each other's pinned fingerprint, so
	// the TLS mutual auth alone authenticates — no code needed.
	if iKnowPeer && their.Known {
		return their.Hostname, their.DryRun, nil
	}
	// Otherwise a code is required and its confirmation MAC must verify.
	if code == "" {
		return "", false, fmt.Errorf("this device isn't paired yet: run `cct sync serve` on the other device and enter its pairing code (add --remember to skip the code next time)")
	}
	if !verifyMAC(their.MAC, wantPeerMAC) {
		return "", false, fmt.Errorf("pairing failed: the code did not match, or the connection is being intercepted")
	}
	if opts.Remember {
		if err := rememberPeer(opts.ConfigDir, their.Hostname, peerFP); err != nil {
			return their.Hostname, their.DryRun, nil // remembering is best-effort
		}
	}
	return their.Hostname, their.DryRun, nil
}

// buildManifest scans the in-scope local sessions and computes their content
// hashes, mirroring what import compares against.
func buildManifest(home codexhome.Home, opts Options) (manifestMsg, error) {
	kind := agent.Normalize(opts.Tool)
	var scan sessions.ScanResult
	var err error
	if kind == agent.Claude {
		scan, err = claudesessions.Scan(opts.ClaudeHome, claudesessions.ScanOptions{})
	} else {
		scan, err = sessions.Scan(home, sessions.ScanOptions{DecompressCompressed: true})
	}
	if err != nil {
		return manifestMsg{}, fmt.Errorf("scan sessions: %w", err)
	}
	m := manifestMsg{Tool: string(kind)}
	for _, s := range scan.Sessions {
		if s.ThreadID == "" {
			continue // unparseable/compressed without an id: not syncable in v1
		}
		if opts.ProjectPath != "" && !pathEqual(s.CWD, opts.ProjectPath) {
			continue
		}
		sum, err := fileSHA(s.Path)
		if err != nil {
			continue
		}
		m.Sessions = append(m.Sessions, syncEntry{ThreadID: s.ThreadID, SHA256: sum, Size: s.SizeBytes})
	}
	return m, nil
}

func exchangeManifests(conn *tls.Conn, role string, local manifestMsg) (manifestMsg, error) {
	var peer manifestMsg
	if role == roleClient {
		if err := writeJSON(conn, local); err != nil {
			return peer, err
		}
		if err := readJSON(conn, &peer); err != nil {
			return peer, err
		}
	} else {
		if err := readJSON(conn, &peer); err != nil {
			return peer, err
		}
		if err := writeJSON(conn, local); err != nil {
			return peer, err
		}
	}
	return peer, nil
}

// computeOffer returns the thread ids in `mine` that the peer lacks or holds with
// different content (identical sessions are skipped). Whether a differing session
// is a clean append-only growth or a true conflict is decided when import applies
// it with --merge, exactly as for a file bundle.
func computeOffer(mine, peer []syncEntry) []string {
	peerSHA := make(map[string]string, len(peer))
	for _, e := range peer {
		peerSHA[e.ThreadID] = e.SHA256
	}
	var out []string
	for _, e := range mine {
		if sha, ok := peerSHA[e.ThreadID]; !ok || sha != e.SHA256 {
			out = append(out, e.ThreadID)
		}
	}
	return out
}

// sendOffer exports the offered sessions to a temp bundle and streams it. Pull-only
// suppresses sending (an empty blob).
func sendOffer(conn *tls.Conn, home codexhome.Home, opts Options, offer []string) (int, error) {
	if opts.PullOnly || len(offer) == 0 {
		return 0, writeBlob(conn, nil, 0)
	}
	tmp, err := os.CreateTemp("", "cct-sync-out-*.codexbundle")
	if err != nil {
		return 0, err
	}
	tmpName := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpName)

	res, err := bundle.Export(home, bundle.ExportOptions{
		Tool:          opts.Tool,
		ClaudeHome:    opts.ClaudeHome,
		OnlyThreadIDs: offer,
		OutputPath:    tmpName,
		Redact:        opts.Redact,
	})
	if err != nil {
		return 0, fmt.Errorf("bundle offered sessions: %w", err)
	}
	// Pre-egress secret gate: refuse to send sessions that still contain likely
	// secrets unless the user redacted them or explicitly allowed it. This is the
	// moment data leaves the machine, so it is the right place to stop a leak.
	if !opts.Redact && !opts.AllowSecrets {
		sres, serr := bundle.ScanBundleSecrets(tmpName)
		if serr == nil && sres.Any() {
			return 0, fmt.Errorf("refusing to send: %d session(s) contain %d likely secret(s); re-run with --redact to replace them with placeholders, or --allow-secrets to send anyway",
				sres.SessionsWithSecrets, sres.TotalFindings)
		}
	}
	f, err := os.Open(tmpName)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return 0, err
	}
	if err := writeBlob(conn, f, info.Size()); err != nil {
		return 0, err
	}
	return res.IncludedCount, nil
}

// recvOffer receives the peer's bundle and applies it through the safe import path
// with --merge. Push-only discards the received data without applying.
func recvOffer(conn *tls.Conn, home codexhome.Home, opts Options) (bundle.ImportResult, error) {
	tmp, err := os.CreateTemp("", "cct-sync-in-*.codexbundle")
	if err != nil {
		return bundle.ImportResult{}, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	n, err := readBlob(conn, tmp)
	tmp.Close()
	if err != nil {
		return bundle.ImportResult{}, fmt.Errorf("receive bundle: %w", err)
	}
	if n == 0 || opts.PushOnly {
		return bundle.ImportResult{}, nil
	}
	res, err := bundle.Import(home, bundle.ImportOptions{
		BundlePath: tmpName,
		Merge:      true,
		MapCWD:     opts.MapCWD,
		MapCWDHere: opts.MapCWDHere,
		HereDir:    opts.HereDir,
	})
	if err != nil {
		return bundle.ImportResult{}, fmt.Errorf("apply received sessions: %w", err)
	}
	return res, nil
}

func fileSHA(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// pathEqual compares two paths, case-insensitively on Windows.
func pathEqual(a, b string) bool {
	ca, cb := filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(ca, cb)
	}
	return ca == cb
}

// safe sanitizes a peer-supplied string (e.g. its hostname) before printing it to
// a terminal: it drops C0 AND C1 control characters (the latter can be interpreted
// as CSI/OSC by some terminals, enabling output spoofing or clipboard tricks) and
// caps the length so a hostile peer cannot flood the screen.
func safe(s string) string {
	const maxLen = 64
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			continue // C0 controls, DEL, and C1 controls
		}
		b.WriteRune(r)
		if b.Len() >= maxLen {
			break
		}
	}
	return b.String()
}
