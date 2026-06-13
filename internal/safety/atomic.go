package safety

import (
	"io"
	"os"
	"path/filepath"
)

// CopyAtomic streams r into dest atomically: it creates any missing parent
// directories, writes to a temp file in the destination directory, fsyncs it,
// and renames it into place. On any failure before the rename, the temp file is
// removed and dest is left untouched.
func CopyAtomic(dest string, r io.Reader) (err error) {
	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".codex-sync-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			tmp.Close()
			os.Remove(tmpName)
		}
	}()

	if _, err := io.Copy(tmp, r); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, dest); err != nil {
		return err
	}
	committed = true
	return nil
}
