package cli

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/ahmojo/codex-claude-transfer/internal/agent"
	"github.com/ahmojo/codex-claude-transfer/internal/meta"
	"github.com/ahmojo/codex-claude-transfer/internal/search"
	"github.com/ahmojo/codex-claude-transfer/internal/sessions"
	"github.com/charmbracelet/huh"
	"github.com/mattn/go-isatty"
)

// runBrowse is an interactive session browser: search, pick a session, then act
// on it (resume, export, annotate). It builds and runs the same commands the
// flag CLI exposes, so nothing is hidden and behavior is identical.
func runBrowse(args []string, stdout, stderr io.Writer) int {
	f, err := parseFlags(args)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	if !isatty.IsTerminal(os.Stdin.Fd()) && !isatty.IsCygwinTerminal(os.Stdin.Fd()) {
		fmt.Fprintln(stderr, "error: `cct browse` needs an interactive terminal.")
		fmt.Fprintln(stderr, "Use `cct search`, `cct resume`, or `cct list` non-interactively instead.")
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
	all := filterForSearch(f, scan.Sessions, stderr)
	store, _ := meta.Load(cctConfigDir())

	for {
		var query string
		if !runField(huh.NewInput().
			Title("Browse sessions — type to filter (blank = all, most recent first)").
			Value(&query), stderr) {
			return 0
		}
		results := filterBrowse(all, strings.TrimSpace(query), f)
		if len(results) == 0 {
			fmt.Fprintln(stdout, "No sessions matched; try another search.")
			continue
		}

		opts := make([]huh.Option[string], 0, len(results)+1)
		for _, s := range results {
			opts = append(opts, huh.NewOption(browseLabel(s, store.Get(s.ThreadID)), s.ThreadID))
		}
		opts = append(opts, huh.NewOption("↩  new search / quit", ""))

		var pick string
		if !runField(huh.NewSelect[string]().
			Title(fmt.Sprintf("%s — choose one", plural(len(results), "match"))).
			Options(opts...).
			Value(&pick), stderr) {
			return 0
		}
		if pick == "" {
			var again bool
			if !runField(huh.NewConfirm().
				Title("Search again?").
				Affirmative("Search").
				Negative("Quit").
				Value(&again), stderr) || !again {
				return 0
			}
			continue
		}
		if !browseAct(f, kind, pick, stdout, stderr) {
			return 0
		}
		// Refresh annotations so newly added tags/names show on the next pass.
		store, _ = meta.Load(cctConfigDir())
	}
}

// browseAct presents the per-session action menu. It returns false to quit.
func browseAct(f commonFlags, kind agent.Kind, threadID string, stdout, stderr io.Writer) bool {
	var action string
	if !runField(huh.NewSelect[string]().
		Title("What would you like to do with this session?").
		Options(
			huh.NewOption("Show how to resume it", "resume"),
			huh.NewOption("Export it to a bundle", "export"),
			huh.NewOption("Save it as Markdown", "md"),
			huh.NewOption("Save it as HTML", "html"),
			huh.NewOption("Tag it", "tag"),
			huh.NewOption("Rename it", "name"),
			huh.NewOption("← back to results", "back"),
		).
		Value(&action), stderr) {
		return false
	}
	switch action {
	case "back":
		return true
	case "resume":
		runBuilt(withToolHome([]string{"resume", threadID}, kind, f), stdout, stderr)
	case "export":
		runBuilt(withToolHome([]string{"export", "--session", threadID}, kind, f), stdout, stderr)
	case "md":
		runBuilt(withToolHome([]string{"export", "--session", threadID, "--format", "md"}, kind, f), stdout, stderr)
	case "html":
		runBuilt(withToolHome([]string{"export", "--session", threadID, "--format", "html"}, kind, f), stdout, stderr)
	case "tag":
		var tag string
		if runField(huh.NewInput().Title("Tag to add (e.g. wip, review)").Value(&tag), stderr) && strings.TrimSpace(tag) != "" {
			runBuilt(withToolHome([]string{"tag", "add", threadID, strings.TrimSpace(tag)}, kind, f), stdout, stderr)
		}
	case "name":
		var name string
		if runField(huh.NewInput().Title("Friendly name for this session").Value(&name), stderr) && strings.TrimSpace(name) != "" {
			runBuilt(withToolHome([]string{"name", threadID, strings.TrimSpace(name)}, kind, f), stdout, stderr)
		}
	}
	return true
}

// filterBrowse returns the sessions to show for a query: all (most-recent-first)
// when the query is blank, otherwise the relevance-ranked full-text matches. Only
// sessions with a thread id are returned (the rest are not actionable).
func filterBrowse(list []sessions.Session, query string, f commonFlags) []sessions.Session {
	withID := make([]sessions.Session, 0, len(list))
	for _, s := range list {
		if s.ThreadID != "" {
			withID = append(withID, s)
		}
	}
	if query == "" {
		sort.SliceStable(withID, func(i, j int) bool {
			return withID[i].UpdatedAt().After(withID[j].UpdatedAt())
		})
		return withID
	}
	matches, err := search.Search(withID, search.Query{Text: query, Regex: f.regex, CaseSensitive: f.caseSensitive})
	if err != nil {
		return nil
	}
	out := make([]sessions.Session, 0, len(matches))
	for _, m := range matches {
		out = append(out, m.Session)
	}
	return out
}

// browseLabel renders a one-line, length-bounded description of a session for the
// picker, prefixed with its cct name/tags when set.
func browseLabel(s sessions.Session, e meta.Entry) string {
	head := s.Preview
	if e.Name != "" {
		head = e.Name
	} else if head == "" {
		head = s.FileName
	}
	head = truncate(head, 48)
	var extra string
	if len(e.Tags) > 0 {
		extra = " [" + strings.Join(e.Tags, ",") + "]"
	}
	cwd := s.CWD
	if cwd == "" {
		cwd = "(no project)"
	}
	return fmt.Sprintf("%s%s — %s — %s", head, extra, truncate(cwd, 32), s.UpdatedAt().Format("2006-01-02"))
}
