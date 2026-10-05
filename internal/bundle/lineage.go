package bundle

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ahmojo/codex-claude-transfer/internal/codexhome"
	"github.com/ahmojo/codex-claude-transfer/internal/sessions"
	"github.com/ahmojo/codex-claude-transfer/internal/zstdcli"
)

// History positions reference physical rollout filenames, not logical session IDs.
func rolloutID(name string) string {
	name = strings.TrimSuffix(strings.TrimSuffix(filepath.Base(name), ".zst"), ".jsonl")
	if len(name) < 37 || name[len(name)-37] != '-' {
		return ""
	}
	return name[len(name)-36:]
}

func expandCodexHistory(home codexhome.Home, selected []sessions.Session, opts ExportOptions) ([]sessions.Session, error) {
	byID := map[string]sessions.Session{}
	included := map[string]bool{}
	for _, s := range selected {
		byID[rolloutID(s.FileName)] = s
		included[rolloutID(s.FileName)] = true
	}
	loaded := false
	for i := 0; i < len(selected); i++ {
		id := selected[i].HistoryBaseID
		if id == "" {
			continue
		}
		if opts.Redact || opts.StripImages {
			return nil, fmt.Errorf("cannot transform inherited Codex history without updating history positions; export without --redact/--strip-images")
		}
		if included[id] {
			continue
		}
		if !loaded {
			scan, err := sessions.Scan(home, sessions.ScanOptions{IncludeArchived: true, DecompressCompressed: true})
			if err != nil {
				return nil, err
			}
			for _, s := range scan.Sessions {
				byID[rolloutID(s.FileName)] = s
			}
			loaded = true
		}
		parent, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("Codex fork requires missing source rollout %s", id)
		}
		selected = append(selected, parent)
		included[id] = true
	}
	return selected, nil
}

func historyBase(content []byte) (string, error) {
	sc := bufio.NewScanner(bytes.NewReader(content))
	sc.Buffer(make([]byte, 4096), 16*1024*1024)
	for sc.Scan() {
		var line struct {
			Type    string `json:"type"`
			Payload struct {
				HistoryBase *struct {
					ThreadID string `json:"thread_id"`
				} `json:"history_base"`
			} `json:"payload"`
		}
		if json.Unmarshal(sc.Bytes(), &line) == nil && line.Type == "session_meta" {
			if line.Payload.HistoryBase != nil {
				if line.Payload.HistoryBase.ThreadID == "" {
					return "", fmt.Errorf("invalid Codex history_base")
				}
				return line.Payload.HistoryBase.ThreadID, nil
			}
			return "", nil
		}
	}
	return "", sc.Err()
}

// Check the complete selection before planning writes. Rewriting a referenced
// prefix changes its byte offsets; copying its ID can redirect a fork to local history.
func checkCodexHistory(zr *zip.Reader, manifest Manifest, selected map[string]bool, opts ImportOptions, mappings []CWDMapping) (bool, error) {
	paths := map[string]string{}
	for _, ms := range manifest.Sessions {
		if isImportableEntryForImport("codex", ms.BundlePath, opts.IncludeArchived) && (selected == nil || selected[ms.BundlePath]) {
			paths[rolloutID(ms.BundlePath)] = ms.BundlePath
		}
	}
	hasHistory := false
	for _, path := range paths {
		content, err := readEntryBytes(zr, path)
		if err != nil {
			return false, err
		}
		if strings.HasSuffix(path, ".zst") {
			if !zstdcli.Available() {
				continue
			}
			content, err = zstdcli.Decompress(content)
			if err != nil {
				continue // opaque compressed entries retain the byte-for-byte import behavior
			}
		}
		base, err := historyBase(content)
		if err != nil {
			return false, err
		}
		if base == "" {
			continue
		}
		hasHistory = true
		if opts.ImportAsCopy || len(mappings) > 0 {
			return false, fmt.Errorf("Codex forks with inherited history cannot be copied or cwd-mapped safely; import unchanged into a separate home")
		}
		if _, ok := paths[base]; !ok {
			return false, fmt.Errorf("Codex fork requires source rollout %s in the import selection; include its parent and use --include-archived if needed", base)
		}
	}
	return hasHistory, nil
}
