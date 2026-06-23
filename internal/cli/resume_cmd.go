package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/ahmojo/codex-claude-transfer/internal/agent"
	"github.com/ahmojo/codex-claude-transfer/internal/search"
	"github.com/ahmojo/codex-claude-transfer/internal/sessions"
)

const resumeUsage = `usage: cct resume [query] [--run] [--tool codex|claude] [--regex]
  With no query, picks your most recently updated session.
  With a query, finds the best matching session by thread-id prefix or by
  conversation text. Prints the command to continue it in the agent; add --run
  to launch the agent on it directly.`

// runResume bridges "find a past session" to "continue it": it locates the best
// matching session and either prints (default) or runs the agent's resume command.
func runResume(args []string, stdout, stderr io.Writer) int {
	f, err := parseFlags(args)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	kind, ok := resolveTool(f, stderr)
	if !ok {
		return 2
	}
	scan, code := scanForSearch(f, kind, stderr)
	if code != 0 {
		return code
	}
	candidates := filterForSearch(f, scan.Sessions, stderr)

	query := strings.TrimSpace(strings.Join(f.positional, " "))
	chosen, err := pickResumeSession(candidates, query, f)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	name, cmdArgs := resumeCommand(kind, chosen.ThreadID)
	fmt.Fprintf(stdout, "Session: %s\n", safeTerminal(title(chosen)))
	fmt.Fprintf(stdout, "Thread:  %s\n", chosen.ThreadID)
	if chosen.CWD != "" {
		fmt.Fprintf(stdout, "Project: %s\n", safeTerminal(chosen.CWD))
	}
	fmt.Fprintf(stdout, "Updated: %s\n\n", chosen.UpdatedAt().Format("2006-01-02 15:04"))

	cmdline := name + " " + strings.Join(cmdArgs, " ")
	if !f.run {
		fmt.Fprintf(stdout, "To continue this session, run:\n  %s\n\n", cmdline)
		fmt.Fprintln(stdout, "(re-run with --run to launch it now)")
		return 0
	}

	bin, lookErr := exec.LookPath(name)
	if lookErr != nil {
		fmt.Fprintf(stderr, "error: %q is not installed or not on PATH; run it yourself:\n  %s\n", name, cmdline)
		return 1
	}
	fmt.Fprintf(stdout, "Launching: %s\n", cmdline)
	// Hand off to the agent with the real terminal streams: this is an interactive
	// relaunch, so it inherits stdin/stdout/stderr directly.
	c := exec.Command(bin, cmdArgs...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	if chosen.CWD != "" {
		if st, statErr := os.Stat(chosen.CWD); statErr == nil && st.IsDir() {
			c.Dir = chosen.CWD
		}
	}
	if err := c.Run(); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

// resumeCommand returns the agent binary and arguments that resume a session by
// thread id. It is pure so it can be unit-tested without launching anything.
func resumeCommand(kind agent.Kind, threadID string) (name string, args []string) {
	switch agent.Normalize(kind) {
	case agent.Claude:
		return "claude", []string{"--resume", threadID}
	default:
		return "codex", []string{"resume", threadID}
	}
}

// pickResumeSession selects the single session to resume. With no query it is the
// most recently updated session; with a query it prefers an exact/unique thread-id
// prefix, then falls back to the top-ranked full-text match.
func pickResumeSession(list []sessions.Session, query string, f commonFlags) (sessions.Session, error) {
	withID := make([]sessions.Session, 0, len(list))
	for _, s := range list {
		if s.ThreadID != "" {
			withID = append(withID, s)
		}
	}
	if len(withID) == 0 {
		return sessions.Session{}, fmt.Errorf("no resumable sessions found (none have a thread id)")
	}

	if query == "" {
		return mostRecent(withID), nil
	}

	// Thread-id prefix wins when it identifies exactly one session.
	var prefix []sessions.Session
	for _, s := range withID {
		if s.ThreadID == query {
			return s, nil
		}
		if strings.HasPrefix(s.ThreadID, query) {
			prefix = append(prefix, s)
		}
	}
	if len(prefix) == 1 {
		return prefix[0], nil
	}

	// Otherwise rank by conversation-text relevance.
	matches, err := search.Search(withID, search.Query{Text: query, Regex: f.regex, CaseSensitive: f.caseSensitive})
	if err != nil {
		return sessions.Session{}, err
	}
	if len(matches) == 0 {
		if len(prefix) > 1 {
			return sessions.Session{}, fmt.Errorf("%q matches %d sessions by id; use a longer prefix or run `cct browse`", query, len(prefix))
		}
		return sessions.Session{}, fmt.Errorf("no session matched %q", query)
	}
	return matches[0].Session, nil
}

func mostRecent(list []sessions.Session) sessions.Session {
	sorted := make([]sessions.Session, len(list))
	copy(sorted, list)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].UpdatedAt().After(sorted[j].UpdatedAt())
	})
	return sorted[0]
}
