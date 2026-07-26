package codexreconcile

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

var threadIDRE = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// ValidThreadID reports whether id is a canonical UUID-shaped Codex thread ID.
// Rollout session_meta is bundle-controlled input, so callers must validate it
// before sending it to Codex or including it in copy/paste shell guidance.
func ValidThreadID(id string) bool {
	return threadIDRE.MatchString(id)
}

// ResumeFallbackCommand builds a copy/paste resume command only when both
// bundle-derived and environment-derived values are safe to embed. The command
// deliberately uses a conservative cross-shell allowlist: when quoting would
// differ between sh, PowerShell, and cmd.exe, omitting the command and telling
// the user to restart Codex is safer than guessing their shell.
func ResumeFallbackCommand(threadID, codexHome string) (string, bool) {
	if !ValidThreadID(threadID) || !safeQuotedShellValue(codexHome) {
		return "", false
	}
	return fmt.Sprintf(`cct resume %s --run --codex-home "%s"`, threadID, codexHome), true
}

// ResumeFallbackCommands returns up to limit safe commands, skipping any
// untrusted values that cannot be represented safely.
func ResumeFallbackCommands(threadIDs []string, codexHome string, limit int) []string {
	if limit <= 0 {
		return nil
	}
	commands := make([]string, 0, limit)
	for _, id := range threadIDs {
		command, ok := ResumeFallbackCommand(id, codexHome)
		if !ok {
			continue
		}
		commands = append(commands, command)
		if len(commands) == limit {
			break
		}
	}
	return commands
}

func safeQuotedShellValue(value string) bool {
	// In POSIX double quotes, \\ is decoded to \. Reject it so the same
	// rendered command preserves the argument in sh, PowerShell, and cmd.exe.
	if value == "" || strings.HasSuffix(value, `\`) || strings.Contains(value, `\\`) {
		return false
	}
	const shellMetacharacters = "\"'`$;&|<>()%!^"
	for _, r := range value {
		if unicode.IsControl(r) || strings.ContainsRune(shellMetacharacters, r) {
			return false
		}
	}
	return true
}
