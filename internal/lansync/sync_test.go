package lansync

import (
	"crypto/tls"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/ahmojo/codex-claude-transfer/internal/agent"
	"github.com/ahmojo/codex-claude-transfer/internal/bundle"
	"github.com/ahmojo/codex-claude-transfer/internal/codexhome"
)

const testCode = "TEST-CODE-ABCD-2345"

func fakeHome(t *testing.T) codexhome.Home {
	t.Helper()
	h, err := codexhome.Detect(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// writeSession writes a rollout file with the given thread id and the given event
// message lines (one per extra arg), so callers can make one home's copy longer
// (an append-only growth) than another's.
func writeSession(t *testing.T, home codexhome.Home, id, cwd string, msgs ...string) string {
	t.Helper()
	dir := filepath.Join(home.SessionsDir, "2026", "06", "13")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"timestamp":"2026-06-13T18:22:01Z","type":"session_meta","payload":{"id":"` + id + `","cwd":"` + cwd + `","source":"cli","model_provider":"openai"}}` + "\n"
	for i, m := range msgs {
		body += `{"timestamp":"2026-06-13T18:2` + string(rune('2'+i)) + `:01Z","type":"event_msg","payload":{"type":"user_message","message":"` + m + `"}}` + "\n"
	}
	p := filepath.Join(dir, "rollout-2026-06-13T18-22-01-"+id+".jsonl")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func runPair(t *testing.T, homeA, homeB codexhome.Home, optsA, optsB Options, code string) (Result, Result) {
	t.Helper()
	serverCert, err := generateCert()
	if err != nil {
		t.Fatal(err)
	}
	clientCert, err := generateCert()
	if err != nil {
		t.Fatal(err)
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", tlsConfigServer(serverCert))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	type out struct {
		r   Result
		err error
	}
	sch := make(chan out, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			sch <- out{err: err}
			return
		}
		defer conn.Close()
		r, err := handleConn(conn.(*tls.Conn), homeB, optsB, roleServer, code, serverCert)
		sch <- out{r, err}
	}()

	conn, err := tls.Dial("tcp", ln.Addr().String(), tlsConfigClient(clientCert))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	cr, cerr := handleConn(conn, homeA, optsA, roleClient, code, clientCert)
	s := <-sch
	if cerr != nil {
		t.Fatalf("client handleConn: %v", cerr)
	}
	if s.err != nil {
		t.Fatalf("server handleConn: %v", s.err)
	}
	return cr, s.r
}

// TestSyncBidirectional: A and B each have a unique session, share an identical
// one, and disagree on a third where A's copy has grown. After sync both
// converge — the unique sessions cross over and the grown one updates.
func TestSyncBidirectional(t *testing.T) {
	a := fakeHome(t)
	b := fakeHome(t)
	const cwd = "/proj/x"

	onlyA := "aaaa1111-2222-3333-4444-555566667777"
	onlyB := "bbbb1111-2222-3333-4444-555566667777"
	shared := "cccc1111-2222-3333-4444-555566667777"
	grown := "dddd1111-2222-3333-4444-555566667777"

	writeSession(t, a, onlyA, cwd, "from A")
	writeSession(t, b, onlyB, cwd, "from B")
	writeSession(t, a, shared, cwd, "same")
	writeSession(t, b, shared, cwd, "same")
	// A's copy of `grown` has an extra message (append-only growth over B's).
	writeSession(t, a, grown, cwd, "turn 1", "turn 2")
	writeSession(t, b, grown, cwd, "turn 1")

	opts := Options{Tool: agent.Codex, Out: io.Discard}
	cr, sr := runPair(t, a, b, opts, opts, testCode)

	if cr.PeerHost == "" || sr.PeerHost == "" {
		t.Errorf("pairing did not record peer hostnames: client=%q server=%q", cr.PeerHost, sr.PeerHost)
	}

	// B should now have A's unique session.
	if !sessionExists(b, onlyA) {
		t.Errorf("server did not receive A-only session %s", onlyA)
	}
	// A should now have B's unique session.
	if !sessionExists(a, onlyB) {
		t.Errorf("client did not receive B-only session %s", onlyB)
	}
	// B's grown session should now match A's longer copy.
	if got, want := readSession(t, b, grown), readSession(t, a, grown); got != want {
		t.Errorf("grown session did not converge on B:\n got=%q\nwant=%q", got, want)
	}
	// The identical shared session must not be a conflict on either side.
	if cr.Received.Conflicts != 0 || sr.Received.Conflicts != 0 {
		t.Errorf("unexpected conflicts: client=%d server=%d", cr.Received.Conflicts, sr.Received.Conflicts)
	}
}

// TestSyncDryRunWritesNothing: a dry run on the client keeps both sides from
// writing, and still reports a non-zero preview.
func TestSyncDryRunWritesNothing(t *testing.T) {
	a := fakeHome(t)
	b := fakeHome(t)
	writeSession(t, a, "aaaa1111-2222-3333-4444-555566667777", "/p", "from A")

	optsA := Options{Tool: agent.Codex, Out: io.Discard, DryRun: true}
	optsB := Options{Tool: agent.Codex, Out: io.Discard}
	cr, _ := runPair(t, a, b, optsA, optsB, testCode)

	if cr.PreviewSend != 1 {
		t.Errorf("preview send = %d, want 1", cr.PreviewSend)
	}
	if sessionExists(b, "aaaa1111-2222-3333-4444-555566667777") {
		t.Error("dry run wrote a session to the server")
	}
}

// TestSyncWrongCodeFails: a mismatched pairing code aborts authentication.
func TestSyncWrongCodeFails(t *testing.T) {
	a := fakeHome(t)
	b := fakeHome(t)
	serverCert, _ := generateCert()
	clientCert, _ := generateCert()
	ln, err := tls.Listen("tcp", "127.0.0.1:0", tlsConfigServer(serverCert))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = handleConn(conn.(*tls.Conn), b, Options{Tool: agent.Codex, Out: io.Discard}, roleServer, "RIGHT-CODE", serverCert)
	}()
	conn, err := tls.Dial("tcp", ln.Addr().String(), tlsConfigClient(clientCert))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, cerr := handleConn(conn, a, Options{Tool: agent.Codex, Out: io.Discard}, roleClient, "WRONG-CODE", clientCert)
	if cerr == nil {
		t.Fatal("expected pairing to fail with a wrong code")
	}
}

func sessionExists(home codexhome.Home, id string) bool {
	p := filepath.Join(home.SessionsDir, "2026", "06", "13", "rollout-2026-06-13T18-22-01-"+id+".jsonl")
	_, err := os.Stat(p)
	return err == nil
}

func readSession(t *testing.T, home codexhome.Home, id string) string {
	t.Helper()
	p := filepath.Join(home.SessionsDir, "2026", "06", "13", "rollout-2026-06-13T18-22-01-"+id+".jsonl")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", id, err)
	}
	return string(b)
}

// keep the bundle import type referenced (helps catch signature drift in CI).
var _ = bundle.ImportResult{}
