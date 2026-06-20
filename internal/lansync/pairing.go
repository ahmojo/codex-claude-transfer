// Package lansync implements cct's experimental device-to-device session sync over
// a local network. It is the project's only feature that sends session data off
// the machine, so it is strictly opt-in, peer-to-peer (no server/relay/cloud),
// refuses non-private addresses by default, and authenticates the peer with a
// high-entropy pairing code before any data moves.
//
// Security model (stdlib only, no PAKE/mDNS dependency):
//   - The channel is TLS with per-process self-signed certificates (mutual auth).
//     Trust is NOT delegated to a CA.
//   - The pairing code is a freshly generated ~96-bit secret shown by the serving
//     side. Both sides derive a key from it and exchange an HMAC confirmation
//     bound to BOTH TLS certificate fingerprints (channel binding). A man-in-the-
//     middle terminates TLS separately with each side, so the fingerprints it
//     presents differ from the real ones; without the code it cannot forge the
//     confirmation, and the code's entropy makes offline guessing infeasible.
//
// The actual session transfer reuses the existing, tested bundle export/import
// (+ --merge) path verbatim, so all checksum/manifest/conflict/mtime guarantees
// carry over unchanged.
package lansync

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base32"
	"fmt"
	"math/big"
	"strings"
	"time"
)

const (
	pairingInfo  = "cct-sync-pairing-v1"
	codeRawBytes = 12 // 96 bits of entropy in the pairing code
)

// generateCert creates an ephemeral self-signed P-256 certificate used for one
// process's TLS identity (as both server and client).
func generateCert() (tls.Certificate, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, err
	}
	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "cct-sync"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: priv}, nil
}

// generatePairingCode returns a fresh, human-typable high-entropy code, grouped
// in fours for readability (e.g. "K3Q2-7F9A-...").
func generatePairingCode() (string, error) {
	raw := make([]byte, codeRawBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	enc := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)
	var b strings.Builder
	for i, r := range enc {
		if i > 0 && i%4 == 0 {
			b.WriteByte('-')
		}
		b.WriteRune(r)
	}
	return b.String(), nil
}

// canonicalizeCode strips formatting (spaces, hyphens) and upper-cases the code so
// the two sides derive the same key regardless of how it was typed.
func canonicalizeCode(code string) string {
	var b strings.Builder
	for _, r := range code {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - 32)
		case (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
		}
	}
	return b.String()
}

// deriveKey turns a pairing code into a 32-byte channel key.
func deriveKey(code string) []byte {
	mac := hmac.New(sha256.New, []byte(canonicalizeCode(code)))
	mac.Write([]byte(pairingInfo))
	return mac.Sum(nil)
}

// fingerprint is the SHA-256 of a certificate's DER bytes.
func fingerprint(der []byte) [32]byte { return sha256.Sum256(der) }

// confirmMAC binds the pairing key to both certificate fingerprints and a role
// label, so each side proves it knows the code over the exact TLS channel in use.
func confirmMAC(key []byte, role string, serverFP, clientFP [32]byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(role))
	mac.Write(serverFP[:])
	mac.Write(clientFP[:])
	return mac.Sum(nil)
}

// verifyMAC is a constant-time comparison of two MACs.
func verifyMAC(a, b []byte) bool { return hmac.Equal(a, b) }

// tlsConfigServer / tlsConfigClient build the mutual-auth TLS configs. The server
// requests (but does not CA-verify) a client cert; the client skips CA
// verification of the server. Authentication is provided entirely by the pairing
// MAC over the captured fingerprints, not by the PKI.
func tlsConfigServer(cert tls.Certificate) *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAnyClientCert,
		MinVersion:   tls.VersionTLS12,
	}
}

func tlsConfigClient(cert tls.Certificate) *tls.Config {
	return &tls.Config{
		Certificates:       []tls.Certificate{cert},
		InsecureSkipVerify: true, // intentional: trust is pinned via the pairing MAC
		MinVersion:         tls.VersionTLS12,
	}
}

// peerLeaf returns the peer's leaf certificate DER from a completed handshake.
func peerLeaf(state tls.ConnectionState) ([]byte, error) {
	if len(state.PeerCertificates) == 0 {
		return nil, fmt.Errorf("peer presented no certificate")
	}
	return state.PeerCertificates[0].Raw, nil
}
