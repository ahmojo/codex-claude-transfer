package lansync

import (
	"io/fs"
	"path/filepath"
	"strings"
)

// The daemon detects "something changed" by polling a lightweight snapshot of the
// session files (path -> size+mtime) rather than depending on a filesystem-watch
// library. Polling is stdlib-only, cross-platform, and entirely sufficient at the
// human cadence sessions actually change. The snapshot diff is pure so it can be
// unit-tested, and snapshot() works against a real directory tree (verified with a
// temp dir) so there are no cross-platform path surprises.

// fileState is the cheap fingerprint of a session file used to notice changes.
type fileState struct {
	Size          int64
	MtimeUnixNano int64
}

// snapshot records the size+mtime of every session-looking file (*.jsonl /
// *.jsonl.zst) under the given absolute roots.
func snapshot(roots []string) map[string]fileState {
	out := map[string]fileState{}
	for _, root := range roots {
		if root == "" {
			continue
		}
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // unreadable subtree: skip, never fail the whole snapshot
			}
			if d.IsDir() || !isSessionFile(p) {
				return nil
			}
			info, ierr := d.Info()
			if ierr != nil {
				return nil
			}
			out[p] = fileState{Size: info.Size(), MtimeUnixNano: info.ModTime().UnixNano()}
			return nil
		})
	}
	return out
}

// isSessionFile reports whether a path looks like a session rollout/transcript.
func isSessionFile(p string) bool {
	return strings.HasSuffix(p, ".jsonl") || strings.HasSuffix(p, ".jsonl.zst")
}

// snapshotChanged reports whether two snapshots differ in any file's presence,
// size, or mtime — i.e. whether a sync-worthy change happened.
func snapshotChanged(old, cur map[string]fileState) bool {
	if len(old) != len(cur) {
		return true
	}
	for p, s := range cur {
		o, ok := old[p]
		if !ok || o != s {
			return true
		}
	}
	return false
}
