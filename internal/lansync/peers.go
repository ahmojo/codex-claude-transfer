package lansync

import (
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Persistent identity + remembered peers (trust-on-first-use). After the first
// code-authenticated pairing with --remember, each side stores the other's stable
// certificate fingerprint; later syncs between mutually-remembered devices skip the
// code. The store lives under the OS config dir, NEVER inside an agent home.

func cctConfigDir(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "cct"), nil
}

// loadOrCreateIdentity returns this device's stable TLS identity, generating and
// persisting one (0600) on first use so its certificate fingerprint stays constant
// across runs — the basis for remembering a peer.
func loadOrCreateIdentity(configDir string) (tls.Certificate, error) {
	dir, err := cctConfigDir(configDir)
	if err != nil {
		return tls.Certificate{}, err
	}
	path := filepath.Join(dir, "identity.pem")
	if data, err := os.ReadFile(path); err == nil {
		return parseIdentityPEM(data)
	}
	cert, err := generateCert()
	if err != nil {
		return tls.Certificate{}, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return tls.Certificate{}, err
	}
	pemBytes, err := identityToPEM(cert)
	if err != nil {
		return tls.Certificate{}, err
	}
	if err := os.WriteFile(path, pemBytes, 0o600); err != nil {
		return tls.Certificate{}, err
	}
	return cert, nil
}

func identityToPEM(cert tls.Certificate) ([]byte, error) {
	key, ok := cert.PrivateKey.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("unexpected private key type")
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	out := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]})
	out = append(out, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})...)
	return out, nil
}

func parseIdentityPEM(data []byte) (tls.Certificate, error) {
	var certDER, keyDER []byte
	for {
		blk, rest := pem.Decode(data)
		if blk == nil {
			break
		}
		switch blk.Type {
		case "CERTIFICATE":
			certDER = blk.Bytes
		case "EC PRIVATE KEY":
			keyDER = blk.Bytes
		}
		data = rest
	}
	if certDER == nil || keyDER == nil {
		return tls.Certificate{}, fmt.Errorf("identity file is incomplete")
	}
	key, err := x509.ParseECPrivateKey(keyDER)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{certDER}, PrivateKey: key}, nil
}

type knownPeer struct {
	Fingerprint string `json:"fingerprint"`
	Label       string `json:"label"`
	Added       string `json:"added"`
}

func peersPath(configDir string) (string, error) {
	dir, err := cctConfigDir(configDir)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "peers.json"), nil
}

func loadPeers(configDir string) map[string]knownPeer {
	path, err := peersPath(configDir)
	if err != nil {
		return map[string]knownPeer{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]knownPeer{}
	}
	var list []knownPeer
	if json.Unmarshal(data, &list) != nil {
		return map[string]knownPeer{}
	}
	m := make(map[string]knownPeer, len(list))
	for _, p := range list {
		m[p.Fingerprint] = p
	}
	return m
}

func isKnownPeer(configDir string, fp [32]byte) bool {
	_, ok := loadPeers(configDir)[fpHex(fp)]
	return ok
}

func rememberPeer(configDir, label string, fp [32]byte) error {
	dir, err := cctConfigDir(configDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	m := loadPeers(configDir)
	m[fpHex(fp)] = knownPeer{Fingerprint: fpHex(fp), Label: label, Added: time.Now().UTC().Format(time.RFC3339)}
	list := make([]knownPeer, 0, len(m))
	for _, p := range m {
		list = append(list, p)
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	path, _ := peersPath(configDir)
	return os.WriteFile(path, data, 0o600)
}

func fpHex(fp [32]byte) string { return hex.EncodeToString(fp[:]) }
