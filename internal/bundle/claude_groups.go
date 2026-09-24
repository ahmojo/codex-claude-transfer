package bundle

import (
	"fmt"
	"github.com/ahmojo/codex-claude-transfer/internal/safety"
	"github.com/ahmojo/codex-claude-transfer/internal/sessions"
	"strings"
)

func claudeGroup(s sessions.Session) string {
	group, _, _, _ := safety.ClaudeSessionGroup("projects/" + s.RelPath)
	return group
}

func expandClaudeGroups(all, selected []sessions.Session) []sessions.Session {
	groups := map[string]bool{}
	for _, s := range selected {
		if g := claudeGroup(s); g != "" {
			groups[g] = true
		}
	}
	var out []sessions.Session
	for _, s := range all {
		if groups[claudeGroup(s)] {
			out = append(out, s)
		}
	}
	return out
}

func selectClaudeGroup(all []sessions.Session, id string) ([]sessions.Session, error) {
	if id == "" {
		return nil, fmt.Errorf("empty thread id")
	}
	exact, prefix := map[string]bool{}, map[string]bool{}
	for _, s := range all {
		g := claudeGroup(s)
		if g == "" {
			continue
		}
		if s.ThreadID == id {
			exact[g] = true
		} else if strings.HasPrefix(s.ThreadID, id) {
			prefix[g] = true
		}
	}
	matches := exact
	if len(matches) == 0 {
		matches = prefix
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no session matches thread id %q", id)
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("thread id %q is ambiguous: it matches %d conversations", id, len(matches))
	}
	var out []sessions.Session
	for _, s := range all {
		if matches[claudeGroup(s)] {
			out = append(out, s)
		}
	}
	return out, nil
}
