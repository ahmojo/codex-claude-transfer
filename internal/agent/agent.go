// Package agent names the coding agents whose local sessions cct can transfer.
//
// cct started as a Codex-only tool and is being extended to Claude Code. The two
// agents store sessions very differently (Codex: ~/.codex/sessions/YYYY/MM/DD/
// rollout-*.jsonl; Claude Code: ~/.claude/projects/<encoded-cwd>/<uuid>.jsonl),
// but the portable export -> move -> import model, the bundle/manifest/checksum
// machinery, and the safety guarantees are identical. This package provides the
// small shared vocabulary (which agent a bundle or command targets) that lets the
// reusable core stay agent-agnostic while the per-agent specifics live in the
// codexhome/sessions and claudehome/claudesessions packages.
package agent

import "fmt"

// Kind identifies a coding agent.
type Kind string

const (
	// Codex is OpenAI's Codex CLI (~/.codex). This is the default for bundles
	// that predate the multi-agent work and do not record a tool.
	Codex Kind = "codex"
	// Claude is Anthropic's Claude Code (~/.claude).
	Claude Kind = "claude"
)

// Parse converts a user-supplied tool name into a Kind. It is tolerant of case
// and a couple of common aliases.
func Parse(s string) (Kind, error) {
	switch s {
	case "codex", "Codex", "CODEX":
		return Codex, nil
	case "claude", "Claude", "CLAUDE", "claude-code", "claudecode":
		return Claude, nil
	default:
		return "", fmt.Errorf("unknown tool %q (expected \"codex\" or \"claude\")", s)
	}
}

// Normalize returns the Kind a manifest or option should be treated as. An empty
// value means a legacy (pre-multi-agent) bundle, which is always Codex.
func Normalize(k Kind) Kind {
	if k == "" {
		return Codex
	}
	return k
}

// Label is a human-readable name for messages and headers.
func (k Kind) Label() string {
	switch Normalize(k) {
	case Claude:
		return "Claude Code"
	default:
		return "Codex"
	}
}

// String implements fmt.Stringer, returning the canonical lowercase token.
func (k Kind) String() string { return string(Normalize(k)) }
