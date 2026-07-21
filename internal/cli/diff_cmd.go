package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/ahmojo/codex-claude-transfer/internal/agent"
	"github.com/ahmojo/codex-claude-transfer/internal/bundle"
)

// runDiff previews what importing a bundle would do, read-only. It runs the full
// import planner with --merge semantics and --dry-run (nothing is written), then
// shows which sessions are new, which would grow (and by how many lines), which
// are already present, and which would conflict — the confidence check before a
// real import. It honors the same selection/remap flags as import so the preview
// matches the command you would run.
func runDiff(args []string, stdout, stderr io.Writer) int {
	f, err := parseFlags(args)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	if len(f.positional) != 1 {
		fmt.Fprintln(stderr, "usage: cct diff <file.codexbundle> [--merge is implied] [--session <id>] [--project <path>] [--since <when>] [--match <q>] [--map-cwd OLD=NEW | --map-cwd-here] [--json]")
		return 2
	}

	var absProject string
	if f.project != "" {
		absProject, err = filepath.Abs(f.project)
		if err != nil {
			fmt.Fprintf(stderr, "error: cannot resolve project path %q: %v\n", f.project, err)
			return 1
		}
	}
	var since time.Time
	if f.since != "" {
		since, err = parseSince(f.since)
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 2
		}
	}
	if f.mapCWDHere && len(f.mapCWD) > 0 {
		fmt.Fprintln(stderr, "error: --map-cwd and --map-cwd-here are mutually exclusive (use one or the other)")
		return 2
	}
	mappings, err := bundle.ParseCWDMappings(f.mapCWD)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	var hereDir string
	if f.mapCWDHere {
		hereDir, err = os.Getwd()
		if err != nil {
			fmt.Fprintf(stderr, "error: --map-cwd-here: cannot determine the current directory: %v\n", err)
			return 1
		}
	}

	bundlePath, cleanup, code := resolveBundlePath(f, stderr)
	if code != 0 {
		return code
	}
	defer cleanup()

	kind, home, ok := resolveImportTarget(f, bundlePath, stderr)
	if !ok {
		return 1
	}

	res, err := bundle.Import(home, bundle.ImportOptions{
		BundlePath:         bundlePath,
		DryRun:             true, // diff never writes
		Merge:              true, // show growth vs. conflict, not just "conflict"
		ProjectPath:        absProject,
		ProjectFilter:      absProject != "",
		Since:              since,
		Match:              f.match,
		MatchRegex:         f.regex,
		MatchCaseSensitive: f.caseSensitive,
		MapCWD:             mappings,
		MapCWDHere:         f.mapCWDHere,
		HereDir:            hereDir,
		SessionIDs:         f.sessions,
	})
	if err != nil {
		fmt.Fprintf(stderr, "error: diff failed: %v\n", err)
		return 1
	}

	if f.jsonOut {
		printDiffJSON(stdout, f.positional[0], res)
	} else {
		printDiff(stdout, kind, f.positional[0], res)
	}
	return 0
}

// printDiff renders the read-only import preview.
func printDiff(w io.Writer, kind agent.Kind, path string, res bundle.ImportResult) {
	byPath := map[string]bundle.ManifestSession{}
	for _, ms := range res.Manifest.Sessions {
		byPath[ms.BundlePath] = ms
	}

	fmt.Fprintf(w, "Bundle: %s\n", path)
	fmt.Fprintf(w, "Comparing %s against your %s sessions (read-only)\n\n", plural(len(res.Manifest.Sessions), "session"), kind.Label())

	fmt.Fprintf(w, "  new        %d   would be imported\n", res.Imported)
	fmt.Fprintf(w, "  grow       %d   would append new messages\n", res.Updated)
	fmt.Fprintf(w, "  identical  %d   already present, unchanged\n", res.SkippedIdentical)
	fmt.Fprintf(w, "  ahead      %d   local already has these (and more)\n", res.AlreadyAhead)
	fmt.Fprintf(w, "  conflict   %d   changed on both sides\n", res.Conflicts)
	if res.SkippedDeselected > 0 {
		fmt.Fprintf(w, "  filtered   %d   excluded by your filters\n", res.SkippedDeselected)
	}

	// Detail the entries that would actually change or block, in a useful order:
	// conflicts first (they need attention), then growth, then new sessions.
	var news, grows, conflicts []bundle.ImportItem
	for _, it := range res.Items {
		switch it.Action {
		case bundle.ActionImport:
			news = append(news, it)
		case bundle.ActionUpdate:
			grows = append(grows, it)
		case bundle.ActionConflict:
			conflicts = append(conflicts, it)
		}
	}

	if len(conflicts) > 0 {
		fmt.Fprintln(w, "\nConflicts (changed on both sides — would be skipped):")
		for _, it := range conflicts {
			fmt.Fprintf(w, "  ! %s\n", diffLabel(byPath, it))
		}
	}
	if len(grows) > 0 {
		fmt.Fprintln(w, "\nWould grow (append-only, lossless):")
		for _, it := range grows {
			suffix := ""
			if it.LinesAdded > 0 {
				suffix = fmt.Sprintf("  +%s", plural(it.LinesAdded, "line"))
			}
			fmt.Fprintf(w, "  ^ %s%s\n", diffLabel(byPath, it), suffix)
		}
	}
	if len(news) > 0 {
		fmt.Fprintln(w, "\nNew sessions:")
		for _, it := range news {
			fmt.Fprintf(w, "  + %s\n", diffLabel(byPath, it))
		}
	}

	if len(res.Warnings) > 0 {
		fmt.Fprintln(w)
		for _, warn := range res.Warnings {
			fmt.Fprintf(w, "warning: %s\n", safeTerminal(warn))
		}
	}

	fmt.Fprintln(w)
	if res.Imported == 0 && res.Updated == 0 && res.Conflicts == 0 {
		fmt.Fprintln(w, "Nothing would change: your sessions are already up to date.")
		return
	}
	fmt.Fprintln(w, "Read-only: nothing was changed. To apply:")
	if res.Conflicts > 0 {
		fmt.Fprintf(w, "  cct import %s --merge   (conflicts stay skipped; add --replace-with-backup or --import-as-copy to resolve them)\n", path)
	} else {
		fmt.Fprintf(w, "  cct import %s --merge\n", path)
	}
}

// diffLabel renders a session as "<short-id>  \"preview\"", falling back to the
// bundle path when the manifest lacks a thread id/preview.
func diffLabel(byPath map[string]bundle.ManifestSession, it bundle.ImportItem) string {
	ms, ok := byPath[it.BundlePath]
	if !ok {
		return safeTerminal(it.BundlePath)
	}
	id := short(ms.ThreadID)
	if id == "" {
		return safeTerminal(it.BundlePath)
	}
	if ms.Preview != "" {
		return fmt.Sprintf("%s  %q", id, truncate(safeTerminal(ms.Preview), 56))
	}
	return id
}
