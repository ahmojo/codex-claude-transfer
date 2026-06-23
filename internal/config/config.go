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

	"github.com/ahmojo/codex-claude-transfer/internal/agent"
)

// FileName is the config file's name within the config dir.
const FileName = "config.json"

// Config is the set of user defaults. Empty fields mean "no default set".
type Config struct {
	Tool       string `json:"tool,omitempty"`        // codex | claude
	CodexHome  string `json:"codex_home,omitempty"`  // default --codex-home
	ClaudeHome string `json:"claude_home,omitempty"` // default --claude-home
	Port       int    `json:"port,omitempty"`        // default app port
}

// Keys lists the settable keys in a stable, display order.
var Keys = []string{"tool", "codex-home", "claude-home", "port"}

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
	default:
		return "", fmt.Errorf("unknown config key %q (known: %v)", key, Keys)
	}
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
