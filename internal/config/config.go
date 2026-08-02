// Package config stores a few user defaults so power users stop retyping the
// same flags. It is a small JSON file (config.json) under cct's config dir; it
// holds only non-secret preferences (which agent to act on, where the homes are,
// a default app port) and never anything sensitive. Every value it provides is
// just a default: an explicit flag always wins, so the file can be absent.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/ahmojo/codex-claude-transfer/internal/agent"
	"github.com/ahmojo/codex-claude-transfer/internal/git"
)

// FileName is the config file's name within the config dir.
const FileName = "config.json"

// Config is the set of user defaults. Empty fields mean "no default set".
type Config struct {
	Tool       string `json:"tool,omitempty"`        // codex | claude
	CodexHome  string `json:"codex_home,omitempty"`  // default --codex-home
	ClaudeHome string `json:"claude_home,omitempty"` // default --claude-home
	Port       int    `json:"port,omitempty"`        // default app port
	// RepoSync records how the cct-session-sync workflow commits a bundle into
	// a project's own repository: as plaintext or age-encrypted. It is the
	// answer to a one-time setup question, not a secret.
	RepoSync string `json:"repo_sync,omitempty"` // plain | encrypted
	// RepoSyncRecipient is the age recipient that workflow encrypts to. A
	// recipient is a public key: it can encrypt, never decrypt.
	RepoSyncRecipient string `json:"repo_sync_recipient,omitempty"`
	// RepoSyncRepo is the git remote of a separate, private session store
	// repository. When set, projects keep only a reference file and the
	// bundles live there; when empty, a project's bundle stays in its own repo.
	RepoSyncRepo string `json:"repo_sync_repo,omitempty"`
	// RepoSyncDir is where that store is cloned on this machine. It is
	// per-machine on purpose and never committed to a project.
	RepoSyncDir string `json:"repo_sync_dir,omitempty"`
}

// Keys lists the settable keys in a stable, display order.
var Keys = []string{"tool", "codex-home", "claude-home", "port", "repo-sync",
	"repo-sync-recipient", "repo-sync-repo", "repo-sync-dir"}

// RepoSync modes.
const (
	RepoSyncPlain     = "plain"
	RepoSyncEncrypted = "encrypted"
)

// FilePath returns the config file path within dir.
func FilePath(dir string) string { return filepath.Join(dir, FileName) }

// Load reads the config from dir. A missing file is not an error: it yields a
// zero Config so callers can treat "no config" and "empty config" alike.
func Load(dir string) (Config, error) {
	var c Config
	data, err := os.ReadFile(FilePath(dir))
	if err != nil {
		if os.IsNotExist(err) {
			return c, nil
		}
		return c, err
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return c, fmt.Errorf("config %s is not valid JSON: %w", FilePath(dir), err)
	}
	return c, nil
}

// Save writes the config to dir (0600), creating the directory if needed.
func (c Config) Save(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(FilePath(dir), append(data, '\n'), 0o600)
}

// Set validates and applies a key/value pair. An empty value clears the key.
func (c *Config) Set(key, value string) error {
	switch key {
	case "tool":
		if value == "" {
			c.Tool = ""
			return nil
		}
		k, err := agent.Parse(value)
		if err != nil {
			return err
		}
		c.Tool = string(k)
	case "codex-home":
		c.CodexHome = value
	case "claude-home":
		c.ClaudeHome = value
	case "port":
		if value == "" {
			c.Port = 0
			return nil
		}
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 || n > 65535 {
			return fmt.Errorf("invalid port %q (want 0-65535)", value)
		}
		c.Port = n
	case "repo-sync":
		if value != "" && value != RepoSyncPlain && value != RepoSyncEncrypted {
			return fmt.Errorf("invalid repo-sync %q (want %s or %s)", value, RepoSyncPlain, RepoSyncEncrypted)
		}
		c.RepoSync = value
	case "repo-sync-recipient":
		if value != "" {
			if err := validRecipient(value); err != nil {
				return err
			}
		}
		c.RepoSyncRecipient = value
	case "repo-sync-repo":
		if value != "" {
			// The same rule import --clone applies: no remote-helper transports,
			// nothing that looks like a flag.
			if err := git.ValidateRemoteURL(value); err != nil {
				return err
			}
			if strings.ContainsAny(value, "\r\n") {
				return fmt.Errorf("the repo URL contains a line break")
			}
		}
		c.RepoSyncRepo = value
	case "repo-sync-dir":
		c.RepoSyncDir = value
	default:
		return fmt.Errorf("unknown config key %q (known: %v)", key, Keys)
	}
	return nil
}

// Get returns the string form of a key's value (empty when unset).
func (c Config) Get(key string) (string, error) {
	switch key {
	case "tool":
		return c.Tool, nil
	case "codex-home":
		return c.CodexHome, nil
	case "claude-home":
		return c.ClaudeHome, nil
	case "port":
		if c.Port == 0 {
			return "", nil
		}
		return strconv.Itoa(c.Port), nil
	case "repo-sync":
		return c.RepoSync, nil
	case "repo-sync-recipient":
		return c.RepoSyncRecipient, nil
	case "repo-sync-repo":
		return c.RepoSyncRepo, nil
	case "repo-sync-dir":
		return c.RepoSyncDir, nil
	default:
		return "", fmt.Errorf("unknown config key %q (known: %v)", key, Keys)
	}
}

// validRecipient rejects anything that is obviously not an age recipient, so a
// typo (or a pasted private key) is caught here rather than by age much later.
// It deliberately does not try to validate the key material itself.
func validRecipient(value string) error {
	if strings.HasPrefix(value, "AGE-SECRET-KEY-") {
		return fmt.Errorf("that is an age private key; repo-sync-recipient takes the public recipient (age1...)")
	}
	if !strings.HasPrefix(value, "age1") && !strings.HasPrefix(value, "ssh-") {
		return fmt.Errorf("invalid recipient %q (want an age recipient: age1... or ssh-ed25519 ...)", value)
	}
	if len(value) > 1024 {
		return fmt.Errorf("recipient is too long (%d bytes)", len(value))
	}
	for _, r := range value {
		if r == '\n' || r == '\r' || r == 0 {
			return fmt.Errorf("recipient contains a line break or NUL byte")
		}
	}
	return nil
}

// Entries returns the configured key/value pairs in Keys order, omitting unset
// keys, for display by `cct config list`.
func (c Config) Entries() [][2]string {
	var out [][2]string
	for _, k := range Keys {
		v, err := c.Get(k)
		if err != nil || v == "" {
			continue
		}
		out = append(out, [2]string{k, v})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out
}
