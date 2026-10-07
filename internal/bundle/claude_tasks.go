package bundle

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ahmojo/codex-claude-transfer/internal/codexhome"
	"github.com/ahmojo/codex-claude-transfer/internal/safety"
	"github.com/ahmojo/codex-claude-transfer/internal/secrets"
)

func claudeTaskParents(manifest Manifest) map[string]string {
	parents := map[string]string{}
	for _, ms := range manifest.Sessions {
		_, id, _, child := safety.ClaudeSessionGroup(ms.BundlePath)
		if id != "" && !child && ms.ThreadID == id {
			parents[id] = ms.BundlePath
		}
	}
	return parents
}

func validateClaudeTask(rel string, data []byte) error {
	var task struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &task); err != nil {
		return fmt.Errorf("unsupported Claude task %s: %w", rel, err)
	}
	if task.ID != strings.TrimSuffix(path.Base(rel), ".json") {
		return fmt.Errorf("Claude task %s has a mismatched or missing id", rel)
	}
	return nil
}

func addClaudeTasks(zw *zip.Writer, opts ExportOptions, manifest *Manifest, checksums Checksums, result *ExportResult) error {
	var ids []string
	for id := range claudeTaskParents(*manifest) {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		dir, err := safety.DestPath(opts.ClaudeHome.Root, "tasks/"+id)
		if err != nil {
			return err
		}
		entries, err := os.ReadDir(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		for _, entry := range entries {
			rel := "tasks/" + id + "/" + entry.Name()
			if !safety.IsClaudeTaskEntry(rel) {
				continue
			}
			file, err := safety.DestPath(opts.ClaudeHome.Root, rel)
			if err != nil {
				return err
			}
			f, err := os.Open(file)
			if err != nil {
				return err
			}
			data, err := readCapped(f, MaxMetadataBytes, "Claude task "+rel)
			f.Close()
			if err != nil {
				return err
			}
			if err := validateClaudeTask(rel, data); err != nil {
				return err
			}
			if opts.Redact {
				var n int
				data, n = secrets.Redact(data)
				result.SecretsRedacted += n
			}
			if err := addBytesToZip(zw, rel, data); err != nil {
				return err
			}
			sum := sha256Hex(data)
			checksums[rel] = sum
			manifest.Tasks = append(manifest.Tasks, ManifestTask{SessionID: id, BundlePath: rel, SHA256: sum})
		}
	}
	return nil
}

// Validate every task before import plans or writes any transcript. Task paths
// must be declared and bound to a parent conversation in the same bundle.
func verifyClaudeTasks(zr *zip.Reader, manifest Manifest, checksums Checksums) error {
	parents := claudeTaskParents(manifest)
	declared := map[string]bool{}
	for _, task := range manifest.Tasks {
		rel := task.BundlePath
		if _, err := safety.CleanRelPath(rel); err != nil {
			return err
		}
		if !safety.IsClaudeTaskEntry(rel) || !strings.HasPrefix(rel, "tasks/"+task.SessionID+"/") || parents[task.SessionID] == "" {
			return fmt.Errorf("Claude task %q is not bound to its parent conversation", rel)
		}
		if declared[rel] || task.SHA256 == "" || task.SHA256 != checksums[rel] {
			return fmt.Errorf("invalid or duplicate Claude task declaration %q", rel)
		}
		declared[rel] = true
		data, err := readEntryBytes(zr, rel, MaxMetadataBytes)
		if err != nil {
			return err
		}
		if err := validateClaudeTask(rel, data); err != nil {
			return err
		}
	}
	seen := map[string]bool{}
	for _, f := range zr.File {
		if safety.IsClaudeTaskEntry(f.Name) {
			if !declared[f.Name] || seen[f.Name] {
				return fmt.Errorf("hidden or duplicate Claude task %q", f.Name)
			}
			seen[f.Name] = true
		}
	}
	return nil
}

func planClaudeTasks(home codexhome.Home, manifest Manifest, opts ImportOptions, result *ImportResult, writes map[string]string) error {
	parents := claudeTaskParents(manifest)
	actions := map[string]Action{}
	for _, item := range result.Items {
		actions[item.BundlePath] = item.Action
	}
	for _, task := range manifest.Tasks {
		rel := task.BundlePath
		item := ImportItem{BundlePath: rel, Task: true}
		switch actions[parents[task.SessionID]] {
		case ActionImport, ActionUpdate, ActionReplace, ActionSkipIdentical:
		default:
			item.Action = ActionSkipDeselected
			result.Warnings = append(result.Warnings, rel+": task skipped because its parent was not imported or identical")
			result.Items = append(result.Items, item)
			continue
		}
		dest, err := safety.DestPath(home.Root, rel)
		if err != nil {
			return err
		}
		item.DestPath = dest
		item.Action, err = decideAction(dest, task.SHA256)
		if err != nil {
			return err
		}
		if item.Action == ActionConflict && opts.ReplaceWithBackup {
			item.Action = ActionReplace
		}
		switch item.Action {
		case ActionImport, ActionReplace:
			key := strings.ToLower(filepath.Clean(dest))
			if previous, ok := writes[key]; ok {
				return fmt.Errorf("bundle entries %q and %q target the same destination", previous, rel)
			}
			writes[key] = rel
			result.TasksImported++
		case ActionConflict:
			result.TaskConflicts++
			result.Warnings = append(result.Warnings, rel+": existing task differs; skipped (use --replace-with-backup to keep a backup and replace it)")
		}
		result.Items = append(result.Items, item)
	}
	return nil
}
