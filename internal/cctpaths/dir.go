// Package cctpaths resolves cct's own on-disk locations, kept deliberately
// separate from any coding-agent home. cct keeps a small amount of cross-cutting
// state of its own — config.json (user defaults), meta.json (session tags and
// names), and the LAN-sync identity/peer store — and that state lives under the
// OS config directory, NEVER inside ~/.codex or ~/.claude.
package cctpaths

import (
	"os"
	"path/filepath"
)

// ConfigDirEnv lets tests (and power users) point cct's config directory
// somewhere hermetic without touching the real one.
const ConfigDirEnv = "CCT_CONFIG_DIR"

// ConfigDir returns cct's config directory. An explicit override wins (used by
// callers that already resolved a location, e.g. lansync options and tests);
// otherwise $CCT_CONFIG_DIR is honored, and finally the OS user config dir's
// cct/ subdirectory is used. The directory is not created here.
func ConfigDir(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	if env := os.Getenv(ConfigDirEnv); env != "" {
		return env, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "cct"), nil
}
