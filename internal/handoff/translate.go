package handoff

import (
	"fmt"
	"path"

	"github.com/ahmojo/codex-claude-transfer/internal/claudehome"
	"github.com/ahmojo/codex-claude-transfer/internal/codexhome"
)

// Translation is the result of converting one session into another agent's
// format: the synthesized file bytes plus where they belong inside the target
// agent's home (a forward-slash path relative to the home root, matching the
// shapes the importer/safety layer already validate).
type Translation struct {
	TargetAgent string
	SessionID   string // deterministic id assigned in the target agent
	CWD         string
	RelPath     string // e.g. projects/<enc>/<id>.jsonl  or  sessions/Y/M/D/rollout-...jsonl
	Content     []byte
	Turns       int // conversation turns carried (excluding the preamble)
}

// Translate converts a source agent's session bytes into the target agent's
// format. sourceAgent/targetAgent are "codex" or "claude". It returns the
// synthesized session and its destination path within the target home.
//
// The output is deterministic (stable id and timestamps derived from the source),
// so re-translating the same session yields byte-identical bytes and re-import is
// an idempotent skip rather than a duplicate.
func Translate(sourceBytes []byte, sourceAgent, targetAgent string) (Translation, error) {
	if sourceAgent == targetAgent {
		return Translation{}, fmt.Errorf("source and target agent are both %q; nothing to translate", sourceAgent)
	}

	var ir AgentSession
	var err error
	switch sourceAgent {
	case "codex":
		ir, err = FromCodexBytes(sourceBytes)
	case "claude":
		ir, err = FromClaudeBytes(sourceBytes)
	default:
		return Translation{}, fmt.Errorf("unknown source agent %q", sourceAgent)
	}
	if err != nil {
		return Translation{}, err
	}
	if len(ir.Conversation) == 0 {
		return Translation{}, fmt.Errorf("no conversation could be recovered to translate")
	}

	t := Translation{TargetAgent: targetAgent, CWD: ir.CWD, Turns: len(ir.Conversation)}
	switch targetAgent {
	case "claude":
		if ir.CWD == "" {
			return Translation{}, fmt.Errorf("source session has no recorded cwd; cannot place a Claude transcript")
		}
		content, id, err := ToClaude(ir)
		if err != nil {
			return Translation{}, err
		}
		t.Content, t.SessionID = content, id
		t.RelPath = path.Join(claudehome.ProjectsSubdir, claudehome.EncodeCWD(ir.CWD), id+claudehome.SessionExt)
	case "codex":
		content, id, err := ToCodex(ir)
		if err != nil {
			return Translation{}, err
		}
		t.Content, t.SessionID = content, id
		base := baseTime(ir)
		day := base.Format("2006/01/02")
		fname := codexhome.RolloutName(base, id)
		t.RelPath = path.Join(codexhome.SessionsSubdir, day, fname)
	default:
		return Translation{}, fmt.Errorf("unknown target agent %q", targetAgent)
	}
	return t, nil
}
