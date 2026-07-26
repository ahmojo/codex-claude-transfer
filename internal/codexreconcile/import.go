package codexreconcile

import (
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ahmojo/codex-claude-transfer/internal/bundle"
	"github.com/ahmojo/codex-claude-transfer/internal/codexhome"
	"github.com/ahmojo/codex-claude-transfer/internal/sessions"
)

// ImportThreads identifies the exact threads whose rollout bytes changed in an
// import. Unknown is non-zero when an affected rollout could not be parsed.
type ImportThreads struct {
	IDs     []string
	Unknown int
}

// ThreadsChangedByImport reads the imported destination rollouts and returns
// their actual session_meta IDs. It deliberately does not trust manifest thread
// IDs, which are metadata rather than the source of truth Codex will read.
func ThreadsChangedByImport(home codexhome.Home, result bundle.ImportResult) (ImportThreads, error) {
	var changed ImportThreads
	seen := make(map[string]bool)
	type pathResult struct {
		id  string
		err error
	}
	byPath := make(map[string]pathResult)
	for _, item := range result.Items {
		switch item.Action {
		case bundle.ActionImport, bundle.ActionReplace, bundle.ActionUpdate, bundle.ActionImportCopy:
		default:
			continue
		}
		if !withinDirectory(item.DestPath, home.SessionsDir) &&
			!withinDirectory(item.DestPath, home.ArchivedSessionsDir) {
			changed.Unknown++
			continue
		}
		key := comparablePath(item.DestPath)
		read, ok := byPath[key]
		if !ok {
			read.id, read.err = sessions.ReadThreadID(item.DestPath)
			byPath[key] = read
		}
		if read.err != nil || !ValidThreadID(read.id) {
			changed.Unknown++
			continue
		}
		if !seen[read.id] {
			seen[read.id] = true
			changed.IDs = append(changed.IDs, read.id)
		}
	}
	return changed, nil
}

func withinDirectory(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func comparablePath(path string) string {
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		return strings.ToLower(path)
	}
	return path
}
