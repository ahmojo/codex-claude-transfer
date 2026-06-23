package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/ahmojo/codex-claude-transfer/internal/meta"
	"github.com/ahmojo/codex-claude-transfer/internal/sessions"
)

const tagUsage = `usage:
  cct tag add <session> <tag>...   add tags to a session
  cct tag rm  <session> <tag>...   remove tags from a session
  cct tag ls  [tag]                list every tag in use, or sessions carrying <tag>
  cct name <session> [text...]     set (or, with no text, show) a session's name

<session> is a thread id or a unique prefix of one. Tags and names are cct's own
notes, stored under its config dir — they never touch the agent's session files.`

// runTag manages per-session tags (the cct-only organizational sidecar).
func runTag(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, tagUsage)
		return 2
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "ls":
		return runTagLs(rest, stdout, stderr)
	case "add", "rm":
		return runTagAddRm(sub, rest, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown tag subcommand %q\n\n%s\n", sub, tagUsage)
		return 2
	}
}

func runTagAddRm(sub string, rest []string, stdout, stderr io.Writer) int {
	f, err := parseFlags(rest)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	if len(f.positional) < 2 {
		fmt.Fprintf(stderr, "usage: cct tag %s <session> <tag>...\n", sub)
		return 2
	}
	id, code := resolveSessionID(f, f.positional[0], stderr)
	if code != 0 {
		return code
	}
	dir := cctConfigDir()
	store, err := meta.Load(dir)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	var changed []string
	for _, tag := range f.positional[1:] {
		if sub == "add" {
			if store.AddTag(id, tag) {
				changed = append(changed, meta.NormalizeTag(tag))
			}
		} else {
			if store.RemoveTag(id, tag) {
				changed = append(changed, meta.NormalizeTag(tag))
			}
		}
	}
	if err := store.Save(dir); err != nil {
		fmt.Fprintf(stderr, "error: cannot save tags: %v\n", err)
		return 1
	}
	verb := "Tagged"
	if sub == "rm" {
		verb = "Untagged"
	}
	if len(changed) == 0 {
		fmt.Fprintln(stdout, "No change (tags already in the requested state).")
	} else {
		fmt.Fprintf(stdout, "%s %s: %s\n", verb, id, strings.Join(changed, ", "))
	}
	if e := store.Get(id); len(e.Tags) > 0 {
		fmt.Fprintf(stdout, "Now: %s\n", strings.Join(e.Tags, ", "))
	}
	return 0
}

func runTagLs(rest []string, stdout, stderr io.Writer) int {
	f, err := parseFlags(rest)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	dir := cctConfigDir()
	store, err := meta.Load(dir)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	if len(f.positional) == 0 {
		tags := store.AllTags()
		if len(tags) == 0 {
			fmt.Fprintln(stdout, "No tags yet. Add one with: cct tag add <session> <tag>")
			return 0
		}
		fmt.Fprintln(stdout, "Tags in use:")
		for _, t := range tags {
			fmt.Fprintf(stdout, "  %s  (%s)\n", t, plural(len(store.IDsWithTag(t)), "session"))
		}
		return 0
	}
	tag := f.positional[0]
	ids := store.IDsWithTag(tag)
	if len(ids) == 0 {
		fmt.Fprintf(stdout, "No sessions tagged %q.\n", meta.NormalizeTag(tag))
		return 0
	}
	fmt.Fprintf(stdout, "Sessions tagged %q:\n", meta.NormalizeTag(tag))
	for _, id := range ids {
		e := store.Get(id)
		if e.Name != "" {
			fmt.Fprintf(stdout, "  %s  — %s\n", id, safeTerminal(e.Name))
		} else {
			fmt.Fprintf(stdout, "  %s\n", id)
		}
	}
	return 0
}

// runName sets or shows a session's friendly name.
func runName(args []string, stdout, stderr io.Writer) int {
	f, err := parseFlags(args)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	if len(f.positional) < 1 {
		fmt.Fprintln(stderr, "usage: cct name <session> [text...]   (no text shows the current name)")
		return 2
	}
	id, code := resolveSessionID(f, f.positional[0], stderr)
	if code != 0 {
		return code
	}
	dir := cctConfigDir()
	store, err := meta.Load(dir)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	if len(f.positional) == 1 {
		e := store.Get(id)
		if e.Name == "" {
			fmt.Fprintf(stdout, "%s has no name. Set one with: cct name %s <text>\n", id, id)
		} else {
			fmt.Fprintf(stdout, "%s: %s\n", id, safeTerminal(e.Name))
		}
		return 0
	}
	name := strings.Join(f.positional[1:], " ")
	store.SetName(id, name)
	if err := store.Save(dir); err != nil {
		fmt.Fprintf(stderr, "error: cannot save name: %v\n", err)
		return 1
	}
	if strings.TrimSpace(name) == "" {
		fmt.Fprintf(stdout, "Cleared the name for %s.\n", id)
	} else {
		fmt.Fprintf(stdout, "Named %s: %s\n", id, safeTerminal(strings.TrimSpace(name)))
	}
	return 0
}

// resolveSessionID turns a thread-id prefix into a full thread id by scanning the
// selected agent's sessions. An exact id that isn't found locally is accepted
// as-is (so you can annotate a session you're about to import); an ambiguous or
// unknown prefix is an error.
func resolveSessionID(f commonFlags, idPrefix string, stderr io.Writer) (string, int) {
	kind, ok := resolveTool(f, stderr)
	if !ok {
		return "", 2
	}
	scan, code := scanForSearch(f, kind, stderr)
	if code != 0 {
		// Scanning failed (e.g. no home); fall back to the literal id rather than
		// blocking annotation entirely.
		return idPrefix, 0
	}
	var matches []sessions.Session
	for _, s := range scan.Sessions {
		if s.ThreadID == idPrefix {
			return idPrefix, 0
		}
		if s.ThreadID != "" && strings.HasPrefix(s.ThreadID, idPrefix) {
			matches = append(matches, s)
		}
	}
	switch len(matches) {
	case 0:
		// Looks like a full id the user typed deliberately; accept it.
		if looksLikeFullID(idPrefix) {
			return idPrefix, 0
		}
		fmt.Fprintf(stderr, "error: no %s session matches %q\n", kind.Label(), idPrefix)
		return "", 1
	case 1:
		return matches[0].ThreadID, 0
	default:
		fmt.Fprintf(stderr, "error: %q is ambiguous (%d sessions); use a longer id\n", idPrefix, len(matches))
		return "", 1
	}
}

// looksLikeFullID reports whether s is plausibly a complete thread id (a UUID-ish
// token), so annotating a not-yet-imported session is allowed but a typo'd short
// prefix is still rejected.
func looksLikeFullID(s string) bool {
	if len(s) < 16 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F', r == '-':
		default:
			return false
		}
	}
	return true
}
