package codexreconcile

import (
	"fmt"
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
	scan, err := sessions.Scan(home, sessions.ScanOptions{DecompressCompressed: true})
	if err != nil {
		return ImportThreads{}, fmt.Errorf("scan imported Codex rollouts for exact thread IDs: %w", err)
	}
	byPath := make(map[string]string, len(scan.Sessions))
	for _, session := range scan.Sessions {
		byPath[comparablePath(session.Path)] = session.ThreadID
	}

	var changed ImportThreads
	seen := make(map[string]bool)
	for _, item := range result.Items {
		switch item.Action {
		case bundle.ActionImport, bundle.ActionReplace, bundle.ActionUpdate, bundle.ActionImportCopy:
		default:
			continue
		}
		id := strings.TrimSpace(byPath[comparablePath(item.DestPath)])
		if !ValidThreadID(id) {
			changed.Unknown++
			continue
		}
		if !seen[id] {
			seen[id] = true
			changed.IDs = append(changed.IDs, id)
		}
	}
	return changed, nil
}

func comparablePath(path string) string {
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		return strings.ToLower(path)
	}
	return path
}
